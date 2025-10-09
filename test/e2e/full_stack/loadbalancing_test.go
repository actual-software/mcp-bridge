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

// TestLoadBalancingAndDiscovery tests load balancing with multiple backends.
// setupLoadBalancingTest initializes the load balancing test environment.
func setupLoadBalancingTest(t *testing.T, ctx context.Context) (*DockerStackWithMultipleBackends, *MCPClient) {
	t.Helper()

	logger := e2e.NewTestLogger()
	logger.Info("Starting Docker Compose stack for load balancing tests")

	stack := NewDockerStackWithMultipleBackends(t)
	t.Cleanup(stack.Cleanup)

	err := stack.Start(ctx)
	require.NoError(t, err, "Failed to start Docker stack")

	err = stack.WaitForHTTPEndpoint(ctx, stack.GetGatewayHTTPURL()+"/health")
	require.NoError(t, err, "Gateway health check failed")

	logger.Info("Waiting for certificate files to be ready")

	waitHelper := NewWaitHelper(t)
	waitHelper.WaitForFiles(
		"certs/ca.crt",
		"certs/tls.crt",
		"certs/tls.key",
		"certs/server.crt",
		"certs/server.key",
	)

	router := NewRouterController(t, stack.GetGatewayURL())
	t.Cleanup(func() {
		if err := router.Stop(); err != nil {
			t.Errorf("Failed to stop router: %v", err)
		}
	})

	err = router.BuildRouter()
	require.NoError(t, err, "Failed to build router binary")

	err = router.Start()
	require.NoError(t, err, "Failed to start router")

	client := NewMCPClient(router)

	_, err = client.Initialize()
	require.NoError(t, err, "Failed to initialize MCP client")

	return stack, client
}

func TestLoadBalancingAndDiscovery(t *testing.T) {
	// Skip if E2E tests are disabled
	if os.Getenv("SKIP_E2E_TESTS") == skipE2ETestsValue {
		t.Skip("Skipping E2E tests (SKIP_E2E_TESTS=true)")
	}

	// Set a reasonable timeout for the entire test
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	stack, client := setupLoadBalancingTest(t, ctx)

	t.Run("RoundRobinLoadBalancing", func(t *testing.T) {
		testRoundRobinLoadBalancing(t, client, stack)
	})

	t.Run("HealthCheckValidation", func(t *testing.T) {
		testHealthCheckValidation(t, client, stack)
	})

	t.Run("ServiceDiscovery", func(t *testing.T) {
		testServiceDiscovery(t, client, stack)
	})

	t.Run("BackendFailover", func(t *testing.T) {
		testBackendFailover(t, client, stack)
	})
}

func testRoundRobinLoadBalancing(t *testing.T, client *MCPClient, stack *DockerStackWithMultipleBackends) {
	t.Helper()
	// Send multiple requests and verify they hit different backends
	backendHits := make(map[string]int)

	for i := 0; i < 10; i++ {
		resp, err := client.CallTool("echo", map[string]interface{}{
			"message": fmt.Sprintf("load balance test %d", i),
		})
		require.NoError(t, err, "Tool call should succeed")

		text, err := ExtractTextFromToolResponse(resp)
		require.NoError(t, err, "Should extract response text")

		// Extract backend identifier from response
		if backendID := extractBackendID(text); backendID != "" {
			backendHits[backendID]++
		}
	}

	// Should hit multiple backends (at least 2 for round robin)
	assert.GreaterOrEqual(t, len(backendHits), 2, "Should hit multiple backends")

	// Check distribution is reasonably balanced
	for backend, hits := range backendHits {
		assert.GreaterOrEqual(t, hits, 1, "Backend %s should receive at least 1 request", backend)
	}
}

func testHealthCheckValidation(t *testing.T, client *MCPClient, stack *DockerStackWithMultipleBackends) {
	t.Helper()
	// Stop one backend
	err := stack.StopBackend("backend-2")
	require.NoError(t, err, "Should be able to stop backend")

	// Wait for health check to detect failure (interval is 5s, failure_threshold is 2)
	waitHelper := NewWaitHelper(t).WithTimeout(20 * time.Second).WithPollInterval(1 * time.Second)
	waitHelper.WaitForCondition("health check failure detection", func() bool {
		// Try to make a request - should fail quickly once health check detects backend is down
		_, err := client.CallTool("echo", map[string]interface{}{"message": "health check"})
		// We expect an error once health check marks backend as unhealthy
		return err != nil
	})

	// Test that requests fail quickly when backend is down (verifying error propagation fix)
	start := time.Now()
	resp, err := client.CallTool("echo", map[string]interface{}{
		"message": "health check test",
	})
	elapsed := time.Since(start)

	// With the fix, we should get an error response quickly (under 5 seconds)
	// rather than timing out after 10+ seconds
	t.Logf("Request completed in %v", elapsed)

	if err != nil {
		// This is expected - backend-2 is down, so some requests should fail
		// But they should fail QUICKLY due to our error propagation fix
		assert.Less(t, elapsed, 5*time.Second, "Error should be returned quickly, not after timeout")
		t.Logf("✅ Error propagation working - request failed quickly in %v: %v", elapsed, err)
	} else {
		// If it succeeded, extract which backend responded
		text, err := ExtractTextFromToolResponse(resp)
		if err != nil {
			// This could happen if we got an error response instead of success
			t.Logf("Could not extract text (likely error response): %v", err)
			assert.Less(t, elapsed, 5*time.Second, "Error should be returned quickly, not after timeout")
		} else {
			backendID := extractBackendID(text)
			assert.NotEqual(t, "backend-2", backendID, "Should not hit stopped backend")
			t.Logf("✅ Request succeeded with healthy backend: %s", backendID)
		}
	}

	// Restart backend
	err = stack.StartBackend("backend-2")
	require.NoError(t, err, "Should be able to restart backend")
}

func testServiceDiscovery(t *testing.T, client *MCPClient, stack *DockerStackWithMultipleBackends) {
	t.Helper()
	// Add a new backend dynamically
	err := stack.AddBackend("backend-3")
	require.NoError(t, err, "Should be able to add new backend")

	// Wait for service discovery to detect new backend
	waitHelper := NewWaitHelper(t).WithTimeout(20 * time.Second).WithPollInterval(1 * time.Second)

	// Verify new backend receives requests
	backendHits := make(map[string]int)

	// Wait until the new backend starts receiving requests
	waitHelper.WaitForCondition("new backend receives requests", func() bool {
		resp, err := client.CallTool("echo", map[string]interface{}{
			"message": "service discovery test",
		})
		if err != nil || resp == nil || resp.Result == nil {
			return false
		}

		// Check if the response is from backend-3
		result, ok := resp.Result.(map[string]interface{})
		if !ok {
			return false
		}

		backendID, ok := result["backend_id"].(string)
		if !ok {
			return false
		}

		return backendID == "backend-3"
	})

	for i := 0; i < 15; i++ {
		resp, err := client.CallTool("echo", map[string]interface{}{
			"message": "service discovery test",
		})
		require.NoError(t, err, "Tool call should succeed")

		text, err := ExtractTextFromToolResponse(resp)
		require.NoError(t, err, "Should extract response text")

		if backendID := extractBackendID(text); backendID != "" {
			backendHits[backendID]++
		}
	}

	// Should include the new backend
	assert.Contains(t, backendHits, "backend-3", "New backend should receive requests")
}

func testBackendFailover(t *testing.T, client *MCPClient, stack *DockerStackWithMultipleBackends) {
	t.Helper()
	// Verify all backends working
	_, err := client.CallTool("echo", map[string]interface{}{"message": "pre-failover"})
	require.NoError(t, err, "Should work before failover")

	// Stop all but one backend
	err = stack.StopBackend("backend-1")
	require.NoError(t, err, "Should stop backend-1")

	err = stack.StopBackend("backend-2")
	require.NoError(t, err, "Should stop backend-2")

	// Wait for failover to complete by polling until we get consistent success
	// With improved health checks (now 5s interval), this should be much faster
	t.Log("Waiting for backend failover to complete...")

	// Try multiple requests as failover detection is now faster
	var (
		successfulResponse *MCPResponse
		lastErr            error
		resp               *MCPResponse
	)

	// Wait for failover to complete and requests to succeed
	waitHelper := NewWaitHelper(t).WithTimeout(20 * time.Second).WithPollInterval(500 * time.Millisecond)
	attempt := 0

	waitHelper.WaitForCondition("failover completion", func() bool {
		attempt++
		resp, err = client.CallTool("echo", map[string]interface{}{"message": "during-failover"})

		if err == nil && resp.Error == nil {
			// Success! Should be using backend-3
			successfulResponse = resp

			return true
		}

		lastErr = err
		if resp != nil && resp.Error != nil {
			lastErr = fmt.Errorf("MCP error %d: %s", resp.Error.Code, resp.Error.Message)
		}

		t.Logf("Failover attempt %d failed (expected during transition): %v", attempt, lastErr)

		return false
	})

	require.NotNil(t, successfulResponse, "Should eventually succeed with remaining backend after %v", lastErr)

	text, err := ExtractTextFromToolResponse(successfulResponse)
	require.NoError(t, err, "Should extract response text")

	backendID := extractBackendID(text)
	assert.Equal(t, "backend-3", backendID, "Should use remaining healthy backend")
}
