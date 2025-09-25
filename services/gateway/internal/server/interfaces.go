package server

import (
	"context"

	"github.com/poiley/mcp-bridge/services/gateway/internal/auth"
)

// RateLimiter defines the rate limiting interface.
type RateLimiter interface {
	Allow(ctx context.Context, key string, config auth.RateLimitConfig) (bool, error)
}
