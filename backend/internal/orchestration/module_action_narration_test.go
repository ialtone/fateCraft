package orchestration

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/conversation"
	gameplay "github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/identity"
	"github.com/ialtone/fatecraft/internal/realtime"
	"github.com/ialtone/fatecraft/internal/storage"
)

type busyModuleNarrationRepo struct{ gateRepoStub }

func (r *busyModuleNarrationRepo) StartGeneration(context.Context, uuid.UUID, storage.GenerationRequest) (*storage.Conversation, *storage.ConversationGenerationRun, bool, error) {
	r.startCalls++
	return nil, nil, false, storage.ErrGenerationInProgress
}

func TestModuleActionNarrationSkipsBusyDurableLease(t *testing.T) {
	playerID, convID := uuid.New(), uuid.New()
	provider := identity.NewContext()
	ctx := identity.WithPlayerID(context.Background(), playerID.String())
	repo := &busyModuleNarrationRepo{gateRepoStub: gateRepoStub{conversation: &storage.Conversation{
		ID: convID, PlayerID: playerID.String(), Status: storage.StatusActive, LLMRoute: storage.LLMRouteServer,
	}}}
	gen := NewGenerator(GeneratorDeps{
		Conv: conversation.NewService(repo, nil, nil, nil, provider), Identity: provider,
		Routes: gateRouteStub{route: storage.LLMRouteServer},
	})
	gen.GenerateForModuleAction(ctx, convID, uuid.New(), func(event realtime.Event) {
		t.Errorf("busy narration should be silent, emitted %#v", event)
	})
	if repo.startCalls != 1 {
		t.Fatalf("StartGeneration calls = %d", repo.startCalls)
	}
}

func TestFoldActionReceiptsReplacesCanonicalTextAndPreservesIdentity(t *testing.T) {
	id := uuid.New()
	history := []storage.ChatMessage{
		{ID: id, Role: storage.RoleSystem, Content: "已执行操作：闭关修行", TurnSeq: 3,
			ContentBlocks: storage.JSONDoc(`[{"type":"text","text":"已执行操作：闭关修行"}]`)},
		{ID: uuid.New(), Role: storage.RoleSystem, Content: "已执行操作：闭关修行", TurnSeq: 4},
		{ID: uuid.New(), Role: storage.RoleAssistant, Content: "叙事"},
	}
	folded := foldSystemActionReceipts(history)
	if len(folded) != 2 || folded[0].Content != "其间执行了：闭关修行 ×2" {
		t.Fatalf("incorrect fold: %#v", folded)
	}
	if len(folded[0].ContentBlocks) != 0 || folded[0].ID != id || folded[0].TurnSeq != 3 {
		t.Fatal("fold retained stale provider text or lost turn identity")
	}
	if history[0].Content != "已执行操作：闭关修行" || len(history[0].ContentBlocks) == 0 {
		t.Fatal("fold mutated stored source history")
	}
}

func TestModuleNarrationRecordsCommittedToolsAndDeduplicatesReplays(t *testing.T) {
	cache := &attemptAssembleCache{}
	action := testNarrativeContext().Actions[0]
	outcome := agentToolOutcome{Status: storage.ToolInvocationRejected, Mutating: true,
		Result: storage.JSONMap{"data": map[string]any{"narrative": action}}}
	if err := cache.recordModuleAction(outcome, action.ActionKey); err != nil || len(cache.moduleActions) != 0 {
		t.Fatalf("rejected action recorded: %v %#v", err, cache.moduleActions)
	}
	outcome.Status = storage.ToolInvocationCommitted
	for i := 0; i < 2; i++ {
		if err := cache.recordModuleAction(outcome, action.ActionKey); err != nil {
			t.Fatal(err)
		}
	}
	if len(cache.moduleActions) != 1 || cache.moduleActions[0].ActionID != action.ActionID {
		t.Fatalf("replayed tool duplicated: %#v", cache.moduleActions)
	}
}

func TestModuleNarrationRecoversBoundedFeedback(t *testing.T) {
	feedback, err := gameplay.NarrativeFeedback("trial.practice", 1, 2,
		map[string]any{"progress": 30}, map[string]any{"progress": 50},
		[]string{"progress"}, map[string]any{"success": true}, false)
	if err != nil {
		t.Fatal(err)
	}
	cache := &attemptAssembleCache{}
	outcome := agentToolOutcome{Status: storage.ToolInvocationCommitted, Mutating: true,
		Result: storage.JSONMap{"data": map[string]any{"revision": 2, "narrative_feedback": feedback}}}
	if err := cache.recordModuleAction(outcome, "trial.practice"); err != nil {
		t.Fatal(err)
	}
	if len(cache.moduleActions) != 1 || cache.moduleActions[0].ActionKey != "trial.practice" {
		t.Fatalf("bounded tool facts missing: %#v", cache.moduleActions)
	}
}

func TestLaterCommittedActionSupersedesEarlierPreservedFacts(t *testing.T) {
	narrative := &gameplay.TurnNarrativeContext{ChallengeMode: true, Actions: []gameplay.ActionResult{
		{ActionKey: "trial.breakthrough", GoalStatus: "failed", Preserved: []gameplay.FactRef{{Path: "character.realm_name"}}},
		{ActionKey: "trial.breakthrough", GoalStatus: "success", Changes: []gameplay.FactChange{{Path: "character.realm_name", Before: "炼气", After: "筑基"}}},
	}}
	if status, detail := ValidateTurnNarrative("这一次突破成功。", narrative, BuildTurnEvidence(narrative, nil, nil, true), nil); status != "passed" {
		t.Fatalf("earlier action overrode later change: %#v", detail)
	}
}
