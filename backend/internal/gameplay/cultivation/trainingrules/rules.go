package trainingrules

import (
	"fmt"
	"math"
	"slices"
	"sort"

	"github.com/ialtone/fatecraft/internal/gameplay/components/ruledata"
	"github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/gameplay/cultivation/effectrules"
	"github.com/ialtone/fatecraft/internal/storage"
)

const DefinitionKind = "cultivation.training"
const ActionRetreat = "cultivation.retreat"
const ActionRecover = "character.recover"

type Material struct {
	ItemKey  string `json:"item_key"`
	Quantity int64  `json:"quantity" minimum:"1" maximum:"1000000"`
}

// Each plan is immutable content. A long retreat is a single paid undertaking,
// not an unbounded loop of ordinary practice actions.
type Plan struct {
	Mode            string     `json:"mode" enum:"retreat,recovery"`
	MinRealm        int        `json:"min_realm_rank" minimum:"0" maximum:"1000"`
	MaxRealm        int        `json:"max_realm_rank" minimum:"0" maximum:"1000"`
	Days            int64      `json:"days" minimum:"1" maximum:"3650"`
	LocationKeys    []string   `json:"location_keys"`
	TechniqueKeys   []string   `json:"technique_keys"`
	Materials       []Material `json:"materials"`
	CultivationGain int64      `json:"cultivation_gain" minimum:"0" maximum:"100000000"`
	MasteryGain     int64      `json:"mastery_gain" minimum:"0" maximum:"10000"`
	ManaCost        int64      `json:"mana_cost" minimum:"0" maximum:"1000000000"`
	HealthBPS       int64      `json:"health_restore_bps" minimum:"0" maximum:"10000"`
	ManaBPS         int64      `json:"mana_restore_bps" minimum:"0" maximum:"10000"`
	SenseBPS        int64      `json:"sense_restore_bps" minimum:"0" maximum:"10000"`
	InjuryRelief    int64      `json:"injury_relief" minimum:"0" maximum:"100"`
}

func Parse(raw map[string]any) (Plan, error) {
	p, err := ruledata.Decode[Plan](raw)
	if err != nil {
		return p, err
	}
	if (p.Mode != "retreat" && p.Mode != "recovery") || p.MinRealm < 0 || p.MaxRealm < p.MinRealm || p.MaxRealm > 1000 || p.Days < 1 || p.Days > 3650 || len(p.LocationKeys) < 1 || len(p.LocationKeys) > 32 || len(p.TechniqueKeys) > 16 || len(p.Materials) > 8 {
		return p, fmt.Errorf("修行方案的模式、境界、时长或选项数量无效")
	}
	for _, keys := range [][]string{p.LocationKeys, p.TechniqueKeys} {
		seen := map[string]bool{}
		for _, key := range keys {
			if !contract.ValidStableKey(key) || seen[key] {
				return p, fmt.Errorf("修行方案引用无效或重复")
			}
			seen[key] = true
		}
	}
	seen := map[string]bool{}
	for _, m := range p.Materials {
		if !contract.ValidStableKey(m.ItemKey) || seen[m.ItemKey] || m.Quantity < 1 || m.Quantity > 1000000 {
			return p, fmt.Errorf("修行材料无效或重复")
		}
		seen[m.ItemKey] = true
	}
	if p.CultivationGain < 0 || p.CultivationGain > 100000000 || p.MasteryGain < 0 || p.MasteryGain > 10000 || p.ManaCost < 0 || p.ManaCost > 1000000000 || p.HealthBPS < 0 || p.HealthBPS > 10000 || p.ManaBPS < 0 || p.ManaBPS > 10000 || p.SenseBPS < 0 || p.SenseBPS > 10000 || p.InjuryRelief < 0 || p.InjuryRelief > 100 {
		return p, fmt.Errorf("修行收益或恢复数值越界")
	}
	if p.Mode == "retreat" {
		if len(p.TechniqueKeys) == 0 || len(p.Materials) == 0 || p.CultivationGain < 1 || p.MasteryGain < 1 || p.HealthBPS+p.ManaBPS+p.SenseBPS+p.InjuryRelief != 0 {
			return p, fmt.Errorf("闭关必须有功法、材料和成长收益，不能兼做免费恢复")
		}
	} else if len(p.TechniqueKeys) != 0 || p.CultivationGain+p.MasteryGain+p.ManaCost != 0 || p.HealthBPS+p.ManaBPS+p.SenseBPS+p.InjuryRelief == 0 {
		return p, fmt.Errorf("休整必须声明恢复效果，不能增加修为或功法熟练度")
	}
	return p, nil
}

func Schema() map[string]any {
	return ruledata.Schema[Plan](map[string]string{"mode": "方案种类", "min_realm_rank": "最低境界", "max_realm_rank": "最高境界", "days": "耗时（天）", "location_keys": "可用地点", "technique_keys": "任一已装配功法（按顺序选择）", "materials": "每次消耗（普通品质）", "item_key": "物品", "quantity": "数量", "cultivation_gain": "闭关修为", "mastery_gain": "闭关熟练度", "mana_cost": "闭关法力消耗", "health_restore_bps": "恢复气血比例（万分比）", "mana_restore_bps": "恢复法力比例（万分比）", "sense_restore_bps": "恢复神识比例（万分比）", "injury_relief": "每项普通伤势减轻值"})
}

func Technique(p Plan, state *storage.CampaignDomainState) *storage.CampaignTechnique {
	for _, key := range p.TechniqueKeys {
		for i := range state.Techniques {
			t := &state.Techniques[i]
			if t.TechniqueKey == key && t.Status == "active" && t.EquippedSlot != nil {
				return t
			}
		}
	}
	return nil
}

func Ready(p Plan, row storage.Campaign, state *storage.CampaignDomainState) error {
	if row.GameTime < 0 || row.GameTime > math.MaxInt64-p.Days*1440 {
		return fmt.Errorf("修行时间跨度无效")
	}
	if state == nil || state.Character == nil || state.Character.Health <= 0 || row.Status != storage.CampaignStatusActive {
		return fmt.Errorf("角色当前不能修行或休整")
	}
	c := state.Character
	if c.RealmRank < p.MinRealm || c.RealmRank > p.MaxRealm {
		return fmt.Errorf("当前境界不适合此方案")
	}
	if !slices.Contains(p.LocationKeys, row.CurrentLocationKey) {
		return fmt.Errorf("请前往方案指定地点")
	}
	if c.LifespanDays-c.AgeDays <= p.Days {
		return fmt.Errorf("剩余寿元不足以完成此次修行")
	}
	for _, e := range state.Encounters {
		if e.Status == "active" {
			return fmt.Errorf("需要先结束当前遭遇")
		}
	}
	for _, q := range state.Quests {
		if q.Status == "active" && q.DueGameTime != nil && *q.DueGameTime <= row.GameTime+p.Days*1440 {
			return fmt.Errorf("本次耗时将越过任务期限，请先处理限时承诺")
		}
	}
	if p.Mode == "retreat" {
		t := Technique(p, state)
		if t == nil {
			return fmt.Errorf("尚未装配方案要求的功法")
		}
		d, ok := state.Definition(storage.DefinitionTechnique, t.TechniqueKey)
		if !ok {
			return fmt.Errorf("闭关功法定义不存在")
		}
		if _, ok := effectrules.MasteredEffects(d.Content, t.Mastery, t.LearnedLayer); !ok {
			return fmt.Errorf("功法熟练度与层级不一致")
		}
		if c.CultivationProgress >= c.CultivationRequired {
			return fmt.Errorf("当前境界修为已满，请突破或处理飞升准备")
		}
		if c.Mana < p.ManaCost {
			return fmt.Errorf("法力不足，请先恢复")
		}
	} else {
		needsRecovery := p.HealthBPS > 0 && c.Health < c.MaxHealth || p.ManaBPS > 0 && c.Mana < c.MaxMana || p.SenseBPS > 0 && c.SpiritualSense < c.MaxSpiritualSense
		for _, raw := range c.Injuries {
			if condition, ok := raw.(map[string]any); ok && condition["category"] == "injury" && p.InjuryRelief > 0 {
				needsRecovery = true
			}
		}
		if !needsRecovery {
			return fmt.Errorf("当前无需此方案的恢复")
		}
	}
	for _, m := range p.Materials {
		found := false
		for _, s := range state.ItemStacks {
			if s.ItemKey == m.ItemKey && s.QualityKey == "common" && s.Quantity >= m.Quantity {
				found = true
				break
			}
		}
		if !found {
			d, _ := state.Definition(storage.DefinitionItem, m.ItemKey)
			return fmt.Errorf("准备材料不足：%s × %d", d.Name, m.Quantity)
		}
	}
	return nil
}

type View struct {
	Key             string `json:"key"`
	Name            string `json:"name"`
	Mode            string `json:"mode"`
	Days            int64  `json:"days"`
	Available       bool   `json:"available"`
	Reason          string `json:"reason"`
	CostText        string `json:"cost_text"`
	CultivationGain int64  `json:"cultivation_gain"`
	MasteryGain     int64  `json:"mastery_gain"`
	ManaCost        int64  `json:"mana_cost"`
	TechniqueName   string `json:"technique_name"`
	EffectText      string `json:"effect_text"`
}

func Views(row storage.Campaign, state *storage.CampaignDomainState) ([]View, error) {
	out := []View{}
	if state == nil || state.Character == nil {
		return out, nil
	}
	for _, d := range state.Definitions {
		if d.Kind != DefinitionKind {
			continue
		}
		p, err := Parse(d.Content)
		if err != nil {
			return nil, err
		}
		if state.Character.RealmRank < p.MinRealm || state.Character.RealmRank > p.MaxRealm {
			continue
		}
		v := View{Key: d.Key, Name: d.Name, Mode: p.Mode, Days: p.Days, CultivationGain: p.CultivationGain, MasteryGain: p.MasteryGain, ManaCost: p.ManaCost, Available: true}
		if p.Mode == "recovery" {
			v.EffectText = fmt.Sprintf("恢复气血 %.0f%%、法力 %.0f%%、神识 %.0f%%；普通伤势减轻 %d", float64(p.HealthBPS)/100, float64(p.ManaBPS)/100, float64(p.SenseBPS)/100, p.InjuryRelief)
		}
		if err := Ready(p, row, state); err != nil {
			v.Available = false
			v.Reason = err.Error()
		}
		if t := Technique(p, state); t != nil {
			definition, _ := state.Definition(storage.DefinitionTechnique, t.TechniqueKey)
			v.TechniqueName = definition.Name
		}
		for i, m := range p.Materials {
			if i > 0 {
				v.CostText += "，"
			}
			item, _ := state.Definition(storage.DefinitionItem, m.ItemKey)
			v.CostText += fmt.Sprintf("%s × %d", item.Name, m.Quantity)
		}
		if v.CostText == "" {
			v.CostText = "消耗时间，无物品费用"
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}
