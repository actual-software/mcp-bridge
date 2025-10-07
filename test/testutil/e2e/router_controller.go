// Router controller test utilities allow flexible style
//

package e2e

import (
	"bufio"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
)

const (
	// DefaultDirectoryPermissions for creating directories.
	DefaultDirectoryPermissions = 0o750
	// DefaultFilePermissions for creating files.

	// Channel buffer sizes for router controller.
	requestChannelBufferSize  = 100
	responseChannelBufferSize = 100
	errorChannelBufferSize    = 10

	// Timeouts.
	routerHealthCheckTimeout = 5 * time.Second
	tokenValidityDuration    = 24 * time.Hour
	messageRetryDelay        = 100 * time.Millisecond
	maxRetryAttempts         = 2
	DefaultFilePermissions   = 0o600
)

// RouterController manages the router binary subprocess and provides stdio interface.
type RouterController struct {
	t          *testing.T
	logger     *zap.Logger
	binaryPath string
	configPath string
	gatewayURL string
	authToken  string // Added for authentication support
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	stdout     io.ReadCloser
	stderr     io.ReadCloser

	// Communication channels
	requests  chan []byte
	responses chan []byte
	errors    chan error

	// Response tracking
	pendingMu sync.RWMutex
	pending   map[string]chan []byte

	// Lifecycle
	ctx     context.Context
	cancel  context.CancelFunc
	done    chan struct{}
	started bool
}

// NewRouterController creates a new router controller for the given gateway URL.
func NewRouterController(t *testing.T, gatewayURL string) *RouterController {
	t.Helper()

	logger, err := zap.NewDevelopment()
	if err != nil {
		logger = zap.NewNop() // Fallback to no-op logger
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &RouterController{
		t:          t,
		logger:     logger,
		binaryPath: "", // Will be set during BuildRouter
		configPath: "", // Will be set during BuildRouter
		gatewayURL: gatewayURL,
		authToken:  "",  // Will be set if authentication is needed
		cmd:        nil, // Will be set when router is started
		stdin:      nil, // Will be set when router is started
		stdout:     nil, // Will be set when router is started
		stderr:     nil, // Will be set when router is started
		requests:   make(chan []byte, requestChannelBufferSize),
		responses:  make(chan []byte, responseChannelBufferSize),
		errors:     make(chan error, errorChannelBufferSize),
		pendingMu:  sync.RWMutex{}, // Initialize mutex
		pending:    make(map[string]chan []byte),
		started:    false, // Not started initially
		ctx:        ctx,
		cancel:     cancel,
		done:       make(chan struct{}),
	}
}

// BuildRouter builds the router binary from source.
func (rc *RouterController) BuildRouter() error {
	return rc.BuildRouterWithContext(context.Background())
}

// BuildRouterWithContext builds the router binary from source with a context.
func (rc *RouterController) BuildRouterWithContext(ctx context.Context) error {
	rc.logger.Info("Building router binary")

	// Find project root using shared infrastructure
	projectRoot, err := FindProjectRoot()
	if err != nil {
		return fmt.Errorf("failed to find project root: %w", err)
	}

	// Build binary
	binaryDir := filepath.Join(projectRoot, "test", "temp")
	if err := os.MkdirAll(binaryDir, DefaultDirectoryPermissions); err != nil {
		return fmt.Errorf("failed to create binary directory: %w", err)
	}

	rc.binaryPath = filepath.Join(binaryDir, "mcp-router")

	//nolint:gosec // Test build command
	buildCmd := exec.CommandContext(ctx, "go", "build",
		"-o", rc.binaryPath,
		filepath.Join(projectRoot, "services", "router", "cmd", "mcp-router"))
	buildCmd.Dir = projectRoot

	if output, err := buildCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to build router: %w\nOutput: %s", err, string(output))
	}

	rc.logger.Info("Router binary built successfully", zap.String("path", rc.binaryPath))

	return nil
}

// Start starts the router process.
func (rc *RouterController) Start() error {
	if rc.started {
		return errors.New("router already started")
	}

	// Generate auth token
	rc.generateAuthToken()

	// Create config file
	if err := rc.createConfigFile(); err != nil {
		return fmt.Errorf("failed to create config file: %w", err)
	}

	// Start the router process
	rc.cmd = exec.CommandContext(rc.ctx, rc.binaryPath, //nolint:gosec // Test router startup
		"--config", rc.configPath, "--log-level", "debug")

	// Set environment variable for auth token
	rc.cmd.Env = append(os.Environ(), "MCP_AUTH_TOKEN="+rc.authToken)

	// Set up pipes
	var err error

	rc.stdin, err = rc.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	rc.stdout, err = rc.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	rc.stderr, err = rc.cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Start the process
	if err := rc.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start router process: %w", err)
	}

	rc.started = true

	// Start I/O handlers
	go rc.handleStdout()
	go rc.handleStderr()
	go rc.handleRequests()
	go rc.handleResponses()

	rc.logger.Info("Router started successfully")

	return nil
}

// Stop stops the router process.
func (rc *RouterController) Stop() {
	if !rc.started {
		return
	}

	rc.logger.Info("Stopping router controller")

	// Cancel context to signal shutdown
	rc.cancel()

	// Close stdin to signal the router to shut down
	if rc.stdin != nil {
		_ = rc.stdin.Close() // Ignore error in test cleanup
	}

	// Wait for process to exit or force kill after timeout
	if rc.cmd != nil && rc.cmd.Process != nil {
		done := make(chan error, 1)

		go func() {
			done <- rc.cmd.Wait()
		}()

		select {
		case <-done:
			// Process exited normally
		case <-time.After(routerHealthCheckTimeout):
			// Force kill after timeout
			rc.logger.Warn("Router process didn't exit gracefully, force killing")
			_ = rc.cmd.Process.Kill() // Ignore error in test cleanup

			<-done // Wait for kill to complete
		}
	}

	// Clean up resources
	rc.cleanup()

	// Signal completion
	close(rc.done)

	rc.logger.Info("Router controller stopped")
}

// SendRequestAndWait sends a request and waits for the response.
func (rc *RouterController) SendRequestAndWait(req MCPRequest, timeout time.Duration) ([]byte, error) {
	if !rc.started {
		return nil, errors.New("router not started")
	}

	// Create response channel
	respChan := make(chan []byte, 1)

	rc.pendingMu.Lock()
	rc.pending[req.ID] = respChan
	rc.pendingMu.Unlock()

	// Clean up on exit
	defer func() {
		rc.pendingMu.Lock()
		delete(rc.pending, req.ID)
		rc.pendingMu.Unlock()
		close(respChan)
	}()

	// Serialize and send request
	reqData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize request: %w", err)
	}

	select {
	case rc.requests <- reqData:
		// Request sent successfully
	case <-time.After(timeout):
		return nil, errors.New("timeout sending request")
	case <-rc.ctx.Done():
		return nil, errors.New("router controller stopped")
	}

	// Wait for response
	select {
	case respData := <-respChan:
		return respData, nil
	case <-time.After(timeout):
		return nil, errors.New("timeout waiting for response")
	case <-rc.ctx.Done():
		return nil, errors.New("router controller stopped")
	}
}

// Internal methods

func (rc *RouterController) processJSONRPCResponse(resp map[string]interface{}, line []byte) {
	respID := resp["id"]
	if respID == nil {
		return
	}

	// Log the response ID and its type
	rc.logger.Debug("Received response from router",
		zap.Any("id", respID),
		zap.String("id_type", fmt.Sprintf("%T", respID)),
		zap.String("response", string(line)))

	// Try to match as string first
	id, ok := respID.(string)
	if !ok {
		rc.logger.Warn("Response ID is not a string",
			zap.Any("id", respID),
			zap.String("type", fmt.Sprintf("%T", respID)))

		return
	}

	rc.pendingMu.RLock()
	respChan, exists := rc.pending[id]

	if !exists {
		rc.logger.Warn("No pending request found for response",
			zap.String("id", id),
			zap.Any("pending_keys", rc.getPendingKeys()))
		rc.pendingMu.RUnlock()

		return
	}

	rc.logger.Debug("Found pending request for response", zap.String("id", id))

	select {
	case respChan <- line:
		// Response delivered
	default:
		// Channel full or closed
		rc.logger.Warn("Failed to deliver response - channel full or closed", zap.String("id", id))
	}

	rc.pendingMu.RUnlock()
}

func (rc *RouterController) generateAuthToken() {
	// Generate JWT token for authentication
	// This uses the same secret as the test gateway configuration
	secretKey := "test-secret-key-for-e2e-testing"

	// Create JWT payload
	now := time.Now()
	payload := map[string]interface{}{
		"iss": "mcp-gateway-e2e",
		"aud": "mcp-clients",
		"sub": "test-e2e-client",
		"iat": now.Unix(),
		"exp": now.Add(tokenValidityDuration).Unix(),
		"jti": "test-e2e-jwt-12345",
	}

	// Create header
	header := map[string]interface{}{
		"alg": "HS256",
		"typ": "JWT",
	}

	// Encode header and payload
	headerJSON, _ := json.Marshal(header)
	payloadJSON, _ := json.Marshal(payload)

	headerB64 := base64.RawURLEncoding.EncodeToString(headerJSON)
	payloadB64 := base64.RawURLEncoding.EncodeToString(payloadJSON)

	// Create signature
	message := headerB64 + "." + payloadB64
	h := hmac.New(sha256.New, []byte(secretKey))
	h.Write([]byte(message))
	signature := base64.RawURLEncoding.EncodeToString(h.Sum(nil))

	// Combine to create JWT
	rc.authToken = message + "." + signature

	rc.logger.Info("Generated auth token for testing")
}

func (rc *RouterController) createConfigFile() error {
	tempDir := filepath.Dir(rc.binaryPath)
	rc.configPath = filepath.Join(tempDir, "router-config.yaml")

	config := fmt.Sprintf(`
gateway_pool:
  endpoints:
    - url: "%s"
      auth:
        type: "bearer"
        token: "%s"
      tls:
        verify: false
        server_name: "localhost"
      connection:
        timeout_ms: 5000
        keepalive_interval_ms: 30000
      reconnect:
        enabled: true
        initial_interval_ms: 1000
        max_interval_ms: 30000
        multiplier: 2.0
        max_attempts: 5
      circuit_breaker:
        enabled: true
        failure_threshold: 5
        timeout_ms: 10000
        recovery_interval_ms: 30000

# Explicitly disable direct mode - all requests go through gateway
direct:
  max_connections: 0

local:
  request_timeout_ms: 30000
  rate_limit:
    enabled: false
    requests_per_sec: 10.0
    burst: 5

logging:
  level: "debug"
  format: "console"

metrics:
  enabled: false
  port: 9090
  path: "/metrics"
`, rc.gatewayURL, rc.authToken)

	if err := os.WriteFile(rc.configPath, []byte(config), DefaultFilePermissions); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	rc.logger.Info("Created router config file", zap.String("path", rc.configPath))

	return nil
}

func (rc *RouterController) handleStdout() {
	defer func() {
		if rc.stdout != nil {
			_ = rc.stdout.Close() // Ignore error in test cleanup
		}
	}()

	scanner := bufio.NewScanner(rc.stdout)
	for scanner.Scan() {
		line := scanner.Bytes()

		// Try to parse as JSON response
		var resp map[string]interface{}
		if err := json.Unmarshal(line, &resp); err != nil {
			// Not JSON, skip processing
			continue
		}

		// Process JSON-RPC response
		rc.processJSONRPCResponse(resp, line)

		// Also send to responses channel for debugging
		select {
		case rc.responses <- line:
		default:
		}
	}

	if err := scanner.Err(); err != nil && !rc.isContextCanceled() {
		rc.logger.Error("Error reading stdout", zap.Error(err))

		select {
		case rc.errors <- err:
		default:
		}
	}
}

func (rc *RouterController) handleStderr() {
	defer func() {
		if rc.stderr != nil {
			_ = rc.stderr.Close() // Ignore error in test cleanup
		}
	}()

	scanner := bufio.NewScanner(rc.stderr)
	for scanner.Scan() {
		line := scanner.Text()
		rc.logger.Debug("Router stderr", zap.String("line", line))
	}

	if err := scanner.Err(); err != nil && !rc.isContextCanceled() {
		rc.logger.Error("Error reading stderr", zap.Error(err))
	}
}

func (rc *RouterController) handleRequests() {
	for {
		select {
		case req := <-rc.requests:
			if rc.stdin != nil {
				rc.logger.Debug("Writing request to router stdin", zap.String("request", string(req)))
				_, writeErr := rc.stdin.Write(append(req, '\n'))

				if writeErr != nil && !rc.isContextCanceled() {
					rc.logger.Error("Error writing to stdin", zap.Error(writeErr))

					select {
					case rc.errors <- writeErr:
					default:
					}
				}
			}
		case <-rc.ctx.Done():
			return
		}
	}
}

func (rc *RouterController) handleResponses() {
	for {
		select {
		case resp := <-rc.responses:
			rc.logger.Debug("Router response", zap.String("response", string(resp)))
		case <-rc.ctx.Done():
			return
		}
	}
}

func (rc *RouterController) isContextCanceled() bool {
	select {
	case <-rc.ctx.Done():
		return true
	default:
		return false
	}
}

func (rc *RouterController) getPendingKeys() []string {
	rc.pendingMu.RLock()
	keys := make([]string, 0, len(rc.pending))

	for k := range rc.pending {
		keys = append(keys, k)
	}

	rc.pendingMu.RUnlock()

	return keys
}

func (rc *RouterController) cleanup() {
	// Clean up temporary files
	if rc.configPath != "" {
		_ = os.Remove(rc.configPath) // Ignore error in test cleanup
	}

	if rc.binaryPath != "" {
		_ = os.Remove(rc.binaryPath) // Ignore error in test cleanup
	}

	// Clean up channels
	rc.pendingMu.Lock()

	for _, ch := range rc.pending {
		close(ch)
	}

	rc.pending = make(map[string]chan []byte)
	rc.pendingMu.Unlock()
}

// GetGatewayURL returns the gateway URL.
func (rc *RouterController) GetGatewayURL() string {
	return rc.gatewayURL
}

// GetAuthToken returns the authentication token.
func (rc *RouterController) GetAuthToken() string {
	return rc.authToken
}

// WaitForHealthy waits for the router to become healthy by checking the gateway.
func (rc *RouterController) WaitForHealthy(timeout time.Duration) error {
	rc.logger.Info("Waiting for router to become healthy")

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		// Check if router is started and can handle requests
		if rc.started && rc.cmd != nil && rc.cmd.Process != nil {
			// Try to send a simple ping request to test connectivity
			testReq := MCPRequest{
				JSONRPC: "2.0",
				ID:      "health-check",
				Method:  "ping",
				Params:  map[string]interface{}{},
			}

			// Use a short timeout for health check
			_, err := rc.SendRequestAndWait(testReq, maxRetryAttempts*time.Second)
			if err == nil {
				rc.logger.Info("Router appears healthy - can handle requests")

				return nil
			}

			// If ping fails, check if router process is still running
			// and connected (based on logs showing "CONNECTED" state)
			select {
			case <-rc.ctx.Done():
				return errors.New("router context canceled")
			default:
				// Process is still running, continue waiting
			}
		}

		time.Sleep(messageRetryDelay)
	}

	return errors.New("router did not become healthy within timeout")
}

// Helper functions

// FindProjectRoot finds the project root directory by looking for go.mod file.
func FindProjectRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current directory: %w", err)
	}

	for {
		// Check for go.work (workspace) first - this indicates the main project root
		if _, err := os.Stat(filepath.Join(dir, "go.work")); err == nil {
			return dir, nil
		}

		// Check for go.mod AND services directory to ensure we're at the project root, not a test subdirectory
		goModPath := filepath.Join(dir, "go.mod")
		servicesPath := filepath.Join(dir, "services")

		if _, err := os.Stat(goModPath); err == nil {
			if _, err := os.Stat(servicesPath); err == nil {
				return dir, nil
			}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("could not find project root (no go.mod with services/ found)")
		}

		dir = parent
	}
}

// NewTestHTTPClient creates an HTTP client suitable for testing.
func NewTestHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
	}
}
