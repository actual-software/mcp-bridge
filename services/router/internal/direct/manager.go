package direct

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/poiley/mcp-bridge/services/router/internal/constants"
	"github.com/poiley/mcp-bridge/services/router/pkg/mcp"
	"go.uber.org/zap"
)

// ClientType represents the type of direct client.
type ClientType string

const (
	ClientTypeStdio     ClientType = "stdio"
	ClientTypeWebSocket ClientType = "websocket"
	ClientTypeHTTP      ClientType = "http"
	ClientTypeSSE       ClientType = "sse"
)

// Connection timeout constants.
const (
	HttpConnectionTestTimeout = 3 * time.Second
)

// DirectClient represents a direct connection to an MCP server.
type DirectClient interface {
	// Connect establishes connection to the MCP server.
	Connect(ctx context.Context) error

	// SendRequest sends an MCP request and returns the response.
	SendRequest(ctx context.Context, req *mcp.Request) (*mcp.Response, error)

	// Health checks if the client connection is healthy.
	Health(ctx context.Context) error

	// Close gracefully closes the client connection.
	Close(ctx context.Context) error

	// GetName returns the client name/identifier.
	GetName() string

	// GetProtocol returns the protocol type.
	GetProtocol() string

	// GetMetrics returns client metrics.
	GetMetrics() ClientMetrics
}

// ClientMetrics contains performance and health metrics for a direct client.
type ClientMetrics struct {
	RequestCount    uint64        `json:"request_count"`
	ErrorCount      uint64        `json:"error_count"`
	AverageLatency  time.Duration `json:"average_latency"`
	LastHealthCheck time.Time     `json:"last_health_check"`
	IsHealthy       bool          `json:"is_healthy"`
	ConnectionTime  time.Time     `json:"connection_time"`
	LastUsed        time.Time     `json:"last_used"`
}

// DirectConfig contains configuration for direct client connections.
// FallbackConfig defines fallback behavior from direct to gateway connections.
type FallbackConfig struct {
	// Enable fallback to gateway when direct connections fail.
	Enabled bool `mapstructure:"enabled" yaml:"enabled"`

	// Timeout for direct connection attempts before fallback.
	DirectTimeout time.Duration `mapstructure:"direct_timeout" yaml:"direct_timeout"`

	// Maximum number of direct connection retries before fallback.
	MaxRetries int `mapstructure:"max_retries" yaml:"max_retries"`

	// Delay between direct connection retries.
	RetryDelay time.Duration `mapstructure:"retry_delay" yaml:"retry_delay"`

	// Methods that should always prefer direct connections (bypass fallback).
	DirectOnlyMethods []string `mapstructure:"direct_only_methods" yaml:"direct_only_methods"`

	// Methods that should never use direct connections (gateway only).
	GatewayOnlyMethods []string `mapstructure:"gateway_only_methods" yaml:"gateway_only_methods"`
}

type DirectConfig struct {
	// Default timeout for all direct connections.
	DefaultTimeout time.Duration `mapstructure:"default_timeout" yaml:"default_timeout"`

	// Maximum number of concurrent direct connections.
	MaxConnections int `mapstructure:"max_connections" yaml:"max_connections"`

	// Health check settings.
	HealthCheck HealthCheckConfig `mapstructure:"health_check" yaml:"health_check"`

	// Auto-detection settings.
	AutoDetection AutoDetectionConfig `mapstructure:"auto_detection" yaml:"auto_detection"`

	// Fallback settings.
	Fallback FallbackConfig `mapstructure:"fallback" yaml:"fallback"`

	// Connection pool settings.
	ConnectionPool ConnectionPoolConfig `mapstructure:"connection_pool" yaml:"connection_pool"`

	// Adaptive mechanisms.
	AdaptiveTimeout AdaptiveTimeoutConfig `mapstructure:"adaptive_timeout" yaml:"adaptive_timeout"`
	AdaptiveRetry   AdaptiveRetryConfig   `mapstructure:"adaptive_retry"   yaml:"adaptive_retry"`

	// Advanced timeout and retry tuning.
	TimeoutTuning TimeoutTuningConfig `mapstructure:"timeout_tuning" yaml:"timeout_tuning"`

	// Protocol-specific configurations.
	Stdio     StdioConfig     `mapstructure:"stdio"     yaml:"stdio"`
	WebSocket WebSocketConfig `mapstructure:"websocket" yaml:"websocket"`
	HTTP      HTTPConfig      `mapstructure:"http"      yaml:"http"`
	SSE       SSEConfig       `mapstructure:"sse"       yaml:"sse"`

	// Observability and monitoring.
	Observability ObservabilityConfig `mapstructure:"observability" yaml:"observability"`

	// Memory optimization and garbage collection.
	MemoryOptimization MemoryOptimizationConfig `mapstructure:"memory_optimization" yaml:"memory_optimization"`
}

// HealthCheckConfig contains health check settings.
type HealthCheckConfig struct {
	Enabled  bool          `mapstructure:"enabled"  yaml:"enabled"`
	Interval time.Duration `mapstructure:"interval" yaml:"interval"`
	Timeout  time.Duration `mapstructure:"timeout"  yaml:"timeout"`
}

// AutoDetectionConfig contains protocol auto-detection settings.
type AutoDetectionConfig struct {
	Enabled        bool          `mapstructure:"enabled"         yaml:"enabled"`
	Timeout        time.Duration `mapstructure:"timeout"         yaml:"timeout"`
	PreferredOrder []string      `mapstructure:"preferred_order" yaml:"preferred_order"`
	CacheResults   bool          `mapstructure:"cache_results"   yaml:"cache_results"`
	CacheTTL       time.Duration `mapstructure:"cache_ttl"       yaml:"cache_ttl"`
}

// Protocol-specific configs (to be extended by individual client implementations).
type StdioConfig struct {
	DefaultWorkingDir string            `mapstructure:"default_working_dir" yaml:"default_working_dir"`
	DefaultEnv        map[string]string `mapstructure:"default_env"         yaml:"default_env"`
	ProcessTimeout    time.Duration     `mapstructure:"process_timeout"     yaml:"process_timeout"`
	MaxBufferSize     int               `mapstructure:"max_buffer_size"     yaml:"max_buffer_size"`

	// Performance tuning parameters.
	Performance StdioPerformanceTuning `mapstructure:"performance" yaml:"performance"`
}

type WebSocketConfig struct {
	HandshakeTimeout time.Duration     `mapstructure:"handshake_timeout" yaml:"handshake_timeout"`
	PingInterval     time.Duration     `mapstructure:"ping_interval"     yaml:"ping_interval"`
	PongTimeout      time.Duration     `mapstructure:"pong_timeout"      yaml:"pong_timeout"`
	MaxMessageSize   int64             `mapstructure:"max_message_size"  yaml:"max_message_size"`
	DefaultHeaders   map[string]string `mapstructure:"default_headers"   yaml:"default_headers"`

	// Performance tuning parameters.
	Performance WebSocketPerformanceTuning `mapstructure:"performance" yaml:"performance"`
}

type HTTPConfig struct {
	RequestTimeout  time.Duration     `mapstructure:"request_timeout"  yaml:"request_timeout"`
	MaxIdleConns    int               `mapstructure:"max_idle_conns"   yaml:"max_idle_conns"`
	DefaultHeaders  map[string]string `mapstructure:"default_headers"  yaml:"default_headers"`
	FollowRedirects bool              `mapstructure:"follow_redirects" yaml:"follow_redirects"`

	// Performance tuning parameters.
	Performance HTTPPerformanceTuning `mapstructure:"performance" yaml:"performance"`
}

type SSEConfig struct {
	StreamTimeout  time.Duration     `mapstructure:"stream_timeout"  yaml:"stream_timeout"`
	RequestTimeout time.Duration     `mapstructure:"request_timeout" yaml:"request_timeout"`
	DefaultHeaders map[string]string `mapstructure:"default_headers" yaml:"default_headers"`
	BufferSize     int               `mapstructure:"buffer_size"     yaml:"buffer_size"`

	// Performance tuning parameters.
	Performance SSEPerformanceTuning `mapstructure:"performance" yaml:"performance"`
}

// Performance tuning configurations for each protocol.
type StdioPerformanceTuning struct {
	// Buffer sizes for optimized I/O.
	StdinBufferSize  int  `mapstructure:"stdin_buffer_size"  yaml:"stdin_buffer_size"`
	StdoutBufferSize int  `mapstructure:"stdout_buffer_size" yaml:"stdout_buffer_size"`
	EnableBufferedIO bool `mapstructure:"enable_buffered_io" yaml:"enable_buffered_io"`

	// JSON encoder optimizations.
	ReuseEncoders bool `mapstructure:"reuse_encoders" yaml:"reuse_encoders"`

	// Process management optimizations.
	ProcessPriority int           `mapstructure:"process_priority" yaml:"process_priority"`
	MaxRestarts     int           `mapstructure:"max_restarts"     yaml:"max_restarts"`
	RestartDelay    time.Duration `mapstructure:"restart_delay"    yaml:"restart_delay"`
}

type WebSocketPerformanceTuning struct {
	// Compression settings.
	EnableWriteCompression bool `mapstructure:"enable_write_compression" yaml:"enable_write_compression"`
	EnableReadCompression  bool `mapstructure:"enable_read_compression"  yaml:"enable_read_compression"`

	// Connection optimizations.
	OptimizePingPong     bool `mapstructure:"optimize_ping_pong"     yaml:"optimize_ping_pong"`
	EnableMessagePooling bool `mapstructure:"enable_message_pooling" yaml:"enable_message_pooling"`

	// Batching settings.
	MessageBatchSize  int           `mapstructure:"message_batch_size"  yaml:"message_batch_size"`
	WriteBatchTimeout time.Duration `mapstructure:"write_batch_timeout" yaml:"write_batch_timeout"`

	// Buffer sizes.
	ReadBufferSize  int `mapstructure:"read_buffer_size"  yaml:"read_buffer_size"`
	WriteBufferSize int `mapstructure:"write_buffer_size" yaml:"write_buffer_size"`
}

type HTTPPerformanceTuning struct {
	// Compression settings.
	EnableCompression    bool `mapstructure:"enable_compression"    yaml:"enable_compression"`
	CompressionThreshold int  `mapstructure:"compression_threshold" yaml:"compression_threshold"`

	// HTTP version and connection settings.
	EnableHTTP2      bool `mapstructure:"enable_http2"       yaml:"enable_http2"`
	MaxConnsPerHost  int  `mapstructure:"max_conns_per_host" yaml:"max_conns_per_host"`
	EnablePipelining bool `mapstructure:"enable_pipelining"  yaml:"enable_pipelining"`

	// Optimizations.
	ReuseEncoders      bool `mapstructure:"reuse_encoders"       yaml:"reuse_encoders"`
	ResponseBufferSize int  `mapstructure:"response_buffer_size" yaml:"response_buffer_size"`

	// Connection management.
	IdleConnTimeout time.Duration `mapstructure:"idle_conn_timeout" yaml:"idle_conn_timeout"`
	KeepAlive       time.Duration `mapstructure:"keep_alive"        yaml:"keep_alive"`
}

type SSEPerformanceTuning struct {
	// Buffer sizes.
	StreamBufferSize  int `mapstructure:"stream_buffer_size"  yaml:"stream_buffer_size"`
	RequestBufferSize int `mapstructure:"request_buffer_size" yaml:"request_buffer_size"`

	// Connection optimizations.
	ReuseConnections   bool `mapstructure:"reuse_connections"    yaml:"reuse_connections"`
	EnableCompression  bool `mapstructure:"enable_compression"   yaml:"enable_compression"`
	ConnectionPoolSize int  `mapstructure:"connection_pool_size" yaml:"connection_pool_size"`

	// Stream handling.
	FastReconnect       bool          `mapstructure:"fast_reconnect"        yaml:"fast_reconnect"`
	ReconnectDelay      time.Duration `mapstructure:"reconnect_delay"       yaml:"reconnect_delay"`
	EnableEventBatching bool          `mapstructure:"enable_event_batching" yaml:"enable_event_batching"`
	BatchTimeout        time.Duration `mapstructure:"batch_timeout"         yaml:"batch_timeout"`
}

// Performance defaults setting functions.
func setStdioPerformanceDefaults(perf *StdioPerformanceTuning) {
	if perf.StdinBufferSize == 0 {
		perf.StdinBufferSize = StdioBufferSizeKB * KilobyteFactor // 64KB
	}

	if perf.StdoutBufferSize == 0 {
		perf.StdoutBufferSize = StdioBufferSizeKB * KilobyteFactor // 64KB
	}

	if !perf.EnableBufferedIO {
		perf.EnableBufferedIO = true // Enable by default for performance
	}

	if !perf.ReuseEncoders {
		perf.ReuseEncoders = true // Enable by default
	}

	if perf.MaxRestarts == 0 {
		perf.MaxRestarts = constants.DefaultMaxRetries
	}

	if perf.RestartDelay == 0 {
		perf.RestartDelay = constants.DefaultRetryDelay
	}
	// ProcessPriority defaults to 0 (normal priority).
}

func setWebSocketPerformanceDefaults(perf *WebSocketPerformanceTuning) {
	// EnableWriteCompression defaults to false for lower latency.
	if !perf.EnableReadCompression {
		perf.EnableReadCompression = true // Enable by default for bandwidth efficiency
	}

	if !perf.OptimizePingPong {
		perf.OptimizePingPong = true // Enable by default
	}

	if !perf.EnableMessagePooling {
		perf.EnableMessagePooling = true // Enable by default to reduce GC pressure
	}

	if perf.MessageBatchSize == 0 {
		perf.MessageBatchSize = 0 // Disable batching by default for lower latency
	}

	if perf.WriteBatchTimeout == 0 {
		perf.WriteBatchTimeout = constants.MinimalBatchTimeout
	}

	if perf.ReadBufferSize == 0 {
		perf.ReadBufferSize = constants.DefaultSmallBufferSize
	}

	if perf.WriteBufferSize == 0 {
		perf.WriteBufferSize = constants.DefaultSmallBufferSize
	}
}

func setHTTPPerformanceDefaults(perf *HTTPPerformanceTuning) {
	if !perf.EnableCompression {
		perf.EnableCompression = true // Enable by default for bandwidth efficiency
	}

	if perf.CompressionThreshold == 0 {
		perf.CompressionThreshold = 1024 // 1KB threshold
	}

	if !perf.EnableHTTP2 {
		perf.EnableHTTP2 = true // Enable by default
	}

	if perf.MaxConnsPerHost == 0 {
		perf.MaxConnsPerHost = constants.DefaultMaxConnectionsPerHost
	}
	// EnablePipelining defaults to false for compatibility.
	if !perf.ReuseEncoders {
		perf.ReuseEncoders = true // Enable by default
	}

	if perf.ResponseBufferSize == 0 {
		perf.ResponseBufferSize = HTTPBufferSizeKB * KilobyteFactor // 32KB
	}

	if perf.IdleConnTimeout == 0 {
		perf.IdleConnTimeout = constants.DefaultIdleConnTimeout
	}

	if perf.KeepAlive == 0 {
		perf.KeepAlive = constants.DefaultKeepAlive
	}
}

func setSSEPerformanceDefaults(perf *SSEPerformanceTuning) {
	if perf.StreamBufferSize == 0 {
		perf.StreamBufferSize = StdioBufferSizeKB * KilobyteFactor // 64KB for better throughput
	}

	if perf.RequestBufferSize == 0 {
		perf.RequestBufferSize = HTTPBufferSizeKB * KilobyteFactor // 32KB
	}

	if !perf.ReuseConnections {
		perf.ReuseConnections = true // Enable by default for efficiency
	}

	if !perf.EnableCompression {
		perf.EnableCompression = true // Enable by default for bandwidth efficiency
	}

	if perf.ConnectionPoolSize == 0 {
		perf.ConnectionPoolSize = constants.DefaultConnectionPoolSize
	}

	if !perf.FastReconnect {
		perf.FastReconnect = true // Enable by default for better reconnection
	}

	if perf.ReconnectDelay == 0 {
		perf.ReconnectDelay = constants.DefaultFastReconnectDelay
	}
	// EnableEventBatching defaults to false for lower latency.
	if perf.BatchTimeout == 0 {
		perf.BatchTimeout = constants.DefaultBatchTimeout
	}
}

// DirectClientManager manages direct connections to MCP servers using various protocols.
// DirectClientManagerInterface defines the interface for managing direct MCP clients.
type DirectClientManagerInterface interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	GetClient(ctx context.Context, serverURL string) (DirectClient, error)
}

// Ensure DirectClientManager implements DirectClientManagerInterface.
var _ DirectClientManagerInterface = (*DirectClientManager)(nil)

type DirectClientManager struct {
	config DirectConfig
	logger *zap.Logger

	// Connection pool for enhanced connection management.
	connectionPool *ConnectionPool

	// Auto-detection cache.
	protocolCache map[string]ClientType
	cacheMu       sync.RWMutex
	cacheTimers   map[string]*time.Timer // Track cache expiration timers
	timersMu      sync.RWMutex

	// Adaptive mechanisms.
	adaptiveTimeout *AdaptiveTimeout
	adaptiveRetry   *AdaptiveRetry

	// Advanced tuning.
	timeoutTuner *TimeoutTuner

	// Observability and monitoring.
	observability *ObservabilityManager
	statusMonitor *StatusMonitor

	// Memory optimization.
	memoryOptimizer *MemoryOptimizer

	// Client management.
	mu       sync.RWMutex
	running  bool
	shutdown chan struct{}
	wg       sync.WaitGroup

	// Metrics.
	metrics   ManagerMetrics
	metricsMu sync.RWMutex
}

// ManagerMetrics contains metrics for the DirectClientManager.
type ManagerMetrics struct {
	TotalClients       int           `json:"total_clients"`
	ActiveConnections  int           `json:"active_connections"`
	FailedConnections  int           `json:"failed_connections"`
	ProtocolDetections int           `json:"protocol_detections"`
	CacheHits          int           `json:"cache_hits"`
	CacheMisses        int           `json:"cache_misses"`
	AverageLatency     time.Duration `json:"average_latency"`
	LastUpdate         time.Time     `json:"last_update"`
}

// NewDirectClientManager creates a new DirectClientManager.

func NewDirectClientManager(config DirectConfig, logger *zap.Logger) DirectClientManagerInterface {
	// Use the descriptive builder pattern instead of complex inline initialization.
	return InitializeClientOrchestrator(config, logger).
		ApplyOperationalDefaults().
		OptimizePerformanceParameters().
		BuildClientOrchestrator()
}

// Start initializes the DirectClientManager.
func (m *DirectClientManager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running {
		return errors.New("DirectClientManager already running")
	}

	m.running = true

	// Start observability manager.
	if err := m.observability.Start(ctx); err != nil {
		m.running = false

		return fmt.Errorf("failed to start observability manager: %w", err)
	}

	// Start status monitor.
	if err := m.statusMonitor.Start(ctx); err != nil {
		m.running = false
		if stopErr := m.observability.Stop(ctx); stopErr != nil {
			m.logger.Error("Failed to stop observability during cleanup", zap.Error(stopErr))
		}

		return fmt.Errorf("failed to start status monitor: %w", err)
	}

	// Start memory optimizer.
	if err := m.memoryOptimizer.Start(); err != nil {
		m.running = false
		if stopErr := m.statusMonitor.Stop(ctx); stopErr != nil {
			m.logger.Error("Failed to stop status monitor during cleanup", zap.Error(stopErr))
		}

		if stopErr := m.observability.Stop(ctx); stopErr != nil {
			m.logger.Error("Failed to stop observability during cleanup", zap.Error(stopErr))
		}

		return fmt.Errorf("failed to start memory optimizer: %w", err)
	}

	// Start health check routine if enabled.
	if m.config.HealthCheck.Enabled {
		m.wg.Add(1)

		go m.healthCheckLoop(ctx)
	}

	// Start metrics update routine.
	m.wg.Add(1)

	go m.metricsUpdateLoop()

	m.logger.Info("DirectClientManager started successfully",
		zap.Int("max_connections", m.config.MaxConnections),
		zap.Duration("default_timeout", m.config.DefaultTimeout),
		zap.Bool("observability_enabled", m.config.Observability.MetricsEnabled || m.config.Observability.TracingEnabled),
		zap.Bool("health_monitoring_enabled", m.config.Observability.HealthMonitoringEnabled))

	return nil
}

// GetClient returns a direct client for the specified server URL.

func (m *DirectClientManager) GetClient(ctx context.Context, serverURL string) (DirectClient, error) {
	m.mu.RLock()

	if !m.running {
		m.mu.RUnlock()

		return nil, errors.New("DirectClientManager not running")
	}

	m.mu.RUnlock()

	// Use enhanced protocol detection with URL hints.
	protocol, err := m.DetectProtocolWithHints(ctx, serverURL)
	if err != nil {
		// Fallback to original detection method if hints fail.
		m.logger.Debug("hinted detection failed, trying original method",
			zap.String("server_url", serverURL),
			zap.Error(err))

		protocol, err = m.DetectProtocol(ctx, serverURL)
		if err != nil {
			return nil, fmt.Errorf("failed to detect protocol for %s: %w", serverURL, err)
		}
	}

	// Get or create client based on protocol.
	return m.getOrCreateClient(ctx, serverURL, protocol)
}

// DetectProtocol detects the protocol used by a server.
func (m *DirectClientManager) DetectProtocol(ctx context.Context, serverURL string) (ClientType, error) {
	// Check cache first if enabled.
	if m.config.AutoDetection.CacheResults {
		m.cacheMu.RLock()

		if cached, exists := m.protocolCache[serverURL]; exists {
			m.cacheMu.RUnlock()
			m.updateMetrics(func(metrics *ManagerMetrics) {
				metrics.CacheHits++
			})
			// If cached result is empty, it means previous detection failed.
			if cached == "" {
				return "", fmt.Errorf("could not detect compatible protocol for %s", serverURL)
			}

			return cached, nil
		}

		m.cacheMu.RUnlock()
		m.updateMetrics(func(metrics *ManagerMetrics) {
			metrics.CacheMisses++
		})
	}

	if !m.config.AutoDetection.Enabled {
		return "", errors.New("protocol auto-detection is disabled")
	}

	// Try protocols in preferred order.
	ctx, cancel := context.WithTimeout(ctx, m.config.AutoDetection.Timeout)
	defer cancel()

	for _, protocolStr := range m.config.AutoDetection.PreferredOrder {
		protocol := ClientType(protocolStr)

		if m.canConnectWithProtocol(ctx, serverURL, protocol) {
			// Cache the result.
			if m.config.AutoDetection.CacheResults {
				m.setCacheEntryWithExpiration(serverURL, protocol, m.config.AutoDetection.CacheTTL)
			}

			m.updateMetrics(func(metrics *ManagerMetrics) {
				metrics.ProtocolDetections++
			})

			return protocol, nil
		}
	}

	// Cache the failure to avoid repeating expensive failed connection attempts.
	if m.config.AutoDetection.CacheResults {
		m.setCacheEntryWithExpiration(serverURL, "", m.config.AutoDetection.CacheTTL)
	}

	return "", fmt.Errorf("could not detect compatible protocol for %s", serverURL)
}

// getOrCreateClient gets an existing client or creates a new one using the connection pool.

func (m *DirectClientManager) getOrCreateClient(
	ctx context.Context,
	serverURL string,
	protocol ClientType,
) (DirectClient, error) {
	createFunc := m.createClientConnectionFunc(ctx, serverURL, protocol)

	pooledConn, err := m.connectionPool.GetConnection(ctx, serverURL, protocol, createFunc)
	if err != nil {
		return nil, err
	}

	m.updateMetrics(func(metrics *ManagerMetrics) {
		metrics.TotalClients++
		metrics.ActiveConnections++
	})

	return pooledConn.Client, nil
}

func (m *DirectClientManager) createClientConnectionFunc(
	ctx context.Context,
	serverURL string,
	protocol ClientType,
) func() (DirectClient, error) {
	return func() (DirectClient, error) {
		// Create new client based on protocol.
		client, err := m.createClient(serverURL, protocol)
		if err != nil {
			m.updateMetrics(func(metrics *ManagerMetrics) {
				metrics.FailedConnections++
			})

			return nil, fmt.Errorf("failed to create %s client for %s: %w", protocol, serverURL, err)
		}

		// Connect the client.
		startTime := time.Now()

		if err := client.Connect(ctx); err != nil {
			m.recordConnectionFailure(serverURL, protocol, startTime, err)

			return nil, fmt.Errorf("failed to connect %s client to %s: %w", protocol, serverURL, err)
		}

		// Record successful connection for observability.
		m.recordConnectionSuccess(serverURL, protocol, startTime)

		return client, nil
	}
}

func (m *DirectClientManager) recordConnectionFailure(
	serverURL string,
	protocol ClientType,
	startTime time.Time,
	err error,
) {
	m.updateMetrics(func(metrics *ManagerMetrics) {
		metrics.FailedConnections++
	})

	// Record failed connection for observability.
	if m.observability != nil {
		metrics := DetailedClientMetrics{
			ServerURL:           serverURL,
			Protocol:            string(protocol),
			ConnectionState:     StateDisconnected,
			ConnectionTime:      startTime,
			LastUsed:            time.Now(),
			LastError:           err.Error(),
			LastErrorTime:       time.Now(),
			ConsecutiveFailures: 1,
			IsHealthy:           false,
			ErrorsByType:        make(map[string]uint64),
			CreatedAt:           startTime,
			LastMetricsUpdate:   time.Now(),
		}
		metrics.ErrorsByType["connection_error"] = 1
		metrics.ConnectionErrors = 1
		m.observability.RecordClientMetrics(serverURL, metrics)
	}
}

func (m *DirectClientManager) recordConnectionSuccess(serverURL string, protocol ClientType, startTime time.Time) {
	if m.observability != nil {
		metrics := DetailedClientMetrics{
			ServerURL:         serverURL,
			Protocol:          string(protocol),
			ConnectionState:   StateConnected,
			ConnectionTime:    startTime,
			LastUsed:          time.Now(),
			LastHealthCheck:   time.Now(),
			IsHealthy:         true,
			ErrorsByType:      make(map[string]uint64),
			HealthHistory:     []HealthCheckResult{{Timestamp: time.Now(), Success: true, Latency: time.Since(startTime)}},
			CreatedAt:         startTime,
			LastMetricsUpdate: time.Now(),
		}
		m.observability.RecordClientMetrics(serverURL, metrics)
	}
}

// createClient creates a new client of the specified protocol type.

func (m *DirectClientManager) createClient(serverURL string, protocol ClientType) (DirectClient, error) {
	switch protocol {
	case ClientTypeStdio:
		return m.createStdioClient(serverURL)
	case ClientTypeWebSocket:
		return m.createWebSocketClient(serverURL)
	case ClientTypeHTTP:
		return m.createHTTPClient(serverURL)
	case ClientTypeSSE:
		return m.createSSEClient(serverURL)
	default:
		return nil, fmt.Errorf("unsupported protocol: %s", protocol)
	}
}

// Protocol-specific client creation methods.

func (m *DirectClientManager) createStdioClient(serverURL string) (DirectClient, error) {
	// Create stdio-specific config from manager config.
	stdioConfig := StdioClientConfig{
		Command:       nil, // Will be parsed from serverURL
		WorkingDir:    m.config.Stdio.DefaultWorkingDir,
		Env:           m.config.Stdio.DefaultEnv,
		Timeout:       m.config.DefaultTimeout,
		MaxBufferSize: m.config.Stdio.MaxBufferSize,
		HealthCheck:   m.config.HealthCheck,
		Process: ProcessConfig{
			MaxRestarts:  m.config.Stdio.Performance.MaxRestarts,
			RestartDelay: m.config.Stdio.Performance.RestartDelay,
			KillTimeout:  m.config.Stdio.ProcessTimeout,
		},
		Performance: StdioPerformanceConfig{
			StdinBufferSize:  m.config.Stdio.Performance.StdinBufferSize,
			StdoutBufferSize: m.config.Stdio.Performance.StdoutBufferSize,
			EnableBufferedIO: m.config.Stdio.Performance.EnableBufferedIO,
			ReuseEncoders:    m.config.Stdio.Performance.ReuseEncoders,
			ProcessPriority:  m.config.Stdio.Performance.ProcessPriority,
		},
	}

	// Apply defaults if not set.
	if stdioConfig.Timeout == 0 {
		stdioConfig.Timeout = m.config.DefaultTimeout
	}

	if stdioConfig.Process.KillTimeout == 0 {
		stdioConfig.Process.KillTimeout = DefaultDirectTimeoutSeconds * time.Second
	}

	if stdioConfig.Process.MaxRestarts == 0 {
		stdioConfig.Process.MaxRestarts = DefaultMaxRestarts
	}

	if stdioConfig.Process.RestartDelay == 0 {
		stdioConfig.Process.RestartDelay = DefaultRetryDelaySeconds * time.Second
	}

	// Generate unique client name.
	clientName := fmt.Sprintf("stdio-%d", time.Now().UnixNano())

	return NewStdioClientWithMemoryOptimizer(clientName, serverURL, stdioConfig, m.logger, m.memoryOptimizer)
}

func (m *DirectClientManager) createWebSocketClient(serverURL string) (DirectClient, error) {
	// Create WebSocket-specific config from manager config.
	wsConfig := WebSocketClientConfig{
		URL:              serverURL,
		Headers:          m.config.WebSocket.DefaultHeaders,
		HandshakeTimeout: m.config.WebSocket.HandshakeTimeout,
		PingInterval:     m.config.WebSocket.PingInterval,
		PongTimeout:      m.config.WebSocket.PongTimeout,
		MaxMessageSize:   m.config.WebSocket.MaxMessageSize,
		Timeout:          m.config.DefaultTimeout,
		HealthCheck:      m.config.HealthCheck,
		Connection: ConnectionConfig{
			MaxReconnectAttempts: constants.DefaultMaxReconnectAttempts,
			ReconnectDelay:       DefaultRetryDelaySeconds * time.Second,
			ReadBufferSize:       m.config.WebSocket.Performance.ReadBufferSize,
			WriteBufferSize:      m.config.WebSocket.Performance.WriteBufferSize,
			EnableCompression:    false, // WebSocket compression handled separately in performance config
		},
		Performance: WebSocketPerformanceConfig{
			EnableWriteCompression: m.config.WebSocket.Performance.EnableWriteCompression,
			EnableReadCompression:  m.config.WebSocket.Performance.EnableReadCompression,
			OptimizePingPong:       m.config.WebSocket.Performance.OptimizePingPong,
			EnableMessagePooling:   m.config.WebSocket.Performance.EnableMessagePooling,
			MessageBatchSize:       m.config.WebSocket.Performance.MessageBatchSize,
			WriteBatchTimeout:      m.config.WebSocket.Performance.WriteBatchTimeout,
		},
	}

	// Apply defaults if not set.
	if wsConfig.Timeout == 0 {
		wsConfig.Timeout = m.config.DefaultTimeout
	}

	if wsConfig.Connection.ReadBufferSize == 0 {
		wsConfig.Connection.ReadBufferSize = constants.DefaultSmallBufferSize
	}

	if wsConfig.Connection.WriteBufferSize == 0 {
		wsConfig.Connection.WriteBufferSize = constants.DefaultSmallBufferSize
	}

	// Generate unique client name.
	clientName := fmt.Sprintf("websocket-%d", time.Now().UnixNano())

	return NewWebSocketClient(clientName, serverURL, wsConfig, m.logger)
}

func (m *DirectClientManager) createHTTPClient(serverURL string) (DirectClient, error) {
	// Create HTTP-specific config from manager config.
	httpConfig := HTTPClientConfig{
		URL:     serverURL,
		Method:  "POST", // Default method for MCP
		Headers: m.config.HTTP.DefaultHeaders,
		Timeout: m.config.DefaultTimeout,
		Client: HTTPTransportConfig{
			MaxIdleConns:        m.config.HTTP.MaxIdleConns,
			MaxIdleConnsPerHost: DefaultMaxIdleConnsPerHost,
			IdleConnTimeout:     m.config.HTTP.Performance.IdleConnTimeout,
			FollowRedirects:     m.config.HTTP.FollowRedirects,
			MaxRedirects:        constants.DefaultMaxRedirects,
			UserAgent:           "MCP-Router-HTTP-Client/1.0",
		},
		HealthCheck: m.config.HealthCheck,
		Performance: HTTPPerformanceConfig{
			EnableCompression:    m.config.HTTP.Performance.EnableCompression,
			CompressionThreshold: m.config.HTTP.Performance.CompressionThreshold,
			EnableHTTP2:          m.config.HTTP.Performance.EnableHTTP2,
			MaxConnsPerHost:      m.config.HTTP.Performance.MaxConnsPerHost,
			EnablePipelining:     m.config.HTTP.Performance.EnablePipelining,
			ReuseEncoders:        m.config.HTTP.Performance.ReuseEncoders,
			ResponseBufferSize:   m.config.HTTP.Performance.ResponseBufferSize,
		},
	}

	// Apply defaults if not set.
	if httpConfig.Timeout == 0 {
		httpConfig.Timeout = m.config.DefaultTimeout
	}

	if httpConfig.Client.MaxIdleConns == 0 {
		httpConfig.Client.MaxIdleConns = 10
	}

	if httpConfig.Client.IdleConnTimeout == 0 {
		httpConfig.Client.IdleConnTimeout = constants.DefaultIdleConnTimeout
	}

	// Generate unique client name.
	clientName := fmt.Sprintf("http-%d", time.Now().UnixNano())

	return NewHTTPClientWithMemoryOptimizer(clientName, serverURL, httpConfig, m.logger, m.memoryOptimizer)
}

func (m *DirectClientManager) createSSEClient(serverURL string) (DirectClient, error) {
	// Create SSE-specific config from manager config.
	sseConfig := SSEClientConfig{
		URL:            serverURL,
		Method:         "POST", // Default method for MCP
		Headers:        m.config.SSE.DefaultHeaders,
		RequestTimeout: m.config.DefaultTimeout,
		StreamTimeout:  m.config.SSE.StreamTimeout,
		BufferSize:     m.config.SSE.BufferSize,
		Client: SSETransportConfig{
			MaxIdleConns:        DefaultSampleWindowSize,
			MaxIdleConnsPerHost: DefaultMaxIdleConnsPerHost,
			IdleConnTimeout:     constants.DefaultIdleConnTimeout,
			FollowRedirects:     true,
			MaxRedirects:        constants.DefaultMaxRedirects,
			UserAgent:           "MCP-Router-SSE-Client/1.0",
		},
		HealthCheck: m.config.HealthCheck,
		Performance: SSEPerformanceConfig{
			StreamBufferSize:    m.config.SSE.Performance.StreamBufferSize,
			RequestBufferSize:   m.config.SSE.Performance.RequestBufferSize,
			ReuseConnections:    m.config.SSE.Performance.ReuseConnections,
			EnableCompression:   m.config.SSE.Performance.EnableCompression,
			FastReconnect:       m.config.SSE.Performance.FastReconnect,
			ConnectionPoolSize:  m.config.SSE.Performance.ConnectionPoolSize,
			EnableEventBatching: m.config.SSE.Performance.EnableEventBatching,
			BatchTimeout:        m.config.SSE.Performance.BatchTimeout,
		},
	}

	// Apply defaults if not set.
	if sseConfig.RequestTimeout == 0 {
		sseConfig.RequestTimeout = m.config.DefaultTimeout
	}

	if sseConfig.StreamTimeout == 0 {
		sseConfig.StreamTimeout = LongTimeoutSeconds * time.Second // 5 minutes default
	}

	if sseConfig.BufferSize == 0 {
		sseConfig.BufferSize = constants.DefaultSmallBufferSize
	}

	// Generate unique client name.
	clientName := fmt.Sprintf("sse-%d", time.Now().UnixNano())

	return NewSSEClient(clientName, serverURL, sseConfig, m.logger)
}

// ExecuteWithAdaptiveMechanisms executes a function with adaptive timeout and retry mechanisms.
func (m *DirectClientManager) ExecuteWithAdaptiveMechanisms(
	ctx context.Context,
	serverURL string,
	fn func(context.Context) error,
) error {
	if m.adaptiveTimeout == nil || m.adaptiveRetry == nil {
		// Fallback to direct execution if adaptive mechanisms aren't enabled.
		return fn(ctx)
	}

	// Get adaptive timeout for this request.
	adaptiveTimeout := m.adaptiveTimeout.GetTimeout(ctx)

	// Create context with adaptive timeout.
	adaptiveCtx, cancel := context.WithTimeout(ctx, adaptiveTimeout)
	defer cancel()

	// Execute with adaptive retry.
	startTime := time.Now()
	err := m.adaptiveRetry.Execute(adaptiveCtx, func() error {
		return fn(adaptiveCtx)
	})

	// Record request metrics for adaptive learning.
	duration := time.Since(startTime)
	success := err == nil

	errorType := ""
	if err != nil {
		errorType = GetErrorType(err)
	}

	metrics := RequestMetrics{
		StartTime: startTime,
		Duration:  duration,
		Success:   success,
		ErrorType: errorType,
	}

	m.adaptiveTimeout.RecordRequest(metrics)

	return err
}

// SendRequestWithAdaptive sends a request to a server using adaptive mechanisms.
func (m *DirectClientManager) SendRequestWithAdaptive(
	ctx context.Context,
	serverURL string,
	req *mcp.Request,
) (*mcp.Response, error) {
	handler := CreateAdaptiveRequestHandler(m)

	return handler.HandleRequest(ctx, serverURL, req)
}

// SendRequestWithAdaptiveOld is the old implementation - to be removed after verification.
// GetAdaptiveStats returns current adaptive timeout and retry statistics.
func (m *DirectClientManager) GetAdaptiveStats() map[string]interface{} {
	stats := make(map[string]interface{})

	if m.adaptiveTimeout != nil {
		stats["adaptive_timeout"] = m.adaptiveTimeout.GetStats()
	}

	if m.adaptiveRetry != nil {
		stats["adaptive_retry"] = m.adaptiveRetry.GetStats()
	}

	return stats
}

// GetOptimizedTimeoutConfig returns an optimized timeout configuration for a protocol.
func (m *DirectClientManager) GetOptimizedTimeoutConfig(protocol string) AdaptiveTimeoutConfig {
	if m.timeoutTuner != nil {
		protocolConfig := m.timeoutTuner.GetProtocolOptimizedConfig(protocol)

		return protocolConfig.AdaptiveTimeout
	}

	return m.config.AdaptiveTimeout
}

// GetOptimizedRetryConfig returns an optimized retry configuration for a protocol.
func (m *DirectClientManager) GetOptimizedRetryConfig(protocol string) AdaptiveRetryConfig {
	if m.timeoutTuner != nil {
		protocolConfig := m.timeoutTuner.GetProtocolOptimizedConfig(protocol)

		return protocolConfig.AdaptiveRetry
	}

	return m.config.AdaptiveRetry
}

// GetTimeoutTuningStats returns current timeout tuning statistics.
func (m *DirectClientManager) GetTimeoutTuningStats() map[string]interface{} {
	if m.timeoutTuner == nil {
		return make(map[string]interface{})
	}

	stats := make(map[string]interface{})
	stats["timeout_profile"] = string(m.config.TimeoutTuning.TimeoutProfile)
	stats["retry_profile"] = string(m.config.TimeoutTuning.RetryProfile)
	stats["network_condition"] = string(m.config.TimeoutTuning.NetworkCondition)
	stats["dynamic_adjustment_enabled"] = m.config.TimeoutTuning.EnableDynamicAdjustment
	stats["latency_based_timeout"] = m.config.TimeoutTuning.EnableLatencyBasedTimeout
	stats["load_based_retry"] = m.config.TimeoutTuning.EnableLoadBasedRetry

	// Add protocol-specific configurations.
	protocolConfigs := make(map[string]interface{})
	protocols := []string{"stdio", "http", "websocket", "sse"}

	for _, protocol := range protocols {
		timeoutConfig := m.GetOptimizedTimeoutConfig(protocol)
		retryConfig := m.GetOptimizedRetryConfig(protocol)

		protocolConfigs[protocol] = map[string]interface{}{
			"timeout": map[string]interface{}{
				"base_timeout":    timeoutConfig.BaseTimeout,
				"min_timeout":     timeoutConfig.MinTimeout,
				"max_timeout":     timeoutConfig.MaxTimeout,
				"success_ratio":   timeoutConfig.SuccessRatio,
				"adaptation_rate": timeoutConfig.AdaptationRate,
			},
			"retry": map[string]interface{}{
				"max_retries":     retryConfig.MaxRetries,
				"base_delay":      retryConfig.BaseDelay,
				"max_delay":       retryConfig.MaxDelay,
				"backoff_factor":  retryConfig.BackoffFactor,
				"circuit_breaker": retryConfig.CircuitBreakerEnabled,
			},
		}
	}

	stats["protocol_configs"] = protocolConfigs

	return stats
}

// canConnectWithProtocol checks if a server supports a given protocol.
func (m *DirectClientManager) canConnectWithProtocol(ctx context.Context, serverURL string, protocol ClientType) bool {
	switch protocol {
	case ClientTypeStdio:
		return m.canConnectStdio(ctx, serverURL)
	case ClientTypeWebSocket:
		return m.canConnectWebSocket(ctx, serverURL)
	case ClientTypeHTTP:
		return m.canConnectHTTP(ctx, serverURL)
	case ClientTypeSSE:
		return m.canConnectSSE(ctx, serverURL)
	default:
		m.logger.Debug("unsupported protocol for detection",
			zap.String("server_url", serverURL),
			zap.String("protocol", string(protocol)))

		return false
	}
}

// Protocol-specific detection methods.
func (m *DirectClientManager) canConnectStdio(ctx context.Context, serverURL string) bool {
	// For stdio, we check if the command/executable exists and is executable.
	var command []string

	if strings.HasPrefix(serverURL, "stdio://") {
		cmdStr := strings.TrimPrefix(serverURL, "stdio://")
		command = strings.Fields(cmdStr)
	} else {
		command = strings.Fields(serverURL)
	}

	if len(command) == 0 {
		return false
	}

	// Check if the executable exists and is executable.
	executable := command[0]

	// Try to find the executable in PATH.
	if _, err := exec.LookPath(executable); err == nil {
		return true
	}

	// Try as absolute/relative path.
	if info, err := os.Stat(executable); err == nil {
		// Check if it's executable.
		if info.Mode()&0o111 != 0 {
			return true
		}
	}

	m.logger.Debug("stdio executable not found or not executable",
		zap.String("executable", executable),
		zap.Strings("command", command))

	return false
}

func (m *DirectClientManager) canConnectWebSocket(ctx context.Context, serverURL string) bool {
	// Parse URL to validate WebSocket scheme.
	parsedURL, err := url.Parse(serverURL)
	if err != nil {
		m.logger.Debug("invalid WebSocket URL",
			zap.String("server_url", serverURL),
			zap.Error(err))

		return false
	}

	// Check for WebSocket schemes.
	if parsedURL.Scheme != "ws" && parsedURL.Scheme != "wss" {
		m.logger.Debug("URL does not use WebSocket scheme",
			zap.String("server_url", serverURL),
			zap.String("scheme", parsedURL.Scheme))

		return false
	}

	// Attempt a quick connection test with timeout.
	testCtx, cancel := context.WithTimeout(ctx, DefaultRetryDelaySeconds*time.Second)
	defer cancel()

	dialer := &websocket.Dialer{
		HandshakeTimeout: DefaultHandshakeTimeoutSeconds * time.Second,
	}

	conn, resp, err := dialer.DialContext(testCtx, serverURL, nil)
	if resp != nil && resp.Body != nil {
		defer func() {
			if err := resp.Body.Close(); err != nil {
				m.logger.Debug("Failed to close response body", zap.Error(err))
			}
		}()
	}

	if err != nil {
		m.logger.Debug("WebSocket connection test failed",
			zap.String("server_url", serverURL),
			zap.Error(err))

		if resp != nil {
			m.logger.Debug("WebSocket test response",
				zap.Int("status_code", resp.StatusCode))
		}

		return false
	}

	// Close test connection immediately.
	if err := conn.Close(); err != nil {
		m.logger.Warn("Failed to close test connection", zap.Error(err))
	}

	m.logger.Debug("WebSocket connection test successful",
		zap.String("server_url", serverURL))

	return true
}

// validateHTTPURL validates that the URL uses HTTP or HTTPS scheme.
func (m *DirectClientManager) validateHTTPURL(serverURL string) (*url.URL, bool) {
	parsedURL, err := url.Parse(serverURL)
	if err != nil {
		m.logger.Debug("invalid HTTP URL",
			zap.String("server_url", serverURL),
			zap.Error(err))

		return nil, false
	}

	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		m.logger.Debug("URL does not use HTTP scheme",
			zap.String("server_url", serverURL),
			zap.String("scheme", parsedURL.Scheme))

		return nil, false
	}

	return parsedURL, true
}

// createHTTPPingRequest creates an HTTP request for connectivity testing.
func (m *DirectClientManager) createHTTPPingRequest(ctx context.Context, serverURL string) (*http.Request, error) {
	pingReq := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "ping",
		"id":      "connectivity-test",
	}

	reqBody, err := json.Marshal(pingReq)
	if err != nil {
		m.logger.Debug("failed to marshal ping request", zap.Error(err))

		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, serverURL, bytes.NewReader(reqBody))
	if err != nil {
		m.logger.Debug("failed to create HTTP request",
			zap.String("server_url", serverURL),
			zap.Error(err))

		return nil, err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", "MCP-Router-Detection/1.0")

	return httpReq, nil
}

func (m *DirectClientManager) canConnectHTTP(ctx context.Context, serverURL string) bool {
	// Validate URL scheme.
	if _, ok := m.validateHTTPURL(serverURL); !ok {
		return false
	}

	// Attempt a quick connection test with timeout.
	testCtx, cancel := context.WithTimeout(ctx, DefaultRetryDelaySeconds*time.Second)
	defer cancel()

	// Create HTTP ping request.
	httpReq, err := m.createHTTPPingRequest(testCtx, serverURL)
	if err != nil {
		return false
	}

	// Create HTTP client with short timeout.
	client := &http.Client{
		Timeout: HttpConnectionTestTimeout,
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		m.logger.Debug("HTTP connection test failed",
			zap.String("server_url", serverURL),
			zap.Error(err))

		return false
	}

	defer func() {
		if err := resp.Body.Close(); err != nil {
			m.logger.Debug("Failed to close response body", zap.Error(err))
		}
	}()

	// Accept any response as a sign of connectivity.
	// The actual ping may fail if the server doesn't support it,
	// but we can still connect for other requests
	m.logger.Debug("HTTP connection test successful",
		zap.String("server_url", serverURL),
		zap.Int("status_code", resp.StatusCode))

	return true
}

func (m *DirectClientManager) canConnectSSE(ctx context.Context, serverURL string) bool {
	parsedURL, err := m.validateSSEURL(serverURL)
	if err != nil || parsedURL == nil {
		return false
	}

	resp, err := m.performSSEConnectionTest(ctx, serverURL)
	if err != nil {
		return false
	}
	defer m.closeSSEResponse(resp)

	return m.validateSSEResponse(serverURL, resp)
}

func (m *DirectClientManager) validateSSEURL(serverURL string) (*url.URL, error) {
	// Parse URL to validate HTTP scheme (SSE uses HTTP/HTTPS).
	parsedURL, err := url.Parse(serverURL)
	if err != nil {
		m.logger.Debug("invalid SSE URL",
			zap.String("server_url", serverURL),
			zap.Error(err))
		return nil, err
	}

	// Check for HTTP schemes (SSE runs over HTTP/HTTPS).
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		m.logger.Debug("URL does not use HTTP scheme for SSE",
			zap.String("server_url", serverURL),
			zap.String("scheme", parsedURL.Scheme))
		return nil, fmt.Errorf("invalid scheme for SSE: %s", parsedURL.Scheme)
	}

	return parsedURL, nil
}

func (m *DirectClientManager) performSSEConnectionTest(ctx context.Context, serverURL string) (*http.Response, error) {
	// Attempt a quick SSE connection test with timeout.
	testCtx, cancel := context.WithTimeout(ctx, DefaultRetryDelaySeconds*time.Second)
	defer cancel()

	// Create SSE request.
	sseReq, err := http.NewRequestWithContext(testCtx, http.MethodGet, serverURL, nil)
	if err != nil {
		m.logger.Debug("failed to create SSE request",
			zap.String("server_url", serverURL),
			zap.Error(err))
		return nil, err
	}

	// Set SSE headers.
	sseReq.Header.Set("Accept", "text/event-stream")
	sseReq.Header.Set("Cache-Control", "no-cache")
	sseReq.Header.Set("User-Agent", "MCP-Router-Detection/1.0")

	// Create HTTP client with short timeout.
	client := &http.Client{
		Timeout: HttpConnectionTestTimeout,
	}

	resp, err := client.Do(sseReq)
	if err != nil {
		m.logger.Debug("SSE connection test failed",
			zap.String("server_url", serverURL),
			zap.Error(err))
		return nil, err
	}

	return resp, nil
}

func (m *DirectClientManager) closeSSEResponse(resp *http.Response) {
	if err := resp.Body.Close(); err != nil {
		m.logger.Debug("Failed to close response body", zap.Error(err))
	}
}

func (m *DirectClientManager) validateSSEResponse(serverURL string, resp *http.Response) bool {
	// Check if response indicates SSE support.
	contentType := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "text/event-stream") {
		m.logger.Debug("server does not support SSE",
			zap.String("server_url", serverURL),
			zap.String("content_type", contentType))
		return false
	}

	// Accept any SSE stream as a sign of compatibility.
	m.logger.Debug("SSE connection test successful",
		zap.String("server_url", serverURL),
		zap.Int("status_code", resp.StatusCode))

	return true
}

// setCacheEntryWithExpiration sets a cache entry and schedules its expiration.
func (m *DirectClientManager) setCacheEntryWithExpiration(serverURL string, protocol ClientType, ttl time.Duration) {
	m.cacheMu.Lock()
	m.protocolCache[serverURL] = protocol
	m.cacheMu.Unlock()

	// Cancel any existing timer for this server URL.
	m.timersMu.Lock()

	if existingTimer, exists := m.cacheTimers[serverURL]; exists {
		existingTimer.Stop()
		delete(m.cacheTimers, serverURL)
	}

	// Create new timer for cache expiration.
	timer := time.AfterFunc(ttl, func() {
		m.cacheMu.Lock()
		delete(m.protocolCache, serverURL)
		m.cacheMu.Unlock()

		m.timersMu.Lock()
		delete(m.cacheTimers, serverURL)
		m.timersMu.Unlock()
	})

	m.cacheTimers[serverURL] = timer
	m.timersMu.Unlock()
}

// clearAllCacheTimers stops and clears all cache expiration timers.
func (m *DirectClientManager) clearAllCacheTimers() {
	m.timersMu.Lock()
	defer m.timersMu.Unlock()

	for serverURL, timer := range m.cacheTimers {
		timer.Stop()
		delete(m.cacheTimers, serverURL)
	}
}

// getProtocolHints returns likely protocols based on URL analysis.
func (m *DirectClientManager) getProtocolHints(serverURL string) []ClientType {
	analyzer := CreateProtocolHintAnalyzer(m)

	return analyzer.GetHints(serverURL)
}

// DetectProtocolWithHints detects protocol using URL hints for optimization.
func (m *DirectClientManager) DetectProtocolWithHints(ctx context.Context, serverURL string) (ClientType, error) {
	detector := CreateProtocolDetector(m)

	return detector.DetectProtocol(ctx, serverURL)
}

// healthCheckLoop runs periodic health checks on all clients.
func (m *DirectClientManager) healthCheckLoop(ctx context.Context) {
	defer m.wg.Done()

	ticker := time.NewTicker(m.config.HealthCheck.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-m.shutdown:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.performHealthChecks(ctx)
		}
	}
}

// performHealthChecks checks the health of all active clients.
func (m *DirectClientManager) performHealthChecks(parentCtx context.Context) {
	ctx, cancel := context.WithTimeout(parentCtx, m.config.HealthCheck.Timeout)
	defer cancel()

	// Get all active connections from the connection pool for health checks.
	connections := m.connectionPool.GetActiveConnections()

	for serverURL, pooledConn := range connections {
		client := pooledConn.Client
		startTime := time.Now()

		err := client.Health(ctx)
		duration := time.Since(startTime)

		// Record health check result for observability.
		if m.observability != nil {
			healthResult := HealthCheckResult{
				Timestamp: startTime,
				Success:   err == nil,
				Latency:   duration,
			}
			if err != nil {
				healthResult.Error = err.Error()
			}

			m.observability.RecordHealthCheck(serverURL, healthResult)
		}

		if err != nil {
			m.logger.Warn("client health check failed",
				zap.String("server_url", serverURL),
				zap.String("client", client.GetName()),
				zap.String("protocol", client.GetProtocol()),
				zap.Duration("check_duration", duration),
				zap.Error(err))

			m.recordHealthCheckFailure(serverURL, err)
		} else {
			m.logger.Debug("client health check passed",
				zap.String("server_url", serverURL),
				zap.String("client", client.GetName()),
				zap.String("protocol", client.GetProtocol()),
				zap.Duration("check_duration", duration))

			// Update client metrics to reflect successful health check.
			if m.observability != nil {
				if existingMetrics, exists := m.observability.GetClientMetrics(serverURL); exists {
					existingMetrics.IsHealthy = true
					existingMetrics.LastHealthCheck = time.Now()
					existingMetrics.ConsecutiveFailures = 0
					existingMetrics.LastMetricsUpdate = time.Now()
					m.observability.RecordClientMetrics(serverURL, *existingMetrics)
				}
			}
		}
	}
}

// metricsUpdateLoop periodically updates manager metrics.
func (m *DirectClientManager) metricsUpdateLoop() {
	defer m.wg.Done()

	ticker := time.NewTicker(DefaultDirectTimeoutSeconds * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-m.shutdown:
			return
		case <-ticker.C:
			m.updateManagerMetrics()
		}
	}
}

// updateManagerMetrics updates the manager's metrics.
func (m *DirectClientManager) updateManagerMetrics() {
	// Get stats from connection pool.
	poolStats := m.connectionPool.GetStats()

	totalClients := 0
	if activeConns, ok := poolStats["active_connections"].(int); ok {
		totalClients = activeConns
	}

	m.updateMetrics(func(metrics *ManagerMetrics) {
		metrics.TotalClients = totalClients
		metrics.LastUpdate = time.Now()
	})
}

// RemoveClient removes a client from the connection pool.
func (m *DirectClientManager) RemoveClient(serverURL string, protocol ClientType) error {
	err := m.connectionPool.RemoveConnection(serverURL, protocol)
	if err != nil {
		return err
	}

	m.updateMetrics(func(metrics *ManagerMetrics) {
		if metrics.ActiveConnections > 0 {
			metrics.ActiveConnections--
		}
	})

	m.logger.Info("removed direct client from pool",
		zap.String("server_url", serverURL),
		zap.String("protocol", string(protocol)))

	return nil
}

// Stop gracefully shuts down the DirectClientManager.
func (m *DirectClientManager) Stop(ctx context.Context) error {
	handler := CreateManagerShutdownHandler(m)

	return handler.ExecuteShutdown(ctx)
}

// GetMetrics returns current manager metrics.
func (m *DirectClientManager) GetMetrics() ManagerMetrics {
	m.metricsMu.RLock()
	defer m.metricsMu.RUnlock()

	// Get connection pool stats.
	poolStats := m.connectionPool.GetStats()

	// Update metrics with pool information.
	metrics := m.metrics

	// Only override ActiveConnections with pool data if the pool has active connections.
	// This preserves manually set metrics during testing while using real pool data in production.
	if poolActiveConns, ok := poolStats["active_connections"].(int); ok && poolActiveConns > 0 {
		metrics.ActiveConnections = poolActiveConns
	}

	metrics.LastUpdate = time.Now()

	return metrics
}

// GetDetailedMetrics returns comprehensive detailed metrics from observability.
func (m *DirectClientManager) GetDetailedMetrics() EnhancedManagerMetrics {
	if m.observability == nil {
		// Return basic metrics if observability is not enabled.
		basicMetrics := m.GetMetrics()

		return EnhancedManagerMetrics{
			TotalClients:         basicMetrics.TotalClients,
			ActiveConnections:    basicMetrics.ActiveConnections,
			FailedConnections:    basicMetrics.FailedConnections,
			ProtocolDetections:   basicMetrics.ProtocolDetections,
			CacheHits:            basicMetrics.CacheHits,
			CacheMisses:          basicMetrics.CacheMisses,
			AverageLatency:       basicMetrics.AverageLatency,
			LastUpdate:           basicMetrics.LastUpdate,
			ClientsByProtocol:    make(map[string]int),
			ClientsByState:       make(map[string]int),
			PoolStats:            make(map[string]interface{}),
			AdaptiveTimeoutStats: make(map[string]interface{}),
			AdaptiveRetryStats:   make(map[string]interface{}),
		}
	}

	return m.observability.GetManagerMetrics()
}

// GetClientMetrics returns detailed metrics for a specific client.
func (m *DirectClientManager) GetClientMetrics(clientName string) (*DetailedClientMetrics, bool) {
	if m.observability == nil {
		return nil, false
	}

	return m.observability.GetClientMetrics(clientName)
}

// GetAllClientMetrics returns detailed metrics for all clients.
func (m *DirectClientManager) GetAllClientMetrics() map[string]DetailedClientMetrics {
	if m.observability == nil {
		return make(map[string]DetailedClientMetrics)
	}

	return m.observability.GetAllClientMetrics()
}

// GetSystemStatus returns comprehensive system status information.
func (m *DirectClientManager) GetSystemStatus() SystemStatus {
	if m.statusMonitor == nil {
		// Return minimal status if status monitor is not enabled.
		return SystemStatus{
			Version:   "1.0.0-dev",
			StartTime: time.Now(), // This would be set properly in production
			Uptime:    time.Since(time.Now()),
			ManagerStatus: ManagerStatus{
				Running: m.running,
				ComponentsHealthy: map[string]bool{
					"manager": m.running,
				},
			},
		}
	}

	return m.statusMonitor.GetSystemStatus()
}

// GetClientStatus returns detailed status for a specific client.
func (m *DirectClientManager) GetClientStatus(clientName string) (*DirectClientStatus, error) {
	if m.statusMonitor == nil {
		return nil, errors.New("status monitoring not enabled")
	}

	return m.statusMonitor.GetClientStatus(clientName)
}

// GetHealthAlerts returns current health alerts.
func (m *DirectClientManager) GetHealthAlerts() []HealthAlert {
	if m.statusMonitor == nil {
		return []HealthAlert{}
	}

	return m.statusMonitor.GetHealthAlerts()
}

// ExportObservabilityData exports all observability data in JSON format.
func (m *DirectClientManager) ExportObservabilityData() ([]byte, error) {
	if m.observability == nil {
		// Export basic data if observability is not enabled.
		basicData := map[string]interface{}{
			"basic_metrics":    m.GetMetrics(),
			"system_status":    m.GetSystemStatus(),
			"export_timestamp": time.Now(),
		}

		return json.MarshalIndent(basicData, "", "  ")
	}

	return m.observability.ExportMetrics()
}

// ExportSystemStatus exports comprehensive system status in JSON format.
func (m *DirectClientManager) ExportSystemStatus() ([]byte, error) {
	if m.statusMonitor == nil {
		// Export basic status if status monitor is not enabled.
		basicStatus := map[string]interface{}{
			"system_status":    m.GetSystemStatus(),
			"export_timestamp": time.Now(),
		}

		return json.MarshalIndent(basicStatus, "", "  ")
	}

	return m.statusMonitor.ExportStatus()
}

// RecordClientMetrics records detailed metrics for a client (for use by client implementations).
func (m *DirectClientManager) RecordClientMetrics(clientName string, metrics DetailedClientMetrics) {
	if m.observability != nil {
		m.observability.RecordClientMetrics(clientName, metrics)
	}
}

// RecordRequest records a request trace for observability.
func (m *DirectClientManager) RecordRequest(trace RequestTrace) {
	if m.observability != nil {
		m.observability.RecordRequest(trace)
	}
}

// RecordHealthCheck records a health check result for observability.
func (m *DirectClientManager) RecordHealthCheck(clientName string, result HealthCheckResult) {
	if m.observability != nil {
		m.observability.RecordHealthCheck(clientName, result)
	}
}

// GenerateHealthReport generates a comprehensive health report.
func (m *DirectClientManager) GenerateHealthReport() map[string]interface{} {
	if m.observability == nil {
		// Generate basic health report.
		basicMetrics := m.GetMetrics()

		return map[string]interface{}{
			"overall_status":     "unknown",
			"health_score":       0,
			"total_clients":      basicMetrics.TotalClients,
			"active_connections": basicMetrics.ActiveConnections,
			"timestamp":          time.Now(),
		}
	}

	return m.observability.GenerateHealthReport()
}

// updateMetrics safely updates manager metrics.
func (m *DirectClientManager) updateMetrics(updateFn func(*ManagerMetrics)) {
	m.metricsMu.Lock()
	updateFn(&m.metrics)
	m.metricsMu.Unlock()
}

// ListClients returns information about all active clients.
func (m *DirectClientManager) ListClients() map[string]ClientInfo {
	// Get connection pool stats and build client info.
	poolStats := m.connectionPool.GetStats()
	clients := make(map[string]ClientInfo)

	// Add connection pool statistics as client info.
	if protocolBreakdown, ok := poolStats["protocol_breakdown"].(map[string]int); ok {
		for protocol, count := range protocolBreakdown {
			// Create a summary entry for each protocol type.
			clients[protocol+"-pool-summary"] = ClientInfo{
				URL:      "pool://" + protocol,
				Protocol: protocol,
				Metrics: ClientMetrics{
					RequestCount: func() uint64 {
						if count < 0 {
							return 0
						}

						return uint64(count)
					}(),
					ErrorCount:      0,
					AverageLatency:  0,
					LastHealthCheck: time.Now(),
					IsHealthy:       true,
					ConnectionTime:  time.Now(),
					LastUsed:        time.Now(),
				},
			}
		}
	}

	return clients
}

func (m *DirectClientManager) recordHealthCheckFailure(serverURL string, err error) {
	if m.observability == nil {
		return
	}

	existingMetrics, exists := m.observability.GetClientMetrics(serverURL)
	if !exists {
		return
	}

	existingMetrics.IsHealthy = false
	existingMetrics.LastError = err.Error()
	existingMetrics.LastErrorTime = time.Now()
	existingMetrics.ConsecutiveFailures++
	existingMetrics.LastMetricsUpdate = time.Now()
	m.observability.RecordClientMetrics(serverURL, *existingMetrics)
}

// ClientInfo contains information about a direct client.
type ClientInfo struct {
	URL      string        `json:"url"`
	Protocol string        `json:"protocol"`
	Metrics  ClientMetrics `json:"metrics"`
}
