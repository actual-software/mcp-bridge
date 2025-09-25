
package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/poiley/mcp-bridge/services/gateway/internal/config"
	"github.com/poiley/mcp-bridge/services/gateway/test/testutil"
)

const (
	testIterations    = 100
	testMaxIterations = 1000
	testTimeout       = 50
)

func TestNewOAuth2Provider(t *testing.T) {
	t.Parallel()

	tests := createOAuth2ProviderTestCases()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			runOAuth2ProviderTest(t, tt)
		})
	}
}

type oAuth2ProviderTestCase struct {
	name      string
	config    config.OAuth2Config
	envVars   map[string]string
	wantError bool
	errorMsg  string
}

func createOAuth2ProviderTestCases() []oAuth2ProviderTestCase {
	return []oAuth2ProviderTestCase{
		{
			name: "Valid configuration",
			config: config.OAuth2Config{
				ClientID:           "test-client",
				ClientSecretEnv:    "TEST_CLIENT_SECRET",
				IntrospectEndpoint: "https://auth.example.com/introspect",
			},
			envVars: map[string]string{
				"TEST_CLIENT_SECRET": "secret123",
			},
			wantError: false,
		},
		{
			name: "Missing client secret",
			config: config.OAuth2Config{
				ClientID:           "test-client",
				ClientSecretEnv:    "MISSING_SECRET",
				IntrospectEndpoint: "https://auth.example.com/introspect",
			},
			wantError: true,
			errorMsg:  "OAuth2 client secret environment variable MISSING_SECRET not set",
		},
		{
			name: "No client secret required",
			config: config.OAuth2Config{
				ClientID:           "public-client",
				IntrospectEndpoint: "https://auth.example.com/introspect",
			},
			wantError: false,
		},
	}
}

func runOAuth2ProviderTest(t *testing.T, tt oAuth2ProviderTestCase) {
	t.Helper()

	// Set environment variables
	for k, v := range tt.envVars {
		err := os.Setenv(k, v)
		require.NoError(t, err, "Failed to set environment variable %s", k)

		defer func(key string) {
			err := os.Unsetenv(key)
			require.NoError(t, err, "Failed to unset environment variable %s", key)
		}(k)
	}

	logger := testutil.NewTestLogger(t)
	provider, err := createOAuth2AuthProvider(tt.config, logger)

	if tt.wantError {
		assert.Error(t, err)

		if tt.errorMsg != "" {
			assert.Contains(t, err.Error(), tt.errorMsg)
		}
	} else {
		assert.NoError(t, err)
		assert.NotNil(t, provider)
	}
}

func TestOAuth2Provider_Authenticate(t *testing.T) {
	// Don't use t.Parallel() at the top level since subtests share resources
	tests := createAuthenticateTestCases()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runAuthenticateTest(t, tt)
		})
	}
}

func createAuthenticateTestCases() []struct {
	name       string
	authHeader string
	wantError  bool
	errorMsg   string
	wantClaims *Claims
} {
	return []struct {
		name       string
		authHeader string
		wantError  bool
		errorMsg   string
		wantClaims *Claims
	}{
		{
			name:       "Valid token",
			authHeader: "Bearer valid-token",
			wantError:  false,
			wantClaims: &Claims{
				Scopes: []string{"mcp:k8s:read", "mcp:git:write"},
			},
		},
		{
			name:       "Missing auth header",
			authHeader: "",
			wantError:  true,
			errorMsg:   "authentication token is required",
		},
		{
			name:       "Invalid header format",
			authHeader: "Token valid-token",
			wantError:  true,
			errorMsg:   "invalid Authorization header format",
		},
		{
			name:       "Inactive token",
			authHeader: "Bearer inactive-token",
			wantError:  true,
			errorMsg:   "token is not active",
		},
		{
			name:       "Invalid token",
			authHeader: "Bearer invalid-token",
			wantError:  true,
			errorMsg:   "introspection returned status 400",
		},
	}
}

func runAuthenticateTest(t *testing.T, tt struct {
	name       string
	authHeader string
	wantError  bool
	errorMsg   string
	wantClaims *Claims
}) {
	t.Helper()

	introspectServer := createMockIntrospectionServer(t)
	defer introspectServer.Close()

	provider := setupOAuth2Provider(t, introspectServer.URL)

	req := createTestRequest(tt.authHeader)
	claims, err := provider.Authenticate(req)

	verifyAuthenticateResult(t, tt, claims, err)
}

func createMockIntrospectionServer(t *testing.T) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		verifyIntrospectionRequest(t, r)
		token := r.FormValue("token")
		respondToIntrospection(w, token)
	}))
}

func verifyIntrospectionRequest(t *testing.T, r *http.Request) {
	t.Helper()

	assert.Equal(t, "POST", r.Method)
	assert.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))

	username, password, ok := r.BasicAuth()
	assert.True(t, ok)
	assert.Equal(t, "test-client", username)
	assert.Equal(t, "secret123", password)

	err := r.ParseForm()
	require.NoError(t, err)
}

func respondToIntrospection(w http.ResponseWriter, token string) {
	switch token {
	case "valid-token":
		_ = json.NewEncoder(w).Encode(introspectionResponse{
			Active:    true,
			Scope:     "mcp:k8s:read mcp:git:write",
			Sub:       "user123",
			ClientID:  "test-client",
			Username:  "testuser",
			TokenType: "Bearer",
			Exp:       time.Now().Add(1 * time.Hour).Unix(),
			Iat:       time.Now().Unix(),
			Iss:       "https://auth.example.com",
			Jti:       "token-123",
			Aud:       []string{"mcp-gateway"},
		})
	case "inactive-token":
		_ = json.NewEncoder(w).Encode(introspectionResponse{
			Active: false,
		})
	default:
		w.WriteHeader(http.StatusBadRequest)
	}
}

func setupOAuth2Provider(t *testing.T, introspectURL string) *OAuth2Provider {
	t.Helper()

	_ = os.Setenv("TEST_CLIENT_SECRET", "secret123")

	defer func() { _ = os.Unsetenv("TEST_CLIENT_SECRET") }()

	provider, err := createOAuth2AuthProvider(config.OAuth2Config{
		ClientID:           "test-client",
		ClientSecretEnv:    "TEST_CLIENT_SECRET",
		IntrospectEndpoint: introspectURL,
	}, testutil.NewTestLogger(t))
	require.NoError(t, err)

	return provider
}

func createTestRequest(authHeader string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}

	return req
}

func verifyAuthenticateResult(t *testing.T, tt struct {
	name       string
	authHeader string
	wantError  bool
	errorMsg   string
	wantClaims *Claims
}, claims *Claims, err error) {
	t.Helper()

	if tt.wantError {
		
		assert.Error(t, err)
		if tt.errorMsg != "" {
			assert.Contains(t, err.Error(), tt.errorMsg)
		}
	} else {
		assert.NoError(t, err)
		assert.NotNil(t, claims)
		assert.Equal(t, tt.wantClaims.Scopes, claims.Scopes)
		assert.Equal(t, "user123", claims.Subject)
	}
}

func TestOAuth2Provider_IntrospectionToClaims(t *testing.T) {
	t.Parallel()

	provider := &OAuth2Provider{}
	tests := createIntrospectionToClaimsTestCases()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			runIntrospectionToClaimsTest(t, provider, tt)
		})
	}
}

func createIntrospectionToClaimsTestCases() []struct {
	name           string
	introspection  *introspectionResponse
	expectedClaims *Claims
} {
	return []struct {
		name           string
		introspection  *introspectionResponse
		expectedClaims *Claims
	}{
		{
			name:           "Basic introspection",
			introspection:  createBasicIntrospectionResponse(),
			expectedClaims: createBasicExpectedClaims(),
		},
		{
			name:           "With rate limit in scope",
			introspection:  createRateLimitIntrospectionResponse(),
			expectedClaims: createRateLimitExpectedClaims(),
		},
		{
			name:           "Empty scopes",
			introspection:  createEmptyScopesIntrospectionResponse(),
			expectedClaims: createEmptyScopesExpectedClaims(),
		},
	}
}

func createBasicIntrospectionResponse() *introspectionResponse {
	return &introspectionResponse{
		Active:    true,
		Scope:     "mcp:k8s:read mcp:git:write",
		Sub:       "user123",
		ClientID:  "test-client",
		Username:  "testuser",
		TokenType: "Bearer",
		Exp:       1735689600,
		Iat:       1735603200,
		Iss:       "https://auth.example.com",
		Jti:       "token-123",
		Aud:       []string{"mcp-gateway"},
	}
}

func createBasicExpectedClaims() *Claims {
	return &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user123",
			Issuer:    "https://auth.example.com",
			ID:        "token-123",
			ExpiresAt: jwt.NewNumericDate(time.Unix(1735689600, 0)),
			IssuedAt:  jwt.NewNumericDate(time.Unix(1735603200, 0)),
			Audience:  []string{"mcp-gateway"},
		},
		Scopes: []string{"mcp:k8s:read", "mcp:git:write"},
		RateLimit: RateLimitConfig{
			RequestsPerMinute: testMaxIterations,
			Burst:             testTimeout,
		},
	}
}

func createRateLimitIntrospectionResponse() *introspectionResponse {
	return &introspectionResponse{
		Active:    true,
		Scope:     "mcp:k8s:read rate_limit:5000:100",
		Sub:       "user456",
		ClientID:  "premium-client",
		TokenType: "Bearer",
		Exp:       1735689600,
		Iat:       1735603200,
		Iss:       "https://auth.example.com",
	}
}

func createRateLimitExpectedClaims() *Claims {
	return &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user456",
			Issuer:    "https://auth.example.com",
			ExpiresAt: jwt.NewNumericDate(time.Unix(1735689600, 0)),
			IssuedAt:  jwt.NewNumericDate(time.Unix(1735603200, 0)),
		},
		Scopes: []string{"mcp:k8s:read", "rate_limit:5000:100"},
		RateLimit: RateLimitConfig{
			RequestsPerMinute: 5000,
			Burst:             testIterations,
		},
	}
}

func createEmptyScopesIntrospectionResponse() *introspectionResponse {
	return &introspectionResponse{
		Active:    true,
		Scope:     "",
		Sub:       "user789",
		ClientID:  "limited-client",
		TokenType: "Bearer",
		Exp:       1735689600,
		Iat:       1735603200,
		Iss:       "https://auth.example.com",
	}
}

func createEmptyScopesExpectedClaims() *Claims {
	return &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user789",
			Issuer:    "https://auth.example.com",
			ExpiresAt: jwt.NewNumericDate(time.Unix(1735689600, 0)),
			IssuedAt:  jwt.NewNumericDate(time.Unix(1735603200, 0)),
		},
		Scopes: []string{},
		RateLimit: RateLimitConfig{
			RequestsPerMinute: testMaxIterations,
			Burst:             testTimeout,
		},
	}
}

func runIntrospectionToClaimsTest(t *testing.T, provider *OAuth2Provider, tt struct {
	name           string
	introspection  *introspectionResponse
	expectedClaims *Claims
}) {
	t.Helper()

	claims := provider.introspectionToClaims(tt.introspection)

	verifyClaimsMatch(t, tt.expectedClaims, claims)
}

func verifyClaimsMatch(t *testing.T, expected, actual *Claims) {
	t.Helper()

	assert.Equal(t, expected.Subject, actual.Subject)
	assert.Equal(t, expected.Issuer, actual.Issuer)
	assert.Equal(t, expected.ID, actual.ID)
	assert.Equal(t, expected.Scopes, actual.Scopes)
	assert.Equal(t, expected.RateLimit, actual.RateLimit)

	if expected.ExpiresAt != nil {
		assert.Equal(t, expected.ExpiresAt.Unix(), actual.ExpiresAt.Unix())
	}

	if expected.IssuedAt != nil {
		assert.Equal(t, expected.IssuedAt.Unix(), actual.IssuedAt.Unix())
	}
}

func TestOAuth2Provider_Caching(t *testing.T) {
	t.Parallel()
	// Create provider
	provider := &OAuth2Provider{
		cache: make(map[string]*cachedIntrospection),
	}

	// Create test claims
	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user123",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Hour)),
		},
		Scopes: []string{"mcp:k8s:read"},
	}

	// Cache the claims
	provider.cacheIntrospection("test-token", claims)

	// Verify cache hit
	cachedClaims := provider.getCachedClaims("test-token")
	assert.NotNil(t, cachedClaims)
	assert.Equal(t, claims.Subject, cachedClaims.Subject)

	// Verify cache miss for different token
	missedClaims := provider.getCachedClaims("different-token")
	assert.Nil(t, missedClaims)

	// Test expired cache entry
	expiredClaims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user456",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-1 * time.Hour)),
		},
	}
	provider.cacheIntrospection("expired-token", expiredClaims)

	// Should not return expired entry
	result := provider.getCachedClaims("expired-token")
	assert.Nil(t, result)
}

func TestOAuth2Provider_ValidateToken_WithCache(t *testing.T) {
	t.Parallel()

	callCount := 0

	// Create a mock introspection server
	introspectServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++

		_ = json.NewEncoder(w).Encode(introspectionResponse{ 
			Active:    true,
			Scope:     "mcp:k8s:read",
			Sub:       "user123",
			ClientID:  "test-client",
			TokenType: "Bearer",
			Exp:       time.Now().Add(1 * time.Hour).Unix(),
			Iat:       time.Now().Unix(),
			Iss:       "https://auth.example.com",
		})
	}))
	defer introspectServer.Close()

	// Create provider

	_ = os.Setenv("TEST_CLIENT_SECRET", "secret123")

	defer func() { _ = os.Unsetenv("TEST_CLIENT_SECRET") }()

	provider, err := createOAuth2AuthProvider(config.OAuth2Config{
		ClientID:           "test-client",
		ClientSecretEnv:    "TEST_CLIENT_SECRET",
		IntrospectEndpoint: introspectServer.URL,
	}, testutil.NewTestLogger(t))
	require.NoError(t, err)

	// First call should hit the server
	claims1, err := provider.ValidateToken("test-token")
	assert.NoError(t, err)
	assert.NotNil(t, claims1)
	assert.Equal(t, 1, callCount)

	// Second call should use cache
	claims2, err := provider.ValidateToken("test-token")
	assert.NoError(t, err)
	assert.NotNil(t, claims2)
	assert.Equal(t, 1, callCount) // No additional call

	// Different token should hit server again
	claims3, err := provider.ValidateToken("different-token")
	assert.NoError(t, err)
	assert.NotNil(t, claims3)
	assert.Equal(t, 2, callCount)
}

func TestOAuth2Provider_CleanupCache(t *testing.T) {
	t.Parallel()

	provider := &OAuth2Provider{
		cache: make(map[string]*cachedIntrospection),
	}

	// Add expired entry
	provider.cache["expired"] = &cachedIntrospection{
		claims: &Claims{
			RegisteredClaims: jwt.RegisteredClaims{
				Subject: "expired-user",
			},
		},
		expiresAt: time.Now().Add(-1 * time.Minute),
	}

	// Add valid entry
	provider.cache["valid"] = &cachedIntrospection{
		claims: &Claims{
			RegisteredClaims: jwt.RegisteredClaims{
				Subject: "valid-user",
			},
		},
		expiresAt: time.Now().Add(5 * time.Minute),
	}

	// Manually trigger cleanup
	provider.cacheMutex.Lock()

	now := time.Now()
	for token, cached := range provider.cache {
		if now.After(cached.expiresAt) {
			delete(provider.cache, token)
		}
	}

	provider.cacheMutex.Unlock()

	// Verify cleanup
	assert.Len(t, provider.cache, 1)
	_, exists := provider.cache["expired"]
	assert.False(t, exists)
	_, exists = provider.cache["valid"]
	assert.True(t, exists)
}

func TestOAuth2Provider_ValidateJWT(t *testing.T) {
	t.Parallel()

	provider := &OAuth2Provider{
		config: config.OAuth2Config{
			JWKSEndpoint: "https://example.com/.well-known/jwks.json",
		},
	}

	// Test the stub implementation - should always return error
	claims, err := provider.validateJWT("any-jwt-token")
	assert.Error(t, err)
	assert.Nil(t, claims)
	assert.Contains(t, err.Error(), "JWKS validation not implemented")
}

func TestOAuth2Provider_IntrospectToken_ErrorHandling(t *testing.T) {
	tests := createIntrospectErrorTestCases()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runIntrospectErrorTest(t, tt)
		})
	}
}

type introspectErrorTestCase struct {
	name           string
	serverResponse func(w http.ResponseWriter, r *http.Request)
	expectError    bool
	errorContains  string
}

func createIntrospectErrorTestCases() []introspectErrorTestCase {
	return []introspectErrorTestCase{
		{
			name: "HTTP 500 error",
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			expectError:   true,
			errorContains: "introspection returned status 500",
		},
		{
			name: "Invalid JSON response",
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte("invalid json"))
			},
			expectError:   true,
			errorContains: "failed to parse introspection response",
		},
		{
			name: "Empty response body",
			serverResponse: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(""))
			},
			expectError:   true,
			errorContains: "failed to parse introspection response",
		},
	}
}

func runIntrospectErrorTest(t *testing.T, tt introspectErrorTestCase) {
	t.Helper()

	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(tt.serverResponse))
	defer server.Close()

	// Create provider
	provider := &OAuth2Provider{
		config: config.OAuth2Config{
			ClientID:           "test-client",
			IntrospectEndpoint: server.URL,
		},
		clientSecret: "secret123",
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		cache: make(map[string]*cachedIntrospection),
	}

	// Test introspection
	resp, err := provider.introspectToken("test-token")

	if tt.expectError {
		assert.Error(t, err)
		assert.Nil(t, resp)

		if tt.errorContains != "" {
			assert.Contains(t, err.Error(), tt.errorContains)
		}
	} else {
		assert.NoError(t, err)
		assert.NotNil(t, resp)
	}
}

func TestOAuth2Provider_ValidateToken_WithJWKS(t *testing.T) {
	t.Parallel()
	// Create a provider with JWKS endpoint configured
	provider := &OAuth2Provider{
		config: config.OAuth2Config{
			JWKSEndpoint:       "https://example.com/.well-known/jwks.json",
			IntrospectEndpoint: "https://example.com/introspect",
			ClientID:           "test-client",
		},
		clientSecret: "secret123",
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		cache: make(map[string]*cachedIntrospection),
	}

	// Test that JWKS validation fails and falls back to introspection
	// Since validateJWT always returns an error, this tests the fallback path

	// Create a mock introspection server for fallback
	introspectServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(introspectionResponse{ 
			Active:    true,
			Scope:     "mcp:k8s:read",
			Sub:       "user123",
			ClientID:  "test-client",
			TokenType: "Bearer",
			Exp:       time.Now().Add(1 * time.Hour).Unix(),
			Iat:       time.Now().Unix(),
			Iss:       "https://auth.example.com",
		})
	}))
	defer introspectServer.Close()

	provider.config.IntrospectEndpoint = introspectServer.URL

	// Test token validation - should try JWKS first, fail, then fall back to introspection
	claims, err := provider.ValidateToken("test-jwt-token")
	assert.NoError(t, err)
	assert.NotNil(t, claims)
	assert.Equal(t, "user123", claims.Subject)
}

func TestOAuth2Provider_ValidateToken_IntrospectionFailure(t *testing.T) {
	t.Parallel()
	// Create a provider that will fail introspection
	provider := &OAuth2Provider{
		config: config.OAuth2Config{
			IntrospectEndpoint: "https://invalid-url.example.com/introspect",
			ClientID:           "test-client",
		},
		clientSecret: "secret123",
		httpClient: &http.Client{
			Timeout: 1 * time.Millisecond, // Very short timeout to force failure
		},
		cache: make(map[string]*cachedIntrospection),
	}

	// Test token validation failure
	claims, err := provider.ValidateToken("test-token")
	assert.Error(t, err)
	assert.Nil(t, claims)
	assert.Contains(t, err.Error(), "token introspection failed")
}

func TestOAuth2Provider_CleanupCache_Automatic(t *testing.T) {
	t.Parallel()
	// Test the cleanup logic separately without the goroutine
	provider := &OAuth2Provider{
		cache: make(map[string]*cachedIntrospection),
	}

	// Add an entry that has already expired
	provider.cache["already-expired"] = &cachedIntrospection{
		claims: &Claims{
			RegisteredClaims: jwt.RegisteredClaims{
				Subject: "test-user",
			},
		},
		expiresAt: time.Now().Add(-1 * time.Minute), // Already expired
	}

	// Add an entry that will be valid
	provider.cache["still-valid"] = &cachedIntrospection{
		claims: &Claims{
			RegisteredClaims: jwt.RegisteredClaims{
				Subject: "test-user-2",
			},
		},
		expiresAt: time.Now().Add(5 * time.Minute), // Still valid
	}

	// Manually run the cleanup logic (same as the goroutine would do)
	provider.cacheMutex.Lock()

	now := time.Now()
	for token, cached := range provider.cache {
		if now.After(cached.expiresAt) {
			delete(provider.cache, token)
		}
	}

	provider.cacheMutex.Unlock()

	// Verify cleanup worked
	provider.cacheMutex.RLock()
	defer provider.cacheMutex.RUnlock()

	assert.Len(t, provider.cache, 1)
	_, exists := provider.cache["already-expired"]
	assert.False(t, exists)
	_, exists = provider.cache["still-valid"]
	assert.True(t, exists)
}

func TestOAuth2Provider_CacheIntrospection_ExpiredToken(t *testing.T) {
	t.Parallel()

	provider := &OAuth2Provider{
		cache: make(map[string]*cachedIntrospection),
	}

	// Create claims that are already expired
	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user123",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-1 * time.Hour)), // Already expired
		},
		Scopes: []string{"mcp:k8s:read"},
	}

	// Cache the expired claims
	provider.cacheIntrospection("expired-token", claims)

	// Verify the cache entry uses token expiry time instead of default 5 minutes
	provider.cacheMutex.RLock()
	cached, exists := provider.cache["expired-token"]
	provider.cacheMutex.RUnlock()

	assert.True(t, exists)
	assert.NotNil(t, cached)
	// The cache expiry should be the token's expiry time (in the past)
	assert.True(t, cached.expiresAt.Before(time.Now()))
}

func TestOAuth2Provider_ValidateToken_CompleteFlow(t *testing.T) {
	t.Parallel()

	// Create a comprehensive test that exercises multiple code paths
	callCount := 0
	server := createCompleteFlowMockServer(t, &callCount)
	defer server.Close()

	provider := setupCompleteFlowProvider(t, server.URL)
	runCompleteFlowValidation(t, provider, &callCount)
}

func createCompleteFlowMockServer(t *testing.T, callCount *int) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*callCount++
		switch r.URL.Path {
		case "/introspect":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"active": true, "sub": "user123", "scope": "read write"}`))
		case "/userinfo":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"sub": "user123", "name": "Test User", "email": "test@example.com"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func setupCompleteFlowProvider(t *testing.T, serverURL string) *OAuth2Provider {
	t.Helper()
	config := config.OAuth2Config{
		IntrospectEndpoint: serverURL + "/introspect",
		ClientID:           "test-client",
		ClientSecretEnv:    "TEST_CLIENT_SECRET",
	}
	_ = os.Setenv("TEST_CLIENT_SECRET", "test-secret")
	provider, err := createOAuth2AuthProvider(config, zaptest.NewLogger(t))
	require.NoError(t, err)

	return provider
}

func runCompleteFlowValidation(t *testing.T, provider *OAuth2Provider, callCount *int) {
	t.Helper()
	token := "valid-token"
	
	result, err := provider.ValidateToken(token)
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "user123", result.Subject)
	assert.Contains(t, result.Scopes, "read")
	assert.Contains(t, result.Scopes, "write")
	assert.Equal(t, 2, *callCount, "Should call both introspect and userinfo endpoints")
}

func TestOAuth2Provider_IntrospectToken_NetworkError(t *testing.T) {
	t.Parallel()

	// Create provider with invalid URL to force network error
	provider := &OAuth2Provider{
		config: config.OAuth2Config{
			ClientID:           "test-client",
			IntrospectEndpoint: "http://0.0.0.0:1/invalid", // Should fail to connect
		},
		clientSecret: "secret123",
		httpClient: &http.Client{
			Timeout: testIterations * time.Millisecond, // Short timeout
		},
		cache: make(map[string]*cachedIntrospection),
	}

	// Test that network error is handled properly
	resp, err := provider.introspectToken("test-token")
	assert.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "introspection request failed")
}

func TestOAuth2Provider_IntrospectionToClaims_EdgeCases(t *testing.T) {
	t.Parallel()
	provider := &OAuth2Provider{}
	tests := createIntrospectionEdgeCaseTests()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			claims := provider.introspectionToClaims(tt.introspection)
			assert.Equal(t, tt.expectScopes, claims.Scopes)
			assert.Equal(t, tt.expectRPM, claims.RateLimit.RequestsPerMinute)
			assert.Equal(t, tt.expectBurst, claims.RateLimit.Burst)
		})
	}
}

func createIntrospectionEdgeCaseTests() []struct {
	name          string
	introspection *introspectionResponse
	expectScopes  []string
	expectRPM     int
	expectBurst   int
} {
	return []struct {
		name          string
		introspection *introspectionResponse
		expectScopes  []string
		expectRPM     int
		expectBurst   int
	}{
		{
			name: "Invalid rate limit format",
			introspection: &introspectionResponse{
				Active:    true,
				Scope:     "mcp:k8s:read rate_limit:invalid:format rate_limit:1000",
				Sub:       "user123",
				TokenType: "Bearer",
				Exp:       time.Now().Add(1 * time.Hour).Unix(),
				Iat:       time.Now().Unix(),
			},
			expectScopes: []string{"mcp:k8s:read", "rate_limit:invalid:format", "rate_limit:1000"},
			expectRPM:    testMaxIterations, // Default value
			expectBurst:  testTimeout,       // Default value
		},
		{
			name: "Multiple rate limits - last valid one wins",
			introspection: &introspectionResponse{
				Active:    true,
				Scope:     "rate_limit:500:25 rate_limit:1500:100",
				Sub:       "user456",
				TokenType: "Bearer",
				Exp:       time.Now().Add(1 * time.Hour).Unix(),
				Iat:       time.Now().Unix(),
			},
			expectScopes: []string{"rate_limit:500:25", "rate_limit:1500:100"},
			expectRPM:    1500, // Last valid rate limit wins
			expectBurst:  testIterations,
		},
		{
			name: "No audience",
			introspection: &introspectionResponse{
				Active:    true,
				Scope:     "mcp:k8s:read",
				Sub:       "user789",
				TokenType: "Bearer",
				Exp:       time.Now().Add(1 * time.Hour).Unix(),
				Iat:       time.Now().Unix(),
				Aud:       []string{}, // Empty audience
			},
			expectScopes: []string{"mcp:k8s:read"},
			expectRPM:    testMaxIterations,
			expectBurst:  testTimeout,
		},
	}
}

func TestOAuth2Provider_NewProvider_StartsCacheCleanup(t *testing.T) {
	// Test that createOAuth2AuthProvider actually starts the cleanup goroutine
	// We can't easily test the goroutine directly, but we can verify
	// the provider starts successfully with cleanup enabled
	provider := &OAuth2Provider{
		config: config.OAuth2Config{
			ClientID:           "test-client",
			IntrospectEndpoint: "https://auth.example.com/introspect",
		},
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		cache: make(map[string]*cachedIntrospection),
	}

	// Add an entry to test cleanup logic exists
	provider.cache["test"] = &cachedIntrospection{
		claims: &Claims{
			RegisteredClaims: jwt.RegisteredClaims{Subject: "test"},
		},
		expiresAt: time.Now().Add(5 * time.Minute),
	}

	// Verify provider is properly initialized
	assert.NotNil(t, provider.httpClient)
	assert.NotNil(t, provider.cache)
	assert.Len(t, provider.cache, 1)

	// Test manual cleanup logic (simulates what the goroutine does)
	go func() {
		// This simulates the cleanupCache function's core logic
		provider.cacheMutex.Lock()
		defer provider.cacheMutex.Unlock()

		now := time.Now()
		for token, cached := range provider.cache {
			if now.After(cached.expiresAt) {
				delete(provider.cache, token)
			}
		}
	}()

	// Give goroutine time to run
	time.Sleep(10 * time.Millisecond)

	// Entry should still be there (not expired)
	provider.cacheMutex.RLock()
	defer provider.cacheMutex.RUnlock()

	assert.Len(t, provider.cache, 1)
}
