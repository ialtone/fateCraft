package campaignactions

import (
	"context"
	"errors"
	"slices"

	"github.com/ialtone/fatecraft/internal/campaign"
	"github.com/ialtone/fatecraft/internal/gameplay/components/worldactions"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/ascensionrules"
	"github.com/ialtone/fatecraft/internal/storage"
)

func ascensionConfig(execution campaign.ActionExecution) (*ascensionrules.Config, error) {
	d, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, ascensionrules.DefinitionKind, ascensionrules.DefinitionKey)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	c, err := ascensionrules.Parse(d.Content)
	return &c, err
}

func checkAscensionReady(execution campaign.ActionExecution, c ascensionrules.Config) error {
	character, err := cultivationStore(execution).Character(execution.Campaign.ID)
	if err != nil {
		return err
	}
	realm, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionRealm, c.RealmKey)
	if err != nil {
		return err
	}
	completed := map[string]bool{}
	for _, group := range c.QuestGroups {
		for _, key := range group {
			quest, err := execution.Store.Quest(execution.Campaign.ID, key)
			if err != nil && !errors.Is(err, storage.ErrNotFound) {
				return err
			}
			completed[key] = err == nil && quest.Status == "completed"
		}
	}
	if err := ascensionrules.Ready(c, character, realm.Content, completed); err != nil {
		return worldactions.Rejected("%s", err)
	}
	return nil
}

func beginAscension(ctx context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	if err := worldactions.AllowArguments(arguments); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if execution.ActorType != "player" {
		return storage.CampaignActionOutcome{}, worldactions.Rejected("引动最终天劫需要玩家亲自确认")
	}
	c, err := ascensionConfig(execution)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if c == nil {
		return storage.CampaignActionOutcome{}, worldactions.Rejected("当前版本没有开放飞升渡劫")
	}
	if err := checkAscensionReady(execution, *c); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	wins, err := execution.Store.EncounterWinCount(execution.Campaign.ID, c.EncounterKey)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if wins > 0 {
		return storage.CampaignActionOutcome{}, worldactions.Rejected("已经渡过最终天劫，请选择飞升道路")
	}
	stacks := make([]*storage.CampaignItemStack, 0, len(c.Materials))
	for _, m := range c.Materials {
		stack, err := execution.Store.ItemStack(execution.Campaign.ID, m.ItemKey, m.Quality)
		if errors.Is(err, storage.ErrNotFound) || err == nil && stack.Quantity < m.Quantity {
			return storage.CampaignActionOutcome{}, worldactions.Rejected("渡劫准备材料不足")
		}
		if err != nil {
			return storage.CampaignActionOutcome{}, err
		}
		stacks = append(stacks, stack)
	}
	// startEncounterCore still checks location and active encounters. Only this
	// player-owned entry can reach the configured tribulation encounter.
	out, err := startEncounterCore(execution, c.EncounterKey)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	for i, stack := range stacks {
		stack.Quantity -= c.Materials[i].Quantity
		if err := execution.Store.SaveItemStack(stack); err != nil {
			return storage.CampaignActionOutcome{}, err
		}
	}
	out.Events = append(out.Events, storage.CampaignEventDraft{EventType: "cultivation.ascension_started", Visibility: storage.CampaignVisibilityPlayer, Payload: storage.JSONMap{"encounter_id": out.Result["encounter_id"], "materials": c.Materials}})
	return out, nil
}

// The common world quest handler remains topic-neutral. The cultivation
// registration composes its own gate before any quest consumes or rewards.
func submitCultivationQuest(ctx context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	key, err := worldactions.RequiredKey(arguments, "quest_key")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	_, quest, err := worldactions.QuestDefinition(execution, key)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if quest.CampaignOutcomeKey != "" {
		if err := requireFinalProgress(execution); err != nil {
			return storage.CampaignActionOutcome{}, err
		}
		if execution.ActorType != "player" {
			return storage.CampaignActionOutcome{}, worldactions.Rejected("最终飞升道路需要玩家亲自选择")
		}
		c, err := ascensionConfig(execution)
		if err != nil {
			return storage.CampaignActionOutcome{}, err
		}
		if c == nil || !slices.Contains(c.OutcomeKeys, quest.CampaignOutcomeKey) {
			return storage.CampaignActionOutcome{}, worldactions.Invalid("终局缺少可信飞升规则")
		}
		if err := checkAscensionReady(execution, *c); err != nil {
			return storage.CampaignActionOutcome{}, err
		}
		wins, err := execution.Store.EncounterWinCount(execution.Campaign.ID, c.EncounterKey)
		if err != nil {
			return storage.CampaignActionOutcome{}, err
		}
		if wins < 1 {
			return storage.CampaignActionOutcome{}, worldactions.Rejected("尚未真正渡过本局最终天劫")
		}
	}
	return worldactions.SubmitQuest(ctx, execution, arguments)
}
