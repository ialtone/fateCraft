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
	// Definitions are module-owned and are the only definitions a rule module
	// ever sees. PlatformDefinitions are validated by the platform and are
	// excluded from module execution.
	Definitions         []Definition
	PlatformDefinitions []Definition
	Outcomes            []Outcome
}

type DefinitionValidator func(Definition) error
type ContentValidator func(Content) error

// ValidateContent dispatches only to the pinned module. An unknown definition
// never becomes valid merely because another installed module recognizes it.
// Platform-owned kinds are validated here first and are removed from the
// module input so single-definition modules keep their exact-count contract.
func (r *Registry) ValidateContent(content Content) error {
	content = splitPlatformDefinitions(content)
	if err := r.ValidateDefinitions(content); err != nil {
		return err
	}
	if err := validateProgressRewardsBudget(content); err != nil {
		return err
	}
	module := r.modules[moduleRegistryKey(content.Runtime.Engine.Key, content.Runtime.Engine.Version)]
	if module.ValidateContent != nil {
		return module.ValidateContent(content)
	}
	return nil
}

// splitPlatformDefinitions moves platform-owned entries out of the module
// definition list. Callers may build content from either list.
func splitPlatformDefinitions(content Content) Content {
	var moduleDefinitions []Definition
	for _, definition := range content.Definitions {
		if !IsPlatformDefinitionKind(definition.Kind) {
			moduleDefinitions = append(moduleDefinitions, definition)
			continue
		}
		content.PlatformDefinitions = append(content.PlatformDefinitions, definition)
	}
	content.Definitions = moduleDefinitions
	return content
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
	content = splitPlatformDefinitions(content)
	if len(content.Definitions)+len(content.PlatformDefinitions) > 10000 {
		return fmt.Errorf("玩法定义数量超过上限")
	}
	seen := map[string]bool{}
	for _, definition := range content.Definitions {
		identity, err := validateDefinitionIdentity(definition, seen)
		if err != nil {
			return err
		}
		validate, registered := module.DefinitionValidators[definition.Kind]
		if !registered || validate == nil {
			return fmt.Errorf("模块 %s 不支持定义 %s", module.Key, identity)
		}
		if err := validate(definition); err != nil {
			return fmt.Errorf("玩法定义 %s: %w", identity, err)
		}
	}
	for _, definition := range content.PlatformDefinitions {
		identity, err := validateDefinitionIdentity(definition, seen)
		if err != nil {
			return err
		}
		if err := r.validatePlatformDefinition(content.Runtime, definition); err != nil {
			return fmt.Errorf("玩法定义 %s: %w", identity, err)
		}
	}
	return nil
}

// validateDefinitionIdentity applies the rules shared by module and platform
// definitions and records the identity for duplicate detection.
func validateDefinitionIdentity(definition Definition, seen map[string]bool) (string, error) {
	identity := definition.Kind + ":" + definition.Key
	if !ValidStableKey(definition.Kind) || !ValidStableKey(definition.Key) ||
		strings.TrimSpace(definition.Name) == "" || len([]rune(definition.Name)) > 128 ||
		definition.SortOrder < 0 || definition.Content == nil || seen[identity] {
		return identity, fmt.Errorf("玩法定义 %s 的身份、名称、内容无效或重复", identity)
	}
	seen[identity] = true
	if definition.Visibility != VisibilityPlayer && definition.Visibility != VisibilityModel && definition.Visibility != VisibilityInternal {
		return identity, fmt.Errorf("玩法定义 %s 的 visibility 无效", identity)
	}
	tags := map[string]bool{}
	for _, tag := range definition.Tags {
		if strings.TrimSpace(tag) == "" || tag != strings.TrimSpace(tag) || tags[tag] {
			return identity, fmt.Errorf("玩法定义 %s 的 tag 无效或重复", identity)
		}
		tags[tag] = true
	}
	return identity, nil
}
