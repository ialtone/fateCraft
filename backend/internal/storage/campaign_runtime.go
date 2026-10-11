package storage

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"time"
)

// CampaignRuntimeState stores a bounded module snapshot scoped to its save.
// It is never exposed directly by the API or copied into conversation state.
type CampaignRuntimeState struct {
	CampaignID uuid.UUID `gorm:"type:uuid;primaryKey"`
	State      JSONMap   `gorm:"type:jsonb;not null"`
}

func InitializeCampaignRuntimeState(tx *gorm.DB, campaignID uuid.UUID, state JSONMap) error {
	return tx.Create(&CampaignRuntimeState{CampaignID: campaignID, State: state}).Error
}

func (s *CampaignActionStore) SaveRuntimeState(state JSONMap) error {
	result := s.tx.Model(&CampaignRuntimeState{}).Where("campaign_id = ?", s.campaign.ID).Update("state", state)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrNotFound
	}
	return nil
}

// LoadCampaignRuntimeDomain reads all fixed rule inputs from the caller's
// connection. The execution adapter never opens a global DB connection.
func LoadCampaignRuntimeDomain(ctx context.Context, tx *gorm.DB, campaign Campaign) (*CampaignDomainState, error) {
	var stored CampaignRuntimeState
	if err := tx.WithContext(ctx).First(&stored, "campaign_id = ?", campaign.ID).Error; err != nil {
		return nil, err
	}
	return loadRuntimeDomainContent(ctx, tx, campaign, stored.State)
}

func loadRuntimeDomainContent(ctx context.Context, tx *gorm.DB, campaign Campaign, state JSONMap) (*CampaignDomainState, error) {
	domain := &CampaignDomainState{RuntimeState: state, Definitions: map[string]GameplayDefinition{}, OutcomeRewards: []CampaignOutcomeReward{}}
	if err := tx.WithContext(ctx).First(&domain.RuntimeDocument, "challenge_version_id = ?", campaign.ChallengeVersionID).Error; err != nil {
		return nil, err
	}
	var definitions []GameplayDefinition
	if err := tx.WithContext(ctx).Where("challenge_version_id = ?", campaign.ChallengeVersionID).Order("kind, sort_order, key").Find(&definitions).Error; err != nil {
		return nil, err
	}
	for _, d := range definitions {
		domain.Definitions[definitionMapKey(d.Kind, d.Key)] = d
	}
	if err := tx.WithContext(ctx).Where("challenge_version_id = ?", campaign.ChallengeVersionID).Find(&domain.Outcomes).Error; err != nil {
		return nil, err
	}
	var err error
	domain.OutcomeRewards, err = loadCampaignOutcomeRewards(ctx, tx, campaign)
	return domain, err
}

func CampaignRuntimeInitialContent(ctx context.Context, tx *gorm.DB, campaign Campaign) (*CampaignDomainState, error) {
	if campaign.ID == uuid.Nil {
		return nil, fmt.Errorf("missing campaign scope")
	}
	return loadRuntimeDomainContent(ctx, tx, campaign, nil)
}

func optionalRuntimeDomain(ctx context.Context, tx *gorm.DB, campaign Campaign) (*CampaignDomainState, error) {
	var stored CampaignRuntimeState
	err := tx.WithContext(ctx).First(&stored, "campaign_id = ?", campaign.ID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	// A missing pinned document after a module row was found is corruption,
	// not permission to reinterpret the save as the relational world module.
	return loadRuntimeDomainContent(ctx, tx, campaign, stored.State)
}

func (r *ChallengeRepository) CampaignActions() *CampaignRepository {
	return NewCampaignRepository(r.db)
}

func (r *ChallengeRepository) EndModuleConversation(ctx context.Context, id uuid.UUID) error {
	now := time.Now()
	result := r.db.WithContext(ctx).Model(&Conversation{}).Where("id = ? AND status = ?", id, StatusActive).
		Updates(map[string]any{"status": StatusEnded, "ended_at": now, "updated_at": now})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrConversationNotActive
	}
	return nil
}
