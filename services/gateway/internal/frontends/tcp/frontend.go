package tcp

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	authpkg "github.com/actual-software/mcp-bridge/services/gateway/internal/auth"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/frontends/types"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/logging"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/session"
	"github.com/actual-software/mcp-bridge/services/gateway/pkg/wire"
	"github.com/actual-software/mcp-bridge/services/router/pkg/mcp"
)

// ClientConnection represents a connected TCP client.
type ClientConnection struct {
	ID        string
	Conn      net.Conn
	Transport *wire.Transport
	Session   *session.Session
	ClientIP  string
	ctx       context.Context
	cancel    context.CancelFunc
	created   time.Time
	lastUsed  time.Time
	mu        sync.RWMutex
	logger    *zap.Logger
}

// Close closes the client connection.
func (c *ClientConnection) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cancel != nil {
		c.cancel()
	}

	if c.Transport != nil {
		_ = c.Transport.Close()
	}

	return c.Conn.Close()
}

// Frontend implements the TCP Binary frontend.
type Frontend struct {
	name     string
	config   Config
	logger   *zap.Logger
	router   types.RequestRouter
	auth     types.AuthProvider
	sessions types.SessionManager

	// TCP listener
	listener net.Listener

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

// CreateTCPFrontend creates a new TCP Binary frontend instance.
func CreateTCPFrontend(
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
		logger:      logger.With(zap.String("frontend", name), zap.String("protocol", "tcp_binary")),
		router:      router,
		auth:        auth,
		sessions:    sessions,
		connections: make(map[string]*ClientConnection),
		ctx:         ctx,
		cancel:      cancel,
		shutdownCh:  make(chan struct{}),
	}
}

// Start initializes the frontend and begins accepting connections.
func (f *Frontend) Start(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.running {
		return fmt.Errorf("tcp frontend already running")
	}

	addr := fmt.Sprintf("%s:%d", f.config.Host, f.config.Port)

	// Create listener
	var listener net.Listener
	var err error

	if f.config.TLS.Enabled {
		tlsConfig := f.createTLSConfig()
		listener, err = tls.Listen("tcp", addr, tlsConfig)
		if err != nil {
			return fmt.Errorf("failed to create TLS listener: %w", err)
		}
		f.logger.Info("starting TCP Binary frontend with TLS", zap.String("address", addr))
	} else {
		listener, err = net.Listen("tcp", addr)
		if err != nil {
			return fmt.Errorf("failed to create listener: %w", err)
		}
		f.logger.Info("starting TCP Binary frontend", zap.String("address", addr))
	}

	f.listener = listener

	// Start accepting connections
	f.wg.Add(1)
	go f.acceptConnections()

	f.running = true
	f.updateMetrics(func(m *types.FrontendMetrics) {
		m.IsRunning = true
	})

	f.logger.Info("tcp binary frontend started",
		zap.String("address", addr),
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

	f.logger.Info("stopping tcp binary frontend")

	// Signal shutdown
	close(f.shutdownCh)
	f.cancel()

	// Close listener
	if f.listener != nil {
		_ = f.listener.Close()
	}

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
		f.logger.Info("tcp binary frontend stopped gracefully")
	case <-time.After(30 * time.Second):
		f.logger.Warn("tcp binary frontend shutdown timeout")
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
	return "tcp_binary"
}

// GetMetrics returns frontend-specific metrics.
func (f *Frontend) GetMetrics() types.FrontendMetrics {
	f.metricsMu.RLock()
	defer f.metricsMu.RUnlock()
	return f.metrics
}

// acceptConnections accepts incoming TCP connections.
func (f *Frontend) acceptConnections() {
	defer f.wg.Done()

	for {
		select {
		case <-f.shutdownCh:
			return
		default:
			conn, err := f.listener.Accept()
			if err != nil {
				select {
				case <-f.shutdownCh:
					return
				default:
					f.logger.Error("Failed to accept connection", zap.Error(err))
					continue
				}
			}

			// Handle connection in background
			f.wg.Add(1)
			go f.handleConnection(f.ctx, conn)
		}
	}
}

// handleConnection handles a TCP connection.
func (f *Frontend) handleConnection(parentCtx context.Context, conn net.Conn) {
	defer f.wg.Done()
	defer func() { _ = conn.Close() }()

	// Initialize request context
	traceID := logging.GenerateTraceID()
	requestID := logging.GenerateRequestID()
	ctx := logging.ContextWithTracing(parentCtx, traceID, requestID)

	clientIP := getIPFromAddr(conn.RemoteAddr().String())

	// Check connection limits
	currentCount := atomic.LoadInt64(&f.connCount)
	if currentCount >= int64(f.config.MaxConnections) {
		f.logger.Warn("Connection limit reached", zap.String("client_ip", clientIP))
		f.updateMetrics(func(m *types.FrontendMetrics) {
			m.ErrorCount++
		})
		return
	}

	// Create wire transport
	transport := wire.NewTransport(conn)

	// For now, create a simple session without full authentication
	// In production, this should implement proper TCP auth
	claims := &authpkg.Claims{}
	sess, err := f.sessions.CreateSession(claims)
	if err != nil {
		f.logger.Error("Failed to create session", zap.Error(err), zap.String("client_ip", clientIP))
		f.updateMetrics(func(m *types.FrontendMetrics) {
			m.ErrorCount++
		})
		return
	}

	// Create client connection context
	connCtx, connCancel := context.WithCancel(f.ctx)
	defer connCancel()

	// Setup client connection
	clientConn := &ClientConnection{
		ID:        sess.ID,
		Conn:      conn,
		Transport: transport,
		Session:   sess,
		ClientIP:  clientIP,
		ctx:       connCtx,
		cancel:    connCancel,
		created:   time.Now(),
		lastUsed:  time.Now(),
		logger:    f.logger.With(zap.String("session_id", sess.ID), zap.String("client_ip", clientIP)),
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

	clientConn.logger.Info("TCP connection established")

	// Handle messages
	f.handleClientMessages(ctx, clientConn)

	// Cleanup
	f.removeConnection(sess.ID)
}

// handleClientMessages handles messages from a client.
func (f *Frontend) handleClientMessages(ctx context.Context, client *ClientConnection) {
	for {
		select {
		case <-client.ctx.Done():
			return
		case <-f.shutdownCh:
			return
		default:
			// Set read deadline
			if err := client.Transport.SetReadDeadline(time.Now().Add(f.config.ReadTimeout)); err != nil {
				client.logger.Warn("Failed to set read deadline", zap.Error(err))
			}

			// Receive message
			msgType, payload, err := client.Transport.ReceiveMessage()
			if err != nil {
				client.logger.Error("Failed to receive message", zap.Error(err))
				return
			}

			// Process based on message type
			if msgType == wire.MessageTypeRequest {
				req, ok := payload.(*mcp.Request)
				if !ok {
					client.logger.Error("Invalid request payload type")
					continue
				}

				if err := f.processRequest(ctx, client, req); err != nil {
					client.logger.Error("Failed to process request", zap.Error(err))
					// Send error response
					errorResp := &mcp.Response{
						JSONRPC: "2.0",
						ID:      req.ID,
						Error: &mcp.Error{
							Code:    mcp.ErrorCodeInternalError,
							Message: err.Error(),
						},
					}
					if sendErr := client.Transport.SendResponse(errorResp); sendErr != nil {
						client.logger.Error("Failed to send error response", zap.Error(sendErr))
					}
				}
			}
		}
	}
}

// processRequest processes a request from a client.
func (f *Frontend) processRequest(ctx context.Context, client *ClientConnection, req *mcp.Request) error {
	// Update last used time
	client.mu.Lock()
	client.lastUsed = time.Now()
	client.mu.Unlock()

	f.updateMetrics(func(m *types.FrontendMetrics) {
		m.RequestCount++
	})

	// Add session to context
	ctx = context.WithValue(ctx, "session", client.Session)

	// Route the request (no namespace for TCP binary for now)
	resp, err := f.router.RouteRequest(ctx, req, "")
	if err != nil {
		f.updateMetrics(func(m *types.FrontendMetrics) {
			m.ErrorCount++
		})
		return err
	}

	// Send response
	if err := client.Transport.SendResponse(resp); err != nil {
		f.updateMetrics(func(m *types.FrontendMetrics) {
			m.ErrorCount++
		})
		return fmt.Errorf("failed to send response: %w", err)
	}

	return nil
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

		client.logger.Info("Connection removed")
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
	atomic.AddInt64(val.(*int64), 1)
}

// decrementIPConnCount decrements the connection count for an IP.
func (f *Frontend) decrementIPConnCount(ip string) {
	if val, ok := f.ipConnCount.Load(ip); ok {
		count := atomic.AddInt64(val.(*int64), -1)
		if count <= 0 {
			f.ipConnCount.Delete(ip)
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
	cert, err := tls.LoadX509KeyPair(f.config.TLS.CertFile, f.config.TLS.KeyFile)
	if err != nil {
		f.logger.Fatal("Failed to load TLS certificate", zap.Error(err))
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		},
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
