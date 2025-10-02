package session

import (
	"context"
	"net/http"
	"time"

	"github.com/actual-software/mcp-bridge/services/gateway/internal/errors"
)

// sessionContextKey is a type for session context keys.
type sessionContextKey string

// Context keys for session-specific values.
const (
	sessionContextKeyID sessionContextKey = "session.id"
)

// Error codes for session operations.
const (
	ErrCodeSessionNotFound       = "SESSION_NOT_FOUND"
	ErrCodeSessionExpired        = "SESSION_EXPIRED"
	ErrCodeSessionInvalid        = "SESSION_INVALID"
	ErrCodeSessionCreationFailed = "SESSION_CREATION_FAILED"
	ErrCodeSessionStoreFailed    = "SESSION_STORE_FAILED"
	ErrCodeSessionDeleteFailed   = "SESSION_DELETE_FAILED"
	ErrCodeSessionLimitExceeded  = "SESSION_LIMIT_EXCEEDED"
	ErrCodeSessionTokenInvalid   = "SESSION_TOKEN_INVALID"
)

// NewSessionNotFoundError creates an error for session not found.
func NewSessionNotFoundError(sessionID string) *errors.GatewayError {
	return errors.New(errors.TypeNotFound, "session not found").
		WithComponent("session").
		WithContext("session_id", sessionID).
		WithContext("code", ErrCodeSessionNotFound).
		WithHTTPStatus(http.StatusNotFound)
}

// NewSessionExpiredError creates an error for expired sessions.
func NewSessionExpiredError(sessionID string, expiredAt time.Time) *errors.GatewayError {
	return errors.New(errors.TypeUnauthorized, "session has expired").
		WithComponent("session").
		WithContext("session_id", sessionID).
		WithContext("expired_at", expiredAt).
		WithContext("code", ErrCodeSessionExpired).
		WithHTTPStatus(http.StatusUnauthorized)
}

// NewSessionInvalidError creates an error for invalid sessions.
func NewSessionInvalidError(reason string) *errors.GatewayError {
	return errors.New(errors.TypeUnauthorized, "invalid session: "+reason).
		WithComponent("session").
		WithContext("code", ErrCodeSessionInvalid).
		WithHTTPStatus(http.StatusUnauthorized)
}

// WrapSessionCreationError wraps an error that occurred during session creation.
func WrapSessionCreationError(ctx context.Context, err error, userID string) *errors.GatewayError {
	return errors.WrapContext(ctx, err, "failed to create session").
		WithComponent("session").
		WithOperation("create").
		WithContext("user_id", userID).
		WithContext("code", ErrCodeSessionCreationFailed)
}

// WrapSessionStoreError wraps an error that occurred during session storage.
func WrapSessionStoreError(ctx context.Context, err error, operation string, sessionID string) *errors.GatewayError {
	return errors.WrapContextf(ctx, err, "session store %s failed", operation).
		WithComponent("session").
		WithOperation("store_"+operation).
		WithContext("session_id", sessionID).
		WithContext("code", ErrCodeSessionStoreFailed)
}

// WrapSessionDeleteError wraps an error that occurred during session deletion.
func WrapSessionDeleteError(ctx context.Context, err error, sessionID string) *errors.GatewayError {
	return errors.WrapContext(ctx, err, "failed to delete session").
		WithComponent("session").
		WithOperation("delete").
		WithContext("session_id", sessionID).
		WithContext("code", ErrCodeSessionDeleteFailed)
}

// NewSessionLimitExceededError creates an error when session limit is exceeded.
func NewSessionLimitExceededError(userID string, limit int) *errors.GatewayError {
	return errors.New(errors.TypeRateLimit, "session limit exceeded").
		WithComponent("session").
		WithContext("user_id", userID).
		WithContext("limit", limit).
		WithContext("code", ErrCodeSessionLimitExceeded).
		WithHTTPStatus(http.StatusTooManyRequests)
}

// NewSessionTokenInvalidError creates an error for invalid session tokens.
func NewSessionTokenInvalidError(reason string) *errors.GatewayError {
	return errors.New(errors.TypeUnauthorized, "invalid session token: "+reason).
		WithComponent("session").
		WithContext("code", ErrCodeSessionTokenInvalid).
		WithHTTPStatus(http.StatusUnauthorized)
}

// EnrichContextWithSession adds session information to context for error tracking.
func EnrichContextWithSession(ctx context.Context, sessionID, userID string) context.Context {
	if sessionID != "" {
		ctx = context.WithValue(ctx, sessionContextKeyID, sessionID)
	}

	if userID != "" {
		ctx = context.WithValue(ctx, errors.ContextKeyUserID, userID)
	}

	return ctx
}
