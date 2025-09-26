// Package testutil provides utility functions and helpers for testing.
//

package testutil

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

const (
	// Test timeouts and intervals.
	defaultClientTimeout = 30 * time.Second
	httpClientTimeout    = 5 * time.Second
	retryDelay           = 100 * time.Millisecond

	// HTTP status codes.
	httpStatusServerError = 500
)

// TestClient provides HTTP client for testing.
type TestClient struct {
	BaseURL   string
	AuthToken string
	Client    *http.Client
}

// NewTestClient creates a new test client.
func NewTestClient() *TestClient {
	baseURL := os.Getenv("MCP_BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}

	return &TestClient{
		BaseURL: baseURL,
		Client: &http.Client{
			Timeout: defaultClientTimeout,
		},
	}
}

// SetAuthToken sets the authentication token.
func (c *TestClient) SetAuthToken(token string) {
	c.AuthToken = token
}

// Get performs a GET request.
func (c *TestClient) Get(path string) (*http.Response, error) {
	url := c.BaseURL + path

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create GET request: %w", err)
	}

	if c.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.AuthToken)
	}

	return c.Client.Do(req)
}

// Post performs a POST request with JSON body.
func (c *TestClient) Post(path string, body interface{}) (*http.Response, error) {
	var reqBody io.Reader

	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}

		reqBody = bytes.NewReader(jsonBody)
	}

	url := c.BaseURL + path

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create POST request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	if c.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.AuthToken)
	}

	return c.Client.Do(req)
}

// Put performs a PUT request with JSON body.
func (c *TestClient) Put(path string, body interface{}) (*http.Response, error) {
	var reqBody io.Reader

	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}

		reqBody = bytes.NewReader(jsonBody)
	}

	url := c.BaseURL + path

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPut, url, reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create PUT request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	if c.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.AuthToken)
	}

	return c.Client.Do(req)
}

// Delete performs a DELETE request.
func (c *TestClient) Delete(path string) (*http.Response, error) {
	url := c.BaseURL + path

	req, err := http.NewRequestWithContext(context.Background(), http.MethodDelete, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create DELETE request: %w", err)
	}

	if c.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.AuthToken)
	}

	return c.Client.Do(req)
}

// CheckPort checks if a port is available on localhost.
func CheckPort(port int) bool {
	address := fmt.Sprintf("localhost:%d", port)

	dialer := &net.Dialer{}

	conn, err := dialer.DialContext(context.Background(), "tcp", address)
	if err != nil {
		return false
	}

	defer func() { _ = conn.Close() }()

	return true
}

// WaitForPort waits for a port to become available with timeout.
func WaitForPort(port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if CheckPort(port) {
			return nil
		}

		time.Sleep(retryDelay)
	}

	return fmt.Errorf("port %d did not become available within %v", port, timeout)
}

// GetMemoryUsage returns current memory usage statistics.
func GetMemoryUsage() runtime.MemStats {
	var m runtime.MemStats

	runtime.ReadMemStats(&m)

	return m
}

// GetProcessMemory returns process memory usage in bytes.
func GetProcessMemory() uint64 {
	var m runtime.MemStats

	runtime.ReadMemStats(&m)

	return m.Sys
}

// RunCommand executes a shell command and returns its output.
func RunCommand(name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(context.Background(), name, args...)

	return cmd.Output()
}

// RunCommandWithTimeout executes a command with a timeout.
func RunCommandWithTimeout(timeout time.Duration, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)

	return cmd.Output()
}

// WaitForService waits for a service to be ready at the given URL.
func WaitForService(url string, timeout time.Duration) error {
	client := &http.Client{Timeout: httpClientTimeout}
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)

		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()

			if resp.StatusCode < httpStatusServerError {
				return nil
			}
		}

		time.Sleep(1 * time.Second)
	}

	return fmt.Errorf("service at %s did not become ready within %v", url, timeout)
}

// MockServer provides a simple mock HTTP server for testing.
type MockServer struct {
	Server   *http.Server
	Handlers map[string]http.HandlerFunc
}

// NewMockServer creates a new mock server.
func NewMockServer(port int) *MockServer {
	ms := &MockServer{
		Handlers: make(map[string]http.HandlerFunc),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", ms.routeHandler)

	const readHeaderTimeout = 10 // seconds for header read timeout

	ms.Server = &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		Handler:           mux,
		ReadHeaderTimeout: readHeaderTimeout * time.Second,
	}

	return ms
}

// AddHandler adds a handler for a specific path.
func (ms *MockServer) AddHandler(path string, handler http.HandlerFunc) {
	ms.Handlers[path] = handler
}

// Start starts the mock server.
func (ms *MockServer) Start() error {
	go func() {
		_ = ms.Server.ListenAndServe()
	}()

	// Wait a bit for server to start
	time.Sleep(retryDelay)

	return nil
}

// Stop stops the mock server.
func (ms *MockServer) Stop() error {
	if ms.Server != nil {
		return ms.Server.Shutdown(context.Background())
	}

	return nil
}

// routeHandler routes requests to registered handlers.
func (ms *MockServer) routeHandler(w http.ResponseWriter, r *http.Request) {
	if handler, exists := ms.Handlers[r.URL.Path]; exists {
		handler(w, r)

		return
	}

	// Default response
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte("Not Found"))
}

// CompareJSON compares two JSON strings for equality.
func CompareJSON(expected, actual string) error {
	var expectedObj, actualObj interface{}

	if err := json.Unmarshal([]byte(expected), &expectedObj); err != nil {
		return fmt.Errorf("failed to unmarshal expected JSON: %w", err)
	}

	if err := json.Unmarshal([]byte(actual), &actualObj); err != nil {
		return fmt.Errorf("failed to unmarshal actual JSON: %w", err)
	}

	expectedBytes, _ := json.Marshal(expectedObj)
	actualBytes, _ := json.Marshal(actualObj)

	if !bytes.Equal(expectedBytes, actualBytes) {
		return fmt.Errorf("JSON mismatch:\nexpected: %s\nactual: %s", expectedBytes, actualBytes)
	}

	return nil
}

// CreateTempDir creates a temporary directory for testing.
func CreateTempDir(prefix string) (string, error) {
	return os.MkdirTemp("", prefix)
}

// CleanupTempDir removes a temporary directory and all its contents.
func CleanupTempDir(dir string) error {
	return os.RemoveAll(dir)
}

// CreateTempFile creates a temporary file with the given content.
func CreateTempFile(content string) (string, error) {
	tmpFile, err := os.CreateTemp("", "test-*.txt")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}

	if _, err := tmpFile.WriteString(content); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpFile.Name())

		return "", fmt.Errorf("failed to write to temp file: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpFile.Name())

		return "", fmt.Errorf("failed to close temp file: %w", err)
	}

	return tmpFile.Name(), nil
}

// ReadTempFile reads content from a temporary file.
func ReadTempFile(filename string) (string, error) {
	content, err := os.ReadFile(filename) //nolint:gosec // Test file reading
	if err != nil {
		return "", fmt.Errorf("failed to read temp file: %w", err)
	}

	return string(content), nil
}

// NewTestLogger creates a test logger for use in tests.
func NewTestLogger(t *testing.T) *zap.Logger {
	t.Helper()

	return zaptest.NewLogger(t)
}

// TempFile creates a temporary file with the given content for testing.
func TempFile(tb testing.TB, content string) (string, func()) {
	tb.Helper()

	tmpFile, err := os.CreateTemp("", "mcp-test-*")
	if err != nil {
		tb.Fatalf("Failed to create temp file: %v", err)
	}

	if _, err := tmpFile.WriteString(content); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpFile.Name())

		tb.Fatalf("Failed to write to temp file: %v", err)
	}

	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpFile.Name())

		tb.Fatalf("Failed to close temp file: %v", err)
	}

	cleanup := func() {
		_ = os.Remove(tmpFile.Name())
	}

	return tmpFile.Name(), cleanup
}
