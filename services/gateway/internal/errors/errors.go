// Package errors provides comprehensive error handling utilities for the MCP Gateway.
// It includes error wrapping, classification, and context management.
package errors

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"runtime"
	"strings"
)

// ErrorType represents the category of an error.
type ErrorType string

const (
	// Stack capture configuration.
	stackSkipFrames = 2  // Number of stack frames to skip when capturing
	maxStackDepth   = 10 // Maximum stack depth to capture

	// Error types for classification.
	TypeValidation   ErrorType = "VALIDATION"
	TypeNotFound     ErrorType = "NOT_FOUND"
	TypeUnauthorized ErrorType = "UNAUTHORIZED"
	TypeForbidden    ErrorType = "FORBIDDEN"
	TypeInternal     ErrorType = "INTERNAL"
	TypeTimeout      ErrorType = "TIMEOUT"
	TypeCanceled     ErrorType = "CANCELED"
	TypeRateLimit    ErrorType = "RATE_LIMIT"
	TypeConflict     ErrorType = "CONFLICT"
	TypeUnavailable  ErrorType = "UNAVAILABLE"
)

// Error severity levels.
type Severity string

const (
	SeverityLow      Severity = "LOW"
	SeverityMedium   Severity = "MEDIUM"
	SeverityHigh     Severity = "HIGH"
	SeverityCritical Severity = "CRITICAL"
)

// GatewayError is the base error type for all gateway errors.
type GatewayError struct {
	Type       ErrorType              `json:"type"`
	Message    string                 `json:"message"`
	Code       string                 `json:"code,omitempty"`
	Cause      error                  `json:"-"`
	Context    map[string]interface{} `json:"context,omitempty"`
	Stack      []string               `json:"stack,omitempty"`
	Severity   Severity               `json:"severity"`
	Retryable  bool                   `json:"retryable"`
	HTTPStatus int                    `json:"http_status,omitempty"`
	Component  string                 `json:"component,omitempty"`
	Operation  string                 `json:"operation,omitempty"`
}

// Error implements the error interface.
func (e *GatewayError) Error() string {
	var b strings.Builder

	if e.Component != "" {
		b.WriteString("[")
		b.WriteString(e.Component)
		b.WriteString("] ")
	}

	if e.Operation != "" {
		b.WriteString(e.Operation)
		b.WriteString(": ")
	}

	b.WriteString(string(e.Type))
	b.WriteString(": ")
	b.WriteString(e.Message)

	if e.Cause != nil {
		b.WriteString(": ")
		b.WriteString(e.Cause.Error())
	}

	return b.String()
}

// Unwrap returns the underlying cause of the error.
func (e *GatewayError) Unwrap() error {
	return e.Cause
}

// Is implements error matching for errors.Is.
func (e *GatewayError) Is(target error) bool {
	t, ok := target.(*GatewayError)
	if !ok {
		return false
	}

	return e.Type == t.Type && e.Code == t.Code
}

// WithContext adds context information to the error.
func (e *GatewayError) WithContext(key string, value interface{}) *GatewayError {
	if e.Context == nil {
		e.Context = make(map[string]interface{})
	}

	e.Context[key] = value

	return e
}

// WithOperation sets the operation that caused the error.
func (e *GatewayError) WithOperation(operation string) *GatewayError {
	e.Operation = operation

	return e
}

// WithComponent sets the component that generated the error.
func (e *GatewayError) WithComponent(component string) *GatewayError {
	e.Component = component

	return e
}

// WithHTTPStatus sets the HTTP status code for the error.
func (e *GatewayError) WithHTTPStatus(status int) *GatewayError {
	e.HTTPStatus = status

	return e
}

// AsRetryable marks the error as retryable.
func (e *GatewayError) AsRetryable() *GatewayError {
	e.Retryable = true

	return e
}

// New creates a new GatewayError with stack trace.
func New(errType ErrorType, message string) *GatewayError {
	return &GatewayError{
		Type:      errType,
		Message:   message,
		Stack:     captureStack(stackSkipFrames),
		Severity:  getSeverityForType(errType),
		Retryable: isRetryableType(errType),
	}
}

// Wrap wraps an existing error with additional context.
func Wrap(err error, message string) *GatewayError {
	if err == nil {
		return nil
	}

	// If it's already a GatewayError, preserve its properties
	var ge *GatewayError
	if errors.As(err, &ge) {
		return &GatewayError{
			Type:       ge.Type,
			Message:    message,
			Cause:      ge,
			Context:    ge.Context,
			Stack:      captureStack(stackSkipFrames),
			Severity:   ge.Severity,
			Retryable:  ge.Retryable,
			HTTPStatus: ge.HTTPStatus,
			Component:  ge.Component,
			Operation:  ge.Operation,
		}
	}

	// Otherwise, create a new internal error
	return &GatewayError{
		Type:      TypeInternal,
		Message:   message,
		Cause:     err,
		Stack:     captureStack(stackSkipFrames),
		Severity:  SeverityMedium,
		Retryable: false,
	}
}

// WrapWithType wraps an error with a specific type.
func WrapWithType(err error, errType ErrorType, message string) *GatewayError {
	if err == nil {
		return nil
	}

	return &GatewayError{
		Type:      errType,
		Message:   message,
		Cause:     err,
		Stack:     captureStack(stackSkipFrames),
		Severity:  getSeverityForType(errType),
		Retryable: isRetryableType(errType),
	}
}

// Wrapf wraps an error with formatted message.
func Wrapf(err error, format string, args ...interface{}) *GatewayError {
	if err == nil {
		return nil
	}

	return Wrap(err, fmt.Sprintf(format, args...))
}

// IsType checks if an error is of a specific type.
func IsType(err error, errType ErrorType) bool {
	var ge *GatewayError
	if errors.As(err, &ge) {
		return ge.Type == errType
	}

	return false
}

// IsRetryable checks if an error is retryable.
func IsRetryable(err error) bool {
	var ge *GatewayError
	if errors.As(err, &ge) {
		return ge.Retryable
	}

	// Check for standard retryable errors
	if errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, context.Canceled) {
		return true
	}

	// Check for temporary network errors
	if temp, ok := err.(interface{ Temporary() bool }); ok {
		return temp.Temporary()
	}

	return false
}

// getErrorTypeStatusMap returns a map of error types to HTTP status codes.
const (
	// HTTPStatusClientClosedRequest is the nginx convention for client closed request.
	HTTPStatusClientClosedRequest = 499
)

func getErrorTypeStatusMap() map[ErrorType]int {
	return map[ErrorType]int{
		TypeValidation:   http.StatusBadRequest,
		TypeUnauthorized: http.StatusUnauthorized,
		TypeForbidden:    http.StatusForbidden,
		TypeNotFound:     http.StatusNotFound,
		TypeConflict:     http.StatusConflict,
		TypeRateLimit:    http.StatusTooManyRequests,
		TypeTimeout:      http.StatusRequestTimeout,
		TypeUnavailable:  http.StatusServiceUnavailable,
		TypeCanceled:     HTTPStatusClientClosedRequest, // Client Closed Request (nginx convention)
		TypeInternal:     http.StatusInternalServerError,
	}
}

// GetHTTPStatus returns the appropriate HTTP status code for an error.
func GetHTTPStatus(err error) int {
	var ge *GatewayError
	if errors.As(err, &ge) && ge.HTTPStatus > 0 {
		return ge.HTTPStatus
	}

	// Get error type and look up status code
	statusMap := getErrorTypeStatusMap()
	for errType, status := range statusMap {
		if IsType(err, errType) {
			return status
		}
	}

	return http.StatusInternalServerError
}

// Helper functions.

func captureStack(skip int) []string {
	var stack []string

	for i := skip; i < skip+maxStackDepth; i++ {
		pc, file, line, ok := runtime.Caller(i)
		if !ok {
			break
		}

		fn := runtime.FuncForPC(pc)
		if fn != nil {
			stack = append(stack, fmt.Sprintf("%s:%d %s", file, line, fn.Name()))
		}
	}

	return stack
}

func getSeverityForType(errType ErrorType) Severity {
	switch errType {
	case TypeInternal:
		return SeverityHigh
	case TypeUnauthorized, TypeForbidden:
		return SeverityMedium
	case TypeValidation, TypeNotFound:
		return SeverityLow
	case TypeTimeout, TypeUnavailable:
		return SeverityMedium
	case TypeRateLimit:
		return SeverityLow
	case TypeCanceled:
		return SeverityLow
	case TypeConflict:
		return SeverityLow
	default:
		return SeverityMedium
	}
}

func isRetryableType(errType ErrorType) bool {
	switch errType {
	case TypeTimeout, TypeUnavailable, TypeRateLimit:
		return true
	case TypeValidation, TypeNotFound, TypeUnauthorized, TypeForbidden, TypeInternal, TypeCanceled, TypeConflict:
		return false
	default:
		return false
	}
}

// Convenience functions for creating common errors

func NewValidationError(message string) *GatewayError {
	return New(TypeValidation, message).WithHTTPStatus(http.StatusBadRequest)
}

func NewNotFoundError(resource string) *GatewayError {
	return New(TypeNotFound, resource + " not found").WithHTTPStatus(http.StatusNotFound)
}

func NewUnauthorizedError(message string) *GatewayError {
	return New(TypeUnauthorized, message).WithHTTPStatus(http.StatusUnauthorized)
}

func NewForbiddenError(message string) *GatewayError {
	return New(TypeForbidden, message).WithHTTPStatus(http.StatusForbidden)
}

func NewInternalError(message string) *GatewayError {
	return New(TypeInternal, message).WithHTTPStatus(http.StatusInternalServerError)
}

func NewTimeoutError(operation string, cause error) *GatewayError {
	if cause != nil {
		return WrapWithType(cause, TypeTimeout, "operation " + operation + " timed out").
			WithHTTPStatus(http.StatusRequestTimeout).
			WithOperation(operation)
	}
	// If no cause, create a new error
	return New(TypeTimeout, "operation " + operation + " timed out").
		WithHTTPStatus(http.StatusRequestTimeout).
		WithOperation(operation)
}

func NewRateLimitError(message string) *GatewayError {
	return New(TypeRateLimit, message).WithHTTPStatus(http.StatusTooManyRequests)
}

func NewUnavailableError(service string) *GatewayError {
	return New(TypeUnavailable, "service " + service + " is unavailable").WithHTTPStatus(http.StatusServiceUnavailable)
}

// Standard sentinel errors.
var (
	ErrInvalidInput       = NewValidationError("invalid input")
	ErrResourceNotFound   = NewNotFoundError("resource")
	ErrUnauthorized       = NewUnauthorizedError("unauthorized access")
	ErrForbidden          = NewForbiddenError("forbidden access")
	ErrInternal           = NewInternalError("internal server error")
	ErrServiceUnavailable = NewUnavailableError("service")
)
