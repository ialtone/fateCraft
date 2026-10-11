package sect

import (
	"fmt"
	"github.com/ialtone/fatecraft/internal/gameplay/components/production"
	"github.com/ialtone/fatecraft/internal/gameplay/components/ruledata"
	"github.com/ialtone/fatecraft/internal/gameplay/contract"
	"slices"
)

const EngineKey = "sect.management"
const DefinitionKind = "sect.foundation"

type Disciple struct {
	Key         string           `json:"key"`
	Name        string           `json:"name"`
	Specialty   string           `json:"specialty"`
	Aptitude    int64            `json:"aptitude"`
	Power       int64            `json:"power"`
	Initial     bool             `json:"initial"`
	RecruitCost map[string]int64 `json:"recruit_cost"`
}
type Mission struct {
	Key        string           `json:"key"`
	Name       string           `json:"name"`
	Cost       map[string]int64 `json:"cost"`
	Reward     map[string]int64 `json:"reward"`
	Minutes    int64            `json:"minutes"`
	Difficulty int64            `json:"difficulty"`
}
type Config struct {
	Resources      map[string]string     `json:"resources"`
	InitialStock   map[string]int64      `json:"initial_stock"`
	Facilities     []production.Facility `json:"facilities"`
	Recipes        []production.Recipe   `json:"recipes"`
	Disciples      []Disciple            `json:"disciples"`
	Missions       []Mission             `json:"missions"`
	UpkeepResource string                `json:"upkeep_resource"`
	Upkeep         int64                 `json:"upkeep"`
	TrainCost      map[string]int64      `json:"train_cost"`
	TeachCost      map[string]int64      `json:"teach_cost"`
	Hall           string                `json:"hall"`
	CrisisTime     int64                 `json:"crisis_time"`
	Threat         int64                 `json:"threat"`
	GoalFacilities map[string]int64      `json:"goal_facilities"`
	GoalDisciples  int64                 `json:"goal_disciples"`
	Deadline       int64                 `json:"deadline"`
}

func disciple(c Config, key string) Disciple {
	for _, d := range c.Disciples {
		if d.Key == key {
			return d
		}
	}
	return Disciple{}
}
func mission(c Config, key string) Mission {
	for _, m := range c.Missions {
		if m.Key == key {
			return m
		}
	}
	return Mission{}
}
func facility(c Config, key string) production.Facility {
	for _, f := range c.Facilities {
		if f.Key == key {
			return f
		}
	}
	return production.Facility{}
}
func validate(c Config) error {
	if len(c.Disciples)*len(c.Recipes) > 64 || len(c.Disciples)*len(c.Missions) > 64 {
		return fmt.Errorf("弟子与任务组合超过行动预览预算")
	}
	if len(c.Resources) < 3 || len(c.Resources) > 16 || len(c.Disciples) < 4 || len(c.Disciples) > 8 || len(c.Missions) < 1 || len(c.Missions) > 8 || c.Upkeep < 1 || c.Upkeep > 10 || c.CrisisTime < 240 || c.CrisisTime > 7*1440 || c.Deadline <= c.CrisisTime || c.Deadline > 14*1440 || c.Threat < 1 || c.Threat > 100 || c.GoalDisciples < 4 || c.GoalDisciples > int64(len(c.Disciples)) {
		return fmt.Errorf("宗门资源、弟子、危机或期限配置无效")
	}
	resources := map[string]bool{}
	for key, name := range c.Resources {
		if !contract.ValidStableKey(key) || name == "" {
			return fmt.Errorf("资源定义无效")
		}
		resources[key] = true
	}
	cost := func(values map[string]int64) error {
		if len(values) == 0 {
			return fmt.Errorf("资源清单不能为空")
		}
		if err := ruledata.ValidateStock(values); err != nil {
			return err
		}
		for key, n := range values {
			if !resources[key] || n < 1 {
				return fmt.Errorf("资源引用或数量无效 %s", key)
			}
		}
		return nil
	}
	if err := cost(c.InitialStock); err != nil {
		return err
	}
	if err := cost(c.TrainCost); err != nil {
		return err
	}
	if err := cost(c.TeachCost); err != nil {
		return err
	}
	if !resources[c.UpkeepResource] {
		return fmt.Errorf("供养资源不存在")
	}
	if err := production.Validate(c.Facilities, c.Recipes, resources); err != nil {
		return err
	}
	if facility(c, c.Hall).Key == "" || len(c.GoalFacilities) < 2 {
		return fmt.Errorf("缺少宗门核心设施或建设目标")
	}
	upgraded := false
	for key, n := range c.GoalFacilities {
		f := facility(c, key)
		if f.Key == "" || n < 1 || n > f.MaxLevel {
			return fmt.Errorf("设施目标无效")
		}
		upgraded = upgraded || n > 1
	}
	if !upgraded {
		return fmt.Errorf("重建目标需要设施升级")
	}
	keys := map[string]bool{}
	initial := 0
	specialties := map[string]bool{}
	for _, d := range c.Disciples {
		if !contract.ValidStableKey(d.Key) || len(d.Key) > 40 || keys[d.Key] || d.Name == "" || !slices.Contains([]string{"craft", "guard", "study"}, d.Specialty) || d.Aptitude < 1 || d.Aptitude > 5 || d.Power < 1 || d.Power > 5 {
			return fmt.Errorf("弟子定义无效 %s", d.Key)
		}
		keys[d.Key] = true
		if d.Initial {
			initial++
			specialties[d.Specialty] = true
		}
		if err := cost(d.RecruitCost); err != nil {
			return err
		}
	}
	if initial < 3 || initial == len(c.Disciples) || len(specialties) < 3 {
		return fmt.Errorf("需三种特长的初始弟子及可招募新人")
	}
	seen := map[string]bool{}
	for _, m := range c.Missions {
		if !contract.ValidStableKey(m.Key) || seen[m.Key] || m.Name == "" || m.Minutes < 60 || m.Minutes > 1440 || m.Difficulty < 1 || m.Difficulty > 60 {
			return fmt.Errorf("派遣任务无效")
		}
		seen[m.Key] = true
		if err := cost(m.Cost); err != nil {
			return err
		}
		if err := cost(m.Reward); err != nil {
			return err
		}
	}
	return validateDependencies(c)
}
func ValidateContent(content contract.Content) error {
	c, err := ruledata.Singleton[Config](content, DefinitionKind)
	if err != nil {
		return err
	}
	if err := validate(c); err != nil {
		return err
	}
	if err := validateBudget(content, c); err != nil {
		return err
	}
	for _, key := range []string{"rebuilt", "fallen", "dispersed", "timeout", "dissolved"} {
		found := false
		for _, o := range content.Outcomes {
			found = found || o.Key == key && o.Enabled
		}
		if !found {
			return fmt.Errorf("缺少宗门结局 %s", key)
		}
	}
	return nil
}
func Descriptor() contract.ModuleDescriptor {
	labels := map[string]string{"resources": "资源名称", "initial_stock": "初始库藏", "facilities": "设施", "recipes": "生产配方", "disciples": "弟子与候选人", "missions": "派遣任务", "key": "标识", "name": "名称", "specialty": "特长（craft/guard/study）", "aptitude": "修行资质（1–5）", "power": "基础战力（1–5）", "initial": "初始成员", "recruit_cost": "收徒成本", "cost": "任务成本", "reward": "成功回报", "minutes": "耗时（分钟）", "difficulty": "任务难度", "upkeep_resource": "供养资源", "upkeep": "每名弟子每日供养", "train_cost": "培养成本", "teach_cost": "传授传承成本", "hall": "核心大殿", "crisis_time": "危机时间（分钟）", "threat": "危机防御要求", "goal_facilities": "重建设施目标", "goal_disciples": "重建弟子人数", "deadline": "重建期限（分钟）", "build_cost": "建设成本", "upgrade_cost": "升级成本", "max_level": "最高等级", "facility": "所需设施", "level": "所需等级", "inputs": "生产投入", "outputs": "生产产出"}
	return contract.WithCampaignRuntime(contract.ModuleDescriptor{Key: EngineKey, Version: 1, Name: "宗门经营", Category: "sect", Description: "收徒分工、传承培养、生产派遣与守宗危机共同决定宗门存续。", Modes: []string{contract.RuntimeModeCampaign}, Actions: []string{"update_state", "domain_action"}, Verifiers: []string{"core.always"}, Runtime: Module{}, AbandonAction: "sect.dissolve", DefinitionLabels: map[string]string{DefinitionKind: "宗门重建"}, DefinitionSchemas: map[string]map[string]any{DefinitionKind: ruledata.Schema[Config](labels)}, DefinitionValidators: map[string]contract.DefinitionValidator{DefinitionKind: func(d contract.Definition) error {
		c, err := ruledata.Decode[Config](d.Content)
		if err != nil {
			return err
		}
		return validate(c)
	}}, ValidateContent: ValidateContent})
}
