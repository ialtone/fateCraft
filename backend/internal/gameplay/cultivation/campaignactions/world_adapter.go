package campaignactions

import (
	"context"
	"strings"
	"time"

	"github.com/ialtone/fatecraft/internal/campaign"
	"github.com/ialtone/fatecraft/internal/gameplay/components/worldactions"
	"github.com/ialtone/fatecraft/internal/storage"
)

func travel(ctx context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	outcome, err := worldactions.Travel(ctx, execution, arguments, missingEntryPermissions)
	return advanceWorldActionAge(execution, outcome, err)
}

func investigate(ctx context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	prepared, err := worldRollExecution(execution)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	outcome, err := worldactions.Investigate(ctx, prepared, arguments)
	return advanceWorldActionAge(execution, outcome, err)
}

func resolveUncertainAction(ctx context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	prepared, err := worldRollExecution(execution)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	return worldactions.ResolveUncertainAction(ctx, prepared, arguments)
}

func advanceWorldActionAge(execution campaign.ActionExecution, outcome storage.CampaignActionOutcome, actionErr error) (storage.CampaignActionOutcome, error) {
	if actionErr != nil {
		return storage.CampaignActionOutcome{}, actionErr
	}
	character, err := cultivationStore(execution).Character(execution.Campaign.ID)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	exhausted := advanceCharacterAge(character, execution.Campaign.GameTime, outcome.GameTimeDelta)
	character.UpdatedAt = time.Now()
	if err := cultivationStore(execution).SaveCharacter(character); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	appendLifespanResolution(&outcome, character, exhausted)
	return outcome, nil
}

func worldRollExecution(execution campaign.ActionExecution) (campaign.ActionExecution, error) {
	if execution.Roll == nil {
		return execution, nil
	}
	character, err := cultivationStore(execution).Character(execution.Campaign.ID)
	if err != nil {
		return execution, err
	}
	roll := execution.Roll
	execution.Roll = func(ctx context.Context, request campaign.CampaignRollRequest) (*campaign.CampaignRollResult, error) {
		attribute, weight, insight := "悟性", 0.7, int64(50)
		if strings.HasPrefix(request.Key, "uncertain.") {
			insight = campaignCharacterInsight(character)
			switch strings.TrimPrefix(request.Key, "uncertain.") {
			case "social":
				attribute = "魅力"
			case "stealth":
				attribute, weight = "根骨", 0.6
			case "exploration":
				weight = 0.6
			case "escape":
				attribute, weight = "根骨", 0.5
			}
		}
		request.RelevantAttributes = []string{attribute, "气运"}
		request.AttributeWeights = map[string]float64{attribute: weight, "气运": 1 - weight}
		request.PlayerState = campaignRollPlayerState(character, insight)
		request.Context = campaignRollContext(character)
		return roll(ctx, request)
	}
	return execution, nil
}

func instantiateNPC(ctx context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	return worldactions.InstantiateNPC(ctx, execution, arguments, func(definition storage.GameplayDefinition, npc *storage.CampaignNPC) {
		npc.RealmKey, _ = definition.Content["realm_key"].(string)
		npc.StageKey, _ = definition.Content["stage_key"].(string)
		if npc.StageKey == "" {
			npc.StageKey = "initial"
		}
	})
}
