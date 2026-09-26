package llmrelay

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/ialtone/fatecraft/internal/llm"
	"github.com/ialtone/fatecraft/internal/logging"
)

// chunkQueueCap 是请求级 chunk 队列长度（每帧 ≤ 64KiB，约 4MiB 缓冲），
// readPump 投递永不阻塞；writer 协程顺序写入 pipe。
const chunkQueueCap = 64

// headResult 是等待响应头的结果。
type headResult struct {
	status      int
	contentType string
	retryAfter  string
	err         error
}

// pendingRequest 是一次在途 Relay 请求的服务端状态。
type pendingRequest struct {
	relayID uuid.UUID
	conn    *relayConn
	release func()

	headCh chan headResult

	pipeReader *io.PipeReader
	pipeWriter *io.PipeWriter
	chunkQueue chan []byte
	bodyClosed atomic.Bool
	// queueMu 串行化 chunk 投递与队列关闭：关闭只在 queueMu 内发生，
	// 投递先在 queueMu 内检查 queueClosed 再发送，避免向已关闭队列发送 panic。
	// 发送为非阻塞：队列满时直接终结请求，不阻塞连接级 readPump（§9.9）。
	queueMu     sync.Mutex
	queueClosed bool

	done     chan struct{}
	finishMu sync.Mutex
	finished bool
	headSeen bool
	// terminalErr 是终结原因，由 finish 写入、writer 在排空后读取关闭 pipe。
	terminalErr error

	expectedSeq int
	chunkCount  int
}

func newPendingRequest(conn *relayConn, release func()) *pendingRequest {
	pr, pw := io.Pipe()
	return &pendingRequest{
		relayID:     uuid.New(),
		conn:        conn,
		release:     release,
		headCh:      make(chan headResult, 1),
		pipeReader:  pr,
		pipeWriter:  pw,
		chunkQueue:  make(chan []byte, chunkQueueCap),
		done:        make(chan struct{}),
		expectedSeq: 1,
	}
}

// runWriter 顺序把队列中的 chunk 写入 pipe；队列关闭且排空后按终结原因
// 关闭 pipe（writer 拥有 pipe 关闭权，避免 finish 提前关管丢数）。
// reader 提前放弃或 ctx 取消时由 pipeWriter.CloseWithError 叫醒 writer。
func (p *pendingRequest) runWriter() {
	for data := range p.chunkQueue {
		if p.bodyClosed.Load() {
			continue
		}
		if _, err := p.pipeWriter.Write(data); err != nil {
			break
		}
	}
	p.finishMu.Lock()
	termErr := p.terminalErr
	p.finishMu.Unlock()
	if termErr == nil {
		_ = p.pipeWriter.Close()
	} else {
		_ = p.pipeWriter.CloseWithError(termErr)
	}
}

// finish 终结请求（幂等）：从连接摘除、关闭队列、释放并发槽。
// pipe 由 writer 在排空队列后按 terminalErr 关闭。
func (p *pendingRequest) finish(err error) {
	p.finishMu.Lock()
	if p.finished {
		p.finishMu.Unlock()
		return
	}
	p.finished = true
	p.terminalErr = err
	p.finishMu.Unlock()

	close(p.done)
	p.conn.removePending(p.relayID)
	p.queueMu.Lock()
	if !p.queueClosed {
		p.queueClosed = true
		close(p.chunkQueue)
	}
	p.queueMu.Unlock()
	if p.release != nil {
		p.release()
	}
}

func (p *pendingRequest) headDelivered() bool {
	p.finishMu.Lock()
	defer p.finishMu.Unlock()
	return p.headSeen
}

func (p *pendingRequest) isFinished() bool {
	p.finishMu.Lock()
	defer p.finishMu.Unlock()
	return p.finished
}

// deliverHead 投递响应头；状态码非法或重复 head 以 ErrRelayLost 终止请求。
func (p *pendingRequest) deliverHead(frame clientHeadFrame) {
	p.finishMu.Lock()
	if p.finished {
		p.finishMu.Unlock()
		return
	}
	if p.headSeen {
		p.finishMu.Unlock()
		p.finish(llm.ErrRelayLost)
		logging.Named("llmrelay").Warn("Relay 收到重复响应头",
			zap.String("relay_id", p.relayID.String()))
		return
	}
	if frame.Status < 100 || frame.Status > 599 {
		p.finishMu.Unlock()
		p.finish(llm.ErrRelayLost)
		logging.Named("llmrelay").Warn("Relay 响应头状态码非法",
			zap.String("relay_id", p.relayID.String()), zap.Int("status", frame.Status))
		return
	}
	p.headSeen = true
	p.finishMu.Unlock()
	select {
	case p.headCh <- headResult{status: frame.Status, contentType: frame.ContentType, retryAfter: frame.RetryAfter}:
	default:
		p.finish(llm.ErrRelayLost)
	}
}

// deliverChunk 投递 chunk；seq 必须从 1 连续递增，head 之前到达视为乱序，
// 否则以 ErrRelayLost 终止请求。
func (p *pendingRequest) deliverChunk(seq int, data string) {
	if p.isFinished() {
		return
	}
	if !p.headDelivered() || seq != p.expectedSeq {
		if !p.headDelivered() {
			select {
			case p.headCh <- headResult{err: llm.ErrRelayLost}:
			default:
			}
		}
		p.finish(llm.ErrRelayLost)
		logging.Named("llmrelay").Warn("Relay chunk 乱序",
			zap.String("relay_id", p.relayID.String()),
			zap.Int("want_seq", p.expectedSeq), zap.Int("got_seq", seq))
		return
	}
	p.expectedSeq++
	p.chunkCount++
	p.queueMu.Lock()
	if p.queueClosed {
		// 终结与投递在此互斥：队列已关闭时直接丢弃，绝不向关闭的 channel 发送。
		p.queueMu.Unlock()
		return
	}
	select {
	case p.chunkQueue <- []byte(data):
		p.queueMu.Unlock()
	default:
		// 队列满时不阻塞 readPump：终结本请求，其余多路复用请求不受影响。
		p.queueMu.Unlock()
		p.finish(llm.ErrRelayLost)
		logging.Named("llmrelay").Warn("Relay chunk 队列已满，终止请求",
			zap.String("relay_id", p.relayID.String()))
	}
}

// deliverEnd 投递正常结束；seq 必须等于已收 chunk 总数，head 之前到达视为乱序。
func (p *pendingRequest) deliverEnd(seq int) {
	if p.isFinished() {
		return
	}
	if !p.headDelivered() {
		select {
		case p.headCh <- headResult{err: llm.ErrRelayLost}:
		default:
		}
		p.finish(llm.ErrRelayLost)
		logging.Named("llmrelay").Warn("Relay 在响应头之前结束",
			zap.String("relay_id", p.relayID.String()))
		return
	}
	if seq != p.chunkCount {
		p.finish(llm.ErrRelayLost)
		logging.Named("llmrelay").Warn("Relay 结束序号不匹配",
			zap.String("relay_id", p.relayID.String()),
			zap.Int("chunks", p.chunkCount), zap.Int("end_seq", seq))
		return
	}
	p.finish(nil)
}

// deliverError 投递客户端失败：head 前按 code 映射，head 后一律 ErrRelayLost。
func (p *pendingRequest) deliverError(code string) {
	if p.isFinished() {
		return
	}
	if !p.headDelivered() {
		err := preHeadError(code)
		select {
		case p.headCh <- headResult{err: err}:
		default:
		}
		return
	}
	p.finish(llm.ErrRelayLost)
}

// requestCancel 尽力通知浏览器 abort 本次 fetch。
func (p *pendingRequest) requestCancel() {
	p.conn.requestCancel(p.relayID)
}

// relayBody 是返回给 llm 解析器的响应体：关闭时若请求尚未终结，
// 说明解析器提前放弃，通知浏览器取消并释放资源。
type relayBody struct {
	reader  *io.PipeReader
	pending *pendingRequest
}

func (b *relayBody) Read(data []byte) (int, error) { return b.reader.Read(data) }

func (b *relayBody) Close() error {
	b.pending.bodyClosed.Store(true)
	select {
	case <-b.pending.done:
	default:
		b.pending.requestCancel()
		b.pending.finish(llm.ErrRelayLost)
		// 叫醒可能阻塞在 Write 的 writer（它退出后会再关一次 pipe，无害）。
		_ = b.pending.pipeWriter.CloseWithError(llm.ErrRelayLost)
	}
	return b.reader.Close()
}

// Do 执行一次 HTTP 调用并返回原始响应；Body 为原始字节流。
func (t *relayTransport) Do(ctx context.Context, req llm.RelayRequest) (*http.Response, error) {
	if !llm.RelayAllowedEndpoints[req.Endpoint] {
		return nil, fmt.Errorf("relay endpoint 不在白名单: %s", req.Endpoint)
	}
	if len(req.Body) > relayMaxBodyBytes {
		return nil, fmt.Errorf("relay 请求体超过 %d 字节上限", relayMaxBodyBytes)
	}
	conn := t.registry.latest(t.playerID)
	if conn == nil {
		return nil, llm.ErrRelayUnavailable
	}
	// 每玩家并发上限，排队等待（受 ctx 约束）。注意连接不在这里固化：
	// 排队期间新标签页可能接管，派发时必须重新取最新连接。
	sem := t.registry.semaphore(t.playerID)
	select {
	case sem <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	release := func() { <-sem }
	conn = t.registry.latest(t.playerID)
	if conn == nil {
		release()
		return nil, llm.ErrRelayUnavailable
	}

	pending := newPendingRequest(conn, release)
	conn.addPending(pending)
	go pending.runWriter()

	conversationID := ""
	if req.ConversationID != uuid.Nil {
		conversationID = req.ConversationID.String()
	}
	if err := conn.sendJSON(serverRequestFrame{
		Type: msgRelayRequest, RelayID: pending.relayID.String(),
		Endpoint: req.Endpoint, Stream: req.Stream, Purpose: req.Purpose,
		ConversationID: conversationID, Body: string(req.Body),
	}); err != nil {
		// 发送失败：响应头尚未到达，按可重试处理。
		pending.finish(llm.ErrRelayUnavailable)
		return nil, llm.ErrRelayUnavailable
	}

	headTimeout := t.registry.headTimeout
	if headTimeout <= 0 {
		headTimeout = relayHeadTimeout
	}
	timer := time.NewTimer(headTimeout)
	defer timer.Stop()
	select {
	case head := <-pending.headCh:
		if head.err != nil {
			pending.finish(head.err)
			return nil, head.err
		}
		header := http.Header{}
		if head.contentType != "" {
			header.Set("Content-Type", head.contentType)
		}
		if head.retryAfter != "" {
			header.Set("Retry-After", head.retryAfter)
		}
		resp := &http.Response{
			Status:        fmt.Sprintf("%d %s", head.status, http.StatusText(head.status)),
			StatusCode:    head.status,
			Proto:         "HTTP/1.1",
			ProtoMajor:    1,
			ProtoMinor:    1,
			Header:        header,
			Body:          &relayBody{reader: pending.pipeReader, pending: pending},
			ContentLength: -1,
		}
		// 响应头到达后仍需监听取消：用户取消、租约心跳失败、attempt 超时都走 ctx。
		// 否则解析器阻塞在 pipe 读取上、浏览器也收不到 relay.cancel，会继续消耗 token。
		go func() {
			select {
			case <-ctx.Done():
				pending.requestCancel()
				pending.finish(ctx.Err())
				// 立刻叫醒阻塞在 Read 的解析器（writer 退出后会再关一次 pipe，无害）。
				_ = pending.pipeWriter.CloseWithError(ctx.Err())
			case <-pending.done:
			}
		}()
		return resp, nil
	case <-timer.C:
		pending.requestCancel()
		pending.finish(llm.ErrRelayUnavailable)
		return nil, llm.ErrRelayUnavailable
	case <-ctx.Done():
		pending.requestCancel()
		pending.finish(ctx.Err())
		return nil, ctx.Err()
	case <-conn.closed:
		pending.finish(llm.ErrRelayUnavailable)
		return nil, llm.ErrRelayUnavailable
	}
}
