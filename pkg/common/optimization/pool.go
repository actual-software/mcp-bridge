// Package optimization provides performance optimization utilities
package optimization

import (
	"bytes"
	"sync"
)

// BufferPool provides a pool of reusable byte buffers.
type BufferPool struct {
	pool sync.Pool
}

// NewBufferPool creates a new buffer pool.
func NewBufferPool() *BufferPool {
	return &BufferPool{
		pool: sync.Pool{
			New: func() interface{} {
				return new(bytes.Buffer)
			},
		},
	}
}

// Get retrieves a buffer from the pool.
func (p *BufferPool) Get() *bytes.Buffer {
	buf, ok := p.pool.Get().(*bytes.Buffer)
	if !ok {
		// This should never happen if the pool is used correctly
		return &bytes.Buffer{}
	}

	buf.Reset()

	return buf
}

// Put returns a buffer to the pool.
func (p *BufferPool) Put(buf *bytes.Buffer) {
	if buf.Cap() > 1024*1024 { // Don't pool buffers larger than 1MB
		return
	}

	buf.Reset()
	p.pool.Put(buf)
}

// ByteSlicePool provides a pool of reusable byte slices.
type ByteSlicePool struct {
	pools map[int]*sync.Pool
	mu    sync.RWMutex
}

// NewByteSlicePool creates a new byte slice pool.
func NewByteSlicePool() *ByteSlicePool {
	return &ByteSlicePool{
		pools: make(map[int]*sync.Pool),
		mu:    sync.RWMutex{}, // Initialize mutex explicitly
	}
}

// Get retrieves a byte slice of the specified size.
func (p *ByteSlicePool) Get(size int) []byte {
	// Round up to nearest power of 2
	poolSize := 1
	for poolSize < size {
		poolSize *= 2
	}

	p.mu.RLock()
	pool, exists := p.pools[poolSize]
	p.mu.RUnlock()

	if !exists {
		p.mu.Lock()

		pool, exists = p.pools[poolSize]
		if !exists {
			pool = &sync.Pool{
				New: func() interface{} {
					buf := make([]byte, poolSize)

					return &buf
				},
			}
			p.pools[poolSize] = pool
		}

		p.mu.Unlock()
	}

	bufPtr, ok := pool.Get().(*[]byte)
	if !ok || bufPtr == nil {
		// This should never happen if the pool is used correctly
		return make([]byte, size)
	}

	return (*bufPtr)[:size]
}

// Put returns a byte slice to the pool.
func (p *ByteSlicePool) Put(buf []byte) {
	size := cap(buf)
	if size == 0 || size > 1024*1024 { // Don't pool empty or very large slices
		return
	}

	// Find the appropriate pool
	poolSize := 1
	for poolSize < size {
		poolSize *= 2
	}

	p.mu.RLock()
	pool, exists := p.pools[poolSize]
	p.mu.RUnlock()

	if exists {
		// Clear the slice before returning to pool
		for i := range buf {
			buf[i] = 0
		}

		// Resize to pool size if needed
		if cap(buf) > poolSize {
			buf = buf[:poolSize]
		}

		pool.Put(&buf)
	}
}

// ObjectPool provides a generic object pool.
type ObjectPool[T any] struct {
	pool sync.Pool
	new  func() T
}

// NewObjectPool creates a new object pool.
func NewObjectPool[T any](newFunc func() T) *ObjectPool[T] {
	return &ObjectPool[T]{
		pool: sync.Pool{
			New: func() interface{} {
				return newFunc()
			},
		},
		new: newFunc,
	}
}

// Get retrieves an object from the pool.
//
//nolint:ireturn // Generic type parameter T is not an interface, it's a type constraint
func (p *ObjectPool[T]) Get() T {
	obj, ok := p.pool.Get().(T)
	if !ok {
		// This should never happen if the pool is used correctly
		return p.new()
	}

	return obj
}

// Put returns an object to the pool.
func (p *ObjectPool[T]) Put(obj T) {
	p.pool.Put(obj)
}

// ConnectionPool manages a pool of reusable connections.
type ConnectionPool[T any] struct {
	pool      chan T
	factory   func() (T, error)
	reset     func(T) error
	closeFunc func(T) error
	maxSize   int
	mu        sync.Mutex
	closed    bool
}

// NewConnectionPool creates a new connection pool.
func NewConnectionPool[T any](
	maxSize int,
	factory func() (T, error),
	reset func(T) error,
	closeFunc func(T) error,
) *ConnectionPool[T] {
	return &ConnectionPool[T]{
		pool:      make(chan T, maxSize),
		factory:   factory,
		reset:     reset,
		closeFunc: closeFunc,
		maxSize:   maxSize,
		mu:        sync.Mutex{}, // Initialize mutex explicitly
		closed:    false,        // Initialize closed state explicitly
	}
}

// Get retrieves a connection from the pool.
//
//nolint:ireturn // Generic type parameter T is not an interface, it's a type constraint
func (p *ConnectionPool[T]) Get() (T, error) {
	p.mu.Lock()

	if p.closed {
		p.mu.Unlock()

		var zero T

		return zero, ErrPoolClosed
	}

	p.mu.Unlock()

	select {
	case conn := <-p.pool:
		// Reset the connection before returning
		if err := p.reset(conn); err != nil {
			// Connection is bad, create a new one
			return p.factory()
		}

		return conn, nil
	default:
		// Pool is empty, create a new connection
		return p.factory()
	}
}

// Put returns a connection to the pool.
func (p *ConnectionPool[T]) Put(conn T) error {
	p.mu.Lock()

	if p.closed {
		p.mu.Unlock()

		return p.closeFunc(conn)
	}

	p.mu.Unlock()

	select {
	case p.pool <- conn:
		return nil
	default:
		// Pool is full, close the connection
		return p.closeFunc(conn)
	}
}

// Close closes all connections in the pool.
func (p *ConnectionPool[T]) Close() error {
	p.mu.Lock()

	if p.closed {
		p.mu.Unlock()

		return nil
	}

	p.closed = true

	p.mu.Unlock()

	close(p.pool)

	var lastErr error

	for conn := range p.pool {
		if err := p.closeFunc(conn); err != nil {
			lastErr = err
		}
	}

	return lastErr
}

// Size returns the current number of connections in the pool.
func (p *ConnectionPool[T]) Size() int {
	return len(p.pool)
}

// ErrPoolClosed is returned when operating on a closed pool.
var ErrPoolClosed = &poolError{"pool is closed"}

type poolError struct {
	msg string
}

func (e *poolError) Error() string {
	return e.msg
}
