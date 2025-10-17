package sse

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	nethttp "net/http"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	authpkg "github.com/actual-software/mcp-bridge/services/gateway/internal/auth"
	customerrors "github.com/actual-software/mcp-bridge/services/gateway/internal/errors"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/frontends/types"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/logging"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/session"
	"github.com/actual-software/mcp-bridge/services/router/pkg/mcp"
)

type contextKey string

const (
	contextKeySession   contextKey = "session"
	shutdownGracePeriod            = 30 * time.Second
)

// Wire message represents the wire protocol message format.
type WireMessage struct {
	ID              interface{} `json:"id"`
	Timestamp       string      `json:"timestamp"`
	Source          string      `json:"source"`
	TargetNamespace string      `json:"target_namespace,omitempty"`
	MCPPayload      interface{} `json:"mcp_payload"`
	AuthToken       string      `json:"auth_token,omitempty"`
}

// StreamConnection represents an SSE stream connection.
type StreamConnection struct {
	ID       string
	Session  *session.Session
	Writer   nethttp.ResponseWriter
	Flusher  nethttp.Flusher
	EventCh  chan []byte
	Created  time.Time
	LastUsed time.Time
	ctx      context.Context
	cancel   context.CancelFunc
	mu       sync.RWMutex
	logger   *zap.Logger
	closed   bool
}

// Close closes the stream connection.
func (s *StreamConnection) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}

	s.closed = true

	if s.cancel != nil {
		s.cancel()
	}

	close(s.EventCh)

	return nil
}

// Frontend implements the SSE frontend.
type Frontend struct {
	name     string
	config   Config
	logger   *zap.Logger
	router   types.RequestRouter
	auth     types.AuthProvider
	sessions types.SessionManager

	// HTTP server
	server *nethttp.Server
	mux    *nethttp.ServeMux

	// Stream management
	streams   map[string]*StreamConnection
	streamsMu sync.RWMutex
	connCount int64

	// Request correlation
	pendingRequests map[string]chan *mcp.Response

	// State management
	running bool
	mu      sync.RWMutex

	// Metrics
	metrics   types.FrontendMetrics
	metricsMu sync.RWMutex

	// Shutdown coordination
	ctx        context.Context
	cancel     context.CancelFunc
	shutdownCh chan struct{}
	wg         sync.WaitGroup
}

// CreateSSEFrontend creates a new SSE frontend instance.
func CreateSSEFrontend(
	name string,
	config Config,
	router types.RequestRouter,
	auth types.AuthProvider,
	sessions types.SessionManager,
	logger *zap.Logger,
) *Frontend {
	config.ApplyDefaults()

	ctx, cancel := context.WithCancel(context.Background())

	return &Frontend{
		name:            name,
		config:          config,
		logger:          logger.With(zap.String("frontend", name), zap.String("protocol", "sse")),
		router:          router,
		auth:            auth,
		sessions:        sessions,
		mux:             nethttp.NewServeMux(),
		streams:         make(map[string]*StreamConnection),
		pendingRequests: make(map[string]chan *mcp.Response),
		ctx:             ctx,
		cancel:          cancel,
		shutdownCh:      make(chan struct{}),
	}
}

// Start initializes the frontend (routes and background workers only).
// The HTTP server is managed externally via SetServer() for shared server support.
func (f *Frontend) Start(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.running {
		return fmt.Errorf("sse frontend already running")
	}

	// Setup routes on internal mux
	f.mux.HandleFunc(f.config.StreamEndpoint, f.handleSSEStream)
	f.mux.HandleFunc(f.config.RequestEndpoint, f.handleRequest)
	f.mux.HandleFunc("/health", f.handleHealth)

	// Start keepalive goroutine
	f.wg.Add(1)
	go f.keepAliveLoop()

	f.running = true
	f.updateMetrics(func(m *types.FrontendMetrics) {
		m.IsRunning = true
	})

	addr := fmt.Sprintf("%s:%d", f.config.Host, f.config.Port)
	f.logger.Info("sse frontend initialized",
		zap.String("address", addr),
		zap.String("stream_endpoint", f.config.StreamEndpoint),
		zap.String("request_endpoint", f.config.RequestEndpoint),
		zap.Bool("tls", f.config.TLS.Enabled))

	return nil
}

// Stop gracefully shuts down the frontend.
func (f *Frontend) Stop(ctx context.Context) error {
	f.mu.Lock()
	if !f.running {
		f.mu.Unlock()

		return nil
	}
	f.running = false
	f.mu.Unlock()

	f.logger.Info("stopping sse frontend")

	// Signal shutdown
	close(f.shutdownCh)
	f.cancel()

	// Close all streams
	f.closeAllStreams()

	// Note: HTTP server is externally managed, so we don't shutdown it here

	// Wait for goroutines to finish
	done := make(chan struct{})
	go func() {
		f.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		f.logger.Info("sse frontend stopped gracefully")
	case <-time.After(shutdownGracePeriod):
		f.logger.Warn("sse frontend shutdown timeout")
	}

	f.updateMetrics(func(m *types.FrontendMetrics) {
		m.IsRunning = false
		m.ActiveConnections = 0
	})

	return nil
}

// GetName returns the frontend name.
func (f *Frontend) GetName() string {
	return f.name
}

// GetProtocol returns the frontend protocol type.
func (f *Frontend) GetProtocol() string {
	return "sse"
}

// GetMetrics returns frontend-specific metrics.
func (f *Frontend) GetMetrics() types.FrontendMetrics {
	f.metricsMu.RLock()
	defer f.metricsMu.RUnlock()

	return f.metrics
}

// GetHandler returns the HTTP handler for this frontend.
func (f *Frontend) GetHandler() nethttp.Handler {
	return f.mux
}

// GetAddress returns the host:port address this frontend listens on.
func (f *Frontend) GetAddress() string {
	return fmt.Sprintf("%s:%d", f.config.Host, f.config.Port)
}

// SetServer injects the shared HTTP server into this frontend.
func (f *Frontend) SetServer(server *nethttp.Server) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.server = server
}

// handleSSEStream handles SSE stream connections.
func (f *Frontend) handleSSEStream(w nethttp.ResponseWriter, r *nethttp.Request) {
	if !f.validateGetMethod(w, r.Method) {
		return
	}

	// Initialize stream context with tracing
	traceID := logging.GenerateTraceID()
	requestID := logging.GenerateRequestID()
	ctx := logging.ContextWithTracing(r.Context(), traceID, requestID)

	flusher, ok := f.validateSSESupport(w)
	if !ok {
		return
	}

	if !f.checkConnectionLimit(w) {
		return
	}

	authClaims, ok := f.authenticateStreamRequest(w, r, ctx)
	if !ok {
		return
	}

	sess, ok := f.createStreamSession(w, ctx, authClaims)
	if !ok {
		return
	}

	f.setSSEHeaders(w)

	stream := f.createAndRegisterStream(sess, w, flusher, authClaims)

	flusher.Flush()
	f.handleStreamLoop(stream)
	f.removeStream(sess.ID)
}

func (f *Frontend) validateGetMethod(w nethttp.ResponseWriter, method string) bool {
	if method != nethttp.MethodGet {
		f.sendHTTPError(w, nethttp.StatusMethodNotAllowed, "method not allowed")
		f.updateMetrics(func(m *types.FrontendMetrics) {
			m.ErrorCount++
		})

		return false
	}

	return true
}

func (f *Frontend) validateSSESupport(w nethttp.ResponseWriter) (nethttp.Flusher, bool) {
	flusher, ok := w.(nethttp.Flusher)
	if !ok {
		nethttp.Error(w, "SSE not supported", nethttp.StatusInternalServerError)

		return nil, false
	}

	return flusher, true
}

func (f *Frontend) checkConnectionLimit(w nethttp.ResponseWriter) bool {
	currentCount := atomic.LoadInt64(&f.connCount)
	if currentCount >= int64(f.config.MaxConnections) {
		nethttp.Error(w, "Connection limit reached", nethttp.StatusServiceUnavailable)
		f.updateMetrics(func(m *types.FrontendMetrics) {
			m.ErrorCount++
		})

		return false
	}

	return true
}

func (f *Frontend) authenticateStreamRequest(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	ctx context.Context,
) (*authpkg.Claims, bool) {
	authClaims, err := f.auth.Authenticate(r)
	if err != nil {
		logging.LogError(ctx, f.logger, "Authentication failed", err)
		nethttp.Error(w, "Unauthorized", nethttp.StatusUnauthorized)
		f.updateMetrics(func(m *types.FrontendMetrics) {
			m.ErrorCount++
		})

		return nil, false
	}

	return authClaims, true
}

func (f *Frontend) createStreamSession(
	w nethttp.ResponseWriter,
	ctx context.Context,
	authClaims *authpkg.Claims,
) (*session.Session, bool) {
	sess, err := f.sessions.CreateSession(authClaims)
	if err != nil {
		logging.LogError(ctx, f.logger, "Failed to create session", err)
		nethttp.Error(w, "Failed to create session", nethttp.StatusInternalServerError)
		f.updateMetrics(func(m *types.FrontendMetrics) {
			m.ErrorCount++
		})

		return nil, false
	}

	return sess, true
}

func (f *Frontend) setSSEHeaders(w nethttp.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
}

func (f *Frontend) createAndRegisterStream(
	sess *session.Session,
	w nethttp.ResponseWriter,
	flusher nethttp.Flusher,
	authClaims *authpkg.Claims,
) *StreamConnection {
	streamCtx, streamCancel := context.WithCancel(f.ctx)
	stream := &StreamConnection{
		ID:       sess.ID,
		Session:  sess,
		Writer:   w,
		Flusher:  flusher,
		EventCh:  make(chan []byte, f.config.BufferSize),
		Created:  time.Now(),
		LastUsed: time.Now(),
		ctx:      streamCtx,
		cancel:   streamCancel,
		logger:   f.logger.With(zap.String("stream_id", sess.ID)),
	}

	f.streamsMu.Lock()
	f.streams[sess.ID] = stream
	f.streamsMu.Unlock()

	atomic.AddInt64(&f.connCount, 1)
	f.updateMetrics(func(m *types.FrontendMetrics) {
		m.ActiveConnections++
		m.TotalConnections++
	})

	stream.logger.Info("SSE stream connected", zap.String("user", authClaims.Subject))

	return stream
}

// handleStreamLoop handles the event stream loop.
func (f *Frontend) handleStreamLoop(stream *StreamConnection) {
	defer func() { _ = stream.Close() }()

	for {
		select {
		case <-stream.ctx.Done():
			return
		case <-f.shutdownCh:
			return
		case event, ok := <-stream.EventCh:
			if !ok {
				return
			}

			// Write event
			if _, err := fmt.Fprintf(stream.Writer, "data: %s\n\n", event); err != nil {
				stream.logger.Error("Failed to write event", zap.Error(err))

				return
			}

			stream.Flusher.Flush()

			stream.mu.Lock()
			stream.LastUsed = time.Now()
			stream.mu.Unlock()
		}
	}
}

// handleRequest handles POST requests.
func (f *Frontend) handleRequest(w nethttp.ResponseWriter, r *nethttp.Request) {
	// Initialize request context with tracing
	traceID := logging.GenerateTraceID()
	requestID := logging.GenerateRequestID()
	ctx := logging.ContextWithTracing(r.Context(), traceID, requestID)

	if !f.validatePostMethod(w, r.Method) {
		return
	}

	authClaims, ok := f.authenticateAPIRequest(w, r, ctx)
	if !ok {
		return
	}

	ctx = logging.ContextWithUserInfo(ctx, authClaims.Subject, "")

	body, ok := f.readAPIRequestBody(w, r, ctx)
	if !ok {
		return
	}

	stream, ok := f.findUserStream(w, authClaims.Subject)
	if !ok {
		return
	}

	resp, err := f.processRequest(ctx, body, stream)
	if err != nil {
		f.handleAPIProcessingError(w, ctx, err, body)

		return
	}

	if err := f.sendEventToStream(stream, resp); err != nil {
		f.handleStreamSendError(w, ctx, err)

		return
	}

	f.acknowledgeRequest(w)
}

func (f *Frontend) validatePostMethod(w nethttp.ResponseWriter, method string) bool {
	if method != nethttp.MethodPost {
		f.sendHTTPError(w, nethttp.StatusMethodNotAllowed, "method not allowed")
		f.updateMetrics(func(m *types.FrontendMetrics) {
			m.ErrorCount++
		})

		return false
	}

	return true
}

func (f *Frontend) authenticateAPIRequest(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	ctx context.Context,
) (*authpkg.Claims, bool) {
	authClaims, err := f.auth.Authenticate(r)
	if err != nil {
		logging.LogError(ctx, f.logger, "Authentication failed", err)
		f.sendHTTPError(w, nethttp.StatusUnauthorized, "unauthorized")
		f.updateMetrics(func(m *types.FrontendMetrics) {
			m.ErrorCount++
		})

		return nil, false
	}

	return authClaims, true
}

func (f *Frontend) readAPIRequestBody(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	ctx context.Context,
) ([]byte, bool) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		logging.LogError(ctx, f.logger, "Failed to read request body", err)
		f.sendHTTPError(w, nethttp.StatusBadRequest, "invalid request body")
		f.updateMetrics(func(m *types.FrontendMetrics) {
			m.ErrorCount++
		})

		return nil, false
	}

	return body, true
}

func (f *Frontend) findUserStream(w nethttp.ResponseWriter, userID string) (*StreamConnection, bool) {
	stream := f.findStreamByUser(userID)
	if stream == nil {
		f.sendHTTPError(w, nethttp.StatusNotFound, "stream not found")
		f.updateMetrics(func(m *types.FrontendMetrics) {
			m.ErrorCount++
		})

		return nil, false
	}

	return stream, true
}

func (f *Frontend) handleAPIProcessingError(
	w nethttp.ResponseWriter,
	ctx context.Context,
	err error,
	body []byte,
) {
	logging.LogError(ctx, f.logger, "Failed to process request", err,
		zap.ByteString("body", body))
	f.sendHTTPError(w, nethttp.StatusInternalServerError, err.Error())
	f.updateMetrics(func(m *types.FrontendMetrics) {
		m.ErrorCount++
	})
}

func (f *Frontend) handleStreamSendError(
	w nethttp.ResponseWriter,
	ctx context.Context,
	err error,
) {
	logging.LogError(ctx, f.logger, "Failed to send event to stream", err)
	f.sendHTTPError(w, nethttp.StatusInternalServerError, "failed to send response")
	f.updateMetrics(func(m *types.FrontendMetrics) {
		m.ErrorCount++
	})
}

func (f *Frontend) acknowledgeRequest(w nethttp.ResponseWriter) {
	w.WriteHeader(nethttp.StatusAccepted)
	_, _ = w.Write([]byte("accepted"))
	f.updateMetrics(func(m *types.FrontendMetrics) {
		m.RequestCount++
	})
}

// processRequest processes a request.
func (f *Frontend) processRequest(ctx context.Context, data []byte, stream *StreamConnection) (*mcp.Response, error) {
	// Parse wire message
	var wireMsg WireMessage
	if err := json.Unmarshal(data, &wireMsg); err != nil {
		return nil, customerrors.NewValidationError("invalid wire message: " + err.Error())
	}

	if wireMsg.MCPPayload == nil {
		return nil, customerrors.NewValidationError("missing MCP payload")
	}

	// Extract MCP request from payload
	mcpData, err := json.Marshal(wireMsg.MCPPayload)
	if err != nil {
		return nil, customerrors.Wrap(err, "failed to marshal MCP payload")
	}

	var req mcp.Request
	if err := json.Unmarshal(mcpData, &req); err != nil {
		return nil, customerrors.NewValidationError("invalid MCP request: " + err.Error())
	}

	// Add session to context
	ctx = context.WithValue(ctx, contextKeySession, stream.Session)

	// Route the request
	resp, err := f.router.RouteRequest(ctx, &req, wireMsg.TargetNamespace)
	if err != nil {
		return nil, err
	}

	return resp, nil
}

// findStreamByUser finds a stream connection by user ID.
func (f *Frontend) findStreamByUser(userID string) *StreamConnection {
	f.streamsMu.RLock()
	defer f.streamsMu.RUnlock()

	for _, stream := range f.streams {
		if stream.Session.User == userID {
			return stream
		}
	}

	return nil
}

// sendEventToStream sends an event to a stream.
func (f *Frontend) sendEventToStream(stream *StreamConnection, resp *mcp.Response) error {
	data, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("failed to marshal response: %w", err)
	}

	select {
	case stream.EventCh <- data:
		return nil
	case <-time.After(streamCloseTimeout):
		return fmt.Errorf("stream write timeout")
	}
}

// keepAliveLoop sends periodic keepalive comments to all streams.
func (f *Frontend) keepAliveLoop() {
	defer f.wg.Done()

	ticker := time.NewTicker(f.config.KeepAlive)
	defer ticker.Stop()

	for {
		select {
		case <-f.shutdownCh:
			return
		case <-ticker.C:
			f.sendKeepAlive()
		}
	}
}

// sendKeepAlive sends keepalive comments to all active streams.
func (f *Frontend) sendKeepAlive() {
	f.streamsMu.RLock()
	defer f.streamsMu.RUnlock()

	for _, stream := range f.streams {
		select {
		case stream.EventCh <- []byte(":keepalive"):
		default:
			// Skip if channel is full
		}
	}
}

// handleHealth handles health check requests.
func (f *Frontend) handleHealth(w nethttp.ResponseWriter, r *nethttp.Request) {
	f.mu.RLock()
	running := f.running
	f.mu.RUnlock()

	if !running {
		w.WriteHeader(nethttp.StatusServiceUnavailable)

		return
	}

	w.WriteHeader(nethttp.StatusOK)
	_, _ = w.Write([]byte("OK"))
}

// sendHTTPError sends an HTTP error response.
func (f *Frontend) sendHTTPError(w nethttp.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	resp := map[string]interface{}{
		"error": message,
	}

	_ = json.NewEncoder(w).Encode(resp)
}

// removeStream removes a stream from the registry.
func (f *Frontend) removeStream(streamID string) {
	f.streamsMu.Lock()
	stream, exists := f.streams[streamID]
	if exists {
		delete(f.streams, streamID)
	}
	f.streamsMu.Unlock()

	if exists {
		atomic.AddInt64(&f.connCount, -1)

		f.updateMetrics(func(m *types.FrontendMetrics) {
			if m.ActiveConnections > 0 {
				m.ActiveConnections--
			}
		})

		stream.logger.Info("Stream removed")
	}
}

// closeAllStreams closes all active streams.
func (f *Frontend) closeAllStreams() {
	f.streamsMu.Lock()
	streams := make([]*StreamConnection, 0, len(f.streams))
	for _, stream := range f.streams {
		streams = append(streams, stream)
	}
	f.streamsMu.Unlock()

	for _, stream := range streams {
		_ = stream.Close()
	}
}

// updateMetrics updates the metrics in a thread-safe manner.
func (f *Frontend) updateMetrics(fn func(*types.FrontendMetrics)) {
	f.metricsMu.Lock()
	defer f.metricsMu.Unlock()
	fn(&f.metrics)
}

// createTLSConfig creates TLS configuration.
func (f *Frontend) createTLSConfig() *tls.Config {
	if !f.config.TLS.Enabled {
		return nil
	}

	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		},
	}
}
