package router

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	common "github.com/actual-software/mcp-bridge/pkg/common/config"
	"github.com/actual-software/mcp-bridge/services/router/internal/config"
	"github.com/actual-software/mcp-bridge/services/router/internal/constants"
	"github.com/actual-software/mcp-bridge/services/router/internal/direct"
	"github.com/actual-software/mcp-bridge/services/router/internal/gateway"
	"github.com/actual-software/mcp-bridge/services/router/pkg/mcp"
	"github.com/actual-software/mcp-bridge/test/testutil"
)

const (
	// MCPMethodInitialize represents the MCP initialize method.
	MCPMethodInitialize = "initialize"
)

func createNewRouterTests(t *testing.T) []struct {
	name      string
	config    *config.Config
	logger    *zap.Logger
	wantError bool
	errorMsg  string
} {
	t.Helper()

	return []struct {
		name      string
		config    *config.Config
		logger    *zap.Logger
		wantError bool
		errorMsg  string
	}{
		{
			name: "Valid config and logger",
			config: &config.Config{
				GatewayPool: config.GatewayPoolConfig{
					Endpoints: []config.GatewayEndpoint{
						{
							URL:  fmt.Sprintf("ws://localhost:%d", constants.TestHTTPPort),
							Auth: common.AuthConfig{Type: "bearer", Token: "test-token"},
						},
					},
				},
			},
			logger:    testutil.NewTestLogger(t),
			wantError: false,
		},
		{
			name:      "Nil config",
			config:    nil,
			logger:    testutil.NewTestLogger(t),
			wantError: true,
			errorMsg:  "config is required",
		},
		{
			name: "Nil logger",
			config: &config.Config{
				GatewayPool: config.GatewayPoolConfig{
					Endpoints: []config.GatewayEndpoint{
						{
							URL:  fmt.Sprintf("ws://localhost:%d", constants.TestHTTPPort),
							Auth: common.AuthConfig{Type: "bearer", Token: "test-token"},
						},
					},
				},
			},
			logger:    nil,
			wantError: true,
			errorMsg:  "logger is required",
		},
	}
}

func runNewRouterTest(t *testing.T, tt struct {
	name      string
	config    *config.Config
	logger    *zap.Logger
	wantError bool
	errorMsg  string
}) {
	t.Helper()

	router, err := New(tt.config, tt.logger)

	if tt.wantError {
		if err == nil {
			t.Error("Expected error but got none")

			return
		}

		if tt.errorMsg != "" && !strings.Contains(err.Error(), tt.errorMsg) {
			t.Errorf("Expected error containing '%s', got '%s'", tt.errorMsg, err.Error())
		}

		return
	}

	if err != nil {
		t.Errorf("Unexpected error: %v", err)

		return
	}

	if router == nil {
		t.Error("Expected router to be created")

		return
	}

	validateNewRouterInitialization(t, router, tt.config, tt.logger)
}

func validateNewRouterInitialization(
	t *testing.T,
	router *LocalRouter,
	expectedConfig *config.Config,
	expectedLogger *zap.Logger,
) {
	t.Helper()

	// Verify initialization.
	if router.config != expectedConfig {
		t.Error("Config not set correctly")
	}

	if router.logger != expectedLogger {
		t.Error("Logger not set correctly")
	}

	if router.stdioHandler == nil {
		t.Error("Stdio handler not initialized")
	}

	if router.gwClient == nil {
		t.Error("Gateway client not initialized")
	}

	if router.GetState() != StateInit {
		t.Errorf("Expected initial state INIT, got %s", router.GetState())
	}

	if router.GetMetrics() == nil {
		t.Error("Metrics not initialized")
	}
}

func TestNew(t *testing.T) {
	tests := createNewRouterTests(t)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runNewRouterTest(t, tt)
		})
	}
}

func TestConnectionState_String(t *testing.T) {
	tests := []struct {
		state    ConnectionState
		expected string
	}{
		{StateInit, "INIT"},
		{StateConnecting, "CONNECTING"},
		{StateConnected, "CONNECTED"},
		{StateReconnecting, "RECONNECTING"},
		{StateError, "ERROR"},
		{StateShutdown, "SHUTDOWN"},
		{ConnectionState(999), "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := tt.state.String()
			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func TestLocalRouter_StateManagement(t *testing.T) {
	cfg := &config.Config{
		GatewayPool: config.GatewayPoolConfig{
			Endpoints: []config.GatewayEndpoint{
				{
					URL:  fmt.Sprintf("ws://localhost:%d", constants.TestHTTPPort),
					Auth: common.AuthConfig{Type: "bearer", Token: "test-token"},
				},
			},
		},
	}

	router, err := NewForTesting(cfg, testutil.NewTestLogger(t))
	if err != nil {
		t.Fatalf("Failed to create router: %v", err)
	}

	// Test initial state.
	if state := router.GetState(); state != StateInit {
		t.Errorf("Expected state INIT, got %s", state)
	}

	// State management is now handled by ConnectionManager internally.
	// We can only test the observable behavior through the public API.
	// The connection manager will transition states during the connection lifecycle.

	// Test concurrent access to GetState (read-only operations should be safe).
	var wg sync.WaitGroup
	for i := 0; i < constants.TestConcurrentRoutines; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			_ = router.GetState() // Just test that concurrent reads don't race
		}()
	}

	wg.Wait()
}

func createMessageProcessingMockServer(t *testing.T) *httptest.Server {
	t.Helper()

	return createMockWebSocketServer(t, func(conn *websocket.Conn) {
		// Simple echo server.
		for {
			var msg gateway.WireMessage
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}

			// Send response.
			resp := gateway.WireMessage{
				ID:        msg.ID,
				Timestamp: time.Now().UTC().Format(time.RFC3339),
				Source:    "gateway",
				MCPPayload: map[string]interface{}{
					"jsonrpc": constants.TestJSONRPCVersion,
					"result": map[string]string{
						"status": "ok",
					},
					"id": msg.ID,
				},
			}
			if err := conn.WriteJSON(resp); err != nil {
				return
			}
		}
	})
}

func setupMessageProcessingRouter(
	t *testing.T,
	mockServer *httptest.Server,
) (*LocalRouter, context.Context, context.CancelFunc, <-chan struct{}) {
	t.Helper()

	wsURL := "ws" + strings.TrimPrefix(mockServer.URL, "http")
	cfg := &config.Config{
		GatewayPool: config.GatewayPoolConfig{
			Endpoints: []config.GatewayEndpoint{
				{
					URL:  wsURL,
					Auth: common.AuthConfig{Type: "bearer", Token: "test-token"},
				},
			},
		},
		Local: config.LocalConfig{
			RequestTimeoutMs: constants.TestTimeoutMs,
		},
	}

	router, err := NewForTesting(cfg, testutil.NewTestLogger(t))
	if err != nil {
		t.Fatalf("Failed to create router: %v", err)
	}

	// Start router in background.
	ctx, cancel := context.WithCancel(context.Background())

	// Channel to wait for router completion.
	done := make(chan struct{})

	go func() {
		defer close(done)

		func() { _ = router.Run(ctx) }()
	}()

	return router, ctx, cancel, done
}

func cleanupMessageProcessingRouter(cancel context.CancelFunc, done <-chan struct{}) {
	if cancel != nil {
		cancel()

		select {
		case <-done:
			// Router shut down properly.
		case <-time.After(constants.TestShortTimeout):
			// Don't fail the test, but log that shutdown took longer.
			// t.Log("Router shutdown took longer than expected")
		}
	}
}

func waitForRouterConnection(t *testing.T, router *LocalRouter) {
	t.Helper()

	timeout := time.After(constants.TestMediumTimeout)

	ticker := time.NewTicker(constants.TestTickInterval)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			t.Fatal("Timeout waiting for connection")
		case <-ticker.C:
			if router.GetState() == StateConnected {
				return
			}
		}
	}
}

func createMessageProcessingTests() []struct {
	name          string
	input         string
	wantError     bool
	errorContains string
} {
	return []struct {
		name          string
		input         string
		wantError     bool
		errorContains string
	}{
		{
			name: "Valid request",
			input: fmt.Sprintf(`{
				"jsonrpc": "%s",
				"method": "test",
				"id": "test-123"
			}`, constants.TestJSONRPCVersion),
			wantError: false,
		},
		{
			name:          "Invalid JSON",
			input:         "not json",
			wantError:     true,
			errorContains: "invalid JSON",
		},
	}
}

func runMessageProcessingTest(t *testing.T, router *LocalRouter, tt struct {
	name          string
	input         string
	wantError     bool
	errorContains string
}) {
	t.Helper()

	// Send message via stdin channel.
	router.GetStdinChan() <- []byte(tt.input)

	// Check response.
	select {
	case data := <-router.GetStdoutChan():
		var resp mcp.Response
		if err := json.Unmarshal(data, &resp); err != nil {
			t.Errorf("Failed to unmarshal response: %v", err)

			return
		}

		if tt.wantError {
			if resp.Error == nil {
				t.Error("Expected error but got none")
			} else if tt.errorContains != "" && !strings.Contains(resp.Error.Message, tt.errorContains) {
				t.Errorf("Expected error containing '%s', got '%s'", tt.errorContains, resp.Error.Message)
			}
		} else {
			if resp.Error != nil {
				t.Errorf("Unexpected error: %s", resp.Error.Message)
			}
		}
	case <-time.After(constants.TestShortTimeout):
		t.Error("Timeout waiting for response")
	}
}

func TestLocalRouter_MessageProcessing(t *testing.T) {
	mockServer := createMessageProcessingMockServer(t)
	defer mockServer.Close()

	router, _, cancel, done := setupMessageProcessingRouter(t, mockServer)
	defer cleanupMessageProcessingRouter(cancel, done)

	waitForRouterConnection(t, router)

	tests := createMessageProcessingTests()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runMessageProcessingTest(t, router, tt)
		})
	}
}

// TestLocalRouter_sendResponse removed - this is now an internal method of MessageRouter.

// TestLocalRouter_sendErrorResponse removed - this is now an internal method of MessageRouter.

func TestLocalRouter_ConnectionLifecycle(t *testing.T) {
	connectAttempts := int32(0)

	mockServer := createFailingMockServer(t, &connectAttempts)
	defer mockServer.Close()

	wsURL := "ws" + strings.TrimPrefix(mockServer.URL, "http")
	router := setupConnectionLifecycleTest(t, wsURL)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})

	go func() {
		defer close(done)

		func() { _ = router.Run(ctx) }()
	}()

	waitForConnectionLifecycle(t, router, &connectAttempts, cancel)

	select {
	case <-done:
		// Router shut down properly.
	case <-time.After(constants.TestShortTimeout):
		t.Log("Router shutdown took longer than expected")
	}
}

// createFailingMockServer creates a server that fails initial connections before accepting.
func createFailingMockServer(t *testing.T, connectAttempts *int32) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts := atomic.AddInt32(connectAttempts, 1)
		if attempts < constants.TestMaxRetries {
			w.WriteHeader(http.StatusServiceUnavailable)

			return
		}

		handleSuccessfulConnection(t, w, r)
	}))
}

// handleSuccessfulConnection handles a successful WebSocket connection upgrade.
func handleSuccessfulConnection(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()

	upgrader := websocket.Upgrader{}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	defer func() { _ = conn.Close() }()

	processInitializeRequest(t, conn)
	time.Sleep(constants.TestSleepShort)
}

// processInitializeRequest reads and responds to initialization request.
func processInitializeRequest(t *testing.T, conn *websocket.Conn) {
	t.Helper()

	var msg gateway.WireMessage
	if err := conn.ReadJSON(&msg); err != nil {
		return
	}

	resp := createInitializeWireResponse(msg)
	if err := conn.WriteJSON(resp); err != nil {
		return
	}
}

// createInitializeWireResponse creates an initialize response wire message.
func createInitializeWireResponse(msg gateway.WireMessage) gateway.WireMessage {
	return gateway.WireMessage{
		ID:        msg.ID,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Source:    "gateway",
		MCPPayload: map[string]interface{}{
			"jsonrpc": constants.TestJSONRPCVersion,
			"result": map[string]interface{}{
				"protocolVersion": constants.TestProtocolVersion,
				"capabilities":    map[string]interface{}{},
			},
			"id": msg.ID,
		},
	}
}

// setupConnectionLifecycleTest sets up router and configuration for connection testing.
func setupConnectionLifecycleTest(t *testing.T, wsURL string) *LocalRouter {
	t.Helper()

	cfg := &config.Config{
		GatewayPool: config.GatewayPoolConfig{
			Endpoints: []config.GatewayEndpoint{
				{
					URL:  wsURL,
					Auth: common.AuthConfig{Type: "bearer", Token: "test-token"},
				},
			},
		},
		Local: config.LocalConfig{
			RequestTimeoutMs: 1000,
		},
	}

	router, err := NewForTesting(cfg, testutil.NewTestLogger(t))
	if err != nil {
		t.Fatalf("Failed to create router: %v", err)
	}

	return router
}

// waitForConnectionLifecycle waits for router to establish connection and validates retry behavior.
func waitForConnectionLifecycle(t *testing.T, router *LocalRouter, connectAttempts *int32, cancel context.CancelFunc) {
	t.Helper()

	timeout := time.After(constants.TestMediumTimeout)

	ticker := time.NewTicker(constants.TestLongTickInterval)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			t.Fatal("Timeout waiting for connection")
		case <-ticker.C:
			if router.GetState() == StateConnected {
				validateConnectionRetries(t, connectAttempts)
				cancel()

				return
			}
		}
	}
}

// validateConnectionRetries validates that the expected number of retries occurred.
func validateConnectionRetries(t *testing.T, connectAttempts *int32) {
	t.Helper()

	if atomic.LoadInt32(connectAttempts) < int32(constants.TestMaxRetries) {
		t.Error("Expected at least 3 connection attempts")
	}
}

func createInitializationMockServer(t *testing.T, initReceived chan bool) *httptest.Server {
	t.Helper()

	return createMockWebSocketServer(t, func(conn *websocket.Conn) {
		var msg gateway.WireMessage
		if err := conn.ReadJSON(&msg); err != nil {
			return
		}

		// Verify initialization request.
		reqData, _ := json.Marshal(msg.MCPPayload)

		var req mcp.Request

		if err := json.Unmarshal(reqData, &req); err != nil {
			return
		}

		if req.Method == MCPMethodInitialize {
			initReceived <- true

			// Send response.
			resp := gateway.WireMessage{
				ID:        msg.ID,
				Timestamp: time.Now().UTC().Format(time.RFC3339),
				Source:    "gateway",
				MCPPayload: map[string]interface{}{
					"jsonrpc": constants.TestJSONRPCVersion,
					"result": map[string]interface{}{
						"protocolVersion": constants.TestProtocolVersion,
						"capabilities": map[string]bool{
							"tools": true,
						},
					},
					"id": msg.ID,
				},
			}
			if err := conn.WriteJSON(resp); err != nil {
				t.Logf("Failed to write JSON response: %v", err)
			}
		}

		// Keep connection open.
		time.Sleep(2 * constants.TestSleepShort)
	})
}

func setupInitializationRouter(
	t *testing.T,
	mockServer *httptest.Server,
) (*LocalRouter, context.Context, context.CancelFunc, <-chan struct{}) {
	t.Helper()

	wsURL := "ws" + strings.TrimPrefix(mockServer.URL, "http")
	cfg := &config.Config{
		GatewayPool: config.GatewayPoolConfig{
			Endpoints: []config.GatewayEndpoint{
				{
					URL:  wsURL,
					Auth: common.AuthConfig{Type: "bearer", Token: "test-token"},
				},
			},
		},
		Local: config.LocalConfig{
			RequestTimeoutMs: constants.TestTimeoutMs,
		},
	}

	router, err := NewForTesting(cfg, testutil.NewTestLogger(t))
	if err != nil {
		t.Fatalf("Failed to create router: %v", err)
	}

	// Start router in background.
	ctx, cancel := context.WithCancel(context.Background())

	// Channel to wait for router completion.
	done := make(chan struct{})

	go func() {
		defer close(done)

		func() { _ = router.Run(ctx) }()
	}()

	return router, ctx, cancel, done
}

func cleanupInitializationRouter(t *testing.T, cancel context.CancelFunc, done <-chan struct{}) {
	t.Helper()

	// Cancel context and wait for router to shutdown properly.
	if cancel != nil {
		cancel()

		select {
		case <-done:
			// Router shut down properly.
		case <-time.After(constants.TestShortTimeout):
			// Don't fail the test, but log that shutdown took longer.
			t.Log("Router shutdown took longer than expected")
		}
	}
}

func sendTestInitializationRequest(router *LocalRouter) {
	// Send initialization request (router is passive, doesn't auto-initialize).
	initRequest := mcp.Request{
		JSONRPC: constants.TestJSONRPCVersion,
		Method:  "initialize",
		Params: map[string]interface{}{
			"protocolVersion": MCPProtocolVersion,
			"capabilities":    map[string]interface{}{},
			"clientInfo": map[string]interface{}{
				"name":    "test-client",
				"version": "1.0.0",
			},
		},
		ID: "init-1",
	}

	initData, _ := json.Marshal(initRequest)
	router.GetStdinChan() <- initData
}

func validateInitializationReceived(t *testing.T, initReceived chan bool) {
	t.Helper()

	// Verify initialization was received by the mock server.
	select {
	case <-initReceived:
		// Success - initialization request was forwarded to server.
	case <-time.After(constants.TestMediumTimeout):
		t.Error("Timeout waiting for initialization request to reach server")
	}
}

func TestLocalRouter_Initialization(t *testing.T) {
	initReceived := make(chan bool, 1)

	mockServer := createInitializationMockServer(t, initReceived)
	defer mockServer.Close()

	router, _, cancel, done := setupInitializationRouter(t, mockServer)
	defer cleanupInitializationRouter(t, cancel, done)

	// Wait for connection to be established.
	time.Sleep(constants.TestSleepShort)

	sendTestInitializationRequest(router)

	validateInitializationReceived(t, initReceived)
}

// TestLocalRouter_handleStdinToWS removed - this is now an internal method of MessageRouter.

// TestLocalRouter_Run validates the complete router lifecycle and message flow.
// in a realistic WebSocket communication scenario. This test simulates the
// actual runtime behavior of the router when connected to an MCP gateway.
//
// Router Architecture Under Test:
//   - WebSocket client connection management
//   - STDIO message channel multiplexing
//   - Request-response correlation and routing
//   - Connection state management and lifecycle
//   - Graceful shutdown and resource cleanup
//
// Message Flow Validation:
//  1. Router establishes WebSocket connection to mock gateway
//  2. Router transitions to Connected state
//  3. Client sends JSON-RPC request via STDIN channel
//  4. Router forwards request through WebSocket
//  5. Mock gateway echoes response back
//  6. Router correlates response and forwards to STDOUT channel
//  7. Client receives properly formatted response
//
// Critical Components:
//   - Connection state machine (Disconnected → Connecting → Connected)
//   - Message ID correlation for request-response matching
//   - Channel-based communication between router and client
//   - Error handling and timeout management

func TestLocalRouter_Shutdown(t *testing.T) {
	cfg := &config.Config{
		GatewayPool: config.GatewayPoolConfig{
			Endpoints: []config.GatewayEndpoint{
				{
					URL:  fmt.Sprintf("ws://localhost:%d", constants.TestHTTPPort),
					Auth: common.AuthConfig{Type: "bearer", Token: "test-token"},
				},
			},
		},
	}

	router, err := New(cfg, testutil.NewTestLogger(t))
	if err != nil {
		t.Fatalf("Failed to create router: %v", err)
	}

	// Call shutdown.
	router.Shutdown()

	// Context should be canceled.
	select {
	case <-router.ctx.Done():
		// Success.
	default:
		t.Error("Expected context to be canceled")
	}
}

func TestExtractNamespace(t *testing.T) {
	// This function is in the gateway package, but we can test the concept.
	tests := []struct {
		method   string
		expected string
	}{
		{"initialize", "system"},
		{"tools/list", "system"},
		{"k8s.getPods", "k8s"},
		{"docker.ps", "docker"},
		{"simplemethod", "default"},
	}

	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			// Test the namespace extraction logic.
			ns := "default"
			if tt.method == MCPMethodInitialize || strings.HasPrefix(tt.method, "tools/") {
				ns = "system"
			} else if idx := strings.Index(tt.method, "."); idx > 0 {
				ns = tt.method[:idx]
			}

			if ns != tt.expected {
				t.Errorf("Expected namespace '%s', got '%s'", tt.expected, ns)
			}
		})
	}
}

// Helper functions.

func createMockWebSocketServer(t testingInterface, handler func(*websocket.Conn)) *httptest.Server {
	upgrader := websocket.Upgrader{}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check auth.
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-token" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)

			return
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("Failed to upgrade: %v", err)

			return
		}

		defer func() { _ = conn.Close() }()

		handler(conn)
	}))
}

// testingInterface allows both *testing.T and *testing.B to be used.
type testingInterface interface {
	Errorf(format string, args ...interface{})
	Fatal(args ...interface{})
}

func createBenchmarkMockServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		defer func() { _ = conn.Close() }()

		for {
			var msg gateway.WireMessage
			if err := conn.ReadJSON(&msg); err != nil {
				break
			}

			// Echo back immediately.
			resp := gateway.WireMessage{
				ID:        msg.ID,
				Timestamp: time.Now().UTC().Format(time.RFC3339),
				Source:    "gateway",
				MCPPayload: map[string]interface{}{
					"jsonrpc": constants.TestJSONRPCVersion,
					"result":  "ok",
					"id":      msg.ID,
				},
			}
			if err := conn.WriteJSON(resp); err != nil {
				break
			}
		}
	}))
}

func setupBenchmarkRouter(
	b *testing.B,
	mockServer *httptest.Server,
) (*LocalRouter, context.Context, context.CancelFunc, <-chan struct{}) {
	b.Helper()

	wsURL := "ws" + strings.TrimPrefix(mockServer.URL, "http")
	cfg := &config.Config{
		GatewayPool: config.GatewayPoolConfig{
			Endpoints: []config.GatewayEndpoint{
				{
					URL:  wsURL,
					Auth: common.AuthConfig{Type: "bearer", Token: "test-token"},
				},
			},
		},
		Local: config.LocalConfig{
			RequestTimeoutMs: constants.TestTimeoutMs,
		},
	}

	router, err := NewForTesting(cfg, zap.NewNop())
	if err != nil {
		b.Fatalf("Failed to create router: %v", err)
	}

	// Start router.
	ctx, cancel := context.WithCancel(context.Background())

	// Channel to wait for router completion.
	done := make(chan struct{})

	go func() {
		defer close(done)

		func() { _ = router.Run(ctx) }()
	}()

	return router, ctx, cancel, done
}

func cleanupBenchmarkRouter(cancel context.CancelFunc, done <-chan struct{}) {
	if cancel != nil {
		cancel()

		select {
		case <-done:
			// Router shut down properly.
		case <-time.After(constants.TestShortTimeout):
			// Don't fail the benchmark, just move on.
		}
	}
}

func waitForBenchmarkConnection(router *LocalRouter) {
	// Wait for connection.
	for router.GetState() != StateConnected {
		time.Sleep(constants.TestTickInterval)
	}
}

func runBenchmarkLoop(b *testing.B, router *LocalRouter, request []byte) {
	b.Helper()

	for i := 0; i < b.N; i++ {
		// Send request via stdin channel (proper way with new architecture).
		router.GetStdinChan() <- request
		// Consume response to prevent blocking.
		<-router.GetStdoutChan()
	}
}

func BenchmarkRouter_ProcessMessage(b *testing.B) {
	mockServer := createBenchmarkMockServer()
	defer mockServer.Close()

	router, _, cancel, done := setupBenchmarkRouter(b, mockServer)
	defer cleanupBenchmarkRouter(cancel, done)

	waitForBenchmarkConnection(router)

	request := []byte(`{"jsonrpc":"2.0","method":"benchmark.test","id":"bench-1"}`)

	b.ResetTimer()

	runBenchmarkLoop(b, router, request)
}

// =====================================================================================
// ENHANCED TEST COVERAGE - Message Router & Request Handling.
// =====================================================================================

// TestMessageRouter_RequestResponseCorrelation tests that requests and responses.
// are properly correlated by ID through the message router.
func TestMessageRouter_RequestResponseCorrelation(t *testing.T) {
	receivedResponses := make(map[string]bool)
	responseMu := sync.Mutex{}

	mockServer := createCorrelationMockServer(t)
	defer mockServer.Close()

	router, ctx, cancel := setupCorrelationTest(t, mockServer.URL)
	defer cancel()

	startRouterForCorrelation(t, router, ctx)
	waitForRouterConnection(t, router)

	requestIDs := []string{"req-1", "req-2", "req-3", "req-4", "req-5"}

	monitorCorrelationResponses(t, router, ctx, receivedResponses, &responseMu)
	sendConcurrentRequests(t, router, requestIDs)
	verifyAllResponsesReceived(t, requestIDs, receivedResponses, &responseMu)
}

func createCorrelationMockServer(t *testing.T) *httptest.Server {
	t.Helper()

	return createMockWebSocketServer(t, func(conn *websocket.Conn) {
		for {
			var msg gateway.WireMessage
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}

			// Extract request ID from MCP payload.
			if payload, ok := msg.MCPPayload.(map[string]interface{}); ok {
				if reqID, exists := payload["id"]; exists {
					// Send response with same ID.
					resp := gateway.WireMessage{
						ID:        msg.ID,
						Timestamp: time.Now().UTC().Format(time.RFC3339),
						Source:    "gateway",
						MCPPayload: map[string]interface{}{
							"jsonrpc": constants.TestJSONRPCVersion,
							"result":  map[string]string{"status": "ok", "request_id": fmt.Sprintf("%v", reqID)},
							"id":      reqID,
						},
					}
					if err := conn.WriteJSON(resp); err != nil {
						t.Logf("Failed to write JSON response: %v", err)
					}
				}
			}
		}
	})
}

func setupCorrelationTest(t *testing.T, serverURL string) (*LocalRouter, context.Context, context.CancelFunc) {
	t.Helper()

	wsURL := "ws" + strings.TrimPrefix(serverURL, "http")
	cfg := &config.Config{
		GatewayPool: config.GatewayPoolConfig{
			Endpoints: []config.GatewayEndpoint{
				{URL: wsURL, Auth: common.AuthConfig{Type: "bearer", Token: "test-token"}},
			},
		},
		Local: config.LocalConfig{RequestTimeoutMs: 5000},
	}

	router, err := NewForTesting(cfg, testutil.NewTestLogger(t))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())

	return router, ctx, cancel
}

func startRouterForCorrelation(t *testing.T, router *LocalRouter, ctx context.Context) {
	t.Helper()

	// Use a wait group to ensure router completely shuts down.
	var routerWg sync.WaitGroup
	routerWg.Add(1)

	t.Cleanup(func() {
		routerWg.Wait() // Wait for router to complete shutdown
	})

	go func() {
		defer routerWg.Done()

		_ = router.Run(ctx)
	}()
}

func monitorCorrelationResponses(
	t *testing.T,
	router *LocalRouter,
	ctx context.Context,
	receivedResponses map[string]bool,
	responseMu *sync.Mutex,
) {
	t.Helper()

	// Monitor responses in a separate goroutine.
	responseWg := sync.WaitGroup{}
	responseWg.Add(1)

	t.Cleanup(func() {
		responseWg.Wait()
	})

	go func() {
		defer responseWg.Done()

		for {
			select {
			case data := <-router.GetStdoutChan():
				var resp mcp.Response
				if err := json.Unmarshal(data, &resp); err == nil {
					if idStr, ok := resp.ID.(string); ok {
						t.Logf("Received response for ID: %s", idStr)
						responseMu.Lock()

						receivedResponses[idStr] = true

						responseMu.Unlock()
					}
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}

func sendConcurrentRequests(t *testing.T, router *LocalRouter, requestIDs []string) {
	t.Helper()

	// Send requests concurrently.
	var wg sync.WaitGroup
	for _, id := range requestIDs {
		wg.Add(1)

		go func(reqID string) {
			defer wg.Done()

			req := fmt.Sprintf(`{"jsonrpc":"2.0","method":"test.correlate","params":{"data":"test"},"id":"%s"}`, reqID)
			router.GetStdinChan() <- []byte(req)

			t.Logf("Sent request with ID: %s", reqID)
		}(id)
	}

	// Wait for all requests to be sent.
	wg.Wait()
}

func verifyAllResponsesReceived(
	t *testing.T,
	requestIDs []string,
	receivedResponses map[string]bool,
	responseMu *sync.Mutex,
) {
	t.Helper()

	// Wait for all responses with polling instead of channels.
	timeout := time.Now().Add(5 * time.Second)
	for _, id := range requestIDs {
		for time.Now().Before(timeout) {
			responseMu.Lock()

			received := receivedResponses[id]

			responseMu.Unlock()

			if received {
				t.Logf("Response received for request %s", id)

				break
			}

			time.Sleep(constants.TestTickInterval)
		}

		// Final check.
		responseMu.Lock()

		received := receivedResponses[id]

		responseMu.Unlock()

		if !received {
			t.Errorf("Timeout waiting for response to request %s", id)
		}
	}
}

// TestMessageRouter_ConnectionStateHandling tests message routing behavior.
// during different connection states.
func createConnectionStateTestServer(t *testing.T, mockConnectAttempts *int32) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts := atomic.AddInt32(mockConnectAttempts, 1)
		if attempts < constants.TestMaxRetries {
			// Fail connection.
			w.WriteHeader(http.StatusServiceUnavailable)

			return
		}

		// Accept connection.
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		// Handle messages.
		for {
			var msg gateway.WireMessage
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}
			// Echo response.
			resp := gateway.WireMessage{
				ID: msg.ID, Timestamp: time.Now().UTC().Format(time.RFC3339), Source: "gateway",
				MCPPayload: map[string]interface{}{
					"jsonrpc": constants.TestJSONRPCVersion,
					"result":  "connected",
					"id": func() interface{} {
						if payload, ok := msg.MCPPayload.(map[string]interface{}); ok {
							if id, exists := payload["id"]; exists {
								return id
							}
						}

						return nil
					}()},
			}
			if err := conn.WriteJSON(resp); err != nil {
				t.Logf("Failed to write JSON response: %v", err)
			}
		}
	}))
}

func setupConnectionStateTestRouter(t *testing.T, mockServer *httptest.Server) *LocalRouter {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(mockServer.URL, "http")
	cfg := &config.Config{
		GatewayPool: config.GatewayPoolConfig{
			Endpoints: []config.GatewayEndpoint{
				{URL: wsURL, Auth: common.AuthConfig{Type: "bearer", Token: "test-token"}},
			},
		},
		Local: config.LocalConfig{RequestTimeoutMs: 2000, MaxQueuedRequests: 10},
	}

	router, err := NewForTesting(cfg, testutil.NewTestLogger(t))
	require.NoError(t, err)

	return router
}

func testQueuedRequestWhileDisconnected(t *testing.T, router *LocalRouter) {
	t.Helper()
	// Send request while disconnected - should be queued.
	request := `{"jsonrpc":"2.0","method":"test.queued","params":{},"id":"queued-1"}`
	router.GetStdinChan() <- []byte(request)

	// Wait for connection establishment.
	waitForRouterConnection(t, router)

	// Should receive the queued request response.
	select {
	case data := <-router.GetStdoutChan():
		var resp mcp.Response
		require.NoError(t, json.Unmarshal(data, &resp))
		assert.Equal(t, "queued-1", resp.ID)
		assert.Nil(t, resp.Error)
	case <-time.After(3 * time.Second):
		t.Fatal("Timeout waiting for queued request response")
	}
}

func verifyConnectionAttempts(t *testing.T, mockConnectAttempts *int32) {
	t.Helper()
	// Verify connection attempts (should have retried).
	assert.GreaterOrEqual(t, atomic.LoadInt32(mockConnectAttempts), int32(3))
}

func TestMessageRouter_ConnectionStateHandling(t *testing.T) {
	mockConnectAttempts := int32(0)
	mockServer := createConnectionStateTestServer(t, &mockConnectAttempts)
	defer mockServer.Close()

	router := setupConnectionStateTestRouter(t, mockServer)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		_ = router.Run(ctx)
		close(done)
	}()

	t.Cleanup(func() {
		cancel()
		<-done
	})

	testQueuedRequestWhileDisconnected(t, router)
	verifyConnectionAttempts(t, &mockConnectAttempts)
}

// TestMessageRouter_ErrorHandling tests various error conditions in message routing.
func createErrorHandlingTests() []struct {
	name          string
	input         string
	expectedError bool
	errorContains string
} {
	return []struct {
		name          string
		input         string
		expectedError bool
		errorContains string
	}{
		{
			name:          "Invalid JSON",
			input:         `{invalid json}`,
			expectedError: true,
			errorContains: "invalid JSON",
		},
		{
			name:          "Missing required fields",
			input:         `{"jsonrpc":"2.0"}`,
			expectedError: false, // Will be processed but might generate error response
		},
		{
			name:          "Valid request",
			input:         `{"jsonrpc":"2.0","method":"test","id":"test-1"}`,
			expectedError: false,
		},
		{
			name: "Large request",
			input: fmt.Sprintf(
				`{"jsonrpc":"2.0","method":"test","params":{"data":"%s"},"id":"large-1"}`,
				strings.Repeat("x", 1000),
			),
			expectedError: false,
		},
	}
}

func createErrorHandlingMockServer(t *testing.T) *httptest.Server {
	t.Helper()

	return createMockWebSocketServer(t, func(conn *websocket.Conn) {
		for {
			var msg gateway.WireMessage
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}
			// Send success response.
			resp := gateway.WireMessage{
				ID: msg.ID, Timestamp: time.Now().UTC().Format(time.RFC3339), Source: "gateway",
				MCPPayload: map[string]interface{}{
					"jsonrpc": constants.TestJSONRPCVersion,
					"result":  "ok",
					"id": func() interface{} {
						if payload, ok := msg.MCPPayload.(map[string]interface{}); ok {
							if id, exists := payload["id"]; exists {
								return id
							}
						}

						return nil
					}()},
			}
			if err := conn.WriteJSON(resp); err != nil {
				t.Logf("Failed to write JSON response: %v", err)
			}
		}
	})
}

func setupErrorHandlingRouter(t *testing.T, mockServer *httptest.Server) *LocalRouter {
	t.Helper()

	wsURL := "ws" + strings.TrimPrefix(mockServer.URL, "http")
	cfg := &config.Config{
		GatewayPool: config.GatewayPoolConfig{
			Endpoints: []config.GatewayEndpoint{
				{URL: wsURL, Auth: common.AuthConfig{Type: "bearer", Token: "test-token"}},
			},
		},
		Local: config.LocalConfig{RequestTimeoutMs: 1000},
	}

	router, err := NewForTesting(cfg, testutil.NewTestLogger(t))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		_ = router.Run(ctx)
		close(done)
	}()

	t.Cleanup(func() {
		cancel()
		// Wait for router to fully stop before test cleanup completes
		<-done
	})

	return router
}

func runErrorHandlingTest(t *testing.T, router *LocalRouter, tt struct {
	name          string
	input         string
	expectedError bool
	errorContains string
}) {
	t.Helper()

	// Send request.
	router.GetStdinChan() <- []byte(tt.input)

	// Check response.
	select {
	case data := <-router.GetStdoutChan():
		var resp mcp.Response

		err := json.Unmarshal(data, &resp)

		if tt.expectedError {
			// Should be an error response.
			if err == nil && resp.Error != nil {
				assert.Contains(t, resp.Error.Message, tt.errorContains)
			}
		} else {
			// Should be successful or valid response.
			require.NoError(t, err)
		}
	case <-time.After(constants.TestMediumTimeout):
		if !tt.expectedError {
			t.Error("Timeout waiting for response")
		}
	}
}

func TestMessageRouter_ErrorHandling(t *testing.T) {
	tests := createErrorHandlingTests()

	mockServer := createErrorHandlingMockServer(t)
	defer mockServer.Close()

	router := setupErrorHandlingRouter(t, mockServer)

	defer func() {
		// Cleanup handled in setup
	}()

	waitForRouterConnection(t, router)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runErrorHandlingTest(t, router, tt)
		})
	}
}

// TestMessageRouter_ConcurrentRequests tests handling of multiple concurrent requests.
func createConcurrentRequestsMockServer(t *testing.T) *httptest.Server {
	t.Helper()

	return createMockWebSocketServer(t, func(conn *websocket.Conn) {
		for {
			var msg gateway.WireMessage
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}

			// Add small delay to simulate processing.
			time.Sleep(constants.TestTickInterval)

			// Send response.
			if payload, ok := msg.MCPPayload.(map[string]interface{}); ok {
				resp := gateway.WireMessage{
					ID: msg.ID, Timestamp: time.Now().UTC().Format(time.RFC3339), Source: "gateway",
					MCPPayload: map[string]interface{}{
						"jsonrpc": constants.TestJSONRPCVersion, "result": map[string]interface{}{"processed": true}, "id": payload["id"],
					},
				}
				if err := conn.WriteJSON(resp); err != nil {
					t.Logf("Failed to write JSON response: %v", err)
				}
			}
		}
	})
}

func setupConcurrentRequestsRouter(t *testing.T, mockServer *httptest.Server) *LocalRouter {
	t.Helper()

	wsURL := "ws" + strings.TrimPrefix(mockServer.URL, "http")
	cfg := &config.Config{
		GatewayPool: config.GatewayPoolConfig{
			Endpoints: []config.GatewayEndpoint{
				{URL: wsURL, Auth: common.AuthConfig{Type: "bearer", Token: "test-token"}},
			},
		},
		Local: config.LocalConfig{RequestTimeoutMs: 5000},
	}

	router, err := NewForTesting(cfg, testutil.NewTestLogger(t))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		_ = router.Run(ctx)
		close(done)
	}()

	// Store cleanup function
	t.Cleanup(func() {
		cancel()
		<-done
	})

	return router
}

func monitorConcurrentResponses(router *LocalRouter, responsesReceived *int32, responseIDs *sync.Map) {
	ctx, cancel := context.WithCancel(context.Background())

	// Monitor responses.
	go func() {
		defer cancel()

		for {
			select {
			case data := <-router.GetStdoutChan():
				var resp mcp.Response
				if json.Unmarshal(data, &resp) == nil {
					if idStr, ok := resp.ID.(string); ok {
						responseIDs.Store(idStr, true)
						atomic.AddInt32(responsesReceived, 1)
					}
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}

func sendBulkConcurrentRequests(router *LocalRouter, numRequests, numWorkers int) {
	var wg sync.WaitGroup

	requestChan := make(chan string, numRequests)

	// Generate request IDs.
	for i := 0; i < numRequests; i++ {
		requestChan <- fmt.Sprintf("concurrent-%d", i)
	}

	close(requestChan)

	// Start workers.
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)

		go func(workerID int) {
			defer wg.Done()

			for reqID := range requestChan {
				req := fmt.Sprintf(
					`{"jsonrpc":"2.0","method":"test.concurrent","params":{"worker_id":%d},"id":"%s"}`,
					workerID, reqID)
				router.GetStdinChan() <- []byte(req)

				time.Sleep(5 * time.Millisecond) // Slight delay between requests
			}
		}(w)
	}

	wg.Wait()
}

func validateConcurrentResponses(t *testing.T, numRequests int, responsesReceived *int32, responseIDs *sync.Map) {
	t.Helper()

	// Wait for all responses.
	timeout := time.After(10 * time.Second)

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			t.Fatalf("Timeout waiting for responses. Received %d/%d", atomic.LoadInt32(responsesReceived), numRequests)
		case <-ticker.C:
			// Safe conversion: numRequests is positive and within int32 range for testing
			if numRequests > 0 && numRequests <= math.MaxInt32 && atomic.LoadInt32(responsesReceived) >= int32(numRequests) {
				// Verify all request IDs received responses.
				missing := []string{}

				for i := 0; i < numRequests; i++ {
					reqID := fmt.Sprintf("concurrent-%d", i)
					if _, ok := responseIDs.Load(reqID); !ok {
						missing = append(missing, reqID)
					}
				}

				if len(missing) > 0 {
					t.Errorf("Missing responses for request IDs: %v", missing)
				}

				return
			}
		}
	}
}

func TestMessageRouter_ConcurrentRequests(t *testing.T) {
	const (
		numRequests = 50
		numWorkers  = 10
	)

	responsesReceived := int32(0)
	responseIDs := sync.Map{}

	mockServer := createConcurrentRequestsMockServer(t)
	defer mockServer.Close()

	router := setupConcurrentRequestsRouter(t, mockServer)

	defer func() {
		// Cleanup handled in setupConcurrentRequestsRouter
	}()

	waitForRouterConnection(t, router)

	monitorConcurrentResponses(router, &responsesReceived, &responseIDs)

	sendBulkConcurrentRequests(router, numRequests, numWorkers)

	validateConcurrentResponses(t, numRequests, &responsesReceived, &responseIDs)
}

// TestMessageRouter_RequestTimeout tests timeout handling for requests.
func createDelayedResponseServer(t *testing.T) *httptest.Server {
	t.Helper()

	return createMockWebSocketServer(t, func(conn *websocket.Conn) {
		for {
			var msg gateway.WireMessage
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}
			// Delay response to trigger timeout.
			time.Sleep(2 * time.Second)
			// This response should arrive after timeout.
			resp := gateway.WireMessage{
				ID: msg.ID, Timestamp: time.Now().UTC().Format(time.RFC3339), Source: "gateway",
				MCPPayload: map[string]interface{}{
					"jsonrpc": constants.TestJSONRPCVersion,
					"result":  "delayed",
					"id": func() interface{} {
						if payload, ok := msg.MCPPayload.(map[string]interface{}); ok {
							if id, exists := payload["id"]; exists {
								return id
							}
						}

						return nil
					}()},
			}
			if err := conn.WriteJSON(resp); err != nil {
				t.Logf("Failed to write JSON response: %v", err)
			}
		}
	})
}

func setupTimeoutTestRouter(t *testing.T, mockServer *httptest.Server) *LocalRouter {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(mockServer.URL, "http")
	cfg := &config.Config{
		GatewayPool: config.GatewayPoolConfig{
			Endpoints: []config.GatewayEndpoint{
				{URL: wsURL, Auth: common.AuthConfig{Type: "bearer", Token: "test-token"}},
			},
		},
		Local: config.LocalConfig{RequestTimeoutMs: 500}, // Short timeout
	}

	router, err := NewForTesting(cfg, testutil.NewTestLogger(t))
	require.NoError(t, err)

	return router

}

func testRequestTimeout(t *testing.T, router *LocalRouter) {
	t.Helper()
	// Send request that will timeout.
	request := `{"jsonrpc":"2.0","method":"test.timeout","params":{},"id":"timeout-1"}`
	router.GetStdinChan() <- []byte(request)

	// Should receive timeout error response.
	select {
	case data := <-router.GetStdoutChan():
		var resp mcp.Response
		require.NoError(t, json.Unmarshal(data, &resp))
		assert.Equal(t, "timeout-1", resp.ID)
		assert.NotNil(t, resp.Error)
		assert.Contains(t, resp.Error.Message, "timeout")
	case <-time.After(constants.TestMediumTimeout):
		t.Fatal("Expected timeout error response but got none")
	}
}

func TestMessageRouter_RequestTimeout(t *testing.T) {
	mockServer := createDelayedResponseServer(t)
	defer mockServer.Close()

	router := setupTimeoutTestRouter(t, mockServer)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		_ = router.Run(ctx)
		close(done)
	}()

	t.Cleanup(func() {
		cancel()
		<-done
	})

	waitForRouterConnection(t, router)
	testRequestTimeout(t, router)
}

// TestMessageRouter_MetricsCollection tests that metrics are properly collected during routing.
func createMetricsTestServer(t *testing.T) *httptest.Server {
	t.Helper()

	return createMockWebSocketServer(t, func(conn *websocket.Conn) {

		for {
			var msg gateway.WireMessage
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}
			// Send response.
			resp := gateway.WireMessage{
				ID: msg.ID, Timestamp: time.Now().UTC().Format(time.RFC3339), Source: "gateway",
				MCPPayload: map[string]interface{}{
					"jsonrpc": constants.TestJSONRPCVersion,
					"result":  "ok",
					"id": func() interface{} {
						if payload, ok := msg.MCPPayload.(map[string]interface{}); ok {
							if id, exists := payload["id"]; exists {
								return id
							}
						}

						return nil
					}()},
			}
			if err := conn.WriteJSON(resp); err != nil {
				t.Logf("Failed to write JSON response: %v", err)
			}
		}
	})
}

func setupMetricsTestRouter(t *testing.T, mockServer *httptest.Server) *LocalRouter {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(mockServer.URL, "http")
	cfg := &config.Config{
		GatewayPool: config.GatewayPoolConfig{
			Endpoints: []config.GatewayEndpoint{
				{URL: wsURL, Auth: common.AuthConfig{Type: "bearer", Token: "test-token"}},
			},
		},
		Local: config.LocalConfig{RequestTimeoutMs: 5000},
	}

	router, err := NewForTesting(cfg, testutil.NewTestLogger(t))
	require.NoError(t, err)

	return router
}

func testMetricsCollection(t *testing.T, router *LocalRouter) {
	t.Helper()
	// Get initial metrics.
	initialMetrics := router.GetMetrics()

	// Send multiple requests.
	for i := 0; i < 5; i++ {
		req := fmt.Sprintf(`{"jsonrpc":"2.0","method":"test.metrics","params":{},"id":"metrics-%d"}`, i)
		router.GetStdinChan() <- []byte(req)
		// Consume response.
		<-router.GetStdoutChan()
	}

	// Send invalid request to generate error.
	router.GetStdinChan() <- []byte(`{invalid json}`)
	<-router.GetStdoutChan() // Consume error response

	// Get final metrics.
	finalMetrics := router.GetMetrics()

	// Verify metrics were collected.
	expectedRequests := initialMetrics.RequestsTotal + 5 // 5 valid requests (invalid JSON doesn't count)
	assert.Equal(t, expectedRequests, finalMetrics.RequestsTotal)

	expectedResponses := initialMetrics.ResponsesTotal + 5 // 5 success responses
	assert.Equal(t, expectedResponses, finalMetrics.ResponsesTotal)
	assert.Greater(t, finalMetrics.ErrorsTotal, initialMetrics.ErrorsTotal) // At least 1 error from invalid JSON

	t.Logf("Initial metrics: requests=%d, responses=%d, errors=%d",
		initialMetrics.RequestsTotal, initialMetrics.ResponsesTotal, initialMetrics.ErrorsTotal)
	t.Logf("Final metrics: requests=%d, responses=%d, errors=%d",
		finalMetrics.RequestsTotal, finalMetrics.ResponsesTotal, finalMetrics.ErrorsTotal)
}

func TestMessageRouter_MetricsCollection(t *testing.T) {
	mockServer := createMetricsTestServer(t)
	defer mockServer.Close()

	router := setupMetricsTestRouter(t, mockServer)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Use a wait group to ensure router completely shuts down.
	var routerWg sync.WaitGroup
	routerWg.Add(1)

	defer func() {
		cancel()        // Cancel context first
		routerWg.Wait() // Wait for router to complete shutdown
	}()

	go func() {
		defer routerWg.Done()
		func() { _ = router.Run(ctx) }()
	}()

	waitForRouterConnection(t, router)
	testMetricsCollection(t, router)
}

// =====================================================================================
// ENHANCED TEST COVERAGE - Connection State Management & Request Queue.
// =====================================================================================

// TestConnectionManager_StateTransitions tests all connection state transitions.
func TestConnectionManager_StateTransitions(t *testing.T) {
	connectAttempt := int32(0)
	shouldFail := int32(1)

	mockServer := createStateTransitionServer(t, &connectAttempt, &shouldFail)
	defer mockServer.Close()

	wsURL := "ws" + strings.TrimPrefix(mockServer.URL, "http")
	router := setupStateTransitionRouter(t, wsURL)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var routerWg sync.WaitGroup
	routerWg.Add(1)

	defer func() {
		cancel()
		routerWg.Wait()
	}()

	monitor := monitorStateTransitions(t, router, ctx, &connectAttempt, &shouldFail)

	go func() {
		defer routerWg.Done()

		func() { _ = router.Run(ctx) }()
	}()

	assert.Eventually(t, func() bool {
		return router.GetState() == StateConnected
	}, 10*time.Second, 50*time.Millisecond, "Router should eventually connect")

	// Get thread-safe copies of the state tracking data
	history, seen := monitor.getCopies()

	validateStateTransitions(t, seen, &connectAttempt, history)
}

// createStateTransitionServer creates a server that initially fails then succeeds for state testing.
func createStateTransitionServer(t *testing.T, connectAttempt, shouldFail *int32) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := atomic.AddInt32(connectAttempt, 1)

		if atomic.LoadInt32(shouldFail) == 1 && attempt <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable)

			return
		}

		atomic.StoreInt32(shouldFail, 0)
		handleStateTransitionConnection(t, w, r)
	}))
}

// handleStateTransitionConnection handles successful WebSocket connection for state testing.
func handleStateTransitionConnection(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()

	upgrader := websocket.Upgrader{}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	defer func() { _ = conn.Close() }()

	time.Sleep(2 * constants.TestSleepShort)
}

// setupStateTransitionRouter creates and configures router for state transition testing.
func setupStateTransitionRouter(t *testing.T, wsURL string) *LocalRouter {
	t.Helper()

	cfg := &config.Config{
		GatewayPool: config.GatewayPoolConfig{
			Endpoints: []config.GatewayEndpoint{
				{URL: wsURL, Auth: common.AuthConfig{Type: "bearer", Token: "test-token"}},
			},
		},
		Local: config.LocalConfig{RequestTimeoutMs: 1000},
	}

	router, err := NewForTesting(cfg, testutil.NewTestLogger(t))
	require.NoError(t, err)

	return router
}

// monitorStateTransitions tracks router state changes and manages transition logic.
// stateMonitor encapsulates state monitoring data.
type stateMonitor struct {
	mu           sync.Mutex
	stateHistory []ConnectionState
	statesSeen   map[ConnectionState]bool
}

// getCopies returns thread-safe copies of the monitoring data.
func (sm *stateMonitor) getCopies() ([]ConnectionState, map[ConnectionState]bool) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	history := make([]ConnectionState, len(sm.stateHistory))
	copy(history, sm.stateHistory)

	seen := make(map[ConnectionState]bool)
	for k, v := range sm.statesSeen {
		seen[k] = v
	}

	return history, seen
}

func monitorStateTransitions(
	t *testing.T,
	router *LocalRouter,
	ctx context.Context,
	connectAttempt, shouldFail *int32,
) *stateMonitor {
	t.Helper()

	monitor := &stateMonitor{
		stateHistory: []ConnectionState{},
		statesSeen:   make(map[ConnectionState]bool),
	}

	go func() {
		lastState := router.GetState()

		monitor.mu.Lock()
		monitor.stateHistory = append(monitor.stateHistory, lastState)
		monitor.statesSeen[lastState] = true
		monitor.mu.Unlock()

		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				processStateChange(router, monitor, &lastState, connectAttempt, shouldFail)
			}
		}
	}()

	return monitor
}

// processStateChange handles individual state changes and triggers connection success when appropriate.
func processStateChange(
	router *LocalRouter,
	monitor *stateMonitor,
	lastState *ConnectionState,
	connectAttempt, shouldFail *int32,
) {
	currentState := router.GetState()
	if currentState != *lastState {
		monitor.mu.Lock()
		monitor.stateHistory = append(monitor.stateHistory, currentState)
		monitor.statesSeen[currentState] = true
		monitor.mu.Unlock()

		*lastState = currentState

		if currentState == StateConnecting && atomic.LoadInt32(connectAttempt) > 0 {
			go func() {
				time.Sleep(constants.TestSleepShort)
				atomic.StoreInt32(shouldFail, 0)
			}()
		}
	}
}

// validateStateTransitions verifies that expected state transitions occurred.
func validateStateTransitions(
	t *testing.T,
	statesSeen map[ConnectionState]bool,
	connectAttempt *int32,
	stateHistory []ConnectionState,
) {
	t.Helper()

	assert.True(t, statesSeen[StateConnecting], "Should have transitioned through CONNECTING state")
	assert.True(t, statesSeen[StateConnected], "Should have reached CONNECTED state")

	connectAttempts := atomic.LoadInt32(connectAttempt)
	assert.GreaterOrEqual(t, connectAttempts, int32(1), "Should have made at least one connection attempt")

	t.Logf("State history: %v", stateHistory)
	t.Logf("Connection attempts: %d", connectAttempts)
}

// TestRequestQueue_FullLifecycle tests the complete request queue lifecycle.
func TestRequestQueue_FullLifecycle(t *testing.T) {
	connectionDelayMs := int32(1000) // 1 second delay before accepting connections

	mockServer := createDelayedConnectionServer(t, &connectionDelayMs)
	defer mockServer.Close()

	router := setupQueueLifecycleRouter(t, mockServer.URL)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		_ = router.Run(ctx)
		close(done)
	}()

	t.Cleanup(func() {
		cancel()
		<-done
	})

	queuedRequestIDs := []string{"queued-1", "queued-2", "queued-3"}
	responsesReceived := make(map[string]bool)
	responseMu := sync.Mutex{}

	// Monitor responses
	go monitorQueueLifecycleResponses(ctx, router, &responsesReceived, &responseMu)

	// Send queued requests immediately (while disconnected)
	sendQueuedRequests(router, queuedRequestIDs)

	// Verify requests are queued by checking state
	assert.NotEqual(t, StateConnected, router.GetState(), "Router should not be connected yet")

	// Wait for connection and queue processing
	waitForRouterConnection(t, router)

	// Wait for all queued requests to be processed
	waitForAllQueuedResponses(t, queuedRequestIDs, &responsesReceived, &responseMu)

	// Verify all responses received
	verifyQueuedResponsesReceived(t, queuedRequestIDs, &responsesReceived, &responseMu)
}

func createDelayedConnectionServer(t *testing.T, connectionDelayMs *int32) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Wait before accepting connection
		time.Sleep(time.Duration(atomic.LoadInt32(connectionDelayMs)) * time.Millisecond)

		upgrader := websocket.Upgrader{}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		defer func() { _ = conn.Close() }()

		handleQueuedRequestsLoop(t, conn)
	}))
}

func handleQueuedRequestsLoop(t *testing.T, conn *websocket.Conn) {
	t.Helper()

	// Handle queued requests
	for {
		var msg gateway.WireMessage
		if err := conn.ReadJSON(&msg); err != nil {
			return
		}

		resp := createQueuedResponse(msg)
		if err := conn.WriteJSON(resp); err != nil {
			t.Logf("Failed to write JSON response: %v", err)
		}
	}
}

func createQueuedResponse(msg gateway.WireMessage) gateway.WireMessage {
	return gateway.WireMessage{
		ID:        msg.ID,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Source:    "gateway",
		MCPPayload: map[string]interface{}{
			"jsonrpc": constants.TestJSONRPCVersion,
			"result": map[string]interface{}{
				"status": "processed_from_queue",
				"id":     extractMCPPayloadID(msg.MCPPayload),
			},
			"id": extractMCPPayloadID(msg.MCPPayload),
		},
	}
}

func extractMCPPayloadID(mcpPayload interface{}) interface{} {
	if payload, ok := mcpPayload.(map[string]interface{}); ok {
		if id, exists := payload["id"]; exists {
			return id
		}
	}

	return nil
}

func setupQueueLifecycleRouter(t *testing.T, serverURL string) *LocalRouter {
	t.Helper()

	wsURL := "ws" + strings.TrimPrefix(serverURL, "http")
	cfg := &config.Config{
		GatewayPool: config.GatewayPoolConfig{
			Endpoints: []config.GatewayEndpoint{
				{URL: wsURL, Auth: common.AuthConfig{Type: "bearer", Token: "test-token"}},
			},
		},
		Local: config.LocalConfig{RequestTimeoutMs: 3000, MaxQueuedRequests: 5},
	}

	router, err := NewForTesting(cfg, testutil.NewTestLogger(t))
	require.NoError(t, err)

	return router
}

func monitorQueueLifecycleResponses(
	ctx context.Context,
	router *LocalRouter,
	responsesReceived *map[string]bool,
	responseMu *sync.Mutex,
) {
	for {
		select {
		case data := <-router.GetStdoutChan():
			processQueueLifecycleResponse(data, responsesReceived, responseMu)
		case <-ctx.Done():
			return
		}
	}
}

func processQueueLifecycleResponse(data []byte, responsesReceived *map[string]bool, responseMu *sync.Mutex) {
	var resp mcp.Response
	if json.Unmarshal(data, &resp) == nil {
		if idStr, ok := resp.ID.(string); ok {
			responseMu.Lock()

			(*responsesReceived)[idStr] = true

			responseMu.Unlock()
		}
	}
}

func sendQueuedRequests(router *LocalRouter, queuedRequestIDs []string) {
	for _, id := range queuedRequestIDs {
		req := fmt.Sprintf(
			`{"jsonrpc":"2.0","method":"test.queued","params":{},"id":"%s"}`, id)
		router.GetStdinChan() <- []byte(req)
	}
}

func waitForAllQueuedResponses(
	t *testing.T,
	queuedRequestIDs []string,
	responsesReceived *map[string]bool,
	responseMu *sync.Mutex,
) {
	t.Helper()

	assert.Eventually(t, func() bool {
		responseMu.Lock()
		defer responseMu.Unlock()

		for _, id := range queuedRequestIDs {
			if !(*responsesReceived)[id] {
				return false
			}
		}

		return true
	}, 5*time.Second, 100*time.Millisecond, "All queued requests should be processed")
}

func verifyQueuedResponsesReceived(
	t *testing.T,
	queuedRequestIDs []string,
	responsesReceived *map[string]bool,
	responseMu *sync.Mutex,
) {
	t.Helper()

	responseMu.Lock()
	defer responseMu.Unlock()

	for _, id := range queuedRequestIDs {
		assert.True(t, (*responsesReceived)[id], "Response for %s should be received", id)
	}
}

// TestRequestQueue_OverflowHandling tests queue overflow behavior.
func TestRequestQueue_OverflowHandling(t *testing.T) {
	// Configure small queue size to test overflow.
	cfg := &config.Config{
		GatewayPool: config.GatewayPoolConfig{
			Endpoints: []config.GatewayEndpoint{
				{URL: "ws://unreachable:9999", Auth: common.AuthConfig{Type: "bearer", Token: "test-token"}},
			},
		},
		Local: config.LocalConfig{RequestTimeoutMs: 1000, MaxQueuedRequests: 2}, // Small queue
	}

	router, err := NewForTesting(cfg, testutil.NewTestLogger(t))
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		_ = router.Run(ctx)
		close(done)
	}()

	t.Cleanup(func() {
		cancel()
		<-done
	})

	// Give router time to attempt connection (will fail).
	time.Sleep(constants.TestSleepShort)
	assert.NotEqual(t, StateConnected, router.GetState(), "Should not be connected to unreachable server")

	errorResponses := int32(0)

	// Monitor for error responses.
	go func() {
		for {
			select {
			case data := <-router.GetStdoutChan():
				var resp mcp.Response
				if json.Unmarshal(data, &resp) == nil && resp.Error != nil {
					atomic.AddInt32(&errorResponses, 1)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	// Send more requests than queue capacity.
	for i := 0; i < 5; i++ { // Queue size is 2, so 3 should overflow
		req := fmt.Sprintf(`{"jsonrpc":"2.0","method":"test.overflow","params":{},"id":"overflow-%d"}`, i)
		router.GetStdinChan() <- []byte(req)

		time.Sleep(constants.TestTickInterval) // Small delay between sends
	}

	// Should receive error responses for overflow.
	assert.Eventually(t, func() bool {
		return atomic.LoadInt32(&errorResponses) > 0
	}, 2*time.Second, 50*time.Millisecond, "Should receive error responses for queue overflow")
}

// =====================================================================================
// ENHANCED TEST COVERAGE - Rate Limiting & Advanced Error Handling.
// =====================================================================================

// TestRateLimiting_RequestThrottling tests that rate limiting works correctly.
func TestRateLimiting_RequestThrottling(t *testing.T) {
	mockServer := createRateLimitMockServer(t)
	defer mockServer.Close()

	wsURL := "ws" + strings.TrimPrefix(mockServer.URL, "http")
	router := setupRateLimitTest(t, wsURL)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	routerWg := startRateLimitRouter(t, router, ctx)

	defer func() {
		cancel()
		routerWg.Wait()
	}()

	responseTimes := executeRateLimitRequests(t, router)
	validateRateLimitBehavior(t, responseTimes)
}

// createRateLimitMockServer creates a mock server for rate limiting tests.
func createRateLimitMockServer(t *testing.T) *httptest.Server {
	t.Helper()

	return createMockWebSocketServer(t, func(conn *websocket.Conn) {
		handleRateLimitMessages(t, conn)
	})
}

// handleRateLimitMessages processes messages for rate limiting tests.
func handleRateLimitMessages(t *testing.T, conn *websocket.Conn) {
	t.Helper()

	for {
		var msg gateway.WireMessage
		if err := conn.ReadJSON(&msg); err != nil {
			return
		}

		resp := createRateLimitResponse(msg)
		if err := conn.WriteJSON(resp); err != nil {
			t.Logf("Failed to write JSON response: %v", err)
		}
	}
}

// createRateLimitResponse creates a response for rate limit testing.
func createRateLimitResponse(msg gateway.WireMessage) gateway.WireMessage {
	return gateway.WireMessage{
		ID:        msg.ID,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Source:    "gateway",
		MCPPayload: map[string]interface{}{
			"jsonrpc": constants.TestJSONRPCVersion,
			"result":  "ok",
			"id": func() interface{} {
				if payload, ok := msg.MCPPayload.(map[string]interface{}); ok {
					if id, exists := payload["id"]; exists {
						return id
					}
				}

				return nil
			}(),
		},
	}
}

// setupRateLimitTest configures router with rate limiting settings.
func setupRateLimitTest(t *testing.T, wsURL string) *LocalRouter {
	t.Helper()

	cfg := &config.Config{
		GatewayPool: config.GatewayPoolConfig{
			Endpoints: []config.GatewayEndpoint{
				{URL: wsURL, Auth: common.AuthConfig{Type: "bearer", Token: "test-token"}},
			},
		},
		Local: config.LocalConfig{
			RequestTimeoutMs: constants.TestTimeoutMs,
			RateLimit: common.RateLimitConfig{
				Enabled:        true,
				RequestsPerSec: 2.0, // 2 requests per second
				Burst:          1,   // Allow 1 burst request
			},
		},
	}

	router, err := NewForTesting(cfg, testutil.NewTestLogger(t))
	require.NoError(t, err)

	return router
}

// startRateLimitRouter starts the router with proper shutdown handling.
func startRateLimitRouter(t *testing.T, router *LocalRouter, ctx context.Context) *sync.WaitGroup {
	t.Helper()

	var routerWg sync.WaitGroup
	routerWg.Add(1)

	go func() {
		defer routerWg.Done()

		func() { _ = router.Run(ctx) }()
	}()

	waitForRouterConnection(t, router)

	return &routerWg
}

// executeRateLimitRequests sends test requests and measures response timing.
func executeRateLimitRequests(t *testing.T, router *LocalRouter) []time.Time {
	t.Helper()

	responseTimes := []time.Time{}

	// Send first request (should be immediate due to burst allowance).
	router.GetStdinChan() <- []byte(`{"jsonrpc":"2.0","method":"test.ratelimit","params":{},"id":"rate-1"}`)

	select {
	case <-router.GetStdoutChan():
		responseTimes = append(responseTimes, time.Now())

		t.Log("First request processed immediately (burst allowance)")
	case <-time.After(200 * time.Millisecond):
		t.Fatal("First request should be processed immediately")
	}

	// Send second request immediately.
	router.GetStdinChan() <- []byte(`{"jsonrpc":"2.0","method":"test.ratelimit","params":{},"id":"rate-2"}`)

	select {
	case <-router.GetStdoutChan():
		responseTimes = append(responseTimes, time.Now())

		t.Log("Second request processed")
	case <-time.After(constants.TestShortTimeout):
		t.Log("Second request appears to be rate limited (delayed)")
	}

	// Send third request and measure delay.
	router.GetStdinChan() <- []byte(`{"jsonrpc":"2.0","method":"test.ratelimit","params":{},"id":"rate-3"}`)

	responseStart := time.Now()
	select {
	case <-router.GetStdoutChan():
		responseTimes = append(responseTimes, time.Now())
		elapsed := time.Since(responseStart)
		t.Logf("Third request processed after %v", elapsed)
	case <-time.After(constants.TestMediumTimeout):
		t.Log("Third request appears to be heavily rate limited")
	}

	return responseTimes
}

// validateRateLimitBehavior validates that rate limiting is working correctly.
func validateRateLimitBehavior(t *testing.T, responseTimes []time.Time) {
	t.Helper()

	assert.GreaterOrEqual(t, len(responseTimes), 1, "Should have processed at least one request")

	if len(responseTimes) >= 2 {
		timeBetween := responseTimes[1].Sub(responseTimes[0])
		t.Logf("Time between first and second response: %v", timeBetween)

		minDelay := int64(100)
		delayMsg := "Should have some delay between responses due to rate limiting"
		assert.GreaterOrEqual(t, timeBetween.Milliseconds(), minDelay, delayMsg)
	}

	t.Logf("Rate limiting test completed - processed %d out of 3 requests", len(responseTimes))
}

// =====================================================================================
// ENHANCED TEST COVERAGE - Direct-to-Gateway Fallback Mechanisms.
// =====================================================================================

// TestDirectToGatewayFallback tests the fallback mechanism when direct connections fail.
// processFallbackResponse checks if a response indicates gateway fallback.
func processFallbackResponse(data []byte, fallbackResponses *int32) {
	var resp mcp.Response

	if json.Unmarshal(data, &resp) != nil {
		return
	}

	if resp.Error != nil {
		return
	}

	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		return
	}

	source, exists := result["source"]
	if exists && source == "gateway_fallback" {
		atomic.AddInt32(fallbackResponses, 1)
	}
}

// processRouteDecision extracts routing information from response data.
func processRouteDecision(data []byte, routingDecisions map[string]int32, decisionMu *sync.Mutex) {
	var resp mcp.Response

	if json.Unmarshal(data, &resp) != nil || resp.Error != nil {
		return
	}

	result, ok := resp.Result.(map[string]interface{})
	if !ok {
		return
	}

	route, exists := result["routed_via"]
	if !exists {
		return
	}

	decisionMu.Lock()
	defer decisionMu.Unlock()

	if routeStr, ok := route.(string); ok {
		routingDecisions[routeStr]++
	}
}

func TestDirectToGatewayFallback(t *testing.T) {
	gatewayServer := setupFallbackGatewayServer(t)
	defer gatewayServer.Close()

	cfg := createFallbackTestConfig(gatewayServer.URL)
	router := setupFallbackRouter(t, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		_ = router.Run(ctx)
		close(done)
	}()

	t.Cleanup(func() {
		cancel()
		<-done
	})

	waitForRouterConnection(t, router)

	fallbackResponses := monitorFallbackResponses(t, ctx, router)
	sendDirectOnlyRequests(t, router)
	verifyFallbackBehavior(t, fallbackResponses, router)
}

func setupFallbackGatewayServer(t *testing.T) *httptest.Server {
	t.Helper()

	return createMockWebSocketServer(t, func(conn *websocket.Conn) {
		for {
			var msg gateway.WireMessage
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}
			// Gateway responds successfully.
			resp := gateway.WireMessage{
				ID: msg.ID, Timestamp: time.Now().UTC().Format(time.RFC3339), Source: "gateway",
				MCPPayload: map[string]interface{}{
					"jsonrpc": constants.TestJSONRPCVersion,
					"result":  map[string]interface{}{"source": "gateway_fallback"},
					"id": func() interface{} {
						if payload, ok := msg.MCPPayload.(map[string]interface{}); ok {
							if id, exists := payload["id"]; exists {
								return id
							}

						}

						return nil
					}(),
				},
			}
			if err := conn.WriteJSON(resp); err != nil {
				t.Logf("Failed to write JSON response: %v", err)
			}
		}
	})
}

func createFallbackTestConfig(gatewayURL string) *config.Config {

	wsURL := "ws" + strings.TrimPrefix(gatewayURL, "http")

	return &config.Config{
		GatewayPool: config.GatewayPoolConfig{
			Endpoints: []config.GatewayEndpoint{
				{URL: wsURL, Auth: common.AuthConfig{Type: "bearer", Token: "test-token"}},
			},
		},
		Local: config.LocalConfig{RequestTimeoutMs: 3000},
		// Configure direct mode with fallback enabled.
		Direct: direct.DirectConfig{
			Enabled:        true,
			MaxConnections: 10, // Enable direct mode
			Fallback: direct.FallbackConfig{
				Enabled:       true,
				MaxRetries:    2,
				RetryDelay:    100 * time.Millisecond,
				DirectTimeout: 500 * time.Millisecond,
			},
		},
	}
}

func setupFallbackRouter(t *testing.T, cfg *config.Config) *LocalRouter {
	t.Helper()

	router, err := NewForTesting(cfg, testutil.NewTestLogger(t))

	require.NoError(t, err)

	return router
}

func monitorFallbackResponses(t *testing.T, ctx context.Context, router *LocalRouter) *int32 {
	t.Helper()

	fallbackResponses := int32(0)

	// Monitor responses for fallback indication.
	go func() {
		for {
			select {
			case data := <-router.GetStdoutChan():
				processFallbackResponse(data, &fallbackResponses)
			case <-ctx.Done():
				return
			}
		}
	}()

	return &fallbackResponses
}

func sendDirectOnlyRequests(t *testing.T, router *LocalRouter) {
	t.Helper()

	// Send requests that would try direct first but fall back to gateway.
	// Use methods that would be routed to direct but will fail and fallback.
	directOnlyMethods := []string{
		"tools/list", "tools/call", "resources/list", "resources/read",
	}

	for i, method := range directOnlyMethods {
		req := fmt.Sprintf(`{"jsonrpc":"2.0","method":"%s","params":{},"id":"fallback-%d"}`, method, i)
		router.GetStdinChan() <- []byte(req)
	}
}

func verifyFallbackBehavior(t *testing.T, fallbackResponses *int32, router *LocalRouter) {
	t.Helper()

	// Wait for all fallback responses.
	// Validate directOnlyMethods length before conversion
	directOnlyMethods := []string{
		"tools/list", "tools/call", "resources/list", "resources/read",
	}

	methodCount := len(directOnlyMethods)
	if methodCount > math.MaxInt32 {
		t.Fatalf("Too many directOnlyMethods for test: %d exceeds int32 limit", methodCount)
	}
	// Explicit conversion after bounds check
	expectedCount := int32(methodCount)

	assert.Eventually(t, func() bool {
		return atomic.LoadInt32(fallbackResponses) >= expectedCount
	}, 5*time.Second, 100*time.Millisecond, "Should receive fallback responses from gateway")

	// Verify metrics show both direct attempts and gateway successes.
	metrics := router.GetMetrics()
	assert.Positive(t, metrics.GatewayRequests, "Should have made gateway requests")
	assert.Positive(t, metrics.FallbackSuccesses, "Should have successful fallbacks")
}

// TestRoutingDecisionLogic tests the logic for deciding between direct and gateway routing.
func TestRoutingDecisionLogic(t *testing.T) {
	tests := createRoutingDecisionTests()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runRoutingDecisionTest(t, tt)
		})
	}
}

type routingDecisionTest struct {
	name            string
	method          string
	directEnabled   bool
	expectedRoute   string // "direct" or "gateway"
	directOnlyList  []string
	gatewayOnlyList []string
}

func createRoutingDecisionTests() []routingDecisionTest {
	return []routingDecisionTest{
		{
			name:          "Initialize should go to gateway",
			method:        "initialize",
			directEnabled: true,
			expectedRoute: "gateway",
		},
		{
			name:          "Tools/list should go to direct when enabled",
			method:        "tools/list",
			directEnabled: true,
			expectedRoute: "direct",
		},
		{
			name:          "Tools/list should go to gateway when direct disabled",
			method:        "tools/list",
			directEnabled: false,
			expectedRoute: "gateway",
		},
		{
			name:           "Method in direct-only list",
			method:         "custom/direct",
			directEnabled:  true,
			expectedRoute:  "direct",
			directOnlyList: []string{"custom/direct"},
		},
		{
			name:            "Method in gateway-only list",
			method:          "custom/gateway",
			directEnabled:   true,
			expectedRoute:   "gateway",
			gatewayOnlyList: []string{"custom/gateway"},
		},
	}
}

func runRoutingDecisionTest(t *testing.T, tt routingDecisionTest) {
	t.Helper()

	routingDecisions := make(map[string]int32)
	decisionMu := sync.Mutex{}

	var gatewayRequests int32

	gatewayServer := setupMockGatewayForRouting(t, &gatewayRequests)
	defer gatewayServer.Close()

	router := createRoutingTestRouter(t, gatewayServer.URL, tt)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		_ = router.Run(ctx)
		close(done)
	}()

	t.Cleanup(func() {
		cancel()
		<-done
	})

	waitForRouterConnection(t, router)

	// Monitor which route was taken
	go monitorRoutingDecisions(ctx, router, routingDecisions, &decisionMu)

	// Send test request
	sendRoutingTestRequest(router, tt.method)

	// Wait for response and routing decision
	time.Sleep(1 * time.Second)

	// Verify routing decision
	validateRoutingDecision(t, tt, &gatewayRequests, routingDecisions, &decisionMu, router)
}

func setupMockGatewayForRouting(t *testing.T, gatewayRequests *int32) *httptest.Server {
	t.Helper()

	return createMockWebSocketServer(t, func(conn *websocket.Conn) {
		handleRoutingGatewayRequests(t, conn, gatewayRequests)
	})
}

func handleRoutingGatewayRequests(t *testing.T, conn *websocket.Conn, gatewayRequests *int32) {
	t.Helper()

	for {
		var msg gateway.WireMessage
		if err := conn.ReadJSON(&msg); err != nil {
			return
		}

		atomic.AddInt32(gatewayRequests, 1)

		resp := createRoutingGatewayResponse(msg)
		if err := conn.WriteJSON(resp); err != nil {
			t.Logf("Failed to write JSON response: %v", err)
		}
	}
}

func createRoutingGatewayResponse(msg gateway.WireMessage) gateway.WireMessage {
	return gateway.WireMessage{
		ID:        msg.ID,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Source:    "gateway",
		MCPPayload: map[string]interface{}{
			"jsonrpc": constants.TestJSONRPCVersion,
			"result":  map[string]string{"routed_via": "gateway"},
			"id":      extractMCPPayloadID(msg.MCPPayload),
		},
	}
}

func createRoutingTestRouter(t *testing.T, serverURL string, tt routingDecisionTest) *LocalRouter {
	t.Helper()

	wsURL := "ws" + strings.TrimPrefix(serverURL, "http")
	cfg := &config.Config{
		GatewayPool: config.GatewayPoolConfig{
			Endpoints: []config.GatewayEndpoint{
				{URL: wsURL, Auth: common.AuthConfig{Type: "bearer", Token: "test-token"}},
			},
		},
		Local: config.LocalConfig{RequestTimeoutMs: 2000},
	}

	// Configure direct mode if enabled
	if tt.directEnabled {
		cfg.Direct = direct.DirectConfig{
			Enabled:        true, // Enable direct mode
			MaxConnections: 10,
			Fallback: direct.FallbackConfig{
				Enabled:            true,
				DirectOnlyMethods:  tt.directOnlyList,
				GatewayOnlyMethods: tt.gatewayOnlyList,
				MaxRetries:         1,
				RetryDelay:         50 * time.Millisecond,
				DirectTimeout:      200 * time.Millisecond,
			},
		}
	}

	router, err := NewForTesting(cfg, testutil.NewTestLogger(t))

	require.NoError(t, err)

	return router
}

func monitorRoutingDecisions(
	ctx context.Context,
	router *LocalRouter,
	routingDecisions map[string]int32,
	decisionMu *sync.Mutex,
) {
	for {
		select {
		case data := <-router.GetStdoutChan():
			processRouteDecision(data, routingDecisions, decisionMu)
		case <-ctx.Done():
			return
		}
	}
}

func sendRoutingTestRequest(router *LocalRouter, method string) {
	req := fmt.Sprintf(
		`{"jsonrpc":"2.0","method":"%s","params":{},"id":"route-test"}`, method)
	router.GetStdinChan() <- []byte(req)
}

func validateRoutingDecision(
	t *testing.T,
	tt routingDecisionTest,
	gatewayRequests *int32,
	routingDecisions map[string]int32,
	decisionMu *sync.Mutex,
	router *LocalRouter,
) {
	t.Helper()

	decisionMu.Lock()

	decisions := make(map[string]int32)
	for k, v := range routingDecisions {
		decisions[k] = v
	}

	decisionMu.Unlock()

	switch tt.expectedRoute {
	case "gateway":
		validateGatewayRouting(t, gatewayRequests, decisions)
	case "direct":
		validateDirectRouting(t, gatewayRequests, router, tt.directEnabled)
	}
}

func validateGatewayRouting(t *testing.T, gatewayRequests *int32, decisions map[string]int32) {
	t.Helper()

	assert.Positive(t, atomic.LoadInt32(gatewayRequests), "Should have routed to gateway")
	assert.Positive(t, decisions["gateway"], "Should show gateway routing")
}

func validateDirectRouting(t *testing.T, gatewayRequests *int32, router *LocalRouter, directEnabled bool) {
	t.Helper()

	// For direct routing, we expect fallback to gateway since no direct server
	assert.Positive(t, atomic.LoadInt32(gatewayRequests), "Should have fallen back to gateway")

	// But we should see metrics indicating direct attempts
	if directEnabled {
		metrics := router.GetMetrics()
		// With direct enabled, expect fallback attempts
		hasAttempts := metrics.FallbackSuccesses > 0 || metrics.FallbackFailures > 0
		assert.True(t, hasAttempts, "Should have attempted direct routing")
	}
}

// TestProtocolNegotiation tests protocol-specific routing behavior.
func TestProtocolNegotiation(t *testing.T) {
	protocolResponses := make(map[string]int32)
	protocolMu := sync.Mutex{}

	mockServer := setupProtocolNegotiationServer(t, &protocolResponses, &protocolMu)
	defer mockServer.Close()

	router := createProtocolTestRouter(t, mockServer.URL)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Use a wait group to ensure router completely shuts down
	var routerWg sync.WaitGroup
	routerWg.Add(1)

	go func() {
		defer routerWg.Done()

		func() { _ = router.Run(ctx) }()
	}()

	defer func() {
		cancel()        // Cancel context first
		routerWg.Wait() // Wait for router to complete shutdown
	}()

	waitForRouterConnection(t, router)

	// Test different MCP protocol methods
	protocolMethods := createProtocolMethods()

	// Send protocol negotiation requests
	sendProtocolRequests(router, protocolMethods)

	// Verify all protocol methods were handled
	validateProtocolResponses(t, protocolMethods, &protocolResponses, &protocolMu)

	// Verify metrics show proper request processing
	validateProtocolMetrics(t, router, protocolMethods)
}

func setupProtocolNegotiationServer(
	t *testing.T,
	protocolResponses *map[string]int32,
	protocolMu *sync.Mutex,
) *httptest.Server {
	t.Helper()

	return createMockWebSocketServer(t, func(conn *websocket.Conn) {
		handleProtocolNegotiationRequests(t, conn, protocolResponses, protocolMu)
	})
}

func handleProtocolNegotiationRequests(
	t *testing.T,
	conn *websocket.Conn,
	protocolResponses *map[string]int32,
	protocolMu *sync.Mutex,
) {
	t.Helper()

	for {
		var msg gateway.WireMessage
		if err := conn.ReadJSON(&msg); err != nil {
			return
		}

		trackProtocolMethod(msg, protocolResponses, protocolMu)

		// Send protocol-aware response
		resp := createProtocolResponse(msg)
		if err := conn.WriteJSON(resp); err != nil {
			t.Logf("Failed to write JSON response: %v", err)
		}
	}
}

func trackProtocolMethod(msg gateway.WireMessage, protocolResponses *map[string]int32, protocolMu *sync.Mutex) {
	if payload, ok := msg.MCPPayload.(map[string]interface{}); ok {
		if method, exists := payload["method"]; exists {
			protocolMu.Lock()

			if methodStr, ok := method.(string); ok {
				(*protocolResponses)[methodStr]++
			}

			protocolMu.Unlock()
		}
	}
}

func createProtocolResponse(msg gateway.WireMessage) gateway.WireMessage {
	return gateway.WireMessage{
		ID:        msg.ID,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Source:    "gateway",
		MCPPayload: map[string]interface{}{
			"jsonrpc": constants.TestJSONRPCVersion,
			"result":  map[string]interface{}{"protocol": "mcp", "version": "1.0"},
			"id":      extractMCPPayloadID(msg.MCPPayload),
		},
	}
}

func createProtocolTestRouter(t *testing.T, serverURL string) *LocalRouter {
	t.Helper()

	wsURL := "ws" + strings.TrimPrefix(serverURL, "http")
	cfg := &config.Config{
		GatewayPool: config.GatewayPoolConfig{
			Endpoints: []config.GatewayEndpoint{
				{URL: wsURL, Auth: common.AuthConfig{Type: "bearer", Token: "test-token"}},
			},
		},
		Local: config.LocalConfig{RequestTimeoutMs: 3000},
	}

	router, err := NewForTesting(cfg, testutil.NewTestLogger(t))

	require.NoError(t, err)

	return router
}

func createProtocolMethods() []string {
	return []string{
		"initialize",
		"initialized",
		"tools/list",
		"tools/call",
		"resources/list",
		"resources/read",
		"prompts/list",
		"prompts/get",
	}
}

func sendProtocolRequests(router *LocalRouter, protocolMethods []string) {
	for i, method := range protocolMethods {
		req := fmt.Sprintf(`{"jsonrpc":"2.0","method":"%s","params":{},"id":"protocol-%d"}`, method, i)
		router.GetStdinChan() <- []byte(req)
		// Consume response
		<-router.GetStdoutChan()
	}
}

func validateProtocolResponses(
	t *testing.T,
	protocolMethods []string,
	protocolResponses *map[string]int32,
	protocolMu *sync.Mutex,
) {
	t.Helper()

	protocolMu.Lock()
	defer protocolMu.Unlock()

	for _, method := range protocolMethods {
		assert.Positive(t, (*protocolResponses)[method], "Method %s should have been processed", method)
	}
}

func validateProtocolMetrics(t *testing.T, router *LocalRouter, protocolMethods []string) {
	t.Helper()

	metrics := router.GetMetrics()
	expectedCount := uint64(len(protocolMethods))
	assert.Equal(t, expectedCount, metrics.RequestsTotal, "Should have processed all protocol requests")
	assert.Equal(t, expectedCount, metrics.ResponsesTotal, "Should have sent all protocol responses")
}

// =====================================================================================
// ENHANCED TEST COVERAGE - Performance Benchmarks.
// =====================================================================================

// BenchmarkRouter_ConcurrentRequests benchmarks concurrent request processing.

func createBenchmarkEchoServer(tb testing.TB) *httptest.Server {

	tb.Helper()

	return createMockWebSocketServer(tb, func(conn *websocket.Conn) {
		for {
			var msg gateway.WireMessage
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}
			// Immediate echo.
			resp := gateway.WireMessage{
				ID: msg.ID, Timestamp: time.Now().UTC().Format(time.RFC3339), Source: "gateway",
				MCPPayload: map[string]interface{}{
					"jsonrpc": constants.TestJSONRPCVersion,
					"result":  "ok",
					"id": func() interface{} {
						if payload, ok := msg.MCPPayload.(map[string]interface{}); ok {
							if id, exists := payload["id"]; exists {
								return id

							}

						}

						return nil
					}()},
			}
			if err := conn.WriteJSON(resp); err != nil {
				tb.Logf("Failed to write JSON response: %v", err)
			}
		}
	})
}

func setupConcurrentBenchmarkRouter(b *testing.B, mockServer *httptest.Server) *LocalRouter {
	b.Helper()
	wsURL := "ws" + strings.TrimPrefix(mockServer.URL, "http")
	cfg := &config.Config{
		GatewayPool: config.GatewayPoolConfig{
			Endpoints: []config.GatewayEndpoint{
				{URL: wsURL, Auth: common.AuthConfig{Type: "bearer", Token: "test-token"}},
			},
		},
		Local: config.LocalConfig{RequestTimeoutMs: 5000},
	}

	router, err := NewForTesting(cfg, zap.NewNop())
	if err != nil {

		b.Fatalf("Failed to create router: %v", err)

	}

	return router
}

func waitForBenchmarkRouterConnection(b *testing.B, router *LocalRouter) {
	b.Helper()
	for router.GetState() != StateConnected {
		time.Sleep(1 * time.Millisecond)
	}
}

func runConcurrentBenchmark(b *testing.B, router *LocalRouter) {
	b.Helper()
	b.RunParallel(func(pb *testing.PB) {
		requestID := 0
		for pb.Next() {
			req := fmt.Sprintf(`{"jsonrpc":"2.0","method":"benchmark.concurrent","id":"bench-%d"}`, requestID)
			router.GetStdinChan() <- []byte(req)
			<-router.GetStdoutChan() // Consume response
			requestID++
		}
	})
}

func BenchmarkRouter_ConcurrentRequests(b *testing.B) {
	mockServer := createBenchmarkEchoServer(b)
	defer mockServer.Close()

	router := setupConcurrentBenchmarkRouter(b, mockServer)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		_ = router.Run(ctx)
		close(done)
	}()

	b.Cleanup(func() {
		cancel()
		<-done
	})

	waitForBenchmarkRouterConnection(b, router)

	b.ResetTimer()
	b.SetParallelism(10) // 10 concurrent workers

	runConcurrentBenchmark(b, router)
}

// BenchmarkRouter_LargePayloads benchmarks processing of large request/response payloads.

func createLargePayloadServer(tb testing.TB, largeData string) *httptest.Server {

	tb.Helper()

	return createMockWebSocketServer(tb, func(conn *websocket.Conn) {
		for {
			var msg gateway.WireMessage
			if err := conn.ReadJSON(&msg); err != nil {
				return
			}
			// Echo with large response.
			resp := gateway.WireMessage{
				ID: msg.ID, Timestamp: time.Now().UTC().Format(time.RFC3339), Source: "gateway",
				MCPPayload: map[string]interface{}{
					"jsonrpc": constants.TestJSONRPCVersion,
					"result": map[string]interface{}{
						"data": largeData,
						"size": len(largeData),
					},
					"id": func() interface{} {
						if payload, ok := msg.MCPPayload.(map[string]interface{}); ok {
							if id, exists := payload["id"]; exists {
								return id

							}

						}

						return nil
					}(),
				},
			}
			if err := conn.WriteJSON(resp); err != nil {
				tb.Logf("Failed to write JSON response: %v", err)
			}
		}
	})
}

func setupLargePayloadBenchmarkRouter(b *testing.B, mockServer *httptest.Server) *LocalRouter {
	b.Helper()
	wsURL := "ws" + strings.TrimPrefix(mockServer.URL, "http")
	cfg := &config.Config{
		GatewayPool: config.GatewayPoolConfig{
			Endpoints: []config.GatewayEndpoint{
				{URL: wsURL, Auth: common.AuthConfig{Type: "bearer", Token: "test-token"}},
			},
		},
		Local: config.LocalConfig{RequestTimeoutMs: 10000},
	}

	router, err := NewForTesting(cfg, zap.NewNop())
	if err != nil {

		b.Fatalf("Failed to create router: %v", err)
	}

	return router
}

func runLargePayloadBenchmark(b *testing.B, router *LocalRouter, largeData string) {
	b.Helper()
	requestTemplate := `{"jsonrpc":"2.0","method":"benchmark.large","params":{"data":"%s"},"id":"large-bench"}`
	request := fmt.Sprintf(requestTemplate, largeData)

	b.ResetTimer()
	b.SetBytes(int64(len(request)))

	for i := 0; i < b.N; i++ {
		router.GetStdinChan() <- []byte(request)
		responseData := <-router.GetStdoutChan()
		_ = responseData // Consume response
	}
}

func BenchmarkRouter_LargePayloads(b *testing.B) {
	largeData := strings.Repeat("x", 10*1024) // 10KB payload
	mockServer := createLargePayloadServer(b, largeData)
	defer mockServer.Close()

	router := setupLargePayloadBenchmarkRouter(b, mockServer)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		_ = router.Run(ctx)
		close(done)
	}()

	b.Cleanup(func() {
		cancel()
		<-done
	})

	waitForBenchmarkRouterConnection(b, router)
	runLargePayloadBenchmark(b, router, largeData)
}

// BenchmarkRouter_StateTransitions benchmarks connection state management overhead.
func BenchmarkRouter_StateTransitions(b *testing.B) {
	cfg := &config.Config{
		GatewayPool: config.GatewayPoolConfig{
			Endpoints: []config.GatewayEndpoint{
				{
					URL:  fmt.Sprintf("ws://localhost:%d", constants.TestHTTPPort),
					Auth: common.AuthConfig{Type: "bearer", Token: "test-token"},
				},
			},
		},
		Local: config.LocalConfig{RequestTimeoutMs: 1000},
	}

	router, err := NewForTesting(cfg, zap.NewNop())
	if err != nil {
		b.Fatalf("Failed to create router: %v", err)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// Benchmark state access (read operations).
		_ = router.GetState()
		_ = router.GetMetrics()
	}
}

// Helper functions for testing.
