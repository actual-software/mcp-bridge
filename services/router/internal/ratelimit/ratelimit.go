package ratelimit

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

const (
	defaultTimeoutSeconds = 30
	// DefaultBackoffTime is the initial backoff time for rate limiting retries.
	DefaultBackoffTime = 10 * time.Millisecond
)

// RateLimiter provides rate limiting functionality.
type RateLimiter interface {
	// Allow checks if a request is allowed.
	Allow(ctx context.Context) error
	// Wait blocks until a request is allowed.
	Wait(ctx context.Context) error
}

// TokenBucketLimiter implements a token bucket rate limiter.
type TokenBucketLimiter struct {
	logger *zap.Logger

	// Configuration.
	rate    float64       // tokens per second
	burst   int           // maximum burst size
	maxWait time.Duration // maximum wait time

	// State.
	mu       sync.Mutex
	tokens   float64
	lastTime time.Time
}

// NewTokenBucketLimiter creates a new token bucket rate limiter.
func NewTokenBucketLimiter(rate float64, burst int, maxWait time.Duration, logger *zap.Logger) *TokenBucketLimiter {
	// Handle initial token count for zero/negative burst.
	initialTokens := float64(burst)
	if burst <= 0 && rate > 0 {
		// For zero burst with positive rate, start with 1 token to allow immediate use.
		initialTokens = 1
	} else if burst <= 0 {
		initialTokens = 0
	}

	return &TokenBucketLimiter{
		logger:   logger,
		rate:     rate,
		burst:    burst,
		maxWait:  maxWait,
		tokens:   initialTokens,
		lastTime: time.Now(),
	}
}

// Allow checks if a request is allowed (non-blocking).
func (l *TokenBucketLimiter) Allow(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Reject immediately if rate is <= 0.
	if l.rate <= 0 {
		return fmt.Errorf("rate limit exceeded (%.1f requests/sec, burst %d)", l.rate, l.burst)
	}

	// Refill tokens based on elapsed time.
	now := time.Now()
	elapsed := now.Sub(l.lastTime)

	l.tokens += elapsed.Seconds() * l.rate

	// Handle burst capacity.
	maxTokens := float64(l.burst)
	if l.burst <= 0 {
		// For zero or negative burst, allow tokens from rate but cap at 1.
		// This ensures zero burst still works with positive rate.
		maxTokens = 1.0
	}

	if l.tokens > maxTokens {
		l.tokens = maxTokens
	}

	l.lastTime = now

	// Check if we have a token.
	if l.tokens >= 1.0 {
		l.tokens -= 1.0

		return nil
	}

	return fmt.Errorf("rate limit exceeded (%.1f requests/sec, burst %d)", l.rate, l.burst)
}

// Wait blocks until a request is allowed or context is cancelled.
func (l *TokenBucketLimiter) Wait(ctx context.Context) error {
	for {
		// Check if we can proceed immediately.
		l.mu.Lock()

		// Refill tokens based on elapsed time.
		now := time.Now()
		elapsed := now.Sub(l.lastTime)

		l.tokens += elapsed.Seconds() * l.rate
		if l.tokens > float64(l.burst) {
			l.tokens = float64(l.burst)
		}

		l.lastTime = now

		// Check if we have a token available.
		if l.tokens >= 1.0 {
			l.tokens -= 1.0
			l.mu.Unlock()

			return nil
		}

		// Calculate how long we need to wait for the next token.
		tokensNeeded := 1.0 - l.tokens
		waitTime := time.Duration(tokensNeeded / l.rate * float64(time.Second))
		l.mu.Unlock()

		// Check if wait time exceeds maximum.
		if waitTime > l.maxWait {
			return fmt.Errorf("rate limit wait time (%v) exceeds maximum (%v)", waitTime, l.maxWait)
		}

		// Wait with context cancellation.
		timer := time.NewTimer(waitTime)

		select {
		case <-ctx.Done():
			timer.Stop()

			return ctx.Err()
		case <-timer.C:
			// Continue the loop to try again.
		}

		timer.Stop()
	}
}

// SlidingWindowLimiter implements a sliding window rate limiter.
type SlidingWindowLimiter struct {
	logger *zap.Logger

	// Configuration.
	windowSize time.Duration
	maxEvents  int

	// State.
	mu        sync.Mutex
	events    []time.Time
	cleanupAt time.Time
}

// NewSlidingWindowLimiter creates a new sliding window rate limiter.
func NewSlidingWindowLimiter(windowSize time.Duration, maxEvents int, logger *zap.Logger) *SlidingWindowLimiter {
	// Handle edge cases for maxEvents to prevent runtime panics.
	capacity := maxEvents
	if capacity < 0 {
		capacity = 0
	}

	return &SlidingWindowLimiter{
		logger:     logger,
		windowSize: windowSize,
		maxEvents:  maxEvents,
		events:     make([]time.Time, 0, capacity),
		cleanupAt:  time.Now().Add(windowSize),
	}
}

// Allow checks if a request is allowed.
func (l *SlidingWindowLimiter) Allow(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Reject immediately if maxEvents is <= 0.
	if l.maxEvents <= 0 {
		return fmt.Errorf("rate limit exceeded (%d events per %v)", l.maxEvents, l.windowSize)
	}

	now := time.Now()

	// Cleanup old events periodically.
	if now.After(l.cleanupAt) {
		l.cleanup(now)
		l.cleanupAt = now.Add(l.windowSize)
	}

	// Count events in window.
	windowStart := now.Add(-l.windowSize)
	count := 0

	for _, t := range l.events {
		if t.After(windowStart) {
			count++
		}
	}

	// Check if we're at the limit.
	if count >= l.maxEvents {
		return fmt.Errorf("rate limit exceeded (%d events per %v)", l.maxEvents, l.windowSize)
	}

	// Add new event.
	l.events = append(l.events, now)

	return nil
}

// Wait blocks until a request is allowed.
func (l *SlidingWindowLimiter) Wait(ctx context.Context) error {
	// For sliding window, we don't have a good way to predict wait time.
	// So we'll poll with exponential backoff.
	backoff := DefaultBackoffTime
	maxBackoff := 1 * time.Second

	for {
		// Check context cancellation first.
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err := l.Allow(ctx); err == nil {
			return nil
		}

		// Wait with context cancellation check.
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()

			return ctx.Err()
		case <-timer.C:
			// Exponential backoff.
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}

		timer.Stop()
	}
}

// cleanup removes old events from the sliding window.
func (l *SlidingWindowLimiter) cleanup(now time.Time) {
	windowStart := now.Add(-l.windowSize)
	newEvents := make([]time.Time, 0, len(l.events))

	for _, t := range l.events {
		if t.After(windowStart) {
			newEvents = append(newEvents, t)
		}
	}

	l.events = newEvents
}

// NoOpLimiter is a rate limiter that allows all requests.
type NoOpLimiter struct{}

// Allow always returns nil.
func (n *NoOpLimiter) Allow(ctx context.Context) error {
	return nil
}

// Wait always returns nil immediately.
func (n *NoOpLimiter) Wait(ctx context.Context) error {
	return nil
}

// NewRateLimiter creates a rate limiter based on configuration.
//
//nolint:ireturn // Factory pattern requires interface return
func NewRateLimiter(ratePerSecond float64, burst int, logger *zap.Logger) RateLimiter {
	if ratePerSecond <= 0 {
		// No rate limiting.
		return &NoOpLimiter{}
	}

	return NewTokenBucketLimiter(ratePerSecond, burst, defaultTimeoutSeconds*time.Second, logger)
}
