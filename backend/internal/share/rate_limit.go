package share

import (
	"sync"
	"time"
)

type rateWindow struct {
	started time.Time
	count   int
}

type rateLimiter struct {
	mu      sync.Mutex
	windows map[string]rateWindow
	now     func() time.Time
}

func newRateLimiter() *rateLimiter {
	return &rateLimiter{windows: make(map[string]rateWindow), now: time.Now}
}

func (l *rateLimiter) allow(key string, maximum int, duration time.Duration) bool {
	if key == "" || maximum < 1 || duration <= 0 {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	window := l.windows[key]
	if window.started.IsZero() || now.Sub(window.started) >= duration {
		l.windows[key] = rateWindow{started: now, count: 1}
		return true
	}
	if window.count >= maximum {
		return false
	}
	window.count++
	l.windows[key] = window
	if len(l.windows) > 10_000 {
		for existingKey, existing := range l.windows {
			if now.Sub(existing.started) >= time.Hour {
				delete(l.windows, existingKey)
			}
		}
	}
	return true
}
