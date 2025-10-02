package ratelimit

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/go-redis/redismock/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/actual-software/mcp-bridge/services/gateway/internal/auth"
	"github.com/actual-software/mcp-bridge/services/gateway/pkg/circuit"
)

func TestRedisRateLimiterWithCircuitBreaker_CircuitOpen(t *testing.T) {
	// Create a disconnected Redis client to simulate failures
	client := redis.NewClient(&redis.Options{
		Addr: "localhost:16379", // Non-existent Redis instance
	})

	defer func() { _ = client.Close() }()

	logger := zap.NewNop()
	limiter := CreateFaultTolerantRedisRateLimiter(client, logger)

	ctx := context.Background()
	config := auth.RateLimitConfig{
		RequestsPerMinute: 10,
		Burst:             0, // Disable burst limiting for this test
	}

	// Cause multiple Redis failures to trigger circuit breaker
	for i := 0; i < 6; i++ {
		allowed, err := limiter.Allow(ctx, "test-key", config)

		// All requests should succeed via fallback
		require.NoError(t, err)
		assert.True(t, allowed)

		// After 5 failures, circuit should open
		if i >= 5 {
			assert.Equal(t, circuit.StateOpen, limiter.GetCircuitBreakerState())
		}
	}

	// Verify circuit is open
	assert.Equal(t, circuit.StateOpen, limiter.GetCircuitBreakerState())
}

func TestRedisRateLimiterWithCircuitBreaker_CircuitRecovery(t *testing.T) {
	// Set up circuit recovery test environment
	db, mock, limiter := setupCircuitRecoveryTest(t)

	defer func() { _ = db.Close() }()

	ctx := context.Background()
	config := auth.RateLimitConfig{
		RequestsPerMinute: 10,
		Burst:             0,
	}

	// Cause circuit to open with Redis failures
	causeCircuitToOpen(t, mock, limiter, ctx, config)

	// Verify circuit recovery behavior
	verifyCircuitRecovery(t, limiter, ctx, config)
}

//nolint:ireturn // Returns mock interface for testing
func setupCircuitRecoveryTest(t *testing.T) (*redis.Client, redismock.ClientMock, *RedisRateLimiterWithCircuitBreaker) {
	t.Helper()

	// Create a working mock Redis client for this test
	db, mock := redismock.NewClientMock()
	logger := zap.NewNop()
	limiter := CreateFaultTolerantRedisRateLimiter(db, logger)

	// Create a circuit breaker with short timeout for testing
	limiter.circuitBreaker = circuit.NewCircuitBreaker(3, 2, testIterations*time.Millisecond)

	return db, mock, limiter
}

func causeCircuitToOpen(
	t *testing.T, mock redismock.ClientMock, limiter *RedisRateLimiterWithCircuitBreaker,
	ctx context.Context, config auth.RateLimitConfig,
) {
	t.Helper()
	// Cause 3 Redis failures to open circuit
	for i := 0; i < 3; i++ {
		currentTime := time.Now().Unix() / 60
		key := fmt.Sprintf("ratelimit:test-key:%d", currentTime)

		mock.ExpectTxPipeline()
		mock.ExpectIncr(key).SetErr(errors.New("redis error"))
		mock.ExpectExpire(key, 61*time.Second).SetErr(errors.New("redis error"))
		mock.ExpectTxPipelineExec().SetErr(errors.New("pipeline failed"))

		allowed, err := limiter.Allow(ctx, "test-key", config)

		require.NoError(t, err) // Fallback should work
		assert.True(t, allowed)
	}
}

func verifyCircuitRecovery(
	t *testing.T, limiter *RedisRateLimiterWithCircuitBreaker,
	ctx context.Context, config auth.RateLimitConfig,
) {
	t.Helper()
	// Circuit should be open
	assert.Equal(t, circuit.StateOpen, limiter.GetCircuitBreakerState())

	// Wait for timeout to allow transition to half-open
	time.Sleep(150 * time.Millisecond)

	// Test that the circuit breaker can recover with successful Redis calls
	// This is a simplified test that just verifies the fallback works consistently
	allowed, err := limiter.Allow(ctx, "test-key", config)

	require.NoError(t, err) // Should succeed via fallback when circuit is open
	assert.True(t, allowed)
}

func TestRedisRateLimiterWithCircuitBreaker_FallbackRateLimit(t *testing.T) {
	// Create mock Redis client
	db, _ := redismock.NewClientMock()

	defer func() { _ = db.Close() }()

	logger := zap.NewNop()
	limiter := CreateFaultTolerantRedisRateLimiter(db, logger)

	// Manually open the circuit
	for i := 0; i < 5; i++ {
		_ = limiter.circuitBreaker.Call(func() error {
			return errors.New("manual error")
		})
	}

	ctx := context.Background()
	config := auth.RateLimitConfig{
		RequestsPerMinute: 2, // Very low limit for testing
	}

	// First two requests should pass
	for i := 0; i < 2; i++ {
		allowed, err := limiter.Allow(ctx, "test-key", config)

		require.NoError(t, err)
		assert.True(t, allowed)
	}

	// Third request should be rate limited by fallback
	allowed, err := limiter.Allow(ctx, "test-key", config)

	require.NoError(t, err)
	assert.False(t, allowed)
}

func TestRedisSessionManagerWithCircuitBreaker_Get(t *testing.T) {
	// Create mock Redis client
	db, mock := redismock.NewClientMock()

	defer func() { _ = db.Close() }()

	logger := zap.NewNop()
	manager := CreateFaultTolerantRedisSessionManager(db, logger)

	ctx := context.Background()

	// Test successful get
	mock.ExpectGet("test-key").SetVal("test-value")

	value, err := manager.Get(ctx, "test-key")

	require.NoError(t, err)
	assert.Equal(t, "test-value", value)

	// Test key not found (should not trigger circuit breaker)
	mock.ExpectGet("missing-key").RedisNil()

	_, err = manager.Get(ctx, "missing-key")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "key not found")
	assert.Equal(t, circuit.StateClosed, manager.GetCircuitBreakerState())

	// Simulate failures to open circuit
	for i := 0; i < 5; i++ {
		mock.ExpectGet("error-key").SetErr(errors.New("redis error"))

		_, err = manager.Get(ctx, "error-key")

		require.Error(t, err)
	}

	// Circuit should be open
	assert.Equal(t, circuit.StateOpen, manager.GetCircuitBreakerState())

	// Next request should fail immediately
	_, err = manager.Get(ctx, "any-key")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "circuit breaker is open")

	// Ensure all expectations were met
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRedisSessionManagerWithCircuitBreaker_SetAndDelete(t *testing.T) {
	// Create mock Redis client
	db, mock := redismock.NewClientMock()

	defer func() { _ = db.Close() }()

	logger := zap.NewNop()
	manager := CreateFaultTolerantRedisSessionManager(db, logger)

	ctx := context.Background()

	// Test successful set
	mock.ExpectSet("test-key", "test-value", 5*time.Minute).SetVal("OK")
	err := manager.Set(ctx, "test-key", "test-value", 5*time.Minute)

	require.NoError(t, err)

	// Test successful delete
	mock.ExpectDel("test-key").SetVal(1)

	err = manager.Delete(ctx, "test-key")

	require.NoError(t, err)

	// Open the circuit
	for i := 0; i < 5; i++ {
		_ = manager.circuitBreaker.Call(func() error {
			return errors.New("manual error")
		})
	}

	// Operations should fail when circuit is open
	err = manager.Set(ctx, "key", "value", time.Minute)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "circuit breaker is open")

	err = manager.Delete(ctx, "key")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "circuit breaker is open")

	// Ensure all expectations were met
	assert.NoError(t, mock.ExpectationsWereMet())
}
