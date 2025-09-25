package direct

import (
	"strings"
	"time"

	"github.com/poiley/mcp-bridge/services/router/pkg/mcp"
	"go.uber.org/zap"
)

// StdioClientBuilder provides a fluent interface for building Stdio clients.
type StdioClientBuilder struct {
	name            string
	serverURL       string
	config          StdioClientConfig
	logger          *zap.Logger
	memoryOptimizer *MemoryOptimizer
}

// CreateStdioBuilder initializes a new Stdio client builder.
func CreateStdioBuilder(name, serverURL string) *StdioClientBuilder {
	return &StdioClientBuilder{
		name:      name,
		serverURL: serverURL,
		config:    StdioClientConfig{},
	}
}

// WithStdioConfig sets the configuration.
func (b *StdioClientBuilder) WithStdioConfig(config StdioClientConfig) *StdioClientBuilder {
	b.config = config

	return b
}

// WithStdioLogger sets the logger.
func (b *StdioClientBuilder) WithStdioLogger(logger *zap.Logger) *StdioClientBuilder {
	b.logger = logger

	return b
}

// WithStdioMemoryOptimizer sets the memory optimizer.
func (b *StdioClientBuilder) WithStdioMemoryOptimizer(optimizer *MemoryOptimizer) *StdioClientBuilder {
	b.memoryOptimizer = optimizer

	return b
}

// BuildStdio creates the Stdio client with all configurations.
func (b *StdioClientBuilder) BuildStdio() (*StdioClient, error) {
	if err := b.parseCommand(); err != nil {
		return nil, err
	}

	b.applyStdioDefaults()

	return b.createStdioClient()
}

// parseCommand extracts the command from the server URL if needed.
func (b *StdioClientBuilder) parseCommand() error {
	if len(b.config.Command) == 0 {
		b.extractCommandFromURL()
	}

	if len(b.config.Command) == 0 {
		return NewConfigError(b.serverURL, "stdio", "command cannot be empty", nil)
	}

	return nil
}

// extractCommandFromURL extracts command from the server URL.
func (b *StdioClientBuilder) extractCommandFromURL() {
	if strings.HasPrefix(b.serverURL, "stdio://") {
		// Remove stdio:// prefix and split command.
		cmdStr := strings.TrimPrefix(b.serverURL, "stdio://")
		b.config.Command = strings.Fields(cmdStr)
	} else {
		// Assume the URL is the command itself.
		b.config.Command = strings.Fields(b.serverURL)
	}
}

// applyStdioDefaults applies all default values.
func (b *StdioClientBuilder) applyStdioDefaults() {
	b.applyTimeoutDefaults()
	b.applyProcessDefaults()
	b.applyPerformanceDefaults()
}

// applyTimeoutDefaults sets timeout-related defaults.
func (b *StdioClientBuilder) applyTimeoutDefaults() {
	if b.config.Timeout == 0 {
		b.config.Timeout = defaultTimeoutSeconds * time.Second
	}

	if b.config.MaxBufferSize == 0 {
		b.config.MaxBufferSize = defaultBufferSize * defaultBufferSize // 1MB
	}

	if b.config.HealthCheck.Interval == 0 {
		b.config.HealthCheck.Interval = defaultTimeoutSeconds * time.Second
	}

	if b.config.HealthCheck.Timeout == 0 {
		b.config.HealthCheck.Timeout = defaultMaxConnections * time.Second
	}
}

// applyProcessDefaults sets process-related defaults.
func (b *StdioClientBuilder) applyProcessDefaults() {
	if b.config.Process.RestartDelay == 0 {
		b.config.Process.RestartDelay = defaultMaxConnections * time.Second
	}

	if b.config.Process.KillTimeout == 0 {
		b.config.Process.KillTimeout = defaultRetryCount * time.Second
	}

	if b.config.Process.MaxRestarts == 0 {
		b.config.Process.MaxRestarts = DefaultMaxRestarts
	}
}

// applyPerformanceDefaults sets performance optimization defaults.
func (b *StdioClientBuilder) applyPerformanceDefaults() {
	if b.config.Performance.StdinBufferSize == 0 {
		b.config.Performance.StdinBufferSize = StdioBufferSizeKB * KilobyteFactor
	}

	if b.config.Performance.StdoutBufferSize == 0 {
		b.config.Performance.StdoutBufferSize = StdioBufferSizeKB * KilobyteFactor
	}

	if !b.config.Performance.EnableBufferedIO {
		b.config.Performance.EnableBufferedIO = true
	}

	if !b.config.Performance.ReuseEncoders {
		b.config.Performance.ReuseEncoders = true
	}
}

// createStdioClient creates the final StdioClient instance.
func (b *StdioClientBuilder) createStdioClient() (*StdioClient, error) {
	logger := b.logger
	if logger != nil {
		logger = logger.With(
			zap.String("component", "stdio_client"),
			zap.String("name", b.name),
		)
	}

	client := &StdioClient{
		name:            b.name,
		url:             b.serverURL,
		config:          b.config,
		logger:          logger,
		memoryOptimizer: b.memoryOptimizer,
		requestMap:      make(map[string]chan *mcp.Response),
		shutdownCh:      make(chan struct{}),
		doneCh:          make(chan struct{}),
		state:           StateDisconnected,
		metrics: ClientMetrics{
			IsHealthy: false,
		},
	}

	return client, nil
}

// InitializeStdioClient is the new descriptive name for NewStdioClientWithMemoryOptimizer.
func InitializeStdioClient(
	name, serverURL string,
	config StdioClientConfig,
	logger *zap.Logger,
	memoryOptimizer *MemoryOptimizer,
) (*StdioClient, error) {
	return CreateStdioBuilder(name, serverURL).
		WithStdioConfig(config).
		WithStdioLogger(logger).
		WithStdioMemoryOptimizer(memoryOptimizer).
		BuildStdio()
}

// EstablishStdioConnection is another descriptive alternative.
func EstablishStdioConnection(
	name, serverURL string,
	config StdioClientConfig,
	logger *zap.Logger,
) (*StdioClient, error) {
	return InitializeStdioClient(name, serverURL, config, logger, nil)
}
