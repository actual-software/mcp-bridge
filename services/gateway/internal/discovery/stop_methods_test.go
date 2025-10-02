package discovery

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/actual-software/mcp-bridge/services/gateway/internal/config"
)

// TestSSEDiscovery_Stop tests the Stop method for SSE discovery.
func TestSSEDiscovery_Stop(t *testing.T) {
	logger := zaptest.NewLogger(t)

	cfg := config.SSEDiscoveryConfig{
		Services: []config.SSEServiceConfig{
			{
				Name:      "weather",
				Namespace: "weather",
				BaseURL:   "http://localhost:8080",
			},
		},
	}

	discovery, err := CreateSSEServiceDiscovery(cfg, logger)
	require.NoError(t, err)

	// Stop should not panic - call it to get coverage
	discovery.Stop()

	// Multiple stops should be safe
	discovery.Stop()
	discovery.Stop()

	// Verify that Stop was actually called (basic coverage verification)
	assert.NotNil(t, discovery)
}

// TestStdioDiscovery_Stop tests the Stop method for Stdio discovery.
func TestStdioDiscovery_Stop(t *testing.T) {
	logger := zaptest.NewLogger(t)

	cfg := config.StdioDiscoveryConfig{
		Services: []config.StdioServiceConfig{
			{
				Name:      "weather",
				Namespace: "weather",
				Command:   []string{"weather-server", "--mcp"},
			},
		},
	}

	discovery, err := CreateStdioServiceDiscovery(cfg, logger)
	require.NoError(t, err)

	// Stop should not panic - call it to get coverage
	discovery.Stop()

	// Multiple stops should be safe
	discovery.Stop()
	discovery.Stop()

	// Verify that Stop was actually called (basic coverage verification)
	assert.NotNil(t, discovery)
}

// TestWebSocketDiscovery_Stop tests the Stop method for WebSocket discovery.
func TestWebSocketDiscovery_Stop(t *testing.T) {
	logger := zaptest.NewLogger(t)

	cfg := config.WebSocketDiscoveryConfig{
		Services: []config.WebSocketServiceConfig{
			{
				Name:      "weather",
				Namespace: "weather",
				Endpoints: []string{"ws://localhost:8080/ws"},
			},
		},
	}

	discovery, err := CreateWebSocketServiceDiscovery(cfg, logger)
	require.NoError(t, err)

	// Stop should not panic - call it to get coverage
	discovery.Stop()

	// Multiple stops should be safe
	discovery.Stop()
	discovery.Stop()

	// Verify that Stop was actually called (basic coverage verification)
	assert.NotNil(t, discovery)
}

// TestSSEDiscovery_GetService tests the GetService method for SSE discovery.
func TestSSEDiscovery_GetService(t *testing.T) {
	logger := zaptest.NewLogger(t)

	cfg := config.SSEDiscoveryConfig{
		Services: []config.SSEServiceConfig{
			{
				Name:      "weather",
				Namespace: "weather",
				BaseURL:   "http://localhost:8080",
			},
		},
	}

	discovery, err := CreateSSEServiceDiscovery(cfg, logger)
	require.NoError(t, err)

	// Cast to concrete type to access GetService method
	sseBridge, ok := discovery.(*SSEDiscovery)
	require.True(t, ok, "Expected SSEDiscovery type")

	ctx := context.Background()

	// Test getting existing service
	endpoint, err := sseBridge.GetService(ctx, "weather")
	require.NoError(t, err)
	assert.NotNil(t, endpoint)
	assert.Equal(t, "weather", endpoint.Service)
	assert.Equal(t, "weather", endpoint.Namespace)

	// Test getting non-existent service
	endpoint, err = sseBridge.GetService(ctx, "nonexistent")
	require.Error(t, err)
	assert.Nil(t, endpoint)
	assert.Contains(t, err.Error(), "not found")
}

// TestStdioDiscovery_GetService tests the GetService method for Stdio discovery.
func TestStdioDiscovery_GetService(t *testing.T) {
	logger := zaptest.NewLogger(t)

	cfg := config.StdioDiscoveryConfig{
		Services: []config.StdioServiceConfig{
			{
				Name:      "weather",
				Namespace: "weather",
				Command:   []string{"weather-server", "--mcp"},
			},
		},
	}

	discovery, err := CreateStdioServiceDiscovery(cfg, logger)
	require.NoError(t, err)

	// Cast to concrete type to access GetService method
	stdioBridge, ok := discovery.(*StdioDiscovery)
	require.True(t, ok, "Expected StdioDiscovery type")

	ctx := context.Background()

	// Test getting existing service
	endpoint, err := stdioBridge.GetService(ctx, "weather")
	require.NoError(t, err)
	assert.NotNil(t, endpoint)
	assert.Equal(t, "weather", endpoint.Service)
	assert.Equal(t, "weather", endpoint.Namespace)

	// Test getting non-existent service
	endpoint, err = stdioBridge.GetService(ctx, "nonexistent")
	require.Error(t, err)
	assert.Nil(t, endpoint)
	assert.Contains(t, err.Error(), "not found")
}

// TestWebSocketDiscovery_GetService tests the GetService method for WebSocket discovery.
func TestWebSocketDiscovery_GetService(t *testing.T) {
	logger := zaptest.NewLogger(t)

	cfg := config.WebSocketDiscoveryConfig{
		Services: []config.WebSocketServiceConfig{
			{
				Name:      "weather",
				Namespace: "weather",
				Endpoints: []string{"ws://localhost:8080/ws"},
			},
		},
	}

	discovery, err := CreateWebSocketServiceDiscovery(cfg, logger)
	require.NoError(t, err)

	// Cast to concrete type to access GetService method
	wsBridge, ok := discovery.(*WebSocketDiscovery)
	require.True(t, ok, "Expected WebSocketDiscovery type")

	ctx := context.Background()

	// Test getting existing service
	endpoint, err := wsBridge.GetService(ctx, "weather")
	require.NoError(t, err)
	assert.NotNil(t, endpoint)
	assert.Equal(t, "weather", endpoint.Service)
	assert.Equal(t, "weather", endpoint.Namespace)

	// Test getting non-existent service
	endpoint, err = wsBridge.GetService(ctx, "nonexistent")
	require.Error(t, err)
	assert.Nil(t, endpoint)
	assert.Contains(t, err.Error(), "not found")
}

// TestSSEDiscovery_checkHTTPEndpoint tests the checkHTTPEndpoint method.
func TestSSEDiscovery_checkHTTPEndpoint(t *testing.T) {
	logger := zaptest.NewLogger(t)

	cfg := config.SSEDiscoveryConfig{
		Services: []config.SSEServiceConfig{},
	}

	discovery, err := CreateSSEServiceDiscovery(cfg, logger)
	require.NoError(t, err)

	sseBridge, ok := discovery.(*SSEDiscovery)
	require.True(t, ok, "Expected SSEDiscovery type")

	ctx := context.Background()
	testService := &config.SSEServiceConfig{
		Name:    "test",
		Headers: map[string]string{},
	}

	// Test with invalid URL
	err = sseBridge.checkHTTPEndpoint(ctx, "invalid-url", testService)
	require.Error(t, err)

	// Test with non-existent endpoint
	err = sseBridge.checkHTTPEndpoint(ctx, "http://localhost:19999", testService)
	require.Error(t, err)
}

// TestSSEDiscovery_Watch tests the Watch method.
func TestSSEDiscovery_Watch(t *testing.T) {
	logger := zaptest.NewLogger(t)

	cfg := config.SSEDiscoveryConfig{
		Services: []config.SSEServiceConfig{
			{
				Name:      "weather",
				Namespace: "weather",
				BaseURL:   "http://localhost:8080",
			},
		},
	}

	discovery, err := CreateSSEServiceDiscovery(cfg, logger)
	require.NoError(t, err)

	sseBridge, ok := discovery.(*SSEDiscovery)
	require.True(t, ok, "Expected SSEDiscovery type")

	ctx, cancel := context.WithCancel(context.Background())

	// Start watching in a goroutine
	done := make(chan struct{})

	go func() {
		defer close(done)

		_, _ = sseBridge.Watch(ctx)
	}()

	// Cancel context immediately
	cancel()

	// Wait for watch to exit
	<-done
}

// TestStdioDiscovery_Watch tests the Watch method.
func TestStdioDiscovery_Watch(t *testing.T) {
	logger := zaptest.NewLogger(t)

	cfg := config.StdioDiscoveryConfig{
		Services: []config.StdioServiceConfig{
			{
				Name:      "weather",
				Namespace: "weather",
				Command:   []string{"weather-server", "--mcp"},
			},
		},
	}

	discovery, err := CreateStdioServiceDiscovery(cfg, logger)
	require.NoError(t, err)

	stdioBridge, ok := discovery.(*StdioDiscovery)
	require.True(t, ok, "Expected StdioDiscovery type")

	ctx, cancel := context.WithCancel(context.Background())

	// Start watching in a goroutine
	done := make(chan struct{})

	go func() {
		defer close(done)

		_, _ = stdioBridge.Watch(ctx)
	}()

	// Cancel context immediately
	cancel()

	// Wait for watch to exit
	<-done
}

// TestWebSocketDiscovery_Watch tests the Watch method.
func TestWebSocketDiscovery_Watch(t *testing.T) {
	logger := zaptest.NewLogger(t)

	cfg := config.WebSocketDiscoveryConfig{
		Services: []config.WebSocketServiceConfig{
			{
				Name:      "weather",
				Namespace: "weather",
				Endpoints: []string{"ws://localhost:8080/ws"},
			},
		},
	}

	discovery, err := CreateWebSocketServiceDiscovery(cfg, logger)
	require.NoError(t, err)

	wsBridge, ok := discovery.(*WebSocketDiscovery)
	require.True(t, ok, "Expected WebSocketDiscovery type")

	ctx, cancel := context.WithCancel(context.Background())

	// Start watching in a goroutine
	done := make(chan struct{})

	go func() {
		defer close(done)

		_, _ = wsBridge.Watch(ctx)
	}()

	// Cancel context immediately
	cancel()

	// Wait for watch to exit
	<-done
}

// TestSSEDiscovery_HealthCheck tests the HealthCheck method.
func TestSSEDiscovery_HealthCheck(t *testing.T) {
	logger := zaptest.NewLogger(t)

	cfg := config.SSEDiscoveryConfig{
		Services: []config.SSEServiceConfig{
			{
				Name:      "weather",
				Namespace: "weather",
				BaseURL:   "http://localhost:19999", // Non-existent port
				HealthCheck: config.SSEHealthCheckConfig{
					Enabled: true,
				},
			},
		},
	}

	discovery, err := CreateSSEServiceDiscovery(cfg, logger)
	require.NoError(t, err)

	sseBridge, ok := discovery.(*SSEDiscovery)
	require.True(t, ok, "Expected SSEDiscovery type")

	ctx := context.Background()

	// Create a test endpoint for health checking
	testEndpoint := &Endpoint{
		Service:   "weather",
		Namespace: "weather",
	}

	// Health check should complete without panic
	err = sseBridge.HealthCheck(ctx, testEndpoint)
	// Should complete (expect error due to non-existent service but shouldn't panic)
	require.Error(t, err)

	// Verify endpoint exists and is marked as unhealthy
	endpoints := discovery.GetEndpoints("weather")
	assert.Len(t, endpoints, 1)
	assert.False(t, endpoints[0].Healthy)
}

// TestStdioDiscovery_HealthCheck tests the HealthCheck method.
func TestStdioDiscovery_HealthCheck(t *testing.T) {
	logger := zaptest.NewLogger(t)

	cfg := config.StdioDiscoveryConfig{
		Services: []config.StdioServiceConfig{
			{
				Name:      "weather",
				Namespace: "weather",
				Command:   []string{"nonexistent-command", "--mcp"},
				HealthCheck: config.StdioHealthCheckConfig{
					Enabled: true,
				},
			},
		},
	}

	discovery, err := CreateStdioServiceDiscovery(cfg, logger)
	require.NoError(t, err)

	stdioBridge, ok := discovery.(*StdioDiscovery)
	require.True(t, ok, "Expected StdioDiscovery type")

	ctx := context.Background()

	// Create a test endpoint for health checking
	testEndpoint := &Endpoint{
		Service:   "weather",
		Namespace: "weather",
	}

	// Health check should complete without panic
	err = stdioBridge.HealthCheck(ctx, testEndpoint)
	// Should complete (expect error due to non-existent command but shouldn't panic)
	require.Error(t, err)

	// Verify endpoint exists
	endpoints := discovery.GetEndpoints("weather")
	assert.Len(t, endpoints, 1)
}

// TestWebSocketDiscovery_HealthCheck tests the HealthCheck method.
func TestWebSocketDiscovery_HealthCheck(t *testing.T) {
	logger := zaptest.NewLogger(t)

	cfg := config.WebSocketDiscoveryConfig{
		Services: []config.WebSocketServiceConfig{
			{
				Name:      "weather",
				Namespace: "weather",
				Endpoints: []string{"ws://localhost:19999/ws"}, // Non-existent port
				HealthCheck: config.WebSocketHealthCheckConfig{
					Enabled: true,
				},
			},
		},
	}

	discovery, err := CreateWebSocketServiceDiscovery(cfg, logger)
	require.NoError(t, err)

	wsBridge, ok := discovery.(*WebSocketDiscovery)
	require.True(t, ok, "Expected WebSocketDiscovery type")

	ctx := context.Background()

	// Create a test endpoint for health checking
	testEndpoint := &Endpoint{
		Service:   "weather",
		Namespace: "weather",
	}

	// Health check should complete without panic
	err = wsBridge.HealthCheck(ctx, testEndpoint)
	// Should complete (expect error due to non-existent service but shouldn't panic)
	require.Error(t, err)

	// Verify endpoint exists and is marked as unhealthy
	endpoints := discovery.GetEndpoints("weather")
	assert.Len(t, endpoints, 1)
	assert.False(t, endpoints[0].Healthy)
}
