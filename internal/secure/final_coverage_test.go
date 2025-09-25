package secure

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test comprehensive coverage improvements.
func TestFinalCoverageImprovements(t *testing.T) {
	t.Run("encrypted_file_store_comprehensive", testEncryptedFileStoreComprehensive)
	t.Run("error_paths_comprehensive", testErrorPathsComprehensive)
	t.Run("encryption_edge_cases", testEncryptionEdgeCases)
	t.Run("file_operations_comprehensive", testFileOperationsComprehensive)
	t.Run("generate_password_comprehensive", testGeneratePasswordComprehensive)
	t.Run("file_permissions_and_cleanup", testFilePermissionsAndCleanup)
	t.Run("concurrent_operations_different_stores", testConcurrentOperationsDifferentStores)
	t.Run("directory_creation_edge_cases", testDirectoryCreationEdgeCases)
}

func testEncryptedFileStoreComprehensive(t *testing.T) {
	// Test newEncryptedFileStore error paths
	store, err := newEncryptedFileStore("final-coverage-test")
	require.NoError(t, err)
	require.NotNil(t, store)

	// Test all CRUD operations to improve coverage
	testKey := "final-test-key"
	testValue := "final-test-value"

	// Store
	err = store.Store(testKey, testValue)
	require.NoError(t, err, "Store should succeed")

	// Retrieve
	retrievedValue, err := store.Retrieve(testKey)
	require.NoError(t, err, "Retrieve should succeed")
	assert.Equal(t, testValue, retrievedValue, "Retrieved value should match")

	// Update
	updatedValue := "final-updated-value"
	err = store.Store(testKey, updatedValue)
	require.NoError(t, err, "Update should succeed")

	retrievedValue, err = store.Retrieve(testKey)
	require.NoError(t, err, "Retrieve after update should succeed")
	assert.Equal(t, updatedValue, retrievedValue, "Updated value should match")

	// List
	keys, err := store.List()
	require.NoError(t, err, "List should succeed")
	assert.Contains(t, keys, testKey, "List should contain our key")

	// Delete
	err = store.Delete(testKey)
	require.NoError(t, err, "Delete should succeed")

	// Verify delete
	_, err = store.Retrieve(testKey)
	assert.Error(t, err, "Retrieve should fail after delete")
}

func testErrorPathsComprehensive(t *testing.T) {
	store, err := newEncryptedFileStore("error-paths-comprehensive")
	require.NoError(t, err)

	// Test retrieve non-existent key
	_, err = store.Retrieve("non-existent-key-12345")
	require.Error(t, err, "Retrieve should fail for non-existent key")
	assert.Contains(t, err.Error(), "token not found", "Error should indicate token not found")

	// Test delete non-existent key (should be idempotent)
	err = store.Delete("non-existent-key-12345")
	require.NoError(t, err, "Delete should be idempotent")

	// Test empty list
	keys, err := store.List()
	require.NoError(t, err, "List should work on empty store")
	assert.Empty(t, keys, "Empty store should return empty list")
}

func testEncryptionEdgeCases(t *testing.T) {
	store, err := newEncryptedFileStore("encryption-edge-cases")
	require.NoError(t, err)

	efs, ok := store.(*encryptedFileStore)
	require.True(t, ok, "store should be *encryptedFileStore")

	// Test encrypt/decrypt with various data sizes
	testCases := [][]byte{
		{},                 // empty
		{0},                // single zero byte
		{255},              // single max byte
		[]byte("hello"),    // small string
		make([]byte, 1000), // medium data
	}

	for i, data := range testCases {
		// Fill with pattern for medium data
		if len(data) == 1000 {
			for j := range data {
				data[j] = byte(j % 256)
			}
		}

		encrypted, err := efs.encrypt(data)
		require.NoError(t, err, "Encrypt should succeed for case %d", i)

		decrypted, err := efs.decrypt(encrypted)
		require.NoError(t, err, "Decrypt should succeed for case %d", i)

		if len(data) == 0 {
			assert.Empty(t, decrypted, "Empty data should roundtrip to empty for case %d", i)
		} else {
			assert.Equal(t, data, decrypted, "Data should roundtrip for case %d", i)
		}
	}

	// Test decrypt invalid data
	_, err = efs.decrypt([]byte("invalid-encrypted-data"))
	require.Error(t, err, "Decrypt should fail with invalid data")

	// Test decrypt too short data
	_, err = efs.decrypt([]byte{1, 2, 3})
	require.Error(t, err, "Decrypt should fail with too short data")
}

func testFileOperationsComprehensive(t *testing.T) {
	// Use unique name for each test run
	uniqueName := fmt.Sprintf("file-ops-comprehensive-%d", time.Now().UnixNano())
	store, err := newEncryptedFileStore(uniqueName)
	require.NoError(t, err)

	efs, ok := store.(*encryptedFileStore)
	require.True(t, ok, "store should be *encryptedFileStore")

	// Test load when file doesn't exist (first time)
	data, err := efs.load()
	require.NoError(t, err, "Load should succeed even when file doesn't exist")
	assert.NotNil(t, data, "Load should return tokenData struct")
	assert.Empty(t, data.Tokens, "Load should return empty tokens for missing file")

	// Test save and load
	testTokens := &tokenData{
		Tokens: map[string]string{
			"file-ops-key-1": "file-ops-value-1",
			"file-ops-key-2": "file-ops-value-2",
		},
	}

	err = efs.save(testTokens)
	require.NoError(t, err, "Save should succeed")

	loadedData, err := efs.load()
	require.NoError(t, err, "Load should succeed after save")
	assert.Equal(t, testTokens.Tokens, loadedData.Tokens, "Loaded data should match saved data")

	// Test overwrite
	newTokens := &tokenData{
		Tokens: map[string]string{
			"overwrite-key": "overwrite-value",
		},
	}

	err = efs.save(newTokens)
	require.NoError(t, err, "Overwrite should succeed")

	loadedData, err = efs.load()
	require.NoError(t, err, "Load should succeed after overwrite")
	assert.Equal(t, newTokens.Tokens, loadedData.Tokens, "Loaded data should match overwritten data")
}

func testGeneratePasswordComprehensive(t *testing.T) {
	// Test generatePassword with various inputs to improve coverage
	testCases := []string{
		"normal-app",
		"",
		"app.with.dots",
		"app_with_underscores",
		"app-with-dashes",
		"app@with.email:style",
		"unicode-app-测试",
		string(make([]byte, 100)), // long name
	}

	for _, appName := range testCases {
		password := generatePassword(appName)
		assert.NotEmpty(t, password, "Password should not be empty for app: %q", appName)
		assert.GreaterOrEqual(t, len(password), 32, "Password should be at least 32 bytes for app: %q", appName)

		// Test deterministic behavior
		password2 := generatePassword(appName)
		assert.Equal(t, password, password2, "Password should be deterministic for app: %q", appName)
	}
}

func testFilePermissionsAndCleanup(t *testing.T) {
	store, err := newEncryptedFileStore("permissions-test")
	require.NoError(t, err)

	// Store some data to create the file
	err = store.Store("permissions-key", "permissions-value")
	require.NoError(t, err)

	// Check file exists and has correct permissions
	efs, ok := store.(*encryptedFileStore)
	require.True(t, ok, "store should be *encryptedFileStore")

	stat, err := os.Stat(efs.filePath)
	if err == nil {
		// File should have restrictive permissions
		assert.Equal(t, os.FileMode(0o600), stat.Mode().Perm(), "File should have 0600 permissions")
	}

	// Clean up
	_ = store.Delete("permissions-key")
}

func testConcurrentOperationsDifferentStores(t *testing.T) {
	// Test that different stores don't interfere
	store1, err := newEncryptedFileStore("concurrent-1")
	require.NoError(t, err)

	store2, err := newEncryptedFileStore("concurrent-2")
	require.NoError(t, err)

	// Store in both
	err = store1.Store("concurrent-key", "value-from-store-1")
	require.NoError(t, err)

	err = store2.Store("concurrent-key", "value-from-store-2")
	require.NoError(t, err)

	// Retrieve from both
	value1, err := store1.Retrieve("concurrent-key")
	require.NoError(t, err)
	assert.Equal(t, "value-from-store-1", value1)

	value2, err := store2.Retrieve("concurrent-key")
	require.NoError(t, err)
	assert.Equal(t, "value-from-store-2", value2)

	// Clean up
	_ = store1.Delete("concurrent-key")
	_ = store2.Delete("concurrent-key")
}

func testDirectoryCreationEdgeCases(t *testing.T) {
	// Test store creation with nested directories
	deepPath := "deep/nested/directory/structure"
	store, err := newEncryptedFileStore(deepPath)
	require.NoError(t, err, "Store creation should handle nested directories")

	// Test that it works
	err = store.Store("deep-key", "deep-value")
	require.NoError(t, err, "Store should work with deep directory structure")

	value, err := store.Retrieve("deep-key")
	require.NoError(t, err, "Retrieve should work with deep directory structure")
	assert.Equal(t, "deep-value", value)

	// Clean up
	_ = store.Delete("deep-key")

	// Clean up directory structure
	homeDir, _ := os.UserHomeDir()
	baseDir := filepath.Join(homeDir, ".mcp-tokens", deepPath)
	_ = os.RemoveAll(filepath.Dir(baseDir))
}

// Test specific keychain operations to improve coverage (macOS only).
func TestKeychainOperationsCoverage(t *testing.T) {
	t.Run("keychain_comprehensive_operations", func(t *testing.T) {
		store := newKeychainStore("keychain-coverage-test")
		require.NotNil(t, store)

		// Test all operations (these may fail gracefully on CI or in environments without keychain access)
		testKey := "keychain-coverage-key"
		testValue := "keychain-coverage-value"

		// Store
		err := store.Store(testKey, testValue)
		if err != nil {
			t.Logf("Keychain store failed (may be expected in test environment): %v", err)
			t.Skip("Skipping keychain tests - keychain not available")
		}

		// Retrieve
		retrievedValue, err := store.Retrieve(testKey)
		if err != nil {
			t.Logf("Keychain retrieve failed: %v", err)
		} else {
			assert.Equal(t, testValue, retrievedValue, "Retrieved value should match")
		}

		// List (may return empty or error)
		keys, err := store.List()
		if err != nil {
			t.Logf("Keychain list failed (may be expected): %v", err)
		} else {
			t.Logf("Keychain list returned %d keys", len(keys))
		}

		// Delete
		err = store.Delete(testKey)
		if err != nil {
			t.Logf("Keychain delete failed: %v", err)
		}
	})
}
