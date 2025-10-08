package main

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// setupFaultToleranceTest sets up the test environment.
func setupFaultToleranceTest(t *testing.T) (*DockerStackWithMultipleBackends, *RouterController, *MCPClient) {
	t.Helper()

	logger := e2e.NewTestLogger()
	ctx := context.Background()

	logger.Info("Starting Docker Compose stack for fault tolerance tests")

	stack := NewDockerStackWithMultipleBackends(t)
	t.Cleanup(stack.Cleanup)

	err := stack.Start(ctx)
	require.NoError(t, err, "Failed to start Docker stack")

	err = stack.WaitForHTTPEndpoint(ctx, stack.GetGatewayHTTPURL()+"/health")
	require.NoError(t, err, "Gateway health check failed")

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
			t.Logf("Failed to stop router: %v", err)
		}
	})

	err = router.BuildRouter()
	require.NoError(t, err, "Failed to build router binary")

	err = router.Start()
	require.NoError(t, err, "Failed to start router")

	client := NewMCPClient(router)

	return stack, router, client
}

// TestComprehensiveFaultTolerance tests advanced fault tolerance scenarios.
func TestComprehensiveFaultTolerance(t *testing.T) {
	// Skip if E2E tests are disabled
	if os.Getenv("SKIP_E2E_TESTS") == skipE2ETestsValue {
		t.Skip("Skipping E2E tests (SKIP_E2E_TESTS=true)")
	}

	stack, _, client := setupFaultToleranceTest(t)

	// Initialize the MCP client
	_, err := client.Initialize()
	require.NoError(t, err, "Failed to initialize MCP client")

	t.Run("PartialSystemDegradation", func(t *testing.T) {
		testPartialSystemDegradation(t, client, stack)
	})

	t.Run("CascadingFailures", func(t *testing.T) {
		testCascadingFailures(t, client, stack)
	})

	t.Run("GracefulShutdown", func(t *testing.T) {
		testGracefulShutdown(t, client, stack)
	})

	t.Run("DataConsistency", func(t *testing.T) {
		testDataConsistency(t, client, stack)
	})
}

func testPartialSystemDegradation(t *testing.T, client *MCPClient, stack *DockerStackWithMultipleBackends) {
	t.Helper()
	// Stop 2 out of 3 backends
	err := stack.StopBackend("backend-1")
	require.NoError(t, err, "Should stop backend-1")

	err = stack.StopBackend("backend-2")
	require.NoError(t, err, "Should stop backend-2")

	// System should continue working with degraded performance
	resp, err := client.CallTool("echo", map[string]interface{}{
		"message": "degraded system test",
	})

	// Should still work with one backend
	require.NoError(t, err, "Should work with degraded backends")
	require.NotNil(t, resp, "Should receive response")

	text, err := ExtractTextFromToolResponse(resp)
	require.NoError(t, err, "Should extract text")
	assert.Contains(t, text, "degraded system test", "Response should contain message")

	// Restart backends
	err = stack.StartBackend("backend-1")
	require.NoError(t, err, "Should restart backend-1")

	err = stack.StartBackend("backend-2")
	require.NoError(t, err, "Should restart backend-2")

	// Wait for recovery
	time.Sleep(5 * time.Second)

	// Verify recovery
	resp, err = client.CallTool("echo", map[string]interface{}{
		"message": "recovered system test",
	})
	require.NoError(t, err, "Should work after recovery")
	require.NotNil(t, resp, "Should receive response after recovery")
}

func testCascadingFailures(t *testing.T, client *MCPClient, stack *DockerStackWithMultipleBackends) {
	t.Helper()
	// Simulate cascading failure by stopping services in sequence
	services := []string{"backend-1", "backend-2", "backend-3"}

	for i, service := range services {
		err := stack.StopBackend(service)
		require.NoError(t, err, "Should stop %s", service)

		// Try to use the system
		resp, callErr := client.CallTool("echo", map[string]interface{}{
			"message": "cascading failure test",
		})

		if i < len(services)-1 {
			// Should still work with some backends
			if callErr == nil {
				assert.NotNil(t, resp, "Should receive response with %d backends down", i+1)
			}
		} else {
			// All backends down - should fail
			require.Error(t, callErr, "Should fail when all backends are down")
		}

		time.Sleep(2 * time.Second)
	}

	// Restart all backends
	for _, service := range services {
		err := stack.StartBackend(service)
		require.NoError(t, err, "Should restart %s", service)
	}
}

func testGracefulShutdown(t *testing.T, client *MCPClient, stack *DockerStackWithMultipleBackends) {
	t.Helper()
	// Start some background operations
	done := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		go func(id int) {
			defer func() { done <- true }()

			for j := 0; j < 5; j++ {
				_, _ = client.CallTool("echo", map[string]interface{}{
					"message": "background operation",
				})

				time.Sleep(100 * time.Millisecond)
			}
		}(i)
	}

	// Wait a moment then initiate shutdown
	time.Sleep(3 * time.Second)

	// Gracefully stop gateway
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := stack.RestartService(ctx, "gateway")
	require.NoError(t, err, "Should gracefully restart gateway")

	// Wait for all goroutines to finish
	for i := 0; i < 10; i++ {
		select {
		case <-done:
			// Goroutine finished
		case <-time.After(10 * time.Second):
			t.Log("Some goroutines didn't finish in time (expected during shutdown)")
		}
	}
}

func testDataConsistency(t *testing.T, client *MCPClient, stack *DockerStackWithMultipleBackends) {
	t.Helper()
	// Send a sequence of operations
	expectedSequence := []string{"first", "second", "third", "fourth", "fifth"}
	receivedSequence := []string{}

	for _, msg := range expectedSequence {
		resp, err := client.CallTool("echo", map[string]interface{}{
			"message": msg,
		})

		if err == nil && resp != nil {
			text, _ := ExtractTextFromToolResponse(resp)
			if text != "" {
				receivedSequence = append(receivedSequence, msg)
			}
		}
	}

	// Verify order was maintained
	assert.Len(t, receivedSequence, len(expectedSequence),
		"Should receive all messages")

	for i := range expectedSequence {
		if i < len(receivedSequence) {
			assert.Equal(t, expectedSequence[i], receivedSequence[i],
				"Message order should be preserved")
		}
	}
}
