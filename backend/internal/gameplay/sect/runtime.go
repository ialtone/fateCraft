package sect

import (
	"fmt"
	"github.com/ialtone/fatecraft/internal/gameplay/components/personnel"
	"github.com/ialtone/fatecraft/internal/gameplay/components/production"
	"github.com/ialtone/fatecraft/internal/gameplay/components/ruledata"
	"github.com/ialtone/fatecraft/internal/gameplay/contract"
	"slices"
)

type Member struct {
	Level        int64 `json:"level"`
	Progress     int64 `json:"progress"`
	Learned      bool  `json:"learned"`
	Loyalty      int64 `json:"loyalty"`
	InjuredUntil int64 `json:"injured_until"`
}
type State struct {
	Stock       map[string]int64                `json:"stock"`
	Facilities  map[string]int64                `json:"facilities"`
	Disciples   map[string]Member               `json:"disciples"`
	Recruited   []string                        `json:"recruited"`
	Departed    []string                        `json:"departed"`
	Assignments map[string]personnel.Assignment `json:"assignments"`
	Jobs        []production.Job                `json:"jobs"`
	Missions    map[string]int64                `json:"missions"`
	Failures    int64                           `json:"failures"`
	Crisis      string                          `json:"crisis"`
	UpkeepDays  int64                           `json:"upkeep_days"`
}
type Module struct{}

func (Module) NarrativeFactPaths() []string {
	return []string{"disciple_count", "defense", "crisis", "successful_missions", "remaining_minutes", "outcome_key"}
}

func (Module) Actions() []contract.RuntimeAction {
	result := []contract.RuntimeAction{}
	for _, a := range [][3]string{{"recruit", "招收弟子", "disciple"}, {"build", "建设设施", "facility"}, {"upgrade", "升级设施", "facility"}, {"train", "安排修行", "disciple"}, {"teach", "传授宗门传承", "disciple"}, {"produce", "分配生产岗位", "plan"}, {"dispatch", "派遣外出", "plan"}, {"guard", "安排驻守", "disciple"}, {"recall", "解除驻守", "disciple"}, {"advance", "推进宗门时间", "hours"}, {"finish", "确认宗门重建", ""}, {"dissolve", "解散宗门", ""}} {
		action := contract.RuntimeAction{Key: "sect." + a[0], Label: a[1], Description: a[1] + "；人员、物资与任务进展均以宗门记录为准", Actors: []string{"player", "model"}, Writes: []string{"stock", "facilities", "disciples", "recruited", "departed", "assignments", "jobs", "missions", "failures", "crisis", "upkeep_days"}, Events: []string{"sect.acted", "sect.task_finished", "sect.upkeep", "sect.crisis", "sect.ended"}}
		if a[2] != "" {
			action.Parameters = []contract.RuntimeParameter{{Key: a[2], Label: a[1] + "对象", Type: "string", Required: true}}
			if a[2] == "hours" {
				action.Parameters[0] = contract.RuntimeParameter{Key: "hours", Label: "推进小时", Type: "integer", Required: true, Minimum: 1, Maximum: 24}
			}
		}
		if a[0] == "finish" || a[0] == "dissolve" {
			action.Actors = []string{"player"}
			action.Ending = true
		}
		result = append(result, action)
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
	s := State{Stock: map[string]int64{}, Facilities: map[string]int64{}, Disciples: map[string]Member{}, Recruited: []string{}, Departed: []string{}, Assignments: map[string]personnel.Assignment{}, Jobs: []production.Job{}, Missions: map[string]int64{}, Crisis: "pending"}
	for key, n := range c.InitialStock {
		s.Stock[key] = n
	}
	for _, d := range c.Disciples {
		if d.Initial {
			s.Disciples[d.Key] = Member{Level: 1, Loyalty: 80}
			s.Recruited = append(s.Recruited, d.Key)
		}
	}
	data, err := ruledata.Object(s)
	return contract.RunState{SchemaVersion: 1, Status: contract.RunActive, Data: data}, err
}
func available(s State, key string, now int64) bool {
	m, exists := s.Disciples[key]
	return exists && m.InjuredUntil <= now && personnel.Available(s.Assignments, key)
}
func defense(c Config, s State) int64 {
	total := s.Facilities[c.Hall] * 2
	for key, a := range s.Assignments {
		if a.Kind == "guard" {
			d := disciple(c, key)
			total += d.Power * s.Disciples[key].Level
			if d.Specialty == "guard" {
				total += 2
			}
		}
	}
	return total
}
func chance(c Config, s State, key string, m Mission) int64 {
	d := disciple(c, key)
	return max(20, min(95, 70+d.Power*s.Disciples[key].Level*5-m.Difficulty))
}
func ready(c Config, s State) bool {
	if s.Crisis != "defended" || int64(len(s.Disciples)) < c.GoalDisciples {
		return false
	}
	for key, n := range c.GoalFacilities {
		if s.Facilities[key] < n {
			return false
		}
	}
	inherited := false
	for _, d := range s.Disciples {
		inherited = inherited || d.Learned && d.Level >= 2
	}
	success := false
	for _, n := range s.Missions {
		success = success || n > 0
	}
	return inherited && success
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
	if run.SchemaVersion != 1 || run.GameTime > c.Deadline || len(s.Disciples) > len(c.Disciples) || len(s.Assignments) > len(s.Disciples) || len(s.Jobs) > len(s.Disciples) || s.UpkeepDays != run.GameTime/1440 || s.Failures < 0 || s.Failures > 1000 || !slices.Contains([]string{"pending", "defended", "fallen"}, s.Crisis) {
		return fmt.Errorf("宗门状态格式无效")
	}
	if err := ruledata.ValidateStock(s.Stock); err != nil {
		return err
	}
	for key := range s.Stock {
		if c.Resources[key] == "" {
			return fmt.Errorf("未知库藏资源")
		}
	}
	for key, n := range s.Facilities {
		f := facility(c, key)
		if f.Key == "" || n < 1 || n > f.MaxLevel {
			return fmt.Errorf("设施等级无效")
		}
	}
	recruited, departed := map[string]bool{}, map[string]bool{}
	for _, key := range s.Recruited {
		if disciple(c, key).Key == "" || recruited[key] {
			return fmt.Errorf("收徒身份重复或未知")
		}
		recruited[key] = true
	}
	for _, key := range s.Departed {
		if !recruited[key] || departed[key] {
			return fmt.Errorf("离宗记录无效")
		}
		departed[key] = true
	}
	for _, d := range c.Disciples {
		if d.Initial && !recruited[d.Key] {
			return fmt.Errorf("初始弟子身份丢失")
		}
	}
	if len(s.Disciples)+len(departed) != len(recruited) {
		return fmt.Errorf("成员身份记录不守恒")
	}
	for key, m := range s.Disciples {
		if !recruited[key] || departed[key] || m.Level < 1 || m.Level > 3 || m.Progress < 0 || m.Progress >= 30 || m.Loyalty < 1 || m.Loyalty > 100 || m.InjuredUntil < 0 || m.InjuredUntil > c.Deadline+120 {
			return fmt.Errorf("弟子成长、忠诚或伤病无效")
		}
	}
	jobs := map[string]production.Job{}
	for _, job := range s.Jobs {
		recipe, exists := production.Find(c.Recipes, job.Recipe)
		if !exists || jobs[job.Key].Key != "" || job.Facility != recipe.Facility || job.Due <= run.GameTime || s.Facilities[job.Facility] < recipe.Level {
			return fmt.Errorf("宗门生产队列无效")
		}
		jobs[job.Key] = job
	}
	used := map[string]bool{}
	for key, a := range s.Assignments {
		m, exists := s.Disciples[key]
		if !exists || m.InjuredUntil > run.GameTime || a.Due < 0 || a.Due > c.Deadline+1440 {
			return fmt.Errorf("弟子任务状态无效")
		}
		switch a.Kind {
		case "guard":
			if a.Due != 0 || a.Target != "" {
				return fmt.Errorf("驻守状态无效")
			}
		case "train":
			if a.Due <= run.GameTime || a.Target != "" || m.Level >= 3 {
				return fmt.Errorf("修行队列无效")
			}
		case "production":
			job := jobs[a.Target]
			if job.Key == "" || used[job.Key] || job.Due != a.Due {
				return fmt.Errorf("生产岗位未与任务一一绑定")
			}
			used[job.Key] = true
		case "mission":
			if mission(c, a.Target).Key == "" || a.Due <= run.GameTime {
				return fmt.Errorf("派遣队列无效")
			}
		default:
			return fmt.Errorf("未知弟子岗位")
		}
	}
	if len(used) != len(jobs) {
		return fmt.Errorf("存在无人负责的生产任务")
	}
	for key, n := range s.Missions {
		if mission(c, key).Key == "" || n < 1 || n > 1000 {
			return fmt.Errorf("派遣成功记录无效")
		}
	}
	if run.Status == contract.RunActive && (run.GameTime >= c.Deadline || len(s.Disciples) == 0 || s.Crisis == "fallen" || run.GameTime >= c.CrisisTime && s.Crisis == "pending") {
		return fmt.Errorf("宗门终止或危机未处理")
	}
	if run.Status == contract.RunCompleted && (run.OutcomeKey != "rebuilt" || !ready(c, s)) {
		return fmt.Errorf("缺少建设、传承、人员或守宗证据")
	}
	if run.Status == contract.RunFailed {
		switch run.OutcomeKey {
		case "fallen":
			if s.Crisis != "fallen" {
				return fmt.Errorf("核心设施未失守")
			}
		case "dispersed":
			if len(s.Disciples) != 0 {
				return fmt.Errorf("仍有弟子在宗")
			}
		case "timeout":
			if run.GameTime != c.Deadline {
				return fmt.Errorf("期限未到")
			}
		case "dissolved":
		default:
			return fmt.Errorf("宗门失败结局无效")
		}
	}
	return nil
}
