package direct

import (
	"net/url"
	"time"

	"github.com/actual-software/mcp-bridge/services/router/internal/constants"
	"github.com/actual-software/mcp-bridge/services/router/pkg/mcp"
	"go.uber.org/zap"
)

// WebSocketClientBuilder provides a fluent interface for building WebSocket clients.
type WebSocketClientBuilder struct {
	name      string
	serverURL string
	config    WebSocketClientConfig
	logger    *zap.Logger
}

// CreateWebSocketBuilder initializes a new WebSocket client builder.
func CreateWebSocketBuilder(name, serverURL string) *WebSocketClientBuilder {
	return &WebSocketClientBuilder{
		name:      name,
		serverURL: serverURL,
		config:    WebSocketClientConfig{},
	}
}

// WithWebSocketConfig sets the configuration.
func (b *WebSocketClientBuilder) WithWebSocketConfig(config WebSocketClientConfig) *WebSocketClientBuilder {
	b.config = config

	return b
}

// WithWebSocketLogger sets the logger.
func (b *WebSocketClientBuilder) WithWebSocketLogger(logger *zap.Logger) *WebSocketClientBuilder {
	b.logger = logger

	return b
}

// BuildWebSocket creates the WebSocket client with all configurations.
func (b *WebSocketClientBuilder) BuildWebSocket() (*WebSocketClient, error) {
	if err := b.validateWebSocketConfig(); err != nil {
		return nil, err
	}

	b.applyWebSocketDefaults()

	return b.createWebSocketClient()
}

// validateWebSocketConfig validates the WebSocket configuration.
func (b *WebSocketClientBuilder) validateWebSocketConfig() error {
	// Use serverURL if config URL is not set.
	if b.config.URL == "" {
		b.config.URL = b.serverURL
	}

	// Validate URL.
	parsedURL, err := url.Parse(b.config.URL)
	if err != nil {
		return NewConfigError(b.serverURL, "websocket", "invalid WebSocket URL", err)
	}

	if parsedURL.Scheme != "ws" && parsedURL.Scheme != "wss" {
		return NewConfigError(b.serverURL, "websocket", "URL must use ws:// or wss:// scheme", nil)
	}

	return nil
}

// applyWebSocketDefaults applies all default values.
func (b *WebSocketClientBuilder) applyWebSocketDefaults() {
	b.applyTimeoutDefaults()
	b.applyConnectionDefaults()
	b.applyPerformanceDefaults()
	b.initializeHeaders()
}

// applyTimeoutDefaults sets timeout-related defaults.
func (b *WebSocketClientBuilder) applyTimeoutDefaults() {
	if b.config.HandshakeTimeout == 0 {
		b.config.HandshakeTimeout = defaultRetryCount * time.Second
	}

	if b.config.PingInterval == 0 {
		b.config.PingInterval = defaultTimeoutSeconds * time.Second
	}

	if b.config.PongTimeout == 0 {
		b.config.PongTimeout = defaultRetryCount * time.Second
	}

	if b.config.Timeout == 0 {
		b.config.Timeout = defaultTimeoutSeconds * time.Second
	}

	if b.config.HealthCheck.Interval == 0 {
		b.config.HealthCheck.Interval = defaultTimeoutSeconds * time.Second
	}

	if b.config.HealthCheck.Timeout == 0 {
		b.config.HealthCheck.Timeout = defaultMaxConnections * time.Second
	}
}

// applyConnectionDefaults sets connection-related defaults.
func (b *WebSocketClientBuilder) applyConnectionDefaults() {
	if b.config.MaxMessageSize == 0 {
		b.config.MaxMessageSize = defaultBufferSize * defaultBufferSize // 1MB
	}

	if b.config.Connection.MaxReconnectAttempts == 0 {
		b.config.Connection.MaxReconnectAttempts = DefaultMaxRestarts
	}

	if b.config.Connection.ReconnectDelay == 0 {
		b.config.Connection.ReconnectDelay = defaultMaxConnections * time.Second
	}

	if b.config.Connection.ReadBufferSize == 0 {
		b.config.Connection.ReadBufferSize = constants.DefaultSmallBufferSize
	}

	if b.config.Connection.WriteBufferSize == 0 {
		b.config.Connection.WriteBufferSize = constants.DefaultSmallBufferSize
	}
}

// applyPerformanceDefaults sets performance optimization defaults.
func (b *WebSocketClientBuilder) applyPerformanceDefaults() {
	// Set compression and optimization flags.
	b.setPerformanceFlags()

	// Set batch processing defaults.
	b.setBatchDefaults()
}

// setPerformanceFlags sets boolean performance flags.
func (b *WebSocketClientBuilder) setPerformanceFlags() {
	if !b.config.Performance.EnableReadCompression {
		b.config.Performance.EnableReadCompression = true
	}

	// EnableWriteCompression defaults to false for lower latency.

	if !b.config.Performance.OptimizePingPong {
		b.config.Performance.OptimizePingPong = true
	}

	if !b.config.Performance.EnableMessagePooling {
		b.config.Performance.EnableMessagePooling = true
	}
}

// setBatchDefaults sets batch processing defaults.
func (b *WebSocketClientBuilder) setBatchDefaults() {
	if b.config.Performance.MessageBatchSize == 0 {
		b.config.Performance.MessageBatchSize = 0 // Disable batching by default
	}

	if b.config.Performance.WriteBatchTimeout == 0 {
		b.config.Performance.WriteBatchTimeout = time.Millisecond
	}
}

// initializeHeaders initializes the headers map.
func (b *WebSocketClientBuilder) initializeHeaders() {
	if b.config.Headers == nil {
		b.config.Headers = make(map[string]string)
	}
}

// createWebSocketClient creates the final WebSocketClient instance.
func (b *WebSocketClientBuilder) createWebSocketClient() (*WebSocketClient, error) {
	logger := b.logger
	if logger != nil {
		logger = logger.With(
			zap.String("component", "websocket_client"),
			zap.String("name", b.name),
		)
	}

	client := &WebSocketClient{
		name:       b.name,
		url:        b.config.URL,
		config:     b.config,
		logger:     logger,
		requestMap: make(map[string]chan *mcp.Response),
		shutdownCh: make(chan struct{}),
		doneCh:     make(chan struct{}),
		state:      StateDisconnected,
		metrics: ClientMetrics{
			IsHealthy: false,
		},
	}

	return client, nil
}

// EstablishWebSocketConnection is the new descriptive name for NewWebSocketClient.
func EstablishWebSocketConnection(
	name, serverURL string,
	config WebSocketClientConfig,
	logger *zap.Logger,
) (*WebSocketClient, error) {
	return CreateWebSocketBuilder(name, serverURL).
		WithWebSocketConfig(config).
		WithWebSocketLogger(logger).
		BuildWebSocket()
}

// InitializeWebSocketClient is another descriptive alternative.
func InitializeWebSocketClient(
	name, serverURL string,
	config WebSocketClientConfig,
	logger *zap.Logger,
) (*WebSocketClient, error) {
	return EstablishWebSocketConnection(name, serverURL, config, logger)
}
