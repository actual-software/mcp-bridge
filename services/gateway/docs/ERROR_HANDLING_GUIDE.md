# Error Handling Guidelines for MCP Gateway

## Overview

This document provides guidelines for consistent error handling across the MCP Gateway codebase.

## Error Types and Structure

### Base Error Types

The gateway uses a comprehensive error system with the following base types:

- `GatewayError`: Base error type with context, stack trace, and metadata
- `BackendError`: Specialized error for backend operations
- `RouterError`: Specialized error for routing operations

### Error Classification

Errors are classified by type:
- `VALIDATION`: Input validation errors (400)
- `NOT_FOUND`: Resource not found (404)
- `UNAUTHORIZED`: Authentication required (401)
- `FORBIDDEN`: Access denied (403)
- `INTERNAL`: Internal server errors (500)
- `TIMEOUT`: Operation timeout (408)
- `UNAVAILABLE`: Service unavailable (503)
- `RATE_LIMIT`: Rate limit exceeded (429)

## Best Practices

### 1. Always Wrap External Errors

Instead of:
```go
if err != nil {
    return nil, err
}
```

Use:
```go
if err != nil {
    return nil, errors.Wrap(err, "operation failed")
}
```

Or with context:
```go
if err != nil {
    return nil, errors.WrapContext(ctx, err, "operation failed")
}
```

### 2. Use Typed Errors for Known Conditions

Instead of:
```go
return fmt.Errorf("no endpoints available for namespace: %s", namespace)
```

Use:
```go
return NewNoEndpointsError(namespace)
```

### 3. Add Context to Errors

Always add relevant context:
```go
err := errors.Wrap(originalErr, "connection failed").
    WithComponent("backend").
    WithOperation("connect").
    WithContext("endpoint", endpoint).
    WithContext("retry_count", retries)
```

### 4. Use Context for Request Tracking

Enrich context at entry points:
```go
ctx = errors.EnrichContext(ctx, requestID, userID, sessionID, clientIP)
ctx = errors.EnrichWithEndpoint(ctx, endpoint, method, protocol)
```

Then use it in errors:
```go
return errors.WrapContext(ctx, err, "request processing failed")
```

## Examples

### Router Error Handling

Before:
```go
func (r *Router) selectEndpoint(targetNamespace string, span opentracing.Span) (*discovery.Endpoint, error) {
    lb := r.getLoadBalancer(targetNamespace)
    if lb == nil {
        r.metrics.IncrementRoutingErrors("no_endpoints")
        err := fmt.Errorf("no endpoints available for namespace: %s", targetNamespace)
        ext.Error.Set(span, true)
        span.LogKV("error", err.Error())
        return nil, err
    }
    // ...
}
```

After:
```go
func (r *Router) selectEndpoint(ctx context.Context, targetNamespace string, span opentracing.Span) (*discovery.Endpoint, error) {
    lb := r.getLoadBalancer(targetNamespace)
    if lb == nil {
        r.metrics.IncrementRoutingErrors("no_endpoints")
        err := NewNoEndpointsError(targetNamespace)
        ext.Error.Set(span, true)
        span.LogKV("error", err.Error(), "code", err.Context["code"])
        return nil, err
    }
    // ...
}
```

### Backend Error Handling

Before:
```go
func (b *Backend) Connect(ctx context.Context) error {
    conn, err := b.dial(ctx)
    if err != nil {
        return fmt.Errorf("failed to connect: %w", err)
    }
    // ...
}
```

After:
```go
func (b *Backend) Connect(ctx context.Context) error {
    conn, err := b.dial(ctx)
    if err != nil {
        return CreateConnectionError(b.name, b.protocol, "failed to establish connection", err).
            WithContext("endpoint", b.endpoint).
            WithContext("attempt", b.connectionAttempts)
    }
    // ...
}
```

### Session Error Handling

Before:
```go
func (m *Manager) CreateSession(claims *auth.Claims) (*Session, error) {
    id, err := generateSessionID()
    if err != nil {
        return nil, err
    }
    // ...
}
```

After:
```go
func (m *Manager) CreateSession(ctx context.Context, claims *auth.Claims) (*Session, error) {
    id, err := generateSessionID()
    if err != nil {
        return nil, errors.WrapContext(ctx, err, "failed to generate session ID").
            WithComponent("session_manager").
            WithOperation("create_session")
    }
    // ...
}
```

## Error Response Format

Errors are serialized to JSON with the following format:

```json
{
  "type": "UNAVAILABLE",
  "message": "no healthy endpoints available for namespace: production",
  "code": "ROUTER_NO_HEALTHY_ENDPOINTS",
  "component": "router",
  "operation": "select_endpoint",
  "context": {
    "namespace": "production",
    "request_id": "req-123",
    "user_id": "user-456",
    "trace_id": "trace-789"
  },
  "severity": "MEDIUM",
  "retryable": true,
  "http_status": 503
}
```

## Migration Strategy

1. **Phase 1**: Update critical paths (router, backends, auth)
2. **Phase 2**: Update session management and health checks
3. **Phase 3**: Update remaining components
4. **Phase 4**: Add comprehensive error metrics and logging

## Testing Error Handling

Always test error conditions:

```go
func TestRouterNoEndpoints(t *testing.T) {
    router := NewRouter()
    ctx := context.Background()
    ctx = errors.EnrichContext(ctx, "test-req-1", "test-user", "", "127.0.0.1")
    
    _, err := router.RouteRequest(ctx, req, "unknown-namespace")
    
    require.Error(t, err)
    
    var gatewayErr *errors.GatewayError
    require.True(t, errors.As(err, &gatewayErr))
    assert.Equal(t, errors.TypeUnavailable, gatewayErr.Type)
    assert.Equal(t, "ROUTER_NO_ENDPOINTS", gatewayErr.Context["code"])
    assert.Equal(t, 503, gatewayErr.HTTPStatus)
    assert.True(t, gatewayErr.Retryable)
}
```

## Logging Errors

Use structured logging with errors:

```go
if err != nil {
    gatewayErr := errors.WrapContext(ctx, err, "operation failed")
    
    logger.Error("Operation failed",
        zap.String("error_type", string(gatewayErr.Type)),
        zap.String("error_code", gatewayErr.Context["code"].(string)),
        zap.String("component", gatewayErr.Component),
        zap.String("operation", gatewayErr.Operation),
        zap.Bool("retryable", gatewayErr.Retryable),
        zap.Any("context", gatewayErr.Context),
        zap.Error(gatewayErr))
    
    return gatewayErr
}
```

## Metrics for Errors

Track errors by type and component:

```go
errorCounter.WithLabelValues(
    string(gatewayErr.Type),
    gatewayErr.Component,
    gatewayErr.Context["code"].(string),
).Inc()
```