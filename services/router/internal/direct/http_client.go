package direct

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/actual-software/mcp-bridge/services/router/pkg/mcp"
	"go.uber.org/zap"
)

// HTTPClientConfig contains configuration for HTTP direct client.
type HTTPClientConfig struct {
	// HTTP URL (required).
	URL string `mapstructure:"url" yaml:"url"`

	// HTTP method to use for requests (default: POST).
	Method string `mapstructure:"method" yaml:"method"`

	// Headers to send with requests.
	Headers map[string]string `mapstructure:"headers" yaml:"headers"`

	// Request timeout.
	Timeout time.Duration `mapstructure:"timeout" yaml:"timeout"`

	// HTTP client configuration.
	Client HTTPTransportConfig `mapstructure:"client" yaml:"client"`

	// Health check configuration.
	HealthCheck HealthCheckConfig `mapstructure:"health_check" yaml:"health_check"`

	// Performance tuning.
	Performance HTTPPerformanceConfig `mapstructure:"performance" yaml:"performance"`
}

// HTTPPerformanceConfig contains HTTP performance optimization settings.
type HTTPPerformanceConfig struct {
	// Enable request/response compression (gzip/deflate).
	EnableCompression bool `mapstructure:"enable_compression" yaml:"enable_compression"`

	// Request body compression threshold (bytes).
	CompressionThreshold int `mapstructure:"compression_threshold" yaml:"compression_threshold"`

	// Enable HTTP/2 (default: true).
	EnableHTTP2 bool `mapstructure:"enable_http2" yaml:"enable_http2"`

	// Connection pooling optimizations.
	MaxConnsPerHost int `mapstructure:"max_conns_per_host" yaml:"max_conns_per_host"`

	// Enable request pipelining optimization.
	EnablePipelining bool `mapstructure:"enable_pipelining" yaml:"enable_pipelining"`

	// Reuse JSON encoders/decoders for better performance.
	ReuseEncoders bool `mapstructure:"reuse_encoders" yaml:"reuse_encoders"`

	// Response body buffer size.
	ResponseBufferSize int `mapstructure:"response_buffer_size" yaml:"response_buffer_size"`
}

// HTTPTransportConfig contains HTTP transport settings.
type HTTPTransportConfig struct {
	MaxIdleConns        int           `mapstructure:"max_idle_conns"          yaml:"max_idle_conns"`
	MaxIdleConnsPerHost int           `mapstructure:"max_idle_conns_per_host" yaml:"max_idle_conns_per_host"`
	IdleConnTimeout     time.Duration `mapstructure:"idle_conn_timeout"       yaml:"idle_conn_timeout"`
	DisableKeepAlives   bool          `mapstructure:"disable_keep_alives"     yaml:"disable_keep_alives"`
	FollowRedirects     bool          `mapstructure:"follow_redirects"        yaml:"follow_redirects"`
	MaxRedirects        int           `mapstructure:"max_redirects"           yaml:"max_redirects"`
	UserAgent           string        `mapstructure:"user_agent"              yaml:"user_agent"`
}

// HTTPClient implements DirectClient for HTTP-based MCP servers.
type HTTPClient struct {
	name   string
	url    string
	config HTTPClientConfig
	logger *zap.Logger

	// HTTP client.
	client *http.Client

	// Memory optimization.
	memoryOptimizer *MemoryOptimizer

	// State management.
	mu        sync.RWMutex
	state     ConnectionState
	startTime time.Time

	// Request correlation.
	requestID uint64

	// Metrics.
	metrics   ClientMetrics
	metricsMu sync.RWMutex

	// Shutdown coordination.
	shutdownCh chan struct{}
	doneCh     chan struct{}
	wg         sync.WaitGroup
}

// NewHTTPClient creates a new HTTP direct client.
func NewHTTPClient(name, serverURL string, config HTTPClientConfig, logger *zap.Logger) (*HTTPClient, error) {
	return NewHTTPClientWithMemoryOptimizer(name, serverURL, config, logger, nil)
}

// NewHTTPClientWithMemoryOptimizer creates a new HTTP direct client with memory optimization.
// Deprecated: Use CreateOptimizedHTTPClient or CreateHTTPClientBuilder instead.
func NewHTTPClientWithMemoryOptimizer(
	name,
	serverURL string,
	config HTTPClientConfig,
	logger *zap.Logger,
	memoryOptimizer *MemoryOptimizer,
) (*HTTPClient, error) {
	return CreateOptimizedHTTPClient(name, serverURL, config, logger, memoryOptimizer)
}

// Connect establishes connection to the HTTP MCP server.
func (c *HTTPClient) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.state == StateConnected || c.state == StateHealthy {
		return NewConnectionError(c.url, "http", "client already connected", ErrClientAlreadyConnected)
	}

	c.state = StateConnecting

	if err := c.connect(ctx); err != nil {
		c.state = StateError

		return NewConnectionError(c.url, "http", "failed to establish HTTP connection", err)
	}

	c.state = StateConnected
	c.startTime = time.Now()
	c.updateMetrics(func(m *ClientMetrics) {
		m.ConnectionTime = time.Now()
		m.IsHealthy = true
	})

	// Start health check routine if enabled.
	if c.config.HealthCheck.Enabled {
		c.wg.Add(1)

		go c.healthCheckLoop(ctx)
	}

	c.logger.Info("HTTP client connected successfully",
		zap.String("url", c.config.URL),
		zap.String("method", c.config.Method))

	return nil
}

// connect sets up the HTTP client.
func (c *HTTPClient) connect(ctx context.Context) error {
	// Create HTTP transport with configuration.
	transport := &http.Transport{
		MaxIdleConns:        c.config.Client.MaxIdleConns,
		MaxIdleConnsPerHost: c.config.Client.MaxIdleConnsPerHost,
		IdleConnTimeout:     c.config.Client.IdleConnTimeout,
		DisableKeepAlives:   c.config.Client.DisableKeepAlives,
	}

	// Create HTTP client.
	client := &http.Client{
		Transport: transport,
		Timeout:   c.config.Timeout,
	}

	// Configure redirect policy.
	if !c.config.Client.FollowRedirects {
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
	} else if c.config.Client.MaxRedirects > 0 {
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			if len(via) >= c.config.Client.MaxRedirects {
				return fmt.Errorf("stopped after %d redirects", c.config.Client.MaxRedirects)
			}

			return nil
		}
	}

	c.client = client

	// Test connectivity with a simple health check request.
	if err := c.testConnectivity(ctx); err != nil {
		return fmt.Errorf("connectivity test failed: %w", err)
	}

	return nil
}

// testConnectivity performs a simple connectivity test.
func (c *HTTPClient) testConnectivity(ctx context.Context) error {
	// Create a simple ping request.
	pingReq := &mcp.Request{
		JSONRPC: "2.0",
		Method:  "ping",
		ID:      "connectivity-test",
	}

	reqBody, err := json.Marshal(pingReq)
	if err != nil {
		return fmt.Errorf("failed to marshal ping request: %w", err)
	}

	// Create HTTP request.
	httpReq, err := http.NewRequestWithContext(ctx, c.config.Method, c.config.URL, bytes.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set headers.
	for key, value := range c.config.Headers {
		httpReq.Header.Set(key, value)
	}

	httpReq.Header.Set("User-Agent", c.config.Client.UserAgent)

	// Send request with a short timeout for connectivity test.
	testCtx, cancel := context.WithTimeout(ctx, defaultMaxConnections*time.Second)
	defer cancel()

	httpReq = httpReq.WithContext(testCtx)

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("HTTP request failed: %w", err)
	}

	defer func() {
		if err := resp.Body.Close(); err != nil {
			c.logger.Debug("Failed to close response body", zap.Error(err))
		}
	}()

	// Accept any response as a sign of connectivity.
	// The actual ping may fail if the server doesn't support it,
	// but we can still connect for other requests
	c.logger.Debug("connectivity test completed",
		zap.String("url", c.config.URL),
		zap.Int("status_code", resp.StatusCode))

	return nil
}

// SendRequest sends an MCP request and returns the response.
func (c *HTTPClient) SendRequest(ctx context.Context, req *mcp.Request) (*mcp.Response, error) {
	processor := CreateHTTPRequestProcessor(c)

	return processor.ProcessRequest(ctx, req)
}

// Health checks if the client connection is healthy.
func (c *HTTPClient) Health(ctx context.Context) error {
	c.mu.RLock()
	state := c.state
	c.mu.RUnlock()

	if state != StateConnected && state != StateHealthy {
		c.updateMetrics(func(m *ClientMetrics) {
			m.IsHealthy = false
			m.LastHealthCheck = time.Now()
		})

		return NewHealthError(c.url, "http", "client not connected", ErrClientNotConnected)
	}

	if c.client == nil {
		c.updateMetrics(func(m *ClientMetrics) {
			m.IsHealthy = false
			m.LastHealthCheck = time.Now()
		})

		return NewHealthError(c.url, "http", "HTTP client is nil", ErrClientNotConnected)
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

			return NewHealthError(c.url, "http", "health check ping failed", err)
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
func (c *HTTPClient) healthCheckLoop(ctx context.Context) {
	defer c.wg.Done()

	ticker := time.NewTicker(c.config.HealthCheck.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-c.shutdownCh:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			healthCtx, cancel := context.WithTimeout(ctx, c.config.HealthCheck.Timeout)
			if err := c.Health(healthCtx); err != nil {
				c.logger.Warn("health check failed", zap.Error(err))
			}

			cancel()
		}
	}
}

// Close gracefully closes the client connection.
func (c *HTTPClient) Close(ctx context.Context) error {
	closer := CreateHTTPClientCloser(c)

	return closer.CloseClient(ctx)
}

// GetName returns the client name.
func (c *HTTPClient) GetName() string {
	return c.name
}

// GetProtocol returns the protocol type.
func (c *HTTPClient) GetProtocol() string {
	return SchemeHTTP
}

// GetMetrics returns current client metrics.
func (c *HTTPClient) GetMetrics() ClientMetrics {
	c.metricsMu.RLock()
	defer c.metricsMu.RUnlock()

	return c.metrics
}

// updateMetrics safely updates client metrics.
func (c *HTTPClient) updateMetrics(updateFn func(*ClientMetrics)) {
	c.metricsMu.Lock()
	updateFn(&c.metrics)
	c.metricsMu.Unlock()
}

// GetState returns the current connection state.
func (c *HTTPClient) GetState() ConnectionState {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.state
}

// GetStatus returns detailed client status.
func (c *HTTPClient) GetStatus() DirectClientStatus {
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
		Protocol:          "http",
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
		Protocol:        "http",
		State:           state,
		LastConnected:   metrics.ConnectionTime,
		LastHealthCheck: metrics.LastHealthCheck,
		Metrics:         detailedMetrics,
		Configuration:   make(map[string]interface{}),
		RuntimeInfo: ClientRuntimeInfo{
			ConnectionID: fmt.Sprintf("http-%s-%d", c.name, metrics.ConnectionTime.Unix()),
			ResourceUsage: ResourceUsage{
				LastUpdated: time.Now(),
			},
		},
	}

	return status
}

// GetURL returns the HTTP URL.
func (c *HTTPClient) GetURL() string {
	return c.url
}
