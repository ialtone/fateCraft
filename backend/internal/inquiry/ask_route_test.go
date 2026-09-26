package inquiry

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/identity"
	"github.com/ialtone/fatecraft/internal/storage"
)

// askRepoStub 只实现路由闸门测试需要的读方法，并记录限流与写入调用。
type askRepoStub struct {
	snapshot   *storage.InquirySnapshotData
	rateChecks int
	appends    int
}

func (r *askRepoStub) ReadSnapshot(context.Context, uuid.UUID, uuid.UUID) (*storage.InquirySnapshotData, error) {
	return r.snapshot, nil
}

func (r *askRepoStub) CurrentVersion(context.Context, uuid.UUID) (storage.InquiryVersion, error) {
	return storage.InquiryVersion{}, nil
}

func (r *askRepoStub) Thread(context.Context, uuid.UUID, uuid.UUID) (*storage.CharacterInquiryThread, error) {
	return &storage.CharacterInquiryThread{}, nil
}

func (r *askRepoStub) Append(context.Context, *storage.CharacterInquiryMessage) error {
	r.appends++
	return nil
}

func (r *askRepoStub) List(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, int) ([]storage.CharacterInquiryMessage, error) {
	return nil, nil
}

func (r *askRepoStub) Clear(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error { return nil }

func (r *askRepoStub) ConsumeRateLimit(context.Context, uuid.UUID, time.Time, time.Duration, int) (bool, error) {
	r.rateChecks++
	return true, nil
}

type askSettingsStub struct{ route string }

func (s askSettingsStub) Get(context.Context, string) (*storage.LLMSettings, error) {
	return &storage.LLMSettings{LLMRoute: s.route, Model: "test-model", MaxResponseTokens: 2048}, nil
}

func TestAskRejectsRouteMismatchWithoutSideEffects(t *testing.T) {
	playerID := uuid.New()
	convID := uuid.New()
	provider := identity.NewContext()
	ctx := identity.WithPlayerID(context.Background(), playerID.String())
	repo := &askRepoStub{
		snapshot: &storage.InquirySnapshotData{
			Conversation: storage.Conversation{ID: convID, LLMRoute: storage.LLMRouteServer},
		},
	}
	service := NewService(repo, nil, nil, askSettingsStub{route: storage.LLMRouteClient}, provider, nil, nil)
	_, err := service.Ask(ctx, convID, "你是谁？")
	if !errors.Is(err, storage.ErrLLMRouteMismatch) {
		t.Fatalf("Ask error = %v, want ErrLLMRouteMismatch", err)
	}
	if repo.rateChecks != 0 || repo.appends != 0 {
		t.Fatalf("mismatched ask consumed rate limit or wrote messages: rate=%d appends=%d", repo.rateChecks, repo.appends)
	}
}
