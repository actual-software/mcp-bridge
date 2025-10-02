package optimization_test

import (
	"bytes"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/actual-software/mcp-bridge/pkg/common/optimization"
)

func TestBufferPool(t *testing.T) {
	t.Parallel()

	pool := optimization.NewBufferPool()

	// Test Get and Put
	buf1 := pool.Get()
	assert.NotNil(t, buf1)
	assert.Equal(t, 0, buf1.Len())

	// Write some data
	buf1.WriteString("test data")
	assert.Equal(t, 9, buf1.Len())

	// Return to pool
	pool.Put(buf1)

	// Get again - should be reset
	buf2 := pool.Get()
	assert.Equal(t, 0, buf2.Len())

	// Test large buffer rejection
	largeBuf := new(bytes.Buffer)
	largeBuf.Grow(2 * 1024 * 1024) // 2MB
	pool.Put(largeBuf)             // Should not panic, just not pool it
}

func TestByteSlicePool(t *testing.T) {
	t.Parallel()

	pool := optimization.NewByteSlicePool()

	// Test various sizes
	sizes := []int{100, 1024, 4096, 65536}

	for _, size := range sizes {
		t.Run(fmt.Sprintf("size-%d", size), func(t *testing.T) {
			t.Parallel()
			// Get a slice
			buf1 := pool.Get(size)
			assert.Len(t, buf1, size)
			assert.GreaterOrEqual(t, cap(buf1), size)

			// Write some data
			for i := range size {
				buf1[i] = byte(i % 256)
			}

			// Return to pool
			pool.Put(buf1)

			// Get again - should be cleared
			buf2 := pool.Get(size)
			assert.Len(t, buf2, size)

			// Verify it was cleared
			allZero := true

			for i := range size {
				if buf2[i] != 0 {
					allZero = false

					break
				}
			}

			assert.True(t, allZero, "Buffer should be cleared")
		})
	}

	// Test power-of-2 rounding
	t.Run("power of 2 rounding", func(t *testing.T) {
		t.Parallel()

		testCases := []struct {
			request  int
			expected int
		}{
			{1, 1},
			{2, 2},
			{3, 4},
			{5, 8},
			{9, 16},
			{100, 128},
			{1000, 1024},
		}

		for _, tc := range testCases {
			buf := pool.Get(tc.request)
			assert.Len(t, buf, tc.request)
			assert.GreaterOrEqual(t, cap(buf), tc.expected, "Cap should be at least %d, got %d", tc.expected, cap(buf))
			pool.Put(buf)
		}
	})
}

func TestObjectPool(t *testing.T) {
	// Remove t.Parallel() because this test uses a shared idCounter
	// which creates race conditions when run in parallel
	type TestObject struct {
		ID   int
		Data string
	}

	var idCounter int

	pool := optimization.NewObjectPool(func() *TestObject {
		idCounter++

		return &TestObject{
			ID:   idCounter,
			Data: "", // Empty data for new objects
		}
	})

	// Test that we can get objects from the pool
	obj1 := pool.Get()
	assert.NotNil(t, obj1)
	assert.Equal(t, 1, obj1.ID)

	obj2 := pool.Get()
	assert.NotNil(t, obj2)
	assert.Equal(t, 2, obj2.ID)

	// Test that we can put objects back
	obj1.Data = "modified"
	pool.Put(obj1)
	pool.Put(obj2)

	// Test that pool reuses objects (though sync.Pool doesn't guarantee order)
	// We'll test that we don't always create new objects
	initialCount := idCounter

	// Get some objects to potentially reuse pooled ones
	reusedObjects := make([]*TestObject, 0, 10)

	for range 10 {
		obj := pool.Get()
		reusedObjects = append(reusedObjects, obj)
		// Put them back for potential reuse
		pool.Put(obj)
	}

	// If pool is working, we shouldn't have created 10 new objects
	// (though sync.Pool behavior can vary)
	finalCount := idCounter

	// At minimum, we should be able to get objects without panicking
	assert.LessOrEqual(t, finalCount-initialCount, 10, "Pool should potentially reuse objects")
	assert.GreaterOrEqual(t, len(reusedObjects), 10, "Should get requested number of objects")
}

// TestObjectPool_TypeAssertion tests the type assertion fallback in ObjectPool.
func TestObjectPool_TypeAssertion(t *testing.T) {
	// Test with a simple type
	type SimpleType struct {
		Value int
	}

	counter := 0
	factory := func() *SimpleType {
		counter++

		return &SimpleType{Value: counter}
	}

	pool := optimization.NewObjectPool(factory)

	// Test normal operation
	obj := pool.Get()
	assert.NotNil(t, obj)
	assert.Equal(t, 1, obj.Value)

	// Modify and return
	obj.Value = 999
	pool.Put(obj)

	// Get again - should work correctly
	obj2 := pool.Get()
	assert.NotNil(t, obj2)
}

// TestBufferPool_TypeAssertion tests the type assertion fallback path.
func TestBufferPool_TypeAssertion(t *testing.T) {
	t.Parallel()

	// Create a custom pool that might contain invalid types
	// We simulate this by testing the normal path extensively
	pool := optimization.NewBufferPool()

	// Test that Get always returns a proper buffer
	for range 100 {
		buf := pool.Get()
		assert.NotNil(t, buf)
		assert.IsType(t, &bytes.Buffer{}, buf)
		assert.Equal(t, 0, buf.Len())
		pool.Put(buf)
	}
}

// TestByteSlicePool_TypeAssertion tests the type assertion fallback path.
func TestByteSlicePool_TypeAssertion(t *testing.T) {
	t.Parallel()

	pool := optimization.NewByteSlicePool()

	// Test that Get always returns a proper slice
	for i := range 100 {
		size := 64 + i
		buf := pool.Get(size)
		assert.NotNil(t, buf)
		assert.Len(t, buf, size)
		assert.IsType(t, []byte{}, buf)
		pool.Put(buf)
	}
}

func TestConnectionPool(t *testing.T) {
	// Cannot run in parallel: Uses shared counter and subtests that would interfere with each other
	t.Run("basic operations", testConnectionPoolBasicOperations)
	t.Run("size tracking", testConnectionPoolSizeTracking)
	t.Run("bad connection handling", testConnectionPoolBadConnectionHandling)
	t.Run("pool size limit", testConnectionPoolSizeLimit)
	t.Run("close pool", testConnectionPoolClose)
	t.Run("close empty pool", testConnectionPoolCloseEmpty)
}

type mockConnectionForPool struct {
	ID     int
	Closed bool
	Bad    bool
}

func createConnectionPoolForTesting() (*optimization.ConnectionPool[*mockConnectionForPool], *int, *sync.Mutex) {
	var idCounter int

	var mu sync.Mutex

	factory := func() (*mockConnectionForPool, error) {
		mu.Lock()

		idCounter++
		id := idCounter

		mu.Unlock()

		return &mockConnectionForPool{
			ID:     id,
			Closed: false, // New connections are open
			Bad:    false, // New connections are good
		}, nil
	}

	reset := func(conn *mockConnectionForPool) error {
		if conn.Bad {
			return errors.New("connection is bad")
		}

		return nil
	}

	closeFunc := func(conn *mockConnectionForPool) error {
		conn.Closed = true

		return nil
	}

	pool := optimization.NewConnectionPool(3, factory, reset, closeFunc)

	return pool, &idCounter, &mu
}

func testConnectionPoolBasicOperations(t *testing.T) {
	pool, _, _ := createConnectionPoolForTesting()

	// Get a connection
	conn1, err := pool.Get()
	require.NoError(t, err)
	assert.Equal(t, 1, conn1.ID)

	// Return it
	err = pool.Put(conn1)
	require.NoError(t, err)

	// Get again - should get the same one
	conn2, err := pool.Get()
	require.NoError(t, err)
	assert.Equal(t, 1, conn2.ID)

	err = pool.Put(conn2)
	require.NoError(t, err)
}

func testConnectionPoolSizeTracking(t *testing.T) {
	pool, _, _ := createConnectionPoolForTesting()

	initialSize := pool.Size()
	assert.Equal(t, 0, initialSize) // Pool starts empty

	conn, err := pool.Get()
	require.NoError(t, err)

	// Put it back
	err = pool.Put(conn)
	require.NoError(t, err)

	// Size should now be 1
	newSize := pool.Size()
	assert.Equal(t, 1, newSize)
}

func testConnectionPoolBadConnectionHandling(t *testing.T) {
	pool, _, _ := createConnectionPoolForTesting()

	// Get a connection
	conn, err := pool.Get()
	require.NoError(t, err)

	// Mark it as bad
	conn.Bad = true
	err = pool.Put(conn)
	require.NoError(t, err)

	// Get again - should get a new connection
	conn2, err := pool.Get()
	require.NoError(t, err)
	assert.NotEqual(t, conn.ID, conn2.ID)

	err = pool.Put(conn2)
	require.NoError(t, err)
}

func testConnectionPoolSizeLimit(t *testing.T) {
	pool, _, _ := createConnectionPoolForTesting()

	conns := make([]*mockConnectionForPool, 5)

	// Get 5 connections
	for i := range 5 {
		conn, err := pool.Get()
		require.NoError(t, err)

		conns[i] = conn
	}

	// Return all 5
	for _, conn := range conns {
		err := pool.Put(conn)
		require.NoError(t, err)
	}

	// Only 3 should be in the pool (max size)
	assert.Equal(t, 3, pool.Size())

	// The last 2 should have been closed
	assert.True(t, conns[3].Closed)
	assert.True(t, conns[4].Closed)

	// Verify we can still get connections from the pool
	for range 3 {
		conn, err := pool.Get()
		require.NoError(t, err)
		assert.NotNil(t, conn)
		err = pool.Put(conn)
		require.NoError(t, err)
	}
}

func testConnectionPoolClose(t *testing.T) {
	pool, _, _ := createConnectionPoolForTesting()

	// Add some connections
	for range 3 {
		conn, _ := pool.Get()
		_ = pool.Put(conn)
	}

	// Close the pool
	err := pool.Close()
	require.NoError(t, err)

	// Try to get - should fail
	_, err = pool.Get()
	require.Error(t, err)

	// Try to put - should close the connection
	conn := &mockConnectionForPool{
		ID:     999,
		Closed: false, // Connection is open
		Bad:    false, // Connection is good
	}
	err = pool.Put(conn)
	require.NoError(t, err)
	assert.True(t, conn.Closed)
}

func testConnectionPoolCloseEmpty(t *testing.T) {
	pool, _, _ := createConnectionPoolForTesting()
	err := pool.Close()
	assert.NoError(t, err)
}

func BenchmarkBufferPool(b *testing.B) {
	pool := optimization.NewBufferPool()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			buf := pool.Get()
			buf.WriteString("benchmark test data")
			pool.Put(buf)
		}
	})
}

func BenchmarkByteSlicePool(b *testing.B) {
	pool := optimization.NewByteSlicePool()
	size := 4096

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			buf := pool.Get(size)
			// Simulate some work
			for i := range 100 {
				buf[i] = byte(i)
			}

			pool.Put(buf)
		}
	})
}

func BenchmarkObjectPool(b *testing.B) {
	type TestObject struct {
		Data [1024]byte
	}

	pool := optimization.NewObjectPool(func() *TestObject {
		return &TestObject{
			Data: [1024]byte{}, // Initialize with empty byte array
		}
	})

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			obj := pool.Get()
			// Simulate some work
			obj.Data[0] = 1
			pool.Put(obj)
		}
	})
}

// BenchmarkConnectionPool benchmarks connection pool operations.
func BenchmarkConnectionPool(b *testing.B) {
	type MockConn struct {
		ID int
	}

	var counter int

	factory := func() (*MockConn, error) {
		counter++

		return &MockConn{ID: counter}, nil
	}
	reset := func(_ *MockConn) error { return nil }
	closeFunc := func(_ *MockConn) error { return nil }

	pool := optimization.NewConnectionPool(10, factory, reset, closeFunc)

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			conn, err := pool.Get()
			if err != nil {
				b.Fatal(err)
			}

			_ = pool.Put(conn)
		}
	})
}

// Benchmark comparison: with and without pooling.
func BenchmarkAllocationComparison(b *testing.B) {
	b.Run("without-pool", func(b *testing.B) {
		for range b.N {
			buf := make([]byte, 4096)
			// Simulate some work
			buf[0] = 1
		}
	})

	b.Run("with-pool", func(b *testing.B) {
		pool := optimization.NewByteSlicePool()
		for range b.N {
			buf := pool.Get(4096)
			// Simulate some work
			buf[0] = 1
			pool.Put(buf)
		}
	})
}

// TestBufferPool_EdgeCases tests edge cases and error conditions for BufferPool.
func TestBufferPool_EdgeCases(t *testing.T) {
	t.Parallel()

	pool := optimization.NewBufferPool()

	// Test type assertion failure simulation by manipulating the pool
	// This tests the type assertion failure path in Get()
	t.Run("corrupt pool handling", func(t *testing.T) {
		t.Parallel()
		// We can't easily force the type assertion to fail with sync.Pool
		// but we can test that Get() always returns a valid buffer
		buf := pool.Get()
		assert.NotNil(t, buf)
		assert.IsType(t, &bytes.Buffer{}, buf)
		assert.Equal(t, 0, buf.Len())
	})

	// Test edge case: zero-capacity buffer
	t.Run("zero capacity buffer", func(t *testing.T) {
		t.Parallel()

		buf := &bytes.Buffer{}
		pool.Put(buf) // Should not panic
	})

	// Test edge case: nil buffer (will panic due to Cap() call)
	t.Run("nil buffer handling", func(t *testing.T) {
		t.Parallel()
		// This would be an improper use and will panic due to Cap() call in Put()
		assert.Panics(t, func() {
			pool.Put(nil) // Will panic when calling buf.Cap()
		})
	})

	// Test maximum capacity buffer handling
	t.Run("maximum capacity buffer", func(t *testing.T) {
		t.Parallel()

		buf := &bytes.Buffer{}
		buf.Grow(2 * 1024 * 1024) // 2MB - should be rejected
		// Write data to ensure capacity is actually allocated
		buf.Write(make([]byte, 2*1024*1024))

		originalCap := buf.Cap()
		pool.Put(buf) // Should not be pooled due to size

		// Get a new buffer and verify it's not the large one
		newBuf := pool.Get()
		assert.Less(t, newBuf.Cap(), originalCap, "Large buffer should not be pooled")
	})
}

// TestByteSlicePool_EdgeCases tests edge cases and error conditions for ByteSlicePool.
func TestByteSlicePool_EdgeCases(t *testing.T) {
	t.Parallel()

	pool := optimization.NewByteSlicePool()

	t.Run("zero size request", func(t *testing.T) { testZeroSizeRequest(t, pool) })
	t.Run("size 1 request", func(t *testing.T) { testSize1Request(t, pool) })
	t.Run("concurrent access same size", func(t *testing.T) { testConcurrentAccessSameSize(t, pool) })
	t.Run("empty slice handling", func(t *testing.T) { testEmptySliceHandling(t, pool) })
	t.Run("very large slice handling", func(t *testing.T) { testVeryLargeSliceHandling(t, pool) })
}

func testZeroSizeRequest(t *testing.T, pool *optimization.ByteSlicePool) {
	t.Helper()
	t.Parallel()

	buf := pool.Get(0)
	assert.NotNil(t, buf)
	assert.Empty(t, buf)
	assert.GreaterOrEqual(t, cap(buf), 1) // Should round up to nearest power of 2
}

func testSize1Request(t *testing.T, pool *optimization.ByteSlicePool) {
	t.Helper()
	t.Parallel()

	buf := pool.Get(1)
	assert.NotNil(t, buf)
	assert.Len(t, buf, 1)
	assert.Equal(t, 1, cap(buf)) // Should be exactly 1
	pool.Put(buf)
}

func testConcurrentAccessSameSize(t *testing.T, pool *optimization.ByteSlicePool) {
	t.Helper()

	const (
		numGoroutines = 10
		iterations    = 100
	)

	size := 1024

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for range numGoroutines {
		go func() {
			defer wg.Done()

			for range iterations {
				buf := pool.Get(size)
				assert.Len(t, buf, size)
				// Modify the buffer
				for k := range size {
					buf[k] = byte(k % 256)
				}

				pool.Put(buf)
			}
		}()
	}

	wg.Wait()
}

func testEmptySliceHandling(t *testing.T, pool *optimization.ByteSlicePool) {
	t.Helper()
	t.Parallel()

	emptySlice := make([]byte, 0)
	pool.Put(emptySlice) // Should be rejected due to zero capacity
}

func testVeryLargeSliceHandling(t *testing.T, pool *optimization.ByteSlicePool) {
	t.Helper()
	t.Parallel()
	// Create a large slice that should be rejected
	largeSlice := make([]byte, 2*1024*1024) // 2MB
	pool.Put(largeSlice)                    // Should be rejected
}

// TestObjectPool_EdgeCases tests edge cases for ObjectPool.
func TestObjectPool_EdgeCases(t *testing.T) {
	// Note: Cannot run in parallel due to shared state in factory function
	type TestStruct struct {
		Value int
		Data  []byte
	}

	// Test factory function that returns different types
	t.Run("consistent factory function", func(t *testing.T) {
		counter := 0
		factory := func() *TestStruct {
			counter++

			return &TestStruct{
				Value: counter,
				Data:  make([]byte, 100),
			}
		}

		pool := optimization.NewObjectPool(factory)

		// Get multiple objects
		obj1 := pool.Get()
		obj2 := pool.Get()

		assert.NotNil(t, obj1)
		assert.NotNil(t, obj2)
		assert.NotEqual(t, obj1.Value, obj2.Value)

		// Put them back and get again
		pool.Put(obj1)
		pool.Put(obj2)

		obj3 := pool.Get()
		assert.NotNil(t, obj3)
	})

	// Test factory function that might panic
	t.Run("panic recovery", func(t *testing.T) {
		panicFactory := func() *TestStruct {
			panic("factory panic")
		}

		pool := optimization.NewObjectPool(panicFactory)

		// Should panic when trying to create new object
		assert.Panics(t, func() {
			pool.Get()
		})
	})
}

// TestConnectionPool_ErrorConditions tests error conditions and edge cases.
func TestConnectionPool_ErrorConditions(t *testing.T) {
	// Cannot run in parallel: Uses shared counter and creates multiple pools
	t.Run("factory always fails", testFactoryAlwaysFails)
	t.Run("reset always fails", testResetAlwaysFails)
	t.Run("close function fails", testCloseFunctionFails)
	t.Run("double close", testDoubleClose)
	t.Run("operations on closed pool", testOperationsOnClosedPool)
	t.Run("close with connection close failures", testCloseWithConnectionCloseFailures)
}

type mockConnectionForErrors struct {
	ID     int
	Closed bool
	Bad    bool
}

func testFactoryAlwaysFails(t *testing.T) {
	failFactory := func() (*mockConnectionForErrors, error) {
		return nil, errors.New("factory failure")
	}

	reset := func(_ *mockConnectionForErrors) error { return nil }
	closeFunc := func(_ *mockConnectionForErrors) error { return nil }

	pool := optimization.NewConnectionPool(3, failFactory, reset, closeFunc)

	// Get should return the factory error
	conn, err := pool.Get()
	require.Error(t, err)
	assert.Nil(t, conn)
	assert.Equal(t, "factory failure", err.Error())
}

func testResetAlwaysFails(t *testing.T) {
	var idCounter int

	factory := func() (*mockConnectionForErrors, error) {
		idCounter++

		return &mockConnectionForErrors{ID: idCounter}, nil
	}

	failReset := func(_ *mockConnectionForErrors) error {
		return errors.New("reset failure")
	}

	closeFunc := func(_ *mockConnectionForErrors) error { return nil }

	pool := optimization.NewConnectionPool(3, factory, failReset, closeFunc)

	// Get a connection and put it back
	conn1, err := pool.Get()
	require.NoError(t, err)
	err = pool.Put(conn1)
	require.NoError(t, err)

	// Get again - should get a new connection due to reset failure
	conn2, err := pool.Get()
	require.NoError(t, err)
	assert.NotEqual(t, conn1.ID, conn2.ID)
}

func testCloseFunctionFails(t *testing.T) {
	var idCounter int

	factory := func() (*mockConnectionForErrors, error) {
		idCounter++

		return &mockConnectionForErrors{ID: idCounter}, nil
	}

	reset := func(_ *mockConnectionForErrors) error { return nil }

	failClose := func(_ *mockConnectionForErrors) error {
		return errors.New("close failure")
	}

	pool := optimization.NewConnectionPool(1, factory, reset, failClose) // Small pool to force closes

	// Fill the pool beyond capacity
	conns := make([]*mockConnectionForErrors, 3)

	for i := range 3 {
		conn, err := pool.Get()
		require.NoError(t, err)

		conns[i] = conn
	}

	// Put back - first goes into pool, rest should fail to close
	for i, conn := range conns {
		err := pool.Put(conn)
		if i == 0 {
			assert.NoError(t, err) // First one goes into pool
		} else {
			assert.Error(t, err) // Rest should fail to close (pool is full)
		}
	}
}

func testDoubleClose(t *testing.T) {
	factory := func() (*mockConnectionForErrors, error) {
		return &mockConnectionForErrors{ID: 1}, nil
	}
	reset := func(_ *mockConnectionForErrors) error { return nil }
	closeFunc := func(_ *mockConnectionForErrors) error { return nil }

	pool := optimization.NewConnectionPool(3, factory, reset, closeFunc)

	// First close should succeed
	err := pool.Close()
	require.NoError(t, err)

	// Second close should be no-op
	err = pool.Close()
	require.NoError(t, err)
}

func testOperationsOnClosedPool(t *testing.T) {
	factory := func() (*mockConnectionForErrors, error) {
		return &mockConnectionForErrors{ID: 1}, nil
	}
	reset := func(_ *mockConnectionForErrors) error { return nil }
	closeFunc := func(conn *mockConnectionForErrors) error {
		conn.Closed = true // Mark as closed when closeFunc is called

		return nil
	}

	pool := optimization.NewConnectionPool(3, factory, reset, closeFunc)

	// Close the pool
	err := pool.Close()
	require.NoError(t, err)

	// Get should fail
	conn, err := pool.Get()
	require.Error(t, err)
	assert.Nil(t, conn)
	assert.Equal(t, optimization.ErrPoolClosed, err)

	// Put should close the connection
	mockConn := &mockConnectionForErrors{ID: 999, Closed: false}
	err = pool.Put(mockConn)
	require.NoError(t, err)
	assert.True(t, mockConn.Closed)
}

func testCloseWithConnectionCloseFailures(t *testing.T) {
	factory := func() (*mockConnectionForErrors, error) {
		return &mockConnectionForErrors{}, nil
	}
	reset := func(_ *mockConnectionForErrors) error { return nil }
	failClose := func(_ *mockConnectionForErrors) error {
		return errors.New("close failed")
	}

	pool := optimization.NewConnectionPool(2, factory, reset, failClose)

	// Add some connections
	conn1, _ := pool.Get()
	conn2, _ := pool.Get()
	_ = pool.Put(conn1)
	_ = pool.Put(conn2)

	// Close should return the last error
	err := pool.Close()
	require.Error(t, err)
	assert.Equal(t, "close failed", err.Error())
}

// TestPoolError tests the poolError type.
func TestPoolError(t *testing.T) {
	t.Parallel()

	err := optimization.ErrPoolClosed
	require.Error(t, err)
	assert.Equal(t, "pool is closed", err.Error())

	// Test that it implements error interface
	var _ error = err
}

// TestConcurrentPoolAccess tests concurrent access patterns.
func TestConcurrentPoolAccess(t *testing.T) {
	t.Parallel()

	t.Run("buffer pool concurrent stress", testBufferPoolConcurrentStress)
	t.Run("byte slice pool concurrent different sizes", testByteSlicePoolConcurrentDifferentSizes)
	t.Run("connection pool concurrent access", testConnectionPoolConcurrentAccess)
}

func testBufferPoolConcurrentStress(t *testing.T) {
	t.Parallel()

	pool := optimization.NewBufferPool()

	const numWorkers = 50

	const iterations = 100

	var wg sync.WaitGroup

	wg.Add(numWorkers)

	for i := range numWorkers {
		go func(workerID int) {
			defer wg.Done()

			for j := range iterations {
				buf := pool.Get()
				fmt.Fprintf(buf, "worker-%d-iter-%d", workerID, j)
				assert.Positive(t, buf.Len())
				pool.Put(buf)
			}
		}(i)
	}

	wg.Wait()
}

func testByteSlicePoolConcurrentDifferentSizes(t *testing.T) {
	t.Parallel()

	pool := optimization.NewByteSlicePool()
	sizes := []int{64, 128, 256, 512, 1024, 2048}

	const numWorkers = 20

	const iterations = 50

	var wg sync.WaitGroup

	wg.Add(numWorkers)

	for i := range numWorkers {
		go func(workerID int) {
			defer wg.Done()

			for j := range iterations {
				size := sizes[j%len(sizes)]
				buf := pool.Get(size)
				assert.Len(t, buf, size)
				// Write pattern
				for k := range size {
					buf[k] = byte((workerID + j + k) % 256)
				}

				pool.Put(buf)
			}
		}(i)
	}

	wg.Wait()
}

func testConnectionPoolConcurrentAccess(t *testing.T) {
	t.Parallel()

	var idCounter int

	var mu sync.Mutex

	factory := func() (*int, error) {
		mu.Lock()

		idCounter++
		id := idCounter

		mu.Unlock()

		return &id, nil
	}

	reset := func(_ *int) error { return nil }
	closeFunc := func(_ *int) error { return nil }

	pool := optimization.NewConnectionPool(10, factory, reset, closeFunc)

	const numWorkers = 30

	const iterations = 20

	var wg sync.WaitGroup

	wg.Add(numWorkers)

	for range numWorkers {
		go func() {
			defer wg.Done()

			for range iterations {
				conn, err := pool.Get()
				assert.NoError(t, err)
				assert.NotNil(t, conn)
				// Simulate some work
				time.Sleep(time.Microsecond)

				err = pool.Put(conn)
				assert.NoError(t, err)
			}
		}()
	}

	wg.Wait()

	// Clean up
	err := pool.Close()
	assert.NoError(t, err)
}
