package storage

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// 注册方式：无本地密码且绑定了 LinuxDo 的账号视为 LinuxDo 注册，其余为邮箱（含 owner 初始化）。
const (
	RegistrationEmail   = "email"
	RegistrationLinuxDo = "linuxdo"

	registrationExpr = `CASE WHEN users.password_hash IS NULL AND EXISTS (SELECT 1 FROM auth_identities ai WHERE ai.user_id = users.id AND ai.provider = 'linuxdo') THEN 'linuxdo' ELSE 'email' END`
)

// AdminUserRepo 提供管理员用户管理页所需的聚合查询；只读，唯一的写入是管理审计。
type AdminUserRepo struct{ db *gorm.DB }

func NewAdminUserRepo(db *gorm.DB) *AdminUserRepo { return &AdminUserRepo{db: db} }

// AdminUserRow 是用户列表行，在 User 基础上附带注册方式。
type AdminUserRow struct {
	User
	RegistrationMethod string `gorm:"column:registration_method" json:"registration_method"`
}

// AdminUserFilter 描述用户列表的筛选与排序；取值范围由调用方校验，未知的 Sort 回落为 created_at。
type AdminUserFilter struct {
	Search       string
	Status       string
	Role         string
	Registration string
	Sort         string // created_at / last_login_at
	Asc          bool
	Limit        int
	Offset       int
}

func (r *AdminUserRepo) List(ctx context.Context, f AdminUserFilter) ([]AdminUserRow, error) {
	query := applyUserSearch(r.db.WithContext(ctx).Table("users").Select("users.*, "+registrationExpr+" AS registration_method"), f.Search)
	if f.Status != "" {
		query = query.Where("users.status = ?", f.Status)
	}
	if f.Role != "" {
		query = query.Where("users.role = ?", f.Role)
	}
	if f.Registration != "" {
		query = query.Where(registrationExpr+" = ?", f.Registration)
	}
	column, direction := "users.created_at", "DESC"
	if f.Sort == "last_login_at" {
		column = "users.last_login_at"
	}
	if f.Asc {
		direction = "ASC"
	}
	// 从未登录（NULL）无论升降序都排在最后；id 兜底保证分页稳定。
	var rows []AdminUserRow
	err := query.Order(column + " " + direction + " NULLS LAST, users.id " + direction).Limit(f.Limit).Offset(f.Offset).Find(&rows).Error
	return rows, err
}

// IDByUsername 按用户 ID（username，大小写不敏感）解析主键。
func (r *AdminUserRepo) IDByUsername(ctx context.Context, username string) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.db.WithContext(ctx).Model(&User{}).Select("id").Where("lower(username) = lower(?)", strings.TrimSpace(username)).Limit(1).Row().Scan(&id)
	if err != nil {
		return uuid.Nil, notFound(err)
	}
	return id, nil
}

// AdminUserDetail 是用户详情；奖励余额与日志覆盖情况由 API 层补充。
type AdminUserDetail struct {
	AdminUserRow
	LinuxDo *AdminLinuxDoBinding `json:"linuxdo,omitempty"`
	Stats   AdminUserPlayStats   `json:"stats"`
}

type AdminLinuxDoBinding struct {
	Username   string    `json:"username"`
	TrustLevel *int      `json:"trust_level,omitempty"`
	BoundAt    time.Time `json:"bound_at"`
}

// AdminUserPlayStats 中 Started 为全部对话数（含已归档/删除），Completed 为已结算的挑战局数。
type AdminUserPlayStats struct {
	Started   int64 `json:"started"`
	Completed int64 `json:"completed"`
	Success   int64 `json:"success"`
	Failure   int64 `json:"failure"`
	Neutral   int64 `json:"neutral"`
}

func (r *AdminUserRepo) Detail(ctx context.Context, id uuid.UUID) (*AdminUserDetail, error) {
	db := r.db.WithContext(ctx)
	var detail AdminUserDetail
	err := db.Table("users").Select("users.*, "+registrationExpr+" AS registration_method").Where("users.id = ?", id).Take(&detail.AdminUserRow).Error
	if err != nil {
		return nil, notFound(err)
	}
	var binding AdminLinuxDoBinding
	res := db.Table("auth_identities").Select("username, trust_level, created_at AS bound_at").
		Where("user_id = ? AND provider = 'linuxdo'", id).Limit(1).Scan(&binding)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected > 0 {
		detail.LinuxDo = &binding
	}
	if err := db.Raw(`SELECT
		(SELECT count(*) FROM conversations WHERE player_id = ?) AS started,
		count(*) AS completed,
		count(*) FILTER (WHERE category = 'success') AS success,
		count(*) FILTER (WHERE category = 'failure') AS failure,
		count(*) FILTER (WHERE category = 'neutral') AS neutral
		FROM challenge_runs WHERE player_id = ?`, id, id).Scan(&detail.Stats).Error; err != nil {
		return nil, err
	}
	return &detail, nil
}

// AdminUserConversation 是只含“已采集详细日志”的对话摘要。
type AdminUserConversation struct {
	ID           uuid.UUID `json:"id"`
	Title        string    `json:"title"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
	RecordCount  int64     `json:"record_count"`
	LastRecordAt time.Time `json:"last_record_at"`
}

const capturedBodies = "body_status IN ('captured','truncated')"

// CapturedConversations 列出该用户拥有、且存在已采集正文的对话，按最近一条记录倒序。
func (r *AdminUserRepo) CapturedConversations(ctx context.Context, userID uuid.UUID, limit, offset int) ([]AdminUserConversation, error) {
	rows := []AdminUserConversation{}
	err := r.db.WithContext(ctx).Raw(`SELECT c.id, c.title, c.status, c.created_at, l.record_count, l.last_record_at
		FROM conversations c
		JOIN (SELECT conversation_id, count(*) AS record_count, max(created_at) AS last_record_at
			FROM llm_request_logs WHERE user_id = ? AND `+capturedBodies+` AND conversation_id IS NOT NULL
			GROUP BY conversation_id) l ON l.conversation_id = c.id
		WHERE c.player_id = ?
		ORDER BY l.last_record_at DESC, c.id DESC LIMIT ? OFFSET ?`, userID, userID, limit, offset).Scan(&rows).Error
	return rows, err
}

// CapturedRecords 按时间正序返回该用户某个对话中已采集正文的模型调用；对话不属于该用户时返回 ErrNotFound。
func (r *AdminUserRepo) CapturedRecords(ctx context.Context, userID, conversationID uuid.UUID, limit, offset int) ([]LLMRequestLog, error) {
	db := r.db.WithContext(ctx)
	var owned bool
	if err := db.Raw("SELECT EXISTS (SELECT 1 FROM conversations WHERE id = ? AND player_id = ?)", conversationID, userID).Scan(&owned).Error; err != nil {
		return nil, err
	}
	if !owned {
		return nil, ErrNotFound
	}
	var rows []LLMRequestLog
	err := db.Select("id", "purpose", "model", "success", "error", "created_at", "body_status", "audit_truncated", "request_body", "response_text").
		Where("user_id = ? AND conversation_id = ? AND "+capturedBodies, userID, conversationID).
		Order("created_at ASC, id ASC").Limit(limit).Offset(offset).Find(&rows).Error
	return rows, err
}

// RecordAudit 写一条以 target 为对象的管理审计，例如查看用户对话记录。
func (r *AdminUserRepo) RecordAudit(ctx context.Context, actor, target uuid.UUID, action string, metadata JSONMap) error {
	return r.db.WithContext(ctx).Create(&AdminAuditLog{
		ID: uuid.New(), ActorUserID: &actor, TargetUserID: &target, Action: action,
		Result: "success", Metadata: metadata, CreatedAt: time.Now(),
	}).Error
}

// UserRef 是日志里引用用户的最小展示信息：界面只显示 Username。
type UserRef struct {
	ID       uuid.UUID `json:"id"`
	Username string    `json:"username"`
}

// UserRefs 批量按主键取用户展示信息；已删除的用户不在结果中。
func (r *AdminUserRepo) UserRefs(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]UserRef, error) {
	refs := map[uuid.UUID]UserRef{}
	if len(ids) == 0 {
		return refs, nil
	}
	var rows []UserRef
	if err := r.db.WithContext(ctx).Table("users").Select("id, username").Where("id IN ?", ids).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		refs[row.ID] = row
	}
	return refs, nil
}

// ConversationUsers 批量取对话所属玩家，键为对话 ID。
func (r *AdminUserRepo) ConversationUsers(ctx context.Context, conversationIDs []uuid.UUID) (map[uuid.UUID]UserRef, error) {
	refs := map[uuid.UUID]UserRef{}
	if len(conversationIDs) == 0 {
		return refs, nil
	}
	var rows []struct {
		ConversationID uuid.UUID
		UserRef
	}
	err := r.db.WithContext(ctx).Table("conversations c").Select("c.id AS conversation_id, u.id, u.username").
		Joins("JOIN users u ON u.id = c.player_id").Where("c.id IN ?", conversationIDs).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		refs[row.ConversationID] = row.UserRef
	}
	return refs, nil
}

func notFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
