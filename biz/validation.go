package biz

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"
)

// 验证常量
const (
	MaxUserIDLength      = 128
	MinUserIDLength      = 1
	MaxDescriptionLength = 500
	MaxURLLength         = 2048
	MinAmount            = 1        // 最小金额：1分
	MaxAmount            = 10000000 // 最大金额：100000元（10000000分）
)

// 白名单
var (
	// 允许的客户端类型
	allowedClients = map[string]bool{
		"web":    true,
		"mobile": true,
	}

	// 允许的币种
	allowedCurrencies = map[string]bool{
		"hkd": true,
		"usd": true,
		"cny": true,
		"eur": true,
		"gbp": true,
		"jpy": true,
	}

	// 允许的退款原因
	allowedRefundReasons = map[string]bool{
		"duplicate":             true,
		"fraudulent":            true,
		"requested_by_customer": true,
	}

	// 允许的支付状态
	allowedPaymentStatuses = map[string]bool{
		"succeeded":  true,
		"failed":     true,
		"canceled":   true,
		"pending":    true,
		"processing": true,
	}

	// user_id格式：允许字母、数字、下划线、连字符、点号，以及中文字符（简体/繁体）
	// \p{Han} 匹配所有汉字（包括简体中文和繁体中文）
	userIDPattern = regexp.MustCompile(`^[\p{L}\p{N}._-]+$`)

	// Stripe PaymentIntent ID格式：pi_开头，后跟24个字符
	stripePaymentIntentPattern = regexp.MustCompile(`^pi_[a-zA-Z0-9]{24}$`)
)

// ValidationError 验证错误
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation error on field '%s': %s", e.Field, e.Message)
}

// ValidateUserID 验证用户ID格式
func ValidateUserID(userID string) error {
	if userID == "" {
		return &ValidationError{Field: "user_id", Message: "user_id is required"}
	}

	// 检查长度
	length := utf8.RuneCountInString(userID)
	if length < MinUserIDLength || length > MaxUserIDLength {
		return &ValidationError{
			Field:   "user_id",
			Message: fmt.Sprintf("user_id length must be between %d and %d characters", MinUserIDLength, MaxUserIDLength),
		}
	}

	// 检查格式（防止SQL注入和XSS）
	// 允许：字母（包括中文）、数字、下划线、点号、连字符
	if !userIDPattern.MatchString(userID) {
		return &ValidationError{
			Field:   "user_id",
			Message: "user_id can only contain letters (including Chinese), numbers, underscores, dots, and hyphens",
		}
	}

	return nil
}

// ValidateDescription 验证描述字段
func ValidateDescription(description string) error {
	if description == "" {
		return nil // 描述是可选的
	}

	// 检查长度
	length := utf8.RuneCountInString(description)
	if length > MaxDescriptionLength {
		return &ValidationError{
			Field:   "description",
			Message: fmt.Sprintf("description length must not exceed %d characters", MaxDescriptionLength),
		}
	}

	// 检查是否包含潜在的危险字符（防止XSS）
	// 允许大部分字符，但检查一些明显的XSS尝试
	dangerousPatterns := []string{
		"<script",
		"javascript:",
		"onerror=",
		"onload=",
		"onclick=",
	}
	lowerDesc := strings.ToLower(description)
	for _, pattern := range dangerousPatterns {
		if strings.Contains(lowerDesc, pattern) {
			return &ValidationError{
				Field:   "description",
				Message: "description contains potentially dangerous content",
			}
		}
	}

	return nil
}

// ValidateURL 验证URL格式
func ValidateURL(urlStr string) error {
	if urlStr == "" {
		return nil // URL是可选的
	}

	// 检查长度
	if len(urlStr) > MaxURLLength {
		return &ValidationError{
			Field:   "return_url",
			Message: fmt.Sprintf("URL length must not exceed %d characters", MaxURLLength),
		}
	}

	// 使用标准库验证URL格式
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return &ValidationError{
			Field:   "return_url",
			Message: "invalid URL format",
		}
	}

	// 只允许http和https协议
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return &ValidationError{
			Field:   "return_url",
			Message: "URL must use http or https protocol",
		}
	}

	return nil
}

// ValidateClient 验证客户端类型（白名单）
func ValidateClient(client string) error {
	if client == "" {
		return nil // 客户端是可选的，有默认值
	}

	clientLower := strings.ToLower(strings.TrimSpace(client))
	if !allowedClients[clientLower] {
		return &ValidationError{
			Field:   "client",
			Message: fmt.Sprintf("client must be one of: %s", strings.Join(getAllowedClients(), ", ")),
		}
	}

	return nil
}

// ValidateCurrency 验证币种（白名单）
func ValidateCurrency(currency string) error {
	if currency == "" {
		return nil // 币种是可选的，有默认值
	}

	currencyLower := strings.ToLower(strings.TrimSpace(currency))
	if !allowedCurrencies[currencyLower] {
		return &ValidationError{
			Field:   "currency",
			Message: fmt.Sprintf("currency must be one of: %s", strings.Join(getAllowedCurrencies(), ", ")),
		}
	}

	return nil
}

// ValidateAmount 验证金额范围
func ValidateAmount(amount int64) error {
	if amount < MinAmount {
		return &ValidationError{
			Field:   "amount",
			Message: fmt.Sprintf("amount must be at least %d (0.01 in smallest currency unit)", MinAmount),
		}
	}

	if amount > MaxAmount {
		return &ValidationError{
			Field:   "amount",
			Message: fmt.Sprintf("amount must not exceed %d (100000 in smallest currency unit)", MaxAmount),
		}
	}

	return nil
}

// ValidatePaymentIntentID 验证Stripe PaymentIntent ID格式
func ValidatePaymentIntentID(paymentIntentID string) error {
	if paymentIntentID == "" {
		return &ValidationError{Field: "payment_intent_id", Message: "payment_intent_id is required"}
	}

	if !stripePaymentIntentPattern.MatchString(paymentIntentID) {
		return &ValidationError{
			Field:   "payment_intent_id",
			Message: "invalid payment_intent_id format (must start with 'pi_' followed by 24 characters)",
		}
	}

	return nil
}

// ValidateRefundReason 验证退款原因（白名单）
func ValidateRefundReason(reason string) error {
	if reason == "" {
		return nil // 退款原因是可选的
	}

	reasonLower := strings.ToLower(strings.TrimSpace(reason))
	if !allowedRefundReasons[reasonLower] {
		return &ValidationError{
			Field:   "reason",
			Message: fmt.Sprintf("reason must be one of: %s", strings.Join(getAllowedRefundReasons(), ", ")),
		}
	}

	return nil
}

// ValidatePaymentStatus 验证支付状态（白名单）
func ValidatePaymentStatus(status string) error {
	if status == "" {
		return &ValidationError{Field: "status", Message: "status is required"}
	}

	statusLower := strings.ToLower(strings.TrimSpace(status))
	if !allowedPaymentStatuses[statusLower] {
		return &ValidationError{
			Field:   "status",
			Message: fmt.Sprintf("status must be one of: %s", strings.Join(getAllowedPaymentStatuses(), ", ")),
		}
	}

	return nil
}

// ValidateReceiptData 验证Apple收据数据
func ValidateReceiptData(receiptData string) error {
	if receiptData == "" {
		return &ValidationError{Field: "receipt_data", Message: "receipt_data is required"}
	}

	// 收据数据应该是base64编码的，检查基本格式
	if len(receiptData) < 10 {
		return &ValidationError{
			Field:   "receipt_data",
			Message: "receipt_data appears to be invalid (too short)",
		}
	}

	// 检查长度上限（防止DoS）
	if len(receiptData) > 100000 { // 100KB上限
		return &ValidationError{
			Field:   "receipt_data",
			Message: "receipt_data is too long (maximum 100KB)",
		}
	}

	return nil
}

// 辅助函数：获取允许的值列表（用于错误消息）
func getAllowedClients() []string {
	clients := make([]string, 0, len(allowedClients))
	for k := range allowedClients {
		clients = append(clients, k)
	}
	return clients
}

func getAllowedCurrencies() []string {
	currencies := make([]string, 0, len(allowedCurrencies))
	for k := range allowedCurrencies {
		currencies = append(currencies, k)
	}
	return currencies
}

func getAllowedRefundReasons() []string {
	reasons := make([]string, 0, len(allowedRefundReasons))
	for k := range allowedRefundReasons {
		reasons = append(reasons, k)
	}
	return reasons
}

func getAllowedPaymentStatuses() []string {
	statuses := make([]string, 0, len(allowedPaymentStatuses))
	for k := range allowedPaymentStatuses {
		statuses = append(statuses, k)
	}
	return statuses
}
