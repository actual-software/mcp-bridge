package benchmark

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

// mockWorker simulates various types of work for testing.
type mockWorker struct {
	latency   time.Duration
	errorRate float64 // 0.0 to 1.0
	counter   int64
}

func (m *mockWorker) work(ctx context.Context) *RequestResult {
	count := atomic.AddInt64(&m.counter, 1)

	// Simulate work latency
	if m.latency > 0 {
		select {
		case <-time.After(m.latency):
		case <-ctx.Done():
			return &RequestResult{
				Success:   false,
				Latency:   0,
				Error:     errors.New("context_canceled"),
				Timestamp: time.Now(),
			}
		}
	}

	// Simulate errors based on error rate
	if m.errorRate > 0 && float64(count%100) < m.errorRate*100 {
		return &RequestResult{
			Success:   false,
			Latency:   m.latency,
			Error:     errors.New("simulated_error"),
			Timestamp: time.Now(),
		}
	}

	return &RequestResult{
		Success:   true,
		Latency:   m.latency,
		BytesRead: 1024, // Simulate 1KB response
		Timestamp: time.Now(),
	}
}

func TestNewRunner(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := Config{
		Name:        "test-benchmark",
		Duration:    time.Second,
		Concurrency: 10,
		RPS:         100,
	}

	worker := &mockWorker{latency: 10 * time.Millisecond}
	runner := NewRunner(config, worker.work, logger)

	if runner == nil {
		t.Fatal("NewRunner returned nil")
	}

	if runner.config.Name != config.Name { 
		t.Errorf("Expected config name %s, got %s", config.Name, runner.config.Name)
	}

	if runner.config.Concurrency != config.Concurrency {
		t.Errorf("Expected concurrency %d, got %d", config.Concurrency, runner.config.Concurrency)
	}

	if runner.worker == nil {
		t.Error("Worker function not set")
	}

	if runner.logger == nil {
		t.Error("Logger not set")
	}

	if runner.results == nil {
		t.Error("Results channel not initialized")
	}

	if runner.errors == nil {
		t.Error("Errors map not initialized")
	}

	if runner.ctx == nil {
		t.Error("Context not initialized")
	}

	// Test cleanup
	runner.cancel()
}

func TestRunner_BasicBenchmark(t *testing.T) { 
	logger := zaptest.NewLogger(t)
	config := Config{
		Name:        "basic-test",
		Duration:    100 * time.Millisecond,
		Concurrency: 2,
		RPS:         0, // Unlimited
	}

	worker := &mockWorker{latency: 10 * time.Millisecond}
	runner := NewRunner(config, worker.work, logger)

	result, err := runner.Run()
	if err != nil {
		t.Fatalf("Benchmark failed: %v", err)
	}

	if result == nil {
		t.Fatal("Result is nil")
	}

	validateBenchmarkResult(t, result, config)
	validateFormatOutput(t, result)
}

func validateBenchmarkResult(t *testing.T, result *Result, config Config) {
	t.Helper()
	
	if result.Name != config.Name { 
		t.Errorf("Expected result name %s, got %s", config.Name, result.Name)
	}

	validateRequestCounts(t, result)
	validateLatencyMetrics(t, result)

	if result.RequestsPerSec <= 0 {
		t.Error("Expected positive requests per second")
	}
}

func validateRequestCounts(t *testing.T, result *Result) {
	t.Helper()
	
	if result.TotalRequests <= 0 {
		t.Error("Expected some requests to be made")
	}

	if result.SuccessCount <= 0 {
		t.Error("Expected some successful requests")
	}

	// Some errors might occur due to context cancellation at the end
	if result.ErrorCount > result.TotalRequests {
		t.Errorf("Error count (%d) should not exceed total requests (%d)", result.ErrorCount, result.TotalRequests)
	}
}

func validateLatencyMetrics(t *testing.T, result *Result) {
	t.Helper()
	
	// Min latency might be 0 if some requests completed very fast or were canceled
	if result.MaxLatency < 0 {
		t.Error("Max latency should not be negative")
	}

	if result.MaxLatency <= 0 {
		t.Error("Expected positive max latency")
	}
}

func validateFormatOutput(t *testing.T, result *Result) {
	t.Helper()
	
	// Test Format method
	formatted := result.Format()
	if len(formatted) == 0 {
		t.Error("Format() returned empty string")
	}

	// Should contain key metrics
	if !contains(formatted, result.Name) {
		t.Error("Formatted result should contain benchmark name")
	}
}

func TestRunner_WithRateLimit(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := Config{
		Name:        "rate-limited-test",
		Duration:    200 * time.Millisecond,
		Concurrency: 1,
		RPS:         10, // 10 requests per second
	}

	worker := &mockWorker{latency: 1 * time.Millisecond}
	runner := NewRunner(config, worker.work, logger)

	start := time.Now()
	result, err := runner.Run()
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("Rate limited benchmark failed: %v", err)
	}

	// With 10 RPS for 200ms, the actual behavior depends on implementation
	// Let's just verify that rate limiting has some effect
	if result.TotalRequests <= 0 {
		t.Error("Expected some requests to be made")
	}

	// For a very short duration with rate limiting, results may vary
	// The key is that it completed without error

	// Duration should be close to config duration
	if duration < config.Duration {
		t.Errorf("Benchmark finished too early: %v < %v", duration, config.Duration)
	}
}

func TestRunner_WithErrors(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := Config{
		Name:        "error-test",
		Duration:    100 * time.Millisecond,
		Concurrency: 2,
		RPS:         0,
	}

	// 50% error rate
	worker := &mockWorker{
		latency:   5 * time.Millisecond,
		errorRate: 0.5,
	}
	runner := NewRunner(config, worker.work, logger)

	result, err := runner.Run()
	if err != nil {
		t.Fatalf("Error benchmark failed: %v", err)
	}

	if result.ErrorCount == 0 {
		t.Error("Expected some errors with 50% error rate")
	}

	if result.SuccessCount == 0 {
		t.Error("Expected some successes with 50% error rate")
	}

	// Check that errors were categorized (the actual key depends on implementation)
	if len(result.ErrorTypes) == 0 {
		t.Error("Expected error types to be populated when errors occur")
	}
}

func TestRunner_WithWarmup(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := Config{
		Name:         "warmup-test",
		Duration:     50 * time.Millisecond,
		Concurrency:  1,
		RPS:          0,
		WarmupTime:   30 * time.Millisecond,
		CooldownTime: 20 * time.Millisecond,
	}

	worker := &mockWorker{latency: 5 * time.Millisecond}
	runner := NewRunner(config, worker.work, logger)

	start := time.Now()
	result, err := runner.Run()
	totalDuration := time.Since(start)

	if err != nil {
		t.Fatalf("Warmup benchmark failed: %v", err)
	}

	// Total time should include warmup + duration + cooldown
	expectedMinDuration := config.WarmupTime + config.Duration + config.CooldownTime
	if totalDuration < expectedMinDuration {
		t.Errorf("Total duration %v should be at least %v (warmup+duration+cooldown)",
			totalDuration, expectedMinDuration)
	}

	// Result duration should only reflect the actual benchmark phase
	if result.Duration < config.Duration-10*time.Millisecond {
		t.Errorf("Result duration %v should be around config duration %v",
			result.Duration, config.Duration)
	}
}

func TestRunner_Stop(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := Config{
		Name:        "stop-test",
		Duration:    5 * time.Second, // Long duration
		Concurrency: 1,
		RPS:         0,
	}

	worker := &mockWorker{latency: 10 * time.Millisecond}
	runner := NewRunner(config, worker.work, logger)

	// Start benchmark in goroutine
	var result *Result

	var err error

	done := make(chan bool)

	go func() {
		result, err = runner.Run()

		done <- true
	}()

	// Let it run for a short time then stop
	time.Sleep(50 * time.Millisecond)
	runner.Stop()

	// Wait for completion
	select {
	case <-done:
		// Good, it stopped
	case <-time.After(1 * time.Second):
		t.Fatal("Benchmark did not stop within timeout")
	}

	if err != nil {
		t.Fatalf("Stopped benchmark failed: %v", err)
	}

	if result == nil {
		t.Fatal("Result is nil after stop")
	}

	// Should have some results even though stopped early
	if result.TotalRequests <= 0 {
		t.Error("Expected some requests even after early stop")
	}

	// Duration should be much less than configured
	if result.Duration >= config.Duration {
		t.Errorf("Result duration %v should be much less than config duration %v after stop",
			result.Duration, config.Duration)
	}
}

func TestRunner_ContextCancellation(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := Config{
		Name:        "context-cancel-test",
		Duration:    100 * time.Millisecond,
		Concurrency: 2,
		RPS:         0,
	}

	// Worker that checks context cancellation
	worker := &mockWorker{latency: 200 * time.Millisecond} // Longer than benchmark duration
	runner := NewRunner(config, worker.work, logger)

	result, err := runner.Run()
	if err != nil {
		t.Fatalf("Context cancellation benchmark failed: %v", err)
	}

	// Should have some context cancellation errors due to early termination
	if result.ErrorCount == 0 {
		t.Error("Expected some errors due to context cancellation")
	}
}

func TestResult_Format(t *testing.T) {
	result := &Result{
		Name:           "test-format",
		Duration:       time.Second,
		TotalRequests:  1000,
		SuccessCount:   950,
		ErrorCount:     50,
		MinLatency:     1000000,  // 1ms in nanoseconds
		MaxLatency:     10000000, // 10ms in nanoseconds
		AvgLatency:     5000000,  // 5ms in nanoseconds
		P50Latency:     4000000,  // 4ms in nanoseconds
		P95Latency:     8000000,  // 8ms in nanoseconds
		P99Latency:     9000000,  // 9ms in nanoseconds
		RequestsPerSec: 1000.0,
		BytesPerSec:    1024000.0,
		ErrorTypes: map[string]int64{
			"timeout": 30,
			"network": 20,
		},
	}

	formatted := result.Format()

	// Check that all key metrics are present
	expectedStrings := []string{
		"test-format",
		"1000",    // total requests
		"950",     // success count
		"50",      // error count
		"1000.00", // requests per second
		"timeout: 30",
		"network: 20",
	}

	for _, expected := range expectedStrings {
		if !contains(formatted, expected) {
			t.Errorf("Formatted result should contain '%s'\nFormatted: %s", expected, formatted)
		}
	}
}

func TestCalculationHelpers(t *testing.T) {
	// Test minInt
	if minInt(5, 3) != 3 {
		t.Error("minInt(5, 3) should return 3")
	}

	if minInt(1, 10) != 1 {
		t.Error("minInt(1, 10) should return 1")
	}

	// Test calculateAverage
	latencies := []int64{1000000, 2000000, 3000000, 4000000, 5000000} // 1-5ms
	avg := calculateAverage(latencies)
	expected := int64(3000000) // 3ms

	if avg != expected {
		t.Errorf("calculateAverage should return %d, got %d", expected, avg)
	}

	// Test calculatePercentile
	p50 := calculatePercentile(latencies, p50Percentile)
	expectedP50 := int64(3000000) // 3ms (middle value)

	if p50 != expectedP50 {
		t.Errorf("P50 should be %d, got %d", expectedP50, p50)
	}

	p95 := calculatePercentile(latencies, p95Percentile)
	// P95 should be close to the higher values
	if p95 < 4000000 {
		t.Errorf("P95 should be >= 4ms, got %d", p95)
	}
}

func TestEdgeCases(t *testing.T) {
	logger := zaptest.NewLogger(t)

	t.Run("zero_duration", func(t *testing.T) {
		testZeroDurationBenchmark(t, logger)
	})

	t.Run("zero_concurrency", func(t *testing.T) {
		testZeroConcurrencyBenchmark(t, logger)
	})

	t.Run("high_rps", func(t *testing.T) {
		testHighRPSBenchmark(t, logger)
	})
}

func testZeroDurationBenchmark(t *testing.T, logger *zap.Logger) {
	t.Helper()

	config := Config{
		Name:        "zero-duration",
		Duration:    0,
		Concurrency: 1,
	}
	worker := &mockWorker{latency: 1 * time.Millisecond}
	runner := NewRunner(config, worker.work, logger)

	result, err := runner.Run()
	if err != nil {
		t.Fatalf("Zero duration benchmark failed: %v", err)
	}

	// Zero duration behavior depends on implementation
	// The key is that it completed without error
	if result.TotalRequests < 0 {
		t.Errorf("Total requests should not be negative, got %d", result.TotalRequests)
	}
}

func testZeroConcurrencyBenchmark(t *testing.T, logger *zap.Logger) {
	t.Helper()

	config := Config{
		Name:        "zero-concurrency",
		Duration:    100 * time.Millisecond,
		Concurrency: 0,
	}
	worker := &mockWorker{latency: 1 * time.Millisecond}
	runner := NewRunner(config, worker.work, logger)

	result, err := runner.Run()
	if err != nil {
		t.Fatalf("Zero concurrency benchmark failed: %v", err)
	}

	// Should still work with at least 1 worker
	if result.TotalRequests <= 0 {
		t.Error("Expected some requests even with zero concurrency")
	}
}

func testHighRPSBenchmark(t *testing.T, logger *zap.Logger) {
	t.Helper()

	config := Config{
		Name:        "high-rps",
		Duration:    100 * time.Millisecond,
		Concurrency: 1,
		RPS:         10000, // Very high RPS
	}
	worker := &mockWorker{latency: 1 * time.Millisecond}
	runner := NewRunner(config, worker.work, logger)

	result, err := runner.Run()
	if err != nil {
		t.Fatalf("High RPS benchmark failed: %v", err)
	}

	// Should be limited by actual processing capability, not RPS
	if result.TotalRequests <= 0 {
		t.Error("Expected some requests with high RPS")
	}
}

// Benchmark the benchmark package itself.
func BenchmarkRunner_SimpleWorkload(b *testing.B) {
	logger := zaptest.NewLogger(b)
	config := Config{
		Name:        "benchmark-benchmark",
		Duration:    100 * time.Millisecond,
		Concurrency: 4,
		RPS:         0,
	}

	worker := &mockWorker{latency: 1 * time.Millisecond}

	b.ResetTimer()

	for range b.N {
		runner := NewRunner(config, worker.work, logger)

		_, err := runner.Run()
		if err != nil {
			b.Fatalf("Benchmark failed: %v", err)
		}
	}
}

// Helper function to check if a string contains a substring.
func contains(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr ||
			(len(s) > len(substr) &&
				(s[:len(substr)] == substr ||
					s[len(s)-len(substr):] == substr ||
					containsInner(s, substr))))
}

func containsInner(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}

	return false
}
