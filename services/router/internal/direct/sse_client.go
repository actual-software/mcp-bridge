package direct

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/actual-software/mcp-bridge/services/router/pkg/mcp"
	"go.uber.org/zap"
)

// Buffer size constants.
const (
	BufferSize64KB       = 64
	BufferSize32KB       = 32
	DefaultRetryAttempts = 10
	RetryMultiplier      = 2
)

// SSEClientConfig contains configuration for SSE direct client.
type SSEClientConfig struct {
	// SSE URL (required).
	URL string `mapstructure:"url" yaml:"url"`

	// HTTP method to use for requests (default: POST).
	Method string `mapstructure:"method" yaml:"method"`

	// Headers to send with requests.
	Headers map[string]string `mapstructure:"headers" yaml:"headers"`

	// Request timeout.
	RequestTimeout time.Duration `mapstructure:"request_timeout" yaml:"request_timeout"`

	// Stream timeout (how long to wait for SSE events).
	StreamTimeout time.Duration `mapstructure:"stream_timeout" yaml:"stream_timeout"`

	// Buffer size for SSE event reading.
	BufferSize int `mapstructure:"buffer_size" yaml:"buffer_size"`

	// HTTP client configuration.
	Client SSETransportConfig `mapstructure:"client" yaml:"client"`

	// Health check configuration.
	HealthCheck HealthCheckConfig `mapstructure:"health_check" yaml:"health_check"`

	// Performance tuning.
	Performance SSEPerformanceConfig `mapstructure:"performance" yaml:"performance"`
}

// SSETransportConfig contains SSE transport settings.
type SSETransportConfig struct {
	MaxIdleConns        int           `mapstructure:"max_idle_conns"          yaml:"max_idle_conns"`
	MaxIdleConnsPerHost int           `mapstructure:"max_idle_conns_per_host" yaml:"max_idle_conns_per_host"`
	IdleConnTimeout     time.Duration `mapstructure:"idle_conn_timeout"       yaml:"idle_conn_timeout"`
	DisableKeepAlives   bool          `mapstructure:"disable_keep_alives"     yaml:"disable_keep_alives"`
	FollowRedirects     bool          `mapstructure:"follow_redirects"        yaml:"follow_redirects"`
	MaxRedirects        int           `mapstructure:"max_redirects"           yaml:"max_redirects"`
	UserAgent           string        `mapstructure:"user_agent"              yaml:"user_agent"`
}

// SSEPerformanceConfig contains SSE performance optimization settings.
type SSEPerformanceConfig struct {
	// Stream read buffer size for better throughput (default: 64KB).
	StreamBufferSize int `mapstructure:"stream_buffer_size" yaml:"stream_buffer_size"`

	// HTTP request buffer size (default: 32KB).
	RequestBufferSize int `mapstructure:"request_buffer_size" yaml:"request_buffer_size"`

	// Enable connection reuse for HTTP requests.
	ReuseConnections bool `mapstructure:"reuse_connections" yaml:"reuse_connections"`

	// Enable request/response compression.
	EnableCompression bool `mapstructure:"enable_compression" yaml:"enable_compression"`

	// Reconnection strategy optimization.
	FastReconnect bool `mapstructure:"fast_reconnect" yaml:"fast_reconnect"`

	// Connection pool for HTTP requests.
	ConnectionPoolSize int `mapstructure:"connection_pool_size" yaml:"connection_pool_size"`

	// Enable event batching for better performance.
	EnableEventBatching bool `mapstructure:"enable_event_batching" yaml:"enable_event_batching"`

	// Batch timeout for event processing.
	BatchTimeout time.Duration `mapstructure:"batch_timeout" yaml:"batch_timeout"`
}

// SSEEvent represents a Server-Sent Event.
type SSEEvent struct {
	ID    string
	Event string
	Data  string
	Retry int
}

// SSEClient implements DirectClient for SSE-based MCP servers.
type SSEClient struct {
	name   string
	url    string
	config SSEClientConfig
	logger *zap.Logger

	// HTTP client for requests.
	client *http.Client

	// State management.
	mu        sync.RWMutex
	state     ConnectionState
	startTime time.Time

	// Request correlation.
	pendingRequests map[string]chan *mcp.Response
	requestMapMu    sync.RWMutex
	requestID       uint64

	// SSE stream management.
	streamConn   *http.Response
	streamReader *bufio.Reader
	streamMu     sync.RWMutex

	// Performance optimizations.
	requestBuffer bytes.Buffer // Reusable buffer for HTTP requests
	requestBufMu  sync.Mutex

	// Metrics.
	metrics   ClientMetrics
	metricsMu sync.RWMutex

	// Shutdown coordination.
	shutdownCh chan struct{}
	doneCh     chan struct{}
	wg         sync.WaitGroup
}

// NewSSEClient creates a new SSE direct client.
// Deprecated: Use EstablishSSEConnection or InitializeSSEClient instead for better naming.
func NewSSEClient(name, serverURL string, config SSEClientConfig, logger *zap.Logger) (*SSEClient, error) {
	return EstablishSSEConnection(name, serverURL, config, logger)
}

// Connect establishes connection to the SSE MCP server.
func (c *SSEClient) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.state == StateConnected || c.state == StateHealthy {
		return NewConnectionError(c.url, "sse", "client already connected", ErrClientAlreadyConnected)
	}

	c.state = StateConnecting

	if err := c.connect(ctx); err != nil {
		c.state = StateError

		return NewConnectionError(c.url, "sse", "failed to establish SSE connection", err)
	}

	c.state = StateConnected
	c.startTime = time.Now()
	c.updateMetrics(func(m *ClientMetrics) {
		m.ConnectionTime = time.Now()
		m.IsHealthy = true
	})

	// Start SSE stream reader.
	c.wg.Add(1)

	go c.readSSEStream()

	// Start health check routine if enabled.
	if c.config.HealthCheck.Enabled {
		c.wg.Add(1)

		go c.healthCheckLoop(ctx)
	}

	c.logger.Info("SSE client connected successfully",
		zap.String("url", c.config.URL),
		zap.String("method", c.config.Method))

	return nil
}

// connect sets up the HTTP client and establishes SSE stream.
func (c *SSEClient) connect(ctx context.Context) error {
	// Create HTTP transport with configuration and performance optimizations.
	transport := &http.Transport{
		MaxIdleConns:        c.config.Client.MaxIdleConns,
		MaxIdleConnsPerHost: c.config.Client.MaxIdleConnsPerHost,
		IdleConnTimeout:     c.config.Client.IdleConnTimeout,
		DisableKeepAlives:   c.config.Client.DisableKeepAlives,
	}

	// Apply performance optimizations.
	if c.config.Performance.ReuseConnections {
		// Increase connection pool size for better reuse.
		if transport.MaxIdleConns < c.config.Performance.ConnectionPoolSize {
			transport.MaxIdleConns = c.config.Performance.ConnectionPoolSize
		}

		if transport.MaxIdleConnsPerHost < c.config.Performance.ConnectionPoolSize/RetryMultiplier {
			transport.MaxIdleConnsPerHost = c.config.Performance.ConnectionPoolSize / RetryMultiplier
		}
	}

	// Enable compression if requested.
	if c.config.Performance.EnableCompression {
		transport.DisableCompression = false
	} else {
		transport.DisableCompression = true
	}

	// Create HTTP client.
	client := &http.Client{
		Transport: transport,
		Timeout:   c.config.RequestTimeout,
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

	// Establish SSE stream connection.
	if err := c.establishSSEStream(ctx); err != nil {
		return fmt.Errorf("failed to establish SSE stream: %w", err)
	}

	return nil
}

// establishSSEStream creates the SSE event stream connection.
func (c *SSEClient) establishSSEStream(ctx context.Context) error {
	// Create SSE stream request.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.config.URL, nil)
	if err != nil {
		return fmt.Errorf("failed to create SSE request: %w", err)
	}

	// Set SSE-specific headers.
	for key, value := range c.config.Headers {
		req.Header.Set(key, value)
	}

	req.Header.Set("User-Agent", c.config.Client.UserAgent)

	// Make the request.
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("SSE stream request failed: %w", err)
	}

	// Check response status.
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()

		return fmt.Errorf("SSE stream failed with status %d", resp.StatusCode)
	}

	// Check content type.
	contentType := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "text/event-stream") {
		_ = resp.Body.Close()

		return fmt.Errorf("invalid content type for SSE: %s", contentType)
	}

	c.streamMu.Lock()
	c.streamConn = resp
	// Use performance-optimized buffer size for stream reading.
	bufferSize := c.config.Performance.StreamBufferSize
	if bufferSize == 0 {
		bufferSize = c.config.BufferSize // Fallback to original buffer size
	}

	c.streamReader = bufio.NewReaderSize(resp.Body, bufferSize)
	c.streamMu.Unlock()

	c.logger.Debug("SSE stream established",
		zap.String("url", c.config.URL),
		zap.Int("status_code", resp.StatusCode))

	return nil
}

// SendRequest sends an MCP request via HTTP and waits for SSE response.
func (c *SSEClient) SendRequest(ctx context.Context, req *mcp.Request) (*mcp.Response, error) {
	// Use the descriptive request processor instead of complex inline logic.
	processor := InitializeSSERequestProcessor(c)

	return processor.ProcessSSERequest(ctx, req)
}

// sendHTTPRequest sends the MCP request via HTTP POST.
func (c *SSEClient) sendHTTPRequest(ctx context.Context, req *mcp.Request) error {
	// Create and marshal request body.
	bodyReader, err := c.createRequestBody(req)
	if err != nil {
		return err
	}

	// Create HTTP request with headers.
	httpReq, err := c.createHTTPRequest(ctx, bodyReader)
	if err != nil {
		return err
	}

	// Send request and handle response.
	return c.executeHTTPRequest(httpReq)
}

// createRequestBody marshals the request to JSON and returns a reader.
func (c *SSEClient) createRequestBody(req *mcp.Request) (io.Reader, error) {
	c.requestBufMu.Lock()
	defer c.requestBufMu.Unlock()

	c.requestBuffer.Reset()

	encoder := json.NewEncoder(&c.requestBuffer)
	if err := encoder.Encode(req); err != nil {
		return nil, NewRequestError(c.url, "sse", "failed to marshal request", err)
	}

	return bytes.NewReader(c.requestBuffer.Bytes()), nil
}

// createHTTPRequest creates an HTTP request with appropriate headers.
func (c *SSEClient) createHTTPRequest(ctx context.Context, body io.Reader) (*http.Request, error) {
	httpReq, err := http.NewRequestWithContext(ctx, c.config.Method, c.config.URL, body)
	if err != nil {
		return nil, NewRequestError(c.url, "sse", "failed to create HTTP request", err)
	}

	// Set headers (excluding SSE-specific ones for request).
	for key, value := range c.config.Headers {
		if key != "Accept" && key != "Cache-Control" {
			httpReq.Header.Set(key, value)
		}
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", c.config.Client.UserAgent)

	// Add compression headers if enabled.
	if c.config.Performance.EnableCompression {
		httpReq.Header.Set("Accept-Encoding", "gzip, deflate")
		httpReq.Header.Set("Content-Encoding", "gzip")
	}

	return httpReq, nil
}

// executeHTTPRequest executes the HTTP request and handles the response.
func (c *SSEClient) executeHTTPRequest(httpReq *http.Request) error {
	resp, err := c.client.Do(httpReq)
	if err != nil {
		// Mark connection as unhealthy on error.
		c.mu.Lock()
		c.state = StateUnhealthy
		c.mu.Unlock()

		return NewRequestError(c.url, "sse", "HTTP request failed", err)
	}

	defer func() {
		if err := resp.Body.Close(); err != nil {
			c.logger.Debug("Failed to close response body", zap.Error(err))
		}
	}()

	// Check for HTTP errors.
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		bodyBytes, _ := io.ReadAll(resp.Body)

		return NewRequestError(c.url, "sse",
			fmt.Sprintf("HTTP error %d: %s", resp.StatusCode, string(bodyBytes)), nil)
	}

	// For SSE, we don't expect response body here - responses come via the SSE stream.
	return nil
}

// readSSEStream continuously reads SSE events and routes responses.
func (c *SSEClient) readSSEStream() {
	reader := CreateSSEStreamReader(c)
	reader.ReadStreamLoop()
}

// processSSEEvent processes an SSE event and routes MCP responses.
func (c *SSEClient) processSSEEvent(event *SSEEvent) {
	// For MCP, we expect JSON data in the event.
	if event.Data == "" {
		return
	}

	// Try to parse as MCP response.
	var response mcp.Response
	if err := json.Unmarshal([]byte(event.Data), &response); err != nil {
		c.logger.Warn("failed to parse SSE event as MCP response",
			zap.String("data", event.Data),
			zap.Error(err))

		return
	}

	// Route response to waiting request.
	c.requestMapMu.RLock()

	if responseID, ok := response.ID.(string); ok {
		if ch, exists := c.pendingRequests[responseID]; exists {
			select {
			case ch <- &response:
			default:
				c.logger.Warn("response channel full, dropping response",
					zap.String("request_id", responseID),
					zap.String("client_name", c.name))
			}
		} else {
			c.logger.Warn("received response for unknown request",
				zap.String("request_id", responseID))
		}
	} else {
		c.logger.Warn("received response with invalid ID type",
			zap.Any("response_id", response.ID))
	}

	c.requestMapMu.RUnlock()
}

// Health checks if the client connection is healthy.
func (c *SSEClient) Health(ctx context.Context) error {
	c.mu.RLock()
	state := c.state
	c.mu.RUnlock()

	if state != StateConnected && state != StateHealthy {
		c.updateMetrics(func(m *ClientMetrics) {
			m.IsHealthy = false
			m.LastHealthCheck = time.Now()
		})

		return NewHealthError(c.url, "sse", "client not connected", ErrClientNotConnected)
	}

	c.streamMu.RLock()
	conn := c.streamConn
	c.streamMu.RUnlock()

	if conn == nil {
		c.updateMetrics(func(m *ClientMetrics) {
			m.IsHealthy = false
			m.LastHealthCheck = time.Now()
		})

		return NewHealthError(c.url, "sse", "SSE stream is nil", ErrClientNotConnected)
	}

	// For SSE health checks, we don't send a ping request to avoid circular locking.
	// Instead, we just verify the connection is still open and mark as healthy.
	// The actual connectivity is verified by the SSE stream reader goroutine.
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
func (c *SSEClient) healthCheckLoop(parentCtx context.Context) {
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
			}

			cancel()
		}
	}
}

// Close gracefully closes the client connection.
func (c *SSEClient) Close(ctx context.Context) error {
	closer := CreateSSEClientCloser(c)

	return closer.CloseClient(ctx)
}

// GetName returns the client name.
func (c *SSEClient) GetName() string {
	return c.name
}

// GetProtocol returns the protocol type.
func (c *SSEClient) GetProtocol() string {
	return "sse"
}

// GetMetrics returns current client metrics.
func (c *SSEClient) GetMetrics() ClientMetrics {
	c.metricsMu.RLock()
	defer c.metricsMu.RUnlock()

	return c.metrics
}

// updateMetrics safely updates client metrics.
func (c *SSEClient) updateMetrics(updateFn func(*ClientMetrics)) {
	c.metricsMu.Lock()
	updateFn(&c.metrics)
	c.metricsMu.Unlock()
}

// GetState returns the current connection state.
func (c *SSEClient) GetState() ConnectionState {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.state
}

// GetStatus returns detailed client status.
func (c *SSEClient) GetStatus() DirectClientStatus {
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
		Protocol:          "sse",
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
		Protocol:        "sse",
		State:           state,
		LastConnected:   metrics.ConnectionTime,
		LastHealthCheck: metrics.LastHealthCheck,
		Metrics:         detailedMetrics,
		Configuration:   make(map[string]interface{}),
		RuntimeInfo: ClientRuntimeInfo{
			ConnectionID: fmt.Sprintf("sse-%s-%d", c.name, metrics.ConnectionTime.Unix()),
			ResourceUsage: ResourceUsage{
				LastUpdated: time.Now(),
			},
		},
	}

	return status
}

// GetURL returns the SSE URL.
func (c *SSEClient) GetURL() string {
	return c.url
}
