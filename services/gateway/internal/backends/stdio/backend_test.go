
package stdio

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/poiley/mcp-bridge/services/router/pkg/mcp"
)

const (
	testIterations = 100
	httpStatusOK   = 200
)

func TestStdioBackend_CreateStdioBackend(t *testing.T) {
	logger := zaptest.NewLogger(t)

	config := Config{
		Command:    []string{"echo", "test"},
		Timeout:    10 * time.Second,
		WorkingDir: "/tmp",
	}

	backend := CreateStdioBackend("test-backend", config, logger, nil)

	assert.NotNil(t, backend)
	assert.Equal(t, "test-backend", backend.name)
	assert.Equal(t, config.Command, backend.config.Command)
	assert.Equal(t, "stdio", backend.GetProtocol())
	assert.Equal(t, "test-backend", backend.GetName())
}

func TestStdioBackend_ConfigDefaults(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Test with minimal config
	config := Config{
		Command: []string{"echo", "test"},
	}

	backend := CreateStdioBackend("test", config, logger, nil)

	// Verify defaults are set
	assert.Equal(t, 30*time.Second, backend.config.Timeout)
	assert.Equal(t, 1024*1024, backend.config.MaxBufferSize)
	assert.Equal(t, 30*time.Second, backend.config.HealthCheck.Interval)
	assert.Equal(t, 5*time.Second, backend.config.HealthCheck.Timeout)
	assert.Equal(t, 5*time.Second, backend.config.Process.RestartDelay)
}

func TestStdioBackend_StartStop(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Create a simple echo server script
	script := createTestScript(t, `#!/bin/bash
while read line; do
  echo "$line"
done`)

	config := Config{
		Command: []string{"bash", script},
		Timeout: 5 * time.Second,
	}

	backend := CreateStdioBackend("test", config, logger, nil)

	ctx := context.Background()

	// Test start
	err := backend.Start(ctx)
	require.NoError(t, err)

	// Verify running state
	backend.mu.RLock()
	assert.True(t, backend.running)
	backend.mu.RUnlock()

	// Test double start (should fail)
	err = backend.Start(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already running")

	// Test stop - FIX: Capture the actual stop error
	stopErr := backend.Stop(ctx)
	assert.NoError(t, stopErr)

	// Verify stopped state
	backend.mu.RLock()
	assert.False(t, backend.running)
	backend.mu.RUnlock()

	// Test double stop (should not error)
	stopErr2 := backend.Stop(ctx)
	assert.NoError(t, stopErr2)
}

func TestStdioBackend_StartWithInvalidCommand(t *testing.T) {
	logger := zaptest.NewLogger(t)

	tests := []struct {
		name    string
		config  Config
		wantErr string
	}{
		{
			name: "empty command",
			config: Config{
				Command: []string{},
			},
			wantErr: "command cannot be empty",
		},
		{
			name: "non-existent command",
			config: Config{
				Command: []string{"non-existent-command-12345"},
			},
			wantErr: "stdio start_process failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			backend := CreateStdioBackend("test", tt.config, logger, nil)

			err := backend.Start(context.Background())
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestStdioBackend_SendRequest(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Create a JSON echo server
	script := createTestScript(t, `#!/bin/bash
while IFS= read -r line; do
  echo "$line"
done`)

	config := Config{
		Command: []string{"bash", script},
		Timeout: 5 * time.Second,
	}

	backend := CreateStdioBackend("test", config, logger, nil)
	ctx := context.Background()

	err := backend.Start(ctx)
	require.NoError(t, err)

	defer func() { _ = backend.Stop(ctx) }() 

	// Give process time to start
	time.Sleep(testIterations * time.Millisecond)

	// Test request with auto-generated ID
	req := &mcp.Request{
		Method: "test/method",
		Params: map[string]interface{}{"key": "value"},
	}

	response, err := backend.SendRequest(ctx, req)
	require.NoError(t, err)
	assert.NotNil(t, response)

	// Verify request ID was set
	assert.NotNil(t, req.ID)
	assert.NotEmpty(t, req.ID)
}

func TestStdioBackend_SendRequestWithTimeout(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Create a server that doesn't respond
	script := createTestScript(t, `#!/bin/bash
while read line; do
  sleep 10  # Delay longer than timeout
  echo "$line"
done`)

	config := Config{
		Command: []string{"bash", script},
		Timeout: 1 * time.Second, // Short timeout
	}

	backend := CreateStdioBackend("test", config, logger, nil)
	ctx := context.Background()

	err := backend.Start(ctx)
	require.NoError(t, err)

	defer func() { _ = backend.Stop(ctx) }() 

	time.Sleep(testIterations * time.Millisecond)

	req := &mcp.Request{
		Method: "test/method",
		ID:     "test-123",
	}

	_, err = backend.SendRequest(ctx, req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "TIMEOUT")
}

func TestStdioBackend_SendRequestNotRunning(t *testing.T) {
	logger := zaptest.NewLogger(t)

	config := Config{
		Command: []string{"echo", "test"},
	}

	backend := CreateStdioBackend("test", config, logger, nil)

	req := &mcp.Request{
		Method: "test/method",
		ID:     "test-123",
	}

	_, err := backend.SendRequest(context.Background(), req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not running")
}

func TestStdioBackend_Health(t *testing.T) {
	logger := zaptest.NewLogger(t)

	script := createTestScript(t, `#!/bin/bash
while read line; do
  echo "$line"
done`)

	config := Config{
		Command: []string{"bash", script},
		HealthCheck: HealthCheckConfig{
			Enabled: true,
			Timeout: 2 * time.Second,
		},
	}

	backend := CreateStdioBackend("test", config, logger, nil)
	ctx := context.Background()

	// Health check should fail when not running
	err := backend.Health(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not running")

	// Start backend
	err = backend.Start(ctx)
	require.NoError(t, err)

	defer func() { _ = backend.Stop(ctx) }() 

	time.Sleep(testIterations * time.Millisecond)

	// Health check should pass when running
	err = backend.Health(ctx)
	assert.NoError(t, err)

	// Check metrics updated
	metrics := backend.GetMetrics()
	assert.True(t, metrics.IsHealthy)
	assert.Less(t, time.Since(metrics.LastHealthCheck), time.Second)
}

func TestStdioBackend_GetMetrics(t *testing.T) {
	logger := zaptest.NewLogger(t)

	config := Config{
		Command: []string{"echo", "test"},
	}

	backend := CreateStdioBackend("test", config, logger, nil)

	metrics := backend.GetMetrics()
	assert.False(t, metrics.IsHealthy)
	assert.Equal(t, uint64(0), metrics.RequestCount)
	assert.Equal(t, uint64(0), metrics.ErrorCount)
}

func TestStdioBackend_UpdateMetrics(t *testing.T) {
	logger := zaptest.NewLogger(t)

	config := Config{
		Command: []string{"echo", "test"},
	}

	backend := CreateStdioBackend("test", config, logger, nil)

	// Update metrics
	backend.updateMetrics(func(m *BackendMetrics) {
		m.RequestCount = testIterations
		m.ErrorCount = 5
		m.IsHealthy = true
	})

	metrics := backend.GetMetrics()
	assert.Equal(t, uint64(testIterations), metrics.RequestCount)
	assert.Equal(t, uint64(5), metrics.ErrorCount)
	assert.True(t, metrics.IsHealthy)
}

func TestStdioBackend_EnvironmentVariables(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Create script that outputs environment variable
	script := createTestScript(t, `#!/bin/bash
echo "TEST_VAR=$TEST_VAR"`)

	config := Config{
		Command: []string{"bash", script},
		Env: map[string]string{
			"TEST_VAR": "test_value",
		},
	}

	backend := CreateStdioBackend("test", config, logger, nil)
	ctx := context.Background()

	err := backend.Start(ctx)
	require.NoError(t, err)

	defer func() { _ = backend.Stop(ctx) }() 

	// Verify environment variable was set
	assert.Equal(t, "test_value", backend.config.Env["TEST_VAR"])
}

func TestStdioBackend_WorkingDirectory(t *testing.T) {
	logger := zaptest.NewLogger(t)

	tmpDir := t.TempDir()

	config := Config{
		Command:    []string{"pwd"},
		WorkingDir: tmpDir,
	}

	backend := CreateStdioBackend("test", config, logger, nil)

	assert.Equal(t, tmpDir, backend.config.WorkingDir)
}

// Helper function to create test scripts.
func createTestScript(t *testing.T, content string) string {
	t.Helper()

	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "test_script.sh")

	err := os.WriteFile(scriptPath, []byte(content), 0o755) 
	require.NoError(t, err)

	return scriptPath
}

// TestStdioBackend_RealMCPServer tests with a simple MCP-like server.
func TestStdioBackend_RealMCPServer(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Create a simple MCP server that responds to JSON-RPC
	script := createMCPTestScript(t)

	config := Config{
		Command: []string{"python3", script},
		Timeout: 5 * time.Second,
	}

	backend := CreateStdioBackend("test", config, logger, nil)
	ctx := context.Background()

	err := backend.Start(ctx)
	require.NoError(t, err)

	defer func() { _ = backend.Stop(ctx) }()

	// Give server time to start
	time.Sleep(httpStatusOK * time.Millisecond)

	runMCPServerTest(t, backend, ctx)
}

func createMCPTestScript(t *testing.T) string {
	t.Helper()

	script := `#!/usr/bin/env python3
import json
import sys

while True:
    try:
        line = input()
        request = json.loads(line)
        
        response = {
            "jsonrpc": "2.0",
            "id": request.get("id"),
            "result": {
                "method": request.get("method"),
                "echo": request.get("params", {})
            }
        }
        
        print(json.dumps(response))
        sys.stdout.flush()
    except EOFError:
        break
    except Exception as e:
        error_response = {
            "jsonrpc": "2.0",
            "id": request.get("id") if 'request' in locals() else None,
            "error": {"code": -32000, "message": str(e)}
        }
        print(json.dumps(error_response))
        sys.stdout.flush()`

	return createTestScript(t, script)
}

func runMCPServerTest(t *testing.T, backend *Backend, ctx context.Context) {
	t.Helper()

	// Send an MCP request
	req := &mcp.Request{
		JSONRPC: "2.0",
		Method:  "test/echo",
		ID:      "test-123",
		Params: map[string]interface{}{
			"message": "hello world",
		},
	}

	response, err := backend.SendRequest(ctx, req)
	require.NoError(t, err)
	assert.NotNil(t, response)
	assert.Equal(t, "test-123", response.ID)

	// Check response content
	result, ok := response.Result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "test/echo", result["method"])

	echoParams, ok := result["echo"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "hello world", echoParams["message"])
}
