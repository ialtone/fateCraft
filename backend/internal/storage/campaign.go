package storage

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/craftingrules"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/effectrules"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/factionrules"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/marketrules"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/worldrules"
	"github.com/ialtone/fatecraft/internal/operation"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrCampaignNotActive           = errors.New("长期存档未处于 active 状态")
	ErrCampaignRevisionConflict    = errors.New("长期存档 revision 冲突")
	ErrCampaignIdempotencyConflict = errors.New("领域动作幂等键已被其他动作使用")
	ErrCampaignHasConversation     = errors.New("长期存档已经绑定对话")
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
	ActorType        string
	ActorID          *string
	SourceMessageID  *uuid.UUID
}

type CampaignActionResult struct {
	Campaign    Campaign        `json:"campaign"`
	Events      []CampaignEvent `json:"events"`
	Result      JSONMap         `json:"result"`
	RollMessage *DisplayMessage `json:"roll_message,omitempty"`
	Replayed    bool            `json:"replayed"`
}

type CampaignInitialization struct {
	RequireRealm bool
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
		return initializeCampaignDomain(tx, row, initialization...)
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
	query := r.db.WithContext(ctx).Where("campaign_id = ? AND sequence > ?", campaignID, afterSequence)
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
	query := r.db.WithContext(ctx).Where("campaign_id = ?", campaignID)
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
		update := tx.Model(&Campaign{}).
			Where("id = ? AND revision = ?", campaign.ID, in.ExpectedRevision).
			Updates(map[string]any{
				"status": campaign.Status, "outcome_key": campaign.OutcomeKey,
				"completed_at_game_time": campaign.CompletedAtGameTime, "current_location_key": campaign.CurrentLocationKey,
				"game_time": campaign.GameTime, "revision": campaign.Revision,
				"event_sequence": campaign.EventSequence, "updated_at": campaign.UpdatedAt,
			})
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return ErrCampaignRevisionConflict
		}
		publicResult := outcome.Result
		if publicResult == nil {
			publicResult = JSONMap{}
		}
		rollMessage, err := commitCampaignRoll(ctx, tx, campaign.ID, in.IdempotencyKey, outcome.Roll, now)
		if err != nil {
			return err
		}
		commit := CampaignActionCommit{
			CampaignID: campaign.ID, IdempotencyKey: in.IdempotencyKey, ActionKey: in.ActionKey,
			Revision: campaign.Revision, FirstSequence: firstSequence,
			LastSequence: campaign.EventSequence, Result: publicResult, CreatedAt: now,
		}
		if hasMetadata {
			commit.TurnID = &metadata.TurnID
			commit.AttemptID = &metadata.AttemptID
		}
		if err := tx.Create(&commit).Error; err != nil {
			return err
		}
		result = CampaignActionResult{Campaign: campaign, Events: events, Result: publicResult, RollMessage: rollMessage}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
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
		if outcome.Status == nil || *outcome.Status != CampaignStatusCompleted || !validCampaignKey(*outcome.OutcomeKey, 128) {
			return fmt.Errorf("Campaign 结局 key 无效")
		}
	} else if outcome.Status != nil && *outcome.Status == CampaignStatusCompleted {
		return fmt.Errorf("Campaign 完成时必须提供结局 key")
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

func initializeCampaignDomain(
	tx *gorm.DB,
	campaign *Campaign,
	initializationOption ...CampaignInitialization,
) error {
	initialization := CampaignInitialization{RequireRealm: true}
	if len(initializationOption) > 0 {
		initialization = initializationOption[0]
	}
	var definitions []GameplayDefinition
	if err := tx.Where("challenge_version_id = ?", campaign.ChallengeVersionID).
		Order("kind ASC, sort_order ASC, key ASC").Find(&definitions).Error; err != nil {
		return err
	}
	var initialRealm *GameplayDefinition
	var initialRealmRank int64
	if initialization.RequireRealm {
		for _, definition := range definitions {
			if definition.Kind == DefinitionRealm {
				var err error
				initialRealm, initialRealmRank, err = selectInitialCampaignRealm(definitions)
				if err != nil {
					return err
				}
				break
			}
		}
	}
	locationFound := false
	locationDefinitions := map[string]GameplayDefinition{}
	for index := range definitions {
		definition := &definitions[index]
		if definition.Kind == DefinitionLocation {
			locationDefinitions[definition.Key] = *definition
			if definition.Key == campaign.CurrentLocationKey {
				locationFound = true
			}
		}
	}
	if !locationFound {
		return fmt.Errorf("Campaign 缺少初始地点定义")
	}
	startingTechniques, err := selectStartingTechniques(definitions, campaign.RandomSeed)
	if err != nil {
		return err
	}
	if initialization.RequireRealm && initialRealm == nil {
		return fmt.Errorf("Campaign 缺少境界定义")
	}
	if initialRealm == nil && len(startingTechniques) > 0 {
		return fmt.Errorf("无角色的通用 Campaign 不能声明开局功法")
	}
	name := "无名修士"
	if campaign.PersonaAssetID != nil {
		var asset ChallengeAsset
		if err := tx.First(&asset, "id = ? AND challenge_version_id = ?", *campaign.PersonaAssetID, campaign.ChallengeVersionID).Error; err == nil {
			var identity struct {
				Name string `json:"name"`
			}
			if json.Unmarshal(asset.Content, &identity) == nil && strings.TrimSpace(identity.Name) != "" {
				name = identity.Name
			}
		}
	}
	now := time.Now()
	var character *CampaignCharacter
	if initialRealm != nil {
		stages, _ := initialRealm.Content["stages"].([]any)
		stageKey := "initial"
		if len(stages) > 0 {
			stageKey, _ = stages[0].(string)
		}
		required, _ := campaignDefinitionInt(initialRealm.Content["cultivation_required"])
		lifespan, _ := campaignDefinitionInt(initialRealm.Content["lifespan_days"])
		maxHealth := campaignDefinitionIntDefault(initialRealm.Content["max_health"], 100)
		maxMana := campaignDefinitionIntDefault(initialRealm.Content["max_mana"], 30)
		maxSense := campaignDefinitionIntDefault(initialRealm.Content["max_spiritual_sense"], 20)
		character = &CampaignCharacter{
			ID: uuid.New(), CampaignID: campaign.ID, Name: name,
			RealmKey: initialRealm.Key, StageKey: stageKey, RealmRank: int(initialRealmRank),
			CultivationRequired: required, FoundationQuality: 50,
			AgeDays:      campaignDefinitionIntDefault(initialRealm.Content["starting_age_days"], 16*365),
			LifespanDays: lifespan,
			Health:       maxHealth, BaseMaxHealth: maxHealth, MaxHealth: maxHealth,
			Mana: maxMana, BaseMaxMana: maxMana, MaxMana: maxMana,
			SpiritualSense: maxSense, BaseMaxSpiritualSense: maxSense, MaxSpiritualSense: maxSense,
			Injuries: JSONMap{}, LongTermEffects: JSONMap{}, CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(character).Error; err != nil {
			return err
		}
	}
	locationKey := campaign.CurrentLocationKey
	for depth := 0; depth < 4 && locationKey != ""; depth++ {
		knowledge := JSONMap{}
		if depth > 0 {
			knowledge["source"] = "initial_hierarchy"
		}
		if err := tx.Create(&CampaignLocationDiscovery{
			CampaignID: campaign.ID, LocationKey: locationKey,
			DiscoveredAtGameTime: campaign.GameTime, Visited: depth == 0, Knowledge: knowledge, UpdatedAt: now,
		}).Error; err != nil {
			return err
		}
		definition, ok := locationDefinitions[locationKey]
		if !ok {
			break
		}
		locationKey, _ = definition.Content["parent_key"].(string)
	}
	startingEffects := map[string]int64{}
	for _, definition := range definitions {
		switch definition.Kind {
		case DefinitionTechnique:
			if startingTechniques[definition.Key] {
				category, _ := definition.Content["category"].(string)
				source := JSONMap{"name": definition.Name, "key": definition.Key}
				if pool, _ := definition.Content["starting_pool"].(string); pool != "" {
					source["starting_pool"] = pool
				}
				row := CampaignTechnique{
					ID: uuid.New(), CampaignID: campaign.ID, TechniqueKey: definition.Key,
					Category: category, LearnedLayer: 1, Status: "active",
					SourceSnapshot: source, CreatedAt: now, UpdatedAt: now,
				}
				if slot, _ := definition.Content["starting_slot"].(string); slot != "" {
					row.EquippedSlot = &slot
					if effects, ok := effectrules.TechniqueEffects(definition.Content, row.LearnedLayer); ok {
						effectrules.Add(startingEffects, effects)
					}
				}
				if err := tx.Create(&row).Error; err != nil {
					return err
				}
			}
		case DefinitionItem:
			if quantity, ok := campaignDefinitionInt(definition.Content["starting_quantity"]); ok && quantity > 0 {
				if err := tx.Create(&CampaignItemStack{
					CampaignID: campaign.ID, ItemKey: definition.Key, QualityKey: "common",
					Quantity: quantity, Metadata: JSONMap{"name": definition.Name}, UpdatedAt: now,
				}).Error; err != nil {
					return err
				}
			}
		case DefinitionEquipment:
			if starting, _ := definition.Content["starting_owned"].(bool); starting {
				durability := int(campaignDefinitionIntDefault(definition.Content["durability_max"], 100))
				if err := tx.Create(&CampaignItemInstance{
					ID: uuid.New(), CampaignID: campaign.ID, DefinitionKey: definition.Key,
					QualityKey: "common", State: "stored", Durability: durability, MaxDurability: durability,
					Properties: JSONMap{"name": definition.Name}, Provenance: JSONMap{"source": "campaign.start"},
					CreatedAt: now, UpdatedAt: now,
				}).Error; err != nil {
					return err
				}
			}
		case DefinitionRecipe:
			recipe, ok := craftingrules.Parse(map[string]any(definition.Content))
			if !ok || !recipe.StartingKnown {
				continue
			}
			if err := tx.Create(&CampaignKnownRecipe{
				CampaignID: campaign.ID, RecipeKey: definition.Key,
				LearnedAtGameTime: campaign.GameTime,
				SourceSnapshot:    JSONMap{"name": definition.Name, "key": definition.Key, "source": "campaign.start"},
				CreatedAt:         now,
			}).Error; err != nil {
				return err
			}
		case DefinitionMarket:
			market, ok := marketrules.Parse(map[string]any(definition.Content))
			if !ok {
				return fmt.Errorf("市场定义无效: %s", definition.Key)
			}
			if err := tx.Create(&CampaignMarketState{
				CampaignID: campaign.ID, MarketKey: definition.Key, LocationKey: market.LocationKey,
				LastRestockGameTime: campaign.GameTime, PriceModifierBPS: 10000, UpdatedAt: now,
			}).Error; err != nil {
				return err
			}
			for _, entry := range market.Entries {
				if err := tx.Create(&CampaignMarketStock{
					CampaignID: campaign.ID, MarketKey: definition.Key,
					ItemKind: entry.Kind, ItemKey: entry.Key, QualityKey: entry.QualityKey,
					Quantity: entry.InitialStock, UpdatedAt: now,
				}).Error; err != nil {
					return err
				}
			}
		case DefinitionFaction:
			reputation := int(campaignDefinitionIntDefault(definition.Content["initial_reputation"], 0))
			if err := tx.Create(&CampaignFactionStanding{
				CampaignID: campaign.ID, FactionKey: definition.Key, Reputation: reputation,
				PositionKey: factionrules.Position(definition.Content, 0),
				Permissions: JSONMap(factionrules.DerivePermissions(definition.Content, reputation)), UpdatedAt: now,
			}).Error; err != nil {
				return err
			}
		case DefinitionLocation:
			if nodeType, _ := definition.Content["node_type"].(string); nodeType == "region" {
				initial, ok := worldrules.ParseRegionInitialState(definition.Content["initial_state"])
				if !ok {
					return fmt.Errorf("区域初始状态无效: %s", definition.Key)
				}
				factionPower := JSONMap{}
				for key, value := range initial.FactionPower {
					factionPower[key] = value
				}
				if err := tx.Create(&CampaignRegionState{
					CampaignID: campaign.ID, RegionKey: definition.Key,
					ResourceLevel: initial.ResourceLevel, DangerLevel: initial.DangerLevel,
					Stability: initial.Stability, ConflictStage: initial.ConflictStage,
					FactionPower: factionPower, UpdatedAt: now,
				}).Error; err != nil {
					return err
				}
			}
		case DefinitionNPCArchetype:
			if initial, _ := definition.Content["initial"].(bool); !initial {
				continue
			}
			instanceKey, _ := definition.Content["instance_key"].(string)
			locationKey, _ := definition.Content["location_key"].(string)
			realmKey, _ := definition.Content["realm_key"].(string)
			stage, _ := definition.Content["stage_key"].(string)
			if stage == "" {
				stage = "initial"
			}
			faction, _ := definition.Content["faction_key"].(string)
			public, _ := definition.Content["public_profile"].(map[string]any)
			private, _ := definition.Content["private_state"].(map[string]any)
			goals, _ := definition.Content["goals"].(map[string]any)
			npc := CampaignNPC{
				ID: uuid.New(), CampaignID: campaign.ID, ArchetypeKey: definition.Key,
				InstanceKey: instanceKey, StableName: definition.Name, FactionKey: faction,
				RealmKey: realmKey, StageKey: stage, CurrentLocationKey: locationKey, Status: "active",
				PublicProfile: JSONMap(public), PrivateState: JSONMap(private), Goals: JSONMap(goals),
				CreatedAt: now, UpdatedAt: now,
			}
			if npc.PublicProfile == nil {
				npc.PublicProfile = JSONMap{}
			}
			if npc.PrivateState == nil {
				npc.PrivateState = JSONMap{}
			}
			if npc.Goals == nil {
				npc.Goals = JSONMap{}
			}
			if err := tx.Create(&npc).Error; err != nil {
				return err
			}
		case DefinitionWorldEvent:
			event, ok := worldrules.ParseWorldEvent(map[string]any(definition.Content))
			if !ok {
				return fmt.Errorf("世界事件定义无效: %s", definition.Key)
			}
			if err := tx.Create(&CampaignScheduledEvent{
				ID: uuid.New(), CampaignID: campaign.ID, EventKind: DefinitionWorldEvent,
				TemplateKey: definition.Key, DueGameTime: campaign.GameTime + event.InitialDelayMinutes,
				Priority: event.Priority, Status: "pending", Occurrence: 0,
				Scope: JSONMap{"region_key": event.RegionKey}, Payload: JSONMap{}, CreatedAt: now, UpdatedAt: now,
			}).Error; err != nil {
				return err
			}
		case DefinitionQuest:
			quest, ok := worldrules.ParseQuest(map[string]any(definition.Content))
			if !ok {
				return fmt.Errorf("任务定义无效: %s", definition.Key)
			}
			if !quest.Initial {
				continue
			}
			row := CampaignQuest{
				CampaignID: campaign.ID, QuestKey: definition.Key, QuestType: quest.QuestType,
				Status: quest.StartingStatus, OfferedAtGameTime: campaign.GameTime,
				Progress: JSONMap{}, SourceSnapshot: JSONMap{"key": definition.Key, "name": definition.Name}, UpdatedAt: now,
			}
			if quest.StartingStatus == "active" {
				accepted := campaign.GameTime
				row.AcceptedAtGameTime = &accepted
				if quest.DeadlineMinutes > 0 {
					due := campaign.GameTime + quest.DeadlineMinutes
					row.DueGameTime = &due
				}
			}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
			if row.DueGameTime != nil {
				if err := tx.Create(&CampaignScheduledEvent{
					ID: uuid.New(), CampaignID: campaign.ID, EventKind: "quest_deadline",
					TemplateKey: definition.Key, DueGameTime: *row.DueGameTime,
					Priority: 1000, Status: "pending", Occurrence: 0,
					Scope: JSONMap{}, Payload: JSONMap{"quest_key": definition.Key}, CreatedAt: now, UpdatedAt: now,
				}).Error; err != nil {
					return err
				}
			}
		}
	}
	if character != nil {
		character.MaxHealth = positiveStat(character.BaseMaxHealth + startingEffects["max_health"])
		character.MaxMana = positiveStat(character.BaseMaxMana + startingEffects["max_mana"])
		character.MaxSpiritualSense = positiveStat(character.BaseMaxSpiritualSense + startingEffects["max_spiritual_sense"])
		character.Health = character.MaxHealth
		character.Mana = character.MaxMana
		character.SpiritualSense = character.MaxSpiritualSense
		character.UpdatedAt = now
		if err := tx.Save(character).Error; err != nil {
			return err
		}
	}
	return nil
}

type startingTechniqueCandidate struct {
	key    string
	weight int64
}

// selectStartingTechniques keeps explicit starters and deterministically picks
// one candidate from every content-defined pool using the campaign seed.
func selectStartingTechniques(definitions []GameplayDefinition, seed int64) (map[string]bool, error) {
	selected := map[string]bool{}
	pools := map[string][]startingTechniqueCandidate{}
	for _, definition := range definitions {
		if definition.Kind != DefinitionTechnique {
			continue
		}
		starting, _ := definition.Content["starting_known"].(bool)
		pool, _ := definition.Content["starting_pool"].(string)
		if starting && pool != "" {
			return nil, fmt.Errorf("功法 %s 不能同时声明 starting_known 与 starting_pool", definition.Key)
		}
		if starting {
			selected[definition.Key] = true
		}
		if pool == "" {
			continue
		}
		if !validCampaignKey(pool, 128) {
			return nil, fmt.Errorf("功法 %s 的 starting_pool 无效", definition.Key)
		}
		weight := campaignDefinitionIntDefault(definition.Content["starting_weight"], 1)
		if weight < 1 || weight > 100000 {
			return nil, fmt.Errorf("功法 %s 的 starting_weight 无效", definition.Key)
		}
		pools[pool] = append(pools[pool], startingTechniqueCandidate{key: definition.Key, weight: weight})
	}

	poolKeys := make([]string, 0, len(pools))
	for pool := range pools {
		poolKeys = append(poolKeys, pool)
	}
	sort.Strings(poolKeys)
	for _, pool := range poolKeys {
		candidates := pools[pool]
		sort.Slice(candidates, func(i, j int) bool { return candidates[i].key < candidates[j].key })
		var total int64
		for _, candidate := range candidates {
			total += candidate.weight
		}
		roll := startingPoolRoll(seed, pool, total)
		for _, candidate := range candidates {
			if roll < candidate.weight {
				selected[candidate.key] = true
				break
			}
			roll -= candidate.weight
		}
	}
	return selected, nil
}

func startingPoolRoll(seed int64, pool string, total int64) int64 {
	var seedBytes [8]byte
	binary.BigEndian.PutUint64(seedBytes[:], uint64(seed))
	hash := sha256.New()
	_, _ = hash.Write(seedBytes[:])
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(pool))
	sum := hash.Sum(nil)
	return int64(binary.BigEndian.Uint64(sum[:8]) % uint64(total))
}

func positiveStat(value int64) int64 {
	if value < 1 {
		return 1
	}
	return value
}

func campaignDefinitionInt(value any) (int64, bool) {
	switch number := value.(type) {
	case int:
		return int64(number), true
	case int64:
		return number, true
	case float64:
		return int64(number), number == float64(int64(number))
	default:
		return 0, false
	}
}

func campaignDefinitionIntDefault(value any, fallback int64) int64 {
	if number, ok := campaignDefinitionInt(value); ok {
		return number
	}
	return fallback
}

func selectInitialCampaignRealm(definitions []GameplayDefinition) (*GameplayDefinition, int64, error) {
	var initial *GameplayDefinition
	var initialRank int64
	seenRanks := map[int64]string{}
	for index := range definitions {
		definition := &definitions[index]
		if definition.Kind != DefinitionRealm {
			continue
		}
		rank, ok := campaignDefinitionInt(definition.Content["rank"])
		if !ok || rank < 0 || rank > 1000 {
			return nil, 0, fmt.Errorf("Campaign 境界定义 %s 的 rank 无效", definition.Key)
		}
		if previous, exists := seenRanks[rank]; exists {
			return nil, 0, fmt.Errorf("Campaign 境界定义 %s 与 %s 使用了相同 rank %d", definition.Key, previous, rank)
		}
		seenRanks[rank] = definition.Key
		if initial == nil || rank < initialRank {
			initial, initialRank = definition, rank
		}
	}
	if initial == nil {
		return nil, 0, fmt.Errorf("Campaign 缺少境界定义")
	}
	return initial, initialRank, nil
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
