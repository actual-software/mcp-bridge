package errors

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"go.uber.org/zap"
)

// contextKey is used as a key for storing values in context.
type contextKey string

const requestIDKey contextKey = "request_id"

// ErrorResponse represents the standardized error response format.
type ErrorResponse struct {
	Error     ErrorDetail `json:"error"`
	RequestID string      `json:"request_id,omitempty"`
	Timestamp string      `json:"timestamp"`
}

// ErrorDetail contains the error details.
type ErrorDetail struct {
	Code        string                 `json:"code"`
	Message     string                 `json:"message"`
	Details     string                 `json:"details,omitempty"`
	Retryable   bool                   `json:"retryable"`
	Remediation string                 `json:"remediation,omitempty"`
	Context     map[string]interface{} `json:"context,omitempty"`
}

// ErrorHandler provides centralized error handling.
type ErrorHandler struct {
	logger *zap.Logger
}

// NewErrorHandler creates a new error handler.
func NewErrorHandler(logger *zap.Logger) *ErrorHandler {
	return &ErrorHandler{
		logger: logger,
	}
}

// HandleError handles an error and writes an appropriate response.
func (h *ErrorHandler) HandleError(w http.ResponseWriter, r *http.Request, err error) {
	// Extract request ID and ensure we have an MCPError
	requestID := h.extractRequestID(r)
	mcpErr := h.ensureMCPError(err)

	// Log the error
	h.logError(r.Context(), mcpErr, requestID)

	// Create error response
	response := ErrorResponse{
		Error: ErrorDetail{
			Code:        string(mcpErr.Code),
			Message:     mcpErr.Message,
			Details:     h.formatDetails(mcpErr.Details),
			Retryable:   mcpErr.Recoverable,
			Remediation: "",  // No remediation info
			Context:     nil, // No additional context
		},
		RequestID: requestID,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	// Set headers and write response
	h.setResponseHeaders(w, mcpErr, requestID)
	w.WriteHeader(mcpErr.HTTPStatus)

	// Write response body
	if err := json.NewEncoder(w).Encode(response); err != nil {
		h.logger.Error("Failed to encode error response",
			zap.Error(err),
			zap.String("original_error", mcpErr.Error()),
		)
	}
}

// extractRequestID extracts the request ID from the HTTP request context.
func (h *ErrorHandler) extractRequestID(r *http.Request) string {
	if id := r.Context().Value(requestIDKey); id != nil {
		if reqID, ok := id.(string); ok {
			return reqID
		}
	}

	return ""
}

// ensureMCPError converts any error to an MCPError, wrapping if necessary.
func (h *ErrorHandler) ensureMCPError(err error) *MCPError {
	var mcpErr *MCPError
	if errors.As(err, &mcpErr) {
		return mcpErr
	}

	// Wrap unknown errors
	var wrappedErr *MCPError
	if errors.As(Error(CMN_INT_UNKNOWN, err.Error()), &wrappedErr) {
		return wrappedErr
	}

	// Fallback - should never happen
	return &MCPError{
		ErrorInfo: ErrorInfo{
			Code:        CMN_INT_UNKNOWN,
			Message:     "Unknown error",
			Details:     nil, // No additional details
			HTTPStatus:  http.StatusInternalServerError,
			Recoverable: false,
			RetryAfter:  0, // No retry
		},
	}
}

// formatDetails converts error details to a string representation.
func (h *ErrorHandler) formatDetails(details interface{}) string {
	if details == nil {
		return ""
	}

	if str, ok := details.(string); ok {
		return str
	}

	return fmt.Sprintf("%v", details)
}

// setResponseHeaders sets the appropriate HTTP headers for the error response.
func (h *ErrorHandler) setResponseHeaders(w http.ResponseWriter, mcpErr *MCPError, requestID string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Error-Code", string(mcpErr.Code))

	if requestID != "" {
		w.Header().Set("X-Request-ID", requestID)
	}

	if mcpErr.Recoverable && mcpErr.RetryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(mcpErr.RetryAfter))
	}
}

// HandleJSONRPCError handles an error for JSON-RPC responses.
func (h *ErrorHandler) HandleJSONRPCError(err error) map[string]interface{} {
	var mcpErr *MCPError
	if !errors.As(err, &mcpErr) {
		var wrappedErr *MCPError
		if errors.As(Error(CMN_INT_UNKNOWN, err.Error()), &wrappedErr) {
			mcpErr = wrappedErr
		} else {
			// Fallback - should never happen
			mcpErr = &MCPError{
				ErrorInfo: ErrorInfo{
					Code:        CMN_INT_UNKNOWN,
					Message:     "Unknown error",
					Details:     nil, // No additional details
					HTTPStatus:  http.StatusInternalServerError,
					Recoverable: false,
					RetryAfter:  0, // No retry
				},
			}
		}
	}

	// Map to JSON-RPC error codes
	jsonRPCCode := h.mapToJSONRPCCode(mcpErr.Code)

	errorData := map[string]interface{}{
		"code":      string(mcpErr.Code),
		"retryable": mcpErr.Recoverable,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}

	if mcpErr.Details != nil {
		errorData["details"] = mcpErr.Details
	}

	return map[string]interface{}{
		"code":    jsonRPCCode,
		"message": mcpErr.Message,
		"data":    errorData,
	}
}

// logError logs the error with appropriate level and context.
func (h *ErrorHandler) logError(_ context.Context, mcpErr *MCPError, requestID string) {
	fields := []zap.Field{
		zap.String("error_code", string(mcpErr.Code)),
		zap.String("error_message", mcpErr.Message),
		zap.Int("http_status", mcpErr.HTTPStatus),
		zap.Bool("retryable", mcpErr.Recoverable),
	}

	if requestID != "" {
		fields = append(fields, zap.String("request_id", requestID))
	}

	if mcpErr.Details != nil {
		fields = append(fields, zap.Any("details", mcpErr.Details))
	}

	// Log based on severity
	switch {
	case mcpErr.HTTPStatus >= http.StatusInternalServerError:
		h.logger.Error("Server error", fields...)
	case mcpErr.HTTPStatus >= http.StatusBadRequest:
		h.logger.Warn("Client error", fields...)
	default:
		h.logger.Info("Error handled", fields...)
	}
}

// mapToJSONRPCCode maps our error codes to JSON-RPC error codes.
//

func (h *ErrorHandler) mapToJSONRPCCode(code ErrorCode) int {
	switch code { 
	case CMN_PROTO_PARSE_ERR:
		return -32700 // Parse error
	case CMN_PROTO_METHOD_UNK:
		return -32601 // Method not found
	case CMN_VAL_INVALID_REQ, CMN_VAL_INVALID_TYPE, CMN_VAL_MISSING_FLD:
		return -32602 // Invalid params
	case CMN_INT_UNKNOWN, CMN_INT_PANIC:
		return -32603 // Internal error
	// All other errors map to application-defined error codes
	case CMN_INT_TIMEOUT, CMN_INT_CONTEXT_CANC, CMN_INT_NOT_IMPL,
		CMN_VAL_OUT_OF_RANGE, CMN_VAL_PATTERN_FAIL,
		CMN_PROTO_INVALID_VER, CMN_PROTO_MARSHAL_ERR, CMN_PROTO_BATCH_ERR,
		GTW_AUTH_MISSING, GTW_AUTH_INVALID, GTW_AUTH_EXPIRED, GTW_AUTH_INSUFFICIENT, GTW_AUTH_REVOKED,
		GTW_AUTH_METHOD_UNK, GTW_AUTH_CERT_FAIL, GTW_AUTH_OAUTH_FAIL,
		GTW_CONN_REFUSED, GTW_CONN_TIMEOUT, GTW_CONN_CLOSED, GTW_CONN_LIMIT, GTW_CONN_TLS_FAIL, GTW_CONN_UPGRADE_FAIL,
		GTW_RATE_LIMIT_REQ, GTW_RATE_LIMIT_CONN, GTW_RATE_LIMIT_BURST, GTW_RATE_LIMIT_QUOTA,
		GTW_SEC_BLOCKED_IP, GTW_SEC_BLOCKED_UA, GTW_SEC_MALICIOUS, GTW_SEC_CSRF_FAIL,
		RTR_CONN_NO_BACKEND, RTR_CONN_BACKEND_ERR, RTR_CONN_POOL_FULL, RTR_CONN_UNHEALTHY, RTR_CONN_CIRCUIT_OPEN,
		RTR_ROUTE_NO_MATCH, RTR_ROUTE_CONFLICT, RTR_ROUTE_DISABLED, RTR_ROUTE_REDIRECT,
		RTR_PROTO_MISMATCH, RTR_PROTO_TRANSFORM, RTR_PROTO_SIZE_LIMIT:
		// Application-defined errors
		return -32000
	}
	// This should never be reached since all error codes are handled above
	// But required for compilation since the exhaustive linter can't prove completeness to the compiler
	return -32603 // Internal error as fallback
}

// ErrorHandlerMiddleware creates HTTP middleware for error handling.
func ErrorHandlerMiddleware(handler *ErrorHandler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Create response writer wrapper to capture errors
			rw := &responseWriterWrapper{
				ResponseWriter: w,
				handler:        handler,
				request:        r,
				wroteHeader:    false, // Initialize header tracking
			}

			// Handle panics
			defer func() {
				if err := recover(); err != nil {
					handler.logger.Error("Panic recovered",
						zap.Any("panic", err),
						zap.String("path", r.URL.Path),
					)

					mcpErr := Error(CMN_INT_PANIC, "An unexpected error occurred")
					handler.HandleError(w, r, mcpErr)
				}
			}()

			next.ServeHTTP(rw, r)
		})
	}
}

// responseWriterWrapper wraps http.ResponseWriter to intercept errors.
type responseWriterWrapper struct {
	http.ResponseWriter
	handler     *ErrorHandler
	request     *http.Request
	wroteHeader bool
}

func (rw *responseWriterWrapper) WriteHeader(statusCode int) {
	if !rw.wroteHeader {
		rw.wroteHeader = true
		rw.ResponseWriter.WriteHeader(statusCode)
	}
}

func (rw *responseWriterWrapper) Write(b []byte) (int, error) {
	if !rw.wroteHeader {
		rw.WriteHeader(http.StatusOK)
	}

	return rw.ResponseWriter.Write(b)
}

// IsRetryable checks if an error is retryable.
func IsRetryable(err error) bool {
	var mcpErr *MCPError
	if errors.As(err, &mcpErr) {
		return mcpErr.Recoverable
	}

	return false
}

// GetHTTPStatus returns the HTTP status code for an error.
func GetHTTPStatus(err error) int {
	var mcpErr *MCPError
	if errors.As(err, &mcpErr) {
		return mcpErr.HTTPStatus
	}

	return http.StatusInternalServerError
}

// GetErrorCode extracts the error code from an error.
func GetErrorCode(err error) ErrorCode {
	var mcpErr *MCPError
	if errors.As(err, &mcpErr) {
		return mcpErr.Code
	}

	return CMN_INT_UNKNOWN
}
