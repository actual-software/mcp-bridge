// Package router provides HTTP request routing and load balancing functionality for the MCP gateway.
package router

import (
	"bufio"
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

	"github.com/actual-software/mcp-bridge/services/gateway/internal/common"
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

// Protocol scheme constants.
const (
	schemeWSS        = "wss"
	schemeHTTP       = "http"
	defaultNamespace = "default"
)

const (
	defaultMaxConnections      = 5
	defaultMaxRetries          = 100
	defaultRetryCount          = 10
	defaultBufferSize          = 1024
	defaultTimeoutSeconds      = 30
	defaultMaxIdleConnsPerHost = 5

	// Timeout constants.
	defaultDialTimeout           = 2 * time.Second
	defaultTLSHandshakeTimeout   = 2 * time.Second
	defaultResponseHeaderTimeout = 3 * time.Second
	defaultHealthCheckTimeout    = 2 * time.Second
)

// Router routes MCP requests to appropriate backend servers.
// sseStream represents a persistent SSE connection to a backend MCP server.
// It manages the session ID, connection, and pending requests for MCP over HTTP with SSE pattern.
type sseStream struct {
	sessionID       string                             // Backend session ID from initial GET request
	conn            *http.Response                     // HTTP response with open SSE stream
	reader          *bufio.Reader                      // Buffered reader for parsing SSE events
	pendingRequests map[interface{}]chan *mcp.Response // Maps request ID to response channel
	pendingMu       sync.Mutex                         // Protects pendingRequests map
	ctx             context.Context                    // Stream context
	cancel          context.CancelFunc                 // Function to cancel stream
}

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

	// HTTP clients per endpoint for proper connection lifecycle management
	endpointClients  map[string]*http.Client
	endpointClientMu sync.RWMutex

	// Backend session tracking for stateful HTTP backends
	// Maps: frontendSessionID -> endpointKey -> backendSessionID
	backendSessions   map[string]map[string]string
	backendSessionsMu sync.RWMutex

	// SSE stream management for MCP over HTTP with SSE backends
	// Maps: endpointURL -> sseStream
	sseStreams   map[string]*sseStream
	sseStreamsMu sync.RWMutex

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
		config:          cfg,
		discovery:       discovery,
		metrics:         metrics,
		logger:          logger,
		balancers:       make(map[string]loadbalancer.LoadBalancer),
		breakers:        make(map[string]*circuit.CircuitBreaker),
		endpointClients: make(map[string]*http.Client),
		backendSessions: make(map[string]map[string]string),
		sseStreams:      make(map[string]*sseStream),
	}

	// Register callback to invalidate load balancer cache when endpoints change
	discovery.RegisterEndpointChangeCallback(func(namespace string) {
		r.invalidateLoadBalancer(namespace)
	})

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

// logSessionContext logs session context at various points for debugging.
func (r *Router) logSessionContext(ctx context.Context, stage, method string) {
	if sess, ok := ctx.Value(common.SessionContextKey).(*session.Session); ok {
		r.logger.Debug("DEBUG: Session found at "+stage,
			zap.String("session_id", sess.ID),
			zap.String("user", sess.User),
			zap.String("method", method))
	} else {
		r.logger.Warn("DEBUG: NO SESSION at "+stage,
			zap.String("method", method))
	}
}

// recordRequestMetrics records success/error metrics and span information.
func (r *Router) recordRequestMetrics(span opentracing.Span, req *mcp.Request, err error, duration time.Duration) {
	span.SetTag("duration.ms", duration.Milliseconds())
	if err != nil {
		r.metrics.RecordRequestDuration(req.Method, "error", duration)
		ext.Error.Set(span, true)
		span.LogKV("error", err.Error())
	} else {
		r.metrics.RecordRequestDuration(req.Method, "success", duration)
	}
}

func (r *Router) RouteRequest(ctx context.Context, req *mcp.Request, targetNamespace string) (*mcp.Response, error) {
	startTime := time.Now()

	// Debug session tracking
	r.logSessionContext(ctx, "start of RouteRequest", req.Method)
	ctx = EnrichContextWithRequest(ctx, req, targetNamespace)
	r.logSessionContext(ctx, "after EnrichContextWithRequest", req.Method)

	// Start tracing span - preserve session explicitly to work around context propagation issues
	sess, _ := ctx.Value(common.SessionContextKey).(*session.Session)
	span, ctx := opentracing.StartSpanFromContext(ctx, "router.RouteRequest")
	if sess != nil {
		ctx = context.WithValue(ctx, common.SessionContextKey, sess)
	}
	defer span.Finish()
	r.logSessionContext(ctx, "after StartSpanFromContext", req.Method)

	// Set span tags and extract namespace
	span.SetTag("mcp.method", req.Method)
	span.SetTag("mcp.id", req.ID)
	span.SetTag("target.namespace", targetNamespace)
	if targetNamespace == "" {
		targetNamespace = r.extractNamespace(req)
		span.SetTag("target.namespace.extracted", targetNamespace)
	}

	// Select endpoint
	endpoint, err := r.selectEndpoint(targetNamespace, span)
	if err != nil {
		wrappedErr := errors.FromContext(ctx, err)
		if r.metrics != nil {
			errors.RecordError(wrappedErr, r.metrics)
		}

		return nil, wrappedErr
	}

	// Execute request through circuit breaker
	resp, err := r.executeWithCircuitBreaker(ctx, endpoint, req)
	if err != nil {
		err = WrapForwardingError(ctx, err, endpoint, req.Method)
		if r.metrics != nil {
			errors.RecordError(err, r.metrics)
		}
	}

	// Record metrics and return
	r.recordRequestMetrics(span, req, err, time.Since(startTime))
	if err != nil {
		return nil, err
	}

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
	case schemeHTTP, "https", "sse":
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

	if sess, ok := ctx.Value(common.SessionContextKey).(*session.Session); ok {
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
	// Check if this endpoint uses SSE session pattern
	if endpoint.Scheme == "sse" {
		span.LogKV("routing.method", "sse_session")

		return r.forwardRequestViaSSE(ctx, endpoint, req, span)
	}

	// Otherwise use standard HTTP request/response
	span.LogKV("routing.method", "http_standard")

	// Prepare the HTTP request
	httpReq, err := r.prepareHTTPRequest(ctx, endpoint, req, span)
	if err != nil {
		return nil, err
	}

	// Enhance request with session and tracing
	r.enhanceHTTPRequest(ctx, httpReq, span)

	// Get or create HTTP client for this endpoint
	client := r.getOrCreateHTTPClient(endpoint)

	// Send request and get response
	httpResp, err := client.Do(httpReq)
	if err != nil {
		return nil, WrapHTTPError(ctx, err, "request", endpoint)
	}
	defer r.closeResponseBody(httpResp)

	// Extract backend session ID from response headers if present
	// This supports stateful HTTP backends like Serena that maintain their own sessions
	// Per MCP spec, the header name is "Mcp-Session-Id" (case-insensitive matching)
	backendSessionID := httpResp.Header.Get("Mcp-Session-Id")
	if backendSessionID != "" {
		if sess, ok := ctx.Value(common.SessionContextKey).(*session.Session); ok {
			endpointURL := httpReq.URL.String()
			r.storeBackendSessionID(sess.ID, endpointURL, backendSessionID)
			r.logger.Info("Received backend session ID from HTTP response",
				zap.String("frontend_session_id", sess.ID),
				zap.String("backend_session_id", backendSessionID),
				zap.String("endpoint", endpointURL),
				zap.Int("status_code", httpResp.StatusCode))
		}
	} else {
		// Log when no backend session ID is present (helps debug if server isn't sending it)
		if sess, ok := ctx.Value(common.SessionContextKey).(*session.Session); ok {
			r.logger.Debug("No backend session ID in HTTP response",
				zap.String("frontend_session_id", sess.ID),
				zap.String("endpoint", httpReq.URL.String()),
				zap.Int("status_code", httpResp.StatusCode),
				zap.Strings("response_headers", getHeaderKeys(httpResp.Header)))
		}
	}

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
	httpReq.Header.Set("Accept", "text/event-stream, application/json")

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
	// DEBUG: Check context value directly
	r.logger.Debug("DEBUG: enhanceHTTPRequest checking for session",
		zap.String("url", httpReq.URL.String()),
		zap.Bool("has_session", ctx.Value(common.SessionContextKey) != nil))

	// Add session info if available
	if sess, ok := ctx.Value(common.SessionContextKey).(*session.Session); ok {
		// Check if we have a backend-specific session ID for this endpoint
		endpointURL := httpReq.URL.String()
		backendSessionID := r.getBackendSessionID(sess.ID, endpointURL)

		// Use backend session ID if available, otherwise use frontend session ID
		sessionIDToUse := sess.ID
		if backendSessionID != "" {
			sessionIDToUse = backendSessionID
			r.logger.Info("Sending HTTP request with CACHED backend session ID",
				zap.String("frontend_session_id", sess.ID),
				zap.String("backend_session_id", backendSessionID),
				zap.String("header_name", "Mcp-Session-Id"),
				zap.String("user", sess.User),
				zap.String("endpoint", endpointURL))
		} else {
			r.logger.Info("Sending HTTP request WITHOUT backend session ID (first request or new backend)",
				zap.String("frontend_session_id", sess.ID),
				zap.String("header_name", "Mcp-Session-Id"),
				zap.String("user", sess.User),
				zap.String("endpoint", endpointURL))
		}

		// Use MCP spec-compliant header name: Mcp-Session-Id
		// This is the official header name per the MCP Streamable HTTP transport spec
		httpReq.Header.Set("Mcp-Session-Id", sessionIDToUse)
		httpReq.Header.Set("X-MCP-User", sess.User)
		span.SetTag("session.id", sessionIDToUse)
		span.SetTag("session.user", sess.User)
	} else {
		r.logger.Warn("No session found in context for HTTP request",
			zap.String("url", httpReq.URL.String()))
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

// getHeaderKeys returns all header keys from an HTTP header map for debugging.
func getHeaderKeys(headers http.Header) []string {
	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}

	return keys
}

// getBackendSessionID retrieves the backend session ID for a specific endpoint.
// Returns empty string if no backend session exists.
func (r *Router) getBackendSessionID(frontendSessionID, endpointURL string) string {
	r.backendSessionsMu.RLock()
	defer r.backendSessionsMu.RUnlock()

	if endpointSessions, ok := r.backendSessions[frontendSessionID]; ok {
		return endpointSessions[endpointURL]
	}

	return ""
}

// storeBackendSessionID stores the backend session ID for a specific endpoint.
func (r *Router) storeBackendSessionID(frontendSessionID, endpointURL, backendSessionID string) {
	r.backendSessionsMu.Lock()
	defer r.backendSessionsMu.Unlock()

	if _, ok := r.backendSessions[frontendSessionID]; !ok {
		r.backendSessions[frontendSessionID] = make(map[string]string)
	}

	r.backendSessions[frontendSessionID][endpointURL] = backendSessionID

	r.logger.Debug("Stored backend session ID",
		zap.String("frontend_session_id", frontendSessionID),
		zap.String("endpoint_url", endpointURL),
		zap.String("backend_session_id", backendSessionID))
}

// ClearBackendSessions removes all backend session mappings for a given frontend session.
// This should be called when a frontend client disconnects to prevent memory leaks.
func (r *Router) ClearBackendSessions(frontendSessionID string) {
	r.backendSessionsMu.Lock()
	defer r.backendSessionsMu.Unlock()

	if sessions, ok := r.backendSessions[frontendSessionID]; ok {
		// Log each backend session being cleared for debugging
		for endpoint, backendSessionID := range sessions {
			r.logger.Debug("Clearing backend session mapping",
				zap.String("frontend_session_id", frontendSessionID),
				zap.String("endpoint", endpoint),
				zap.String("backend_session_id", backendSessionID))
		}

		r.logger.Info("Cleared all backend sessions for frontend session",
			zap.String("frontend_session_id", frontendSessionID),
			zap.Int("backend_session_count", len(sessions)))
		delete(r.backendSessions, frontendSessionID)
	} else {
		r.logger.Debug("No backend sessions to clear for frontend session",
			zap.String("frontend_session_id", frontendSessionID))
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

	// Check if response is SSE format
	contentType := httpResp.Header.Get("Content-Type")
	if strings.Contains(contentType, "text/event-stream") {
		// Parse SSE response
		return r.parseSSEResponse(respBody, span)
	}

	// Parse JSON response
	var resp mcp.Response
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	span.LogKV("http.success", true)

	return &resp, nil
}

// parseSSEResponse parses an SSE (Server-Sent Events) response and extracts the JSON data.
func (r *Router) parseSSEResponse(respBody []byte, span opentracing.Span) (*mcp.Response, error) {
	// SSE format: lines starting with "data: " contain the JSON payload
	lines := strings.Split(string(respBody), "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Look for data lines in SSE format
		if strings.HasPrefix(line, "data: ") {
			// Extract JSON from the data line
			jsonData := strings.TrimPrefix(line, "data: ")

			// Parse the JSON response
			var resp mcp.Response
			if err := json.Unmarshal([]byte(jsonData), &resp); err != nil {
				return nil, fmt.Errorf("failed to parse SSE data as JSON: %w", err)
			}

			span.LogKV("http.success", true, "format", "sse")

			return &resp, nil
		}
	}

	return nil, fmt.Errorf("no data line found in SSE response")
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
	healthCtx, cancel := context.WithTimeout(ctx, defaultHealthCheckTimeout)
	defer cancel()

	url := r.buildHealthCheckURL(endpoint)
	req, err := http.NewRequestWithContext(healthCtx, http.MethodGet, url, nil)
	if err != nil {
		endpoint.SetHealthy(false)
		r.logger.Debug("Failed to create health check request",
			zap.String("address", endpoint.Address),
			zap.Error(err),
		)

		return
	}

	req.Header.Set("Accept", "text/event-stream, application/json")

	client := r.getOrCreateHTTPClient(endpoint)
	resp, err := client.Do(req)
	if err != nil {
		r.handleHealthCheckError(endpoint, err)

		return
	}

	defer func() {
		if err := resp.Body.Close(); err != nil {
			r.logger.Warn("failed to close health check response body", zap.Error(err))
		}
	}()

	r.updateEndpointHealth(endpoint, resp.StatusCode)
}

// buildHealthCheckURL constructs the health check URL for an endpoint.
func (r *Router) buildHealthCheckURL(endpoint *discovery.Endpoint) string {
	healthPath := "/health"
	if endpoint.Path != "" {
		healthPath = endpoint.Path
	}

	scheme := "http"
	if endpoint.Scheme != "" {
		scheme = endpoint.Scheme
	}

	return fmt.Sprintf("%s://%s:%d%s", scheme, endpoint.Address, endpoint.Port, healthPath)
}

// handleHealthCheckError marks an endpoint as unhealthy and triggers circuit breaker.
func (r *Router) handleHealthCheckError(endpoint *discovery.Endpoint, err error) {
	endpoint.SetHealthy(false)
	r.logger.Debug("Endpoint health check failed",
		zap.String("address", endpoint.Address),
		zap.Error(err),
	)

	if breaker := r.getCircuitBreaker(endpoint.Address); breaker != nil {
		_ = breaker.Call(func() error {
			return fmt.Errorf("health check failed: %w", err)
		})
	}
}

// updateEndpointHealth updates endpoint health status and logs changes.
func (r *Router) updateEndpointHealth(endpoint *discovery.Endpoint, statusCode int) {
	wasHealthy := endpoint.IsHealthy()
	isHealthy := statusCode == http.StatusOK
	endpoint.SetHealthy(isHealthy)

	if wasHealthy != isHealthy {
		r.logger.Info("Endpoint health status changed",
			zap.String("address", endpoint.Address),
			zap.Bool("healthy", isHealthy),
			zap.Int("status_code", statusCode),
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

// invalidateLoadBalancer invalidates the load balancer cache for a specific namespace.
func (r *Router) invalidateLoadBalancer(namespace string) {
	r.balancerMu.Lock()
	delete(r.balancers, namespace)
	r.balancerMu.Unlock()

	// Close HTTP clients for endpoints in this namespace
	r.closeEndpointClientsForNamespace(namespace)

	r.logger.Info("Load balancer invalidated due to endpoint change", zap.String("namespace", namespace))
}

// getEndpointKey generates a unique key for an endpoint.
func getEndpointKey(endpoint *discovery.Endpoint) string {
	return fmt.Sprintf("%s:%d", endpoint.Address, endpoint.Port)
}

// getOrCreateHTTPClient returns an HTTP client for the given endpoint, creating it if necessary.
func (r *Router) getOrCreateHTTPClient(endpoint *discovery.Endpoint) *http.Client {
	key := getEndpointKey(endpoint)

	// Try read lock first for fast path
	r.endpointClientMu.RLock()
	if client, exists := r.endpointClients[key]; exists {
		r.endpointClientMu.RUnlock()

		return client
	}
	r.endpointClientMu.RUnlock()

	// Create new client with write lock
	r.endpointClientMu.Lock()
	defer r.endpointClientMu.Unlock()

	// Double-check after acquiring write lock
	if client, exists := r.endpointClients[key]; exists {
		return client
	}

	// Create new HTTP client for this endpoint
	client := &http.Client{
		Timeout: defaultMaxConnections * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        defaultMaxRetries,
			MaxIdleConnsPerHost: defaultMaxIdleConnsPerHost, // Reduced from 10 for better scaling with many endpoints
			IdleConnTimeout:     defaultTimeoutSeconds * time.Second,
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				d := &net.Dialer{Timeout: defaultDialTimeout}

				return d.DialContext(ctx, network, addr)
			},
			TLSHandshakeTimeout:   defaultTLSHandshakeTimeout,
			ResponseHeaderTimeout: defaultResponseHeaderTimeout,
		},
	}

	r.endpointClients[key] = client
	r.logger.Debug("Created HTTP client for endpoint",
		zap.String("endpoint", key),
		zap.String("namespace", endpoint.Namespace))

	return client
}

// closeEndpointClientsForNamespace closes and removes HTTP clients for endpoints no longer active in ANY namespace.
func (r *Router) closeEndpointClientsForNamespace(namespace string) {
	// Get ALL active endpoints across ALL namespaces to avoid closing clients still in use
	allActiveKeys := make(map[string]bool)

	// Get list of namespaces with proper locking to avoid concurrent map iteration
	r.balancerMu.RLock()
	namespaces := make([]string, 0, len(r.balancers))
	for ns := range r.balancers {
		namespaces = append(namespaces, ns)
	}
	r.balancerMu.RUnlock()

	// We need to check all namespaces because endpoint keys don't include namespace
	// (multiple namespaces could have endpoints with same address:port)
	for _, ns := range namespaces {
		endpoints := r.discovery.GetEndpoints(ns)
		for _, endpoint := range endpoints {
			key := getEndpointKey(&endpoint)
			allActiveKeys[key] = true
		}
	}

	r.endpointClientMu.Lock()
	defer r.endpointClientMu.Unlock()

	// Close HTTP clients for endpoints that are no longer active in ANY namespace
	closedCount := 0
	for key, client := range r.endpointClients {
		// Only close if this endpoint is not active in any namespace
		if !allActiveKeys[key] {
			// Close idle connections for this endpoint
			client.CloseIdleConnections()
			delete(r.endpointClients, key)
			closedCount++

			r.logger.Debug("Closed HTTP client for removed endpoint",
				zap.String("endpoint", key),
				zap.String("triggering_namespace", namespace))
		}
	}

	if closedCount > 0 {
		r.logger.Debug("Closed HTTP clients after namespace change",
			zap.String("triggering_namespace", namespace),
			zap.Int("count", closedCount))
	}
}

// GetRequestCount returns the total request count.
func (r *Router) GetRequestCount() uint64 {
	return atomic.LoadUint64(&r.requestCounter)
}

// establishSSESession establishes an SSE session with a backend MCP server.
// It sends a GET request with Accept: text/event-stream to initiate the session,
// extracts the session ID from the response, and starts reading from the SSE stream.
func (r *Router) establishSSESession(ctx context.Context, endpoint *discovery.Endpoint) (*sseStream, error) {
	// Build URL for session establishment
	url := r.buildHTTPURL(endpoint)

	// Create GET request for session establishment
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create session request: %w", err)
	}

	// Set Accept header to request SSE stream
	req.Header.Set("Accept", "text/event-stream")

	// Get HTTP client for this endpoint
	client := r.getOrCreateHTTPClient(endpoint)

	r.logger.Info("Establishing SSE session with backend",
		zap.String("endpoint", url),
		zap.String("method", "GET"))

	// Send GET request to establish session
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("session establishment failed: %w", err)
	}

	// Check response status
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		return nil, fmt.Errorf("session establishment returned %d: %s", resp.StatusCode, string(body))
	}

	// Extract session ID from response header
	sessionID := resp.Header.Get("Mcp-Session-Id")
	if sessionID == "" {
		_ = resp.Body.Close()

		return nil, fmt.Errorf("no session ID received from backend (missing Mcp-Session-Id header)")
	}

	r.logger.Info("SSE session established successfully",
		zap.String("endpoint", url),
		zap.String("session_id", sessionID),
		zap.String("content_type", resp.Header.Get("Content-Type")))

	// Create cancellable context for the stream that inherits from parent
	// but will outlive the initial request
	streamCtx, cancel := context.WithCancel(ctx)

	// Create sseStream struct
	stream := &sseStream{
		sessionID:       sessionID,
		conn:            resp,
		reader:          bufio.NewReader(resp.Body),
		pendingRequests: make(map[interface{}]chan *mcp.Response),
		ctx:             streamCtx,
		cancel:          cancel,
	}

	// Start goroutine to read from SSE stream
	go r.readSSEStream(streamCtx, url, stream)

	return stream, nil
}

// readSSEStream reads from an SSE stream and delivers responses to pending requests.
// This runs as a goroutine for each SSE connection.
//
//nolint:funlen // Complex stream reading logic benefits from being in one function
func (r *Router) readSSEStream(ctx context.Context, endpointURL string, stream *sseStream) {
	defer func() {
		// Clean up when stream ends
		_ = stream.conn.Body.Close()

		// Cancel any pending requests
		stream.pendingMu.Lock()
		for requestID, ch := range stream.pendingRequests {
			close(ch)
			r.logger.Warn("SSE stream closed with pending request",
				zap.String("endpoint", endpointURL),
				zap.String("session_id", stream.sessionID),
				zap.Any("request_id", requestID))
		}
		stream.pendingMu.Unlock()

		// Remove stream from router's map
		r.sseStreamsMu.Lock()
		delete(r.sseStreams, endpointURL)
		r.sseStreamsMu.Unlock()

		r.logger.Info("SSE stream closed",
			zap.String("endpoint", endpointURL),
			zap.String("session_id", stream.sessionID))
	}()

	r.logger.Info("Starting to read SSE stream",
		zap.String("endpoint", endpointURL),
		zap.String("session_id", stream.sessionID))

	for {
		select {
		case <-ctx.Done():
			r.logger.Info("SSE stream context cancelled",
				zap.String("endpoint", endpointURL))

			return
		default:
		}

		// Read line from SSE stream
		line, err := stream.reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				r.logger.Info("SSE stream EOF",
					zap.String("endpoint", endpointURL),
					zap.String("session_id", stream.sessionID))
			} else {
				r.logger.Error("SSE stream read error",
					zap.String("endpoint", endpointURL),
					zap.String("session_id", stream.sessionID),
					zap.Error(err))
			}

			return
		}

		line = strings.TrimSpace(line)

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}

		// Parse SSE event
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")

			// Parse MCP response
			var resp mcp.Response
			if err := json.Unmarshal([]byte(data), &resp); err != nil {
				r.logger.Error("Failed to parse SSE response",
					zap.String("endpoint", endpointURL),
					zap.String("session_id", stream.sessionID),
					zap.String("data", data),
					zap.Error(err))

				continue
			}

			r.logger.Debug("Received SSE response",
				zap.String("endpoint", endpointURL),
				zap.String("session_id", stream.sessionID),
				zap.Any("response_id", resp.ID))

			// Deliver response to waiting request
			stream.pendingMu.Lock()
			if ch, ok := stream.pendingRequests[resp.ID]; ok {
				select {
				case ch <- &resp:
					r.logger.Debug("Delivered response to pending request",
						zap.String("endpoint", endpointURL),
						zap.Any("response_id", resp.ID))
				default:
					r.logger.Warn("Response channel full or closed",
						zap.String("endpoint", endpointURL),
						zap.Any("response_id", resp.ID))
				}
				delete(stream.pendingRequests, resp.ID)
			} else {
				r.logger.Warn("Received response for unknown request ID",
					zap.String("endpoint", endpointURL),
					zap.String("session_id", stream.sessionID),
					zap.Any("response_id", resp.ID))
			}
			stream.pendingMu.Unlock()
		}
	}
}

// forwardRequestViaSSE forwards a request to a backend MCP server via SSE session.
// It establishes an SSE session if needed, sends the request via POST with the session ID,
// and waits for the response to arrive via the SSE stream.
//
//nolint:funlen // Complex SSE session management with error handling needs multiple statements
func (r *Router) forwardRequestViaSSE(
	ctx context.Context,
	endpoint *discovery.Endpoint,
	req *mcp.Request,
	span opentracing.Span,
) (*mcp.Response, error) {
	endpointURL := r.buildHTTPURL(endpoint)

	// Get or establish SSE stream
	r.sseStreamsMu.Lock()
	stream, exists := r.sseStreams[endpointURL]
	if !exists {
		// Release lock while establishing session (may take time)
		r.sseStreamsMu.Unlock()

		r.logger.Info("Establishing new SSE session",
			zap.String("endpoint", endpointURL))

		newStream, err := r.establishSSESession(ctx, endpoint)
		if err != nil {
			return nil, fmt.Errorf("failed to establish SSE session: %w", err)
		}

		r.sseStreamsMu.Lock()
		r.sseStreams[endpointURL] = newStream
		stream = newStream
	}
	r.sseStreamsMu.Unlock()

	span.SetTag("sse.session_id", stream.sessionID)
	span.SetTag("sse.endpoint", endpointURL)

	// Prepare POST request with session ID
	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpointURL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Mcp-Session-Id", stream.sessionID)

	// Add tracing headers
	if err := tracing.InjectHTTPHeaders(span, httpReq); err != nil {
		span.LogKV("warning", "failed to inject tracing headers", "err", err)
	}

	r.logger.Info("Sending request via SSE session",
		zap.String("endpoint", endpointURL),
		zap.String("session_id", stream.sessionID),
		zap.String("method", req.Method),
		zap.Any("request_id", req.ID))

	// Create channel for response
	respChan := make(chan *mcp.Response, 1)
	stream.pendingMu.Lock()
	stream.pendingRequests[req.ID] = respChan
	stream.pendingMu.Unlock()

	// Send POST request
	client := r.getOrCreateHTTPClient(endpoint)
	httpResp, err := client.Do(httpReq)
	if err != nil {
		// Remove pending request on error
		stream.pendingMu.Lock()
		delete(stream.pendingRequests, req.ID)
		stream.pendingMu.Unlock()

		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer func() { _ = httpResp.Body.Close() }()

	// Check that POST was accepted (200 or 202)
	if httpResp.StatusCode != http.StatusOK && httpResp.StatusCode != http.StatusAccepted {
		// Remove pending request on error
		stream.pendingMu.Lock()
		delete(stream.pendingRequests, req.ID)
		stream.pendingMu.Unlock()

		body, _ := io.ReadAll(httpResp.Body)
		return nil, fmt.Errorf("backend returned status %d: %s", httpResp.StatusCode, string(body))
	}

	r.logger.Debug("Request sent successfully, waiting for SSE response",
		zap.String("endpoint", endpointURL),
		zap.Any("request_id", req.ID),
		zap.Int("http_status", httpResp.StatusCode))

	// Wait for response via SSE stream
	select {
	case resp := <-respChan:
		if resp == nil {
			return nil, fmt.Errorf("SSE stream closed while waiting for response")
		}
		r.logger.Info("Received response via SSE stream",
			zap.String("endpoint", endpointURL),
			zap.Any("request_id", req.ID),
			zap.Any("response_id", resp.ID))
		span.LogKV("sse.success", true)
		return resp, nil
	case <-ctx.Done():
		// Remove pending request on timeout
		stream.pendingMu.Lock()
		delete(stream.pendingRequests, req.ID)
		stream.pendingMu.Unlock()
		return nil, fmt.Errorf("context cancelled while waiting for SSE response: %w", ctx.Err())
	}
}
