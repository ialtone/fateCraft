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
	"sort"
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
		result.Definitions = append(result.Definitions, contract.Definition{Kind: d.Kind, Key: d.Key, Name: d.Name, Content: d.Content, Tags: d.Tags, Visibility: d.Visibility, SortOrder: d.SortOrder})
	}
	sort.Slice(result.Definitions, func(i, j int) bool {
		return result.Definitions[i].Kind+result.Definitions[i].Key < result.Definitions[j].Kind+result.Definitions[j].Key
	})
	for _, o := range domain.Outcomes {
		result.Outcomes = append(result.Outcomes, contract.Outcome{Key: o.OutcomeKey, Enabled: o.Enabled, Category: o.Category, RewardPoints: o.RewardPoints})
	}
	return result, nil
}

type adapter struct{ descriptor contract.ModuleDescriptor }

func Install(service *campaign.Service, descriptor contract.ModuleDescriptor) {
	if descriptor.Runtime == nil || !slices.Contains(descriptor.Modes, contract.RuntimeModeCampaign) {
		return
	}
	a := adapter{descriptor: descriptor}
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
		rewards, err := store.GrantCampaignOutcomeRewards(ctx, input.Campaign, key)
		if err != nil {
			return storage.CampaignActionOutcome{}, err
		}
		if outcome.Result == nil {
			outcome.Result = storage.JSONMap{}
		}
		outcome.Result["outcome_rewards"] = rewards
	}
	return outcome, nil
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
	return a.project(input, contract.AudiencePlayer)
}
func (a adapter) Inquiry(_ context.Context, input campaign.ProjectionContext) (map[string]any, error) {
	return a.project(input, contract.AudienceInquiry)
}
func (a adapter) Context(_ context.Context, input campaign.ProjectionContext) (campaign.ProjectionView, error) {
	view, err := a.project(input, contract.AudienceModel)
	paths := []string{}
	for _, path := range contract.NarrativeFactPaths(a.descriptor.Runtime) {
		paths = append(paths, "gameplay."+path)
	}
	return campaign.ProjectionView{Data: view, Priority: []string{"gameplay"}, FactPaths: paths}, err
}
