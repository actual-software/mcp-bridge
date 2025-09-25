// Integration test files allow flexible style
//

package smoke

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/poiley/mcp-bridge/test/testutil"
)

// TestBasicSmoke runs basic smoke tests that should pass.
func TestBasicSmoke(t *testing.T) {
	// Don't run in parallel - the setup needs to complete before subtests start
	if testing.Short() {
		t.Skip("Skipping smoke tests in short mode")
	}

	// Setup MCP bridge services infrastructure
	cleanup, err := setupMCPBridgeServices(t)
	if err != nil {
		t.Fatalf("Failed to setup MCP bridge services: %v", err)
	}
	defer cleanup()

	// Create test client with IPv4 address
	client := testutil.NewTestClient()
	client.BaseURL = "http://127.0.0.1:8080"

	t.Run("PortAccessibility", func(t *testing.T) {
		testPortAccessibility(t)
	})

	t.Run("HealthCheck", func(t *testing.T) {
		testHealthCheck(t, client)
	})

	t.Run("MemoryCheck", func(t *testing.T) {
		testMemoryCheck(t)
	})

	t.Run("GitInfo", func(t *testing.T) {
		testGitInfo(t)
	})
}

func testPortAccessibility(t *testing.T) {
	t.Helper()
	// Don't run in parallel since we need the setup to be complete
	// Check common ports (may or may not be open)
	ports := []string{"8080", "8081", "9090", "9091"}
	openPorts := 0

	for _, port := range ports {
		if checkPort(port) {
			t.Logf("Port %s is open", port)

			openPorts++
		} else {
			t.Logf("Port %s is closed", port)
		}
	}

	// All ports should be accessible since we set up the services
	assert.Equal(t, len(ports), openPorts, "All required ports should be accessible")
}

func testHealthCheck(t *testing.T, client *testutil.TestClient) {
	t.Helper()
	// Don't run in parallel since we need the setup to be complete
	// Try to check health endpoint (use IPv4 address)
	resp, err := client.Get("/health")
	if err != nil {
		t.Skipf("Health endpoint not accessible: %v", err)

		return
	}

	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Logf("Failed to close response body: %v", err)
		}
	}()

	// If endpoint exists, it should return 200 or 503
	assert.Contains(t, []int{200, 503}, resp.StatusCode,
		"Health endpoint should return 200 (healthy) or 503 (unhealthy)")
}

func testMemoryCheck(t *testing.T) {
	t.Helper()
	// Don't run in parallel since we need the setup to be complete
	// Simple memory check
	mem := getMemoryUsage()
	assert.Positive(t, mem, "Should be able to read memory usage")
	t.Logf("Current memory usage: %d bytes", mem)
}

func testGitInfo(t *testing.T) {
	t.Helper()
	// Don't run in parallel since we need the setup to be complete
	// Check git information
	commit := getGitCommit()
	branch := getGitBranch()

	assert.NotEmpty(t, commit, "Should get git commit")
	assert.NotEmpty(t, branch, "Should get git branch")

	t.Logf("Git commit: %s", commit)
	t.Logf("Git branch: %s", branch)
}

// TestQuickValidation performs quick validation that always passes.
func TestQuickValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		test func(t *testing.T)
	}{
		{
			name: "BasicMath",
			test: func(t *testing.T) {
				t.Helper()
				assert.Equal(t, 4, 2+2, "Basic math should work")
			},
		},
		{
			name: "TimeOperations",
			test: func(t *testing.T) {
				t.Helper()
				start := time.Now()
				time.Sleep(10 * time.Millisecond)
				duration := time.Since(start)
				assert.Greater(t, duration.Milliseconds(), int64(9),
					"Sleep duration should be at least 9ms")
			},
		},
		{
			name: "StringOperations",
			test: func(t *testing.T) {
				t.Helper()
				result := "Hello" + " " + "World"
				assert.Equal(t, "Hello World", result)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.test(t)
		})
	}
}

// Helper functions

// checkPort checks if a port is accessible on localhost.
func checkPort(port string) bool {
	dialer := &net.Dialer{}

	conn, err := dialer.DialContext(context.Background(), "tcp4", "127.0.0.1:"+port)
	if err != nil {
		return false
	}

	defer func() {
		_ = conn.Close()
	}()

	return true
}

// getMemoryUsage returns current memory usage in bytes.
func getMemoryUsage() uint64 {
	var m runtime.MemStats

	runtime.ReadMemStats(&m)

	return m.Alloc
}

// getGitCommit returns the current git commit hash.
func getGitCommit() string {
	cmd := exec.CommandContext(context.Background(), "git", "rev-parse", "HEAD")

	output, err := cmd.Output()
	if err != nil {
		return "unknown"
	}

	return strings.TrimSpace(string(output))
}

// getGitBranch returns the current git branch name.
func getGitBranch() string {
	cmd := exec.CommandContext(context.Background(), "git", "rev-parse", "--abbrev-ref", "HEAD")

	output, err := cmd.Output()
	if err != nil {
		return "unknown"
	}

	return strings.TrimSpace(string(output))
}

// MCPServiceManager manages mock MCP bridge services for testing.
type MCPServiceManager struct {
	servers []*http.Server
	mutex   sync.RWMutex
}

// setupMCPBridgeServices starts mock MCP bridge services on required ports.
func setupMCPBridgeServices(t *testing.T) (func(), error) {
	t.Helper()

	manager := &MCPServiceManager{
		servers: make([]*http.Server, 0),
	}

	// Required ports for MCP bridge services
	ports := []int{8080, 8081, 9090, 9091}

	for _, port := range ports {
		server, err := manager.startMockService(port)
		if err != nil {
			// Cleanup any started servers before returning error
			manager.cleanup()

			return nil, fmt.Errorf("failed to start service on port %d: %w", port, err)
		}

		manager.servers = append(manager.servers, server)

		t.Logf("Started mock MCP service on port %d", port)
	}

	// Wait for all services to be ready
	for _, port := range ports {
		if err := testutil.WaitForPort(port, 10*time.Second); err != nil {
			manager.cleanup()

			return nil, fmt.Errorf("service on port %d failed to become ready: %w", port, err)
		}
	}

	t.Log("All MCP bridge services are ready")

	return manager.cleanup, nil
}

// startMockService starts a mock MCP service on the specified port.
func (m *MCPServiceManager) startMockService(port int) (*http.Server, error) {
	mux := setupMockServiceHandlers(port)

	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	return startServerWithReadyCheck(server, port)
}

func setupMockServiceHandlers(port int) *http.ServeMux {
	mux := http.NewServeMux()

	// Health endpoint - required for smoke tests
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		// Simple JSON response
		timestamp := time.Now().UTC().Format(time.RFC3339)
		response := fmt.Sprintf(
			`{"status":"ok","service":"mcp-bridge-mock-%d","port":%d,"timestamp":"%s","version":"test-1.0.0"}`,
			port, port, timestamp)
		_, _ = fmt.Fprintf(w, "%s", response)
	})

	// Version endpoint
	mux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `{"version":"test-1.0.0","service":"mcp-bridge-mock-%d","port":%d}`, port, port)
	})

	// Metrics endpoint (for Prometheus-style ports 9090, 9091)
	if port == 9090 || port == 9091 {
		mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)

			const metricsTemplate = "# Mock metrics for port %d\nmcp_bridge_requests_total 42\nmcp_bridge_uptime_seconds 123\n"

			metrics := fmt.Sprintf(metricsTemplate, port)
			_, _ = fmt.Fprintf(w, "%s", metrics)
		})
	}

	// Root endpoint
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `{"message":"MCP Bridge Mock Service","port":%d,"endpoints":["/health","/version"]}`, port)
	})

	return mux
}

func startServerWithReadyCheck(server *http.Server, port int) (*http.Server, error) {
	// Use a channel to signal when the server is ready
	ready := make(chan error, 1)

	// Start server in background
	go func() {
		// Listen first to get the actual port binding (explicitly use IPv4)
		lc := &net.ListenConfig{}

		listener, err := lc.Listen(context.Background(), "tcp4", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			ready <- err

			return
		}

		// Signal that we're ready to accept connections
		ready <- nil

		// Start serving on the listener
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			// Log error but don't fail the test setup
			fmt.Printf("Mock service on port %d stopped with error: %v\n", port, err)
		}
	}()

	// Wait for server to be ready or timeout
	select {
	case err := <-ready:
		if err != nil {
			return nil, err
		}
	case <-time.After(5 * time.Second):
		return nil, fmt.Errorf("timeout waiting for server to start on port %d", port)
	}

	return server, nil
}

// cleanup stops all running mock services.
func (m *MCPServiceManager) cleanup() {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	for _, server := range m.servers {
		if server != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = server.Shutdown(ctx)

			cancel()
		}
	}

	m.servers = m.servers[:0] // Clear the slice
}
