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
	sessionContextKey contextKey = "session"
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

	// Create per-message authenticator if enabled
	if cfg.Auth.PerMessageAuth {
		s.messageAuth = authpkg.CreateMessageLevelAuthenticator(auth, logger, cfg.Auth.PerMessageAuthCache)
	}

	// Initialize frontends from configuration
	s.frontends = make([]frontends.Frontend, 0, len(cfg.Server.Frontends))
	for _, frontendCfg := range cfg.Server.Frontends {
		if !frontendCfg.Enabled {
			logger.Info("skipping disabled frontend",
				zap.String("name", frontendCfg.Name),
				zap.String("protocol", frontendCfg.Protocol))
			continue
		}

		// Create frontend using factory
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

	return s
}

// Start starts the gateway server and all configured frontends.
func (s *GatewayServer) Start() error {
	s.logger.Info("starting gateway server", zap.Int("frontends", len(s.frontends)))

	// Start all frontends
	for _, frontend := range s.frontends {
		if err := frontend.Start(s.ctx); err != nil {
			s.logger.Error("failed to start frontend",
				zap.String("name", frontend.GetName()),
				zap.String("protocol", frontend.GetProtocol()),
				zap.Error(err))
			// Stop previously started frontends
			s.stopStartedFrontends()
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
func (s *GatewayServer) stopStartedFrontends() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for _, frontend := range s.frontends {
		if err := frontend.Stop(ctx); err != nil {
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

	// Stop all frontends
	s.stopStartedFrontends()

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
