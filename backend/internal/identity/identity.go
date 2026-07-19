// Package identity 提供玩家身份抽象：把过去散落注入各构造函数的
// playerID string 收敛成单方法接口 Provider。
//
// 当前为单用户模式，Static 固定返回一个开发期玩家 id；
// 未来多用户化时只需替换 Provider 实现（如从请求上下文/JWT 解析），
// 下游持有者（conversation.Service、api.Server、动作处理器等）无需改动。
package identity

import "context"

// Provider 解析「当前玩家 id」。多用户化时改为从 ctx 中读取鉴权信息即可。
type Provider interface {
	// CurrentPlayerID 返回当前请求对应的玩家 id。
	CurrentPlayerID(ctx context.Context) string
}

// Static 是单用户模式下的固定身份实现，始终返回同一个玩家 id。
type Static struct {
	playerID string
}

// NewStatic 用固定玩家 id 构造 Static Provider（供组合根注入）。
func NewStatic(playerID string) Static {
	return Static{playerID: playerID}
}

// CurrentPlayerID 返回固定玩家 id，忽略 ctx。
func (s Static) CurrentPlayerID(ctx context.Context) string {
	return s.playerID
}
