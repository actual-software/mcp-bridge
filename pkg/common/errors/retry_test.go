package errors

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

func TestRetryConfig(t *testing.T) {
	t.Parallel()

	config := DefaultRetryConfig()

	if config.MaxAttempts != 3 {
		t.Errorf("Expected MaxAttempts 3, got %d", config.MaxAttempts)
	}

	if config.InitialInterval != time.Second {
		t.Errorf("Expected InitialInterval 1s, got %v", config.InitialInterval)
	}

	if config.MaxInterval != 30*time.Second {
		t.Errorf("Expected MaxInterval 30s, got %v", config.MaxInterval)
	}

	if config.Multiplier != 2.0 {
		t.Errorf("Expected Multiplier 2.0, got %f", config.Multiplier)
	}
}

func TestExponentialBackoffPolicy(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)
	config := RetryConfig{
		MaxAttempts:     3,
		InitialInterval: 100 * time.Millisecond,
		MaxInterval:     1 * time.Second,
		Multiplier:      2.0,
		RandomizeFactor: 0.0, // No jitter for predictable testing
	}

	policy := NewExponentialBackoffPolicy(config, logger)

	t.Run("Should retry retryable errors", func(t *testing.T) { testShouldRetryRetryableErrors(t, policy) })
	t.Run("Should not retry non-retryable errors", func(t *testing.T) { testShouldNotRetryNonRetryableErrors(t, policy) })
	t.Run("Exponential backoff intervals", func(t *testing.T) { testExponentialBackoffIntervals(t, policy) })
	t.Run("Max interval cap", func(t *testing.T) { testMaxIntervalCap(t, policy, config) })
}

func testShouldRetryRetryableErrors(t *testing.T, policy *ExponentialBackoffPolicy) {
	t.Helper()
	t.Parallel()

	err := Error(GTW_CONN_TIMEOUT)

	if !policy.ShouldRetry(err, 1) {
		t.Error("Should retry on first attempt for retryable error")
	}

	if !policy.ShouldRetry(err, 2) {
		t.Error("Should retry on second attempt")
	}

	if policy.ShouldRetry(err, 3) {
		t.Error("Should not retry after max attempts reached")
	}
}

func testShouldNotRetryNonRetryableErrors(t *testing.T, policy *ExponentialBackoffPolicy) {
	t.Helper()
	t.Parallel()

	err := Error(GTW_AUTH_MISSING)

	if policy.ShouldRetry(err, 1) {
		t.Error("Should not retry non-retryable error")
	}
}

func testExponentialBackoffIntervals(t *testing.T, policy *ExponentialBackoffPolicy) {
	t.Helper()
	t.Parallel()

	interval1 := policy.NextInterval(1)
	interval2 := policy.NextInterval(2)
	interval3 := policy.NextInterval(3)

	if interval1 != 100*time.Millisecond {
		t.Errorf("Expected interval1 100ms, got %v", interval1)
	}

	if interval2 != 200*time.Millisecond {
		t.Errorf("Expected interval2 200ms, got %v", interval2)
	}

	if interval3 != 400*time.Millisecond {
		t.Errorf("Expected interval3 400ms, got %v", interval3)
	}
}

func testMaxIntervalCap(t *testing.T, policy *ExponentialBackoffPolicy, config RetryConfig) {
	t.Helper()
	t.Parallel()

	interval := policy.NextInterval(10) // Should be capped

	if interval > config.MaxInterval {
		t.Errorf("Interval %v exceeds max %v", interval, config.MaxInterval)
	}
}

// Helper function to create a test operation that succeeds immediately.
func createSuccessOperation(attempts *int) func(context.Context) error {
	return func(_ context.Context) error {
		*attempts++

		return nil
	}
}

// Helper function to create a test operation that succeeds after specified attempts.
func createSuccessAfterAttemptsOperation(attempts *int, successAfter int) func(context.Context) error {
	return func(_ context.Context) error {
		*attempts++
		if *attempts < successAfter {
			return Error(GTW_CONN_TIMEOUT, "Temporary failure")
		}

		return nil
	}
}

// Helper function to create a test operation that always fails with retryable error.
func createAlwaysFailOperation(attempts *int) func(context.Context) error {
	return func(_ context.Context) error {
		*attempts++

		return Error(GTW_CONN_TIMEOUT, "Persistent failure")
	}
}

// Helper function to create a test operation that fails with non-retryable error.
func createNonRetryableFailOperation(attempts *int) func(context.Context) error {
	return func(_ context.Context) error {
		*attempts++

		return Error(GTW_AUTH_MISSING, "Authentication required")
	}
}

// Helper function to create a test operation for context cancellation tests.
func createContextCancelOperation(attempts *int, cancel context.CancelFunc) func(context.Context) error {
	return func(_ context.Context) error {
		*attempts++
		if *attempts == 1 {
			// Cancel context during first retry delay
			go func() {
				time.Sleep(5 * time.Millisecond)
				cancel()
			}()

			return Error(GTW_CONN_TIMEOUT, "Failure that would be retried")
		}

		return nil
	}
}

// Helper function to create a test operation that respects context timeout.
func createTimeoutAwareOperation() func(context.Context) error {
	return func(operationCtx context.Context) error {
		// Simulate work that takes longer than context timeout
		for range 10 {
			time.Sleep(2 * time.Millisecond)
			// Check if context was canceled
			select {
			case <-operationCtx.Done():
				return Error(CMN_INT_CONTEXT_CANC, operationCtx.Err().Error())
			default:
			}
		}

		return nil
	}
}

// Helper function to validate expected execution results.
func validateExecutionResult(t *testing.T, err error, expectedError bool, expectedAttempts, actualAttempts int) {
	t.Helper()

	if expectedError && err == nil {
		t.Error("Expected error but got none")
	}

	if !expectedError && err != nil {
		t.Errorf("Expected success, got error: %v", err)
	}

	if actualAttempts != expectedAttempts {
		t.Errorf("Expected %d attempts, got %d", expectedAttempts, actualAttempts)
	}
}

// Helper function to validate minimum duration for retry tests.
func validateMinimumDuration(t *testing.T, duration, minExpected time.Duration) {
	t.Helper()

	if duration < minExpected {
		t.Errorf("Expected at least %v duration, got %v", minExpected, duration)
	}
}

// Helper function to validate error codes.
func validateErrorCode(t *testing.T, err error, expectedCode ErrorCode) {
	t.Helper()

	if !IsErrorCode(err, expectedCode) {
		t.Errorf("Expected error code %v, got %v", expectedCode, GetErrorCode(err))
	}
}

func TestRetryManager(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)
	config := RetryConfig{
		MaxAttempts:     3,
		InitialInterval: 10 * time.Millisecond, // Fast retries for testing
		MaxInterval:     100 * time.Millisecond,
		Multiplier:      2.0,
		RandomizeFactor: 0.0,
	}

	policy := NewExponentialBackoffPolicy(config, logger)
	manager := NewRetryManager(policy, logger)

	t.Run("Success on first attempt", func(t *testing.T) { testSuccessOnFirstAttempt(t, manager) })
	t.Run("Success after retries", func(t *testing.T) { testSuccessAfterRetries(t, manager) })
	t.Run("Failure after max attempts", func(t *testing.T) { testFailureAfterMaxAttempts(t, manager) })
	t.Run("Non-retryable error", func(t *testing.T) { testNonRetryableError(t, manager) })
	t.Run("Context cancellation", func(t *testing.T) { testContextCancellation(t, manager) })
	t.Run("Context timeout during operation", func(t *testing.T) { testContextTimeoutDuringOperation(t, manager) })
}

func testSuccessOnFirstAttempt(t *testing.T, manager *RetryManager) {
	t.Helper()
	t.Parallel()

	attempts := 0
	operation := createSuccessOperation(&attempts)

	err := manager.Execute(context.Background(), operation)
	validateExecutionResult(t, err, false, 1, attempts)
}

func testSuccessAfterRetries(t *testing.T, manager *RetryManager) {
	t.Helper()
	t.Parallel()

	attempts := 0
	operation := createSuccessAfterAttemptsOperation(&attempts, 3)

	start := time.Now()
	err := manager.Execute(context.Background(), operation)
	duration := time.Since(start)

	validateExecutionResult(t, err, false, 3, attempts)

	// Should have taken some time due to retries
	minExpectedDuration := 10*time.Millisecond + 20*time.Millisecond // First + second retry delays
	validateMinimumDuration(t, duration, minExpectedDuration)
}

func testFailureAfterMaxAttempts(t *testing.T, manager *RetryManager) {
	t.Helper()
	t.Parallel()

	attempts := 0
	operation := createAlwaysFailOperation(&attempts)

	err := manager.Execute(context.Background(), operation)
	validateExecutionResult(t, err, true, 3, attempts)
	validateErrorCode(t, err, GTW_CONN_TIMEOUT)
}

func testNonRetryableError(t *testing.T, manager *RetryManager) {
	t.Helper()
	t.Parallel()

	attempts := 0
	operation := createNonRetryableFailOperation(&attempts)

	err := manager.Execute(context.Background(), operation)
	validateExecutionResult(t, err, true, 1, attempts)
}

func testContextCancellation(t *testing.T, manager *RetryManager) {
	t.Helper()
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	attempts := 0
	operation := createContextCancelOperation(&attempts, cancel)

	err := manager.Execute(ctx, operation)
	validateExecutionResult(t, err, true, -1, -1) // Don't validate attempts for this test
	validateErrorCode(t, err, CMN_INT_CONTEXT_CANC)
}

func testContextTimeoutDuringOperation(t *testing.T, manager *RetryManager) {
	t.Helper()
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)

	defer cancel()

	operation := createTimeoutAwareOperation()

	err := manager.Execute(ctx, operation)
	validateExecutionResult(t, err, true, -1, -1) // Don't validate attempts for this test
	validateErrorCode(t, err, CMN_INT_CONTEXT_CANC)
}

func TestCircuitBreakerPolicy(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)
	config := RetryConfig{
		MaxAttempts:     3,                      // Failure threshold
		InitialInterval: 10 * time.Millisecond,  // Initial retry interval
		MaxInterval:     100 * time.Millisecond, // Recovery timeout
		Multiplier:      2.0,                    // Exponential backoff multiplier
		RandomizeFactor: 0.1,                    // Add randomization to avoid thundering herd
	}

	policy := NewCircuitBreakerPolicy(config, logger)

	t.Run("Circuit closed initially", func(t *testing.T) { testCircuitClosedInitially(t, policy) })
	t.Run("Circuit opens after failures", func(t *testing.T) { testCircuitOpensAfterFailures(t, config, logger) })
	t.Run("Circuit transitions to half-open", func(t *testing.T) { testCircuitTransitionsToHalfOpen(t, config, logger) })
	t.Run("Circuit closes after success", func(t *testing.T) { testCircuitClosesAfterSuccess(t, config, logger) })
	t.Run("Circuit reopens after failure in half-open", func(t *testing.T) {
		testCircuitReopensAfterFailureInHalfOpen(t, config, logger)
	})
	t.Run("Non-retryable errors don't affect circuit", func(t *testing.T) {
		testNonRetryableErrorsDoNotAffectCircuit(t, config, logger)
	})
}

func testCircuitClosedInitially(t *testing.T, policy *CircuitBreakerPolicy) {
	t.Helper()
	t.Parallel()

	err := Error(GTW_CONN_TIMEOUT)

	if !policy.ShouldRetry(err, 1) {
		t.Error("Should retry when circuit is closed")
	}
}

func testCircuitOpensAfterFailures(t *testing.T, config RetryConfig, logger *zap.Logger) {
	t.Helper()

	policy := NewCircuitBreakerPolicy(config, logger)
	err := Error(GTW_CONN_TIMEOUT)

	// First few failures should be retried
	policy.ShouldRetry(err, 1)
	policy.ShouldRetry(err, 1)

	// After threshold, circuit should open
	if policy.ShouldRetry(err, 1) {
		t.Error("Circuit should be open after failure threshold")
	}
}

func testCircuitTransitionsToHalfOpen(t *testing.T, config RetryConfig, logger *zap.Logger) {
	t.Helper()

	policy := NewCircuitBreakerPolicy(config, logger)
	err := Error(GTW_CONN_TIMEOUT)

	// Trigger circuit open
	policy.ShouldRetry(err, 1)
	policy.ShouldRetry(err, 1)
	policy.ShouldRetry(err, 1) // Opens circuit

	// Wait for recovery timeout
	time.Sleep(config.MaxInterval + 10*time.Millisecond)

	// Should allow one retry in half-open state
	if !policy.ShouldRetry(err, 1) {
		t.Error("Should allow retry in half-open state")
	}
}

func testCircuitClosesAfterSuccess(t *testing.T, config RetryConfig, logger *zap.Logger) {
	t.Helper()

	policy := NewCircuitBreakerPolicy(config, logger)

	// Record success
	policy.RecordSuccess()

	// Should continue to allow retries
	err := Error(GTW_CONN_TIMEOUT)
	if !policy.ShouldRetry(err, 1) {
		t.Error("Should retry after recording success")
	}
}

func testCircuitReopensAfterFailureInHalfOpen(t *testing.T, config RetryConfig, logger *zap.Logger) {
	t.Helper()

	policy := NewCircuitBreakerPolicy(config, logger)
	err := Error(GTW_CONN_TIMEOUT)

	// Open circuit
	policy.ShouldRetry(err, 1)
	policy.ShouldRetry(err, 1)
	policy.ShouldRetry(err, 1)

	// Wait for half-open
	time.Sleep(config.MaxInterval + 10*time.Millisecond)
	policy.ShouldRetry(err, 1) // Half-open

	// Record failure
	policy.RecordFailure()

	// Should not allow retries (circuit open again)
	if policy.ShouldRetry(err, 1) {
		t.Error("Circuit should be open after failure in half-open state")
	}
}

func testNonRetryableErrorsDoNotAffectCircuit(t *testing.T, config RetryConfig, logger *zap.Logger) {
	t.Helper()

	policy := NewCircuitBreakerPolicy(config, logger)
	err := Error(GTW_AUTH_MISSING)

	// Non-retryable error should not trigger circuit
	if policy.ShouldRetry(err, 1) {
		t.Error("Should not retry non-retryable error")
	}

	// Circuit should still be closed for retryable errors
	retryableErr := Error(GTW_CONN_TIMEOUT)
	if !policy.ShouldRetry(retryableErr, 1) {
		t.Error("Circuit should still be closed for retryable errors")
	}
}

func BenchmarkRetryManager(b *testing.B) {
	logger := zaptest.NewLogger(b)
	config := DefaultRetryConfig()
	policy := NewExponentialBackoffPolicy(config, logger)
	manager := NewRetryManager(policy, logger)

	b.Run("Success on first attempt", func(b *testing.B) {
		for range b.N {
			operation := func(_ context.Context) error {
				return nil
			}
			_ = manager.Execute(context.Background(), operation)
		}
	})

	b.Run("Non-retryable error", func(b *testing.B) {
		for range b.N {
			operation := func(_ context.Context) error {
				return Error(GTW_AUTH_MISSING)
			}
			_ = manager.Execute(context.Background(), operation)
		}
	})
}

// BenchmarkSecureRandom benchmarks the secureRandom function.
func BenchmarkSecureRandom(b *testing.B) {
	for range b.N {
		secureRandom()
	}
}

// BenchmarkCircuitBreakerPolicy benchmarks circuit breaker operations.
func BenchmarkCircuitBreakerPolicy(b *testing.B) {
	logger := zaptest.NewLogger(b)
	config := DefaultRetryConfig()
	policy := NewCircuitBreakerPolicy(config, logger)
	err := Error(GTW_CONN_TIMEOUT)

	b.Run("ShouldRetry", func(b *testing.B) {
		for range b.N {
			policy.ShouldRetry(err, 1)
		}
	})

	b.Run("NextInterval", func(b *testing.B) {
		for i := range b.N {
			policy.NextInterval(i%10 + 1)
		}
	})

	b.Run("RecordSuccess", func(b *testing.B) {
		for range b.N {
			policy.RecordSuccess()
		}
	})
}

func ExampleError() {
	// Creating basic errors
	err1 := Error(GTW_AUTH_MISSING)
	fmt.Printf("Error: %s\n", err1.Error())

	// Creating errors with details
	err2 := Error(GTW_CONN_TIMEOUT, "Connection to backend failed")
	fmt.Printf("Error with details: %s\n", err2.Error())

	// Checking error properties
	fmt.Printf("Is retryable: %v\n", IsRetryable(err2))
	fmt.Printf("HTTP Status: %d\n", GetHTTPStatus(err2))

	// Output:
	// Error: [GTW_AUTH_001] Missing authentication credentials
	// Error with details: [GTW_CONN_002] Connection timeout: Connection to backend failed
	// Is retryable: true
	// HTTP Status: 504
}

// TestSecureRandom tests the secureRandom function.
func TestSecureRandom(t *testing.T) {
	t.Parallel()

	// Test that secureRandom returns values between 0 and 1
	for range 100 {
		value := secureRandom()
		assert.GreaterOrEqual(t, value, 0.0, "secureRandom should return value >= 0")
		assert.LessOrEqual(t, value, 1.0, "secureRandom should return value <= 1")
	}

	// Test that secureRandom returns different values
	values := make(map[float64]bool)

	for range 100 {
		value := secureRandom()
		values[value] = true
	}

	// Should have many different values (not exact due to floating point)
	assert.Greater(t, len(values), 50, "secureRandom should generate diverse values")
}

// TestExponentialBackoffPolicy_JitterHandling tests jitter in NextInterval.
func TestExponentialBackoffPolicy_JitterHandling(t *testing.T) {
	t.Parallel()

	logger := zaptest.NewLogger(t)
	config := RetryConfig{
		MaxAttempts:     5,
		InitialInterval: 100 * time.Millisecond,
		MaxInterval:     1 * time.Second,
		Multiplier:      2.0,
		RandomizeFactor: 0.3, // 30% jitter
	}

	policy := NewExponentialBackoffPolicy(config, logger)

	// Test jitter application
	t.Run("jitter applies randomization", func(t *testing.T) {
		t.Parallel()

		intervals := make([]time.Duration, 20)
		for i := range 20 {
			intervals[i] = policy.NextInterval(2) // Second attempt
		}

		// Should have variation due to jitter
		uniqueIntervals := make(map[time.Duration]bool)
		for _, interval := range intervals {
			uniqueIntervals[interval] = true
		}

		// With 30% jitter, we should see some variation
		assert.Greater(t, len(uniqueIntervals), 1, "Jitter should create interval variation")

		// All intervals should be within expected range
		baseInterval := 200 * time.Millisecond // 100ms * 2^(2-1)
		minExpected := time.Duration(float64(baseInterval) * (1 - config.RandomizeFactor))
		maxExpected := time.Duration(float64(baseInterval) * (1 + config.RandomizeFactor))

		for _, interval := range intervals {
			assert.GreaterOrEqual(t, interval, minExpected, "Interval should be >= min expected")
			assert.LessOrEqual(t, interval, maxExpected, "Interval should be <= max expected")
		}
	})

	// Test zero jitter
	t.Run("zero jitter produces consistent intervals", func(t *testing.T) {
		zeroJitterConfig := config
		zeroJitterConfig.RandomizeFactor = 0
		zeroJitterPolicy := NewExponentialBackoffPolicy(zeroJitterConfig, logger)

		interval1 := zeroJitterPolicy.NextInterval(1)
		interval2 := zeroJitterPolicy.NextInterval(1)
		interval3 := zeroJitterPolicy.NextInterval(1)

		assert.Equal(t, interval1, interval2)
		assert.Equal(t, interval2, interval3)
		assert.Equal(t, config.InitialInterval, interval1)
	})

	// Test max interval capping with jitter
	t.Run("max interval capping with jitter", func(t *testing.T) {
		// Test very high attempt number
		interval := policy.NextInterval(10)

		// Should not exceed max interval even with jitter
		maxPossible := time.Duration(float64(config.MaxInterval) * (1 + config.RandomizeFactor))
		assert.LessOrEqual(t, interval, maxPossible, "Interval should not exceed max with jitter")
	})
}

// TestCircuitBreakerPolicy_NextInterval tests the NextInterval method for circuit breaker.
func TestCircuitBreakerPolicy_NextInterval(t *testing.T) {
	t.Parallel()

	logger := zaptest.NewLogger(t)
	config := RetryConfig{
		MaxAttempts:     3,
		InitialInterval: 100 * time.Millisecond,
		MaxInterval:     1 * time.Second,
		Multiplier:      2.0,
		RandomizeFactor: 0.1,
	}

	policy := NewCircuitBreakerPolicy(config, logger)

	// Test intervals in closed state
	t.Run("closed state intervals", func(t *testing.T) {
		t.Parallel()

		interval1 := policy.NextInterval(1)
		interval2 := policy.NextInterval(2)
		interval3 := policy.NextInterval(3)

		// Should follow exponential backoff
		assert.Equal(t, 100*time.Millisecond, interval1)
		assert.Equal(t, 200*time.Millisecond, interval2)
		assert.Equal(t, 400*time.Millisecond, interval3)
	})

	// Test intervals in half-open state
	t.Run("half-open state intervals", func(t *testing.T) {
		t.Parallel()
		// Force circuit to half-open state by opening it first
		err := Error(GTW_CONN_TIMEOUT)
		policy.ShouldRetry(err, 1)
		policy.ShouldRetry(err, 1)
		policy.ShouldRetry(err, 1) // Opens circuit

		// Wait for transition to half-open
		time.Sleep(config.MaxInterval + 10*time.Millisecond)
		policy.ShouldRetry(err, 1) // Should transition to half-open

		// In half-open state, should use initial interval
		interval := policy.NextInterval(1)
		assert.Equal(t, config.InitialInterval, interval)
	})
}

// TestCircuitBreakerPolicy_RecordSuccess tests all branches of RecordSuccess.
func TestCircuitBreakerPolicy_RecordSuccess(t *testing.T) {
	t.Parallel()

	logger := zaptest.NewLogger(t)
	config := RetryConfig{
		MaxAttempts:     3,
		InitialInterval: 50 * time.Millisecond,
		MaxInterval:     100 * time.Millisecond,
		Multiplier:      2.0,
		RandomizeFactor: 0.1,
	}

	// Test RecordSuccess in closed state
	t.Run("record success in closed state", func(t *testing.T) {
		t.Parallel()

		policy := NewCircuitBreakerPolicy(config, logger)

		// Initially in closed state, record some failures
		err := Error(GTW_CONN_TIMEOUT)
		policy.ShouldRetry(err, 1) // Adds to failure count

		// Record multiple successes to trigger failure count reset
		for range config.MaxAttempts + 1 {
			policy.RecordSuccess()
		}

		// Should still allow retries (circuit should remain closed)
		assert.True(t, policy.ShouldRetry(err, 1), "Circuit should remain closed after success")
	})

	// Test RecordSuccess in half-open state transitions to closed
	t.Run("record success in half-open transitions to closed", func(t *testing.T) {
		t.Parallel()

		policy := NewCircuitBreakerPolicy(config, logger)
		err := Error(GTW_CONN_TIMEOUT)

		// Open the circuit
		policy.ShouldRetry(err, 1)
		policy.ShouldRetry(err, 1)
		policy.ShouldRetry(err, 1) // Opens circuit

		// Wait for half-open transition
		time.Sleep(config.MaxInterval + 10*time.Millisecond)
		policy.ShouldRetry(err, 1) // Transitions to half-open

		// Record success should close the circuit
		policy.RecordSuccess()

		// Should now allow retries (circuit closed)
		assert.True(t, policy.ShouldRetry(err, 1), "Circuit should be closed after success in half-open")
	})

	// Test RecordSuccess in open state (should have no effect)
	t.Run("record success in open state", func(t *testing.T) {
		policy := NewCircuitBreakerPolicy(config, logger)
		err := Error(GTW_CONN_TIMEOUT)

		// Open the circuit
		policy.ShouldRetry(err, 1)
		policy.ShouldRetry(err, 1)
		policy.ShouldRetry(err, 1) // Opens circuit

		// Record success in open state
		policy.RecordSuccess()

		// Should still not allow retries (circuit remains open)
		assert.False(t, policy.ShouldRetry(err, 1), "Circuit should remain open")
	})
}

// TestCircuitBreakerPolicy_StateTransitions tests comprehensive state transitions.
func TestCircuitBreakerPolicy_StateTransitions(t *testing.T) {
	// Test comprehensive state machine behavior
	logger := zaptest.NewLogger(t)
	config := RetryConfig{
		MaxAttempts:     2, // Lower threshold for faster testing
		InitialInterval: 10 * time.Millisecond,
		MaxInterval:     50 * time.Millisecond,
		Multiplier:      2.0,
		RandomizeFactor: 0.0, // No jitter for predictable testing
	}

	policy := NewCircuitBreakerPolicy(config, logger)
	err := Error(GTW_CONN_TIMEOUT)

	// Test: Closed -> Open -> Half-Open -> Closed cycle
	t.Run("full state transition cycle", func(t *testing.T) {
		// Start in closed state
		assert.True(t, policy.ShouldRetry(err, 1), "Should start in closed state")
		assert.False(t, policy.ShouldRetry(err, 1), "Should open after threshold (MaxAttempts=2)")

		// Now in open state
		assert.False(t, policy.ShouldRetry(err, 1), "Should remain open")

		// Wait for half-open transition
		time.Sleep(config.MaxInterval + 10*time.Millisecond)
		assert.True(t, policy.ShouldRetry(err, 1), "Should transition to half-open")

		// Record success to close circuit
		policy.RecordSuccess()
		assert.True(t, policy.ShouldRetry(err, 1), "Should be closed after success")
	})

	// Test: Half-Open -> Open on failure
	t.Run("half-open to open on failure", func(t *testing.T) {
		policy2 := NewCircuitBreakerPolicy(config, logger)

		// Open the circuit
		policy2.ShouldRetry(err, 1)
		policy2.ShouldRetry(err, 1) // Opens

		// Wait for half-open
		time.Sleep(config.MaxInterval + 10*time.Millisecond)
		policy2.ShouldRetry(err, 1) // Half-open

		// Record failure should reopen
		policy2.RecordFailure()
		assert.False(t, policy2.ShouldRetry(err, 1), "Should reopen after failure in half-open")
	})
}

// TestRetryWithCircuitBreaker tests retry manager with circuit breaker.
func TestRetryWithCircuitBreaker(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := RetryConfig{
		MaxAttempts:     2,
		InitialInterval: 10 * time.Millisecond,
		MaxInterval:     50 * time.Millisecond,
		Multiplier:      2.0,
		RandomizeFactor: 0.0,
	}

	policy := NewCircuitBreakerPolicy(config, logger)
	manager := NewRetryManager(policy, logger)

	t.Run("circuit breaker prevents excessive retries", func(t *testing.T) {
		attempts := 0
		operation := func(_ context.Context) error {
			attempts++

			return Error(GTW_CONN_TIMEOUT, "Connection failed")
		}

		err := manager.Execute(context.Background(), operation)
		require.Error(t, err)
		// Circuit breaker should limit attempts
		assert.LessOrEqual(t, attempts, config.MaxAttempts*2, "Circuit breaker should limit attempts")
	})
}
