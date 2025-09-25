package errors

import (
	"context"
	"net/http"
)

// Error codes for server operations.
const (
	ErrCodeServerStartFailed       = "SERVER_START_FAILED"
	ErrCodeServerShutdownFailed    = "SERVER_SHUTDOWN_FAILED"
	ErrCodeListenerCreationFailed  = "SERVER_LISTENER_FAILED"
	ErrCodeTLSConfigFailed         = "SERVER_TLS_CONFIG_FAILED"
	ErrCodeConnectionAcceptFailed  = "SERVER_ACCEPT_FAILED"
	ErrCodeConnectionHandleFailed  = "SERVER_HANDLE_FAILED"
	ErrCodeConfigLoadFailed        = "SERVER_CONFIG_LOAD_FAILED"
	ErrCodeServerHealthCheckFailed = "SERVER_HEALTH_CHECK_FAILED"
	ErrCodeRateLimitExceeded       = "SERVER_RATE_LIMIT_EXCEEDED"
	ErrCodeMaxConnectionsReached   = "SERVER_MAX_CONNECTIONS"
	ErrCodeProtocolError           = "SERVER_PROTOCOL_ERROR"
	ErrCodeInvalidRequest          = "SERVER_INVALID_REQUEST"
)

// WrapServerStartError wraps an error that occurred during server startup.
func WrapServerStartError(ctx context.Context, err error, address string) *GatewayError {
	return WrapContext(ctx, err, "failed to start server").
		WithComponent("server").
		WithOperation("start").
		WithContext("address", address).
		WithContext("code", ErrCodeServerStartFailed)
}

// WrapServerShutdownError wraps an error that occurred during server shutdown.
func WrapServerShutdownError(ctx context.Context, err error) *GatewayError {
	return WrapContext(ctx, err, "failed to shutdown server gracefully").
		WithComponent("server").
		WithOperation("shutdown").
		WithContext("code", ErrCodeServerShutdownFailed)
}

// WrapListenerCreationError wraps an error that occurred while creating a listener.
func WrapListenerCreationError(ctx context.Context, err error, address string, protocol string) *GatewayError {
	return WrapContext(ctx, err, "failed to create listener").
		WithComponent("server").
		WithOperation("create_listener").
		WithContext("address", address).
		WithContext("protocol", protocol).
		WithContext("code", ErrCodeListenerCreationFailed)
}

// WrapTLSConfigError wraps an error related to TLS configuration.
func WrapTLSConfigError(ctx context.Context, err error, certFile, keyFile string) *GatewayError {
	return WrapContext(ctx, err, "TLS configuration failed").
		WithComponent("server").
		WithOperation("configure_tls").
		WithContext("cert_file", certFile).
		WithContext("key_file", keyFile).
		WithContext("code", ErrCodeTLSConfigFailed)
}

// WrapConnectionAcceptError wraps an error that occurred while accepting a connection.
func WrapConnectionAcceptError(ctx context.Context, err error) *GatewayError {
	return WrapContext(ctx, err, "failed to accept connection").
		WithComponent("server").
		WithOperation("accept_connection").
		WithContext("code", ErrCodeConnectionAcceptFailed)
}

// WrapConnectionHandleError wraps an error that occurred while handling a connection.
func WrapConnectionHandleError(ctx context.Context, err error, clientAddr string) *GatewayError {
	return WrapContext(ctx, err, "failed to handle connection").
		WithComponent("server").
		WithOperation("handle_connection").
		WithContext("client_address", clientAddr).
		WithContext("code", ErrCodeConnectionHandleFailed)
}

// WrapConfigLoadError wraps an error that occurred while loading configuration.
func WrapConfigLoadError(ctx context.Context, err error, configPath string) *GatewayError {
	return WrapContext(ctx, err, "failed to load configuration").
		WithComponent("server").
		WithOperation("load_config").
		WithContext("config_path", configPath).
		WithContext("code", ErrCodeConfigLoadFailed)
}

// NewRateLimitExceededError creates an error for rate limit exceeded.
func NewRateLimitExceededError(clientAddr string, limit int, window string) *GatewayError {
	return New(TypeRateLimit, "rate limit exceeded").
		WithComponent("server").
		WithContext("client_address", clientAddr).
		WithContext("limit", limit).
		WithContext("window", window).
		WithContext("code", ErrCodeRateLimitExceeded).
		WithHTTPStatus(http.StatusTooManyRequests)
}

// NewMaxConnectionsError creates an error when max connections is reached.
func NewMaxConnectionsError(current, max int) *GatewayError {
	return New(TypeUnavailable, "maximum connections reached").
		WithComponent("server").
		WithContext("current_connections", current).
		WithContext("max_connections", max).
		WithContext("code", ErrCodeMaxConnectionsReached).
		WithHTTPStatus(http.StatusServiceUnavailable)
}

// NewProtocolError creates an error for protocol violations.
func NewProtocolError(reason string, protocol string) *GatewayError {
	return New(TypeValidation, "protocol error: " + reason ).
		WithComponent("server").
		WithContext("reason", reason).
		WithContext("protocol", protocol).
		WithContext("code", ErrCodeProtocolError).
		WithHTTPStatus(http.StatusBadRequest)
}

// NewInvalidRequestError creates an error for invalid requests.
func NewInvalidRequestError(reason string, path string) *GatewayError {
	return New(TypeValidation, "invalid request: " + reason ).
		WithComponent("server").
		WithContext("reason", reason).
		WithContext("path", path).
		WithContext("code", ErrCodeInvalidRequest).
		WithHTTPStatus(http.StatusBadRequest)
}

// serverContextKey is a type for server-specific context keys.
type serverContextKey string

const (
	// Context keys for server connection information.
	serverContextKeyClientAddr serverContextKey = "connection.client"
	serverContextKeyServerAddr serverContextKey = "connection.server"
	serverContextKeyProtocol   serverContextKey = "connection.protocol"
)

// EnrichContextWithConnection adds connection information to context for error tracking.
func EnrichContextWithConnection(ctx context.Context, clientAddr, serverAddr string, protocol string) context.Context {
	if clientAddr != "" {
		ctx = context.WithValue(ctx, serverContextKeyClientAddr, clientAddr)
	}

	if serverAddr != "" {
		ctx = context.WithValue(ctx, serverContextKeyServerAddr, serverAddr)
	}

	if protocol != "" {
		ctx = context.WithValue(ctx, serverContextKeyProtocol, protocol)
	}

	return ctx
}
