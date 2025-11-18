package common

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/cloudwego/hertz/pkg/app/server"
	"go.uber.org/zap"
)

// ShutdownManager 优雅关闭管理器
type ShutdownManager struct {
	server        *server.Hertz
	shutdownFuncs []func(context.Context) error
	mu            sync.Mutex
	shuttingDown  bool
}

// NewShutdownManager 创建关闭管理器
func NewShutdownManager(s *server.Hertz) *ShutdownManager {
	return &ShutdownManager{
		server:        s,
		shutdownFuncs: make([]func(context.Context) error, 0),
	}
}

// RegisterShutdownFunc 注册关闭函数
func (sm *ShutdownManager) RegisterShutdownFunc(fn func(context.Context) error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.shutdownFuncs = append(sm.shutdownFuncs, fn)
}

// StartGracefulShutdown 启动优雅关闭监听
// 注意：这个方法会在后台 goroutine 中监听信号
// 当收到 SIGINT 或 SIGTERM 时，会执行优雅关闭
func (sm *ShutdownManager) StartGracefulShutdown() {
	// 创建信号通道
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// 在 goroutine 中等待信号
	go func() {
		sig := <-sigChan
		zap.L().Info("Received shutdown signal",
			zap.String("signal", sig.String()))

		sm.mu.Lock()
		if sm.shuttingDown {
			sm.mu.Unlock()
			return
		}
		sm.shuttingDown = true
		sm.mu.Unlock()

		// 执行优雅关闭
		// 注意：Hertz 的 Spin() 在收到信号时会自动停止
		// 但我们需要确保关闭函数被执行
		sm.gracefulShutdown()
	}()
}

// gracefulShutdown 执行优雅关闭
// 注意：这个方法在收到关闭信号时被调用
// Hertz 的 Spin() 会自动处理信号并停止接收新请求，等待现有请求完成
func (sm *ShutdownManager) gracefulShutdown() {
	zap.L().Info("Starting graceful shutdown...")

	// 创建关闭上下文，设置超时时间
	shutdownTimeout := 30 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	// 1. 停止接收新请求
	// 注意：Hertz 的 Spin() 在收到信号时会自动：
	// - 停止接收新请求
	// - 等待现有请求完成（有超时保护）
	zap.L().Info("Server will stop accepting new requests and wait for ongoing requests to complete")

	// 2. 执行注册的关闭函数
	sm.mu.Lock()
	shutdownFuncs := make([]func(context.Context) error, len(sm.shutdownFuncs))
	copy(shutdownFuncs, sm.shutdownFuncs)
	sm.mu.Unlock()

	if len(shutdownFuncs) > 0 {
		zap.L().Info("Executing shutdown functions...",
			zap.Int("count", len(shutdownFuncs)))

		// 并发执行关闭函数，但设置超时
		done := make(chan struct{})
		go func() {
			var wg sync.WaitGroup
			for _, fn := range shutdownFuncs {
				wg.Add(1)
				go func(f func(context.Context) error) {
					defer wg.Done()
					if err := f(ctx); err != nil {
						zap.L().Warn("Shutdown function error", zap.Error(err))
					}
				}(fn)
			}
			wg.Wait()
			close(done)
		}()

		// 等待关闭完成或超时
		select {
		case <-done:
			zap.L().Info("All shutdown functions completed")
		case <-ctx.Done():
			zap.L().Warn("Shutdown timeout exceeded, some functions may not have completed",
				zap.Duration("timeout", shutdownTimeout))
		}
	}

	// 3. 记录关闭完成
	zap.L().Info("Graceful shutdown process completed")

	// 同步日志
	_ = zap.L().Sync()
}

// WaitForShutdown 等待关闭信号（阻塞调用）
// 注意：这个方法不应该在 Hertz 的 Spin() 之后调用
// 因为 Spin() 已经会阻塞并处理信号
func (sm *ShutdownManager) WaitForShutdown() {
	// 创建信号通道
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigChan
	zap.L().Info("Received shutdown signal",
		zap.String("signal", sig.String()))

	sm.mu.Lock()
	if sm.shuttingDown {
		sm.mu.Unlock()
		return
	}
	sm.shuttingDown = true
	sm.mu.Unlock()

	// 执行优雅关闭
	sm.gracefulShutdown()
}

// IsShuttingDown 检查是否正在关闭
func (sm *ShutdownManager) IsShuttingDown() bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return sm.shuttingDown
}

// GracefulShutdown 便捷函数：设置优雅关闭
func GracefulShutdown(s *server.Hertz, shutdownFuncs ...func(context.Context) error) {
	manager := NewShutdownManager(s)

	// 注册关闭函数
	for _, fn := range shutdownFuncs {
		manager.RegisterShutdownFunc(fn)
	}

	// 启动监听
	manager.StartGracefulShutdown()
}

// CreateShutdownFunc 创建标准的关闭函数
func CreateShutdownFunc(name string, fn func() error) func(context.Context) error {
	return func(ctx context.Context) error {
		zap.L().Info("Executing shutdown function",
			zap.String("name", name))

		// 创建带超时的上下文
		done := make(chan error, 1)
		go func() {
			done <- fn()
		}()

		select {
		case err := <-done:
			if err != nil {
				zap.L().Error("Shutdown function failed",
					zap.String("name", name),
					zap.Error(err))
				return fmt.Errorf("%s shutdown failed: %w", name, err)
			}
			zap.L().Info("Shutdown function completed",
				zap.String("name", name))
			return nil
		case <-ctx.Done():
			zap.L().Warn("Shutdown function timeout",
				zap.String("name", name))
			return ctx.Err()
		}
	}
}
