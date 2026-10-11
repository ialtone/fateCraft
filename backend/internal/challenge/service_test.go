package challenge

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/ialtone/fatecraft/internal/gameplay/campaigngeneric"
	gameplay "github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation"
	"github.com/ialtone/fatecraft/internal/storage"
)

func testRegistry() *gameplay.Registry {
	registry := gameplay.NewCoreRegistry()
	cultivation.Register(registry)
	return registry
}

func validVersionDocument(t testing.TB) *VersionDocument {
	t.Helper()
	manifest := Manifest{
		ID: "test-pack", Name: "Test Pack", Version: "1.0.0", Description: "test",
		DefaultConfig: DefaultConfig{
			RecommendedCharacter: "character", RecommendedPersona: "persona",
			RecommendedScenario: "scenario", RecommendedPrompt: "prompt",
			AllowedPersonas: []string{"persona"},
		},
	}
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	outcomeID := uuid.New()
	return &VersionDocument{
		Version: storage.ChallengeVersion{Version: manifest.Version, Manifest: storage.JSONDoc(manifestJSON)},
		Runtime: storage.ChallengeRuntimeConfig{
			EngineKey: "core.generic", EngineVersion: 1,
			StateSchema:      storage.JSONDoc(`{"schema_version":1,"fields":[{"path":"score","type":"integer","default":0,"visibility":"player","update_policy":"model"}]}`),
			ActionConfig:     storage.JSONDoc(`{"model":["complete_challenge"],"player":[]}`),
			SettlementConfig: storage.JSONDoc(`{"handler":"core.challenge_completion"}`),
			Presentation:     storage.JSONDoc(`{"state_panel_title":"Status","state_sections":[],"completion_view":{}}`),
		},
		Assets: []storage.ChallengeAsset{
			{Kind: storage.ChallengeAssetCharacter, Slug: "character", Name: "Character", Content: storage.JSONDoc(`{"id":"character"}`)},
			{Kind: storage.ChallengeAssetPersona, Slug: "persona", Name: "Persona", Content: storage.JSONDoc(`{"id":"persona"}`)},
			{Kind: storage.ChallengeAssetScenario, Slug: "scenario", Name: "Scenario", Content: storage.JSONDoc(`{"id":"scenario"}`)},
			{Kind: storage.ChallengeAssetPrompt, Slug: "prompt", Name: "Prompt", Content: storage.JSONDoc(`{"id":"prompt","segments":[{"key":"system_master","role":"system","content":"system","enabled":true,"required":true},{"key":"settlement_security","role":"system","content":"success","enabled":true,"required":true},{"key":"agent_tool_loop","role":"system","content":"tools","enabled":true,"required":true}]}`)},
		},
		Outcomes: []storage.ChallengeOutcomeRule{{
			ID: outcomeID, OutcomeKey: "success", Category: "success", Title: "Success", Tone: "success",
			VerifierKey: "core.always", RewardPoints: 10, VerifierConfig: storage.JSONMap{}, Enabled: true,
		}},
	}
}

func TestContentHashCanonicalizesJSONAndDoesNotMutateInput(t *testing.T) {
	docA := validVersionDocument(t)
	docB := *docA
	docB.Assets = append([]storage.ChallengeAsset(nil), docA.Assets...)
	docB.Outcomes = append([]storage.ChallengeOutcomeRule(nil), docA.Outcomes...)
	for left, right := 0, len(docB.Assets)-1; left < right; left, right = left+1, right-1 {
		docB.Assets[left], docB.Assets[right] = docB.Assets[right], docB.Assets[left]
	}
	var manifest any
	if err := json.Unmarshal(docB.Version.Manifest, &manifest); err != nil {
		t.Fatal(err)
	}
	docB.Version.Manifest, _ = json.Marshal(manifest)
	hashA, err := ContentHashDocument(docA)
	if err != nil {
		t.Fatal(err)
	}
	hashB, err := ContentHashDocument(&docB)
	if err != nil {
		t.Fatal(err)
	}
	if hashA != hashB {
		t.Fatalf("canonical hashes differ: %s != %s", hashA, hashB)
	}
	if docA.Assets[0].Slug != "character" {
		t.Fatal("ContentHashDocument mutated caller slices")
	}
}

func TestContentHashIncludesCampaignConfig(t *testing.T) {
	docA := validVersionDocument(t)
	docA.Runtime.EngineKey = cultivation.CampaignEngineKey
	docA.Runtime.Mode = gameplay.RuntimeModeCampaign
	campaignConfig := storage.JSONDoc(`{
		"lifecycle_handler":"cultivation.campaign_lifecycle",
		"projection_handler":"cultivation.campaign_projection",
		"context_policy":"cultivation.campaign_context",
		"action_allowlist":["cultivation.practice"],
		"initial_location_key":"world.start",
		"world_clock":{"calendar_key":"world.default","max_due_events_per_turn":16}
	}`)
	docA.Runtime.CampaignConfig = &campaignConfig
	docB := *docA
	docB.Runtime = docA.Runtime
	otherConfig := storage.JSONDoc(strings.Replace(string(*docA.Runtime.CampaignConfig), "world.start", "world.other", 1))
	docB.Runtime.CampaignConfig = &otherConfig
	hashA, err := ContentHashDocument(docA)
	if err != nil {
		t.Fatal(err)
	}
	hashB, err := ContentHashDocument(&docB)
	if err != nil {
		t.Fatal(err)
	}
	if hashA == hashB {
		t.Fatal("campaign_config change did not affect content hash")
	}
}

func TestValidateDocumentRequiresSettlementSecurity(t *testing.T) {
	doc := validVersionDocument(t)
	prompt := &doc.Assets[len(doc.Assets)-1]
	prompt.Content = storage.JSONDoc(strings.ReplaceAll(string(prompt.Content), "settlement_security", "optional_notes"))
	err := ValidateDocument(doc, testRegistry())
	if err == nil || !strings.Contains(err.Error(), "settlement_security") {
		t.Fatalf("ValidateDocument() error = %v", err)
	}
}

func TestValidateDocumentRejectsPlatformCapabilityInRuntime(t *testing.T) {
	doc := validVersionDocument(t)
	doc.Runtime.ActionConfig = storage.JSONDoc(`{"model":["complete_challenge"],"player":["message_rewrite"],"capabilities":{"message_rewrite":{"enabled":true,"cost_reward_key":"fatecraft.message-rewrite-credit","cost_quantity":1}}}`)
	err := ValidateDocument(doc, testRegistry())
	if err == nil || !strings.Contains(err.Error(), "不得声明平台能力") {
		t.Fatalf("ValidateDocument() error = %v, want platform capability rejection", err)
	}
}

func TestPublicGameplayCapabilitiesHidePlatformKeys(t *testing.T) {
	visible := publicGameplayCapabilities(map[string]gameplay.Capability{
		"domain_action": {}, "message_rewrite": {Enabled: true},
	})
	if _, ok := visible["message_rewrite"]; ok {
		t.Fatal("platform capability leaked into gameplay capability projection")
	}
	if _, ok := visible["domain_action"]; !ok {
		t.Fatal("gameplay capability was removed from public projection")
	}
}

func TestValidateDocumentCampaignDoesNotRequireSingleRunSettlement(t *testing.T) {
	doc := validVersionDocument(t)
	contract := gameplay.RuntimeContract{
		Engine: gameplay.EngineRef{Key: cultivation.CampaignEngineKey, Version: cultivation.CampaignEngineVersion},
		Mode:   gameplay.RuntimeModeCampaign,
		Campaign: &gameplay.CampaignConfig{
			LifecycleHandler:  cultivation.CampaignLifecycleHandler,
			ProjectionHandler: cultivation.CampaignProjectionHandler,
			ContextPolicy:     cultivation.CampaignContextPolicy, ActionAllowlist: []string{"cultivation.practice", "campaign.abandon"}, InitialLocationKey: "world.start",
			FieldOwnership: []gameplay.FieldOwnership{{Path: "character", Owner: gameplay.OwnerCampaign, ValueType: "object", Visibility: gameplay.VisibilityPlayer, StorageProjection: "campaign_characters"}},
			WorldClock:     gameplay.WorldClockConfig{CalendarKey: "world.default", MaxDueEventsPerTurn: 16},
		},
		StateSchema: gameplay.StateSchema{SchemaVersion: 1, Fields: []gameplay.StateField{{
			Path: "narrative.current_focus", Type: "string", Visibility: gameplay.VisibilityPlayer,
			UpdatePolicy: gameplay.UpdateModel,
		}}},
		Actions:      gameplay.ActionConfig{Model: []string{"update_state", "domain_action"}},
		Presentation: gameplay.Presentation{StatePanelTitle: "状态"},
	}
	runtime, err := storage.EncodeGameplayRuntime(contract)
	if err != nil {
		t.Fatal(err)
	}
	doc.Runtime = runtime
	doc.Definitions = []storage.GameplayDefinition{
		{Kind: storage.DefinitionRealm, Key: "mortal", Name: "凡人", Visibility: storage.CampaignVisibilityPlayer,
			Content: storage.JSONMap{"rank": float64(0), "cultivation_required": float64(100), "lifespan_days": float64(36500), "stages": []any{"initial"}}},
		{Kind: storage.DefinitionLocation, Key: "world", Name: "世界", Visibility: storage.CampaignVisibilityPlayer,
			Content: storage.JSONMap{"node_type": "realm", "connections": []any{}}},
		{Kind: storage.DefinitionLocation, Key: "world.region", Name: "区域", Visibility: storage.CampaignVisibilityPlayer,
			Content: storage.JSONMap{"node_type": "region", "parent_key": "world", "connections": []any{}}},
		{Kind: storage.DefinitionLocation, Key: "world.start", Name: "起点", Visibility: storage.CampaignVisibilityPlayer,
			Content: storage.JSONMap{"node_type": "location", "parent_key": "world.region", "connections": []any{}}},
	}
	doc.Outcomes = nil
	prompt := &doc.Assets[len(doc.Assets)-1]
	prompt.Content = storage.JSONDoc(strings.ReplaceAll(string(prompt.Content), "settlement_security", "campaign_rules"))
	if err := ValidateDocument(doc, testRegistry()); err != nil {
		t.Fatalf("campaign document rejected: %v", err)
	}
}

func TestValidateDocumentGenericCampaignWithoutRealm(t *testing.T) {
	doc := validVersionDocument(t)
	contract := gameplay.RuntimeContract{
		Engine: gameplay.EngineRef{Key: "campaign.generic", Version: 1},
		Mode:   gameplay.RuntimeModeCampaign,
		Campaign: &gameplay.CampaignConfig{
			LifecycleHandler:   "campaign.generic_lifecycle",
			ProjectionHandler:  "campaign.generic_projection",
			ContextPolicy:      "campaign.generic_context",
			ActionAllowlist:    []string{"travel.move"},
			FieldOwnership:     []gameplay.FieldOwnership{{Path: "world", Owner: gameplay.OwnerCampaign, ValueType: "object", Visibility: gameplay.VisibilityPlayer, StorageProjection: "campaigns"}},
			InitialLocationKey: "world.start",
			WorldClock:         gameplay.WorldClockConfig{CalendarKey: "world.default", MaxDueEventsPerTurn: 16},
		},
		StateSchema: gameplay.StateSchema{SchemaVersion: 1, Fields: []gameplay.StateField{{
			Path: "narrative.current_focus", Type: "string", Visibility: gameplay.VisibilityPlayer,
			UpdatePolicy: gameplay.UpdateModel,
		}}},
		Actions:      gameplay.ActionConfig{Model: []string{"update_state", "domain_action"}},
		Presentation: gameplay.Presentation{StatePanelTitle: "World"},
	}
	runtime, err := storage.EncodeGameplayRuntime(contract)
	if err != nil {
		t.Fatal(err)
	}
	doc.Runtime = runtime
	doc.Definitions = []storage.GameplayDefinition{
		{Kind: storage.DefinitionLocation, Key: "world", Name: "World", Visibility: storage.CampaignVisibilityPlayer,
			Content: storage.JSONMap{"node_type": "world", "connections": []any{}}},
		{Kind: storage.DefinitionLocation, Key: "world.region", Name: "Region", Visibility: storage.CampaignVisibilityPlayer,
			Content: storage.JSONMap{"node_type": "region", "parent_key": "world", "connections": []any{}}},
		{Kind: storage.DefinitionLocation, Key: "world.start", Name: "Start", Visibility: storage.CampaignVisibilityPlayer,
			Content: storage.JSONMap{"node_type": "location", "parent_key": "world.region", "connections": []any{}}},
	}
	doc.Outcomes = nil
	registry := gameplay.NewCoreRegistry()
	campaigngeneric.Register(registry)
	if err := ValidateDocument(doc, registry); err != nil {
		t.Fatalf("realm-free generic campaign rejected: %v", err)
	}
}

func TestValidateDocumentRequiresRecommendedPersonaToBeAllowed(t *testing.T) {
	doc := validVersionDocument(t)
	var manifest Manifest
	if err := json.Unmarshal(doc.Version.Manifest, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.DefaultConfig.AllowedPersonas = []string{"other-persona"}
	doc.Version.Manifest, _ = json.Marshal(manifest)
	doc.Assets = append(doc.Assets, storage.ChallengeAsset{
		Kind: storage.ChallengeAssetPersona, Slug: "other-persona", Name: "Other",
		Content: storage.JSONDoc(`{"id":"other-persona"}`),
	})
	err := ValidateDocument(doc, testRegistry())
	if err == nil || !strings.Contains(err.Error(), "recommended_persona") {
		t.Fatalf("ValidateDocument() error = %v", err)
	}
}

type settlementReaderStub struct {
	outcome    storage.ChallengeOutcomeRule
	successful []string
	runtime    storage.ChallengeRuntimeConfig
}

func (s settlementReaderStub) GetRun(context.Context, uuid.UUID) (*storage.ChallengeRun, error) {
	return nil, storage.ErrNotFound
}
func (s settlementReaderStub) OutcomeRule(context.Context, uuid.UUID, string) (*storage.ChallengeOutcomeRule, error) {
	return &s.outcome, nil
}
func (s settlementReaderStub) SuccessfulRollKeys(context.Context, uuid.UUID) ([]string, error) {
	return s.successful, nil
}
func (s settlementReaderStub) GetRuntime(context.Context, uuid.UUID) (*storage.ChallengeRuntimeConfig, error) {
	return &s.runtime, nil
}

func cultivationSettlementFixture() (settlementReaderStub, *storage.Conversation, uuid.UUID) {
	playerID := uuid.New()
	versionID := uuid.New()
	outcomeID := uuid.New()
	return settlementReaderStub{
			outcome: storage.ChallengeOutcomeRule{
				ID: outcomeID, OutcomeKey: "affinity_ascension", Category: "success", Title: "仙缘飞升", Tone: "success",
				VerifierKey: "core.conditions", RewardPoints: 150,
				VerifierConfig: storage.JSONMap{"conditions": []any{
					map[string]any{"type": "state_in", "path": "profile.cultivation_realm_key", "values": []any{"golden-core", "nascent-soul"}},
					map[string]any{"type": "successful_roll", "key": "筑基突破判定"},
					map[string]any{"type": "successful_roll", "key": "结丹突破判定"},
					map[string]any{"type": "successful_roll", "key": "仙缘获取判定"},
				}},
				Enabled: true,
			},
			successful: []string{"筑基突破判定", "结丹突破判定", "仙缘获取判定"},
			runtime: storage.ChallengeRuntimeConfig{
				EngineKey: "core.generic", EngineVersion: 1,
				StateSchema:      storage.JSONDoc(`{"schema_version":1,"fields":[{"path":"profile","type":"object","visibility":"player","update_policy":"model","allow_children":true},{"path":"profile.cultivation_realm_key","type":"enum","default":"mortal","visibility":"player","update_policy":"system","constraints":{"values":["mortal","qi-refining","golden-core","nascent-soul"]}}]}`),
				ActionConfig:     storage.JSONDoc(`{"model":["complete_challenge"],"player":[]}`),
				SettlementConfig: storage.JSONDoc(`{"handler":"core.challenge_completion"}`),
				Presentation:     storage.JSONDoc(`{"state_panel_title":"角色状态","state_sections":[],"completion_view":{}}`),
			},
		}, &storage.Conversation{
			ID: uuid.New(), PlayerID: playerID.String(), Status: storage.StatusActive, ChallengeVersionID: versionID,
		}, playerID
}

func TestEvaluateSettlementRejectsFabricatedAffinityEndingAtQiRefining(t *testing.T) {
	repo, conv, playerID := cultivationSettlementFixture()
	service := NewService(nil, testRegistry())
	_, err := service.evaluateSettlement(context.Background(), repo, conv, SettlementInput{
		ConversationID: conv.ID, PlayerID: playerID, OutcomeKey: "affinity_ascension",
		TrustedState: map[string]any{"profile": map[string]any{"cultivation_realm_key": "qi-refining"}, "attributes": map[string]any{"灵石": 999999}},
	})
	if !errors.Is(err, ErrSettlementDenied) || !strings.Contains(err.Error(), "状态条件") {
		t.Fatalf("evaluateSettlement() error = %v", err)
	}
}

func TestEvaluateSettlementUsesPublishedFixedPoints(t *testing.T) {
	repo, conv, playerID := cultivationSettlementFixture()
	service := NewService(nil, testRegistry())
	evaluation, err := service.evaluateSettlement(context.Background(), repo, conv, SettlementInput{
		ConversationID: conv.ID, PlayerID: playerID, OutcomeKey: "affinity_ascension",
		TrustedState: map[string]any{"profile": map[string]any{"cultivation_realm_key": "golden-core"}, "attributes": map[string]any{"灵石": 100}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if evaluation.points != 150 {
		t.Fatalf("grants = %#v", evaluation.points)
	}
}

func TestValidateDefinitionDocumentsNormalizesNilTags(t *testing.T) {
	definitions := []storage.GameplayDefinition{{
		Kind: storage.DefinitionRealm, Key: "mortal", Name: "凡人",
		Visibility: storage.CampaignVisibilityPlayer,
		Content: storage.JSONMap{
			"rank": int64(0), "cultivation_required": int64(100),
			"lifespan_days": int64(36500), "stages": []any{"initial"},
		},
	}}
	if err := validateDefinitionDocuments(definitions); err != nil {
		t.Fatal(err)
	}
	if definitions[0].Tags == nil || len(definitions[0].Tags) != 0 {
		t.Fatalf("tags were not normalized: %#v", definitions[0].Tags)
	}
}

func TestBundledMortalPathPhaseFourDefinitionsPassPublishValidation(t *testing.T) {
	directory := filepath.Join("..", "..", "data", "preset_packs", "mortal-path")
	definitions := readBundledDefinitionCatalog(t, directory)
	if err := validateDefinitionDocuments(definitions); err != nil {
		t.Fatal(err)
	}
	contract := readBundledRuntimeContract(t, directory)
	if err := gameplay.ValidateContract(&contract); err != nil {
		t.Fatal(err)
	}
	if err := validateCampaignDefinitionReferences(&contract, definitions); err != nil {
		t.Fatal(err)
	}
}

func readBundledRuntimeContract(t *testing.T, directory string) gameplay.RuntimeContract {
	t.Helper()
	type includes struct {
		Campaign     string `json:"campaign"`
		StateSchema  string `json:"state_schema"`
		Actions      string `json:"actions"`
		Settlement   string `json:"settlement"`
		Presentation string `json:"presentation"`
	}
	var entry struct {
		Engine   gameplay.EngineRef `json:"engine"`
		Mode     string             `json:"mode"`
		Includes includes           `json:"includes"`
	}
	entryBytes, err := os.ReadFile(filepath.Join(directory, "runtime", "runtime.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(entryBytes, &entry); err != nil {
		t.Fatal(err)
	}
	read := func(name string, target any) {
		raw, err := os.ReadFile(filepath.Join(directory, "runtime", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, target); err != nil {
			t.Fatal(err)
		}
	}
	contract := gameplay.RuntimeContract{Engine: entry.Engine, Mode: entry.Mode}
	var statePart struct {
		StateSchema gameplay.StateSchema `json:"state_schema"`
	}
	var actionsPart struct {
		Actions gameplay.ActionConfig `json:"actions"`
	}
	var settlementPart struct {
		Settlement gameplay.SettlementConfig `json:"settlement"`
	}
	var presentationPart struct {
		Presentation gameplay.Presentation `json:"presentation"`
	}
	read(entry.Includes.StateSchema, &statePart)
	read(entry.Includes.Actions, &actionsPart)
	read(entry.Includes.Settlement, &settlementPart)
	read(entry.Includes.Presentation, &presentationPart)
	contract.StateSchema = statePart.StateSchema
	contract.Actions = actionsPart.Actions
	contract.Settlement = settlementPart.Settlement
	contract.Presentation = presentationPart.Presentation
	if entry.Includes.Campaign != "" {
		var campaignPart struct {
			Campaign gameplay.CampaignConfig `json:"campaign"`
		}
		read(entry.Includes.Campaign, &campaignPart)
		contract.Campaign = &campaignPart.Campaign
	}
	return contract
}

func TestValidateCampaignDefinitionReferencesRejectsInvalidRealmProgression(t *testing.T) {
	contract := &gameplay.RuntimeContract{Campaign: &gameplay.CampaignConfig{InitialLocationKey: "start"}}
	validDefinitions := func() []storage.GameplayDefinition {
		return []storage.GameplayDefinition{
			{Kind: storage.DefinitionRealm, Key: "first", Content: storage.JSONMap{"rank": int64(3), "next_realm_key": "second"}},
			{Kind: storage.DefinitionRealm, Key: "second", Content: storage.JSONMap{"rank": int64(7)}},
			{Kind: storage.DefinitionLocation, Key: "world", Content: storage.JSONMap{"node_type": "realm", "connections": []any{}}},
			{Kind: storage.DefinitionLocation, Key: "region", Content: storage.JSONMap{"node_type": "region", "parent_key": "world", "connections": []any{}}},
			{Kind: storage.DefinitionLocation, Key: "start", Content: storage.JSONMap{"node_type": "location", "parent_key": "region", "connections": []any{}}},
		}
	}
	if err := validateCampaignDefinitionReferences(contract, validDefinitions()); err != nil {
		t.Fatalf("valid realm progression was rejected: %v", err)
	}
	tests := []struct {
		name    string
		mutate  func([]storage.GameplayDefinition)
		message string
	}{
		{
			name: "duplicate rank",
			mutate: func(definitions []storage.GameplayDefinition) {
				delete(definitions[0].Content, "next_realm_key")
				definitions[1].Content["rank"] = int64(3)
			},
			message: "相同 rank",
		},
		{
			name: "descending progression",
			mutate: func(definitions []storage.GameplayDefinition) {
				definitions[0].Content["rank"] = int64(8)
			},
			message: "更高 rank",
		},
		{
			name: "unknown next realm",
			mutate: func(definitions []storage.GameplayDefinition) {
				definitions[0].Content["next_realm_key"] = "missing"
			},
			message: "未知 next_realm_key",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			definitions := validDefinitions()
			test.mutate(definitions)
			err := validateCampaignDefinitionReferences(contract, definitions)
			if err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("validation error = %v, want message containing %q", err, test.message)
			}
		})
	}
}

func TestValidateRealmDefinitionRejectsInvalidNextKey(t *testing.T) {
	definition := storage.GameplayDefinition{
		Kind: storage.DefinitionRealm, Key: "first", Name: "First", Visibility: storage.CampaignVisibilityPlayer,
		Content: storage.JSONMap{
			"rank": int64(0), "cultivation_required": int64(100), "lifespan_days": int64(1000),
			"stages": []any{"initial"}, "next_realm_key": "first",
		},
	}
	if err := validateDefinitionDocuments([]storage.GameplayDefinition{definition}); err == nil || !strings.Contains(err.Error(), "next_realm_key") {
		t.Fatalf("self-referencing next realm was accepted: %v", err)
	}
}

func readBundledDefinitionCatalog(t *testing.T, directory string) []storage.GameplayDefinition {
	t.Helper()
	files := []string{filepath.Join(directory, "definitions.json")}
	fragments, err := filepath.Glob(filepath.Join(directory, "definitions", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(fragments)
	files = append(files, fragments...)
	definitions := make([]storage.GameplayDefinition, 0)
	for _, file := range files {
		raw, err := os.ReadFile(file)
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
		definitions = append(definitions, rows...)
	}
	return definitions
}

func TestValidateDefinitionShapeAcceptsBoundedSpawnableNPC(t *testing.T) {
	err := validateDefinitionShape(storage.GameplayDefinition{
		Kind: storage.DefinitionNPCArchetype,
		Content: storage.JSONMap{
			"spawnable": true, "max_instances": int64(8),
			"allowed_locations": []any{"world.start"}, "realm_key": "mortal",
			"public_profile": map[string]any{}, "private_state": map[string]any{}, "goals": map[string]any{},
		},
	})
	if err != nil {
		t.Fatalf("spawnable archetype without fixed instance_key was rejected: %v", err)
	}
}

func TestValidateEncounterDefinitionShapeAndReferences(t *testing.T) {
	encounter := storage.GameplayDefinition{Kind: storage.DefinitionEncounter, Key: "arena-guardian", Content: storage.JSONMap{
		"location_key": "arena", "opponent_key": "guardian", "opponent_name": "守关傀儡",
		"opponent_realm_rank": int64(1), "opponent_max_health": int64(100), "opponent_attack": int64(10),
		"opponent_defense": int64(5), "player_damage_base": int64(4), "player_damage_scale_bps": int64(10_000),
		"player_mana_cost": int64(1), "round_minutes": int64(5), "max_rounds": int64(20), "flee_chance_bps": int64(2_000),
		"rewards": []any{map[string]any{"item_key": "guardian-core", "quantity": int64(1)}},
	}}
	if err := validateDefinitionShape(encounter); err != nil {
		t.Fatalf("valid encounter shape rejected: %v", err)
	}
	invalidShape := encounter
	invalidShape.Content = cloneJSONMap(encounter.Content)
	invalidShape.Content["winner"] = "model"
	if err := validateDefinitionShape(invalidShape); err == nil || !strings.Contains(err.Error(), "遭遇") {
		t.Fatalf("unknown encounter field error = %v", err)
	}

	contract := &gameplay.RuntimeContract{Campaign: &gameplay.CampaignConfig{InitialLocationKey: "arena"}}
	definitions := []storage.GameplayDefinition{
		{Kind: storage.DefinitionRealm, Key: "mortal", Content: storage.JSONMap{"rank": int64(0)}},
		{Kind: storage.DefinitionLocation, Key: "world", Content: storage.JSONMap{"node_type": "realm", "connections": []any{}}},
		{Kind: storage.DefinitionLocation, Key: "county", Content: storage.JSONMap{"node_type": "region", "parent_key": "world", "connections": []any{}}},
		{Kind: storage.DefinitionLocation, Key: "arena", Content: storage.JSONMap{"node_type": "location", "parent_key": "county", "connections": []any{}}},
		{Kind: storage.DefinitionItem, Key: "guardian-core", Content: storage.JSONMap{"stackable": true}},
		encounter,
	}
	if err := validateCampaignDefinitionReferences(contract, definitions); err != nil {
		t.Fatalf("valid encounter references rejected: %v", err)
	}
	missingLocation := append([]storage.GameplayDefinition(nil), definitions...)
	missingLocation[5].Content = cloneJSONMap(encounter.Content)
	missingLocation[5].Content["location_key"] = "missing-arena"
	if err := validateCampaignDefinitionReferences(contract, missingLocation); err == nil || !strings.Contains(err.Error(), "location_key") {
		t.Fatalf("missing encounter location error = %v", err)
	}
	nonStackableReward := append([]storage.GameplayDefinition(nil), definitions...)
	nonStackableReward[4].Content = storage.JSONMap{"stackable": false}
	if err := validateCampaignDefinitionReferences(contract, nonStackableReward); err == nil || !strings.Contains(err.Error(), "可堆叠") {
		t.Fatalf("invalid encounter reward error = %v", err)
	}
}

func cloneJSONMap(source storage.JSONMap) storage.JSONMap {
	clone := make(storage.JSONMap, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
}

func TestValidateDefinitionShapeRejectsInvalidFactionPermissionsAndEntryRequirements(t *testing.T) {
	if err := validateDefinitionShape(storage.GameplayDefinition{
		Kind:    storage.DefinitionFaction,
		Content: storage.JSONMap{"permission_thresholds": map[string]any{"Outer Gate": float64(100)}},
	}); err == nil || !strings.Contains(err.Error(), "permission_thresholds") {
		t.Fatalf("invalid permission key error = %v", err)
	}
	if err := validateDefinitionShape(storage.GameplayDefinition{
		Kind:    storage.DefinitionFaction,
		Content: storage.JSONMap{"permission_thresholds": map[string]any{"outer_gate": float64(10001)}},
	}); err == nil || !strings.Contains(err.Error(), "permission_thresholds") {
		t.Fatalf("unbounded permission threshold error = %v", err)
	}
	if err := validateDefinitionShape(storage.GameplayDefinition{
		Kind: storage.DefinitionLocation,
		Content: storage.JSONMap{
			"node_type":   "location",
			"connections": []any{},
			"entry_requirements": []any{map[string]any{
				"faction_key": "sect", "permission_key": "outer_gate", "typo": true,
			}},
		},
	}); err == nil || !strings.Contains(err.Error(), "entry_requirements") {
		t.Fatalf("malformed entry requirement error = %v", err)
	}
}

func TestValidateDefinitionShapeChecksTrustedTechniqueAndEquipmentEffects(t *testing.T) {
	technique := storage.GameplayDefinition{
		Kind: storage.DefinitionTechnique,
		Content: storage.JSONMap{
			"category": "cultivation", "max_layer": int64(3), "practice_minutes": int64(60),
			"allowed_slots": []any{"primary"},
			"layer_effects": []any{map[string]any{
				"layer": int64(2), "effects": map[string]any{"max_mana": int64(10)},
			}},
		},
	}
	if err := validateDefinitionShape(technique); err != nil {
		t.Fatalf("valid technique effects were rejected: %v", err)
	}
	technique.Content["layer_effects"] = []any{map[string]any{
		"layer": int64(4), "effects": map[string]any{"luck": int64(1)},
	}}
	if err := validateDefinitionShape(technique); err == nil || !strings.Contains(err.Error(), "layer_effects") {
		t.Fatalf("untrusted technique effect error = %v", err)
	}
	equipment := storage.GameplayDefinition{
		Kind: storage.DefinitionEquipment,
		Content: storage.JSONMap{
			"allowed_slots": []any{"body"}, "durability_max": int64(100),
			"bonuses": map[string]any{"physical_defense": int64(3)},
		},
	}
	if err := validateDefinitionShape(equipment); err != nil {
		t.Fatalf("valid equipment bonuses were rejected: %v", err)
	}
	equipment.Content["bonuses"] = map[string]any{"untrusted": int64(1)}
	if err := validateDefinitionShape(equipment); err == nil || !strings.Contains(err.Error(), "bonuses") {
		t.Fatalf("untrusted equipment bonus error = %v", err)
	}
}

func TestValidateDefinitionShapeChecksTechniqueStartingPool(t *testing.T) {
	content := storage.JSONMap{
		"category": "cultivation", "max_layer": int64(3), "practice_minutes": int64(60),
		"allowed_slots": []any{"primary"}, "starting_pool": "primary-method",
		"starting_weight": int64(2), "starting_slot": "primary",
	}
	if err := validateDefinitionShape(storage.GameplayDefinition{Kind: storage.DefinitionTechnique, Content: content}); err != nil {
		t.Fatalf("valid starting pool was rejected: %v", err)
	}
	content["starting_known"] = true
	if err := validateDefinitionShape(storage.GameplayDefinition{Kind: storage.DefinitionTechnique, Content: content}); err == nil || !strings.Contains(err.Error(), "不能同时") {
		t.Fatalf("mixed fixed and pooled starter error = %v", err)
	}
	delete(content, "starting_known")
	content["starting_weight"] = int64(0)
	if err := validateDefinitionShape(storage.GameplayDefinition{Kind: storage.DefinitionTechnique, Content: content}); err == nil || !strings.Contains(err.Error(), "starting_weight") {
		t.Fatalf("invalid starting weight error = %v", err)
	}
}

func TestValidateCampaignDefinitionReferencesChecksFactionEntryPermission(t *testing.T) {
	contract := &gameplay.RuntimeContract{Campaign: &gameplay.CampaignConfig{InitialLocationKey: "world.start"}}
	definitions := []storage.GameplayDefinition{
		{Kind: storage.DefinitionRealm, Key: "mortal", Content: storage.JSONMap{"rank": int64(0)}},
		{Kind: storage.DefinitionLocation, Key: "world", Content: storage.JSONMap{
			"node_type": "realm", "connections": []any{},
		}},
		{Kind: storage.DefinitionLocation, Key: "world.region", Content: storage.JSONMap{
			"node_type": "region", "parent_key": "world", "connections": []any{},
		}},
		{Kind: storage.DefinitionLocation, Key: "world.start", Content: storage.JSONMap{
			"node_type": "location", "parent_key": "world.region",
			"connections": []any{},
			"entry_requirements": []any{map[string]any{
				"faction_key": "sect", "permission_key": "outer_gate",
			}},
		}},
		{Kind: storage.DefinitionFaction, Key: "sect", Content: storage.JSONMap{
			"permission_thresholds": map[string]any{"outer_gate": int64(100)},
		}},
	}
	if err := validateCampaignDefinitionReferences(contract, definitions); err != nil {
		t.Fatalf("valid faction entry reference was rejected: %v", err)
	}

	unknownFaction := append([]storage.GameplayDefinition(nil), definitions[:4]...)
	if err := validateCampaignDefinitionReferences(contract, unknownFaction); err == nil || !strings.Contains(err.Error(), "未知 faction_key") {
		t.Fatalf("unknown faction reference error = %v", err)
	}

	unknownPermission := append([]storage.GameplayDefinition(nil), definitions...)
	unknownPermission[4].Content = storage.JSONMap{"permission_thresholds": map[string]any{"inner_gate": int64(100)}}
	if err := validateCampaignDefinitionReferences(contract, unknownPermission); err == nil || !strings.Contains(err.Error(), "未声明的 permission_key") {
		t.Fatalf("unknown permission reference error = %v", err)
	}
}

func TestValidateCampaignDefinitionReferencesChecksFourLevelLocationHierarchy(t *testing.T) {
	contract := &gameplay.RuntimeContract{Campaign: &gameplay.CampaignConfig{InitialLocationKey: "cave"}}
	definitions := []storage.GameplayDefinition{
		{Kind: storage.DefinitionRealm, Key: "mortal", Content: storage.JSONMap{"rank": int64(0)}},
		{Kind: storage.DefinitionLocation, Key: "world", Content: storage.JSONMap{"node_type": "realm", "connections": []any{}}},
		{Kind: storage.DefinitionLocation, Key: "county", Content: storage.JSONMap{
			"node_type": "region", "parent_key": "world", "connections": []any{},
		}},
		{Kind: storage.DefinitionLocation, Key: "woods", Content: storage.JSONMap{
			"node_type": "location", "parent_key": "county", "connections": []any{map[string]any{
				"location_key": "cave", "travel_minutes": int64(10),
			}},
		}},
		{Kind: storage.DefinitionLocation, Key: "cave", Content: storage.JSONMap{
			"node_type": "scene", "parent_key": "woods", "connections": []any{map[string]any{
				"location_key": "woods", "travel_minutes": int64(10),
			}},
		}},
	}
	if err := validateCampaignDefinitionReferences(contract, definitions); err != nil {
		t.Fatalf("valid four-level hierarchy was rejected: %v", err)
	}
	invalid := append([]storage.GameplayDefinition(nil), definitions...)
	invalid[4].Content = storage.JSONMap{
		"node_type": "scene", "parent_key": "county", "connections": []any{},
	}
	if err := validateCampaignDefinitionReferences(contract, invalid); err == nil || !strings.Contains(err.Error(), "父节点") {
		t.Fatalf("invalid hierarchy error = %v", err)
	}

	missingType := append([]storage.GameplayDefinition(nil), definitions...)
	missingType[3].Content = storage.JSONMap{"parent_key": "county", "connections": []any{}}
	if err := validateDefinitionShape(missingType[3]); err == nil || !strings.Contains(err.Error(), "node_type") {
		t.Fatalf("missing node_type error = %v", err)
	}

	detached := append([]storage.GameplayDefinition(nil), definitions...)
	detached[3].Content = storage.JSONMap{"node_type": "location", "connections": []any{}}
	if err := validateCampaignDefinitionReferences(contract, detached); err == nil || !strings.Contains(err.Error(), "必须声明父节点") {
		t.Fatalf("detached location error = %v", err)
	}
}

func TestValidateCampaignDefinitionReferencesRejectsInitializationCollisions(t *testing.T) {
	contract := &gameplay.RuntimeContract{Campaign: &gameplay.CampaignConfig{InitialLocationKey: "world.start"}}
	base := []storage.GameplayDefinition{
		{Kind: storage.DefinitionRealm, Key: "mortal", Content: storage.JSONMap{"rank": int64(0)}},
		{Kind: storage.DefinitionLocation, Key: "world", Content: storage.JSONMap{
			"node_type": "realm", "connections": []any{},
		}},
		{Kind: storage.DefinitionLocation, Key: "world.region", Content: storage.JSONMap{
			"node_type": "region", "parent_key": "world", "connections": []any{},
		}},
		{Kind: storage.DefinitionLocation, Key: "world.start", Content: storage.JSONMap{
			"node_type": "location", "parent_key": "world.region", "connections": []any{},
		}},
	}
	techniques := append(append([]storage.GameplayDefinition{}, base...),
		storage.GameplayDefinition{Kind: storage.DefinitionTechnique, Key: "first", Content: storage.JSONMap{"starting_known": true, "starting_slot": "primary"}},
		storage.GameplayDefinition{Kind: storage.DefinitionTechnique, Key: "second", Content: storage.JSONMap{"starting_known": true, "starting_slot": "primary"}},
	)
	if err := validateCampaignDefinitionReferences(contract, techniques); err == nil || !strings.Contains(err.Error(), "相同初始槽位") {
		t.Fatalf("duplicate starting slot error = %v", err)
	}
	pool := append(append([]storage.GameplayDefinition{}, base...),
		storage.GameplayDefinition{Kind: storage.DefinitionTechnique, Key: "wood", Content: storage.JSONMap{"starting_pool": "primary", "starting_slot": "primary"}},
		storage.GameplayDefinition{Kind: storage.DefinitionTechnique, Key: "fire", Content: storage.JSONMap{"starting_pool": "primary", "starting_slot": "primary"}},
	)
	if err := validateCampaignDefinitionReferences(contract, pool); err != nil {
		t.Fatalf("same pool candidates may share a slot: %v", err)
	}
	singleCandidatePool := append(append([]storage.GameplayDefinition{}, base...), pool[len(pool)-1])
	if err := validateCampaignDefinitionReferences(contract, singleCandidatePool); err == nil || !strings.Contains(err.Error(), "至少需要两个") {
		t.Fatalf("single candidate pool error = %v", err)
	}
	crossPool := append(append([]storage.GameplayDefinition{}, pool...),
		storage.GameplayDefinition{Kind: storage.DefinitionTechnique, Key: "water", Content: storage.JSONMap{"starting_pool": "other", "starting_slot": "primary"}},
		storage.GameplayDefinition{Kind: storage.DefinitionTechnique, Key: "earth", Content: storage.JSONMap{"starting_pool": "other", "starting_slot": "secondary"}},
	)
	if err := validateCampaignDefinitionReferences(contract, crossPool); err == nil || !strings.Contains(err.Error(), "相同初始槽位") {
		t.Fatalf("cross-pool slot collision error = %v", err)
	}
	npcs := append(append([]storage.GameplayDefinition{}, base...),
		storage.GameplayDefinition{Kind: storage.DefinitionNPCArchetype, Key: "first", Content: storage.JSONMap{
			"initial": true, "instance_key": "same", "location_key": "world.start", "realm_key": "mortal",
		}},
		storage.GameplayDefinition{Kind: storage.DefinitionNPCArchetype, Key: "second", Content: storage.JSONMap{
			"initial": true, "instance_key": "same", "location_key": "world.start", "realm_key": "mortal",
		}},
	)
	if err := validateCampaignDefinitionReferences(contract, npcs); err == nil || !strings.Contains(err.Error(), "相同 instance_key") {
		t.Fatalf("duplicate initial NPC key error = %v", err)
	}
}

func TestValidateCampaignDefinitionReferencesChecksProductionAndMarketCatalogs(t *testing.T) {
	contract := &gameplay.RuntimeContract{Campaign: &gameplay.CampaignConfig{InitialLocationKey: "workshop"}}
	validDefinitions := func() []storage.GameplayDefinition {
		return []storage.GameplayDefinition{
			{Kind: storage.DefinitionRealm, Key: "mortal", Content: storage.JSONMap{"rank": int64(0)}},
			{Kind: storage.DefinitionLocation, Key: "world", Content: storage.JSONMap{"node_type": "realm", "connections": []any{}}},
			{Kind: storage.DefinitionLocation, Key: "region", Content: storage.JSONMap{"node_type": "region", "parent_key": "world", "connections": []any{}}},
			{Kind: storage.DefinitionLocation, Key: "workshop", Content: storage.JSONMap{"node_type": "location", "parent_key": "region", "connections": []any{}}},
			{Kind: storage.DefinitionItem, Key: "coin", Content: storage.JSONMap{"stackable": true}},
			{Kind: storage.DefinitionItem, Key: "herb", Content: storage.JSONMap{"stackable": true}},
			{Kind: storage.DefinitionItem, Key: "pill", Content: storage.JSONMap{"stackable": true}},
			{Kind: storage.DefinitionEquipment, Key: "furnace", Content: storage.JSONMap{"allowed_slots": []any{"tool"}, "durability_max": int64(100)}},
			{Kind: storage.DefinitionTechnique, Key: "alchemy", Content: storage.JSONMap{"category": "craft"}},
			{Kind: storage.DefinitionRecipe, Key: "pill-recipe", Content: storage.JSONMap{
				"craft_type": "alchemy", "starting_known": true, "allowed_location_keys": []any{"workshop"},
				"inputs":                  []any{map[string]any{"quantity": int64(1), "options": []any{map[string]any{"item_key": "herb"}}}},
				"required_equipment_keys": []any{"furnace"}, "technique_key": "alchemy",
				"craft_minutes": int64(60), "success_chance_bps": int64(8000), "failure_input_loss_bps": int64(5000),
				"outputs": []any{map[string]any{"kind": "item", "key": "pill", "quantity": int64(1), "quality_weights": map[string]any{"common": int64(1)}}},
			}},
			{Kind: storage.DefinitionMarket, Key: "market", Content: storage.JSONMap{
				"location_key": "workshop", "currency_item_key": "coin", "restock_interval_minutes": int64(60),
				"items": []any{map[string]any{
					"kind": "item", "key": "pill", "max_stock": int64(10), "initial_stock": int64(2),
					"restock_quantity": int64(1), "base_buy_price": int64(10), "base_sell_price": int64(5),
				}},
			}},
		}
	}
	if err := validateCampaignDefinitionReferences(contract, validDefinitions()); err != nil {
		t.Fatalf("valid production and market catalog was rejected: %v", err)
	}

	tests := []struct {
		name    string
		mutate  func([]storage.GameplayDefinition)
		message string
	}{
		{
			name: "unknown recipe material",
			mutate: func(definitions []storage.GameplayDefinition) {
				definitions[9].Content["inputs"] = []any{map[string]any{
					"quantity": int64(1), "options": []any{map[string]any{"item_key": "missing-herb"}},
				}}
			},
			message: "材料",
		},
		{
			name: "unknown recipe product",
			mutate: func(definitions []storage.GameplayDefinition) {
				definitions[9].Content["outputs"] = []any{map[string]any{
					"kind": "item", "key": "missing-pill", "quantity": int64(1), "quality_weights": map[string]any{"common": int64(1)},
				}}
			},
			message: "可堆叠 item",
		},
		{
			name: "non craft technique",
			mutate: func(definitions []storage.GameplayDefinition) {
				definitions[8].Content = storage.JSONMap{"category": "cultivation"}
			},
			message: "craft 类功法",
		},
		{
			name: "non stackable currency",
			mutate: func(definitions []storage.GameplayDefinition) {
				definitions[4].Content = storage.JSONMap{"stackable": false}
			},
			message: "可堆叠 item",
		},
		{
			name: "unknown market location",
			mutate: func(definitions []storage.GameplayDefinition) {
				definitions[10].Content["location_key"] = "missing-market"
			},
			message: "location_key",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			definitions := validDefinitions()
			test.mutate(definitions)
			err := validateCampaignDefinitionReferences(contract, definitions)
			if err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("validation error = %v, want message containing %q", err, test.message)
			}
		})
	}
}
