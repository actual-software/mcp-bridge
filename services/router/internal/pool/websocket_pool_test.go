package pool

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	common "github.com/poiley/mcp-bridge/pkg/common/config"
	"github.com/poiley/mcp-bridge/services/router/internal/config"
	"github.com/poiley/mcp-bridge/services/router/internal/constants"
)

func TestWebSocketConnection(t *testing.T) {
	t.Parallel()
	// Test the WebSocket connection wrapper using the new generic implementation.
	conn := NewGenericConnection(nil) // nil client for testing

	// GetID should return a valid UUID.
	assert.NotEmpty(t, conn.GetID())
	assert.Nil(t, conn.GetClient())

	// Test IsAlive when alive flag is set but client is nil.
	// With nil client, IsAlive should return false due to defensive programming.
	assert.False(t, conn.IsAlive())

	// Test Close sets alive to 0.
	err := conn.Close()
	require.NoError(t, err)
	assert.False(t, conn.IsAlive())
}

func TestWebSocketFactory(t *testing.T) {
	t.Parallel()

	logger := zap.NewNop()
	gwConfig := config.GatewayConfig{
		URL: fmt.Sprintf("ws://localhost:%d", constants.TestHTTPPort),
		Auth: common.AuthConfig{
			Type:  "bearer",
			Token: "test-token",
		},
	}

	factory := NewGenericFactory(gwConfig, logger, createWebSocketClient, "WebSocket")
	require.NotNil(t, factory)

	// Test Validate with wrong connection type.
	wrongConn := &mockConnection{id: "wrong"}
	err := factory.Validate(wrongConn)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid connection type")

	// Test Validate with dead connection.
	deadConn := NewGenericConnection(nil)
	_ = deadConn.Close() // Makes it not alive

	err = factory.Validate(deadConn)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not alive")
}

func TestNewWebSocketPool(t *testing.T) {
	t.Parallel()

	logger := zap.NewNop()
	poolConfig := Config{
		MinSize:        0, // Don't create connections in test
		MaxSize:        5,
		AcquireTimeout: time.Second,
	}

	gwConfig := config.GatewayConfig{
		URL: "wss://gateway.example.com",
	}

	pool, err := NewWebSocketPool(poolConfig, gwConfig, logger)
	require.NoError(t, err)
	require.NotNil(t, pool)

	defer func() {
		if err := pool.Close(); err != nil {
			t.Logf("Failed to close pool: %v", err)
		}
	}()

	// Verify pool configuration using Stats() method since config is not exposed.
	stats := pool.Stats()
	assert.Equal(t, int64(0), stats.TotalConnections)
}

func TestWebSocketPoolIntegration(t *testing.T) {
	t.Parallel()

	logger := zap.NewNop()

	// Create pool with factory that will fail to create connections.
	poolConfig := Config{
		MinSize:        0,
		MaxSize:        3,
		AcquireTimeout: testIterations * time.Millisecond,
	}

	gwConfig := config.GatewayConfig{
		URL: fmt.Sprintf("ws://test.invalid:%d", constants.TestHTTPPort), // Invalid host to ensure connection fails
	}

	pool, err := NewWebSocketPool(poolConfig, gwConfig, logger)
	require.NoError(t, err)

	defer func() {
		if err := pool.Close(); err != nil {
			t.Logf("Failed to close pool: %v", err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), httpStatusOK*time.Millisecond)
	defer cancel()

	// Try to acquire - should fail due to connection error.
	conn, err := pool.Acquire(ctx)
	require.Error(t, err)
	assert.Nil(t, conn)

	// Check pool stats.
	stats := pool.Stats()
	assert.Positive(t, stats.FailedCount)
}
