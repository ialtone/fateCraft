package trial

import (
	"fmt"
	"github.com/ialtone/fatecraft/internal/gameplay/components/ruledata"
	"github.com/ialtone/fatecraft/internal/gameplay/contract"
	"slices"
)

const DefinitionKind = "cultivation.trial_rules"
const Day int64 = 1440
const Year int64 = 365 * Day

var realmKeys = []string{"mortal", "qi-refining", "foundation", "golden-core", "nascent-soul", "spirit-transformation", "void-refining", "body-integration", "great-ascension"}
var itemKeys = []string{"stone", "herb", "breakthrough-pill", "healing-pill", "life-pill", "thunder-talisman", "ward", "void-anchor"}
var outcomeKeys = []string{"orthodox_ascension", "void_ascension", "affinity_ascension", "peaceful_passing", "lifespan_exhausted", "perished", "abandoned"}

type Realm struct {
	Key                string `json:"key"`
	Name               string `json:"name"`
	LifespanYears      int64  `json:"lifespan_years" minimum:"30" maximum:"20000"`
	PracticeDays       int64  `json:"practice_days" minimum:"1" maximum:"694"`
	PracticeCost       int64  `json:"practice_cost" minimum:"1" maximum:"1000"`
	PracticeGain       int64  `json:"practice_gain" minimum:"10" maximum:"35"`
	BreakthroughChance int64  `json:"breakthrough_chance" minimum:"30" maximum:"95"`
}
type Birth struct {
	Key          string           `json:"key"`
	Name         string           `json:"name"`
	Stock        map[string]int64 `json:"stock"`
	VigorBonus   int64            `json:"vigor_bonus" minimum:"-10" maximum:"10"`
	InsightBonus int64            `json:"insight_bonus" minimum:"-10" maximum:"10"`
	LuckBonus    int64            `json:"luck_bonus" minimum:"-10" maximum:"10"`
}
type Method struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	GrowthBonus int64  `json:"growth_bonus" minimum:"0" maximum:"5"`
	GuardBonus  int64  `json:"guard_bonus" minimum:"0" maximum:"5"`
}
type Item struct {
	Key      string `json:"key"`
	Name     string `json:"name"`
	Price    int64  `json:"price" minimum:"0" maximum:"1000"`
	MinRealm int    `json:"min_realm" minimum:"0" maximum:"8"`
}
type Activity struct {
	Key       string `json:"key"`
	Name      string `json:"name"`
	Kind      string `json:"kind" enum:"work,explore,combat,social,inheritance,affinity"`
	MinRealm  int    `json:"min_realm" minimum:"0" maximum:"8"`
	Days      int64  `json:"days" minimum:"1" maximum:"694"`
	Chance    int64  `json:"chance" minimum:"20" maximum:"100"`
	Damage    int64  `json:"damage" minimum:"0" maximum:"60"`
	Stones    int64  `json:"stones" minimum:"0" maximum:"100"`
	Herbs     int64  `json:"herbs" minimum:"0" maximum:"20"`
	Cost      int64  `json:"cost" minimum:"0" maximum:"100"`
	Karma     int64  `json:"karma" minimum:"-5" maximum:"5"`
	Limit     int64  `json:"limit" minimum:"0" maximum:"10"`
	MethodKey string `json:"method_key"`
}
type Recipe struct {
	Key      string           `json:"key"`
	Name     string           `json:"name"`
	Output   string           `json:"output"`
	Inputs   map[string]int64 `json:"inputs"`
	MinRealm int              `json:"min_realm" minimum:"0" maximum:"8"`
	Days     int64            `json:"days" minimum:"1" maximum:"694"`
	Chance   int64            `json:"chance" minimum:"30" maximum:"100"`
}
type Routes struct {
	AffinityChance      int64 `json:"affinity_chance" minimum:"5" maximum:"80"`
	AffinityAttempts    int64 `json:"affinity_attempts" minimum:"1" maximum:"2"`
	RiftDiscoveryChance int64 `json:"rift_discovery_chance" minimum:"10" maximum:"90"`
	RiftChance          int64 `json:"rift_chance" minimum:"20" maximum:"90"`
	TribulationChance   int64 `json:"tribulation_chance" minimum:"40" maximum:"90"`
	WaveDamage          int64 `json:"wave_damage" minimum:"10" maximum:"50"`
	WaveCost            int64 `json:"wave_cost" minimum:"1" maximum:"20"`
	CooldownDays        int64 `json:"cooldown_days" minimum:"1" maximum:"694"`
}
type Config struct {
	Difficulty int64      `json:"difficulty" minimum:"-20" maximum:"20"`
	Realms     []Realm    `json:"realms"`
	Births     []Birth    `json:"births"`
	Methods    []Method   `json:"methods"`
	Items      []Item     `json:"items"`
	Activities []Activity `json:"activities"`
	Recipes    []Recipe   `json:"recipes"`
	Routes     Routes     `json:"routes"`
}

func validateConfig(c Config) error {
	if c.Difficulty < -20 || c.Difficulty > 20 || len(c.Realms) != 9 || len(c.Births) < 1 || len(c.Births) > 16 || len(c.Methods) < 2 || len(c.Methods) > 8 || len(c.Items) != len(itemKeys) || len(c.Activities) < 4 || len(c.Activities) > 16 || len(c.Recipes) < 1 || len(c.Recipes) > 8 {
		return fmt.Errorf("单局规则规模或难度无效")
	}
	for i, r := range c.Realms {
		if r.Key != realmKeys[i] || r.Name == "" || r.LifespanYears < 30 || r.LifespanYears > 20000 || i > 0 && r.LifespanYears <= c.Realms[i-1].LifespanYears || r.PracticeDays < 1 || r.PracticeDays > 694 || r.PracticeCost < 1 || r.PracticeCost > 1000 || r.PracticeGain < 10 || r.PracticeGain > 35 || r.BreakthroughChance < 30 || r.BreakthroughChance > 95 {
			return fmt.Errorf("境界成长或寿元配置无效: %s", r.Key)
		}
	}
	seen := map[string]bool{}
	validIdentity := func(key, name string) bool {
		if !contract.ValidStableKey(key) || name == "" || len([]rune(name)) > 64 || seen[key] {
			return false
		}
		seen[key] = true
		return true
	}
	for _, b := range c.Births {
		if !validIdentity(b.Key, b.Name) || b.VigorBonus < -10 || b.VigorBonus > 10 || b.InsightBonus < -10 || b.InsightBonus > 10 || b.LuckBonus < -10 || b.LuckBonus > 10 {
			return fmt.Errorf("出身配置无效")
		}
		if err := ruledata.ValidateStock(b.Stock); err != nil {
			return err
		}
		for k, n := range b.Stock {
			if !slices.Contains([]string{"stone", "herb"}, k) || n > 10 {
				return fmt.Errorf("出身只能配置少量普通资源")
			}
		}
	}
	seen = map[string]bool{}
	for _, m := range c.Methods {
		if !validIdentity(m.Key, m.Name) || m.GrowthBonus < 0 || m.GrowthBonus > 5 || m.GuardBonus < 0 || m.GuardBonus > 5 {
			return fmt.Errorf("传承配置无效")
		}
	}
	methods := seen
	seen = map[string]bool{}
	for _, item := range c.Items {
		if !validIdentity(item.Key, item.Name) || !slices.Contains(itemKeys, item.Key) || item.Price < 0 || item.Price > 1000 || item.MinRealm < 0 || item.MinRealm > 8 || item.Key != "stone" && item.Price == 0 || item.Key == "stone" && item.Price != 0 {
			return fmt.Errorf("物品价格或身份无效")
		}
	}
	seen = map[string]bool{}
	sources, safeWork, affinity := map[string]bool{}, false, false
	for _, a := range c.Activities {
		if !validIdentity(a.Key, a.Name) || !slices.Contains([]string{"work", "explore", "combat", "social", "inheritance", "affinity"}, a.Kind) || a.MinRealm < 0 || a.MinRealm > 8 || a.Days < 1 || a.Days > 694 || a.Chance < 20 || a.Chance > 100 || a.Damage < 0 || a.Damage > 60 || a.Stones < 0 || a.Stones > 100 || a.Herbs < 0 || a.Herbs > 20 || a.Cost < 0 || a.Cost > 100 || a.Karma < -5 || a.Karma > 5 || a.Limit < 0 || a.Limit > 10 {
			return fmt.Errorf("历练配置无效: %s", a.Key)
		}
		if a.Kind == "inheritance" {
			if !methods[a.MethodKey] || a.MinRealm != 0 || a.Limit != 1 {
				return fmt.Errorf("首部传承必须在凡人阶段可取得且一次生效")
			}
			sources[a.MethodKey] = true
		} else if a.MethodKey != "" {
			return fmt.Errorf("非传承行动不能授法")
		}
		if a.Kind == "affinity" {
			if a.Limit != 1 || a.MinRealm > 3 {
				return fmt.Errorf("仙缘委托需要一次性可达来源")
			}
			affinity = true
		}
		if a.Kind == "work" && a.MinRealm == 0 && a.Chance == 100 && a.Damage == 0 && a.Cost == 0 && a.Stones > 0 && a.Herbs > 0 && a.Limit == 0 {
			safeWork = true
		}
	}
	if len(sources) != len(methods) || !safeWork || !affinity {
		return fmt.Errorf("缺少可达传承、安全谋生或仙缘委托")
	}
	seen = map[string]bool{}
	for _, r := range c.Recipes {
		if !validIdentity(r.Key, r.Name) || !slices.Contains(itemKeys[2:], r.Output) || r.MinRealm < 0 || r.MinRealm > 8 || r.Days < 1 || r.Days > 694 || r.Chance < 30 || r.Chance > 100 || len(r.Inputs) == 0 {
			return fmt.Errorf("炼制配置无效")
		}
		if err := ruledata.ValidateStock(r.Inputs); err != nil {
			return err
		}
		for k, n := range r.Inputs {
			if !slices.Contains(itemKeys, k) || k == r.Output || n < 1 || n > 100 {
				return fmt.Errorf("炼制必须消耗合法的其他材料")
			}
		}
	}
	r := c.Routes
	if r.AffinityChance < 5 || r.AffinityChance > 80 || r.AffinityAttempts < 1 || r.AffinityAttempts > 2 || r.RiftDiscoveryChance < 10 || r.RiftDiscoveryChance > 90 || r.RiftChance < 20 || r.RiftChance > 90 || r.TribulationChance < 40 || r.TribulationChance > 90 || r.WaveDamage < 10 || r.WaveDamage > 50 || r.WaveCost < 1 || r.WaveCost > 20 || r.CooldownDays < 1 || r.CooldownDays > 694 {
		return fmt.Errorf("飞升路线概率、成本或冷却无效")
	}
	return nil
}

func ValidateContent(content contract.Content) error {
	c, err := ruledata.Singleton[Config](content, DefinitionKind)
	if err != nil {
		return err
	}
	if err = validateConfig(c); err != nil {
		return err
	}
	if len(content.Outcomes) != len(outcomeKeys) {
		return fmt.Errorf("单局必须配置三条飞升和四种非奖励结局")
	}
	for _, key := range outcomeKeys {
		found := false
		for _, o := range content.Outcomes {
			if o.Key == key && o.Enabled {
				found = true
				if slices.Contains(outcomeKeys[3:], key) && o.RewardPoints != 0 {
					return fmt.Errorf("非飞升结局不能奖励积分")
				}
			}
		}
		if !found {
			return fmt.Errorf("缺少结局 %s", key)
		}
	}
	return nil
}

func configSchema() map[string]any {
	return ruledata.Schema[Config](map[string]string{"difficulty": "额外难度（降低成功率百分点）", "realms": "九阶成长", "births": "出身映射", "methods": "首部传承", "items": "物品与价格", "activities": "可信历练", "recipes": "炼制配方", "routes": "飞升路线", "key": "标识", "name": "名称", "lifespan_years": "寿元上限（年）", "practice_days": "每次闭关天数", "practice_cost": "闭关灵石", "practice_gain": "基础修为进度", "breakthrough_chance": "基础突破概率（%）", "stock": "初始普通资源", "vigor_bonus": "根骨修正", "insight_bonus": "悟性修正", "luck_bonus": "气运修正", "growth_bonus": "闭关进度加成", "guard_bonus": "防护减伤", "price": "购买灵石", "min_realm": "最低境界序号（凡人为0）", "kind": "历练类型", "days": "耗时（天）", "chance": "基础成功率（%）", "damage": "失败伤害", "stones": "灵石收益基数（随境界增长）", "herbs": "灵草收益", "cost": "灵石成本", "karma": "业力变化（正数增加风险）", "limit": "成功次数上限（0为不限）", "method_key": "授予传承", "output": "炼成物", "inputs": "材料", "affinity_chance": "寻得仙缘概率（%）", "affinity_attempts": "每世仙缘机会数", "rift_discovery_chance": "发现裂缝概率（%）", "rift_chance": "裂缝穿越概率（%）", "tribulation_chance": "天劫基础概率（%）", "wave_damage": "雷劫失败基础伤害", "wave_cost": "每波灵石成本", "cooldown_days": "失败/撤退后准备天数"})
}
