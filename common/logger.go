package common

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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

// generateRequestID 生成请求ID（使用更安全的随机数生成）
func generateRequestID() string {
	// 生成 8 字节随机数
	bytes := make([]byte, 8)
	if _, err := rand.Read(bytes); err != nil {
		// 如果随机数生成失败，使用时间戳作为后备
		return "REQ-" + time.Now().Format("20060102150405") + "-" + time.Now().Format("000000000")
	}
	return "REQ-" + hex.EncodeToString(bytes)
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

// PaymentLogger 支付相关日志记录器
type PaymentLogger struct {
	requestID string
	userID    string
}

// NewPaymentLogger 创建支付日志记录器
func NewPaymentLogger(c *app.RequestContext, userID string) *PaymentLogger {
	return &PaymentLogger{
		requestID: getRequestID(c),
		userID:    userID,
	}
}

// LogPaymentCreated 记录支付创建
func (pl *PaymentLogger) LogPaymentCreated(paymentID, paymentIntentID string, amount int64, currency string, fields ...zap.Field) {
	baseFields := []zap.Field{
		zap.String("event", "payment_created"),
		zap.String("request_id", pl.requestID),
		zap.String("user_id", pl.userID),
		zap.String("payment_id", paymentID),
		zap.String("payment_intent_id", paymentIntentID),
		zap.Int64("amount", amount),
		zap.String("currency", currency),
	}
	allFields := append(baseFields, fields...)
	zap.L().Info("Payment created", allFields...)
}

// LogPaymentSucceeded 记录支付成功
func (pl *PaymentLogger) LogPaymentSucceeded(paymentID, paymentIntentID string, amount int64, currency string, fields ...zap.Field) {
	baseFields := []zap.Field{
		zap.String("event", "payment_succeeded"),
		zap.String("request_id", pl.requestID),
		zap.String("user_id", pl.userID),
		zap.String("payment_id", paymentID),
		zap.String("payment_intent_id", paymentIntentID),
		zap.Int64("amount", amount),
		zap.String("currency", currency),
	}
	allFields := append(baseFields, fields...)
	zap.L().Info("Payment succeeded", allFields...)
}

// LogPaymentFailed 记录支付失败
func (pl *PaymentLogger) LogPaymentFailed(paymentID, paymentIntentID string, reason string, err error, fields ...zap.Field) {
	baseFields := []zap.Field{
		zap.String("event", "payment_failed"),
		zap.String("request_id", pl.requestID),
		zap.String("user_id", pl.userID),
		zap.String("payment_id", paymentID),
		zap.String("payment_intent_id", paymentIntentID),
		zap.String("reason", reason),
	}
	if err != nil {
		baseFields = append(baseFields, zap.Error(err))
	}
	allFields := append(baseFields, fields...)
	zap.L().Error("Payment failed", allFields...)
}

// LogPaymentCanceled 记录支付取消
func (pl *PaymentLogger) LogPaymentCanceled(paymentID, paymentIntentID string, fields ...zap.Field) {
	baseFields := []zap.Field{
		zap.String("event", "payment_canceled"),
		zap.String("request_id", pl.requestID),
		zap.String("user_id", pl.userID),
		zap.String("payment_id", paymentID),
		zap.String("payment_intent_id", paymentIntentID),
	}
	allFields := append(baseFields, fields...)
	zap.L().Warn("Payment canceled", allFields...)
}

// LogPaymentStatusUpdate 记录支付状态更新
func (pl *PaymentLogger) LogPaymentStatusUpdate(paymentIntentID, oldStatus, newStatus string, fields ...zap.Field) {
	baseFields := []zap.Field{
		zap.String("event", "payment_status_updated"),
		zap.String("request_id", pl.requestID),
		zap.String("user_id", pl.userID),
		zap.String("payment_intent_id", paymentIntentID),
		zap.String("old_status", oldStatus),
		zap.String("new_status", newStatus),
	}
	allFields := append(baseFields, fields...)
	zap.L().Info("Payment status updated", allFields...)
}

// LogWebhookReceived 记录 Webhook 接收
func LogWebhookReceived(eventType, eventID string, fields ...zap.Field) {
	baseFields := []zap.Field{
		zap.String("event", "webhook_received"),
		zap.String("webhook_type", eventType),
		zap.String("webhook_event_id", eventID),
	}
	allFields := append(baseFields, fields...)
	zap.L().Info("Webhook received", allFields...)
}
