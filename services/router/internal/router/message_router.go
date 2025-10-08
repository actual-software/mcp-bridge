package router

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/actual-software/mcp-bridge/services/router/internal/config"
	"github.com/actual-software/mcp-bridge/services/router/internal/direct"
	"github.com/actual-software/mcp-bridge/services/router/internal/gateway"
	"github.com/actual-software/mcp-bridge/services/router/internal/ratelimit"
	"github.com/actual-software/mcp-bridge/services/router/pkg/mcp"
)

// MessageRouter handles request/response correlation and routing between stdin/stdout and WebSocket.
type MessageRouter struct {
	config        *config.Config
	logger        *zap.Logger
	gwClient      gateway.GatewayClient
	directManager direct.DirectClientManagerInterface
	rateLimiter   ratelimit.RateLimiter

	// Channels for bidirectional communication.
	stdinChan  chan []byte
	stdoutChan chan []byte

	// Request tracking for correlation.
	pendingReqs sync.Map // map[string]chan *mcp.Response

	// Request queue for handling requests before connection.
	requestQueue *RequestQueue

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Dependencies.
	connMgr    *ConnectionManager
	metricsCol *MetricsCollector
}

// NewMessageRouter creates a new message router.
func NewMessageRouter(
	cfg *config.Config,
	logger *zap.Logger,
	gwClient gateway.GatewayClient,
	directManager direct.DirectClientManagerInterface,
	rateLimiter ratelimit.RateLimiter,
	stdinChan, stdoutChan chan []byte,
	connMgr *ConnectionManager,
	metricsCol *MetricsCollector,
) *MessageRouter {
	ctx, cancel := context.WithCancel(context.Background())

	// Create request queue with configurable size (default 100).
	queueSize := 100
	if cfg != nil && cfg.Local.MaxQueuedRequests > 0 {
		queueSize = cfg.Local.MaxQueuedRequests
	}

	return &MessageRouter{
		config:        cfg,
		logger:        logger,
		gwClient:      gwClient,
		directManager: directManager,
		rateLimiter:   rateLimiter,
		stdinChan:     stdinChan,
		stdoutChan:    stdoutChan,
		requestQueue:  NewRequestQueue(queueSize, logger),
		ctx:           ctx,
		cancel:        cancel,
		connMgr:       connMgr,
		metricsCol:    metricsCol,
	}
}

// Start begins message routing between stdin/stdout and WebSocket.
func (mr *MessageRouter) Start(ctx context.Context) {
	mr.wg.Add(DefaultRetryCountLimited)

	//nolint:contextcheck // Background goroutines use router's internal context mr.ctx for lifecycle management
	go mr.handleStdinToWS()
	//nolint:contextcheck // Background goroutines use router's internal context mr.ctx for lifecycle management
	go mr.handleWSToStdout()
	go mr.handleConnectionStateChanges()
}

// Stop gracefully stops the message router.
func (mr *MessageRouter) Stop() {
	mr.logger.Info("Stopping message router")
	mr.cancel()

	// Clean up any remaining pending requests to prevent goroutine leaks.
	// Use LoadAndDelete to safely remove and close channels one by one.
	var channelsToClose []chan *mcp.Response

	mr.pendingReqs.Range(func(key, value interface{}) bool {
		if ch, ok := value.(chan *mcp.Response); ok {
			// Use LoadAndDelete to atomically remove from map if still present.
			if _, loaded := mr.pendingReqs.LoadAndDelete(key); loaded {
				channelsToClose = append(channelsToClose, ch)
			}
		}

		return true // Continue iteration
	})

	// Close channels outside of the Range iteration to avoid issues.
	for _, ch := range channelsToClose {
		close(ch)
	}

	mr.wg.Wait()
}

// handleConnectionStateChanges monitors connection state and processes queued requests.
func (mr *MessageRouter) handleConnectionStateChanges() {
	defer mr.wg.Done()

	// Track previous state to detect transitions.
	var lastState = StateInit

	// Get state change notification channel.
	stateChangeChan := mr.connMgr.GetStateChangeChan()

	for {
		select {
		case <-mr.ctx.Done():
			// Clear queue on shutdown.
			mr.requestQueue.Clear(errors.New("router shutting down"))

			return

		case newState, ok := <-stateChangeChan:
			if !ok {
				// Channel closed, we're shutting down.
				return
			}

			// Detect transition to connected state.
			if lastState != StateConnected && newState == StateConnected {
				mr.logger.Info("Connection established, processing queued requests")
				mr.processQueuedRequests()
			}

			// Detect transition from connected state.
			if lastState == StateConnected && newState != StateConnected {
				mr.logger.Warn("Connection lost, new requests will be queued")
			}

			lastState = newState
		}
	}
}

// processQueuedRequests processes all requests in the queue.
func (mr *MessageRouter) processQueuedRequests() {
	requests := mr.requestQueue.DequeueAll()
	if len(requests) == 0 {
		return
	}

	mr.logger.Info("Processing queued requests", zap.Int("count", len(requests)))

	for _, qr := range requests {
		// Check if request context is still valid.
		if qr.Context.Err() != nil {
			qr.Response <- errors.New("request context canceled")

			close(qr.Response)

			continue
		}

		// Process the request.
		err := mr.processStdinMessageDirect(mr.ctx, qr.Data)

		// Send result back to waiting goroutine.
		select {
		case qr.Response <- err:
		default:
		}

		close(qr.Response)
	}
}

// handleStdinToWS processes messages from stdin and forwards to WebSocket.
func (mr *MessageRouter) handleStdinToWS() {
	defer mr.wg.Done()

	for {
		select {
		case <-mr.ctx.Done():
			return
		case data := <-mr.stdinChan:
			if err := mr.processStdinMessage(data); err != nil {
				mr.logger.Error("Failed to process stdin message",
					zap.Error(err),
					zap.ByteString("data", data),
				)
				// Extract request ID for proper error response correlation.
				requestID := mr.extractRequestID(data)
				mr.sendErrorResponse(requestID, err)
			}
		}
	}
}

// handleWSToStdout processes messages from WebSocket and forwards to stdout.
func (mr *MessageRouter) handleWSToStdout() {
	defer mr.wg.Done()

	for {
		select {
		case <-mr.ctx.Done():
			return
		default:
			if !mr.isConnectionReady() {
				continue
			}

			mr.processWSMessage()
		}
	}
}

// isConnectionReady checks if connection is ready and handles waiting if not.
func (mr *MessageRouter) isConnectionReady() bool {
	if mr.connMgr.GetState() != StateConnected {
		// Check context again before sleeping.
		select {
		case <-mr.ctx.Done():
			return false
		case <-time.After(ConnectionStateCheckInterval):
			return false
		}
	}

	return true
}

// processWSMessage handles receiving and routing a single WebSocket message.
func (mr *MessageRouter) processWSMessage() {
	// Use a shorter timeout and check context more frequently.
	resp, err := mr.receiveResponseWithTimeout(DefaultReceiveTimeout)
	if err != nil {
		mr.handleWSReceiveError(err)

		return
	}

	// Only route response if no error occurred and response is not nil.
	if resp != nil {
		mr.routeResponse(resp)
	}
}

// handleWSReceiveError processes errors from WebSocket message reception.
func (mr *MessageRouter) handleWSReceiveError(err error) {
	// Check context first.
	select {
	case <-mr.ctx.Done():
		return
	default:
	}

	// Check if connection is still valid.
	if !mr.gwClient.IsConnected() {
		mr.handleConnectionLoss()

		return
	}

	// For other errors, continue without changing state.
	if !isTimeoutError(err) {
		mr.logger.Debug("Receive error, continuing", zap.Error(err))
	}

	select {
	case <-mr.ctx.Done():
		return
	case <-time.After(ShortStateCheckInterval):
		return
	}
}

// handleConnectionLoss handles when the WebSocket connection is lost.
func (mr *MessageRouter) handleConnectionLoss() {
	mr.logger.Info("Connection lost, waiting for reconnection")
	mr.connMgr.setState(StateReconnecting)

	select {
	case <-mr.ctx.Done():
		return
	case <-time.After(ConnectionStateCheckInterval):
		return
	}
}

// processStdinMessage handles a single MCP request message from stdin.
func (mr *MessageRouter) processStdinMessage(data []byte) error {
	// Parse MCP request first to validate it.
	var req mcp.Request
	if err := json.Unmarshal(data, &req); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}

	// Apply rate limiting.
	if err := mr.rateLimiter.Wait(mr.ctx); err != nil {
		mr.metricsCol.IncrementErrors()
		mr.logger.Warn("Request rate limited",
			zap.Any("request_id", req.ID),
			zap.String("method", req.Method),
			zap.Error(err),
		)

		return fmt.Errorf("rate limit exceeded: %w", err)
	}

	// Check connection state.
	if mr.connMgr.GetState() != StateConnected {
		// Queue the request for processing when connected.
		mr.logger.Debug("Connection not ready, queueing request",
			zap.Any("request_id", req.ID),
			zap.String("method", req.Method),
			zap.String("state", mr.connMgr.GetState().String()))

		// Enqueue the request with a timeout context.
		timeout := mr.config.GetRequestTimeout()

		ctx, cancel := context.WithTimeout(mr.ctx, timeout)
		defer cancel()

		respChan, err := mr.requestQueue.Enqueue(data, ctx)
		if err != nil {
			mr.metricsCol.IncrementErrors()

			return fmt.Errorf("failed to queue request: %w", err)
		}

		// Wait for the queued request to be processed.
		select {
		case processErr := <-respChan:
			if processErr != nil {
				mr.metricsCol.IncrementErrors()

				return processErr
			}

			return nil
		case <-ctx.Done():
			mr.metricsCol.IncrementErrors()

			return errors.New("request timeout while queued")
		}
	}

	// Connection is ready, process immediately.
	return mr.processStdinMessageDirect(mr.ctx, data)
}

// processStdinMessageDirect processes a request when already connected.
// determineRoutingTarget determines whether to use direct or gateway connection for a request.
func (mr *MessageRouter) determineRoutingTarget(req *mcp.Request) (bool, error) {
	// If direct manager is not configured, always use gateway.
	if mr.directManager == nil {
		return false, nil
	}

	// Get direct configuration for fallback settings.
	directConfig := mr.config.GetDirectConfig()

	// Check gateway-only methods.
	for _, method := range directConfig.Fallback.GatewayOnlyMethods {
		if req.Method == method {
			return false, nil
		}
	}

	// Check direct-only methods.
	for _, method := range directConfig.Fallback.DirectOnlyMethods {
		if req.Method == method {
			return true, nil
		}
	}

	// If direct mode is enabled, use enhanced routing logic.
	if mr.config.IsDirectMode() {
		// Enhanced routing based on method patterns and fallback configuration.
		switch req.Method {
		case "initialize", "initialized":
			// Meta operations - prefer gateway for centralized handling unless explicitly configured otherwise.
			return false, nil
		case "tools/list", "tools/call", "resources/list", "resources/read", "prompts/list", "prompts/get":
			// Server-specific operations - prefer direct connection with fallback.
			return true, nil
		default:
			// For unknown methods, prefer direct if fallback is enabled, otherwise gateway.
			return directConfig.Fallback.Enabled, nil
		}
	}

	// Default to gateway.
	return false, nil
}

func (mr *MessageRouter) processStdinMessageDirect(ctx context.Context, data []byte) error {
	startTime := time.Now()

	// Parse MCP request.
	var req mcp.Request
	if err := json.Unmarshal(data, &req); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}

	// Increment request counter.
	mr.metricsCol.IncrementRequests()

	// Log request with correlation ID for tracing.
	mr.logger.Debug("Processing request",
		zap.Any("request_id", req.ID),
		zap.String("method", req.Method),
	)

	// Determine routing target (direct vs gateway).
	useDirect, err := mr.determineRoutingTarget(&req)
	if err != nil {
		mr.metricsCol.IncrementErrors()
		mr.logger.Error("Failed to determine routing target",
			zap.Any("request_id", req.ID),
			zap.String("method", req.Method),
			zap.Error(err),
		)

		return fmt.Errorf("failed to determine routing target: %w", err)
	}

	if useDirect {
		// Try direct connection first, with fallback to gateway.
		return mr.processRequestWithFallback(ctx, &req, data, startTime)
	} else {
		// Route directly to gateway connection.
		return mr.processGatewayRequest(ctx, &req, data, startTime)
	}
}

// sendResponse sends a response to stdout.
// processDirectRequest processes a request via direct connection.
// attemptDirectWithRetries tries direct connection with retries.
func (mr *MessageRouter) attemptDirectWithRetries(
	ctx context.Context,
	req *mcp.Request,
	data []byte,
	startTime time.Time,
	config direct.DirectConfig,
) error {
	var lastErr error

	for attempt := 0; attempt <= config.Fallback.MaxRetries; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(config.Fallback.RetryDelay):
			case <-ctx.Done():
				return errors.New("context canceled during retry")
			}

			mr.logger.Debug("Retrying direct connection",
				zap.Any("request_id", req.ID),
				zap.String("method", req.Method),
				zap.Int("attempt", attempt+1),
			)
		}

		timeoutCtx, cancel := context.WithTimeout(ctx, config.Fallback.DirectTimeout)
		err := mr.processDirectRequestWithContext(timeoutCtx, req, data, startTime)

		cancel()

		if err == nil {
			if attempt > 0 {
				mr.logger.Info("Direct connection succeeded after retries",
					zap.Any("request_id", req.ID),
					zap.Int("attempts", attempt+1),
				)
			}

			return nil
		}

		lastErr = err

		if !mr.isRetryableDirectError(err) {
			mr.logger.Debug("Non-retryable error, skipping retries",
				zap.Any("request_id", req.ID),
				zap.Error(err),
			)

			break
		}

		mr.logger.Debug("Direct attempt failed",
			zap.Any("request_id", req.ID),
			zap.Int("attempt", attempt+1),
			zap.Error(err),
		)
	}

	return lastErr
}

// handleFallbackToGateway handles fallback to gateway after direct failure.
func (mr *MessageRouter) handleFallbackToGateway(
	ctx context.Context,
	req *mcp.Request,
	data []byte,
	startTime time.Time,
	directErr error,
	config direct.DirectConfig,
) error {
	mr.logger.Warn("Falling back to gateway",
		zap.Any("request_id", req.ID),
		zap.String("method", req.Method),
		zap.Error(directErr),
	)

	fallbackStart := time.Now()

	mr.logger.Info("Attempting gateway fallback",
		zap.Any("request_id", req.ID),
		zap.Duration("direct_duration", fallbackStart.Sub(startTime)),
	)

	err := mr.processGatewayRequest(ctx, req, data, fallbackStart)
	if err != nil {
		mr.logger.Error("Both direct and gateway failed",
			zap.Any("request_id", req.ID),
			zap.Error(directErr),
			zap.NamedError("fallback_error", err),
		)

		mr.metricsCol.IncrementFallbackFailures()

		return fmt.Errorf("direct failed (%w), gateway failed: %w", directErr, err)
	}

	mr.logger.Info("Gateway fallback succeeded",
		zap.Any("request_id", req.ID),
		zap.Duration("total_duration", time.Since(startTime)),
	)

	mr.metricsCol.IncrementFallbackSuccesses()

	return nil
}

// processRequestWithFallback attempts direct connection first, then falls back to gateway on failure.
func (mr *MessageRouter) processRequestWithFallback(
	ctx context.Context, req *mcp.Request, data []byte, startTime time.Time,
) error {
	directConfig := mr.config.GetDirectConfig()

	// Check if fallback is disabled.
	if !directConfig.Fallback.Enabled {
		return mr.processDirectRequest(ctx, req, data, startTime)
	}

	// Try direct connection with retries.
	directErr := mr.attemptDirectWithRetries(ctx, req, data, startTime, directConfig)
	if directErr == nil {
		return nil
	}

	// Direct failed, try gateway fallback.
	return mr.handleFallbackToGateway(ctx, req, data, startTime, directErr, directConfig)
}

// isRetryableDirectError determines if a direct connection error should trigger gateway fallback.
func (mr *MessageRouter) isRetryableDirectError(err error) bool {
	if err == nil {
		return false
	}

	// Convert error to string for pattern matching.
	errStr := err.Error()

	// Network-related errors that should trigger fallback.
	retryablePatterns := []string{
		"connection refused",
		"connection timeout",
		"connection reset",
		"no such host",
		"network unreachable",
		"host unreachable",
		"timeout",
		"context deadline exceeded",
		"failed to detect protocol",
		"failed to get direct client",
		"protocol detection failed",
		"no route to host",
		"connection aborted",
	}

	for _, pattern := range retryablePatterns {
		if strings.Contains(strings.ToLower(errStr), pattern) {
			return true
		}
	}

	// Also check for specific error types that indicate connectivity issues.
	// You can extend this with more sophisticated error type checking.
	return strings.Contains(errStr, "direct") ||
		strings.Contains(errStr, "connect") ||
		strings.Contains(errStr, "dial")
}

// setupDirectRequest prepares a direct request and returns cleanup function.
func (mr *MessageRouter) setupDirectRequest(req *mcp.Request) func() {
	respChan := make(chan *mcp.Response, 1)
	mr.pendingReqs.Store(req.ID, respChan)

	return func() {
		if ch, ok := mr.pendingReqs.LoadAndDelete(req.ID); ok {
			if respCh, ok := ch.(chan *mcp.Response); ok {
				close(respCh)
			}
		}
	}
}

// handleDirectError handles errors from direct connection attempts.
func (mr *MessageRouter) handleDirectError(req *mcp.Request, serverURL string, err error, msg string) error {
	mr.metricsCol.IncrementErrors()
	mr.logger.Error(msg,
		zap.Any("request_id", req.ID),
		zap.String("method", req.Method),
		zap.String("server_url", serverURL),
		zap.Error(err),
	)

	return fmt.Errorf("%s: %w", msg, err)
}

// processDirectRequestWithContext processes a request via direct connection with context.
func (mr *MessageRouter) processDirectRequestWithContext(
	ctx context.Context,
	req *mcp.Request,
	data []byte,
	startTime time.Time,
) error {
	cleanup := mr.setupDirectRequest(req)
	defer cleanup()

	mr.logger.Debug("Routing request to direct connection",
		zap.Any("request_id", req.ID),
		zap.String("method", req.Method),
	)

	mr.metricsCol.IncrementDirectRequests()
	serverURL := mr.extractServerURL(req)

	client, err := mr.directManager.GetClient(ctx, serverURL)
	if err != nil {
		return mr.handleDirectError(req, serverURL, err, "failed to get direct client")
	}

	resp, err := client.SendRequest(ctx, req)
	if err != nil {
		return mr.handleDirectError(req, serverURL, err, "failed to send direct request")
	}

	duration := time.Since(startTime)
	mr.metricsCol.RecordRequestDuration(req.Method, duration)

	// Send response and record metrics.
	if err := mr.sendResponse(resp); err != nil {
		mr.metricsCol.IncrementErrors()

		return err
	}

	mr.metricsCol.IncrementResponses()

	return nil
}

//nolint:funlen // Function is 61 lines, just over the 60 limit, but splitting would harm readability
func (mr *MessageRouter) processDirectRequest(
	ctx context.Context, req *mcp.Request, data []byte, startTime time.Time,
) error {
	// Create response channel for correlation - buffered to prevent blocking.
	respChan := make(chan *mcp.Response, 1)

	// Store the channel and ensure cleanup.
	mr.pendingReqs.Store(req.ID, respChan)

	// Cleanup function to ensure proper channel management.
	cleanup := func() {
		if ch, ok := mr.pendingReqs.LoadAndDelete(req.ID); ok {
			if respCh, ok := ch.(chan *mcp.Response); ok {
				close(respCh)
			}
		}
	}

	mr.logger.Debug("Routing request to direct connection",
		zap.Any("request_id", req.ID),
		zap.String("method", req.Method),
	)

	// Extract server URL from request parameters or use configuration defaults.
	serverURL := mr.extractServerURL(req)

	// Get or create direct client for the server.
	client, err := mr.directManager.GetClient(ctx, serverURL)
	if err != nil {
		cleanup() // Clean up on error
		mr.metricsCol.IncrementErrors()
		mr.logger.Error("Failed to get direct client",
			zap.Any("request_id", req.ID),
			zap.String("method", req.Method),
			zap.String("server_url", serverURL),
			zap.Error(err),
		)

		return fmt.Errorf("failed to get direct client: %w", err)
	}

	// Send request via direct client.
	resp, err := client.SendRequest(ctx, req)
	if err != nil {
		cleanup() // Clean up on send error
		mr.metricsCol.IncrementErrors()
		mr.logger.Error("Failed to send request via direct client",
			zap.Any("request_id", req.ID),
			zap.String("method", req.Method),
			zap.String("server_url", serverURL),
			zap.Error(err),
		)

		return fmt.Errorf("failed to send direct request: %w", err)
	}

	// Clean up the pending request (direct response received immediately).
	cleanup()

	// Record request duration.
	duration := time.Since(startTime)
	mr.metricsCol.RecordRequestDuration(req.Method, duration)

	// Send response and record metrics.
	if err := mr.sendResponse(resp); err != nil {
		mr.metricsCol.IncrementErrors()

		return err
	}

	mr.metricsCol.IncrementResponses()

	return nil
}

// processGatewayRequest processes a request via gateway connection.
//
//nolint:funlen // Function is 61 lines, just over the 60 limit, but splitting would harm readability
func (mr *MessageRouter) processGatewayRequest(
	ctx context.Context, req *mcp.Request, data []byte, startTime time.Time,
) error {
	// Create response channel for correlation - buffered to prevent blocking.
	respChan := make(chan *mcp.Response, 1)

	// Store the channel and ensure cleanup.
	mr.pendingReqs.Store(req.ID, respChan)

	// Cleanup function to ensure proper channel management.
	cleanup := func() {
		if ch, ok := mr.pendingReqs.LoadAndDelete(req.ID); ok {
			if respCh, ok := ch.(chan *mcp.Response); ok {
				close(respCh)
			}
		}
	}

	mr.logger.Debug("Routing request to gateway connection",
		zap.Any("request_id", req.ID),
		zap.String("method", req.Method),
	)

	// Track gateway request.
	mr.metricsCol.IncrementGatewayRequests()

	mr.logger.Debug("Launching async goroutine for request",
		zap.Any("request_id", req.ID),
		zap.String("method", req.Method),
	)

	// Launch goroutine to handle send + wait for response asynchronously.
	// This allows multiple concurrent in-flight requests to the gateway.
	go func() {
		mr.logger.Debug("Goroutine started, sending request to gateway",
			zap.Any("request_id", req.ID),
			zap.String("method", req.Method),
		)

		// Forward to gateway.
		if err := mr.gwClient.SendRequest(ctx, req); err != nil {
			cleanup() // Clean up on send error
			mr.metricsCol.IncrementErrors()
			mr.logger.Error("Failed to send request to gateway",
				zap.Any("request_id", req.ID),
				zap.String("method", req.Method),
				zap.Error(err),
			)

			// Send error response to stdout
			mr.sendErrorResponse(req.ID, err)

			return
		}

		// Wait for response with timeout.
		timeout := mr.config.GetRequestTimeout()

		mr.logger.Debug("Waiting for response",
			zap.Any("request_id", req.ID),
			zap.String("method", req.Method),
			zap.Duration("timeout", timeout),
		)

		var resp *mcp.Response

		select {
		case resp = <-respChan:
			// Response received - channel will be cleaned up by routeResponse.
			mr.logger.Debug("Response received in goroutine",
				zap.Any("request_id", req.ID),
				zap.String("method", req.Method),
			)

			// Record request duration.
			duration := time.Since(startTime)
			mr.metricsCol.RecordRequestDuration(req.Method, duration)

			// Send response and record metrics.
			if err := mr.sendResponse(resp); err != nil {
				mr.metricsCol.IncrementErrors()
				mr.logger.Error("Failed to send response",
					zap.Any("request_id", req.ID),
					zap.Error(err),
				)

				return
			}

			mr.metricsCol.IncrementResponses()
			mr.logger.Debug("Goroutine completed successfully",
				zap.Any("request_id", req.ID),
				zap.String("method", req.Method),
			)

		case <-time.After(timeout):
			cleanup() // Clean up on timeout
			mr.metricsCol.IncrementErrors()
			mr.logger.Error("Request timeout in goroutine",
				zap.Any("request_id", req.ID),
				zap.String("method", req.Method),
				zap.Duration("timeout", timeout),
			)

			mr.sendErrorResponse(req.ID, fmt.Errorf("request timeout after %v", timeout))

		case <-mr.ctx.Done():
			cleanup() // Clean up on context cancellation
			mr.metricsCol.IncrementErrors()
			mr.logger.Error("Context canceled during request in goroutine",
				zap.Any("request_id", req.ID),
				zap.String("method", req.Method),
			)

			mr.sendErrorResponse(req.ID, errors.New("context canceled"))
		}

		mr.logger.Debug("Goroutine exiting",
			zap.Any("request_id", req.ID),
			zap.String("method", req.Method),
		)
	}()

	// Return immediately to allow processing next request.
	return nil
}

// extractServerURL extracts server URL from request parameters or uses configuration defaults.
func (mr *MessageRouter) extractServerURL(req *mcp.Request) string {
	// First, try to extract from request parameters.
	if params, ok := req.Params.(map[string]interface{}); ok {
		if url := mr.extractURLFromParams(params, req.ID); url != "" {
			return url
		}
	}

	// Fall back to configuration-based routing.
	return mr.getFallbackServerURL(req.ID)
}

// extractURLFromParams tries to extract server URL from request parameters.
func (mr *MessageRouter) extractURLFromParams(params map[string]interface{}, requestID interface{}) string {
	// Try server_url
	if url := mr.tryExtractURL(params, "server_url", requestID); url != "" {
		return url
	}

	// Try target_url
	if url := mr.tryExtractURL(params, "target_url", requestID); url != "" {
		return url
	}

	// Try endpoint
	if url := mr.tryExtractURL(params, "endpoint", requestID); url != "" {
		return url
	}

	return ""
}

// tryExtractURL attempts to extract and validate a URL from parameters.
func (mr *MessageRouter) tryExtractURL(params map[string]interface{}, key string, requestID interface{}) string {
	if value, exists := params[key]; exists {
		if url, ok := value.(string); ok && url != "" {
			mr.logger.Debug(fmt.Sprintf("Using %s from request parameters", key),
				zap.Any("request_id", requestID),
				zap.String(key, url))

			return url
		}
	}

	return ""
}

// getFallbackServerURL returns the default fallback URL.
func (mr *MessageRouter) getFallbackServerURL(requestID interface{}) string {
	// Ultimate fallback to a default local URL.
	defaultURL := "http://localhost:8080/mcp"
	mr.logger.Debug("Using fallback server URL",
		zap.Any("request_id", requestID),
		zap.String("server_url", defaultURL))

	return defaultURL
}

func (mr *MessageRouter) sendResponse(resp *mcp.Response) error {
	data, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("failed to marshal response: %w", err)
	}

	// Record response size.
	mr.metricsCol.RecordResponseSize(len(data))

	select {
	case mr.stdoutChan <- data:
		return nil
	case <-mr.ctx.Done():
		return errors.New("context canceled")
	}
}

// sendErrorResponse sends an error response to stdout.
func (mr *MessageRouter) sendErrorResponse(id interface{}, err error) {
	resp := &mcp.Response{
		JSONRPC: "2.0",
		Error: &mcp.Error{
			Code:    MCPInternalErrorCode,
			Message: err.Error(),
		},
		ID: id,
	}

	if sendErr := mr.sendResponse(resp); sendErr != nil {
		mr.logger.Error("Failed to send error response",
			zap.Error(sendErr),
			zap.Error(err),
		)
	}

	mr.metricsCol.IncrementErrors()
}

// routeResponse routes a response to the appropriate waiting request.
func (mr *MessageRouter) routeResponse(resp *mcp.Response) {
	// Load and delete in one atomic operation to prevent race conditions.
	if chInterface, ok := mr.pendingReqs.LoadAndDelete(resp.ID); ok {
		ch, ok := chInterface.(chan *mcp.Response)
		if !ok {
			return
		}

		mr.logger.Debug("Routing response to waiting request",
			zap.Any("request_id", resp.ID),
		)

		// Use select to prevent blocking and provide context cancellation.
		select {
		case ch <- resp:
			// Response sent successfully.
		case <-mr.ctx.Done():
			// Context canceled, abandon response.
		default:
			// Channel is full or receiver is gone, log warning.
			mr.logger.Warn("Failed to route response - channel full or closed",
				zap.Any("request_id", resp.ID),
			)
		}

		// Close the channel to prevent goroutine leaks.
		close(ch)
	} else {
		mr.logger.Warn("Received response for unknown request",
			zap.Any("request_id", resp.ID),
		)
	}
}

// receiveResponseWithTimeout receives a response with the specified timeout.
func (mr *MessageRouter) receiveResponseWithTimeout(timeout time.Duration) (*mcp.Response, error) {
	ctx, cancel := context.WithTimeout(mr.ctx, timeout)
	defer cancel()

	// Use a channel to handle the async operation with timeout.
	respChan := make(chan *mcp.Response, 1)
	errChan := make(chan error, 1)

	go func() {
		resp, err := mr.gwClient.ReceiveResponse()
		if err != nil {
			select {
			case errChan <- err:
			case <-ctx.Done():
			}
		} else {
			select {
			case respChan <- resp:
			case <-ctx.Done():
			}
		}
	}()

	select {
	case resp := <-respChan:
		return resp, nil
	case err := <-errChan:
		return nil, err
	case <-ctx.Done():
		if ctx.Err() == context.DeadlineExceeded {
			return nil, ErrReceiveTimeout
		}

		return nil, ctx.Err()
	}
}

// SendInitialization sends the MCP initialization request.
func (mr *MessageRouter) SendInitialization() error {
	initReq := &mcp.Request{
		JSONRPC: "2.0",
		Method:  "initialize",
		Params: map[string]interface{}{
			"protocolVersion": MCPProtocolVersion,
			"capabilities": map[string]interface{}{
				"tools":     true,
				"resources": true,
				"prompts":   true,
			},
			"clientInfo": map[string]interface{}{
				"name":    MCPRouterClientName,
				"version": MCPRouterClientVersion,
			},
		},
		ID: InitializationRequestID,
	}

	// Send through the normal request path.
	data, err := json.Marshal(initReq)
	if err != nil {
		return err
	}

	// Process as if it came from stdin.
	return mr.processStdinMessage(data)
}

// extractRequestID safely extracts the request ID from raw JSON data.
func (mr *MessageRouter) extractRequestID(data []byte) interface{} {
	var req struct {
		ID interface{} `json:"id"`
	}

	// Try to parse just the ID field, ignore errors for malformed JSON.
	if err := json.Unmarshal(data, &req); err != nil {
		mr.logger.Debug("Failed to extract request ID from data", zap.Error(err))

		return nil
	}

	return req.ID
}

// Helper function to check for timeout errors.
func isTimeoutError(err error) bool {
	return errors.Is(err, ErrReceiveTimeout)
}
