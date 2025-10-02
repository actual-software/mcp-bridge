package auth

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/actual-software/mcp-bridge/services/gateway/internal/errors"
)

// Error codes for auth operations.
const (
	ErrCodeInvalidToken       = "AUTH_INVALID_TOKEN"       //nolint:gosec
	ErrCodeTokenExpired       = "AUTH_TOKEN_EXPIRED"       //nolint:gosec
	ErrCodeMissingToken       = "AUTH_MISSING_TOKEN"       //nolint:gosec
	ErrCodeInvalidCredentials = "AUTH_INVALID_CREDENTIALS" //nolint:gosec
	ErrCodeInvalidScopes      = "AUTH_INVALID_SCOPES"
	ErrCodeRateLimitExceeded  = "AUTH_RATE_LIMIT_EXCEEDED"
	ErrCodeProviderError      = "AUTH_PROVIDER_ERROR"
	ErrCodeKeyNotFound        = "AUTH_KEY_NOT_FOUND"
	ErrCodeInvalidSignature   = "AUTH_INVALID_SIGNATURE"
	ErrCodeInvalidClaims      = "AUTH_INVALID_CLAIMS"
)

// contextKey is a type for auth context keys.
type contextKey string

// Context keys for auth-specific values.
const (
	contextKeyAuthScopes contextKey = "auth.scopes"
)

// NewInvalidTokenError creates an error for invalid tokens.
func NewInvalidTokenError(reason string) *errors.GatewayError {
	return errors.New(errors.TypeUnauthorized, "invalid token: "+reason).
		WithComponent("auth").
		WithContext("code", ErrCodeInvalidToken).
		WithHTTPStatus(http.StatusUnauthorized)
}

// NewTokenExpiredError creates an error for expired tokens.
func NewTokenExpiredError(expiredAt time.Time) *errors.GatewayError {
	return errors.New(errors.TypeUnauthorized, "token has expired").
		WithComponent("auth").
		WithContext("code", ErrCodeTokenExpired).
		WithContext("expired_at", expiredAt).
		WithHTTPStatus(http.StatusUnauthorized)
}

// NewMissingTokenError creates an error for missing tokens.
func NewMissingTokenError() *errors.GatewayError {
	return errors.New(errors.TypeUnauthorized, "authentication token is required").
		WithComponent("auth").
		WithContext("code", ErrCodeMissingToken).
		WithHTTPStatus(http.StatusUnauthorized)
}

// NewInvalidCredentialsError creates an error for invalid credentials.
func NewInvalidCredentialsError() *errors.GatewayError {
	return errors.New(errors.TypeUnauthorized, "invalid credentials").
		WithComponent("auth").
		WithContext("code", ErrCodeInvalidCredentials).
		WithHTTPStatus(http.StatusUnauthorized)
}

// NewInvalidScopesError creates an error for invalid scopes.
func NewInvalidScopesError(required []string, provided []string) *errors.GatewayError {
	return errors.New(errors.TypeForbidden, "insufficient scopes").
		WithComponent("auth").
		WithContext("code", ErrCodeInvalidScopes).
		WithContext("required_scopes", required).
		WithContext("provided_scopes", provided).
		WithHTTPStatus(http.StatusForbidden)
}

// NewRateLimitError creates an error for rate limit exceeded.
func NewRateLimitError(limit int, window time.Duration) *errors.GatewayError {
	return errors.New(errors.TypeRateLimit, "rate limit exceeded").
		WithComponent("auth").
		WithContext("code", ErrCodeRateLimitExceeded).
		WithContext("limit", limit).
		WithContext("window", window.String()).
		WithHTTPStatus(http.StatusTooManyRequests)
}

// NewProviderError creates an error for auth provider failures.
func NewProviderError(provider string, err error) *errors.GatewayError {
	return errors.WrapWithType(err, errors.TypeInternal, fmt.Sprintf("auth provider %s failed", provider)).
		WithComponent("auth").
		WithContext("code", ErrCodeProviderError).
		WithContext("provider", provider).
		WithHTTPStatus(http.StatusInternalServerError)
}

// NewKeyNotFoundError creates an error for missing signing keys.
func NewKeyNotFoundError(keyID string) *errors.GatewayError {
	return errors.New(errors.TypeInternal, "signing key not found: "+keyID).
		WithComponent("auth").
		WithContext("code", ErrCodeKeyNotFound).
		WithContext("key_id", keyID).
		WithHTTPStatus(http.StatusInternalServerError)
}

// NewInvalidSignatureError creates an error for invalid signatures.
func NewInvalidSignatureError(err error) *errors.GatewayError {
	return errors.WrapWithType(err, errors.TypeUnauthorized, "invalid signature").
		WithComponent("auth").
		WithContext("code", ErrCodeInvalidSignature).
		WithHTTPStatus(http.StatusUnauthorized)
}

// NewInvalidClaimsError creates an error for invalid token claims.
func NewInvalidClaimsError(claim string) *errors.GatewayError {
	return errors.New(errors.TypeUnauthorized, "invalid claim: "+claim).
		WithComponent("auth").
		WithContext("code", ErrCodeInvalidClaims).
		WithContext("invalid_claim", claim).
		WithHTTPStatus(http.StatusUnauthorized)
}

// WrapAuthError wraps an auth error with context.
func WrapAuthError(ctx context.Context, err error, operation string) *errors.GatewayError {
	return errors.WrapContext(ctx, err, "auth operation failed: "+operation).
		WithComponent("auth").
		WithOperation(operation)
}

// EnrichContextWithAuth adds auth information to context.
func EnrichContextWithAuth(ctx context.Context, userID, scopes string) context.Context {
	if userID != "" {
		ctx = context.WithValue(ctx, errors.ContextKeyUserID, userID)
	}

	if scopes != "" {
		ctx = context.WithValue(ctx, contextKeyAuthScopes, scopes)
	}

	return ctx
}
