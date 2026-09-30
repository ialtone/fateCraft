package llm

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"
)

// streamIdleTimeout 是响应体两次读到数据之间允许的最长间隔。它只约束
// "供应商完全不吐数据"的挂起,不限制总生成时长;总时长由调用方 ctx 决定。
const streamIdleTimeout = 90 * time.Second

// ErrLLMStreamStalled 表示响应体在 streamIdleTimeout 内没有任何新数据。
var ErrLLMStreamStalled = fmt.Errorf("模型连续 %d 秒没有返回任何数据", int(streamIdleTimeout/time.Second))

// idleBody 给响应体加上空闲看门狗:每次 Read 前重置计时,超时则关闭底层
// 连接,让阻塞中的 Read 立即返回并被映射为 ErrLLMStreamStalled。
type idleBody struct {
	rc      io.ReadCloser
	timeout time.Duration
	timer   *time.Timer
	stalled atomic.Bool
	mu      sync.Mutex
	closed  bool
}

func newIdleBody(rc io.ReadCloser, timeout time.Duration) *idleBody {
	b := &idleBody{rc: rc, timeout: timeout}
	b.timer = time.AfterFunc(timeout, func() {
		b.stalled.Store(true)
		b.mu.Lock()
		defer b.mu.Unlock()
		if !b.closed {
			b.closed = true
			_ = b.rc.Close()
		}
	})
	return b
}

func (b *idleBody) Read(p []byte) (int, error) {
	if b.stalled.Load() {
		return 0, ErrLLMStreamStalled
	}
	b.timer.Reset(b.timeout)
	n, err := b.rc.Read(p)
	if err != nil && b.stalled.Load() {
		return n, ErrLLMStreamStalled
	}
	return n, err
}

func (b *idleBody) Close() error {
	b.timer.Stop()
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil
	}
	b.closed = true
	return b.rc.Close()
}

// These limits protect the model boundary from provider/gateway responses that
// are much larger than the configured token budget. They are deliberately
// byte-based because the transport has not decoded a provider payload yet.
const (
	maxLLMResponseBytes = 32 << 20  // 32 MiB complete response or accumulated text
	maxSSELineBytes     = 1 << 20   // 1 MiB per event/data line
	maxLLMAuditBytes    = 512 << 10 // detailed logs are diagnostic, not a second payload store
	maxHTTPErrorBytes   = 64 << 10
)

var (
	ErrLLMResponseTooLarge = errors.New("模型响应超过允许大小")
	ErrLLMSSELineTooLarge  = errors.New("模型 SSE 事件行超过允许大小")
)

// readAllBounded consumes at most max+1 bytes so callers can distinguish an
// exactly-at-limit response from one that exceeded the boundary.
func readAllBounded(reader io.Reader, max int) ([]byte, error) {
	if max <= 0 {
		max = maxLLMResponseBytes
	}
	data, err := io.ReadAll(io.LimitReader(reader, int64(max)+1))
	if len(data) > max {
		return data[:max], ErrLLMResponseTooLarge
	}
	return data, err
}

// auditBuffer keeps a bounded prefix while retaining one extra byte as an
// overflow marker. writeAudit/writeStructuredAudit turn that marker into the
// explicit audit_truncated flag.
type auditBuffer struct {
	data      []byte
	max       int
	total     int64
	truncated bool
}

func newCaptureBuffer(enabled bool) *auditBuffer {
	if !enabled {
		return &auditBuffer{max: -1}
	}
	return newAuditBuffer(maxLLMAuditBytes)
}

func newAuditBuffer(max int) *auditBuffer {
	if max <= 0 {
		max = maxLLMAuditBytes
	}
	return &auditBuffer{data: make([]byte, 0, max+1), max: max}
}

func (b *auditBuffer) Write(p []byte) (int, error) {
	if b == nil || len(p) == 0 {
		return len(p), nil
	}
	b.total += int64(len(p))
	// Retain one byte past the persisted limit. That byte is never stored in
	// the audit row; it lets boundedAuditString distinguish an exact-limit
	// payload from a genuinely truncated stream.
	remaining := b.max + 1 - len(b.data)
	if remaining > 0 {
		if len(p) > remaining {
			b.data = append(b.data, p[:remaining]...)
		} else {
			b.data = append(b.data, p...)
		}
	}
	b.truncated = b.total > int64(b.max)
	return len(p), nil
}

func (b *auditBuffer) String() string {
	if b == nil {
		return ""
	}
	return string(b.data)
}

func (b *auditBuffer) Truncated() bool {
	return b != nil && b.truncated
}

func (b *auditBuffer) TooLarge() bool {
	return b != nil && b.total > maxLLMResponseBytes
}

func boundedAuditString(value string) (string, bool) {
	text, info := safeAuditText(value, "", maxLLMAuditBytes)
	return text, info.Truncated
}

// readSSELine bounds a complete SSE line while still accepting a line that is
// split across bufio.Reader buffers. It returns a diagnostic prefix with the
// explicit error as soon as the provider exceeds the line boundary.
func readSSELine(reader *bufio.Reader) (string, error) {
	var line []byte
	for {
		part, err := reader.ReadSlice('\n')
		if len(line)+len(part) > maxSSELineBytes {
			remaining := maxSSELineBytes - len(line)
			if remaining > 0 {
				line = append(line, part[:remaining]...)
			}
			return string(line), ErrLLMSSELineTooLarge
		}
		line = append(line, part...)
		if err != bufio.ErrBufferFull {
			return string(line), err
		}
	}
}
