package roll

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"
)

// ExecuteRoll 执行掷骰并确定结果等级。
// 成功率以 1% 为粒度计算，只有面数是 100 的倍数时才能精确表示，其他面数直接拒绝，
// 避免例如 6 面骰把 1% 的成功率放大成 1/6。
func ExecuteRoll(detailedResult *DetailedResult, sides int) (*RollOutcome, error) {
	if sides < 100 || sides%100 != 0 {
		return nil, fmt.Errorf("骰子面数必须是 100 的倍数")
	}
	value, err := rand.Int(rand.Reader, big.NewInt(int64(sides)))
	if err != nil {
		return nil, fmt.Errorf("生成安全随机数: %w", err)
	}
	rolled := int(value.Int64()) + 1

	outcome := &RollOutcome{
		Rolled:      rolled,
		Sides:       sides,
		SuccessRate: detailedResult.Breakdown.Final,
		Breakdown:   detailedResult.Breakdown,
		Outcomes:    detailedResult.Outcomes,
	}

	// 结果区间（RangeStart/RangeEnd）以 1-100 百分比刻度定义。面数是 100 的倍数时，
	// 每个百分点恰好对应 sides/100 个点数，映射后的概率与计算出的成功率完全一致；
	// sides=100 时 normalized == rolled。
	normalized := 1 + (rolled-1)*100/sides

	// 确定结果等级
	dist := detailedResult.Outcomes
	if dist.CriticalSuccess != nil && normalized >= dist.CriticalSuccess.RangeStart && normalized <= dist.CriticalSuccess.RangeEnd {
		outcome.Level = "critical_success"
		outcome.Success = true
	} else if normalized >= dist.Success.RangeStart && normalized <= dist.Success.RangeEnd {
		outcome.Level = "success"
		outcome.Success = true
	} else if dist.CriticalFailure != nil && normalized >= dist.CriticalFailure.RangeStart && normalized <= dist.CriticalFailure.RangeEnd {
		outcome.Level = "critical_failure"
		outcome.Success = false
	} else {
		outcome.Level = "failure"
		outcome.Success = false
	}
	// Keep the authoritative success interval with the outcome snapshot. The
	// interval is expressed on the normalized 1-100 scale, so clients never
	// need to reproduce arbitrary-sided-die conversion logic.
	if dist != nil {
		outcome.SuccessRange = dist.Success
	}

	return outcome, nil
}

// RollOutcome 掷骰结果。
type RollOutcome struct {
	Rolled      int        `json:"rolled"`
	Sides       int        `json:"sides"`
	SuccessRate int        `json:"success_rate"`
	Success     bool       `json:"success"`
	Level       string     `json:"level"` // critical_success / success / failure / critical_failure
	Breakdown   *Breakdown `json:"breakdown"`
	// Outcomes and SuccessRange are immutable server snapshots used by the
	// player-facing details panel. They are optional for backwards compatibility.
	Outcomes     *OutcomeDistribution `json:"outcomes,omitempty"`
	SuccessRange *OutcomeRange        `json:"success_range,omitempty"`
}

// FormatResult 格式化判定结果为文本。
func FormatResult(rollType string, description string, outcome *RollOutcome) string {
	var b strings.Builder

	// 标题行
	levelText := ""
	switch outcome.Level {
	case "critical_success":
		levelText = "大成功！"
	case "success":
		levelText = "判定成功"
	case "failure":
		levelText = "判定失败"
	case "critical_failure":
		levelText = "大失败！"
	}

	b.WriteString(fmt.Sprintf("【天命判定】%s (D%d)\n", rollType, outcome.Sides))
	b.WriteString(fmt.Sprintf("行为：%s\n", description))
	b.WriteString(fmt.Sprintf("投出：%d，目标：%d，%s\n", outcome.Rolled, outcome.SuccessRate, levelText))

	return b.String()
}

// FormatDetailedResult 格式化详细的判定结果（包含分解）。
func FormatDetailedResult(rollType string, description string, outcome *RollOutcome) string {
	var b strings.Builder

	b.WriteString(FormatResult(rollType, description, outcome))
	b.WriteString("\n【成功率分解】\n")

	bd := outcome.Breakdown
	b.WriteString(fmt.Sprintf("基础：%d%%\n", bd.Base))
	if bd.Attributes != 0 {
		b.WriteString(fmt.Sprintf("属性：%+d%%\n", bd.Attributes))
	}
	if bd.Traits != 0 {
		b.WriteString(fmt.Sprintf("天赋特质：%+d%%\n", bd.Traits))
	}
	if bd.Context != 0 {
		b.WriteString(fmt.Sprintf("情境：%+d%%\n", bd.Context))
	}
	if bd.RewardCap != 0 {
		b.WriteString(fmt.Sprintf("收益平衡：%+d%%\n", bd.RewardCap))
	}
	if bd.History != 0 {
		b.WriteString(fmt.Sprintf("历史调整：%+d%%\n", bd.History))
	}
	if bd.DifficultyMode != 0 {
		b.WriteString(fmt.Sprintf("难度模式：%+d%%\n", bd.DifficultyMode))
	}
	b.WriteString(fmt.Sprintf("最终：%d%%", bd.Final))

	return b.String()
}
