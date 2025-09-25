# Error Handling System

This document describes the comprehensive error handling system implemented in the MCP Bridge project.

## Overview

The error handling system provides:
- Structured error codes with consistent format
- HTTP and gRPC error handling
- Retry logic with exponential backoff
- Circuit breaker pattern
- Comprehensive logging and observability
- JSON-RPC error mapping

## Error Code Structure

Error codes follow a structured format: `<SERVICE>_<CATEGORY>_<NUMBER>`

### Services
- `CMN` - Common/shared errors
- `GTW` - Gateway service errors  
- `RTR` - Router service errors

### Categories
- `INT` - Internal system errors
- `AUTH` - Authentication/authorization errors
- `CONN` - Connection errors
- `PROTO` - Protocol errors
- `VAL` - Validation errors
- `RATE` - Rate limiting errors
- `SEC` - Security errors
- `ROUTE` - Routing errors

## Error Categories

### Common Errors (CMN_XXX_XXX)

| Code | Description | HTTP Status | Recoverable |
|------|-------------|-------------|-------------|
| CMN_INT_001 | Unknown internal error | 500 | No |
| CMN_INT_002 | Panic recovery | 500 | No |
| CMN_INT_003 | Operation timeout | 504 | Yes (30s) |
| CMN_INT_004 | Context cancelled | 408 | Yes |
| CMN_INT_005 | Feature not implemented | 501 | No |
| CMN_VAL_001 | Invalid request format | 400 | No |
| CMN_VAL_002 | Required field missing | 400 | No |
| CMN_VAL_003 | Invalid field type | 400 | No |
| CMN_VAL_004 | Value out of range | 400 | No |
| CMN_VAL_005 | Pattern validation failed | 400 | No |
| CMN_PROTO_001 | Invalid protocol version | 400 | No |
| CMN_PROTO_002 | Protocol parsing error | 400 | No |
| CMN_PROTO_003 | Protocol marshaling error | 500 | No |
| CMN_PROTO_004 | Unknown method | 404 | No |
| CMN_PROTO_005 | Batch request error | 400 | No |

### Gateway Errors (GTW_XXX_XXX)

#### Authentication Errors
| Code | Description | HTTP Status | Recoverable |
|------|-------------|-------------|-------------|
| GTW_AUTH_001 | Missing authentication | 401 | No |
| GTW_AUTH_002 | Invalid credentials | 401 | No |
| GTW_AUTH_003 | Expired token | 401 | No |
| GTW_AUTH_004 | Insufficient permissions | 403 | No |
| GTW_AUTH_005 | Revoked credentials | 401 | No |
| GTW_AUTH_006 | Unknown auth method | 401 | No |
| GTW_AUTH_007 | Certificate validation failed | 401 | No |
| GTW_AUTH_008 | OAuth2 flow failed | 401 | No |

#### Connection Errors
| Code | Description | HTTP Status | Recoverable | Retry After |
|------|-------------|-------------|-------------|-------------|
| GTW_CONN_001 | Connection refused | 503 | Yes | 30s |
| GTW_CONN_002 | Connection timeout | 504 | Yes | 30s |
| GTW_CONN_003 | Connection closed | 503 | Yes | 5s |
| GTW_CONN_004 | Connection limit reached | 503 | Yes | 60s |
| GTW_CONN_005 | TLS handshake failed | 502 | No | - |
| GTW_CONN_006 | WebSocket upgrade failed | 400 | No | - |

#### Rate Limiting Errors
| Code | Description | HTTP Status | Recoverable | Retry After |
|------|-------------|-------------|-------------|-------------|
| GTW_RATE_001 | Request rate limit exceeded | 429 | Yes | 60s |
| GTW_RATE_002 | Connection rate limit exceeded | 429 | Yes | 300s |
| GTW_RATE_003 | Burst limit exceeded | 429 | Yes | 10s |
| GTW_RATE_004 | API quota exceeded | 429 | Yes | 3600s |

#### Security Errors
| Code | Description | HTTP Status | Recoverable |
|------|-------------|-------------|-------------|
| GTW_SEC_001 | IP address blocked | 403 | No |
| GTW_SEC_002 | User agent blocked | 403 | No |
| GTW_SEC_003 | Malicious request detected | 403 | No |
| GTW_SEC_004 | CSRF validation failed | 403 | No |

### Router Errors (RTR_XXX_XXX)

#### Connection Errors
| Code | Description | HTTP Status | Recoverable | Retry After |
|------|-------------|-------------|-------------|-------------|
| RTR_CONN_001 | No backend available | 503 | Yes | 30s |
| RTR_CONN_002 | Backend connection error | 502 | Yes | 10s |
| RTR_CONN_003 | Connection pool exhausted | 503 | Yes | 30s |
| RTR_CONN_004 | Backend unhealthy | 503 | Yes | 60s |
| RTR_CONN_005 | Circuit breaker open | 503 | Yes | 30s |

#### Routing Errors
| Code | Description | HTTP Status | Recoverable |
|------|-------------|-------------|-------------|
| RTR_ROUTE_001 | No matching route | 404 | No |
| RTR_ROUTE_002 | Route configuration conflict | 409 | No |
| RTR_ROUTE_003 | Route disabled | 503 | No |
| RTR_ROUTE_004 | Route requires redirect | 308 | No |

#### Protocol Errors
| Code | Description | HTTP Status | Recoverable |
|------|-------------|-------------|-------------|
| RTR_PROTO_001 | Protocol version mismatch | 400 | No |
| RTR_PROTO_002 | Protocol transformation error | 502 | No |
| RTR_PROTO_003 | Message size limit exceeded | 413 | No |

## Usage Examples

### Creating Errors

```go
package main

import (
    "github.com/poiley/mcp-bridge/pkg/common/errors"
)

func handleRequest() error {
    // Simple error
    return errors.Error(errors.GTW_AUTH_MISSING)
    
    // Error with details
    return errors.Error(errors.GTW_CONN_TIMEOUT, "Connection to backend failed after 30s")
    
    // Error with additional context
    err := errors.Error(errors.CMN_VAL_INVALID_REQ)
    return err.(*errors.MCPError).WithDetails("Invalid JSON structure")
}
```

### HTTP Error Handling

```go
package main

import (
    "net/http"
    "go.uber.org/zap"
    "github.com/poiley/mcp-bridge/pkg/common/errors"
)

func setupErrorHandling() {
    logger := zap.NewProduction()
    errorHandler := errors.NewErrorHandler(logger)
    
    // Use middleware
    middleware := errors.ErrorHandlerMiddleware(errorHandler)
    handler := middleware(http.HandlerFunc(myHandler))
    
    // Or handle errors directly
    http.HandleFunc("/api/test", func(w http.ResponseWriter, r *http.Request) {
        if err := someOperation(); err != nil {
            errorHandler.HandleError(w, r, err)
            return
        }
        // Success response
    })
}

func myHandler(w http.ResponseWriter, r *http.Request) {
    // Handler logic that may panic
    // Middleware will catch panics and convert to proper error responses
}
```

### JSON-RPC Error Handling

```go
func handleJSONRPCRequest(req *jsonrpc.Request) *jsonrpc.Response {
    if err := validateRequest(req); err != nil {
        errorHandler := errors.NewErrorHandler(logger)
        errorData := errorHandler.HandleJSONRPCError(err)
        
        return &jsonrpc.Response{
            Version: "2.0",
            ID:      req.ID,
            Error:   errorData,
        }
    }
    
    // Process request...
    return &jsonrpc.Response{
        Version: "2.0",
        ID:      req.ID,
        Result:  result,
    }
}
```

### Retry Logic

```go
package main

import (
    "context"
    "time"
    "go.uber.org/zap"
    "github.com/poiley/mcp-bridge/pkg/common/errors"
)

func withRetry() error {
    logger := zap.NewProduction()
    
    // Configure retry policy
    config := errors.RetryConfig{
        MaxAttempts:     3,
        InitialInterval: 1 * time.Second,
        MaxInterval:     30 * time.Second,
        Multiplier:      2.0,
        RandomizeFactor: 0.1,
    }
    
    policy := errors.NewExponentialBackoffPolicy(config, logger)
    retryManager := errors.NewRetryManager(policy, logger)
    
    // Execute operation with retry
    return retryManager.Execute(context.Background(), func(ctx context.Context) error {
        // Your operation here
        return connectToBackend()
    })
}
```

### Circuit Breaker

```go
func withCircuitBreaker() error {
    logger := zap.NewProduction()
    
    config := errors.RetryConfig{
        MaxAttempts: 5, // Failure threshold
        MaxInterval: 60 * time.Second, // Recovery timeout
    }
    
    policy := errors.NewCircuitBreakerPolicy(config, logger)
    retryManager := errors.NewRetryManager(policy, logger)
    
    return retryManager.Execute(context.Background(), func(ctx context.Context) error {
        result := callExternalService()
        
        // Record success/failure for circuit breaker
        if circuitPolicy, ok := policy.(*errors.CircuitBreakerPolicy); ok {
            if result.Success {
                circuitPolicy.RecordSuccess()
            } else {
                circuitPolicy.RecordFailure()
            }
        }
        
        return result.Error
    })
}
```

### Error Classification

```go
func classifyError(err error) {
    // Check if it's our error type
    if errors.IsErrorCode(err, errors.GTW_AUTH_MISSING) {
        // Handle missing authentication
        return
    }
    
    // Check if retryable
    if errors.IsRetryable(err) {
        // Schedule retry
        return
    }
    
    // Get HTTP status
    status := errors.GetHTTPStatus(err)
    if status >= 500 {
        // Server error - alert operations
        return
    }
    
    // Get error code for logging
    code := errors.GetErrorCode(err)
    logger.Error("Operation failed", zap.String("error_code", string(code)))
}
```

## Error Response Format

### HTTP/REST Errors

```json
{
  "error": {
    "code": "GTW_AUTH_001",
    "message": "Missing authentication credentials",
    "details": "Authorization header not provided",
    "retryable": false,
    "remediation": "Provide valid authentication credentials in the request header",
    "context": {
      "endpoint": "/api/v1/tools",
      "method": "POST"
    }
  },
  "request_id": "req-12345",
  "timestamp": "2024-01-15T10:30:00Z"
}
```

### JSON-RPC Errors

```json
{
  "jsonrpc": "2.0",
  "id": "req-12345",
  "error": {
    "code": -32602,
    "message": "Invalid request format",
    "data": {
      "code": "CMN_VAL_001",
      "retryable": false,
      "remediation": "Check the request parameters and try again",
      "timestamp": "2024-01-15T10:30:00Z",
      "details": "Missing required field: method"
    }
  }
}
```

## Best Practices

### 1. Use Appropriate Error Codes
- Choose the most specific error code that matches the failure condition
- Use common errors (CMN_*) for generic failures
- Use service-specific errors for domain-specific failures

### 2. Provide Context
```go
// Good - provides context
return errors.Error(errors.GTW_CONN_TIMEOUT, 
    fmt.Sprintf("Connection to %s timed out after %v", endpoint, timeout))

// Bad - no context
return errors.Error(errors.GTW_CONN_TIMEOUT)
```

### 3. Handle Retryable Errors
```go
if errors.IsRetryable(err) {
    // Implement retry logic
    time.Sleep(time.Duration(info.RetryAfter) * time.Second)
    return retry()
}
```

### 4. Log Errors Appropriately
```go
// Use structured logging with error context
logger.Error("Authentication failed",
    zap.String("error_code", string(errors.GetErrorCode(err))),
    zap.String("user_id", userID),
    zap.String("client_ip", clientIP),
    zap.Error(err),
)
```

### 5. Don't Expose Internal Details
```go
// Good - safe error for client
return errors.Error(errors.CMN_INT_UNKNOWN, "An unexpected error occurred")

// Bad - exposes internal details
return errors.Error(errors.CMN_INT_UNKNOWN, fmt.Sprintf("Database connection failed: %v", dbErr))
```

## Monitoring and Alerting

### Metrics to Track
- Error rate by code
- HTTP status code distribution
- Retry attempts and success rates
- Circuit breaker state changes
- Response times for error handling

### Example Prometheus Metrics
```go
var (
    errorCounter = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "mcp_errors_total",
            Help: "Total number of errors by code",
        },
        []string{"service", "error_code", "recoverable"},
    )
    
    retryCounter = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "mcp_retries_total", 
            Help: "Total number of retry attempts",
        },
        []string{"error_code", "attempt"},
    )
)
```

### Alerting Rules
```yaml
# High error rate
- alert: HighErrorRate
  expr: rate(mcp_errors_total[5m]) > 0.1
  for: 2m
  labels:
    severity: warning
  annotations:
    summary: "High error rate detected"
    
# Circuit breaker open
- alert: CircuitBreakerOpen
  expr: mcp_circuit_breaker_state == 1
  for: 1m
  labels:
    severity: critical
  annotations:
    summary: "Circuit breaker is open"
```

## Testing

### Unit Tests
```go
func TestErrorHandling(t *testing.T) {
    // Test error creation
    err := errors.Error(errors.GTW_AUTH_MISSING, "Token missing")
    assert.True(t, errors.IsErrorCode(err, errors.GTW_AUTH_MISSING))
    assert.False(t, errors.IsRetryable(err))
    
    // Test HTTP status mapping
    status := errors.GetHTTPStatus(err)
    assert.Equal(t, http.StatusUnauthorized, status)
}
```

### Integration Tests
```go
func TestErrorHandlerIntegration(t *testing.T) {
    handler := errors.NewErrorHandler(zaptest.NewLogger(t))
    
    w := httptest.NewRecorder()
    req := httptest.NewRequest("GET", "/test", nil)
    
    err := errors.Error(errors.GTW_AUTH_MISSING)
    handler.HandleError(w, req, err)
    
    assert.Equal(t, http.StatusUnauthorized, w.Code)
    assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
}
```

This error handling system provides comprehensive, structured error management across the entire MCP Bridge system, ensuring consistent error responses, proper retry behavior, and excellent observability.