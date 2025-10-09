package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/actual-software/mcp-bridge/test/testutil/e2e"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// TestMonitoringAndMetrics tests observability features.
func TestMonitoringAndMetrics(t *testing.T) {
	// Skip if E2E tests are disabled
	if os.Getenv("SKIP_E2E_TESTS") == skipE2ETestsValue {
		t.Skip("Skipping E2E tests (SKIP_E2E_TESTS=true)")
	}

	logger := e2e.NewTestLogger()
	ctx := context.Background()

	// Set up monitoring test infrastructure
	stack, router, client := setupMonitoringTestInfrastructure(t, logger, ctx)
	defer cleanupMonitoringTestInfrastructure(stack, router)

	// Run monitoring test suite
	runMonitoringTestSuite(t, client, stack)
}

func setupMonitoringTestInfrastructure(
	t *testing.T,
	logger *zap.Logger,
	ctx context.Context,
) (*DockerStackWithMonitoring, *RouterController, *MCPClient) {
	t.Helper()
	// Start Docker services with monitoring enabled
	logger.Info("Starting Docker Compose stack for monitoring tests")

	stack := NewDockerStackWithMonitoring(t)
	err := stack.Start(ctx)
	require.NoError(t, err, "Failed to start Docker stack")

	// Wait for services to be ready
	err = stack.WaitForHTTPEndpoint(ctx, stack.GetGatewayHTTPURL()+"/health")
	require.NoError(t, err, "Gateway health check failed")

	// Create router and client
	router := NewRouterController(t, stack.GetGatewayURL())

	err = router.BuildRouter()
	require.NoError(t, err, "Failed to build router binary")

	err = router.Start()
	require.NoError(t, err, "Failed to start router")

	client := NewMCPClient(router)

	return stack, router, client
}

func cleanupMonitoringTestInfrastructure(stack *DockerStackWithMonitoring, router *RouterController) {
	if err := router.Stop(); err != nil {
		// Log error but don't fail cleanup - this is intentionally ignored
		_ = err
	}

	stack.Cleanup()
}

func runMonitoringTestSuite(t *testing.T, client *MCPClient, stack *DockerStackWithMonitoring) {
	t.Helper()
	t.Run("MetricsCollection", func(t *testing.T) {
		testMetricsCollection(t, client, stack)
	})

	t.Run("PrometheusMetrics", func(t *testing.T) {
		testPrometheusMetrics(t, stack)
	})

	t.Run("LoggingIntegration", func(t *testing.T) {
		testLoggingIntegration(t, client, stack)
	})

	t.Run("HealthChecks", func(t *testing.T) {
		testHealthChecks(t, stack)
	})
}

func testMetricsCollection(t *testing.T, client *MCPClient, stack *DockerStackWithMonitoring) {
	t.Helper()
	// Generate some activity
	for i := 0; i < 10; i++ {
		_, err := client.CallTool("echo", map[string]interface{}{
			"message": fmt.Sprintf("metrics test %d", i),
		})
		require.NoError(t, err, "Tool call should succeed")
	}

	// Check metrics endpoint
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, stack.GetGatewayHTTPURL()+"/metrics", nil)
	require.NoError(t, err, "Should create request")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err, "Should reach metrics endpoint")

	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Logf("Failed to close response body: %v", err)
		}
	}()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "Should read metrics")

	metrics := string(body)

	// Verify key metrics are present
	assert.Contains(t, metrics, "mcp_requests_total", "Should have request counter")
	assert.Contains(t, metrics, "mcp_request_duration_seconds", "Should have latency histogram")
	assert.Contains(t, metrics, "mcp_active_connections", "Should have connection gauge")
}

func testPrometheusMetrics(t *testing.T, stack *DockerStackWithMonitoring) {
	t.Helper()
	// Query Prometheus directly if available
	prometheusURL := "http://localhost:9090"

	req, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		prometheusURL+"/api/v1/query?query=up",
		nil,
	)
	if err != nil {
		t.Skip("Failed to create request")

		return
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Skip("Prometheus not available")

		return
	}

	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Logf("Failed to close response body: %v", err)
		}
	}()

	assert.Equal(t, http.StatusOK, resp.StatusCode, "Prometheus should be healthy")

	// Verify gateway metrics are being scraped
	req, err = http.NewRequestWithContext(context.Background(), http.MethodGet, prometheusURL+"/api/v1/targets", nil)
	if err == nil {
		resp, err = http.DefaultClient.Do(req)
	}

	if err == nil {
		defer func() {
			if err := resp.Body.Close(); err != nil {
				t.Logf("Failed to close response body: %v", err)
			}
		}()

		body, _ := io.ReadAll(resp.Body)
		assert.Contains(t, string(body), "gateway", "Gateway should be a Prometheus target")
	}
}

func testLoggingIntegration(t *testing.T, client *MCPClient, stack *DockerStackWithMonitoring) {
	t.Helper()
	// Generate some activity with unique markers
	uniqueID := fmt.Sprintf("LOG_TEST_%d", time.Now().UnixNano())

	_, err := client.CallTool("echo", map[string]interface{}{
		"message": uniqueID,
	})
	require.NoError(t, err, "Tool call should succeed")

	// Wait a moment for logs to be written
	require.Eventually(t, func() bool {
		logs, _ := stack.GetServiceLogs(context.Background(), "gateway")

		return strings.Contains(logs, uniqueID)
	}, 5*time.Second, 500*time.Millisecond, "Logs should contain unique marker")

	// Get updated logs
	finalLogs, err := stack.GetServiceLogs(context.Background(), "gateway")
	require.NoError(t, err, "Should get gateway logs")

	// Verify log structure
	assert.Contains(t, finalLogs, "level", "Logs should have level field")
	assert.Contains(t, finalLogs, "timestamp", "Logs should have timestamp")
	assert.Contains(t, finalLogs, uniqueID, "Logs should contain our unique marker")
}

func testHealthChecks(t *testing.T, stack *DockerStackWithMonitoring) {
	t.Helper()
	// Test gateway health endpoint
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, stack.GetGatewayHTTPURL()+"/health", nil)
	require.NoError(t, err, "Should create request")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err, "Should reach health endpoint")

	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Logf("Failed to close response body: %v", err)
		}
	}()

	assert.Equal(t, http.StatusOK, resp.StatusCode, "Health check should return OK")

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "Should read health response")

	// Verify health response structure
	health := string(body)
	assert.Contains(t, health, "status", "Health should have status")

	// Test readiness endpoint if available
	req, err = http.NewRequestWithContext(context.Background(), http.MethodGet, stack.GetGatewayHTTPURL()+"/ready", nil)
	if err == nil {
		resp, err = http.DefaultClient.Do(req)
	}

	if err == nil {
		defer func() {
			if err := resp.Body.Close(); err != nil {
				t.Logf("Failed to close response body: %v", err)
			}
		}()

		assert.Equal(t, http.StatusOK, resp.StatusCode, "Readiness check should return OK")
	}

	// Test liveness endpoint if available
	req, err = http.NewRequestWithContext(context.Background(), http.MethodGet, stack.GetGatewayHTTPURL()+"/live", nil)
	if err == nil {
		resp, err = http.DefaultClient.Do(req)
	}

	if err == nil {
		defer func() {
			if err := resp.Body.Close(); err != nil {
				t.Logf("Failed to close response body: %v", err)
			}
		}()

		assert.Equal(t, http.StatusOK, resp.StatusCode, "Liveness check should return OK")
	}
}

// DockerStackWithMonitoring extends DockerStack for monitoring tests.
type DockerStackWithMonitoring struct {
	*DockerStack
}

// NewDockerStackWithMonitoring creates a monitoring-enabled Docker stack.
func NewDockerStackWithMonitoring(t *testing.T) *DockerStackWithMonitoring {
	t.Helper()

	return &DockerStackWithMonitoring{
		DockerStack: NewDockerStack(t),
	}
}
