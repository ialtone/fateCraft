package orchestration

import (
	"context"

	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/protocol"
	"github.com/ialtone/fatecraft/internal/realtime"
)

// ActionHandler 处理一种 AI 动作（envelope.action）的玩法副作用。
//
// 编排器本身保持题材中立：具体玩法的记账逻辑（如"修仙"预设包的灵石/轮回结算）
// 以处理器形式注册进 Generator（GeneratorDeps.Actions），编排器只负责按类型分发。
type ActionHandler interface {
	// Handle 执行动作副作用。实现应自行容错（尽力而为），不中断生成主流程。
	Handle(ctx context.Context, convID uuid.UUID, params map[string]any, emit realtime.EmitFunc)
}

// handleAction 按 action.type 分发到注册的处理器；未注册的动作类型直接忽略。
func (g *Generator) handleAction(ctx context.Context, convID uuid.UUID, action *protocol.Action, emit realtime.EmitFunc) {
	if action == nil {
		return
	}
	if h, ok := g.actions[action.Type]; ok {
		h.Handle(ctx, convID, action.Params, emit)
	}
}

// getStringParam 从参数 map 中安全提取字符串值，失败时返回默认值。
func getStringParam(params map[string]any, key string, defaultVal string) string {
	if v, ok := params[key].(string); ok {
		return v
	}
	return defaultVal
}
