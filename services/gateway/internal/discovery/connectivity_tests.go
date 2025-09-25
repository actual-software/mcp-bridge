package discovery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/poiley/mcp-bridge/services/gateway/internal/config"
)

// setupSSEDiscovery creates a test SSE discovery instance.
func setupSSEDiscovery(t *testing.T) (*SSEDiscovery, context.Context) {
	t.Helper()
	logger := zaptest.NewLogger(t)
	cfg := config.SSEDiscoveryConfig{
		Services: []config.SSEServiceConfig{},
	}
	discovery, err := CreateSSEServiceDiscovery(cfg, logger)
	require.NoError(t, err)

	sseBridge, ok := discovery.(*SSEDiscovery)
	require.True(t, ok, "discovery should be *SSEDiscovery")

	return sseBridge, context.Background()
}

// testSSEValidEndpoint tests a valid SSE endpoint.
func testSSEValidEndpoint(t *testing.T, sseBridge *SSEDiscovery, ctx context.Context) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: test event\n\n"))
	}))
	defer server.Close()

	testService := &config.SSEServiceConfig{
		Name:    "test",
		Headers: map[string]string{},
	}
	err := sseBridge.checkSSEEndpoint(ctx, server.URL, testService)
	assert.NoError(t, err)
}

// TestSSEDiscovery_checkSSEEndpoint tests SSE endpoint checking.
func TestSSEDiscovery_checkSSEEndpoint(t *testing.T) {
	sseBridge, ctx := setupSSEDiscovery(t)

	t.Run("valid SSE endpoint", func(t *testing.T) {
		testSSEValidEndpoint(t, sseBridge, ctx)
	})

	t.Run("invalid URL", func(t *testing.T) {
		testService := &config.SSEServiceConfig{
			Name:    "test",
			Headers: map[string]string{},
		}
		err := sseBridge.checkSSEEndpoint(ctx, "invalid-url", testService)
		assert.Error(t, err)
	})

	t.Run("non-SSE endpoint", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("regular response"))
		}))
		defer server.Close()

		testService := &config.SSEServiceConfig{
			Name:    "test",
			Headers: map[string]string{},
		}
		err := sseBridge.checkSSEEndpoint(ctx, server.URL, testService)
		assert.Error(t, err)
	})

	t.Run("timeout context", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(RetryDelayMillis * time.Millisecond)
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		testService := &config.SSEServiceConfig{
			Name:    "test",
			Headers: map[string]string{},
		}

		timeoutCtx, cancel := context.WithTimeout(ctx, TestDelayMillis*time.Millisecond)
		defer cancel()

		err := sseBridge.checkSSEEndpoint(timeoutCtx, server.URL, testService)
		assert.Error(t, err)
	})
}

// testHTTPHealthCheck tests a successful HTTP health check.
func testHTTPHealthCheck(t *testing.T, sseBridge *SSEDiscovery, ctx context.Context) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("healthy"))
	}))
	defer server.Close()

	testService := &config.SSEServiceConfig{
		Name:    "test",
		Headers: map[string]string{"Authorization": "Bearer test"},
	}
	err := sseBridge.checkHTTPEndpoint(ctx, server.URL+"/health", testService)
	assert.NoError(t, err)
}

// testHTTPErrorStatus tests HTTP error status responses.
func testHTTPErrorStatus(t *testing.T, sseBridge *SSEDiscovery, ctx context.Context) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("server error"))
	}))
	defer server.Close()

	testService := &config.SSEServiceConfig{
		Name:    "test",
		Headers: map[string]string{},
	}
	err := sseBridge.checkHTTPEndpoint(ctx, server.URL, testService)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP httpStatusInternalError")
}

// TestSSEDiscovery_checkHTTPEndpoint tests HTTP endpoint checking with more coverage.
func TestSSEDiscovery_checkHTTPEndpoint_Enhanced(t *testing.T) {
	sseBridge, ctx := setupSSEDiscovery(t)

	t.Run("successful health check", func(t *testing.T) {
		testHTTPHealthCheck(t, sseBridge, ctx)
	})

	t.Run("server returns error status", func(t *testing.T) {
		testHTTPErrorStatus(t, sseBridge, ctx)
	})

	t.Run("connection refused", func(t *testing.T) {
		testService := &config.SSEServiceConfig{
			Name:    "test",
			Headers: map[string]string{},
		}
		err := sseBridge.checkHTTPEndpoint(ctx, "http://localhost:19999", testService)
		assert.Error(t, err)
	})

	t.Run("invalid URL format", func(t *testing.T) {
		testService := &config.SSEServiceConfig{
			Name:    "test",
			Headers: map[string]string{},
		}
		err := sseBridge.checkHTTPEndpoint(ctx, "://invalid", testService)
		assert.Error(t, err)
	})
}

// TestWebSocketDiscovery_checkEndpointConnectivity tests WebSocket connectivity checking.
func TestWebSocketDiscovery_checkEndpointConnectivity(t *testing.T) {
	logger := zaptest.NewLogger(t)

	cfg := config.WebSocketDiscoveryConfig{
		Services: []config.WebSocketServiceConfig{},
	}

	discovery, err := CreateWebSocketServiceDiscovery(cfg, logger)
	require.NoError(t, err)

	wsBridge, ok := discovery.(*WebSocketDiscovery)
	require.True(t, ok, "discovery should be *WebSocketDiscovery")

	ctx := context.Background()

	t.Run("invalid websocket URL", func(t *testing.T) {
		testService := &config.WebSocketServiceConfig{
			Name:    "test",
			Headers: map[string]string{},
		}

		err := wsBridge.checkEndpointConnectivity(ctx, "invalid-ws-url", testService)
		assert.Error(t, err)
	})

	t.Run("connection refused", func(t *testing.T) {
		testService := &config.WebSocketServiceConfig{
			Name:    "test",
			Headers: map[string]string{},
		}

		err := wsBridge.checkEndpointConnectivity(ctx, "ws://localhost:19999/ws", testService)
		assert.Error(t, err)
	})

	t.Run("timeout context", func(t *testing.T) {
		testService := &config.WebSocketServiceConfig{
			Name:    "test",
			Headers: map[string]string{},
		}

		timeoutCtx, cancel := context.WithTimeout(ctx, 1*time.Millisecond)
		defer cancel()

		err := wsBridge.checkEndpointConnectivity(timeoutCtx, "ws://example.com:defaultHTTPPort/ws", testService)
		assert.Error(t, err)
	})
}

// TestSSEDiscovery_checkServiceConnectivity tests service connectivity with enhanced coverage.
func TestSSEDiscovery_checkServiceConnectivity_Enhanced(t *testing.T) {
	logger := zaptest.NewLogger(t)

	cfg := config.SSEDiscoveryConfig{
		Services: []config.SSEServiceConfig{},
	}

	discovery, err := CreateSSEServiceDiscovery(cfg, logger)
	require.NoError(t, err)

	sseBridge, ok := discovery.(*SSEDiscovery)
	require.True(t, ok, "discovery should be *SSEDiscovery")

	ctx := context.Background()

	t.Run("service connectivity with health check enabled", func(t *testing.T) {
		testService := &config.SSEServiceConfig{
			Name:    "test",
			BaseURL: "http://localhost:19999",
			Headers: map[string]string{},
			HealthCheck: config.SSEHealthCheckConfig{
				Enabled: true,
			},
		}

		err := sseBridge.checkServiceConnectivity(ctx, testService)
		assert.Error(t, err) // Should error due to non-existent endpoint
	})

	t.Run("service connectivity with health check disabled", func(t *testing.T) {
		testService := &config.SSEServiceConfig{
			Name:    "test",
			BaseURL: "http://localhost:19999",
			Headers: map[string]string{},
			HealthCheck: config.SSEHealthCheckConfig{
				Enabled: false,
			},
		}

		err := sseBridge.checkServiceConnectivity(ctx, testService)
		assert.NoError(t, err) // Should not error when disabled
	})
}
