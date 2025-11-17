package common

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"go.uber.org/zap"
)

// APIError 统一的API错误响应结构
type APIError struct {
	Code    int    `json:"code"`               // HTTP状态码
	Message string `json:"message"`            // 用户友好的错误消息
	Details string `json:"details,omitempty"`  // 可选的详细信息（仅开发环境）
	ErrorID string `json:"error_id,omitempty"` // 错误ID（用于日志追踪）
}

// Error 实现error接口
func (e *APIError) Error() string {
	return e.Message
}

// 预定义的错误类型
var (
	// 400 Bad Request
	ErrInvalidRequest   = &APIError{Code: consts.StatusBadRequest, Message: "Invalid request"}
	ErrInvalidParameter = &APIError{Code: consts.StatusBadRequest, Message: "Invalid parameter"}
	ErrMissingParameter = &APIError{Code: consts.StatusBadRequest, Message: "Missing required parameter"}
	ErrValidationFailed = &APIError{Code: consts.StatusBadRequest, Message: "Validation failed"}

	// 401 Unauthorized
	ErrUnauthorized = &APIError{Code: consts.StatusUnauthorized, Message: "Unauthorized"}

	// 403 Forbidden
	ErrForbidden = &APIError{Code: consts.StatusForbidden, Message: "Forbidden"}

	// 404 Not Found
	ErrNotFound        = &APIError{Code: consts.StatusNotFound, Message: "Resource not found"}
	ErrPaymentNotFound = &APIError{Code: consts.StatusNotFound, Message: "Payment not found"}
	ErrUserNotFound    = &APIError{Code: consts.StatusNotFound, Message: "User not found"}

	// 409 Conflict
	ErrConflict = &APIError{Code: consts.StatusConflict, Message: "Resource conflict"}

	// 429 Too Many Requests
	ErrTooManyRequests = &APIError{Code: consts.StatusTooManyRequests, Message: "Too many requests"}

	// 500 Internal Server Error
	ErrInternalServer    = &APIError{Code: consts.StatusInternalServerError, Message: "Internal server error"}
	ErrDatabaseError     = &APIError{Code: consts.StatusInternalServerError, Message: "Database error"}
	ErrExternalService   = &APIError{Code: consts.StatusInternalServerError, Message: "External service error"}
	ErrPaymentProcessing = &APIError{Code: consts.StatusInternalServerError, Message: "Payment processing error"}

	// 503 Service Unavailable
	ErrServiceUnavailable = &APIError{Code: consts.StatusServiceUnavailable, Message: "Service unavailable"}
)

// IsDevelopment 检查是否为开发环境（用于显示详细错误信息）
var IsDevelopment = false

// NewAPIError 创建新的API错误
func NewAPIError(code int, message string) *APIError {
	return &APIError{
		Code:    code,
		Message: message,
	}
}

// WithDetails 添加详细信息
func (e *APIError) WithDetails(details string) *APIError {
	e.Details = details
	return e
}

// WithErrorID 添加错误ID
func (e *APIError) WithErrorID(errorID string) *APIError {
	e.ErrorID = errorID
	return e
}

// WrapError 包装错误，将内部错误转换为API错误
func WrapError(err error) *APIError {
	if err == nil {
		return nil
	}

	// 如果已经是APIError，直接返回
	if apiErr, ok := err.(*APIError); ok {
		return apiErr
	}

	// 检查是否是已知的错误类型
	errStr := strings.ToLower(err.Error())

	// 数据库相关错误
	if strings.Contains(errStr, "database") || strings.Contains(errStr, "sql") {
		return ErrDatabaseError.WithDetails(sanitizeError(err))
	}

	// 网络/外部服务错误
	if strings.Contains(errStr, "connection") || strings.Contains(errStr, "timeout") {
		return ErrExternalService.WithDetails(sanitizeError(err))
	}

	// 缺少参数错误（优先检查，因为更具体）
	if strings.Contains(errStr, "missing") ||
		strings.Contains(errStr, "is required") ||
		(strings.Contains(errStr, "required") && strings.Contains(errStr, "user_id")) {
		return ErrMissingParameter.WithDetails(sanitizeError(err))
	}

	// 验证错误
	if strings.Contains(errStr, "validation") ||
		strings.Contains(errStr, "invalid") ||
		strings.Contains(errStr, "format") {
		return ErrValidationFailed.WithDetails(sanitizeError(err))
	}

	// 默认返回内部服务器错误
	return ErrInternalServer.WithDetails(sanitizeError(err))
}

// sanitizeError 清理错误信息，移除敏感信息
func sanitizeError(err error) string {
	if err == nil {
		return ""
	}

	errStr := err.Error()

	// 移除敏感信息
	sensitivePatterns := []string{
		"password",
		"secret",
		"key",
		"token",
		"credential",
		"connection string",
		"database://",
		"mysql://",
		"postgres://",
	}

	// 在开发环境中返回完整错误，生产环境移除敏感信息
	if IsDevelopment {
		return errStr
	}

	// 生产环境：移除包含敏感信息的行
	lines := strings.Split(errStr, "\n")
	var safeLines []string
	for _, line := range lines {
		lineLower := strings.ToLower(line)
		isSensitive := false
		for _, pattern := range sensitivePatterns {
			if strings.Contains(lineLower, pattern) {
				isSensitive = true
				break
			}
		}
		if !isSensitive {
			safeLines = append(safeLines, line)
		}
	}

	return strings.Join(safeLines, "\n")
}

// SendError 发送错误响应
func SendError(c *app.RequestContext, err error) {
	var apiErr *APIError

	if e, ok := err.(*APIError); ok {
		// 已经是APIError
		apiErr = e
	} else {
		// 包装为APIError
		apiErr = WrapError(err)
	}

	// 生成错误ID用于日志追踪
	if apiErr.ErrorID == "" {
		apiErr.ErrorID = generateErrorID()
	}

	// 记录错误日志
	logError(c, apiErr, err)

	// 在生产环境中移除详细信息
	if !IsDevelopment && apiErr.Details != "" {
		apiErr.Details = ""
	}

	// 发送错误响应
	c.JSON(apiErr.Code, apiErr)
}

// SendErrorWithCode 发送指定状态码的错误响应
func SendErrorWithCode(c *app.RequestContext, code int, message string, details ...string) {
	apiErr := NewAPIError(code, message)
	if len(details) > 0 {
		apiErr.Details = details[0]
	}
	apiErr.ErrorID = generateErrorID()

	logError(c, apiErr, errors.New(message))

	if !IsDevelopment && apiErr.Details != "" {
		apiErr.Details = ""
	}

	c.JSON(code, apiErr)
}

// SendSuccess 发送成功响应
func SendSuccess(c *app.RequestContext, data interface{}) {
	c.JSON(consts.StatusOK, data)
}

// logError 记录错误日志
func logError(c *app.RequestContext, apiErr *APIError, originalErr error) {
	fields := []zap.Field{
		zap.String("error_id", apiErr.ErrorID),
		zap.Int("status_code", apiErr.Code),
		zap.String("message", apiErr.Message),
		zap.String("path", string(c.Path())),
		zap.String("method", string(c.Method())),
	}

	if originalErr != nil && originalErr != apiErr {
		fields = append(fields, zap.Error(originalErr))
	}

	if apiErr.Details != "" {
		fields = append(fields, zap.String("details", apiErr.Details))
	}

	// 根据状态码选择日志级别
	switch {
	case apiErr.Code >= 500:
		// 服务器错误：记录为Error级别
		zap.L().Error("Internal server error", fields...)
	case apiErr.Code == 404:
		// 404 Not Found：记录为Info级别（资源不存在是正常情况）
		zap.L().Info("Resource not found", fields...)
	case apiErr.Code == 400:
		// 400 Bad Request：记录为Info级别（客户端请求错误，不是服务器问题）
		zap.L().Info("Client request error", fields...)
	case apiErr.Code >= 400:
		// 其他4xx错误：记录为Warn级别
		zap.L().Warn("Client error", fields...)
	default:
		zap.L().Info("Error response", fields...)
	}
}

// generateErrorID 生成错误ID（简单实现，实际可以使用UUID）
func generateErrorID() string {
	// 使用时间戳生成简单的错误ID
	// 实际项目中可以使用UUID
	return fmt.Sprintf("ERR-%d", time.Now().UnixNano())
}

// ErrorHandler 错误处理中间件
func ErrorHandler() app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		c.Next(ctx)

		// 检查是否有错误
		if len(c.Errors) > 0 {
			// 获取最后一个错误
			err := c.Errors.Last()
			SendError(c, err)
			c.Abort()
		}
	}
}

// RecoveryHandler 恢复中间件，捕获panic
func RecoveryHandler() app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		defer func() {
			if r := recover(); r != nil {
				err, ok := r.(error)
				if !ok {
					err = fmt.Errorf("%v", r)
				}

				zap.L().Error("Panic recovered",
					zap.Error(err),
					zap.String("path", string(c.Path())),
					zap.String("method", string(c.Method())),
					zap.Stack("stack"))

				SendError(c, ErrInternalServer.WithDetails("An unexpected error occurred"))
				c.Abort()
			}
		}()

		c.Next(ctx)
	}
}
