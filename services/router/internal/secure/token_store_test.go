package secure

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/actual-software/mcp-bridge/services/router/internal/constants"
	"github.com/stretchr/testify/require"
)

func TestNewTokenStore(t *testing.T) {
	store := validateTokenStoreCreation(t, "test-app")
	validatePlatformSpecificStore(t, store)
}

// validateTokenStoreCreation creates a token store and validates it's not nil.
//
//nolint:ireturn // Test helper requires interface return
func validateTokenStoreCreation(t *testing.T, appName string) TokenStore {
	t.Helper()

	store, err := NewTokenStore(appName)
	if err != nil {
		t.Fatalf("Failed to create token store: %v", err)
	}

	if store == nil {
		t.Fatal("Expected non-nil token store")
	}

	return store
}

// validatePlatformSpecificStore validates that the correct store type is created for each platform.
func validatePlatformSpecificStore(t *testing.T, store TokenStore) {
	t.Helper()

	switch runtime.GOOS {
	case OSTypeDarwin:
		validateDarwinStore(t, store)
	case OSTypeWindows:
		validateWindowsStore(t, store)
	case OSTypeLinux:
		validateLinuxStore(t, store)
	default:
		validateDefaultStore(t, store)
	}
}

// validateDarwinStore validates macOS keychain or encrypted file store.
func validateDarwinStore(t *testing.T, store TokenStore) {
	t.Helper()

	if _, ok := store.(*keychainStore); !ok {
		if _, ok := store.(*encryptedFileStore); !ok {
			t.Errorf("Expected keychainStore or encryptedFileStore on macOS, got %T", store)
		}
	}
}

// validateWindowsStore validates Windows credential store or encrypted file store.
func validateWindowsStore(t *testing.T, store TokenStore) {
	t.Helper()

	if _, ok := store.(*credentialStore); !ok {
		if _, ok := store.(*encryptedFileStore); !ok {
			t.Errorf("Expected credentialStore or encryptedFileStore on Windows, got %T", store)
		}
	}
}

// validateLinuxStore validates Linux secret service or encrypted file store.
func validateLinuxStore(t *testing.T, store TokenStore) {
	t.Helper()

	if _, ok := store.(*secretServiceStore); !ok {
		if _, ok := store.(*encryptedFileStore); !ok {
			t.Errorf("Expected secretServiceStore or encryptedFileStore on Linux, got %T", store)
		}
	}
}

// validateDefaultStore validates encrypted file store for other platforms.
func validateDefaultStore(t *testing.T, store TokenStore) {
	t.Helper()

	if _, ok := store.(*encryptedFileStore); !ok {
		t.Errorf("Expected encryptedFileStore on %s, got %T", runtime.GOOS, store)
	}
}

func TestTokenStoreOperations(t *testing.T) {
	testKey := "test-token-key"
	testToken := "super-secret-token-123"
	updatedToken := "updated-secret-token-456"

	store := setupTokenStoreTest(t, testKey)

	testStoreAndRetrieve(t, store, testKey, testToken)
	testListOperation(t, store, testKey)
	testUpdateOperation(t, store, testKey, updatedToken)
	testDeleteOperation(t, store, testKey)
}

// setupTokenStoreTest creates a token store and cleans up existing test data.
//
//nolint:ireturn // Test helper requires interface return
func setupTokenStoreTest(t *testing.T, testKey string) TokenStore {
	t.Helper()

	store, err := NewTokenStore("test-app-operations")
	if err != nil {
		t.Fatalf("Failed to create token store: %v", err)
	}

	// Clean up any existing token.
	if err := store.Delete(testKey); err != nil {
		t.Logf("Failed to delete from store: %v", err)
	}

	return store
}

// testStoreAndRetrieve tests basic store and retrieve operations.
func testStoreAndRetrieve(t *testing.T, store TokenStore, testKey, testToken string) {
	t.Helper()

	err := store.Store(testKey, testToken)
	if err != nil {
		t.Fatalf("Failed to store token: %v", err)
	}

	retrieved, err := store.Retrieve(testKey)
	if err != nil {
		t.Fatalf("Failed to retrieve token: %v", err)
	}

	if retrieved != testToken {
		t.Errorf("Retrieved token mismatch: expected %q, got %q", testToken, retrieved)
	}
}

// testListOperation tests the list functionality if supported by the platform.
func testListOperation(t *testing.T, store TokenStore, testKey string) {
	t.Helper()

	keys, err := store.List()
	if err != nil {
		t.Logf("List not supported: %v", err)

		return
	}

	found := false

	for _, key := range keys {
		if key == testKey {
			found = true

			break
		}
	}

	if !found && len(keys) > 0 {
		t.Logf("Token key not found in list, this may be expected on some platforms")
	}
}

// testUpdateOperation tests token update functionality.
func testUpdateOperation(t *testing.T, store TokenStore, testKey, updatedToken string) {
	t.Helper()

	err := store.Store(testKey, updatedToken)
	if err != nil {
		t.Fatalf("Failed to update token: %v", err)
	}

	retrieved, err := store.Retrieve(testKey)
	if err != nil {
		t.Fatalf("Failed to retrieve updated token: %v", err)
	}

	if retrieved != updatedToken {
		t.Errorf("Updated token mismatch: expected %q, got %q", updatedToken, retrieved)
	}
}

// testDeleteOperation tests token deletion and verification.
func testDeleteOperation(t *testing.T, store TokenStore, testKey string) {
	t.Helper()

	err := store.Delete(testKey)
	if err != nil {
		t.Fatalf("Failed to delete token: %v", err)
	}

	_, err = store.Retrieve(testKey)
	if err == nil {
		t.Error("Expected error retrieving deleted token, got nil")
	}
}

func TestEncryptedFileStore(t *testing.T) {
	store, filePath, cleanup := setupEncryptedFileStoreTest(t)
	defer cleanup()

	testKey := "encrypted-test-key"
	testToken := "encrypted-test-token"

	testEncryptedStoreOperations(t, store, testKey, testToken)
	testEncryptedFileCreation(t, filePath)
	testEncryptedListAndDelete(t, store, testKey)
}

// setupEncryptedFileStoreTest creates an encrypted file store with temporary directory for testing.
//
//nolint:ireturn // Test helper requires interface return
func setupEncryptedFileStoreTest(t *testing.T) (TokenStore, string, func()) {
	t.Helper()

	store, err := newEncryptedFileStore("test-encrypted-app")
	if err != nil {
		t.Fatalf("Failed to create encrypted file store: %v", err)
	}

	tempDir, err := os.MkdirTemp("", "token-store-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	fileStore, ok := store.(*encryptedFileStore)
	require.True(t, ok, "type assertion failed")

	fileStore.filePath = tempDir + "/test-tokens.enc"

	cleanup := func() { _ = os.RemoveAll(tempDir) }

	return store, fileStore.filePath, cleanup
}

// testEncryptedStoreOperations tests basic encrypted store operations.
func testEncryptedStoreOperations(t *testing.T, store TokenStore, testKey, testToken string) {
	t.Helper()

	err := store.Store(testKey, testToken)
	if err != nil {
		t.Fatalf("Failed to store encrypted token: %v", err)
	}

	retrieved, err := store.Retrieve(testKey)
	if err != nil {
		t.Fatalf("Failed to retrieve encrypted token: %v", err)
	}

	if retrieved != testToken {
		t.Errorf("Encrypted token mismatch: expected %q, got %q", testToken, retrieved)
	}
}

// testEncryptedFileCreation verifies the encrypted file is created.
func testEncryptedFileCreation(t *testing.T, filePath string) {
	t.Helper()

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Error("Encrypted token file was not created")
	}
}

// testEncryptedListAndDelete tests list and delete operations for encrypted store.
func testEncryptedListAndDelete(t *testing.T, store TokenStore, testKey string) {
	t.Helper()

	// List
	keys, err := store.List()
	if err != nil {
		t.Fatalf("Failed to list encrypted tokens: %v", err)
	}

	if len(keys) != 1 || keys[0] != testKey {
		t.Errorf("Expected [%q], got %v", testKey, keys)
	}

	// Delete
	err = store.Delete(testKey)
	if err != nil {
		t.Fatalf("Failed to delete encrypted token: %v", err)
	}

	// Verify deletion
	keys, err = store.List()
	if err != nil {
		t.Fatalf("Failed to list after deletion: %v", err)
	}

	if len(keys) != 0 {
		t.Errorf("Expected empty list after deletion, got %v", keys)
	}
}

func TestEncryptionDecryption(t *testing.T) {
	store, err := newEncryptedFileStore("test-crypto")
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	fileStore, ok := store.(*encryptedFileStore)

	require.True(t, ok, "Expected *encryptedFileStore type")

	testData := []string{
		"short",
		"medium length test string",
		"very long test string that contains more data to ensure our " +
			"encryption and decryption works correctly with larger payloads",
		"",
		"special chars: !@#$%^&*()_+-={}[]|\\:\";<>?,./",
	}

	for _, data := range testData {
		encrypted, err := fileStore.encrypt([]byte(data))
		if err != nil {
			t.Errorf("Failed to encrypt %q: %v", data, err)

			continue
		}

		decrypted, err := fileStore.decrypt(encrypted)
		if err != nil {
			t.Errorf("Failed to decrypt %q: %v", data, err)

			continue
		}

		if string(decrypted) != data {
			t.Errorf("Decryption mismatch: expected %q, got %q", data, string(decrypted))
		}
	}
}

func TestConcurrentAccess(t *testing.T) {
	store := setupConcurrentTest(t)

	done := make(chan bool, 10)

	runConcurrentWrites(t, store, done)
	runConcurrentReads(t, store, done)
	cleanupConcurrentTest(t, store)
}

// setupConcurrentTest creates a token store for concurrent testing.
//
//nolint:ireturn // Test helper requires interface return
func setupConcurrentTest(t *testing.T) TokenStore {
	t.Helper()

	store, err := NewTokenStore("test-concurrent")
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	return store
}

// runConcurrentWrites performs concurrent write operations and waits for completion.
func runConcurrentWrites(t *testing.T, store TokenStore, done chan bool) {
	t.Helper()

	for i := 0; i < constants.TestBatchSize; i++ {
		go func(id int) {
			key := fmt.Sprintf("concurrent-key-%d", id)
			token := fmt.Sprintf("concurrent-token-%d", id)

			if err := store.Store(key, token); err != nil {
				t.Errorf("Failed to store token %d: %v", id, err)
			}

			done <- true
		}(i)
	}

	// Wait for all writes to complete.
	for i := 0; i < constants.TestBatchSize; i++ {
		<-done
	}
}

// runConcurrentReads performs concurrent read operations and validates results.
func runConcurrentReads(t *testing.T, store TokenStore, done chan bool) {
	t.Helper()

	for i := 0; i < constants.TestBatchSize; i++ {
		go func(id int) {
			key := fmt.Sprintf("concurrent-key-%d", id)
			expectedToken := fmt.Sprintf("concurrent-token-%d", id)

			retrieved, err := store.Retrieve(key)
			if err != nil {
				t.Errorf("Failed to retrieve token %d: %v", id, err)
			} else if retrieved != expectedToken {
				t.Errorf("Token %d mismatch: expected %q, got %q", id, expectedToken, retrieved)
			}

			done <- true
		}(i)
	}

	// Wait for all reads to complete.
	for i := 0; i < constants.TestBatchSize; i++ {
		<-done
	}
}

// cleanupConcurrentTest removes all test keys from the store.
func cleanupConcurrentTest(t *testing.T, store TokenStore) {
	t.Helper()

	for i := 0; i < constants.TestBatchSize; i++ {
		key := fmt.Sprintf("concurrent-key-%d", i)
		if err := store.Delete(key); err != nil {
			t.Logf("Failed to delete from store: %v", err)
		}
	}
}

func TestTokenNotFound(t *testing.T) {
	store, err := NewTokenStore("test-notfound")
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	// Try to retrieve non-existent token.
	_, err = store.Retrieve("non-existent-key")
	if err == nil {
		t.Error("Expected error for non-existent token, got nil")
	}
}

func TestDeleteNonExistent(t *testing.T) {
	store, err := NewTokenStore("test-delete-nonexistent")
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	// Delete non-existent token should not error.
	err = store.Delete("non-existent-key")
	if err != nil {
		t.Errorf("Expected no error deleting non-existent token, got: %v", err)
	}
}

func TestEncryptedFileStore_SecurityFeatures(t *testing.T) {
	tempDir := t.TempDir()

	fileStore, store := setupSecurityTestStore(t, tempDir)

	testEncryptionVariability(t, fileStore)
	testFilePermissions(t, store, fileStore)
}

// setupSecurityTestStore creates an encrypted file store for security testing.
//
//nolint:ireturn // Test helper requires interface return
func setupSecurityTestStore(t *testing.T, tempDir string) (*encryptedFileStore, TokenStore) {
	t.Helper()

	store, err := newEncryptedFileStore("security-test")
	if err != nil {
		t.Fatalf("Failed to create encrypted store: %v", err)
	}

	fileStore, ok := store.(*encryptedFileStore)
	require.True(t, ok, "Expected *encryptedFileStore type")

	fileStore.filePath = filepath.Join(tempDir, "secure-tokens.enc")

	return fileStore, store
}

// testEncryptionVariability tests that encryption produces different outputs for same input.
func testEncryptionVariability(t *testing.T, fileStore *encryptedFileStore) {
	t.Helper()

	testData := []byte("sensitive-token-data")

	encrypted1, err := fileStore.encrypt(testData)
	if err != nil {
		t.Fatalf("Encryption failed: %v", err)
	}

	encrypted2, err := fileStore.encrypt(testData)
	if err != nil {
		t.Fatalf("Second encryption failed: %v", err)
	}

	if string(encrypted1) == string(encrypted2) {
		t.Error("Multiple encryptions of same data should produce different outputs")
	}

	validateDecryption(t, fileStore, encrypted1, encrypted2, testData)
}

// validateDecryption validates that both encrypted outputs decrypt to the same original data.
func validateDecryption(t *testing.T, fileStore *encryptedFileStore, encrypted1, encrypted2, originalData []byte) {
	t.Helper()

	decrypted1, err := fileStore.decrypt(encrypted1)
	if err != nil {
		t.Fatalf("First decryption failed: %v", err)
	}

	decrypted2, err := fileStore.decrypt(encrypted2)
	if err != nil {
		t.Fatalf("Second decryption failed: %v", err)
	}

	if string(decrypted1) != string(originalData) || string(decrypted2) != string(originalData) {
		t.Error("Decrypted data doesn't match original")
	}
}

// testFilePermissions tests that the encrypted file has proper restrictive permissions.
func testFilePermissions(t *testing.T, store TokenStore, fileStore *encryptedFileStore) {
	t.Helper()

	err := store.Store("test-key", "test-token")
	if err != nil {
		t.Fatalf("Failed to store token: %v", err)
	}

	info, err := os.Stat(fileStore.filePath)
	if err != nil {
		t.Fatalf("Failed to stat token file: %v", err)
	}

	perm := info.Mode().Perm()

	expectedPerm := os.FileMode(0o600)
	if perm != expectedPerm {
		t.Errorf("Token file has incorrect permissions: got %v, want %v", perm, expectedPerm)
	}
}

func TestEncryptedFileStore_CorruptionResistance(t *testing.T) {
	// Test behavior with corrupted encrypted files.
	tempDir := t.TempDir()

	store, err := newEncryptedFileStore("corruption-test")
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	fileStore, ok := store.(*encryptedFileStore)

	require.True(t, ok, "Expected *encryptedFileStore type")

	fileStore.filePath = filepath.Join(tempDir, "corrupt-test.enc")

	// Store a valid token first.
	err = store.Store("valid-key", "valid-token")
	if err != nil {
		t.Fatalf("Failed to store valid token: %v", err)
	}

	// Corrupt the file by truncating it.
	err = os.WriteFile(fileStore.filePath, []byte("corrupted-data"), 0o600)
	if err != nil {
		t.Fatalf("Failed to corrupt file: %v", err)
	}

	// Try to retrieve - should fail gracefully.
	_, err = store.Retrieve("valid-key")
	if err == nil {
		t.Error("Expected error retrieving from corrupted file")
	}

	// Try to store new token - should recover by creating new file.
	err = store.Store("recovery-key", "recovery-token")
	if err != nil {
		t.Errorf("Store should recover from corruption: %v", err)
	}

	// Should be able to retrieve new token.
	retrieved, err := store.Retrieve("recovery-key")
	if err != nil {
		t.Errorf("Failed to retrieve after recovery: %v", err)
	}

	if retrieved != "recovery-token" {
		t.Errorf("Retrieved wrong token after recovery: got %s, want recovery-token", retrieved)
	}
}

func TestEncryptedFileStore_KeyDerivation(t *testing.T) {
	// Test that key derivation is deterministic but secure.
	appName1 := "test-app-1"
	appName2 := "test-app-2"

	key1a := generateKey(appName1)
	key1b := generateKey(appName1)
	key2 := generateKey(appName2)

	// Same app name should produce same key.
	if !equalBytes(key1a, key1b) {
		t.Error("Key derivation should be deterministic for same app name")
	}

	// Different app names should produce different keys.
	if equalBytes(key1a, key2) {
		t.Error("Different app names should produce different keys")
	}

	// Keys should be 32 bytes (256 bits).
	if len(key1a) != 32 {
		t.Errorf("Key should be 32 bytes, got %d", len(key1a))
	}
}

func TestEncryptedFileStore_ConcurrentOperations(t *testing.T) {
	tempDir := t.TempDir()
	store := setupConcurrentEncryptedTest(t, tempDir)

	const (
		numGoroutines   = 20
		opsPerGoroutine = 10
	)

	runConcurrentEncryptedWrites(t, store, numGoroutines, opsPerGoroutine)
	verifyConcurrentEncryptedTokens(t, store, numGoroutines, opsPerGoroutine)
}

// setupConcurrentEncryptedTest creates an encrypted file store for concurrent testing.
//
//nolint:ireturn // Test helper requires interface return
func setupConcurrentEncryptedTest(t *testing.T, tempDir string) TokenStore {
	t.Helper()

	store, err := newEncryptedFileStore("concurrent-test")
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	fileStore, ok := store.(*encryptedFileStore)
	require.True(t, ok, "Expected *encryptedFileStore type")

	fileStore.filePath = filepath.Join(tempDir, "concurrent.enc")

	return store
}

// runConcurrentEncryptedWrites performs concurrent write operations for encrypted store.
func runConcurrentEncryptedWrites(t *testing.T, store TokenStore, numGoroutines, opsPerGoroutine int) {
	t.Helper()

	done := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			for j := 0; j < opsPerGoroutine; j++ {
				key := fmt.Sprintf("key-%d-%d", id, j)
				token := fmt.Sprintf("token-%d-%d", id, j)

				if err := store.Store(key, token); err != nil {
					done <- fmt.Errorf("store failed for %s: %w", key, err)

					return
				}
			}

			done <- nil
		}(i)
	}

	// Wait for all operations to complete.
	for i := 0; i < numGoroutines; i++ {
		if err := <-done; err != nil {
			t.Error(err)
		}
	}
}

// verifyConcurrentEncryptedTokens validates that all tokens were stored correctly.
func verifyConcurrentEncryptedTokens(t *testing.T, store TokenStore, numGoroutines, opsPerGoroutine int) {
	t.Helper()

	for i := 0; i < numGoroutines; i++ {
		for j := 0; j < opsPerGoroutine; j++ {
			key := fmt.Sprintf("key-%d-%d", i, j)
			expectedToken := fmt.Sprintf("token-%d-%d", i, j)

			retrieved, err := store.Retrieve(key)
			if err != nil {
				t.Errorf("Failed to retrieve %s: %v", key, err)
			}

			if retrieved != expectedToken {
				t.Errorf("Token mismatch for %s: got %s, want %s", key, retrieved, expectedToken)
			}
		}
	}
}

func TestTokenStore_EdgeCases(t *testing.T) {
	store := setupTokenStoreEdgeCaseTest(t)
	tests := createTokenStoreEdgeCaseTests()
	runTokenStoreEdgeCaseTests(t, store, tests)
}

//nolint:ireturn // Test helper requires interface return
func setupTokenStoreEdgeCaseTest(t *testing.T) TokenStore {
	t.Helper()

	store, err := NewTokenStore("edge-case-test")
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	return store
}

func createTokenStoreEdgeCaseTests() []struct {
	name    string
	key     string
	token   string
	wantErr bool
} {
	return []struct {
		name    string
		key     string
		token   string
		wantErr bool
	}{
		{
			name:  "empty key",
			key:   "",
			token: "some-token",
		},
		{
			name:  "empty token",
			key:   "some-key",
			token: "",
		},
		{
			name:  "very long key",
			key:   strings.Repeat("long-key-", 100),
			token: "token",
		},
		{
			name:  "very long token",
			key:   "key",
			token: strings.Repeat("very-long-token-data-", 1000),
		},
		{
			name:  "special characters in key",
			key:   "key/with\\special:chars",
			token: "token",
		},
		{
			name:  "special characters in token",
			key:   "key",
			token: "token\nwith\\special\tchars\r\n",
		},
		{
			name:  "unicode in key",
			key:   "key-with-unicode-äöü-émoji-",
			token: "token",
		},
		{
			name:  "unicode in token",
			key:   "key",
			token: "token-with-unicode-äöü-émoji-",
		},
	}
}

func runTokenStoreEdgeCaseTests(t *testing.T, store TokenStore, tests []struct {
	name    string
	key     string
	token   string
	wantErr bool
}) {
	t.Helper()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			processEdgeCaseTest(t, store, tt)
		})
	}
}

func processEdgeCaseTest(t *testing.T, store TokenStore, tt struct {
	name    string
	key     string
	token   string
	wantErr bool
}) {
	t.Helper()

	// Store the token.
	err := store.Store(tt.key, tt.token)

	if !validateEdgeCaseStoreResult(t, err, tt.wantErr) {
		return
	}

	if !tt.wantErr {
		verifyAndCleanupEdgeCaseToken(t, store, tt.key, tt.token)
	}
}

func validateEdgeCaseStoreResult(t *testing.T, err error, wantErr bool) bool {
	t.Helper()

	if wantErr && err == nil {
		t.Error("Expected error but got none")

		return false
	}

	if !wantErr && err != nil {
		t.Errorf("Unexpected error: %v", err)

		return false
	}

	return true
}

func verifyAndCleanupEdgeCaseToken(t *testing.T, store TokenStore, key, expectedToken string) {
	t.Helper()

	// Retrieve and verify.
	retrieved, err := store.Retrieve(key)
	if err != nil {
		t.Errorf("Failed to retrieve: %v", err)

		return
	}

	if retrieved != expectedToken {
		t.Errorf("Token mismatch: got %q, want %q", retrieved, expectedToken)
	}

	// Cleanup.
	if err := store.Delete(key); err != nil {
		t.Logf("Failed to delete from store: %v", err)
	}
}

func TestTokenStore_PerformanceUnderLoad(t *testing.T) {
	store := setupPerformanceTest(t)

	const numTokens = 1000

	runBulkStoreOperations(t, store, numTokens)
	runBulkRetrieveOperations(t, store, numTokens)
	testListPerformance(t, store, numTokens)
	cleanupPerformanceTest(t, store, numTokens)
}

// setupPerformanceTest creates a token store for performance testing.
//
//nolint:ireturn // Test helper requires interface return.
func setupPerformanceTest(t *testing.T) TokenStore {
	t.Helper()

	store, err := NewTokenStore("performance-test")
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	return store
}

// runBulkStoreOperations performs bulk store operations and measures performance.
func runBulkStoreOperations(t *testing.T, store TokenStore, numTokens int) time.Duration {
	t.Helper()

	start := time.Now()

	for i := 0; i < numTokens; i++ {
		key := fmt.Sprintf("perf-key-%d", i)
		token := fmt.Sprintf("perf-token-%d-with-some-longer-content-to-test-real-world-scenarios", i)

		err := store.Store(key, token)
		if err != nil {
			t.Errorf("Failed to store token %d: %v", i, err)
		}
	}

	storeTime := time.Since(start)
	tokensPerSec := float64(numTokens) / storeTime.Seconds()
	t.Logf("Bulk store of %d tokens took: %v (%.2f tokens/sec)", numTokens, storeTime, tokensPerSec)

	return storeTime
}

// runBulkRetrieveOperations performs bulk retrieve operations and measures performance.
func runBulkRetrieveOperations(t *testing.T, store TokenStore, numTokens int) time.Duration {
	t.Helper()

	start := time.Now()

	for i := 0; i < numTokens; i++ {
		key := fmt.Sprintf("perf-key-%d", i)

		_, err := store.Retrieve(key)
		if err != nil {
			t.Errorf("Failed to retrieve token %d: %v", i, err)
		}
	}

	retrieveTime := time.Since(start)
	retrievePerSec := float64(numTokens) / retrieveTime.Seconds()
	t.Logf("Bulk retrieve of %d tokens took: %v (%.2f tokens/sec)", numTokens, retrieveTime, retrievePerSec)

	return retrieveTime
}

// testListPerformance tests and measures list operation performance.
func testListPerformance(t *testing.T, store TokenStore, numTokens int) {
	t.Helper()

	start := time.Now()
	keys, err := store.List()
	listTime := time.Since(start)
	t.Logf("List of %d tokens took: %v", len(keys), listTime)

	if err != nil && !errors.Is(err, ErrListNotSupported) {
		t.Errorf("List failed: %v", err)
	}

	if err == nil && len(keys) != numTokens {
		t.Errorf("Expected %d keys, got %d", numTokens, len(keys))
	}
}

// cleanupPerformanceTest removes all performance test tokens.
func cleanupPerformanceTest(t *testing.T, store TokenStore, numTokens int) {
	t.Helper()

	for i := 0; i < numTokens; i++ {
		key := fmt.Sprintf("perf-key-%d", i)
		if err := store.Delete(key); err != nil {
			t.Logf("Failed to delete from store: %v", err)
		}
	}
}

func BenchmarkTokenStore_Store(b *testing.B) {
	store, err := NewTokenStore("benchmark-store")
	if err != nil {
		b.Fatalf("Failed to create store: %v", err)
	}

	token := "benchmark-token-with-some-realistic-length-content-that-might-be-used-in-practice"

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("bench-key-%d", i)

		err := store.Store(key, token)
		if err != nil {
			b.Fatalf("Store failed: %v", err)
		}
	}
}

func BenchmarkTokenStore_Retrieve(b *testing.B) {
	store, err := NewTokenStore("benchmark-retrieve")
	if err != nil {
		b.Fatalf("Failed to create store: %v", err)
	}

	// Pre-populate with tokens.
	token := "benchmark-token-for-retrieval-testing"

	for i := 0; i < constants.TestMaxMessages; i++ {
		key := fmt.Sprintf("bench-key-%d", i)

		err := store.Store(key, token)
		if err != nil {
			b.Fatalf("Failed to pre-populate: %v", err)
		}
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("bench-key-%d", i%1000)

		_, err := store.Retrieve(key)
		if err != nil {
			b.Fatalf("Retrieve failed: %v", err)
		}
	}
}

// Helper function to compare byte slices.
func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}
