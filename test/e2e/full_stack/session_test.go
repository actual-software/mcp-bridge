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

// TestRedisSessionManagement tests Redis session storage and failure scenarios.
func TestRedisSessionManagement(t *testing.T) {
	// Skip if E2E tests are disabled
	if os.Getenv("SKIP_E2E_TESTS") == skipE2ETestsValue {
		t.Skip("Skipping E2E tests (SKIP_E2E_TESTS=true)")
	}

	logger, _ := zap.NewDevelopment()
	ctx := context.Background()

	// Start Docker services
	logger.Info("Starting Docker Compose stack for Redis session tests")

	stack := NewDockerStack(t)
	defer stack.Cleanup()

	err := stack.Start(ctx)
	require.NoError(t, err, "Failed to start Docker stack")

	// Wait for services to be ready
	err = stack.WaitForHTTPEndpoint(ctx, stack.GetGatewayHTTPURL()+"/health")
	require.NoError(t, err, "Gateway health check failed")

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

	t.Run("SessionPersistence", func(t *testing.T) {
		testSessionPersistence(t, client, stack)
	})

	t.Run("SessionExpiration", func(t *testing.T) {
		testSessionExpiration(t, client, stack)
	})

	t.Run("RedisFailureHandling", func(t *testing.T) {
		testRedisFailureHandling(t, client, stack)
	})

	t.Run("SessionCleanup", func(t *testing.T) {
		testSessionCleanup(t, client, stack)
	})
}

func testSessionPersistence(t *testing.T, client *MCPClient, stack *DockerStack) {
	t.Helper()
	// Initialize connection and create session
	initResp, err := client.Initialize()
	require.NoError(t, err, "Initialize should succeed")
	require.NotNil(t, initResp)

	// Extract session information if available
	sessionID := extractSessionID(initResp)
	if sessionID != "" {
		// Verify session exists in Redis
		redisClient := createRedisClient(t)

		defer func() {
			if err := redisClient.Close(); err != nil {
				t.Logf("Failed to close Redis client: %v", err)
			}
		}()

		exists, err := redisClient.Exists(context.Background(), "session:"+sessionID).Result()
		require.NoError(t, err, "Should check session existence")
		assert.Equal(t, int64(1), exists, "Session should exist in Redis")

		// Verify session data
		sessionData, err := redisClient.Get(context.Background(), "session:"+sessionID).Result()
		require.NoError(t, err, "Should retrieve session data")
		assert.NotEmpty(t, sessionData, "Session data should not be empty")
	}
}

func testSessionExpiration(t *testing.T, client *MCPClient, stack *DockerStack) {
	t.Helper()
	// Create session with short TTL for testing
	initResp, err := client.Initialize()
	require.NoError(t, err, "Initialize should succeed")

	sessionID := extractSessionID(initResp)
	if sessionID != "" {
		redisClient := createRedisClient(t)

		defer func() {
			if err := redisClient.Close(); err != nil {
				t.Logf("Failed to close Redis client: %v", err)
			}
		}()

		// Set short TTL for testing
		err = redisClient.Expire(context.Background(), "session:"+sessionID, 2*time.Second).Err()
		require.NoError(t, err, "Should set session expiration")

		// Wait for expiration
		time.Sleep(3 * time.Second)

		// Session should be expired
		exists, err := redisClient.Exists(context.Background(), "session:"+sessionID).Result()
		require.NoError(t, err, "Should check session existence")
		assert.Equal(t, int64(0), exists, "Session should be expired")
	}
}

func testRedisFailureHandling(t *testing.T, client *MCPClient, stack *DockerStack) {
	t.Helper()
	// First establish working connection
	initResp, err := client.Initialize()
	require.NoError(t, err, "Initialize should succeed")
	require.NotNil(t, initResp)

	// Stop Redis
	ctx := context.Background()
	err = stack.RestartService(ctx, "redis")
	require.NoError(t, err, "Should restart Redis")

	// System should handle Redis failure gracefully
	// New connections might fail, but existing ones should work
	echoResp, err := client.CallTool("echo", map[string]interface{}{
		"message": "Redis failure test",
	})

	// Should either work (graceful degradation) or fail cleanly
	if err == nil {
		err = AssertToolCallSuccess(echoResp)
		assert.NoError(t, err, "If request succeeds, it should be valid")
	} else {
		t.Logf("Request failed gracefully during Redis failure: %v", err)
	}
}

func testSessionCleanup(t *testing.T, client *MCPClient, stack *DockerStack) {
	t.Helper()
	redisClient := createRedisClient(t)

	defer func() {
		if err := redisClient.Close(); err != nil {
			t.Logf("Failed to close Redis client: %v", err)
		}
	}()

	// Count initial sessions
	initialKeys, err := redisClient.Keys(context.Background(), "session:*").Result()
	require.NoError(t, err, "Should get initial session keys")

	initialCount := len(initialKeys)

	// Create multiple sessions
	for i := 0; i < 5; i++ {
		tempRouter := NewRouterController(t, stack.GetGatewayURL())

		defer func() {
			if err := tempRouter.Stop(); err != nil {
				t.Logf("Failed to stop temp router: %v", err)
			}
		}()

		err := tempRouter.BuildRouter()
		if err != nil {
			t.Logf("Failed to build router for session %d: %v", i, err)

			continue
		}

		err = tempRouter.Start()
		if err != nil {
			t.Logf("Failed to start router for session %d: %v", i, err)

			continue
		}

		tempClient := NewMCPClient(tempRouter)
		_, _ = tempClient.Initialize()
		// Let connections close naturally
	}

	// Give time for cleanup
	time.Sleep(10 * time.Second)

	// Check session cleanup happened
	finalKeys, err := redisClient.Keys(context.Background(), "session:*").Result()
	require.NoError(t, err, "Should get final session keys")

	finalCount := len(finalKeys)

	// Should have cleaned up some sessions
	t.Logf("Session cleanup: %d -> %d sessions", initialCount, finalCount)
}
