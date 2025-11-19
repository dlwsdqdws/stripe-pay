package common

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"stripe-pay/conf"
	"sync"

	"github.com/cloudwego/hertz/pkg/app"
	"go.uber.org/zap"
)

// AuthConfig 认证配置
type AuthConfig struct {
	Enabled      bool     // 是否启用认证
	APIKeys      []string // 允许的 API Key 列表
	JWTSecret    string   // JWT 密钥
	JWTExpire    int      // JWT 过期时间（小时）
	PublicPaths  []string // 公开路径（不需要认证）
	AdminAPIKeys []string // 管理员 API Key（用于管理员接口）
}

var (
	// 默认认证配置
	defaultAuthConfig = AuthConfig{
		Enabled:      true,
		APIKeys:      []string{},
		PublicPaths:  []string{"/ping", "/health", "/metrics"},
		AdminAPIKeys: []string{},
	}

	// API Key 缓存（用于快速验证）
	apiKeyCache = struct {
		sync.RWMutex
		keys map[string]bool
	}{
		keys: make(map[string]bool),
	}

	// 管理员 API Key 缓存
	adminKeyCache = struct {
		sync.RWMutex
		keys map[string]bool
	}{
		keys: make(map[string]bool),
	}
)

// InitAuth 初始化认证配置
func InitAuth() {
	_ = conf.GetConf() // 预留配置读取

	// 从配置读取 API Keys
	// 支持从环境变量读取
	apiKeys := []string{}
	if envKeys := strings.TrimSpace(getEnv("API_KEYS", "")); envKeys != "" {
		// 支持逗号分隔的多个 API Key
		keys := strings.Split(envKeys, ",")
		for _, key := range keys {
			key = strings.TrimSpace(key)
			if key != "" {
				apiKeys = append(apiKeys, key)
			}
		}
	}

	// 从配置文件读取（如果配置中有）
	// 这里可以扩展从配置文件读取

	// 更新缓存
	apiKeyCache.Lock()
	apiKeyCache.keys = make(map[string]bool)
	for _, key := range apiKeys {
		apiKeyCache.keys[key] = true
	}
	apiKeyCache.Unlock()

	// 管理员 API Keys
	adminKeys := []string{}
	if envAdminKeys := strings.TrimSpace(getEnv("ADMIN_API_KEYS", "")); envAdminKeys != "" {
		keys := strings.Split(envAdminKeys, ",")
		for _, key := range keys {
			key = strings.TrimSpace(key)
			if key != "" {
				adminKeys = append(adminKeys, key)
			}
		}
	}

	adminKeyCache.Lock()
	adminKeyCache.keys = make(map[string]bool)
	for _, key := range adminKeys {
		adminKeyCache.keys[key] = true
	}
	adminKeyCache.Unlock()

	zap.L().Info("Auth initialized",
		zap.Int("api_keys_count", len(apiKeys)),
		zap.Int("admin_keys_count", len(adminKeys)),
		zap.Bool("enabled", defaultAuthConfig.Enabled))
}

// getEnv 获取环境变量（辅助函数）
func getEnv(key, defaultValue string) string {
	// 这里可以扩展从配置文件读取
	// 目前从环境变量读取
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return defaultValue
	}
	return value
}

// GenerateAPIKey 生成新的 API Key
func GenerateAPIKey() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("failed to generate API key: %w", err)
	}
	return base64.URLEncoding.EncodeToString(bytes), nil
}

// IsPublicPath 检查路径是否为公开路径
func IsPublicPath(path string) bool {
	for _, publicPath := range defaultAuthConfig.PublicPaths {
		if path == publicPath || strings.HasPrefix(path, publicPath+"/") {
			return true
		}
	}
	return false
}

// IsWebhookPath 检查路径是否为 Webhook 路径（Webhook 有自己的签名验证）
func IsWebhookPath(path string) bool {
	webhookPaths := []string{
		"/api/v1/stripe/webhook",
		"/api/v1/apple/webhook",
	}
	for _, webhookPath := range webhookPaths {
		if path == webhookPath || strings.HasPrefix(path, webhookPath+"/") {
			return true
		}
	}
	return false
}

// ValidateAPIKey 验证 API Key
func ValidateAPIKey(apiKey string) bool {
	if apiKey == "" {
		return false
	}

	apiKeyCache.RLock()
	defer apiKeyCache.RUnlock()

	return apiKeyCache.keys[apiKey]
}

// ValidateAdminAPIKey 验证管理员 API Key
func ValidateAdminAPIKey(apiKey string) bool {
	if apiKey == "" {
		return false
	}

	// 管理员 API Key 也包含普通 API Key 的权限
	if ValidateAPIKey(apiKey) {
		return true
	}

	adminKeyCache.RLock()
	defer adminKeyCache.RUnlock()

	return adminKeyCache.keys[apiKey]
}

// ExtractAPIKey 从请求中提取 API Key
func ExtractAPIKey(c *app.RequestContext) string {
	// 方式1: 从 X-API-Key Header 获取
	apiKey := string(c.GetHeader("X-API-Key"))
	if apiKey != "" {
		return apiKey
	}

	// 方式2: 从 Authorization Header 获取 (Bearer <token>)
	authHeader := string(c.GetHeader("Authorization"))
	if authHeader != "" {
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) == 2 && strings.ToLower(parts[0]) == "bearer" {
			return parts[1]
		}
		// 如果没有 Bearer 前缀，直接使用整个值作为 API Key
		return authHeader
	}

	// 方式3: 从查询参数获取（不推荐，但为了兼容性支持）
	apiKey = c.Query("api_key")
	if apiKey != "" {
		return apiKey
	}

	return ""
}

// AuthMiddleware 认证中间件
func AuthMiddleware() app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		path := string(c.Path())

		// 检查是否为公开路径
		if IsPublicPath(path) {
			c.Next(ctx)
			return
		}

		// Webhook 路径有自己的签名验证，跳过认证中间件
		if IsWebhookPath(path) {
			c.Next(ctx)
			return
		}

		// 如果认证未启用，直接通过
		if !defaultAuthConfig.Enabled {
			c.Next(ctx)
			return
		}

		// 提取 API Key
		apiKey := ExtractAPIKey(c)
		if apiKey == "" {
			zap.L().Warn("API key missing",
				zap.String("path", path),
				zap.String("ip", c.ClientIP()))
			SendError(c, ErrUnauthorized.WithDetails("API key is required. Please provide X-API-Key header or Authorization: Bearer <api_key>"))
			c.Abort()
			return
		}

		// 验证 API Key
		if !ValidateAPIKey(apiKey) {
			zap.L().Warn("Invalid API key",
				zap.String("path", path),
				zap.String("ip", c.ClientIP()),
				zap.String("api_key_prefix", maskAPIKey(apiKey)))
			SendError(c, ErrUnauthorized.WithDetails("Invalid API key"))
			c.Abort()
			return
		}

		// 将 API Key 存储到上下文，供后续使用
		c.Set("api_key", apiKey)

		zap.L().Debug("API key validated",
			zap.String("path", path),
			zap.String("api_key_prefix", maskAPIKey(apiKey)))

		c.Next(ctx)
	}
}

// AdminAuthMiddleware 管理员认证中间件（用于管理员接口）
func AdminAuthMiddleware() app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		path := string(c.Path())

		// 如果认证未启用，直接通过
		if !defaultAuthConfig.Enabled {
			c.Next(ctx)
			return
		}

		// 提取 API Key
		apiKey := ExtractAPIKey(c)
		if apiKey == "" {
			zap.L().Warn("Admin API key missing",
				zap.String("path", path),
				zap.String("ip", c.ClientIP()))
			SendError(c, ErrUnauthorized.WithDetails("Admin API key is required"))
			c.Abort()
			return
		}

		// 验证管理员 API Key
		if !ValidateAdminAPIKey(apiKey) {
			zap.L().Warn("Invalid admin API key",
				zap.String("path", path),
				zap.String("ip", c.ClientIP()),
				zap.String("api_key_prefix", maskAPIKey(apiKey)))
			SendError(c, ErrForbidden.WithDetails("Admin access required"))
			c.Abort()
			return
		}

		// 将 API Key 存储到上下文
		c.Set("api_key", apiKey)
		c.Set("is_admin", true)

		zap.L().Debug("Admin API key validated",
			zap.String("path", path),
			zap.String("api_key_prefix", maskAPIKey(apiKey)))

		c.Next(ctx)
	}
}

// maskAPIKey 掩码 API Key（用于日志，只显示前4位和后4位）
func maskAPIKey(apiKey string) string {
	if len(apiKey) <= 8 {
		return "****"
	}
	return apiKey[:4] + "..." + apiKey[len(apiKey)-4:]
}

// GetAPIKeyFromContext 从上下文获取 API Key
func GetAPIKeyFromContext(c *app.RequestContext) string {
	if key, ok := c.Get("api_key"); ok {
		if str, ok := key.(string); ok {
			return str
		}
	}
	return ""
}

// IsAdminFromContext 从上下文检查是否为管理员
func IsAdminFromContext(c *app.RequestContext) bool {
	if isAdmin, ok := c.Get("is_admin"); ok {
		if admin, ok := isAdmin.(bool); ok {
			return admin
		}
	}
	return false
}
