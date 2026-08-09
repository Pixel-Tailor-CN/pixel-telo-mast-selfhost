package logging

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const requestIDHeader = "X-Request-ID"

// Access 为 Gin 提供基于 slog 的安全访问日志。
func Access(logger *slog.Logger) gin.HandlerFunc {
	if logger == nil {
		logger = slog.Default()
	}
	return func(c *gin.Context) {
		requestID := normalizedRequestID(c.GetHeader(requestIDHeader))
		c.Header(requestIDHeader, requestID)
		started := time.Now()
		c.Next()

		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		status := c.Writer.Status()
		level := slog.LevelInfo
		if status >= http.StatusInternalServerError {
			level = slog.LevelError
		} else if status >= http.StatusBadRequest {
			level = slog.LevelWarn
		}
		logger.Log(c.Request.Context(), level, "http request completed",
			"method", c.Request.Method,
			"route", route,
			"status", status,
			"latency_ms", time.Since(started).Milliseconds(),
			"request_id", requestID,
		)
	}
}

// Recovery 捕获 Gin Handler panic，并通过 slog 记录不包含请求内容的错误。
func Recovery(logger *slog.Logger) gin.HandlerFunc {
	if logger == nil {
		logger = slog.Default()
	}
	return func(c *gin.Context) {
		defer func() {
			if recovered := recover(); recovered != nil {
				requestID := c.Writer.Header().Get(requestIDHeader)
				logger.ErrorContext(c.Request.Context(), "http request panic recovered",
					"method", c.Request.Method,
					"route", safeRoute(c),
					"request_id", requestID,
					"error_type", fmt.Sprintf("%T", recovered),
				)
				c.AbortWithStatus(http.StatusInternalServerError)
			}
		}()
		c.Next()
	}
}

func normalizedRequestID(value string) string {
	if parsed, err := uuid.Parse(value); err == nil {
		return parsed.String()
	}
	return uuid.NewString()
}

func safeRoute(c *gin.Context) string {
	if route := c.FullPath(); route != "" {
		return route
	}
	return "unmatched"
}
