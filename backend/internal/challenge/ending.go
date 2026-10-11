package challenge

import (
	"context"
	"github.com/ialtone/fatecraft/internal/storage"
)

func (s *Service) Completion(ctx context.Context, conv storage.Conversation) (*storage.CompletionView, error) {
	return s.repo.Completion(ctx, conv)
}
func (s *Service) EndingProposal(ctx context.Context, conv storage.Conversation) (*storage.EndingProposal, error) {
	return s.repo.EndingProposal(ctx, conv)
}
