package orchestration

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/ialtone/fatecraft/internal/operation"
	"github.com/ialtone/fatecraft/internal/storage"
)

// AgentToolTransaction is the database boundary for a mutating tool. Model
// calls never hold this transaction; it is opened only after a complete tool
// call has been received and remains short enough for PostgreSQL to commit the
// domain fact and its audit record atomically.
type AgentToolTransaction struct {
	db                 *gorm.DB
	definitionProvider storage.GameplayDefinitionProvider
}

func NewAgentToolTransaction(db *gorm.DB, providers ...storage.GameplayDefinitionProvider) *AgentToolTransaction {
	var provider storage.GameplayDefinitionProvider
	if len(providers) > 0 {
		provider = providers[0]
	}
	return &AgentToolTransaction{db: db, definitionProvider: provider}
}

// agentToolTx 把工具执行期间的每一次数据库访问都绑定到同一个事务连接。
// 事务内持有多张表的行锁，若再向全局连接池申请第二个连接，高并发下会把
// 连接池耗尽并互相等待，整站无响应。
type agentToolTx struct {
	db         *gorm.DB
	agents     *storage.AgentRepo
	states     *storage.StateRepo
	campaigns  *storage.CampaignRepository
	challenges *storage.ChallengeRepository
	messages   *storage.MessageRepo
	rollHist   *storage.RollHistoryRepo
}

func (t *AgentToolTransaction) Run(ctx context.Context, fn func(*agentToolTx) error) error {
	if t == nil || t.db == nil || fn == nil {
		return errors.New("Agent 工具事务未配置")
	}
	return t.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if metadata, ok := operation.FromContext(ctx); ok && metadata.ConversationID != uuid.Nil {
			if err := storage.AssertGenerationActive(ctx, tx, metadata.ConversationID); err != nil {
				return err
			}
		}
		return fn(&agentToolTx{
			db: tx, agents: storage.NewAgentRepo(tx), states: storage.NewStateRepo(tx),
			campaigns: storage.NewCampaignRepository(tx, t.definitionProvider), challenges: storage.NewChallengeRepository(tx),
			messages: storage.NewMessageRepo(tx), rollHist: storage.NewRollHistoryRepo(tx),
		})
	})
}
