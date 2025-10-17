package websocket

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	authpkg "github.com/actual-software/mcp-bridge/services/gateway/internal/auth"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/common"
	customerrors "github.com/actual-software/mcp-bridge/services/gateway/internal/errors"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/frontends/types"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/logging"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/session"
	"github.com/actual-software/mcp-bridge/services/router/pkg/mcp"
)

const (
	defaultWriteWait  = 10 * time.Second
	defaultPongWait   = 60 * time.Second
	defaultPingPeriod = 30 * time.Second
	wsHandlerCount    = 2 // read + write goroutines
)

// WireMessage represents the wire protocol message format.
type WireMessage struct {
	ID              interface{} `json:"id"`
	Timestamp       string      `json:"timestamp"`
	Source          string      `json:"source"`
	TargetNamespace string      `json:"target_namespace,omitempty"`
	MCPPayload      interface{} `json:"mcp_payload"`
	AuthToken       string      `json:"auth_token,omitempty"`
}

// ClientConnection represents a connected WebSocket client.
type ClientConnection struct {
	ID       string
	Conn     *websocket.Conn
	Session  *session.Session
	WriteCh  chan []byte
	ClientIP string
	ctx      context.Context
	cancel   context.CancelFunc
	created  time.Time
	lastUsed time.Time
	closed   bool
	mu       sync.RWMutex
	logger   *zap.Logger
}

// Close closes the client connection.
func (c *ClientConnection) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}
	c.closed = true

	if c.cancel != nil {
		c.cancel()
	}

	close(c.WriteCh)

	return c.Conn.Close()
}

// Frontend implements the WebSocket frontend.
type Frontend struct {
	name     string
	config   Config
	logger   *zap.Logger
	router   types.RequestRouter
	auth     types.AuthProvider
	sessions types.SessionManager

	// WebSocket server
	server   *http.Server
	mux      *http.ServeMux
	upgrader websocket.Upgrader

	// Connection management
	connections map[string]*ClientConnection
	connMu      sync.RWMutex
	connCount   int64
	ipConnCount sync.Map // map[string]int64

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

// CreateWebSocketFrontend creates a new WebSocket frontend instance.
func CreateWebSocketFrontend(
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
		name:        name,
		config:      config,
		logger:      logger.With(zap.String("frontend", name), zap.String("protocol", "websocket")),
		router:      router,
		auth:        auth,
		sessions:    sessions,
		mux:         http.NewServeMux(),
		connections: make(map[string]*ClientConnection),
		upgrader: websocket.Upgrader{
			ReadBufferSize:  defaultReadBufferSize,
			WriteBufferSize: defaultWriteBufferSize,
			CheckOrigin:     makeOriginChecker(config.AllowedOrigins),
		},
		ctx:        ctx,
		cancel:     cancel,
		shutdownCh: make(chan struct{}),
	}
}

// Start initializes the frontend (routes only).
// The HTTP server is managed externally via SetServer() for shared server support.
func (f *Frontend) Start(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.running {
		return fmt.Errorf("websocket frontend already running")
	}

	// Setup routes on internal mux
	f.mux.HandleFunc(f.config.Path, f.handleWebSocketUpgrade)

	f.running = true
	f.updateMetrics(func(m *types.FrontendMetrics) {
		m.IsRunning = true
	})

	addr := fmt.Sprintf("%s:%d", f.config.Host, f.config.Port)
	f.logger.Info("websocket frontend initialized",
		zap.String("address", addr),
		zap.String("path", f.config.Path),
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

	f.logger.Info("stopping websocket frontend")

	// Signal shutdown
	close(f.shutdownCh)
	f.cancel()

	// Note: HTTP server is externally managed, so we don't shutdown it here

	// Notify all connections of shutdown
	f.notifyConnectionsOfShutdown()

	// Close all connections
	f.closeAllConnections()

	// Wait for goroutines to finish
	done := make(chan struct{})
	go func() {
		f.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		f.logger.Info("websocket frontend stopped gracefully")
	case <-time.After(shutdownTimeout):
		f.logger.Warn("websocket frontend shutdown timeout")
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
	return "websocket"
}

// GetMetrics returns frontend-specific metrics.
func (f *Frontend) GetMetrics() types.FrontendMetrics {
	f.metricsMu.RLock()
	defer f.metricsMu.RUnlock()

	return f.metrics
}

// GetHandler returns the HTTP handler for this frontend.
func (f *Frontend) GetHandler() http.Handler {
	return f.mux
}

// GetAddress returns the host:port address this frontend listens on.
func (f *Frontend) GetAddress() string {
	return fmt.Sprintf("%s:%d", f.config.Host, f.config.Port)
}

// SetServer injects the shared HTTP server into this frontend.
func (f *Frontend) SetServer(server *http.Server) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.server = server
}

// GetTLSConfig returns TLS configuration for this frontend.
func (f *Frontend) GetTLSConfig() (enabled bool, certFile, keyFile string) {
	return f.config.TLS.Enabled, f.config.TLS.CertFile, f.config.TLS.KeyFile
}

// handleWebSocketUpgrade handles WebSocket upgrade requests.
func (f *Frontend) handleWebSocketUpgrade(w http.ResponseWriter, r *http.Request) {
	// Initialize request context
	traceID := logging.GenerateTraceID()
	requestID := logging.GenerateRequestID()
	ctx := logging.ContextWithTracing(r.Context(), traceID, requestID)
	clientIP := getIPFromAddr(r.RemoteAddr)

	// Check connection limits
	if err := f.checkConnectionLimits(clientIP); err != nil {
		f.handleConnectionLimitError(ctx, w, err, clientIP)

		return
	}

	// Authenticate request
	authClaims, err := f.authenticateWebSocketRequest(ctx, w, r, clientIP, traceID, requestID)
	if err != nil {
		return
	}

	// Add user info to context
	enrichedCtx := logging.ContextWithUserInfo(ctx, authClaims.Subject, "")

	// Upgrade to WebSocket and create session
	conn, sess, err := f.upgradeAndCreateSession(enrichedCtx, w, r, authClaims, traceID, requestID)
	if err != nil {
		return
	}

	// Setup and register connection
	f.setupAndRegisterConnection(enrichedCtx, conn, sess, authClaims, clientIP, r.RemoteAddr)
}

// checkConnectionLimits checks global and per-IP connection limits.
func (f *Frontend) checkConnectionLimits(clientIP string) error {
	// Check global limit
	currentCount := atomic.LoadInt64(&f.connCount)
	if currentCount >= int64(f.config.MaxConnections) {
		return customerrors.NewUnavailableError(
			fmt.Sprintf("connection limit reached: %d/%d", currentCount, f.config.MaxConnections))
	}

	return nil
}

// handleConnectionLimitError handles connection limit errors.
func (f *Frontend) handleConnectionLimitError(
	ctx context.Context,
	w http.ResponseWriter,
	err error,
	clientIP string,
) {
	logging.LogError(ctx, f.logger, "Connection limit reached", err,
		zap.String("client_ip", clientIP))

	var gatewayErr *customerrors.GatewayError
	if errors.As(err, &gatewayErr) {
		http.Error(w, gatewayErr.Message, gatewayErr.HTTPStatus)
	} else {
		http.Error(w, "Connection limit reached", http.StatusServiceUnavailable)
	}

	f.updateMetrics(func(m *types.FrontendMetrics) {
		m.ErrorCount++
	})
}

// authenticateWebSocketRequest authenticates a WebSocket upgrade request.
func (f *Frontend) authenticateWebSocketRequest(
	ctx context.Context,
	w http.ResponseWriter,
	r *http.Request,
	clientIP, traceID, requestID string,
) (*authpkg.Claims, error) {
	authClaims, err := f.auth.Authenticate(r)
	if err != nil {
		logging.LogError(ctx, f.logger, "Authentication failed", err,
			zap.String("remote_addr", r.RemoteAddr),
			zap.String("client_ip", clientIP))

		http.Error(w, "Unauthorized", http.StatusUnauthorized)

		f.updateMetrics(func(m *types.FrontendMetrics) {
			m.ErrorCount++
		})

		return nil, err
	}

	return authClaims, nil
}

// upgradeAndCreateSession upgrades to WebSocket and creates a session.
func (f *Frontend) upgradeAndCreateSession(
	ctx context.Context,
	w http.ResponseWriter,
	r *http.Request,
	authClaims *authpkg.Claims,
	traceID, requestID string,
) (*websocket.Conn, *session.Session, error) {
	// Upgrade to WebSocket
	conn, err := f.upgrader.Upgrade(w, r, nil)
	if err != nil {
		logging.LogError(ctx, f.logger, "WebSocket upgrade failed", err)

		f.updateMetrics(func(m *types.FrontendMetrics) {
			m.ErrorCount++
		})

		return nil, nil, err
	}

	// Create session
	sess, err := f.sessions.CreateSession(authClaims)
	if err != nil {
		logging.LogError(ctx, f.logger, "Failed to create session", err)

		if closeErr := conn.Close(); closeErr != nil {
			logging.LogDebug(ctx, f.logger, "Error closing connection", zap.Error(closeErr))
		}

		f.updateMetrics(func(m *types.FrontendMetrics) {
			m.ErrorCount++
		})

		return nil, nil, err
	}

	return conn, sess, nil
}

// setupAndRegisterConnection sets up and registers a client connection.
func (f *Frontend) setupAndRegisterConnection(
	ctx context.Context,
	conn *websocket.Conn,
	sess *session.Session,
	authClaims *authpkg.Claims,
	clientIP, remoteAddr string,
) {
	// Update context with session info
	ctx = logging.ContextWithUserInfo(ctx, authClaims.Subject, sess.ID)

	// Create client connection context
	connCtx, connCancel := context.WithCancel(f.ctx)

	// Setup client connection
	clientConn := &ClientConnection{
		ID:       sess.ID,
		Conn:     conn,
		Session:  sess,
		WriteCh:  make(chan []byte, defaultWriteChSize),
		ClientIP: clientIP,
		ctx:      connCtx,
		cancel:   connCancel,
		created:  time.Now(),
		lastUsed: time.Now(),
		logger:   f.logger.With(zap.String("session_id", sess.ID), zap.String("client_ip", clientIP)),
	}

	// Register connection
	f.connMu.Lock()
	f.connections[sess.ID] = clientConn
	f.connMu.Unlock()

	atomic.AddInt64(&f.connCount, 1)
	f.incrementIPConnCount(clientIP)

	f.updateMetrics(func(m *types.FrontendMetrics) {
		m.ActiveConnections++
		m.TotalConnections++
	})

	// Start connection handlers
	f.wg.Add(wsHandlerCount)
	go f.handleClientRead(clientConn, clientIP)
	go f.handleClientWrite(clientConn)

	logging.LogRequest(ctx, f.logger, "Client connected",
		zap.String("remote_addr", remoteAddr),
		zap.String("user", authClaims.Subject),
		zap.String("client_ip", clientIP),
	)
}

// handleClientRead handles reading from the client.
func (f *Frontend) handleClientRead(client *ClientConnection, clientIP string) {
	defer f.wg.Done()
	defer func() { _ = client.Close() }()
	defer f.removeConnection(client.ID)

	// Configure connection settings
	if err := client.Conn.SetReadDeadline(time.Time{}); err != nil {
		client.logger.Warn("Failed to set read deadline", zap.Error(err))
	}

	client.Conn.SetPongHandler(func(string) error {
		client.logger.Debug("Received pong")

		return nil
	})

	client.Conn.SetReadLimit(f.config.MaxMessageSize)

	for {
		select {
		case <-client.ctx.Done():
			return
		case <-f.shutdownCh:
			return
		default:
			// Read message
			messageType, data, err := client.Conn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					client.logger.Error("WebSocket read error", zap.Error(err))
				}

				return
			}

			if messageType != websocket.TextMessage {
				continue
			}

			// Process the message
			if err := f.processClientMessage(client.ctx, client, data); err != nil {
				client.logger.Error("Failed to process message",
					zap.Error(err),
					zap.ByteString("data", data),
				)
				requestID := f.extractRequestID(data)
				f.sendErrorResponse(client, requestID, err)
			}
		}
	}
}

// handleClientWrite handles writing to the client.
func (f *Frontend) handleClientWrite(client *ClientConnection) {
	defer f.wg.Done()

	ticker := time.NewTicker(defaultPingPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-client.ctx.Done():
			return
		case <-f.shutdownCh:
			return
		case msg, ok := <-client.WriteCh:
			if !ok {
				return
			}

			if err := client.Conn.SetWriteDeadline(time.Now().Add(defaultWriteWait)); err != nil {
				client.logger.Warn("Failed to set write deadline", zap.Error(err))
			}

			if err := client.Conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				client.logger.Error("Write error", zap.Error(err))

				return
			}

		case <-ticker.C:
			if err := client.Conn.SetWriteDeadline(time.Now().Add(defaultWriteWait)); err != nil {
				client.logger.Warn("Failed to set write deadline", zap.Error(err))
			}

			if err := client.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				client.logger.Error("Ping error", zap.Error(err))

				return
			}
		}
	}
}

// processClientMessage processes a message from the client.
func (f *Frontend) processClientMessage(ctx context.Context, client *ClientConnection, data []byte) error {
	// Update last used time
	client.mu.Lock()
	client.lastUsed = time.Now()
	client.mu.Unlock()

	f.updateMetrics(func(m *types.FrontendMetrics) {
		m.RequestCount++
	})

	// Parse wire message
	var wireMsg WireMessage
	if err := json.Unmarshal(data, &wireMsg); err != nil {
		return customerrors.NewValidationError("invalid wire message: " + err.Error())
	}

	if wireMsg.MCPPayload == nil {
		return customerrors.NewValidationError("missing MCP payload")
	}

	// Extract MCP request from payload
	mcpData, err := json.Marshal(wireMsg.MCPPayload)
	if err != nil {
		return customerrors.Wrap(err, "failed to marshal MCP payload")
	}

	var req mcp.Request
	if err := json.Unmarshal(mcpData, &req); err != nil {
		return customerrors.NewValidationError("invalid MCP request: " + err.Error())
	}

	// Add session to context
	ctx = context.WithValue(ctx, common.SessionContextKey, client.Session)
	client.logger.Debug("DEBUG: Session added to context in WebSocket frontend",
		zap.String("session_id", client.Session.ID),
		zap.String("user", client.Session.User),
		zap.String("method", req.Method))

	// Route the request
	resp, err := f.router.RouteRequest(ctx, &req, wireMsg.TargetNamespace)
	if err != nil {
		client.logger.Error("Request routing failed",
			zap.String("method", req.Method),
			zap.Any("id", req.ID),
			zap.Error(err))

		return err
	}

	// Send response
	if err := f.sendResponse(client, resp); err != nil {
		client.logger.Error("Failed to send response to WriteCh",
			zap.Any("response_id", resp.ID),
			zap.Error(err))

		return err
	}

	return nil
}

// sendResponse sends a response to the client.
func (f *Frontend) sendResponse(client *ClientConnection, resp *mcp.Response) error {
	// Wrap response in WireMessage format for router compatibility
	wireMsg := WireMessage{
		ID:         resp.ID,
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
		Source:     "gateway",
		MCPPayload: resp,
	}

	data, err := json.Marshal(wireMsg)
	if err != nil {
		return fmt.Errorf("failed to marshal response: %w", err)
	}

	select {
	case client.WriteCh <- data:
		return nil
	case <-client.ctx.Done():
		return fmt.Errorf("connection closed")
	case <-time.After(streamCloseTimeout):
		return fmt.Errorf("write timeout")
	}
}

// sendErrorResponse sends an error response to the client.
func (f *Frontend) sendErrorResponse(client *ClientConnection, requestID interface{}, err error) {
	resp := &mcp.Response{
		JSONRPC: "2.0",
		ID:      requestID,
		Error: &mcp.Error{
			Code:    mcp.ErrorCodeInternalError,
			Message: err.Error(),
		},
	}

	if sendErr := f.sendResponse(client, resp); sendErr != nil {
		client.logger.Error("Failed to send error response", zap.Error(sendErr))
	}

	f.updateMetrics(func(m *types.FrontendMetrics) {
		m.ErrorCount++
	})
}

// extractRequestID extracts the request ID from raw JSON data.
func (f *Frontend) extractRequestID(data []byte) interface{} {
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}

	return raw["id"]
}

// removeConnection removes a connection from the registry.
func (f *Frontend) removeConnection(sessionID string) {
	f.connMu.Lock()
	client, exists := f.connections[sessionID]
	if exists {
		delete(f.connections, sessionID)
	}
	f.connMu.Unlock()

	if exists {
		atomic.AddInt64(&f.connCount, -1)
		f.decrementIPConnCount(client.ClientIP)

		f.updateMetrics(func(m *types.FrontendMetrics) {
			if m.ActiveConnections > 0 {
				m.ActiveConnections--
			}
		})

		// Clean up backend sessions associated with this frontend session
		// This prevents memory leaks when clients disconnect
		if f.router != nil {
			// Type assert to access ClearBackendSessions method
			// The router interface may not expose this method, so we check first
			type backendSessionCleaner interface {
				ClearBackendSessions(frontendSessionID string)
			}
			if cleaner, ok := f.router.(backendSessionCleaner); ok {
				cleaner.ClearBackendSessions(sessionID)
			}
		}

		client.logger.Info("Connection removed")
	}
}

// notifyConnectionsOfShutdown notifies all connections of shutdown.
func (f *Frontend) notifyConnectionsOfShutdown() {
	f.connMu.RLock()
	defer f.connMu.RUnlock()

	for _, client := range f.connections {
		// Send shutdown notification
		resp := &mcp.Response{
			JSONRPC: "2.0",
			Error: &mcp.Error{
				Code:    mcp.ErrorCodeInternalError,
				Message: "Server shutting down",
			},
		}

		if data, err := json.Marshal(resp); err == nil {
			select {
			case client.WriteCh <- data:
			case <-time.After(1 * time.Second):
			}
		}
	}
}

// closeAllConnections closes all active connections.
func (f *Frontend) closeAllConnections() {
	f.connMu.Lock()
	connections := make([]*ClientConnection, 0, len(f.connections))
	for _, client := range f.connections {
		connections = append(connections, client)
	}
	f.connMu.Unlock()

	for _, client := range connections {
		_ = client.Close()
	}
}

// incrementIPConnCount increments the connection count for an IP.
func (f *Frontend) incrementIPConnCount(ip string) {
	val, _ := f.ipConnCount.LoadOrStore(ip, new(int64))
	if counter, ok := val.(*int64); ok {
		atomic.AddInt64(counter, 1)
	}
}

// decrementIPConnCount decrements the connection count for an IP.
func (f *Frontend) decrementIPConnCount(ip string) {
	if val, ok := f.ipConnCount.Load(ip); ok {
		if counter, ok := val.(*int64); ok {
			count := atomic.AddInt64(counter, -1)
			if count <= 0 {
				f.ipConnCount.Delete(ip)
			}
		}
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

// makeOriginChecker creates an origin checker function.
func makeOriginChecker(allowedOrigins []string) func(*http.Request) bool {
	if len(allowedOrigins) == 0 {
		return func(r *http.Request) bool { return true }
	}

	originMap := make(map[string]bool, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		originMap[origin] = true
	}

	return func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true
		}

		return originMap[origin]
	}
}

// getIPFromAddr extracts IP address from net.Addr string.
func getIPFromAddr(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}

	return host
}
