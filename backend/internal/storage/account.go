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
	PlayerID           string    `gorm:"column:player_id;unique;not null" json:"player_id"`
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

// TrialHistory 轮回历史记录。
type TrialHistory struct {
	ID                 string    `gorm:"column:id;primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	PlayerID           string    `gorm:"column:player_id;not null" json:"player_id"`
	ConversationID     string    `gorm:"column:conversation_id;type:uuid;not null" json:"conversation_id"`
	EndingType         string    `gorm:"column:ending_type;not null" json:"ending_type"`
	SpiritStonesEarned int64     `gorm:"column:spirit_stones_earned;not null;default:0" json:"spirit_stones_earned"`
	CharacterName      string    `gorm:"column:character_name" json:"character_name"`
	CultivationLevel   string    `gorm:"column:cultivation_level" json:"cultivation_level"`
	SurvivalTurns      int       `gorm:"column:survival_turns" json:"survival_turns"`
	EndedAt            time.Time `gorm:"column:ended_at;default:CURRENT_TIMESTAMP" json:"ended_at"`
}

func (TrialHistory) TableName() string { return "trial_history" }

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

// RecordTrialSuccess 记录成功轮回（破碎虚空）。
func (r *AccountRepository) RecordTrialSuccess(playerID string, spiritStones int64) error {
	return r.db.Model(&Account{}).
		Where("player_id = ?", playerID).
		Updates(map[string]interface{}{
			"total_spirit_stones": gorm.Expr("total_spirit_stones + ?", spiritStones),
			"successful_trials":   gorm.Expr("successful_trials + 1"),
			"updated_at":          time.Now(),
		}).Error
}

// RecordTrialFailure 记录失败轮回（道消身殒）。
func (r *AccountRepository) RecordTrialFailure(playerID string) error {
	return r.db.Model(&Account{}).
		Where("player_id = ?", playerID).
		Updates(map[string]interface{}{
			"failed_trials": gorm.Expr("failed_trials + 1"),
			"updated_at":    time.Now(),
		}).Error
}

// AddTrialHistory 添加轮回历史记录。
func (r *AccountRepository) AddTrialHistory(history *TrialHistory) error {
	return r.db.Create(history).Error
}

// GetTrialHistory 获取轮回历史列表。
func (r *AccountRepository) GetTrialHistory(playerID string, limit int) ([]TrialHistory, error) {
	var history []TrialHistory
	err := r.db.Where("player_id = ?", playerID).
		Order("ended_at DESC").
		Limit(limit).
		Find(&history).Error
	return history, err
}
