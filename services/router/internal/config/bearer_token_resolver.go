package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/actual-software/mcp-bridge/pkg/common/config"
	"github.com/actual-software/mcp-bridge/services/router/internal/secure"
)

// BearerTokenResolver handles bearer token resolution from various sources.
type BearerTokenResolver struct {
	store         secure.TokenStore
	auth          *config.AuthConfig
	endpointIndex int
}

// InitializeBearerTokenResolver creates a new bearer token resolver.
func InitializeBearerTokenResolver(
	store secure.TokenStore,
	auth *config.AuthConfig,
	endpointIndex int,
) *BearerTokenResolver {
	return &BearerTokenResolver{
		store:         store,
		auth:          auth,
		endpointIndex: endpointIndex,
	}
}

// ResolveToken resolves a bearer token from available sources.
func (r *BearerTokenResolver) ResolveToken() (string, error) {
	// Try secure storage first (highest priority).
	if token := r.tryLoadFromSecureStorage(); token != "" {
		return token, nil
	}

	// Try default environment variable.
	if token := r.tryLoadFromDefaultEnvironment(); token != "" {
		return token, nil
	}

	// Try configured environment variable.
	if token := r.tryLoadFromConfiguredEnvironment(); token != "" {
		return token, nil
	}

	// Try token file (lowest priority).
	if token, err := r.tryLoadFromFile(); err == nil && token != "" {
		return token, nil
	}

	return "", errors.New("no auth token found (check token_secure_key, MCP_AUTH_TOKEN env var, or token_file config)")
}

// tryLoadFromSecureStorage attempts to load token from secure store.
func (r *BearerTokenResolver) tryLoadFromSecureStorage() string {
	if r.store == nil || r.auth.TokenSecureKey == "" {
		return ""
	}

	token, err := r.store.Retrieve(r.auth.TokenSecureKey)
	if err != nil {
		return ""
	}

	return token
}

// tryLoadFromDefaultEnvironment loads from MCP_AUTH_TOKEN environment variable.
func (r *BearerTokenResolver) tryLoadFromDefaultEnvironment() string {
	token := os.Getenv("MCP_AUTH_TOKEN")
	if token == "" {
		return ""
	}

	// Store for future use with endpoint-specific key.
	r.storeTokenWithDefaultKey(token)

	return token
}

// storeTokenWithDefaultKey stores token with a generated key if needed.
func (r *BearerTokenResolver) storeTokenWithDefaultKey(token string) {
	if r.store == nil || r.auth.TokenSecureKey != "" {
		return
	}

	r.auth.TokenSecureKey = fmt.Sprintf("endpoint-%d-bearer-token", r.endpointIndex)
	_ = r.store.Store(r.auth.TokenSecureKey, token) // Best effort storage
}

// tryLoadFromConfiguredEnvironment loads from configured environment variable.
func (r *BearerTokenResolver) tryLoadFromConfiguredEnvironment() string {
	if r.auth.TokenEnv == "" {
		return ""
	}

	token := os.Getenv(r.auth.TokenEnv)
	if token == "" {
		return ""
	}

	r.persistTokenToSecureStorage(token)

	return token
}

// tryLoadFromFile loads token from a file.
func (r *BearerTokenResolver) tryLoadFromFile() (string, error) {
	if r.auth.TokenFile == "" {
		return "", nil
	}

	tokenPath := expandPath(r.auth.TokenFile)

	data, err := os.ReadFile(filepath.Clean(tokenPath))
	if err != nil {
		return "", fmt.Errorf("failed to read token file: %w", err)
	}

	token := strings.TrimSpace(string(data))
	r.persistTokenToSecureStorage(token)

	return token, nil
}

// persistTokenToSecureStorage saves token to secure storage if available.
func (r *BearerTokenResolver) persistTokenToSecureStorage(token string) {
	if r.store != nil && r.auth.TokenSecureKey != "" && token != "" {
		_ = r.store.Store(r.auth.TokenSecureKey, token) // Best effort storage
	}
}
