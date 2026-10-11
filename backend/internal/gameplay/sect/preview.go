package sect

import (
	"fmt"
	"github.com/ialtone/fatecraft/internal/gameplay/components/production"
	"github.com/ialtone/fatecraft/internal/gameplay/components/ruledata"
	"github.com/ialtone/fatecraft/internal/gameplay/contract"
	"slices"
)

func (m Module) Preview(content contract.Content, run contract.RunState, actor string) ([]contract.ActionPreview, error) {
	c, err := ruledata.Singleton[Config](content, DefinitionKind)
	if err != nil {
		return nil, err
	}
	s, err := ruledata.Decode[State](run.Data)
	if err != nil {
		return nil, err
	}
	result := []contract.ActionPreview{}
	for _, a := range m.Actions() {
		if !slices.Contains(a.Actors, actor) {
			continue
		}
		p := contract.ActionPreview{Action: a, Available: true, CostLabels: c.Resources, Risk: "安排工作后需推进宗门时间；弟子不能同时承担冲突岗位"}
		add := func(key, label string, cost map[string]int64, risk string) {
			p.Plans = append(p.Plans, contract.ActionPlan{Label: label, Arguments: map[string]any{a.Parameters[0].Key: key}, Costs: cost, Risk: risk})
		}
		switch a.Key {
		case "sect.recruit":
			for _, d := range c.Disciples {
				if !slices.Contains(s.Recruited, d.Key) && ruledata.CanPay(s.Stock, d.RecruitCost) {
					add(d.Key, d.Name, d.RecruitCost, "收徒后每日增加供养；同一候选人只能招收一次")
				}
			}
			p.Reason = "没有尚未招收且能支付成本的候选弟子"
		case "sect.build", "sect.upgrade":
			for _, f := range c.Facilities {
				level := s.Facilities[f.Key]
				cost := f.BuildCost
				ok := level == 0
				if a.Key == "sect.upgrade" {
					cost = f.UpgradeCost
					ok = level > 0 && level < f.MaxLevel
				}
				if ok && ruledata.CanPay(s.Stock, cost) {
					add(f.Key, f.Name, cost, "建设立即扣除库藏；核心大殿每级提供 2 点守宗防御")
				}
			}
			p.Reason = "设施状态不符合或库藏不足"
		case "sect.train", "sect.teach", "sect.guard", "sect.recall":
			for _, d := range c.Disciples {
				member, exists := s.Disciples[d.Key]
				if !exists {
					continue
				}
				idle := available(s, d.Key, run.GameTime)
				switch a.Key {
				case "sect.train":
					if idle && member.Level < 3 && s.Facilities[c.Hall] > 0 && ruledata.CanPay(s.Stock, c.TrainCost) {
						add(d.Key, d.Name, c.TrainCost, fmt.Sprintf("修行需 120 分钟，获得 %d 成长；每 30 成长升一级，最高 3 级", d.Aptitude*10))
					}
				case "sect.teach":
					if idle && !member.Learned && s.Facilities[c.Hall] > 0 && ruledata.CanPay(s.Stock, c.TeachCost) {
						add(d.Key, d.Name, c.TeachCost, "传承只能传授一次；弟子达到 2 级后形成继承人证据")
					}
				case "sect.guard":
					if idle {
						add(d.Key, d.Name, nil, "驻守期间不能生产、修行或外出；危机到来时参与防御")
					}
				case "sect.recall":
					if s.Assignments[d.Key].Kind == "guard" {
						add(d.Key, d.Name, nil, "解除驻守将立即降低危机防御")
					}
				}
			}
			p.Reason = "需要合适的空闲弟子、设施与物资；驻守只能通过解除驻守撤销"
		case "sect.produce":
			for _, d := range c.Disciples {
				if !available(s, d.Key, run.GameTime) {
					continue
				}
				for _, r := range c.Recipes {
					if production.CanStart(s.Stock, s.Facilities, s.Jobs, r) {
						minutes := r.Minutes
						if d.Specialty == "craft" {
							minutes = max(1, minutes*3/4)
						}
						add(d.Key+"/"+r.Key, d.Name+" · "+r.Name, r.Inputs, fmt.Sprintf("生产需 %d 分钟，期间占用弟子与设施工位", minutes))
					}
				}
			}
			p.Reason = "缺少空闲弟子、设施工位或生产材料"
		case "sect.dispatch":
			for _, d := range c.Disciples {
				if !available(s, d.Key, run.GameTime) {
					continue
				}
				for _, mission := range c.Missions {
					if ruledata.CanPay(s.Stock, mission.Cost) {
						add(d.Key+"/"+mission.Key, d.Name+" · "+mission.Name, mission.Cost, fmt.Sprintf("外出 %d 分钟，成功率 %d%%；失败将受伤 120 分钟并损失 5 忠诚，出发物资不退", mission.Minutes, chance(c, s, d.Key, mission)))
					}
				}
			}
			p.Reason = "缺少可派遣弟子或出发物资"
		case "sect.advance":
			for hours := int64(1); hours <= 24; hours++ {
				p.Plans = append(p.Plans, contract.ActionPlan{Label: fmt.Sprintf("推进 %d 小时", hours), Arguments: map[string]any{"hours": hours}, Minutes: hours * 60, Risk: "按实际时点处理任务、供养与危机；不会产生离线收益"})
			}
		case "sect.finish":
			p.Available = ready(c, s)
			p.Reason = "需达到设施与人数目标，培养继承人、完成至少一次派遣并守住危机"
			p.Risk = "确认完成重建，结束本次宗门经营"
		case "sect.dissolve":
			p.Risk = "解散宗门并结束本次经营，未完成任务不继续结算"
		}
		if len(a.Parameters) > 0 {
			p.Available = len(p.Plans) > 0
			if a.Parameters[0].Type == "string" {
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
