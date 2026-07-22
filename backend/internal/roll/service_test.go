package roll

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/storage"
)

type challengeHistoryStub struct {
	hasSuccess bool
	err        error
}

func (*challengeHistoryStub) Append(context.Context, uuid.UUID, string, bool) error { return nil }

func (*challengeHistoryStub) Recent(context.Context, uuid.UUID, int) ([]storage.RollHistoryEntry, error) {
	return nil, nil
}

func (s *challengeHistoryStub) HasSuccess(context.Context, uuid.UUID) (bool, error) {
	return s.hasSuccess, s.err
}

func TestHasSuccessfulChallenge(t *testing.T) {
	convID := uuid.New()
	wantErr := errors.New("history unavailable")

	tests := []struct {
		name    string
		service *Service
		want    bool
		wantErr error
	}{
		{name: "missing service is not successful", service: nil},
		{name: "missing history is not successful", service: NewService(nil, nil)},
		{name: "failed challenge", service: NewService(nil, &challengeHistoryStub{})},
		{name: "successful challenge", service: NewService(nil, &challengeHistoryStub{hasSuccess: true}), want: true},
		{name: "storage error", service: NewService(nil, &challengeHistoryStub{err: wantErr}), wantErr: wantErr},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.service.HasSuccessfulChallenge(context.Background(), convID)
			if got != tt.want {
				t.Fatalf("HasSuccessfulChallenge() = %v, want %v", got, tt.want)
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("HasSuccessfulChallenge() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}
