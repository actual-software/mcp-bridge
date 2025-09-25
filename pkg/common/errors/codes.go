// Package errors provides standardized error handling for the MCP Bridge system.
package errors

import (
	"errors"
	"fmt"
	"net/http"
)

// Retry timeout constants (in seconds).
const (
	// ShortRetryTimeout for quick recovery scenarios.
	ShortRetryTimeout = 5
	// StandardRetryTimeout for normal error recovery.
	StandardRetryTimeout = 10
	// MediumRetryTimeout for rate limiting scenarios.
	MediumRetryTimeout = 30
	// LongRetryTimeout for heavy rate limiting.
	LongRetryTimeout = 60
	// ExtendedRetryTimeout for quota limits.
	ExtendedRetryTimeout = 300
	// MaxRetryTimeout for daily quota limits.
	MaxRetryTimeout = 3600
)

// ErrorCode represents a unique error code for the MCP Bridge system.
type ErrorCode string

// Categories: AUTH, CONN, PROTO, VAL, INT, RATE, SEC.
//

const (
	// Common errors (CMN_XXX_XXX) - 1000-1999.
	CMN_INT_UNKNOWN      ErrorCode = "CMN_INT_001" // Unknown internal error
	CMN_INT_PANIC        ErrorCode = "CMN_INT_002" // Panic recovery
	CMN_INT_TIMEOUT      ErrorCode = "CMN_INT_003" // Operation timeout
	CMN_INT_CONTEXT_CANC ErrorCode = "CMN_INT_004" // Context canceled
	CMN_INT_NOT_IMPL     ErrorCode = "CMN_INT_005" // Feature not implemented

	// Validation errors.

	CMN_VAL_INVALID_REQ ErrorCode = "CMN_VAL_001" // Invalid request format

	CMN_VAL_MISSING_FLD ErrorCode = "CMN_VAL_002" // Required field missing

	CMN_VAL_INVALID_TYPE ErrorCode = "CMN_VAL_003" // Invalid field type

	CMN_VAL_OUT_OF_RANGE ErrorCode = "CMN_VAL_004" // Value out of acceptable range

	CMN_VAL_PATTERN_FAIL ErrorCode = "CMN_VAL_005" // Pattern validation failed

	// Protocol errors.

	CMN_PROTO_INVALID_VER ErrorCode = "CMN_PROTO_001" // Invalid protocol version

	CMN_PROTO_PARSE_ERR ErrorCode = "CMN_PROTO_002" // Protocol parsing error

	CMN_PROTO_MARSHAL_ERR ErrorCode = "CMN_PROTO_003" // Protocol marshaling error

	CMN_PROTO_METHOD_UNK ErrorCode = "CMN_PROTO_004" // Unknown method

	CMN_PROTO_BATCH_ERR ErrorCode = "CMN_PROTO_005" // Batch request error

	// Authentication errors.
	GTW_AUTH_MISSING      ErrorCode = "GTW_AUTH_001" // Missing authentication
	GTW_AUTH_INVALID      ErrorCode = "GTW_AUTH_002" // Invalid credentials
	GTW_AUTH_EXPIRED      ErrorCode = "GTW_AUTH_003" // Expired token
	GTW_AUTH_INSUFFICIENT ErrorCode = "GTW_AUTH_004" // Insufficient permissions
	GTW_AUTH_REVOKED      ErrorCode = "GTW_AUTH_005" // Revoked credentials
	GTW_AUTH_METHOD_UNK   ErrorCode = "GTW_AUTH_006" // Unknown auth method
	GTW_AUTH_CERT_FAIL    ErrorCode = "GTW_AUTH_007" // Certificate validation failed
	GTW_AUTH_OAUTH_FAIL   ErrorCode = "GTW_AUTH_008" // OAuth2 flow failed

	// Connection errors.
	GTW_CONN_REFUSED      ErrorCode = "GTW_CONN_001" // Connection refused
	GTW_CONN_TIMEOUT      ErrorCode = "GTW_CONN_002" // Connection timeout
	GTW_CONN_CLOSED       ErrorCode = "GTW_CONN_003" // Connection closed
	GTW_CONN_LIMIT        ErrorCode = "GTW_CONN_004" // Connection limit reached
	GTW_CONN_TLS_FAIL     ErrorCode = "GTW_CONN_005" // TLS handshake failed
	GTW_CONN_UPGRADE_FAIL ErrorCode = "GTW_CONN_006" // WebSocket upgrade failed

	// Rate limiting errors.
	GTW_RATE_LIMIT_REQ   ErrorCode = "GTW_RATE_001" // Request rate limit exceeded
	GTW_RATE_LIMIT_CONN  ErrorCode = "GTW_RATE_002" // Connection rate limit exceeded
	GTW_RATE_LIMIT_BURST ErrorCode = "GTW_RATE_003" // Burst limit exceeded
	GTW_RATE_LIMIT_QUOTA ErrorCode = "GTW_RATE_004" // Quota exceeded

	// Security errors.
	GTW_SEC_BLOCKED_IP ErrorCode = "GTW_SEC_001" // IP address blocked
	GTW_SEC_BLOCKED_UA ErrorCode = "GTW_SEC_002" // User agent blocked
	GTW_SEC_MALICIOUS  ErrorCode = "GTW_SEC_003" // Malicious request detected
	GTW_SEC_CSRF_FAIL  ErrorCode = "GTW_SEC_004" // CSRF validation failed

	// Connection errors.
	RTR_CONN_NO_BACKEND   ErrorCode = "RTR_CONN_001" // No backend available
	RTR_CONN_BACKEND_ERR  ErrorCode = "RTR_CONN_002" // Backend connection error
	RTR_CONN_POOL_FULL    ErrorCode = "RTR_CONN_003" // Connection pool exhausted
	RTR_CONN_UNHEALTHY    ErrorCode = "RTR_CONN_004" // Backend unhealthy
	RTR_CONN_CIRCUIT_OPEN ErrorCode = "RTR_CONN_005" // Circuit breaker open

	// Routing errors.
	RTR_ROUTE_NO_MATCH ErrorCode = "RTR_ROUTE_001" // No matching route
	RTR_ROUTE_CONFLICT ErrorCode = "RTR_ROUTE_002" // Route conflict
	RTR_ROUTE_DISABLED ErrorCode = "RTR_ROUTE_003" // Route disabled
	RTR_ROUTE_REDIRECT ErrorCode = "RTR_ROUTE_004" // Route requires redirect

	// Protocol errors.
	RTR_PROTO_MISMATCH   ErrorCode = "RTR_PROTO_001" // Protocol version mismatch
	RTR_PROTO_TRANSFORM  ErrorCode = "RTR_PROTO_002" // Protocol transformation error
	RTR_PROTO_SIZE_LIMIT ErrorCode = "RTR_PROTO_003" // Message size limit exceeded
)

// ErrorInfo contains detailed information about an error.
type ErrorInfo struct {
	Code        ErrorCode   `json:"code"`
	Message     string      `json:"message"`
	Details     interface{} `json:"details,omitempty"`
	HTTPStatus  int         `json:"http_status"`
	Recoverable bool        `json:"recoverable"`
	RetryAfter  int         `json:"retry_after,omitempty"` // Seconds to wait before retry
}

// errorDefinitions maps error codes to their definitions.
var errorDefinitions = map[ErrorCode]ErrorInfo{
	// Common errors
	CMN_INT_UNKNOWN: {
		Code:        CMN_INT_UNKNOWN,
		Message:     "An unknown internal error occurred",
		Details:     nil, // No additional details
		HTTPStatus:  http.StatusInternalServerError,
		Recoverable: false,
		RetryAfter:  0, // No retry
	},
	CMN_INT_PANIC: {
		Code:        CMN_INT_PANIC,
		Message:     "A panic occurred and was recovered",
		HTTPStatus:  http.StatusInternalServerError,
		Recoverable: false,
		Details:     nil, // No additional details
		RetryAfter:  0,   // No retry
	},
	CMN_INT_TIMEOUT: {
		Code:        CMN_INT_TIMEOUT,
		Message:     "Operation timed out",
		Details:     nil, // No additional details
		HTTPStatus:  http.StatusGatewayTimeout,
		Recoverable: true,
		RetryAfter:  MediumRetryTimeout,
	},
	CMN_INT_CONTEXT_CANC: {
		Code:        CMN_INT_CONTEXT_CANC,
		Message:     "Request was canceled",
		Details:     nil, // No additional details
		HTTPStatus:  http.StatusRequestTimeout,
		Recoverable: true,
		RetryAfter:  0, // No retry for canceled requests
	},
	CMN_INT_NOT_IMPL: {
		Code:        CMN_INT_NOT_IMPL,
		Message:     "Feature not implemented",
		HTTPStatus:  http.StatusInternalServerError,
		Recoverable: false,
		Details:     nil, // No additional details
		RetryAfter:  0,   // No retry
	},

	// Validation errors
	CMN_VAL_INVALID_REQ: {
		Code:        CMN_VAL_INVALID_REQ,
		Message:     "Invalid request format",
		HTTPStatus:  http.StatusBadRequest,
		Recoverable: false,
		Details:     nil, // No additional details
		RetryAfter:  0,   // No retry
	},
	CMN_VAL_MISSING_FLD: {
		Code:        CMN_VAL_MISSING_FLD,
		Message:     "Required field missing",
		HTTPStatus:  http.StatusInternalServerError,
		Recoverable: false,
		Details:     nil, // No additional details
		RetryAfter:  0,   // No retry
	},
	CMN_VAL_INVALID_TYPE: {
		Code:        CMN_VAL_INVALID_TYPE,
		Message:     "Invalid field type",
		HTTPStatus:  http.StatusInternalServerError,
		Recoverable: false,
		Details:     nil, // No additional details
		RetryAfter:  0,   // No retry
	},
	CMN_VAL_OUT_OF_RANGE: {
		Code:        CMN_VAL_OUT_OF_RANGE,
		Message:     "Value out of acceptable range",
		HTTPStatus:  http.StatusInternalServerError,
		Recoverable: false,
		Details:     nil, // No additional details
		RetryAfter:  0,   // No retry
	},
	CMN_VAL_PATTERN_FAIL: {
		Code:        CMN_VAL_PATTERN_FAIL,
		Message:     "Pattern validation failed",
		HTTPStatus:  http.StatusInternalServerError,
		Recoverable: false,
		Details:     nil, // No additional details
		RetryAfter:  0,   // No retry
	},

	// Protocol errors
	CMN_PROTO_INVALID_VER: {
		Code:        CMN_PROTO_INVALID_VER,
		Message:     "Invalid protocol version",
		HTTPStatus:  http.StatusInternalServerError,
		Recoverable: false,
		Details:     nil, // No additional details
		RetryAfter:  0,   // No retry
	},
	CMN_PROTO_PARSE_ERR: {
		Code:        CMN_PROTO_PARSE_ERR,
		Message:     "Protocol parsing error",
		HTTPStatus:  http.StatusInternalServerError,
		Recoverable: false,
		Details:     nil, // No additional details
		RetryAfter:  0,   // No retry
	},
	CMN_PROTO_MARSHAL_ERR: {
		Code:        CMN_PROTO_MARSHAL_ERR,
		Message:     "Protocol marshaling error",
		HTTPStatus:  http.StatusInternalServerError,
		Recoverable: false,
		Details:     nil, // No additional details
		RetryAfter:  0,   // No retry
	},
	CMN_PROTO_METHOD_UNK: {
		Code:        CMN_PROTO_METHOD_UNK,
		Message:     "Unknown method",
		HTTPStatus:  http.StatusInternalServerError,
		Recoverable: false,
		Details:     nil, // No additional details
		RetryAfter:  0,   // No retry
	},
	CMN_PROTO_BATCH_ERR: {
		Code:        CMN_PROTO_BATCH_ERR,
		Message:     "Batch request error",
		HTTPStatus:  http.StatusInternalServerError,
		Recoverable: false,
		Details:     nil, // No additional details
		RetryAfter:  0,   // No retry
	},

	// Gateway authentication errors
	GTW_AUTH_MISSING: {
		Code:        GTW_AUTH_MISSING,
		Message:     "Missing authentication credentials",
		HTTPStatus:  http.StatusUnauthorized,
		Recoverable: false,
		Details:     nil, // No additional details
		RetryAfter:  0,   // No retry
	},
	GTW_AUTH_INVALID: {
		Code:        GTW_AUTH_INVALID,
		Message:     "Invalid authentication credentials",
		HTTPStatus:  http.StatusInternalServerError,
		Recoverable: false,
		Details:     nil, // No additional details
		RetryAfter:  0,   // No retry
	},
	GTW_AUTH_EXPIRED: {
		Code:        GTW_AUTH_EXPIRED,
		Message:     "Authentication token has expired",
		HTTPStatus:  http.StatusInternalServerError,
		Recoverable: false,
		Details:     nil, // No additional details
		RetryAfter:  0,   // No retry
	},
	GTW_AUTH_INSUFFICIENT: {
		Code:        GTW_AUTH_INSUFFICIENT,
		Message:     "Insufficient permissions for this operation",
		HTTPStatus:  http.StatusInternalServerError,
		Recoverable: false,
		Details:     nil, // No additional details
		RetryAfter:  0,   // No retry
	},
	GTW_AUTH_REVOKED: {
		Code:        GTW_AUTH_REVOKED,
		Message:     "Authentication credentials have been revoked",
		HTTPStatus:  http.StatusInternalServerError,
		Recoverable: false,
		Details:     nil, // No additional details
		RetryAfter:  0,   // No retry
	},
	GTW_AUTH_METHOD_UNK: {
		Code:        GTW_AUTH_METHOD_UNK,
		Message:     "Unknown authentication method",
		HTTPStatus:  http.StatusInternalServerError,
		Recoverable: false,
		Details:     nil, // No additional details
		RetryAfter:  0,   // No retry
	},
	GTW_AUTH_CERT_FAIL: {
		Code:        GTW_AUTH_CERT_FAIL,
		Message:     "Certificate validation failed",
		HTTPStatus:  http.StatusInternalServerError,
		Recoverable: false,
		Details:     nil, // No additional details
		RetryAfter:  0,   // No retry
	},
	GTW_AUTH_OAUTH_FAIL: {
		Code:        GTW_AUTH_OAUTH_FAIL,
		Message:     "OAuth2 authentication flow failed",
		HTTPStatus:  http.StatusInternalServerError,
		Recoverable: false,
		Details:     nil, // No additional details
		RetryAfter:  0,   // No retry
	},

	// Gateway connection errors
	GTW_CONN_REFUSED: {
		Code:        GTW_CONN_REFUSED,
		Message:     "Connection refused",
		Details:     nil, // No additional details
		HTTPStatus:  http.StatusServiceUnavailable,
		Recoverable: true,
		RetryAfter:  MediumRetryTimeout,
	},
	GTW_CONN_TIMEOUT: {
		Code:        GTW_CONN_TIMEOUT,
		Message:     "Connection timeout",
		Details:     nil, // No additional details
		HTTPStatus:  http.StatusGatewayTimeout,
		Recoverable: true,
		RetryAfter:  MediumRetryTimeout,
	},
	GTW_CONN_CLOSED: {
		Code:        GTW_CONN_CLOSED,
		Message:     "Connection closed unexpectedly",
		Details:     nil, // No additional details
		HTTPStatus:  http.StatusServiceUnavailable,
		Recoverable: true,
		RetryAfter:  ShortRetryTimeout,
	},
	GTW_CONN_LIMIT: {
		Code:        GTW_CONN_LIMIT,
		Message:     "Connection limit reached",
		Details:     nil, // No additional details
		HTTPStatus:  http.StatusServiceUnavailable,
		Recoverable: true,
		RetryAfter:  LongRetryTimeout,
	},
	GTW_CONN_TLS_FAIL: {
		Code:        GTW_CONN_TLS_FAIL,
		Message:     "TLS handshake failed",
		HTTPStatus:  http.StatusInternalServerError,
		Recoverable: false,
		Details:     nil, // No additional details
		RetryAfter:  0,   // No retry
	},
	GTW_CONN_UPGRADE_FAIL: {
		Code:        GTW_CONN_UPGRADE_FAIL,
		Message:     "WebSocket upgrade failed",
		HTTPStatus:  http.StatusInternalServerError,
		Recoverable: false,
		Details:     nil, // No additional details
		RetryAfter:  0,   // No retry
	},

	// Gateway rate limiting errors
	GTW_RATE_LIMIT_REQ: {
		Code:        GTW_RATE_LIMIT_REQ,
		Message:     "Request rate limit exceeded",
		Details:     nil, // No additional details
		HTTPStatus:  http.StatusTooManyRequests,
		Recoverable: true,
		RetryAfter:  LongRetryTimeout,
	},
	GTW_RATE_LIMIT_CONN: {
		Code:        GTW_RATE_LIMIT_CONN,
		Message:     "Connection rate limit exceeded",
		Details:     nil, // No additional details
		HTTPStatus:  http.StatusTooManyRequests,
		Recoverable: true,
		RetryAfter:  ExtendedRetryTimeout,
	},
	GTW_RATE_LIMIT_BURST: {
		Code:        GTW_RATE_LIMIT_BURST,
		Message:     "Burst limit exceeded",
		Details:     nil, // No additional details
		HTTPStatus:  http.StatusTooManyRequests,
		Recoverable: true,
		RetryAfter:  StandardRetryTimeout,
	},
	GTW_RATE_LIMIT_QUOTA: {
		Code:        GTW_RATE_LIMIT_QUOTA,
		Message:     "API quota exceeded",
		Details:     nil, // No additional details
		HTTPStatus:  http.StatusTooManyRequests,
		Recoverable: true,
		RetryAfter:  MaxRetryTimeout,
	},

	// Gateway security errors
	GTW_SEC_BLOCKED_IP: {
		Code:        GTW_SEC_BLOCKED_IP,
		Message:     "IP address has been blocked",
		HTTPStatus:  http.StatusInternalServerError,
		Recoverable: false,
		Details:     nil, // No additional details
		RetryAfter:  0,   // No retry
	},
	GTW_SEC_BLOCKED_UA: {
		Code:        GTW_SEC_BLOCKED_UA,
		Message:     "User agent has been blocked",
		HTTPStatus:  http.StatusInternalServerError,
		Recoverable: false,
		Details:     nil, // No additional details
		RetryAfter:  0,   // No retry
	},
	GTW_SEC_MALICIOUS: {
		Code:        GTW_SEC_MALICIOUS,
		Message:     "Malicious request pattern detected",
		HTTPStatus:  http.StatusInternalServerError,
		Recoverable: false,
		Details:     nil, // No additional details
		RetryAfter:  0,   // No retry
	},
	GTW_SEC_CSRF_FAIL: {
		Code:        GTW_SEC_CSRF_FAIL,
		Message:     "CSRF validation failed",
		HTTPStatus:  http.StatusInternalServerError,
		Recoverable: false,
		Details:     nil, // No additional details
		RetryAfter:  0,   // No retry
	},

	// Router connection errors
	RTR_CONN_NO_BACKEND: {
		Code:        RTR_CONN_NO_BACKEND,
		Message:     "No backend server available",
		Details:     nil, // No additional details
		HTTPStatus:  http.StatusServiceUnavailable,
		Recoverable: true,
		RetryAfter:  MediumRetryTimeout,
	},
	RTR_CONN_BACKEND_ERR: {
		Code:        RTR_CONN_BACKEND_ERR,
		Message:     "Backend server connection error",
		Details:     nil, // No additional details
		HTTPStatus:  http.StatusBadGateway,
		Recoverable: true,
		RetryAfter:  StandardRetryTimeout,
	},
	RTR_CONN_POOL_FULL: {
		Code:        RTR_CONN_POOL_FULL,
		Message:     "Connection pool exhausted",
		Details:     nil, // No additional details
		HTTPStatus:  http.StatusServiceUnavailable,
		Recoverable: true,
		RetryAfter:  MediumRetryTimeout,
	},
	RTR_CONN_UNHEALTHY: {
		Code:        RTR_CONN_UNHEALTHY,
		Message:     "Backend server is unhealthy",
		Details:     nil, // No additional details
		HTTPStatus:  http.StatusServiceUnavailable,
		Recoverable: true,
		RetryAfter:  LongRetryTimeout,
	},
	RTR_CONN_CIRCUIT_OPEN: {
		Code:        RTR_CONN_CIRCUIT_OPEN,
		Message:     "Circuit breaker is open",
		Details:     nil, // No additional details
		HTTPStatus:  http.StatusServiceUnavailable,
		Recoverable: true,
		RetryAfter:  MediumRetryTimeout,
	},

	// Router routing errors
	RTR_ROUTE_NO_MATCH: {
		Code:        RTR_ROUTE_NO_MATCH,
		Message:     "No matching route found",
		HTTPStatus:  http.StatusInternalServerError,
		Recoverable: false,
		Details:     nil, // No additional details
		RetryAfter:  0,   // No retry
	},
	RTR_ROUTE_CONFLICT: {
		Code:        RTR_ROUTE_CONFLICT,
		Message:     "Route configuration conflict",
		HTTPStatus:  http.StatusInternalServerError,
		Recoverable: false,
		Details:     nil, // No additional details
		RetryAfter:  0,   // No retry
	},
	RTR_ROUTE_DISABLED: {
		Code:        RTR_ROUTE_DISABLED,
		Message:     "Route has been disabled",
		HTTPStatus:  http.StatusInternalServerError,
		Recoverable: false,
		Details:     nil, // No additional details
		RetryAfter:  0,   // No retry
	},
	RTR_ROUTE_REDIRECT: {
		Code:        RTR_ROUTE_REDIRECT,
		Message:     "Route requires redirect",
		HTTPStatus:  http.StatusInternalServerError,
		Recoverable: false,
		Details:     nil, // No additional details
		RetryAfter:  0,   // No retry
	},

	// Router protocol errors
	RTR_PROTO_MISMATCH: {
		Code:        RTR_PROTO_MISMATCH,
		Message:     "Protocol version mismatch",
		HTTPStatus:  http.StatusInternalServerError,
		Recoverable: false,
		Details:     nil, // No additional details
		RetryAfter:  0,   // No retry
	},
	RTR_PROTO_TRANSFORM: {
		Code:        RTR_PROTO_TRANSFORM,
		Message:     "Protocol transformation error",
		HTTPStatus:  http.StatusInternalServerError,
		Recoverable: false,
		Details:     nil, // No additional details
		RetryAfter:  0,   // No retry
	},
	RTR_PROTO_SIZE_LIMIT: {
		Code:        RTR_PROTO_SIZE_LIMIT,
		Message:     "Message size limit exceeded",
		HTTPStatus:  http.StatusInternalServerError,
		Recoverable: false,
		Details:     nil, // No additional details
		RetryAfter:  0,   // No retry
	},
}

// GetErrorInfo returns the error information for a given error code.
func GetErrorInfo(code ErrorCode) (ErrorInfo, bool) {
	info, exists := errorDefinitions[code]

	return info, exists
}

// Error creates a new error with the given code and optional details.
func Error(code ErrorCode, details ...interface{}) error {
	info, exists := errorDefinitions[code]
	if !exists {
		info = errorDefinitions[CMN_INT_UNKNOWN]
	}

	if len(details) > 0 {
		info.Details = details[0]
	}

	return &MCPError{
		ErrorInfo: info,
	}
}

// MCPError represents an MCP system error.
type MCPError struct {
	ErrorInfo
}

// Error implements the error interface.
func (e *MCPError) Error() string {
	if e.Details != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Details)
	}

	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

// HasCode checks if the error matches the given code.
func (e *MCPError) HasCode(code ErrorCode) bool {
	return e.Code == code
}

// WithDetails returns a new error with additional details.
func (e *MCPError) WithDetails(details interface{}) *MCPError {
	newErr := *e
	newErr.Details = details

	return &newErr
}

// IsErrorCode checks if an error is an MCPError with the given code.
func IsErrorCode(err error, code ErrorCode) bool {
	var mcpErr *MCPError
	if errors.As(err, &mcpErr) {
		return mcpErr.HasCode(code)
	}

	return false
}
