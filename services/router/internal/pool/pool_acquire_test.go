package pool

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.uber.org/zap/zaptest"
)

// TestPoolAcquire tests the refactored Acquire method.
func TestPoolAcquire(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Create a test connection factory.
	factory := &testFactory{}

	// Create pool config.
	cfg := Config{
		MaxSize:        5,
		MinSize:        1,
		MaxIdleTime:    time.Minute,
		AcquireTimeout: time.Second,
	}

	// Create pool.
	pool, err := NewPool(cfg, factory, logger)
	if err != nil {
		t.Fatalf("Failed to create pool: %v", err)
	}

	defer func() {
		if err := pool.Close(); err != nil {
			t.Logf("Failed to close pool: %v", err)
		}
	}()

	// Test acquiring a connection.
	ctx := context.Background()

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("Failed to acquire connection: %v", err)
	}

	if conn == nil {
		t.Fatal("Expected non-nil connection")
	}

	// Release the connection.
	if err := pool.Release(conn); err != nil {
		t.Fatalf("Failed to release connection: %v", err)
	}

	// Test acquiring multiple connections.
	var conns []Connection

	for i := 0; i < 3; i++ {
		conn, err := pool.Acquire(ctx)
		if err != nil {
			t.Fatalf("Failed to acquire connection %d: %v", i, err)
		}

		conns = append(conns, conn)
	}

	// Release all connections.
	for _, conn := range conns {
		if err := pool.Release(conn); err != nil {
			t.Fatalf("Failed to release connection: %v", err)
		}
	}

	t.Logf("Pool acquire test passed successfully")
}

// testFactory implements the Factory interface for testing.
type testFactory struct {
	createCount int
}

func (tf *testFactory) Create(ctx context.Context) (Connection, error) {
	tf.createCount++

	return &testConnection{
		id:    "test-conn-" + string(rune(tf.createCount)),
		alive: true,
	}, nil
}

func (tf *testFactory) Validate(conn Connection) error {
	if !conn.IsAlive() {
		return errors.New("connection not alive")
	}

	return nil
}

// testConnection is a simple test connection implementation.
type testConnection struct {
	id    string
	alive bool
}

func (tc *testConnection) IsAlive() bool {
	return tc.alive
}

func (tc *testConnection) Close() error {
	tc.alive = false

	return nil
}

func (tc *testConnection) GetID() string {
	return tc.id
}
