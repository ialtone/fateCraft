package contract

import (
	"fmt"
	"strings"
)

type Definition struct {
	Kind       string
	Key        string
	Name       string
	Content    map[string]any
	Tags       []string
	Visibility string
	SortOrder  int
}

type Outcome struct {
	Key          string
	Enabled      bool
	Category     string
	RewardPoints int64
}

type Content struct {
	Runtime     *RuntimeContract
	PersonaKeys []string
	Definitions []Definition
	Outcomes    []Outcome
}

type DefinitionValidator func(Definition) error
type ContentValidator func(Content) error

// ValidateContent dispatches only to the pinned module. An unknown definition
// never becomes valid merely because another installed module recognizes it.
func (r *Registry) ValidateContent(content Content) error {
	if err := r.ValidateDefinitions(content); err != nil {
		return err
	}
	module := r.modules[moduleRegistryKey(content.Runtime.Engine.Key, content.Runtime.Engine.Version)]
	if module.ValidateContent != nil {
		return module.ValidateContent(content)
	}
	return nil
}

// ValidateDefinitions also runs for incomplete drafts, before cross-reference
// validation at publication time.
func (r *Registry) ValidateDefinitions(content Content) error {
	if content.Runtime == nil {
		return fmt.Errorf("缺少 Runtime")
	}
	module, ok := r.modules[moduleRegistryKey(content.Runtime.Engine.Key, content.Runtime.Engine.Version)]
	if !ok {
		return fmt.Errorf("玩法模块未注册")
	}
	if len(content.Definitions) > 10000 {
		return fmt.Errorf("玩法定义数量超过上限")
	}
	seen := map[string]bool{}
	for _, definition := range content.Definitions {
		identity := definition.Kind + ":" + definition.Key
		validate, registered := module.DefinitionValidators[definition.Kind]
		if !registered || validate == nil {
			return fmt.Errorf("模块 %s 不支持定义 %s", module.Key, identity)
		}
		if !ValidStableKey(definition.Kind) || !ValidStableKey(definition.Key) ||
			strings.TrimSpace(definition.Name) == "" || len([]rune(definition.Name)) > 128 ||
			definition.SortOrder < 0 || definition.Content == nil || seen[identity] {
			return fmt.Errorf("玩法定义 %s 的身份、名称、内容无效或重复", identity)
		}
		seen[identity] = true
		if definition.Visibility != VisibilityPlayer && definition.Visibility != VisibilityModel && definition.Visibility != VisibilityInternal {
			return fmt.Errorf("玩法定义 %s 的 visibility 无效", identity)
		}
		tags := map[string]bool{}
		for _, tag := range definition.Tags {
			if strings.TrimSpace(tag) == "" || tag != strings.TrimSpace(tag) || tags[tag] {
				return fmt.Errorf("玩法定义 %s 的 tag 无效或重复", identity)
			}
			tags[tag] = true
		}
		if err := validate(definition); err != nil {
			return fmt.Errorf("玩法定义 %s: %w", identity, err)
		}
	}
	return nil
}
