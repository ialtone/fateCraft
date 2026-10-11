package contract

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// OwnershipRegistry is the compiled, immutable view of where runtime fields
// may be read and written. It is built from the pinned Runtime contract; no
// client or model can add entries at runtime.
type OwnershipRegistry struct {
	Fields []FieldOwnership `json:"fields"`
	Hash   string           `json:"hash"`
}

// CompileOwnershipRegistry creates the single ownership map shared by state
// tools, Campaign actions and presentation validation. State schema entries
// are conversation-owned by default; Campaign authority must be declared in
// campaign.field_ownership.
func CompileOwnershipRegistry(contract *RuntimeContract) (OwnershipRegistry, error) {
	registry := OwnershipRegistry{Fields: []FieldOwnership{}}
	if contract == nil {
		return registry, fmt.Errorf("编译字段所有权缺少 Runtime")
	}

	for _, field := range contract.StateSchema.Fields {
		owner := field.Owner
		if owner == "" {
			owner = OwnerConversation
		}
		if owner != OwnerConversation {
			return registry, fmt.Errorf("state_schema 字段 %s 的 owner 必须为 %s", field.Path, OwnerConversation)
		}
		registry.Fields = append(registry.Fields, FieldOwnership{
			Path: field.Path, Owner: OwnerConversation, ValueType: field.Type,
			AllowChildren: field.AllowChildren, Visibility: field.Visibility,
			WriteCapability: stateWriteCapability(field),
		})
	}
	if NormalizeRuntimeMode(contract.Mode) == RuntimeModeCampaign {
		if contract.Campaign == nil {
			return registry, fmt.Errorf("campaign 模式缺少 campaign 配置")
		}
		if err := validateCampaignStateOwnership(contract.StateSchema, contract.Campaign.FieldOwnership, contract.Campaign.ActionAllowlist); err != nil {
			return registry, err
		}
		for _, declared := range contract.Campaign.FieldOwnership {
			copy := declared
			copy.EventTypes = append([]string(nil), declared.EventTypes...)
			sort.Strings(copy.EventTypes)
			registry.Fields = append(registry.Fields, copy)
		}
	}

	sort.Slice(registry.Fields, func(i, j int) bool {
		if registry.Fields[i].Path != registry.Fields[j].Path {
			return registry.Fields[i].Path < registry.Fields[j].Path
		}
		return registry.Fields[i].Owner < registry.Fields[j].Owner
	})
	for i := range registry.Fields {
		left := registry.Fields[i]
		if left.Owner == OwnerCampaign && !validOwnershipPath(left.Path) {
			return OwnershipRegistry{}, fmt.Errorf("字段所有权路径无效: %q", left.Path)
		}
		for j := i + 1; j < len(registry.Fields); j++ {
			right := registry.Fields[j]
			if !pathsOverlap(left.Path, right.Path) {
				continue
			}
			if left.Owner != right.Owner {
				return OwnershipRegistry{}, fmt.Errorf("字段所有权跨域重叠: %s(%s) 与 %s(%s)", left.Path, left.Owner, right.Path, right.Owner)
			}
		}
	}
	canonical, err := json.Marshal(registry.Fields)
	if err != nil {
		return OwnershipRegistry{}, fmt.Errorf("编码字段所有权: %w", err)
	}
	hash := sha256.Sum256(canonical)
	registry.Hash = hex.EncodeToString(hash[:])
	return registry, nil
}

func stateWriteCapability(field StateField) string {
	if field.UpdatePolicy == UpdateModel {
		return "fc_state_*"
	}
	return ""
}

// Resolve returns the most specific owner for a concrete path. A dynamic
// challenge child resolves to its declared allow_children parent, while an
// undeclared Campaign path has no owner and is rejected by the caller.
func (registry OwnershipRegistry) Resolve(path string) (FieldOwnership, bool) {
	path = strings.TrimSpace(path)
	var best FieldOwnership
	found := false
	for _, field := range registry.Fields {
		if path != field.Path && !strings.HasPrefix(path, field.Path+".") {
			continue
		}
		if path != field.Path && field.Owner == OwnerConversation && !field.AllowChildren {
			continue
		}
		if !found || len(field.Path) > len(best.Path) {
			best, found = field, true
		}
	}
	return best, found
}

// OwnerForPath is a convenience for presentation and authorization checks.
func (registry OwnershipRegistry) OwnerForPath(path string) (string, bool) {
	field, ok := registry.Resolve(path)
	if !ok {
		return "", false
	}
	return field.Owner, true
}

func validateCampaignViewOwnership(view CampaignView, registry OwnershipRegistry) error {
	check := func(path, label string) error {
		path = strings.TrimSpace(path)
		if path == "" || strings.HasPrefix(path, "item.") || strings.HasPrefix(path, "parent.") {
			return nil
		}
		path = strings.TrimPrefix(path, "snapshot.")
		field, ok := registry.Resolve(path)
		if !ok {
			return fmt.Errorf("campaign_view %s 引用了未注册所有权路径: %s", label, path)
		}
		if field.Owner != OwnerCampaign {
			return fmt.Errorf("campaign_view %s 不能引用 conversation 字段: %s", label, path)
		}
		if field.Visibility != VisibilityPlayer {
			return fmt.Errorf("campaign_view %s 引用了非公开 %s 字段: %s", label, field.Visibility, path)
		}
		return nil
	}
	if err := check(view.Header.TitlePath, "header.title_path"); err != nil {
		return err
	}
	if err := check(view.Header.TimePath, "header.time_path"); err != nil {
		return err
	}
	for _, section := range view.Sections {
		if section.VisibleWhen != nil {
			if err := check(section.VisibleWhen.Path, "section.visible_when"); err != nil {
				return err
			}
		}
		for _, field := range section.Fields {
			if err := check(field.Path, "field.path"); err != nil {
				return err
			}
			if field.VisibleWhen != nil {
				if err := check(field.VisibleWhen.Path, "field.visible_when"); err != nil {
					return err
				}
			}
		}
	}
	for _, action := range view.Actions {
		if err := check(action.SourcePath, "action.source_path"); err != nil {
			return err
		}
	}
	return nil
}

// validateGuidanceOwnership applies the same public-field boundary as
// campaign_view to guidance conditions and argument bindings.  A guidance
// item may be authored with item/parent/option context for future projections,
// but any concrete runtime path must resolve to a player-visible Campaign
// field when the Runtime is in campaign mode.
func validateGuidanceOwnership(guidance Guidance, registry OwnershipRegistry) error {
	check := func(path, label string) error {
		path = strings.TrimSpace(path)
		if path == "" || strings.HasPrefix(path, "item.") || strings.HasPrefix(path, "parent.") || strings.HasPrefix(path, "option.") {
			return nil
		}
		path = strings.TrimPrefix(path, "snapshot.")
		field, ok := registry.Resolve(strings.TrimPrefix(path, "state."))
		if !ok {
			return fmt.Errorf("guidance %s 引用了未注册所有权路径: %s", label, path)
		}
		if field.Owner != OwnerCampaign {
			return fmt.Errorf("guidance %s 不能引用 conversation 字段: %s", label, path)
		}
		if field.Visibility != VisibilityPlayer {
			return fmt.Errorf("guidance %s 引用了非公开 %s 字段: %s", label, field.Visibility, path)
		}
		return nil
	}
	for _, group := range guidance.Groups {
		for _, item := range group.Items {
			if item.VisibleWhen != nil {
				if err := check(item.VisibleWhen.Path, "visible_when"); err != nil {
					return err
				}
			}
			if item.EnabledWhen != nil {
				if err := check(item.EnabledWhen.Path, "enabled_when"); err != nil {
					return err
				}
			}
			for name, binding := range item.Arguments {
				if binding.From != "" {
					if err := check(binding.From, "argument."+name); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}
