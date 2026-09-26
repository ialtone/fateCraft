package llmrelay

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"github.com/ialtone/fatecraft/internal/llm"
	"github.com/ialtone/fatecraft/internal/logging"
	"github.com/ialtone/fatecraft/internal/storage"
)

// relayConn 是一条用户级 Relay 连接（一个浏览器标签页）。
// 多标签页时请求只派发给最近注册且存活的一条，旧连接保留作候补。
type relayConn struct {
	id       uuid.UUID
	playerID uuid.UUID
	ws       *websocket.Conn
	registry *Registry

	writeMu sync.Mutex

	pendingMu sync.Mutex
	pending   map[uuid.UUID]*pendingRequest

	closed    chan struct{}
	closeOnce sync.Once
}

func newRelayConn(playerID uuid.UUID, ws *websocket.Conn, registry *Registry) *relayConn {
	return &relayConn{
		id:       uuid.New(),
		playerID: playerID,
		ws:       ws,
		registry: registry,
		pending:  make(map[uuid.UUID]*pendingRequest),
		closed:   make(chan struct{}),
	}
}

func (c *relayConn) alive() bool {
	select {
	case <-c.closed:
		return false
	default:
		return true
	}
}

// close 关闭连接：readPump 退出，各 pending 按“连接断开”收尾。
func (c *relayConn) close() {
	c.closeOnce.Do(func() {
		close(c.closed)
		_ = c.ws.Close()
	})
}

// Serve 接管一条已鉴权的 HTTP 连接：准入校验通过后升级为 Relay 连接，
// 退出时注销并收尾该连接上的全部请求。调用方须已做 requireAuth。
func (r *Registry) Serve(w http.ResponseWriter, req *http.Request, playerID uuid.UUID) {
	if r.effectiveRoute != nil {
		route, err := r.effectiveRoute(req.Context(), playerID.String())
		if err != nil || route != storage.LLMRouteClient {
			http.Error(w, "relay requires client llm route", http.StatusForbidden)
			return
		}
	}
	upgrader := websocket.Upgrader{
		CheckOrigin: func(req *http.Request) bool {
			if len(r.allowedOrigins) == 0 {
				return true
			}
			origin := req.Header.Get("Origin")
			for _, allowed := range r.allowedOrigins {
				if origin == allowed {
					return true
				}
			}
			return false
		},
	}
	ws, err := upgrader.Upgrade(w, req, nil)
	if err != nil {
		logging.Named("llmrelay").Warn("Relay 连接升级失败",
			zap.String("player_id", playerID.String()), zap.Error(err))
		return
	}
	conn := newRelayConn(playerID, ws, r)
	r.add(playerID, conn)
	defer r.remove(playerID, conn)

	ws.SetReadLimit(relayMaxFrameBytes)
	_ = ws.SetReadDeadline(time.Now().Add(relayPongWait))
	ws.SetPongHandler(func(string) error {
		return ws.SetReadDeadline(time.Now().Add(relayPongWait))
	})
	go conn.pingLoop()
	conn.readPump()
	conn.close()
}

// pingLoop 每 30s 发 ping，与 Hub 参数一致。
func (c *relayConn) pingLoop() {
	ticker := time.NewTicker(relayPingPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			c.writeMu.Lock()
			_ = c.ws.SetWriteDeadline(time.Now().Add(relayWriteWait))
			err := c.ws.WriteMessage(websocket.PingMessage, nil)
			c.writeMu.Unlock()
			if err != nil {
				c.close()
				return
			}
		case <-c.closed:
			return
		}
	}
}

// sendJSON 串行写帧。
func (c *relayConn) sendJSON(frame any) error {
	raw, err := json.Marshal(frame)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = c.ws.SetWriteDeadline(time.Now().Add(relayWriteWait))
	return c.ws.WriteMessage(websocket.TextMessage, raw)
}

// requestCancel 尽力发送取消帧；连接已死时静默丢弃。
func (c *relayConn) requestCancel(relayID uuid.UUID) {
	_ = c.sendJSON(serverCancelFrame{Type: msgRelayCancel, RelayID: relayID.String()})
}

func (c *relayConn) addPending(p *pendingRequest) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	c.pending[p.relayID] = p
}

func (c *relayConn) takePending(relayID uuid.UUID) (*pendingRequest, bool) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	p, ok := c.pending[relayID]
	return p, ok
}

func (c *relayConn) removePending(relayID uuid.UUID) {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	delete(c.pending, relayID)
}

// failAllPending 以连接断开收尾该连接上的全部请求：
// 响应头到达之前的请求可重试（ErrRelayUnavailable），之后的不可重试（ErrRelayLost）。
func (c *relayConn) failAllPending() {
	c.pendingMu.Lock()
	pending := make([]*pendingRequest, 0, len(c.pending))
	for _, p := range c.pending {
		pending = append(pending, p)
	}
	c.pendingMu.Unlock()
	for _, p := range pending {
		if p.headDelivered() {
			p.finish(llm.ErrRelayLost)
		} else {
			p.finish(llm.ErrRelayUnavailable)
		}
	}
}

// readPump 消费客户端帧并分发到各 pending 请求；退出时收尾全部请求。
func (c *relayConn) readPump() {
	defer c.failAllPending()
	for {
		_ = c.ws.SetReadDeadline(time.Now().Add(relayPongWait))
		_, raw, err := c.ws.ReadMessage()
		if err != nil {
			return
		}
		var header clientFrameHeader
		if err := json.Unmarshal(raw, &header); err != nil {
			logging.Named("llmrelay").Warn("Relay 帧解析失败", zap.Error(err))
			continue
		}
		relayID, err := uuid.Parse(header.RelayID)
		if err != nil {
			logging.Named("llmrelay").Warn("Relay 帧缺少合法 relay_id",
				zap.String("type", header.Type))
			continue
		}
		switch header.Type {
		case msgRelayHead:
			var frame clientHeadFrame
			if err := json.Unmarshal(raw, &frame); err != nil {
				continue
			}
			c.onHead(relayID, frame)
		case msgRelayChunk:
			var frame clientChunkFrame
			if err := json.Unmarshal(raw, &frame); err != nil {
				continue
			}
			c.onChunk(relayID, frame)
		case msgRelayEnd:
			var frame clientEndFrame
			if err := json.Unmarshal(raw, &frame); err != nil {
				continue
			}
			c.onEnd(relayID, frame)
		case msgRelayError:
			var frame clientErrorFrame
			if err := json.Unmarshal(raw, &frame); err != nil {
				continue
			}
			c.onError(relayID, frame)
		default:
			logging.Named("llmrelay").Warn("未知 Relay 帧类型",
				zap.String("type", header.Type), zap.String("relay_id", relayID.String()))
		}
	}
}

func (c *relayConn) onHead(relayID uuid.UUID, frame clientHeadFrame) {
	p, ok := c.takePending(relayID)
	if !ok {
		return
	}
	p.deliverHead(frame)
}

func (c *relayConn) onChunk(relayID uuid.UUID, frame clientChunkFrame) {
	p, ok := c.takePending(relayID)
	if !ok {
		return
	}
	p.deliverChunk(frame.Seq, frame.Data)
}

func (c *relayConn) onEnd(relayID uuid.UUID, frame clientEndFrame) {
	p, ok := c.takePending(relayID)
	if !ok {
		return
	}
	p.deliverEnd(frame.Seq)
}

func (c *relayConn) onError(relayID uuid.UUID, frame clientErrorFrame) {
	p, ok := c.takePending(relayID)
	if !ok {
		return
	}
	p.deliverError(frame.Code)
}
