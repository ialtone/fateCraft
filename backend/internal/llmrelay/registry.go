// Package llmrelay 管理用户级 Relay 连接，实现 llm.RelayTransport。
//
// 一条 Relay 连接对应一个打开着前端页面的浏览器标签页；服务端把已构建好的
// 模型 HTTP 请求经该连接交给浏览器，用浏览器本地 Key 发出并把原始响应字节
// 回传。服务端继续承担全部协议解析、校验与审计，不信任浏览器做任何归一化。
package llmrelay

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/llm"
)

const (
	// relayHeadTimeout 等待响应头的最长时长，与 ResponseHeaderTimeout=2m 对齐。
	relayHeadTimeout = 120 * time.Second
	// relayMaxBodyBytes 出站 relay.request 的 body 上限，超限直接失败，不发送。
	relayMaxBodyBytes = 8 << 20
	// relayMaxFrameBytes 入站单帧上限，独立于会话 WS 的 64KiB 限制。
	relayMaxFrameBytes = 256 << 10
	// relayMaxConcurrent 每玩家并发 Relay 请求上限，超出排队等待（受 ctx 约束）。
	relayMaxConcurrent = 4
	// relayReleaseRetryDelay 释放回 pending 的任务重试间隔由调用方决定；
	// 此处仅为心跳参数。
	relayWriteWait  = 10 * time.Second
	relayPongWait   = 60 * time.Second
	relayPingPeriod = 30 * time.Second
)

// RouteResolver 解析玩家当前有效路由（llmsettings.Service 实现）。
type RouteResolver func(ctx context.Context, playerID string) (string, error)

// Registry 维护玩家到 Relay 连接的映射（按注册时间排序）。
type Registry struct {
	mu             sync.Mutex
	conns          map[uuid.UUID][]*relayConn
	semaphores     map[uuid.UUID]chan struct{}
	effectiveRoute RouteResolver
	allowedOrigins []string
	// headTimeout 等待响应头的最长时长，默认 relayHeadTimeout；测试可调小。
	headTimeout time.Duration
}

// NewRegistry 构造 Relay 注册表。effectiveRoute 为 nil 时不做路由准入
// （仅测试路径）；allowedOrigins 为空时允许所有来源（与 Hub 一致）。
func NewRegistry(effectiveRoute RouteResolver, allowedOrigins []string) *Registry {
	return &Registry{
		conns:          make(map[uuid.UUID][]*relayConn),
		semaphores:     make(map[uuid.UUID]chan struct{}),
		effectiveRoute: effectiveRoute,
		allowedOrigins: allowedOrigins,
		headTimeout:    relayHeadTimeout,
	}
}

// relayTransport 是 Registry.For 返回的 llm.RelayTransport 实现。
type relayTransport struct {
	registry *Registry
	playerID uuid.UUID
}

// parsePlayerID 解析玩家 ID；非法时返回零 UUID（该 ID 下永远没有连接，
// Do 直接报 ErrRelayUnavailable，不会误派给他人）。
func parsePlayerID(playerID string) uuid.UUID {
	parsed, err := uuid.Parse(playerID)
	if err != nil {
		return uuid.Nil
	}
	return parsed
}

// For 返回该玩家当前的 Relay 传输；没有活动连接时返回的 Transport 在 Do
// 时报 ErrRelayUnavailable（可重试）。
func (r *Registry) For(playerID string) llm.RelayTransport {
	return &relayTransport{registry: r, playerID: parsePlayerID(playerID)}
}

// HasActive 报告该玩家当前是否有存活的 Relay 连接。
func (r *Registry) HasActive(playerID string) bool {
	id := parsePlayerID(playerID)
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, conn := range r.conns[id] {
		if conn.alive() {
			return true
		}
	}
	return false
}

// ClosePlayer 主动关闭该玩家的全部 Relay 连接（切回 server / 切到
// platform_managed 时调用）。进行中的请求按连接断开处理。
func (r *Registry) ClosePlayer(playerID string) {
	id := parsePlayerID(playerID)
	r.mu.Lock()
	conns := append([]*relayConn(nil), r.conns[id]...)
	delete(r.conns, id)
	r.mu.Unlock()
	for _, conn := range conns {
		conn.close()
	}
}

// CloseAll 关闭全部玩家的 Relay 连接（管理员切换到 platform_managed 时调用）。
func (r *Registry) CloseAll() {
	r.mu.Lock()
	var conns []*relayConn
	for playerID, list := range r.conns {
		conns = append(conns, list...)
		delete(r.conns, playerID)
	}
	r.mu.Unlock()
	for _, conn := range conns {
		conn.close()
	}
}

// semaphore 返回该玩家的并发信号量（cap relayMaxConcurrent）。
func (r *Registry) semaphore(playerID uuid.UUID) chan struct{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	sem, ok := r.semaphores[playerID]
	if !ok {
		sem = make(chan struct{}, relayMaxConcurrent)
		r.semaphores[playerID] = sem
	}
	return sem
}

// latest 返回该玩家最近注册且存活的连接，无则返回 nil。
func (r *Registry) latest(playerID uuid.UUID) *relayConn {
	r.mu.Lock()
	defer r.mu.Unlock()
	conns := r.conns[playerID]
	for i := len(conns) - 1; i >= 0; i-- {
		if conns[i].alive() {
			return conns[i]
		}
	}
	return nil
}

func (r *Registry) add(playerID uuid.UUID, conn *relayConn) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.conns[playerID] = append(r.conns[playerID], conn)
}

func (r *Registry) remove(playerID uuid.UUID, conn *relayConn) {
	r.mu.Lock()
	defer r.mu.Unlock()
	conns := r.conns[playerID]
	for i, c := range conns {
		if c == conn {
			r.conns[playerID] = append(conns[:i], conns[i+1:]...)
			break
		}
	}
	if len(r.conns[playerID]) == 0 {
		delete(r.conns, playerID)
	}
}
