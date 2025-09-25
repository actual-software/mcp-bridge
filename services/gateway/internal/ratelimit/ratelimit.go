// Package ratelimit provides rate limiting functionality with support for multiple
// backends including Redis and in-memory storage.
package ratelimit

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
	"go.uber.org/zap"

	"github.com/poiley/mcp-bridge/services/gateway/internal/auth"
	customerrors "github.com/poiley/mcp-bridge/services/gateway/internal/errors"
)

const (
	defaultMaxTimeoutSeconds = 60
	defaultRetryCount        = 10
	rateLimitWindowSeconds   = 60 // Sliding window for rate limiting
	burstWindowSeconds       = 10 // Burst window for burst limiting
)

// RateLimiter defines the rate limiting interface.
type RateLimiter interface {
	Allow(ctx context.Context, key string, config auth.RateLimitConfig) (bool, error)
}

// RedisRateLimiter implements rate limiting using Redis.
type RedisRateLimiter struct {
	client *redis.Client
	logger *zap.Logger
	prefix string
}

// CreateRedisBackedRateLimiter creates a Redis-backed distributed rate limiter.
func CreateRedisBackedRateLimiter(client *redis.Client, logger *zap.Logger) *RedisRateLimiter {
	return &RedisRateLimiter{
		client: client,
		logger: logger,
		prefix: "ratelimit:",
	}
}

// Allow checks if a request should be allowed based on rate limiting configuration.
// Uses a sliding window counter algorithm.
// checkRateLimit checks the basic rate limit.
func (r *RedisRateLimiter) checkRateLimit(ctx context.Context, key string, limit int) (int64, error) {
	now := time.Now()
	window := time.Minute
	redisKey := fmt.Sprintf("%s%s:%d", r.prefix, key, now.Unix()/rateLimitWindowSeconds)

	// Use pipeline for atomic operations
	pipe := r.client.Pipeline()
	incr := pipe.Incr(ctx, redisKey)
	pipe.Expire(ctx, redisKey, window+time.Second)

	_, err := pipe.Exec(ctx)
	if err != nil {
		r.logger.Error("Failed to execute rate limit pipeline",
			zap.String("key", key),
			zap.Error(err))

		return 0, customerrors.Wrap(err, "rate limit check failed").
			WithComponent("ratelimit")
	}

	return incr.Val(), nil
}

// checkBurstLimit checks the burst limit.
func (r *RedisRateLimiter) checkBurstLimit(ctx context.Context, key string, burst int) (bool, error) {
	now := time.Now()
	burstWindow := defaultRetryCount * time.Second
	burstKey := fmt.Sprintf("%sburst:%s:%d", r.prefix, key, now.Unix()/burstWindowSeconds)

	burstPipe := r.client.Pipeline()
	burstIncr := burstPipe.Incr(ctx, burstKey)
	burstPipe.Expire(ctx, burstKey, burstWindow+time.Second)

	_, err := burstPipe.Exec(ctx)
	if err != nil {
		r.logger.Error("Failed to check burst limit",
			zap.String("key", key),
			zap.Error(err))

		return false, customerrors.Wrap(err, "burst limit check failed").
			WithComponent("ratelimit")
	}

	burstCount := burstIncr.Val()
	if burstCount > int64(burst) {
		r.logger.Debug("Burst limit exceeded",
			zap.String("key", key),
			zap.Int64("burst_count", burstCount),
			zap.Int("burst_limit", burst))

		return false, nil
	}

	return true, nil
}

func (r *RedisRateLimiter) Allow(ctx context.Context, key string, config auth.RateLimitConfig) (bool, error) {
	if config.RequestsPerMinute <= 0 {
		return true, nil
	}

	count, err := r.checkRateLimit(ctx, key, config.RequestsPerMinute)
	if err != nil {
		return false, err
	}

	allowed := count <= int64(config.RequestsPerMinute)
	if !allowed {
		r.logger.Debug("Rate limit exceeded",
			zap.String("key", key),
			zap.Int64("count", count),
			zap.Int("limit", config.RequestsPerMinute))

		return false, nil
	}

	// Handle burst if configured
	if config.Burst > 0 {
		return r.checkBurstLimit(ctx, key, config.Burst)
	}

	return allowed, nil
}

// InMemoryRateLimiter provides a simple in-memory rate limiter for testing.
type InMemoryRateLimiter struct {
	requests    map[string][]time.Time
	logger      *zap.Logger
	mu          sync.Mutex
	burstWindow time.Duration // configurable burst window for testing
}

// CreateLocalMemoryRateLimiter creates a local in-memory rate limiter for single-instance deployments.
func CreateLocalMemoryRateLimiter(logger *zap.Logger) *InMemoryRateLimiter {
	return &InMemoryRateLimiter{
		requests:    make(map[string][]time.Time),
		logger:      logger,
		burstWindow: defaultRetryCount * time.Second, // default burst window
	}
}

// Allow checks if a request is allowed (in-memory implementation).
func (r *InMemoryRateLimiter) Allow(_ context.Context, key string, config auth.RateLimitConfig) (bool, error) {
	if config.RequestsPerMinute <= 0 {
		return true, nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	windowStart := now.Add(-time.Minute)

	// Clean up old entries
	if requests, exists := r.requests[key]; exists {
		validRequests := make([]time.Time, 0, len(requests))

		for _, t := range requests {
			if t.After(windowStart) {
				validRequests = append(validRequests, t)
			}
		}

		r.requests[key] = validRequests
	}

	// Check current count
	currentCount := len(r.requests[key])
	if currentCount >= config.RequestsPerMinute {
		r.logger.Debug("Rate limit exceeded (in-memory)",
			zap.String("key", key),
			zap.Int("count", currentCount),
			zap.Int("limit", config.RequestsPerMinute))

		return false, nil
	}

	// Check burst limit
	if config.Burst > 0 {
		burstWindow := now.Add(-r.burstWindow)
		burstCount := 0

		for _, t := range r.requests[key] {
			if t.After(burstWindow) {
				burstCount++
			}
		}

		if burstCount >= config.Burst {
			r.logger.Debug("Burst limit exceeded (in-memory)",
				zap.String("key", key),
				zap.Int("burst_count", burstCount),
				zap.Int("burst_limit", config.Burst))

			return false, nil
		}
	}

	// Add current request
	r.requests[key] = append(r.requests[key], now)

	return true, nil
}

// SetBurstWindow sets the burst window duration (for testing).
func (r *InMemoryRateLimiter) SetBurstWindow(duration time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.burstWindow = duration
}
