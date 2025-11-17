package handlers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"stripe-pay/biz"
	"stripe-pay/biz/models"
	"stripe-pay/biz/services"
	"stripe-pay/cache"
	"stripe-pay/common"
	"stripe-pay/conf"
	"stripe-pay/db"
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
)

var (
	paymentService     *services.PaymentService
	paymentServiceOnce sync.Once
)

// getPaymentService gets the payment service (lazy loading)
func getPaymentService() *services.PaymentService {
	paymentServiceOnce.Do(func() {
		paymentService = services.NewPaymentService()
	})
	return paymentService
}

// getIdempotencyKey gets the idempotency key from the request
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

// CreateStripePayment creates a Stripe payment
func CreateStripePayment(ctx context.Context, c *app.RequestContext) {
	var req models.CreatePaymentRequest
	if err := c.BindAndValidate(&req); err != nil {
		common.SendError(c, common.ErrInvalidRequest.WithDetails("Failed to bind request: "+err.Error()))
		return
	}

	// Explicitly validate required fields (because BindAndValidate may allow empty strings)
	if req.UserID == "" {
		common.SendError(c, common.ErrMissingParameter.WithDetails("user_id is required"))
		return
	}

	// Input validation
	if err := biz.ValidateUserID(req.UserID); err != nil {
		common.SendError(c, common.ErrValidationFailed.WithDetails(err.Error()))
		return
	}
	if err := biz.ValidateDescription(req.Description); err != nil {
		common.SendError(c, common.ErrValidationFailed.WithDetails(err.Error()))
		return
	}

	// Get Idempotency Key
	idempotencyKey := getIdempotencyKey(c)

	// Check idempotency
	existingPayment, err := getPaymentService().CheckIdempotency(ctx, idempotencyKey)
	if err != nil {
		zap.L().Error("Failed to check idempotency", zap.Error(err))
		// Continue execution, don't block the request
	} else if existingPayment != nil {
		zap.L().Info("Duplicate request detected, returning existing payment",
			zap.String("idempotency_key", idempotencyKey),
			zap.String("payment_intent_id", existingPayment.PaymentIntentID))
		c.JSON(consts.StatusOK, existingPayment)
		return
	}

	// Create payment
	response, err := getPaymentService().CreateStripePayment(ctx, &req, idempotencyKey)
	if err != nil {
		// Check if it's an already paid error
		if alreadyPaidErr, ok := err.(*services.AlreadyPaidError); ok {
			c.JSON(consts.StatusOK, utils.H{
				"already_paid":   true,
				"message":        "User has already paid successfully, no need to pay again",
				"user_info":      alreadyPaidErr.UserInfo,
				"days_remaining": alreadyPaidErr.DaysRemaining,
			})
			return
		}

		// Check if it's a validation error (should return 400 instead of 500)
		errStr := strings.ToLower(err.Error())
		if strings.Contains(errStr, "required") ||
			strings.Contains(errStr, "invalid") ||
			strings.Contains(errStr, "validation") {
			// This is a validation error, should return 400
			common.SendError(c, err) // WrapError will automatically identify and convert to the correct error type
			return
		}

		// Other errors as payment processing errors
		common.SendError(c, common.ErrPaymentProcessing.WithDetails(err.Error()))
		return
	}

	// Update cache (asynchronously)
	if cache.IsAvailable() && response.PaymentID != "" {
		go func() {
			// Get complete information from database and cache
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
	}

	c.JSON(consts.StatusOK, response)
}

// CreateStripeWeChatPayment creates a WeChat Pay payment
func CreateStripeWeChatPayment(ctx context.Context, c *app.RequestContext) {
	var req models.CreateWeChatPaymentRequest
	if err := c.BindAndValidate(&req); err != nil {
		common.SendError(c, common.ErrInvalidRequest.WithDetails("Failed to bind request"))
		return
	}

	idempotencyKey := getIdempotencyKey(c)

	// Check idempotency
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
			"message":           "Returning existing payment record",
		})
		return
	}

	// Create payment
	response, err := getPaymentService().CreateWeChatPayment(ctx, &req, idempotencyKey)
	if err != nil {
		common.SendError(c, common.ErrPaymentProcessing.WithDetails(err.Error()))
		return
	}

	c.JSON(consts.StatusOK, response)
}

// CreateStripeAlipayPayment creates an Alipay payment
func CreateStripeAlipayPayment(ctx context.Context, c *app.RequestContext) {
	var req models.CreateAlipayPaymentRequest
	if err := c.BindAndValidate(&req); err != nil {
		common.SendError(c, common.ErrInvalidRequest.WithDetails("Failed to bind request"))
		return
	}

	idempotencyKey := getIdempotencyKey(c)

	// Check idempotency
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
			"return_url":        req.ReturnURL,
			"message":           "Returning existing payment record",
		})
		return
	}

	// Create payment
	response, err := getPaymentService().CreateAlipayPayment(ctx, &req, idempotencyKey)
	if err != nil {
		common.SendError(c, common.ErrPaymentProcessing.WithDetails(err.Error()))
		return
	}

	c.JSON(consts.StatusOK, response)
}

// GetPricing gets pricing information
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

// ConfirmStripePayment confirms a payment
func ConfirmStripePayment(ctx context.Context, c *app.RequestContext) {
	var req models.ConfirmPaymentRequest
	if err := c.BindAndValidate(&req); err != nil {
		common.SendError(c, common.ErrInvalidRequest)
		return
	}

	// Enhanced input validation
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

// UpdatePaymentConfig updates payment configuration
func UpdatePaymentConfig(ctx context.Context, c *app.RequestContext) {
	var req models.UpdatePaymentConfigRequest
	if err := c.BindAndValidate(&req); err != nil {
		common.SendError(c, common.ErrInvalidRequest)
		return
	}

	// Enhanced input validation
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
		req.Description = "Payment amount configuration"
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

// GetPaymentConfig gets payment configuration
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

// GetUserPaymentInfo gets user payment information
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

// UpdatePaymentStatusFromFrontend updates payment status
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
			// Update cache (asynchronously)
			if cache.IsAvailable() {
				go func() {
					// Update Stripe status cache (cache strategy based on status)
					updateStripeStatusCache(context.Background(), req.PaymentIntentID, intent)

					// Find payment_id by payment_intent_id and update payment cache
					payment, err := db.GetPaymentByPaymentID(req.PaymentIntentID)
					if err == nil && payment == nil {
						// If not found by payment_intent_id, delete related cache
						cache.DeletePayment(context.Background(), req.PaymentIntentID)
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
			userID := intent.Metadata["user_id"]
			if userID != "" {
				if err := db.UpdateUserPaymentInfo(userID, intent.Amount); err != nil {
					zap.L().Warn("Failed to update user payment info", zap.Error(err))
				} else {
					// Invalidate user payment cache
					if cache.IsAvailable() {
						go func() {
							cache.InvalidateUserPaymentCache(context.Background(), userID)
						}()
					}
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

// GetUserPaymentHistory gets user payment history
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

// RefundPayment processes a refund
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

// StripeWebhook handles Stripe webhook
func StripeWebhook(ctx context.Context, c *app.RequestContext) {
	cfg := conf.GetConf()

	// Read request body
	body, err := io.ReadAll(c.Request.BodyStream())
	if err != nil {
		common.SendError(c, common.ErrInvalidRequest.WithDetails("Invalid request body"))
		return
	}

	// Get signature header
	signatureBytes := c.GetHeader("Stripe-Signature")
	if len(signatureBytes) == 0 {
		common.SendError(c, common.ErrInvalidRequest.WithDetails("Missing Stripe-Signature header"))
		return
	}
	signature := string(signatureBytes)

	// Verify signature using Stripe official library
	endpointSecret := cfg.Stripe.WebhookSecret
	if endpointSecret == "" {
		common.SendError(c, common.ErrInternalServer.WithDetails("Webhook secret not configured"))
		return
	}

	event, err := webhook.ConstructEvent(body, signature, endpointSecret)
	if err != nil {
		common.SendError(c, common.ErrInvalidRequest.WithDetails("Invalid signature"))
		return
	}

	// Handle different types of events
	switch event.Type {
	case "payment_intent.succeeded":
		zap.L().Info("Payment succeeded", zap.String("event_id", event.ID))

		// Parse PaymentIntent
		var pi stripe.PaymentIntent
		if err := json.Unmarshal(event.Data.Raw, &pi); err != nil {
			zap.L().Error("Failed to parse payment intent", zap.Error(err))
		} else {
			// Update payment status in database
			if db.DB != nil {
				// Update payment history status
				if err := db.UpdatePaymentStatus(pi.ID, string(pi.Status)); err != nil {
					zap.L().Warn("Failed to update payment status", zap.Error(err))
				} else {
					// Accuracy first: immediately update cache for final status to ensure accuracy
					if cache.IsAvailable() {
						go func() {
							// Final status: delete cache, force next query to Stripe to get latest status
							// This ensures the returned status is accurate
							if cache.IsFinalStatus(string(pi.Status)) {
								zap.L().Info("Final status in webhook, deleting cache for accuracy",
									zap.String("payment_intent_id", pi.ID),
									zap.String("status", string(pi.Status)))
								cache.DeleteStripeStatus(context.Background(), pi.ID)
								// Delete payment cache, force next read from database
								cache.DeletePayment(context.Background(), pi.ID)
							} else {
								// Intermediate status: update cache
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

				// Get user ID (from metadata)
				userID := pi.Metadata["user_id"]
				if userID != "" {
					// Update user payment information
					if err := db.UpdateUserPaymentInfo(userID, pi.Amount); err != nil {
						zap.L().Warn("Failed to update user payment info", zap.Error(err))
					} else {
						// Invalidate user payment cache
						if cache.IsAvailable() {
							go func() {
								cache.InvalidateUserPaymentCache(context.Background(), userID)
							}()
						}
					}
				}
			}
		}

	case "payment_intent.payment_failed":
		zap.L().Info("Payment failed", zap.String("event_id", event.ID))

		// Parse PaymentIntent and update status
		var pi stripe.PaymentIntent
		if err := json.Unmarshal(event.Data.Raw, &pi); err == nil && db.DB != nil {
			db.UpdatePaymentStatus(pi.ID, string(pi.Status))
			// Accuracy first: delete cache for final status (failed) to ensure accuracy
			if cache.IsAvailable() {
				go func() {
					zap.L().Info("Final status (failed) in webhook, deleting cache for accuracy",
						zap.String("payment_intent_id", pi.ID),
						zap.String("status", string(pi.Status)))
					cache.DeleteStripeStatus(context.Background(), pi.ID)
					cache.DeletePayment(context.Background(), pi.ID)
				}()
			}
		}

	case "payment_intent.canceled":
		zap.L().Info("Payment canceled", zap.String("event_id", event.ID))

		// Parse PaymentIntent and update status
		var pi stripe.PaymentIntent
		if err := json.Unmarshal(event.Data.Raw, &pi); err == nil && db.DB != nil {
			db.UpdatePaymentStatus(pi.ID, string(pi.Status))
			// Accuracy first: delete cache for final status (canceled) to ensure accuracy
			if cache.IsAvailable() {
				go func() {
					zap.L().Info("Final status (canceled) in webhook, deleting cache for accuracy",
						zap.String("payment_intent_id", pi.ID),
						zap.String("status", string(pi.Status)))
					cache.DeleteStripeStatus(context.Background(), pi.ID)
					cache.DeletePayment(context.Background(), pi.ID)
				}()
			}
		}

	default:
		zap.L().Info("Unhandled event type", zap.String("type", string(event.Type)))
	}

	c.JSON(consts.StatusOK, utils.H{"received": true})
}

// VerifyApplePurchase verifies Apple in-app purchase
func VerifyApplePurchase(ctx context.Context, c *app.RequestContext) {
	var req models.AppleVerifyRequest
	if err := c.BindAndValidate(&req); err != nil {
		common.SendError(c, common.ErrInvalidRequest)
		return
	}

	// Enhanced input validation
	if err := biz.ValidateReceiptData(req.ReceiptData); err != nil {
		common.SendError(c, common.ErrValidationFailed.WithDetails(err.Error()))
		return
	}

	cfg := conf.GetConf()

	// Prepare request data
	requestData := map[string]interface{}{
		"receipt-data": req.ReceiptData,
		"password":     cfg.Apple.SharedSecret,
	}

	jsonData, _ := json.Marshal(requestData)

	// Try production environment first
	prodResp, err := http.Post(cfg.Apple.ProductionURL, "application/json",
		io.NopCloser(strings.NewReader(string(jsonData))))
	if err != nil {
		common.SendError(c, common.ErrExternalService.WithDetails("Failed to connect to Apple"))
		return
	}
	defer prodResp.Body.Close()

	// If production environment returns 21007 (sandbox receipt), request sandbox environment
	var verifyResp models.AppleVerifyResponse
	if err := json.NewDecoder(prodResp.Body).Decode(&verifyResp); err != nil {
		common.SendError(c, common.ErrExternalService.WithDetails("Failed to parse response"))
		return
	}

	if verifyResp.Status == 21007 {
		// Sandbox receipt, use sandbox URL
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

// VerifyAppleSubscription verifies Apple subscription
func VerifyAppleSubscription(ctx context.Context, c *app.RequestContext) {
	// Similar to VerifyApplePurchase, specifically handles subscriptions
	// Additional subscription verification logic is needed in actual implementation
	VerifyApplePurchase(ctx, c)
}

// AppleWebhook handles Apple webhook
func AppleWebhook(ctx context.Context, c *app.RequestContext) {
	// Apple server-to-server notifications (App Store Server Notifications)
	// Need to handle Apple webhook notifications here
	zap.L().Info("Received Apple webhook")

	c.JSON(consts.StatusOK, utils.H{"received": true})
}

// GetPaymentStatus gets payment status (with Redis cache)
func GetPaymentStatus(ctx context.Context, c *app.RequestContext) {
	paymentID := string(c.Param("id"))
	if paymentID == "" {
		common.SendError(c, common.ErrMissingParameter.WithDetails("payment_id is required"))
		return
	}

	zap.L().Info("GetPaymentStatus called", zap.String("payment_id", paymentID))

	// 1. First try to get payment information from Redis cache
	if cache.IsAvailable() {
		cachedData, err := cache.GetPayment(ctx, paymentID)
		if err == nil && cachedData != nil {
			zap.L().Info("Payment found in cache", zap.String("payment_id", paymentID))

			// 1.1 First check Stripe status cache (accuracy-first strategy)
			if cachedData.PaymentIntentID != "" {
				stripeStatus, err := cache.GetStripeStatus(ctx, cachedData.PaymentIntentID)
				if err == nil && stripeStatus != nil {
					// Accuracy first: final status must be queried in real-time, do not use cache
					if cache.IsFinalStatus(stripeStatus.Status) {
						zap.L().Info("Final status detected, bypassing cache for accuracy",
							zap.String("payment_intent_id", cachedData.PaymentIntentID),
							zap.String("status", stripeStatus.Status))
						// Final status: must query Stripe to get latest status, do not use cache
						// Continue to Stripe API query below
					} else {
						// Intermediate status: can use cache, but adopt stale-while-revalidate strategy
						zap.L().Info("Stripe status cache hit (intermediate status)",
							zap.String("payment_intent_id", cachedData.PaymentIntentID),
							zap.String("status", stripeStatus.Status))

						// Check if there is a status change event
						statusChangeEvent, _ := cache.GetStatusChangeEvent(ctx, cachedData.PaymentIntentID)
						hasStatusChange := statusChangeEvent != nil

						// Immediately return cached data (fast response)
						response := utils.H{
							"payment_id":        cachedData.PaymentID,
							"payment_intent_id": stripeStatus.PaymentIntentID,
							"status":            stripeStatus.Status,
							"amount":            stripeStatus.Amount,
							"currency":          stripeStatus.Currency,
							"source":            "cache",
							"cached":            true, // Indicates this is cached data
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
			})
			return
		}
	}

	// 2. 缓存未命中，从数据库查询
	var paymentIntentID string
	var dbStatus string
	var dbAmount int64
	var dbCurrency string
	var payment *db.PaymentHistory

	if db.DB == nil {
		zap.L().Warn("Database not available for GetPaymentStatus", zap.String("payment_id", paymentID))
	} else {
		var err error
		payment, err = db.GetPaymentByPaymentID(paymentID)
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

			// Update cache (asynchronously)
			if cache.IsAvailable() && payment != nil {
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

	// 3. 如果找到了payment_intent_id，查询 Stripe 获取最新状态（准确性优先）
	if paymentIntentID != "" {
		// 3.1 先检查 Stripe 状态缓存（仅用于中间状态）
		if cache.IsAvailable() {
			stripeStatus, err := cache.GetStripeStatus(ctx, paymentIntentID)
			if err == nil && stripeStatus != nil {
				// 准确性优先：最终状态必须实时查询
				if cache.IsFinalStatus(stripeStatus.Status) {
					zap.L().Info("Final status in cache, querying Stripe for accuracy",
						zap.String("payment_intent_id", paymentIntentID),
						zap.String("cached_status", stripeStatus.Status))
					// 继续查询 Stripe 获取最新状态
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
		intent, err := getPaymentService().GetPaymentIntent(paymentIntentID)
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

	// 4. 如果payment_id看起来像Stripe的payment_intent_id（以pi_开头），查询 Stripe（准确性优先）
	if len(paymentID) > 3 && paymentID[:3] == "pi_" {
		// 4.1 先检查 Stripe 状态缓存（仅用于中间状态）
		if cache.IsAvailable() {
			stripeStatus, err := cache.GetStripeStatus(ctx, paymentID)
			if err == nil && stripeStatus != nil {
				// 准确性优先：最终状态必须实时查询
				if cache.IsFinalStatus(stripeStatus.Status) {
					zap.L().Info("Final status in cache, querying Stripe for accuracy",
						zap.String("payment_intent_id", paymentID),
						zap.String("cached_status", stripeStatus.Status))
					// 继续查询 Stripe
				} else {
					// 中间状态：可以使用缓存，但后台验证
					zap.L().Info("Stripe status cache hit (intermediate status, direct payment_intent_id)",
						zap.String("payment_intent_id", paymentID),
						zap.String("status", stripeStatus.Status))

					// 检查是否有状态变化事件
					statusChangeEvent, _ := cache.GetStatusChangeEvent(ctx, paymentID)
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
						intent, err := getPaymentService().GetPaymentIntent(paymentID)
						if err == nil {
							// 如果状态发生变化，记录状态变化事件
							if string(intent.Status) != stripeStatus.Status {
								cache.RecordStatusChange(context.Background(),
									paymentID,
									stripeStatus.Status,
									string(intent.Status),
									"revalidate")
							}
							updateStripeStatusCache(context.Background(), paymentID, intent)
						}
					}()
					return
				}
			}
		}

		// 4.2 查询 Stripe API 获取最新状态（保证准确性）
		intent, err := getPaymentService().GetPaymentIntent(paymentID)
		if err != nil {
			common.SendError(c, common.ErrPaymentNotFound)
			return
		}

		// 4.3 更新缓存（根据状态决定缓存策略）
		if cache.IsAvailable() {
			go func() {
				updateStripeStatusCache(context.Background(), paymentID, intent)
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

// updateStripeStatusCache 更新 Stripe 状态缓存（根据状态决定缓存策略）
func updateStripeStatusCache(ctx context.Context, paymentIntentID string, intent *stripe.PaymentIntent) {
	if !cache.IsAvailable() {
		return
	}

	status := string(intent.Status)

	// 准确性优先：最终状态不缓存或极短时间缓存
	if cache.IsFinalStatus(status) {
		// 最终状态：不缓存或极短时间缓存（5秒）
		// 这样可以避免返回过时的最终状态
		zap.L().Debug("Final status detected, using short TTL or no cache",
			zap.String("payment_intent_id", paymentIntentID),
			zap.String("status", status))

		// 可以选择不缓存，或者缓存极短时间
		// 这里选择不缓存最终状态，确保准确性
		cache.DeleteStripeStatus(ctx, paymentIntentID)
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

// updateCacheFromStripe 从 Stripe 更新缓存
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
			cache.SetPayment(ctx, paymentID, cacheData, cache.DefaultPaymentCacheTTL)
			cache.SetPaymentByIntentID(ctx, paymentIntentID, cacheData, cache.DefaultPaymentCacheTTL)
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
