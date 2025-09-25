package protocol

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/poiley/mcp-bridge/services/gateway/internal/discovery"
)

const (
	defaultTimeoutSeconds = 30
	defaultMaxConnections = 5
)

// ConnectionMultiplexer manages connection pooling and multiplexing across protocols.
type ConnectionMultiplexer struct {
	logger *zap.Logger

	// Connection pools by endpoint
	pools   map[string]*ConnectionPool
	poolsMu sync.RWMutex

	// Configuration
	config MultiplexConfig

	// Statistics
	stats   MultiplexStats
	statsMu sync.RWMutex
}

// MultiplexConfig configures connection multiplexing behavior.
type MultiplexConfig struct {
	MaxConnectionsPerEndpoint int           `yaml:"max_connections_per_endpoint"`
	ConnectionTimeout         time.Duration `yaml:"connection_timeout"`
	IdleTimeout               time.Duration `yaml:"idle_timeout"`
	HealthCheckInterval       time.Duration `yaml:"health_check_interval"`
	EnableMultiplexing        bool          `yaml:"enable_multiplexing"`
}

// ConnectionPool manages connections to a specific endpoint.
type ConnectionPool struct {
	endpoint    *discovery.Endpoint
	connections []*PooledConnection
	available   chan *PooledConnection
	config      MultiplexConfig
	mu          sync.RWMutex
	ctx         context.Context
	cancel      context.CancelFunc
	logger      *zap.Logger
	stats       PoolStats
}

// PooledConnection represents a connection in the pool.
type PooledConnection struct {
	ID         string
	Protocol   string
	Endpoint   *discovery.Endpoint
	CreatedAt  time.Time
	LastUsedAt time.Time
	UseCount   int64
	IsHealthy  bool
	connection interface{} // Actual connection object (protocol-specific)
	mu         sync.RWMutex
}

// MultiplexStats tracks multiplexing statistics.
type MultiplexStats struct {
	TotalPools        int                  `json:"total_pools"`
	TotalConnections  int                  `json:"total_connections"`
	ActiveConnections int                  `json:"active_connections"`
	PoolStats         map[string]PoolStats `json:"pool_stats"`
	LastActivity      time.Time            `json:"last_activity"`
}

// PoolStats tracks statistics for a connection pool.
type PoolStats struct {
	EndpointKey       string    `json:"endpoint_key"`
	TotalConnections  int       `json:"total_connections"`
	ActiveConnections int       `json:"active_connections"`
	IdleConnections   int       `json:"idle_connections"`
	CreatedAt         time.Time `json:"created_at"`
	LastUsed          time.Time `json:"last_used"`
	RequestsServed    int64     `json:"requests_served"`
	ErrorsCount       int64     `json:"errors_count"`
}

// NewConnectionMultiplexer creates a new connection multiplexer.
func NewConnectionMultiplexer(logger *zap.Logger) *ConnectionMultiplexer {
	config := MultiplexConfig{
		MaxConnectionsPerEndpoint: defaultRetryCount,
		ConnectionTimeout:         defaultTimeoutSeconds * time.Second,
		IdleTimeout:               defaultMaxConnections * time.Minute,
		HealthCheckInterval:       defaultTimeoutSeconds * time.Second,
		EnableMultiplexing:        true,
	}

	return &ConnectionMultiplexer{
		logger: logger,
		pools:  make(map[string]*ConnectionPool),
		config: config,
		stats: MultiplexStats{
			PoolStats: make(map[string]PoolStats),
		},
	}
}

// GetConnection gets or creates a connection to the specified endpoint.
func (m *ConnectionMultiplexer) GetConnection(
	ctx context.Context,
	endpoint *discovery.Endpoint,
) (*PooledConnection, error) {
	if !m.config.EnableMultiplexing {
		// Create direct connection without pooling
		return m.createDirectConnection(ctx, endpoint)
	}

	poolKey := m.getPoolKey(endpoint)

	m.poolsMu.RLock()
	pool, exists := m.pools[poolKey]
	m.poolsMu.RUnlock()

	if !exists {
		pool = m.createPool(endpoint)
		m.poolsMu.Lock()
		m.pools[poolKey] = pool
		m.poolsMu.Unlock()
	}

	return pool.GetConnection(ctx)
}

// createPool creates a new connection pool for an endpoint.
func (m *ConnectionMultiplexer) createPool(endpoint *discovery.Endpoint) *ConnectionPool {
	ctx, cancel := context.WithCancel(context.Background())

	pool := &ConnectionPool{
		endpoint:    endpoint,
		connections: make([]*PooledConnection, 0, m.config.MaxConnectionsPerEndpoint),
		available:   make(chan *PooledConnection, m.config.MaxConnectionsPerEndpoint),
		config:      m.config,
		ctx:         ctx,
		cancel:      cancel,
		logger:      m.logger.With(zap.String("endpoint", fmt.Sprintf("%s:%d", endpoint.Address, endpoint.Port))),
		stats: PoolStats{
			EndpointKey: m.getPoolKey(endpoint),
			CreatedAt:   time.Now(),
		},
	}

	// Start pool management goroutines
	go pool.manageConnections()
	go pool.healthChecker()

	m.logger.Info("Created connection pool",
		zap.String("endpoint", fmt.Sprintf("%s:%d", endpoint.Address, endpoint.Port)),
		zap.String("protocol", endpoint.Scheme))

	return pool
}

// GetConnection gets a connection from the pool.
func (p *ConnectionPool) GetConnection(ctx context.Context) (*PooledConnection, error) {
	// Try to get an available connection immediately
	conn := p.tryGetAvailableConnection(ctx)
	if conn != nil {
		return p.prepareConnection(conn)
	}

	// Check if context was cancelled
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Try to create a new connection
	conn, err := p.tryCreateNewConnection(ctx)
	if err == nil {
		return conn, nil
	}

	// Wait for a connection to become available
	return p.waitForConnection(ctx)
}

// tryGetAvailableConnection tries to get an available healthy connection.
func (p *ConnectionPool) tryGetAvailableConnection(ctx context.Context) *PooledConnection {
	select {
	case conn := <-p.available:
		if p.isConnectionHealthy(conn) {
			return conn
		}
		// Connection unhealthy, remove it
		p.removeConnection(conn)

		return nil
	case <-ctx.Done():
		return nil
	default:
		return nil
	}
}

// tryCreateNewConnection tries to create a new connection if limit not reached.
func (p *ConnectionPool) tryCreateNewConnection(ctx context.Context) (*PooledConnection, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.connections) >= p.config.MaxConnectionsPerEndpoint {
		return nil, errors.New("connection limit reached")
	}

	conn, err := p.createConnection(ctx)
	if err != nil {
		p.updateStatsUnsafe("error")

		return nil, fmt.Errorf("failed to create connection: %w", err)
	}

	p.connections = append(p.connections, conn)
	p.updateStatsUnsafe("request_served")

	return conn, nil
}

// waitForConnection waits for a connection to become available.
func (p *ConnectionPool) waitForConnection(ctx context.Context) (*PooledConnection, error) {
	select {
	case conn := <-p.available:
		if p.isConnectionHealthy(conn) {
			return p.prepareConnection(conn)
		}
		// Connection unhealthy, remove and try again
		p.removeConnection(conn)

		return p.GetConnection(ctx)

	case <-ctx.Done():
		return nil, ctx.Err()

	case <-time.After(p.config.ConnectionTimeout):
		p.updateStats("error")

		return nil, errors.New("timeout waiting for connection")
	}
}

// prepareConnection prepares a connection for use.
func (p *ConnectionPool) prepareConnection(conn *PooledConnection) (*PooledConnection, error) {
	conn.mu.Lock()
	conn.LastUsedAt = time.Now()
	conn.UseCount++
	conn.mu.Unlock()

	p.updateStats("request_served")

	return conn, nil
}

// createConnection creates a new pooled connection.
func (p *ConnectionPool) createConnection(ctx context.Context) (*PooledConnection, error) {
	connID := fmt.Sprintf("%s_%d_%d", p.endpoint.Scheme, p.endpoint.Port, time.Now().UnixNano())

	conn := &PooledConnection{
		ID:         connID,
		Protocol:   p.endpoint.Scheme,
		Endpoint:   p.endpoint,
		CreatedAt:  time.Now(),
		LastUsedAt: time.Now(),
		UseCount:   1,
		IsHealthy:  true,
		connection: nil, // Would be set to actual connection object
	}

	// Protocol-specific connection creation would go here
	// For now, we'll simulate connection creation

	p.logger.Debug("Created new connection",
		zap.String("connection_id", conn.ID),
		zap.String("protocol", conn.Protocol))

	return conn, nil
}

// ReturnConnection returns a connection to the pool.
func (p *ConnectionPool) ReturnConnection(conn *PooledConnection) {
	if !p.isConnectionHealthy(conn) {
		p.removeConnection(conn)

		return
	}

	conn.mu.Lock()
	conn.LastUsedAt = time.Now()
	conn.mu.Unlock()

	// Return to available pool
	select {
	case p.available <- conn:
		// Successfully returned to pool
	default:
		// Pool is full, close the connection
		p.removeConnection(conn)
	}
}

// isConnectionHealthy checks if a connection is healthy.
func (p *ConnectionPool) isConnectionHealthy(conn *PooledConnection) bool {
	if conn == nil {
		return false
	}

	conn.mu.RLock()
	defer conn.mu.RUnlock()

	// Check if connection is too old
	if time.Since(conn.CreatedAt) > 24*time.Hour {
		return false
	}

	// Check if connection has been idle too long
	if time.Since(conn.LastUsedAt) > p.config.IdleTimeout {
		return false
	}

	return conn.IsHealthy
}

// removeConnection removes a connection from the pool.
func (p *ConnectionPool) removeConnection(conn *PooledConnection) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Remove from connections slice
	for i, c := range p.connections {
		if c.ID == conn.ID {
			p.connections = append(p.connections[:i], p.connections[i+1:]...)

			break
		}
	}

	// Close the actual connection here
	// conn.connection.Close() - protocol-specific cleanup

	p.logger.Debug("Removed connection from pool",
		zap.String("connection_id", conn.ID),
		zap.String("reason", "unhealthy"))
}

// manageConnections manages the lifecycle of connections in the pool.
func (p *ConnectionPool) manageConnections() {
	ticker := time.NewTicker(p.config.IdleTimeout)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			p.cleanupIdleConnections()
		}
	}
}

// cleanupIdleConnections removes idle connections from the pool.
func (p *ConnectionPool) cleanupIdleConnections() {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	activeConnections := make([]*PooledConnection, 0, len(p.connections))

	for _, conn := range p.connections {
		conn.mu.RLock()
		isIdle := now.Sub(conn.LastUsedAt) > p.config.IdleTimeout
		conn.mu.RUnlock()

		if isIdle {
			// Close idle connection
			p.logger.Debug("Closing idle connection",
				zap.String("connection_id", conn.ID),
				zap.Duration("idle_time", now.Sub(conn.LastUsedAt)))
		} else {
			activeConnections = append(activeConnections, conn)
		}
	}

	removedCount := len(p.connections) - len(activeConnections)
	p.connections = activeConnections

	if removedCount > 0 {
		p.logger.Info("Cleaned up idle connections",
			zap.Int("removed", removedCount),
			zap.Int("remaining", len(p.connections)))
	}
}

// healthChecker performs periodic health checks on connections.
func (p *ConnectionPool) healthChecker() {
	ticker := time.NewTicker(p.config.HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			p.performHealthChecks()
		}
	}
}

// performHealthChecks checks the health of all connections in the pool.
func (p *ConnectionPool) performHealthChecks() {
	p.mu.RLock()
	connections := make([]*PooledConnection, len(p.connections))
	copy(connections, p.connections)
	p.mu.RUnlock()

	unhealthyCount := 0
	var unhealthyConns []*PooledConnection

	for _, conn := range connections {
		if !p.performHealthCheck(conn) {
			unhealthyCount++
			unhealthyConns = append(unhealthyConns, conn)
		}
	}

	// Remove unhealthy connections after health check loop
	for _, conn := range unhealthyConns {
		p.removeConnection(conn)
	}

	if unhealthyCount > 0 {
		p.logger.Info("Health check completed",
			zap.Int("unhealthy_removed", unhealthyCount),
			zap.Int("total_checked", len(connections)))
	}
}

// performHealthCheck performs a health check on a single connection.
func (p *ConnectionPool) performHealthCheck(conn *PooledConnection) bool {
	// Protocol-specific health check would go here
	// For now, we'll do basic checks
	if !p.isConnectionHealthy(conn) {
		return false
	}

	// Simulate protocol-specific ping/health check
	// In real implementation, this would send a ping message
	// and wait for a response within a timeout

	return true
}

// createDirectConnection creates a direct connection without pooling.
func (m *ConnectionMultiplexer) createDirectConnection(
	ctx context.Context,
	endpoint *discovery.Endpoint,
) (*PooledConnection, error) {
	connID := fmt.Sprintf("direct_%s_%d_%d", endpoint.Scheme, endpoint.Port, time.Now().UnixNano())

	conn := &PooledConnection{
		ID:         connID,
		Protocol:   endpoint.Scheme,
		Endpoint:   endpoint,
		CreatedAt:  time.Now(),
		LastUsedAt: time.Now(),
		UseCount:   1,
		IsHealthy:  true,
		connection: nil, // Would be set to actual connection object
	}

	m.logger.Debug("Created direct connection",
		zap.String("connection_id", conn.ID),
		zap.String("protocol", conn.Protocol))

	return conn, nil
}

// getPoolKey generates a unique key for a connection pool.
func (m *ConnectionMultiplexer) getPoolKey(endpoint *discovery.Endpoint) string {
	return fmt.Sprintf("%s_%s_%d_%s", endpoint.Scheme, endpoint.Address, endpoint.Port, endpoint.Namespace)
}

// updateStats updates pool statistics (acquires its own lock).
func (p *ConnectionPool) updateStats(operation string) {
	p.mu.Lock()
	p.updateStatsUnsafe(operation)
	p.mu.Unlock()
}

// updateStatsUnsafe updates pool statistics (assumes lock is already held).
func (p *ConnectionPool) updateStatsUnsafe(operation string) {
	p.stats.LastUsed = time.Now()
	p.stats.TotalConnections = len(p.connections)
	
	// Copy connections slice to avoid holding pool lock while accessing connection locks
	connections := make([]*PooledConnection, len(p.connections))
	copy(connections, p.connections)
	
	// Update counters while still holding the lock
	switch operation {
	case "request_served":
		p.stats.RequestsServed++
	case "error":
		p.stats.ErrorsCount++
	}
	
	// Temporarily release pool lock to avoid lock ordering issues
	p.mu.Unlock()

	// Count active vs idle connections without holding pool lock
	now := time.Now()
	activeCount := 0

	for _, conn := range connections {
		conn.mu.RLock()
		if now.Sub(conn.LastUsedAt) < time.Minute {
			activeCount++
		}
		conn.mu.RUnlock()
	}

	// Re-acquire pool lock to update computed stats
	p.mu.Lock()
	p.stats.ActiveConnections = activeCount
	p.stats.IdleConnections = p.stats.TotalConnections - activeCount
}

// GetStats returns current multiplexing statistics.
func (m *ConnectionMultiplexer) GetStats() MultiplexStats {
	m.statsMu.Lock()
	defer m.statsMu.Unlock()

	m.poolsMu.RLock()
	defer m.poolsMu.RUnlock()

	stats := MultiplexStats{
		TotalPools:   len(m.pools),
		PoolStats:    make(map[string]PoolStats),
		LastActivity: time.Now(),
	}

	totalConnections := 0
	activeConnections := 0

	for key, pool := range m.pools {
		pool.updateStats("")
		poolStats := pool.stats
		stats.PoolStats[key] = poolStats

		totalConnections += poolStats.TotalConnections
		activeConnections += poolStats.ActiveConnections
	}

	stats.TotalConnections = totalConnections
	stats.ActiveConnections = activeConnections

	return stats
}

// Shutdown gracefully shuts down the connection multiplexer.
func (m *ConnectionMultiplexer) Shutdown(ctx context.Context) error {
	m.logger.Info("Shutting down connection multiplexer")

	m.poolsMu.Lock()
	defer m.poolsMu.Unlock()

	// Close all pools
	for key, pool := range m.pools {
		pool.cancel()

		// Close all connections in pool
		pool.mu.Lock()

		for _, conn := range pool.connections {
			// Close actual connections here
			m.logger.Debug("Closing connection", zap.String("connection_id", conn.ID))
		}

		pool.mu.Unlock()

		delete(m.pools, key)
	}

	m.logger.Info("Connection multiplexer shutdown completed")

	return nil
}
