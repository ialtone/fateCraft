package realtime

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"github.com/ialtone/fatecraft/internal/logging"
)

// 读超时：writePump 每 30s ping 一次，客户端 pong 会刷新读期限；
// 超过 60s 无任何帧（含 pong）视为死连接，及时释放而非等 TCP 超时。
const readWait = 60 * time.Second

// Generator 是 Hub 触发生成所需的编排入口，由 orchestration 包的 Generator 实现。
// 以消费端接口声明（事件类型定义在本包），避免 realtime 与编排包形成环依赖。
type Generator interface {
	// Generate 处理一次用户输入：保存用户消息并生成 AI 回复。
	Generate(ctx context.Context, convID uuid.UUID, userContent string, emit EmitFunc)
	// Regenerate 删除最近一条 AI 回复后重新生成。
	Regenerate(ctx context.Context, convID uuid.UUID, emit EmitFunc)
	// Continue 在不新增用户输入的情况下追加一段 AI 续写。
	Continue(ctx context.Context, convID uuid.UUID, emit EmitFunc)
	// GenerateAfterEdit 在用户消息已被编辑、后续消息已删除的前提下重新生成（天道回溯）。
	GenerateAfterEdit(ctx context.Context, convID uuid.UUID, emit EmitFunc)
}

// Hub 管理 WebSocket 连接，按 conversation_id 分组。
// 一个连接只绑定一个对话；同一对话同一时间只允许一个生成任务。
type Hub struct {
	gen      Generator
	upgrader websocket.Upgrader

	mu      sync.Mutex
	conns   map[uuid.UUID]map[*conn]struct{} // 每个对话的连接集合
	cancels map[uuid.UUID]*genTask           // 每个对话当前生成任务
}

// genTask 包装一次生成任务的取消函数；用指针身份区分新旧任务，
// 避免旧任务结束时误删新任务已注册的 cancel。
// done 在任务 goroutine 退出时关闭，供后继任务等待旧任务释放对话锁。
type genTask struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// NewHub 创建 Hub。allowedOrigins 为允许的 WebSocket 来源列表；
// 为空时放开所有来源（仅供开发使用，生产必须配置 ALLOWED_ORIGINS）。
func NewHub(gen Generator, allowedOrigins []string) *Hub {
	return &Hub{
		gen:     gen,
		conns:   map[uuid.UUID]map[*conn]struct{}{},
		cancels: map[uuid.UUID]*genTask{},
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
type conn struct {
	ws   *websocket.Conn
	send chan []byte
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
	c := &conn{ws: ws, send: make(chan []byte, 32)}
	h.register(convID, c)
	defer h.unregister(convID, c)

	go h.writePump(c)
	h.readPump(convID, c)
	return nil
}

func (h *Hub) register(convID uuid.UUID, c *conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.conns[convID] == nil {
		h.conns[convID] = map[*conn]struct{}{}
	}
	h.conns[convID][c] = struct{}{}
}

func (h *Hub) unregister(convID uuid.UUID, c *conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if set := h.conns[convID]; set != nil {
		delete(set, c)
		if len(set) == 0 {
			delete(h.conns, convID)
		}
	}
	close(c.send)
}

// Broadcast 向某对话的所有连接推送事件。
func (h *Hub) Broadcast(convID uuid.UUID, ev Event) {
	data, err := json.Marshal(ev)
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.conns[convID] {
		select {
		case c.send <- data:
		default:
			// 发送队列已满则丢弃，避免阻塞整个 hub。
		}
	}
}

func (h *Hub) writePump(c *conn) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case msg, ok := <-c.send:
			if !ok {
				_ = c.ws.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.ws.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			if err := c.ws.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (h *Hub) readPump(convID uuid.UUID, c *conn) {
	defer c.ws.Close()
	// 死连接检测：初始读期限 + pong 刷新（配合 writePump 的周期 ping）。
	_ = c.ws.SetReadDeadline(time.Now().Add(readWait))
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
		switch msg.Type {
		case TypeUserInput:
			// 与 REST 入口保持一致：空内容拒绝，避免落一条空用户消息。
			if strings.TrimSpace(msg.Content) == "" {
				h.Broadcast(convID, Event{Type: TypeError, ConversationID: convID.String(), Error: "消息内容不能为空"})
				continue
			}
			h.startGeneration(convID, func(ctx context.Context, emit EmitFunc) {
				h.gen.Generate(ctx, convID, msg.Content, emit)
			})
		case TypeCancelGeneration:
			h.cancelGeneration(convID)
		}
	}
}

// startGeneration 启动一个可取消的生成任务，结果通过 Broadcast 推送给对话内所有连接。
func (h *Hub) startGeneration(convID uuid.UUID, run func(context.Context, EmitFunc)) {
	ctx, cancel := context.WithCancel(context.Background())
	task := &genTask{cancel: cancel, done: make(chan struct{})}
	h.mu.Lock()
	// 若已有任务在跑，先取消旧任务（对话锁会进一步保证互斥）。
	old := h.cancels[convID]
	if old != nil {
		old.cancel()
	}
	h.cancels[convID] = task
	h.mu.Unlock()

	emit := func(ev Event) { h.Broadcast(convID, ev) }
	go func() {
		defer func() {
			h.mu.Lock()
			// 仅当 map 中登记的仍是自己时才删除，避免误删新任务的 cancel。
			if h.cancels[convID] == task {
				delete(h.cancels, convID)
			}
			h.mu.Unlock()
			cancel()
			close(task.done)
		}()
		// 等旧任务退出（其 defer 会释放对话 DB 锁）再启动，
		// 避免新任务 TryLock 撞上旧锁而误报"正在生成中"。限时兜底防止挂死。
		if old != nil {
			select {
			case <-old.done:
			case <-time.After(5 * time.Second):
			}
		}
		run(ctx, emit)
	}()
}

func (h *Hub) cancelGeneration(convID uuid.UUID) {
	h.mu.Lock()
	task := h.cancels[convID]
	h.mu.Unlock()
	if task != nil {
		task.cancel()
	}
}

// Gen 暴露生成编排入口，便于 REST 触发的 regenerate/continue 复用生成流程。
func (h *Hub) Gen() Generator { return h.gen }

// RunDetached 在后台运行一个生成任务（用于 REST 触发的 regenerate/continue），
// 结果仍通过对话的 WebSocket 广播。
func (h *Hub) RunDetached(convID uuid.UUID, run func(context.Context, EmitFunc)) {
	h.startGeneration(convID, run)
}
