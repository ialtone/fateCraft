package logging

import (
	"context"
	"errors"
	"time"

	"go.uber.org/zap"
	gormlogger "gorm.io/gorm/logger"
)

// gormZap 把 GORM 的日志桥接到 zap，使 SQL 错误/慢查询与业务日志同源。
type gormZap struct {
	log           *zap.Logger
	level         gormlogger.LogLevel
	slowThreshold time.Duration
}

// GormLogger 返回 zap 实现的 GORM 日志器：
// SQL 错误记 Error、慢查询（>200ms）记 Warn、其余语句仅在 debug 级别可见。
func GormLogger() gormlogger.Interface {
	return &gormZap{
		log:           Named("gorm"),
		level:         gormlogger.Warn,
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

func (g *gormZap) Trace(_ context.Context, begin time.Time, fc func() (string, int64), err error) {
	if g.level <= gormlogger.Silent {
		return
	}
	elapsed := time.Since(begin)
	sql, rows := fc()
	fields := []zap.Field{
		zap.Duration("elapsed", elapsed),
		zap.Int64("rows", rows),
		zap.String("sql", sql),
	}
	switch {
	case err != nil && !errors.Is(err, gormlogger.ErrRecordNotFound):
		// ErrRecordNotFound 是常规业务分支（上层翻译为 404），不按错误记。
		g.log.Error("SQL 执行失败", append(fields, zap.Error(err))...)
	case g.slowThreshold > 0 && elapsed > g.slowThreshold:
		g.log.Warn("慢查询", fields...)
	default:
		g.log.Debug("SQL", fields...)
	}
}
