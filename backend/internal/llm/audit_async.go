package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/ialtone/fatecraft/internal/logging"
	"go.uber.org/zap"
)

var errAsyncAuditClosed = errors.New("模型诊断写入器已关闭")

const auditQueueBytes = 32 << 20

type auditTask struct {
	ctx   context.Context
	audit RequestAudit
	bytes int64
}
type AuditWriterStatus struct {
	Pending       int               `json:"pending"`
	Bytes         int64             `json:"bytes"`
	Capacity      int               `json:"capacity"`
	ByteLimit     int64             `json:"byte_limit"`
	Written       uint64            `json:"written"`
	Failed        uint64            `json:"failed"`
	Dropped       map[string]uint64 `json:"dropped"`
	LastFailureAt *time.Time        `json:"last_failure_at,omitempty"`
	Closed        bool              `json:"closed"`
	Unflushed     int               `json:"unflushed"`
}

// Bounds queued and in-flight bytes. Diagnostic loss never replays business work.
type AsyncRequestAuditWriter struct {
	writer      RequestAuditWriter
	queue       chan auditTask
	done        chan struct{}
	mu          sync.Mutex
	closed      bool
	status      AuditWriterStatus
	lastWarning time.Time
	suppressed  uint64
}

func NewAsyncRequestAuditWriter(writer RequestAuditWriter, capacity int) *AsyncRequestAuditWriter {
	if capacity <= 0 || capacity > 128 {
		capacity = 128
	}
	w := &AsyncRequestAuditWriter{writer: writer, queue: make(chan auditTask, capacity), done: make(chan struct{}), status: AuditWriterStatus{Capacity: capacity, ByteLimit: auditQueueBytes, Dropped: map[string]uint64{}}}
	go w.run()
	return w
}

func (w *AsyncRequestAuditWriter) Status() AuditWriterStatus {
	w.mu.Lock()
	defer w.mu.Unlock()
	status := w.status
	status.Dropped = map[string]uint64{}
	for key, value := range w.status.Dropped {
		status.Dropped[key] = value
	}
	if status.LastFailureAt != nil {
		value := *status.LastFailureAt
		status.LastFailureAt = &value
	}
	return status
}

func auditSize(a RequestAudit) int64 {
	// Encoded field metadata plus string backing storage and conservative record overhead.
	metadata, _ := json.Marshal(a.BodyMetadata)
	size := int64(len(a.RequestBody) + len(metadata) + 4096)
	value := reflect.ValueOf(a)
	for i := 0; i < value.NumField(); i++ {
		if value.Field(i).Kind() == reflect.String {
			size += int64(value.Field(i).Len())
		}
	}
	for _, value := range a.OutputTypes {
		size += int64(len(value) + 16)
	}
	return size
}

func (w *AsyncRequestAuditWriter) Write(ctx context.Context, audit RequestAudit) error {
	if w == nil || w.writer == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	size := auditSize(audit)
	w.mu.Lock()
	defer w.mu.Unlock()
	reason := ""
	switch {
	case w.closed:
		reason = "closed"
	case size > auditRecordLimit:
		reason = "record_limit"
	case w.status.Pending >= w.status.Capacity:
		reason = "queue_limit"
	case w.status.Bytes+size > w.status.ByteLimit:
		reason = "byte_limit"
	}
	if reason != "" {
		w.status.Dropped[reason]++
		w.warnLocked("模型诊断丢弃", reason)
		if reason == "closed" {
			return errAsyncAuditClosed
		}
		return errors.New("模型诊断预算已满")
	}
	// Reserve before copying, and keep the reservation until the write finishes.
	w.status.Pending++
	w.status.Bytes += size
	w.queue <- auditTask{ctx: context.WithoutCancel(ctx), audit: cloneRequestAudit(audit), bytes: size}
	return nil
}

func (w *AsyncRequestAuditWriter) warnLocked(message, reason string) {
	now := time.Now()
	w.status.LastFailureAt = &now
	if !w.lastWarning.IsZero() && now.Sub(w.lastWarning) < time.Minute {
		w.suppressed++
		return
	}
	logging.Named("diagnostics").Warn(message, zap.String("reason", reason), zap.Uint64("suppressed", w.suppressed))
	w.lastWarning = now
	w.suppressed = 0
}

func (w *AsyncRequestAuditWriter) run() {
	defer close(w.done)
	for task := range w.queue {
		ctx, cancel := context.WithTimeout(task.ctx, 5*time.Second)
		err := w.persist(ctx, task.audit)
		cancel()
		w.mu.Lock()
		w.status.Pending--
		w.status.Bytes -= task.bytes
		if err != nil {
			w.status.Failed++
			w.warnLocked("模型诊断写入失败", logging.ErrorText(err))
		} else {
			w.status.Written++
		}
		w.mu.Unlock()
	}
}

func (w *AsyncRequestAuditWriter) persist(ctx context.Context, audit RequestAudit) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("diagnostic writer panic (%T)", recovered)
		}
	}()
	return w.writer(ctx, audit)
}

func (w *AsyncRequestAuditWriter) Close(ctx context.Context) error {
	if w == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	w.mu.Lock()
	if !w.closed {
		w.closed = true
		w.status.Closed = true
		close(w.queue)
	}
	w.mu.Unlock()
	select {
	case <-w.done:
		return nil
	case <-ctx.Done():
		w.mu.Lock()
		w.status.Unflushed = w.status.Pending
		w.mu.Unlock()
		return ctx.Err()
	}
}

func cloneRequestAudit(a RequestAudit) RequestAudit {
	a.RequestBody = append([]byte(nil), a.RequestBody...)
	value := reflect.ValueOf(&a).Elem()
	for i := 0; i < value.NumField(); i++ {
		if value.Field(i).Kind() == reflect.String {
			value.Field(i).SetString(strings.Clone(value.Field(i).String()))
		}
	}
	a.OutputTypes = append([]string(nil), a.OutputTypes...)
	for i := range a.OutputTypes {
		a.OutputTypes[i] = strings.Clone(a.OutputTypes[i])
	}
	metadata := map[string]AuditFieldInfo{}
	for key, value := range a.BodyMetadata {
		metadata[strings.Clone(key)] = value
	}
	a.BodyMetadata = metadata
	if a.InputTokens != nil {
		value := *a.InputTokens
		a.InputTokens = &value
	}
	if a.OutputTokens != nil {
		value := *a.OutputTokens
		a.OutputTokens = &value
	}
	return a
}
