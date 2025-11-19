package biz

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"stripe-pay/conf"
	"stripe-pay/db"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v78"
	"github.com/stripe/stripe-go/v78/paymentintent"
	"github.com/stripe/stripe-go/v78/refund"
	"github.com/stripe/stripe-go/v78/webhook"
	"go.uber.org/zap"
)

// Stripe 支付相关结构体
type CreatePaymentRequest struct {
	// 金额与币种改为由后端决定，不再信任前端传入
	UserID      string `json:"user_id" binding:"required"` // 用户ID（必填）
	Description string `json:"description"`                // 描述（可选）
}

// CreateWeChatPaymentRequest 专用于创建微信支付的请求
type CreateWeChatPaymentRequest struct {
	UserID      string `json:"user_id" binding:"required"` // 用户ID（必填）
	Description string `json:"description"`                // 可选描述
	ReturnURL   string `json:"return_url"`                 // 可选：支付完成后跳转地址（前端也可用二维码流程，无需跳转）
	Client      string `json:"client"`                     // 可选：web 或 mobile，默认 web
}


// getCurrentPricing 返回当前售卖价格（单位：分）与币种
// 从数据库读取支付金额配置
func getCurrentPricing() (amount int64, currency string, label string) {
	// 从数据库读取配置
	if db.DB != nil {
		config, err := db.GetPaymentConfig("hkd")
		if err == nil && config != nil {
			amount = config.Amount
			currency = config.Currency
			// 生成显示标签，如 HK$59
			label = "HK$" + formatAmount(amount)
			return
		}
		zap.L().Warn("Failed to get payment config from database, using default", zap.Error(err))
	}

	// 如果数据库读取失败，使用默认值
	amount = 5900
	currency = "hkd"
	label = "HK$59"
	return
}

// formatAmount 格式化金额（分转元，保留2位小数）
func formatAmount(amount int64) string {
	dollars := float64(amount) / 100.0
	if dollars == float64(int64(dollars)) {
		return formatInt(int64(dollars))
	}
	return formatFloat(dollars)
}

// formatInt 格式化整数
func formatInt(n int64) string {
	return strconv.FormatInt(n, 10)
}

// formatFloat 格式化浮点数（保留2位小数）
func formatFloat(f float64) string {
	return strconv.FormatFloat(f, 'f', 2, 64)
}

type PaymentResponse struct {
	ClientSecret    string `json:"client_secret"`
	PaymentID       string `json:"payment_id"`
	PaymentIntentID string `json:"payment_intent_id"`
}

// PricingResponse 提供给前端展示的定价信息
type PricingResponse struct {
	Amount   int64  `json:"amount"`   // 分
	Currency string `json:"currency"` // hkd/usd 等
	Label    string `json:"label"`    // 展示文案，如 HK$59
}

type ConfirmPaymentRequest struct {
	PaymentID string `json:"payment_id"`
}

// Apple 内购相关结构体
type AppleVerifyRequest struct {
	ReceiptData string `json:"receipt_data"`
	Password    string `json:"password"` // 可选的共享密钥
}

type AppleVerifyResponse struct {
	Status             int `json:"status"`
	Receipt            any `json:"receipt,omitempty"`
	LatestReceiptInfo  any `json:"latest_receipt_info,omitempty"`
	PendingRenewalInfo any `json:"pending_renewal_info,omitempty"`
}

// paymentStore 已移除，所有支付状态查询从数据库读取

// getIdempotencyKey 从请求中获取幂等性密钥
// 优先从Header获取，如果没有则从请求体获取
func getIdempotencyKey(c *app.RequestContext) string {
	// 优先从Header获取（标准做法）
	key := string(c.GetHeader("Idempotency-Key"))
	if key != "" {
		return key
	}
	// 也可以从X-Idempotency-Key Header获取
	key = string(c.GetHeader("X-Idempotency-Key"))
	if key != "" {
		return key
	}
	return ""
}

func CreateStripePayment(ctx context.Context, c *app.RequestContext) {
	var req CreatePaymentRequest
	if err := c.BindAndValidate(&req); err != nil {
		zap.L().Error("Failed to bind request", zap.Error(err))
		c.JSON(consts.StatusBadRequest, utils.H{"error": "Invalid request"})
		return
	}

	// 输入验证增强
	if err := ValidateUserID(req.UserID); err != nil {
		zap.L().Error("Invalid user_id", zap.Error(err))
		c.JSON(consts.StatusBadRequest, utils.H{"error": err.Error()})
		return
	}
	if err := ValidateDescription(req.Description); err != nil {
		zap.L().Error("Invalid description", zap.Error(err))
		c.JSON(consts.StatusBadRequest, utils.H{"error": err.Error()})
		return
	}

	// 幂等性检查：获取Idempotency Key
	idempotencyKey := getIdempotencyKey(c)
	if idempotencyKey != "" && db.DB != nil {
		existingPayment, err := db.GetPaymentByIdempotencyKey(idempotencyKey)
		if err != nil {
			zap.L().Error("Failed to check idempotency", zap.Error(err))
			// 继续执行，不阻止请求
		} else if existingPayment != nil {
			// 找到已存在的支付记录，返回已存在的支付信息
			zap.L().Info("Duplicate request detected, returning existing payment",
				zap.String("idempotency_key", idempotencyKey),
				zap.String("payment_intent_id", existingPayment.PaymentIntentID))

			// 从Stripe获取最新的PaymentIntent信息（包含client_secret）
			cfg := conf.GetConf()
			stripe.Key = cfg.Stripe.SecretKey
			intent, err := paymentintent.Get(existingPayment.PaymentIntentID, nil)
			if err != nil {
				zap.L().Warn("Failed to get payment intent from Stripe, returning cached data", zap.Error(err))
				// 如果获取失败，返回数据库中的信息
				c.JSON(consts.StatusOK, PaymentResponse{
					ClientSecret:    "", // 无法获取client_secret
					PaymentID:       existingPayment.PaymentID,
					PaymentIntentID: existingPayment.PaymentIntentID,
				})
			} else {
				// 成功获取，返回完整的支付信息
				c.JSON(consts.StatusOK, PaymentResponse{
					ClientSecret:    intent.ClientSecret,
					PaymentID:       existingPayment.PaymentID,
					PaymentIntentID: intent.ID,
				})
			}
			return
		}
	}

	// 先检查用户是否已经支付成功过（30天内有效）
	if db.DB != nil {
		userInfo, err := db.GetUserPaymentInfo(req.UserID)
		if err == nil && userInfo != nil && userInfo.HasPaid {
			// 检查上次支付时间是否在30天内
			if userInfo.LastPaymentAt != nil {
				daysSinceLastPayment := time.Since(*userInfo.LastPaymentAt).Hours() / 24
				if daysSinceLastPayment <= 30 {
					zap.L().Info("User already paid within 30 days, skipping payment creation",
						zap.String("user_id", req.UserID),
						zap.Float64("days_since", daysSinceLastPayment))
					c.JSON(consts.StatusOK, utils.H{
						"already_paid":   true,
						"message":        "用户已支付成功，无需重复支付",
						"user_info":      userInfo,
						"days_remaining": int(30 - daysSinceLastPayment),
					})
					return
				} else {
					zap.L().Info("User payment expired, need to pay again",
						zap.String("user_id", req.UserID),
						zap.Float64("days_since", daysSinceLastPayment))
					// 支付已过期，继续创建新支付
				}
			} else {
				// 没有上次支付时间，继续创建新支付
				zap.L().Info("User has paid but no last payment time, continuing", zap.String("user_id", req.UserID))
			}
		}
	}

	cfg := conf.GetConf()
	stripe.Key = cfg.Stripe.SecretKey

	// 使用后端定义的定价（示例：HKD 59）
	defaultAmount, defaultCurrency, _ := getCurrentPricing()

	// 创建 Payment Intent
	params := &stripe.PaymentIntentParams{
		Amount:   stripe.Int64(defaultAmount),
		Currency: stripe.String(defaultCurrency),
		Metadata: map[string]string{
			"user_id":     req.UserID,
			"description": req.Description,
		},
		// 明确指定支付方式类型，包含 Apple Pay
		PaymentMethodTypes: stripe.StringSlice([]string{"card", "apple_pay"}),
	}

	// 如果提供了Idempotency Key，传递给Stripe（Stripe也支持幂等性）
	if idempotencyKey != "" {
		params.IdempotencyKey = stripe.String(idempotencyKey)
	}

	intent, err := paymentintent.New(params)
	if err != nil {
		zap.L().Error("Failed to create payment intent", zap.Error(err))
		c.JSON(consts.StatusInternalServerError, utils.H{"error": "Failed to create payment intent"})
		return
	}

	// 生成 paymentID
	paymentID := uuid.New().String()

	// 保存到数据库
	if db.DB != nil {
		metadata := map[string]string{
			"user_id":     req.UserID,
			"description": req.Description,
		}
		zap.L().Info("Saving payment to database",
			zap.String("payment_intent_id", intent.ID),
			zap.String("idempotency_key", idempotencyKey))

		err = db.SavePaymentWithMetadata(
			intent.ID,
			paymentID,
			idempotencyKey, // 保存幂等性密钥
			req.UserID,
			intent.Amount,
			string(intent.Currency),
			string(intent.Status),
			"card", // 默认支付方式，实际可能通过 Apple Pay 等
			req.Description,
			metadata,
		)
		if err != nil {
			// 检查是否是重复的idempotency_key（并发情况）
			if dupErr, ok := err.(*db.DuplicateIdempotencyKeyError); ok {
				zap.L().Info("Concurrent request with same idempotency_key detected",
					zap.String("idempotency_key", dupErr.Key))
				// 查询已存在的支付记录并返回
				existingPayment, queryErr := db.GetPaymentByIdempotencyKey(dupErr.Key)
				if queryErr == nil && existingPayment != nil {
					// 从Stripe获取最新的PaymentIntent信息
					intent, getErr := paymentintent.Get(existingPayment.PaymentIntentID, nil)
					if getErr == nil {
						c.JSON(consts.StatusOK, PaymentResponse{
							ClientSecret:    intent.ClientSecret,
							PaymentID:       existingPayment.PaymentID,
							PaymentIntentID: intent.ID,
						})
						return
					}
				}
			}
			zap.L().Warn("Failed to save payment to database", zap.Error(err))
		}
	}

	zap.L().Info("Payment intent created", zap.String("payment_id", paymentID))

	c.JSON(consts.StatusOK, PaymentResponse{
		ClientSecret:    intent.ClientSecret,
		PaymentID:       paymentID,
		PaymentIntentID: intent.ID,
	})
}

// CreateStripeWeChatPayment 创建 Stripe WeChat Pay 的 PaymentIntent
func CreateStripeWeChatPayment(ctx context.Context, c *app.RequestContext) {
	var req CreateWeChatPaymentRequest
	if err := c.BindAndValidate(&req); err != nil {
		zap.L().Error("Failed to bind request", zap.Error(err))
		c.JSON(consts.StatusBadRequest, utils.H{"error": "Invalid request"})
		return
	}

	// 输入验证增强
	if err := ValidateUserID(req.UserID); err != nil {
		zap.L().Error("Invalid user_id", zap.Error(err))
		c.JSON(consts.StatusBadRequest, utils.H{"error": err.Error()})
		return
	}
	if err := ValidateDescription(req.Description); err != nil {
		zap.L().Error("Invalid description", zap.Error(err))
		c.JSON(consts.StatusBadRequest, utils.H{"error": err.Error()})
		return
	}
	if err := ValidateURL(req.ReturnURL); err != nil {
		zap.L().Error("Invalid return_url", zap.Error(err))
		c.JSON(consts.StatusBadRequest, utils.H{"error": err.Error()})
		return
	}
	if err := ValidateClient(req.Client); err != nil {
		zap.L().Error("Invalid client", zap.Error(err))
		c.JSON(consts.StatusBadRequest, utils.H{"error": err.Error()})
		return
	}

	// 幂等性检查：获取Idempotency Key
	idempotencyKey := getIdempotencyKey(c)
	if idempotencyKey != "" && db.DB != nil {
		existingPayment, err := db.GetPaymentByIdempotencyKey(idempotencyKey)
		if err != nil {
			zap.L().Error("Failed to check idempotency", zap.Error(err))
			// 继续执行，不阻止请求
		} else if existingPayment != nil {
			// 找到已存在的支付记录，返回已存在的支付信息
			zap.L().Info("Duplicate request detected, returning existing payment",
				zap.String("idempotency_key", idempotencyKey),
				zap.String("payment_intent_id", existingPayment.PaymentIntentID))
			c.JSON(consts.StatusOK, utils.H{
				"client_secret":     "", // 已存在的支付需要从Stripe获取
				"payment_intent_id": existingPayment.PaymentIntentID,
				"status":            existingPayment.Status,
				"message":           "返回已存在的支付记录",
			})
			return
		}
	}

	// 先检查用户是否已经支付成功过（30天内有效）
	if db.DB != nil {
		userInfo, err := db.GetUserPaymentInfo(req.UserID)
		if err == nil && userInfo != nil && userInfo.HasPaid {
			// 检查上次支付时间是否在30天内
			if userInfo.LastPaymentAt != nil {
				daysSinceLastPayment := time.Since(*userInfo.LastPaymentAt).Hours() / 24
				if daysSinceLastPayment <= 30 {
					zap.L().Info("User already paid within 30 days, skipping wechat payment creation",
						zap.String("user_id", req.UserID),
						zap.Float64("days_since", daysSinceLastPayment))
					c.JSON(consts.StatusOK, utils.H{
						"already_paid":   true,
						"message":        "用户已支付成功，无需重复支付",
						"user_info":      userInfo,
						"days_remaining": int(30 - daysSinceLastPayment),
					})
					return
				} else {
					zap.L().Info("User payment expired, need to pay again",
						zap.String("user_id", req.UserID),
						zap.Float64("days_since", daysSinceLastPayment))
					// 支付已过期，继续创建新支付
				}
			} else {
				// 没有上次支付时间，继续创建新支付
				zap.L().Info("User has paid but no last payment time, continuing", zap.String("user_id", req.UserID))
			}
		}
	}

	cfg := conf.GetConf()
	stripe.Key = cfg.Stripe.SecretKey

	amount, currency, _ := getCurrentPricing()

	client := strings.ToLower(strings.TrimSpace(req.Client))
	if client == "" {
		client = "web"
	}

	params := &stripe.PaymentIntentParams{
		Amount:             stripe.Int64(amount),
		Currency:           stripe.String(currency),
		PaymentMethodTypes: stripe.StringSlice([]string{"wechat_pay"}),
		// 不立即确认，让前端使用 Stripe.js 确认以生成二维码
		Metadata: map[string]string{
			"user_id":     req.UserID,
			"description": req.Description,
		},
		PaymentMethodOptions: &stripe.PaymentIntentPaymentMethodOptionsParams{
			WeChatPay: &stripe.PaymentIntentPaymentMethodOptionsWeChatPayParams{
				Client: stripe.String(client), // web 或 mobile
			},
		},
	}

	// 如果设置了 ReturnURL，在创建时设置
	if req.ReturnURL != "" {
		params.ReturnURL = stripe.String(req.ReturnURL)
	}

	intent, err := paymentintent.New(params)
	if err != nil {
		zap.L().Error("Failed to create wechat payment intent", zap.Error(err))
		stripeErr := ""
		if stripeErrObj, ok := err.(*stripe.Error); ok {
			stripeErr = stripeErrObj.Error()
		}
		c.JSON(consts.StatusInternalServerError, utils.H{
			"error":        "Failed to create payment intent",
			"stripe_error": stripeErr,
			"message":      "请确认 Stripe 账户已启用 WeChat Pay，且账户地区支持微信支付",
		})
		return
	}

	// 保存到数据库
	if db.DB != nil {
		metadata := map[string]string{
			"user_id":     req.UserID,
			"description": req.Description,
			"client":      client,
		}
		err = db.SavePaymentWithMetadata(
			intent.ID,
			uuid.New().String(),
			idempotencyKey, // 保存幂等性密钥
			req.UserID,
			intent.Amount,
			string(intent.Currency),
			string(intent.Status),
			"wechat_pay",
			req.Description,
			metadata,
		)
		if err != nil {
			zap.L().Warn("Failed to save wechat payment to database", zap.Error(err))
		}
	}

	// 返回 client_secret 给前端，前端需要使用 Stripe.js confirmPayment 来确认
	// 确认后会生成 next_action（二维码）
	c.JSON(consts.StatusOK, utils.H{
		"client_secret":     intent.ClientSecret,
		"payment_intent_id": intent.ID,
		"status":            intent.Status,
		"message":           "请使用 Stripe.js 在前端确认支付以生成二维码",
	})
}

func ConfirmStripePayment(ctx context.Context, c *app.RequestContext) {
	var req ConfirmPaymentRequest
	if err := c.BindAndValidate(&req); err != nil {
		c.JSON(consts.StatusBadRequest, utils.H{"error": "Invalid request"})
		return
	}

	// 输入验证增强
	if err := ValidatePaymentIntentID(req.PaymentID); err != nil {
		zap.L().Error("Invalid payment_id", zap.Error(err))
		c.JSON(consts.StatusBadRequest, utils.H{"error": err.Error()})
		return
	}

	cfg := conf.GetConf()
	stripe.Key = cfg.Stripe.SecretKey

	// 获取 Payment Intent
	intent, err := paymentintent.Get(req.PaymentID, nil)
	if err != nil {
		zap.L().Error("Failed to get payment intent", zap.Error(err))
		c.JSON(consts.StatusNotFound, utils.H{"error": "Payment not found"})
		return
	}

	c.JSON(consts.StatusOK, utils.H{
		"payment_id": intent.ID,
		"status":     intent.Status,
		"amount":     intent.Amount,
		"currency":   intent.Currency,
	})
}

func StripeWebhook(ctx context.Context, c *app.RequestContext) {
	cfg := conf.GetConf()

	// 读取请求体
	body, err := io.ReadAll(c.Request.BodyStream())
	if err != nil {
		zap.L().Error("Failed to read request body", zap.Error(err))
		c.JSON(consts.StatusBadRequest, utils.H{"error": "Invalid request body"})
		return
	}

	// 获取签名头
	signatureBytes := c.GetHeader("Stripe-Signature")
	if len(signatureBytes) == 0 {
		zap.L().Error("Missing Stripe-Signature header")
		c.JSON(consts.StatusBadRequest, utils.H{"error": "Missing signature"})
		return
	}
	signature := string(signatureBytes)

	// 使用Stripe官方库验证签名
	endpointSecret := cfg.Stripe.WebhookSecret
	if endpointSecret == "" {
		zap.L().Error("Webhook secret not configured")
		c.JSON(consts.StatusInternalServerError, utils.H{"error": "Webhook secret not configured"})
		return
	}

	event, err := webhook.ConstructEvent(body, signature, endpointSecret)
	if err != nil {
		zap.L().Error("Webhook signature verification failed", zap.Error(err))
		c.JSON(consts.StatusBadRequest, utils.H{"error": "Invalid signature"})
		return
	}

	// 处理不同类型的事件
	switch event.Type {
	case "payment_intent.succeeded":
		zap.L().Info("Payment succeeded", zap.String("event_id", event.ID))

		// 解析 PaymentIntent
		var pi stripe.PaymentIntent
		if err := json.Unmarshal(event.Data.Raw, &pi); err != nil {
			zap.L().Error("Failed to parse payment intent", zap.Error(err))
		} else {
			// 更新数据库中的支付状态
			if db.DB != nil {
				// 更新支付历史状态
				if err := db.UpdatePaymentStatus(pi.ID, string(pi.Status)); err != nil {
					zap.L().Warn("Failed to update payment status", zap.Error(err))
				}

				// 获取用户ID（从 metadata 中）
				userID := pi.Metadata["user_id"]
				if userID != "" {
					// 更新用户支付信息
					if err := db.UpdateUserPaymentInfo(userID, pi.Amount); err != nil {
						zap.L().Warn("Failed to update user payment info", zap.Error(err))
					}
				}
			}
		}

	case "payment_intent.payment_failed":
		zap.L().Info("Payment failed", zap.String("event_id", event.ID))

		// 解析 PaymentIntent 并更新状态
		var pi stripe.PaymentIntent
		if err := json.Unmarshal(event.Data.Raw, &pi); err == nil && db.DB != nil {
			db.UpdatePaymentStatus(pi.ID, string(pi.Status))
		}

	case "payment_intent.canceled":
		zap.L().Info("Payment canceled", zap.String("event_id", event.ID))

		// 解析 PaymentIntent 并更新状态
		var pi stripe.PaymentIntent
		if err := json.Unmarshal(event.Data.Raw, &pi); err == nil && db.DB != nil {
			db.UpdatePaymentStatus(pi.ID, string(pi.Status))
		}

	default:
		zap.L().Info("Unhandled event type", zap.String("type", string(event.Type)))
	}

	c.JSON(consts.StatusOK, utils.H{"received": true})
}

// Refund 支付
type RefundRequest struct {
	PaymentIntentID string `json:"payment_intent_id"` // 必填：要退款的 PaymentIntent ID
	Amount          int64  `json:"amount,omitempty"`  // 可选：退款金额（分）。不填则全额退款
	Reason          string `json:"reason,omitempty"`  // 可选：退款原因（duplicate, fraudulent, requested_by_customer）
}

// GetPricing 向前端返回当前应支付的币种与价格
func GetPricing(ctx context.Context, c *app.RequestContext) {
	amount, currency, label := getCurrentPricing()
	c.JSON(consts.StatusOK, PricingResponse{
		Amount:   amount,
		Currency: currency,
		Label:    label,
	})
}

// UpdatePaymentConfigRequest 更新支付配置请求
type UpdatePaymentConfigRequest struct {
	Amount      int64  `json:"amount" binding:"required"` // 金额（分），必填
	Currency    string `json:"currency"`                  // 币种，可选，默认为 hkd
	Description string `json:"description"`               // 描述，可选
}

// UpdatePaymentConfig 更新支付金额配置（管理员接口）
func UpdatePaymentConfig(ctx context.Context, c *app.RequestContext) {
	var req UpdatePaymentConfigRequest
	if err := c.BindAndValidate(&req); err != nil {
		zap.L().Error("Failed to bind request", zap.Error(err))
		c.JSON(consts.StatusBadRequest, utils.H{"error": "Invalid request"})
		return
	}

	// 输入验证增强
	if err := ValidateAmount(req.Amount); err != nil {
		zap.L().Error("Invalid amount", zap.Error(err))
		c.JSON(consts.StatusBadRequest, utils.H{"error": err.Error()})
		return
	}

	// 设置默认币种
	if req.Currency == "" {
		req.Currency = "hkd"
	}

	// 验证币种
	if err := ValidateCurrency(req.Currency); err != nil {
		zap.L().Error("Invalid currency", zap.Error(err))
		c.JSON(consts.StatusBadRequest, utils.H{"error": err.Error()})
		return
	}

	// 验证描述
	if err := ValidateDescription(req.Description); err != nil {
		zap.L().Error("Invalid description", zap.Error(err))
		c.JSON(consts.StatusBadRequest, utils.H{"error": err.Error()})
		return
	}

	// 设置默认描述
	if req.Description == "" {
		req.Description = "支付金额配置"
	}

	// 更新数据库
	if db.DB == nil {
		c.JSON(consts.StatusInternalServerError, utils.H{"error": "Database not available"})
		return
	}

	err := db.UpdatePaymentConfig(req.Currency, req.Amount, req.Description)
	if err != nil {
		zap.L().Error("Failed to update payment config", zap.Error(err))
		c.JSON(consts.StatusInternalServerError, utils.H{"error": "Failed to update payment config"})
		return
	}

	// 返回更新后的配置
	config, err := db.GetPaymentConfig(req.Currency)
	if err != nil {
		zap.L().Warn("Failed to get updated config", zap.Error(err))
		// 即使获取失败，也返回成功（因为更新已经成功）
		c.JSON(consts.StatusOK, utils.H{
			"message":  "Payment config updated successfully",
			"amount":   req.Amount,
			"currency": req.Currency,
		})
		return
	}

	c.JSON(consts.StatusOK, utils.H{
		"message": "Payment config updated successfully",
		"config": utils.H{
			"id":          config.ID,
			"amount":      config.Amount,
			"currency":    config.Currency,
			"description": config.Description,
			"label":       "HK$" + formatAmount(config.Amount),
			"updated_at":  config.UpdatedAt,
		},
	})
}

// GetPaymentConfig 获取当前支付配置（管理员接口）
func GetPaymentConfig(ctx context.Context, c *app.RequestContext) {
	currency := c.Query("currency")
	if currency == "" {
		currency = "hkd"
	}

	if db.DB == nil {
		c.JSON(consts.StatusInternalServerError, utils.H{"error": "Database not available"})
		return
	}

	config, err := db.GetPaymentConfig(currency)
	if err != nil {
		zap.L().Error("Failed to get payment config", zap.Error(err))
		c.JSON(consts.StatusInternalServerError, utils.H{"error": "Failed to get payment config"})
		return
	}

	c.JSON(consts.StatusOK, utils.H{
		"id":          config.ID,
		"amount":      config.Amount,
		"currency":    config.Currency,
		"description": config.Description,
		"label":       "HK$" + formatAmount(config.Amount),
		"created_at":  config.CreatedAt,
		"updated_at":  config.UpdatedAt,
	})
}

// GetUserPaymentInfo 获取用户支付信息
func GetUserPaymentInfo(ctx context.Context, c *app.RequestContext) {
	userID := string(c.Param("user_id"))
	if userID == "" {
		c.JSON(consts.StatusBadRequest, utils.H{"error": "user_id is required"})
		return
	}

	// 输入验证增强
	if err := ValidateUserID(userID); err != nil {
		zap.L().Error("Invalid user_id", zap.Error(err))
		c.JSON(consts.StatusBadRequest, utils.H{"error": err.Error()})
		return
	}

	if db.DB == nil {
		c.JSON(consts.StatusInternalServerError, utils.H{"error": "Database not available"})
		return
	}

	info, err := db.GetUserPaymentInfo(userID)
	if err != nil {
		zap.L().Error("Failed to get user payment info", zap.Error(err))
		c.JSON(consts.StatusInternalServerError, utils.H{"error": "Failed to get user payment info"})
		return
	}

	c.JSON(consts.StatusOK, info)
}

// UpdatePaymentStatusRequest 更新支付状态请求
type UpdatePaymentStatusRequest struct {
	PaymentIntentID string `json:"payment_intent_id" binding:"required"`
	Status          string `json:"status" binding:"required"` // succeeded, failed, canceled 等
}

// UpdatePaymentStatusFromFrontend 前端支付成功后调用此接口更新状态
func UpdatePaymentStatusFromFrontend(ctx context.Context, c *app.RequestContext) {
	var req UpdatePaymentStatusRequest
	if err := c.BindAndValidate(&req); err != nil {
		c.JSON(consts.StatusBadRequest, utils.H{"error": "Invalid request"})
		return
	}

	// 输入验证增强
	if err := ValidatePaymentIntentID(req.PaymentIntentID); err != nil {
		zap.L().Error("Invalid payment_intent_id", zap.Error(err))
		c.JSON(consts.StatusBadRequest, utils.H{"error": err.Error()})
		return
	}
	if err := ValidatePaymentStatus(req.Status); err != nil {
		zap.L().Error("Invalid status", zap.Error(err))
		c.JSON(consts.StatusBadRequest, utils.H{"error": err.Error()})
		return
	}

	cfg := conf.GetConf()
	stripe.Key = cfg.Stripe.SecretKey

	// 从 Stripe 获取最新的 PaymentIntent 状态
	intent, err := paymentintent.Get(req.PaymentIntentID, nil)
	if err != nil {
		zap.L().Error("Failed to get payment intent", zap.Error(err))
		c.JSON(consts.StatusNotFound, utils.H{"error": "Payment intent not found"})
		return
	}

	// 使用 Stripe 返回的实际状态
	actualStatus := string(intent.Status)

	// 更新数据库
	if db.DB != nil {
		// 更新支付历史状态
		if err := db.UpdatePaymentStatus(req.PaymentIntentID, actualStatus); err != nil {
			zap.L().Warn("Failed to update payment status", zap.Error(err))
		}

		// 如果支付成功，更新用户支付信息
		if actualStatus == "succeeded" {
			userID := intent.Metadata["user_id"]
			if userID != "" {
				if err := db.UpdateUserPaymentInfo(userID, intent.Amount); err != nil {
					zap.L().Warn("Failed to update user payment info", zap.Error(err))
				}
			}
		}
	}

	c.JSON(consts.StatusOK, utils.H{
		"payment_intent_id": req.PaymentIntentID,
		"status":            actualStatus,
		"message":           "Payment status updated",
	})
}

// GetUserPaymentHistory 获取用户支付历史
func GetUserPaymentHistory(ctx context.Context, c *app.RequestContext) {
	userID := string(c.Param("user_id"))
	if userID == "" {
		c.JSON(consts.StatusBadRequest, utils.H{"error": "user_id is required"})
		return
	}

	// 输入验证增强
	if err := ValidateUserID(userID); err != nil {
		zap.L().Error("Invalid user_id", zap.Error(err))
		c.JSON(consts.StatusBadRequest, utils.H{"error": err.Error()})
		return
	}

	// 获取 limit 参数（可选，默认 50）
	limit := 50
	if limitStr := c.Query("limit"); limitStr != "" {
		if parsedLimit, err := strconv.Atoi(limitStr); err == nil && parsedLimit > 0 {
			limit = parsedLimit
		}
	}

	if db.DB == nil {
		c.JSON(consts.StatusInternalServerError, utils.H{"error": "Database not available"})
		return
	}

	history, err := db.GetPaymentHistory(userID, limit)
	if err != nil {
		zap.L().Error("Failed to get payment history", zap.Error(err))
		c.JSON(consts.StatusInternalServerError, utils.H{"error": "Failed to get payment history"})
		return
	}

	c.JSON(consts.StatusOK, utils.H{
		"user_id": userID,
		"count":   len(history),
		"history": history,
	})
}

func RefundPayment(ctx context.Context, c *app.RequestContext) {
	var req RefundRequest
	if err := c.BindAndValidate(&req); err != nil || req.PaymentIntentID == "" {
		c.JSON(consts.StatusBadRequest, utils.H{"error": "payment_intent_id required"})
		return
	}

	// 输入验证增强
	if err := ValidatePaymentIntentID(req.PaymentIntentID); err != nil {
		zap.L().Error("Invalid payment_intent_id", zap.Error(err))
		c.JSON(consts.StatusBadRequest, utils.H{"error": err.Error()})
		return
	}
	if req.Amount > 0 {
		if err := ValidateAmount(req.Amount); err != nil {
			zap.L().Error("Invalid amount", zap.Error(err))
			c.JSON(consts.StatusBadRequest, utils.H{"error": err.Error()})
			return
		}
	}
	if err := ValidateRefundReason(req.Reason); err != nil {
		zap.L().Error("Invalid reason", zap.Error(err))
		c.JSON(consts.StatusBadRequest, utils.H{"error": err.Error()})
		return
	}

	cfg := conf.GetConf()
	stripe.Key = cfg.Stripe.SecretKey

	params := &stripe.RefundParams{
		PaymentIntent: stripe.String(req.PaymentIntentID),
	}
	if req.Amount > 0 {
		params.Amount = stripe.Int64(req.Amount)
	}
	if req.Reason != "" {
		params.Reason = stripe.String(req.Reason)
	}

	refund, err := refund.New(params)
	if err != nil {
		zap.L().Error("Failed to create refund", zap.Error(err))
		c.JSON(consts.StatusBadRequest, utils.H{"error": err.Error()})
		return
	}

	c.JSON(consts.StatusOK, utils.H{
		"refund_id": refund.ID,
		"status":    refund.Status,
		"amount":    refund.Amount,
		"currency":  refund.Currency,
	})
}

func VerifyApplePurchase(ctx context.Context, c *app.RequestContext) {
	var req AppleVerifyRequest
	if err := c.BindAndValidate(&req); err != nil {
		c.JSON(consts.StatusBadRequest, utils.H{"error": "Invalid request"})
		return
	}

	// 输入验证增强
	if err := ValidateReceiptData(req.ReceiptData); err != nil {
		zap.L().Error("Invalid receipt_data", zap.Error(err))
		c.JSON(consts.StatusBadRequest, utils.H{"error": err.Error()})
		return
	}

	cfg := conf.GetConf()

	// 准备请求数据
	requestData := map[string]interface{}{
		"receipt-data": req.ReceiptData,
		"password":     cfg.Apple.SharedSecret,
	}

	jsonData, _ := json.Marshal(requestData)

	// 先尝试生产环境
	prodResp, err := http.Post(cfg.Apple.ProductionURL, "application/json",
		readerCloserFromBytes(jsonData))
	if err != nil {
		zap.L().Error("Failed to connect to Apple", zap.Error(err))
		c.JSON(consts.StatusInternalServerError, utils.H{"error": "Failed to connect to Apple"})
		return
	}
	defer prodResp.Body.Close()

	// 如果生产环境返回 21007（沙盒收据），则请求沙盒环境
	var verifyResp AppleVerifyResponse
	if err := json.NewDecoder(prodResp.Body).Decode(&verifyResp); err != nil {
		c.JSON(consts.StatusInternalServerError, utils.H{"error": "Failed to parse response"})
		return
	}

	if verifyResp.Status == 21007 {
		// 沙盒收据，使用沙盒 URL
		sandboxResp, err := http.Post(cfg.Apple.SandboxURL, "application/json",
			readerCloserFromBytes(jsonData))
		if err != nil {
			c.JSON(consts.StatusInternalServerError, utils.H{"error": "Failed to connect to Apple sandbox"})
			return
		}
		defer sandboxResp.Body.Close()
		json.NewDecoder(sandboxResp.Body).Decode(&verifyResp)
	}

	c.JSON(consts.StatusOK, verifyResp)
}

func VerifyAppleSubscription(ctx context.Context, c *app.RequestContext) {
	// 类似于 VerifyApplePurchase，专门处理订阅
	// 实际实现中需要额外的订阅验证逻辑
	VerifyApplePurchase(ctx, c)
}

func AppleWebhook(ctx context.Context, c *app.RequestContext) {
	// Apple 服务器到服务器的通知（App Store Server Notifications）
	// 这里需要处理 Apple 的 webhook 通知
	zap.L().Info("Received Apple webhook")

	c.JSON(consts.StatusOK, utils.H{"received": true})
}

func GetPaymentStatus(ctx context.Context, c *app.RequestContext) {
	paymentID := c.Param("id")
	if paymentID == "" {
		c.JSON(consts.StatusBadRequest, utils.H{"error": "payment_id is required"})
		return
	}

	zap.L().Info("GetPaymentStatus called", zap.String("payment_id", paymentID))

	// 先尝试从数据库通过payment_id查询
	var paymentIntentID string
	var dbStatus string
	var dbAmount int64
	var dbCurrency string

	if db.DB == nil {
		zap.L().Warn("Database not available for GetPaymentStatus", zap.String("payment_id", paymentID))
		c.JSON(consts.StatusInternalServerError, utils.H{"error": "Database not available"})
		return
	}

	payment, err := db.GetPaymentByPaymentID(paymentID)
	if err != nil {
		zap.L().Warn("Failed to get payment from database", zap.Error(err), zap.String("payment_id", paymentID))
	} else if payment != nil {
		zap.L().Info("Found payment in database",
			zap.String("payment_id", paymentID),
			zap.String("payment_intent_id", payment.PaymentIntentID),
			zap.String("status", payment.Status))
		paymentIntentID = payment.PaymentIntentID
		dbStatus = payment.Status
		dbAmount = payment.Amount
		dbCurrency = payment.Currency
	} else {
		zap.L().Info("Payment not found in database", zap.String("payment_id", paymentID))
	}

	// 如果找到了payment_intent_id，从Stripe获取最新状态
	if paymentIntentID != "" {
		cfg := conf.GetConf()
		stripe.Key = cfg.Stripe.SecretKey

		intent, err := paymentintent.Get(paymentIntentID, nil)
		if err != nil {
			zap.L().Warn("Failed to get payment intent from Stripe, using database status",
				zap.Error(err),
				zap.String("payment_intent_id", paymentIntentID))
			// 如果Stripe查询失败，返回数据库中的状态
			c.JSON(consts.StatusOK, utils.H{
				"payment_id":        paymentID,
				"payment_intent_id": paymentIntentID,
				"status":            dbStatus,
				"amount":            dbAmount,
				"currency":          dbCurrency,
				"source":            "database",
			})
			return
		}

		// 成功从Stripe获取，返回最新状态
		c.JSON(consts.StatusOK, utils.H{
			"payment_id":        paymentID,
			"payment_intent_id": intent.ID,
			"status":            intent.Status,
			"amount":            intent.Amount,
			"currency":          intent.Currency,
			"source":            "stripe",
		})
		return
	}

	// 如果payment_id看起来像Stripe的payment_intent_id（以pi_开头），直接查询Stripe
	if len(paymentID) > 3 && paymentID[:3] == "pi_" {
		cfg := conf.GetConf()
		stripe.Key = cfg.Stripe.SecretKey

		intent, err := paymentintent.Get(paymentID, nil)
		if err != nil {
			c.JSON(consts.StatusNotFound, utils.H{"error": "Payment not found"})
			return
		}

		c.JSON(consts.StatusOK, utils.H{
			"payment_id":        paymentID,
			"payment_intent_id": intent.ID,
			"status":            intent.Status,
			"amount":            intent.Amount,
			"currency":          intent.Currency,
			"source":            "stripe",
		})
		return
	}

	// 既不是数据库中的payment_id，也不是Stripe的payment_intent_id
	c.JSON(consts.StatusNotFound, utils.H{"error": "Payment not found"})
}

// readerCloserFromBytes 将 []byte 转换为 io.ReadCloser
func readerCloserFromBytes(data []byte) io.ReadCloser {
	return io.NopCloser(strings.NewReader(string(data)))
}

func init() {
	// 延迟初始化 Stripe
}
