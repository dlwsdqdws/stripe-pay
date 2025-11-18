package main

import (
	"fmt"
	"stripe-pay/cache"
	"stripe-pay/conf"
	"stripe-pay/db"

	"go.uber.org/zap"
)

func main() {
	// 初始化配置
	if err := conf.Init(); err != nil {
		fmt.Printf("❌ 配置初始化失败: %v\n", err)
		return
	}

	// 初始化日志
	logger, _ := zap.NewDevelopment()
	zap.ReplaceGlobals(logger)

	// 初始化数据库（可选）
	_ = db.Init()

	// 初始化 Redis
	fmt.Println("正在连接 Redis...")
	if err := cache.Init(); err != nil {
		fmt.Printf("❌ Redis 连接失败: %v\n", err)
		return
	}

	if cache.IsAvailable() {
		fmt.Println("✅ Redis 连接成功！")
		fmt.Println("")
		fmt.Println("Redis 配置信息：")
		cfg := conf.GetConf()
		fmt.Printf("  地址: %s:%d\n", cfg.Redis.Address, cfg.Redis.Port)
		fmt.Printf("  数据库: %d\n", cfg.Redis.DB)
		fmt.Printf("  连接池大小: %d\n", cfg.Redis.PoolSize)
		fmt.Println("")
		fmt.Println("✅ Redis 缓存功能已启用")
	} else {
		fmt.Println("⚠️  Redis 未配置或连接失败，缓存功能已禁用")
		fmt.Println("   系统仍可正常工作，但不会使用缓存")
	}

	// 关闭连接
	cache.Close()
}
