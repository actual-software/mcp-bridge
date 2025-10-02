package websocket

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	customerrors "github.com/actual-software/mcp-bridge/services/gateway/internal/errors"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/metrics"
	"github.com/actual-software/mcp-bridge/services/router/pkg/mcp"

	"go.uber.org/zap"
)

const (
	defaultTimeoutSeconds      = 30
	defaultRetryCount          = 10
	defaultBufferSize          = 1024
	defaultMaxConnections      = 5
	defaultMaxTimeoutSeconds   = 60
	defaultMaxIdleSeconds      = 300
	defaultWriteBufferSize     = 4096
	defaultReadBufferSize      = 4096
	halfDivisor                = 2
	websocketRetryDelaySeconds = 10
)

// BackendMetrics contains performance and health metrics for a backend.
type BackendMetrics struct {
	RequestCount    uint64        `json:"request_count"`
	ErrorCount      uint64        `json:"error_count"`
	AverageLatency  time.Duration `json:"average_latency"`
	LastHealthCheck time.Time     `json:"last_health_check"`
	IsHealthy       bool          `json:"is_healthy"`
	ConnectionTime  time.Time     `json:"connection_time"`
}

// Config contains WebSocket backend configuration.
type Config struct {
	Endpoints        []string          `mapstructure:"endpoints"`
	Headers          map[string]string `mapstructure:"headers"`
	ConnectionPool   PoolConfig        `mapstructure:"connection_pool"`
	HealthCheck      HealthCheckConfig `mapstructure:"health_check"`
	Timeout          time.Duration     `mapstructure:"timeout"`
	HandshakeTimeout time.Duration     `mapstructure:"handshake_timeout"`
	PingInterval     time.Duration     `mapstructure:"ping_interval"`
	PongTimeout      time.Duration     `mapstructure:"pong_timeout"`
	MaxMessageSize   int64             `mapstructure:"max_message_size"`
	Origin           string            `mapstructure:"origin"`
}

// PoolConfig contains connection pool settings.
type PoolConfig struct {
	MinSize     int           `mapstructure:"min_size"`
	MaxSize     int           `mapstructure:"max_size"`
	MaxIdle     time.Duration `mapstructure:"max_idle"`
	IdleTimeout time.Duration `mapstructure:"idle_timeout"`
}

// HealthCheckConfig contains health check settings.
type HealthCheckConfig struct {
	Enabled  bool          `mapstructure:"enabled"`
	Interval time.Duration `mapstructure:"interval"`
	Timeout  time.Duration `mapstructure:"timeout"`
}

// Connection represents a WebSocket connection with metadata.
type Connection struct {
	conn     *websocket.Conn
	url      string
	created  time.Time
	lastUsed time.Time
	inUse    bool
	healthy  bool
	mu       sync.RWMutex
	writeMu  sync.Mutex // Protects concurrent writes to the WebSocket
}

// ConnectionPool manages a pool of WebSocket connections.
type ConnectionPool struct {
	config      PoolConfig
	connections []*Connection
	mu          sync.RWMutex
	logger      *zap.Logger
}

// Backend implements the WebSocket backend for MCP servers.
type Backend struct {
	name            string
	config          Config
	logger          *zap.Logger
	metricsRegistry *metrics.Registry

	// Connection pool
	pools   map[string]*ConnectionPool
	poolsMu sync.RWMutex

	// Request correlation
	requestMap   map[string]chan *mcp.Response
	requestMapMu sync.RWMutex
	requestID    uint64

	// State management
	mu      sync.RWMutex
	running bool

	// Metrics
	metrics    BackendMetrics
	mu_metrics sync.RWMutex

	// Shutdown coordination
	shutdownCh chan struct{}
	wg         sync.WaitGroup
}

// CreateWebSocketBackend creates a WebSocket-based backend instance for bidirectional communication.
func CreateWebSocketBackend(
	name string,
	config Config,
	logger *zap.Logger,
	metricsRegistry *metrics.Registry,
) *Backend {
	// Apply default configuration
	config = applyDefaultConfig(config)

	return &Backend{
		name:            name,
		config:          config,
		logger:          logger.With(zap.String("backend", name), zap.String("protocol", "websocket")),
		metricsRegistry: metricsRegistry,
		pools:           make(map[string]*ConnectionPool),
		requestMap:      make(map[string]chan *mcp.Response),
		shutdownCh:      make(chan struct{}),
		metrics: BackendMetrics{
			IsHealthy: false,
		},
	}
}

// applyDefaultConfig sets default values for missing configuration.
func applyDefaultConfig(config Config) Config {
	// Apply timeout defaults
	config = applyTimeoutDefaults(config)

	// Apply connection pool defaults
	config = applyConnectionPoolDefaults(config)

	// Apply health check defaults
	config = applyHealthCheckDefaults(config)

	return config
}

// applyTimeoutDefaults sets default timeout values.
func applyTimeoutDefaults(config Config) Config {
	if config.Timeout == 0 {
		config.Timeout = defaultTimeoutSeconds * time.Second
	}

	if config.HandshakeTimeout == 0 {
		config.HandshakeTimeout = defaultRetryCount * time.Second
	}

	if config.PingInterval == 0 {
		config.PingInterval = defaultTimeoutSeconds * time.Second
	}

	if config.PongTimeout == 0 {
		config.PongTimeout = defaultRetryCount * time.Second
	}

	if config.MaxMessageSize == 0 {
		config.MaxMessageSize = defaultBufferSize * defaultBufferSize // 1MB
	}

	return config
}

// applyConnectionPoolDefaults sets default connection pool values.
func applyConnectionPoolDefaults(config Config) Config {
	if config.ConnectionPool.MinSize == 0 {
		config.ConnectionPool.MinSize = 1
	}

	if config.ConnectionPool.MaxSize == 0 {
		config.ConnectionPool.MaxSize = 10
	}

	if config.ConnectionPool.MaxIdle == 0 {
		config.ConnectionPool.MaxIdle = defaultMaxIdleSeconds * time.Second
	}

	if config.ConnectionPool.IdleTimeout == 0 {
		config.ConnectionPool.IdleTimeout = defaultMaxTimeoutSeconds * time.Second
	}

	return config
}

// applyHealthCheckDefaults sets default health check values.
func applyHealthCheckDefaults(config Config) Config {
	if config.HealthCheck.Interval == 0 {
		config.HealthCheck.Interval = defaultTimeoutSeconds * time.Second
	}

	if config.HealthCheck.Timeout == 0 {
		config.HealthCheck.Timeout = defaultMaxConnections * time.Second
	}

	return config
}

// Start initializes the WebSocket backend.
func (b *Backend) Start(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.running {
		return customerrors.New(customerrors.TypeInternal, "backend already running").
			WithComponent("backend_websocket").
			WithContext("backend_name", b.name)
	}

	// Initialize connection pools for each endpoint
	for _, endpoint := range b.config.Endpoints {
		pool := &ConnectionPool{
			config:      b.config.ConnectionPool,
			connections: make([]*Connection, 0, b.config.ConnectionPool.MaxSize),
			logger:      b.logger.With(zap.String("endpoint", endpoint)),
		}

		b.poolsMu.Lock()
		b.pools[endpoint] = pool
		b.poolsMu.Unlock()

		// Pre-create minimum connections
		if err := b.initializePool(ctx, endpoint, pool); err != nil {
			b.logger.Warn("failed to initialize pool", zap.String("endpoint", endpoint), zap.Error(err))
		}
	}

	b.running = true
	b.updateMetrics(func(m *BackendMetrics) {
		m.ConnectionTime = time.Now()
		m.IsHealthy = true
	})

	// Start background tasks
	if b.config.HealthCheck.Enabled {
		b.wg.Add(1)

		go b.healthCheckLoop(ctx)
	}

	b.wg.Add(1)

	go b.poolMaintenanceLoop()

	b.logger.Info("WebSocket backend started successfully",
		zap.Strings("endpoints", b.config.Endpoints))

	return nil
}

// initializePool creates initial connections for a pool.
func (b *Backend) initializePool(ctx context.Context, endpoint string, pool *ConnectionPool) error {
	pool.mu.Lock()
	defer pool.mu.Unlock()

	for i := 0; i < pool.config.MinSize; i++ {
		conn, err := b.createConnection(ctx, endpoint)
		if err != nil {
			return customerrors.CreateConnectionError(b.name, "websocket",
				fmt.Sprintf("failed to create initial connection %d", i), err)
		}

		pool.connections = append(pool.connections, conn)
	}

	return nil
}

// createConnection establishes a new WebSocket connection.
func (b *Backend) createConnection(ctx context.Context, endpoint string) (*Connection, error) {
	// Parse URL
	_, err := url.Parse(endpoint)
	if err != nil {
		return nil, customerrors.Wrap(err, "invalid endpoint URL").
			WithComponent("backend_websocket").
			WithContext("backend_name", b.name).
			WithContext("endpoint", endpoint)
	}

	// Create dialer with timeout
	dialer := websocket.Dialer{
		HandshakeTimeout: b.config.HandshakeTimeout,
		ReadBufferSize:   defaultReadBufferSize,
		WriteBufferSize:  defaultWriteBufferSize,
	}

	// Set up headers
	headers := http.Header{}
	for k, v := range b.config.Headers {
		headers.Set(k, v)
	}

	// Set origin if specified
	if b.config.Origin != "" {
		headers.Set("Origin", b.config.Origin)
	}

	// Connect
	conn, resp, err := dialer.DialContext(ctx, endpoint, headers)
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}

	if err != nil {
		return nil, customerrors.WrapWebSocketUpgradeError(ctx, err, b.name, 0).
			WithContext("endpoint", endpoint)
	}

	// Set connection limits
	conn.SetReadLimit(b.config.MaxMessageSize)

	// Set up ping/pong handlers
	conn.SetPongHandler(func(appData string) error {
		return conn.SetReadDeadline(time.Now().Add(b.config.PongTimeout))
	})

	connection := &Connection{
		conn:     conn,
		url:      endpoint,
		created:  time.Now(),
		lastUsed: time.Now(),
		healthy:  true,
	}

	// Start ping routine for this connection
	b.wg.Add(1)

	go b.pingRoutine(connection)

	// Start response reader for this connection
	b.wg.Add(1)

	go b.readResponses(connection)

	return connection, nil
}

// getConnection gets an available connection from the pool.
func (b *Backend) getConnection(ctx context.Context) (*Connection, error) {
	// Simple round-robin across endpoints
	if len(b.config.Endpoints) == 0 {
		return nil, customerrors.New(customerrors.TypeValidation, "no endpoints configured").
			WithComponent("backend_websocket").
			WithContext("backend_name", b.name)
	}

	endpoint := b.config.Endpoints[b.selectEndpoint()]

	b.poolsMu.RLock()
	pool, exists := b.pools[endpoint]
	b.poolsMu.RUnlock()

	if !exists {
		return nil, customerrors.CreateBackendUnavailableError(b.name, "no pool for endpoint").
			WithContext("endpoint", endpoint)
	}

	pool.mu.Lock()
	defer pool.mu.Unlock()

	// Look for available connection
	for _, conn := range pool.connections {
		conn.mu.Lock()

		if !conn.inUse && conn.healthy {
			conn.inUse = true
			conn.lastUsed = time.Now()
			conn.mu.Unlock()

			return conn, nil
		}

		conn.mu.Unlock()
	}

	// No available connection, create new one if under limit
	if len(pool.connections) < pool.config.MaxSize {
		conn, err := b.createConnection(ctx, endpoint)
		if err != nil {
			return nil, err
		}

		conn.inUse = true
		pool.connections = append(pool.connections, conn)

		return conn, nil
	}

	return nil, customerrors.CreateBackendUnavailableError(b.name, "no available connections").
		WithContext("endpoint", endpoint).
		WithContext("pool_size", len(pool.connections)).
		WithContext("max_pool_size", pool.config.MaxSize)
}

// releaseConnection returns a connection to the pool.
func (b *Backend) releaseConnection(conn *Connection) {
	conn.mu.Lock()
	conn.inUse = false
	conn.lastUsed = time.Now()
	conn.mu.Unlock()
}

// selectEndpoint selects an endpoint using round-robin.
func (b *Backend) selectEndpoint() int {
	endpointCount := len(b.config.Endpoints)
	if endpointCount == 0 {
		return 0
	}

	// Safe modulo operation with bounds checking
	nextID := atomic.AddUint64(&b.requestID, 1)

	// Use modulo to get index within bounds, avoiding overflow concerns
	// by working with the remainder which is guaranteed < endpointCount
	remainder := nextID % uint64(endpointCount)

	// remainder is guaranteed to fit in int since endpointCount comes from len()
	// and Go slice lengths are bounded by int (max int32 on 32-bit, int64 on 64-bit)
	// Since remainder < endpointCount and endpointCount <= MaxInt, this is safe
	if remainder > math.MaxInt {
		// This should never happen given the constraints, but check anyway
		return 0
	}

	return int(remainder)
}

// SendRequest sends an MCP request over WebSocket.
func (b *Backend) SendRequest(ctx context.Context, req *mcp.Request) (*mcp.Response, error) {
	return b.sendRequestInternal(ctx, req)
}

// sendRequestInternal implements the actual request sending logic.
func (b *Backend) sendRequestInternal(ctx context.Context, req *mcp.Request) (*mcp.Response, error) {
	// Ensure request has an ID
	requestID := b.ensureRequestID(req)

	// Get a connection from the pool
	conn, err := b.getConnection(ctx)
	if err != nil {
		b.recordErrorMetric(err)

		return nil, err
	}
	defer b.releaseConnection(conn)

	// Setup response channel
	respCh := b.setupResponseChannel(requestID)
	defer b.cleanupResponseChannel(requestID, respCh)

	// Send the request over WebSocket
	if err := b.sendWebSocketRequest(ctx, conn, req); err != nil {
		b.recordErrorMetric(err)

		return nil, err
	}

	// Wait for and return response
	resp, err := b.waitForWebSocketResponse(ctx, respCh, requestID)
	if err != nil {
		b.recordErrorMetric(err)

		return nil, err
	}

	return resp, nil
}

// ensureRequestID generates or extracts a request ID.
func (b *Backend) ensureRequestID(req *mcp.Request) string {
	if req.ID == nil || req.ID == "" {
		requestID := fmt.Sprintf("%s-%d", b.name, atomic.AddUint64(&b.requestID, 1))
		req.ID = requestID

		return requestID
	}

	if requestID, ok := req.ID.(string); ok {
		return requestID
	}

	return fmt.Sprintf("%v", req.ID)
}

// setupResponseChannel creates and registers a response channel.
func (b *Backend) setupResponseChannel(requestID string) chan *mcp.Response {
	respCh := make(chan *mcp.Response, 1)

	b.requestMapMu.Lock()
	b.requestMap[requestID] = respCh
	b.requestMapMu.Unlock()

	return respCh
}

// cleanupResponseChannel removes the response channel from the map.
func (b *Backend) cleanupResponseChannel(requestID string, respCh chan *mcp.Response) {
	b.requestMapMu.Lock()
	delete(b.requestMap, requestID)
	close(respCh)
	b.requestMapMu.Unlock()
}

// sendWebSocketRequest sends the request over the WebSocket connection.
func (b *Backend) sendWebSocketRequest(ctx context.Context, conn *Connection, req *mcp.Request) error {
	// Use write mutex to prevent concurrent writes
	conn.writeMu.Lock()
	err := conn.conn.WriteJSON(req)
	conn.writeMu.Unlock()

	if err != nil {
		b.markConnectionUnhealthy(conn)
		b.recordErrorMetric(err)

		return customerrors.WrapWriteError(ctx, err, b.name, 0).
			WithContext("request_id", req.ID).
			WithContext("method", req.Method)
	}

	return nil
}

// markConnectionUnhealthy marks a connection as unhealthy.
func (b *Backend) markConnectionUnhealthy(conn *Connection) {
	conn.mu.Lock()
	conn.healthy = false
	conn.mu.Unlock()
}

// waitForWebSocketResponse waits for a response with timeout handling.
func (b *Backend) waitForWebSocketResponse(
	ctx context.Context, respCh chan *mcp.Response, requestID string,
) (*mcp.Response, error) {
	startTime := time.Now()
	timeout := b.calculateTimeout(ctx)

	select {
	case resp := <-respCh:
		b.recordSuccessMetric(startTime)

		return resp, nil
	case <-time.After(timeout):
		timeoutErr := customerrors.CreateConnectionTimeoutError(b.name, timeout).
			WithContext("request_id", requestID)
		b.recordErrorMetric(timeoutErr)

		return nil, timeoutErr
	case <-ctx.Done():
		b.recordErrorMetric(ctx.Err())

		return nil, ctx.Err()
	}
}

// calculateTimeout determines the appropriate timeout for the request.
func (b *Backend) calculateTimeout(ctx context.Context) time.Duration {
	timeout := b.config.Timeout
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining < timeout {
			timeout = remaining
		}
	}

	return timeout
}

// recordSuccessMetric updates metrics for a successful request.
func (b *Backend) recordSuccessMetric(startTime time.Time) {
	duration := time.Since(startTime)

	b.updateMetrics(func(m *BackendMetrics) {
		m.RequestCount++
		if m.RequestCount == 1 {
			m.AverageLatency = duration
		} else {
			m.AverageLatency = (m.AverageLatency + duration) / halfDivisor
		}
	})
}

// recordErrorMetric updates metrics for a failed request.
func (b *Backend) recordErrorMetric(err error) {
	b.updateMetrics(func(m *BackendMetrics) {
		m.ErrorCount++
	})

	// Record to Prometheus metrics if available
	if b.metricsRegistry != nil && err != nil {
		// Check if it's already a GatewayError
		var gatewayErr *customerrors.GatewayError
		if !errors.As(err, &gatewayErr) {
			// Wrap the error with backend context
			gatewayErr = customerrors.Wrap(err, "backend error").
				WithComponent("websocket_backend").
				WithContext("backend_name", b.name)
		} else {
			// Ensure component is set
			gatewayErr = gatewayErr.WithComponent("websocket_backend").
				WithContext("backend_name", b.name)
		}

		customerrors.RecordError(gatewayErr, b.metricsRegistry)
	}
}

// readResponses reads responses from a WebSocket connection.
func (b *Backend) readResponses(conn *Connection) {
	defer b.wg.Done()

	for {
		// Check for shutdown signal
		if b.shouldStopReading() {
			return
		}

		// Check connection health
		if !b.isConnectionHealthy(conn) {
			return
		}

		// Read and route response
		if !b.readAndRouteResponse(conn) {
			return
		}
	}
}

// shouldStopReading checks if the reader should stop.
func (b *Backend) shouldStopReading() bool {
	select {
	case <-b.shutdownCh:
		return true
	default:
		return false
	}
}

// isConnectionHealthy checks if the connection is healthy.
func (b *Backend) isConnectionHealthy(conn *Connection) bool {
	conn.mu.RLock()
	healthy := conn.healthy
	conn.mu.RUnlock()

	return healthy
}

// readAndRouteResponse reads a response and routes it to the waiting request.
func (b *Backend) readAndRouteResponse(conn *Connection) bool {
	var response mcp.Response

	// Don't hold the mutex during the blocking read operation
	err := conn.conn.ReadJSON(&response)
	if err != nil {
		b.handleReadError(conn, err)

		return false
	}

	// Route the response
	b.routeResponse(&response)

	return true
}

// handleReadError handles errors from reading the WebSocket connection.
func (b *Backend) handleReadError(conn *Connection, err error) {
	if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
		b.logger.Error("WebSocket read error", zap.Error(err))
	}

	b.markConnectionUnhealthy(conn)
}

// routeResponse routes a response to the waiting request channel.
func (b *Backend) routeResponse(response *mcp.Response) {
	responseID, ok := response.ID.(string)
	if !ok {
		return
	}

	b.requestMapMu.RLock()
	defer b.requestMapMu.RUnlock()

	if ch, exists := b.requestMap[responseID]; exists {
		select {
		case ch <- response:
		default:
			b.logger.Warn("response channel full", zap.String("request_id", responseID))
		}
	}
}

// pingRoutine sends periodic ping frames.
func (b *Backend) pingRoutine(conn *Connection) {
	defer b.wg.Done()

	ticker := time.NewTicker(b.config.PingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-b.shutdownCh:
			return
		case <-ticker.C:
			conn.mu.RLock()
			healthy := conn.healthy
			conn.mu.RUnlock()

			if healthy {
				conn.writeMu.Lock()

				deadline := time.Now().Add(websocketRetryDelaySeconds * time.Second)
				err := conn.conn.WriteControl(websocket.PingMessage, []byte{}, deadline)
				conn.writeMu.Unlock()

				if err != nil {
					conn.mu.Lock()
					conn.healthy = false
					conn.mu.Unlock()
					b.logger.Warn("ping failed", zap.String("endpoint", conn.url), zap.Error(err))
				}
			}
		}
	}
}

// Health checks if the backend is healthy.
func (b *Backend) Health(ctx context.Context) error {
	b.mu.RLock()

	if !b.running {
		b.mu.RUnlock()

		return customerrors.CreateBackendUnavailableError(b.name, "not running")
	}

	b.mu.RUnlock()

	// Check if any pools have healthy connections
	b.poolsMu.RLock()
	defer b.poolsMu.RUnlock()

	hasHealthy := false

	for endpoint, pool := range b.pools {
		pool.mu.RLock()

		for _, conn := range pool.connections {
			conn.mu.RLock()

			if conn.healthy {
				hasHealthy = true
			}

			conn.mu.RUnlock()
		}

		pool.mu.RUnlock()

		if !hasHealthy {
			b.logger.Warn("no healthy connections", zap.String("endpoint", endpoint))
		}
	}

	if !hasHealthy {
		b.updateMetrics(func(m *BackendMetrics) {
			m.IsHealthy = false
			m.LastHealthCheck = time.Now()
		})

		return customerrors.CreateBackendUnavailableError(b.name, "no healthy connections")
	}

	b.updateMetrics(func(m *BackendMetrics) {
		m.IsHealthy = true
		m.LastHealthCheck = time.Now()
	})

	return nil
}

// healthCheckLoop runs periodic health checks.
func (b *Backend) healthCheckLoop(ctx context.Context) {
	defer b.wg.Done()

	ticker := time.NewTicker(b.config.HealthCheck.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-b.shutdownCh:
			return
		case <-ticker.C:
			healthCtx, cancel := context.WithTimeout(ctx, b.config.HealthCheck.Timeout)
			if err := b.Health(healthCtx); err != nil {
				b.logger.Warn("health check failed", zap.Error(err))
			}

			cancel()
		}
	}
}

// poolMaintenanceLoop cleans up idle connections.
func (b *Backend) poolMaintenanceLoop() {
	defer b.wg.Done()

	ticker := time.NewTicker(defaultTimeoutSeconds * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-b.shutdownCh:
			return
		case <-ticker.C:
			b.cleanupIdleConnections()
		}
	}
}

// cleanupIdleConnections removes idle and unhealthy connections.
func (b *Backend) cleanupIdleConnections() {
	b.poolsMu.RLock()
	defer b.poolsMu.RUnlock()

	for endpoint, pool := range b.pools {
		pool.mu.Lock()

		var activeConnections []*Connection

		for _, conn := range pool.connections {
			conn.mu.Lock()
			shouldKeep := conn.inUse ||
				(conn.healthy && time.Since(conn.lastUsed) < pool.config.IdleTimeout)

			if shouldKeep {
				activeConnections = append(activeConnections, conn)
			} else {
				// Close the connection
				if err := conn.conn.Close(); err != nil {
					b.logger.Warn("failed to close connection", zap.Error(err))
				}

				b.logger.Debug("closed idle connection", zap.String("endpoint", endpoint))
			}

			conn.mu.Unlock()
		}

		pool.connections = activeConnections
		pool.mu.Unlock()
	}
}

// Stop gracefully shuts down the WebSocket backend.
func (b *Backend) Stop(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.running {
		return nil
	}

	b.logger.Info("stopping WebSocket backend")

	// Signal shutdown
	close(b.shutdownCh)

	// Close all connections
	b.poolsMu.RLock()

	for endpoint, pool := range b.pools {
		pool.mu.Lock()

		for _, conn := range pool.connections {
			conn.mu.Lock()

			if err := conn.conn.Close(); err != nil {
				b.logger.Warn("failed to close connection during shutdown", zap.Error(err))
			}

			conn.mu.Unlock()
		}

		pool.connections = nil
		pool.mu.Unlock()
		b.logger.Debug("closed connections", zap.String("endpoint", endpoint))
	}

	b.poolsMu.RUnlock()

	// Wait for background routines
	done := make(chan struct{})

	go func() {
		b.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(defaultRetryCount * time.Second):
		b.logger.Warn("background routines did not finish in time")
	case <-ctx.Done():
		return ctx.Err()
	}

	b.running = false
	b.updateMetrics(func(m *BackendMetrics) {
		m.IsHealthy = false
	})

	b.logger.Info("WebSocket backend stopped")

	return nil
}

// GetName returns the backend name.
func (b *Backend) GetName() string {
	return b.name
}

// GetProtocol returns the backend protocol.
func (b *Backend) GetProtocol() string {
	return "websocket"
}

// GetMetrics returns current backend metrics.
func (b *Backend) GetMetrics() BackendMetrics {
	b.mu_metrics.RLock()
	defer b.mu_metrics.RUnlock()

	return b.metrics
}

// updateMetrics safely updates backend metrics.
func (b *Backend) updateMetrics(updateFn func(*BackendMetrics)) {
	b.mu_metrics.Lock()
	updateFn(&b.metrics)
	b.mu_metrics.Unlock()
}
