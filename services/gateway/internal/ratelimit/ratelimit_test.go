package ratelimit

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/poiley/mcp-bridge/services/gateway/internal/auth"
)

const testUser = "test-user"

const (
	testIterations = 100
	testTimeout    = 50
)

func TestInMemoryRateLimiter_Allow(t *testing.T) {
	logger := zap.NewNop()
	limiter := CreateLocalMemoryRateLimiter(logger)

	config := auth.RateLimitConfig{
		RequestsPerMinute: 10,
		Burst:             0, // Disable burst for this test
	}

	ctx := context.Background()
	key := testUser

	// First 10 requests should be allowed
	for i := 0; i < 10; i++ {
		allowed, err := limiter.Allow(ctx, key, config)
		require.NoError(t, err)
		assert.True(t, allowed, "Request %d should be allowed", i+1)
	}

	// 11th request should be denied
	allowed, err := limiter.Allow(ctx, key, config)
	require.NoError(t, err)
	assert.False(t, allowed, "11th request should be denied")
}

func TestInMemoryRateLimiter_Burst(t *testing.T) {
	logger := zap.NewNop()
	limiter := CreateLocalMemoryRateLimiter(logger)
	// Use a short burst window for fast testing
	limiter.SetBurstWindow(testTimeout * time.Millisecond)

	config := auth.RateLimitConfig{
		RequestsPerMinute: 60, // 1 per second
		Burst:             3,
	}

	ctx := context.Background()
	key := testUser

	// First 3 requests in rapid succession should be allowed (burst)
	for i := 0; i < 3; i++ {
		allowed, err := limiter.Allow(ctx, key, config)
		require.NoError(t, err)
		assert.True(t, allowed, "Request %d should be allowed (burst)", i+1)
		time.Sleep(10 * time.Millisecond)
	}

	// 4th request within burst window should be denied
	allowed, err := limiter.Allow(ctx, key, config)
	require.NoError(t, err)
	assert.False(t, allowed, "4th request within burst window should be denied")

	// Wait for burst window to pass (now configured to 50ms)
	time.Sleep(60 * time.Millisecond)

	// Request should be allowed again
	allowed, err = limiter.Allow(ctx, key, config)
	require.NoError(t, err)
	assert.True(t, allowed, "Request after burst window should be allowed")
}

func TestInMemoryRateLimiter_NoLimit(t *testing.T) {
	logger := zap.NewNop()
	limiter := CreateLocalMemoryRateLimiter(logger)

	config := auth.RateLimitConfig{
		RequestsPerMinute: 0, // No limit
		Burst:             0,
	}

	ctx := context.Background()
	key := testUser

	// All requests should be allowed
	for i := 0; i < testIterations; i++ {
		allowed, err := limiter.Allow(ctx, key, config)
		require.NoError(t, err)
		assert.True(t, allowed, "Request %d should be allowed (no limit)", i+1)
	}
}

func TestInMemoryRateLimiter_MultipleKeys(t *testing.T) {
	logger := zap.NewNop()
	limiter := CreateLocalMemoryRateLimiter(logger)

	config := auth.RateLimitConfig{
		RequestsPerMinute: 5,
		Burst:             0, // Disable burst for this test
	}

	ctx := context.Background()

	// Each user should have independent limits
	for _, user := range []string{"user1", "user2", "user3"} {
		for i := 0; i < 5; i++ {
			allowed, err := limiter.Allow(ctx, user, config)
			require.NoError(t, err)
			assert.True(t, allowed, "Request %d for %s should be allowed", i+1, user)
		}

		// 6th request should be denied
		allowed, err := limiter.Allow(ctx, user, config)
		require.NoError(t, err)
		assert.False(t, allowed, "6th request for %s should be denied", user)
	}
}
