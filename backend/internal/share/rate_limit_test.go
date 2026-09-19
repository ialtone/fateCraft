package share

import (
	"testing"
	"time"
)

func TestRateLimiterResetsAfterWindow(t *testing.T) {
	limiter := newRateLimiter()
	now := time.Unix(100, 0)
	limiter.now = func() time.Time { return now }
	if !limiter.allow("key", 2, time.Minute) || !limiter.allow("key", 2, time.Minute) {
		t.Fatal("initial requests rejected")
	}
	if limiter.allow("key", 2, time.Minute) {
		t.Fatal("limit was not enforced")
	}
	now = now.Add(time.Minute)
	if !limiter.allow("key", 2, time.Minute) {
		t.Fatal("window did not reset")
	}
}
