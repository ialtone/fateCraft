package orchestration

import (
	"encoding/json"
	"os"

	"go.uber.org/zap"

	"github.com/ialtone/fatecraft/internal/llm"
	"github.com/ialtone/fatecraft/internal/logging"
	"github.com/ialtone/fatecraft/internal/roll"
)

// LoadResponseSchema 从指定路径加载结构化输出（Structured Outputs）的 JSON Schema。
//
// 路径由组合根（main）根据配置注入，本包不再自行探测候选文件位置。
// 加载失败时返回错误，调用方可降级为传入 nil（Generator 将不启用 response_format）。
func LoadResponseSchema(path string) (*llm.JSONSchema, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var schema struct {
		Name        string         `json:"name"`
		Description string         `json:"description"`
		Strict      bool           `json:"strict"`
		Schema      map[string]any `json:"schema"`
	}
	if err := json.Unmarshal(data, &schema); err != nil {
		return nil, err
	}

	return &llm.JSONSchema{
		Name:        schema.Name,
		Description: schema.Description,
		Schema:      schema.Schema,
		Strict:      schema.Strict,
	}, nil
}

// InjectRollVocab 把判定词表注入 schema 的 roll_request.context 各枚举字段，
// 使"模型能输出什么"与"后端精确查表认什么"保持同源（词表唯一来源：roll_config.yaml）。
// schema 文件里的静态 enum 只是可读性参考，启动时一律被词表覆盖。
// 任一路径缺失时记警告并跳过（schema 结构变更不应让启动失败）。
func InjectRollVocab(schema *llm.JSONSchema, vocab roll.Vocab) {
	if schema == nil || schema.Schema == nil {
		return
	}
	ctxProps, ok := walkTo(schema.Schema,
		"properties", "roll_request", "properties", "context", "properties")
	if !ok {
		logging.Named("orchestration").Warn("schema 中找不到 roll_request.context.properties，词表 enum 未注入")
		return
	}

	setArrayItemsEnum(ctxProps, "advantages", vocab.Advantages)
	setArrayItemsEnum(ctxProps, "disadvantages", vocab.Disadvantages)
	setArrayItemsEnum(ctxProps, "environment", vocab.Environment)

	if oppProps, ok := walkTo(ctxProps, "opponent", "properties"); ok {
		setArrayItemsEnum(oppProps, "statuses", vocab.OpponentStatuses)
		if elem, ok := walkTo(oppProps, "element"); ok {
			elem["enum"] = toAnySlice(vocab.OpponentElements)
		}
	}
}

// walkTo 沿 map 路径逐层下钻，任意一层不存在或类型不符则返回 false。
func walkTo(m map[string]any, path ...string) (map[string]any, bool) {
	cur := m
	for _, key := range path {
		next, ok := cur[key].(map[string]any)
		if !ok {
			return nil, false
		}
		cur = next
	}
	return cur, true
}

// setArrayItemsEnum 给 props[field].items 设置 enum（字段或 items 缺失时静默跳过）。
func setArrayItemsEnum(props map[string]any, field string, values []string) {
	items, ok := walkTo(props, field, "items")
	if !ok {
		logging.Named("orchestration").Warn("schema 中找不到 items，该词表 enum 未注入",
			zap.String("field", field))
		return
	}
	items["enum"] = toAnySlice(values)
}

func toAnySlice(values []string) []any {
	out := make([]any, len(values))
	for i, v := range values {
		out[i] = v
	}
	return out
}
