// Package circuit provides circuit breaker pattern implementation for handling failures gracefully.
package circuit

import (
	"errors"
	"sync"
	"time"
)

// State represents the circuit breaker state.
type State int

const (
	// StateClosed indicates the circuit breaker is closed and requests are allowed through.
	StateClosed State = iota
	// StateOpen indicates the circuit breaker is open and requests are rejected.
	StateOpen
	// StateHalfOpen indicates the circuit breaker is half-open and testing if requests should be allowed.
	StateHalfOpen
)

// String returns the string representation of the state.
func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// CircuitBreaker implements the circuit breaker pattern.
type CircuitBreaker struct {
	maxFailures      int
	successThreshold int
	timeout          time.Duration

	mu              sync.Mutex
	state           State
	failures        int
	successes       int
	lastFailureTime time.Time
	lastStateChange time.Time

	// Auto-recovery support
	stopCh chan struct{}
	doneCh chan struct{}
}

// NewCircuitBreaker creates a new circuit breaker.
func NewCircuitBreaker(maxFailures, successThreshold int, timeout time.Duration) *CircuitBreaker {
	cb := &CircuitBreaker{
		maxFailures:      maxFailures,
		successThreshold: successThreshold,
		timeout:          timeout,
		state:            StateClosed,
		lastStateChange:  time.Now(),
		stopCh:           make(chan struct{}),
		doneCh:           make(chan struct{}),
	}

	// Start background auto-recovery goroutine
	go cb.autoRecovery()

	return cb
}

// autoRecovery periodically checks if the circuit should transition from Open to Half-Open.
// This enables active recovery without waiting for incoming requests.
func (cb *CircuitBreaker) autoRecovery() {
	defer close(cb.doneCh)

	// Check every 100ms for potential state transitions
	const autoRecoveryCheckInterval = 100 * time.Millisecond
	ticker := time.NewTicker(autoRecoveryCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-cb.stopCh:
			return
		case <-ticker.C:
			cb.checkAndTransitionToHalfOpen()
		}
	}
}

// checkAndTransitionToHalfOpen checks if enough time has passed to transition from Open to Half-Open.
func (cb *CircuitBreaker) checkAndTransitionToHalfOpen() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.state == StateOpen && time.Since(cb.lastFailureTime) > cb.timeout {
		cb.setState(StateHalfOpen)
		cb.successes = 0
	}
}

// Call executes the function through the circuit breaker.
func (cb *CircuitBreaker) Call(fn func() error) error {
	cb.mu.Lock()

	// Fail fast when circuit is open - the background goroutine will handle recovery
	if cb.state == StateOpen {
		cb.mu.Unlock()

		return errors.New("circuit breaker is open (failing fast)")
	}

	cb.mu.Unlock()

	// Execute the function with shorter context timeout for faster error detection
	err := fn()

	cb.mu.Lock()
	defer cb.mu.Unlock()

	if err != nil {
		cb.recordFailure()
	} else {
		cb.recordSuccess()
	}

	return err
}

// recordFailure records a failure and potentially opens the circuit.
func (cb *CircuitBreaker) recordFailure() {
	cb.failures++
	cb.lastFailureTime = time.Now()

	switch cb.state {
	case StateClosed:
		if cb.failures >= cb.maxFailures {
			cb.setState(StateOpen)
			cb.failures = 0
		}
	case StateHalfOpen:
		// Any failure in half-open state reopens the circuit
		cb.setState(StateOpen)
		cb.failures = 0
	case StateOpen:
	}
}

// recordSuccess records a success and potentially closes the circuit.
func (cb *CircuitBreaker) recordSuccess() {
	cb.failures = 0

	switch cb.state {
	case StateHalfOpen:
		cb.successes++
		if cb.successes >= cb.successThreshold {
			cb.setState(StateClosed)
			cb.successes = 0
		}
	case StateClosed:
	case StateOpen:
	}
}

// setState changes the circuit breaker state.
func (cb *CircuitBreaker) setState(state State) {
	cb.state = state
	cb.lastStateChange = time.Now()
}

// GetState returns the current state.
func (cb *CircuitBreaker) GetState() State {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	return cb.state
}

// GetStateFloat returns the state as a float64 for metrics.
func (cb *CircuitBreaker) GetStateFloat() float64 {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	return float64(cb.state)
}

// IsOpen returns true if the circuit is open.
func (cb *CircuitBreaker) IsOpen() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	return cb.state == StateOpen
}

// Reset resets the circuit breaker to closed state.
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.state = StateClosed
	cb.failures = 0
	cb.successes = 0
	cb.lastStateChange = time.Now()
}

// Close stops the background auto-recovery goroutine and cleans up resources.
// This should be called when the circuit breaker is no longer needed.
func (cb *CircuitBreaker) Close() {
	close(cb.stopCh)
	<-cb.doneCh
}
