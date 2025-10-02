package server

import (
	"context"
	"fmt"
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
	frontends []frontends.Frontend

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
		config:      cfg,
		logger:      logger,
		auth:        auth,
		sessions:    sessions,
		router:      router,
		health:      health,
		metrics:     metrics,
		rateLimiter: rateLimiter,
		ctx:         ctx,
		cancel:      cancel,
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

	// Start all frontends
	for _, frontend := range s.frontends {
		if err := frontend.Start(s.ctx); err != nil {
			s.logger.Error("failed to start frontend",
				zap.String("name", frontend.GetName()),
				zap.String("protocol", frontend.GetProtocol()),
				zap.Error(err))
			// Stop health server and previously started frontends
			if s.healthServer != nil {
				_ = s.healthServer.Stop(s.ctx)
			}
			s.stopStartedFrontends(s.ctx)
			return fmt.Errorf("failed to start frontend %s: %w", frontend.GetName(), err)
		}
		s.logger.Info("started frontend",
			zap.String("name", frontend.GetName()),
			zap.String("protocol", frontend.GetProtocol()))
	}

	s.logger.Info("gateway server started successfully")
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
