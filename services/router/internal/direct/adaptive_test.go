package direct

import (
	"context"
	"github.com/poiley/mcp-bridge/services/router/internal/constants"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func TestAdaptiveTimeout_Basic(t *testing.T) { 
	t.Parallel()
	logger := zaptest.NewLogger(t)
	config := AdaptiveTimeoutConfig{
		BaseTimeout:    30 * time.Second,
		MinTimeout:     5 * time.Second,
		MaxTimeout:     120 * time.Second,
		WindowSize:     50,
		SuccessRatio:   0.95,
		AdaptationRate: 0.1,
		EnableLearning: true,
		LearningPeriod: 1 * time.Minute,
	}

	adaptive := NewAdaptiveTimeout(config, logger)
	require.NotNil(t, adaptive)

	// Test initial timeout.
	ctx := context.Background()
	timeout := adaptive.GetTimeout(ctx)
	assert.Equal(t, config.BaseTimeout, timeout)

	// Test stats.
	stats := adaptive.GetStats()
	assert.Equal(t, int64(0), stats["total_requests"])
	assert.InDelta(t, float64(0), stats["success_ratio"], 0.0001) // Use InDelta for float comparison
	assert.Equal(t, config.BaseTimeout, stats["current_timeout"])
}

func TestAdaptiveTimeout_Defaults(t *testing.T) { 
	t.Parallel()
	logger := zaptest.NewLogger(t)
	config := AdaptiveTimeoutConfig{} // Empty config to test defaults

	adaptive := NewAdaptiveTimeout(config, logger)
	require.NotNil(t, adaptive)

	// Check that defaults were applied.
	assert.Equal(t, 30*time.Second, adaptive.config.BaseTimeout)
	assert.Equal(t, 1*time.Second, adaptive.config.MinTimeout)
	assert.Equal(t, 300*time.Second, adaptive.config.MaxTimeout)
	assert.Equal(t, 100, adaptive.config.WindowSize)
	assert.InEpsilon(t, 0.95, adaptive.config.SuccessRatio, 0.001)
	assert.InEpsilon(t, 0.1, adaptive.config.AdaptationRate, 0.001)
	assert.Equal(t, 5*time.Minute, adaptive.config.LearningPeriod)
}

func TestAdaptiveTimeout_ContextDeadline(t *testing.T) { 
	t.Parallel()
	logger := zaptest.NewLogger(t)
	config := AdaptiveTimeoutConfig{
		BaseTimeout: 30 * time.Second,
		MinTimeout:  5 * time.Second,
		MaxTimeout:  120 * time.Second,
	}

	adaptive := NewAdaptiveTimeout(config, logger)

	// Test with context deadline shorter than base timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	timeout := adaptive.GetTimeout(ctx)
	assert.LessOrEqual(t, timeout, 10*time.Second)

	// Test with context deadline longer than base timeout.
	ctx2, cancel2 := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel2()

	timeout2 := adaptive.GetTimeout(ctx2)
	assert.Equal(t, config.BaseTimeout, timeout2)

	// Test with no deadline.
	ctx3 := context.Background()
	timeout3 := adaptive.GetTimeout(ctx3)
	assert.Equal(t, config.BaseTimeout, timeout3)
}

func TestAdaptiveTimeout_RecordRequest(t *testing.T) { 
	t.Parallel()
	logger := zaptest.NewLogger(t)
	config := AdaptiveTimeoutConfig{
		BaseTimeout:  30 * time.Second,
		WindowSize:   5,
		SuccessRatio: 0.95,
	}

	adaptive := NewAdaptiveTimeout(config, logger)

	// Record some successful requests.
	for i := 0; i < 3; i++ {
		metrics := RequestMetrics{
			StartTime:    time.Now().Add(-100 * time.Millisecond),
			Duration:     100 * time.Millisecond,
			Success:      true,
			ErrorType:    "",
			RetryAttempt: 0,
		}
		adaptive.RecordRequest(metrics)
	}

	stats := adaptive.GetStats()
	assert.Equal(t, int64(3), stats["total_requests"])
	assert.InEpsilon(t, float64(1.0), stats["success_ratio"], 0.001)
	assert.Equal(t, 100*time.Millisecond, stats["avg_response_time"])
	assert.Equal(t, 3, stats["window_size"])

	// Record a failed request.
	failedMetrics := RequestMetrics{
		StartTime:    time.Now().Add(-200 * time.Millisecond),
		Duration:     200 * time.Millisecond,
		Success:      false,
		ErrorType:    "timeout",
		RetryAttempt: 1,
	}
	adaptive.RecordRequest(failedMetrics)

	stats = adaptive.GetStats()
	assert.Equal(t, int64(4), stats["total_requests"])
	assert.InEpsilon(t, float64(0.75), stats["success_ratio"], 0.001) // 3/4 = 0.75
	assert.Equal(t, 4, stats["window_size"])
}

func TestAdaptiveTimeout_WindowSizeLimit(t *testing.T) { 
	t.Parallel()
	logger := zaptest.NewLogger(t)
	config := AdaptiveTimeoutConfig{
		BaseTimeout: 30 * time.Second,
		WindowSize:  3, // Small window for testing
	}

	adaptive := NewAdaptiveTimeout(config, logger)

	// Add more requests than window size.
	for i := 0; i < 5; i++ {
		metrics := RequestMetrics{
			StartTime: time.Now(),
			Duration:  time.Duration(i) * 10 * time.Millisecond,
			Success:   true,
		}
		adaptive.RecordRequest(metrics)
	}

	stats := adaptive.GetStats()
	assert.Equal(t, int64(5), stats["total_requests"])
	assert.Equal(t, 3, stats["window_size"]) // Should be limited to WindowSize
}

func TestAdaptiveTimeout_Adaptation(t *testing.T) { 
	t.Parallel()

	adaptive, config := setupAdaptiveTest(t)
	initialTimeout := adaptive.GetTimeout(context.Background())

	// Test timeout increase with failures
	recordFailedRequests(adaptive, 20)
	newTimeout := waitForTimeoutIncrease(t, adaptive, initialTimeout)

	assert.Greater(t, newTimeout, initialTimeout)
	assert.LessOrEqual(t, newTimeout, config.MaxTimeout)

	// Test timeout decrease with successes
	recordSuccessfulRequests(adaptive, 20)
	finalTimeout := waitForTimeoutDecrease(t, adaptive, newTimeout)

	assert.Less(t, finalTimeout, newTimeout)
	assert.GreaterOrEqual(t, finalTimeout, config.MinTimeout)
}

func setupAdaptiveTest(t *testing.T) (*AdaptiveTimeout, AdaptiveTimeoutConfig) {
	t.Helper()
	logger := zaptest.NewLogger(t)
	config := AdaptiveTimeoutConfig{
		BaseTimeout:    10 * time.Second,
		MinTimeout:     2 * time.Second,
		MaxTimeout:     30 * time.Second,
		WindowSize:     20,
		SuccessRatio:   0.95,
		AdaptationRate: 0.2, // Higher rate for faster testing
		EnableLearning: true,
		LearningPeriod: 100 * time.Millisecond, // Short for testing
	}
	adaptive := NewAdaptiveTimeout(config, logger)

	return adaptive, config
}

func recordFailedRequests(adaptive *AdaptiveTimeout, count int) {
	for i := 0; i < count; i++ {
		metrics := RequestMetrics{
			StartTime: time.Now(),
			Duration:  15 * time.Second, // Long duration
			Success:   false,            // Failed
			ErrorType: "timeout",
		}
		adaptive.RecordRequest(metrics)
	}
}

func recordSuccessfulRequests(adaptive *AdaptiveTimeout, count int) {
	for i := 0; i < count; i++ {
		metrics := RequestMetrics{
			StartTime: time.Now(),
			Duration:  1 * time.Second, // Fast response
			Success:   true,            // Successful
		}
		adaptive.RecordRequest(metrics)
	}
}

func waitForTimeoutIncrease(t *testing.T, adaptive *AdaptiveTimeout, initialTimeout time.Duration) time.Duration {
	t.Helper()

	// Trigger adaptation
	time.Sleep(2 * constants.TestSleepShort)

	// Poll for timeout increase
	for i := 0; i < constants.TestBatchSize; i++ {
		newTimeout := adaptive.GetTimeout(context.Background())
		if newTimeout > initialTimeout {
			time.Sleep(100 * time.Millisecond) // Wait for adaptation to complete

			return newTimeout
		}

		time.Sleep(constants.TestLongTickInterval)
	}

	return adaptive.GetTimeout(context.Background())
}

func waitForTimeoutDecrease(t *testing.T, adaptive *AdaptiveTimeout, previousTimeout time.Duration) time.Duration {
	t.Helper()

	// Wait for learning period to fully elapse after last adaptation
	time.Sleep(2 * constants.TestSleepShort)

	// Force new adaptation by calling GetTimeout multiple times
	for i := 0; i < 30; i++ {
		finalTimeout := adaptive.GetTimeout(context.Background())
		if finalTimeout < previousTimeout {
			t.Logf("Timeout decreased from %v to %v after %d attempts", previousTimeout, finalTimeout, i+1)

			return finalTimeout
		}

		time.Sleep(constants.TestLongTickInterval)
	}

	// Log diagnostic info if timeout didn't decrease
	finalTimeout := adaptive.GetTimeout(context.Background())
	if finalTimeout >= previousTimeout {
		t.Logf("Timeout did not decrease after 30 attempts. Current: %v, Previous: %v", finalTimeout, previousTimeout)

		stats := adaptive.GetStats()
		t.Logf("Adaptation stats: %+v", stats)
	}

	return finalTimeout
}

func TestAdaptiveTimeout_AdaptationConstraints(t *testing.T) { 
	t.Parallel()
	logger := zaptest.NewLogger(t)
	config := AdaptiveTimeoutConfig{
		BaseTimeout:    10 * time.Second,
		MinTimeout:     5 * time.Second,
		MaxTimeout:     20 * time.Second,
		WindowSize:     15,
		SuccessRatio:   0.95,
		AdaptationRate: 0.5, // High rate
		EnableLearning: true,
		LearningPeriod: 100 * time.Millisecond,
	}

	adaptive := NewAdaptiveTimeout(config, logger)

	// Record many failed requests with very long durations.
	for i := 0; i < 20; i++ {
		metrics := RequestMetrics{
			StartTime: time.Now(),
			Duration:  60 * time.Second, // Very long
			Success:   false,
		}
		adaptive.RecordRequest(metrics)
	}

	// Trigger adaptation.
	time.Sleep(150 * time.Millisecond)

	timeout := adaptive.GetTimeout(context.Background())

	// Should not exceed max timeout.
	assert.LessOrEqual(t, timeout, config.MaxTimeout)

	// Record many very fast successful requests.
	for i := 0; i < 20; i++ {
		metrics := RequestMetrics{
			StartTime: time.Now(),
			Duration:  100 * time.Millisecond, // Very fast
			Success:   true,
		}
		adaptive.RecordRequest(metrics)
	}

	// Trigger adaptation.
	time.Sleep(150 * time.Millisecond)

	timeout = adaptive.GetTimeout(context.Background())

	// Should not go below min timeout.
	assert.GreaterOrEqual(t, timeout, config.MinTimeout)
}

func TestAdaptiveTimeout_LearningDisabled(t *testing.T) { 
	t.Parallel()
	logger := zaptest.NewLogger(t)
	config := AdaptiveTimeoutConfig{
		BaseTimeout:    10 * time.Second,
		EnableLearning: false, // Disabled
		WindowSize:     10,
	}

	adaptive := NewAdaptiveTimeout(config, logger)
	initialTimeout := adaptive.GetTimeout(context.Background())

	// Record many failed requests.
	for i := 0; i < 15; i++ {
		metrics := RequestMetrics{
			StartTime: time.Now(),
			Duration:  20 * time.Second,
			Success:   false,
		}
		adaptive.RecordRequest(metrics)
	}

	// Wait and check timeout.
	time.Sleep(100 * time.Millisecond)

	finalTimeout := adaptive.GetTimeout(context.Background())

	// Timeout should not have changed since learning is disabled.
	assert.Equal(t, initialTimeout, finalTimeout)
}

func TestAdaptiveTimeout_InsufficientData(t *testing.T) { 
	t.Parallel()
	logger := zaptest.NewLogger(t)
	config := AdaptiveTimeoutConfig{
		BaseTimeout:    10 * time.Second,
		EnableLearning: true,
		LearningPeriod: 100 * time.Millisecond,
		WindowSize:     20,
	}

	adaptive := NewAdaptiveTimeout(config, logger)
	initialTimeout := adaptive.GetTimeout(context.Background())

	// Record only a few requests (less than minimum for adaptation).
	for i := 0; i < 5; i++ {
		metrics := RequestMetrics{
			StartTime: time.Now(),
			Duration:  20 * time.Second,
			Success:   false,
		}
		adaptive.RecordRequest(metrics)
	}

	// Trigger adaptation attempt.
	time.Sleep(150 * time.Millisecond)

	timeout := adaptive.GetTimeout(context.Background())

	// Should not have adapted due to insufficient data.
	assert.Equal(t, initialTimeout, timeout)
}

func TestAdaptiveTimeout_ConcurrentAccess(t *testing.T) { 
	t.Parallel()

	adaptive, config := setupConcurrentAdaptiveTest(t)

	const (
		numGoroutines        = 20
		requestsPerGoroutine = 50
	)

	var wg sync.WaitGroup

	ctx := context.Background()

	// Run concurrent operations
	runConcurrentTimeoutCalls(t, &wg, adaptive, ctx, numGoroutines, requestsPerGoroutine)
	runConcurrentRecordCalls(t, &wg, adaptive, numGoroutines, requestsPerGoroutine)
	runConcurrentStatsCalls(t, &wg, adaptive, numGoroutines, requestsPerGoroutine)

	wg.Wait()

	// Verify final state consistency
	verifyFinalConcurrencyState(t, adaptive, config, numGoroutines, requestsPerGoroutine)
}

func setupConcurrentAdaptiveTest(t *testing.T) (*AdaptiveTimeout, AdaptiveTimeoutConfig) {
	t.Helper()
	logger := zaptest.NewLogger(t)
	config := AdaptiveTimeoutConfig{
		BaseTimeout: 10 * time.Second,
		WindowSize:  100,
	}
	adaptive := NewAdaptiveTimeout(config, logger)

	return adaptive, config
}

func runConcurrentTimeoutCalls(
	t *testing.T,
	wg *sync.WaitGroup,
	adaptive *AdaptiveTimeout,
	ctx context.Context,
	numGoroutines,
	requestsPerGoroutine int,
) {
	t.Helper()

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)

		go func(routineID int) {
			defer wg.Done()

			for j := 0; j < requestsPerGoroutine; j++ {
				timeout := adaptive.GetTimeout(ctx)
				assert.Greater(t, timeout, time.Duration(0))
			}
		}(i)
	}
}

func runConcurrentRecordCalls(
	t *testing.T,
	wg *sync.WaitGroup,
	adaptive *AdaptiveTimeout,
	numGoroutines,
	requestsPerGoroutine int,
) {
	t.Helper()

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)

		go func(routineID int) {
			defer wg.Done()

			for j := 0; j < requestsPerGoroutine; j++ {
				metrics := RequestMetrics{
					StartTime:    time.Now(),
					Duration:     time.Duration(j) * time.Millisecond,
					Success:      j%2 == 0, // Alternate success/failure
					RetryAttempt: j % 3,
				}
				adaptive.RecordRequest(metrics)
			}
		}(i)
	}
}

func runConcurrentStatsCalls(
	t *testing.T,
	wg *sync.WaitGroup,
	adaptive *AdaptiveTimeout,
	numGoroutines,
	requestsPerGoroutine int,
) {
	t.Helper()

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for j := 0; j < requestsPerGoroutine; j++ {
				stats := adaptive.GetStats()
				assert.NotNil(t, stats)
				assert.Contains(t, stats, "total_requests")
			}
		}()
	}
}

func verifyFinalConcurrencyState(
	t *testing.T,
	adaptive *AdaptiveTimeout,
	config AdaptiveTimeoutConfig,
	numGoroutines,
	requestsPerGoroutine int,
) {
	t.Helper()

	stats := adaptive.GetStats()
	expectedRequests := int64(numGoroutines * requestsPerGoroutine)
	assert.Equal(t, expectedRequests, stats["total_requests"])
	assert.LessOrEqual(t, stats["window_size"], config.WindowSize)
}

func TestAdaptiveTimeout_SuccessRatioCalculation(t *testing.T) { 
	t.Parallel()
	logger := zaptest.NewLogger(t)
	config := AdaptiveTimeoutConfig{
		BaseTimeout: 10 * time.Second,
		WindowSize:  10,
	}

	adaptive := NewAdaptiveTimeout(config, logger)

	// Record 7 successful and 3 failed requests.
	for i := 0; i < 7; i++ {
		adaptive.RecordRequest(RequestMetrics{
			StartTime: time.Now(),
			Duration:  100 * time.Millisecond,
			Success:   true,
		})
	}

	for i := 0; i < 3; i++ {
		adaptive.RecordRequest(RequestMetrics{
			StartTime: time.Now(),
			Duration:  100 * time.Millisecond,
			Success:   false,
		})
	}

	stats := adaptive.GetStats()
	assert.InEpsilon(t, float64(0.7), stats["success_ratio"], 0.001) // 7/10 = 0.7
	assert.Equal(t, int64(10), stats["total_requests"])
	assert.Equal(t, 10, stats["window_size"])
}

func TestAdaptiveTimeout_AverageResponseTime(t *testing.T) { 
	t.Parallel()
	logger := zaptest.NewLogger(t)
	config := AdaptiveTimeoutConfig{
		BaseTimeout: 10 * time.Second,
	}

	adaptive := NewAdaptiveTimeout(config, logger)

	// Record requests with known durations.
	durations := []time.Duration{
		100 * time.Millisecond,
		200 * time.Millisecond,
		300 * time.Millisecond,
	}

	for _, duration := range durations {
		adaptive.RecordRequest(RequestMetrics{
			StartTime: time.Now(),
			Duration:  duration,
			Success:   true,
		})
	}

	stats := adaptive.GetStats()

	// Average should be computed correctly (simple moving average).
	// Note: The implementation uses a simple moving average, not arithmetic mean.
	avgTime, ok := stats["avg_response_time"].(time.Duration)
	assert.True(t, ok, "avg_response_time should be time.Duration")
	assert.Greater(t, avgTime, time.Duration(0))
	assert.LessOrEqual(t, avgTime, 300*time.Millisecond)
}

// Benchmark tests.
func BenchmarkAdaptiveTimeout_GetTimeout(b *testing.B) {
	logger := zaptest.NewLogger(b)
	config := AdaptiveTimeoutConfig{
		BaseTimeout: 30 * time.Second,
	}

	adaptive := NewAdaptiveTimeout(config, logger)
	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = adaptive.GetTimeout(ctx)
		}
	})
}

func BenchmarkAdaptiveTimeout_RecordRequest(b *testing.B) {
	logger := zaptest.NewLogger(b)
	config := AdaptiveTimeoutConfig{
		BaseTimeout: 30 * time.Second,
		WindowSize:  1000,
	}

	adaptive := NewAdaptiveTimeout(config, logger)

	metrics := RequestMetrics{
		StartTime:    time.Now(),
		Duration:     100 * time.Millisecond,
		Success:      true,
		RetryAttempt: 0,
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			adaptive.RecordRequest(metrics)
		}
	})
}

func BenchmarkAdaptiveTimeout_GetStats(b *testing.B) {
	logger := zaptest.NewLogger(b)
	config := AdaptiveTimeoutConfig{
		BaseTimeout: 30 * time.Second,
	}

	adaptive := NewAdaptiveTimeout(config, logger)

	// Pre-populate with some data.
	for i := 0; i < constants.TestConcurrentRoutines; i++ {
		adaptive.RecordRequest(RequestMetrics{
			StartTime: time.Now(),
			Duration:  time.Duration(i) * time.Millisecond,
			Success:   i%2 == 0,
		})
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = adaptive.GetStats()
	}
}

func TestAdaptiveTimeout_RealWorldScenario(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping real-world scenario test in short mode")
	}
	
	adaptive := setupAdaptiveTimeoutTest(t)
	initialTimeout := simulateGoodPerformance(t, adaptive)
	peakTimeout := simulateDegradedPerformance(t, adaptive, initialTimeout)
	finalTimeout := simulateRecoveryPerformance(t, adaptive)
	verifyAdaptiveBehavior(t, adaptive, initialTimeout, peakTimeout, finalTimeout)
}

func setupAdaptiveTimeoutTest(t *testing.T) *AdaptiveTimeout {
	t.Helper()
	
	logger := zaptest.NewLogger(t)
	config := AdaptiveTimeoutConfig{
		BaseTimeout:    5 * time.Second,
		MinTimeout:     1 * time.Second,
		MaxTimeout:     30 * time.Second,
		WindowSize:     50,
		SuccessRatio:   0.95,
		AdaptationRate: 0.15,
		EnableLearning: true,
		LearningPeriod: 200 * time.Millisecond,
	}
	
	return NewAdaptiveTimeout(config, logger)
}

func simulateGoodPerformance(t *testing.T, adaptive *AdaptiveTimeout) time.Duration {
	t.Helper()
	
	// Simulate initial good performance
	for i := 0; i < 20; i++ {
		adaptive.RecordRequest(RequestMetrics{
			StartTime: time.Now(),
			Duration:  500 * time.Millisecond,
			Success:   true,
		})
	}
	
	// Let adaptation occur
	time.Sleep(250 * time.Millisecond)
	
	return adaptive.GetTimeout(context.Background())
}

func simulateDegradedPerformance(t *testing.T, adaptive *AdaptiveTimeout, initialTimeout time.Duration) time.Duration {
	t.Helper()
	
	// Simulate degraded performance
	for i := 0; i < 50; i++ {
		adaptive.RecordRequest(RequestMetrics{
			StartTime: time.Now(),
			Duration:  8 * time.Second,
			Success:   i%5 != 0, // 80% success rate - below 95% threshold
		})
	}
	
	// Track peak timeout during degradation
	var peakTimeout time.Duration
	for i := 0; i < 5; i++ {
		time.Sleep(250 * time.Millisecond)
		
		currentTimeout := adaptive.GetTimeout(context.Background())
		if currentTimeout > peakTimeout {
			peakTimeout = currentTimeout
		}
	}
	
	return peakTimeout
}

func simulateRecoveryPerformance(t *testing.T, adaptive *AdaptiveTimeout) time.Duration {
	t.Helper()
	
	// Simulate recovery with overwhelming successful requests
	for i := 0; i < 100; i++ {
		adaptive.RecordRequest(RequestMetrics{
			StartTime: time.Now(),
			Duration:  300 * time.Millisecond,
			Success:   true,
		})
	}
	
	// Wait for recovery adaptation
	time.Sleep(500 * time.Millisecond)
	
	return adaptive.GetTimeout(context.Background())
}

func verifyAdaptiveBehavior(t *testing.T, adaptive *AdaptiveTimeout, initialTimeout, peakTimeout, finalTimeout time.Duration) {
	t.Helper()
	
	config := AdaptiveTimeoutConfig{
		MinTimeout: 1 * time.Second,
		MaxTimeout: 30 * time.Second,
	}
	
	// Verify adaptive behavior
	assert.Greater(t, peakTimeout, initialTimeout, "Timeout should increase during degraded performance")
	assert.Less(t, finalTimeout, peakTimeout, "Timeout should decrease during recovery")
	assert.GreaterOrEqual(t, finalTimeout, config.MinTimeout)
	assert.LessOrEqual(t, peakTimeout, config.MaxTimeout)
	
	// Verify stats are reasonable
	stats := adaptive.GetStats()
	assert.Greater(t, stats["total_requests"], int64(160)) // 20 + 50 + 100 = 170 total
	assert.Greater(t, stats["success_ratio"], float64(0.9))
}

// Test the abs helper function.
func TestAbs(t *testing.T) { 
	t.Parallel()

	assert.Equal(t, 5*time.Second, abs(5*time.Second))
	assert.Equal(t, 5*time.Second, abs(-5*time.Second))
	assert.Equal(t, time.Duration(0), abs(time.Duration(0)))
}
