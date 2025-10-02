package ratelimit

import (
	"context"
	"errors"
	"time"

	"github.com/go-redis/redis/v8"
	"go.uber.org/zap"

	"github.com/actual-software/mcp-bridge/services/gateway/internal/auth"
	customerrors "github.com/actual-software/mcp-bridge/services/gateway/internal/errors"
	"github.com/actual-software/mcp-bridge/services/gateway/pkg/circuit"
)

const (
	circuitBreakerThreshold      = 5
	circuitBreakerFailureCount   = 3
	circuitBreakerTimeoutSeconds = 30
)

// RedisRateLimiterWithCircuitBreaker wraps RedisRateLimiter with circuit breaker pattern.
type RedisRateLimiterWithCircuitBreaker struct {
	limiter        *RedisRateLimiter
	circuitBreaker *circuit.CircuitBreaker
	logger         *zap.Logger
	fallback       RateLimiter // Fallback to in-memory rate limiter when circuit is open
}

// CreateFaultTolerantRedisRateLimiter creates a Redis rate limiter with circuit breaker protection.
func CreateFaultTolerantRedisRateLimiter(client *redis.Client, logger *zap.Logger) *RedisRateLimiterWithCircuitBreaker {
	return &RedisRateLimiterWithCircuitBreaker{
		limiter: CreateRedisBackedRateLimiter(client, logger),
		circuitBreaker: circuit.NewCircuitBreaker(
			circuitBreakerThreshold,
			circuitBreakerFailureCount,
			circuitBreakerTimeoutSeconds*time.Second,
		),
		logger:   logger,
		fallback: CreateLocalMemoryRateLimiter(logger),
	}
}

// Allow checks if a request is allowed with circuit breaker protection.
func (r *RedisRateLimiterWithCircuitBreaker) Allow(
	ctx context.Context,
	key string,
	config auth.RateLimitConfig,
) (bool, error) {
	// Check circuit breaker state
	if r.circuitBreaker.IsOpen() {
		r.logger.Warn("Circuit breaker is open, using fallback rate limiter",
			zap.String("key", key))

		return r.fallback.Allow(ctx, key, config)
	}

	// Try Redis with circuit breaker
	var allowed bool

	var err error

	cbErr := r.circuitBreaker.Call(func() error {
		allowed, err = r.limiter.Allow(ctx, key, config)

		return err
	})
	if cbErr != nil {
		// Circuit breaker opened or Redis call failed
		r.logger.Warn("Redis rate limiter failed, using fallback",
			zap.String("key", key),
			zap.Error(cbErr),
			zap.String("circuit_state", r.circuitBreaker.GetState().String()))

		// Use fallback in-memory rate limiter
		return r.fallback.Allow(ctx, key, config)
	}

	return allowed, nil
}

// GetCircuitBreakerState returns the current state of the circuit breaker.
func (r *RedisRateLimiterWithCircuitBreaker) GetCircuitBreakerState() circuit.State {
	return r.circuitBreaker.GetState()
}

// ResetCircuitBreaker manually resets the circuit breaker to closed state.
func (r *RedisRateLimiterWithCircuitBreaker) ResetCircuitBreaker() {
	r.circuitBreaker.Reset()
	r.logger.Info("Circuit breaker manually reset to closed state")
}

// RedisSessionManagerWithCircuitBreaker provides circuit breaker protection for Redis session operations.
type RedisSessionManagerWithCircuitBreaker struct {
	client         *redis.Client
	circuitBreaker *circuit.CircuitBreaker
	logger         *zap.Logger
}

// CreateFaultTolerantRedisSessionManager creates a Redis session manager with circuit breaker protection.
func CreateFaultTolerantRedisSessionManager(
	client *redis.Client,
	logger *zap.Logger,
) *RedisSessionManagerWithCircuitBreaker {
	return &RedisSessionManagerWithCircuitBreaker{
		client: client,
		circuitBreaker: circuit.NewCircuitBreaker(
			circuitBreakerThreshold,
			circuitBreakerFailureCount,
			circuitBreakerTimeoutSeconds*time.Second,
		),
		logger: logger,
	}
}

// Get retrieves a value from Redis with circuit breaker protection.
func (r *RedisSessionManagerWithCircuitBreaker) Get(ctx context.Context, key string) (string, error) {
	if r.circuitBreaker.IsOpen() {
		return "", customerrors.New(customerrors.TypeUnavailable, "circuit breaker is open for Redis operations").
			WithComponent("ratelimit_circuit")
	}

	var result string

	var err error

	cbErr := r.circuitBreaker.Call(func() error {
		result, err = r.client.Get(ctx, key).Result()
		if errors.Is(err, redis.Nil) {
			// Not found is not a circuit breaker failure
			return nil
		}

		return err
	})
	if cbErr != nil {
		r.logger.Warn("Redis Get operation failed",
			zap.String("key", key),
			zap.Error(cbErr),
			zap.String("circuit_state", r.circuitBreaker.GetState().String()))

		return "", cbErr
	}

	if errors.Is(err, redis.Nil) {
		return "", customerrors.New(customerrors.TypeNotFound, "key not found").
			WithComponent("ratelimit_circuit")
	}

	return result, nil
}

// Set stores a value in Redis with circuit breaker protection.
func (r *RedisSessionManagerWithCircuitBreaker) Set(
	ctx context.Context,
	key, value string,
	expiration time.Duration,
) error {
	if r.circuitBreaker.IsOpen() {
		return customerrors.New(customerrors.TypeUnavailable, "circuit breaker is open for Redis operations").
			WithComponent("ratelimit_circuit")
	}

	return r.circuitBreaker.Call(func() error {
		return r.client.Set(ctx, key, value, expiration).Err()
	})
}

// Delete removes a value from Redis with circuit breaker protection.
func (r *RedisSessionManagerWithCircuitBreaker) Delete(ctx context.Context, key string) error {
	if r.circuitBreaker.IsOpen() {
		return customerrors.New(customerrors.TypeUnavailable, "circuit breaker is open for Redis operations").
			WithComponent("ratelimit_circuit")
	}

	return r.circuitBreaker.Call(func() error {
		return r.client.Del(ctx, key).Err()
	})
}

// GetCircuitBreakerState returns the current state of the circuit breaker.
func (r *RedisSessionManagerWithCircuitBreaker) GetCircuitBreakerState() circuit.State {
	return r.circuitBreaker.GetState()
}
