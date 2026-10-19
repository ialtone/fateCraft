package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	gameplay "github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/operation"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrCampaignNotActive           = errors.New("长期存档未处于 active 状态")
	ErrCampaignRevisionConflict    = errors.New("长期存档 revision 冲突")
	ErrCampaignIdempotencyConflict = errors.New("领域动作幂等键已被其他动作使用")
	ErrCampaignHasConversation     = errors.New("长期存档已经绑定对话")
	ErrCampaignOutcomeMissing      = errors.New("Campaign 终局缺少 outcome_key")
)

type CampaignEventDraft struct {
	EventType  string
	Visibility string
	Payload    JSONMap
}

type CampaignActionOutcome struct {
	Events             []CampaignEventDraft
	GameTimeDelta      int64
	CurrentLocationKey *string
	Status             *string
	OutcomeKey         *string
	Result             JSONMap
	Roll               *CampaignRollSnapshot
}

// CampaignRollSnapshot is the authoritative roll produced while resolving a
// domain action. CommitAction persists it with the domain changes so a roll
// can never disagree with the state transition it decided.
type CampaignRollSnapshot struct {
	RollKey  string
	Title    string
	Category string
	Success  bool
	Content  string
	Markdown string
	Metadata JSONMap
}

type CommitCampaignActionInput struct {
	CampaignID       uuid.UUID
	PlayerID         uuid.UUID
	ExpectedRevision int64
	IdempotencyKey   string
	ActionKey        string
	Label            string
	ActorType        string
	ActorID          *string
	SourceMessageID  *uuid.UUID
}

type CampaignActionResult struct {
	Campaign    Campaign               `json:"campaign"`
	Events      []CampaignEvent        `json:"events"`
	Result      JSONMap                `json:"result"`
	Narrative   *gameplay.ActionResult `json:"narrative,omitempty"`
	RollMessage *DisplayMessage        `json:"roll_message,omitempty"`
	Replayed    bool                   `json:"replayed"`
}

type CampaignInitializer func(*gorm.DB, *Campaign) error

type CampaignInitialization struct {
	Initialize CampaignInitializer
}

// BeforeCreate keeps direct status/seed inserts (including test fixtures) valid
// before the service computes the real state hash.
func (c *Campaign) BeforeCreate(_ *gorm.DB) error {
	if strings.TrimSpace(c.StateHash) == "" {
		c.StateHash = strings.Repeat("0", 64)
	}
	return nil
}

type GameplayDefinitionProvider interface {
	DefinitionsForVersion(context.Context, uuid.UUID, string) (map[string]GameplayDefinition, error)
}

// CampaignRepository owns Campaign row locks, event sequence allocation and
// idempotent action commits. Callers cannot append campaign_events directly.
type CampaignRepository struct {
	db                 *gorm.DB
	definitionProvider GameplayDefinitionProvider
}

func NewCampaignRepository(db *gorm.DB, providers ...GameplayDefinitionProvider) *CampaignRepository {
	var provider GameplayDefinitionProvider
	if len(providers) > 0 {
		provider = providers[0]
	}
	return &CampaignRepository{db: db, definitionProvider: provider}
}

func (r *CampaignRepository) Create(ctx context.Context, row *Campaign, initialization ...CampaignInitialization) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(row).Error; err != nil {
			return err
		}
		if len(initialization) != 1 || initialization[0].Initialize == nil {
			return fmt.Errorf("Campaign 缺少已注册的初始化处理器")
		}
		if err := initialization[0].Initialize(tx, row); err != nil {
			return err
		}
		stateHash, err := CampaignStateHash(ctx, tx, *row)
		if err != nil {
			return err
		}
		row.StateHash = stateHash
		return tx.Model(&Campaign{}).Where("id = ?", row.ID).Update("state_hash", stateHash).Error
	})
}

func (r *CampaignRepository) DomainState(ctx context.Context, campaignID uuid.UUID) (*CampaignDomainState, error) {
	row, err := r.Get(ctx, campaignID)
	if err != nil {
		return nil, err
	}
	return loadCampaignDomainState(ctx, r.db, *row, r.definitionProvider)
}

func (r *CampaignRepository) Get(ctx context.Context, id uuid.UUID) (*Campaign, error) {
	var row Campaign
	err := r.db.WithContext(ctx).First(&row, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &row, err
}

func (r *CampaignRepository) List(ctx context.Context, playerID uuid.UUID, statuses []string) ([]Campaign, error) {
	query := r.db.WithContext(ctx).Where("player_id = ?", playerID)
	if len(statuses) > 0 {
		query = query.Where("status IN ?", statuses)
	}
	var rows []Campaign
	err := query.Order("updated_at DESC, id DESC").Limit(100).Find(&rows).Error
	return rows, err
}

func (r *CampaignRepository) ListEvents(
	ctx context.Context,
	campaignID uuid.UUID,
	visibilities []string,
	afterSequence int64,
	limit int,
) ([]CampaignEvent, error) {
	query := activeCampaignEvents(r.db.WithContext(ctx)).Where("campaign_id = ? AND sequence > ?", campaignID, afterSequence)
	if len(visibilities) > 0 {
		query = query.Where("visibility IN ?", visibilities)
	}
	if limit > 0 {
		query = query.Limit(limit)
	}
	var rows []CampaignEvent
	err := query.Order("sequence ASC").Find(&rows).Error
	return rows, err
}

func (r *CampaignRepository) RecentEvents(
	ctx context.Context,
	campaignID uuid.UUID,
	visibilities []string,
	limit int,
) ([]CampaignEvent, error) {
	query := activeCampaignEvents(r.db.WithContext(ctx)).Where("campaign_id = ?", campaignID)
	if len(visibilities) > 0 {
		query = query.Where("visibility IN ?", visibilities)
	}
	if limit > 0 {
		query = query.Limit(limit)
	}
	var rows []CampaignEvent
	if err := query.Order("sequence DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	for left, right := 0, len(rows)-1; left < right; left, right = left+1, right-1 {
		rows[left], rows[right] = rows[right], rows[left]
	}
	return rows, nil
}

func (r *CampaignRepository) ListConversations(ctx context.Context, campaignID uuid.UUID) ([]Conversation, error) {
	var rows []Conversation
	err := r.db.WithContext(ctx).Where("campaign_id = ?", campaignID).
		Order("created_at ASC, id ASC").Limit(100).Find(&rows).Error
	return rows, err
}

// CreateConversation atomically creates the Campaign's only conversation and
// its initial state while holding the Campaign lock.
func (r *CampaignRepository) CreateConversation(
	ctx context.Context,
	playerID uuid.UUID,
	conversation *Conversation,
	initialState JSONMap,
	schemaVersion int,
) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var campaign Campaign
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&campaign, "id = ? AND player_id = ?", conversationCampaignID(conversation), playerID).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if campaign.Status != CampaignStatusActive {
			return ErrCampaignNotActive
		}
		if conversation.PlayerID != campaign.PlayerID.String() ||
			conversation.ChallengeVersionID != campaign.ChallengeVersionID ||
			conversation.ChallengeHash != campaign.ChallengeHash ||
			!sameUUIDPointer(conversation.CharacterAssetID, campaign.CharacterAssetID) ||
			!sameUUIDPointer(conversation.PersonaAssetID, campaign.PersonaAssetID) ||
			!sameUUIDPointer(conversation.ScenarioAssetID, campaign.ScenarioAssetID) {
			return ErrNotFound
		}

		var conversationCount int64
		if err := tx.Model(&Conversation{}).
			Where("campaign_id = ?", campaign.ID).
			Count(&conversationCount).Error; err != nil {
			return err
		}
		if conversationCount > 0 {
			return ErrCampaignHasConversation
		}
		conversation.Title = campaign.Title
		if err := tx.Create(conversation).Error; err != nil {
			return err
		}
		return tx.Create(&ConversationState{
			ConversationID: conversation.ID,
			State:          initialState,
			SchemaVersion:  schemaVersion,
			UpdatedAt:      conversation.CreatedAt,
		}).Error
	})
}

func conversationCampaignID(conversation *Conversation) uuid.UUID {
	if conversation == nil || conversation.CampaignID == nil {
		return uuid.Nil
	}
	return *conversation.CampaignID
}

func sameUUIDPointer(left, right *uuid.UUID) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

// CommitAction serializes one domain action on the Campaign row. Idempotency
// is checked before active/revision validation so a stale retry returns the
// original result instead of reporting a false conflict.
func (r *CampaignRepository) CommitAction(
	ctx context.Context,
	in CommitCampaignActionInput,
	execute func(*CampaignActionStore, Campaign) (CampaignActionOutcome, error),
) (*CampaignActionResult, error) {
	if err := validateCampaignCommitInput(in, execute); err != nil {
		return nil, err
	}
	var result CampaignActionResult
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var campaign Campaign
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&campaign, "id = ? AND player_id = ?", in.CampaignID, in.PlayerID).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}

		existing, err := loadCampaignCommit(ctx, tx, campaign.ID, in.IdempotencyKey)
		if err == nil {
			if existing.TurnID != nil {
				var active int64
				if err := tx.Model(&ConversationTurn{}).Where("id = ? AND invalidated_at IS NULL", *existing.TurnID).Count(&active).Error; err != nil {
					return err
				}
				if active != 1 {
					return ErrCampaignIdempotencyConflict
				}
			}
			if existing.ActionKey != in.ActionKey {
				return ErrCampaignIdempotencyConflict
			}
			events, loadErr := loadCommitEvents(ctx, tx, existing)
			if loadErr != nil {
				return loadErr
			}
			result = CampaignActionResult{
				Campaign: campaign, Events: events, Result: existing.Result, Replayed: true,
			}
			result.Narrative, err = campaignActionResultFromCommit(existing)
			if err != nil {
				return err
			}
			result.RollMessage, err = loadCampaignRollDisplay(ctx, tx, campaign.ID, in.IdempotencyKey)
			if err != nil {
				return err
			}
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		if campaign.Status != CampaignStatusActive {
			return ErrCampaignNotActive
		}
		if campaign.Revision != in.ExpectedRevision {
			return fmt.Errorf("%w: 当前 revision 为 %d", ErrCampaignRevisionConflict, campaign.Revision)
		}
		if in.SourceMessageID != nil {
			var sourceCount int64
			if err := tx.Model(&ChatMessage{}).
				Joins("JOIN conversations ON conversations.id = chat_messages.conversation_id").
				Where("chat_messages.id = ? AND conversations.campaign_id = ?", *in.SourceMessageID, campaign.ID).
				Count(&sourceCount).Error; err != nil {
				return err
			}
			if sourceCount != 1 {
				return ErrNotFound
			}
		}

		outcome, err := execute(newCampaignActionStore(ctx, tx, campaign, r.definitionProvider), campaign)
		if err != nil {
			return err
		}
		if err := validateCampaignOutcome(campaign, outcome); err != nil {
			return err
		}

		now := time.Now()
		metadata, hasMetadata := operation.FromContext(ctx)
		newGameTime := campaign.GameTime + outcome.GameTimeDelta
		firstSequence := campaign.EventSequence + 1
		events := make([]CampaignEvent, 0, len(outcome.Events))
		for index, draft := range outcome.Events {
			payload := draft.Payload
			if payload == nil {
				payload = JSONMap{}
			}
			event := CampaignEvent{
				ID: uuid.New(), CampaignID: campaign.ID,
				Sequence: firstSequence + int64(index), EventType: draft.EventType,
				Visibility: draft.Visibility, ActorType: in.ActorType, ActorID: in.ActorID,
				Payload: payload, SourceMessageID: in.SourceMessageID,
				IdempotencyKey: in.IdempotencyKey, GameTime: newGameTime, CreatedAt: now,
			}
			if hasMetadata {
				event.TurnID = &metadata.TurnID
				event.AttemptID = &metadata.AttemptID
			}
			if err := tx.Create(&event).Error; err != nil {
				return err
			}
			events = append(events, event)
		}

		campaign.GameTime = newGameTime
		campaign.Revision++
		campaign.EventSequence += int64(len(events))
		campaign.UpdatedAt = now
		if outcome.CurrentLocationKey != nil {
			campaign.CurrentLocationKey = *outcome.CurrentLocationKey
		}
		if outcome.Status != nil {
			campaign.Status = *outcome.Status
		}
		if outcome.OutcomeKey != nil {
			campaign.OutcomeKey = *outcome.OutcomeKey
			campaign.CompletedAtGameTime = &newGameTime
		}
		stateHash, err := CampaignStateHash(ctx, tx, campaign)
		if err != nil {
			return err
		}
		campaign.StateHash = stateHash
		update := tx.Model(&Campaign{}).
			Where("id = ? AND revision = ?", campaign.ID, in.ExpectedRevision).
			Updates(map[string]any{
				"status": campaign.Status, "outcome_key": campaign.OutcomeKey,
				"completed_at_game_time": campaign.CompletedAtGameTime, "current_location_key": campaign.CurrentLocationKey,
				"game_time": campaign.GameTime, "revision": campaign.Revision,
				"event_sequence": campaign.EventSequence, "state_hash": campaign.StateHash,
				"updated_at": campaign.UpdatedAt,
			})
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return ErrCampaignRevisionConflict
		}
		if campaign.Status != CampaignStatusActive {
			closed := tx.Model(&Conversation{}).Where("campaign_id = ? AND status = ?", campaign.ID, StatusActive).Updates(map[string]any{"status": StatusEnded, "ended_at": now, "updated_at": now})
			if closed.Error != nil {
				return closed.Error
			}
			if closed.RowsAffected != 1 {
				return ErrConversationNotActive
			}
		}
		publicResult := outcome.Result
		if publicResult == nil {
			publicResult = JSONMap{}
		}
		rollMessage, err := commitCampaignRoll(ctx, tx, campaign.ID, in.IdempotencyKey, outcome.Roll, now)
		if err != nil {
			return err
		}
		narrative, err := gameplay.BuildActionResult(campaignNarrativeFeedback(publicResult), gameplay.ActionResultMeta{
			ActionID: in.IdempotencyKey, ActionKey: in.ActionKey, ActorType: in.ActorType,
			Revision: campaign.Revision, StateHash: campaign.StateHash, GameTime: campaign.GameTime,
			Summary: in.Label,
		})
		if err != nil {
			return err
		}
		narrativeRaw, err := json.Marshal(narrative)
		if err != nil {
			return err
		}
		narrativeDocument := JSONMap{}
		if err := json.Unmarshal(narrativeRaw, &narrativeDocument); err != nil {
			return err
		}
		commit := CampaignActionCommit{
			CampaignID: campaign.ID, IdempotencyKey: in.IdempotencyKey, ActionKey: in.ActionKey,
			Revision: campaign.Revision, FirstSequence: firstSequence,
			LastSequence: campaign.EventSequence, Result: publicResult,
			Narrative: narrativeDocument, StateHash: campaign.StateHash,
			SchemaVersion: int16(narrative.SchemaVersion), CreatedAt: now,
		}
		if hasMetadata {
			commit.TurnID = &metadata.TurnID
			commit.AttemptID = &metadata.AttemptID
		}
		if err := tx.Create(&commit).Error; err != nil {
			return err
		}
		result = CampaignActionResult{Campaign: campaign, Events: events, Result: publicResult, Narrative: &narrative, RollMessage: rollMessage}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func campaignNarrativeFeedback(result JSONMap) map[string]any {
	if result == nil {
		return nil
	}
	raw, ok := result["narrative_feedback"]
	if !ok || raw == nil {
		return nil
	}
	switch value := raw.(type) {
	case map[string]any:
		return value
	case JSONMap:
		return map[string]any(value)
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var feedback map[string]any
	if err := json.Unmarshal(encoded, &feedback); err != nil {
		return nil
	}
	return feedback
}

func campaignActionResultFromCommit(commit *CampaignActionCommit) (*gameplay.ActionResult, error) {
	if commit == nil || len(commit.Narrative) == 0 {
		return nil, nil
	}
	encoded, err := json.Marshal(commit.Narrative)
	if err != nil {
		return nil, err
	}
	var result gameplay.ActionResult
	if err := json.Unmarshal(encoded, &result); err != nil {
		return nil, err
	}
	if strings.TrimSpace(result.ActionKey) == "" {
		return nil, nil
	}
	return &result, nil
}

func CampaignActionResultFromCommit(commit CampaignActionCommit) (*gameplay.ActionResult, error) {
	return campaignActionResultFromCommit(&commit)
}

func campaignRollIDs(campaignID uuid.UUID, idempotencyKey string) (uuid.UUID, uuid.UUID, uuid.UUID) {
	return uuid.NewSHA1(campaignID, []byte("roll-history:"+idempotencyKey)),
		uuid.NewSHA1(campaignID, []byte("roll-chat:"+idempotencyKey)),
		uuid.NewSHA1(campaignID, []byte("roll-display:"+idempotencyKey))
}

func commitCampaignRoll(ctx context.Context, tx *gorm.DB, campaignID uuid.UUID, idempotencyKey string, snapshot *CampaignRollSnapshot, now time.Time) (*DisplayMessage, error) {
	if snapshot == nil {
		return nil, nil
	}
	var conversation Conversation
	if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		First(&conversation, "campaign_id = ? AND deleted_at IS NULL", campaignID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("天命判定需要 Campaign 绑定对话: %w", ErrNotFound)
		}
		return nil, err
	}
	historyID, chatID, displayID := campaignRollIDs(campaignID, idempotencyKey)
	entry := &RollHistoryEntry{
		ID: historyID.String(), ConversationID: conversation.ID.String(), RollKey: snapshot.RollKey,
		TitleSnapshot: snapshot.Title, Category: snapshot.Category, Success: snapshot.Success, CreatedAt: now,
	}
	if metadata, ok := operation.FromContext(ctx); ok {
		entry.TurnID, entry.AttemptID = &metadata.TurnID, &metadata.AttemptID
	}
	if err := tx.WithContext(ctx).Create(entry).Error; err != nil {
		return nil, err
	}
	chat := &ChatMessage{
		ID: chatID, ConversationID: conversation.ID, Role: RoleSystem, Content: snapshot.Content,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := NewMessageRepo(tx).CreateChat(ctx, chat); err != nil {
		return nil, err
	}
	metadata := snapshot.Metadata
	if metadata == nil {
		metadata = JSONMap{}
	}
	display := &DisplayMessage{
		ID: displayID, ConversationID: conversation.ID, ChatMessageID: &chat.ID, Type: "roll_result",
		Markdown: snapshot.Markdown, Metadata: metadata, CreatedAt: now,
	}
	if err := NewMessageRepo(tx).CreateDisplay(ctx, display); err != nil {
		return nil, err
	}
	return display, nil
}

func loadCampaignRollDisplay(ctx context.Context, tx *gorm.DB, campaignID uuid.UUID, idempotencyKey string) (*DisplayMessage, error) {
	_, _, displayID := campaignRollIDs(campaignID, idempotencyKey)
	var display DisplayMessage
	err := tx.WithContext(ctx).First(&display, "id = ? AND invalidated_at IS NULL", displayID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &display, err
}

func loadCampaignCommit(ctx context.Context, tx *gorm.DB, campaignID uuid.UUID, key string) (*CampaignActionCommit, error) {
	var commit CampaignActionCommit
	err := tx.WithContext(ctx).First(&commit, "campaign_id = ? AND idempotency_key = ?", campaignID, key).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	return &commit, err
}

func loadCommitEvents(ctx context.Context, tx *gorm.DB, commit *CampaignActionCommit) ([]CampaignEvent, error) {
	var events []CampaignEvent
	err := tx.WithContext(ctx).
		Where("campaign_id = ? AND sequence BETWEEN ? AND ?", commit.CampaignID, commit.FirstSequence, commit.LastSequence).
		Order("sequence ASC").Find(&events).Error
	return events, err
}

func validateCampaignOutcome(campaign Campaign, outcome CampaignActionOutcome) error {
	if len(outcome.Events) == 0 || len(outcome.Events) > 64 {
		return fmt.Errorf("领域动作必须产生 1 到 64 条事件")
	}
	if outcome.GameTimeDelta < 0 || campaign.GameTime > math.MaxInt64-outcome.GameTimeDelta {
		return fmt.Errorf("游戏时间增量无效")
	}
	if outcome.CurrentLocationKey != nil && strings.TrimSpace(*outcome.CurrentLocationKey) == "" {
		return fmt.Errorf("当前位置 key 不能为空")
	}
	if outcome.Status != nil {
		switch *outcome.Status {
		case CampaignStatusActive, CampaignStatusCompleted, CampaignStatusDead, CampaignStatusAbandoned:
		default:
			return fmt.Errorf("Campaign 状态无效")
		}
	}
	if outcome.OutcomeKey != nil {
		if outcome.Status == nil || *outcome.Status == CampaignStatusActive || !validCampaignKey(*outcome.OutcomeKey, 128) {
			return fmt.Errorf("Campaign 结局 key 无效")
		}
	} else if outcome.Status != nil && *outcome.Status != CampaignStatusActive {
		return fmt.Errorf("%w: status=%s", ErrCampaignOutcomeMissing, *outcome.Status)
	}
	totalPayloadBytes := 0
	for _, event := range outcome.Events {
		if !validCampaignKey(event.EventType, 128) {
			return fmt.Errorf("领域事件类型必须是稳定 key")
		}
		switch event.Visibility {
		case CampaignVisibilityPlayer, CampaignVisibilityModel, CampaignVisibilityInternal:
		default:
			return fmt.Errorf("领域事件可见性无效")
		}
		payload, err := json.Marshal(event.Payload)
		if err != nil || len(payload) > 8*1024 {
			return fmt.Errorf("领域事件 payload 无效或超过 8 KiB")
		}
		totalPayloadBytes += len(payload)
	}
	if totalPayloadBytes > 64*1024 {
		return fmt.Errorf("单次领域动作事件 payload 合计超过 64 KiB")
	}
	publicResult, err := json.Marshal(outcome.Result)
	if err != nil || len(publicResult) > 16*1024 {
		return fmt.Errorf("领域动作公开结果无效或超过 16 KiB")
	}
	if roll := outcome.Roll; roll != nil {
		if !validCampaignKey(roll.RollKey, 128) || strings.TrimSpace(roll.Title) == "" || len([]rune(roll.Title)) > 128 ||
			strings.TrimSpace(roll.Category) == "" || len(roll.Category) > 64 || strings.TrimSpace(roll.Content) == "" ||
			len([]byte(roll.Content)) > 8*1024 || strings.TrimSpace(roll.Markdown) == "" || len([]byte(roll.Markdown)) > 8*1024 {
			return fmt.Errorf("Campaign 天命判定快照无效")
		}
		metadata, metadataErr := json.Marshal(roll.Metadata)
		if metadataErr != nil || len(metadata) > 8*1024 {
			return fmt.Errorf("Campaign 天命判定 metadata 无效或超过 8 KiB")
		}
	}
	return nil
}

func validateCampaignCommitInput(
	in CommitCampaignActionInput,
	execute func(*CampaignActionStore, Campaign) (CampaignActionOutcome, error),
) error {
	if in.CampaignID == uuid.Nil || in.PlayerID == uuid.Nil || in.ExpectedRevision < 0 || execute == nil {
		return fmt.Errorf("Campaign 动作提交参数无效")
	}
	if strings.TrimSpace(in.IdempotencyKey) == "" || strings.TrimSpace(in.IdempotencyKey) != in.IdempotencyKey || len(in.IdempotencyKey) > 160 ||
		!validCampaignKey(in.ActionKey, 128) {
		return fmt.Errorf("Campaign 动作 key 或幂等键无效")
	}
	switch in.ActorType {
	case "player", "model", "system":
	default:
		return fmt.Errorf("Campaign 动作 actor_type 无效")
	}
	if in.ActorID != nil && len(*in.ActorID) > 160 {
		return fmt.Errorf("Campaign 动作 actor_id 过长")
	}
	return nil
}

func validCampaignKey(value string, maxLength int) bool {
	if value == "" || strings.TrimSpace(value) != value || len(value) > maxLength {
		return false
	}
	for index, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') ||
			index > 0 && (char == '.' || char == '_' || char == '-') {
			continue
		}
		return false
	}
	return true
}
