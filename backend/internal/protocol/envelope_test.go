package protocol

import (
	"encoding/json"
	"testing"
)

// TestParseEnvelope_WithAction 测试带 action 字段的信封解析。
func TestParseEnvelope_WithAction(t *testing.T) {
	raw := `{
		"narrative": "说书人轻叹一声，醒木第三次落下。茶馆的灯火渐熄，门外晨光初现。「缘尽于此，客官请回吧。」",
		"state_update": {
			"location.name": "茶馆门外"
		},
		"action": {
			"type": "trigger_ending",
			"params": {
				"ending_type": "good",
				"title": "命运归处",
				"summary": "你走出茶馆，回望时它已消失在晨光中。"
			}
		},
		"notes": "触发好结局"
	}`

	env := ParseEnvelope(raw)

	if env.Narrative == "" {
		t.Error("narrative 不应为空")
	}
	if env.Action == nil {
		t.Fatal("action 应该被解析出来")
	}
	if env.Action.Type != "trigger_ending" {
		t.Errorf("action.type = %s, 期望 trigger_ending", env.Action.Type)
	}
	if endingType, ok := env.Action.Params["ending_type"].(string); !ok || endingType != "good" {
		t.Errorf("action.params.ending_type = %v, 期望 good", env.Action.Params["ending_type"])
	}
	if title, ok := env.Action.Params["title"].(string); !ok || title == "" {
		t.Error("action.params.title 不应为空")
	}
}

// TestParseEnvelope_WithoutAction 测试 action 为可选字段的普通叙事信封。
func TestParseEnvelope_WithoutAction(t *testing.T) {
	raw := `{
		"narrative": "普通的剧情推进。",
		"state_update": {"profile.title": "旅者"},
		"notes": "常规回合"
	}`

	env := ParseEnvelope(raw)

	if env.Narrative == "" {
		t.Error("narrative 不应为空")
	}
	if env.Action != nil {
		t.Error("action 应该为 nil（没有提供该字段）")
	}
	if len(env.StateUpdate) == 0 {
		t.Error("state_update 应该被解析出来")
	}
}

// TestAction_JSONMarshal 测试 Action 结构体的序列化。
func TestAction_JSONMarshal(t *testing.T) {
	action := &Action{
		Type: "trigger_ending",
		Params: map[string]any{
			"ending_type": "bad",
			"title":       "毁灭之路",
			"summary":     "你的选择导致了不可挽回的后果。",
		},
	}

	data, err := json.Marshal(action)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	var parsed Action
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}

	if parsed.Type != action.Type {
		t.Errorf("type = %s, 期望 %s", parsed.Type, action.Type)
	}
	if parsed.Params["ending_type"] != "bad" {
		t.Errorf("params.ending_type = %v, 期望 bad", parsed.Params["ending_type"])
	}
}

// TestRollContext_StructuredForm 测试新的结构化情境（枚举数组 + 结构化 opponent）。
func TestRollContext_StructuredForm(t *testing.T) {
	raw := `{"narrative":"战斗","roll_request":{"type":"斩杀","sides":100,"description":"斩杀妖兽",
		"context":{"advantages":["得到指点"],"disadvantages":["首次尝试"],"environment":["夜晚"],
		"opponent":{"realm_gap":1,"statuses":["对手受伤"],"element":"属性克制"}}}}`
	env := ParseEnvelope(raw)
	if env.RollRequest == nil || env.RollRequest.Context == nil {
		t.Fatal("roll_request.context 应解析成功")
	}
	c := env.RollRequest.Context
	if len(c.Advantages) != 1 || c.Advantages[0] != "得到指点" {
		t.Errorf("advantages = %v", c.Advantages)
	}
	if len(c.Environment) != 1 || c.Environment[0] != "夜晚" {
		t.Errorf("environment = %v", c.Environment)
	}
	if c.Opponent == nil || c.Opponent.RealmGap != 1 || c.Opponent.Element != "属性克制" {
		t.Errorf("opponent = %+v", c.Opponent)
	}
	if len(c.Opponent.Statuses) != 1 || c.Opponent.Statuses[0] != "对手受伤" {
		t.Errorf("opponent.statuses = %v", c.Opponent.Statuses)
	}
}

// TestRollContext_LegacyFormTolerated 测试旧格式（environment 为对象、opponent 为自由 map）
// 不会导致整个信封解析失败——尽力提取，失败字段留空。
func TestRollContext_LegacyFormTolerated(t *testing.T) {
	raw := `{"narrative":"战斗","roll_request":{"type":"斩杀","sides":100,"description":"斩杀妖兽",
		"context":{"environment":{"weather":"大雨"},"opponent":{"境界差距":2,"状态":"受伤"}}}}`
	env := ParseEnvelope(raw)
	if env.Narrative != "战斗" {
		t.Fatalf("旧格式不应拖垮信封解析, narrative = %q", env.Narrative)
	}
	if env.RollRequest == nil || env.RollRequest.Context == nil {
		t.Fatal("context 应尽力解析")
	}
	c := env.RollRequest.Context
	// environment 对象取其字符串值
	if len(c.Environment) != 1 || c.Environment[0] != "大雨" {
		t.Errorf("legacy environment = %v", c.Environment)
	}
	// opponent 自由 map：境界差距进 RealmGap，其余字符串进 Statuses
	if c.Opponent == nil || c.Opponent.RealmGap != 2 {
		t.Errorf("legacy opponent.realm_gap = %+v", c.Opponent)
	}
	if len(c.Opponent.Statuses) != 1 || c.Opponent.Statuses[0] != "受伤" {
		t.Errorf("legacy opponent.statuses = %v", c.Opponent.Statuses)
	}
}

// TestRollRequest_FlexIntTolerated 测试 schema 降级时模型把 sides/base_difficulty
// 输出成字符串（"100"）不会拖垮整个信封解析。
func TestRollRequest_FlexIntTolerated(t *testing.T) {
	raw := `{"narrative":"判定","roll_request":{"type":"斩杀","sides":"100","base_difficulty":"65",
		"description":"斩杀妖兽","context":{"advantages":"充分准备","opponent":{"realm_gap":"2"}}}}`
	env := ParseEnvelope(raw)
	if env.Narrative != "判定" {
		t.Fatalf("字符串数值不应拖垮信封, narrative=%q", env.Narrative)
	}
	rr := env.RollRequest
	if rr == nil || int(rr.Sides) != 100 || int(rr.BaseDifficulty) != 65 {
		t.Fatalf("sides/base_difficulty 字符串未容错: %+v", rr)
	}
	// advantages 单字符串应被 FlexStrings 收进数组
	if rr.Context == nil || len(rr.Context.Advantages) != 1 || rr.Context.Advantages[0] != "充分准备" {
		t.Fatalf("advantages 字符串未容错: %+v", rr.Context)
	}
	if rr.Context.Opponent == nil || int(rr.Context.Opponent.RealmGap) != 2 {
		t.Fatalf("opponent.realm_gap 字符串未容错: %+v", rr.Context.Opponent)
	}
}
