package pool

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/poiley/mcp-bridge/services/router/internal/config"
	"github.com/poiley/mcp-bridge/services/router/internal/constants"
	"github.com/poiley/mcp-bridge/services/router/pkg/mcp"
)

func TestNewPooledGatewayClient(t *testing.T) {
	t.Parallel()

	logger := zap.NewNop()
	poolConfig := Config{
		MinSize:        1,
		MaxSize:        5,
		AcquireTimeout: time.Second,
	}

	tests := []struct {
		name        string
		url         string
		expectError bool
		isWebSocket bool
	}{
		{
			name:        "WebSocket URL",
			url:         fmt.Sprintf("ws://localhost:%d", constants.TestHTTPPort),
			expectError: true, // Will fail to connect in test
			isWebSocket: true,
		},
		{
			name:        "WebSocket Secure URL",
			url:         "wss://localhost:8443",
			expectError: true, // Will fail to connect in test
			isWebSocket: true,
		},
		{
			name:        "TCP URL",
			url:         "tcp://localhost:9000",
			expectError: true, // Will fail to connect in test
			isWebSocket: false,
		},
		{
			name:        "TCP Secure URL",
			url:         "tcps://localhost:9443",
			expectError: true, // Will fail to connect in test
			isWebSocket: false,
		},
		{
			name:        "Invalid URL",
			url:         "://invalid",
			expectError: true,
		},
		{
			name:        "Unsupported Scheme",
			url:         fmt.Sprintf("http://localhost:%d", constants.TestHTTPPort),
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gwConfig := config.GatewayConfig{
				URL: tt.url,
			}

			client, err := NewPooledGatewayClient(poolConfig, gwConfig, logger)

			if tt.name == "Invalid URL" || tt.name == "Unsupported Scheme" {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			require.NotNil(t, client)

			if tt.isWebSocket {
				assert.NotNil(t, client.wsPool)
				assert.Nil(t, client.tcpPool)
			} else {
				assert.Nil(t, client.wsPool)
				assert.NotNil(t, client.tcpPool)
			}

			_ = client.Close()
		})
	}
}

func TestPooledGatewayClientLifecycle(t *testing.T) {
	t.Parallel()
	// This test verifies the basic lifecycle without actual connections.
	logger := zap.NewNop()
	poolConfig := Config{
		MinSize:        0, // Don't create connections on startup
		MaxSize:        5,
		AcquireTimeout: testIterations * time.Millisecond,
	}

	gwConfig := config.GatewayConfig{
		URL: fmt.Sprintf("ws://localhost:%d", constants.TestHTTPPort),
	}

	client, err := NewPooledGatewayClient(poolConfig, gwConfig, logger)
	require.NoError(t, err)

	defer func() {
		if err := client.Close(); err != nil {
			t.Logf("Failed to close client: %v", err)
		}
	}()

	// Test initial state.
	assert.False(t, client.IsConnected())

	// Test Stats.
	stats := client.Stats()
	assert.Equal(t, int64(0), stats.TotalConnections)
	assert.Equal(t, int64(0), stats.ActiveConnections)

	// Test Connect (will fail but should handle gracefully).
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout*time.Millisecond)
	defer cancel()

	err = client.Connect(ctx)
	require.Error(t, err) // Expected to fail in test environment

	// Test SendRequest without connection.
	req := &mcp.Request{
		ID:     "test-1",
		Method: "test",
	}
	err = client.SendRequest(req)
	require.Error(t, err)

	// Test ReceiveResponse without connection.
	_, err = client.ReceiveResponse()
	require.Error(t, err)

	// Test SendPing without connection.
	err = client.SendPing()
	require.Error(t, err)
}

func TestPooledGatewayClientAutoConnect(t *testing.T) {
	t.Parallel()
	// Test auto-connect behavior with mock factory.
	logger := zap.NewNop()

	// Create a mock factory that always fails.
	factory := &mockFactory{
		createErr: context.DeadlineExceeded,
	}

	poolConfig := DefaultConfig()
	poolConfig.MinSize = 0
	poolConfig.AcquireTimeout = testTimeout * time.Millisecond

	pool, err := NewPool(poolConfig, factory, logger)
	require.NoError(t, err)

	pgc := &PooledGatewayClient{
		wsPool:      pool,
		isWebSocket: true,
		logger:      logger,
	}

	defer func() { _ = pgc.Close() }()

	// Test SendRequest triggers auto-connect.
	req := &mcp.Request{
		ID:     "test-1",
		Method: "test",
	}

	err = pgc.SendRequest(req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to auto-connect")
}

func TestPooledGatewayClientConcurrentAccess(t *testing.T) {
	t.Parallel()

	logger := zap.NewNop()
	poolConfig := Config{
		MinSize:        0,
		MaxSize:        5,
		AcquireTimeout: time.Second,
	}

	gwConfig := config.GatewayConfig{
		URL: "tcp://localhost:9000",
	}

	client, err := NewPooledGatewayClient(poolConfig, gwConfig, logger)
	require.NoError(t, err)

	defer func() {
		if err := client.Close(); err != nil {
			t.Logf("Failed to close client: %v", err)
		}
	}()

	// Test concurrent IsConnected calls.
	done := make(chan bool, 10)

	for i := 0; i < constants.TestBatchSize; i++ {
		go func() {
			_ = client.IsConnected()

			done <- true
		}()
	}

	for i := 0; i < constants.TestBatchSize; i++ {
		<-done
	}

	// Test concurrent Stats calls.
	for i := 0; i < constants.TestBatchSize; i++ {
		go func() {
			_ = client.Stats()

			done <- true
		}()
	}

	for i := 0; i < constants.TestBatchSize; i++ {
		<-done
	}
}
