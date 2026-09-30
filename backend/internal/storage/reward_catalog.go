package storage

import (
	"context"

	"github.com/google/uuid"
)

type RewardRuleCatalogEntry struct {
	ChallengeOutcomeRule
	PackID        uuid.UUID `json:"pack_id"`
	PackName      string    `json:"pack_name"`
	PackSlug      string    `json:"pack_slug"`
	Version       string    `json:"version"`
	VersionStatus string    `json:"version_status"`
	Current       bool      `json:"current"`
}

func (r *RewardRepository) RuleCatalog(ctx context.Context, limit, offset int) ([]RewardRuleCatalogEntry, error) {
	if limit < 1 || limit > 201 {
		limit = 51
	}
	if offset < 0 {
		offset = 0
	}
	q := r.db.WithContext(ctx).Table("challenge_outcome_rules AS r").Select(`r.*,p.id AS pack_id,p.name AS pack_name,p.slug AS pack_slug,v.version,v.status AS version_status,COALESCE(p.current_version_id=v.id,false) AS current`).Joins("JOIN challenge_versions v ON v.id=r.challenge_version_id").Joins("JOIN challenge_packs p ON p.id=v.challenge_pack_id")
	rows := []RewardRuleCatalogEntry{}
	err := q.Order("p.name ASC,v.created_at DESC,r.id ASC").Limit(limit).Offset(offset).Scan(&rows).Error
	return rows, err
}

func (r *RewardRepository) AuditPage(ctx context.Context, cursor RewardPageCursor, limit int) ([]AdminAuditLog, error) {
	if limit < 1 || limit > 201 {
		limit = 51
	}
	q := r.db.WithContext(ctx).Where("action LIKE 'reward_%'")
	if !cursor.Time.IsZero() {
		q = q.Where("(created_at,id)<(?,?)", cursor.Time, cursor.ID)
	}
	rows := []AdminAuditLog{}
	err := q.Order("created_at DESC,id DESC").Limit(limit).Find(&rows).Error
	return rows, err
}
