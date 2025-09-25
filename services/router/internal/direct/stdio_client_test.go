package direct

import (
	"github.com/poiley/mcp-bridge/services/router/internal/constants"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/poiley/mcp-bridge/services/router/pkg/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

func TestNewStdioClient(t *testing.T) { 
	logger := zaptest.NewLogger(t)

	testCases := []struct {
		name      string
		serverURL string
		config    StdioClientConfig
		wantError bool
	}{
		{
			name:      "valid config with command",
			serverURL: "stdio://echo hello",
			config: StdioClientConfig{
				Command: []string{"echo", "hello"},
			},
			wantError: false,
		},
		{
			name:      "valid config parsing from URL",
			serverURL: "stdio://python -m json.tool",
			config:    StdioClientConfig{},
			wantError: false,
		},
		{
			name:      "valid config with raw command",
			serverURL: "python -c print('hello')",
			config:    StdioClientConfig{},
			wantError: false,
		},
		{
			name:      "empty command",
			serverURL: "",
			config:    StdioClientConfig{},
			wantError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			client, err := NewStdioClient("test-client", tc.serverURL, tc.config, logger)

			if tc.wantError {
				require.Error(t, err)
				assert.Nil(t, client)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, client)
				assert.Equal(t, "test-client", client.GetName())
				assert.Equal(t, "stdio", client.GetProtocol())
				assert.Equal(t, StateDisconnected, client.GetState())
			}
		})
	}
}

func TestStdioClientDefaults(t *testing.T) { 
	logger := zaptest.NewLogger(t)
	config := StdioClientConfig{} // Empty config to test defaults

	client, err := NewStdioClient("test-client", "echo hello", config, logger)
	require.NoError(t, err)

	assert.Equal(t, 30*time.Second, client.config.Timeout)
	assert.Equal(t, 1024*1024, client.config.MaxBufferSize)
	assert.Equal(t, 30*time.Second, client.config.HealthCheck.Interval)
	assert.Equal(t, 5*time.Second, client.config.HealthCheck.Timeout)
	assert.Equal(t, 5*time.Second, client.config.Process.RestartDelay)
	assert.Equal(t, 10*time.Second, client.config.Process.KillTimeout)
	assert.Equal(t, 3, client.config.Process.MaxRestarts)
}

func TestStdioClientConnectAndClose(t *testing.T) { 
	logger := zaptest.NewLogger(t)
	config := StdioClientConfig{
		Command: []string{"cat"}, // cat command reads from stdin and writes to stdout
		Timeout: 5 * time.Second,
		HealthCheck: HealthCheckConfig{
			Enabled: false, // Disable for simple test
		},
	}

	client, err := NewStdioClient("test-client", "stdio://cat", config, logger)
	require.NoError(t, err)

	ctx := context.Background()

	// Test connect.
	err = client.Connect(ctx)
	require.NoError(t, err)
	assert.Equal(t, StateConnected, client.GetState())

	// Test connect when already connected.
	err = client.Connect(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already connected")

	// Test close.
	err = client.Close(ctx)
	require.NoError(t, err)
	assert.Equal(t, StateClosed, client.GetState())

	// Test close when already closed.
	err = client.Close(ctx)
	require.NoError(t, err) // Should not error
}

func TestStdioClientSendRequest(t *testing.T) { 
	logger := zaptest.NewLogger(t)
	scriptPath := createStdioEchoScript(t)
	
	client := setupStdioTestClient(t, logger, scriptPath)
	defer cleanupStdioTestClient(t, client)

	runStdioSendRequestTest(t, client)
}

func createStdioEchoScript(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "echo_server.py")
	scriptContent := `#!/usr/bin/env python3
import json
import sys

def main():
    for line in sys.stdin:
        try:
            request = json.loads(line.strip())
            response = {
                "jsonrpc": "2.0",
                "id": request.get("id"),
                "result": {"echo": request.get("method", "unknown")}
            }
            print(json.dumps(response))
            sys.stdout.flush()
        except Exception as e:
            error_response = {
                "jsonrpc": "2.0",
                "id": request.get("id") if "request" in locals() else None,
                "error": {"code": -32603, "message": str(e)}
            }
            print(json.dumps(error_response))
            sys.stdout.flush()

if __name__ == "__main__":
    main()
`

	err := os.WriteFile(scriptPath, []byte(scriptContent), 0o755)
	require.NoError(t, err)
	return scriptPath
}

func setupStdioTestClient(t *testing.T, logger *zap.Logger, scriptPath string) *StdioClient {
	t.Helper()
	config := StdioClientConfig{
		Command: []string{"python3", scriptPath},
		Timeout: 5 * time.Second,
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
	}

	client, err := NewStdioClient("echo-client", "stdio://python3 "+scriptPath, config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = client.Connect(ctx)
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)
	return client
}

func cleanupStdioTestClient(t *testing.T, client *StdioClient) {
	t.Helper()
	ctx := context.Background()
	err := client.Close(ctx)
	require.NoError(t, err)
}

func runStdioSendRequestTest(t *testing.T, client *StdioClient) {
	t.Helper()
	ctx := context.Background()
	
	req := &mcp.Request{
		JSONRPC: constants.TestJSONRPCVersion,
		Method:  "test_method",
		ID:      "test-123",
	}

	resp, err := client.SendRequest(ctx, req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "2.0", resp.JSONRPC)
	assert.Equal(t, "test-123", resp.ID)

	verifyStdioRequestMetrics(t, client)
}

func verifyStdioRequestMetrics(t *testing.T, client *StdioClient) {
	t.Helper()
	metrics := client.GetMetrics()
	assert.Equal(t, uint64(1), metrics.RequestCount)
	assert.Equal(t, uint64(0), metrics.ErrorCount)
	assert.Positive(t, metrics.AverageLatency)
}

func TestStdioClientSendRequestNotConnected(t *testing.T) { 
	logger := zaptest.NewLogger(t)
	config := StdioClientConfig{
		Command: []string{"cat"},
	}

	client, err := NewStdioClient("test-client", "stdio://cat", config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	req := &mcp.Request{
		JSONRPC: constants.TestJSONRPCVersion,
		Method:  "test",
		ID:      "test-123",
	}

	// Try to send request when not connected.
	_, err = client.SendRequest(ctx, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not connected")
}

func TestStdioClientSendRequestTimeout(t *testing.T) { 
	logger := zaptest.NewLogger(t)

	// Create a slow server script that doesn't respond.
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "slow_server.py")
	scriptContent := `#!/usr/bin/env python3
import time
import sys

# Read input but never respond
for line in sys.stdin:
    time.sleep(10)  # Sleep longer than the timeout
`

	err := os.WriteFile(scriptPath, []byte(scriptContent), 0o755) 
	require.NoError(t, err)

	config := StdioClientConfig{
		Command: []string{"python3", scriptPath},
		Timeout: 100 * time.Millisecond, // Very short timeout
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
	}

	client, err := NewStdioClient("slow-client", "stdio://python3 "+scriptPath, config, logger)
	require.NoError(t, err)

	ctx := context.Background()

	// Connect.
	err = client.Connect(ctx)
	require.NoError(t, err)

	defer func() {
		err := client.Close(ctx)
		require.NoError(t, err)
	}()

	// Wait a moment for the process to be ready.
	time.Sleep(constants.TestLongTickInterval)

	// Test timeout request.
	req := &mcp.Request{
		JSONRPC: constants.TestJSONRPCVersion,
		Method:  "slow_method",
		ID:      "test-123",
	}

	_, err = client.SendRequest(ctx, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timeout")

	// Check metrics.
	metrics := client.GetMetrics()
	assert.Equal(t, uint64(0), metrics.RequestCount)
	assert.Equal(t, uint64(1), metrics.ErrorCount)
}

func TestStdioClientHealth(t *testing.T) { 
	logger := zaptest.NewLogger(t)
	config := StdioClientConfig{
		Command: []string{"cat"},
		HealthCheck: HealthCheckConfig{
			Enabled: false, // We'll test manually
			Timeout: 1 * time.Second,
		},
	}

	client, err := NewStdioClient("test-client", "stdio://cat", config, logger)
	require.NoError(t, err)

	ctx := context.Background()

	// Test health when not connected.
	err = client.Health(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not connected")

	// Connect.
	err = client.Connect(ctx)
	require.NoError(t, err)

	defer func() {
		err := client.Close(ctx)
		require.NoError(t, err)
	}()

	// Test health when connected.
	err = client.Health(ctx)
	require.NoError(t, err)
	assert.Equal(t, StateHealthy, client.GetState())

	// Check metrics.
	metrics := client.GetMetrics()
	assert.True(t, metrics.IsHealthy)
	assert.False(t, metrics.LastHealthCheck.IsZero())
}

func TestStdioClientHealthCheck(t *testing.T) { 
	logger := zaptest.NewLogger(t)
	scriptPath := createStdioPingScript(t)
	
	client := setupStdioHealthCheckClient(t, logger, scriptPath)
	defer cleanupStdioHealthCheckClient(t, client)

	runStdioHealthCheckTest(t, client)
}

func createStdioPingScript(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "ping_server.py")
	scriptContent := `#!/usr/bin/env python3
import json
import sys

def main():
    for line in sys.stdin:
        try:
            request = json.loads(line.strip())
            if request.get("method") == "ping":
                response = {
                    "jsonrpc": "2.0",
                    "id": request.get("id"),
                    "result": "pong"
                }
            else:
                response = {
                    "jsonrpc": "2.0",
                    "id": request.get("id"),
                    "result": {"echo": request.get("method", "unknown")}
                }
            print(json.dumps(response))
            sys.stdout.flush()
        except Exception as e:
            error_response = {
                "jsonrpc": "2.0",
                "id": request.get("id") if "request" in locals() else None,
                "error": {"code": -32603, "message": str(e)}
            }
            print(json.dumps(error_response))
            sys.stdout.flush()

if __name__ == "__main__":
    main()
`

	err := os.WriteFile(scriptPath, []byte(scriptContent), 0o755)
	require.NoError(t, err)
	return scriptPath
}

func setupStdioHealthCheckClient(t *testing.T, logger *zap.Logger, scriptPath string) *StdioClient {
	t.Helper()
	config := StdioClientConfig{
		Command: []string{"python3", scriptPath},
		Timeout: 5 * time.Second,
		HealthCheck: HealthCheckConfig{
			Enabled:  true,
			Interval: 50 * time.Millisecond,
			Timeout:  1 * time.Second,
		},
	}

	client, err := NewStdioClient("ping-client", "stdio://python3 "+scriptPath, config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = client.Connect(ctx)
	require.NoError(t, err)

	return client
}

func cleanupStdioHealthCheckClient(t *testing.T, client *StdioClient) {
	t.Helper()
	ctx := context.Background()
	err := client.Close(ctx)
	require.NoError(t, err)
}

func runStdioHealthCheckTest(t *testing.T, client *StdioClient) {
	t.Helper()
	time.Sleep(2 * constants.TestSleepShort)

	assert.Equal(t, StateHealthy, client.GetState())
	metrics := client.GetMetrics()
	assert.True(t, metrics.IsHealthy)
}

func TestStdioClientGetStatus(t *testing.T) { 
	logger := zaptest.NewLogger(t)
	config := StdioClientConfig{
		Command: []string{"cat"},
	}

	client, err := NewStdioClient("status-client", "stdio://cat", config, logger)
	require.NoError(t, err)

	status := client.GetStatus()
	assert.Equal(t, "status-client", status.Name)
	assert.Equal(t, "stdio://cat", status.URL)
	assert.Equal(t, "stdio", status.Protocol)
	assert.Equal(t, StateDisconnected, status.State)
}

func TestStdioClientConnectInvalidCommand(t *testing.T) { 
	logger := zaptest.NewLogger(t)
	config := StdioClientConfig{
		Command: []string{"nonexistent-command-12345"},
	}

	client, err := NewStdioClient("invalid-client", "stdio://nonexistent-command-12345", config, logger)
	require.NoError(t, err)

	ctx := context.Background()

	// Try to connect with invalid command.
	err = client.Connect(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to start process")
	assert.Equal(t, StateError, client.GetState())
}

func TestStdioClientEnvironmentVariables(t *testing.T) { 
	logger := zaptest.NewLogger(t)
	scriptPath := createStdioEnvScript(t)
	
	client := setupStdioEnvClient(t, logger, scriptPath)
	defer cleanupStdioEnvClient(t, client)

	runStdioEnvVariableTest(t, client)
}

func createStdioEnvScript(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "env_server.py")
	scriptContent := `#!/usr/bin/env python3
import json
import sys
import os

def main():
    for line in sys.stdin:
        try:
            request = json.loads(line.strip())
            response = {
                "jsonrpc": "2.0",
                "id": request.get("id"),
                "result": {"env_var": os.environ.get("TEST_VAR", "not_set")}
            }
            print(json.dumps(response))
            sys.stdout.flush()
        except Exception as e:
            error_response = {
                "jsonrpc": "2.0",
                "id": request.get("id") if "request" in locals() else None,
                "error": {"code": -32603, "message": str(e)}
            }
            print(json.dumps(error_response))
            sys.stdout.flush()

if __name__ == "__main__":
    main()
`

	err := os.WriteFile(scriptPath, []byte(scriptContent), 0o755)
	require.NoError(t, err)
	return scriptPath
}

func setupStdioEnvClient(t *testing.T, logger *zap.Logger, scriptPath string) *StdioClient {
	t.Helper()
	config := StdioClientConfig{
		Command: []string{"python3", scriptPath},
		Env: map[string]string{
			"TEST_VAR": "test_value_12345",
		},
		Timeout: 5 * time.Second,
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
	}

	client, err := NewStdioClient("env-client", "stdio://python3 "+scriptPath, config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = client.Connect(ctx)
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)
	return client
}

func cleanupStdioEnvClient(t *testing.T, client *StdioClient) {
	t.Helper()
	ctx := context.Background()
	err := client.Close(ctx)
	require.NoError(t, err)
}

func runStdioEnvVariableTest(t *testing.T, client *StdioClient) {
	t.Helper()
	ctx := context.Background()
	
	req := &mcp.Request{
		JSONRPC: constants.TestJSONRPCVersion,
		Method:  "get_env",
		ID:      "env-test",
	}

	resp, err := client.SendRequest(ctx, req)
	require.NoError(t, err)

	verifyStdioEnvResponse(t, resp)
}

func verifyStdioEnvResponse(t *testing.T, resp *mcp.Response) {
	t.Helper()
	result, ok := resp.Result.(map[string]interface{})
	require.True(t, ok)
	
	envVar, ok := result["env_var"].(string)
	require.True(t, ok)
	assert.Equal(t, "test_value_12345", envVar)
}

func TestStdioClientWorkingDirectory(t *testing.T) { 
	logger := zaptest.NewLogger(t)
	tmpDir, scriptPath := createStdioWorkingDirScript(t)
	
	client := setupStdioWorkingDirClient(t, logger, scriptPath, tmpDir)
	defer cleanupStdioWorkingDirClient(t, client)

	runStdioWorkingDirTest(t, client, tmpDir)
}

func createStdioWorkingDirScript(t *testing.T) (string, string) {
	t.Helper()
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "pwd_server.py")
	scriptContent := `#!/usr/bin/env python3
import json
import sys
import os

def main():
    for line in sys.stdin:
        try:
            request = json.loads(line.strip())
            response = {
                "jsonrpc": "2.0",
                "id": request.get("id"),
                "result": {"cwd": os.getcwd()}
            }
            print(json.dumps(response))
            sys.stdout.flush()
        except Exception as e:
            error_response = {
                "jsonrpc": "2.0",
                "id": request.get("id") if "request" in locals() else None,
                "error": {"code": -32603, "message": str(e)}
            }
            print(json.dumps(error_response))
            sys.stdout.flush()

if __name__ == "__main__":
    main()
`

	err := os.WriteFile(scriptPath, []byte(scriptContent), 0o755)
	require.NoError(t, err)
	return tmpDir, scriptPath
}

func setupStdioWorkingDirClient(t *testing.T, logger *zap.Logger, scriptPath, tmpDir string) *StdioClient {
	t.Helper()
	config := StdioClientConfig{
		Command:    []string{"python3", scriptPath},
		WorkingDir: tmpDir,
		Timeout:    5 * time.Second,
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
	}

	client, err := NewStdioClient("workdir-client", "stdio://python3 "+scriptPath, config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = client.Connect(ctx)
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)
	return client
}

func cleanupStdioWorkingDirClient(t *testing.T, client *StdioClient) {
	t.Helper()
	ctx := context.Background()
	err := client.Close(ctx)
	require.NoError(t, err)
}

func runStdioWorkingDirTest(t *testing.T, client *StdioClient, expectedTmpDir string) {
	t.Helper()
	ctx := context.Background()
	
	req := &mcp.Request{
		JSONRPC: constants.TestJSONRPCVersion,
		Method:  "get_cwd",
		ID:      "cwd-test",
	}

	resp, err := client.SendRequest(ctx, req)
	require.NoError(t, err)

	verifyStdioWorkingDirResponse(t, resp, expectedTmpDir)
}

func verifyStdioWorkingDirResponse(t *testing.T, resp *mcp.Response, expectedTmpDir string) {
	t.Helper()
	result, ok := resp.Result.(map[string]interface{})
	require.True(t, ok)
	
	cwd, ok := result["cwd"].(string)
	require.True(t, ok)

	expectedDir, err := filepath.EvalSymlinks(expectedTmpDir)
	require.NoError(t, err)
	actualDir, err := filepath.EvalSymlinks(cwd)
	require.NoError(t, err)
	assert.Equal(t, expectedDir, actualDir)
}

// ==============================================================================
// ENHANCED STDIO CLIENT TESTS - COMPREHENSIVE COVERAGE.
// ==============================================================================

// TestStdioClientProcessLifecycle tests complete process lifecycle management.
func TestStdioClientProcessLifecycle(t *testing.T) { 
	logger := zaptest.NewLogger(t)
	scriptPath := createStdioLifecycleScript(t)
	
	client := setupStdioLifecycleClient(t, logger, scriptPath)
	runStdioLifecycleTest(t, client)
}

func createStdioLifecycleScript(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "lifecycle_server.py")
	scriptContent := `#!/usr/bin/env python3
import json
import sys
import signal
import time
import atexit

# Track lifecycle events
lifecycle_log = []

def signal_handler(signum, frame):
    lifecycle_log.append(f"received_signal_{signum}")
    sys.stderr.write(f"Signal {signum} received\n")
    sys.stderr.flush()
    sys.exit(0)

def cleanup():
    lifecycle_log.append("cleanup_called")
    sys.stderr.write("Cleanup called\n")
    sys.stderr.flush()

# Register handlers
signal.signal(signal.SIGTERM, signal_handler)
signal.signal(signal.SIGINT, signal_handler)
atexit.register(cleanup)

lifecycle_log.append("process_started")

def main():
    for line in sys.stdin:
        try:
            request = json.loads(line.strip())
            if request.get("method") == "get_lifecycle":
                response = {
                    "jsonrpc": "2.0",
                    "id": request.get("id"),
                    "result": {"lifecycle": lifecycle_log}
                }
            elif request.get("method") == "exit":
                lifecycle_log.append("exit_requested")
                response = {
                    "jsonrpc": "2.0",
                    "id": request.get("id"),
                    "result": {"status": "exiting"}
                }
                print(json.dumps(response))
                sys.stdout.flush()
                sys.exit(0)
            else:
                response = {
                    "jsonrpc": "2.0",
                    "id": request.get("id"),
                    "result": {"echo": request.get("method", "unknown")}
                }
            print(json.dumps(response))
            sys.stdout.flush()
        except Exception as e:
            error_response = {
                "jsonrpc": "2.0",
                "id": request.get("id") if "request" in locals() else None,
                "error": {"code": -32603, "message": str(e)}
            }
            print(json.dumps(error_response))
            sys.stdout.flush()

if __name__ == "__main__":
    main()
`

	err := os.WriteFile(scriptPath, []byte(scriptContent), 0o755)
	require.NoError(t, err)
	return scriptPath
}

func setupStdioLifecycleClient(t *testing.T, logger *zap.Logger, scriptPath string) *StdioClient {
	t.Helper()
	config := StdioClientConfig{
		Command: []string{"python3", scriptPath},
		Timeout: 5 * time.Second,
		Process: ProcessConfig{
			KillTimeout: 2 * time.Second,
		},
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
	}

	client, err := NewStdioClient("lifecycle-client", "stdio://python3 "+scriptPath, config, logger)
	require.NoError(t, err)
	return client
}

func runStdioLifecycleTest(t *testing.T, client *StdioClient) {
	t.Helper()
	ctx := context.Background()

	testStdioLifecycleStartup(t, client, ctx)
	testStdioLifecycleResponse(t, client, ctx)
	testStdioLifecycleShutdown(t, client, ctx)
}

func testStdioLifecycleStartup(t *testing.T, client *StdioClient, ctx context.Context) {
	t.Helper()
	err := client.Connect(ctx)
	require.NoError(t, err)
	assert.Equal(t, StateConnected, client.GetState())

	time.Sleep(2 * constants.TestSleepShort)
}

func testStdioLifecycleResponse(t *testing.T, client *StdioClient, ctx context.Context) {
	t.Helper()
	req := &mcp.Request{
		JSONRPC: constants.TestJSONRPCVersion,
		Method:  "get_lifecycle",
		ID:      "lifecycle-test",
	}

	resp, err := client.SendRequest(ctx, req)
	require.NoError(t, err)
	assert.NotNil(t, resp)
}

func testStdioLifecycleShutdown(t *testing.T, client *StdioClient, ctx context.Context) {
	t.Helper()
	err := client.Close(ctx)
	require.NoError(t, err)
	assert.Equal(t, StateClosed, client.GetState())
}

// TestStdioClientProcessRestart tests process restart scenarios.
func TestStdioClientProcessRestart(t *testing.T) { 
	logger := zaptest.NewLogger(t)
	scriptPath := createStdioRestartScript(t)
	
	client := setupStdioRestartClient(t, logger, scriptPath)
	defer cleanupStdioRestartClient(t, client)

	runStdioRestartTest(t, client)
}

func createStdioRestartScript(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "restart_server.py")
	scriptContent := `#!/usr/bin/env python3
import json
import sys
import os

def main():
    request_count = 0
    for line in sys.stdin:
        try:
            request = json.loads(line.strip())
            request_count += 1
            
            if request.get("method") == "crash" and request_count > 1:
                # Crash after the second request
                sys.stderr.write("Simulating crash\n")
                sys.stderr.flush()
                os._exit(1)
            
            response = {
                "jsonrpc": "2.0",
                "id": request.get("id"),
                "result": {"request_count": request_count, "method": request.get("method")}
            }
            print(json.dumps(response))
            sys.stdout.flush()
        except Exception as e:
            error_response = {
                "jsonrpc": "2.0",
                "id": request.get("id") if "request" in locals() else None,
                "error": {"code": -32603, "message": str(e)}
            }
            print(json.dumps(error_response))
            sys.stdout.flush()

if __name__ == "__main__":
    main()
`

	err := os.WriteFile(scriptPath, []byte(scriptContent), 0o755)
	require.NoError(t, err)
	return scriptPath
}

func setupStdioRestartClient(t *testing.T, logger *zap.Logger, scriptPath string) *StdioClient {
	t.Helper()
	config := StdioClientConfig{
		Command: []string{"python3", scriptPath},
		Timeout: 3 * time.Second,
		Process: ProcessConfig{
			MaxRestarts:  2,
			RestartDelay: 500 * time.Millisecond,
			KillTimeout:  1 * time.Second,
		},
		HealthCheck: HealthCheckConfig{
			Enabled:  true,
			Interval: 100 * time.Millisecond,
			Timeout:  1 * time.Second,
		},
	}

	client, err := NewStdioClient("restart-client", "stdio://python3 "+scriptPath, config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = client.Connect(ctx)
	require.NoError(t, err)

	return client
}

func cleanupStdioRestartClient(t *testing.T, client *StdioClient) {
	t.Helper()
	ctx := context.Background()
	err := client.Close(ctx)
	require.NoError(t, err)
}

func runStdioRestartTest(t *testing.T, client *StdioClient) {
	t.Helper()
	ctx := context.Background()

	time.Sleep(2 * constants.TestSleepShort)

	sendStdioNormalRequest(t, client, ctx)
	triggerStdioProcessCrash(t, client, ctx)
	waitForStdioRestart(t)
}

func sendStdioNormalRequest(t *testing.T, client *StdioClient, ctx context.Context) {
	t.Helper()
	req := &mcp.Request{
		JSONRPC: constants.TestJSONRPCVersion,
		Method:  "normal",
		ID:      "restart-test-1",
	}

	_, err := client.SendRequest(ctx, req)
	require.NoError(t, err)
}

func triggerStdioProcessCrash(t *testing.T, client *StdioClient, ctx context.Context) {
	t.Helper()
	req := &mcp.Request{
		JSONRPC: constants.TestJSONRPCVersion,
		Method:  "crash",
		ID:      "restart-test-2",
	}

	_, _ = client.SendRequest(ctx, req)
}

func waitForStdioRestart(t *testing.T) {
	t.Helper()
	time.Sleep(2 * time.Second)
}

// TestStdioClientPerformanceOptimizations tests stdio-specific performance features.
func TestStdioClientPerformanceOptimizations(t *testing.T) { 
	logger := zaptest.NewLogger(t)
	testCases := createStdioPerformanceTestCases()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			runStdioPerformanceOptimizationTest(t, logger, tc)
		})
	}
}

func createStdioPerformanceTestCases() []struct {
	name   string
	config StdioPerformanceConfig
} {
	return []struct {
		name   string
		config StdioPerformanceConfig
	}{
		{
			name: "large buffers with buffered IO",
			config: StdioPerformanceConfig{
				StdinBufferSize:  128 * 1024,
				StdoutBufferSize: 128 * 1024,
				EnableBufferedIO: true,
				ReuseEncoders:    true,
			},
		},
		{
			name: "small buffers direct IO",
			config: StdioPerformanceConfig{
				StdinBufferSize:  4 * 1024,
				StdoutBufferSize: 4 * 1024,
				EnableBufferedIO: false,
				ReuseEncoders:    false,
			},
		},
		{
			name: "optimized settings",
			config: StdioPerformanceConfig{
				StdinBufferSize:  64 * 1024,
				StdoutBufferSize: 64 * 1024,
				EnableBufferedIO: true,
				ReuseEncoders:    true,
				ProcessPriority:  -5,
			},
		},
	}
}

func runStdioPerformanceOptimizationTest(t *testing.T, logger *zap.Logger, tc struct {
	name   string
	config StdioPerformanceConfig
}) {
	t.Helper()
	scriptPath := createStdioPerformanceScript(t)
	client := setupStdioPerformanceClient(t, logger, scriptPath, tc.config)
	defer cleanupStdioPerformanceClient(t, client)

	connectTime := measureStdioConnectTime(t, client)
	time.Sleep(100 * time.Millisecond)

	requestTimes := measureStdioRequestPerformance(t, client)
	analyzeStdioPerformanceResults(t, tc.name, connectTime, requestTimes)
}

func createStdioPerformanceScript(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "perf_server.py")
	scriptContent := `#!/usr/bin/env python3
import json
import sys
import time

def main():
    start_time = time.time()
    request_count = 0
    
    for line in sys.stdin:
        try:
            request = json.loads(line.strip())
            request_count += 1
            
            response = {
                "jsonrpc": "2.0",
                "id": request.get("id"),
                "result": {
                    "request_count": request_count,
                    "uptime": time.time() - start_time,
                    "method": request.get("method")
                }
            }
            print(json.dumps(response))
            sys.stdout.flush()
        except Exception as e:
            error_response = {
                "jsonrpc": "2.0",
                "id": request.get("id") if "request" in locals() else None,
                "error": {"code": -32603, "message": str(e)}
            }
            print(json.dumps(error_response))
            sys.stdout.flush()

if __name__ == "__main__":
    main()
`

	err := os.WriteFile(scriptPath, []byte(scriptContent), 0o755)
	require.NoError(t, err)
	return scriptPath
}

func setupStdioPerformanceClient(t *testing.T, logger *zap.Logger, scriptPath string, perfConfig StdioPerformanceConfig) *StdioClient {
	t.Helper()
	config := StdioClientConfig{
		Command:     []string{"python3", scriptPath},
		Timeout:     5 * time.Second,
		Performance: perfConfig,
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
	}

	client, err := NewStdioClient("perf-client", "stdio://python3 "+scriptPath, config, logger)
	require.NoError(t, err)
	return client
}

func cleanupStdioPerformanceClient(t *testing.T, client *StdioClient) {
	t.Helper()
	ctx := context.Background()
	err := client.Close(ctx)
	require.NoError(t, err)
}

func measureStdioConnectTime(t *testing.T, client *StdioClient) time.Duration {
	t.Helper()
	ctx := context.Background()
	start := time.Now()
	err := client.Connect(ctx)
	require.NoError(t, err)
	return time.Since(start)
}

func measureStdioRequestPerformance(t *testing.T, client *StdioClient) []time.Duration {
	t.Helper()
	const numRequests = 20
	ctx := context.Background()
	requestTimes := make([]time.Duration, numRequests)

	for i := 0; i < numRequests; i++ {
		req := &mcp.Request{
			JSONRPC: constants.TestJSONRPCVersion,
			Method:  fmt.Sprintf("perf_test_%d", i),
			ID:      fmt.Sprintf("perf-%d", i),
		}

		start := time.Now()
		resp, err := client.SendRequest(ctx, req)
		requestTimes[i] = time.Since(start)

		require.NoError(t, err)
		assert.NotNil(t, resp)
	}

	return requestTimes
}

func analyzeStdioPerformanceResults(t *testing.T, testName string, connectTime time.Duration, requestTimes []time.Duration) {
	t.Helper()
	assert.Less(t, connectTime, 3*time.Second, "Connection should be reasonably fast: %v", connectTime)

	var totalRequestTime time.Duration
	for _, reqTime := range requestTimes {
		totalRequestTime += reqTime
		assert.Less(t, reqTime, 2*time.Second, "Request should be reasonably fast: %v", reqTime)
	}

	avgRequestTime := totalRequestTime / time.Duration(len(requestTimes))

	t.Logf("Performance stats for %s:", testName)
	t.Logf("  Connect time: %v", connectTime)
	t.Logf("  Avg request time: %v", avgRequestTime)
	t.Logf("  Total request time: %v", totalRequestTime)
}

// TestStdioClientConcurrentOperations tests concurrent request handling.
func TestStdioClientConcurrentOperations(t *testing.T) { 
	logger := zaptest.NewLogger(t)
	scriptPath := createStdioConcurrentScript(t)
	
	client := setupStdioConcurrentClient(t, logger, scriptPath)
	defer cleanupStdioConcurrentClient(t, client)

	runStdioConcurrentOperationsTest(t, client)
}

func createStdioConcurrentScript(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "concurrent_server.py")
	scriptContent := `#!/usr/bin/env python3
import json
import sys
import threading
import time

class ConcurrentEchoServer:
    def __init__(self):
        self.request_count = 0
        self.lock = threading.Lock()
    
    def handle_request(self, line):
        with self.lock:
            self.request_count += 1
            current_count = self.request_count
        
        try:
            request = json.loads(line.strip())
            response = {
                "jsonrpc": "2.0",
                "id": request.get("id"),
                "result": {
                    "request_count": current_count,
                    "method": request.get("method"),
                    "timestamp": time.time()
                }
            }
            return json.dumps(response)
        except Exception as e:
            error_response = {
                "jsonrpc": "2.0",
                "id": request.get("id") if "request" in locals() else None,
                "error": {"code": -32603, "message": str(e)}
            }
            return json.dumps(error_response)

def main():
    server = ConcurrentEchoServer()
    for line in sys.stdin:
        response = server.handle_request(line)
        print(response)
        sys.stdout.flush()

if __name__ == "__main__":
    main()
`

	err := os.WriteFile(scriptPath, []byte(scriptContent), 0o755)
	require.NoError(t, err)
	return scriptPath
}

func setupStdioConcurrentClient(t *testing.T, logger *zap.Logger, scriptPath string) *StdioClient {
	t.Helper()
	config := StdioClientConfig{
		Command: []string{"python3", scriptPath},
		Timeout: 10 * time.Second,
		Performance: StdioPerformanceConfig{
			StdinBufferSize:  128 * 1024,
			StdoutBufferSize: 128 * 1024,
			EnableBufferedIO: true,
			ReuseEncoders:    true,
		},
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
	}

	client, err := NewStdioClient("concurrent-client", "stdio://python3 "+scriptPath, config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = client.Connect(ctx)
	require.NoError(t, err)

	time.Sleep(2 * constants.TestSleepShort)
	return client
}

func cleanupStdioConcurrentClient(t *testing.T, client *StdioClient) {
	t.Helper()
	ctx := context.Background()
	err := client.Close(ctx)
	require.NoError(t, err)
}

func runStdioConcurrentOperationsTest(t *testing.T, client *StdioClient) {
	t.Helper()
	const (
		numGoroutines        = 8
		requestsPerGoroutine = 3
	)

	errChan, responseChan := createStdioConcurrentChannels(numGoroutines, requestsPerGoroutine)
	
	var wg sync.WaitGroup
	ctx := context.Background()

	launchStdioConcurrentWorkers(t, &wg, client, ctx, numGoroutines, requestsPerGoroutine, errChan, responseChan)
	waitForStdioConcurrentCompletion(t, &wg)

	errors, responses := collectStdioConcurrentResults(errChan, responseChan)
	verifyStdioConcurrentResults(t, client, errors, responses, numGoroutines, requestsPerGoroutine)
}

func createStdioConcurrentChannels(numGoroutines, requestsPerGoroutine int) (chan error, chan *mcp.Response) {
	errChan := make(chan error, numGoroutines*requestsPerGoroutine)
	responseChan := make(chan *mcp.Response, numGoroutines*requestsPerGoroutine)
	return errChan, responseChan
}

func launchStdioConcurrentWorkers(t *testing.T, wg *sync.WaitGroup, client *StdioClient, ctx context.Context,
	numGoroutines, requestsPerGoroutine int, errChan chan error, responseChan chan *mcp.Response) {
	
	for g := 0; g < numGoroutines; g++ {
		wg.Add(1)
		go runStdioConcurrentWorker(wg, client, ctx, g, requestsPerGoroutine, errChan, responseChan)
	}
}

func runStdioConcurrentWorker(wg *sync.WaitGroup, client *StdioClient, ctx context.Context,
	goroutineID, requestsPerGoroutine int, errChan chan error, responseChan chan *mcp.Response) {
	
	defer wg.Done()

	for r := 0; r < requestsPerGoroutine; r++ {
		req := &mcp.Request{
			JSONRPC: constants.TestJSONRPCVersion,
			Method:  "concurrent_test",
			ID:      fmt.Sprintf("concurrent-%d-%d", goroutineID, r),
		}

		resp, err := client.SendRequest(ctx, req)
		if err != nil {
			errChan <- err
			return
		}

		responseChan <- resp
	}
}

func waitForStdioConcurrentCompletion(t *testing.T, wg *sync.WaitGroup) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(30 * time.Second):
		t.Fatal("Concurrent operations timed out")
	}
}

func collectStdioConcurrentResults(errChan chan error, responseChan chan *mcp.Response) ([]error, []*mcp.Response) {
	close(errChan)
	close(responseChan)

	var errors []error
	for err := range errChan {
		errors = append(errors, err)
	}

	var responses []*mcp.Response
	for resp := range responseChan {
		responses = append(responses, resp)
	}

	return errors, responses
}

func verifyStdioConcurrentResults(t *testing.T, client *StdioClient, errors []error, responses []*mcp.Response,
	numGoroutines, requestsPerGoroutine int) {
	
	t.Helper()
	expectedCount := numGoroutines * requestsPerGoroutine

	assert.Empty(t, errors, "No errors should occur during concurrent operations")
	assert.Len(t, responses, expectedCount, "All requests should receive responses")

	metrics := client.GetMetrics()
	assert.Equal(t, uint64(expectedCount), metrics.RequestCount)
	assert.Equal(t, uint64(0), metrics.ErrorCount)
	assert.True(t, metrics.IsHealthy)
}

// TestStdioClientMemoryOptimization tests memory optimization features.
func TestStdioClientMemoryOptimization(t *testing.T) { 
	logger := zaptest.NewLogger(t)
	scriptPath := createStdioMemoryScript(t)
	
	client := setupStdioMemoryOptimizedClient(t, logger, scriptPath)
	defer cleanupStdioMemoryOptimizedClient(t, client)

	runStdioMemoryOptimizationTest(t, client)
}

func createStdioMemoryScript(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "memory_server.py")
	scriptContent := `#!/usr/bin/env python3
import json
import sys

def main():
    for line in sys.stdin:
        try:
            request = json.loads(line.strip())
            response = {
                "jsonrpc": "2.0",
                "id": request.get("id"),
                "result": {"echo": request.get("method", "unknown")}
            }
            print(json.dumps(response))
            sys.stdout.flush()
        except Exception as e:
            error_response = {
                "jsonrpc": "2.0",
                "id": request.get("id") if "request" in locals() else None,
                "error": {"code": -32603, "message": str(e)}
            }
            print(json.dumps(error_response))
            sys.stdout.flush()

if __name__ == "__main__":
    main()
`

	err := os.WriteFile(scriptPath, []byte(scriptContent), 0o755)
	require.NoError(t, err)
	return scriptPath
}

func setupStdioMemoryOptimizedClient(t *testing.T, logger *zap.Logger, scriptPath string) *StdioClient {
	t.Helper()
	memoryOptimizer := createStdioMemoryOptimizer(logger)
	config := createStdioMemoryOptimizedConfig(scriptPath)

	client, err := NewStdioClientWithMemoryOptimizer(
		"memory-client",
		"stdio://python3 "+scriptPath,
		config,
		logger,
		memoryOptimizer,
	)
	require.NoError(t, err)

	ctx := context.Background()
	err = client.Connect(ctx)
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)
	return client
}

func createStdioMemoryOptimizer(logger *zap.Logger) *MemoryOptimizer {
	return NewMemoryOptimizer(MemoryOptimizationConfig{
		EnableObjectPooling: true,
		BufferPoolConfig: BufferPoolConfig{
			Enabled:          true,
			InitialSize:      4096,
			MaxSize:          64 * 1024,
			MaxPooledBuffers: 10,
		},
		JSONPoolConfig: JSONPoolConfig{
			Enabled:          true,
			MaxPooledObjects: 5,
		},
	}, logger)
}

func createStdioMemoryOptimizedConfig(scriptPath string) StdioClientConfig {
	return StdioClientConfig{
		Command: []string{"python3", scriptPath},
		Timeout: 5 * time.Second,
		Performance: StdioPerformanceConfig{
			EnableBufferedIO: true,
			ReuseEncoders:    true,
		},
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
	}
}

func cleanupStdioMemoryOptimizedClient(t *testing.T, client *StdioClient) {
	t.Helper()
	ctx := context.Background()
	err := client.Close(ctx)
	require.NoError(t, err)
}

func runStdioMemoryOptimizationTest(t *testing.T, client *StdioClient) {
	t.Helper()
	const numRequests = 50
	ctx := context.Background()

	for i := 0; i < numRequests; i++ {
		req := &mcp.Request{
			JSONRPC: constants.TestJSONRPCVersion,
			Method:  fmt.Sprintf("memory_test_%d", i),
			ID:      fmt.Sprintf("mem-%d", i),
		}

		resp, err := client.SendRequest(ctx, req)
		require.NoError(t, err)
		assert.NotNil(t, resp)
	}

	verifyStdioMemoryOptimizationMetrics(t, client, numRequests)
}

func verifyStdioMemoryOptimizationMetrics(t *testing.T, client *StdioClient, expectedRequests int) {
	t.Helper()
	metrics := client.GetMetrics()
	assert.Equal(t, uint64(expectedRequests), metrics.RequestCount)
	assert.Equal(t, uint64(0), metrics.ErrorCount)
}

// TestStdioClientLargePayloads tests handling of large request/response payloads.
func TestStdioClientLargePayloads(t *testing.T) { 
	logger := zaptest.NewLogger(t)
	scriptPath := createStdioLargePayloadScript(t)
	
	client := setupStdioLargePayloadClient(t, logger, scriptPath)
	defer cleanupStdioLargePayloadClient(t, client)

	runStdioLargePayloadTest(t, client)
}

func createStdioLargePayloadScript(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "large_payload_server.py")
	scriptContent := `#!/usr/bin/env python3
import json
import sys

def main():
    for line in sys.stdin:
        try:
            request = json.loads(line.strip())
            
            # Create a large response payload
            large_data = "x" * 50000  # 50KB of data
            
            response = {
                "jsonrpc": "2.0",
                "id": request.get("id"),
                "result": {
                    "method": request.get("method"),
                    "large_data": large_data,
                    "size": len(large_data)
                }
            }
            print(json.dumps(response))
            sys.stdout.flush()
        except Exception as e:
            error_response = {
                "jsonrpc": "2.0",
                "id": request.get("id") if "request" in locals() else None,
                "error": {"code": -32603, "message": str(e)}
            }
            print(json.dumps(error_response))
            sys.stdout.flush()

if __name__ == "__main__":
    main()
`

	err := os.WriteFile(scriptPath, []byte(scriptContent), 0o755)
	require.NoError(t, err)
	return scriptPath
}

func setupStdioLargePayloadClient(t *testing.T, logger *zap.Logger, scriptPath string) *StdioClient {
	t.Helper()
	config := StdioClientConfig{
		Command:       []string{"python3", scriptPath},
		Timeout:       10 * time.Second,
		MaxBufferSize: 1024 * 1024, // 1MB buffer
		Performance: StdioPerformanceConfig{
			StdinBufferSize:  256 * 1024,
			StdoutBufferSize: 256 * 1024,
			EnableBufferedIO: true,
		},
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
	}

	client, err := NewStdioClient("large-payload-client", "stdio://python3 "+scriptPath, config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = client.Connect(ctx)
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)
	return client
}

func cleanupStdioLargePayloadClient(t *testing.T, client *StdioClient) {
	t.Helper()
	ctx := context.Background()
	err := client.Close(ctx)
	require.NoError(t, err)
}

func runStdioLargePayloadTest(t *testing.T, client *StdioClient) {
	t.Helper()
	ctx := context.Background()
	
	largePayload := strings.Repeat("test_data_", 1000) // ~10KB payload
	req := &mcp.Request{
		JSONRPC: constants.TestJSONRPCVersion,
		Method:  "large_payload_test",
		Params:  map[string]interface{}{"data": largePayload},
		ID:      "large-payload-test",
	}

	resp, err := client.SendRequest(ctx, req)
	require.NoError(t, err)
	assert.NotNil(t, resp)

	verifyStdioLargePayloadResponse(t, resp)
}

func verifyStdioLargePayloadResponse(t *testing.T, resp *mcp.Response) {
	t.Helper()
	result, ok := resp.Result.(map[string]interface{})
	require.True(t, ok)

	largeData, ok := result["large_data"].(string)
	require.True(t, ok)
	assert.Len(t, largeData, 50000, "Should receive large response payload")

	size, ok := result["size"].(float64)
	require.True(t, ok)
	assert.InEpsilon(t, float64(50000), size, 0.001)
}

// TestStdioClientErrorRecovery tests error recovery and process management.
func TestStdioClientErrorRecovery(t *testing.T) { 
	logger := zaptest.NewLogger(t)
	scriptPath := createStdioErrorRecoveryScript(t)
	
	client := setupStdioErrorRecoveryClient(t, logger, scriptPath)
	defer cleanupStdioErrorRecoveryClient(t, client)

	runStdioErrorRecoveryTest(t, client)
}

func createStdioErrorRecoveryScript(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	scriptPath := filepath.Join(tmpDir, "error_server.py")
	scriptContent := `#!/usr/bin/env python3
import json
import sys
import random

def main():
    request_count = 0
    for line in sys.stdin:
        try:
            request_count += 1
            request = json.loads(line.strip())
            
            method = request.get("method", "")
            
            if method == "random_error" and random.random() < 0.3:
                # 30% chance of error
                raise Exception("Random server error")
            elif method == "invalid_json_response":
                # Send invalid JSON
                print("{invalid json}")
                sys.stdout.flush()
                continue
            elif method == "stderr_message":
                # Send message to stderr
                sys.stderr.write(f"Error message for request {request_count}\n")
                sys.stderr.flush()
            
            response = {
                "jsonrpc": "2.0",
                "id": request.get("id"),
                "result": {
                    "method": method,
                    "request_count": request_count,
                    "status": "success"
                }
            }
            print(json.dumps(response))
            sys.stdout.flush()
            
        except Exception as e:
            error_response = {
                "jsonrpc": "2.0",
                "id": request.get("id") if "request" in locals() else None,
                "error": {"code": -32603, "message": str(e)}
            }
            print(json.dumps(error_response))
            sys.stdout.flush()

if __name__ == "__main__":
    main()
`

	err := os.WriteFile(scriptPath, []byte(scriptContent), 0o755)
	require.NoError(t, err)
	return scriptPath
}

func setupStdioErrorRecoveryClient(t *testing.T, logger *zap.Logger, scriptPath string) *StdioClient {
	t.Helper()
	config := StdioClientConfig{
		Command: []string{"python3", scriptPath},
		Timeout: 5 * time.Second,
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
	}

	client, err := NewStdioClient("error-recovery-client", "stdio://python3 "+scriptPath, config, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = client.Connect(ctx)
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)
	return client
}

func cleanupStdioErrorRecoveryClient(t *testing.T, client *StdioClient) {
	t.Helper()
	ctx := context.Background()
	err := client.Close(ctx)
	require.NoError(t, err)
}

func runStdioErrorRecoveryTest(t *testing.T, client *StdioClient) {
	t.Helper()
	testCases := createStdioErrorTestCases()
	successCount, errorCount := executeStdioErrorTestCases(t, client, testCases)
	
	verifyStdioErrorRecoveryResults(t, client, successCount, errorCount)
}

func createStdioErrorTestCases() []struct {
	method      string
	expectError bool
} {
	return []struct {
		method      string
		expectError bool
	}{
		{"normal_request", false},
		{"stderr_message", false}, // Stderr should not cause request failure
		{"random_error", true},    // May cause error due to server exception
		{"normal_request", false}, // Should recover
	}
}

func executeStdioErrorTestCases(t *testing.T, client *StdioClient, testCases []struct {
	method      string
	expectError bool
}) (int, int) {
	t.Helper()
	var successCount, errorCount int
	ctx := context.Background()

	for i, tc := range testCases {
		req := &mcp.Request{
			JSONRPC: constants.TestJSONRPCVersion,
			Method:  tc.method,
			ID:      fmt.Sprintf("error-test-%d", i),
		}

		_, err := client.SendRequest(ctx, req)
		if err != nil {
			errorCount++
		} else {
			successCount++
		}
	}

	return successCount, errorCount
}

func verifyStdioErrorRecoveryResults(t *testing.T, client *StdioClient, successCount, errorCount int) {
	t.Helper()
	assert.Positive(t, successCount, "Should have at least some successful requests")

	metrics := client.GetMetrics()
	t.Logf("Final metrics - Requests: %d, Errors: %d", metrics.RequestCount, metrics.ErrorCount)
}

// Enhanced benchmark tests.

func BenchmarkStdioClientSendRequest(b *testing.B) {
	logger := zaptest.NewLogger(b)

	// Create a simple echo server.
	tmpDir := b.TempDir()
	scriptPath := filepath.Join(tmpDir, "echo_server.py")
	scriptContent := constants.TestPythonEchoScript

	err := os.WriteFile(scriptPath, []byte(scriptContent), 0o755) 
	require.NoError(b, err)

	config := StdioClientConfig{
		Command: []string{"python3", scriptPath},
		Timeout: 5 * time.Second,
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
	}

	client, err := NewStdioClient("bench-client", "stdio://python3 "+scriptPath, config, logger)
	require.NoError(b, err)

	ctx := context.Background()
	err = client.Connect(ctx)
	require.NoError(b, err)

	defer func() {
		err := client.Close(ctx)
		require.NoError(b, err)
	}()

	// Wait for the server to be ready.
	time.Sleep(100 * time.Millisecond)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			req := &mcp.Request{
				JSONRPC: constants.TestJSONRPCVersion,
				Method:  "benchmark",
				ID:      fmt.Sprintf("bench-%d", i),
			}
			i++

			_, err := client.SendRequest(ctx, req)
			if err != nil {
				b.Errorf("Request failed: %v", err)
			}
		}
	})
}

// BenchmarkStdioClientConcurrency benchmarks concurrent request handling.
func BenchmarkStdioClientConcurrency(b *testing.B) {
	logger := zaptest.NewLogger(b)
	scriptPath := createStdioConcurrentBenchScript(b)
	
	client := setupStdioConcurrentBenchClient(b, logger, scriptPath)
	defer cleanupStdioConcurrentBenchClient(b, client)

	runStdioConcurrentBenchmark(b, client)
}

func createStdioConcurrentBenchScript(b *testing.B) string {
	b.Helper()
	tmpDir := b.TempDir()
	scriptPath := filepath.Join(tmpDir, "concurrent_bench_server.py")
	scriptContent := `#!/usr/bin/env python3
import json
import sys
import time

def main():
    for line in sys.stdin:
        try:
            request = json.loads(line.strip())
            response = {
                "jsonrpc": "2.0",
                "id": request.get("id"),
                "result": {
                    "echo": request.get("method", "unknown"),
                    "timestamp": time.time()
                }
            }
            print(json.dumps(response))
            sys.stdout.flush()
        except:
            pass

if __name__ == "__main__":
    main()
`

	err := os.WriteFile(scriptPath, []byte(scriptContent), 0o755)
	require.NoError(b, err)
	return scriptPath
}

func setupStdioConcurrentBenchClient(b *testing.B, logger *zap.Logger, scriptPath string) *StdioClient {
	b.Helper()
	config := StdioClientConfig{
		Command: []string{"python3", scriptPath},
		Timeout: 10 * time.Second,
		Performance: StdioPerformanceConfig{
			StdinBufferSize:  256 * 1024,
			StdoutBufferSize: 256 * 1024,
			EnableBufferedIO: true,
			ReuseEncoders:    true,
		},
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
	}

	client, err := NewStdioClient("concurrent-bench-client", "stdio://python3 "+scriptPath, config, logger)
	require.NoError(b, err)

	ctx := context.Background()
	err = client.Connect(ctx)
	require.NoError(b, err)

	time.Sleep(100 * time.Millisecond)
	return client
}

func cleanupStdioConcurrentBenchClient(b *testing.B, client *StdioClient) {
	b.Helper()
	ctx := context.Background()
	err := client.Close(ctx)
	require.NoError(b, err)
}

func runStdioConcurrentBenchmark(b *testing.B, client *StdioClient) {
	b.Helper()
	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			req := &mcp.Request{
				JSONRPC: constants.TestJSONRPCVersion,
				Method:  "concurrent_benchmark",
				ID:      fmt.Sprintf("concurrent-bench-%d-%d", i, time.Now().UnixNano()),
			}
			i++

			_, err := client.SendRequest(ctx, req)
			if err != nil {
				b.Errorf("Concurrent request failed: %v", err)
			}
		}
	})
}

// BenchmarkStdioClientMemoryUsage benchmarks memory efficiency.
func BenchmarkStdioClientMemoryUsage(b *testing.B) {
	logger := zaptest.NewLogger(b)
	memoryOptimizer := createStdioBenchMemoryOptimizer(logger)
	scriptPath := createStdioMemoryBenchScript(b)
	
	client := setupStdioMemoryBenchClient(b, logger, scriptPath, memoryOptimizer)
	defer cleanupStdioMemoryBenchClient(b, client)

	runStdioMemoryBenchmark(b, client)
}

func createStdioBenchMemoryOptimizer(logger *zap.Logger) *MemoryOptimizer {
	return NewMemoryOptimizer(MemoryOptimizationConfig{
		EnableObjectPooling: true,
		BufferPoolConfig: BufferPoolConfig{
			Enabled:          true,
			InitialSize:      4096,
			MaxSize:          128 * 1024,
			MaxPooledBuffers: 20,
		},
		JSONPoolConfig: JSONPoolConfig{
			Enabled:          true,
			MaxPooledObjects: 10,
		},
	}, logger)
}

func createStdioMemoryBenchScript(b *testing.B) string {
	b.Helper()
	tmpDir := b.TempDir()
	scriptPath := filepath.Join(tmpDir, "memory_bench_server.py")
	scriptContent := constants.TestPythonEchoScript

	err := os.WriteFile(scriptPath, []byte(scriptContent), 0o755)
	require.NoError(b, err)
	return scriptPath
}

func setupStdioMemoryBenchClient(b *testing.B, logger *zap.Logger, scriptPath string, memoryOptimizer *MemoryOptimizer) *StdioClient {
	b.Helper()
	config := StdioClientConfig{
		Command: []string{"python3", scriptPath},
		Timeout: 5 * time.Second,
		Performance: StdioPerformanceConfig{
			EnableBufferedIO: true,
			ReuseEncoders:    true,
		},
		HealthCheck: HealthCheckConfig{
			Enabled: false,
		},
	}

	client, err := NewStdioClientWithMemoryOptimizer(
		"memory-bench-client",
		"stdio://python3 "+scriptPath,
		config,
		logger,
		memoryOptimizer,
	)
	require.NoError(b, err)

	ctx := context.Background()
	err = client.Connect(ctx)
	require.NoError(b, err)

	time.Sleep(100 * time.Millisecond)
	return client
}

func cleanupStdioMemoryBenchClient(b *testing.B, client *StdioClient) {
	b.Helper()
	ctx := context.Background()
	err := client.Close(ctx)
	require.NoError(b, err)
}

func runStdioMemoryBenchmark(b *testing.B, client *StdioClient) {
	b.Helper()
	ctx := context.Background()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			req := &mcp.Request{
				JSONRPC: constants.TestJSONRPCVersion,
				Method:  "memory_benchmark",
				ID:      fmt.Sprintf("memory-bench-%d", i),
			}
			i++

			_, err := client.SendRequest(ctx, req)
			if err != nil {
				b.Errorf("Memory benchmark request failed: %v", err)
			}
		}
	})
}
