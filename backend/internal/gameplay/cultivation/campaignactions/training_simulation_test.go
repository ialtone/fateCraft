package campaignactions

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/gameplay/components/worldactions"
	"github.com/ialtone/fatecraft/internal/gameplay/components/worldrules"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/trainingrules"
	"github.com/ialtone/fatecraft/internal/storage"
)

func readCultivationDefinitions(t *testing.T) []storage.GameplayDefinition {
	t.Helper()
	root := filepath.Join("..", "..", "..", "..", "data", "preset_packs", "mortal-path")
	files, err := filepath.Glob(filepath.Join(root, "definitions", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	files = append([]string{filepath.Join(root, "definitions.json")}, files...)
	all := []storage.GameplayDefinition{}
	for _, path := range files {
		raw, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		var rows []storage.GameplayDefinition
		if err := json.Unmarshal(raw, &rows); err != nil {
			t.Fatal(err)
		}
		all = append(all, rows...)
	}
	return all
}

// This is a phase simulation with the relevant manual and preparation budget
// supplied at entry. It exercises actual learning/equipment, retreat/recovery,
// clocks and seeded world effects, not a claim of a complete economic playthrough.
func TestBuiltinRetreatsReachEveryRealmWithPaidRecoveryAndWorldEvents(t *testing.T) {
	definitions := readCultivationDefinitions(t)
	for seed := int64(0); seed < 8; seed++ {
		for rank := 0; rank < 9; rank++ {
			for _, body := range []bool{false, true} {
				t.Run(fmt.Sprintf("seed-%d/rank-%d/body-%v", seed, rank, body), func(t *testing.T) {
					s, e := trainingFixture()
					s.definitions = map[string]*storage.GameplayDefinition{}
					s.techniques = map[string]*storage.CampaignTechnique{}
					e.Campaign.RandomSeed = seed
					e.Campaign.CurrentLocationKey = "law-market"
					if rank < 5 {
						e.Campaign.CurrentLocationKey = "cloud-sail-port"
					}
					if rank < 2 {
						e.Campaign.CurrentLocationKey = "green-river-village"
					}
					var realm *storage.GameplayDefinition
					for _, d := range definitions {
						copy := d
						s.definitions[d.Kind+":"+d.Key] = &copy
						if d.Kind == "realm" && worldactions.DefinitionIntDefault(d.Content["rank"], -1) == int64(rank) {
							realm = &copy
						}
					}
					if realm == nil {
						t.Fatal("missing realm")
					}
					prefix := "retreat-"
					if body {
						prefix = "body-"
					}
					planKey := prefix + realm.Key
					p, err := trainingrules.Parse(s.definitions[trainingrules.DefinitionKind+":"+planKey].Content)
					if err != nil {
						t.Fatal(err)
					}
					recoveryKey := "herbal-recovery"
					if rank >= 3 {
						recoveryKey = "spiritual-recovery"
					}
					if rank >= 6 {
						recoveryKey = "celestial-recovery"
					}
					recovery, err := trainingrules.Parse(s.definitions[trainingrules.DefinitionKind+":"+recoveryKey].Content)
					if err != nil {
						t.Fatal(err)
					}
					c := s.character
					c.RealmKey = realm.Key
					c.RealmRank = rank
					c.Injuries = storage.JSONMap{}
					c.CultivationProgress = 0
					c.CultivationRequired = worldactions.DefinitionIntDefault(realm.Content["cultivation_required"], 0)
					c.LifespanDays = worldactions.DefinitionIntDefault(realm.Content["lifespan_days"], 0)
					c.BaseMaxHealth = worldactions.DefinitionIntDefault(realm.Content["max_health"], 0)
					c.MaxHealth = c.BaseMaxHealth
					c.Health = c.MaxHealth
					c.BaseMaxMana = worldactions.DefinitionIntDefault(realm.Content["max_mana"], 0)
					c.MaxMana = c.BaseMaxMana
					c.Mana = p.ManaCost
					c.BaseMaxSpiritualSense = worldactions.DefinitionIntDefault(realm.Content["max_spiritual_sense"], 0)
					c.MaxSpiritualSense = c.BaseMaxSpiritualSense
					c.SpiritualSense = c.MaxSpiritualSense
					for _, m := range append(append([]trainingrules.Material{}, p.Materials...), recovery.Materials...) {
						key := m.ItemKey + ":common"
						if s.stacks[key] == nil {
							s.stacks[key] = &storage.CampaignItemStack{ItemKey: m.ItemKey, QualityKey: "common"}
						}
						s.stacks[key].Quantity += m.Quantity * 8
					}
					method := s.definitions["technique:"+p.TechniqueKeys[0]]
					manual := method.Content["source_item_key"].(string)
					s.stacks[manual+":common"] = &storage.CampaignItemStack{ItemKey: manual, QualityKey: "common", Quantity: 1}
					if _, err := learnTechnique(context.Background(), e, map[string]any{"technique_key": method.Key}); err != nil {
						t.Fatal(err)
					}
					if _, err := equipTechnique(context.Background(), e, map[string]any{"technique_key": method.Key, "slot": method.Content["allowed_slots"].([]any)[0]}); err != nil {
						t.Fatal(err)
					}
					// Populate all twelve versioned ambient schedules and their effect
					// targets so long actions cannot accidentally ignore the world clock.
					for _, d := range definitions {
						if d.Kind != storage.DefinitionWorldEvent {
							continue
						}
						event, ok := worldrules.ParseWorldEvent(d.Content)
						if !ok {
							t.Fatal("invalid world event")
						}
						if s.regions[event.RegionKey] == nil {
							s.regions[event.RegionKey] = &storage.CampaignRegionState{RegionKey: event.RegionKey, ResourceLevel: 5000, DangerLevel: 2000, Stability: 5000, FactionPower: storage.JSONMap{}}
						}
						for _, v := range event.Variants {
							for key := range v.Effects.MarketPriceModifierDeltasBPS {
								s.markets[key] = &storage.CampaignMarketState{MarketKey: key, PriceModifierBPS: 10000}
							}
							for key := range v.Effects.NPCGoalDeltas {
								s.npcs[key] = &storage.CampaignNPC{InstanceKey: key, Goals: storage.JSONMap{}}
							}
						}
						id := uuid.New()
						s.scheduled[id] = &storage.CampaignScheduledEvent{ID: id, EventKind: storage.DefinitionWorldEvent, TemplateKey: d.Key, DueGameTime: event.InitialDelayMinutes, Priority: event.Priority, Status: "pending"}
					}
					count, rests := 0, 0
					for c.CultivationProgress < c.CultivationRequired && count < 9 {
						key := planKey
						handler := retreat
						if c.Mana < p.ManaCost {
							key = recoveryKey
							handler = recoverCharacter
							rests++
						} else {
							count++
						}
						out, err := handler(context.Background(), e, map[string]any{"plan_key": key})
						if err != nil {
							t.Fatal(err)
						}
						if err := worldactions.ProcessDueScheduledEvents(e, &out); err != nil {
							t.Fatal(err)
						}
						e.Campaign.GameTime += out.GameTimeDelta
						if rests > 8 {
							t.Fatal("recovery loop")
						}
					}
					if count < 3 || count > 8 || c.CultivationProgress != c.CultivationRequired {
						t.Fatalf("growth target missed: count=%d progress=%d/%d", count, c.CultivationProgress, c.CultivationRequired)
					}
					if c.AgeDays != 5000+int64(count)*p.Days+int64(rests)*recovery.Days || rests == 0 {
						t.Fatal("time or required paid recovery was bypassed")
					}
					for _, pending := range s.scheduled {
						if pending.Status == "pending" && pending.DueGameTime <= e.Campaign.GameTime {
							t.Fatal("overdue event left behind")
						}
					}
				})
			}
		}
	}
}
