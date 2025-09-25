// Package auth provides authentication and authorization functionality including
// JWT validation and per-message authentication.
package auth

import (
	"context"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap"
)

// MessageAuthenticator handles per-message authentication.
type MessageAuthenticator struct {
	provider    Provider
	logger      *zap.Logger
	cacheExpiry time.Duration
	cache       *tokenCache
}

// tokenCache provides a simple thread-safe cache for validated tokens.
type tokenCache struct {
	mu      sync.RWMutex
	entries map[string]*cacheEntry
}

type cacheEntry struct {
	claims    *Claims
	expiresAt time.Time
}

// CreateMessageLevelAuthenticator creates an authenticator for per-message authentication with caching.
func CreateMessageLevelAuthenticator(provider Provider, logger *zap.Logger, cacheSeconds int) *MessageAuthenticator {
	cache := &tokenCache{
		entries: make(map[string]*cacheEntry),
	}

	ma := &MessageAuthenticator{
		provider:    provider,
		logger:      logger,
		cacheExpiry: time.Duration(cacheSeconds) * time.Second,
		cache:       cache,
	}

	// Start cleanup goroutine if cache is enabled
	if cacheSeconds > 0 {
		go ma.cleanupCache()
	}

	return ma
}

// ValidateMessageToken validates a token from a per-message auth field.
func (ma *MessageAuthenticator) ValidateMessageToken(_ context.Context, token, sessionID string) error {
	if token == "" {
		return NewMissingTokenError()
	}

	// Check cache first
	if ma.tryGetFromCache(token) {
		return nil
	}

	// Validate token
	claims, err := ma.provider.ValidateToken(token)
	if err != nil {
		return NewInvalidTokenError(err.Error())
	}

	// Verify claims
	if err := ma.verifyClaims(claims, sessionID); err != nil {
		return err
	}

	// Cache the validated token
	ma.tryAddToCache(token, claims)

	return nil
}

// tryGetFromCache checks cache and returns true if valid cached entry found.
func (ma *MessageAuthenticator) tryGetFromCache(token string) bool {
	if ma.cacheExpiry <= 0 {
		return false
	}

	return ma.getFromCache(token) != nil
}

// verifyClaims verifies token claims including expiry and session ID.
func (ma *MessageAuthenticator) verifyClaims(claims *Claims, sessionID string) error {
	// Verify token is not expired
	if claims.ExpiresAt != nil && claims.ExpiresAt.Before(time.Now()) {
		return NewTokenExpiredError(time.Now())
	}

	// Verify session ID matches if present in claims
	if sessionID != "" && claims.Subject != "" && claims.Subject != sessionID {
		return NewInvalidClaimsError("session mismatch")
	}

	return nil
}

// tryAddToCache adds token to cache if caching is enabled.
func (ma *MessageAuthenticator) tryAddToCache(token string, claims *Claims) {
	if ma.cacheExpiry > 0 {
		ma.addToCache(token, claims)
	}
}

// getFromCache retrieves cached claims if still valid.
func (ma *MessageAuthenticator) getFromCache(token string) *Claims {
	ma.cache.mu.RLock()
	defer ma.cache.mu.RUnlock()

	if entry, ok := ma.cache.entries[token]; ok {
		if entry.expiresAt.After(time.Now()) {
			return entry.claims
		}
	}

	return nil
}

// addToCache adds validated claims to cache.
func (ma *MessageAuthenticator) addToCache(token string, claims *Claims) {
	ma.cache.mu.Lock()
	defer ma.cache.mu.Unlock()

	expiresAt := time.Now().Add(ma.cacheExpiry)
	// Use token expiry if sooner than cache expiry
	if claims.ExpiresAt != nil && claims.ExpiresAt.Before(expiresAt) {
		expiresAt = claims.ExpiresAt.Time
	}

	ma.cache.entries[token] = &cacheEntry{
		claims:    claims,
		expiresAt: expiresAt,
	}
}

// cleanupCache periodically removes expired entries.
func (ma *MessageAuthenticator) cleanupCache() {
	ticker := time.NewTicker(ma.cacheExpiry)
	defer ticker.Stop()

	for range ticker.C {
		ma.cache.mu.Lock()

		now := time.Now()
		for token, entry := range ma.cache.entries {
			if entry.expiresAt.Before(now) {
				delete(ma.cache.entries, token)
			}
		}

		ma.cache.mu.Unlock()
	}
}

// GenerateMessageToken generates a short-lived token for per-message auth.
func GenerateMessageToken(sessionID string, secret []byte, duration time.Duration) (string, error) {
	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   sessionID,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(duration)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	return token.SignedString(secret)
}
