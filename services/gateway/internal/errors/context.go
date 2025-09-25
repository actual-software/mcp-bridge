package errors

import (
	"context"
	"errors"
	"fmt"
)

// ContextKey is a type for context keys to avoid collisions.
type ContextKey string

const (
	// Context keys for error metadata.
	ContextKeyRequestID ContextKey = "request_id"
	ContextKeyUserID    ContextKey = "user_id"
	ContextKeySessionID ContextKey = "session_id"
	ContextKeyEndpoint  ContextKey = "endpoint"
	ContextKeyMethod    ContextKey = "method"
	ContextKeyNamespace ContextKey = "namespace"
	ContextKeyBackend   ContextKey = "backend"
	ContextKeyProtocol  ContextKey = "protocol"
	ContextKeyClientIP  ContextKey = "client_ip"
	ContextKeyTraceID   ContextKey = "trace_id"
)

// contextMapping defines a mapping between context keys and field names.
type contextMapping struct {
	key   ContextKey
	field string
}

// getContextMappings returns all context mappings to reduce complexity.
func getContextMappings() []contextMapping {
	return []contextMapping{
		{ContextKeyRequestID, "request_id"},
		{ContextKeyUserID, "user_id"},
		{ContextKeySessionID, "session_id"},
		{ContextKeyEndpoint, "endpoint"},
		{ContextKeyMethod, "method"},
		{ContextKeyNamespace, "namespace"},
		{ContextKeyBackend, "backend"},
		{ContextKeyProtocol, "protocol"},
		{ContextKeyClientIP, "client_ip"},
		{ContextKeyTraceID, "trace_id"},
	}
}

// FromContext extracts error context from a context.Context and adds it to the error.
func FromContext(ctx context.Context, err error) *GatewayError {
	if err == nil {
		return nil
	}

	var ge *GatewayError
	if !errors.As(err, &ge) {
		ge = Wrap(err, err.Error())
	}

	// Add context values using a loop to reduce complexity
	for _, mapping := range getContextMappings() {
		if value := ctx.Value(mapping.key); value != nil {
			ge = ge.WithContext(mapping.field, value)
		}
	}

	return ge
}

// WrapContext wraps an error with context information.
func WrapContext(ctx context.Context, err error, message string) *GatewayError {
	if err == nil {
		return nil
	}

	return FromContext(ctx, Wrap(err, message))
}

// WrapContextf wraps an error with formatted message and context.
func WrapContextf(ctx context.Context, err error, format string, args ...interface{}) *GatewayError {
	if err == nil {
		return nil
	}

	return FromContext(ctx, Wrap(err, fmt.Sprintf(format, args...)))
}

// NewWithContext creates a new error with context information.
func NewWithContext(ctx context.Context, errType ErrorType, message string) *GatewayError {
	return FromContext(ctx, New(errType, message))
}

// EnrichContext adds standard request context to the context.Context.
func EnrichContext(ctx context.Context, requestID, userID, sessionID, clientIP string) context.Context {
	if requestID != "" {
		ctx = context.WithValue(ctx, ContextKeyRequestID, requestID)
	}

	if userID != "" {
		ctx = context.WithValue(ctx, ContextKeyUserID, userID)
	}

	if sessionID != "" {
		ctx = context.WithValue(ctx, ContextKeySessionID, sessionID)
	}

	if clientIP != "" {
		ctx = context.WithValue(ctx, ContextKeyClientIP, clientIP)
	}

	return ctx
}

// EnrichWithEndpoint adds endpoint information to the context.
func EnrichWithEndpoint(ctx context.Context, endpoint, method, protocol string) context.Context {
	if endpoint != "" {
		ctx = context.WithValue(ctx, ContextKeyEndpoint, endpoint)
	}

	if method != "" {
		ctx = context.WithValue(ctx, ContextKeyMethod, method)
	}

	if protocol != "" {
		ctx = context.WithValue(ctx, ContextKeyProtocol, protocol)
	}

	return ctx
}

// EnrichWithBackend adds backend information to the context.
func EnrichWithBackend(ctx context.Context, backend, namespace string) context.Context {
	if backend != "" {
		ctx = context.WithValue(ctx, ContextKeyBackend, backend)
	}

	if namespace != "" {
		ctx = context.WithValue(ctx, ContextKeyNamespace, namespace)
	}

	return ctx
}

// EnrichWithTraceID adds trace ID to the context.
func EnrichWithTraceID(ctx context.Context, traceID string) context.Context {
	if traceID != "" {
		ctx = context.WithValue(ctx, ContextKeyTraceID, traceID)
	}

	return ctx
}
