package main

import (
	"bufio"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
)

const (
	// Channel buffer sizes.
	requestBufferSize  = 100
	responseBufferSize = 100
	errorBufferSize    = 10

	// Timeouts.
	jwtExpiryTime     = 60 * time.Minute
	jwtExpirySeconds  = 24 * 60 * 60 // 24 hours in seconds
	startTimeout      = 3 * time.Second
	shutdownTimeout   = 10 * time.Second
	connectionTimeout = 5 * time.Second
	readTimeout       = 5 * time.Second
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
	ctx         context.Context
	cancel      context.CancelFunc
	done        chan struct{}
	processDone chan struct{} // Signals when cmd.Wait() has completed
	started     bool
	mu          sync.RWMutex

	// Log storage for testing
	logsMu     sync.RWMutex
	stderrLogs []string
}

// NewRouterController creates a new router controller.
func NewRouterController(t *testing.T, gatewayURL string) *RouterController {
	t.Helper()

	logger, _ := zap.NewDevelopment()

	// Build the router binary path
	binaryPath := filepath.Join("..", "..", "..", "services", "router", "bin", "mcp-router")

	// Create config path - use stdio config for proper log separation
	configPath := filepath.Join("configs", "router-stdio.yaml")

	ctx, cancel := context.WithCancel(context.Background())

	return &RouterController{
		t:           t,
		logger:      logger,
		binaryPath:  binaryPath,
		configPath:  configPath,
		gatewayURL:  gatewayURL,
		requests:    make(chan []byte, requestBufferSize),
		responses:   make(chan []byte, responseBufferSize),
		errors:      make(chan error, errorBufferSize),
		pending:     make(map[string]chan []byte),
		ctx:         ctx,
		cancel:      cancel,
		done:        make(chan struct{}),
		processDone: make(chan struct{}),
	}
}

// NewRouterControllerWithAuth creates a router controller with authentication token.
func NewRouterControllerWithAuth(t *testing.T, gatewayURL, authToken string) *RouterController {
	t.Helper()
	router := NewRouterController(t, gatewayURL)
	router.authToken = authToken

	return router
}

// BuildRouter builds the router binary if it doesn't exist or is outdated.
func (rc *RouterController) BuildRouter() error {
	rc.logger.Info("Building router binary", zap.String("path", rc.binaryPath))

	// Always rebuild for E2E tests to ensure we have the latest code and avoid caching issues
	rc.logger.Info("Force rebuilding router binary for E2E test consistency")

	// Remove existing binary to force rebuild
	if err := os.Remove(rc.binaryPath); err != nil && !os.IsNotExist(err) {
		rc.logger.Warn("Failed to remove old binary", zap.Error(err))
	}

	// Clear Go caches to ensure completely clean build
	routerDir := filepath.Join("..", "..", "..", "services", "router")

	rc.logger.Info("Clearing Go caches for clean build")

	cleanCacheCmd := exec.CommandContext(rc.ctx, "go", "clean", "-cache", "-modcache", "-testcache")

	cleanCacheCmd.Dir = routerDir
	if err := cleanCacheCmd.Run(); err != nil {
		rc.logger.Warn("Failed to clear Go caches", zap.Error(err))
	}

	// Build the binary with verbose output to verify build process
	rc.logger.Info("Building router binary with make clean build")

	cmd := exec.CommandContext(rc.ctx, "make", "clean", "build")
	cmd.Dir = routerDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to build router: %w", err)
	}

	// Verify the binary was created and get its build info
	stat, err := os.Stat(rc.binaryPath)
	if os.IsNotExist(err) {
		return fmt.Errorf("router binary was not created at %s", rc.binaryPath)
	}

	// Log binary details for verification
	rc.logger.Info("Router binary built successfully",
		zap.String("path", rc.binaryPath),
		zap.Int64("size", stat.Size()),
		zap.Time("mod_time", stat.ModTime()),
		zap.String("build_timestamp", time.Now().Format(time.RFC3339Nano)))

	// Verify the binary is executable and can display version
	versionCmd := exec.CommandContext(rc.ctx, rc.binaryPath, "--version") // #nosec G204 - binaryPath is controlled

	versionOutput, err := versionCmd.Output()
	if err != nil {
		rc.logger.Warn("Failed to verify binary version", zap.Error(err))
	} else {
		rc.logger.Info("Binary version verification", zap.String("version", string(versionOutput)))
	}

	return nil
}

// generateFreshJWT creates a fresh JWT token with 24-hour expiration.
func (rc *RouterController) generateFreshJWT() string {
	const secretKey = "test-jwt-secret-for-e2e-testing-only"

	// Create JWT header
	header := map[string]interface{}{
		"alg": "HS256",
		"typ": "JWT",
	}

	// Create JWT payload with extended expiration
	now := time.Now().Unix()
	payload := map[string]interface{}{
		"iss": "mcp-gateway-e2e",
		"aud": "mcp-clients",
		"sub": "test-e2e-client",
		"iat": now,
		"exp": now + jwtExpirySeconds, // 24 hours from now
		"jti": "test-e2e-jwt-12345",
	}

	// Base64 encode header and payload
	headerJSON, _ := json.Marshal(header)
	payloadJSON, _ := json.Marshal(payload)

	headerB64 := strings.TrimRight(base64.URLEncoding.EncodeToString(headerJSON), "=")
	payloadB64 := strings.TrimRight(base64.URLEncoding.EncodeToString(payloadJSON), "=")

	// Create signature
	message := headerB64 + "." + payloadB64
	h := hmac.New(sha256.New, []byte(secretKey))
	h.Write([]byte(message))
	signature := h.Sum(nil)
	signatureB64 := strings.TrimRight(base64.URLEncoding.EncodeToString(signature), "=")

	// Create final JWT
	jwt := headerB64 + "." + payloadB64 + "." + signatureB64

	var expiresAt int64
	if exp, ok := payload["exp"].(int64); ok {
		expiresAt = exp
	}

	rc.logger.Info("Generated fresh JWT token",
		zap.Int64("expires_at", expiresAt),
		zap.String("expires_readable", time.Unix(expiresAt, 0).Format(time.RFC3339)))

	return jwt
}

// Start launches the router binary and sets up communication.
//nolint:contextcheck // Test harness function
func (rc *RouterController) Start() error {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	if rc.started {
		return errors.New("router already started")
	}

	rc.logStartupInfo()

	if err := rc.validatePaths(); err != nil {
		return err
	}

	rc.logCertificateInfo()

	if err := rc.setupCommand(); err != nil {
		return err
	}

	if err := rc.setupPipes(); err != nil {
		return err
	}

	if err := rc.startProcess(); err != nil {
		return err
	}

	rc.startIOHandlers()

	if err := rc.testBasicConnectivity(); err != nil {
		rc.logger.Error("Gateway basic connectivity test failed", zap.Error(err))

		return fmt.Errorf("gateway not accessible: %w", err)
	}

	// Wait for router to connect to gateway
	time.Sleep(startTimeout)

	return nil
}

// logStartupInfo logs the startup information.
func (rc *RouterController) logStartupInfo() {
	rc.logger.Info("Starting router binary",
		zap.String("binary", rc.binaryPath),
		zap.String("config", rc.configPath),
		zap.String("startup_timestamp", time.Now().Format(time.RFC3339Nano)))
}

// validatePaths validates that binary and config files exist.
func (rc *RouterController) validatePaths() error {
	if _, err := os.Stat(rc.binaryPath); os.IsNotExist(err) {
		rc.logger.Error("Router binary not found", zap.String("path", rc.binaryPath))

		return fmt.Errorf("router binary not found at %s (run BuildRouter first)", rc.binaryPath)
	}

	rc.logger.Info("Router binary verified", zap.String("path", rc.binaryPath))

	if _, err := os.Stat(rc.configPath); os.IsNotExist(err) {
		rc.logger.Error("Router config not found", zap.String("path", rc.configPath))

		return fmt.Errorf("router config not found at %s", rc.configPath)
	}

	rc.logger.Info("Router config verified", zap.String("path", rc.configPath))

	return nil
}

// logCertificateInfo logs CA certificate information.
func (rc *RouterController) logCertificateInfo() {
	caCertPath := "certs/ca.crt"

	stat, err := os.Stat(caCertPath)
	if err != nil {
		rc.logger.Warn("CA certificate file not accessible",
			zap.String("path", caCertPath),
			zap.Error(err))

		return
	}

	certContent, readErr := os.ReadFile(caCertPath)

	var certHash string

	if readErr == nil {
		h := sha256.Sum256(certContent)
		certHash = hex.EncodeToString(h[:8]) // First 8 bytes for brevity
	}

	rc.logger.Info("CA certificate file status",
		zap.String("path", caCertPath),
		zap.Int64("size", stat.Size()),
		zap.Time("mod_time", stat.ModTime()),
		zap.String("content_hash", certHash),
	)
}

// setupCommand sets up the command and environment.
func (rc *RouterController) setupCommand() error {
	// #nosec G204 - binaryPath and configPath are controlled
	rc.cmd = exec.CommandContext(rc.ctx, rc.binaryPath, "--config", rc.configPath)
	rc.cmd.Dir = "."

	jwtToken := rc.generateFreshJWT()
	if rc.authToken != "" {
		jwtToken = rc.authToken
	}

	rc.cmd.Env = append(os.Environ(), "MCP_AUTH_TOKEN="+jwtToken)
	rc.logger.Info("Router environment configured", zap.String("auth_token_length", strconv.Itoa(len(jwtToken))))

	return nil
}

// setupPipes sets up stdin, stdout, and stderr pipes.
func (rc *RouterController) setupPipes() error {
	stdin, err := rc.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	rc.stdin = stdin

	stdout, err := rc.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	rc.stdout = stdout

	stderr, err := rc.cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	rc.stderr = stderr

	return nil
}

// startProcess starts the router process.
func (rc *RouterController) startProcess() error {
	startTime := time.Now()
	rc.logger.Info("Starting router process", zap.String("start_time", startTime.Format(time.RFC3339Nano)))

	if err := rc.cmd.Start(); err != nil {
		rc.logger.Error("Failed to start router process", zap.Error(err))

		return fmt.Errorf("failed to start router: %w", err)
	}

	processStartTime := time.Now()
	rc.started = true

	rc.logger.Info("Router started successfully",
		zap.Int("pid", rc.cmd.Process.Pid),
		zap.Duration("startup_duration", processStartTime.Sub(startTime)),
		zap.String("process_start_time", processStartTime.Format(time.RFC3339Nano)))

	return nil
}

// startIOHandlers starts the I/O handler goroutines.
func (rc *RouterController) startIOHandlers() {
	go rc.handleStdin()
	go rc.handleStdout()
	go rc.handleStderr()
	go rc.handleProcess()
}

// Stop gracefully shuts down the router.
func (rc *RouterController) Stop() error {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	if !rc.started {
		return nil
	}

	rc.logger.Info("Stopping router")

	rc.initiateShutdown()
	rc.waitForProcessExit()
	rc.closePipes()
	rc.waitForGoroutines()

	rc.started = false
	rc.logger.Info("Router stopped")

	return nil
}

// initiateShutdown signals the router to shut down.
func (rc *RouterController) initiateShutdown() {
	rc.cancel()

	if rc.stdin != nil {
		_ = rc.stdin.Close()
	}
}

// waitForProcessExit waits for the router process to exit.
func (rc *RouterController) waitForProcessExit() {
	// Check if handleProcess is already waiting
	rc.mu.Lock()

	if !rc.started || rc.cmd == nil {
		rc.mu.Unlock()

		return
	}

	rc.mu.Unlock()

	// Wait for process to exit or timeout
	select {
	case <-rc.processDone:
		// Process exited normally (handleProcess completed)
		return
	case <-time.After(shutdownTimeout):
		rc.logger.Warn("Router did not exit gracefully, killing")
		rc.forceKillProcess()
		// Wait for handleProcess to complete after killing
		select {
		case <-rc.processDone:
			// Process finally exited
		case <-time.After(1 * time.Second):
			// Give up waiting
			rc.logger.Error("Process did not exit after kill")
		}
	}
}

// forceKillProcess forcefully kills the router process.
func (rc *RouterController) forceKillProcess() {
	if rc.cmd.Process != nil {
		if err := rc.cmd.Process.Kill(); err != nil {
			rc.logger.Error("Failed to kill process", zap.Error(err))
		}
	}
}

// closePipes closes the remaining pipes.
func (rc *RouterController) closePipes() {
	if rc.stdout != nil {
		_ = rc.stdout.Close()
	}

	if rc.stderr != nil {
		_ = rc.stderr.Close()
	}
}

// waitForGoroutines waits for background goroutines to finish.
func (rc *RouterController) waitForGoroutines() {
	select {
	case <-rc.done:
	case <-time.After(readTimeout):
		rc.logger.Warn("Timeout waiting for goroutines to finish")
	}
}

// GetLogs returns the captured stderr logs.
func (rc *RouterController) GetLogs() []string {
	rc.logsMu.RLock()
	defer rc.logsMu.RUnlock()

	// Return a copy of the logs
	logs := make([]string, len(rc.stderrLogs))
	copy(logs, rc.stderrLogs)

	return logs
}

// SendRequest sends an MCP request to the router.
func (rc *RouterController) SendRequest(req interface{}) error {
	fmt.Printf("[DEBUG] Router: SendRequest called\n")

	data, err := json.Marshal(req)
	if err != nil {
		fmt.Printf("[DEBUG] Router: Failed to marshal request in SendRequest: %v\n", err)

		return fmt.Errorf("failed to marshal request: %w", err)
	}

	fmt.Printf("[DEBUG] Router: Marshaled request data: %s\n", string(data))
	fmt.Printf("[DEBUG] Router: Sending to requests channel...\n")

	select {
	case rc.requests <- data:
		fmt.Printf("[DEBUG] Router: Successfully sent request to channel\n")

		return nil
	case <-rc.ctx.Done():
		fmt.Printf("[DEBUG] Router: Router shutting down while sending request\n")

		return errors.New("router is shutting down")
	case <-time.After(readTimeout):
		fmt.Printf("[DEBUG] Router: Timeout sending request to channel\n")

		return errors.New("timeout sending request")
	}
}

// SendRequestAndWait sends an MCP request and waits for the response.
func (rc *RouterController) SendRequestAndWait(req interface{}, timeout time.Duration) ([]byte, error) {
	fmt.Printf("[DEBUG] Router: SendRequestAndWait called with timeout %v\n", timeout)

	reqID, err := rc.extractRequestID(req)
	if err != nil {
		return nil, err
	}

	respChan, err := rc.setupResponseChannel(reqID)
	if err != nil {
		return nil, err
	}

	if err := rc.sendRequestWithCleanup(req, reqID, respChan); err != nil {
		return nil, err
	}

	return rc.waitForResponse(reqID, respChan, timeout)
}

// extractRequestID extracts the request ID from the request.
func (rc *RouterController) extractRequestID(req interface{}) (string, error) {
	reqData, err := json.Marshal(req)
	if err != nil {
		fmt.Printf("[DEBUG] Router: Failed to marshal request: %v\n", err)

		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	fmt.Printf("[DEBUG] Router: Request data: %s\n", string(reqData))

	var reqMap map[string]interface{}
	if err := json.Unmarshal(reqData, &reqMap); err != nil {
		fmt.Printf("[DEBUG] Router: Failed to parse request: %v\n", err)

		return "", fmt.Errorf("failed to parse request: %w", err)
	}

	reqID, ok := reqMap["id"].(string)
	if !ok {
		fmt.Printf("[DEBUG] Router: Request missing string ID: %+v\n", reqMap)

		return "", errors.New("request must have string ID")
	}

	fmt.Printf("[DEBUG] Router: Extracted request ID: %s\n", reqID)

	return reqID, nil
}

// setupResponseChannel creates and registers a response channel.
func (rc *RouterController) setupResponseChannel(reqID string) (chan []byte, error) {
	respChan := make(chan []byte, 1)

	rc.pendingMu.Lock()
	rc.pending[reqID] = respChan
	rc.pendingMu.Unlock()

	fmt.Printf("[DEBUG] Router: Created response channel for request %s\n", reqID)

	return respChan, nil
}

// sendRequestWithCleanup sends the request with cleanup on failure.
func (rc *RouterController) sendRequestWithCleanup(req interface{}, reqID string, respChan chan []byte) error {
	fmt.Printf("[DEBUG] Router: Sending request to router process...\n")

	if err := rc.SendRequest(req); err != nil {
		fmt.Printf("[DEBUG] Router: Failed to send request: %v\n", err)
		rc.cleanupResponseChannel(reqID)

		return err
	}

	return nil
}

// waitForResponse waits for the response with timeout.
func (rc *RouterController) waitForResponse(reqID string, respChan chan []byte, timeout time.Duration) ([]byte, error) {
	fmt.Printf("[DEBUG] Router: Request sent, waiting for response with timeout %v...\n", timeout)

	select {
	case resp := <-respChan:
		fmt.Printf("[DEBUG] Router: Received response: %s\n", string(resp))

		return resp, nil

	case <-time.After(timeout):
		fmt.Printf("[DEBUG] Router: Timeout waiting for response to request %s\n", reqID)
		rc.cleanupResponseChannel(reqID)

		return nil, fmt.Errorf("timeout waiting for response to request %s", reqID)

	case <-rc.ctx.Done():
		fmt.Printf("[DEBUG] Router: Router shutting down while waiting for response\n")
		rc.cleanupResponseChannel(reqID)

		return nil, errors.New("router is shutting down")
	}
}

// cleanupResponseChannel cleans up the response channel for a request ID.
func (rc *RouterController) cleanupResponseChannel(reqID string) {
	rc.pendingMu.Lock()
	defer rc.pendingMu.Unlock()

	if ch, exists := rc.pending[reqID]; exists {
		delete(rc.pending, reqID)
		close(ch)
		fmt.Printf("[DEBUG] Router: Cleaned up response channel for request %s\n", reqID)
	}
}

// GetNextResponse returns the next response from the router.
func (rc *RouterController) GetNextResponse(timeout time.Duration) ([]byte, error) {
	select {
	case resp := <-rc.responses:
		return resp, nil
	case err := <-rc.errors:
		return nil, fmt.Errorf("router error: %w", err)
	case <-time.After(timeout):
		return nil, errors.New("timeout waiting for response")
	case <-rc.ctx.Done():
		return nil, errors.New("router is shutting down")
	}
}

// IsRunning returns true if the router process is running.
func (rc *RouterController) IsRunning() bool {
	rc.mu.RLock()
	defer rc.mu.RUnlock()

	return rc.started && rc.cmd != nil && rc.cmd.ProcessState == nil
}

// GetPID returns the process ID of the router.
func (rc *RouterController) GetPID() int {
	rc.mu.RLock()
	defer rc.mu.RUnlock()

	if rc.cmd != nil && rc.cmd.Process != nil {
		return rc.cmd.Process.Pid
	}

	return 0
}

// handleStdin forwards requests to the router's stdin.
func (rc *RouterController) handleStdin() {
	fmt.Printf("[DEBUG] Router: handleStdin started\n")

	defer func() {
		fmt.Printf("[DEBUG] Router: handleStdin exiting\n")

		if rc.stdin != nil {
			_ = rc.stdin.Close()
		}
	}()

	for {
		select {
		case <-rc.ctx.Done():
			fmt.Printf("[DEBUG] Router: handleStdin context done\n")

			return
		case data := <-rc.requests:
			fmt.Printf("[DEBUG] Router: handleStdin received request: %s\n", string(data))
			// Add newline for JSON line protocol
			line := append(append([]byte{}, data...), '\n')

			fmt.Printf("[DEBUG] Router: Writing to router stdin: %s", string(line))

			if _, err := rc.stdin.Write(line); err != nil {
				fmt.Printf("[DEBUG] Router: Failed to write to stdin: %v\n", err)
				rc.logger.Error("Failed to write to router stdin", zap.Error(err))

				rc.errors <- fmt.Errorf("stdin write error: %w", err)

				return
			}

			fmt.Printf("[DEBUG] Router: Successfully wrote to router stdin\n")
		}
	}
}

// handleStdout reads responses from the router's stdout.
func (rc *RouterController) handleStdout() {
	fmt.Printf("[DEBUG] Router: handleStdout started\n")

	defer func() {
		fmt.Printf("[DEBUG] Router: handleStdout exiting\n")
		close(rc.done)
	}()

	scanner := bufio.NewScanner(rc.stdout)

	for scanner.Scan() {
		line := scanner.Bytes()
		fmt.Printf("[DEBUG] Router: handleStdout received line: %s\n", string(line))

		if err := rc.processResponseLine(line); err != nil {
			continue // Skip invalid lines
		}

		rc.sendToGeneralResponseChannel(line)
	}

	if err := scanner.Err(); err != nil {
		rc.logger.Error("Error reading router stdout", zap.Error(err))

		rc.errors <- fmt.Errorf("stdout read error: %w", err)
	}
}

// processResponseLine processes a single response line.
func (rc *RouterController) processResponseLine(line []byte) error {
	respMap, err := rc.parseResponseJSON(line)
	if err != nil {
		return err
	}

	fmt.Printf("[DEBUG] Router: Parsed response: %+v\n", respMap)

	if respID, ok := respMap["id"].(string); ok {
		rc.handlePendingResponse(respID, line)
	} else {
		fmt.Printf("[DEBUG] Router: Response has no ID field\n")
	}

	return nil
}

// parseResponseJSON parses JSON response and handles errors.
func (rc *RouterController) parseResponseJSON(line []byte) (map[string]interface{}, error) {
	var respMap map[string]interface{}
	if err := json.Unmarshal(line, &respMap); err != nil {
		fmt.Printf("[DEBUG] Router: Failed to parse response JSON: %v, line: %s\n", err, string(line))
		rc.logger.Warn("Failed to parse response JSON",
			zap.String("line", string(line)),
			zap.Error(err))

		return nil, err
	}

	// Filter out log lines (they have "level", "timestamp", "caller", "msg" fields)
	if _, hasLevel := respMap["level"]; hasLevel {
		if _, hasTimestamp := respMap["timestamp"]; hasTimestamp {
			if _, hasCaller := respMap["caller"]; hasCaller {
				// This is a log line, not an MCP response
				fmt.Printf("[DEBUG] Router: Filtered out log line\n")

				return nil, errors.New("log line, not MCP response")
			}
		}
	}

	// Valid MCP responses should have "jsonrpc" field
	if _, hasJSONRPC := respMap["jsonrpc"]; !hasJSONRPC {
		fmt.Printf("[DEBUG] Router: Response missing jsonrpc field, likely not an MCP response\n")

		return nil, errors.New("not an MCP response")
	}

	return respMap, nil
}

// handlePendingResponse handles responses for pending requests.
func (rc *RouterController) handlePendingResponse(respID string, line []byte) {
	fmt.Printf("[DEBUG] Router: Found response ID: %s\n", respID)

	rc.pendingMu.Lock()
	defer rc.pendingMu.Unlock()

	ch, exists := rc.pending[respID]
	if !exists {
		fmt.Printf("[DEBUG] Router: No pending channel found for ID %s (already processed or timed out)\n", respID)

		return
	}

	fmt.Printf("[DEBUG] Router: Found pending channel for ID %s, sending response\n", respID)
	delete(rc.pending, respID)

	rc.sendResponseToChannel(ch, line, respID)
}

// sendResponseToChannel sends response to the appropriate channel.
func (rc *RouterController) sendResponseToChannel(ch chan []byte, line []byte, respID string) {
	select {
	case ch <- line:
		fmt.Printf("[DEBUG] Router: Successfully sent response to channel for ID %s\n", respID)
		close(ch)
	default:
		fmt.Printf("[DEBUG] Router: Response channel full or closed for ID %s\n", respID)
		rc.logger.Warn("Response channel full or closed", zap.String("id", respID))
		close(ch)
	}
}

// sendToGeneralResponseChannel sends response to general response channel.
func (rc *RouterController) sendToGeneralResponseChannel(line []byte) {
	select {
	case rc.responses <- line:
	case <-rc.ctx.Done():
		return
	default:
		// Channel full, continue
	}
}

// handleStderr logs router's stderr output.
func (rc *RouterController) handleStderr() {
	scanner := bufio.NewScanner(rc.stderr)

	for scanner.Scan() {
		line := scanner.Text()

		// With the router fix, stderr should now only contain actual system errors
		// and zap's ErrorOutputPaths (error-level structured logs)
		var logEntry map[string]interface{}
		if err := json.Unmarshal([]byte(line), &logEntry); err == nil {
			// This is a structured error log from zap's ErrorOutputPaths
			if level, ok := logEntry["level"].(string); ok && strings.ToLower(level) == "error" {
				rc.logger.Error("Router error", zap.String("msg", line))
			} else {
				// Unexpected: non-error structured log in stderr (shouldn't happen with fix)
				rc.logger.Warn("Router unexpected stderr", zap.String("msg", line))
			}
		} else {
			// Non-JSON content in stderr = actual system error, panic, or fatal condition
			rc.logger.Error("Router fatal error", zap.String("error", line))
		}

		// Store logs for testing
		rc.logsMu.Lock()
		rc.stderrLogs = append(rc.stderrLogs, line)
		rc.logsMu.Unlock()
	}

	if err := scanner.Err(); err != nil {
		rc.logger.Error("Error reading router stderr", zap.Error(err))
	}
}

// handleProcess monitors the router process.
func (rc *RouterController) handleProcess() {
	defer close(rc.processDone) // Signal that Wait() has completed

	if rc.cmd == nil {
		return
	}

	err := rc.cmd.Wait()

	rc.mu.Lock()
	rc.started = false
	rc.mu.Unlock()

	switch {
	case err != nil && rc.ctx.Err() == nil:
		rc.logger.Error("Router process exited unexpectedly", zap.Error(err))

		rc.errors <- fmt.Errorf("router process error: %w", err)
	default:
		rc.logger.Info("Router process exited normally")
	}
}

// testBasicConnectivity tests if the gateway health endpoint is accessible.
func (rc *RouterController) testBasicConnectivity() error {
	rc.logger.Info("Testing basic gateway connectivity...")

	client := &http.Client{
		Timeout: readTimeout,
	}

	rc.logger.Info("Testing HTTP health endpoint...")

	ctx := context.Background()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://localhost:9092/health", nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		rc.logger.Error("Failed to connect to gateway health endpoint", zap.Error(err))

		return fmt.Errorf("failed to connect to gateway health endpoint: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		rc.logger.Error("Gateway health check failed", zap.Int("status", resp.StatusCode))

		return fmt.Errorf("gateway health check failed with status: %d", resp.StatusCode)
	}

	rc.logger.Info("Gateway health endpoint is accessible - router should be able to connect")

	return nil
}
