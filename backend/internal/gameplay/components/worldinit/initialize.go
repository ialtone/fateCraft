package worldinit

import (
	"fmt"
	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/gameplay/components/craftingrules"
	"github.com/ialtone/fatecraft/internal/gameplay/components/factionrules"
	"github.com/ialtone/fatecraft/internal/gameplay/components/marketrules"
	"github.com/ialtone/fatecraft/internal/gameplay/components/worldrules"
	"github.com/ialtone/fatecraft/internal/storage"
	"gorm.io/gorm"
	"time"
)

func Initialize(tx *gorm.DB, campaign *storage.Campaign) error {
	var definitions []storage.GameplayDefinition
	if err := tx.Where("challenge_version_id = ?", campaign.ChallengeVersionID).
		Order("kind ASC, sort_order ASC, key ASC").Find(&definitions).Error; err != nil {
		return err
	}
	locationFound := false
	locationDefinitions := map[string]storage.GameplayDefinition{}
	for index := range definitions {
		definition := &definitions[index]
		if definition.Kind == storage.DefinitionLocation {
			locationDefinitions[definition.Key] = *definition
			if definition.Key == campaign.CurrentLocationKey {
				locationFound = true
			}
		}
	}
	if !locationFound {
		return fmt.Errorf("storage.Campaign 缺少初始地点定义")
	}
	now := time.Now()
	locationKey := campaign.CurrentLocationKey
	for depth := 0; depth < 4 && locationKey != ""; depth++ {
		knowledge := storage.JSONMap{}
		if depth > 0 {
			knowledge["source"] = "initial_hierarchy"
		}
		if err := tx.Create(&storage.CampaignLocationDiscovery{
			CampaignID: campaign.ID, LocationKey: locationKey,
			DiscoveredAtGameTime: campaign.GameTime, Visited: depth == 0, Knowledge: knowledge, UpdatedAt: now,
		}).Error; err != nil {
			return err
		}
		definition, ok := locationDefinitions[locationKey]
		if !ok {
			break
		}
		locationKey, _ = definition.Content["parent_key"].(string)
	}
	for _, definition := range definitions {
		switch definition.Kind {
		case storage.DefinitionItem:
			if quantity, ok := Int(definition.Content["starting_quantity"]); ok && quantity > 0 {
				if err := tx.Create(&storage.CampaignItemStack{
					CampaignID: campaign.ID, ItemKey: definition.Key, QualityKey: "common",
					Quantity: quantity, Metadata: storage.JSONMap{"name": definition.Name}, UpdatedAt: now,
				}).Error; err != nil {
					return err
				}
			}
		case storage.DefinitionEquipment:
			if starting, _ := definition.Content["starting_owned"].(bool); starting {
				durability := int(IntDefault(definition.Content["durability_max"], 100))
				if err := tx.Create(&storage.CampaignItemInstance{
					ID: uuid.New(), CampaignID: campaign.ID, DefinitionKey: definition.Key,
					QualityKey: "common", State: "stored", Durability: durability, MaxDurability: durability,
					Properties: storage.JSONMap{"name": definition.Name}, Provenance: storage.JSONMap{"source": "campaign.start"},
					CreatedAt: now, UpdatedAt: now,
				}).Error; err != nil {
					return err
				}
			}
		case storage.DefinitionRecipe:
			recipe, ok := craftingrules.Parse(map[string]any(definition.Content))
			if !ok || !recipe.StartingKnown {
				continue
			}
			if err := tx.Create(&storage.CampaignKnownRecipe{
				CampaignID: campaign.ID, RecipeKey: definition.Key,
				LearnedAtGameTime: campaign.GameTime,
				SourceSnapshot:    storage.JSONMap{"name": definition.Name, "key": definition.Key, "source": "campaign.start"},
				CreatedAt:         now,
			}).Error; err != nil {
				return err
			}
		case storage.DefinitionMarket:
			market, ok := marketrules.Parse(map[string]any(definition.Content))
			if !ok {
				return fmt.Errorf("市场定义无效: %s", definition.Key)
			}
			if err := tx.Create(&storage.CampaignMarketState{
				CampaignID: campaign.ID, MarketKey: definition.Key, LocationKey: market.LocationKey,
				LastRestockGameTime: campaign.GameTime, PriceModifierBPS: 10000, UpdatedAt: now,
			}).Error; err != nil {
				return err
			}
			for _, entry := range market.Entries {
				if err := tx.Create(&storage.CampaignMarketStock{
					CampaignID: campaign.ID, MarketKey: definition.Key,
					ItemKind: entry.Kind, ItemKey: entry.Key, QualityKey: entry.QualityKey,
					Quantity: entry.InitialStock, UpdatedAt: now,
				}).Error; err != nil {
					return err
				}
			}
		case storage.DefinitionFaction:
			reputation := int(IntDefault(definition.Content["initial_reputation"], 0))
			if err := tx.Create(&storage.CampaignFactionStanding{
				CampaignID: campaign.ID, FactionKey: definition.Key, Reputation: reputation,
				PositionKey: factionrules.Position(definition.Content, 0),
				Permissions: storage.JSONMap(factionrules.DerivePermissions(definition.Content, reputation)), UpdatedAt: now,
			}).Error; err != nil {
				return err
			}
		case storage.DefinitionLocation:
			if nodeType, _ := definition.Content["node_type"].(string); nodeType == "region" {
				initial, ok := worldrules.ParseRegionInitialState(definition.Content["initial_state"])
				if !ok {
					return fmt.Errorf("区域初始状态无效: %s", definition.Key)
				}
				factionPower := storage.JSONMap{}
				for key, value := range initial.FactionPower {
					factionPower[key] = value
				}
				if err := tx.Create(&storage.CampaignRegionState{
					CampaignID: campaign.ID, RegionKey: definition.Key,
					ResourceLevel: initial.ResourceLevel, DangerLevel: initial.DangerLevel,
					Stability: initial.Stability, ConflictStage: initial.ConflictStage,
					FactionPower: factionPower, UpdatedAt: now,
				}).Error; err != nil {
					return err
				}
			}
		case storage.DefinitionNPCArchetype:
			if initial, _ := definition.Content["initial"].(bool); !initial {
				continue
			}
			instanceKey, _ := definition.Content["instance_key"].(string)
			locationKey, _ := definition.Content["location_key"].(string)
			realmKey, _ := definition.Content["realm_key"].(string)
			stage, _ := definition.Content["stage_key"].(string)
			if stage == "" {
				stage = "initial"
			}
			faction, _ := definition.Content["faction_key"].(string)
			public, _ := definition.Content["public_profile"].(map[string]any)
			private, _ := definition.Content["private_state"].(map[string]any)
			goals, _ := definition.Content["goals"].(map[string]any)
			npc := storage.CampaignNPC{
				ID: uuid.New(), CampaignID: campaign.ID, ArchetypeKey: definition.Key,
				InstanceKey: instanceKey, StableName: definition.Name, FactionKey: faction,
				RealmKey: realmKey, StageKey: stage, CurrentLocationKey: locationKey, Status: "active",
				PublicProfile: storage.JSONMap(public), PrivateState: storage.JSONMap(private), Goals: storage.JSONMap(goals),
				CreatedAt: now, UpdatedAt: now,
			}
			if npc.PublicProfile == nil {
				npc.PublicProfile = storage.JSONMap{}
			}
			if npc.PrivateState == nil {
				npc.PrivateState = storage.JSONMap{}
			}
			if npc.Goals == nil {
				npc.Goals = storage.JSONMap{}
			}
			if err := tx.Create(&npc).Error; err != nil {
				return err
			}
		case storage.DefinitionWorldEvent:
			event, ok := worldrules.ParseWorldEvent(map[string]any(definition.Content))
			if !ok {
				return fmt.Errorf("世界事件定义无效: %s", definition.Key)
			}
			if err := tx.Create(&storage.CampaignScheduledEvent{
				ID: uuid.New(), CampaignID: campaign.ID, EventKind: storage.DefinitionWorldEvent,
				TemplateKey: definition.Key, DueGameTime: campaign.GameTime + event.InitialDelayMinutes,
				Priority: event.Priority, Status: "pending", Occurrence: 0,
				Scope: storage.JSONMap{"region_key": event.RegionKey}, Payload: storage.JSONMap{}, CreatedAt: now, UpdatedAt: now,
			}).Error; err != nil {
				return err
			}
		case storage.DefinitionQuest:
			quest, ok := worldrules.ParseQuest(map[string]any(definition.Content))
			if !ok {
				return fmt.Errorf("任务定义无效: %s", definition.Key)
			}
			if !quest.Initial {
				continue
			}
			row := storage.CampaignQuest{
				CampaignID: campaign.ID, QuestKey: definition.Key, QuestType: quest.QuestType,
				Status: quest.StartingStatus, OfferedAtGameTime: campaign.GameTime,
				Progress: storage.JSONMap{}, SourceSnapshot: storage.JSONMap{"key": definition.Key, "name": definition.Name}, UpdatedAt: now,
			}
			if quest.StartingStatus == "active" {
				accepted := campaign.GameTime
				row.AcceptedAtGameTime = &accepted
				if quest.DeadlineMinutes > 0 {
					due := campaign.GameTime + quest.DeadlineMinutes
					row.DueGameTime = &due
				}
			}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
			if row.DueGameTime != nil {
				if err := tx.Create(&storage.CampaignScheduledEvent{
					ID: uuid.New(), CampaignID: campaign.ID, EventKind: "quest_deadline",
					TemplateKey: definition.Key, DueGameTime: *row.DueGameTime,
					Priority: 1000, Status: "pending", Occurrence: 0,
					Scope: storage.JSONMap{}, Payload: storage.JSONMap{"quest_key": definition.Key}, CreatedAt: now, UpdatedAt: now,
				}).Error; err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func Int(value any) (int64, bool) {
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

func IntDefault(value any, fallback int64) int64 {
	if number, ok := Int(value); ok {
		return number
	}
	return fallback
}
