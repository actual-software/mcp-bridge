package websocket

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/poiley/mcp-bridge/services/router/pkg/mcp"
)

const (
	testIterations          = 100
	httpStatusOK            = 200
	httpStatusInternalError = 500
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for testing
	},
}

func TestWebSocketBackend_CreateWebSocketBackend(t *testing.T) {
	logger := zaptest.NewLogger(t)

	config := Config{
		Endpoints: []string{"ws://localhost:8080/ws"},
		Timeout:   10 * time.Second,
	}

	backend := CreateWebSocketBackend("test-backend", config, logger, nil)

	assert.NotNil(t, backend)
	assert.Equal(t, "test-backend", backend.name)
	assert.Equal(t, config.Endpoints, backend.config.Endpoints)
	assert.Equal(t, "websocket", backend.GetProtocol())
	assert.Equal(t, "test-backend", backend.GetName())
}

func TestWebSocketBackend_ConfigDefaults(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Test with minimal config
	config := Config{
		Endpoints: []string{"ws://localhost:8080/ws"},
	}

	backend := CreateWebSocketBackend("test", config, logger, nil)

	// Verify defaults are set
	assert.Equal(t, 30*time.Second, backend.config.Timeout)
	assert.Equal(t, 10*time.Second, backend.config.HandshakeTimeout)
	assert.Equal(t, 30*time.Second, backend.config.PingInterval)
	assert.Equal(t, 10*time.Second, backend.config.PongTimeout)
	assert.Equal(t, int64(1024*1024), backend.config.MaxMessageSize)
	assert.Equal(t, 1, backend.config.ConnectionPool.MinSize)
	assert.Equal(t, 10, backend.config.ConnectionPool.MaxSize)
	assert.Equal(t, 300*time.Second, backend.config.ConnectionPool.MaxIdle)
	assert.Equal(t, 60*time.Second, backend.config.ConnectionPool.IdleTimeout)
	assert.Equal(t, 30*time.Second, backend.config.HealthCheck.Interval)
	assert.Equal(t, 5*time.Second, backend.config.HealthCheck.Timeout)
}

func TestWebSocketBackend_StartStop(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Create test WebSocket server
	server := createTestWebSocketServer(t)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	config := Config{
		Endpoints: []string{wsURL},
		Timeout:   5 * time.Second,
		ConnectionPool: PoolConfig{
			MinSize: 1,
			MaxSize: 2,
		},
	}

	backend := CreateWebSocketBackend("test", config, logger, nil)
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
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already running")

	// Test stop - FIX: Capture the actual stop error
	stopErr := backend.Stop(ctx)
	require.NoError(t, stopErr)

	// Verify stopped state
	backend.mu.RLock()
	assert.False(t, backend.running)
	backend.mu.RUnlock()
}

func TestWebSocketBackend_StartWithInvalidEndpoint(t *testing.T) {
	logger := zaptest.NewLogger(t)

	config := Config{
		Endpoints: []string{"ws://non-existent-host:99999/ws"},
		Timeout:   1 * time.Second,
		ConnectionPool: PoolConfig{
			MinSize: 1,
			MaxSize: 1,
		},
	}

	backend := CreateWebSocketBackend("test", config, logger, nil)

	// Start should not fail even if initial connections fail
	err := backend.Start(context.Background())
	require.NoError(t, err) // Connection failures are logged but don't fail startup

	_ = backend.Stop(context.Background())
}

func TestWebSocketBackend_SendRequest(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Create echo WebSocket server
	server := createTestWebSocketServer(t)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	config := Config{
		Endpoints: []string{wsURL},
		Timeout:   5 * time.Second,
		ConnectionPool: PoolConfig{
			MinSize: 1,
			MaxSize: 2,
		},
	}

	backend := CreateWebSocketBackend("test", config, logger, nil)
	ctx := context.Background()

	err := backend.Start(ctx)
	require.NoError(t, err)

	defer func() { _ = backend.Stop(ctx) }()

	// Give time for connections to establish
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

func TestWebSocketBackend_SendRequestWithTimeout(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Create server that doesn't respond
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		defer func() { _ = conn.Close() }()

		// Read messages but don't respond
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				break
			}
		}
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	config := Config{
		Endpoints: []string{wsURL},
		Timeout:   httpStatusInternalError * time.Millisecond, // Short timeout
		ConnectionPool: PoolConfig{
			MinSize: 1,
			MaxSize: 1,
		},
	}

	backend := CreateWebSocketBackend("test", config, logger, nil)
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

func TestWebSocketBackend_SendRequestNotRunning(t *testing.T) {
	logger := zaptest.NewLogger(t)

	config := Config{
		Endpoints: []string{"ws://localhost:8080/ws"},
	}

	backend := CreateWebSocketBackend("test", config, logger, nil)

	req := &mcp.Request{
		Method: "test/method",
		ID:     "test-123",
	}

	_, err := backend.SendRequest(context.Background(), req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no pool for endpoint")
}

func TestWebSocketBackend_Health(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Create test server
	server := createTestWebSocketServer(t)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	config := Config{
		Endpoints: []string{wsURL},
		HealthCheck: HealthCheckConfig{
			Enabled:  true,
			Timeout:  2 * time.Second,
			Interval: 1 * time.Second,
		},
		ConnectionPool: PoolConfig{
			MinSize: 1,
			MaxSize: 1,
		},
	}

	backend := CreateWebSocketBackend("test", config, logger, nil)
	ctx := context.Background()

	// Health check should fail when not running
	err := backend.Health(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not running")

	// Start backend
	err = backend.Start(ctx)
	require.NoError(t, err)

	defer func() { _ = backend.Stop(ctx) }()

	time.Sleep(httpStatusOK * time.Millisecond)

	// Health check should pass when running with healthy connections
	err = backend.Health(ctx)
	assert.NoError(t, err)

	// Check metrics updated
	metrics := backend.GetMetrics()
	assert.True(t, metrics.IsHealthy)
	assert.Less(t, time.Since(metrics.LastHealthCheck), time.Second)
}

func TestWebSocketBackend_GetMetrics(t *testing.T) {
	logger := zaptest.NewLogger(t)

	config := Config{
		Endpoints: []string{"ws://localhost:8080/ws"},
	}

	backend := CreateWebSocketBackend("test", config, logger, nil)

	metrics := backend.GetMetrics()
	assert.False(t, metrics.IsHealthy)
	assert.Equal(t, uint64(0), metrics.RequestCount)
	assert.Equal(t, uint64(0), metrics.ErrorCount)
}

func TestWebSocketBackend_UpdateMetrics(t *testing.T) {
	logger := zaptest.NewLogger(t)

	config := Config{
		Endpoints: []string{"ws://localhost:8080/ws"},
	}

	backend := CreateWebSocketBackend("test", config, logger, nil)

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

func TestWebSocketBackend_ConnectionPool(t *testing.T) {
	logger := zaptest.NewLogger(t)

	server := createTestWebSocketServer(t)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	config := Config{
		Endpoints: []string{wsURL},
		ConnectionPool: PoolConfig{
			MinSize:     2,
			MaxSize:     5,
			IdleTimeout: 1 * time.Second,
		},
	}

	backend := CreateWebSocketBackend("test", config, logger, nil)
	ctx := context.Background()

	err := backend.Start(ctx)
	require.NoError(t, err)

	defer func() { _ = backend.Stop(ctx) }()

	time.Sleep(httpStatusOK * time.Millisecond)

	// Verify pool was created
	backend.poolsMu.RLock()
	pool, exists := backend.pools[wsURL]
	backend.poolsMu.RUnlock()

	assert.True(t, exists)
	assert.NotNil(t, pool)

	// Check initial connections
	pool.mu.RLock()
	initialConnCount := len(pool.connections)
	pool.mu.RUnlock()

	assert.Equal(t, config.ConnectionPool.MinSize, initialConnCount)
}

func TestWebSocketBackend_MultipleEndpoints(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Create multiple test servers
	server1 := createTestWebSocketServer(t)
	defer server1.Close()

	server2 := createTestWebSocketServer(t)
	defer server2.Close()

	wsURL1 := "ws" + strings.TrimPrefix(server1.URL, "http")
	wsURL2 := "ws" + strings.TrimPrefix(server2.URL, "http")

	config := Config{
		Endpoints: []string{wsURL1, wsURL2},
		Timeout:   5 * time.Second,
		ConnectionPool: PoolConfig{
			MinSize: 1,
			MaxSize: 2,
		},
	}

	backend := CreateWebSocketBackend("test", config, logger, nil)
	ctx := context.Background()

	err := backend.Start(ctx)
	require.NoError(t, err)

	defer func() { _ = backend.Stop(ctx) }()

	time.Sleep(httpStatusOK * time.Millisecond)

	// Verify pools were created for both endpoints
	backend.poolsMu.RLock()
	assert.Len(t, backend.pools, 2)
	_, exists1 := backend.pools[wsURL1]
	_, exists2 := backend.pools[wsURL2]
	backend.poolsMu.RUnlock()

	assert.True(t, exists1)
	assert.True(t, exists2)

	// Send multiple requests to test round-robin
	for i := 0; i < 4; i++ {
		req := &mcp.Request{
			JSONRPC: "2.0",
			Method:  "test/method",
			ID:      fmt.Sprintf("test-%d", i),
		}

		response, err := backend.SendRequest(ctx, req)
		assert.NoError(t, err)
		assert.NotNil(t, response)
		assert.Equal(t, req.ID, response.ID)
	}
}

// Helper function to create a test WebSocket server that echoes JSON messages.
func createTestWebSocketServer(t *testing.T) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("WebSocket upgrade failed: %v", err)

			return
		}

		defer func() { _ = conn.Close() }()

		for {
			// Read message
			messageType, data, err := conn.ReadMessage()
			if err != nil {
				if !websocket.IsCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
					t.Logf("WebSocket read error: %v", err)
				}

				break
			}

			if messageType == websocket.TextMessage {
				// Parse as JSON-RPC request
				var request mcp.Request
				if err := json.Unmarshal(data, &request); err != nil {
					t.Logf("Failed to parse request: %v", err)

					continue
				}

				// Create echo response
				response := mcp.Response{
					JSONRPC: "2.0",
					ID:      request.ID,
					Result: map[string]interface{}{
						"method": request.Method,
						"params": request.Params,
					},
				}

				// Send response
				responseData, err := json.Marshal(response)
				if err != nil {
					t.Logf("Failed to marshal response: %v", err)

					continue
				}

				if err := conn.WriteMessage(websocket.TextMessage, responseData); err != nil {
					t.Logf("Failed to write response: %v", err)

					break
				}
			}
		}
	}))
}
