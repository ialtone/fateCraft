package roll

import (
	"context"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/ialtone/fatecraft/internal/logging"
	"github.com/ialtone/fatecraft/internal/storage"
)

// HistoryStore 是判定历史的持久化接口（消费端定义，*storage.RollHistoryRepo 天然满足）。
// 历史入库使连败补偿/连胜惩罚跨进程重启仍然生效。
type HistoryStore interface {
	// Append 追加一条判定流水。
	Append(ctx context.Context, convID uuid.UUID, rollType string, success bool) error
	// Recent 按时间倒序返回最近 limit 条流水（最新在前）。
	Recent(ctx context.Context, convID uuid.UUID, limit int) ([]storage.RollHistoryEntry, error)
}

// recentWindow 计算历史调整时读取的流水条数（与旧内存实现的"最近10次"一致）。
const recentWindow = 10

// historyAdjustment 从最近的判定流水（最新在前）计算历史调整值。
// 纯函数：连败补偿、同类行动重试加成、高收益连胜惩罚。
func historyAdjustment(recent []storage.RollHistoryEntry, rollType string, rewardValue int, config *Config) int {
	if len(recent) == 0 {
		return 0
	}

	adjustment := 0

	// 连续失败/成功计数：从最新一条向前数同结果的连续段。
	consecutiveFails, consecutiveSucc := 0, 0
	if !recent[0].Success {
		for _, r := range recent {
			if r.Success {
				break
			}
			consecutiveFails++
		}
	} else {
		for _, r := range recent {
			if !r.Success {
				break
			}
			consecutiveSucc++
		}
	}

	// 连续失败加成：超过配置最大档时封顶到最大档（连败 5 次不应比 4 次补偿更少）。
	if consecutiveFails > 0 {
		if bonus, ok := config.HistoryAdjust.ConsecutiveFails[consecutiveFails]; ok {
			adjustment += bonus
		} else {
			maxTier, maxBonus := 0, 0
			for tier, bonus := range config.HistoryAdjust.ConsecutiveFails {
				if tier > maxTier {
					maxTier, maxBonus = tier, bonus
				}
			}
			if consecutiveFails > maxTier {
				adjustment += maxBonus
			}
		}
	}

	// 同类行动重试加成（上一次同类判定失败后重试）
	last := recent[0]
	if last.RollType == rollType && !last.Success {
		adjustment += config.HistoryAdjust.SameActionRetry.Bonus
	}

	// 连续成功惩罚（仅高收益判定）
	if consecutiveSucc >= config.HistoryAdjust.LuckyStreakPenalty.Threshold &&
		rewardValue >= 100000 {
		adjustment += config.HistoryAdjust.LuckyStreakPenalty.Penalty
	}

	return adjustment
}

// getAdjustment 读取流水并计算历史调整；读取失败时记日志并按无历史处理（判定不因此失败）。
func (s *Service) getAdjustment(ctx context.Context, convID uuid.UUID, rollType string, rewardValue int) int {
	if s.history == nil {
		return 0
	}
	recent, err := s.history.Recent(ctx, convID, recentWindow)
	if err != nil {
		logging.Named("roll").Warn("读取判定历史失败，本次按无历史处理",
			zap.String("conversation_id", convID.String()), zap.Error(err))
		return 0
	}
	return historyAdjustment(recent, rollType, rewardValue, s.config)
}
