package storage

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/operation"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ErrNotFound 表示记录不存在。
var ErrNotFound = errors.New("记录不存在")

var (
	ErrConversationNotActive     = errors.New("对话未处于 active 状态")
	ErrGenerationInProgress      = errors.New("对话已有进行中的生成任务")
	ErrGenerationNotActive       = errors.New("生成任务已失效")
	ErrGenerationRequestConflict = errors.New("生成请求 ID 已用于不同操作")
	ErrAccountDisabled           = errors.New("账号已停用，无法继续生成")
)

type GenerationRequest struct {
	RequestID       uuid.UUID
	OperationType   string
	TargetMessageID *uuid.UUID
	PayloadHash     string
}

// 生成租约由持有方定期心跳续期；超过 generationLeaseDuration 没有心跳即视为
// 持有进程已经死亡，可被新的生成请求抢占或被回收器封存。
const (
	generationLeaseDuration     = 2 * time.Minute
	GenerationHeartbeatInterval = 30 * time.Second
)

// ConversationRepo 负责对话运行时数据的持久化。
type ConversationRepo struct {
	db *gorm.DB
	// instanceID 标识当前后端实例，写入生成租约的 owner_instance_id。
	instanceID string
}

func NewConversationRepo(db *gorm.DB) *ConversationRepo {
	return &ConversationRepo{db: db, instanceID: uuid.NewString()}
}

// SetInstanceID 设置稳定的实例标识；同一实例重启后可立即回收自己遗留的租约。
func (r *ConversationRepo) SetInstanceID(id string) {
	if strings.TrimSpace(id) != "" {
		r.instanceID = strings.TrimSpace(id)
	}
}

func (r *ConversationRepo) InstanceID() string { return r.instanceID }

func (r *ConversationRepo) Create(ctx context.Context, c *Conversation) error {
	return r.db.WithContext(ctx).Create(c).Error
}

// CreateWithState creates a conversation and its initial state in one
// transaction. A failed state insert must not leave an apparently usable
// conversation behind.
func (r *ConversationRepo) CreateWithState(ctx context.Context, c *Conversation, state *ConversationState) error {
	if c == nil || state == nil || c.ID != state.ConversationID {
		return errors.New("对话与初始状态不匹配")
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(c).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "conversation_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"state", "schema_version", "updated_at"}),
		}).Create(state).Error; err != nil {
			return err
		}
		// Preserve the actual seeded opening for first-turn branch restoration.
		return tx.Create(&StateRevision{ID: uuid.New(), ConversationID: c.ID, Source: SourceSystem, Patch: JSONMap{}, BeforeState: state.State, AfterState: state.State, CreatedAt: state.UpdatedAt}).Error
	})
}

// Get 按 id 读取对话（包含已软删除的，便于恢复场景）。
func (r *ConversationRepo) Get(ctx context.Context, id uuid.UUID) (*Conversation, error) {
	var c Conversation
	err := r.db.WithContext(ctx).First(&c, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &c, err
}

// List 按玩家、状态过滤并支持标题/内容搜索，按 updated_at 倒序。
func (r *ConversationRepo) List(ctx context.Context, playerID string, statuses []string, search string) ([]Conversation, error) {
	return r.ListPage(ctx, playerID, statuses, search, 50)
}

// ListPage returns a bounded, stable conversation list. The id tie-breaker is
// required because PostgreSQL timestamps can be equal for multiple rows.
func (r *ConversationRepo) ListPage(ctx context.Context, playerID string, statuses []string, search string, limit int) ([]Conversation, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	q := r.db.WithContext(ctx).Where("player_id = ?", playerID)
	if len(statuses) > 0 {
		q = q.Where("status IN ?", statuses)
	} else {
		// 默认隐藏已删除对话。
		q = q.Where("status <> ?", StatusDeleted)
	}
	if search = strings.TrimSpace(search); search != "" {
		escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(search)
		q = q.Where(`title ILIKE ? ESCAPE '\'`, "%"+escaped+"%")
	}
	var out []Conversation
	err := q.Order("updated_at DESC, id DESC").Limit(limit).Find(&out).Error
	return out, err
}

// Save 全量保存对话。
func (r *ConversationRepo) Save(ctx context.Context, c *Conversation) error {
	return r.db.WithContext(ctx).Save(c).Error
}

// UpdateFields 局部更新字段。
func (r *ConversationRepo) UpdateFields(ctx context.Context, id uuid.UUID, fields map[string]any) error {
	if _, ok := operation.GenerationFromContext(ctx); ok {
		return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := AssertGenerationActive(ctx, tx, id); err != nil {
				return err
			}
			return tx.Model(&Conversation{}).Where("id = ?", id).Updates(fields).Error
		})
	}
	return r.db.WithContext(ctx).Model(&Conversation{}).Where("id = ?", id).Updates(fields).Error
}

// StartGeneration atomically validates an active conversation and creates its
// durable generation lease. The conversation row lock serializes this with
// lifecycle transitions, while the partial unique index protects against
// callers that bypass this repository transaction.
func (r *ConversationRepo) StartGeneration(ctx context.Context, id uuid.UUID, request GenerationRequest) (*Conversation, *ConversationGenerationRun, bool, error) {
	var conversation Conversation
	var run ConversationGenerationRun
	var replayed bool
	if request.RequestID == uuid.Nil || request.OperationType == "" || request.PayloadHash == "" {
		return nil, nil, false, errors.New("生成请求元数据不完整")
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&conversation, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		var existing ConversationGenerationRun
		existingErr := tx.First(&existing, "request_id = ?", request.RequestID).Error
		if existingErr == nil {
			if existing.ConversationID != id || existing.OperationType != request.OperationType || existing.PayloadHash != request.PayloadHash ||
				!sameOptionalUUID(existing.TargetMessageID, request.TargetMessageID) {
				return ErrGenerationRequestConflict
			}
			run = existing
			replayed = true
			return nil
		}
		if !errors.Is(existingErr, gorm.ErrRecordNotFound) {
			return existingErr
		}
		// Idempotent replay is valid even after the original operation made the
		// conversation terminal/archived. Only a brand-new request requires an
		// active conversation.
		if conversation.Status != StatusActive {
			return ErrConversationNotActive
		}
		// WebSocket 只在握手时鉴权；账号被停用后，已打开的连接不能继续发起新生成
		// （平台模型模式下会持续消耗平台 Key），因此在取得租约的事务里复查账号状态。
		var ownerStatuses []string
		if err := tx.Model(&User{}).Where("id = ?", conversation.PlayerID).Pluck("status", &ownerStatuses).Error; err != nil {
			return err
		}
		if len(ownerStatuses) != 1 || ownerStatuses[0] != UserStatusActive {
			return ErrAccountDisabled
		}

		var active ConversationGenerationRun
		activeErr := tx.Where("conversation_id = ? AND status IN ?", id, activeGenerationRunStatuses()).
			Order("started_at DESC, id DESC").First(&active).Error
		now := time.Now()
		if activeErr == nil {
			// 租约已过期说明持有方已经死亡（进程崩溃、实例下线）。抢占它，
			// 否则该对话会永远停留在“正在生成中”。
			if !active.LeaseExpiresAt.Before(now) {
				return ErrGenerationInProgress
			}
			if err := tx.Model(&ConversationGenerationRun{}).Where("id = ?", active.ID).
				Updates(map[string]any{"status": GenerationRunAbandoned, "error_code": "lease_expired", "finished_at": now, "last_heartbeat_at": now}).Error; err != nil {
				return err
			}
			// 被抢占的 run 遗留的 pending 回合、running attempt 和未完成工具调用
			// 必须在同一事务里封口，否则新请求会一直因 ErrTurnInProgress 失败。
			if err := sealLostGenerationInTx(tx, id, now); err != nil {
				return err
			}
		} else if !errors.Is(activeErr, gorm.ErrRecordNotFound) {
			return activeErr
		}

		run = ConversationGenerationRun{
			ID:                   uuid.New(),
			ConversationID:       id,
			RequestID:            request.RequestID,
			OperationType:        request.OperationType,
			TargetMessageID:      request.TargetMessageID,
			PayloadHash:          request.PayloadHash,
			ConversationRevision: conversation.BranchEpoch,
			Status:               GenerationRunRunning,
			OwnerInstanceID:      r.instanceID,
			StartedAt:            now,
			LastHeartbeatAt:      now,
			LeaseExpiresAt:       now.Add(generationLeaseDuration),
		}
		if err := tx.Create(&run).Error; err != nil {
			return err
		}
		result := tx.Model(&Conversation{}).Where("id = ?", id).
			Update("is_processing", true)
		if result.Error != nil {
			return result.Error
		}
		conversation.IsProcessing = true
		return nil
	})
	if err != nil {
		return nil, nil, false, err
	}
	return &conversation, &run, replayed, nil
}

// HeartbeatGeneration 续期仍然活动的生成租约。返回 ErrGenerationNotActive
// 表示租约已被回收或抢占，持有方必须立即停止写入。
func (r *ConversationRepo) HeartbeatGeneration(ctx context.Context, runID uuid.UUID) error {
	var binding struct {
		ChallengeVersionID uuid.UUID
		ChallengeHash      string
	}
	if err := r.db.WithContext(ctx).Raw("SELECT c.challenge_version_id,c.challenge_hash FROM conversations c JOIN conversation_generation_runs g ON g.conversation_id=c.id WHERE g.id=?", runID).Scan(&binding).Error; err != nil {
		return err
	}
	if binding.ChallengeVersionID != uuid.Nil {
		if err := NewChallengeRepository(r.db).CheckVersionRun(ctx, binding.ChallengeVersionID, binding.ChallengeHash); err != nil {
			return err
		}
	}
	now := time.Now()
	result := r.db.WithContext(ctx).Model(&ConversationGenerationRun{}).
		Where("id = ? AND status IN ?", runID, activeGenerationRunStatuses()).
		Updates(map[string]any{"last_heartbeat_at": now, "lease_expires_at": now.Add(generationLeaseDuration)})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrGenerationNotActive
	}
	return nil
}

func sameOptionalUUID(left, right *uuid.UUID) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

// GenerationRequestExists 判断该对话是否已有使用此 request_id 的 durable 生成记录。
func (r *ConversationRepo) GenerationRequestExists(ctx context.Context, id, requestID uuid.UUID) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).Model(&ConversationGenerationRun{}).
		Where("conversation_id = ? AND request_id = ?", id, requestID).Count(&count).Error
	return count > 0, err
}

// AssertNoActiveGeneration checks an already-locked conversation against the
// durable run owner. Callers that mutate a branch must take the conversation
// row lock before invoking this helper so a new run cannot slip in afterward.
func AssertNoActiveGeneration(ctx context.Context, db *gorm.DB, id uuid.UUID) error {
	var active ConversationGenerationRun
	err := db.WithContext(ctx).
		Where("conversation_id = ? AND status IN ?", id, activeGenerationRunStatuses()).
		Order("started_at DESC, id DESC").First(&active).Error
	if err == nil {
		return ErrGenerationInProgress
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	return err
}

// FinishGeneration releases only the supplied run. A stale worker can never
// clear a newer run because the run id is part of the conditional transition.
// 加锁顺序与 CancelGeneration / TransitionLifecycle 一致：先对话行，再 run 行，
// 避免用户在生成刚结束的瞬间点取消时触发 PostgreSQL 死锁。
func (r *ConversationRepo) FinishGeneration(ctx context.Context, runID uuid.UUID, status, errorCode string) error {
	if runID == uuid.Nil {
		return ErrGenerationNotActive
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var peek ConversationGenerationRun
		if err := tx.Select("id", "conversation_id").First(&peek, "id = ?", runID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrGenerationNotActive
			}
			return err
		}
		var conversation Conversation
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").First(&conversation, "id = ?", peek.ConversationID).Error; err != nil {
			return err
		}
		var run ConversationGenerationRun
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&run, "id = ?", runID).Error; err != nil {
			return err
		}
		if !isGenerationRunActive(run.Status) {
			return nil
		}
		if !isGenerationRunTerminal(status) {
			return errors.New("无效的生成任务终态")
		}
		now := time.Now()
		result := tx.Model(&ConversationGenerationRun{}).
			Where("id = ? AND status IN ?", runID, activeGenerationRunStatuses()).
			Updates(map[string]any{"status": status, "error_code": errorCode, "finished_at": now, "last_heartbeat_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return nil
		}
		return tx.Model(&Conversation{}).
			Where("id = ? AND is_processing = ?", run.ConversationID, true).
			Where("NOT EXISTS (?)", tx.Model(&ConversationGenerationRun{}).
				Select("1").Where("conversation_id = ? AND status IN ?", run.ConversationID, activeGenerationRunStatuses())).
			Update("is_processing", false).Error
	})
}

// CancelGeneration marks the active run as cancellation requested. The
// processing flag remains set until the worker really exits.
func (r *ConversationRepo) CancelGeneration(ctx context.Context, id uuid.UUID, reason string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var conversation Conversation
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&conversation, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		result := tx.Model(&ConversationGenerationRun{}).
			Where("conversation_id = ? AND status = ?", id, GenerationRunRunning).
			Updates(map[string]any{"status": GenerationRunCancelRequested, "error_code": reason, "last_heartbeat_at": time.Now()})
		return result.Error
	})
}

// TransitionLifecycle changes a conversation status while serializing with an
// active generation. End/archive/delete request cancellation and keep
// is_processing true until the owning worker finishes; restore refuses to race
// an existing run.
func (r *ConversationRepo) TransitionLifecycle(ctx context.Context, id uuid.UUID, status string, fields map[string]any) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var conversation Conversation
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&conversation, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		// 源状态必须在行锁内按状态机校验：已删除对话一律视为不存在；ended/archived
		// 只能来自 active，恢复只能来自 archived。锁外的检查只是快速失败。
		if conversation.Status == StatusDeleted {
			return ErrNotFound
		}
		if !lifecycleTransitionAllowed(conversation.Status, status) {
			return ErrConversationNotActive
		}
		var activeCount int64
		if err := tx.Model(&ConversationGenerationRun{}).
			Where("conversation_id = ? AND status IN ?", id, activeGenerationRunStatuses()).Count(&activeCount).Error; err != nil {
			return err
		}
		if status == StatusActive && activeCount > 0 {
			return ErrGenerationInProgress
		}
		if fields == nil {
			fields = map[string]any{}
		}
		fields["status"] = status
		if activeCount == 0 {
			fields["is_processing"] = false
		}
		if err := tx.Model(&Conversation{}).Where("id = ?", id).Updates(fields).Error; err != nil {
			return err
		}
		if status != StatusActive && activeCount > 0 {
			if err := tx.Model(&ConversationGenerationRun{}).
				Where("conversation_id = ? AND status = ?", id, GenerationRunRunning).
				Updates(map[string]any{"status": GenerationRunCancelRequested, "error_code": "conversation_" + status, "last_heartbeat_at": time.Now()}).Error; err != nil {
				return err
			}
		}
		if status == StatusDeleted {
			now := time.Now().UTC()
			if err := tx.Model(&ConversationShare{}).
				Where("conversation_id = ? AND status = 'active'", id).
				Updates(map[string]any{"status": "revoked", "revoked_at": now}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func activeGenerationRunStatuses() []string {
	return []string{GenerationRunRunning, GenerationRunCancelRequested}
}

// lifecycleTransitionAllowed 定义对话生命周期状态机：ended 与 archived 只能从
// active 进入，恢复只能从 archived 回到 active，删除可以来自任何未删除状态。
func lifecycleTransitionAllowed(from, to string) bool {
	switch to {
	case StatusEnded, StatusArchived:
		return from == StatusActive
	case StatusActive:
		return from == StatusArchived
	case StatusDeleted:
		return from != StatusDeleted
	default:
		return false
	}
}

func isGenerationRunActive(status string) bool {
	return status == GenerationRunRunning || status == GenerationRunCancelRequested
}

func isGenerationRunTerminal(status string) bool {
	switch status {
	case GenerationRunCompleted, GenerationRunFailed, GenerationRunCancelled, GenerationRunAbandoned:
		return true
	default:
		return false
	}
}

// AssertGenerationActive must be called by generation write boundaries. The
// query locks both rows when used inside a transaction, so a lifecycle change
// cannot pass between validation and the protected write.
func AssertGenerationActive(ctx context.Context, db *gorm.DB, conversationID uuid.UUID) error {
	generation, ok := operation.GenerationFromContext(ctx)
	if !ok {
		return nil
	}
	if generation.ConversationID != conversationID {
		return ErrGenerationNotActive
	}
	var row struct{ ID uuid.UUID }
	queryText := `
		SELECT c.id
		FROM conversations c
		JOIN conversation_generation_runs r ON r.conversation_id = c.id
		WHERE c.id = ? AND r.id = ? AND c.status = ? AND r.status = ?
		FOR UPDATE`
	args := []any{conversationID, generation.ID, StatusActive, GenerationRunRunning}
	// 一旦 attempt 已存在，围栏就不能只看 durable run：分支改写可能在旧模型请求
	// 尚未退出时作废其 turn/attempt，因此要求 attempt 仍在运行且绑定当前分支，
	// 阻止迟到的旧任务写入事实。
	// 本 attempt 自己完成挑战或 Campaign 结算后，对话会在同一工具事务里变为 ended；此后
	// 同一 attempt 仍需写入工具结果、收尾叙事和步骤记录，所以放行“由本 attempt
	// 结算而结束”的对话，其他非 active 状态一律拒绝。
	if metadata, hasMetadata := operation.FromContext(ctx); hasMetadata {
		queryText = `
			SELECT c.id
			FROM conversations c
			JOIN conversation_generation_runs r ON r.conversation_id = c.id
			JOIN generation_attempts a ON a.conversation_id = c.id
			JOIN conversation_turns t ON t.conversation_id = c.id AND t.id = a.turn_id
			WHERE c.id = ? AND r.id = ? AND r.status = ?
			  AND (c.status = ? OR (c.status = ? AND (EXISTS (
			        SELECT 1 FROM challenge_runs cr
			        WHERE cr.conversation_id = c.id AND cr.attempt_id = a.id)
			      OR EXISTS (
			        SELECT 1 FROM campaigns cp
			        JOIN campaign_action_commits cc ON cc.campaign_id = cp.id AND cc.revision = cp.revision
			        WHERE cp.id = c.campaign_id AND cp.status <> 'active'
			          AND cc.attempt_id = a.id AND cc.turn_id = t.id))))
			  AND a.id = ? AND a.status = ? AND a.branch_epoch = c.branch_epoch
			  AND t.status = ? AND t.invalidated_at IS NULL
			FOR UPDATE OF c, r, a, t`
		args = []any{conversationID, generation.ID, GenerationRunRunning,
			StatusActive, StatusEnded,
			metadata.AttemptID, AttemptStatusRunning, TurnStatusPending}
	}
	query := db.WithContext(ctx).Raw(queryText, args...)
	if err := query.Row().Scan(&row.ID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrGenerationNotActive
		}
		return err
	}
	return nil
}

// FailActiveGeneration 封存本实例在该对话上仍处于活动状态的租约（生成 goroutine
// panic 后的兜底）。只处理 owner_instance_id 为本实例的 run，避免多实例部署下误杀
// 其他实例刚为同一对话启动的新生成；忙碌投影也只在不存在其他活动 run 时才清除。
func (r *ConversationRepo) FailActiveGeneration(ctx context.Context, id uuid.UUID, errorCode string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var conversation Conversation
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").First(&conversation, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		now := time.Now()
		if err := tx.Model(&ConversationGenerationRun{}).
			Where("conversation_id = ? AND status IN ? AND owner_instance_id = ?", id, activeGenerationRunStatuses(), r.instanceID).
			Updates(map[string]any{"status": GenerationRunFailed, "error_code": errorCode, "finished_at": now, "last_heartbeat_at": now}).Error; err != nil {
			return err
		}
		return tx.Model(&Conversation{}).
			Where("id = ? AND is_processing = ?", id, true).
			Where("NOT EXISTS (?)", tx.Model(&ConversationGenerationRun{}).
				Select("1").Where("conversation_id = ? AND status IN ?", id, activeGenerationRunStatuses())).
			Update("is_processing", false).Error
	})
}

// ReapOwnedGenerations is a startup-only recovery operation. A stable instance
// ID lets a restarted process immediately abandon work left by its predecessor,
// but calling this from the periodic reaper would cancel this process's live runs.
func (r *ConversationRepo) ReapOwnedGenerations(ctx context.Context) (int64, error) {
	return r.reapGenerations(ctx, "instance_restarted", "owner_instance_id = ?", r.instanceID)
}

// ReapStaleGenerations is safe to run periodically: it only abandons leases
// whose heartbeat deadline has actually passed, regardless of their owner.
func (r *ConversationRepo) ReapStaleGenerations(ctx context.Context) (int64, error) {
	return r.reapGenerations(ctx, "lease_expired", "lease_expires_at < ?", time.Now())
}

func (r *ConversationRepo) reapGenerations(ctx context.Context, errorCode, predicate string, args ...any) (int64, error) {
	now := time.Now()
	var count int64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&ConversationGenerationRun{}).
			Where("status IN ?", activeGenerationRunStatuses()).
			Where(predicate, args...).
			Updates(map[string]any{"status": GenerationRunAbandoned, "error_code": errorCode, "finished_at": now, "last_heartbeat_at": now})
		if result.Error != nil {
			return result.Error
		}
		count = result.RowsAffected
		return tx.Model(&Conversation{}).
			Where("is_processing = ?", true).
			Where("NOT EXISTS (?)", tx.Model(&ConversationGenerationRun{}).
				Select("1").Where("conversation_generation_runs.conversation_id = conversations.id AND status IN ?", activeGenerationRunStatuses())).
			Update("is_processing", false).Error
	})
	return count, err
}
