// Package protocol 定义模型输出的结构化协议（doc 08）：
// JSON 信封（narrative/state_update/action/roll_request）及其解析与流式提取。
//
// 协议类型独立于 llm 包（HTTP 客户端），供编排（orchestration）与
// 判定（roll）等领域包共同依赖，避免"领域包依赖 LLM 客户端"的倒挂。
package protocol

import (
	"encoding/json"
	"strconv"
	"strings"
)

// Envelope 是约定的模型结构化输出（doc 08）。
type Envelope struct {
	Narrative   string         `json:"narrative"`
	StateUpdate map[string]any `json:"state_update"`
	Action      *Action        `json:"action,omitempty"`
	RollRequest *RollRequest   `json:"roll_request,omitempty"` // 判定请求
	Notes       string         `json:"notes"`
}

// Action 是 AI 触发的特殊动作（如结束对话、触发事件）。
type Action struct {
	Type   string         `json:"type"`   // 动作类型：trigger_ending / trigger_event 等
	Params map[string]any `json:"params"` // 动作参数
}

// RollRequest 是 AI 请求的判定（两阶段判定协议）。
type RollRequest struct {
	Type               string             `json:"type"`                          // 判定类型：根骨判定、悟性判定、气运判定等
	Category           string             `json:"category,omitempty"`            // 判定大类：combat/exploration/crafting/social
	RelevantAttributes []string           `json:"relevant_attributes,omitempty"` // 相关属性列表
	AttributeWeights   map[string]float64 `json:"attribute_weights,omitempty"`   // 属性权重（和为1）
	BaseDifficulty     FlexInt            `json:"base_difficulty,omitempty"`     // 基础难度（建议值，后端可调整）
	Sides              FlexInt            `json:"sides"`                         // 骰子面数（D20=20, D100=100）
	Description        string             `json:"description"`                   // 判定行为描述
	Context            *RollContext       `json:"context,omitempty"`             // 情境信息
	Stakes             *RollStakes        `json:"stakes,omitempty"`              // 风险收益信息
}

// RollContext 判定的情境信息。
// advantages/disadvantages/environment 的取值来自 roll_config.yaml 的词表（枚举），
// 后端按精确查表计分；词表会在启动时注入 response_format 的 JSON Schema 强约束模型输出。
// 三个词条数组均用 FlexStrings：schema 降级时模型输出字符串/对象也不会拖垮信封解析。
type RollContext struct {
	Advantages    FlexStrings   `json:"advantages,omitempty"`    // 有利因素（枚举词表）
	Disadvantages FlexStrings   `json:"disadvantages,omitempty"` // 不利因素（枚举词表）
	Environment   FlexStrings   `json:"environment,omitempty"`   // 环境因素（枚举词表）
	Opponent      *RollOpponent `json:"opponent,omitempty"`      // 对手信息（结构化）
}

// RollOpponent 对手信息（战斗类判定），结构化字段替代旧的自由 map。
type RollOpponent struct {
	RealmGap FlexInt     `json:"realm_gap,omitempty"` // 对手比玩家高的境界数（可为负）
	Statuses FlexStrings `json:"statuses,omitempty"`  // 对手状态（枚举：对手受伤/对手疲惫）
	Element  string      `json:"element,omitempty"`   // 克制关系（枚举：属性克制/属性被克）
}

// UnmarshalJSON 容错解析：优先按结构化字段解析；解析结果为空（旧格式的键会被
// encoding/json 静默忽略，得到全零结构体）时按旧 map 形态尽力提取，
// 保证单个字段格式错误不会让整个信封解析失败（信封失败会导致裸 JSON 当剧情展示）。
func (o *RollOpponent) UnmarshalJSON(data []byte) error {
	// 先尝试结构化形态（避免递归，用别名类型）。命中任一字段即采纳。
	type alias RollOpponent
	var a alias
	if err := json.Unmarshal(data, &a); err == nil &&
		(a.RealmGap != 0 || len(a.Statuses) > 0 || a.Element != "") {
		*o = RollOpponent(a)
		return nil
	}

	// 旧 map 形态：提取境界差距数值，其余字符串值并入 Statuses。
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil // 尽力而为：无法解析时留空，不让上层信封失败
	}
	for key, val := range m {
		switch {
		case strings.Contains(key, "境界差") || strings.Contains(strings.ToLower(key), "realm"):
			if n, ok := flexInt(val); ok {
				o.RealmGap = FlexInt(n)
			}
		default:
			if s, ok := val.(string); ok && s != "" {
				o.Statuses = append(o.Statuses, s)
			}
		}
	}
	return nil
}

// FlexStrings 是容错的字符串数组：接受 JSON 数组，也接受对象（取其字符串值）或单个字符串。
// 解析失败时留空而不报错，避免拖垮整个信封。
type FlexStrings []string

// FlexInt 是容错的整数：接受 JSON 数字，也接受数字字符串（如 "100"）。
// 解析失败时保持零值而不报错，避免拖垮整个信封。
type FlexInt int

func (n *FlexInt) UnmarshalJSON(data []byte) error {
	var num float64
	if err := json.Unmarshal(data, &num); err == nil {
		*n = FlexInt(int(num))
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		if v, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
			*n = FlexInt(v)
		}
	}
	return nil // 尽力而为
}

func (f *FlexStrings) UnmarshalJSON(data []byte) error {
	var arr []string
	if err := json.Unmarshal(data, &arr); err == nil {
		*f = arr
		return nil
	}
	var single string
	if err := json.Unmarshal(data, &single); err == nil {
		if single != "" {
			*f = []string{single}
		}
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err == nil {
		for _, v := range m {
			if s, ok := v.(string); ok && s != "" {
				*f = append(*f, s)
			}
		}
	}
	return nil // 尽力而为
}

// flexInt 尽力把 JSON 数值/字符串转成 int。
func flexInt(val any) (int, bool) {
	switch v := val.(type) {
	case float64:
		return int(v), true
	case string:
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return n, true
		}
	}
	return 0, false
}

// RollStakes 判定的风险和收益。
type RollStakes struct {
	RewardValue int    `json:"reward_value,omitempty"` // 预期收益（灵石价值）
	RiskLevel   string `json:"risk_level,omitempty"`   // 风险等级：low/medium/high/extreme
}

// ParseEnvelope 尽力将模型输出解析为约定的 JSON 信封（doc 08）。
//
// 解析顺序：
//  1. 去除可能的 ```json 代码块包裹；
//  2. 截取首个 '{' 到末个 '}' 之间的内容尝试解析；
//  3. 解析失败则降级：把整段文本当作 narrative，state_update 为空。
func ParseEnvelope(raw string) Envelope {
	text := strings.TrimSpace(raw)
	text = stripCodeFence(text)

	if env, ok := tryParse(text); ok {
		return env
	}
	// 尝试截取最外层大括号。
	if start := strings.Index(text, "{"); start >= 0 {
		if end := strings.LastIndex(text, "}"); end > start {
			if env, ok := tryParse(text[start : end+1]); ok {
				return env
			}
		}
	}
	// 降级：整段文本作为剧情。
	return Envelope{Narrative: raw, StateUpdate: map[string]any{}}
}

func tryParse(s string) (Envelope, bool) {
	var env Envelope
	if err := json.Unmarshal([]byte(s), &env); err != nil {
		return Envelope{}, false
	}
	if env.StateUpdate == nil {
		env.StateUpdate = map[string]any{}
	}
	// narrative 为空且没有 state_update 时视为无效解析。
	if env.Narrative == "" && len(env.StateUpdate) == 0 {
		return Envelope{}, false
	}
	return env, true
}

// stripCodeFence 去除 ```json ... ``` 或 ``` ... ``` 包裹。
func stripCodeFence(s string) string {
	if !strings.HasPrefix(s, "```") {
		return s
	}
	s = strings.TrimPrefix(s, "```")
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		// 去掉可能的语言标注（如 json）。
		s = s[i+1:]
	}
	s = strings.TrimSuffix(strings.TrimSpace(s), "```")
	return strings.TrimSpace(s)
}
