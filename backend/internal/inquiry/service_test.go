package inquiry

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/storage"
	"github.com/ialtone/fatecraft/internal/tokenizer"
)

func TestParseAnswerRequiresExactContractAndKnownSources(t *testing.T) {
	sources := []Source{{ID: "source-1", Category: "character", Title: "状态"}}
	answer, err := parseAnswer(`{"answer":"当前状态正常","source_ids":["source-1"],"cannot_answer":false}`, sources)
	if err != nil || answer.Answer == "" {
		t.Fatalf("valid answer rejected: %+v %v", answer, err)
	}
	tests := []string{
		`{"answer":"猜测","source_ids":[],"cannot_answer":false}`,
		`{"answer":"猜测","source_ids":["source-2"],"cannot_answer":false}`,
		`{"answer":"未知","source_ids":[]}`,
		`{"answer":"x","answer":"y","source_ids":["source-1"],"cannot_answer":false}`,
		`{"answer":"x","source_ids":["source-1"],"cannot_answer":false,"action":{}}`,
	}
	for _, raw := range tests {
		if _, err := parseAnswer(raw, sources); !errors.Is(err, ErrInvalidResponse) {
			t.Fatalf("expected invalid response for %s, got %v", raw, err)
		}
	}
}

func TestPublicHistoryKeepsNewestCompletedMessagesWithinBudget(t *testing.T) {
	rows := []storage.CharacterInquiryMessage{
		{Role: storage.RoleUser, Content: strings.Repeat("旧", 2000), Status: "completed"},
		{Role: storage.RoleAssistant, Content: "失败内容", Status: "failed"},
		{Role: storage.RoleUser, Content: "最近问题", Status: "completed"},
		{Role: storage.RoleAssistant, Content: "最近回答", Status: "completed"},
	}
	history := publicHistory(rows, tokenizer.NewHeuristic(), 100)
	if len(history) != 2 || history[0]["content"] != "最近问题" || history[1]["content"] != "最近回答" {
		t.Fatalf("history = %#v", history)
	}
}

func TestInquiryConcurrencyLimitsConversationAndPlayer(t *testing.T) {
	service := &Service{
		processing: map[uuid.UUID]activeInquiry{}, activeUsers: map[uuid.UUID]int{},
		slots: make(chan struct{}, maxConcurrentInquiries),
	}
	playerID := uuid.New()
	conversationID := uuid.New()
	_, cancel := context.WithCancel(context.Background())
	if !service.begin(conversationID, playerID, cancel) {
		t.Fatal("first inquiry should acquire a slot")
	}
	_, duplicateCancel := context.WithCancel(context.Background())
	if service.begin(conversationID, uuid.New(), duplicateCancel) {
		t.Fatal("same conversation acquired twice")
	}
	duplicateCancel()
	_, userCancel := context.WithCancel(context.Background())
	if service.begin(uuid.New(), playerID, userCancel) {
		t.Fatal("same player acquired two inquiry slots")
	}
	userCancel()
	service.end(conversationID, playerID)
	if len(service.slots) != 0 || len(service.processing) != 0 || len(service.activeUsers) != 0 {
		t.Fatalf("resources were not released: slots=%d processing=%d users=%d", len(service.slots), len(service.processing), len(service.activeUsers))
	}
}

func TestProjectionSourceUsesPublicTitle(t *testing.T) {
	sources := appendProjectionSources(nil, map[string]any{"internal_inventory_key": map[string]any{"count": 1}}, tokenizer.NewHeuristic())
	if len(sources) != 1 || strings.Contains(sources[0].Title, "internal_inventory_key") {
		t.Fatalf("source title leaked internal key: %#v", sources)
	}
}

func TestProjectionSourceLabelsRealmRulesAsGameplayRules(t *testing.T) {
	sources := appendProjectionSources(nil, map[string]any{"realm_system": []map[string]any{{"name": "练气"}}}, tokenizer.NewHeuristic())
	if len(sources) != 1 || sources[0].Category != "rules" || sources[0].Title != "当前玩法规则" {
		t.Fatalf("realm source = %#v", sources)
	}
}

func TestAuthoritativeSourcesIncludesExplicitGameplayRules(t *testing.T) {
	sources := authoritativeSources(&storage.InquirySnapshotData{
		State: storage.JSONMap{"profile": map[string]any{"name": "韩立"}},
		Rules: []storage.InquiryRuleSource{{Title: "共同修仙境界规范", Content: "凡人、练气、筑基"}},
	})
	if len(sources) != 2 || sources[1].Category != "rules" || sources[1].Title != "共同修仙境界规范" {
		t.Fatalf("authoritative sources = %#v", sources)
	}
}

func TestProjectionSourcesPrioritizeRulesBeforeLargeOptionalSections(t *testing.T) {
	sources := appendProjectionSources(nil, map[string]any{
		"world":        map[string]any{"name": "人间界"},
		"realm_system": []map[string]any{{"name": "凡人"}, {"name": "练气"}},
		"inventory":    []map[string]any{{"name": "灵石"}},
	}, tokenizer.NewHeuristic())
	if len(sources) != 3 || sources[0].Category != "rules" {
		t.Fatalf("projection source order = %#v", sources)
	}
}

func TestSameSnapshotVersionIncludesStateRevision(t *testing.T) {
	conversationID := uuid.New()
	firstRevision := uuid.New()
	secondRevision := uuid.New()
	snapshot := &storage.InquirySnapshotData{
		Conversation: storage.Conversation{ID: conversationID, BranchEpoch: 3},
		Revision:     8, StateRevisionID: &firstRevision,
	}
	if !sameSnapshotVersion(storage.InquiryVersion{BranchEpoch: 3, Revision: 8, StateRevisionID: &firstRevision}, snapshot) {
		t.Fatal("identical snapshot version was marked stale")
	}
	if sameSnapshotVersion(storage.InquiryVersion{BranchEpoch: 3, Revision: 8, StateRevisionID: &secondRevision}, snapshot) {
		t.Fatal("state-only revision change was not detected")
	}
}
