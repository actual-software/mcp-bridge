package direct

import (
	"errors"
	"time"
)

// Common error types for direct clients.
var (
	// ErrClientNotConnected indicates the client is not connected.
	ErrClientNotConnected = errors.New("client not connected")

	// ErrClientAlreadyConnected indicates the client is already connected.
	ErrClientAlreadyConnected = errors.New("client already connected")

	// ErrProtocolNotSupported indicates the protocol is not supported.
	ErrProtocolNotSupported = errors.New("protocol not supported")

	// ErrConnectionFailed indicates a connection could not be established.
	ErrConnectionFailed = errors.New("connection failed")

	// ErrRequestTimeout indicates a request has timed out.
	ErrRequestTimeout = errors.New("request timeout")

	// ErrInvalidServerURL indicates the server URL is invalid.
	ErrInvalidServerURL = errors.New("invalid server URL")

	// ErrHealthCheckFailed indicates a health check has failed.
	ErrHealthCheckFailed = errors.New("health check failed")

	// ErrClientClosed indicates the client has been closed.
	ErrClientClosed = errors.New("client closed")
)

// DirectClientError represents a structured error from direct clients.
type DirectClientError struct {
	Type      string    `json:"type"`
	Message   string    `json:"message"`
	Cause     error     `json:"cause,omitempty"`
	ClientURL string    `json:"client_url"`
	Protocol  string    `json:"protocol"`
	Timestamp time.Time `json:"timestamp"`
	Retryable bool      `json:"retryable"`
}

func (e *DirectClientError) Error() string {
	if e.Cause != nil {
		return e.Message + ": " + e.Cause.Error()
	}

	return e.Message
}

func (e *DirectClientError) Unwrap() error {
	return e.Cause
}

// NewDirectClientError creates a new structured direct client error.
func NewDirectClientError(errorType, message, clientURL, protocol string, cause error) *DirectClientError {
	return &DirectClientError{
		Type:      errorType,
		Message:   message,
		Cause:     cause,
		ClientURL: clientURL,
		Protocol:  protocol,
		Timestamp: time.Now(),
		Retryable: isRetryableError(cause),
	}
}

// WithRetryable sets whether the error is retryable.
func (e *DirectClientError) WithRetryable(retryable bool) *DirectClientError {
	e.Retryable = retryable

	return e
}

// Error type constants.
const (
	ErrTypeConnection = "CONNECTION_ERROR"
	ErrTypeTimeout    = "TIMEOUT_ERROR"
	ErrTypeProtocol   = "PROTOCOL_ERROR"
	ErrTypeProcess    = "PROCESS_ERROR"
	ErrTypeConfig     = "CONFIG_ERROR"
	ErrTypeHealth     = "HEALTH_ERROR"
	ErrTypeRequest    = "REQUEST_ERROR"
	ErrTypeResponse   = "RESPONSE_ERROR"
	ErrTypeShutdown   = "SHUTDOWN_ERROR"
	ErrTypeDetection  = "DETECTION_ERROR"
)

// Helper function to determine if an error is retryable.
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Network and temporary errors are typically retryable.
	if errors.Is(err, ErrConnectionFailed) ||
		errors.Is(err, ErrRequestTimeout) ||
		errors.Is(err, ErrHealthCheckFailed) {
		return true
	}

	// Check if the error has a Temporary method (common for network errors).
	if temp, ok := err.(interface{ Temporary() bool }); ok {
		return temp.Temporary()
	}

	return false
}

// Convenience functions for creating common direct client errors.

func NewConnectionError(clientURL, protocol, message string, cause error) *DirectClientError {
	return NewDirectClientError(ErrTypeConnection, message, clientURL, protocol, cause).WithRetryable(true)
}

func NewTimeoutError(clientURL, protocol string, timeout time.Duration, cause error) *DirectClientError {
	message := "operation timed out"
	if timeout > 0 {
		message = "operation timed out after " + timeout.String()
	}

	return NewDirectClientError(ErrTypeTimeout, message, clientURL, protocol, cause).WithRetryable(true)
}

func NewProtocolError(clientURL, protocol, message string, cause error) *DirectClientError {
	return NewDirectClientError(ErrTypeProtocol, message, clientURL, protocol, cause).WithRetryable(false)
}

func NewProcessError(clientURL, protocol, message string, cause error) *DirectClientError {
	return NewDirectClientError(ErrTypeProcess, message, clientURL, protocol, cause).WithRetryable(false)
}

func NewConfigError(clientURL, protocol, message string, cause error) *DirectClientError {
	return NewDirectClientError(ErrTypeConfig, message, clientURL, protocol, cause).WithRetryable(false)
}

func NewHealthError(clientURL, protocol, message string, cause error) *DirectClientError {
	return NewDirectClientError(ErrTypeHealth, message, clientURL, protocol, cause).WithRetryable(true)
}

func NewRequestError(clientURL, protocol, message string, cause error) *DirectClientError {
	return NewDirectClientError(ErrTypeRequest, message, clientURL, protocol, cause).WithRetryable(true)
}

func NewResponseError(clientURL, protocol, message string, cause error) *DirectClientError {
	return NewDirectClientError(ErrTypeResponse, message, clientURL, protocol, cause).WithRetryable(false)
}

func NewShutdownError(clientURL, protocol, message string, cause error) *DirectClientError {
	return NewDirectClientError(ErrTypeShutdown, message, clientURL, protocol, cause).WithRetryable(false)
}

func NewDetectionError(clientURL, protocol, message string, cause error) *DirectClientError {
	return NewDirectClientError(ErrTypeDetection, message, clientURL, protocol, cause).WithRetryable(true)
}

// ProtocolDetectionResult represents the result of protocol detection.
type ProtocolDetectionResult struct {
	Protocol   ClientType `json:"protocol"`
	Confidence float64    `json:"confidence"` // 0.0 to 1.0
	Source     string     `json:"source"`     // How the protocol was detected
	Details    string     `json:"details"`    // Additional detection details
}

// ServerCandidate represents a server endpoint candidate for protocol detection.
type ServerCandidate struct {
	URL      string                 `json:"url"`
	Protocol ClientType             `json:"protocol"`
	Priority int                    `json:"priority"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// ConnectionState represents the state of a direct client connection.
type ConnectionState int

const (
	StateDisconnected ConnectionState = iota
	StateConnecting
	StateConnected
	StateHealthy
	StateUnhealthy
	StateClosing
	StateClosed
	StateError
)

func (s ConnectionState) String() string {
	switch s {
	case StateDisconnected:
		return "disconnected"
	case StateConnecting:
		return "connecting"
	case StateConnected:
		return "connected"
	case StateHealthy:
		return "healthy"
	case StateUnhealthy:
		return "unhealthy"
	case StateClosing:
		return "closing"
	case StateClosed:
		return "closed"
	case StateError:
		return "error"
	default:
		return "unknown"
	}
}

// DirectClientStatus represents the status of a direct client.

// DirectManagerStatus represents the status of the DirectClientManager.
type DirectManagerStatus struct {
	Running          bool                          `json:"running"`
	TotalClients     int                           `json:"total_clients"`
	HealthyClients   int                           `json:"healthy_clients"`
	UnhealthyClients int                           `json:"unhealthy_clients"`
	Clients          map[string]DirectClientStatus `json:"clients"`
	Metrics          ManagerMetrics                `json:"metrics"`
	Config           DirectConfig                  `json:"config"`
}
