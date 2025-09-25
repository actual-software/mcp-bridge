package stdio



import (
	"bufio"
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/poiley/mcp-bridge/services/gateway/internal/errors"
	"github.com/poiley/mcp-bridge/services/gateway/internal/metrics"
	"github.com/poiley/mcp-bridge/services/router/pkg/mcp"

	"go.uber.org/zap"
)

const (
	defaultTimeoutSeconds = 30
	defaultRetryCount     = 10
	defaultBufferSize     = 1024
	defaultMaxConnections = 5
	averagingDivisor      = 2 // For simple moving average
)

// BackendMetrics contains performance and health metrics for a backend.
type BackendMetrics struct {
	RequestCount    uint64        `json:"request_count"`
	ErrorCount      uint64        `json:"error_count"`
	AverageLatency  time.Duration `json:"average_latency"`
	LastHealthCheck time.Time     `json:"last_health_check"`
	IsHealthy       bool          `json:"is_healthy"`
	ConnectionTime  time.Time     `json:"connection_time"`
}

// Config contains stdio backend configuration.
type Config struct {
	Command       []string          `mapstructure:"command"`
	WorkingDir    string            `mapstructure:"working_dir"`
	Env           map[string]string `mapstructure:"env"`
	HealthCheck   HealthCheckConfig `mapstructure:"health_check"`
	Process       ProcessConfig     `mapstructure:"process"`
	Timeout       time.Duration     `mapstructure:"timeout"`
	MaxBufferSize int               `mapstructure:"max_buffer_size"`
}

// HealthCheckConfig contains health check settings.
type HealthCheckConfig struct {
	Enabled  bool          `mapstructure:"enabled"`
	Interval time.Duration `mapstructure:"interval"`
	Timeout  time.Duration `mapstructure:"timeout"`
}

// ProcessConfig contains process management settings.
type ProcessConfig struct {
	MaxRestarts  int           `mapstructure:"max_restarts"`
	RestartDelay time.Duration `mapstructure:"restart_delay"`
}

// Backend implements the stdio backend for MCP servers.
type Backend struct {
	name            string
	config          Config
	logger          *zap.Logger
	metricsRegistry *metrics.Registry

	// Process management
	cmd           *exec.Cmd
	stdin         io.WriteCloser
	stdout        io.ReadCloser
	stderr        io.ReadCloser
	stdinEncoder  *json.Encoder
	stdoutDecoder *json.Decoder

	// State management
	mu        sync.RWMutex
	running   bool
	startTime time.Time

	// Request correlation
	requestMap   map[string]chan *mcp.Response
	requestMapMu sync.RWMutex
	requestID    uint64

	// Metrics
	metrics    BackendMetrics
	mu_metrics sync.RWMutex

	// Channels for shutdown coordination
	shutdownCh chan struct{}
	doneCh     chan struct{}
}

// CreateStdioBackend creates a stdio-based backend instance for process communication.
func CreateStdioBackend(name string, config Config, logger *zap.Logger, metricsRegistry *metrics.Registry) *Backend {
	if config.Timeout == 0 {
		config.Timeout = defaultTimeoutSeconds * time.Second
	}

	if config.MaxBufferSize == 0 {
		config.MaxBufferSize = defaultBufferSize * defaultBufferSize // 1MB
	}

	if config.HealthCheck.Interval == 0 {
		config.HealthCheck.Interval = defaultTimeoutSeconds * time.Second
	}

	if config.HealthCheck.Timeout == 0 {
		config.HealthCheck.Timeout = defaultMaxConnections * time.Second
	}

	if config.Process.RestartDelay == 0 {
		config.Process.RestartDelay = defaultMaxConnections * time.Second
	}

	return &Backend{
		name:            name,
		config:          config,
		logger:          logger.With(zap.String("backend", name), zap.String("protocol", "stdio")),
		metricsRegistry: metricsRegistry,
		requestMap:      make(map[string]chan *mcp.Response),
		shutdownCh:      make(chan struct{}),
		doneCh:          make(chan struct{}),
		metrics: BackendMetrics{
			IsHealthy: false,
		},
	}
}

// Start initializes the stdio backend process.
func (b *Backend) Start(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.running {
		return errors.New(errors.TypeInternal, "backend already running").
			WithComponent("backend_stdio").
			WithContext("backend_name", b.name)
	}

	if err := b.startProcess(ctx); err != nil {
		return errors.WrapStdioError(ctx, err, "start_process").
			WithContext("command", b.config.Command)
	}

	b.running = true
	b.startTime = time.Now()
	b.updateMetrics(func(m *BackendMetrics) {
		m.ConnectionTime = time.Now()
		m.IsHealthy = true
	})

	// Start background routines
	go b.readResponses()

	if b.config.HealthCheck.Enabled {
		go b.healthCheckLoop(ctx)
	}

	b.logger.Info("stdio backend started successfully",
		zap.Strings("command", b.config.Command),
		zap.String("working_dir", b.config.WorkingDir))

	return nil
}

// startProcess creates and starts the stdio process.
func (b *Backend) startProcess(ctx context.Context) error {
	if len(b.config.Command) == 0 {
		return errors.New(errors.TypeValidation, "command cannot be empty").
			WithComponent("backend_stdio").
			WithContext("backend_name", b.name)
	}

	// G204: Command arguments are from validated configuration, not user input
	b.cmd = exec.CommandContext(ctx, b.config.Command[0], b.config.Command[1:]...) //nolint:gosec 

	// Set working directory
	if b.config.WorkingDir != "" {
		b.cmd.Dir = b.config.WorkingDir
	}

	// Set environment variables
	b.cmd.Env = os.Environ()
	for key, value := range b.config.Env {
		b.cmd.Env = append(b.cmd.Env, fmt.Sprintf("%s=%s", key, value))
	}

	// Create pipes
	stdin, err := b.cmd.StdinPipe()
	if err != nil {
		return errors.WrapStdioError(ctx, err, "create_stdin_pipe")
	}

	b.stdin = stdin
	b.stdinEncoder = json.NewEncoder(stdin)

	stdout, err := b.cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close() 

		return errors.WrapStdioError(ctx, err, "create_stdout_pipe")
	}

	b.stdout = stdout
	b.stdoutDecoder = json.NewDecoder(stdout)

	stderr, err := b.cmd.StderrPipe()
	if err != nil {
		_ = stdin.Close()  
		_ = stdout.Close() 

		return errors.WrapStdioError(ctx, err, "create_stderr_pipe")
	}

	b.stderr = stderr

	// Start the process
	if err := b.cmd.Start(); err != nil {
		_ = stdin.Close()  
		_ = stdout.Close() 
		_ = stderr.Close() 

		return errors.WrapStdioError(ctx, err, "start")
	}

	// Monitor stderr in background
	go b.monitorStderr()

	b.logger.Info("process started", zap.Int("pid", b.cmd.Process.Pid))

	return nil
}

// readResponses continuously reads responses from stdout.
func (b *Backend) readResponses() {
	defer close(b.doneCh)

	for {
		select {
		case <-b.shutdownCh:
			return
		default:
		}

		var response mcp.Response
		if err := b.stdoutDecoder.Decode(&response); err != nil {
			if err == io.EOF {
				b.logger.Info("process stdout closed")

				return
			}

			b.logger.Error("failed to decode response",
				zap.Error(err),
				zap.String("backend_name", b.name),
				zap.String("protocol", "stdio"),
				zap.Duration("uptime", time.Since(b.startTime)))
			b.updateMetrics(func(m *BackendMetrics) {
				m.ErrorCount++
				m.IsHealthy = false
			})

			continue
		}

		// Route response to waiting request
		b.requestMapMu.RLock()

		if responseID, ok := response.ID.(string); ok {
			if ch, exists := b.requestMap[responseID]; exists {
				select {
				case ch <- &response:
				default:
					b.logger.Warn("response channel full, dropping response",
						zap.String("request_id", responseID),
						zap.String("backend_name", b.name),
						zap.Uint64("active_requests", uint64(len(b.requestMap))))
				}
			} else {
				b.logger.Warn("received response for unknown request", zap.String("request_id", responseID))
			}
		}

		b.requestMapMu.RUnlock()
	}
}

// monitorStderr logs stderr output from the process.
func (b *Backend) monitorStderr() {
	scanner := bufio.NewScanner(b.stderr)
	for scanner.Scan() {
		line := scanner.Text()
		b.logger.Warn("process stderr", zap.String("line", line))
	}

	if err := scanner.Err(); err != nil {
		b.logger.Error("error reading stderr", zap.Error(err))
	}
}

// SendRequest sends an MCP request to the backend process.
func (b *Backend) SendRequest(ctx context.Context, req *mcp.Request) (*mcp.Response, error) {
	if err := b.checkRunning(); err != nil {
		b.recordErrorMetric(err)

		return nil, err
	}

	// Ensure request has an ID
	requestID := b.ensureRequestID(req)

	// Setup response channel
	respCh := b.setupResponseChannel(requestID)
	defer b.cleanupResponseChannel(requestID, respCh)

	// Send the request
	if err := b.sendRequestToProcess(ctx, req, requestID); err != nil {
		b.recordErrorMetric(err)

		return nil, err
	}

	// Wait for and return response
	resp, err := b.waitForResponse(ctx, respCh, requestID, req.Method)
	if err != nil {
		b.recordErrorMetric(err)

		return nil, err
	}
	
	return resp, nil
}

// checkRunning verifies the backend is running.
func (b *Backend) checkRunning() error {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if !b.running {
		return errors.CreateBackendUnavailableError(b.name, "not running")
	}

	return nil
}

// ensureRequestID generates or extracts a request ID.
func (b *Backend) ensureRequestID(req *mcp.Request) string {
	if req.ID == nil || req.ID == "" {
		requestID := fmt.Sprintf("%s-%d", b.name, atomic.AddUint64(&b.requestID, 1))
		req.ID = requestID

		return requestID
	}

	if requestID, ok := req.ID.(string); ok {
		return requestID
	}

	return fmt.Sprintf("%v", req.ID)
}

// setupResponseChannel creates and registers a response channel.
func (b *Backend) setupResponseChannel(requestID string) chan *mcp.Response {
	respCh := make(chan *mcp.Response, 1)

	b.requestMapMu.Lock()
	b.requestMap[requestID] = respCh
	b.requestMapMu.Unlock()

	return respCh
}

// cleanupResponseChannel removes the response channel from the map.
func (b *Backend) cleanupResponseChannel(requestID string, respCh chan *mcp.Response) {
	b.requestMapMu.Lock()
	delete(b.requestMap, requestID)
	close(respCh)
	b.requestMapMu.Unlock()
}

// sendRequestToProcess encodes and sends the request to the stdio process.
func (b *Backend) sendRequestToProcess(ctx context.Context, req *mcp.Request, requestID string) error {
	if err := b.stdinEncoder.Encode(req); err != nil {
		b.updateMetrics(func(m *BackendMetrics) {
			m.ErrorCount++
		})

		return errors.WrapWriteError(ctx, err, b.name, 0).
			WithContext("request_id", requestID).
			WithContext("method", req.Method)
	}

	return nil
}

// waitForResponse waits for a response with timeout handling.
func (b *Backend) waitForResponse(
	ctx context.Context, respCh chan *mcp.Response, requestID, method string,
) (*mcp.Response, error) {
	startTime := time.Now()
	timeout := b.calculateTimeout(ctx)

	select {
	case resp := <-respCh:
		b.recordSuccessMetrics(startTime)

		return resp, nil
	case <-time.After(timeout):
		b.recordErrorMetrics()

		return nil, errors.CreateConnectionTimeoutError(b.name, timeout).
			WithContext("request_id", requestID).
			WithContext("method", method)
	case <-ctx.Done():
		b.recordErrorMetrics()

		return nil, ctx.Err()
	}
}

// calculateTimeout determines the appropriate timeout for the request.
func (b *Backend) calculateTimeout(ctx context.Context) time.Duration {
	timeout := b.config.Timeout
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining < timeout {
			timeout = remaining
		}
	}

	return timeout
}

// recordSuccessMetrics updates metrics for a successful request.
func (b *Backend) recordSuccessMetrics(startTime time.Time) {
	duration := time.Since(startTime)

	b.updateMetrics(func(m *BackendMetrics) {
		m.RequestCount++
		// Simple moving average for latency
		if m.RequestCount == 1 {
			m.AverageLatency = duration
		} else {
			m.AverageLatency = (m.AverageLatency + duration) / averagingDivisor
		}
	})
}

// recordErrorMetrics updates metrics for a failed request.
func (b *Backend) recordErrorMetrics() {
	b.updateMetrics(func(m *BackendMetrics) {
		m.ErrorCount++
	})
}

// recordErrorMetric updates metrics for a failed request and records to Prometheus.
func (b *Backend) recordErrorMetric(err error) {
	b.updateMetrics(func(m *BackendMetrics) {
		m.ErrorCount++
	})
	
	// Record to Prometheus metrics if available
	if b.metricsRegistry != nil && err != nil {
		// Check if it's already a GatewayError
		var gatewayErr *errors.GatewayError
		if !stderrors.As(err, &gatewayErr) {
			// Wrap the error with backend context
			gatewayErr = errors.Wrap(err, "stdio backend error").
				WithComponent("stdio_backend").
				WithContext("backend_name", b.name)
		} else {
			// Ensure component is set
			gatewayErr = gatewayErr.WithComponent("stdio_backend").
				WithContext("backend_name", b.name)
		}
		
		errors.RecordError(gatewayErr, b.metricsRegistry)
	}
}

// Health checks if the backend process is healthy.
func (b *Backend) Health(ctx context.Context) error {
	b.mu.RLock()

	if !b.running {
		b.mu.RUnlock()

		return errors.CreateBackendUnavailableError(b.name, "not running")
	}

	b.mu.RUnlock()

	// Check if process is still alive
	if b.cmd.Process != nil {
		if err := b.cmd.Process.Signal(syscall.Signal(0)); err != nil {
			return errors.CreateBackendUnavailableError(b.name, fmt.Sprintf("process not responding: %v", err))
		}
	}

	// Optionally send a ping request
	if b.config.HealthCheck.Enabled {
		pingReq := &mcp.Request{
			Method: "ping",
			ID:     fmt.Sprintf("health-%d", time.Now().UnixNano()),
		}

		healthCtx, cancel := context.WithTimeout(ctx, b.config.HealthCheck.Timeout)
		defer cancel()

		_, err := b.SendRequest(healthCtx, pingReq)
		if err != nil {
			b.updateMetrics(func(m *BackendMetrics) {
				m.IsHealthy = false
				m.LastHealthCheck = time.Now()
			})

			return errors.WrapStdioError(ctx, err, "health_check")
		}
	}

	b.updateMetrics(func(m *BackendMetrics) {
		m.IsHealthy = true
		m.LastHealthCheck = time.Now()
	})

	return nil
}

// healthCheckLoop runs periodic health checks.
func (b *Backend) healthCheckLoop(ctx context.Context) {
	ticker := time.NewTicker(b.config.HealthCheck.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-b.shutdownCh:
			return
		case <-ticker.C:
			healthCtx, cancel := context.WithTimeout(ctx, b.config.HealthCheck.Timeout)
			if err := b.Health(healthCtx); err != nil {
				b.logger.Warn("health check failed", zap.Error(err))
			}

			cancel()
		}
	}
}

// Stop gracefully shuts down the backend process.
func (b *Backend) Stop(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.running {
		return nil
	}

	b.logger.Info("stopping stdio backend")

	// Signal shutdown and close stdin
	b.initiateShutdown()

	// Wait for process to exit
	if err := b.waitForProcessExit(ctx); err != nil {
		return err
	}

	// Wait for response reader to finish
	b.waitForResponseReader()

	// Cleanup resources
	b.cleanupResources()

	b.logger.Info("stdio backend stopped")

	return nil
}

// initiateShutdown signals the backend to start shutting down.
func (b *Backend) initiateShutdown() {
	close(b.shutdownCh)

	// Close stdin to signal the process to shutdown
	if b.stdin != nil {
		_ = b.stdin.Close() 
	}
}

// waitForProcessExit waits for the backend process to exit gracefully.
func (b *Backend) waitForProcessExit(ctx context.Context) error {
	done := make(chan error, 1)

	go func() {
		done <- b.cmd.Wait()
	}()

	select {
	case err := <-done:
		if err != nil {
			b.logger.Warn("process exited with error", zap.Error(err))
		}

		return nil
	case <-time.After(defaultRetryCount * time.Second):
		// Force kill if graceful shutdown takes too long
		b.forceKillProcess()
		<-done // Wait for the kill to complete

		return nil
	case <-ctx.Done():
		b.forceKillProcess()

		return ctx.Err()
	}
}

// forceKillProcess forcefully terminates the backend process.
func (b *Backend) forceKillProcess() {
	b.logger.Warn("force killing process")

	if b.cmd.Process != nil {
		_ = b.cmd.Process.Kill() 
	}
}

// waitForResponseReader waits for the response reader goroutine to finish.
func (b *Backend) waitForResponseReader() {
	select {
	case <-b.doneCh:
	case <-time.After(defaultMaxConnections * time.Second):
		b.logger.Warn("response reader did not finish in time")
	}
}

// cleanupResources closes pipes and updates metrics.
func (b *Backend) cleanupResources() {
	// Close remaining pipes
	if b.stdout != nil {
		_ = b.stdout.Close() 
	}

	if b.stderr != nil {
		_ = b.stderr.Close() 
	}

	b.running = false
	b.updateMetrics(func(m *BackendMetrics) {
		m.IsHealthy = false
	})
}

// GetName returns the backend name.
func (b *Backend) GetName() string {
	return b.name
}

// GetProtocol returns the backend protocol.
func (b *Backend) GetProtocol() string {
	return "stdio"
}

// GetMetrics returns current backend metrics.
func (b *Backend) GetMetrics() BackendMetrics {
	b.mu_metrics.RLock()
	defer b.mu_metrics.RUnlock()

	return b.metrics
}

// updateMetrics safely updates backend metrics.
func (b *Backend) updateMetrics(updateFn func(*BackendMetrics)) {
	b.mu_metrics.Lock()
	updateFn(&b.metrics)
	b.mu_metrics.Unlock()
}
