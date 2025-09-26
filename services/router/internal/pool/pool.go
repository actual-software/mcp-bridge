// Package pool provides connection pooling implementations for MCP gateway clients.
//
// This package implements efficient connection pooling patterns for both TCP and.
// WebSocket connections to MCP gateway servers. The pool provides:
//
//   - Connection lifecycle management with health checking
//   - Load distribution across multiple gateway endpoints
//   - Automatic connection recovery and cleanup
//   - Metrics collection for pool performance monitoring
//   - Configurable timeouts and limits for production use
//
// The implementation uses generic types to avoid code duplication between.
// TCP and WebSocket connection pools while maintaining type safety.
//
// # Architecture
//
// The pool follows a factory pattern where different connection types.
// are created through the Factory interface. This allows the same pool
// logic to handle different connection protocols:
//
//   - WebSocket connections via gateway.Client
//   - TCP connections via gateway.TCPClient
//   - Future connection types through interface implementation
//
// # Connection Lifecycle
//
// Connections progress through these states:
//
//  1. Creation: Factory.Create() produces new connections
//  2. Validation: Factory.Validate() verifies connection health
//  3. Active Use: Connections serve requests
//  4. Idle State: Unused connections wait for reuse
//  5. Cleanup: Expired or invalid connections are removed
//
// # Pool Management
//
// The pool maintains connections within configured limits:
//
//   - MinSize: Minimum connections to keep available
//   - MaxSize: Maximum total connections allowed
//   - MaxIdleTime: How long unused connections remain available
//   - MaxLifetime: Maximum age regardless of usage
//   - AcquireTimeout: How long to wait for available connections
//
// Background goroutines handle maintenance and health checking:
//
//   - maintainer(): Ensures minimum pool size is maintained
//   - healthChecker(): Validates idle connections periodically
//
// # Usage Example
//
// Basic pool creation and usage:
//
//	config := pool.DefaultConfig()
//	config.MaxSize = 20
//	config.AcquireTimeout = defaultRetryCount * time.Second
//
//	factory := pool.NewGenericFactory(gwConfig, logger, createClientFunc, "WebSocket")
//	p, err := pool.NewPool(config, factory, logger)
//	if err != nil {
//	    return err
//	}
//	defer func() { _ = p.Close() }()
//
//	// Acquire connection for use
//	conn, err := p.Acquire(ctx)
//	if err != nil {
//	    return err
//	}
//	defer p.Release(conn)
//
//	// Use connection for MCP operations
//	client := conn.GetClient().(gateway.GatewayClient)
//	response, err := client.SendRequest(request)
//
// # Monitoring and Observability
//
// The pool provides comprehensive statistics through Stats():
//
//   - TotalConnections: Current connections in pool
//   - ActiveConnections: Connections currently in use
//   - IdleConnections: Available connections waiting for use
//   - WaitCount: Total number of acquisition waits
//   - WaitDuration: Cumulative time spent waiting
//   - CreatedCount: Total connections created over lifetime
//   - ClosedCount: Total connections closed over lifetime
//   - FailedCount: Total failed connection creation attempts
//
// # Thread Safety
//
// All pool operations are thread-safe and designed for concurrent use.
// The implementation uses:
//
//   - sync.RWMutex for protecting shared state
//   - atomic operations for statistics counters
//   - Channels for connection distribution and waiting
//
// # Error Handling
//
// The pool defines specific error types for different failure conditions:
//
//   - ErrPoolClosed: Operations on closed pool
//   - ErrPoolExhausted: No connections available within timeout
//   - ErrInvalidConn: Invalid connection passed to Release()
//   - ErrConnTimeout: Timeout waiting for connection
//
// These errors allow callers to implement appropriate retry and fallback logic.
package pool

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"github.com/poiley/mcp-bridge/services/router/internal/constants"
)

// Constants removed - will add as needed

var (
	ErrPoolClosed    = errors.New("connection pool is closed")
	ErrPoolExhausted = errors.New("connection pool exhausted")
	ErrInvalidConn   = errors.New("invalid connection")
	ErrConnTimeout   = errors.New("connection timeout")
)

// Connection represents a pooled connection.
type Connection interface {
	// IsAlive checks if the connection is still alive.
	IsAlive() bool
	// Close closes the connection.
	Close() error
	// GetID returns the connection ID.
	GetID() string
}

// Factory creates new connections.
type Factory interface {
	// Create creates a new connection.
	Create(ctx context.Context) (Connection, error)
	// Validate validates an existing connection.
	Validate(conn Connection) error
}

// Stats represents pool statistics.
type Stats struct {
	TotalConnections  int64
	ActiveConnections int64
	IdleConnections   int64
	WaitCount         int64
	WaitDuration      time.Duration
	CreatedCount      int64
	ClosedCount       int64
	FailedCount       int64
}

// Config represents pool configuration.
type Config struct {
	// MinSize is the minimum number of connections to maintain.
	MinSize int
	// MaxSize is the maximum number of connections allowed.
	MaxSize int
	// MaxIdleTime is the maximum time a connection can be idle.
	MaxIdleTime time.Duration
	// MaxLifetime is the maximum lifetime of a connection.
	MaxLifetime time.Duration
	// AcquireTimeout is the maximum time to wait for a connection.
	AcquireTimeout time.Duration
	// HealthCheckInterval is the interval between health checks.
	HealthCheckInterval time.Duration
}

// DefaultConfig returns default pool configuration.
func DefaultConfig() Config {
	return Config{
		MinSize:             DefaultMinPoolSize,
		MaxSize:             DefaultMaxPoolSize,
		MaxIdleTime:         constants.DefaultMaxIdleTime,
		MaxLifetime:         constants.DefaultMaxLifetime,
		AcquireTimeout:      constants.DefaultAcquireTimeout,
		HealthCheckInterval: constants.DefaultHealthCheckInterval,
	}
}

// Pool represents a connection pool.
type Pool struct {
	config  Config
	factory Factory
	logger  *zap.Logger

	mu          sync.RWMutex
	connections map[string]*pooledConn
	idle        chan *pooledConn
	waiters     chan chan *pooledConn

	stats Stats

	closed    int32
	closeOnce sync.Once
	closeCh   chan struct{}
	wg        sync.WaitGroup // Wait group for background goroutines
}

// pooledConn wraps a connection with metadata.
type pooledConn struct {
	Connection
	id         string
	pool       *Pool
	createdAt  time.Time
	lastUsedAt time.Time
	usageCount int64
	mu         sync.Mutex
}

// NewPool creates a new connection pool.
func NewPool(config Config, factory Factory, logger *zap.Logger) (*Pool, error) {
	config = validateAndNormalizePoolConfig(config)

	p := createPoolStruct(config, factory, logger)

	// Start background workers
	startBackgroundWorkers(p)

	// Initialize minimum connections
	err := initializeMinimumConnections(p, config, logger)
	if err != nil {
		return p, err
	}

	logPoolCreation(logger, config)

	return p, nil
}

func validateAndNormalizePoolConfig(config Config) Config {
	if config.MinSize < 0 {
		config.MinSize = 0
	}

	if config.MaxSize <= 0 {
		config.MaxSize = 10
	}

	if config.MinSize > config.MaxSize {
		config.MinSize = config.MaxSize
	}

	if config.MaxIdleTime <= 0 {
		config.MaxIdleTime = constants.DefaultMaxIdleTime
	}

	if config.MaxLifetime <= 0 {
		config.MaxLifetime = constants.DefaultMaxLifetime
	}

	if config.AcquireTimeout <= 0 {
		config.AcquireTimeout = constants.DefaultAcquireTimeout
	}

	if config.HealthCheckInterval <= 0 {
		config.HealthCheckInterval = constants.DefaultHealthCheckInterval
	}

	return config
}

func createPoolStruct(config Config, factory Factory, logger *zap.Logger) *Pool {
	return &Pool{
		config:      config,
		factory:     factory,
		logger:      logger,
		connections: make(map[string]*pooledConn),
		idle:        make(chan *pooledConn, config.MaxSize),
		waiters:     make(chan chan *pooledConn, config.MaxSize),
		closeCh:     make(chan struct{}),
	}
}

func startBackgroundWorkers(p *Pool) {
	p.wg.Add(BackgroundWorkerCount)

	go p.maintainer()
	go p.healthChecker()
}

func initializeMinimumConnections(p *Pool, config Config, logger *zap.Logger) error {
	ctx, cancel := context.WithTimeout(context.Background(), constants.PoolCleanupTimeout)
	defer cancel()

	for i := 0; i < config.MinSize; i++ {
		conn, err := p.createConnection(ctx)
		if err != nil {
			logger.Warn("Failed to create initial connection",
				zap.Int("index", i),
				zap.Error(err),
			)

			continue
		}

		p.idle <- conn

		atomic.AddInt64(&p.stats.IdleConnections, 1)
	}

	return nil
}

func logPoolCreation(logger *zap.Logger, config Config) {
	logger.Info("Connection pool created",
		zap.Int("min_size", config.MinSize),
		zap.Int("max_size", config.MaxSize),
		zap.Duration("max_idle_time", config.MaxIdleTime),
		zap.Duration("max_lifetime", config.MaxLifetime),
	)
}

// Acquire gets a connection from the pool.
func (p *Pool) Acquire(ctx context.Context) (Connection, error) {
	acquirer := CreateConnectionAcquirer(p)

	return acquirer.AcquireConnection(ctx)
}

// Release returns a connection to the pool.
func (p *Pool) Release(conn Connection) error {
	if atomic.LoadInt32(&p.closed) == 1 {
		return conn.Close()
	}

	pc, ok := conn.(*pooledConn)
	if !ok || pc.pool != p {
		return ErrInvalidConn
	}

	atomic.AddInt64(&p.stats.ActiveConnections, -1)

	// Check if connection is still valid.
	if !p.isValidConnection(pc) {
		p.removeConnection(pc)

		return nil
	}

	// Try to return to a waiter.
	select {
	case waiter := <-p.waiters:
		waiter <- pc

		return nil
	default:
		// No waiters.
	}

	// Return to idle pool.
	select {
	case p.idle <- pc:
		atomic.AddInt64(&p.stats.IdleConnections, 1)

		return nil
	default:
		// Pool is full, close the connection.
		p.removeConnection(pc)

		return nil
	}
}

// Close closes the pool and all connections.
func (p *Pool) Close() error {
	if !atomic.CompareAndSwapInt32(&p.closed, 0, 1) {
		return nil
	}

	var err error

	p.closeOnce.Do(func() {
		close(p.closeCh)

		// Wait for background goroutines to finish.
		p.wg.Wait()

		// Close waiters.
		close(p.waiters)

		for waiter := range p.waiters {
			close(waiter)
		}

		// Close idle connections.
		close(p.idle)

		for conn := range p.idle {
			if e := conn.Close(); e != nil && err == nil {
				err = e
			}
		}

		// Close all connections.
		p.mu.Lock()

		for _, conn := range p.connections {
			if e := conn.Close(); e != nil && err == nil {
				err = e
			}
		}

		p.connections = nil
		p.mu.Unlock()

		p.logger.Info("Connection pool closed",
			zap.Int64("total_created", atomic.LoadInt64(&p.stats.CreatedCount)),
			zap.Int64("total_closed", atomic.LoadInt64(&p.stats.ClosedCount)),
		)
	})

	return err
}

// Stats returns pool statistics.
func (p *Pool) Stats() Stats {
	p.mu.RLock()
	totalConnections := int64(len(p.connections))
	p.mu.RUnlock()

	stats := Stats{
		TotalConnections:  totalConnections,
		ActiveConnections: atomic.LoadInt64(&p.stats.ActiveConnections),
		IdleConnections:   atomic.LoadInt64(&p.stats.IdleConnections),
		WaitCount:         atomic.LoadInt64(&p.stats.WaitCount),
		CreatedCount:      atomic.LoadInt64(&p.stats.CreatedCount),
		ClosedCount:       atomic.LoadInt64(&p.stats.ClosedCount),
		FailedCount:       atomic.LoadInt64(&p.stats.FailedCount),
	}

	return stats
}

// createConnection creates a new connection.
func (p *Pool) createConnection(ctx context.Context) (*pooledConn, error) {
	conn, err := p.factory.Create(ctx)
	if err != nil {
		atomic.AddInt64(&p.stats.FailedCount, 1)

		return nil, err
	}

	pc := &pooledConn{
		Connection: conn,
		id:         conn.GetID(),
		pool:       p,
		createdAt:  time.Now(),
		lastUsedAt: time.Now(),
	}

	p.mu.Lock()
	p.connections[pc.id] = pc
	totalCount := len(p.connections)
	p.mu.Unlock()

	atomic.AddInt64(&p.stats.CreatedCount, 1)
	atomic.AddInt64(&p.stats.TotalConnections, 1)

	p.logger.Debug("Created new connection",
		zap.String("conn_id", pc.id),
		zap.Int("total", totalCount),
	)

	return pc, nil
}

// removeConnection removes a connection from the pool.
func (p *Pool) removeConnection(pc *pooledConn) {
	p.mu.Lock()
	delete(p.connections, pc.id)
	remainingCount := len(p.connections)
	p.mu.Unlock()

	atomic.AddInt64(&p.stats.TotalConnections, -1)
	atomic.AddInt64(&p.stats.ClosedCount, 1)

	if err := pc.Close(); err != nil {
		p.logger.Warn("Failed to close connection",
			zap.String("conn_id", pc.id),
			zap.Error(err),
		)
	}

	p.logger.Debug("Removed connection",
		zap.String("conn_id", pc.id),
		zap.Int("remaining", remainingCount),
	)
}

// isValidConnection checks if a connection is still valid.
func (p *Pool) isValidConnection(pc *pooledConn) bool {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	// Check if connection is alive.
	if !pc.IsAlive() {
		return false
	}

	// Check lifetime.
	if time.Since(pc.createdAt) > p.config.MaxLifetime {
		return false
	}

	// Check idle time.
	if time.Since(pc.lastUsedAt) > p.config.MaxIdleTime {
		return false
	}

	// Validate with factory.
	if err := p.factory.Validate(pc.Connection); err != nil {
		return false
	}

	return true
}

// maintainer maintains the minimum pool size.
func (p *Pool) maintainer() {
	defer p.wg.Done()

	ticker := time.NewTicker(constants.PoolStatisticsInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			p.maintain()
		case <-p.closeCh:
			return
		}
	}
}

// maintain ensures minimum connections.
func (p *Pool) maintain() {
	p.mu.RLock()
	currentSize := len(p.connections)
	p.mu.RUnlock()

	if currentSize >= p.config.MinSize {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), constants.PoolCleanupTimeout)
	defer cancel()

	needed := p.config.MinSize - currentSize
	for i := 0; i < needed; i++ {
		conn, err := p.createConnection(ctx)
		if err != nil {
			p.logger.Warn("Failed to create connection during maintenance",
				zap.Error(err),
			)

			continue
		}

		select {
		case p.idle <- conn:
			atomic.AddInt64(&p.stats.IdleConnections, 1)
		default:
			// Pool is full.
			p.removeConnection(conn)
		}
	}
}

// healthChecker periodically checks connection health.
func (p *Pool) healthChecker() {
	defer p.wg.Done()

	ticker := time.NewTicker(p.config.HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			p.checkHealth()
		case <-p.closeCh:
			return
		}
	}
}

// checkHealth checks the health of idle connections.
func (p *Pool) checkHealth() {
	// Check if pool is closing.
	select {
	case <-p.closeCh:
		return
	default:
	}

	conns, removedFromIdle := p.collectIdleConnections()
	validConns, removedConns := p.processHealthCheckedConnections(conns, removedFromIdle)
	p.finalizeHealthCheck(validConns, removedConns, len(conns))
}

// collectIdleConnections gathers all idle connections from the pool.
func (p *Pool) collectIdleConnections() ([]*pooledConn, int64) {
	var conns []*pooledConn

	removedFromIdle := int64(0)

	for {
		select {
		case conn := <-p.idle:
			conns = append(conns, conn)
			removedFromIdle++
		default:
			// Decrement idle count for all connections we removed.
			atomic.AddInt64(&p.stats.IdleConnections, -removedFromIdle)

			return conns, removedFromIdle
		}
	}
}

// processHealthCheckedConnections validates and processes each connection.
func (p *Pool) processHealthCheckedConnections(conns []*pooledConn, removedFromIdle int64) (int, int) {
	validConns := 0
	removedConns := 0

	for _, conn := range conns {
		if p.isValidConnection(conn) {
			if p.returnValidConnection(conn) {
				validConns++
			} else {
				p.removeConnection(conn)

				removedConns++
			}
		} else {
			p.removeConnection(conn)

			removedConns++
		}
	}

	return validConns, removedConns
}

// returnValidConnection attempts to return a valid connection to the idle pool.
func (p *Pool) returnValidConnection(conn *pooledConn) bool {
	select {
	case p.idle <- conn:
		atomic.AddInt64(&p.stats.IdleConnections, 1)

		return true
	case <-p.closeCh:
		// Pool is closing, remove connection.
		return false
	default:
		// Pool is full.
		return false
	}
}

// finalizeHealthCheck completes the health check process.
func (p *Pool) finalizeHealthCheck(validConns, removedConns, totalConns int) {
	// If we removed connections, trigger maintenance to ensure minimum pool size.
	if removedConns > 0 {
		go p.maintain()
	}

	if validConns < totalConns {
		p.logger.Info("Health check completed",
			zap.Int("checked", totalConns),
			zap.Int("valid", validConns),
			zap.Int("removed", totalConns-validConns),
		)
	}
}
