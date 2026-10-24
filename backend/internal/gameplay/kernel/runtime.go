// Package kernel enforces mode-independent budgets and runtime contracts.
// Persistence and authentication are supplied by Challenge/Campaign adapters.
package kernel

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strings"

	"github.com/ialtone/fatecraft/internal/gameplay/contract"
)

const MaxStateBytes = 64 * 1024
const MaxEvents = 32

// MaxActionMinutes 限定单次可信动作的最大耗时（约 6944 天 ≈ 19 年）。
// 修仙类玩法的深闭关（单次数千天）需要超出旧的 1e6 分钟（≈694 天）预算；
// 累计时钟仍受 MaxGameTime 约束。
const MaxActionMinutes int64 = 10_000_000

// Long-lived characters may outlive 1e9 minutes (~1900 years). Keep the
// cumulative clock bounded and exactly representable by JSON/JavaScript;
// the per-action work budget remains independent.
const MaxGameTime int64 = 100_000_000_000

type tracedRandom struct {
	source contract.RandomSource
	draws  []contract.RandomDraw
	used   map[string]bool
}

func (r *tracedRandom) Intn(purpose string, upper int64) (int64, error) {
	if !contract.ValidStableKey(purpose) || upper < 1 || upper > 1_000_000_000 || len(r.draws) >= 64 || r.used[purpose] {
		return 0, fmt.Errorf("随机请求无效或超过预算")
	}
	value, err := r.source.Intn(purpose, upper)
	if err != nil {
		return 0, err
	}
	if value < 0 || value >= upper {
		return 0, fmt.Errorf("随机源返回越界结果")
	}
	r.used[purpose] = true
	r.draws = append(r.draws, contract.RandomDraw{Purpose: purpose, Upper: upper, Value: value})
	return value, nil
}

type randomSource struct {
	seed  int64
	scope string
	used  map[string]bool
}

func NewRandom(seed int64, scope string) contract.RandomSource {
	return &randomSource{seed: seed, scope: scope, used: map[string]bool{}}
}

func (r *randomSource) Intn(purpose string, upper int64) (int64, error) {
	if !contract.ValidStableKey(purpose) || upper < 1 || upper > 1_000_000_000 || len(r.used) >= 64 || r.used[purpose] {
		return 0, fmt.Errorf("随机用途重复或超出预算: %s", purpose)
	}
	r.used[purpose] = true
	var key [8]byte
	binary.BigEndian.PutUint64(key[:], uint64(r.seed))
	for counter := uint64(0); counter < 16; counter++ {
		hash := hmac.New(sha256.New, key[:])
		hash.Write([]byte(r.scope))
		hash.Write([]byte{0})
		hash.Write([]byte(purpose))
		var bytes [8]byte
		binary.BigEndian.PutUint64(bytes[:], counter)
		hash.Write(bytes[:])
		value := binary.BigEndian.Uint64(hash.Sum(nil)[:8])
		// Rejection sampling avoids bias without allowing the caller to pick a seed.
		limit := uint64(math.MaxUint64) - uint64(math.MaxUint64)%uint64(upper)
		if value < limit {
			return int64(value % uint64(upper)), nil
		}
	}
	return 0, fmt.Errorf("随机采样预算耗尽")
}

func clone[T any](value T, limit int) (T, error) {
	var result T
	encoded, err := json.Marshal(value)
	if err != nil {
		return result, err
	}
	if len(encoded) > limit {
		return result, fmt.Errorf("玩法数据超过 %d 字节预算", limit)
	}
	err = json.Unmarshal(encoded, &result)
	return result, err
}

func ValidateState(module contract.RuntimeModule, content contract.Content, state contract.RunState) error {
	var cloneErr error
	content, cloneErr = clone(content, 4*1024*1024)
	if cloneErr != nil {
		return cloneErr
	}
	state, cloneErr = clone(state, MaxStateBytes)
	if cloneErr != nil {
		return cloneErr
	}
	if state.SchemaVersion < 1 || state.Revision < 0 || state.Revision > 9007199254740990 || state.GameTime < 0 || state.GameTime > MaxGameTime || state.Data == nil {
		return fmt.Errorf("玩法状态头无效")
	}
	if state.Status != contract.RunActive && state.Status != contract.RunCompleted && state.Status != contract.RunFailed {
		return fmt.Errorf("玩法状态无效: %s", state.Status)
	}
	if (state.Status == contract.RunActive) != (state.OutcomeKey == "") {
		return fmt.Errorf("玩法终态与结局不一致")
	}
	if state.OutcomeKey != "" {
		found := false
		for _, outcome := range content.Outcomes {
			if outcome.Enabled && outcome.Key == state.OutcomeKey {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("玩法结局未在当前版本启用: %s", state.OutcomeKey)
		}
	}
	if _, err := clone(state, MaxStateBytes); err != nil {
		return err
	}
	if len(state.RandomEvidence) > 64 {
		return fmt.Errorf("随机证据超过预算")
	}
	seen := map[string]bool{}
	for _, draw := range state.RandomEvidence {
		if !contract.ValidStableKey(draw.Purpose) || seen[draw.Purpose] || draw.Upper < 1 || draw.Upper > 1_000_000_000 || draw.Value < 0 || draw.Value >= draw.Upper {
			return fmt.Errorf("随机证据无效")
		}
		seen[draw.Purpose] = true
	}
	return module.ValidateState(content, state)
}

func Initialize(module contract.RuntimeModule, content contract.Content, input contract.InitialInput) (contract.RunState, error) {
	var err error
	content, err = clone(content, 4*1024*1024)
	if err != nil {
		return contract.RunState{}, err
	}
	var trace *tracedRandom
	if input.Random != nil {
		trace = &tracedRandom{source: input.Random, used: map[string]bool{}}
		input.Random = trace
	}
	state, err := module.Initialize(content, input)
	if err != nil {
		return contract.RunState{}, err
	}
	if state.Revision != 0 || state.GameTime != 0 || state.Status != contract.RunActive {
		return contract.RunState{}, fmt.Errorf("玩法初始状态必须为第 0 次修订的活动状态")
	}
	state.RandomEvidence = nil
	if trace != nil {
		state.RandomEvidence = trace.draws
	}
	if err := ValidateState(module, content, state); err != nil {
		return contract.RunState{}, err
	}
	return clone(state, MaxStateBytes)
}

func Preview(module contract.RuntimeModule, content contract.Content, state contract.RunState, actor string) ([]contract.ActionPreview, error) {
	var err error
	content, err = clone(content, 4*1024*1024)
	if err != nil {
		return nil, err
	}
	if err := ValidateState(module, content, state); err != nil {
		return nil, err
	}
	if state.Status != contract.RunActive {
		return []contract.ActionPreview{}, nil
	}
	copy, err := clone(state, MaxStateBytes)
	if err != nil {
		return nil, err
	}
	previews, err := module.Preview(content, copy, actor)
	if err != nil {
		return nil, err
	}
	if len(previews) > 64 {
		return nil, fmt.Errorf("动作预览数量超过预算")
	}
	declared := map[string]contract.RuntimeAction{}
	for _, action := range module.Actions() {
		declared[action.Key] = action
	}
	seen := map[string]bool{}
	for _, preview := range previews {
		action, ok := declared[preview.Action.Key]
		if !ok || seen[action.Key] || !slices.Contains(action.Actors, actor) || preview.Minutes < 0 || preview.Minutes > MaxActionMinutes {
			return nil, fmt.Errorf("动作预览未声明、越权或超出预算: %s", preview.Action.Key)
		}
		seen[action.Key] = true
		if len(preview.Costs) > 32 {
			return nil, fmt.Errorf("动作成本超过预算")
		}
		for key, quantity := range preview.Costs {
			if !contract.ValidStableKey(key) || quantity < 0 || quantity > 1_000_000_000 {
				return nil, fmt.Errorf("动作成本无效")
			}
		}
		// Preview may narrow available string choices but cannot add parameters.
		if err := validatePreviewAction(action, preview.Action); err != nil {
			return nil, err
		}
		if len(preview.Plans) > 64 {
			return nil, fmt.Errorf("动作方案超过预算")
		}
		for _, plan := range preview.Plans {
			if plan.Label == "" || plan.Minutes < 0 || plan.Minutes > MaxActionMinutes || len(plan.Costs) > 32 {
				return nil, fmt.Errorf("动作方案无效")
			}
			if err := ValidateArguments(preview.Action, plan.Arguments); err != nil {
				return nil, err
			}
			for key, quantity := range plan.Costs {
				if !contract.ValidStableKey(key) || quantity < 0 || quantity > 1_000_000_000 {
					return nil, fmt.Errorf("动作方案成本无效")
				}
			}
		}
	}
	return clone(previews, 32*1024)
}

func Execute(module contract.RuntimeModule, content contract.Content, state contract.RunState, request contract.RuntimeRequest) (contract.Transition, error) {
	if request.Random == nil {
		return contract.Transition{}, fmt.Errorf("可信动作缺少随机源")
	}
	var err error
	content, err = clone(content, 4*1024*1024)
	if err != nil {
		return contract.Transition{}, err
	}
	previews, err := Preview(module, content, state, request.Actor)
	if err != nil {
		return contract.Transition{}, err
	}
	var action *contract.RuntimeAction
	for _, preview := range previews {
		if preview.Action.Key != request.ActionKey {
			continue
		}
		if !preview.Available {
			return contract.Transition{}, contract.Reject("action.unavailable", "%s", preview.Reason)
		}
		selected := preview.Action
		action = &selected
		break
	}
	if action == nil {
		return contract.Transition{}, contract.Reject("action.forbidden", "当前没有此操作")
	}
	if err := ValidateArguments(*action, request.Arguments); err != nil {
		return contract.Transition{}, err
	}
	copy, err := clone(state, MaxStateBytes)
	if err != nil {
		return contract.Transition{}, err
	}
	request.Arguments, err = clone(request.Arguments, 8192)
	if err != nil {
		return contract.Transition{}, err
	}
	trace := &tracedRandom{source: request.Random, used: map[string]bool{}}
	request.Random = trace
	transition, err := module.Execute(content, copy, request)
	if err != nil {
		return contract.Transition{}, err
	}
	if state.Revision == math.MaxInt64 || transition.State.SchemaVersion != state.SchemaVersion || transition.State.Revision != state.Revision || transition.State.GameTime < state.GameTime || transition.State.GameTime-state.GameTime > MaxActionMinutes {
		return contract.Transition{}, fmt.Errorf("动作非法修改修订或游戏时间")
	}
	transition.State.Revision++
	transition.State.RandomEvidence = trace.draws
	if err := ValidateState(module, content, transition.State); err != nil {
		return contract.Transition{}, err
	}
	// An accepted transition must not strand the save behind an oversized
	// action menu or projection. These are pure dry runs; failure aborts the
	// surrounding transaction before any costs or rewards can be committed.
	for _, actor := range []string{"player", "model"} {
		if _, err := Preview(module, content, transition.State, actor); err != nil {
			return contract.Transition{}, fmt.Errorf("动作后的%s预览不可用: %w", actor, err)
		}
	}
	for _, audience := range []string{contract.AudiencePlayer, contract.AudienceModel, contract.AudienceInquiry} {
		if _, err := Project(module, content, transition.State, audience); err != nil {
			return contract.Transition{}, fmt.Errorf("动作后的%s投影不可用: %w", audience, err)
		}
	}
	keys := map[string]bool{}
	for key := range state.Data {
		keys[key] = true
	}
	for key := range transition.State.Data {
		keys[key] = true
	}
	for key := range keys {
		_, beforeExists := state.Data[key]
		_, afterExists := transition.State.Data[key]
		before, _ := json.Marshal(state.Data[key])
		after, _ := json.Marshal(transition.State.Data[key])
		if (beforeExists != afterExists || string(before) != string(after)) && !slices.Contains(action.Writes, key) {
			return contract.Transition{}, fmt.Errorf("动作 %s 越权写入 %s", action.Key, key)
		}
	}
	if len(transition.Events) > MaxEvents {
		return contract.Transition{}, fmt.Errorf("动作事件超出预算")
	}
	for _, event := range transition.Events {
		if !slices.Contains(action.Events, event.Kind) || !contract.ValidStableKey(event.Kind) || event.Data == nil || !slices.Contains([]string{contract.VisibilityPlayer, contract.VisibilityModel, contract.VisibilityInternal}, event.Visibility) {
			return contract.Transition{}, fmt.Errorf("动作事件无效或未声明")
		}
	}
	return clone(transition, MaxStateBytes+32*1024)
}

func Project(module contract.RuntimeModule, content contract.Content, state contract.RunState, audience string) (map[string]any, error) {
	var err error
	content, err = clone(content, 4*1024*1024)
	if err != nil {
		return nil, err
	}
	if !slices.Contains([]string{contract.AudiencePlayer, contract.AudienceModel, contract.AudienceInquiry}, audience) {
		return nil, fmt.Errorf("投影主体无效")
	}
	if err := ValidateState(module, content, state); err != nil {
		return nil, err
	}
	copy, err := clone(state, MaxStateBytes)
	if err != nil {
		return nil, err
	}
	view, err := module.Project(content, copy, audience)
	if err != nil {
		return nil, err
	}
	if view == nil {
		return nil, fmt.Errorf("模块投影必须是对象")
	}
	if audience == contract.AudienceModel {
		paths := contract.NarrativeFactPaths(module)
		if len(paths) > contract.MaxModuleNarrativeFacts {
			return nil, fmt.Errorf("模块叙事关键事实不能超过 %d 项", contract.MaxModuleNarrativeFacts)
		}
		if _, err := contract.SelectNarrativeFacts(view, paths); err != nil {
			return nil, err
		}
	}
	return clone(view, 32*1024)
}

func ValidateArguments(action contract.RuntimeAction, arguments map[string]any) error {
	parameters := map[string]contract.RuntimeParameter{}
	for _, parameter := range action.Parameters {
		parameters[parameter.Key] = parameter
		if _, ok := arguments[parameter.Key]; !ok && parameter.Required {
			return contract.Reject("argument.required", "缺少参数 %s", parameter.Label)
		}
	}
	for key, value := range arguments {
		parameter, ok := parameters[key]
		if !ok {
			return contract.Reject("argument.unknown", "不支持参数 %s", key)
		}
		valid := false
		switch parameter.Type {
		case "string":
			text, ok := value.(string)
			valid = ok && strings.TrimSpace(text) != "" && len([]rune(text)) <= 128 && (len(parameter.Options) == 0 || slices.Contains(parameter.Options, text))
		case "integer":
			number, ok := Integer(value)
			valid = ok && number >= parameter.Minimum && number <= parameter.Maximum
		case "boolean":
			_, valid = value.(bool)
		}
		if !valid {
			return contract.Reject("argument.invalid", "参数 %s 不在当前允许范围内", parameter.Label)
		}
	}
	return nil
}

func Integer(value any) (int64, bool) {
	switch number := value.(type) {
	case int:
		return int64(number), true
	case int64:
		return number, true
	case float64:
		if !math.IsNaN(number) && !math.IsInf(number, 0) && number == math.Trunc(number) && number >= -9007199254740991 && number <= 9007199254740991 {
			return int64(number), true
		}
	}
	return 0, false
}

func validatePreviewAction(base, preview contract.RuntimeAction) error {
	if !reflect.DeepEqual(base.Actors, preview.Actors) || !reflect.DeepEqual(base.Writes, preview.Writes) || !reflect.DeepEqual(base.Events, preview.Events) {
		return fmt.Errorf("动作预览改变权限声明")
	}
	if len(base.Parameters) != len(preview.Parameters) {
		return fmt.Errorf("动作预览改变参数结构")
	}
	for index, parameter := range preview.Parameters {
		original := base.Parameters[index]
		if parameter.Key != original.Key || parameter.Type != original.Type || parameter.Required != original.Required || parameter.Minimum < original.Minimum || parameter.Maximum > original.Maximum {
			return fmt.Errorf("动作预览扩大参数范围")
		}
		if len(original.Options) > 0 {
			if len(parameter.Options) == 0 {
				return fmt.Errorf("动作预览移除选项约束")
			}
			for _, option := range parameter.Options {
				if !slices.Contains(original.Options, option) {
					return fmt.Errorf("动作预览扩大选项范围")
				}
			}
		}
	}
	return nil
}
