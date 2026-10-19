package campaignactions

import (
	"context"
	"errors"
	"fmt"
	"github.com/ialtone/fatecraft/internal/gameplay/components/worldactions"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/campaign"
	"github.com/ialtone/fatecraft/internal/gameplay/components/factionrules"
	"github.com/ialtone/fatecraft/internal/gameplay/components/gatheringrules"
	gameplay "github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/campaigncontract"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/effectrules"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/practicerules"
	"github.com/ialtone/fatecraft/internal/storage"
)

const (
	ActionPractice          = campaigncontract.ActionPractice
	ActionRetreat           = campaigncontract.ActionRetreat
	ActionRecover           = campaigncontract.ActionRecover
	ActionRenameCharacter   = campaigncontract.ActionRenameCharacter
	ActionBreakthrough      = campaigncontract.ActionBreakthrough
	ActionBeginAscension    = campaigncontract.ActionBeginAscension
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
	ActionAbandon           = campaigncontract.ActionAbandon
)

func Register(service *campaign.Service) {
	type actionSpec struct {
		key, description string
		actors           []string
		handler          campaign.ActionHandlerFunc
	}
	specs := []actionSpec{
		{ActionAbandon, "由玩家确认中止本世，不产生终局积分", []string{"player"}, abandonCampaign},
		{ActionRetreat, "按固定方案消耗材料、法力和时间闭关；遇期限或事件预算不足时拒绝", []string{"player", "model"}, retreat},
		{ActionRecover, "按固定方案消耗时间和材料恢复气血、法力、神识或普通伤势", []string{"player", "model"}, recoverCharacter},
		{ActionBeginAscension, "确认消耗准备材料并引动最终天劫；需境界圆满及主线完成", []string{"player"}, beginAscension},
		{ActionRenameCharacter, "更新角色的长期公开姓名", []string{"player", "model"}, renameCharacter},
		{ActionPractice, "修炼已掌握且已装配的功法", []string{"player", "model"}, practice},
		{ActionBreakthrough, "尝试突破到定义中的下一境界", []string{"player", "model"}, attemptBreakthrough},
		{ActionLearnTechnique, "消耗传承物并学习功法", []string{"player", "model"}, learnTechnique},
		{ActionEquipTechnique, "将已掌握功法装配到能力槽", []string{"player", "model"}, equipTechnique},
		{ActionUnequipTechnique, "卸下已装配的功法", []string{"player", "model"}, unequipTechnique},
		{ActionApplyCondition, "施加受限伤势、中毒、心魔或长期效果", []string{"model", "system"}, applyCondition},
		{ActionResolveCondition, "移除已经存在的伤势或长期效果", []string{"model", "system"}, resolveCondition},
		{ActionAdjustVitals, "结算叙事造成的气血、法力与神识损耗；恢复必须使用休整或物品", []string{"model", "system"}, adjustVitals},
		{ActionGainItem, "授予定义目录中允许堆叠的物品", []string{"model", "system"}, worldactions.GainItem},
		{ActionUseItem, "消耗堆叠物品并应用后端白名单效果", []string{"player", "model"}, useItem},
		{ActionGainEquipment, "授予定义目录中的唯一装备实例", []string{"model", "system"}, gainEquipment},
		{ActionEquipItem, "将唯一装备实例装配到合法槽位", []string{"player", "model"}, equipItem},
		{ActionUnequipItem, "卸下已装备的唯一物品", []string{"player", "model"}, unequipItem},
		{ActionLearnRecipe, "消耗配方载体并掌握版本化生产配方", []string{"player", "model"}, learnRecipe},
		{ActionCraftRecipe, "按已掌握配方执行可信制作与品质判定", []string{"player", "model"}, executeRecipe},
		{ActionTradeBuy, "从当前位置市场的有限商品池购买物品", []string{"player", "model"}, worldactions.TradeBuy},
		{ActionTradeSell, "向当前位置市场出售物品或未装备实例", []string{"player", "model"}, worldactions.TradeSell},
		{ActionAdjustMarket, "由系统调整市场动态价格系数", []string{"system"}, worldactions.AdjustMarketPrice},
		{ActionTravel, "沿已发现的相邻连接旅行", []string{"player", "model"}, travel},
		{ActionInvestigate, "调查当前地点并发现相邻地点", []string{"player", "model"}, investigate},
		{ActionResolveUncertain, "仅判定具有明确成败目标、真实不确定性和失败后果的交涉、洞察、潜行、探索或脱险；普通交谈、打听公开信息、倾听与观察已呈现事实不得调用；不直接改写其他领域事实", []string{"model"}, resolveUncertainAction},
		{ActionGather, "在当前地点的权威资源表中采集、采矿或狩猎", []string{"player", "model"}, gather},
		{ActionInstantiateNPC, "按版本原型和实例上限在当前地点创建 NPC", []string{"model", "system"}, instantiateNPC},
		{ActionMoveNPC, "沿地点连接移动 NPC 实例", []string{"model", "system"}, worldactions.MoveNPC},
		{ActionSetNPCStatus, "按状态机更新 NPC 生命周期状态", []string{"model", "system"}, worldactions.SetNPCStatus},
		{ActionRecordInteraction, "记录当前地点 NPC 的多维关系变化；玩家交谈时每次固定 +1 信任/+1 亲近", []string{"player", "model", "system"}, worldactions.RecordInteraction},
		{ActionAdjustFaction, "调整已定义势力的声望、贡献与通缉", []string{"model", "system"}, worldactions.AdjustFaction},
		{ActionAcceptQuest, "接受已提供且符合地点条件的任务或承诺", []string{"player", "model"}, worldactions.AcceptQuest},
		{ActionSubmitQuest, "提交任务并由后端核验目标、扣除物品和发放奖励；最终道路须玩家确认", []string{"player", "model"}, submitCultivationQuest},
		{ActionStartEncounter, "在当前地点开始一个版本化遭遇", []string{"player", "model"}, startEncounter},
		{ActionEncounterAct, "在活动遭遇中执行一次攻击或防御行动", []string{"player", "model"}, actEncounter},
		{ActionEncounterFlee, "尝试从活动遭遇中撤离", []string{"player", "model"}, fleeEncounter},
	}
	for _, spec := range specs {
		registerAction(service, spec.key, spec.description, spec.actors, spec.handler)
	}
}

func abandonCampaign(_ context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	if err := worldactions.AllowArguments(arguments); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	status := storage.CampaignStatusAbandoned
	key := "abandoned"
	return storage.CampaignActionOutcome{
		Status: &status, OutcomeKey: &key,
		Events: []storage.CampaignEventDraft{{EventType: "campaign.abandoned", Visibility: storage.CampaignVisibilityPlayer, Payload: storage.JSONMap{"game_time": execution.Campaign.GameTime}}},
		Result: storage.JSONMap{"campaign_status": status, "outcome_key": key},
	}, nil
}

func renameCharacter(_ context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	if err := worldactions.AllowArguments(arguments, "name"); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	name, err := worldactions.OptionalText(arguments, "name", 128)
	if err != nil || strings.TrimSpace(name) == "" {
		return storage.CampaignActionOutcome{}, worldactions.Invalid("name 必须是 1 到 128 个字符的公开姓名")
	}
	character, err := cultivationStore(execution).Character(execution.Campaign.ID)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if character.Health <= 0 {
		return storage.CampaignActionOutcome{}, worldactions.Rejected("角色已无法更改姓名")
	}
	if character.Name == name {
		return storage.CampaignActionOutcome{}, worldactions.Rejected("角色姓名没有变化")
	}
	oldName := character.Name
	character.Name = name
	character.UpdatedAt = time.Now()
	if err := cultivationStore(execution).SaveCharacter(character); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	return worldactions.SingleEvent("character.renamed", storage.JSONMap{
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
		if _, ok := execution.Store.(Store); !ok {
			return storage.CampaignActionOutcome{}, fmt.Errorf("修仙模块缺少领域仓储")
		}
		if err := rejectWhileEncounterActive(execution, key); err != nil {
			return storage.CampaignActionOutcome{}, err
		}
		outcome, err := handler(ctx, execution, arguments)
		if err != nil {
			return storage.CampaignActionOutcome{}, err
		}
		if err := settleProgressRewards(ctx, execution, &outcome); err != nil {
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
		if err := worldactions.ProcessDueScheduledEvents(execution, &outcome); err != nil {
			return storage.CampaignActionOutcome{}, err
		}
		return outcome, nil
	})
	service.RegisterAction(gameplay.EngineRef{Key: campaigncontract.EngineKey, Version: campaigncontract.EngineVersion}, campaign.RegisteredAction{
		Definition: campaign.ActionDefinition{Ending: key == ActionAbandon,
			Key: key, Description: description, Actors: actors, ArgumentSchema: actionArgumentSchema(key),
			Writes: writes, Events: events,
		},
		Capability: cultivationCapabilityProvider{},
		Handler:    wrapped,
	})
}

func rejectWhileEncounterActive(execution campaign.ActionExecution, actionKey string) error {
	active, err := cultivationStore(execution).ActiveEncounter(execution.Campaign.ID)
	if errors.Is(err, storage.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if active != nil && actionKey != ActionEncounterAct && actionKey != ActionEncounterFlee && actionKey != ActionAbandon {
		return worldactions.Rejected("必须先解决活动遭遇 %s", active.EncounterKey)
	}
	return nil
}

func practice(_ context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	if err := worldactions.AllowArguments(arguments, "technique_key", "sessions"); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	techniqueKey, err := worldactions.RequiredKey(arguments, "technique_key")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	sessions, err := worldactions.OptionalInt(arguments, "sessions", 1, 1, practicerules.MaximumSessions)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	technique, err := cultivationStore(execution).Technique(execution.Campaign.ID, techniqueKey)
	if err != nil {
		return storage.CampaignActionOutcome{}, worldactions.Rejected("尚未掌握功法 %s", techniqueKey)
	}
	if technique.Status != "active" || technique.EquippedSlot == nil {
		return storage.CampaignActionOutcome{}, worldactions.Rejected("功法 %s 未处于已装配的可修炼状态", techniqueKey)
	}
	definition, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionTechnique, techniqueKey)
	if err != nil {
		return storage.CampaignActionOutcome{}, worldactions.Rejected("功法定义不存在: %s", techniqueKey)
	}
	character, err := cultivationStore(execution).Character(execution.Campaign.ID)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	activeEffects, err := equippedEffects(execution)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	rules := practicerules.Evaluate(practicerules.Facts{
		Character: character, Technique: *technique, Definition: *definition, EquippedEffects: activeEffects,
	}, &sessions)
	if !rules.Available {
		if rules.InvalidDefinition {
			return storage.CampaignActionOutcome{}, worldactions.Invalid("%s", rules.ViolationMessage)
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
	character.CultivationProgress = worldactions.ClampInt64(character.CultivationProgress+gain, 0, character.CultivationRequired*2)
	if realm, realmErr := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionRealm, character.RealmKey); realmErr == nil {
		character.StageKey = stageForProgress(realm.Content, character.CultivationProgress, character.CultivationRequired)
	}
	character.UpdatedAt = time.Now()
	technique.Mastery = worldactions.ClampInt64(technique.Mastery+masteryGain, 0, 10000)
	technique.Insight = maxInt64(0, technique.Insight+insightGain)
	maxLayer := worldactions.DefinitionIntDefault(definition.Content["max_layer"], 1)
	layer, validLayer := effectrules.LayerForMastery(technique.Mastery, maxLayer)
	if !validLayer {
		return storage.CampaignActionOutcome{}, worldactions.Invalid("功法定义的层级规则无效")
	}
	technique.LearnedLayer = layer
	exhausted := advanceCharacterAge(character, execution.Campaign.GameTime, minutes)
	technique.UpdatedAt = character.UpdatedAt
	if err := cultivationStore(execution).SaveTechnique(technique); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	derivedEffects, err := recalculateCharacterStats(execution, character)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if err := cultivationStore(execution).SaveCharacter(character); err != nil {
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
	if err := worldactions.AllowArguments(arguments); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	character, err := cultivationStore(execution).Character(execution.Campaign.ID)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if character.CultivationProgress < character.CultivationRequired {
		return storage.CampaignActionOutcome{}, worldactions.Rejected("修为尚未圆满：%d/%d", character.CultivationProgress, character.CultivationRequired)
	}
	current, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionRealm, character.RealmKey)
	if err != nil {
		return storage.CampaignActionOutcome{}, worldactions.Rejected("当前境界定义不存在")
	}
	currentRank, currentRankOK := worldactions.ExactActionInt(current.Content["rank"])
	if !currentRankOK || currentRank < 0 || int64(character.RealmRank) != currentRank {
		return storage.CampaignActionOutcome{}, worldactions.Rejected("角色境界缓存与当前玩法定义不一致")
	}
	nextKey, _ := current.Content["next_realm_key"].(string)
	if nextKey == "" {
		return storage.CampaignActionOutcome{}, worldactions.Rejected("当前境界没有可突破的下一境界")
	}
	next, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionRealm, nextKey)
	if err != nil {
		return storage.CampaignActionOutcome{}, worldactions.Rejected("下一境界定义不存在: %s", nextKey)
	}
	nextRank, nextRankOK := worldactions.ExactActionInt(next.Content["rank"])
	if !nextRankOK || nextRank < 0 || nextRank <= currentRank {
		return storage.CampaignActionOutcome{}, worldactions.Rejected("下一境界 %s 缺少有效且递增的 rank", nextKey)
	}
	minutes := worldactions.DefinitionIntDefault(current.Content["breakthrough_minutes"], 24*60)
	if err := breakthroughTimeReady(character, execution.Campaign.GameTime, minutes); err != nil {
		return storage.CampaignActionOutcome{}, worldactions.Rejected("%s", err)
	}
	if err := worldactions.CheckTimeAdvance(execution, minutes, 2); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	baseChance := worldactions.DefinitionIntDefault(current.Content["breakthrough_chance"], 65)
	legacyChance := worldactions.ClampInt64(baseChance+int64((character.FoundationQuality-50)/2), 5, 95)
	rollResult, err := worldactions.ResolveCampaignRoll(ctx, execution, campaign.CampaignRollRequest{
		Key: "cultivation.breakthrough." + character.RealmKey, Title: "境界突破",
		Description: fmt.Sprintf("从%s突破至%s", current.Name, next.Name), Category: "cultivation",
		BaseSuccessRate: int(baseChance), Sides: 100, RelevantAttributes: []string{"根骨"},
		AttributeWeights: map[string]float64{"根骨": 1}, PlayerState: campaignRollPlayerState(character, 50),
		Context: campaignRollContext(character),
	})
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	roll, chance, success := worldactions.DeterministicPercent(execution, "breakthrough:"+character.RealmKey), legacyChance, false
	if rollResult != nil {
		roll, chance, success = int64(rollResult.Rolled), int64(rollResult.SuccessRate), rollResult.Success
	} else {
		success = roll <= chance
	}
	now := time.Now()
	payload := storage.JSONMap{
		"from_realm_key": character.RealmKey, "to_realm_key": nextKey,
		"chance": chance, "roll": roll, "success": success,
	}
	if success {
		required := worldactions.DefinitionIntDefault(next.Content["cultivation_required"], character.CultivationRequired*2)
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
		character.LifespanDays = maxInt64(character.LifespanDays, worldactions.DefinitionIntDefault(next.Content["lifespan_days"], character.LifespanDays))
		growCharacterBaseStats(character, next.Content)
		payload["realm_rank"] = nextRank
	} else {
		loss := character.CultivationRequired * worldactions.DefinitionIntDefault(current.Content["failure_progress_loss_percent"], 20) / 100
		damage := worldactions.DefinitionIntDefault(current.Content["failure_health_damage"], maxInt64(1, character.MaxHealth/10))
		character.CultivationProgress = maxInt64(0, character.CultivationProgress-loss)
		character.Health = maxInt64(1, character.Health-damage)
		if character.Injuries == nil {
			character.Injuries = storage.JSONMap{}
		}
		character.Injuries["breakthrough_backlash"] = map[string]any{
			"category": "injury", "severity": worldactions.ClampInt64(damage, 1, 100),
			"description":          "突破失败造成的反噬，需通过休整恢复",
			"applied_at_game_time": execution.Campaign.GameTime + minutes, "duration_minutes": int64(0),
		}
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
	if err := cultivationStore(execution).SaveCharacter(character); err != nil {
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
	worldactions.AttachCampaignRoll(&outcome, rollResult)
	appendLifespanResolution(&outcome, character, exhausted)
	return outcome, nil
}

func learnTechnique(_ context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	if err := worldactions.AllowArguments(arguments, "technique_key"); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	key, err := worldactions.RequiredKey(arguments, "technique_key")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if _, err := cultivationStore(execution).Technique(execution.Campaign.ID, key); err == nil {
		return storage.CampaignActionOutcome{}, worldactions.Rejected("已经掌握功法 %s", key)
	} else if !errors.Is(err, storage.ErrNotFound) {
		return storage.CampaignActionOutcome{}, err
	}
	definition, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionTechnique, key)
	if err != nil {
		return storage.CampaignActionOutcome{}, worldactions.Rejected("未知功法 %s", key)
	}
	character, err := cultivationStore(execution).Character(execution.Campaign.ID)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	minimumRank := worldactions.DefinitionIntDefault(definition.Content["minimum_realm_rank"], 0)
	if int64(character.RealmRank) < minimumRank {
		return storage.CampaignActionOutcome{}, worldactions.Rejected("境界不足，学习 %s 需要 rank %d", definition.Name, minimumRank)
	}
	var consumedItem string
	if sourceItem, _ := definition.Content["source_item_key"].(string); sourceItem != "" {
		stack, stackErr := execution.Store.ItemStack(execution.Campaign.ID, sourceItem, "common")
		if stackErr != nil || stack.Quantity < 1 {
			return storage.CampaignActionOutcome{}, worldactions.Rejected("缺少功法传承物品 %s", sourceItem)
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
	if err := cultivationStore(execution).CreateTechnique(row); err != nil {
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
	if err := worldactions.AllowArguments(arguments, "technique_key", "slot"); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	key, err := worldactions.RequiredKey(arguments, "technique_key")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	row, err := cultivationStore(execution).Technique(execution.Campaign.ID, key)
	if err != nil || row.Status != "active" {
		return storage.CampaignActionOutcome{}, worldactions.Rejected("功法不可装配: %s", key)
	}
	definition, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionTechnique, key)
	if err != nil {
		return storage.CampaignActionOutcome{}, worldactions.Rejected("功法定义不存在: %s", key)
	}
	allowed := definitionStrings(definition.Content["allowed_slots"])
	if len(allowed) == 0 {
		return storage.CampaignActionOutcome{}, worldactions.Invalid("功法 %s 没有可用的槽位定义", key)
	}
	slot, _ := arguments["slot"].(string)
	if slot == "" {
		// 玩家界面只传 technique_key；优先装入空闲的合法槽位，
		// 全部占用时替换第一个合法槽位，仍由领域层统一重算属性。
		rows, listErr := cultivationStore(execution).Techniques(execution.Campaign.ID)
		if listErr != nil {
			return storage.CampaignActionOutcome{}, listErr
		}
		occupied := map[string]bool{}
		for _, other := range rows {
			if other.ID != row.ID && other.Status == "active" && other.EquippedSlot != nil {
				occupied[*other.EquippedSlot] = true
			}
		}
		for _, candidate := range allowed {
			if !occupied[candidate] {
				slot = candidate
				break
			}
		}
		if slot == "" {
			slot = allowed[0]
		}
	}
	if !containsActionString(allowed, slot) {
		return storage.CampaignActionOutcome{}, worldactions.Rejected("功法 %s 不能装配到 %s", key, slot)
	}
	if err := cultivationStore(execution).ClearTechniqueSlot(execution.Campaign.ID, slot, row.ID); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	row.EquippedSlot = &slot
	row.UpdatedAt = time.Now()
	if err := cultivationStore(execution).SaveTechnique(row); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	character, err := cultivationStore(execution).Character(execution.Campaign.ID)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	effects, err := recalculateCharacterStats(execution, character)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if err := cultivationStore(execution).SaveCharacter(character); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	return worldactions.SingleEvent("technique.equipped", storage.JSONMap{"technique_key": key, "slot": slot, "active_effects": effects}), nil
}

func unequipTechnique(_ context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	if err := worldactions.AllowArguments(arguments, "technique_key"); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	key, err := worldactions.RequiredKey(arguments, "technique_key")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	row, err := cultivationStore(execution).Technique(execution.Campaign.ID, key)
	if err != nil || row.EquippedSlot == nil {
		return storage.CampaignActionOutcome{}, worldactions.Rejected("功法当前未装配: %s", key)
	}
	slot := *row.EquippedSlot
	row.EquippedSlot = nil
	row.UpdatedAt = time.Now()
	if err := cultivationStore(execution).SaveTechnique(row); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	character, err := cultivationStore(execution).Character(execution.Campaign.ID)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	effects, err := recalculateCharacterStats(execution, character)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if err := cultivationStore(execution).SaveCharacter(character); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	return worldactions.SingleEvent("technique.unequipped", storage.JSONMap{"technique_key": key, "slot": slot, "active_effects": effects}), nil
}

func applyCondition(_ context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	if err := worldactions.RequireModelAuthority(execution); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if err := worldactions.AllowArguments(arguments, "category", "effect_key", "severity", "duration_minutes", "description"); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	category, err := conditionCategory(arguments)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	key, err := worldactions.RequiredKey(arguments, "effect_key")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if execution.ActorType == "model" && key == "breakthrough_backlash" {
		return storage.CampaignActionOutcome{}, worldactions.Rejected("突破反噬由规则结算，不能用叙事状态覆盖")
	}
	severity, err := worldactions.RequiredInt(arguments, "severity", 1, 100)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	duration, err := worldactions.OptionalInt(arguments, "duration_minutes", 0, 0, 5256000)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	description, err := worldactions.OptionalText(arguments, "description", 256)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	character, err := cultivationStore(execution).Character(execution.Campaign.ID)
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
	if err := cultivationStore(execution).SaveCharacter(character); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	return worldactions.SingleEvent("character.condition_applied", storage.JSONMap{
		"category": category, "effect_key": key, "severity": severity, "duration_minutes": duration,
	}), nil
}

func resolveCondition(_ context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	if err := worldactions.RequireModelAuthority(execution); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if err := worldactions.AllowArguments(arguments, "category", "effect_key"); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	category, err := conditionCategory(arguments)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	key, err := worldactions.RequiredKey(arguments, "effect_key")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if execution.ActorType == "model" && key == "breakthrough_backlash" {
		return storage.CampaignActionOutcome{}, worldactions.Rejected("突破反噬需要通过休整恢复")
	}
	character, err := cultivationStore(execution).Character(execution.Campaign.ID)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	target := character.LongTermEffects
	if category == "injury" {
		target = character.Injuries
	}
	if _, exists := target[key]; !exists {
		return storage.CampaignActionOutcome{}, worldactions.Rejected("状态不存在: %s", key)
	}
	delete(target, key)
	character.UpdatedAt = time.Now()
	if err := cultivationStore(execution).SaveCharacter(character); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	return worldactions.SingleEvent("character.condition_resolved", storage.JSONMap{"category": category, "effect_key": key}), nil
}

func adjustVitals(_ context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	if err := worldactions.RequireModelAuthority(execution); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if err := worldactions.AllowArguments(arguments, "health_delta", "mana_delta", "spiritual_sense_delta", "cause_key"); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	cause, err := worldactions.RequiredKey(arguments, "cause_key")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	healthDelta, err := worldactions.OptionalInt(arguments, "health_delta", 0, -10000, 10000)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	manaDelta, err := worldactions.OptionalInt(arguments, "mana_delta", 0, -10000, 10000)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	senseDelta, err := worldactions.OptionalInt(arguments, "spiritual_sense_delta", 0, -10000, 10000)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if healthDelta == 0 && manaDelta == 0 && senseDelta == 0 {
		return storage.CampaignActionOutcome{}, worldactions.Invalid("至少需要一项非零生命资源变化")
	}
	if execution.ActorType == "model" && (healthDelta > 0 || manaDelta > 0 || senseDelta > 0) {
		return storage.CampaignActionOutcome{}, worldactions.Rejected("恢复必须使用有明确代价的休整方案或消耗品")
	}
	character, err := cultivationStore(execution).Character(execution.Campaign.ID)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	character.Health = worldactions.ClampInt64(character.Health+healthDelta, 0, character.MaxHealth)
	character.Mana = worldactions.ClampInt64(character.Mana+manaDelta, 0, character.MaxMana)
	character.SpiritualSense = worldactions.ClampInt64(character.SpiritualSense+senseDelta, 0, character.MaxSpiritualSense)
	character.UpdatedAt = time.Now()
	if err := cultivationStore(execution).SaveCharacter(character); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	payload := storage.JSONMap{
		"cause_key": cause, "health": character.Health, "max_health": character.MaxHealth,
		"mana": character.Mana, "max_mana": character.MaxMana,
		"spiritual_sense": character.SpiritualSense, "max_spiritual_sense": character.MaxSpiritualSense,
	}
	outcome := worldactions.SingleEvent("character.vitals_changed", payload)
	if character.Health == 0 {
		status := storage.CampaignStatusDead
		key := "perished"
		outcome.Status = &status
		outcome.OutcomeKey = &key
		outcome.Events = append(outcome.Events, storage.CampaignEventDraft{
			EventType: "character.died", Visibility: storage.CampaignVisibilityPlayer,
			Payload: storage.JSONMap{"cause_key": cause, "outcome_key": key, "game_time": execution.Campaign.GameTime},
		})
		outcome.Result["campaign_status"] = status
		outcome.Result["outcome_key"] = key
	}
	return outcome, nil
}

func useItem(_ context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	if err := worldactions.AllowArguments(arguments, "item_key", "quality_key", "quantity"); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	key, err := worldactions.RequiredKey(arguments, "item_key")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	quality, err := worldactions.OptionalKey(arguments, "quality_key", "common")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	quantity, err := worldactions.RequiredInt(arguments, "quantity", 1, 20)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	definition, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionItem, key)
	if err != nil {
		return storage.CampaignActionOutcome{}, worldactions.Rejected("未知物品 %s", key)
	}
	consumable, _ := definition.Content["consumable"].(bool)
	if !consumable {
		return storage.CampaignActionOutcome{}, worldactions.Rejected("物品 %s 不可直接使用", key)
	}
	stack, err := execution.Store.ItemStack(execution.Campaign.ID, key, quality)
	if err != nil || stack.Quantity < quantity {
		return storage.CampaignActionOutcome{}, worldactions.Rejected("物品数量不足: %s", key)
	}
	character, err := cultivationStore(execution).Character(execution.Campaign.ID)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	effects, _ := definition.Content["effects"].(map[string]any)
	activeEffects, err := recalculateCharacterStats(execution, character)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	applied := storage.JSONMap{}
	for field, raw := range effects {
		amount, ok := worldactions.ExactActionInt(raw)
		if !ok {
			return storage.CampaignActionOutcome{}, worldactions.Invalid("物品 %s 的效果 %s 不是整数", key, field)
		}
		amount *= quantity
		switch field {
		case "health":
			amount = effectrules.HealingAmount(amount, worldactions.DefinitionIntDefault(activeEffects["healing_power"], 0))
			character.Health = worldactions.ClampInt64(character.Health+amount, 0, character.MaxHealth)
			applied[field] = character.Health
		case "mana":
			character.Mana = worldactions.ClampInt64(character.Mana+amount, 0, character.MaxMana)
			applied[field] = character.Mana
		case "spiritual_sense":
			character.SpiritualSense = worldactions.ClampInt64(character.SpiritualSense+amount, 0, character.MaxSpiritualSense)
			applied[field] = character.SpiritualSense
		case "cultivation_progress":
			character.CultivationProgress = worldactions.ClampInt64(character.CultivationProgress+amount, 0, character.CultivationRequired*2)
			applied[field] = character.CultivationProgress
		default:
			return storage.CampaignActionOutcome{}, worldactions.Invalid("物品 %s 声明了不受信任的效果 %s", key, field)
		}
	}
	stack.Quantity -= quantity
	stack.UpdatedAt = time.Now()
	character.UpdatedAt = stack.UpdatedAt
	if err := execution.Store.SaveItemStack(stack); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if err := cultivationStore(execution).SaveCharacter(character); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	return worldactions.SingleEvent("inventory.consumed", storage.JSONMap{
		"item_key": key, "quality_key": quality, "quantity": quantity,
		"quantity_after": stack.Quantity, "applied": applied,
	}), nil
}

func gainEquipment(_ context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	if err := worldactions.RequireModelAuthority(execution); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if err := worldactions.AllowArguments(arguments, "definition_key", "quality_key", "source"); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	key, err := worldactions.RequiredKey(arguments, "definition_key")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	quality, err := worldactions.OptionalKey(arguments, "quality_key", "common")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	source, err := worldactions.OptionalKey(arguments, "source", "narrative.reward")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	definition, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionEquipment, key)
	if err != nil {
		return storage.CampaignActionOutcome{}, worldactions.Rejected("未知装备 %s", key)
	}
	if err := worldactions.RequireDefinitionGrantAuthority(execution, definition); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	durability := worldactions.DefinitionIntDefault(definition.Content["durability_max"], 100)
	if durability < 1 || durability > math.MaxInt32 {
		return storage.CampaignActionOutcome{}, worldactions.Invalid("装备耐久定义无效")
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
	return worldactions.SingleEvent("inventory.instance_gained", storage.JSONMap{
		"item_instance_id": row.ID, "definition_key": key, "name": definition.Name, "quality_key": quality,
	}), nil
}

func equipItem(_ context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	if err := worldactions.AllowArguments(arguments, "item_instance_id", "slot"); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	id, err := worldactions.RequiredUUID(arguments, "item_instance_id")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	slot, err := worldactions.RequiredKey(arguments, "slot")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	item, err := execution.Store.ItemInstance(execution.Campaign.ID, id)
	if err != nil || (item.State != "stored" && item.State != "equipped") || item.Durability <= 0 {
		return storage.CampaignActionOutcome{}, worldactions.Rejected("装备实例不可装配")
	}
	definition, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionEquipment, item.DefinitionKey)
	if err != nil || !worldactions.ContainsDefinitionString(definition.Content["allowed_slots"], slot) {
		return storage.CampaignActionOutcome{}, worldactions.Rejected("装备 %s 不能装配到 %s", item.DefinitionKey, slot)
	}
	character, err := cultivationStore(execution).Character(execution.Campaign.ID)
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
	if err := cultivationStore(execution).SaveCharacter(character); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	return worldactions.SingleEvent("equipment.changed", storage.JSONMap{
		"operation": "equip", "item_instance_id": item.ID, "definition_key": item.DefinitionKey,
		"slot": slot, "active_effects": effects,
	}), nil
}

func unequipItem(_ context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	if err := worldactions.AllowArguments(arguments, "item_instance_id"); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	id, err := worldactions.RequiredUUID(arguments, "item_instance_id")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	item, err := execution.Store.ItemInstance(execution.Campaign.ID, id)
	if err != nil || item.State != "equipped" || item.EquippedSlot == nil {
		return storage.CampaignActionOutcome{}, worldactions.Rejected("物品当前未装备")
	}
	slot := *item.EquippedSlot
	item.State = "stored"
	item.EquippedSlot = nil
	item.UpdatedAt = time.Now()
	if err := execution.Store.SaveItemInstance(item); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	character, err := cultivationStore(execution).Character(execution.Campaign.ID)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	effects, err := recalculateCharacterStats(execution, character)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if err := cultivationStore(execution).SaveCharacter(character); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	return worldactions.SingleEvent("equipment.changed", storage.JSONMap{
		"operation": "unequip", "item_instance_id": item.ID, "definition_key": item.DefinitionKey,
		"slot": slot, "active_effects": effects,
	}), nil
}

func gather(ctx context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	if err := worldactions.AllowArguments(arguments, "item_key"); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	itemKey := ""
	if raw, exists := arguments["item_key"]; exists {
		value, ok := raw.(string)
		if !ok || !worldactions.StableActionKey(value) {
			return storage.CampaignActionOutcome{}, worldactions.Invalid("item_key 必须是稳定 key")
		}
		itemKey = value
	}
	location, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionLocation, execution.Campaign.CurrentLocationKey)
	if err != nil {
		return storage.CampaignActionOutcome{}, worldactions.Rejected("当前位置定义不存在")
	}
	table, ok := gatheringrules.Parse(location.Content["gathering"])
	if !ok {
		return storage.CampaignActionOutcome{}, worldactions.Rejected("当前位置没有可采集资源")
	}
	entries := gatheringrules.MatchingEntries(table, itemKey)
	if len(entries) == 0 {
		return storage.CampaignActionOutcome{}, worldactions.Rejected("当前位置不能采集物品 %s", itemKey)
	}
	entry, ok := gatheringrules.Choose(entries, worldactions.DeterministicUint64(execution, "gather.resource:"+itemKey))
	if !ok {
		return storage.CampaignActionOutcome{}, worldactions.Invalid("当前位置资源表无效")
	}
	quantity := gatheringrules.Quantity(entry, worldactions.DeterministicUint64(execution, "gather.quantity:"+entry.ItemKey+":"+entry.QualityKey))
	region, err := gatheringRegion(execution, location)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if region.ResourceLevel <= 0 {
		return storage.CampaignActionOutcome{}, worldactions.Rejected("当前区域资源已经枯竭，需等待世界变化")
	}
	if region.ResourceLevel < 5000 {
		quantity = maxInt64(1, quantity*region.ResourceLevel/5000)
	}
	definition, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionItem, entry.ItemKey)
	if err != nil {
		return storage.CampaignActionOutcome{}, worldactions.Invalid("采集资源定义不存在: %s", entry.ItemKey)
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
		return storage.CampaignActionOutcome{}, worldactions.Invalid("采集数量溢出")
	}
	character, err := cultivationStore(execution).Character(execution.Campaign.ID)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	exhausted := advanceCharacterAge(character, execution.Campaign.GameTime, table.Minutes)
	riskDamage := int64(0)
	var rollResult *campaign.CampaignRollResult
	if table.RiskChanceBPS > 0 {
		rollResult, err = worldactions.ResolveCampaignRoll(ctx, execution, campaign.CampaignRollRequest{
			Key: "gather.risk." + location.Key, Title: "采集风险", Description: "在" + location.Name + "采集" + definition.Name,
			Category: "exploration", BaseSuccessRate: campaignRollBaseFromBPS(10_000 - table.RiskChanceBPS), Sides: 100,
			RelevantAttributes: []string{"根骨", "气运"}, AttributeWeights: map[string]float64{"根骨": 0.6, "气运": 0.4},
			PlayerState: campaignRollPlayerState(character, campaignCharacterInsight(character)), Context: campaignRollContext(character),
		})
		if err != nil {
			return storage.CampaignActionOutcome{}, err
		}
		avoided := int64(worldactions.DeterministicUint64(execution, "gather.risk:"+entry.ItemKey)%10_000) >= table.RiskChanceBPS
		if rollResult != nil {
			avoided = rollResult.Success
		}
		if !avoided {
			riskDamage = minInt64(table.RiskHealthDamage, character.Health)
			character.Health -= riskDamage
		}
	}
	character.UpdatedAt = time.Now()
	if err := cultivationStore(execution).SaveCharacter(character); err != nil {
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
	worldactions.AttachCampaignRoll(&outcome, rollResult)
	if character.Health == 0 {
		status := storage.CampaignStatusDead
		key := "perished"
		outcome.Status = &status
		outcome.OutcomeKey = &key
		outcome.Events = append(outcome.Events, storage.CampaignEventDraft{
			EventType: "character.died", Visibility: storage.CampaignVisibilityPlayer,
			Payload: storage.JSONMap{"cause_key": "gathering-risk", "outcome_key": key, "game_time": execution.Campaign.GameTime + table.Minutes},
		})
		outcome.Result["campaign_status"] = status
		outcome.Result["outcome_key"] = key
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
			return nil, worldactions.Invalid("采集地点的区域层级无效")
		}
		current = parent
	}
	return nil, worldactions.Invalid("采集地点不属于任何区域")
}

func missingEntryPermissions(execution campaign.ActionExecution, content storage.JSONMap) ([]string, error) {
	requirements, ok := factionrules.EntryRequirements(content)
	if !ok {
		return nil, worldactions.Invalid("目标地点的 entry_requirements 定义无效")
	}
	standings := map[string]*storage.CampaignFactionStanding{}
	missing := make([]string, 0)
	var character *storage.CampaignCharacter
	for _, requirement := range requirements {
		if requirement.MinRealmRank > 0 {
			if character == nil {
				var err error
				character, err = cultivationStore(execution).Character(execution.Campaign.ID)
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

func conditionCategory(arguments map[string]any) (string, error) {
	value, ok := arguments["category"].(string)
	if !ok || !map[string]bool{"injury": true, "poison": true, "inner_demon": true, "long_term": true}[value] {
		return "", worldactions.Invalid("category 必须是 injury、poison、inner_demon 或 long_term")
	}
	return value, nil
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
	character.BaseMaxHealth = maxInt64(character.BaseMaxHealth, worldactions.DefinitionIntDefault(content["max_health"], character.BaseMaxHealth))
	character.BaseMaxMana = maxInt64(character.BaseMaxMana, worldactions.DefinitionIntDefault(content["max_mana"], character.BaseMaxMana))
	character.BaseMaxSpiritualSense = maxInt64(character.BaseMaxSpiritualSense, worldactions.DefinitionIntDefault(content["max_spiritual_sense"], character.BaseMaxSpiritualSense))
}

func recalculateCharacterStats(execution campaign.ActionExecution, character *storage.CampaignCharacter) (storage.JSONMap, error) {
	normalizeCharacterBaseStats(character)
	effects, err := equippedEffects(execution)
	if err != nil {
		return nil, err
	}
	character.MaxHealth = maxInt64(1, character.BaseMaxHealth+effects["max_health"])
	character.MaxMana = maxInt64(1, character.BaseMaxMana+effects["max_mana"])
	character.MaxSpiritualSense = maxInt64(1, character.BaseMaxSpiritualSense+effects["max_spiritual_sense"])
	character.Health = worldactions.ClampInt64(character.Health, 0, character.MaxHealth)
	character.Mana = worldactions.ClampInt64(character.Mana, 0, character.MaxMana)
	character.SpiritualSense = worldactions.ClampInt64(character.SpiritualSense, 0, character.MaxSpiritualSense)
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
	character, err := cultivationStore(execution).Character(execution.Campaign.ID)
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
			appliedAt, appliedOK := worldactions.ExactActionInt(condition["applied_at_game_time"])
			duration, durationOK := worldactions.ExactActionInt(condition["duration_minutes"])
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
	return cultivationStore(execution).SaveCharacter(character)
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
	key := "lifespan_exhausted"
	outcome.Status = &status
	outcome.OutcomeKey = &key
	outcome.Events = append(outcome.Events, storage.CampaignEventDraft{
		EventType: "character.lifespan_exhausted", Visibility: storage.CampaignVisibilityPlayer,
		Payload: storage.JSONMap{"age_days": character.AgeDays, "lifespan_days": character.LifespanDays, "outcome_key": key},
	})
	outcome.Result["campaign_status"] = status
	outcome.Result["outcome_key"] = key
}
