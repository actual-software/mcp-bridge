// Package router provides HTTP request routing and load balancing functionality for the MCP gateway.
package router

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/ext"
	"go.uber.org/zap"

	"github.com/actual-software/mcp-bridge/services/gateway/internal/config"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/discovery"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/errors"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/metrics"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/session"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/tracing"
	"github.com/actual-software/mcp-bridge/services/gateway/pkg/circuit"
	"github.com/actual-software/mcp-bridge/services/gateway/pkg/loadbalancer"
	"github.com/actual-software/mcp-bridge/services/router/pkg/mcp"
)

// contextKey is a custom type for context keys to avoid collisions.
type contextKey string

const (
	// sessionContextKey is the context key for session information.
	sessionContextKey contextKey = "session"
)

// Protocol scheme constants.
const (
	schemeWSS        = "wss"
	schemeHTTP       = "http"
	defaultNamespace = "default"
)

const (
	defaultMaxConnections = 5
	defaultMaxRetries     = 100
	defaultRetryCount     = 10
	defaultBufferSize     = 1024
	defaultTimeoutSeconds = 30

	// Timeout constants.
	defaultDialTimeout           = 2 * time.Second
	defaultTLSHandshakeTimeout   = 2 * time.Second
	defaultResponseHeaderTimeout = 3 * time.Second
	defaultHealthCheckTimeout    = 2 * time.Second
)

// Router routes MCP requests to appropriate backend servers.
type Router struct {
	config    config.RoutingConfig
	discovery discovery.ServiceDiscovery
	metrics   *metrics.Registry
	logger    *zap.Logger

	// Load balancers per namespace
	balancers  map[string]loadbalancer.LoadBalancer
	balancerMu sync.RWMutex

	// Circuit breakers per endpoint
	breakers  map[string]*circuit.CircuitBreaker
	breakerMu sync.RWMutex

	// HTTP client for backend connections
	httpClient *http.Client

	// Request counter for round-robin
	requestCounter uint64
}

// InitializeRequestRouter creates and configures a request routing engine.
func InitializeRequestRouter(
	ctx context.Context,
	cfg config.RoutingConfig,
	discovery discovery.ServiceDiscovery,
	metrics *metrics.Registry,
	logger *zap.Logger,
) *Router {
	r := &Router{
		config:    cfg,
		discovery: discovery,
		metrics:   metrics,
		logger:    logger,
		balancers: make(map[string]loadbalancer.LoadBalancer),
		breakers:  make(map[string]*circuit.CircuitBreaker),
		httpClient: &http.Client{
			// Reduce timeout for faster error propagation
			Timeout: defaultMaxConnections * time.Second, // Was 30s, now 5s for faster failure detection
			Transport: &http.Transport{
				MaxIdleConns:        defaultMaxRetries,
				MaxIdleConnsPerHost: defaultRetryCount,
				IdleConnTimeout:     defaultTimeoutSeconds * time.Second, // Reduced from 90s
				// Add connection timeouts for faster failure detection
				DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
					d := &net.Dialer{Timeout: defaultDialTimeout}

					return d.DialContext(ctx, network, addr)
				},
				TLSHandshakeTimeout:   defaultTLSHandshakeTimeout,
				ResponseHeaderTimeout: defaultResponseHeaderTimeout,
			},
		},
	}

	// Start health check routine
	go r.runHealthChecks(ctx)

	return r
}

// RouteRequest routes an MCP request to an appropriate backend.
// selectEndpoint selects an endpoint for the given namespace.
func (r *Router) selectEndpoint(targetNamespace string, span opentracing.Span) (*discovery.Endpoint, error) {
	lb := r.getLoadBalancer(targetNamespace)
	if lb == nil {
		r.metrics.IncrementRoutingErrors("no_endpoints")

		err := NewNoEndpointsError(targetNamespace)

		ext.Error.Set(span, true)
		span.LogKV("error", err.Error(), "error_code", ErrCodeNoEndpoints)

		return nil, err
	}

	endpoint := lb.Next()
	if endpoint == nil {
		r.metrics.IncrementRoutingErrors("no_healthy_endpoints")

		err := NewNoHealthyEndpointsError(targetNamespace)

		ext.Error.Set(span, true)
		span.LogKV("error", err.Error(), "error_code", ErrCodeNoHealthyEndpoints)

		return nil, err
	}

	span.SetTag("endpoint.address", endpoint.Address)
	span.SetTag("endpoint.port", endpoint.Port)

	return endpoint, nil
}

// executeWithCircuitBreaker executes the request through a circuit breaker.
func (r *Router) executeWithCircuitBreaker(
	ctx context.Context,
	endpoint *discovery.Endpoint,
	req *mcp.Request,
) (*mcp.Response, error) {
	breaker := r.getCircuitBreaker(endpoint.Address)

	var resp *mcp.Response

	err := breaker.Call(func() error {
		var err error

		resp, err = r.forwardRequest(ctx, endpoint, req)

		return err
	})

	return resp, err
}

func (r *Router) RouteRequest(ctx context.Context, req *mcp.Request, targetNamespace string) (*mcp.Response, error) {
	startTime := time.Now()

	// Enrich context with request information
	ctx = EnrichContextWithRequest(ctx, req, targetNamespace)

	// Start tracing span
	span, ctx := opentracing.StartSpanFromContext(ctx, "router.RouteRequest")
	defer span.Finish()

	// Set span tags
	span.SetTag("mcp.method", req.Method)
	span.SetTag("mcp.id", req.ID)
	span.SetTag("target.namespace", targetNamespace)

	// Extract namespace from request if not provided
	if targetNamespace == "" {
		targetNamespace = r.extractNamespace(req)
		span.SetTag("target.namespace.extracted", targetNamespace)
	}

	// Select endpoint
	endpoint, err := r.selectEndpoint(targetNamespace, span)
	if err != nil {
		wrappedErr := errors.FromContext(ctx, err)
		// Record error metrics
		if r.metrics != nil {
			errors.RecordError(wrappedErr, r.metrics)
		}

		return nil, wrappedErr
	}

	// Execute request through circuit breaker
	resp, err := r.executeWithCircuitBreaker(ctx, endpoint, req)
	if err != nil {
		err = WrapForwardingError(ctx, err, endpoint, req.Method)
		// Record error metrics
		if r.metrics != nil {
			errors.RecordError(err, r.metrics)
		}
	}

	// Record metrics
	duration := time.Since(startTime)
	span.SetTag("duration.ms", duration.Milliseconds())

	if err != nil {
		r.metrics.RecordRequestDuration(req.Method, "error", duration)
		ext.Error.Set(span, true)
		span.LogKV("error", err.Error())

		return nil, err
	}

	r.metrics.RecordRequestDuration(req.Method, "success", duration)

	return resp, nil
}

// forwardRequest forwards a request to a backend endpoint.
func (r *Router) forwardRequest(
	ctx context.Context,
	endpoint *discovery.Endpoint,
	req *mcp.Request,
) (*mcp.Response, error) {
	// Start child span
	span, ctx := opentracing.StartSpanFromContext(ctx, "router.forwardRequest")
	defer span.Finish()

	// Set endpoint information in span
	span.SetTag("endpoint.scheme", endpoint.Scheme)
	span.SetTag("endpoint.address", endpoint.Address)
	span.SetTag("endpoint.port", endpoint.Port)
	span.SetTag("endpoint.path", endpoint.Path)

	// Route based on endpoint scheme
	switch endpoint.Scheme {
	case "ws", schemeWSS:
		span.LogKV("routing.path", "websocket")

		return r.forwardRequestWebSocket(ctx, endpoint, req, span)
	case schemeHTTP, "https":
		span.LogKV("routing.path", "http")

		return r.forwardRequestHTTP(ctx, endpoint, req, span)
	default:
		err := NewUnsupportedSchemeError(endpoint.Scheme, endpoint)

		ext.Error.Set(span, true)
		span.LogKV("error", err.Error(), "error_code", ErrCodeUnsupportedScheme)

		return nil, err
	}
}

// forwardRequestWebSocket forwards a request to a backend MCP server over WebSocket.
func (r *Router) forwardRequestWebSocket(
	ctx context.Context,
	endpoint *discovery.Endpoint,
	req *mcp.Request,
	span opentracing.Span,
) (*mcp.Response, error) {
	// Build WebSocket URL
	wsURL := r.buildWebSocketURL(endpoint)
	span.SetTag("websocket.url", wsURL)

	// Create WebSocket dialer
	dialer := r.createWebSocketDialer()

	// Build request headers
	headers := r.buildWebSocketHeaders(ctx, span)

	// Establish connection
	conn, err := r.establishWebSocketConnection(ctx, dialer, wsURL, headers, span)
	if err != nil {
		return nil, WrapWebSocketError(ctx, err, "connect", endpoint)
	}
	defer r.closeWebSocketConnection(conn)

	// Configure connection timeouts
	if err := r.configureWebSocketTimeouts(conn); err != nil {
		return nil, WrapWebSocketError(ctx, err, "configure", endpoint)
	}

	// Send request and receive response
	return r.exchangeWebSocketMessages(ctx, conn, req, span)
}

// buildWebSocketURL constructs the WebSocket URL from endpoint.
func (r *Router) buildWebSocketURL(endpoint *discovery.Endpoint) string {
	wsScheme := r.getWebSocketScheme(endpoint.Scheme)
	path := r.getWebSocketPath(endpoint.Path)

	return fmt.Sprintf("%s://%s:%d%s", wsScheme, endpoint.Address, endpoint.Port, path)
}

// getWebSocketScheme returns the appropriate WebSocket scheme.
func (r *Router) getWebSocketScheme(scheme string) string {
	if scheme == schemeWSS {
		return schemeWSS
	}

	return "ws"
}

// getWebSocketPath returns the WebSocket path with default.
func (r *Router) getWebSocketPath(path string) string {
	if path == "" {
		return "/mcp"
	}

	return path
}

// createWebSocketDialer creates a configured WebSocket dialer.
func (r *Router) createWebSocketDialer() websocket.Dialer {
	return websocket.Dialer{
		HandshakeTimeout: defaultMaxConnections * time.Second,
		ReadBufferSize:   defaultBufferSize,
		WriteBufferSize:  defaultBufferSize,
	}
}

// buildWebSocketHeaders builds request headers including session info.
func (r *Router) buildWebSocketHeaders(ctx context.Context, span opentracing.Span) http.Header {
	headers := http.Header{}
	headers.Set("User-Agent", "mcp-gateway/1.0.0")

	if sess, ok := ctx.Value(sessionContextKey).(*session.Session); ok {
		headers.Set("X-MCP-Session-ID", sess.ID)
		headers.Set("X-MCP-User", sess.User)
		span.SetTag("session.id", sess.ID)
		span.SetTag("session.user", sess.User)
	}

	return headers
}

// establishWebSocketConnection establishes the WebSocket connection.
func (r *Router) establishWebSocketConnection(
	ctx context.Context,
	dialer websocket.Dialer,
	wsURL string,
	headers http.Header,
	span opentracing.Span,
) (*websocket.Conn, error) {
	conn, httpResp, err := dialer.DialContext(ctx, wsURL, headers)
	if httpResp != nil {
		defer func() { _ = httpResp.Body.Close() }()
	}

	if err != nil {
		ext.Error.Set(span, true)
		span.LogKV("error", "websocket dial failed", "err", err)

		return nil, WrapWebSocketError(ctx, err, "dial", &discovery.Endpoint{Address: wsURL})
	}

	return conn, nil
}

// closeWebSocketConnection safely closes the WebSocket connection.
func (r *Router) closeWebSocketConnection(conn *websocket.Conn) {
	if err := conn.Close(); err != nil {
		// Log but don't fail - connection is being closed anyway
		_ = err // Explicitly ignore error
	}
}

// configureWebSocketTimeouts sets read and write timeouts.
func (r *Router) configureWebSocketTimeouts(conn *websocket.Conn) error {
	if err := conn.SetReadDeadline(time.Now().Add(defaultRetryCount * time.Second)); err != nil {
		return fmt.Errorf("failed to set read deadline: %w", err)
	}

	if err := conn.SetWriteDeadline(time.Now().Add(defaultMaxConnections * time.Second)); err != nil {
		return fmt.Errorf("failed to set write deadline: %w", err)
	}

	return nil
}

// exchangeWebSocketMessages sends request and receives response.
func (r *Router) exchangeWebSocketMessages(
	ctx context.Context,
	conn *websocket.Conn,
	req *mcp.Request,
	span opentracing.Span,
) (*mcp.Response, error) {
	// Send the MCP request
	if err := conn.WriteJSON(req); err != nil {
		ext.Error.Set(span, true)
		span.LogKV("error", "websocket write failed", "err", err)

		return nil, WrapWebSocketError(ctx, err, "write", &discovery.Endpoint{})
	}

	// Read the MCP response
	var resp mcp.Response
	if err := conn.ReadJSON(&resp); err != nil {
		ext.Error.Set(span, true)
		span.LogKV("error", "websocket read failed", "err", err)

		return nil, WrapWebSocketError(ctx, err, "read", &discovery.Endpoint{})
	}

	span.LogKV("websocket.success", true)

	return &resp, nil
}

// forwardRequestHTTP forwards a request to a backend MCP server over HTTP.
func (r *Router) forwardRequestHTTP(
	ctx context.Context,
	endpoint *discovery.Endpoint,
	req *mcp.Request,
	span opentracing.Span,
) (*mcp.Response, error) {
	// Prepare the HTTP request
	httpReq, err := r.prepareHTTPRequest(ctx, endpoint, req, span)
	if err != nil {
		return nil, err
	}

	// Enhance request with session and tracing
	r.enhanceHTTPRequest(ctx, httpReq, span)

	// Send request and get response
	httpResp, err := r.httpClient.Do(httpReq)
	if err != nil {
		return nil, WrapHTTPError(ctx, err, "request", endpoint)
	}
	defer r.closeResponseBody(httpResp)

	// Process the response
	return r.processHTTPResponse(httpResp, span)
}

// prepareHTTPRequest creates and configures the HTTP request.
func (r *Router) prepareHTTPRequest(
	ctx context.Context,
	endpoint *discovery.Endpoint,
	req *mcp.Request,
	span opentracing.Span,
) (*http.Request, error) {
	// Marshal request body
	body, err := json.Marshal(req)
	if err != nil {
		ext.Error.Set(span, true)
		span.LogKV("error", "failed to marshal request", "err", err)

		return nil, WrapMarshalError(err, "request")
	}

	// Build URL
	url := r.buildHTTPURL(endpoint)
	span.SetTag("http.url", url)

	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		ext.Error.Set(span, true)
		span.LogKV("error", "failed to create request", "err", err)

		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	return httpReq, nil
}

// buildHTTPURL constructs the URL for the HTTP request.
func (r *Router) buildHTTPURL(endpoint *discovery.Endpoint) string {
	httpScheme := endpoint.Scheme
	if httpScheme != schemeHTTP && httpScheme != "https" {
		httpScheme = schemeHTTP // default fallback
	}

	path := endpoint.Path
	if path == "" {
		path = "/mcp"
	}

	return fmt.Sprintf("%s://%s:%d%s", httpScheme, endpoint.Address, endpoint.Port, path)
}

// enhanceHTTPRequest adds session info and tracing headers.
func (r *Router) enhanceHTTPRequest(ctx context.Context, httpReq *http.Request, span opentracing.Span) {
	// Add session info if available
	if sess, ok := ctx.Value(sessionContextKey).(*session.Session); ok {
		httpReq.Header.Set("X-MCP-Session-ID", sess.ID)
		httpReq.Header.Set("X-MCP-User", sess.User)
		span.SetTag("session.id", sess.ID)
		span.SetTag("session.user", sess.User)
	}

	// Inject tracing headers
	if err := tracing.InjectHTTPHeaders(span, httpReq); err != nil {
		span.LogKV("warning", "failed to inject tracing headers", "err", err)
	}
}

// closeResponseBody safely closes the HTTP response body.
func (r *Router) closeResponseBody(resp *http.Response) {
	if err := resp.Body.Close(); err != nil {
		r.logger.Warn("failed to close response body", zap.Error(err))
	}
}

// processHTTPResponse reads and validates the HTTP response.
func (r *Router) processHTTPResponse(httpResp *http.Response, span opentracing.Span) (*mcp.Response, error) {
	// Read response body
	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Check HTTP status
	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("backend returned status %d: %s", httpResp.StatusCode, string(respBody))
	}

	// Parse response
	var resp mcp.Response
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	span.LogKV("http.success", true)

	return &resp, nil
}

// extractNamespace extracts the target namespace from a request.
func (r *Router) extractNamespace(req *mcp.Request) string {
	// Handle standard MCP methods
	if req.Method == "initialize" || req.Method == "shutdown" {
		return "system"
	}

	// For tools/call, extract from tool name
	if req.Method == "tools/call" {
		params, ok := req.Params.(map[string]interface{})
		if !ok {
			return defaultNamespace
		}

		toolName, ok := params["name"].(string)
		if !ok {
			return defaultNamespace
		}

		parts := strings.Split(toolName, ".")
		if len(parts) > 0 {
			return parts[0]
		}
	}

	// For tools/list, check for namespace parameter
	if req.Method == "tools/list" {
		if params, ok := req.Params.(map[string]interface{}); ok {
			if ns, ok := params["namespace"].(string); ok {
				return ns
			}
		}

		return "system" // List all tools
	}

	return defaultNamespace
}

// getLoadBalancer gets or creates a load balancer for a namespace.
//

//nolint:ireturn // Returns interface for flexibility
func (r *Router) getLoadBalancer(namespace string) loadbalancer.LoadBalancer {
	r.balancerMu.RLock()
	lb, exists := r.balancers[namespace]
	r.balancerMu.RUnlock()

	if exists {
		return lb
	}

	// Create new load balancer
	r.balancerMu.Lock()
	defer r.balancerMu.Unlock()

	// Double-check after acquiring write lock
	if lb, exists = r.balancers[namespace]; exists {
		return lb
	}

	// Get endpoints
	endpoints := r.discovery.GetEndpoints(namespace)
	if len(endpoints) == 0 {
		return nil
	}

	// Convert to pointer slice
	lbEndpoints := make([]*discovery.Endpoint, len(endpoints))
	for i := range endpoints {
		lbEndpoints[i] = &endpoints[i]
	}

	// Create load balancer based on strategy
	switch r.config.Strategy {
	case "round_robin":
		lb = loadbalancer.NewRoundRobin(lbEndpoints)
	case "least_connections":
		lb = loadbalancer.NewLeastConnections(lbEndpoints)
	case "weighted":
		lb = loadbalancer.NewWeighted(lbEndpoints)
	default:
		lb = loadbalancer.NewRoundRobin(lbEndpoints)
	}

	r.balancers[namespace] = lb

	return lb
}

// getCircuitBreaker gets or creates a circuit breaker for an endpoint.
func (r *Router) getCircuitBreaker(address string) *circuit.CircuitBreaker {
	r.breakerMu.RLock()
	breaker, exists := r.breakers[address]
	r.breakerMu.RUnlock()

	if exists {
		return breaker
	}

	// Create new circuit breaker
	r.breakerMu.Lock()
	defer r.breakerMu.Unlock()

	// Double-check after acquiring write lock
	if breaker, exists = r.breakers[address]; exists {
		return breaker
	}

	// Convert timeout seconds to duration with faster recovery
	timeout := time.Duration(r.config.CircuitBreaker.TimeoutSeconds) * time.Second
	if timeout == 0 || timeout > 10*time.Second {
		// Reduce default timeout from 30s to 10s for faster recovery
		timeout = defaultRetryCount * time.Second
	}

	breaker = circuit.NewCircuitBreaker(
		r.config.CircuitBreaker.FailureThreshold,
		r.config.CircuitBreaker.SuccessThreshold,
		timeout,
	)

	r.breakers[address] = breaker

	return breaker
}

// runHealthChecks periodically checks endpoint health.
func (r *Router) runHealthChecks(ctx context.Context) {
	interval, err := time.ParseDuration(r.config.HealthCheckInterval)
	if err != nil {
		// Use default if parsing fails
		interval = defaultMaxConnections * time.Second // Reduced from 30s for faster failure detection
	}

	if interval == 0 || interval > 30*time.Second {
		// Cap maximum health check interval and use faster default
		interval = defaultMaxConnections * time.Second
	}

	r.logger.Info("Starting health checks", zap.Duration("interval", interval))

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Run initial health check immediately
	r.checkEndpointHealth(ctx)
	r.updateLoadBalancers()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.checkEndpointHealth(ctx)
			r.updateLoadBalancers()
		}
	}
}

// checkEndpointHealth checks the health of all endpoints.
func (r *Router) checkEndpointHealth(ctx context.Context) {
	allEndpoints := r.discovery.GetAllEndpoints()

	var wg sync.WaitGroup

	for _, endpoints := range allEndpoints {
		for i := range endpoints {
			endpoint := &endpoints[i]

			wg.Add(1)

			go func(ep *discovery.Endpoint) {
				defer wg.Done()

				r.checkEndpoint(ctx, ep)
			}(endpoint)
		}
	}

	wg.Wait()
}

// checkEndpoint checks a single endpoint's health.
func (r *Router) checkEndpoint(ctx context.Context, endpoint *discovery.Endpoint) {
	// Use shorter timeout for faster health check response
	healthCtx, cancel := context.WithTimeout(ctx, defaultHealthCheckTimeout) // Reduced from 5s
	defer cancel()

	url := fmt.Sprintf("http://%s:%d/health", endpoint.Address, endpoint.Port)

	req, err := http.NewRequestWithContext(healthCtx, http.MethodGet, url, nil)
	if err != nil {
		endpoint.Healthy = false
		r.logger.Debug("Failed to create health check request",
			zap.String("address", endpoint.Address),
			zap.Error(err),
		)

		return
	}

	resp, err := r.httpClient.Do(req)
	if err != nil {
		endpoint.Healthy = false
		r.logger.Debug("Endpoint health check failed",
			zap.String("address", endpoint.Address),
			zap.Error(err),
		)

		// Immediately mark circuit breaker as failed for faster error propagation
		if breaker := r.getCircuitBreaker(endpoint.Address); breaker != nil {
			_ = breaker.Call(func() error {
				return fmt.Errorf("health check failed: %w", err)
			})
		}

		return
	}

	defer func() {
		if err := resp.Body.Close(); err != nil {
			r.logger.Warn("failed to close health check response body", zap.Error(err))
		}
	}()

	wasHealthy := endpoint.Healthy
	endpoint.Healthy = resp.StatusCode == http.StatusOK

	// Log health status changes for better observability
	if wasHealthy != endpoint.Healthy {
		r.logger.Info("Endpoint health status changed",
			zap.String("address", endpoint.Address),
			zap.Bool("healthy", endpoint.Healthy),
			zap.Int("status_code", resp.StatusCode),
		)
	}
}

// updateLoadBalancers updates load balancers with current endpoint state.
func (r *Router) updateLoadBalancers() {
	r.balancerMu.Lock()
	defer r.balancerMu.Unlock()

	// Clear and recreate load balancers
	r.balancers = make(map[string]loadbalancer.LoadBalancer)
}

// GetRequestCount returns the total request count.
func (r *Router) GetRequestCount() uint64 {
	return atomic.LoadUint64(&r.requestCounter)
}
