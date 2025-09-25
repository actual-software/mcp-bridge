
package discovery

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	"github.com/poiley/mcp-bridge/services/gateway/internal/config"
)

// TestStopMethodsCoverage tests Stop methods to improve coverage.
func TestStopMethodsCoverage(t *testing.T) {
	logger := zaptest.NewLogger(t)
	t.Run("SSE Discovery Stop", func(t *testing.T) {
		testSSEDiscoveryStop(t, logger)
	})
	t.Run("Stdio Discovery Stop", func(t *testing.T) {
		testStdioDiscoveryStop(t, logger)
	})
	t.Run("WebSocket Discovery Stop", func(t *testing.T) {
		testWebSocketDiscoveryStop(t, logger)
	})
}

func testSSEDiscoveryStop(t *testing.T, logger *zap.Logger) {
	t.Helper()
	cfg := config.SSEDiscoveryConfig{
		Services: []config.SSEServiceConfig{
			{
				Name:      "test",
				Namespace: "test",
				BaseURL:   "http://localhost:8080",
			},
		},
	}
	discovery, err := CreateSSEServiceDiscovery(cfg, logger)
	assert.NoError(t, err)
	sseBridge, ok := discovery.(*SSEDiscovery)
	require.True(t, ok)
	// Call Stop multiple times to ensure it's safe
	sseBridge.Stop()
	sseBridge.Stop()
	sseBridge.Stop()
}

func testStdioDiscoveryStop(t *testing.T, logger *zap.Logger) {
	t.Helper()
	cfg := config.StdioDiscoveryConfig{
		Services: []config.StdioServiceConfig{
			{
				Name:      "test",
				Namespace: "test",
				Command:   []string{"echo", "test"},
			},
		},
	}
	discovery, err := CreateStdioServiceDiscovery(cfg, logger)
	assert.NoError(t, err)
	stdioBridge, ok := discovery.(*StdioDiscovery)
	require.True(t, ok, "Expected StdioDiscovery type")
	// Call Stop multiple times to ensure it's safe
	stdioBridge.Stop()
	stdioBridge.Stop()
	stdioBridge.Stop()
}

func testWebSocketDiscoveryStop(t *testing.T, logger *zap.Logger) {
	t.Helper()
	cfg := config.WebSocketDiscoveryConfig{
		Services: []config.WebSocketServiceConfig{
			{
				Name:      "test",
				Namespace: "test",
				Endpoints: []string{"ws://localhost:8080/ws"},
			},
		},
	}
	discovery, err := CreateWebSocketServiceDiscovery(cfg, logger)
	assert.NoError(t, err)
	wsBridge, ok := discovery.(*WebSocketDiscovery)
	require.True(t, ok, "Expected WebSocketDiscovery type")
	// Call Stop multiple times to ensure it's safe
	wsBridge.Stop()
	wsBridge.Stop()
	wsBridge.Stop()
}

// TestWatchMethodsCoverage tests Watch methods to improve coverage.
func TestWatchMethodsCoverage(t *testing.T) {
	logger := zaptest.NewLogger(t)
	t.Run("SSE Discovery Watch", func(t *testing.T) {
		testSSEDiscoveryWatch(t, logger)
	})
	t.Run("Stdio Discovery Watch", func(t *testing.T) {
		testStdioDiscoveryWatch(t, logger)
	})
	t.Run("WebSocket Discovery Watch", func(t *testing.T) {
		testWebSocketDiscoveryWatch(t, logger)
	})
}

func testSSEDiscoveryWatch(t *testing.T, logger *zap.Logger) {
	t.Helper()
	cfg := config.SSEDiscoveryConfig{
		Services: []config.SSEServiceConfig{
			{
				Name:      "test",
				Namespace: "test",
				BaseURL:   "http://localhost:8080",
			},
		},
	}
	discovery, err := CreateSSEServiceDiscovery(cfg, logger)
	assert.NoError(t, err)
	sseBridge, ok := discovery.(*SSEDiscovery)
	require.True(t, ok, "Expected SSEDiscovery type")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = sseBridge.Watch(ctx)
	}()
	cancel() // Cancel immediately to exit the watch
	<-done   // Wait for watch to exit
}

func testStdioDiscoveryWatch(t *testing.T, logger *zap.Logger) {
	t.Helper()
	cfg := config.StdioDiscoveryConfig{
		Services: []config.StdioServiceConfig{
			{
				Name:      "test",
				Namespace: "test",
				Command:   []string{"echo", "test"},
			},
		},
	}
	discovery, err := CreateStdioServiceDiscovery(cfg, logger)
	assert.NoError(t, err)
	stdioBridge, ok := discovery.(*StdioDiscovery)
	require.True(t, ok, "Expected StdioDiscovery type")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = stdioBridge.Watch(ctx)
	}()
	cancel() // Cancel immediately to exit the watch
	<-done   // Wait for watch to exit
}

func testWebSocketDiscoveryWatch(t *testing.T, logger *zap.Logger) {
	t.Helper()
	cfg := config.WebSocketDiscoveryConfig{
		Services: []config.WebSocketServiceConfig{
			{
				Name:      "test",
				Namespace: "test",
				Endpoints: []string{"ws://localhost:8080/ws"},
			},
		},
	}
	discovery, err := CreateWebSocketServiceDiscovery(cfg, logger)
	assert.NoError(t, err)
	wsBridge, ok := discovery.(*WebSocketDiscovery)
	require.True(t, ok, "Expected WebSocketDiscovery type")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = wsBridge.Watch(ctx)
	}()
	cancel() // Cancel immediately to exit the watch
	<-done   // Wait for watch to exit
}

// TestHealthCheckMethodsCoverage tests HealthCheck methods to improve coverage.
func TestHealthCheckMethodsCoverage(t *testing.T) {
	logger := zaptest.NewLogger(t)
	t.Run("SSE Discovery HealthCheck", func(t *testing.T) {
		testSSEDiscoveryHealthCheck(t, logger)
	})
	t.Run("Stdio Discovery HealthCheck", func(t *testing.T) {
		testStdioDiscoveryHealthCheck(t, logger)
	})
	t.Run("WebSocket Discovery HealthCheck", func(t *testing.T) {
		testWebSocketDiscoveryHealthCheck(t, logger)
	})
}

func testSSEDiscoveryHealthCheck(t *testing.T, logger *zap.Logger) {
	t.Helper()
	cfg := config.SSEDiscoveryConfig{
		Services: []config.SSEServiceConfig{
			{
				Name:      "test",
				Namespace: "test",
				BaseURL:   "http://localhost:19999", // Non-existent port
				HealthCheck: config.SSEHealthCheckConfig{
					Enabled: true,
				},
			},
		},
	}
	discovery, err := CreateSSEServiceDiscovery(cfg, logger)
	assert.NoError(t, err)
	sseBridge, ok := discovery.(*SSEDiscovery)
	require.True(t, ok, "Expected SSEDiscovery type")
	ctx := context.Background()
	endpoint := &Endpoint{
		Service:   "test",
		Namespace: "test",
	}
	// Should complete without panic (expect error due to non-existent service)
	err = sseBridge.HealthCheck(ctx, endpoint)
	assert.Error(t, err)
}

func testStdioDiscoveryHealthCheck(t *testing.T, logger *zap.Logger) {
	t.Helper()
	cfg := config.StdioDiscoveryConfig{
		Services: []config.StdioServiceConfig{
			{
				Name:      "test",
				Namespace: "test",
				Command:   []string{"nonexistent-command"},
				HealthCheck: config.StdioHealthCheckConfig{
					Enabled: true,
				},
			},
		},
	}
	discovery, err := CreateStdioServiceDiscovery(cfg, logger)
	assert.NoError(t, err)
	stdioBridge, ok := discovery.(*StdioDiscovery)
	require.True(t, ok, "Expected StdioDiscovery type")
	ctx := context.Background()
	endpoint := &Endpoint{
		Service:   "test",
		Namespace: "test",
	}
	// Should complete without panic (expect error due to non-existent command)
	err = stdioBridge.HealthCheck(ctx, endpoint)
	assert.Error(t, err)
}

func testWebSocketDiscoveryHealthCheck(t *testing.T, logger *zap.Logger) {
	t.Helper()
	cfg := config.WebSocketDiscoveryConfig{
		Services: []config.WebSocketServiceConfig{
			{
				Name:      "test",
				Namespace: "test",
				Endpoints: []string{"ws://localhost:19999/ws"}, // Non-existent port
				HealthCheck: config.WebSocketHealthCheckConfig{
					Enabled: true,
				},
			},
		},
	}
	discovery, err := CreateWebSocketServiceDiscovery(cfg, logger)
	assert.NoError(t, err)
	wsBridge, ok := discovery.(*WebSocketDiscovery)
	require.True(t, ok, "Expected WebSocketDiscovery type")
	ctx := context.Background()
	endpoint := &Endpoint{
		Service:   "test",
		Namespace: "test",
	}
	// Should complete without panic (expect error due to non-existent service)
	err = wsBridge.HealthCheck(ctx, endpoint)
	assert.Error(t, err)
}
