package gateway

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/poiley/mcp-bridge/services/router/pkg/mcp"
)

// CircuitState represents the state of a circuit breaker.
type CircuitState int

const (
	// CircuitClosed - requests are allowed through.
	CircuitClosed CircuitState = iota
	// CircuitOpen - requests are blocked, circuit is "tripped".
	CircuitOpen
	// CircuitHalfOpen - limited requests are allowed to test if service is recovered.
	CircuitHalfOpen
)

const (
	// DefaultFailureThreshold is the default number of consecutive failures before circuit opens.
	DefaultFailureThreshold = 5
	// DefaultRecoveryTimeout is the default time to wait before attempting recovery.
	DefaultRecoveryTimeout = 30 * time.Second
	// DefaultSuccessThreshold is the default number of consecutive successes needed to close circuit.
	DefaultSuccessThreshold = 3
	// DefaultTimeoutDuration is the default timeout for circuit breaker operations.
	DefaultTimeoutDuration = 10 * time.Second
	// DefaultMonitoringWindow is the default window for monitoring failures.
	DefaultMonitoringWindow = 60 * time.Second
)

func (s CircuitState) String() string {
	switch s {
	case CircuitClosed:
		return "CLOSED"
	case CircuitOpen:
		return "OPEN"
	case CircuitHalfOpen:
		return "HALF_OPEN"
	default:
		return "UNKNOWN"
	}
}

// CircuitBreakerConfig defines configuration for circuit breaker behavior.
type CircuitBreakerConfig struct {
	// FailureThreshold is the number of failures required to trip the circuit.
	FailureThreshold int `mapstructure:"failure_threshold"`

	// RecoveryTimeout is how long to wait before transitioning from OPEN to HALF_OPEN.
	RecoveryTimeout time.Duration `mapstructure:"recovery_timeout"`

	// SuccessThreshold is the number of successes required in HALF_OPEN to go to CLOSED.
	SuccessThreshold int `mapstructure:"success_threshold"`

	// TimeoutDuration is the maximum time to wait for a request.
	TimeoutDuration time.Duration `mapstructure:"timeout_duration"`

	// MonitoringWindow is the time window for monitoring failures.
	MonitoringWindow time.Duration `mapstructure:"monitoring_window"`
}

// DefaultCircuitBreakerConfig returns a default circuit breaker configuration.
func DefaultCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		FailureThreshold: DefaultFailureThreshold,
		RecoveryTimeout:  DefaultRecoveryTimeout,
		SuccessThreshold: DefaultSuccessThreshold,
		TimeoutDuration:  DefaultTimeoutDuration,
		MonitoringWindow: DefaultMonitoringWindow,
	}
}

// CircuitBreakerStats holds statistics about circuit breaker performance.
type CircuitBreakerStats struct {
	State                CircuitState
	FailureCount         int
	SuccessCount         int
	ConsecutiveFailures  int
	ConsecutiveSuccesses int
	TotalRequests        int64
	TotalFailures        int64
	TotalSuccesses       int64
	LastFailureTime      time.Time
	LastSuccessTime      time.Time
	StateChangedTime     time.Time
}

// failureRecord represents a single failure with timestamp.
type failureRecord struct {
	timestamp time.Time
	error     error
}

// CircuitBreaker implements the circuit breaker pattern for gateway connections.
type CircuitBreaker struct {
	config CircuitBreakerConfig
	client GatewayClient
	logger *zap.Logger

	// State management.
	state          CircuitState
	stateChangedAt time.Time
	mu             sync.RWMutex

	// Failure tracking.
	recentFailures       []failureRecord
	consecutiveFailures  int
	consecutiveSuccesses int

	// Statistics.
	totalRequests   int64
	totalFailures   int64
	totalSuccesses  int64
	lastFailureTime time.Time
	lastSuccessTime time.Time
}

// NewCircuitBreaker creates a new circuit breaker wrapping a gateway client.
func NewCircuitBreaker(client GatewayClient, config CircuitBreakerConfig, logger *zap.Logger) *CircuitBreaker {
	return &CircuitBreaker{
		config:         config,
		client:         client,
		logger:         logger,
		state:          CircuitClosed,
		stateChangedAt: time.Now(),
		recentFailures: make([]failureRecord, 0),
	}
}

// Connect establishes connection through the circuit breaker.
func (cb *CircuitBreaker) Connect(ctx context.Context) error {
	return cb.executeWithCircuitBreaker(ctx, "Connect", func(ctx context.Context) error {
		return cb.client.Connect(ctx)
	})
}

// SendRequest sends a request through the circuit breaker.
func (cb *CircuitBreaker) SendRequest(ctx context.Context, req *mcp.Request) error {
	ctx, cancel := context.WithTimeout(ctx, cb.config.TimeoutDuration)
	defer cancel()

	return cb.executeWithCircuitBreaker(ctx, "SendRequest", func(ctx context.Context) error {
		return cb.client.SendRequest(ctx, req)
	})
}

// ReceiveResponse receives a response through the circuit breaker.
func (cb *CircuitBreaker) ReceiveResponse() (*mcp.Response, error) {
	ctx, cancel := context.WithTimeout(context.Background(), cb.config.TimeoutDuration)
	defer cancel()

	var result *mcp.Response

	err := cb.executeWithCircuitBreaker(ctx, "ReceiveResponse", func(ctx context.Context) error {
		var err error

		result, err = cb.client.ReceiveResponse()

		return err
	})

	return result, err
}

// SendPing sends a ping through the circuit breaker.
func (cb *CircuitBreaker) SendPing() error {
	ctx, cancel := context.WithTimeout(context.Background(), cb.config.TimeoutDuration)
	defer cancel()

	return cb.executeWithCircuitBreaker(ctx, "SendPing", func(ctx context.Context) error {
		return cb.client.SendPing()
	})
}

// IsConnected checks connection status through the circuit breaker.
func (cb *CircuitBreaker) IsConnected() bool {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	// If circuit is open, assume not connected.
	if cb.state == CircuitOpen {
		return false
	}

	return cb.client.IsConnected()
}

// Close closes the underlying client.
func (cb *CircuitBreaker) Close() error {
	return cb.client.Close()
}

// executeWithCircuitBreaker executes a function with circuit breaker logic.
func (cb *CircuitBreaker) executeWithCircuitBreaker(
	ctx context.Context,
	operation string,
	fn func(context.Context) error,
) error {
	// Check if request should be allowed.
	if !cb.allowRequest() {
		cb.recordFailure(errors.New("circuit breaker is OPEN"))

		return fmt.Errorf("circuit breaker is OPEN, rejecting %s request", operation)
	}

	// Execute the request with timeout.
	done := make(chan error, 1)

	go func() {
		done <- fn(ctx)
	}()

	select {
	case err := <-done:
		if err != nil {
			cb.recordFailure(err)

			return err
		}

		cb.recordSuccess()

		return nil
	case <-ctx.Done():
		cb.recordFailure(ctx.Err())

		return fmt.Errorf("%s request timed out: %w", operation, ctx.Err())
	}
}

// allowRequest determines if a request should be allowed based on circuit state.
func (cb *CircuitBreaker) allowRequest() bool {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	switch cb.state {
	case CircuitClosed:
		return true
	case CircuitOpen:
		// Check if recovery timeout has passed.
		if time.Since(cb.stateChangedAt) >= cb.config.RecoveryTimeout {
			// Transition to half-open (this will be done in recordSuccess/recordFailure).
			return true
		}

		return false
	case CircuitHalfOpen:
		// Allow limited requests to test recovery.
		return true
	default:
		return false
	}
}

// recordSuccess records a successful operation and updates circuit state.
func (cb *CircuitBreaker) recordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.totalRequests++
	cb.totalSuccesses++
	cb.consecutiveFailures = 0
	cb.consecutiveSuccesses++
	cb.lastSuccessTime = time.Now()

	// State transitions based on success.
	switch cb.state {
	case CircuitHalfOpen:
		// Check if we have enough successes to close the circuit.
		if cb.consecutiveSuccesses >= cb.config.SuccessThreshold {
			cb.setState(CircuitClosed)
			cb.logger.Info("Circuit breaker transitioned to CLOSED after recovery",
				zap.Int("consecutive_successes", cb.consecutiveSuccesses))
		}
	case CircuitOpen:
		// Should not happen if allowRequest is working correctly.
		if time.Since(cb.stateChangedAt) >= cb.config.RecoveryTimeout {
			cb.setState(CircuitHalfOpen)
			cb.logger.Info("Circuit breaker transitioned to HALF_OPEN for recovery test")
		}
	case CircuitClosed:
		// Already closed, no action needed on success.
	}
}

// recordFailure records a failed operation and updates circuit state.
func (cb *CircuitBreaker) recordFailure(err error) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.totalRequests++
	cb.totalFailures++
	cb.consecutiveSuccesses = 0
	cb.consecutiveFailures++
	cb.lastFailureTime = time.Now()

	// Add to recent failures for monitoring window.
	cb.recentFailures = append(cb.recentFailures, failureRecord{
		timestamp: time.Now(),
		error:     err,
	})

	// Clean up old failures outside monitoring window.
	cb.cleanupOldFailures()

	// State transitions based on failure.
	switch cb.state {
	case CircuitClosed:
		// Check if we should trip the circuit.
		if cb.consecutiveFailures >= cb.config.FailureThreshold {
			cb.setState(CircuitOpen)
			cb.logger.Warn("Circuit breaker tripped to OPEN",
				zap.Int("consecutive_failures", cb.consecutiveFailures),
				zap.Int("failure_threshold", cb.config.FailureThreshold),
				zap.Error(err))
		}
	case CircuitHalfOpen:
		// Any failure in half-open state should trip the circuit back to open.
		cb.setState(CircuitOpen)
		cb.logger.Warn("Circuit breaker returned to OPEN after failed recovery test",
			zap.Error(err))
	case CircuitOpen:
		// Already open, continue accumulating failures.
	}
}

// setState changes the circuit breaker state and logs the transition.
func (cb *CircuitBreaker) setState(newState CircuitState) {
	oldState := cb.state
	cb.state = newState
	cb.stateChangedAt = time.Now()

	if oldState != newState {
		cb.logger.Info("Circuit breaker state changed",
			zap.String("old_state", oldState.String()),
			zap.String("new_state", newState.String()),
			zap.Int("consecutive_failures", cb.consecutiveFailures),
			zap.Int("consecutive_successes", cb.consecutiveSuccesses))
	}
}

// cleanupOldFailures removes failures outside the monitoring window.
func (cb *CircuitBreaker) cleanupOldFailures() {
	cutoff := time.Now().Add(-cb.config.MonitoringWindow)
	newFailures := make([]failureRecord, 0)

	for _, failure := range cb.recentFailures {
		if failure.timestamp.After(cutoff) {
			newFailures = append(newFailures, failure)
		}
	}

	cb.recentFailures = newFailures
}

// GetStats returns current circuit breaker statistics.
func (cb *CircuitBreaker) GetStats() CircuitBreakerStats {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	return CircuitBreakerStats{
		State:                cb.state,
		FailureCount:         len(cb.recentFailures),
		SuccessCount:         int(cb.totalSuccesses),
		ConsecutiveFailures:  cb.consecutiveFailures,
		ConsecutiveSuccesses: cb.consecutiveSuccesses,
		TotalRequests:        cb.totalRequests,
		TotalFailures:        cb.totalFailures,
		TotalSuccesses:       cb.totalSuccesses,
		LastFailureTime:      cb.lastFailureTime,
		LastSuccessTime:      cb.lastSuccessTime,
		StateChangedTime:     cb.stateChangedAt,
	}
}

// ForceOpen forces the circuit breaker to the OPEN state.
func (cb *CircuitBreaker) ForceOpen() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.setState(CircuitOpen)
	cb.logger.Warn("Circuit breaker forced to OPEN state")
}

// ForceClose forces the circuit breaker to the CLOSED state.
func (cb *CircuitBreaker) ForceClose() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.setState(CircuitClosed)
	cb.consecutiveFailures = 0
	cb.consecutiveSuccesses = 0
	cb.logger.Info("Circuit breaker forced to CLOSED state")
}

// ForceFail simulates a failure for testing purposes.
func (cb *CircuitBreaker) ForceFail(err error) {
	cb.recordFailure(err)
}
