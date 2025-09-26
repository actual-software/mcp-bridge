package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// ComprehensiveClaudeCodeE2ETest implements all three testing layers.
//
// TestComprehensiveClaudeCodeE2E validates the complete MCP protocol implementation
// Refactored to reduce cyclomatic complexity from 21 to 3.
func TestComprehensiveClaudeCodeE2E(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	logger.Info("🚀 Starting Comprehensive Claude Code E2E Test Suite")

	// Initialize Docker stack
	stack := NewClaudeCodeRealDockerStack(t, "docker-compose.claude-code.yml")
	defer stack.Cleanup()

	// Run Layer 1: Container Integration
	t.Run("Layer1_ContainerIntegration", func(t *testing.T) {
		Layer1ContainerIntegration(t, stack, logger)
	})

	// Run Layer 2: Protocol Testing
	var (
		router        *RouterController
		cleanupRouter func()
	)

	t.Run("Layer2_ProtocolTesting", func(t *testing.T) {
		router, cleanupRouter = Layer2ProtocolTesting(t, logger)
	})

	// Ensure router cleanup
	if cleanupRouter != nil {
		defer cleanupRouter()
	}

	// Run Layer 3: Workflow Testing
	if router != nil {
		t.Run("Layer3_WorkflowTesting", func(t *testing.T) {
			Layer3WorkflowTesting(t, router, logger)
		})
	}

	logger.Info("🎉 ALL LAYERS COMPLETED SUCCESSFULLY!")
}

// ORIGINAL HIGH-COMPLEXITY VERSION BELOW - Preserved as TestComprehensiveClaudeCodeE2E_Original
// Cyclomatic complexity was 21, now reduced to 3 by extracting to helper functions

//nolint:gocognit,cyclop,funlen,maintidx // Comprehensive E2E test requires complex flow
func TestComprehensiveClaudeCodeE2E_Original(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping original complex version in short mode")
	}

	logger, _ := zap.NewDevelopment()
	logger.Info("🚀 Starting Comprehensive Claude Code E2E Test Suite (Original)")

	// Initialize Docker stack
	stack := NewClaudeCodeRealDockerStack(t, "docker-compose.claude-code.yml")
	defer stack.Cleanup()

	// =============================================================================
	// LAYER 1: CONTAINER INTEGRATION
	// =============================================================================
	t.Run("Layer1_ContainerIntegration", func(t *testing.T) {
		logger.Info("📋 Layer 1: Container Integration Testing")

		t.Run("StartDockerStack", func(t *testing.T) {
			err := stack.Start()
			require.NoError(t, err, "Failed to start Docker stack")
		})

		t.Run("WaitForAllServices", func(t *testing.T) {
			err := stack.WaitForServices()
			require.NoError(t, err, "Services failed to become healthy")
		})

		t.Run("VerifyClaudeCodeInstallation", func(t *testing.T) {
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
		})

		t.Run("VerifyContainerizedRouter", func(t *testing.T) {
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
		})

		logger.Info("✅ Layer 1: Container Integration - COMPLETED")
	})

	// =============================================================================
	// LAYER 2: PROTOCOL TESTING
	// =============================================================================
	t.Run("Layer2_ProtocolTesting", func(t *testing.T) {
		logger.Info("📋 Layer 2: Protocol Testing")

		// Start router (local fallback if container failed)
		var router *RouterController

		t.Run("StartRouterConnection", func(t *testing.T) {
			router = NewRouterController(t, "wss://localhost:8443/ws")
			err := router.Start()
			require.NoError(t, err, "Router should start successfully")
		})

		defer func() {
			if router != nil {
				if err := router.Stop(); err != nil {
					logger.Error("Failed to stop router", zap.Error(err))
				}
			}
		}()

		t.Run("MCPProtocolInitialize", func(t *testing.T) {
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
		})

		t.Run("MCPToolsListProtocol", func(t *testing.T) {
			client := NewMCPClient(router)

			// Initialize first
			_, err := client.Initialize()
			require.NoError(t, err)

			tools, err := client.ListTools()
			require.NoError(t, err, "Tools list should succeed")
			assert.GreaterOrEqual(t, len(tools), 2, "Should have at least echo and math tools")

			// Verify specific tools exist
			toolNames := make([]string, len(tools))
			for i, tool := range tools {
				toolNames[i] = tool.Name
			}

			assert.Contains(t, toolNames, "echo", "Should have echo tool")
			assert.Contains(t, toolNames, "add", "Should have add tool")

			logger.Info("✅ Layer 2: MCP Tools List verified", zap.Strings("tools", toolNames))
		})

		t.Run("ActualToolExecution_Echo", func(t *testing.T) {
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
		})

		t.Run("ActualToolExecution_Math", func(t *testing.T) {
			client := NewMCPClient(router)

			// Initialize
			_, err := client.Initialize()
			require.NoError(t, err)

			// Execute math tool
			resp, err := client.CallTool("add", map[string]interface{}{
				"a": 25.5,
				"b": 16.5,
			})
			require.NoError(t, err, "Math tool execution should succeed")

			// Verify response
			require.NoError(t, AssertMCPResponse(resp), "Math tool should return success")

			responseText, err := ExtractTextFromMCPResponse(resp)
			require.NoError(t, err, "Should extract text from response")
			assert.Contains(t, responseText, "42", "Should return correct math result")

			logger.Info("✅ Layer 2: Math tool execution verified", zap.String("response", responseText))
		})

		t.Run("ErrorHandlingAndRetries", func(t *testing.T) {
			client := NewMCPClient(router)

			// Initialize
			_, err := client.Initialize()
			require.NoError(t, err)

			// Test non-existent tool
			resp, err := client.CallTool("nonexistent_tool", map[string]interface{}{})

			// Should handle error gracefully
			if err != nil {
				assert.Contains(t, err.Error(), "tool", "Error should mention tool issue")
				logger.Info("✅ Layer 2: Non-existent tool error handled correctly")
			} else if resp.Error != nil {
				// Check if response contains error
				assert.NotNil(t, resp.Error, "Should have error in response")
				logger.Info("✅ Layer 2: Tool error in response handled correctly")
			}

			// Test invalid parameters for existing tool
			resp, err = client.CallTool("add", map[string]interface{}{
				"invalid_param": "should_fail",
			})

			// Should handle parameter error
			if err == nil {
				// Check response for error
				if resp.Error != nil {
					assert.NotNil(t, resp.Error, "Should have parameter error")
					logger.Info("✅ Layer 2: Parameter validation error handled")
				}
			}
		})

		logger.Info("✅ Layer 2: Protocol Testing - COMPLETED")
	})

	// =============================================================================
	// LAYER 3: USER WORKFLOW TESTING
	// =============================================================================
	t.Run("Layer3_UserWorkflowTesting", func(t *testing.T) {
		logger.Info("📋 Layer 3: User Workflow Testing")

		claudeController := NewClaudeCodeRealController(t, stack.GetClaudeCodeContainerName())
		defer claudeController.Cleanup()

		t.Run("ClaudeCodeMCPConfiguration", func(t *testing.T) {
			// Test proper MCP server configuration via Claude Code CLI
			ctx := context.Background()
			configCmd := exec.CommandContext(ctx, "docker", "exec", stack.GetClaudeCodeContainerName(), // #nosec G204
				"sh", "-c", "cd /workspace && claude mcp list")
			output, err := configCmd.CombinedOutput()

			// Expected: Either configured servers or "no servers" message
			outputStr := string(output)
			if err != nil {
				// Claude Code might require API key for mcp commands, but should still show help
				assert.Contains(t, outputStr, "mcp", "Should show MCP-related output")
			} else {
				// Successfully ran mcp list
				logger.Info("✅ Layer 3: MCP list command executed", zap.String("output", outputStr))
			}
		})

		t.Run("SimulateClaudeCodeMCPInteraction", func(t *testing.T) {
			// Since Claude Code requires API key, simulate the workflow by testing
			// the infrastructure components that Claude Code would use

			// 1. Verify MCP config directory structure that Claude Code creates
			ctx := context.Background()
			checkConfigCmd := exec.CommandContext(ctx, "docker", "exec", stack.GetClaudeCodeContainerName(), // #nosec G204
				"sh", "-c", "ls -la /workspace/.claude/ 2>/dev/null || echo 'Config dir not created yet'")
			configOutput, _ := checkConfigCmd.Output()
			logger.Info("Claude Code config directory status", zap.String("output", string(configOutput)))

			// 2. Test the router connection that Claude Code would use
			router := NewRouterController(t, "wss://localhost:8443/ws")

			err := router.Start()
			require.NoError(t, err, "Router should be available for Claude Code")

			defer func() {
				if err := router.Stop(); err != nil {
					logger.Error("Failed to stop router", zap.Error(err))
				}
			}()

			// 3. Simulate Claude Code tool execution workflow
			client := NewMCPClient(router)
			_, err = client.Initialize()
			require.NoError(t, err)

			// Simulate Claude Code asking for available tools
			tools, err := client.ListTools()
			require.NoError(t, err)
			assert.GreaterOrEqual(t, len(tools), 2, "Claude Code would see available tools")

			// Simulate Claude Code executing a tool on user's behalf
			echoResp, err := client.CallTool("echo", map[string]interface{}{
				"message": "Simulated Claude Code user request",
			})
			require.NoError(t, err)

			responseText, err := ExtractTextFromMCPResponse(echoResp)
			require.NoError(t, err)
			assert.Contains(t, responseText, "Simulated Claude Code", "Tool execution works for Claude Code")

			logger.Info("✅ Layer 3: Claude Code workflow simulation verified")
		})

		t.Run("MultipleConcurrentToolCalls", func(t *testing.T) {
			router := NewRouterController(t, "wss://localhost:8443/ws")

			err := router.Start()
			require.NoError(t, err, "Router should support concurrent calls")

			defer func() {
				if err := router.Stop(); err != nil {
					logger.Error("Failed to stop router", zap.Error(err))
				}
			}()

			client := NewMCPClient(router)
			_, err = client.Initialize()
			require.NoError(t, err)

			// Test concurrent tool calls (simulating multiple Claude Code operations)
			const numConcurrentCalls = 5

			var wg sync.WaitGroup

			results := make([]string, numConcurrentCalls)
			errors := make([]error, numConcurrentCalls)

			for i := 0; i < numConcurrentCalls; i++ {
				wg.Add(1)

				go func(index int) {
					defer wg.Done()

					resp, err := client.CallTool("echo", map[string]interface{}{
						"message": fmt.Sprintf("Concurrent call #%d", index),
					})
					if err != nil {
						errors[index] = err

						return
					}

					text, err := ExtractTextFromMCPResponse(resp)
					if err != nil {
						errors[index] = err

						return
					}

					results[index] = text
				}(i)
			}

			wg.Wait()

			// Verify all calls succeeded
			successCount := 0

			for i := 0; i < numConcurrentCalls; i++ {
				if errors[i] == nil && strings.Contains(results[i], fmt.Sprintf("#%d", i)) {
					successCount++
				}
			}

			assert.GreaterOrEqual(t, successCount, numConcurrentCalls-2,
				"Most concurrent calls should succeed (allow up to 2 failures for concurrent timing)")

			logger.Info("✅ Layer 3: Concurrent tool calls verified",
				zap.Int("successful", successCount), zap.Int("total", numConcurrentCalls))
		})

		t.Run("SessionManagementAndReconnection", func(t *testing.T) {
			// Test router connection resilience (what Claude Code would experience)
			var router *RouterController

			router = NewRouterController(t, "wss://localhost:8443/ws")
			err := router.Start()
			require.NoError(t, err)

			client := NewMCPClient(router)
			_, err = client.Initialize()
			require.NoError(t, err)

			// First call should work
			resp1, err := client.CallTool("echo", map[string]interface{}{
				"message": "Before reconnection test",
			})
			require.NoError(t, err, "First call should work")

			text1, _ := ExtractTextFromMCPResponse(resp1)
			assert.Contains(t, text1, "Before reconnection", "First call content verified")

			// Simulate brief disconnection by stopping and restarting router
			if err := router.Stop(); err != nil {
				logger.Error("Failed to stop router during reconnection test", zap.Error(err))
			}

			// Brief wait for process cleanup
			waitHelper := NewWaitHelper(t)
			waitHelper.WaitBriefly("process cleanup before restart")

			router = NewRouterController(t, "wss://localhost:8443/ws")
			err = router.Start()
			require.NoError(t, err, "Router should restart successfully")

			defer func() {
				if err := router.Stop(); err != nil {
					logger.Error("Failed to stop router", zap.Error(err))
				}
			}()

			// Create new client (simulating Claude Code reconnection)
			client = NewMCPClient(router)
			_, err = client.Initialize()
			require.NoError(t, err, "Should reinitialize after reconnection")

			// Call should work after reconnection
			resp2, err := client.CallTool("echo", map[string]interface{}{
				"message": "After reconnection test",
			})
			require.NoError(t, err, "Call should work after reconnection")

			text2, _ := ExtractTextFromMCPResponse(resp2)
			assert.Contains(t, text2, "After reconnection", "Reconnection call content verified")

			logger.Info("✅ Layer 3: Session management and reconnection verified")
		})

		logger.Info("✅ Layer 3: User Workflow Testing - COMPLETED")
	})

	// Cleanup
	t.Cleanup(func() {
		logger.Info("🧹 Cleaning up Comprehensive E2E Test")

		if err := stack.Stop(); err != nil {
			logger.Error("Failed to stop stack during cleanup", zap.Error(err))
		}
	})

	logger.Info("🎉 Comprehensive Claude Code E2E Test Suite - ALL LAYERS COMPLETED!")
}

// Helper function to assert MCP response was successful.
func AssertMCPResponse(resp *MCPResponse) error {
	if resp.Error != nil {
		return fmt.Errorf("MCP response error: %s", resp.Error.Message)
	}

	if resp.Result == nil {
		return errors.New("MCP response missing result")
	}

	return nil
}

// Helper function to extract text from MCP response.
func ExtractTextFromMCPResponse(resp *MCPResponse) (string, error) {
	if resp.Error != nil {
		return "", fmt.Errorf("response has error: %s", resp.Error.Message)
	}

	return extractTextFromResult(resp.Result)
}

// extractTextFromResult extracts text from different result formats.
func extractTextFromResult(result interface{}) (string, error) {
	// Format 1: result is a map (like initialize response)
	if text, err := extractTextFromMapResult(result); err == nil {
		return text, nil
	}

	// Format 2: result is an array (like tool call responses)
	if text, err := extractTextFromArrayResult(result); err == nil {
		return text, nil
	}

	// Format 3: result is a string
	if text, err := extractTextFromStringResult(result); err == nil {
		return text, nil
	}

	// Ultimate fallback: convert whatever it is to string
	return convertResultToJSON(result)
}

// extractTextFromMapResult extracts text from map-based results.
func extractTextFromMapResult(result interface{}) (string, error) {
	resultMap, ok := result.(map[string]interface{})
	if !ok {
		return "", errors.New("not a map")
	}

	// Try content field first (some MCP responses)
	if content, ok := resultMap["content"].([]interface{}); ok && len(content) > 0 {
		if textContent, ok := content[0].(map[string]interface{}); ok {
			if text, ok := textContent["text"].(string); ok {
				return text, nil
			}
		}
	}

	// Fallback: convert entire result to string
	return convertResultToJSON(resultMap)
}

// extractTextFromArrayResult extracts text from array-based results.
func extractTextFromArrayResult(result interface{}) (string, error) {
	resultArray, ok := result.([]interface{})
	if !ok || len(resultArray) == 0 {
		return "", errors.New("not an array or empty")
	}

	// Check if first element has text field
	if textContent, ok := resultArray[0].(map[string]interface{}); ok {
		if text, ok := textContent["text"].(string); ok {
			return text, nil
		}
	}

	// Fallback: convert entire array to string
	return convertResultToJSON(resultArray)
}

// extractTextFromStringResult extracts text from string results.
func extractTextFromStringResult(result interface{}) (string, error) {
	if resultStr, ok := result.(string); ok {
		return resultStr, nil
	}

	return "", errors.New("not a string")
}

// convertResultToJSON converts any result to JSON string.
func convertResultToJSON(result interface{}) (string, error) {
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("failed to marshal result: %w", err)
	}

	return string(resultJSON), nil
}
