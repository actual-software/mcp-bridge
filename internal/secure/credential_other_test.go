//go:build !windows

package secure

import (
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCredentialStore_UnsupportedPlatform(t *testing.T) {
	// Test that credential store returns appropriate error on non-Windows platforms
	tests := []struct {
		name    string
		appName string
	}{
		{
			name:    "normal app name",
			appName: "test-app",
		},
		{
			name:    "empty app name",
			appName: "",
		},
		{
			name:    "app name with special characters",
			appName: "test-app!@#$%",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, err := newCredentialStore(tt.appName)

			require.Error(t, err)
			assert.Nil(t, store)
			assert.Contains(t, err.Error(), "credential store not available on this platform")
		})
	}
}

func TestCredentialStore_PlatformDetection(t *testing.T) {
	// Verify that we correctly detect non-Windows platforms
	switch runtime.GOOS {
	case osWindows:
		t.Skip("This test is for non-Windows platforms")
	case osDarwin, osLinux, "freebsd", "openbsd", "netbsd":
		// These are expected non-Windows platforms
		t.Logf("Testing on platform: %s", runtime.GOOS)
	default:
		t.Logf("Testing on unknown platform: %s", runtime.GOOS)
	}

	// Ensure the credential store factory fails appropriately
	store, err := newCredentialStore("platform-test")
	require.Error(t, err)
	assert.Nil(t, store)
}

func TestCredentialStore_ErrorMessage(t *testing.T) {
	// Test that error messages are informative
	store, err := newCredentialStore("error-test")

	require.Error(t, err)
	assert.Nil(t, store)

	// Error should be clear about platform not being supported
	assert.Contains(t, err.Error(), "not available on this platform")

	// Error should not contain sensitive information
	assert.NotContains(t, err.Error(), "error-test") // app name should not be in error
}

func TestCredentialStore_ConsistentBehavior(t *testing.T) {
	// Test that multiple calls return consistent errors
	appName := "consistent-test"

	// Call multiple times
	for i := range 5 {
		store, err := newCredentialStore(appName)

		require.Error(t, err, "Call %d should fail", i+1)
		assert.Nil(t, store, "Call %d should return nil store", i+1)
		assert.Contains(t, err.Error(), "not available on this platform", "Call %d error message", i+1)
	}
}

func TestCredentialStore_ThreadSafety(t *testing.T) {
	// Test concurrent calls to newCredentialStore
	const numGoroutines = 10

	errors := make([]error, numGoroutines)
	stores := make([]TokenStore, numGoroutines)

	done := make(chan int, numGoroutines)

	for i := range numGoroutines {
		go func(id int) {
			defer func() { done <- id }()

			appName := "thread-test"
			store, err := newCredentialStore(appName)

			errors[id] = err
			stores[id] = store
		}(i)
	}

	// Wait for all goroutines to complete
	for range numGoroutines {
		<-done
	}

	// Verify all calls failed consistently
	for i := range numGoroutines {
		require.Error(t, errors[i], "Goroutine %d should have failed", i)
		assert.Nil(t, stores[i], "Goroutine %d should return nil store", i)
		assert.Contains(t, errors[i].Error(), "not available on this platform", "Goroutine %d error message", i)
	}
}

func TestCredentialStore_IntegrationWithTokenStoreFactory(t *testing.T) {
	// Test that NewTokenStore falls back correctly on non-Windows platforms
	// This ensures the platform detection in token_store.go works properly
	store, err := NewTokenStore("integration-test")

	// On non-Windows platforms, it should fall back to encrypted file store
	require.NoError(t, err, "NewTokenStore should fall back to encrypted file store")
	require.NotNil(t, store, "NewTokenStore should return a valid store")

	// Verify it's not a credential store (credential store type only exists on Windows)
	// On non-Windows platforms, we should get a platform-appropriate store (keychain, secret service, or encrypted file)
	storeType := fmt.Sprintf("%T", store)
	isValidStore := strings.Contains(storeType, "encryptedFileStore") ||
		strings.Contains(storeType, "keychainStore") ||
		strings.Contains(storeType, "secretServiceStore")
	assert.True(t, isValidStore, "Should be a valid non-Windows store type, got: %s", storeType)

	// Verify it implements the TokenStore interface
	assert.Implements(t, (*TokenStore)(nil), store)
}

func TestCredentialStore_PlatformSpecificFallback(t *testing.T) {
	// Test platform-specific behavior based on GOOS
	switch runtime.GOOS {
	case "darwin":
		t.Run("macOS should try keychain first", func(t *testing.T) {
			// NewTokenStore should try keychain, then fall back to encrypted file
			store, err := NewTokenStore("macos-test")
			require.NoError(t, err)
			require.NotNil(t, store)

			// Should be a valid macOS store (keychain or encrypted file)
			storeType := fmt.Sprintf("%T", store)
			isValidStore := strings.Contains(storeType, "encryptedFileStore") ||
				strings.Contains(storeType, "keychainStore")
			assert.True(t, isValidStore, "Should be a valid macOS store type, got: %s", storeType)
		})

	case osLinux:
		t.Run("Linux should try secret service first", func(t *testing.T) {
			// NewTokenStore should try secret service, then fall back to encrypted file
			store, err := NewTokenStore("linux-test")
			require.NoError(t, err)
			require.NotNil(t, store)

			// Should be a valid Linux store (secret service or encrypted file)
			storeType := fmt.Sprintf("%T", store)
			isValidStore := strings.Contains(storeType, "encryptedFileStore") ||
				strings.Contains(storeType, "secretServiceStore")
			assert.True(t, isValidStore, "Should be a valid Linux store type, got: %s", storeType)
		})

	default:
		t.Run("other platforms should use encrypted file", func(t *testing.T) {
			// NewTokenStore should use encrypted file store
			store, err := NewTokenStore("other-platform-test")
			require.NoError(t, err)
			require.NotNil(t, store)

			// Should be encrypted file store on other platforms
			storeType := fmt.Sprintf("%T", store)
			assert.Contains(t, storeType, "encryptedFileStore",
				"Should be encrypted file store on other platforms, got: %s", storeType)
		})
	}
}

func TestCredentialStore_DocumentationConsistency(t *testing.T) {
	// Ensure the behavior matches what's documented
	// newCredentialStore should not be available on non-Windows platforms
	store, err := newCredentialStore("doc-test")

	require.Error(t, err)
	assert.Nil(t, store)

	// Error message should be clear and consistent with documentation
	expectedMsg := "credential store not available on this platform"
	assert.Contains(t, err.Error(), expectedMsg)
}
