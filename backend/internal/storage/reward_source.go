package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type GameplayRewardSource struct {
	PackID         uuid.UUID  `json:"pack_id"`
	PackSlug       string     `json:"pack_slug"`
	PackName       string     `json:"pack_name"`
	VersionID      uuid.UUID  `json:"version_id"`
	Version        string     `json:"version"`
	ContentHash    string     `json:"content_hash"`
	ConversationID *uuid.UUID `json:"conversation_id,omitempty"`
	CampaignID     *uuid.UUID `json:"campaign_id,omitempty"`
	StageKey       string     `json:"stage_key,omitempty"`
	StageName      string     `json:"stage_name,omitempty"`
	OutcomeKey     string     `json:"outcome_key,omitempty"`
	OutcomeTitle   string     `json:"outcome_title,omitempty"`
}

func (r *RewardRepository) gameplayRewardSource(ctx context.Context, in *RewardMutation) error {
	if in.SourceType != "challenge_run" && in.SourceType != "campaign_stage" && in.SourceType != "campaign_outcome" && in.SourceType != "challenge_stage" {
		return nil
	}
	versionID, err := uuid.Parse(fmt.Sprint(in.Metadata["challenge_version_id"]))
	if err != nil {
		return ErrRewardState
	}
	repo := NewChallengeRepository(r.db)
	v, err := repo.GetVersion(ctx, versionID)
	if err != nil {
		return err
	}
	if err := repo.CheckVersionRun(ctx, v.ID, v.ContentHash); err != nil {
		return err
	}
	p, err := repo.GetPack(ctx, v.ChallengePackID)
	if err != nil {
		return err
	}
	var manifest struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(v.Manifest, &manifest); err != nil {
		return err
	}
	snapshot := GameplayRewardSource{PackID: p.ID, PackSlug: p.Slug, PackName: manifest.Name, VersionID: v.ID, Version: v.Version, ContentHash: v.ContentHash}
	var conv Conversation
	if in.SourceType == "challenge_run" {
		if in.ChallengeRunID == nil {
			return ErrRewardState
		}
		var run ChallengeRun
		if err := r.db.WithContext(ctx).First(&run, "id=?", *in.ChallengeRunID).Error; err != nil {
			return err
		}
		if run.ChallengeVersionID != v.ID || run.PlayerID != in.PlayerID {
			return ErrRewardState
		}
		snapshot.ConversationID = &run.ConversationID
		snapshot.OutcomeKey = run.OutcomeKey
		snapshot.OutcomeTitle = run.TitleSnapshot
	} else if in.SourceType == "challenge_stage" {
		// Single-run stages belong to a conversation, not a Campaign; source_id
		// is "<conversation_id>:<stage_key>" so the conversation comes from
		// metadata rather than the ledger source id.
		conversationID, err := uuid.Parse(fmt.Sprint(in.Metadata["conversation_id"]))
		if err != nil {
			return ErrRewardState
		}
		if err := r.db.WithContext(ctx).First(&conv, "id=?", conversationID).Error; err != nil {
			return err
		}
		if conv.ChallengeVersionID != v.ID || conv.PlayerID != in.PlayerID.String() {
			return ErrRewardState
		}
		snapshot.ConversationID = &conversationID
		snapshot.StageKey, _ = in.Metadata["stage_key"].(string)
		snapshot.StageName, _ = in.Metadata["stage_name"].(string)
	} else {
		campaignID := in.SourceID
		if in.SourceType == "campaign_stage" {
			campaignID = fmt.Sprint(in.Metadata["campaign_id"])
		}
		id, err := uuid.Parse(campaignID)
		if err != nil {
			return ErrRewardState
		}
		var campaign Campaign
		if err := r.db.WithContext(ctx).First(&campaign, "id=?", id).Error; err != nil {
			return err
		}
		if campaign.ChallengeVersionID != v.ID || campaign.PlayerID != in.PlayerID {
			return ErrRewardState
		}
		snapshot.CampaignID = &id
		if err := r.db.WithContext(ctx).First(&conv, "campaign_id=?", id).Error; err == nil {
			snapshot.ConversationID = &conv.ID
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if in.SourceType == "campaign_stage" {
			snapshot.StageKey, _ = in.Metadata["stage_key"].(string)
			snapshot.StageName, _ = in.Metadata["stage_name"].(string)
		} else {
			snapshot.OutcomeKey, _ = in.Metadata["outcome_key"].(string)
			rule, err := repo.OutcomeRule(ctx, v.ID, snapshot.OutcomeKey)
			if err != nil {
				return err
			}
			snapshot.OutcomeTitle = rule.Title
		}
	}
	if in.Metadata == nil {
		in.Metadata = JSONMap{}
	}
	in.Metadata["gameplay_source"] = snapshot
	return nil
}

func rewardGameplayProjection(entry RewardLedgerEntry) *GameplayRewardSource {
	raw, err := json.Marshal(entry.Metadata["gameplay_source"])
	if err != nil {
		return nil
	}
	var s GameplayRewardSource
	if err := json.Unmarshal(raw, &s); err != nil || s.VersionID == uuid.Nil || s.PackID == uuid.Nil {
		return nil
	}
	return &s
}
