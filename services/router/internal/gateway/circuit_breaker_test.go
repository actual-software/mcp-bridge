package gateway

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/poiley/mcp-bridge/services/router/pkg/mcp"
)



// MockGatewayClient implements GatewayClient for testing.
type MockGatewayClient struct {
	connectErr     error
	sendRequestErr error
	receiveErr     error
	pingErr        error
	isConnected    bool
	callCounts     map[string]int
	shouldBlock    bool
	blockDuration  time.Duration
}

func NewMockGatewayClient() *MockGatewayClient {
	return &MockGatewayClient{
		callCounts: make(map[string]int),
	}
}

func (m *MockGatewayClient) Connect(ctx context.Context) error {
	m.callCounts["Connect"]++
	if m.shouldBlock {
		time.Sleep(m.blockDuration)
	}

	return m.connectErr
}

func (m *MockGatewayClient) SendRequest(req *mcp.Request) error {
	m.callCounts["SendRequest"]++
	if m.shouldBlock {
		time.Sleep(m.blockDuration)
	}

	return m.sendRequestErr
}

func (m *MockGatewayClient) ReceiveResponse() (*mcp.Response, error) {
	m.callCounts["ReceiveResponse"]++
	if m.shouldBlock {
		time.Sleep(m.blockDuration)
	}

	if m.receiveErr != nil {
		return nil, m.receiveErr
	}

	return &mcp.Response{ID: "test"}, nil
}

func (m *MockGatewayClient) SendPing() error {
	m.callCounts["SendPing"]++
	if m.shouldBlock {
		time.Sleep(m.blockDuration)
	}

	return m.pingErr
}

func (m *MockGatewayClient) IsConnected() bool {
	m.callCounts["IsConnected"]++

	return m.isConnected
}

func (m *MockGatewayClient) Close() error {
	m.callCounts["Close"]++

	return nil
}

func TestNewCircuitBreaker(t *testing.T) { 
	client := NewMockGatewayClient()
	config := CircuitBreakerConfig{
		FailureThreshold: 5,
		RecoveryTimeout:  30 * time.Second,
		SuccessThreshold: 3,
		TimeoutDuration:  10 * time.Second,
		MonitoringWindow: 60 * time.Second,
	}
	logger := zap.NewNop()

	cb := NewCircuitBreaker(client, config, logger)

	if cb == nil {
		t.Fatal("Expected non-nil circuit breaker")
	}

	if cb.state != CircuitClosed {
		t.Errorf("Expected initial state CLOSED, got %v", cb.state)
	}

	stats := cb.GetStats()
	if stats.State != CircuitClosed {
		t.Errorf("Expected stats state CLOSED, got %v", stats.State)
	}
}

func TestCircuitBreaker_SuccessfulOperations(t *testing.T) { 
	client := NewMockGatewayClient()
	client.isConnected = true

	config := CircuitBreakerConfig{
		FailureThreshold: 3,
		RecoveryTimeout:  1 * time.Second,
		SuccessThreshold: 2,
		TimeoutDuration:  1 * time.Second,
		MonitoringWindow: 60 * time.Second,
	}
	logger := zap.NewNop()

	cb := NewCircuitBreaker(client, config, logger)

	// Test successful operations.
	ctx := context.Background()

	// Connect.
	err := cb.Connect(ctx)
	if err != nil {
		t.Errorf("Expected successful connect, got error: %v", err)
	}

	// Send request.
	req := &mcp.Request{ID: "test", Method: "test"}

	err = cb.SendRequest(req)
	if err != nil {
		t.Errorf("Expected successful SendRequest, got error: %v", err)
	}

	// Receive response.
	resp, err := cb.ReceiveResponse()
	if err != nil {
		t.Errorf("Expected successful ReceiveResponse, got error: %v", err)
	}

	if resp == nil {
		t.Error("Expected non-nil response")
	}

	// Send ping.
	err = cb.SendPing()
	if err != nil {
		t.Errorf("Expected successful SendPing, got error: %v", err)
	}

	// Check IsConnected.
	connected := cb.IsConnected()
	if !connected {
		t.Error("Expected IsConnected to return true")
	}

	// Verify stats.
	stats := cb.GetStats()
	if stats.State != CircuitClosed {
		t.Errorf("Expected state CLOSED, got %v", stats.State)
	}

	if stats.TotalSuccesses == 0 {
		t.Error("Expected some successful operations")
	}
}

func TestCircuitBreaker_FailureHandling(t *testing.T) { 
	client := NewMockGatewayClient()
	testErr := errors.New("test error")
	client.connectErr = testErr
	client.sendRequestErr = testErr
	client.receiveErr = testErr
	client.pingErr = testErr

	config := CircuitBreakerConfig{
		FailureThreshold: 2, // Trip after 2 failures
		RecoveryTimeout:  testIterations * time.Millisecond,
		SuccessThreshold: 1,
		TimeoutDuration:  testIterations * time.Millisecond,
		MonitoringWindow: 60 * time.Second,
	}
	logger := zap.NewNop()

	cb := NewCircuitBreaker(client, config, logger)
	ctx := context.Background()

	// First failure.
	err := cb.Connect(ctx)
	if err == nil {
		t.Error("Expected error from Connect")
	}

	// Second failure should trip the circuit.
	err = cb.SendRequest(&mcp.Request{ID: "test"})
	if err == nil {
		t.Error("Expected error from SendRequest")
	}

	// Circuit should now be OPEN.
	stats := cb.GetStats()
	if stats.State != CircuitOpen {
		t.Errorf("Expected state OPEN after failures, got %v", stats.State)
	}

	// Next request should be rejected immediately.
	err = cb.SendPing()
	if err == nil {
		t.Error("Expected circuit breaker to reject request")
	}

	if err != nil && err.Error() != "circuit breaker is OPEN, rejecting SendPing request" {
		t.Errorf("Expected circuit breaker rejection error, got: %v", err)
	}
}

func TestCircuitBreaker_Recovery(t *testing.T) { 
	client := NewMockGatewayClient()
	testErr := errors.New("test error")
	client.sendRequestErr = testErr

	config := CircuitBreakerConfig{
		FailureThreshold: 2,
		RecoveryTimeout:  testTimeout * time.Millisecond, // Short recovery time for testing
		SuccessThreshold: 1,
		TimeoutDuration:  testIterations * time.Millisecond,
		MonitoringWindow: 60 * time.Second,
	}
	logger := zap.NewNop()

	cb := NewCircuitBreaker(client, config, logger)

	// Cause failures to trip the circuit.
	_ = cb.SendRequest(&mcp.Request{ID: "test1"})
	_ = cb.SendRequest(&mcp.Request{ID: "test2"})

	// Verify circuit is open.
	stats := cb.GetStats()
	if stats.State != CircuitOpen {
		t.Errorf("Expected state OPEN, got %v", stats.State)
	}

	// Wait for recovery timeout.
	time.Sleep(60 * time.Millisecond)

	// Fix the client.
	client.sendRequestErr = nil

	// Next request should be allowed (circuit goes to HALF_OPEN).
	err := cb.SendRequest(&mcp.Request{ID: "test3"})
	if err != nil {
		t.Errorf("Expected successful request after recovery, got: %v", err)
	}

	// Circuit should now be CLOSED or HALF_OPEN (depending on timing).
	stats = cb.GetStats()
	if stats.State != CircuitClosed && stats.State != CircuitHalfOpen {
		t.Errorf("Expected state CLOSED or HALF_OPEN after recovery, got %v", stats.State)
	}

	// If still HALF_OPEN, send another successful request to close it.
	if stats.State == CircuitHalfOpen {
		err = cb.SendRequest(&mcp.Request{ID: "test4"})
		if err != nil {
			t.Errorf("Expected second successful request after recovery, got: %v", err)
		}

		// Now it should be CLOSED.
		stats = cb.GetStats()
		if stats.State != CircuitClosed {
			t.Errorf("Expected state CLOSED after second successful request, got %v", stats.State)
		}
	}
}

func TestCircuitBreaker_Timeout(t *testing.T) { 
	client := NewMockGatewayClient()
	client.shouldBlock = true
	client.blockDuration = httpStatusOK * time.Millisecond

	config := CircuitBreakerConfig{
		FailureThreshold: 3,
		RecoveryTimeout:  1 * time.Second,
		SuccessThreshold: 2,
		TimeoutDuration:  testTimeout * time.Millisecond, // Short timeout
		MonitoringWindow: 60 * time.Second,
	}
	logger := zap.NewNop()

	cb := NewCircuitBreaker(client, config, logger)

	// This should timeout.
	start := time.Now()
	err := cb.SendRequest(&mcp.Request{ID: "test"})
	duration := time.Since(start)

	if err == nil {
		t.Error("Expected timeout error")
	}

	if duration < config.TimeoutDuration {
		t.Errorf("Expected operation to take at least %v, took %v", config.TimeoutDuration, duration)
	}

	// Should be treated as a failure.
	stats := cb.GetStats()
	if stats.TotalFailures == 0 {
		t.Error("Expected timeout to be counted as failure")
	}
}

func TestCircuitBreaker_ForceStateChanges(t *testing.T) { 
	client := NewMockGatewayClient()
	config := DefaultCircuitBreakerConfig()
	logger := zap.NewNop()

	cb := NewCircuitBreaker(client, config, logger)

	// Initially closed.
	if cb.GetStats().State != CircuitClosed {
		t.Error("Expected initial state CLOSED")
	}

	// Force open.
	cb.ForceOpen()

	if cb.GetStats().State != CircuitOpen {
		t.Error("Expected state OPEN after ForceOpen")
	}

	// Force close.
	cb.ForceClose()

	if cb.GetStats().State != CircuitClosed {
		t.Error("Expected state CLOSED after ForceClose")
	}

	// After ForceClose, consecutive failure counts should be reset.
	stats := cb.GetStats()
	if stats.ConsecutiveFailures != 0 {
		t.Errorf("Expected 0 consecutive failures after ForceClose, got %d", stats.ConsecutiveFailures)
	}
}

func TestCircuitBreaker_IsConnected(t *testing.T) { 
	client := NewMockGatewayClient()
	config := DefaultCircuitBreakerConfig()
	logger := zap.NewNop()

	cb := NewCircuitBreaker(client, config, logger)

	// When circuit is closed, should delegate to client.
	client.isConnected = true

	if !cb.IsConnected() {
		t.Error("Expected IsConnected to return true when circuit closed and client connected")
	}

	client.isConnected = false

	if cb.IsConnected() {
		t.Error("Expected IsConnected to return false when circuit closed and client disconnected")
	}

	// When circuit is open, should return false regardless of client.
	cb.ForceOpen()

	client.isConnected = true

	if cb.IsConnected() {
		t.Error("Expected IsConnected to return false when circuit is open")
	}
}

func TestCircuitBreaker_ForceFail(t *testing.T) { 
	client := NewMockGatewayClient()
	config := CircuitBreakerConfig{
		FailureThreshold: 2,
		RecoveryTimeout:  1 * time.Second,
		SuccessThreshold: 1,
		TimeoutDuration:  1 * time.Second,
		MonitoringWindow: 60 * time.Second,
	}
	logger := zap.NewNop()

	cb := NewCircuitBreaker(client, config, logger)

	// Force failures to trip circuit.
	testErr := errors.New("forced failure")
	cb.ForceFail(testErr)
	cb.ForceFail(testErr)

	// Circuit should be open.
	stats := cb.GetStats()
	if stats.State != CircuitOpen {
		t.Errorf("Expected state OPEN after forced failures, got %v", stats.State)
	}

	if stats.ConsecutiveFailures != 2 {
		t.Errorf("Expected 2 consecutive failures, got %d", stats.ConsecutiveFailures)
	}
}
