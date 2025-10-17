package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"

	"go.uber.org/zap"

	authpkg "github.com/actual-software/mcp-bridge/services/gateway/internal/auth"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/config"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/frontends"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/health"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/metrics"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/ratelimit"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/router"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/session"
)

// contextKey is a type for context keys to avoid collisions.
type contextKey string

const (
	sessionContextKey       contextKey = "session"
	frontendShutdownTimeout            = 30 * time.Second
)

// multiHandler multiplexes requests across multiple handlers.
// It tries each handler in order. Since each handler is a frontend's mux
// with specific paths registered, the first handler that matches will handle the request.
type multiHandler struct {
	handlers []http.Handler
}

func (m *multiHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Try each handler in sequence, using a response recorder to detect 404s
	for i, h := range m.handlers {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)

		// If this handler returned a non-404 status, use its response
		if rec.Code != http.StatusNotFound {
			// Copy the recorded response to the actual response writer
			for k, v := range rec.Header() {
				w.Header()[k] = v
			}
			w.WriteHeader(rec.Code)
			_, _ = w.Write(rec.Body.Bytes())
			return
		}

		// If this was the last handler and it returned 404, send that 404
		if i == len(m.handlers)-1 {
			for k, v := range rec.Header() {
				w.Header()[k] = v
			}
			w.WriteHeader(rec.Code)
			_, _ = w.Write(rec.Body.Bytes())
		}
	}
}

// GatewayServer handles MCP client connections through pluggable frontends.
type GatewayServer struct {
	config      *config.Config
	logger      *zap.Logger
	auth        authpkg.Provider
	sessions    session.Manager
	router      *router.Router
	health      *health.Checker
	metrics     *metrics.Registry
	rateLimiter ratelimit.RateLimiter

	// Frontend management
	frontends     []frontends.Frontend
	sharedServers map[string]*http.Server

	// Health HTTP server
	healthServer *HealthHTTPServer

	// Server lifecycle
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Per-message authentication
	messageAuth *authpkg.MessageAuthenticator
}

// BootstrapGatewayServer creates a new gateway server with the provided dependencies.
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
		config:        cfg,
		logger:        logger,
		auth:          auth,
		sessions:      sessions,
		router:        router,
		health:        health,
		metrics:       metrics,
		rateLimiter:   rateLimiter,
		sharedServers: make(map[string]*http.Server),
		ctx:           ctx,
		cancel:        cancel,
	}

	s.initializeMessageAuth(cfg, auth, logger)
	s.initializeFrontends(cfg, router, auth, sessions, logger)
	s.initializeHealthServer(cfg, health, router, logger)

	return s
}

func (s *GatewayServer) initializeMessageAuth(
	cfg *config.Config,
	auth authpkg.Provider,
	logger *zap.Logger,
) {
	if cfg.Auth.PerMessageAuth {
		s.messageAuth = authpkg.CreateMessageLevelAuthenticator(
			auth,
			logger,
			cfg.Auth.PerMessageAuthCache,
		)
	}
}

func (s *GatewayServer) initializeFrontends(
	cfg *config.Config,
	router *router.Router,
	auth authpkg.Provider,
	sessions session.Manager,
	logger *zap.Logger,
) {
	s.frontends = make([]frontends.Frontend, 0, len(cfg.Server.Frontends))
	for _, frontendCfg := range cfg.Server.Frontends {
		if !frontendCfg.Enabled {
			logger.Info("skipping disabled frontend",
				zap.String("name", frontendCfg.Name),
				zap.String("protocol", frontendCfg.Protocol))

			continue
		}

		frontend, err := frontends.CreateFrontend(
			frontendCfg.Name,
			frontendCfg.Protocol,
			frontendCfg.Config,
			router,
			auth,
			sessions,
			logger,
		)
		if err != nil {
			logger.Error("failed to create frontend",
				zap.String("name", frontendCfg.Name),
				zap.String("protocol", frontendCfg.Protocol),
				zap.Error(err))

			continue
		}

		s.frontends = append(s.frontends, frontend)
		logger.Info("created frontend",
			zap.String("name", frontendCfg.Name),
			zap.String("protocol", frontendCfg.Protocol))
	}
}

func (s *GatewayServer) initializeHealthServer(
	cfg *config.Config,
	health *health.Checker,
	router *router.Router,
	logger *zap.Logger,
) {
	if cfg.Server.HealthPort > 0 {
		s.healthServer = NewHealthHTTPServer(
			cfg.Server.HealthPort,
			health,
			router,
			s,
			logger,
		)
		logger.Info("created health HTTP server", zap.Int("port", cfg.Server.HealthPort))
	}
}

// Start starts the gateway server and all configured frontends.
func (s *GatewayServer) Start() error {
	s.logger.Info("starting gateway server", zap.Int("frontends", len(s.frontends)))

	// Start health HTTP server if configured
	if s.healthServer != nil {
		if err := s.healthServer.Start(); err != nil {
			return fmt.Errorf("failed to start health HTTP server: %w", err)
		}
	}

	// Initialize all frontends (setup routes, but don't start servers)
	for _, frontend := range s.frontends {
		if err := frontend.Start(s.ctx); err != nil {
			s.logger.Error("failed to initialize frontend",
				zap.String("name", frontend.GetName()),
				zap.String("protocol", frontend.GetProtocol()),
				zap.Error(err))
			// Stop health server
			if s.healthServer != nil {
				_ = s.healthServer.Stop(s.ctx)
			}

			return fmt.Errorf("failed to initialize frontend %s: %w", frontend.GetName(), err)
		}
		s.logger.Info("initialized frontend",
			zap.String("name", frontend.GetName()),
			zap.String("protocol", frontend.GetProtocol()))
	}

	// Start shared HTTP servers (groups frontends by address)
	if err := s.startSharedServers(); err != nil {
		s.logger.Error("failed to start shared servers", zap.Error(err))
		// Stop health server and frontends
		if s.healthServer != nil {
			_ = s.healthServer.Stop(s.ctx)
		}
		s.stopStartedFrontends(s.ctx)

		return fmt.Errorf("failed to start shared servers: %w", err)
	}

	s.logger.Info("gateway server started successfully")

	return nil
}

// startSharedServers groups frontends by address and starts shared HTTP servers.
// Multiple frontends on the same address will share a single HTTP server with multiplexed handlers.
func (s *GatewayServer) startSharedServers() error {
	// Group frontends by address
	addressGroups := make(map[string][]frontends.Frontend)
	for _, frontend := range s.frontends {
		addr := frontend.GetAddress()
		addressGroups[addr] = append(addressGroups[addr], frontend)
	}

	// Create and start a shared server for each address
	for addr, fes := range addressGroups {
		// Create a multiplexing handler that delegates to each frontend's handler
		// Each frontend's handler (their internal mux) has specific paths registered
		var handler http.Handler
		if len(fes) == 1 {
			// Single frontend - use its handler directly
			handler = fes[0].GetHandler()
		} else {
			// Multiple frontends - create a custom multiplexing handler
			// that delegates to each frontend's handler
			handlers := make([]http.Handler, len(fes))
			for i, fe := range fes {
				handlers[i] = fe.GetHandler()
			}
			handler = &multiHandler{handlers: handlers}
		}

		// Check TLS configuration from frontends
		// If any frontend on this address needs TLS, enable it for the shared server
		var tlsEnabled bool
		var certFile, keyFile string
		for _, fe := range fes {
			enabled, cert, key := fe.GetTLSConfig()
			if enabled {
				tlsEnabled = true
				certFile = cert
				keyFile = key
				break
			}
		}

		// Create shared HTTP server
		server := &http.Server{
			Addr:    addr,
			Handler: handler,
		}

		// Start the server in a goroutine
		s.wg.Add(1)
		go func(addr string, srv *http.Server, useTLS bool, cert, key string) {
			defer s.wg.Done()

			if useTLS {
				s.logger.Info("starting shared HTTPS server",
					zap.String("address", addr),
					zap.String("cert_file", cert))

				err := srv.ListenAndServeTLS(cert, key)
				if err != nil && err != http.ErrServerClosed {
					s.logger.Error("shared HTTPS server error",
						zap.String("address", addr),
						zap.Error(err))
				}
			} else {
				s.logger.Info("starting shared HTTP server", zap.String("address", addr))

				err := srv.ListenAndServe()
				if err != nil && err != http.ErrServerClosed {
					s.logger.Error("shared HTTP server error",
						zap.String("address", addr),
						zap.Error(err))
				}
			}
		}(addr, server, tlsEnabled, certFile, keyFile)

		// Store the server and inject it into each frontend
		s.sharedServers[addr] = server
		for _, fe := range fes {
			fe.SetServer(server)
		}

		if tlsEnabled {
			s.logger.Info("started shared HTTPS server",
				zap.String("address", addr),
				zap.Int("frontends", len(fes)),
				zap.Bool("tls", true))
		} else {
			s.logger.Info("started shared HTTP server",
				zap.String("address", addr),
				zap.Int("frontends", len(fes)),
				zap.Bool("tls", false))
		}
	}

	return nil
}

// stopStartedFrontends stops all frontends that have been started.
func (s *GatewayServer) stopStartedFrontends(ctx context.Context) {
	shutdownCtx, cancel := context.WithTimeout(ctx, frontendShutdownTimeout)
	defer cancel()

	for _, frontend := range s.frontends {
		if err := frontend.Stop(shutdownCtx); err != nil {
			s.logger.Error("error stopping frontend",
				zap.String("name", frontend.GetName()),
				zap.String("protocol", frontend.GetProtocol()),
				zap.Error(err))
		}
	}
}

// Shutdown gracefully shuts down the server.
func (s *GatewayServer) Shutdown(ctx context.Context) error {
	s.logger.Info("shutting down gateway server")

	// Cancel context to signal shutdown
	s.cancel()

	// Stop health HTTP server
	if s.healthServer != nil {
		if err := s.healthServer.Stop(ctx); err != nil {
			s.logger.Warn("error stopping health HTTP server", zap.Error(err))
		}
	}

	// Stop all frontends
	s.stopStartedFrontends(ctx)

	// Shutdown shared HTTP servers
	shutdownCtx, cancel := context.WithTimeout(ctx, frontendShutdownTimeout)
	defer cancel()

	for addr, server := range s.sharedServers {
		if err := server.Shutdown(shutdownCtx); err != nil {
			s.logger.Warn("error shutting down shared HTTP server",
				zap.String("address", addr),
				zap.Error(err))
		} else {
			s.logger.Info("shut down shared HTTP server", zap.String("address", addr))
		}
	}

	// Wait for all goroutines to finish
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		s.logger.Info("gateway server shutdown completed")
	case <-ctx.Done():
		s.logger.Warn("gateway server shutdown timeout")
	}

	return nil
}
