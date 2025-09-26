//go:build examples
// +build examples

// Package examples provides usage patterns and examples for the MCP router components.
//
// This file demonstrates common patterns and best practices for using the router's.
// connection pools, metrics collection, and other advanced features.
//
// Build with: go build -tags examples ./examples/
package examples

import (
	"context"
	"log"
	"sync/atomic"
	"time"

	"github.com/poiley/mcp-bridge/services/router/internal/constants"
	"github.com/poiley/mcp-bridge/services/router/internal/gateway"
	"github.com/poiley/mcp-bridge/services/router/internal/metrics"
	"github.com/poiley/mcp-bridge/services/router/internal/pool"
	"github.com/poiley/mcp-bridge/services/router/pkg/mcp"
	"go.uber.org/zap"
)

const (
	defaultTimeoutSeconds = 30
	defaultRetryCount     = 10
	defaultMaxConnections = 5
	defaultMaxRetries     = 100
)

// ConnectionPoolExample demonstrates advanced connection pool usage patterns.
func ConnectionPoolExample() {
	logger, _ := zap.NewProduction()
	defer logger.Sync()

	// Create a custom pool configuration for high-throughput scenarios.
	config := pool.Config{
		MinSize:             5,  // Keep 5 connections warm
		MaxSize:             50, // Allow up to 50 concurrent connections
		MaxIdleTime:         constants.DefaultMaxIdleTime,
		MaxLifetime:         constants.DefaultMaxLifetime,
		AcquireTimeout:      constants.DefaultAcquireTimeout,
		HealthCheckInterval: constants.DefaultHealthCheckInterval,
	}

	// Factory function for WebSocket connections.
	createWebSocketClient := func(cfg config.GatewayConfig, logger *zap.Logger) (gateway.GatewayClient, error) {
		return gateway.NewClient(cfg, logger)
	}

	// Create WebSocket pool with custom configuration.
	gwConfig := config.GatewayConfig{
		URL: "wss://gateway.example.com",
	}
	factory := pool.NewGenericFactory(gwConfig, logger, createWebSocketClient, "WebSocket")

	p, err := pool.NewPool(config, factory, logger)
	if err != nil {
		log.Fatalf("Failed to create pool: %v", err)
	}
	defer func() { _ = p.Close() }()

	// Example: Concurrent request processing with pool.
	for i := 0; i < 10; i++ {
		go func(requestID int) {
			ctx, cancel := context.WithTimeout(context.Background(), defaultTimeoutSeconds*time.Second)
			defer cancel()

			// Acquire connection from pool.
			conn, err := p.Acquire(ctx)
			if err != nil {
				logger.Error("Failed to acquire connection",
					zap.Int("request_id", requestID),
					zap.Error(err))
				return
			}
			defer p.Release(conn)

			// Use connection for MCP operations.
			// Note: In practice, you would cast to the specific connection type.
			// and access the underlying client through the generic connection interface
			genericConn := conn.(*pool.GenericConnection)
			client := genericConn.GetClient().(gateway.GatewayClient)
			request := &mcp.Request{
				ID:     string(rune(requestID)),
				Method: "example/method",
				Params: map[string]interface{}{
					"data": "example data",
				},
			}

			if err := client.SendRequest(request); err != nil {
				logger.Error("Failed to send request",
					zap.Int("request_id", requestID),
					zap.Error(err))
				return
			}

			logger.Info("Request sent successfully",
				zap.Int("request_id", requestID))
		}(i)
	}

	// Monitor pool statistics.
	time.Sleep(1 * time.Second)
	stats := p.Stats()
	logger.Info("Pool statistics",
		zap.Int64("total_connections", stats.TotalConnections),
		zap.Int64("active_connections", stats.ActiveConnections),
		zap.Int64("idle_connections", stats.IdleConnections),
		zap.Int64("wait_count", stats.WaitCount),
	)
}

// MetricsCollectionExample demonstrates comprehensive metrics collection patterns.
func MetricsCollectionExample() {
	logger, _ := zap.NewProduction()
	defer logger.Sync()

	// Metrics counters (using atomic operations for thread safety).
	var (
		requestCount  uint64
		responseCount uint64
		errorCount    uint64
		retryCount    uint64
		activeConns   int32
	)

	// Request durations for histogram (in practice, use a ring buffer or sampling).
	requestDurations := make(map[string][]time.Duration)
	responseSizes := make([]int, 0)

	// Create Prometheus exporter.
	exporter := metrics.NewPrometheusExporter(":defaultMetricsPort", logger)

	// Set up metrics data source.
	exporter.SetRouterMetrics(func() *metrics.RouterMetrics {
		return &metrics.RouterMetrics{
			RequestsTotal:     atomic.LoadUint64(&requestCount),
			ResponsesTotal:    atomic.LoadUint64(&responseCount),
			ErrorsTotal:       atomic.LoadUint64(&errorCount),
			ConnectionRetries: atomic.LoadUint64(&retryCount),
			ActiveConnections: activeConns,
			RequestDuration:   requestDurations,
			ResponseSizes:     responseSizes,
		}
	})

	// Start metrics server.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		if err := exporter.Start(ctx); err != nil {
			logger.Error("Metrics server failed", zap.Error(err))
		}
	}()

	// Simulate request processing with metrics collection.
	for i := 0; i < 100; i++ {
		start := time.Now()

		// Simulate request processing.
		atomic.AddUint64(&requestCount, 1)
		atomic.AddInt32(&activeConns, 1)

		// Simulate some processing time.
		processingTime := time.Duration(i%10) * 10 * time.Millisecond
		time.Sleep(processingTime)

		// Record metrics.
		duration := time.Since(start)
		if requestDurations["example_method"] == nil {
			requestDurations["example_method"] = make([]time.Duration, 0)
		}
		requestDurations["example_method"] = append(requestDurations["example_method"], duration)

		// Simulate response.
		responseSize := 100 + i*10 // Variable response sizes
		responseSizes = append(responseSizes, responseSize)

		atomic.AddUint64(&responseCount, 1)
		atomic.AddInt32(&activeConns, -1)

		// Occasionally simulate errors.
		if i%20 == 0 {
			atomic.AddUint64(&errorCount, 1)
			atomic.AddUint64(&retryCount, 1)
		}
	}

	logger.Info("Metrics collection complete",
		zap.Uint64("requests", atomic.LoadUint64(&requestCount)),
		zap.Uint64("responses", atomic.LoadUint64(&responseCount)),
		zap.Uint64("errors", atomic.LoadUint64(&errorCount)),
	)

	// Keep server running to observe metrics.
	time.Sleep(defaultMaxConnections * time.Second)
}

// ErrorHandlingPatterns demonstrates robust error handling patterns.
func ErrorHandlingPatterns() {
	logger, _ := zap.NewProduction()
	defer logger.Sync()

	// Pattern 1: Circuit breaker for fault tolerance.
	config := pool.DefaultConfig()
	config.AcquireTimeout = 2 * time.Second // Shorter timeout for faster failure

	// Pattern 2: Retry with exponential backoff.
	retryOperation := func(operation func() error) error {
		backoff := constants.RateLimiterBackoffMin
		maxBackoff := constants.RateLimiterBackoffMax
		maxRetries := 5

		for attempt := 0; attempt < maxRetries; attempt++ {
			if err := operation(); err != nil {
				if attempt == maxRetries-1 {
					return err // Final attempt failed
				}

				logger.Warn("Operation failed, retrying",
					zap.Int("attempt", attempt+1),
					zap.Duration("backoff", backoff),
					zap.Error(err))

				time.Sleep(backoff)
				backoff *= 2
				if backoff > maxBackoff {
					backoff = maxBackoff
				}
				continue
			}
			return nil // Success
		}
		return nil
	}

	// Pattern 3: Context-based timeout and cancellation.
	operationWithTimeout := func() error {
		ctx, cancel := context.WithTimeout(context.Background(), constants.DefaultReceiveTimeout)
		defer cancel()

		// Simulate long-running operation.
		select {
		case <-time.After(1 * time.Second):
			return nil // Operation completed
		case <-ctx.Done():
			return ctx.Err() // Timed out or cancelled
		}
	}

	// Pattern 4: Graceful degradation.
	primaryOperation := func() error {
		// Simulate primary operation that might fail.
		return pool.ErrPoolExhausted
	}

	fallbackOperation := func() error {
		logger.Info("Using fallback operation")
		return nil // Simple fallback that always works
	}

	// Execute with patterns.
	if err := retryOperation(operationWithTimeout); err != nil {
		logger.Error("Operation with timeout failed", zap.Error(err))
	}

	// Try primary, fall back to secondary.
	if err := primaryOperation(); err != nil {
		logger.Warn("Primary operation failed, using fallback", zap.Error(err))
		if err := fallbackOperation(); err != nil {
			logger.Error("Fallback also failed", zap.Error(err))
		}
	}

	logger.Info("Error handling patterns demonstration complete")
}

// ConfigurationBestPractices demonstrates configuration management patterns.
func ConfigurationBestPractices() {
	logger, _ := zap.NewProduction()
	defer logger.Sync()

	// Pattern 1: Environment-specific configurations.
	var config pool.Config
	if isProduction() {
		// Production: Higher limits, longer timeouts.
		config = pool.Config{
			MinSize:             10,
			MaxSize:             100,
			MaxIdleTime:         constants.DefaultMaxIdleTime,
			MaxLifetime:         constants.DefaultMaxLifetime,
			AcquireTimeout:      constants.DefaultAcquireTimeout,
			HealthCheckInterval: constants.DefaultHealthCheckInterval,
		}
	} else {
		// Development: Lower limits, shorter timeouts for faster feedback.
		config = pool.Config{
			MinSize:             2,
			MaxSize:             10,
			MaxIdleTime:         1 * time.Minute,
			MaxLifetime:         5 * time.Minute,
			AcquireTimeout:      2 * time.Second,
			HealthCheckInterval: defaultRetryCount * time.Second,
		}
	}

	// Pattern 2: Configuration validation.
	if err := validatePoolConfig(config); err != nil {
		logger.Fatal("Invalid pool configuration", zap.Error(err))
	}

	// Pattern 3: Configuration monitoring.
	logger.Info("Pool configuration loaded",
		zap.Int("min_size", config.MinSize),
		zap.Int("max_size", config.MaxSize),
		zap.Duration("max_idle_time", config.MaxIdleTime),
		zap.Duration("acquire_timeout", config.AcquireTimeout),
	)
}

// Helper functions for examples.

func isProduction() bool {
	// In practice, check environment variables or config files.
	return false
}

func validatePoolConfig(config pool.Config) error {
	if config.MinSize < 0 {
		return pool.ErrInvalidConn
	}
	if config.MaxSize <= 0 {
		return pool.ErrInvalidConn
	}
	if config.MinSize > config.MaxSize {
		return pool.ErrInvalidConn
	}
	return nil
}
