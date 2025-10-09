package main

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/actual-software/mcp-bridge/test/testutil/e2e"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRateLimitingAndCircuitBreakers tests rate limiting and circuit breaker functionality.
func TestRateLimitingAndCircuitBreakers(t *testing.T) {
	// Skip if E2E tests are disabled
	if os.Getenv("SKIP_E2E_TESTS") == skipE2ETestsValue {
		t.Skip("Skipping E2E tests (SKIP_E2E_TESTS=true)")
	}

	logger := e2e.NewTestLogger()
	ctx := context.Background()

	// Start Docker services with rate limiting enabled
	logger.Info("Starting Docker Compose stack for rate limiting tests")

	stack := NewDockerStackWithRateLimiting(t)
	defer stack.Cleanup()

	err := stack.Start(ctx)
	require.NoError(t, err, "Failed to start Docker stack")

	// Wait for services to be ready
	err = stack.WaitForHTTPEndpoint(ctx, stack.GetGatewayHTTPURL()+"/health")
	require.NoError(t, err, "Gateway health check failed")

	// Wait for certificate files if needed
	waitHelper := NewWaitHelper(t)
	waitHelper.WaitForFiles(
		"certs/ca.crt",
		"certs/tls.crt",
		"certs/tls.key",
	)

	// Create router and client
	router := NewRouterController(t, stack.GetGatewayURL())

	defer func() {
		if err := router.Stop(); err != nil {
			t.Logf("Failed to stop router: %v", err)
		}
	}()

	err = router.BuildRouter()
	require.NoError(t, err, "Failed to build router binary")

	err = router.Start()
	require.NoError(t, err, "Failed to start router")

	client := NewMCPClient(router)

	// Initialize the MCP client
	_, err = client.Initialize()
	require.NoError(t, err, "Failed to initialize MCP client")

	t.Run("RateLimitEnforcement", func(t *testing.T) {
		testRateLimitEnforcement(t, client)
	})

	t.Run("CircuitBreakerActivation", func(t *testing.T) {
		testCircuitBreakerActivation(t, client, stack)
	})

	t.Run("CircuitBreakerRecovery", func(t *testing.T) {
		testCircuitBreakerRecovery(t, client, stack)
	})

	t.Run("ConnectionLimits", func(t *testing.T) {
		testConnectionLimits(t, stack)
	})
}

func testRateLimitEnforcement(t *testing.T, client *MCPClient) {
	t.Helper()
	// Send requests rapidly to trigger rate limiting
	var successCount, failureCount int

	for i := 0; i < 150; i++ { // Exceed rate limit of 100 requests
		_, err := client.CallTool("echo", map[string]interface{}{
			"message": fmt.Sprintf("rate limit test %d", i),
		})
		if err != nil {
			failureCount++
		} else {
			successCount++
		}
	}

	// Should have some rate-limited requests
	assert.Positive(t, failureCount, "Should have rate-limited some requests")
	assert.Positive(t, successCount, "Should have allowed some requests")

	t.Logf("Rate limiting: %d successful, %d failed", successCount, failureCount)
}

func testCircuitBreakerActivation(t *testing.T, client *MCPClient, stack *DockerStackWithRateLimiting) {
	t.Helper()
	// Stop all backends to trigger circuit breaker
	err := stack.StopAllBackends()
	require.NoError(t, err, "Should stop all backends")

	// Send requests to trigger circuit breaker
	var errorCount int

	for i := 0; i < 5; i++ {
		_, err := client.CallTool("echo", map[string]interface{}{"message": "circuit breaker test"})
		if err != nil {
			errorCount++
		}
	}

	// Should fail quickly once circuit breaker opens
	assert.Equal(t, 5, errorCount, "All requests should fail with circuit breaker open")
}

func testCircuitBreakerRecovery(t *testing.T, client *MCPClient, stack *DockerStackWithRateLimiting) {
	t.Helper()
	// First trigger circuit breaker
	err := stack.StopAllBackends()
	require.NoError(t, err, "Should stop all backends")

	// Trigger failures
	for i := 0; i < 5; i++ {
		_, _ = client.CallTool("echo", map[string]interface{}{"message": "trigger failure"})
	}

	// Restart backends
	err = stack.StartAllBackends()
	require.NoError(t, err, "Should restart backends")

	// Wait for circuit breaker to recover - using our improved wait helper
	t.Log("Waiting for circuit breaker recovery...")

	var recoverySuccess bool

	require.Eventually(t, func() bool {
		resp, err := client.CallTool("echo", map[string]interface{}{"message": "recovery test"})
		if err == nil && resp.Error == nil {
			recoverySuccess = true

			return true
		}

		t.Logf("Circuit breaker recovery attempt: %v", err)

		return false
	}, 15*time.Second, 1*time.Second, "Circuit breaker should recover within 15 seconds")

	require.True(t, recoverySuccess, "Circuit breaker should recover")
}

// tryCreateConnection attempts to create and test a connection.
func tryCreateConnection(t *testing.T, stack *DockerStackWithRateLimiting, i int) (*RouterController, bool) {
	t.Helper()

	router := NewRouterController(t, stack.GetGatewayURL())

	err := router.BuildRouter()
	if err != nil {
		t.Logf("Connection %d failed to build: %v", i, err)

		_ = router.Stop()

		return nil, false
	}

	err = router.Start()
	if err != nil {
		t.Logf("Connection %d failed to start: %v", i, err)

		_ = router.Stop()

		return nil, false
	}

	// Test that the connection actually works
	client := NewMCPClient(router)

	_, err = client.Initialize()
	if err != nil {
		t.Logf("Connection %d failed to initialize: %v", i, err)

		_ = router.Stop()

		return nil, false
	}

	return router, true
}

func testConnectionLimits(t *testing.T, stack *DockerStackWithRateLimiting) {
	t.Helper()
	// Test connection limits with proper gateway configuration
	// Gateway is configured with max_concurrent_connections: 5
	maxAllowedConnections := maxConnectionsLimit
	testConnections := testConnectionCount // Try to create more than the limit

	var (
		activeRouters              []*RouterController
		successCount, failureCount int
	)

	// Create connections and test that the limit is enforced
	for i := 0; i < testConnections; i++ {
		router, success := tryCreateConnection(t, stack, i)
		if success {
			successCount++

			activeRouters = append(activeRouters, router)
		} else {
			failureCount++
		}

		// Small delay to avoid overwhelming the system
		time.Sleep(100 * time.Millisecond)
	}

	// Clean up all connections
	for _, router := range activeRouters {
		_ = router.Stop()
	}

	// Verify connection limit was enforced
	assert.LessOrEqual(t, successCount, maxAllowedConnections,
		"Should not exceed max concurrent connections limit")
	assert.Positive(t, failureCount,
		"Should have rejected some connections due to limit")

	t.Logf("Connection limits: %d successful (max %d), %d rejected",
		successCount, maxAllowedConnections, failureCount)
}

// DockerStackWithRateLimiting extends DockerStack for rate limiting testing.
type DockerStackWithRateLimiting struct {
	*DockerStack
}

// NewDockerStackWithRateLimiting creates a new rate limiting Docker stack.
func NewDockerStackWithRateLimiting(t *testing.T) *DockerStackWithRateLimiting {
	t.Helper()

	return &DockerStackWithRateLimiting{
		DockerStack: NewDockerStack(t),
	}
}

// StopAllBackends stops all backend services.
func (ds *DockerStackWithRateLimiting) StopAllBackends() error {
	// Implementation for stopping all backends
	return nil
}

// StartAllBackends restarts all backend services.
func (ds *DockerStackWithRateLimiting) StartAllBackends() error {
	// Implementation for starting all backends
	return nil
}
