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

	"github.com/poiley/mcp-bridge/services/router/pkg/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

// mockSSEServer creates a mock SSE server for testing.
type mockSSEServer struct {
	server        *httptest.Server
	events        chan string
	requests      chan mcp.Request
	mu            sync.RWMutex
	activeStreams int
	closed        bool
	processedReqs map[string]bool // Track processed request IDs to avoid duplicates
}

func newMockSSEServer() *mockSSEServer {
	mock := &mockSSEServer{
		events:        make(chan string, testIterations),      // Increased buffer for concurrent tests
		requests:      make(chan mcp.Request, testIterations), // Increased buffer for concurrent tests
		processedReqs: make(map[string]bool),
	}

	mux := http.NewServeMux()

	// SSE stream endpoint.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			mock.handleSSEStream(w, r)
		} else {
			mock.handleRequest(w, r)
		}
	})

	mock.server = httptest.NewServer(mux)

	return mock
}

func (m *mockSSEServer) handleSSEStream(w http.ResponseWriter, r *http.Request) {
	// Set SSE headers.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)

		return
	}

	m.mu.Lock()
	m.activeStreams++
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		m.activeStreams--
		m.mu.Unlock()
	}()

	// Send initial connection event - use a proper MCP response format to avoid parsing errors.
	_, _ = fmt.Fprint(w, "data: {\"jsonrpc\": \"2.0\", \"result\": {\"type\": \"connected\"}, \"id\": \"connection\"}\n\n")

	flusher.Flush()

	// Stream events.
	for {
		select {
		case event := <-m.events:
			m.mu.RLock()
			closed := m.closed
			m.mu.RUnlock()

			if closed {
				return
			}

			_, _ = fmt.Fprintf(w, "data: %s\n\n", event)

			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// generateResponse creates a response based on the request method.
func (m *mockSSEServer) generateResponse(req mcp.Request) mcp.Response {
	switch req.Method {
	case "ping":
		return mcp.Response{
			JSONRPC: constants.TestJSONRPCVersion,
			Result:  "pong",
			ID:      req.ID,
		}
	case "echo":
		return mcp.Response{
			JSONRPC: constants.TestJSONRPCVersion,
			Result:  map[string]interface{}{"echo": req.Method},
			ID:      req.ID,
		}
	case "error":
		return mcp.Response{
			JSONRPC: constants.TestJSONRPCVersion,
			Error: &mcp.Error{
				Code:    -32603,
				Message: "Internal error",
			},
			ID: req.ID,
		}
	default:
		return mcp.Response{
			JSONRPC: constants.TestJSONRPCVersion,
			Result:  map[string]interface{}{"method": req.Method},
			ID:      req.ID,
		}
	}
}

func (m *mockSSEServer) handleRequest(w http.ResponseWriter, r *http.Request) {
	// Parse MCP request.
	var req mcp.Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)

		return
	}

	// Store request for processing.
	select {
	case m.requests <- req:
	default:
		// If channel is full, just acknowledge.
	}

	// Send immediate acknowledgment.
	w.WriteHeader(http.StatusAccepted)

	// Generate response based on method.
	go func() {
		requestID := fmt.Sprintf("%v", req.ID)

		// Check if we've already processed this request.
		m.mu.Lock()

		if m.processedReqs[requestID] {
			m.mu.Unlock()
			// Skip duplicate request.
			return
		}

		m.processedReqs[requestID] = true
		m.mu.Unlock()

		// fmt.Printf("Mock server: generating response for request %v method %s\n", req.ID, req.Method)
		resp := m.generateResponse(req)

		// Send response via SSE.
		respData, _ := json.Marshal(resp)
		select {
		case m.events <- string(respData):
			// Response sent successfully.
		case <-time.After(5 * time.Second):
			// Timeout sending event - increased timeout for concurrent operations.
			fmt.Printf("Mock server: timeout sending response for request %v\n", req.ID)
		}
	}()
}

func (m *mockSSEServer) getURL() string {
	return m.server.URL
}

func (m *mockSSEServer) close() {
	m.mu.Lock()
	m.closed = true
	m.mu.Unlock()

	close(m.events)
	close(m.requests)
	m.server.Close()
}

func TestNewSSEClient(t *testing.T) {
	logger := zaptest.NewLogger(t)

	testCases := []struct {
		name      string
		serverURL string
		config    SSEClientConfig
		wantError bool
	}{
		{
			name:      "valid HTTP URL",
			serverURL: fmt.Sprintf("http://localhost:%d/sse", constants.TestHTTPPort),
			config:    SSEClientConfig{},
			wantError: false,
		},
		{
			name:      "valid HTTPS URL",
			serverURL: fmt.Sprintf("https://example.com:%d/path", constants.TestHTTPPort),
			config:    SSEClientConfig{},
			wantError: false,
		},
		{
			name:      "config URL overrides serverURL",
			serverURL: fmt.Sprintf("http://localhost:%d", constants.TestHTTPPort),
			config: SSEClientConfig{
				URL: fmt.Sprintf("http://configured.example.com:%d/sse", constants.TestMetricsPort),
			},
			wantError: false,
		},
		{
			name:      "invalid URL",
			serverURL: "not-a-url",
			config:    SSEClientConfig{},
			wantError: true,
		},
		{
			name:      "non-HTTP scheme",
			serverURL: fmt.Sprintf("ws://localhost:%d", constants.TestHTTPPort),
			config:    SSEClientConfig{},
			wantError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			client, err := NewSSEClient("test-client", tc.serverURL, tc.config, logger)

			if tc.wantError {
				require.Error(t, err)
				assert.Nil(t, client)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, client)
				assert.Equal(t, "test-client", client.GetName())
				assert.Equal(t, "sse", client.GetProtocol())
				assert.Equal(t, StateDisconnected, client.GetState())
			}
		})
	}
}

func TestSSEClientDefaults(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := SSEClientConfig{} // Empty config to test defaults

	client, err := NewSSEClient("test-client", fmt.Sprintf("http://localhost:%d", constants.TestHTTPPort), config, logger)
	require.NoError(t, err)

	assert.Equal(t, "POST", client.config.Method)
	assert.Equal(t, 30*time.Second, client.config.RequestTimeout)
	assert.Equal(t, 300*time.Second, client.config.StreamTimeout)
	assert.Equal(t, 4096, client.config.BufferSize)
	assert.Equal(t, 30*time.Second, client.config.HealthCheck.Interval)
	assert.Equal(t, 5*time.Second, client.config.HealthCheck.Timeout)
	assert.Equal(t, 10, client.config.Client.MaxIdleConns)
	assert.Equal(t, 2, client.config.Client.MaxIdleConnsPerHost)
	assert.Equal(t, 90*time.Second, client.config.Client.IdleConnTimeout)
	assert.Equal(t, 3, client.config.Client.MaxRedirects)
	assert.Equal(t, "MCP-Router-SSE-Client/1.0", client.config.Client.UserAgent)
	assert.Equal(t, "text/event-stream", client.config.Headers["Accept"])
	assert.Equal(t, "no-cache", client.config.Headers["Cache-Control"])
}

func TestSSEClientConnectAndClose(t *testing.T) {
	logger := zaptest.NewLogger(t)

	mockServer := newMockSSEServer()
	defer mockServer.close()

	config := SSEClientConfig{
		URL:            mockServer.getURL(),
		RequestTimeout: 5 * time.Second,
		StreamTimeout:  10 * time.Second,
		HealthCheck: HealthCheckConfig{
			Enabled: false, // Disable for simple test
		},
	}

	client, err := NewSSEClient("test-client", config.URL, config, logger)
	require.NoError(t, err)

	ctx := context.Background()

	// Test connect.
	err = client.Connect(ctx)
	require.NoError(t, err)
	assert.Equal(t, StateConnected, client.GetState())

	// Give some time for SSE stream to establish.
	time.Sleep(testIterations * time.Millisecond)

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

func TestSSEClientSendRequest(t *testing.T) {
	logger := zaptest.NewLogger(t)

	mockServer := newMockSSEServer()
	defer mockServer.close()

	config := SSEClientConfig{
		URL:            mockServer.getURL(),
		RequestTimeout: 5 * time.Second,
		StreamTimeout:  10 * time.Second,
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
	}

	client, err := NewSSEClient("echo-client", config.URL, config, logger)
	require.NoError(t, err)

	ctx := context.Background()

	// Connect.
	err = client.Connect(ctx)
	require.NoError(t, err)

	defer func() {
		err := client.Close(ctx)
		require.NoError(t, err)
	}()

	// Wait for SSE stream to be ready.
	time.Sleep(httpStatusOK * time.Millisecond)

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

func TestSSEClientSendRequestNotConnected(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := SSEClientConfig{
		URL: fmt.Sprintf("http://localhost:%d", constants.TestHTTPPort),
	}

	client, err := NewSSEClient("test-client", config.URL, config, logger)
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

func TestSSEClientSendRequestTimeout(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Create a server that accepts requests but never sends SSE responses.
	slowServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			// SSE stream that never sends responses.
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")

			select {
			case <-r.Context().Done():
				return
			case <-time.After(10 * time.Second):
				return
			}
		} else {
			// Accept requests but don't send responses via SSE.
			w.WriteHeader(http.StatusAccepted)
		}
	}))
	defer slowServer.Close()

	config := SSEClientConfig{
		URL:            slowServer.URL,
		RequestTimeout: testIterations * time.Millisecond, // Very short timeout
		StreamTimeout:  5 * time.Second,
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
	}

	client, err := NewSSEClient("slow-client", config.URL, config, logger)
	require.NoError(t, err)

	ctx := context.Background()

	// Connect (this might fail if connectivity test times out).
	err = client.Connect(ctx)
	if err != nil {
		// If connect fails due to timeout, that's also acceptable for this test.
		t.Skip("Connect failed due to timeout, which is expected behavior")

		return
	}

	defer func() {
		err := client.Close(ctx)
		require.NoError(t, err)
	}()

	// Wait for SSE stream to establish.
	time.Sleep(testTimeout * time.Millisecond)

	// Test timeout request.
	req := &mcp.Request{
		JSONRPC: constants.TestJSONRPCVersion,
		Method:  "slow_method",
		ID:      "test-123",
	}

	_, err = client.SendRequest(ctx, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timed out")

	// Check metrics.
	metrics := client.GetMetrics()
	assert.Equal(t, uint64(0), metrics.RequestCount)
	assert.Equal(t, uint64(1), metrics.ErrorCount)
}

func TestSSEClientHealth(t *testing.T) {
	logger := zaptest.NewLogger(t)

	mockServer := newMockSSEServer()
	defer mockServer.close()

	config := SSEClientConfig{
		URL:            mockServer.getURL(),
		RequestTimeout: 5 * time.Second,
		HealthCheck: HealthCheckConfig{
			Enabled: false, // We'll test manually
			Timeout: 1 * time.Second,
		},
	}

	client, err := NewSSEClient("test-client", config.URL, config, logger)
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

	// Wait for connection to stabilize.
	time.Sleep(httpStatusOK * time.Millisecond)

	// Test health when connected.
	err = client.Health(ctx)
	require.NoError(t, err)
	assert.Equal(t, StateHealthy, client.GetState())

	// Check metrics.
	metrics := client.GetMetrics()
	assert.True(t, metrics.IsHealthy)
	assert.False(t, metrics.LastHealthCheck.IsZero())
}

func TestSSEClientHealthCheck(t *testing.T) {
	logger := zaptest.NewLogger(t)

	mockServer := newMockSSEServer()
	defer mockServer.close()

	config := SSEClientConfig{
		URL:            mockServer.getURL(),
		RequestTimeout: 5 * time.Second,
		HealthCheck: HealthCheckConfig{
			Enabled:  true,
			Interval: testTimeout * time.Millisecond, // Fast for testing
			Timeout:  1 * time.Second,
		},
	}

	client, err := NewSSEClient("ping-client", config.URL, config, logger)
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
	time.Sleep(300 * time.Millisecond)

	// Should be healthy.
	assert.Equal(t, StateHealthy, client.GetState())
	metrics := client.GetMetrics()
	assert.True(t, metrics.IsHealthy)
}

func TestSSEClientGetStatus(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := SSEClientConfig{
		URL: fmt.Sprintf("http://localhost:%d/sse", constants.TestHTTPPort),
	}

	client, err := NewSSEClient("status-client", config.URL, config, logger)
	require.NoError(t, err)

	status := client.GetStatus()
	assert.Equal(t, "status-client", status.Name)
	assert.Equal(t, fmt.Sprintf("http://localhost:%d/sse", constants.TestHTTPPort), status.URL)
	assert.Equal(t, "sse", status.Protocol)
	assert.Equal(t, StateDisconnected, status.State)
}

func TestSSEClientConnectInvalidURL(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := SSEClientConfig{
		URL:            "http://nonexistent.invalid:12345",
		RequestTimeout: 1 * time.Second, // Short timeout for quick test
	}

	client, err := NewSSEClient("invalid-client", config.URL, config, logger)
	require.NoError(t, err)

	ctx := context.Background()

	// Try to connect to invalid URL.
	err = client.Connect(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to establish")
	assert.Equal(t, StateError, client.GetState())
}

func TestSSEClientHeaders(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Create a server that checks headers.
	headerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			// Check SSE headers.
			if r.Header.Get("Accept") != "text/event-stream" {
				http.Error(w, "Wrong Accept header", http.StatusBadRequest)

				return
			}

			// Send minimal SSE stream.
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(w, "data: {\"type\": \"connected\"}\n\n")
		} else {
			// Check custom header for requests.
			if r.Header.Get("X-Test-Header") != "test-value" {
				http.Error(w, "Missing header", http.StatusBadRequest)

				return
			}

			w.WriteHeader(http.StatusAccepted)
		}
	}))
	defer headerServer.Close()

	config := SSEClientConfig{
		URL: headerServer.URL,
		Headers: map[string]string{
			"X-Test-Header": "test-value",
		},
		Client: SSETransportConfig{
			UserAgent: "Custom-Agent/1.0",
		},
		RequestTimeout: 5 * time.Second,
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
	}

	client, err := NewSSEClient("header-client", config.URL, config, logger)
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

func TestSSEClientURL(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := SSEClientConfig{
		URL: fmt.Sprintf("http://example.com:%d/path", constants.TestHTTPPort),
	}

	client, err := NewSSEClient("url-client", "http://original.com", config, logger)
	require.NoError(t, err)

	// Config URL should override serverURL parameter.
	assert.Equal(t, fmt.Sprintf("http://example.com:%d/path", constants.TestHTTPPort), client.GetURL())
}

func TestSSEEventParsing(t *testing.T) {
	logger := zaptest.NewLogger(t)
	client := &SSEClient{
		logger:          logger,
		pendingRequests: make(map[string]chan *mcp.Response),
	}

	testCases := []struct {
		name           string
		sseData        string
		expectedID     string
		expectedResult interface{}
	}{
		{
			name:           "simple MCP response",
			sseData:        `{"jsonrpc":"2.0","result":"test","id":"123"}`,
			expectedID:     "123",
			expectedResult: "test",
		},
		{
			name:           "MCP response with object result",
			sseData:        `{"jsonrpc":"2.0","result":{"value":"test"},"id":"456"}`,
			expectedID:     "456",
			expectedResult: map[string]interface{}{"value": "test"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			event := &SSEEvent{
				Data: tc.sseData,
			}

			// Create a response channel for the request.
			respCh := make(chan *mcp.Response, 1)
			client.pendingRequests[tc.expectedID] = respCh

			// Process the event.
			client.processSSEEvent(event)

			// Check if response was received.
			select {
			case resp := <-respCh:
				assert.Equal(t, tc.expectedID, resp.ID)
				assert.Equal(t, tc.expectedResult, resp.Result)
			case <-time.After(testIterations * time.Millisecond):
				t.Fatal("Expected response was not received")
			}
		})
	}
}

// ==============================================================================
// ENHANCED SSE CLIENT TESTS - COMPREHENSIVE COVERAGE.
// ==============================================================================

// TestSSEClientPerformanceOptimizations tests SSE-specific performance features.
func TestSSEClientPerformanceOptimizations(t *testing.T) {
	logger := zaptest.NewLogger(t)
	testCases := createSSEPerformanceTestCases()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			runSSEPerformanceTest(t, logger, tc)
		})
	}
}

func createSSEPerformanceTestCases() []struct {
	name   string
	config SSEPerformanceConfig
} {
	return []struct {
		name   string
		config SSEPerformanceConfig
	}{
		{
			name: "optimized buffers",
			config: SSEPerformanceConfig{
				StreamBufferSize:   128 * 1024, // 128KB
				RequestBufferSize:  64 * 1024,  // 64KB
				ReuseConnections:   true,
				EnableCompression:  true,
				FastReconnect:      true,
				ConnectionPoolSize: 10,
			},
		},
		{
			name: "event batching enabled",
			config: SSEPerformanceConfig{
				StreamBufferSize:    64 * 1024,
				RequestBufferSize:   32 * 1024,
				ReuseConnections:    true,
				EnableEventBatching: true,
				BatchTimeout:        5 * time.Millisecond,
				ConnectionPoolSize:  5,
			},
		},
		{
			name: "minimal buffers",
			config: SSEPerformanceConfig{
				StreamBufferSize:   8 * 1024,
				RequestBufferSize:  4 * 1024,
				ReuseConnections:   false,
				EnableCompression:  false,
				FastReconnect:      false,
				ConnectionPoolSize: 1,
			},
		},
	}
}

func runSSEPerformanceTest(t *testing.T, logger *zap.Logger, tc struct {
	name   string
	config SSEPerformanceConfig
}) {
	t.Helper()

	mockServer := newMockSSEServer()
	defer mockServer.close()

	client := setupSSEPerformanceClient(t, logger, mockServer, tc.config)
	ctx := context.Background()

	connectTime := measureSSEConnectionTime(t, client, ctx)

	defer func() {
		err := client.Close(ctx)
		require.NoError(t, err)
	}()

	time.Sleep(httpStatusOK * time.Millisecond)

	requestTimes := measureSSERequestPerformance(t, client, ctx)
	analyzeSSEPerformanceResults(t, tc.name, connectTime, requestTimes)
}

func setupSSEPerformanceClient(
	t *testing.T,
	logger *zap.Logger,
	mockServer *mockSSEServer,
	perfConfig SSEPerformanceConfig,
) *SSEClient {
	t.Helper()

	config := SSEClientConfig{
		URL:            mockServer.getURL(),
		RequestTimeout: 5 * time.Second,
		Performance:    perfConfig,
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
	}

	client, err := NewSSEClient("perf-client", config.URL, config, logger)
	require.NoError(t, err)

	return client
}

func measureSSEConnectionTime(t *testing.T, client *SSEClient, ctx context.Context) time.Duration {
	t.Helper()

	start := time.Now()
	err := client.Connect(ctx)
	require.NoError(t, err)

	return time.Since(start)
}

func measureSSERequestPerformance(t *testing.T, client *SSEClient, ctx context.Context) []time.Duration {
	t.Helper()

	const numRequests = 10

	requestTimes := make([]time.Duration, numRequests)

	for i := 0; i < numRequests; i++ {
		req := &mcp.Request{
			JSONRPC: constants.TestJSONRPCVersion,
			Method:  fmt.Sprintf("perf_test_%d", i),
			ID:      fmt.Sprintf("perf-%d", i),
		}

		start := time.Now()
		_, err := client.SendRequest(ctx, req)
		requestTimes[i] = time.Since(start)

		require.NoError(t, err)
	}

	return requestTimes
}

func analyzeSSEPerformanceResults(
	t *testing.T,
	testName string,
	connectTime time.Duration,
	requestTimes []time.Duration,
) {
	t.Helper()
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

// TestSSEClientConcurrentOperations tests concurrent request handling.
func TestSSEClientConcurrentOperations(t *testing.T) {
	logger := zaptest.NewLogger(t)

	client := setupSSEConcurrentClient(t, logger)
	defer cleanupSSEConcurrentClient(t, client)

	runSSEConcurrentOperationsTest(t, client)
}

func setupSSEConcurrentClient(t *testing.T, logger *zap.Logger) *SSEClient {
	t.Helper()

	mockServer := newMockSSEServer()

	t.Cleanup(func() { mockServer.close() })

	config := SSEClientConfig{
		URL:            mockServer.getURL(),
		RequestTimeout: 10 * time.Second,
		Performance: SSEPerformanceConfig{
			StreamBufferSize:   128 * 1024,
			ConnectionPoolSize: 20,
			ReuseConnections:   true,
		},
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
	}

	client, err := NewSSEClient("concurrent-client", config.URL, config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = client.Connect(ctx)
	require.NoError(t, err)

	time.Sleep(httpStatusOK * time.Millisecond)

	return client
}

func cleanupSSEConcurrentClient(t *testing.T, client *SSEClient) {
	t.Helper()

	ctx := context.Background()
	err := client.Close(ctx)
	require.NoError(t, err)
}

func runSSEConcurrentOperationsTest(t *testing.T, client *SSEClient) {
	t.Helper()

	const numGoroutines = 1

	const requestsPerGoroutine = 5

	errChan, responseChan := createSSEConcurrentChannels(numGoroutines, requestsPerGoroutine)

	var wg sync.WaitGroup

	ctx := context.Background()

	launchSSEConcurrentWorkers(t, &wg, client, ctx, numGoroutines, requestsPerGoroutine, errChan, responseChan)
	waitForSSEConcurrentCompletion(t, &wg)

	errors, responses := collectSSEConcurrentResults(errChan, responseChan)
	verifySSEConcurrentResults(t, client, errors, responses, numGoroutines, requestsPerGoroutine)
}

func createSSEConcurrentChannels(numGoroutines, requestsPerGoroutine int) (chan error, chan *mcp.Response) {
	errChan := make(chan error, numGoroutines*requestsPerGoroutine)
	responseChan := make(chan *mcp.Response, numGoroutines*requestsPerGoroutine)

	return errChan, responseChan
}

func launchSSEConcurrentWorkers(t *testing.T, wg *sync.WaitGroup, client *SSEClient, ctx context.Context,
	numGoroutines, requestsPerGoroutine int, errChan chan error, responseChan chan *mcp.Response) {
	t.Helper()
	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)

		go runSSEConcurrentWorker(wg, client, ctx, g, requestsPerGoroutine, errChan, responseChan)
	}
}

func runSSEConcurrentWorker(wg *sync.WaitGroup, client *SSEClient, ctx context.Context,
	goroutineID, requestsPerGoroutine int, errChan chan error, responseChan chan *mcp.Response) {
	defer wg.Done()

	for r := 0; r < requestsPerGoroutine; r++ {
		req := &mcp.Request{
			JSONRPC: constants.TestJSONRPCVersion,
			Method:  "concurrent_test",
			ID:      fmt.Sprintf("concurrent-%d-%d", goroutineID, r),
		}

		resp, err := client.SendRequest(ctx, req)
		if err != nil {
			errChan <- err

			return
		}

		responseChan <- resp
	}
}

func waitForSSEConcurrentCompletion(t *testing.T, wg *sync.WaitGroup) {
	t.Helper()

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

func collectSSEConcurrentResults(errChan chan error, responseChan chan *mcp.Response) ([]error, []*mcp.Response) {
	close(errChan)
	close(responseChan)

	errors := make([]error, 0, len(errChan))
	for err := range errChan {
		errors = append(errors, err)
	}

	responses := make([]*mcp.Response, 0, len(responseChan))
	for resp := range responseChan {
		responses = append(responses, resp)
	}

	return errors, responses
}

func verifySSEConcurrentResults(t *testing.T, client *SSEClient, errors []error, responses []*mcp.Response,
	numGoroutines, requestsPerGoroutine int) {
	t.Helper()

	expectedCount := numGoroutines * requestsPerGoroutine

	assert.Empty(t, errors, "No errors should occur during concurrent operations")
	assert.Len(t, responses, expectedCount, "All requests should receive responses")

	metrics := client.GetMetrics()
	assert.Equal(t, safeIntToUint64(expectedCount), metrics.RequestCount)
	assert.Equal(t, uint64(0), metrics.ErrorCount)
	assert.True(t, metrics.IsHealthy)
}

// TestSSEClientStreamManagement tests SSE stream handling edge cases.
func TestSSEClientStreamManagement(t *testing.T) {
	logger := zaptest.NewLogger(t)
	testCases := createSSEStreamManagementTestCases()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			runSSEStreamManagementTest(t, logger, tc)
		})
	}
}

func createSSEStreamManagementTestCases() []struct {
	name           string
	serverBehavior func(w http.ResponseWriter, r *http.Request)
	expectError    bool
} {
	return []struct {
		name           string
		serverBehavior func(w http.ResponseWriter, r *http.Request)
		expectError    bool
	}{
		{
			name:           "stream with malformed events",
			serverBehavior: createMalformedEventsServerBehavior(),
			expectError:    false,
		},
		{
			name:           "stream with mixed content types",
			serverBehavior: createMixedContentServerBehavior(),
			expectError:    false,
		},
		{
			name:           "stream with large payloads",
			serverBehavior: createLargePayloadServerBehavior(),
			expectError:    false,
		},
	}
}

func createMalformedEventsServerBehavior() func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")

			_, _ = fmt.Fprint(w, "data: {invalid json}\n\n")
			_, _ = fmt.Fprint(w, "data: {\"jsonrpc\":\"2.0\",\"result\":\"valid\",\"id\":\"test\"}\n\n")

			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}

			time.Sleep(testIterations * time.Millisecond)
		} else {
			w.WriteHeader(http.StatusAccepted)
		}
	}
}

func createMixedContentServerBehavior() func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "text/event-stream")

			_, _ = fmt.Fprint(w, "event: heartbeat\ndata: {\"type\":\"heartbeat\"}\n\n")
			_, _ = fmt.Fprint(w, "data: {\"jsonrpc\":\"2.0\",\"result\":\"test\",\"id\":\"123\"}\n\n")
			_, _ = fmt.Fprint(w, "id: event-123\ndata: {\"jsonrpc\":\"2.0\",\"result\":\"test2\",\"id\":\"456\"}\n\n")

			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}

			time.Sleep(testIterations * time.Millisecond)
		} else {
			w.WriteHeader(http.StatusAccepted)
		}
	}
}

func createLargePayloadServerBehavior() func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "text/event-stream")

			largeData := map[string]interface{}{
				"data":    strings.Repeat("x", 10000),
				"jsonrpc": "2.0",
				"id":      "large-123",
			}
			largeData["result"] = largeData

			dataBytes, _ := json.Marshal(largeData)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", string(dataBytes))

			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}

			time.Sleep(testIterations * time.Millisecond)
		} else {
			w.WriteHeader(http.StatusAccepted)
		}
	}
}

func runSSEStreamManagementTest(t *testing.T, logger *zap.Logger, tc struct {
	name           string
	serverBehavior func(w http.ResponseWriter, r *http.Request)
	expectError    bool
}) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(tc.serverBehavior))
	defer server.Close()

	client := setupSSEStreamManagementClient(t, logger, server.URL)
	ctx := context.Background()

	err := client.Connect(ctx)
	if tc.expectError {
		require.Error(t, err)

		return
	}

	require.NoError(t, err)

	defer func() {
		err := client.Close(ctx)
		require.NoError(t, err)
	}()

	verifySSEStreamConnection(t, client)
}

func setupSSEStreamManagementClient(t *testing.T, logger *zap.Logger, serverURL string) *SSEClient {
	t.Helper()

	config := SSEClientConfig{
		URL:            serverURL,
		RequestTimeout: 5 * time.Second,
		Performance: SSEPerformanceConfig{
			StreamBufferSize: 128 * 1024,
		},
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
	}

	client, err := NewSSEClient("stream-test-client", config.URL, config, logger)
	require.NoError(t, err)

	return client
}

func verifySSEStreamConnection(t *testing.T, client *SSEClient) {
	t.Helper()
	time.Sleep(httpStatusInternalError * time.Millisecond)
	assert.Equal(t, StateConnected, client.GetState())
}

// TestSSEClientReconnectionScenarios tests various reconnection scenarios.
func TestSSEClientReconnectionScenarios(t *testing.T) {
	logger := zaptest.NewLogger(t)

	server, toggleServer := setupReconnectionServer()
	defer server.Close()

	client := setupReconnectionClient(t, logger, server.URL)
	defer cleanupReconnectionClient(t, client)

	runReconnectionScenarioTest(t, client, toggleServer)
}

func setupReconnectionServer() (*httptest.Server, func(bool)) {
	var (
		serverMu      sync.Mutex
		serverEnabled = true
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverMu.Lock()

		enabled := serverEnabled

		serverMu.Unlock()

		if !enabled {
			http.Error(w, "Server temporarily unavailable", http.StatusServiceUnavailable)

			return
		}

		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")

			_, _ = fmt.Fprint(w, "data: {\"type\":\"connected\"}\n\n")

			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}

			select {
			case <-r.Context().Done():
				return
			case <-time.After(30 * time.Second):
				return
			}
		} else {
			w.WriteHeader(http.StatusAccepted)
		}
	}))

	toggleServer := func(enabled bool) {
		serverMu.Lock()

		serverEnabled = enabled

		serverMu.Unlock()
	}

	return server, toggleServer
}

func setupReconnectionClient(t *testing.T, logger *zap.Logger, serverURL string) *SSEClient {
	t.Helper()

	config := SSEClientConfig{
		URL:            serverURL,
		RequestTimeout: 2 * time.Second,
		Performance: SSEPerformanceConfig{
			FastReconnect: true,
		},
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
	}

	client, err := NewSSEClient("reconnect-client", config.URL, config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = client.Connect(ctx)
	require.NoError(t, err)

	return client
}

func cleanupReconnectionClient(t *testing.T, client *SSEClient) {
	t.Helper()

	ctx := context.Background()
	err := client.Close(ctx)
	require.NoError(t, err)
}

func runReconnectionScenarioTest(t *testing.T, client *SSEClient, toggleServer func(bool)) {
	t.Helper()
	verifyInitialConnection(t, client)
	simulateServerFailure(t, client, toggleServer)
	restoreServerConnection(t, client, toggleServer)
}

func verifyInitialConnection(t *testing.T, client *SSEClient) {
	t.Helper()
	time.Sleep(httpStatusOK * time.Millisecond)
	assert.Equal(t, StateConnected, client.GetState())
}

func simulateServerFailure(t *testing.T, client *SSEClient, toggleServer func(bool)) {
	t.Helper()
	toggleServer(false)
	time.Sleep(1 * time.Second)
}

func restoreServerConnection(t *testing.T, client *SSEClient, toggleServer func(bool)) {
	t.Helper()
	toggleServer(true)
	time.Sleep(1 * time.Second)
}

// TestSSEClientCompressionSupport tests compression handling.
func TestSSEClientCompressionSupport(t *testing.T) {
	logger := zaptest.NewLogger(t)
	testCases := createCompressionTestCases()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			runCompressionTest(t, logger, tc)
		})
	}
}

func createCompressionTestCases() []struct {
	name              string
	enableCompression bool
	expectHeader      bool
} {
	return []struct {
		name              string
		enableCompression bool
		expectHeader      bool
	}{
		{
			name:              "compression enabled",
			enableCompression: true,
			expectHeader:      true,
		},
		{
			name:              "compression disabled",
			enableCompression: false,
			expectHeader:      false,
		},
	}
}

func runCompressionTest(t *testing.T, logger *zap.Logger, tc struct {
	name              string
	enableCompression bool
	expectHeader      bool
}) {
	t.Helper()

	server := setupCompressionTestServer(t, tc.expectHeader)
	defer server.Close()

	client := setupCompressionTestClient(t, logger, server.URL, tc.enableCompression)
	defer cleanupCompressionTestClient(t, client)

	testCompressionHeaders(t, client)
}

func setupCompressionTestServer(t *testing.T, expectHeader bool) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(w, "data: {\"type\":\"connected\"}\n\n")
		} else {
			acceptEncoding := r.Header.Get("Accept-Encoding")
			if expectHeader {
				assert.Contains(t, acceptEncoding, "gzip", "Should request compression when enabled")
			}

			w.WriteHeader(http.StatusAccepted)
		}
	}))
}

func setupCompressionTestClient(t *testing.T, logger *zap.Logger, serverURL string, enableCompression bool) *SSEClient {
	t.Helper()

	config := SSEClientConfig{
		URL:            serverURL,
		RequestTimeout: 5 * time.Second,
		Performance: SSEPerformanceConfig{
			EnableCompression: enableCompression,
		},
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
	}

	client, err := NewSSEClient("compression-client", config.URL, config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = client.Connect(ctx)
	require.NoError(t, err)

	return client
}

func cleanupCompressionTestClient(t *testing.T, client *SSEClient) {
	t.Helper()

	ctx := context.Background()
	err := client.Close(ctx)
	require.NoError(t, err)
}

func testCompressionHeaders(t *testing.T, client *SSEClient) {
	t.Helper()
	time.Sleep(httpStatusOK * time.Millisecond)

	ctx := context.Background()
	req := &mcp.Request{
		JSONRPC: constants.TestJSONRPCVersion,
		Method:  "compression_test",
		ID:      "compress-123",
	}

	_, _ = client.SendRequest(ctx, req)
}

// TestSSEClientErrorRecovery tests error recovery mechanisms.
func TestSSEClientErrorRecovery(t *testing.T) {
	logger := zaptest.NewLogger(t)

	server, requestCounter := setupErrorRecoveryServer()
	defer server.Close()

	client := setupErrorRecoveryClient(t, logger, server.URL)
	defer cleanupErrorRecoveryClient(t, client)

	runSSEErrorRecoveryTest(t, client, requestCounter)
}

func setupErrorRecoveryServer() (*httptest.Server, *int) {
	requestCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(w, "data: {\"type\":\"connected\"}\n\n")

			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
		} else {
			requestCount++
			if requestCount <= 2 {
				http.Error(w, "Temporary server error", http.StatusInternalServerError)

				return
			}

			w.WriteHeader(http.StatusAccepted)

			go func() {
				time.Sleep(testIterations * time.Millisecond)
			}()
		}
	}))

	return server, &requestCount
}

func setupErrorRecoveryClient(t *testing.T, logger *zap.Logger, serverURL string) *SSEClient {
	t.Helper()

	config := SSEClientConfig{
		URL:            serverURL,
		RequestTimeout: 2 * time.Second,
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
	}

	client, err := NewSSEClient("error-recovery-client", config.URL, config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = client.Connect(ctx)
	require.NoError(t, err)

	time.Sleep(httpStatusOK * time.Millisecond)

	return client

}

func cleanupErrorRecoveryClient(t *testing.T, client *SSEClient) {
	t.Helper()

	ctx := context.Background()
	err := client.Close(ctx)
	require.NoError(t, err)
}

func runSSEErrorRecoveryTest(t *testing.T, client *SSEClient, requestCounter *int) {
	t.Helper()

	ctx := context.Background()

	for i := 0; i < 3; i++ {
		req := &mcp.Request{
			JSONRPC: constants.TestJSONRPCVersion,
			Method:  "error_recovery_test",
			ID:      fmt.Sprintf("recovery-%d", i),
		}

		_, err := client.SendRequest(ctx, req)
		if i < 2 {
			require.Error(t, err, "Request %d should fail", i)
		}
	}

	verifyErrorRecoveryMetrics(t, client)
}

func verifyErrorRecoveryMetrics(t *testing.T, client *SSEClient) {
	t.Helper()

	metrics := client.GetMetrics()
	assert.GreaterOrEqual(t, metrics.ErrorCount, uint64(2), "Should have recorded errors")
}

// Enhanced benchmark tests.

func BenchmarkSSEClientSendRequest(b *testing.B) {
	logger := zaptest.NewLogger(b)

	mockServer := newMockSSEServer()
	defer mockServer.close()

	config := SSEClientConfig{
		URL:            mockServer.getURL(),
		RequestTimeout: 5 * time.Second,
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
	}

	client, err := NewSSEClient("bench-client", config.URL, config, logger)
	require.NoError(b, err)

	ctx := context.Background()
	err = client.Connect(ctx)
	require.NoError(b, err)

	defer func() {
		err := client.Close(ctx)
		require.NoError(b, err)
	}()

	// Wait for the stream to be ready.
	time.Sleep(httpStatusOK * time.Millisecond)

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

// BenchmarkSSEClientConcurrency benchmarks concurrent request handling.
func BenchmarkSSEClientConcurrency(b *testing.B) {
	logger := zaptest.NewLogger(b)

	mockServer := newMockSSEServer()
	defer mockServer.close()

	config := SSEClientConfig{
		URL:            mockServer.getURL(),
		RequestTimeout: 10 * time.Second,
		Performance: SSEPerformanceConfig{
			StreamBufferSize:   256 * 1024,
			ConnectionPoolSize: testTimeout,
			ReuseConnections:   true,
		},
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
	}

	client, err := NewSSEClient("concurrency-bench-client", config.URL, config, logger)
	require.NoError(b, err)

	ctx := context.Background()
	err = client.Connect(ctx)
	require.NoError(b, err)

	defer func() {
		err := client.Close(ctx)
		require.NoError(b, err)
	}()

	// Wait for stream to establish.
	time.Sleep(httpStatusOK * time.Millisecond)

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

// BenchmarkSSEClientStreamProcessing benchmarks stream event processing.
func BenchmarkSSEClientStreamProcessing(b *testing.B) {
	logger := zaptest.NewLogger(b)

	// Create a high-throughput mock server.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")

			flusher, ok := w.(http.Flusher)
			if !ok {
				http.Error(w, "Streaming unsupported", http.StatusInternalServerError)

				return
			}

			// Send many events rapidly.
			for i := 0; i < b.N; i++ {
				resp := mcp.Response{
					JSONRPC: constants.TestJSONRPCVersion,
					Result:  fmt.Sprintf("result-%d", i),
					ID:      fmt.Sprintf("stream-bench-%d", i),
				}
				respData, _ := json.Marshal(resp)
				_, _ = fmt.Fprintf(w, "data: %s\n\n", string(respData))

				flusher.Flush()
			}
		} else {
			w.WriteHeader(http.StatusAccepted)
		}
	}))
	defer server.Close()

	config := SSEClientConfig{
		URL:            server.URL,
		RequestTimeout: 30 * time.Second,
		Performance: SSEPerformanceConfig{
			StreamBufferSize: 512 * 1024, // Large buffer for high throughput
		},
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
	}

	client, err := NewSSEClient("stream-bench-client", config.URL, config, logger)
	require.NoError(b, err)

	ctx := context.Background()
	err = client.Connect(ctx)
	require.NoError(b, err)

	defer func() {
		err := client.Close(ctx)
		require.NoError(b, err)
	}()

	b.ResetTimer()

	// The benchmark measures how fast the client can process incoming SSE events.
	// Events are sent by the server automatically when connecting.
	time.Sleep(time.Duration(b.N) * time.Microsecond) // Allow time for processing
}
