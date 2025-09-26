
package protocol

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/poiley/mcp-bridge/services/gateway/internal/discovery"
)

func TestNewConnectionMultiplexer(t *testing.T) {
	logger := zaptest.NewLogger(t)
	multiplexer := NewConnectionMultiplexer(logger)

	require.NotNil(t, multiplexer)
	assert.NotNil(t, multiplexer.logger)
	assert.NotNil(t, multiplexer.pools)
	assert.NotNil(t, multiplexer.stats.PoolStats)
	assert.True(t, multiplexer.config.EnableMultiplexing)
	assert.Equal(t, 10, multiplexer.config.MaxConnectionsPerEndpoint)
	assert.Equal(t, 30*time.Second, multiplexer.config.ConnectionTimeout)
	assert.Equal(t, 5*time.Minute, multiplexer.config.IdleTimeout)
	assert.Equal(t, 30*time.Second, multiplexer.config.HealthCheckInterval)
}

func TestConnectionMultiplexer_GetConnection_DirectConnection(t *testing.T) {
	logger := zaptest.NewLogger(t)
	multiplexer := NewConnectionMultiplexer(logger)

	// Disable multiplexing to test direct connections
	multiplexer.config.EnableMultiplexing = false

	endpoint := &discovery.Endpoint{
		Address:   "127.0.0.1",
		Port:      8080,
		Scheme:    "websocket",
		Namespace: "test",
	}

	ctx := context.Background()
	conn, err := multiplexer.GetConnection(ctx, endpoint)

	require.NoError(t, err)
	require.NotNil(t, conn)
	assert.Contains(t, conn.ID, "direct_")
	assert.Equal(t, "websocket", conn.Protocol)
	assert.Equal(t, endpoint, conn.Endpoint)
	assert.True(t, conn.IsHealthy)
	assert.Equal(t, int64(1), conn.UseCount)
	assert.WithinDuration(t, time.Now(), conn.CreatedAt, time.Second)
	assert.WithinDuration(t, time.Now(), conn.LastUsedAt, time.Second)
}

func TestConnectionMultiplexer_GetConnection_Pooled(t *testing.T) {
	logger := zaptest.NewLogger(t)
	multiplexer := NewConnectionMultiplexer(logger)

	endpoint := &discovery.Endpoint{
		Address:   "127.0.0.1",
		Port:      8080,
		Scheme:    "websocket",
		Namespace: "test",
	}

	ctx := context.Background()

	// Get first connection - should create pool
	conn1, err := multiplexer.GetConnection(ctx, endpoint)
	require.NoError(t, err)
	require.NotNil(t, conn1)

	// Verify pool was created
	poolKey := multiplexer.getPoolKey(endpoint)
	multiplexer.poolsMu.RLock()
	pool, exists := multiplexer.pools[poolKey]
	multiplexer.poolsMu.RUnlock()

	assert.True(t, exists)
	assert.NotNil(t, pool)
	assert.Equal(t, endpoint, pool.endpoint)

	// Get second connection from same endpoint
	conn2, err := multiplexer.GetConnection(ctx, endpoint)
	require.NoError(t, err)
	require.NotNil(t, conn2)

	// Should be different connections
	assert.NotEqual(t, conn1.ID, conn2.ID)
}

func TestConnectionMultiplexer_createPool(t *testing.T) {
	logger := zaptest.NewLogger(t)
	multiplexer := NewConnectionMultiplexer(logger)

	endpoint := &discovery.Endpoint{
		Address:   "127.0.0.1",
		Port:      8080,
		Scheme:    "http",
		Namespace: "test",
	}

	pool := multiplexer.createPool(endpoint)

	require.NotNil(t, pool)
	assert.Equal(t, endpoint, pool.endpoint)
	assert.NotNil(t, pool.connections)
	assert.NotNil(t, pool.available)
	assert.Equal(t, multiplexer.config, pool.config)
	assert.NotNil(t, pool.ctx)
	assert.NotNil(t, pool.cancel)
	assert.NotNil(t, pool.logger)
	assert.Equal(t, multiplexer.getPoolKey(endpoint), pool.stats.EndpointKey)
	assert.WithinDuration(t, time.Now(), pool.stats.CreatedAt, time.Second)

	// Clean up
	pool.cancel()
}

func TestConnectionMultiplexer_getPoolKey(t *testing.T) {
	logger := zaptest.NewLogger(t)
	multiplexer := NewConnectionMultiplexer(logger)

	tests := []struct {
		name     string
		endpoint *discovery.Endpoint
		expected string
	}{
		{
			name: "Basic endpoint",
			endpoint: &discovery.Endpoint{
				Scheme:    "http",
				Address:   "127.0.0.1",
				Port:      8080,
				Namespace: "default",
			},
			expected: "http_127.0.0.1_8080_default",
		},
		{
			name: "WebSocket endpoint",
			endpoint: &discovery.Endpoint{
				Scheme:    "ws",
				Address:   "localhost",
				Port:      9090,
				Namespace: "websocket",
			},
			expected: "ws_localhost_9090_websocket",
		},
		{
			name: "Empty namespace",
			endpoint: &discovery.Endpoint{
				Scheme:    "https",
				Address:   "api.example.com",
				Port:      443,
				Namespace: "",
			},
			expected: "https_api.example.com_443_",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := multiplexer.getPoolKey(tt.endpoint)
			assert.Equal(t, tt.expected, key)
		})
	}
}

func TestConnectionPool_GetConnection(t *testing.T) {
	logger := zaptest.NewLogger(t)
	multiplexer := NewConnectionMultiplexer(logger)
	multiplexer.config.MaxConnectionsPerEndpoint = 2
	multiplexer.config.ConnectionTimeout = testIterations * time.Millisecond

	endpoint := &discovery.Endpoint{
		Address:   "127.0.0.1",
		Port:      8080,
		Scheme:    "http",
		Namespace: "test",
	}

	pool := multiplexer.createPool(endpoint)
	defer pool.cancel()

	ctx := context.Background()

	// Test first connection creation
	conn1, err := pool.GetConnection(ctx)
	require.NoError(t, err)
	require.NotNil(t, conn1)
	assert.Equal(t, int64(1), conn1.UseCount)

	// Test second connection creation
	conn2, err := pool.GetConnection(ctx)
	require.NoError(t, err)
	require.NotNil(t, conn2)
	assert.NotEqual(t, conn1.ID, conn2.ID)

	// Return first connection to pool
	pool.ReturnConnection(conn1)

	// Test getting connection from pool (should reuse conn1)
	conn3, err := pool.GetConnection(ctx)
	require.NoError(t, err)
	require.NotNil(t, conn3)
	assert.Equal(t, conn1.ID, conn3.ID)
	assert.Equal(t, int64(2), conn3.UseCount) // Should be incremented
}

func TestConnectionPool_GetConnection_Timeout(t *testing.T) {
	logger := zaptest.NewLogger(t)
	multiplexer := NewConnectionMultiplexer(logger)
	multiplexer.config.MaxConnectionsPerEndpoint = 1
	multiplexer.config.ConnectionTimeout = testTimeout * time.Millisecond

	endpoint := &discovery.Endpoint{
		Address:   "127.0.0.1",
		Port:      8080,
		Scheme:    "http",
		Namespace: "test",
	}

	pool := multiplexer.createPool(endpoint)
	defer pool.cancel()

	ctx := context.Background()

	// Create the maximum number of connections
	conn1, err := pool.GetConnection(ctx)
	require.NoError(t, err)
	require.NotNil(t, conn1)

	// Try to get another connection - should timeout
	start := time.Now()
	conn2, err := pool.GetConnection(ctx)
	duration := time.Since(start)

	assert.Error(t, err)
	assert.Nil(t, conn2)
	assert.Contains(t, err.Error(), "timeout waiting for connection")
	assert.GreaterOrEqual(t, duration, testTimeout*time.Millisecond)
}

func TestConnectionPool_GetConnection_ContextCanceled(t *testing.T) {
	logger := zaptest.NewLogger(t)
	multiplexer := NewConnectionMultiplexer(logger)
	multiplexer.config.MaxConnectionsPerEndpoint = 1

	endpoint := &discovery.Endpoint{
		Address:   "127.0.0.1",
		Port:      8080,
		Scheme:    "http",
		Namespace: "test",
	}

	pool := multiplexer.createPool(endpoint)
	defer pool.cancel()

	// Create context that will be canceled
	ctx, cancel := context.WithCancel(context.Background())

	// Create the maximum number of connections
	conn1, err := pool.GetConnection(ctx)
	require.NoError(t, err)
	require.NotNil(t, conn1)

	// Cancel context and try to get another connection
	cancel()

	conn2, err := pool.GetConnection(ctx)

	assert.Error(t, err)
	assert.Nil(t, conn2)
	assert.Equal(t, context.Canceled, err)
}

func TestConnectionPool_createConnection(t *testing.T) {
	logger := zaptest.NewLogger(t)
	multiplexer := NewConnectionMultiplexer(logger)

	endpoint := &discovery.Endpoint{
		Address:   "127.0.0.1",
		Port:      8080,
		Scheme:    "websocket",
		Namespace: "test",
	}

	pool := multiplexer.createPool(endpoint)
	defer pool.cancel()

	ctx := context.Background()
	conn, err := pool.createConnection(ctx)

	require.NoError(t, err)
	require.NotNil(t, conn)
	assert.NotEmpty(t, conn.ID)
	assert.Contains(t, conn.ID, "websocket_8080_")
	assert.Equal(t, "websocket", conn.Protocol)
	assert.Equal(t, endpoint, conn.Endpoint)
	assert.True(t, conn.IsHealthy)
	assert.Equal(t, int64(1), conn.UseCount)
	assert.WithinDuration(t, time.Now(), conn.CreatedAt, time.Second)
	assert.WithinDuration(t, time.Now(), conn.LastUsedAt, time.Second)
}

func TestConnectionPool_ReturnConnection(t *testing.T) {
	logger := zaptest.NewLogger(t)
	multiplexer := NewConnectionMultiplexer(logger)

	endpoint := &discovery.Endpoint{
		Address:   "127.0.0.1",
		Port:      8080,
		Scheme:    "http",
		Namespace: "test",
	}

	pool := multiplexer.createPool(endpoint)
	defer pool.cancel()

	ctx := context.Background()
	conn, err := pool.GetConnection(ctx)
	require.NoError(t, err)

	// Return connection to pool
	pool.ReturnConnection(conn)

	// Verify we can get it back
	returnedConn, err := pool.GetConnection(ctx)
	require.NoError(t, err)
	assert.Equal(t, conn.ID, returnedConn.ID)
}

func TestConnectionPool_ReturnConnection_Unhealthy(t *testing.T) {
	logger := zaptest.NewLogger(t)
	multiplexer := NewConnectionMultiplexer(logger)

	endpoint := &discovery.Endpoint{
		Address:   "127.0.0.1",
		Port:      8080,
		Scheme:    "http",
		Namespace: "test",
	}

	pool := multiplexer.createPool(endpoint)
	defer pool.cancel()

	ctx := context.Background()
	conn, err := pool.GetConnection(ctx)
	require.NoError(t, err)

	// Mark connection as unhealthy
	conn.IsHealthy = false

	// Return unhealthy connection
	pool.ReturnConnection(conn)

	// Verify connection was removed from pool
	pool.mu.RLock()

	found := false

	for _, c := range pool.connections {
		if c.ID == conn.ID {
			found = true

			break
		}
	}

	pool.mu.RUnlock()
	assert.False(t, found)
}

func TestConnectionPool_isConnectionHealthy(t *testing.T) {
	logger := zaptest.NewLogger(t)
	multiplexer := NewConnectionMultiplexer(logger)
	multiplexer.config.IdleTimeout = testIterations * time.Millisecond

	endpoint := &discovery.Endpoint{
		Address:   "127.0.0.1",
		Port:      8080,
		Scheme:    "http",
		Namespace: "test",
	}

	pool := multiplexer.createPool(endpoint)
	defer pool.cancel()

	t.Run("nil connection", func(t *testing.T) {
		healthy := pool.isConnectionHealthy(nil)
		assert.False(t, healthy)
	})

	t.Run("healthy connection", func(t *testing.T) {
		conn := &PooledConnection{
			ID:         "test",
			CreatedAt:  time.Now(),
			LastUsedAt: time.Now(),
			IsHealthy:  true,
		}
		healthy := pool.isConnectionHealthy(conn)
		assert.True(t, healthy)
	})

	t.Run("unhealthy connection", func(t *testing.T) {
		conn := &PooledConnection{
			ID:         "test",
			CreatedAt:  time.Now(),
			LastUsedAt: time.Now(),
			IsHealthy:  false,
		}
		healthy := pool.isConnectionHealthy(conn)
		assert.False(t, healthy)
	})

	t.Run("too old connection", func(t *testing.T) {
		conn := &PooledConnection{
			ID:         "test",
			CreatedAt:  time.Now().Add(-25 * time.Hour), // Too old
			LastUsedAt: time.Now(),
			IsHealthy:  true,
		}
		healthy := pool.isConnectionHealthy(conn)
		assert.False(t, healthy)
	})

	t.Run("idle connection", func(t *testing.T) {
		conn := &PooledConnection{
			ID:         "test",
			CreatedAt:  time.Now(),
			LastUsedAt: time.Now().Add(-httpStatusOK * time.Millisecond), // Idle too long
			IsHealthy:  true,
		}
		healthy := pool.isConnectionHealthy(conn)
		assert.False(t, healthy)
	})
}

func TestConnectionPool_removeConnection(t *testing.T) {
	logger := zaptest.NewLogger(t)
	multiplexer := NewConnectionMultiplexer(logger)

	endpoint := &discovery.Endpoint{
		Address:   "127.0.0.1",
		Port:      8080,
		Scheme:    "http",
		Namespace: "test",
	}

	pool := multiplexer.createPool(endpoint)
	defer pool.cancel()

	ctx := context.Background()

	// Create some connections
	conn1, err := pool.GetConnection(ctx)
	require.NoError(t, err)
	conn2, err := pool.GetConnection(ctx)
	require.NoError(t, err)

	// Verify both connections exist
	pool.mu.RLock()
	initialCount := len(pool.connections)
	pool.mu.RUnlock()
	assert.Equal(t, 2, initialCount)

	// Remove one connection
	pool.removeConnection(conn1)

	// Verify connection was removed
	pool.mu.RLock()
	finalCount := len(pool.connections)
	conn1Found := false
	conn2Found := false

	for _, c := range pool.connections {
		if c.ID == conn1.ID {
			conn1Found = true
		}

		if c.ID == conn2.ID {
			conn2Found = true
		}
	}

	pool.mu.RUnlock()

	assert.Equal(t, 1, finalCount)
	assert.False(t, conn1Found) // conn1 should be removed
	assert.True(t, conn2Found)  // conn2 should remain
}

func TestConnectionPool_cleanupIdleConnections(t *testing.T) {
	logger := zaptest.NewLogger(t)
	multiplexer := NewConnectionMultiplexer(logger)
	multiplexer.config.IdleTimeout = testTimeout * time.Millisecond

	endpoint := &discovery.Endpoint{
		Address:   "127.0.0.1",
		Port:      8080,
		Scheme:    "http",
		Namespace: "test",
	}

	pool := multiplexer.createPool(endpoint)
	defer pool.cancel()

	ctx := context.Background()

	// Create connections
	conn1, err := pool.GetConnection(ctx)
	require.NoError(t, err)
	conn2, err := pool.GetConnection(ctx)
	require.NoError(t, err)

	// Make one connection idle
	conn1.mu.Lock()
	conn1.LastUsedAt = time.Now().Add(-testIterations * time.Millisecond)
	conn1.mu.Unlock()

	// Keep the other recent
	conn2.mu.Lock()
	conn2.LastUsedAt = time.Now()
	conn2.mu.Unlock()

	// Run cleanup
	pool.cleanupIdleConnections()

	// Verify idle connection was removed
	pool.mu.RLock()
	remainingCount := len(pool.connections)
	found := false

	for _, c := range pool.connections {
		if c.ID == conn2.ID {
			found = true

			break
		}
	}

	pool.mu.RUnlock()

	assert.Equal(t, 1, remainingCount)
	assert.True(t, found) // conn2 should remain
}

func TestConnectionPool_performHealthCheck(t *testing.T) {
	logger := zaptest.NewLogger(t)
	multiplexer := NewConnectionMultiplexer(logger)

	endpoint := &discovery.Endpoint{
		Address:   "127.0.0.1",
		Port:      8080,
		Scheme:    "http",
		Namespace: "test",
	}

	pool := multiplexer.createPool(endpoint)
	defer pool.cancel()

	t.Run("healthy connection", func(t *testing.T) {
		conn := &PooledConnection{
			ID:         "test",
			CreatedAt:  time.Now(),
			LastUsedAt: time.Now(),
			IsHealthy:  true,
		}
		healthy := pool.performHealthCheck(conn)
		assert.True(t, healthy)
	})

	t.Run("unhealthy connection", func(t *testing.T) {
		conn := &PooledConnection{
			ID:         "test",
			CreatedAt:  time.Now(),
			LastUsedAt: time.Now(),
			IsHealthy:  false,
		}
		healthy := pool.performHealthCheck(conn)
		assert.False(t, healthy)
	})
}

func TestConnectionPool_updateStats(t *testing.T) {
	logger := zaptest.NewLogger(t)
	multiplexer := NewConnectionMultiplexer(logger)

	endpoint := &discovery.Endpoint{
		Address:   "127.0.0.1",
		Port:      8080,
		Scheme:    "http",
		Namespace: "test",
	}

	pool := multiplexer.createPool(endpoint)
	defer pool.cancel()

	ctx := context.Background()

	// Create a connection to have stats to track
	_, err := pool.GetConnection(ctx)
	require.NoError(t, err)

	// Test request served
	initialServed := pool.stats.RequestsServed
	pool.updateStats("request_served")
	assert.Equal(t, initialServed+1, pool.stats.RequestsServed)
	assert.WithinDuration(t, time.Now(), pool.stats.LastUsed, time.Second)

	// Test error count
	initialErrors := pool.stats.ErrorsCount
	pool.updateStats("error")
	assert.Equal(t, initialErrors+1, pool.stats.ErrorsCount)

	// Test other operation (no increment)
	beforeServed := pool.stats.RequestsServed
	beforeErrors := pool.stats.ErrorsCount
	pool.updateStats("other")
	assert.Equal(t, beforeServed, pool.stats.RequestsServed)
	assert.Equal(t, beforeErrors, pool.stats.ErrorsCount)
}

func TestConnectionMultiplexer_GetStats(t *testing.T) {
	logger := zaptest.NewLogger(t)
	multiplexer := NewConnectionMultiplexer(logger)

	endpoint1 := &discovery.Endpoint{
		Address:   "127.0.0.1",
		Port:      8080,
		Scheme:    "http",
		Namespace: "test1",
	}

	endpoint2 := &discovery.Endpoint{
		Address:   "127.0.0.1",
		Port:      8081,
		Scheme:    "websocket",
		Namespace: "test2",
	}

	ctx := context.Background()

	// Create connections to different endpoints
	_, err := multiplexer.GetConnection(ctx, endpoint1)
	require.NoError(t, err)
	_, err = multiplexer.GetConnection(ctx, endpoint2)
	require.NoError(t, err)

	// Get stats
	stats := multiplexer.GetStats()

	assert.Equal(t, 2, stats.TotalPools)
	assert.GreaterOrEqual(t, stats.TotalConnections, 2)
	assert.WithinDuration(t, time.Now(), stats.LastActivity, time.Second)
	assert.Len(t, stats.PoolStats, 2)

	// Verify pool stats exist for both endpoints
	key1 := multiplexer.getPoolKey(endpoint1)
	key2 := multiplexer.getPoolKey(endpoint2)

	assert.Contains(t, stats.PoolStats, key1)
	assert.Contains(t, stats.PoolStats, key2)
}

func TestConnectionMultiplexer_Shutdown(t *testing.T) {
	logger := zaptest.NewLogger(t)
	multiplexer := NewConnectionMultiplexer(logger)

	endpoint := &discovery.Endpoint{
		Address:   "127.0.0.1",
		Port:      8080,
		Scheme:    "http",
		Namespace: "test",
	}

	ctx := context.Background()

	// Create some connections
	_, err := multiplexer.GetConnection(ctx, endpoint)
	require.NoError(t, err)
	_, err = multiplexer.GetConnection(ctx, endpoint)
	require.NoError(t, err)

	// Verify pools exist
	multiplexer.poolsMu.RLock()
	initialPoolCount := len(multiplexer.pools)
	multiplexer.poolsMu.RUnlock()
	assert.Equal(t, 1, initialPoolCount)

	// Shutdown
	err = multiplexer.Shutdown(ctx)
	require.NoError(t, err)

	// Verify pools were cleaned up
	multiplexer.poolsMu.RLock()
	finalPoolCount := len(multiplexer.pools)
	multiplexer.poolsMu.RUnlock()
	assert.Equal(t, 0, finalPoolCount)
}

func TestConnectionPool_ManagementRoutines(t *testing.T) {
	logger := zaptest.NewLogger(t)
	multiplexer := NewConnectionMultiplexer(logger)
	// Use longer intervals to reduce race conditions
	multiplexer.config.IdleTimeout = 200 * time.Millisecond
	multiplexer.config.HealthCheckInterval = 300 * time.Millisecond

	endpoint := &discovery.Endpoint{
		Address:   "127.0.0.1",
		Port:      8080,
		Scheme:    "http",
		Namespace: "test",
	}

	pool := multiplexer.createPool(endpoint)

	ctx := context.Background()

	// Create connections
	conn1, err := pool.GetConnection(ctx)
	require.NoError(t, err)
	conn2, err := pool.GetConnection(ctx)
	require.NoError(t, err)

	// Make one connection idle
	conn1.mu.Lock()
	conn1.LastUsedAt = time.Now().Add(-testIterations * time.Millisecond)
	conn1.mu.Unlock()

	// Keep the other connection recent
	conn2.mu.Lock()
	conn2.LastUsedAt = time.Now()
	conn2.mu.Unlock()

	// Wait for management routines to run
	time.Sleep(500 * time.Millisecond)

	// Verify idle connection was cleaned up but active one remains
	pool.mu.RLock()
	connectionCount := len(pool.connections)
	pool.mu.RUnlock()

	// Should have fewer connections now (idle one removed)
	assert.LessOrEqual(t, connectionCount, 1)

	// Clean up
	pool.cancel()
}

// Benchmark tests.
func BenchmarkConnectionMultiplexer_GetConnection_Direct(b *testing.B) {
	logger := zaptest.NewLogger(b)
	multiplexer := NewConnectionMultiplexer(logger)
	multiplexer.config.EnableMultiplexing = false

	endpoint := &discovery.Endpoint{
		Address:   "127.0.0.1",
		Port:      8080,
		Scheme:    "http",
		Namespace: "test",
	}

	ctx := context.Background()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := multiplexer.GetConnection(ctx, endpoint)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkConnectionMultiplexer_GetConnection_Pooled(b *testing.B) {
	logger := zaptest.NewLogger(b)
	multiplexer := NewConnectionMultiplexer(logger)

	endpoint := &discovery.Endpoint{
		Address:   "127.0.0.1",
		Port:      8080,
		Scheme:    "http",
		Namespace: "test",
	}

	ctx := context.Background()

	// Pre-create pool
	_, err := multiplexer.GetConnection(ctx, endpoint)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		conn, err := multiplexer.GetConnection(ctx, endpoint)
		if err != nil {
			b.Fatal(err)
		}
		// Simulate returning connection to pool
		poolKey := multiplexer.getPoolKey(endpoint)
		if pool, exists := multiplexer.pools[poolKey]; exists {
			pool.ReturnConnection(conn)
		}
	}
}

func TestConnectionMultiplexer_Integration(t *testing.T) {
	logger := zaptest.NewLogger(t)
	multiplexer := NewConnectionMultiplexer(logger)
	multiplexer.config.MaxConnectionsPerEndpoint = 3

	// Set up test endpoints and create connections
	endpoints := createIntegrationTestEndpoints()
	connections := createMultipleConnections(t, multiplexer, endpoints)
	
	// Verify initial stats and test connection reuse
	verifyMultiplexerStats(t, multiplexer, len(endpoints), len(connections))
	returnAndReuseConnections(t, multiplexer, endpoints, connections)
	
	// Shutdown and verify cleanup
	shutdownAndVerifyCleanup(t, multiplexer)
}

func createIntegrationTestEndpoints() []*discovery.Endpoint {
	return []*discovery.Endpoint{
		{Address: "127.0.0.1", Port: 8080, Scheme: "http", Namespace: "test1"},
		{Address: "127.0.0.1", Port: 8081, Scheme: "websocket", Namespace: "test2"},
		{Address: "localhost", Port: 9090, Scheme: "https", Namespace: "test3"},
	}
}

func createMultipleConnections(
	t *testing.T, multiplexer *ConnectionMultiplexer, endpoints []*discovery.Endpoint,
) []*PooledConnection {
	ctx := context.Background()
	connections := make([]*PooledConnection, 0)

	// Create connections to multiple endpoints
	for _, endpoint := range endpoints {
		for i := 0; i < 2; i++ {
			conn, err := multiplexer.GetConnection(ctx, endpoint)
			require.NoError(t, err)

			connections = append(connections, conn)
		}
	}
	return connections
}

func verifyMultiplexerStats(t *testing.T, multiplexer *ConnectionMultiplexer, expectedPools, expectedConnections int) {
	stats := multiplexer.GetStats()
	assert.Equal(t, expectedPools, stats.TotalPools)
	assert.Equal(t, expectedConnections, stats.TotalConnections)
}

func returnAndReuseConnections(
	t *testing.T, multiplexer *ConnectionMultiplexer, endpoints []*discovery.Endpoint,
	connections []*PooledConnection,
) {
	ctx := context.Background()

	// Return some connections
	for i, conn := range connections {
		if i%2 == 0 {
			poolKey := multiplexer.getPoolKey(conn.Endpoint)
			if pool, exists := multiplexer.pools[poolKey]; exists {
				pool.ReturnConnection(conn)
			}
		}
	}

	// Verify we can reuse connections
	for _, endpoint := range endpoints {
		conn, err := multiplexer.GetConnection(ctx, endpoint)
		require.NoError(t, err)
		assert.NotNil(t, conn)
	}
}

func shutdownAndVerifyCleanup(t *testing.T, multiplexer *ConnectionMultiplexer) {
	ctx := context.Background()

	// Shutdown gracefully
	err := multiplexer.Shutdown(ctx)
	require.NoError(t, err)

	// Verify cleanup
	stats := multiplexer.GetStats()
	assert.Equal(t, 0, stats.TotalPools)
}
