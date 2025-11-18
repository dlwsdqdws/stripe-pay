package main

import (
	"context"
	"fmt"
	"stripe-pay/biz/handlers"
	"stripe-pay/cache"
	"stripe-pay/common"
	"stripe-pay/conf"
	"stripe-pay/db"

	"os"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/hertz-contrib/cors"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func main() {
	// 初始化配置
	if err := conf.Init(); err != nil {
		panic(err)
	}

	// 初始化日志
	initLogger()

	// 初始化数据库
	dbInitialized := false
	if err := db.Init(); err != nil {
		zap.L().Warn("Failed to initialize database", zap.Error(err))
		zap.L().Warn("Application will continue without database support")
	} else {
		dbInitialized = true
	}

	// 初始化 Redis 缓存
	cacheInitialized := false
	if err := cache.Init(); err != nil {
		zap.L().Warn("Failed to initialize Redis cache", zap.Error(err))
		zap.L().Warn("Application will continue without cache support")
	} else {
		cacheInitialized = true
	}

	// 获取配置
	cfg := conf.GetConf()

	// 创建 Hertz 服务器
	h := server.Default(
		server.WithHostPorts(cfg.Server.Host + ":" + cfg.Server.Port),
	)

	// 添加全局 CORS 头处理（必须放在最前面，确保所有响应都包含 CORS 头）
	h.Use(func(ctx context.Context, c *app.RequestContext) {
		origin := string(c.Request.Header.Get("Origin"))
		// 如果请求包含 Origin 头，使用该 Origin；否则允许所有源
		if origin != "" {
			c.Header("Access-Control-Allow-Origin", origin)
		} else {
			c.Header("Access-Control-Allow-Origin", "*")
		}
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization, Accept")
		c.Header("Access-Control-Allow-Credentials", "false")
		c.Header("Access-Control-Max-Age", "43200") // 12 hours

		if string(c.Request.Method()) == "OPTIONS" {
			c.JSON(consts.StatusOK, utils.H{})
			c.Abort()
			return
		}
		c.Next(ctx)
	})

	// 添加 CORS 中间件（作为备用）
	h.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Content-Type", "Authorization", "Accept"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: false,
		MaxAge:           12 * time.Hour,
	}))

	// 添加监控指标中间件（必须在最前面，以便记录所有请求）
	h.Use(common.MetricsMiddleware())

	// 添加请求日志中间件（记录请求开始、结束和耗时）
	h.Use(common.RequestLogger())

	// 添加速率限制中间件（防止恶意刷接口）
	h.Use(common.RateLimitMiddleware())

	// 添加错误恢复中间件（捕获panic）
	h.Use(common.RecoveryHandler())

	// 注册路由
	registerRoutes(h)

	// 添加错误处理中间件（处理c.Errors，必须在路由注册之后）
	h.Use(common.ErrorHandler())

	// 设置优雅关闭（必须在启动前设置）
	setupGracefulShutdown(h, dbInitialized, cacheInitialized)

	// 启动服务器
	zap.L().Info("Server starting",
		zap.String("host", cfg.Server.Host),
		zap.String("port", cfg.Server.Port))

	// 启动服务器（阻塞调用，直到收到关闭信号）
	// Hertz 的 Spin() 会阻塞运行，当收到 SIGINT 或 SIGTERM 时会自动停止
	h.Spin()

	// 服务器已停止，执行清理工作
	zap.L().Info("Server stopped, performing cleanup...")

	// 执行清理
	if dbInitialized {
		zap.L().Info("Closing database connections...")
		db.Close()
	}
	if cacheInitialized {
		zap.L().Info("Closing Redis connections...")
		cache.Close()
	}

	zap.L().Info("Cleanup completed")
	_ = zap.L().Sync()
}

// setupGracefulShutdown 设置优雅关闭
func setupGracefulShutdown(h *server.Hertz, dbInitialized, cacheInitialized bool) *common.ShutdownManager {
	// 创建关闭管理器
	shutdownManager := common.NewShutdownManager(h)

	// 注册关闭函数
	if dbInitialized {
		shutdownManager.RegisterShutdownFunc(common.CreateShutdownFunc("database", func() error {
			zap.L().Info("Closing database connections...")
			db.Close()
			return nil
		}))
	}

	if cacheInitialized {
		shutdownManager.RegisterShutdownFunc(common.CreateShutdownFunc("redis", func() error {
			zap.L().Info("Closing Redis connections...")
			cache.Close()
			return nil
		}))
	}

	// 启动优雅关闭监听（在后台监听信号）
	shutdownManager.StartGracefulShutdown()

	return shutdownManager
}

func registerRoutes(h *server.Hertz) {
	// 健康检查
	h.GET("/ping", func(ctx context.Context, c *app.RequestContext) {
		c.JSON(consts.StatusOK, utils.H{"message": "pong"})
	})

	// 增强的健康检查
	h.GET("/health", handlers.HealthCheck)

	// Prometheus 指标端点
	h.GET("/metrics", common.MetricsHandler)

	// 静态测试页：直接由 8080 提供，便于与 ngrok 同域测试 Apple Pay
	h.GET("/apple_pay_test.html", func(ctx context.Context, c *app.RequestContext) {
		// 读取项目根目录下的 apple_pay_test.html
		data, err := os.ReadFile("apple_pay_test.html")
		if err != nil {
			c.SetStatusCode(consts.StatusNotFound)
			c.Write([]byte("not found"))
			return
		}
		c.Response.Header.SetContentType("text/html; charset=utf-8")
		c.Write(data)
	})

	// 静态测试页：微信支付测试
	h.GET("/wechat_test.html", func(ctx context.Context, c *app.RequestContext) {
		// 尝试多个可能的路径
		var data []byte
		var err error
		paths := []string{"wechat_test.html", "./wechat_test.html"}
		for _, path := range paths {
			data, err = os.ReadFile(path)
			if err == nil {
				break
			}
		}
		if err != nil {
			c.SetStatusCode(consts.StatusNotFound)
			c.JSON(consts.StatusNotFound, utils.H{"error": "wechat_test.html not found", "paths_tried": paths})
			return
		}
		c.Response.Header.SetContentType("text/html; charset=utf-8")
		c.Write(data)
	})

	// 静态测试页：支付宝支付测试
	h.GET("/alipay_test.html", func(ctx context.Context, c *app.RequestContext) {
		var data []byte
		var err error
		paths := []string{"alipay_test.html", "./alipay_test.html"}
		for _, path := range paths {
			data, err = os.ReadFile(path)
			if err == nil {
				break
			}
		}
		if err != nil {
			c.SetStatusCode(consts.StatusNotFound)
			c.JSON(consts.StatusNotFound, utils.H{"error": "alipay_test.html not found", "paths_tried": paths})
			return
		}
		c.Response.Header.SetContentType("text/html; charset=utf-8")
		c.Write(data)
	})

	// 支付相关路由
	api := h.Group("/api/v1")
	{
		// 定价信息
		api.GET("/pricing", handlers.GetPricing)

		// Stripe 支付（应用更严格的速率限制）
		paymentAPI := api.Group("/stripe")
		paymentAPI.Use(common.PaymentRateLimitMiddleware())
		{
			paymentAPI.POST("/create-payment", handlers.CreateStripePayment)
			paymentAPI.POST("/create-wechat-payment", handlers.CreateStripeWeChatPayment)
			paymentAPI.POST("/create-alipay-payment", handlers.CreateStripeAlipayPayment)
			paymentAPI.POST("/confirm-payment", handlers.ConfirmStripePayment)
			paymentAPI.POST("/refund", handlers.RefundPayment)
		}

		// Webhook 不需要速率限制（由 Stripe 控制）
		api.POST("/stripe/webhook", handlers.StripeWebhook)

		// Apple 内购
		api.POST("/apple/verify", handlers.VerifyApplePurchase)
		api.POST("/apple/verify-subscription", handlers.VerifyAppleSubscription)
		api.POST("/apple/webhook", handlers.AppleWebhook)

		// 用户支付信息查询
		api.GET("/user/:user_id/payment-info", handlers.GetUserPaymentInfo)
		api.GET("/user/:user_id/payment-history", handlers.GetUserPaymentHistory)

		// 支付状态相关接口（应用更严格的速率限制）
		paymentStatusAPI := api.Group("/payment")
		paymentStatusAPI.Use(common.PaymentRateLimitMiddleware())
		{
			// 支付状态更新（前端支付成功后调用）
			paymentStatusAPI.POST("/update-status", handlers.UpdatePaymentStatusFromFrontend)
			// 支付状态查询
			paymentStatusAPI.GET("/status/:id", handlers.GetPaymentStatus)
			// 支付状态变化查询
			paymentStatusAPI.GET("/status-change/:payment_intent_id", handlers.CheckStatusChange)
		}

		// 支付配置管理（管理员接口）
		api.GET("/payment/config", handlers.GetPaymentConfig)
		api.PUT("/payment/config", handlers.UpdatePaymentConfig)
	}
}

func initLogger() {
	cfg := conf.GetConf()

	var logger *zap.Logger
	var err error

	// 根据环境选择日志配置
	env := cfg.Log.Environment
	if env == "" {
		env = "development" // 默认开发环境
	}

	// 解析日志级别
	var logLevel zapcore.Level
	levelStr := cfg.Log.Level
	if levelStr == "" {
		levelStr = "info"
	}

	switch levelStr {
	case "debug":
		logLevel = zapcore.DebugLevel
	case "info":
		logLevel = zapcore.InfoLevel
	case "warn":
		logLevel = zapcore.WarnLevel
	case "error":
		logLevel = zapcore.ErrorLevel
	default:
		logLevel = zapcore.InfoLevel
	}

	// 根据环境创建日志配置
	if env == "production" {
		// 生产环境配置
		config := zap.NewProductionConfig()
		config.Level = zap.NewAtomicLevelAt(logLevel)

		// 根据输出格式选择编码器
		if cfg.Log.Output == "json" {
			config.Encoding = "json"
		} else {
			config.Encoding = "console"
		}

		// 生产环境优化
		config.EncoderConfig.TimeKey = "timestamp"
		config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
		config.EncoderConfig.StacktraceKey = "stacktrace"

		// 禁用调用者信息（生产环境性能优化）
		config.DisableCaller = false
		config.DisableStacktrace = logLevel > zapcore.ErrorLevel

		logger, err = config.Build()
	} else {
		// 开发环境配置
		config := zap.NewDevelopmentConfig()
		config.Level = zap.NewAtomicLevelAt(logLevel)

		// 开发环境使用彩色控制台输出
		config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder

		logger, err = config.Build()
	}

	if err != nil {
		panic(fmt.Errorf("failed to initialize logger: %w", err))
	}

	zap.ReplaceGlobals(logger)

	// 设置 Hertz 日志级别
	var hzLevel hlog.Level
	switch logLevel {
	case zapcore.DebugLevel:
		hzLevel = hlog.LevelDebug
	case zapcore.InfoLevel:
		hzLevel = hlog.LevelInfo
	case zapcore.WarnLevel:
		hzLevel = hlog.LevelWarn
	case zapcore.ErrorLevel:
		hzLevel = hlog.LevelError
	default:
		hzLevel = hlog.LevelInfo
	}
	hlog.SetLevel(hzLevel)

	// 记录日志系统初始化信息
	zap.L().Info("Logger initialized",
		zap.String("environment", env),
		zap.String("level", levelStr),
		zap.String("output", cfg.Log.Output))
}
