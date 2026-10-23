package storage

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm/clause"
)

var (
	ErrPointsCDKUnknown   = errors.New("CDK 不存在，请检查后重试")
	ErrPointsCDKUsed      = errors.New("该 CDK 已被使用")
	ErrPointsCDKExpired   = errors.New("该 CDK 已过期")
	ErrPointsCDKSameBatch = errors.New("该批次CDK您已兑换，请勿重复兑换")
)

// PointsCDKExpiryDays 是管理员可选的有效期天数，默认 7 天。
var PointsCDKExpiryDays = []int{1, 3, 7, 15, 30}

type PointsCDKBatch struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
	Name      string    `json:"name"`
	Points    int64     `json:"points"`
	Quantity  int       `json:"quantity"`
	ExpiresAt time.Time `json:"expires_at"`
	ActorID   uuid.UUID `gorm:"type:uuid" json:"actor_id"`
	CreatedAt time.Time `json:"created_at"`
	Redeemed  int64     `gorm:"->;-:migration" json:"redeemed"`
}

type PointsCDK struct {
	Code       string     `gorm:"primaryKey" json:"code"`
	BatchID    uuid.UUID  `gorm:"type:uuid" json:"batch_id"`
	RedeemedBy *uuid.UUID `gorm:"type:uuid" json:"redeemed_by"`
	RedeemedAt *time.Time `json:"redeemed_at"`
	LedgerID   *uuid.UUID `gorm:"type:uuid" json:"ledger_id"`
	Username   string     `gorm:"->;-:migration" json:"username,omitempty"`
}

func (PointsCDKBatch) TableName() string { return "points_cdk_batches" }
func (PointsCDK) TableName() string      { return "points_cdks" }

// GeneratePointsCDKs 生成一批面值相同的积分 CDK；码为去掉连字符的 32 位 UUID。
func (r *RewardRepository) GeneratePointsCDKs(ctx context.Context, actor uuid.UUID, name string, points int64, quantity, days int) (*PointsCDKBatch, []PointsCDK, error) {
	valid := false
	for _, d := range PointsCDKExpiryDays {
		valid = valid || d == days
	}
	if !valid || points < 1 || points > MaxRewardQuantity || quantity < 1 || quantity > 1000 || len(name) > 128 {
		return nil, nil, ErrRewardQuantity
	}
	now := time.Now()
	batch := PointsCDKBatch{ID: uuid.New(), Name: strings.TrimSpace(name), Points: points, Quantity: quantity, ExpiresAt: now.AddDate(0, 0, days), ActorID: actor, CreatedAt: now}
	codes := make([]PointsCDK, quantity)
	for i := range codes {
		codes[i] = PointsCDK{Code: strings.ReplaceAll(uuid.NewString(), "-", ""), BatchID: batch.ID}
	}
	err := r.WithTx(ctx, func(tx *RewardRepository) error {
		if err := tx.requireRewardAdmin(ctx, actor); err != nil {
			return err
		}
		if err := tx.db.Create(&batch).Error; err != nil {
			return err
		}
		if err := tx.db.Omit("Username").CreateInBatches(codes, 500).Error; err != nil {
			return err
		}
		return tx.rewardAudit(ctx, actor, nil, "points_cdk_generate", uuid.NewString(), JSONMap{"batch_id": batch.ID, "points": points, "quantity": quantity, "days": days})
	})
	return &batch, codes, err
}

func (r *RewardRepository) PointsCDKBatches(ctx context.Context, limit, offset int) ([]PointsCDKBatch, error) {
	rows := []PointsCDKBatch{}
	err := r.db.WithContext(ctx).Select("points_cdk_batches.*, (SELECT count(*) FROM points_cdks c WHERE c.batch_id = points_cdk_batches.id AND c.redeemed_by IS NOT NULL) AS redeemed").
		Order("created_at DESC, id DESC").Limit(limit).Offset(offset).Find(&rows).Error
	return rows, err
}

func (r *RewardRepository) PointsCDKs(ctx context.Context, batch uuid.UUID) ([]PointsCDK, error) {
	rows := []PointsCDK{}
	err := r.db.WithContext(ctx).Table("points_cdks c").Select("c.*, COALESCE(u.username, '') AS username").Joins("LEFT JOIN users u ON u.id = c.redeemed_by").
		Where("c.batch_id = ?", batch).Order("c.redeemed_at DESC NULLS LAST, c.code").Find(&rows).Error
	return rows, err
}

// RedeemPointsCDK 在同一事务内锁定 CDK、校验并发放积分。
func (r *RewardRepository) RedeemPointsCDK(ctx context.Context, player uuid.UUID, code string) (*PointsCDKBatch, error) {
	code = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(code), "-", ""))
	if len(code) != 32 {
		return nil, ErrPointsCDKUnknown
	}
	var batch PointsCDKBatch
	err := r.WithTx(ctx, func(tx *RewardRepository) error {
		var cdk PointsCDK
		if err := tx.db.Clauses(clause.Locking{Strength: "UPDATE"}).Omit("Username").Limit(1).Find(&cdk, "code = ?", code).Error; err != nil {
			return err
		}
		if cdk.Code == "" {
			return ErrPointsCDKUnknown
		}
		if cdk.RedeemedBy != nil {
			return ErrPointsCDKUsed
		}
		if err := tx.db.First(&batch, "id = ?", cdk.BatchID).Error; err != nil {
			return err
		}
		if !time.Now().Before(batch.ExpiresAt) {
			return ErrPointsCDKExpired
		}
		if err := tx.lockRewardKey(ctx, "points-cdk:"+batch.ID.String()+":"+player.String()); err != nil {
			return err
		}
		var same int64
		if err := tx.db.Model(&PointsCDK{}).Where("batch_id = ? AND redeemed_by = ?", batch.ID, player).Count(&same).Error; err != nil {
			return err
		}
		if same > 0 {
			return ErrPointsCDKSameBatch
		}
		grant, err := tx.GrantInCurrentTx(ctx, RewardMutation{PlayerID: player, RewardKey: PointsKey, Quantity: batch.Points, SourceType: "points_cdk", SourceID: code, IdempotencyKey: "points-cdk:" + code, Metadata: JSONMap{"batch_id": batch.ID}})
		if err != nil {
			return err
		}
		return tx.db.Exec("UPDATE points_cdks SET redeemed_by = ?, redeemed_at = now(), ledger_id = ? WHERE code = ?", player, grant.Ledger.ID, code).Error
	})
	return &batch, err
}
