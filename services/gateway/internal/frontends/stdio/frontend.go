package stdio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"

	customerrors "github.com/poiley/mcp-bridge/services/gateway/internal/errors"
	"github.com/poiley/mcp-bridge/services/router/pkg/mcp"

	"go.uber.org/zap"
)

const (
	defaultMaxRetries           = 100
	defaultTimeoutSeconds       = 30
	defaultMaxTimeoutSeconds    = 60
	defaultRetryCount           = 10
	defaultMaxConcurrentClients = 100
	defaultClientTimeoutSeconds = 300
	unixSocketPermissions       = 0o600
)

// FrontendMetrics contains performance and health metrics for a frontend.
type FrontendMetrics struct {
	ActiveConnections uint64 `json:"active_connections"`
	TotalConnections  uint64 `json:"total_connections"`
	RequestCount      uint64 `json:"request_count"`
	ErrorCount        uint64 `json:"error_count"`
	IsRunning         bool   `json:"is_running"`
}

// RequestRouter handles routing requests from frontends to backends.
type RequestRouter interface {
	RouteRequest(ctx context.Context, req *mcp.Request, targetNamespace string) (*mcp.Response, error)
}

// AuthProvider handles authentication for frontend connections.
type AuthProvider interface {
	Authenticate(ctx context.Context, credentials map[string]string) (bool, error)
	GetUserInfo(ctx context.Context, credentials map[string]string) (map[string]interface{}, error)
}

// SessionManager manages client sessions.
type SessionManager interface {
	CreateSession(ctx context.Context, clientID string) (string, error)
	ValidateSession(ctx context.Context, sessionID string) (bool, error)
	DestroySession(ctx context.Context, sessionID string) error
}

// Config contains stdio frontend configuration.
type Config struct {
	Enabled bool                    `mapstructure:"enabled"`
	Modes   []ModeConfig            `mapstructure:"modes"`
	Process ProcessManagementConfig `mapstructure:"process_management"`
}

// ModeConfig contains configuration for a specific stdio mode.
type ModeConfig struct {
	Type        string `mapstructure:"type"`        // "unix_socket", "stdin_stdout", "named_pipes"
	Path        string `mapstructure:"path"`        // Socket path or pipe name
	Permissions string `mapstructure:"permissions"` // Unix socket permissions
	Enabled     bool   `mapstructure:"enabled"`
}

// ProcessManagementConfig contains process management settings.
type ProcessManagementConfig struct {
	MaxConcurrentClients int           `mapstructure:"max_concurrent_clients"`
	ClientTimeout        time.Duration `mapstructure:"client_timeout"`
	AuthRequired         bool          `mapstructure:"auth_required"`
}

// ClientConnection represents a single stdio client connection.
type ClientConnection struct {
	id       string
	reader   *json.Decoder
	writer   *json.Encoder
	conn     io.ReadWriteCloser
	created  time.Time
	lastUsed time.Time
	mu       sync.RWMutex
}

// Frontend implements the stdio frontend for MCP clients.
type Frontend struct {
	name     string
	config   Config
	logger   *zap.Logger
	router   RequestRouter
	auth     AuthProvider
	sessions SessionManager

	// Connection management
	connections   map[string]*ClientConnection
	connectionsMu sync.RWMutex
	clientCounter uint64

	// Unix socket listener
	unixListener net.Listener

	// State management
	mu      sync.RWMutex
	running bool

	// Metrics
	metrics    FrontendMetrics
	mu_metrics sync.RWMutex

	// Shutdown coordination
	shutdownCh chan struct{}
	wg         sync.WaitGroup
}

// CreateStdioFrontend creates a stdio-based frontend instance for local client communication.
func CreateStdioFrontend(
	name string,
	config Config,
	router RequestRouter,
	auth AuthProvider,
	sessions SessionManager,
	logger *zap.Logger,
) *Frontend {
	// Set defaults
	if config.Process.MaxConcurrentClients == 0 {
		config.Process.MaxConcurrentClients = defaultMaxConcurrentClients
	}

	if config.Process.ClientTimeout == 0 {
		config.Process.ClientTimeout = defaultClientTimeoutSeconds * time.Second
	}

	return &Frontend{
		name:        name,
		config:      config,
		logger:      logger.With(zap.String("frontend", name), zap.String("protocol", "stdio")),
		router:      router,
		auth:        auth,
		sessions:    sessions,
		connections: make(map[string]*ClientConnection),
		shutdownCh:  make(chan struct{}),
		metrics: FrontendMetrics{
			IsRunning: false,
		},
	}
}

// Start initializes the stdio frontend and begins accepting connections.
func (f *Frontend) Start(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Validate startup state
	if err := f.validateStartup(); err != nil {
		return err
	}

	if !f.config.Enabled {
		f.logger.Info("stdio frontend disabled")

		return nil
	}

	// Start all configured modes
	if err := f.startConfiguredModes(ctx); err != nil {
		return err
	}

	// Mark as running and start cleanup routine
	f.finalizeStartup()

	f.logger.Info("stdio frontend started successfully")

	return nil
}

// validateStartup checks if the frontend can be started.
func (f *Frontend) validateStartup() error {
	if f.running {
		return customerrors.New(customerrors.TypeInternal, fmt.Sprintf("frontend %s already running", f.name)).
			WithComponent("frontend_stdio")
	}

	return nil
}

// startConfiguredModes starts all enabled modes.
func (f *Frontend) startConfiguredModes(ctx context.Context) error {
	for _, mode := range f.config.Modes {
		if !mode.Enabled {
			continue
		}

		if err := f.startMode(ctx, mode); err != nil {
			return err
		}
	}

	return nil
}

// startMode starts a specific mode based on its type.
func (f *Frontend) startMode(ctx context.Context, mode ModeConfig) error {
	switch mode.Type {
	case "unix_socket":
		return f.startUnixSocketMode(ctx, mode)
	case "stdin_stdout":
		return f.startStdinStdoutMode(ctx, mode)
	case "named_pipes":
		return f.startNamedPipesMode(ctx, mode)
	default:
		f.logger.Warn("unsupported stdio mode", zap.String("mode", mode.Type))

		return nil
	}
}

// startUnixSocketMode wraps unix socket startup with error context.
func (f *Frontend) startUnixSocketMode(ctx context.Context, mode ModeConfig) error {
	if err := f.startUnixSocket(ctx, mode); err != nil {
		return customerrors.Wrap(err, "failed to start unix socket mode").
			WithComponent("frontend_stdio")
	}

	return nil
}

// startStdinStdoutMode wraps stdin/stdout startup with error context.
func (f *Frontend) startStdinStdoutMode(ctx context.Context, mode ModeConfig) error {
	if err := f.startStdinStdout(ctx, mode); err != nil {
		return customerrors.Wrap(err, "failed to start stdin/stdout mode").
			WithComponent("frontend_stdio")
	}

	return nil
}

// startNamedPipesMode wraps named pipes startup with error context.
func (f *Frontend) startNamedPipesMode(ctx context.Context, mode ModeConfig) error {
	// startNamedPipes always returns an error (not implemented)
	return customerrors.Wrap(f.startNamedPipes(ctx, mode), "failed to start named pipes mode").
		WithComponent("frontend_stdio")
}

// finalizeStartup completes the startup process.
func (f *Frontend) finalizeStartup() {
	f.running = true
	f.updateMetrics(func(m *FrontendMetrics) {
		m.IsRunning = true
	})

	// Start connection cleanup routine
	f.wg.Add(1)

	go f.connectionCleanup()
}

// startUnixSocket starts a Unix socket listener.
func (f *Frontend) startUnixSocket(ctx context.Context, mode ModeConfig) error {
	// Remove existing socket file
	if err := os.Remove(mode.Path); err != nil && !os.IsNotExist(err) {
		return customerrors.Wrap(err, "failed to remove existing socket").
			WithComponent("frontend_stdio")
	}

	// Create Unix socket listener
	lc := &net.ListenConfig{}

	listener, err := lc.Listen(ctx, "unix", mode.Path)
	if err != nil {
		return customerrors.Wrap(err, "failed to create unix socket").
			WithComponent("frontend_stdio")
	}

	// Set permissions if specified
	if mode.Permissions != "" {
		if err := os.Chmod(mode.Path, unixSocketPermissions); err != nil {
			_ = listener.Close()

			return customerrors.Wrap(err, "failed to set socket permissions").
				WithComponent("frontend_stdio")
		}
	}

	f.unixListener = listener

	// Start accepting connections
	f.wg.Add(1)

	go f.acceptUnixConnections(ctx, listener)

	f.logger.Info("unix socket listener started", zap.String("path", mode.Path))

	return nil
}

// acceptUnixConnections accepts connections on the Unix socket.
func (f *Frontend) acceptUnixConnections(ctx context.Context, listener net.Listener) {
	defer f.wg.Done()
	defer func() {
		if err := listener.Close(); err != nil {
			f.logger.Warn("Failed to close listener", zap.Error(err))
		}
	}()

	for {
		select {
		case <-f.shutdownCh:
			return
		default:
		}

		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-f.shutdownCh:
				return
			default:
				f.logger.Error("failed to accept unix socket connection", zap.Error(err))

				continue
			}
		}

		// Handle connection in background
		f.wg.Add(1)

		go f.handleConnection(ctx, conn)
	}
}

// startStdinStdout starts stdio mode using stdin/stdout.
func (f *Frontend) startStdinStdout(ctx context.Context, mode ModeConfig) error {
	// Create a connection using stdin/stdout
	stdinStdout := &stdinStdoutConn{
		stdin:  os.Stdin,
		stdout: os.Stdout,
	}

	f.wg.Add(1)

	go f.handleConnection(ctx, stdinStdout)

	f.logger.Info("stdin/stdout mode started")

	return nil
}

// startNamedPipes starts named pipes mode (Windows).
func (f *Frontend) startNamedPipes(ctx context.Context, mode ModeConfig) error {
	// Named pipes implementation would go here
	// For now, return not implemented
	return customerrors.New(customerrors.TypeInternal, "named pipes mode not implemented on this platform").
		WithComponent("frontend_stdio")
}

// stdinStdoutConn wraps stdin/stdout as a ReadWriteCloser.
type stdinStdoutConn struct {
	stdin  *os.File
	stdout *os.File
}

func (s *stdinStdoutConn) Read(p []byte) (n int, err error) {
	return s.stdin.Read(p)
}

func (s *stdinStdoutConn) Write(p []byte) (n int, err error) {
	return s.stdout.Write(p)
}

func (s *stdinStdoutConn) Close() error {
	// Don't actually close stdin/stdout
	return nil
}

// handleConnection handles a single client connection.
func (f *Frontend) handleConnection(ctx context.Context, rawConn io.ReadWriteCloser) {
	defer f.wg.Done()
	defer f.closeConnection(rawConn)

	// Setup client connection
	conn, err := f.setupClientConnection(rawConn)
	if err != nil {
		f.logger.Warn("Failed to setup client connection", zap.Error(err))

		return
	}

	// Register connection
	if !f.registerConnection(conn) {
		return
	}
	defer f.unregisterConnection(conn)

	f.logger.Info("client connected", zap.String("client_id", conn.id))

	// Process requests
	f.processRequests(ctx, conn)
}

// closeConnection safely closes the raw connection.
func (f *Frontend) closeConnection(rawConn io.ReadWriteCloser) {
	if err := rawConn.Close(); err != nil {
		f.logger.Warn("Failed to close connection", zap.Error(err))
	}
}

// setupClientConnection creates and initializes a client connection.
func (f *Frontend) setupClientConnection(rawConn io.ReadWriteCloser) (*ClientConnection, error) {
	clientID := fmt.Sprintf("stdio-%d", atomic.AddUint64(&f.clientCounter, 1))

	return &ClientConnection{
		id:       clientID,
		reader:   json.NewDecoder(rawConn),
		writer:   json.NewEncoder(rawConn),
		conn:     rawConn,
		created:  time.Now(),
		lastUsed: time.Now(),
	}, nil
}

// registerConnection registers a new client connection.
func (f *Frontend) registerConnection(conn *ClientConnection) bool {
	f.connectionsMu.Lock()
	defer f.connectionsMu.Unlock()

	if len(f.connections) >= f.config.Process.MaxConcurrentClients {
		f.logger.Warn("max concurrent clients reached", zap.String("client_id", conn.id))

		return false
	}

	f.connections[conn.id] = conn
	f.updateMetrics(func(m *FrontendMetrics) {
		m.ActiveConnections++
		m.TotalConnections++
	})

	return true
}

// unregisterConnection removes a client connection.
func (f *Frontend) unregisterConnection(conn *ClientConnection) {
	f.connectionsMu.Lock()
	delete(f.connections, conn.id)
	f.connectionsMu.Unlock()

	f.updateMetrics(func(m *FrontendMetrics) {
		m.ActiveConnections--
	})
}

// processRequests handles incoming requests from the client.
func (f *Frontend) processRequests(ctx context.Context, conn *ClientConnection) {
	for {
		if !f.continueProcessing() {
			return
		}

		// Read and process single request
		if !f.processSingleRequest(ctx, conn) {
			return
		}
	}
}

// continueProcessing checks if processing should continue.
func (f *Frontend) continueProcessing() bool {
	select {
	case <-f.shutdownCh:
		return false
	default:
		return true
	}
}

// processSingleRequest processes a single request from the client.
func (f *Frontend) processSingleRequest(ctx context.Context, conn *ClientConnection) bool {
	// Set read timeout
	f.setReadTimeout(conn)

	// Read request
	req, err := f.readRequest(conn)
	if err != nil {
		return f.handleReadError(err, conn)
	}

	// Update connection activity
	f.updateConnectionActivity(conn)

	// Process the request
	f.handleRequest(ctx, conn, req)

	return true
}

// setReadTimeout sets the read timeout on the connection.
func (f *Frontend) setReadTimeout(conn *ClientConnection) {
	if netConn, ok := conn.conn.(net.Conn); ok {
		_ = netConn.SetReadDeadline(time.Now().Add(f.config.Process.ClientTimeout))
	}
}

// readRequest reads a request from the connection.
func (f *Frontend) readRequest(conn *ClientConnection) (*mcp.Request, error) {
	var req mcp.Request

	err := conn.reader.Decode(&req)

	return &req, err
}

// handleReadError handles errors from reading requests.
func (f *Frontend) handleReadError(err error, conn *ClientConnection) bool {
	if errors.Is(err, io.EOF) {
		f.logger.Info("client disconnected", zap.String("client_id", conn.id))

		return false
	}

	f.logger.Error("failed to decode request", zap.String("client_id", conn.id), zap.Error(err))
	f.updateMetrics(func(m *FrontendMetrics) {
		m.ErrorCount++
	})

	return true // Continue processing
}

// updateConnectionActivity updates the last used time for the connection.
func (f *Frontend) updateConnectionActivity(conn *ClientConnection) {
	conn.mu.Lock()
	conn.lastUsed = time.Now()
	conn.mu.Unlock()
}

// handleRequest processes an authenticated request.
func (f *Frontend) handleRequest(ctx context.Context, conn *ClientConnection, req *mcp.Request) {
	// Authenticate if required
	if !f.authenticateRequest(ctx, conn, req) {
		return
	}

	// Route request asynchronously
	f.wg.Add(1)

	go f.routeRequest(ctx, conn, req)
}

// authenticateRequest handles request authentication.
func (f *Frontend) authenticateRequest(ctx context.Context, conn *ClientConnection, req *mcp.Request) bool {
	if !f.config.Process.AuthRequired || f.auth == nil {
		return true
	}

	// Extract credentials
	credentials := f.extractCredentials(req)

	// Authenticate
	authed, err := f.auth.Authenticate(ctx, credentials)
	if err != nil || !authed {
		f.logger.Warn("authentication failed", zap.String("client_id", conn.id))
		f.sendErrorResponse(conn, req, "authentication failed")

		return false
	}

	return true
}

// extractCredentials extracts authentication credentials from request.
func (f *Frontend) extractCredentials(req *mcp.Request) map[string]string {
	credentials := make(map[string]string)

	params, ok := req.Params.(map[string]interface{})
	if !ok {
		return credentials
	}

	// Extract common auth fields
	f.extractAuthField(params, credentials, "auth_token")
	f.extractAuthField(params, credentials, "bearer_token")
	f.extractAuthField(params, credentials, "username")
	f.extractAuthField(params, credentials, "password")

	// Extract additional auth headers
	for key, value := range params {
		if f.isAuthField(key) {
			if valueStr, ok := value.(string); ok {
				credentials[key] = valueStr
			}
		}
	}

	return credentials
}

// extractAuthField extracts a single auth field from params.
func (f *Frontend) extractAuthField(params map[string]interface{}, credentials map[string]string, field string) {
	if value, exists := params[field]; exists {
		if str, ok := value.(string); ok {
			credentials[field] = str
		}
	}
}

// isAuthField checks if a field name is auth-related.
func (f *Frontend) isAuthField(field string) bool {
	return field == "authorization" || field == "x-auth-token" || field == "x-api-key"
}

// routeRequest processes a single request.
func (f *Frontend) routeRequest(ctx context.Context, conn *ClientConnection, req *mcp.Request) {
	defer f.wg.Done()

	ctx, cancel := context.WithTimeout(ctx, defaultTimeoutSeconds*time.Second)
	defer cancel()

	// Route request through the router
	resp, err := f.router.RouteRequest(ctx, req, "")
	if err != nil {
		f.logger.Error("request routing failed",
			zap.String("client_id", conn.id),
			zap.Any("request_id", req.ID),
			zap.Error(err))
		f.sendErrorResponse(conn, req, err.Error())
		f.updateMetrics(func(m *FrontendMetrics) {
			m.ErrorCount++
		})

		return
	}

	// Send response
	conn.mu.Lock()
	err = conn.writer.Encode(resp)
	conn.mu.Unlock()

	if err != nil {
		f.logger.Error("failed to send response",
			zap.String("client_id", conn.id),
			zap.Any("request_id", req.ID),
			zap.Error(err))
		f.updateMetrics(func(m *FrontendMetrics) {
			m.ErrorCount++
		})

		return
	}

	f.updateMetrics(func(m *FrontendMetrics) {
		m.RequestCount++
	})

	f.logger.Debug("request handled successfully",
		zap.String("client_id", conn.id),
		zap.Any("request_id", req.ID))
}

// sendErrorResponse sends an error response to the client.
func (f *Frontend) sendErrorResponse(conn *ClientConnection, req *mcp.Request, errorMsg string) {
	errorResp := &mcp.Response{
		ID: req.ID,
		Error: &mcp.Error{
			Code:    -1,
			Message: errorMsg,
		},
	}

	conn.mu.Lock()
	_ = conn.writer.Encode(errorResp)
	conn.mu.Unlock()
}

// connectionCleanup periodically cleans up idle connections.
func (f *Frontend) connectionCleanup() {
	defer f.wg.Done()

	ticker := time.NewTicker(defaultMaxTimeoutSeconds * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-f.shutdownCh:
			return
		case <-ticker.C:
			f.cleanupIdleConnections()
		}
	}
}

// cleanupIdleConnections removes connections that have been idle too long.
func (f *Frontend) cleanupIdleConnections() {
	f.connectionsMu.Lock()
	defer f.connectionsMu.Unlock()

	now := time.Now()

	for clientID, conn := range f.connections {
		conn.mu.RLock()
		lastUsed := conn.lastUsed
		conn.mu.RUnlock()

		if now.Sub(lastUsed) > f.config.Process.ClientTimeout {
			f.logger.Info("closing idle connection", zap.String("client_id", clientID))

			_ = conn.conn.Close()

			delete(f.connections, clientID)
			f.updateMetrics(func(m *FrontendMetrics) {
				m.ActiveConnections--
			})
		}
	}
}

// Stop gracefully shuts down the stdio frontend.
func (f *Frontend) Stop(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if !f.running {
		return nil
	}

	f.logger.Info("stopping stdio frontend")

	// Signal shutdown
	close(f.shutdownCh)

	// Close Unix socket listener
	if f.unixListener != nil {
		_ = f.unixListener.Close()
	}

	// Close all active connections
	f.connectionsMu.Lock()

	for clientID, conn := range f.connections {
		f.logger.Debug("closing connection", zap.String("client_id", clientID))

		_ = conn.conn.Close()
	}

	f.connections = make(map[string]*ClientConnection)
	f.connectionsMu.Unlock()

	// Wait for background routines
	done := make(chan struct{})

	go func() {
		f.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(defaultRetryCount * time.Second):
		f.logger.Warn("background routines did not finish in time")
	case <-ctx.Done():
		return ctx.Err()
	}

	f.running = false
	f.updateMetrics(func(m *FrontendMetrics) {
		m.IsRunning = false
		m.ActiveConnections = 0
	})

	f.logger.Info("stdio frontend stopped")

	return nil
}

// GetName returns the frontend name.
func (f *Frontend) GetName() string {
	return f.name
}

// GetProtocol returns the frontend protocol.
func (f *Frontend) GetProtocol() string {
	return "stdio"
}

// GetMetrics returns current frontend metrics.
func (f *Frontend) GetMetrics() FrontendMetrics {
	f.mu_metrics.RLock()
	defer f.mu_metrics.RUnlock()

	return f.metrics
}

// updateMetrics safely updates frontend metrics.
func (f *Frontend) updateMetrics(updateFn func(*FrontendMetrics)) {
	f.mu_metrics.Lock()
	updateFn(&f.metrics)
	f.mu_metrics.Unlock()
}
