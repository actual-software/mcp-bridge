package errors

import (
	"fmt"
	"net/http"
)

// Error codes for validation operations.
const (
	ErrCodeEmptyField         = "VALIDATION_EMPTY_FIELD"
	ErrCodeFieldTooLong       = "VALIDATION_FIELD_TOO_LONG"
	ErrCodeInvalidUTF8        = "VALIDATION_INVALID_UTF8"
	ErrCodeInvalidCharacters  = "VALIDATION_INVALID_CHARACTERS"
	ErrCodeDirectoryTraversal = "VALIDATION_DIRECTORY_TRAVERSAL"
	ErrCodeInvalidIDType      = "VALIDATION_INVALID_ID_TYPE"
	ErrCodeInvalidURL         = "VALIDATION_INVALID_URL"
	ErrCodeInvalidScheme      = "VALIDATION_INVALID_SCHEME"
	ErrCodeMissingHost        = "VALIDATION_MISSING_HOST"
	ErrCodeInvalidHostname    = "VALIDATION_INVALID_HOSTNAME"
	ErrCodeInvalidIP          = "VALIDATION_INVALID_IP"
	ErrCodeInvalidToken       = "VALIDATION_INVALID_TOKEN"
	ErrCodeInvalidConfig      = "VALIDATION_INVALID_CONFIG"
)

// NewEmptyFieldError creates an error for empty required fields.
func NewEmptyFieldError(field string) *GatewayError {
	return New(TypeValidation, field+" cannot be empty").
		WithComponent("validation").
		WithContext("field", field).
		WithContext("code", ErrCodeEmptyField).
		WithHTTPStatus(http.StatusBadRequest)
}

// NewFieldTooLongError creates an error for fields exceeding max length.
func NewFieldTooLongError(field string, length, maxLength int) *GatewayError {
	return New(TypeValidation, fmt.Sprintf("%s too long: %d > %d", field, length, maxLength)).
		WithComponent("validation").
		WithContext("field", field).
		WithContext("length", length).
		WithContext("max_length", maxLength).
		WithContext("code", ErrCodeFieldTooLong).
		WithHTTPStatus(http.StatusBadRequest)
}

// NewInvalidUTF8Error creates an error for invalid UTF-8 in fields.
func NewInvalidUTF8Error(field string) *GatewayError {
	return New(TypeValidation, field+" contains invalid UTF-8").
		WithComponent("validation").
		WithContext("field", field).
		WithContext("code", ErrCodeInvalidUTF8).
		WithHTTPStatus(http.StatusBadRequest)
}

// NewInvalidCharactersError creates an error for invalid characters in fields.
func NewInvalidCharactersError(field, value string) *GatewayError {
	return New(TypeValidation, field+" contains invalid characters: "+value).
		WithComponent("validation").
		WithContext("field", field).
		WithContext("value", value).
		WithContext("code", ErrCodeInvalidCharacters).
		WithHTTPStatus(http.StatusBadRequest)
}

// NewDirectoryTraversalError creates an error for directory traversal attempts.
func NewDirectoryTraversalError(field string) *GatewayError {
	return New(TypeValidation, field+" contains directory traversal").
		WithComponent("validation").
		WithContext("field", field).
		WithContext("code", ErrCodeDirectoryTraversal).
		WithHTTPStatus(http.StatusBadRequest)
}

// NewInvalidIDTypeError creates an error for invalid ID types.
func NewInvalidIDTypeError(idType string) *GatewayError {
	return New(TypeValidation, "invalid ID type: "+idType).
		WithComponent("validation").
		WithContext("type", idType).
		WithContext("code", ErrCodeInvalidIDType).
		WithHTTPStatus(http.StatusBadRequest)
}

// NewInvalidURLError creates an error for invalid URLs.
func NewInvalidURLError(url string, reason string) *GatewayError {
	return New(TypeValidation, "invalid URL: "+reason).
		WithComponent("validation").
		WithContext("url", url).
		WithContext("reason", reason).
		WithContext("code", ErrCodeInvalidURL).
		WithHTTPStatus(http.StatusBadRequest)
}

// NewInvalidSchemeError creates an error for invalid URL schemes.
func NewInvalidSchemeError(scheme string) *GatewayError {
	return New(TypeValidation, "invalid URL scheme: "+scheme).
		WithComponent("validation").
		WithContext("scheme", scheme).
		WithContext("code", ErrCodeInvalidScheme).
		WithHTTPStatus(http.StatusBadRequest)
}

// NewMissingHostError creates an error for URLs missing a host.
func NewMissingHostError() *GatewayError {
	return New(TypeValidation, "URL missing host").
		WithComponent("validation").
		WithContext("code", ErrCodeMissingHost).
		WithHTTPStatus(http.StatusBadRequest)
}

// NewInvalidHostnameError creates an error for invalid hostnames.
func NewInvalidHostnameError(hostname string) *GatewayError {
	return New(TypeValidation, "invalid hostname: "+hostname).
		WithComponent("validation").
		WithContext("hostname", hostname).
		WithContext("code", ErrCodeInvalidHostname).
		WithHTTPStatus(http.StatusBadRequest)
}

// NewInvalidIPError creates an error for invalid IP addresses.
func NewInvalidIPError(ip string) *GatewayError {
	return New(TypeValidation, "invalid IP address: "+ip).
		WithComponent("validation").
		WithContext("ip", ip).
		WithContext("code", ErrCodeInvalidIP).
		WithHTTPStatus(http.StatusBadRequest)
}

// NewInvalidTokenError creates an error for invalid tokens.
func NewInvalidTokenError(reason string) *GatewayError {
	return New(TypeValidation, "token "+reason).
		WithComponent("validation").
		WithContext("reason", reason).
		WithContext("code", ErrCodeInvalidToken).
		WithHTTPStatus(http.StatusBadRequest)
}
