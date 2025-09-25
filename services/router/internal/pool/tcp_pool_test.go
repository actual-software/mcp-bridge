package pool

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	common "github.com/poiley/mcp-bridge/pkg/common/config"
	"github.com/poiley/mcp-bridge/services/router/internal/config"
)



func TestTCPConnection(t *testing.T) { 
	t.Parallel()
	// Test the TCP connection wrapper using the new generic implementation.
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

func TestTCPFactory(t *testing.T) { 
	t.Parallel()

	logger := zap.NewNop()
	gwConfig := config.GatewayConfig{
		URL: "tcp://localhost:9000",
		Auth: common.AuthConfig{
			Type:  "bearer",
			Token: "test-token",
		},
	}

	factory := NewGenericFactory(gwConfig, logger, createTCPClient, "TCP")
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

func TestNewTCPPool(t *testing.T) { 
	t.Parallel()

	logger := zap.NewNop()
	poolConfig := Config{
		MinSize:        0, // Don't create connections in test
		MaxSize:        5,
		AcquireTimeout: time.Second,
	}

	gwConfig := config.GatewayConfig{
		URL: "tcps://gateway.example.com:9443",
	}

	pool, err := NewTCPPool(poolConfig, gwConfig, logger)
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

func TestTCPPoolIntegration(t *testing.T) { 
	t.Parallel()

	logger := zap.NewNop()

	// Create pool with factory that will fail to create connections.
	poolConfig := Config{
		MinSize:        0,
		MaxSize:        3,
		AcquireTimeout: testIterations * time.Millisecond,
	}

	gwConfig := config.GatewayConfig{
		URL: "tcp://test.invalid:9000", // Invalid host to ensure connection fails
	}

	pool, err := NewTCPPool(poolConfig, gwConfig, logger)
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

func TestTCPPoolWithTLS(t *testing.T) { 
	t.Parallel()

	logger := zap.NewNop()
	poolConfig := Config{
		MinSize:        0,
		MaxSize:        2,
		AcquireTimeout: time.Second,
	}

	// Test various TLS schemes.
	tlsSchemes := []string{
		"tcps://gateway.example.com:9443",
		"tcp+tls://gateway.example.com:9443",
	}

	for _, url := range tlsSchemes {
		t.Run(url, func(t *testing.T) {
			t.Parallel()

			gwConfig := config.GatewayConfig{
				URL: url,
				TLS: common.TLSConfig{
					Verify:     true,
					MinVersion: "1.3",
				},
			}

			pool, err := NewTCPPool(poolConfig, gwConfig, logger)
			require.NoError(t, err)
			require.NotNil(t, pool)

   defer func() {
		if err := pool.Close(); err != nil {
			t.Logf("Failed to close pool: %v", err)
		}
	}()

			// Verify the pool was created successfully (factory config is not directly accessible).
			stats := pool.Stats()
			assert.Equal(t, int64(0), stats.TotalConnections)
		})
	}
}
