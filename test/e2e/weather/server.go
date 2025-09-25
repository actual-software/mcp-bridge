
// Package weather provides HTTP server implementation for the Weather MCP server
package weather

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Default server configuration constants.
const (
	defaultPort           = 8080
	defaultMetricsPort    = 9090
	defaultReadTimeout    = 30
	defaultWriteTimeout   = 30
	defaultIdleTimeout    = 60
	defaultRequestsPerMin = 1000
	defaultBurstSize      = 100
	defaultFailureThresh  = 5
	defaultSuccessThresh  = 2
	defaultCircuitTimeout = 30
	shutdownTimeout       = 30
	serverReadTimeout     = 10 // seconds
	
	// Health check status constants.
	healthStatusUnhealthy = "unhealthy"
)

// ServerConfig holds configuration for the Weather MCP server.
type ServerConfig struct {
	// Server configuration
	Port         int           `json:"port"          yaml:"port"`
	Host         string        `json:"host"          yaml:"host"`
	ReadTimeout  time.Duration `json:"read_timeout"  yaml:"read_timeout"`
	WriteTimeout time.Duration `json:"write_timeout" yaml:"write_timeout"`
	IdleTimeout  time.Duration `json:"idle_timeout"  yaml:"idle_timeout"`

	// TLS configuration
	TLSEnabled  bool   `json:"tls_enabled"   yaml:"tls_enabled"`
	TLSCertFile string `json:"tls_cert_file" yaml:"tls_cert_file"`
	TLSKeyFile  string `json:"tls_key_file"  yaml:"tls_key_file"`

	// Observability
	MetricsEnabled bool   `json:"metrics_enabled" yaml:"metrics_enabled"`
	MetricsPort    int    `json:"metrics_port"    yaml:"metrics_port"`
	LogLevel       string `json:"log_level"       yaml:"log_level"`
	LogFormat      string `json:"log_format"      yaml:"log_format"`

	// Rate limiting
	RateLimitEnabled bool `json:"rate_limit_enabled"  yaml:"rate_limit_enabled"`
	RequestsPerMin   int  `json:"requests_per_minute" yaml:"requests_per_minute"`
	BurstSize        int  `json:"burst_size"          yaml:"burst_size"`

	// Circuit breaker
	CircuitBreakerEnabled bool          `json:"circuit_breaker_enabled" yaml:"circuit_breaker_enabled"`
	FailureThreshold      int           `json:"failure_threshold"       yaml:"failure_threshold"`
	SuccessThreshold      int           `json:"success_threshold"       yaml:"success_threshold"`
	CircuitBreakerTimeout time.Duration `json:"circuit_breaker_timeout" yaml:"circuit_breaker_timeout"`
}

// DefaultServerConfig returns default server configuration.
func DefaultServerConfig() *ServerConfig {
	return &ServerConfig{
		Port:         defaultPort,
		Host:         "0.0.0.0",
		ReadTimeout:  defaultReadTimeout * time.Second,
		WriteTimeout: defaultWriteTimeout * time.Second,
		IdleTimeout:  defaultIdleTimeout * time.Second,

		MetricsEnabled: true,
		MetricsPort:    defaultMetricsPort,
		LogLevel:       "info",
		LogFormat:      "json",

		RateLimitEnabled: true,
		RequestsPerMin:   defaultRequestsPerMin,
		BurstSize:        defaultBurstSize,

		CircuitBreakerEnabled: true,
		FailureThreshold:      defaultFailureThresh,
		SuccessThreshold:      defaultSuccessThresh,
		CircuitBreakerTimeout: defaultCircuitTimeout * time.Second,
	}
}

// Server represents the HTTP server for Weather MCP.
type Server struct {
	config        *ServerConfig
	mcpServer     *WeatherMCPServer
	httpServer    *http.Server
	metricsServer *http.Server
	logger        *zap.Logger

	// Prometheus metrics
	requestCounter    *prometheus.CounterVec
	requestDuration   *prometheus.HistogramVec
	activeConnections prometheus.Gauge
}

// NewServer creates a new Weather MCP HTTP server.
func NewServer(config *ServerConfig) (*Server, error) {
	if config == nil {
		config = DefaultServerConfig()
	}

	// Set up logger
	logger, err := setupLogger(config.LogLevel, config.LogFormat)
	if err != nil {
		return nil, fmt.Errorf("failed to setup logger: %w", err)
	}

	// Create MCP server
	mcpServer := NewWeatherMCPServer(logger)

	// Create HTTP server
	mux := http.NewServeMux()

	server := &Server{
		config:    config,
		mcpServer: mcpServer,
		logger:    logger,
	}

	// Set up routes
	server.setupRoutes(mux)

	// Set up Prometheus metrics if enabled
	if config.MetricsEnabled {
		server.setupMetrics()
	}

	// Create HTTP server with proper middleware
	server.httpServer = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", config.Host, config.Port),
		Handler:      server.loggingMiddleware(server.metricsMiddleware(mux)),
		ReadTimeout:  config.ReadTimeout,
		WriteTimeout: config.WriteTimeout,
		IdleTimeout:  config.IdleTimeout,
	}

	return server, nil
}

// setupRoutes configures HTTP routes.
func (s *Server) setupRoutes(mux *http.ServeMux) {
	// MCP WebSocket endpoint
	mux.HandleFunc("/mcp", s.mcpServer.HandleWebSocket)

	// Health endpoints
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/health/live", s.handleLiveness)
	mux.HandleFunc("/health/ready", s.handleReadiness)

	// Metrics endpoint
	mux.HandleFunc("/metrics/custom", s.handleMetrics)

	// Info endpoint
	mux.HandleFunc("/info", s.handleInfo)
}

// setupMetrics initializes Prometheus metrics.
func (s *Server) setupMetrics() {
	s.requestCounter = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "weather_mcp_http_requests_total",
			Help: "Total number of HTTP requests",
		},
		[]string{"method", "endpoint", "status"},
	)

	s.requestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "weather_mcp_http_request_duration_seconds",
			Help:    "HTTP request duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "endpoint"},
	)

	s.activeConnections = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "weather_mcp_active_connections",
			Help: "Number of active WebSocket connections",
		},
	)

	// Register metrics
	prometheus.MustRegister(s.requestCounter)
	prometheus.MustRegister(s.requestDuration)
	prometheus.MustRegister(s.activeConnections)

	// Start metrics server
	metricsMux := http.NewServeMux()
	metricsMux.Handle("/metrics", promhttp.Handler())

	s.metricsServer = &http.Server{
		Addr:              fmt.Sprintf(":%d", s.config.MetricsPort),
		Handler:           metricsMux,
		ReadHeaderTimeout: serverReadTimeout * time.Second,
	}

	go func() {
		s.logger.Info("Starting metrics server", zap.Int("port", s.config.MetricsPort))

		if err := s.metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Error("Metrics server failed", zap.Error(err))
		}
	}()
}

// loggingMiddleware logs HTTP requests.
func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Wrap response writer to capture status
		wrapped := &responseWriter{
			ResponseWriter: w,
			statusCode:     http.StatusOK,
		}

		// Process request
		next.ServeHTTP(wrapped, r)

		// Log request
		duration := time.Since(start)
		s.logger.Info("HTTP request",
			zap.String("method", r.Method),
			zap.String("path", r.URL.Path),
			zap.Int("status", wrapped.statusCode),
			zap.Duration("duration", duration),
			zap.String("remote", r.RemoteAddr),
		)
	})
}

// metricsMiddleware records Prometheus metrics.
func (s *Server) metricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.requestCounter == nil {
			next.ServeHTTP(w, r)

			return
		}

		start := time.Now()

		// Wrap response writer to capture status
		wrapped := &responseWriter{
			ResponseWriter: w,
			statusCode:     http.StatusOK,
		}

		// Process request
		next.ServeHTTP(wrapped, r)

		// Record metrics
		duration := time.Since(start).Seconds()
		status := strconv.Itoa(wrapped.statusCode)

		s.requestCounter.WithLabelValues(r.Method, r.URL.Path, status).Inc()
		s.requestDuration.WithLabelValues(r.Method, r.URL.Path).Observe(duration)
	})
}

// handleHealth returns comprehensive health status.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	health := s.mcpServer.GetHealth()

	// Determine HTTP status based on health
	status := http.StatusOK

	switch health.Status {
	case healthStatusUnhealthy:
		status = http.StatusServiceUnavailable
	case "degraded":
		status = http.StatusPartialContent
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(health); err != nil {
		s.logger.Error("Failed to encode health response", zap.Error(err))
	}
}

// handleLiveness returns liveness probe response.
func (s *Server) handleLiveness(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(map[string]string{
		"status": "alive",
		"time":   time.Now().Format(time.RFC3339),
	}); err != nil {
		s.logger.Error("Failed to encode liveness response", zap.Error(err))
	}
}

// handleReadiness returns readiness probe response.
func (s *Server) handleReadiness(w http.ResponseWriter, r *http.Request) {
	health := s.mcpServer.GetHealth()

	// Check if server is ready
	ready := health.Status != healthStatusUnhealthy && health.APIHealthy

	status := http.StatusOK
	if !ready {
		status = http.StatusServiceUnavailable
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"ready":   ready,
		"status":  health.Status,
		"api":     health.APIHealthy,
		"circuit": health.CircuitBreaker,
		"time":    time.Now().Format(time.RFC3339),
	}); err != nil {
		s.logger.Error("Failed to encode readiness response", zap.Error(err))
	}
}

// handleMetrics returns custom metrics.
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	metrics := s.mcpServer.GetMetrics()

	// Update Prometheus gauge for active connections
	if s.activeConnections != nil {
		s.activeConnections.Set(float64(metrics.ActiveConnections))
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(metrics); err != nil {
		s.logger.Error("Failed to encode metrics response", zap.Error(err))
	}
}

// handleInfo returns server information.
func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	info := map[string]interface{}{
		"name":    "weather-mcp-server",
		"version": "1.0.0",
		"config": map[string]interface{}{
			"port":                    s.config.Port,
			"metrics_enabled":         s.config.MetricsEnabled,
			"rate_limit_enabled":      s.config.RateLimitEnabled,
			"circuit_breaker_enabled": s.config.CircuitBreakerEnabled,
		},
		"uptime": time.Since(s.mcpServer.healthStatus.LastCheck).String(),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(info); err != nil {
		s.logger.Error("Failed to encode info response", zap.Error(err))
	}
}

// Start starts the HTTP server.
func (s *Server) Start() error {
	s.logger.Info("Starting Weather MCP server",
		zap.String("address", s.httpServer.Addr),
		zap.Bool("tls", s.config.TLSEnabled),
	)

	if s.config.TLSEnabled {
		return s.httpServer.ListenAndServeTLS(s.config.TLSCertFile, s.config.TLSKeyFile)
	}

	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("Shutting down Weather MCP server")

	// Shutdown metrics server if running
	if s.metricsServer != nil {
		if err := s.metricsServer.Shutdown(ctx); err != nil {
			s.logger.Error("Failed to shutdown metrics server", zap.Error(err))
		}
	}

	// Shutdown MCP server
	s.mcpServer.Shutdown()

	// Shutdown HTTP server
	return s.httpServer.Shutdown(ctx)
}

// Run starts the server and handles graceful shutdown.
func (s *Server) Run() error {
	// Set up signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start server in goroutine
	errChan := make(chan error, 1)

	go func() {
		if err := s.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errChan <- err
		}
	}()

	// Wait for signal or error
	select {
	case err := <-errChan:
		return err
	case sig := <-sigChan:
		s.logger.Info("Received signal", zap.String("signal", sig.String()))

		// Graceful shutdown with timeout
		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout*time.Second)
		defer cancel()

		return s.Shutdown(ctx)
	}
}

// responseWriter wraps http.ResponseWriter to capture status code
// and implements http.Hijacker for WebSocket support.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
	written    bool
}

func (w *responseWriter) WriteHeader(statusCode int) {
	if !w.written {
		w.statusCode = statusCode
		w.written = true
		w.ResponseWriter.WriteHeader(statusCode)
	}
}

func (w *responseWriter) Write(b []byte) (int, error) {
	if !w.written {
		w.WriteHeader(http.StatusOK)
	}

	return w.ResponseWriter.Write(b)
}

// Hijack implements http.Hijacker interface for WebSocket upgrades.
func (w *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hijacker, ok := w.ResponseWriter.(http.Hijacker); ok {
		return hijacker.Hijack()
	}

	return nil, nil, errors.New("responseWriter does not implement http.Hijacker")
}

// setupLogger configures the zap logger.
func setupLogger(level, format string) (*zap.Logger, error) {
	var config zap.Config

	if format == "json" {
		config = zap.NewProductionConfig()
	} else {
		config = zap.NewDevelopmentConfig()
		config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	}

	// Parse log level
	var zapLevel zapcore.Level
	if err := zapLevel.UnmarshalText([]byte(level)); err != nil {
		return nil, fmt.Errorf("invalid log level: %w", err)
	}

	config.Level = zap.NewAtomicLevelAt(zapLevel)

	return config.Build()
}
