package ratelimit

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/poiley/mcp-bridge/services/router/internal/constants"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

func TestTokenBucketLimiter_Allow(t *testing.T) {
	logger := zaptest.NewLogger(t)

	tests := []struct {
		name      string
		rate      float64
		burst     int
		calls     int
		delay     time.Duration
		wantAllow int
	}{
		{
			name:      "burst capacity",
			rate:      10.0,
			burst:     5,
			calls:     5,
			delay:     0,
			wantAllow: 5,
		},
		{
			name:      "exceed burst",
			rate:      10.0,
			burst:     3,
			calls:     5,
			delay:     0,
			wantAllow: 3,
		},
		{
			name:      "with refill",
			rate:      10.0,
			burst:     1,
			calls:     3,
			delay:     200 * time.Millisecond, // Should refill ~2 tokens
			wantAllow: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			limiter := NewTokenBucketLimiter(tt.rate, tt.burst, time.Minute, logger)
			ctx := context.Background()

			allowed := 0

			for i := 0; i < tt.calls; i++ {
				if i > 0 && tt.delay > 0 {
					time.Sleep(tt.delay)
				}

				if err := limiter.Allow(ctx); err == nil {
					allowed++
				}
			}

			if allowed != tt.wantAllow {
				t.Errorf("Allowed %d requests, want %d", allowed, tt.wantAllow)
			}
		})
	}
}

func TestTokenBucketLimiter_Wait(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Rate of 10 per second, burst of 2.
	limiter := NewTokenBucketLimiter(10.0, 2, time.Second, logger)
	ctx := context.Background()

	// Use up burst.
	for i := 0; i < 2; i++ {
		if err := limiter.Allow(ctx); err != nil {
			t.Fatalf("Failed to use burst capacity: %v", err)
		}
	}

	// Next request should wait.
	start := time.Now()

	if err := limiter.Wait(ctx); err != nil {
		t.Fatalf("Wait() error = %v", err)
	}

	elapsed := time.Since(start)

	// Should wait approximately 100ms (1 token at 10/sec rate).
	expectedWait := 100 * time.Millisecond
	tolerance := 50 * time.Millisecond

	if elapsed < expectedWait-tolerance || elapsed > expectedWait+tolerance {
		t.Errorf("Wait time = %v, want approximately %v", elapsed, expectedWait)
	}
}

func TestTokenBucketLimiter_WaitContextCancellation(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Very slow rate to ensure waiting.
	limiter := NewTokenBucketLimiter(1.0, 1, time.Minute, logger)

	// Use up the token.
	_ = limiter.Allow(context.Background())

	// Create cancellable context.
	ctx, cancel := context.WithCancel(context.Background())

	// Start waiting in goroutine.
	done := make(chan error, 1)

	go func() {
		done <- limiter.Wait(ctx)
	}()

	// Cancel after short delay.
	time.Sleep(constants.TestLongTickInterval)
	cancel()

	// Should return context error.
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Expected context.Canceled, got %v", err)
		}
	case <-time.After(time.Second):
		t.Error("Wait did not return after context cancellation")
	}
}

func TestTokenBucketLimiter_MaxWait(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Very slow rate with short max wait.
	limiter := NewTokenBucketLimiter(0.1, 1, 10*time.Millisecond, logger)

	// Use up the token.
	_ = limiter.Allow(context.Background())

	// Should fail immediately due to max wait.
	err := limiter.Wait(context.Background())
	if err == nil {
		t.Error("Expected error due to max wait exceeded")
	}

	if !contains(err.Error(), "exceeds maximum") {
		t.Errorf("Expected max wait error, got %v", err)
	}
}

func TestSlidingWindowLimiter_Allow(t *testing.T) {
	logger := zaptest.NewLogger(t)

	tests := []struct {
		name       string
		windowSize time.Duration
		maxEvents  int
		events     []time.Duration
		wantAllow  []bool
	}{
		{
			name:       "within limit",
			windowSize: time.Second,
			maxEvents:  3,
			events:     []time.Duration{0, 100 * time.Millisecond, 200 * time.Millisecond},
			wantAllow:  []bool{true, true, true},
		},
		{
			name:       "exceed limit",
			windowSize: time.Second,
			maxEvents:  2,
			events:     []time.Duration{0, 100 * time.Millisecond, 200 * time.Millisecond},
			wantAllow:  []bool{true, true, false},
		},
		{
			name:       "events outside window",
			windowSize: 500 * time.Millisecond,
			maxEvents:  2,
			events:     []time.Duration{0, 100 * time.Millisecond, 600 * time.Millisecond, 700 * time.Millisecond},
			wantAllow:  []bool{true, true, true, true}, // First two fall out of window
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			limiter := NewSlidingWindowLimiter(tt.windowSize, tt.maxEvents, logger)
			ctx := context.Background()

			start := time.Now()
			for i, delay := range tt.events {
				// Wait until the specified time.
				time.Sleep(time.Until(start.Add(delay)))

				err := limiter.Allow(ctx)
				allowed := err == nil

				if allowed != tt.wantAllow[i] {
					t.Errorf("Event %d: allowed = %v, want %v", i, allowed, tt.wantAllow[i])
				}
			}
		})
	}
}

func TestSlidingWindowLimiter_Wait(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Window of 200ms, max 2 events.
	limiter := NewSlidingWindowLimiter(200*time.Millisecond, 2, logger)
	ctx := context.Background()

	// Fill the window.
	for i := 0; i < 2; i++ {
		if err := limiter.Allow(ctx); err != nil {
			t.Fatalf("Failed to fill window: %v", err)
		}
	}

	// Next request should wait.
	start := time.Now()

	if err := limiter.Wait(ctx); err != nil {
		t.Fatalf("Wait() error = %v", err)
	}

	elapsed := time.Since(start)

	// Should wait at least some time for window to slide.
	if elapsed < 10*time.Millisecond {
		t.Errorf("Wait time too short: %v", elapsed)
	}
}

func TestSlidingWindowLimiter_Cleanup(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Short window for quick cleanup.
	limiter := NewSlidingWindowLimiter(100*time.Millisecond, 1000, logger)
	ctx := context.Background()

	// Add many events.
	for i := 0; i < constants.TestConcurrentRoutines; i++ {
		_ = limiter.Allow(ctx)
	}

	// Check initial event count.
	limiter.mu.Lock()
	initialCount := len(limiter.events)
	limiter.mu.Unlock()

	if initialCount != 100 {
		t.Errorf("Expected 100 events, got %d", initialCount)
	}

	// Wait for window to pass and trigger cleanup.
	time.Sleep(150 * time.Millisecond)

	// Trigger cleanup by adding new event.
	_ = limiter.Allow(ctx)

	// Check cleaned up.
	limiter.mu.Lock()
	finalCount := len(limiter.events)
	limiter.mu.Unlock()

	// Should only have the recent event.
	if finalCount > 10 {
		t.Errorf("Expected cleanup, still have %d events", finalCount)
	}
}

func TestNoOpLimiter(t *testing.T) {
	limiter := &NoOpLimiter{}
	ctx := context.Background()

	// Should always allow.
	for i := 0; i < constants.TestMaxMessages; i++ {
		if err := limiter.Allow(ctx); err != nil {
			t.Errorf("NoOpLimiter.Allow() error = %v", err)
		}

		if err := limiter.Wait(ctx); err != nil {
			t.Errorf("NoOpLimiter.Wait() error = %v", err)
		}
	}
}

func TestNewRateLimiter(t *testing.T) {
	logger := zaptest.NewLogger(t)

	tests := []struct {
		name     string
		rate     float64
		burst    int
		wantType string
	}{
		{
			name:     "positive rate",
			rate:     10.0,
			burst:    5,
			wantType: "*ratelimit.TokenBucketLimiter",
		},
		{
			name:     "zero rate",
			rate:     0,
			burst:    5,
			wantType: "*ratelimit.NoOpLimiter",
		},
		{
			name:     "negative rate",
			rate:     -1.0,
			burst:    5,
			wantType: "*ratelimit.NoOpLimiter",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			limiter := NewRateLimiter(tt.rate, tt.burst, logger)

			gotType := fmt.Sprintf("%T", limiter)
			if gotType != tt.wantType {
				t.Errorf("NewRateLimiter() type = %v, want %v", gotType, tt.wantType)
			}
		})
	}
}

func TestTokenBucketLimiter_Concurrent(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// 100 requests per second, burst of 10
	limiter := NewTokenBucketLimiter(100.0, 10, time.Minute, logger)
	ctx := context.Background()

	// Launch concurrent requests.
	const numGoroutines = 20

	const requestsPerGoroutine = 5

	var allowed int32

	var wg sync.WaitGroup

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for j := 0; j < requestsPerGoroutine; j++ {
				if err := limiter.Allow(ctx); err == nil {
					atomic.AddInt32(&allowed, 1)
				}

				time.Sleep(time.Millisecond) // Small delay between requests
			}
		}()
	}

	wg.Wait()

	// Should allow burst + some refilled tokens.
	// With 100/sec rate and ~100ms total time, should get burst + ~10 more.
	minExpected := int32(10) // At least burst
	maxExpected := int32(30) // Burst + some refill

	if allowed < minExpected || allowed > maxExpected {
		t.Errorf("Allowed %d requests, expected between %d and %d", allowed, minExpected, maxExpected)
	}
}

func TestSlidingWindowLimiter_Concurrent(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// 1 second window, max 50 events
	limiter := NewSlidingWindowLimiter(time.Second, 50, logger)
	ctx := context.Background()

	// Launch concurrent requests.
	const numGoroutines = 10

	const requestsPerGoroutine = 10

	var allowed int32

	var wg sync.WaitGroup

	start := time.Now()

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)

		go func(id int) {
			defer wg.Done()

			for j := 0; j < requestsPerGoroutine; j++ {
				if err := limiter.Allow(ctx); err == nil {
					atomic.AddInt32(&allowed, 1)
				}
			}
		}(i)
	}

	wg.Wait()

	elapsed := time.Since(start)

	// Should allow exactly 50 within the window.
	if allowed != 50 {
		t.Errorf("Allowed %d requests, expected 50", allowed)
	}

	// Should complete quickly (not waiting for window).
	if elapsed > 100*time.Millisecond {
		t.Errorf("Took too long: %v", elapsed)
	}
}

func BenchmarkTokenBucketLimiter_Allow(b *testing.B) {
	logger := zaptest.NewLogger(b)
	limiter := NewTokenBucketLimiter(1000000.0, 1000000, time.Minute, logger)
	ctx := context.Background()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = limiter.Allow(ctx)
	}
}

func BenchmarkSlidingWindowLimiter_Allow(b *testing.B) {
	logger := zaptest.NewLogger(b)
	limiter := NewSlidingWindowLimiter(time.Minute, 1000000, logger)
	ctx := context.Background()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = limiter.Allow(ctx)
	}
}

func BenchmarkNoOpLimiter_Allow(b *testing.B) {
	limiter := &NoOpLimiter{}
	ctx := context.Background()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = limiter.Allow(ctx)
	}
}

func TestTokenBucketLimiter_EdgeCases(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tests := createTokenBucketEdgeCaseTests()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			limiter := NewTokenBucketLimiter(tt.rate, tt.burst, tt.maxWait, logger)
			ctx := context.Background()

			err := limiter.Allow(ctx)
			validateTokenBucketResult(t, tt, err)
		})
	}
}

func createTokenBucketEdgeCaseTests() []struct {
	name        string
	rate        float64
	burst       int
	maxWait     time.Duration
	expectError bool
	errorMsg    string
} {
	return []struct {
		name        string
		rate        float64
		burst       int
		maxWait     time.Duration
		expectError bool
		errorMsg    string
	}{
		{
			name:        "zero rate",
			rate:        0,
			burst:       5,
			maxWait:     time.Second,
			expectError: true,
			errorMsg:    "rate limit exceeded",
		},
		{
			name:        "negative rate",
			rate:        -1.0,
			burst:       5,
			maxWait:     time.Second,
			expectError: true,
			errorMsg:    "rate limit exceeded",
		},
		{
			name:    "very small rate",
			rate:    0.001, // 1 request per 1000 seconds
			burst:   1,
			maxWait: 10 * time.Millisecond,
		},
		{
			name:    "very large rate",
			rate:    1000000.0, // 1 million per second
			burst:   1000,
			maxWait: time.Second,
		},
		{
			name:    "zero burst",
			rate:    10.0,
			burst:   0,
			maxWait: time.Second,
		},
		{
			name:    "very large burst",
			rate:    10.0,
			burst:   1000000,
			maxWait: time.Second,
		},
	}
}

func validateTokenBucketResult(t *testing.T, tt struct {
	name        string
	rate        float64
	burst       int
	maxWait     time.Duration
	expectError bool
	errorMsg    string
}, err error) {
	t.Helper()

	if tt.expectError {
		if err == nil {
			t.Error("Expected error but got none")
		} else if tt.errorMsg != "" && !contains(err.Error(), tt.errorMsg) {
			t.Errorf("Expected error containing '%s', got '%s'", tt.errorMsg, err.Error())
		}
	} else if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
}

func TestTokenBucketLimiter_BurstRecovery(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Test burst capacity recovery over time.
	limiter := NewTokenBucketLimiter(2.0, 5, time.Minute, logger) // 2 per second, burst 5
	ctx := context.Background()

	// Use up all burst capacity.
	for i := 0; i < 5; i++ {
		if err := limiter.Allow(ctx); err != nil {
			t.Fatalf("Should allow burst capacity: %v", err)
		}
	}

	// Next request should fail.
	if err := limiter.Allow(ctx); err == nil {
		t.Error("Should reject after burst exhaustion")
	}

	// Wait for partial recovery (1 second = 2 tokens).
	time.Sleep(1 * time.Second)

	// Should allow 2 more requests.
	for i := 0; i < 2; i++ {
		if err := limiter.Allow(ctx); err != nil {
			t.Errorf("Should allow after recovery: %v", err)
		}
	}

	// Should reject the next one.
	if err := limiter.Allow(ctx); err == nil {
		t.Error("Should reject after using recovered tokens")
	}
}

func TestTokenBucketLimiter_PreciseRateControl(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Test precise rate control over time.
	rate := 5.0 // 5 requests per second
	limiter := NewTokenBucketLimiter(rate, 1, time.Minute, logger)
	ctx := context.Background()

	// Use the initial token.
	if err := limiter.Allow(ctx); err != nil {
		t.Fatalf("Should allow initial token: %v", err)
	}

	// Measure time for next 10 tokens.
	start := time.Now()
	allowed := 0

	for i := 0; i < constants.TestBatchSize; i++ {
		if err := limiter.Wait(ctx); err != nil {
			t.Errorf("Wait failed: %v", err)

			break
		}

		allowed++
	}

	elapsed := time.Since(start)
	expectedTime := time.Duration(float64(time.Second) * float64(allowed) / rate)
	tolerance := 100 * time.Millisecond

	if elapsed < expectedTime-tolerance || elapsed > expectedTime+tolerance {
		t.Errorf("Rate control imprecise: elapsed %v, expected ~%v", elapsed, expectedTime)
	}
}

type slidingWindowEdgeCase struct {
	name       string
	windowSize time.Duration
	maxEvents  int
	should     string
}

func getSlidingWindowEdgeCaseTests() []slidingWindowEdgeCase {
	return []slidingWindowEdgeCase{
		{
			name:       "zero window size",
			windowSize: 0,
			maxEvents:  5,
			should:     "handle gracefully",
		},
		{
			name:       "negative window size",
			windowSize: -1 * time.Second,
			maxEvents:  5,
			should:     "handle gracefully",
		},
		{
			name:       "very small window",
			windowSize: 1 * time.Microsecond,
			maxEvents:  5,
			should:     "work correctly",
		},
		{
			name:       "very large window",
			windowSize: 24 * time.Hour,
			maxEvents:  5,
			should:     "work correctly",
		},
		{
			name:       "zero max events",
			windowSize: time.Second,
			maxEvents:  0,
			should:     "reject all",
		},
		{
			name:       "negative max events",
			windowSize: time.Second,
			maxEvents:  -1,
			should:     "reject all",
		},
	}
}

func testSlidingWindowEdgeCase(t *testing.T, tt slidingWindowEdgeCase, logger *zap.Logger) {
	t.Helper()
	limiter := NewSlidingWindowLimiter(tt.windowSize, tt.maxEvents, logger)
	ctx := context.Background()

	// Test behavior.
	err := limiter.Allow(ctx)

	switch tt.should {
	case "reject all":
		if err == nil {
			t.Error("Should reject when max events <= 0")
		}
	case "work correctly":
		if tt.maxEvents > 0 && err != nil {
			t.Errorf("Should allow first request: %v", err)
		}
	case "handle gracefully":
		// Should not panic or crash.
		t.Logf("Handled edge case gracefully: %v", err)
	}
}

func TestSlidingWindowLimiter_EdgeCases(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tests := getSlidingWindowEdgeCaseTests()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testSlidingWindowEdgeCase(t, tt, logger)
		})
	}
}

func TestSlidingWindowLimiter_MemoryCleanup(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Test that old events are cleaned up to prevent memory leaks.
	limiter := NewSlidingWindowLimiter(100*time.Millisecond, 1000, logger)
	ctx := context.Background()

	// Add many events.
	for i := 0; i < 500; i++ {
		_ = limiter.Allow(ctx)
	}

	// Check event count.
	limiter.mu.Lock()
	initialCount := len(limiter.events)
	limiter.mu.Unlock()

	if initialCount != 500 {
		t.Errorf("Expected 500 events, got %d", initialCount)
	}

	// Wait for window to expire.
	time.Sleep(150 * time.Millisecond)

	// Add new event to trigger cleanup.
	_ = limiter.Allow(ctx)

	// Check that old events were cleaned up.
	limiter.mu.Lock()
	finalCount := len(limiter.events)
	limiter.mu.Unlock()

	if finalCount > 10 { // Should only have recent events
		t.Errorf("Memory cleanup failed: still have %d events", finalCount)
	}
}

type highLoadTest struct {
	name    string
	limiter RateLimiter
}

func getHighLoadTests(logger *zap.Logger) []highLoadTest {
	return []highLoadTest{
		{
			name:    "TokenBucket under load",
			limiter: NewTokenBucketLimiter(100.0, 10, time.Second, logger),
		},
		{
			name:    "SlidingWindow under load",
			limiter: NewSlidingWindowLimiter(time.Second, 100, logger),
		},
		{
			name:    "NoOp under load",
			limiter: &NoOpLimiter{},
		},
	}
}

func testRateLimiterUnderHighLoad(t *testing.T, tt highLoadTest) {
	t.Helper()
	ctx := context.Background()

	const (
		numGoroutines        = 50
		requestsPerGoroutine = 20
	)

	allowed := runHighLoadTest(ctx, tt.limiter, numGoroutines, requestsPerGoroutine)
	
	t.Logf("%s: %d allowed out of %d total",
		tt.name, allowed, numGoroutines*requestsPerGoroutine)

	verifyHighLoadResults(t, tt.limiter, allowed, numGoroutines*requestsPerGoroutine)
}

func runHighLoadTest(ctx context.Context, limiter RateLimiter, numGoroutines, requestsPerGoroutine int) int32 {
	var (
		allowed int32
		wg      sync.WaitGroup
	)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < requestsPerGoroutine; j++ {
				if err := limiter.Allow(ctx); err == nil {
					atomic.AddInt32(&allowed, 1)
				}
			}
		}()
	}

	wg.Wait()

	return allowed
}

func verifyHighLoadResults(t *testing.T, limiter RateLimiter, allowed int32, totalRequests int) {
	t.Helper()
	if _, isNoOp := limiter.(*NoOpLimiter); !isNoOp {
		// #nosec G115 - test code comparing request counts
		if allowed >= int32(totalRequests) {
			t.Errorf("Rate limiter should have limited some requests")
		}
	} else {
		// #nosec G115 - test code comparing request counts  
		if allowed != int32(totalRequests) {
			t.Errorf("NoOp limiter should allow all requests")
		}
	}
}

func TestRateLimiter_UnderHighLoad(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tests := getHighLoadTests(logger)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testRateLimiterUnderHighLoad(t, tt)
		})
	}
}

func TestRateLimiter_ContextCancellation(t *testing.T) {
	logger := zaptest.NewLogger(t)

	tests := []struct {
		name    string
		limiter RateLimiter
	}{
		{
			name:    "TokenBucket context cancellation",
			limiter: NewTokenBucketLimiter(0.1, 1, time.Minute, logger), // Very slow
		},
		{
			name:    "SlidingWindow context cancellation",
			limiter: NewSlidingWindowLimiter(time.Minute, 1, logger), // Very restrictive with long window
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use up capacity.
			_ = tt.limiter.Allow(context.Background())

			// Create cancellable context.
			ctx, cancel := context.WithCancel(context.Background())

			// Start waiting.
			done := make(chan error, 1)

			go func() {
				done <- tt.limiter.Wait(ctx)
			}()

			// Cancel after short delay.
			time.Sleep(constants.TestLongTickInterval)
			cancel()

			// Should return with context error.
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Errorf("Expected context.Canceled, got %v", err)
				}
			case <-time.After(time.Second):
				t.Error("Wait did not return after context cancellation")
			}
		})
	}
}

func TestTokenBucketLimiter_ClockSkew(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Test behavior with simulated clock skew.
	limiter := NewTokenBucketLimiter(10.0, 5, time.Minute, logger)
	ctx := context.Background()

	// Use up burst.
	for i := 0; i < 5; i++ {
		_ = limiter.Allow(ctx)
	}

	// Manually adjust last time to simulate clock skew.
	limiter.mu.Lock()
	limiter.lastTime = time.Now().Add(-10 * time.Second) // Simulate backward clock jump
	limiter.mu.Unlock()

	// Should handle gracefully and refill tokens.
	err := limiter.Allow(ctx)
	if err != nil {
		t.Errorf("Should handle clock skew gracefully: %v", err)
	}
}

func TestSlidingWindowLimiter_WindowBoundaryBehavior(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Test precise behavior at window boundaries.
	windowSize := 100 * time.Millisecond
	limiter := NewSlidingWindowLimiter(windowSize, 2, logger)
	ctx := context.Background()

	// Fill window.
	start := time.Now()
	err1 := limiter.Allow(ctx)

	time.Sleep(constants.TestLongTickInterval)

	err2 := limiter.Allow(ctx)

	if err1 != nil || err2 != nil {
		t.Errorf("Should allow within window: %v, %v", err1, err2)
	}

	// Should reject immediately.
	err3 := limiter.Allow(ctx)
	if err3 == nil {
		t.Error("Should reject when window is full")
	}

	// Wait for first event to expire.
	time.Sleep(60 * time.Millisecond) // Total 110ms from start

	// Should allow again.
	err4 := limiter.Allow(ctx)
	if err4 != nil {
		t.Errorf("Should allow after first event expires: %v", err4)
	}

	elapsed := time.Since(start)
	t.Logf("Window boundary test completed in %v", elapsed)
}

func BenchmarkTokenBucketLimiter_HighRate(b *testing.B) {
	logger := zaptest.NewLogger(b)
	limiter := NewTokenBucketLimiter(1000000.0, 1000000, time.Minute, logger)
	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = limiter.Allow(ctx)
		}
	})
}

func BenchmarkSlidingWindowLimiter_HighRate(b *testing.B) {
	logger := zaptest.NewLogger(b)
	limiter := NewSlidingWindowLimiter(time.Minute, 1000000, logger)
	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = limiter.Allow(ctx)
		}
	})
}

func BenchmarkRateLimiter_Comparison(b *testing.B) {
	logger := zaptest.NewLogger(b)
	ctx := context.Background()

	benchmarks := []struct {
		name    string
		limiter RateLimiter
	}{
		{
			name:    "TokenBucket",
			limiter: NewTokenBucketLimiter(1000.0, 1000, time.Minute, logger),
		},
		{
			name:    "SlidingWindow",
			limiter: NewSlidingWindowLimiter(time.Second, 1000, logger),
		},
		{
			name:    "NoOp",
			limiter: &NoOpLimiter{},
		},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				_ = bm.limiter.Allow(ctx)
			}
		})
	}
}

// Helper function.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		(len(s) > len(substr) && (s[:len(substr)] == substr ||
			s[len(s)-len(substr):] == substr ||
			(len(substr) < len(s) && findSubstring(s, substr)))))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}

	return false
}
