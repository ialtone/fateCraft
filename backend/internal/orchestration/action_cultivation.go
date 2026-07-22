package orchestration

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/ialtone/fatecraft/internal/challenge"
	"github.com/ialtone/fatecraft/internal/chat"
	"github.com/ialtone/fatecraft/internal/identity"
	"github.com/ialtone/fatecraft/internal/realtime"
	"github.com/ialtone/fatecraft/internal/state"
	"github.com/ialtone/fatecraft/internal/storage"
)

// CultivationEndingHandler 是"修仙"玩法（reincarnation-trials 预设包）的
// trigger_ending 动作处理器：采集服务端状态并委托挑战服务事务结算。
//
// 该玩法记账逻辑从通用编排器中拆出：换题材的预设包注册各自的处理器即可，
// 编排器不再硬编码任何题材语义。
type CultivationEndingHandler struct {
	chatSvc   *chat.Service
	stateSvc  *state.Service
	challenge *challenge.Service
	identity  identity.Provider // 当前玩家身份来源（由认证中间件写入 context）
}

// NewCultivationEndingHandler 构造修仙结局处理器。
func NewCultivationEndingHandler(
	chatSvc *chat.Service,
	stateSvc *state.Service,
	challengeSvc *challenge.Service,
	ident identity.Provider,
) *CultivationEndingHandler {
	return &CultivationEndingHandler{
		chatSvc: chatSvc, stateSvc: stateSvc, challenge: challengeSvc, identity: ident,
	}
}

// Prepare 在模型状态更新落库前采集并校验结算输入。
func (h *CultivationEndingHandler) Prepare(ctx context.Context, convID uuid.UUID, params map[string]any) (PreparedAction, error) {
	endingType := getStringParam(params, "ending_type", "neutral")
	rewardKey := getStringParam(params, "reward_key", "")
	title := getStringParam(params, "title", "故事终章")
	summary := getStringParam(params, "summary", "")

	// 获取当前状态，提取灵石数量和角色信息。
	currentState, err := h.stateSvc.Get(ctx, convID)
	if err != nil {
		return nil, err
	}
	spiritStones := extractSpiritStones(currentState)
	if spiritStones < 0 {
		spiritStones = 0
	}
	characterName := extractStringFromState(currentState, "profile.name")
	cultivationLevel := extractStringFromState(currentState, "profile.cultivation_level")

	// 计算存活回合数（用户消息数量）。
	messages, err := h.chatSvc.History(ctx, convID, false)
	if err != nil {
		return nil, err
	}
	survivalTurns := 0
	for _, msg := range messages {
		if msg.Role == storage.RoleUser {
			survivalTurns++
		}
	}

	playerID, err := uuid.Parse(h.identity.CurrentPlayerID(ctx))
	if err != nil {
		return nil, errors.New("当前用户身份无效")
	}
	input := challenge.SettlementInput{
		ConversationID: convID, PlayerID: playerID, EndingType: endingType,
		EndingTitle: title, RewardKey: rewardKey, BaseReward: spiritStones,
		CharacterName: characterName, CultivationLevel: cultivationLevel, SurvivalTurns: survivalTurns,
	}
	if err := h.challenge.ValidateSettlement(ctx, input); err != nil {
		return nil, err
	}
	return &preparedCultivationEnding{handler: h, input: input, summary: summary}, nil
}

type preparedCultivationEnding struct {
	handler *CultivationEndingHandler
	input   challenge.SettlementInput
	summary string
}

// Commit 使用 Prepare 时冻结的状态和奖励基数执行事务结算。
func (a *preparedCultivationEnding) Commit(ctx context.Context, emit realtime.EmitFunc) {
	result, err := a.handler.challenge.Settle(ctx, a.input)
	if err != nil {
		msg := "挑战结算未通过：" + err.Error()
		fields := []zap.Field{
			zap.String("ending_type", a.input.EndingType),
			zap.String("ending_title", a.input.EndingTitle),
			zap.String("reward_key", a.input.RewardKey),
			zap.Error(err),
		}
		switch {
		case errors.Is(err, challenge.ErrSettlementDenied), errors.Is(err, storage.ErrNotFound):
			emit(warningEvent(a.input.ConversationID, msg, fields...))
		case errors.Is(err, storage.ErrAlreadySettled), errors.Is(err, challenge.ErrConversationEnded):
			emit(infoEvent(a.input.ConversationID, msg, fields...))
		default:
			emit(errorEvent(a.input.ConversationID, "挑战结算失败: "+err.Error(), fields...))
		}
		return
	}

	// 广播结局事件。
	emit(realtime.Event{
		Type:           realtime.TypeEndingTriggered,
		ConversationID: a.input.ConversationID.String(),
		EndingType:     a.input.EndingType,
		EndingTitle:    a.input.EndingTitle,
		EndingSummary:  a.summary,
		SpiritStones:   result.Run.RewardAmount,
		SurvivalTurns:  a.input.SurvivalTurns,
	})
}

// extractSpiritStones 从状态中提取灵石数量。
func extractSpiritStones(state map[string]any) int64 {
	// 尝试从 attributes.灵石 提取。
	if attrs, ok := state["attributes"].(map[string]any); ok {
		if stones, ok := attrs["灵石"]; ok {
			switch v := stones.(type) {
			case float64:
				return int64(v)
			case int:
				return int64(v)
			case int64:
				return v
			}
		}
	}
	return 0
}

// extractStringFromState 从状态中按点号路径提取字符串值。
func extractStringFromState(state map[string]any, path string) string {
	parts := strings.Split(path, ".")
	current := state
	for i, part := range parts {
		if i == len(parts)-1 {
			if v, ok := current[part].(string); ok {
				return v
			}
			return ""
		}
		if next, ok := current[part].(map[string]any); ok {
			current = next
		} else {
			return ""
		}
	}
	return ""
}
