package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/actual-software/mcp-bridge/services/gateway/internal/health"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/router"
)

const (
	healthReadTimeout  = 10 * time.Second
	healthWriteTimeout = 10 * time.Second
	healthIdleTimeout  = 30 * time.Second
	minPathParts       = 4 // Minimum path parts for /health/frontend/{name} or /health/backend/{name}.
)

// HealthHTTPServer provides granular health check endpoints via HTTP.
type HealthHTTPServer struct {
	port   int
	logger *zap.Logger
	health *health.Checker
	router *router.Router
	server *GatewayServer

	// HTTP server
	httpServer *http.Server
	mux        *http.ServeMux

	// Shutdown coordination
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// HealthResponse represents a health check response.
type HealthResponse struct {
	Healthy   bool                   `json:"healthy"`
	Status    string                 `json:"status"`
	Timestamp time.Time              `json:"timestamp"`
	Details   map[string]interface{} `json:"details,omitempty"`
}

// FrontendHealth represents frontend health status.
type FrontendHealth struct {
	Name     string          `json:"name"`
	Protocol string          `json:"protocol"`
	Healthy  bool            `json:"healthy"`
	Status   string          `json:"status"`
	Metrics  FrontendMetrics `json:"metrics"`
}

// FrontendMetrics represents frontend metrics.
type FrontendMetrics struct {
	ActiveConnections uint64 `json:"active_connections"`
	RequestCount      uint64 `json:"request_count"`
	ErrorCount        uint64 `json:"error_count"`
}

// BackendHealth represents backend health status.
type BackendHealth struct {
	Name      string    `json:"name"`
	Namespace string    `json:"namespace"`
	Healthy   bool      `json:"healthy"`
	Status    string    `json:"status"`
	LastCheck time.Time `json:"last_check,omitempty"`
}

// NewHealthHTTPServer creates a new health HTTP server.
func NewHealthHTTPServer(
	port int,
	healthChecker *health.Checker,
	router *router.Router,
	gatewayServer *GatewayServer,
	logger *zap.Logger,
) *HealthHTTPServer {
	ctx, cancel := context.WithCancel(context.Background())

	h := &HealthHTTPServer{
		port:   port,
		logger: logger.With(zap.String("component", "health_http_server")),
		health: healthChecker,
		router: router,
		server: gatewayServer,
		mux:    http.NewServeMux(),
		ctx:    ctx,
		cancel: cancel,
	}

	// Register handlers
	h.setupHandlers()

	return h
}

// setupHandlers registers all health check handlers.
func (h *HealthHTTPServer) setupHandlers() {
	// Overall health endpoints
	h.mux.HandleFunc("/health", h.handleHealth)
	h.mux.HandleFunc("/healthz", h.handleHealthz)
	h.mux.HandleFunc("/ready", h.handleReady)

	// Granular endpoints
	h.mux.HandleFunc("/health/frontends", h.handleFrontendsHealth)
	h.mux.HandleFunc("/health/frontend/", h.handleFrontendHealth)
	h.mux.HandleFunc("/health/backends", h.handleBackendsHealth)
	h.mux.HandleFunc("/health/backend/", h.handleBackendHealth)
	h.mux.HandleFunc("/health/components", h.handleComponentsHealth)
}

// Start starts the health HTTP server.
func (h *HealthHTTPServer) Start() error {
	addr := fmt.Sprintf(":%d", h.port)

	h.httpServer = &http.Server{
		Addr:         addr,
		Handler:      h.mux,
		ReadTimeout:  healthReadTimeout,
		WriteTimeout: healthWriteTimeout,
		IdleTimeout:  healthIdleTimeout,
	}

	h.wg.Add(1)
	go func() {
		defer h.wg.Done()

		h.logger.Info("starting health HTTP server", zap.String("address", addr))

		if err := h.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			h.logger.Error("health HTTP server error", zap.Error(err))
		}
	}()

	return nil
}

// Stop gracefully stops the health HTTP server.
func (h *HealthHTTPServer) Stop(ctx context.Context) error {
	h.cancel()

	if h.httpServer != nil {
		if err := h.httpServer.Shutdown(ctx); err != nil {
			h.logger.Warn("health HTTP server shutdown error", zap.Error(err))
			return err
		}
	}

	h.wg.Wait()
	h.logger.Info("health HTTP server stopped")
	return nil
}

// handleHealth returns overall system health.
func (h *HealthHTTPServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	status := h.health.GetStatus()

	resp := HealthResponse{
		Healthy:   status.Healthy,
		Status:    h.getStatusString(status.Healthy),
		Timestamp: time.Now(),
		Details: map[string]interface{}{
			"endpoints":         status.Endpoints,
			"healthy_endpoints": status.HealthyEndpoints,
			"namespaces":        status.Namespaces,
			"subsystems":        status.Subsystems,
		},
	}

	h.sendJSON(w, resp, status.Healthy)
}

// handleHealthz returns a simple liveness check (Kubernetes style).
func (h *HealthHTTPServer) handleHealthz(w http.ResponseWriter, r *http.Request) {
	// Simple liveness - just check if server is running
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK"))
}

// handleReady returns readiness check (Kubernetes style).
func (h *HealthHTTPServer) handleReady(w http.ResponseWriter, r *http.Request) {
	status := h.health.GetStatus()

	if status.Healthy && status.HealthyEndpoints > 0 {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("READY"))
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("NOT READY"))
	}
}

// handleFrontendsHealth returns health of all frontends.
func (h *HealthHTTPServer) handleFrontendsHealth(w http.ResponseWriter, r *http.Request) {
	if h.server == nil {
		h.sendError(w, http.StatusServiceUnavailable, "gateway server not available")
		return
	}

	frontends := h.getFrontendsHealth()

	allHealthy := true
	for _, f := range frontends {
		if !f.Healthy {
			allHealthy = false
			break
		}
	}

	resp := HealthResponse{
		Healthy:   allHealthy,
		Status:    h.getStatusString(allHealthy),
		Timestamp: time.Now(),
		Details: map[string]interface{}{
			"frontends": frontends,
			"count":     len(frontends),
		},
	}

	h.sendJSON(w, resp, allHealthy)
}

// handleFrontendHealth returns health of a specific frontend.
func (h *HealthHTTPServer) handleFrontendHealth(w http.ResponseWriter, r *http.Request) {
	// Extract frontend name from path: /health/frontend/{name}
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < minPathParts {
		h.sendError(w, http.StatusBadRequest, "invalid frontend path")
		return
	}

	frontendName := parts[3]
	frontends := h.getFrontendsHealth()

	var found *FrontendHealth
	for i := range frontends {
		if frontends[i].Protocol == frontendName {
			found = &frontends[i]
			break
		}
	}

	if found == nil {
		h.sendError(w, http.StatusNotFound, fmt.Sprintf("frontend %s not found", frontendName))
		return
	}

	resp := HealthResponse{
		Healthy:   found.Healthy,
		Status:    h.getStatusString(found.Healthy),
		Timestamp: time.Now(),
		Details: map[string]interface{}{
			"frontend": found,
		},
	}

	h.sendJSON(w, resp, found.Healthy)
}

// handleBackendsHealth returns health of all backends.
func (h *HealthHTTPServer) handleBackendsHealth(w http.ResponseWriter, r *http.Request) {
	if h.router == nil {
		h.sendError(w, http.StatusServiceUnavailable, "router not available")
		return
	}

	backends := h.getBackendsHealth()

	allHealthy := true
	for _, b := range backends {
		if !b.Healthy {
			allHealthy = false
			break
		}
	}

	resp := HealthResponse{
		Healthy:   allHealthy,
		Status:    h.getStatusString(allHealthy),
		Timestamp: time.Now(),
		Details: map[string]interface{}{
			"backends": backends,
			"count":    len(backends),
		},
	}

	h.sendJSON(w, resp, allHealthy)
}

// handleBackendHealth returns health of a specific backend.
func (h *HealthHTTPServer) handleBackendHealth(w http.ResponseWriter, r *http.Request) {
	// Extract backend name from path: /health/backend/{namespace}/{name}
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < minPathParts {
		h.sendError(w, http.StatusBadRequest, "invalid backend path")
		return
	}

	backendName := parts[3]
	backends := h.getBackendsHealth()

	var found *BackendHealth
	for i := range backends {
		if backends[i].Name == backendName {
			found = &backends[i]
			break
		}
	}

	if found == nil {
		h.sendError(w, http.StatusNotFound, fmt.Sprintf("backend %s not found", backendName))
		return
	}

	resp := HealthResponse{
		Healthy:   found.Healthy,
		Status:    h.getStatusString(found.Healthy),
		Timestamp: time.Now(),
		Details: map[string]interface{}{
			"backend": found,
		},
	}

	h.sendJSON(w, resp, found.Healthy)
}

// handleComponentsHealth returns detailed component health.
func (h *HealthHTTPServer) handleComponentsHealth(w http.ResponseWriter, r *http.Request) {
	status := h.health.GetStatus()

	resp := HealthResponse{
		Healthy:   status.Healthy,
		Status:    h.getStatusString(status.Healthy),
		Timestamp: time.Now(),
		Details: map[string]interface{}{
			"subsystems": status.Subsystems,
			"checks":     status.Checks,
		},
	}

	h.sendJSON(w, resp, status.Healthy)
}

// getFrontendsHealth returns health status of all frontends.
func (h *HealthHTTPServer) getFrontendsHealth() []FrontendHealth {
	var result []FrontendHealth

	if h.server == nil {
		return result
	}

	for _, frontend := range h.server.frontends {
		metrics := frontend.GetMetrics()

		result = append(result, FrontendHealth{
			Name:     frontend.GetName(),
			Protocol: frontend.GetProtocol(),
			Healthy:  metrics.IsRunning,
			Status:   h.getStatusString(metrics.IsRunning),
			Metrics: FrontendMetrics{
				ActiveConnections: metrics.ActiveConnections,
				RequestCount:      metrics.RequestCount,
				ErrorCount:        metrics.ErrorCount,
			},
		})
	}

	return result
}

// getBackendsHealth returns health status of all backends.
func (h *HealthHTTPServer) getBackendsHealth() []BackendHealth {
	var result []BackendHealth

	if h.router == nil {
		return result
	}

	// Get backend health from router/discovery
	status := h.health.GetStatus()

	for _, check := range status.Checks {
		result = append(result, BackendHealth{
			Name:      check.Message,
			Namespace: "default",
			Healthy:   check.Healthy,
			Status:    h.getStatusString(check.Healthy),
			LastCheck: check.LastCheck,
		})
	}

	return result
}

// getStatusString converts boolean health to status string.
func (h *HealthHTTPServer) getStatusString(healthy bool) string {
	if healthy {
		return "healthy"
	}
	return "unhealthy"
}

// sendJSON sends a JSON response.
func (h *HealthHTTPServer) sendJSON(w http.ResponseWriter, data interface{}, healthy bool) {
	w.Header().Set("Content-Type", "application/json")

	statusCode := http.StatusOK
	if !healthy {
		statusCode = http.StatusServiceUnavailable
	}
	w.WriteHeader(statusCode)

	if err := json.NewEncoder(w).Encode(data); err != nil {
		h.logger.Error("failed to encode JSON response", zap.Error(err))
	}
}

// sendError sends an error response.
func (h *HealthHTTPServer) sendError(w http.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	resp := map[string]string{
		"error": message,
	}

	_ = json.NewEncoder(w).Encode(resp)
}
