package contentrules

import (
	"fmt"
	"slices"

	"github.com/ialtone/fatecraft/internal/gameplay/components/worldcontent"
	"github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/trainingrules"
)

func validateTraining(content contract.Content) error {
	catalog := map[string]contract.Definition{}
	for _, d := range content.Definitions {
		catalog[d.Kind+":"+d.Key] = d
	}
	count := 0
	for _, d := range content.Definitions {
		if d.Kind != trainingrules.DefinitionKind {
			continue
		}
		count++
		if count > 64 {
			return fmt.Errorf("修行方案不能超过 64 项")
		}
		p, err := trainingrules.Parse(d.Content)
		if err != nil {
			return err
		}
		action := trainingrules.ActionRecover
		if p.Mode == "retreat" {
			action = trainingrules.ActionRetreat
		}
		if content.Runtime == nil || content.Runtime.Campaign == nil || !slices.Contains(content.Runtime.Campaign.ActionAllowlist, action) {
			return fmt.Errorf("方案 %s 缺少行动 %s", d.Key, action)
		}
		for _, key := range p.LocationKeys {
			location, exists := catalog["location:"+key]
			if !exists || location.Content["node_type"] == "region" || location.Content["node_type"] == "realm" {
				return fmt.Errorf("修行方案 %s 指定的地点不可用: %s", d.Key, key)
			}
		}
		for _, key := range p.TechniqueKeys {
			t, exists := catalog["technique:"+key]
			rank, valid := worldcontent.NonNegativeDefinitionInt(t.Content["minimum_realm_rank"])
			if !exists || !valid || rank > int64(p.MinRealm) {
				return fmt.Errorf("闭关功法 %s 必须能在方案最低境界学习", key)
			}
		}
		for _, m := range p.Materials {
			item, ok := catalog["item:"+m.ItemKey]
			if !ok || item.Content["stackable"] != true {
				return fmt.Errorf("修行方案材料 %s 不存在或不可堆叠", m.ItemKey)
			}
		}
	}
	return nil
}
