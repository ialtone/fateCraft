package logging

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// GinLogger 返回一个用 zap 记录 HTTP 访问日志的 gin 中间件，
// 取代 gin.Default() 自带的文本 logger，使请求日志与业务日志同源、可结构化检索。
// Business rejections are informational; security rejections and faults stand out.
func GinLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		ctx := NewTrace(c.Request.Context())
		c.Request = c.Request.WithContext(ctx)
		c.Header("X-Request-ID", TraceID(ctx))

		c.Next()

		status := c.Writer.Status()
		if c.FullPath() == "/api/health" && status < 400 {
			return
		}
		path := c.FullPath()
		if path == "" {
			path = "[unmatched]"
		}
		log := FromContext(c.Request.Context(), "http")
		fields := []zap.Field{
			zap.Int("status", status),
			zap.String("method", c.Request.Method),
			zap.String("path", path),
			zap.String("ip", c.ClientIP()),
			zap.Duration("latency", time.Since(start)),
		}
		if len(c.Errors) > 0 {
			for _, entry := range c.Errors {
				fields = append(fields, zap.String("error", ErrorText(entry.Err)))
			}
		}

		msg := "请求"
		switch {
		case status >= http.StatusInternalServerError:
			log.Error(msg, fields...)
		case status == http.StatusForbidden || status == http.StatusTooManyRequests:
			log.Warn(msg, fields...)
		default:
			log.Info(msg, fields...)
		}
	}
}

// GinRecovery 返回一个用 zap 记录 panic 的 gin 恢复中间件，
// 取代 gin.Default() 自带的 Recovery，使 panic 堆栈进入结构化日志。
func GinRecovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				FromContext(c.Request.Context(), "http").Error("请求处理 panic",
					zap.String("panic_type", fmt.Sprintf("%T", err)),
					zap.String("path", c.FullPath()),
					zap.Stack("stack"),
				)
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "服务暂时不可用", "code": "internal_error", "trace_id": TraceID(c.Request.Context())})
			}
		}()
		c.Next()
	}
}

func safeRequestPath(path string) string {
	for _, prefix := range []string{"/s/", "/shared/", "/api/public/conversation-shares/"} {
		if index := strings.Index(path, prefix); index >= 0 {
			tail := path[index+len(prefix):]
			if slash := strings.IndexByte(tail, '/'); slash >= 0 && tail[slash:] == "/reports" && prefix == "/api/public/conversation-shares/" {
				tail = "/reports"
			} else {
				tail = ""
			}
			return path[:index] + prefix + "[redacted]" + tail
		}
	}
	return path
}
