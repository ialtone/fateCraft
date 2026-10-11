package campaign

import (
	"fmt"
	"slices"

	"github.com/ialtone/fatecraft/internal/gameplay/contract"
)

// ValidateInstallation checks executable registrations against the catalogue
// used by publication and import. Missing handlers fail startup, not a turn.
func (s *Service) ValidateInstallation(registry *contract.Registry) error {
	declared := map[moduleActionKey]bool{}
	for _, module := range registry.Modules() {
		if !slices.Contains(module.Modes, contract.RuntimeModeCampaign) {
			continue
		}
		engine := contract.EngineRef{Key: module.Key, Version: module.Version}
		for _, key := range module.CampaignLifecycleHandlers {
			if s.initializers[key] == nil {
				return fmt.Errorf("模块 %s@%d 缺少初始化处理器 %s", module.Key, module.Version, key)
			}
		}
		for _, key := range module.CampaignProjectionHandlers {
			if s.projections[key] == nil {
				return fmt.Errorf("模块 %s@%d 缺少投影处理器 %s", module.Key, module.Version, key)
			}
		}
		for _, key := range module.CampaignActions {
			identity := moduleActionKey{Engine: engine, Action: key}
			if declared[identity] {
				return fmt.Errorf("模块 %s@%d 重复声明动作 %s", module.Key, module.Version, key)
			}
			declared[identity] = true
			action, ok := s.actions[identity]
			if !ok {
				return fmt.Errorf("模块 %s@%d 缺少动作处理器 %s", module.Key, module.Version, key)
			}
			if key == module.AbandonAction && (!action.Definition.Ending || !sameStringSet(action.Definition.Actors, []string{"player"})) {
				return fmt.Errorf("中止动作必须仅授权玩家并声明 Ending: %s", key)
			}
			if !sameStringSet(module.CampaignActionWrites[key], action.Definition.Writes) || !sameStringSet(module.CampaignActionEvents[key], action.Definition.Events) {
				return fmt.Errorf("模块 %s@%d 的动作 %s 与目录中的写入/事件声明不一致", module.Key, module.Version, key)
			}
		}
	}
	for key := range s.actions {
		if !declared[key] {
			return fmt.Errorf("动作处理器未在模块目录声明: %s@%d/%s", key.Engine.Key, key.Engine.Version, key.Action)
		}
	}
	return nil
}

func sameStringSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	left, right = slices.Clone(left), slices.Clone(right)
	slices.Sort(left)
	slices.Sort(right)
	return slices.Equal(left, right)
}
