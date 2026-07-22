// Package identity 提供请求级玩家身份抽象。
package identity

import "context"

// Provider 解析当前请求对应的玩家 id。
type Provider interface {
	// CurrentPlayerID 返回当前请求对应的玩家 id。
	CurrentPlayerID(ctx context.Context) string
}

type playerIDKey struct{}

// WithPlayerID 把已认证的玩家 id 写入 context。后台任务必须保留该 context 的 values。
func WithPlayerID(ctx context.Context, playerID string) context.Context {
	return context.WithValue(ctx, playerIDKey{}, playerID)
}

// Context 从请求 context 读取身份。未经过认证中间件时返回空字符串。
type Context struct{}

func NewContext() Context { return Context{} }

func (Context) CurrentPlayerID(ctx context.Context) string {
	playerID, _ := ctx.Value(playerIDKey{}).(string)
	return playerID
}
