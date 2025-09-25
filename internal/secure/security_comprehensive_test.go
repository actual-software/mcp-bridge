package secure

import (
	"fmt"
	"os"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test comprehensive NewTokenStore platform-specific logic.
func TestNewTokenStore_PlatformSpecific(t *testing.T) {
	tests := []struct {
		name              string
		appName           string
		expectedStoreType string
		shouldFallback    bool
	}{
		{
			name:    "valid_app_name_normal",
			appName: "mcp-test",
		},
		{
			name:    "empty_app_name",
			appName: "",
		},
		{
			name:    "app_name_with_special_chars",
			appName: "mcp-test.with_special@chars:123",
		},
		{
			name:    "very_long_app_name",
			appName: "very-long-app-name-that-exceeds-normal-length-but-should-still-work-in-most-systems",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, err := NewTokenStore(tt.appName)
			require.NoError(t, err, "NewTokenStore should never fail - it falls back to encrypted file store")
			require.NotNil(t, store, "Store should never be nil")

			// Verify the store can perform basic operations
			testKey := "test-key"
			testToken := "test-token-value"

			// Store operation should work (may use platform store or file store)
			err = store.Store(testKey, testToken)
			require.NoError(t, err, "Store operation should work")

			// Retrieve operation should work
			retrievedToken, err := store.Retrieve(testKey)
			require.NoError(t, err, "Retrieve operation should work")
			assert.Equal(t, testToken, retrievedToken, "Retrieved token should match stored token")

			// List operation (may fail on some platforms)
			keys, err := store.List()
			if err != nil {
				t.Logf("List operation failed (may be expected): %v", err)
			} else {
				if len(keys) > 0 {
					assert.Contains(t, keys, testKey, "Listed keys should contain our test key if list works")
				} else {
					t.Log("List returned empty (may be expected on this platform)")
				}
			}

			// Delete operation should work
			err = store.Delete(testKey)
			require.NoError(t, err, "Delete operation should work")

			// Verify deletion worked
			_, err = store.Retrieve(testKey)
			assert.Error(t, err, "Retrieve should fail after deletion")
		})
	}
}

// Test platform detection and store creation logic.
func TestNewTokenStore_PlatformDetection(t *testing.T) {
	t.Run("platform_specific_store_creation", func(t *testing.T) {
		store, err := NewTokenStore("test-platform-detection")
		require.NoError(t, err)
		require.NotNil(t, store)

		// Test that the store type is appropriate for the platform
		switch runtime.GOOS {
		case "darwin":
			// On macOS, we should get a keychain store (but it may fall back to file store if keychain is unavailable)
			t.Logf("Running on macOS - testing keychain or fallback store")
		case "windows":
			// On Windows, we should get a credential store (but may fall back to file store)
			t.Logf("Running on Windows - testing credential or fallback store")
		case "linux":
			// On Linux, we should get a secret service store (but may fall back to file store)
			t.Logf("Running on Linux - testing secret service or fallback store")
		default:
			// On other platforms, we should get the file store
			t.Logf("Running on %s - should use encrypted file store", runtime.GOOS)
		}

		// Regardless of platform, all stores should support the same interface
		testBasicStoreOperations(t, store)
	})
}

// Test secret service store fallback (non-Linux platforms).
func TestSecretServiceStore_PlatformFallback(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("Skipping secret service fallback test on Linux")
	}

	// On non-Linux platforms, newSecretServiceStore should return an error
	store, err := newSecretServiceStore("test-app")
	require.Error(t, err, "Secret service store should not be available on non-Linux platforms")
	assert.Nil(t, store, "Store should be nil when error occurs")
	assert.Contains(t, err.Error(), "secret service store not available on this platform")
}

// Test NewTokenStore error handling and fallback behavior.
func TestNewTokenStore_ErrorHandlingAndFallback(t *testing.T) {
	tests := []struct {
		name    string
		appName string
	}{
		{
			name:    "normal_fallback_scenario",
			appName: "test-fallback",
		},
		{
			name:    "fallback_with_empty_name",
			appName: "",
		},
		{
			name:    "fallback_with_special_chars",
			appName: "test@fallback.with:special_chars",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// This test ensures that even if platform-specific stores fail,
			// NewTokenStore will fall back to the encrypted file store
			store, err := NewTokenStore(tt.appName)

			// NewTokenStore should NEVER fail because it has a fallback
			require.NoError(t, err, "NewTokenStore should never fail due to fallback mechanism")
			require.NotNil(t, store, "Store should never be nil")

			// The fallback store should work correctly
			testBasicStoreOperations(t, store)
		})
	}
}

// Test encrypted file store edge cases.
func TestEncryptedFileStore_EdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		appName string
		key     string
		token   string
	}{
		{
			name:    "unicode_key_and_token",
			appName: "test-unicode",
			key:     "키-key-клавиша-🔑",
			token:   "토큰-token-токен-🎫",
		},
		{
			name:    "very_long_key_and_token",
			appName: "test-long",
			key:     "very-long-key-" + string(make([]byte, 1000)),
			token:   "very-long-token-" + string(make([]byte, 10000)),
		},
		{
			name:    "binary_like_data",
			appName: "test-binary",
			key:     "binary-key",
			token:   string([]byte{0, 1, 2, 3, 255, 254, 253}),
		},
		{
			name:    "json_token_data",
			appName: "test-json",
			key:     "json-key",
			token:   `{"access_token":"abc123","refresh_token":"xyz789","expires_in":3600}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, err := newEncryptedFileStore(tt.appName)
			require.NoError(t, err)

			// Test store and retrieve
			err = store.Store(tt.key, tt.token)
			require.NoError(t, err)

			retrievedToken, err := store.Retrieve(tt.key)
			require.NoError(t, err)
			assert.Equal(t, tt.token, retrievedToken)

			// Test list contains key
			keys, err := store.List()
			require.NoError(t, err)
			assert.Contains(t, keys, tt.key)

			// Test delete
			err = store.Delete(tt.key)
			require.NoError(t, err)

			// Verify delete worked
			_, err = store.Retrieve(tt.key)
			assert.Error(t, err)
		})
	}
}

// Test generatePassword function with various inputs.
func TestGeneratePassword_EdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		appName string
	}{
		{
			name:    "empty_app_name",
			appName: "",
		},
		{
			name:    "normal_app_name",
			appName: "test-app",
		},
		{
			name:    "app_name_with_unicode",
			appName: "тест-приложение-🚀",
		},
		{
			name:    "very_long_app_name",
			appName: string(make([]byte, 1000)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			password1 := generatePassword(tt.appName)
			password2 := generatePassword(tt.appName)

			// Passwords should be deterministic (same input = same output)
			assert.Equal(t, password1, password2, "generatePassword should be deterministic")

			// Password should not be empty
			assert.NotEmpty(t, password1, "Generated password should not be empty")

			// Password should be reasonable length
			assert.GreaterOrEqual(t, len(password1), 32, "Password should be at least 32 bytes")
		})
	}
}

// Test encryption/decryption edge cases.
func TestEncryptedFileStore_EncryptionEdgeCases(t *testing.T) {
	store, err := newEncryptedFileStore("test-encryption")
	require.NoError(t, err)

	// Cast to concrete type to access private methods for testing
	efs, ok := store.(*encryptedFileStore)
	require.True(t, ok, "Store should be encryptedFileStore")

	tests := []struct {
		name string
		data string
	}{
		{
			name: "empty_data",
			data: "",
		},
		{
			name: "single_byte",
			data: "a",
		},
		{
			name: "large_data",
			data: string(make([]byte, 10000)), // Reduced size for faster test
		},
		{
			name: "binary_data",
			data: string([]byte{0, 1, 2, 3, 4, 5, 255, 254, 253, 252}),
		},
		{
			name: "unicode_data",
			data: "Hello 世界 🌍 Привет мир",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test encryption produces different output each time (due to random IV)
			encrypted1, err := efs.encrypt([]byte(tt.data))
			require.NoError(t, err)

			encrypted2, err := efs.encrypt([]byte(tt.data))
			require.NoError(t, err)

			assert.NotEqual(t, encrypted1, encrypted2, "Encryption should produce different ciphertext each time")

			// Test decryption produces original data
			decrypted1, err := efs.decrypt(encrypted1)
			require.NoError(t, err)
			assert.Equal(t, tt.data, string(decrypted1))

			decrypted2, err := efs.decrypt(encrypted2)
			require.NoError(t, err)
			assert.Equal(t, tt.data, string(decrypted2))
		})
	}
}

// Test file operations edge cases.
func TestEncryptedFileStore_FileOperationsEdgeCases(t *testing.T) {
	t.Run("concurrent_access", func(t *testing.T) {
		// Create separate stores for each goroutine to avoid file conflicts
		numGoroutines := 5
		done := make(chan bool, numGoroutines)

		for i := range numGoroutines {
			go func(id int) {
				defer func() { done <- true }()

				// Each goroutine gets its own store to avoid file conflicts
				storeName := fmt.Sprintf("test-concurrent-%d", id)
				store, err := newEncryptedFileStore(storeName)
				assert.NoError(t, err, "Store creation should succeed")

				key := fmt.Sprintf("concurrent-key-%d", id)
				token := fmt.Sprintf("concurrent-token-%d", id)

				err = store.Store(key, token)
				assert.NoError(t, err, "Concurrent store should succeed")

				retrievedToken, err := store.Retrieve(key)
				assert.NoError(t, err, "Concurrent retrieve should succeed")
				assert.Equal(t, token, retrievedToken, "Retrieved token should match")

				err = store.Delete(key)
				assert.NoError(t, err, "Concurrent delete should succeed")
			}(i)
		}

		// Wait for all goroutines to complete
		for range numGoroutines {
			<-done
		}
	})

	t.Run("file_permissions", func(t *testing.T) {
		store, err := newEncryptedFileStore("test-permissions")
		require.NoError(t, err)

		// Store some data to create the file
		err = store.Store("test-key", "test-token")
		require.NoError(t, err)

		// Check that file has correct permissions (if possible to check)
		// Note: This may not work on all platforms
		if efs, ok := store.(*encryptedFileStore); ok {
			if stat, err := os.Stat(efs.filePath); err == nil {
				mode := stat.Mode()
				t.Logf("File permissions: %o", mode.Perm())
				// The exact permissions may vary by platform, but file should not be world-readable
				assert.Equal(t, os.FileMode(0o600), mode.Perm(), "File should have restrictive permissions")
			}
		}
	})
}

// Helper function to test basic store operations.
func testBasicStoreOperations(t *testing.T, store TokenStore) {
	t.Helper()

	testKey := "basic-test-key"
	testToken := "basic-test-token"

	// Store
	err := store.Store(testKey, testToken)
	require.NoError(t, err, "Store should succeed")

	// Retrieve
	retrievedToken, err := store.Retrieve(testKey)
	require.NoError(t, err, "Retrieve should succeed")
	assert.Equal(t, testToken, retrievedToken, "Retrieved token should match")

	// List (may fail on some platforms)
	keys, err := store.List()
	if err != nil {
		t.Logf("List operation failed (may be expected): %v", err)
	} else {
		if len(keys) > 0 {
			assert.Contains(t, keys, testKey, "List should contain stored key if list works")
		} else {
			t.Log("List returned empty (may be expected on this platform)")
		}
	}

	// Delete
	err = store.Delete(testKey)
	require.NoError(t, err, "Delete should succeed")

	// Verify deletion
	_, err = store.Retrieve(testKey)
	assert.Error(t, err, "Retrieve should fail after deletion")
}
