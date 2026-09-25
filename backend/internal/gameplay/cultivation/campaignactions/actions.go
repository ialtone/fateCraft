package campaignactions

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/campaign"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/campaigncontract"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/effectrules"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/factionrules"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/gatheringrules"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/practicerules"
	"github.com/ialtone/fatecraft/internal/storage"
)

const (
	ActionPractice          = campaigncontract.ActionPractice
	ActionRenameCharacter   = campaigncontract.ActionRenameCharacter
	ActionBreakthrough      = campaigncontract.ActionBreakthrough
	ActionLearnTechnique    = campaigncontract.ActionLearnTechnique
	ActionEquipTechnique    = campaigncontract.ActionEquipTechnique
	ActionUnequipTechnique  = campaigncontract.ActionUnequipTechnique
	ActionApplyCondition    = campaigncontract.ActionApplyCondition
	ActionResolveCondition  = campaigncontract.ActionResolveCondition
	ActionAdjustVitals      = campaigncontract.ActionAdjustVitals
	ActionGainItem          = campaigncontract.ActionGainItem
	ActionUseItem           = campaigncontract.ActionUseItem
	ActionGainEquipment     = campaigncontract.ActionGainEquipment
	ActionEquipItem         = campaigncontract.ActionEquipItem
	ActionUnequipItem       = campaigncontract.ActionUnequipItem
	ActionLearnRecipe       = campaigncontract.ActionLearnRecipe
	ActionCraftRecipe       = campaigncontract.ActionCraftRecipe
	ActionTradeBuy          = campaigncontract.ActionTradeBuy
	ActionTradeSell         = campaigncontract.ActionTradeSell
	ActionAdjustMarket      = campaigncontract.ActionAdjustMarket
	ActionTravel            = campaigncontract.ActionTravel
	ActionInvestigate       = campaigncontract.ActionInvestigate
	ActionResolveUncertain  = campaigncontract.ActionResolveUncertain
	ActionGather            = campaigncontract.ActionGather
	ActionInstantiateNPC    = campaigncontract.ActionInstantiateNPC
	ActionMoveNPC           = campaigncontract.ActionMoveNPC
	ActionSetNPCStatus      = campaigncontract.ActionSetNPCStatus
	ActionRecordInteraction = campaigncontract.ActionRecordInteraction
	ActionAdjustFaction     = campaigncontract.ActionAdjustFaction
	ActionAcceptQuest       = campaigncontract.ActionAcceptQuest
	ActionSubmitQuest       = campaigncontract.ActionSubmitQuest
	ActionStartEncounter    = campaigncontract.ActionStartEncounter
	ActionEncounterAct      = campaigncontract.ActionEncounterAct
	ActionEncounterFlee     = campaigncontract.ActionEncounterFlee
)

func Register(service *campaign.Service) {
	type actionSpec struct {
		key, description string
		actors           []string
		handler          campaign.ActionHandlerFunc
	}
	specs := []actionSpec{
		{ActionRenameCharacter, "更新角色的长期公开姓名", []string{"player", "model"}, renameCharacter},
		{ActionPractice, "修炼已掌握且已装配的功法", []string{"player", "model"}, practice},
		{ActionBreakthrough, "尝试突破到定义中的下一境界", []string{"player", "model"}, attemptBreakthrough},
		{ActionLearnTechnique, "消耗传承物并学习功法", []string{"player", "model"}, learnTechnique},
		{ActionEquipTechnique, "将已掌握功法装配到能力槽", []string{"player", "model"}, equipTechnique},
		{ActionUnequipTechnique, "卸下已装配的功法", []string{"player", "model"}, unequipTechnique},
		{ActionApplyCondition, "施加受限伤势、中毒、心魔或长期效果", []string{"model", "system"}, applyCondition},
		{ActionResolveCondition, "移除已经存在的伤势或长期效果", []string{"model", "system"}, resolveCondition},
		{ActionAdjustVitals, "结算气血、法力与神识变化，气血归零时结束存档", []string{"model", "system"}, adjustVitals},
		{ActionGainItem, "授予定义目录中允许堆叠的物品", []string{"model", "system"}, gainItem},
		{ActionUseItem, "消耗堆叠物品并应用后端白名单效果", []string{"player", "model"}, useItem},
		{ActionGainEquipment, "授予定义目录中的唯一装备实例", []string{"model", "system"}, gainEquipment},
		{ActionEquipItem, "将唯一装备实例装配到合法槽位", []string{"player", "model"}, equipItem},
		{ActionUnequipItem, "卸下已装备的唯一物品", []string{"player", "model"}, unequipItem},
		{ActionLearnRecipe, "消耗配方载体并掌握版本化生产配方", []string{"player", "model"}, learnRecipe},
		{ActionCraftRecipe, "按已掌握配方执行可信制作与品质判定", []string{"player", "model"}, executeRecipe},
		{ActionTradeBuy, "从当前位置市场的有限商品池购买物品", []string{"player", "model"}, tradeBuy},
		{ActionTradeSell, "向当前位置市场出售物品或未装备实例", []string{"player", "model"}, tradeSell},
		{ActionAdjustMarket, "由系统调整市场动态价格系数", []string{"system"}, adjustMarketPrice},
		{ActionTravel, "沿已发现的相邻连接旅行", []string{"player", "model"}, travel},
		{ActionInvestigate, "调查当前地点并发现相邻地点", []string{"player", "model"}, investigate},
		{ActionResolveUncertain, "仅判定具有明确成败目标、真实不确定性和失败后果的交涉、洞察、潜行、探索或脱险；普通交谈、打听公开信息、倾听与观察已呈现事实不得调用；不直接改写其他领域事实", []string{"model"}, resolveUncertainAction},
		{ActionGather, "在当前地点的权威资源表中采集、采矿或狩猎", []string{"player", "model"}, gather},
		{ActionInstantiateNPC, "按版本原型和实例上限在当前地点创建 NPC", []string{"model", "system"}, instantiateNPC},
		{ActionMoveNPC, "沿地点连接移动 NPC 实例", []string{"model", "system"}, moveNPC},
		{ActionSetNPCStatus, "按状态机更新 NPC 生命周期状态", []string{"model", "system"}, setNPCStatus},
		{ActionRecordInteraction, "记录当前地点 NPC 的多维关系变化", []string{"model", "system"}, recordInteraction},
		{ActionAdjustFaction, "调整已定义势力的声望、贡献与通缉", []string{"model", "system"}, adjustFaction},
		{ActionAcceptQuest, "接受已提供且符合地点条件的任务或承诺", []string{"player", "model"}, acceptQuest},
		{ActionSubmitQuest, "提交任务并由后端核验目标、扣除物品和发放奖励", []string{"player", "model"}, submitQuest},
		{ActionStartEncounter, "在当前地点开始一个版本化遭遇", []string{"player", "model"}, startEncounter},
		{ActionEncounterAct, "在活动遭遇中执行一次攻击或防御行动", []string{"player", "model"}, actEncounter},
		{ActionEncounterFlee, "尝试从活动遭遇中撤离", []string{"player", "model"}, fleeEncounter},
	}
	for _, spec := range specs {
		registerAction(service, spec.key, spec.description, spec.actors, spec.handler)
	}
}

func renameCharacter(_ context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	if err := allowArguments(arguments, "name"); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	name, err := optionalText(arguments, "name", 128)
	if err != nil || strings.TrimSpace(name) == "" {
		return storage.CampaignActionOutcome{}, invalid("name 必须是 1 到 128 个字符的公开姓名")
	}
	character, err := execution.Store.Character(execution.Campaign.ID)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if character.Health <= 0 {
		return storage.CampaignActionOutcome{}, rejected("角色已无法更改姓名")
	}
	if character.Name == name {
		return storage.CampaignActionOutcome{}, rejected("角色姓名没有变化")
	}
	oldName := character.Name
	character.Name = name
	character.UpdatedAt = time.Now()
	if err := execution.Store.SaveCharacter(character); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	return singleEvent("character.renamed", storage.JSONMap{
		"old_name": oldName, "new_name": name,
	}), nil
}

func registerAction(
	service *campaign.Service,
	key, description string,
	actors []string,
	handler campaign.ActionHandlerFunc,
) {
	var writes []string
	var events []string
	if key == ActionRenameCharacter {
		writes = []string{"character.name"}
		events = []string{"character.renamed"}
	}
	wrapped := campaign.ActionHandlerFunc(func(ctx context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
		if err := rejectWhileEncounterActive(execution, key); err != nil {
			return storage.CampaignActionOutcome{}, err
		}
		outcome, err := handler(ctx, execution, arguments)
		if err != nil {
			return storage.CampaignActionOutcome{}, err
		}
		// Campaign 已终局（本次提交刚终结，或提交前已不是 active）时，
		// 不再处理状态到期与计划事件：completed 之后不应再追加任务失败等事件，
		// 角色死亡后也不应继续推进世界事件。
		if outcome.Status != nil || execution.Campaign.Status != storage.CampaignStatusActive {
			return outcome, nil
		}
		if err := expireDueConditions(execution, &outcome); err != nil && !errors.Is(err, storage.ErrNotFound) {
			return storage.CampaignActionOutcome{}, err
		}
		if err := processDueScheduledEvents(execution, &outcome); err != nil {
			return storage.CampaignActionOutcome{}, err
		}
		return outcome, nil
	})
	service.RegisterAction(campaign.RegisteredAction{
		Definition: campaign.ActionDefinition{
			Key: key, Description: description, Actors: actors, ArgumentSchema: actionArgumentSchema(key),
			Writes: writes, Events: events,
		},
		Capability: cultivationCapabilityProvider{},
		Handler:    wrapped,
	})
}

func rejectWhileEncounterActive(execution campaign.ActionExecution, actionKey string) error {
	active, err := execution.Store.ActiveEncounter(execution.Campaign.ID)
	if errors.Is(err, storage.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if active != nil && actionKey != ActionEncounterAct && actionKey != ActionEncounterFlee {
		return rejected("必须先解决活动遭遇 %s", active.EncounterKey)
	}
	return nil
}

func practice(_ context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	if err := allowArguments(arguments, "technique_key", "sessions"); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	techniqueKey, err := requiredKey(arguments, "technique_key")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	sessions, err := optionalInt(arguments, "sessions", 1, 1, practicerules.MaximumSessions)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	technique, err := execution.Store.Technique(execution.Campaign.ID, techniqueKey)
	if err != nil {
		return storage.CampaignActionOutcome{}, rejected("尚未掌握功法 %s", techniqueKey)
	}
	if technique.Status != "active" || technique.EquippedSlot == nil {
		return storage.CampaignActionOutcome{}, rejected("功法 %s 未处于已装配的可修炼状态", techniqueKey)
	}
	definition, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionTechnique, techniqueKey)
	if err != nil {
		return storage.CampaignActionOutcome{}, rejected("功法定义不存在: %s", techniqueKey)
	}
	character, err := execution.Store.Character(execution.Campaign.ID)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	rules := practicerules.Evaluate(practicerules.Facts{
		Character: character, Technique: *technique, Definition: *definition,
	}, &sessions)
	if !rules.Available {
		if rules.InvalidDefinition {
			return storage.CampaignActionOutcome{}, invalid("%s", rules.ViolationMessage)
		}
		return storage.CampaignActionOutcome{}, campaign.RejectAction(campaign.ActionViolation{
			Code: rules.ViolationCode, Path: "arguments.sessions", Message: rules.ViolationMessage, Public: true,
		})
	}
	minutes := rules.PracticeMinutesPerSession * sessions
	manaCost := rules.ManaCostPerSession * sessions
	gain := rules.CultivationGainPerSession * sessions
	masteryGain := rules.MasteryGainPerSession * sessions
	insightGain := rules.InsightGainPerSession * sessions
	character.Mana -= manaCost
	character.CultivationProgress = clampInt64(character.CultivationProgress+gain, 0, character.CultivationRequired*2)
	if realm, realmErr := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionRealm, character.RealmKey); realmErr == nil {
		character.StageKey = stageForProgress(realm.Content, character.CultivationProgress, character.CultivationRequired)
	}
	character.UpdatedAt = time.Now()
	technique.Mastery = clampInt64(technique.Mastery+masteryGain, 0, 10000)
	technique.Insight = maxInt64(0, technique.Insight+insightGain)
	maxLayer := definitionIntDefault(definition.Content["max_layer"], 1)
	masteryPerLayer := definitionIntDefault(definition.Content["mastery_per_layer"], 1000)
	if maxLayer <= 0 || masteryPerLayer <= 0 {
		return storage.CampaignActionOutcome{}, invalid("功法定义的层级规则无效")
	}
	technique.LearnedLayer = int(clampInt64(1+technique.Mastery/masteryPerLayer, 1, maxLayer))
	exhausted := advanceCharacterAge(character, execution.Campaign.GameTime, minutes)
	technique.UpdatedAt = character.UpdatedAt
	if err := execution.Store.SaveTechnique(technique); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	derivedEffects, err := recalculateCharacterStats(execution, character)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if err := execution.Store.SaveCharacter(character); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	outcome := storage.CampaignActionOutcome{
		GameTimeDelta: minutes,
		Events: []storage.CampaignEventDraft{{
			EventType: "cultivation.progressed", Visibility: storage.CampaignVisibilityPlayer,
			Payload: storage.JSONMap{
				"technique_key": techniqueKey, "sessions": sessions, "minutes": minutes,
				"cultivation_gain": gain, "cultivation_progress": character.CultivationProgress,
				"mastery": technique.Mastery, "learned_layer": technique.LearnedLayer, "mana": character.Mana,
			},
		}},
		Result: storage.JSONMap{
			"cultivation_progress": character.CultivationProgress, "stage_key": character.StageKey,
			"technique_mastery": technique.Mastery, "learned_layer": technique.LearnedLayer,
			"active_effects": derivedEffects, "game_time_delta": minutes,
		},
	}
	appendLifespanResolution(&outcome, character, exhausted)
	return outcome, nil
}

func attemptBreakthrough(ctx context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	if err := allowArguments(arguments); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	character, err := execution.Store.Character(execution.Campaign.ID)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if character.CultivationProgress < character.CultivationRequired {
		return storage.CampaignActionOutcome{}, rejected("修为尚未圆满：%d/%d", character.CultivationProgress, character.CultivationRequired)
	}
	current, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionRealm, character.RealmKey)
	if err != nil {
		return storage.CampaignActionOutcome{}, rejected("当前境界定义不存在")
	}
	currentRank, currentRankOK := exactActionInt(current.Content["rank"])
	if !currentRankOK || currentRank < 0 || int64(character.RealmRank) != currentRank {
		return storage.CampaignActionOutcome{}, rejected("角色境界缓存与当前玩法定义不一致")
	}
	nextKey, _ := current.Content["next_realm_key"].(string)
	if nextKey == "" {
		return storage.CampaignActionOutcome{}, rejected("当前境界没有可突破的下一境界")
	}
	next, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionRealm, nextKey)
	if err != nil {
		return storage.CampaignActionOutcome{}, rejected("下一境界定义不存在: %s", nextKey)
	}
	nextRank, nextRankOK := exactActionInt(next.Content["rank"])
	if !nextRankOK || nextRank < 0 || nextRank <= currentRank {
		return storage.CampaignActionOutcome{}, rejected("下一境界 %s 缺少有效且递增的 rank", nextKey)
	}
	baseChance := definitionIntDefault(current.Content["breakthrough_chance"], 65)
	legacyChance := clampInt64(baseChance+int64((character.FoundationQuality-50)/2), 5, 95)
	rollResult, err := resolveCampaignRoll(ctx, execution, campaign.CampaignRollRequest{
		Key: "cultivation.breakthrough." + character.RealmKey, Title: "境界突破",
		Description: fmt.Sprintf("从%s突破至%s", current.Name, next.Name), Category: "cultivation",
		BaseSuccessRate: int(baseChance), Sides: 100, RelevantAttributes: []string{"根骨"},
		AttributeWeights: map[string]float64{"根骨": 1}, PlayerState: campaignRollPlayerState(character, 50),
		Context: campaignRollContext(character),
	})
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	roll, chance, success := deterministicPercent(execution, "breakthrough:"+character.RealmKey), legacyChance, false
	if rollResult != nil {
		roll, chance, success = int64(rollResult.Rolled), int64(rollResult.SuccessRate), rollResult.Success
	} else {
		success = roll <= chance
	}
	minutes := definitionIntDefault(current.Content["breakthrough_minutes"], 24*60)
	now := time.Now()
	payload := storage.JSONMap{
		"from_realm_key": character.RealmKey, "to_realm_key": nextKey,
		"chance": chance, "roll": roll, "success": success,
	}
	if success {
		required := definitionIntDefault(next.Content["cultivation_required"], character.CultivationRequired*2)
		stages, _ := next.Content["stages"].([]any)
		stage := "initial"
		if len(stages) > 0 {
			stage, _ = stages[0].(string)
		}
		character.RealmKey = nextKey
		character.RealmRank = int(nextRank)
		character.StageKey = stage
		character.CultivationProgress = 0
		character.CultivationRequired = required
		character.LifespanDays = maxInt64(character.LifespanDays, definitionIntDefault(next.Content["lifespan_days"], character.LifespanDays))
		growCharacterBaseStats(character, next.Content)
		payload["realm_rank"] = nextRank
	} else {
		loss := character.CultivationRequired * definitionIntDefault(current.Content["failure_progress_loss_percent"], 20) / 100
		damage := definitionIntDefault(current.Content["failure_health_damage"], maxInt64(1, character.MaxHealth/10))
		character.CultivationProgress = maxInt64(0, character.CultivationProgress-loss)
		character.Health = maxInt64(1, character.Health-damage)
		if character.Injuries == nil {
			character.Injuries = storage.JSONMap{}
		}
		character.Injuries["breakthrough_backlash"] = map[string]any{"severity": damage, "at_game_time": execution.Campaign.GameTime + minutes}
		payload["progress_loss"] = loss
		payload["health_damage"] = damage
	}
	character.UpdatedAt = now
	exhausted := advanceCharacterAge(character, execution.Campaign.GameTime, minutes)
	if success {
		if _, err := recalculateCharacterStats(execution, character); err != nil {
			return storage.CampaignActionOutcome{}, err
		}
		character.Health = character.MaxHealth
		character.Mana = character.MaxMana
		character.SpiritualSense = character.MaxSpiritualSense
	}
	if err := execution.Store.SaveCharacter(character); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	outcome := storage.CampaignActionOutcome{
		GameTimeDelta: minutes,
		Events: []storage.CampaignEventDraft{{
			EventType: "realm.breakthrough_resolved", Visibility: storage.CampaignVisibilityPlayer, Payload: payload,
		}},
		Result: storage.JSONMap{
			"success": success, "roll": roll, "chance": chance, "realm_key": character.RealmKey,
			"stage_key": character.StageKey, "cultivation_progress": character.CultivationProgress,
			"game_time_delta": minutes,
		},
	}
	attachCampaignRoll(&outcome, rollResult)
	appendLifespanResolution(&outcome, character, exhausted)
	return outcome, nil
}

func learnTechnique(_ context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	if err := allowArguments(arguments, "technique_key"); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	key, err := requiredKey(arguments, "technique_key")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if _, err := execution.Store.Technique(execution.Campaign.ID, key); err == nil {
		return storage.CampaignActionOutcome{}, rejected("已经掌握功法 %s", key)
	} else if !errors.Is(err, storage.ErrNotFound) {
		return storage.CampaignActionOutcome{}, err
	}
	definition, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionTechnique, key)
	if err != nil {
		return storage.CampaignActionOutcome{}, rejected("未知功法 %s", key)
	}
	character, err := execution.Store.Character(execution.Campaign.ID)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	minimumRank := definitionIntDefault(definition.Content["minimum_realm_rank"], 0)
	if int64(character.RealmRank) < minimumRank {
		return storage.CampaignActionOutcome{}, rejected("境界不足，学习 %s 需要 rank %d", definition.Name, minimumRank)
	}
	var consumedItem string
	if sourceItem, _ := definition.Content["source_item_key"].(string); sourceItem != "" {
		stack, stackErr := execution.Store.ItemStack(execution.Campaign.ID, sourceItem, "common")
		if stackErr != nil || stack.Quantity < 1 {
			return storage.CampaignActionOutcome{}, rejected("缺少功法传承物品 %s", sourceItem)
		}
		stack.Quantity--
		stack.UpdatedAt = time.Now()
		if err := execution.Store.SaveItemStack(stack); err != nil {
			return storage.CampaignActionOutcome{}, err
		}
		consumedItem = sourceItem
	}
	category, _ := definition.Content["category"].(string)
	now := time.Now()
	row := &storage.CampaignTechnique{
		ID: uuid.New(), CampaignID: execution.Campaign.ID, TechniqueKey: key,
		Category: category, LearnedLayer: 1, Status: "active",
		SourceSnapshot: storage.JSONMap{"key": key, "name": definition.Name}, CreatedAt: now, UpdatedAt: now,
	}
	if err := execution.Store.CreateTechnique(row); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	return storage.CampaignActionOutcome{
		Events: []storage.CampaignEventDraft{{
			EventType: "technique.learned", Visibility: storage.CampaignVisibilityPlayer,
			Payload: storage.JSONMap{"technique_key": key, "name": definition.Name, "consumed_item_key": consumedItem},
		}},
		Result: storage.JSONMap{"technique_id": row.ID, "technique_key": key, "name": definition.Name},
	}, nil
}

func equipTechnique(_ context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	if err := allowArguments(arguments, "technique_key", "slot"); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	key, err := requiredKey(arguments, "technique_key")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	slot, err := requiredKey(arguments, "slot")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	row, err := execution.Store.Technique(execution.Campaign.ID, key)
	if err != nil || row.Status != "active" {
		return storage.CampaignActionOutcome{}, rejected("功法不可装配: %s", key)
	}
	definition, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionTechnique, key)
	if err != nil || !containsDefinitionString(definition.Content["allowed_slots"], slot) {
		return storage.CampaignActionOutcome{}, rejected("功法 %s 不能装配到 %s", key, slot)
	}
	if err := execution.Store.ClearTechniqueSlot(execution.Campaign.ID, slot, row.ID); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	row.EquippedSlot = &slot
	row.UpdatedAt = time.Now()
	if err := execution.Store.SaveTechnique(row); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	character, err := execution.Store.Character(execution.Campaign.ID)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	effects, err := recalculateCharacterStats(execution, character)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if err := execution.Store.SaveCharacter(character); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	return singleEvent("technique.equipped", storage.JSONMap{"technique_key": key, "slot": slot, "active_effects": effects}), nil
}

func unequipTechnique(_ context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	if err := allowArguments(arguments, "technique_key"); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	key, err := requiredKey(arguments, "technique_key")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	row, err := execution.Store.Technique(execution.Campaign.ID, key)
	if err != nil || row.EquippedSlot == nil {
		return storage.CampaignActionOutcome{}, rejected("功法当前未装配: %s", key)
	}
	slot := *row.EquippedSlot
	row.EquippedSlot = nil
	row.UpdatedAt = time.Now()
	if err := execution.Store.SaveTechnique(row); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	character, err := execution.Store.Character(execution.Campaign.ID)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	effects, err := recalculateCharacterStats(execution, character)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if err := execution.Store.SaveCharacter(character); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	return singleEvent("technique.unequipped", storage.JSONMap{"technique_key": key, "slot": slot, "active_effects": effects}), nil
}

func applyCondition(_ context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	if err := requireModelAuthority(execution); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if err := allowArguments(arguments, "category", "effect_key", "severity", "duration_minutes", "description"); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	category, err := conditionCategory(arguments)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	key, err := requiredKey(arguments, "effect_key")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	severity, err := requiredInt(arguments, "severity", 1, 100)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	duration, err := optionalInt(arguments, "duration_minutes", 0, 0, 5256000)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	description, err := optionalText(arguments, "description", 256)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	character, err := execution.Store.Character(execution.Campaign.ID)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	target := character.LongTermEffects
	if category == "injury" {
		target = character.Injuries
	}
	if target == nil {
		target = storage.JSONMap{}
		if category == "injury" {
			character.Injuries = target
		} else {
			character.LongTermEffects = target
		}
	}
	target[key] = map[string]any{
		"category": category, "severity": severity, "description": description,
		"applied_at_game_time": execution.Campaign.GameTime, "duration_minutes": duration,
	}
	character.UpdatedAt = time.Now()
	if err := execution.Store.SaveCharacter(character); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	return singleEvent("character.condition_applied", storage.JSONMap{
		"category": category, "effect_key": key, "severity": severity, "duration_minutes": duration,
	}), nil
}

func resolveCondition(_ context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	if err := requireModelAuthority(execution); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if err := allowArguments(arguments, "category", "effect_key"); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	category, err := conditionCategory(arguments)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	key, err := requiredKey(arguments, "effect_key")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	character, err := execution.Store.Character(execution.Campaign.ID)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	target := character.LongTermEffects
	if category == "injury" {
		target = character.Injuries
	}
	if _, exists := target[key]; !exists {
		return storage.CampaignActionOutcome{}, rejected("状态不存在: %s", key)
	}
	delete(target, key)
	character.UpdatedAt = time.Now()
	if err := execution.Store.SaveCharacter(character); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	return singleEvent("character.condition_resolved", storage.JSONMap{"category": category, "effect_key": key}), nil
}

func adjustVitals(_ context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	if err := requireModelAuthority(execution); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if err := allowArguments(arguments, "health_delta", "mana_delta", "spiritual_sense_delta", "cause_key"); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	cause, err := requiredKey(arguments, "cause_key")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	healthDelta, err := optionalInt(arguments, "health_delta", 0, -10000, 10000)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	manaDelta, err := optionalInt(arguments, "mana_delta", 0, -10000, 10000)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	senseDelta, err := optionalInt(arguments, "spiritual_sense_delta", 0, -10000, 10000)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if healthDelta == 0 && manaDelta == 0 && senseDelta == 0 {
		return storage.CampaignActionOutcome{}, invalid("至少需要一项非零生命资源变化")
	}
	character, err := execution.Store.Character(execution.Campaign.ID)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	character.Health = clampInt64(character.Health+healthDelta, 0, character.MaxHealth)
	character.Mana = clampInt64(character.Mana+manaDelta, 0, character.MaxMana)
	character.SpiritualSense = clampInt64(character.SpiritualSense+senseDelta, 0, character.MaxSpiritualSense)
	character.UpdatedAt = time.Now()
	if err := execution.Store.SaveCharacter(character); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	payload := storage.JSONMap{
		"cause_key": cause, "health": character.Health, "max_health": character.MaxHealth,
		"mana": character.Mana, "max_mana": character.MaxMana,
		"spiritual_sense": character.SpiritualSense, "max_spiritual_sense": character.MaxSpiritualSense,
	}
	outcome := singleEvent("character.vitals_changed", payload)
	if character.Health == 0 {
		status := storage.CampaignStatusDead
		outcome.Status = &status
		outcome.Events = append(outcome.Events, storage.CampaignEventDraft{
			EventType: "character.died", Visibility: storage.CampaignVisibilityPlayer,
			Payload: storage.JSONMap{"cause_key": cause, "game_time": execution.Campaign.GameTime},
		})
		outcome.Result["campaign_status"] = status
	}
	return outcome, nil
}

func gainItem(_ context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	if err := requireModelAuthority(execution); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if err := allowArguments(arguments, "item_key", "quality_key", "quantity"); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	key, err := requiredKey(arguments, "item_key")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	quality, err := optionalKey(arguments, "quality_key", "common")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	definition, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionItem, key)
	if err != nil {
		return storage.CampaignActionOutcome{}, rejected("未知物品 %s", key)
	}
	if err := requireDefinitionGrantAuthority(execution, definition); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if stackable, _ := definition.Content["stackable"].(bool); !stackable {
		return storage.CampaignActionOutcome{}, rejected("物品 %s 不是可堆叠物品", key)
	}
	maximum := definitionIntDefault(definition.Content["max_grant_per_action"], 100)
	quantity, err := requiredInt(arguments, "quantity", 1, maximum)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	stack, err := execution.Store.ItemStack(execution.Campaign.ID, key, quality)
	if errors.Is(err, storage.ErrNotFound) {
		stack = &storage.CampaignItemStack{
			CampaignID: execution.Campaign.ID, ItemKey: key, QualityKey: quality,
			Metadata: storage.JSONMap{"name": definition.Name},
		}
	} else if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if stack.Quantity > math.MaxInt64-quantity {
		return storage.CampaignActionOutcome{}, rejected("物品数量溢出")
	}
	stack.Quantity += quantity
	stack.UpdatedAt = time.Now()
	if err := execution.Store.SaveItemStack(stack); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	return singleEvent("inventory.gained", storage.JSONMap{
		"item_key": key, "name": definition.Name, "quality_key": quality,
		"quantity": quantity, "quantity_after": stack.Quantity,
	}), nil
}

func useItem(_ context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	if err := allowArguments(arguments, "item_key", "quality_key", "quantity"); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	key, err := requiredKey(arguments, "item_key")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	quality, err := optionalKey(arguments, "quality_key", "common")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	quantity, err := requiredInt(arguments, "quantity", 1, 20)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	definition, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionItem, key)
	if err != nil {
		return storage.CampaignActionOutcome{}, rejected("未知物品 %s", key)
	}
	consumable, _ := definition.Content["consumable"].(bool)
	if !consumable {
		return storage.CampaignActionOutcome{}, rejected("物品 %s 不可直接使用", key)
	}
	stack, err := execution.Store.ItemStack(execution.Campaign.ID, key, quality)
	if err != nil || stack.Quantity < quantity {
		return storage.CampaignActionOutcome{}, rejected("物品数量不足: %s", key)
	}
	character, err := execution.Store.Character(execution.Campaign.ID)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	effects, _ := definition.Content["effects"].(map[string]any)
	applied := storage.JSONMap{}
	for field, raw := range effects {
		amount, ok := exactActionInt(raw)
		if !ok {
			return storage.CampaignActionOutcome{}, invalid("物品 %s 的效果 %s 不是整数", key, field)
		}
		amount *= quantity
		switch field {
		case "health":
			character.Health = clampInt64(character.Health+amount, 0, character.MaxHealth)
			applied[field] = character.Health
		case "mana":
			character.Mana = clampInt64(character.Mana+amount, 0, character.MaxMana)
			applied[field] = character.Mana
		case "spiritual_sense":
			character.SpiritualSense = clampInt64(character.SpiritualSense+amount, 0, character.MaxSpiritualSense)
			applied[field] = character.SpiritualSense
		case "cultivation_progress":
			character.CultivationProgress = clampInt64(character.CultivationProgress+amount, 0, character.CultivationRequired*2)
			applied[field] = character.CultivationProgress
		default:
			return storage.CampaignActionOutcome{}, invalid("物品 %s 声明了不受信任的效果 %s", key, field)
		}
	}
	stack.Quantity -= quantity
	stack.UpdatedAt = time.Now()
	character.UpdatedAt = stack.UpdatedAt
	if err := execution.Store.SaveItemStack(stack); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if err := execution.Store.SaveCharacter(character); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	return singleEvent("inventory.consumed", storage.JSONMap{
		"item_key": key, "quality_key": quality, "quantity": quantity,
		"quantity_after": stack.Quantity, "applied": applied,
	}), nil
}

func gainEquipment(_ context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	if err := requireModelAuthority(execution); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if err := allowArguments(arguments, "definition_key", "quality_key", "source"); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	key, err := requiredKey(arguments, "definition_key")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	quality, err := optionalKey(arguments, "quality_key", "common")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	source, err := optionalKey(arguments, "source", "narrative.reward")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	definition, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionEquipment, key)
	if err != nil {
		return storage.CampaignActionOutcome{}, rejected("未知装备 %s", key)
	}
	if err := requireDefinitionGrantAuthority(execution, definition); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	durability := definitionIntDefault(definition.Content["durability_max"], 100)
	if durability < 1 || durability > math.MaxInt32 {
		return storage.CampaignActionOutcome{}, invalid("装备耐久定义无效")
	}
	now := time.Now()
	row := &storage.CampaignItemInstance{
		ID: uuid.New(), CampaignID: execution.Campaign.ID, DefinitionKey: key,
		QualityKey: quality, State: "stored", Durability: int(durability), MaxDurability: int(durability),
		Properties: storage.JSONMap{"name": definition.Name, "bonuses": definition.Content["bonuses"]},
		Provenance: storage.JSONMap{"source": source}, CreatedAt: now, UpdatedAt: now,
	}
	if err := execution.Store.CreateItemInstance(row); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	return singleEvent("inventory.instance_gained", storage.JSONMap{
		"item_instance_id": row.ID, "definition_key": key, "name": definition.Name, "quality_key": quality,
	}), nil
}

func equipItem(_ context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	if err := allowArguments(arguments, "item_instance_id", "slot"); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	id, err := requiredUUID(arguments, "item_instance_id")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	slot, err := requiredKey(arguments, "slot")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	item, err := execution.Store.ItemInstance(execution.Campaign.ID, id)
	if err != nil || (item.State != "stored" && item.State != "equipped") || item.Durability <= 0 {
		return storage.CampaignActionOutcome{}, rejected("装备实例不可装配")
	}
	definition, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionEquipment, item.DefinitionKey)
	if err != nil || !containsDefinitionString(definition.Content["allowed_slots"], slot) {
		return storage.CampaignActionOutcome{}, rejected("装备 %s 不能装配到 %s", item.DefinitionKey, slot)
	}
	character, err := execution.Store.Character(execution.Campaign.ID)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if err := execution.Store.UnequipItemSlot(execution.Campaign.ID, slot, item.ID); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	item.State = "equipped"
	item.EquippedSlot = &slot
	item.BoundCharacterID = &character.ID
	item.UpdatedAt = time.Now()
	if err := execution.Store.SaveItemInstance(item); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	effects, err := recalculateCharacterStats(execution, character)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if err := execution.Store.SaveCharacter(character); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	return singleEvent("equipment.changed", storage.JSONMap{
		"operation": "equip", "item_instance_id": item.ID, "definition_key": item.DefinitionKey,
		"slot": slot, "active_effects": effects,
	}), nil
}

func unequipItem(_ context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	if err := allowArguments(arguments, "item_instance_id"); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	id, err := requiredUUID(arguments, "item_instance_id")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	item, err := execution.Store.ItemInstance(execution.Campaign.ID, id)
	if err != nil || item.State != "equipped" || item.EquippedSlot == nil {
		return storage.CampaignActionOutcome{}, rejected("物品当前未装备")
	}
	slot := *item.EquippedSlot
	item.State = "stored"
	item.EquippedSlot = nil
	item.UpdatedAt = time.Now()
	if err := execution.Store.SaveItemInstance(item); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	character, err := execution.Store.Character(execution.Campaign.ID)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	effects, err := recalculateCharacterStats(execution, character)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if err := execution.Store.SaveCharacter(character); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	return singleEvent("equipment.changed", storage.JSONMap{
		"operation": "unequip", "item_instance_id": item.ID, "definition_key": item.DefinitionKey,
		"slot": slot, "active_effects": effects,
	}), nil
}

func travel(_ context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	if err := allowArguments(arguments, "destination_key"); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	destination, err := requiredKey(arguments, "destination_key")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if destination == execution.Campaign.CurrentLocationKey {
		return storage.CampaignActionOutcome{}, rejected("已经位于目标地点")
	}
	known, err := execution.Store.IsLocationDiscovered(execution.Campaign.ID, destination)
	if err != nil || !known {
		return storage.CampaignActionOutcome{}, rejected("目标地点尚未发现: %s", destination)
	}
	current, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionLocation, execution.Campaign.CurrentLocationKey)
	if err != nil {
		return storage.CampaignActionOutcome{}, rejected("当前位置定义不存在")
	}
	minutes, connected := connectionTravelMinutes(current.Content, destination)
	if !connected {
		return storage.CampaignActionOutcome{}, rejected("目标地点与当前位置不相邻")
	}
	destinationDefinition, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionLocation, destination)
	if err != nil {
		return storage.CampaignActionOutcome{}, rejected("目标地点定义不存在")
	}
	missing, err := missingEntryPermissions(execution, destinationDefinition.Content)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if len(missing) > 0 {
		return storage.CampaignActionOutcome{}, rejected("缺少进入权限: %s", strings.Join(missing, ", "))
	}
	now := time.Now()
	character, err := execution.Store.Character(execution.Campaign.ID)
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		return storage.CampaignActionOutcome{}, err
	}
	exhausted := false
	if character != nil {
		exhausted = advanceCharacterAge(character, execution.Campaign.GameTime, minutes)
		character.UpdatedAt = now
		if err := execution.Store.SaveCharacter(character); err != nil {
			return storage.CampaignActionOutcome{}, err
		}
	}
	if err := execution.Store.DiscoverLocation(&storage.CampaignLocationDiscovery{
		CampaignID: execution.Campaign.ID, LocationKey: destination,
		DiscoveredAtGameTime: execution.Campaign.GameTime, Visited: true,
		Knowledge: storage.JSONMap{}, UpdatedAt: now,
	}); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	outcome := storage.CampaignActionOutcome{
		GameTimeDelta: minutes, CurrentLocationKey: &destination,
		Events: []storage.CampaignEventDraft{
			{EventType: "travel.started", Visibility: storage.CampaignVisibilityModel, Payload: storage.JSONMap{
				"from_location_key": execution.Campaign.CurrentLocationKey, "to_location_key": destination, "minutes": minutes,
			}},
			{EventType: "travel.arrived", Visibility: storage.CampaignVisibilityPlayer, Payload: storage.JSONMap{
				"from_location_key": execution.Campaign.CurrentLocationKey, "location_key": destination, "minutes": minutes,
			}},
		},
		Result: storage.JSONMap{"location_key": destination, "game_time_delta": minutes},
	}
	if character != nil {
		appendLifespanResolution(&outcome, character, exhausted)
	}
	return outcome, nil
}

func investigate(ctx context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	if err := allowArguments(arguments); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	current, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionLocation, execution.Campaign.CurrentLocationKey)
	if err != nil {
		return storage.CampaignActionOutcome{}, rejected("当前位置定义不存在")
	}
	connections, _ := current.Content["connections"].([]any)
	unknown := make([]string, 0)
	for _, raw := range connections {
		connection, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		key, _ := connection["location_key"].(string)
		known, checkErr := execution.Store.IsLocationDiscovered(execution.Campaign.ID, key)
		if checkErr != nil {
			return storage.CampaignActionOutcome{}, checkErr
		}
		if !known {
			unknown = append(unknown, key)
		}
	}
	minutes := definitionIntDefault(current.Content["investigation_minutes"], 120)
	character, err := execution.Store.Character(execution.Campaign.ID)
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		return storage.CampaignActionOutcome{}, err
	}
	exhausted := false
	if character != nil {
		exhausted = advanceCharacterAge(character, execution.Campaign.GameTime, minutes)
		character.UpdatedAt = time.Now()
		if err := execution.Store.SaveCharacter(character); err != nil {
			return storage.CampaignActionOutcome{}, err
		}
	}
	payload := storage.JSONMap{"location_key": execution.Campaign.CurrentLocationKey, "minutes": minutes}
	result := storage.JSONMap{"discovered": false, "game_time_delta": minutes}
	var rollResult *campaign.CampaignRollResult
	if len(unknown) > 0 {
		danger := definitionIntDefault(current.Content["danger_rank"], 0)
		rollResult, err = resolveCampaignRoll(ctx, execution, campaign.CampaignRollRequest{
			Key: "world.investigate." + execution.Campaign.CurrentLocationKey, Title: "调查判定",
			Description: "调查" + current.Name, Category: "exploration", BaseSuccessRate: int(clampInt64(70-danger*4, 30, 80)), Sides: 100,
			RelevantAttributes: []string{"悟性", "气运"}, AttributeWeights: map[string]float64{"悟性": 0.7, "气运": 0.3},
			PlayerState: campaignRollPlayerState(character, 50), Context: campaignRollContext(character),
		})
		if err != nil {
			return storage.CampaignActionOutcome{}, err
		}
		success := true
		if rollResult != nil {
			success = rollResult.Success
		}
		if success {
			index := int(deterministicUint64(execution, "investigate:"+execution.Campaign.CurrentLocationKey) % uint64(len(unknown)))
			discovered := unknown[index]
			if err := execution.Store.DiscoverLocation(&storage.CampaignLocationDiscovery{
				CampaignID: execution.Campaign.ID, LocationKey: discovered,
				DiscoveredAtGameTime: execution.Campaign.GameTime + minutes, Visited: false,
				Knowledge: storage.JSONMap{"source": "investigation"}, UpdatedAt: time.Now(),
			}); err != nil {
				return storage.CampaignActionOutcome{}, err
			}
			payload["discovered_location_key"] = discovered
			result["discovered"] = true
			result["location_key"] = discovered
		}
	}
	outcome := storage.CampaignActionOutcome{
		GameTimeDelta: minutes,
		Events: []storage.CampaignEventDraft{{
			EventType: "world.investigated", Visibility: storage.CampaignVisibilityPlayer, Payload: payload,
		}}, Result: result,
	}
	attachCampaignRoll(&outcome, rollResult)
	if character != nil {
		appendLifespanResolution(&outcome, character, exhausted)
	}
	return outcome, nil
}

func resolveUncertainAction(ctx context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	if err := allowArguments(arguments, "kind", "description"); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	kind, ok := arguments["kind"].(string)
	rules := map[string]struct {
		title      string
		category   string
		base       int
		attributes []string
		weights    map[string]float64
	}{
		"social":      {"交涉判定", "social", 60, []string{"魅力", "气运"}, map[string]float64{"魅力": 0.7, "气运": 0.3}},
		"insight":     {"洞察判定", "exploration", 60, []string{"悟性", "气运"}, map[string]float64{"悟性": 0.7, "气运": 0.3}},
		"stealth":     {"潜行判定", "exploration", 55, []string{"根骨", "气运"}, map[string]float64{"根骨": 0.6, "气运": 0.4}},
		"exploration": {"探索判定", "exploration", 60, []string{"悟性", "气运"}, map[string]float64{"悟性": 0.6, "气运": 0.4}},
		"escape":      {"脱险判定", "exploration", 50, []string{"根骨", "气运"}, map[string]float64{"根骨": 0.5, "气运": 0.5}},
	}
	rule, ok := rules[kind]
	if !ok {
		return storage.CampaignActionOutcome{}, invalid("kind 必须是 social、insight、stealth、exploration 或 escape")
	}
	description, err := optionalText(arguments, "description", 160)
	if err != nil || strings.TrimSpace(description) == "" {
		return storage.CampaignActionOutcome{}, invalid("description 必须是 1 到 160 个字符的公开行动描述")
	}
	character, err := execution.Store.Character(execution.Campaign.ID)
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		return storage.CampaignActionOutcome{}, err
	}
	insight := campaignCharacterInsight(character)
	rollResult, err := resolveCampaignRoll(ctx, execution, campaign.CampaignRollRequest{
		Key: "uncertain." + kind, Title: rule.title, Description: description, Category: rule.category,
		BaseSuccessRate: rule.base, Sides: 100, RelevantAttributes: rule.attributes, AttributeWeights: rule.weights,
		PlayerState: campaignRollPlayerState(character, insight), Context: campaignRollContext(character),
	})
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	success := deterministicPercent(execution, "uncertain:"+kind) <= int64(rule.base)
	level, rolled, chance := "success", int(deterministicPercent(execution, "uncertain:"+kind)), rule.base
	if !success {
		level = "failure"
	}
	if rollResult != nil {
		success, level, rolled, chance = rollResult.Success, rollResult.Level, rollResult.Rolled, rollResult.SuccessRate
	}
	payload := storage.JSONMap{
		"kind": kind, "description": description, "success": success, "level": level,
		"roll": rolled, "chance": chance,
	}
	outcome := storage.CampaignActionOutcome{
		Events: []storage.CampaignEventDraft{{EventType: "fortune.uncertain_resolved", Visibility: storage.CampaignVisibilityPlayer, Payload: payload}},
		Result: payload,
	}
	attachCampaignRoll(&outcome, rollResult)
	return outcome, nil
}

func gather(ctx context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	if err := allowArguments(arguments, "item_key"); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	itemKey := ""
	if raw, exists := arguments["item_key"]; exists {
		value, ok := raw.(string)
		if !ok || !stableActionKey(value) {
			return storage.CampaignActionOutcome{}, invalid("item_key 必须是稳定 key")
		}
		itemKey = value
	}
	location, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionLocation, execution.Campaign.CurrentLocationKey)
	if err != nil {
		return storage.CampaignActionOutcome{}, rejected("当前位置定义不存在")
	}
	table, ok := gatheringrules.Parse(location.Content["gathering"])
	if !ok {
		return storage.CampaignActionOutcome{}, rejected("当前位置没有可采集资源")
	}
	entries := gatheringrules.MatchingEntries(table, itemKey)
	if len(entries) == 0 {
		return storage.CampaignActionOutcome{}, rejected("当前位置不能采集物品 %s", itemKey)
	}
	entry, ok := gatheringrules.Choose(entries, deterministicUint64(execution, "gather.resource:"+itemKey))
	if !ok {
		return storage.CampaignActionOutcome{}, invalid("当前位置资源表无效")
	}
	quantity := gatheringrules.Quantity(entry, deterministicUint64(execution, "gather.quantity:"+entry.ItemKey+":"+entry.QualityKey))
	region, err := gatheringRegion(execution, location)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if region.ResourceLevel <= 0 {
		return storage.CampaignActionOutcome{}, rejected("当前区域资源已经枯竭，需等待世界变化")
	}
	if region.ResourceLevel < 5000 {
		quantity = maxInt64(1, quantity*region.ResourceLevel/5000)
	}
	definition, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionItem, entry.ItemKey)
	if err != nil {
		return storage.CampaignActionOutcome{}, invalid("采集资源定义不存在: %s", entry.ItemKey)
	}
	stack, err := execution.Store.ItemStack(execution.Campaign.ID, entry.ItemKey, entry.QualityKey)
	if errors.Is(err, storage.ErrNotFound) {
		stack = &storage.CampaignItemStack{
			CampaignID: execution.Campaign.ID, ItemKey: entry.ItemKey, QualityKey: entry.QualityKey,
			Metadata: storage.JSONMap{"name": definition.Name},
		}
	} else if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if stack.Quantity > math.MaxInt64-quantity {
		return storage.CampaignActionOutcome{}, invalid("采集数量溢出")
	}
	character, err := execution.Store.Character(execution.Campaign.ID)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	exhausted := advanceCharacterAge(character, execution.Campaign.GameTime, table.Minutes)
	riskDamage := int64(0)
	var rollResult *campaign.CampaignRollResult
	if table.RiskChanceBPS > 0 {
		rollResult, err = resolveCampaignRoll(ctx, execution, campaign.CampaignRollRequest{
			Key: "gather.risk." + location.Key, Title: "采集风险", Description: "在" + location.Name + "采集" + definition.Name,
			Category: "exploration", BaseSuccessRate: campaignRollBaseFromBPS(10_000 - table.RiskChanceBPS), Sides: 100,
			RelevantAttributes: []string{"根骨", "气运"}, AttributeWeights: map[string]float64{"根骨": 0.6, "气运": 0.4},
			PlayerState: campaignRollPlayerState(character, campaignCharacterInsight(character)), Context: campaignRollContext(character),
		})
		if err != nil {
			return storage.CampaignActionOutcome{}, err
		}
		avoided := int64(deterministicUint64(execution, "gather.risk:"+entry.ItemKey)%10_000) >= table.RiskChanceBPS
		if rollResult != nil {
			avoided = rollResult.Success
		}
		if !avoided {
			riskDamage = minInt64(table.RiskHealthDamage, character.Health)
			character.Health -= riskDamage
		}
	}
	character.UpdatedAt = time.Now()
	if err := execution.Store.SaveCharacter(character); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	stack.Quantity += quantity
	stack.UpdatedAt = time.Now()
	if err := execution.Store.SaveItemStack(stack); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	depletion := minInt64(region.ResourceLevel, quantity*10)
	region.ResourceLevel -= depletion
	region.UpdatedAt = time.Now()
	if err := execution.Store.SaveRegionState(region); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	outcome := storage.CampaignActionOutcome{
		GameTimeDelta: table.Minutes,
		Events: []storage.CampaignEventDraft{{EventType: "world.resource_gathered", Visibility: storage.CampaignVisibilityPlayer, Payload: storage.JSONMap{
			"location_key": location.Key, "item_key": entry.ItemKey, "name": definition.Name,
			"quality_key": entry.QualityKey, "quantity": quantity, "minutes": table.Minutes,
			"risk_health_damage": riskDamage, "region_resource_after": region.ResourceLevel,
		}}},
		Result: storage.JSONMap{
			"location_key": location.Key, "item_key": entry.ItemKey, "quality_key": entry.QualityKey,
			"quantity": quantity, "quantity_after": stack.Quantity, "game_time_delta": table.Minutes,
			"risk_health_damage": riskDamage, "region_resource_after": region.ResourceLevel,
		},
	}
	attachCampaignRoll(&outcome, rollResult)
	if character.Health == 0 {
		status := storage.CampaignStatusDead
		outcome.Status = &status
		outcome.Events = append(outcome.Events, storage.CampaignEventDraft{
			EventType: "character.died", Visibility: storage.CampaignVisibilityPlayer,
			Payload: storage.JSONMap{"cause_key": "gathering-risk", "game_time": execution.Campaign.GameTime + table.Minutes},
		})
		outcome.Result["campaign_status"] = status
	}
	appendLifespanResolution(&outcome, character, exhausted)
	return outcome, nil
}

func gatheringRegion(execution campaign.ActionExecution, location *storage.GameplayDefinition) (*storage.CampaignRegionState, error) {
	current := location
	for depth := 0; depth < 4 && current != nil; depth++ {
		if nodeType, _ := current.Content["node_type"].(string); nodeType == "region" {
			region, err := execution.Store.RegionState(execution.Campaign.ID, current.Key)
			if err != nil {
				return nil, err
			}
			return region, nil
		}
		parentKey, _ := current.Content["parent_key"].(string)
		if parentKey == "" {
			break
		}
		parent, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionLocation, parentKey)
		if err != nil {
			return nil, invalid("采集地点的区域层级无效")
		}
		current = parent
	}
	return nil, invalid("采集地点不属于任何区域")
}

func instantiateNPC(_ context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	if err := requireModelAuthority(execution); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if err := allowArguments(arguments, "archetype_key", "instance_key", "stable_name"); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	archetypeKey, err := requiredKey(arguments, "archetype_key")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	instanceKey, err := requiredKey(arguments, "instance_key")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	definition, err := execution.Store.Definition(
		execution.Campaign.ChallengeVersionID, storage.DefinitionNPCArchetype, archetypeKey,
	)
	if err != nil {
		return storage.CampaignActionOutcome{}, rejected("未知 NPC 原型 %s", archetypeKey)
	}
	spawnable, _ := definition.Content["spawnable"].(bool)
	initial, _ := definition.Content["initial"].(bool)
	if !spawnable || initial {
		return storage.CampaignActionOutcome{}, rejected("NPC 原型不可动态实例化: %s", archetypeKey)
	}
	if !containsDefinitionString(definition.Content["allowed_locations"], execution.Campaign.CurrentLocationKey) {
		return storage.CampaignActionOutcome{}, rejected("NPC 原型不能在当前地点实例化")
	}
	if existing, lookupErr := execution.Store.NPC(execution.Campaign.ID, instanceKey); lookupErr == nil && existing != nil {
		return storage.CampaignActionOutcome{}, rejected("NPC instance_key 已存在: %s", instanceKey)
	} else if lookupErr != nil && !errors.Is(lookupErr, storage.ErrNotFound) {
		return storage.CampaignActionOutcome{}, lookupErr
	}
	maximum := definitionIntDefault(definition.Content["max_instances"], 1)
	count, err := execution.Store.CountNPCsByArchetype(execution.Campaign.ID, archetypeKey)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if maximum <= 0 || count >= maximum {
		return storage.CampaignActionOutcome{}, rejected("NPC 原型实例数已达上限")
	}
	name, err := optionalText(arguments, "stable_name", 128)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if name == "" {
		name = definition.Name
	}
	realmKey, _ := definition.Content["realm_key"].(string)
	stageKey, _ := definition.Content["stage_key"].(string)
	if stageKey == "" {
		stageKey = "initial"
	}
	factionKey, _ := definition.Content["faction_key"].(string)
	publicProfile, _ := definition.Content["public_profile"].(map[string]any)
	privateState, _ := definition.Content["private_state"].(map[string]any)
	goals, _ := definition.Content["goals"].(map[string]any)
	if publicProfile == nil {
		publicProfile = map[string]any{}
	}
	if privateState == nil {
		privateState = map[string]any{}
	}
	if goals == nil {
		goals = map[string]any{}
	}
	now := time.Now()
	npc := &storage.CampaignNPC{
		ID: uuid.New(), CampaignID: execution.Campaign.ID, ArchetypeKey: archetypeKey,
		InstanceKey: instanceKey, StableName: name, FactionKey: factionKey,
		RealmKey: realmKey, StageKey: stageKey, CurrentLocationKey: execution.Campaign.CurrentLocationKey,
		Status: "active", PublicProfile: storage.JSONMap(publicProfile), PrivateState: storage.JSONMap(privateState),
		Goals: storage.JSONMap(goals), LastActiveAt: execution.Campaign.GameTime,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := execution.Store.CreateNPC(npc); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	return singleEvent("npc.instantiated", storage.JSONMap{
		"instance_key": instanceKey, "archetype_key": archetypeKey, "name": name,
		"location_key": execution.Campaign.CurrentLocationKey,
	}), nil
}

func moveNPC(_ context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	if err := requireModelAuthority(execution); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if err := allowArguments(arguments, "npc_key", "destination_key"); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	npcKey, err := requiredKey(arguments, "npc_key")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	destination, err := requiredKey(arguments, "destination_key")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	npc, err := execution.Store.NPC(execution.Campaign.ID, npcKey)
	if err != nil || npc.Status != "active" {
		return storage.CampaignActionOutcome{}, rejected("NPC 不存在或当前不能移动: %s", npcKey)
	}
	if execution.ActorType == "model" && npc.CurrentLocationKey != execution.Campaign.CurrentLocationKey {
		return storage.CampaignActionOutcome{}, rejected("模型只能移动当前地点的 NPC")
	}
	if destination == npc.CurrentLocationKey {
		return storage.CampaignActionOutcome{}, rejected("NPC 已位于目标地点")
	}
	current, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionLocation, npc.CurrentLocationKey)
	if err != nil {
		return storage.CampaignActionOutcome{}, invalid("NPC 当前位置定义不存在")
	}
	if _, connected := connectionTravelMinutes(current.Content, destination); !connected {
		return storage.CampaignActionOutcome{}, rejected("NPC 目标地点与当前位置不相邻")
	}
	if _, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionLocation, destination); err != nil {
		return storage.CampaignActionOutcome{}, rejected("NPC 目标地点定义不存在")
	}
	from := npc.CurrentLocationKey
	npc.CurrentLocationKey = destination
	npc.LastActiveAt = execution.Campaign.GameTime
	npc.UpdatedAt = time.Now()
	if err := execution.Store.SaveNPC(npc); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	visibility := storage.CampaignVisibilityModel
	if from == execution.Campaign.CurrentLocationKey || destination == execution.Campaign.CurrentLocationKey {
		visibility = storage.CampaignVisibilityPlayer
	}
	payload := storage.JSONMap{"npc_key": npcKey, "from_location_key": from, "location_key": destination}
	return storage.CampaignActionOutcome{
		Events: []storage.CampaignEventDraft{{EventType: "npc.moved", Visibility: visibility, Payload: payload}},
		Result: payload,
	}, nil
}

func setNPCStatus(_ context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	if err := requireModelAuthority(execution); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if err := allowArguments(arguments, "npc_key", "status"); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	npcKey, err := requiredKey(arguments, "npc_key")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	status, err := requiredKey(arguments, "status")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	npc, err := execution.Store.NPC(execution.Campaign.ID, npcKey)
	if err != nil {
		return storage.CampaignActionOutcome{}, rejected("NPC 不存在: %s", npcKey)
	}
	if execution.ActorType == "model" && npc.CurrentLocationKey != execution.Campaign.CurrentLocationKey {
		return storage.CampaignActionOutcome{}, rejected("模型只能更新当前地点的 NPC")
	}
	if !validNPCStatusTransition(npc.Status, status) {
		return storage.CampaignActionOutcome{}, rejected("NPC 状态不能从 %s 变为 %s", npc.Status, status)
	}
	previous := npc.Status
	npc.Status = status
	npc.LastActiveAt = execution.Campaign.GameTime
	npc.UpdatedAt = time.Now()
	if err := execution.Store.SaveNPC(npc); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	visibility := storage.CampaignVisibilityModel
	if npc.CurrentLocationKey == execution.Campaign.CurrentLocationKey {
		visibility = storage.CampaignVisibilityPlayer
	}
	payload := storage.JSONMap{"npc_key": npcKey, "previous_status": previous, "status": status}
	return storage.CampaignActionOutcome{
		Events: []storage.CampaignEventDraft{{EventType: "npc.status_changed", Visibility: visibility, Payload: payload}},
		Result: payload,
	}, nil
}

func validNPCStatusTransition(current, target string) bool {
	if current == target {
		return false
	}
	allowed := map[string]map[string]bool{
		"active":   {"missing": true, "dead": true, "departed": true, "sealed": true},
		"missing":  {"active": true, "dead": true, "departed": true},
		"sealed":   {"active": true, "dead": true},
		"departed": {"active": true},
	}
	return allowed[current][target]
}

func recordInteraction(_ context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	if err := requireModelAuthority(execution); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if err := allowArguments(arguments, "npc_key", "trust_delta", "fear_delta", "affinity_delta", "hostility_delta", "debt_delta", "public_stance", "known_fact_key", "known_fact"); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	npcKey, err := requiredKey(arguments, "npc_key")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	npc, err := execution.Store.NPC(execution.Campaign.ID, npcKey)
	if err != nil || npc.Status != "active" {
		return storage.CampaignActionOutcome{}, rejected("NPC 不存在或不可交互: %s", npcKey)
	}
	if npc.CurrentLocationKey != execution.Campaign.CurrentLocationKey {
		return storage.CampaignActionOutcome{}, rejected("NPC 不在当前地点")
	}
	relation, err := execution.Store.Relationship(execution.Campaign.ID, npc.ID)
	if errors.Is(err, storage.ErrNotFound) {
		relation = &storage.CampaignRelationship{
			CampaignID: execution.Campaign.ID, NPCID: npc.ID, PublicStance: "neutral", KnownFacts: storage.JSONMap{},
		}
	} else if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	deltas := map[string]int64{}
	for _, field := range []string{"trust_delta", "fear_delta", "affinity_delta", "hostility_delta"} {
		value, fieldErr := optionalInt(arguments, field, 0, -20, 20)
		if fieldErr != nil {
			return storage.CampaignActionOutcome{}, fieldErr
		}
		deltas[field] = value
	}
	debtDelta, err := optionalInt(arguments, "debt_delta", 0, -1000, 1000)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	relation.Trust = int(clampInt64(int64(relation.Trust)+deltas["trust_delta"], -100, 100))
	relation.Fear = int(clampInt64(int64(relation.Fear)+deltas["fear_delta"], 0, 100))
	relation.Affinity = int(clampInt64(int64(relation.Affinity)+deltas["affinity_delta"], -100, 100))
	relation.Hostility = int(clampInt64(int64(relation.Hostility)+deltas["hostility_delta"], 0, 100))
	relation.Debt += debtDelta
	if stance, ok := arguments["public_stance"]; ok {
		value, valid := stance.(string)
		if !valid || strings.TrimSpace(value) == "" || len([]rune(value)) > 32 {
			return storage.CampaignActionOutcome{}, invalid("public_stance 无效")
		}
		relation.PublicStance = value
	}
	if factKeyRaw, ok := arguments["known_fact_key"]; ok {
		factKey, valid := factKeyRaw.(string)
		fact, hasFact := arguments["known_fact"]
		if !valid || !stableActionKey(factKey) || !hasFact {
			return storage.CampaignActionOutcome{}, invalid("known_fact_key 与 known_fact 必须成对且 key 合法")
		}
		if relation.KnownFacts == nil {
			relation.KnownFacts = storage.JSONMap{}
		}
		relation.KnownFacts[factKey] = fact
	} else if _, hasFact := arguments["known_fact"]; hasFact {
		return storage.CampaignActionOutcome{}, invalid("known_fact 缺少 known_fact_key")
	}
	relation.LastInteractionAt = execution.Campaign.GameTime
	relation.UpdatedAt = time.Now()
	if err := execution.Store.SaveRelationship(relation); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	return singleEvent("npc.relationship_changed", storage.JSONMap{
		"npc_key": npcKey, "npc_name": npc.StableName, "trust": relation.Trust,
		"fear": relation.Fear, "affinity": relation.Affinity, "hostility": relation.Hostility,
		"debt": relation.Debt, "public_stance": relation.PublicStance,
	}), nil
}

func adjustFaction(_ context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	if err := requireModelAuthority(execution); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if err := allowArguments(arguments, "faction_key", "reputation_delta", "contribution_delta", "wanted_delta"); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	key, err := requiredKey(arguments, "faction_key")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	definition, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionFaction, key)
	if err != nil {
		return storage.CampaignActionOutcome{}, rejected("未知势力 %s", key)
	}
	reputationDelta, err := optionalInt(arguments, "reputation_delta", 0, -500, 500)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	contributionDelta, err := optionalInt(arguments, "contribution_delta", 0, -1000, 1000)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	wantedDelta, err := optionalInt(arguments, "wanted_delta", 0, -1, 1)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	standing, err := execution.Store.FactionStanding(execution.Campaign.ID, key)
	if errors.Is(err, storage.ErrNotFound) {
		standing = &storage.CampaignFactionStanding{
			CampaignID: execution.Campaign.ID, FactionKey: key, PositionKey: "outsider", Permissions: storage.JSONMap{},
		}
	} else if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	standing.Reputation = int(clampInt64(int64(standing.Reputation)+reputationDelta, -10000, 10000))
	standing.Contribution = clampInt64(standing.Contribution+contributionDelta, 0, factionrules.MaximumContribution)
	standing.WantedLevel = int(clampInt64(int64(standing.WantedLevel)+wantedDelta, 0, 5))
	standing.PositionKey = factionrules.Position(definition.Content, standing.Contribution)
	standing.Permissions = storage.JSONMap(factionrules.DerivePermissions(definition.Content, standing.Reputation))
	standing.UpdatedAt = time.Now()
	if err := execution.Store.SaveFactionStanding(standing); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	return singleEvent("faction.reputation_changed", storage.JSONMap{
		"faction_key": key, "reputation": standing.Reputation,
		"reputation_level": factionrules.ReputationLevel(standing.Reputation), "position_key": standing.PositionKey,
		"contribution": standing.Contribution, "wanted_level": standing.WantedLevel,
		"permissions": standing.Permissions,
	}), nil
}

func missingEntryPermissions(execution campaign.ActionExecution, content storage.JSONMap) ([]string, error) {
	requirements, ok := factionrules.EntryRequirements(content)
	if !ok {
		return nil, invalid("目标地点的 entry_requirements 定义无效")
	}
	standings := map[string]*storage.CampaignFactionStanding{}
	missing := make([]string, 0)
	var character *storage.CampaignCharacter
	for _, requirement := range requirements {
		if requirement.MinRealmRank > 0 {
			if character == nil {
				var err error
				character, err = execution.Store.Character(execution.Campaign.ID)
				if errors.Is(err, storage.ErrNotFound) {
					missing = append(missing, fmt.Sprintf("realm_rank.%d", requirement.MinRealmRank))
					continue
				}
				if err != nil {
					return nil, err
				}
			}
			if character.RealmRank < requirement.MinRealmRank {
				missing = append(missing, fmt.Sprintf("realm_rank.%d", requirement.MinRealmRank))
			}
			continue
		}
		standing, loaded := standings[requirement.FactionKey]
		if !loaded {
			var err error
			standing, err = execution.Store.FactionStanding(execution.Campaign.ID, requirement.FactionKey)
			if errors.Is(err, storage.ErrNotFound) {
				standing = nil
			} else if err != nil {
				return nil, err
			}
			standings[requirement.FactionKey] = standing
		}
		if standing == nil || !factionrules.IsGranted(standing.Permissions, requirement.PermissionKey) {
			missing = append(missing, requirement.FactionKey+"."+requirement.PermissionKey)
		}
	}
	return missing, nil
}

func singleEvent(eventType string, payload storage.JSONMap) storage.CampaignActionOutcome {
	return storage.CampaignActionOutcome{
		Events: []storage.CampaignEventDraft{{EventType: eventType, Visibility: storage.CampaignVisibilityPlayer, Payload: payload}},
		Result: payload,
	}
}

func requireModelAuthority(execution campaign.ActionExecution) error {
	if execution.ActorType != "model" && execution.ActorType != "system" {
		return rejected("动作 %s 只能由叙事裁定或系统执行", execution.ActionKey)
	}
	return nil
}

// requireDefinitionGrantAuthority prevents narrative actions from minting
// economy-backed items or equipment. System-owned workflows may still grant any
// validated definition; model grants require an explicit content whitelist.
func requireDefinitionGrantAuthority(execution campaign.ActionExecution, definition *storage.GameplayDefinition) error {
	if execution.ActorType == "system" {
		return nil
	}
	if execution.ActorType != "model" {
		return rejected("动作 %s 只能由叙事裁定或系统执行", execution.ActionKey)
	}
	if grantable, _ := definition.Content["model_grantable"].(bool); !grantable {
		return rejected("%s 必须通过采集、任务、制作、交易或系统结算获得", definition.Name)
	}
	return nil
}

func allowArguments(arguments map[string]any, allowed ...string) error {
	set := map[string]bool{}
	for _, key := range allowed {
		set[key] = true
	}
	for key := range arguments {
		if !set[key] {
			return invalid("包含未知参数 %s", key)
		}
	}
	return nil
}

func requiredKey(arguments map[string]any, field string) (string, error) {
	value, ok := arguments[field].(string)
	if !ok || !stableActionKey(value) {
		return "", invalid("%s 必须是稳定 key", field)
	}
	return value, nil
}

func optionalKey(arguments map[string]any, field, fallback string) (string, error) {
	if _, ok := arguments[field]; !ok {
		return fallback, nil
	}
	return requiredKey(arguments, field)
}

func optionalText(arguments map[string]any, field string, maxLength int) (string, error) {
	raw, ok := arguments[field]
	if !ok {
		return "", nil
	}
	value, ok := raw.(string)
	if !ok || strings.TrimSpace(value) != value || len([]rune(value)) > maxLength {
		return "", invalid("%s 必须是不超过 %d 个字符的文本", field, maxLength)
	}
	return value, nil
}

func conditionCategory(arguments map[string]any) (string, error) {
	value, ok := arguments["category"].(string)
	if !ok || !map[string]bool{"injury": true, "poison": true, "inner_demon": true, "long_term": true}[value] {
		return "", invalid("category 必须是 injury、poison、inner_demon 或 long_term")
	}
	return value, nil
}

func requiredUUID(arguments map[string]any, field string) (uuid.UUID, error) {
	value, ok := arguments[field].(string)
	if !ok {
		return uuid.Nil, invalid("%s 必须是 UUID", field)
	}
	id, err := uuid.Parse(value)
	if err != nil {
		return uuid.Nil, invalid("%s 必须是 UUID", field)
	}
	return id, nil
}

func requiredInt(arguments map[string]any, field string, minimum, maximum int64) (int64, error) {
	value, ok := exactActionInt(arguments[field])
	if !ok || value < minimum || value > maximum {
		return 0, invalid("%s 必须是 %d 到 %d 的整数", field, minimum, maximum)
	}
	return value, nil
}

func optionalInt(arguments map[string]any, field string, fallback, minimum, maximum int64) (int64, error) {
	if _, ok := arguments[field]; !ok {
		return fallback, nil
	}
	return requiredInt(arguments, field, minimum, maximum)
}

func exactActionInt(value any) (int64, bool) {
	switch number := value.(type) {
	case int:
		return int64(number), true
	case int64:
		return number, true
	case float64:
		if math.IsNaN(number) || math.IsInf(number, 0) || number != math.Trunc(number) ||
			number < math.MinInt64 || number > math.MaxInt64 {
			return 0, false
		}
		return int64(number), true
	default:
		return 0, false
	}
}

func definitionIntDefault(value any, fallback int64) int64 {
	if number, ok := exactActionInt(value); ok {
		return number
	}
	return fallback
}

func containsDefinitionString(value any, wanted string) bool {
	list, ok := value.([]any)
	if !ok {
		return false
	}
	for _, raw := range list {
		if raw == wanted {
			return true
		}
	}
	return false
}

func connectionTravelMinutes(content storage.JSONMap, destination string) (int64, bool) {
	connections, ok := content["connections"].([]any)
	if !ok {
		return 0, false
	}
	for _, raw := range connections {
		connection, ok := raw.(map[string]any)
		if !ok || connection["location_key"] != destination {
			continue
		}
		minutes, ok := exactActionInt(connection["travel_minutes"])
		return minutes, ok && minutes > 0
	}
	return 0, false
}

func stageForProgress(content storage.JSONMap, progress, required int64) string {
	stages, ok := content["stages"].([]any)
	if !ok || len(stages) == 0 || required <= 0 {
		return "initial"
	}
	index := int(progress * int64(len(stages)) / required)
	if index >= len(stages) {
		index = len(stages) - 1
	}
	stage, _ := stages[index].(string)
	return stage
}

func growCharacterBaseStats(character *storage.CampaignCharacter, content storage.JSONMap) {
	normalizeCharacterBaseStats(character)
	character.BaseMaxHealth = maxInt64(character.BaseMaxHealth, definitionIntDefault(content["max_health"], character.BaseMaxHealth))
	character.BaseMaxMana = maxInt64(character.BaseMaxMana, definitionIntDefault(content["max_mana"], character.BaseMaxMana))
	character.BaseMaxSpiritualSense = maxInt64(character.BaseMaxSpiritualSense, definitionIntDefault(content["max_spiritual_sense"], character.BaseMaxSpiritualSense))
}

func recalculateCharacterStats(execution campaign.ActionExecution, character *storage.CampaignCharacter) (storage.JSONMap, error) {
	normalizeCharacterBaseStats(character)
	effects := map[string]int64{}
	techniques, err := execution.Store.Techniques(execution.Campaign.ID)
	if err != nil {
		return nil, err
	}
	for _, technique := range techniques {
		if technique.Status != "active" || technique.EquippedSlot == nil {
			continue
		}
		definition, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionTechnique, technique.TechniqueKey)
		if err != nil {
			return nil, invalid("已装配功法定义不存在: %s", technique.TechniqueKey)
		}
		current, ok := effectrules.TechniqueEffects(definition.Content, technique.LearnedLayer)
		if !ok {
			return nil, invalid("功法 %s 的 layer_effects 无效", technique.TechniqueKey)
		}
		effectrules.Add(effects, current)
	}
	items, err := execution.Store.ItemInstances(execution.Campaign.ID)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if item.State != "equipped" || item.EquippedSlot == nil || item.Durability <= 0 {
			continue
		}
		definition, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionEquipment, item.DefinitionKey)
		if err != nil {
			return nil, invalid("已装备物品定义不存在: %s", item.DefinitionKey)
		}
		bonuses, ok := effectrules.EquipmentBonuses(definition.Content)
		if !ok {
			return nil, invalid("装备 %s 的 bonuses 无效", item.DefinitionKey)
		}
		effectrules.Add(effects, bonuses)
	}
	character.MaxHealth = maxInt64(1, character.BaseMaxHealth+effects["max_health"])
	character.MaxMana = maxInt64(1, character.BaseMaxMana+effects["max_mana"])
	character.MaxSpiritualSense = maxInt64(1, character.BaseMaxSpiritualSense+effects["max_spiritual_sense"])
	character.Health = clampInt64(character.Health, 0, character.MaxHealth)
	character.Mana = clampInt64(character.Mana, 0, character.MaxMana)
	character.SpiritualSense = clampInt64(character.SpiritualSense, 0, character.MaxSpiritualSense)
	character.UpdatedAt = time.Now()
	out := storage.JSONMap{}
	for key, value := range effects {
		out[key] = value
	}
	return out, nil
}

func normalizeCharacterBaseStats(character *storage.CampaignCharacter) {
	if character.BaseMaxHealth <= 0 {
		character.BaseMaxHealth = maxInt64(1, character.MaxHealth)
	}
	if character.BaseMaxMana <= 0 {
		character.BaseMaxMana = maxInt64(1, character.MaxMana)
	}
	if character.BaseMaxSpiritualSense <= 0 {
		character.BaseMaxSpiritualSense = maxInt64(1, character.MaxSpiritualSense)
	}
}

func expireDueConditions(execution campaign.ActionExecution, outcome *storage.CampaignActionOutcome) error {
	if outcome == nil || outcome.GameTimeDelta <= 0 {
		return nil
	}
	character, err := execution.Store.Character(execution.Campaign.ID)
	if err != nil {
		return err
	}
	dueAt := execution.Campaign.GameTime + outcome.GameTimeDelta
	changed := false
	for category, conditions := range map[string]storage.JSONMap{
		"injury": character.Injuries, "long_term": character.LongTermEffects,
	} {
		for key, raw := range conditions {
			condition, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			appliedAt, appliedOK := exactActionInt(condition["applied_at_game_time"])
			duration, durationOK := exactActionInt(condition["duration_minutes"])
			if !appliedOK || !durationOK || duration <= 0 || appliedAt > math.MaxInt64-duration || appliedAt+duration > dueAt {
				continue
			}
			delete(conditions, key)
			changed = true
			eventCategory := category
			if storedCategory, ok := condition["category"].(string); ok && storedCategory != "" {
				eventCategory = storedCategory
			}
			outcome.Events = append(outcome.Events, storage.CampaignEventDraft{
				EventType: "character.condition_expired", Visibility: storage.CampaignVisibilityPlayer,
				Payload: storage.JSONMap{"category": eventCategory, "effect_key": key, "expired_at_game_time": dueAt},
			})
		}
	}
	if !changed {
		return nil
	}
	character.UpdatedAt = time.Now()
	return execution.Store.SaveCharacter(character)
}

func deterministicPercent(execution campaign.ActionExecution, purpose string) int64 {
	return int64(deterministicUint64(execution, purpose)%100) + 1
}

func deterministicUint64(execution campaign.ActionExecution, purpose string) uint64 {
	payload := fmt.Sprintf("%d:%d:%s:%s", execution.Campaign.RandomSeed, execution.Campaign.Revision, execution.ActionID, purpose)
	sum := sha256.Sum256([]byte(payload))
	return binary.BigEndian.Uint64(sum[:8])
}

func stableActionKey(value string) bool {
	if value == "" || strings.TrimSpace(value) != value || len(value) > 128 {
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

func clampInt64(value, minimum, maximum int64) int64 {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}

func maxInt64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}

func minInt64(left, right int64) int64 {
	if left < right {
		return left
	}
	return right
}

func advanceCharacterAge(character *storage.CampaignCharacter, gameTime, deltaMinutes int64) bool {
	if character == nil || deltaMinutes <= 0 {
		return character != nil && character.AgeDays >= character.LifespanDays
	}
	beforeDays := gameTime / 1440
	afterDays := (gameTime + deltaMinutes) / 1440
	if afterDays > beforeDays {
		character.AgeDays += afterDays - beforeDays
	}
	return character.AgeDays >= character.LifespanDays
}

func appendLifespanResolution(outcome *storage.CampaignActionOutcome, character *storage.CampaignCharacter, exhausted bool) {
	if !exhausted {
		return
	}
	status := storage.CampaignStatusDead
	outcome.Status = &status
	outcome.Events = append(outcome.Events, storage.CampaignEventDraft{
		EventType: "character.lifespan_exhausted", Visibility: storage.CampaignVisibilityPlayer,
		Payload: storage.JSONMap{"age_days": character.AgeDays, "lifespan_days": character.LifespanDays},
	})
	outcome.Result["campaign_status"] = status
}

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", campaign.ErrInvalidAction, fmt.Sprintf(format, args...))
}

func rejected(format string, args ...any) error {
	return campaign.RejectAction(campaign.ActionViolation{
		Code: "action.precondition_failed", Message: fmt.Sprintf(format, args...), Public: true,
	})
}
