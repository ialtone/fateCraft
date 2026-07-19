package roll

import (
	"context"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/ialtone/fatecraft/internal/logging"
)

// Service 判定服务：持有判定配置与判定历史存储，
// 取代旧的 GetConfig()/GetTracker() 全局单例，由组合根（main）构造并注入下游。
type Service struct {
	config  *Config
	history HistoryStore
}

// NewService 用已加载的配置与历史存储构造判定服务。
// 历史存储持久化到数据库，连败补偿/连胜惩罚跨进程重启仍然生效。
func NewService(cfg *Config, history HistoryStore) *Service {
	return &Service{
		config:  cfg,
		history: history,
	}
}

// Record 记录一次判定结果，供后续判定做历史调整（连败补偿、重试加成等）。
// 写入失败留日志但不阻断主流程（历史缺一条只影响手感，不影响正确性）。
func (s *Service) Record(ctx context.Context, convID uuid.UUID, rollType string, success bool) {
	if s.history == nil {
		return
	}
	if err := s.history.Append(ctx, convID, rollType, success); err != nil {
		logging.Named("roll").Warn("写入判定历史失败",
			zap.String("conversation_id", convID.String()), zap.Error(err))
	}
}
