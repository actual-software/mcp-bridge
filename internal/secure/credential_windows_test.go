//go:build windows

package secure

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Mock Windows API for testing
type mockWindowsAPI struct {
	mu          sync.RWMutex
	credentials map[string]*CREDENTIAL
	failures    map[string]error
	callCounts  map[string]int
}

func newMockWindowsAPI() *mockWindowsAPI {
	return &mockWindowsAPI{
		credentials: make(map[string]*CREDENTIAL),
		failures:    make(map[string]error),
		callCounts:  make(map[string]int),
	}
}

func (m *mockWindowsAPI) setFailure(operation string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failures[operation] = err
}

func (m *mockWindowsAPI) clearFailures() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failures = make(map[string]error)
}

func (m *mockWindowsAPI) getCallCount(operation string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.callCounts[operation]
}

func (m *mockWindowsAPI) incrementCallCount(operation string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.callCounts[operation]++
}

// Mock implementations
func (m *mockWindowsAPI) credWrite(cred *CREDENTIAL, flags uintptr) (uintptr, error) {
	m.incrementCallCount("credWrite")

	m.mu.Lock()
	defer m.mu.Unlock()

	if err, exists := m.failures["credWrite"]; exists {
		return 0, err
	}

	// Convert target name from UTF-16
	targetName := utf16PtrToString(cred.TargetName)

	// Create a copy of the credential
	credCopy := &CREDENTIAL{
		Flags:              cred.Flags,
		Type:               cred.Type,
		TargetName:         cred.TargetName,
		Comment:            cred.Comment,
		LastWritten:        cred.LastWritten,
		CredentialBlobSize: cred.CredentialBlobSize,
		CredentialBlob:     cred.CredentialBlob,
		Persist:            cred.Persist,
		AttributeCount:     cred.AttributeCount,
		Attributes:         cred.Attributes,
		TargetAlias:        cred.TargetAlias,
		UserName:           cred.UserName,
	}

	m.credentials[targetName] = credCopy

	return 1, nil
}

func (m *mockWindowsAPI) credRead(targetName *uint16, credType uint32, flags uint32, credential **CREDENTIAL) (uintptr, error) {
	m.incrementCallCount("credRead")

	m.mu.RLock()
	defer m.mu.RUnlock()

	if err, exists := m.failures["credRead"]; exists {
		return 0, err
	}

	target := utf16PtrToString(targetName)
	if cred, exists := m.credentials[target]; exists {
		*credential = cred
		return 1, nil
	}

	return 0, syscall.ERROR_NOT_FOUND
}

func (m *mockWindowsAPI) credDelete(targetName *uint16, credType uint32, flags uint32) (uintptr, error) {
	m.incrementCallCount("credDelete")

	m.mu.Lock()
	defer m.mu.Unlock()

	if err, exists := m.failures["credDelete"]; exists {
		return 0, err
	}

	target := utf16PtrToString(targetName)
	if _, exists := m.credentials[target]; exists {
		delete(m.credentials, target)
		return 1, nil
	}

	return 0, syscall.ERROR_NOT_FOUND
}

func (m *mockWindowsAPI) credFree() {
	m.incrementCallCount("credFree")
	// Mock implementation - no actual memory to free
}

// Helper function to convert UTF-16 pointer to string
func utf16PtrToString(ptr *uint16) string {
	if ptr == nil {
		return ""
	}
	// This is a simplified implementation for testing
	// In real implementation, we'd need to walk the memory properly
	return "mocked-target-name"
}

var mockAPI *mockWindowsAPI

// Override system calls for testing
func mockSystemCalls() {
	mockAPI = newMockWindowsAPI()
}

func restoreSystemCalls() {
	mockAPI = nil
}

func TestCredentialStore_NewCredentialStore(t *testing.T) {
	tests := []struct {
		name    string
		appName string
		wantErr bool
	}{
		{
			name:    "creates store with valid app name",
			appName: "test-app",
			wantErr: false,
		},
		{
			name:    "creates store with empty app name",
			appName: "",
			wantErr: false,
		},
		{
			name:    "creates store with special characters",
			appName: "test-app!@#$%",
			wantErr: false,
		},
		{
			name:    "creates store with unicode",
			appName: "test-app-ünïcödé",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, err := newCredentialStore(tt.appName)

			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, store)
			} else {
				require.NoError(t, err)
				require.NotNil(t, store)

				credStore, ok := store.(*credentialStore)
				require.True(t, ok)
				assert.Equal(t, fmt.Sprintf("%s-mcp-router", tt.appName), credStore.targetPrefix)
			}
		})
	}
}

func TestCredentialStore_Store(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows credential store tests require Windows")
	}

	mockSystemCalls()
	defer restoreSystemCalls()

	store, err := newCredentialStore("test-app")
	require.NoError(t, err)

	tests := getCredentialStoreStoreTestCases()
	runCredentialStoreStoreTests(t, store, tests)
}

func getCredentialStoreStoreTestCases() []struct {
	name      string
	key       string
	token     string
	mockError error
	wantErr   bool
	errMsg    string
} {
	return []struct {
		name      string
		key       string
		token     string
		mockError error
		wantErr   bool
		errMsg    string
	}{
		{
			name:    "store valid token",
			key:     "service1",
			token:   "valid-token-123",
			wantErr: false,
		},
		{
			name:    "store empty token",
			key:     "service2",
			token:   "",
			wantErr: false,
		},
		{
			name:    "store empty key",
			key:     "",
			token:   "token-for-empty-key",
			wantErr: false,
		},
		{
			name:    "store large token",
			key:     "service3",
			token:   strings.Repeat("a", 10000),
			wantErr: false,
		},
		{
			name:    "store unicode token",
			key:     "service4",
			token:   "token-with-üníçödé-characters-🔑",
			wantErr: false,
		},
		{
			name:      "windows api failure",
			key:       "service5",
			token:     "token",
			mockError: fmt.Errorf("access denied"),
			wantErr:   true,
			errMsg:    "failed to store credential",
		},
	}
}

func runCredentialStoreStoreTests(t *testing.T, store *windowsCredentialStore, tests []struct {
	name      string
	key       string
	token     string
	mockError error
	wantErr   bool
	errMsg    string
}) {
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupMockForStoreTest(tt.mockError)

			err := store.Store(tt.key, tt.token)

			validateStoreTestResult(t, err, tt.wantErr, tt.errMsg)
		})
	}
}

func setupMockForStoreTest(mockError error) {
	if mockError != nil {
		mockAPI.setFailure("credWrite", mockError)
	} else {
		mockAPI.clearFailures()
	}
}

func validateStoreTestResult(t *testing.T, err error, wantErr bool, errMsg string) {
	if wantErr {
		require.Error(t, err)
		if errMsg != "" {
			assert.Contains(t, err.Error(), errMsg)
		}
	} else {
		require.NoError(t, err)
		// Verify the call was made
		assert.Greater(t, mockAPI.getCallCount("credWrite"), 0)
	}
}

func TestCredentialStore_Retrieve(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows credential store tests require Windows")
	}

	mockSystemCalls()
	defer restoreSystemCalls()

	store, err := newCredentialStore("test-app")
	require.NoError(t, err)

	tests := getCredentialStoreRetrieveTestCases()
	runCredentialStoreRetrieveTests(t, store, tests)
}

func getCredentialStoreRetrieveTestCases() []struct {
	name        string
	key         string
	setupToken  string
	mockError   error
	wantErr     bool
	errMsg      string
	expectToken string
} {
	return []struct {
		name        string
		key         string
		setupToken  string
		mockError   error
		wantErr     bool
		errMsg      string
		expectToken string
	}{
		{
			name:        "retrieve existing token",
			key:         "service1",
			setupToken:  "stored-token-123",
			expectToken: "stored-token-123",
			wantErr:     false,
		},
		{
			name:        "retrieve empty token",
			key:         "service2",
			setupToken:  "",
			expectToken: "",
			wantErr:     false,
		},
		{
			name:      "retrieve non-existent token",
			key:       "non-existent",
			mockError: syscall.ERROR_NOT_FOUND,
			wantErr:   true,
			errMsg:    "failed to read credential",
		},
		{
			name:      "windows api failure",
			key:       "service3",
			mockError: fmt.Errorf("access denied"),
			wantErr:   true,
			errMsg:    "failed to read credential",
		},
	}
}

func runCredentialStoreRetrieveTests(t *testing.T, store *windowsCredentialStore, tests []struct {
	name        string
	key         string
	setupToken  string
	mockError   error
	wantErr     bool
	errMsg      string
	expectToken string
}) {
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupTokenForRetrieveTest(t, store, tt.key, tt.setupToken)
			setupMockForRetrieveTest(tt.mockError, tt.setupToken, tt.key)

			token, err := store.Retrieve(tt.key)

			validateRetrieveTestResult(t, token, err, tt.wantErr, tt.errMsg, tt.expectToken)
		})
	}
}

func setupTokenForRetrieveTest(t *testing.T, store *windowsCredentialStore, key, setupToken string) {
	// Setup: store token if needed
	if setupToken != "" {
		mockAPI.clearFailures()
		err := store.Store(key, setupToken)
		require.NoError(t, err)
	}
}

func setupMockForRetrieveTest(mockError error, setupToken, key string) {
	// Set up mock failure if needed
	if mockError != nil {
		mockAPI.setFailure("credRead", mockError)
	} else if setupToken == "" && key != "non-existent" {
		mockAPI.clearFailures()
	}
}

func validateRetrieveTestResult(t *testing.T, token string, err error, wantErr bool, errMsg, expectToken string) {
	if wantErr {
		require.Error(t, err)
		if errMsg != "" {
			assert.Contains(t, err.Error(), errMsg)
		}
	} else {
		require.NoError(t, err)
		assert.Equal(t, expectToken, token)

		// Verify API calls
		assert.Greater(t, mockAPI.getCallCount("credRead"), 0)
		assert.Greater(t, mockAPI.getCallCount("credFree"), 0)
	}
}

func TestCredentialStore_Delete(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows credential store tests require Windows")
	}

	mockSystemCalls()
	defer restoreSystemCalls()

	store, err := newCredentialStore("test-app")
	require.NoError(t, err)

	tests := getCredentialStoreDeleteTestCases()
	runCredentialStoreDeleteTests(t, store, tests)
}

func getCredentialStoreDeleteTestCases() []struct {
	name       string
	key        string
	setupToken string
	mockError  error
	wantErr    bool
	errMsg     string
} {
	return []struct {
		name       string
		key        string
		setupToken string
		mockError  error
		wantErr    bool
		errMsg     string
	}{
		{
			name:       "delete existing token",
			key:        "service1",
			setupToken: "token-to-delete",
			wantErr:    false,
		},
		{
			name:    "delete non-existent token (should not error)",
			key:     "non-existent",
			wantErr: false,
		},
		{
			name:      "windows api failure",
			key:       "service2",
			mockError: fmt.Errorf("access denied"),
			wantErr:   true,
			errMsg:    "failed to delete credential",
		},
	}
}

func runCredentialStoreDeleteTests(t *testing.T, store *windowsCredentialStore, tests []struct {
	name       string
	key        string
	setupToken string
	mockError  error
	wantErr    bool
	errMsg     string
}) {
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupTokenForDeleteTest(t, store, tt.key, tt.setupToken)
			setupMockForDeleteTest(tt.mockError)

			err := store.Delete(tt.key)

			validateDeleteTestResult(t, err, tt.wantErr, tt.errMsg)
		})
	}
}

func setupTokenForDeleteTest(t *testing.T, store *windowsCredentialStore, key, setupToken string) {
	// Setup: store token if needed
	if setupToken != "" {
		mockAPI.clearFailures()
		err := store.Store(key, setupToken)
		require.NoError(t, err)
	}
}

func setupMockForDeleteTest(mockError error) {
	// Set up mock failure if needed
	if mockError != nil {
		mockAPI.setFailure("credDelete", mockError)
	} else {
		mockAPI.clearFailures()
	}
}

func validateDeleteTestResult(t *testing.T, err error, wantErr bool, errMsg string) {
	if wantErr {
		require.Error(t, err)
		if errMsg != "" {
			assert.Contains(t, err.Error(), errMsg)
		}
	} else {
		require.NoError(t, err)
		assert.Greater(t, mockAPI.getCallCount("credDelete"), 0)
	}
}

func TestCredentialStore_List(t *testing.T) {
	store, err := newCredentialStore("test-app")
	require.NoError(t, err)

	// List should return empty list (not implemented on Windows)
	keys, err := store.List()
	require.NoError(t, err)
	assert.Empty(t, keys)
}

func TestCredentialStore_GetTargetName(t *testing.T) {
	store, err := newCredentialStore("test-app")
	require.NoError(t, err)

	credStore := store.(*credentialStore)

	tests := []struct {
		name     string
		key      string
		expected string
	}{
		{
			name:     "normal key",
			key:      "service1",
			expected: "test-app-mcp-router:service1",
		},
		{
			name:     "empty key",
			key:      "",
			expected: "test-app-mcp-router:",
		},
		{
			name:     "key with special chars",
			key:      "service/with:special@chars",
			expected: "test-app-mcp-router:service/with:special@chars",
		},
		{
			name:     "unicode key",
			key:      "service-ünïcödé",
			expected: "test-app-mcp-router:service-ünïcödé",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := credStore.getTargetName(tt.key)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCredentialStore_ConcurrentAccess(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows credential store tests require Windows")
	}

	mockSystemCalls()
	defer restoreSystemCalls()

	store, err := newCredentialStore("test-concurrent")
	require.NoError(t, err)

	runConcurrentStoreOperations(t, store)
	runConcurrentRetrieveOperations(t, store)
}

func runConcurrentStoreOperations(t *testing.T, store *windowsCredentialStore) {
	const numGoroutines = 10
	const numOperations = 5

	var wg sync.WaitGroup
	errors := make([]error, numGoroutines*numOperations)
	errorsMu := sync.Mutex{}
	errorIndex := 0

	// Concurrent stores
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			performConcurrentStoreOperations(id, numOperations, store, errors, &errorsMu, &errorIndex)
		}(i)
	}

	wg.Wait()
	validateConcurrentOperationResults(t, errors, numGoroutines*numOperations, "Store")
}

func performConcurrentStoreOperations(id, numOperations int, store *windowsCredentialStore, errors []error, errorsMu *sync.Mutex, errorIndex *int) {
	for j := 0; j < numOperations; j++ {
		key := fmt.Sprintf("concurrent-key-%d-%d", id, j)
		token := fmt.Sprintf("concurrent-token-%d-%d", id, j)

		err := store.Store(key, token)

		errorsMu.Lock()
		errors[*errorIndex] = err
		*errorIndex++
		errorsMu.Unlock()
	}
}

func runConcurrentRetrieveOperations(t *testing.T, store *windowsCredentialStore) {
	const numGoroutines = 10
	const numOperations = 5

	var wg sync.WaitGroup
	errors := make([]error, numGoroutines*numOperations)
	errorsMu := sync.Mutex{}
	errorIndex := 0

	// Concurrent reads
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			performConcurrentRetrieveOperations(id, numOperations, store, errors, &errorsMu, &errorIndex)
		}(i)
	}

	wg.Wait()
	validateConcurrentOperationResults(t, errors, numGoroutines*numOperations, "Retrieve")
}

func performConcurrentRetrieveOperations(id, numOperations int, store *windowsCredentialStore, errors []error, errorsMu *sync.Mutex, errorIndex *int) {
	for j := 0; j < numOperations; j++ {
		key := fmt.Sprintf("concurrent-key-%d-%d", id, j)
		expectedToken := fmt.Sprintf("concurrent-token-%d-%d", id, j)

		token, err := store.Retrieve(key)

		errorsMu.Lock()
		if err != nil {
			errors[*errorIndex] = err
		} else if token != expectedToken {
			errors[*errorIndex] = fmt.Errorf("token mismatch: expected %s, got %s", expectedToken, token)
		}
		*errorIndex++
		errorsMu.Unlock()
	}
}

func validateConcurrentOperationResults(t *testing.T, errors []error, expectedCount int, operationType string) {
	// Check that all operations succeeded
	for i, err := range errors {
		if i < expectedCount {
			assert.NoError(t, err, "%s operation %d failed", operationType, i)
		}
	}
}

func TestCredentialStore_MemorySafety(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows credential store tests require Windows")
	}

	mockSystemCalls()

	defer restoreSystemCalls()

	store, err := newCredentialStore("test-memory")
	require.NoError(t, err)

	// Test with large tokens that might cause memory issues
	tests := []struct {
		name      string
		tokenSize int
	}{
		{"small token", 100},
		{"medium token", 1024},
		{"large token", 10000},
		{"very large token", 100000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token := strings.Repeat("a", tt.tokenSize)
			key := fmt.Sprintf("memory-test-%d", tt.tokenSize)

			// Store
			err := store.Store(key, token)
			require.NoError(t, err)

			// Retrieve
			retrieved, err := store.Retrieve(key)
			require.NoError(t, err)
			assert.Equal(t, token, retrieved)

			// Delete
			err = store.Delete(key)
			require.NoError(t, err)
		})
	}
}

func TestCredentialStore_ErrorHandling(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows credential store tests require Windows")
	}

	mockSystemCalls()

	defer restoreSystemCalls()

	store, err := newCredentialStore("test-error")
	require.NoError(t, err)

	// Test various error conditions
	t.Run("store with api failure", func(t *testing.T) {
		mockAPI.setFailure("credWrite", fmt.Errorf("access denied"))

		err := store.Store("test-key", "test-token")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to store credential")
	})

	t.Run("retrieve with api failure", func(t *testing.T) {
		mockAPI.setFailure("credRead", fmt.Errorf("access denied"))

		_, err := store.Retrieve("test-key")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to read credential")
	})

	t.Run("delete with api failure", func(t *testing.T) {
		mockAPI.setFailure("credDelete", fmt.Errorf("access denied"))

		err := store.Delete("test-key")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to delete credential")
	})

	t.Run("element not found error handling", func(t *testing.T) {
		mockAPI.clearFailures()

		// Try to delete non-existent credential - should not error
		err := store.Delete("non-existent-key")
		require.NoError(t, err)
	})
}

func TestCredentialStore_UTF16Conversion(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows credential store tests require Windows")
	}

	store, err := newCredentialStore("test-utf16")
	require.NoError(t, err)

	credStore := store.(*credentialStore)

	// Test UTF-16 conversion edge cases
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "normal ascii",
			input:   "normal-key",
			wantErr: false,
		},
		{
			name:    "unicode characters",
			input:   "key-with-ünïcödé",
			wantErr: false,
		},
		{
			name:    "emoji characters",
			input:   "key-with-🔑-emoji",
			wantErr: false,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			targetName := credStore.getTargetName(tt.input)

			// Try to convert to UTF-16 (this is what the real implementation does)
			_, err := syscall.UTF16PtrFromString(targetName)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestCredentialStore_SystemIntegration(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows credential store tests require Windows")
	}

	if testing.Short() {
		t.Skip("Skipping system integration test in short mode")
	}

	// This test uses the real Windows API (not mocked)
	// It should only run in integration test environments
	store, err := newCredentialStore("mcp-test-integration")
	require.NoError(t, err)

	testKey := "integration-test-key"
	testToken := "integration-test-token-12345"

	// Clean up any existing test data
	_ = store.Delete(testKey)

	t.Run("full integration cycle", func(t *testing.T) {
		// Store
		err := store.Store(testKey, testToken)
		require.NoError(t, err)

		// Retrieve
		retrieved, err := store.Retrieve(testKey)
		require.NoError(t, err)
		assert.Equal(t, testToken, retrieved)

		// Update
		updatedToken := "updated-integration-test-token-67890"
		err = store.Store(testKey, updatedToken)
		require.NoError(t, err)

		retrieved, err = store.Retrieve(testKey)
		require.NoError(t, err)
		assert.Equal(t, updatedToken, retrieved)

		// Delete
		err = store.Delete(testKey)
		require.NoError(t, err)

		// Verify deletion
		_, err = store.Retrieve(testKey)
		require.Error(t, err)
	})

	// Clean up
	_ = store.Delete(testKey)
}

// Benchmark tests for performance validation
func BenchmarkCredentialStore_Store(b *testing.B) {
	if runtime.GOOS != "windows" {
		b.Skip("Windows credential store benchmarks require Windows")
	}

	mockSystemCalls()

	defer restoreSystemCalls()

	store, err := newCredentialStore("bench-store")
	require.NoError(b, err)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("bench-key-%d", i)
		token := fmt.Sprintf("bench-token-%d", i)
		_ = store.Store(key, token)
	}
}

func BenchmarkCredentialStore_Retrieve(b *testing.B) {
	if runtime.GOOS != "windows" {
		b.Skip("Windows credential store benchmarks require Windows")
	}

	mockSystemCalls()

	defer restoreSystemCalls()

	store, err := newCredentialStore("bench-retrieve")
	require.NoError(b, err)

	// Pre-store some tokens
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("bench-key-%d", i)
		token := fmt.Sprintf("bench-token-%d", i)
		_ = store.Store(key, token)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("bench-key-%d", i%100)
		_, _ = store.Retrieve(key)
	}
}

func BenchmarkCredentialStore_GetTargetName(b *testing.B) {
	store, err := newCredentialStore("bench-target")
	require.NoError(b, err)

	credStore := store.(*credentialStore)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("bench-key-%d", i)
		_ = credStore.getTargetName(key)
	}
}
