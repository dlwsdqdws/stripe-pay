package common

import (
	"context"
	"fmt"
	"strings"
	"stripe-pay/cache"
	"stripe-pay/conf"
	"sync"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// RateLimitConfig 速率限制配置
type RateLimitConfig struct {
	Limit  int           // 请求次数限制
	Window time.Duration // 时间窗口
}

// RateLimitStrategy 速率限制策略
type RateLimitStrategy struct {
	Global    RateLimitConfig // 全局限制（按IP）
	Payment   RateLimitConfig // 支付接口限制（更严格）
	User      RateLimitConfig // 按用户ID限制
	Whitelist []string        // IP白名单（不受限制）
}

var (
	// 默认策略
	defaultStrategy = RateLimitStrategy{
		Global: RateLimitConfig{
			Limit:  100, // 每分钟100次
			Window: time.Minute,
		},
		Payment: RateLimitConfig{
			Limit:  10, // 支付接口每分钟10次
			Window: time.Minute,
		},
		User: RateLimitConfig{
			Limit:  50, // 每个用户每分钟50次
			Window: time.Minute,
		},
		Whitelist: []string{},
	}

	// 内存存储（当Redis不可用时使用）
	memoryStore = struct {
		sync.RWMutex
		requests map[string][]time.Time
	}{
		requests: make(map[string][]time.Time),
	}
)

// initRateLimitStrategy 从配置初始化速率限制策略
func initRateLimitStrategy() RateLimitStrategy {
	_ = conf.GetConf() // 预留配置读取
	strategy := defaultStrategy

	// 从配置读取（如果配置中有）
	// 这里可以扩展配置支持

	return strategy
}

// getRateLimitKey 生成速率限制键
func getRateLimitKey(identifier, path string) string {
	return fmt.Sprintf("ratelimit:%s:%s", identifier, path)
}

// isPaymentEndpoint 判断是否为支付相关接口
func isPaymentEndpoint(path string) bool {
	paymentPaths := []string{
		"/api/v1/stripe/create-payment",
		"/api/v1/stripe/create-wechat-payment",
		"/api/v1/stripe/create-alipay-payment",
		"/api/v1/stripe/confirm-payment",
		"/api/v1/stripe/refund",
		"/api/v1/payment/update-status",
		"/api/v1/payment/status",
		"/api/v1/payment/status-change",
	}

	pathLower := strings.ToLower(path)
	for _, paymentPath := range paymentPaths {
		if strings.Contains(pathLower, paymentPath) {
			return true
		}
	}
	return false
}

// getUserIDFromRequest 从请求中提取用户ID
func getUserIDFromRequest(c *app.RequestContext) string {
	// 尝试从请求体获取（需要解析JSON，这里简化处理）
	// 或者从URL参数获取
	userID := c.Param("user_id")
	if userID != "" {
		return userID
	}

	// 尝试从JWT token获取（如果实现了认证）
	if userID, ok := c.Get("user_id"); ok {
		if str, ok := userID.(string); ok {
			return str
		}
	}

	return ""
}

// checkRateLimitRedis 使用Redis检查速率限制
func checkRateLimitRedis(ctx context.Context, key string, limit int, window time.Duration) (bool, int, error) {
	if !cache.IsAvailable() {
		return false, 0, fmt.Errorf("redis not available")
	}

	client := cache.GetClient()
	if client == nil {
		return false, 0, fmt.Errorf("redis client not available")
	}

	// 使用滑动窗口算法
	now := time.Now()
	windowStart := now.Add(-window)

	// 获取当前计数
	count, err := client.ZCount(ctx, key,
		fmt.Sprintf("%d", windowStart.Unix()),
		fmt.Sprintf("%d", now.Unix())).Result()
	if err != nil {
		return false, 0, err
	}

	// 检查是否超过限制
	if int(count) >= limit {
		return true, int(count), nil // 超过限制
	}

	// 添加当前请求
	member := fmt.Sprintf("%d", now.UnixNano())
	score := float64(now.Unix())
	err = client.ZAdd(ctx, key, redis.Z{
		Score:  score,
		Member: member,
	}).Err()
	if err != nil {
		return false, 0, err
	}

	// 设置过期时间
	err = client.Expire(ctx, key, window).Err()
	if err != nil {
		zap.L().Warn("Failed to set rate limit key expiry", zap.Error(err))
	}

	// 清理过期记录
	err = client.ZRemRangeByScore(ctx, key,
		"0",
		fmt.Sprintf("%d", windowStart.Unix())).Err()
	if err != nil {
		zap.L().Warn("Failed to clean expired rate limit records", zap.Error(err))
	}

	return false, int(count) + 1, nil
}

// checkRateLimitMemory 使用内存检查速率限制
func checkRateLimitMemory(key string, limit int, window time.Duration) (bool, int) {
	memoryStore.Lock()
	defer memoryStore.Unlock()

	now := time.Now()
	windowStart := now.Add(-window)

	// 获取或初始化记录
	times, exists := memoryStore.requests[key]
	if !exists {
		times = []time.Time{}
	}

	// 清理过期记录
	validTimes := []time.Time{}
	for _, t := range times {
		if t.After(windowStart) {
			validTimes = append(validTimes, t)
		}
	}

	// 检查是否超过限制
	if len(validTimes) >= limit {
		return true, len(validTimes) // 超过限制
	}

	// 添加当前请求
	validTimes = append(validTimes, now)
	memoryStore.requests[key] = validTimes

	return false, len(validTimes)
}

// isWhitelisted 检查IP是否在白名单中
func isWhitelisted(ip string, whitelist []string) bool {
	for _, whiteIP := range whitelist {
		if ip == whiteIP {
			return true
		}
		// 支持CIDR格式（简化实现）
		if strings.Contains(whiteIP, "/") {
			// 这里可以添加CIDR匹配逻辑
			// 简化处理：只做精确匹配
		}
	}
	return false
}

// RateLimitMiddleware 速率限制中间件
func RateLimitMiddleware() app.HandlerFunc {
	strategy := initRateLimitStrategy()

	return func(ctx context.Context, c *app.RequestContext) {
		path := string(c.Path())
		clientIP := c.ClientIP()

		// 跳过健康检查端点
		if path == "/ping" || path == "/health" {
			c.Next(ctx)
			return
		}

		// 检查白名单
		if isWhitelisted(clientIP, strategy.Whitelist) {
			c.Next(ctx)
			return
		}

		// 确定使用的限制策略
		var config RateLimitConfig
		if isPaymentEndpoint(path) {
			config = strategy.Payment
		} else {
			config = strategy.Global
		}

		// 1. 按IP限制
		ipKey := getRateLimitKey(clientIP, path)
		exceeded := false
		count := 0
		var err error

		if cache.IsAvailable() {
			exceeded, count, err = checkRateLimitRedis(ctx, ipKey, config.Limit, config.Window)
			if err != nil {
				// Redis失败，降级到内存存储
				zap.L().Warn("Redis rate limit check failed, falling back to memory",
					zap.Error(err),
					zap.String("ip", clientIP))
				exceeded, count = checkRateLimitMemory(ipKey, config.Limit, config.Window)
			}
		} else {
			exceeded, count = checkRateLimitMemory(ipKey, config.Limit, config.Window)
		}

		if exceeded {
			// 记录速率限制命中指标
			RecordRateLimitHit("ip", path)

			zap.L().Warn("Rate limit exceeded by IP",
				zap.String("ip", clientIP),
				zap.String("path", path),
				zap.Int("count", count),
				zap.Int("limit", config.Limit))

			c.Header("X-RateLimit-Limit", fmt.Sprintf("%d", config.Limit))
			c.Header("X-RateLimit-Remaining", "0")
			c.Header("X-RateLimit-Reset", fmt.Sprintf("%d", time.Now().Add(config.Window).Unix()))
			c.Header("Retry-After", fmt.Sprintf("%d", int(config.Window.Seconds())))

			c.JSON(consts.StatusTooManyRequests, utils.H{
				"code":    "RATE_LIMIT_EXCEEDED",
				"message": "Rate limit exceeded. Please try again later.",
				"details": fmt.Sprintf("Maximum %d requests per %v allowed", config.Limit, config.Window),
			})
			c.Abort()
			return
		}

		// 2. 按用户ID限制（如果提供了用户ID）
		userID := getUserIDFromRequest(c)
		if userID != "" && strategy.User.Limit > 0 {
			userKey := getRateLimitKey(fmt.Sprintf("user:%s", userID), path)

			if cache.IsAvailable() {
				exceeded, count, err = checkRateLimitRedis(ctx, userKey, strategy.User.Limit, strategy.User.Window)
				if err != nil {
					exceeded, count = checkRateLimitMemory(userKey, strategy.User.Limit, strategy.User.Window)
				}
			} else {
				exceeded, count = checkRateLimitMemory(userKey, strategy.User.Limit, strategy.User.Window)
			}

			if exceeded {
				zap.L().Warn("Rate limit exceeded by user",
					zap.String("user_id", userID),
					zap.String("ip", clientIP),
					zap.String("path", path),
					zap.Int("count", count),
					zap.Int("limit", strategy.User.Limit))

				c.Header("X-RateLimit-Limit", fmt.Sprintf("%d", strategy.User.Limit))
				c.Header("X-RateLimit-Remaining", "0")
				c.Header("X-RateLimit-Reset", fmt.Sprintf("%d", time.Now().Add(strategy.User.Window).Unix()))
				c.Header("Retry-After", fmt.Sprintf("%d", int(strategy.User.Window.Seconds())))

				c.JSON(consts.StatusTooManyRequests, utils.H{
					"code":    "RATE_LIMIT_EXCEEDED",
					"message": "Rate limit exceeded. Please try again later.",
					"details": fmt.Sprintf("Maximum %d requests per %v allowed for this user", strategy.User.Limit, strategy.User.Window),
				})
				c.Abort()
				return
			}
		}

		// 设置响应头
		remaining := config.Limit - count
		if remaining < 0 {
			remaining = 0
		}
		c.Header("X-RateLimit-Limit", fmt.Sprintf("%d", config.Limit))
		c.Header("X-RateLimit-Remaining", fmt.Sprintf("%d", remaining))
		c.Header("X-RateLimit-Reset", fmt.Sprintf("%d", time.Now().Add(config.Window).Unix()))

		c.Next(ctx)
	}
}

// PaymentRateLimitMiddleware 支付接口专用速率限制（更严格）
func PaymentRateLimitMiddleware() app.HandlerFunc {
	strategy := initRateLimitStrategy()

	return func(ctx context.Context, c *app.RequestContext) {
		path := string(c.Path())
		clientIP := c.ClientIP()

		// 检查白名单
		if isWhitelisted(clientIP, strategy.Whitelist) {
			c.Next(ctx)
			return
		}

		config := strategy.Payment
		ipKey := getRateLimitKey(clientIP, path)
		exceeded := false
		count := 0
		var err error

		if cache.IsAvailable() {
			exceeded, count, err = checkRateLimitRedis(ctx, ipKey, config.Limit, config.Window)
			if err != nil {
				exceeded, count = checkRateLimitMemory(ipKey, config.Limit, config.Window)
			}
		} else {
			exceeded, count = checkRateLimitMemory(ipKey, config.Limit, config.Window)
		}

		if exceeded {
			zap.L().Warn("Payment rate limit exceeded",
				zap.String("ip", clientIP),
				zap.String("path", path),
				zap.Int("count", count),
				zap.Int("limit", config.Limit))

			c.Header("X-RateLimit-Limit", fmt.Sprintf("%d", config.Limit))
			c.Header("X-RateLimit-Remaining", "0")
			c.Header("X-RateLimit-Reset", fmt.Sprintf("%d", time.Now().Add(config.Window).Unix()))
			c.Header("Retry-After", fmt.Sprintf("%d", int(config.Window.Seconds())))

			c.JSON(consts.StatusTooManyRequests, utils.H{
				"code":    "RATE_LIMIT_EXCEEDED",
				"message": "Payment rate limit exceeded. Please try again later.",
				"details": fmt.Sprintf("Maximum %d payment requests per %v allowed", config.Limit, config.Window),
			})
			c.Abort()
			return
		}

		remaining := config.Limit - count
		if remaining < 0 {
			remaining = 0
		}
		c.Header("X-RateLimit-Limit", fmt.Sprintf("%d", config.Limit))
		c.Header("X-RateLimit-Remaining", fmt.Sprintf("%d", remaining))
		c.Header("X-RateLimit-Reset", fmt.Sprintf("%d", time.Now().Add(config.Window).Unix()))

		c.Next(ctx)
	}
}
