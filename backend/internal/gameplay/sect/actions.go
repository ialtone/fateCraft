package sect

import (
	"fmt"
	"github.com/ialtone/fatecraft/internal/gameplay/components/personnel"
	"github.com/ialtone/fatecraft/internal/gameplay/components/production"
	"github.com/ialtone/fatecraft/internal/gameplay/components/ruledata"
	"github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/gameplay/kernel"
	"slices"
	"strings"
)

func (Module) Execute(content contract.Content, run contract.RunState, request contract.RuntimeRequest) (contract.Transition, error) {
	c, err := ruledata.Singleton[Config](content, DefinitionKind)
	if err != nil {
		return contract.Transition{}, err
	}
	s, err := ruledata.Decode[State](run.Data)
	if err != nil {
		return contract.Transition{}, err
	}
	arg := func(key string) string { value, _ := request.Arguments[key].(string); return value }
	events := []contract.RuntimeEvent{}
	started := run.GameTime
	switch request.ActionKey {
	case "sect.recruit":
		d := disciple(c, arg("disciple"))
		err = ruledata.Pay(s.Stock, d.RecruitCost)
		s.Disciples[d.Key] = Member{Level: 1, Loyalty: 80}
		s.Recruited = append(s.Recruited, d.Key)
	case "sect.build", "sect.upgrade":
		f := facility(c, arg("facility"))
		cost := f.BuildCost
		if request.ActionKey == "sect.upgrade" {
			cost = f.UpgradeCost
		}
		err = ruledata.Pay(s.Stock, cost)
		s.Facilities[f.Key]++
	case "sect.train":
		key := arg("disciple")
		err = ruledata.Pay(s.Stock, c.TrainCost)
		if err == nil {
			err = personnel.Assign(s.Assignments, key, personnel.Assignment{Kind: "train", Due: run.GameTime + 120})
		}
	case "sect.teach":
		key := arg("disciple")
		err = ruledata.Pay(s.Stock, c.TeachCost)
		member := s.Disciples[key]
		member.Learned = true
		s.Disciples[key] = member
	case "sect.guard":
		err = personnel.Assign(s.Assignments, arg("disciple"), personnel.Assignment{Kind: "guard"})
	case "sect.recall":
		delete(s.Assignments, arg("disciple"))
	case "sect.produce":
		key, recipeKey, _ := strings.Cut(arg("plan"), "/")
		recipe, _ := production.Find(c.Recipes, recipeKey)
		if disciple(c, key).Specialty == "craft" {
			recipe.Minutes = max(1, recipe.Minutes*3/4)
		}
		jobKey := fmt.Sprintf("job.%d.%s", run.Revision, key)
		s.Jobs, err = production.Start(s.Stock, s.Facilities, s.Jobs, recipe, run.GameTime, jobKey)
		if err == nil {
			err = personnel.Assign(s.Assignments, key, personnel.Assignment{Kind: "production", Target: jobKey, Due: run.GameTime + recipe.Minutes})
		}
	case "sect.dispatch":
		key, missionKey, _ := strings.Cut(arg("plan"), "/")
		m := mission(c, missionKey)
		err = ruledata.Pay(s.Stock, m.Cost)
		if err == nil {
			err = personnel.Assign(s.Assignments, key, personnel.Assignment{Kind: "mission", Target: m.Key, Due: run.GameTime + m.Minutes})
		}
	case "sect.advance":
		hours, _ := kernel.Integer(request.Arguments["hours"])
		events, err = advance(c, &s, &run, hours*60, request.Random)
	case "sect.finish":
		run.Status = contract.RunCompleted
		run.OutcomeKey = "rebuilt"
	case "sect.dissolve":
		run.Status = contract.RunFailed
		run.OutcomeKey = "dissolved"
	default:
		return contract.Transition{}, contract.Reject("action.unknown", "未知宗门动作")
	}
	if err != nil {
		return contract.Transition{}, err
	}
	run.Data, err = ruledata.Object(s)
	events = append(events, contract.RuntimeEvent{Kind: "sect.acted", Visibility: contract.VisibilityPlayer, Data: map[string]any{"action": request.ActionKey, "minutes": run.GameTime - started}})
	if run.Status != contract.RunActive {
		events = append(events, contract.RuntimeEvent{Kind: "sect.ended", Visibility: contract.VisibilityPlayer, Data: map[string]any{"outcome_key": run.OutcomeKey}})
	}
	return contract.Transition{State: run, Events: events, Result: map[string]any{"minutes": run.GameTime - started, "outcome_key": run.OutcomeKey}}, err
}
func advance(c Config, s *State, run *contract.RunState, minutes int64, random contract.RandomSource) ([]contract.RuntimeEvent, error) {
	end := min(c.Deadline, run.GameTime+minutes)
	events := []contract.RuntimeEvent{}
	for run.GameTime < end && run.Status == contract.RunActive {
		next := min(end, (run.GameTime/1440+1)*1440)
		if s.Crisis == "pending" {
			next = min(next, c.CrisisTime)
		}
		for _, a := range s.Assignments {
			if a.Due > run.GameTime {
				next = min(next, a.Due)
			}
		}
		if next <= run.GameTime {
			return nil, fmt.Errorf("宗门时间事件未能前进")
		}
		run.GameTime = next
		pending, completed, err := production.Complete(s.Stock, s.Jobs, c.Recipes, next)
		if err != nil {
			return nil, err
		}
		s.Jobs = pending
		for _, d := range c.Disciples {
			a, exists := s.Assignments[d.Key]
			if !exists || a.Kind == "guard" || a.Due > next {
				continue
			}
			member := s.Disciples[d.Key]
			detail := map[string]any{"disciple": d.Name, "task": a.Kind}
			switch a.Kind {
			case "production":
				if !slices.Contains(completed, a.Target) {
					return nil, fmt.Errorf("生产岗位与完成事件不一致")
				}
				detail["success"] = true
			case "train":
				member.Progress += d.Aptitude * 10
				for member.Progress >= 30 && member.Level < 3 {
					member.Progress -= 30
					member.Level++
				}
				if member.Level == 3 {
					member.Progress = 0
				}
				detail["level"] = member.Level
			case "mission":
				m := mission(c, a.Target)
				draw, err := random.Intn("sect.mission."+d.Key, 100)
				if err != nil {
					return nil, err
				}
				success := draw < chance(c, *s, d.Key, m)
				detail["success"] = success
				detail["mission"] = m.Name
				if success {
					for key, n := range m.Reward {
						if err := ruledata.Add(s.Stock, key, n); err != nil {
							return nil, err
						}
					}
					s.Missions[m.Key]++
				} else {
					s.Failures++
					member.InjuredUntil = next + 120
					member.Loyalty = max(1, member.Loyalty-5)
				}
			default:
				return nil, fmt.Errorf("未知宗门任务")
			}
			s.Disciples[d.Key] = member
			delete(s.Assignments, d.Key)
			events = append(events, contract.RuntimeEvent{Kind: "sect.task_finished", Visibility: contract.VisibilityPlayer, Data: detail})
		}
		// Tasks due exactly at a day boundary finish first. Their outputs can pay
		// upkeep; departed members cannot contribute to the subsequent crisis.
		if next%1440 == 0 {
			cost := int64(len(s.Disciples)) * c.Upkeep
			paid := s.Stock[c.UpkeepResource] >= cost
			if paid {
				s.Stock[c.UpkeepResource] -= cost
			} else {
				s.Stock[c.UpkeepResource] = 0
			}
			s.UpkeepDays++
			for _, d := range c.Disciples {
				member, exists := s.Disciples[d.Key]
				if !exists {
					continue
				}
				if paid {
					member.Loyalty = min(100, member.Loyalty+5)
				} else {
					member.Loyalty -= 20
				}
				if member.Loyalty <= 0 {
					s.Departed = append(s.Departed, d.Key)
					delete(s.Disciples, d.Key)
					a := s.Assignments[d.Key]
					s.Jobs = slices.DeleteFunc(s.Jobs, func(j production.Job) bool { return j.Key == a.Target })
					delete(s.Assignments, d.Key)
				} else {
					s.Disciples[d.Key] = member
				}
			}
			events = append(events, contract.RuntimeEvent{Kind: "sect.upkeep", Visibility: contract.VisibilityPlayer, Data: map[string]any{"paid": paid, "required": cost, "remaining_disciples": len(s.Disciples)}})
			if len(s.Disciples) == 0 {
				run.Status = contract.RunFailed
				run.OutcomeKey = "dispersed"
			}
		}
		if run.Status == contract.RunActive && next == c.CrisisTime && s.Crisis == "pending" {
			strength := defense(c, *s)
			s.Crisis = "defended"
			if s.Facilities[c.Hall] == 0 || strength < c.Threat {
				s.Crisis = "fallen"
				run.Status = contract.RunFailed
				run.OutcomeKey = "fallen"
			}
			events = append(events, contract.RuntimeEvent{Kind: "sect.crisis", Visibility: contract.VisibilityPlayer, Data: map[string]any{"defense": strength, "required": c.Threat, "result": s.Crisis}})
		}
		if run.Status == contract.RunActive && next == c.Deadline {
			run.Status = contract.RunFailed
			run.OutcomeKey = "timeout"
		}
	}
	return events, nil
}
