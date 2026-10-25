package storage

import (
	"context"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm/clause"
)

// CampaignStageProgress is branch-scoped milestone progress for a long-running
// Campaign. It is captured by the Campaign checkpoint and therefore rolls back
// with the story branch. It is not an account fact and never contains a
// granted amount.
type CampaignStageProgress struct {
	CampaignID     uuid.UUID `gorm:"column:campaign_id;type:uuid;primaryKey" json:"-"`
	StageKey       string    `gorm:"column:stage_key;primaryKey" json:"stage_key"`
	SourceRevision int64     `gorm:"column:source_revision" json:"source_revision"`
	SourceGameTime int64     `gorm:"column:source_game_time" json:"source_game_time"`
	CreatedAt      time.Time `gorm:"column:created_at" json:"-"`
}

func (CampaignStageProgress) TableName() string { return "campaign_stage_progress" }

// StageProgressView is the public "reached but not settled yet" projection
// shared by both gameplay modes. Amounts always come from the pinned version.
type StageProgressView struct {
	StageKey         string    `json:"stage_key"`
	Name             string    `json:"name"`
	SortOrder        int       `json:"sort_order"`
	ConfiguredPoints int64     `json:"configured_points"`
	DisabledReason   string    `json:"disabled_reason,omitempty"`
	ReachedAt        time.Time `json:"reached_at,omitempty"`
}

// RecordCampaignStageProgress inserts newly reached milestones in the caller's
// action transaction. Existing keys are never updated, so the first source
// revision on the current branch is preserved. It returns only the rows added
// by this call plus the complete current branch progress.
func (s *CampaignActionStore) RecordCampaignStageProgress(ctx context.Context, campaign Campaign, keys []string) ([]CampaignStageProgress, []CampaignStageProgress, error) {
	existing, err := s.CampaignStageProgress(ctx, campaign.ID)
	if err != nil {
		return nil, nil, err
	}
	seen := make(map[string]bool, len(existing))
	for _, row := range existing {
		seen[row.StageKey] = true
	}
	rows := make([]CampaignStageProgress, 0, len(keys))
	for _, key := range keys {
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		rows = append(rows, CampaignStageProgress{
			CampaignID: campaign.ID, StageKey: key,
			SourceRevision: campaign.Revision, SourceGameTime: campaign.GameTime,
			CreatedAt: time.Now().UTC(),
		})
	}
	if len(rows) == 0 {
		return nil, existing, nil
	}
	if err := s.tx.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "campaign_id"}, {Name: "stage_key"}},
		DoNothing: true,
	}).Create(&rows).Error; err != nil {
		return nil, nil, err
	}
	all, err := s.CampaignStageProgress(ctx, campaign.ID)
	if err != nil {
		return nil, nil, err
	}
	return rows, all, nil
}

// CampaignStageProgress lists the current branch's reached milestones in
// stable stage order.
func (s *CampaignActionStore) CampaignStageProgress(ctx context.Context, campaignID uuid.UUID) ([]CampaignStageProgress, error) {
	rows := []CampaignStageProgress{}
	err := s.tx.WithContext(ctx).Where("campaign_id = ?", campaignID).
		Order("source_revision ASC, stage_key ASC").Find(&rows).Error
	return rows, err
}

// SettleCampaignStageRewards turns current branch progress into immutable
// receipts. Positive points are posted to the account ledger in this same
// transaction. It is called only once, from the terminal action or endpoint.
func (s *CampaignActionStore) SettleCampaignStageRewards(ctx context.Context, campaign Campaign, rules []StageRewardGrant) ([]CampaignStageReward, int64, error) {
	progress, err := s.CampaignStageProgress(ctx, campaign.ID)
	if err != nil {
		return nil, 0, err
	}
	reached := make(map[string]bool, len(progress))
	for _, entry := range progress {
		reached[entry.StageKey] = true
	}
	rows := []CampaignStageReward{}
	var points int64
	for _, rule := range rules {
		if !reached[rule.Key] {
			continue
		}
		row, created, err := s.GrantCampaignStageReward(ctx, campaign, rule.Key, rule.Name, rule.Points)
		if err != nil {
			return nil, 0, err
		}
		if created {
			rows = append(rows, *row)
			points += row.GrantedPoints
		}
	}
	return rows, points, nil
}
