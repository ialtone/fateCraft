package storage

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/operation"
	"gorm.io/gorm/clause"
)

// ChallengeStageReward is one account-level, settled milestone of a single
// challenge conversation. Rows are append-only settlement receipts: rolling a
// story branch back never revokes a settled stage, and in-run progress is kept
// separately in the platform-owned conversation state region.
type ChallengeStageReward struct {
	ID                 uuid.UUID `gorm:"type:uuid;primaryKey" json:"-"`
	ConversationID     uuid.UUID `gorm:"type:uuid;not null" json:"-"`
	PlayerID           uuid.UUID `gorm:"type:uuid;not null" json:"-"`
	ChallengeVersionID uuid.UUID `gorm:"type:uuid;not null" json:"-"`
	StageKey           string    `json:"stage_key"`
	Name               string    `gorm:"column:name_snapshot" json:"name"`
	SortOrder          int       `json:"sort_order"`
	ConfiguredPoints   int64     `json:"configured_points"`
	GrantedPoints      int64     `json:"granted_points"`
	DisabledReason     string    `json:"disabled_reason,omitempty"`
	SourceBranchEpoch  int64     `json:"-"`
	SourceTurnID       uuid.UUID `gorm:"type:uuid" json:"-"`
	SourceActionKey    string    `json:"-"`
	GrantedAt          time.Time `json:"granted_at"`
}

func (ChallengeStageReward) TableName() string { return "challenge_stage_rewards" }

// StageRewardGrant is the fixed-version rule for one stage at settlement. The
// source fields describe where the milestone was first reached, not where the
// final settlement happened.
type StageRewardGrant struct {
	Key               string
	Name              string
	SortOrder         int
	Points            int64
	ActionKey         string
	SourceTurnID      uuid.UUID
	SourceBranchEpoch int64
}

// GrantStageReward inserts one terminal settlement receipt and, for positive
// points, posts the account ledger entry in the same transaction. It is called
// only during final settlement. A conflict returns the existing row without
// granting again. Client-routed conversations keep the receipt with zero
// granted points.
func (r *ChallengeRepository) GrantStageReward(ctx context.Context, conv Conversation, grant StageRewardGrant) (*ChallengeStageReward, bool, error) {
	if !stageRewardKey.MatchString(grant.Key) || strings.TrimSpace(grant.Name) == "" || len([]rune(grant.Name)) > 128 ||
		grant.SortOrder < 0 || grant.Points < 1 || grant.Points > MaxRewardQuantity {
		return nil, false, fmt.Errorf("阶段奖励配置无效")
	}
	sourceTurnID := grant.SourceTurnID
	if sourceTurnID == uuid.Nil {
		if metadata, ok := operation.FromContext(ctx); ok {
			sourceTurnID = metadata.TurnID
		}
	}
	if sourceTurnID == uuid.Nil {
		return nil, false, fmt.Errorf("阶段奖励缺少来源回合")
	}
	playerID, err := uuid.Parse(conv.PlayerID)
	if err != nil {
		return nil, false, fmt.Errorf("玩家 ID 无效: %w", err)
	}
	row := ChallengeStageReward{
		ID:                 uuid.New(),
		ConversationID:     conv.ID,
		PlayerID:           playerID,
		ChallengeVersionID: conv.ChallengeVersionID,
		StageKey:           grant.Key,
		Name:               grant.Name,
		SortOrder:          grant.SortOrder,
		ConfiguredPoints:   grant.Points,
		GrantedPoints:      grant.Points,
		SourceBranchEpoch:  grant.SourceBranchEpoch,
		SourceTurnID:       sourceTurnID,
		SourceActionKey:    grant.ActionKey,
		GrantedAt:          time.Now().UTC(),
	}
	if conv.LLMRoute == LLMRouteClient {
		row.GrantedPoints = 0
		row.DisabledReason = RewardsDisabledReasonClientRoute
	}
	result := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "conversation_id"}, {Name: "stage_key"}},
		DoNothing: true,
	}).Create(&row)
	if result.Error != nil {
		return nil, false, result.Error
	}
	if result.RowsAffected == 0 {
		var existing ChallengeStageReward
		if err := r.db.WithContext(ctx).First(&existing, "conversation_id = ? AND stage_key = ?", conv.ID, grant.Key).Error; err != nil {
			return nil, false, err
		}
		if existing.ConfiguredPoints != grant.Points || existing.Name != grant.Name {
			return nil, false, fmt.Errorf("已领取阶段与固定版本规则不一致")
		}
		return &existing, false, nil
	}
	if row.GrantedPoints > 0 {
		source := conv.ID.String() + ":" + grant.Key
		if _, err := NewRewardRepository(r.db).GrantInCurrentTx(ctx, RewardMutation{
			PlayerID:       playerID,
			RewardKey:      PointsKey,
			Quantity:       row.GrantedPoints,
			SourceType:     "challenge_stage",
			SourceID:       source,
			IdempotencyKey: "challenge-stage:" + source,
			Metadata: JSONMap{
				"conversation_id":      conv.ID,
				"challenge_version_id": conv.ChallengeVersionID,
				"stage_key":            grant.Key,
				"stage_name":           grant.Name,
			},
		}); err != nil {
			return nil, false, err
		}
	}
	return &row, true, nil
}

// StateRepo binds the conversation-state store to the same transaction as the
// challenge repository. Background readers may use the same method to obtain a
// read handle.
func (r *ChallengeRepository) StateRepo() *StateRepo { return NewStateRepo(r.db) }

// SettleStageRewards turns the branch's reached milestones into terminal
// receipts. It is idempotent per (conversation, stage) and must be called from
// the single final-settlement transaction.
func (r *ChallengeRepository) SettleStageRewards(ctx context.Context, conv Conversation, grants []StageRewardGrant) ([]ChallengeStageReward, int64, error) {
	rows := make([]ChallengeStageReward, 0, len(grants))
	var points int64
	for _, grant := range grants {
		row, created, err := r.GrantStageReward(ctx, conv, grant)
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

// StageRewards lists the conversation's settlement receipts in display order.
func (r *ChallengeRepository) StageRewards(ctx context.Context, convID uuid.UUID) ([]ChallengeStageReward, error) {
	rows := []ChallengeStageReward{}
	err := r.db.WithContext(ctx).Where("conversation_id = ?", convID).
		Order("sort_order ASC, granted_at ASC, stage_key ASC").Find(&rows).Error
	return rows, err
}

// ConversationStageRewards projects either mode's durable stage facts onto the
// common public receipt shape.
func (r *ChallengeRepository) ConversationStageRewards(ctx context.Context, conv Conversation) ([]ChallengeStageReward, error) {
	if conv.CampaignID == nil {
		return r.StageRewards(ctx, conv.ID)
	}
	var claims []CampaignStageReward
	if err := r.db.WithContext(ctx).Where("campaign_id = ?", *conv.CampaignID).
		Order("created_at ASC, stage_key ASC").Find(&claims).Error; err != nil {
		return nil, err
	}
	rows := make([]ChallengeStageReward, 0, len(claims))
	for i, claim := range claims {
		rows = append(rows, ChallengeStageReward{StageKey: claim.StageKey, Name: claim.Name,
			SortOrder: i, ConfiguredPoints: claim.ConfiguredPoints, GrantedPoints: claim.GrantedPoints,
			DisabledReason: claim.DisabledReason, GrantedAt: claim.CreatedAt})
	}
	return rows, nil
}

// StageRewardPoints sums the actual granted points of the conversation.
func (r *ChallengeRepository) StageRewardPoints(ctx context.Context, convID uuid.UUID) (int64, error) {
	var total int64
	err := r.db.WithContext(ctx).Model(&ChallengeStageReward{}).
		Where("conversation_id = ?", convID).
		Select("COALESCE(SUM(granted_points), 0)").Scan(&total).Error
	return total, err
}

// UserTurnCount counts user messages, matching the turn-count definition used
// by settlement verification.
func (r *ChallengeRepository) UserTurnCount(ctx context.Context, convID uuid.UUID) (int, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&ChatMessage{}).
		Where("conversation_id = ? AND role = ? AND invalidated_at IS NULL", convID, RoleUser).Count(&count).Error
	return int(count), err
}
