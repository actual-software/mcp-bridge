// Package testutil provides testing utilities and helpers for the MCP gateway services.
package testutil

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

const (
	// DefaultRSAKeySize is the default RSA key size for testing.
	DefaultRSAKeySize = 2048

	// Default rate limit values for testing.
	defaultTestRateLimitPerMinute = 100
	defaultTestRateLimitBurst     = 10
)

// NewTestLogger creates a logger for testing.
func NewTestLogger(t *testing.T) *zap.Logger {
	t.Helper()

	return zaptest.NewLogger(t)
}

// NewBenchLogger creates a logger for benchmarks.
func NewBenchLogger(_ *testing.B) *zap.Logger {
	// For benchmarks, use a no-op logger to avoid overhead
	return zap.NewNop()
}

// GenerateRSAKeyPair generates an RSA key pair for testing.
// GenerateRSAKeyPair creates a complete RSA key pair for cryptographic testing.
// This utility generates both private and public keys in multiple formats
// to support various testing scenarios including JWT signing, TLS certificates,
// and file-based public key validation.
//
// Key Generation Process:
//  1. Generate RSA private key using crypto/rand for secure randomness
//  2. Extract corresponding public key from private key
//  3. Encode private key in PKCS#1 format with PEM encoding
//  4. Encode public key in PKIX format with PEM encoding
//  5. Return all formats for maximum test flexibility
//
// Security Considerations:
//   - Uses cryptographically secure random number generator
//   - Generates keys with configurable bit length (default: 2048 bits)
//   - Follows standard encoding practices (PKCS#1 for private, PKIX for public)
//   - Keys are suitable for production-strength testing scenarios
//
// Return Values:
//   - *rsa.PrivateKey: Raw private key object for direct cryptographic operations
//   - []byte: PEM-encoded private key for file storage or string operations
//   - []byte: PEM-encoded public key for verification and file-based operations
//
// Usage Examples:
//
//	// Generate key pair for JWT testing
//	privateKey, _, publicKeyPEM := GenerateRSAKeyPair(t)
//	token := GenerateTestToken(t, claims, privateKey)
//
//	// Create temporary public key file for file-based validation
//	_, _, publicKeyPEM := GenerateRSAKeyPair(t)
//	tmpFile, _ := os.CreateTemp("", "public-key-*.pem")
//	tmpFile.Write(publicKeyPEM)
//
// Performance Notes:
//   - Key generation is computationally expensive (RSA 2048-bit)
//   - Consider caching key pairs for test suites with many test cases
//   - Generation time: ~10-50ms depending on system performance
func GenerateRSAKeyPair(t *testing.T) (*rsa.PrivateKey, []byte, []byte) {
	t.Helper()

	// Generate RSA private key using secure random number generator
	privateKey, err := rsa.GenerateKey(rand.Reader, DefaultRSAKeySize)
	if err != nil {
		t.Fatalf("Failed to generate RSA private key: %v", err)
	}

	// Encode private key in PKCS#1 format with PEM encoding
	// PKCS#1 is the standard format for RSA private keys
	privateKeyPEM := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	}
	privateKeyBytes := pem.EncodeToMemory(privateKeyPEM)

	// Encode public key in PKIX format with PEM encoding
	// PKIX (Public Key Infrastructure X.509) is the standard for public keys
	publicKeyBytes, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		t.Fatalf("Failed to marshal public key to PKIX format: %v", err)
	}

	publicKeyPEM := &pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: publicKeyBytes,
	}
	publicKeyPEMBytes := pem.EncodeToMemory(publicKeyPEM)

	return privateKey, privateKeyBytes, publicKeyPEMBytes
}

// GenerateTestToken generates a JWT token for testing.
// GenerateTestToken creates a properly signed JWT token for testing purposes.
// This utility function handles both HMAC-SHA256 (symmetric) and RSA-SHA256 (asymmetric)
// signing algorithms based on the provided signing key type.
//
// Token Generation Process:
//  1. Determine signing algorithm based on key type (HMAC vs RSA)
//  2. Create JWT token with provided claims
//  3. Sign token with appropriate algorithm
//  4. Return base64-encoded token string
//
// Parameters:
//   - t: Testing instance for error reporting and helper marking
//   - claims: JWT claims structure (standard + custom claims)
//   - signingKey: Either []byte for HMAC or *rsa.PrivateKey for RSA
//
// Supported Algorithms:
//   - HMAC-SHA256: For development and testing with shared secrets
//   - RSA-SHA256: For production-like testing with public/private key pairs
//
// Usage Examples:
//
//	// HMAC token for simple testing
//	token := GenerateTestToken(t, claims, []byte("secret-key"))
//
//	// RSA token for realistic authentication testing
//	privateKey, _, _ := GenerateRSAKeyPair(t)
//	token := GenerateTestToken(t, claims, privateKey)
//
// Error Handling:
//   - Automatically fails test on signing errors
//   - Provides detailed error messages for debugging
func GenerateTestToken(t *testing.T, claims jwt.Claims, signingKey interface{}) string {
	t.Helper()

	// Default to HMAC-SHA256 for symmetric key signing
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	// Detect RSA private key and switch to RSA-SHA256 algorithm
	if _, ok := signingKey.(*rsa.PrivateKey); ok {
		token = jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	}

	// Sign the token with the provided key
	tokenString, err := token.SignedString(signingKey)
	if err != nil {
		t.Fatalf("Failed to sign JWT token: %v", err)
	}

	return tokenString
}

// TestClaims creates test JWT claims.
func TestClaims(subject string, scopes []string, expiry time.Duration) jwt.MapClaims {
	now := time.Now()

	return jwt.MapClaims{
		"sub":    subject,
		"iat":    now.Unix(),
		"exp":    now.Add(expiry).Unix(),
		"scopes": scopes,
		"rate_limit": map[string]int{
			"requests_per_minute": defaultTestRateLimitPerMinute,
			"burst":               defaultTestRateLimitBurst,
		},
	}
}

// AssertError checks if an error matches expected conditions.
// AssertError provides comprehensive error assertion functionality for tests.
// This utility function validates both the presence/absence of errors and
// the content of error messages, enabling precise error condition testing
// in table-driven tests and complex error handling scenarios.
//
// Error Validation Modes:
//  1. Success Case: Verifies no error occurred when none expected
//  2. Error Presence: Verifies error occurred when expected
//  3. Error Content: Validates error message contains expected text
//  4. Combined: Both presence and content validation
//
// Parameters:
//   - t: Testing instance for failure reporting and helper marking
//   - err: The actual error value to validate (may be nil)
//   - shouldError: Boolean indicating whether an error is expected
//   - contains: Expected substring in error message (empty string skips content check)
//
// Assertion Logic:
//   - If shouldError=true and err=nil: FAIL (missing expected error)
//   - If shouldError=false and err!=nil: FAIL (unexpected error occurred)
//   - If shouldError=true and contains!="" and error doesn't contain text: FAIL
//   - All other cases: PASS
//
// Usage Examples:
//
//	// Validate no error expected and none occurred
//	AssertError(t, err, false, "")
//
//	// Validate error expected and occurred
//	AssertError(t, err, true, "")
//
//	// Validate specific error message content
//	AssertError(t, err, true, "token is expired")
//
//	// Table-driven test integration
//	AssertError(t, err, tt.wantError, tt.errorContains)
//
// Error Message Quality:
//   - Provides clear failure descriptions for debugging
//   - Shows expected vs actual error states
//   - Includes full error text for content mismatches
func AssertError(t *testing.T, err error, shouldError bool, contains string) {
	t.Helper()

	// Case 1: Error expected but none occurred
	if shouldError {
		if err == nil {
			handleMissingError(t, contains)

			return
		}

		// Case 2: Error expected and occurred, validate content if specified
		if contains != "" && !containsString(err.Error(), contains) {
			t.Fatalf("Expected error message containing '%s', but got: '%s'", contains, err.Error())
		}

		return
	}

	// Case 3: No error expected but one occurred
	if err != nil {
		t.Fatalf("Expected no error, but got: %v", err)
	}
}

func handleMissingError(t *testing.T, contains string) {
	t.Helper()

	if contains != "" {
		t.Fatalf("Expected error containing '%s', but got nil error", contains)
	} else {
		t.Fatal("Expected an error to occur, but got nil")
	}
}

func containsString(s, substr string) bool {
	return substr != "" && len(s) >= len(substr) && s[:len(substr)] == substr ||
		len(s) > len(substr) && containsString(s[1:], substr)
}
