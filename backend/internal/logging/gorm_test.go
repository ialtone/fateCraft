package logging

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

type testSQLStateError struct{ code string }

func (e testSQLStateError) Error() string    { return "database constraint" }
func (e testSQLStateError) SQLState() string { return e.code }

func observedGormLogger() (*gormZap, *observer.ObservedLogs) {
	core, logs := observer.New(zapcore.DebugLevel)
	return &gormZap{
		log: zap.New(core), level: gormlogger.Info, slowThreshold: 200 * time.Millisecond,
	}, logs
}

func traceSQL(g *gormZap, err error, elapsed time.Duration) {
	g.Trace(context.Background(), time.Now().Add(-elapsed), func() (string, int64) {
		return "SELECT * FROM users WHERE email = $1", 1
	}, err)
}

func TestGormLoggerFiltersParameters(t *testing.T) {
	g, _ := observedGormLogger()
	sql, params := g.ParamsFilter(context.Background(), "SELECT * FROM users WHERE email = $1", "secret@example.com")
	if sql != "SELECT * FROM users WHERE email = $1" || params != nil {
		t.Fatalf("ParamsFilter() = (%q, %v), want parameterized SQL and nil params", sql, params)
	}
}

func TestGormLoggerClassifiesTraceLevels(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		elapsed time.Duration
		level   zapcore.Level
		message string
	}{
		{name: "not found", err: gorm.ErrRecordNotFound, level: zapcore.DebugLevel, message: "SQL 未命中"},
		{name: "cancelled", err: context.Canceled, level: zapcore.DebugLevel, message: "SQL 已取消"},
		{name: "constraint", err: testSQLStateError{code: "23505"}, level: zapcore.WarnLevel, message: "SQL 约束拒绝"},
		{name: "failure", err: errors.New("connection lost"), level: zapcore.ErrorLevel, message: "SQL 执行失败"},
		{name: "slow", elapsed: time.Second, level: zapcore.WarnLevel, message: "慢查询"},
		{name: "normal", level: zapcore.DebugLevel, message: "SQL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, logs := observedGormLogger()
			traceSQL(g, tt.err, tt.elapsed)
			entries := logs.All()
			if len(entries) != 1 {
				t.Fatalf("logged %d entries, want 1", len(entries))
			}
			if entries[0].Level != tt.level || entries[0].Message != tt.message {
				t.Fatalf("entry = (%s, %q), want (%s, %q)", entries[0].Level, entries[0].Message, tt.level, tt.message)
			}
		})
	}
}

func TestGormLoggerHonorsLogMode(t *testing.T) {
	g, logs := observedGormLogger()
	g.level = gormlogger.Error
	traceSQL(g, testSQLStateError{code: "23505"}, 0)
	if logs.Len() != 0 {
		t.Fatalf("Warn trace should be suppressed in Error mode, got %d entries", logs.Len())
	}
}
