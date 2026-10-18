package challengeimport

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ialtone/fatecraft/internal/challenge"
	"github.com/ialtone/fatecraft/internal/gameplay/components/craftingrules"
	"github.com/ialtone/fatecraft/internal/gameplay/components/gatheringrules"
	"github.com/ialtone/fatecraft/internal/gameplay/components/marketrules"
	"github.com/ialtone/fatecraft/internal/gameplay/components/worldrules"
	gameplay "github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/encounterrules"
	"github.com/ialtone/fatecraft/internal/storage"
)

func TestBundledMortalPathCampaignLoads(t *testing.T) {
	directory := filepath.Join("..", "..", "data", "preset_packs", "mortal-path")
	loaded, err := loadDirectory(directory, time.Now(), defaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	if loaded.manifest.ID != "mortal-path" || loaded.manifest.Version != "1.0.1" || len(loaded.document.Definitions) < 200 {
		t.Fatalf("campaign pack not fully loaded: id=%s definitions=%d", loaded.manifest.ID, len(loaded.document.Definitions))
	}
	counts := map[string]int{}
	startingPools := map[string]int{}
	for _, definition := range loaded.document.Definitions {
		counts[definition.Kind]++
		if definition.Kind == storage.DefinitionTechnique {
			if pool, _ := definition.Content["starting_pool"].(string); pool != "" {
				startingPools[pool]++
			}
			if definition.Key == "basic-breathing" {
				if fixed, _ := definition.Content["starting_known"].(bool); fixed {
					t.Fatal("引气诀 must not be a fixed starter")
				}
			}
		}
	}
	minimums := map[string]int{
		storage.DefinitionRealm: 9, storage.DefinitionTechnique: 24, storage.DefinitionItem: 59,
		storage.DefinitionEquipment: 12, storage.DefinitionLocation: 32, storage.DefinitionFaction: 11,
		storage.DefinitionNPCArchetype: 18, storage.DefinitionRecipe: 12, storage.DefinitionMarket: 5,
		storage.DefinitionWorldEvent: 12, storage.DefinitionQuest: 19, storage.DefinitionEncounter: 7,
	}
	for kind, minimum := range minimums {
		if counts[kind] < minimum {
			t.Fatalf("mortal path %s definitions = %d, want at least %d", kind, counts[kind], minimum)
		}
	}
	if got := realmKeysByRank(loaded.document.Definitions); !reflect.DeepEqual(got, []string{
		"mortal", "qi-refining", "foundation", "golden-core", "nascent-soul",
		"spirit-transformation", "void-refining", "body-integration", "great-ascension",
	}) {
		t.Fatalf("unexpected realm progression: %#v", got)
	}
	if startingPools["primary-method"] != 5 || startingPools["secondary-art"] != 3 || startingPools["craft-discipline"] != 3 {
		t.Fatalf("unexpected starting technique pools: %#v", startingPools)
	}
	var promptText strings.Builder
	for _, asset := range loaded.document.Assets {
		if (asset.Kind == storage.ChallengeAssetCharacter || asset.Kind == storage.ChallengeAssetScenario) &&
			strings.Contains(string(asset.Content), "引气诀") {
			t.Fatalf("opening asset %s must not hard-code 引气诀", asset.Slug)
		}
		if asset.Kind == storage.ChallengeAssetPrompt {
			promptText.Write(asset.Content)
		}
	}
	if !strings.Contains(promptText.String(), "普通交谈") ||
		!strings.Contains(promptText.String(), "不得调用 fortune.resolve_uncertain") {
		t.Fatal("mortal-path prompt must exclude ordinary conversation from uncertain action rolls")
	}
	contract, err := storage.DecodeGameplayRuntime(loaded.document.Runtime)
	if err != nil {
		t.Fatal(err)
	}
	if contract.Mode != gameplay.RuntimeModeCampaign || contract.Campaign.InitialLocationKey != "green-river-village" {
		t.Fatalf("unexpected campaign runtime: %+v", contract.Campaign)
	}
	if contract.Presentation.ValueLabels["campaign.relationship.public_stance"]["neutral"] != "中立" {
		t.Fatalf("mortal-path presentation labels were not imported: %#v", contract.Presentation.ValueLabels)
	}
	if contract.Presentation.CampaignView == nil || contract.Presentation.CampaignView.Renderer != "generic" {
		t.Fatalf("mortal-path campaign view was not imported: %#v", contract.Presentation.CampaignView)
	}
	if _, ok := contract.Actions.Capabilities["message_rewrite"]; ok {
		t.Fatalf("mortal-path must not declare platform message rewrite capability: %#v", contract.Actions.Capabilities)
	}
	successPoints := 0
	failurePoints := 0
	for _, outcome := range loaded.document.Outcomes {
		if outcome.Category == "success" && outcome.RewardPoints == 700 {
			successPoints++
		}
		if outcome.Category == "failure" || outcome.Category == "neutral" {
			failurePoints += int(outcome.RewardPoints)
		}
	}
	if successPoints != 2 || failurePoints != 0 {
		t.Fatal("missing fixed campaign points")
	}
}

func realmKeysByRank(definitions []storage.GameplayDefinition) []string {
	realms := realmDefinitionsByRank(definitions)
	keys := make([]string, 0, len(realms))
	for _, realm := range realms {
		keys = append(keys, realm.Key)
	}
	return keys
}

func realmDefinitionsByRank(definitions []storage.GameplayDefinition) []storage.GameplayDefinition {
	realms := make([]storage.GameplayDefinition, 0)
	for _, definition := range definitions {
		if definition.Kind == storage.DefinitionRealm {
			realms = append(realms, definition)
		}
	}
	sort.Slice(realms, func(left, right int) bool {
		leftRank, _ := testDefinitionInt(realms[left].Content["rank"])
		rightRank, _ := testDefinitionInt(realms[right].Content["rank"])
		return leftRank < rightRank
	})
	return realms
}

func TestGameplayDefinitionCatalogLoadsSortedFragments(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "definitions.json"), []byte(`[{"kind":"realm","key":"base","name":"Base","content":{}}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	fragmentDirectory := filepath.Join(directory, "definitions")
	if err := os.Mkdir(fragmentDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, raw := range map[string]string{
		"20-last.json":  `[{"kind":"item","key":"last","name":"Last","content":{}}]`,
		"10-first.json": `[{"kind":"item","key":"first","name":"First","content":{}}]`,
	} {
		if err := os.WriteFile(filepath.Join(fragmentDirectory, name), []byte(raw), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	definitions, err := loadGameplayDefinitionCatalog(directory, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(definitions) != 3 || definitions[0].Key != "base" || definitions[1].Key != "first" || definitions[2].Key != "last" {
		t.Fatalf("unexpected definition order: %#v", definitions)
	}
}

func TestRuntimeSourceRejectsUnsafeEntryReferences(t *testing.T) {
	root := t.TempDir()
	runtimeDir := filepath.Join(root, "runtime")
	if err := os.Mkdir(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	entry := `{"$schema":"fatecraft.runtime.schema.json","runtime_contract_version":2,"engine":{"key":"core.generic","version":1},"mode":"challenge","includes":{"state_schema":"../state-schema.json","actions":"actions.json","settlement":"settlement.json","presentation":"presentation.json"}}`
	if err := os.WriteFile(filepath.Join(runtimeDir, "runtime.json"), []byte(entry), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadRuntimeSource(root, "runtime/runtime.json"); err == nil || !strings.Contains(err.Error(), "不能离开玩法包") {
		t.Fatalf("unsafe Runtime include error = %v", err)
	}
}

func TestRuntimeSourceRejectsWrongSchema(t *testing.T) {
	root := t.TempDir()
	runtimeDir := filepath.Join(root, "runtime")
	if err := os.Mkdir(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	entry := `{"$schema":"other.schema.json","runtime_contract_version":2,"engine":{"key":"core.generic","version":1},"mode":"challenge","includes":{"state_schema":"state-schema.json","actions":"actions.json","settlement":"settlement.json","presentation":"presentation.json"}}`
	if err := os.WriteFile(filepath.Join(runtimeDir, "runtime.json"), []byte(entry), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadRuntimeSource(root, "runtime/runtime.json"); err == nil || !strings.Contains(err.Error(), "$schema 无效") {
		t.Fatalf("wrong Runtime schema error = %v", err)
	}
}

func TestBundledMortalPathContentSourcesAndGraphs(t *testing.T) {
	directory := filepath.Join("..", "..", "data", "preset_packs", "mortal-path")
	loaded, err := loadDirectory(directory, time.Now(), defaultRegistry())
	if err != nil {
		t.Fatal(err)
	}

	definitions := loaded.document.Definitions
	obtainableItems := map[string]bool{}
	techniques := map[string]storage.GameplayDefinition{}
	recipes := map[string]craftingrules.Recipe{}
	quests := map[string]worldrules.Quest{}
	travelable := map[string]bool{}
	connections := map[string][]string{}
	encounterLocations := map[string]string{}

	for _, definition := range definitions {
		switch definition.Kind {
		case storage.DefinitionItem:
			if grantable, _ := definition.Content["model_grantable"].(bool); grantable {
				t.Fatalf("economic item %s must not be mintable by narrative grant", definition.Key)
			}
			if quantity, ok := testDefinitionInt(definition.Content["starting_quantity"]); ok && quantity > 0 {
				obtainableItems[definition.Key] = true
			}
		case storage.DefinitionEquipment:
			if grantable, _ := definition.Content["model_grantable"].(bool); grantable {
				t.Fatalf("economic equipment %s must not be mintable by narrative grant", definition.Key)
			}
		case storage.DefinitionTechnique:
			techniques[definition.Key] = definition
		case storage.DefinitionRecipe:
			recipe, ok := craftingrules.Parse(map[string]any(definition.Content))
			if !ok {
				t.Fatalf("recipe %s failed strict parse", definition.Key)
			}
			recipes[definition.Key] = recipe
			for _, output := range append(append([]craftingrules.Product{}, recipe.Outputs...), recipe.FailureOutputs...) {
				if output.Kind == storage.DefinitionItem {
					obtainableItems[output.Key] = true
				}
			}
		case storage.DefinitionMarket:
			market, ok := marketrules.Parse(map[string]any(definition.Content))
			if !ok {
				t.Fatalf("market %s failed strict parse", definition.Key)
			}
			for _, entry := range market.Entries {
				if entry.BaseSellPrice >= entry.BaseBuyPrice {
					t.Fatalf("market %s entry %s:%s can be directly arbitraged: buy=%d sell=%d", definition.Key, entry.Kind, entry.Key, entry.BaseBuyPrice, entry.BaseSellPrice)
				}
				if entry.Kind == storage.DefinitionItem && entry.InitialStock > 0 {
					obtainableItems[entry.Key] = true
				}
			}
		case storage.DefinitionEncounter:
			encounter, ok := encounterrules.Parse(map[string]any(definition.Content))
			if !ok {
				t.Fatalf("encounter %s failed strict parse", definition.Key)
			}
			encounterLocations[definition.Key] = encounter.LocationKey
			for _, reward := range encounter.Rewards {
				obtainableItems[reward.ItemKey] = true
			}
		case storage.DefinitionLocation:
			nodeType, _ := definition.Content["node_type"].(string)
			if nodeType == "location" || nodeType == "scene" {
				travelable[definition.Key] = true
			}
			if raw, exists := definition.Content["gathering"]; exists {
				table, ok := gatheringrules.Parse(raw)
				if !ok {
					t.Fatalf("location %s gathering table failed strict parse", definition.Key)
				}
				for _, entry := range table.Entries {
					obtainableItems[entry.ItemKey] = true
				}
			}
			for _, rawConnection := range definitionSlice(definition.Content["connections"]) {
				connection := definitionMap(rawConnection)
				key, _ := connection["location_key"].(string)
				connections[definition.Key] = append(connections[definition.Key], key)
			}
		case storage.DefinitionQuest:
			quest, ok := worldrules.ParseQuest(map[string]any(definition.Content))
			if !ok {
				t.Fatalf("quest %s failed strict parse", definition.Key)
			}
			quests[definition.Key] = quest
			for _, reward := range quest.Rewards {
				if reward.Type == "item" {
					obtainableItems[reward.ItemKey] = true
				}
			}
		}
	}
	for encounterKey, locationKey := range encounterLocations {
		if !travelable[locationKey] {
			t.Fatalf("encounter %s uses non-travelable location %s", encounterKey, locationKey)
		}
	}

	for key, definition := range techniques {
		if starting, _ := definition.Content["starting_known"].(bool); starting {
			continue
		}
		source, _ := definition.Content["source_item_key"].(string)
		if source == "" || !obtainableItems[source] {
			t.Fatalf("technique %s has no obtainable source item %q", key, source)
		}
	}
	for key, recipe := range recipes {
		if !recipe.StartingKnown && (recipe.SourceItemKey == "" || !obtainableItems[recipe.SourceItemKey]) {
			t.Fatalf("recipe %s has no obtainable source item %q", key, recipe.SourceItemKey)
		}
		for _, ingredient := range recipe.Inputs {
			for _, option := range ingredient.Options {
				if !obtainableItems[option.ItemKey] {
					t.Fatalf("recipe %s material %s has no starting, market, gathering, quest or production source", key, option.ItemKey)
				}
			}
		}
	}

	for from, targets := range connections {
		if !travelable[from] {
			continue
		}
		for _, to := range targets {
			if !travelable[to] {
				t.Fatalf("travel connection %s -> %s points to a non-travelable node", from, to)
			}
			if !containsTestString(connections[to], from) {
				t.Fatalf("travel connection %s -> %s is not bidirectional", from, to)
			}
		}
	}
	assertReachableLocations(t, "green-river-village", travelable, connections)
	assertReachableQuests(t, quests)
}

func assertReachableLocations(t *testing.T, start string, locations map[string]bool, connections map[string][]string) {
	t.Helper()
	seen := map[string]bool{start: true}
	queue := []string{start}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, next := range connections[current] {
			if locations[next] && !seen[next] {
				seen[next] = true
				queue = append(queue, next)
			}
		}
	}
	for key := range locations {
		if !seen[key] {
			t.Fatalf("travelable location %s is unreachable from %s", key, start)
		}
	}
}

func assertReachableQuests(t *testing.T, quests map[string]worldrules.Quest) {
	t.Helper()
	seen := map[string]bool{}
	queue := []string{}
	for key, quest := range quests {
		if quest.Initial {
			seen[key] = true
			queue = append(queue, key)
		}
	}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		for _, next := range quests[current].UnlockQuestKeys {
			if !seen[next] {
				seen[next] = true
				queue = append(queue, next)
			}
		}
	}
	for key := range quests {
		if !seen[key] {
			t.Fatalf("quest %s is not reachable from an initial quest", key)
		}
	}
}

func testDefinitionInt(value any) (int64, bool) {
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

func definitionSlice(value any) []any {
	values, _ := value.([]any)
	return values
}

func definitionMap(value any) map[string]any {
	switch typed := value.(type) {
	case map[string]any:
		return typed
	case storage.JSONMap:
		return map[string]any(typed)
	default:
		return nil
	}
}

func containsTestString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func TestBuiltinDirectoriesReturnsSortedDirectoriesOnly(t *testing.T) {
	dataDir := t.TempDir()
	root := filepath.Join(dataDir, "preset_packs")
	for _, name := range []string{"z-pack", "a-pack"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("ignored"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := builtinDirectories(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(root, "a-pack"), filepath.Join(root, "z-pack")}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("builtinDirectories() = %v, want %v", got, want)
	}
}

func TestLoadBundledReincarnationTrials(t *testing.T) {
	root := filepath.Join("..", "..", "data", "preset_packs", "reincarnation-trials")
	loaded, err := loadDirectory(root, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if loaded.manifest.Version != "1.0.0" {
		t.Fatalf("version = %q, want 1.0.0", loaded.manifest.Version)
	}
	if loaded.hash == "" {
		t.Fatal("content hash is empty")
	}
	if err := challenge.ValidateDocument(&loaded.document, defaultRegistry()); err != nil {
		t.Fatalf("bundled challenge validation failed: %v", err)
	}
	contract, err := storage.DecodeGameplayRuntime(loaded.document.Runtime)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := contract.Actions.Capabilities["message_rewrite"]; ok {
		t.Fatalf("reincarnation-trials must not declare platform message rewrite capability: %#v", contract.Actions.Capabilities)
	}
	if contract.Presentation.ValueLabels["state.gameplay.realm_key"]["great-ascension"] != "大乘期" {
		t.Fatalf("reincarnation presentation labels were not imported: %#v", contract.Presentation.ValueLabels)
	}

	var prompt storage.PromptPreset
	var worldBook storage.WorldBook
	for _, asset := range loaded.document.Assets {
		switch {
		case asset.Kind == storage.ChallengeAssetPrompt && asset.Slug == "ascension-trials-v1":
			if err := json.Unmarshal(asset.Content, &prompt); err != nil {
				t.Fatal(err)
			}
		case asset.Kind == storage.ChallengeAssetWorldBook && asset.Slug == "ascension-system-v1":
			if err := json.Unmarshal(asset.Content, &worldBook); err != nil {
				t.Fatal(err)
			}
		}
	}
	if len(prompt.Segments) < 12 {
		t.Fatalf("prompt segments = %d, want at least 12 complete gameplay segments", len(prompt.Segments))
	}
	requiredSegments := map[string]bool{
		"system_master": false, "core_rules": false, "character_generation": false,
		"lifespan_management": false, "breakthrough_system": false,
		"ascension_orthodox": false, "ascension_void_rift": false,
		"ascension_token_acquisition": false, "ascension_immortal_token": false,
		"input_authority": false, "settlement_security": false, "agent_tool_loop": false,
	}
	var promptText strings.Builder
	for _, segment := range prompt.Segments {
		promptText.WriteString(segment.Content)
		if _, required := requiredSegments[segment.Key]; required {
			requiredSegments[segment.Key] = strings.TrimSpace(segment.Content) != ""
		}
	}
	for key, present := range requiredSegments {
		if !present {
			t.Fatalf("complete gameplay prompt is missing segment %q", key)
		}
	}
	if strings.Contains(promptText.String(), "trigger_ending") {
		t.Fatal("complete gameplay prompt still contains the retired ending action")
	}
	if !strings.Contains(promptText.String(), "二至四个明确可执行的方向") ||
		!strings.Contains(promptText.String(), "推进世界的责任推回给玩家") {
		t.Fatal("gameplay prompt lost its proactive turn-driving instructions")
	}
	if !strings.Contains(promptText.String(), "功法开局为空") ||
		!strings.Contains(promptText.String(), "不得把引气诀") {
		t.Fatal("reincarnation prompt must acquire varied techniques through play instead of granting a fixed starter")
	}
	if len(worldBook.Entries) < 12 {
		t.Fatalf("worldbook entries = %d, want at least 12 complete rule entries", len(worldBook.Entries))
	}
	requiredEntries := map[string]bool{
		"共同修仙境界规范": false, "共同修仙基础体系": false, "寿元系统": false, "境界突破机制": false, "延寿手段": false,
		"正统飞升（渡劫）": false, "虚空裂缝飞升": false, "仙缘传送飞升": false,
		"寿终正寝（坐化结局）": false, "寿元耗尽（强制死亡）": false,
		"寿元警告机制": false, "灵石的新定位": false,
	}
	for _, entry := range worldBook.Entries {
		if _, required := requiredEntries[entry.Title]; required {
			requiredEntries[entry.Title] = strings.TrimSpace(entry.Content) != ""
		}
	}
	for title, present := range requiredEntries {
		if !present {
			t.Fatalf("complete gameplay worldbook is missing entry %q", title)
		}
	}

	if len(contract.StateSchema.Fields) < 30 {
		t.Fatalf("state schema fields = %d, want complete ascension state", len(contract.StateSchema.Fields))
	}
	visiblePaths := map[string]bool{}
	for _, section := range contract.Presentation.StateSections {
		for _, field := range section.Fields {
			visiblePaths[field.Path] = true
		}
	}
	for _, path := range []string{
		"gameplay.birth", "gameplay.vigor", "gameplay.lifespan", "gameplay.remaining_days",
		"gameplay.inventory", "gameplay.goal", "gameplay.method", "gameplay.injury",
		"gameplay.rift_discovered", "gameplay.preparation",
	} {
		if !visiblePaths[path] {
			t.Fatalf("runtime presentation is missing important state path %q", path)
		}
	}
	for _, path := range []string{"module_state", "gameplay", "gameplay.realm_key", "gameplay.progress", "gameplay.health", "gameplay.outcome_key"} {
		field, ok := gameplay.FieldForPath(contract.StateSchema, path)
		if !ok || field.UpdatePolicy != gameplay.UpdateModule {
			t.Fatalf("trusted field %s is not module-owned", path)
		}
	}
	if contract.Engine.Version != 2 || len(contract.Actions.Domain) < 15 || strings.Contains(strings.Join(contract.Actions.Model, ","), "roll") {
		t.Fatal("trial must use the version 2 trusted action catalogue")
	}

}

func TestBundledCultivationPacksSharePublicSettingRules(t *testing.T) {
	root := filepath.Join("..", "..", "data", "preset_packs")
	campaign, err := loadDirectory(filepath.Join(root, "mortal-path"), time.Now(), defaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	trial, err := loadDirectory(filepath.Join(root, "reincarnation-trials"), time.Now(), defaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	campaignRules := sharedRuleSnapshots(campaign.document.Assets)
	trialRules := sharedRuleSnapshots(trial.document.Assets)
	for _, key := range []string{"cultivation.realm-catalog.v1", "cultivation.foundation-concepts.v1"} {
		left, leftOK := campaignRules[key]
		right, rightOK := trialRules[key]
		if !leftOK || !rightOK || left != right {
			t.Fatalf("shared public rule %s drifted: campaign=%q trial=%q", key, left, right)
		}
	}

	campaignRealms := realmDefinitionsByRank(campaign.document.Definitions)
	if len(trial.document.Definitions) != 1 {
		t.Fatal("trial rule singleton missing")
	}
	trialRealms, ok := trial.document.Definitions[0].Content["realms"].([]any)
	if !ok || len(trialRealms) != len(campaignRealms) {
		t.Fatal("realm catalogues differ in length")
	}
	for i, realm := range campaignRealms {
		r := trialRealms[i].(map[string]any)
		lifespanDays, daysOK := testDefinitionInt(realm.Content["lifespan_days"])
		years, yearsOK := testDefinitionInt(r["lifespan_years"])
		if r["key"] != realm.Key || r["name"] == "" || !daysOK || !yearsOK || years*365 != lifespanDays {
			t.Fatalf("shared realm differs at rank %d: campaign=%+v trial=%+v", i, realm, r)
		}
		if i+1 < len(campaignRealms) && realm.Content["next_realm_key"] != campaignRealms[i+1].Key {
			t.Fatal("campaign realm chain broken")
		}
	}

}

func sharedRuleSnapshots(assets []storage.ChallengeAsset) map[string]string {
	out := map[string]string{}
	for _, asset := range assets {
		if asset.Kind != storage.ChallengeAssetWorldBook {
			continue
		}
		var book storage.WorldBook
		if err := json.Unmarshal(asset.Content, &book); err != nil {
			continue
		}
		for _, entry := range book.Entries {
			key, _ := entry.Metadata["shared_rule_id"].(string)
			if key != "" {
				out[key] = entry.Title + "\n" + entry.Content
			}
		}
	}
	return out
}

func containsValue(values []any, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func TestLoadGenericMidnightCaseFixture(t *testing.T) {
	root := filepath.Join("testdata", "midnight-case")
	loaded, err := loadDirectory(root, time.Now(), defaultRegistry())
	if err != nil {
		t.Fatal(err)
	}
	if loaded.document.Runtime.EngineKey != "core.generic" {
		t.Fatalf("engine = %q, want core.generic", loaded.document.Runtime.EngineKey)
	}
	if len(loaded.document.Outcomes) == 0 || loaded.document.Outcomes[0].RewardPoints != 100 {
		t.Fatal("missing generic fixed points")
	}
	for _, forbidden := range []string{"修仙", "境界", "灵石", "飞升", "天道"} {
		if strings.Contains(string(loaded.document.Version.Manifest), forbidden) {
			t.Fatalf("generic fixture manifest contains cultivation term %q", forbidden)
		}
	}
	if err := challenge.ValidateDocument(&loaded.document, defaultRegistry()); err != nil {
		t.Fatalf("generic fixture validation failed: %v", err)
	}
}

func TestBuiltinDirectoriesRejectsEmptyRoot(t *testing.T) {
	dataDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dataDir, "preset_packs"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := builtinDirectories(dataDir)
	if err == nil || !strings.Contains(err.Error(), "没有找到内置挑战") {
		t.Fatalf("builtinDirectories() error = %v, want empty-root error", err)
	}
}

func TestDirectoryBySlugDoesNotAcceptFilesystemPaths(t *testing.T) {
	dataDir := filepath.Join("..", "..", "data")
	importer := New(nil, dataDir)
	directory, err := importer.directoryBySlug("reincarnation-trials")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(directory) != "reincarnation-trials" {
		t.Fatalf("directory = %q", directory)
	}
	if _, err := importer.directoryBySlug("../reincarnation-trials"); err != ErrBuiltinNotFound {
		t.Fatalf("path-like slug error = %v, want ErrBuiltinNotFound", err)
	}
}

func TestCompareStableVersions(t *testing.T) {
	tests := []struct {
		left  string
		right string
		want  int
	}{
		{left: "1.0.0", right: "1.0.0", want: 0},
		{left: "1.1.0", right: "1.0.9", want: 1},
		{left: "2.0.0", right: "10.0.0", want: -1},
	}
	for _, test := range tests {
		got, err := compareStableVersions(test.left, test.right)
		if err != nil {
			t.Fatal(err)
		}
		if got != test.want {
			t.Fatalf("compareStableVersions(%q, %q) = %d, want %d", test.left, test.right, got, test.want)
		}
	}
	for _, invalid := range []string{"1", "1.0", "01.0.0", "1.0.0-beta"} {
		if _, err := parseStableVersion(invalid); err == nil {
			t.Fatalf("parseStableVersion(%q) error = nil", invalid)
		}
	}
}

func TestClassifyBuiltin(t *testing.T) {
	tests := []struct {
		name                      string
		sourceVersion, sourceHash string
		current, existing         *storage.ChallengeVersion
		status                    string
		importable, overwritable  bool
	}{
		{
			name: "new content", sourceVersion: "1.0.0", sourceHash: "source",
			status: StatusAvailable, importable: true,
		},
		{
			name: "current content matches", sourceVersion: "1.0.0", sourceHash: "source",
			current:  builtinVersion("1.0.0", storage.ChallengeVersionPublished, "source"),
			existing: builtinVersion("1.0.0", storage.ChallengeVersionPublished, "source"),
			status:   StatusImported,
		},
		{
			name: "same version changed", sourceVersion: "1.0.0", sourceHash: "source",
			current:  builtinVersion("1.0.0", storage.ChallengeVersionPublished, "database"),
			existing: builtinVersion("1.0.0", storage.ChallengeVersionPublished, "database"),
			status:   StatusUpdateAvailable,
		},
		{
			name: "higher version can import", sourceVersion: "1.1.0", sourceHash: "source",
			current: builtinVersion("1.0.0", storage.ChallengeVersionPublished, "database"),
			status:  StatusAvailable, importable: true,
		},
		{
			name: "lower version can import as draft", sourceVersion: "1.0.0", sourceHash: "source",
			current: builtinVersion("1.1.0", storage.ChallengeVersionPublished, "database"),
			status:  StatusAvailable, importable: true,
		},
		{
			name: "matching source exists but is not current", sourceVersion: "1.0.0", sourceHash: "source",
			current:  builtinVersion("1.1.0", storage.ChallengeVersionPublished, "database"),
			existing: builtinVersion("1.0.0", storage.ChallengeVersionPublished, "source"),
			status:   StatusUpdateAvailable,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			status, importable, overwritable := classifyBuiltin(
				test.sourceVersion, test.sourceHash, test.current, test.existing,
			)
			if status != test.status || importable != test.importable || overwritable != test.overwritable {
				t.Fatalf(
					"classifyBuiltin() = (%q, %t, %t), want (%q, %t, %t)",
					status, importable, overwritable, test.status, test.importable, test.overwritable,
				)
			}
		})
	}
}

func builtinVersion(version, status, hash string) *storage.ChallengeVersion {
	return &storage.ChallengeVersion{Version: version, Status: status, ContentHash: hash}
}
