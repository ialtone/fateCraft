package effectrules

import (
	"fmt"
	"github.com/ialtone/fatecraft/internal/storage"
)

// Equipped is shared by actions and projections. An invalid equipped item or
// technique is an error, never a silently omitted bonus in a combat outcome.
func Equipped(state *storage.CampaignDomainState) (map[string]int64, error) {
	out := map[string]int64{}
	if state == nil {
		return out, nil
	}
	for _, t := range state.Techniques {
		if t.Status != "active" || t.EquippedSlot == nil {
			continue
		}
		d, exists := state.Definition(storage.DefinitionTechnique, t.TechniqueKey)
		if !exists {
			return nil, fmt.Errorf("已装配功法定义不存在: %s", t.TechniqueKey)
		}
		effects, ok := MasteredEffects(d.Content, t.Mastery, t.LearnedLayer)
		if !ok {
			return nil, fmt.Errorf("已装配功法熟练度或层级效果无效: %s", t.TechniqueKey)
		}
		Add(out, effects)
	}
	for _, item := range state.ItemInstances {
		if item.State != "equipped" || item.EquippedSlot == nil || item.Durability <= 0 {
			continue
		}
		d, exists := state.Definition(storage.DefinitionEquipment, item.DefinitionKey)
		if !exists {
			return nil, fmt.Errorf("已装备物品定义不存在: %s", item.DefinitionKey)
		}
		effects, ok := EquipmentBonuses(d.Content)
		if !ok {
			return nil, fmt.Errorf("已装备物品效果无效: %s", item.DefinitionKey)
		}
		Add(out, effects)
	}
	return out, nil
}
