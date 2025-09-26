package router

import (
	"context"
	"fmt"
	"net/http"

	"github.com/poiley/mcp-bridge/services/gateway/internal/discovery"
	"github.com/poiley/mcp-bridge/services/gateway/internal/errors"
	"github.com/poiley/mcp-bridge/services/router/pkg/mcp"
)

// RouterError extends the base GatewayError with router-specific functionality.
type RouterError struct {
	*errors.GatewayError
	Namespace string
	Endpoint  string
	Method    string
}

// Error codes specific to routing.
const (
	ErrCodeNoEndpoints        = "ROUTER_NO_ENDPOINTS"
	ErrCodeNoHealthyEndpoints = "ROUTER_NO_HEALTHY_ENDPOINTS"
	ErrCodeUnsupportedScheme  = "ROUTER_UNSUPPORTED_SCHEME"
	ErrCodeCircuitBreakerOpen = "ROUTER_CIRCUIT_BREAKER_OPEN"
	ErrCodeForwardingFailed   = "ROUTER_FORWARDING_FAILED"
	ErrCodeWebSocketFailed    = "ROUTER_WEBSOCKET_FAILED"
	ErrCodeHTTPFailed         = "ROUTER_HTTP_FAILED"
	ErrCodeMarshalFailed      = "ROUTER_MARSHAL_FAILED"
	ErrCodeUnmarshalFailed    = "ROUTER_UNMARSHAL_FAILED"
	ErrCodeTimeout            = "ROUTER_TIMEOUT"
)

// NewNoEndpointsError creates an error for when no endpoints are available.
func NewNoEndpointsError(namespace string) *errors.GatewayError {
	return errors.New(errors.TypeUnavailable, "no endpoints available for namespace: "+namespace).
		WithComponent("router").
		WithContext("namespace", namespace).
		WithContext("code", ErrCodeNoEndpoints).
		WithHTTPStatus(http.StatusServiceUnavailable)
}

// NewNoHealthyEndpointsError creates an error for when no healthy endpoints are available.
func NewNoHealthyEndpointsError(namespace string) *errors.GatewayError {
	return errors.New(errors.TypeUnavailable, "no healthy endpoints available for namespace: "+namespace).
		WithComponent("router").
		WithContext("namespace", namespace).
		WithContext("code", ErrCodeNoHealthyEndpoints).
		WithHTTPStatus(http.StatusServiceUnavailable)
}

// NewUnsupportedSchemeError creates an error for unsupported endpoint schemes.
func NewUnsupportedSchemeError(scheme string, endpoint *discovery.Endpoint) *errors.GatewayError {
	return errors.New(errors.TypeValidation, "unsupported endpoint scheme: "+scheme).
		WithComponent("router").
		WithContext("scheme", scheme).
		WithContext("endpoint", endpoint.Address).
		WithContext("code", ErrCodeUnsupportedScheme).
		WithHTTPStatus(http.StatusBadRequest)
}

// NewCircuitBreakerOpenError creates an error for when the circuit breaker is open.
func NewCircuitBreakerOpenError(endpoint string) *errors.GatewayError {
	return errors.New(errors.TypeUnavailable, "circuit breaker is open").
		WithComponent("router").
		WithContext("endpoint", endpoint).
		WithContext("code", ErrCodeCircuitBreakerOpen).
		WithHTTPStatus(http.StatusServiceUnavailable)
}

// WrapForwardingError wraps an error that occurred during request forwarding.
func WrapForwardingError(
	ctx context.Context,
	err error,
	endpoint *discovery.Endpoint,
	method string,
) *errors.GatewayError {
	return errors.WrapContext(ctx, err, "failed to forward request").
		WithComponent("router").
		WithOperation("forward_request").
		WithContext("endpoint", fmt.Sprintf("%s:%d", endpoint.Address, endpoint.Port)).
		WithContext("method", method).
		WithContext("protocol", endpoint.Scheme).
		WithContext("code", ErrCodeForwardingFailed)
}

// WrapWebSocketError wraps a WebSocket-specific error.
func WrapWebSocketError(
	ctx context.Context,
	err error,
	operation string,
	endpoint *discovery.Endpoint,
) *errors.GatewayError {
	return errors.WrapContextf(ctx, err, "WebSocket %s failed", operation).
		WithComponent("router").
		WithOperation("websocket_"+operation).
		WithContext("endpoint", fmt.Sprintf("%s:%d", endpoint.Address, endpoint.Port)).
		WithContext("code", ErrCodeWebSocketFailed)
}

// WrapHTTPError wraps an HTTP-specific error.
func WrapHTTPError(
	ctx context.Context,
	err error,
	operation string,
	endpoint *discovery.Endpoint,
) *errors.GatewayError {
	return errors.WrapContextf(ctx, err, "HTTP %s failed", operation).
		WithComponent("router").
		WithOperation("http_"+operation).
		WithContext("endpoint", fmt.Sprintf("%s:%d", endpoint.Address, endpoint.Port)).
		WithContext("code", ErrCodeHTTPFailed)
}

// WrapMarshalError wraps a marshaling error.
func WrapMarshalError(err error, dataType string) *errors.GatewayError {
	return errors.Wrapf(err, "failed to marshal %s", dataType).
		WithComponent("router").
		WithOperation("marshal").
		WithContext("data_type", dataType).
		WithContext("code", ErrCodeMarshalFailed)
}

// WrapUnmarshalError wraps an unmarshaling error.
func WrapUnmarshalError(err error, dataType string) *errors.GatewayError {
	return errors.Wrapf(err, "failed to unmarshal %s", dataType).
		WithComponent("router").
		WithOperation("unmarshal").
		WithContext("data_type", dataType).
		WithContext("code", ErrCodeUnmarshalFailed)
}

// WrapTimeoutError wraps a timeout error with context.
func WrapTimeoutError(
	ctx context.Context,
	err error,
	operation string,
	endpoint *discovery.Endpoint,
) *errors.GatewayError {
	return errors.NewTimeoutError(operation, err).
		WithComponent("router").
		WithContext("endpoint", fmt.Sprintf("%s:%d", endpoint.Address, endpoint.Port)).
		WithContext("code", ErrCodeTimeout)
}

// EnrichContextWithRequest adds request information to the context for error tracking.
func EnrichContextWithRequest(ctx context.Context, req *mcp.Request, namespace string) context.Context {
	ctx = errors.EnrichWithEndpoint(ctx, "", req.Method, "mcp")
	ctx = errors.EnrichWithBackend(ctx, "", namespace)

	if req.ID != "" {
		ctx = context.WithValue(ctx, errors.ContextKeyRequestID, req.ID)
	}

	return ctx
}
