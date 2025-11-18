package handlers

import (
	"context"
	"fmt"
	"stripe-pay/cache"
	"stripe-pay/conf"
	"stripe-pay/db"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"go.uber.org/zap"
)

// HealthResponse 健康检查响应结构
type HealthResponse struct {
	Status    string            `json:"status"`
	Timestamp time.Time         `json:"timestamp"`
	Version   string            `json:"version"`
	Uptime    string            `json:"uptime"`
	Services  map[string]string `json:"services"`
}

var (
	startTime = time.Now()
	version   = "1.0.0" // 可以通过构建时注入：-ldflags "-X stripe-pay/biz/handlers.version=1.0.0"
)

// HealthCheck 健康检查处理器
func HealthCheck(ctx context.Context, c *app.RequestContext) {
	response := HealthResponse{
		Status:    "healthy",
		Timestamp: time.Now(),
		Version:   version,
		Uptime:    formatUptime(time.Since(startTime)),
		Services:  make(map[string]string),
	}

	// 检查数据库连接
	if db.DB != nil {
		ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()

		if err := db.DB.PingContext(ctx); err != nil {
			response.Status = "unhealthy"
			response.Services["database"] = "disconnected"
			zap.L().Error("Database health check failed", zap.Error(err))
		} else {
			response.Services["database"] = "connected"
		}
	} else {
		response.Services["database"] = "not configured"
	}

	// 检查 Redis 连接
	if cache.IsAvailable() {
		ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()

		client := cache.GetClient()
		if client != nil {
			if err := client.Ping(ctx).Err(); err != nil {
				response.Status = "unhealthy"
				response.Services["redis"] = "disconnected"
				zap.L().Error("Redis health check failed", zap.Error(err))
			} else {
				response.Services["redis"] = "connected"
			}
		} else {
			response.Services["redis"] = "not available"
		}
	} else {
		response.Services["redis"] = "not configured"
	}

	// 检查 Stripe 配置
	cfg := conf.GetConf()
	if cfg != nil && cfg.Stripe.SecretKey != "" {
		response.Services["stripe"] = "configured"
	} else {
		response.Status = "unhealthy"
		response.Services["stripe"] = "not configured"
		zap.L().Warn("Stripe secret key not configured")
	}

	// 根据状态返回相应的 HTTP 状态码
	statusCode := consts.StatusOK
	if response.Status == "unhealthy" {
		statusCode = consts.StatusServiceUnavailable
	}

	c.JSON(statusCode, response)
}

// formatUptime 格式化运行时间
func formatUptime(duration time.Duration) string {
	days := int(duration.Hours() / 24)
	hours := int(duration.Hours()) % 24
	minutes := int(duration.Minutes()) % 60
	seconds := int(duration.Seconds()) % 60

	if days > 0 {
		return fmt.Sprintf("%dd%dh%dm%ds", days, hours, minutes, seconds)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh%dm%ds", hours, minutes, seconds)
	}
	if minutes > 0 {
		return fmt.Sprintf("%dm%ds", minutes, seconds)
	}
	return fmt.Sprintf("%ds", seconds)
}
