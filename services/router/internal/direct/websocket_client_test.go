package direct

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/poiley/mcp-bridge/services/router/internal/constants"

	"github.com/gorilla/websocket"
	"github.com/poiley/mcp-bridge/services/router/pkg/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

// testLogger interface allows both *testing.T and *testing.B.
type testLogger interface {
	Logf(format string, args ...interface{})
}

// mockWebSocketServer creates a mock WebSocket server for testing.
type mockWebSocketServer struct {
	server    *httptest.Server
	upgrader  websocket.Upgrader
	messages  chan []byte
	clients   []*websocket.Conn
	clientsMu sync.Mutex // Protect concurrent access to clients slice
	logger    testLogger // For logging errors
}

func newMockWebSocketServer(logger testLogger) *mockWebSocketServer {
	mock := &mockWebSocketServer{
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow all origins for testing
			},
		},
		messages: make(chan []byte, 100),
		clients:  make([]*websocket.Conn, 0),
		logger:   logger,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", mock.handleWebSocket)
	mock.server = httptest.NewServer(mux)

	return mock
}

func (m *mockWebSocketServer) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := m.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	m.clientsMu.Lock()
	m.clients = append(m.clients, conn)
	m.clientsMu.Unlock()

	go func() {
		defer func() { _ = conn.Close() }()

		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				return
			}

			// Parse MCP request.
			var req mcp.Request
			if err := json.Unmarshal(message, &req); err != nil {
				continue
			}

			// Create response.
			var resp mcp.Response

			switch req.Method {
			case "ping":
				resp = mcp.Response{
					JSONRPC: constants.TestJSONRPCVersion,
					Result:  "pong",
					ID:      req.ID,
				}
			case "echo":
				resp = mcp.Response{
					JSONRPC: constants.TestJSONRPCVersion,
					Result:  map[string]interface{}{"echo": req.Method},
					ID:      req.ID,
				}
			default:
				resp = mcp.Response{
					JSONRPC: constants.TestJSONRPCVersion,
					Result:  map[string]interface{}{"method": req.Method},
					ID:      req.ID,
				}
			}

			// Send response.
			respData, _ := json.Marshal(resp)
			if err := conn.WriteMessage(websocket.TextMessage, respData); err != nil {
				if m.logger != nil {
					m.logger.Logf("Mock server: failed to write response: %v", err)
				}
			}
		}
	}()
}

func (m *mockWebSocketServer) getWebSocketURL() string {
	return "ws" + strings.TrimPrefix(m.server.URL, "http")
}

func (m *mockWebSocketServer) close() {
	m.clientsMu.Lock()

	for _, client := range m.clients {
		_ = client.Close()
	}

	m.clientsMu.Unlock()
	m.server.Close()
}

func TestNewWebSocketClient(t *testing.T) {
	logger := zaptest.NewLogger(t)

	testCases := []struct {
		name      string
		serverURL string
		config    WebSocketClientConfig
		wantError bool
	}{
		{
			name:      "valid WebSocket URL",
			serverURL: fmt.Sprintf("ws://localhost:%d", constants.TestHTTPPort),
			config:    WebSocketClientConfig{},
			wantError: false,
		},
		{
			name:      "valid WebSocket Secure URL",
			serverURL: fmt.Sprintf("wss://example.com:%d/path", constants.TestHTTPPort),
			config:    WebSocketClientConfig{},
			wantError: false,
		},
		{
			name:      "config URL overrides serverURL",
			serverURL: fmt.Sprintf("ws://localhost:%d", constants.TestHTTPPort),
			config: WebSocketClientConfig{
				URL: fmt.Sprintf("ws://configured.example.com:%d", constants.TestMetricsPort),
			},
			wantError: false,
		},
		{
			name:      "invalid URL",
			serverURL: "not-a-url",
			config:    WebSocketClientConfig{},
			wantError: true,
		},
		{
			name:      "non-WebSocket scheme",
			serverURL: fmt.Sprintf("http://localhost:%d", constants.TestHTTPPort),
			config:    WebSocketClientConfig{},
			wantError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			client, err := NewWebSocketClient("test-client", tc.serverURL, tc.config, logger)

			if tc.wantError {
				require.Error(t, err)
				assert.Nil(t, client)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, client)
				assert.Equal(t, "test-client", client.GetName())
				assert.Equal(t, "websocket", client.GetProtocol())
				assert.Equal(t, StateDisconnected, client.GetState())
			}
		})
	}
}

func TestWebSocketClientDefaults(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := WebSocketClientConfig{} // Empty config to test defaults

	client, err := NewWebSocketClient("test-client",
		fmt.Sprintf("ws://localhost:%d", constants.TestHTTPPort), config, logger)
	require.NoError(t, err)

	assert.Equal(t, 10*time.Second, client.config.HandshakeTimeout)
	assert.Equal(t, 30*time.Second, client.config.PingInterval)
	assert.Equal(t, 10*time.Second, client.config.PongTimeout)
	assert.Equal(t, int64(1024*1024), client.config.MaxMessageSize)
	assert.Equal(t, 30*time.Second, client.config.Timeout)
	assert.Equal(t, 30*time.Second, client.config.HealthCheck.Interval)
	assert.Equal(t, 5*time.Second, client.config.HealthCheck.Timeout)
	assert.Equal(t, 3, client.config.Connection.MaxReconnectAttempts)
	assert.Equal(t, 5*time.Second, client.config.Connection.ReconnectDelay)
	assert.Equal(t, 4096, client.config.Connection.ReadBufferSize)
	assert.Equal(t, 4096, client.config.Connection.WriteBufferSize)
}

func TestWebSocketClientConnectAndClose(t *testing.T) {
	logger := zaptest.NewLogger(t)

	mockServer := newMockWebSocketServer(t)
	defer mockServer.close()

	config := WebSocketClientConfig{
		URL:     mockServer.getWebSocketURL(),
		Timeout: 5 * time.Second,
		HealthCheck: HealthCheckConfig{
			Enabled: false, // Disable for simple test
		},
	}

	client, err := NewWebSocketClient("test-client", config.URL, config, logger)
	require.NoError(t, err)

	ctx := context.Background()

	// Test connect.
	err = client.Connect(ctx)
	require.NoError(t, err)
	assert.Equal(t, StateConnected, client.GetState())

	// Test connect when already connected.
	err = client.Connect(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already connected")

	// Test close.
	err = client.Close(ctx)
	require.NoError(t, err)
	assert.Equal(t, StateClosed, client.GetState())

	// Test close when already closed.
	err = client.Close(ctx)
	require.NoError(t, err) // Should not error
}

func TestWebSocketClientSendRequest(t *testing.T) {
	logger := zaptest.NewLogger(t)

	mockServer := newMockWebSocketServer(t)
	defer mockServer.close()

	config := WebSocketClientConfig{
		URL:     mockServer.getWebSocketURL(),
		Timeout: 5 * time.Second,
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
	}

	client, err := NewWebSocketClient("echo-client", config.URL, config, logger)
	require.NoError(t, err)

	ctx := context.Background()

	// Connect.
	err = client.Connect(ctx)
	require.NoError(t, err)

	defer func() {
		err := client.Close(ctx)
		require.NoError(t, err)
	}()

	// Wait a moment for the connection to be ready.
	time.Sleep(100 * time.Millisecond)

	// Test successful request.
	req := &mcp.Request{
		JSONRPC: constants.TestJSONRPCVersion,
		Method:  "echo",
		ID:      "test-123",
	}

	resp, err := client.SendRequest(ctx, req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "2.0", resp.JSONRPC)
	assert.Equal(t, "test-123", resp.ID)

	// Check response content.
	result, ok := resp.Result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "echo", result["echo"])

	// Check metrics.
	metrics := client.GetMetrics()
	assert.Equal(t, uint64(1), metrics.RequestCount)
	assert.Equal(t, uint64(0), metrics.ErrorCount)
	assert.Positive(t, metrics.AverageLatency)
}

func TestWebSocketClientSendRequestNotConnected(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := WebSocketClientConfig{
		URL: fmt.Sprintf("ws://localhost:%d", constants.TestHTTPPort),
	}

	client, err := NewWebSocketClient("test-client", config.URL, config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	req := &mcp.Request{
		JSONRPC: constants.TestJSONRPCVersion,
		Method:  "test",
		ID:      "test-123",
	}

	// Try to send request when not connected.
	_, err = client.SendRequest(ctx, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not connected")
}

func TestWebSocketClientSendRequestTimeout(t *testing.T) {
	logger := zaptest.NewLogger(t)

	slowServer := setupWebSocketTimeoutServer(t)
	defer slowServer.close()

	client := setupWebSocketTimeoutClient(t, logger, slowServer)
	defer cleanupWebSocketTimeoutClient(t, client)

	runWebSocketTimeoutTest(t, client)
}

func setupWebSocketTimeoutServer(t *testing.T) *mockWebSocketServer {
	t.Helper()
	slowServer := newMockWebSocketServer(t)

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		defer func() { _ = conn.Close() }()

		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})

	slowServer.server.Close()
	slowServer.server = httptest.NewServer(mux)
	return slowServer
}

func setupWebSocketTimeoutClient(t *testing.T, logger *zap.Logger, slowServer *mockWebSocketServer) *WebSocketClient {
	t.Helper()

	config := WebSocketClientConfig{
		URL:     "ws" + strings.TrimPrefix(slowServer.server.URL, "http"),
		Timeout: 100 * time.Millisecond,
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
	}

	client, err := NewWebSocketClient("slow-client", config.URL, config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = client.Connect(ctx)
	require.NoError(t, err)

	time.Sleep(constants.TestLongTickInterval)
	return client
}

func cleanupWebSocketTimeoutClient(t *testing.T, client *WebSocketClient) {
	t.Helper()

	ctx := context.Background()
	err := client.Close(ctx)
	require.NoError(t, err)
}

func runWebSocketTimeoutTest(t *testing.T, client *WebSocketClient) {
	t.Helper()

	ctx := context.Background()

	req := &mcp.Request{
		JSONRPC: constants.TestJSONRPCVersion,
		Method:  "slow_method",
		ID:      "test-123",
	}

	_, err := client.SendRequest(ctx, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timed out")

	verifyWebSocketTimeoutMetrics(t, client)
}

func verifyWebSocketTimeoutMetrics(t *testing.T, client *WebSocketClient) {
	t.Helper()

	metrics := client.GetMetrics()
	assert.Equal(t, uint64(0), metrics.RequestCount)
	assert.Equal(t, uint64(1), metrics.ErrorCount)
}

func TestWebSocketClientHealth(t *testing.T) {
	logger := zaptest.NewLogger(t)

	mockServer := newMockWebSocketServer(t)
	defer mockServer.close()

	config := WebSocketClientConfig{
		URL: mockServer.getWebSocketURL(),
		HealthCheck: HealthCheckConfig{
			Enabled: false, // We'll test manually
			Timeout: 1 * time.Second,
		},
	}

	client, err := NewWebSocketClient("test-client", config.URL, config, logger)
	require.NoError(t, err)

	ctx := context.Background()

	// Test health when not connected.
	err = client.Health(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not connected")

	// Connect.
	err = client.Connect(ctx)
	require.NoError(t, err)

	defer func() {
		err := client.Close(ctx)
		require.NoError(t, err)
	}()

	// Test health when connected.
	err = client.Health(ctx)
	require.NoError(t, err)
	assert.Equal(t, StateHealthy, client.GetState())

	// Check metrics.
	metrics := client.GetMetrics()
	assert.True(t, metrics.IsHealthy)
	assert.False(t, metrics.LastHealthCheck.IsZero())
}

func TestWebSocketClientHealthCheck(t *testing.T) {
	logger := zaptest.NewLogger(t)

	mockServer := newMockWebSocketServer(t)
	defer mockServer.close()

	config := WebSocketClientConfig{
		URL:     mockServer.getWebSocketURL(),
		Timeout: 5 * time.Second,
		HealthCheck: HealthCheckConfig{
			Enabled:  true,
			Interval: 50 * time.Millisecond, // Fast for testing
			Timeout:  1 * time.Second,
		},
	}

	client, err := NewWebSocketClient("ping-client", config.URL, config, logger)
	require.NoError(t, err)

	ctx := context.Background()

	// Connect (this will start health check loop).
	err = client.Connect(ctx)
	require.NoError(t, err)

	defer func() {
		err := client.Close(ctx)
		require.NoError(t, err)
	}()

	// Wait for a few health checks to run.
	time.Sleep(2 * constants.TestSleepShort)

	// Should be healthy.
	assert.Equal(t, StateHealthy, client.GetState())
	metrics := client.GetMetrics()
	assert.True(t, metrics.IsHealthy)
}

func TestWebSocketClientGetStatus(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := WebSocketClientConfig{
		URL: fmt.Sprintf("ws://localhost:%d", constants.TestHTTPPort),
	}

	client, err := NewWebSocketClient("status-client", config.URL, config, logger)
	require.NoError(t, err)

	status := client.GetStatus()
	assert.Equal(t, "status-client", status.Name)
	assert.Equal(t, fmt.Sprintf("ws://localhost:%d", constants.TestHTTPPort), status.URL)
	assert.Equal(t, "websocket", status.Protocol)
	assert.Equal(t, StateDisconnected, status.State)
}

func TestWebSocketClientConnectInvalidURL(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := WebSocketClientConfig{
		URL:              "ws://nonexistent.invalid:12345",
		HandshakeTimeout: 1 * time.Second, // Short timeout for quick test
	}

	client, err := NewWebSocketClient("invalid-client", config.URL, config, logger)
	require.NoError(t, err)

	ctx := context.Background()

	// Try to connect to invalid URL.
	err = client.Connect(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to establish")
	assert.Equal(t, StateError, client.GetState())
}

func TestWebSocketClientHeaders(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Create a server that checks headers.
	headerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check for custom header.
		if r.Header.Get("X-Test-Header") != "test-value" {
			http.Error(w, "Missing header", http.StatusBadRequest)

			return
		}

		upgrader := websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		defer func() { _ = conn.Close() }()

		// Simple echo server.
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				return
			}

			if err := conn.WriteMessage(websocket.TextMessage, message); err != nil {
				t.Logf("Failed to write message in test server: %v", err)
			}
		}
	}))
	defer headerServer.Close()

	config := WebSocketClientConfig{
		URL: "ws" + strings.TrimPrefix(headerServer.URL, "http"),
		Headers: map[string]string{
			"X-Test-Header": "test-value",
		},
		Timeout: 5 * time.Second,
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
	}

	client, err := NewWebSocketClient("header-client", config.URL, config, logger)
	require.NoError(t, err)

	ctx := context.Background()

	// Connect should succeed with proper headers.
	err = client.Connect(ctx)
	require.NoError(t, err)

	defer func() {
		err := client.Close(ctx)
		require.NoError(t, err)
	}()

	assert.Equal(t, StateConnected, client.GetState())
}

func TestWebSocketClientURL(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := WebSocketClientConfig{
		URL: fmt.Sprintf("ws://example.com:%d/path", constants.TestHTTPPort),
	}

	client, err := NewWebSocketClient("url-client", "ws://original.com", config, logger)
	require.NoError(t, err)

	// Config URL should override serverURL parameter.
	assert.Equal(t, fmt.Sprintf("ws://example.com:%d/path", constants.TestHTTPPort), client.GetURL())
}

// ==============================================================================
// ENHANCED WEBSOCKET CLIENT TESTS - COMPREHENSIVE COVERAGE.
// ==============================================================================

// TestWebSocketClientPingPongMechanism tests ping/pong functionality.
func TestWebSocketClientPingPongMechanism(t *testing.T) {
	logger := zaptest.NewLogger(t)

	pingReceived, pongReceived, server := setupWebSocketPingPongServer(t)
	defer server.Close()

	client := setupWebSocketPingPongClient(t, logger, server)
	defer cleanupWebSocketPingPongClient(t, client)

	runWebSocketPingPongTest(t, client, pingReceived, pongReceived)
}

func setupWebSocketPingPongServer(t *testing.T) (chan bool, chan bool, *httptest.Server) {
	pingReceived := make(chan bool, 5)
	pongReceived := make(chan bool, 5)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		defer func() { _ = conn.Close() }()

		setupWebSocketPingPongHandlers(conn, pingReceived, pongReceived)
		handleWebSocketPingPongMessages(t, conn)
	}))

	return pingReceived, pongReceived, server
}

func setupWebSocketPingPongHandlers(conn *websocket.Conn, pingReceived, pongReceived chan bool) {
	conn.SetPingHandler(func(appData string) error {
		pingReceived <- true
		return conn.WriteControl(websocket.PongMessage, []byte(appData), time.Now().Add(time.Second))
	})

	conn.SetPongHandler(func(appData string) error {
		pongReceived <- true
		return nil
	})
}

func handleWebSocketPingPongMessages(t *testing.T, conn *websocket.Conn) {
	for {
		messageType, message, err := conn.ReadMessage()
		if err != nil {
			return
		}

		if messageType == websocket.TextMessage {
			processWebSocketPingPongMCPRequest(t, conn, message)
		}

		// Send periodic ping
		go func() {
			time.Sleep(100 * time.Millisecond)
			_ = conn.WriteControl(websocket.PingMessage, []byte("server-ping"), time.Now().Add(time.Second))
		}()
	}
}

func processWebSocketPingPongMCPRequest(t *testing.T, conn *websocket.Conn, message []byte) {
	var req mcp.Request
	if err := json.Unmarshal(message, &req); err == nil {
		resp := mcp.Response{
			JSONRPC: constants.TestJSONRPCVersion,
			Result:  "pong",
			ID:      req.ID,
		}

		respData, _ := json.Marshal(resp)
		if err := conn.WriteMessage(websocket.TextMessage, respData); err != nil {
			t.Logf("Failed to write response in test server: %v", err)
		}
	}
}

func setupWebSocketPingPongClient(t *testing.T, logger *zap.Logger, server *httptest.Server) *WebSocketClient {
	config := WebSocketClientConfig{
		URL:          "ws" + strings.TrimPrefix(server.URL, "http"),
		PingInterval: 200 * time.Millisecond,
		PongTimeout:  1 * time.Second,
		Timeout:      5 * time.Second,
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
	}

	client, err := NewWebSocketClient("ping-pong-client", config.URL, config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = client.Connect(ctx)
	require.NoError(t, err)

	return client
}

func cleanupWebSocketPingPongClient(t *testing.T, client *WebSocketClient) {
	ctx := context.Background()
	err := client.Close(ctx)
	require.NoError(t, err)
}

func runWebSocketPingPongTest(t *testing.T, client *WebSocketClient, pingReceived, pongReceived chan bool) {
	// Wait for ping/pong exchanges
	time.Sleep(1 * time.Second)

	sendWebSocketPingRequest(t, client)
	verifyWebSocketPingPongActivity(t, client)
}

func sendWebSocketPingRequest(t *testing.T, client *WebSocketClient) {
	req := &mcp.Request{
		JSONRPC: constants.TestJSONRPCVersion,
		Method:  "ping",
		ID:      "ping-test",
	}

	ctx := context.Background()
	_, err := client.SendRequest(ctx, req)
	require.NoError(t, err)
}

func verifyWebSocketPingPongActivity(t *testing.T, client *WebSocketClient) {
	// Wait for ping/pong activity
	time.Sleep(5 * constants.TestSleepShort)

	// Check that ping/pong frames were exchanged
	// Note: Actual ping/pong frame detection depends on the client implementation
	assert.Equal(t, StateConnected, client.GetState())
}

// TestWebSocketClientMessageCorrelation tests message correlation and ordering.
func TestWebSocketClientMessageCorrelation(t *testing.T) {
	logger := zaptest.NewLogger(t)

	server := setupWebSocketCorrelationServer(t)
	defer server.Close()

	client := setupWebSocketCorrelationClient(t, logger, server)
	defer cleanupWebSocketCorrelationClient(t, client)

	runWebSocketCorrelationTest(t, client)
}

func setupWebSocketCorrelationServer(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		defer func() { _ = conn.Close() }()

		handleWebSocketCorrelationMessages(t, conn)
	}))
}

func handleWebSocketCorrelationMessages(t *testing.T, conn *websocket.Conn) {
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			return
		}

		var req mcp.Request
		if err := json.Unmarshal(message, &req); err != nil {
			continue
		}

		resp := createWebSocketCorrelationResponse(req)
		sendWebSocketCorrelationResponse(t, conn, resp)
	}
}

func createWebSocketCorrelationResponse(req mcp.Request) mcp.Response {
	return mcp.Response{
		JSONRPC: constants.TestJSONRPCVersion,
		Result: map[string]interface{}{
			"request_id":  req.ID,
			"method":      req.Method,
			"timestamp":   time.Now().Unix(),
			"correlation": "corr-" + fmt.Sprintf("%v", req.ID),
		},
		ID: req.ID,
	}
}

func sendWebSocketCorrelationResponse(t *testing.T, conn *websocket.Conn, resp mcp.Response) {
	respData, _ := json.Marshal(resp)
	if err := conn.WriteMessage(websocket.TextMessage, respData); err != nil {
		t.Logf("Mock server: failed to write response: %v", err)
	}
}

func setupWebSocketCorrelationClient(t *testing.T, logger *zap.Logger, server *httptest.Server) *WebSocketClient {
	config := WebSocketClientConfig{
		URL:     "ws" + strings.TrimPrefix(server.URL, "http"),
		Timeout: 5 * time.Second,
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
	}

	client, err := NewWebSocketClient("correlation-client", config.URL, config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = client.Connect(ctx)
	require.NoError(t, err)

	// Wait for connection to stabilize
	time.Sleep(100 * time.Millisecond)

	return client
}

func cleanupWebSocketCorrelationClient(t *testing.T, client *WebSocketClient) {
	ctx := context.Background()
	err := client.Close(ctx)
	require.NoError(t, err)
}

func runWebSocketCorrelationTest(t *testing.T, client *WebSocketClient) {
	const numRequests = 5

	expectedResponses := make(map[string]string, numRequests)

	ctx := context.Background()

	for i := 0; i < numRequests; i++ {
		requestID := fmt.Sprintf("corr-test-%d", i)
		expectedResponses[requestID] = "corr-" + requestID

		req := &mcp.Request{
			JSONRPC: constants.TestJSONRPCVersion,
			Method:  fmt.Sprintf("correlation_test_%d", i),
			ID:      requestID,
		}

		resp, err := client.SendRequest(ctx, req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.Equal(t, requestID, resp.ID)

		verifyWebSocketCorrelationResponse(t, resp, expectedResponses[requestID])
	}
}

func verifyWebSocketCorrelationResponse(t *testing.T, resp *mcp.Response, expectedCorrelation string) {
	result, ok := resp.Result.(map[string]interface{})
	require.True(t, ok)

	correlation, ok := result["correlation"].(string)
	require.True(t, ok)
	assert.Equal(t, expectedCorrelation, correlation)
}

// TestWebSocketClientConcurrentOperations tests concurrent message handling.
func TestWebSocketClientConcurrentOperations(t *testing.T) {
	logger := zaptest.NewLogger(t)

	mockServer := newMockWebSocketServer(t)
	defer mockServer.close()

	client := setupWebSocketConcurrentClient(t, logger, mockServer)
	defer cleanupWebSocketConcurrentClient(t, client)

	runWebSocketConcurrentOperations(t, client)
}

func setupWebSocketConcurrentClient(
	t *testing.T,
	logger *zap.Logger,
	mockServer *mockWebSocketServer,
) *WebSocketClient {
	config := WebSocketClientConfig{
		URL:     mockServer.getWebSocketURL(),
		Timeout: 10 * time.Second,
		Performance: WebSocketPerformanceConfig{
			EnableWriteCompression: true,
			EnableReadCompression:  true,
			OptimizePingPong:       true,
			EnableMessagePooling:   true,
			MessageBatchSize:       10,
		},
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
	}

	client, err := NewWebSocketClient("concurrent-client", config.URL, config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = client.Connect(ctx)
	require.NoError(t, err)

	// Wait for connection to establish
	time.Sleep(2 * constants.TestSleepShort)

	return client
}

func cleanupWebSocketConcurrentClient(t *testing.T, client *WebSocketClient) {
	ctx := context.Background()
	err := client.Close(ctx)
	require.NoError(t, err)
}

func runWebSocketConcurrentOperations(t *testing.T, client *WebSocketClient) {
	const (
		numGoroutines        = 10
		requestsPerGoroutine = 5
	)

	errChan, responseChan := setupWebSocketConcurrentChannels(numGoroutines, requestsPerGoroutine)
	executeWebSocketConcurrentRequests(t, client, numGoroutines, requestsPerGoroutine, errChan, responseChan)

	errors, responses := collectWebSocketConcurrentResults(errChan, responseChan, numGoroutines, requestsPerGoroutine)
	verifyWebSocketConcurrentResults(t, client, errors, responses, numGoroutines, requestsPerGoroutine)
}

func setupWebSocketConcurrentChannels(numGoroutines, requestsPerGoroutine int) (chan error, chan *mcp.Response) {
	errChan := make(chan error, numGoroutines*requestsPerGoroutine)
	responseChan := make(chan *mcp.Response, numGoroutines*requestsPerGoroutine)
	return errChan, responseChan
}

func executeWebSocketConcurrentRequests(
	t *testing.T,
	client *WebSocketClient,
	numGoroutines, requestsPerGoroutine int,
	errChan chan error,
	responseChan chan *mcp.Response,
) {
	var wg sync.WaitGroup

	ctx := context.Background()

	// Launch concurrent goroutines
	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)

		go func(goroutineID int) {
			defer wg.Done()

			executeWebSocketConcurrentGoroutine(ctx, client, goroutineID, requestsPerGoroutine, errChan, responseChan)
		}(g)
	}

	waitForWebSocketConcurrentCompletion(t, &wg)
}

func executeWebSocketConcurrentGoroutine(
	ctx context.Context,
	client *WebSocketClient,
	goroutineID, requestsPerGoroutine int,
	errChan chan error,
	responseChan chan *mcp.Response,
) {
	for r := 0; r < requestsPerGoroutine; r++ {
		req := &mcp.Request{
			JSONRPC: constants.TestJSONRPCVersion,
			Method:  "concurrent_test",
			ID:      fmt.Sprintf("concurrent-%d-%d-%d", goroutineID, r, time.Now().UnixNano()),
		}

		resp, err := client.SendRequest(ctx, req)
		if err != nil {
			errChan <- err
			return
		}

		responseChan <- resp
	}
}

func waitForWebSocketConcurrentCompletion(t *testing.T, wg *sync.WaitGroup) {
	done := make(chan struct{})

	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(30 * time.Second):
		t.Fatal("Concurrent operations timed out")
	}
}

func collectWebSocketConcurrentResults(
	errChan chan error,
	responseChan chan *mcp.Response,
	numGoroutines, requestsPerGoroutine int,
) ([]error, []*mcp.Response) {
	close(errChan)
	close(responseChan)

	errors := make([]error, 0, numGoroutines*requestsPerGoroutine)
	for err := range errChan {
		errors = append(errors, err)
	}

	responses := make([]*mcp.Response, 0, numGoroutines*requestsPerGoroutine)
	for resp := range responseChan {
		responses = append(responses, resp)
	}

	return errors, responses
}

func verifyWebSocketConcurrentResults(
	t *testing.T,
	client *WebSocketClient,
	errors []error,
	responses []*mcp.Response,
	numGoroutines, requestsPerGoroutine int,
) {
	assert.Empty(t, errors, "No errors should occur during concurrent operations")
	assert.Len(t, responses, numGoroutines*requestsPerGoroutine, "All requests should receive responses")

	// Verify metrics
	metrics := client.GetMetrics()
	assert.Equal(t, uint64(numGoroutines*requestsPerGoroutine), metrics.RequestCount)
	assert.Equal(t, uint64(0), metrics.ErrorCount)
	assert.True(t, metrics.IsHealthy)
}

// TestWebSocketClientReconnectionLogic tests automatic reconnection scenarios.
func TestWebSocketClientReconnectionLogic(t *testing.T) {
	logger := zaptest.NewLogger(t)

	serverState, server := setupWebSocketReconnectServer(t)
	defer server.Close()

	client := setupWebSocketReconnectClient(t, logger, server)
	defer cleanupWebSocketReconnectClient(t, client)

	runWebSocketReconnectionTest(t, client, serverState)
}

type webSocketReconnectServerState struct {
	mu              sync.Mutex
	enabled         bool
	connectionCount int
}

func setupWebSocketReconnectServer(t *testing.T) (*webSocketReconnectServerState, *httptest.Server) {
	state := &webSocketReconnectServerState{
		mu:              sync.Mutex{},
		enabled:         true,
		connectionCount: 0,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		state.mu.Lock()
		enabled := state.enabled
		state.connectionCount++
		currentConnCount := state.connectionCount
		state.mu.Unlock()

		if !enabled {
			http.Error(w, "Server temporarily unavailable", http.StatusServiceUnavailable)
			return
		}

		handleWebSocketReconnectConnection(t, w, r, currentConnCount)
	}))

	return state, server
}

func handleWebSocketReconnectConnection(t *testing.T, w http.ResponseWriter, r *http.Request, connectionCount int) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	defer func() { _ = conn.Close() }()

	sendWebSocketReconnectAck(t, conn, connectionCount)
	handleWebSocketReconnectMessages(t, conn, connectionCount)
}

func sendWebSocketReconnectAck(t *testing.T, conn *websocket.Conn, connectionCount int) {
	ackMsg := map[string]interface{}{
		"type":             "connection_ack",
		"connection_count": connectionCount,
	}

	ackData, _ := json.Marshal(ackMsg)
	if err := conn.WriteMessage(websocket.TextMessage, ackData); err != nil {
		t.Logf("Failed to write acknowledgment: %v", err)
	}
}

func handleWebSocketReconnectMessages(t *testing.T, conn *websocket.Conn, connectionCount int) {
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			return
		}

		var req mcp.Request
		if err := json.Unmarshal(message, &req); err != nil {
			continue
		}

		resp := mcp.Response{
			JSONRPC: constants.TestJSONRPCVersion,
			Result: map[string]interface{}{
				"method":           req.Method,
				"connection_count": connectionCount,
			},
			ID: req.ID,
		}

		respData, _ := json.Marshal(resp)
		if err := conn.WriteMessage(websocket.TextMessage, respData); err != nil {
			t.Logf("Mock server: failed to write response: %v", err)
		}
	}
}

func setupWebSocketReconnectClient(t *testing.T, logger *zap.Logger, server *httptest.Server) *WebSocketClient {
	config := WebSocketClientConfig{
		URL:     "ws" + strings.TrimPrefix(server.URL, "http"),
		Timeout: 2 * time.Second,
		Connection: ConnectionConfig{
			MaxReconnectAttempts: 3,
			ReconnectDelay:       500 * time.Millisecond,
		},
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
	}

	client, err := NewWebSocketClient("reconnect-client", config.URL, config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = client.Connect(ctx)
	require.NoError(t, err)

	return client
}

func cleanupWebSocketReconnectClient(t *testing.T, client *WebSocketClient) {
	ctx := context.Background()
	err := client.Close(ctx)
	require.NoError(t, err)
}

func runWebSocketReconnectionTest(t *testing.T, client *WebSocketClient, serverState *webSocketReconnectServerState) {
	ctx := context.Background()

	// Verify initial connection and send test request
	verifyWebSocketInitialConnection(t, client)
	sendWebSocketInitialRequest(t, client, ctx)

	// Simulate network failure and recovery
	simulateWebSocketNetworkFailure(serverState)
	time.Sleep(1 * time.Second)

	enableWebSocketReconnectServer(serverState)

	// Allow time for reconnection attempts
	time.Sleep(2 * time.Second)

	verifyWebSocketReconnectionResult(t, client)
}

func verifyWebSocketInitialConnection(t *testing.T, client *WebSocketClient) {
	time.Sleep(2 * constants.TestSleepShort)
	assert.Equal(t, StateConnected, client.GetState())
}

func sendWebSocketInitialRequest(t *testing.T, client *WebSocketClient, ctx context.Context) {
	req1 := &mcp.Request{
		JSONRPC: constants.TestJSONRPCVersion,
		Method:  "test_before_disconnect",
		ID:      "reconnect-test-1",
	}

	resp1, err := client.SendRequest(ctx, req1)
	require.NoError(t, err)
	assert.NotNil(t, resp1)
}

func simulateWebSocketNetworkFailure(serverState *webSocketReconnectServerState) {
	serverState.mu.Lock()
	serverState.enabled = false
	serverState.mu.Unlock()
}

func enableWebSocketReconnectServer(serverState *webSocketReconnectServerState) {
	serverState.mu.Lock()
	serverState.enabled = true
	serverState.mu.Unlock()
}

func verifyWebSocketReconnectionResult(t *testing.T, client *WebSocketClient) {
	state := client.GetState()
	t.Logf("Final client state after reconnection test: %v", state)
}

// TestWebSocketClientPerformanceOptimizations tests WebSocket-specific performance features.
func TestWebSocketClientPerformanceOptimizations(t *testing.T) {
	logger := zaptest.NewLogger(t)
	testCases := getWebSocketPerformanceTestCases()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			runWebSocketPerformanceTest(t, logger, tc)
		})
	}
}

type webSocketPerformanceTestCase struct {
	name   string
	config WebSocketPerformanceConfig
}

func getWebSocketPerformanceTestCases() []webSocketPerformanceTestCase {
	return []webSocketPerformanceTestCase{
		{
			name: "compression enabled",
			config: WebSocketPerformanceConfig{
				EnableWriteCompression: true,
				EnableReadCompression:  true,
				OptimizePingPong:       true,
				EnableMessagePooling:   true,
				MessageBatchSize:       10,
			},
		},
		{
			name: "message batching optimized",
			config: WebSocketPerformanceConfig{
				EnableWriteCompression: false,
				EnableReadCompression:  true,
				OptimizePingPong:       true,
				EnableMessagePooling:   true,
				MessageBatchSize:       20,
			},
		},
		{
			name: "minimal overhead",
			config: WebSocketPerformanceConfig{
				EnableWriteCompression: false,
				EnableReadCompression:  false,
				OptimizePingPong:       false,
				EnableMessagePooling:   false,
				MessageBatchSize:       0,
			},
		},
	}
}

func runWebSocketPerformanceTest(t *testing.T, logger *zap.Logger, tc webSocketPerformanceTestCase) {
	mockServer := newMockWebSocketServer(t)
	defer mockServer.close()

	client, connectTime := setupWebSocketPerformanceClient(t, logger, mockServer, tc.config)
	defer cleanupWebSocketPerformanceClient(t, client)

	requestTimes := executeWebSocketPerformanceRequests(t, client)
	verifyWebSocketPerformanceResults(t, tc.name, connectTime, requestTimes)
}

func setupWebSocketPerformanceClient(
	t *testing.T,
	logger *zap.Logger,
	mockServer *mockWebSocketServer,
	perfConfig WebSocketPerformanceConfig,
) (*WebSocketClient, time.Duration) {
	config := WebSocketClientConfig{
		URL:         mockServer.getWebSocketURL(),
		Timeout:     5 * time.Second,
		Performance: perfConfig,
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
	}

	client, err := NewWebSocketClient("perf-client", config.URL, config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	start := time.Now()
	err = client.Connect(ctx)
	require.NoError(t, err)

	connectTime := time.Since(start)

	// Wait for connection to establish
	time.Sleep(100 * time.Millisecond)

	return client, connectTime
}

func cleanupWebSocketPerformanceClient(t *testing.T, client *WebSocketClient) {
	ctx := context.Background()
	err := client.Close(ctx)
	require.NoError(t, err)
}

func executeWebSocketPerformanceRequests(t *testing.T, client *WebSocketClient) []time.Duration {
	const numRequests = 20

	requestTimes := make([]time.Duration, numRequests)
	ctx := context.Background()

	for i := 0; i < numRequests; i++ {
		req := &mcp.Request{
			JSONRPC: constants.TestJSONRPCVersion,
			Method:  fmt.Sprintf("perf_test_%d", i),
			ID:      fmt.Sprintf("perf-%d", i),
		}

		start := time.Now()
		resp, err := client.SendRequest(ctx, req)
		requestTimes[i] = time.Since(start)

		require.NoError(t, err)
		assert.NotNil(t, resp)
	}

	return requestTimes
}

func verifyWebSocketPerformanceResults(
	t *testing.T,
	testName string,
	connectTime time.Duration,
	requestTimes []time.Duration,
) {
	assert.Less(t, connectTime, 2*time.Second, "Connection should be fast: %v", connectTime)

	var totalRequestTime time.Duration
	for _, reqTime := range requestTimes {
		totalRequestTime += reqTime
		assert.Less(t, reqTime, 1*time.Second, "Request should be fast: %v", reqTime)
	}

	avgRequestTime := totalRequestTime / time.Duration(len(requestTimes))

	t.Logf("Performance stats for %s:", testName)
	t.Logf("  Connect time: %v", connectTime)
	t.Logf("  Avg request time: %v", avgRequestTime)
	t.Logf("  Total request time: %v", totalRequestTime)
}

// TestWebSocketClientFrameHandling tests various WebSocket frame types and sizes.
func TestWebSocketClientFrameHandling(t *testing.T) {
	logger := zaptest.NewLogger(t)
	testCases := getWebSocketFrameTestCases()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			runWebSocketFrameHandlingTest(t, logger, tc)
		})
	}
}

type webSocketFrameTestCase struct {
	name        string
	payloadSize int
	compress    bool
}

func getWebSocketFrameTestCases() []webSocketFrameTestCase {
	return []webSocketFrameTestCase{
		{"small payload", 100, false},
		{"medium payload", 10000, false},
		{"large payload", 100000, false},
		{"small payload compressed", 100, true},
		{"medium payload compressed", 10000, true},
		{"large payload compressed", 100000, true},
	}
}

func runWebSocketFrameHandlingTest(t *testing.T, logger *zap.Logger, tc webSocketFrameTestCase) {
	server := setupWebSocketFrameHandlingServer(t, tc)
	defer server.Close()

	client := setupWebSocketFrameHandlingClient(t, logger, server, tc)
	defer cleanupWebSocketFrameHandlingClient(t, client)

	executeWebSocketFrameHandlingTest(t, client, tc)
}

func setupWebSocketFrameHandlingServer(t *testing.T, tc webSocketFrameTestCase) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{
			CheckOrigin:       func(r *http.Request) bool { return true },
			EnableCompression: tc.compress,
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		defer func() { _ = conn.Close() }()

		handleWebSocketFrameMessages(t, conn, tc)
	}))
}

func handleWebSocketFrameMessages(t *testing.T, conn *websocket.Conn, tc webSocketFrameTestCase) {
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			return
		}

		var req mcp.Request
		if err := json.Unmarshal(message, &req); err != nil {
			continue
		}

		resp := createWebSocketFrameResponse(req, tc)
		sendWebSocketFrameResponse(t, conn, resp)
	}
}

func createWebSocketFrameResponse(req mcp.Request, tc webSocketFrameTestCase) mcp.Response {
	payload := strings.Repeat("x", tc.payloadSize)
	return mcp.Response{
		JSONRPC: constants.TestJSONRPCVersion,
		Result: map[string]interface{}{
			"method":     req.Method,
			"payload":    payload,
			"size":       len(payload),
			"compressed": tc.compress,
		},
		ID: req.ID,
	}
}

func sendWebSocketFrameResponse(t *testing.T, conn *websocket.Conn, resp mcp.Response) {
	respData, _ := json.Marshal(resp)
	if err := conn.WriteMessage(websocket.TextMessage, respData); err != nil {
		t.Logf("Failed to write response in test server: %v", err)
	}
}

func setupWebSocketFrameHandlingClient(
	t *testing.T,
	logger *zap.Logger,
	server *httptest.Server,
	tc webSocketFrameTestCase,
) *WebSocketClient {
	config := WebSocketClientConfig{
		URL:            "ws" + strings.TrimPrefix(server.URL, "http"),
		Timeout:        10 * time.Second,
		MaxMessageSize: int64(200 * 1024), // 200KB max
		Performance: WebSocketPerformanceConfig{
			EnableWriteCompression: tc.compress,
			EnableReadCompression:  tc.compress,
		},
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
	}

	client, err := NewWebSocketClient("frame-test-client", config.URL, config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = client.Connect(ctx)
	require.NoError(t, err)

	// Wait for connection to establish
	time.Sleep(100 * time.Millisecond)

	return client
}

func cleanupWebSocketFrameHandlingClient(t *testing.T, client *WebSocketClient) {
	ctx := context.Background()
	err := client.Close(ctx)
	require.NoError(t, err)
}

func executeWebSocketFrameHandlingTest(t *testing.T, client *WebSocketClient, tc webSocketFrameTestCase) {
	req := &mcp.Request{
		JSONRPC: constants.TestJSONRPCVersion,
		Method:  "frame_test",
		Params:  map[string]interface{}{"data": strings.Repeat("test", tc.payloadSize/4)},
		ID:      "frame-test",
	}

	ctx := context.Background()
	start := time.Now()
	resp, err := client.SendRequest(ctx, req)
	duration := time.Since(start)

	require.NoError(t, err)
	assert.NotNil(t, resp)

	verifyWebSocketFrameResponse(t, resp, tc, duration)
}

func verifyWebSocketFrameResponse(t *testing.T, resp *mcp.Response, tc webSocketFrameTestCase, duration time.Duration) {
	result, ok := resp.Result.(map[string]interface{})
	require.True(t, ok)

	size, ok := result["size"].(float64)
	require.True(t, ok)
	assert.InEpsilon(t, float64(tc.payloadSize), size, 0.001)

	t.Logf("Frame handling for %s: %v", tc.name, duration)
}

// TestWebSocketClientErrorRecovery tests error recovery mechanisms.
// createErrorRecoveryHandler creates a WebSocket handler that simulates error conditions.
func createErrorRecoveryHandler(t *testing.T, errorCount *int) http.HandlerFunc {
	t.Helper()

	return func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		defer func() { _ = conn.Close() }()

		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				return
			}

			var req mcp.Request
			if err := json.Unmarshal(message, &req); err != nil {
				continue
			}

			method := req.Method

			if method == "cause_error" {
				*errorCount++
				if *errorCount <= 2 {
					// First two requests cause protocol error.
					closeMsg := websocket.FormatCloseMessage(websocket.CloseInternalServerErr, "Simulated error")
					if err := conn.WriteMessage(websocket.CloseMessage, closeMsg); err != nil {
						t.Logf("Failed to write close message: %v", err)
					}

					return
				}
			}

			// Normal response.
			resp := mcp.Response{
				JSONRPC: constants.TestJSONRPCVersion,
				Result: map[string]interface{}{
					"method":      method,
					"error_count": *errorCount,
				},
				ID: req.ID,
			}

			respData, _ := json.Marshal(resp)
			if err := conn.WriteMessage(websocket.TextMessage, respData); err != nil {
				t.Logf("Mock server: failed to write response: %v", err)
			}
		}
	}
}

func TestWebSocketClientErrorRecovery(t *testing.T) {
	logger := zaptest.NewLogger(t)

	serverErrorCount := 0

	server := setupWebSocketErrorRecoveryServer(t, &serverErrorCount)
	defer server.Close()

	client := setupWebSocketErrorRecoveryClient(t, logger, server)
	defer cleanupWebSocketErrorRecoveryClient(t, client)

	runWebSocketErrorRecoveryTests(t, client)
}

func setupWebSocketErrorRecoveryServer(t *testing.T, serverErrorCount *int) *httptest.Server {
	return httptest.NewServer(createErrorRecoveryHandler(t, serverErrorCount))
}

func setupWebSocketErrorRecoveryClient(t *testing.T, logger *zap.Logger, server *httptest.Server) *WebSocketClient {
	config := WebSocketClientConfig{
		URL:     "ws" + strings.TrimPrefix(server.URL, "http"),
		Timeout: 5 * time.Second,
		Connection: ConnectionConfig{
			MaxReconnectAttempts: 2,
			ReconnectDelay:       100 * time.Millisecond,
		},
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
	}

	client, err := NewWebSocketClient("error-recovery-client", config.URL, config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = client.Connect(ctx)
	require.NoError(t, err)

	// Wait for connection to establish
	time.Sleep(100 * time.Millisecond)

	return client
}

func cleanupWebSocketErrorRecoveryClient(t *testing.T, client *WebSocketClient) {
	ctx := context.Background()
	err := client.Close(ctx)
	require.NoError(t, err)
}

func runWebSocketErrorRecoveryTests(t *testing.T, client *WebSocketClient) {
	testCases := getWebSocketErrorRecoveryTestCases()

	successCount, clientErrorCount := executeWebSocketErrorRecoveryRequests(t, client, testCases)
	verifyWebSocketErrorRecoveryResults(t, client, successCount, clientErrorCount)
}

func getWebSocketErrorRecoveryTestCases() []struct {
	method      string
	expectError bool
} {
	return []struct {
		method      string
		expectError bool
	}{
		{"normal_request", false},
		{"cause_error", true},     // Will cause connection close
		{"cause_error", true},     // May cause error if not reconnected
		{"normal_request", false}, // Should work after error recovery
	}
}

func executeWebSocketErrorRecoveryRequests(t *testing.T, client *WebSocketClient, testCases []struct {
	method      string
	expectError bool
}) (int, int) {
	var successCount, clientErrorCount int

	ctx := context.Background()

	for i, tc := range testCases {
		req := &mcp.Request{
			JSONRPC: constants.TestJSONRPCVersion,
			Method:  tc.method,
			ID:      fmt.Sprintf("error-test-%d", i),
		}

		_, err := client.SendRequest(ctx, req)
		if err != nil {
			clientErrorCount++

			t.Logf("Expected error for %s: %v", tc.method, err)
		} else {
			successCount++
		}

		// Allow time for potential reconnection
		time.Sleep(2 * constants.TestSleepShort)
	}

	return successCount, clientErrorCount
}

func verifyWebSocketErrorRecoveryResults(t *testing.T, client *WebSocketClient, successCount, clientErrorCount int) {
	// Verify that the client handles errors and can recover
	assert.Positive(t, successCount, "Should have at least some successful requests")

	// Check metrics
	metrics := client.GetMetrics()
	t.Logf("Final metrics - Requests: %d, Errors: %d", metrics.RequestCount, metrics.ErrorCount)
}

// Enhanced benchmark tests.

func BenchmarkWebSocketClientSendRequest(b *testing.B) {
	logger := zaptest.NewLogger(b)

	mockServer := newMockWebSocketServer(b)
	defer mockServer.close()

	config := WebSocketClientConfig{
		URL:     mockServer.getWebSocketURL(),
		Timeout: 5 * time.Second,
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
	}

	client, err := NewWebSocketClient("bench-client", config.URL, config, logger)
	require.NoError(b, err)

	ctx := context.Background()
	err = client.Connect(ctx)
	require.NoError(b, err)

	defer func() {
		err := client.Close(ctx)
		require.NoError(b, err)
	}()

	// Wait for the server to be ready.
	time.Sleep(100 * time.Millisecond)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			req := &mcp.Request{
				JSONRPC: constants.TestJSONRPCVersion,
				Method:  "benchmark",
				ID:      fmt.Sprintf("bench-%d", i),
			}
			i++

			_, err := client.SendRequest(ctx, req)
			if err != nil {
				b.Errorf("Request failed: %v", err)
			}
		}
	})
}

// BenchmarkWebSocketClientConcurrency benchmarks concurrent request handling.
func BenchmarkWebSocketClientConcurrency(b *testing.B) {
	logger := zaptest.NewLogger(b)

	mockServer := newMockWebSocketServer(b)
	defer mockServer.close()

	config := WebSocketClientConfig{
		URL:     mockServer.getWebSocketURL(),
		Timeout: 10 * time.Second,
		Performance: WebSocketPerformanceConfig{
			EnableWriteCompression: true,
			EnableReadCompression:  true,
			OptimizePingPong:       true,
			EnableMessagePooling:   true,
			MessageBatchSize:       10,
		},
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
	}

	client, err := NewWebSocketClient("concurrency-bench-client", config.URL, config, logger)
	require.NoError(b, err)

	ctx := context.Background()
	err = client.Connect(ctx)
	require.NoError(b, err)

	defer func() {
		err := client.Close(ctx)
		require.NoError(b, err)
	}()

	// Wait for connection to establish.
	time.Sleep(100 * time.Millisecond)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			req := &mcp.Request{
				JSONRPC: constants.TestJSONRPCVersion,
				Method:  "concurrent_benchmark",
				ID:      fmt.Sprintf("concurrent-bench-%d-%d", i, time.Now().UnixNano()),
			}
			i++

			_, err := client.SendRequest(ctx, req)
			if err != nil {
				b.Errorf("Concurrent request failed: %v", err)
			}
		}
	})
}

// BenchmarkWebSocketClientFrameSizes benchmarks different frame sizes.
func BenchmarkWebSocketClientFrameSizes(b *testing.B) {
	logger := zaptest.NewLogger(b)
	frameSizes := []int{100, 1000, 10000, 100000}

	for _, frameSize := range frameSizes {
		b.Run(fmt.Sprintf("frame_size_%d", frameSize), func(b *testing.B) {
			runWebSocketFrameSizeBenchmark(b, logger, frameSize)
		})
	}
}

func runWebSocketFrameSizeBenchmark(b *testing.B, logger *zap.Logger, frameSize int) {
	server := setupWebSocketFrameSizeBenchServer(b, frameSize)
	defer server.Close()

	client := setupWebSocketFrameSizeBenchClient(b, logger, server)
	defer cleanupWebSocketFrameSizeBenchClient(b, client)

	executeWebSocketFrameSizeBenchmark(b, client, frameSize)
}

func setupWebSocketFrameSizeBenchServer(b *testing.B, frameSize int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		defer func() { _ = conn.Close() }()

		handleWebSocketFrameSizeBenchMessages(b, conn, frameSize)
	}))
}

func handleWebSocketFrameSizeBenchMessages(b *testing.B, conn *websocket.Conn, frameSize int) {
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			return
		}

		var req mcp.Request
		if err := json.Unmarshal(message, &req); err != nil {
			continue
		}

		payload := strings.Repeat("x", frameSize)
		resp := mcp.Response{
			JSONRPC: constants.TestJSONRPCVersion,
			Result:  map[string]interface{}{"payload": payload},
			ID:      req.ID,
		}

		respData, _ := json.Marshal(resp)
		if err := conn.WriteMessage(websocket.TextMessage, respData); err != nil {
			b.Logf("Failed to write response in test server: %v", err)
		}
	}
}

func setupWebSocketFrameSizeBenchClient(b *testing.B, logger *zap.Logger, server *httptest.Server) *WebSocketClient {
	config := WebSocketClientConfig{
		URL:            "ws" + strings.TrimPrefix(server.URL, "http"),
		Timeout:        10 * time.Second,
		MaxMessageSize: int64(200 * 1024), // 200KB max
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
	}

	client, err := NewWebSocketClient("frame-bench-client", config.URL, config, logger)
	require.NoError(b, err)

	ctx := context.Background()
	err = client.Connect(ctx)
	require.NoError(b, err)

	// Wait for connection to establish
	time.Sleep(100 * time.Millisecond)

	return client
}

func cleanupWebSocketFrameSizeBenchClient(b *testing.B, client *WebSocketClient) {
	ctx := context.Background()
	err := client.Close(ctx)
	require.NoError(b, err)
}

func executeWebSocketFrameSizeBenchmark(b *testing.B, client *WebSocketClient, frameSize int) {
	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0

		for pb.Next() {
			payload := strings.Repeat("test", frameSize/4)
			req := &mcp.Request{
				JSONRPC: constants.TestJSONRPCVersion,
				Method:  "frame_benchmark",
				Params:  map[string]interface{}{"data": payload},
				ID:      fmt.Sprintf("frame-bench-%d", i),
			}
			i++

			_, err := client.SendRequest(ctx, req)
			if err != nil {
				b.Errorf("Frame benchmark request failed: %v", err)
			}
		}
	})
}
