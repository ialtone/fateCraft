package originrules

import (
	"fmt"
	"github.com/ialtone/fatecraft/internal/gameplay/components/ruledata"
	"github.com/ialtone/fatecraft/internal/gameplay/contract"
)

const DefinitionKind = "cultivation.origins"

type Origin struct {
	PersonaKey string           `json:"persona_key"`
	Name       string           `json:"name"`
	Items      map[string]int64 `json:"items"`
}
type Config struct {
	Origins []Origin `json:"origins"`
}

func Parse(raw map[string]any) (Config, error) {
	c, err := ruledata.Decode[Config](raw)
	if err != nil {
		return c, err
	}
	if len(c.Origins) < 1 || len(c.Origins) > 32 {
		return c, fmt.Errorf("出身方案需要 1–32 项")
	}
	seen := map[string]bool{}
	for _, o := range c.Origins {
		if !contract.ValidStableKey(o.PersonaKey) || seen[o.PersonaKey] || o.Name == "" || len([]rune(o.Name)) > 64 || len(o.Items) < 1 || len(o.Items) > 8 {
			return c, fmt.Errorf("出身身份、名称或物资组合无效")
		}
		seen[o.PersonaKey] = true
		if err = ruledata.ValidateStock(o.Items); err != nil {
			return c, err
		}
		total := int64(0)
		for _, n := range o.Items {
			if n < 1 || n > 20 {
				return c, fmt.Errorf("出身单项物资须为 1–20")
			}
			total += n
		}
		if total > 30 {
			return c, fmt.Errorf("出身额外物资总量超过 30")
		}
	}
	return c, nil
}
func Validate(content contract.Content) error {
	var config *Config
	items := map[string]contract.Definition{}
	for _, d := range content.Definitions {
		if d.Kind == "item" {
			items[d.Key] = d
		}
		if d.Kind == DefinitionKind {
			if config != nil || d.Key != "origins" {
				return fmt.Errorf("出身配置必须唯一且标识为 origins")
			}
			c, err := Parse(d.Content)
			if err != nil {
				return err
			}
			config = &c
		}
	}
	if config == nil {
		return nil
	}
	configured := map[string]bool{}
	for _, o := range config.Origins {
		configured[o.PersonaKey] = true
		for key := range o.Items {
			d, exists := items[key]
			starting, err := ruledata.Decode[int64](d.Content["starting_quantity"])
			if !exists || err != nil || starting <= 0 || d.Content["stackable"] != true {
				return fmt.Errorf("出身物资 %s 必须为已有的普通开局资源", key)
			}
		}
	}
	for _, key := range content.PersonaKeys {
		if !configured[key] {
			return fmt.Errorf("出身 %s 缺少实际开局方案", key)
		}
	}
	return nil
}
func Schema() map[string]any {
	return ruledata.Schema[Config](map[string]string{"origins": "出身资源组合", "persona_key": "Persona 标识", "name": "出身名称", "items": "额外开局物资（在基础物资上增加）"})
}
