package storage

import (
	"context"

	"github.com/google/uuid"
)

// Aggregated quantities are decimal strings: sums across users can exceed JSON's safe integer range.
type RewardAssetOverview struct {
	ID             uuid.UUID `json:"id"`
	Key            string    `json:"key"`
	Name           string    `json:"name"`
	Unit           string    `json:"unit"`
	Disabled       bool      `json:"disabled"`
	Balance        string    `json:"balance"`
	Granted        string    `json:"granted"`
	Consumed       string    `json:"consumed"`
	Refunded       string    `json:"refunded"`
	Reversed       string    `json:"reversed"`
	Pending        string    `json:"pending"`
	ExchangeCost   string    `json:"exchange_cost"`
	ExchangeReward string    `json:"exchange_reward"`
}

func (r *RewardRepository) Overview(ctx context.Context) ([]RewardAssetOverview, error) {
	rows := []RewardAssetOverview{}
	err := r.db.WithContext(ctx).Raw(`SELECT d.id,d.key,d.name,d.unit,d.disabled,
 COALESCE(b.balance,0)::text AS balance,COALESCE(b.granted,0)::text AS granted,
 COALESCE(b.consumed,0)::text AS consumed,COALESCE(b.refunded,0)::text AS refunded,
 COALESCE(b.reversed,0)::text AS reversed,COALESCE(p.pending,0)::text AS pending,
 COALESCE(e.cost,0)::text AS exchange_cost,COALESCE(e.reward,0)::text AS exchange_reward
 FROM reward_definitions d
 LEFT JOIN (SELECT reward_definition_id,sum(balance) balance,sum(granted_total) granted,
 sum(consumed_total) consumed,sum(refunded_total) refunded,sum(reversed_total) reversed
 FROM player_reward_balances GROUP BY reward_definition_id) b ON b.reward_definition_id=d.id
 LEFT JOIN (SELECT reward_definition_id,sum(quantity) pending FROM reward_records WHERE status='pending'
 GROUP BY reward_definition_id) p ON p.reward_definition_id=d.id
 LEFT JOIN (SELECT reward_definition_id,sum(CASE WHEN operation='consume' THEN -quantity_delta ELSE 0 END) cost,
 sum(CASE WHEN operation='grant' THEN quantity_delta ELSE 0 END) reward FROM reward_ledger WHERE source_type='reward_exchange'
 GROUP BY reward_definition_id) e ON e.reward_definition_id=d.id
 ORDER BY d.sort_order,d.name,d.id`).Scan(&rows).Error
	return rows, err
}

type RewardRuleCatalogEntry struct {
	ChallengeRewardRule
	PackID        uuid.UUID `json:"pack_id"`
	PackName      string    `json:"pack_name"`
	PackSlug      string    `json:"pack_slug"`
	Version       string    `json:"version"`
	VersionStatus string    `json:"version_status"`
	Current       bool      `json:"current"`
	RewardName    string    `json:"reward_name"`
	RewardKey     string    `json:"reward_key"`
}

func (r *RewardRepository) RuleCatalog(ctx context.Context, definition *uuid.UUID, limit, offset int) ([]RewardRuleCatalogEntry, error) {
	if limit < 1 || limit > 201 {
		limit = 51
	}
	if offset < 0 {
		offset = 0
	}
	q := r.db.WithContext(ctx).Table("challenge_reward_rules AS r").Select(`r.*,p.id AS pack_id,p.name AS pack_name,p.slug AS pack_slug,v.version,v.status AS version_status,COALESCE(p.current_version_id=v.id,false) AS current,d.name AS reward_name,d.key AS reward_key`).Joins("JOIN challenge_versions v ON v.id=r.challenge_version_id").Joins("JOIN challenge_packs p ON p.id=v.challenge_pack_id").Joins("JOIN reward_definitions d ON d.id=r.reward_definition_id")
	if definition != nil {
		q = q.Where("r.reward_definition_id = ?", *definition)
	}
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
