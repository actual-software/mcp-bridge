package direct

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap/zaptest"
)

// Comprehensive benchmark suite for pool performance characteristics.

func BenchmarkPoolPerformance_ConnectionCreation(b *testing.B) {
	logger := zaptest.NewLogger(b)
	config := DefaultConnectionPoolConfig()
	config.EnableConnectionReuse = false // Force new connections

	pool := NewConnectionPool(config, logger)

	defer func() {
		if err := pool.Close(); err != nil {
			b.Logf("Failed to close pool: %v", err)
		}
	}()

	ctx := context.Background()
	createFunc := func() (DirectClient, error) {
		return NewMockDirectClient("test", "stdio", "test://example"), nil
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := pool.GetConnection(ctx, fmt.Sprintf("test://server%d", i), ClientTypeStdio, createFunc)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPoolPerformance_ConnectionReuse(b *testing.B) {
	logger := zaptest.NewLogger(b)
	config := DefaultConnectionPoolConfig()
	config.EnableConnectionReuse = true

	pool := NewConnectionPool(config, logger)

	defer func() {
		if err := pool.Close(); err != nil {
			b.Logf("Failed to close pool: %v", err)
		}
	}()

	ctx := context.Background()
	createFunc := func() (DirectClient, error) {
		return NewMockDirectClient("test", "stdio", "test://example"), nil
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := pool.GetConnection(ctx, "test://server", ClientTypeStdio, createFunc)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPoolPerformance_MultiProtocol(b *testing.B) {
	logger := zaptest.NewLogger(b)
	config := DefaultConnectionPoolConfig()

	pool := NewConnectionPool(config, logger)

	defer func() {
		if err := pool.Close(); err != nil {
			b.Logf("Failed to close pool: %v", err)
		}
	}()

	ctx := context.Background()
	createFunc := func() (DirectClient, error) {
		return NewMockDirectClient("test", "stdio", "test://example"), nil
	}

	protocols := []ClientType{ClientTypeStdio, ClientTypeHTTP, ClientTypeWebSocket, ClientTypeSSE}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		protocol := protocols[i%len(protocols)]

		_, err := pool.GetConnection(ctx, "test://server", protocol, createFunc)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPoolPerformance_ConcurrentGetConnection(b *testing.B) {
	logger := zaptest.NewLogger(b)
	config := DefaultConnectionPoolConfig()
	config.MaxActiveConnections = testIterations

	pool := NewConnectionPool(config, logger)

	defer func() {
		if err := pool.Close(); err != nil {
			b.Logf("Failed to close pool: %v", err)
		}
	}()

	ctx := context.Background()
	createFunc := func() (DirectClient, error) {
		return NewMockDirectClient("test", "stdio", "test://example"), nil
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			serverURL := fmt.Sprintf("test://server%d", i%10)

			_, err := pool.GetConnection(ctx, serverURL, ClientTypeStdio, createFunc)
			if err != nil {
				b.Fatal(err)
			}

			i++
		}
	})
}

func BenchmarkPoolPerformance_HighContention(b *testing.B) {
	logger := zaptest.NewLogger(b)
	config := DefaultConnectionPoolConfig()
	config.MaxActiveConnections = 10 // Low limit to force contention
	config.EnableConnectionReuse = true

	pool := NewConnectionPool(config, logger)

	defer func() {
		if err := pool.Close(); err != nil {
			b.Logf("Failed to close pool: %v", err)
		}
	}()

	ctx := context.Background()
	createFunc := func() (DirectClient, error) {
		return NewMockDirectClient("test", "stdio", "test://example"), nil
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err := pool.GetConnection(ctx, "test://hotserver", ClientTypeStdio, createFunc)
			if err != nil && err.Error() != "maximum active connections reached (10)" {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkPoolPerformance_StatsCollection(b *testing.B) {
	logger := zaptest.NewLogger(b)
	config := DefaultConnectionPoolConfig()

	pool := NewConnectionPool(config, logger)

	defer func() {
		if err := pool.Close(); err != nil {
			b.Logf("Failed to close pool: %v", err)
		}
	}()

	// Pre-populate pool with connections.
	ctx := context.Background()
	createFunc := func() (DirectClient, error) {
		return NewMockDirectClient("test", "stdio", "test://example"), nil
	}

	for i := 0; i < testTimeout; i++ {
		_, err := pool.GetConnection(ctx, fmt.Sprintf("test://server%d", i), ClientTypeStdio, createFunc)
		if err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = pool.GetStats()
		}
	})
}

func BenchmarkPoolPerformance_ActiveConnections(b *testing.B) {
	logger := zaptest.NewLogger(b)
	config := DefaultConnectionPoolConfig()

	pool := NewConnectionPool(config, logger)

	defer func() {
		if err := pool.Close(); err != nil {
			b.Logf("Failed to close pool: %v", err)
		}
	}()

	// Pre-populate pool.
	ctx := context.Background()
	createFunc := func() (DirectClient, error) {
		return NewMockDirectClient("test", "stdio", "test://example"), nil
	}

	for i := 0; i < testIterations; i++ {
		_, err := pool.GetConnection(ctx, fmt.Sprintf("test://server%d", i), ClientTypeStdio, createFunc)
		if err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = pool.GetActiveConnections()
	}
}

func BenchmarkPoolPerformance_ConnectionRemoval(b *testing.B) {
	logger := zaptest.NewLogger(b)
	config := DefaultConnectionPoolConfig()

	pool := NewConnectionPool(config, logger)

	defer func() {
		if err := pool.Close(); err != nil {
			b.Logf("Failed to close pool: %v", err)
		}
	}()

	ctx := context.Background()
	createFunc := func() (DirectClient, error) {
		return NewMockDirectClient("test", "stdio", "test://example"), nil
	}

	// Pre-create connections for removal.
	serverURLs := make([]string, b.N)
	for i := 0; i < b.N; i++ {
		serverURLs[i] = fmt.Sprintf("test://server%d", i)

		_, err := pool.GetConnection(ctx, serverURLs[i], ClientTypeStdio, createFunc)
		if err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		err := pool.RemoveConnection(serverURLs[i], ClientTypeStdio)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// Memory-focused benchmarks.
func BenchmarkPoolPerformance_MemoryAllocations(b *testing.B) {
	logger := zaptest.NewLogger(b)
	config := DefaultConnectionPoolConfig()
	config.EnableConnectionReuse = true

	pool := NewConnectionPool(config, logger)

	defer func() {
		if err := pool.Close(); err != nil {
			b.Logf("Failed to close pool: %v", err)
		}
	}()

	ctx := context.Background()
	createFunc := func() (DirectClient, error) {
		return NewMockDirectClient("test", "stdio", "test://example"), nil
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := pool.GetConnection(ctx, "test://server", ClientTypeStdio, createFunc)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPoolPerformance_LargePoolStats(b *testing.B) {
	logger := zaptest.NewLogger(b)
	config := DefaultConnectionPoolConfig()
	config.MaxActiveConnections = testMaxIterations

	pool := NewConnectionPool(config, logger)

	defer func() {
		if err := pool.Close(); err != nil {
			b.Logf("Failed to close pool: %v", err)
		}
	}()

	// Create a large number of connections.
	ctx := context.Background()
	createFunc := func() (DirectClient, error) {
		return NewMockDirectClient("test", "stdio", "test://example"), nil
	}

	for i := 0; i < httpStatusInternalError; i++ {
		_, err := pool.GetConnection(ctx, fmt.Sprintf("test://server%d", i), ClientTypeStdio, createFunc)
		if err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = pool.GetStats()
	}
}

// Timeout and adaptive performance benchmarks.
func BenchmarkTimeoutTuning_ProfileGeneration(b *testing.B) {
	logger := zaptest.NewLogger(b)

	profiles := []TimeoutProfile{
		TimeoutProfileFast, TimeoutProfileDefault, TimeoutProfileRobust,
		TimeoutProfileBulk, TimeoutProfileInteractive,
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		config := DefaultTimeoutTuningConfig()
		config.TimeoutProfile = profiles[i%len(profiles)]
		tuner := NewTimeoutTuner(config, logger)
		_ = tuner.GetOptimizedTimeoutConfig()
		_ = tuner.GetOptimizedRetryConfig()
	}
}

func BenchmarkTimeoutTuning_NetworkAdjustments(b *testing.B) {
	logger := zaptest.NewLogger(b)

	conditions := []NetworkCondition{
		NetworkConditionExcellent, NetworkConditionGood,
		NetworkConditionFair, NetworkConditionPoor,
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		config := DefaultTimeoutTuningConfig()
		config.NetworkCondition = conditions[i%len(conditions)]
		tuner := NewTimeoutTuner(config, logger)
		_ = tuner.GetOptimizedTimeoutConfig()
	}
}

func BenchmarkAdaptiveTimeout_MixedOperations(b *testing.B) {
	logger := zaptest.NewLogger(b)
	config := AdaptiveTimeoutConfig{
		BaseTimeout:  30 * time.Second,
		WindowSize:   testIterations,
		SuccessRatio: 0.95,
	}

	adaptive := NewAdaptiveTimeout(config, logger)
	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			switch i % 3 {
			case 0:
				_ = adaptive.GetTimeout(ctx)
			case 1:
				metrics := RequestMetrics{
					StartTime: time.Now(),
					Duration:  time.Duration(i%testMaxIterations) * time.Millisecond,
					Success:   i%4 != 0,
				}
				adaptive.RecordRequest(metrics)
			default:
				_ = adaptive.GetStats()
			}

			i++
		}
	})
}

// Load testing benchmarks.
func BenchmarkPoolPerformance_SustainedLoad(b *testing.B) {
	if testing.Short() {
		b.Skip("Skipping sustained load test in short mode")
	}

	logger := zaptest.NewLogger(b)
	config := DefaultConnectionPoolConfig()
	config.MaxActiveConnections = testIterations
	config.CleanupInterval = testIterations * time.Millisecond

	pool := NewConnectionPool(config, logger)

	defer func() {
		if err := pool.Close(); err != nil {
			b.Logf("Failed to close pool: %v", err)
		}
	}()

	ctx := context.Background()
	createFunc := func() (DirectClient, error) {
		time.Sleep(1 * time.Millisecond) // Simulate connection creation cost

		return NewMockDirectClient("test", "stdio", "test://example"), nil
	}

	// Warm up.
	for i := 0; i < testTimeout; i++ {
		_, _ = pool.GetConnection(ctx, fmt.Sprintf("test://server%d", i%10), ClientTypeStdio, createFunc)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			serverURL := fmt.Sprintf("test://server%d", i%20)

			_, err := pool.GetConnection(ctx, serverURL, ClientTypeStdio, createFunc)
			if err != nil && err.Error() != "maximum active connections reached (testIterations)" {
				b.Error(err)
			}

			i++
			if i%testIterations == 0 {
				time.Sleep(1 * time.Millisecond) // Brief pause to simulate real usage
			}
		}
	})
}

// Stress testing for edge cases.
func BenchmarkPoolPerformance_HighChurnRate(b *testing.B) {
	logger := zaptest.NewLogger(b)
	config := DefaultConnectionPoolConfig()
	config.MaxActiveConnections = testTimeout
	config.ConnectionTTL = testIterations * time.Millisecond // Short TTL for high churn
	config.CleanupInterval = testTimeout * time.Millisecond

	pool := NewConnectionPool(config, logger)

	defer func() {
		if err := pool.Close(); err != nil {
			b.Logf("Failed to close pool: %v", err)
		}
	}()

	ctx := context.Background()
	createFunc := func() (DirectClient, error) {
		return NewMockDirectClient("test", "stdio", "test://example"), nil
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := pool.GetConnection(ctx, fmt.Sprintf("test://server%d", i%10), ClientTypeStdio, createFunc)
		if err != nil && err.Error() != "maximum active connections reached (testTimeout)" {
			b.Fatal(err)
		}

		if i%10 == 0 {
			time.Sleep(150 * time.Millisecond) // Allow some connections to expire
		}
	}
}

// Performance comparison benchmarks.
func BenchmarkPoolPerformance_ComparePoolingStrategies(b *testing.B) {
	logger := zaptest.NewLogger(b)

	b.Run("WithPooling", func(b *testing.B) {
		config := DefaultConnectionPoolConfig()
		config.EnableConnectionReuse = true

		pool := NewConnectionPool(config, logger)

		defer func() {
			if err := pool.Close(); err != nil {
				b.Logf("Failed to close pool: %v", err)
			}
		}()

		ctx := context.Background()
		createFunc := func() (DirectClient, error) {
			return NewMockDirectClient("test", "stdio", "test://example"), nil
		}

		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			_, err := pool.GetConnection(ctx, "test://server", ClientTypeStdio, createFunc)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("WithoutPooling", func(b *testing.B) {
		config := DefaultConnectionPoolConfig()
		config.EnableConnectionReuse = false

		pool := NewConnectionPool(config, logger)

		defer func() {
			if err := pool.Close(); err != nil {
				b.Logf("Failed to close pool: %v", err)
			}
		}()

		ctx := context.Background()
		createFunc := func() (DirectClient, error) {
			return NewMockDirectClient("test", "stdio", "test://example"), nil
		}

		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			_, err := pool.GetConnection(ctx, fmt.Sprintf("test://server%d", i), ClientTypeStdio, createFunc)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

// Throughput measurement benchmark.
func BenchmarkPoolPerformance_Throughput(b *testing.B) {
	logger := zaptest.NewLogger(b)
	config := DefaultConnectionPoolConfig()
	config.MaxActiveConnections = httpStatusOK

	pool := NewConnectionPool(config, logger)

	defer func() {
		if err := pool.Close(); err != nil {
			b.Logf("Failed to close pool: %v", err)
		}
	}()

	ctx := context.Background()
	createFunc := func() (DirectClient, error) {
		return NewMockDirectClient("test", "stdio", "test://example"), nil
	}

	var requestCount int64

	const duration = 5 * time.Second

	// Run for fixed duration and measure throughput.
	start := time.Now()

	var wg sync.WaitGroup

	for i := 0; i < runtime.NumCPU(); i++ {
		wg.Add(1)

		go func(workerID int) {
			defer wg.Done()

			for time.Since(start) < duration {
				serverURL := fmt.Sprintf("test://server%d", workerID%10)

				_, err := pool.GetConnection(ctx, serverURL, ClientTypeStdio, createFunc)
				if err == nil {
					atomic.AddInt64(&requestCount, 1)
				}
			}
		}(i)
	}

	wg.Wait()

	elapsed := time.Since(start)
	totalRequests := atomic.LoadInt64(&requestCount)

	b.ReportMetric(float64(totalRequests)/elapsed.Seconds(), "requests/sec")
	b.ReportMetric(float64(totalRequests), "total_requests")
}

// Memory optimization benchmarks.
func BenchmarkMemoryOptimization_BufferOperations(b *testing.B) {
	logger := zaptest.NewLogger(b)

	b.Run("WithPooling", func(b *testing.B) {
		config := DefaultMemoryOptimizationConfig()
		config.BufferPoolConfig.Enabled = true
		optimizer := NewMemoryOptimizer(config, logger)

		b.ReportAllocs()
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				buf := optimizer.GetBuffer()
				_, _ = buf.WriteString("test data for performance measurement")
				optimizer.PutBuffer(buf)
			}
		})
	})

	b.Run("WithoutPooling", func(b *testing.B) {
		config := DefaultMemoryOptimizationConfig()
		config.BufferPoolConfig.Enabled = false
		optimizer := NewMemoryOptimizer(config, logger)

		b.ReportAllocs()
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				buf := optimizer.GetBuffer()
				_, _ = buf.WriteString("test data for performance measurement")
				optimizer.PutBuffer(buf)
			}
		})
	})
}

func BenchmarkMemoryOptimization_JSONOperations(b *testing.B) {
	logger := zaptest.NewLogger(b)

	testData := map[string]interface{}{
		"string_field":  "test value",
		"number_field":  42,
		"boolean_field": true,
		"array_field":   []int{1, 2, 3, 4, 5},
		"object_field":  map[string]string{"nested": "value"},
	}

	b.Run("WithPooling", func(b *testing.B) {
		config := DefaultMemoryOptimizationConfig()
		config.JSONPoolConfig.Enabled = true
		optimizer := NewMemoryOptimizer(config, logger)

		b.ReportAllocs()
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				buf := optimizer.GetBuffer()
				encoder := optimizer.GetJSONEncoder(buf)

				err := encoder.Encode(testData)
				if err != nil {
					b.Fatal(err)
				}

				optimizer.PutJSONEncoder(encoder)
				optimizer.PutBuffer(buf)
			}
		})
	})

	b.Run("WithoutPooling", func(b *testing.B) {
		config := DefaultMemoryOptimizationConfig()
		config.JSONPoolConfig.Enabled = false
		optimizer := NewMemoryOptimizer(config, logger)

		b.ReportAllocs()
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				buf := optimizer.GetBuffer()
				encoder := optimizer.GetJSONEncoder(buf)

				err := encoder.Encode(testData)
				if err != nil {
					b.Fatal(err)
				}

				optimizer.PutJSONEncoder(encoder)
				optimizer.PutBuffer(buf)
			}
		})
	})
}
