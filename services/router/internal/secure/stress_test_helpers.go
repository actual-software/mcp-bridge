package secure

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"testing"
	"time"
)

const (
	// StressTestTimeout is the timeout for stress testing operations.
	// Increased to accommodate serialized keychain operations (50 tokens × 3 ops each = 150 ops).
	StressTestTimeout = 5 * time.Minute
	// TokenByteLength is the length in bytes for random tokens.
	TokenByteLength = 32
)

// StressTestEnvironment manages stress testing for token store.
type StressTestEnvironment struct {
	t         *testing.T
	store     TokenStore
	tokens    map[string]string
	numTokens int
	ctx       context.Context
	cancel    context.CancelFunc
}

// EstablishStressTestEnvironment creates a stress test environment.
func EstablishStressTestEnvironment(t *testing.T, storeName string, numTokens int) *StressTestEnvironment {
	t.Helper()

	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	store, err := NewTokenStore(storeName)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), StressTestTimeout)

	return &StressTestEnvironment{
		t:         t,
		store:     store,
		tokens:    make(map[string]string, numTokens),
		numTokens: numTokens,
		ctx:       ctx,
		cancel:    cancel,
	}
}

// GenerateTestTokens creates random test tokens.
func (env *StressTestEnvironment) GenerateTestTokens() {
	for i := 0; i < env.numTokens; i++ {
		key := fmt.Sprintf("stress-key-%d", i)
		token := env.generateRandomToken()
		env.tokens[key] = token
	}
}

// generateRandomToken creates a random token string.
func (env *StressTestEnvironment) generateRandomToken() string {
	tokenBytes := make([]byte, TokenByteLength)
	_, _ = rand.Read(tokenBytes)

	return hex.EncodeToString(tokenBytes)
}

// StressTestOperation represents a timed operation.
type StressTestOperation struct {
	name     string
	duration time.Duration
}

// ExecuteStoreOperations performs bulk store operations.
func (env *StressTestEnvironment) ExecuteStoreOperations() *StressTestOperation {
	start := time.Now()

	for key, token := range env.tokens {
		if err := env.checkTimeout(); err != nil {
			env.t.Fatal("Test timed out during store operations")
		}

		if err := env.store.Store(key, token); err != nil {
			env.t.Errorf("Failed to store %s: %v", key, err)
		}
	}

	return &StressTestOperation{
		name:     "Store",
		duration: time.Since(start),
	}
}

// ExecuteRetrieveOperations performs bulk retrieve operations.
func (env *StressTestEnvironment) ExecuteRetrieveOperations() *StressTestOperation {
	start := time.Now()

	for key, expectedToken := range env.tokens {
		if err := env.checkTimeout(); err != nil {
			env.t.Fatal("Test timed out during retrieve operations")
		}

		env.verifyTokenRetrieval(key, expectedToken)
	}

	return &StressTestOperation{
		name:     "Retrieve",
		duration: time.Since(start),
	}
}

// verifyTokenRetrieval checks a single token retrieval.
func (env *StressTestEnvironment) verifyTokenRetrieval(key, expectedToken string) {
	retrieved, err := env.store.Retrieve(key)
	if err != nil {
		env.t.Errorf("Failed to retrieve %s: %v", key, err)

		return
	}

	if retrieved != expectedToken {
		env.t.Errorf("Token mismatch for %s: got %s, want %s", key, retrieved, expectedToken)
	}
}

// ExecuteListOperation performs list operation.
func (env *StressTestEnvironment) ExecuteListOperation() *StressTestOperation {
	start := time.Now()

	keys, err := env.store.List()

	if err != nil && !errors.Is(err, ErrListNotSupported) {
		env.t.Errorf("List failed: %v", err)
	} else if err == nil && len(keys) < env.numTokens {
		env.t.Errorf("List returned fewer keys than expected: got %d, want at least %d",
			len(keys), env.numTokens)
	}

	return &StressTestOperation{
		name:     "List",
		duration: time.Since(start),
	}
}

// ExecuteDeleteOperations performs bulk delete operations.
func (env *StressTestEnvironment) ExecuteDeleteOperations() *StressTestOperation {
	start := time.Now()

	for key := range env.tokens {
		if err := env.checkTimeout(); err != nil {
			env.t.Fatal("Test timed out during delete operations")
		}

		if err := env.store.Delete(key); err != nil {
			env.t.Errorf("Failed to delete %s: %v", key, err)
		}
	}

	return &StressTestOperation{
		name:     "Delete",
		duration: time.Since(start),
	}
}

// checkTimeout checks if the test context has timed out.
func (env *StressTestEnvironment) checkTimeout() error {
	select {
	case <-env.ctx.Done():
		return env.ctx.Err()
	default:
		return nil
	}
}

// ReportPerformance logs performance metrics.
func (env *StressTestEnvironment) ReportPerformance(operations []*StressTestOperation, totalTime time.Duration) {
	env.t.Logf("Stress test completed:")
	env.t.Logf("  Total operations: %d tokens", env.numTokens)

	for _, op := range operations {
		if op.name == "List" {
			env.t.Logf("  %s time: %v", op.name, op.duration)
		} else {
			opsPerSec := float64(env.numTokens) / op.duration.Seconds()
			env.t.Logf("  %s time: %v (%.2f ops/sec)", op.name, op.duration, opsPerSec)
		}

		env.checkPerformanceThreshold(op)
	}

	env.t.Logf("  Total time: %v", totalTime)
}

// checkPerformanceThreshold checks if operation meets performance expectations.
func (env *StressTestEnvironment) checkPerformanceThreshold(op *StressTestOperation) {
	const thresholdSeconds = 30

	if op.name == "List" {
		return // List operation doesn't have a threshold
	}

	if op.duration.Seconds() > thresholdSeconds {
		env.t.Logf("WARNING: %s operations took longer than expected: %v", op.name, op.duration)
	}
}

// Cleanup cleans up the test environment.
func (env *StressTestEnvironment) Cleanup() {
	env.cancel()
}
