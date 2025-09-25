package secure

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test secret service store on non-Linux platforms.
func TestSecretServiceStore_NonLinux(t *testing.T) {
	if runtime.GOOS == osLinux {
		t.Skip("Skipping non-Linux test on Linux platform")
	}

	store, err := newSecretServiceStore("test-app")
	require.Error(t, err, "Secret service should not be available on non-Linux platforms")
	assert.Nil(t, store, "Store should be nil when creation fails")
	assert.Contains(t, err.Error(), "secret service store not available on this platform")
}

// Test platform-specific fallback behavior in NewTokenStore.
func TestNewTokenStore_PlatformFallbackPaths(t *testing.T) {
	tests := []struct {
		name    string
		appName string
	}{
		{
			name:    "normal_app_name",
			appName: "test-fallback-normal",
		},
		{
			name:    "empty_app_name",
			appName: "",
		},
		{
			name:    "special_characters",
			appName: "test.app@service:123",
		},
		{
			name:    "very_long_name",
			appName: "very-long-app-name-that-tests-edge-cases-with-lengthy-identifiers",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test that NewTokenStore always succeeds due to fallback mechanism
			store, err := NewTokenStore(tt.appName)
			require.NoError(t, err, "NewTokenStore should never fail due to fallback")
			require.NotNil(t, store, "Store should never be nil")

			// Test that the returned store actually works
			testKey := "fallback-test-key"
			testToken := "fallback-test-token"

			// Store operation
			err = store.Store(testKey, testToken)
			require.NoError(t, err, "Store should work with fallback")

			// Retrieve operation
			retrievedToken, err := store.Retrieve(testKey)
			require.NoError(t, err, "Retrieve should work with fallback")
			assert.Equal(t, testToken, retrievedToken, "Token should match")

			// List operation (may fail on some platforms, that's OK)
			keys, err := store.List()
			if err != nil {
				t.Logf("List operation failed (may be expected on this platform): %v", err)
			} else {
				// If list succeeds, our key should be there
				if len(keys) > 0 {
					assert.Contains(t, keys, testKey, "Key should be in list if list succeeds")
				} else {
					t.Log("List returned empty (may be expected on this platform)")
				}
			}

			// Delete operation
			err = store.Delete(testKey)
			require.NoError(t, err, "Delete should work with fallback")

			// Verify deletion
			_, err = store.Retrieve(testKey)
			assert.Error(t, err, "Token should not exist after deletion")
		})
	}
}

// Test NewTokenStore platform detection logic.
func TestNewTokenStore_PlatformBehavior(t *testing.T) {
	store, err := NewTokenStore("platform-detection-test")
	require.NoError(t, err)
	require.NotNil(t, store)

	// Test basic functionality regardless of platform
	testKey := "platform-key"
	testToken := "platform-token"

	err = store.Store(testKey, testToken)
	require.NoError(t, err)

	retrievedToken, err := store.Retrieve(testKey)
	require.NoError(t, err)
	assert.Equal(t, testToken, retrievedToken)

	// Test list functionality (may fail on some platforms)
	keys, err := store.List()
	if err != nil {
		t.Logf("List operation failed (may be expected): %v", err)
	} else {
		if len(keys) > 0 {
			assert.Contains(t, keys, testKey, "Key should be in list if list succeeds")
		} else {
			t.Log("List returned empty (may be expected on this platform)")
		}
	}

	// Clean up
	err = store.Delete(testKey)
	require.NoError(t, err)

	// Verify specific platform behavior expectations
	switch runtime.GOOS {
	case osDarwin:
		t.Log("Running on macOS - should attempt keychain first, fallback to file store if needed")
	case osWindows:
		t.Log("Running on Windows - should attempt credential store first, fallback to file store if needed")
	case osLinux:
		t.Log("Running on Linux - should attempt secret service first, fallback to file store if needed")
	default:
		t.Logf("Running on %s - should use encrypted file store directly", runtime.GOOS)
	}
}

// Test that platform-specific store types implement the interface correctly.
func TestPlatformStores_InterfaceCompliance(t *testing.T) {
	// Test that all platform-specific stores implement TokenStore interface
	var _ TokenStore = &encryptedFileStore{}

	// Test platform-specific store creation doesn't panic
	switch runtime.GOOS {
	case osDarwin:
		store := newKeychainStore("interface-test")

		var _ = store

		assert.NotNil(t, store)

	case "windows":
		// Test that credential store creation returns something (might be error)
		store, err := newCredentialStore("interface-test")
		if err == nil {
			var _ = store

			assert.NotNil(t, store)
		}

	case osLinux:
		// Test that secret service creation returns error on non-Linux or works on Linux
		store, err := newSecretServiceStore("interface-test")
		if err == nil {
			var _ = store

			assert.NotNil(t, store)
		} else {
			assert.Contains(t, err.Error(), "secret service store not available")
		}
	}
}

// Test edge cases in NewTokenStore logic branches.
func TestNewTokenStore_LogicBranches(t *testing.T) {
	t.Run("multiple_consecutive_calls", func(t *testing.T) {
		// Test that multiple calls to NewTokenStore work consistently
		store1, err1 := NewTokenStore("branch-test-1")
		require.NoError(t, err1)
		require.NotNil(t, store1)

		store2, err2 := NewTokenStore("branch-test-2")
		require.NoError(t, err2)
		require.NotNil(t, store2)

		// Stores should be independent
		err := store1.Store("key1", "token1")
		require.NoError(t, err)

		err = store2.Store("key2", "token2")
		require.NoError(t, err)

		// Cross-contamination check
		_, err = store1.Retrieve("key2")
		require.Error(t, err, "Store1 should not contain Store2's keys")

		_, err = store2.Retrieve("key1")
		require.Error(t, err, "Store2 should not contain Store1's keys")

		// Cleanup
		_ = store1.Delete("key1")
		_ = store2.Delete("key2")
	})

	t.Run("app_name_normalization", func(t *testing.T) {
		// Test that different app names create isolated stores
		appNames := []string{
			"test-normal",
			"test.with.dots",
			"test_with_underscores",
			"test@with.email:style",
		}

		stores := make([]TokenStore, len(appNames))

		for i, appName := range appNames {
			store, err := NewTokenStore(appName)
			require.NoError(t, err)
			require.NotNil(t, store)
			stores[i] = store

			// Store a unique value in each store
			err = store.Store("unique-key", "token-for-"+appName)
			require.NoError(t, err)
		}

		// Verify isolation between stores
		for i, store := range stores {
			token, err := store.Retrieve("unique-key")
			require.NoError(t, err)

			expected := "token-for-" + appNames[i]
			assert.Equal(t, expected, token)

			// Cleanup
			_ = store.Delete("unique-key")
		}
	})
}
