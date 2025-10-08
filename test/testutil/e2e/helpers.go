// Package e2e provides end-to-end testing utilities and helpers.
//
// E2E utilities allow flexible style
//

package e2e

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
)

const (
	// Test timeouts and intervals.
	defaultTestTimeout = 30 * time.Second
	httpClientTimeout  = 5 * time.Second

	// HTTP status codes.
	httpStatusServerError = 500

	// File permissions.
	dirPermissions  = 0o750
	filePermissions = 0o600
)

// TestConfig holds configuration for E2E tests.
type TestConfig struct {
	GatewayURL       string
	AuthToken        string
	TestTimeout      time.Duration
	RouterBinaryPath string
	RouterConfigPath string
	TempDir          string
	CleanupOnFailure bool
}

// DefaultTestConfig creates a default test configuration.
func DefaultTestConfig() *TestConfig {
	return &TestConfig{
		GatewayURL:       "wss://localhost:8443/ws",
		AuthToken:        "", // No default auth token
		TestTimeout:      defaultTestTimeout,
		RouterBinaryPath: "./bin/mcp-router",
		RouterConfigPath: "./test/configs/router.yaml",
		TempDir:          "",
		CleanupOnFailure: true,
	}
}

// TestEnvironment manages E2E test environment.
type TestEnvironment struct {
	Config    *TestConfig
	Logger    *zap.Logger
	TempDir   string
	processes []*os.Process
	cleanup   []func() error
}

// NewTestEnvironment creates a new test environment.
func NewTestEnvironment(t *testing.T, config *TestConfig) *TestEnvironment {
	t.Helper()

	logger := NewTestLogger()

	tempDir, err := os.MkdirTemp("", "mcp-e2e-*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}

	if config.TempDir == "" {
		config.TempDir = tempDir
	}

	return &TestEnvironment{
		Config:    config,
		Logger:    logger,
		TempDir:   tempDir,
		processes: make([]*os.Process, 0),
		cleanup:   make([]func() error, 0),
	}
}

// AddCleanup adds a cleanup function to be called on environment shutdown.
func (env *TestEnvironment) AddCleanup(cleanup func() error) {
	env.cleanup = append(env.cleanup, cleanup)
}

// StartRouter starts the MCP router process.
func (env *TestEnvironment) StartRouter() error {
	if env.Config.RouterBinaryPath == "" {
		return errors.New("router binary path not specified")
	}

	if env.Config.RouterConfigPath == "" {
		return errors.New("router config path not specified")
	}

	//nolint:gosec // Test router startup
	cmd := exec.CommandContext(context.Background(), env.Config.RouterBinaryPath,
		"--config", env.Config.RouterConfigPath)
	cmd.Env = append(os.Environ(), "MCP_AUTH_TOKEN="+env.Config.AuthToken)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start router: %w", err)
	}

	env.processes = append(env.processes, cmd.Process)
	env.AddCleanup(func() error {
		if cmd.Process != nil {
			return cmd.Process.Kill()
		}

		return nil
	})

	env.Logger.Info("Router started", zap.Int("pid", cmd.Process.Pid))

	return nil
}

// WaitForService waits for a service to become available.
func (env *TestEnvironment) WaitForService(url string, timeout time.Duration) error {
	client := &http.Client{
		Timeout: httpClientTimeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // Test environment only
		},
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)

		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode < httpStatusServerError {
				env.Logger.Info("Service became available", zap.String("url", url))

				return nil
			}
		}

		time.Sleep(1 * time.Second)
	}

	return fmt.Errorf("service at %s did not become available within %v", url, timeout)
}

// CreateTestFile creates a test file in the temp directory.
func (env *TestEnvironment) CreateTestFile(filename, content string) (string, error) {
	fullPath := filepath.Join(env.TempDir, filename)

	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, dirPermissions); err != nil {
		return "", fmt.Errorf("failed to create directories: %w", err)
	}

	if err := os.WriteFile(fullPath, []byte(content), filePermissions); err != nil {
		return "", fmt.Errorf("failed to write test file: %w", err)
	}

	return fullPath, nil
}

// ReadTestFile reads a test file from the temp directory.
func (env *TestEnvironment) ReadTestFile(filename string) (string, error) {
	fullPath := filepath.Join(env.TempDir, filename)

	content, err := os.ReadFile(fullPath) //nolint:gosec // Test file reading
	if err != nil {
		return "", fmt.Errorf("failed to read test file: %w", err)
	}

	return string(content), nil
}

// HTTPClient creates an HTTP client suitable for E2E tests.
func (env *TestEnvironment) HTTPClient() *http.Client {
	return &http.Client{
		Timeout: env.Config.TestTimeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // Test environment only
		},
	}
}

// MakeRequest makes an HTTP request to the test environment.
func (env *TestEnvironment) MakeRequest(method, path string, body io.Reader) (*http.Response, error) {
	url := env.Config.GatewayURL + path

	req, err := http.NewRequestWithContext(context.Background(), method, url, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if env.Config.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+env.Config.AuthToken)
	}

	client := env.HTTPClient()

	return client.Do(req)
}

// RunCommand executes a command in the test environment.
func (env *TestEnvironment) RunCommand(name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(context.Background(), name, args...)
	cmd.Dir = env.TempDir

	output, err := cmd.Output()
	if err != nil {
		env.Logger.Error("Command failed",
			zap.String("command", name),
			zap.Strings("args", args),
			zap.Error(err))

		return nil, fmt.Errorf("command failed: %w", err)
	}

	return output, nil
}

// RunCommandWithTimeout executes a command with timeout.
func (env *TestEnvironment) RunCommandWithTimeout(timeout time.Duration, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = env.TempDir

	return cmd.Output()
}

// CheckServiceHealth performs a basic health check on a service.
func (env *TestEnvironment) CheckServiceHealth(serviceURL string) error {
	client := env.HTTPClient()

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, serviceURL+"/health", nil)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("health check request failed: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("service unhealthy: status %d", resp.StatusCode)
	}

	return nil
}

// WaitForProcessToStop waits for a process to stop.
func (env *TestEnvironment) WaitForProcessToStop(process *os.Process, timeout time.Duration) error {
	done := make(chan error, 1)

	go func() {
		_, err := process.Wait()
		done <- err
	}()

	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		return fmt.Errorf("process did not stop within %v", timeout)
	}
}

// Cleanup cleans up the test environment.
func (env *TestEnvironment) Cleanup() error {
	var errors []error

	// Run cleanup functions in reverse order
	for i := len(env.cleanup) - 1; i >= 0; i-- {
		if err := env.cleanup[i](); err != nil {
			errors = append(errors, err)
		}
	}

	// Kill any remaining processes
	for _, process := range env.processes {
		if process != nil {
			_ = process.Kill()
		}
	}

	// Remove temp directory
	if env.TempDir != "" {
		if err := os.RemoveAll(env.TempDir); err != nil {
			errors = append(errors, err)
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("cleanup failed with %d errors: %v", len(errors), errors)
	}

	return nil
}

// TestRunner provides test execution utilities.
type TestRunner struct {
	env *TestEnvironment
	t   *testing.T
}

// NewTestRunner creates a new test runner.
func NewTestRunner(t *testing.T, env *TestEnvironment) *TestRunner {
	t.Helper()

	return &TestRunner{
		env: env,
		t:   t,
	}
}

// RunTest executes a test with proper setup and cleanup.
func (tr *TestRunner) RunTest(name string, testFunc func(*TestEnvironment) error) {
	tr.t.Run(name, func(t *testing.T) {
		defer func() {
			if err := tr.env.Cleanup(); err != nil {
				t.Logf("Cleanup error: %v", err)
			}
		}()

		if err := testFunc(tr.env); err != nil {
			t.Fatalf("Test failed: %v", err)
		}
	})
}

// AssertResponse checks HTTP response expectations.
func (tr *TestRunner) AssertResponse(resp *http.Response, expectedStatus int) {
	if resp.StatusCode != expectedStatus {
		tr.t.Errorf("Expected status %d, got %d", expectedStatus, resp.StatusCode)
	}
}

// AssertNoError fails the test if error is not nil.
func (tr *TestRunner) AssertNoError(err error) {
	if err != nil {
		tr.t.Fatalf("Unexpected error: %v", err)
	}
}

// AssertError fails the test if error is nil.
func (tr *TestRunner) AssertError(err error) {
	if err == nil {
		tr.t.Fatal("Expected error but got none")
	}
}

// AssertContains fails the test if haystack doesn't contain needle.
func (tr *TestRunner) AssertContains(haystack, needle string) {
	if !strings.Contains(haystack, needle) {
		tr.t.Errorf("Expected %q to contain %q", haystack, needle)
	}
}

// LoadTestData loads test data from a file.
func LoadTestData(filename string) ([]byte, error) {
	return os.ReadFile(filename) //nolint:gosec // Test file reading
}

// DirectGatewayRouter provides a simple implementation of RouterInterface
// that connects directly to the gateway without a router process.
type DirectGatewayRouter struct{}

// SendRequestAndWait sends a request directly to the gateway via WebSocket.
func (dgr *DirectGatewayRouter) SendRequestAndWait(req MCPRequest, timeout time.Duration) ([]byte, error) {
	// For now, return a simple error indicating this is not yet implemented
	// K8s tests will need to be updated to work differently or this needs full implementation
	return nil, errors.New("DirectGatewayRouter not yet implemented - K8s tests need RouterController")
}

// TestSuite provides a higher-level interface for E2E testing.
// This is a compatibility wrapper around TestEnvironment for K8s tests.
type TestSuite struct {
	env    *TestEnvironment
	client *MCPClient
	router *RouterController
	t      *testing.T
}

// NewTestSuite creates a new test suite with the given configuration.
func NewTestSuite(t *testing.T, config *TestConfig) *TestSuite {
	t.Helper()

	env := NewTestEnvironment(t, config)

	return &TestSuite{
		env: env,
		t:   t,
	}
}

// Setup initializes the test suite and creates the MCP client.
func (ts *TestSuite) Setup() error {
	return ts.SetupWithContext(context.Background())
}

// SetupWithContext initializes the test suite and creates the MCP client with context.
func (ts *TestSuite) SetupWithContext(ctx context.Context) error {
	// Create RouterController to manage the router process
	ts.router = NewRouterController(ts.t, ts.env.Config.GatewayURL)

	// Build and start the router
	if err := ts.router.BuildRouterWithContext(ctx); err != nil {
		return fmt.Errorf("failed to build router: %w", err)
	}

	if err := ts.router.Start(); err != nil {
		return fmt.Errorf("failed to start router: %w", err)
	}

	// Wait for router to become healthy
	if err := ts.router.WaitForHealthy(defaultTestTimeout); err != nil {
		return fmt.Errorf("router failed to become healthy: %w", err)
	}

	// Create MCP client using the router
	ts.client = NewMCPClient(ts.router, ts.env.Logger)

	return nil
}

// Teardown cleans up the test suite resources.
func (ts *TestSuite) Teardown() {
	if ts.router != nil {
		ts.router.Stop()
	}

	if err := ts.env.Cleanup(); err != nil {
		ts.t.Logf("Error during environment cleanup: %v", err)
	}
}

// GetClient returns the MCP client for making requests.
func (ts *TestSuite) GetClient() *MCPClient {
	return ts.client
}

// SaveTestData saves data to a test file.
func SaveTestData(filename string, data []byte) error {
	dir := filepath.Dir(filename)
	if err := os.MkdirAll(dir, dirPermissions); err != nil {
		return fmt.Errorf("failed to create directories: %w", err)
	}

	return os.WriteFile(filename, data, filePermissions)
}

// IsPortOpen checks if a port is open and accepting connections.
func IsPortOpen(host string, port int) bool {
	address := fmt.Sprintf("%s:%d", host, port)

	dialer := &net.Dialer{Timeout: 1 * time.Second}

	conn, err := dialer.DialContext(context.Background(), "tcp", address)
	if err != nil {
		return false
	}

	defer func() { _ = conn.Close() }()

	return true
}
