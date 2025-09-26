package secure

import (
	"fmt"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test NewTokenStore platform branches thoroughly.
func TestNewTokenStore_PlatformBranches(t *testing.T) {
	// Test on current platform - this will hit the actual platform branch
	store, err := NewTokenStore("platform-branch-test")
	require.NoError(t, err)
	require.NotNil(t, store)

	// Test basic functionality to ensure the platform-specific store works
	testKey := "platform-test-key"
	testToken := "platform-test-token"

	err = store.Store(testKey, testToken)
	require.NoError(t, err)

	retrievedToken, err := store.Retrieve(testKey)
	require.NoError(t, err)
	assert.Equal(t, testToken, retrievedToken)

	err = store.Delete(testKey)
	require.NoError(t, err)

	// Log which platform we're testing
	t.Logf("Tested NewTokenStore platform branch for: %s", runtime.GOOS)
}

// Test error conditions in encrypted file store operations.
func TestEncryptedFileStore_ErrorPaths(t *testing.T) {
	t.Run("malformed_encrypted_data", func(t *testing.T) {
		store, err := newEncryptedFileStore("test-malformed")
		require.NoError(t, err)

		efs, ok := store.(*encryptedFileStore)
		require.True(t, ok, "store should be *encryptedFileStore")

		// Test decrypt with invalid data
		_, err = efs.decrypt([]byte("not-valid-encrypted-data"))
		require.Error(t, err, "Decrypt should fail with invalid data")

		// Test decrypt with empty data
		_, err = efs.decrypt([]byte{})
		require.Error(t, err, "Decrypt should fail with empty data")

		// Test decrypt with short data (less than nonce size)
		_, err = efs.decrypt([]byte{1, 2, 3})
		assert.Error(t, err, "Decrypt should fail with too-short data")
	})

	t.Run("file_system_errors", func(t *testing.T) {
		// Test with invalid path that would cause filesystem errors
		store, err := newEncryptedFileStore("test-invalid-path/with/deep/nonexistent/dirs")
		require.NoError(t, err)

		// Trying to store should create directories and work
		err = store.Store("test-key", "test-value")
		require.NoError(t, err, "Store should create directories as needed")

		// Clean up
		if err := store.Delete("test-key"); err != nil {
			t.Logf("cleanup warning: failed to delete test key: %v", err)
		}
	})

	t.Run("retrieve_nonexistent_key", func(t *testing.T) {
		store, err := newEncryptedFileStore("test-nonexistent")
		require.NoError(t, err)

		// Retrieve from non-existent key
		_, err = store.Retrieve("definitely-does-not-exist")
		require.Error(t, err, "Retrieve should fail for non-existent key")
		assert.Contains(t, err.Error(), "token not found", "Error should indicate token not found")
	})

	t.Run("delete_nonexistent_key", func(t *testing.T) {
		store, err := newEncryptedFileStore("test-delete-nonexistent")
		require.NoError(t, err)

		// Delete non-existent key should not error (idempotent)
		err = store.Delete("definitely-does-not-exist")
		require.NoError(t, err, "Delete should be idempotent for non-existent keys")
	})

	t.Run("list_empty_store", func(t *testing.T) {
		store, err := newEncryptedFileStore("test-list-empty")
		require.NoError(t, err)

		keys, err := store.List()
		require.NoError(t, err, "List should work on empty store")
		assert.Empty(t, keys, "Empty store should return empty list")
	})
}

// Test edge cases in password generation.
func TestGeneratePassword_CompleteEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		appName  string
		testFunc func(t *testing.T, password []byte)
	}{
		{
			name:    "empty_app_name",
			appName: "",
			testFunc: func(t *testing.T, password []byte) {
				t.Helper()
				assert.NotEmpty(t, password, "Password should not be empty even with empty app name")
				assert.GreaterOrEqual(t, len(password), 32, "Password should be at least 32 bytes")
			},
		},
		{
			name:    "unicode_app_name",
			appName: "تطبيق-测试-アプリ-🚀",
			testFunc: func(t *testing.T, password []byte) {
				t.Helper()
				assert.NotEmpty(t, password, "Password should handle unicode app names")
				assert.GreaterOrEqual(t, len(password), 32, "Password should be at least 32 bytes")
			},
		},
		{
			name:    "very_long_app_name",
			appName: string(make([]byte, 10000)),
			testFunc: func(t *testing.T, password []byte) {
				t.Helper()
				assert.NotEmpty(t, password, "Password should handle very long app names")
				assert.GreaterOrEqual(t, len(password), 32, "Password should be at least 32 bytes")
			},
		},
		{
			name:    "binary_app_name",
			appName: string([]byte{0, 1, 2, 3, 255, 254, 253}),
			testFunc: func(t *testing.T, password []byte) {
				t.Helper()
				assert.NotEmpty(t, password, "Password should handle binary app names")
				assert.GreaterOrEqual(t, len(password), 32, "Password should be at least 32 bytes")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			password := generatePassword(tt.appName)
			tt.testFunc(t, password)

			// Test deterministic property
			password2 := generatePassword(tt.appName)
			assert.Equal(t, password, password2, "Password generation should be deterministic")
		})
	}
}

// Test error conditions and edge cases in file operations.
func TestEncryptedFileStore_FileEdgeCases(t *testing.T) {
	t.Run("store_and_overwrite", testStoreAndOverwrite)
	t.Run("multiple_keys_operations", testMultipleKeysOperations)
	t.Run("store_with_existing_directory", testStoreWithExistingDirectory)
}

func testStoreAndOverwrite(t *testing.T) {
	store, err := newEncryptedFileStore("test-overwrite")
	require.NoError(t, err)

	key := "overwrite-key"

	// Store initial value
	err = store.Store(key, "initial-value")
	require.NoError(t, err)

	// Overwrite with new value
	err = store.Store(key, "new-value")
	require.NoError(t, err)

	// Verify new value
	value, err := store.Retrieve(key)
	require.NoError(t, err)
	assert.Equal(t, "new-value", value)

	// Clean up
	if err := store.Delete(key); err != nil {
		t.Logf("cleanup warning: failed to delete key: %v", err)
	}
}

func testMultipleKeysOperations(t *testing.T) {
	store, err := newEncryptedFileStore("test-multiple")
	require.NoError(t, err)

	// Store multiple keys
	keys := []string{"key1", "key2", "key3", "key4", "key5"}
	for i, key := range keys {
		err = store.Store(key, fmt.Sprintf("value%d", i+1))
		require.NoError(t, err)
	}

	// List all keys
	listedKeys, err := store.List()
	require.NoError(t, err)
	assert.Len(t, listedKeys, len(keys))

	for _, key := range keys {
		assert.Contains(t, listedKeys, key)
	}

	// Retrieve all keys
	for i, key := range keys {
		value, err := store.Retrieve(key)
		require.NoError(t, err)
		assert.Equal(t, fmt.Sprintf("value%d", i+1), value)
	}

	// Delete all keys
	for _, key := range keys {
		err = store.Delete(key)
		require.NoError(t, err)
	}

	// Verify all deleted
	listedKeys, err = store.List()
	require.NoError(t, err)
	assert.Empty(t, listedKeys)
}

func testStoreWithExistingDirectory(t *testing.T) {
	// First, create a store to establish the directory
	store1, err := newEncryptedFileStore("test-existing-dir")
	require.NoError(t, err)
	err = store1.Store("temp-key", "temp-value")
	require.NoError(t, err)

	// Now create a second store with the same name (directory already exists)
	store2, err := newEncryptedFileStore("test-existing-dir")
	require.NoError(t, err)

	// Should be able to retrieve the value from the existing file
	value, err := store2.Retrieve("temp-key")
	require.NoError(t, err)
	assert.Equal(t, "temp-value", value)

	// Clean up
	if err := store2.Delete("temp-key"); err != nil {
		t.Logf("cleanup warning: failed to delete temp key: %v", err)
	}
}

// Test encryption/decryption error paths.
func TestEncryptedFileStore_EncryptionErrorPaths(t *testing.T) {
	store, err := newEncryptedFileStore("test-encryption-errors")
	require.NoError(t, err)

	efs, ok := store.(*encryptedFileStore)
	require.True(t, ok, "store should be *encryptedFileStore")

	t.Run("decrypt_corrupted_data", func(t *testing.T) {
		// Create valid encrypted data first
		validData, err := efs.encrypt([]byte("test data"))
		require.NoError(t, err)

		// Corrupt the data by modifying some bytes
		corruptedData := make([]byte, len(validData))
		copy(corruptedData, validData)

		// Corrupt the last few bytes (which would be the auth tag)
		if len(corruptedData) > 5 {
			corruptedData[len(corruptedData)-1] ^= 0xFF
			corruptedData[len(corruptedData)-2] ^= 0xFF
		}

		// Try to decrypt corrupted data
		_, err = efs.decrypt(corruptedData)
		assert.Error(t, err, "Decrypt should fail with corrupted data")
	})

	t.Run("encrypt_large_data", func(t *testing.T) {
		// Test encryption with large data
		largeData := make([]byte, 1024*1024) // 1MB
		for i := range largeData {
			largeData[i] = byte(i % 256)
		}

		encrypted, err := efs.encrypt(largeData)
		require.NoError(t, err, "Should encrypt large data")

		decrypted, err := efs.decrypt(encrypted)
		require.NoError(t, err, "Should decrypt large data")

		assert.Equal(t, largeData, decrypted, "Large data should roundtrip correctly")
	})
}

// Test NewTokenStore with different app names to cover all branches.
func TestNewTokenStore_AllPlatformPaths(t *testing.T) {
	// Test various app names that might trigger different code paths
	appNames := []string{
		"normal-app",
		"", // empty name
		"app.with.dots",
		"app_with_underscores",
		"app-with-dashes",
		"app@with.special:chars",
		"very-long-application-name-that-might-cause-issues-in-some-stores",
	}

	for _, appName := range appNames {
		t.Run("app_name_"+appName, func(t *testing.T) {
			if appName == "" {
				t.Log("Testing empty app name")
			}

			// This should always succeed due to fallback
			store, err := NewTokenStore(appName)
			require.NoError(t, err, "NewTokenStore should never fail")
			require.NotNil(t, store, "Store should never be nil")

			// Test that it actually works
			testKey := "branch-test-key"
			testValue := "branch-test-value"

			err = store.Store(testKey, testValue)
			require.NoError(t, err, "Store should work")

			value, err := store.Retrieve(testKey)
			require.NoError(t, err, "Retrieve should work")
			assert.Equal(t, testValue, value, "Value should match")

			err = store.Delete(testKey)
			require.NoError(t, err, "Delete should work")
		})
	}
}
