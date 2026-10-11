package storage

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm/clause"
)

// Stage claims are account facts and intentionally excluded from Campaign
// checkpoints. Replaying a story branch never recreates a reward entitlement.
type CampaignStageReward struct {
	ID               uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
	CampaignID       uuid.UUID `gorm:"type:uuid;not null" json:"campaign_id"`
	StageKey         string    `json:"stage_key"`
	Name             string    `json:"name"`
	ConfiguredPoints int64     `json:"configured_points"`
	GrantedPoints    int64     `json:"granted_points"`
	DisabledReason   string    `json:"disabled_reason,omitempty"`
	GameTime         int64     `json:"game_time"`
	CreatedAt        time.Time `json:"created_at"`
}

func (CampaignStageReward) TableName() string { return "campaign_stage_rewards" }

var stageRewardKey = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,63}$`)

func (s *CampaignActionStore) GrantCampaignStageReward(ctx context.Context, campaign Campaign, key, name string, points int64) (*CampaignStageReward, bool, error) {
	if !stageRewardKey.MatchString(key) || strings.TrimSpace(name) == "" || len([]rune(name)) > 128 || points < 0 || points > MaxRewardQuantity {
		return nil, false, fmt.Errorf("阶段奖励配置无效")
	}
	row := CampaignStageReward{ID: uuid.New(), CampaignID: campaign.ID, StageKey: key, Name: name, ConfiguredPoints: points, GrantedPoints: points, GameTime: campaign.GameTime, CreatedAt: time.Now()}
	if campaign.LLMRoute == LLMRouteClient {
		row.GrantedPoints = 0
		row.DisabledReason = RewardsDisabledReasonClientRoute
	} else if points == 0 {
		row.DisabledReason = "zero_points"
	}
	result := s.tx.WithContext(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "campaign_id"}, {Name: "stage_key"}}, DoNothing: true}).Create(&row)
	if result.Error != nil {
		return nil, false, result.Error
	}
	if result.RowsAffected == 0 {
		var existing CampaignStageReward
		if err := s.tx.WithContext(ctx).First(&existing, "campaign_id = ? AND stage_key = ?", campaign.ID, key).Error; err != nil {
			return nil, false, err
		}
		if existing.ConfiguredPoints != points || existing.Name != name {
			return nil, false, fmt.Errorf("已领取阶段与固定版本规则不一致")
		}
		return &existing, false, nil
	}
	if row.GrantedPoints > 0 {
		source := campaign.ID.String() + ":" + key
		_, err := NewRewardRepository(s.tx).GrantInCurrentTx(ctx, RewardMutation{PlayerID: campaign.PlayerID, RewardKey: PointsKey, Quantity: row.GrantedPoints, SourceType: "campaign_stage", SourceID: source, IdempotencyKey: "campaign-stage:" + source, Metadata: JSONMap{"campaign_id": campaign.ID, "stage_key": key, "stage_name": name, "challenge_version_id": campaign.ChallengeVersionID}})
		if err != nil {
			return nil, false, err
		}
	}
	return &row, true, nil
}
