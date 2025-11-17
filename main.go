package main

import (
	"context"
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
)

func main() {
	// Initialize configuration
	if err := conf.Init(); err != nil {
		panic(err)
	}

	// Initialize logger
	initLogger()

	// Initialize database
	if err := db.Init(); err != nil {
		zap.L().Warn("Failed to initialize database", zap.Error(err))
		zap.L().Warn("Application will continue without database support")
	} else {
		// Ensure database connection is closed on exit
		defer db.Close()
	}

	// Initialize Redis cache
	if err := cache.Init(); err != nil {
		zap.L().Warn("Failed to initialize Redis cache", zap.Error(err))
		zap.L().Warn("Application will continue without cache support")
	} else {
		// Ensure Redis connection is closed on exit
		defer cache.Close()
	}

	// Get configuration
	cfg := conf.GetConf()

	// Create Hertz server
	h := server.Default(
		server.WithHostPorts(cfg.Server.Host + ":" + cfg.Server.Port),
	)

	// Add global CORS header handling (must be placed first to ensure all responses include CORS headers)
	h.Use(func(ctx context.Context, c *app.RequestContext) {
		origin := string(c.Request.Header.Get("Origin"))
		// If request contains Origin header, use that Origin; otherwise allow all origins
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

	// Add CORS middleware (as backup)
	h.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Content-Type", "Authorization", "Accept"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: false,
		MaxAge:           12 * time.Hour,
	}))

	// Add error recovery middleware (catches panic)
	h.Use(common.RecoveryHandler())

	// Register routes
	registerRoutes(h)

	// Add error handling middleware (handles c.Errors, must be after route registration)
	h.Use(common.ErrorHandler())

	// Start server
	zap.L().Info("Server starting",
		zap.String("host", cfg.Server.Host),
		zap.String("port", cfg.Server.Port))
	h.Spin()
}

func registerRoutes(h *server.Hertz) {
	// Health check
	h.GET("/ping", func(ctx context.Context, c *app.RequestContext) {
		c.JSON(consts.StatusOK, utils.H{"message": "pong"})
	})

	// Static test page: served directly on 8080 for same-domain testing with ngrok for Apple Pay
	h.GET("/apple_pay_test.html", func(ctx context.Context, c *app.RequestContext) {
		// Read apple_pay_test.html from project root
		data, err := os.ReadFile("apple_pay_test.html")
		if err != nil {
			c.SetStatusCode(consts.StatusNotFound)
			c.Write([]byte("not found"))
			return
		}
		c.Response.Header.SetContentType("text/html; charset=utf-8")
		c.Write(data)
	})

	// Static test page: WeChat Pay test
	h.GET("/wechat_test.html", func(ctx context.Context, c *app.RequestContext) {
		// Try multiple possible paths
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

	// Static test page: Alipay test
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

	// Payment related routes
	api := h.Group("/api/v1")
	{
		// Pricing information
		api.GET("/pricing", handlers.GetPricing)
		// Stripe payment
		api.POST("/stripe/create-payment", handlers.CreateStripePayment)
		api.POST("/stripe/create-wechat-payment", handlers.CreateStripeWeChatPayment)
		api.POST("/stripe/create-alipay-payment", handlers.CreateStripeAlipayPayment)
		api.POST("/stripe/confirm-payment", handlers.ConfirmStripePayment)
		api.POST("/stripe/webhook", handlers.StripeWebhook)
		api.POST("/stripe/refund", handlers.RefundPayment)

		// Apple in-app purchase
		api.POST("/apple/verify", handlers.VerifyApplePurchase)
		api.POST("/apple/verify-subscription", handlers.VerifyAppleSubscription)
		api.POST("/apple/webhook", handlers.AppleWebhook)

		// User payment information query
		api.GET("/user/:user_id/payment-info", handlers.GetUserPaymentInfo)
		api.GET("/user/:user_id/payment-history", handlers.GetUserPaymentHistory)

		// Payment status update (called after frontend payment success)
		api.POST("/payment/update-status", handlers.UpdatePaymentStatusFromFrontend)

		// Payment configuration management (admin interface)
		api.GET("/payment/config", handlers.GetPaymentConfig)
		api.PUT("/payment/config", handlers.UpdatePaymentConfig)

		// Payment status query
		api.GET("/payment/status/:id", handlers.GetPaymentStatus)
	}
}

func initLogger() {
	// Simplify logger configuration, use Hertz default logger
	// Use zap for logging in business code
	logger, err := zap.NewDevelopment()
	if err != nil {
		panic(err)
	}
	zap.ReplaceGlobals(logger)

	// Hertz uses default logger
	hlog.SetLevel(hlog.LevelDebug)
}
