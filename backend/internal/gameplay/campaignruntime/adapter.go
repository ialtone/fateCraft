// Package campaignruntime binds pure trusted modules to the existing Campaign
// transaction, event, reward and projection lifecycle.
package campaignruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/ialtone/fatecraft/internal/campaign"
	"github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/gameplay/kernel"
	"github.com/ialtone/fatecraft/internal/storage"
	"gorm.io/gorm"
	"slices"
	"strings"
)

func decode[T any](value any) (T, error) {
	var out T
	raw, err := json.Marshal(value)
	if err != nil {
		return out, err
	}
	err = json.Unmarshal(raw, &out)
	return out, err
}

func content(domain *storage.CampaignDomainState) (contract.Content, error) {
	runtime, err := storage.DecodeGameplayRuntime(domain.RuntimeDocument)
	if err != nil {
		return contract.Content{}, err
	}
	result := contract.Content{Runtime: runtime}
	for _, d := range domain.Definitions {
		definition := contract.Definition{Kind: d.Kind, Key: d.Key, Name: d.Name, Content: d.Content, Tags: d.Tags, Visibility: d.Visibility, SortOrder: d.SortOrder}
		if contract.IsPlatformDefinitionKind(d.Kind) {
			result.PlatformDefinitions = append(result.PlatformDefinitions, definition)
			continue
		}
		result.Definitions = append(result.Definitions, definition)
	}
	sortDefinitions(result.Definitions)
	sortDefinitions(result.PlatformDefinitions)
	for _, o := range domain.Outcomes {
		result.Outcomes = append(result.Outcomes, contract.Outcome{Key: o.OutcomeKey, Enabled: o.Enabled, Category: o.Category, RewardPoints: o.RewardPoints})
	}
	return result, nil
}

func sortDefinitions(definitions []contract.Definition) {
	slices.SortFunc(definitions, func(left, right contract.Definition) int {
		return strings.Compare(left.Kind+left.Key, right.Kind+right.Key)
	})
}

type adapter struct {
	descriptor contract.ModuleDescriptor
	registry   *contract.Registry
}

func Install(service *campaign.Service, descriptor contract.ModuleDescriptor, registry *contract.Registry) {
	if descriptor.Runtime == nil || !slices.Contains(descriptor.Modes, contract.RuntimeModeCampaign) {
		return
	}
	a := adapter{descriptor: descriptor, registry: registry}
	service.RegisterInitializer(descriptor.CampaignLifecycleHandlers[0], a.initialize)
	service.RegisterProjection(descriptor.CampaignProjectionHandlers[0], a)
	for _, action := range descriptor.Runtime.Actions() {
		service.RegisterAction(contract.EngineRef{Key: descriptor.Key, Version: descriptor.Version}, campaign.RegisteredAction{
			Definition: campaign.ActionDefinition{Key: action.Key, Ending: action.Ending, Description: action.Description, Actors: action.Actors, ArgumentSchema: action.ArgumentSchema(), Writes: descriptor.CampaignActionWrites[action.Key], Events: descriptor.CampaignActionEvents[action.Key]},
			Capability: campaign.ActionCapabilityProviderFunc(a.capability), Handler: campaign.ActionHandlerFunc(a.execute),
		})
	}
}

func (a adapter) initialize(tx *gorm.DB, row *storage.Campaign) error {
	domain, err := storage.CampaignRuntimeInitialContent(tx.Statement.Context, tx, *row)
	if err != nil {
		return err
	}
	rules, err := content(domain)
	if err != nil {
		return err
	}
	persona := ""
	if row.PersonaAssetID != nil {
		var asset storage.ChallengeAsset
		if err := tx.First(&asset, "id = ? AND challenge_version_id = ?", *row.PersonaAssetID, row.ChallengeVersionID).Error; err != nil {
			return err
		}
		persona = asset.Slug
	}
	initial, err := kernel.Initialize(a.descriptor.Runtime, rules, contract.InitialInput{PersonaKey: persona, Random: kernel.NewRandom(row.RandomSeed, "initial")})
	if err != nil {
		return err
	}
	data, err := decode[storage.JSONMap](initial)
	if err != nil {
		return err
	}
	return storage.InitializeCampaignRuntimeState(tx, row.ID, data)
}

func (a adapter) state(row storage.Campaign, domain *storage.CampaignDomainState) (contract.Content, contract.RunState, error) {
	rules, err := content(domain)
	if err != nil {
		return rules, contract.RunState{}, err
	}
	run, err := decode[contract.RunState](domain.RuntimeState)
	if err != nil {
		return rules, run, err
	}
	if rules.Runtime.Engine != (contract.EngineRef{Key: a.descriptor.Key, Version: a.descriptor.Version}) || run.Revision != row.Revision || run.GameTime != row.GameTime {
		return rules, run, fmt.Errorf("长期玩法状态与存档游标不一致")
	}
	if (row.Status == storage.CampaignStatusActive) != (run.Status == contract.RunActive) || row.OutcomeKey != run.OutcomeKey || row.Status != storage.CampaignStatusActive && row.Status != storage.CampaignStatusCompleted && row.Status != storage.CampaignStatusDead && row.Status != storage.CampaignStatusAbandoned {
		return rules, run, fmt.Errorf("长期玩法终态与存档不一致")
	}
	return rules, run, kernel.ValidateState(a.descriptor.Runtime, rules, run)
}

func (a adapter) capability(_ context.Context, input campaign.CapabilityContext, definition campaign.ActionDefinition) (campaign.ActionCapability, error) {
	result := campaign.ActionCapability{Key: definition.Key, Description: definition.Description, Revision: input.Campaign.Revision, ArgumentSchema: definition.ArgumentSchema}
	rules, run, err := a.state(input.Campaign, input.Domain)
	if err != nil {
		return result, err
	}
	previews, err := kernel.Preview(a.descriptor.Runtime, rules, run, input.ActorType)
	if err != nil {
		return result, err
	}
	for _, p := range previews {
		if p.Action.Key != definition.Key {
			continue
		}
		result.Available = p.Available
		result.ArgumentSchema = p.Action.ArgumentSchema()
		result.Metadata = storage.JSONMap{"label": p.Action.Label, "minutes": p.Minutes, "costs": p.Costs, "risk": p.Risk, "parameters": p.Action.Parameters, "plans": p.Plans, "cost_labels": p.CostLabels}
		if !p.Available {
			result.Violations = []campaign.ActionViolation{{Code: "action.unavailable", Message: p.Reason, Public: true}}
		}
		return result, nil
	}
	result.Violations = []campaign.ActionViolation{{Code: "action.unavailable", Message: "本局已结束或无权执行此行动", Public: true}}
	return result, nil
}

func (a adapter) execute(ctx context.Context, input campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	store, ok := input.Store.(*storage.CampaignActionStore)
	if !ok {
		return storage.CampaignActionOutcome{}, fmt.Errorf("可信模块缺少事务仓储")
	}
	domain, err := store.DomainState(ctx, input.Campaign)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	rules, run, err := a.state(input.Campaign, domain)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	transition, err := kernel.Execute(a.descriptor.Runtime, rules, run, contract.RuntimeRequest{ActionKey: input.ActionKey, Actor: input.ActorType, Arguments: arguments, Random: kernel.NewRandom(input.Campaign.RandomSeed, fmt.Sprintf("revision/%d", run.Revision))})
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	data, err := decode[storage.JSONMap](transition.State)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if err := store.SaveRuntimeState(data); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	outcome := storage.CampaignActionOutcome{GameTimeDelta: transition.State.GameTime - run.GameTime, Result: storage.JSONMap(transition.Result)}
	if outcome.Result == nil {
		outcome.Result = storage.JSONMap{}
	}
	for _, event := range transition.Events {
		outcome.Events = append(outcome.Events, storage.CampaignEventDraft{EventType: event.Kind, Visibility: event.Visibility, Payload: storage.JSONMap(event.Data)})
	}
	// A narrative turn can contain several module actions. Checkpoints capture
	// only the turn boundary, so retain every action's draws in append-only
	// internal events as well. Chunks stay below the event payload budget.
	for offset := 0; offset < len(transition.State.RandomEvidence); offset += 16 {
		draws := transition.State.RandomEvidence[offset:min(offset+16, len(transition.State.RandomEvidence))]
		outcome.Events = append(outcome.Events, storage.CampaignEventDraft{EventType: contract.CampaignModuleRandomEvent, Visibility: contract.VisibilityInternal, Payload: storage.JSONMap{"engine": rules.Runtime.Engine, "action": input.ActionKey, "revision": transition.State.Revision, "draws": draws}})
	}
	// Record milestones reached by this action on the current branch. Nothing
	// is paid here; the receipt is written only when the run ends.
	row := input.Campaign
	row.Revision = transition.State.Revision
	row.GameTime = transition.State.GameTime
	progressRules, hasRules, err := contract.ParseProgressRewards(rules)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if hasRules {
		reached, err := a.reachedStages(progressRules, rules, transition.State)
		if err != nil {
			return storage.CampaignActionOutcome{}, err
		}
		keys := make([]string, 0, len(reached))
		for _, stage := range reached {
			keys = append(keys, stage.Key)
		}
		added, _, err := store.RecordCampaignStageProgress(ctx, row, keys)
		if err != nil {
			return storage.CampaignActionOutcome{}, err
		}
		// Report only the milestones first recorded by this action so the
		// player is not notified again on every later turn. The snapshot
		// endpoint exposes the complete current branch progress.
		views, pending := progressViews(progressRules, added, row.LLMRoute)
		outcome.Result["stage_progress"] = views
		outcome.Result["pending_stage_points"] = pending
	}
	if transition.State.Status != contract.RunActive {
		status := storage.CampaignStatusCompleted
		key := transition.State.OutcomeKey
		for _, outcome := range rules.Outcomes {
			if outcome.Key != key || !outcome.Enabled {
				continue
			}
			switch outcome.Category {
			case "failure":
				status = storage.CampaignStatusDead
			case "neutral":
				status = storage.CampaignStatusAbandoned
			}
			break
		}
		outcome.Status = &status
		outcome.OutcomeKey = &key
		if hasRules {
			grants := make([]storage.StageRewardGrant, 0, len(progressRules.Stages))
			for _, stage := range contract.SortedProgressRewardStages(progressRules.Stages) {
				grants = append(grants, storage.StageRewardGrant{Key: stage.Key, Name: stage.Name, SortOrder: stage.Order, Points: stage.Points})
			}
			rows, points, err := store.SettleCampaignStageRewards(ctx, row, grants)
			if err != nil {
				return storage.CampaignActionOutcome{}, err
			}
			outcome.Result["stage_progress"] = []storage.StageProgressView{}
			outcome.Result["pending_stage_points"] = int64(0)
			outcome.Result["stage_rewards"] = rows
			outcome.Result["stage_reward_points"] = points
		}
		rewards, err := store.GrantCampaignOutcomeRewards(ctx, input.Campaign, key)
		if err != nil {
			return storage.CampaignActionOutcome{}, err
		}
		outcome.Result["outcome_rewards"] = rewards
	}
	return outcome, nil
}

// reachedStages verifies every configured milestone against the post-action
// player projection. The projection is the same trusted view the player sees,
// and its paths are the ones validated against field ownership at publication.
func (a adapter) reachedStages(rules contract.ProgressRewards, content contract.Content, run contract.RunState) ([]contract.ProgressRewardStage, error) {
	if a.registry == nil {
		return nil, fmt.Errorf("Campaign 阶段核验缺少注册表")
	}
	view, err := kernel.Project(a.descriptor.Runtime, content, run, contract.AudiencePlayer)
	if err != nil {
		return nil, err
	}
	evidence := contract.Evidence{State: map[string]any{"gameplay": view}, Facts: map[string]any{"engine_key": content.Runtime.Engine.Key}}
	reached := make([]contract.ProgressRewardStage, 0, len(rules.Stages))
	for _, stage := range contract.SortedProgressRewardStages(rules.Stages) {
		if err := a.registry.Verify(stage.VerifierKey, evidence, stage.VerifierConfig); err != nil {
			continue
		}
		reached = append(reached, stage)
	}
	return reached, nil
}

func progressViews(rules contract.ProgressRewards, progress []storage.CampaignStageProgress, route string) ([]storage.StageProgressView, int64) {
	byKey := make(map[string]contract.ProgressRewardStage, len(rules.Stages))
	for _, stage := range rules.Stages {
		byKey[stage.Key] = stage
	}
	views := make([]storage.StageProgressView, 0, len(progress))
	var pending int64
	for _, row := range progress {
		stage, ok := byKey[row.StageKey]
		if !ok {
			continue
		}
		disabled := ""
		if route == storage.LLMRouteClient {
			disabled = storage.RewardsDisabledReasonClientRoute
		}
		views = append(views, storage.StageProgressView{StageKey: stage.Key, Name: stage.Name, SortOrder: stage.Order, ConfiguredPoints: stage.Points, ReachedAt: row.CreatedAt, DisabledReason: disabled})
		pending += stage.Points
	}
	return views, pending
}

func (a adapter) project(input campaign.ProjectionContext, audience string) (map[string]any, error) {
	rules, run, err := a.state(input.Campaign, input.Domain)
	if err != nil {
		return nil, err
	}
	view, err := kernel.Project(a.descriptor.Runtime, rules, run, audience)
	if err != nil {
		return nil, err
	}
	return map[string]any{"gameplay": view}, nil
}
func (a adapter) Snapshot(_ context.Context, input campaign.ProjectionContext) (map[string]any, error) {
	view, err := a.project(input, contract.AudiencePlayer)
	if err != nil {
		return nil, err
	}
	return a.withProgress(view, input)
}
func (a adapter) Inquiry(_ context.Context, input campaign.ProjectionContext) (map[string]any, error) {
	view, err := a.project(input, contract.AudienceInquiry)
	if err != nil {
		return nil, err
	}
	return a.withProgress(view, input)
}

// withProgress attaches the branch's unsettled milestones to the player view.
// Ended Campaigns show receipts instead, so pending progress is emptied.
func (a adapter) withProgress(view map[string]any, input campaign.ProjectionContext) (map[string]any, error) {
	if view == nil {
		view = map[string]any{}
	}
	views := []storage.StageProgressView{}
	var pending int64
	if input.Domain != nil && input.Campaign.Status == storage.CampaignStatusActive {
		rulesContent, err := content(input.Domain)
		if err != nil {
			return nil, err
		}
		rules, found, err := contract.ParseProgressRewards(rulesContent)
		if err != nil {
			return nil, err
		}
		if found {
			views, pending = progressViews(rules, input.Domain.StageProgress, input.Campaign.LLMRoute)
		}
	}
	view["stage_progress"] = views
	view["pending_stage_points"] = pending
	return view, nil
}
func (a adapter) Context(_ context.Context, input campaign.ProjectionContext) (campaign.ProjectionView, error) {
	view, err := a.project(input, contract.AudienceModel)
	paths := []string{}
	for _, path := range contract.NarrativeFactPaths(a.descriptor.Runtime) {
		paths = append(paths, "gameplay."+path)
	}
	return campaign.ProjectionView{Data: view, Priority: []string{"gameplay"}, FactPaths: paths}, err
}
