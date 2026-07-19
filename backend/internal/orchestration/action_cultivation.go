package orchestration

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/ialtone/fatecraft/internal/chat"
	"github.com/ialtone/fatecraft/internal/conversation"
	"github.com/ialtone/fatecraft/internal/identity"
	"github.com/ialtone/fatecraft/internal/logging"
	"github.com/ialtone/fatecraft/internal/realtime"
	"github.com/ialtone/fatecraft/internal/state"
	"github.com/ialtone/fatecraft/internal/storage"
)

// CultivationEndingHandler 是"修仙"玩法（reincarnation-trials 预设包）的
// trigger_ending 动作处理器：提取灵石/修为等级、记录轮回历史（TrialHistory）、
// 按结局类型更新账号统计，最后结束对话并广播结局事件。
//
// 该玩法记账逻辑从通用编排器中拆出：换题材的预设包注册各自的处理器即可，
// 编排器不再硬编码任何题材语义。
type CultivationEndingHandler struct {
	convSvc     *conversation.Service
	chatSvc     *chat.Service
	stateSvc    *state.Service
	accountRepo *storage.AccountRepository
	identity    identity.Provider // 当前玩家身份来源（单用户模式下固定返回 dev id）
}

// NewCultivationEndingHandler 构造修仙结局处理器。
func NewCultivationEndingHandler(
	convSvc *conversation.Service,
	chatSvc *chat.Service,
	stateSvc *state.Service,
	accountRepo *storage.AccountRepository,
	ident identity.Provider,
) *CultivationEndingHandler {
	return &CultivationEndingHandler{
		convSvc: convSvc, chatSvc: chatSvc, stateSvc: stateSvc,
		accountRepo: accountRepo, identity: ident,
	}
}

// Handle 处理 trigger_ending 动作：自动结束对话并广播结局事件。
func (h *CultivationEndingHandler) Handle(ctx context.Context, convID uuid.UUID, params map[string]any, emit realtime.EmitFunc) {
	endingType := getStringParam(params, "ending_type", "neutral")
	title := getStringParam(params, "title", "故事终章")
	summary := getStringParam(params, "summary", "")

	// 获取当前状态，提取灵石数量和角色信息。
	currentState, _ := h.stateSvc.Get(ctx, convID)
	spiritStones := extractSpiritStones(currentState)
	characterName := extractStringFromState(currentState, "profile.name")
	cultivationLevel := extractStringFromState(currentState, "profile.cultivation_level")

	// 计算存活回合数（用户消息数量）。
	messages, _ := h.chatSvc.History(ctx, convID, false)
	survivalTurns := 0
	for _, msg := range messages {
		if msg.Role == storage.RoleUser {
			survivalTurns++
		}
	}

	// 记录轮回历史（playerID 由身份 Provider 解析，保持与 conversation.Service 一致）。
	playerID := h.identity.CurrentPlayerID(ctx)
	history := &storage.TrialHistory{
		PlayerID:           playerID,
		ConversationID:     convID.String(),
		EndingType:         endingType,
		SpiritStonesEarned: 0,
		CharacterName:      characterName,
		CultivationLevel:   cultivationLevel,
		SurvivalTurns:      survivalTurns,
	}

	log := logging.Named("ending").With(zap.String("conversation_id", convID.String()))

	// 根据结局类型更新账号统计。记账失败会导致灵石/胜负未落库，必须留痕（不中断结局）。
	if endingType == "good" {
		// 成功结局：记录灵石。
		history.SpiritStonesEarned = spiritStones
		if h.accountRepo != nil {
			if err := h.accountRepo.RecordTrialSuccess(playerID, spiritStones); err != nil {
				log.Error("记录成功结局账号统计失败", zap.Int64("spirit_stones", spiritStones), zap.Error(err))
			}
		}
	} else {
		// 失败结局：灵石归零。
		history.SpiritStonesEarned = 0
		if h.accountRepo != nil {
			if err := h.accountRepo.RecordTrialFailure(playerID); err != nil {
				log.Error("记录失败结局账号统计失败", zap.Error(err))
			}
		}
	}

	// 保存历史记录。
	if h.accountRepo != nil {
		if err := h.accountRepo.AddTrialHistory(history); err != nil {
			log.Error("保存轮回历史失败", zap.Error(err))
		}
	}

	// 结束对话。失败时记录日志但仍广播结局事件——结局在叙事上已经发生，
	// 不应因状态位写入失败而让前端收不到结局弹窗（对话锁自愈会兜底处理残留状态）。
	if _, err := h.convSvc.End(ctx, convID); err != nil {
		log.Warn("结束对话失败，仍广播结局事件", zap.Error(err))
	}

	// 广播结局事件。
	emit(realtime.Event{
		Type:           realtime.TypeEndingTriggered,
		ConversationID: convID.String(),
		EndingType:     endingType,
		EndingTitle:    title,
		EndingSummary:  summary,
		SpiritStones:   spiritStones,
		SurvivalTurns:  survivalTurns,
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
