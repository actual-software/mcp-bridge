package direct

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/poiley/mcp-bridge/services/router/internal/constants"
	"github.com/poiley/mcp-bridge/services/router/pkg/mcp"
	"go.uber.org/zap"
)

// WebSocketClientConfig contains configuration for WebSocket direct client.
type WebSocketClientConfig struct {
	// WebSocket URL (required).
	URL string `mapstructure:"url" yaml:"url"`

	// Headers to send with connection.
	Headers map[string]string `mapstructure:"headers" yaml:"headers"`

	// Connection timeouts.
	HandshakeTimeout time.Duration `mapstructure:"handshake_timeout" yaml:"handshake_timeout"`

	// Ping/pong configuration.
	PingInterval time.Duration `mapstructure:"ping_interval" yaml:"ping_interval"`
	PongTimeout  time.Duration `mapstructure:"pong_timeout"  yaml:"pong_timeout"`

	// Message size limits.
	MaxMessageSize int64 `mapstructure:"max_message_size" yaml:"max_message_size"`

	// Request timeout.
	Timeout time.Duration `mapstructure:"timeout" yaml:"timeout"`

	// Health check configuration.
	HealthCheck HealthCheckConfig `mapstructure:"health_check" yaml:"health_check"`

	// Connection configuration.
	Connection ConnectionConfig `mapstructure:"connection" yaml:"connection"`

	// Performance tuning.
	Performance WebSocketPerformanceConfig `mapstructure:"performance" yaml:"performance"`
}

// WebSocketPerformanceConfig contains WebSocket performance optimization settings.
type WebSocketPerformanceConfig struct {
	// Enable write compression (default: false for lower latency).
	EnableWriteCompression bool `mapstructure:"enable_write_compression" yaml:"enable_write_compression"`

	// Enable read compression (default: true for bandwidth efficiency).
	EnableReadCompression bool `mapstructure:"enable_read_compression" yaml:"enable_read_compression"`

	// Optimize ping/pong for better keepalive.
	OptimizePingPong bool `mapstructure:"optimize_ping_pong" yaml:"optimize_ping_pong"`

	// Enable message pooling to reduce GC pressure.
	EnableMessagePooling bool `mapstructure:"enable_message_pooling" yaml:"enable_message_pooling"`

	// Batch size for message writes (0 = disabled).
	MessageBatchSize int `mapstructure:"message_batch_size" yaml:"message_batch_size"`

	// Write buffer timeout for batching.
	WriteBatchTimeout time.Duration `mapstructure:"write_batch_timeout" yaml:"write_batch_timeout"`
}

// ConnectionConfig contains WebSocket connection settings.
type ConnectionConfig struct {
	MaxReconnectAttempts int           `mapstructure:"max_reconnect_attempts" yaml:"max_reconnect_attempts"`
	ReconnectDelay       time.Duration `mapstructure:"reconnect_delay"        yaml:"reconnect_delay"`
	ReadBufferSize       int           `mapstructure:"read_buffer_size"       yaml:"read_buffer_size"`
	WriteBufferSize      int           `mapstructure:"write_buffer_size"      yaml:"write_buffer_size"`
	EnableCompression    bool          `mapstructure:"enable_compression"     yaml:"enable_compression"`
}

// WebSocketClient implements DirectClient for WebSocket-based MCP servers.
type WebSocketClient struct {
	name   string
	url    string
	config WebSocketClientConfig
	logger *zap.Logger

	// WebSocket connection.
	conn   *websocket.Conn
	connMu sync.RWMutex

	// Write synchronization - WebSocket concurrent writes are not safe.
	writeMu sync.Mutex

	// State management.
	mu                sync.RWMutex
	state             ConnectionState
	startTime         time.Time
	reconnectAttempts int

	// Request correlation.
	requestMap   map[string]chan *mcp.Response
	requestMapMu sync.RWMutex
	requestID    uint64

	// Metrics.
	metrics   ClientMetrics
	metricsMu sync.RWMutex

	// Shutdown coordination.
	shutdownCh chan struct{}
	doneCh     chan struct{}
	wg         sync.WaitGroup
}

// NewWebSocketClient creates a new WebSocket direct client.
// Deprecated: Use EstablishWebSocketConnection or InitializeWebSocketClient for better naming.
func NewWebSocketClient(
	name, serverURL string,
	config WebSocketClientConfig,
	logger *zap.Logger,
) (*WebSocketClient, error) {
	return EstablishWebSocketConnection(name, serverURL, config, logger)
}
// Connect establishes connection to the WebSocket MCP server.
func (c *WebSocketClient) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.state == StateConnected || c.state == StateHealthy {
		return NewConnectionError(c.url, "websocket", "client already connected", ErrClientAlreadyConnected)
	}

	c.state = StateConnecting

	if err := c.connect(ctx); err != nil {
		c.state = StateError

		return NewConnectionError(c.url, "websocket", "failed to establish WebSocket connection", err)
	}

	c.state = StateConnected
	c.startTime = time.Now()
	c.reconnectAttempts = 0
	c.updateMetrics(func(m *ClientMetrics) {
		m.ConnectionTime = time.Now()
		m.IsHealthy = true
	})

	// Start background routines.
	c.wg.Add(1)

	go c.readMessages() 

	c.wg.Add(1)

	go c.pingLoop()

	if c.config.HealthCheck.Enabled {
		c.wg.Add(1)

		go c.healthCheckLoop(ctx)
	}

	c.logger.Info("WebSocket client connected successfully",
		zap.String("url", c.config.URL))

	return nil
}

// connect establishes the WebSocket connection.
func (c *WebSocketClient) connect(ctx context.Context) error {
	dialer := c.createDialer()
	headers := c.prepareHeaders()
	
	conn, _, err := c.establishConnection(ctx, dialer, headers)
	if err != nil {
		return err
	}
	
	c.configureConnectionHandlers(conn)
	
	c.connMu.Lock()
	c.conn = conn
	c.connMu.Unlock()
	
	return nil
}

func (c *WebSocketClient) createDialer() *websocket.Dialer {
	return &websocket.Dialer{
		HandshakeTimeout:  c.config.HandshakeTimeout,
		ReadBufferSize:    c.config.Connection.ReadBufferSize,
		WriteBufferSize:   c.config.Connection.WriteBufferSize,
		EnableCompression: c.config.Connection.EnableCompression,
	}
}

func (c *WebSocketClient) prepareHeaders() http.Header {
	headers := http.Header{}
	for key, value := range c.config.Headers {
		headers.Set(key, value)
	}
	return headers
}

func (c *WebSocketClient) establishConnection(
	ctx context.Context,
	dialer *websocket.Dialer,
	headers http.Header,
) (*websocket.Conn, *http.Response, error) {
	conn, resp, err := dialer.DialContext(ctx, c.config.URL, headers)
	
	if resp != nil {
		defer func() {
			if err := resp.Body.Close(); err != nil {
				c.logger.Debug("Failed to close response body", zap.Error(err))
			}
		}()
	}

	if err != nil {
		if resp != nil {
			return nil, resp, fmt.Errorf("WebSocket connection failed with status %d: %w", resp.StatusCode, err)
		}
		return nil, resp, fmt.Errorf("WebSocket connection failed: %w", err)
	}

	return conn, resp, nil
}

func (c *WebSocketClient) configureConnectionHandlers(conn *websocket.Conn) {
	// Set connection limits.
	conn.SetReadLimit(c.config.MaxMessageSize)

	// Set pong handler.
	conn.SetPongHandler(func(appData string) error {
		c.logger.Debug("received pong", zap.String("data", appData))
		return conn.SetReadDeadline(time.Now().Add(c.config.PongTimeout))
	})

	// Set close handler.
	conn.SetCloseHandler(func(code int, text string) error {
		c.logger.Info("WebSocket connection closed",
			zap.Int("code", code),
			zap.String("text", text))

		c.mu.Lock()
		if c.state != StateClosing && c.state != StateClosed {
			c.state = StateDisconnected
		}
		c.mu.Unlock()

		c.updateMetrics(func(m *ClientMetrics) {
			m.IsHealthy = false
		})

		return nil
	})
}

// SendRequest sends an MCP request and returns the response.
func (c *WebSocketClient) SendRequest(ctx context.Context, req *mcp.Request) (*mcp.Response, error) {
	handler := CreateWebSocketRequestHandler(c)

	return handler.ProcessRequest(ctx, req)
}

// readMessages continuously reads messages from WebSocket.
func (c *WebSocketClient) readMessages() {
	reader := CreateWebSocketMessageReader(c)
	reader.ReadMessageLoop()
}

// pingLoop sends periodic ping frames.
func (c *WebSocketClient) pingLoop() {
	defer c.wg.Done()

	ticker := time.NewTicker(c.config.PingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.shutdownCh:
			return
		case <-ticker.C:
			c.connMu.RLock()
			conn := c.conn
			c.connMu.RUnlock()

			if conn == nil {
				return
			}

			c.mu.RLock()
			isHealthy := c.state == StateConnected || c.state == StateHealthy
			c.mu.RUnlock()

			if isHealthy {
				// Protect WebSocket writes with mutex - concurrent writes are not safe.
				c.writeMu.Lock()

				err := conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(defaultRetryCount*time.Second))

				c.writeMu.Unlock()

				if err != nil {
					c.logger.Warn("ping failed", zap.Error(err))
					c.mu.Lock()
					c.state = StateUnhealthy
					c.mu.Unlock()
					c.updateMetrics(func(m *ClientMetrics) {
						m.IsHealthy = false
					})
				}
			}
		}
	}
}

// Health checks if the client connection is healthy.
func (c *WebSocketClient) Health(ctx context.Context) error {
	c.mu.RLock()
	state := c.state
	c.mu.RUnlock()

	if state != StateConnected && state != StateHealthy {
		c.updateMetrics(func(m *ClientMetrics) {
			m.IsHealthy = false
			m.LastHealthCheck = time.Now()
		})

		return NewHealthError(c.url, "websocket", "client not connected", ErrClientNotConnected)
	}

	c.connMu.RLock()
	conn := c.conn
	c.connMu.RUnlock()

	if conn == nil {
		c.updateMetrics(func(m *ClientMetrics) {
			m.IsHealthy = false
			m.LastHealthCheck = time.Now()
		})

		return NewHealthError(c.url, "websocket", "connection is nil", ErrClientNotConnected)
	}

	// Optionally send a ping to verify connectivity.
	if c.config.HealthCheck.Enabled {
		pingReq := &mcp.Request{
			JSONRPC: "2.0",
			Method:  "ping",
			ID:      fmt.Sprintf("health-%d", time.Now().UnixNano()),
		}

		healthCtx, cancel := context.WithTimeout(ctx, c.config.HealthCheck.Timeout)
		defer cancel()

		_, err := c.SendRequest(healthCtx, pingReq)
		if err != nil {
			c.mu.Lock()
			c.state = StateUnhealthy
			c.mu.Unlock()
			c.updateMetrics(func(m *ClientMetrics) {
				m.IsHealthy = false
				m.LastHealthCheck = time.Now()
			})

			return NewHealthError(c.url, "websocket", "health check ping failed", err)
		}
	}

	c.mu.Lock()
	c.state = StateHealthy
	c.mu.Unlock()
	c.updateMetrics(func(m *ClientMetrics) {
		m.IsHealthy = true
		m.LastHealthCheck = time.Now()
	})

	return nil
}

// healthCheckLoop runs periodic health checks.
func (c *WebSocketClient) healthCheckLoop(parentCtx context.Context) {
	defer c.wg.Done()

	ticker := time.NewTicker(c.config.HealthCheck.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-parentCtx.Done():
			return
		case <-c.shutdownCh:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(parentCtx, c.config.HealthCheck.Timeout)
			if err := c.Health(ctx); err != nil {
				c.logger.Warn("health check failed", zap.Error(err))
				// Consider reconnection if unhealthy.
				if c.shouldReconnect() {
					go c.reconnect() 
				}
			}

			cancel()
		}
	}
}

// shouldReconnect determines if reconnection should be attempted.
func (c *WebSocketClient) shouldReconnect() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.state == StateDisconnected &&
		c.reconnectAttempts < c.config.Connection.MaxReconnectAttempts
}

// reconnect attempts to reconnect the WebSocket.
func (c *WebSocketClient) reconnect() {
	c.mu.Lock()
	c.reconnectAttempts++
	attempts := c.reconnectAttempts
	c.mu.Unlock()

	c.logger.Info("attempting to reconnect WebSocket",
		zap.Int("attempt", attempts),
		zap.Int("max_attempts", c.config.Connection.MaxReconnectAttempts))

	// Wait before reconnecting.
	time.Sleep(c.config.Connection.ReconnectDelay)

	ctx, cancel := context.WithTimeout(context.Background(), c.config.HandshakeTimeout)
	defer cancel()

	c.mu.Lock()
	c.state = StateConnecting
	c.mu.Unlock()

	if err := c.connect(ctx); err != nil {
		c.logger.Error("reconnection failed", zap.Error(err), zap.Int("attempt", attempts))
		c.mu.Lock()
		c.state = StateError
		c.mu.Unlock()

		return
	}

	c.mu.Lock()
	c.state = StateConnected
	c.reconnectAttempts = 0 // Reset on successful reconnection
	c.mu.Unlock()

	c.updateMetrics(func(m *ClientMetrics) {
		m.ConnectionTime = time.Now()
		m.IsHealthy = true
	})

	c.logger.Info("WebSocket reconnected successfully")
}

// Close gracefully closes the client connection.
func (c *WebSocketClient) Close(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.state == StateClosed || c.state == StateClosing {
		return nil
	}

	c.state = StateClosing
	c.logger.Info("closing WebSocket client")

	c.signalShutdown()
	c.closeConnection()
	
	if err := c.waitForShutdown(ctx); err != nil {
		return err
	}

	c.finalizeClose()
	return nil
}

func (c *WebSocketClient) signalShutdown() {
	// Signal shutdown (protect against double-close).
	select {
	case <-c.shutdownCh:
		// Channel already closed.
	default:
		close(c.shutdownCh)
	}
}

func (c *WebSocketClient) closeConnection() {
	// Close WebSocket connection.
	c.connMu.Lock()
	defer c.connMu.Unlock()

	if c.conn != nil {
		// Send close frame - protect with write mutex.
		c.writeMu.Lock()
		err := c.conn.WriteControl(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, "client shutdown"),
			time.Now().Add(defaultMaxConnections*time.Second))
		c.writeMu.Unlock()

		if err != nil {
			c.logger.Warn("failed to send close frame", zap.Error(err))
		}

		// Close connection.
		_ = c.conn.Close()
		c.conn = nil
	}
}

func (c *WebSocketClient) waitForShutdown(ctx context.Context) error {
	// Wait for background routines with a shorter timeout for tests.
	finished := make(chan struct{})

	go func() {
		c.wg.Wait()
		close(finished)
	}()

	select {
	case <-finished:
		c.logger.Debug("all background routines finished")
	case <-time.After(constants.GracefulShutdownTimeout):
		c.logger.Debug("background routines did not finish quickly, continuing with close")
	case <-ctx.Done():
		c.state = StateClosed
		return ctx.Err()
	}
	
	return nil
}

func (c *WebSocketClient) finalizeClose() {
	c.state = StateClosed
	c.updateMetrics(func(m *ClientMetrics) {
		m.IsHealthy = false
	})
	c.logger.Info("WebSocket client closed")
}

// GetName returns the client name.
func (c *WebSocketClient) GetName() string {
	return c.name
}

// GetProtocol returns the protocol type.
func (c *WebSocketClient) GetProtocol() string {
	return "websocket"
}

// GetMetrics returns current client metrics.
func (c *WebSocketClient) GetMetrics() ClientMetrics {
	c.metricsMu.RLock()
	defer c.metricsMu.RUnlock()

	return c.metrics
}

// updateMetrics safely updates client metrics.
func (c *WebSocketClient) updateMetrics(updateFn func(*ClientMetrics)) {
	c.metricsMu.Lock()
	updateFn(&c.metrics)
	c.metricsMu.Unlock()
}

// GetState returns the current connection state.
func (c *WebSocketClient) GetState() ConnectionState {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.state
}

// GetStatus returns detailed client status.
func (c *WebSocketClient) GetStatus() DirectClientStatus {
	c.mu.RLock()
	state := c.state
	c.mu.RUnlock()

	metrics := c.GetMetrics()

	// Convert ClientMetrics to DetailedClientMetrics.
	detailedMetrics := DetailedClientMetrics{
		RequestCount:    metrics.RequestCount,
		ErrorCount:      metrics.ErrorCount,
		AverageLatency:  metrics.AverageLatency,
		LastHealthCheck: metrics.LastHealthCheck,
		IsHealthy:       metrics.IsHealthy,
		ConnectionTime:  metrics.ConnectionTime,
		LastUsed:        metrics.LastUsed,

		// Set defaults for extended metrics.
		Protocol:          "websocket",
		ServerURL:         c.url,
		ConnectionState:   state,
		ErrorsByType:      make(map[string]uint64),
		HealthHistory:     []HealthCheckResult{},
		CreatedAt:         metrics.ConnectionTime,
		LastMetricsUpdate: time.Now(),
	}

	status := DirectClientStatus{
		Name:            c.name,
		URL:             c.url,
		Protocol:        "websocket",
		State:           state,
		LastConnected:   metrics.ConnectionTime,
		LastHealthCheck: metrics.LastHealthCheck,
		Metrics:         detailedMetrics,
		Configuration:   make(map[string]interface{}),
		RuntimeInfo: ClientRuntimeInfo{
			ConnectionID: fmt.Sprintf("websocket-%s-%d", c.name, metrics.ConnectionTime.Unix()),
			ResourceUsage: ResourceUsage{
				LastUpdated: time.Now(),
			},
		},
	}

	return status
}

// GetURL returns the WebSocket URL.
func (c *WebSocketClient) GetURL() string {
	return c.url
}
