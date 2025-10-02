package http

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	nethttp "net/http"
	"sync"
	"time"

	"go.uber.org/zap"

	authpkg "github.com/actual-software/mcp-bridge/services/gateway/internal/auth"
	customerrors "github.com/actual-software/mcp-bridge/services/gateway/internal/errors"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/frontends/types"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/logging"
	"github.com/actual-software/mcp-bridge/services/router/pkg/mcp"
)

type contextKey string

const (
	contextKeyUser contextKey = "user"
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

// Frontend implements the HTTP frontend.
type Frontend struct {
	name   string
	config Config
	logger *zap.Logger
	router types.RequestRouter
	auth   types.AuthProvider

	// HTTP server
	server *nethttp.Server
	mux    *nethttp.ServeMux

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

// CreateHTTPFrontend creates a new HTTP frontend instance.
func CreateHTTPFrontend(
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
		name:       name,
		config:     config,
		logger:     logger.With(zap.String("frontend", name), zap.String("protocol", "http")),
		router:     router,
		auth:       auth,
		mux:        nethttp.NewServeMux(),
		ctx:        ctx,
		cancel:     cancel,
		shutdownCh: make(chan struct{}),
	}
}

// Start initializes the frontend and begins accepting connections.
func (f *Frontend) Start(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.running {
		return fmt.Errorf("http frontend already running")
	}

	// Setup routes
	f.mux.HandleFunc(f.config.RequestPath, f.handleRequest)
	f.mux.HandleFunc("/health", f.handleHealth)

	addr := fmt.Sprintf("%s:%d", f.config.Host, f.config.Port)

	f.server = &nethttp.Server{
		Addr:           addr,
		Handler:        f.mux,
		ReadTimeout:    f.config.ReadTimeout,
		WriteTimeout:   f.config.WriteTimeout,
		IdleTimeout:    f.config.IdleTimeout,
		MaxHeaderBytes: f.config.MaxHeaderBytes,
		TLSConfig:      f.createTLSConfig(),
	}

	// Start server
	f.wg.Add(1)
	go func() {
		defer f.wg.Done()

		var err error
		if f.config.TLS.Enabled {
			f.logger.Info("starting HTTP frontend with TLS",
				zap.String("address", addr),
				zap.String("path", f.config.RequestPath))
			err = f.server.ListenAndServeTLS(f.config.TLS.CertFile, f.config.TLS.KeyFile)
		} else {
			f.logger.Info("starting HTTP frontend",
				zap.String("address", addr),
				zap.String("path", f.config.RequestPath))
			err = f.server.ListenAndServe()
		}

		if err != nil && err != nethttp.ErrServerClosed {
			f.logger.Error("server error", zap.Error(err))
		}
	}()

	f.running = true
	f.updateMetrics(func(m *types.FrontendMetrics) {
		m.IsRunning = true
	})

	f.logger.Info("http frontend started",
		zap.String("address", addr),
		zap.String("path", f.config.RequestPath),
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

	f.logger.Info("stopping http frontend")

	// Signal shutdown
	close(f.shutdownCh)
	f.cancel()

	// Shutdown HTTP server
	if f.server != nil {
		shutdownCtx, cancel := context.WithTimeout(ctx, shutdownTimeout)
		defer cancel()

		if err := f.server.Shutdown(shutdownCtx); err != nil {
			f.logger.Error("error shutting down HTTP server", zap.Error(err))
		}
	}

	// Wait for goroutines to finish
	done := make(chan struct{})
	go func() {
		f.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		f.logger.Info("http frontend stopped gracefully")
	case <-time.After(shutdownTimeout):
		f.logger.Warn("http frontend shutdown timeout")
	}

	f.updateMetrics(func(m *types.FrontendMetrics) {
		m.IsRunning = false
	})

	return nil
}

// GetName returns the frontend name.
func (f *Frontend) GetName() string {
	return f.name
}

// GetProtocol returns the frontend protocol type.
func (f *Frontend) GetProtocol() string {
	return "http"
}

// GetMetrics returns frontend-specific metrics.
func (f *Frontend) GetMetrics() types.FrontendMetrics {
	f.metricsMu.RLock()
	defer f.metricsMu.RUnlock()
	return f.metrics
}

// handleRequest handles HTTP POST requests with JSON-RPC payloads.
func (f *Frontend) handleRequest(w nethttp.ResponseWriter, r *nethttp.Request) {
	r = r.WithContext(f.initializeRequestContext(r))

	if !f.validateHTTPMethod(w, r.Method) {
		return
	}

	r.Body = nethttp.MaxBytesReader(w, r.Body, f.config.MaxRequestSize)

	authClaims, ok := f.authenticateRequest(w, r, r.Context())
	if !ok {
		return
	}

	ctx := logging.ContextWithUserInfo(r.Context(), authClaims.Subject, "")
	r = r.WithContext(ctx)

	body, ok := f.readRequestBody(w, r, r.Context())
	if !ok {
		return
	}

	resp, err := f.processRequest(r.Context(), body, authClaims)
	if err != nil {
		f.handleProcessingError(w, r.Context(), err, body)
		return
	}

	f.sendSuccessResponse(w, resp)
}

func (f *Frontend) initializeRequestContext(r *nethttp.Request) context.Context {
	traceID := logging.GenerateTraceID()
	requestID := logging.GenerateRequestID()
	return logging.ContextWithTracing(r.Context(), traceID, requestID)
}

func (f *Frontend) validateHTTPMethod(w nethttp.ResponseWriter, method string) bool {
	if method != nethttp.MethodPost {
		f.sendHTTPError(w, nethttp.StatusMethodNotAllowed, "method not allowed")
		f.updateMetrics(func(m *types.FrontendMetrics) {
			m.ErrorCount++
		})
		return false
	}
	return true
}

func (f *Frontend) authenticateRequest(
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

func (f *Frontend) readRequestBody(
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

func (f *Frontend) handleProcessingError(
	w nethttp.ResponseWriter,
	ctx context.Context,
	err error,
	body []byte,
) {
	logging.LogError(ctx, f.logger, "Failed to process request", err,
		zap.ByteString("body", body))

	errorResp := &mcp.Response{
		JSONRPC: "2.0",
		Error: &mcp.Error{
			Code:    mcp.ErrorCodeInternalError,
			Message: err.Error(),
		},
	}
	f.sendJSONResponse(w, nethttp.StatusOK, errorResp)
	f.updateMetrics(func(m *types.FrontendMetrics) {
		m.ErrorCount++
	})
}

func (f *Frontend) sendSuccessResponse(w nethttp.ResponseWriter, resp *mcp.Response) {
	f.sendJSONResponse(w, nethttp.StatusOK, resp)
	f.updateMetrics(func(m *types.FrontendMetrics) {
		m.TotalConnections++
		m.RequestCount++
	})
}

// processRequest processes an HTTP request.
func (f *Frontend) processRequest(ctx context.Context, data []byte, authClaims *authpkg.Claims) (*mcp.Response, error) {
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

	// Create a session context (HTTP requests are stateless, but we need to pass context)
	ctx = context.WithValue(ctx, contextKeyUser, authClaims.Subject)

	// Route the request
	resp, err := f.router.RouteRequest(ctx, &req, wireMsg.TargetNamespace)
	if err != nil {
		return nil, err
	}

	return resp, nil
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

// sendJSONResponse sends a JSON response.
func (f *Frontend) sendJSONResponse(w nethttp.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	if err := json.NewEncoder(w).Encode(data); err != nil {
		f.logger.Error("Failed to encode JSON response", zap.Error(err))
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
