package logging

import (
	"context"
	"errors"
	"strings"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

var (
	_ gormlogger.Interface = (*gormZap)(nil)
	_ gorm.ParamsFilter    = (*gormZap)(nil)
)

// gormZap 把 GORM 的日志桥接到 zap，使 SQL 错误/慢查询与业务日志同源。
type gormZap struct {
	log           *zap.Logger
	level         gormlogger.LogLevel
	slowThreshold time.Duration
}

// GormLogger 返回 zap 实现的 GORM 日志器：
// SQL 故障记 Error、约束拒绝和慢查询记 Warn、未命中及普通 SQL 仅在 debug 可见。
func GormLogger() gormlogger.Interface {
	return &gormZap{
		log:           Named("gorm"),
		level:         gormlogger.Info,
		slowThreshold: 200 * time.Millisecond,
	}
}

func (g *gormZap) LogMode(level gormlogger.LogLevel) gormlogger.Interface {
	clone := *g
	clone.level = level
	return &clone
}

func (g *gormZap) Info(_ context.Context, msg string, args ...any) {
	if g.level >= gormlogger.Info {
		g.log.Sugar().Infof(msg, args...)
	}
}

func (g *gormZap) Warn(_ context.Context, msg string, args ...any) {
	if g.level >= gormlogger.Warn {
		g.log.Sugar().Warnf(msg, args...)
	}
}

func (g *gormZap) Error(_ context.Context, msg string, args ...any) {
	if g.level >= gormlogger.Error {
		g.log.Sugar().Errorf(msg, args...)
	}
}

// ParamsFilter 让 GORM 保留 SQL 占位符，不把密码、令牌、邮箱或 API Key
// 等绑定参数展开到日志。callbacks 会在调用 Trace 的 fc 中应用此过滤器。
func (g *gormZap) ParamsFilter(_ context.Context, sql string, _ ...any) (string, []any) {
	return sql, nil
}

func (g *gormZap) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	if g.level <= gormlogger.Silent {
		return
	}
	elapsed := time.Since(begin)
	switch {
	case err != nil && errors.Is(err, gormlogger.ErrRecordNotFound):
		g.trace(ctx, gormlogger.Info, zapcore.DebugLevel, "SQL 未命中", elapsed, fc)
	case err != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)):
		g.trace(ctx, gormlogger.Info, zapcore.DebugLevel, "SQL 已取消", elapsed, fc)
	case err != nil && isIntegrityConstraint(err):
		g.trace(ctx, gormlogger.Warn, zapcore.WarnLevel, "SQL 约束拒绝", elapsed, fc, zap.String("error", ErrorText(err)))
	case err != nil:
		g.trace(ctx, gormlogger.Error, zapcore.ErrorLevel, "SQL 执行失败", elapsed, fc, zap.String("error", ErrorText(err)))
	case g.slowThreshold > 0 && elapsed > g.slowThreshold:
		g.trace(ctx, gormlogger.Warn, zapcore.WarnLevel, "慢查询", elapsed, fc)
	default:
		g.trace(ctx, gormlogger.Info, zapcore.DebugLevel, "SQL", elapsed, fc)
	}
}

func (g *gormZap) trace(
	ctx context.Context,
	minimum gormlogger.LogLevel,
	level zapcore.Level,
	message string,
	elapsed time.Duration,
	fc func() (string, int64),
	extra ...zap.Field,
) {
	if g.level < minimum || !g.log.Core().Enabled(level) {
		return
	}
	sql, rows := fc()
	fields := []zap.Field{
		zap.Duration("elapsed", elapsed),
		zap.Int64("rows", rows),
		zap.String("sql", sql),
	}
	fields = append(fields, extra...)
	withContext(g.log, ctx).Log(level, message, fields...)
}

type sqlStateError interface {
	SQLState() string
}

func isIntegrityConstraint(err error) bool {
	if errors.Is(err, gorm.ErrDuplicatedKey) || errors.Is(err, gorm.ErrForeignKeyViolated) {
		return true
	}
	var stateErr sqlStateError
	return errors.As(err, &stateErr) && strings.HasPrefix(stateErr.SQLState(), "23")
}
