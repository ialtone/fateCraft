package challenge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	gameplay "github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/storage"
)

type ModulePlayerActionInput struct {
	RequestID        uuid.UUID      `json:"request_id"`
	BranchEpoch      int64          `json:"branch_epoch"`
	ExpectedRevision int64          `json:"expected_revision"`
	Key              string         `json:"key"`
	Arguments        map[string]any `json:"arguments"`
}

func (s *Service) ExecuteModulePlayerAction(ctx context.Context, id, playerID uuid.UUID, in ModulePlayerActionInput) (storage.JSONMap, error) {
	if in.RequestID == uuid.Nil || in.BranchEpoch < 0 || in.ExpectedRevision < 0 || !gameplay.ValidStableKey(in.Key) {
		return nil, fmt.Errorf("%w: 玩法动作请求无效", ErrInvalidChallenge)
	}
	conv, err := s.repo.Conversation(ctx, id)
	if err != nil {
		return nil, err
	}
	if conv.PlayerID != playerID.String() {
		return nil, storage.ErrNotFound
	}
	content, err := s.ModuleContentForVersion(ctx, conv.ChallengeVersionID, conv.ChallengeHash)
	if err != nil {
		return nil, err
	}
	module, err := s.registry.Module(content.Runtime.Engine)
	if err != nil {
		return nil, err
	}
	if module.Runtime == nil && content.Runtime.Mode != gameplay.RuntimeModeCampaign {
		return nil, fmt.Errorf("%w: 玩法动作不可用", ErrInvalidChallenge)
	}
	label := ""
	if module.Runtime != nil {
		for _, action := range module.Runtime.Actions() {
			if action.Key == in.Key {
				label = action.Label
			}
		}
	} else {
		for _, key := range module.CampaignActions {
			if key == in.Key {
				label = key
			}
		}
	}
	if label == "" {
		return nil, gameplay.Reject("action.unknown", "操作不存在")
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	if len(raw) > 8192 {
		return nil, gameplay.Reject("action.too_large", "动作参数过大")
	}
	hash := sha256.Sum256(raw)
	return s.repo.CommitModuleAction(ctx, storage.CommitModuleActionInput{ConversationID: id, PlayerID: playerID, RequestID: in.RequestID, BranchEpoch: in.BranchEpoch, ActionKey: in.Key, ArgumentsHash: hex.EncodeToString(hash[:]), Label: label}, func(bound context.Context, states *storage.StateRepo, repo *storage.ChallengeRepository, locked *storage.Conversation) (storage.JSONMap, error) {
		result, err := s.ExecuteModuleInTx(bound, states, repo, locked, content, in.ExpectedRevision, "player", in.Key, in.Arguments)
		if err != nil {
			return nil, err
		}
		raw, err := json.Marshal(result)
		if err != nil {
			return nil, err
		}
		var data storage.JSONMap
		err = json.Unmarshal(raw, &data)
		return data, err
	})
}
