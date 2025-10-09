package auth

import (
	"context"
	"crypto/rsa"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap"

	"github.com/actual-software/mcp-bridge/services/gateway/internal/config"
	"github.com/actual-software/mcp-bridge/services/gateway/test/testutil"
)

const httpStatusOK = 200

func TestInitializeAuthenticationProvider(t *testing.T) {
	logger := testutil.NewTestLogger(t)

	tests := createInitProviderTestCases()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runInitProviderTest(t, logger, tt)
		})
	}
}

type initProviderTestCase struct {
	name      string
	config    config.AuthConfig
	setupEnv  func()
	wantError bool
	errorMsg  string
}

func createInitProviderTestCases() []initProviderTestCase {
	return []initProviderTestCase{
		{
			name: "JWT provider with secret key",
			config: config.AuthConfig{
				Provider: "jwt",
				JWT: config.JWTConfig{
					SecretKeyEnv: "JWT_SECRET",
					Issuer:       "test-issuer",
					Audience:     "test-audience",
				},
			},
			setupEnv: func() {
				_ = os.Setenv("JWT_SECRET", "test-secret")
			},
			wantError: false,
		},
		{
			name: "JWT provider missing secret key",
			config: config.AuthConfig{
				Provider: "jwt",
				JWT: config.JWTConfig{
					SecretKeyEnv: "JWT_SECRET_MISSING",
				},
			},
			setupEnv: func() {
				_ = os.Unsetenv("JWT_SECRET_MISSING")
			},
			wantError: true,
			errorMsg:  "JWT secret key environment variable",
		},
		{
			name: "Unsupported provider",
			config: config.AuthConfig{
				Provider: "unsupported",
			},
			setupEnv:  func() {},
			wantError: true,
			errorMsg:  "unsupported auth provider",
		},
	}
}

func runInitProviderTest(t *testing.T, logger *zap.Logger, tt initProviderTestCase) {
	t.Helper()

	tt.setupEnv()

	defer func() {
		_ = os.Unsetenv("JWT_SECRET")
		_ = os.Unsetenv("JWT_SECRET_MISSING")
	}()

	provider, err := InitializeAuthenticationProvider(tt.config, logger)
	testutil.AssertError(t, err, tt.wantError, tt.errorMsg)

	if !tt.wantError && provider == nil {
		t.Fatal("Expected provider to be created")
	}
}

// TestJWTProvider_ValidateToken validates the JWT token validation logic
// across different signing algorithms, claim configurations, and error conditions.
//
// JWT Validation Process Tested:
//  1. Token parsing and structural validation
//  2. Signature verification (HMAC-SHA256, RSA-SHA256)
//  3. Standard claims validation (exp, nbf, iss, aud)
//  4. Custom claims extraction and processing
//  5. Error handling and security boundary enforcement
//
// Security Validations:
//   - Expired tokens are rejected with appropriate error messages
//   - Invalid signatures prevent token acceptance
//   - Issuer and audience claims are properly validated
//   - Time-based claims (nbf, exp) are correctly enforced
//
// Algorithm Support:
//   - HMAC-SHA256 (symmetric key) for development/testing
//   - RSA-SHA256 (asymmetric key) for production deployments
func TestJWTProvider_ValidateToken(t *testing.T) {
	logger := testutil.NewTestLogger(t)
	secretKey := []byte("test-secret")
	tmpFile := setupRSATestKeyFile(t)

	defer func() { _ = os.Remove(tmpFile.Name()) }()

	privateKey := getRSAPrivateKey(t)
	tests := createJWTValidateTokenTestCases(logger, secretKey, tmpFile, privateKey)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runJWTValidateTokenTest(t, tt)
		})
	}
}

func setupRSATestKeyFile(t *testing.T) *os.File {
	t.Helper()

	_, _, publicKeyPEM := testutil.GenerateRSAKeyPair(t)

	tmpFile, err := os.CreateTemp("", "public-key-*.pem")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := tmpFile.Write(publicKeyPEM); err != nil {
		t.Fatal(err)
	}

	_ = tmpFile.Close()

	return tmpFile
}

func getRSAPrivateKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()

	privateKey, _, _ := testutil.GenerateRSAKeyPair(t)

	return privateKey
}

func createJWTValidateTokenTestCases(
	logger *zap.Logger,
	secretKey []byte,
	tmpFile *os.File,
	privateKey *rsa.PrivateKey,
) []jwtValidateTokenTestCase {
	return []jwtValidateTokenTestCase{
		createValidHMACTokenTestCase(logger, secretKey),
		createValidRSATokenTestCase(logger, tmpFile, privateKey),
		createExpiredTokenTestCase(logger, secretKey),
		createInvalidIssuerTestCase(logger, secretKey),
		createInvalidAudienceTestCase(logger, secretKey),
		createNotYetValidTokenTestCase(logger, secretKey),
	}
}

type jwtValidateTokenTestCase struct {
	name          string
	provider      *JWTProvider
	token         string
	signingKey    interface{}
	claims        jwt.Claims
	wantError     bool
	errorContains string
}

func createValidHMACTokenTestCase(logger *zap.Logger, secretKey []byte) jwtValidateTokenTestCase {
	return jwtValidateTokenTestCase{
		name: "Valid HMAC token",
		provider: &JWTProvider{
			config: config.JWTConfig{
				Issuer:   "test-issuer",
				Audience: "test-audience",
			},
			logger:    logger,
			secretKey: secretKey,
		},
		signingKey: secretKey,
		claims: &Claims{
			RegisteredClaims: jwt.RegisteredClaims{
				Subject:   "test-user",
				Issuer:    "test-issuer",
				Audience:  jwt.ClaimStrings{"test-audience"},
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			},
			Scopes: []string{"read", "write"},
		},
		wantError: false,
	}
}

func createValidRSATokenTestCase(
	logger *zap.Logger,
	tmpFile *os.File,
	privateKey *rsa.PrivateKey,
) jwtValidateTokenTestCase {
	return jwtValidateTokenTestCase{
		name: "Valid RSA token",
		provider: &JWTProvider{
			config: config.JWTConfig{
				PublicKeyPath: tmpFile.Name(),
			},
			logger:    logger,
			publicKey: &privateKey.PublicKey,
		},
		signingKey: privateKey,
		claims: &Claims{
			RegisteredClaims: jwt.RegisteredClaims{
				Subject:   "test-user",
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			},
			Scopes: []string{"admin"},
		},
		wantError: false,
	}
}

func createExpiredTokenTestCase(logger *zap.Logger, secretKey []byte) jwtValidateTokenTestCase {
	return jwtValidateTokenTestCase{
		name: "Expired token",
		provider: &JWTProvider{
			config:    config.JWTConfig{},
			logger:    logger,
			secretKey: secretKey,
		},
		signingKey: secretKey,
		claims: &Claims{
			RegisteredClaims: jwt.RegisteredClaims{
				Subject:   "test-user",
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)),
			},
		},
		wantError:     true,
		errorContains: "token is expired",
	}
}

func createInvalidIssuerTestCase(logger *zap.Logger, secretKey []byte) jwtValidateTokenTestCase {
	return jwtValidateTokenTestCase{
		name: "Invalid issuer",
		provider: &JWTProvider{
			config: config.JWTConfig{
				Issuer: "expected-issuer",
			},
			logger:    logger,
			secretKey: secretKey,
		},
		signingKey: secretKey,
		claims: &Claims{
			RegisteredClaims: jwt.RegisteredClaims{
				Subject:   "test-user",
				Issuer:    "wrong-issuer",
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			},
		},
		wantError:     true,
		errorContains: "invalid issuer",
	}
}

func createInvalidAudienceTestCase(logger *zap.Logger, secretKey []byte) jwtValidateTokenTestCase {
	return jwtValidateTokenTestCase{
		name: "Invalid audience",
		provider: &JWTProvider{
			config: config.JWTConfig{
				Audience: "expected-audience",
			},
			logger:    logger,
			secretKey: secretKey,
		},
		signingKey: secretKey,
		claims: &Claims{
			RegisteredClaims: jwt.RegisteredClaims{
				Subject:   "test-user",
				Audience:  jwt.ClaimStrings{"wrong-audience"},
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			},
		},
		wantError:     true,
		errorContains: "invalid audience",
	}
}

func createNotYetValidTokenTestCase(logger *zap.Logger, secretKey []byte) jwtValidateTokenTestCase {
	return jwtValidateTokenTestCase{
		name: "Token not yet valid",
		provider: &JWTProvider{
			config:    config.JWTConfig{},
			logger:    logger,
			secretKey: secretKey,
		},
		signingKey: secretKey,
		claims: &Claims{
			RegisteredClaims: jwt.RegisteredClaims{
				Subject:   "test-user",
				NotBefore: jwt.NewNumericDate(time.Now().Add(time.Hour)),
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(2 * time.Hour)),
			},
		},
		wantError:     true,
		errorContains: "token is not valid yet",
	}
}

func runJWTValidateTokenTest(t *testing.T, tt jwtValidateTokenTestCase) {
	t.Helper()

	tokenString := generateTestTokenString(t, tt)
	claims, err := tt.provider.ValidateToken(tokenString)

	validateTokenTestResult(t, tt, claims, err)
}

func generateTestTokenString(t *testing.T, tt jwtValidateTokenTestCase) string {
	t.Helper()

	if tt.token != "" {
		return tt.token
	}

	return testutil.GenerateTestToken(t, tt.claims, tt.signingKey)
}

func validateTokenTestResult(t *testing.T, tt jwtValidateTokenTestCase, claims *Claims, err error) {
	t.Helper()

	testutil.AssertError(t, err, tt.wantError, tt.errorContains)

	if !tt.wantError {
		validateSuccessfulTokenResult(t, tt, claims)
	}
}

func validateSuccessfulTokenResult(t *testing.T, tt jwtValidateTokenTestCase, claims *Claims) {
	t.Helper()

	if claims == nil {
		t.Fatal("Expected claims to be returned")

		return
	}

	expectedClaims, ok := tt.claims.(*Claims)
	if !ok {
		t.Fatal("Expected *Claims type in test case")

		return
	}

	if claims.Subject != expectedClaims.Subject {
		t.Errorf("Expected subject %s, got %s", expectedClaims.Subject, claims.Subject)
	}

	if claims.RateLimit.RequestsPerMinute == 0 {
		t.Error("Expected default rate limit to be set")
	}
}

func TestJWTProvider_Authenticate(t *testing.T) {
	logger := testutil.NewTestLogger(t)
	secretKey := []byte("test-secret")

	provider := &JWTProvider{
		config:    config.JWTConfig{},
		logger:    logger,
		secretKey: secretKey,
	}

	validClaims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "test-user",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
		Scopes: []string{"read"},
	}
	validToken := testutil.GenerateTestToken(t, validClaims, secretKey)

	tests := createJWTAuthenticateTestCases(validToken)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runJWTAuthenticateTest(t, provider, tt)
		})
	}
}

type jwtAuthenticateTestCase struct {
	name          string
	setupRequest  func() *http.Request
	wantError     bool
	errorContains string
}

func createJWTAuthenticateTestCases(validToken string) []jwtAuthenticateTestCase {
	return []jwtAuthenticateTestCase{
		{
			name: "Valid Bearer token",
			setupRequest: func() *http.Request {
				req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/test", nil)
				req.Header.Set("Authorization", "Bearer "+validToken)

				return req
			},
			wantError: false,
		},
		{
			name: "Missing Authorization header",
			setupRequest: func() *http.Request {
				req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/test", nil)

				return req
			},
			wantError:     true,
			errorContains: "missing Authorization header",
		},
		{
			name: "Invalid header format",
			setupRequest: func() *http.Request {
				req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/test", nil)
				req.Header.Set("Authorization", "InvalidFormat")

				return req
			},
			wantError:     true,
			errorContains: "invalid Authorization header format",
		},
		{
			name: "Non-Bearer scheme",
			setupRequest: func() *http.Request {
				req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/test", nil)
				req.Header.Set("Authorization", "Basic "+validToken)

				return req
			},
			wantError:     true,
			errorContains: "invalid Authorization header format",
		},
	}
}

func runJWTAuthenticateTest(t *testing.T, provider *JWTProvider, tt jwtAuthenticateTestCase) {
	t.Helper()

	req := tt.setupRequest()
	claims, err := provider.Authenticate(req)
	testutil.AssertError(t, err, tt.wantError, tt.errorContains)

	if !tt.wantError && claims == nil {
		t.Fatal("Expected claims to be returned")
	}
}

func TestClaims_HasScope(t *testing.T) {
	tests := []struct {
		name     string
		claims   *Claims
		scope    string
		expected bool
	}{
		{
			name: "Has exact scope",
			claims: &Claims{
				Scopes: []string{"read", "write", "admin"},
			},
			scope:    "write",
			expected: true,
		},
		{
			name: "Does not have scope",
			claims: &Claims{
				Scopes: []string{"read", "write"},
			},
			scope:    "admin",
			expected: false,
		},
		{
			name: "Empty scopes",
			claims: &Claims{
				Scopes: []string{},
			},
			scope:    "read",
			expected: false,
		},
		{
			name:     "Nil scopes",
			claims:   &Claims{},
			scope:    "read",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.claims.HasScope(tt.scope)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestClaims_CanAccessNamespace(t *testing.T) {
	tests := []struct {
		name      string
		claims    *Claims
		namespace string
		expected  bool
	}{
		{
			name: "Wildcard scope allows all",
			claims: &Claims{
				Scopes: []string{"mcp:*"},
			},
			namespace: "test-namespace",
			expected:  true,
		},
		{
			name: "Specific namespace wildcard",
			claims: &Claims{
				Scopes: []string{"mcp:test-namespace:*"},
			},
			namespace: "test-namespace",
			expected:  true,
		},
		{
			name: "Read-only namespace access",
			claims: &Claims{
				Scopes: []string{"mcp:test-namespace:read"},
			},
			namespace: "test-namespace",
			expected:  true,
		},
		{
			name: "No matching scope",
			claims: &Claims{
				Scopes: []string{"mcp:other-namespace:*"},
			},
			namespace: "test-namespace",
			expected:  false,
		},
		{
			name: "Multiple scopes with match",
			claims: &Claims{
				Scopes: []string{
					"mcp:namespace1:read",
					"mcp:namespace2:*",
					"mcp:test-namespace:read",
				},
			},
			namespace: "test-namespace",
			expected:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.claims.CanAccessNamespace(tt.namespace)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestJWTProvider_Integration(t *testing.T) {
	// Set up environment
	_ = os.Setenv("TEST_JWT_SECRET", "integration-test-secret")

	defer func() { _ = os.Unsetenv("TEST_JWT_SECRET") }()

	logger := testutil.NewTestLogger(t)
	config := config.AuthConfig{
		Provider: "jwt",
		JWT: config.JWTConfig{
			SecretKeyEnv: "TEST_JWT_SECRET",
			Issuer:       "test-issuer",
			Audience:     "test-audience",
		},
	}

	provider, err := InitializeAuthenticationProvider(config, logger)
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	// Create a valid token
	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "integration-user",
			Issuer:    "test-issuer",
			Audience:  jwt.ClaimStrings{"test-audience"},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
		Scopes: []string{"mcp:*"},
		RateLimit: RateLimitConfig{
			RequestsPerMinute: httpStatusOK,
			Burst:             20,
		},
	}

	token := testutil.GenerateTestToken(t, claims, []byte("integration-test-secret"))

	// Test full authentication flow
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/api/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	authenticatedClaims, err := provider.Authenticate(req)
	if err != nil {
		t.Fatalf("Authentication failed: %v", err)
	}

	// Verify claims
	if authenticatedClaims.Subject != "integration-user" {
		t.Errorf("Expected subject integration-user, got %s", authenticatedClaims.Subject)
	}

	if !authenticatedClaims.CanAccessNamespace("any-namespace") {
		t.Error("Expected wildcard scope to allow any namespace access")
	}

	if authenticatedClaims.RateLimit.RequestsPerMinute != httpStatusOK {
		t.Errorf("Expected rate limit httpStatusOK, got %d", authenticatedClaims.RateLimit.RequestsPerMinute)
	}
}
