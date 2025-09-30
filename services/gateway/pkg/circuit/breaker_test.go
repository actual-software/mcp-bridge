package circuit

import (
	"errors"
	"math"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	testIterations    = 100
	testMaxIterations = 1000
	httpStatusOK      = 200
	testTimeout       = 50
)

func TestState_String(t *testing.T) {
	tests := []struct {
		state    State
		expected string
	}{
		{StateClosed, "closed"},
		{StateOpen, "open"},
		{StateHalfOpen, "half-open"},
		{State(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.state.String(); got != tt.expected {
				t.Errorf("State.String() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestNewCircuitBreaker(t *testing.T) {
	cb := NewCircuitBreaker(3, 2, 5*time.Second)

	if cb == nil {
		t.Fatal("Expected circuit breaker to be created")
	}

	if cb.maxFailures != 3 {
		t.Errorf("Expected maxFailures=3, got %d", cb.maxFailures)
	}

	if cb.successThreshold != 2 {
		t.Errorf("Expected successThreshold=2, got %d", cb.successThreshold)
	}

	if cb.timeout != 5*time.Second {
		t.Errorf("Expected timeout=5s, got %v", cb.timeout)
	}

	if cb.state != StateClosed {
		t.Errorf("Expected initial state to be closed, got %s", cb.state)
	}
}

func TestCircuitBreaker_Call_Success(t *testing.T) {
	cb := NewCircuitBreaker(3, 2, time.Second)

	// Successful call
	err := cb.Call(func() error {
		return nil
	})
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if cb.GetState() != StateClosed {
		t.Errorf("Expected state to remain closed, got %s", cb.GetState())
	}
}

func TestCircuitBreaker_Call_Failures(t *testing.T) {
	cb := NewCircuitBreaker(3, 2, time.Second)
	testErr := errors.New("test error")

	// First failure
	err := cb.Call(func() error {
		return testErr
	})
	if !errors.Is(err, testErr) {
		t.Errorf("Expected test error, got %v", err)
	}

	if cb.GetState() != StateClosed {
		t.Errorf("Expected state to remain closed after 1 failure, got %s", cb.GetState())
	}

	// Second failure
	_ = cb.Call(func() error {
		return testErr
	})

	if cb.GetState() != StateClosed {
		t.Errorf("Expected state to remain closed after 2 failures, got %s", cb.GetState())
	}

	// Third failure - should open circuit
	_ = cb.Call(func() error {
		return testErr
	})

	if cb.GetState() != StateOpen {
		t.Errorf("Expected state to be open after 3 failures, got %s", cb.GetState())
	}

	// Call while open should fail immediately
	err = cb.Call(func() error {
		t.Error("Function should not be called when circuit is open")

		return nil
	})
	if err == nil || err.Error() != "circuit breaker is open (failing fast)" {
		t.Errorf("Expected circuit breaker open error, got %v", err)
	}
}

func TestCircuitBreaker_HalfOpen(t *testing.T) {
	cb := NewCircuitBreaker(2, 2, testTimeout*time.Millisecond)
	testErr := errors.New("test error")

	// Open the circuit
	_ = cb.Call(func() error { return testErr })
	_ = cb.Call(func() error { return testErr })

	if cb.GetState() != StateOpen {
		t.Fatal("Expected circuit to be open")
	}

	// Wait for the circuit timeout to expire
	// Instead of sleeping, wait just a bit longer than the timeout
	waitTime := cb.timeout + 10*time.Millisecond

	timer := time.NewTimer(waitTime)
	defer timer.Stop()

	select {
	case <-timer.C:
		// Circuit should now be ready to transition to half-open
	case <-time.After(httpStatusOK * time.Millisecond):
		t.Fatal("Timeout waiting for circuit breaker timeout")
	}
	// Next call should transition to half-open and execute
	called := false
	err := cb.Call(func() error {
		called = true

		return nil
	})

	if !called {
		t.Error("Expected function to be called in half-open state")
	}

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if cb.GetState() != StateHalfOpen {
		t.Errorf("Expected state to be half-open, got %s", cb.GetState())
	}

	// One more success should close the circuit
	_ = cb.Call(func() error { return nil })

	if cb.GetState() != StateClosed {
		t.Errorf("Expected state to be closed after 2 successes, got %s", cb.GetState())
	}
}

func TestCircuitBreaker_HalfOpen_Failure(t *testing.T) {
	cb := NewCircuitBreaker(2, 2, testTimeout*time.Millisecond)
	testErr := errors.New("test error")

	// Open the circuit
	_ = cb.Call(func() error { return testErr })
	_ = cb.Call(func() error { return testErr })

	// Wait for the circuit timeout to expire
	waitTime := cb.timeout + 10*time.Millisecond

	timer := time.NewTimer(waitTime)
	defer timer.Stop()

	<-timer.C

	// Failure in half-open should reopen immediately
	_ = cb.Call(func() error { return testErr })

	if cb.GetState() != StateOpen {
		t.Errorf("Expected state to be open after failure in half-open, got %s", cb.GetState())
	}
}

func TestCircuitBreaker_Reset(t *testing.T) {
	cb := NewCircuitBreaker(2, 2, time.Second)
	testErr := errors.New("test error")

	// Open the circuit
	_ = cb.Call(func() error { return testErr })
	_ = cb.Call(func() error { return testErr })

	if cb.GetState() != StateOpen {
		t.Fatal("Expected circuit to be open")
	}

	// Reset
	cb.Reset()

	if cb.GetState() != StateClosed {
		t.Errorf("Expected state to be closed after reset, got %s", cb.GetState())
	}

	if cb.failures != 0 {
		t.Errorf("Expected failures to be 0 after reset, got %d", cb.failures)
	}

	if cb.successes != 0 {
		t.Errorf("Expected successes to be 0 after reset, got %d", cb.successes)
	}
}

func TestCircuitBreaker_GetStateFloat(t *testing.T) {
	cb := NewCircuitBreaker(2, 2, time.Second)

	tests := []struct {
		setState State
		expected float64
	}{
		{StateClosed, 0.0},
		{StateOpen, 1.0},
		{StateHalfOpen, 2.0},
	}

	for _, tt := range tests {
		t.Run(tt.setState.String(), func(t *testing.T) {
			cb.setState(tt.setState)

			if got := cb.GetStateFloat(); got != tt.expected {
				t.Errorf("GetStateFloat() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestCircuitBreaker_IsOpen(t *testing.T) {
	cb := NewCircuitBreaker(2, 2, time.Second)

	if cb.IsOpen() {
		t.Error("Expected circuit to not be open initially")
	}

	// Open the circuit
	testErr := errors.New("test error")

	_ = cb.Call(func() error { return testErr })
	_ = cb.Call(func() error { return testErr })

	if !cb.IsOpen() {
		t.Error("Expected circuit to be open after failures")
	}
}

func TestCircuitBreaker_ConcurrentCalls(t *testing.T) {
	cb := NewCircuitBreaker(10, 5, time.Second)

	var wg sync.WaitGroup

	var successCount, failureCount int32

	numGoroutines := testIterations
	callsPerGoroutine := testIterations

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)

		go func(id int) {
			defer wg.Done()

			for j := 0; j < callsPerGoroutine; j++ {
				err := cb.Call(func() error {
					// Simulate 30% failure rate
					if (id+j)%10 < 3 {
						return errors.New("simulated error")
					}

					return nil
				})
				if err != nil {
					atomic.AddInt32(&failureCount, 1)
				} else {
					atomic.AddInt32(&successCount, 1)
				}
			}
		}(i)
	}

	wg.Wait()

	// Validate bounds before conversion to prevent overflow
	totalCallsInt := numGoroutines * callsPerGoroutine
	require.LessOrEqual(t, totalCallsInt, math.MaxInt32, "Total calls must fit in int32")
	// #nosec G115 - validated above that totalCallsInt fits in int32
	totalCalls := int32(totalCallsInt)
	actualCalls := successCount + failureCount

	// Due to circuit breaker, actual calls might be less than total attempts
	if actualCalls > totalCalls {
		t.Errorf("Actual calls (%d) exceeded total calls (%d)", actualCalls, totalCalls)
	}

	t.Logf("Total attempts: %d, Successes: %d, Failures: %d, Blocked: %d",
		totalCalls, successCount, failureCount, totalCalls-actualCalls)
}

func TestCircuitBreaker_StateTransitions(t *testing.T) {
	cb := NewCircuitBreaker(2, 2, testTimeout*time.Millisecond)

	// Track state changes
	states := []State{}
	checkState := createStateChecker(t, cb, &states)

	// Test complete state transition lifecycle
	testClosedStateTransitions(cb, checkState)
	testOpenStateTransitions(t, cb, checkState)
	testHalfOpenStateTransitions(cb, checkState)
}

func createStateChecker(t *testing.T, cb *CircuitBreaker, states *[]State) func(State) {
	t.Helper()

	return func(expected State) {
		state := cb.GetState()
		*states = append(*states, state)

		if state != expected {
			t.Errorf("Expected state %s, got %s. State history: %v",
				expected, state, *states)
		}
	}
}

func testClosedStateTransitions(cb *CircuitBreaker, checkState func(State)) {
	// Initial state
	checkState(StateClosed)

	// Success in closed state
	_ = cb.Call(func() error { return nil })

	checkState(StateClosed)

	// First failure
	_ = cb.Call(func() error { return errors.New("error") })

	checkState(StateClosed)

	// Second failure - opens circuit
	_ = cb.Call(func() error { return errors.New("error") })

	checkState(StateOpen)
}

func testOpenStateTransitions(t *testing.T, cb *CircuitBreaker, checkState func(State)) {
	t.Helper()
	// Call while open
	err := cb.Call(func() error { return nil })
	if err == nil {
		t.Error("Expected error when calling open circuit")
	}

	checkState(StateOpen)

	// Wait for the circuit timeout to expire
	waitTime := cb.timeout + 10*time.Millisecond

	timer := time.NewTimer(waitTime)
	defer timer.Stop()

	<-timer.C
}

func testHalfOpenStateTransitions(cb *CircuitBreaker, checkState func(State)) {
	// Success transitions to half-open
	_ = cb.Call(func() error { return nil })

	checkState(StateHalfOpen)

	// Another success closes circuit
	_ = cb.Call(func() error { return nil })

	checkState(StateClosed)
}

func BenchmarkCircuitBreaker_Call_Closed(b *testing.B) {
	cb := NewCircuitBreaker(testMaxIterations, 10, time.Minute)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		err := cb.Call(func() error {
			return nil
		})
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCircuitBreaker_Call_Open(b *testing.B) {
	cb := NewCircuitBreaker(1, 10, time.Hour)

	// Open the circuit
	_ = cb.Call(func() error { return errors.New("error") })

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = cb.Call(func() error {
			return nil
		})
	}
}

func BenchmarkCircuitBreaker_ConcurrentCalls(b *testing.B) {
	cb := NewCircuitBreaker(testIterations, 10, time.Second)

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = cb.Call(func() error {
				return nil
			})
		}
	})
}
