// Package tracing provides common tracing utilities for MCP Bridge components.
package tracing

import (
	"context"
	"fmt"
	"net/http"

	"github.com/gorilla/websocket"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// WebSocketSpanAttributes creates standard OpenTelemetry attributes for WebSocket connections.
func WebSocketSpanAttributes(request *http.Request, remoteAddr string) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.String("net.transport", "websocket"),
		attribute.String("net.peer.ip", remoteAddr),
		attribute.String("http.scheme", "ws"),
		attribute.String("http.method", request.Method),
		attribute.String("http.target", request.URL.Path),
		attribute.String("http.host", request.Host),
		attribute.String("http.user_agent", request.UserAgent()),
	}

	// Add protocol headers if present
	if proto := request.Header.Get("Sec-WebSocket-Protocol"); proto != "" {
		attrs = append(attrs, attribute.String("websocket.protocol", proto))
	}

	if version := request.Header.Get("Sec-WebSocket-Version"); version != "" {
		attrs = append(attrs, attribute.String("websocket.version", version))
	}

	return attrs
}

// MCPMessageAttributes creates OpenTelemetry attributes for MCP protocol messages.
func MCPMessageAttributes(msgType, method string, id interface{}) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.String("mcp.message.type", msgType),
	}

	if method != "" {
		attrs = append(attrs, attribute.String("mcp.method", method))
	}

	if id != nil {
		attrs = append(attrs, attribute.String("mcp.message.id", fmt.Sprintf("%v", id)))
	}

	return attrs
}

// WebSocketMessageEvent records a WebSocket message event in the current span.
func WebSocketMessageEvent(ctx context.Context, direction string, messageType, size int) {
	span := trace.SpanFromContext(ctx)
	if span == nil || !span.IsRecording() {
		return
	}

	msgTypeStr := "text"
	if messageType == websocket.BinaryMessage {
		msgTypeStr = "binary"
	}

	span.AddEvent("websocket.message."+direction,
		trace.WithAttributes(
			attribute.String("websocket.message.type", msgTypeStr),
			attribute.Int("websocket.message.size", size),
		),
	)
}

// ConnectionPoolAttributes creates attributes for connection pool operations.
func ConnectionPoolAttributes(poolType string, size, active, idle int) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("pool.type", poolType),
		attribute.Int("pool.size", size),
		attribute.Int("pool.connections.active", active),
		attribute.Int("pool.connections.idle", idle),
	}
}

// GatewayRouteAttributes creates attributes for gateway routing decisions.
func GatewayRouteAttributes(serviceName, targetHost string, targetPort int) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("gateway.service", serviceName),
		attribute.String("gateway.target.host", targetHost),
		attribute.Int("gateway.target.port", targetPort),
	}
}

// AuthenticationAttributes creates attributes for authentication operations.
func AuthenticationAttributes(authType string, success bool, userID string) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.String("auth.type", authType),
		attribute.Bool("auth.success", success),
	}

	if userID != "" {
		attrs = append(attrs, attribute.String("auth.user.id", userID))
	}

	return attrs
}

// ErrorAttributes creates attributes from an error.
func ErrorAttributes(err error) []attribute.KeyValue {
	if err == nil {
		return nil
	}

	return []attribute.KeyValue{
		attribute.Bool("error", true),
		attribute.String("error.type", fmt.Sprintf("%T", err)),
		attribute.String("error.message", err.Error()),
	}
}

// RateLimitAttributes creates attributes for rate limiting operations.
func RateLimitAttributes(limited bool, limit, remaining int, resetTime int64) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.Bool("ratelimit.limited", limited),
		attribute.Int("ratelimit.limit", limit),
		attribute.Int("ratelimit.remaining", remaining),
		attribute.Int64("ratelimit.reset_time", resetTime),
	}
}

// HealthCheckAttributes creates attributes for health check operations.
func HealthCheckAttributes(checkType string, healthy bool, latencyMs int64) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("health.check.type", checkType),
		attribute.Bool("health.check.healthy", healthy),
		attribute.Int64("health.check.latency_ms", latencyMs),
	}
}

// CircuitBreakerAttributes creates attributes for circuit breaker state changes.
func CircuitBreakerAttributes(state string, failures, successes int) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String("circuitbreaker.state", state),
		attribute.Int("circuitbreaker.failures", failures),
		attribute.Int("circuitbreaker.successes", successes),
	}
}
