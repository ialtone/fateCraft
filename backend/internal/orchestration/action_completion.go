package orchestration

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/challenge"
	"github.com/ialtone/fatecraft/internal/chat"
	"github.com/ialtone/fatecraft/internal/gameplay"
	"github.com/ialtone/fatecraft/internal/identity"
	"github.com/ialtone/fatecraft/internal/state"
	"github.com/ialtone/fatecraft/internal/storage"
)

// CompletionHandler implements the generic challenge completion action. The
// challenge runtime selects the verifier and reward calculators; this handler
// only freezes trusted state and forwards the requested outcome key.
type CompletionHandler struct {
	chatSvc   *chat.Service
	stateSvc  *state.Service
	challenge *challenge.Service
	identity  identity.Provider
}

func NewCompletionHandler(chatSvc *chat.Service, stateSvc *state.Service, challengeSvc *challenge.Service, ident identity.Provider) *CompletionHandler {
	return &CompletionHandler{chatSvc: chatSvc, stateSvc: stateSvc, challenge: challengeSvc, identity: ident}
}

// PrepareInTx 在工具事务内冻结可信状态与回合数，并用事务仓储做结算预校验。
// 事务内所有读取都走同一连接，不再触碰全局连接池。
func (h *CompletionHandler) PrepareInTx(ctx context.Context, tx *agentToolTx, convID uuid.UUID, contract *gameplay.RuntimeContract, params map[string]any) (*preparedCompletion, error) {
	if tx == nil || contract == nil {
		return nil, errors.New("结算事务未配置")
	}
	outcomeKey := getStringParam(params, "outcome_key", "")
	summary := getStringParam(params, "summary", "")
	trustedState, err := state.GetInTx(ctx, tx.states, convID, contract.StateSchema)
	if err != nil {
		return nil, err
	}
	messages, err := tx.messages.ListChat(ctx, convID, false)
	if err != nil {
		return nil, err
	}
	turnCount := 0
	for _, message := range messages {
		if message.Role == storage.RoleUser {
			turnCount++
		}
	}
	playerID, err := uuid.Parse(h.identity.CurrentPlayerID(ctx))
	if err != nil {
		return nil, errors.New("当前用户身份无效")
	}
	input := challenge.SettlementInput{
		ConversationID: convID,
		PlayerID:       playerID,
		OutcomeKey:     outcomeKey,
		Summary:        summary,
		TrustedState:   trustedState,
		TurnCount:      turnCount,
	}
	if err := h.challenge.ValidateSettlementWithRepository(ctx, tx.challenges, input); err != nil {
		return nil, err
	}
	return &preparedCompletion{handler: h, input: input}, nil
}

type preparedCompletion struct {
	handler *CompletionHandler
	input   challenge.SettlementInput
}
