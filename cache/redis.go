package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"stripe-pay/conf"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

var (
	client     *redis.Client
	clientOnce sync.Once
)

// Init 初始化 Redis 连接
func Init() error {
	var err error
	clientOnce.Do(func() {
		cfg := conf.GetConf()

		// 如果 Redis 未配置，跳过初始化
		if cfg.Redis.Address == "" {
			zap.L().Info("Redis not configured, caching disabled")
			return
		}

		client = redis.NewClient(&redis.Options{
			Addr:         fmt.Sprintf("%s:%d", cfg.Redis.Address, cfg.Redis.Port),
			Password:     cfg.Redis.Password,
			DB:           cfg.Redis.DB,
			DialTimeout:  time.Duration(cfg.Redis.DialTimeout) * time.Second,
			ReadTimeout:  time.Duration(cfg.Redis.ReadTimeout) * time.Second,
			WriteTimeout: time.Duration(cfg.Redis.WriteTimeout) * time.Second,
			PoolSize:     cfg.Redis.PoolSize,
			MinIdleConns: cfg.Redis.MinIdleConns,
		})

		// 测试连接
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err = client.Ping(ctx).Err(); err != nil {
			zap.L().Warn("Failed to connect to Redis, caching disabled", zap.Error(err))
			client = nil
			return
		}

		zap.L().Info("Redis connected successfully",
			zap.String("address", fmt.Sprintf("%s:%d", cfg.Redis.Address, cfg.Redis.Port)),
			zap.Int("db", cfg.Redis.DB))
	})
	return err
}

// Close 关闭 Redis 连接
func Close() error {
	if client != nil {
		return client.Close()
	}
	return nil
}

// IsAvailable 检查 Redis 是否可用
func IsAvailable() bool {
	return client != nil
}

// GetClient 获取 Redis 客户端（用于高级操作）
func GetClient() *redis.Client {
	return client
}

// 缓存键前缀
const (
	PaymentKeyPrefix        = "payment:"
	PaymentIntentKeyPrefix  = "payment_intent:"
	UserPaymentKeyPrefix    = "user_payment:"
	StripeStatusKeyPrefix   = "stripe_status:" // Stripe 状态缓存
	StatusChangeEventPrefix = "status_change:" // 状态变化事件
)

// PaymentCacheData 支付缓存数据结构
type PaymentCacheData struct {
	PaymentID       string `json:"payment_id"`
	PaymentIntentID string `json:"payment_intent_id"`
	UserID          string `json:"user_id"`
	Amount          int64  `json:"amount"`
	Currency        string `json:"currency"`
	Status          string `json:"status"`
	PaymentMethod   string `json:"payment_method"`
	Description     string `json:"description"`
	CreatedAt       string `json:"created_at"`
	UpdatedAt       string `json:"updated_at"`
}

// GetPayment 从缓存获取支付信息
func GetPayment(ctx context.Context, paymentID string) (*PaymentCacheData, error) {
	if !IsAvailable() {
		return nil, nil
	}

	key := PaymentKeyPrefix + paymentID
	val, err := client.Get(ctx, key).Result()
	if err == redis.Nil {
		return nil, nil // 缓存未命中
	}
	if err != nil {
		zap.L().Warn("Failed to get payment from cache", zap.Error(err), zap.String("payment_id", paymentID))
		return nil, err
	}

	var data PaymentCacheData
	if err := json.Unmarshal([]byte(val), &data); err != nil {
		zap.L().Warn("Failed to unmarshal payment cache", zap.Error(err), zap.String("payment_id", paymentID))
		return nil, err
	}

	zap.L().Debug("Payment cache hit", zap.String("payment_id", paymentID))
	return &data, nil
}

// SetPayment 设置支付信息到缓存
func SetPayment(ctx context.Context, paymentID string, data *PaymentCacheData, ttl time.Duration) error {
	if !IsAvailable() {
		return nil
	}

	key := PaymentKeyPrefix + paymentID
	val, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal payment cache: %w", err)
	}

	if err := client.Set(ctx, key, val, ttl).Err(); err != nil {
		zap.L().Warn("Failed to set payment cache", zap.Error(err), zap.String("payment_id", paymentID))
		return err
	}

	zap.L().Debug("Payment cached", zap.String("payment_id", paymentID), zap.Duration("ttl", ttl))
	return nil
}

// DeletePayment 删除支付缓存
func DeletePayment(ctx context.Context, paymentID string) error {
	if !IsAvailable() {
		return nil
	}

	key := PaymentKeyPrefix + paymentID
	if err := client.Del(ctx, key).Err(); err != nil {
		zap.L().Warn("Failed to delete payment cache", zap.Error(err), zap.String("payment_id", paymentID))
		return err
	}

	zap.L().Debug("Payment cache deleted", zap.String("payment_id", paymentID))
	return nil
}

// GetPaymentByIntentID 通过 payment_intent_id 从缓存获取
func GetPaymentByIntentID(ctx context.Context, paymentIntentID string) (*PaymentCacheData, error) {
	if !IsAvailable() {
		return nil, nil
	}

	key := PaymentIntentKeyPrefix + paymentIntentID
	val, err := client.Get(ctx, key).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		zap.L().Warn("Failed to get payment by intent_id from cache", zap.Error(err), zap.String("payment_intent_id", paymentIntentID))
		return nil, err
	}

	var data PaymentCacheData
	if err := json.Unmarshal([]byte(val), &data); err != nil {
		zap.L().Warn("Failed to unmarshal payment cache by intent_id", zap.Error(err))
		return nil, err
	}

	zap.L().Debug("Payment cache hit by intent_id", zap.String("payment_intent_id", paymentIntentID))
	return &data, nil
}

// SetPaymentByIntentID 通过 payment_intent_id 设置缓存
func SetPaymentByIntentID(ctx context.Context, paymentIntentID string, data *PaymentCacheData, ttl time.Duration) error {
	if !IsAvailable() {
		return nil
	}

	key := PaymentIntentKeyPrefix + paymentIntentID
	val, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal payment cache: %w", err)
	}

	if err := client.Set(ctx, key, val, ttl).Err(); err != nil {
		zap.L().Warn("Failed to set payment cache by intent_id", zap.Error(err))
		return err
	}

	zap.L().Debug("Payment cached by intent_id", zap.String("payment_intent_id", paymentIntentID))
	return nil
}

// InvalidateUserPaymentCache 使某个用户的支付缓存失效
func InvalidateUserPaymentCache(ctx context.Context, userID string) error {
	if !IsAvailable() {
		return nil
	}

	// 使用模式匹配删除所有相关缓存
	pattern := UserPaymentKeyPrefix + userID + ":*"
	keys, err := client.Keys(ctx, pattern).Result()
	if err != nil {
		zap.L().Warn("Failed to get user payment cache keys", zap.Error(err), zap.String("user_id", userID))
		return err
	}

	if len(keys) > 0 {
		if err := client.Del(ctx, keys...).Err(); err != nil {
			zap.L().Warn("Failed to delete user payment cache", zap.Error(err), zap.String("user_id", userID))
			return err
		}
		zap.L().Debug("User payment cache invalidated", zap.String("user_id", userID), zap.Int("keys_deleted", len(keys)))
	}

	return nil
}

// GetStripeStatus 从缓存获取 Stripe 状态
func GetStripeStatus(ctx context.Context, paymentIntentID string) (*StripeStatusCacheData, error) {
	if !IsAvailable() {
		return nil, nil
	}

	key := StripeStatusKeyPrefix + paymentIntentID
	val, err := client.Get(ctx, key).Result()
	if err == redis.Nil {
		return nil, nil // 缓存未命中
	}
	if err != nil {
		zap.L().Debug("Failed to get Stripe status from cache", zap.Error(err), zap.String("payment_intent_id", paymentIntentID))
		return nil, err
	}

	var data StripeStatusCacheData
	if err := json.Unmarshal([]byte(val), &data); err != nil {
		zap.L().Warn("Failed to unmarshal Stripe status cache", zap.Error(err), zap.String("payment_intent_id", paymentIntentID))
		return nil, err
	}

	zap.L().Debug("Stripe status cache hit", zap.String("payment_intent_id", paymentIntentID))
	return &data, nil
}

// SetStripeStatus 设置 Stripe 状态到缓存
func SetStripeStatus(ctx context.Context, paymentIntentID string, data *StripeStatusCacheData, ttl time.Duration) error {
	if !IsAvailable() {
		return nil
	}

	key := StripeStatusKeyPrefix + paymentIntentID
	val, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal Stripe status cache: %w", err)
	}

	if err := client.Set(ctx, key, val, ttl).Err(); err != nil {
		zap.L().Warn("Failed to set Stripe status cache", zap.Error(err), zap.String("payment_intent_id", paymentIntentID))
		return err
	}

	zap.L().Debug("Stripe status cached", zap.String("payment_intent_id", paymentIntentID), zap.Duration("ttl", ttl))
	return nil
}

// DeleteStripeStatus 删除 Stripe 状态缓存
func DeleteStripeStatus(ctx context.Context, paymentIntentID string) error {
	if !IsAvailable() {
		return nil
	}

	key := StripeStatusKeyPrefix + paymentIntentID
	if err := client.Del(ctx, key).Err(); err != nil {
		zap.L().Warn("Failed to delete Stripe status cache", zap.Error(err), zap.String("payment_intent_id", paymentIntentID))
		return err
	}

	zap.L().Debug("Stripe status cache deleted", zap.String("payment_intent_id", paymentIntentID))
	return nil
}

// StripeStatusCacheData Stripe 状态缓存数据结构
type StripeStatusCacheData struct {
	PaymentIntentID string `json:"payment_intent_id"`
	Status          string `json:"status"`
	Amount          int64  `json:"amount"`
	Currency        string `json:"currency"`
	CachedAt        string `json:"cached_at"` // 缓存时间戳
}

// IsFinalStatus 判断是否为最终状态（不应缓存或应立即失效）
func IsFinalStatus(status string) bool {
	finalStatuses := []string{
		"succeeded",        // 支付成功
		"failed",           // 支付失败
		"canceled",         // 支付取消
		"requires_capture", // 需要捕获（最终状态）
	}
	for _, s := range finalStatuses {
		if status == s {
			return true
		}
	}
	return false
}

// IsIntermediateStatus 判断是否为中间状态（可以缓存）
func IsIntermediateStatus(status string) bool {
	intermediateStatuses := []string{
		"requires_payment_method", // 需要支付方式
		"requires_confirmation",   // 需要确认
		"requires_action",         // 需要操作
		"processing",              // 处理中
	}
	for _, s := range intermediateStatuses {
		if status == s {
			return true
		}
	}
	return false
}

// GetStripeStatusTTL 根据状态获取合适的缓存过期时间
// 准确性优先：最终状态不缓存，中间状态短时间缓存
func GetStripeStatusTTL(status string) time.Duration {
	if IsFinalStatus(status) {
		// 最终状态：不缓存（返回0表示不缓存）
		// 或者返回极短时间（5秒），确保立即失效
		return 5 * time.Second
	}
	if IsIntermediateStatus(status) {
		// 中间状态：可以缓存较短时间（10秒）
		return 10 * time.Second
	}
	// 未知状态：默认不缓存，保证准确性
	return 5 * time.Second
}

// StatusChangeEvent 状态变化事件
type StatusChangeEvent struct {
	PaymentIntentID string `json:"payment_intent_id"`
	OldStatus       string `json:"old_status"`
	NewStatus       string `json:"new_status"`
	ChangedAt       string `json:"changed_at"`
	Source          string `json:"source"` // "revalidate" 或 "webhook"
}

// RecordStatusChange 记录状态变化事件
func RecordStatusChange(ctx context.Context, paymentIntentID, oldStatus, newStatus, source string) error {
	if !IsAvailable() {
		return nil
	}

	event := StatusChangeEvent{
		PaymentIntentID: paymentIntentID,
		OldStatus:       oldStatus,
		NewStatus:       newStatus,
		ChangedAt:       time.Now().Format(time.RFC3339),
		Source:          source,
	}

	key := StatusChangeEventPrefix + paymentIntentID
	val, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal status change event: %w", err)
	}

	// 状态变化事件保存 60 秒，足够客户端查询
	if err := client.Set(ctx, key, val, 60*time.Second).Err(); err != nil {
		zap.L().Warn("Failed to record status change event", zap.Error(err), zap.String("payment_intent_id", paymentIntentID))
		return err
	}

	zap.L().Info("Status change event recorded",
		zap.String("payment_intent_id", paymentIntentID),
		zap.String("old_status", oldStatus),
		zap.String("new_status", newStatus),
		zap.String("source", source))
	return nil
}

// GetStatusChangeEvent 获取状态变化事件
func GetStatusChangeEvent(ctx context.Context, paymentIntentID string) (*StatusChangeEvent, error) {
	if !IsAvailable() {
		return nil, nil
	}

	key := StatusChangeEventPrefix + paymentIntentID
	val, err := client.Get(ctx, key).Result()
	if err == redis.Nil {
		return nil, nil // 没有状态变化事件
	}
	if err != nil {
		zap.L().Debug("Failed to get status change event", zap.Error(err), zap.String("payment_intent_id", paymentIntentID))
		return nil, err
	}

	var event StatusChangeEvent
	if err := json.Unmarshal([]byte(val), &event); err != nil {
		zap.L().Warn("Failed to unmarshal status change event", zap.Error(err), zap.String("payment_intent_id", paymentIntentID))
		return nil, err
	}

	return &event, nil
}

// ClearStatusChangeEvent 清除状态变化事件（查询后清除）
func ClearStatusChangeEvent(ctx context.Context, paymentIntentID string) error {
	if !IsAvailable() {
		return nil
	}

	key := StatusChangeEventPrefix + paymentIntentID
	if err := client.Del(ctx, key).Err(); err != nil {
		zap.L().Warn("Failed to clear status change event", zap.Error(err), zap.String("payment_intent_id", paymentIntentID))
		return err
	}
	return nil
}

// 默认缓存过期时间
const (
	DefaultPaymentCacheTTL = 30 * time.Minute // 支付信息缓存30分钟
	DefaultUserCacheTTL    = 15 * time.Minute // 用户支付信息缓存15分钟
	DefaultStripeStatusTTL = 10 * time.Second // Stripe 状态缓存10秒（仅用于中间状态）
)
