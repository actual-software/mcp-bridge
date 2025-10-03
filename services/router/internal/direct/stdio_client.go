package direct

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/actual-software/mcp-bridge/services/router/pkg/mcp"
	"go.uber.org/zap"
)

// Stdio buffer size constants.
const (
	StdioBufferSize = 64
	RetryDivider    = 2
)

// StdioClientConfig contains configuration for stdio direct client.
type StdioClientConfig struct {
	// Command to execute (required).
	Command []string `mapstructure:"command" yaml:"command"`

	// Working directory for the process.
	WorkingDir string `mapstructure:"working_dir" yaml:"working_dir"`

	// Environment variables.
	Env map[string]string `mapstructure:"env" yaml:"env"`

	// Timeout for requests.
	Timeout time.Duration `mapstructure:"timeout" yaml:"timeout"`

	// Maximum buffer size for stdio.
	MaxBufferSize int `mapstructure:"max_buffer_size" yaml:"max_buffer_size"`

	// Health check configuration.
	HealthCheck HealthCheckConfig `mapstructure:"health_check" yaml:"health_check"`

	// Process management.
	Process ProcessConfig `mapstructure:"process" yaml:"process"`

	// Performance tuning.
	Performance StdioPerformanceConfig `mapstructure:"performance" yaml:"performance"`
}

// StdioPerformanceConfig contains performance optimization settings.
type StdioPerformanceConfig struct {
	// Buffer size for stdin/stdout (default: 64KB).
	StdinBufferSize  int `mapstructure:"stdin_buffer_size"  yaml:"stdin_buffer_size"`
	StdoutBufferSize int `mapstructure:"stdout_buffer_size" yaml:"stdout_buffer_size"`

	// Enable buffered I/O for better performance.
	EnableBufferedIO bool `mapstructure:"enable_buffered_io" yaml:"enable_buffered_io"`

	// Enable JSON encoder/decoder reuse.
	ReuseEncoders bool `mapstructure:"reuse_encoders" yaml:"reuse_encoders"`

	// Process priority (-20 to 19, lower is higher priority).
	ProcessPriority int `mapstructure:"process_priority" yaml:"process_priority"`
}

// ProcessConfig contains process management settings.
type ProcessConfig struct {
	MaxRestarts  int           `mapstructure:"max_restarts"  yaml:"max_restarts"`
	RestartDelay time.Duration `mapstructure:"restart_delay" yaml:"restart_delay"`
	KillTimeout  time.Duration `mapstructure:"kill_timeout"  yaml:"kill_timeout"`
}

// StdioClient implements DirectClient for stdio-based MCP servers.
type StdioClient struct {
	name   string
	url    string // Command string representation
	config StdioClientConfig
	logger *zap.Logger

	// Process management.
	cmd           *exec.Cmd
	stdin         io.WriteCloser
	stdout        io.ReadCloser
	stderr        io.ReadCloser
	stdinEncoder  *json.Encoder
	stdoutDecoder *json.Decoder

	// Buffered I/O for performance.
	stdinBuf  *bufio.Writer
	stdoutBuf *bufio.Reader

	// Memory optimization.
	memoryOptimizer *MemoryOptimizer

	// State management.
	mu        sync.RWMutex
	state     ConnectionState
	startTime time.Time
	restarts  int

	// Request correlation.
	requestMap   map[string]chan *mcp.Response
	requestMapMu sync.RWMutex
	requestID    uint64

	// Write synchronization.
	writeMu sync.Mutex

	// Metrics.
	metrics   ClientMetrics
	metricsMu sync.RWMutex

	// Shutdown coordination.
	shutdownCh chan struct{}
	doneCh     chan struct{}
	wg         sync.WaitGroup
}

// NewStdioClient creates a new stdio direct client.
// Deprecated: Use InitializeStdioClient or EstablishStdioConnection for better naming.
func NewStdioClient(name, serverURL string, config StdioClientConfig, logger *zap.Logger) (*StdioClient, error) {
	return InitializeStdioClient(name, serverURL, config, logger, nil)
}

// NewStdioClientWithMemoryOptimizer creates a new stdio direct client with memory optimization.
// Deprecated: Use InitializeStdioClient for better naming.
func NewStdioClientWithMemoryOptimizer(
	name, serverURL string,
	config StdioClientConfig,
	logger *zap.Logger,
	memoryOptimizer *MemoryOptimizer,
) (*StdioClient, error) {
	return InitializeStdioClient(name, serverURL, config, logger, memoryOptimizer)
}

// Connect establishes connection to the stdio MCP server.
func (c *StdioClient) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.state == StateConnected || c.state == StateHealthy {
		return NewConnectionError(c.url, "stdio", "client already connected", ErrClientAlreadyConnected)
	}

	c.state = StateConnecting

	if err := c.startProcess(ctx); err != nil {
		c.state = StateError

		return NewProcessError(c.url, "stdio", "failed to start process", err)
	}

	c.state = StateConnected
	c.startTime = time.Now()
	c.updateMetrics(func(m *ClientMetrics) {
		m.ConnectionTime = time.Now()
		m.IsHealthy = true
	})

	// Start background routines.
	c.wg.Add(1)

	go c.readResponses()

	if c.config.HealthCheck.Enabled {
		c.wg.Add(1)

		go c.healthCheckLoop(ctx)
	}

	c.logger.Info("stdio client connected successfully",
		zap.Strings("command", c.config.Command),
		zap.String("working_dir", c.config.WorkingDir),
		zap.Int("pid", c.cmd.Process.Pid))

	return nil
}

// startProcess creates and starts the stdio process.
func (c *StdioClient) startProcess(ctx context.Context) error {
	builder := InitializeStdioProcess(c)

	return builder.BuildAndStartProcess(ctx)
}

// SendRequest sends an MCP request and returns the response.
func (c *StdioClient) SendRequest(ctx context.Context, req *mcp.Request) (*mcp.Response, error) {
	handler := CreateStdioRequestHandler(c)

	return handler.ProcessRequest(ctx, req)
}

// readResponses continuously reads responses from stdout.
func (c *StdioClient) readResponses() {
	reader := CreateStdioResponseReader(c)
	reader.ReadResponseLoop()
}

// monitorStderr logs stderr output from the process.
func (c *StdioClient) monitorStderr() {
	defer c.wg.Done()

	c.mu.RLock()
	stderr := c.stderr
	c.mu.RUnlock()

	if stderr == nil {
		return
	}

	scanner := bufio.NewScanner(stderr)

	for {
		select {
		case <-c.shutdownCh:
			return
		default:
		}

		// Set read timeout on stderr to avoid blocking indefinitely.
		if deadliner, ok := stderr.(interface{ SetReadDeadline(time.Time) error }); ok {
			_ = deadliner.SetReadDeadline(time.Now().Add(time.Second))
		}

		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				// Check if it's a timeout error and continue if we're not shutting down.
				var netErr net.Error
				if errors.As(err, &netErr) {
					select {
					case <-c.shutdownCh:
						return
					default:
						continue
					}
				}

				c.logger.Error("error reading stderr", zap.Error(err))
			}
			// EOF or scanner finished.
			return
		}

		line := scanner.Text()
		c.logger.Warn("process stderr",
			zap.String("line", line),
			zap.String("client_name", c.name))
	}
}

// Health checks if the client connection is healthy.
func (c *StdioClient) Health(ctx context.Context) error {
	c.mu.RLock()

	if c.state != StateConnected && c.state != StateHealthy {
		c.mu.RUnlock()

		return NewHealthError(c.url, "stdio", "client not connected", ErrClientNotConnected)
	}

	c.mu.RUnlock()

	// Check if process is still alive.
	if c.cmd.Process != nil {
		if err := c.cmd.Process.Signal(syscall.Signal(0)); err != nil {
			c.mu.Lock()
			c.state = StateUnhealthy
			c.mu.Unlock()
			c.updateMetrics(func(m *ClientMetrics) {
				m.IsHealthy = false
				m.LastHealthCheck = time.Now()
			})

			return NewHealthError(c.url, "stdio", "process not responding", err)
		}
	}

	// For stdio health checks, we only verify the process is alive.
	// Don't send ping requests to avoid circular locking during shutdown.
	// The process signal check above is sufficient for stdio connections.

	c.mu.Lock()
	c.state = StateHealthy
	c.mu.Unlock()
	c.updateMetrics(func(m *ClientMetrics) {
		m.IsHealthy = true
		m.LastHealthCheck = time.Now()
	})

	return nil
}

// healthCheckLoop runs periodic health checks.
func (c *StdioClient) healthCheckLoop(parentCtx context.Context) {
	defer c.wg.Done()

	ticker := time.NewTicker(c.config.HealthCheck.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-parentCtx.Done():
			return
		case <-c.shutdownCh:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(parentCtx, c.config.HealthCheck.Timeout)
			if err := c.Health(ctx); err != nil {
				c.logger.Warn("health check failed", zap.Error(err))
				// Consider restarting the process if it's unhealthy.
				if c.restarts < c.config.Process.MaxRestarts {
					c.logger.Info("attempting to restart unhealthy process",
						zap.Int("restart_count", c.restarts),
						zap.Int("max_restarts", c.config.Process.MaxRestarts))

					go c.restartProcess(parentCtx)
				}
			}

			cancel()
		}
	}
}

// restartProcess attempts to restart the stdio process.
func (c *StdioClient) restartProcess(ctx context.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.restarts >= c.config.Process.MaxRestarts {
		c.logger.Error("maximum restart attempts reached",
			zap.Int("restarts", c.restarts),
			zap.Int("max_restarts", c.config.Process.MaxRestarts))
		c.state = StateError

		return
	}

	c.restarts++
	c.state = StateConnecting

	// Stop current process.
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}

	// Wait for restart delay.
	time.Sleep(c.config.Process.RestartDelay)

	// Restart process.
	startCtx, cancel := context.WithTimeout(ctx, defaultTimeoutSeconds*time.Second)
	defer cancel()

	if err := c.startProcess(startCtx); err != nil {
		c.logger.Error("failed to restart process", zap.Error(err))
		c.state = StateError

		return
	}

	c.state = StateConnected
	c.logger.Info("process restarted successfully",
		zap.Int("restart_count", c.restarts))
}

// Close gracefully closes the client connection.
// Close gracefully shuts down the stdio client.
func (c *StdioClient) Close(ctx context.Context) error {
	closer := CreateStdioCloser(c)

	return closer.GracefulShutdown(ctx)
}

// GetName returns the client name.
func (c *StdioClient) GetName() string {
	return c.name
}

// GetProtocol returns the protocol type.
func (c *StdioClient) GetProtocol() string {
	return "stdio"
}

// GetMetrics returns current client metrics.
func (c *StdioClient) GetMetrics() ClientMetrics {
	c.metricsMu.RLock()
	defer c.metricsMu.RUnlock()

	return c.metrics
}

// updateMetrics safely updates client metrics.
func (c *StdioClient) updateMetrics(updateFn func(*ClientMetrics)) {
	c.metricsMu.Lock()
	updateFn(&c.metrics)
	c.metricsMu.Unlock()
}

// GetState returns the current connection state.
func (c *StdioClient) GetState() ConnectionState {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.state
}

// GetStatus returns detailed client status.
func (c *StdioClient) GetStatus() DirectClientStatus {
	c.mu.RLock()
	state := c.state
	c.mu.RUnlock()

	metrics := c.GetMetrics()

	// Convert ClientMetrics to DetailedClientMetrics.
	detailedMetrics := DetailedClientMetrics{
		RequestCount:    metrics.RequestCount,
		ErrorCount:      metrics.ErrorCount,
		AverageLatency:  metrics.AverageLatency,
		LastHealthCheck: metrics.LastHealthCheck,
		IsHealthy:       metrics.IsHealthy,
		ConnectionTime:  metrics.ConnectionTime,
		LastUsed:        metrics.LastUsed,

		// Set defaults for extended metrics.
		Protocol:          "stdio",
		ServerURL:         c.url,
		ConnectionState:   state,
		ErrorsByType:      make(map[string]uint64),
		HealthHistory:     []HealthCheckResult{},
		CreatedAt:         metrics.ConnectionTime,
		LastMetricsUpdate: time.Now(),
	}

	status := DirectClientStatus{
		Name:            c.name,
		URL:             c.url,
		Protocol:        "stdio",
		State:           state,
		LastConnected:   metrics.ConnectionTime,
		LastHealthCheck: metrics.LastHealthCheck,
		Metrics:         detailedMetrics,
		Configuration:   make(map[string]interface{}),
		RuntimeInfo: ClientRuntimeInfo{
			ConnectionID: fmt.Sprintf("stdio-%s-%d", c.name, metrics.ConnectionTime.Unix()),
			ResourceUsage: ResourceUsage{
				LastUpdated: time.Now(),
			},
		},
	}

	return status
}
