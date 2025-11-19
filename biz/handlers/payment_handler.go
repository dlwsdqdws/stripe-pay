package handlers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"stripe-pay/biz"
	"stripe-pay/biz/models"
	"stripe-pay/biz/services"
	"stripe-pay/cache"
	"stripe-pay/common"
	"stripe-pay/conf"
	"stripe-pay/db"
	"strconv"
	"strings"
	"time"

	"sync"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/stripe/stripe-go/v78"
	"github.com/stripe/stripe-go/v78/paymentintent"
	"github.com/stripe/stripe-go/v78/refund"
	"github.com/stripe/stripe-go/v78/webhook"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	paymentService     *services.PaymentService
	paymentServiceOnce sync.Once
)

// getPaymentService 获取支付服务（懒加载）
func getPaymentService() *services.PaymentService {
	paymentServiceOnce.Do(func() {
		paymentService = services.NewPaymentService()
	})
	return paymentService
}

// getIdempotencyKey 从请求中获取幂等性密钥
func getIdempotencyKey(c *app.RequestContext) string {
	key := string(c.GetHeader("Idempotency-Key"))
	if key != "" {
		return key
	}
	key = string(c.GetHeader("X-Idempotency-Key"))
	if key != "" {
		return key
	}
	return ""
}

// CreateStripePayment 创建Stripe支付
func CreateStripePayment(ctx context.Context, c *app.RequestContext) {
	common.LogStage(c, "request_received", zap.String("handler", "CreateStripePayment"))

	var req models.CreatePaymentRequest
	common.LogStage(c, "binding_request")
	if err := c.BindAndValidate(&req); err != nil {
		common.LogStageWithLevel(c, zapcore.WarnLevel, "bind_failed", zap.Error(err))
		common.SendError(c, common.ErrInvalidRequest.WithDetails("Failed to bind request: "+err.Error()))
		return
	}
	common.LogStage(c, "request_bound", zap.String("user_id", req.UserID), zap.String("description", req.Description))

	// 显式验证必需字段（因为BindAndValidate可能允许空字符串）
	common.LogStage(c, "validating_required_fields")
	if req.UserID == "" {
		common.LogStageWithLevel(c, zapcore.WarnLevel, "validation_failed", zap.String("reason", "user_id is required"))
		common.SendError(c, common.ErrMissingParameter.WithDetails("user_id is required"))
		return
	}

	// 输入验证
	common.LogStage(c, "validating_input")
	if err := biz.ValidateUserID(req.UserID); err != nil {
		common.LogStageWithLevel(c, zapcore.WarnLevel, "validation_failed", zap.String("field", "user_id"), zap.Error(err))
		common.SendError(c, common.ErrValidationFailed.WithDetails(err.Error()))
		return
	}
	if err := biz.ValidateDescription(req.Description); err != nil {
		common.LogStageWithLevel(c, zapcore.WarnLevel, "validation_failed", zap.String("field", "description"), zap.Error(err))
		common.SendError(c, common.ErrValidationFailed.WithDetails(err.Error()))
		return
	}
	common.LogStage(c, "validation_passed")

	// 获取Idempotency Key
	idempotencyKey := getIdempotencyKey(c)
	common.LogStage(c, "checking_idempotency", zap.String("idempotency_key", idempotencyKey))

	// 检查幂等性
	existingPayment, err := getPaymentService().CheckIdempotency(ctx, idempotencyKey)
	if err != nil {
		common.LogStageWithLevel(c, zapcore.ErrorLevel, "idempotency_check_failed", zap.Error(err))
		zap.L().Error("Failed to check idempotency", zap.Error(err))
		// 继续执行，不阻止请求
	} else if existingPayment != nil {
		common.LogStage(c, "duplicate_request_detected",
			zap.String("idempotency_key", idempotencyKey),
			zap.String("payment_intent_id", existingPayment.PaymentIntentID))
		zap.L().Info("Duplicate request detected, returning existing payment",
			zap.String("idempotency_key", idempotencyKey),
			zap.String("payment_intent_id", existingPayment.PaymentIntentID))
		c.JSON(consts.StatusOK, existingPayment)
		return
	}
	common.LogStage(c, "idempotency_check_passed")

	// 创建支付
	common.LogStage(c, "creating_payment")
	response, err := getPaymentService().CreateStripePayment(ctx, &req, idempotencyKey)
	if err != nil {
		common.LogStageWithLevel(c, zapcore.ErrorLevel, "payment_creation_failed", zap.Error(err))
		// 检查是否是已支付错误
		if alreadyPaidErr, ok := err.(*services.AlreadyPaidError); ok {
			c.JSON(consts.StatusOK, utils.H{
				"already_paid":   true,
				"message":        "用户已支付成功，无需重复支付",
				"user_info":      alreadyPaidErr.UserInfo,
				"days_remaining": alreadyPaidErr.DaysRemaining,
			})
			return
		}

		// 检查是否是验证错误（应该返回400而不是500）
		errStr := strings.ToLower(err.Error())
		if strings.Contains(errStr, "required") ||
			strings.Contains(errStr, "invalid") ||
			strings.Contains(errStr, "validation") {
			// 这是验证错误，应该返回400
			common.SendError(c, err) // WrapError会自动识别并转换为正确的错误类型
			return
		}

		// 其他错误作为支付处理错误
		common.SendError(c, common.ErrPaymentProcessing.WithDetails(err.Error()))
		return
	}

	common.LogStage(c, "payment_created",
		zap.String("payment_id", response.PaymentID),
		zap.String("payment_intent_id", response.PaymentIntentID))

	// 更新缓存（异步）
	common.LogStage(c, "updating_cache")
	if cache.IsAvailable() && response.PaymentID != "" {
		go func() {
			// 从数据库获取完整信息并缓存
			if db.DB != nil {
				payment, err := db.GetPaymentByPaymentID(response.PaymentID)
				if err == nil && payment != nil {
					cacheData := &cache.PaymentCacheData{
						PaymentID:       payment.PaymentID,
						PaymentIntentID: payment.PaymentIntentID,
						UserID:          payment.UserID,
						Amount:          payment.Amount,
						Currency:        payment.Currency,
						Status:          payment.Status,
						PaymentMethod:   payment.PaymentMethod,
						Description:     payment.Description,
						CreatedAt:       payment.CreatedAt.Format(time.RFC3339),
						UpdatedAt:       payment.UpdatedAt.Format(time.RFC3339),
					}
					cache.SetPayment(context.Background(), response.PaymentID, cacheData, cache.DefaultPaymentCacheTTL)
					cache.SetPaymentByIntentID(context.Background(), response.PaymentIntentID, cacheData, cache.DefaultPaymentCacheTTL)
				}
			}
		}()
		common.LogStage(c, "cache_update_scheduled")
	}

	common.LogStage(c, "sending_response", zap.String("payment_id", response.PaymentID))
	c.JSON(consts.StatusOK, response)
}

// CreateStripeWeChatPayment 创建微信支付
func CreateStripeWeChatPayment(ctx context.Context, c *app.RequestContext) {
	var req models.CreateWeChatPaymentRequest
	if err := c.BindAndValidate(&req); err != nil {
		common.SendError(c, common.ErrInvalidRequest.WithDetails("Failed to bind request"))
		return
	}

	idempotencyKey := getIdempotencyKey(c)

	// 检查幂等性
	existingPayment, err := getPaymentService().CheckIdempotency(ctx, idempotencyKey)
	if err != nil {
		zap.L().Error("Failed to check idempotency", zap.Error(err))
	} else if existingPayment != nil {
		zap.L().Info("Duplicate request detected, returning existing payment",
			zap.String("idempotency_key", idempotencyKey))
		c.JSON(consts.StatusOK, utils.H{
			"client_secret":     existingPayment.ClientSecret,
			"payment_intent_id": existingPayment.PaymentIntentID,
			"status":            "pending",
			"message":           "返回已存在的支付记录",
		})
		return
	}

	// 创建支付
	response, err := getPaymentService().CreateWeChatPayment(ctx, &req, idempotencyKey)
	if err != nil {
		common.SendError(c, common.ErrPaymentProcessing.WithDetails(err.Error()))
		return
	}

	c.JSON(consts.StatusOK, response)
}

// GetPricing 获取定价信息
func GetPricing(ctx context.Context, c *app.RequestContext) {
	pricing, err := getPaymentService().GetCurrentPricing()
	if err != nil {
		common.SendError(c, common.ErrInternalServer.WithDetails("Failed to get pricing"))
		return
	}

	c.JSON(consts.StatusOK, models.PricingResponse{
		Amount:   pricing.Amount,
		Currency: pricing.Currency,
		Label:    pricing.Label,
	})
}

// ConfirmStripePayment 确认支付
func ConfirmStripePayment(ctx context.Context, c *app.RequestContext) {
	var req models.ConfirmPaymentRequest
	if err := c.BindAndValidate(&req); err != nil {
		common.SendError(c, common.ErrInvalidRequest)
		return
	}

	// 输入验证增强
	if err := biz.ValidatePaymentIntentID(req.PaymentID); err != nil {
		common.SendError(c, common.ErrValidationFailed.WithDetails(err.Error()))
		return
	}

	cfg := conf.GetConf()
	stripe.Key = cfg.Stripe.SecretKey

	intent, err := paymentintent.Get(req.PaymentID, nil)
	if err != nil {
		common.SendError(c, common.ErrPaymentNotFound)
		return
	}

	c.JSON(consts.StatusOK, utils.H{
		"payment_id": intent.ID,
		"status":     intent.Status,
		"amount":     intent.Amount,
		"currency":   intent.Currency,
	})
}

// UpdatePaymentConfig 更新支付配置
func UpdatePaymentConfig(ctx context.Context, c *app.RequestContext) {
	var req models.UpdatePaymentConfigRequest
	if err := c.BindAndValidate(&req); err != nil {
		common.SendError(c, common.ErrInvalidRequest)
		return
	}

	// 输入验证增强
	if err := biz.ValidateAmount(req.Amount); err != nil {
		common.SendError(c, common.ErrValidationFailed.WithDetails(err.Error()))
		return
	}

	if req.Currency == "" {
		req.Currency = "hkd"
	}

	if err := biz.ValidateCurrency(req.Currency); err != nil {
		common.SendError(c, common.ErrValidationFailed.WithDetails(err.Error()))
		return
	}

	if err := biz.ValidateDescription(req.Description); err != nil {
		common.SendError(c, common.ErrValidationFailed.WithDetails(err.Error()))
		return
	}

	if req.Description == "" {
		req.Description = "支付金额配置"
	}

	if db.DB == nil {
		c.JSON(consts.StatusInternalServerError, utils.H{"error": "Database not available"})
		return
	}

	err := db.UpdatePaymentConfig(req.Currency, req.Amount, req.Description)
	if err != nil {
		common.SendError(c, common.ErrDatabaseError.WithDetails("Failed to update payment config"))
		return
	}

	config, err := db.GetPaymentConfig(req.Currency)
	if err != nil {
		zap.L().Warn("Failed to get updated config", zap.Error(err))
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

// GetPaymentConfig 获取支付配置
func GetPaymentConfig(ctx context.Context, c *app.RequestContext) {
	currency := c.Query("currency")
	if currency == "" {
		currency = "hkd"
	}

	if db.DB == nil {
		common.SendError(c, common.ErrDatabaseError.WithDetails("Database not available"))
		return
	}

	config, err := db.GetPaymentConfig(currency)
	if err != nil {
		common.SendError(c, common.ErrDatabaseError.WithDetails("Failed to get payment config"))
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
		common.SendError(c, common.ErrMissingParameter.WithDetails("user_id is required"))
		return
	}

	if err := biz.ValidateUserID(userID); err != nil {
		common.SendError(c, common.ErrValidationFailed.WithDetails(err.Error()))
		return
	}

	if db.DB == nil {
		common.SendError(c, common.ErrDatabaseError.WithDetails("Database not available"))
		return
	}

	info, err := db.GetUserPaymentInfo(userID)
	if err != nil {
		common.SendError(c, common.ErrDatabaseError.WithDetails("Failed to get user payment info"))
		return
	}

	c.JSON(consts.StatusOK, info)
}

// UpdatePaymentStatusFromFrontend 更新支付状态
func UpdatePaymentStatusFromFrontend(ctx context.Context, c *app.RequestContext) {
	var req models.UpdatePaymentStatusRequest
	if err := c.BindAndValidate(&req); err != nil {
		common.SendError(c, common.ErrInvalidRequest)
		return
	}

	if err := biz.ValidatePaymentIntentID(req.PaymentIntentID); err != nil {
		common.SendError(c, common.ErrValidationFailed.WithDetails(err.Error()))
		return
	}
	if err := biz.ValidatePaymentStatus(req.Status); err != nil {
		common.SendError(c, common.ErrValidationFailed.WithDetails(err.Error()))
		return
	}

	cfg := conf.GetConf()
	stripe.Key = cfg.Stripe.SecretKey

	intent, err := paymentintent.Get(req.PaymentIntentID, nil)
	if err != nil {
		common.SendError(c, common.ErrPaymentNotFound)
		return
	}

	actualStatus := string(intent.Status)

	if db.DB != nil {
		if err := db.UpdatePaymentStatus(req.PaymentIntentID, actualStatus); err != nil {
			zap.L().Warn("Failed to update payment status", zap.Error(err))
		} else {
			// 更新缓存（异步）
			if cache.IsAvailable() {
				go func() {
					// 更新 Stripe 状态缓存（根据状态决定缓存策略）
					updateStripeStatusCache(context.Background(), req.PaymentIntentID, intent)

					// 通过 payment_intent_id 查找 payment_id 并更新支付缓存
					payment, err := db.GetPaymentByIntentID(req.PaymentIntentID)
					if err == nil && payment == nil {
						// 如果通过 payment_intent_id 找不到，删除相关缓存
						cache.DeletePaymentByIntentID(context.Background(), req.PaymentIntentID)
					} else if payment != nil {
						cacheData := &cache.PaymentCacheData{
							PaymentID:       payment.PaymentID,
							PaymentIntentID: payment.PaymentIntentID,
							UserID:          payment.UserID,
							Amount:          intent.Amount,
							Currency:        string(intent.Currency),
							Status:          actualStatus,
							PaymentMethod:   payment.PaymentMethod,
							Description:     payment.Description,
							CreatedAt:       payment.CreatedAt.Format(time.RFC3339),
							UpdatedAt:       time.Now().Format(time.RFC3339),
						}
						cache.SetPayment(context.Background(), payment.PaymentID, cacheData, cache.DefaultPaymentCacheTTL)
						cache.SetPaymentByIntentID(context.Background(), req.PaymentIntentID, cacheData, cache.DefaultPaymentCacheTTL)
					}
				}()
			}
		}

		if actualStatus == "succeeded" {
			// 记录支付成功指标
			common.RecordPayment("stripe", "succeeded", intent.Amount, string(intent.Currency), 0)

			userID := intent.Metadata["user_id"]
			if userID != "" {
				if err := db.UpdateUserPaymentInfo(userID, intent.Amount); err != nil {
					zap.L().Warn("Failed to update user payment info", zap.Error(err))
				} else {
					// 使用户支付缓存失效
					if cache.IsAvailable() {
						go func() {
							cache.InvalidateUserPaymentCache(context.Background(), userID)
						}()
					}
				}
			}
		} else if actualStatus == "failed" || actualStatus == "canceled" {
			// 记录支付失败或取消指标
			common.RecordPayment("stripe", actualStatus, intent.Amount, string(intent.Currency), 0)
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
		common.SendError(c, common.ErrMissingParameter.WithDetails("user_id is required"))
		return
	}

	if err := biz.ValidateUserID(userID); err != nil {
		common.SendError(c, common.ErrValidationFailed.WithDetails(err.Error()))
		return
	}

	limit := 50
	if limitStr := c.Query("limit"); limitStr != "" {
		if parsedLimit, err := strconv.Atoi(limitStr); err == nil && parsedLimit > 0 {
			limit = parsedLimit
		}
	}

	if db.DB == nil {
		common.SendError(c, common.ErrDatabaseError.WithDetails("Database not available"))
		return
	}

	history, err := db.GetPaymentHistory(userID, limit)
	if err != nil {
		common.SendError(c, common.ErrDatabaseError.WithDetails("Failed to get payment history"))
		return
	}

	c.JSON(consts.StatusOK, utils.H{
		"user_id": userID,
		"count":   len(history),
		"history": history,
	})
}

// RefundPayment 退款
func RefundPayment(ctx context.Context, c *app.RequestContext) {
	var req models.RefundRequest
	if err := c.BindAndValidate(&req); err != nil || req.PaymentIntentID == "" {
		common.SendError(c, common.ErrMissingParameter.WithDetails("payment_intent_id required"))
		return
	}

	if err := biz.ValidatePaymentIntentID(req.PaymentIntentID); err != nil {
		common.SendError(c, common.ErrValidationFailed.WithDetails(err.Error()))
		return
	}
	if req.Amount > 0 {
		if err := biz.ValidateAmount(req.Amount); err != nil {
			common.SendError(c, common.ErrValidationFailed.WithDetails(err.Error()))
			return
		}
	}
	if err := biz.ValidateRefundReason(req.Reason); err != nil {
		common.SendError(c, common.ErrValidationFailed.WithDetails(err.Error()))
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

	refundResult, err := refund.New(params)
	if err != nil {
		common.SendError(c, common.ErrPaymentProcessing.WithDetails(err.Error()))
		return
	}

	c.JSON(consts.StatusOK, utils.H{
		"refund_id": refundResult.ID,
		"status":    refundResult.Status,
		"amount":    refundResult.Amount,
		"currency":  refundResult.Currency,
	})
}

// StripeWebhook 处理Stripe webhook
func StripeWebhook(ctx context.Context, c *app.RequestContext) {
	cfg := conf.GetConf()

	// 读取请求体 - 使用 Body() 而不是 BodyStream()，确保获取原始请求体
	// 注意：在 Hertz 中，如果中间件已经读取了 BodyStream()，需要使用 Body()
	body := c.Request.Body()
	if len(body) == 0 {
		// 如果 Body() 为空，尝试从 BodyStream() 读取（可能中间件没有消耗）
		var err error
		body, err = io.ReadAll(c.Request.BodyStream())
		if err != nil {
			zap.L().Error("Failed to read request body", zap.Error(err))
			common.SendError(c, common.ErrInvalidRequest.WithDetails("Invalid request body"))
			return
		}
	}

	if len(body) == 0 {
		zap.L().Error("Request body is empty")
		common.SendError(c, common.ErrInvalidRequest.WithDetails("Request body is empty"))
		return
	}

	// 获取签名头
	signatureBytes := c.GetHeader("Stripe-Signature")
	if len(signatureBytes) == 0 {
		common.SendError(c, common.ErrInvalidRequest.WithDetails("Missing Stripe-Signature header"))
		return
	}
	signature := string(signatureBytes)

	// 使用Stripe官方库验证签名
	endpointSecret := cfg.Stripe.WebhookSecret
	if endpointSecret == "" {
		common.SendError(c, common.ErrInternalServer.WithDetails("Webhook secret not configured"))
		return
	}

	// 添加调试日志
	zap.L().Debug("Webhook signature verification",
		zap.String("signature_prefix", func() string {
			if len(signature) > 20 {
				return signature[:20] + "..."
			}
			return signature
		}()),
		zap.String("secret_prefix", func() string {
			if len(endpointSecret) > 10 {
				return endpointSecret[:10] + "..."
			}
			return endpointSecret
		}()),
		zap.Int("body_length", len(body)),
	)

	// 使用 ConstructEventWithOptions 以支持不同的 API 版本
	// 忽略 API 版本不匹配，因为 Stripe Dashboard 可能使用较新的 API 版本
	event, err := webhook.ConstructEventWithOptions(
		body,
		signature,
		endpointSecret,
		webhook.ConstructEventOptions{
			IgnoreAPIVersionMismatch: true,
		},
	)
	if err != nil {
		zap.L().Error("Webhook signature verification failed",
			zap.Error(err),
			zap.String("error_details", err.Error()),
			zap.String("signature_prefix", func() string {
				if len(signature) > 20 {
					return signature[:20] + "..."
				}
				return signature
			}()),
		)
		common.SendError(c, common.ErrInvalidRequest.WithDetails("Invalid signature: "+err.Error()))
		return
	}

	// 优化3: 检查事件是否已处理（幂等性）
	if cache.IsAvailable() {
		processed, err := cache.IsWebhookEventProcessed(ctx, event.ID)
		if err == nil && processed {
			zap.L().Info("Webhook event already processed, skipping duplicate",
				zap.String("event_id", event.ID),
				zap.String("event_type", string(event.Type)))
			// 事件已处理，直接返回成功（避免 Stripe 重试）
			c.JSON(consts.StatusOK, utils.H{"received": true, "duplicate": true})
			return
		}
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
			// 记录支付成功指标
			common.RecordPayment("stripe", "succeeded", pi.Amount, string(pi.Currency), 0)

			// 更新数据库中的支付状态
			if db.DB != nil {
				// 更新支付历史状态
				if err := db.UpdatePaymentStatus(pi.ID, string(pi.Status)); err != nil {
					zap.L().Warn("Failed to update payment status", zap.Error(err))
				} else {
					// 优化5: 最终状态也设置短期缓存（必须设置失效时间）
					if cache.IsAvailable() {
						go func() {
							// 优化4: 从 metadata 获取 payment_id（避免查询数据库）
							paymentID := pi.Metadata["payment_id"]

							// 优化5: 最终状态设置短期缓存（5分钟），而不是删除
							if cache.IsFinalStatus(string(pi.Status)) {
								zap.L().Info("Final status in webhook, setting short-term cache",
									zap.String("payment_intent_id", pi.ID),
									zap.String("status", string(pi.Status)),
									zap.String("payment_id", paymentID))

								// 更新 Stripe 状态缓存（短期，5分钟）
								stripeStatusData := &cache.StripeStatusCacheData{
									PaymentIntentID: pi.ID,
									Status:          string(pi.Status),
									Amount:          pi.Amount,
									Currency:        string(pi.Currency),
									CachedAt:        time.Now().Format(time.RFC3339),
								}
								cache.SetStripeStatus(context.Background(), pi.ID, stripeStatusData, cache.FinalStatusCacheTTL)

								// 更新支付缓存（短期，5分钟）
								if paymentID != "" {
									payment, err := db.GetPaymentByPaymentID(paymentID)
									if err == nil && payment != nil {
										cacheData := &cache.PaymentCacheData{
											PaymentID:       payment.PaymentID,
											PaymentIntentID: pi.ID,
											UserID:          payment.UserID,
											Amount:          pi.Amount,
											Currency:        string(pi.Currency),
											Status:          string(pi.Status),
											PaymentMethod:   payment.PaymentMethod,
											Description:     payment.Description,
											CreatedAt:       payment.CreatedAt.Format(time.RFC3339),
											UpdatedAt:       time.Now().Format(time.RFC3339),
										}
										cache.SetPayment(context.Background(), payment.PaymentID, cacheData, cache.FinalStatusCacheTTL)
										cache.SetPaymentByIntentID(context.Background(), pi.ID, cacheData, cache.FinalStatusCacheTTL)
									}
								} else {
									// 如果 metadata 中没有 payment_id，回退到查询数据库
									payment, err := db.GetPaymentByIntentID(pi.ID)
									if err == nil && payment != nil {
										cacheData := &cache.PaymentCacheData{
											PaymentID:       payment.PaymentID,
											PaymentIntentID: pi.ID,
											UserID:          payment.UserID,
											Amount:          pi.Amount,
											Currency:        string(pi.Currency),
											Status:          string(pi.Status),
											PaymentMethod:   payment.PaymentMethod,
											Description:     payment.Description,
											CreatedAt:       payment.CreatedAt.Format(time.RFC3339),
											UpdatedAt:       time.Now().Format(time.RFC3339),
										}
										cache.SetPayment(context.Background(), payment.PaymentID, cacheData, cache.FinalStatusCacheTTL)
										cache.SetPaymentByIntentID(context.Background(), pi.ID, cacheData, cache.FinalStatusCacheTTL)
									}
								}
							} else {
								// 中间状态：更新缓存
								stripeStatusData := &cache.StripeStatusCacheData{
									PaymentIntentID: pi.ID,
									Status:          string(pi.Status),
									Amount:          pi.Amount,
									Currency:        string(pi.Currency),
									CachedAt:        time.Now().Format(time.RFC3339),
								}
								ttl := cache.GetStripeStatusTTL(string(pi.Status))
								cache.SetStripeStatus(context.Background(), pi.ID, stripeStatusData, ttl)
							}
						}()
					}
				}

				// 获取用户ID（从 metadata 中）
				userID := pi.Metadata["user_id"]
				if userID != "" {
					// 更新用户支付信息
					if err := db.UpdateUserPaymentInfo(userID, pi.Amount); err != nil {
						zap.L().Warn("Failed to update user payment info", zap.Error(err))
					} else {
						// 使用户支付缓存失效
						if cache.IsAvailable() {
							go func() {
								cache.InvalidateUserPaymentCache(context.Background(), userID)
							}()
						}
					}

					// 触发支付成功后的业务逻辑（异步执行，不阻塞 Webhook 响应）
					go handlePaymentSuccessBusinessLogic(userID, &pi)
				}
			}
		}

	case "payment_intent.payment_failed":
		zap.L().Info("Payment failed", zap.String("event_id", event.ID))

		// 解析 PaymentIntent 并更新状态
		var pi stripe.PaymentIntent
		if err := json.Unmarshal(event.Data.Raw, &pi); err == nil {
			// 记录支付失败指标
			common.RecordPayment("stripe", "failed", pi.Amount, string(pi.Currency), 0)

			if db.DB != nil {
				db.UpdatePaymentStatus(pi.ID, string(pi.Status))
				// 优化5: 最终状态设置短期缓存（必须设置失效时间）
				if cache.IsAvailable() {
					go func() {
						// 优化4: 从 metadata 获取 payment_id
						paymentID := pi.Metadata["payment_id"]

						zap.L().Info("Final status (failed) in webhook, setting short-term cache",
							zap.String("payment_intent_id", pi.ID),
							zap.String("status", string(pi.Status)),
							zap.String("payment_id", paymentID))

						// 更新 Stripe 状态缓存（短期，5分钟）
						stripeStatusData := &cache.StripeStatusCacheData{
							PaymentIntentID: pi.ID,
							Status:          string(pi.Status),
							Amount:          pi.Amount,
							Currency:        string(pi.Currency),
							CachedAt:        time.Now().Format(time.RFC3339),
						}
						cache.SetStripeStatus(context.Background(), pi.ID, stripeStatusData, cache.FinalStatusCacheTTL)

						// 更新支付缓存（短期，5分钟）
						if paymentID != "" {
							payment, err := db.GetPaymentByPaymentID(paymentID)
							if err == nil && payment != nil {
								cacheData := &cache.PaymentCacheData{
									PaymentID:       payment.PaymentID,
									PaymentIntentID: pi.ID,
									UserID:          payment.UserID,
									Amount:          pi.Amount,
									Currency:        string(pi.Currency),
									Status:          string(pi.Status),
									PaymentMethod:   payment.PaymentMethod,
									Description:     payment.Description,
									CreatedAt:       payment.CreatedAt.Format(time.RFC3339),
									UpdatedAt:       time.Now().Format(time.RFC3339),
								}
								cache.SetPayment(context.Background(), payment.PaymentID, cacheData, cache.FinalStatusCacheTTL)
								cache.SetPaymentByIntentID(context.Background(), pi.ID, cacheData, cache.FinalStatusCacheTTL)
							}
						} else {
							// 回退到查询数据库
							payment, err := db.GetPaymentByIntentID(pi.ID)
							if err == nil && payment != nil {
								cacheData := &cache.PaymentCacheData{
									PaymentID:       payment.PaymentID,
									PaymentIntentID: pi.ID,
									UserID:          payment.UserID,
									Amount:          pi.Amount,
									Currency:        string(pi.Currency),
									Status:          string(pi.Status),
									PaymentMethod:   payment.PaymentMethod,
									Description:     payment.Description,
									CreatedAt:       payment.CreatedAt.Format(time.RFC3339),
									UpdatedAt:       time.Now().Format(time.RFC3339),
								}
								cache.SetPayment(context.Background(), payment.PaymentID, cacheData, cache.FinalStatusCacheTTL)
								cache.SetPaymentByIntentID(context.Background(), pi.ID, cacheData, cache.FinalStatusCacheTTL)
							}
						}
					}()
				}

				// 触发支付失败后的业务逻辑（异步执行）
				userID := pi.Metadata["user_id"]
				if userID != "" {
					go handlePaymentFailedBusinessLogic(userID, &pi)
				}
			}
		}

	case "payment_intent.canceled":
		zap.L().Info("Payment canceled", zap.String("event_id", event.ID))

		// 解析 PaymentIntent 并更新状态
		var pi stripe.PaymentIntent
		if err := json.Unmarshal(event.Data.Raw, &pi); err == nil {
			// 记录支付取消指标
			common.RecordPayment("stripe", "canceled", pi.Amount, string(pi.Currency), 0)

			if db.DB != nil {
				db.UpdatePaymentStatus(pi.ID, string(pi.Status))
				// 优化5: 最终状态设置短期缓存（必须设置失效时间）
				if cache.IsAvailable() {
					go func() {
						// 优化4: 从 metadata 获取 payment_id
						paymentID := pi.Metadata["payment_id"]

						zap.L().Info("Final status (canceled) in webhook, setting short-term cache",
							zap.String("payment_intent_id", pi.ID),
							zap.String("status", string(pi.Status)),
							zap.String("payment_id", paymentID))

						// 更新 Stripe 状态缓存（短期，5分钟）
						stripeStatusData := &cache.StripeStatusCacheData{
							PaymentIntentID: pi.ID,
							Status:          string(pi.Status),
							Amount:          pi.Amount,
							Currency:        string(pi.Currency),
							CachedAt:        time.Now().Format(time.RFC3339),
						}
						cache.SetStripeStatus(context.Background(), pi.ID, stripeStatusData, cache.FinalStatusCacheTTL)

						// 更新支付缓存（短期，5分钟）
						if paymentID != "" {
							payment, err := db.GetPaymentByPaymentID(paymentID)
							if err == nil && payment != nil {
								cacheData := &cache.PaymentCacheData{
									PaymentID:       payment.PaymentID,
									PaymentIntentID: pi.ID,
									UserID:          payment.UserID,
									Amount:          pi.Amount,
									Currency:        string(pi.Currency),
									Status:          string(pi.Status),
									PaymentMethod:   payment.PaymentMethod,
									Description:     payment.Description,
									CreatedAt:       payment.CreatedAt.Format(time.RFC3339),
									UpdatedAt:       time.Now().Format(time.RFC3339),
								}
								cache.SetPayment(context.Background(), payment.PaymentID, cacheData, cache.FinalStatusCacheTTL)
								cache.SetPaymentByIntentID(context.Background(), pi.ID, cacheData, cache.FinalStatusCacheTTL)
							}
						} else {
							// 回退到查询数据库
							payment, err := db.GetPaymentByIntentID(pi.ID)
							if err == nil && payment != nil {
								cacheData := &cache.PaymentCacheData{
									PaymentID:       payment.PaymentID,
									PaymentIntentID: pi.ID,
									UserID:          payment.UserID,
									Amount:          pi.Amount,
									Currency:        string(pi.Currency),
									Status:          string(pi.Status),
									PaymentMethod:   payment.PaymentMethod,
									Description:     payment.Description,
									CreatedAt:       payment.CreatedAt.Format(time.RFC3339),
									UpdatedAt:       time.Now().Format(time.RFC3339),
								}
								cache.SetPayment(context.Background(), payment.PaymentID, cacheData, cache.FinalStatusCacheTTL)
								cache.SetPaymentByIntentID(context.Background(), pi.ID, cacheData, cache.FinalStatusCacheTTL)
							}
						}
					}()
				}

				// 触发支付取消后的业务逻辑（异步执行）
				userID := pi.Metadata["user_id"]
				if userID != "" {
					go handlePaymentCanceledBusinessLogic(userID, &pi)
				}
			}
		}

	default:
		zap.L().Info("Unhandled event type", zap.String("type", string(event.Type)))
	}

	// 优化3: 标记事件已处理（在所有事件类型处理完成后）
	if cache.IsAvailable() {
		if err := cache.MarkWebhookEventProcessed(ctx, event.ID); err != nil {
			zap.L().Warn("Failed to mark webhook event as processed", zap.Error(err), zap.String("event_id", event.ID))
		}
	}

	c.JSON(consts.StatusOK, utils.H{"received": true})
}

// VerifyApplePurchase 验证Apple内购
func VerifyApplePurchase(ctx context.Context, c *app.RequestContext) {
	var req models.AppleVerifyRequest
	if err := c.BindAndValidate(&req); err != nil {
		common.SendError(c, common.ErrInvalidRequest)
		return
	}

	// 输入验证增强
	if err := biz.ValidateReceiptData(req.ReceiptData); err != nil {
		common.SendError(c, common.ErrValidationFailed.WithDetails(err.Error()))
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
		io.NopCloser(strings.NewReader(string(jsonData))))
	if err != nil {
		common.SendError(c, common.ErrExternalService.WithDetails("Failed to connect to Apple"))
		return
	}
	defer prodResp.Body.Close()

	// 如果生产环境返回 21007（沙盒收据），则请求沙盒环境
	var verifyResp models.AppleVerifyResponse
	if err := json.NewDecoder(prodResp.Body).Decode(&verifyResp); err != nil {
		common.SendError(c, common.ErrExternalService.WithDetails("Failed to parse response"))
		return
	}

	if verifyResp.Status == 21007 {
		// 沙盒收据，使用沙盒 URL
		sandboxResp, err := http.Post(cfg.Apple.SandboxURL, "application/json",
			io.NopCloser(strings.NewReader(string(jsonData))))
		if err != nil {
			common.SendError(c, common.ErrExternalService.WithDetails("Failed to connect to Apple sandbox"))
			return
		}
		defer sandboxResp.Body.Close()
		json.NewDecoder(sandboxResp.Body).Decode(&verifyResp)
	}

	c.JSON(consts.StatusOK, verifyResp)
}

// VerifyAppleSubscription 验证Apple订阅
func VerifyAppleSubscription(ctx context.Context, c *app.RequestContext) {
	// 类似于 VerifyApplePurchase，专门处理订阅
	// 实际实现中需要额外的订阅验证逻辑
	VerifyApplePurchase(ctx, c)
}

// AppleWebhook 处理Apple webhook
func AppleWebhook(ctx context.Context, c *app.RequestContext) {
	// Apple 服务器到服务器的通知（App Store Server Notifications）
	// 这里需要处理 Apple 的 webhook 通知
	zap.L().Info("Received Apple webhook")

	c.JSON(consts.StatusOK, utils.H{"received": true})
}

// GetPaymentStatus 获取支付状态（带 Redis 缓存）
func GetPaymentStatus(ctx context.Context, c *app.RequestContext) {
	common.LogStage(c, "request_received", zap.String("handler", "GetPaymentStatus"))

	paymentID := string(c.Param("id"))
	if paymentID == "" {
		common.LogStageWithLevel(c, zapcore.WarnLevel, "validation_failed", zap.String("reason", "payment_id is required"))
		common.SendError(c, common.ErrMissingParameter.WithDetails("payment_id is required"))
		return
	}

	common.LogStage(c, "payment_status_query_started", zap.String("payment_id", paymentID))
	zap.L().Info("GetPaymentStatus called", zap.String("payment_id", paymentID))

	// 1. 先尝试从 Redis 缓存获取支付信息
	common.LogStage(c, "checking_cache", zap.String("payment_id", paymentID))
	if cache.IsAvailable() {
		cachedData, err := cache.GetPayment(ctx, paymentID)
		if err == nil && cachedData != nil {
			common.LogStage(c, "cache_hit", zap.String("payment_id", paymentID))
			zap.L().Info("Payment found in cache", zap.String("payment_id", paymentID))

			// 1.1 先检查 Stripe 状态缓存（优化2: 优先查数据库，优化5: 最终状态也缓存）
			if cachedData.PaymentIntentID != "" {
				stripeStatus, err := cache.GetStripeStatus(ctx, cachedData.PaymentIntentID)
				if err == nil && stripeStatus != nil {
					// 优化2: 最终状态优先查数据库（Webhook 已保证准确性）
					if cache.IsFinalStatus(stripeStatus.Status) {
						zap.L().Info("Final status in cache, checking database first (webhook guaranteed accuracy)",
							zap.String("payment_intent_id", cachedData.PaymentIntentID),
							zap.String("status", stripeStatus.Status))
						// 优化2: 优先查数据库，如果数据库有最终状态，直接返回
						// 继续执行到数据库查询
					} else {
						// 中间状态：可以使用缓存，但采用 stale-while-revalidate 策略
						zap.L().Info("Stripe status cache hit (intermediate status)",
							zap.String("payment_intent_id", cachedData.PaymentIntentID),
							zap.String("status", stripeStatus.Status))

						// 检查是否有状态变化事件
						statusChangeEvent, _ := cache.GetStatusChangeEvent(ctx, cachedData.PaymentIntentID)
						hasStatusChange := statusChangeEvent != nil

						// 立即返回缓存数据（快速响应）
						response := utils.H{
							"payment_id":        cachedData.PaymentID,
							"payment_intent_id": stripeStatus.PaymentIntentID,
							"status":            stripeStatus.Status,
							"amount":            stripeStatus.Amount,
							"currency":          stripeStatus.Currency,
							"source":            "cache",
							"cached":            true, // 标识这是缓存数据
						}

						// 如果有状态变化，提示客户端重新查询
						if hasStatusChange {
							response["status_changed"] = true
							response["new_status"] = statusChangeEvent.NewStatus
							response["status_changed_at"] = statusChangeEvent.ChangedAt
							response["message"] = "Payment status has changed. Please query again for the latest status."
						}

						c.JSON(consts.StatusOK, response)

						// 后台异步验证并更新缓存（stale-while-revalidate）
						go func() {
							intent, err := getPaymentService().GetPaymentIntent(cachedData.PaymentIntentID)
							if err == nil {
								// 如果状态发生变化，记录状态变化事件并更新缓存
								if string(intent.Status) != stripeStatus.Status {
									zap.L().Info("Status changed during revalidation, recording event and updating cache",
										zap.String("payment_intent_id", cachedData.PaymentIntentID),
										zap.String("old_status", stripeStatus.Status),
										zap.String("new_status", string(intent.Status)))

									// 记录状态变化事件（客户端可以查询）
									cache.RecordStatusChange(context.Background(),
										cachedData.PaymentIntentID,
										stripeStatus.Status,
										string(intent.Status),
										"revalidate")

									// 更新数据库状态
									if db.DB != nil {
										db.UpdatePaymentStatus(cachedData.PaymentIntentID, string(intent.Status))
									}
								}
								updateStripeStatusCache(context.Background(), cachedData.PaymentIntentID, intent)
								updateCacheFromStripe(context.Background(), cachedData.PaymentID, cachedData.PaymentIntentID, intent)
							}
						}()
						return
					}
				}

				// 1.2 Stripe 状态缓存未命中或最终状态，查询 Stripe API（保证准确性）
				intent, err := getPaymentService().GetPaymentIntent(cachedData.PaymentIntentID)
				if err == nil {
					// 更新缓存（根据状态决定是否缓存）
					go func() {
						updateStripeStatusCache(context.Background(), cachedData.PaymentIntentID, intent)
						updateCacheFromStripe(context.Background(), cachedData.PaymentID, cachedData.PaymentIntentID, intent)
					}()

					c.JSON(consts.StatusOK, utils.H{
						"payment_id":        cachedData.PaymentID,
						"payment_intent_id": intent.ID,
						"status":            intent.Status,
						"amount":            intent.Amount,
						"currency":          intent.Currency,
						"source":            "stripe",
						"cached":            false, // 标识这是实时数据
					})
					return
				}
			}

			// 1.3 如果 Stripe 查询失败，返回缓存数据
			c.JSON(consts.StatusOK, utils.H{
				"payment_id":        cachedData.PaymentID,
				"payment_intent_id": cachedData.PaymentIntentID,
				"status":            cachedData.Status,
				"amount":            cachedData.Amount,
				"currency":          cachedData.Currency,
				"source":            "cache",
				"cached":            true, // 标识这是缓存数据
			})
			return
		}
	}

	// 优化2: 优先查询数据库（Webhook 已更新，保证准确性）
	common.LogStage(c, "querying_database_priority", zap.String("payment_id", paymentID))
	var paymentIntentID string
	var dbStatus string
	var dbAmount int64
	var dbCurrency string
	var payment *db.PaymentHistory

	// 优化2: 如果 paymentID 是 payment_intent_id（以 pi_ 开头），直接用 GetPaymentByIntentID 查询
	if len(paymentID) > 3 && paymentID[:3] == "pi_" {
		paymentIntentID = paymentID
		if db.DB != nil {
			common.LogStage(c, "querying_database_by_intent_id", zap.String("payment_intent_id", paymentIntentID))
			var err error
			payment, err = db.GetPaymentByIntentID(paymentIntentID)
			if err != nil {
				common.LogStageWithLevel(c, zapcore.WarnLevel, "database_query_failed", zap.Error(err), zap.String("payment_intent_id", paymentIntentID))
				zap.L().Warn("Failed to get payment from database by intent_id", zap.Error(err), zap.String("payment_intent_id", paymentIntentID))
			} else if payment != nil {
				common.LogStage(c, "database_query_success",
					zap.String("payment_id", payment.PaymentID),
					zap.String("payment_intent_id", paymentIntentID),
					zap.String("status", payment.Status))
				zap.L().Info("Found payment in database by intent_id",
					zap.String("payment_id", payment.PaymentID),
					zap.String("payment_intent_id", paymentIntentID),
					zap.String("status", payment.Status))
				dbStatus = payment.Status
				dbAmount = payment.Amount
				dbCurrency = payment.Currency

				// 优化2: 如果是最终状态，直接返回数据库状态（Webhook 已保证准确性）
				if cache.IsFinalStatus(dbStatus) {
					zap.L().Info("Final status from database (intent_id query), returning directly (webhook guaranteed accuracy)",
						zap.String("payment_id", payment.PaymentID),
						zap.String("payment_intent_id", paymentIntentID),
						zap.String("status", dbStatus),
						zap.String("source", "database"))

					// 更新缓存（短期，5分钟）
					if cache.IsAvailable() {
						go func() {
							cacheData := &cache.PaymentCacheData{
								PaymentID:       payment.PaymentID,
								PaymentIntentID: payment.PaymentIntentID,
								UserID:          payment.UserID,
								Amount:          payment.Amount,
								Currency:        payment.Currency,
								Status:          payment.Status,
								PaymentMethod:   payment.PaymentMethod,
								Description:     payment.Description,
								CreatedAt:       payment.CreatedAt.Format(time.RFC3339),
								UpdatedAt:       payment.UpdatedAt.Format(time.RFC3339),
							}
							// 优化5: 最终状态使用短期缓存（必须设置失效时间）
							cache.SetPayment(context.Background(), payment.PaymentID, cacheData, cache.FinalStatusCacheTTL)
							cache.SetPaymentByIntentID(context.Background(), paymentIntentID, cacheData, cache.FinalStatusCacheTTL)

							stripeStatusData := &cache.StripeStatusCacheData{
								PaymentIntentID: paymentIntentID,
								Status:          payment.Status,
								Amount:          payment.Amount,
								Currency:        payment.Currency,
								CachedAt:        time.Now().Format(time.RFC3339),
							}
							cache.SetStripeStatus(context.Background(), paymentIntentID, stripeStatusData, cache.FinalStatusCacheTTL)
						}()
					}

					// 直接返回数据库状态（Webhook 已保证准确性，无需查询 Stripe）
					c.JSON(consts.StatusOK, utils.H{
						"payment_id":        payment.PaymentID,
						"payment_intent_id": paymentIntentID,
						"status":            dbStatus,
						"amount":            dbAmount,
						"currency":          dbCurrency,
						"source":            "database", // Webhook 已更新，保证准确性
						"cached":            false,
					})
					return
				}

				// 中间状态：更新缓存，但需要查询 Stripe 验证
				if cache.IsAvailable() {
					go func() {
						cacheData := &cache.PaymentCacheData{
							PaymentID:       payment.PaymentID,
							PaymentIntentID: payment.PaymentIntentID,
							UserID:          payment.UserID,
							Amount:          payment.Amount,
							Currency:        payment.Currency,
							Status:          payment.Status,
							PaymentMethod:   payment.PaymentMethod,
							Description:     payment.Description,
							CreatedAt:       payment.CreatedAt.Format(time.RFC3339),
							UpdatedAt:       payment.UpdatedAt.Format(time.RFC3339),
						}
						cache.SetPayment(context.Background(), payment.PaymentID, cacheData, cache.DefaultPaymentCacheTTL)
						cache.SetPaymentByIntentID(context.Background(), paymentIntentID, cacheData, cache.DefaultPaymentCacheTTL)
					}()
				}
			} else {
				zap.L().Info("Payment not found in database by intent_id", zap.String("payment_intent_id", paymentIntentID))
			}
		}
	} else {
		// 使用 payment_id 查询
		if db.DB == nil {
			common.LogStageWithLevel(c, zapcore.WarnLevel, "database_unavailable", zap.String("payment_id", paymentID))
			zap.L().Warn("Database not available for GetPaymentStatus", zap.String("payment_id", paymentID))
		} else {
			var err error
			common.LogStage(c, "querying_database", zap.String("payment_id", paymentID))
			payment, err = db.GetPaymentByPaymentID(paymentID)
			if err != nil {
				common.LogStageWithLevel(c, zapcore.WarnLevel, "database_query_failed", zap.Error(err), zap.String("payment_id", paymentID))
				zap.L().Warn("Failed to get payment from database", zap.Error(err), zap.String("payment_id", paymentID))
			} else if payment != nil {
				common.LogStage(c, "database_query_success",
					zap.String("payment_id", paymentID),
					zap.String("payment_intent_id", payment.PaymentIntentID),
					zap.String("status", payment.Status))
				zap.L().Info("Found payment in database",
					zap.String("payment_id", paymentID),
					zap.String("payment_intent_id", payment.PaymentIntentID),
					zap.String("status", payment.Status))
				paymentIntentID = payment.PaymentIntentID
				dbStatus = payment.Status
				dbAmount = payment.Amount
				dbCurrency = payment.Currency

				// 优化2: 如果是最终状态，直接返回数据库状态（Webhook 已保证准确性）
				if cache.IsFinalStatus(dbStatus) {
					zap.L().Info("Final status from database, returning directly (webhook guaranteed accuracy)",
						zap.String("payment_id", paymentID),
						zap.String("status", dbStatus),
						zap.String("source", "database"))

					// 更新缓存（短期，5分钟）
					if cache.IsAvailable() {
						go func() {
							cacheData := &cache.PaymentCacheData{
								PaymentID:       payment.PaymentID,
								PaymentIntentID: payment.PaymentIntentID,
								UserID:          payment.UserID,
								Amount:          payment.Amount,
								Currency:        payment.Currency,
								Status:          payment.Status,
								PaymentMethod:   payment.PaymentMethod,
								Description:     payment.Description,
								CreatedAt:       payment.CreatedAt.Format(time.RFC3339),
								UpdatedAt:       payment.UpdatedAt.Format(time.RFC3339),
							}
							// 优化5: 最终状态使用短期缓存（必须设置失效时间）
							cache.SetPayment(context.Background(), paymentID, cacheData, cache.FinalStatusCacheTTL)

							// 同时更新 Stripe 状态缓存
							stripeStatusData := &cache.StripeStatusCacheData{
								PaymentIntentID: payment.PaymentIntentID,
								Status:          payment.Status,
								Amount:          payment.Amount,
								Currency:        payment.Currency,
								CachedAt:        time.Now().Format(time.RFC3339),
							}
							cache.SetStripeStatus(context.Background(), payment.PaymentIntentID, stripeStatusData, cache.FinalStatusCacheTTL)
						}()
					}

					// 直接返回数据库状态（Webhook 已保证准确性，无需查询 Stripe）
					c.JSON(consts.StatusOK, utils.H{
						"payment_id":        paymentID,
						"payment_intent_id": paymentIntentID,
						"status":            dbStatus,
						"amount":            dbAmount,
						"currency":          dbCurrency,
						"source":            "database", // Webhook 已更新，保证准确性
						"cached":            false,
					})
					return
				}

				// 中间状态：更新缓存，但需要查询 Stripe 验证
				if cache.IsAvailable() {
					go func() {
						cacheData := &cache.PaymentCacheData{
							PaymentID:       payment.PaymentID,
							PaymentIntentID: payment.PaymentIntentID,
							UserID:          payment.UserID,
							Amount:          payment.Amount,
							Currency:        payment.Currency,
							Status:          payment.Status,
							PaymentMethod:   payment.PaymentMethod,
							Description:     payment.Description,
							CreatedAt:       payment.CreatedAt.Format(time.RFC3339),
							UpdatedAt:       payment.UpdatedAt.Format(time.RFC3339),
						}
						cache.SetPayment(context.Background(), paymentID, cacheData, cache.DefaultPaymentCacheTTL)
					}()
				}
			} else {
				zap.L().Info("Payment not found in database", zap.String("payment_id", paymentID))
			}
		}
	}

	// 3. 如果找到了payment_intent_id，查询 Stripe 获取最新状态（准确性优先）
	if paymentIntentID != "" {
		common.LogStage(c, "querying_stripe", zap.String("payment_intent_id", paymentIntentID))
		// 3.1 先检查 Stripe 状态缓存（优化：信任 Webhook 设置的最终状态缓存）
		if cache.IsAvailable() {
			common.LogStage(c, "checking_stripe_status_cache", zap.String("payment_intent_id", paymentIntentID))
			stripeStatus, err := cache.GetStripeStatus(ctx, paymentIntentID)
			if err == nil && stripeStatus != nil {
				// 优化：如果缓存中有最终状态，信任缓存（Webhook 已设置），直接返回，避免 Stripe API 调用
				// 同时后台验证数据库状态，确保准确性
				if cache.IsFinalStatus(stripeStatus.Status) {
					zap.L().Info("Final status in cache (webhook guaranteed), returning directly to avoid Stripe API call",
						zap.String("payment_intent_id", paymentIntentID),
						zap.String("cached_status", stripeStatus.Status),
						zap.String("source", "cache"))

					// 后台异步验证数据库状态（确保准确性，但不阻塞响应）
					if db.DB != nil {
						go func() {
							// 验证数据库状态是否与缓存一致
							dbPayment, err := db.GetPaymentByIntentID(paymentIntentID)
							if err == nil && dbPayment != nil {
								if dbPayment.Status != stripeStatus.Status {
									// 发现不一致，记录告警
									zap.L().Warn("Cache and database status mismatch detected",
										zap.String("payment_intent_id", paymentIntentID),
										zap.String("cached_status", stripeStatus.Status),
										zap.String("database_status", dbPayment.Status))
									// 更新缓存以数据库为准（数据库是权威来源）
									stripeStatusData := &cache.StripeStatusCacheData{
										PaymentIntentID: paymentIntentID,
										Status:          dbPayment.Status,
										Amount:          dbPayment.Amount,
										Currency:        dbPayment.Currency,
										CachedAt:        time.Now().Format(time.RFC3339),
									}
									cache.SetStripeStatus(context.Background(), paymentIntentID, stripeStatusData, cache.FinalStatusCacheTTL)
								}
							} else if err != nil {
								// 数据库查询失败，但缓存存在（可能是连接问题）
								// 记录日志但不影响，因为缓存是可靠的（Webhook 已设置）
								zap.L().Debug("Database verification failed (cache is still reliable)",
									zap.String("payment_intent_id", paymentIntentID),
									zap.Error(err))
							}
						}()
					}

					// 直接返回缓存中的最终状态（Webhook 已保证准确性）
					c.JSON(consts.StatusOK, utils.H{
						"payment_id":        paymentID,
						"payment_intent_id": stripeStatus.PaymentIntentID,
						"status":            stripeStatus.Status,
						"amount":            stripeStatus.Amount,
						"currency":          stripeStatus.Currency,
						"source":            "cache", // Webhook 已更新，保证准确性
						"cached":            true,
					})
					return
				} else {
					// 中间状态：可以使用缓存，但后台验证
					zap.L().Info("Stripe status cache hit (intermediate status, from database path)",
						zap.String("payment_intent_id", paymentIntentID),
						zap.String("status", stripeStatus.Status))

					// 检查是否有状态变化事件
					statusChangeEvent, _ := cache.GetStatusChangeEvent(ctx, paymentIntentID)
					hasStatusChange := statusChangeEvent != nil

					// 立即返回缓存，后台验证
					response := utils.H{
						"payment_id":        paymentID,
						"payment_intent_id": stripeStatus.PaymentIntentID,
						"status":            stripeStatus.Status,
						"amount":            stripeStatus.Amount,
						"currency":          stripeStatus.Currency,
						"source":            "database+cache",
						"cached":            true,
					}

					// 如果有状态变化，提示客户端重新查询
					if hasStatusChange {
						response["status_changed"] = true
						response["new_status"] = statusChangeEvent.NewStatus
						response["status_changed_at"] = statusChangeEvent.ChangedAt
						response["message"] = "Payment status has changed. Please query again for the latest status."
					}

					c.JSON(consts.StatusOK, response)

					// 后台异步验证
					go func() {
						intent, err := getPaymentService().GetPaymentIntent(paymentIntentID)
						if err == nil {
							// 如果状态发生变化，记录状态变化事件
							if string(intent.Status) != stripeStatus.Status {
								cache.RecordStatusChange(context.Background(),
									paymentIntentID,
									stripeStatus.Status,
									string(intent.Status),
									"revalidate")

								// 更新数据库状态
								if db.DB != nil {
									db.UpdatePaymentStatus(paymentIntentID, string(intent.Status))
								}
							}
							updateStripeStatusCache(context.Background(), paymentIntentID, intent)
							if payment != nil {
								updateCacheFromStripe(context.Background(), paymentID, paymentIntentID, intent)
							}
						}
					}()
					return
				}
			}
		}

		// 3.2 查询 Stripe API 获取最新状态（保证准确性）
		common.LogStage(c, "querying_stripe_api", zap.String("payment_intent_id", paymentIntentID))
		intent, err := getPaymentService().GetPaymentIntent(paymentIntentID)
		if err != nil {
			common.LogStageWithLevel(c, zapcore.WarnLevel, "stripe_query_failed", zap.Error(err), zap.String("payment_intent_id", paymentIntentID))
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
				"cached":            false,
			})
			return
		}

		// 3.3 成功从Stripe获取，更新缓存（根据状态决定缓存策略）
		if cache.IsAvailable() {
			go func() {
				updateStripeStatusCache(context.Background(), paymentIntentID, intent)
				if payment != nil {
					updateCacheFromStripe(context.Background(), paymentID, paymentIntentID, intent)
				}
			}()
		}

		c.JSON(consts.StatusOK, utils.H{
			"payment_id":        paymentID,
			"payment_intent_id": intent.ID,
			"status":            intent.Status,
			"amount":            intent.Amount,
			"currency":          intent.Currency,
			"source":            "database+stripe",
			"cached":            false,
		})
		return
	}

	// 4. 如果payment_id看起来像Stripe的payment_intent_id（以pi_开头），但前面没找到，检查缓存并查询Stripe
	if len(paymentID) > 3 && paymentID[:3] == "pi_" && paymentIntentID == "" {
		paymentIntentID = paymentID

		// 4.1 先检查 Stripe 状态缓存（优化：信任 Webhook 设置的最终状态缓存）
		if cache.IsAvailable() {
			stripeStatus, err := cache.GetStripeStatus(ctx, paymentIntentID)
			if err == nil && stripeStatus != nil {
				// 优化：如果缓存中有最终状态，信任缓存（Webhook 已设置），直接返回，避免 Stripe API 调用
				// 同时后台验证数据库状态，确保准确性
				if cache.IsFinalStatus(stripeStatus.Status) {
					zap.L().Info("Final status in cache (webhook guaranteed), returning directly to avoid Stripe API call",
						zap.String("payment_intent_id", paymentIntentID),
						zap.String("cached_status", stripeStatus.Status),
						zap.String("source", "cache"))

					// 后台异步验证数据库状态（确保准确性，但不阻塞响应）
					if db.DB != nil {
						go func() {
							// 验证数据库状态是否与缓存一致
							dbPayment, err := db.GetPaymentByIntentID(paymentIntentID)
							if err == nil && dbPayment != nil {
								if dbPayment.Status != stripeStatus.Status {
									// 发现不一致，记录告警
									zap.L().Warn("Cache and database status mismatch detected",
										zap.String("payment_intent_id", paymentIntentID),
										zap.String("cached_status", stripeStatus.Status),
										zap.String("database_status", dbPayment.Status))
									// 更新缓存以数据库为准（数据库是权威来源）
									stripeStatusData := &cache.StripeStatusCacheData{
										PaymentIntentID: paymentIntentID,
										Status:          dbPayment.Status,
										Amount:          dbPayment.Amount,
										Currency:        dbPayment.Currency,
										CachedAt:        time.Now().Format(time.RFC3339),
									}
									cache.SetStripeStatus(context.Background(), paymentIntentID, stripeStatusData, cache.FinalStatusCacheTTL)
								}
							} else if err != nil {
								// 数据库查询失败，但缓存存在（可能是连接问题）
								// 记录日志但不影响，因为缓存是可靠的（Webhook 已设置）
								zap.L().Debug("Database verification failed (cache is still reliable)",
									zap.String("payment_intent_id", paymentIntentID),
									zap.Error(err))
							}
						}()
					}

					// 直接返回缓存中的最终状态（Webhook 已保证准确性）
					c.JSON(consts.StatusOK, utils.H{
						"payment_id":        paymentID,
						"payment_intent_id": stripeStatus.PaymentIntentID,
						"status":            stripeStatus.Status,
						"amount":            stripeStatus.Amount,
						"currency":          stripeStatus.Currency,
						"source":            "cache", // Webhook 已更新，保证准确性
						"cached":            true,
					})
					return
				} else {
					// 中间状态：可以使用缓存，但后台验证
					zap.L().Info("Stripe status cache hit (intermediate status, direct payment_intent_id)",
						zap.String("payment_intent_id", paymentIntentID),
						zap.String("status", stripeStatus.Status))

					// 检查是否有状态变化事件
					statusChangeEvent, _ := cache.GetStatusChangeEvent(ctx, paymentIntentID)
					hasStatusChange := statusChangeEvent != nil

					response := utils.H{
						"payment_id":        paymentID,
						"payment_intent_id": stripeStatus.PaymentIntentID,
						"status":            stripeStatus.Status,
						"amount":            stripeStatus.Amount,
						"currency":          stripeStatus.Currency,
						"source":            "cache",
						"cached":            true,
					}

					// 如果有状态变化，提示客户端重新查询
					if hasStatusChange {
						response["status_changed"] = true
						response["new_status"] = statusChangeEvent.NewStatus
						response["status_changed_at"] = statusChangeEvent.ChangedAt
						response["message"] = "Payment status has changed. Please query again for the latest status."
					}

					c.JSON(consts.StatusOK, response)

					// 后台异步验证
					go func() {
						intent, err := getPaymentService().GetPaymentIntent(paymentIntentID)
						if err == nil {
							// 如果状态发生变化，记录状态变化事件
							if string(intent.Status) != stripeStatus.Status {
								cache.RecordStatusChange(context.Background(),
									paymentIntentID,
									stripeStatus.Status,
									string(intent.Status),
									"revalidate")
							}
							updateStripeStatusCache(context.Background(), paymentIntentID, intent)
						}
					}()
					return
				}
			}
		}

		// 4.2 查询 Stripe API 获取最新状态（保证准确性）
		intent, err := getPaymentService().GetPaymentIntent(paymentIntentID)
		if err != nil {
			common.SendError(c, common.ErrPaymentNotFound)
			return
		}

		// 4.3 更新缓存（根据状态决定缓存策略）
		if cache.IsAvailable() {
			go func() {
				updateStripeStatusCache(context.Background(), paymentIntentID, intent)
			}()
		}

		c.JSON(consts.StatusOK, utils.H{
			"payment_id":        paymentID,
			"payment_intent_id": intent.ID,
			"status":            intent.Status,
			"amount":            intent.Amount,
			"currency":          intent.Currency,
			"source":            "stripe",
			"cached":            false,
		})
		return
	}

	// 5. 既不是数据库中的payment_id，也不是Stripe的payment_intent_id
	common.SendError(c, common.ErrPaymentNotFound.WithDetails("The payment with the given ID was not found in the database or Stripe"))
}

// CheckStatusChange 检查支付状态是否发生变化（用于客户端轮询）
// GET /api/v1/payment/status-change/:payment_intent_id
func CheckStatusChange(ctx context.Context, c *app.RequestContext) {
	paymentIntentID := string(c.Param("payment_intent_id"))
	if paymentIntentID == "" {
		common.SendError(c, common.ErrMissingParameter.WithDetails("payment_intent_id is required"))
		return
	}

	// 检查是否有状态变化事件
	statusChangeEvent, err := cache.GetStatusChangeEvent(ctx, paymentIntentID)
	if err != nil {
		zap.L().Warn("Failed to get status change event", zap.Error(err), zap.String("payment_intent_id", paymentIntentID))
	}

	if statusChangeEvent != nil {
		// 返回状态变化信息
		c.JSON(consts.StatusOK, utils.H{
			"payment_intent_id": paymentIntentID,
			"status_changed":    true,
			"old_status":        statusChangeEvent.OldStatus,
			"new_status":        statusChangeEvent.NewStatus,
			"changed_at":        statusChangeEvent.ChangedAt,
			"source":            statusChangeEvent.Source,
			"message":           "Payment status has changed",
		})

		// 清除状态变化事件（已通知客户端）
		cache.ClearStatusChangeEvent(ctx, paymentIntentID)
	} else {
		// 没有状态变化
		c.JSON(consts.StatusOK, utils.H{
			"payment_intent_id": paymentIntentID,
			"status_changed":    false,
			"message":           "No status change detected",
		})
	}
}

// updateStripeStatusCache 更新 Stripe 状态缓存（优化5: 所有缓存都必须设置失效时间）
func updateStripeStatusCache(ctx context.Context, paymentIntentID string, intent *stripe.PaymentIntent) {
	if !cache.IsAvailable() {
		return
	}

	status := string(intent.Status)

	// 优化5: 最终状态也设置短期缓存（必须设置失效时间，不允许永久缓存）
	if cache.IsFinalStatus(status) {
		// 最终状态：设置短期缓存（5分钟），必须设置失效时间
		zap.L().Debug("Final status detected, setting short-term cache (5 minutes)",
			zap.String("payment_intent_id", paymentIntentID),
			zap.String("status", status))

		stripeStatusData := &cache.StripeStatusCacheData{
			PaymentIntentID: paymentIntentID,
			Status:          status,
			Amount:          intent.Amount,
			Currency:        string(intent.Currency),
			CachedAt:        time.Now().Format(time.RFC3339),
		}
		// 优化5: 使用短期缓存（5分钟），必须设置失效时间
		cache.SetStripeStatus(ctx, paymentIntentID, stripeStatusData, cache.FinalStatusCacheTTL)
		return
	}

	// 中间状态：可以缓存
	if cache.IsIntermediateStatus(status) {
		stripeStatusData := &cache.StripeStatusCacheData{
			PaymentIntentID: intent.ID,
			Status:          status,
			Amount:          intent.Amount,
			Currency:        string(intent.Currency),
			CachedAt:        time.Now().Format(time.RFC3339),
		}
		// 使用根据状态计算的 TTL
		ttl := cache.GetStripeStatusTTL(status)
		cache.SetStripeStatus(ctx, paymentIntentID, stripeStatusData, ttl)
		zap.L().Debug("Stripe status cached (intermediate status)",
			zap.String("payment_intent_id", paymentIntentID),
			zap.String("status", status),
			zap.Duration("ttl", ttl))
	}
}

// updateCacheFromStripe 从 Stripe 更新缓存（优化5: 所有缓存都必须设置失效时间）
func updateCacheFromStripe(ctx context.Context, paymentID, paymentIntentID string, intent *stripe.PaymentIntent) {
	if !cache.IsAvailable() {
		return
	}

	// 更新 Stripe 状态缓存
	updateStripeStatusCache(ctx, paymentIntentID, intent)

	// 从数据库获取完整信息并更新支付缓存
	if db.DB != nil {
		payment, err := db.GetPaymentByPaymentID(paymentID)
		if err == nil && payment != nil {
			cacheData := &cache.PaymentCacheData{
				PaymentID:       payment.PaymentID,
				PaymentIntentID: intent.ID,
				UserID:          payment.UserID,
				Amount:          intent.Amount,
				Currency:        string(intent.Currency),
				Status:          string(intent.Status),
				PaymentMethod:   payment.PaymentMethod,
				Description:     payment.Description,
				CreatedAt:       payment.CreatedAt.Format(time.RFC3339),
				UpdatedAt:       time.Now().Format(time.RFC3339),
			}

			// 优化5: 根据状态决定缓存时间，最终状态使用短期缓存（必须设置失效时间）
			var ttl time.Duration
			if cache.IsFinalStatus(string(intent.Status)) {
				ttl = cache.FinalStatusCacheTTL // 最终状态：5分钟
			} else {
				ttl = cache.DefaultPaymentCacheTTL // 中间状态：30分钟
			}

			cache.SetPayment(ctx, paymentID, cacheData, ttl)
			cache.SetPaymentByIntentID(ctx, paymentIntentID, cacheData, ttl)
		}
	}
}

// formatAmount 格式化金额（临时，应该移到service）
func formatAmount(amount int64) string {
	dollars := float64(amount) / 100.0
	if dollars == float64(int64(dollars)) {
		return strconv.FormatInt(int64(dollars), 10)
	}
	return strconv.FormatFloat(dollars, 'f', 2, 64)
}

// handlePaymentSuccessBusinessLogic 处理支付成功后的业务逻辑
// 这个函数在 Webhook 中异步执行，不阻塞 Webhook 响应
func handlePaymentSuccessBusinessLogic(userID string, pi *stripe.PaymentIntent) {
	zap.L().Info("Processing payment success business logic",
		zap.String("user_id", userID),
		zap.String("payment_intent_id", pi.ID),
		zap.Int64("amount", pi.Amount),
	)

	// TODO: 在这里添加你的业务逻辑
	// 以下是示例，你可以根据实际需求修改或扩展：

	// 1. 激活用户服务/会员（示例）
	// ctx := context.Background()
	// activateUserService(ctx, userID, pi)

	// 2. 发送确认邮件（示例）
	// sendPaymentConfirmationEmail(userID, pi)

	// 3. 更新订单状态（示例）
	// updateOrderStatus(userID, pi)

	// 4. 发放积分或优惠券（示例）
	// grantRewards(userID, pi)

	// 5. 记录业务日志（示例）
	// logBusinessEvent("payment_success", userID, pi)

	zap.L().Info("Payment success business logic completed",
		zap.String("user_id", userID),
		zap.String("payment_intent_id", pi.ID),
	)
}

// handlePaymentFailedBusinessLogic 处理支付失败后的业务逻辑
func handlePaymentFailedBusinessLogic(userID string, pi *stripe.PaymentIntent) {
	ctx := context.Background()

	zap.L().Info("Processing payment failed business logic",
		zap.String("user_id", userID),
		zap.String("payment_intent_id", pi.ID),
	)

	// TODO: 在这里添加你的业务逻辑
	// 例如：
	// 1. 发送失败通知邮件
	// 2. 记录失败原因
	// 3. 引导用户重试

	_ = ctx // 避免未使用变量警告
	zap.L().Info("Payment failed business logic completed",
		zap.String("user_id", userID),
		zap.String("payment_intent_id", pi.ID),
	)
}

// handlePaymentCanceledBusinessLogic 处理支付取消后的业务逻辑
func handlePaymentCanceledBusinessLogic(userID string, pi *stripe.PaymentIntent) {
	ctx := context.Background()

	zap.L().Info("Processing payment canceled business logic",
		zap.String("user_id", userID),
		zap.String("payment_intent_id", pi.ID),
	)

	// TODO: 在这里添加你的业务逻辑
	// 例如：
	// 1. 释放库存
	// 2. 取消相关订单
	// 3. 发送取消通知

	_ = ctx // 避免未使用变量警告
	zap.L().Info("Payment canceled business logic completed",
		zap.String("user_id", userID),
		zap.String("payment_intent_id", pi.ID),
	)
}
