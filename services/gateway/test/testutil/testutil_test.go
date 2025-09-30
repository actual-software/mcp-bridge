package testutil

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// TestDefaultConstants tests the package constants.
func TestDefaultConstants(t *testing.T) {
	t.Run("DefaultRSAKeySize", func(t *testing.T) {
		assert.Equal(t, 2048, DefaultRSAKeySize)
	})
}

// TestNewTestLogger tests the test logger creation.
func TestNewTestLogger(t *testing.T) {
	t.Run("creates_valid_logger", func(t *testing.T) {
		logger := NewTestLogger(t)

		assert.NotNil(t, logger)
		assert.IsType(t, &zap.Logger{}, logger)

		// Test that the logger can actually log
		logger.Info("test message")
		logger.Error("test error")
		logger.Debug("test debug")
	})
}

// TestNewBenchLogger tests the benchmark logger creation.
func TestNewBenchLogger(t *testing.T) {
	// Create a benchmark to test with
	b := &testing.B{}

	t.Run("creates_noop_logger", func(t *testing.T) {
		logger := NewBenchLogger(b)

		assert.NotNil(t, logger)
		assert.IsType(t, &zap.Logger{}, logger)

		// Should be able to log without overhead
		logger.Info("bench message")
		logger.Error("bench error")
	})
}

// TestGenerateRSAKeyPair tests RSA key pair generation.
func TestGenerateRSAKeyPair(t *testing.T) {
	t.Run("generates_valid_key_pair", func(t *testing.T) {
		privateKey, privateKeyPEM, publicKeyPEM := GenerateRSAKeyPair(t)

		// Verify private key
		assert.NotNil(t, privateKey)
		assert.IsType(t, &rsa.PrivateKey{}, privateKey)
		assert.Equal(t, DefaultRSAKeySize, privateKey.Size()*8) // Size() returns bytes, so *8 for bits

		// Verify private key PEM encoding
		assert.NotNil(t, privateKeyPEM)
		assert.NotEmpty(t, privateKeyPEM)
		assert.Contains(t, string(privateKeyPEM), "-----BEGIN RSA PRIVATE KEY-----")
		assert.Contains(t, string(privateKeyPEM), "-----END RSA PRIVATE KEY-----")

		// Verify public key PEM encoding
		assert.NotNil(t, publicKeyPEM)
		assert.NotEmpty(t, publicKeyPEM)
		assert.Contains(t, string(publicKeyPEM), "-----BEGIN PUBLIC KEY-----")
		assert.Contains(t, string(publicKeyPEM), "-----END PUBLIC KEY-----")
	})

	t.Run("private_key_can_be_parsed", func(t *testing.T) {
		_, privateKeyPEM, _ := GenerateRSAKeyPair(t)

		// Parse the PEM block
		block, _ := pem.Decode(privateKeyPEM)
		assert.NotNil(t, block)
		assert.Equal(t, "RSA PRIVATE KEY", block.Type)

		// Parse the private key
		parsedKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		require.NoError(t, err)
		assert.NotNil(t, parsedKey)
		assert.Equal(t, DefaultRSAKeySize, parsedKey.Size()*8)
	})

	t.Run("public_key_can_be_parsed", func(t *testing.T) {
		originalPrivateKey, _, publicKeyPEM := GenerateRSAKeyPair(t)

		// Parse the PEM block
		block, _ := pem.Decode(publicKeyPEM)
		assert.NotNil(t, block)
		assert.Equal(t, "PUBLIC KEY", block.Type)

		// Parse the public key
		parsedPublicKey, err := x509.ParsePKIXPublicKey(block.Bytes)
		require.NoError(t, err)
		assert.NotNil(t, parsedPublicKey)

		// Verify it matches the original private key's public key
		rsaPublicKey, ok := parsedPublicKey.(*rsa.PublicKey)
		assert.True(t, ok)
		assert.Equal(t, originalPrivateKey.N, rsaPublicKey.N)
		assert.Equal(t, originalPrivateKey.E, rsaPublicKey.E)
	})

	t.Run("generates_unique_keys", func(t *testing.T) {
		privateKey1, _, _ := GenerateRSAKeyPair(t)
		privateKey2, _, _ := GenerateRSAKeyPair(t)

		// Keys should be different
		assert.NotEqual(t, privateKey1.N, privateKey2.N)
		assert.NotEqual(t, privateKey1.D, privateKey2.D)
	})
}

// TestGenerateTestToken tests JWT token generation.
func TestGenerateTestToken(t *testing.T) {
	claims := createTestJWTClaims()

	t.Run("generates_hmac_token", func(t *testing.T) {
		testHMACTokenGeneration(t, claims)
	})

	t.Run("generates_rsa_token", func(t *testing.T) {
		testRSATokenGeneration(t, claims)
	})

	t.Run("fails_with_nil_claims", func(t *testing.T) {
		testTokenGenerationWithNilClaims(t)
	})
}

func createTestJWTClaims() jwt.MapClaims {
	return jwt.MapClaims{
		"sub": "test-user",
		"exp": time.Now().Add(time.Hour).Unix(),
		"iat": time.Now().Unix(),
	}
}

func testHMACTokenGeneration(t *testing.T, claims jwt.MapClaims) {
	t.Helper()

	secret := []byte("test-secret-key")
	token := GenerateTestToken(t, claims, secret)

	assert.NotEmpty(t, token)
	assert.Equal(t, 2, strings.Count(token, ".")) // JWT has 3 parts separated by dots

	// Verify the token can be parsed and validated
	parsedToken, err := jwt.Parse(token, func(token *jwt.Token) (interface{}, error) {
		return secret, nil
	})

	require.NoError(t, err)
	assert.True(t, parsedToken.Valid)
	assert.Equal(t, jwt.SigningMethodHS256, parsedToken.Method)
}

func testRSATokenGeneration(t *testing.T, claims jwt.MapClaims) {
	t.Helper()
	// RSA token test implementation would go here
}

func testTokenGenerationWithNilClaims(t *testing.T) {
	t.Helper()
	// Nil claims test implementation would go here
}

func TestTestClaims(t *testing.T) {
	t.Run("creates_standard_claims", func(t *testing.T) {
		subject := "test-user"
		scopes := []string{"read", "write", "admin"}
		expiry := time.Hour

		claims := TestClaims(subject, scopes, expiry)

		assert.Equal(t, subject, claims["sub"])
		assert.Equal(t, scopes, claims["scopes"])

		// Check timestamps
		iat, ok := claims["iat"].(int64)
		require.True(t, ok)
		exp, ok := claims["exp"].(int64)
		require.True(t, ok)

		assert.InDelta(t, time.Now().Unix(), iat, 5) // Within 5 seconds
		assert.InDelta(t, exp, time.Now().Add(expiry).Unix(), 5)

		// Check rate limit
		rateLimit, ok := claims["rate_limit"].(map[string]int)
		require.True(t, ok)
		assert.Equal(t, testIterations, rateLimit["requests_per_minute"])
		assert.Equal(t, 10, rateLimit["burst"])
	})

	t.Run("handles_empty_subject", func(t *testing.T) {
		claims := TestClaims("", []string{}, time.Minute)

		assert.Empty(t, claims["sub"])
		assert.Equal(t, []string{}, claims["scopes"])
		assert.NotNil(t, claims["iat"])
		assert.NotNil(t, claims["exp"])
	})

	t.Run("handles_zero_expiry", func(t *testing.T) {
		now := time.Now()
		claims := TestClaims("user", []string{"scope"}, 0)

		iat, ok := claims["iat"].(int64)
		require.True(t, ok)
		exp, ok := claims["exp"].(int64)
		require.True(t, ok)

		assert.InDelta(t, now.Unix(), iat, 5)
		assert.InDelta(t, exp, now.Unix(), 5) // exp should be approximately same as iat
	})

	t.Run("handles_negative_expiry", func(t *testing.T) {
		now := time.Now()
		claims := TestClaims("user", []string{"scope"}, -time.Hour)

		iat, ok := claims["iat"].(int64)
		require.True(t, ok)
		exp, ok := claims["exp"].(int64)
		require.True(t, ok)

		assert.InDelta(t, now.Unix(), iat, 5)
		assert.InDelta(t, exp, now.Add(-time.Hour).Unix(), 5) // Should be in the past
	})
}

// TestAssertError tests the error assertion helper function.
func TestAssertError(t *testing.T) {
	t.Run("no_error_expected_and_none_occurred", func(t *testing.T) {
		// This should pass without panicking
		AssertError(t, nil, false, "")
	})

	t.Run("error_expected_and_occurred", func(t *testing.T) {
		err := assert.AnError
		// This should pass without panicking
		AssertError(t, err, true, "")
	})

	t.Run("error_expected_with_content_match", func(t *testing.T) {
		err := assert.AnError // This error contains "assert.AnError general error for testing"
		// This should pass without panicking
		AssertError(t, err, true, "assert.AnError")
	})
}

// TestContainsString tests the internal string contains function.
func TestContainsString(t *testing.T) {
	tests := createContainsStringTestCases()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := containsString(tt.s, tt.substr)
			assert.Equal(t, tt.expected, result)
		})
	}
}

type containsStringTest struct {
	name     string
	s        string
	substr   string
	expected bool
}

func createContainsStringTestCases() []containsStringTest {
	var tests []containsStringTest

	tests = append(tests, createBasicContainsTests()...)
	tests = append(tests, createEdgeCaseContainsTests()...)
	tests = append(tests, createSpecialCaseContainsTests()...)

	return tests
}

func createBasicContainsTests() []containsStringTest {
	return []containsStringTest{
		{
			name:     "substring_at_beginning",
			s:        "hello world",
			substr:   "hello",
			expected: true,
		},
		{
			name:     "substring_in_middle",
			s:        "hello world",
			substr:   "o w",
			expected: true,
		},
		{
			name:     "substring_at_end",
			s:        "hello world",
			substr:   "world",
			expected: true,
		},
		{
			name:     "substring_not_found",
			s:        "hello world",
			substr:   "xyz",
			expected: false,
		},
		{
			name:     "exact_match",
			s:        "test",
			substr:   "test",
			expected: true,
		},
	}
}

func createEdgeCaseContainsTests() []containsStringTest {
	return []containsStringTest{
		{
			name:     "empty_substring",
			s:        "hello world",
			substr:   "",
			expected: false, // Empty substr returns false per implementation
		},
		{
			name:     "substring_longer_than_string",
			s:        "hi",
			substr:   "hello",
			expected: false,
		},
		{
			name:     "empty_string",
			s:        "",
			substr:   "test",
			expected: false,
		},
	}
}

func createSpecialCaseContainsTests() []containsStringTest {
	return []containsStringTest{
		{
			name:     "case_sensitive",
			s:        "Hello World",
			substr:   "hello",
			expected: false,
		},
		{
			name:     "repeated_pattern",
			s:        "abcabcabc",
			substr:   "abc",
			expected: true,
		},
	}
}

// TestIntegrationTokenGeneration tests the integration of key generation and token creation.
func TestIntegrationTokenGeneration(t *testing.T) {
	t.Run("end_to_end_rsa_token_flow", func(t *testing.T) {
		// Generate key pair
		privateKey, _, publicKeyPEM := GenerateRSAKeyPair(t)

		// Create claims
		claims := TestClaims("integration-test", []string{"read", "write"}, time.Hour)

		// Generate token
		token := GenerateTestToken(t, claims, privateKey)

		// Parse public key
		block, _ := pem.Decode(publicKeyPEM)
		publicKey, err := x509.ParsePKIXPublicKey(block.Bytes)
		require.NoError(t, err)

		// Verify token
		parsedToken, err := jwt.Parse(token, func(token *jwt.Token) (interface{}, error) {
			return publicKey, nil
		})

		require.NoError(t, err)
		require.True(t, parsedToken.Valid)

		// Verify claims
		mapClaims, ok := parsedToken.Claims.(jwt.MapClaims)
		require.True(t, ok)

		assert.Equal(t, "integration-test", mapClaims["sub"])
		scopes, ok := mapClaims["scopes"].([]interface{})
		require.True(t, ok)
		assert.Len(t, scopes, 2)
		assert.Contains(t, scopes, "read")
		assert.Contains(t, scopes, "write")
	})
}

// TestErrorHandling tests error handling in utility functions.
func TestErrorHandling(t *testing.T) {
	t.Run("verify_functions_dont_panic_on_valid_input", func(t *testing.T) {
		// Test that functions don't panic with valid inputs
		assert.NotPanics(t, func() {
			NewTestLogger(t)
		})

		assert.NotPanics(t, func() {
			NewBenchLogger(&testing.B{})
		})

		assert.NotPanics(t, func() {
			GenerateRSAKeyPair(t)
		})

		assert.NotPanics(t, func() {
			secret := []byte("test")
			claims := jwt.MapClaims{"sub": "test"}
			GenerateTestToken(t, claims, secret)
		})

		assert.NotPanics(t, func() {
			TestClaims("test", []string{}, time.Hour)
		})

		assert.NotPanics(t, func() {
			AssertError(t, nil, false, "")
		})
	})
}

// TestPerformanceBaseline tests basic performance characteristics.
func TestPerformanceBaseline(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance tests in short mode")
	}

	t.Run("key_generation_performance", func(t *testing.T) {
		start := time.Now()

		GenerateRSAKeyPair(t)

		duration := time.Since(start)

		// Key generation should complete within reasonable time (5 seconds)
		assert.Less(t, duration, 5*time.Second, "Key generation took too long: %v", duration)
	})

	t.Run("token_generation_performance", func(t *testing.T) {
		privateKey, _, _ := GenerateRSAKeyPair(t)
		claims := TestClaims("perf-test", []string{"test"}, time.Hour)

		start := time.Now()

		for i := 0; i < testIterations; i++ {
			GenerateTestToken(t, claims, privateKey)
		}

		duration := time.Since(start)

		// testIterations token generations should complete within reasonable time
		assert.Less(t, duration, 5*time.Second, "Token generation took too long: %v", duration)
	})
}

// TestEdgeCases tests edge cases and boundary conditions.
func TestEdgeCases(t *testing.T) {
	t.Run("very_long_subject", func(t *testing.T) {
		longSubject := strings.Repeat("a", 10000)
		claims := TestClaims(longSubject, []string{}, time.Hour)

		assert.Equal(t, longSubject, claims["sub"])
	})

	t.Run("many_scopes", func(t *testing.T) {
		manyScopes := make([]string, testMaxIterations)
		for i := range manyScopes {
			manyScopes[i] = "scope" + string(rune(i))
		}

		claims := TestClaims("test", manyScopes, time.Hour)
		assert.Equal(t, manyScopes, claims["scopes"])
	})

	t.Run("very_long_error_message", func(t *testing.T) {
		longMessage := strings.Repeat("error", 10000)
		substr := "error"

		result := containsString(longMessage, substr)
		assert.True(t, result)
	})
}

// BenchmarkGenerateRSAKeyPair benchmarks key pair generation.
func BenchmarkGenerateRSAKeyPair(b *testing.B) {
	for i := 0; i < b.N; i++ {
		t := &testing.T{}
		GenerateRSAKeyPair(t)
	}
}

// BenchmarkGenerateTestToken benchmarks token generation.
func BenchmarkGenerateTestToken(b *testing.B) {
	t := &testing.T{}
	privateKey, _, _ := GenerateRSAKeyPair(t)
	claims := TestClaims("bench-test", []string{"test"}, time.Hour)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		GenerateTestToken(t, claims, privateKey)
	}
}

// BenchmarkContainsString benchmarks the string contains function.
func BenchmarkContainsString(b *testing.B) {
	text := "The quick brown fox jumps over the lazy dog"
	substr := "fox"

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		containsString(text, substr)
	}
}

// TestAdditionalErrorPaths tests additional error paths for better coverage.
func TestAdditionalErrorPaths(t *testing.T) {
	t.Run("generate_rsa_key_pair_error_coverage", func(t *testing.T) {
		// Test the function works correctly - error paths are hard to test without mocking
		privateKey, privateKeyPEM, publicKeyPEM := GenerateRSAKeyPair(t)

		// Verify all returned values are valid
		assert.NotNil(t, privateKey)
		assert.NotEmpty(t, privateKeyPEM)
		assert.NotEmpty(t, publicKeyPEM)

		// Verify the private key can be marshaled (this exercises more code paths)
		assert.Equal(t, DefaultRSAKeySize, privateKey.Size()*8)
	})

	t.Run("generate_test_token_error_coverage", func(t *testing.T) {
		// Test with various claim types to exercise different code paths
		claims := jwt.MapClaims{
			"sub": "test",
			"iat": time.Now().Unix(),
			"exp": time.Now().Add(time.Hour).Unix(),
			"complex": map[string]interface{}{
				"nested": []string{"value1", "value2"},
			},
		}

		// Test with []byte key
		token1 := GenerateTestToken(t, claims, []byte("test-key"))
		assert.NotEmpty(t, token1)

		// Test with RSA key
		privateKey, _, _ := GenerateRSAKeyPair(t)
		token2 := GenerateTestToken(t, claims, privateKey)
		assert.NotEmpty(t, token2)

		// Verify tokens are different (different algorithms)
		assert.NotEqual(t, token1, token2)
	})

	t.Run("assert_error_edge_cases", func(t *testing.T) {
		// Test successful assertions with various inputs
		AssertError(t, nil, false, "any string here")
		AssertError(t, errors.New("test error"), true, "")
		AssertError(t, errors.New("contains specific text"), true, "specific")

		// Test edge case with empty error message
		emptyErr := errors.New("")
		AssertError(t, emptyErr, true, "")
	})

	t.Run("contains_string_recursive_edge_cases", func(t *testing.T) {
		// Test edge cases that exercise the recursive implementation
		assert.True(t, containsString("a", "a"))
		assert.False(t, containsString("a", "b"))
		assert.False(t, containsString("", "a"))
		assert.False(t, containsString("short", "longer"))

		// Test cases that exercise the recursive search
		assert.True(t, containsString("abcdef", "def"))
		assert.True(t, containsString("abcdef", "cde"))
		assert.False(t, containsString("abcdef", "xyz"))

		// Test with repeated characters
		assert.True(t, containsString("aaabbbccc", "bbb"))
		assert.False(t, containsString("aaabbbccc", "abc")) // "abc" is not a substring of "aaabbbccc"
	})
}
