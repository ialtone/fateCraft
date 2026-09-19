package orchestration

import (
	"testing"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/ialtone/fatecraft/internal/realtime"
)

func TestLoggedEventUsesBusinessSeverityWithoutChangingClientEvent(t *testing.T) {
	tests := []struct {
		name      string
		level     zapcore.Level
		eventType string
	}{
		{name: "info", level: zapcore.InfoLevel, eventType: realtime.TypeNotice},
		{name: "warning", level: zapcore.WarnLevel, eventType: realtime.TypeError},
		{name: "error", level: zapcore.ErrorLevel, eventType: realtime.TypeError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			core, logs := observer.New(zapcore.DebugLevel)
			convID := uuid.New()
			event := loggedEventWith(zap.New(core), convID, tt.eventType, "用户可见原因", tt.level, "日志消息")

			entries := logs.All()
			if len(entries) != 1 || entries[0].Level != tt.level {
				t.Fatalf("log entries = %v, want one %s entry", entries, tt.level)
			}
			if event.Type != tt.eventType || event.ConversationID != convID.String() || event.Error != "用户可见原因" {
				t.Fatalf("client event = %#v", event)
			}
		})
	}
}
