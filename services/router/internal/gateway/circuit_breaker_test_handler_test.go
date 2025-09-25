package gateway

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync/atomic"
	"testing"

	common "github.com/poiley/mcp-bridge/pkg/common/config"
	"github.com/poiley/mcp-bridge/services/router/internal/config"
	"github.com/poiley/mcp-bridge/services/router/internal/constants"
	"github.com/poiley/mcp-bridge/services/router/pkg/mcp"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

// CircuitBreakerTestHandler handles circuit breaker testing scenarios.
type CircuitBreakerTestHandler struct {
	t                     *testing.T
	logger                *zap.Logger
	failuresBeforeSuccess int
	totalAttempts         int
	failureCount          *int64
	successCount          *int64
}

// CreateCircuitBreakerTestHandler creates a new circuit breaker test handler.
func CreateCircuitBreakerTestHandler(t *testing.T) *CircuitBreakerTestHandler {
	t.Helper()

	var failureCount, successCount int64

	return &CircuitBreakerTestHandler{
		t:                     t,
		logger:                zaptest.NewLogger(t),
		failuresBeforeSuccess: 5,
		totalAttempts:         10,
		failureCount:          &failureCount,
		successCount:          &successCount,
	}
}

// ExecuteTest runs the complete circuit breaker test scenario.
func (h *CircuitBreakerTestHandler) ExecuteTest() {
	server := h.createMockServer()

	addr := h.startServer(server)
	defer server.Stop()

	cfg := h.createConfig(addr)
	successfulRequests := h.runAttempts(cfg)

	h.validateResults(successfulRequests)
}

// createMockServer creates the mock TCP server with circuit breaker behavior.
func (h *CircuitBreakerTestHandler) createMockServer() *mockTCPServerBench {
	return newMockTCPServerBench(h.t, func(conn net.Conn) {
		handler := &circuitBreakerConnectionHandler{
			conn:                  conn,
			failuresBeforeSuccess: h.failuresBeforeSuccess,
			failureCount:          h.failureCount,
			successCount:          h.successCount,
		}
		handler.handle()
	})
}

// startServer starts the mock server and returns its address.
func (h *CircuitBreakerTestHandler) startServer(server *mockTCPServerBench) string {
	addr, err := server.Start()
	if err != nil {
		h.t.Fatalf("Failed to start server: %v", err)
	}

	return addr
}

// createConfig creates the gateway configuration.
func (h *CircuitBreakerTestHandler) createConfig(addr string) config.GatewayConfig {
	return config.GatewayConfig{
		URL: "tcp://" + addr,
		Connection: common.ConnectionConfig{
			TimeoutMs: 2000,
		},
	}
}

// runAttempts executes all connection attempts.
func (h *CircuitBreakerTestHandler) runAttempts(cfg config.GatewayConfig) int {
	successfulRequests := 0

	for i := 0; i < h.totalAttempts; i++ {
		attempt := &connectionAttempt{
			handler: h,
			cfg:     cfg,
			index:   i,
		}

		if attempt.execute() {
			successfulRequests++
		}
	}

	return successfulRequests
}

// validateResults validates the test results.
func (h *CircuitBreakerTestHandler) validateResults(successfulRequests int) {
	validator := &circuitBreakerValidator{
		t:                  h.t,
		failureCount:       atomic.LoadInt64(h.failureCount),
		successCount:       atomic.LoadInt64(h.successCount),
		successfulRequests: successfulRequests,
		expectedFailures:   int64(h.failuresBeforeSuccess),
		totalAttempts:      h.totalAttempts,
	}
	validator.validate()
}

// circuitBreakerConnectionHandler handles individual connections.
type circuitBreakerConnectionHandler struct {
	conn                  net.Conn
	failuresBeforeSuccess int
	failureCount          *int64
	successCount          *int64
}

func (h *circuitBreakerConnectionHandler) handle() {
	reader := bufio.NewReader(h.conn)

	if !h.handleVersionNegotiation(reader) {
		return
	}

	h.handleRequests(reader)
}

func (h *circuitBreakerConnectionHandler) handleVersionNegotiation(reader *bufio.Reader) bool {
	frame, err := ReadBinaryFrame(reader)
	if err != nil {
		return false
	}

	if frame.MessageType == MessageTypeVersionNegotiation {
		sendVersionAck(h.conn)
	}

	return true
}

func (h *circuitBreakerConnectionHandler) handleRequests(reader *bufio.Reader) {
	for {
		frame, err := ReadBinaryFrame(reader)
		if err != nil {
			return
		}

		if frame.MessageType == MessageTypeRequest {
			h.processRequest(frame)
		}
	}
}

func (h *circuitBreakerConnectionHandler) processRequest(frame *BinaryFrame) {
	currentFailures := atomic.LoadInt64(h.failureCount)

	if currentFailures < int64(h.failuresBeforeSuccess) {
		h.simulateFailure()

		return
	}

	h.sendSuccessResponse(frame)
}

func (h *circuitBreakerConnectionHandler) simulateFailure() {
	atomic.AddInt64(h.failureCount, 1)
	_ = h.conn.Close()
}

func (h *circuitBreakerConnectionHandler) sendSuccessResponse(frame *BinaryFrame) {
	atomic.AddInt64(h.successCount, 1)

	var wireMsg struct {
		ID         interface{}     `json:"id"`
		MCPPayload json.RawMessage `json:"mcp_payload"`
	}
	if err := json.Unmarshal(frame.Payload, &wireMsg); err != nil {
		// Log error and return early.
		return
	}

	resp := mcp.Response{
		JSONRPC: constants.TestJSONRPCVersion,
		ID:      wireMsg.ID,
		Result:  map[string]interface{}{"status": "recovered"},
	}

	sendResponse(h.conn, frame.Version, wireMsg.ID, resp)
}

// connectionAttempt represents a single connection attempt.
type connectionAttempt struct {
	handler *CircuitBreakerTestHandler
	cfg     config.GatewayConfig
	index   int
}

func (a *connectionAttempt) execute() bool {
	client := a.createClient()
	if client == nil {
		return false
	}

	defer func() {
		if err := client.Close(); err != nil {
			a.handler.t.Logf("Failed to close client: %v", err)
		}
	}()

	if !a.connect(client) {
		return false
	}

	if !a.sendRequest(client) {
		return false
	}

	return a.receiveResponse(client)
}

func (a *connectionAttempt) createClient() *TCPClient {
	client, err := NewTCPClient(a.cfg, a.handler.logger)
	if err != nil {
		a.handler.t.Fatalf("Failed to create client: %v", err)

		return nil
	}

	return client
}

func (a *connectionAttempt) connect(client *TCPClient) bool {
	ctx := context.Background()
	if err := client.Connect(ctx); err != nil {
		a.handler.t.Logf("Connection attempt %d failed (expected): %v", a.index+1, err)

		return false
	}

	return true
}

func (a *connectionAttempt) sendRequest(client *TCPClient) bool {
	req := &mcp.Request{
		JSONRPC: constants.TestJSONRPCVersion,
		Method:  fmt.Sprintf("circuit_test_%d", a.index),
		ID:      fmt.Sprintf("circuit-%d", a.index),
	}

	if err := client.SendRequest(req); err != nil {
		a.handler.t.Logf("Request %d failed: %v", a.index+1, err)

		return false
	}

	return true
}

func (a *connectionAttempt) receiveResponse(client *TCPClient) bool {
	_, err := client.ReceiveResponse()
	if err != nil {
		a.handler.t.Logf("Response %d failed: %v", a.index+1, err)

		return false
	}

	return true
}

// circuitBreakerValidator validates circuit breaker test results.
type circuitBreakerValidator struct {
	t                  *testing.T
	failureCount       int64
	successCount       int64
	successfulRequests int
	expectedFailures   int64
	totalAttempts      int
}

func (v *circuitBreakerValidator) validate() {
	v.validateFailures()
	v.validateSuccesses()
	v.logResults()
}

func (v *circuitBreakerValidator) validateFailures() {
	if v.failureCount < v.expectedFailures {
		v.t.Errorf("Expected at least %d failures, got %d", v.expectedFailures, v.failureCount)
	}
}

func (v *circuitBreakerValidator) validateSuccesses() {
	if v.successCount == 0 {
		v.t.Error("Expected some successful requests after recovery, got 0")
	}

	if v.successfulRequests == 0 {
		v.t.Error("Expected some successful end-to-end requests, got 0")
	}
}

func (v *circuitBreakerValidator) logResults() {
	v.t.Logf("Circuit breaker test results:")
	v.t.Logf("  Total failures: %d", v.failureCount)
	v.t.Logf("  Total successes: %d", v.successCount)
	v.t.Logf("  Successful requests: %d/%d", v.successfulRequests, v.totalAttempts)
}
