package models

// 支付相关请求和响应模型

// CreatePaymentRequest 创建支付请求
type CreatePaymentRequest struct {
	UserID      string `json:"user_id" binding:"required"` // 用户ID（必填）
	Description string `json:"description"`                // 描述（可选）
}

// CreateWeChatPaymentRequest 创建微信支付请求
type CreateWeChatPaymentRequest struct {
	UserID      string `json:"user_id" binding:"required"` // 用户ID（必填）
	Description string `json:"description"`                // 可选描述
	ReturnURL   string `json:"return_url"`                 // 可选：支付完成后跳转地址
	Client      string `json:"client"`                     // 可选：web 或 mobile，默认 web
}


// PaymentResponse 支付响应
type PaymentResponse struct {
	ClientSecret    string `json:"client_secret"`
	PaymentID       string `json:"payment_id"`
	PaymentIntentID string `json:"payment_intent_id"`
}

// PricingResponse 定价信息响应
type PricingResponse struct {
	Amount   int64  `json:"amount"`   // 分
	Currency string `json:"currency"` // hkd/usd 等
	Label    string `json:"label"`    // 展示文案，如 HK$59
}

// ConfirmPaymentRequest 确认支付请求
type ConfirmPaymentRequest struct {
	PaymentID string `json:"payment_id"`
}

// UpdatePaymentConfigRequest 更新支付配置请求
type UpdatePaymentConfigRequest struct {
	Amount      int64  `json:"amount" binding:"required"` // 金额（分），必填
	Currency    string `json:"currency"`                  // 币种，可选，默认为 hkd
	Description string `json:"description"`               // 描述，可选
}

// UpdatePaymentStatusRequest 更新支付状态请求
type UpdatePaymentStatusRequest struct {
	PaymentIntentID string `json:"payment_intent_id" binding:"required"`
	Status          string `json:"status" binding:"required"` // succeeded, failed, canceled 等
}

// RefundRequest 退款请求
type RefundRequest struct {
	PaymentIntentID string `json:"payment_intent_id"` // 必填：要退款的 PaymentIntent ID
	Amount          int64  `json:"amount,omitempty"`  // 可选：退款金额（分）。不填则全额退款
	Reason          string `json:"reason,omitempty"`  // 可选：退款原因（duplicate, fraudulent, requested_by_customer）
}

// AppleVerifyRequest Apple内购验证请求
type AppleVerifyRequest struct {
	ReceiptData string `json:"receipt_data"`
	Password    string `json:"password"` // 可选的共享密钥
}

// AppleVerifyResponse Apple内购验证响应
type AppleVerifyResponse struct {
	Status             int `json:"status"`
	Receipt            any `json:"receipt,omitempty"`
	LatestReceiptInfo  any `json:"latest_receipt_info,omitempty"`
	PendingRenewalInfo any `json:"pending_renewal_info,omitempty"`
}
