package direct

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/actual-software/mcp-bridge/services/router/internal/constants"
	"go.uber.org/zap"
)

// ConnectionPoolConfig defines configuration for connection pooling.
type ConnectionPoolConfig struct {
	MaxIdleConnections    int           `mapstructure:"max_idle_connections"    yaml:"max_idle_connections"`
	MaxActiveConnections  int           `mapstructure:"max_active_connections"  yaml:"max_active_connections"`
	ConnectionTTL         time.Duration `mapstructure:"connection_ttl"          yaml:"connection_ttl"`
	IdleTimeout           time.Duration `mapstructure:"idle_timeout"            yaml:"idle_timeout"`
	HealthCheckInterval   time.Duration `mapstructure:"health_check_interval"   yaml:"health_check_interval"`
	EnableConnectionReuse bool          `mapstructure:"enable_connection_reuse" yaml:"enable_connection_reuse"`
	CleanupInterval       time.Duration `mapstructure:"cleanup_interval"        yaml:"cleanup_interval"`
}

// DefaultConnectionPoolConfig returns default connection pool configuration.
func DefaultConnectionPoolConfig() ConnectionPoolConfig {
	return ConnectionPoolConfig{
		MaxIdleConnections:    constants.DefaultMaxIdleConnections,
		MaxActiveConnections:  constants.DefaultMaxActiveConnections,
		ConnectionTTL:         constants.DefaultMaxLifetime,
		IdleTimeout:           constants.DefaultMaxIdleTime,
		HealthCheckInterval:   time.Minute,
		EnableConnectionReuse: true,
		CleanupInterval:       constants.DefaultCleanupInterval,
	}
}

// PooledConnection represents a connection in the pool.
type PooledConnection struct {
	Client     DirectClient
	ServerURL  string
	Protocol   ClientType
	CreatedAt  time.Time
	LastUsedAt time.Time
	UsageCount int64
	IsHealthy  bool
	mu         sync.RWMutex
}

// UpdateLastUsed updates the last used timestamp and increments usage count.
func (pc *PooledConnection) UpdateLastUsed() {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	pc.LastUsedAt = time.Now()
	pc.UsageCount++
}

// GetUsageCount returns the current usage count.
func (pc *PooledConnection) GetUsageCount() int64 {
	pc.mu.RLock()
	defer pc.mu.RUnlock()

	return pc.UsageCount
}

// IsExpired checks if the connection has exceeded its TTL.
func (pc *PooledConnection) IsExpired(ttl time.Duration) bool {
	pc.mu.RLock()
	defer pc.mu.RUnlock()

	return time.Since(pc.CreatedAt) > ttl
}

// IsIdle checks if the connection has been idle for too long.
func (pc *PooledConnection) IsIdle(idleTimeout time.Duration) bool {
	pc.mu.RLock()
	defer pc.mu.RUnlock()

	return time.Since(pc.LastUsedAt) > idleTimeout
}

// SetHealthy sets the health status of the connection.
func (pc *PooledConnection) SetHealthy(healthy bool) {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	pc.IsHealthy = healthy
}

// GetHealthy returns the current health status.
func (pc *PooledConnection) GetHealthy() bool {
	pc.mu.RLock()
	defer pc.mu.RUnlock()

	return pc.IsHealthy
}

// ConnectionPool manages pooled connections with enhanced lifecycle management.
type ConnectionPool struct {
	config ConnectionPoolConfig
	logger *zap.Logger

	// Connection storage by protocol and server URL.
	connections map[ClientType]map[string]*PooledConnection
	mu          sync.RWMutex

	// Pool statistics.
	activeConnections int
	totalConnections  int

	// Background tasks.
	ctx           context.Context
	cancel        context.CancelFunc
	cleanupTicker *time.Ticker
	healthTicker  *time.Ticker
	wg            sync.WaitGroup
}

// NewConnectionPool creates a new connection pool.
func NewConnectionPool(config ConnectionPoolConfig, logger *zap.Logger) *ConnectionPool {
	ctx, cancel := context.WithCancel(context.Background())

	pool := &ConnectionPool{
		config:      config,
		logger:      logger,
		connections: make(map[ClientType]map[string]*PooledConnection),
		ctx:         ctx,
		cancel:      cancel,
	}

	// Initialize protocol maps.
	for _, protocol := range []ClientType{ClientTypeStdio, ClientTypeHTTP, ClientTypeWebSocket, ClientTypeSSE} {
		pool.connections[protocol] = make(map[string]*PooledConnection)
	}

	// Start background tasks.
	pool.startBackgroundTasks()

	return pool
}

// GetConnection retrieves or creates a connection from the pool.
func (p *ConnectionPool) GetConnection(
	ctx context.Context,
	serverURL string,
	protocol ClientType,
	createFunc func() (DirectClient, error),
) (*PooledConnection, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Check if connection reuse is enabled and we have an existing healthy connection.
	if p.config.EnableConnectionReuse {
		if conn, exists := p.connections[protocol][serverURL]; exists && conn.GetHealthy() {
			conn.UpdateLastUsed()
			p.logger.Debug("Reusing existing connection",
				zap.String("server_url", serverURL),
				zap.String("protocol", string(protocol)),
				zap.Int64("usage_count", conn.GetUsageCount()))

			return conn, nil
		}
	}

	// Check active connection limits.
	if p.activeConnections >= p.config.MaxActiveConnections {
		// Try to evict idle connections to make room.
		if evicted := p.evictIdleConnections(); evicted == 0 {
			return nil, fmt.Errorf("maximum active connections reached (%d)", p.config.MaxActiveConnections)
		}
	}

	// Create new connection.
	client, err := createFunc()
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}

	conn := &PooledConnection{
		Client:     client,
		ServerURL:  serverURL,
		Protocol:   protocol,
		CreatedAt:  time.Now(),
		LastUsedAt: time.Now(),
		UsageCount: 1,
		IsHealthy:  true,
	}

	p.connections[protocol][serverURL] = conn
	p.activeConnections++
	p.totalConnections++

	p.logger.Debug("Created new pooled connection",
		zap.String("server_url", serverURL),
		zap.String("protocol", string(protocol)),
		zap.Int("active_connections", p.activeConnections))

	return conn, nil
}

// RemoveConnection removes a connection from the pool.
func (p *ConnectionPool) RemoveConnection(serverURL string, protocol ClientType) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if conn, exists := p.connections[protocol][serverURL]; exists {
		// Close the client connection.
		if err := conn.Client.Close(p.ctx); err != nil {
			p.logger.Warn("Error closing client during removal",
				zap.String("server_url", serverURL),
				zap.String("protocol", string(protocol)),
				zap.Error(err))
		}

		delete(p.connections[protocol], serverURL)
		p.activeConnections--

		p.logger.Debug("Removed connection from pool",
			zap.String("server_url", serverURL),
			zap.String("protocol", string(protocol)),
			zap.Int("active_connections", p.activeConnections))

		return nil
	}

	return fmt.Errorf("connection not found: %s (%s)", serverURL, protocol)
}

// GetStats returns current pool statistics.
func (p *ConnectionPool) GetStats() map[string]interface{} {
	p.mu.RLock()
	defer p.mu.RUnlock()

	stats := map[string]interface{}{
		"active_connections": p.activeConnections,
		"total_connections":  p.totalConnections,
		"max_active":         p.config.MaxActiveConnections,
		"max_idle":           p.config.MaxIdleConnections,
	}

	// Add per-protocol statistics.
	protocolStats := make(map[string]int)
	for protocol, connMap := range p.connections {
		protocolStats[string(protocol)] = len(connMap)
	}

	stats["protocol_breakdown"] = protocolStats

	return stats
}

// GetActiveConnections returns a map of active connections for health checking.
func (p *ConnectionPool) GetActiveConnections() map[string]*PooledConnection {
	p.mu.RLock()
	defer p.mu.RUnlock()

	activeConnections := make(map[string]*PooledConnection)

	for _, protocolMap := range p.connections {
		for serverURL, conn := range protocolMap {
			if conn != nil && conn.GetHealthy() && !conn.IsExpired(p.config.ConnectionTTL) {
				activeConnections[serverURL] = conn
			}
		}
	}

	return activeConnections
}

// Close shuts down the connection pool.
func (p *ConnectionPool) Close() error {
	p.logger.Info("Shutting down connection pool")

	// Cancel background tasks.
	p.cancel()

	// Stop tickers.
	if p.cleanupTicker != nil {
		p.cleanupTicker.Stop()
	}

	if p.healthTicker != nil {
		p.healthTicker.Stop()
	}

	// Wait for background tasks to complete.
	p.wg.Wait()

	// Close all connections.
	p.mu.Lock()
	defer p.mu.Unlock()

	for protocol, connMap := range p.connections {
		for serverURL, conn := range connMap {
			if err := conn.Client.Close(p.ctx); err != nil {
				p.logger.Warn("Error closing connection during shutdown",
					zap.String("server_url", serverURL),
					zap.String("protocol", string(protocol)),
					zap.Error(err))
			}
		}
	}

	p.logger.Info("Connection pool shutdown complete",
		zap.Int("closed_connections", p.activeConnections))

	return nil
}

// startBackgroundTasks starts background maintenance tasks.
func (p *ConnectionPool) startBackgroundTasks() {
	// Cleanup task for expired and idle connections.
	p.cleanupTicker = time.NewTicker(p.config.CleanupInterval)
	p.wg.Add(1)

	go func() {
		defer p.wg.Done()

		for {
			select {
			case <-p.ctx.Done():
				return
			case <-p.cleanupTicker.C:
				p.performCleanup()
			}
		}
	}()

	// Health check task.
	if p.config.HealthCheckInterval > 0 {
		p.healthTicker = time.NewTicker(p.config.HealthCheckInterval)
		p.wg.Add(1)

		go func() {
			defer p.wg.Done()

			for {
				select {
				case <-p.ctx.Done():
					return
				case <-p.healthTicker.C:
					p.performHealthChecks()
				}
			}
		}()
	}
}

// performCleanup removes expired and idle connections.
func (p *ConnectionPool) performCleanup() {
	p.mu.Lock()
	defer p.mu.Unlock()

	var removedCount int

	for protocol, connMap := range p.connections {
		for serverURL, conn := range connMap {
			shouldRemove := false
			reason := ""

			if conn.IsExpired(p.config.ConnectionTTL) {
				shouldRemove = true
				reason = "expired"
			} else if conn.IsIdle(p.config.IdleTimeout) {
				shouldRemove = true
				reason = "idle"
			}

			if shouldRemove {
				if err := conn.Client.Close(p.ctx); err != nil {
					p.logger.Warn("Error closing connection during cleanup",
						zap.String("server_url", serverURL),
						zap.String("protocol", string(protocol)),
						zap.String("reason", reason),
						zap.Error(err))
				}

				delete(connMap, serverURL)

				p.activeConnections--
				removedCount++

				p.logger.Debug("Removed connection during cleanup",
					zap.String("server_url", serverURL),
					zap.String("protocol", string(protocol)),
					zap.String("reason", reason))
			}
		}
	}

	if removedCount > 0 {
		p.logger.Info("Connection cleanup completed",
			zap.Int("removed_connections", removedCount),
			zap.Int("active_connections", p.activeConnections))
	}
}

// performHealthChecks checks the health of all connections.
func (p *ConnectionPool) performHealthChecks() {
	p.mu.RLock()

	var connectionsToCheck []*PooledConnection

	for _, connMap := range p.connections {
		for _, conn := range connMap {
			connectionsToCheck = append(connectionsToCheck, conn)
		}
	}

	p.mu.RUnlock()

	var healthyCount, unhealthyCount int

	for _, conn := range connectionsToCheck {
		ctx, cancel := context.WithTimeout(p.ctx, defaultMaxConnections*time.Second)

		if err := conn.Client.Health(ctx); err != nil {
			conn.SetHealthy(false)

			unhealthyCount++

			p.logger.Debug("Connection health check failed",
				zap.String("server_url", conn.ServerURL),
				zap.String("protocol", string(conn.Protocol)),
				zap.Error(err))
		} else {
			conn.SetHealthy(true)

			healthyCount++
		}

		cancel()
	}

	if len(connectionsToCheck) > 0 {
		p.logger.Debug("Health check completed",
			zap.Int("total_checked", len(connectionsToCheck)),
			zap.Int("healthy", healthyCount),
			zap.Int("unhealthy", unhealthyCount))
	}
}

// evictIdleConnections removes idle connections to make room for new ones.
func (p *ConnectionPool) evictIdleConnections() int {
	var evictedCount int

	for protocol, connMap := range p.connections {
		for serverURL, conn := range connMap {
			if conn.IsIdle(p.config.IdleTimeout) {
				if err := conn.Client.Close(p.ctx); err != nil {
					p.logger.Warn("Error closing connection during eviction",
						zap.String("server_url", serverURL),
						zap.String("protocol", string(protocol)),
						zap.Error(err))
				}

				delete(connMap, serverURL)

				p.activeConnections--
				evictedCount++

				p.logger.Debug("Evicted idle connection",
					zap.String("server_url", serverURL),
					zap.String("protocol", string(protocol)))
			}
		}
	}

	return evictedCount
}
