// Package validation provides input validation functions for MCP request parameters and data.
package validation

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	customerrors "github.com/actual-software/mcp-bridge/services/gateway/internal/errors"
)

const (
	defaultRetryCount = 10
	// MaxNamespaceLength is the maximum allowed length for a namespace.
	MaxNamespaceLength = 255
	// MaxMethodLength is the maximum allowed length for a method name.
	MaxMethodLength = 255
	// MaxIDLength is the maximum allowed length for request IDs.
	MaxIDLength = 255
	// MaxTokenLength is the maximum allowed length for auth tokens.
	MaxTokenLength = 4096
	// MaxURLLength is the maximum allowed length for URLs.
	MaxURLLength = 2048
)

var (
	// namespaceRegex validates namespace format (alphanumeric, dots, hyphens, underscores).
	namespaceRegex = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)
	// methodRegex validates method names (alphanumeric, dots, slashes, hyphens, underscores).
	methodRegex = regexp.MustCompile(`^[a-zA-Z0-9._/-]+$`)
	// idRegex validates request IDs (alphanumeric, hyphens, underscores).
	idRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
)

// ValidateNamespace validates a namespace string.
func ValidateNamespace(namespace string) error {
	if namespace == "" {
		return customerrors.NewEmptyFieldError("namespace")
	}

	if len(namespace) > MaxNamespaceLength {
		return customerrors.NewFieldTooLongError("namespace", len(namespace), MaxNamespaceLength)
	}

	if !utf8.ValidString(namespace) {
		return customerrors.NewInvalidUTF8Error("namespace")
	}

	if !namespaceRegex.MatchString(namespace) {
		return customerrors.NewInvalidCharactersError("namespace", namespace)
	}

	// Prevent directory traversal
	if strings.Contains(namespace, "..") {
		return customerrors.NewDirectoryTraversalError("namespace")
	}

	return nil
}

// ValidateMethod validates a method name.
func ValidateMethod(method string) error {
	if method == "" {
		return customerrors.NewEmptyFieldError("method")
	}

	if len(method) > MaxMethodLength {
		return customerrors.NewFieldTooLongError("method", len(method), MaxMethodLength)
	}

	if !utf8.ValidString(method) {
		return customerrors.NewInvalidUTF8Error("method")
	}

	if !methodRegex.MatchString(method) {
		return customerrors.NewInvalidCharactersError("method", method)
	}

	// Prevent directory traversal
	if strings.Contains(method, "..") {
		return customerrors.NewDirectoryTraversalError("method")
	}

	return nil
}

// ValidateRequestID validates a request ID.
func ValidateRequestID(id interface{}) error {
	// Convert ID to string
	idStr, err := convertIDToString(id)
	if err != nil {
		return err
	}

	// Validate the string representation
	if err := validateIDString(idStr); err != nil {
		return err
	}

	// For string IDs, validate format
	if _, ok := id.(string); ok {
		return validateStringIDFormat(idStr)
	}

	return nil
}

// convertIDToString converts various ID types to string.
func convertIDToString(id interface{}) (string, error) {
	switch v := id.(type) {
	case string:
		return v, nil
	case float64:
		return fmt.Sprintf("%g", v), nil
	case int:
		return strconv.Itoa(v), nil
	case int64:
		return strconv.FormatInt(v, defaultRetryCount), nil
	default:
		return "", customerrors.NewInvalidIDTypeError(fmt.Sprintf("%T", id))
	}
}

// validateIDString validates the string representation of an ID.
func validateIDString(idStr string) error {
	if idStr == "" {
		return customerrors.NewEmptyFieldError("ID")
	}

	if len(idStr) > MaxIDLength {
		return customerrors.NewFieldTooLongError("ID", len(idStr), MaxIDLength)
	}

	if !utf8.ValidString(idStr) {
		return customerrors.NewInvalidUTF8Error("ID")
	}

	return nil
}

// validateStringIDFormat validates the format of string IDs.
func validateStringIDFormat(idStr string) error {
	if !idRegex.MatchString(idStr) {
		return customerrors.NewInvalidCharactersError("ID", idStr)
	}

	return nil
}

// ValidateToken validates an authentication token.
func ValidateToken(token string) error {
	if token == "" {
		return customerrors.NewEmptyFieldError("token")
	}

	if len(token) > MaxTokenLength {
		return customerrors.NewFieldTooLongError("token", len(token), MaxTokenLength)
	}

	if !utf8.ValidString(token) {
		return customerrors.NewInvalidUTF8Error("token")
	}

	// Check for common injection patterns
	if strings.ContainsAny(token, "\x00\r\n") {
		return customerrors.NewInvalidTokenError("contains invalid characters")
	}

	return nil
}

// ValidateURL validates a URL string.
func ValidateURL(urlStr string) error {
	if urlStr == "" {
		return customerrors.NewEmptyFieldError("URL")
	}

	if len(urlStr) > MaxURLLength {
		return customerrors.NewFieldTooLongError("URL", len(urlStr), MaxURLLength)
	}

	if !utf8.ValidString(urlStr) {
		return customerrors.NewInvalidUTF8Error("URL")
	}

	// Parse URL
	u, err := url.Parse(urlStr)
	if err != nil {
		return customerrors.NewInvalidURLError(urlStr, err.Error())
	}

	// Validate scheme
	switch u.Scheme {
	case "http", "https", "ws", "wss", "tcp", "tcps":
		// Valid schemes
	default:
		return customerrors.NewInvalidSchemeError(u.Scheme)
	}

	// Validate host
	if u.Host == "" {
		return customerrors.NewMissingHostError()
	}

	// Extract host without port
	host := u.Hostname()

	// Validate host format
	if net.ParseIP(host) == nil {
		// Not an IP, validate as hostname
		if !isValidHostname(host) {
			return customerrors.NewInvalidHostnameError(host)
		}
	}

	return nil
}

// ValidateIP validates an IP address string.
func ValidateIP(ip string) error {
	if ip == "" {
		return customerrors.NewEmptyFieldError("IP")
	}

	if net.ParseIP(ip) == nil {
		return customerrors.NewInvalidIPError(ip)
	}

	return nil
}

// isValidHostname checks if a hostname is valid.
func isValidHostname(hostname string) bool {
	if !isValidHostnameLength(hostname) {
		return false
	}

	// Check each label
	labels := strings.Split(hostname, ".")
	for _, label := range labels {
		if !isValidLabel(label) {
			return false
		}
	}

	return true
}

// isValidHostnameLength checks if hostname length is valid.
func isValidHostnameLength(hostname string) bool {
	return hostname != "" && len(hostname) <= 255
}

// isValidLabel checks if a DNS label is valid.
func isValidLabel(label string) bool {
	if len(label) == 0 || len(label) > 63 {
		return false
	}

	// Label must start and end with alphanumeric
	if !isAlphaNumeric(label[0]) || !isAlphaNumeric(label[len(label)-1]) {
		return false
	}

	// Label can contain alphanumeric and hyphens
	return isValidLabelContent(label)
}

// isValidLabelContent checks if label content is valid.
func isValidLabelContent(label string) bool {
	for _, ch := range label {
		if !isAlphaNumeric(byte(ch)) && ch != '-' {
			return false
		}
	}

	return true
}

// isAlphaNumeric checks if a byte is alphanumeric.
func isAlphaNumeric(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9')
}

// SanitizeString removes potentially dangerous characters from a string.
func SanitizeString(s string) string {
	// Remove null bytes and control characters
	s = strings.ReplaceAll(s, "\x00", "")

	// Replace newlines and carriage returns with spaces
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")

	// Trim whitespace
	s = strings.TrimSpace(s)

	return s
}
