package sect

import (
	"fmt"
	"github.com/ialtone/fatecraft/internal/gameplay/components/ruledata"
	"github.com/ialtone/fatecraft/internal/gameplay/contract"
	"slices"
)

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
	keys := []string{}
	for key := range c.Resources {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		stock = append(stock, map[string]any{"name": c.Resources[key], "quantity": s.Stock[key]})
	}
	facilities := []any{}
	for _, f := range c.Facilities {
		facilities = append(facilities, map[string]any{"name": f.Name, "level": s.Facilities[f.Key], "goal": c.GoalFacilities[f.Key]})
	}
	members := []any{}
	tasks := []any{}
	specialty := map[string]string{"craft": "生产", "guard": "驻守", "study": "修行"}
	taskNames := map[string]string{"production": "生产", "guard": "驻守", "train": "修行", "mission": "外出派遣"}
	for _, d := range c.Disciples {
		member, exists := s.Disciples[d.Key]
		if !exists {
			continue
		}
		status := "空闲"
		if member.InjuredUntil > run.GameTime {
			status = fmt.Sprintf("养伤中，剩余 %d 分钟", member.InjuredUntil-run.GameTime)
		}
		if a, assigned := s.Assignments[d.Key]; assigned {
			status = taskNames[a.Kind]
			target := status
			if a.Kind == "mission" {
				target = mission(c, a.Target).Name
			}
			if a.Kind == "production" {
				for _, job := range s.Jobs {
					if job.Key == a.Target {
						for _, r := range c.Recipes {
							if r.Key == job.Recipe {
								target = r.Name
							}
						}
					}
				}
			}
			tasks = append(tasks, map[string]any{"name": d.Name, "task": target, "remaining_minutes": max(0, a.Due-run.GameTime)})
		}
		inherited := "未学传承"
		if member.Learned {
			inherited = "已学传承"
		}
		members = append(members, map[string]any{"name": d.Name, "specialty": specialty[d.Specialty], "level": member.Level, "progress": member.Progress, "loyalty": member.Loyalty, "inheritance": inherited, "status": status})
	}
	successful := int64(0)
	for _, n := range s.Missions {
		successful += n
	}
	crisis := map[string]string{"pending": "尚未到来", "defended": "已守住", "fallen": "核心失守"}
	facilityLevels := int64(0)
	for _, f := range c.Facilities {
		facilityLevels += s.Facilities[f.Key]
	}
	return map[string]any{"goal": "达成设施与人数目标，培养至少一名二级传承弟子，完成派遣并守住宗门危机", "stock": stock, "facilities": facilities, "disciples": members, "tasks": tasks, "disciple_count": len(s.Disciples), "goal_disciples": c.GoalDisciples, "upkeep": int64(len(s.Disciples)) * c.Upkeep, "upkeep_resource": c.Resources[c.UpkeepResource], "defense": defense(c, s), "threat": c.Threat, "crisis": crisis[s.Crisis], "crisis_minutes": max(0, c.CrisisTime-run.GameTime), "successful_missions": successful, "failed_missions": s.Failures, "facility_level_sum": facilityLevels, "remaining_minutes": max(0, c.Deadline-run.GameTime), "outcome_key": run.OutcomeKey}, nil
}
