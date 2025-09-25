package secure

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	// LongTokenLength is the length for testing long token handling.
	LongTokenLength = 1024
)

// TokenStoreTestEnvironment provides comprehensive test environment for token store.
type TokenStoreTestEnvironment struct {
	t          *testing.T
	stores     map[string]TokenStore
	testTokens map[string]string
	tempDirs   []string
	mutex      sync.Mutex
}

// EstablishTokenStoreTest creates a new test environment.
func EstablishTokenStoreTest(t *testing.T) *TokenStoreTestEnvironment {
	t.Helper()

	return &TokenStoreTestEnvironment{
		t:          t,
		stores:     make(map[string]TokenStore),
		testTokens: make(map[string]string),
		tempDirs:   []string{},
	}
}

// CreateStore creates a token store with the given name.
func (env *TokenStoreTestEnvironment) CreateStore(serviceName string) TokenStore {
	store, err := NewTokenStore(serviceName)
	if err != nil {
		env.t.Fatalf("Failed to create token store: %v", err)
	}

	env.mutex.Lock()
	env.stores[serviceName] = store
	env.mutex.Unlock()

	return store
}

// StoreToken stores a test token.
func (env *TokenStoreTestEnvironment) StoreToken(store TokenStore, key, token string) {
	if err := store.Store(key, token); err != nil {
		env.t.Fatalf("Failed to store token: %v", err)
	}

	env.mutex.Lock()
	env.testTokens[key] = token
	env.mutex.Unlock()
}

// RetrieveAndVerify retrieves and verifies a token.
func (env *TokenStoreTestEnvironment) RetrieveAndVerify(store TokenStore, key, expected string) {
	retrieved, err := store.Retrieve(key)
	if err != nil {
		env.t.Fatalf("Failed to retrieve token: %v", err)
	}

	if retrieved != expected {
		env.t.Errorf("Token mismatch: expected %s, got %s", expected, retrieved)
	}
}

// TestBasicOperations tests basic store operations.
func (env *TokenStoreTestEnvironment) TestBasicOperations(store TokenStore) {
	// Store operation.
	testToken := "test-token-12345"
	testKey := "test-key"

	env.StoreToken(store, testKey, testToken)

	// Retrieve operation.
	env.RetrieveAndVerify(store, testKey, testToken)

	// Delete operation.
	env.DeleteToken(store, testKey)

	// Verify deletion.
	env.VerifyTokenDeleted(store, testKey)
}

// DeleteToken deletes a token.
func (env *TokenStoreTestEnvironment) DeleteToken(store TokenStore, key string) {
	if err := store.Delete(key); err != nil {
		env.t.Fatalf("Failed to delete token: %v", err)
	}

	env.mutex.Lock()
	delete(env.testTokens, key)
	env.mutex.Unlock()
}

// VerifyTokenDeleted verifies a token has been deleted.
func (env *TokenStoreTestEnvironment) VerifyTokenDeleted(store TokenStore, key string) {
	_, err := store.Retrieve(key)
	if err == nil {
		env.t.Errorf("Expected error retrieving deleted token, got none")
	}
}

// TestUpdateOperation tests token update.
func (env *TokenStoreTestEnvironment) TestUpdateOperation(store TokenStore) {
	key := "update-key"
	originalToken := "original-token"
	updatedToken := "updated-token"

	// Store original.
	env.StoreToken(store, key, originalToken)

	// Update.
	env.StoreToken(store, key, updatedToken)

	// Verify update.
	env.RetrieveAndVerify(store, key, updatedToken)
}

// TestListOperation tests listing tokens.
func (env *TokenStoreTestEnvironment) TestListOperation(store TokenStore) {
	// Store multiple tokens.
	tokens := map[string]string{
		"key1": "token1",
		"key2": "token2",
		"key3": "token3",
	}

	for key, token := range tokens {
		env.StoreToken(store, key, token)
	}

	// List tokens.
	keys, err := store.List()
	if err != nil {
		if errors.Is(err, ErrListNotSupported) {
			env.t.Skip("List operation not supported")

			return
		}

		env.t.Fatalf("Failed to list tokens: %v", err)
	}

	// Verify all keys are present.
	env.verifyKeysPresent(keys, tokens)
}

// verifyKeysPresent verifies expected keys are in the list.
func (env *TokenStoreTestEnvironment) verifyKeysPresent(keys []string, expected map[string]string) {
	keyMap := make(map[string]bool)
	for _, key := range keys {
		keyMap[key] = true
	}

	for expectedKey := range expected {
		if !keyMap[expectedKey] {
			env.t.Errorf("Expected key %s not found in list", expectedKey)
		}
	}
}

// TestSpecialCharacters tests handling of special characters.
func (env *TokenStoreTestEnvironment) TestSpecialCharacters(store TokenStore) {
	testCases := []struct {
		key   string
		token string
	}{
		{"key-with-dash", "token-value"},
		{"key.with.dots", "token.value"},
		{"key_with_underscore", "token_value"},
		{"key/with/slash", "token/value"},
	}

	for _, tc := range testCases {
		env.StoreToken(store, tc.key, tc.token)
		env.RetrieveAndVerify(store, tc.key, tc.token)
		env.DeleteToken(store, tc.key)
	}
}

// TestEmptyValues tests empty key and token handling.
func (env *TokenStoreTestEnvironment) TestEmptyValues(store TokenStore) {
	// Test empty token.
	if err := store.Store("empty-token-key", ""); err == nil {
		env.t.Error("Expected error storing empty token")
	}

	// Test empty key.
	if err := store.Store("", "some-token"); err == nil {
		env.t.Error("Expected error storing with empty key")
	}
}

// CreateTempDir creates a temporary directory for testing.
func (env *TokenStoreTestEnvironment) CreateTempDir(prefix string) string {
	tmpDir, err := os.MkdirTemp("", prefix)
	if err != nil {
		env.t.Fatalf("Failed to create temp dir: %v", err)
	}

	env.tempDirs = append(env.tempDirs, tmpDir)

	return tmpDir
}

// TestPlatformSpecific tests platform-specific behavior.
func (env *TokenStoreTestEnvironment) TestPlatformSpecific() {
	store := env.CreateStore("test-platform")

	switch runtime.GOOS {
	case "darwin":
		env.testMacOSKeychain(store)
	case "linux":
		env.testLinuxSecretService(store)
	case "windows":
		env.testWindowsCredentialManager(store)
	default:
		env.t.Skipf("No platform-specific tests for %s", runtime.GOOS)
	}
}

// testMacOSKeychain tests macOS keychain specific behavior.
func (env *TokenStoreTestEnvironment) testMacOSKeychain(store TokenStore) {
	// Test keychain-specific features.
	testKey := "macos-keychain-test"
	testToken := "keychain-token"

	env.StoreToken(store, testKey, testToken)
	env.RetrieveAndVerify(store, testKey, testToken)

	// Keychain should handle long tokens.
	longToken := strings.Repeat("a", LongTokenLength)
	env.StoreToken(store, "long-token", longToken)
	env.RetrieveAndVerify(store, "long-token", longToken)
}

// testLinuxSecretService tests Linux secret service behavior.
func (env *TokenStoreTestEnvironment) testLinuxSecretService(store TokenStore) {
	// Test secret service or fallback.
	testKey := "linux-secret-test"
	testToken := "secret-token"

	env.StoreToken(store, testKey, testToken)
	env.RetrieveAndVerify(store, testKey, testToken)
}

// testWindowsCredentialManager tests Windows credential manager.
func (env *TokenStoreTestEnvironment) testWindowsCredentialManager(store TokenStore) {
	// Test Windows credential manager.
	testKey := "windows-cred-test"
	testToken := "credential-token" // #nosec G101 - test credential, not production

	env.StoreToken(store, testKey, testToken)
	env.RetrieveAndVerify(store, testKey, testToken)
}

// TestConcurrentAccess tests concurrent operations.
func (env *TokenStoreTestEnvironment) TestConcurrentAccess(store TokenStore) {
	const (
		numGoroutines = 10
		numOperations = 5
	)

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()

			for j := 0; j < numOperations; j++ {
				key := fmt.Sprintf("concurrent-key-%d-%d", id, j)
				token := fmt.Sprintf("concurrent-token-%d-%d", id, j)

				// Store.
				if err := store.Store(key, token); err != nil {
					env.t.Errorf("Concurrent store failed: %v", err)
				}

				// Retrieve.
				retrieved, err := store.Retrieve(key)
				if err != nil {
					env.t.Errorf("Concurrent retrieve failed: %v", err)
				}

				if retrieved != token {
					env.t.Errorf("Concurrent token mismatch: expected %s, got %s",
						token, retrieved)
				}

				// Delete.
				if err := store.Delete(key); err != nil {
					env.t.Errorf("Concurrent delete failed: %v", err)
				}
			}
		}(i)
	}

	wg.Wait()
}

// TestPerformance tests performance under load.
func (env *TokenStoreTestEnvironment) TestPerformance(store TokenStore, numOperations int) {
	start := time.Now()

	// Store operations.
	for i := 0; i < numOperations; i++ {
		key := fmt.Sprintf("perf-key-%d", i)
		token := fmt.Sprintf("perf-token-%d", i)

		if err := store.Store(key, token); err != nil {
			env.t.Fatalf("Performance test store failed: %v", err)
		}
	}

	storeTime := time.Since(start)

	// Retrieve operations.
	start = time.Now()

	for i := 0; i < numOperations; i++ {
		key := fmt.Sprintf("perf-key-%d", i)

		_, err := store.Retrieve(key)
		if err != nil {
			env.t.Fatalf("Performance test retrieve failed: %v", err)
		}
	}

	retrieveTime := time.Since(start)

	// Log performance metrics.
	env.t.Logf("Performance metrics for %d operations:", numOperations)
	env.t.Logf("  Store time: %v (%.2f ops/sec)", storeTime,
		float64(numOperations)/storeTime.Seconds())
	env.t.Logf("  Retrieve time: %v (%.2f ops/sec)", retrieveTime,
		float64(numOperations)/retrieveTime.Seconds())
}

// TestEncryptedFileStore tests encrypted file store.
func (env *TokenStoreTestEnvironment) TestEncryptedFileStore() {
	// Skip if encrypted file store is not implemented.
	env.t.Skip("Encrypted file store not yet implemented")
}

// Cleanup cleans up test resources.
func (env *TokenStoreTestEnvironment) Cleanup() {
	// Clean up test tokens.
	for serviceName, store := range env.stores {
		for key := range env.testTokens {
			if err := store.Delete(key); err != nil {
				env.t.Logf("Failed to delete from store: %v", err)
			}
		}

		env.t.Logf("Cleaned up store: %s", serviceName)
	}

	// Clean up temp directories.
	for _, dir := range env.tempDirs {
		_ = os.RemoveAll(dir)
	}
}
