package direct

import (
	"context"
	"errors"
	"fmt"
	"github.com/poiley/mcp-bridge/services/router/internal/constants"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func TestConnectionPool_Basic(t *testing.T) { 
	t.Parallel()

	config := DefaultConnectionPoolConfig()
	config.MaxActiveConnections = 3
	config.ConnectionTTL = 5 * time.Second
	config.IdleTimeout = 2 * time.Second
	config.EnableConnectionReuse = true

	logger := zaptest.NewLogger(t)
	pool := NewConnectionPool(config, logger)

	require.NotNil(t, pool)

	defer func() {
		if err := pool.Close(); err != nil {
			t.Logf("Failed to close pool: %v", err)
		}
	}()

	// Test getting a connection.
	ctx := context.Background()
	createFunc := func() (DirectClient, error) {
		return NewMockDirectClient("test", "stdio", "test://example"), nil
	}

	conn1, err := pool.GetConnection(ctx, "test://server1", ClientTypeStdio, createFunc)
	require.NoError(t, err)
	require.NotNil(t, conn1)
	assert.Equal(t, "test://server1", conn1.ServerURL)
	assert.Equal(t, ClientTypeStdio, conn1.Protocol)

	// Test connection reuse.
	conn2, err := pool.GetConnection(ctx, "test://server1", ClientTypeStdio, createFunc)
	require.NoError(t, err)
	require.NotNil(t, conn2)

	// Should be the same connection (reused).
	assert.Equal(t, conn1, conn2)
	assert.Equal(t, int64(2), conn1.GetUsageCount())

	// Test different server gets new connection.
	conn3, err := pool.GetConnection(ctx, "test://server2", ClientTypeStdio, createFunc)
	require.NoError(t, err)
	require.NotNil(t, conn3)
	assert.NotEqual(t, conn1, conn3)

	// Test stats.
	stats := pool.GetStats()
	assert.Equal(t, 2, stats["active_connections"])
	assert.Greater(t, stats["total_connections"], 1)
}

func TestConnectionPool_MaxConnections(t *testing.T) { 
	t.Parallel()

	config := DefaultConnectionPoolConfig()
	config.MaxActiveConnections = 2
	config.MaxIdleConnections = 1
	config.EnableConnectionReuse = false // Disable reuse for this test

	logger := zaptest.NewLogger(t)
	pool := NewConnectionPool(config, logger)

	require.NotNil(t, pool)

	defer func() {
		if err := pool.Close(); err != nil {
			t.Logf("Failed to close pool: %v", err)
		}
	}()

	ctx := context.Background()
	createFunc := func() (DirectClient, error) {
		return NewMockDirectClient("test", "stdio", "test://example"), nil
	}

	// Create first connection.
	conn1, err := pool.GetConnection(ctx, "test://server1", ClientTypeStdio, createFunc)
	require.NoError(t, err)
	require.NotNil(t, conn1)

	// Create second connection.
	conn2, err := pool.GetConnection(ctx, "test://server2", ClientTypeStdio, createFunc)
	require.NoError(t, err)
	require.NotNil(t, conn2)

	// Third connection should trigger eviction or fail (depending on idle connections).
	_, err = pool.GetConnection(ctx, "test://server3", ClientTypeStdio, createFunc)
	// This might succeed if idle connections were evicted, or fail if limit reached.
	if err != nil {
		assert.Contains(t, err.Error(), "maximum active connections")
	}

	stats := pool.GetStats()
	assert.LessOrEqual(t, stats["active_connections"], 2)
}

func TestConnectionPool_RemoveConnection(t *testing.T) { 
	t.Parallel()

	config := DefaultConnectionPoolConfig()
	logger := zaptest.NewLogger(t)
	pool := NewConnectionPool(config, logger)

	require.NotNil(t, pool)

	defer func() {
		if err := pool.Close(); err != nil {
			t.Logf("Failed to close pool: %v", err)
		}
	}()

	ctx := context.Background()
	createFunc := func() (DirectClient, error) {
		return NewMockDirectClient("test", "stdio", "test://example"), nil
	}

	// Create connection.
	_, err := pool.GetConnection(ctx, "test://server1", ClientTypeStdio, createFunc)
	require.NoError(t, err)

	// Remove connection.
	err = pool.RemoveConnection("test://server1", ClientTypeStdio)
	require.NoError(t, err)

	// Try to remove non-existent connection.
	err = pool.RemoveConnection("test://nonexistent", ClientTypeStdio)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection not found")
}

func TestConnectionPool_Stats(t *testing.T) { 
	t.Parallel()

	config := DefaultConnectionPoolConfig()
	logger := zaptest.NewLogger(t)
	pool := NewConnectionPool(config, logger)

	require.NotNil(t, pool)

	defer func() {
		if err := pool.Close(); err != nil {
			t.Logf("Failed to close pool: %v", err)
		}
	}()

	// Initial stats.
	stats := pool.GetStats()
	assert.Equal(t, 0, stats["active_connections"])
	assert.Equal(t, 0, stats["total_connections"])
	assert.NotNil(t, stats["protocol_breakdown"])

	ctx := context.Background()
	createFunc := func() (DirectClient, error) {
		return NewMockDirectClient("test", "stdio", "test://example"), nil
	}

	// Add a connection.
	_, err := pool.GetConnection(ctx, "test://server1", ClientTypeStdio, createFunc)
	require.NoError(t, err)

	// Check updated stats.
	stats = pool.GetStats()
	assert.Equal(t, 1, stats["active_connections"])
	assert.Equal(t, 1, stats["total_connections"])

	protocolBreakdown, ok := stats["protocol_breakdown"].(map[string]int)
	assert.True(t, ok, "type assertion failed")
	assert.Equal(t, 1, protocolBreakdown["stdio"])
}

// Enhanced test coverage for pool lifecycle management.
func TestConnectionPool_LifecycleManagement(t *testing.T) { 
	t.Parallel()

	config := DefaultConnectionPoolConfig()
	config.ConnectionTTL = 2 * time.Second
	config.IdleTimeout = 1 * time.Second
	config.CleanupInterval = 500 * time.Millisecond
	config.HealthCheckInterval = 200 * time.Millisecond

	logger := zaptest.NewLogger(t)
	pool := NewConnectionPool(config, logger)

	require.NotNil(t, pool)

	defer func() {
		if err := pool.Close(); err != nil {
			t.Logf("Failed to close pool: %v", err)
		}
	}()

	ctx := context.Background()
	createFunc := func() (DirectClient, error) {
		return NewMockDirectClient("test", "stdio", "test://example"), nil
	}

	// Test connection creation and TTL expiration.
	conn1, err := pool.GetConnection(ctx, "test://server1", ClientTypeStdio, createFunc)
	require.NoError(t, err)
	require.NotNil(t, conn1)

	// Verify connection is healthy.
	assert.True(t, conn1.GetHealthy())
	assert.False(t, conn1.IsExpired(config.ConnectionTTL))
	assert.False(t, conn1.IsIdle(config.IdleTimeout))

	// Wait for connection to expire.
	time.Sleep(3 * time.Second)

	// Connection should be expired.
	assert.True(t, conn1.IsExpired(config.ConnectionTTL))

	// Getting same connection should return new one after cleanup.
	time.Sleep(1 * time.Second) // Allow cleanup to run

	conn2, err := pool.GetConnection(ctx, "test://server1", ClientTypeStdio, createFunc)
	require.NoError(t, err)
	assert.NotEqual(t, conn1, conn2) // Should be different connection
}

func TestConnectionPool_IdleTimeout(t *testing.T) { 
	t.Parallel()

	config := DefaultConnectionPoolConfig()
	config.IdleTimeout = 500 * time.Millisecond
	config.CleanupInterval = 200 * time.Millisecond

	logger := zaptest.NewLogger(t)
	pool := NewConnectionPool(config, logger)

	require.NotNil(t, pool)

	defer func() {
		if err := pool.Close(); err != nil {
			t.Logf("Failed to close pool: %v", err)
		}
	}()

	ctx := context.Background()
	createFunc := func() (DirectClient, error) {
		return NewMockDirectClient("test", "stdio", "test://example"), nil
	}

	// Create connection.
	conn, err := pool.GetConnection(ctx, "test://server1", ClientTypeStdio, createFunc)
	require.NoError(t, err)

	// Initially not idle.
	assert.False(t, conn.IsIdle(config.IdleTimeout))

	// Wait for idle timeout.
	time.Sleep(600 * time.Millisecond)
	assert.True(t, conn.IsIdle(config.IdleTimeout))

	// Update last used time.
	conn.UpdateLastUsed()
	assert.False(t, conn.IsIdle(config.IdleTimeout))
}

func TestConnectionPool_HealthChecking(t *testing.T) { 
	t.Parallel()

	config := DefaultConnectionPoolConfig()
	config.HealthCheckInterval = 100 * time.Millisecond

	logger := zaptest.NewLogger(t)
	pool := NewConnectionPool(config, logger)

	require.NotNil(t, pool)

	defer func() {
		if err := pool.Close(); err != nil {
			t.Logf("Failed to close pool: %v", err)
		}
	}()

	ctx := context.Background()

	var mock *MockDirectClient

	createFunc := func() (DirectClient, error) {
		mock = NewMockDirectClient("test", "stdio", "test://example")

		return mock, nil
	}

	// Create connection.
	conn, err := pool.GetConnection(ctx, "test://server1", ClientTypeStdio, createFunc)
	require.NoError(t, err)

	// Initially healthy.
	assert.True(t, conn.GetHealthy())

	// Set health error directly on mock.
	mock.healthError = errors.New("simulated health failure")

	// Wait for health check to detect failure.
	time.Sleep(5 * constants.TestSleepShort)

	// Connection should now be unhealthy.
	assert.False(t, conn.GetHealthy())
}

func TestConnectionPool_ConnectionReuse(t *testing.T) { 
	t.Parallel()

	config := DefaultConnectionPoolConfig()
	config.EnableConnectionReuse = true

	logger := zaptest.NewLogger(t)
	pool := NewConnectionPool(config, logger)

	require.NotNil(t, pool)

	defer func() {
		if err := pool.Close(); err != nil {
			t.Logf("Failed to close pool: %v", err)
		}
	}()

	ctx := context.Background()

	var createCount int32

	createFunc := func() (DirectClient, error) {
		atomic.AddInt32(&createCount, 1)

		return NewMockDirectClient("test", "stdio", "test://example"), nil
	}

	// Get same connection multiple times.
	conn1, err := pool.GetConnection(ctx, "test://server1", ClientTypeStdio, createFunc)
	require.NoError(t, err)

	conn2, err := pool.GetConnection(ctx, "test://server1", ClientTypeStdio, createFunc)
	require.NoError(t, err)

	conn3, err := pool.GetConnection(ctx, "test://server1", ClientTypeStdio, createFunc)
	require.NoError(t, err)

	// All should be the same connection.
	assert.Equal(t, conn1, conn2)
	assert.Equal(t, conn2, conn3)

	// Only one connection should have been created.
	assert.Equal(t, int32(1), atomic.LoadInt32(&createCount))

	// Usage count should be incremented.
	assert.Equal(t, int64(3), conn1.GetUsageCount())
}

func TestConnectionPool_NoReuse(t *testing.T) { 
	t.Parallel()

	config := DefaultConnectionPoolConfig()
	config.EnableConnectionReuse = false

	logger := zaptest.NewLogger(t)
	pool := NewConnectionPool(config, logger)

	require.NotNil(t, pool)

	defer func() {
		if err := pool.Close(); err != nil {
			t.Logf("Failed to close pool: %v", err)
		}
	}()

	ctx := context.Background()

	var createCount int32

	createFunc := func() (DirectClient, error) {
		atomic.AddInt32(&createCount, 1)

		return NewMockDirectClient("test", "stdio", "test://example"), nil
	}

	// Get same connection multiple times.
	conn1, err := pool.GetConnection(ctx, "test://server1", ClientTypeStdio, createFunc)
	require.NoError(t, err)

	conn2, err := pool.GetConnection(ctx, "test://server1", ClientTypeStdio, createFunc)
	require.NoError(t, err)

	// Should be different connections.
	assert.NotEqual(t, conn1, conn2)

	// Two connections should have been created.
	assert.Equal(t, int32(2), atomic.LoadInt32(&createCount))
}

func TestConnectionPool_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	pool := setupConcurrentAccessPool(t)
	defer closeConcurrentAccessPool(t, pool)

	connections := runConcurrentAccess(t, pool)
	verifyConcurrentAccessResults(t, pool, connections)
}

func TestConnectionPool_EvictionPolicy(t *testing.T) { 
	t.Parallel()

	config := DefaultConnectionPoolConfig()
	config.MaxActiveConnections = 2
	config.IdleTimeout = 100 * time.Millisecond
	config.EnableConnectionReuse = false // Force new connections

	logger := zaptest.NewLogger(t)
	pool := NewConnectionPool(config, logger)

	require.NotNil(t, pool)

	defer func() {
		if err := pool.Close(); err != nil {
			t.Logf("Failed to close pool: %v", err)
		}
	}()

	ctx := context.Background()
	createFunc := func() (DirectClient, error) {
		return NewMockDirectClient("test", "stdio", "test://example"), nil
	}

	// Create first connection.
	_, err := pool.GetConnection(ctx, "test://server1", ClientTypeStdio, createFunc)
	require.NoError(t, err)

	// Create second connection.
	_, err = pool.GetConnection(ctx, "test://server2", ClientTypeStdio, createFunc)
	require.NoError(t, err)

	// Wait for connections to become idle.
	time.Sleep(150 * time.Millisecond)

	// Try to create third connection - should trigger eviction.
	conn3, err := pool.GetConnection(ctx, "test://server3", ClientTypeStdio, createFunc)

	// Should succeed due to eviction of idle connections.
	if err != nil {
		assert.Contains(t, err.Error(), "maximum active connections")
	} else {
		assert.NotNil(t, conn3)
	}

	// Verify stats don't exceed limits.
	stats := pool.GetStats()
	assert.LessOrEqual(t, stats["active_connections"], config.MaxActiveConnections)
}

func TestConnectionPool_MultiProtocol(t *testing.T) { 
	t.Parallel()

	config := DefaultConnectionPoolConfig()

	logger := zaptest.NewLogger(t)
	pool := NewConnectionPool(config, logger)

	require.NotNil(t, pool)

	defer func() {
		if err := pool.Close(); err != nil {
			t.Logf("Failed to close pool: %v", err)
		}
	}()

	ctx := context.Background()
	createFunc := func() (DirectClient, error) {
		return NewMockDirectClient("test", "stdio", "test://example"), nil
	}

	// Create connections for different protocols.
	protocols := []ClientType{ClientTypeStdio, ClientTypeHTTP, ClientTypeWebSocket, ClientTypeSSE}

	for _, protocol := range protocols {
		conn, err := pool.GetConnection(ctx, "test://server1", protocol, createFunc)
		require.NoError(t, err)
		assert.Equal(t, protocol, conn.Protocol)
	}

	// Verify protocol breakdown in stats.
	stats := pool.GetStats()

	protocolBreakdown, ok := stats["protocol_breakdown"].(map[string]int)
	if !ok {
		t.Fatalf("Expected protocol_breakdown to be map[string]int, got %T", stats["protocol_breakdown"])
	}

	for _, protocol := range protocols {
		assert.Equal(t, 1, protocolBreakdown[string(protocol)])
	}
}

func TestConnectionPool_ErrorHandling(t *testing.T) { 
	t.Parallel()

	config := DefaultConnectionPoolConfig()

	logger := zaptest.NewLogger(t)
	pool := NewConnectionPool(config, logger)

	require.NotNil(t, pool)

	defer func() {
		if err := pool.Close(); err != nil {
			t.Logf("Failed to close pool: %v", err)
		}
	}()

	ctx := context.Background()

	// Test create function error.
	errorCreateFunc := func() (DirectClient, error) {
		return nil, errors.New("simulated creation error")
	}

	conn, err := pool.GetConnection(ctx, "test://server1", ClientTypeStdio, errorCreateFunc)
	require.Error(t, err)
	assert.Nil(t, conn)
	assert.Contains(t, err.Error(), "failed to create client")

	// Test removal of non-existent connection.
	err = pool.RemoveConnection("test://nonexistent", ClientTypeStdio)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection not found")
}

func TestConnectionPool_Shutdown(t *testing.T) { 
	t.Parallel()

	config := DefaultConnectionPoolConfig()
	config.CleanupInterval = 50 * time.Millisecond
	config.HealthCheckInterval = 50 * time.Millisecond

	logger := zaptest.NewLogger(t)
	pool := NewConnectionPool(config, logger)
	require.NotNil(t, pool)

	ctx := context.Background()
	createFunc := func() (DirectClient, error) {
		return NewMockDirectClient("test", "stdio", "test://example"), nil
	}

	// Create some connections.
	for i := 0; i < 3; i++ {
		_, err := pool.GetConnection(ctx, fmt.Sprintf("test://server%d", i), ClientTypeStdio, createFunc)
		require.NoError(t, err)
	}

	// Verify connections exist.
	stats := pool.GetStats()
	assert.Equal(t, 3, stats["active_connections"])

	// Close pool.
	err := pool.Close()
	require.NoError(t, err)

	// Verify background tasks are stopped by waiting briefly.
	time.Sleep(2 * constants.TestSleepShort)
}

func TestConnectionPool_GetActiveConnections(t *testing.T) { 
	t.Parallel()

	config := DefaultConnectionPoolConfig()
	config.ConnectionTTL = 1 * time.Second

	logger := zaptest.NewLogger(t)
	pool := NewConnectionPool(config, logger)

	require.NotNil(t, pool)

	defer func() {
		if err := pool.Close(); err != nil {
			t.Logf("Failed to close pool: %v", err)
		}
	}()

	ctx := context.Background()
	createFunc := func() (DirectClient, error) {
		return NewMockDirectClient("test", "stdio", "test://example"), nil
	}

	// Create connections.
	conn1, err := pool.GetConnection(ctx, "test://server1", ClientTypeStdio, createFunc)
	require.NoError(t, err)

	_, err = pool.GetConnection(ctx, "test://server2", ClientTypeHTTP, createFunc)
	require.NoError(t, err)

	// Get active connections.
	activeConns := pool.GetActiveConnections()
	assert.Len(t, activeConns, 2)

	// Mark one as unhealthy.
	conn1.SetHealthy(false)

	// Should only return healthy connections.
	activeConns = pool.GetActiveConnections()
	assert.Len(t, activeConns, 1)
	assert.Contains(t, activeConns, "test://server2")

	// Wait for TTL expiration.
	time.Sleep(1200 * time.Millisecond)

	// Expired connections should not be returned.
	activeConns = pool.GetActiveConnections()
	// May be empty if all connections expired.
	for _, conn := range activeConns {
		assert.False(t, conn.IsExpired(config.ConnectionTTL))
	}
}

// Benchmark tests for pool performance.
func BenchmarkConnectionPool_GetConnection(b *testing.B) {
	config := DefaultConnectionPoolConfig()
	config.EnableConnectionReuse = true

	logger := zaptest.NewLogger(b)

	pool := NewConnectionPool(config, logger)

	defer func() {
		if err := pool.Close(); err != nil {
			b.Logf("Failed to close pool: %v", err)
		}
	}()

	ctx := context.Background()
	createFunc := func() (DirectClient, error) {
		return NewMockDirectClient("test", "stdio", "test://example"), nil
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err := pool.GetConnection(ctx, "test://server1", ClientTypeStdio, createFunc)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkConnectionPool_GetConnectionNoReuse(b *testing.B) {
	config := DefaultConnectionPoolConfig()
	config.EnableConnectionReuse = false

	logger := zaptest.NewLogger(b)

	pool := NewConnectionPool(config, logger)

	defer func() {
		if err := pool.Close(); err != nil {
			b.Logf("Failed to close pool: %v", err)
		}
	}()

	ctx := context.Background()
	createFunc := func() (DirectClient, error) {
		return NewMockDirectClient("test", "stdio", "test://example"), nil
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := pool.GetConnection(ctx, fmt.Sprintf("test://server%d", i), ClientTypeStdio, createFunc)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkConnectionPool_ConcurrentAccess(b *testing.B) {
	config := DefaultConnectionPoolConfig()
	config.MaxActiveConnections = 100
	config.EnableConnectionReuse = true

	logger := zaptest.NewLogger(b)

	pool := NewConnectionPool(config, logger)

	defer func() {
		if err := pool.Close(); err != nil {
			b.Logf("Failed to close pool: %v", err)
		}
	}()

	ctx := context.Background()
	createFunc := func() (DirectClient, error) {
		return NewMockDirectClient("test", "stdio", "test://example"), nil
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		var counter int
		for pb.Next() {
			serverURL := fmt.Sprintf("test://server%d", counter%10)

			_, err := pool.GetConnection(ctx, serverURL, ClientTypeStdio, createFunc)
			if err != nil {
				b.Fatal(err)
			}

			counter++
		}
	})
}

func BenchmarkConnectionPool_Stats(b *testing.B) {
	config := DefaultConnectionPoolConfig()

	logger := zaptest.NewLogger(b)

	pool := NewConnectionPool(config, logger)

	defer func() {
		if err := pool.Close(); err != nil {
			b.Logf("Failed to close pool: %v", err)
		}
	}()

	// Create some connections first.
	ctx := context.Background()
	createFunc := func() (DirectClient, error) {
		return NewMockDirectClient("test", "stdio", "test://example"), nil
	}

	for i := 0; i < constants.TestBatchSize; i++ {
		_, err := pool.GetConnection(ctx, fmt.Sprintf("test://server%d", i), ClientTypeStdio, createFunc)
		if err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = pool.GetStats()
	}
}

// Stress test for memory usage under load.
func TestConnectionPool_MemoryUsage(t *testing.T) { 
	if testing.Short() {
		t.Skip("Skipping memory usage test in short mode")
	}

	config := DefaultConnectionPoolConfig()
	config.MaxActiveConnections = 1000
	config.EnableConnectionReuse = true

	logger := zaptest.NewLogger(t)

	pool := NewConnectionPool(config, logger)

	defer func() {
		if err := pool.Close(); err != nil {
			t.Logf("Failed to close pool: %v", err)
		}
	}()

	ctx := context.Background()
	createFunc := func() (DirectClient, error) {
		return NewMockDirectClient("test", "stdio", "test://example"), nil
	}

	// Record initial memory.
	var initialMem runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&initialMem)

	// Create many connections.
	const numConnections = 500
	for i := 0; i < numConnections; i++ {
		_, err := pool.GetConnection(ctx, fmt.Sprintf("test://server%d", i), ClientTypeStdio, createFunc)
		require.NoError(t, err)
	}

	// Check memory usage.
	var finalMem runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&finalMem)

	memoryIncrease := finalMem.Alloc - initialMem.Alloc
	t.Logf("Memory increase: %d bytes (%.2f MB)", memoryIncrease, float64(memoryIncrease)/(1024*1024))

	// Verify stats.
	stats := pool.GetStats()
	assert.Equal(t, numConnections, stats["active_connections"])
	assert.Equal(t, numConnections, stats["total_connections"])
}

// Test pool behavior under stress.
func TestConnectionPool_StressTest(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	pool, config := setupStressTestPool(t)
	defer closeStressTestPool(t, pool)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	successRate := runStressTest(t, pool, ctx)
	verifyStressTestResults(t, pool, config, successRate)
}

// Helper functions for TestConnectionPool_ConcurrentAccess
func setupConcurrentAccessPool(t *testing.T) *ConnectionPool {
	t.Helper()

	config := DefaultConnectionPoolConfig()
	config.MaxActiveConnections = 10
	config.EnableConnectionReuse = true

	logger := zaptest.NewLogger(t)
	pool := NewConnectionPool(config, logger)

	require.NotNil(t, pool)

	return pool
}

func closeConcurrentAccessPool(t *testing.T, pool *ConnectionPool) {
	t.Helper()

	if err := pool.Close(); err != nil {
		t.Logf("Failed to close pool: %v", err)
	}
}

func runConcurrentAccess(t *testing.T, pool *ConnectionPool) []*PooledConnection {
	t.Helper()

	ctx := context.Background()

	var createCount int32

	createFunc := func() (DirectClient, error) {
		atomic.AddInt32(&createCount, 1)

		return NewMockDirectClient("test", "stdio", "test://example"), nil
	}

	// Launch multiple goroutines to access pool concurrently.
	const (
		numGoroutines        = 20
		requestsPerGoroutine = 10
	)

	var wg sync.WaitGroup

	connections := make(chan *PooledConnection, numGoroutines*requestsPerGoroutine)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)

		go func(routineID int) {
			defer wg.Done()

			for j := 0; j < requestsPerGoroutine; j++ {
				serverURL := fmt.Sprintf("test://server%d", routineID%3) // 3 different servers

				conn, err := pool.GetConnection(ctx, serverURL, ClientTypeStdio, createFunc)
				if err == nil {
					connections <- conn
				}

				time.Sleep(1 * time.Millisecond) // Small delay
			}
		}(i)
	}

	wg.Wait()
	close(connections)

	// Collect all connections.
	collectedConnections := make([]*PooledConnection, 0, numGoroutines*requestsPerGoroutine)
	for conn := range connections {
		collectedConnections = append(collectedConnections, conn)
	}

	return collectedConnections
}

func verifyConcurrentAccessResults(t *testing.T, pool *ConnectionPool, connections []*PooledConnection) {
	t.Helper()

	assert.NotEmpty(t, connections)

	// Verify stats are consistent.
	stats := pool.GetStats()
	assert.GreaterOrEqual(t, stats["active_connections"], 1)
	assert.LessOrEqual(t, stats["active_connections"], 10) // MaxActiveConnections from setup
}

// Helper functions for TestConnectionPool_StressTest
func setupStressTestPool(t *testing.T) (*ConnectionPool, *ConnectionPoolConfig) {
	t.Helper()

	config := DefaultConnectionPoolConfig()
	config.MaxActiveConnections = 50
	config.EnableConnectionReuse = true
	config.CleanupInterval = 100 * time.Millisecond

	logger := zaptest.NewLogger(t)
	pool := NewConnectionPool(config, logger)

	return pool, &config
}

func closeStressTestPool(t *testing.T, pool *ConnectionPool) {
	t.Helper()

	if err := pool.Close(); err != nil {
		t.Logf("Failed to close pool: %v", err)
	}
}

func runStressTest(t *testing.T, pool *ConnectionPool, ctx context.Context) float64 {
	t.Helper()

	createFunc := func() (DirectClient, error) {
		return NewMockDirectClient("test", "stdio", "test://example"), nil
	}

	// Launch multiple workers.
	const numWorkers = 20

	var wg sync.WaitGroup

	errorCount := int32(0)
	successCount := int32(0)

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)

		go func(workerID int) {
			defer wg.Done()

			for {
				select {
				case <-ctx.Done():
					return
				default:
					serverURL := fmt.Sprintf("test://server%d", workerID%10)

					_, err := pool.GetConnection(ctx, serverURL, ClientTypeStdio, createFunc)
					if err != nil {
						atomic.AddInt32(&errorCount, 1)
					} else {
						atomic.AddInt32(&successCount, 1)
					}

					time.Sleep(1 * time.Millisecond)
				}
			}
		}(i)
	}

	wg.Wait()

	totalRequests := atomic.LoadInt32(&successCount) + atomic.LoadInt32(&errorCount)
	successRate := float64(atomic.LoadInt32(&successCount)) / float64(totalRequests) * 100

	t.Logf("Stress test results: %d total requests, %.2f%% success rate", totalRequests, successRate)

	return successRate
}

func verifyStressTestResults(t *testing.T, pool *ConnectionPool, config *ConnectionPoolConfig, successRate float64) {
	t.Helper()

	// Verify pool is still functional.
	stats := pool.GetStats()
	assert.LessOrEqual(t, stats["active_connections"], config.MaxActiveConnections)
	assert.GreaterOrEqual(t, successRate, 80.0) // At least 80% success rate
}
