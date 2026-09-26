package campaignactions

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"math"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/campaign"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/factionrules"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/worldrules"
	"github.com/ialtone/fatecraft/internal/storage"
)

func acceptQuest(_ context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	if err := allowArguments(arguments, "quest_key"); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	key, err := requiredKey(arguments, "quest_key")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	definition, parsed, err := questDefinition(execution, key)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	row, err := execution.Store.Quest(execution.Campaign.ID, key)
	if err != nil || row.Status != "offered" {
		return storage.CampaignActionOutcome{}, rejected("任务 %s 当前不可接受", key)
	}
	if len(parsed.AcceptLocationKeys) > 0 && !containsStringValue(parsed.AcceptLocationKeys, execution.Campaign.CurrentLocationKey) {
		return storage.CampaignActionOutcome{}, rejected("任务 %s 不能在当前位置接受", key)
	}
	now := execution.Campaign.GameTime
	row.Status = "active"
	row.AcceptedAtGameTime = &now
	row.Progress = storage.JSONMap{}
	row.UpdatedAt = time.Now()
	if parsed.DeadlineMinutes > 0 {
		if now > math.MaxInt64-parsed.DeadlineMinutes {
			return storage.CampaignActionOutcome{}, invalid("任务截止时间溢出")
		}
		due := now + parsed.DeadlineMinutes
		row.DueGameTime = &due
		if err := execution.Store.CreateScheduledEvent(&storage.CampaignScheduledEvent{
			ID: uuid.New(), CampaignID: execution.Campaign.ID, EventKind: "quest_deadline",
			TemplateKey: key, DueGameTime: due, Priority: 1000, Status: "pending", Occurrence: 0,
			Scope: storage.JSONMap{}, Payload: storage.JSONMap{"quest_key": key}, CreatedAt: time.Now(), UpdatedAt: time.Now(),
		}); err != nil {
			return storage.CampaignActionOutcome{}, err
		}
	}
	if err := execution.Store.SaveQuest(row); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	return storage.CampaignActionOutcome{
		Events: []storage.CampaignEventDraft{{EventType: "quest.state_changed", Visibility: storage.CampaignVisibilityPlayer, Payload: storage.JSONMap{
			"quest_key": key, "name": definition.Name, "quest_type": row.QuestType, "from": "offered", "to": "active", "due_game_time": row.DueGameTime,
		}}},
		Result: storage.JSONMap{"quest_key": key, "status": "active", "due_game_time": row.DueGameTime},
	}, nil
}

func submitQuest(ctx context.Context, execution campaign.ActionExecution, arguments map[string]any) (storage.CampaignActionOutcome, error) {
	if err := allowArguments(arguments, "quest_key"); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	key, err := requiredKey(arguments, "quest_key")
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	definition, parsed, err := questDefinition(execution, key)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	row, err := execution.Store.Quest(execution.Campaign.ID, key)
	if err != nil || row.Status != "active" && row.Status != "blocked" {
		return storage.CampaignActionOutcome{}, rejected("任务 %s 当前不可提交", key)
	}
	if row.DueGameTime != nil && *row.DueGameTime <= execution.Campaign.GameTime {
		return storage.CampaignActionOutcome{}, rejected("任务 %s 已到截止时间，需先结算到期事件", key)
	}
	progress, satisfied, err := evaluateQuestObjectives(execution, parsed)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	from := row.Status
	row.Progress = progress
	row.UpdatedAt = time.Now()
	if !satisfied {
		row.Status = "blocked"
		if err := execution.Store.SaveQuest(row); err != nil {
			return storage.CampaignActionOutcome{}, err
		}
		return storage.CampaignActionOutcome{
			Events: []storage.CampaignEventDraft{{EventType: "quest.state_changed", Visibility: storage.CampaignVisibilityPlayer, Payload: storage.JSONMap{
				"quest_key": key, "name": definition.Name, "quest_type": row.QuestType, "from": from, "to": "blocked", "progress": progress,
			}}},
			Result: storage.JSONMap{"quest_key": key, "status": "blocked", "progress": progress},
		}, nil
	}
	if err := consumeQuestObjectives(execution, parsed); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if err := grantQuestRewards(execution, parsed); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	now := execution.Campaign.GameTime
	row.Status = "completed"
	row.ResolvedAtGameTime = &now
	if err := execution.Store.SaveQuest(row); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	if err := execution.Store.CancelScheduledEvents(execution.Campaign.ID, "quest_deadline", key); err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	unlockedEvents, unlockedKeys, err := offerUnlockedQuests(execution, parsed)
	if err != nil {
		return storage.CampaignActionOutcome{}, err
	}
	events := []storage.CampaignEventDraft{{EventType: "quest.state_changed", Visibility: storage.CampaignVisibilityPlayer, Payload: storage.JSONMap{
		"quest_key": key, "name": definition.Name, "quest_type": row.QuestType, "from": from, "to": "completed", "progress": progress,
	}}}
	events = append(events, unlockedEvents...)
	result := storage.JSONMap{"quest_key": key, "status": "completed", "progress": progress}
	if len(unlockedKeys) > 0 {
		result["unlocked_quest_keys"] = unlockedKeys
	}
	outcome := storage.CampaignActionOutcome{Events: events, Result: result}
	if parsed.MilestoneKey != "" {
		outcome.Events = append(outcome.Events, storage.CampaignEventDraft{
			EventType: "campaign.milestone_reached", Visibility: storage.CampaignVisibilityPlayer,
			Payload: storage.JSONMap{"milestone_key": parsed.MilestoneKey, "quest_key": key, "game_time": execution.Campaign.GameTime},
		})
		result["milestone_key"] = parsed.MilestoneKey
	}
	if parsed.CampaignOutcomeKey != "" {
		rewards, err := execution.Store.GrantCampaignOutcomeRewards(ctx, execution.Campaign, parsed.CampaignOutcomeKey)
		if err != nil {
			return storage.CampaignActionOutcome{}, err
		}
		status := storage.CampaignStatusCompleted
		outcome.Status = &status
		outcome.OutcomeKey = &parsed.CampaignOutcomeKey
		result["campaign_status"] = status
		result["campaign_outcome_key"] = parsed.CampaignOutcomeKey
		result["account_rewards"] = rewards
		completedPayload := storage.JSONMap{
			"outcome_key": parsed.CampaignOutcomeKey, "milestone_key": parsed.MilestoneKey,
			"quest_key": key, "account_rewards": rewards,
		}
		// client 路由不发放奖励：原因随结果与事件下发，前端据此展示替代文案。
		if execution.Campaign.LLMRoute == storage.LLMRouteClient {
			result["rewards_disabled_reason"] = storage.RewardsDisabledReasonClientRoute
			completedPayload["rewards_disabled_reason"] = storage.RewardsDisabledReasonClientRoute
		}
		outcome.Events = append(outcome.Events, storage.CampaignEventDraft{
			EventType: "campaign.completed", Visibility: storage.CampaignVisibilityPlayer,
			Payload: completedPayload,
		})
	}
	return outcome, nil
}

func offerUnlockedQuests(execution campaign.ActionExecution, completed worldrules.Quest) ([]storage.CampaignEventDraft, []string, error) {
	events := make([]storage.CampaignEventDraft, 0, len(completed.UnlockQuestKeys))
	keys := make([]string, 0, len(completed.UnlockQuestKeys))
	for _, key := range completed.UnlockQuestKeys {
		if _, err := execution.Store.Quest(execution.Campaign.ID, key); err == nil {
			continue
		} else if !errors.Is(err, storage.ErrNotFound) {
			return nil, nil, err
		}
		definition, quest, err := questDefinition(execution, key)
		if err != nil {
			return nil, nil, err
		}
		if quest.Initial || quest.StartingStatus != "offered" {
			return nil, nil, invalid("解锁任务必须是非初始 offered 任务: %s", key)
		}
		row := &storage.CampaignQuest{
			CampaignID: execution.Campaign.ID, QuestKey: key, QuestType: quest.QuestType,
			Status: "offered", OfferedAtGameTime: execution.Campaign.GameTime,
			Progress: storage.JSONMap{}, SourceSnapshot: storage.JSONMap{"key": key, "name": definition.Name}, UpdatedAt: time.Now(),
		}
		if err := execution.Store.SaveQuest(row); err != nil {
			return nil, nil, err
		}
		keys = append(keys, key)
		events = append(events, storage.CampaignEventDraft{EventType: "quest.offered", Visibility: storage.CampaignVisibilityPlayer, Payload: storage.JSONMap{
			"quest_key": key, "name": definition.Name, "quest_type": quest.QuestType, "status": "offered",
		}})
	}
	return events, keys, nil
}

func questDefinition(execution campaign.ActionExecution, key string) (*storage.GameplayDefinition, worldrules.Quest, error) {
	definition, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionQuest, key)
	if err != nil {
		return nil, worldrules.Quest{}, rejected("任务定义不存在: %s", key)
	}
	parsed, ok := worldrules.ParseQuest(map[string]any(definition.Content))
	if !ok {
		return nil, worldrules.Quest{}, invalid("任务定义无效: %s", key)
	}
	return definition, parsed, nil
}

func evaluateQuestObjectives(execution campaign.ActionExecution, quest worldrules.Quest) (storage.JSONMap, bool, error) {
	entries := make([]any, 0, len(quest.Objectives))
	allSatisfied := true
	for _, objective := range quest.Objectives {
		current, target, satisfied, err := evaluateQuestObjective(execution, objective)
		if err != nil {
			return nil, false, err
		}
		entries = append(entries, storage.JSONMap{
			"key": objective.Key, "type": objective.Type, "current": current, "target": target,
			"comparison": objective.Comparison, "satisfied": satisfied,
		})
		allSatisfied = allSatisfied && satisfied
	}
	return storage.JSONMap{"objectives": entries, "satisfied": allSatisfied}, allSatisfied, nil
}

func evaluateQuestObjective(execution campaign.ActionExecution, objective worldrules.QuestObjective) (int64, int64, bool, error) {
	switch objective.Type {
	case "item":
		quality := objective.QualityKey
		if quality == "" {
			quality = "common"
		}
		stack, err := execution.Store.ItemStack(execution.Campaign.ID, objective.ItemKey, quality)
		if errors.Is(err, storage.ErrNotFound) {
			return 0, objective.Quantity, false, nil
		}
		if err != nil {
			return 0, 0, false, err
		}
		return stack.Quantity, objective.Quantity, stack.Quantity >= objective.Quantity, nil
	case "encounter_win":
		count, err := execution.Store.EncounterWinCount(execution.Campaign.ID, objective.EncounterKey)
		if err != nil {
			return 0, 0, false, err
		}
		return count, objective.Quantity, count >= objective.Quantity, nil
	case "location":
		current := int64(0)
		if execution.Campaign.CurrentLocationKey == objective.LocationKey {
			current = 1
		}
		return current, 1, current == 1, nil
	case "faction_reputation":
		standing, err := execution.Store.FactionStanding(execution.Campaign.ID, objective.FactionKey)
		if err != nil {
			return 0, 0, false, err
		}
		return int64(standing.Reputation), objective.Target, int64(standing.Reputation) >= objective.Target, nil
	case "npc_relationship":
		npc, err := execution.Store.NPC(execution.Campaign.ID, objective.NPCKey)
		if err != nil {
			return 0, 0, false, err
		}
		relation, err := execution.Store.Relationship(execution.Campaign.ID, npc.ID)
		if errors.Is(err, storage.ErrNotFound) {
			return 0, objective.Target, 0 >= objective.Target, nil
		}
		if err != nil {
			return 0, 0, false, err
		}
		current := relationshipDimension(*relation, objective.Dimension)
		return current, objective.Target, current >= objective.Target, nil
	case "region_state":
		region, err := execution.Store.RegionState(execution.Campaign.ID, objective.RegionKey)
		if err != nil {
			return 0, 0, false, err
		}
		current := regionStateField(*region, objective.RegionField)
		satisfied := current >= objective.Target
		if objective.Comparison == "at_most" {
			satisfied = current <= objective.Target
		}
		return current, objective.Target, satisfied, nil
	default:
		return 0, 0, false, invalid("任务目标类型无效")
	}
}

func consumeQuestObjectives(execution campaign.ActionExecution, quest worldrules.Quest) error {
	for _, objective := range quest.Objectives {
		if objective.Type != "item" || !objective.Consume {
			continue
		}
		quality := objective.QualityKey
		if quality == "" {
			quality = "common"
		}
		stack, err := execution.Store.ItemStack(execution.Campaign.ID, objective.ItemKey, quality)
		if err != nil || stack.Quantity < objective.Quantity {
			return rejected("提交任务时物品不足: %s", objective.ItemKey)
		}
		stack.Quantity -= objective.Quantity
		stack.UpdatedAt = time.Now()
		if err := execution.Store.SaveItemStack(stack); err != nil {
			return err
		}
	}
	return nil
}

func grantQuestRewards(execution campaign.ActionExecution, quest worldrules.Quest) error {
	for _, reward := range quest.Rewards {
		switch reward.Type {
		case "item":
			quality := reward.QualityKey
			if quality == "" {
				quality = "common"
			}
			stack, err := execution.Store.ItemStack(execution.Campaign.ID, reward.ItemKey, quality)
			if errors.Is(err, storage.ErrNotFound) {
				stack = &storage.CampaignItemStack{CampaignID: execution.Campaign.ID, ItemKey: reward.ItemKey, QualityKey: quality, Metadata: storage.JSONMap{}}
			} else if err != nil {
				return err
			}
			if stack.Quantity > math.MaxInt64-reward.Quantity {
				return invalid("任务奖励数量溢出")
			}
			stack.Quantity += reward.Quantity
			stack.UpdatedAt = time.Now()
			if err := execution.Store.SaveItemStack(stack); err != nil {
				return err
			}
		case "faction_reputation", "faction_contribution":
			standing, err := execution.Store.FactionStanding(execution.Campaign.ID, reward.FactionKey)
			if err != nil {
				return err
			}
			faction, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionFaction, reward.FactionKey)
			if err != nil {
				return err
			}
			if reward.Type == "faction_reputation" {
				standing.Reputation = clampInt(standing.Reputation+int(reward.Amount), factionrules.MinimumReputation, factionrules.MaximumReputation)
			} else {
				standing.Contribution = clamp64(standing.Contribution+reward.Amount, 0, factionrules.MaximumContribution)
			}
			standing.PositionKey = factionrules.Position(faction.Content, standing.Contribution)
			standing.Permissions = storage.JSONMap(factionrules.DerivePermissions(faction.Content, standing.Reputation))
			standing.UpdatedAt = time.Now()
			if err := execution.Store.SaveFactionStanding(standing); err != nil {
				return err
			}
		}
	}
	return nil
}

func processDueScheduledEvents(execution campaign.ActionExecution, outcome *storage.CampaignActionOutcome) error {
	if outcome == nil || execution.MaxDueEventsPerTurn < 1 {
		return nil
	}
	capacity := 64 - len(outcome.Events)
	if capacity > execution.MaxDueEventsPerTurn {
		capacity = execution.MaxDueEventsPerTurn
	}
	if capacity < 1 || outcome.GameTimeDelta < 0 || execution.Campaign.GameTime > math.MaxInt64-outcome.GameTimeDelta {
		return nil
	}
	effectiveTime := execution.Campaign.GameTime + outcome.GameTimeDelta
	rows, err := execution.Store.DueScheduledEvents(execution.Campaign.ID, effectiveTime, capacity)
	if err != nil {
		return err
	}
	for index := range rows {
		draft, err := processScheduledEvent(execution, &rows[index], effectiveTime)
		if err != nil {
			return err
		}
		outcome.Events = append(outcome.Events, draft)
	}
	return nil
}

func processScheduledEvent(execution campaign.ActionExecution, scheduled *storage.CampaignScheduledEvent, gameTime int64) (storage.CampaignEventDraft, error) {
	switch scheduled.EventKind {
	case storage.DefinitionWorldEvent:
		return processWorldEvent(execution, scheduled, gameTime)
	case "quest_deadline":
		return processQuestDeadline(execution, scheduled, gameTime)
	default:
		return storage.CampaignEventDraft{}, invalid("未知计划事件类型: %s", scheduled.EventKind)
	}
}

func processWorldEvent(execution campaign.ActionExecution, scheduled *storage.CampaignScheduledEvent, gameTime int64) (storage.CampaignEventDraft, error) {
	definition, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionWorldEvent, scheduled.TemplateKey)
	if err != nil {
		return storage.CampaignEventDraft{}, err
	}
	event, ok := worldrules.ParseWorldEvent(map[string]any(definition.Content))
	if !ok {
		return storage.CampaignEventDraft{}, invalid("世界事件定义无效: %s", scheduled.TemplateKey)
	}
	variant := deterministicVariant(execution.Campaign.RandomSeed, scheduled.TemplateKey, scheduled.Occurrence, event.Variants)
	region, err := execution.Store.RegionState(execution.Campaign.ID, event.RegionKey)
	if err != nil {
		return storage.CampaignEventDraft{}, err
	}
	applyRegionEffects(region, variant.Effects)
	region.LastEventKey, region.LastEventVariant, region.LastEventSummary = scheduled.TemplateKey, variant.Key, variant.Summary
	region.LastEventGameTime = &gameTime
	region.UpdatedAt = time.Now()
	if err := execution.Store.SaveRegionState(region); err != nil {
		return storage.CampaignEventDraft{}, err
	}
	marketKeys := sortedIntMapKeys(variant.Effects.MarketPriceModifierDeltasBPS)
	for _, marketKey := range marketKeys {
		market, err := execution.Store.MarketState(execution.Campaign.ID, marketKey)
		if err != nil {
			return storage.CampaignEventDraft{}, err
		}
		market.PriceModifierBPS = clampInt(market.PriceModifierBPS+int(variant.Effects.MarketPriceModifierDeltasBPS[marketKey]), 1000, 30000)
		market.UpdatedAt = time.Now()
		if err := execution.Store.SaveMarketState(market); err != nil {
			return storage.CampaignEventDraft{}, err
		}
	}
	npcKeys := make([]string, 0, len(variant.Effects.NPCGoalDeltas))
	for npcKey := range variant.Effects.NPCGoalDeltas {
		npcKeys = append(npcKeys, npcKey)
	}
	sort.Strings(npcKeys)
	for _, npcKey := range npcKeys {
		npc, err := execution.Store.NPC(execution.Campaign.ID, npcKey)
		if err != nil {
			return storage.CampaignEventDraft{}, err
		}
		if npc.Goals == nil {
			npc.Goals = storage.JSONMap{}
		}
		goalKeys := sortedIntMapKeys(variant.Effects.NPCGoalDeltas[npcKey])
		for _, goalKey := range goalKeys {
			current, _ := numericInt64(npc.Goals[goalKey])
			npc.Goals[goalKey] = clamp64(current+variant.Effects.NPCGoalDeltas[npcKey][goalKey], -1_000_000_000, 1_000_000_000)
		}
		npc.LastActiveAt = gameTime
		npc.UpdatedAt = time.Now()
		if err := execution.Store.SaveNPC(npc); err != nil {
			return storage.CampaignEventDraft{}, err
		}
	}
	processed := gameTime
	scheduled.Status, scheduled.ProcessedAtGameTime, scheduled.UpdatedAt = "processed", &processed, time.Now()
	if err := execution.Store.SaveScheduledEvent(scheduled); err != nil {
		return storage.CampaignEventDraft{}, err
	}
	if event.RepeatIntervalMinutes > 0 {
		if scheduled.DueGameTime > math.MaxInt64-event.RepeatIntervalMinutes || scheduled.Occurrence == math.MaxInt64 {
			return storage.CampaignEventDraft{}, invalid("周期世界事件调度溢出")
		}
		if err := execution.Store.CreateScheduledEvent(&storage.CampaignScheduledEvent{
			ID: uuid.New(), CampaignID: execution.Campaign.ID, EventKind: storage.DefinitionWorldEvent,
			TemplateKey: scheduled.TemplateKey, DueGameTime: scheduled.DueGameTime + event.RepeatIntervalMinutes,
			Priority: event.Priority, Status: "pending", Occurrence: scheduled.Occurrence + 1,
			Scope: storage.JSONMap{"region_key": event.RegionKey}, Payload: storage.JSONMap{}, CreatedAt: time.Now(), UpdatedAt: time.Now(),
		}); err != nil {
			return storage.CampaignEventDraft{}, err
		}
	}
	return storage.CampaignEventDraft{EventType: "world.event_triggered", Visibility: storage.CampaignVisibilityPlayer, Payload: storage.JSONMap{
		"template_key": scheduled.TemplateKey, "name": definition.Name, "region_key": event.RegionKey,
		"variant_key": variant.Key, "summary": variant.Summary, "occurrence": scheduled.Occurrence,
		"region_state": publicRegionState(*region),
	}}, nil
}

func processQuestDeadline(execution campaign.ActionExecution, scheduled *storage.CampaignScheduledEvent, gameTime int64) (storage.CampaignEventDraft, error) {
	row, err := execution.Store.Quest(execution.Campaign.ID, scheduled.TemplateKey)
	if err != nil {
		return storage.CampaignEventDraft{}, err
	}
	definition, _, err := questDefinition(execution, scheduled.TemplateKey)
	if err != nil {
		return storage.CampaignEventDraft{}, err
	}
	from := row.Status
	to := row.Status
	if row.Status == "active" || row.Status == "blocked" {
		if row.QuestType == "obligation" {
			to = "failed"
		} else {
			to = "expired"
		}
		row.Status = to
		row.ResolvedAtGameTime = &gameTime
		row.UpdatedAt = time.Now()
		if err := execution.Store.SaveQuest(row); err != nil {
			return storage.CampaignEventDraft{}, err
		}
	}
	processed := gameTime
	scheduled.Status, scheduled.ProcessedAtGameTime, scheduled.UpdatedAt = "processed", &processed, time.Now()
	if err := execution.Store.SaveScheduledEvent(scheduled); err != nil {
		return storage.CampaignEventDraft{}, err
	}
	return storage.CampaignEventDraft{EventType: "quest.state_changed", Visibility: storage.CampaignVisibilityPlayer, Payload: storage.JSONMap{
		"quest_key": row.QuestKey, "name": definition.Name, "quest_type": row.QuestType,
		"from": from, "to": to, "deadline_processed": true,
	}}, nil
}

func deterministicVariant(seed int64, templateKey string, occurrence int64, variants []worldrules.EventVariant) worldrules.EventVariant {
	hash := sha256.New()
	var buffer [8]byte
	binary.BigEndian.PutUint64(buffer[:], uint64(seed))
	hash.Write(buffer[:])
	hash.Write([]byte{0})
	hash.Write([]byte(templateKey))
	hash.Write([]byte{0})
	binary.BigEndian.PutUint64(buffer[:], uint64(occurrence))
	hash.Write(buffer[:])
	sum := hash.Sum(nil)
	roll := binary.BigEndian.Uint64(sum[:8])
	var total uint64
	for _, variant := range variants {
		total += uint64(variant.Weight)
	}
	roll %= total
	for _, variant := range variants {
		if roll < uint64(variant.Weight) {
			return variant
		}
		roll -= uint64(variant.Weight)
	}
	return variants[len(variants)-1]
}

func applyRegionEffects(region *storage.CampaignRegionState, effects worldrules.EventEffects) {
	region.ResourceLevel = clamp64(region.ResourceLevel+effects.ResourceDelta, 0, 10000)
	region.DangerLevel = clamp64(region.DangerLevel+effects.DangerDelta, 0, 10000)
	region.Stability = clamp64(region.Stability+effects.StabilityDelta, 0, 10000)
	if effects.ConflictStage != nil {
		region.ConflictStage = *effects.ConflictStage
	}
	if region.FactionPower == nil {
		region.FactionPower = storage.JSONMap{}
	}
	for _, key := range sortedIntMapKeys(effects.FactionPowerDeltas) {
		current, _ := numericInt64(region.FactionPower[key])
		region.FactionPower[key] = clamp64(current+effects.FactionPowerDeltas[key], 0, 10000)
	}
}

func publicRegionState(region storage.CampaignRegionState) storage.JSONMap {
	return storage.JSONMap{
		"region_key": region.RegionKey, "resource_level": region.ResourceLevel, "danger_level": region.DangerLevel,
		"stability": region.Stability, "conflict_stage": region.ConflictStage,
	}
}

func relationshipDimension(relation storage.CampaignRelationship, dimension string) int64 {
	switch dimension {
	case "trust":
		return int64(relation.Trust)
	case "fear":
		return int64(relation.Fear)
	case "affinity":
		return int64(relation.Affinity)
	case "hostility":
		return int64(relation.Hostility)
	case "debt":
		return relation.Debt
	default:
		return 0
	}
}

func regionStateField(region storage.CampaignRegionState, field string) int64 {
	switch field {
	case "resource_level":
		return region.ResourceLevel
	case "danger_level":
		return region.DangerLevel
	case "stability":
		return region.Stability
	case "conflict_stage":
		return region.ConflictStage
	default:
		return 0
	}
}

func sortedIntMapKeys(values map[string]int64) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func numericInt64(value any) (int64, bool) {
	switch number := value.(type) {
	case int:
		return int64(number), true
	case int64:
		return number, true
	case float64:
		return int64(number), number == float64(int64(number))
	default:
		return 0, false
	}
}

func clamp64(value, minimum, maximum int64) int64 {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}

func clampInt(value, minimum, maximum int) int {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}

func containsStringValue(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
