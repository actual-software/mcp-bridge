package direct

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/actual-software/mcp-bridge/services/router/internal/constants"
	"github.com/actual-software/mcp-bridge/services/router/pkg/mcp"
	"go.uber.org/zap"
)

// SSEClientBuilder provides a fluent interface for building SSE clients.
type SSEClientBuilder struct {
	name      string
	serverURL string
	config    SSEClientConfig
	logger    *zap.Logger
}

// CreateSSEClientBuilder initializes a new SSE client builder.
func CreateSSEClientBuilder(name, serverURL string) *SSEClientBuilder {
	return &SSEClientBuilder{
		name:      name,
		serverURL: serverURL,
		config:    SSEClientConfig{},
	}
}

// WithSSEConfig sets the configuration.
func (b *SSEClientBuilder) WithSSEConfig(config SSEClientConfig) *SSEClientBuilder {
	b.config = config

	return b
}

// WithSSELogger sets the logger.
func (b *SSEClientBuilder) WithSSELogger(logger *zap.Logger) *SSEClientBuilder {
	b.logger = logger

	return b
}

// BuildSSE creates the SSE client with all configurations.
func (b *SSEClientBuilder) BuildSSE() (*SSEClient, error) {
	if err := b.validateSSEConfig(); err != nil {
		return nil, err
	}

	b.applySSEDefaults()

	httpClient := b.buildSSEHTTPClient()

	return b.createSSEClient(httpClient)
}

// validateSSEConfig validates the SSE configuration.
func (b *SSEClientBuilder) validateSSEConfig() error {
	// Use serverURL if config URL is not set.
	if b.config.URL == "" {
		b.config.URL = b.serverURL
	}

	// Validate URL.
	parsedURL, err := url.Parse(b.config.URL)
	if err != nil {
		return NewConfigError(b.serverURL, "sse", "invalid SSE URL", err)
	}

	if parsedURL.Scheme != SchemeHTTP && parsedURL.Scheme != SchemeHTTPS {
		return NewConfigError(b.serverURL, "sse", "URL must use http:// or https:// scheme", nil)
	}

	return nil
}

// applySSEDefaults applies all default values.
func (b *SSEClientBuilder) applySSEDefaults() {
	b.applySSEBasicDefaults()
	b.applySSEClientDefaults()
	b.applySSEPerformanceDefaults()
	b.initializeSSEHeaders()
}

// applySSEBasicDefaults sets basic SSE configuration defaults.
func (b *SSEClientBuilder) applySSEBasicDefaults() {
	if b.config.Method == "" {
		b.config.Method = "POST"
	}

	if b.config.RequestTimeout == 0 {
		b.config.RequestTimeout = defaultTimeoutSeconds * time.Second
	}

	if b.config.StreamTimeout == 0 {
		b.config.StreamTimeout = constants.LongStreamTimeout
	}

	if b.config.BufferSize == 0 {
		b.config.BufferSize = constants.DefaultSmallBufferSize
	}

	if b.config.HealthCheck.Interval == 0 {
		b.config.HealthCheck.Interval = defaultTimeoutSeconds * time.Second
	}

	if b.config.HealthCheck.Timeout == 0 {
		b.config.HealthCheck.Timeout = defaultMaxConnections * time.Second
	}
}

// applySSEClientDefaults sets SSE client transport defaults.
func (b *SSEClientBuilder) applySSEClientDefaults() {
	if b.config.Client.MaxIdleConns == 0 {
		b.config.Client.MaxIdleConns = DefaultSampleWindowSize
	}

	if b.config.Client.MaxIdleConnsPerHost == 0 {
		b.config.Client.MaxIdleConnsPerHost = DefaultMaxIdleConnsPerHost
	}

	if b.config.Client.IdleConnTimeout == 0 {
		b.config.Client.IdleConnTimeout = constants.DefaultIdleConnTimeout
	}

	if b.config.Client.MaxRedirects == 0 {
		b.config.Client.MaxRedirects = DefaultMaxRestarts
	}

	if b.config.Client.UserAgent == "" {
		b.config.Client.UserAgent = "MCP-Router-SSE-Client/1.0"
	}
}

// applySSEPerformanceDefaults sets SSE performance optimization defaults.
func (b *SSEClientBuilder) applySSEPerformanceDefaults() {
	if b.config.Performance.StreamBufferSize == 0 {
		b.config.Performance.StreamBufferSize = StdioBufferSizeKB * KilobyteFactor
	}

	if b.config.Performance.RequestBufferSize == 0 {
		b.config.Performance.RequestBufferSize = HTTPBufferSizeKB * KilobyteFactor
	}

	// Set boolean defaults explicitly.
	b.setSSEBooleanDefaults()

	if b.config.Performance.ConnectionPoolSize == 0 {
		b.config.Performance.ConnectionPoolSize = defaultMaxConnections
	}

	if b.config.Performance.BatchTimeout == 0 {
		b.config.Performance.BatchTimeout = DefaultSampleWindowSize * time.Millisecond
	}
}

// setSSEBooleanDefaults sets boolean performance flags.
func (b *SSEClientBuilder) setSSEBooleanDefaults() {
	if !b.config.Performance.ReuseConnections {
		b.config.Performance.ReuseConnections = true
	}

	if !b.config.Performance.EnableCompression {
		b.config.Performance.EnableCompression = true
	}

	if !b.config.Performance.FastReconnect {
		b.config.Performance.FastReconnect = true
	}

	// Event batching defaults to false for lower latency.
	if !b.config.Performance.EnableEventBatching {
		b.config.Performance.EnableEventBatching = false
	}
}

// initializeSSEHeaders initializes and sets default headers.
func (b *SSEClientBuilder) initializeSSEHeaders() {
	if b.config.Headers == nil {
		b.config.Headers = make(map[string]string)
	}

	// Set default headers for SSE.
	sseHeaders := map[string]string{
		"Accept":        "text/event-stream",
		"Cache-Control": "no-cache",
		"Connection":    "keep-alive",
	}

	for key, value := range sseHeaders {
		if _, exists := b.config.Headers[key]; !exists {
			b.config.Headers[key] = value
		}
	}

	// Add user agent if not present.
	if _, exists := b.config.Headers["User-Agent"]; !exists {
		b.config.Headers["User-Agent"] = b.config.Client.UserAgent
	}
}

// buildSSEHTTPClient creates the HTTP client for SSE.
func (b *SSEClientBuilder) buildSSEHTTPClient() *http.Client {
	transport := &http.Transport{
		MaxIdleConns:        b.config.Client.MaxIdleConns,
		MaxIdleConnsPerHost: b.config.Client.MaxIdleConnsPerHost,
		IdleConnTimeout:     b.config.Client.IdleConnTimeout,
		DisableCompression:  !b.config.Performance.EnableCompression,
	}

	// Proxy configuration would go here if supported.

	client := &http.Client{
		Transport: transport,
		Timeout:   0, // SSE requires no timeout for long-lived connections
	}

	// Configure redirect policy.
	b.configureRedirectPolicy(client)

	return client
}

// configureRedirectPolicy sets up redirect handling for the HTTP client.
func (b *SSEClientBuilder) configureRedirectPolicy(client *http.Client) {
	if !b.config.Client.FollowRedirects {
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
	} else if b.config.Client.MaxRedirects > 0 {
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			if len(via) >= b.config.Client.MaxRedirects {
				return fmt.Errorf("too many redirects (%d)", len(via))
			}

			return nil
		}
	}
}

// createSSEClient creates the final SSEClient instance.
func (b *SSEClientBuilder) createSSEClient(httpClient *http.Client) (*SSEClient, error) {
	client := &SSEClient{
		name:            b.name,
		url:             b.config.URL,
		config:          b.config,
		logger:          b.logger,
		client:          httpClient,
		state:           StateDisconnected,
		startTime:       time.Now(),
		pendingRequests: make(map[string]chan *mcp.Response),
		metrics:         ClientMetrics{},
		shutdownCh:      make(chan struct{}),
		doneCh:          make(chan struct{}),
	}

	// Start health check if enabled.
	if b.config.HealthCheck.Enabled {
		client.wg.Add(1)

		// Use background context for health check loop started during client creation
		go client.healthCheckLoop(context.Background())
	}

	return client, nil
}

// EstablishSSEConnection is the new descriptive name for NewSSEClient.
func EstablishSSEConnection(name, serverURL string, config SSEClientConfig, logger *zap.Logger) (*SSEClient, error) {
	return CreateSSEClientBuilder(name, serverURL).
		WithSSEConfig(config).
		WithSSELogger(logger).
		BuildSSE()
}

// InitializeSSEClient is another descriptive alternative.
func InitializeSSEClient(name, serverURL string, config SSEClientConfig, logger *zap.Logger) (*SSEClient, error) {
	return EstablishSSEConnection(name, serverURL, config, logger)
}
