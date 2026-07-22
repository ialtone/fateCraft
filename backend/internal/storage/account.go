package storage

import (
	"errors"
	"time"

	"gorm.io/gorm"
)

// ErrNoRewinds 表示没有可用的天道回溯次数。
var ErrNoRewinds = errors.New("没有可用的天道回溯次数")

// Account 账号全局数据。
type Account struct {
	ID                 int       `gorm:"column:id;primaryKey" json:"id"`
	PlayerID           string    `gorm:"column:player_id;type:uuid;unique;not null" json:"player_id"`
	RewindCount        int       `gorm:"column:rewind_count;not null;default:10" json:"rewind_count"`
	TotalRewindsEarned int       `gorm:"column:total_rewinds_earned;not null;default:10" json:"total_rewinds_earned"`
	RewindsUsed        int       `gorm:"column:rewinds_used;not null;default:0" json:"rewinds_used"`
	TotalSpiritStones  int64     `gorm:"column:total_spirit_stones;not null;default:0" json:"total_spirit_stones"`
	SuccessfulTrials   int       `gorm:"column:successful_trials;not null;default:0" json:"successful_trials"`
	FailedTrials       int       `gorm:"column:failed_trials;not null;default:0" json:"failed_trials"`
	CreatedAt          time.Time `gorm:"column:created_at" json:"created_at"`
	UpdatedAt          time.Time `gorm:"column:updated_at" json:"updated_at"`
}

func (Account) TableName() string { return "accounts" }

// AccountRepository 账号数据访问层。
type AccountRepository struct {
	db *gorm.DB
}

func NewAccountRepository(db *gorm.DB) *AccountRepository {
	return &AccountRepository{db: db}
}

// Get 获取账号数据，不存在时返回 ErrNotFound。
func (r *AccountRepository) Get(playerID string) (*Account, error) {
	var acc Account
	err := r.db.Where("player_id = ?", playerID).First(&acc).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &acc, nil
}

// GetOrCreate 获取或创建账号。
func (r *AccountRepository) GetOrCreate(playerID string) (*Account, error) {
	var acc Account
	err := r.db.Where("player_id = ?", playerID).FirstOrCreate(&acc, Account{
		PlayerID:           playerID,
		RewindCount:        10,
		TotalRewindsEarned: 10,
		RewindsUsed:        0,
		TotalSpiritStones:  0,
		SuccessfulTrials:   0,
		FailedTrials:       0,
	}).Error
	return &acc, err
}

// UseRewind 使用一次天道回溯；次数不足时返回 ErrNoRewinds。
func (r *AccountRepository) UseRewind(playerID string) error {
	res := r.db.Model(&Account{}).
		Where("player_id = ? AND rewind_count > 0", playerID).
		Updates(map[string]interface{}{
			"rewind_count": gorm.Expr("rewind_count - 1"),
			"rewinds_used": gorm.Expr("rewinds_used + 1"),
			"updated_at":   time.Now(),
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNoRewinds
	}
	return nil
}

// AddRewind 增加天道回溯次数。
func (r *AccountRepository) AddRewind(playerID string, count int) error {
	return r.db.Model(&Account{}).
		Where("player_id = ?", playerID).
		Updates(map[string]interface{}{
			"rewind_count":         gorm.Expr("rewind_count + ?", count),
			"total_rewinds_earned": gorm.Expr("total_rewinds_earned + ?", count),
			"updated_at":           time.Now(),
		}).Error
}
