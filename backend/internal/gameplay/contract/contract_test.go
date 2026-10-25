package contract

import (
	"testing"
)

func TestStateSchemaCannotDeclarePlatformProgress(t *testing.T) {
	for _, path := range []string{"_platform", "_platform.stage_progress"} {
		if err := validateStateField(StateField{Path: path, Type: "object", Default: map[string]any{}, Visibility: VisibilityPlayer, UpdatePolicy: UpdateModel, AllowChildren: true}); err == nil {
			t.Fatalf("pack declared reserved state %s", path)
		}
	}
}

func TestPlayerStateDoesNotLeakHiddenObjectChildren(t *testing.T) {
	contract := RuntimeContract{StateSchema: StateSchema{SchemaVersion: 1, Fields: []StateField{
		{Path: "case", Type: "object", Visibility: VisibilityPlayer, UpdatePolicy: UpdateModel, AllowChildren: true},
		{Path: "case.clue", Type: "string", Visibility: VisibilityPlayer, UpdatePolicy: UpdateModel},
		{Path: "case.solution", Type: "string", Visibility: VisibilityInternal, UpdatePolicy: UpdateSystem},
	}}}

	projected := PlayerState(contract, map[string]any{
		"case": map[string]any{"clue": "旧钟停在午夜", "solution": "管家"},
	})
	caseState := projected["case"].(map[string]any)
	if caseState["clue"] != "旧钟停在午夜" {
		t.Fatalf("player clue = %v", caseState["clue"])
	}
	if _, leaked := caseState["solution"]; leaked {
		t.Fatal("internal solution leaked through player-visible parent object")
	}
}

func TestPlayerStateKeepsDynamicChildrenBesideDeclaredFields(t *testing.T) {
	contract := RuntimeContract{StateSchema: StateSchema{SchemaVersion: 1, Fields: []StateField{
		{Path: "attributes", Type: "object", Visibility: VisibilityPlayer, UpdatePolicy: UpdateModel, AllowChildren: true},
		{Path: "attributes.灵石", Type: "integer", Visibility: VisibilityPlayer, UpdatePolicy: UpdateModel},
		{Path: "attributes.hidden_score", Type: "integer", Visibility: VisibilityInternal, UpdatePolicy: UpdateSystem},
	}}}

	projected := PlayerState(contract, map[string]any{
		"attributes": map[string]any{"灵石": 10, "根骨": 72, "悟性": 85, "hidden_score": 99},
	})
	attributes := projected["attributes"].(map[string]any)
	if attributes["根骨"] != float64(72) || attributes["悟性"] != float64(85) || attributes["灵石"] != float64(10) {
		t.Fatalf("dynamic player attributes were not preserved: %#v", attributes)
	}
	if _, leaked := attributes["hidden_score"]; leaked {
		t.Fatal("explicit internal child leaked from a player-visible dynamic object")
	}
}

func TestValidateContractRejectsInvalidDefaultValue(t *testing.T) {
	contract := RuntimeContract{
		Engine: EngineRef{Key: "core.generic", Version: 1},
		StateSchema: StateSchema{SchemaVersion: 1, Fields: []StateField{{
			Path: "progress", Type: "integer", Default: 101,
			Visibility: VisibilityPlayer, UpdatePolicy: UpdateModel,
			Constraints: map[string]any{"min": float64(0), "max": float64(100)},
		}}},
		Settlement: SettlementConfig{Handler: "core.challenge_completion"},
	}
	if err := ValidateContract(&contract); err == nil {
		t.Fatal("invalid constrained default was accepted")
	}
}

func TestValidateContractCampaignRequiresCompleteConfig(t *testing.T) {
	contract := validCampaignContract()
	if err := ValidateContract(&contract); err != nil {
		t.Fatalf("valid campaign contract rejected: %v", err)
	}

	contract.Campaign.ContextPolicy = ""
	if err := ValidateContract(&contract); err == nil {
		t.Fatal("campaign contract without context policy was accepted")
	}
}

func TestCompileOwnershipRegistryIsStableAndRejectsCrossDomainOverlap(t *testing.T) {
	contract := validCampaignContract()
	one, err := CompileOwnershipRegistry(&contract)
	if err != nil {
		t.Fatal(err)
	}
	contract.Campaign.FieldOwnership = append(contract.Campaign.FieldOwnership, FieldOwnership{Path: "narrative", Owner: OwnerCampaign})
	if _, err := CompileOwnershipRegistry(&contract); err == nil {
		t.Fatal("cross-domain ownership overlap was accepted")
	}
	contract = validCampaignContract()
	two, err := CompileOwnershipRegistry(&contract)
	if err != nil || one.Hash != two.Hash || one.Hash == "" {
		t.Fatalf("ownership hash is not stable: %q vs %q, err=%v", one.Hash, two.Hash, err)
	}
	if owner, ok := one.OwnerForPath("character.name"); !ok || owner != OwnerCampaign {
		t.Fatalf("character.name owner = %q, %v", owner, ok)
	}
	if owner, ok := one.OwnerForPath("narrative.current_focus"); !ok || owner != OwnerConversation {
		t.Fatalf("narrative.current_focus owner = %q, %v", owner, ok)
	}
}

func TestValidateContractCampaignRejectsUnregisteredWriter(t *testing.T) {
	contract := validCampaignContract()
	contract.Campaign.FieldOwnership = append(contract.Campaign.FieldOwnership, FieldOwnership{
		Path: "inventory", Owner: OwnerCampaign, WriteCapability: "inventory.mutate",
	})
	if err := ValidateContract(&contract); err == nil {
		t.Fatal("campaign field with unregistered writer was accepted")
	}
}

func TestRuntimeCampaignEncodeDecodeRoundTrip(t *testing.T) {
	contract := validCampaignContract()
	contract.Presentation.ValueLabels = map[string]map[string]string{
		"campaign.relationship.public_stance": {"neutral": "中立"},
	}
	row, err := EncodeRuntime(contract)
	if err != nil {
		t.Fatal(err)
	}
	if row.Mode != RuntimeModeCampaign || row.CampaignConfig == nil || len(*row.CampaignConfig) == 0 {
		t.Fatalf("encoded campaign fields missing: mode=%q config=%v", row.Mode, row.CampaignConfig)
	}
	decoded, err := DecodeRuntime(row)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Campaign == nil || decoded.Campaign.InitialLocationKey != "world.start" {
		t.Fatalf("campaign config did not round-trip: %#v", decoded.Campaign)
	}
	if decoded.Presentation.ValueLabels["campaign.relationship.public_stance"]["neutral"] != "中立" {
		t.Fatalf("presentation value labels did not round-trip: %#v", decoded.Presentation.ValueLabels)
	}
}

func TestValidateContractRejectsInvalidValueLabels(t *testing.T) {
	tests := []struct {
		name   string
		labels map[string]map[string]string
	}{
		{name: "invalid namespace", labels: map[string]map[string]string{"Campaign Status": {"active": "进行中"}}},
		{name: "empty group", labels: map[string]map[string]string{"campaign.status": {}}},
		{name: "untrimmed key", labels: map[string]map[string]string{"campaign.status": {" active": "进行中"}}},
		{name: "blank label", labels: map[string]map[string]string{"campaign.status": {"active": " "}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			contract := validCampaignContract()
			contract.Presentation.ValueLabels = test.labels
			if err := ValidateContract(&contract); err == nil {
				t.Fatal("invalid presentation value labels were accepted")
			}
		})
	}
}

func TestCampaignViewRoundTripAndValidation(t *testing.T) {
	contract := validCampaignContract()
	contract.Presentation.CampaignView = &CampaignView{
		Renderer: "generic",
		Header:   CampaignViewHeader{TitlePath: "campaign.title", TimePath: "campaign.game_time", TimeFormat: "game_time"},
		Sections: []CampaignViewSection{{
			Key: "character", Title: "角色", Order: 10,
			Fields: []CampaignViewField{{Path: "character.name", Label: "姓名", Widget: "title", Order: 10}},
		}},
		Actions: []CampaignViewAction{{
			Key: "cultivation.practice", Label: "修炼", Placement: "character",
			Arguments: map[string]CampaignViewBinding{"technique_key": {From: "item.key"}},
		}},
	}
	if err := ValidateContract(&contract); err != nil {
		t.Fatalf("valid campaign view rejected: %v", err)
	}
	row, err := EncodeRuntime(contract)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeRuntime(row)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Presentation.CampaignView == nil || decoded.Presentation.CampaignView.Renderer != "generic" {
		t.Fatalf("campaign view did not round-trip: %#v", decoded.Presentation.CampaignView)
	}
}

func TestGuidanceRoundTripAndValidation(t *testing.T) {
	contract := validCampaignContract()
	contract.Presentation.Guidance = &Guidance{Groups: []GuidanceGroup{{
		Key: "next_steps", Title: "接下来可以", Placement: "composer", MaxItems: 2,
		Items: []GuidanceItem{{
			Key: "rest", Label: "休息", Description: "恢复状态", Mode: "message", Message: "我先休息一会儿。",
			VisibleWhen: &CampaignViewCondition{Path: "snapshot.character", Exists: ptrBool(true)},
		}, {
			Key: "practice", Label: "修炼", Mode: "action", ActionKey: "cultivation.practice",
		}},
	}}}
	if err := ValidateContract(&contract); err != nil {
		t.Fatalf("valid guidance rejected: %v", err)
	}
	row, err := EncodeRuntime(contract)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeRuntime(row)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Presentation.Guidance == nil || len(decoded.Presentation.Guidance.Groups) != 1 {
		t.Fatalf("guidance did not round-trip: %#v", decoded.Presentation.Guidance)
	}

	invalid := contract
	invalid.Presentation.Guidance = &Guidance{Groups: []GuidanceGroup{{
		Key: "unsafe", Title: "不安全", Items: []GuidanceItem{{
			Key: "secret", Label: "秘密", Mode: "message", Message: "x",
			VisibleWhen: &CampaignViewCondition{Path: "state.secret", Exists: ptrBool(true)},
		}},
	}}}
	if err := ValidateContract(&invalid); err == nil {
		t.Fatal("guidance referencing hidden state was accepted")
	}
	internal := validCampaignContract()
	internal.Campaign.FieldOwnership = append(internal.Campaign.FieldOwnership, FieldOwnership{
		Path: "character.secret", Owner: OwnerCampaign, ValueType: "string", Visibility: VisibilityInternal, StorageProjection: "campaign_characters",
	})
	internal.Presentation.Guidance = &Guidance{Groups: []GuidanceGroup{{
		Key: "unsafe_snapshot", Title: "不安全", Items: []GuidanceItem{{
			Key: "secret", Label: "秘密", Mode: "message", Message: "x",
			VisibleWhen: &CampaignViewCondition{Path: "snapshot.character.secret", Exists: ptrBool(true)},
		}},
	}}}
	if err := ValidateContract(&internal); err == nil {
		t.Fatal("guidance referencing internal campaign field was accepted")
	}
}

func TestProjectGuidanceFiltersAndMarksItems(t *testing.T) {
	guidance := Guidance{Groups: []GuidanceGroup{{
		Key: "next", Title: "接下来", MaxItems: 1,
		Items: []GuidanceItem{
			{Key: "hidden", Label: "隐藏", Mode: "message", Message: "x", VisibleWhen: &CampaignViewCondition{Path: "state.ready", Equals: true}},
			{Key: "shown", Label: "显示", Mode: "message", Message: "y", EnabledWhen: &CampaignViewCondition{Path: "state.charges", Operator: "gt", Value: float64(0)}},
		},
	}}}
	projected := ProjectGuidance(guidance, map[string]any{"ready": false, "charges": float64(2)}, nil)
	if len(projected.Groups) != 1 || len(projected.Groups[0].Items) != 1 || projected.Groups[0].Items[0].Key != "shown" {
		t.Fatalf("unexpected guidance projection: %#v", projected)
	}
	if projected.Groups[0].Items[0].Enabled == nil || !*projected.Groups[0].Items[0].Enabled {
		t.Fatal("enabled guidance item was not marked enabled")
	}

	invalid := guidance
	invalid.Groups[0].Items = []GuidanceItem{{Key: "bad", Label: "坏", Mode: "message", Message: "x", Enabled: ptrBool(true)}}
	contract := validCampaignContract()
	contract.Presentation.Guidance = &invalid
	if err := ValidateContract(&contract); err == nil {
		t.Fatal("authored guidance enabled flag was accepted")
	}
}

func ptrBool(value bool) *bool { return &value }

func TestValidateCampaignViewRejectsUnsafeReferences(t *testing.T) {
	tests := []CampaignView{
		{Renderer: "custom", Sections: []CampaignViewSection{}},
		{Renderer: "generic", Sections: []CampaignViewSection{{Key: "x", Title: "X", Fields: []CampaignViewField{{Path: "state.secret", Label: "X", Widget: "text"}}}}},
		{Renderer: "generic", Sections: []CampaignViewSection{{Key: "x", Title: "X", Fields: []CampaignViewField{{Path: "character.name", Label: "X", Widget: "unknown"}}}}},
		{Renderer: "generic", Sections: []CampaignViewSection{{Key: "x", Title: "X"}}, Actions: []CampaignViewAction{{Key: "not.allowed", Label: "X"}}},
	}
	for index, view := range tests {
		contract := validCampaignContract()
		contract.Presentation.CampaignView = &view
		if err := ValidateContract(&contract); err == nil {
			t.Fatalf("unsafe campaign view %d was accepted", index)
		}
	}
}

func TestDecodeRuntimeDefaultsMissingModeToChallenge(t *testing.T) {
	contract := validCampaignContract()
	contract.Mode = RuntimeModeChallenge
	contract.Campaign = nil
	contract.Settlement = SettlementConfig{Handler: "core.challenge_completion"}
	row, err := EncodeRuntime(contract)
	if err != nil {
		t.Fatal(err)
	}
	row.Mode = ""
	decoded, err := DecodeRuntime(row)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Mode != RuntimeModeChallenge {
		t.Fatalf("mode = %q, want challenge", decoded.Mode)
	}
}

func validCampaignContract() RuntimeContract {
	return RuntimeContract{
		Engine: EngineRef{Key: "cultivation.campaign", Version: 1},
		Mode:   RuntimeModeCampaign,
		Campaign: &CampaignConfig{
			LifecycleHandler: "cultivation.campaign_lifecycle", ProjectionHandler: "cultivation.campaign_projection",
			ContextPolicy: "cultivation.campaign_context", ActionAllowlist: []string{"cultivation.practice"}, InitialLocationKey: "world.start",
			FieldOwnership: []FieldOwnership{
				{Path: "campaign", Owner: OwnerCampaign, ValueType: "object", Visibility: VisibilityPlayer, StorageProjection: "campaigns"},
				{Path: "world", Owner: OwnerCampaign, ValueType: "object", Visibility: VisibilityPlayer, StorageProjection: "campaigns"},
				{Path: "character", Owner: OwnerCampaign, ValueType: "object", Visibility: VisibilityPlayer, StorageProjection: "campaign_characters"},
			},
			WorldClock: WorldClockConfig{CalendarKey: "world.default", MaxDueEventsPerTurn: 16},
		},
		StateSchema: StateSchema{SchemaVersion: 1, Fields: []StateField{{
			Path: "narrative.current_focus", Type: "string", Visibility: VisibilityPlayer,
			UpdatePolicy: UpdateModel,
		}}},
		Actions: ActionConfig{Model: []string{"update_state", "domain_action"}},
		Presentation: Presentation{
			StatePanelTitle: "状态", StateSections: []StateSection{},
		},
	}
}
