package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/opentracing/opentracing-go"
	"go.uber.org/zap"

	"github.com/actual-software/mcp-bridge/services/gateway/internal/auth"
	customerrors "github.com/actual-software/mcp-bridge/services/gateway/internal/errors"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/metrics"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/session"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/validation"
	"github.com/actual-software/mcp-bridge/services/gateway/pkg/wire"
	"github.com/actual-software/mcp-bridge/services/router/pkg/mcp"
)

const (
	// Retry configuration.
	retryDelayBaseMillis  = 100 // Base delay for exponential backoff in milliseconds
	healthCheckMaxRetries = 3   // Maximum retries for health checks
)

// TCPHandler handles TCP connections using the binary wire protocol.
type TCPHandler struct {
	logger      *zap.Logger
	auth        auth.Provider
	router      RouterInterface
	sessions    session.Manager
	metrics     *metrics.Registry
	rateLimit   RateLimiter
	messageAuth *auth.MessageAuthenticator
}

// CreateTCPProtocolHandler creates a handler for TCP binary wire protocol connections.
func CreateTCPProtocolHandler(
	logger *zap.Logger,
	auth auth.Provider,
	router RouterInterface,
	sessions session.Manager,
	metrics *metrics.Registry,
	rateLimit RateLimiter,
	messageAuth *auth.MessageAuthenticator,
) *TCPHandler {
	return &TCPHandler{
		logger:      logger,
		auth:        auth,
		router:      router,
		sessions:    sessions,
		metrics:     metrics,
		rateLimit:   rateLimit,
		messageAuth: messageAuth,
	}
}

// HandleConnection handles a TCP connection.
func (h *TCPHandler) HandleConnection(ctx context.Context, conn net.Conn) {
	// Initialize connection
	transport := wire.NewTransport(conn)
	defer h.closeTransport(transport)

	remoteAddr := conn.RemoteAddr().String()
	h.logger.Info("New TCP connection", zap.String("remote", remoteAddr))

	// Setup metrics and context
	h.metrics.IncrementTCPConnections()
	defer h.metrics.DecrementTCPConnections()

	connCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Initialize connection state
	state := &connectionState{
		transport:         transport,
		remoteAddr:        remoteAddr,
		authenticated:     false,
		versionNegotiated: false,
		agreedVersion:     0,
		wg:                &sync.WaitGroup{},
	}

	// Cleanup on exit
	defer h.cleanupConnection(state)

	// Main message loop
	h.messageLoop(connCtx, state)
}

// connectionState holds the state for a TCP connection.
type connectionState struct {
	transport         *wire.Transport
	remoteAddr        string
	session           *session.Session
	authenticated     bool
	versionNegotiated bool
	agreedVersion     uint16
	wg                *sync.WaitGroup
}

// messageLoop handles the main message processing loop.
func (h *TCPHandler) messageLoop(ctx context.Context, state *connectionState) {
	for {
		if err := h.processNextMessage(ctx, state); err != nil {
			if errors.Is(err, context.Canceled) {
				h.logger.Debug("Connection context canceled", zap.String("remote", state.remoteAddr))
			} else {
				h.logger.Error("Error processing message", zap.Error(err))
			}

			return
		}
	}
}

// processNextMessage reads and processes the next message.
func (h *TCPHandler) processNextMessage(ctx context.Context, state *connectionState) error {
	select {
	case <-ctx.Done():
		return context.Canceled
	default:
	}

	// Set read deadline
	if err := state.transport.SetReadDeadline(time.Now().Add(time.Minute)); err != nil {
		h.logger.Debug("Failed to set read deadline", zap.Error(err))
	}

	// Receive message
	msgType, msg, err := state.transport.ReceiveMessage()
	if err != nil {
		return h.handleReceiveError(state, err)
	}

	// Check version negotiation requirement
	if !state.versionNegotiated && msgType != wire.MessageTypeVersionNegotiation {
		h.logger.Warn("Received message before version negotiation",
			zap.Uint16("msgType", uint16(msgType)),
			zap.String("remote", state.remoteAddr))
		h.sendProtocolError(state.transport, "Version negotiation required")

		return customerrors.NewProtocolError("version negotiation required", "tcp")
	}

	// Handle message based on type
	return h.handleMessage(ctx, state, msgType, msg)
}

// handleReceiveError handles errors when receiving messages.
func (h *TCPHandler) handleReceiveError(state *connectionState, err error) error {
	h.metrics.IncrementTCPProtocolErrors("receive_error")

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		// Handle timeout with health check
		if err := h.sendHealthCheckWithRetry(state); err != nil {
			return err
		}

		return nil // Continue after successful health check
	}

	return err // Fatal error
}

// sendHealthCheckWithRetry sends a health check with retry logic.
func (h *TCPHandler) sendHealthCheckWithRetry(state *connectionState) error {
	for retryCount := 0; retryCount < healthCheckMaxRetries; retryCount++ {
		if err := state.transport.SendHealthCheck(); err != nil {
			h.logger.Warn("Failed to send health check, retrying",
				zap.Error(err),
				zap.Int("retry", retryCount+1),
				zap.Int("max_retries", healthCheckMaxRetries))

			if retryCount < healthCheckMaxRetries-1 {
				// Exponential backoff
				time.Sleep(time.Duration(retryDelayBaseMillis*(retryCount+1)) * time.Millisecond)

				continue
			}

			h.logger.Error("Failed to send health check after retries, closing connection",
				zap.Error(err),
				zap.String("remote", state.remoteAddr))

			return err
		}
		// Health check succeeded
		return nil
	}

	return customerrors.New(customerrors.TypeInternal, "health check failed after retries").
		WithComponent("tcp_handler").
		WithOperation("health_check")
}

// closeTransport safely closes the transport.
func (h *TCPHandler) closeTransport(transport *wire.Transport) {
	if err := transport.Close(); err != nil {
		h.logger.Debug("Error closing transport", zap.Error(err))
	}
}

// cleanupConnection performs cleanup when connection closes.
func (h *TCPHandler) cleanupConnection(state *connectionState) {
	// Wait for all ongoing requests to complete
	if !h.waitForRequests(state.wg, defaultTimeoutSeconds*time.Second) {
		h.logger.Warn("Some requests did not complete within timeout",
			zap.String("remote", state.remoteAddr))
	}

	h.logger.Info("TCP connection closed", zap.String("remote", state.remoteAddr))
}

// handleMessage processes a message based on its type.
func (h *TCPHandler) handleMessage(
	ctx context.Context,
	state *connectionState,
	msgType wire.MessageType,
	msg interface{},
) error {
	switch msgType {
	case wire.MessageTypeVersionNegotiation:
		return h.handleVersionNegotiation(state, msg)
	case wire.MessageTypeRequest:
		return h.handleRequest(ctx, state, msg)
	case wire.MessageTypeHealthCheck:
		return h.handleHealthCheck(state)
	case wire.MessageTypeResponse:
		h.sendProtocolError(state.transport, "Unexpected response message from client")

		return nil
	case wire.MessageTypeError:
		h.logger.Info("Received error message from client",
			zap.String("remote", state.remoteAddr))

		return nil
	case wire.MessageTypeVersionAck:
		h.sendProtocolError(state.transport, "Unexpected version ack from client")

		return nil
	case wire.MessageTypeControl:
		h.logger.Debug("Received control message",
			zap.String("remote", state.remoteAddr))

		return nil
	default:
		h.sendProtocolError(state.transport, "Unknown message type")

		return nil
	}
}

// handleVersionNegotiation handles version negotiation messages.
func (h *TCPHandler) handleVersionNegotiation(state *connectionState, msg interface{}) error {
	h.metrics.IncrementTCPMessages("inbound", "version_negotiation")

	if state.versionNegotiated {
		h.sendProtocolError(state.transport, "Version already negotiated")

		return nil
	}

	negotiation, ok := msg.(*wire.VersionNegotiationPayload)
	if !ok {
		h.sendProtocolError(state.transport, "Invalid version negotiation payload")

		return nil
	}

	// Negotiate version
	agreed, err := wire.NegotiateVersion(
		negotiation.MinVersion, negotiation.MaxVersion,
		wire.MinVersion, wire.MaxVersion)
	if err != nil {
		h.logger.Error("Version negotiation failed",
			zap.Error(err),
			zap.Uint16("clientMin", negotiation.MinVersion),
			zap.Uint16("clientMax", negotiation.MaxVersion))
		h.sendProtocolError(state.transport, fmt.Sprintf("Version negotiation failed: %v", err))

		return err
	}

	state.agreedVersion = agreed
	state.versionNegotiated = true

	// Send version acknowledgment
	ack := &wire.VersionAckPayload{
		AgreedVersion: state.agreedVersion,
	}
	if err := h.sendVersionAck(state.transport, ack); err != nil {
		h.logger.Error("Failed to send version ack", zap.Error(err))

		return err
	}

	h.logger.Info("Version negotiated successfully",
		zap.Uint16("agreedVersion", state.agreedVersion),
		zap.String("remote", state.remoteAddr))

	return nil
}

// handleRequest handles request messages.
func (h *TCPHandler) handleRequest(ctx context.Context, state *connectionState, msg interface{}) error {
	h.metrics.IncrementTCPMessages("inbound", "request")

	// Extract request and auth token from message
	req, authToken, err := h.extractRequestFromMessage(msg)
	if err != nil {
		return err
	}

	// Validate request format
	if err := h.validateRequestFormat(req, state); err != nil {
		return err // Already sent error response
	}

	// Handle authentication if not authenticated
	if !state.authenticated {
		return h.handleInitialization(ctx, state, req)
	}

	// Validate per-message authentication
	if err := h.validatePerMessageAuth(ctx, authToken, state, req); err != nil {
		return err // Already sent error response
	}

	// Check rate limit
	if err := h.checkRateLimit(ctx, state, req); err != nil {
		return err // Already sent error response
	}

	// Route request
	state.wg.Add(1)

	go func(req *mcp.Request) {
		defer state.wg.Done()

		h.processRequestAsync(ctx, state, req)
	}(req)

	return nil
}

// processRequestAsync processes a request asynchronously.
func (h *TCPHandler) processRequestAsync(ctx context.Context, state *connectionState, req *mcp.Request) {
	startTime := time.Now()

	defer func() {
		duration := time.Since(startTime)
		h.metrics.RecordRequestDuration(req.Method, "success", duration)
	}()

	// Create tracing span
	span, ctx := opentracing.StartSpanFromContext(ctx, "tcp.processRequest")
	defer span.Finish()

	span.SetTag("mcp.method", req.Method)
	span.SetTag("mcp.id", req.ID)

	if state.session != nil {
		span.SetTag("session.user", state.session.User)
	}

	// Extract target namespace from params
	targetNamespace := ""

	if params, ok := req.Params.(map[string]interface{}); ok {
		if ns, ok := params["namespace"].(string); ok {
			if err := validation.ValidateNamespace(ns); err == nil {
				targetNamespace = ns
			}
		}
	}

	// Route the request
	ctx = context.WithValue(ctx, sessionContextKey, state.session)

	response, err := h.router.RouteRequest(ctx, req, targetNamespace)
	if err != nil {
		h.logger.Error("Failed to route request",
			zap.Error(err),
			zap.String("method", req.Method),
			zap.Any("id", req.ID))
		h.sendError(state.transport, req.ID, fmt.Sprintf("Routing failed: %v", err))

		return
	}

	// Send response
	h.metrics.IncrementTCPMessages("outbound", "response")

	if err := state.transport.SendResponse(response); err != nil {
		h.logger.Error("Failed to send response", zap.Error(err))
		h.metrics.IncrementTCPProtocolErrors("send_error")
	}
}

// waitForRequests waits for all ongoing requests to complete before returning.
func (h *TCPHandler) waitForRequests(wg *sync.WaitGroup, timeout time.Duration) bool {
	done := make(chan struct{})

	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// handleHealthCheck handles health check messages.
func (h *TCPHandler) handleHealthCheck(state *connectionState) error {
	h.metrics.IncrementTCPMessages("inbound", "health_check")

	// Send health check response with retry
	for attempt := 0; attempt < healthCheckMaxRetries; attempt++ {
		if err := state.transport.SendHealthCheck(); err != nil {
			h.logger.Warn("Failed to send health check response",
				zap.Error(err),
				zap.Int("attempt", attempt+1),
				zap.String("remote", state.remoteAddr))

			if attempt < healthCheckMaxRetries-1 {
				time.Sleep(time.Duration(retryDelayBaseMillis*(attempt+1)) * time.Millisecond)

				continue
			}

			return err
		}

		h.logger.Debug("Health check response sent",
			zap.String("remote", state.remoteAddr))
		h.metrics.IncrementTCPMessages("outbound", "health_check")

		return nil
	}

	return customerrors.New(customerrors.TypeInternal, "failed to send health check after retries").
		WithComponent("tcp_handler").
		WithOperation("send_health_check")
}

// extractRequestFromMessage extracts request and auth token from message.
func (h *TCPHandler) extractRequestFromMessage(msg interface{}) (*mcp.Request, string, error) {
	if authMsg, ok := msg.(*wire.AuthMessage); ok {
		req, err := authMsg.ExtractRequest()
		if err != nil {
			h.logger.Error("Failed to extract request from auth message", zap.Error(err))

			return nil, "", nil
		}

		return req, authMsg.AuthToken, nil
	}

	if req, ok := msg.(*mcp.Request); ok {
		return req, "", nil
	}

	h.logger.Error("Invalid message type")

	return nil, "", nil
}

// validateRequestFormat validates basic request format.
func (h *TCPHandler) validateRequestFormat(req *mcp.Request, state *connectionState) error {
	// Validate request ID
	if err := validation.ValidateRequestID(req.ID); err != nil {
		h.sendError(state.transport, req.ID, fmt.Sprintf("Invalid request ID: %v", err))

		return err
	}

	// Validate method
	if err := validation.ValidateMethod(req.Method); err != nil {
		h.sendError(state.transport, req.ID, fmt.Sprintf("Invalid method: %v", err))

		return err
	}

	return nil
}

// handleInitialization handles the initialization request with authentication.
func (h *TCPHandler) handleInitialization(ctx context.Context, state *connectionState, req *mcp.Request) error {
	if req.Method != "initialize" {
		h.sendError(state.transport, req.ID, "Unauthorized: must initialize first")

		return nil
	}

	// Extract and validate token
	token, err := h.extractInitToken(req)
	if err != nil {
		h.sendError(state.transport, req.ID, err.Error())

		return nil
	}

	// Authenticate token
	if err := h.authenticateInitToken(ctx, state, req, token); err != nil {
		return err
	}

	// Send success response
	return h.sendInitializeSuccess(state, req)
}

// extractInitToken extracts token from initialize request.
func (h *TCPHandler) extractInitToken(req *mcp.Request) (string, error) {
	params, ok := req.Params.(map[string]interface{})
	if !ok {
		return "", customerrors.New(customerrors.TypeValidation, "invalid initialize params").
			WithComponent("tcp_handler").
			WithOperation("initialize")
	}

	token, ok := params["token"].(string)
	if !ok {
		return "", customerrors.New(customerrors.TypeUnauthorized, "missing authentication token").
			WithComponent("tcp_handler").
			WithOperation("initialize")
	}

	if err := validation.ValidateToken(token); err != nil {
		return "", customerrors.Wrap(err, "invalid token format").
			WithComponent("tcp_handler").
			WithOperation("validate_token")
	}

	return token, nil
}

// authenticateInitToken authenticates the initialization token.
func (h *TCPHandler) authenticateInitToken(
	ctx context.Context,
	state *connectionState,
	req *mcp.Request,
	token string,
) error {
	claims, err := h.auth.ValidateToken(token)
	if err != nil {
		h.logger.Warn("Authentication failed",
			zap.String("remote", state.remoteAddr),
			zap.Error(err))
		h.sendError(state.transport, req.ID, "Authentication failed")
		h.metrics.IncrementAuthFailures("invalid_token")

		return err
	}

	// Create session
	state.session, err = h.sessions.CreateSession(claims)
	if err != nil {
		h.logger.Error("Failed to create session", zap.Error(err))
		h.sendError(state.transport, req.ID, "Failed to create session")

		return err
	}

	state.authenticated = true

	return nil
}

// sendInitializeSuccess sends successful initialization response.
func (h *TCPHandler) sendInitializeSuccess(state *connectionState, req *mcp.Request) error {
	resp := &mcp.Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"sessionId": state.session.ID,
			"expiresAt": state.session.ExpiresAt,
		},
	}

	if err := state.transport.SendResponse(resp); err != nil {
		h.logger.Error("Failed to send initialize response", zap.Error(err))
		h.metrics.IncrementTCPProtocolErrors("send_error")

		return err
	}

	h.metrics.IncrementTCPMessages("outbound", "response")

	return nil
}

// validatePerMessageAuth validates per-message authentication.
func (h *TCPHandler) validatePerMessageAuth(
	ctx context.Context,
	authToken string,
	state *connectionState,
	req *mcp.Request,
) error {
	if h.messageAuth == nil || authToken == "" {
		return nil
	}

	if err := h.messageAuth.ValidateMessageToken(ctx, authToken, state.session.ID); err != nil {
		h.metrics.IncrementAuthFailures("invalid_message_token")
		h.logger.Warn("Per-message authentication failed",
			zap.String("remote", state.remoteAddr),
			zap.Error(err))
		h.sendError(state.transport, req.ID, "Per-message authentication failed")

		return err
	}

	return nil
}

// checkRateLimit checks if request is within rate limits.
func (h *TCPHandler) checkRateLimit(ctx context.Context, state *connectionState, req *mcp.Request) error {
	allowed, err := h.rateLimit.Allow(ctx, state.session.User, state.session.RateLimit)
	if err != nil {
		h.logger.Error("Rate limit check failed", zap.Error(err))
		h.sendError(state.transport, req.ID, "Internal error")

		return err
	}

	if !allowed {
		h.metrics.IncrementRequests(req.Method, "rate_limited")
		h.sendError(state.transport, req.ID, "Rate limit exceeded")

		return customerrors.NewRateLimitExceededError(state.remoteAddr, state.session.RateLimit.RequestsPerMinute, "user")
	}

	return nil
}

// sendError sends an error response.
func (h *TCPHandler) sendError(transport *wire.Transport, id interface{}, message string) {
	resp := &mcp.Response{
		JSONRPC: "2.0",
		ID:      id,
		Error: &mcp.Error{
			Code:    -32603,
			Message: message,
		},
	}

	if err := transport.SendResponse(resp); err != nil {
		h.logger.Error("Failed to send error response", zap.Error(err))
		h.metrics.IncrementTCPProtocolErrors("send_error")
	} else {
		h.metrics.IncrementTCPMessages("outbound", "error")
	}
}

// sendProtocolError sends a protocol-level error.
func (h *TCPHandler) sendProtocolError(transport *wire.Transport, message string) {
	frame := &wire.Frame{
		Version:     wire.CurrentVersion,
		MessageType: wire.MessageTypeError,
		Payload:     []byte(message),
	}

	if err := frame.Encode(transport.GetWriter()); err != nil {
		h.logger.Error("Failed to send protocol error", zap.Error(err))

		return
	}

	if err := transport.Flush(); err != nil {
		h.logger.Error("Failed to flush protocol error", zap.Error(err))
	}
}

// sendVersionAck sends a version acknowledgment.
func (h *TCPHandler) sendVersionAck(transport *wire.Transport, ack *wire.VersionAckPayload) error {
	payload, err := transport.EncodeJSON(ack)
	if err != nil {
		return customerrors.Wrap(err, "failed to encode version ack").
			WithComponent("tcp_handler").
			WithOperation("send_version_ack")
	}

	frame := &wire.Frame{
		Version:     wire.CurrentVersion,
		MessageType: wire.MessageTypeVersionAck,
		Payload:     payload,
	}

	if err := frame.Encode(transport.GetWriter()); err != nil {
		return customerrors.Wrap(err, "failed to send version ack").
			WithComponent("tcp_handler").
			WithOperation("send_version_ack")
	}

	if err := transport.Flush(); err != nil {
		return customerrors.Wrap(err, "failed to flush version ack").
			WithComponent("tcp_handler").
			WithOperation("flush_version_ack")
	}

	h.metrics.IncrementTCPMessages("outbound", "version_ack")

	return nil
}
