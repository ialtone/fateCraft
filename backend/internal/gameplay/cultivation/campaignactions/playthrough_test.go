package campaignactions

import (
	"context"
	"fmt"
	"sort"
	"testing"

	"github.com/google/uuid"
	"github.com/ialtone/fatecraft/internal/campaign"
	"github.com/ialtone/fatecraft/internal/gameplay/components/factionrules"
	"github.com/ialtone/fatecraft/internal/gameplay/components/gatheringrules"
	"github.com/ialtone/fatecraft/internal/gameplay/components/marketrules"
	"github.com/ialtone/fatecraft/internal/gameplay/components/worldactions"
	"github.com/ialtone/fatecraft/internal/gameplay/components/worldrules"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/campaigninit"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/originrules"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/trainingrules"
	"github.com/ialtone/fatecraft/internal/storage"
)

// An in-memory playthrough of the published starting facts and actual action
// handlers. Random results use the production neutral D100 fallback. SQL and
// narrative quality are deliberately outside this simulation's coverage.
type cultivationPlaythrough struct {
	t                           *testing.T
	s                           *actionStoreStub
	e                           campaign.ActionExecution
	ds                          []storage.GameplayDefinition
	counts                      map[string]int
	earned, spent, recoveryCost int64
	body                        bool
}

func newCultivationPlaythrough(t *testing.T, seed int64, body bool) *cultivationPlaythrough {
	s := newActionStore()
	p := &cultivationPlaythrough{t: t, s: s, ds: readCultivationDefinitions(t), counts: map[string]int{}, body: body}
	p.e = campaign.ActionExecution{Store: s, ActorType: "player", MaxDueEventsPerTurn: 16, Campaign: storage.Campaign{ID: uuid.New(), ChallengeVersionID: uuid.New(), RandomSeed: seed, Status: storage.CampaignStatusActive, CurrentLocationKey: "green-river-village"}}
	selected, err := campaigninit.StartingTechniqueKeys(p.ds, seed)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range p.ds {
		copy := d
		s.definitions[d.Kind+":"+d.Key] = &copy
		c := d.Content
		switch d.Kind {
		case "realm":
			if number(c, "rank") == 0 {
				s.character = &storage.CampaignCharacter{ID: uuid.New(), CampaignID: p.e.Campaign.ID, RealmKey: d.Key, StageKey: c["stages"].([]any)[0].(string), FoundationQuality: 50, CultivationRequired: number(c, "cultivation_required"), AgeDays: number(c, "starting_age_days"), LifespanDays: number(c, "lifespan_days"), BaseMaxHealth: number(c, "max_health"), MaxHealth: number(c, "max_health"), Health: number(c, "max_health"), BaseMaxMana: number(c, "max_mana"), MaxMana: number(c, "max_mana"), Mana: number(c, "max_mana"), BaseMaxSpiritualSense: number(c, "max_spiritual_sense"), MaxSpiritualSense: number(c, "max_spiritual_sense"), SpiritualSense: number(c, "max_spiritual_sense"), Injuries: storage.JSONMap{}, LongTermEffects: storage.JSONMap{}}
			}
		case "item":
			if qty := number(c, "starting_quantity"); qty > 0 {
				s.stacks[d.Key+":common"] = &storage.CampaignItemStack{CampaignID: p.e.Campaign.ID, ItemKey: d.Key, QualityKey: "common", Quantity: qty}
			}
		case "equipment":
			if c["starting_owned"] == true {
				id := uuid.New()
				dur := int(number(c, "durability_max"))
				s.instances[id] = &storage.CampaignItemInstance{ID: id, CampaignID: p.e.Campaign.ID, DefinitionKey: d.Key, QualityKey: "common", State: "stored", Durability: dur, MaxDurability: dur}
			}
		case "recipe":
			if c["starting_known"] == true {
				s.recipes[d.Key] = &storage.CampaignKnownRecipe{CampaignID: p.e.Campaign.ID, RecipeKey: d.Key}
			}
		case "technique":
			if selected[d.Key] {
				row := &storage.CampaignTechnique{ID: uuid.New(), CampaignID: p.e.Campaign.ID, TechniqueKey: d.Key, Category: c["category"].(string), LearnedLayer: 1, Status: "active"}
				if slot, ok := c["starting_slot"].(string); ok {
					row.EquippedSlot = &slot
				}
				s.techniques[d.Key] = row
			}
		case "location":
			if c["node_type"] == "region" {
				initial, ok := worldrules.ParseRegionInitialState(c["initial_state"])
				if !ok {
					t.Fatal(d.Key)
				}
				power := storage.JSONMap{}
				for k, v := range initial.FactionPower {
					power[k] = v
				}
				s.regions[d.Key] = &storage.CampaignRegionState{CampaignID: p.e.Campaign.ID, RegionKey: d.Key, ResourceLevel: initial.ResourceLevel, DangerLevel: initial.DangerLevel, Stability: initial.Stability, ConflictStage: initial.ConflictStage, FactionPower: power}
			}
		case "faction":
			rep := int(number(c, "initial_reputation"))
			s.factions[d.Key] = &storage.CampaignFactionStanding{CampaignID: p.e.Campaign.ID, FactionKey: d.Key, Reputation: rep, Permissions: storage.JSONMap(factionrules.DerivePermissions(c, rep))}
		case "market":
			m, ok := marketrules.Parse(map[string]any(c))
			if !ok {
				t.Fatal(d.Key)
			}
			s.markets[d.Key] = &storage.CampaignMarketState{CampaignID: p.e.Campaign.ID, MarketKey: d.Key, LocationKey: m.LocationKey, PriceModifierBPS: 10000}
			for _, entry := range m.Entries {
				row := &storage.CampaignMarketStock{CampaignID: p.e.Campaign.ID, MarketKey: d.Key, ItemKind: entry.Kind, ItemKey: entry.Key, QualityKey: entry.QualityKey, Quantity: entry.InitialStock}
				s.marketStocks[marketStockTestKey(d.Key, entry.Kind, entry.Key, entry.QualityKey)] = row
			}
		case "world_event":
			rule, ok := worldrules.ParseWorldEvent(c)
			if !ok {
				t.Fatal(d.Key)
			}
			id := uuid.New()
			s.scheduled[id] = &storage.CampaignScheduledEvent{ID: id, CampaignID: p.e.Campaign.ID, EventKind: d.Kind, TemplateKey: d.Key, DueGameTime: rule.InitialDelayMinutes, Priority: rule.Priority, Status: "pending"}
		case "npc_archetype":
			if c["initial"] == true {
				key := c["instance_key"].(string)
				goals, _ := c["goals"].(map[string]any)
				s.npcs[key] = &storage.CampaignNPC{ID: uuid.New(), CampaignID: p.e.Campaign.ID, InstanceKey: key, ArchetypeKey: d.Key, CurrentLocationKey: c["location_key"].(string), Status: "active", Goals: storage.JSONMap(goals)}
			}
		case "quest":
			q, ok := worldrules.ParseQuest(c)
			if !ok {
				t.Fatal(d.Key)
			}
			if q.Initial {
				s.quests[d.Key] = &storage.CampaignQuest{CampaignID: p.e.Campaign.ID, QuestKey: d.Key, QuestType: q.QuestType, Status: q.StartingStatus, Progress: storage.JSONMap{}}
			}
		}
	}
	origins, err := originrules.Parse(s.definitions[originrules.DefinitionKind+":origins"].Content)
	if err != nil {
		t.Fatal(err)
	}
	for key, qty := range origins.Origins[int(seed)%len(origins.Origins)].Items {
		s.stacks[key+":common"].Quantity += qty
	}
	for key := "green-river-village"; key != ""; {
		s.discovered[key] = true
		key, _ = s.definitions["location:"+key].Content["parent_key"].(string)
	}
	if _, err := recalculateCharacterStats(p.e, s.character); err != nil {
		t.Fatal(err)
	}
	s.character.Health = s.character.MaxHealth
	s.character.Mana = s.character.MaxMana
	s.character.SpiritualSense = s.character.MaxSpiritualSense
	return p
}

func number(c map[string]any, key string) int64 { return worldactions.DefinitionIntDefault(c[key], 0) }
func (p *cultivationPlaythrough) qty(key string) int64 {
	if s := p.s.stacks[key+":common"]; s != nil {
		return s.Quantity
	}
	return 0
}
func (p *cultivationPlaythrough) do(key string, handler campaign.ActionHandlerFunc, args map[string]any) storage.CampaignActionOutcome {
	p.t.Helper()
	p.e.ActionID = fmt.Sprintf("sim-%d", p.e.Campaign.Revision)
	p.e.ActionKey = key
	if p.e.Campaign.Revision > 12000 {
		p.t.Fatalf("run exceeded action budget: rank=%d counts=%v", p.s.character.RealmRank, p.counts)
	}
	if err := rejectWhileEncounterActive(p.e, key); err != nil {
		p.t.Fatal(err)
	}
	before := p.qty("low-spirit-stone")
	out, err := handler(context.Background(), p.e, args)
	if err != nil {
		p.t.Fatalf("rank=%d at=%s action=%s args=%v step=%d: %v", p.s.character.RealmRank, p.e.Campaign.CurrentLocationKey, key, args, p.e.Campaign.Revision, err)
	}
	after := p.qty("low-spirit-stone")
	if after > before {
		p.earned += after - before
	} else {
		p.spent += before - after
		if key == ActionRecover {
			p.recoveryCost += before - after
		}
	}
	if err := recordProgressRewards(context.Background(), p.e, &out); err != nil {
		p.t.Fatal(err)
	}
	if out.Status == nil {
		if err := expireDueConditions(p.e, &out); err != nil {
			p.t.Fatal(err)
		}
		if err := worldactions.ProcessDueScheduledEvents(p.e, &out); err != nil {
			p.t.Fatal(err)
		}
	}
	if out.Status != nil {
		if err := settleProgressRewards(context.Background(), p.e, &out); err != nil {
			p.t.Fatal(err)
		}
	}
	p.e.Campaign.GameTime += out.GameTimeDelta
	p.e.Campaign.Revision++
	p.counts[key]++
	if out.CurrentLocationKey != nil {
		p.e.Campaign.CurrentLocationKey = *out.CurrentLocationKey
	}
	if out.Status != nil {
		p.e.Campaign.Status = *out.Status
		if *out.Status == storage.CampaignStatusDead {
			p.t.Fatalf("died at rank=%d action=%s", p.s.character.RealmRank, key)
		}
	}
	return out
}

func (p *cultivationPlaythrough) path(to string) []string {
	from := p.e.Campaign.CurrentLocationKey
	if to == from {
		return []string{}
	}
	paths := map[string][]string{from: {}}
	queue := []string{from}
	for len(queue) > 0 {
		at := queue[0]
		queue = queue[1:]
		for _, raw := range p.s.definitions["location:"+at].Content["connections"].([]any) {
			next := raw.(map[string]any)["location_key"].(string)
			if _, seen := paths[next]; seen {
				continue
			}
			allowed, err := missingEntryPermissions(p.e, p.s.definitions["location:"+next].Content)
			if err != nil || len(allowed) > 0 {
				continue
			}
			path := append(append([]string{}, paths[at]...), next)
			if next == to {
				return path
			}
			paths[next] = path
			queue = append(queue, next)
		}
	}
	return nil
}

func (p *cultivationPlaythrough) goTo(to string) {
	p.t.Helper()
	if to == p.e.Campaign.CurrentLocationKey {
		return
	}
	path := p.path(to)
	if path == nil {
		p.t.Fatalf("no route to %s from %s rank=%d", to, p.e.Campaign.CurrentLocationKey, p.s.character.RealmRank)
	}
	for _, next := range path {
		for tries := 0; !p.s.discovered[next]; tries++ {
			if tries > 100 {
				p.t.Fatal("discovery stalled")
			}
			p.do(ActionInvestigate, investigate, map[string]any{})
		}
		p.do(ActionTravel, travel, map[string]any{"destination_key": next})
	}
}

func (p *cultivationPlaythrough) nearest(keys []string) string {
	best := ""
	distance := 10000
	for _, key := range keys {
		path := p.path(key)
		if path == nil && key != p.e.Campaign.CurrentLocationKey {
			continue
		}
		if len(path) < distance {
			best = key
			distance = len(path)
		}
	}
	if best == "" {
		p.t.Fatalf("no reachable option in %v", keys)
	}
	return best
}

func (p *cultivationPlaythrough) gatherItem(item string, amount int64) {
	p.t.Helper()
	sources := []string{}
	for _, d := range p.ds {
		if d.Kind != "location" {
			continue
		}
		g, ok := gatheringrules.Parse(d.Content["gathering"])
		if ok && len(gatheringrules.MatchingEntries(g, item)) > 0 {
			sources = append(sources, d.Key)
		}
	}
	for p.qty(item) < amount {
		loc := p.nearest(sources)
		g, _ := gatheringrules.Parse(p.s.definitions["location:"+loc].Content["gathering"])
		if p.s.character.Health <= g.RiskHealthDamage*2 {
			p.rest(false)
			continue
		}
		p.goTo(loc)
		p.do(ActionGather, gather, map[string]any{"item_key": item})
	}
}

func (p *cultivationPlaythrough) funds(amount int64) {
	p.t.Helper()
	// Use progressively valuable local resources instead of farming village
	// herbs at upper realms. Markets still enforce price, capacity and time.
	item, market, batch := "moon-dew", "county-association-market", int64(6)
	if p.s.character.RealmRank >= 3 {
		item, market, batch = "void-crystal", "tide-star-exchange", 6
	}
	if p.s.character.RealmRank >= 5 {
		item, market, batch = "law-fragment", "nine-law-exchange", 7
	}
	for p.qty("low-spirit-stone") < amount {
		p.gatherItem(item, batch)
		md := p.s.definitions["market:"+market]
		m, _ := marketrules.Parse(map[string]any(md.Content))
		p.goTo(m.LocationKey)
		_, stocks, err := worldactions.RefreshMarket(p.e, market, m)
		if err != nil {
			p.t.Fatal(err)
		}
		entry, _ := marketrules.FindEntry(m, "item", item, "common")
		capacity := entry.MaxStock - stocks[worldactions.MarketStockIdentity("item", item, "common")].Quantity
		if capacity == 0 {
			p.do(ActionInvestigate, investigate, map[string]any{})
			continue
		}
		p.do(ActionTradeSell, worldactions.TradeSell, map[string]any{"market_key": market, "item_kind": "item", "item_key": item, "quantity": min(capacity, p.qty(item))})
	}
}

func (p *cultivationPlaythrough) rest(paid bool) {
	p.t.Helper()
	key := "quiet-rest"
	rank := p.s.character.RealmRank
	if paid {
		key = "herbal-recovery"
		if rank >= 3 {
			key = "spiritual-recovery"
		}
		if rank >= 6 {
			key = "celestial-recovery"
		}
	}
	plan, err := trainingrules.Parse(p.s.definitions[trainingrules.DefinitionKind+":"+key].Content)
	if err != nil {
		p.t.Fatal(err)
	}
	// Free recovery is the fallback while gathering a paid recovery budget.
	if paid {
		for _, m := range plan.Materials {
			if p.qty(m.ItemKey) < m.Quantity {
				c := p.s.character
				if c.Health < c.MaxHealth || c.Mana < c.MaxMana || c.SpiritualSense < c.MaxSpiritualSense {
					p.rest(false)
				}
				p.prepare(plan.Materials)
				break
			}
		}
	}
	p.goTo(p.nearest(plan.LocationKeys))
	p.do(ActionRecover, recoverCharacter, map[string]any{"plan_key": key})
}

func (p *cultivationPlaythrough) prepare(materials []trainingrules.Material) {
	for _, m := range materials {
		if m.ItemKey == "low-spirit-stone" {
			p.funds(m.Quantity)
		} else if p.qty(m.ItemKey) < m.Quantity {
			p.gatherItem(m.ItemKey, m.Quantity)
		}
	}
}

func (p *cultivationPlaythrough) method(key, slot string) {
	p.t.Helper()
	if p.s.techniques[key] == nil {
		p.do(ActionLearnTechnique, learnTechnique, map[string]any{"technique_key": key})
	}
	p.do(ActionEquipTechnique, equipTechnique, map[string]any{"technique_key": key, "slot": slot})
}

func (p *cultivationPlaythrough) grow() {
	p.t.Helper()
	c := p.s.character
	prefix := "retreat-"
	if p.body {
		prefix = "body-"
	}
	key := prefix + c.RealmKey
	plan, err := trainingrules.Parse(p.s.definitions[trainingrules.DefinitionKind+":"+key].Content)
	if err != nil {
		p.t.Fatal(err)
	}
	for c.CultivationProgress < c.CultivationRequired {
		if len(c.Injuries) > 0 || c.Mana < plan.ManaCost {
			p.rest(true)
			continue
		}
		p.prepare(plan.Materials)
		p.goTo(p.nearest(plan.LocationKeys))
		p.do(ActionRetreat, retreat, map[string]any{"plan_key": key})
	}
}

func (p *cultivationPlaythrough) nextRealm() {
	p.t.Helper()
	rank := p.s.character.RealmRank
	for tries := 0; p.s.character.RealmRank == rank; tries++ {
		if tries > 80 {
			p.t.Fatal("breakthrough retry tail exceeded")
		}
		p.grow()
		if len(p.s.character.Injuries) > 0 {
			p.rest(true)
		}
		p.do(ActionBreakthrough, attemptBreakthrough, map[string]any{})
	}
}

func (p *cultivationPlaythrough) accept(key string) {
	p.t.Helper()
	d := p.s.definitions["quest:"+key]
	q, _ := worldrules.ParseQuest(d.Content)
	p.goTo(p.nearest(q.AcceptLocationKeys))
	p.do(ActionAcceptQuest, worldactions.AcceptQuest, map[string]any{"quest_key": key})
}
func (p *cultivationPlaythrough) submit(key string) {
	p.t.Helper()
	p.do(ActionSubmitQuest, submitCultivationQuest, map[string]any{"quest_key": key})
	if p.s.quests[key].Status != "completed" {
		p.t.Fatalf("quest %s blocked: %v", key, p.s.quests[key].Progress)
	}
}

func (p *cultivationPlaythrough) fight(key string) {
	p.t.Helper()
	for attempt := 0; attempt < 10; attempt++ {
		d := p.s.definitions["encounter:"+key]
		for p.s.character.Health < p.s.character.MaxHealth*9/10 || p.s.character.Mana < p.s.character.MaxMana*9/10 {
			p.rest(false)
		}
		p.goTo(d.Content["location_key"].(string))
		if key == "final-ascension-tribulation" {
			p.do(ActionBeginAscension, beginAscension, map[string]any{})
		} else {
			p.do(ActionStartEncounter, startEncounter, map[string]any{"encounter_key": key})
		}
		active, err := p.s.ActiveEncounter(p.e.Campaign.ID)
		if err != nil {
			p.t.Fatal(err)
		}
		for active.Status == "active" {
			args := map[string]any{"encounter_id": active.ID.String(), "move": "attack"}
			best := int64(0)
			for _, d := range p.ds {
				method := p.s.techniques[d.Key]
				if d.Kind != "technique" || method == nil || method.EquippedSlot == nil {
					continue
				}
				damage, mana, err := encounterPlayerDamage(p.e, p.s.character, d.Key, active, number(p.s.definitions["encounter:"+key].Content, "player_mana_cost"))
				if err == nil && damage > best && mana < p.s.character.Mana {
					args["technique_key"] = d.Key
					best = damage
				}
			}
			p.do(ActionEncounterAct, actEncounter, args)
		}
		if active.Status == "won" {
			return
		}
		if key == "final-ascension-tribulation" {
			p.gatherItem("law-fragment", 9)
		}
	}
	p.t.Fatalf("fight %s exceeded retry budget", key)
}

func TestCultivationProductionIncomePaysForEarlyGrowth(t *testing.T) {
	for seed := int64(0); seed < 6; seed++ {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			p := newCultivationPlaythrough(t, seed, false)
			if p.s.techniques["basic-alchemy"] == nil {
				p.buy("manual.basic-alchemy", "county-association-market")
				p.method("basic-alchemy", p.s.definitions["technique:basic-alchemy"].Content["allowed_slots"].([]any)[0].(string))
			}
			initialPills := p.qty("healing-pill")
			for p.qty("healing-pill") < initialPills+10 {
				if p.counts[ActionCraftRecipe] > 60 {
					t.Fatal("production never produced saleable pills")
				}
				if p.s.character.Mana < 4 {
					p.rest(false)
				}
				p.gatherItem("green-river-herb", 2)
				p.gatherItem("clear-spring-water", 1)
				p.goTo("green-river-village")
				p.do(ActionCraftRecipe, executeRecipe, map[string]any{"recipe_key": "healing-pill-recipe"})
			}
			p.goTo("bluestone-market")
			before := p.qty("low-spirit-stone")
			p.do(ActionTradeSell, worldactions.TradeSell, map[string]any{"market_key": "bluestone-guild-market", "item_kind": "item", "item_key": "healing-pill", "quantity": 10})
			income := p.qty("low-spirit-stone") - before
			if income < 20 {
				t.Fatalf("production income too low: %d", income)
			}
			p.nextRealm()
			if p.s.character.RealmRank != 1 || p.counts[ActionCraftRecipe] == 0 {
				t.Fatal("production did not support early growth")
			}
			t.Logf("seed=%d crafts=%d sale_income=%d actions=%d days=%d", seed, p.counts[ActionCraftRecipe], income, p.e.Campaign.Revision, p.e.Campaign.GameTime/1440)
		})
	}
}

func (p *cultivationPlaythrough) run(ending string) {
	// Ordinary materials, earned income and sequential quest facts only.
	if p.body && p.s.techniques["iron-body"] == nil && p.s.techniques["stone-skin-method"] == nil {
		p.buy("manual.iron-body", "county-association-market")
		p.method("iron-body", "body")
	}
	p.nextRealm()
	p.nextRealm()
	p.accept("guild-herb-order")
	p.gatherItem("green-river-herb", 6)
	p.submit("guild-herb-order")
	p.accept("bamboo-trail-clue")
	p.fight("bamboo-woods-mist-beast")
	p.goTo("herb-valley")
	p.submit("bamboo-trail-clue")
	p.accept("cave-rescue")
	npc := p.s.npcs["lin-hunter"]
	p.goTo(npc.CurrentLocationKey)
	p.e.ActorType = "model"
	p.do(ActionRecordInteraction, worldactions.RecordInteraction, map[string]any{"npc_key": "lin-hunter", "trust_delta": 8})
	p.e.ActorType = "player"
	p.fight("underground-river-captive-rescue")
	p.submit("cave-rescue")
	p.accept("star-sea-charter")
	p.goTo("cloud-sail-port")
	p.submit("star-sea-charter")
	p.method("star-tide-scripture", "primary")
	p.nextRealm()
	if p.body {
		p.method("star-bone-body", "body")
	}
	p.grow()
	p.accept("hunt-abyssal-isle")
	p.fight("abyssal-isle-star-beast")
	p.submit("hunt-abyssal-isle")
	p.accept("cross-boundary-ruins")
	p.gatherItem("void-crystal", 3)
	p.goTo("void-crossing-gate")
	p.submit("cross-boundary-ruins")
	p.nextRealm()
	p.method("void-unity-scripture", "primary")
	p.method("star-tide-scripture", "secondary")
	p.grow()
	p.accept("seal-rift-heart")
	p.fight("rift-heart-guardian")
	p.submit("seal-rift-heart")
	p.nextRealm()
	p.accept("enter-upper-spirit-realm")
	p.goTo("ascension-platform")
	p.submit("enter-upper-spirit-realm")
	p.method("law-harmonizing-scripture", "primary")
	p.method("celestial-body", "body")
	p.nextRealm()
	p.accept("void-spatial-survey")
	p.gatherItem("void-crystal", 3)
	p.goTo("void-survey-platform")
	p.submit("void-spatial-survey")
	p.nextRealm()
	p.method("great-unity-scripture", "primary")
	p.accept("integration-body-trial")
	p.gatherItem("celestial-herb", 3)
	p.fight("dharma-pressure-trial")
	p.submit("integration-body-trial")
	p.nextRealm()
	p.accept("great-ascension-preparation")
	p.gatherItem("law-fragment", 4)
	p.goTo("ascension-sanctum")
	p.submit("great-ascension-preparation")
	p.grow()
	p.gatherItem("law-fragment", 9)
	p.fight("final-ascension-tribulation")
	p.accept(ending)
	if ending == "warden-ending" {
		p.fight("celestial-frontier-breach")
	}
	p.goTo("celestial-frontier")
	p.submit(ending)
	if p.e.Campaign.Status != storage.CampaignStatusCompleted || len(p.s.stageRewards) != 3 || p.s.stageGranted != 300 {
		p.t.Fatalf("incomplete ending/rewards: %s %d", p.e.Campaign.Status, p.s.stageGranted)
	}
}

func (p *cultivationPlaythrough) buy(item, market string) {
	p.t.Helper()
	m, _ := marketrules.Parse(map[string]any(p.s.definitions["market:"+market].Content))
	for tries := 0; tries < 20; tries++ {
		p.goTo(m.LocationKey)
		state, stocks, err := worldactions.RefreshMarket(p.e, market, m)
		if err != nil {
			p.t.Fatal(err)
		}
		entry, _ := marketrules.FindEntry(m, "item", item, "common")
		price, _ := worldactions.MarketUnitPrice(p.e, m, state, entry, stocks[worldactions.MarketStockIdentity("item", item, "common")].Quantity, true)
		if p.qty("low-spirit-stone") < price {
			p.funds(price + 20)
			continue
		}
		p.do(ActionTradeBuy, worldactions.TradeBuy, map[string]any{"market_key": market, "item_kind": "item", "item_key": item, "quantity": 1})
		return
	}
	p.t.Fatal("purchase budget never stabilized")
}

func TestCultivationCompleteEconomicPlaythroughs(t *testing.T) {
	for _, body := range []bool{false, true} {
		for _, ending := range []string{"warden-ending", "seeker-ending"} {
			actions, days := []int{}, []int{}
			for seed := int64(0); seed < 6; seed++ {
				t.Run(fmt.Sprintf("body-%v/%s/seed-%d", body, ending, seed), func(t *testing.T) {
					p := newCultivationPlaythrough(t, seed, body)
					p.run(ending)
					actions = append(actions, int(p.e.Campaign.Revision))
					days = append(days, int(p.e.Campaign.GameTime/1440))
					t.Logf("actions=%d days=%d earned=%d spent=%d recovery_stones=%d counts=%v", p.e.Campaign.Revision, p.e.Campaign.GameTime/1440, p.earned, p.spent, p.recoveryCost, p.counts)
				})
			}
			if len(actions) == 6 {
				sort.Ints(actions)
				sort.Ints(days)
				t.Logf("body=%v ending=%s n=6 actions=%d/%d/%d days=%d/%d/%d", body, ending, actions[0], actions[3], actions[5], days[0], days[3], days[5])
			}
		}
	}
}
