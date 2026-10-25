package management

import (
	"fmt"
	"github.com/ialtone/fatecraft/internal/gameplay/components/production"
	"github.com/ialtone/fatecraft/internal/gameplay/components/ruledata"
	"github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/gameplay/kernel"
	"slices"
	"sort"
)

type State struct {
	Stock      map[string]int64  `json:"stock"`
	Facilities map[string]int64  `json:"facilities"`
	Jobs       []production.Job  `json:"jobs"`
	Orders     map[string]string `json:"orders"`
	Revenue    int64             `json:"revenue"`
	Debt       int64             `json:"debt"`
}
type Module struct{}

func (Module) NarrativeFactPaths() []string {
	return []string{"cash", "debt", "revenue", "delivered", "game_time", "outcome_key"}
}

func (Module) Actions() []contract.RuntimeAction {
	result := []contract.RuntimeAction{}
	for _, entry := range [][4]string{
		{"build", "建设设施", "支付材料建设一座设施", "facility"}, {"upgrade", "升级设施", "支付材料升级设施，增加工位并解锁配方", "facility"},
		{"buy", "采购材料", "按固定报价采购一份原料", "resource"}, {"sell", "变卖库存", "出售一份库存，收入优先偿还欠款", "resource"},
		{"produce", "安排生产", "预付材料并占用设施工位，到期获得产出", "recipe"}, {"accept", "承接订单", "承担交付期限，逾期会形成违约欠款", "order"},
		{"deliver", "交付订单", "交付真实库存并一次性获得订单收入", "order"}, {"advance", "推进经营周期", "按小时推进生产、订单截止和每日支出", "hours"},
		{"finish", "确认经营目标", "核验设施、完成订单与收入目标并结束经营", ""}, {"close", "停止经营", "主动结束本次经营", ""},
	} {
		a := contract.RuntimeAction{Key: "management." + entry[0], Label: entry[1], Description: entry[2], Actors: []string{"player", "model"}, Writes: []string{"stock", "facilities", "jobs", "orders", "revenue", "debt"}, Events: []string{"management.acted", "management.ended"}}
		if entry[3] != "" {
			p := contract.RuntimeParameter{Key: entry[3], Label: entry[1] + "方案", Type: "string", Required: true}
			if entry[3] == "hours" {
				p.Type = "integer"
				p.Minimum = 1
				p.Maximum = 24
			}
			a.Parameters = []contract.RuntimeParameter{p}
		}
		if entry[0] == "finish" || entry[0] == "close" {
			a.Actors = []string{"player"}
			a.Ending = true
		}
		result = append(result, a)
	}
	return result
}
func (Module) Initialize(content contract.Content, _ contract.InitialInput) (contract.RunState, error) {
	c, err := ruledata.Singleton[Config](content, DefinitionKind)
	if err != nil {
		return contract.RunState{}, err
	}
	if err := validate(c); err != nil {
		return contract.RunState{}, err
	}
	s := State{Stock: c.InitialStock, Facilities: map[string]int64{}, Jobs: []production.Job{}, Orders: map[string]string{}}
	for _, o := range c.Orders {
		s.Orders[o.Key] = "available"
	}
	data, err := ruledata.Object(s)
	return contract.RunState{SchemaVersion: 1, Status: contract.RunActive, Data: data}, err
}
func goal(c Config, s State) bool {
	delivered := int64(0)
	for _, status := range s.Orders {
		if status == "fulfilled" {
			delivered++
		}
	}
	if delivered < c.GoalOrders || s.Revenue < c.GoalRevenue || s.Debt > 0 {
		return false
	}
	for key, level := range c.GoalFacilities {
		if s.Facilities[key] < level {
			return false
		}
	}
	return true
}
func (Module) ValidateState(content contract.Content, run contract.RunState) error {
	c, err := ruledata.Singleton[Config](content, DefinitionKind)
	if err != nil {
		return err
	}
	s, err := ruledata.Decode[State](run.Data)
	if err != nil {
		return err
	}
	if run.SchemaVersion != 1 || s.Revenue < 0 || s.Revenue > 10000000 || s.Debt < 0 || s.Debt > 10000000 || len(s.Jobs) > 32 || len(s.Orders) != len(c.Orders) {
		return fmt.Errorf("经营状态头、欠款或队列无效")
	}
	if err := ruledata.ValidateStock(s.Stock); err != nil {
		return err
	}
	known := map[string]bool{"cash": true}
	for _, r := range c.Resources {
		known[r.Key] = true
	}
	for key := range s.Stock {
		if !known[key] {
			return fmt.Errorf("未知库存资源")
		}
	}
	facilities := map[string]production.Facility{}
	for _, f := range c.Facilities {
		facilities[f.Key] = f
	}
	for key, level := range s.Facilities {
		if level < 1 || level > facilities[key].MaxLevel {
			return fmt.Errorf("设施等级无效")
		}
	}
	jobs := map[string]bool{}
	busy := map[string]int64{}
	for _, j := range s.Jobs {
		r, ok := production.Find(c.Recipes, j.Recipe)
		if !ok || j.Key == "" || jobs[j.Key] || r.Facility != j.Facility || s.Facilities[j.Facility] < r.Level || j.Due <= run.GameTime || j.Due-run.GameTime > 7*1440 {
			return fmt.Errorf("生产队列无效")
		}
		jobs[j.Key] = true
		busy[j.Facility]++
	}
	for key, n := range busy {
		if n > s.Facilities[key] {
			return fmt.Errorf("生产工位超额")
		}
	}
	revenue := int64(0)
	for _, o := range c.Orders {
		status := s.Orders[o.Key]
		if !slices.Contains([]string{"available", "accepted", "fulfilled", "breached", "expired"}, status) {
			return fmt.Errorf("订单状态无效")
		}
		if status == "fulfilled" {
			revenue += o.Income
		}
	}
	if revenue != s.Revenue {
		return fmt.Errorf("订单收入证据不一致")
	}
	if run.Status == contract.RunActive && run.GameTime >= c.Deadline {
		return fmt.Errorf("经营超过期限但未结束")
	}
	if run.OutcomeKey == "prosperous" && !goal(c, s) {
		return fmt.Errorf("经营目标事实不足")
	}
	return nil
}
func (m Module) Preview(content contract.Content, run contract.RunState, actor string) ([]contract.ActionPreview, error) {
	c, err := ruledata.Singleton[Config](content, DefinitionKind)
	if err != nil {
		return nil, err
	}
	s, err := ruledata.Decode[State](run.Data)
	if err != nil {
		return nil, err
	}
	labels := map[string]string{"cash": "经营资金"}
	for _, r := range c.Resources {
		labels[r.Key] = r.Name
	}
	result := []contract.ActionPreview{}
	for _, a := range m.Actions() {
		if !slices.Contains(a.Actors, actor) {
			continue
		}
		p := contract.ActionPreview{Action: a, Available: true, CostLabels: labels, Risk: "时间推进会处理订单期限和每日经营支出"}
		add := func(key, label string, minutes int64, costs map[string]int64) {
			p.Plans = append(p.Plans, contract.ActionPlan{Label: label, Arguments: map[string]any{a.Parameters[0].Key: key}, Minutes: minutes, Costs: costs})
		}
		switch a.Key {
		case "management.build", "management.upgrade":
			for _, f := range c.Facilities {
				cost := f.BuildCost
				ok := s.Facilities[f.Key] == 0
				if a.Key == "management.upgrade" {
					cost = f.UpgradeCost
					ok = s.Facilities[f.Key] > 0 && s.Facilities[f.Key] < f.MaxLevel
				}
				if ok && ruledata.CanPay(s.Stock, cost) {
					add(f.Key, f.Name, 30, cost)
				}
			}
			p.Reason = "没有可建设或升级的设施，或材料不足"
		case "management.buy":
			for _, r := range c.Resources {
				if r.Buy > 0 && s.Stock["cash"] >= r.Buy {
					add(r.Key, r.Name, 5, map[string]int64{"cash": r.Buy})
				}
			}
			p.Reason = "当前资金不足或没有可采购的资源"
		case "management.sell":
			for _, r := range c.Resources {
				if r.Sell > 0 && s.Stock[r.Key] > 0 {
					add(r.Key, fmt.Sprintf("%s（收入 %d）", r.Name, r.Sell), 5, map[string]int64{r.Key: 1})
				}
			}
			p.Reason = "当前没有可变卖库存"
		case "management.produce":
			for _, r := range c.Recipes {
				if production.CanStart(s.Stock, s.Facilities, s.Jobs, r) {
					add(r.Key, fmt.Sprintf("%s（生产 %d 分钟）", r.Name, r.Minutes), 5, r.Inputs)
				}
			}
			p.Reason = "设施等级、材料或空闲工位不足"
		case "management.accept":
			for _, o := range c.Orders {
				if s.Orders[o.Key] == "available" && run.GameTime+5 <= o.Deadline {
					add(o.Key, fmt.Sprintf("%s（剩余 %d 分钟）", o.Name, o.Deadline-run.GameTime), 5, nil)
				}
			}
			p.Reason = "没有尚可承接的订单"
		case "management.deliver":
			for _, o := range c.Orders {
				if s.Orders[o.Key] == "accepted" && run.GameTime+5 <= o.Deadline && ruledata.CanPay(s.Stock, o.Items) {
					add(o.Key, fmt.Sprintf("%s（收入 %d）", o.Name, o.Income), 5, o.Items)
				}
			}
			p.Reason = "需要已承接、尚未逾期且货物齐备的订单"
		case "management.advance":
			for h := int64(1); h <= 24; h++ {
				p.Plans = append(p.Plans, contract.ActionPlan{Label: fmt.Sprintf("%d 小时", h), Arguments: map[string]any{"hours": h}, Minutes: h * 60})
			}
		case "management.finish":
			p.Available = goal(c, s)
			p.Reason = "设施、订单数、经营收入尚未达标，或仍有欠款"
			p.Risk = "确认后本局结束"
		case "management.close":
			p.Risk = "结束本次经营，未达成的目标不再继续"
		}
		if len(a.Parameters) > 0 {
			p.Available = len(p.Plans) > 0
			if a.Parameters[0].Type == "string" {
				p.Action.Parameters[0].Options = []string{}
				p.Action.Parameters[0].OptionLabels = map[string]string{}
				for _, plan := range p.Plans {
					key := plan.Arguments[a.Parameters[0].Key].(string)
					p.Action.Parameters[0].Options = append(p.Action.Parameters[0].Options, key)
					p.Action.Parameters[0].OptionLabels[key] = plan.Label
				}
			}
		}
		if p.Available {
			p.Reason = ""
		}
		result = append(result, p)
	}
	return result, nil
}
func charge(s *State, n int64) {
	paid := min(s.Stock["cash"], n)
	s.Stock["cash"] -= paid
	s.Debt += n - paid
}
func income(s *State, n int64) error {
	paid := min(s.Debt, n)
	s.Debt -= paid
	return ruledata.Add(s.Stock, "cash", n-paid)
}
func advance(c Config, s *State, run *contract.RunState, minutes int64) error {
	end := min(run.GameTime+minutes, c.Deadline)
	// Visit only meaningful times; queue/order counts and maximum days are bounded.
	times := map[int64]bool{end: true}
	for _, j := range s.Jobs {
		if j.Due <= end {
			times[j.Due] = true
		}
	}
	for _, o := range c.Orders {
		if o.Deadline >= run.GameTime && o.Deadline < end {
			times[o.Deadline+1] = true
		}
	}
	for t := (run.GameTime/1440 + 1) * 1440; t <= end; t += 1440 {
		times[t] = true
	}
	ordered := []int64{}
	for t := range times {
		ordered = append(ordered, t)
	}
	slices.Sort(ordered)
	for _, t := range ordered {
		var err error
		s.Jobs, _, err = production.Complete(s.Stock, s.Jobs, c.Recipes, t)
		if err != nil {
			return err
		}
		for _, o := range c.Orders {
			if t > o.Deadline {
				if s.Orders[o.Key] == "accepted" {
					s.Orders[o.Key] = "breached"
					charge(s, o.Penalty)
				} else if s.Orders[o.Key] == "available" {
					s.Orders[o.Key] = "expired"
				}
			}
		}
		if t%1440 == 0 && t > run.GameTime {
			charge(s, c.DailyExpense)
		}
		run.GameTime = t
		hasStock := false
		for key, n := range s.Stock {
			hasStock = hasStock || key != "cash" && n > 0
		}
		if s.Debt > 0 && s.Stock["cash"] == 0 && !hasStock && len(s.Jobs) == 0 {
			run.Status = contract.RunFailed
			run.OutcomeKey = "bankrupt"
			return nil
		}
	}
	if end >= c.Deadline {
		run.Status = contract.RunFailed
		run.OutcomeKey = "timeout"
	}
	return nil
}
func (Module) Execute(content contract.Content, run contract.RunState, request contract.RuntimeRequest) (contract.Transition, error) {
	c, err := ruledata.Singleton[Config](content, DefinitionKind)
	if err != nil {
		return contract.Transition{}, err
	}
	s, err := ruledata.Decode[State](run.Data)
	if err != nil {
		return contract.Transition{}, err
	}
	arg := func(key string) string { v, _ := request.Arguments[key].(string); return v }
	minutes := int64(5)
	switch request.ActionKey {
	case "management.build", "management.upgrade":
		for _, f := range c.Facilities {
			if f.Key != arg("facility") {
				continue
			}
			cost := f.BuildCost
			if request.ActionKey == "management.upgrade" {
				cost = f.UpgradeCost
			}
			if err := ruledata.Pay(s.Stock, cost); err != nil {
				return contract.Transition{}, err
			}
			s.Facilities[f.Key]++
			minutes = 30
		}
	case "management.buy", "management.sell":
		for _, r := range c.Resources {
			if r.Key != arg("resource") {
				continue
			}
			if request.ActionKey == "management.buy" {
				err = ruledata.Pay(s.Stock, map[string]int64{"cash": r.Buy})
				if err == nil {
					err = ruledata.Add(s.Stock, r.Key, 1)
				}
			} else {
				err = ruledata.Pay(s.Stock, map[string]int64{r.Key: 1})
				if err == nil {
					err = income(&s, r.Sell)
				}
			}
			if err != nil {
				return contract.Transition{}, err
			}
		}
	case "management.produce":
		r, ok := production.Find(c.Recipes, arg("recipe"))
		if !ok {
			return contract.Transition{}, fmt.Errorf("配方不存在")
		}
		s.Jobs, err = production.Start(s.Stock, s.Facilities, s.Jobs, r, run.GameTime+5, fmt.Sprintf("job.%d", run.Revision))
		if err != nil {
			return contract.Transition{}, err
		}
	case "management.accept":
		s.Orders[arg("order")] = "accepted"
	case "management.deliver":
		for _, o := range c.Orders {
			if o.Key == arg("order") {
				if err := ruledata.Pay(s.Stock, o.Items); err != nil {
					return contract.Transition{}, err
				}
				if err := income(&s, o.Income); err != nil {
					return contract.Transition{}, err
				}
				s.Orders[o.Key] = "fulfilled"
				s.Revenue += o.Income
			}
		}
	case "management.advance":
		hours, _ := kernel.Integer(request.Arguments["hours"])
		minutes = hours * 60
	case "management.finish":
		run.Status = contract.RunCompleted
		run.OutcomeKey = "prosperous"
		minutes = 0
	case "management.close":
		run.Status = contract.RunFailed
		run.OutcomeKey = "closed"
		minutes = 0
	default:
		return contract.Transition{}, contract.Reject("action.unknown", "未知经营动作")
	}
	if minutes > 0 {
		if err := advance(c, &s, &run, minutes); err != nil {
			return contract.Transition{}, err
		}
	}
	run.Data, err = ruledata.Object(s)
	if err != nil {
		return contract.Transition{}, err
	}
	events := []contract.RuntimeEvent{{Kind: "management.acted", Visibility: contract.VisibilityPlayer, Data: map[string]any{"action": request.ActionKey, "minutes": minutes, "debt": s.Debt}}}
	if run.Status != contract.RunActive {
		events = append(events, contract.RuntimeEvent{Kind: "management.ended", Visibility: contract.VisibilityPlayer, Data: map[string]any{"outcome_key": run.OutcomeKey}})
	}
	return contract.Transition{State: run, Events: events, Result: map[string]any{"minutes": minutes, "outcome_key": run.OutcomeKey}}, nil
}
func (Module) Project(content contract.Content, run contract.RunState, _ string) (map[string]any, error) {
	c, err := ruledata.Singleton[Config](content, DefinitionKind)
	if err != nil {
		return nil, err
	}
	s, err := ruledata.Decode[State](run.Data)
	if err != nil {
		return nil, err
	}
	stock := []any{}
	for _, r := range c.Resources {
		if s.Stock[r.Key] > 0 {
			stock = append(stock, map[string]any{"key": r.Key, "name": r.Name, "quantity": s.Stock[r.Key]})
		}
	}
	facilities := []any{}
	for _, f := range c.Facilities {
		facilities = append(facilities, map[string]any{"key": f.Key, "name": f.Name, "level": s.Facilities[f.Key], "goal": c.GoalFacilities[f.Key]})
	}
	jobs := []any{}
	for _, j := range s.Jobs {
		r, _ := production.Find(c.Recipes, j.Recipe)
		jobs = append(jobs, map[string]any{"key": j.Key, "name": r.Name, "remaining_minutes": j.Due - run.GameTime})
	}
	labels := map[string]string{"available": "待承接", "accepted": "待交付", "fulfilled": "已完成", "breached": "已违约", "expired": "已过期"}
	orders := []any{}
	delivered := 0
	for _, o := range c.Orders {
		if s.Orders[o.Key] == "fulfilled" {
			delivered++
		}
		items := []string{}
		for key, n := range o.Items {
			for _, r := range c.Resources {
				if r.Key == key {
					items = append(items, fmt.Sprintf("%s × %d", r.Name, n))
				}
			}
		}
		sort.Strings(items)
		orders = append(orders, map[string]any{"key": o.Key, "name": o.Name, "status": labels[s.Orders[o.Key]], "items": items, "income": o.Income, "penalty": o.Penalty, "remaining_minutes": max(0, o.Deadline-run.GameTime)})
	}
	return map[string]any{"cash": s.Stock["cash"], "debt": s.Debt, "revenue": s.Revenue, "goal_revenue": c.GoalRevenue, "delivered": delivered, "goal_orders": c.GoalOrders, "daily_expense": c.DailyExpense, "inventory": stock, "facilities": facilities, "jobs": jobs, "orders": orders, "game_time": run.GameTime, "deadline": c.Deadline, "goal": "升级目标设施，履约经营订单并偿清欠款", "outcome_key": run.OutcomeKey, "facility_level_sum": facilityLevelSum(c, s)}, nil
}

func facilityLevelSum(c Config, s State) int64 {
	var total int64
	for _, facility := range c.Facilities {
		total += s.Facilities[facility.Key]
	}
	return total
}
