package campaignactions

import (
	"context"
	"errors"
	"github.com/ialtone/fatecraft/internal/gameplay/components/worldactions"
	"time"

	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/campaign"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/effectrules"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/encounterrules"
	"github.com/ialtone/fatecraft/internal/storage"
)

func startEncounter(_ context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	if err := worldactions.AllowArguments(arguments, "encounter_key"); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	key, err := worldactions.RequiredKey(arguments, "encounter_key")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	c, err := ascensionConfig(execution)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if c != nil && key == c.EncounterKey {
		return storage.CampaignActionOutcome{}, worldactions.Rejected("最终天劫必须通过玩家引劫行动开始")
	}
	return startEncounterCore(execution, key)
}

func startEncounterCore(execution campaign.ActionExecution, key string) (storage.CampaignActionOutcome, error) {
	if active, activeErr := cultivationStore(execution).ActiveEncounter(execution.Campaign.ID); activeErr == nil && active != nil {
		return storage.CampaignActionOutcome{}, worldactions.Rejected("已有活动遭遇 %s", active.EncounterKey)
	} else if activeErr != nil && !errors.Is(activeErr, storage.ErrNotFound) {
		return storage.CampaignActionOutcome{}, activeErr
	}
	definition, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionEncounter, key)
	if err != nil {
		return storage.CampaignActionOutcome{}, worldactions.Rejected("未知遭遇 %s", key)
	}
	rules, ok := encounterrules.Parse(map[string]any(definition.Content))
	if !ok {
		return storage.CampaignActionOutcome{}, worldactions.Invalid("遭遇定义无效: %s", key)
	}
	if rules.LocationKey != execution.Campaign.CurrentLocationKey {
		return storage.CampaignActionOutcome{}, worldactions.Rejected("遭遇 %s 不在当前位置", key)
	}
	character, err := cultivationStore(execution).Character(execution.Campaign.ID)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if character.Health <= 0 {
		return storage.CampaignActionOutcome{}, worldactions.Rejected("角色已无法开始遭遇")
	}
	now := time.Now()
	row := &storage.CampaignEncounter{
		ID: uuid.New(), CampaignID: execution.Campaign.ID, EncounterKey: key,
		LocationKey: rules.LocationKey, OpponentKey: rules.OpponentKey, OpponentName: rules.OpponentName,
		OpponentRealmRank: int(rules.OpponentRealmRank), OpponentHealth: rules.OpponentMaxHealth,
		OpponentMaxHealth: rules.OpponentMaxHealth, OpponentAttack: rules.OpponentAttack,
		OpponentDefense: rules.OpponentDefense, PlayerDamageBase: rules.PlayerDamageBase,
		PlayerDamageScaleBPS: rules.PlayerDamageScaleBPS, Round: 1, MaxRounds: int(rules.MaxEncounterRounds),
		Status: "active", StartedAtGameTime: execution.Campaign.GameTime, CreatedAt: now, UpdatedAt: now,
	}
	if err := cultivationStore(execution).CreateEncounter(row); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	payload := storage.JSONMap{
		"encounter_id": row.ID.String(), "encounter_key": key, "opponent_key": row.OpponentKey,
		"opponent_name": row.OpponentName, "opponent_max_health": row.OpponentMaxHealth,
		"location_key": row.LocationKey,
	}
	return storage.CampaignActionOutcome{
		Events: []storage.CampaignEventDraft{{EventType: "encounter.started", Visibility: storage.CampaignVisibilityPlayer, Payload: payload}},
		Result: payload,
	}, nil
}

func actEncounter(ctx context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	if err := worldactions.AllowArguments(arguments, "encounter_id", "move", "technique_key"); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	id, err := worldactions.RequiredUUID(arguments, "encounter_id")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	move, err := requiredMove(arguments)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	techniqueKey, err := worldactions.OptionalKey(arguments, "technique_key", "")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	return resolveEncounterTurn(ctx, execution, id, move, techniqueKey)
}

func fleeEncounter(ctx context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	if err := worldactions.AllowArguments(arguments, "encounter_id"); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	id, err := worldactions.RequiredUUID(arguments, "encounter_id")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	return resolveEncounterTurn(ctx, execution, id, "flee", "")
}

func requiredMove(arguments map[string]any) (string, error) {
	move, ok := arguments["move"].(string)
	if !ok || (move != "attack" && move != "defend") {
		return "", worldactions.Invalid("move 必须是 attack 或 defend")
	}
	return move, nil
}

func resolveEncounterTurn(ctx context.Context, execution campaign.ActionExecution, id uuid.UUID, move, techniqueKey string) (storage.CampaignActionOutcome, error) {
	encounter, err := cultivationStore(execution).Encounter(execution.Campaign.ID, id)
	if err != nil {
		return storage.CampaignActionOutcome{}, worldactions.Rejected("遭遇不存在")
	}
	if encounter.Status != "active" {
		return storage.CampaignActionOutcome{}, worldactions.Rejected("遭遇已经结束: %s", encounter.Status)
	}
	if encounter.LocationKey != execution.Campaign.CurrentLocationKey {
		return storage.CampaignActionOutcome{}, worldactions.Rejected("角色已不在遭遇地点")
	}
	character, err := cultivationStore(execution).Character(execution.Campaign.ID)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if character.Health <= 0 {
		return storage.CampaignActionOutcome{}, worldactions.Rejected("角色已无法继续遭遇")
	}
	definition, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionEncounter, encounter.EncounterKey)
	if err != nil {
		return storage.CampaignActionOutcome{}, worldactions.Invalid("遭遇定义不存在: %s", encounter.EncounterKey)
	}
	rules, ok := encounterrules.Parse(map[string]any(definition.Content))
	if !ok {
		return storage.CampaignActionOutcome{}, worldactions.Invalid("遭遇定义无效: %s", encounter.EncounterKey)
	}
	if move == "flee" {
		return resolveFleeTurn(ctx, execution, encounter, character, rules)
	}
	outcome := storage.CampaignActionOutcome{Result: storage.JSONMap{
		"encounter_id": encounter.ID.String(), "encounter_key": encounter.EncounterKey, "move": move,
	}}
	outcome.GameTimeDelta = rules.RoundMinutes
	if move == "attack" {
		damage, manaCost, damageErr := encounterPlayerDamage(execution, character, techniqueKey, encounter, rules.PlayerManaCost)
		if damageErr != nil {
			return storage.CampaignActionOutcome{}, damageErr
		}
		if character.Mana < manaCost {
			return storage.CampaignActionOutcome{}, worldactions.Rejected("法力不足，需要 %d", manaCost)
		}
		rollContext := campaignRollContext(character)
		rollContext.Opponent = &campaign.CampaignRollOpponent{RealmGap: encounter.OpponentRealmRank - character.RealmRank}
		rollResult, rollErr := worldactions.ResolveCampaignRoll(ctx, execution, campaign.CampaignRollRequest{
			Key: "encounter.attack." + encounter.EncounterKey, Title: "遭遇攻击",
			Description: "攻击" + encounter.OpponentName, Category: "combat", BaseSuccessRate: 65, Sides: 100,
			RelevantAttributes: []string{"根骨", "气运"}, AttributeWeights: map[string]float64{"根骨": 0.7, "气运": 0.3},
			PlayerState: campaignRollPlayerState(character, 50), Context: rollContext,
		})
		if rollErr != nil {
			return storage.CampaignActionOutcome{}, rollErr
		}
		character.Mana -= manaCost
		if rollResult != nil {
			switch rollResult.Level {
			case "critical_success":
				damage = maxInt64(1, damage*3/2)
			case "failure", "critical_failure":
				damage = 0
			}
			worldactions.AttachCampaignRoll(&outcome, rollResult)
		}
		encounter.OpponentHealth = maxInt64(0, encounter.OpponentHealth-damage)
		outcome.Result["player_damage"] = damage
		outcome.Events = append(outcome.Events, storage.CampaignEventDraft{
			EventType: "encounter.player_attacked", Visibility: storage.CampaignVisibilityPlayer,
			Payload: storage.JSONMap{"encounter_id": encounter.ID.String(), "damage": damage, "opponent_health": encounter.OpponentHealth, "technique_key": techniqueKey},
		})
	} else if techniqueKey != "" {
		return storage.CampaignActionOutcome{}, worldactions.Invalid("defend 行动不能指定 technique_key")
	}
	exhausted := advanceCharacterAge(character, execution.Campaign.GameTime, outcome.GameTimeDelta)
	if encounter.OpponentHealth == 0 {
		appendLifespanResolution(&outcome, character, exhausted)
		if exhausted {
			return finishEncounter(execution, encounter, character, "lost", &outcome, nil)
		}
		return finishEncounter(execution, encounter, character, "won", &outcome, &rules)
	}
	defense, err := encounterPlayerDefense(execution, character)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	opponentDamage := maxInt64(1, encounter.OpponentAttack-defense)
	if move == "defend" {
		opponentDamage = maxInt64(1, opponentDamage/2)
	}
	if outcome.Result["roll_level"] == "critical_failure" {
		opponentDamage = maxInt64(1, opponentDamage*3/2)
	}
	character.Health = maxInt64(0, character.Health-opponentDamage)
	outcome.Result["opponent_damage"] = opponentDamage
	outcome.Result["health"] = character.Health
	outcome.Events = append(outcome.Events, storage.CampaignEventDraft{
		EventType: "encounter.opponent_attacked", Visibility: storage.CampaignVisibilityPlayer,
		Payload: storage.JSONMap{"encounter_id": encounter.ID.String(), "damage": opponentDamage, "health": character.Health, "defense": defense},
	})
	appendLifespanResolution(&outcome, character, exhausted)
	if character.Health == 0 {
		return finishEncounter(execution, encounter, character, "lost", &outcome, &rules)
	}
	if exhausted {
		return finishEncounter(execution, encounter, character, "lost", &outcome, &rules)
	}
	if encounter.Round >= encounter.MaxRounds {
		return finishEncounter(execution, encounter, character, "lost", &outcome, &rules)
	}
	encounter.Round++
	encounter.LastMove = move
	encounter.UpdatedAt = time.Now()
	character.UpdatedAt = time.Now()
	if err := cultivationStore(execution).SaveCharacter(character); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if err := cultivationStore(execution).SaveEncounter(encounter); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	outcome.Events = append(outcome.Events, storage.CampaignEventDraft{
		EventType: "encounter.round_resolved", Visibility: storage.CampaignVisibilityPlayer,
		Payload: storage.JSONMap{"encounter_id": encounter.ID.String(), "round": encounter.Round, "status": encounter.Status},
	})
	return outcome, nil
}

func resolveFleeTurn(ctx context.Context, execution campaign.ActionExecution, encounter *storage.CampaignEncounter, character *storage.CampaignCharacter, rules encounterrules.Encounter) (storage.CampaignActionOutcome, error) {
	movement, err := encounterPlayerMovement(execution, character)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	chance := rules.FleeChanceBPS + movement*100
	if chance > 10_000 {
		chance = 10_000
	}
	outcome := storage.CampaignActionOutcome{Result: storage.JSONMap{
		"encounter_id": encounter.ID.String(), "encounter_key": encounter.EncounterKey, "move": "flee", "flee_chance_bps": chance,
	}}
	outcome.GameTimeDelta = rules.RoundMinutes
	exhausted := advanceCharacterAge(character, execution.Campaign.GameTime, outcome.GameTimeDelta)
	rollContext := campaignRollContext(character)
	rollContext.Opponent = &campaign.CampaignRollOpponent{RealmGap: encounter.OpponentRealmRank - character.RealmRank}
	rollResult, err := worldactions.ResolveCampaignRoll(ctx, execution, campaign.CampaignRollRequest{
		Key: "encounter.flee." + encounter.EncounterKey, Title: "脱离遭遇", Description: "尝试摆脱" + encounter.OpponentName,
		Category: "combat", BaseSuccessRate: campaignRollBaseFromBPS(chance), Sides: 100, RelevantAttributes: []string{"气运"},
		AttributeWeights: map[string]float64{"气运": 1}, PlayerState: campaignRollPlayerState(character, 50), Context: rollContext,
	})
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	success := int64(worldactions.DeterministicUint64(execution, "encounter.flee")%10_000) < chance
	if rollResult != nil {
		success = rollResult.Success
		chance = int64(rollResult.SuccessRate) * 100
		outcome.Result["flee_chance_bps"] = chance
		worldactions.AttachCampaignRoll(&outcome, rollResult)
	}
	if success {
		if exhausted {
			appendLifespanResolution(&outcome, character, true)
			return finishEncounter(execution, encounter, character, "lost", &outcome, nil)
		}
		return finishEncounter(execution, encounter, character, "fled", &outcome, nil)
	}
	defense, err := encounterPlayerDefense(execution, character)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	damage := maxInt64(1, encounter.OpponentAttack-defense)
	if rollResult != nil && rollResult.Level == "critical_failure" {
		damage = maxInt64(1, damage*3/2)
	}
	character.Health = maxInt64(0, character.Health-damage)
	outcome.Result["flee_failed"] = true
	outcome.Result["opponent_damage"] = damage
	outcome.Result["health"] = character.Health
	outcome.Events = append(outcome.Events, storage.CampaignEventDraft{
		EventType: "encounter.flee_failed", Visibility: storage.CampaignVisibilityPlayer,
		Payload: storage.JSONMap{"encounter_id": encounter.ID.String(), "damage": damage, "health": character.Health},
	})
	appendLifespanResolution(&outcome, character, exhausted)
	if character.Health == 0 {
		return finishEncounter(execution, encounter, character, "lost", &outcome, nil)
	}
	if exhausted {
		return finishEncounter(execution, encounter, character, "lost", &outcome, nil)
	}
	if encounter.Round >= encounter.MaxRounds {
		return finishEncounter(execution, encounter, character, "lost", &outcome, nil)
	}
	encounter.Round++
	encounter.LastMove = "flee"
	encounter.UpdatedAt = time.Now()
	character.UpdatedAt = time.Now()
	if err := cultivationStore(execution).SaveCharacter(character); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if err := cultivationStore(execution).SaveEncounter(encounter); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	outcome.Events = append(outcome.Events, storage.CampaignEventDraft{
		EventType: "encounter.round_resolved", Visibility: storage.CampaignVisibilityPlayer,
		Payload: storage.JSONMap{"encounter_id": encounter.ID.String(), "round": encounter.Round, "status": encounter.Status},
	})
	return outcome, nil
}

func finishEncounter(execution campaign.ActionExecution, encounter *storage.CampaignEncounter, character *storage.CampaignCharacter, status string, outcome *storage.CampaignActionOutcome, rules *encounterrules.Encounter) (storage.CampaignActionOutcome, error) {
	resolvedAt := execution.Campaign.GameTime + outcome.GameTimeDelta
	encounter.Status = status
	encounter.ResolvedAtGameTime = &resolvedAt
	encounter.LastMove = status
	encounter.UpdatedAt = time.Now()
	if err := cultivationStore(execution).SaveEncounter(encounter); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	character.UpdatedAt = time.Now()
	if err := cultivationStore(execution).SaveCharacter(character); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if status == "won" && rules != nil {
		if err := grantEncounterRewards(execution, rules.Rewards, outcome); err != nil {
			return storage.CampaignActionOutcome{}, err
		}
	}
	statusPayload := storage.JSONMap{"encounter_id": encounter.ID.String(), "encounter_key": encounter.EncounterKey, "status": status, "opponent_health": encounter.OpponentHealth, "health": character.Health}
	outcome.Result["status"] = status
	outcome.Events = append(outcome.Events, storage.CampaignEventDraft{
		EventType: "encounter." + status, Visibility: storage.CampaignVisibilityPlayer, Payload: statusPayload,
	})
	if status == "lost" && character.Health == 0 {
		campaignStatus := storage.CampaignStatusDead
		outcome.Status = &campaignStatus
		if outcome.OutcomeKey == nil {
			key := "perished"
			outcome.OutcomeKey = &key
			outcome.Result["outcome_key"] = key
		}
		outcome.Result["campaign_status"] = campaignStatus
		outcome.Events = append(outcome.Events, storage.CampaignEventDraft{
			EventType: "character.defeated", Visibility: storage.CampaignVisibilityPlayer,
			Payload: storage.JSONMap{"encounter_id": encounter.ID.String(), "cause": "encounter", "outcome_key": *outcome.OutcomeKey},
		})
	}
	return *outcome, nil
}

func grantEncounterRewards(execution campaign.ActionExecution, rewards []encounterrules.Reward, outcome *storage.CampaignActionOutcome) error {
	for _, reward := range rewards {
		definition, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionItem, reward.ItemKey)
		if err != nil {
			return worldactions.Invalid("遭遇奖励物品不存在: %s", reward.ItemKey)
		}
		stackable, _ := definition.Content["stackable"].(bool)
		if !stackable {
			return worldactions.Invalid("遭遇奖励必须是可堆叠物品: %s", reward.ItemKey)
		}
		quality := reward.QualityKey
		stack, err := execution.Store.ItemStack(execution.Campaign.ID, reward.ItemKey, quality)
		if errors.Is(err, storage.ErrNotFound) {
			stack = &storage.CampaignItemStack{CampaignID: execution.Campaign.ID, ItemKey: reward.ItemKey, QualityKey: quality, Metadata: storage.JSONMap{"source": "encounter"}}
		} else if err != nil {
			return err
		}
		stack.Quantity += reward.Quantity
		stack.UpdatedAt = time.Now()
		if err := execution.Store.SaveItemStack(stack); err != nil {
			return err
		}
		outcome.Events = append(outcome.Events, storage.CampaignEventDraft{
			EventType: "inventory.gained", Visibility: storage.CampaignVisibilityPlayer,
			Payload: storage.JSONMap{"item_key": reward.ItemKey, "quality_key": quality, "quantity": reward.Quantity, "source": "encounter"},
		})
	}
	return nil
}

func encounterPlayerDamage(execution campaign.ActionExecution, character *storage.CampaignCharacter, techniqueKey string, encounter *storage.CampaignEncounter, baseManaCost int64) (int64, int64, error) {
	attack, err := encounterCharacterAttack(execution, character)
	if err != nil {
		return 0, 0, err
	}
	manaCost := baseManaCost
	if techniqueKey != "" {
		technique, err := cultivationStore(execution).Technique(execution.Campaign.ID, techniqueKey)
		if err != nil || technique.Status != "active" || technique.EquippedSlot == nil {
			return 0, 0, worldactions.Rejected("功法未处于已装配状态: %s", techniqueKey)
		}
		definition, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionTechnique, techniqueKey)
		if err != nil {
			return 0, 0, worldactions.Invalid("功法定义不存在: %s", techniqueKey)
		}
		effects, ok := effectrules.MasteredEffects(definition.Content, technique.Mastery, technique.LearnedLayer)
		if !ok {
			return 0, 0, worldactions.Invalid("功法效果无效: %s", techniqueKey)
		}
		attack += maxInt64(0, effects["physical_attack"]) + maxInt64(0, effects["spiritual_attack"])
		manaCost += worldactions.DefinitionIntDefault(definition.Content["combat_mana_cost"], worldactions.DefinitionIntDefault(definition.Content["mana_cost"], 0))
	}
	damage := encounter.PlayerDamageBase + attack*encounter.PlayerDamageScaleBPS/10_000 - encounter.OpponentDefense
	if damage < 1 {
		damage = 1
	}
	variation := int64(worldactions.DeterministicUint64(execution, "encounter.damage")%41) + 80
	damage = damage * variation / 100
	if damage < 1 {
		damage = 1
	}
	effects, err := equippedEffects(execution)
	if err != nil {
		return 0, 0, err
	}
	if manaCost > 0 {
		manaCost = max(int64(1), manaCost-effects["mana_cost_reduction"])
	}
	return damage, manaCost, nil
}

func encounterCharacterAttack(execution campaign.ActionExecution, character *storage.CampaignCharacter) (int64, error) {
	attack := int64(character.RealmRank) * 8
	items, err := execution.Store.ItemInstances(execution.Campaign.ID)
	if err != nil {
		return 0, err
	}
	for _, item := range items {
		if item.State != "equipped" || item.EquippedSlot == nil || item.Durability <= 0 {
			continue
		}
		definition, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionEquipment, item.DefinitionKey)
		if err != nil {
			return 0, err
		}
		bonuses, ok := effectrules.EquipmentBonuses(definition.Content)
		if !ok {
			return 0, worldactions.Invalid("装备效果无效: %s", item.DefinitionKey)
		}
		attack += maxInt64(0, bonuses["physical_attack"]) + maxInt64(0, bonuses["spiritual_attack"])
	}
	return attack, nil
}

func encounterPlayerDefense(execution campaign.ActionExecution, character *storage.CampaignCharacter) (int64, error) {
	effects, err := equippedEffects(execution)
	if err != nil {
		return 0, err
	}
	return max(int64(0), int64(character.FoundationQuality)/10+effects["physical_defense"]+effects["spiritual_defense"]), nil
}

func encounterPlayerMovement(execution campaign.ActionExecution, character *storage.CampaignCharacter) (int64, error) {
	effects, err := equippedEffects(execution)
	if err != nil {
		return 0, err
	}
	return max(int64(0), effects["movement"]), nil
}
