package secure

import (
	"crypto/rand"
	"encoding/base64"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTokenStore(t *testing.T) {
	tests := []struct {
		name       string
		appName    string
		expectType string
	}{
		{
			name:       "creates token store with valid app name",
			appName:    "test-app",
			expectType: "*secure.encryptedFileStore", // Should fall back to file store in test
		},
		{
			name:       "creates token store with empty app name",
			appName:    "",
			expectType: "*secure.encryptedFileStore",
		},
		{
			name:       "creates token store with special characters",
			appName:    "test-app!@#$%",
			expectType: "*secure.encryptedFileStore",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, err := NewTokenStore(tt.appName)
			require.NoError(t, err)
			require.NotNil(t, store)

			// Verify we got some kind of store
			assert.Implements(t, (*TokenStore)(nil), store)
		})
	}
}

func TestEncryptedFileStore(t *testing.T) {
	// Create a temporary directory for testing
	tempDir, err := os.MkdirTemp("", "token-store-test")
	require.NoError(t, err)

	defer func() { _ = os.RemoveAll(tempDir) }()

	// Temporarily override the home directory
	originalHome := os.Getenv("HOME")

	defer func() { _ = os.Setenv("HOME", originalHome) }()

	err = os.Setenv("HOME", tempDir)
	require.NoError(t, err)

	appName := "test-mcp-bridge"
	store, err := newEncryptedFileStore(appName)
	require.NoError(t, err)
	require.NotNil(t, store)

	fileStore, ok := store.(*encryptedFileStore)
	require.True(t, ok)
	assert.Equal(t, appName, fileStore.appName)
	assert.NotEmpty(t, fileStore.password)

	t.Run("Store and retrieve token", func(t *testing.T) { testStoreAndRetrieveToken(t, store) })
	t.Run("Store multiple tokens", func(t *testing.T) { testStoreMultipleTokens(t, store) })
	t.Run("Retrieve non-existent token", func(t *testing.T) { testRetrieveNonExistentToken(t, store) })
	t.Run("Delete token", func(t *testing.T) { testDeleteToken(t, store) })
	t.Run("List tokens", testListTokens)
	t.Run("Update existing token", func(t *testing.T) { testUpdateExistingToken(t, store) })
}

// generateTestToken creates a random test token.
func generateTestToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	
	return base64.StdEncoding.EncodeToString(b)
}

func testStoreAndRetrieveToken(t *testing.T, store TokenStore) {
	t.Helper()

	testKey := "test-service"
	testToken := generateTestToken() 

	err := store.Store(testKey, testToken)
	require.NoError(t, err)

	retrievedToken, err := store.Retrieve(testKey)
	require.NoError(t, err)
	assert.Equal(t, testToken, retrievedToken)
}

func testStoreMultipleTokens(t *testing.T, store TokenStore) {
	t.Helper()

	tokens := map[string]string{
		"service1": "token1",
		"service2": "token2",
		"service3": "very-long-token-with-special-chars!@#$%^&*()",
	}

	// Store all tokens
	for key, token := range tokens {
		err := store.Store(key, token)
		require.NoError(t, err)
	}

	// Retrieve and verify all tokens
	for key, expectedToken := range tokens {
		retrievedToken, err := store.Retrieve(key)
		require.NoError(t, err)
		assert.Equal(t, expectedToken, retrievedToken)
	}
}

func testRetrieveNonExistentToken(t *testing.T, store TokenStore) {
	t.Helper()

	_, err := store.Retrieve("non-existent-key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "token not found")
}

func testDeleteToken(t *testing.T, store TokenStore) {
	t.Helper()

	testKey := "delete-test"
	testToken := "delete-token"

	// Store token
	err := store.Store(testKey, testToken)
	require.NoError(t, err)

	// Verify it exists
	_, err = store.Retrieve(testKey)
	require.NoError(t, err)

	// Delete token
	err = store.Delete(testKey)
	require.NoError(t, err)

	// Verify it's gone
	_, err = store.Retrieve(testKey)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "token not found")
}

func testListTokens(t *testing.T) {
	// Clear any existing tokens by creating a new store instance
	store2, err := newEncryptedFileStore("test-list-app")
	require.NoError(t, err)

	// Initially should be empty
	keys, err := store2.List()
	require.NoError(t, err)
	assert.Empty(t, keys)

	// Store some tokens
	testTokens := map[string]string{
		"key1": "token1",
		"key2": "token2",
		"key3": "token3",
	}

	for key, token := range testTokens {
		err := store2.Store(key, token)
		require.NoError(t, err)
	}

	// List should return all keys
	keys, err = store2.List()
	require.NoError(t, err)
	assert.Len(t, keys, 3)

	// Convert to map for easier checking
	keyMap := make(map[string]bool)
	for _, key := range keys {
		keyMap[key] = true
	}

	for expectedKey := range testTokens {
		assert.True(t, keyMap[expectedKey], "Expected key %s not found in list", expectedKey)
	}
}

func testUpdateExistingToken(t *testing.T, store TokenStore) {
	t.Helper()

	testKey := "update-test"
	originalToken := "original-token"
	updatedToken := "updated-token"

	// Store original token
	err := store.Store(testKey, originalToken)
	require.NoError(t, err)

	// Update with new token
	err = store.Store(testKey, updatedToken)
	require.NoError(t, err)

	// Retrieve should return updated token
	retrievedToken, err := store.Retrieve(testKey)
	require.NoError(t, err)
	assert.Equal(t, updatedToken, retrievedToken)
}

func TestGeneratePassword(t *testing.T) {
	tests := []struct {
		name    string
		appName string
	}{
		{
			name:    "generates password for normal app name",
			appName: "mcp-bridge",
		},
		{
			name:    "generates password for empty app name",
			appName: "",
		},
		{
			name:    "generates password for app name with special chars",
			appName: "app!@#$%^&*()",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			password := generatePassword(tt.appName)
			assert.NotEmpty(t, password)
			assert.Len(t, password, keyLength)

			// Generate again with same app name - should be deterministic
			password2 := generatePassword(tt.appName)
			assert.Equal(t, password, password2)

			// Generate with different app name - should be different
			differentPassword := generatePassword(tt.appName + "-different")
			assert.NotEqual(t, password, differentPassword)
		})
	}
}

func TestEncryptedFileStoreEncryption(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "encryption-test")
	require.NoError(t, err)

	defer func() { _ = os.RemoveAll(tempDir) }()

	originalHome := os.Getenv("HOME")

	defer func() { _ = os.Setenv("HOME", originalHome) }()

	err = os.Setenv("HOME", tempDir)
	require.NoError(t, err)

	store, err := newEncryptedFileStore("encryption-test")
	require.NoError(t, err)

	fileStore, ok := store.(*encryptedFileStore)
	require.True(t, ok, "store should be *encryptedFileStore")

	t.Run("encrypt and decrypt various data types",
		func(t *testing.T) { testEncryptDecryptVariousDataTypes(t, fileStore) })
	t.Run("decrypt invalid data", func(t *testing.T) { testDecryptInvalidData(t, fileStore) })
	t.Run("encrypt/decrypt produces different ciphertexts",
		func(t *testing.T) { testEncryptProducesDifferentCiphertexts(t, fileStore) })
}

func testEncryptDecryptVariousDataTypes(t *testing.T, fileStore *encryptedFileStore) {
	t.Helper()

	tests := []struct {
		name      string
		plaintext string
	}{
		{
			name:      "encrypt short text",
			plaintext: "hello",
		},
		{
			name:      "encrypt long text",
			plaintext: strings.Repeat("a", 1000),
		},
		{
			name:      "encrypt empty text",
			plaintext: "",
		},
		{
			name:      "encrypt binary-like text",
			plaintext: string([]byte{0x00, 0x01, 0x02, 0xFF, 0xFE, 0xFD}),
		},
		{
			name:      "encrypt JSON token",
			plaintext: `{"access_token":"abc123","token_type":"bearer","expires_in":3600}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test encryption/decryption round trip
			encrypted, err := fileStore.encrypt([]byte(tt.plaintext))
			require.NoError(t, err)
			assert.NotEmpty(t, encrypted)

			// Encrypted data should be different from plaintext
			assert.NotEqual(t, []byte(tt.plaintext), encrypted)

			// Decrypt should return original plaintext
			decrypted, err := fileStore.decrypt(encrypted)
			require.NoError(t, err)

			if tt.plaintext == "" {
				// For empty plaintext, handle nil vs empty slice difference
				assert.Empty(t, decrypted)
			} else {
				assert.Equal(t, []byte(tt.plaintext), decrypted)
			}
		})
	}
}

func testDecryptInvalidData(t *testing.T, fileStore *encryptedFileStore) {
	t.Helper()
	// Test with too short ciphertext
	_, err := fileStore.decrypt([]byte("short"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ciphertext too short")

	// Test with random data
	randomData := make([]byte, 32)
	_, err = rand.Read(randomData)
	require.NoError(t, err)

	_, err = fileStore.decrypt(randomData)
	require.Error(t, err)
}

func testEncryptProducesDifferentCiphertexts(t *testing.T, fileStore *encryptedFileStore) {
	t.Helper()

	plaintext := "test message"

	// Encrypt the same plaintext twice
	encrypted1, err := fileStore.encrypt([]byte(plaintext))
	require.NoError(t, err)

	encrypted2, err := fileStore.encrypt([]byte(plaintext))
	require.NoError(t, err)

	// Should produce different ciphertexts due to random nonce
	assert.NotEqual(t, encrypted1, encrypted2)

	// But both should decrypt to the same plaintext
	decrypted1, err := fileStore.decrypt(encrypted1)
	require.NoError(t, err)

	decrypted2, err := fileStore.decrypt(encrypted2)
	require.NoError(t, err)

	assert.Equal(t, []byte(plaintext), decrypted1)
	assert.Equal(t, []byte(plaintext), decrypted2)
}

func TestEncryptedFileStoreFileOperations(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "file-ops-test")
	require.NoError(t, err)

	defer func() { _ = os.RemoveAll(tempDir) }()

	originalHome := os.Getenv("HOME")

	defer func() { _ = os.Setenv("HOME", originalHome) }()

	err = os.Setenv("HOME", tempDir)
	require.NoError(t, err)

	store, err := newEncryptedFileStore("file-ops-test")
	require.NoError(t, err)

	fileStore, ok := store.(*encryptedFileStore)
	require.True(t, ok, "store should be *encryptedFileStore")

	t.Run("load non-existent file", func(t *testing.T) {
		tokenData, err := fileStore.load()
		require.NoError(t, err)
		assert.NotNil(t, tokenData)
		assert.NotNil(t, tokenData.Tokens)
		assert.Empty(t, tokenData.Tokens)
	})

	t.Run("save and load data", func(t *testing.T) {
		originalData := &tokenData{
			Tokens: map[string]string{
				"key1": "dG9rZW4x", // base64: token1
				"key2": "dG9rZW4y", // base64: token2
			},
		}

		err := fileStore.save(originalData)
		require.NoError(t, err)

		// Verify file exists
		_, err = os.Stat(fileStore.filePath)
		require.NoError(t, err)

		// Load and verify data
		loadedData, err := fileStore.load()
		require.NoError(t, err)
		assert.Equal(t, originalData.Tokens, loadedData.Tokens)
	})

	t.Run("file permissions", func(t *testing.T) {
		if runtime.GOOS == osWindows {
			t.Skip("File permission tests not applicable on Windows")
		}

		// Store a token to create the file
		err := store.Store("test", "token")
		require.NoError(t, err)

		// Check file permissions
		info, err := os.Stat(fileStore.filePath)
		require.NoError(t, err)

		// Should have restricted permissions (0600)
		mode := info.Mode()
		assert.Equal(t, os.FileMode(0o600), mode&0o777)
	})
}

func TestPlatformSpecificStores(t *testing.T) {
	appName := "test-platform-store"

	t.Run("factory returns appropriate store type", func(t *testing.T) {
		store, err := NewTokenStore(appName)
		require.NoError(t, err)
		assert.Implements(t, (*TokenStore)(nil), store)

		// Should work regardless of platform
		err = store.Store("test-key", "test-token")
		require.NoError(t, err)

		token, err := store.Retrieve("test-key")
		require.NoError(t, err)
		assert.Equal(t, "test-token", token)

		err = store.Delete("test-key")
		assert.NoError(t, err)
	})
}

func TestTokenStoreEdgeCases(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "edge-cases-test")
	require.NoError(t, err)

	defer func() { _ = os.RemoveAll(tempDir) }()

	originalHome := os.Getenv("HOME")

	defer func() { _ = os.Setenv("HOME", originalHome) }()

	err = os.Setenv("HOME", tempDir)
	require.NoError(t, err)

	store, err := newEncryptedFileStore("edge-cases-test")
	require.NoError(t, err)

	t.Run("empty key and token", func(t *testing.T) {
		err := store.Store("", "")
		require.NoError(t, err)

		token, err := store.Retrieve("")
		require.NoError(t, err)
		assert.Empty(t, token)
	})

	t.Run("very long key and token", func(t *testing.T) {
		longKey := strings.Repeat("k", 1000)
		longToken := strings.Repeat("t", 10000)

		err := store.Store(longKey, longToken)
		require.NoError(t, err)

		token, err := store.Retrieve(longKey)
		require.NoError(t, err)
		assert.Equal(t, longToken, token)
	})

	t.Run("unicode characters", func(t *testing.T) {
		unicodeKey := "key-🔑-unicode"
		// Use random token with unicode prefix
		unicodeToken := "🎫-ünïcödé-тест-🚀-" + generateTestToken() 

		err := store.Store(unicodeKey, unicodeToken)
		require.NoError(t, err)

		token, err := store.Retrieve(unicodeKey)
		require.NoError(t, err)
		assert.Equal(t, unicodeToken, token)
	})

	t.Run("special characters in key", func(t *testing.T) {
		specialKey := "key/with\\special:chars|<>?\"*"
		specialToken := "token-for-special-key"

		err := store.Store(specialKey, specialToken)
		require.NoError(t, err)

		token, err := store.Retrieve(specialKey)
		require.NoError(t, err)
		assert.Equal(t, specialToken, token)
	})
}
