package contract

import (
	"fmt"
	"slices"
	"strings"
)

func validateRuntimeModule(module RuntimeModule) error {
	actions := module.Actions()
	if len(actions) == 0 || len(actions) > 64 {
		return fmt.Errorf("可信模块动作数量无效")
	}
	seen := map[string]bool{}
	for _, action := range actions {
		if !ValidStableKey(action.Key) || seen[action.Key] || action.Label == "" || action.Description == "" || len(action.Parameters) > 16 || len(action.Actors) == 0 || len(action.Writes) > 64 || len(action.Events) > 32 {
			return fmt.Errorf("可信动作声明无效: %s", action.Key)
		}
		if action.Ending && (len(action.Actors) != 1 || action.Actors[0] != "player") {
			return fmt.Errorf("玩家终局动作必须仅授权 player: %s", action.Key)
		}
		seen[action.Key] = true
		for _, actor := range action.Actors {
			if !slices.Contains([]string{"player", "model", "system"}, actor) {
				return fmt.Errorf("动作主体无效")
			}
		}
		parameters := map[string]bool{}
		for _, parameter := range action.Parameters {
			if !ValidStableKey(parameter.Key) || parameters[parameter.Key] || parameter.Label == "" || !slices.Contains([]string{"string", "integer", "boolean"}, parameter.Type) || len(parameter.Options) > 128 {
				return fmt.Errorf("动作参数声明无效")
			}
			parameters[parameter.Key] = true
			if parameter.Type == "integer" && (parameter.Minimum > parameter.Maximum || parameter.Minimum < -9007199254740991 || parameter.Maximum > 9007199254740991) {
				return fmt.Errorf("动作参数数值范围无效")
			}
		}
		for _, key := range append(slices.Clone(action.Writes), action.Events...) {
			if !ValidStableKey(key) {
				return fmt.Errorf("动作写入或事件标识无效")
			}
		}
	}
	return nil
}

func validateModuleRuntimeContract(module ModuleDescriptor, runtime *RuntimeContract) error {
	if module.Runtime == nil {
		if len(runtime.Actions.Domain) > 0 {
			return fmt.Errorf("当前模块未安装共用可信动作执行器")
		}
		return nil
	}
	known := map[string]bool{}
	for _, action := range module.Runtime.Actions() {
		known[action.Key] = true
	}
	if len(runtime.Actions.Domain) == 0 {
		return fmt.Errorf("可信玩法必须选择 domain 动作")
	}
	seen := map[string]bool{}
	for _, key := range runtime.Actions.Domain {
		if !known[key] || seen[key] {
			return fmt.Errorf("未知或重复的可信动作 %s", key)
		}
		seen[key] = true
	}
	// A module owns the dependency graph of its actions. Until it declares an
	// optional-action contract, a content pack must retain the complete catalog;
	// removing a resource or progression action can make a valid goal unreachable.
	for _, action := range module.Runtime.Actions() {
		if !seen[action.Key] {
			return fmt.Errorf("可信玩法必须保留模块动作 %s", action.Key)
		}
		if runtime.Mode == RuntimeModeCampaign && (runtime.Campaign == nil || !slices.Contains(runtime.Campaign.ActionAllowlist, action.Key)) {
			return fmt.Errorf("Campaign 必须启用模块动作 %s", action.Key)
		}
	}
	if runtime.Mode == RuntimeModeCampaign && (!seen[module.AbandonAction] || runtime.Campaign == nil || !slices.Contains(runtime.Campaign.ActionAllowlist, module.AbandonAction)) {
		return fmt.Errorf("长期模块必须保留玩家结束动作 %s", module.AbandonAction)
	}
	if runtime.Mode == RuntimeModeChallenge {
		internal, ok := FieldForPath(runtime.StateSchema, "module_state")
		if !ok || internal.Path != "module_state" || internal.Type != "object" || internal.Visibility != VisibilityInternal || internal.UpdatePolicy != UpdateModule {
			return fmt.Errorf("可信玩法必须声明 module_state 为模块独占的内部对象")
		}
		public, ok := FieldForPath(runtime.StateSchema, "gameplay")
		if !ok || public.Path != "gameplay" || public.Type != "object" || public.Visibility != VisibilityPlayer || public.UpdatePolicy != UpdateModule {
			return fmt.Errorf("可信玩法必须声明 gameplay 为模块独占的玩家投影")
		}
		for _, field := range runtime.StateSchema.Fields {
			if strings.HasPrefix(field.Path, "module_state.") {
				return fmt.Errorf("不能重声明模块内部子字段")
			}
			if strings.HasPrefix(field.Path, "gameplay.") && (field.UpdatePolicy != UpdateModule || field.Visibility != VisibilityPlayer) {
				return fmt.Errorf("玩法投影子字段不能覆盖所属权限")
			}
		}
	}
	return nil
}
