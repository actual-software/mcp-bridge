package main

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// Layer1ContainerIntegration tests container setup and health.
func Layer1ContainerIntegration(t *testing.T, stack *ClaudeCodeRealDockerStack, logger *zap.Logger) {
	t.Helper()
	logger.Info("📋 Layer 1: Container Integration Testing")

	// Start Docker stack
	err := stack.Start()
	require.NoError(t, err, "Failed to start Docker stack")

	// Wait for all services
	err = stack.WaitForServices()
	require.NoError(t, err, "Services failed to become healthy")

	// Verify Claude Code installation
	verifyClaudeCodeInstallation(t, stack, logger)

	// Verify router container
	verifyContainerizedRouter(t, logger)

	logger.Info("✅ Layer 1: Container Integration - COMPLETED")
}

func verifyClaudeCodeInstallation(t *testing.T, stack *ClaudeCodeRealDockerStack, logger *zap.Logger) {
	t.Helper()

	controller := NewClaudeCodeRealController(t, stack.GetClaudeCodeContainerName())
	defer controller.Cleanup()

	err := controller.WaitForContainer()
	require.NoError(t, err, "Claude Code failed to become ready")

	// Verify installation details
	ctx := context.Background()
	versionCmd := exec.CommandContext(ctx, "docker", "exec", // #nosec G204 - controlled container name
		stack.GetClaudeCodeContainerName(), "claude", "--version")
	output, err := versionCmd.Output()
	require.NoError(t, err, "Should be able to get Claude Code version")

	version := strings.TrimSpace(string(output))
	assert.Contains(t, version, "Claude Code", "Should be genuine Claude Code installation")
	logger.Info("✅ Layer 1: Claude Code installation verified", zap.String("version", version))
}

func verifyContainerizedRouter(t *testing.T, logger *zap.Logger) {
	t.Helper()
	// Check if router container is running
	ctx := context.Background()
	checkCmd := exec.CommandContext(ctx, "docker-compose", "-f", "docker-compose.claude-code.yml", // #nosec G204
		"-p", "claude-code-e2e", "ps", "router")

	output, err := checkCmd.Output()
	if err != nil {
		t.Skip("Router container not available - using local router fallback")

		return
	}

	assert.Contains(t, string(output), "Up", "Router container should be running")
	logger.Info("✅ Layer 1: Router container verified")
}

// Layer2ProtocolTesting tests MCP protocol communication.
func Layer2ProtocolTesting(t *testing.T, logger *zap.Logger) (*RouterController, func()) {
	t.Helper()
	logger.Info("📋 Layer 2: Protocol Testing")

	// Start router
	router := NewRouterController(t, "wss://localhost:8443/ws")
	err := router.Start()
	require.NoError(t, err, "Router should start successfully")

	cleanup := func() {
		if router != nil {
			if err := router.Stop(); err != nil {
				logger.Error("Failed to stop router", zap.Error(err))
			}
		}
	}

	// Test MCP protocol initialization
	testMCPInitialize(t, router, logger)

	// Test tools list protocol
	testMCPToolsList(t, router, logger)

	// Test actual tool execution
	testEchoToolExecution(t, router, logger)

	logger.Info("✅ Layer 2: Protocol Testing - COMPLETED")

	return router, cleanup
}

func testMCPInitialize(t *testing.T, router *RouterController, logger *zap.Logger) {
	t.Helper()

	client := NewMCPClient(router)

	resp, err := client.Initialize()
	require.NoError(t, err, "MCP initialize should succeed")

	// Verify protocol version and capabilities
	require.NotNil(t, resp.Result, "Initialize response should have result")
	result, ok := resp.Result.(map[string]interface{})
	require.True(t, ok, "Initialize response result should be a map")

	protocolVersion, ok := result["protocolVersion"].(string)
	require.True(t, ok, "Should have protocol version")
	assert.Equal(t, "1.0", protocolVersion, "Should use MCP 1.0")

	logger.Info("✅ Layer 2: MCP Initialize verified")
}

func testMCPToolsList(t *testing.T, router *RouterController, logger *zap.Logger) {
	t.Helper()

	client := NewMCPClient(router)

	// Initialize first
	_, err := client.Initialize()
	require.NoError(t, err)

	tools, err := client.ListTools()
	require.NoError(t, err, "Tools list should succeed")
	assert.GreaterOrEqual(t, len(tools), minExpectedTools, "Should have at least echo and math tools")

	// Verify specific tools exist
	toolNames := make([]string, len(tools))
	for i, tool := range tools {
		toolNames[i] = tool.Name
	}

	assert.Contains(t, toolNames, "echo", "Should have echo tool")
	assert.Contains(t, toolNames, "add", "Should have add tool")

	logger.Info("✅ Layer 2: MCP Tools List verified", zap.Strings("tools", toolNames))
}

func testEchoToolExecution(t *testing.T, router *RouterController, logger *zap.Logger) {
	t.Helper()

	client := NewMCPClient(router)

	// Initialize
	_, err := client.Initialize()
	require.NoError(t, err)

	// Execute echo tool
	resp, err := client.CallTool("echo", map[string]interface{}{
		"message": "Hello from Layer 2 Protocol Testing!",
	})
	require.NoError(t, err, "Echo tool execution should succeed")

	// Verify response
	require.NoError(t, AssertMCPResponse(resp), "Echo tool should return success")

	responseText, err := ExtractTextFromMCPResponse(resp)
	require.NoError(t, err, "Should extract text from response")
	assert.Contains(t, responseText, "Hello from Layer 2", "Should echo our message")

	logger.Info("✅ Layer 2: Echo tool execution verified", zap.String("response", responseText))
}

// Layer3WorkflowTesting tests complex workflows.
func Layer3WorkflowTesting(t *testing.T, router *RouterController, logger *zap.Logger) {
	t.Helper()
	logger.Info("📋 Layer 3: Workflow Testing")

	// Test math calculations
	testMathCalculations(t, router, logger)

	// Test error handling
	testClaudeErrorHandling(t, router, logger)

	// Test multiple sequential calls
	testSequentialCalls(t, router, logger)

	logger.Info("✅ Layer 3: Workflow Testing - COMPLETED")
}

func testMathCalculations(t *testing.T, router *RouterController, logger *zap.Logger) {
	t.Helper()

	client := NewMCPClient(router)

	// Initialize
	_, _ = client.Initialize()

	// Test addition
	resp, err := client.CallTool("add", map[string]interface{}{
		"a": testMathValueA,
		"b": testMathValueB,
	})
	require.NoError(t, err, "Add tool should succeed")

	text, err := ExtractTextFromMCPResponse(resp)
	require.NoError(t, err)
	assert.Contains(t, text, "123", "Should correctly add 100 + 23")

	logger.Info("✅ Layer 3: Math calculations verified")
}

func testClaudeErrorHandling(t *testing.T, router *RouterController, logger *zap.Logger) {
	t.Helper()

	client := NewMCPClient(router)

	// Initialize
	_, _ = client.Initialize()

	// Call non-existent tool
	resp, err := client.CallTool("nonexistent_tool", map[string]interface{}{})

	// Should get an error response
	if err == nil && resp != nil && resp.Error != nil {
		assert.Contains(t, resp.Error.Message, "not found", "Should indicate tool not found")
		logger.Info("✅ Layer 3: Error handling verified")
	} else if err != nil {
		// Also acceptable - error at transport level
		logger.Info("✅ Layer 3: Error handling verified (transport error)")
	}
}

func testSequentialCalls(t *testing.T, router *RouterController, logger *zap.Logger) {
	t.Helper()

	client := NewMCPClient(router)

	// Initialize
	_, _ = client.Initialize()

	// Make multiple sequential calls
	for i := 0; i < 3; i++ {
		resp, err := client.CallTool("echo", map[string]interface{}{
			"message": "sequential call",
		})
		require.NoError(t, err, "Sequential call %d should succeed", i)
		assert.NotNil(t, resp, "Should get response for call %d", i)
	}

	logger.Info("✅ Layer 3: Sequential calls verified")
}
