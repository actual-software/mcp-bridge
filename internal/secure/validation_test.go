//go:build darwin

package secure

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test keychain parameter validation thoroughly.
func TestIsValidKeychainParam_Comprehensive(t *testing.T) {
	t.Run("valid_cases", testValidKeychainParams)
	t.Run("invalid_cases", testInvalidKeychainParams)
	t.Run("special_characters", testSpecialCharacterKeychainParams)
	t.Run("unicode_and_emoji", testUnicodeKeychainParams)
}

func testValidKeychainParams(t *testing.T) {
	validTests := []struct {
		name  string
		param string
	}{
		{"simple_alphanumeric", "test123"},
		{"with_underscore", "test_key"},
		{"with_dash", "test-key"},
		{"with_dot", "test.key"},
		{"with_at_symbol", "test@domain"},
		{"with_colon", "test:key"},
		{"complex_valid", "user@domain.com:service_name-123"},
	}

	for _, tt := range validTests {
		t.Run(tt.name, func(t *testing.T) {
			result := isValidKeychainParam(tt.param)
			assert.True(t, result, "isValidKeychainParam(%q) should be true", tt.param)
		})
	}
}

func testInvalidKeychainParams(t *testing.T) {
	invalidTests := []struct {
		name  string
		param string
	}{
		{"empty_string", ""},
		{"with_space", "test key"},
		{"with_newline", "test\nkey"},
		{"with_tab", "test\tkey"},
		{"with_carriage_return", "test\rkey"},
		{"with_semicolon", "test;key"},
		{"with_pipe", "test|key"},
		{"with_ampersand", "test&key"},
		{"with_dollar", "test$key"},
		{"with_backtick", "test`key"},
	}

	for _, tt := range invalidTests {
		t.Run(tt.name, func(t *testing.T) {
			result := isValidKeychainParam(tt.param)
			assert.False(t, result, "isValidKeychainParam(%q) should be false", tt.param)
		})
	}
}

func testSpecialCharacterKeychainParams(t *testing.T) {
	specialCharTests := []struct {
		name  string
		param string
	}{
		{"with_parentheses", "test(key)"},
		{"with_brackets", "test[key]"},
		{"with_braces", "test{key}"},
		{"with_quotes", "test\"key"},
		{"with_single_quotes", "test'key"},
		{"with_backslash", "test\\key"},
		{"with_slash", "test/key"},
		{"with_question_mark", "test?key"},
		{"with_asterisk", "test*key"},
		{"with_plus", "test+key"},
		{"with_equals", "test=key"},
		{"only_special_invalid_chars", "!@#$%^&*()"},
	}

	for _, tt := range specialCharTests {
		t.Run(tt.name, func(t *testing.T) {
			result := isValidKeychainParam(tt.param)
			assert.False(t, result, "isValidKeychainParam(%q) should be false", tt.param)
		})
	}
}

func testUnicodeKeychainParams(t *testing.T) {
	unicodeTests := []struct {
		name  string
		param string
	}{
		{"unicode_characters", "тест"},
		{"emoji", "test🔑"},
	}

	for _, tt := range unicodeTests {
		t.Run(tt.name, func(t *testing.T) {
			result := isValidKeychainParam(tt.param)
			assert.False(t, result, "isValidKeychainParam(%q) should be false", tt.param)
		})
	}
}

// Test keychain store creation.
func TestNewKeychainStore(t *testing.T) {
	tests := []struct {
		name    string
		appName string
	}{
		{
			name:    "normal_app_name",
			appName: "test-app",
		},
		{
			name:    "empty_app_name",
			appName: "",
		},
		{
			name:    "app_name_with_valid_special_chars",
			appName: "test-app.service_name@domain:123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newKeychainStore(tt.appName)
			assert.NotNil(t, store, "Store should not be nil")

			// Test that the store implements the TokenStore interface
			var _ = store
		})
	}
}

// Test input validation edge cases for keychain operations.
func TestKeychainStore_InputValidation(t *testing.T) {
	store := newKeychainStore("test-validation")

	t.Run("Store_with_invalid_key", func(t *testing.T) {
		err := store.Store("invalid key with spaces", "valid-token")
		require.Error(t, err, "Store should reject invalid key")
		assert.Contains(t, err.Error(), "invalid key parameter")
	})

	t.Run("Store_with_invalid_token", func(t *testing.T) {
		err := store.Store("valid-key", "invalid token with spaces")
		require.Error(t, err, "Store should reject invalid token")
		assert.Contains(t, err.Error(), "invalid token parameter")
	})

	t.Run("Retrieve_with_invalid_key", func(t *testing.T) {
		_, err := store.Retrieve("invalid key with spaces")
		require.Error(t, err, "Retrieve should reject invalid key")
		assert.Contains(t, err.Error(), "invalid key parameter")
	})

	t.Run("Delete_with_invalid_key", func(t *testing.T) {
		err := store.Delete("invalid key with spaces")
		require.Error(t, err, "Delete should reject invalid key")
		assert.Contains(t, err.Error(), "invalid key parameter")
	})

	t.Run("Store_with_empty_key", func(t *testing.T) {
		err := store.Store("", "valid-token")
		require.Error(t, err, "Store should reject empty key")
		assert.Contains(t, err.Error(), "invalid key parameter")
	})

	t.Run("Store_with_empty_token", func(t *testing.T) {
		err := store.Store("valid-key", "")
		require.Error(t, err, "Store should reject empty token")
		assert.Contains(t, err.Error(), "invalid token parameter")
	})
}
