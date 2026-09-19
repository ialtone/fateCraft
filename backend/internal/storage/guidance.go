package storage

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/guidance"
	"gorm.io/gorm"
)

var ErrGuidanceExpired = errors.New("行动提示已失效，请使用当前提示")

// CurrentGuidance never searches backwards for a non-empty suggestion: only
// the current turn's current attempt can own actionable suggestions.
func (r *MessageRepo) CurrentGuidance(ctx context.Context, conversationID uuid.UUID) (*guidance.Active, error) {
	display, snapshot, err := currentGuidance(r.db.WithContext(ctx), conversationID)
	if err != nil || snapshot == nil {
		return nil, err
	}
	active := snapshot.Public(display.ID.String())
	return &active, nil
}

func currentGuidance(db *gorm.DB, conversationID uuid.UUID) (*DisplayMessage, *guidance.Snapshot, error) {
	var display DisplayMessage
	latestTurn := db.Model(&ConversationTurn{}).Select("id").Where("conversation_id = ? AND invalidated_at IS NULL", conversationID).Order("turn_seq DESC").Limit(1)
	latestAttempt := db.Model(&GenerationAttempt{}).Select("id").Where("turn_id = (?)", latestTurn).Order("attempt_no DESC").Limit(1)
	err := db.Where("conversation_id = ? AND attempt_id = (?) AND type = 'assistant' AND invalidated_at IS NULL", conversationID, latestAttempt).
		Order("created_at DESC, id DESC").First(&display).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	value, exists := display.Metadata[guidance.MetadataKey]
	if !exists {
		return nil, nil, nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, nil, err
	}
	var snapshot guidance.Snapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return nil, nil, err
	}
	proposal, err := json.Marshal(snapshot.Proposal)
	if err != nil {
		return nil, nil, err
	}
	if _, err := guidance.Decode(proposal); err != nil {
		return nil, nil, err
	}
	var conv Conversation
	if err := db.Select("id", "status", "campaign_id").First(&conv, "id = ?", conversationID).Error; err != nil {
		return nil, nil, err
	}
	if conv.Status != StatusActive {
		return nil, nil, nil
	}
	var state ConversationState
	err = db.First(&state, "conversation_id = ?", conversationID).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, err
	}
	if state.State == nil {
		state.State = JSONMap{}
	}
	if snapshot.StateFingerprint != guidance.Fingerprint(state.State) {
		return nil, nil, nil
	}
	if conv.CampaignID != nil {
		var campaign Campaign
		if err := db.Select("revision", "status").First(&campaign, "id = ?", *conv.CampaignID).Error; err != nil {
			return nil, nil, err
		}
		if campaign.Revision != snapshot.CampaignRevision || campaign.Status != StatusActive {
			return nil, nil, nil
		}
	}
	return &display, &snapshot, nil
}

func resolveGuidanceSelection(tx *gorm.DB, conversationID uuid.UUID, selection guidance.Selection) (string, error) {
	display, snapshot, err := currentGuidance(tx, conversationID)
	if err != nil {
		return "", err
	}
	if snapshot == nil || display.ID.String() != selection.ID {
		return "", ErrGuidanceExpired
	}
	message, ok := snapshot.Message(selection.Key)
	if !ok {
		return "", ErrGuidanceExpired
	}
	return message, nil
}
