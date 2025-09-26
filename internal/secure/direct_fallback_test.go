package secure

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test direct encrypted file store creation to improve coverage.
func TestDirectEncryptedFileStore(t *testing.T) {
	t.Run("direct_creation", func(t *testing.T) {
		// This directly tests the fallback path that NewTokenStore uses
		store, err := newEncryptedFileStore("direct-test")
		require.NoError(t, err, "Direct encrypted file store creation should succeed")
		require.NotNil(t, store, "Store should not be nil")

		// Test that it implements the interface
		var _ = store

		// Test comprehensive operations to improve coverage
		testOps := []struct {
			key   string
			value string
		}{
			{"direct-key-1", "direct-value-1"},
			{"direct-key-2", "direct-value-2"},
			{"unicode-key-こんにちは", "unicode-value-世界"},
			{"special-chars@key:123", "special-value@123"},
		}

		// Store all values
		for _, op := range testOps {
			err := store.Store(op.key, op.value)
			require.NoError(t, err, "Store should succeed for key: %s", op.key)
		}

		// Retrieve all values
		for _, op := range testOps {
			value, err := store.Retrieve(op.key)
			require.NoError(t, err, "Retrieve should succeed for key: %s", op.key)
			assert.Equal(t, op.value, value, "Value should match for key: %s", op.key)
		}

		// List all values
		keys, err := store.List()
		require.NoError(t, err, "List should succeed")
		assert.Len(t, keys, len(testOps), "List should return all stored keys")

		for _, op := range testOps {
			assert.Contains(t, keys, op.key, "List should contain key: %s", op.key)
		}

		// Update a value
		err = store.Store(testOps[0].key, "updated-value")
		require.NoError(t, err, "Update should succeed")

		value, err := store.Retrieve(testOps[0].key)
		require.NoError(t, err, "Retrieve after update should succeed")
		assert.Equal(t, "updated-value", value, "Value should be updated")

		// Delete all values
		for _, op := range testOps {
			err := store.Delete(op.key)
			require.NoError(t, err, "Delete should succeed for key: %s", op.key)
		}

		// Verify all deleted
		for _, op := range testOps {
			_, err := store.Retrieve(op.key)
			require.Error(t, err, "Retrieve should fail after deletion for key: %s", op.key)
		}

		// List should be empty
		keys, err = store.List()
		require.NoError(t, err, "List should succeed even when empty")
		assert.Empty(t, keys, "List should be empty after all deletions")
	})
}

// Test error paths in encrypted operations to improve coverage.
func TestEncryptedFileStore_ErrorCoverage(t *testing.T) {
	store, err := newEncryptedFileStore("error-coverage-test")
	require.NoError(t, err)

	efs, ok := store.(*encryptedFileStore)
	require.True(t, ok, "store should be *encryptedFileStore")

	t.Run("encrypt_decrypt_edge_cases", func(t *testing.T) { testEncryptDecryptEdgeCases(t, efs) })
	t.Run("file_operations_coverage", func(t *testing.T) { testFileOperationsCoverage(t, efs) })
	t.Run("password_generation_coverage", testPasswordGenerationCoverage)
}

func testEncryptDecryptEdgeCases(t *testing.T, efs *encryptedFileStore) {
	t.Helper()
	// Test with empty data
	emptyData := []byte{}
	encrypted, err := efs.encrypt(emptyData)
	require.NoError(t, err, "Encrypt empty data should succeed")

	decrypted, err := efs.decrypt(encrypted)
	require.NoError(t, err, "Decrypt empty data should succeed")
	assert.Empty(t, decrypted, "Empty data should roundtrip to empty")

	// Test with very small data
	smallData := []byte("a")
	encrypted, err = efs.encrypt(smallData)
	require.NoError(t, err, "Encrypt small data should succeed")

	decrypted, err = efs.decrypt(encrypted)
	require.NoError(t, err, "Decrypt small data should succeed")
	assert.Equal(t, smallData, decrypted, "Small data should roundtrip")

	// Test with medium data
	mediumData := make([]byte, 1024)
	for i := range mediumData {
		mediumData[i] = byte(i % 256)
	}

	encrypted, err = efs.encrypt(mediumData)
	require.NoError(t, err, "Encrypt medium data should succeed")

	decrypted, err = efs.decrypt(encrypted)
	require.NoError(t, err, "Decrypt medium data should succeed")
	assert.Equal(t, mediumData, decrypted, "Medium data should roundtrip")
}

func testFileOperationsCoverage(t *testing.T, efs *encryptedFileStore) {
	t.Helper()
	// Test saving and loading
	testData := &tokenData{
		Tokens: map[string]string{
			"coverage-key-1": "coverage-value-1",
			"coverage-key-2": "coverage-value-2",
		},
	}

	err := efs.save(testData)
	require.NoError(t, err, "Save should succeed")

	loadedData, err := efs.load()
	require.NoError(t, err, "Load should succeed after save")
	assert.Equal(t, testData.Tokens, loadedData.Tokens, "Loaded data should match saved data")

	// Test overwriting
	newTestData := &tokenData{
		Tokens: map[string]string{
			"new-key": "new-value",
		},
	}

	err = efs.save(newTestData)
	require.NoError(t, err, "Save should succeed when overwriting")

	loadedData, err = efs.load()
	require.NoError(t, err, "Load should succeed after overwrite")
	assert.Equal(t, newTestData.Tokens, loadedData.Tokens, "Loaded data should match new saved data")
}

func testPasswordGenerationCoverage(t *testing.T) {
	// Test password generation with different inputs to improve coverage
	appNames := []string{
		"coverage-test-1",
		"coverage-test-2",
		"", // empty name
		"test@coverage.com:123",
	}

	for _, appName := range appNames {
		password := generatePassword(appName)
		assert.NotEmpty(t, password, "Password should not be empty for app: %s", appName)
		assert.GreaterOrEqual(t, len(password), 32, "Password should be at least 32 bytes for app: %s", appName)

		// Test deterministic behavior
		password2 := generatePassword(appName)
		assert.Equal(t, password, password2, "Password should be deterministic for app: %s", appName)
	}
}

// Test specific edge cases to reach higher coverage.
func TestEncryptedFileStore_SpecificCoverage(t *testing.T) {
	t.Run("multiple_stores_same_app", func(t *testing.T) {
		// Test that multiple stores with the same app name work correctly
		store1, err := newEncryptedFileStore("same-app-test")
		require.NoError(t, err)

		store2, err := newEncryptedFileStore("same-app-test")
		require.NoError(t, err)

		// Store in first store
		err = store1.Store("shared-key", "value-from-store1")
		require.NoError(t, err)

		// Should be able to retrieve from second store (same underlying file)
		value, err := store2.Retrieve("shared-key")
		require.NoError(t, err)
		assert.Equal(t, "value-from-store1", value, "Second store should see data from first store")

		// Clean up
		_ = store1.Delete("shared-key")
	})

	t.Run("error_handling_paths", func(t *testing.T) {
		store, err := newEncryptedFileStore("error-handling-paths")
		require.NoError(t, err)

		// Test retrieving non-existent key
		_, err = store.Retrieve("definitely-does-not-exist")
		require.Error(t, err, "Retrieve should fail for non-existent key")
		assert.Contains(t, err.Error(), "token not found", "Error should indicate token not found")

		// Test deleting non-existent key (should be idempotent)
		err = store.Delete("definitely-does-not-exist")
		require.NoError(t, err, "Delete should be idempotent")
	})
}
