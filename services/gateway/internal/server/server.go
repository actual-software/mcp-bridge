package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	authpkg "github.com/poiley/mcp-bridge/services/gateway/internal/auth"
	"github.com/poiley/mcp-bridge/services/gateway/internal/config"
	customerrors "github.com/poiley/mcp-bridge/services/gateway/internal/errors"
	"github.com/poiley/mcp-bridge/services/gateway/internal/health"
	"github.com/poiley/mcp-bridge/services/gateway/internal/logging"
	"github.com/poiley/mcp-bridge/services/gateway/internal/metrics"
	"github.com/poiley/mcp-bridge/services/gateway/internal/ratelimit"
	"github.com/poiley/mcp-bridge/services/gateway/internal/router"
	"github.com/poiley/mcp-bridge/services/gateway/internal/session"
	"github.com/poiley/mcp-bridge/services/gateway/internal/validation"
	"github.com/poiley/mcp-bridge/services/gateway/pkg/wire"
	"github.com/poiley/mcp-bridge/services/router/pkg/mcp"
)

// contextKey is a type for context keys to avoid collisions.
type contextKey string

const (
	sessionContextKey contextKey = "session"
)

const (
	// Retry and buffer configuration.
	defaultMaxRetries          = 100
	defaultBufferSize          = 1024
	websocketChannelBufferSize = 256 // WebSocket channel buffer size
	wsHandlerCount             = 2   // Number of WebSocket handlers (read + write)

	// Timeout configuration.
	shutdownTimeoutSeconds = 5   // Graceful shutdown timeout
	httpReadTimeoutSeconds = 10  // HTTP read timeout
	httpIdleTimeoutSeconds = 120 // HTTP idle connection timeout

	// Network configuration.
	defaultTCPPortOffset = 1000    // Default TCP port offset from HTTP port
	maxHTTPHeaderBytes   = 1 << 20 // 1MB max header size
)

// GatewayServer handles WebSocket connections from MCP clients.
type GatewayServer struct {
	config      *config.Config
	logger      *zap.Logger
	auth        authpkg.Provider
	sessions    session.Manager
	router      *router.Router
	health      *health.Checker
	metrics     *metrics.Registry
	rateLimiter ratelimit.RateLimiter

	// WebSocket upgrader.
	upgrader websocket.Upgrader

	// Connection management.
	connections    sync.Map // map[string]*ClientConnection
	tcpConnections sync.Map // map[string]net.Conn - for TCP connections
	connCount      int64
	ipConnCount    sync.Map // map[string]int64 - per-IP connection count

	// Server lifecycle.
	server          *http.Server
	tcpListener     net.Listener
	tcpHandler      *TCPHandler
	tcpHealthServer *TCPHealthServer
	ctx             context.Context
	cancel          context.CancelFunc
	wg              sync.WaitGroup

	// Per-message authentication
	messageAuth *authpkg.MessageAuthenticator
}

// ClientConnection represents a connected MCP client.
type ClientConnection struct {
	ID       string
	Conn     *websocket.Conn
	Session  *session.Session
	Logger   *zap.Logger
	ClientIP string // Store client IP for proper cleanup

	// Channels
	send chan []byte

	// Connection state
	ctx    context.Context
	cancel context.CancelFunc

	// Metrics
	responseCount uint64
	errorCount    uint64
}

// BootstrapGatewayServer initializes and configures a complete gateway server instance.
func BootstrapGatewayServer(
	cfg *config.Config,
	auth authpkg.Provider,
	sessions session.Manager,
	router *router.Router,
	health *health.Checker,
	metrics *metrics.Registry,
	rateLimiter ratelimit.RateLimiter,
	logger *zap.Logger,
) *GatewayServer {
	ctx, cancel := context.WithCancel(context.Background())

	s := &GatewayServer{
		config:      cfg,
		logger:      logger,
		auth:        auth,
		sessions:    sessions,
		router:      router,
		health:      health,
		metrics:     metrics,
		rateLimiter: rateLimiter,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  cfg.Server.ConnectionBufferSize,
			WriteBufferSize: cfg.Server.ConnectionBufferSize,
			CheckOrigin:     makeOriginChecker(cfg.Server.AllowedOrigins, logger),
			// Limit header size to prevent memory exhaustion
			// Default gorilla/websocket limit is 4096 which is reasonable
		},
		ctx:    ctx,
		cancel: cancel,
	}

	// Create per-message authenticator if enabled
	if cfg.Auth.PerMessageAuth {
		s.messageAuth = authpkg.CreateMessageLevelAuthenticator(auth, logger, cfg.Auth.PerMessageAuthCache)
	}

	// Create TCP handler if needed
	if cfg.Server.Protocol == ProtocolTCP || cfg.Server.Protocol == ProtocolBoth {
		s.tcpHandler = CreateTCPProtocolHandler(
			logger,
			auth,
			router,
			sessions,
			metrics,
			rateLimiter,
			s.messageAuth,
		)
	}

	return s
}

// Start starts the gateway server.
func (s *GatewayServer) Start() error {
	// Setup TLS if needed
	var tlsConfig *tls.Config

	if s.config.Server.TLS.Enabled {
		var err error

		tlsConfig, err = s.createTLSConfig()
		if err != nil {
			return customerrors.WrapTLSConfigError(
				context.Background(),
				err,
				s.config.Server.TLS.CertFile,
				s.config.Server.TLS.KeyFile,
			)
		}
	}

	// Setup HTTP server
	if err := s.setupHTTPServer(tlsConfig); err != nil {
		return err
	}

	// Start TCP services if configured
	if err := s.startTCPServices(); err != nil {
		return err
	}

	// Start HTTP services if configured
	return s.startHTTPServices()
}

// setupHTTPServer creates and configures the HTTP server.
func (s *GatewayServer) setupHTTPServer(tlsConfig *tls.Config) error {
	// Create router
	mux := s.createHTTPRouter()

	// Wrap with security headers middleware
	handler := s.securityHeadersMiddleware(mux)

	// Configure HTTP server
	s.server = s.createHTTPServerConfig(handler, tlsConfig)

	return nil
}

// createHTTPRouter sets up the HTTP route handlers.
func (s *GatewayServer) createHTTPRouter() *http.ServeMux {
	mux := http.NewServeMux()

	// Main handlers
	mux.HandleFunc("/", s.handleWebSocket)
	mux.HandleFunc("/.well-known/security.txt", s.handleSecurityTxt)
	mux.HandleFunc("/security.txt", s.handleSecurityTxt)

	// Health endpoints
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/ready", s.handleReady)

	return mux
}

// createHTTPServerConfig creates the HTTP server configuration.
func (s *GatewayServer) createHTTPServerConfig(handler http.Handler, tlsConfig *tls.Config) *http.Server {
	return &http.Server{
		Addr:              fmt.Sprintf(":%d", s.config.Server.Port),
		Handler:           handler,
		TLSConfig:         tlsConfig,
		ReadTimeout:       httpReadTimeoutSeconds * time.Second,
		ReadHeaderTimeout: httpReadTimeoutSeconds * time.Second, // G112: Prevent Slowloris attacks
		WriteTimeout:      httpReadTimeoutSeconds * time.Second,
		IdleTimeout:       httpIdleTimeoutSeconds * time.Second,
		MaxHeaderBytes:    maxHTTPHeaderBytes,
	}
}

// startTCPServices starts TCP listener and health check if configured.
func (s *GatewayServer) startTCPServices() error {
	if !s.shouldStartTCP() {
		return nil
	}

	// Start TCP listener
	if err := s.startTCPListener(); err != nil {
		return err
	}

	// Start TCP health check server
	return s.startTCPHealthServer()
}

// shouldStartTCP checks if TCP services should be started.
func (s *GatewayServer) shouldStartTCP() bool {
	return s.config.Server.Protocol == ProtocolTCP || s.config.Server.Protocol == ProtocolBoth
}

// startTCPListener starts the TCP listener.
func (s *GatewayServer) startTCPListener() error {
	tcpPort := s.config.Server.TCPPort
	if tcpPort == 0 {
		tcpPort = s.config.Server.Port + defaultTCPPortOffset // Default to HTTP port + 1000
	}

	var err error

	lc := &net.ListenConfig{}

	s.tcpListener, err = lc.Listen(context.Background(), "tcp", fmt.Sprintf(":%d", tcpPort))
	if err != nil {
		return customerrors.WrapListenerCreationError(context.Background(), err, fmt.Sprintf(":%d", tcpPort), "tcp")
	}

	// Start TCP accept loop
	s.wg.Add(1)

	go s.acceptTCPConnections()

	s.logger.Info("Started TCP listener", zap.Int("port", tcpPort))

	return nil
}

// startTCPHealthServer starts the TCP health check server if configured.
func (s *GatewayServer) startTCPHealthServer() error {
	if s.config.Server.TCPHealthPort <= 0 {
		return nil
	}

	s.tcpHealthServer = CreateTCPHealthCheckServer(
		s.config.Server.TCPHealthPort,
		s.health,
		s.metrics,
		s.logger,
	)

	if err := s.tcpHealthServer.Start(); err != nil {
		return customerrors.WrapServerStartError(context.Background(), err, fmt.Sprintf(":%d", s.config.Server.TCPHealthPort))
	}

	return nil
}

// startHTTPServices starts the HTTP/WebSocket server if configured.
func (s *GatewayServer) startHTTPServices() error {
	if !s.shouldStartHTTP() {
		return nil
	}

	s.logger.Info("Starting gateway server",
		zap.Int("port", s.config.Server.Port),
		zap.Int("max_connections", s.config.Server.MaxConnections),
	)

	var err error
	if s.config.Server.TLS.Enabled {
		err = s.startHTTPS()
	} else {
		err = s.startHTTP()
	}

	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		s.logger.Error("HTTP server error", zap.Error(err))

		return err
	}

	return nil
}

// shouldStartHTTP checks if HTTP services should be started.
func (s *GatewayServer) shouldStartHTTP() bool {
	return s.config.Server.Protocol == "websocket" ||
		s.config.Server.Protocol == ProtocolBoth ||
		s.config.Server.Protocol == ""
}

// startHTTPS starts the HTTPS server.
func (s *GatewayServer) startHTTPS() error {
	s.logger.Info("Starting HTTPS server with TLS",
		zap.String("cert", s.config.Server.TLS.CertFile),
		zap.String("min_version", s.config.Server.TLS.MinVersion))

	return s.server.ListenAndServeTLS("", "") // certs are already loaded in TLSConfig
}

// startHTTP starts the HTTP server.
func (s *GatewayServer) startHTTP() error {
	s.logger.Info("Starting HTTP server without TLS")

	return s.server.ListenAndServe()
}

// Shutdown gracefully shuts down the server.
func (s *GatewayServer) Shutdown(ctx context.Context) error {
	s.logger.Info("Shutting down gateway server")

	// Phase 1: Stop accepting new connections
	s.stopAcceptingConnections(ctx)

	// Phase 2: Notify all connections of shutdown
	s.notifyConnectionsOfShutdown()

	// Phase 3: Cancel context to signal handlers
	s.cancel()

	// Phase 4: Close active connections
	s.closeActiveConnections()

	// Phase 5: Wait for graceful shutdown or timeout
	s.waitForShutdown(ctx)

	// Phase 6: Cleanup resources
	s.cleanupResources()

	return nil
}

// stopAcceptingConnections stops accepting new connections.
func (s *GatewayServer) stopAcceptingConnections(ctx context.Context) {
	// Shutdown HTTP server
	s.shutdownHTTPServer(ctx)

	// Close TCP listener
	s.closeTCPListener()

	// Stop TCP health server
	s.stopTCPHealthServer()
}

// shutdownHTTPServer gracefully shuts down the HTTP server.
func (s *GatewayServer) shutdownHTTPServer(ctx context.Context) {
	if s.server != nil {
		shutdownCtx, cancel := context.WithTimeout(ctx, shutdownTimeoutSeconds*time.Second)
		defer cancel()

		if err := s.server.Shutdown(shutdownCtx); err != nil {
			s.logger.Error("Error shutting down HTTP server", zap.Error(err))
		}
	}
}

// closeTCPListener closes the TCP listener.
func (s *GatewayServer) closeTCPListener() {
	if s.tcpListener != nil {
		if err := s.tcpListener.Close(); err != nil {
			s.logger.Error("Error closing TCP listener", zap.Error(err))
		}
	}
}

// stopTCPHealthServer stops the TCP health server.
func (s *GatewayServer) stopTCPHealthServer() {
	if s.tcpHealthServer != nil {
		if err := s.tcpHealthServer.Stop(); err != nil {
			s.logger.Error("Error stopping TCP health server", zap.Error(err))
		}
	}
}

// notifyConnectionsOfShutdown sends shutdown notifications to all connections.
func (s *GatewayServer) notifyConnectionsOfShutdown() {
	s.tcpConnections.Range(func(key, value interface{}) bool {
		s.notifyTCPConnection(key, value)

		return true
	})
}

// notifyTCPConnection sends shutdown notification to a TCP connection.
func (s *GatewayServer) notifyTCPConnection(key, value interface{}) {
	conn, ok := value.(net.Conn)
	if !ok {
		return
	}

	// Set write deadline
	_ = conn.SetWriteDeadline(time.Now().Add(defaultMaxConnections * time.Second))

	// Send shutdown control message
	s.sendShutdownMessage(conn, key)
}

// sendShutdownMessage sends a shutdown control message.
func (s *GatewayServer) sendShutdownMessage(conn net.Conn, key interface{}) {
	transport := wire.NewTransport(conn)
	control := map[string]interface{}{
		"command": "shutdown",
		"reason":  "server shutting down",
	}

	if err := transport.SendControl(control); err != nil {
		s.logShutdownMessageError(err, key)
	}
}

// logShutdownMessageError logs shutdown message send errors.
func (s *GatewayServer) logShutdownMessageError(err error, key interface{}) {
	if connID, ok := key.(string); ok {
		s.logger.Debug("Failed to send shutdown message",
			zap.String("conn_id", connID),
			zap.Error(err))
	} else {
		s.logger.Debug("Failed to send shutdown message", zap.Error(err))
	}
}

// closeActiveConnections closes all active WebSocket connections.
func (s *GatewayServer) closeActiveConnections() {
	s.connections.Range(func(key, value interface{}) bool {
		if conn, ok := value.(*ClientConnection); ok {
			conn.Close()
			s.connections.Delete(key)
		}

		return true
	})
}

// waitForShutdown waits for graceful shutdown or timeout.
func (s *GatewayServer) waitForShutdown(ctx context.Context) {
	done := make(chan struct{})

	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		s.logger.Info("All connections closed gracefully")
	case <-ctx.Done():
		s.handleShutdownTimeout()
	}
}

// handleShutdownTimeout handles shutdown timeout by force closing connections.
func (s *GatewayServer) handleShutdownTimeout() {
	s.logger.Warn("Shutdown timeout reached, forcing close on remaining connections")
	s.tcpConnections.Range(func(key, value interface{}) bool {
		if conn, ok := value.(net.Conn); ok {
			if err := conn.Close(); err != nil {
				s.logger.Debug("Error force closing TCP connection", zap.Error(err))
			}
		}

		return true
	})
}

// cleanupResources cleans up remaining resources.
func (s *GatewayServer) cleanupResources() {
	if s.sessions != nil {
		if err := s.sessions.Close(); err != nil {
			s.logger.Error("Error closing session manager", zap.Error(err))
		}
	}
}

// handleWebSocket handles incoming WebSocket connections.
// checkConnectionLimits checks global and per-IP connection limits.
func (s *GatewayServer) checkConnectionLimits(clientIP string) error {
	currentConns := atomic.LoadInt64(&s.connCount)
	if currentConns >= int64(s.config.Server.MaxConnections) {
		s.logger.Warn("Connection limit reached",
			zap.Int64("current", currentConns),
			zap.Int("limit", s.config.Server.MaxConnections))
		s.metrics.IncrementConnectionsRejected()

		return customerrors.NewMaxConnectionsError(int(atomic.LoadInt64(&s.connCount)), s.config.Server.MaxConnections)
	}

	perIPLimit := s.config.Server.MaxConnectionsPerIP
	if perIPLimit > 0 {
		ipConns := s.getIPConnCount(clientIP)
		if ipConns >= int64(perIPLimit) {
			s.logger.Warn("Per-IP connection limit reached",
				zap.String("ip", clientIP),
				zap.Int64("current", ipConns),
				zap.Int("limit", perIPLimit))
			s.metrics.IncrementConnectionsRejected()

			return customerrors.NewRateLimitExceededError(clientIP, s.config.Server.MaxConnectionsPerIP, "per-ip")
		}
	}

	return nil
}

// setupClientConnection creates and initializes a client connection.
func (s *GatewayServer) setupClientConnection(
	conn *websocket.Conn,
	sess *session.Session,
	clientIP string,
) *ClientConnection {
	clientConn := &ClientConnection{
		ID:       sess.ID,
		Conn:     conn,
		Session:  sess,
		Logger:   s.logger.With(zap.String("client_id", sess.ID)),
		ClientIP: clientIP,
		send:     make(chan []byte, websocketChannelBufferSize),
	}
	clientConn.ctx, clientConn.cancel = context.WithCancel(s.ctx)

	return clientConn
}

func (s *GatewayServer) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Initialize request context
	traceID := logging.GenerateTraceID()
	requestID := logging.GenerateRequestID()
	ctx := logging.ContextWithTracing(r.Context(), traceID, requestID)
	r.Body = http.MaxBytesReader(w, r.Body, defaultBufferSize*defaultBufferSize)
	clientIP := getIPFromAddr(r.RemoteAddr)

	// Check connection limits
	if err := s.checkConnectionLimits(clientIP); err != nil {
		s.handleConnectionLimitError(ctx, w, err, clientIP)

		return
	}

	// Authenticate request

	authClaims, err := s.authenticateWebSocketRequest(ctx, w, r, clientIP, traceID, requestID)
	if err != nil {
		return
	}

	// Add user info to context - create enriched context for subsequent operations
	enrichedCtx := logging.ContextWithUserInfo(ctx, authClaims.Subject, "")

	// Upgrade to WebSocket and create session

	conn, sess, err := s.upgradeAndCreateSession(enrichedCtx, w, r, authClaims, traceID, requestID)
	if err != nil {
		return
	}

	// Setup and register connection

	s.setupAndRegisterConnection(enrichedCtx, conn, sess, authClaims, clientIP, r.RemoteAddr)
}

func (s *GatewayServer) handleConnectionLimitError(
	ctx context.Context,
	w http.ResponseWriter,
	err error,
	clientIP string,
) {
	logging.LogError(ctx, s.logger, "Connection limit reached", err,
		zap.String("client_ip", clientIP))

	var gatewayErr *customerrors.GatewayError
	if errors.As(err, &gatewayErr) {
		http.Error(w, gatewayErr.Message, gatewayErr.HTTPStatus)
	} else {
		http.Error(w, "Connection limit reached", http.StatusServiceUnavailable)
	}
}

func (s *GatewayServer) authenticateWebSocketRequest(
	ctx context.Context,
	w http.ResponseWriter,
	r *http.Request,
	clientIP, traceID, requestID string,
) (*authpkg.Claims, error) {
	authClaims, err := s.auth.Authenticate(r)
	if err != nil {
		logging.LogError(ctx, s.logger, "Authentication failed", err,
			zap.String("remote_addr", r.RemoteAddr),
			zap.String("client_ip", clientIP))

		authErr := customerrors.WrapWithType(err, customerrors.TypeUnauthorized, "authentication failed").
			WithComponent("server").
			WithOperation("websocket_auth").
			WithContext("client_ip", clientIP).
			WithContext("trace_id", traceID).
			WithContext("request_id", requestID)
		customerrors.RecordError(authErr, s.metrics)

		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		s.metrics.IncrementAuthFailures("invalid_token")

		return nil, err
	}

	return authClaims, nil
}

func (s *GatewayServer) upgradeAndCreateSession(
	ctx context.Context,
	w http.ResponseWriter,
	r *http.Request,
	authClaims *authpkg.Claims,
	traceID, requestID string,
) (*websocket.Conn, *session.Session, error) {
	// Upgrade to WebSocket
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		logging.LogError(ctx, s.logger, "WebSocket upgrade failed", err)

		upgradeErr := customerrors.Wrap(err, "WebSocket upgrade failed").
			WithComponent("server").
			WithOperation("websocket_upgrade").
			WithHTTPStatus(http.StatusBadRequest).
			WithContext("trace_id", traceID).
			WithContext("request_id", requestID)
		customerrors.RecordError(upgradeErr, s.metrics)

		return nil, nil, err
	}

	// Create session
	sess, err := s.sessions.CreateSession(authClaims)
	if err != nil {
		logging.LogError(ctx, s.logger, "Failed to create session", err)

		sessionErr := customerrors.Wrap(err, "session creation failed").
			WithComponent("server").
			WithOperation("create_session").
			WithContext("user_id", authClaims.Subject).
			WithContext("trace_id", traceID).
			WithContext("request_id", requestID)
		customerrors.RecordError(sessionErr, s.metrics)

		if closeErr := conn.Close(); closeErr != nil {
			logging.LogDebug(ctx, s.logger, "Error closing connection", zap.Error(closeErr))
		}

		return nil, nil, err
	}

	return conn, sess, nil
}

func (s *GatewayServer) setupAndRegisterConnection(
	ctx context.Context,
	conn *websocket.Conn,
	sess *session.Session,
	authClaims *authpkg.Claims,
	clientIP, remoteAddr string,
) {
	// Update context with session info
	ctx = logging.ContextWithUserInfo(ctx, authClaims.Subject, sess.ID)

	// Setup client connection
	clientConn := s.setupClientConnection(conn, sess, clientIP)
	clientConn.ctx = ctx

	// Register connection
	s.connections.Store(sess.ID, clientConn)
	atomic.AddInt64(&s.connCount, 1)
	s.incrementIPConnCount(clientIP)
	s.metrics.IncrementConnections()

	// Start connection handlers
	s.wg.Add(wsHandlerCount)

	go s.handleClientRead(clientConn, clientIP)
	go s.handleClientWrite(clientConn)

	logging.LogRequest(ctx, s.logger, "Client connected",
		zap.String("remote_addr", remoteAddr),
		zap.String("user", authClaims.Subject),
		zap.String("client_ip", clientIP),
	)
}

// handleClientRead handles reading from the client.
func (s *GatewayServer) handleClientRead(client *ClientConnection, clientIP string) {
	defer s.wg.Done()
	defer func() { client.Close() }()
	defer s.removeConnection(client.ID) // This now handles IP count decrement

	// Configure connection settings
	if err := client.Conn.SetReadDeadline(time.Time{}); err != nil {
		client.Logger.Warn("Failed to set read deadline", zap.Error(err))
	}

	client.Conn.SetPongHandler(func(string) error {
		client.Logger.Debug("Received pong")

		return nil
	})

	// Set message size limit to 10MB (matching binary protocol)
	const maxMessageSize = defaultRetryCount * defaultBufferSize * defaultBufferSize // 10MB

	client.Conn.SetReadLimit(maxMessageSize)

	for {
		select {
		case <-client.ctx.Done():
			return
		default:
			// Read message
			messageType, data, err := client.Conn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					client.Logger.Error("WebSocket read error", zap.Error(err))
				}

				return
			}

			if messageType != websocket.TextMessage {
				continue
			}

			// Process the message
			if err := s.processClientMessage(client.ctx, client, data); err != nil {
				client.Logger.Error("Failed to process message",
					zap.Error(err),
					zap.ByteString("data", data),
				)
				// Extract request ID from the raw message for proper error correlation
				requestID := s.extractRequestID(data)
				s.sendErrorResponse(client, requestID, err)
			}
		}
	}
}

// handleClientWrite handles writing to the client.
func (s *GatewayServer) handleClientWrite(client *ClientConnection) {
	defer s.wg.Done()

	ticker := time.NewTicker(defaultTimeoutSeconds * time.Second)
	defer ticker.Stop()

	for {
		if !s.processWriteEvent(client, ticker) {
			return
		}
	}
}

// processWriteEvent processes a single write event.
func (s *GatewayServer) processWriteEvent(client *ClientConnection, ticker *time.Ticker) bool {
	select {
	case <-client.ctx.Done():
		return false

	case message := <-client.send:
		return s.writeMessageWithRetry(client, websocket.TextMessage, message)

	case <-ticker.C:
		return s.writePingWithRetry(client)
	}
}

// writeMessageWithRetry writes a message with retry logic.
func (s *GatewayServer) writeMessageWithRetry(client *ClientConnection, messageType int, message []byte) bool {
	return s.writeWithRetry(client, messageType, message, "message")
}

// writePingWithRetry sends a ping with retry logic.
func (s *GatewayServer) writePingWithRetry(client *ClientConnection) bool {
	return s.writeWithRetry(client, websocket.PingMessage, []byte("keepalive"), "ping")
}

// writeWithRetry performs write with exponential backoff retry.
func (s *GatewayServer) writeWithRetry(client *ClientConnection, messageType int, data []byte, operation string) bool {
	maxRetries := 3

	for attempt := 0; attempt < maxRetries; attempt++ {
		// Set write deadline
		s.setWriteDeadline(client)

		// Attempt write
		err := client.Conn.WriteMessage(messageType, data)
		if err == nil {
			return true // Success
		}

		// Handle write failure
		if !s.handleWriteError(client, err, attempt, maxRetries, operation) {
			return false
		}
	}

	return false
}

// setWriteDeadline sets the write deadline on the connection.
func (s *GatewayServer) setWriteDeadline(client *ClientConnection) {
	if err := client.Conn.SetWriteDeadline(time.Now().Add(defaultRetryCount * time.Second)); err != nil {
		client.Logger.Warn("Failed to set write deadline", zap.Error(err))
	}
}

// handleWriteError handles write errors with retry logic.
func (s *GatewayServer) handleWriteError(
	client *ClientConnection,
	err error,
	attempt, maxRetries int,
	operation string,
) bool {
	client.Logger.Warn("WebSocket "+operation+" error, retrying",
		zap.Error(err),
		zap.Int("attempt", attempt+1),
		zap.Int("max_retries", maxRetries))

	// Check if we should retry
	if attempt >= maxRetries-1 {
		client.Logger.Error("WebSocket "+operation+" failed after retries, closing connection",
			zap.Error(err))

		return false
	}

	// Check if error is temporary
	if s.isTemporaryError(err) {
		// Exponential backoff
		backoff := time.Duration(defaultMaxRetries*(attempt+1)) * time.Millisecond
		time.Sleep(backoff)

		return true // Continue retrying
	}

	// Non-temporary error
	client.Logger.Error("WebSocket "+operation+" error is not temporary, closing connection",
		zap.Error(err))

	return false
}

// isTemporaryError checks if an error is temporary.
func (s *GatewayServer) isTemporaryError(err error) bool {
	var netErr net.Error

	return errors.As(err, &netErr) && netErr.Timeout()
}

// processClientMessage processes a message from the client.
func (s *GatewayServer) processClientMessage(ctx context.Context, client *ClientConnection, data []byte) error {
	// Parse and extract the request
	wireMsg, req, err := s.parseClientMessage(data)
	if err != nil {
		return err
	}

	// Validate the request
	if err := s.validateClientRequest(req, wireMsg); err != nil {
		return err
	}

	// Authenticate if needed
	if err := s.authenticateMessage(ctx, wireMsg, client); err != nil {
		return err
	}

	// Check rate limit
	if err := s.checkClientRateLimit(ctx, client, req); err != nil {
		return err
	}

	// Process and route the request
	return s.routeClientRequest(ctx, client, wireMsg, req)
}

// parseClientMessage parses wire message and extracts MCP request.
func (s *GatewayServer) parseClientMessage(data []byte) (*WireMessage, *mcp.Request, error) {
	var wireMsg WireMessage
	if err := json.Unmarshal(data, &wireMsg); err != nil {
		return nil, nil, customerrors.NewProtocolError(fmt.Sprintf("invalid wire message: %v", err), "mcp")
	}

	if wireMsg.MCPPayload == nil {
		return nil, nil, customerrors.NewProtocolError("missing MCP payload", "mcp")
	}

	mcpData, err := json.Marshal(wireMsg.MCPPayload)
	if err != nil {
		return nil, nil, customerrors.Wrap(err, "failed to marshal MCP payload").
			WithComponent("server")
	}

	var req mcp.Request
	if err := json.Unmarshal(mcpData, &req); err != nil {
		return nil, nil, customerrors.NewInvalidRequestError(err.Error(), "")
	}

	return &wireMsg, &req, nil
}

// validateClientRequest validates request fields.
func (s *GatewayServer) validateClientRequest(req *mcp.Request, wireMsg *WireMessage) error {
	// Validate request ID
	if err := validation.ValidateRequestID(req.ID); err != nil {
		return customerrors.NewInvalidRequestError(fmt.Sprintf("invalid request ID: %v", err), "")
	}

	// Validate method
	if err := validation.ValidateMethod(req.Method); err != nil {
		return customerrors.NewInvalidRequestError(fmt.Sprintf("invalid method: %v", err), req.Method)
	}

	// Validate namespace if present
	if wireMsg.TargetNamespace != "" {
		if err := validation.ValidateNamespace(wireMsg.TargetNamespace); err != nil {
			return customerrors.NewInvalidRequestError(fmt.Sprintf("invalid namespace: %v", err), wireMsg.TargetNamespace)
		}
	}

	// Validate auth token if present
	if wireMsg.AuthToken != "" {
		if err := validation.ValidateToken(wireMsg.AuthToken); err != nil {
			return customerrors.NewInvalidRequestError(fmt.Sprintf("invalid auth token: %v", err), "")
		}
	}

	return nil
}

// authenticateMessage performs per-message authentication.
func (s *GatewayServer) authenticateMessage(ctx context.Context, wireMsg *WireMessage, client *ClientConnection) error {
	if s.messageAuth == nil {
		return nil
	}

	if err := s.messageAuth.ValidateMessageToken(ctx, wireMsg.AuthToken, client.Session.ID); err != nil {
		s.metrics.IncrementAuthFailures("invalid_message_token")

		return customerrors.Wrap(err, "per-message authentication failed").
			WithComponent("server").
			WithOperation("authenticate")
	}

	return nil
}

// checkClientRateLimit checks rate limiting for the client.
func (s *GatewayServer) checkClientRateLimit(ctx context.Context, client *ClientConnection, req *mcp.Request) error {
	allowed, err := s.rateLimiter.Allow(ctx, client.Session.User, client.Session.RateLimit)
	if err != nil {
		return customerrors.Wrap(err, "rate limit check failed").
			WithComponent("server").
			WithOperation("rate_limit")
	}

	if !allowed {
		s.metrics.IncrementRequests(req.Method, "rate_limited")

		return customerrors.NewRateLimitExceededError(client.ClientIP, client.Session.RateLimit.RequestsPerMinute, "user")
	}

	return nil
}

// routeClientRequest routes the request and sends response.
func (s *GatewayServer) routeClientRequest(
	ctx context.Context,
	client *ClientConnection,
	wireMsg *WireMessage,
	req *mcp.Request,
) error {
	// Update wire message fields
	wireMsg.ID = req.ID
	wireMsg.Source = client.ID
	wireMsg.Timestamp = time.Now().UTC().Format(time.RFC3339)

	// Log request
	client.Logger.Debug("Processing request",
		zap.Any("id", req.ID),
		zap.String("method", req.Method),
		zap.String("target_namespace", wireMsg.TargetNamespace),
	)

	// Route the request
	ctx = context.WithValue(ctx, sessionContextKey, client.Session)

	response, err := s.router.RouteRequest(ctx, req, wireMsg.TargetNamespace)
	if err != nil {
		s.metrics.IncrementRequests(req.Method, "error")

		return err
	}

	// Send response
	s.metrics.IncrementRequests(req.Method, "success")

	return s.sendResponse(client, response)
}

// sendResponse sends a response to the client.
func (s *GatewayServer) sendResponse(client *ClientConnection, resp *mcp.Response) error {
	// Wrap in wire message
	wireMsg := WireMessage{
		ID:         resp.ID,
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
		Source:     "gateway",
		MCPPayload: resp,
	}

	data, err := json.Marshal(wireMsg)
	if err != nil {
		return customerrors.Wrap(err, "failed to marshal response").
			WithComponent("server")
	}

	select {
	case client.send <- data:
		atomic.AddUint64(&client.responseCount, 1)

		return nil
	case <-client.ctx.Done():
		return customerrors.New(customerrors.TypeInternal, "client disconnected").
			WithComponent("server")
	}
}

// sendErrorResponse sends an error response to the client and records error metrics.
func (s *GatewayServer) sendErrorResponse(client *ClientConnection, id interface{}, err error) {
	// Record error metrics
	if s.metrics != nil {
		customerrors.RecordError(err, s.metrics)
	}

	// Increment client error count
	atomic.AddUint64(&client.errorCount, 1)

	// Determine appropriate error code based on error type
	errorCode := mapErrorToMCPCode(err)

	resp := mcp.NewErrorResponse(
		errorCode,
		err.Error(),
		nil,
		id,
	)

	if sendErr := s.sendResponse(client, resp); sendErr != nil {
		s.handleSendErrorFailure(client, sendErr, err)
	}
}

// mapErrorToMCPCode maps gateway errors to MCP error codes.
func mapErrorToMCPCode(err error) int {
	var gatewayErr *customerrors.GatewayError
	if !errors.As(err, &gatewayErr) {
		return mcp.ErrorCodeInternalError
	}

	return getErrorCodeForType(gatewayErr.Type)
}

// getErrorCodeForType maps gateway error types to MCP error codes.
func getErrorCodeForType(errorType customerrors.ErrorType) int {
	// Handle simple one-to-one mappings
	if code, exists := getSimpleErrorMappings()[errorType]; exists {
		return code
	}

	// Handle special cases
	return handleSpecialErrorCases(errorType)
}

// getSimpleErrorMappings returns the simple one-to-one error type mappings.
func getSimpleErrorMappings() map[customerrors.ErrorType]int {
	return map[customerrors.ErrorType]int{
		customerrors.TypeValidation:  mcp.ErrorCodeInvalidRequest,
		customerrors.TypeNotFound:    mcp.ErrorCodeMethodNotFound,
		customerrors.TypeTimeout:     mcp.ErrorCodeRequestTimeout,
		customerrors.TypeRateLimit:   mcp.ErrorCodeRateLimitExceeded,
		customerrors.TypeInternal:    mcp.ErrorCodeInternalError,
		customerrors.TypeCanceled:    mcp.ErrorCodeInternalError,
		customerrors.TypeConflict:    mcp.ErrorCodeInvalidRequest,
		customerrors.TypeUnavailable: mcp.ErrorCodeBackendUnavailable,
	}
}

// handleSpecialErrorCases handles error types that require special logic.
func handleSpecialErrorCases(errorType customerrors.ErrorType) int {
	// Handle auth-related errors (both unauthorized and forbidden)
	if errorType == customerrors.TypeUnauthorized || errorType == customerrors.TypeForbidden {
		return mcp.ErrorCodeUnauthorized
	}

	// Default case
	return mcp.ErrorCodeInternalError
}

// handleSendErrorFailure handles the case where sending an error response fails.
func (s *GatewayServer) handleSendErrorFailure(client *ClientConnection, sendErr, originalErr error) {
	client.Logger.Error("Failed to send error response",
		zap.Error(sendErr),
		zap.Error(originalErr),
	)
	// Record the send failure as well
	if s.metrics != nil {
		sendError := customerrors.Wrap(sendErr, "failed to send error response").
			WithComponent("server").
			WithOperation("send_error_response")
		customerrors.RecordError(sendError, s.metrics)
	}

	atomic.AddUint64(&client.errorCount, 1)
}

// extractRequestID extracts the request ID from raw message data for error correlation.
func (s *GatewayServer) extractRequestID(data []byte) interface{} {
	// Try to parse as wire message first
	var wireMsg WireMessage
	if err := json.Unmarshal(data, &wireMsg); err != nil {
		// If wire message parsing fails, try parsing as direct MCP request
		var directReq map[string]interface{}
		if err := json.Unmarshal(data, &directReq); err != nil {
			return nil
		}

		if id, exists := directReq["id"]; exists {
			return id
		}

		return nil
	}

	// First check if wire message has ID at top level
	if wireMsg.ID != nil {
		return wireMsg.ID
	}

	// Otherwise, extract from MCP payload
	if wireMsg.MCPPayload == nil {
		return nil
	}

	mcpPayload, ok := wireMsg.MCPPayload.(map[string]interface{})
	if !ok {
		return nil
	}

	if id, exists := mcpPayload["id"]; exists {
		return id
	}

	return nil
}

// removeConnection removes a connection from the registry.
func (s *GatewayServer) removeConnection(id string) {
	if conn, ok := s.connections.LoadAndDelete(id); ok {
		atomic.AddInt64(&s.connCount, -1)
		s.metrics.DecrementConnections()

		// Decrement per-IP connection count
		if clientConn, ok := conn.(*ClientConnection); ok {
			s.decrementIPConnCount(clientConn.ClientIP)
		}

		// Remove session
		if err := s.sessions.RemoveSession(id); err != nil {
			s.logger.Error("Failed to remove session", zap.Error(err))
		}
	}
}

// Close closes the client connection.
func (c *ClientConnection) Close() {
	c.cancel()

	if c.Conn != nil {
		if err := c.Conn.Close(); err != nil {
			c.Logger.Debug("Error closing WebSocket connection", zap.Error(err))
		}
	}
}

// acceptTCPConnections accepts TCP connections.
func (s *GatewayServer) acceptTCPConnections() {
	defer s.wg.Done()

	for {
		if !s.waitForNextConnection() {
			return
		}

		conn := s.acceptConnection()
		if conn == nil {
			continue
		}

		if !s.checkTCPConnectionLimits(conn) {
			continue
		}

		s.handleAcceptedConnection(conn)
	}
}

// waitForNextConnection checks if we should continue accepting connections.
func (s *GatewayServer) waitForNextConnection() bool {
	select {
	case <-s.ctx.Done():
		return false
	default:
		return true
	}
}

// acceptConnection accepts a new TCP connection.
func (s *GatewayServer) acceptConnection() net.Conn {
	// Set accept deadline
	if tcpListener, ok := s.tcpListener.(*net.TCPListener); ok {
		if err := tcpListener.SetDeadline(time.Now().Add(time.Second)); err != nil {
			s.logger.Warn("failed to set TCP listener deadline", zap.Error(err))
		}
	}

	conn, err := s.tcpListener.Accept()
	if err != nil {
		if s.shouldIgnoreAcceptError(err) {
			return nil
		}

		s.logger.Error("Failed to accept TCP connection", zap.Error(err))

		return nil
	}

	return conn
}

// shouldIgnoreAcceptError checks if an accept error should be ignored.
func (s *GatewayServer) shouldIgnoreAcceptError(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}

	return s.ctx.Err() != nil
}

// checkTCPConnectionLimits checks global and per-IP connection limits for TCP connections.
func (s *GatewayServer) checkTCPConnectionLimits(conn net.Conn) bool {
	clientIP := getIPFromAddr(conn.RemoteAddr().String())

	if !s.checkGlobalLimit(conn) {
		return false
	}

	if !s.checkPerIPLimit(conn, clientIP) {
		return false
	}

	return true
}

// checkGlobalLimit checks the global connection limit.
func (s *GatewayServer) checkGlobalLimit(conn net.Conn) bool {
	currentConns := atomic.LoadInt64(&s.connCount)
	if currentConns >= int64(s.config.Server.MaxConnections) {
		s.logger.Warn("TCP connection limit reached",
			zap.Int64("current", currentConns),
			zap.Int("limit", s.config.Server.MaxConnections),
		)

		s.closeConnectionWithLimit(conn)

		return false
	}

	return true
}

// checkPerIPLimit checks the per-IP connection limit.
func (s *GatewayServer) checkPerIPLimit(conn net.Conn, clientIP string) bool {
	perIPLimit := s.config.Server.MaxConnectionsPerIP
	if perIPLimit <= 0 {
		return true
	}

	ipConns := s.getIPConnCount(clientIP)
	if ipConns >= int64(perIPLimit) {
		s.logger.Warn("Per-IP connection limit reached",
			zap.String("ip", clientIP),
			zap.Int64("current", ipConns),
			zap.Int("limit", perIPLimit),
		)

		s.closeConnectionWithLimit(conn)

		return false
	}

	return true
}

// closeConnectionWithLimit closes a connection due to limits.
func (s *GatewayServer) closeConnectionWithLimit(conn net.Conn) {
	if err := conn.Close(); err != nil {
		s.logger.Debug("Error closing TCP connection after limit reached", zap.Error(err))
	}

	s.metrics.IncrementConnectionsRejected()
}

// handleAcceptedConnection handles a newly accepted connection.
func (s *GatewayServer) handleAcceptedConnection(conn net.Conn) {
	clientIP := getIPFromAddr(conn.RemoteAddr().String())

	// Increment connection counts
	atomic.AddInt64(&s.connCount, 1)
	s.incrementIPConnCount(clientIP)

	// Generate connection ID
	connID := uuid.New().String()

	// Store connection for graceful shutdown
	s.tcpConnections.Store(connID, conn)

	// Handle connection
	s.wg.Add(1)

	go func() {
		defer s.wg.Done()
		defer atomic.AddInt64(&s.connCount, -1)
		defer s.decrementIPConnCount(clientIP)
		defer s.tcpConnections.Delete(connID)

		s.tcpHandler.HandleConnection(s.ctx, conn)
	}()
}

// Allow implements the RateLimiter interface.
func (s *GatewayServer) Allow(ctx context.Context, key string, config authpkg.RateLimitConfig) (bool, error) {
	return s.rateLimiter.Allow(ctx, key, config)
}

// getIPFromAddr extracts IP from a network address.
func getIPFromAddr(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		// If split fails, return the whole address
		return addr
	}

	return host
}

// incrementIPConnCount increments the connection count for an IP.
func (s *GatewayServer) incrementIPConnCount(ip string) int64 {
	// Use a pointer to int64 for atomic operations
	val, _ := s.ipConnCount.LoadOrStore(ip, new(int64))
	if currentPtr, ok := val.(*int64); ok {
		return atomic.AddInt64(currentPtr, 1)
	}

	return 0
}

// decrementIPConnCount decrements the connection count for an IP.
func (s *GatewayServer) decrementIPConnCount(ip string) {
	if val, ok := s.ipConnCount.Load(ip); ok {
		if currentPtr, ok := val.(*int64); ok {
			newVal := atomic.AddInt64(currentPtr, -1)
			// Clean up if count reaches 0
			if newVal <= 0 {
				s.ipConnCount.Delete(ip)
			}
		}
	}
}

// getIPConnCount gets the current connection count for an IP.
func (s *GatewayServer) getIPConnCount(ip string) int64 {
	if val, ok := s.ipConnCount.Load(ip); ok {
		if currentPtr, ok := val.(*int64); ok {
			return atomic.LoadInt64(currentPtr)
		}
	}

	return 0
}

// WireMessage represents the wire protocol message.
type WireMessage struct {
	ID              interface{} `json:"id"`
	Timestamp       string      `json:"timestamp"`
	Source          string      `json:"source"`
	TargetNamespace string      `json:"target_namespace,omitempty"`
	MCPPayload      interface{} `json:"mcp_payload"`
	AuthToken       string      `json:"auth_token,omitempty"` // Per-message authentication token
}

// makeOriginChecker creates a WebSocket origin checker function.

func makeOriginChecker(allowedOrigins []string, logger *zap.Logger) func(*http.Request) bool {
	// If no origins are specified, allow localhost for development
	if len(allowedOrigins) == 0 {
		logger.Warn("No allowed origins configured, defaulting to localhost only")

		allowedOrigins = []string{"http://localhost", "https://localhost"}
	}

	// Normalize origins
	normalizedOrigins := make(map[string]bool)

	for _, origin := range allowedOrigins {
		// Handle wildcard
		if origin == "*" {
			logger.Warn("Wildcard origin '*' is configured - this is insecure for production")

			return func(r *http.Request) bool {
				return true
			}
		}

		// Parse and normalize the origin
		u, err := url.Parse(origin)
		if err != nil {
			logger.Error("Invalid origin in configuration",
				zap.String("origin", origin),
				zap.Error(err))

			continue
		}

		// Store normalized origin (scheme + host + port)
		normalized := u.Scheme + "://" + u.Host
		normalizedOrigins[normalized] = true
		normalizedOrigins[strings.ToLower(normalized)] = true // Case insensitive
	}

	return func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			// No origin header - could be same-origin or non-browser client
			logger.Debug("No origin header in WebSocket request",
				zap.String("remote_addr", r.RemoteAddr))

			return true
		}

		// Parse the origin
		u, err := url.Parse(origin)
		if err != nil {
			logger.Warn("Invalid origin header",
				zap.String("origin", origin),
				zap.Error(err))

			return false
		}

		// Check against allowed origins
		normalized := u.Scheme + "://" + u.Host
		allowed := normalizedOrigins[normalized] || normalizedOrigins[strings.ToLower(normalized)]

		if !allowed {
			logger.Warn("Rejected WebSocket connection from unauthorized origin",
				zap.String("origin", origin),
				zap.String("remote_addr", r.RemoteAddr))
		}

		return allowed
	}
}

// createTLSConfig creates a TLS configuration based on the server config.
func (s *GatewayServer) createTLSConfig() (*tls.Config, error) {
	cfg := s.config.Server.TLS

	// Load certificate
	cert, err := s.loadTLSCertificate(cfg)
	if err != nil {
		return nil, err
	}

	// Get minimum TLS version
	minVersion, err := s.getTLSMinVersion(cfg.MinVersion)
	if err != nil {
		return nil, err
	}

	// Create base TLS config
	// #nosec G402 - minVersion is validated by getTLSMinVersion to be TLS 1.2 or 1.3
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   minVersion,
	}

	// Configure cipher suites
	if err := s.configureCipherSuites(tlsConfig, cfg, minVersion); err != nil {
		return nil, err
	}

	// Configure client authentication
	if err := s.configureClientAuth(tlsConfig, cfg); err != nil {
		return nil, err
	}

	return tlsConfig, nil
}

// loadTLSCertificate loads the TLS certificate and key.
func (s *GatewayServer) loadTLSCertificate(cfg config.TLSConfig) (tls.Certificate, error) {
	if cfg.CertFile == "" || cfg.KeyFile == "" {
		return tls.Certificate{}, customerrors.New(
			customerrors.TypeValidation,
			"TLS cert_file and key_file are required when TLS is enabled",
		).WithComponent("server")
	}

	cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
	if err != nil {
		return tls.Certificate{}, customerrors.WrapTLSConfigError(context.Background(), err, cfg.CertFile, cfg.KeyFile)
	}

	return cert, nil
}

// getTLSMinVersion parses and returns the minimum TLS version.
func (s *GatewayServer) getTLSMinVersion(version string) (uint16, error) {
	switch version {
	case "1.2":
		return tls.VersionTLS12, nil
	case "1.3", "":
		return tls.VersionTLS13, nil
	default:
		return 0, customerrors.New(customerrors.TypeValidation, "unsupported TLS min_version: "+version).
			WithComponent("server")
	}
}

// configureCipherSuites configures the cipher suites for TLS.
func (s *GatewayServer) configureCipherSuites(tlsConfig *tls.Config, cfg config.TLSConfig, minVersion uint16) error {
	if len(cfg.CipherSuites) > 0 {
		cipherSuites, err := s.parseCipherSuites(cfg.CipherSuites)
		if err != nil {
			return err
		}

		tlsConfig.CipherSuites = cipherSuites
	} else if minVersion == tls.VersionTLS13 {
		tlsConfig.CipherSuites = s.getDefaultTLS13CipherSuites()
	}

	return nil
}

// parseCipherSuites parses cipher suite names to constants.
func (s *GatewayServer) parseCipherSuites(suites []string) ([]uint16, error) {
	cipherSuites := make([]uint16, 0, len(suites))

	for _, suite := range suites {
		cipherSuite := getCipherSuite(suite)
		if cipherSuite == 0 {
			return nil, customerrors.New(customerrors.TypeValidation, "unsupported cipher suite: "+suite).
				WithComponent("server")
		}

		cipherSuites = append(cipherSuites, cipherSuite)
	}

	return cipherSuites, nil
}

// getDefaultTLS13CipherSuites returns secure default cipher suites for TLS 1.3.
func (s *GatewayServer) getDefaultTLS13CipherSuites() []uint16 {
	return []uint16{
		tls.TLS_AES_256_GCM_SHA384,
		tls.TLS_CHACHA20_POLY1305_SHA256,
		tls.TLS_AES_128_GCM_SHA256,
	}
}

// configureClientAuth configures client authentication (mTLS).
func (s *GatewayServer) configureClientAuth(tlsConfig *tls.Config, cfg config.TLSConfig) error {
	switch cfg.ClientAuth {
	case "none", "":
		tlsConfig.ClientAuth = tls.NoClientCert

		return nil
	case "request":
		tlsConfig.ClientAuth = tls.RequestClientCert

		return nil
	case "require":
		return s.configureRequiredClientAuth(tlsConfig, cfg)
	default:
		return customerrors.New(customerrors.TypeValidation, "unsupported client_auth: "+cfg.ClientAuth).
			WithComponent("server")
	}
}

// configureRequiredClientAuth configures required client authentication.
func (s *GatewayServer) configureRequiredClientAuth(tlsConfig *tls.Config, cfg config.TLSConfig) error {
	tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert

	if cfg.CAFile == "" {
		return customerrors.New(customerrors.TypeValidation, "ca_file is required when client_auth is 'require'").
			WithComponent("server")
	}

	caCert, err := os.ReadFile(cfg.CAFile)
	if err != nil {
		return customerrors.Wrap(err, "failed to read CA file").
			WithComponent("server")
	}

	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCert) {
		return customerrors.New(customerrors.TypeValidation, "failed to parse CA certificate").
			WithComponent("server")
	}

	tlsConfig.ClientCAs = caCertPool

	return nil
}

// getCipherSuite returns the cipher suite constant for a given name.
func getCipherSuite(name string) uint16 {
	// Check TLS 1.2 cipher suites
	if suite := getTLS12CipherSuite(name); suite != 0 {
		return suite
	}

	// Check TLS 1.3 cipher suites
	return getTLS13CipherSuite(name)
}

// getTLS12CipherSuite returns TLS 1.2 cipher suite constants.
func getTLS12CipherSuite(name string) uint16 {
	tls12Suites := map[string]uint16{
		"TLS_RSA_WITH_AES_128_GCM_SHA256":               tls.TLS_RSA_WITH_AES_128_GCM_SHA256,
		"TLS_RSA_WITH_AES_256_GCM_SHA384":               tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
		"TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256":       tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
		"TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384":       tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
		"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256":         tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		"TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384":         tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
		"TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256":   tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,
		"TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256": tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,
	}

	return tls12Suites[name]
}

// getTLS13CipherSuite returns TLS 1.3 cipher suite constants.
func getTLS13CipherSuite(name string) uint16 {
	tls13Suites := map[string]uint16{
		"TLS_AES_128_GCM_SHA256":       tls.TLS_AES_128_GCM_SHA256,
		"TLS_AES_256_GCM_SHA384":       tls.TLS_AES_256_GCM_SHA384,
		"TLS_CHACHA20_POLY1305_SHA256": tls.TLS_CHACHA20_POLY1305_SHA256,
	}

	return tls13Suites[name]
}

// securityHeadersMiddleware adds security headers to all HTTP responses.
func (s *GatewayServer) securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Add security headers
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "accelerometer=(), camera=(), geolocation=(), gyroscope=(),"+
			"magnetometer=(), microphone=(), payment=(), usb=()")

		// Add HSTS header for HTTPS connections
		if r.TLS != nil {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}

		// Add CSP header - restrictive policy for WebSocket gateway
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none';"+
			"base-uri 'none'; form-action 'none';")

		// Add CORS headers if needed (WebSocket handshake)
		if origin := r.Header.Get("Origin"); origin != "" && s.isAllowedOrigin(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Requested-With")
			w.Header().Set("Access-Control-Max-Age", "86400")
		}

		// Handle preflight requests
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)

			return
		}

		// Call the next handler
		next.ServeHTTP(w, r)
	})
}

// isAllowedOrigin checks if the origin is allowed for CORS.
func (s *GatewayServer) isAllowedOrigin(origin string) bool {
	// For now, we'll be restrictive and not allow any origins
	// This can be configured later if needed
	// Example: return s.config.Server.AllowedOrigins.Contains(origin)
	return false
}

// handleSecurityTxt serves the security.txt file.
func (s *GatewayServer) handleSecurityTxt(w http.ResponseWriter, r *http.Request) {
	// Only allow GET requests
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)

		return
	}

	// Set appropriate headers
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "max-age=10800") // Cache for 3 hours

	// Serve the security.txt content
	if _, err := fmt.Fprint(w, `Contact: security@mcp-gateway.example.com
Expires: 2025-12-31T23:59:59.000Z
Preferred-Languages: en
Canonical: https://mcp-gateway.example.com/.well-known/security.txt
Encryption: https://mcp-gateway.example.com/pgp-key.txt
Policy: https://mcp-gateway.example.com/security-policy
Acknowledgments: https://mcp-gateway.example.com/security-acknowledgments

# Quick Report
# If you've found a security vulnerability, please email security@mcp-gateway.example.com
# Please do not report security issues via GitHub issues.
`); err != nil {
		s.logger.Error("Failed to write security.txt content", zap.Error(err))
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// handleHealth returns detailed health status.
func (s *GatewayServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	status := s.health.GetStatus()

	w.Header().Set("Content-Type", "application/json")

	if !status.Healthy {
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	if err := json.NewEncoder(w).Encode(status); err != nil {
		http.Error(w, "Failed to encode health status", http.StatusInternalServerError)
	}
}

// handleHealthz returns simple health status.
func (s *GatewayServer) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if s.health.IsHealthy() {
		w.WriteHeader(http.StatusOK)

		if _, err := w.Write([]byte("OK")); err != nil {
			s.logger.Warn("failed to write health check response", zap.Error(err))
		}
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)

		if _, err := w.Write([]byte("Unhealthy")); err != nil {
			s.logger.Warn("failed to write health check response", zap.Error(err))
		}
	}
}

// handleReady returns readiness status.
func (s *GatewayServer) handleReady(w http.ResponseWriter, r *http.Request) {
	if s.health.IsReady() {
		w.WriteHeader(http.StatusOK)

		if _, err := w.Write([]byte("Ready")); err != nil {
			s.logger.Warn("failed to write readiness response", zap.Error(err))
		}
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)

		if _, err := w.Write([]byte("Not ready")); err != nil {
			s.logger.Warn("failed to write readiness response", zap.Error(err))
		}
	}
}
