package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"gorm.io/gorm/clause"
	"time"
)

type VersionReferences struct {
	Conversations       int64 `json:"conversations"`
	ActiveConversations int64 `json:"active_conversations"`
	Campaigns           int64 `json:"campaigns"`
	ActiveCampaigns     int64 `json:"active_campaigns"`
	Generating          int64 `json:"generating"`
	Rewards             int64 `json:"rewards"`
}

func (r *ChallengeRepository) SetDraftProvenance(ctx context.Context, id uuid.UUID, provenance JSONMap) error {
	if provenance == nil {
		provenance = JSONMap{}
	}
	result := r.db.WithContext(ctx).Model(&ChallengeVersion{}).Where("id=? AND status=?", id, ChallengeVersionDraft).Update("provenance", provenance)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrPublishedVersion
	}
	return nil
}

func (r *ChallengeRepository) RequiredRuntimeVersions(ctx context.Context) ([]ChallengeVersion, error) {
	var versions []ChallengeVersion
	err := r.db.WithContext(ctx).Where("id IN (SELECT current_version_id FROM challenge_packs WHERE current_version_id IS NOT NULL) OR id IN (SELECT challenge_version_id FROM conversations WHERE status IN ('active','archived') AND deleted_at IS NULL) OR id IN (SELECT challenge_version_id FROM campaigns WHERE status='active')").Find(&versions).Error
	return versions, err
}

func (r *ChallengeRepository) VersionReferences(ctx context.Context, id uuid.UUID) (*VersionReferences, error) {
	result := &VersionReferences{}
	queries := []struct {
		sql  string
		dest *int64
	}{
		{"SELECT count(*) FROM conversations WHERE challenge_version_id=?", &result.Conversations},
		{"SELECT count(*) FROM conversations WHERE challenge_version_id=? AND status='active'", &result.ActiveConversations},
		{"SELECT count(*) FROM campaigns WHERE challenge_version_id=?", &result.Campaigns},
		{"SELECT count(*) FROM campaigns WHERE challenge_version_id=? AND status='active'", &result.ActiveCampaigns},
		{"SELECT count(*) FROM conversation_generation_runs g JOIN conversations c ON c.id=g.conversation_id WHERE c.challenge_version_id=? AND g.status='running'", &result.Generating},
		{"SELECT count(*) FROM reward_ledger WHERE metadata->>'challenge_version_id'=?", &result.Rewards},
	}
	for _, q := range queries {
		if err := r.db.WithContext(ctx).Raw(q.sql, id.String()).Scan(q.dest).Error; err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (r *ChallengeRepository) SelectCurrent(ctx context.Context, packID, versionID, actor uuid.UUID, expected int64) error {
	return r.WithTx(ctx, func(tx *ChallengeRepository) error {
		pack, err := tx.LockPack(ctx, packID)
		if err != nil {
			return err
		}
		if expected > 0 && pack.ManagementRevision != expected {
			return ErrRevisionConflict
		}
		version, err := tx.LockVersion(ctx, versionID)
		if err != nil {
			return err
		}
		if version.ChallengePackID != packID {
			return ErrNotFound
		}
		if version.Status != ChallengeVersionPublished && version.Status != ChallengeVersionRetired {
			return ErrPublishedVersion
		}
		if version.RunPolicy == "blocked" {
			return ErrVersionBlocked
		}
		if pack.CurrentVersionID != nil && *pack.CurrentVersionID != versionID {
			if err := tx.retireVersion(ctx, *pack.CurrentVersionID, time.Now()); err != nil {
				return err
			}
		}
		if err := tx.db.WithContext(ctx).Model(version).Updates(map[string]any{"status": ChallengeVersionPublished, "updated_at": time.Now()}).Error; err != nil {
			return err
		}
		old := pack.CurrentVersionID
		var manifest struct {
			Name        string
			Description string
		}
		if err := json.Unmarshal(version.Manifest, &manifest); err != nil {
			return err
		}
		pack.Name = manifest.Name
		pack.Description = manifest.Description
		pack.CurrentVersionID = &versionID
		pack.ManagementRevision++
		pack.UpdatedAt = time.Now()
		if err := tx.SavePack(ctx, pack); err != nil {
			return err
		}
		return tx.AuditGameplay(ctx, actor, "gameplay_current_version", map[string]any{"pack_id": packID, "previous_version_id": old, "version_id": versionID})
	})
}

func (r *ChallengeRepository) Retire(ctx context.Context, id, actor uuid.UUID, expected int64) error {
	return r.WithTx(ctx, func(tx *ChallengeRepository) error {
		v, err := tx.GetVersion(ctx, id)
		if err != nil {
			return err
		}
		p, err := tx.LockPack(ctx, v.ChallengePackID)
		if err != nil {
			return err
		}
		v, err = tx.LockVersion(ctx, id)
		if err != nil {
			return err
		}
		if expected != v.PolicyRevision {
			return ErrRevisionConflict
		}
		if p.CurrentVersionID != nil && *p.CurrentVersionID == id {
			return fmt.Errorf("当前新开局版本不能直接退役，请先切换入口")
		}
		if v.Status == ChallengeVersionDraft {
			return ErrPublishedVersion
		}
		if err := tx.retireVersion(ctx, id, time.Now()); err != nil {
			return err
		}
		if err := tx.db.WithContext(ctx).Model(v).Update("policy_revision", v.PolicyRevision+1).Error; err != nil {
			return err
		}
		p.ManagementRevision++
		if err := tx.SavePack(ctx, p); err != nil {
			return err
		}
		return tx.AuditGameplay(ctx, actor, "gameplay_retire", map[string]any{"version_id": id})
	})
}

func (r *ChallengeRepository) SetRunPolicy(ctx context.Context, id, actor uuid.UUID, policy, reason string, expected int64) error {
	if (policy != "allowed" && policy != "blocked") || (policy == "blocked" && reason == "") {
		return fmt.Errorf("运行策略无效或缺少暂停原因")
	}
	return r.WithTx(ctx, func(tx *ChallengeRepository) error {
		peek, err := tx.GetVersion(ctx, id)
		if err != nil {
			return err
		}
		pack, err := tx.LockPack(ctx, peek.ChallengePackID)
		if err != nil {
			return err
		}
		v, err := tx.LockVersion(ctx, id)
		if err != nil {
			return err
		}
		if v.PolicyRevision != expected {
			return ErrRevisionConflict
		}
		if v.Status == ChallengeVersionDraft {
			return ErrPublishedVersion
		}
		if err := tx.db.WithContext(ctx).Model(v).Updates(map[string]any{"run_policy": policy, "run_policy_reason": reason, "policy_revision": v.PolicyRevision + 1, "policy_actor": actor, "policy_changed_at": time.Now()}).Error; err != nil {
			return err
		}
		pack.ManagementRevision++
		if err := tx.SavePack(ctx, pack); err != nil {
			return err
		}
		return tx.AuditGameplay(ctx, actor, "gameplay_run_policy", map[string]any{"version_id": id, "policy": policy, "reason": reason})
	})
}

func (r *ChallengeRepository) AuditGameplay(ctx context.Context, actor uuid.UUID, action string, metadata map[string]any) error {
	var user *uuid.UUID
	if actor != uuid.Nil {
		user = &actor
	}
	return r.db.WithContext(ctx).Create(&AdminAuditLog{ID: uuid.New(), ActorUserID: user, Action: action, Result: "success", Metadata: JSONMap(metadata), CreatedAt: time.Now()}).Error
}

// CheckVersionRun takes a shared transaction lock which serializes writes with emergency blocking.
func (r *ChallengeRepository) CheckVersionRun(ctx context.Context, id uuid.UUID, hash string) error {
	var v ChallengeVersion
	if err := r.db.WithContext(ctx).Clauses(clause.Locking{Strength: "SHARE"}).First(&v, "id = ?", id).Error; err != nil {
		return err
	}
	if v.RunPolicy == "blocked" {
		return fmt.Errorf("%w: %s", ErrVersionBlocked, v.RunPolicyReason)
	}
	if (v.Status != ChallengeVersionPublished && v.Status != ChallengeVersionRetired) || v.ContentHash != hash {
		return ErrPublishedVersion
	}
	return nil
}

func (r *ChallengeRepository) ReplaceDraftChecked(ctx context.Context, id uuid.UUID, expected int64, manifest JSONDoc, runtime ChallengeRuntimeConfig, assets []ChallengeAsset, definitions []GameplayDefinition, outcomes []ChallengeOutcomeRule) error {
	return r.WithTx(ctx, func(tx *ChallengeRepository) error {
		v, err := tx.LockVersion(ctx, id)
		if err != nil {
			return err
		}
		if expected > 0 && v.Revision != expected {
			return ErrRevisionConflict
		}
		return tx.replaceDraft(ctx, id, manifest, runtime, assets, definitions, outcomes)
	})
}
