package llm

import (
	"context"
	"errors"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/ialtone/fatecraft/internal/logging"
)

var errAsyncAuditClosed = errors.New("LLM 详细日志写入器已关闭")

type auditTask struct {
	ctx   context.Context
	audit RequestAudit
}

// AsyncRequestAuditWriter 把详细请求日志从模型请求关键路径移到有界后台队列。
// 队列满时立即返回错误，不阻塞叙事；调用方会记录丢弃告警。
type AsyncRequestAuditWriter struct {
	writer RequestAuditWriter
	queue  chan auditTask
	done   chan struct{}

	mu     sync.RWMutex
	closed bool
}

func NewAsyncRequestAuditWriter(writer RequestAuditWriter, capacity int) *AsyncRequestAuditWriter {
	if capacity <= 0 {
		capacity = 128
	}
	w := &AsyncRequestAuditWriter{
		writer: writer,
		queue:  make(chan auditTask, capacity),
		done:   make(chan struct{}),
	}
	go w.run()
	return w
}

func (w *AsyncRequestAuditWriter) Write(ctx context.Context, audit RequestAudit) error {
	if w == nil || w.writer == nil {
		return nil
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.closed {
		return errAsyncAuditClosed
	}
	if ctx == nil {
		ctx = context.Background()
	}
	task := auditTask{ctx: context.WithoutCancel(ctx), audit: cloneRequestAudit(audit)}
	select {
	case w.queue <- task:
		return nil
	default:
		return errors.New("LLM 详细日志队列已满")
	}
}

func (w *AsyncRequestAuditWriter) run() {
	defer close(w.done)
	for task := range w.queue {
		ctx, cancel := context.WithTimeout(task.ctx, 5*time.Second)
		err := w.writer(ctx, task.audit)
		cancel()
		if err != nil {
			logging.Named("llm").Warn("异步详细模型请求日志写入失败", zap.Error(err))
		}
	}
}

// Close 停止接收新日志并等待队列排空。服务正常关闭时应调用它。
func (w *AsyncRequestAuditWriter) Close(ctx context.Context) error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	if !w.closed {
		w.closed = true
		close(w.queue)
	}
	w.mu.Unlock()
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-w.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func cloneRequestAudit(audit RequestAudit) RequestAudit {
	audit.RequestBody = append([]byte(nil), audit.RequestBody...)
	audit.OutputTypes = append([]string(nil), audit.OutputTypes...)
	return audit
}
