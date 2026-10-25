package management

import (
	"fmt"
	"github.com/ialtone/fatecraft/internal/gameplay/components/production"
	"github.com/ialtone/fatecraft/internal/gameplay/components/ruledata"
	"github.com/ialtone/fatecraft/internal/gameplay/contract"
)

const EngineKey = "management.settlement"
const DefinitionKind = "management.business"

type Resource struct {
	Key  string `json:"key"`
	Name string `json:"name"`
	Buy  int64  `json:"buy"`
	Sell int64  `json:"sell"`
}
type Order struct {
	Key      string           `json:"key"`
	Name     string           `json:"name"`
	Items    map[string]int64 `json:"items"`
	Income   int64            `json:"income"`
	Deadline int64            `json:"deadline"`
	Penalty  int64            `json:"penalty"`
}
type Config struct {
	InitialStock   map[string]int64      `json:"initial_stock"`
	Resources      []Resource            `json:"resources"`
	Facilities     []production.Facility `json:"facilities"`
	Recipes        []production.Recipe   `json:"recipes"`
	Orders         []Order               `json:"orders"`
	DailyExpense   int64                 `json:"daily_expense"`
	GoalFacilities map[string]int64      `json:"goal_facilities"`
	GoalOrders     int64                 `json:"goal_orders"`
	GoalRevenue    int64                 `json:"goal_revenue"`
	Deadline       int64                 `json:"deadline"`
}

func validate(c Config) error {
	if len(c.Resources) < 2 || len(c.Resources) > 32 || len(c.Orders) < 1 || len(c.Orders) > 32 || c.DailyExpense < 1 || c.DailyExpense > 10000 || c.Deadline < 1440 || c.Deadline > 30*1440 || c.GoalOrders < 1 || c.GoalOrders > int64(len(c.Orders)) || c.GoalRevenue < 1 || c.GoalRevenue > 1000000 {
		return fmt.Errorf("经营周期、目标或资源数量无效")
	}
	known := map[string]bool{"cash": true}
	available := map[string]bool{"cash": true}
	for _, r := range c.Resources {
		if !contract.ValidStableKey(r.Key) || r.Name == "" || known[r.Key] || r.Buy < 0 || r.Buy > 10000 || r.Sell < 0 || r.Sell > 10000 || r.Buy > 0 && r.Sell > r.Buy {
			return fmt.Errorf("资源价格无效或存在采购套利: %s", r.Key)
		}
		known[r.Key] = true
		if r.Buy > 0 {
			available[r.Key] = true
		}
	}
	if err := ruledata.ValidateStock(c.InitialStock); err != nil {
		return err
	}
	if c.InitialStock["cash"] < 1 {
		return fmt.Errorf("必须配置初始经营资金")
	}
	for key, n := range c.InitialStock {
		if !known[key] {
			return fmt.Errorf("未知初始资源 %s", key)
		}
		if n > 0 {
			available[key] = true
		}
	}
	if err := production.Validate(c.Facilities, c.Recipes, known); err != nil {
		return err
	}
	levels := map[string]int64{}
	for _, f := range c.Facilities {
		levels[f.Key] = f.MaxLevel
	}
	if len(c.GoalFacilities) == 0 {
		return fmt.Errorf("成功条件必须包含设施")
	}
	for key, level := range c.GoalFacilities {
		if level < 1 || level > levels[key] {
			return fmt.Errorf("经营设施目标无效 %s", key)
		}
	}
	available, buildable := production.DependencyClosure(c.Facilities, c.Recipes, available)
	for key, level := range c.GoalFacilities {
		if buildable[key] < level {
			return fmt.Errorf("目标设施 %s 的生产与建造依赖不可达", key)
		}
	}
	for _, f := range c.Facilities {
		for _, cost := range []map[string]int64{f.BuildCost, f.UpgradeCost} {
			for key := range cost {
				if !available[key] {
					return fmt.Errorf("设施 %s 的材料 %s 无来源", f.Key, key)
				}
			}
		}
	}
	seen := map[string]bool{}
	total := int64(0)
	for _, o := range c.Orders {
		if !contract.ValidStableKey(o.Key) || seen[o.Key] || o.Name == "" || o.Income < 1 || o.Income > 100000 || o.Penalty < 0 || o.Penalty > 100000 || o.Deadline < 60 || o.Deadline >= c.Deadline || len(o.Items) == 0 {
			return fmt.Errorf("订单无效 %s", o.Key)
		}
		seen[o.Key] = true
		total += o.Income
		if err := ruledata.ValidateStock(o.Items); err != nil {
			return err
		}
		for key, n := range o.Items {
			if key == "cash" || !available[key] || n < 1 {
				return fmt.Errorf("订单 %s 的物资不可生产或采购", o.Key)
			}
		}
	}
	if total < c.GoalRevenue {
		return fmt.Errorf("订单总收入不足以达成经营目标")
	}
	return nil
}
func ValidateContent(content contract.Content) error {
	c, err := ruledata.Singleton[Config](content, DefinitionKind)
	if err != nil {
		return err
	}
	if err := validate(c); err != nil {
		return err
	}
	for _, key := range []string{"prosperous", "bankrupt", "timeout", "closed"} {
		found := false
		for _, o := range content.Outcomes {
			found = found || o.Key == key && o.Enabled
		}
		if !found {
			return fmt.Errorf("缺少经营结局 %s", key)
		}
	}
	return nil
}
func Descriptor() contract.ModuleDescriptor {
	return contract.WithCampaignRuntime(contract.ModuleDescriptor{DefinitionSchemas: map[string]map[string]any{DefinitionKind: ruledata.Schema[Config](map[string]string{"initial_stock": "初始库存与资金", "resources": "资源报价", "key": "标识", "name": "名称", "buy": "采购单价（0 表示不出售）", "sell": "变卖单价", "facilities": "设施", "recipes": "生产配方", "orders": "订单", "daily_expense": "每日经营支出", "goal_facilities": "目标设施等级", "goal_orders": "目标订单数", "goal_revenue": "目标订单收入", "deadline": "截止时间（分钟）", "build_cost": "建设成本", "upgrade_cost": "每级升级成本", "max_level": "最高等级", "facility": "所需设施", "level": "所需等级", "inputs": "投入资源", "outputs": "产出资源", "minutes": "生产时长（分钟）", "items": "交付物资", "income": "订单收入", "penalty": "违约金"})}, DefinitionLabels: map[string]string{DefinitionKind: "经营规则"}, Key: EngineKey, Version: 1, Name: "经营建设", Category: "management", Description: "建设设施、安排生产、承接订单与管理现金周转。", Modes: []string{contract.RuntimeModeCampaign}, Actions: []string{"update_state", "domain_action"}, Verifiers: []string{"core.always", "core.conditions"}, Runtime: Module{}, AbandonAction: "management.close", DefinitionValidators: map[string]contract.DefinitionValidator{DefinitionKind: func(d contract.Definition) error {
		c, e := ruledata.Decode[Config](d.Content)
		if e != nil {
			return e
		}
		return validate(c)
	}}, ValidateContent: ValidateContent})
}
