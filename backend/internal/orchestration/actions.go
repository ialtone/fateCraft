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
	// Prepare 在任何模型叙事或状态落库前校验动作，并冻结提交所需的服务端输入。
	Prepare(ctx context.Context, convID uuid.UUID, params map[string]any) (PreparedAction, error)
}

// PreparedAction 是已经通过保存前校验的动作。Commit 只能使用 Prepare 冻结的输入，
// 不得重新读取本次模型回复刚写入的状态。
type PreparedAction interface {
	Commit(ctx context.Context, emit realtime.EmitFunc)
}

// prepareAction 按 action.type 分发；未注册的动作保持向后兼容，直接忽略。
func (g *Generator) prepareAction(ctx context.Context, convID uuid.UUID, action *protocol.Action) (PreparedAction, error) {
	if action == nil {
		return nil, nil
	}
	if h, ok := g.actions[action.Type]; ok {
		return h.Prepare(ctx, convID, action.Params)
	}
	return nil, nil
}

// getStringParam 从参数 map 中安全提取字符串值，失败时返回默认值。
func getStringParam(params map[string]any, key string, defaultVal string) string {
	if v, ok := params[key].(string); ok {
		return v
	}
	return defaultVal
}
