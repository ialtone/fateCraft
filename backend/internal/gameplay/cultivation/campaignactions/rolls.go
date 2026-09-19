package campaignactions

import (
	"context"
	"fmt"

	"github.com/ialtone/fatecraft/internal/campaign"
	"github.com/ialtone/fatecraft/internal/storage"
)

func resolveCampaignRoll(ctx context.Context, execution campaign.ActionExecution, request campaign.CampaignRollRequest) (*campaign.CampaignRollResult, error) {
	if execution.Roll == nil {
		return nil, nil
	}
	return execution.Roll(ctx, request)
}

func campaignRollPlayerState(character *storage.CampaignCharacter, insight int64) map[string]any {
	if insight < 0 {
		insight = 0
	}
	if insight > 100 {
		insight = 100
	}
	foundation := int64(50)
	if character != nil {
		foundation = clampInt64(int64(character.FoundationQuality), 0, 100)
	}
	attributes := map[string]any{
		"根骨": foundation,
		"悟性": insight,
		"慧根": (foundation + insight) / 2,
		"气运": int64(50),
		"魅力": int64(50),
	}
	if character == nil {
		return map[string]any{"attributes": attributes, "traits": []any{}}
	}
	traits := make([]any, 0, len(character.Injuries)+len(character.LongTermEffects))
	for key, value := range character.Injuries {
		traits = append(traits, fmt.Sprintf("%s %v", key, value))
	}
	for key, value := range character.LongTermEffects {
		traits = append(traits, fmt.Sprintf("%s %v", key, value))
	}
	return map[string]any{"attributes": attributes, "traits": traits}
}

func campaignRollContext(character *storage.CampaignCharacter) *campaign.CampaignRollContext {
	result := &campaign.CampaignRollContext{}
	if character == nil {
		return result
	}
	if len(character.Injuries) > 0 {
		result.Disadvantages = append(result.Disadvantages, "受伤状态")
	}
	if character.MaxHealth > 0 && character.Health*2 < character.MaxHealth {
		result.Disadvantages = append(result.Disadvantages, "体力不支")
	}
	if character.MaxMana > 0 && character.Mana*5 < character.MaxMana {
		result.Disadvantages = append(result.Disadvantages, "疲惫不堪")
	}
	return result
}

func campaignCharacterInsight(character *storage.CampaignCharacter) int64 {
	if character == nil || character.MaxSpiritualSense <= 0 {
		return 50
	}
	return clampInt64(character.SpiritualSense*100/character.MaxSpiritualSense, 0, 100)
}

func campaignRollBaseFromBPS(bps int64) int {
	return int(clampInt64((bps+50)/100, 1, 99))
}

func attachCampaignRoll(outcome *storage.CampaignActionOutcome, result *campaign.CampaignRollResult) {
	if outcome == nil || result == nil {
		return
	}
	outcome.Roll = result.Snapshot
	if outcome.Result == nil {
		outcome.Result = storage.JSONMap{}
	}
	outcome.Result["roll"] = result.Rolled
	outcome.Result["chance"] = result.SuccessRate
	outcome.Result["roll_level"] = result.Level
}
