package logging

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// GinLogger 返回一个用 zap 记录 HTTP 访问日志的 gin 中间件，
// 取代 gin.Default() 自带的文本 logger，使请求日志与业务日志同源、可结构化检索。
// 4xx 记为 Warn、5xx 记为 Error、其余 Info。
func GinLogger() gin.HandlerFunc {
	log := Named("http")
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery

		c.Next()

		status := c.Writer.Status()
		fields := []zap.Field{
			zap.Int("status", status),
			zap.String("method", c.Request.Method),
			zap.String("path", path),
			zap.String("query", query),
			zap.String("ip", c.ClientIP()),
			zap.Duration("latency", time.Since(start)),
		}
		if len(c.Errors) > 0 {
			fields = append(fields, zap.String("errors", c.Errors.String()))
		}

		msg := "请求"
		switch {
		case status >= http.StatusInternalServerError:
			log.Error(msg, fields...)
		case status >= http.StatusBadRequest:
			log.Warn(msg, fields...)
		default:
			log.Info(msg, fields...)
		}
	}
}

// GinRecovery 返回一个用 zap 记录 panic 的 gin 恢复中间件，
// 取代 gin.Default() 自带的 Recovery，使 panic 堆栈进入结构化日志。
func GinRecovery() gin.HandlerFunc {
	log := Named("http")
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				log.Error("请求处理 panic",
					zap.Any("error", err),
					zap.String("path", c.Request.URL.Path),
					zap.Stack("stack"),
				)
				c.AbortWithStatus(http.StatusInternalServerError)
			}
		}()
		c.Next()
	}
}
