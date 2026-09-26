package llmrelay

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"github.com/ialtone/fatecraft/internal/llm"
)

// testServer 启动带 Registry 的 HTTP 服务器，返回注册表与 WS 地址。
func testServer(t *testing.T, registry *Registry) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 测试不做会话鉴权，直接用固定玩家 ID。
		registry.Serve(w, r, testPlayerID)
	}))
	t.Cleanup(server.Close)
	url := "ws" + strings.TrimPrefix(server.URL, "http")
	testRegistries.Store(url, registry)
	t.Cleanup(func() { testRegistries.Delete(url) })
	return url
}

var testPlayerID = uuid.New()

// testRegistries 记录测试服务器地址对应的注册表，供 dialBrowser 等待服务端注册完成。
var testRegistries sync.Map

func registeredConns(registry *Registry) int {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	return len(registry.conns[testPlayerID])
}

// testBrowser 是假浏览器端：拨号、读写 JSON 帧。
type testBrowser struct {
	t  *testing.T
	ws *websocket.Conn
	mu sync.Mutex
}

func dialBrowser(t *testing.T, url string) *testBrowser {
	t.Helper()
	var registry *Registry
	before := 0
	if value, ok := testRegistries.Load(url); ok {
		registry = value.(*Registry)
		before = registeredConns(registry)
	}
	ws, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	browser := &testBrowser{t: t, ws: ws}
	t.Cleanup(func() { _ = ws.Close() })
	// Dial 返回只代表握手完成，服务端 Serve 里的 add 仍可能在途：
	// 等注册完成再返回，否则紧随其后的 Do 可能报 ErrRelayUnavailable 或派发到旧连接。
	if registry != nil {
		deadline := time.Now().Add(3 * time.Second)
		for registeredConns(registry) <= before {
			if time.Now().After(deadline) {
				t.Fatal("relay connection was not registered in time")
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	return browser
}

func (b *testBrowser) readFrame() map[string]any {
	b.t.Helper()
	_ = b.ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, raw, err := b.ws.ReadMessage()
	if err != nil {
		b.t.Fatalf("browser read: %v", err)
	}
	var frame map[string]any
	if err := json.Unmarshal(raw, &frame); err != nil {
		b.t.Fatalf("browser frame decode: %v", err)
	}
	return frame
}

func (b *testBrowser) writeFrame(frame any) {
	b.t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	_ = b.ws.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if err := b.ws.WriteJSON(frame); err != nil {
		b.t.Fatalf("browser write: %v", err)
	}
}

// awaitRequest 读取一帧 relay.request 并返回其 relay_id 与 body。
func (b *testBrowser) awaitRequest() (relayID, body string) {
	b.t.Helper()
	frame := b.readFrame()
	if frame["type"] != msgRelayRequest {
		b.t.Fatalf("frame type = %v, want %s", frame["type"], msgRelayRequest)
	}
	id, _ := frame["relay_id"].(string)
	content, _ := frame["body"].(string)
	return id, content
}

func (b *testBrowser) respondHead(relayID string, status int, contentType string) {
	b.writeFrame(clientHeadFrame{Type: msgRelayHead, RelayID: relayID, Status: status, ContentType: contentType})
}

func (b *testBrowser) respondChunks(relayID string, chunks ...string) {
	for index, chunk := range chunks {
		b.writeFrame(clientChunkFrame{Type: msgRelayChunk, RelayID: relayID, Seq: index + 1, Data: chunk})
	}
}

func (b *testBrowser) respondEnd(relayID string, chunks int) {
	b.writeFrame(clientEndFrame{Type: msgRelayEnd, RelayID: relayID, Seq: chunks})
}

func testRegistry() *Registry {
	registry := NewRegistry(nil, nil)
	registry.headTimeout = 500 * time.Millisecond
	return registry
}

func readAll(t *testing.T, resp *http.Response) string {
	t.Helper()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	return string(raw)
}

func TestRelayRoundTrip(t *testing.T) {
	registry := testRegistry()
	browser := dialBrowser(t, testServer(t, registry))
	if !registry.HasActive(testPlayerID.String()) {
		t.Fatal("HasActive = false after dial")
	}
	type result struct {
		resp *http.Response
		err  error
	}
	done := make(chan result, 1)
	go func() {
		resp, err := registry.For(testPlayerID.String()).Do(context.Background(), llm.RelayRequest{
			Endpoint: "/chat/completions", Body: []byte(`{"model":"m"}`), Stream: true, Purpose: "narrative",
		})
		done <- result{resp: resp, err: err}
	}()
	relayID, body := browser.awaitRequest()
	if body != `{"model":"m"}` {
		t.Fatalf("request body = %q", body)
	}
	browser.respondHead(relayID, 200, "text/event-stream")
	browser.respondChunks(relayID, "data: 1\n\n", "data: 2\n\n")
	browser.respondEnd(relayID, 2)
	outcome := <-done
	if outcome.err != nil {
		t.Fatal(outcome.err)
	}
	if outcome.resp.StatusCode != 200 || outcome.resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("response = %+v", outcome.resp)
	}
	if content := readAll(t, outcome.resp); content != "data: 1\n\ndata: 2\n\n" {
		t.Fatalf("response body = %q", content)
	}
}

func TestRelayHeadTimeoutIsRetryable(t *testing.T) {
	registry := testRegistry()
	_ = dialBrowser(t, testServer(t, registry))
	_, err := registry.For(testPlayerID.String()).Do(context.Background(), llm.RelayRequest{
		Endpoint: "/responses", Body: []byte(`{}`),
	})
	if !errors.Is(err, llm.ErrRelayUnavailable) {
		t.Fatalf("head timeout error = %v, want ErrRelayUnavailable", err)
	}
}

func TestRelayDisconnectBeforeHeadIsRetryable(t *testing.T) {
	registry := testRegistry()
	browser := dialBrowser(t, testServer(t, registry))
	type result struct {
		err error
	}
	done := make(chan result, 1)
	go func() {
		_, err := registry.For(testPlayerID.String()).Do(context.Background(), llm.RelayRequest{
			Endpoint: "/responses", Body: []byte(`{}`),
		})
		done <- result{err: err}
	}()
	relayID, _ := browser.awaitRequest()
	_ = relayID
	_ = browser.ws.Close()
	if outcome := <-done; !errors.Is(outcome.err, llm.ErrRelayUnavailable) {
		t.Fatalf("disconnect before head error = %v, want ErrRelayUnavailable", outcome.err)
	}
}

func TestRelayDisconnectAfterHeadIsLost(t *testing.T) {
	registry := testRegistry()
	browser := dialBrowser(t, testServer(t, registry))
	type result struct {
		resp *http.Response
		err  error
	}
	done := make(chan result, 1)
	go func() {
		resp, err := registry.For(testPlayerID.String()).Do(context.Background(), llm.RelayRequest{
			Endpoint: "/responses", Body: []byte(`{}`),
		})
		done <- result{resp: resp, err: err}
	}()
	relayID, _ := browser.awaitRequest()
	browser.respondHead(relayID, 200, "text/event-stream")
	// 等服务端把响应交出去再断开：轮询 Body 可读性没有好办法，
	// 直接等一小段时间（head 已发出，Do 必已返回）。
	time.Sleep(200 * time.Millisecond)
	_ = browser.ws.Close()
	outcome := <-done
	if outcome.err != nil {
		t.Fatalf("Do error = %v, want response", outcome.err)
	}
	_, err := io.ReadAll(outcome.resp.Body)
	if !errors.Is(err, llm.ErrRelayLost) {
		t.Fatalf("body read error = %v, want ErrRelayLost", err)
	}
	_ = outcome.resp.Body.Close()
}

func TestRelayCancelNotifiesBrowser(t *testing.T) {
	registry := testRegistry()
	browser := dialBrowser(t, testServer(t, registry))

	// 情况一：等待 head 期间取消，Do 返回 ctx 错误，并尽力通知浏览器。
	ctx, cancel := context.WithCancel(context.Background())
	type result struct {
		err error
	}
	done := make(chan result, 1)
	go func() {
		_, err := registry.For(testPlayerID.String()).Do(ctx, llm.RelayRequest{
			Endpoint: "/responses", Body: []byte(`{}`),
		})
		done <- result{err: err}
	}()
	relayID, _ := browser.awaitRequest()
	cancel()
	if outcome := <-done; !errors.Is(outcome.err, context.Canceled) {
		t.Fatalf("cancelled Do error = %v, want context.Canceled", outcome.err)
	}
	frame := browser.readFrame()
	if frame["type"] != msgRelayCancel || frame["relay_id"] != relayID {
		t.Fatalf("cancel frame = %v", frame)
	}

	// 情况二：响应交付后解析器提前关闭 Body，同样通知浏览器取消。
	done2 := make(chan result, 1)
	var respBody io.ReadCloser
	go func() {
		resp, err := registry.For(testPlayerID.String()).Do(context.Background(), llm.RelayRequest{
			Endpoint: "/responses", Body: []byte(`{}`),
		})
		if err == nil {
			respBody = resp.Body
		}
		done2 <- result{err: err}
	}()
	relayID2, _ := browser.awaitRequest()
	browser.respondHead(relayID2, 200, "text/event-stream")
	if outcome := <-done2; outcome.err != nil {
		t.Fatalf("Do error = %v, want response", outcome.err)
	}
	_ = respBody.Close()
	frame = browser.readFrame()
	if frame["type"] != msgRelayCancel || frame["relay_id"] != relayID2 {
		t.Fatalf("cancel frame = %v", frame)
	}
}

func TestRelaySeqDisorderTerminates(t *testing.T) {
	registry := testRegistry()
	browser := dialBrowser(t, testServer(t, registry))
	type result struct {
		resp *http.Response
		err  error
	}
	done := make(chan result, 1)
	go func() {
		resp, err := registry.For(testPlayerID.String()).Do(context.Background(), llm.RelayRequest{
			Endpoint: "/responses", Body: []byte(`{}`),
		})
		done <- result{resp: resp, err: err}
	}()
	relayID, _ := browser.awaitRequest()
	browser.respondHead(relayID, 200, "text/event-stream")
	time.Sleep(200 * time.Millisecond)
	// 跳过 seq=1 直接发 seq=2。
	browser.writeFrame(clientChunkFrame{Type: msgRelayChunk, RelayID: relayID, Seq: 2, Data: "x"})
	outcome := <-done
	if outcome.err != nil {
		t.Fatalf("Do error = %v, want response", outcome.err)
	}
	_, err := io.ReadAll(outcome.resp.Body)
	if !errors.Is(err, llm.ErrRelayLost) {
		t.Fatalf("body read error = %v, want ErrRelayLost", err)
	}
	_ = outcome.resp.Body.Close()
}

func TestRelayUnknownIDIsDropped(t *testing.T) {
	registry := testRegistry()
	browser := dialBrowser(t, testServer(t, registry))
	// 未知 relay_id 的帧被丢弃，连接保持可用：随后一次正常往返不受影响。
	browser.writeFrame(clientChunkFrame{Type: msgRelayChunk, RelayID: uuid.NewString(), Seq: 1, Data: "x"})
	type result struct {
		resp *http.Response
		err  error
	}
	done := make(chan result, 1)
	go func() {
		resp, err := registry.For(testPlayerID.String()).Do(context.Background(), llm.RelayRequest{
			Endpoint: "/responses", Body: []byte(`{}`),
		})
		done <- result{resp: resp, err: err}
	}()
	relayID, _ := browser.awaitRequest()
	browser.respondHead(relayID, 200, "text/plain")
	browser.respondEnd(relayID, 0)
	outcome := <-done
	if outcome.err != nil {
		t.Fatal(outcome.err)
	}
	if content := readAll(t, outcome.resp); content != "" {
		t.Fatalf("response body = %q, want empty", content)
	}
}

func TestRelayClientErrorMapping(t *testing.T) {
	for _, test := range []struct {
		code string
		want error
	}{
		{code: "network", want: llm.ErrRelayNetwork},
		{code: "no_key", want: llm.ErrRelayNoKey},
		{code: "aborted", want: llm.ErrRelayLost},
	} {
		t.Run(test.code, func(t *testing.T) {
			registry := testRegistry()
			browser := dialBrowser(t, testServer(t, registry))
			type result struct {
				err error
			}
			done := make(chan result, 1)
			go func() {
				_, err := registry.For(testPlayerID.String()).Do(context.Background(), llm.RelayRequest{
					Endpoint: "/responses", Body: []byte(`{}`),
				})
				done <- result{err: err}
			}()
			relayID, _ := browser.awaitRequest()
			browser.writeFrame(clientErrorFrame{Type: msgRelayError, RelayID: relayID, Code: test.code})
			if outcome := <-done; !errors.Is(outcome.err, test.want) {
				t.Fatalf("error = %v, want %v", outcome.err, test.want)
			}
		})
	}
}

func TestRelayRejectsOversizeFrame(t *testing.T) {
	registry := testRegistry()
	browser := dialBrowser(t, testServer(t, registry))
	type result struct {
		err error
	}
	done := make(chan result, 1)
	go func() {
		_, err := registry.For(testPlayerID.String()).Do(context.Background(), llm.RelayRequest{
			Endpoint: "/responses", Body: []byte(`{}`),
		})
		done <- result{err: err}
	}()
	relayID, _ := browser.awaitRequest()
	// 256KiB+1 的单帧触发服务端读限制，连接断开，head 前请求可重试。
	huge := strings.Repeat("x", relayMaxFrameBytes+1)
	browser.writeFrame(clientChunkFrame{Type: msgRelayChunk, RelayID: relayID, Seq: 1, Data: huge})
	if outcome := <-done; !errors.Is(outcome.err, llm.ErrRelayUnavailable) {
		t.Fatalf("oversize frame error = %v, want ErrRelayUnavailable", outcome.err)
	}
}

func TestRelayRejectsOversizeBody(t *testing.T) {
	registry := testRegistry()
	browser := dialBrowser(t, testServer(t, registry))
	huge := make([]byte, relayMaxBodyBytes+1)
	_, err := registry.For(testPlayerID.String()).Do(context.Background(), llm.RelayRequest{
		Endpoint: "/responses", Body: huge,
	})
	if err == nil || errors.Is(err, llm.ErrRelayUnavailable) {
		t.Fatalf("oversize body error = %v, want immediate non-retryable failure", err)
	}
	// 超限请求不发送：浏览器在 200ms 内收不到任何帧。
	_ = browser.ws.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	if _, _, err := browser.ws.ReadMessage(); err == nil {
		t.Fatal("oversize body was sent to browser")
	}
}

func TestRelayLatestConnWinsAndFailsOver(t *testing.T) {
	registry := testRegistry()
	url := testServer(t, registry)
	first := dialBrowser(t, url)
	second := dialBrowser(t, url)
	time.Sleep(200 * time.Millisecond)

	// 最新连接（second）收到请求。
	type result struct {
		resp *http.Response
		err  error
	}
	done := make(chan result, 1)
	go func() {
		resp, err := registry.For(testPlayerID.String()).Do(context.Background(), llm.RelayRequest{
			Endpoint: "/responses", Body: []byte(`{}`),
		})
		done <- result{resp: resp, err: err}
	}()
	second.ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, raw, err := second.ws.ReadMessage()
	if err != nil {
		t.Fatalf("latest conn did not receive request: %v", err)
	}
	var frame map[string]any
	if err := json.Unmarshal(raw, &frame); err != nil || frame["type"] != msgRelayRequest {
		t.Fatalf("latest frame = %s", raw)
	}
	relayID, _ := frame["relay_id"].(string)
	second.respondHead(relayID, 200, "text/plain")
	second.respondEnd(relayID, 0)
	outcome := <-done
	if outcome.err != nil {
		t.Fatal(outcome.err)
	}
	_ = readAll(t, outcome.resp)

	// 关闭最新连接后，下一次请求由旧连接接管。
	_ = second.ws.Close()
	time.Sleep(300 * time.Millisecond)
	done2 := make(chan result, 1)
	go func() {
		resp, err := registry.For(testPlayerID.String()).Do(context.Background(), llm.RelayRequest{
			Endpoint: "/responses", Body: []byte(`{}`),
		})
		done2 <- result{resp: resp, err: err}
	}()
	relayID2, _ := first.awaitRequest()
	first.respondHead(relayID2, 200, "text/plain")
	first.respondEnd(relayID2, 0)
	outcome2 := <-done2
	if outcome2.err != nil {
		t.Fatalf("failover Do error = %v", outcome2.err)
	}
	_ = readAll(t, outcome2.resp)
}

func TestRelayConcurrencyLimit(t *testing.T) {
	registry := testRegistry()
	// 本用例不依赖 head 超时：调大到 5s，避免停放的请求在断言窗口内超时释放槽位。
	registry.headTimeout = 5 * time.Second
	serverURL := testServer(t, registry)
	browser := dialBrowser(t, serverURL)
	launch := func(done chan<- struct{}) {
		go func() {
			resp, err := registry.For(testPlayerID.String()).Do(context.Background(), llm.RelayRequest{
				Endpoint: "/responses", Body: []byte(`{}`),
			})
			if err == nil {
				_, _ = io.ReadAll(resp.Body)
				_ = resp.Body.Close()
			}
			done <- struct{}{}
		}()
	}
	readRequest := func() string {
		t.Helper()
		_ = browser.ws.SetReadDeadline(time.Now().Add(3 * time.Second))
		_, raw, err := browser.ws.ReadMessage()
		if err != nil {
			t.Fatalf("browser read: %v", err)
		}
		var frame map[string]any
		if err := json.Unmarshal(raw, &frame); err != nil || frame["type"] != msgRelayRequest {
			t.Fatalf("frame = %s", raw)
		}
		id, _ := frame["relay_id"].(string)
		return id
	}
	// 先占满 4 个槽：4 个请求确定性派发（只做成功读，从不做失败读，
	// gorilla 失败读会粘住连接，后续读全部立即失败）。
	done := make(chan struct{}, 5)
	var first string
	for i := 0; i < 4; i++ {
		launch(done)
	}
	for i := 0; i < 4; i++ {
		id := readRequest()
		if i == 0 {
			first = id
		}
	}
	// 再起第 5 个：4 槽被占，它只能排队。300ms 内 5 个 Do 都不得返回
	//（前 4 个在等 head——500ms 超时还没到；第 5 个在等槽）。
	launch(done)
	time.Sleep(300 * time.Millisecond)
	for i := 0; i < 5; i++ {
		select {
		case <-done:
			t.Fatal("a request finished before any slot was released")
		default:
		}
	}
	// 缺席断言：B 上 300ms 内不得再有帧。注意这次超时读会粘住 B 的后续读
	//（gorilla 失败读不可恢复），因此之后只在 B 上写，读转到新连接 C。
	// 若限流失效、第 5 个已被提前派发，这次读会立刻拿到它的帧并失败。
	_ = browser.ws.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	if _, _, err := browser.ws.ReadMessage(); err == nil {
		t.Fatal("5th request arrived before a slot was released")
	}
	// 拨新连接 C 成为最新，并等待服务端注册完成（dial 返回只代表客户端
	// 握手结束，服务端 accept 仍在途中；不等它，第 5 个请求会发到旧连接 B 上，
	// 而 B 的读在缺席断言后已不可用）。
	fresh := dialBrowser(t, serverURL)
	deadline := time.Now().Add(3 * time.Second)
	for {
		registry.mu.Lock()
		registered := len(registry.conns[testPlayerID])
		registry.mu.Unlock()
		if registered >= 2 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	registry.mu.Lock()
	registered := len(registry.conns[testPlayerID])
	registry.mu.Unlock()
	if registered < 2 {
		t.Fatalf("server registered %d conns, want 2", registered)
	}
	t.Logf("conns before end-first: B-alive check")
	browser.respondHead(first, 200, "text/plain")
	browser.respondEnd(first, 0)
	_ = fresh.ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, raw, err := fresh.ws.ReadMessage()
	if err != nil {
		t.Fatalf("5th request was not dispatched after release: %v", err)
	}
	var frame map[string]any
	if err := json.Unmarshal(raw, &frame); err != nil || frame["type"] != msgRelayRequest {
		t.Fatalf("frame = %s", raw)
	}
	if id, _ := frame["relay_id"].(string); id == first {
		t.Fatal("5th frame reused a finished relay id")
	}
	// 收尾：结束第 5 个请求，避免测试内 goroutine 泄漏。
	fifth, _ := frame["relay_id"].(string)
	fresh.writeFrame(clientHeadFrame{Type: msgRelayHead, RelayID: fifth, Status: 200})
	fresh.writeFrame(clientEndFrame{Type: msgRelayEnd, RelayID: fifth, Seq: 0})
	completed := 0
	timeout := time.After(5 * time.Second)
	for completed < 2 {
		select {
		case <-done:
			completed++
		case <-timeout:
			t.Fatalf("only %d requests completed, want 2", completed)
		}
	}
}

func TestRelayCancelAfterHeadInterruptsRead(t *testing.T) {
	registry := testRegistry()
	browser := dialBrowser(t, testServer(t, registry))
	ctx, cancel := context.WithCancel(context.Background())
	type result struct {
		resp *http.Response
		err  error
	}
	done := make(chan result, 1)
	go func() {
		resp, err := registry.For(testPlayerID.String()).Do(ctx, llm.RelayRequest{
			Endpoint: "/responses", Body: []byte(`{}`),
		})
		done <- result{resp: resp, err: err}
	}()
	relayID, _ := browser.awaitRequest()
	browser.respondHead(relayID, 200, "text/event-stream")
	outcome := <-done
	if outcome.err != nil {
		t.Fatalf("Do error = %v, want response", outcome.err)
	}
	// head 之后取消：Read 必须立即返回，不等上游自然结束或空闲看门狗。
	cancel()
	readDone := make(chan error, 1)
	go func() {
		_, err := io.ReadAll(outcome.resp.Body)
		readDone <- err
	}()
	select {
	case err := <-readDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("body read error = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("body read was not interrupted by cancel")
	}
	_ = outcome.resp.Body.Close()
	// 浏览器必须收到 relay.cancel，不再继续消耗 token。
	frame := browser.readFrame()
	if frame["type"] != msgRelayCancel || frame["relay_id"] != relayID {
		t.Fatalf("cancel frame = %v", frame)
	}
	// 并发槽必须已释放：下一次请求可正常派发。
	done2 := make(chan result, 1)
	go func() {
		resp, err := registry.For(testPlayerID.String()).Do(context.Background(), llm.RelayRequest{
			Endpoint: "/responses", Body: []byte(`{}`),
		})
		done2 <- result{resp: resp, err: err}
	}()
	relayID2, _ := browser.awaitRequest()
	browser.respondHead(relayID2, 200, "text/plain")
	browser.respondEnd(relayID2, 0)
	outcome2 := <-done2
	if outcome2.err != nil {
		t.Fatalf("second Do error = %v, slot was not released", outcome2.err)
	}
	_ = readAll(t, outcome2.resp)
}

// newUnitPending 构造不依赖网络的 pending 请求：只测投递与终结的同步，
// 不启动 runWriter、不触碰 websocket。
func newUnitPending() *pendingRequest {
	conn := &relayConn{pending: make(map[uuid.UUID]*pendingRequest), closed: make(chan struct{})}
	p := newPendingRequest(conn, nil)
	conn.pending[p.relayID] = p
	p.deliverHead(clientHeadFrame{Type: msgRelayHead, RelayID: p.relayID.String(), Status: 200})
	return p
}

func TestRelayChunkAfterFinishDoesNotPanic(t *testing.T) {
	p := newUnitPending()
	p.finish(llm.ErrRelayLost)
	// 终结后再投递必须静默丢弃，不得向已关闭队列发送 panic（-race 下跑）。
	p.deliverChunk(1, "data")
	p.deliverEnd(0)
}

func TestRelayFullQueueTerminatesRequest(t *testing.T) {
	p := newUnitPending()
	// 不启动 runWriter，队列会被填满：第 chunkQueueCap+1 次投递必须终结请求，
	// 而不是阻塞 readPump。
	for i := 1; i <= chunkQueueCap+1; i++ {
		p.deliverChunk(i, "x")
	}
	if !p.isFinished() {
		t.Fatal("request with full chunk queue was not terminated")
	}
}

func TestRelayChunkFinishRace(t *testing.T) {
	p := newUnitPending()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 1; i <= 200; i++ {
			p.deliverChunk(i, "x")
		}
	}()
	go func() {
		defer wg.Done()
		p.finish(llm.ErrRelayLost)
	}()
	wg.Wait()
}
