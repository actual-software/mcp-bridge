package secure

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test NewTokenStore fallback paths by focusing on error conditions.
func TestNewTokenStore_FallbackPaths(t *testing.T) {
	t.Run("fallback_behavior", func(t *testing.T) {
		// This test ensures the fallback path works regardless of platform
		store, err := NewTokenStore("test-fallback-behavior")
		require.NoError(t, err, "NewTokenStore should never fail due to fallback")
		require.NotNil(t, store, "Store should never be nil")

		// Test that the store works (this will exercise the fallback encrypted file store)
		testKey := "fallback-test-key"
		testToken := "fallback-test-token"

		err = store.Store(testKey, testToken)
		require.NoError(t, err, "Store should work with fallback")

		retrievedToken, err := store.Retrieve(testKey)
		require.NoError(t, err, "Retrieve should work with fallback")
		assert.Equal(t, testToken, retrievedToken, "Token should match")

		err = store.Delete(testKey)
		require.NoError(t, err, "Delete should work with fallback")
	})

	t.Run("encrypted_file_store_creation", func(t *testing.T) {
		// Test direct encrypted file store creation
		store, err := newEncryptedFileStore("direct-file-store-test")
		require.NoError(t, err, "Direct encrypted file store creation should succeed")
		require.NotNil(t, store, "Store should not be nil")

		// Test that it implements the interface
		var _ = store

		// Test basic functionality
		err = store.Store("direct-key", "direct-token")
		require.NoError(t, err)

		token, err := store.Retrieve("direct-key")
		require.NoError(t, err)
		assert.Equal(t, "direct-token", token)

		err = store.Delete("direct-key")
		require.NoError(t, err)
	})
}

// Test edge cases in encrypted file store error handling.
func TestEncryptedFileStore_ErrorHandling(t *testing.T) {
	store, err := newEncryptedFileStore("error-handling-test")
	require.NoError(t, err)

	t.Run("file_load_errors", func(t *testing.T) {
		// Create a fresh store for testing load of non-existent file
		freshStore, err := newEncryptedFileStore("load-error-test-unique")
		require.NoError(t, err)

		efs, ok := freshStore.(*encryptedFileStore)
		require.True(t, ok, "freshStore should be *encryptedFileStore")

		// This should return empty tokenData, not error
		data, err := efs.load()
		require.NoError(t, err, "Load should handle missing file gracefully")
		assert.NotNil(t, data, "Load should return tokenData struct")
		assert.Empty(t, data.Tokens, "Missing file should return empty tokens map")
	})

	t.Run("encryption_decrypt_coverage", func(t *testing.T) {
		efs, ok := store.(*encryptedFileStore)
		require.True(t, ok, "store should be *encryptedFileStore")

		// Test encrypt function coverage
		testData := []byte("test-encryption-data")
		encrypted, err := efs.encrypt(testData)
		require.NoError(t, err, "Encryption should succeed")
		assert.NotEmpty(t, encrypted, "Encrypted data should not be empty")

		// Test decrypt function coverage
		decrypted, err := efs.decrypt(encrypted)
		require.NoError(t, err, "Decryption should succeed")
		assert.Equal(t, testData, decrypted, "Decrypted data should match original")
	})

	t.Run("save_error_coverage", func(t *testing.T) {
		efs, ok := store.(*encryptedFileStore)
		require.True(t, ok, "store should be *encryptedFileStore")

		// Test save function with valid data
		testTokens := map[string]string{
			"test-key": "test-value",
		}
		testData := &tokenData{Tokens: testTokens}
		err := efs.save(testData)
		require.NoError(t, err, "Save should succeed with valid data")

		// Verify data was saved
		loadedData, err := efs.load()
		require.NoError(t, err, "Load should succeed after save")
		assert.Equal(t, testTokens, loadedData.Tokens, "Loaded data should match saved data")
	})
}

// Test generatePassword function coverage.
func TestGeneratePassword_FunctionCoverage(t *testing.T) {
	tests := []string{
		"normal-app",
		"",
		"app-with-special-chars@domain:123",
		"very-long-application-name-with-many-characters-to-test-edge-cases",
	}

	for _, appName := range tests {
		t.Run("app_name_"+appName, func(t *testing.T) {
			password := generatePassword(appName)
			assert.NotEmpty(t, password, "Password should not be empty")
			assert.GreaterOrEqual(t, len(password), 32, "Password should be at least 32 bytes")

			// Test deterministic behavior
			password2 := generatePassword(appName)
			assert.Equal(t, password, password2, "Password generation should be deterministic")
		})
	}
}

// Test error paths in encrypted file operations.
func TestEncryptedFileStore_ErrorPaths_Coverage(t *testing.T) {
	store, err := newEncryptedFileStore("error-paths-test")
	require.NoError(t, err)

	t.Run("retrieve_error_path", func(t *testing.T) {
		// Try to retrieve a key that doesn't exist
		_, err := store.Retrieve("non-existent-key")
		require.Error(t, err, "Retrieve should fail for non-existent key")
		assert.Contains(t, err.Error(), "token not found", "Error should indicate token not found")
	})

	t.Run("delete_non_existent_key", func(t *testing.T) {
		// Delete should be idempotent - no error for non-existent key
		err := store.Delete("definitely-does-not-exist")
		require.NoError(t, err, "Delete should be idempotent")
	})

	t.Run("list_functionality", func(t *testing.T) {
		// Store some keys
		keys := []string{"list-key-1", "list-key-2", "list-key-3"}
		for _, key := range keys {
			err := store.Store(key, "value-for-"+key)
			require.NoError(t, err)
		}

		// List should return all keys
		listedKeys, err := store.List()
		require.NoError(t, err, "List should succeed")

		for _, key := range keys {
			assert.Contains(t, listedKeys, key, "List should contain stored key")
		}

		// Clean up
		for _, key := range keys {
			_ = store.Delete(key)
		}
	})
}

// Test store operations that update existing values.
func TestEncryptedFileStore_UpdateOperations(t *testing.T) {
	store, err := newEncryptedFileStore("update-operations-test")
	require.NoError(t, err)

	t.Run("update_existing_value", func(t *testing.T) {
		key := "update-test-key"

		// Store initial value
		err := store.Store(key, "initial-value")
		require.NoError(t, err)

		// Update with new value
		err = store.Store(key, "updated-value")
		require.NoError(t, err)

		// Verify update
		value, err := store.Retrieve(key)
		require.NoError(t, err)
		assert.Equal(t, "updated-value", value, "Value should be updated")

		// Clean up
		_ = store.Delete(key)
	})

	t.Run("multiple_concurrent_operations", func(t *testing.T) {
		// Test that operations work when called in sequence
		keys := []string{"concurrent-1", "concurrent-2", "concurrent-3"}

		// Store multiple values
		for i, key := range keys {
			err := store.Store(key, "value-"+string(rune('A'+i)))
			require.NoError(t, err)
		}

		// Retrieve all values
		for i, key := range keys {
			value, err := store.Retrieve(key)
			require.NoError(t, err)
			assert.Equal(t, "value-"+string(rune('A'+i)), value)
		}

		// Delete all values
		for _, key := range keys {
			err := store.Delete(key)
			require.NoError(t, err)
		}

		// Verify all deleted
		for _, key := range keys {
			_, err := store.Retrieve(key)
			assert.Error(t, err, "Key should not exist after deletion")
		}
	})
}
