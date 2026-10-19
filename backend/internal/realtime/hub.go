package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"github.com/ialtone/fatecraft/internal/logging"
)

// 读超时：writePump 每 30s ping 一次，客户端 pong 会刷新读期限；
// 超过 60s 无任何帧（含 pong）视为死连接，及时释放而非等 TCP 超时。
const readWait = 60 * time.Second
const writeWait = 10 * time.Second

const (
	// wsSendQueueCapacity is a bounded burst buffer. It is deliberately not
	// treated as the reliability mechanism: delta events are coalesced before
	// they get here and control events wait briefly when the queue is full.
	wsSendQueueCapacity   = 256
	wsDeltaMaxBytes       = 4 * 1024
	wsMaxClientFrameBytes = 64 * 1024
	maxClientContentBytes = 32 * 1024
	// A client can explicitly queue follow-up/steer requests, but an
	// unbounded queue would turn a disconnected or abusive client into a
	// memory growth vector.
	maxGenerationQueueLength = 64
)

var wsDeltaFlushInterval = 40 * time.Millisecond

// Generator 是 Hub 触发生成所需的编排入口，由 orchestration 包的 Generator 实现。
// 以消费端接口声明（事件类型定义在本包），避免 realtime 与编排包形成环依赖。
type Generator interface {
	RequestCancel(ctx context.Context, convID uuid.UUID, reason string) error
	// Generate 处理一次用户输入：保存用户消息并生成 AI 回复。
	Generate(ctx context.Context, convID, requestID uuid.UUID, userContent string, emit EmitFunc)
	// Regenerate 删除最近一条 AI 回复后重新生成。
	Regenerate(ctx context.Context, convID, requestID uuid.UUID, emit EmitFunc)
	// Continue 在不新增用户输入的情况下追加一段 AI 续写。
	Continue(ctx context.Context, convID, requestID uuid.UUID, emit EmitFunc)
	// Rewrite atomically edits one user message, replaces its branch and regenerates.
	Rewrite(ctx context.Context, convID, requestID, messageID uuid.UUID, content string, emit EmitFunc)
	// RecoverPanic 在生成 goroutine panic 后释放该对话残留的持久化生成租约。
	RecoverPanic(ctx context.Context, convID uuid.UUID, recovered any)
}

// Hub 管理 WebSocket 连接，按 conversation_id 分组。
// 一个连接只绑定一个对话；同一对话同一时间只允许一个生成任务。
type Hub struct {
	gen      Generator
	upgrader websocket.Upgrader

	mu                  sync.Mutex
	closed              bool
	activityUnavailable bool
	conns               map[uuid.UUID]map[*conn]struct{} // 每个对话的连接集合
	cancels             map[uuid.UUID]*genTask           // 每个对话当前生成任务
	queued              map[uuid.UUID][]queuedGeneration // 显式 steer/follow-up 队列
}

// genTask 包装一次生成任务的取消函数；用指针身份区分新旧任务，
// 避免旧任务结束时误删新任务已注册的 cancel。
// done 在任务 goroutine 退出时关闭，供后继任务等待旧任务释放对话锁。
type genTask struct {
	cancel   context.CancelFunc
	done     chan struct{}
	finished bool // protected by Hub.mu; queueGeneration checks this flag
}

type queuedGeneration struct {
	ctx context.Context
	run func(context.Context, EmitFunc)
}

// NewHub 创建 Hub。allowedOrigins 为允许的 WebSocket 来源列表；
// 为空时放开所有来源（仅供开发使用，生产必须配置 ALLOWED_ORIGINS）。
func NewHub(gen Generator, allowedOrigins []string) *Hub {
	return &Hub{
		gen:     gen,
		conns:   map[uuid.UUID]map[*conn]struct{}{},
		cancels: map[uuid.UUID]*genTask{},
		queued:  map[uuid.UUID][]queuedGeneration{},
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				if len(allowedOrigins) == 0 {
					// 未配置来源白名单：全放开，仅开发用。
					return true
				}
				origin := r.Header.Get("Origin")
				if origin == "" {
					return true // 非浏览器客户端
				}
				for _, o := range allowedOrigins {
					if o == "*" || o == origin {
						return true
					}
				}
				return false
			},
		},
	}
}

// conn 是单个 WebSocket 连接，带独立的发送队列以保证单写者。
// mu 同时保护生命周期和增量合并器，避免 unregister 与 Broadcast 之间
// 向已关闭 channel 写入；done 用于让所有等待中的发送快速感知连接关闭。
type conn struct {
	ws     *websocket.Conn
	send   chan []byte
	done   chan struct{}
	convID uuid.UUID

	mu     sync.Mutex
	closed bool
	// pending 只保存当前连接上最后一个、尚未 flush 的同键 delta。
	pending *pendingDelta
}

type deltaKey struct {
	typeName       string
	conversationID string
	turnID         string
	attemptID      string
	stepNo         int
	messageID      string
	preview        bool
}

type pendingDelta struct {
	key           deltaKey
	event         Event
	delta         strings.Builder
	bytes         int
	sequenceStart int64
	timer         *time.Timer
}

func newConn(ws *websocket.Conn, convID uuid.UUID) *conn {
	return &conn{
		ws:     ws,
		send:   make(chan []byte, wsSendQueueCapacity),
		done:   make(chan struct{}),
		convID: convID,
	}
}

// Serve 升级 HTTP 连接为 WebSocket 并进入读写循环。
func (h *Hub) Serve(w http.ResponseWriter, r *http.Request, convID uuid.UUID) error {
	ws, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		logging.Named("ws").Warn("WebSocket 升级失败",
			zap.String("conversation_id", convID.String()), zap.Error(err))
		return err
	}
	logging.Named("ws").Debug("WebSocket 连接建立", zap.String("conversation_id", convID.String()))
	c := newConn(ws, convID)
	if !h.register(convID, c) {
		_ = ws.Close()
		return errHubClosed
	}
	defer h.unregister(convID, c)
	c.enqueue(Event{Type: TypeSubscriptionReady, ConversationID: convID.String()})

	go h.writePump(c)
	// WebSocket request context 关闭后连接仍可继续生成，保留其中的认证身份值。
	h.readPump(context.WithoutCancel(r.Context()), convID, c)
	return nil
}

func (h *Hub) register(convID uuid.UUID, c *conn) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || h.activityUnavailable {
		return false
	}
	if h.conns[convID] == nil {
		h.conns[convID] = map[*conn]struct{}{}
	}
	h.conns[convID][c] = struct{}{}
	return true
}

func (h *Hub) unregister(convID uuid.UUID, c *conn) {
	h.mu.Lock()
	if set := h.conns[convID]; set != nil {
		delete(set, c)
		if len(set) == 0 {
			delete(h.conns, convID)
		}
	}
	h.mu.Unlock()
	// Do this outside the Hub lock. A pending delta is best-effort flushed, and
	// the connection-level close is idempotent with writePump/readPump exits.
	c.close()
}

// Broadcast 向某对话的所有连接推送事件。
func (h *Hub) Broadcast(convID uuid.UUID, ev Event) {
	h.mu.Lock()
	targets := make([]*conn, 0, len(h.conns[convID]))
	for c := range h.conns[convID] {
		targets = append(targets, c)
	}
	h.mu.Unlock()

	// Never wait for a slow connection while holding the global Hub lock. Each
	// connection applies its own coalescing/backpressure policy independently.
	for _, c := range targets {
		c.enqueue(ev)
	}
}

// SetActivityAvailable invalidates all subscriptions on a database-notification
// gap. Reconnecting clients must take fresh REST snapshots after ready.
func (h *Hub) SetActivityAvailable(available bool) {
	h.mu.Lock()
	h.activityUnavailable = !available
	var targets []*conn
	if !available {
		for _, set := range h.conns {
			for c := range set {
				targets = append(targets, c)
			}
		}
	}
	h.mu.Unlock()
	for _, c := range targets {
		c.close()
	}
}

func (h *Hub) HasSubscribers(id uuid.UUID) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.conns[id]) > 0
}

func (h *Hub) InvalidateConversation(id uuid.UUID) {
	h.mu.Lock()
	var targets []*conn
	for c := range h.conns[id] {
		targets = append(targets, c)
	}
	h.mu.Unlock()
	for _, c := range targets {
		c.close()
	}
}

// enqueue sends one event through the connection-local delta coalescer. Only
// adjacent deltas with the same public identity can be merged; any control
// event is a hard ordering boundary and uses reliable bounded waiting.
func (c *conn) enqueue(ev Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}

	if delta, key, ok := deltaInfo(ev); ok {
		if c.pending != nil && c.pending.key == key {
			c.pending.delta.WriteString(delta)
			c.pending.bytes += len(delta)
			c.pending.event.Sequence = ev.Sequence
			c.pending.event.OccurredAt = ev.OccurredAt
			if c.pending.sequenceStart == 0 {
				c.pending.sequenceStart = ev.Sequence
			}
			if c.pending.bytes >= wsDeltaMaxBytes {
				c.flushPendingLocked()
			}
			return
		}
		c.flushPendingLocked()
		p := &pendingDelta{key: key, event: ev, bytes: len(delta), sequenceStart: ev.Sequence}
		p.delta.WriteString(delta)
		c.pending = p
		p.timer = time.AfterFunc(wsDeltaFlushInterval, func() {
			c.mu.Lock()
			defer c.mu.Unlock()
			if !c.closed && c.pending == p {
				c.flushPendingLocked()
			}
		})
		return
	}
	if isDeltaType(ev.Type) {
		// Malformed/empty delta frames are still temporary previews. They cannot
		// be coalesced safely, but must not gain the stronger control-event
		// delivery semantics merely because their payload is incomplete.
		c.flushPendingLocked()
		c.enqueueFrameLocked(ev, false)
		return
	}

	// A control/terminal/fact event flushes any preceding preview first, so the
	// observable order remains identical to the order emitted by orchestration.
	c.flushPendingLocked()
	c.enqueueFrameLocked(ev, true)
}

func (c *conn) flushPendingLocked() {
	p := c.pending
	if p == nil {
		return
	}
	c.pending = nil
	if p.timer != nil {
		p.timer.Stop()
	}

	merged := p.event
	merged.EventID = uuid.NewString()
	payload := clonePayloadMap(merged.Payload)
	payload["delta"] = p.delta.String()
	if p.key.messageID != "" {
		payload["message_id"] = p.key.messageID
	}
	if p.sequenceStart > 0 && p.sequenceStart != merged.Sequence {
		// sequence is the high-water mark of this coalesced range. The start
		// lets clients distinguish intentional coalescing from a real gap.
		payload["sequence_start"] = p.sequenceStart
	}
	merged.Payload = payload
	if merged.Delta != "" {
		merged.Delta = p.delta.String()
	}
	// Delta previews are explicitly best effort. If the bounded queue is full,
	// discard this temporary frame but keep the connection available for the
	// following reliable control/fact events.
	c.enqueueFrameLocked(merged, false)
}

func (c *conn) enqueueFrameLocked(ev Event, reliable bool) bool {
	data, err := json.Marshal(ev)
	if err != nil {
		logging.Named("ws").Error("WebSocket 事件序列化失败",
			zap.String("conversation_id", c.convID.String()), zap.String("event_type", ev.Type), zap.Error(err))
		if reliable {
			c.closeLocked()
		}
		return false
	}

	if !reliable {
		select {
		case c.send <- data:
			return true
		default:
			logging.Named("ws").Debug("WebSocket 增量队列已满，丢弃临时预览",
				zap.String("conversation_id", c.convID.String()), zap.String("event_type", ev.Type))
			return false
		}
	}

	// Do not wait in the model callback for a slow client. A full queue is a
	// connection health failure, not a reason to hold the conversation's
	// generation lease; close the client so it can reconnect and replay state.
	select {
	case c.send <- data:
		return true
	case <-c.done:
		return false
	default:
		logging.Named("ws").Warn("WebSocket 控制事件队列已满，关闭慢连接",
			zap.String("conversation_id", c.convID.String()), zap.String("event_type", ev.Type),
			zap.Int("queue_capacity", wsSendQueueCapacity))
		c.closeLocked()
		return false
	}
}

func (c *conn) closeLocked() {
	if c.closed {
		return
	}
	if c.pending != nil {
		// close() calls this after a best-effort flush; timeout paths intentionally
		// skip another send because the queue is already known to be saturated.
		if c.pending.timer != nil {
			c.pending.timer.Stop()
		}
		c.pending = nil
	}
	c.closed = true
	close(c.done)
}

func (c *conn) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	// On a normal disconnect, make one best effort to preserve the last preview
	// before signalling the writer to exit. No caller waits for this to succeed.
	c.flushPendingLocked()
	c.closeLocked()
}

func deltaInfo(ev Event) (string, deltaKey, bool) {
	if !isDeltaType(ev.Type) {
		return "", deltaKey{}, false
	}
	payload, ok := payloadMap(ev.Payload)
	if !ok {
		if ev.Delta == "" || ev.MessageID == "" {
			return "", deltaKey{}, false
		}
		return ev.Delta, deltaKey{
			typeName: ev.Type, conversationID: ev.ConversationID, turnID: ev.TurnID,
			attemptID: ev.AttemptID, stepNo: ev.StepNo, messageID: ev.MessageID,
		}, true
	}
	delta, _ := payload["delta"].(string)
	if delta == "" {
		delta = ev.Delta
	}
	messageID, _ := payload["message_id"].(string)
	if messageID == "" {
		messageID = ev.MessageID
	}
	if delta == "" || messageID == "" {
		return "", deltaKey{}, false
	}
	preview, _ := payload["preview"].(bool)
	return delta, deltaKey{
		typeName: ev.Type, conversationID: ev.ConversationID, turnID: ev.TurnID,
		attemptID: ev.AttemptID, stepNo: ev.StepNo, messageID: messageID, preview: preview,
	}, true
}

func isDeltaType(kind string) bool {
	return kind == TypeAgentReasoningDelta || kind == TypeAgentTextDelta
}

func payloadMap(payload any) (map[string]any, bool) {
	switch value := payload.(type) {
	case map[string]any:
		return value, true
	case map[string]string:
		result := make(map[string]any, len(value))
		for k, v := range value {
			result[k] = v
		}
		return result, true
	case nil:
		return nil, false
	}
	// JSONMap and other named map/struct payloads are intentionally supported
	// without coupling realtime to storage. This is only on delta flush paths,
	// so the small conversion cost is preferable to dropping a valid preview.
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, false
	}
	result := make(map[string]any)
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, false
	}
	return result, true
}

func clonePayloadMap(payload any) map[string]any {
	if source, ok := payloadMap(payload); ok {
		result := make(map[string]any, len(source)+1)
		for k, v := range source {
			result[k] = v
		}
		return result
	}
	return make(map[string]any)
}

func (h *Hub) writePump(c *conn) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	defer func() {
		if recovered := recover(); recovered != nil {
			logging.Named("ws").Error("WebSocket 写循环 panic",
				zap.String("conversation_id", c.convID.String()), zap.Any("panic", recovered), zap.String("stack", string(debug.Stack())))
		}
		c.close()
		if c.ws != nil {
			_ = c.ws.SetWriteDeadline(time.Now().Add(writeWait))
		}
		_ = c.ws.Close()
	}()
	for {
		// Prefer shutdown over draining an already queued burst. This keeps a
		// backpressured connection from spending a long time writing stale
		// previews after its control-event timeout has fired.
		select {
		case <-c.done:
			if c.ws != nil {
				_ = c.ws.SetWriteDeadline(time.Now().Add(writeWait))
			}
			_ = c.ws.WriteMessage(websocket.CloseMessage, []byte{})
			return
		default:
		}
		select {
		case <-c.done:
			// writePump is the only writer, so it can make a best-effort close
			// handshake before releasing the socket.
			if c.ws != nil {
				_ = c.ws.SetWriteDeadline(time.Now().Add(writeWait))
			}
			_ = c.ws.WriteMessage(websocket.CloseMessage, []byte{})
			return
		case msg := <-c.send:
			if c.ws != nil {
				_ = c.ws.SetWriteDeadline(time.Now().Add(writeWait))
			}
			if err := c.ws.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			if c.ws != nil {
				_ = c.ws.SetWriteDeadline(time.Now().Add(writeWait))
			}
			if err := c.ws.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (h *Hub) readPump(baseCtx context.Context, convID uuid.UUID, c *conn) {
	defer c.ws.Close()
	// 死连接检测：初始读期限 + pong 刷新（配合 writePump 的周期 ping）。
	_ = c.ws.SetReadDeadline(time.Now().Add(readWait))
	c.ws.SetReadLimit(wsMaxClientFrameBytes)
	c.ws.SetPongHandler(func(string) error {
		return c.ws.SetReadDeadline(time.Now().Add(readWait))
	})
	for {
		_, data, err := c.ws.ReadMessage()
		if err != nil {
			return
		}
		// 收到任何数据帧同样刷新读期限。
		_ = c.ws.SetReadDeadline(time.Now().Add(readWait))
		var msg ClientMessage
		if json.Unmarshal(data, &msg) != nil {
			continue
		}
		commandCtx := logging.NewTrace(baseCtx)
		switch msg.Type {
		case TypeUserInput:
			// 与 REST 入口保持一致：空内容拒绝，避免落一条空用户消息。
			if strings.TrimSpace(msg.Content) == "" {
				c.notice(convID, "消息内容不能为空")
				continue
			}
			if len(msg.Content) > maxClientContentBytes || !utf8.ValidString(msg.Content) {
				c.notice(convID, "消息内容超过大小限制")
				continue
			}
			requestID, validRequestID := parseRequestID(msg.RequestID)
			if !validRequestID {
				c.notice(convID, "request_id 必须是有效 UUID")
				continue
			}
			if !h.startGeneration(commandCtx, convID, func(ctx context.Context, emit EmitFunc) {
				h.gen.Generate(ctx, convID, requestID, msg.Content, emit)
			}) {
				// 忙碌提示只回发给发起请求的连接：其他连接正在接收该对话的
				// 流式输出，广播会让它们误以为生成已经终止。
				c.notice(convID, "当前对话正在生成中，请稍候")
			}
		case TypeCancelGeneration:
			h.cancelGeneration(commandCtx, convID)
		case TypeSteerGeneration, TypeFollowUp:
			if strings.TrimSpace(msg.Content) == "" {
				c.notice(convID, "排队消息内容不能为空")
				continue
			}
			if len(msg.Content) > maxClientContentBytes || !utf8.ValidString(msg.Content) {
				c.notice(convID, "排队消息超过大小限制")
				continue
			}
			requestID, validRequestID := parseRequestID(msg.RequestID)
			if !validRequestID {
				c.notice(convID, "request_id 必须是有效 UUID")
				continue
			}
			if err := h.queueGeneration(commandCtx, convID, func(ctx context.Context, emit EmitFunc) {
				h.gen.Generate(ctx, convID, requestID, msg.Content, emit)
			}); err != nil {
				c.notice(convID, err.Error())
			}
		case TypeClearQueue:
			h.clearGenerationQueue(convID)
		}
	}
}

func parseRequestID(raw string) (uuid.UUID, bool) {
	if strings.TrimSpace(raw) == "" {
		return uuid.New(), true
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}

// startGeneration 启动一个可取消的生成任务，结果通过 Broadcast 推送给对话内所有连接。
func (h *Hub) startGeneration(baseCtx context.Context, convID uuid.UUID, run func(context.Context, EmitFunc)) bool {
	ctx, cancel := context.WithCancel(context.WithoutCancel(baseCtx))
	task := &genTask{cancel: cancel, done: make(chan struct{})}
	h.mu.Lock()
	old := h.cancels[convID]
	if h.closed || old != nil && !old.finished {
		h.mu.Unlock()
		cancel()
		return false
	}
	h.cancels[convID] = task
	h.mu.Unlock()

	emit := func(ev Event) { h.Broadcast(convID, ev) }
	go func() {
		defer func() {
			cancel()
			h.mu.Lock()
			task.finished = true
			// 仅当 map 中登记的仍是自己时才删除，避免误删新任务的 cancel。
			if h.cancels[convID] == task {
				delete(h.cancels, convID)
			}
			h.mu.Unlock()
			close(task.done)
		}()
		for {
			if ctx.Err() != nil {
				return
			}
			runCtx, cancelRun := context.WithCancel(context.WithoutCancel(baseCtx))
			stopCancel := context.AfterFunc(ctx, cancelRun)
			h.runGuarded(runCtx, convID, run, emit)
			stopCancel()
			cancelRun()
			h.mu.Lock()
			// Marking the task finished and deciding whether to consume the next
			// queued run happen under the same lock as queueGeneration. A request
			// arriving at this boundary therefore either joins the queue before
			// it is drained or starts a fresh task after this one is finished; it
			// can never be appended after the queue was deleted and then lost.
			if ctx.Err() != nil {
				task.finished = true
				dropped := 0
				if h.cancels[convID] == task {
					dropped = len(h.queued[convID])
					delete(h.queued, convID)
				}
				h.mu.Unlock()
				if dropped > 0 {
					// 取消会连带丢弃排队中的 steer/follow-up，必须让客户端计数归零。
					h.Broadcast(convID, Event{Type: TypeAgentQueueUpdated, ConversationID: convID.String(), Payload: map[string]any{"count": 0}})
				}
				return
			}
			pending := h.queued[convID]
			if len(pending) == 0 {
				task.finished = true
				delete(h.queued, convID)
				h.mu.Unlock()
				return
			}
			next := pending[0]
			if len(pending) == 1 {
				delete(h.queued, convID)
			} else {
				h.queued[convID] = pending[1:]
			}
			h.mu.Unlock()
			run = next.run
			baseCtx = next.ctx
		}
	}()
	return true
}

// queueGeneration appends an explicit follow-up/steer run when a task is
// active. If idle, it starts immediately. The active task drains queued runs
// serially, preserving conversation lock and event order.
func (h *Hub) queueGeneration(baseCtx context.Context, convID uuid.UUID, run func(context.Context, EmitFunc)) error {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return errHubClosed
	}
	task := h.cancels[convID]
	if task != nil && !task.finished {
		if len(h.queued[convID]) >= maxGenerationQueueLength {
			h.mu.Unlock()
			return errGenerationQueueFull
		}
		h.queued[convID] = append(h.queued[convID], queuedGeneration{ctx: context.WithoutCancel(baseCtx), run: run})
		queuedCount := len(h.queued[convID])
		h.mu.Unlock()
		h.Broadcast(convID, Event{Type: TypeAgentQueueUpdated, ConversationID: convID.String(), Payload: map[string]any{"count": queuedCount}})
		return nil
	}
	h.mu.Unlock()
	if !h.startGeneration(baseCtx, convID, run) {
		// 极小窗口：释放锁后刚好有别的任务启动。作为排队请求再入队一次。
		return h.queueGeneration(baseCtx, convID, run)
	}
	return nil
}

var errGenerationQueueFull = errors.New("生成队列已满")
var errHubClosed = errors.New("生成服务正在关闭")

// Shutdown rejects new producers, disconnects sockets and waits for active runs.
func (h *Hub) Shutdown(ctx context.Context) error {
	h.mu.Lock()
	h.closed = true
	tasks := make([]*genTask, 0, len(h.cancels))
	for _, task := range h.cancels {
		task.cancel()
		tasks = append(tasks, task)
	}
	h.queued = map[uuid.UUID][]queuedGeneration{}
	connections := []*conn{}
	for _, set := range h.conns {
		for c := range set {
			connections = append(connections, c)
		}
	}
	h.mu.Unlock()
	for _, c := range connections {
		_ = c.ws.Close()
		c.close()
	}
	for _, task := range tasks {
		select {
		case <-task.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// runGuarded 执行一次生成并兜住 panic：任何玩法或工具代码的 panic 只能
// 让这一次生成失败并释放对话，不能拖垮整个进程。
func (h *Hub) runGuarded(ctx context.Context, convID uuid.UUID, run func(context.Context, EmitFunc), emit EmitFunc) {
	defer func() {
		if recovered := recover(); recovered != nil {
			logging.FromContext(ctx, "ws").Error("生成任务 panic",
				zap.String("conversation_id", convID.String()),
				zap.Any("panic", recovered), zap.String("stack", string(debug.Stack())))
			if h.gen != nil {
				h.gen.RecoverPanic(context.WithoutCancel(ctx), convID, recovered)
			}
			emit(Event{Type: TypeError, ConversationID: convID.String(), Error: "生成过程发生内部错误，已中止本轮"})
		}
	}()
	run(ctx, emit)
}

// notice 只向当前连接发送一条非终止提示。
func (c *conn) notice(convID uuid.UUID, message string) {
	c.enqueue(Event{Type: TypeNotice, ConversationID: convID.String(), Error: message})
}

func (h *Hub) clearGenerationQueue(convID uuid.UUID) {
	h.mu.Lock()
	delete(h.queued, convID)
	h.mu.Unlock()
	h.Broadcast(convID, Event{Type: TypeAgentQueueUpdated, ConversationID: convID.String(), Payload: map[string]any{"count": 0}})
}

// WaitGeneration waits for the current task and all of its queued runs to
// finish. It is useful to callers that need a consistent post-generation
// snapshot without exposing task internals.
func (h *Hub) WaitGeneration(ctx context.Context, convID uuid.UUID) error {
	h.mu.Lock()
	task := h.cancels[convID]
	h.mu.Unlock()
	if task == nil {
		return nil
	}
	select {
	case <-task.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// QueueDetached appends a generation run without cancelling an active task.
// REST handlers can use this for explicit follow-up operations.
func (h *Hub) QueueDetached(baseCtx context.Context, convID uuid.UUID, run func(context.Context, EmitFunc)) error {
	return h.queueGeneration(baseCtx, convID, run)
}

// ClearQueue removes pending steer/follow-up runs but leaves the active model
// request untouched.
func (h *Hub) ClearQueue(convID uuid.UUID) {
	h.clearGenerationQueue(convID)
}

func (h *Hub) cancelGeneration(ctx context.Context, convID uuid.UUID) {
	h.mu.Lock()
	task := h.cancels[convID]
	h.mu.Unlock()
	if task != nil {
		h.requestCancel(ctx, convID, "user_cancelled")
		task.cancel()
	}
}

func (h *Hub) requestCancel(ctx context.Context, convID uuid.UUID, reason string) {
	if h.gen == nil {
		return
	}
	cancelCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := h.gen.RequestCancel(cancelCtx, convID, reason); err != nil {
		logging.Named("ws").Warn("持久化生成取消请求失败",
			zap.String("conversation_id", convID.String()), zap.String("reason", reason), zap.Error(err))
	}
}

// CancelAndWait requests cancellation and waits until the task goroutine has
// released its durable generation run. Lifecycle mutations use this boundary
// so they never race an in-flight writer.
func (h *Hub) CancelAndWait(ctx context.Context, convID uuid.UUID) error {
	h.mu.Lock()
	task := h.cancels[convID]
	h.mu.Unlock()
	if task == nil {
		return nil
	}
	h.requestCancel(ctx, convID, "lifecycle_changed")
	task.cancel()
	select {
	case <-task.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Gen 暴露生成编排入口，便于 REST 触发的 regenerate/continue 复用生成流程。
func (h *Hub) Gen() Generator { return h.gen }

// RunDetached 在后台运行一个生成任务（用于 REST 触发的 regenerate/continue），
// 结果仍通过对话的 WebSocket 广播。
func (h *Hub) RunDetached(baseCtx context.Context, convID uuid.UUID, run func(context.Context, EmitFunc)) bool {
	return h.startGeneration(baseCtx, convID, run)
}
