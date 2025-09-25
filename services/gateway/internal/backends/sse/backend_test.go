
package sse

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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	"github.com/poiley/mcp-bridge/services/router/pkg/mcp"
)

const (
	testIterations          = 100
	httpStatusOK            = 200
	httpStatusInternalError = 500
	sseEndpoint             = "/sse"
)

func TestSSEBackend_CreateSSEBackend(t *testing.T) {
	logger := zaptest.NewLogger(t)

	config := Config{
		BaseURL:         "http://localhost:8080",
		StreamEndpoint:  sseEndpoint,
		RequestEndpoint: "/request",
		Timeout:         10 * time.Second,
	}

	backend := CreateSSEBackend("test-backend", config, logger, nil)

	assert.NotNil(t, backend)
	assert.Equal(t, "test-backend", backend.name)
	assert.Equal(t, config.BaseURL, backend.config.BaseURL)
	assert.Equal(t, "sse", backend.GetProtocol())
	assert.Equal(t, "test-backend", backend.GetName())
}

func TestSSEBackend_ConfigDefaults(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Test with minimal config
	config := Config{
		BaseURL:         "http://localhost:8080",
		StreamEndpoint:  sseEndpoint,
		RequestEndpoint: "/request",
	}

	backend := CreateSSEBackend("test", config, logger, nil)

	// Verify defaults are set
	assert.Equal(t, 30*time.Second, backend.config.Timeout)
	assert.Equal(t, 5*time.Second, backend.config.ReconnectDelay)
	assert.Equal(t, 10, backend.config.MaxReconnects)
	assert.Equal(t, 30*time.Second, backend.config.HealthCheck.Interval)
	assert.Equal(t, 5*time.Second, backend.config.HealthCheck.Timeout)
}

func TestSSEBackend_StartStop(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Create test SSE server
	server := createTestSSEServer(t)
	defer server.Close()

	config := Config{
		BaseURL:         server.URL,
		StreamEndpoint:  sseEndpoint,
		RequestEndpoint: "/request",
		Timeout:         5 * time.Second,
	}

	backend := CreateSSEBackend("test", config, logger, nil)
	ctx := context.Background()

	// Test start
	err := backend.Start(ctx)
	require.NoError(t, err)

	// Verify running state
	backend.mu.RLock()
	assert.True(t, backend.running)
	backend.mu.RUnlock()

	// Test double start (should fail)
	err = backend.Start(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already running")

	// Test stop - FIX: Capture the actual stop error
	stopErr := backend.Stop(ctx)
	assert.NoError(t, stopErr)

	// Verify stopped state
	backend.mu.RLock()
	assert.False(t, backend.running)
	backend.mu.RUnlock()
}

func TestSSEBackend_StartWithInvalidEndpoint(t *testing.T) {
	logger := zaptest.NewLogger(t)

	tests := []struct {
		name    string
		baseURL string
		wantErr string
	}{
		{
			name:    "invalid URL",
			baseURL: "not-a-url",
			wantErr: "unsupported protocol scheme",
		},
		{
			name:    "unreachable endpoint",
			baseURL: "http://non-existent-host:99999",
			wantErr: "invalid port",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := Config{
				BaseURL:         tt.baseURL,
				StreamEndpoint:  sseEndpoint,
				RequestEndpoint: "/request",
				Timeout:         1 * time.Second,
			}

			backend := CreateSSEBackend("test", config, logger, nil)

			err := backend.Start(context.Background())
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestSSEBackend_SendRequest(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Create echo SSE server
	server := createTestSSEServer(t)
	defer server.Close()

	config := Config{
		BaseURL:         server.URL,
		StreamEndpoint:  sseEndpoint,
		RequestEndpoint: "/request",
		Timeout:         5 * time.Second,
	}

	backend := CreateSSEBackend("test", config, logger, nil)
	ctx := context.Background()

	err := backend.Start(ctx)
	require.NoError(t, err)

	defer func() { _ = backend.Stop(ctx) }() 

	// Give time for SSE connection to establish
	time.Sleep(httpStatusOK * time.Millisecond)

	// Test request with auto-generated ID
	req := &mcp.Request{
		JSONRPC: "2.0",
		Method:  "test/method",
		Params: map[string]interface{}{
			"key": "value",
		},
	}

	response, err := backend.SendRequest(ctx, req)
	require.NoError(t, err)
	assert.NotNil(t, response)

	// Verify request ID was set
	assert.NotNil(t, req.ID)
	assert.NotEmpty(t, req.ID)

	// Verify response
	assert.Equal(t, req.ID, response.ID)
	assert.Equal(t, "2.0", response.JSONRPC)
}

func TestSSEBackend_SendRequestWithSpecificID(t *testing.T) {
	logger := zaptest.NewLogger(t)

	server := createTestSSEServer(t)
	defer server.Close()

	config := Config{
		BaseURL:         server.URL,
		StreamEndpoint:  sseEndpoint,
		RequestEndpoint: "/request",
		Timeout:         5 * time.Second,
	}

	backend := CreateSSEBackend("test", config, logger, nil)
	ctx := context.Background()

	err := backend.Start(ctx)
	require.NoError(t, err)

	defer func() { _ = backend.Stop(ctx) }() 

	time.Sleep(httpStatusOK * time.Millisecond)

	// Test request with specific ID
	req := &mcp.Request{
		JSONRPC: "2.0",
		Method:  "test/method",
		ID:      "custom-123",
		Params: map[string]interface{}{
			"key": "value",
		},
	}

	response, err := backend.SendRequest(ctx, req)
	require.NoError(t, err)
	assert.NotNil(t, response)
	assert.Equal(t, "custom-123", response.ID)
}

func TestSSEBackend_SendRequestWithTimeout(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Create server that doesn't respond to requests
	server := createNonResponsiveSSEServer(t)
	defer server.Close()

	config := Config{
		BaseURL:         server.URL,
		StreamEndpoint:  sseEndpoint,
		RequestEndpoint: "/request",
		Timeout:         httpStatusInternalError * time.Millisecond, // Short timeout
	}

	backend := CreateSSEBackend("test", config, logger, nil)
	ctx := context.Background()

	err := backend.Start(ctx)
	require.NoError(t, err)

	defer func() { _ = backend.Stop(ctx) }() 

	time.Sleep(httpStatusOK * time.Millisecond)

	req := &mcp.Request{
		JSONRPC: "2.0",
		Method:  "test/method",
		ID:      "test-123",
	}

	_, err = backend.SendRequest(ctx, req)
	assert.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "timeout")
}

func TestSSEBackend_SendRequestNotRunning(t *testing.T) {
	logger := zaptest.NewLogger(t)

	config := Config{
		BaseURL:         "http://localhost:8080",
		StreamEndpoint:  sseEndpoint,
		RequestEndpoint: "/request",
	}

	backend := CreateSSEBackend("test", config, logger, nil)

	req := &mcp.Request{
		Method: "test/method",
		ID:     "test-123",
	}

	_, err := backend.SendRequest(context.Background(), req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not running")
}

func TestSSEBackend_Health(t *testing.T) {
	logger := zaptest.NewLogger(t)

	server := createTestSSEServer(t)
	defer server.Close()

	config := Config{
		BaseURL:         server.URL,
		StreamEndpoint:  sseEndpoint,
		RequestEndpoint: "/request",
		HealthCheck: HealthCheckConfig{
			Enabled:  true,
			Timeout:  2 * time.Second,
			Interval: 1 * time.Second,
		},
	}

	backend := CreateSSEBackend("test", config, logger, nil)
	ctx := context.Background()

	// Health check should fail when not running
	err := backend.Health(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not running")

	// Start backend
	err = backend.Start(ctx)
	require.NoError(t, err)

	defer func() { _ = backend.Stop(ctx) }() 

	time.Sleep(httpStatusOK * time.Millisecond)

	// Health check should pass when running with active connection
	err = backend.Health(ctx)
	assert.NoError(t, err)

	// Check metrics updated
	metrics := backend.GetMetrics()
	assert.True(t, metrics.IsHealthy)
	assert.Less(t, time.Since(metrics.LastHealthCheck), time.Second)
}

func TestSSEBackend_GetMetrics(t *testing.T) {
	logger := zaptest.NewLogger(t)

	config := Config{
		BaseURL:         "http://localhost:8080",
		StreamEndpoint:  sseEndpoint,
		RequestEndpoint: "/request",
	}

	backend := CreateSSEBackend("test", config, logger, nil)

	metrics := backend.GetMetrics()
	assert.False(t, metrics.IsHealthy)
	assert.Equal(t, uint64(0), metrics.RequestCount)
	assert.Equal(t, uint64(0), metrics.ErrorCount)
}

func TestSSEBackend_UpdateMetrics(t *testing.T) {
	logger := zaptest.NewLogger(t)

	config := Config{
		BaseURL:         "http://localhost:8080",
		StreamEndpoint:  sseEndpoint,
		RequestEndpoint: "/request",
	}

	backend := CreateSSEBackend("test", config, logger, nil)

	// Update metrics
	backend.updateMetrics(func(m *BackendMetrics) {
		m.RequestCount = testIterations
		m.ErrorCount = 5
		m.IsHealthy = true
	})

	metrics := backend.GetMetrics()
	assert.Equal(t, uint64(testIterations), metrics.RequestCount)
	assert.Equal(t, uint64(5), metrics.ErrorCount)
	assert.True(t, metrics.IsHealthy)
}

func TestSSEBackend_CustomHeaders(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Create server that checks for custom headers
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == sseEndpoint {
			// Check for custom headers
			if r.Header.Get("X-Custom-Header") != "test-value" {
				http.Error(w, "Missing custom header", http.StatusBadRequest)

				return
			}

			// Set SSE headers
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")

			flusher, ok := w.(http.Flusher)
			if !ok {
				http.Error(w, "Streaming unsupported", http.StatusInternalServerError)

				return
			}

			// Keep connection alive but don't send any events
			ticker := time.NewTicker(1 * time.Second)
			defer ticker.Stop()

			for {
				select {
				case <-r.Context().Done():
					return
				case <-ticker.C:
					_, _ = fmt.Fprintf(w, "data: {\"type\":\"keepalive\"}\n\n")

					flusher.Flush()
				}
			}
		}
	}))
	defer server.Close()

	config := Config{
		BaseURL:         server.URL,
		StreamEndpoint:  sseEndpoint,
		RequestEndpoint: "/request",
		Headers: map[string]string{
			"X-Custom-Header": "test-value",
		},
		Timeout: 5 * time.Second,
	}

	backend := CreateSSEBackend("test", config, logger, nil)
	ctx := context.Background()

	err := backend.Start(ctx)
	require.NoError(t, err)

	defer func() { _ = backend.Stop(ctx) }() 

	// Verify headers were used (connection should succeed)
	time.Sleep(httpStatusOK * time.Millisecond)

	err = backend.Health(ctx)
	assert.NoError(t, err)
}

func TestSSEBackend_ConcurrentRequests(t *testing.T) {
	logger := zaptest.NewLogger(t)
	server := createTestSSEServer(t)
	defer server.Close()

	backend := setupConcurrentRequestsBackend(t, server, logger)
	ctx := context.Background()

	err := backend.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = backend.Stop(ctx) }()

	time.Sleep(httpStatusOK * time.Millisecond)

	sendConcurrentRequests(t, ctx, backend)
}

func setupConcurrentRequestsBackend(t *testing.T, server *httptest.Server, logger *zap.Logger) *Backend {
	t.Helper()
	config := Config{
		BaseURL:         server.URL,
		StreamEndpoint:  sseEndpoint,
		RequestEndpoint: "/request",
		Timeout:         5 * time.Second,
	}

	return CreateSSEBackend("test", config, logger, nil)
}

func sendConcurrentRequests(t *testing.T, ctx context.Context, backend *Backend) {
	t.Helper()
	const numRequests = 5
	responses := make(chan *mcp.Response, numRequests)
	errors := make(chan error, numRequests)

	for i := 0; i < numRequests; i++ {
		go func(id int) {
			req := &mcp.Request{
				JSONRPC: "2.0",
				Method:  "test/method",
				ID:      fmt.Sprintf("concurrent-%d", id),
				Params: map[string]interface{}{
					"requestId": id,
				},
			}
			response, err := backend.SendRequest(ctx, req)
			if err != nil {
				errors <- err
			} else {
				responses <- response
			}
		}(i)
	}

	receivedResponses := 0
	receivedErrors := 0

	for i := 0; i < numRequests; i++ {
		select {
		case <-responses:
			receivedResponses++
		case <-errors:
			receivedErrors++
		case <-time.After(10 * time.Second):
			t.Fatal("Timeout waiting for responses")
		}
	}

	assert.Equal(t, numRequests, receivedResponses)
	assert.Equal(t, 0, receivedErrors)
}

// Helper function to create a test SSE server that echoes JSON-RPC requests.
func createTestSSEServer(t *testing.T) *httptest.Server {
	t.Helper()
	// Use a shared map to coordinate between POST and SSE handlers
	pendingRequests := make(map[string]mcp.Request)

	var requestsMu sync.Mutex

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == sseEndpoint {
			handleSSEConnection(w, r, pendingRequests, &requestsMu)
		} else if r.Method == http.MethodPost {
			handlePostRequest(w, r, pendingRequests, &requestsMu)
		}
	}))
}

// handleSSEConnection handles SSE connection and sends responses.
func handleSSEConnection(
	w http.ResponseWriter,
	r *http.Request,
	pendingRequests map[string]mcp.Request,
	requestsMu *sync.Mutex,
) {
	// Setup SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)

		return
	}

	// Send initial connection message
	sendSSEMessage(w, flusher, `{"type":"connected"}`)

	// Process pending requests and send responses via SSE
	processSSERequests(w, r, flusher, pendingRequests, requestsMu)
}

// processSSERequests processes pending requests in SSE stream.
func processSSERequests(w http.ResponseWriter, r *http.Request, flusher http.Flusher, pendingRequests map[string]mcp.Request, requestsMu *sync.Mutex) {
	ticker := time.NewTicker(testIterations * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			processPendingRequests(w, flusher, pendingRequests, requestsMu)
		}
	}
}

// processPendingRequests processes and responds to pending requests.
func processPendingRequests(w http.ResponseWriter, flusher http.Flusher, pendingRequests map[string]mcp.Request, requestsMu *sync.Mutex) {
	requestsMu.Lock()
	defer requestsMu.Unlock()

	if len(pendingRequests) == 0 {
		sendSSEMessage(w, flusher, `{"type":"keepalive"}`)

		return
	}

	for reqID, req := range pendingRequests {
		sendRequestResponse(w, flusher, req)
		delete(pendingRequests, reqID)
	}
}

// sendRequestResponse sends a response for a request via SSE.
func sendRequestResponse(w http.ResponseWriter, flusher http.Flusher, req mcp.Request) {
	response := mcp.Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"method": req.Method,
			"params": req.Params,
		},
	}

	responseData, err := json.Marshal(response)
	if err != nil {
		return
	}

	sendSSEMessage(w, flusher, string(responseData))
}

// sendSSEMessage sends a message via SSE.
func sendSSEMessage(w http.ResponseWriter, flusher http.Flusher, data string) {
	_, _ = fmt.Fprintf(w, "data: %s\n\n", data)

	flusher.Flush()
}

// handlePostRequest handles POST request submission.
func handlePostRequest(w http.ResponseWriter, r *http.Request, pendingRequests map[string]mcp.Request, requestsMu *sync.Mutex) {
	var request mcp.Request
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)

		return
	}

	// Queue request for SSE response
	requestsMu.Lock()

	requestID := fmt.Sprintf("%v", request.ID)
	pendingRequests[requestID] = request

	requestsMu.Unlock()

	// Acknowledge receipt with HTTP 202
	w.WriteHeader(http.StatusAccepted)
	_, _ = w.Write([]byte("Request queued"))
}

// Helper function to create a non-responsive SSE server.
func createNonResponsiveSSEServer(t *testing.T) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == sseEndpoint {
			// Handle SSE connection - properly establish the connection
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.Header().Set("Connection", "keep-alive")

			flusher, ok := w.(http.Flusher)
			if !ok {
				http.Error(w, "Streaming unsupported", http.StatusInternalServerError)

				return
			}

			// Send headers immediately so client can establish connection
			flusher.Flush()

			// Keep connection alive but don't send any data (non-responsive)
			<-r.Context().Done()
		} else if r.Method == http.MethodPost {
			// Accept POST requests but don't do anything with them
			w.WriteHeader(http.StatusOK)
		}
	}))
}
