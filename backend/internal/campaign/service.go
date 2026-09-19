// Package campaign implements long-running save lifecycle and trusted domain
// action dispatch. Concrete cultivation systems register handlers here instead
// of allowing models or HTTP clients to append domain events directly.
package campaign

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/challenge"
	"github.com/ialtone/fatecraft/internal/gameplay"
	"github.com/ialtone/fatecraft/internal/identity"
	"github.com/ialtone/fatecraft/internal/roll"
	"github.com/ialtone/fatecraft/internal/storage"
)

var (
	ErrCampaignModeRequired = errors.New("该挑战不是长期存档玩法")
	ErrInvalidInput         = errors.New("长期存档请求格式无效")
	ErrInvalidAction        = errors.New("领域动作格式无效")
	ErrUnknownAction        = errors.New("领域动作处理器未安装")
	ErrActionRejected       = errors.New("领域动作前置条件未满足")
)

// ActionCommitter 是动作提交所需的最小仓储面。Get / ListConversations 与
// CommitAction 必须来自同一连接（事务）：Agent 工具事务已持有多张表的行锁，
// 事务内若再用全局连接池读取，会在高并发下把连接池耗尽并整体死锁。
type ActionCommitter interface {
	Get(context.Context, uuid.UUID) (*storage.Campaign, error)
	ListConversations(context.Context, uuid.UUID) ([]storage.Conversation, error)
	CommitAction(
		context.Context,
		storage.CommitCampaignActionInput,
		func(*storage.CampaignActionStore, storage.Campaign) (storage.CampaignActionOutcome, error),
	) (*storage.CampaignActionResult, error)
}

// TrustedActionContext 是编排层在回合内已经固定的可信输入。提供后动作执行
// 不再回查玩法版本、平台设置或用全局连接读取判定历史。
type TrustedActionContext struct {
	Contract *gameplay.RuntimeContract
	Platform *storage.SystemSettings
	// Roll 是绑定到当前事务连接的判定服务；nil 时退回服务默认实例。
	Roll *roll.Service
}

type repository interface {
	ActionCommitter
	Create(context.Context, *storage.Campaign, ...storage.CampaignInitialization) error
	List(context.Context, uuid.UUID, []string) ([]storage.Campaign, error)
	ListEvents(context.Context, uuid.UUID, []string, int64, int) ([]storage.CampaignEvent, error)
	RecentEvents(context.Context, uuid.UUID, []string, int) ([]storage.CampaignEvent, error)
	DomainState(context.Context, uuid.UUID) (*storage.CampaignDomainState, error)
	CreateConversation(context.Context, uuid.UUID, *storage.Conversation, storage.JSONMap, int) error
}

type challengeProvider interface {
	ResolveSelection(context.Context, string, string, ...uuid.UUID) (*challenge.Selection, error)
	RuntimeForVersion(context.Context, uuid.UUID, string) (*gameplay.RuntimeContract, error)
}

type openingSeeder interface {
	SeedOpening(context.Context, *storage.Conversation, time.Time)
}

type ActionHandler interface {
	Execute(context.Context, ActionExecution, map[string]any) (storage.CampaignActionOutcome, error)
}

type ActionStore interface {
	Definition(uuid.UUID, string, string) (*storage.GameplayDefinition, error)
	Character(uuid.UUID) (*storage.CampaignCharacter, error)
	SaveCharacter(*storage.CampaignCharacter) error
	Technique(uuid.UUID, string) (*storage.CampaignTechnique, error)
	CreateTechnique(*storage.CampaignTechnique) error
	SaveTechnique(*storage.CampaignTechnique) error
	Techniques(uuid.UUID) ([]storage.CampaignTechnique, error)
	ClearTechniqueSlot(uuid.UUID, string, uuid.UUID) error
	ItemStack(uuid.UUID, string, string) (*storage.CampaignItemStack, error)
	SaveItemStack(*storage.CampaignItemStack) error
	ItemInstance(uuid.UUID, uuid.UUID) (*storage.CampaignItemInstance, error)
	CreateItemInstance(*storage.CampaignItemInstance) error
	SaveItemInstance(*storage.CampaignItemInstance) error
	ItemInstances(uuid.UUID) ([]storage.CampaignItemInstance, error)
	UnequipItemSlot(uuid.UUID, string, uuid.UUID) error
	KnownRecipe(uuid.UUID, string) (*storage.CampaignKnownRecipe, error)
	CreateKnownRecipe(*storage.CampaignKnownRecipe) error
	MarketState(uuid.UUID, string) (*storage.CampaignMarketState, error)
	SaveMarketState(*storage.CampaignMarketState) error
	MarketStock(uuid.UUID, string, string, string, string) (*storage.CampaignMarketStock, error)
	SaveMarketStock(*storage.CampaignMarketStock) error
	DiscoverLocation(*storage.CampaignLocationDiscovery) error
	IsLocationDiscovered(uuid.UUID, string) (bool, error)
	NPC(uuid.UUID, string) (*storage.CampaignNPC, error)
	CountNPCsByArchetype(uuid.UUID, string) (int64, error)
	CreateNPC(*storage.CampaignNPC) error
	SaveNPC(*storage.CampaignNPC) error
	Relationship(uuid.UUID, uuid.UUID) (*storage.CampaignRelationship, error)
	SaveRelationship(*storage.CampaignRelationship) error
	FactionStanding(uuid.UUID, string) (*storage.CampaignFactionStanding, error)
	SaveFactionStanding(*storage.CampaignFactionStanding) error
	DueScheduledEvents(uuid.UUID, int64, int) ([]storage.CampaignScheduledEvent, error)
	CreateScheduledEvent(*storage.CampaignScheduledEvent) error
	SaveScheduledEvent(*storage.CampaignScheduledEvent) error
	CancelScheduledEvents(uuid.UUID, string, string) error
	RegionState(uuid.UUID, string) (*storage.CampaignRegionState, error)
	SaveRegionState(*storage.CampaignRegionState) error
	Quest(uuid.UUID, string) (*storage.CampaignQuest, error)
	SaveQuest(*storage.CampaignQuest) error
	Encounter(uuid.UUID, uuid.UUID) (*storage.CampaignEncounter, error)
	ActiveEncounter(uuid.UUID) (*storage.CampaignEncounter, error)
	CreateEncounter(*storage.CampaignEncounter) error
	SaveEncounter(*storage.CampaignEncounter) error
	EncounterWinCount(uuid.UUID, string) (int64, error)
	GrantCampaignOutcomeRewards(context.Context, storage.Campaign, string) ([]storage.CampaignOutcomeReward, error)
}

type ActionExecution struct {
	Campaign            storage.Campaign
	Store               ActionStore
	ActorType           string
	ActionID            string
	ActionKey           string
	SourceMessageID     *uuid.UUID
	MaxDueEventsPerTurn int
	Roll                CampaignRollFunc
}

type ActionHandlerFunc func(context.Context, ActionExecution, map[string]any) (storage.CampaignActionOutcome, error)

func (fn ActionHandlerFunc) Execute(ctx context.Context, execution ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	return fn(ctx, execution, arguments)
}

type Service struct {
	repo           repository
	challenges     challengeProvider
	openings       openingSeeder
	identity       identity.Provider
	actions        map[string]RegisteredAction
	projections    map[string]ProjectionProvider
	gameplay       *gameplay.Registry
	roll           *roll.Service
	systemSettings systemSettingsProvider
}

func NewService(
	repo repository,
	challenges challengeProvider,
	openings openingSeeder,
	ident identity.Provider,
	registries ...*gameplay.Registry,
) *Service {
	service := &Service{
		repo: repo, challenges: challenges, openings: openings, identity: ident,
		actions: map[string]RegisteredAction{}, projections: map[string]ProjectionProvider{},
	}
	if len(registries) > 0 {
		service.gameplay = registries[0]
	}
	return service
}

// RegisterAction installs a complete trusted action during application startup.
func (s *Service) RegisterAction(action RegisteredAction) {
	if err := validateActionDefinition(action.Definition); err != nil || action.Capability == nil || action.Handler == nil {
		panic("invalid campaign action registration: " + action.Definition.Key)
	}
	if _, exists := s.actions[action.Definition.Key]; exists {
		panic("duplicate campaign action registration: " + action.Definition.Key)
	}
	s.actions[action.Definition.Key] = action
}

func (s *Service) RegisterProjection(key string, provider ProjectionProvider) {
	if !validStableKey(key) || provider == nil {
		panic("invalid campaign projection provider")
	}
	if _, exists := s.projections[key]; exists {
		panic("duplicate campaign projection provider: " + key)
	}
	s.projections[key] = provider
}

type CreateInput struct {
	Title         string `json:"title"`
	ChallengeSlug string `json:"challenge_slug"`
	PersonaSlug   string `json:"persona_slug"`
}

func (s *Service) Create(ctx context.Context, in CreateInput) (*storage.Campaign, error) {
	playerID, err := s.currentPlayerID(ctx)
	if err != nil {
		return nil, err
	}
	selection, err := s.challenges.ResolveSelection(ctx, in.ChallengeSlug, in.PersonaSlug, playerID)
	if err != nil {
		return nil, err
	}
	if gameplay.NormalizeRuntimeMode(selection.Contract.Mode) != gameplay.RuntimeModeCampaign || selection.Contract.Campaign == nil {
		return nil, ErrCampaignModeRequired
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = strings.TrimSpace(selection.ChallengeName)
	}
	if title == "" {
		title = "长期存档"
	}
	if len([]rune(title)) > 256 {
		return nil, fmt.Errorf("%w: 存档标题不能超过 256 个字符", ErrInvalidInput)
	}
	seed, err := randomSeed()
	if err != nil {
		return nil, fmt.Errorf("生成存档随机种子: %w", err)
	}
	now := time.Now()
	row := &storage.Campaign{
		ID: uuid.New(), PlayerID: playerID, Title: title,
		ChallengeVersionID: selection.Version.ID, ChallengeHash: selection.Version.ContentHash,
		Status:             storage.CampaignStatusActive,
		CurrentLocationKey: selection.Contract.Campaign.InitialLocationKey,
		RandomSeed:         seed, CreatedAt: now, UpdatedAt: now,
	}
	if selection.Character != nil {
		row.CharacterAssetID = &selection.Character.ID
	}
	if selection.Persona != nil {
		row.PersonaAssetID = &selection.Persona.ID
	}
	if selection.Scenario != nil {
		row.ScenarioAssetID = &selection.Scenario.ID
	}
	requireRealm := true
	if s.gameplay != nil {
		requireRealm = s.gameplay.CampaignRequiresRealm(&selection.Contract)
	}
	if err := s.repo.Create(ctx, row, storage.CampaignInitialization{RequireRealm: requireRealm}); err != nil {
		return nil, err
	}
	return row, nil
}

func (s *Service) GetOwned(ctx context.Context, id uuid.UUID) (*storage.Campaign, error) {
	playerID, err := s.currentPlayerID(ctx)
	if err != nil {
		return nil, err
	}
	row, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if row.PlayerID != playerID {
		return nil, storage.ErrNotFound
	}
	return row, nil
}

func (s *Service) ListOwned(ctx context.Context, statuses []string) ([]storage.Campaign, error) {
	playerID, err := s.currentPlayerID(ctx)
	if err != nil {
		return nil, err
	}
	for _, status := range statuses {
		if !validCampaignStatus(status) {
			return nil, fmt.Errorf("%w: 存档状态无效: %s", ErrInvalidInput, status)
		}
	}
	return s.repo.List(ctx, playerID, statuses)
}

func (s *Service) ListEvents(ctx context.Context, id uuid.UUID, afterSequence int64, limit int) ([]storage.CampaignEvent, error) {
	if _, err := s.GetOwned(ctx, id); err != nil {
		return nil, err
	}
	if afterSequence < 0 {
		return nil, fmt.Errorf("%w: after_sequence 不能为负数", ErrInvalidInput)
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	return s.repo.ListEvents(ctx, id, []string{storage.CampaignVisibilityPlayer}, afterSequence, limit)
}

func (s *Service) ListConversations(ctx context.Context, id uuid.UUID) ([]storage.Conversation, error) {
	if _, err := s.GetOwned(ctx, id); err != nil {
		return nil, err
	}
	return s.repo.ListConversations(ctx, id)
}

func (s *Service) CreateConversation(ctx context.Context, id uuid.UUID) (*storage.Conversation, error) {
	row, err := s.GetOwned(ctx, id)
	if err != nil {
		return nil, err
	}
	if row.Status != storage.CampaignStatusActive {
		return nil, storage.ErrCampaignNotActive
	}
	contract, err := s.challenges.RuntimeForVersion(ctx, row.ChallengeVersionID, row.ChallengeHash)
	if err != nil {
		return nil, err
	}
	if contract == nil || gameplay.NormalizeRuntimeMode(contract.Mode) != gameplay.RuntimeModeCampaign || contract.Campaign == nil {
		return nil, ErrCampaignModeRequired
	}
	now := time.Now()
	conv := &storage.Conversation{
		ID: uuid.New(), PlayerID: row.PlayerID.String(), Title: row.Title,
		Status: storage.StatusActive, ChallengeVersionID: row.ChallengeVersionID,
		ChallengeHash: row.ChallengeHash, CampaignID: &row.ID,
		CharacterAssetID: row.CharacterAssetID, PersonaAssetID: row.PersonaAssetID,
		ScenarioAssetID: row.ScenarioAssetID, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.repo.CreateConversation(
		ctx, row.PlayerID, conv, storage.JSONMap(gameplay.InitialState(contract.StateSchema)), contract.StateSchema.SchemaVersion,
	); err != nil {
		return nil, err
	}
	if s.openings != nil {
		s.openings.SeedOpening(ctx, conv, now)
	}
	return conv, nil
}

type ActionInput struct {
	ActionID         string         `json:"action_id"`
	Key              string         `json:"key"`
	ExpectedRevision int64          `json:"expected_revision"`
	Arguments        map[string]any `json:"arguments"`
}

func (s *Service) ExecutePlayerAction(ctx context.Context, id uuid.UUID, in ActionInput) (*storage.CampaignActionResult, error) {
	return s.executeAction(ctx, s.repo, id, in, "player", nil, nil)
}

// ExecuteModelAction is the Tool Calling entry point. Identity, revision,
// capability and idempotency are revalidated inside the trusted transaction.
func (s *Service) ExecuteModelAction(ctx context.Context, id uuid.UUID, in ActionInput) (*storage.CampaignActionResult, error) {
	return s.executeAction(ctx, s.repo, id, in, "model", nil, nil)
}

// ExecuteModelActionWithCommitter lets orchestration bind the action to an
// outer transaction that also persists the assistant message and state patch.
// trusted 提供回合内已固定的玩法合约、平台设置和事务内判定服务，动作执行
// 期间不再触碰全局连接池。
func (s *Service) ExecuteModelActionWithCommitter(
	ctx context.Context,
	committer ActionCommitter,
	id uuid.UUID,
	in ActionInput,
	sourceMessageID *uuid.UUID,
	trusted *TrustedActionContext,
) (*storage.CampaignActionResult, error) {
	if committer == nil {
		return nil, errors.New("Campaign 动作提交器未配置")
	}
	return s.executeAction(ctx, committer, id, in, "model", sourceMessageID, trusted)
}

func (s *Service) executeAction(
	ctx context.Context,
	committer ActionCommitter,
	id uuid.UUID,
	in ActionInput,
	actorType string,
	sourceMessageID *uuid.UUID,
	trusted *TrustedActionContext,
) (*storage.CampaignActionResult, error) {
	playerID, err := s.currentPlayerID(ctx)
	if err != nil {
		return nil, err
	}
	if err := validateActionInput(in); err != nil {
		return nil, err
	}
	if in.Arguments == nil {
		in.Arguments = map[string]any{}
	}
	registered, ok := s.actions[in.Key]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownAction, in.Key)
	}
	if !actionAllowsActor(registered.Definition, actorType) {
		return nil, fmt.Errorf("%w: 动作 %s 不允许由 %s 执行", ErrActionRejected, in.Key, actorType)
	}
	if err := validateActionArguments(registered.Definition.ArgumentSchema, in.Arguments); err != nil {
		return nil, err
	}
	campaignRow, err := committer.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if campaignRow.PlayerID != playerID {
		return nil, storage.ErrNotFound
	}
	var contract *gameplay.RuntimeContract
	if trusted != nil && trusted.Contract != nil {
		contract = trusted.Contract
	} else {
		contract, err = s.challenges.RuntimeForVersion(ctx, campaignRow.ChallengeVersionID, campaignRow.ChallengeHash)
		if err != nil {
			return nil, err
		}
	}
	if contract == nil || gameplay.NormalizeRuntimeMode(contract.Mode) != gameplay.RuntimeModeCampaign || contract.Campaign == nil {
		return nil, ErrCampaignModeRequired
	}
	if !containsActionKey(contract.Campaign.ActionAllowlist, in.Key) {
		return nil, fmt.Errorf("%w: 动作未被当前玩法启用: %s", ErrUnknownAction, in.Key)
	}
	maxDueEvents := 16
	if contract.Campaign.WorldClock.MaxDueEventsPerTurn > 0 {
		maxDueEvents = contract.Campaign.WorldClock.MaxDueEventsPerTurn
	}
	rollService := s.roll
	var platform *storage.SystemSettings
	if trusted != nil {
		if trusted.Roll != nil {
			rollService = trusted.Roll
		}
		platform = trusted.Platform
	}
	var conversationID uuid.UUID
	if rollService != nil {
		conversations, conversationErr := committer.ListConversations(ctx, id)
		if conversationErr != nil {
			return nil, conversationErr
		}
		if len(conversations) == 1 {
			conversationID = conversations[0].ID
		}
	}
	actorID := playerID.String()
	result, err := committer.CommitAction(ctx, storage.CommitCampaignActionInput{
		CampaignID: id, PlayerID: playerID, ExpectedRevision: in.ExpectedRevision,
		IdempotencyKey: in.ActionID, ActionKey: in.Key, ActorType: actorType,
		ActorID: &actorID, SourceMessageID: sourceMessageID,
	}, func(store *storage.CampaignActionStore, row storage.Campaign) (storage.CampaignActionOutcome, error) {
		if store != nil {
			lockedDomain, domainErr := store.DomainState(ctx, row)
			if domainErr != nil {
				return storage.CampaignActionOutcome{}, domainErr
			}
			providerCtx, cancel := context.WithTimeout(ctx, defaultCapabilityProviderTimeout)
			capability, capabilityErr := registered.Capability.Project(providerCtx, CapabilityContext{
				Campaign: row, Domain: lockedDomain, ActorType: actorType,
				Budget: CapabilityBudget{MaxOptions: defaultCapabilityOptionsPerAction, MaxBytes: defaultCapabilityTotalBytes},
			}, registered.Definition)
			cancel()
			if capabilityErr != nil {
				return storage.CampaignActionOutcome{}, fmt.Errorf("事务内重建动作能力 %s: %w", in.Key, capabilityErr)
			}
			capability, capabilityErr = validateProjectedCapability(
				capability, registered.Definition, row.Revision,
				CapabilityBudget{MaxOptions: defaultCapabilityOptionsPerAction, MaxBytes: defaultCapabilityTotalBytes},
			)
			if capabilityErr != nil {
				return storage.CampaignActionOutcome{}, capabilityErr
			}
			if capabilityErr = validateCapabilityArguments(capability, in.Arguments); capabilityErr != nil {
				return storage.CampaignActionOutcome{}, capabilityErr
			}
		}
		return registered.Handler.Execute(ctx, ActionExecution{
			Campaign: row, Store: store, ActorType: actorType, ActionID: in.ActionID,
			ActionKey: in.Key, SourceMessageID: sourceMessageID, MaxDueEventsPerTurn: maxDueEvents,
			Roll: s.campaignRollFunc(rollService, platform, conversationID),
		}, in.Arguments)
	})
	if err != nil {
		return nil, err
	}
	result.Events = playerVisibleEvents(result.Events)
	return result, nil
}

type AbandonInput struct {
	ActionID         string `json:"action_id"`
	ExpectedRevision int64  `json:"expected_revision"`
}

func (s *Service) Abandon(ctx context.Context, id uuid.UUID, in AbandonInput) (*storage.CampaignActionResult, error) {
	playerID, err := s.currentPlayerID(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.ActionID) == "" || strings.TrimSpace(in.ActionID) != in.ActionID ||
		len(in.ActionID) > 160 || in.ExpectedRevision < 0 {
		return nil, ErrInvalidAction
	}
	actorID := playerID.String()
	status := storage.CampaignStatusAbandoned
	return s.repo.CommitAction(ctx, storage.CommitCampaignActionInput{
		CampaignID: id, PlayerID: playerID, ExpectedRevision: in.ExpectedRevision,
		IdempotencyKey: in.ActionID, ActionKey: "campaign.abandon",
		ActorType: "player", ActorID: &actorID,
	}, func(*storage.CampaignActionStore, storage.Campaign) (storage.CampaignActionOutcome, error) {
		return storage.CampaignActionOutcome{
			Status: &status,
			Events: []storage.CampaignEventDraft{{
				EventType: "campaign.abandoned", Visibility: storage.CampaignVisibilityPlayer,
				Payload: storage.JSONMap{},
			}},
			Result: storage.JSONMap{"status": status},
		}, nil
	})
}

// ContextProjection returns a bounded, non-authoritative view for model
// context. It deliberately excludes random_seed and internal-only events.
func (s *Service) ContextProjection(ctx context.Context, id uuid.UUID, query ...string) (ProjectionView, error) {
	row, err := s.GetOwned(ctx, id)
	if err != nil {
		return ProjectionView{}, err
	}
	events, err := s.repo.RecentEvents(ctx, id, []string{
		storage.CampaignVisibilityPlayer, storage.CampaignVisibilityModel,
	}, 16)
	if err != nil {
		return ProjectionView{}, err
	}
	domain, err := s.repo.DomainState(ctx, id)
	if err != nil {
		return ProjectionView{}, err
	}
	contract, err := s.challenges.RuntimeForVersion(ctx, row.ChallengeVersionID, row.ChallengeHash)
	if err != nil {
		return ProjectionView{}, err
	}
	if contract == nil || gameplay.NormalizeRuntimeMode(contract.Mode) != gameplay.RuntimeModeCampaign || contract.Campaign == nil {
		return ProjectionView{}, ErrCampaignModeRequired
	}
	provider, ok := s.projections[contract.Campaign.ProjectionHandler]
	if !ok {
		return ProjectionView{}, fmt.Errorf("Campaign 投影处理器未安装: %s", contract.Campaign.ProjectionHandler)
	}
	projection := map[string]any{
		"id": row.ID.String(), "status": row.Status,
		"current_location_key": row.CurrentLocationKey, "game_time": row.GameTime,
		"revision": row.Revision, "recent_events": boundedContextEvents(events, 16*1024),
	}
	referenceQuery := ""
	if len(query) > 0 {
		referenceQuery = query[0]
	}
	domainView, err := provider.Context(ctx, ProjectionContext{
		Campaign: *row, Domain: domain, Query: referenceQuery, ContextPolicy: contract.Campaign.ContextPolicy,
	})
	if err != nil {
		return ProjectionView{}, fmt.Errorf("构建 Campaign 玩法投影: %w", err)
	}
	if err := mergeProjection(projection, domainView.Data); err != nil {
		return ProjectionView{}, err
	}
	capabilities, err := s.actionCapabilities(ctx, *row, domain, contract.Campaign.ActionAllowlist, "model", referenceQuery)
	if err != nil {
		return ProjectionView{}, err
	}
	projection["action_capabilities"] = capabilities
	priority := make([]string, 0, len(domainView.Priority)+1)
	priority = append(priority, "recent_events")
	priority = append(priority, domainView.Priority...)
	return ProjectionView{Data: projection, Priority: priority}, nil
}

func actionAllowsActor(definition ActionDefinition, actorType string) bool {
	for _, actor := range definition.Actors {
		if actor == actorType {
			return true
		}
	}
	return false
}

func (s *Service) Snapshot(ctx context.Context, id uuid.UUID) (map[string]any, error) {
	row, err := s.GetOwned(ctx, id)
	if err != nil {
		return nil, err
	}
	domain, err := s.repo.DomainState(ctx, id)
	if err != nil {
		return nil, err
	}
	contract, err := s.challenges.RuntimeForVersion(ctx, row.ChallengeVersionID, row.ChallengeHash)
	if err != nil {
		return nil, err
	}
	if contract == nil || gameplay.NormalizeRuntimeMode(contract.Mode) != gameplay.RuntimeModeCampaign || contract.Campaign == nil {
		return nil, ErrCampaignModeRequired
	}
	provider, ok := s.projections[contract.Campaign.ProjectionHandler]
	if !ok {
		return nil, fmt.Errorf("Campaign 投影处理器未安装: %s", contract.Campaign.ProjectionHandler)
	}
	snapshot, err := provider.Snapshot(ctx, ProjectionContext{
		Campaign: *row, Domain: domain, ContextPolicy: contract.Campaign.ContextPolicy,
	})
	if err != nil {
		return nil, err
	}
	if snapshot == nil {
		snapshot = map[string]any{}
	}
	if _, exists := snapshot["outcome_rewards"]; exists {
		return nil, fmt.Errorf("Campaign 玩法投影覆盖了平台保留字段: outcome_rewards")
	}
	rewards := domain.OutcomeRewards
	if rewards == nil {
		rewards = []storage.CampaignOutcomeReward{}
	}
	snapshot["outcome_rewards"] = rewards
	return snapshot, nil
}

func (s *Service) actionCapabilities(
	ctx context.Context,
	row storage.Campaign,
	domain *storage.CampaignDomainState,
	allowlist []string,
	actorType string,
	query string,
) ([]ActionCapability, error) {
	const maxActions = 64
	if len(allowlist) > maxActions {
		return nil, fmt.Errorf("Campaign 动作数量超过上限: %d", len(allowlist))
	}
	budget := CapabilityBudget{MaxOptions: defaultCapabilityOptionsPerAction, MaxBytes: defaultCapabilityTotalBytes}
	out := make([]ActionCapability, 0, len(allowlist))
	seen := map[string]bool{}
	for _, key := range allowlist {
		if seen[key] {
			return nil, fmt.Errorf("Campaign 动作白名单包含重复项: %s", key)
		}
		seen[key] = true
		registered, ok := s.actions[key]
		if !ok {
			return nil, fmt.Errorf("Campaign 动作未安装: %s", key)
		}
		if !actionAllowsActor(registered.Definition, actorType) {
			continue
		}
		providerCtx, cancel := context.WithTimeout(ctx, defaultCapabilityProviderTimeout)
		capability, err := registered.Capability.Project(providerCtx, CapabilityContext{
			Campaign: row, Domain: domain, ActorType: actorType, Query: query, Budget: budget,
		}, registered.Definition)
		cancel()
		if err != nil {
			return nil, fmt.Errorf("构建动作能力 %s: %w", key, err)
		}
		capability, err = validateProjectedCapability(capability, registered.Definition, row.Revision, budget)
		if err != nil {
			return nil, err
		}
		out = append(out, capability)
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("序列化动作能力: %w", err)
	}
	if len(raw) > budget.MaxBytes {
		return nil, fmt.Errorf("Campaign 动作能力投影超过字节上限: %d", len(raw))
	}
	return out, nil
}

func mergeProjection(base, extension map[string]any) error {
	reserved := map[string]bool{
		"id": true, "status": true, "current_location_key": true, "game_time": true,
		"revision": true, "recent_events": true, "action_capabilities": true,
		"projection_control": true,
	}
	for key, value := range extension {
		if reserved[key] {
			return fmt.Errorf("玩法投影覆盖了平台保留字段: %s", key)
		}
		base[key] = value
	}
	return nil
}

func containsActionKey(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func (s *Service) currentPlayerID(ctx context.Context) (uuid.UUID, error) {
	playerID, err := uuid.Parse(s.identity.CurrentPlayerID(ctx))
	if err != nil {
		return uuid.Nil, storage.ErrNotFound
	}
	return playerID, nil
}

func validateActionInput(in ActionInput) error {
	if strings.TrimSpace(in.ActionID) == "" || strings.TrimSpace(in.ActionID) != in.ActionID || len(in.ActionID) > 160 ||
		!validStableKey(in.Key) || in.ExpectedRevision < 0 {
		return ErrInvalidAction
	}
	return nil
}

func validStableKey(value string) bool {
	if value == "" || value != strings.TrimSpace(value) || len(value) > 128 {
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

func validCampaignStatus(status string) bool {
	switch status {
	case storage.CampaignStatusActive, storage.CampaignStatusCompleted,
		storage.CampaignStatusDead, storage.CampaignStatusAbandoned:
		return true
	default:
		return false
	}
}

func randomSeed() (int64, error) {
	value, err := cryptorand.Int(cryptorand.Reader, big.NewInt(1<<63-1))
	if err != nil {
		return 0, err
	}
	return value.Int64(), nil
}

func playerVisibleEvents(events []storage.CampaignEvent) []storage.CampaignEvent {
	visible := make([]storage.CampaignEvent, 0, len(events))
	for _, event := range events {
		if event.Visibility == storage.CampaignVisibilityPlayer {
			visible = append(visible, event)
		}
	}
	return visible
}

func boundedContextEvents(events []storage.CampaignEvent, budget int) []storage.CampaignEvent {
	if budget <= 0 {
		return nil
	}
	selected := make([]storage.CampaignEvent, 0, len(events))
	used := 2 // JSON array brackets.
	for index := len(events) - 1; index >= 0; index-- {
		encoded, err := json.Marshal(events[index])
		if err != nil || used+len(encoded)+1 > budget {
			continue
		}
		selected = append(selected, events[index])
		used += len(encoded) + 1
	}
	for left, right := 0, len(selected)-1; left < right; left, right = left+1, right-1 {
		selected[left], selected[right] = selected[right], selected[left]
	}
	return selected
}
