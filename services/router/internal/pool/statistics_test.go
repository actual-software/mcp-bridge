package pool

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestPoolStatisticsAccounting(t *testing.T) {
	t.Parallel()

	logger := zap.NewNop()
	factory := &mockFactory{}
	config := Config{
		MinSize:        2,
		MaxSize:        4,
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

	// Initial state: 2 idle connections from MinSize.
	stats := pool.Stats()
	assert.Equal(t, int64(0), stats.ActiveConnections, "Initial active should be 0")
	assert.Equal(t, int64(2), stats.IdleConnections, "Initial idle should be 2 (MinSize)")
	assert.Equal(t, int64(2), stats.TotalConnections, "Initial total should be 2")

	// Acquire one connection.
	conn1, err := pool.Acquire(ctx)
	require.NoError(t, err)

	stats = pool.Stats()
	assert.Equal(t, int64(1), stats.ActiveConnections, "After acquire: active should be 1")
	assert.Equal(t, int64(1), stats.IdleConnections, "After acquire: idle should be 1")
	assert.Equal(t, int64(2), stats.TotalConnections, "After acquire: total should be 2")

	// Acquire second connection.
	conn2, err := pool.Acquire(ctx)
	require.NoError(t, err)

	stats = pool.Stats()
	assert.Equal(t, int64(2), stats.ActiveConnections, "After 2nd acquire: active should be 2")
	assert.Equal(t, int64(0), stats.IdleConnections, "After 2nd acquire: idle should be 0")
	assert.Equal(t, int64(2), stats.TotalConnections, "After 2nd acquire: total should be 2")

	// Acquire third connection (should create new one).
	conn3, err := pool.Acquire(ctx)
	require.NoError(t, err)

	stats = pool.Stats()
	assert.Equal(t, int64(3), stats.ActiveConnections, "After 3rd acquire: active should be 3")
	assert.Equal(t, int64(0), stats.IdleConnections, "After 3rd acquire: idle should be 0")
	assert.Equal(t, int64(3), stats.TotalConnections, "After 3rd acquire: total should be 3")

	// Release one connection.
	err = pool.Release(conn1)
	require.NoError(t, err)

	stats = pool.Stats()
	assert.Equal(t, int64(2), stats.ActiveConnections, "After release: active should be 2")
	assert.Equal(t, int64(1), stats.IdleConnections, "After release: idle should be 1")
	assert.Equal(t, int64(3), stats.TotalConnections, "After release: total should be 3")

	// Release all connections.
	err = pool.Release(conn2)
	require.NoError(t, err)
	err = pool.Release(conn3)
	require.NoError(t, err)

	stats = pool.Stats()
	assert.Equal(t, int64(0), stats.ActiveConnections, "After releasing all: active should be 0")
	assert.Equal(t, int64(3), stats.IdleConnections, "After releasing all: idle should be 3")
	assert.Equal(t, int64(3), stats.TotalConnections, "After releasing all: total should be 3")

	// Verify all numbers add up correctly.
	assert.Equal(t, stats.ActiveConnections+stats.IdleConnections, stats.TotalConnections,
		"Active + Idle should equal Total connections")
}
