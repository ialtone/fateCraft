// Package logging 提供全局 zap 日志器：main 启动时 Init 一次，
// 各包通过 L()/Named() 取用。级别与格式由环境变量控制：
//
//	LOG_LEVEL:  debug / info / warn / error（默认 info）
//	LOG_FORMAT: console / json（默认 console，生产建议 json）
//
// 说明：这里选择包级单例而非逐构造函数注入——项目已有 20+ 构造函数，
// 日志器作为横切设施注入会显著加重装配噪音；单例的可测性代价用
// zap.NewNop 兜底（测试中不 Init 时 L() 返回 Nop，不产生输出）。
package logging

import (
	"os"
	"strings"
	"sync/atomic"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// global 持有全局 logger；未 Init 时为 Nop，保证任何时刻 L() 可用。
var global atomic.Pointer[zap.Logger]

func init() {
	global.Store(zap.NewNop())
}

// Init 按级别与格式构建全局 logger 并返回它（供 main 记启动日志与 defer Sync）。
func Init(level, format string) *zap.Logger {
	var lv zapcore.Level
	switch strings.ToLower(level) {
	case "debug":
		lv = zapcore.DebugLevel
	case "warn":
		lv = zapcore.WarnLevel
	case "error":
		lv = zapcore.ErrorLevel
	default:
		lv = zapcore.InfoLevel
	}

	encCfg := zap.NewProductionEncoderConfig()
	encCfg.TimeKey = "ts"
	encCfg.EncodeTime = zapcore.ISO8601TimeEncoder

	var enc zapcore.Encoder
	if strings.ToLower(format) == "json" {
		enc = zapcore.NewJSONEncoder(encCfg)
	} else {
		// console：开发态彩色级别，便于人眼扫读。
		encCfg.EncodeLevel = zapcore.CapitalColorLevelEncoder
		enc = zapcore.NewConsoleEncoder(encCfg)
	}

	core := zapcore.NewCore(enc, zapcore.Lock(os.Stdout), lv)
	logger := zap.New(core, zap.AddCaller())
	global.Store(logger)
	return logger
}

// L 返回全局 logger（未 Init 时为 Nop）。
func L() *zap.Logger {
	return global.Load()
}

// Named 返回带子系统名的 logger，如 logging.Named("roll")。
func Named(name string) *zap.Logger {
	return global.Load().Named(name)
}
