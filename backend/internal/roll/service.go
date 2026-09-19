package roll

import (
	"context"

	"github.com/google/uuid"
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

// WithHistory 返回一个共享配置、但使用指定历史存储的判定服务副本。
// Agent 工具事务用它把判定历史读取绑定到当前事务连接，避免持锁期间再向连接池要第二个连接。
func (s *Service) WithHistory(history HistoryStore) *Service {
	if s == nil {
		return nil
	}
	return &Service{config: s.config, history: history}
}

// HasSuccessfulChallenge 用服务端判定流水校验挑战是否具备成功结算资格。
func (s *Service) HasSuccessfulChallenge(ctx context.Context, convID uuid.UUID) (bool, error) {
	if s == nil || s.history == nil {
		return false, nil
	}
	return s.history.HasSuccess(ctx, convID)
}
