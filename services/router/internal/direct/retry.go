package direct

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/poiley/mcp-bridge/services/router/internal/constants"
	"go.uber.org/zap"
)

// Retry calculation constants.
const (
	HalfFactor        = 0.5
	AverageCalculator = 2.0
)

// RetryableError represents an error that can be retried.
type RetryableError struct {
	Err       error
	Retryable bool
	ErrorType string
}

func (e *RetryableError) Error() string {
	return e.Err.Error()
}

func (e *RetryableError) Unwrap() error {
	return e.Err
}

// IsRetryable checks if an error is retryable.
func IsRetryable(err error) bool {
	retryableErr := &RetryableError{}
	if errors.As(err, &retryableErr) {
		return retryableErr.Retryable
	}
	// Default retry logic for common error types.
	errStr := err.Error()
	// Retry on timeout, connection, and temporary errors.
	return containsAny(errStr, []string{
		"timeout", "connection", "temporary", "refused",
		"reset", "broken pipe", "no route to host",
	})
}

// GetErrorType returns the type of error for contextual retry logic.
func GetErrorType(err error) string {
	retryableErr := &RetryableError{}
	if errors.As(err, &retryableErr) {
		return retryableErr.ErrorType
	}

	errStr := err.Error()
	switch {
	case containsAny(errStr, []string{"timeout", "deadline"}):
		return "timeout"
	case containsAny(errStr, []string{"connection", "refused", "reset"}):
		return "connection"
	case containsAny(errStr, []string{"temporary", "unavailable"}):
		return "temporary"
	case containsAny(errStr, []string{"authentication", "unauthorized"}):
		return "auth"
	default:
		return "unknown"
	}
}

// CircuitBreaker implements a simple circuit breaker pattern.
type CircuitBreaker struct {
	mu              sync.RWMutex
	failureCount    int
	lastFailureTime time.Time
	state           CircuitState
	config          AdaptiveRetryConfig
}

// CircuitState represents the state of the circuit breaker.
type CircuitState int

const (
	CircuitClosed CircuitState = iota
	CircuitOpen
	CircuitHalfOpen
)

// NewCircuitBreaker creates a new circuit breaker.
func NewCircuitBreaker(config AdaptiveRetryConfig) *CircuitBreaker {
	return &CircuitBreaker{
		state:  CircuitClosed,
		config: config,
	}
}

// Execute executes a function with circuit breaker protection.
func (cb *CircuitBreaker) Execute(fn func() error) error {
	if !cb.config.CircuitBreakerEnabled {
		return fn()
	}

	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case CircuitOpen:
		if time.Since(cb.lastFailureTime) > cb.config.RecoveryTimeout {
			cb.state = CircuitHalfOpen
			cb.failureCount = 0
		} else {
			return errors.New("circuit breaker is open")
		}
	case CircuitHalfOpen:
		// Allow one request through.
	case CircuitClosed:
		// Normal operation.
	}

	err := fn()
	if err != nil {
		cb.recordFailure()

		return err
	}

	cb.recordSuccess()

	return nil
}

func (cb *CircuitBreaker) recordFailure() {
	cb.failureCount++
	cb.lastFailureTime = time.Now()

	if cb.failureCount >= cb.config.FailureThreshold {
		cb.state = CircuitOpen
	}
}

func (cb *CircuitBreaker) recordSuccess() {
	cb.failureCount = 0
	cb.state = CircuitClosed
}

// AdaptiveRetry manages adaptive retry behavior for clients.
type AdaptiveRetry struct {
	config         AdaptiveRetryConfig
	logger         *zap.Logger
	circuitBreaker *CircuitBreaker
	mu             sync.RWMutex

	// Error type statistics for contextual retries.
	errorTypeStats map[string]*ErrorStats

	// Random source for jitter.
	rng *rand.Rand
}

// ErrorStats tracks statistics for specific error types.
type ErrorStats struct {
	Count             int64
	SuccessAfterRetry int64
	AvgRetriesNeeded  float64
}

// NewAdaptiveRetry creates a new adaptive retry manager.
func NewAdaptiveRetry(config AdaptiveRetryConfig, logger *zap.Logger) *AdaptiveRetry {
	// Set defaults.
	if config.MaxRetries == 0 {
		config.MaxRetries = 3
	}

	if config.BaseDelay == 0 {
		config.BaseDelay = time.Duration(DefaultBaseDelayMillis) * time.Millisecond
	}

	if config.MaxDelay == 0 {
		config.MaxDelay = constants.DefaultTimeout
	}

	if config.BackoffFactor == 0 {
		config.BackoffFactor = 2.0
	}

	if config.JitterRatio == 0 {
		config.JitterRatio = 0.1 // 10% jitter
	}

	if config.FailureThreshold == 0 {
		config.FailureThreshold = 5
	}

	if config.RecoveryTimeout == 0 {
		config.RecoveryTimeout = constants.LongTimeout
	}

	return &AdaptiveRetry{
		config:         config,
		logger:         logger.With(zap.String("component", "adaptive_retry")),
		circuitBreaker: NewCircuitBreaker(config),
		errorTypeStats: make(map[string]*ErrorStats),
		// Non-cryptographic randomness for jitter is acceptable
		rng: rand.New(rand.NewSource(time.Now().UnixNano())), 
	}
}

// Execute executes a function with adaptive retry logic.
func (ar *AdaptiveRetry) Execute(ctx context.Context, fn func() error) error {
	return ar.circuitBreaker.Execute(func() error {
		return ar.executeWithRetry(ctx, fn)
	})
}

func (ar *AdaptiveRetry) executeWithRetry(ctx context.Context, fn func() error) error {
	var lastErr error

	for attempt := 0; attempt <= ar.config.MaxRetries; attempt++ {
		// Check context cancellation.
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Execute the function.
		err := fn()
		if err == nil {
			// Success - record metrics if this was a retry.
			if attempt > 0 {
				ar.recordRetrySuccess(GetErrorType(lastErr), attempt)
			}

			return nil
		}

		lastErr = err

		// Check if error is retryable.
		if !IsRetryable(err) {
			ar.logger.Debug("error not retryable, giving up",
				zap.Error(err),
				zap.Int("attempt", attempt))

			return err
		}

		// Don't retry on the last attempt.
		if attempt == ar.config.MaxRetries {
			break
		}

		// Calculate delay for next attempt.
		delay := ar.calculateDelay(attempt, GetErrorType(err))

		ar.logger.Debug("retrying after delay",
			zap.Error(err),
			zap.Int("attempt", attempt+1),
			zap.Int("max_retries", ar.config.MaxRetries),
			zap.Duration("delay", delay))

		// Wait before retry.
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()

			return ctx.Err()
		case <-timer.C:
			// Continue to next attempt.
		}
	}

	// Record failure statistics.
	ar.recordRetryFailure(GetErrorType(lastErr), ar.config.MaxRetries)

	return fmt.Errorf("max retries exceeded: %w", lastErr)
}

func (ar *AdaptiveRetry) calculateDelay(attempt int, errorType string) time.Duration {
	// Base exponential backoff.
	delay := time.Duration(float64(ar.config.BaseDelay) * math.Pow(ar.config.BackoffFactor, float64(attempt)))

	// Apply contextual adjustment based on error type.
	if ar.config.ContextualRetries {
		delay = ar.adjustDelayForErrorType(delay, errorType)
	}

	// Apply max delay limit.
	if delay > ar.config.MaxDelay {
		delay = ar.config.MaxDelay
	}

	// Add jitter if enabled.
	if ar.config.EnableJitter {
		jitter := time.Duration(float64(delay) * ar.config.JitterRatio * (ar.rng.Float64()*2.0 - 1.0))
		delay += jitter

		// Ensure delay is not negative.
		if delay < 0 {
			delay = ar.config.BaseDelay
		}
	}

	return delay
}

func (ar *AdaptiveRetry) adjustDelayForErrorType(baseDelay time.Duration, errorType string) time.Duration {
	if !ar.config.FailureTypeWeighting {
		return baseDelay
	}

	ar.mu.RLock()
	stats, exists := ar.errorTypeStats[errorType]
	ar.mu.RUnlock()

	if !exists {
		return baseDelay
	}

	// Adjust delay based on historical success rate for this error type.
	successRate := float64(stats.SuccessAfterRetry) / float64(stats.Count)

	switch errorType {
	case "timeout":
		// Timeout errors may benefit from longer delays.
		if successRate < LowSuccessRateThreshold {
			return time.Duration(float64(baseDelay) * HighBackoffMultiplier)
		}
	case "connection":
		// Connection errors might need shorter delays for quick reconnection.
		if successRate > HighSuccessRateThreshold {
			return time.Duration(float64(baseDelay) * LowBackoffMultiplier)
		}
	case "temporary":
		// Temporary errors might need varied delays.
		return time.Duration(float64(baseDelay) * (1.0 + (ar.rng.Float64()-HalfFactor)*0.4))
	case "auth":
		// Auth errors rarely benefit from quick retries.
		const authDelayMultiplier = 2.0

		return time.Duration(float64(baseDelay) * authDelayMultiplier)
	}

	return baseDelay
}

func (ar *AdaptiveRetry) recordRetrySuccess(errorType string, attempts int) {
	ar.mu.Lock()
	defer ar.mu.Unlock()

	stats, exists := ar.errorTypeStats[errorType]
	if !exists {
		stats = &ErrorStats{}
		ar.errorTypeStats[errorType] = stats
	}

	stats.Count++
	stats.SuccessAfterRetry++

	// Update average retries needed (simple moving average).
	if stats.SuccessAfterRetry == 1 {
		stats.AvgRetriesNeeded = float64(attempts)
	} else {
		stats.AvgRetriesNeeded = (stats.AvgRetriesNeeded + float64(attempts)) / AverageCalculator
	}
}

func (ar *AdaptiveRetry) recordRetryFailure(errorType string, attempts int) {
	ar.mu.Lock()
	defer ar.mu.Unlock()

	stats, exists := ar.errorTypeStats[errorType]
	if !exists {
		stats = &ErrorStats{}
		ar.errorTypeStats[errorType] = stats
	}

	stats.Count++
	// Don't increment SuccessAfterRetry for failures.
}

// GetStats returns current adaptive retry statistics.
func (ar *AdaptiveRetry) GetStats() map[string]interface{} {
	ar.mu.RLock()
	defer ar.mu.RUnlock()

	errorStats := make(map[string]map[string]interface{})

	for errorType, stats := range ar.errorTypeStats {
		successRate := float64(0)
		if stats.Count > 0 {
			successRate = float64(stats.SuccessAfterRetry) / float64(stats.Count)
		}

		errorStats[errorType] = map[string]interface{}{
			"count":               stats.Count,
			"success_after_retry": stats.SuccessAfterRetry,
			"success_rate":        successRate,
			"avg_retries_needed":  stats.AvgRetriesNeeded,
		}
	}

	return map[string]interface{}{
		"max_retries":      ar.config.MaxRetries,
		"base_delay":       ar.config.BaseDelay,
		"max_delay":        ar.config.MaxDelay,
		"backoff_factor":   ar.config.BackoffFactor,
		"error_type_stats": errorStats,
	}
}

// Helper function to check if string contains any of the substrings.
func containsAny(s string, substrings []string) bool {
	for _, substr := range substrings {
		if strings.Contains(strings.ToLower(s), strings.ToLower(substr)) {
			return true
		}
	}

	return false
}
