package pool

import (
	"context"
	"errors"
	"sync/atomic"
	"time"
)

// ConnectionAcquirer handles connection acquisition logic.
type ConnectionAcquirer struct {
	pool *Pool
}

// CreateConnectionAcquirer creates a new connection acquirer.
func CreateConnectionAcquirer(pool *Pool) *ConnectionAcquirer {
	return &ConnectionAcquirer{
		pool: pool,
	}
}

// AcquireConnection acquires a connection from the pool.
//
//nolint:ireturn // Pool/manager pattern requires interface return
func (a *ConnectionAcquirer) AcquireConnection(ctx context.Context) (Connection, error) {
	if err := a.validatePool(); err != nil {
		return nil, err
	}

	a.recordWaitStart()
	defer a.recordWaitDuration(time.Now())

	// Try to get an existing connection first.
	if conn := a.tryGetIdleConnection(); conn != nil {
		return conn, nil
	}

	// Try to create a new connection if possible.
	if conn, err := a.tryCreateNewConnection(ctx); err != nil {
		// If creation failed and it's not due to pool exhaustion, return the error
		if !errors.Is(err, ErrPoolExhausted) {
			return nil, err
		}
	} else if conn != nil {
		return conn, nil
	}

	// Wait for an available connection.
	return a.waitForConnection(ctx)
}

// validatePool checks if the pool is closed.
func (a *ConnectionAcquirer) validatePool() error {
	if atomic.LoadInt32(&a.pool.closed) == 1 {
		return ErrPoolClosed
	}

	return nil
}

// recordWaitStart increments the wait counter.
func (a *ConnectionAcquirer) recordWaitStart() {
	atomic.AddInt64(&a.pool.stats.WaitCount, 1)
}

// recordWaitDuration records how long the wait took.
func (a *ConnectionAcquirer) recordWaitDuration(start time.Time) {
	duration := time.Since(start).Nanoseconds()
	atomic.AddInt64((*int64)(&a.pool.stats.WaitDuration), duration)
}

// tryGetIdleConnection attempts to get an idle connection.
//
//nolint:ireturn // Pool/manager pattern requires interface return
func (a *ConnectionAcquirer) tryGetIdleConnection() Connection {
	select {
	case conn := <-a.pool.idle:
		if a.pool.isValidConnection(conn) {
			a.activateConnection(conn)

			return conn
		}
		// Invalid connection, remove it.
		a.pool.removeConnection(conn)
	default:
		// No idle connections available.
	}

	return nil
}

// activateConnection marks a connection as active.
func (a *ConnectionAcquirer) activateConnection(conn *pooledConn) {
	conn.lastUsedAt = time.Now()
	atomic.AddInt64(&conn.usageCount, 1)
	atomic.AddInt64(&a.pool.stats.ActiveConnections, 1)
	atomic.AddInt64(&a.pool.stats.IdleConnections, -1)
}

// tryCreateNewConnection attempts to create a new connection if within limits.
//
//nolint:ireturn // Pool/manager pattern requires interface return
func (a *ConnectionAcquirer) tryCreateNewConnection(ctx context.Context) (Connection, error) {
	if !a.canCreateNewConnection() {
		return nil, ErrPoolExhausted
	}

	conn, err := a.pool.createConnection(ctx)
	if err != nil {
		return nil, err
	}

	atomic.AddInt64(&a.pool.stats.ActiveConnections, 1)

	return conn, nil
}

// canCreateNewConnection checks if we can create a new connection.
func (a *ConnectionAcquirer) canCreateNewConnection() bool {
	a.pool.mu.RLock()
	totalConns := len(a.pool.connections)
	a.pool.mu.RUnlock()

	return totalConns < a.pool.config.MaxSize
}

// waitForConnection waits for an available connection.
//
//nolint:ireturn // Pool/manager pattern requires interface return
func (a *ConnectionAcquirer) waitForConnection(ctx context.Context) (Connection, error) {
	waiter := &ConnectionWaiter{
		pool:    a.pool,
		channel: make(chan *pooledConn, 1),
		ctx:     ctx,
	}

	if err := waiter.QueueRequest(); err != nil {
		return nil, err
	}

	return waiter.WaitForConnection()
}

// ConnectionWaiter handles waiting for connections.
type ConnectionWaiter struct {
	pool    *Pool
	channel chan *pooledConn
	ctx     context.Context
}

// QueueRequest queues the waiter request.
func (w *ConnectionWaiter) QueueRequest() error {
	select {
	case w.pool.waiters <- w.channel:
		// Successfully queued.
		return nil
	case <-w.ctx.Done():
		return w.ctx.Err()
	case <-time.After(w.pool.config.AcquireTimeout):
		return ErrConnTimeout
	}
}

// WaitForConnection waits for a connection to become available.
//
//nolint:ireturn // Pool/manager pattern requires interface return
func (w *ConnectionWaiter) WaitForConnection() (Connection, error) {
	select {
	case conn := <-w.channel:
		return w.handleReceivedConnection(conn)
	case <-w.ctx.Done():
		return nil, w.ctx.Err()
	case <-time.After(w.pool.config.AcquireTimeout):
		return nil, ErrConnTimeout
	}
}

// handleReceivedConnection processes a received connection.
//
//nolint:ireturn // Pool/manager pattern requires interface return
func (w *ConnectionWaiter) handleReceivedConnection(conn *pooledConn) (Connection, error) {
	if conn == nil {
		return nil, ErrPoolClosed
	}

	conn.lastUsedAt = time.Now()
	atomic.AddInt64(&conn.usageCount, 1)
	atomic.AddInt64(&w.pool.stats.ActiveConnections, 1)

	return conn, nil
}
