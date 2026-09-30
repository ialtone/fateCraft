package campaign

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/challenge"
	"github.com/ialtone/fatecraft/internal/gameplay"
	"github.com/ialtone/fatecraft/internal/identity"
	"github.com/ialtone/fatecraft/internal/llmsettings"
	"github.com/ialtone/fatecraft/internal/storage"
)

type campaignRepoStub struct {
	mu           sync.Mutex
	row          storage.Campaign
	created      *storage.Campaign
	conversation *storage.Conversation
	initial      storage.JSONMap
	commits      map[string]*storage.CampaignActionResult
	lastIn       storage.CommitCampaignActionInput
	domain       *storage.CampaignDomainState
}

func (r *campaignRepoStub) Create(_ context.Context, row *storage.Campaign, _ ...storage.CampaignInitialization) error {
	r.created = row
	r.row = *row
	return nil
}

func (r *campaignRepoStub) Get(_ context.Context, id uuid.UUID) (*storage.Campaign, error) {
	if r.row.ID != id {
		return nil, storage.ErrNotFound
	}
	copy := r.row
	return &copy, nil
}

func (r *campaignRepoStub) List(context.Context, uuid.UUID, []string) ([]storage.Campaign, error) {
	return []storage.Campaign{r.row}, nil
}

func (r *campaignRepoStub) ListEvents(context.Context, uuid.UUID, []string, int64, int) ([]storage.CampaignEvent, error) {
	return nil, nil
}

func (r *campaignRepoStub) RecentEvents(context.Context, uuid.UUID, []string, int) ([]storage.CampaignEvent, error) {
	return nil, nil
}

func (r *campaignRepoStub) ListConversations(context.Context, uuid.UUID) ([]storage.Conversation, error) {
	if r.conversation == nil {
		return nil, nil
	}
	return []storage.Conversation{*r.conversation}, nil
}

func (r *campaignRepoStub) DomainState(context.Context, uuid.UUID) (*storage.CampaignDomainState, error) {
	if r.domain != nil {
		return r.domain, nil
	}
	return &storage.CampaignDomainState{Definitions: map[string]storage.GameplayDefinition{}}, nil
}

func (r *campaignRepoStub) CreateConversation(_ context.Context, _ uuid.UUID, row *storage.Conversation, state storage.JSONMap, _ int) error {
	if r.conversation != nil {
		return storage.ErrCampaignHasConversation
	}
	r.conversation = row
	r.initial = state
	return nil
}

func (r *campaignRepoStub) CommitAction(
	_ context.Context,
	in storage.CommitCampaignActionInput,
	execute func(*storage.CampaignActionStore, storage.Campaign) (storage.CampaignActionOutcome, error),
) (*storage.CampaignActionResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastIn = in
	if r.commits == nil {
		r.commits = map[string]*storage.CampaignActionResult{}
	}
	if previous := r.commits[in.IdempotencyKey]; previous != nil {
		copy := *previous
		copy.Replayed = true
		return &copy, nil
	}
	if r.row.Status != storage.CampaignStatusActive {
		return nil, storage.ErrCampaignNotActive
	}
	if r.row.Revision != in.ExpectedRevision {
		return nil, storage.ErrCampaignRevisionConflict
	}
	outcome, err := execute(nil, r.row)
	if err != nil {
		return nil, err
	}
	r.row.Revision++
	if outcome.Status != nil {
		r.row.Status = *outcome.Status
	}
	if outcome.OutcomeKey != nil {
		r.row.OutcomeKey = *outcome.OutcomeKey
		completedAt := r.row.GameTime + outcome.GameTimeDelta
		r.row.CompletedAtGameTime = &completedAt
	}
	r.row.EventSequence += int64(len(outcome.Events))
	events := make([]storage.CampaignEvent, len(outcome.Events))
	for index, event := range outcome.Events {
		events[index] = storage.CampaignEvent{
			ID: uuid.New(), CampaignID: r.row.ID, Sequence: int64(index + 1),
			EventType: event.EventType, Visibility: event.Visibility,
		}
	}
	result := &storage.CampaignActionResult{Campaign: r.row, Events: events, Result: outcome.Result}
	r.commits[in.IdempotencyKey] = result
	copy := *result
	return &copy, nil
}

type challengeStub struct {
	selection *challenge.Selection
	contract  *gameplay.RuntimeContract
}

func (s challengeStub) ResolveSelection(context.Context, string, string, ...uuid.UUID) (*challenge.Selection, error) {
	return s.selection, nil
}

func (s challengeStub) RuntimeForVersion(context.Context, uuid.UUID, string) (*gameplay.RuntimeContract, error) {
	return s.contract, nil
}

type openingStub struct{ seeded *storage.Conversation }

func (s *openingStub) SeedOpening(_ context.Context, row *storage.Conversation, _ time.Time) {
	s.seeded = row
}

// routeStub 桩实现路由解析器，固定返回指定有效路由。
type routeStub struct {
	route string
	err   error
}

func (s routeStub) ReadyRoute(context.Context, string) (string, error) {
	return s.route, s.err
}

func testIdentityContext(playerID uuid.UUID) (identity.Context, context.Context) {
	provider := identity.NewContext()
	return provider, identity.WithPlayerID(context.Background(), playerID.String())
}

func campaignContract() *gameplay.RuntimeContract {
	return &gameplay.RuntimeContract{
		Mode: gameplay.RuntimeModeCampaign,
		Campaign: &gameplay.CampaignConfig{
			LifecycleHandler:  "cultivation.campaign_lifecycle",
			ProjectionHandler: "cultivation.campaign_projection",
			ContextPolicy:     "cultivation.campaign_context", ActionAllowlist: []string{"test.action"}, InitialLocationKey: "world.start",
			FieldOwnership: []gameplay.FieldOwnership{{Path: "character", Owner: gameplay.OwnerCampaign, ValueType: "object", Visibility: gameplay.VisibilityPlayer, StorageProjection: "campaign_characters"}},
			WorldClock:     gameplay.WorldClockConfig{CalendarKey: "world.default", MaxDueEventsPerTurn: 16},
		},
		StateSchema: gameplay.StateSchema{SchemaVersion: 1, Fields: []gameplay.StateField{{
			Path: "narrative.current_focus", Type: "string", Default: "", Visibility: gameplay.VisibilityPlayer,
			UpdatePolicy: gameplay.UpdateModel,
		}}},
	}
}

func TestCreatePinsCampaignSelectionAndRejectsChallengeMode(t *testing.T) {
	playerID := uuid.New()
	provider, ctx := testIdentityContext(playerID)
	versionID := uuid.New()
	contract := campaignContract()
	repo := &campaignRepoStub{}
	selection := &challenge.Selection{
		ChallengeName: "凡尘问道",
		Version:       storage.ChallengeVersion{ID: versionID, ContentHash: "hash"}, Contract: *contract,
	}
	service := NewService(repo, challengeStub{selection: selection, contract: contract}, nil, provider)
	created, err := service.Create(ctx, CreateInput{Title: "问道", ChallengeSlug: "campaign"})
	if err != nil {
		t.Fatal(err)
	}
	if created.PlayerID != playerID || created.ChallengeVersionID != versionID ||
		created.CurrentLocationKey != "world.start" || created.Status != storage.CampaignStatusActive {
		t.Fatalf("campaign selection was not pinned: %+v", created)
	}
	defaultTitle, err := service.Create(ctx, CreateInput{ChallengeSlug: "campaign"})
	if err != nil || defaultTitle.Title != selection.ChallengeName {
		t.Fatalf("default title = %q, err = %v", defaultTitle.Title, err)
	}

	selection.Contract.Mode = gameplay.RuntimeModeChallenge
	selection.Contract.Campaign = nil
	if _, err := service.Create(ctx, CreateInput{ChallengeSlug: "single"}); !errors.Is(err, ErrCampaignModeRequired) {
		t.Fatalf("challenge mode Create error = %v", err)
	}
}

func TestCreateConversationUsesPinnedVersionAndInitialState(t *testing.T) {
	playerID := uuid.New()
	provider, ctx := testIdentityContext(playerID)
	contract := campaignContract()
	repo := &campaignRepoStub{row: storage.Campaign{
		ID: uuid.New(), PlayerID: playerID, Title: "问道", Status: storage.CampaignStatusActive,
		ChallengeVersionID: uuid.New(), ChallengeHash: "hash",
	}}
	openings := &openingStub{}
	service := NewService(repo, challengeStub{contract: contract}, openings, provider)
	conversation, err := service.CreateConversation(ctx, repo.row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if conversation.CampaignID == nil || *conversation.CampaignID != repo.row.ID ||
		conversation.ChallengeVersionID != repo.row.ChallengeVersionID || conversation.Title != repo.row.Title {
		t.Fatalf("conversation did not inherit campaign: %+v", conversation)
	}
	if _, ok := gameplay.ValueAtPath(map[string]any(repo.initial), "narrative.current_focus"); !ok || openings.seeded != conversation {
		t.Fatalf("conversation initialization missing: state=%#v seeded=%v", repo.initial, openings.seeded != nil)
	}
	if _, err := service.CreateConversation(ctx, repo.row.ID); !errors.Is(err, storage.ErrCampaignHasConversation) {
		t.Fatalf("second CreateConversation error = %v", err)
	}
}

func TestExecuteActionIsIdempotentAndConflictsOnConcurrentRevision(t *testing.T) {
	playerID := uuid.New()
	provider, ctx := testIdentityContext(playerID)
	repo := &campaignRepoStub{row: storage.Campaign{
		ID: uuid.New(), PlayerID: playerID, Status: storage.CampaignStatusActive,
	}}
	contract := campaignContract()
	contract.Campaign.ActionAllowlist = []string{"test.advance"}
	service := NewService(repo, challengeStub{contract: contract}, nil, provider)
	var executions atomic.Int64
	registerTestAction(service, "test.advance", []string{"player"}, ActionHandlerFunc(func(context.Context, ActionExecution, map[string]any) (storage.CampaignActionOutcome, error) {
		executions.Add(1)
		return storage.CampaignActionOutcome{
			Events: []storage.CampaignEventDraft{{EventType: "test.advanced", Visibility: storage.CampaignVisibilityPlayer}},
			Result: storage.JSONMap{"ok": true},
		}, nil
	}))

	in := ActionInput{ActionID: "turn-1", Key: "test.advance", ExpectedRevision: 0, Arguments: map[string]any{}}
	first, err := service.ExecutePlayerAction(ctx, repo.row.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := service.ExecutePlayerAction(ctx, repo.row.ID, in)
	if err != nil || !replayed.Replayed || executions.Load() != 1 {
		t.Fatalf("idempotent replay failed: result=%+v err=%v executions=%d", replayed, err, executions.Load())
	}
	if first.Campaign.Revision != 1 {
		t.Fatalf("revision = %d, want 1", first.Campaign.Revision)
	}

	repo.row.Revision = 0
	repo.row.EventSequence = 0
	repo.commits = map[string]*storage.CampaignActionResult{}
	results := make(chan error, 2)
	for _, actionID := range []string{"concurrent-a", "concurrent-b"} {
		go func(id string) {
			_, callErr := service.ExecutePlayerAction(ctx, repo.row.ID, ActionInput{
				ActionID: id, Key: "test.advance", ExpectedRevision: 0, Arguments: map[string]any{},
			})
			results <- callErr
		}(actionID)
	}
	var succeeded, conflicted int
	for range 2 {
		err := <-results
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, storage.ErrCampaignRevisionConflict):
			conflicted++
		default:
			t.Fatalf("unexpected concurrent error: %v", err)
		}
	}
	if succeeded != 1 || conflicted != 1 {
		t.Fatalf("concurrent results: succeeded=%d conflicted=%d", succeeded, conflicted)
	}
}

func TestExecuteModelActionWithCommitterBindsSourceMessage(t *testing.T) {
	playerID := uuid.New()
	provider, ctx := testIdentityContext(playerID)
	repo := &campaignRepoStub{row: storage.Campaign{
		ID: uuid.New(), PlayerID: playerID, Status: storage.CampaignStatusActive,
	}}
	contract := campaignContract()
	contract.Campaign.ActionAllowlist = []string{"test.model"}
	service := NewService(repo, challengeStub{contract: contract}, nil, provider)
	sourceMessageID := uuid.New()
	registerTestAction(service, "test.model", []string{"model"}, ActionHandlerFunc(func(_ context.Context, execution ActionExecution, _ map[string]any) (storage.CampaignActionOutcome, error) {
		if execution.SourceMessageID == nil || *execution.SourceMessageID != sourceMessageID {
			t.Fatalf("execution source_message_id = %v", execution.SourceMessageID)
		}
		return storage.CampaignActionOutcome{
			Events: []storage.CampaignEventDraft{{EventType: "test.model", Visibility: storage.CampaignVisibilityPlayer}},
			Result: storage.JSONMap{"ok": true},
		}, nil
	}))
	_, err := service.ExecuteModelActionWithCommitter(ctx, repo, repo.row.ID, ActionInput{
		ActionID: "model-turn-1", Key: "test.model", ExpectedRevision: 0, Arguments: map[string]any{},
	}, &sourceMessageID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if repo.lastIn.SourceMessageID == nil || *repo.lastIn.SourceMessageID != sourceMessageID || repo.lastIn.ActorType != "model" {
		t.Fatalf("commit input = %+v", repo.lastIn)
	}
}

func TestExecuteActionRejectsUndeclaredActor(t *testing.T) {
	playerID := uuid.New()
	provider, ctx := testIdentityContext(playerID)
	repo := &campaignRepoStub{row: storage.Campaign{
		ID: uuid.New(), PlayerID: playerID, Status: storage.CampaignStatusActive,
	}}
	contract := campaignContract()
	contract.Campaign.ActionAllowlist = []string{"test.model-only"}
	service := NewService(repo, challengeStub{contract: contract}, nil, provider)
	var executions atomic.Int64
	registerTestAction(service, "test.model-only", []string{"model"}, ActionHandlerFunc(func(context.Context, ActionExecution, map[string]any) (storage.CampaignActionOutcome, error) {
		executions.Add(1)
		return storage.CampaignActionOutcome{
			Events: []storage.CampaignEventDraft{{EventType: "test.model-only", Visibility: storage.CampaignVisibilityPlayer}},
		}, nil
	}))

	_, err := service.ExecutePlayerAction(ctx, repo.row.ID, ActionInput{
		ActionID: "player-call", Key: "test.model-only", ExpectedRevision: 0, Arguments: map[string]any{},
	})
	if !errors.Is(err, ErrActionRejected) || executions.Load() != 0 {
		t.Fatalf("undeclared actor was not rejected before execution: err=%v executions=%d", err, executions.Load())
	}
}

func registerTestAction(service *Service, key string, actors []string, handler ActionHandler) {
	service.RegisterAction(RegisteredAction{
		Definition: ActionDefinition{
			Key: key, Description: "test action", Actors: actors,
			ArgumentSchema: storage.JSONMap{
				"type": "object", "required": []string{}, "properties": map[string]any{}, "additionalProperties": false,
			},
		},
		Capability: StaticCapabilityProvider{}, Handler: handler,
	})
}

func TestBoundedContextEventsKeepsNewestWithinBudget(t *testing.T) {
	events := []storage.CampaignEvent{
		{Sequence: 1, Payload: storage.JSONMap{"text": strings.Repeat("a", 100)}},
		{Sequence: 2, Payload: storage.JSONMap{"text": strings.Repeat("b", 100)}},
		{Sequence: 3, Payload: storage.JSONMap{"text": "newest"}},
	}
	selected := boundedContextEvents(events, 500)
	if len(selected) == 0 || selected[len(selected)-1].Sequence != 3 {
		t.Fatalf("newest event was not retained: %+v", selected)
	}
	encoded, err := json.Marshal(selected)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) > 500 {
		t.Fatalf("bounded events use %d bytes", len(encoded))
	}
}

func TestCreateSolidifiesEffectiveRoute(t *testing.T) {
	playerID := uuid.New()
	provider, ctx := testIdentityContext(playerID)
	contract := campaignContract()
	versionID := uuid.New()
	selection := &challenge.Selection{
		ChallengeName: "凡尘问道",
		Version:       storage.ChallengeVersion{ID: versionID, ContentHash: "hash"}, Contract: *contract,
	}
	service := NewService(&campaignRepoStub{}, challengeStub{selection: selection, contract: contract}, nil, provider)
	service.SetRouteResolver(routeStub{route: storage.LLMRouteClient})
	created, err := service.Create(ctx, CreateInput{Title: "问道", ChallengeSlug: "campaign"})
	if err != nil {
		t.Fatal(err)
	}
	if created.LLMRoute != storage.LLMRouteClient {
		t.Fatalf("campaign route = %q, want client", created.LLMRoute)
	}
	// 未注入解析器时默认 server，保持旧行为。
	legacy := NewService(&campaignRepoStub{}, challengeStub{selection: selection, contract: contract}, nil, provider)
	createdServer, err := legacy.Create(ctx, CreateInput{Title: "问道", ChallengeSlug: "campaign"})
	if err != nil {
		t.Fatal(err)
	}
	if createdServer.LLMRoute != storage.LLMRouteServer {
		t.Fatalf("campaign route = %q, want server", createdServer.LLMRoute)
	}
}

func TestMissingModelConfigurationDoesNotCreateCampaignOrConversation(t *testing.T) {
	playerID := uuid.New()
	provider, ctx := testIdentityContext(playerID)
	contract := campaignContract()
	selection := &challenge.Selection{Version: storage.ChallengeVersion{ID: uuid.New(), ContentHash: "hash"}, Contract: *contract}
	repo := &campaignRepoStub{row: storage.Campaign{
		ID: uuid.New(), PlayerID: playerID, Status: storage.CampaignStatusActive,
		ChallengeVersionID: selection.Version.ID, ChallengeHash: "hash", LLMRoute: storage.LLMRouteServer,
	}}
	service := NewService(repo, challengeStub{selection: selection, contract: contract}, nil, provider)
	service.SetRouteResolver(routeStub{err: llmsettings.ErrConfigurationIncomplete})
	if _, err := service.Create(ctx, CreateInput{ChallengeSlug: "campaign"}); !errors.Is(err, llmsettings.ErrConfigurationIncomplete) {
		t.Fatalf("Create error = %v", err)
	}
	if _, err := service.CreateConversation(ctx, repo.row.ID); !errors.Is(err, llmsettings.ErrConfigurationIncomplete) {
		t.Fatalf("CreateConversation error = %v", err)
	}
	if repo.created != nil || repo.conversation != nil {
		t.Fatal("missing configuration created game state")
	}
}

func TestCreateConversationRequiresMatchingRoute(t *testing.T) {
	playerID := uuid.New()
	provider, ctx := testIdentityContext(playerID)
	contract := campaignContract()
	newService := func(row storage.Campaign, route string) *Service {
		service := NewService(&campaignRepoStub{row: row}, challengeStub{contract: contract}, &openingStub{}, provider)
		service.SetRouteResolver(routeStub{route: route})
		return service
	}
	newRow := func(route string) storage.Campaign {
		return storage.Campaign{
			ID: uuid.New(), PlayerID: playerID, Title: "问道", Status: storage.CampaignStatusActive,
			ChallengeVersionID: uuid.New(), ChallengeHash: "hash", LLMRoute: route,
		}
	}
	// Campaign 路由与当前有效路由一致：对话继承同一路由。
	row := newRow(storage.LLMRouteClient)
	service := newService(row, storage.LLMRouteClient)
	conversation, err := service.CreateConversation(ctx, row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if conversation.LLMRoute != storage.LLMRouteClient {
		t.Fatalf("conversation route = %q, want client", conversation.LLMRoute)
	}
	// 用户切到另一路由后：旧 Campaign 的对话不可创建。
	oldRow := newRow(storage.LLMRouteServer)
	mismatched := newService(oldRow, storage.LLMRouteClient)
	if _, err := mismatched.CreateConversation(ctx, oldRow.ID); !errors.Is(err, storage.ErrLLMRouteMismatch) {
		t.Fatalf("CreateConversation error = %v, want ErrLLMRouteMismatch", err)
	}
}
