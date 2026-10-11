package campaignactions

import (
	"context"
	"errors"
	"time"

	"github.com/ialtone/fatecraft/internal/campaign"
	"github.com/ialtone/fatecraft/internal/gameplay/components/worldactions"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/effectrules"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/trainingrules"
	"github.com/ialtone/fatecraft/internal/storage"
)

func retreat(_ context.Context, execution campaign.ActionExecution, args map[string]any) (storage.CampaignActionOutcome, error) {
	return executeTraining(execution, args, "retreat")
}
func recoverCharacter(_ context.Context, execution campaign.ActionExecution, args map[string]any) (storage.CampaignActionOutcome, error) {
	return executeTraining(execution, args, "recovery")
}

func executeTraining(execution campaign.ActionExecution, args map[string]any, mode string) (storage.CampaignActionOutcome, error) {
	if err := worldactions.AllowArguments(args, "plan_key"); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	key, err := worldactions.RequiredKey(args, "plan_key")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	d, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, trainingrules.DefinitionKind, key)
	if err != nil {
		return storage.CampaignActionOutcome{}, worldactions.Rejected("修行方案不存在")
	}
	p, err := trainingrules.Parse(d.Content)
	if err != nil {
		return storage.CampaignActionOutcome{}, worldactions.Invalid("修行方案无效: %s", err)
	}
	if p.Mode != mode {
		return storage.CampaignActionOutcome{}, worldactions.Rejected("此方案不适用于当前行动")
	}
	minutes := p.Days * 1440
	if err := worldactions.CheckTimeAdvance(execution, minutes, 3); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	state := &storage.CampaignDomainState{Definitions: map[string]storage.GameplayDefinition{}}
	state.Character, err = cultivationStore(execution).Character(execution.Campaign.ID)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	state.Techniques, err = cultivationStore(execution).Techniques(execution.Campaign.ID)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	for _, t := range state.Techniques {
		definition, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionTechnique, t.TechniqueKey)
		if err != nil {
			return storage.CampaignActionOutcome{}, err
		}
		state.Definitions[definition.Kind+":"+definition.Key] = *definition
	}
	stacks := []*storage.CampaignItemStack{}
	for _, m := range p.Materials {
		item, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionItem, m.ItemKey)
		if err != nil {
			return storage.CampaignActionOutcome{}, err
		}
		state.Definitions[item.Kind+":"+item.Key] = *item
		stack, err := execution.Store.ItemStack(execution.Campaign.ID, m.ItemKey, "common")
		if errors.Is(err, storage.ErrNotFound) {
			continue
		}
		if err != nil {
			return storage.CampaignActionOutcome{}, err
		}
		state.ItemStacks = append(state.ItemStacks, *stack)
		stacks = append(stacks, stack)
	}
	if active, err := cultivationStore(execution).ActiveEncounter(execution.Campaign.ID); err == nil {
		state.Encounters = append(state.Encounters, *active)
	} else if !errors.Is(err, storage.ErrNotFound) {
		return storage.CampaignActionOutcome{}, err
	}
	// The preflight already proved all roots fit in the event budget.
	due, err := execution.Store.DueScheduledEvents(execution.Campaign.ID, execution.Campaign.GameTime+minutes, 64)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	for _, event := range due {
		if event.EventKind == "quest_deadline" {
			q, err := execution.Store.Quest(execution.Campaign.ID, event.TemplateKey)
			if err != nil {
				return storage.CampaignActionOutcome{}, err
			}
			state.Quests = append(state.Quests, *q)
		}
	}
	if err := trainingrules.Ready(p, execution.Campaign, state); err != nil {
		return storage.CampaignActionOutcome{}, worldactions.Rejected("%s", err)
	}
	character := state.Character
	// Copy condition maps before editing: failed validation must not alter the
	// caller's in-memory facts, and SQL writes still share the action transaction.
	injuries := storage.JSONMap{}
	for key, raw := range character.Injuries {
		injuries[key] = raw
	}
	character.Injuries = injuries
	effects, err := recalculateCharacterStats(execution, character)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	healing := worldactions.DefinitionIntDefault(effects["healing_power"], 0)
	actualGain := int64(0)
	if mode == "retreat" {
		t := trainingrules.Technique(p, state)
		d, exists := state.Definition(storage.DefinitionTechnique, t.TechniqueKey)
		if !exists {
			return storage.CampaignActionOutcome{}, worldactions.Invalid("功法定义不存在")
		}
		actualGain = min(p.CultivationGain, character.CultivationRequired-character.CultivationProgress)
		character.CultivationProgress += actualGain
		character.Mana -= p.ManaCost
		t.Mastery = min(int64(10000), t.Mastery+p.MasteryGain)
		t.LearnedLayer, _ = effectrules.LayerForMastery(t.Mastery, worldactions.DefinitionIntDefault(d.Content["max_layer"], 1))
		t.UpdatedAt = time.Now()
		if err := cultivationStore(execution).SaveTechnique(t); err != nil {
			return storage.CampaignActionOutcome{}, err
		}
		realm, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionRealm, character.RealmKey)
		if err != nil {
			return storage.CampaignActionOutcome{}, err
		}
		character.StageKey = stageForProgress(realm.Content, character.CultivationProgress, character.CultivationRequired)
		if _, err := recalculateCharacterStats(execution, character); err != nil {
			return storage.CampaignActionOutcome{}, err
		}
	} else {
		character.Health = min(character.MaxHealth, character.Health+effectrules.HealingAmount(character.MaxHealth*p.HealthBPS/10000, healing))
		character.Mana = min(character.MaxMana, character.Mana+character.MaxMana*p.ManaBPS/10000)
		character.SpiritualSense = min(character.MaxSpiritualSense, character.SpiritualSense+character.MaxSpiritualSense*p.SenseBPS/10000)
		for key, raw := range character.Injuries {
			condition, ok := raw.(map[string]any)
			if !ok || condition["category"] != "injury" || p.InjuryRelief == 0 {
				continue
			}
			severity := worldactions.DefinitionIntDefault(condition["severity"], 0) - p.InjuryRelief
			if severity <= 0 {
				delete(character.Injuries, key)
			} else {
				next := map[string]any{}
				for k, v := range condition {
					next[k] = v
				}
				next["severity"] = severity
				character.Injuries[key] = next
			}
		}
	}
	for i, stack := range stacks {
		stack.Quantity -= p.Materials[i].Quantity
		stack.UpdatedAt = time.Now()
		if err := execution.Store.SaveItemStack(stack); err != nil {
			return storage.CampaignActionOutcome{}, err
		}
	}
	advanceCharacterAge(character, execution.Campaign.GameTime, minutes)
	character.UpdatedAt = time.Now()
	if err := cultivationStore(execution).SaveCharacter(character); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	payload := storage.JSONMap{"plan_key": key, "name": d.Name, "days": p.Days, "minutes": minutes, "materials": p.Materials, "cultivation_gain": actualGain, "health": character.Health, "mana": character.Mana, "spiritual_sense": character.SpiritualSense, "age_days": character.AgeDays}
	event := "cultivation.retreated"
	if mode == "recovery" {
		event = "character.recovered"
	}
	return storage.CampaignActionOutcome{GameTimeDelta: minutes, Events: []storage.CampaignEventDraft{{EventType: event, Visibility: storage.CampaignVisibilityPlayer, Payload: payload}}, Result: payload}, nil
}
