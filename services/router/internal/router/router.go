// Package router implements a local MCP (Model Context Protocol) router that bridges.
// stdin/stdout communication with WebSocket gateway connections.
//
// The router acts as a bridge between MCP clients that communicate via stdin/stdout.
// and MCP servers accessible through WebSocket gateways. It provides:
//   - Automatic connection management with exponential backoff retry
//   - Request/response correlation for proper message routing
//   - Rate limiting and connection state management
//   - Comprehensive error handling and logging
//   - Graceful shutdown coordination
//
// Usage Example:
//
//	cfg := &config.Config{
//	    Gateway: config.GatewayConfig{
//	        URL: "ws://localhost:defaultHTTPPort/mcp",
//	    },
//	}
//
//	router, err := New(cfg, logger)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	ctx := context.Background()
//	if err := router.Run(ctx); err != nil {
//	    log.Fatal(err)
//	}
package router

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/poiley/mcp-bridge/services/router/internal/config"
	"github.com/poiley/mcp-bridge/services/router/internal/direct"
	"github.com/poiley/mcp-bridge/services/router/internal/gateway"
	"github.com/poiley/mcp-bridge/services/router/internal/ratelimit"
	"github.com/poiley/mcp-bridge/services/router/internal/stdio"
)

// Error definitions for proper error handling instead of string comparison.
var (
	// ErrReceiveTimeout indicates a timeout while receiving WebSocket messages.
	ErrReceiveTimeout = errors.New("receive timeout")

	// ErrContextCanceled indicates the operation was canceled via context.
	ErrContextCanceled = errors.New("context canceled")

	// ErrNotConnected indicates the router is not connected to the gateway.
	ErrNotConnected = errors.New("not connected to gateway")

	// ErrRateLimitExceeded indicates the request was rate limited.
	ErrRateLimitExceeded = errors.New("rate limit exceeded")
)

const (
	// DefaultChannelBufferSize defines the buffer size for stdin/stdout channels.
	// Large enough to prevent blocking during burst traffic while maintaining memory efficiency.
	DefaultChannelBufferSize = 100

	// MCPInternalErrorCode represents JSON-RPC 2.0 internal error code.
	// As defined in JSON-RPC 2.0 specification: https://www.jsonrpc.org/specification
	MCPInternalErrorCode = -32603

	// DefaultReceiveTimeout is the maximum time to wait for WebSocket responses.
	// Balances responsiveness with network latency tolerance.
	DefaultReceiveTimeout = 5 * time.Second

	// ConnectionStateCheckInterval defines how frequently to check connection state.
	// Short enough for quick failure detection, long enough to avoid CPU overhead.
	ConnectionStateCheckInterval = 100 * time.Millisecond

	// ShortStateCheckInterval is used for rapid state checks during critical operations.
	ShortStateCheckInterval = 10 * time.Millisecond

	// ShutdownTimeout is the maximum time to wait for goroutines during shutdown.
	// Prevents indefinite blocking while allowing graceful cleanup.
	ShutdownTimeout = 5 * time.Second

	// MCPProtocolVersion defines the MCP protocol version this router supports.
	MCPProtocolVersion = "1.0"

	// MCPRouterClientName is the client identifier sent during MCP initialization.
	MCPRouterClientName = "mcp-local-router"

	// MCPRouterClientVersion is the version sent during MCP initialization.
	MCPRouterClientVersion = "1.0.0"

	// InitializationRequestID is the fixed ID used for the MCP initialization request.
	InitializationRequestID = "init-1"
)

// ConnectionState represents the current state of the WebSocket connection to the gateway.
// The state machine progresses through these states during connection lifecycle.
type ConnectionState int

const (
	// StateInit indicates the router has been created but not yet started.
	StateInit ConnectionState = iota

	// StateConnecting indicates an active connection attempt is in progress.
	StateConnecting

	// StateConnected indicates a successful connection to the gateway.
	StateConnected

	// StateReconnecting indicates the connection was lost and attempting to reconnect.
	StateReconnecting

	// StateError indicates a connection error occurred that prevents reconnection.
	StateError

	// StateShutdown indicates the router is shutting down gracefully.
	StateShutdown
)

// LocalRouter implements a local MCP (Model Context Protocol) router that bridges.
// stdin/stdout communication with WebSocket gateway connections.
//
// Architecture Overview:
//
// The router is now composed of focused components:
// - ConnectionManager: Handles WebSocket connection lifecycle with reconnection
// - MessageRouter: Manages request/response correlation and routing
// - MetricsCollector: Collects and reports operational metrics
// - stdio.Handler: Manages stdin/stdout I/O (when not in testing mode)
//
// This separation of concerns improves maintainability and testability while.
// preserving all existing functionality and thread safety guarantees.
//
// Thread Safety:
//
// All public methods are safe for concurrent use. Each component manages its
// own synchronization and the router coordinates their lifecycle.
type LocalRouter struct {
	config *config.Config
	logger *zap.Logger

	// Core components.
	stdioHandler  *stdio.Handler
	gwClient      gateway.GatewayClient
	directManager direct.DirectClientManagerInterface
	rateLimiter   ratelimit.RateLimiter

	// Focused responsibility components.
	connMgr    *ConnectionManager
	msgRouter  *MessageRouter
	metricsCol *MetricsCollector

	// Channels for bidirectional communication.
	stdinChan  chan []byte
	stdoutChan chan []byte

	// Shutdown coordination.
	ctx    context.Context
	cancel context.CancelFunc

	// Testing.
	skipStdio bool // Skip real stdio handling for tests
}

type Metrics struct {
	// Atomic counters - use atomic.AddUint64 to modify
	RequestsTotal     uint64
	ResponsesTotal    uint64
	ErrorsTotal       uint64
	ConnectionRetries uint64
	DirectRequests    uint64 // Requests routed directly
	GatewayRequests   uint64 // Requests routed through gateway
	FallbackSuccesses uint64 // Successful fallbacks from direct to gateway
	FallbackFailures  uint64 // Failed fallbacks (both direct and gateway failed)

	// Additional metrics for Prometheus.
	RequestDuration   map[string][]time.Duration
	ResponseSizes     []int
	ResponseSizesMu   sync.Mutex
	ActiveConnections int32 // Use atomic.AddInt32 to modify
}

// New creates a new LocalRouter instance for production use.
func New(cfg *config.Config, logger *zap.Logger) (*LocalRouter, error) {
	if cfg == nil {
		return nil, errors.New("config is required")
	}

	if logger == nil {
		return nil, errors.New("logger is required")
	}

	r := &LocalRouter{
		config:     cfg,
		logger:     logger,
		stdinChan:  make(chan []byte, DefaultChannelBufferSize),
		stdoutChan: make(chan []byte, DefaultChannelBufferSize),
		skipStdio:  false, // Enable stdio for production
	}

	// Initialize components.
	if err := r.initializeComponents(); err != nil {
		return nil, fmt.Errorf("failed to initialize components: %w", err)
	}

	return r, nil
}

// initializeComponents sets up the router's internal components.
func (r *LocalRouter) initializeComponents() error {
	// Create context for lifecycle management.
	r.ctx, r.cancel = context.WithCancel(context.Background())

	// Create stdio handler for production.
	if !r.skipStdio {
		r.stdioHandler = stdio.NewHandler(r.logger, r.stdinChan, r.stdoutChan)
	}

	// Create gateway client (with pool support).
	gwClient, err := gateway.NewGatewayClientWithPool(r.config, r.logger)
	if err != nil {
		return fmt.Errorf("failed to create gateway client: %w", err)
	}

	r.gwClient = gwClient

	// Create direct client manager if direct mode is enabled.
	if r.config.IsDirectMode() {
		directConfig := r.config.GetDirectConfig()
		r.directManager = direct.NewDirectClientManager(directConfig, r.logger)
		r.logger.Info("Direct client manager enabled",
			zap.Int("max_connections", directConfig.MaxConnections),
			zap.Bool("auto_detection", directConfig.AutoDetection.Enabled),
		)
	}

	// Create rate limiter.
	if r.config.Local.RateLimit.Enabled {
		r.rateLimiter = ratelimit.NewRateLimiter(
			r.config.Local.RateLimit.RequestsPerSec,
			r.config.Local.RateLimit.Burst,
			r.logger,
		)
		r.logger.Info("Rate limiting enabled",
			zap.Float64("requests_per_sec", r.config.Local.RateLimit.RequestsPerSec),
			zap.Int("burst", r.config.Local.RateLimit.Burst),
		)
	} else {
		r.rateLimiter = &ratelimit.NoOpLimiter{}
	}

	// Create focused components.
	r.metricsCol = NewMetricsCollector(r.logger)
	r.connMgr = NewConnectionManager(r.config, r.logger, r.gwClient)
	r.msgRouter = NewMessageRouter(
		r.config,
		r.logger,
		r.gwClient,
		r.directManager,
		r.rateLimiter,
		r.stdinChan,
		r.stdoutChan,
		r.connMgr,
		r.metricsCol,
	)

	return nil
}

// - Timeout errors are logged but don't trigger reconnection.
func (r *LocalRouter) Run(ctx context.Context) error {
	r.logger.Info("Starting MCP Local Router")

	// Merge contexts.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	r.ctx = ctx
	r.cancel = cancel

	// Start focused components.
	r.connMgr.Start() 
	r.msgRouter.Start() 

	// Start direct client manager if enabled.
	if r.directManager != nil {
		if err := r.directManager.Start(ctx); err != nil {
			return fmt.Errorf("failed to start direct client manager: %w", err)
		}

		r.logger.Info("Direct client manager started")
	}

	// Handle stdio I/O (manages its own WaitGroup) - skip for testing.
	if !r.skipStdio && r.stdioHandler != nil {
		var wg sync.WaitGroup

		r.stdioHandler.Run(ctx, &wg)

		// Wait for stdio handler to complete.
		go func() {
			wg.Wait()
		}()
	}

	// DISABLED: Router should not auto-initialize when acting as stdio bridge.
	// The client controls when to send initialization.
	// go r.handleInitialization()

	// Signal readiness for Kubernetes readiness probe.
	if err := r.signalReady(); err != nil {
		r.logger.Warn("Failed to create readiness signal", zap.Error(err))
	} else {
		r.logger.Info("Router is ready to accept connections")
	}

	// Wait for context cancellation.
	<-ctx.Done()

	// Stop components gracefully.
	r.connMgr.Stop()
	r.msgRouter.Stop()

	// Stop direct client manager if enabled.
	if r.directManager != nil {
		if err := r.directManager.Stop(ctx); err != nil {
			r.logger.Warn("Failed to stop direct client manager", zap.Error(err))
		}
	}

	// Clean up readiness signal.
	r.cleanupReady()

	// Log shutdown completion.
	r.logger.Info("MCP Local Router shutdown complete")

	return nil
}

// handleInitialization waits for connection and sends MCP initialization.
// DISABLED: When acting as a stdio bridge, the client controls initialization.
// func (r *LocalRouter) handleInitialization() {
// 	// Wait for connection
// 	for {
// 		select {
// 		case <-r.ctx.Done():
// 			return
// 		case <-time.After(ConnectionStateCheckInterval):
// 			if r.connMgr.GetState() == StateConnected {
// 				// Send initialization
// 				r.logger.Info("[ROUTER DEBUG] Router sending its own initialization request with ID 'init-1'")
// 				if err := r.msgRouter.SendInitialization(); err != nil {
// 					r.logger.Error("Failed to send initialization", zap.Error(err))
// 				}
// 				return
// 			}
// 		}
// 	}
// }

// GetState returns the current connection state.
func (r *LocalRouter) GetState() ConnectionState {
	return r.connMgr.GetState()
}

// GetMetrics returns current metrics.
func (r *LocalRouter) GetMetrics() *Metrics {
	return r.metricsCol.GetMetrics()
}

// Shutdown gracefully shuts down the router.
func (r *LocalRouter) Shutdown() {
	r.logger.Info("Shutting down router")
	r.cancel()
}

// String returns the string representation of the connection state.
func (s ConnectionState) String() string {
	switch s {
	case StateInit:
		return "INIT"
	case StateConnecting:
		return "CONNECTING"
	case StateConnected:
		return "CONNECTED"
	case StateReconnecting:
		return "RECONNECTING"
	case StateError:
		return "ERROR"
	case StateShutdown:
		return "SHUTDOWN"
	default:
		return "UNKNOWN"
	}
}

// signalReady creates the readiness signal file for Kubernetes readiness probe.
func (r *LocalRouter) signalReady() error {
	readyFile := "/tmp/ready"

	// Create the readiness signal file.
	file, err := os.Create(readyFile)
	if err != nil {
		return fmt.Errorf("failed to create readiness file: %w", err)
	}

	defer func() {
		if err := file.Close(); err != nil {
			r.logger.Warn("Failed to close file", zap.Error(err))
		}
	}() 

	// Write timestamp to show when router became ready.
	if _, err := fmt.Fprintf(file, "ready at %s\n", time.Now().Format(time.RFC3339)); err != nil {
		return fmt.Errorf("failed to write to readiness file: %w", err)
	}

	return nil
}

// cleanupReady removes the readiness signal file during shutdown.
func (r *LocalRouter) cleanupReady() {
	readyFile := "/tmp/ready"

	if err := os.Remove(readyFile); err != nil {
		// Log but don't fail on cleanup errors.
		r.logger.Debug("Failed to remove readiness file", zap.Error(err))
	}
}
