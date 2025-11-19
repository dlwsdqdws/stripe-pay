package services

import (
	"context"
	"errors"
	"fmt"
	"stripe-pay/biz"
	"stripe-pay/biz/models"
	"stripe-pay/common"
	"stripe-pay/conf"
	"stripe-pay/db"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v78"
	"github.com/stripe/stripe-go/v78/paymentintent"
	"go.uber.org/zap"
)

// PaymentService 支付服务
type PaymentService struct {
	cfg *conf.Config
}

// NewPaymentService 创建支付服务
func NewPaymentService() *PaymentService {
	return &PaymentService{
		cfg: conf.GetConf(),
	}
}

// PricingInfo 定价信息
type PricingInfo struct {
	Amount   int64
	Currency string
	Label    string
}

// GetCurrentPricing 获取当前定价信息
func (s *PaymentService) GetCurrentPricing() (*PricingInfo, error) {
	// 从数据库读取配置
	if db.DB != nil {
		config, err := db.GetPaymentConfig("hkd")
		if err == nil && config != nil {
			label := "HK$" + formatAmount(config.Amount)
			return &PricingInfo{
				Amount:   config.Amount,
				Currency: config.Currency,
				Label:    label,
			}, nil
		}
		zap.L().Warn("Failed to get payment config from database, using default", zap.Error(err))
	}

	// 如果数据库读取失败，使用默认值
	return &PricingInfo{
		Amount:   5900,
		Currency: "hkd",
		Label:    "HK$59",
	}, nil
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

// CheckUserPaymentValidity 检查用户支付有效性（30天内有效）
func (s *PaymentService) CheckUserPaymentValidity(userID string) (*UserPaymentValidity, error) {
	if db.DB == nil {
		return &UserPaymentValidity{Valid: false}, nil
	}

	userInfo, err := db.GetUserPaymentInfo(userID)
	if err != nil {
		return nil, fmt.Errorf("failed to get user payment info: %w", err)
	}

	if userInfo == nil || !userInfo.HasPaid {
		return &UserPaymentValidity{Valid: false}, nil
	}

	// 检查上次支付时间是否在30天内
	if userInfo.LastPaymentAt == nil {
		return &UserPaymentValidity{Valid: false}, nil
	}

	daysSinceLastPayment := time.Since(*userInfo.LastPaymentAt).Hours() / 24
	if daysSinceLastPayment <= 30 {
		return &UserPaymentValidity{
			Valid:         true,
			DaysRemaining: int(30 - daysSinceLastPayment),
			UserInfo:      userInfo,
		}, nil
	}

	return &UserPaymentValidity{Valid: false}, nil
}

// UserPaymentValidity 用户支付有效性信息
type UserPaymentValidity struct {
	Valid         bool
	DaysRemaining int
	UserInfo      *db.UserPaymentInfo
}

// CheckIdempotency 检查幂等性，如果已存在则返回已存在的支付信息
func (s *PaymentService) CheckIdempotency(ctx context.Context, idempotencyKey string) (*models.PaymentResponse, error) {
	zap.L().Debug("Service: CheckIdempotency started", zap.String("idempotency_key", idempotencyKey))
	if idempotencyKey == "" || db.DB == nil {
		zap.L().Debug("Service: No idempotency key or DB unavailable, skipping check")
		return nil, nil
	}

	zap.L().Debug("Service: Querying database for existing payment", zap.String("idempotency_key", idempotencyKey))
	existingPayment, err := db.GetPaymentByIdempotencyKey(idempotencyKey)
	if err != nil {
		zap.L().Error("Service: Failed to check idempotency", zap.Error(err), zap.String("idempotency_key", idempotencyKey))
		return nil, nil // 继续执行，不阻止请求
	}

	if existingPayment == nil {
		zap.L().Debug("Service: No existing payment found, proceeding with new payment")
		return nil, nil
	}

	zap.L().Info("Service: Existing payment found",
		zap.String("payment_id", existingPayment.PaymentID),
		zap.String("payment_intent_id", existingPayment.PaymentIntentID))

	// 找到已存在的支付记录，从Stripe获取最新的PaymentIntent信息
	zap.L().Debug("Service: Fetching latest PaymentIntent from Stripe", zap.String("payment_intent_id", existingPayment.PaymentIntentID))
	stripe.Key = s.cfg.Stripe.SecretKey
	intent, err := paymentintent.Get(existingPayment.PaymentIntentID, nil)
	if err != nil {
		zap.L().Warn("Service: Failed to get payment intent from Stripe, returning cached data", zap.Error(err))
		// 如果获取失败，返回数据库中的信息（client_secret为空）
		return &models.PaymentResponse{
			ClientSecret:    "",
			PaymentID:       existingPayment.PaymentID,
			PaymentIntentID: existingPayment.PaymentIntentID,
		}, nil
	}

	zap.L().Info("Service: Successfully retrieved PaymentIntent from Stripe",
		zap.String("payment_intent_id", intent.ID),
		zap.String("status", string(intent.Status)))

	// 成功获取，返回完整的支付信息
	return &models.PaymentResponse{
		ClientSecret:    intent.ClientSecret,
		PaymentID:       existingPayment.PaymentID,
		PaymentIntentID: intent.ID,
	}, nil
}

// CreateStripePayment 创建Stripe支付
func (s *PaymentService) CreateStripePayment(ctx context.Context, req *models.CreatePaymentRequest, idempotencyKey string) (*models.PaymentResponse, error) {
	zap.L().Info("Service: CreateStripePayment started",
		zap.String("user_id", req.UserID),
		zap.String("idempotency_key", idempotencyKey))

	// 验证必需字段
	zap.L().Debug("Service: Validating required fields")
	if req.UserID == "" {
		zap.L().Warn("Service: user_id is required")
		return nil, fmt.Errorf("user_id is required")
	}

	// 验证输入
	zap.L().Debug("Service: Validating input fields")
	if err := biz.ValidateUserID(req.UserID); err != nil {
		zap.L().Warn("Service: Invalid user_id", zap.Error(err))
		return nil, fmt.Errorf("invalid user_id: %w", err)
	}
	if err := biz.ValidateDescription(req.Description); err != nil {
		zap.L().Warn("Service: Invalid description", zap.Error(err))
		return nil, fmt.Errorf("invalid description: %w", err)
	}

	// 检查用户支付有效性
	zap.L().Debug("Service: Checking user payment validity", zap.String("user_id", req.UserID))
	validity, err := s.CheckUserPaymentValidity(req.UserID)
	if err != nil {
		zap.L().Error("Service: Failed to check user payment validity", zap.Error(err))
		return nil, fmt.Errorf("failed to check user payment validity: %w", err)
	}
	if validity.Valid {
		zap.L().Info("Service: User already paid",
			zap.String("user_id", req.UserID),
			zap.Int("days_remaining", validity.DaysRemaining))
		return nil, &AlreadyPaidError{
			UserInfo:      validity.UserInfo,
			DaysRemaining: validity.DaysRemaining,
		}
	}
	zap.L().Debug("Service: User payment validity check passed")

	// 获取定价信息
	zap.L().Debug("Service: Getting current pricing")
	pricing, err := s.GetCurrentPricing()
	if err != nil {
		zap.L().Error("Service: Failed to get pricing", zap.Error(err))
		return nil, fmt.Errorf("failed to get pricing: %w", err)
	}
	zap.L().Debug("Service: Pricing retrieved",
		zap.Int64("amount", pricing.Amount),
		zap.String("currency", pricing.Currency))

	// 设置Stripe密钥
	zap.L().Debug("Service: Setting Stripe API key")
	stripe.Key = s.cfg.Stripe.SecretKey

	// 生成支付ID（在创建 PaymentIntent 之前，以便存入 metadata）
	paymentID := uuid.New().String()
	zap.L().Debug("Service: Generated payment ID", zap.String("payment_id", paymentID))

	// 创建 Payment Intent
	zap.L().Info("Service: Creating Stripe PaymentIntent",
		zap.Int64("amount", pricing.Amount),
		zap.String("currency", pricing.Currency),
		zap.String("idempotency_key", idempotencyKey),
		zap.String("payment_id", paymentID))
	params := &stripe.PaymentIntentParams{
		Amount:   stripe.Int64(pricing.Amount),
		Currency: stripe.String(pricing.Currency),
		Metadata: map[string]string{
			"user_id":     req.UserID,
			"description": req.Description,
			"payment_id":  paymentID, // 优化4: 将 payment_id 存入 metadata，Webhook 可直接获取
		},
		// 启用自动支付方式（包含 Apple Pay）
		AutomaticPaymentMethods: &stripe.PaymentIntentAutomaticPaymentMethodsParams{
			Enabled: stripe.Bool(true),
		},
	}

	// 如果提供了Idempotency Key，传递给Stripe
	if idempotencyKey != "" {
		params.IdempotencyKey = stripe.String(idempotencyKey)
	}

	startTime := time.Now()
	intent, err := paymentintent.New(params)
	duration := time.Since(startTime)

	if err != nil {
		zap.L().Error("Service: Failed to create Stripe PaymentIntent", zap.Error(err))
		// 记录支付失败指标
		common.RecordPayment("stripe", "failed", pricing.Amount, pricing.Currency, duration)
		return nil, fmt.Errorf("failed to create payment intent: %w", err)
	}

	// 记录支付创建指标
	common.RecordPayment("stripe", "created", pricing.Amount, pricing.Currency, duration)

	zap.L().Info("Service: Stripe PaymentIntent created",
		zap.String("payment_intent_id", intent.ID),
		zap.String("status", string(intent.Status)),
		zap.String("payment_id", paymentID))

	// 保存到数据库
	zap.L().Debug("Service: Saving payment to database",
		zap.String("payment_id", paymentID),
		zap.String("payment_intent_id", intent.ID))
	if db.DB != nil {
		metadata := map[string]string{
			"user_id":     req.UserID,
			"description": req.Description,
		}

		err = db.SavePaymentWithMetadata(
			intent.ID,
			paymentID,
			idempotencyKey,
			req.UserID,
			intent.Amount,
			string(intent.Currency),
			string(intent.Status),
			"card",
			req.Description,
			metadata,
		)

		if err != nil {
			// 检查是否是重复的idempotency_key（并发情况）
			if dupErr, ok := err.(*db.DuplicateIdempotencyKeyError); ok {
				zap.L().Info("Service: Concurrent request with same idempotency_key detected",
					zap.String("idempotency_key", dupErr.Key))
				// 查询已存在的支付记录并返回
				existingPayment, queryErr := db.GetPaymentByIdempotencyKey(dupErr.Key)
				if queryErr == nil && existingPayment != nil {
					// 从Stripe获取最新的PaymentIntent信息
					intent, getErr := paymentintent.Get(existingPayment.PaymentIntentID, nil)
					if getErr == nil {
						zap.L().Info("Service: Returning existing payment from concurrent request",
							zap.String("payment_id", existingPayment.PaymentID))
						return &models.PaymentResponse{
							ClientSecret:    intent.ClientSecret,
							PaymentID:       existingPayment.PaymentID,
							PaymentIntentID: intent.ID,
						}, nil
					}
				}
			}
			zap.L().Warn("Service: Failed to save payment to database", zap.Error(err))
		} else {
			zap.L().Info("Service: Payment saved to database successfully",
				zap.String("payment_id", paymentID),
				zap.String("payment_intent_id", intent.ID))
		}
	}

	zap.L().Info("Service: CreateStripePayment completed successfully",
		zap.String("payment_id", paymentID),
		zap.String("payment_intent_id", intent.ID))
	return &models.PaymentResponse{
		ClientSecret:    intent.ClientSecret,
		PaymentID:       paymentID,
		PaymentIntentID: intent.ID,
	}, nil
}

// AlreadyPaidError 用户已支付错误
type AlreadyPaidError struct {
	UserInfo      *db.UserPaymentInfo
	DaysRemaining int
}

func (e *AlreadyPaidError) Error() string {
	return fmt.Sprintf("user already paid, %d days remaining", e.DaysRemaining)
}

// CreateWeChatPayment 创建微信支付
func (s *PaymentService) CreateWeChatPayment(ctx context.Context, req *models.CreateWeChatPaymentRequest, idempotencyKey string) (map[string]interface{}, error) {
	// 验证输入
	if err := biz.ValidateUserID(req.UserID); err != nil {
		return nil, fmt.Errorf("invalid user_id: %w", err)
	}
	if err := biz.ValidateDescription(req.Description); err != nil {
		return nil, fmt.Errorf("invalid description: %w", err)
	}
	if err := biz.ValidateURL(req.ReturnURL); err != nil {
		return nil, fmt.Errorf("invalid return_url: %w", err)
	}
	if err := biz.ValidateClient(req.Client); err != nil {
		return nil, fmt.Errorf("invalid client: %w", err)
	}

	// 检查用户支付有效性
	validity, err := s.CheckUserPaymentValidity(req.UserID)
	if err != nil {
		return nil, fmt.Errorf("failed to check user payment validity: %w", err)
	}
	if validity.Valid {
		return map[string]interface{}{
			"already_paid":   true,
			"message":        "用户已支付成功，无需重复支付",
			"user_info":      validity.UserInfo,
			"days_remaining": validity.DaysRemaining,
		}, nil
	}

	// 获取定价信息
	pricing, err := s.GetCurrentPricing()
	if err != nil {
		return nil, fmt.Errorf("failed to get pricing: %w", err)
	}

	// 设置Stripe密钥
	stripe.Key = s.cfg.Stripe.SecretKey

	// 优化4: 生成支付ID（在创建 PaymentIntent 之前，以便存入 metadata）
	paymentID := uuid.New().String()
	zap.L().Debug("Service: Generated payment ID for WeChat payment", zap.String("payment_id", paymentID))

	client := strings.ToLower(strings.TrimSpace(req.Client))
	if client == "" {
		client = "web"
	}

	params := &stripe.PaymentIntentParams{
		Amount:             stripe.Int64(pricing.Amount),
		Currency:           stripe.String(pricing.Currency),
		PaymentMethodTypes: stripe.StringSlice([]string{"wechat_pay"}),
		Metadata: map[string]string{
			"user_id":     req.UserID,
			"description": req.Description,
			"payment_id":  paymentID, // 优化4: 将 payment_id 存入 metadata
		},
		PaymentMethodOptions: &stripe.PaymentIntentPaymentMethodOptionsParams{
			WeChatPay: &stripe.PaymentIntentPaymentMethodOptionsWeChatPayParams{
				Client: stripe.String(client),
			},
		},
	}

	if req.ReturnURL != "" {
		params.ReturnURL = stripe.String(req.ReturnURL)
	}

	if idempotencyKey != "" {
		params.IdempotencyKey = stripe.String(idempotencyKey)
	}

	intent, err := paymentintent.New(params)
	if err != nil {
		return nil, fmt.Errorf("failed to create wechat payment intent: %w", err)
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
			paymentID, // 使用之前生成的 paymentID
			idempotencyKey,
			req.UserID,
			intent.Amount,
			string(intent.Currency),
			string(intent.Status),
			"wechat_pay",
			req.Description,
			metadata,
		)
		if err != nil {
			if dupErr, ok := err.(*db.DuplicateIdempotencyKeyError); ok {
				existingPayment, queryErr := db.GetPaymentByIdempotencyKey(dupErr.Key)
				if queryErr == nil && existingPayment != nil {
					intent, getErr := paymentintent.Get(existingPayment.PaymentIntentID, nil)
					if getErr == nil {
						return map[string]interface{}{
							"client_secret":     intent.ClientSecret,
							"payment_intent_id": intent.ID,
							"status":            intent.Status,
							"message":           "返回已存在的支付记录",
						}, nil
					}
				}
			}
			zap.L().Warn("Failed to save wechat payment to database", zap.Error(err))
		}
	}

	return map[string]interface{}{
		"client_secret":     intent.ClientSecret,
		"payment_intent_id": intent.ID,
		"status":            intent.Status,
		"message":           "请使用 Stripe.js 在前端确认支付以生成二维码",
	}, nil
}

// GetPaymentIntent 从Stripe获取PaymentIntent
func (s *PaymentService) GetPaymentIntent(paymentIntentID string) (*stripe.PaymentIntent, error) {
	stripe.Key = s.cfg.Stripe.SecretKey
	intent, err := paymentintent.Get(paymentIntentID, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get payment intent: %w", err)
	}
	return intent, nil
}

// ValidatePaymentRequest 验证支付请求
func (s *PaymentService) ValidatePaymentRequest(req *models.CreatePaymentRequest) error {
	if err := biz.ValidateUserID(req.UserID); err != nil {
		return err
	}
	if err := biz.ValidateDescription(req.Description); err != nil {
		return err
	}
	return nil
}

// IsNotFoundError 检查是否是未找到错误
func IsNotFoundError(err error) bool {
	return errors.Is(err, ErrPaymentNotFound)
}

// ErrPaymentNotFound 支付未找到错误
var ErrPaymentNotFound = errors.New("payment not found")
