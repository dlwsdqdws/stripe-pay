package common

import (
	"context"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// RequestLogger 请求日志中间件，记录每个请求的详细信息
func RequestLogger() app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		start := time.Now()
		path := string(c.Path())
		method := string(c.Method())
		clientIP := c.ClientIP()
		userAgent := string(c.UserAgent())

		// 记录请求开始
		zap.L().Info("Request started",
			zap.String("method", method),
			zap.String("path", path),
			zap.String("client_ip", clientIP),
			zap.String("user_agent", userAgent),
			zap.String("request_id", getRequestID(c)),
		)

		// 继续处理请求
		c.Next(ctx)

		// 计算处理时间
		latency := time.Since(start)
		statusCode := c.Response.StatusCode()

		// 记录请求完成
		logLevel := zapcore.InfoLevel
		if statusCode >= 500 {
			logLevel = zapcore.ErrorLevel
		} else if statusCode >= 400 {
			logLevel = zapcore.WarnLevel
		}

		zap.L().Check(logLevel, "Request completed").Write(
			zap.String("method", method),
			zap.String("path", path),
			zap.Int("status_code", statusCode),
			zap.Duration("latency", latency),
			zap.String("client_ip", clientIP),
			zap.String("request_id", getRequestID(c)),
		)
	}
}

// getRequestID 获取或生成请求ID（用于日志追踪）
func getRequestID(c *app.RequestContext) string {
	// 尝试从请求头获取
	requestID := string(c.GetHeader("X-Request-ID"))
	if requestID != "" {
		return requestID
	}

	// 尝试从上下文获取
	if id, ok := c.Get("request_id"); ok {
		if str, ok := id.(string); ok {
			return str
		}
	}

	// 生成新的请求ID
	requestID = generateRequestID()
	c.Set("request_id", requestID)
	return requestID
}

// generateRequestID 生成请求ID
func generateRequestID() string {
	return "REQ-" + time.Now().Format("20060102150405") + "-" + time.Now().Format("000000000")
}

// LogStage 记录处理阶段日志
func LogStage(c *app.RequestContext, stage string, fields ...zap.Field) {
	baseFields := []zap.Field{
		zap.String("stage", stage),
		zap.String("path", string(c.Path())),
		zap.String("method", string(c.Method())),
		zap.String("request_id", getRequestID(c)),
	}
	allFields := append(baseFields, fields...)
	zap.L().Info("Processing stage", allFields...)
}

// LogStageWithLevel 记录处理阶段日志（指定日志级别）
func LogStageWithLevel(c *app.RequestContext, level zapcore.Level, stage string, fields ...zap.Field) {
	baseFields := []zap.Field{
		zap.String("stage", stage),
		zap.String("path", string(c.Path())),
		zap.String("method", string(c.Method())),
		zap.String("request_id", getRequestID(c)),
	}
	allFields := append(baseFields, fields...)
	zap.L().Check(level, "Processing stage").Write(allFields...)
}
