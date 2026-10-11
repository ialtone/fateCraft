package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/operation"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	AgentStepRunning   = "running"
	AgentStepCompleted = "completed"
	AgentStepFailed    = "failed"
	AgentStepCancelled = "cancelled"

	AgentLaneUndecided = "undecided"
	AgentLaneText      = "text"
	AgentLaneTool      = "tool"

	ToolInvocationReceived  = "received"
	ToolInvocationRejected  = "rejected"
	ToolInvocationCommitted = "committed"
	ToolInvocationFailed    = "failed"
	ToolInvocationAbandoned = "abandoned"
)

type AgentRepo struct{ db *gorm.DB }

func NewAgentRepo(db *gorm.DB) *AgentRepo { return &AgentRepo{db: db} }

type AgentActivity struct {
	AttemptID uuid.UUID
	StepNo    int
	Phase     string
	ToolName  string
}

type AgentStepMetrics struct {
	PromptTokens     int
	CompletionTokens int
	TokenUsageSource string
	FirstEventMS     int64
	FirstTextMS      int64
	ModelDurationMS  int64
	GenerationMS     int64
}

type AgentAttemptPerformance struct {
	Attempt   GenerationAttempt
	Steps     []AgentStep
	ToolCalls int64
}

// CurrentActivity reconstructs the public coordinator phase from durable
// rows. It is intentionally a projection: WebSocket deltas remain ephemeral,
// while reconnects can always recover whether the model or a tool is active.
func (r *AgentRepo) CurrentActivity(ctx context.Context, conversationID uuid.UUID) (*AgentActivity, error) {
	var attempt GenerationAttempt
	err := r.db.WithContext(ctx).Select("id").Where("conversation_id = ? AND status = ?", conversationID, AttemptStatusRunning).
		Order("started_at DESC").First(&attempt).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	activity := &AgentActivity{AttemptID: attempt.ID, Phase: "preparing"}
	var invocation AgentToolInvocation
	err = r.db.WithContext(ctx).Select("tool_name", "step_id").Where("attempt_id = ? AND status = ?", attempt.ID, ToolInvocationReceived).
		Order("created_at DESC").First(&invocation).Error
	if err == nil {
		activity.Phase = "tool"
		activity.ToolName = invocation.ToolName
		var step AgentStep
		if loadErr := r.db.WithContext(ctx).Select("step_no").First(&step, "id = ?", invocation.StepID).Error; loadErr == nil {
			activity.StepNo = step.StepNo
		}
		return activity, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	var step AgentStep
	err = r.db.WithContext(ctx).Select("step_no", "status").Where("attempt_id = ?", attempt.ID).Order("step_no DESC").First(&step).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return activity, nil
	}
	if err != nil {
		return nil, err
	}
	activity.StepNo = step.StepNo
	if step.Status == AgentStepRunning {
		activity.Phase = "model"
	}
	return activity, nil
}

func (r *AgentRepo) PinAttempt(ctx context.Context, attemptID uuid.UUID, catalogVersion, runtimeHash string) error {
	result := r.db.WithContext(ctx).Model(&GenerationAttempt{}).
		Where("id = ? AND status = ? AND (tool_catalog_version = '' OR tool_catalog_version = ?)", attemptID, AttemptStatusRunning, catalogVersion).
		Updates(map[string]any{"tool_catalog_version": catalogVersion, "runtime_hash": runtimeHash})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrNotFound
	}
	return nil
}

func (r *AgentRepo) CreateStep(ctx context.Context, step *AgentStep) error {
	metadata, ok := operation.FromContext(ctx)
	if !ok {
		return errors.New("Agent step 缺少 attempt 元数据")
	}
	step.TurnID = metadata.TurnID
	step.AttemptID = metadata.AttemptID
	if step.ID == uuid.Nil {
		step.ID = uuid.New()
	}
	if step.Status == "" {
		step.Status = AgentStepRunning
	}
	if step.OutputLane == "" {
		step.OutputLane = AgentLaneUndecided
	}
	if len(step.ToolSnapshot) == 0 {
		step.ToolSnapshot = JSONDoc("[]")
	}
	if step.StartedAt.IsZero() {
		step.StartedAt = time.Now()
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := AssertGenerationActive(ctx, tx, step.ConversationID); err != nil {
			return err
		}
		if err := tx.Create(step).Error; err != nil {
			return err
		}
		result := tx.Model(&GenerationAttempt{}).
			Where("id = ? AND status = ?", metadata.AttemptID, AttemptStatusRunning).
			Update("step_count", step.StepNo)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrNotFound
		}
		return nil
	})
}

func (r *AgentRepo) FinishStep(ctx context.Context, stepID uuid.UUID, status, lane, finishReason, errorCode string, metrics AgentStepMetrics) error {
	now := time.Now()
	metadata, _ := operation.FromContext(ctx)
	update := func(db *gorm.DB) error {
		if metadata.ConversationID != uuid.Nil {
			if err := AssertGenerationActive(ctx, db, metadata.ConversationID); err != nil {
				return err
			}
		}
		result := db.WithContext(ctx).Model(&AgentStep{}).
			Where("id = ? AND status = ?", stepID, AgentStepRunning).
			Updates(map[string]any{
				"status": status, "output_lane": lane, "finish_reason": finishReason,
				"error_code": errorCode, "prompt_tokens": metrics.PromptTokens,
				"completion_tokens": metrics.CompletionTokens, "token_usage_source": metrics.TokenUsageSource,
				"first_event_ms": metrics.FirstEventMS, "first_text_ms": metrics.FirstTextMS,
				"model_duration_ms": metrics.ModelDurationMS, "generation_ms": metrics.GenerationMS,
				"finished_at": now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrNotFound
		}
		return nil
	}
	if _, ok := operation.GenerationFromContext(ctx); ok {
		return r.db.WithContext(ctx).Transaction(update)
	}
	return update(r.db)
}

// LatestAttemptPerformance returns the newest completed, non-invalidated turn attempt and
// its durable model/tool measurements for the player runtime panel.
func (r *AgentRepo) LatestAttemptPerformance(ctx context.Context, conversationID uuid.UUID) (*AgentAttemptPerformance, error) {
	type attemptProjection struct {
		ID         uuid.UUID
		Status     string
		StartedAt  time.Time
		FinishedAt *time.Time
		ToolCalls  int64
	}
	var latest attemptProjection
	result := r.db.WithContext(ctx).Table("generation_attempts").
		Select("generation_attempts.id", "generation_attempts.status", "generation_attempts.started_at", "generation_attempts.finished_at", "(SELECT COUNT(*) FROM agent_tool_invocations WHERE agent_tool_invocations.attempt_id = generation_attempts.id AND agent_tool_invocations.status = 'committed') AS tool_calls").
		Joins("JOIN conversation_turns ON conversation_turns.id = generation_attempts.turn_id").
		Where("generation_attempts.conversation_id = ? AND conversation_turns.invalidated_at IS NULL AND generation_attempts.status = ? AND conversation_turns.status = ?", conversationID, AttemptStatusCompleted, TurnStatusCompleted).
		Order("generation_attempts.started_at DESC").Limit(1).Scan(&latest)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	var steps []AgentStep
	if err := r.db.WithContext(ctx).Model(&AgentStep{}).
		Select("model", "prompt_tokens", "completion_tokens", "token_usage_source", "first_event_ms", "model_duration_ms", "generation_ms", "finished_at").
		Where("attempt_id = ? AND status = ?", latest.ID, AgentStepCompleted).Order("step_no ASC").Find(&steps).Error; err != nil {
		return nil, err
	}
	attempt := GenerationAttempt{
		ID: latest.ID, Status: latest.Status, StartedAt: latest.StartedAt, FinishedAt: latest.FinishedAt,
	}
	return &AgentAttemptPerformance{Attempt: attempt, Steps: steps, ToolCalls: latest.ToolCalls}, nil
}

// ClaimInvocation persists a complete wire call before any domain handler is
// entered. A duplicate provider call id returns the authoritative existing row.
func (r *AgentRepo) ClaimInvocation(ctx context.Context, invocation *AgentToolInvocation) (*AgentToolInvocation, bool, error) {
	metadata, ok := operation.FromContext(ctx)
	if !ok {
		return nil, false, errors.New("工具调用缺少 attempt 元数据")
	}
	invocation.TurnID = metadata.TurnID
	invocation.AttemptID = metadata.AttemptID
	if metadata.ConversationID != uuid.Nil {
		if err := AssertGenerationActive(ctx, r.db, metadata.ConversationID); err != nil {
			return nil, false, err
		}
	}
	if invocation.ID == uuid.Nil {
		invocation.ID = uuid.New()
	}
	if invocation.Arguments == nil {
		invocation.Arguments = JSONMap{}
	}
	if invocation.Result == nil {
		invocation.Result = JSONMap{}
	}
	invocation.Status = ToolInvocationReceived
	now := time.Now()
	invocation.CreatedAt = now
	invocation.UpdatedAt = now
	create := func(db *gorm.DB) error { return db.WithContext(ctx).Create(invocation).Error }
	var err error
	if _, ok := operation.GenerationFromContext(ctx); ok {
		err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if guardErr := AssertGenerationActive(ctx, tx, metadata.ConversationID); guardErr != nil {
				return guardErr
			}
			return create(tx)
		})
	} else {
		err = create(r.db)
	}
	if err == nil {
		return invocation, false, nil
	}
	// A generation that lost its durable lease must never be treated as a
	// duplicate provider call. Replaying an existing invocation here would let
	// stale work continue through the tool loop after End/Archive/Delete.
	if errors.Is(err, ErrGenerationNotActive) {
		return nil, false, err
	}
	var existing AgentToolInvocation
	loadErr := r.db.WithContext(ctx).
		Where("attempt_id = ? AND call_id = ?", metadata.AttemptID, invocation.CallID).
		First(&existing).Error
	if loadErr != nil {
		return nil, false, err
	}
	return &existing, true, nil
}

// FinishInvocation persists the provider-visible call/result pair and the
// invocation terminal state in one transaction. Domain handlers must already
// have completed before status=committed is supplied.
func (r *AgentRepo) FinishInvocation(
	ctx context.Context,
	invocationID uuid.UUID,
	status, resultCode string,
	result JSONMap,
	isMutating bool,
	toolCalls JSONDoc,
	toolResultText string,
) error {
	return r.FinishInvocationWithBlocks(ctx, invocationID, status, resultCode, result, isMutating, invocationBlocks(toolCalls), toolResultText)
}

// FinishInvocationWithBlocks is the canonical terminal transition. The first
// invocation supplies the complete model step; subsequent invocations pass nil
// and append only their output to that same declaration. Each invocation still
// owns an independent audit/result record and domain transaction.
func (r *AgentRepo) FinishInvocationWithBlocks(
	ctx context.Context,
	invocationID uuid.UUID,
	status, resultCode string,
	result JSONMap,
	isMutating bool,
	contentBlocks JSONDoc,
	toolResultText string,
) error {
	metadata, ok := operation.FromContext(ctx)
	if !ok {
		return errors.New("工具结果缺少 attempt 元数据")
	}
	if result == nil {
		result = JSONMap{}
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if metadata.ConversationID != uuid.Nil {
			if err := AssertGenerationActive(ctx, tx, metadata.ConversationID); err != nil {
				return err
			}
		}
		return finishInvocationInTx(ctx, tx, metadata.AttemptID, invocationID, status, resultCode, result, isMutating, contentBlocks, toolResultText)
	})
}

// FinishInvocationInCurrentTx is the same terminal transition without opening
// a nested transaction. The caller must pass a transaction that already owns
// the domain write represented by this invocation.
func (r *AgentRepo) FinishInvocationInCurrentTx(
	ctx context.Context,
	tx *gorm.DB,
	invocationID uuid.UUID,
	status, resultCode string,
	result JSONMap,
	isMutating bool,
	toolCalls JSONDoc,
	toolResultText string,
) error {
	return r.FinishInvocationInCurrentTxWithBlocks(ctx, tx, invocationID, status, resultCode, result, isMutating, invocationBlocks(toolCalls), toolResultText)
}

func (r *AgentRepo) FinishInvocationInCurrentTxWithBlocks(
	ctx context.Context,
	tx *gorm.DB,
	invocationID uuid.UUID,
	status, resultCode string,
	result JSONMap,
	isMutating bool,
	contentBlocks JSONDoc,
	toolResultText string,
) error {
	metadata, ok := operation.FromContext(ctx)
	if !ok {
		return errors.New("工具结果缺少 attempt 元数据")
	}
	if tx == nil {
		return errors.New("工具结果事务未配置")
	}
	if metadata.ConversationID != uuid.Nil {
		if err := AssertGenerationActive(ctx, tx, metadata.ConversationID); err != nil {
			return err
		}
	}
	return finishInvocationInTx(ctx, tx, metadata.AttemptID, invocationID, status, resultCode, result, isMutating, contentBlocks, toolResultText)
}

func finishInvocationInTx(
	ctx context.Context,
	tx *gorm.DB,
	attemptID, invocationID uuid.UUID,
	status, resultCode string,
	result JSONMap,
	isMutating bool,
	contentBlocks JSONDoc,
	toolResultText string,
) error {
	if result == nil {
		result = JSONMap{}
	}
	var invocation AgentToolInvocation
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		First(&invocation, "id = ? AND attempt_id = ?", invocationID, attemptID).Error; err != nil {
		return err
	}
	if invocation.Status != ToolInvocationReceived {
		if invocation.Status == status {
			return nil
		}
		return errors.New("工具调用已经进入终态")
	}
	now := time.Now()
	committedAt := (*time.Time)(nil)
	if status == ToolInvocationCommitted {
		committedAt = &now
	}
	updates := map[string]any{
		"status": status, "result_code": resultCode, "result_json": result,
		"is_mutating": isMutating, "committed_at": committedAt, "updated_at": now,
	}
	if err := tx.Model(&AgentToolInvocation{}).Where("id = ?", invocation.ID).Updates(updates).Error; err != nil {
		return err
	}
	if err := persistToolDeclaration(ctx, tx, invocation, contentBlocks, now); err != nil {
		return err
	}
	messages := NewMessageRepo(tx)
	toolResult := &ChatMessage{
		ID: uuid.New(), ConversationID: invocation.ConversationID, Role: RoleTool, Kind: "tool_result",
		Content: toolResultText, ContentBlocks: textContentBlocks(toolResultText), ToolCallID: invocation.CallID, ToolName: invocation.ToolName,
		StepID: &invocation.StepID, ToolInvocationID: &invocation.ID, CreatedAt: now, UpdatedAt: now,
	}
	if err := messages.CreateChat(ctx, toolResult); err != nil {
		return err
	}
	if status == ToolInvocationCommitted && isMutating {
		update := tx.Model(&GenerationAttempt{}).
			Where("id = ? AND status = ?", attemptID, AttemptStatusRunning).
			UpdateColumn("committed_mutating_tool_count", gorm.Expr("committed_mutating_tool_count + 1"))
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return ErrNotFound
		}
	}
	return nil
}

func persistToolDeclaration(ctx context.Context, tx *gorm.DB, invocation AgentToolInvocation, blocks JSONDoc, now time.Time) error {
	var declarations []ChatMessage
	if err := tx.WithContext(ctx).Select("id", "content_blocks").
		Where("conversation_id = ? AND attempt_id = ? AND step_id = ? AND kind = ?", invocation.ConversationID, invocation.AttemptID, invocation.StepID, "tool_call").
		Limit(2).Find(&declarations).Error; err != nil {
		return err
	}
	if len(blocks) == 0 {
		if len(declarations) != 1 {
			return errors.New("后续工具缺少唯一的本步骤调用声明")
		}
		return validateInvocationDeclaration(declarations[0].ContentBlocks, invocation)
	}
	if len(declarations) != 0 {
		return errors.New("同一步骤不能重复写入工具调用声明")
	}
	if err := validateInvocationDeclaration(blocks, invocation); err != nil {
		return err
	}
	return NewMessageRepo(tx).CreateChat(ctx, &ChatMessage{
		ID: uuid.New(), ConversationID: invocation.ConversationID, Role: RoleAssistant, Kind: "tool_call",
		Content: "", ContentBlocks: blocks, ToolCallID: invocation.CallID, ToolName: invocation.ToolName,
		StepID: &invocation.StepID, ToolInvocationID: &invocation.ID, CreatedAt: now, UpdatedAt: now,
	})
}

// A result can only refer to a call from the same immutable model step. Verify
// the raw argument hash as well, including malformed arguments that were rejected.
func validateInvocationDeclaration(raw JSONDoc, invocation AgentToolInvocation) error {
	var blocks []struct {
		Kind string `json:"kind"`
		Call *struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"tool_call"`
	}
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return errors.New("工具调用声明不是有效内容数组")
	}
	seen := map[string]bool{}
	matched := false
	for _, block := range blocks {
		if block.Kind != "tool_call" {
			continue
		}
		if block.Call == nil || block.Call.ID == "" || seen[block.Call.ID] {
			return errors.New("工具调用声明缺少或重复 call_id")
		}
		seen[block.Call.ID] = true
		if block.Call.ID == invocation.CallID {
			hash := sha256.Sum256([]byte(block.Call.Arguments))
			if block.Call.Name != invocation.ToolName || hex.EncodeToString(hash[:]) != invocation.ArgumentsHash {
				return errors.New("工具执行与本步骤声明的名称或参数不一致")
			}
			matched = true
		}
	}
	if !matched {
		return errors.New("工具未包含在本步骤调用声明中")
	}
	return nil
}

func invocationBlocks(toolCalls JSONDoc) JSONDoc {
	var calls []json.RawMessage
	if json.Unmarshal(toolCalls, &calls) != nil {
		return JSONDoc("[]")
	}
	blocks := make([]map[string]any, 0, len(calls))
	for _, call := range calls {
		blocks = append(blocks, map[string]any{"kind": "tool_call", "tool_call": json.RawMessage(call)})
	}
	return marshalContentBlocks(blocks)
}

func textContentBlocks(text string) JSONDoc {
	if text == "" {
		return JSONDoc("[]")
	}
	return marshalContentBlocks([]map[string]any{{"kind": "text", "text": text}})
}

func marshalContentBlocks(blocks any) JSONDoc {
	raw, err := json.Marshal(blocks)
	if err != nil || len(raw) == 0 {
		return JSONDoc("[]")
	}
	return JSONDoc(raw)
}

// MarkOrphanedInvocations 封口所有“已接收但其对话已无活动租约”的工具调用。
// 只看租约、不看实例：其他实例仍在生成的对话拥有活动租约，不会被误伤。
func (r *AgentRepo) MarkOrphanedInvocations(ctx context.Context) (int64, error) {
	now := time.Now()
	result := r.db.WithContext(ctx).Model(&AgentToolInvocation{}).
		Where("status = ?", ToolInvocationReceived).
		Where("NOT EXISTS (?)", r.db.Model(&ConversationGenerationRun{}).
			Select("1").Where("conversation_generation_runs.conversation_id = agent_tool_invocations.conversation_id AND status IN ?", activeGenerationRunStatuses())).
		Updates(map[string]any{
			"status": ToolInvocationAbandoned, "result_code": "generation_lost",
			"result_json": JSONMap{"ok": false, "code": "generation_lost", "message": "工具调用所属的生成已经失效，未提交"},
			"updated_at":  now,
		})
	return result.RowsAffected, result.Error
}
