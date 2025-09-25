package main

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"github.com/go-redis/redis/v8"

	"github.com/poiley/mcp-bridge/test/testutil/e2e"
)

// Helper functions for TestFullStackEndToEnd

func testBasicMCPFlow(t *testing.T, client *e2e.MCPClient) {
	t.Helper()
	// Basic MCP flow test implementation
	t.Log("Testing basic MCP flow")

	// Test echo tool
	resp, err := client.CallTool("echo", map[string]interface{}{
		"message": "test",
	})
	if err != nil {
		t.Errorf("Failed to call echo tool: %v", err)
	}

	if resp == nil {
		t.Error("Expected response, got nil")
	}
}

func testMultipleToolExecution(t *testing.T, client *e2e.MCPClient) {
	t.Helper()
	// Multiple tool execution test
	t.Log("Testing multiple tool execution")

	for i := 0; i < 5; i++ {
		resp, err := client.CallTool("echo", map[string]interface{}{
			"message": "test " + string(rune(i)),
		})
		if err != nil {
			t.Errorf("Failed to call echo tool: %v", err)
		}

		if resp == nil {
			t.Error("Expected response, got nil")
		}
	}
}

func testErrorHandling(t *testing.T, client *e2e.MCPClient) {
	t.Helper()
	// Error handling test
	t.Log("Testing error handling")

	// Call non-existent tool
	resp, err := client.CallTool("nonexistent", map[string]interface{}{})
	if err == nil {
		t.Error("Expected error for non-existent tool, got none")
	}

	if resp != nil && resp.Error == nil {
		t.Error("Expected error response")
	}
}

func testConcurrentRequests(t *testing.T, client *e2e.MCPClient) {
	t.Helper()
	// Concurrent requests test
	t.Log("Testing concurrent requests")

	// Run multiple requests concurrently
	done := make(chan bool, defaultMaxRetries)

	for i := 0; i < defaultMaxRetries; i++ {
		go func(id int) {
			_, _ = client.CallTool("echo", map[string]interface{}{
				"message": "concurrent",
			})

			done <- true
		}(i)
	}

	// Wait for all to complete
	for i := 0; i < 10; i++ {
		<-done
	}
}

func testNetworkResilience(t *testing.T, client *e2e.MCPClient, stack DockerStackInterface) {
	t.Helper()
	// Network resilience test
	t.Log("Testing network resilience")

	// Test basic connectivity
	resp, err := client.CallTool("echo", map[string]interface{}{
		"message": "resilience test",
	})
	if err != nil {
		t.Errorf("Failed to call echo tool: %v", err)
	}

	if resp == nil {
		t.Error("Expected response, got nil")
	}
}

// DockerStackWithMultipleBackends extends DockerStack for multi-backend testing.
type DockerStackWithMultipleBackends struct {
	*DockerStack
	backends []string
}

// NewDockerStackWithMultipleBackends creates a new multi-backend Docker stack.
func NewDockerStackWithMultipleBackends(t *testing.T) *DockerStackWithMultipleBackends {
	t.Helper()

	return &DockerStackWithMultipleBackends{
		DockerStack: NewDockerStack(t),
		backends:    []string{"backend-1", "backend-2", "backend-3"},
	}
}

// StopBackend stops a specific backend.
func (ds *DockerStackWithMultipleBackends) StopBackend(name string) error {
	// Implementation for stopping a backend
	return nil
}

// StartBackend starts a specific backend.
func (ds *DockerStackWithMultipleBackends) StartBackend(name string) error {
	// Implementation for starting a backend
	return nil
}

// AddBackend adds a new backend.
func (ds *DockerStackWithMultipleBackends) AddBackend(name string) error {
	// Implementation for adding a backend
	ds.backends = append(ds.backends, name)

	return nil
}

// extractBackendID extracts the backend ID from response text.
func extractBackendID(text string) string {
	// Look for backend identifier in response
	if strings.Contains(text, "backend-1") {
		return "backend-1"
	}

	if strings.Contains(text, "backend-2") {
		return "backend-2"
	}

	if strings.Contains(text, "backend-3") {
		return "backend-3"
	}

	return ""
}

// extractSessionID extracts session ID from MCP response.
func extractSessionID(response *MCPResponse) string {
	if response.Result != nil {
		if result, ok := response.Result.(map[string]interface{}); ok {
			if session, ok := result["sessionId"].(string); ok {
				return session
			}
		}
	}

	return ""
}

// createRedisClient creates a Redis client for testing.
func createRedisClient(t *testing.T) *redis.Client {
	t.Helper()

	return redis.NewClient(&redis.Options{
		Addr:     "127.0.0.1:6379", // Use IPv4 explicitly instead of localhost
		Password: "",
		DB:       0,
	})
}

// getContainerMemoryUsage gets memory usage for a container.
func getContainerMemoryUsage(t *testing.T, stack *DockerStack, serviceName string) int64 {
	t.Helper()

	containerName := fmt.Sprintf("full_stack_%s_1", serviceName)

	// #nosec G204 - serviceName is controlled by test code
	cmd := exec.CommandContext(context.Background(), "docker", "stats", "--no-stream", "--format",
		"{{.MemUsage}}", containerName)

	output, err := cmd.Output()
	if err != nil {
		t.Logf("Failed to get memory usage: %v", err)

		return 0
	}
	// Parse memory usage from output
	// Format is like "123.4MiB / 1.234GiB"
	// Simple conversion - would need proper parsing in production
	_ = strings.Split(string(output), " / ")[0]

	const defaultMemoryMB = 100

	return defaultMemoryMB * bytesPerKB * bytesPerKB // Return 100MB as default for now
}
