package pool

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// mockConnection implements the Connection interface.
type mockConnection struct {
	id     string
	alive  int32
	closed int32
}

func newMockConnection(id string) *mockConnection {
	return &mockConnection{
		id:    id,
		alive: 1,
	}
}

func (c *mockConnection) IsAlive() bool {
	return atomic.LoadInt32(&c.alive) == 1 && atomic.LoadInt32(&c.closed) == 0
}

func (c *mockConnection) Close() error {
	if !atomic.CompareAndSwapInt32(&c.closed, 0, 1) {
		// Already closed - this should be safe and not return an error.
		return nil
	}

	atomic.StoreInt32(&c.alive, 0)

	return nil
}

func (c *mockConnection) GetID() string {
	return c.id
}

func (c *mockConnection) SetAlive(alive bool) {
	if alive {
		atomic.StoreInt32(&c.alive, 1)
	} else {
		atomic.StoreInt32(&c.alive, 0)
	}
}

// mockFactory implements the Factory interface.
type mockFactory struct {
	createCount int32
	createErr   error
	validateErr error
}

//nolint:ireturn // Test helper requires interface return
func (f *mockFactory) Create(ctx context.Context) (Connection, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}

	count := atomic.AddInt32(&f.createCount, 1)

	return newMockConnection(fmt.Sprintf("conn-%d", count)), nil
}

func (f *mockFactory) Validate(conn Connection) error {
	return f.validateErr
}

func (f *mockFactory) GetCreateCount() int32 {
	return atomic.LoadInt32(&f.createCount)
}

func TestNewPool(t *testing.T) {
	logger := zap.NewNop()
	factory := &mockFactory{}

	t.Run("valid config", func(t *testing.T) {
		testValidPoolConfig(t, factory, logger)
	})

	t.Run("invalid config corrections", func(t *testing.T) {
		testInvalidPoolConfig(t, factory, logger)
	})

	t.Run("min greater than max", func(t *testing.T) {
		testMinGreaterThanMax(t, factory, logger)
	})
}

func testValidPoolConfig(t *testing.T, factory *mockFactory, logger *zap.Logger) {
	t.Helper()
	config := Config{
		MinSize:             2,
		MaxSize:             5,
		MaxIdleTime:         time.Minute,
		MaxLifetime:         time.Hour,
		AcquireTimeout:      time.Second,
		HealthCheckInterval: time.Second,
	}

	pool := createAndVerifyPool(t, config, factory, logger)
	defer closePool(t, pool)

	time.Sleep(testIterations * time.Millisecond)
	verifyMinSizeConnections(t, factory, config)

	stats := pool.Stats()
	assert.Equal(t, int64(config.MinSize), stats.TotalConnections)
}

func testInvalidPoolConfig(t *testing.T, factory *mockFactory, logger *zap.Logger) {
	t.Helper()
	config := Config{
		MinSize: -1,
		MaxSize: 0,
	}

	pool := createAndVerifyPool(t, config, factory, logger)
	defer closePool(t, pool)

	assert.Equal(t, 0, pool.config.MinSize)
	assert.Equal(t, 10, pool.config.MaxSize)
}

func testMinGreaterThanMax(t *testing.T, factory *mockFactory, logger *zap.Logger) {
	t.Helper()
	config := Config{
		MinSize: 10,
		MaxSize: 5,
	}

	pool := createAndVerifyPool(t, config, factory, logger)
	defer closePool(t, pool)

	assert.Equal(t, 5, pool.config.MinSize)
	assert.Equal(t, 5, pool.config.MaxSize)
}

func createAndVerifyPool(t *testing.T, config Config, factory *mockFactory, logger *zap.Logger) *Pool {
	t.Helper()
	pool, err := NewPool(config, factory, logger)
	require.NoError(t, err)
	require.NotNil(t, pool)

	return pool
}

func closePool(t *testing.T, pool *Pool) {
	t.Helper()
	if err := pool.Close(); err != nil {
		t.Logf("Failed to close pool: %v", err)
	}
}

func verifyMinSizeConnections(t *testing.T, factory *mockFactory, config Config) {
	t.Helper()
	expectedMinSize := func() int32 {
		if config.MinSize <= math.MaxInt32 && config.MinSize >= 0 {
			return int32(config.MinSize)
		}

		return 0
	}()
	assert.GreaterOrEqual(t, factory.GetCreateCount(), expectedMinSize)
}

func TestPoolAcquireRelease(t *testing.T) {
	logger := zap.NewNop()
	factory := &mockFactory{}
	config := Config{
		MinSize:        0,
		MaxSize:        3,
		AcquireTimeout: time.Second,
	}

	pool, err := NewPool(config, factory, logger)
	require.NoError(t, err)

	defer func() {
		if err := pool.Close(); err != nil {
			t.Logf("Failed to close pool: %v", err)
		}
	}()

	ctx := context.Background()

	t.Run("acquire and release", func(t *testing.T) {
		// Acquire connection.
		conn, err := pool.Acquire(ctx)
		require.NoError(t, err)
		require.NotNil(t, conn)

		stats := pool.Stats()
		assert.Equal(t, int64(1), stats.ActiveConnections)

		// Release connection.
		err = pool.Release(conn)
		require.NoError(t, err)

		stats = pool.Stats()
		assert.Equal(t, int64(0), stats.ActiveConnections)
		assert.Equal(t, int64(1), stats.IdleConnections)
	})

	t.Run("acquire multiple", func(t *testing.T) {
		conns := make([]Connection, 3)

		// Acquire max connections.
		for i := 0; i < 3; i++ {
			conn, err := pool.Acquire(ctx)
			require.NoError(t, err)

			conns[i] = conn
		}

		stats := pool.Stats()
		assert.Equal(t, int64(3), stats.ActiveConnections)
		assert.Equal(t, int64(3), stats.TotalConnections)

		// Release all.
		for _, conn := range conns {
			err := pool.Release(conn)
			require.NoError(t, err)
		}
	})
}

func TestPoolExhaustion(t *testing.T) {
	logger := zap.NewNop()
	factory := &mockFactory{}
	config := Config{
		MinSize:        0,
		MaxSize:        2,
		AcquireTimeout: testIterations * time.Millisecond,
	}

	pool, err := NewPool(config, factory, logger)
	require.NoError(t, err)

	defer func() {
		if err := pool.Close(); err != nil {
			t.Logf("Failed to close pool: %v", err)
		}
	}()

	ctx := context.Background()

	// Acquire all connections.
	conn1, err := pool.Acquire(ctx)
	require.NoError(t, err)

	conn2, err := pool.Acquire(ctx)
	require.NoError(t, err)

	// Verify pool is exhausted by checking stats.
	stats := pool.Stats()
	assert.Equal(t, int64(2), stats.ActiveConnections)
	assert.Equal(t, int64(2), stats.TotalConnections)

	// Try to acquire another - should timeout.
	shortCtx, shortCancel := context.WithTimeout(ctx, config.AcquireTimeout)
	defer shortCancel()

	start := time.Now()
	_, err = pool.Acquire(shortCtx)
	require.Error(t, err)
	// Error can be either "timeout" or "context deadline exceeded"
	assert.True(t, strings.Contains(err.Error(), "timeout") || strings.Contains(err.Error(), "context deadline exceeded"))
	assert.GreaterOrEqual(t, time.Since(start), config.AcquireTimeout)

	// Test that we can release and reuse connections.
	// Release one connection.
	err = pool.Release(conn1)
	require.NoError(t, err)

	// Verify one connection is now available.
	statsAfterRelease := pool.Stats()
	assert.Equal(t, int64(1), statsAfterRelease.ActiveConnections)

	// Release the other connection.
	err = pool.Release(conn2)
	require.NoError(t, err)

	// Verify all connections are released.
	finalStats := pool.Stats()
	assert.Equal(t, int64(0), finalStats.ActiveConnections)

	// Should be able to acquire new connections.
	conn3, err := pool.Acquire(ctx)
	require.NoError(t, err)
	require.NotNil(t, conn3)

	// Cleanup.
	_ = pool.Release(conn3)
}

func TestPoolWaiters(t *testing.T) {
	logger := zap.NewNop()
	factory := &mockFactory{}
	config := Config{
		MinSize:        0,
		MaxSize:        1,
		AcquireTimeout: time.Second,
	}

	pool, err := NewPool(config, factory, logger)
	require.NoError(t, err)

	defer func() {
		if err := pool.Close(); err != nil {
			t.Logf("Failed to close pool: %v", err)
		}
	}()

	ctx := context.Background()

	// Acquire the only connection.
	conn1, err := pool.Acquire(ctx)
	require.NoError(t, err)

	// Start a waiter.
	waiterDone := make(chan struct{})

	var waiterConn Connection

	var waiterErr error

	go func() {
		waiterConn, waiterErr = pool.Acquire(ctx)

		close(waiterDone)
	}()

	// Give waiter time to start waiting.
	time.Sleep(testTimeout * time.Millisecond)

	// Release connection - waiter should get it.
	err = pool.Release(conn1)
	require.NoError(t, err)

	// Wait for waiter to complete.
	<-waiterDone
	require.NoError(t, waiterErr)
	require.NotNil(t, waiterConn)

	// Cleanup.
	_ = pool.Release(waiterConn)
}

func TestPoolInvalidConnection(t *testing.T) {
	logger := zap.NewNop()
	factory := &mockFactory{}
	config := DefaultConfig()

	pool, err := NewPool(config, factory, logger)
	require.NoError(t, err)

	defer func() {
		if err := pool.Close(); err != nil {
			t.Logf("Failed to close pool: %v", err)
		}
	}()

	ctx := context.Background()

	// Acquire connection.
	conn, err := pool.Acquire(ctx)
	require.NoError(t, err)

	// Verify connection is initially alive.
	assert.True(t, conn.IsAlive())

	// Close connection to make it invalid.
	err = conn.Close()
	require.NoError(t, err)

	// Verify connection is no longer alive.
	assert.False(t, conn.IsAlive())

	// Check stats before release.
	statsBefore := pool.Stats()

	// Release invalid connection - should be removed from pool.
	err = pool.Release(conn)
	require.NoError(t, err)

	// Check final stats - invalid connection should be removed.
	statsAfter := pool.Stats()

	// Active connections should decrease by 1.
	assert.Equal(t, statsBefore.ActiveConnections-1, statsAfter.ActiveConnections)
	// Total connections should decrease by 1 (connection was removed).
	assert.Equal(t, statsBefore.TotalConnections-1, statsAfter.TotalConnections)
	// Closed count should increase.
	assert.Greater(t, statsAfter.ClosedCount, statsBefore.ClosedCount)
}

func TestPoolHealthCheck(t *testing.T) {
	logger := zap.NewNop()
	factory := &mockFactory{}
	config := Config{
		MinSize:             2,
		MaxSize:             5,
		MaxIdleTime:         2 * time.Second, // Longer idle time
		HealthCheckInterval: testIterations * time.Millisecond,
	}

	pool, err := NewPool(config, factory, logger)
	require.NoError(t, err)

	defer func() {
		if err := pool.Close(); err != nil {
			t.Logf("Failed to close pool: %v", err)
		}
	}()

	// Wait for initial connections to be created.
	require.Eventually(t, func() bool {
		stats := pool.Stats()

		return stats.TotalConnections >= int64(config.MinSize)
	}, time.Second, testTimeout*time.Millisecond, "Initial connections should be created")

	// Get initial stats.
	initialStats := pool.Stats()
	initialConns := initialStats.TotalConnections
	assert.GreaterOrEqual(t, initialConns, int64(config.MinSize))

	// Wait for health checks to run naturally (multiple health check intervals).
	// Connections should remain valid since they're well within MaxIdleTime.
	time.Sleep(3 * config.HealthCheckInterval)

	// Verify connections are still maintained.
	finalStats := pool.Stats()
	assert.GreaterOrEqual(t, finalStats.TotalConnections, int64(config.MinSize),
		"Pool should maintain minimum connections after health checks")

	// Test that expired connections are properly handled.
	t.Run("expired_connections", func(t *testing.T) {
		// Create a separate test with very short idle time.
		shortConfig := Config{
			MinSize:             1,
			MaxSize:             3,
			MaxIdleTime:         10 * time.Millisecond, // Very short
			HealthCheckInterval: testTimeout * time.Millisecond,
		}

		shortPool, err := NewPool(shortConfig, factory, logger)
		require.NoError(t, err)

		defer func() { _ = shortPool.Close() }()

		// Wait for initial connection.
		require.Eventually(t, func() bool {
			stats := shortPool.Stats()

			return stats.TotalConnections >= 1
		}, time.Second, 20*time.Millisecond)

		// Wait for connections to become idle and expire, then for health check to run.
		time.Sleep(shortConfig.MaxIdleTime + 2*shortConfig.HealthCheckInterval)

		// Should still have minimum connections after maintenance.
		require.Eventually(t, func() bool {
			stats := shortPool.Stats()

			return stats.TotalConnections >= int64(shortConfig.MinSize)
		}, time.Second, testTimeout*time.Millisecond, "Pool should recreate expired connections")
	})
}

func TestPoolConcurrentAccess(t *testing.T) {
	logger := zap.NewNop()
	factory := &mockFactory{}
	config := Config{
		MinSize:        0,
		MaxSize:        10,
		AcquireTimeout: time.Second,
	}

	pool, err := NewPool(config, factory, logger)
	require.NoError(t, err)

	defer func() {
		if err := pool.Close(); err != nil {
			t.Logf("Failed to close pool: %v", err)
		}
	}()

	ctx := context.Background()
	workers := 20
	iterations := 10

	var wg sync.WaitGroup

	errors := make(chan error, workers*iterations)

	for i := 0; i < workers; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for j := 0; j < iterations; j++ {
				conn, err := pool.Acquire(ctx)
				if err != nil {
					errors <- err

					continue
				}

				// Simulate work.
				time.Sleep(time.Millisecond)

				if err := pool.Release(conn); err != nil {
					errors <- err
				}
			}
		}()
	}

	wg.Wait()
	close(errors)

	// Check for errors.
	for err := range errors {
		t.Errorf("Concurrent access error: %v", err)
	}

	// Verify pool is still functional.
	conn, err := pool.Acquire(ctx)
	require.NoError(t, err)
	require.NotNil(t, conn)
	_ = pool.Release(conn)
}

func TestPoolClose(t *testing.T) {
	logger := zap.NewNop()
	factory := &mockFactory{}
	config := DefaultConfig()

	pool, err := NewPool(config, factory, logger)
	require.NoError(t, err)

	ctx := context.Background()

	// Acquire some connections.
	conn1, err := pool.Acquire(ctx)
	require.NoError(t, err)

	conn2, err := pool.Acquire(ctx)
	require.NoError(t, err)

	// Release one.
	_ = pool.Release(conn1)

	// Close pool.
	err = pool.Close()
	require.NoError(t, err)

	// Verify closed connections.
	assert.False(t, conn1.IsAlive())
	assert.False(t, conn2.IsAlive())

	// Try to acquire after close.
	_, err = pool.Acquire(ctx)
	require.Error(t, err)
	assert.Equal(t, ErrPoolClosed, err)

	// Close again should be safe.
	err = pool.Close()
	require.NoError(t, err)
}

func TestPoolStats(t *testing.T) {
	logger := zap.NewNop()
	factory := &mockFactory{}
	config := Config{
		MinSize: 2,
		MaxSize: 5,
	}

	pool, err := NewPool(config, factory, logger)
	require.NoError(t, err)

	defer func() {
		if err := pool.Close(); err != nil {
			t.Logf("Failed to close pool: %v", err)
		}
	}()

	// Wait for initial connections.
	time.Sleep(testIterations * time.Millisecond)

	stats := pool.Stats()
	assert.GreaterOrEqual(t, stats.CreatedCount, int64(config.MinSize))
	assert.Equal(t, stats.TotalConnections, stats.CreatedCount-stats.ClosedCount)

	ctx := context.Background()

	// Acquire and release.
	conn, err := pool.Acquire(ctx)
	require.NoError(t, err)

	stats = pool.Stats()
	assert.Equal(t, int64(1), stats.ActiveConnections)

	_ = pool.Release(conn)

	stats = pool.Stats()
	assert.Equal(t, int64(0), stats.ActiveConnections)
	assert.GreaterOrEqual(t, stats.IdleConnections, int64(1))
}

func TestPoolFactoryError(t *testing.T) {
	logger := zap.NewNop()
	factory := &mockFactory{
		createErr: errors.New("factory error"),
	}
	config := Config{
		MinSize: 0,
		MaxSize: 5,
	}

	pool, err := NewPool(config, factory, logger)
	require.NoError(t, err)

	defer func() {
		if err := pool.Close(); err != nil {
			t.Logf("Failed to close pool: %v", err)
		}
	}()

	ctx := context.Background()

	// Try to acquire - should get factory error.
	_, err = pool.Acquire(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "factory error")

	stats := pool.Stats()
	assert.Positive(t, stats.FailedCount)
}

func TestPoolValidation(t *testing.T) {
	logger := zap.NewNop()
	factory := &mockFactory{}
	config := DefaultConfig()

	pool, err := NewPool(config, factory, logger)
	require.NoError(t, err)

	defer func() {
		if err := pool.Close(); err != nil {
			t.Logf("Failed to close pool: %v", err)
		}
	}()

	ctx := context.Background()

	// Acquire connection.
	conn, err := pool.Acquire(ctx)
	require.NoError(t, err)

	// Set validation to fail.
	factory.validateErr = errors.New("validation failed")

	// Release - validation will fail, connection should be removed.
	err = pool.Release(conn)
	require.NoError(t, err)

	stats := pool.Stats()
	assert.Positive(t, stats.ClosedCount)
}

// Enhanced tests for better coverage.

// Test connection lifecycle with different aging scenarios.
func TestPoolConnectionLifecycle(t *testing.T) {
	logger := zap.NewNop()
	factory := &mockFactory{}

	t.Run("connection maximum lifetime", func(t *testing.T) {
		testMaxLifetime(t, factory, logger)
	})

	t.Run("connection idle timeout", func(t *testing.T) {
		testIdleTimeout(t, factory, logger)
	})
}

func testMaxLifetime(t *testing.T, factory *mockFactory, logger *zap.Logger) {
	t.Helper()
	config := Config{
		MinSize:             1,
		MaxSize:             3,
		MaxLifetime:         testIterations * time.Millisecond,
		MaxIdleTime:         1 * time.Second,
		HealthCheckInterval: testTimeout * time.Millisecond,
	}

	pool := createAndVerifyPool(t, config, factory, logger)
	defer closePool(t, pool)

	waitForInitialConnection(t, pool)
	initialStats := pool.Stats()
	initialCreated := initialStats.CreatedCount

	time.Sleep(config.MaxLifetime + 2*config.HealthCheckInterval)
	verifyConnectionReplacement(t, pool, initialCreated)

	finalStats := pool.Stats()
	assert.GreaterOrEqual(t, finalStats.TotalConnections, int64(config.MinSize))
}

func testIdleTimeout(t *testing.T, factory *mockFactory, logger *zap.Logger) {
	t.Helper()
	config := Config{
		MinSize:             1,
		MaxSize:             3,
		MaxLifetime:         1 * time.Hour,
		MaxIdleTime:         testTimeout * time.Millisecond,
		HealthCheckInterval: 30 * time.Millisecond,
	}

	pool := createAndVerifyPool(t, config, factory, logger)
	defer closePool(t, pool)

	testIdleConnection(t, pool)
	time.Sleep(config.MaxIdleTime + 2*config.HealthCheckInterval)
	verifyMinimumConnections(t, pool, config)
}

func waitForInitialConnection(t *testing.T, pool *Pool) {
	t.Helper()
	require.Eventually(t, func() bool {
		stats := pool.Stats()

		return stats.TotalConnections >= 1
	}, time.Second, 20*time.Millisecond)
}

func verifyConnectionReplacement(t *testing.T, pool *Pool, initialCreated int64) {
	t.Helper()
	require.Eventually(t, func() bool {
		stats := pool.Stats()

		return stats.CreatedCount > initialCreated
	}, time.Second, testTimeout*time.Millisecond, "New connections should be created to replace expired ones")
}

func testIdleConnection(t *testing.T, pool *Pool) {
	t.Helper()
	ctx := context.Background()
	conn, err := pool.Acquire(ctx)
	require.NoError(t, err)
	err = pool.Release(conn)
	require.NoError(t, err)
}

func verifyMinimumConnections(t *testing.T, pool *Pool, config Config) {
	t.Helper()
	require.Eventually(t, func() bool {
		stats := pool.Stats()

		return stats.TotalConnections >= int64(config.MinSize)
	}, time.Second, testTimeout*time.Millisecond, "Pool should maintain minimum connections")
}

// Test pool behavior under stress.
func TestPoolStressScenarios(t *testing.T) {
	logger := zap.NewNop()
	factory := &mockFactory{}

	t.Run("rapid acquire release cycles", func(t *testing.T) {
		runRapidAcquireReleaseCycles(t, factory, logger)
	})

	t.Run("connection failure during operations", func(t *testing.T) {
		runConnectionFailureDuringOperations(t, factory, logger)
	})
}

func runRapidAcquireReleaseCycles(t *testing.T, factory *mockFactory, logger *zap.Logger) {
	t.Helper()
	config := Config{
		MinSize:        2,
		MaxSize:        5,
		AcquireTimeout: time.Second,
	}

	pool, err := NewPool(config, factory, logger)
	require.NoError(t, err)

	defer func() {
		if err := pool.Close(); err != nil {
			t.Logf("Failed to close pool: %v", err)
		}
	}()

	ctx := context.Background()
	numCycles := testIterations

	for i := 0; i < numCycles; i++ {
		// Acquire multiple connections
		conns := make([]Connection, 3)

		for j := 0; j < 3; j++ {
			conn, err := pool.Acquire(ctx)
			require.NoError(t, err, "Cycle %d, connection %d", i, j)
			conns[j] = conn
		}

		// Release them immediately
		for _, conn := range conns {
			err := pool.Release(conn)
			require.NoError(t, err, "Cycle %d", i)
		}
	}

	// Pool should still be functional
	finalStats := pool.Stats()
	assert.Positive(t, finalStats.CreatedCount)
	assert.Equal(t, int64(0), finalStats.ActiveConnections)
}

func runConnectionFailureDuringOperations(t *testing.T, factory *mockFactory, logger *zap.Logger) {
	t.Helper()
	config := Config{
		MinSize:        1,
		MaxSize:        3,
		AcquireTimeout: time.Second,
	}

	pool, err := NewPool(config, factory, logger)
	require.NoError(t, err)

	defer func() {
		if err := pool.Close(); err != nil {
			t.Logf("Failed to close pool: %v", err)
		}
	}()

	ctx := context.Background()

	// Acquire connection
	conn, err := pool.Acquire(ctx)
	require.NoError(t, err)

	// Simulate connection failure
	mockConn, ok := conn.(*pooledConn).Connection.(*mockConnection)
	assert.True(t, ok, "type assertion failed")
	mockConn.SetAlive(false)

	// Release the failed connection
	err = pool.Release(conn)
	require.NoError(t, err)

	// Pool should handle the failed connection gracefully
	stats := pool.Stats()
	assert.Positive(t, stats.ClosedCount)

	// Should still be able to acquire new connections
	newConn, err := pool.Acquire(ctx)
	require.NoError(t, err)
	assert.True(t, newConn.IsAlive())
	_ = pool.Release(newConn)
}

// Test edge cases in pool operations.
func TestPoolEdgeCases(t *testing.T) {
	logger := zap.NewNop()
	factory := &mockFactory{}

	t.Run("release invalid connection", func(t *testing.T) {
		runReleaseInvalidConnectionTest(t, factory, logger)
	})

	t.Run("factory create errors", func(t *testing.T) {
		runFactoryCreateErrorsTest(t, factory, logger)
	})

	t.Run("concurrent close and operations", func(t *testing.T) {
		runConcurrentCloseAndOperationsTest(t, factory, logger)
	})
}

func runReleaseInvalidConnectionTest(t *testing.T, factory *mockFactory, logger *zap.Logger) {
	t.Helper()
	config := DefaultConfig()
	pool, err := NewPool(config, factory, logger)
	require.NoError(t, err)

	defer func() {
		if err := pool.Close(); err != nil {
			t.Logf("Failed to close pool: %v", err)
		}
	}()

	// Try to release nil connection
	err = pool.Release(nil)
	require.Error(t, err)
	assert.Equal(t, ErrInvalidConn, err)

	// Create a connection from different pool
	otherFactory := &mockFactory{}
	otherPool, err := NewPool(config, otherFactory, logger)
	require.NoError(t, err)

	defer func() { _ = otherPool.Close() }()

	ctx := context.Background()
	otherConn, err := otherPool.Acquire(ctx)
	require.NoError(t, err)

	// Try to release connection from other pool
	err = pool.Release(otherConn)
	require.Error(t, err)
	assert.Equal(t, ErrInvalidConn, err)

	// Cleanup
	_ = otherPool.Release(otherConn)
}

func runFactoryCreateErrorsTest(t *testing.T, factory *mockFactory, logger *zap.Logger) {
	t.Helper()
	config := Config{
		MinSize: 0,
		MaxSize: 2,
	}

	pool, err := NewPool(config, factory, logger)
	require.NoError(t, err)

	defer func() {
		if err := pool.Close(); err != nil {
			t.Logf("Failed to close pool: %v", err)
		}
	}()

	ctx := context.Background()

	// First acquisition should work
	conn1, err := pool.Acquire(ctx)
	require.NoError(t, err)

	// Set factory to fail
	factory.createErr = errors.New("factory failure")

	// Try to acquire another - should fail
	_, err = pool.Acquire(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "factory failure")

	// Release first connection
	_ = pool.Release(conn1)

	// Try to acquire again - should get the released connection
	conn2, err := pool.Acquire(ctx)
	require.NoError(t, err)
	assert.Equal(t, conn1.GetID(), conn2.GetID(), "Should reuse existing connection")

	_ = pool.Release(conn2)
}

func runConcurrentCloseAndOperationsTest(t *testing.T, factory *mockFactory, logger *zap.Logger) {
	t.Helper()
	config := Config{
		MinSize: 1,
		MaxSize: 3,
	}

	pool, err := NewPool(config, factory, logger)
	require.NoError(t, err)

	ctx := context.Background()

	var wg sync.WaitGroup

	stopChan := make(chan struct{})

	// Start background acquire/release operations
	for i := 0; i < 3; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for {
				select {
				case <-stopChan:
					return
				default:
					conn, err := pool.Acquire(ctx)
					if err != nil {
						// Pool might be closed
						return
					}

					time.Sleep(time.Millisecond)

					_ = pool.Release(conn)
				}
			}
		}()
	}

	// Let operations run briefly
	time.Sleep(testTimeout * time.Millisecond)

	// Close pool
	err = pool.Close()
	require.NoError(t, err)

	// Stop background operations
	close(stopChan)
	wg.Wait()

	// Further operations should fail
	_, err = pool.Acquire(ctx)
	require.Error(t, err)
	assert.Equal(t, ErrPoolClosed, err)
}

// Test pool statistics accuracy.
func TestPoolStatisticsAccuracy(t *testing.T) {
	logger := zap.NewNop()
	factory := &mockFactory{}
	config := Config{
		MinSize:             2,
		MaxSize:             5,
		MaxIdleTime:         1 * time.Second,
		HealthCheckInterval: httpStatusOK * time.Millisecond,
	}

	pool, err := NewPool(config, factory, logger)
	require.NoError(t, err)

	defer func() {
		if err := pool.Close(); err != nil {
			t.Logf("Failed to close pool: %v", err)
		}
	}()

	ctx := context.Background()

	// Wait for initial connections.
	require.Eventually(t, func() bool {
		stats := pool.Stats()

		return stats.TotalConnections >= int64(config.MinSize)
	}, time.Second, testTimeout*time.Millisecond)

	initialStats := pool.Stats()
	assert.Equal(t, int64(0), initialStats.ActiveConnections, "Initially no active connections")
	assert.GreaterOrEqual(t, initialStats.IdleConnections, int64(config.MinSize), "Should have minimum idle connections")
	assert.GreaterOrEqual(t, initialStats.CreatedCount, int64(config.MinSize), "Should have created minimum connections")

	// Acquire connections and verify stats.
	conns := make([]Connection, 3)

	for i := 0; i < 3; i++ {
		conn, err := pool.Acquire(ctx)
		require.NoError(t, err)

		conns[i] = conn

		stats := pool.Stats()
		assert.Equal(t, int64(i+1), stats.ActiveConnections, "Active count should increase")
	}

	midStats := pool.Stats()
	assert.Equal(t, int64(3), midStats.ActiveConnections)
	assert.GreaterOrEqual(t, midStats.WaitCount, int64(3), "Wait count should include acquisitions")

	// Release connections and verify stats.
	for i, conn := range conns {
		err := pool.Release(conn)
		require.NoError(t, err)

		stats := pool.Stats()
		assert.Equal(t, int64(3-i-1), stats.ActiveConnections, "Active count should decrease")
	}

	finalStats := pool.Stats()
	assert.Equal(t, int64(0), finalStats.ActiveConnections, "No active connections after release")
	assert.GreaterOrEqual(t, finalStats.IdleConnections, int64(1), "Should have idle connections")
}

// Test pool maintenance and background operations.
func TestPoolMaintenance(t *testing.T) {
	logger := zap.NewNop()
	factory := &mockFactory{}

	t.Run("minimum size maintenance", func(t *testing.T) {
		testMinimumSizeMaintenance(t, factory, logger)
	})

	t.Run("health check removes expired connections", func(t *testing.T) {
		testHealthCheckRemovesExpiredConnections(t, factory, logger)
	})
}

func testMinimumSizeMaintenance(t *testing.T, factory *mockFactory, logger *zap.Logger) {
	t.Helper()
	config := Config{
		MinSize:             2,
		MaxSize:             5,
		MaxIdleTime:         testIterations * time.Millisecond,
		HealthCheckInterval: testTimeout * time.Millisecond,
	}

	pool, err := NewPool(config, factory, logger)
	require.NoError(t, err)

	defer func() {
		if err := pool.Close(); err != nil {
			t.Logf("Failed to close pool: %v", err)
		}
	}()

	// Wait for initial connections.
	require.Eventually(t, func() bool {
		stats := pool.Stats()

		return stats.TotalConnections >= int64(config.MinSize)
	}, time.Second, testTimeout*time.Millisecond)

	initialStats := pool.Stats()
	assert.GreaterOrEqual(t, initialStats.TotalConnections, int64(config.MinSize))

	// Test basic functionality - that pool maintains its connections.
	ctx := context.Background()
	conn, err := pool.Acquire(ctx)
	require.NoError(t, err)
	err = pool.Release(conn)
	require.NoError(t, err)

	// Verify pool still maintains minimum connections.
	finalStats := pool.Stats()
	assert.GreaterOrEqual(t, finalStats.TotalConnections, int64(config.MinSize))
}

func testHealthCheckRemovesExpiredConnections(t *testing.T, factory *mockFactory, logger *zap.Logger) {
	t.Helper()
	config := Config{
		MinSize:             1,
		MaxSize:             3,
		MaxIdleTime:         20 * time.Millisecond, // Very short
		HealthCheckInterval: 30 * time.Millisecond,
	}

	pool, err := NewPool(config, factory, logger)
	require.NoError(t, err)

	defer func() {
		if err := pool.Close(); err != nil {
			t.Logf("Failed to close pool: %v", err)
		}
	}()

	ctx := context.Background()

	// Create extra connections beyond minimum.
	conns := make([]Connection, 3)
	for i := 0; i < 3; i++ {
		conn, err := pool.Acquire(ctx)
		require.NoError(t, err)
		conns[i] = conn
	}

	// Release them to make them idle.
	for _, conn := range conns {
		_ = pool.Release(conn)
	}

	// Check initial state - should have idle connections.
	require.Eventually(t, func() bool {
		stats := pool.Stats()

		return stats.IdleConnections >= 1
	}, time.Second, testTimeout*time.Millisecond)

	// Wait for connections to expire and health check to run.
	time.Sleep(config.MaxIdleTime + 2*config.HealthCheckInterval)

	// Should maintain minimum connections after cleanup.
	require.Eventually(t, func() bool {
		stats := pool.Stats()

		return stats.TotalConnections >= int64(config.MinSize)
	}, 2*time.Second, testIterations*time.Millisecond, "Should maintain minimum after cleanup")

	finalStats := pool.Stats()
	assert.GreaterOrEqual(t, finalStats.TotalConnections, int64(config.MinSize))
	assert.Positive(t, finalStats.ClosedCount, "Should have closed expired connections")
}

// Benchmark pool operations.
func BenchmarkPoolAcquireRelease(b *testing.B) {
	logger := zap.NewNop()
	factory := &mockFactory{}
	config := Config{
		MinSize: 5,
		MaxSize: 10,
	}

	pool, err := NewPool(config, factory, logger)
	require.NoError(b, err)

	defer func() {
		if err := pool.Close(); err != nil {
			b.Logf("Failed to close pool: %v", err)
		}
	}()

	ctx := context.Background()

	// Wait for initial connections.
	time.Sleep(testIterations * time.Millisecond)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			conn, err := pool.Acquire(ctx)
			if err != nil {
				b.Error(err)

				continue
			}

			if err := pool.Release(conn); err != nil {
				b.Error(err)
			}
		}
	})
}

func BenchmarkPoolConcurrentAccess(b *testing.B) {
	logger := zap.NewNop()
	factory := &mockFactory{}
	config := Config{
		MinSize: 2,
		MaxSize: 8,
	}

	pool, err := NewPool(config, factory, logger)
	require.NoError(b, err)

	defer func() {
		if err := pool.Close(); err != nil {
			b.Logf("Failed to close pool: %v", err)
		}
	}()

	ctx := context.Background()

	// Wait for initial connections.
	time.Sleep(testIterations * time.Millisecond)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			conn, err := pool.Acquire(ctx)
			if err != nil {
				b.Error(err)

				continue
			}

			// Simulate some work.
			time.Sleep(time.Microsecond)

			if err := pool.Release(conn); err != nil {
				b.Error(err)
			}
		}
	})
}
