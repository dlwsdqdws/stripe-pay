package common

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	dto "github.com/prometheus/client_model/go"
	"go.uber.org/zap"
)

var (
	// HTTP 请求指标
	httpRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total number of HTTP requests",
		},
		[]string{"method", "path", "status_code"},
	)

	httpRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP request duration in seconds",
			Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		},
		[]string{"method", "path", "status_code"},
	)

	httpRequestSize = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_size_bytes",
			Help:    "HTTP request size in bytes",
			Buckets: prometheus.ExponentialBuckets(100, 10, 7),
		},
		[]string{"method", "path"},
	)

	httpResponseSize = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_response_size_bytes",
			Help:    "HTTP response size in bytes",
			Buckets: prometheus.ExponentialBuckets(100, 10, 7),
		},
		[]string{"method", "path", "status_code"},
	)

	// 支付相关指标
	paymentTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "payment_total",
			Help: "Total number of payment attempts",
		},
		[]string{"payment_method", "status"},
	)

	paymentAmount = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "payment_amount",
			Help:    "Payment amount",
			Buckets: []float64{100, 500, 1000, 5000, 10000, 50000, 100000, 500000},
		},
		[]string{"payment_method", "currency"},
	)

	paymentDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "payment_duration_seconds",
			Help:    "Payment processing duration in seconds",
			Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60},
		},
		[]string{"payment_method", "status"},
	)

	// 错误指标
	errorsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "errors_total",
			Help: "Total number of errors",
		},
		[]string{"type", "error_code"},
	)

	// 速率限制指标
	rateLimitHits = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "rate_limit_hits_total",
			Help: "Total number of rate limit hits",
		},
		[]string{"limit_type", "path"},
	)

	// 数据库指标
	dbQueryDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "db_query_duration_seconds",
			Help:    "Database query duration in seconds",
			Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5},
		},
		[]string{"operation", "table"},
	)

	dbQueryTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "db_queries_total",
			Help: "Total number of database queries",
		},
		[]string{"operation", "table", "status"},
	)

	// Redis 指标
	redisOperationDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "redis_operation_duration_seconds",
			Help:    "Redis operation duration in seconds",
			Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5},
		},
		[]string{"operation", "status"},
	)

	redisOperationTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "redis_operations_total",
			Help: "Total number of Redis operations",
		},
		[]string{"operation", "status"},
	)

	// 系统指标
	activeConnections = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "active_connections",
			Help: "Number of active connections",
		},
	)
)

// MetricsMiddleware 监控指标中间件
func MetricsMiddleware() app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		start := time.Now()
		method := string(c.Method())
		path := normalizePath(string(c.Path()))

		// 记录请求大小
		requestSize := float64(len(c.Request.Body()))
		httpRequestSize.WithLabelValues(method, path).Observe(requestSize)

		// 继续处理请求
		c.Next(ctx)

		// 计算处理时间
		duration := time.Since(start).Seconds()
		statusCode := c.Response.StatusCode()
		statusCodeStr := statusCodeToString(statusCode)

		// 记录响应大小
		responseSize := float64(len(c.Response.Body()))
		httpResponseSize.WithLabelValues(method, path, statusCodeStr).Observe(responseSize)

		// 记录指标
		httpRequestsTotal.WithLabelValues(method, path, statusCodeStr).Inc()
		httpRequestDuration.WithLabelValues(method, path, statusCodeStr).Observe(duration)

		// 记录错误
		if statusCode >= 500 {
			errorsTotal.WithLabelValues("http_error", statusCodeStr).Inc()
		} else if statusCode >= 400 {
			errorsTotal.WithLabelValues("client_error", statusCodeStr).Inc()
		}
	}
}

// RecordPayment 记录支付指标
func RecordPayment(method, status string, amount int64, currency string, duration time.Duration) {
	paymentTotal.WithLabelValues(method, status).Inc()
	paymentAmount.WithLabelValues(method, currency).Observe(float64(amount))
	paymentDuration.WithLabelValues(method, status).Observe(duration.Seconds())

	// 记录支付错误（只记录真正的失败状态）
	// created, processing, requires_action 等是正常中间状态，不应记录为错误
	failureStatuses := []string{"failed", "canceled", "requires_payment_method"}
	isFailure := false
	for _, failureStatus := range failureStatuses {
		if status == failureStatus {
			isFailure = true
			break
		}
	}

	if isFailure {
		errorsTotal.WithLabelValues("payment_error", status).Inc()
	}
}

// RecordRateLimitHit 记录速率限制命中
func RecordRateLimitHit(limitType, path string) {
	rateLimitHits.WithLabelValues(limitType, normalizePath(path)).Inc()
}

// RecordDBQuery 记录数据库查询指标
func RecordDBQuery(operation, table, status string, duration time.Duration) {
	dbQueryTotal.WithLabelValues(operation, table, status).Inc()
	dbQueryDuration.WithLabelValues(operation, table).Observe(duration.Seconds())

	if status == "error" {
		errorsTotal.WithLabelValues("db_error", operation).Inc()
	}
}

// RecordRedisOperation 记录 Redis 操作指标
func RecordRedisOperation(operation, status string, duration time.Duration) {
	redisOperationTotal.WithLabelValues(operation, status).Inc()
	redisOperationDuration.WithLabelValues(operation, status).Observe(duration.Seconds())

	if status == "error" {
		errorsTotal.WithLabelValues("redis_error", operation).Inc()
	}
}

// SetActiveConnections 设置活跃连接数
func SetActiveConnections(count float64) {
	activeConnections.Set(count)
}

// normalizePath 规范化路径（移除动态参数）
func normalizePath(path string) string {
	// 移除常见的动态参数
	normalized := path
	// 可以添加更多路径规范化逻辑
	// 例如：/api/v1/user/123 -> /api/v1/user/:id
	return normalized
}

// statusCodeToString 将状态码转换为字符串
func statusCodeToString(code int) string {
	return fmt.Sprintf("%d", code)
}

// MetricsHandler Prometheus 指标处理器
func MetricsHandler(ctx context.Context, c *app.RequestContext) {
	// 获取所有指标
	gatherer := prometheus.DefaultGatherer
	metricFamilies, err := gatherer.Gather()
	if err != nil {
		c.SetStatusCode(500)
		c.JSON(500, utils.H{"error": "Failed to gather metrics", "details": err.Error()})
		zap.L().Error("Failed to gather metrics", zap.Error(err))
		return
	}

	// 格式化为 Prometheus 文本格式
	var output strings.Builder
	for _, mf := range metricFamilies {
		// 输出 HELP
		if mf.Help != nil {
			output.WriteString(fmt.Sprintf("# HELP %s %s\n", *mf.Name, *mf.Help))
		}
		// 输出 TYPE
		output.WriteString(fmt.Sprintf("# TYPE %s %s\n", *mf.Name, mf.Type.String()))

		// 输出指标值
		for _, metric := range mf.Metric {
			// 构建标签
			labels := buildLabels(metric.Label)

			// 获取值
			var metricType dto.MetricType
			if mf.Type != nil {
				metricType = *mf.Type
			}
			value := getMetricValue(metricType, metric)
			output.WriteString(fmt.Sprintf("%s%s %v\n", *mf.Name, labels, value))
		}
		output.WriteString("\n")
	}

	c.SetStatusCode(200)
	c.Header("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	c.Write([]byte(output.String()))
}

// buildLabels 构建标签字符串
func buildLabels(labels []*dto.LabelPair) string {
	if len(labels) == 0 {
		return ""
	}

	parts := make([]string, 0, len(labels))
	for _, label := range labels {
		parts = append(parts, fmt.Sprintf("%s=\"%s\"", *label.Name, *label.Value))
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// getMetricValue 获取指标值
func getMetricValue(metricType dto.MetricType, metric *dto.Metric) interface{} {
	switch metricType {
	case dto.MetricType_COUNTER:
		if metric.Counter != nil {
			return *metric.Counter.Value
		}
	case dto.MetricType_GAUGE:
		if metric.Gauge != nil {
			return *metric.Gauge.Value
		}
	case dto.MetricType_HISTOGRAM:
		if metric.Histogram != nil {
			return float64(*metric.Histogram.SampleCount)
		}
	case dto.MetricType_SUMMARY:
		if metric.Summary != nil {
			return float64(*metric.Summary.SampleCount)
		}
	}
	return 0
}
