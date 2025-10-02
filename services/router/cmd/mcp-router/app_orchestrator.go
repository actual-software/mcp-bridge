package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"

	common "github.com/actual-software/mcp-bridge/pkg/common/config"
	"github.com/actual-software/mcp-bridge/services/router/internal/config"
	"github.com/actual-software/mcp-bridge/services/router/internal/metrics"
	"github.com/actual-software/mcp-bridge/services/router/internal/router"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

// ApplicationOrchestrator manages the application lifecycle with proper separation of concerns.
type ApplicationOrchestrator struct {
	cmd       *cobra.Command
	cfg       *config.Config
	logger    *zap.Logger
	router    *router.LocalRouter
	ctx       context.Context
	cancel    context.CancelFunc
	metricsWg sync.WaitGroup
}

// InitializeApplicationOrchestrator creates a new application orchestrator.
func InitializeApplicationOrchestrator(cmd *cobra.Command) *ApplicationOrchestrator {
	return &ApplicationOrchestrator{
		cmd: cmd,
	}
}

// ExecuteApplication runs the complete application lifecycle.
func (o *ApplicationOrchestrator) ExecuteApplication(args []string) error {
	// Handle version display.
	if shouldShowVersion, err := o.handleVersionDisplay(); shouldShowVersion || err != nil {
		return err
	}

	// Initialize configuration.
	if err := o.initializeConfiguration(); err != nil {
		return err
	}

	// Initialize logging.
	if err := o.initializeLogging(); err != nil {
		return err
	}

	defer func() {
		if err := o.logger.Sync(); err != nil {
			// Logger sync errors are typically not critical at shutdown.
			fmt.Fprintf(os.Stderr, "Failed to sync logger: %v\n", err)
		}
	}()

	// Setup application context and signal handling.
	o.setupApplicationContext()
	defer o.cancel()

	// Create and configure router.
	if err := o.createRouter(); err != nil {
		return err
	}

	// Start metrics collection if enabled.
	if err := o.startMetricsCollection(); err != nil {
		return err
	}

	// Run the application.
	return o.runApplication()
}

// handleVersionDisplay checks and handles version flag.
func (o *ApplicationOrchestrator) handleVersionDisplay() (bool, error) {
	showVersion, err := o.cmd.Flags().GetBool("version")
	if err != nil {
		return false, fmt.Errorf("failed to get version flag: %w", err)
	}

	if showVersion {
		displayVersionInformation()

		return true, nil
	}

	return false, nil
}

// displayVersionInformation prints version details.
func displayVersionInformation() {
	fmt.Printf("MCP Router\n")
	fmt.Printf("Version: %s\n", Version)
	fmt.Printf("Build Time: %s\n", BuildTime)
	fmt.Printf("Git Commit: %s\n", GitCommit)
}

// initializeConfiguration loads the application configuration.
func (o *ApplicationOrchestrator) initializeConfiguration() error {
	configPath, err := o.cmd.Flags().GetString("config")
	if err != nil {
		return fmt.Errorf("failed to get config flag: %w", err)
	}

	o.cfg, err = config.Load(configPath)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	return nil
}

// initializeLogging sets up the logger with proper precedence.
func (o *ApplicationOrchestrator) initializeLogging() error {
	logConfig, err := o.determineLoggingConfiguration()
	if err != nil {
		return err
	}

	o.logger, err = createApplicationLogger(logConfig.level, logConfig.quiet, &o.cfg.Logging)
	if err != nil {
		return fmt.Errorf("failed to initialize logger: %w", err)
	}

	return nil
}

// createApplicationLogger creates a logger with the specified configuration.
func createApplicationLogger(level string, quiet bool, loggingConfig *common.LoggingConfig) (*zap.Logger, error) {
	// Delegate to the existing initLogger function in main.go
	return initLogger(level, quiet, loggingConfig)
}

// LoggingConfiguration holds logging setup parameters.
type LoggingConfiguration struct {
	level string
	quiet bool
}

// determineLoggingConfiguration determines the logging configuration to use.
func (o *ApplicationOrchestrator) determineLoggingConfiguration() (*LoggingConfiguration, error) {
	quiet, err := o.cmd.Flags().GetBool("quiet")
	if err != nil {
		return nil, fmt.Errorf("failed to get quiet flag: %w", err)
	}

	logLevel, err := o.cmd.Flags().GetString("log-level")
	if err != nil {
		return nil, fmt.Errorf("failed to get log-level flag: %w", err)
	}

	// Use config file log level if CLI flag not explicitly changed.
	if !o.cmd.Flags().Changed("log-level") {
		logLevel = o.cfg.Logging.Level
	}

	return &LoggingConfiguration{
		level: logLevel,
		quiet: quiet,
	}, nil
}

// setupApplicationContext creates context and signal handling.
func (o *ApplicationOrchestrator) setupApplicationContext() {
	o.ctx, o.cancel = context.WithCancel(context.Background())

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go o.handleShutdownSignals(sigChan)
}

// handleShutdownSignals processes shutdown signals.
func (o *ApplicationOrchestrator) handleShutdownSignals(sigChan chan os.Signal) {
	<-sigChan
	o.logger.Info("Received shutdown signal")
	o.cancel()
}

// createRouter initializes the router component.
func (o *ApplicationOrchestrator) createRouter() error {
	var err error

	o.router, err = router.New(o.cfg, o.logger)
	if err != nil {
		return fmt.Errorf("failed to create router: %w", err)
	}

	return nil
}

// startMetricsCollection initializes and starts metrics collection.
func (o *ApplicationOrchestrator) startMetricsCollection() error {
	if !o.shouldEnableMetrics() {
		return nil
	}

	metricsManager := o.createMetricsManager()
	o.configureMetricsCollection(metricsManager)
	o.launchMetricsServer(metricsManager)

	o.logger.Info("Metrics server started",
		zap.String("endpoint", o.cfg.Metrics.Endpoint))

	return nil
}

// shouldEnableMetrics checks if metrics should be enabled.
func (o *ApplicationOrchestrator) shouldEnableMetrics() bool {
	return o.cfg.Metrics.Enabled && o.cfg.Metrics.Endpoint != ""
}

// createMetricsManager creates a new metrics exporter.
func (o *ApplicationOrchestrator) createMetricsManager() *metrics.PrometheusExporter {
	return metrics.NewPrometheusExporter(o.cfg.Metrics.Endpoint, o.logger)
}

// configureMetricsCollection sets up metrics collection callbacks.
func (o *ApplicationOrchestrator) configureMetricsCollection(exporter *metrics.PrometheusExporter) {
	exporter.SetRouterMetrics(o.collectRouterMetrics)
}

// collectRouterMetrics collects current router metrics.
func (o *ApplicationOrchestrator) collectRouterMetrics() *metrics.RouterMetrics {
	// Get metrics from router and transform to the expected format.
	metrics := o.router.GetMetrics()
	if metrics == nil {
		o.logger.Debug("No metrics available")
	}

	// The actual router metrics structure will be adapted to RouterMetrics format.
	// For now, return nil since metrics may not be available.
	return nil
}

// launchMetricsServer starts the metrics server in a goroutine.
func (o *ApplicationOrchestrator) launchMetricsServer(exporter *metrics.PrometheusExporter) {
	o.metricsWg.Add(1)

	go func() {
		defer o.metricsWg.Done()

		if err := exporter.Start(o.ctx); err != nil {
			o.logger.Error("Metrics server error", zap.Error(err))
		}
	}()
}

// runApplication runs the main application logic.
func (o *ApplicationOrchestrator) runApplication() error {
	// Log startup information.
	o.logStartupInformation()

	// Run the router.
	if err := o.router.Run(o.ctx); err != nil {
		return fmt.Errorf("router error: %w", err)
	}

	// Wait for metrics server to shutdown.
	o.metricsWg.Wait()

	o.logger.Info("MCP Router shutdown complete")

	return nil
}

// logStartupInformation logs router startup details.
func (o *ApplicationOrchestrator) logStartupInformation() {
	gatewayInfo := o.determineGatewayInfo()

	o.logger.Info("Starting MCP Router",
		zap.String("version", Version),
		zap.String("gateway", gatewayInfo),
	)
}

// determineGatewayInfo creates gateway information string.
func (o *ApplicationOrchestrator) determineGatewayInfo() string {
	endpoints := o.cfg.GetGatewayEndpoints()

	if len(endpoints) == 0 {
		return "no endpoints configured"
	}

	if len(endpoints) == 1 {
		return endpoints[0].URL
	}

	return fmt.Sprintf("%d endpoints configured", len(endpoints))
}
