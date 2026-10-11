package trial

import (
	"fmt"
	"github.com/ialtone/fatecraft/internal/gameplay/components/ruledata"
	"github.com/ialtone/fatecraft/internal/gameplay/contract"
	"github.com/ialtone/fatecraft/internal/gameplay/kernel"
	"math"
	"slices"
)

func (m Module) Execute(content contract.Content, run contract.RunState, request contract.RuntimeRequest) (contract.Transition, error) {
	if err := m.ValidateState(content, run); err != nil {
		return contract.Transition{}, err
	}
	if run.Status != contract.RunActive || request.Random == nil {
		return contract.Transition{}, contract.Reject("trial.inactive", "本世已结束或缺少可信随机源")
	}
	var declared *contract.RuntimeAction
	for _, a := range m.Actions() {
		if a.Key == request.ActionKey && slices.Contains(a.Actors, request.Actor) {
			declared = &a
		}
	}
	if declared == nil {
		return contract.Transition{}, contract.Reject("trial.actor", "此行动必须由有权限的主体执行")
	}
	if err := kernel.ValidateArguments(*declared, request.Arguments); err != nil {
		return contract.Transition{}, err
	}
	c, s, err := read(content, run)
	if err != nil {
		return contract.Transition{}, err
	}
	choices, reason := options(c, s, run, request.ActionKey)
	var selected *option
	arg, _ := request.Arguments["key"].(string)
	for i, o := range choices {
		k, _ := o.plan.Arguments["key"].(string)
		if k == arg {
			selected = &choices[i]
			break
		}
	}
	if selected == nil {
		return contract.Transition{}, contract.Reject("trial.unavailable", "%s", reason)
	}
	o := *selected
	if err = ruledata.Pay(s.Stock, o.plan.Costs); err != nil {
		return contract.Transition{}, err
	}
	result := map[string]any{"action_key": request.ActionKey, "label": o.plan.Label, "costs": o.plan.Costs, "minutes": o.plan.Minutes}
	finish := func(key, cause string) {
		run.OutcomeKey = key
		run.Status = contract.RunFailed
		if slices.Contains(outcomeKeys[:3], key) {
			run.Status = contract.RunCompleted
		}
		s.DeathCause = cause
		s.Tribulation.Active = false
	}
	remaining := lifeRemaining(c, s, run)
	// Time is paid before success, loot or recovery. Crossing the lifespan
	// boundary can never grant an extra breakthrough or after-death reward.
	if o.plan.Minutes >= remaining {
		run.GameTime += remaining
		result["minutes"] = remaining
		finish("lifespan_exhausted", "lifespan")
	} else {
		run.GameTime += o.plan.Minutes
		ok := true
		draw := int64(0)
		if o.probability < 100 {
			draw, err = request.Random.Intn("trial.resolve", 100)
			if err != nil {
				return contract.Transition{}, err
			}
			draw++
			ok = draw <= o.probability
			result["roll"] = draw
			result["chance"] = o.probability
		}
		result["success"] = ok
		add := func(key string, n int64) {
			if err == nil {
				err = ruledata.Add(s.Stock, key, n)
			}
		}
		damage := func(n, injury int64, cause string) {
			s.HP = max(0, s.HP-n)
			s.Injury = min(100, s.Injury+injury)
			result["damage"] = n
			if s.HP == 0 {
				finish("perished", cause)
			}
		}
		cooldown := func() { s.CooldownUntil = run.GameTime + c.Routes.CooldownDays*Day }
		switch request.ActionKey {
		case "trial.activity":
			a := o.activity
			if ok {
				s.Completed[a.Key]++
				add("stone", a.Stones*int64(s.Realm+1))
				add("herb", a.Herbs)
				s.Karma = clamp(s.Karma+a.Karma, -20, 20)
				if a.Kind == "inheritance" {
					s.Method = a.MethodKey
				}
				if a.Kind == "affinity" {
					s.AffinityQualified = true
				}
			} else if a.Damage > 0 {
				damage(a.Damage, 5, "combat")
			}
		case "trial.practice":
			s.Progress = min(100, s.Progress+c.Realms[s.Realm].PracticeGain+method(c, s).GrowthBonus+(s.Insight-55)/10)
		case "trial.breakthrough":
			if ok {
				s.Realm++
				s.Progress = 0
				s.HP = min(100, s.HP+20)
			} else {
				s.Progress = max(0, s.Progress-20)
				damage(15, 10, "combat")
			}
		case "trial.buy":
			add(o.item.Key, 1)
		case "trial.sell":
			add("stone", o.item.Price/2)
		case "trial.craft":
			if ok {
				add(o.recipe.Output, 1)
			}
		case "trial.consume":
			if o.item.Key == "life-pill" {
				s.LifePills++
			} else {
				s.HP = min(100, s.HP+40)
			}
		case "trial.rest":
			s.HP = min(100, s.HP+35)
			s.Injury = max(0, s.Injury-25)
		case "trial.wait":
		case "trial.affinity_seek":
			s.AffinityAttempts++
			s.Token = ok
		case "trial.affinity_use":
			s.Token = false
			s.TokenUsed = true
			finish("affinity_ascension", "")
		case "trial.rift_search":
			s.Rift = ok
			if !ok {
				cooldown()
			}
		case "trial.rift_enter":
			s.Rift = false
			if ok {
				s.RiftCrossed = true
				finish("void_ascension", "")
			} else {
				n := int64(40)
				if o.plan.Costs["void-anchor"] > 0 {
					n -= 15
				}
				damage(n, 20, "void_rift")
				cooldown()
			}
		case "trial.tribulation_begin":
			s.Tribulation = Tribulation{Attempt: s.Tribulation.Attempt + 1, Active: true, Wave: 1, Ward: true}
		case "trial.tribulation_wave":
			if o.plan.Costs["thunder-talisman"] > 0 {
				s.Tribulation.ProtectedPhase = phase(s.Tribulation.Wave)
			}
			if ok {
				s.Tribulation.Wave++
				if s.Tribulation.Wave == 10 {
					finish("orthodox_ascension", "")
				}
			} else {
				n := waveDamage(c, s, o.plan.Costs)
				if draw >= 96 {
					n += 15
				}
				s.Tribulation.Failures++
				damage(n, waveInjury(s, o.plan.Costs), "tribulation")
			}
		case "trial.tribulation_retreat":
			s.Tribulation.Active = false
			s.Injury = min(100, s.Injury+15)
			cooldown()
		case "trial.abandon":
			finish("abandoned", "")
		case "trial.pass_away":
			finish("peaceful_passing", "voluntary")
		default:
			return contract.Transition{}, fmt.Errorf("未实现的单局动作")
		}
		if err != nil {
			return contract.Transition{}, err
		}
	}
	data, err := ruledata.Object(s)
	if err != nil {
		return contract.Transition{}, err
	}
	run.Data = map[string]any{"life": data}
	result["outcome_key"] = run.OutcomeKey
	events := []contract.RuntimeEvent{{Kind: "trial.acted", Visibility: contract.VisibilityPlayer, Data: result}}
	if run.Status != contract.RunActive {
		events = append(events, contract.RuntimeEvent{Kind: "trial.ended", Visibility: contract.VisibilityPlayer, Data: map[string]any{"outcome_key": run.OutcomeKey, "cause": s.DeathCause}})
	}
	return contract.Transition{State: run, Result: result, Events: events}, nil
}

func (Module) Project(content contract.Content, run contract.RunState, _ string) (map[string]any, error) {
	c, s, err := read(content, run)
	if err != nil {
		return nil, err
	}
	inventory := []map[string]any{}
	for _, i := range c.Items {
		if s.Stock[i.Key] > 0 {
			inventory = append(inventory, map[string]any{"name": i.Name, "quantity": s.Stock[i.Key]})
		}
	}
	preparation := int64(0)
	if s.Stock["ward"] > 0 {
		preparation += 50
	}
	preparation += min(3, s.Stock["thunder-talisman"]) * 10
	goal := "取得首部功法：可通过药庐帮工、宗门请求或险地遗刻获得不同传承"
	if s.Method != "" {
		goal = "积累修为、备齐破境丹并逐级突破；安全委托与险地历练均可补充资源"
	}
	if s.Realm >= 3 {
		goal = "完成仙缘委托后最多两次寻符；继续修行可开启元婴裂缝与化神九波天劫"
	}
	if s.Realm >= 5 {
		goal = "化神及以上修为圆满后可引劫；备足灵石、护体法宝、雷符与疗伤丹，也可继续突破降低风险"
	}
	if s.Tribulation.Active {
		goal = fmt.Sprintf("第 %d 次雷劫，第 %d / 9 波；可承劫、服药或撤退，失败不跳波", s.Tribulation.Attempt, s.Tribulation.Wave)
	}
	if run.Status != contract.RunActive {
		goal = "本世已结束"
	}
	birthName := ""
	for _, b := range c.Births {
		if b.Key == s.Birth {
			birthName = b.Name
		}
	}
	deathNames := map[string]string{"": "", "lifespan": "寿元耗尽", "combat": "冒险或破境陨落", "tribulation": "雷劫陨落", "void_rift": "虚空陨落", "voluntary": "主动坐化"}
	view := map[string]any{"realm": c.Realms[s.Realm].Name, "realm_key": c.Realms[s.Realm].Key, "birth": birthName, "age": math.Round(float64(s.AgeAtBirth+run.GameTime)/float64(Year)*10) / 10, "lifespan": c.Realms[s.Realm].LifespanYears + s.LifePills*10, "remaining_days": max(0, lifeRemaining(c, s, run)) / Day, "elapsed_days": run.GameTime / Day, "health": s.HP, "injury": s.Injury, "progress": s.Progress, "method": method(c, s).Name, "vigor": s.Vigor, "insight": s.Insight, "luck": s.Luck, "karma": s.Karma, "inventory": inventory, "goal": goal, "affinity_qualified": s.AffinityQualified, "affinity_remaining": c.Routes.AffinityAttempts - s.AffinityAttempts, "affinity_token": s.Token, "rift_discovered": s.Rift, "cooldown_days": (max(0, s.CooldownUntil-run.GameTime) + Day - 1) / Day, "preparation": preparation, "tribulation_attempt": s.Tribulation.Attempt, "tribulation_wave": s.Tribulation.Wave, "tribulation_active": s.Tribulation.Active, "outcome_key": run.OutcomeKey, "death_cause": deathNames[s.DeathCause]}
	return view, nil
}
