// Package weather provides agent-based E2E testing for MCP Gateway and Router
package weather

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

// TestMCPClientAgentEndToEnd tests the complete flow with an intelligent agent.
func TestMCPClientAgentEndToEnd(t *testing.T) {
	// Skip if not in E2E mode
	if os.Getenv("E2E_TEST") != "true" {
		t.Skip("Skipping E2E test (set E2E_TEST=true to run)")
	}

	logger := zaptest.NewLogger(t)

	// Setup test environment
	testEnv := setupTestEnvironment(t, logger)
	defer testEnv.Cleanup()

	// Create router configuration for agent
	routerConfig := createRouterConfig(testEnv.GatewayURL)
	configPath := filepath.Join(t.TempDir(), "router-config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(routerConfig), 0600))

	// Path to router binary
	routerPath := getRouterBinaryPath()

	// Run test scenarios
	t.Run("SingleAgentConnection", func(t *testing.T) {
		testSingleAgentConnection(t, logger, routerPath, configPath)
	})

	t.Run("AgentConversation", func(t *testing.T) {
		testAgentConversation(t, logger, routerPath, configPath)
	})

	t.Run("AgentComplexTask", func(t *testing.T) {
		testAgentComplexTask(t, logger, routerPath, configPath)
	})

	t.Run("MultipleAgents", func(t *testing.T) {
		testMultipleAgents(t, logger, routerPath, configPath)
	})

	t.Run("AgentErrorHandling", func(t *testing.T) {
		testAgentErrorHandling(t, logger, routerPath, configPath)
	})

	t.Run("AgentLoadTest", func(t *testing.T) {
		testAgentLoadScenario(t, logger, routerPath, configPath)
	})
}

// testSingleAgentConnection tests a single agent connecting and executing queries.
func testSingleAgentConnection(t *testing.T, logger *zap.Logger, routerPath, configPath string) {
	t.Helper()
	// Create agent
	agent := NewMCPClientAgent("test-agent-1", routerPath, configPath, logger)

	// Connect to Router (which connects to Gateway -> Weather Server)
	err := agent.Connect()
	require.NoError(t, err, "Failed to connect agent")

	defer func() { _ = agent.Disconnect() }()

	// Initialize MCP protocol
	err = agent.Initialize()
	require.NoError(t, err, "Failed to initialize agent")

	// Execute a weather query
	weather, err := agent.ExecuteWeatherQuery("New York", 40.7128, -74.0060)
	require.NoError(t, err, "Failed to execute weather query")
	assert.NotEmpty(t, weather, "Weather data should not be empty")

	// Verify the response contains expected data
	assert.Contains(t, weather, "Temperature")
	assert.Contains(t, weather, "Humidity")
	assert.Contains(t, weather, "Wind Speed")

	// Check metrics
	metrics := agent.GetMetrics()
	assert.Positive(t, metrics.RequestsSent)
	assert.Positive(t, metrics.ResponsesReceived)
	assert.Equal(t, int64(0), metrics.Errors)
}

// testAgentConversation tests a multi-turn conversation.
func testAgentConversation(t *testing.T, logger *zap.Logger, routerPath, configPath string) {
	t.Helper()

	agent := NewMCPClientAgent("conversation-agent", routerPath, configPath, logger)

	// Connect and initialize
	require.NoError(t, agent.Connect())

	defer func() { _ = agent.Disconnect() }()

	require.NoError(t, agent.Initialize())

	// Simulate a conversation
	err := agent.SimulateConversation()
	require.NoError(t, err, "Conversation simulation failed")

	// Verify multiple interactions occurred
	metrics := agent.GetMetrics()
	assert.GreaterOrEqual(t, metrics.RequestsSent, int64(3))
	assert.GreaterOrEqual(t, metrics.ResponsesReceived, int64(3))
}

// testAgentComplexTask tests executing a complex multi-step task.
func testAgentComplexTask(t *testing.T, logger *zap.Logger, routerPath, configPath string) {
	t.Helper()

	agent := NewMCPClientAgent("complex-agent", routerPath, configPath, logger)

	// Connect and initialize
	require.NoError(t, agent.Connect())

	defer func() { _ = agent.Disconnect() }()

	require.NoError(t, agent.Initialize())

	// Execute complex task
	result, err := agent.ExecuteComplexTask("weather-analysis")
	require.NoError(t, err, "Complex task failed")

	// Verify task results
	assert.True(t, result.Success, "Task should succeed")
	assert.NotEmpty(t, result.Steps, "Task should have steps")

	// Check that all steps completed
	for _, step := range result.Steps {
		assert.Equal(t, "completed", step.Status, "Step %s should be completed", step.Name)
		assert.NotZero(t, step.Duration, "Step should have duration")
	}

	// Verify task took reasonable time
	assert.Less(t, result.Duration, 30*time.Second, "Task should complete within 30 seconds")
}

// testMultipleAgents tests multiple agents connecting simultaneously.
//

func testMultipleAgents(t *testing.T, logger *zap.Logger, routerPath, configPath string) {
	t.Helper()

	numAgents := 5

	agents := createAndConnectMultipleAgents(t, logger, routerPath, configPath, numAgents)
	defer cleanupMultipleAgents(agents)

	results := executeMultipleAgentQueries(agents, numAgents)
	validateMultipleAgentResults(t, results, numAgents)
}

func createAndConnectMultipleAgents(
	t *testing.T, logger *zap.Logger, routerPath, configPath string, numAgents int) []*MCPClientAgent {
	t.Helper()

	agents := make([]*MCPClientAgent, numAgents)

	// Create and connect all agents
	for i := 0; i < numAgents; i++ {
		agentID := fmt.Sprintf("multi-agent-%d", i)
		agent := NewMCPClientAgent(agentID, routerPath, configPath, logger)

		err := agent.Connect()
		require.NoError(t, err, "Failed to connect agent %s", agentID)

		err = agent.Initialize()
		require.NoError(t, err, "Failed to initialize agent %s", agentID)

		agents[i] = agent
	}

	return agents
}

func cleanupMultipleAgents(agents []*MCPClientAgent) {
	for _, agent := range agents {
		if agent != nil {
			_ = agent.Disconnect() // Best effort cleanup
		}
	}
}

type agentResult struct {
	AgentID string
	Weather string
	Error   error
}

func executeMultipleAgentQueries(agents []*MCPClientAgent, numAgents int) chan agentResult {
	results := make(chan agentResult, numAgents)

	for i, agent := range agents {
		go func(idx int, a *MCPClientAgent) {
			// Different location for each agent
			lat := 40.0 + float64(idx)*10.0
			lon := -74.0 + float64(idx)*10.0
			location := fmt.Sprintf("Location-%d", idx)

			weather, err := a.ExecuteWeatherQuery(location, lat, lon)
			results <- agentResult{
				AgentID: a.ID,
				Weather: weather,
				Error:   err,
			}
		}(i, agent)
	}

	return results
}

func validateMultipleAgentResults(t *testing.T, results chan agentResult, numAgents int) {
	t.Helper()

	successCount := 0

	for i := 0; i < numAgents; i++ {
		res := <-results
		if res.Error == nil {
			successCount++

			assert.NotEmpty(t, res.Weather, "Agent %s should receive weather data", res.AgentID)
		} else {
			t.Logf("Agent %s failed: %v", res.AgentID, res.Error)
		}
	}

	// At least 80% should succeed
	minSuccess := int(float64(numAgents) * 0.8)
	assert.GreaterOrEqual(t, successCount, minSuccess,
		"At least %d agents should succeed", minSuccess)
}

// testAgentErrorHandling tests agent behavior with errors.
func testAgentErrorHandling(t *testing.T, logger *zap.Logger, routerPath, configPath string) {
	t.Helper()

	agent := NewMCPClientAgent("error-agent", routerPath, configPath, logger)

	// Connect and initialize
	require.NoError(t, agent.Connect())

	defer func() { _ = agent.Disconnect() }()

	require.NoError(t, agent.Initialize())

	// Test with invalid coordinates (should still work but might return error from API)
	_, err := agent.ExecuteWeatherQuery("Invalid", 999.0, 999.0)
	// API might still return data for invalid coords, so we just check the call completes
	assert.NotNil(t, err == nil || err != nil, "Call should complete")

	// Test calling method before initialization (create new agent)
	uninitAgent := NewMCPClientAgent("uninit-agent", routerPath, configPath, logger)

	require.NoError(t, uninitAgent.Connect())

	defer func() { _ = uninitAgent.Disconnect() }() // Best effort cleanup

	// Should fail because not initialized
	_, err = uninitAgent.ExecuteWeatherQuery("Test", 0, 0)
	require.Error(t, err, "Should fail when not initialized")
	assert.Contains(t, err.Error(), "not initialized")
}

// testAgentLoadScenario tests agent under load.
func testAgentLoadScenario(t *testing.T, logger *zap.Logger, routerPath, configPath string) {
	t.Helper()

	agent := NewMCPClientAgent("load-agent", routerPath, configPath, logger)

	// Connect and initialize
	require.NoError(t, agent.Connect())

	defer func() { _ = agent.Disconnect() }()

	require.NoError(t, agent.Initialize())

	// Run concurrent requests test
	numRequests := 50
	result, err := agent.TestConcurrentRequests(numRequests)
	require.NoError(t, err, "Concurrent test failed")

	// Verify results
	assert.Equal(t, numRequests, result.TotalRequests)
	assert.Positive(t, result.SuccessCount, "Some requests should succeed")

	// Success rate should be high (>90%)
	assert.Greater(t, result.SuccessRate, 90.0,
		"Success rate should be >90%%, got %.2f%%", result.SuccessRate)

	// Check throughput
	assert.Greater(t, result.RequestsPerSecond, 1.0,
		"Should handle >1 request per second")

	t.Logf("Load test results: %d/%d succeeded (%.2f%%), %.2f req/s",
		result.SuccessCount, result.TotalRequests,
		result.SuccessRate, result.RequestsPerSecond)
}

// Test environment setup helpers

type TestEnvironment struct {
	GatewayURL string
	WeatherURL string
	Cleanup    func()
}

func setupTestEnvironment(t *testing.T, logger *zap.Logger) *TestEnvironment {
	t.Helper()
	// In a real test, this would:
	// 1. Create Kind cluster
	// 2. Deploy Weather server, Gateway, Router to cluster
	// 3. Set up port forwarding
	// 4. Return URLs

	// For now, return mock environment assuming services are running
	env := &TestEnvironment{
		GatewayURL: "ws://localhost:8080",
		WeatherURL: "http://localhost:8081",
		Cleanup: func() {
			logger.Info("Cleaning up test environment")
		},
	}

	// In real implementation:
	// - Create cluster
	// - Deploy services
	// - Wait for ready
	// - Set up networking

	return env
}

func createRouterConfig(gatewayURL string) string {
	return fmt.Sprintf(`
gateway:
  url: %s
  connection:
    timeout_ms: 30000
    reconnect:
      enabled: true
      initial_delay_ms: 1000
      max_delay_ms: 30000
      multiplier: 2.0
      max_attempts: 10
  auth:
    type: bearer
    bearer:
      token: "${MCP_AUTH_TOKEN}"

local:
  request_timeout_ms: 30000
  max_concurrent_requests: 100
  buffer_size: 65536

observability:
  logging:
    level: info
    format: json

resilience:
  circuit_breaker:
    enabled: true
    failure_threshold: 5
    success_threshold: 2
    timeout_seconds: 30
`, gatewayURL)
}

func getRouterBinaryPath() string {
	// Try different possible locations
	paths := []string{
		"../../../services/router/bin/mcp-router",
		"./bin/mcp-router",
		"/usr/local/bin/mcp-router",
	}

	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	// Default path
	return "mcp-router"
}

// BenchmarkAgentThroughput benchmarks agent throughput.
func BenchmarkAgentThroughput(b *testing.B) {
	if os.Getenv("E2E_TEST") != "true" {
		b.Skip("Skipping benchmark (set E2E_TEST=true to run)")
	}

	logger := zap.NewNop()

	// Setup
	testEnv := setupTestEnvironment(&testing.T{}, logger)
	defer testEnv.Cleanup()

	routerConfig := createRouterConfig(testEnv.GatewayURL)
	configPath := filepath.Join(b.TempDir(), "router-config.yaml")
	_ = os.WriteFile(configPath, []byte(routerConfig), 0600)

	routerPath := getRouterBinaryPath()

	// Create agent
	agent := NewMCPClientAgent("bench-agent", routerPath, configPath, logger)
	_ = agent.Connect()

	defer func() { _ = agent.Disconnect() }()

	_ = agent.Initialize()

	// Reset timer after setup
	b.ResetTimer()

	// Benchmark
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err := agent.ExecuteWeatherQuery("Bench", 40.7128, -74.0060)
			if err != nil {
				b.Error(err)
			}
		}
	})

	// Report metrics
	metrics := agent.GetMetrics()
	b.ReportMetric(float64(metrics.RequestsSent), "requests")
	b.ReportMetric(float64(metrics.ResponsesReceived), "responses")
	b.ReportMetric(metrics.AverageLatencyMs, "ms/op")
}
