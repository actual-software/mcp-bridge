// Package patterns_test demonstrates improved testing patterns for MCP components.
//

package patterns_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
)

// Test timing constants.
const (
	defaultTestTimeout        = 30 * time.Second
	defaultRateLimitTolerance = 50 * time.Millisecond
	defaultTestRetryDelay     = 50 * time.Millisecond
	
	// Test configuration constants.
	defaultRetryCount = 3
	defaultConcurrentWorkers = 10
	defaultPercentMultiplier = 100
	defaultTimeoutMillis = 50
	defaultToleranceLower = 0.8
	defaultToleranceUpper = 1.2
	testTickerInterval = 10 * time.Millisecond
	testRateLimitBurst = 5
	workerSimulationDelay = 100 * time.Millisecond
	workerTimeoutDuration = 5 * time.Second
)

// BaseTestSuite provides consistent test patterns and utilities.
type BaseTestSuite struct {
	suite.Suite
	ctx        context.Context 
	cancel     context.CancelFunc
	timeout    time.Duration
	retryCount int
	resources  []func() error
	mu         sync.Mutex
}

// SetupSuite runs once before all tests in the suite.
func (s *BaseTestSuite) SetupSuite() {
	s.timeout = getEnvDuration("TEST_TIMEOUT", defaultTestTimeout)
	s.retryCount = getEnvInt("TEST_RETRY_COUNT", defaultRetryCount)
	s.resources = make([]func() error, 0)

	s.T().Logf("Test suite setup complete - timeout: %v, retries: %d", s.timeout, s.retryCount)
}

// SetupTest runs before each test.
func (s *BaseTestSuite) SetupTest() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), s.timeout)
	s.resources = make([]func() error, 0) // Reset resources for each test
}

// TearDownTest runs after each test.
func (s *BaseTestSuite) TearDownTest() {
	// Clean up resources in reverse order
	s.mu.Lock()

	for i := len(s.resources) - 1; i >= 0; i-- {
		if cleanup := s.resources[i]; cleanup != nil {
			if err := cleanup(); err != nil {
				s.T().Logf("Cleanup error: %v", err)
			}
		}
	}

	s.resources = nil
	s.mu.Unlock()

	if s.cancel != nil {
		s.cancel()
	}
}

// AddCleanup adds a cleanup function to be called during teardown.
func (s *BaseTestSuite) AddCleanup(cleanup func() error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.resources = append(s.resources, cleanup)
}

// WaitForCondition waits for a condition to be true with configurable timeout and retry.
func (s *BaseTestSuite) WaitForCondition(description string, condition func() bool) bool {
	return s.WaitForConditionWithTimeout(description, condition, s.timeout)
}

// WaitForConditionWithTimeout waits for condition with specific timeout.
func (s *BaseTestSuite) WaitForConditionWithTimeout(
	description string, condition func() bool, timeout time.Duration,
) bool {
	ctx, cancel := context.WithTimeout(s.ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(testTickerInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.T().Logf("Condition '%s' not met within timeout %v", description, timeout)

			return false
		case <-ticker.C:
			if condition() {
				s.T().Logf("Condition '%s' met", description)

				return true
			}
		}
	}
}

// RetryOperation retries an operation with exponential backoff.
func (s *BaseTestSuite) RetryOperation(description string, operation func() error) error {
	var lastErr error

	backoff := defaultPercentMultiplier * time.Millisecond

	for attempt := range s.retryCount {
		if err := operation(); err == nil {
			s.T().Logf("Operation '%s' succeeded on attempt %d", description, attempt+1)

			return nil
		} else {
			lastErr = err
			s.T().Logf("Operation '%s' failed on attempt %d: %v", description, attempt+1, err)

			if attempt < s.retryCount-1 {
				time.Sleep(backoff)
				backoff *= 2 // Exponential backoff
			}
		}
	}

	return fmt.Errorf("operation '%s' failed after %d attempts: %w", description, s.retryCount, lastErr)
}

// TimingDependentTestSuite provides testing patterns for timing-sensitive tests.
type TimingDependentTestSuite struct {
	BaseTestSuite
	timekeeper *MockTimeKeeper
}

// SetupTest initializes timing-dependent test resources.
func (s *TimingDependentTestSuite) SetupTest() {
	s.BaseTestSuite.SetupTest()
	s.timekeeper = NewMockTimeKeeper()
	s.AddCleanup(func() error {
		s.timekeeper = nil

		return nil
	})
}

// TestRetryLogicImproved demonstrates improved retry testing patterns.
func (s *TimingDependentTestSuite) TestRetryLogicImproved() {
	attemptCount := int64(0)
	maxAttempts := int64(defaultRetryCount)

	operation := func() error {
		count := atomic.AddInt64(&attemptCount, 1)
		if count < maxAttempts {
			return errors.New("simulated failure")
		}

		return nil
	}

	err := s.RetryOperation("test operation", operation)
	s.Require().NoError(err)
	s.Equal(maxAttempts, atomic.LoadInt64(&attemptCount))
}

// TestRateLimitingPatterns demonstrates improved rate limiting testing.
func (s *TimingDependentTestSuite) TestRateLimitingPatterns() {
	const requestsPerSecond = 10

	const testDuration = 1 * time.Second

	rateLimiter := NewMockRateLimiter(requestsPerSecond)
	successfulRequests := 0

	startTime := time.Now()

	for range 20 { // Try to make 20 requests
		if rateLimiter.Allow() {
			successfulRequests++
		}

		time.Sleep(defaultTestRetryDelay)
	}

	elapsed := time.Since(startTime)

	// Allow some tolerance for timing variations
	minExpected := int(float64(requestsPerSecond) * defaultToleranceLower)
	maxExpected := int(float64(requestsPerSecond) * defaultToleranceUpper)

	s.GreaterOrEqual(successfulRequests, minExpected)
	s.LessOrEqual(successfulRequests, maxExpected)
	s.WithinDuration(startTime.Add(testDuration), startTime.Add(elapsed), defaultRateLimitTolerance)
}

// ResourceCleanupTestSuite provides testing patterns for resource cleanup.
type ResourceCleanupTestSuite struct {
	BaseTestSuite
	tempFiles []string
	mu        sync.Mutex
}

// TestFileResourceCleanup demonstrates proper file resource cleanup.
func (s *ResourceCleanupTestSuite) TestFileResourceCleanup() {
	filename := s.createTempFile("test-content")

	// Register cleanup
	s.AddCleanup(func() error {
		if err := os.Remove(filename); err != nil {
			return fmt.Errorf("failed to remove temp file %s: %w", filename, err)
		}

		return nil
	})

	// Use the file
	content, err := os.ReadFile(filename) //nolint:gosec // Test file reading 
	s.Require().NoError(err)
	s.Equal("test-content", string(content))
}

// TestConcurrentResourceCleanup demonstrates concurrent resource cleanup patterns.
func (s *ResourceCleanupTestSuite) TestConcurrentResourceCleanup() {
	const numWorkers = 5

	var wg sync.WaitGroup

	workerStopped := make(chan struct{})

	// Start workers
	for i := range numWorkers {
		wg.Add(1)

		go func(workerID int) {
			defer wg.Done()

			filename := s.createTempFile(fmt.Sprintf("worker-%d-content", workerID))

			// Register cleanup for this worker's resources
			s.AddCleanup(func() error {
				return os.Remove(filename)
			})

			// Simulate work
			time.Sleep(workerSimulationDelay)
		}(i)
	}

	// Register cleanup to wait for all workers
	s.AddCleanup(func() error {
		go func() {
			wg.Wait()
			close(workerStopped)
		}()

		select {
		case <-workerStopped:
			return nil
		case <-time.After(workerTimeoutDuration):
			return errors.New("timeout waiting for workers to complete")
		}
	})

	// Wait for workers to complete
	s.True(s.WaitForCondition("all workers completed", func() bool {
		select {
		case <-workerStopped:
			return true
		default:
			return false
		}
	}))
}

// createTempFile creates a temporary file for testing.
func (s *ResourceCleanupTestSuite) createTempFile(content string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	tmpFile, err := os.CreateTemp("", "test-*.txt")
	if err != nil {
		s.T().Errorf("Failed to create temp file: %v", err)

		return ""
	}

	_, err = tmpFile.WriteString(content)
	if err != nil {
		s.T().Errorf("Failed to write to temp file: %v", err)

		return ""
	}

	err = tmpFile.Close()
	if err != nil {
		s.T().Errorf("Failed to close temp file: %v", err)

		return ""
	}

	s.tempFiles = append(s.tempFiles, tmpFile.Name())

	return tmpFile.Name()
}

// Mock implementations for testing

// MockTimeKeeper provides controlled time for testing.
type MockTimeKeeper struct {
	currentTime time.Time
	mu          sync.RWMutex
}

// NewMockTimeKeeper creates a new mock time keeper.
func NewMockTimeKeeper() *MockTimeKeeper {
	return &MockTimeKeeper{
		currentTime: time.Now(),
	}
}

// Now returns the current mock time.
func (m *MockTimeKeeper) Now() time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.currentTime
}

// Advance advances the mock time by the specified duration.
func (m *MockTimeKeeper) Advance(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.currentTime = m.currentTime.Add(d)
}

// MockRateLimiter provides rate limiting for testing.
type MockRateLimiter struct {
	limit    int
	window   time.Duration
	requests []time.Time
	mu       sync.Mutex
}

// NewMockRateLimiter creates a new mock rate limiter.
func NewMockRateLimiter(requestsPerSecond int) *MockRateLimiter {
	return &MockRateLimiter{
		limit:    requestsPerSecond,
		window:   time.Second,
		requests: make([]time.Time, 0),
	}
}

// Allow checks if a request is allowed under the current rate limit.
func (r *MockRateLimiter) Allow() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-r.window)

	// Remove old requests
	validRequests := make([]time.Time, 0)

	for _, req := range r.requests {
		if req.After(cutoff) {
			validRequests = append(validRequests, req)
		}
	}

	r.requests = validRequests

	// Check if we can add this request
	if len(r.requests) < r.limit {
		r.requests = append(r.requests, now)

		return true
	}

	return false
}

// MockConnection provides a mock connection for testing.
type MockConnection struct {
	connected bool
	mu        sync.RWMutex
}

// NewMockConnection creates a new mock connection.
func NewMockConnection() *MockConnection {
	return &MockConnection{connected: true}
}

// IsConnected returns whether the connection is active.
func (c *MockConnection) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.connected
}

// Disconnect simulates a connection failure.
func (c *MockConnection) Disconnect() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.connected = false
}

// Connect simulates reconnection.
func (c *MockConnection) Connect() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.connected = true
}

// Helper functions

// getEnvDuration returns duration from environment variable or default.
func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if duration, err := time.ParseDuration(value); err == nil {
			return duration
		}
	}

	return defaultValue
}

// getEnvInt returns integer from environment variable or default.
func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}

	return defaultValue
}

// Test suite runner functions

// TestPatternsSuite runs the patterns test suite.
func TestPatternsSuite(t *testing.T) {
	suite.Run(t, new(TimingDependentTestSuite))
	suite.Run(t, new(ResourceCleanupTestSuite))
}
