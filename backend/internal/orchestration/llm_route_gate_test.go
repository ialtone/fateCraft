package orchestration

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/conversation"
	"github.com/ialtone/fatecraft/internal/identity"
	"github.com/ialtone/fatecraft/internal/realtime"
	"github.com/ialtone/fatecraft/internal/storage"
)

// gateRepoStub 只实现路由闸门测试需要的读方法，其余返回零值。
// StartGeneration 被调用即记录，断言闸门拦截时它不得被调用。
type gateRepoStub struct {
	conversation *storage.Conversation
	startCalls   int
}

func (r *gateRepoStub) Create(context.Context, *storage.Conversation) error { return nil }
func (r *gateRepoStub) CreateWithState(context.Context, *storage.Conversation, *storage.ConversationState) error {
	return nil
}
func (r *gateRepoStub) Get(_ context.Context, id uuid.UUID) (*storage.Conversation, error) {
	if r.conversation == nil || r.conversation.ID != id {
		return nil, storage.ErrNotFound
	}
	return r.conversation, nil
}
func (r *gateRepoStub) List(context.Context, string, []string, string) ([]storage.Conversation, error) {
	return nil, nil
}
func (r *gateRepoStub) ListPage(context.Context, string, []string, string, int) ([]storage.Conversation, error) {
	return nil, nil
}
func (r *gateRepoStub) UpdateFields(context.Context, uuid.UUID, map[string]any) error { return nil }
func (r *gateRepoStub) StartGeneration(context.Context, uuid.UUID, storage.GenerationRequest) (*storage.Conversation, *storage.ConversationGenerationRun, bool, error) {
	r.startCalls++
	return nil, nil, false, nil
}
func (r *gateRepoStub) FinishGeneration(context.Context, uuid.UUID, string, string) error {
	return nil
}
func (r *gateRepoStub) HeartbeatGeneration(context.Context, uuid.UUID) error { return nil }
func (r *gateRepoStub) FailActiveGeneration(context.Context, uuid.UUID, string) error {
	return nil
}
func (r *gateRepoStub) CancelGeneration(context.Context, uuid.UUID, string) error { return nil }
func (r *gateRepoStub) TransitionLifecycle(context.Context, uuid.UUID, string, map[string]any) error {
	return nil
}
func (r *gateRepoStub) GenerationRequestExists(context.Context, uuid.UUID, uuid.UUID) (bool, error) {
	return false, nil
}

type gateRouteStub struct{ route string }

func (s gateRouteStub) EffectiveRoute(context.Context, string) (string, error) {
	return s.route, nil
}

func TestGenerateRouteMismatchEmitsNoticeWithoutStartingRun(t *testing.T) {
	playerID := uuid.New()
	convID := uuid.New()
	provider := identity.NewContext()
	ctx := identity.WithPlayerID(context.Background(), playerID.String())
	repo := &gateRepoStub{conversation: &storage.Conversation{
		ID: convID, PlayerID: playerID.String(), Status: storage.StatusActive,
		LLMRoute: storage.LLMRouteServer,
	}}
	convSvc := conversation.NewService(repo, nil, nil, nil, provider)
	gen := NewGenerator(GeneratorDeps{
		Conv: convSvc, Identity: provider, Routes: gateRouteStub{route: storage.LLMRouteClient},
	})
	var emitted []realtime.Event
	gen.Generate(ctx, convID, uuid.New(), "你好", func(event realtime.Event) {
		emitted = append(emitted, event)
	})
	if repo.startCalls != 0 {
		t.Fatalf("StartGeneration was called %d times, want 0", repo.startCalls)
	}
	if len(emitted) != 1 || emitted[0].Type != realtime.TypeNotice {
		t.Fatalf("emitted events = %#v, want single notice", emitted)
	}
}

func TestGenerateRouteMatchPassesGate(t *testing.T) {
	playerID := uuid.New()
	convID := uuid.New()
	provider := identity.NewContext()
	ctx := identity.WithPlayerID(context.Background(), playerID.String())
	repo := &gateRepoStub{conversation: &storage.Conversation{
		ID: convID, PlayerID: playerID.String(), Status: storage.StatusActive,
		LLMRoute: storage.LLMRouteClient,
	}}
	convSvc := conversation.NewService(repo, nil, nil, nil, provider)
	gen := NewGenerator(GeneratorDeps{
		Conv: convSvc, Identity: provider, Routes: gateRouteStub{route: storage.LLMRouteClient},
	})
	// 路由一致时闸门放行，进入 StartGeneration（桩返回空 run，走“生成中”分支）。
	var emitted []realtime.Event
	gen.Generate(ctx, convID, uuid.New(), "你好", func(event realtime.Event) {
		emitted = append(emitted, event)
	})
	if repo.startCalls != 1 {
		t.Fatalf("StartGeneration was called %d times, want 1", repo.startCalls)
	}
}
