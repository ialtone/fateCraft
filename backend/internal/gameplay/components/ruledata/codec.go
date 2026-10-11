package ruledata

import (
	"bytes"
	"encoding/json"
	"fmt"
	"unicode/utf8"

	"github.com/ialtone/fatecraft/internal/gameplay/contract"
)

func Decode[T any](value any) (T, error) {
	var result T
	raw, err := json.Marshal(value)
	if err != nil {
		return result, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return result, err
	}
	return result, nil
}

func Object(value any) (map[string]any, error) { return Decode[map[string]any](value) }

func Singleton[T any](content contract.Content, kind string) (T, error) {
	var result T
	if len(content.Definitions) != 1 || content.Definitions[0].Kind != kind {
		return result, fmt.Errorf("模块必须有且只有一个 %s 定义", kind)
	}
	if err := validateConfigBounds(content.Definitions[0].Content, kind, 0); err != nil {
		return result, err
	}
	return Decode[T](content.Definitions[0].Content)
}

// These structural limits match the generated editor schema. Narrative prose
// belongs in assets; compact rule labels must remain safe when repeated in
// dynamic action options, including options unlocked later in a run.
func validateConfigBounds(value any, path string, depth int) error {
	if depth > 8 {
		return fmt.Errorf("%s: 配置嵌套超过 8 层", path)
	}
	switch v := value.(type) {
	case string:
		if utf8.RuneCountInString(v) > 256 {
			return fmt.Errorf("%s: 规则文本超过 256 字符，请将长篇背景放入内容资源", path)
		}
	case map[string]any:
		if len(v) > 64 {
			return fmt.Errorf("%s: 配置字段超过 64 项", path)
		}
		for key, child := range v {
			if len(key) > 128 {
				return fmt.Errorf("%s: 配置标识超过 128 字节", path)
			}
			if err := validateConfigBounds(child, path+"."+key, depth+1); err != nil {
				return err
			}
		}
	case []any:
		if len(v) > 64 {
			return fmt.Errorf("%s: 配置列表超过 64 项", path)
		}
		for i, child := range v {
			if err := validateConfigBounds(child, fmt.Sprintf("%s[%d]", path, i), depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}

func ValidateStock(stock map[string]int64) error {
	if stock == nil || len(stock) > 64 {
		return fmt.Errorf("资源种类超过上限或为空")
	}
	for key, quantity := range stock {
		if !contract.ValidStableKey(key) || quantity < 0 || quantity > 1_000_000 {
			return fmt.Errorf("资源 %s 数量无效", key)
		}
	}
	return nil
}

func CanPay(stock, cost map[string]int64) bool {
	for key, quantity := range cost {
		if quantity < 0 || stock[key] < quantity {
			return false
		}
	}
	return true
}

func Pay(stock, cost map[string]int64) error {
	if !CanPay(stock, cost) {
		return contract.Reject("resources.insufficient", "持有的资源不足")
	}
	for key, quantity := range cost {
		stock[key] -= quantity
	}
	return nil
}

func Add(stock map[string]int64, key string, quantity int64) error {
	if quantity < 0 || quantity > 1_000_000-stock[key] || len(stock) >= 64 && stock[key] == 0 {
		return fmt.Errorf("资源数量或种类超过上限")
	}
	stock[key] += quantity
	return nil
}
