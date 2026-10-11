package stealth

import (
	"fmt"
	"github.com/ialtone/fatecraft/internal/gameplay/components/ruledata"
	"github.com/ialtone/fatecraft/internal/gameplay/contract"
	"slices"
)

const EngineKey = "stealth.escape"
const DefinitionKind = "stealth.operation"

type Location struct {
	Key     string   `json:"key"`
	Name    string   `json:"name"`
	Links   []string `json:"links"`
	Risk    int64    `json:"risk"`
	Locked  bool     `json:"locked"`
	Hiding  bool     `json:"hiding"`
	Intel   bool     `json:"intel"`
	Minutes int64    `json:"minutes"`
}
type Config struct {
	Locations        []Location `json:"locations"`
	Start            string     `json:"start"`
	Target           string     `json:"target"`
	Exits            []string   `json:"exits"`
	Patrol           []string   `json:"patrol"`
	PatrolMinutes    int64      `json:"patrol_minutes"`
	PatrolRisk       int64      `json:"patrol_risk"`
	PursuitThreshold int64      `json:"pursuit_threshold"`
	Lockpicks        int64      `json:"lockpicks"`
	Decoys           int64      `json:"decoys"`
	Deadline         int64      `json:"deadline"`
}

func find(c Config, key string) Location {
	for _, l := range c.Locations {
		if l.Key == key {
			return l
		}
	}
	return Location{}
}
func validate(c Config) error {
	if len(c.Locations) < 4 || len(c.Locations) > 32 || len(c.Exits) < 1 || len(c.Exits) > 4 || len(c.Patrol) < 2 || len(c.Patrol) > 32 || c.PatrolMinutes < 1 || c.PatrolMinutes > 30 || c.PatrolRisk < 0 || c.PatrolRisk > 60 || c.PursuitThreshold < 30 || c.PursuitThreshold > 80 || c.Lockpicks < 0 || c.Lockpicks > 10 || c.Decoys < 0 || c.Decoys > 10 || c.Deadline < 60 || c.Deadline > 1440 {
		return fmt.Errorf("潜入地点、工具、巡逻或期限参数无效")
	}
	seen := map[string]bool{}
	hiding := false
	for _, l := range c.Locations {
		if !contract.ValidStableKey(l.Key) || seen[l.Key] || l.Name == "" || l.Risk < 0 || l.Risk > 80 || l.Minutes < 1 || l.Minutes > 60 || len(l.Links) == 0 || len(l.Links) > 8 {
			return fmt.Errorf("潜入地点无效 %s", l.Key)
		}
		seen[l.Key] = true
		hiding = hiding || l.Hiding
	}
	if !seen[c.Start] || !seen[c.Target] || find(c, c.Start).Locked || !hiding {
		return fmt.Errorf("初始地点、目标或脱身地点不可用")
	}
	for _, l := range c.Locations {
		links := map[string]bool{}
		for _, key := range l.Links {
			if key == l.Key || links[key] || !seen[key] || !slices.Contains(find(c, key).Links, l.Key) {
				return fmt.Errorf("地点连接须唯一且可往返: %s", l.Key)
			}
			links[key] = true
		}
	}
	for _, key := range append(slices.Clone(c.Exits), c.Patrol...) {
		if !seen[key] {
			return fmt.Errorf("撤离或巡逻地点不存在 %s", key)
		}
	}
	return validateGateRoute(c)
}
func ValidateContent(content contract.Content) error {
	c, err := ruledata.Singleton[Config](content, DefinitionKind)
	if err != nil {
		return err
	}
	if err := validate(c); err != nil {
		return err
	}
	for _, key := range []string{"escaped", "withdrawn", "captured", "timeout", "abandoned"} {
		found := false
		for _, o := range content.Outcomes {
			found = found || o.Key == key && o.Enabled
		}
		if !found {
			return fmt.Errorf("缺少潜入结局 %s", key)
		}
	}
	return nil
}
func Descriptor() contract.ModuleDescriptor {
	schema := ruledata.Schema[Config](map[string]string{"locations": "地点", "key": "标识", "name": "名称", "links": "相邻地点", "risk": "基础发现概率（%）", "locked": "有门禁", "hiding": "可以藏身", "intel": "可获取门禁情报", "minutes": "潜行进入耗时（分钟）", "start": "初始地点", "target": "目标地点", "exits": "撤离地点", "patrol": "巡逻路线（不向玩家公开）", "patrol_minutes": "巡逻移动间隔（分钟）", "patrol_risk": "巡逻发现概率加成", "pursuit_threshold": "进入追捕的警戒阈值", "lockpicks": "初始开锁器", "decoys": "初始干扰器", "deadline": "行动期限（分钟）"})
	return contract.ModuleDescriptor{Key: EngineKey, Version: 1, Name: "潜入逃脱", Category: "stealth", Description: "观察巡逻、选择路线、解除门禁并携带目标安全撤离。", Modes: []string{contract.RuntimeModeChallenge}, Actions: []string{"update_state", "domain_action", "complete_challenge"}, Verifiers: []string{"core.conditions"}, SettlementHandlers: []string{"core.challenge_completion"}, Runtime: Module{}, AbandonAction: "stealth.abandon", DefinitionLabels: map[string]string{DefinitionKind: "潜入行动"}, DefinitionSchemas: map[string]map[string]any{DefinitionKind: schema}, DefinitionValidators: map[string]contract.DefinitionValidator{DefinitionKind: func(d contract.Definition) error {
		c, err := ruledata.Decode[Config](d.Content)
		if err != nil {
			return err
		}
		return validate(c)
	}}, ValidateContent: ValidateContent}
}
