package worldactions

import (
	"fmt"
	"math"
	"sort"

	"github.com/ialtone/fatecraft/internal/campaign"
	"github.com/ialtone/fatecraft/internal/gameplay/components/worldrules"
	"github.com/ialtone/fatecraft/internal/storage"
)

// CheckTimeAdvance plans at most capacity+1 occurrences without mutating state.
// In particular it counts successors that have not yet been inserted into SQL.
// Long actions call it before spending resources; all actions also check inside
// the transaction before processing scheduled effects.
func CheckTimeAdvance(execution campaign.ActionExecution, minutes int64, actionEvents int) error {
	_, err := planDueEvents(execution, minutes, actionEvents)
	return err
}

func planDueEvents(execution campaign.ActionExecution, minutes int64, actionEvents int) (int, error) {
	if minutes < 0 || execution.Campaign.GameTime < 0 || execution.Campaign.GameTime > math.MaxInt64-minutes {
		return 0, Invalid("时间跨度无效或溢出")
	}
	if execution.MaxDueEventsPerTurn < 1 || actionEvents < 0 || actionEvents > 64 {
		return 0, Invalid("世界事件处理预算无效")
	}
	capacity := min(64-actionEvents, execution.MaxDueEventsPerTurn)
	end := execution.Campaign.GameTime + minutes
	queue, err := execution.Store.DueScheduledEvents(execution.Campaign.ID, end, capacity+1)
	if err != nil {
		return 0, err
	}
	count := 0
	for len(queue) > 0 {
		sort.Slice(queue, func(i, j int) bool { return scheduledBefore(queue[i], queue[j]) })
		row := queue[0]
		queue = queue[1:]
		if count == capacity {
			safe := max(int64(0), row.DueGameTime-execution.Campaign.GameTime-1)
			return 0, campaign.RejectAction(campaign.ActionViolation{
				Code: "world.time_span_exceeded", Path: "arguments", Public: true,
				Message: fmt.Sprintf("本次跨度超过世界事件预算，请缩短到至多 %d 分钟；到期事件不能跳过", safe),
				Details: storage.JSONMap{"max_minutes": safe, "event_budget": capacity, "blocked_at": row.DueGameTime},
			})
		}
		count++
		switch row.EventKind {
		case "quest_deadline":
		case storage.DefinitionWorldEvent:
			definition, err := execution.Store.Definition(execution.Campaign.ChallengeVersionID, storage.DefinitionWorldEvent, row.TemplateKey)
			if err != nil {
				return 0, err
			}
			event, valid := worldrules.ParseWorldEvent(map[string]any(definition.Content))
			if !valid {
				return 0, Invalid("世界事件定义无效: %s", row.TemplateKey)
			}
			if event.RepeatIntervalMinutes > 0 {
				if row.DueGameTime > math.MaxInt64-event.RepeatIntervalMinutes || row.Occurrence == math.MaxInt64 {
					return 0, Invalid("周期世界事件调度溢出")
				}
				row.DueGameTime += event.RepeatIntervalMinutes
				row.Occurrence++
				row.Priority = event.Priority
				if row.DueGameTime <= end {
					queue = append(queue, row)
				}
			}
		default:
			return 0, Invalid("未知计划事件类型: %s", row.EventKind)
		}
	}
	return count, nil
}

func scheduledBefore(left, right storage.CampaignScheduledEvent) bool {
	if left.DueGameTime != right.DueGameTime {
		return left.DueGameTime < right.DueGameTime
	}
	if left.Priority != right.Priority {
		return left.Priority > right.Priority
	}
	if left.EventKind != right.EventKind {
		return left.EventKind < right.EventKind
	}
	if left.TemplateKey != right.TemplateKey {
		return left.TemplateKey < right.TemplateKey
	}
	if left.Occurrence != right.Occurrence {
		return left.Occurrence < right.Occurrence
	}
	return left.ID.String() < right.ID.String()
}

func ProcessDueScheduledEvents(execution campaign.ActionExecution, outcome *storage.CampaignActionOutcome) error {
	if outcome == nil {
		return Invalid("缺少世界事件结算结果")
	}
	count, err := planDueEvents(execution, outcome.GameTimeDelta, len(outcome.Events))
	if err != nil {
		return err
	}
	end := execution.Campaign.GameTime + outcome.GameTimeDelta
	for index := 0; index < count; index++ {
		// Requery after each occurrence: a recurrence can precede another root.
		rows, err := execution.Store.DueScheduledEvents(execution.Campaign.ID, end, 1)
		if err != nil {
			return err
		}
		if len(rows) != 1 {
			return Invalid("计划事件在结算期间发生不一致变化")
		}
		row := &rows[0]
		draft, err := ProcessScheduledEvent(execution, row, row.DueGameTime)
		if err != nil {
			return err
		}
		draft.Payload["due_game_time"] = row.DueGameTime
		outcome.Events = append(outcome.Events, draft)
	}
	return nil
}
