package discovery

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/poiley/mcp-bridge/services/gateway/internal/config"
)

// TestConsulDiscovery_Start_WithoutRealConsul tests Start method without real Consul.
func TestConsulDiscovery_Start_WithoutRealConsul(t *testing.T) {
	t.Run("start fails when consul unavailable", func(t *testing.T) {
		cfg := config.ServiceDiscoveryConfig{
			Provider: "consul",
		}

		consulCfg := ConsulConfig{
			Address:       "localhost:18500", // Non-existent port
			ServicePrefix: "mcp/",
			Datacenter:    "dc1",
			WatchTimeout:  "5s",
		}

		logger := zaptest.NewLogger(t)

		discovery, err := InitializeConsulServiceDiscovery(cfg, consulCfg, logger)
		if err != nil {
			// Expected since Consul is not available
			t.Logf("Expected error creating Consul discovery: %v", err)

			return
		}

		// If we got a discovery instance, test that Start fails gracefully
		require.NotNil(t, discovery)

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		err = discovery.Start(ctx)
		require.Error(t, err, "Start should fail when Consul is unavailable")
		assert.Contains(t, err.Error(), "initial service discovery failed")

		// Clean up
		discovery.Stop()
	})
}

// TestConsulDiscovery_discoverServices_Logic tests discoverServices logic.
func TestConsulDiscovery_discoverServices_Logic(t *testing.T) {
	t.Run("test service filtering logic", func(t *testing.T) {
		// Test the core logic used in discoverServices without needing real Consul
		servicePrefix := "mcp/"

		tests := []struct {
			name          string
			serviceName   string
			tags          []string
			shouldInclude bool
		}{
			{
				name:          "valid mcp service",
				serviceName:   "mcp/weather",
				tags:          []string{"mcp", "http"},
				shouldInclude: true,
			},
			{
				name:          "mcp service without mcp tag",
				serviceName:   "mcp/weather",
				tags:          []string{"http", "api"},
				shouldInclude: false,
			},
			{
				name:          "non-mcp service",
				serviceName:   "weather-api",
				tags:          []string{"mcp", "http"},
				shouldInclude: false,
			},
			{
				name:          "empty service name",
				serviceName:   "",
				tags:          []string{"mcp"},
				shouldInclude: false,
			},
		}

		// Create a dummy discovery instance to use the hasTag method
		discovery := &ConsulDiscovery{}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				hasPrefix := len(tt.serviceName) >= len(servicePrefix) &&
					tt.serviceName[:len(servicePrefix)] == servicePrefix
				hasMcpTag := discovery.hasTag(tt.tags, "mcp")

				shouldInclude := hasPrefix && hasMcpTag
				assert.Equal(t, tt.shouldInclude, shouldInclude,
					"Service %s with tags %v should include: %v",
					tt.serviceName, tt.tags, tt.shouldInclude)
			})
		}
	})
}

// TestConsulDiscovery_watchServices_CancelContext tests watchServices cancellation.
func TestConsulDiscovery_watchServices_CancelContext(t *testing.T) {
	t.Run("watch stops on context cancellation", func(t *testing.T) {
		cfg := config.ServiceDiscoveryConfig{
			Provider: "consul",
		}

		logger := zaptest.NewLogger(t)

		discovery := &ConsulDiscovery{
			config:    cfg,
			logger:    logger,
			endpoints: make(map[string][]Endpoint),
			lastIndex: 0,
		}

		ctx, cancel := context.WithCancel(context.Background())
		discovery.ctx = ctx
		discovery.cancel = cancel

		// Add to waitgroup before starting goroutine
		discovery.wg.Add(1)

		// Start watchServices in a goroutine
		done := make(chan struct{})

		go func() {
			defer close(done)
			// This will fail immediately due to nil client, but we're testing the context cancellation
			discovery.watchServices()
		}()

		// Cancel context immediately
		cancel()

		// Verify the goroutine exits quickly
		select {
		case <-done:
			// Expected - goroutine should exit due to context cancellation
		case <-time.After(testIterations * time.Millisecond):
			t.Error("watchServices should have exited when context was cancelled")
		}
	})
}

// TestConsulDiscovery_periodicHealthCheck_CancelContext tests periodicHealthCheck cancellation.
func TestConsulDiscovery_periodicHealthCheck_CancelContext(t *testing.T) {
	t.Run("periodic health check stops on context cancellation", func(t *testing.T) {
		logger := zaptest.NewLogger(t)

		discovery := &ConsulDiscovery{
			logger:    logger,
			endpoints: make(map[string][]Endpoint),
		}

		ctx, cancel := context.WithCancel(context.Background())
		discovery.ctx = ctx
		discovery.cancel = cancel

		// Add to waitgroup before starting goroutine
		discovery.wg.Add(1)

		// Start periodicHealthCheck in a goroutine
		done := make(chan struct{})

		go func() {
			defer close(done)

			discovery.periodicHealthCheck()
		}()

		// Cancel context immediately
		cancel()

		// Verify the goroutine exits quickly
		select {
		case <-done:
			// Expected - goroutine should exit due to context cancellation
		case <-time.After(testIterations * time.Millisecond):
			t.Error("periodicHealthCheck should have exited when context was cancelled")
		}
	})
}

// TestConsulDiscovery_updateHealthStatus_EmptyEndpoints tests updateHealthStatus with empty endpoints.
func TestConsulDiscovery_updateHealthStatus_EmptyEndpoints(t *testing.T) {
	t.Run("update health status with no endpoints", func(t *testing.T) {
		logger := zaptest.NewLogger(t)

		discovery := &ConsulDiscovery{
			logger:    logger,
			endpoints: make(map[string][]Endpoint),
		}

		// This should not panic even with no client or endpoints
		err := discovery.updateHealthStatus()
		require.NoError(t, err, "updateHealthStatus should not error with empty endpoints")

		// Verify no endpoints were created
		assert.Empty(t, discovery.endpoints)
	})

	t.Run("update health status without consul client", func(t *testing.T) {
		logger := zaptest.NewLogger(t)

		discovery := &ConsulDiscovery{
			logger:    logger,
			endpoints: make(map[string][]Endpoint),
		}

		// This should not panic when there are no endpoints
		err := discovery.updateHealthStatus()
		require.NoError(t, err)

		// Verify no endpoints were created
		endpoints := discovery.GetEndpoints("any-namespace")
		assert.Empty(t, endpoints)
	})
}
