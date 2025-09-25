package sse



import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	customerrors "github.com/poiley/mcp-bridge/services/gateway/internal/errors"
	"github.com/poiley/mcp-bridge/services/gateway/internal/metrics"
	"github.com/poiley/mcp-bridge/services/router/pkg/mcp"

	"go.uber.org/zap"
)

const (
	defaultTimeoutSeconds    = 30
	defaultMaxConnections    = 5
	defaultRetryCount        = 10
	defaultBufferSize        = 1024
	defaultMaxTimeoutSeconds = 60
	defaultMaxRetries        = 100
	bufferSizeMultiplier     = 64
	retryDelaySeconds        = 10
	halfDivisor              = 2
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

// Config contains SSE backend configuration.
type Config struct {
	BaseURL          string            `mapstructure:"base_url"`
	StreamEndpoint   string            `mapstructure:"stream_endpoint"`
	RequestEndpoint  string            `mapstructure:"request_endpoint"`
	Headers          map[string]string `mapstructure:"headers"`
	Timeout          time.Duration     `mapstructure:"timeout"`
	ReconnectDelay   time.Duration     `mapstructure:"reconnect_delay"`
	MaxReconnects    int               `mapstructure:"max_reconnects"`
	HealthCheck      HealthCheckConfig `mapstructure:"health_check"`
	BufferSize       int               `mapstructure:"buffer_size"`
	KeepAliveTimeout time.Duration     `mapstructure:"keep_alive_timeout"`
}

// HealthCheckConfig contains health check settings.
type HealthCheckConfig struct {
	Enabled  bool          `mapstructure:"enabled"`
	Interval time.Duration `mapstructure:"interval"`
	Timeout  time.Duration `mapstructure:"timeout"`
}

// RequestResponse represents a pending request-response correlation.
type RequestResponse struct {
	ID        string
	Request   *mcp.Request
	Response  chan *mcp.Response
	Timestamp time.Time
}

// Backend implements the SSE backend for MCP servers.
type Backend struct {
	name            string
	config          Config
	logger          *zap.Logger
	metricsRegistry *metrics.Registry

	// HTTP client for requests
	httpClient *http.Client

	// SSE connection management
	sseConn        *http.Response
	sseReader      *bufio.Reader
	connected      bool
	connMu         sync.RWMutex
	reconnectCount int

	// Request correlation
	correlation map[string]chan *mcp.Response
	corrMu      sync.RWMutex
	requestID   uint64

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

// CreateSSEBackend creates a Server-Sent Events backend instance for stream-based communication.
func CreateSSEBackend(name string, config Config, logger *zap.Logger, metricsRegistry *metrics.Registry) *Backend {
	// Set defaults
	if config.Timeout == 0 {
		config.Timeout = defaultTimeoutSeconds * time.Second
	}

	if config.ReconnectDelay == 0 {
		config.ReconnectDelay = defaultMaxConnections * time.Second
	}

	if config.MaxReconnects == 0 {
		config.MaxReconnects = defaultRetryCount
	}

	if config.BufferSize == 0 {
		config.BufferSize = bufferSizeMultiplier * defaultBufferSize // 64KB
	}

	if config.KeepAliveTimeout == 0 {
		config.KeepAliveTimeout = defaultMaxTimeoutSeconds * time.Second
	}

	if config.StreamEndpoint == "" {
		config.StreamEndpoint = "/events"
	}

	if config.RequestEndpoint == "" {
		config.RequestEndpoint = "/api/v1/request"
	}

	if config.HealthCheck.Interval == 0 {
		config.HealthCheck.Interval = defaultTimeoutSeconds * time.Second
	}

	if config.HealthCheck.Timeout == 0 {
		config.HealthCheck.Timeout = defaultMaxConnections * time.Second
	}

	// Create HTTP client
	httpClient := &http.Client{
		Timeout: config.Timeout,
		Transport: &http.Transport{
			IdleConnTimeout:   config.KeepAliveTimeout,
			DisableKeepAlives: false,
		},
	}

	return &Backend{
		name:            name,
		config:          config,
		logger:          logger.With(zap.String("backend", name), zap.String("protocol", "sse")),
		metricsRegistry: metricsRegistry,
		httpClient:      httpClient,
		correlation:     make(map[string]chan *mcp.Response),
		shutdownCh:      make(chan struct{}),
		metrics: BackendMetrics{
			IsHealthy: false,
		},
	}
}

// Start initializes the SSE backend.
func (b *Backend) Start(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.running {
		return customerrors.New(customerrors.TypeInternal, "backend " + b.name + " already running").
			WithComponent("backend_sse").
			WithContext("backend_name", b.name)
	}

	// Start SSE connection
	if err := b.connectSSE(ctx); err != nil {
		return customerrors.WrapSSEConnectionError(ctx, err, b.name)
	}

	b.running = true
	b.updateMetrics(func(m *BackendMetrics) {
		m.ConnectionTime = time.Now()
		m.IsHealthy = true
	})

	// Start background routines
	b.wg.Add(1)

	go b.readSSEEvents(ctx)

	b.wg.Add(1)

	go b.connectionMonitor(ctx)

	if b.config.HealthCheck.Enabled {
		b.wg.Add(1)

		go b.healthCheckLoop(ctx)
	}

	b.logger.Info("SSE backend started successfully",
		zap.String("base_url", b.config.BaseURL),
		zap.String("stream_endpoint", b.config.StreamEndpoint))

	return nil
}

// connectSSE establishes the SSE connection.
func (b *Backend) connectSSE(ctx context.Context) error {
	streamURL := b.config.BaseURL + b.config.StreamEndpoint

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, streamURL, nil)
	if err != nil {
		return customerrors.Wrap(err, "failed to create SSE request").
			WithComponent("backend_sse").
			WithContext("backend_name", b.name).
			WithContext("endpoint", b.config.BaseURL)
	}

	// Set SSE headers
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Connection", "keep-alive")

	// Add custom headers
	for k, v := range b.config.Headers {
		req.Header.Set(k, v)
	}

	b.logger.Debug("connecting to SSE stream", zap.String("url", streamURL))

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return customerrors.WrapSSEConnectionError(ctx, err, b.name)
	}

	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close() 

		return customerrors.New(customerrors.TypeInternal, fmt.Sprintf("SSE stream returned status %d", resp.StatusCode)).
			WithComponent("backend_sse").
			WithContext("backend_name", b.name).
			WithContext("status_code", resp.StatusCode).
			WithHTTPStatus(http.StatusBadGateway)
	}

	// Verify content type
	contentType := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "text/event-stream") {
		_ = resp.Body.Close() 

		return customerrors.New(customerrors.TypeValidation, "invalid content type: " + contentType ).
			WithComponent("backend_sse").
			WithContext("backend_name", b.name).
			WithContext("content_type", contentType)
	}

	b.connMu.Lock()
	b.sseConn = resp
	b.sseReader = bufio.NewReaderSize(resp.Body, b.config.BufferSize)
	b.connected = true
	b.reconnectCount = 0
	b.connMu.Unlock()

	b.logger.Info("SSE connection established")

	return nil
}

// readSSEEvents reads and processes SSE events.
func (b *Backend) readSSEEvents(ctx context.Context) {
	defer b.wg.Done()

	for {
		select {
		case <-b.shutdownCh:
			return
		default:
		}

		b.connMu.RLock()

		if !b.connected || b.sseReader == nil {
			b.connMu.RUnlock()
			time.Sleep(defaultMaxRetries * time.Millisecond)

			continue
		}

		line, err := b.sseReader.ReadString('\n')
		b.connMu.RUnlock()

		if err != nil {
			b.handleSSEReadError(err)

			continue
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue // Skip empty lines
		}

		// Parse SSE event
		if err := b.parseSSEEvent(ctx, line); err != nil {
			b.logger.Warn("failed to parse SSE event", zap.String("line", line), zap.Error(err))
		}
	}
}

// handleSSEReadError handles errors that occur when reading from the SSE stream.
func (b *Backend) handleSSEReadError(err error) {
	if errors.Is(err, io.EOF) {
		b.logger.Warn("SSE stream closed")
	} else {
		b.logger.Error("error reading SSE stream", zap.Error(err))
	}

	b.connMu.Lock()
	b.connected = false

	if b.sseConn != nil {
		_ = b.sseConn.Body.Close() 
		b.sseConn = nil
	}

	b.connMu.Unlock()

	b.updateMetrics(func(m *BackendMetrics) {
		m.IsHealthy = false
	})
}

// parseSSEEvent parses an SSE event line.
func (b *Backend) parseSSEEvent(ctx context.Context, line string) error {
	// SSE format: "data: JSON"
	if !strings.HasPrefix(line, "data: ") {
		// Skip non-data lines (comments, event types, etc.)
		return nil
	}

	data := strings.TrimPrefix(line, "data: ")

	// Try to parse as MCP response
	var response mcp.Response
	if err := json.Unmarshal([]byte(data), &response); err != nil {
		return customerrors.WrapInvalidResponseError(ctx, err, b.name, "json")
	}

	// Route response to waiting request
	b.corrMu.RLock()

	if responseID, ok := response.ID.(string); ok {
		if ch, exists := b.correlation[responseID]; exists {
			select {
			case ch <- &response:
			default:
				b.logger.Warn("response channel full", zap.String("request_id", responseID))
			}
		} else {
			b.logger.Warn("received response for unknown request", zap.String("request_id", responseID))
		}
	}

	b.corrMu.RUnlock()

	return nil
}

// connectionMonitor monitors the SSE connection and handles reconnection.
func (b *Backend) connectionMonitor(ctx context.Context) {
	defer b.wg.Done()

	ticker := time.NewTicker(defaultMaxConnections * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-b.shutdownCh:
			return
		case <-ticker.C:
			b.connMu.RLock()
			connected := b.connected
			b.connMu.RUnlock()

			if !connected && b.reconnectCount < b.config.MaxReconnects {
				b.logger.Info("attempting to reconnect SSE stream",
					zap.Int("attempt", b.reconnectCount+1),
					zap.Int("max_attempts", b.config.MaxReconnects))

				time.Sleep(b.config.ReconnectDelay)

				reconnectCtx, cancel := context.WithTimeout(ctx, defaultTimeoutSeconds*time.Second)
				if err := b.connectSSE(reconnectCtx); err != nil {
					b.logger.Warn("SSE reconnection failed", zap.Error(err))

					b.reconnectCount++
				} else {
					b.logger.Info("SSE reconnection successful")
					b.updateMetrics(func(m *BackendMetrics) {
						m.IsHealthy = true
					})
				}

				cancel()
			}
		}
	}
}

// SendRequest sends an MCP request via HTTP POST and waits for SSE response.
func (b *Backend) SendRequest(ctx context.Context, req *mcp.Request) (*mcp.Response, error) {
	if err := b.validateRequestPreconditions(); err != nil {
		b.recordErrorMetric(err)

		return nil, err
	}

	requestID := b.ensureRequestID(req)

	// Create response channel and setup correlation
	respCh := make(chan *mcp.Response, 1)

	b.corrMu.Lock()
	b.correlation[requestID] = respCh
	b.corrMu.Unlock()

	// Cleanup on exit
	defer func() {
		b.corrMu.Lock()
		delete(b.correlation, requestID)
		close(respCh)
		b.corrMu.Unlock()
	}()

	// Send HTTP POST request
	startTime := time.Now()

	if err := b.sendHTTPRequest(ctx, req); err != nil {
		b.updateMetrics(func(m *BackendMetrics) {
			m.ErrorCount++
		})
		
		wrappedErr := customerrors.WrapWriteError(ctx, err, b.name, 0).
			WithContext("request_id", req.ID).
			WithContext("method", req.Method)
		b.recordErrorMetric(wrappedErr)

		return nil, wrappedErr
	}

	// Wait for response via SSE
	resp, err := b.waitForResponse(ctx, respCh, startTime)
	if err != nil {
		b.recordErrorMetric(err)

		return nil, err
	}
	
	return resp, nil
}

// validateRequestPreconditions checks if the backend is ready to handle requests.
func (b *Backend) validateRequestPreconditions() error {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if !b.running {
		return customerrors.CreateBackendUnavailableError(b.name, "not running")
	}

	b.connMu.RLock()
	defer b.connMu.RUnlock()

	if !b.connected {
		return customerrors.CreateBackendUnavailableError(b.name, "SSE stream not connected")
	}

	return nil
}

// ensureRequestID generates or extracts a request ID from the request.
func (b *Backend) ensureRequestID(req *mcp.Request) string {
	if req.ID == nil || req.ID == "" {
		requestID := fmt.Sprintf("%s-%d", b.name, atomic.AddUint64(&b.requestID, 1))
		req.ID = requestID

		return requestID
	}

	if requestID, ok := req.ID.(string); ok {
		return requestID
	}

	return fmt.Sprint(req.ID)
}

// calculateTimeout determines the effective timeout for the request.
func (b *Backend) calculateTimeout(ctx context.Context) time.Duration {
	timeout := b.config.Timeout
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining < timeout {
			timeout = remaining
		}
	}

	return timeout
}

// waitForResponse waits for the response and handles timeout/cancellation.
func (b *Backend) waitForResponse(
	ctx context.Context,
	respCh chan *mcp.Response,
	startTime time.Time,
) (*mcp.Response, error) {
	timeout := b.calculateTimeout(ctx)

	select {
	case resp := <-respCh:
		b.updateSuccessMetrics(time.Since(startTime))

		return resp, nil
	case <-time.After(timeout):
		b.updateErrorMetrics()

		return nil, customerrors.CreateConnectionTimeoutError(b.name, timeout)
	case <-ctx.Done():
		b.updateErrorMetrics()

		return nil, ctx.Err()
	}
}

// updateSuccessMetrics updates metrics for successful requests.
func (b *Backend) updateSuccessMetrics(duration time.Duration) {
	b.updateMetrics(func(m *BackendMetrics) {
		m.RequestCount++
		if m.RequestCount == 1 {
			m.AverageLatency = duration
		} else {
			m.AverageLatency = (m.AverageLatency + duration) / halfDivisor
		}
	})
}

// updateErrorMetrics updates metrics for failed requests.
func (b *Backend) updateErrorMetrics() {
	b.updateMetrics(func(m *BackendMetrics) {
		m.ErrorCount++
	})
}

// sendHTTPRequest sends the MCP request via HTTP POST.
func (b *Backend) sendHTTPRequest(ctx context.Context, req *mcp.Request) error {
	requestURL := b.config.BaseURL + b.config.RequestEndpoint

	// Marshal request to JSON
	jsonData, err := json.Marshal(req)
	if err != nil {
		return customerrors.Wrap(err, "failed to marshal request").
			WithComponent("backend_sse").
			WithContext("backend_name", b.name)
	}

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return customerrors.Wrap(err, "failed to create HTTP request").
			WithComponent("backend_sse").
			WithContext("backend_name", b.name).
			WithContext("endpoint", b.config.RequestEndpoint)
	}

	// Set headers
	httpReq.Header.Set("Content-Type", "application/json")

	for k, v := range b.config.Headers {
		httpReq.Header.Set(k, v)
	}

	// Send request
	resp, err := b.httpClient.Do(httpReq)
	if err != nil {
		return customerrors.WrapWriteError(ctx, err, b.name, 0)
	}

	defer func() {
		_ = resp.Body.Close() 
	}()

	// Check response status
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body) 

		return customerrors.New(customerrors.TypeInternal,
			fmt.Sprintf("HTTP request failed with status %d: %s", resp.StatusCode, string(body))).
			WithComponent("backend_sse").
			WithContext("backend_name", b.name).
			WithContext("status_code", resp.StatusCode).
			WithHTTPStatus(http.StatusBadGateway)
	}

	return nil
}

// Health checks if the backend is healthy.
func (b *Backend) Health(ctx context.Context) error {
	b.mu.RLock()

	if !b.running {
		b.mu.RUnlock()

		return customerrors.CreateBackendUnavailableError(b.name, "not running")
	}

	b.mu.RUnlock()

	// Check SSE connection
	b.connMu.RLock()
	connected := b.connected
	b.connMu.RUnlock()

	if !connected {
		b.updateMetrics(func(m *BackendMetrics) {
			m.IsHealthy = false
			m.LastHealthCheck = time.Now()
		})

		return customerrors.CreateBackendUnavailableError(b.name, "SSE stream not connected")
	}

	// Optionally send a health check request
	if b.config.HealthCheck.Enabled {
		healthReq := &mcp.Request{
			Method: "ping",
			ID:     fmt.Sprintf("health-%d", time.Now().UnixNano()),
		}

		healthCtx, cancel := context.WithTimeout(ctx, b.config.HealthCheck.Timeout)
		defer cancel()

		_, err := b.SendRequest(healthCtx, healthReq)
		if err != nil {
			b.updateMetrics(func(m *BackendMetrics) {
				m.IsHealthy = false
				m.LastHealthCheck = time.Now()
			})

			return customerrors.WrapSSEConnectionError(ctx, err, b.name)
		}
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

// Stop gracefully shuts down the SSE backend.
func (b *Backend) Stop(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.running {
		return nil
	}

	b.logger.Info("stopping SSE backend")

	// Signal shutdown
	close(b.shutdownCh)

	// Close SSE connection
	b.connMu.Lock()

	if b.sseConn != nil {
		_ = b.sseConn.Body.Close() 
		b.sseConn = nil
	}

	b.connected = false
	b.connMu.Unlock()

	// Wait for background routines
	done := make(chan struct{})

	go func() {
		b.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(retryDelaySeconds * time.Second):
		b.logger.Warn("background routines did not finish in time")
	case <-ctx.Done():
		return ctx.Err()
	}

	b.running = false
	b.updateMetrics(func(m *BackendMetrics) {
		m.IsHealthy = false
	})

	b.logger.Info("SSE backend stopped")

	return nil
}

// GetName returns the backend name.
func (b *Backend) GetName() string {
	return b.name
}

// GetProtocol returns the backend protocol.
func (b *Backend) GetProtocol() string {
	return "sse"
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

// recordErrorMetric updates metrics for a failed request and records to Prometheus.
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
			gatewayErr = customerrors.Wrap(err, "sse backend error").
				WithComponent("sse_backend").
				WithContext("backend_name", b.name)
		} else {
			// Ensure component is set
			gatewayErr = gatewayErr.WithComponent("sse_backend").
				WithContext("backend_name", b.name)
		}
		
		customerrors.RecordError(gatewayErr, b.metricsRegistry)
	}
}
