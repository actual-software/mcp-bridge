package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/actual-software/mcp-bridge/services/gateway/internal/auth"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/config"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/discovery"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/health"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/metrics"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/ratelimit"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/router"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/server"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/session"
)

const (
	defaultTimeoutSeconds = 30
	portOffset            = 1000 // Offset for TCP port from HTTP port
)

var (
	Version   = "v1.0.0"
	BuildTime = "unknown"
	GitCommit = "unknown"
)

// VersionRequestedError is returned when the version flag is set.
type VersionRequestedError struct{}

func (e VersionRequestedError) Error() string {
	return "version requested"
}

func main() {
	rootCmd := &cobra.Command{
		Use:   "mcp-gateway",
		Short: "MCP Gateway - Kubernetes gateway for MCP servers",
		Long: `MCP Gateway provides WebSocket endpoints for MCP clients and
routes requests to appropriate MCP servers running in Kubernetes.`,
		RunE: run,
	}

	rootCmd.Flags().StringP("config", "c", "/etc/mcp-gateway/gateway.yaml", "Path to configuration file")
	rootCmd.Flags().BoolP("version", "v", false, "Show version information")
	rootCmd.Flags().String("log-level", "info", "Log level (debug, info, warn, error)")

	// Add subcommands
	rootCmd.AddCommand(adminCmd())

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run(cmd *cobra.Command, args []string) error {
	if err := handleVersionFlag(cmd); err != nil {
		var errVersionRequested VersionRequestedError
		if errors.As(err, &errVersionRequested) {
			return nil // Version flag was handled, exit cleanly
		}

		return err
	}

	logger, err := setupLogger(cmd)
	if err != nil {
		return err
	}
	defer syncLogger(logger)

	cfg, err := loadConfig(cmd)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	components, err := initializeComponents(ctx, cfg, logger)
	if err != nil {
		return err
	}

	servers, serverErrChan, err := startServers(cfg, components, logger)
	if err != nil {
		return err
	}

	return waitForShutdownAndCleanup(ctx, cancel, servers, components, serverErrChan, logger)
}

type Components struct {
	SessionMgr       session.Manager
	AuthProvider     auth.Provider
	ServiceDiscovery discovery.ServiceDiscovery
	RequestRouter    *router.Router
	HealthChecker    *health.Checker
	RateLimiter      ratelimit.RateLimiter
	MetricsRegistry  *metrics.Registry
}

type Servers struct {
	Gateway *server.GatewayServer
	Metrics *http.Server
	Health  *health.Server
}

func handleVersionFlag(cmd *cobra.Command) error {
	showVersion, err := cmd.Flags().GetBool("version")
	if err != nil {
		return fmt.Errorf("failed to get version flag: %w", err)
	}

	if showVersion {
		fmt.Printf("MCP Gateway\n")
		fmt.Printf("Version: %s\n", Version)
		fmt.Printf("Build Time: %s\n", BuildTime)
		fmt.Printf("Git Commit: %s\n", GitCommit)

		return VersionRequestedError{}
	}

	return nil
}

func setupLogger(cmd *cobra.Command) (*zap.Logger, error) {
	logLevel, err := cmd.Flags().GetString("log-level")
	if err != nil {
		return nil, fmt.Errorf("failed to get log-level flag: %w", err)
	}

	logger, err := initLogger(logLevel)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize logger: %w", err)
	}

	return logger, nil
}

func syncLogger(logger *zap.Logger) {
	if syncErr := logger.Sync(); syncErr != nil {
		// Ignore "sync /dev/stderr: invalid argument" error in containers
		// This is a known issue with zap logger
		if syncErr.Error() != "sync /dev/stderr: invalid argument" &&
			syncErr.Error() != "sync /dev/stdout: invalid argument" {
			fmt.Fprintf(os.Stderr, "Failed to sync logger: %v\n", syncErr)
		}
	}
}

func loadConfig(cmd *cobra.Command) (*config.Config, error) {
	configPath, err := cmd.Flags().GetString("config")
	if err != nil {
		return nil, fmt.Errorf("failed to get config flag: %w", err)
	}

	cfg, err := server.LoadConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load configuration: %w", err)
	}

	return cfg, nil
}

func initializeComponents(ctx context.Context, cfg *config.Config, logger *zap.Logger) (*Components, error) {
	logger.Info("Initializing gateway components")

	// Initialize metrics
	metricsRegistry := metrics.InitializeMetricsRegistry()

	// Session manager
	sessionMgr, err := session.CreateSessionStorageManager(ctx, cfg.Sessions, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create session manager: %w", err)
	}

	// Auth provider
	authProvider, err := auth.InitializeAuthenticationProvider(cfg.Auth, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create auth provider: %w", err)
	}

	// Service discovery
	serviceDiscovery, err := discovery.CreateServiceDiscoveryProvider(cfg.Discovery, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create service discovery: %w", err)
	}

	// Start service discovery
	if err := serviceDiscovery.Start(ctx); err != nil {
		return nil, fmt.Errorf("failed to start service discovery: %w", err)
	}

	// Request router
	requestRouter := router.InitializeRequestRouter(ctx, cfg.Routing, serviceDiscovery, metricsRegistry, logger)

	// Health checker
	healthChecker := health.CreateHealthMonitor(serviceDiscovery, logger)

	// Enable TCP health checks if configured
	if cfg.Server.Protocol == "tcp" || cfg.Server.Protocol == "both" {
		tcpPort := cfg.Server.TCPPort
		if tcpPort == 0 {
			tcpPort = cfg.Server.Port + portOffset
		}

		healthChecker.EnableTCP(tcpPort)
	}

	// Create rate limiter
	rateLimiter := createRateLimiter(cfg, sessionMgr, logger)

	return &Components{
		SessionMgr:       sessionMgr,
		AuthProvider:     authProvider,
		ServiceDiscovery: serviceDiscovery,
		RequestRouter:    requestRouter,
		HealthChecker:    healthChecker,
		RateLimiter:      rateLimiter,
		MetricsRegistry:  metricsRegistry,
	}, nil
}

//nolint:ireturn // Factory pattern requires interface return
func createRateLimiter(cfg *config.Config, sessionMgr session.Manager, logger *zap.Logger) ratelimit.RateLimiter {
	if cfg.Sessions.Provider == "redis" {
		// Use Redis rate limiter with circuit breaker for production resilience
		logger.Info("Using Redis rate limiter with circuit breaker protection")

		return ratelimit.CreateFaultTolerantRedisRateLimiter(sessionMgr.RedisClient(), logger)
	}

	// Use in-memory rate limiter for memory session provider
	logger.Info("Using in-memory rate limiter")

	return ratelimit.CreateLocalMemoryRateLimiter(logger)
}

func startServers(cfg *config.Config, components *Components, logger *zap.Logger) (*Servers, <-chan error, error) {
	// Create gateway server
	gatewayServer := server.BootstrapGatewayServer(
		cfg,
		components.AuthProvider,
		components.SessionMgr,
		components.RequestRouter,
		components.HealthChecker,
		components.MetricsRegistry,
		components.RateLimiter,
		logger,
	)

	// Start metrics server
	metricsServer := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Server.MetricsPort),
		Handler:           promhttp.Handler(),
		ReadHeaderTimeout: defaultTimeoutSeconds * time.Second, // G112: Prevent Slowloris attacks
	}

	go func() {
		logger.Info("Starting metrics server", zap.Int("port", cfg.Server.MetricsPort))

		if err := metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("Metrics server error", zap.Error(err))
		}
	}()

	// Start health endpoints
	healthServer := health.CreateHealthCheckServer(components.HealthChecker, cfg.Server.HealthPort)

	go func() {
		if err := healthServer.Start(); err != nil {
			logger.Error("Health server error", zap.Error(err))
		}
	}()

	// Start gateway server
	serverErrChan := make(chan error, 1)

	go func() {
		logger.Info("Starting MCP Gateway",
			zap.String("version", Version),
			zap.Int("port", cfg.Server.Port),
			zap.Int("max_connections", cfg.Server.MaxConnections),
		)

		serverErrChan <- gatewayServer.Start()
	}()

	return &Servers{
		Gateway: gatewayServer,
		Metrics: metricsServer,
		Health:  healthServer,
	}, serverErrChan, nil
}

func waitForShutdownAndCleanup(
	ctx context.Context,
	cancel context.CancelFunc,
	servers *Servers,
	components *Components,
	serverErrChan <-chan error,
	logger *zap.Logger,
) error {
	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Wait for shutdown signal or server error
	select {
	case <-sigChan:
		logger.Info("Received shutdown signal")
	case err := <-serverErrChan:
		if err != nil {
			logger.Error("Gateway server error", zap.Error(err))

			return err
		}
	}

	return performGracefulShutdown(ctx, cancel, servers, components, logger)
}

func performGracefulShutdown(
	ctx context.Context,
	cancel context.CancelFunc,
	servers *Servers,
	components *Components,
	logger *zap.Logger,
) error {
	logger.Info("Starting graceful shutdown")

	shutdownCtx, shutdownCancel := context.WithTimeout(ctx, defaultTimeoutSeconds*time.Second)
	defer shutdownCancel()

	// Stop accepting new connections
	if err := servers.Gateway.Shutdown(shutdownCtx); err != nil {
		logger.Error("Error shutting down gateway server", zap.Error(err))
	}

	// Shutdown metrics server
	if err := servers.Metrics.Shutdown(shutdownCtx); err != nil {
		logger.Error("Error shutting down metrics server", zap.Error(err))
	}

	// Stop health server
	servers.Health.Stop()

	// Cancel main context to stop all components
	cancel()

	// Wait for all components to stop
	components.ServiceDiscovery.Stop()

	if err := components.SessionMgr.Close(); err != nil {
		logger.Error("Error closing session manager", zap.Error(err))
	}

	logger.Info("MCP Gateway shutdown complete")

	return nil
}

func initLogger(level string) (*zap.Logger, error) {
	// Parse log level
	var zapLevel zapcore.Level
	if err := zapLevel.UnmarshalText([]byte(level)); err != nil {
		return nil, fmt.Errorf("invalid log level: %w", err)
	}

	// Create logger configuration
	config := zap.Config{
		Level:            zap.NewAtomicLevelAt(zapLevel),
		Development:      false,
		Encoding:         "json",
		EncoderConfig:    zap.NewProductionEncoderConfig(),
		OutputPaths:      []string{"stderr"},
		ErrorOutputPaths: []string{"stderr"},
	}

	// Customize encoder config
	config.EncoderConfig.TimeKey = "timestamp"
	config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	config.EncoderConfig.StacktraceKey = ""

	// Add service field
	logger, err := config.Build()
	if err != nil {
		return nil, err
	}

	return logger.With(zap.String("service", "mcp-gateway")), nil
}
