package state

import (
	"testing"

	gameplay "github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/storage"
)

func testSchema() gameplay.StateSchema {
	return gameplay.StateSchema{SchemaVersion: 1, Fields: []gameplay.StateField{
		{Path: "profile", Type: "object", Default: map[string]any{}, Visibility: gameplay.VisibilityPlayer, UpdatePolicy: gameplay.UpdateModel, AllowChildren: true},
		{Path: "profile.age", Type: "number", Default: float64(18), Visibility: gameplay.VisibilityPlayer, UpdatePolicy: gameplay.UpdateModel, Constraints: map[string]any{"min": float64(0)}},
		{Path: "profile.remaining_lifespan", Type: "number", Default: float64(120), Visibility: gameplay.VisibilityPlayer, UpdatePolicy: gameplay.UpdateModel, Constraints: map[string]any{"min": float64(0)}},
		{Path: "inventory.items", Type: "list", Default: []any{}, Visibility: gameplay.VisibilityPlayer, UpdatePolicy: gameplay.UpdateModel},
		{Path: "traits", Type: "list", Default: []any{}, Visibility: gameplay.VisibilityPlayer, UpdatePolicy: gameplay.UpdateModel},
		{Path: "server.flag", Type: "boolean", Default: false, Visibility: gameplay.VisibilityInternal, UpdatePolicy: gameplay.UpdateSystem},
	}}
}

func TestApplyPatchRejectsMixedPatchAtomically(t *testing.T) {
	schema := testSchema()
	base := gameplay.InitialState(schema)
	patch := map[string]any{
		"profile.title": "勇者", "inventory.items+": "剑",
		"server.flag": true, "player_id": "hack",
	}
	result, applied, skipped := ApplyPatch(schema, storage.SourceAI, base, patch)
	if _, exists := result["profile"].(map[string]any)["title"]; exists {
		t.Fatal("valid key from a mixed patch must not be applied")
	}
	items := result["inventory"].(map[string]any)["items"].([]any)
	if len(items) != 0 {
		t.Fatalf("inventory.items = %v", items)
	}
	if len(applied) != 0 {
		t.Fatalf("applied = %v", applied)
	}
	if len(skipped) != 2 || skipped[0] != "player_id" || skipped[1] != "server.flag" {
		t.Fatalf("skipped = %v", skipped)
	}
	if len(base["inventory"].(map[string]any)["items"].([]any)) != 0 {
		t.Fatal("ApplyPatch mutated input")
	}
}

func TestSplitPatchKeepsValidKeysAndReportsSkipped(t *testing.T) {
	schema := testSchema()
	patch := map[string]any{
		"profile.age": float64(19), "traits+": "剑心通明",
		"server.flag": true, "player_id": "hack", "location": "青云山",
	}
	valid, skipped := SplitPatch(schema, storage.SourceAI, patch)
	if len(valid) != 2 || valid["profile.age"] != float64(19) || valid["traits+"] != "剑心通明" {
		t.Fatalf("valid = %v", valid)
	}
	if len(skipped) != 3 || skipped[0] != "location" || skipped[1] != "player_id" || skipped[2] != "server.flag" {
		t.Fatalf("skipped = %v", skipped)
	}
	// 全合法时原样返回，无跳过。
	valid, skipped = SplitPatch(schema, storage.SourceAI, map[string]any{"profile.age": float64(20)})
	if len(valid) != 1 || skipped != nil {
		t.Fatalf("clean patch: valid=%v skipped=%v", valid, skipped)
	}
}

// allow_children 对象下的动态子路径允许追加（与绝对赋值同权限）：
// 真实场景是 reincarnation-trials 的 inventory 未声明 items 子字段，
// 模型输出 "inventory.items+" 曾被整键跳过，造成剧情拿到物品而背包没记。
func TestApplyPatchAppendsToDynamicChildOfAllowChildrenObject(t *testing.T) {
	schema := gameplay.StateSchema{SchemaVersion: 1, Fields: []gameplay.StateField{
		{Path: "inventory", Type: "object", Default: map[string]any{"items": []any{}}, Visibility: gameplay.VisibilityPlayer, UpdatePolicy: gameplay.UpdateModel, AllowChildren: true},
	}}
	patch := map[string]any{"inventory.items+": "残破的《青云诀》"}
	if err := ValidatePatch(schema, storage.SourceAI, patch); err != nil {
		t.Fatalf("dynamic child append should be valid: %v", err)
	}
	result, applied, skipped := ApplyPatch(schema, storage.SourceAI, gameplay.InitialState(schema), patch)
	if len(skipped) != 0 || len(applied) != 1 {
		t.Fatalf("applied=%v skipped=%v", applied, skipped)
	}
	items := result["inventory"].(map[string]any)["items"].([]any)
	if len(items) != 1 || items[0] != "残破的《青云诀》" {
		t.Fatalf("items = %v", items)
	}
}

func TestValidatePatchAcceptsAbsoluteNumberAndListAppend(t *testing.T) {
	patch := map[string]any{"profile.age": float64(19), "traits+": "剑心通明"}
	if err := ValidatePatch(testSchema(), storage.SourceAI, patch); err != nil {
		t.Fatalf("ValidatePatch() error = %v", err)
	}
	result, applied, skipped := ApplyPatch(testSchema(), storage.SourceAI, gameplay.InitialState(testSchema()), patch)
	if len(skipped) != 0 || len(applied) != 2 {
		t.Fatalf("applied=%v skipped=%v", applied, skipped)
	}
	if result["profile"].(map[string]any)["age"] != float64(19) {
		t.Fatalf("age = %v", result["profile"].(map[string]any)["age"])
	}
	if got := result["traits"].([]any); len(got) != 1 || got[0] != "剑心通明" {
		t.Fatalf("traits = %v", got)
	}
}

func TestValidatePatchRejectsInvalidNumericUpdates(t *testing.T) {
	tests := []struct {
		name  string
		patch map[string]any
		key   string
	}{
		{name: "numeric plus", patch: map[string]any{"profile.age+": float64(0.01)}, key: "profile.age+"},
		{name: "numeric minus", patch: map[string]any{"profile.remaining_lifespan-": float64(0.01)}, key: "profile.remaining_lifespan-"},
		{name: "numeric string", patch: map[string]any{"profile.age": "19"}, key: "profile.age"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePatch(testSchema(), storage.SourceAI, tt.patch)
			validationErr, ok := err.(*PatchValidationError)
			if !ok || len(validationErr.Keys) != 1 || validationErr.Keys[0] != tt.key {
				t.Fatalf("validation error = %#v", err)
			}
		})
	}
}

func TestValidatePatchBlocksObjectOwnershipBypass(t *testing.T) {
	schema := gameplay.StateSchema{SchemaVersion: 1, Fields: []gameplay.StateField{
		{Path: "profile", Type: "object", UpdatePolicy: gameplay.UpdateModel, AllowChildren: true},
		{Path: "profile.name", Type: "string", UpdatePolicy: gameplay.UpdateModel},
		{Path: "profile.cultivation_level", Type: "enum", UpdatePolicy: gameplay.UpdateSystem,
			Constraints: map[string]any{"values": []any{"凡人", "练气期"}}},
		{Path: "ascension", Type: "object", UpdatePolicy: gameplay.UpdateModel, AllowChildren: true},
		{Path: "ascension.ascension_token_owned", Type: "boolean", UpdatePolicy: gameplay.UpdateSystem},
	}}

	tests := []struct {
		name  string
		patch map[string]any
	}{
		{name: "whole profile", patch: map[string]any{"profile": map[string]any{"cultivation_level": "练气期"}}},
		{name: "whole ascension", patch: map[string]any{"ascension": map[string]any{"ascension_token_owned": true}}},
		{name: "tunnel through scalar", patch: map[string]any{"profile.cultivation_level.extra": true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidatePatch(schema, storage.SourceAI, tt.patch); err == nil {
				t.Fatalf("bypass patch must be rejected: %v", tt.patch)
			}
		})
	}

	if err := ValidatePatch(schema, storage.SourceAI, map[string]any{"profile.nickname": "阿青"}); err != nil {
		t.Fatalf("unclaimed dynamic child should remain writable: %v", err)
	}
}

func TestFilterStateDropsUndeclaredValues(t *testing.T) {
	filtered := FilterState(testSchema(), map[string]any{
		"profile": map[string]any{"name": "A"}, "player_id": "hack",
	})
	if _, ok := filtered["player_id"]; ok {
		t.Fatal("undeclared state survived filtering")
	}
}

func TestProjectPlayerRevisionDropsInternalState(t *testing.T) {
	contract := gameplay.RuntimeContract{StateSchema: testSchema()}
	revision := storage.StateRevision{
		Patch: storage.JSONMap{"profile.age": float64(19), "server.flag": true},
		BeforeState: storage.JSONMap{
			"profile": map[string]any{"age": float64(18)},
			"server":  map[string]any{"flag": false},
		},
		AfterState: storage.JSONMap{
			"profile": map[string]any{"age": float64(19)},
			"server":  map[string]any{"flag": true},
		},
	}

	public := projectPlayerRevision(contract, revision)
	if _, leaked := public.Patch["server.flag"]; leaked {
		t.Fatal("internal patch leaked into player revision")
	}
	if _, leaked := public.BeforeState["server"]; leaked {
		t.Fatal("internal before_state leaked into player revision")
	}
	if _, leaked := public.AfterState["server"]; leaked {
		t.Fatal("internal after_state leaked into player revision")
	}
	if public.Patch["profile.age"] != float64(19) {
		t.Fatalf("player patch = %v", public.Patch)
	}
}
