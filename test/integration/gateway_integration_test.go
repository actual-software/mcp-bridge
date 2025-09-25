// Integration test files allow flexible style
//

package integration_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResource manages test resources with proper cleanup.
type TestResource struct {
	name        string
	cleanup     func() error
	cleanupOnce sync.Once
}

// TestManager manages multiple test resources.
type TestManager struct {
	resources []*TestResource
	mu        sync.Mutex
	t         *testing.T
}

// NewTestManager creates a new test resource manager.
func NewTestManager(t *testing.T) *TestManager {
	t.Helper()
	tm := &TestManager{
		resources: make([]*TestResource, 0),
		mu:        sync.Mutex{}, // Initialize mutex
		t:         t,
	}

	// Register cleanup with testing framework
	t.Cleanup(func() {
		if err := tm.CleanupAll(); err != nil {
			t.Logf("Error during test cleanup: %v", err)
		}
	})

	return tm
}

// AddResource adds a resource to be managed.
func (tm *TestManager) AddResource(name string, cleanup func() error) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	resource := &TestResource{
		name:        name,
		cleanup:     cleanup,
		cleanupOnce: sync.Once{}, // Initialize sync.Once
	}

	tm.resources = append(tm.resources, resource)
}

// CleanupAll cleans up all managed resources.
func (tm *TestManager) CleanupAll() error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	var errors []error

	// Cleanup in reverse order (LIFO)
	for i := len(tm.resources) - 1; i >= 0; i-- {
		resource := tm.resources[i]
		resource.cleanupOnce.Do(func() {
			if resource.cleanup != nil {
				if err := resource.cleanup(); err != nil {
					errors = append(errors, fmt.Errorf("cleanup failed for %s: %w", resource.name, err))
				}
			}
		})
	}

	if len(errors) > 0 {
		return fmt.Errorf("multiple cleanup errors: %v", errors)
	}

	return nil
}

// ImprovedTokenGenerator generates proper JWT tokens with error handling.
type ImprovedTokenGenerator struct {
	privateKey *rsa.PrivateKey
	keyID      string
	issuer     string
}

// NewTokenGenerator creates a new token generator with proper key management.
func NewTokenGenerator(issuer string) (*ImprovedTokenGenerator, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("failed to generate RSA key: %w", err)
	}

	return &ImprovedTokenGenerator{
		privateKey: privateKey,
		keyID:      "test-key-id",
		issuer:     issuer,
	}, nil
}

// GenerateToken creates a properly signed JWT token.
func (tg *ImprovedTokenGenerator) GenerateToken(subject string, scopes []string,
	expiresIn time.Duration,
) (string, error) {
	claims := jwt.MapClaims{
		"iss":   tg.issuer,
		"sub":   subject,
		"aud":   []string{"mcp-gateway"},
		"exp":   time.Now().Add(expiresIn).Unix(),
		"iat":   time.Now().Unix(),
		"scope": scopes,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = tg.keyID

	signedToken, err := token.SignedString(tg.privateKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign token: %w", err)
	}

	return signedToken, nil
}

// TestGatewayIntegrationImproved demonstrates improved integration testing.
func TestGatewayIntegrationImproved(t *testing.T) {
	// Create test manager for resource cleanup
	tm := NewTestManager(t)

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Setup test environment
	tokenGen, err := NewTokenGenerator("test-gateway")
	require.NoError(t, err, "Failed to create token generator")

	// Generate test token
	token, err := tokenGen.GenerateToken("test-user", []string{"mcp:read", "mcp:write"}, time.Hour)
	require.NoError(t, err, "Failed to generate test token")

	// Start mock backend server with proper error handling
	backendServer := startMockBackendServerImproved(t, tm)

	// Start gateway server with proper resource management
	gatewayServer := startMockGatewayServerImproved(t, tm, backendServer.URL)

	// Test scenarios
	t.Run("successful_connection", func(t *testing.T) {
		testSuccessfulConnection(ctx, t, gatewayServer.URL, token)
	})

	t.Run("authentication_failure", func(t *testing.T) {
		testAuthenticationFailure(ctx, t, gatewayServer.URL)
	})

	t.Run("connection_timeout", func(t *testing.T) {
		testConnectionTimeout(t, gatewayServer.URL, token)
	})

	t.Run("resource_cleanup", func(t *testing.T) {
		testResourceCleanup(t, tm)
	})
}

// startMockBackendServerImproved creates a mock backend with proper error handling.
func startMockBackendServerImproved(t *testing.T,
	tm *TestManager, // Mock backend server with comprehensive endpoint handling
) *httptest.Server {
	t.Helper()

	mux := setupBackendHandlers(t)
	server := httptest.NewServer(mux)

	// Register cleanup with test manager
	tm.AddResource("backend-server", func() error {
		server.Close()

		return nil
	})

	t.Logf("Started mock backend server at %s", server.URL)

	return server
}

func setupBackendHandlers(t *testing.T) *http.ServeMux {
	t.Helper()

	mux := http.NewServeMux()

	// Health check endpoint
	mux.HandleFunc("/health", handleBackendHealth)

	// Echo endpoint for testing
	mux.HandleFunc("/echo", func(w http.ResponseWriter, r *http.Request) {
		handleBackendEcho(t, w, r)
	})

	// Error simulation endpoint
	mux.HandleFunc("/error", handleBackendError)

	return mux
}

func handleBackendHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(map[string]string{
		"status":    "healthy",
		"timestamp": time.Now().Format(time.RFC3339),
	}); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
	}
}

func handleBackendEcho(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)

		return
	}

	var request map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, fmt.Sprintf("Invalid JSON: %v", err), http.StatusBadRequest)

		return
	}

	response := map[string]interface{}{
		"echo":      request,
		"timestamp": time.Now().Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(response); err != nil {
		t.Logf("Failed to encode response: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

func handleBackendError(w http.ResponseWriter, r *http.Request) {
	errorType := r.URL.Query().Get("type")
	switch errorType {
	case "timeout":
		time.Sleep(10 * time.Second) // Simulate timeout
	case "500":
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	case "404":
		http.Error(w, "Not found", http.StatusNotFound)
	default:
		http.Error(w, "Unknown error type", http.StatusBadRequest)
	}
}

// startMockGatewayServerImproved creates a mock gateway with proper resource management.
func startMockGatewayServerImproved(t *testing.T, tm *TestManager,
	backendURL string, // Mock gateway server with comprehensive functionality
) *httptest.Server {
	t.Helper()

	mux := setupGatewayHandlers(t, backendURL)
	server := httptest.NewServer(mux)

	// Register cleanup with test manager
	tm.AddResource("gateway-server", func() error {
		server.Close()

		return nil
	})

	t.Logf("Started mock gateway server at %s", server.URL)

	return server
}

func setupGatewayHandlers(t *testing.T, backendURL string) *http.ServeMux {
	t.Helper()

	mux := http.NewServeMux()

	// Gateway endpoint with proper error handling
	mux.HandleFunc("/gateway", func(w http.ResponseWriter, r *http.Request) {
		handleGatewayRequest(t, w, r, backendURL)
	})

	// Metrics endpoint
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, _ *http.Request) {
		handleGatewayMetrics(t, w)
	})

	return mux
}

func handleGatewayRequest(t *testing.T, w http.ResponseWriter, r *http.Request, backendURL string) {
	t.Helper()
	// Validate authorization header
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		http.Error(w, "Authorization header required", http.StatusUnauthorized)

		return
	}

	// Simulate token validation
	if authHeader != "Bearer valid-token" && len(authHeader) < 20 {
		http.Error(w, "Invalid token", http.StatusUnauthorized)

		return
	}

	// Proxy request to backend
	if !proxyToBackend(t, w, r, backendURL) {
		return
	}

	// Return success response
	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(map[string]string{
		"status":  "gateway_healthy",
		"backend": "connected",
	}); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
	}
}

func proxyToBackend(t *testing.T, w http.ResponseWriter, r *http.Request, backendURL string) bool {
	t.Helper()

	client := &http.Client{
		Timeout:       5 * time.Second,
		Transport:     nil, // Use default transport
		CheckRedirect: nil, // Use default redirect policy
		Jar:           nil, // No cookie jar
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, backendURL+"/health", http.NoBody)
	require.NoError(t, err)

	backendResp, err := client.Do(req)
	if err != nil {
		t.Logf("Backend request failed: %v", err)
		http.Error(w, fmt.Sprintf("Backend unavailable: %v", err), http.StatusBadGateway)

		return false
	}

	defer func() {
		if err := backendResp.Body.Close(); err != nil {
			t.Logf("Failed to close backend response body: %v", err)
		}
	}()

	if backendResp.StatusCode != http.StatusOK {
		http.Error(w, "Backend returned error", http.StatusBadGateway)

		return false
	}

	return true
}

func handleGatewayMetrics(t *testing.T, w http.ResponseWriter) {
	t.Helper()
	w.Header().Set("Content-Type", "text/plain")

	if _, err := fmt.Fprintf(w, "# HELP gateway_requests_total Total number of requests\n"); err != nil {
		t.Logf("Failed to write metrics header: %v", err)
	}

	if _, err := fmt.Fprintf(w, "gateway_requests_total 42\n"); err != nil {
		t.Logf("Failed to write metrics value: %v", err)
	}
}

// testSuccessfulConnection tests a successful gateway connection.
func testSuccessfulConnection(ctx context.Context, t *testing.T, gatewayURL, token string) {
	t.Helper()

	client := &http.Client{
		Timeout:       10 * time.Second,
		Transport:     nil, // Use default transport
		CheckRedirect: nil, // Use default redirect policy
		Jar:           nil, // No cookie jar
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, gatewayURL+"/gateway", http.NoBody)
	require.NoError(t, err, "Failed to create request")

	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := client.Do(req)
	require.NoError(t, err, "Request failed")

	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Logf("Failed to close response body: %v", err)
		}
	}()

	assert.Equal(t, http.StatusOK, resp.StatusCode, "Unexpected status code")

	var response map[string]string

	err = json.NewDecoder(resp.Body).Decode(&response)
	require.NoError(t, err, "Failed to decode response")

	assert.Equal(t, "gateway_healthy", response["status"])
	assert.Equal(t, "connected", response["backend"])
}

// testAuthenticationFailure tests authentication failure scenarios.
func testAuthenticationFailure(ctx context.Context, t *testing.T, gatewayURL string) {
	t.Helper()

	client := &http.Client{
		Timeout:       5 * time.Second,
		Transport:     nil, // Use default transport
		CheckRedirect: nil, // Use default redirect policy
		Jar:           nil, // No cookie jar
	}

	// Test missing authorization header
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, gatewayURL+"/gateway", http.NoBody)
	require.NoError(t, err, "Failed to create request")

	resp, err := client.Do(req)
	require.NoError(t, err, "Request failed")

	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Logf("Failed to close response body: %v", err)
		}
	}()

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode, "Expected unauthorized status")

	// Test invalid token
	req2, err := http.NewRequestWithContext(ctx, http.MethodGet, gatewayURL+"/gateway", http.NoBody)
	require.NoError(t, err, "Failed to create request")
	req2.Header.Set("Authorization", "Bearer invalid")

	resp2, err := client.Do(req2)
	require.NoError(t, err, "Request failed")

	defer func() {
		if err := resp2.Body.Close(); err != nil {
			t.Logf("Failed to close response body: %v", err)
		}
	}()

	assert.Equal(t, http.StatusUnauthorized, resp2.StatusCode, "Expected unauthorized status")
}

// testConnectionTimeout tests timeout handling.
func testConnectionTimeout(t *testing.T, _, token string) {
	t.Helper()
	// Create client with very short timeout
	client := &http.Client{
		Timeout:       100 * time.Millisecond,
		Transport:     nil, // Use default transport
		CheckRedirect: nil, // Use default redirect policy
		Jar:           nil, // No cookie jar
	}

	// Create a listener that will cause a timeout
	lc := &net.ListenConfig{}
	listener, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err, "Failed to create listener")

	defer func() {
		if err := listener.Close(); err != nil {
			t.Logf("Failed to close listener: %v", err)
		}
	}()

	// Don't accept connections to cause timeout
	timeoutURL := fmt.Sprintf("http://%s/timeout", listener.Addr().String())

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, timeoutURL, http.NoBody)
	require.NoError(t, err, "Failed to create request")
	req.Header.Set("Authorization", "Bearer "+token)

	start := time.Now()

	resp, err := client.Do(req)
	if resp != nil {
		if err := resp.Body.Close(); err != nil {
			t.Logf("Failed to close response body: %v", err)
		}
	}

	duration := time.Since(start)

	// Should timeout
	require.Error(t, err, "Expected timeout error")
	assert.Less(t, duration, 5*time.Second, "Timeout took too long")

	// Check if it's a timeout error
	var netErr net.Error
	if errors.As(err, &netErr) {
		assert.True(t, netErr.Timeout(), "Expected timeout error")
	}
}

// testResourceCleanup verifies that resources are properly cleaned up.
func testResourceCleanup(t *testing.T, tm *TestManager) {
	t.Helper()
	// Add a test resource
	cleaned := false

	tm.AddResource("test-resource", func() error {
		cleaned = true

		return nil
	})

	// Force cleanup
	err := tm.CleanupAll()
	require.NoError(t, err, "Cleanup should not fail")
	assert.True(t, cleaned, "Resource should be cleaned up")
}

// TestNetworkListener tests proper network listener management.
func TestNetworkListener(t *testing.T) {
	tm := NewTestManager(t)

	// Create listener with proper error handling
	lc := &net.ListenConfig{}
	listener, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err, "Failed to create listener")

	// Register cleanup
	tm.AddResource("network-listener", func() error {
		return listener.Close()
	})

	// Test that listener is working
	addr := listener.Addr().String()
	assert.NotEmpty(t, addr, "Listener address should not be empty")

	// Test connection to listener
	dialer := &net.Dialer{}

	conn, err := dialer.DialContext(context.Background(), "tcp", addr)
	if err != nil {
		t.Logf("Expected dial error since no server is accepting: %v", err)
	} else {
		defer func() {
			_ = conn.Close()
		}()
	}
}

// TestContextCancellation tests proper context handling.
func TestContextCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Simulate long-running operation
	done := make(chan bool)

	go func() {
		defer close(done)

		select {
		case <-time.After(1 * time.Second):
			t.Error("Operation did not respect context cancellation")
		case <-ctx.Done():
			t.Log("Operation properly canceled by context")
		}
	}()

	// Wait for completion
	select {
	case <-done:
		// Operation completed
	case <-time.After(2 * time.Second):
		t.Error("Test timed out")
	}
}

// TestGoroutineCleanup tests proper goroutine cleanup.
func TestGoroutineCleanup(t *testing.T) {
	var wg sync.WaitGroup

	ctx, cancel := context.WithCancel(context.Background())

	// Start background goroutine
	wg.Add(1)

	go func() {
		defer wg.Done()

		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				// Do work
			case <-ctx.Done():
				t.Log("Goroutine properly canceled")

				return
			}
		}
	}()

	// Let it run briefly
	time.Sleep(50 * time.Millisecond)

	// Cancel and wait for cleanup
	cancel()

	// Wait with timeout
	done := make(chan struct{})

	go func() {
		defer close(done)

		wg.Wait()
	}()

	select {
	case <-done:
		t.Log("Goroutine cleanup completed successfully")
	case <-time.After(1 * time.Second):
		t.Error("Goroutine cleanup timed out")
	}
}

// TestErrorPropagation tests proper error handling and propagation.
func TestErrorPropagation(t *testing.T) {
	testCases := getErrorPropagationTestCases(t)

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			runErrorPropagationTest(t, tc)
		})
	}
}

func getErrorPropagationTestCases(t *testing.T) []struct {
	name        string
	operation   func() error
	expectError bool
	errorType   string
} {
	t.Helper()

	return []struct {
		name        string
		operation   func() error
		expectError bool
		errorType   string
	}{
		{
			name:        "network_error",
			operation:   createNetworkErrorOperation(t),
			expectError: true,
			errorType:   "network",
		},
		{
			name:        "context_cancellation",
			operation:   createContextCancellationOperation(),
			expectError: true,
			errorType:   "context",
		},
		{
			name:        "successful_operation",
			operation:   func() error { return nil },
			expectError: false,
			errorType:   "",
		},
	}
}

func createNetworkErrorOperation(t *testing.T) func() error {
	t.Helper()

	return func() error {
		// Create a timeout scenario by connecting to a non-routable address
		dialer := &net.Dialer{Timeout: 100 * time.Millisecond}

		conn, err := dialer.DialContext(context.Background(), "tcp", "10.255.255.1:1")
		if conn != nil {
			if closeErr := conn.Close(); closeErr != nil {
				t.Logf("Error closing connection: %v", closeErr)
			}
		}

		return err
	}
}

func createContextCancellationOperation() func() error {
	return func() error {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
			return errors.New("context not canceled")
		}
	}
}

func runErrorPropagationTest(t *testing.T, tc struct {
	name        string
	operation   func() error
	expectError bool
	errorType   string
}) {
	t.Helper()

	err := tc.operation()

	if tc.expectError {
		require.Error(t, err, "Expected error for %s", tc.name)
		validateErrorType(t, err, tc.errorType)
	} else {
		assert.NoError(t, err, "Unexpected error for %s", tc.name)
	}
}

func validateErrorType(t *testing.T, err error, errorType string) {
	t.Helper()

	if errorType == "" {
		return
	}

	switch errorType {
	case "network":
		var netErr net.Error
		if errors.As(err, &netErr) {
			assert.True(t, netErr.Timeout(), "Expected network error")
		}
	case "context":
		assert.Equal(t, context.Canceled, err, "Expected context cancellation error")
	}
}

// Helper functions for environment-based configuration.

func isVerbose() bool {
	return os.Getenv("TEST_VERBOSE") == "true"
}

// Example of proper test structure with setup/teardown.
func TestProperTestStructure(t *testing.T) {
	// Setup phase with error handling
	tm := NewTestManager(t)

	setup := func() (interface{}, error) {
		// Simulate setup that might fail
		if os.Getenv("FORCE_SETUP_FAILURE") == "true" {
			return nil, errors.New("forced setup failure")
		}

		resource := map[string]string{
			"status": "initialized",
			"id":     "test-resource-123",
		}

		// Register cleanup
		tm.AddResource("setup-resource", func() error {
			if isVerbose() {
				t.Log("Cleaning up setup resource")
			}

			return nil
		})

		return resource, nil
	}

	// Execute setup
	resource, err := setup()
	require.NoError(t, err, "Setup should not fail")
	require.NotNil(t, resource, "Resource should be initialized")

	// Test execution phase
	resourceMap, ok := resource.(map[string]string)
	require.True(t, ok, "Resource should be a map[string]string")
	assert.Equal(t, "initialized", resourceMap["status"])
	assert.NotEmpty(t, resourceMap["id"])

	// Teardown is handled automatically by TestManager
	t.Log("Test completed successfully")
}
