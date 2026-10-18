package storage

import (
	"context"

	"github.com/google/uuid"
)

// NarrativeInputs returns committed actions that have not yet been consumed
// by a live assistant narrative. System action receipts are deliberately
// ignored: they describe the click, not the story built from its result.
func (r *CampaignRepository) NarrativeInputs(ctx context.Context, campaignID uuid.UUID, revision int64, limit int) ([]CampaignActionCommit, error) {
	if limit <= 0 || limit > 16 {
		limit = 8
	}
	rows := []CampaignActionCommit{}
	err := r.db.WithContext(ctx).
		Where("campaign_id = ? AND revision <= ?", campaignID, revision).
		Where(`NOT EXISTS (
			SELECT 1
			FROM display_message_campaign_actions binding
			JOIN display_messages message ON message.id = binding.display_message_id
			WHERE binding.campaign_id = campaign_action_commits.campaign_id
			  AND binding.action_id = campaign_action_commits.idempotency_key
			  AND message.type = 'assistant'
			  AND message.invalidated_at IS NULL
		)`).
		Order("revision ASC").
		Limit(limit).
		Find(&rows).Error
	return rows, err
}
