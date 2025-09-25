package errors

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"math"
	"time"

	"go.uber.org/zap"
)

const (
	// fallbackModulo is used for fallback random number generation.
	fallbackModulo = 1000.0
	// defaultMaxAttempts is the default number of retry attempts.
	defaultMaxAttempts = 3
	// defaultMaxIntervalSeconds is the default maximum retry interval.
	defaultMaxIntervalSeconds = 30
	// defaultMultiplier is the default exponential backoff multiplier.
	defaultMultiplier = 2.0
	// defaultRandomizeFactor is the default jitter factor.
	defaultRandomizeFactor = 0.1
)

// secureRandom generates a cryptographically secure random float64 between 0 and 1.
func secureRandom() float64 {
	var b [8]byte

	_, err := rand.Read(b[:])
	if err != nil {
		// Fallback to current time if crypto/rand fails
		return float64(time.Now().UnixNano()%int64(fallbackModulo)) / fallbackModulo
	}

	// Convert bytes to uint64, then to float64 between 0 and 1
	return float64(binary.LittleEndian.Uint64(b[:])) / float64(^uint64(0))
}

// RetryConfig defines retry behavior configuration.
type RetryConfig struct {
	MaxAttempts     int
	InitialInterval time.Duration
	MaxInterval     time.Duration
	Multiplier      float64
	RandomizeFactor float64
}

// DefaultRetryConfig returns default retry configuration.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts:     defaultMaxAttempts,
		InitialInterval: 1 * time.Second,
		MaxInterval:     defaultMaxIntervalSeconds * time.Second,
		Multiplier:      defaultMultiplier,
		RandomizeFactor: defaultRandomizeFactor,
	}
}

// RetryPolicy defines the retry policy interface.
type RetryPolicy interface {
	ShouldRetry(err error, attempt int) bool
	NextInterval(attempt int) time.Duration
}

// ExponentialBackoffPolicy implements exponential backoff with jitter.
type ExponentialBackoffPolicy struct {
	config RetryConfig
	logger *zap.Logger
}

// NewExponentialBackoffPolicy creates a new exponential backoff policy.
func NewExponentialBackoffPolicy(config RetryConfig, logger *zap.Logger) *ExponentialBackoffPolicy {
	return &ExponentialBackoffPolicy{
		config: config,
		logger: logger,
	}
}

// ShouldRetry determines if an error should be retried.
func (p *ExponentialBackoffPolicy) ShouldRetry(err error, attempt int) bool {
	if attempt >= p.config.MaxAttempts {
		return false
	}

	// Check if error is retryable
	if !IsRetryable(err) {
		p.logger.Debug("Error is not retryable",
			zap.Error(err),
			zap.Int("attempt", attempt),
		)

		return false
	}

	return true
}

// NextInterval calculates the next retry interval.
func (p *ExponentialBackoffPolicy) NextInterval(attempt int) time.Duration {
	// Calculate base interval with exponential backoff
	interval := float64(p.config.InitialInterval) * math.Pow(p.config.Multiplier, float64(attempt-1))

	// Cap at max interval
	if interval > float64(p.config.MaxInterval) {
		interval = float64(p.config.MaxInterval)
	}

	// Add jitter
	if p.config.RandomizeFactor > 0 {
		delta := interval * p.config.RandomizeFactor
		minInterval := interval - delta
		maxInterval := interval + delta

		// Generate random interval between min and max
		interval = minInterval + (secureRandom() * (maxInterval - minInterval))
	}

	return time.Duration(interval)
}

// RetryOperation represents a retryable operation.
type RetryOperation func(ctx context.Context) error

// RetryManager manages retry operations.
type RetryManager struct {
	policy RetryPolicy
	logger *zap.Logger
}

// NewRetryManager creates a new retry manager.
func NewRetryManager(policy RetryPolicy, logger *zap.Logger) *RetryManager {
	return &RetryManager{
		policy: policy,
		logger: logger,
	}
}

// Execute executes an operation with retry logic.
func (m *RetryManager) Execute(ctx context.Context, operation RetryOperation) error {
	var lastErr error

	for attempt := 1; ; attempt++ {
		// Check context
		if err := ctx.Err(); err != nil {
			return Error(CMN_INT_CONTEXT_CANC, "Operation canceled: "+err.Error())
		}

		// Execute operation
		startTime := time.Now()
		err := operation(ctx)
		duration := time.Since(startTime)

		// Log attempt
		m.logger.Debug("Retry attempt completed",
			zap.Int("attempt", attempt),
			zap.Duration("duration", duration),
			zap.Error(err),
		)

		// Success
		if err == nil {
			if attempt > 1 {
				m.logger.Info("Operation succeeded after retry",
					zap.Int("attempts", attempt),
					zap.Duration("total_duration", time.Since(startTime)),
				)
			}

			return nil
		}

		lastErr = err

		// Check if we should retry
		if !m.policy.ShouldRetry(err, attempt) {
			m.logger.Warn("Operation failed, no more retries",
				zap.Int("attempts", attempt),
				zap.Error(err),
			)

			return err
		}

		// Calculate retry interval
		interval := m.policy.NextInterval(attempt)

		m.logger.Info("Retrying operation",
			zap.Int("attempt", attempt),
			zap.Duration("retry_after", interval),
			zap.Error(err),
		)

		// Wait before retry
		select {
		case <-time.After(interval):
			// Continue to next attempt
		case <-ctx.Done():
			return Error(CMN_INT_CONTEXT_CANC, "Operation canceled during retry: "+lastErr.Error())
		}
	}
}

// CircuitBreakerPolicy implements circuit breaker pattern.
type CircuitBreakerPolicy struct {
	config          RetryConfig
	failureCount    int
	successCount    int
	lastFailureTime time.Time
	state           CircuitState
	logger          *zap.Logger
}

// CircuitState represents circuit breaker states.
type CircuitState int

const (
	// CircuitClosed indicates the circuit breaker is closed and allowing requests.
	CircuitClosed CircuitState = iota
	// CircuitOpen indicates the circuit breaker is open and blocking requests.
	CircuitOpen
	// CircuitHalfOpen indicates the circuit breaker is testing if the service has recovered.
	CircuitHalfOpen
)

// NewCircuitBreakerPolicy creates a new circuit breaker policy.
func NewCircuitBreakerPolicy(config RetryConfig, logger *zap.Logger) *CircuitBreakerPolicy {
	return &CircuitBreakerPolicy{
		config:          config,
		failureCount:    0,           // Initialize failure count
		successCount:    0,           // Initialize success count
		lastFailureTime: time.Time{}, // Initialize to zero time
		state:           CircuitClosed,
		logger:          logger,
	}
}

// ShouldRetry implements circuit breaker logic.
func (p *CircuitBreakerPolicy) ShouldRetry(err error, attempt int) bool {
	switch p.state {
	case CircuitOpen:
		// Check if we should transition to half-open
		if time.Since(p.lastFailureTime) > p.config.MaxInterval {
			p.state = CircuitHalfOpen
			p.logger.Info("Circuit breaker transitioning to half-open")

			return true
		}

		return false

	case CircuitHalfOpen:
		// Allow one retry in half-open state
		return attempt == 1

	case CircuitClosed:
		// Check if error is retryable
		if !IsRetryable(err) {
			return false
		}

		// Check failure threshold
		p.failureCount++
		if p.failureCount >= p.config.MaxAttempts {
			p.state = CircuitOpen
			p.lastFailureTime = time.Now()
			p.logger.Warn("Circuit breaker opened due to failures",
				zap.Int("failure_count", p.failureCount),
			)

			return false
		}

		return true
	}

	return false
}

// NextInterval returns retry interval for circuit breaker.
func (p *CircuitBreakerPolicy) NextInterval(attempt int) time.Duration {
	if p.state == CircuitHalfOpen {
		// Use shorter interval for half-open state
		return p.config.InitialInterval
	}

	// Use exponential backoff for closed state
	return time.Duration(float64(p.config.InitialInterval) * math.Pow(p.config.Multiplier, float64(attempt-1)))
}

// RecordSuccess records a successful operation.
func (p *CircuitBreakerPolicy) RecordSuccess() {
	p.successCount++

	switch p.state {
	case CircuitHalfOpen:
		// Transition to closed after success in half-open
		p.state = CircuitClosed
		p.failureCount = 0
		p.logger.Info("Circuit breaker closed after successful retry")

	case CircuitClosed:
		// Reset failure count after consecutive successes
		if p.successCount > p.config.MaxAttempts {
			p.failureCount = 0
		}

	case CircuitOpen:
		// No action needed for open state on success
	} 
}

// RecordFailure records a failed operation.
func (p *CircuitBreakerPolicy) RecordFailure() {
	p.successCount = 0

	if p.state == CircuitHalfOpen {
		// Return to open state
		p.state = CircuitOpen
		p.lastFailureTime = time.Now()
		p.logger.Warn("Circuit breaker reopened after failure in half-open state")
	}
}
