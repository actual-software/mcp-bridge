package config

import (
	"fmt"
	"os"

	"github.com/poiley/mcp-bridge/services/router/internal/secure"
)

// CredentialResolver handles credential loading from various sources with proper fallback.
type CredentialResolver struct {
	store secure.TokenStore
}

// InitializeCredentialResolver creates a new credential resolver.
func InitializeCredentialResolver(store secure.TokenStore) *CredentialResolver {
	return &CredentialResolver{
		store: store,
	}
}

// ResolveCredential loads a credential from secure storage, environment, or defaults.
func (r *CredentialResolver) ResolveCredential(req *credentialRequest) {
	// Try secure storage first.
	if r.tryLoadFromSecureStorage(req) {
		return
	}

	// Try specific environment variable.
	if r.tryLoadFromEnvironment(req) {
		return
	}

	// Try default environment variable.
	r.tryLoadFromDefaultEnvironment(req)
}

// tryLoadFromSecureStorage attempts to load credential from secure store.
func (r *CredentialResolver) tryLoadFromSecureStorage(req *credentialRequest) bool {
	if r.store == nil || req.secureKey == "" {
		return false
	}

	value, err := r.store.Retrieve(req.secureKey)
	if err != nil {
		return false
	}

	*req.targetField = value

	return true
}

// tryLoadFromEnvironment attempts to load from specific environment variable.
func (r *CredentialResolver) tryLoadFromEnvironment(req *credentialRequest) bool {
	if *req.targetField != "" || req.envVar == "" {
		return false
	}

	value := os.Getenv(req.envVar)
	if value == "" {
		return false
	}

	*req.targetField = value
	r.persistToSecureStorage(req.secureKey, value)

	return true
}

// tryLoadFromDefaultEnvironment attempts to load from default environment variable.
func (r *CredentialResolver) tryLoadFromDefaultEnvironment(req *credentialRequest) bool {
	if *req.targetField != "" || req.defaultEnvVar == "" {
		return false
	}

	value := os.Getenv(req.defaultEnvVar)
	if value == "" {
		return false
	}

	*req.targetField = value

	// Determine storage key.
	storageKey := r.determineStorageKey(req)
	r.persistToSecureStorage(storageKey, value)

	return true
}

// determineStorageKey determines the secure storage key to use.
func (r *CredentialResolver) determineStorageKey(req *credentialRequest) string {
	if req.secureKey != "" {
		return req.secureKey
	}

	if req.defaultKeyFmt != "" {
		return fmt.Sprintf(req.defaultKeyFmt, req.endpointIndex)
	}

	return ""
}

// persistToSecureStorage saves credential to secure storage for future use.
func (r *CredentialResolver) persistToSecureStorage(key, value string) {
	if r.store != nil && key != "" && value != "" {
		_ = r.store.Store(key, value) // Best effort storage
	}
}
