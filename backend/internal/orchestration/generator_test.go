package orchestration

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/ialtone/fatecraft/internal/protocol"
	"github.com/ialtone/fatecraft/internal/realtime"
)

type preparedActionStub struct{ commits int }

func (a *preparedActionStub) Commit(context.Context, realtime.EmitFunc) { a.commits++ }

type actionHandlerStub struct {
	prepares int
	action   *preparedActionStub
}

func (h *actionHandlerStub) Prepare(context.Context, uuid.UUID, map[string]any) (PreparedAction, error) {
	h.prepares++
	return h.action, nil
}

func TestLoggedEventUsesBusinessSeverityWithoutChangingClientEvent(t *testing.T) {
	tests := []struct {
		name  string
		level zapcore.Level
	}{
		{name: "info", level: zapcore.InfoLevel},
		{name: "warning", level: zapcore.WarnLevel},
		{name: "error", level: zapcore.ErrorLevel},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			core, logs := observer.New(zapcore.DebugLevel)
			convID := uuid.New()
			event := loggedEventWith(zap.New(core), convID, "用户可见原因", tt.level, "日志消息")

			entries := logs.All()
			if len(entries) != 1 || entries[0].Level != tt.level {
				t.Fatalf("log entries = %v, want one %s entry", entries, tt.level)
			}
			if event.Type != realtime.TypeError || event.ConversationID != convID.String() || event.Error != "用户可见原因" {
				t.Fatalf("client event = %#v", event)
			}
		})
	}
}

func TestActionPreparationDoesNotCommitSideEffects(t *testing.T) {
	prepared := &preparedActionStub{}
	handler := &actionHandlerStub{action: prepared}
	generator := &Generator{actions: map[string]ActionHandler{"trigger_ending": handler}}

	action, err := generator.prepareAction(context.Background(), uuid.New(), &protocol.Action{Type: "trigger_ending"})
	if err != nil {
		t.Fatal(err)
	}
	if handler.prepares != 1 || prepared.commits != 0 {
		t.Fatalf("after Prepare: prepares=%d commits=%d", handler.prepares, prepared.commits)
	}
	action.Commit(context.Background(), func(realtime.Event) {})
	if prepared.commits != 1 {
		t.Fatalf("after Commit: commits=%d, want 1", prepared.commits)
	}
}

func TestRejectedEndingDirectiveRevokesCandidateFacts(t *testing.T) {
	directive := rejectedEndingDirective(errors.New("境界不足"))
	for _, expected := range []string{"境界不足", "不能视为已经发生", "不得触发结局", "不得发起新的判定"} {
		if !strings.Contains(directive, expected) {
			t.Fatalf("directive %q missing %q", directive, expected)
		}
	}
}
