package storage

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/google/uuid"
)

type EndingProposal struct {
	ID             string         `json:"id"`
	ConversationID uuid.UUID      `json:"conversation_id"`
	BranchEpoch    int64          `json:"branch_epoch"`
	Revision       int64          `json:"revision"`
	Key            string         `json:"key"`
	Arguments      map[string]any `json:"arguments"`
	Label          string         `json:"label"`
	Available      bool           `json:"available"`
	Reason         string         `json:"reason,omitempty"`
	Metadata       map[string]any `json:"metadata"`
}

func (r *ChallengeRepository) EndingProposal(ctx context.Context, conv Conversation) (*EndingProposal, error) {
	if conv.Status != StatusActive {
		return nil, nil
	}
	var raw []byte
	err := r.db.WithContext(ctx).Raw(`SELECT a.result_json->'data' FROM agent_tool_invocations a
		JOIN conversation_turns t ON t.id=a.turn_id
		WHERE a.conversation_id=? AND a.tool_name='fc_propose_ending' AND a.status='committed'
		AND t.invalidated_at IS NULL AND t.status IN ('completed','partial') AND t.turn_seq=(SELECT MAX(turn_seq) FROM conversation_turns WHERE conversation_id=? AND invalidated_at IS NULL AND status IN ('completed','partial'))
		ORDER BY a.created_at DESC LIMIT 1`, conv.ID, conv.ID).Row().Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var proposal EndingProposal
	if err := json.Unmarshal(raw, &proposal); err != nil {
		return nil, err
	}
	if proposal.ConversationID != conv.ID || proposal.BranchEpoch != conv.BranchEpoch {
		return nil, nil
	}
	return &proposal, nil
}
