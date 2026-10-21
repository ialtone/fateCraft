package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
)

type PlayerCampaignExecutor func(context.Context, *CampaignRepository, *RollHistoryRepo) (*CampaignActionResult, error)

// Player actions use the same turn/checkpoint journal as narrative tools.
// Lock order is conversation, then Campaign, for both execution paths.
func (r *CampaignRepository) CommitPlayerCampaignAction(ctx context.Context, in CommitCampaignActionInput, label string, arguments JSONMap, execute PlayerCampaignExecutor) (*CampaignActionResult, error) {
	conversations, err := r.ListConversations(ctx, in.CampaignID)
	if err != nil {
		return nil, err
	}
	if len(conversations) == 0 {
		// A campaign may be abandoned before its first conversation exists, or
		// after the only conversation was deleted. There is no turn journal to
		// bind in that case, so commit directly against campaign state.
		return execute(ctx, r, NewRollHistoryRepo(r.db))
	}
	if len(conversations) != 1 {
		return nil, ErrNotFound
	}
	conv := conversations[0]
	raw, err := json.Marshal(struct {
		Key       string
		Revision  int64
		Arguments JSONMap
	}{in.ActionKey, in.ExpectedRevision, arguments})
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(raw)
	result, err := NewChallengeRepository(r.db).CommitModuleAction(ctx, CommitModuleActionInput{ConversationID: conv.ID, PlayerID: in.PlayerID, RequestID: uuid.NewSHA1(in.CampaignID, []byte(in.IdempotencyKey)), BranchEpoch: conv.BranchEpoch, ActionKey: in.ActionKey, ArgumentsHash: hex.EncodeToString(hash[:]), Label: label}, func(bound context.Context, _ *StateRepo, repo *ChallengeRepository, locked *Conversation) (JSONMap, error) {
		committed, err := execute(bound, repo.CampaignActions(), NewRollHistoryRepo(repo.db))
		if err != nil {
			return nil, err
		}
		raw, err := json.Marshal(committed)
		if err != nil {
			return nil, err
		}
		var data JSONMap
		err = json.Unmarshal(raw, &data)
		return data, err
	})
	if err != nil {
		return nil, err
	}
	raw, err = json.Marshal(result)
	if err != nil {
		return nil, err
	}
	var committed CampaignActionResult
	err = json.Unmarshal(raw, &committed)
	return &committed, err
}
