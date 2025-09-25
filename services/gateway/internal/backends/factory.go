package backends

import (
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/poiley/mcp-bridge/services/gateway/internal/backends/sse"
	"github.com/poiley/mcp-bridge/services/gateway/internal/backends/stdio"
	"github.com/poiley/mcp-bridge/services/gateway/internal/backends/websocket"
	customerrors "github.com/poiley/mcp-bridge/services/gateway/internal/errors"
	"github.com/poiley/mcp-bridge/services/gateway/internal/metrics"
)

// Wrapper types to implement the Backend interface

type stdioBackendWrapper struct {
	*stdio.Backend
}

func (w *stdioBackendWrapper) GetMetrics() BackendMetrics {
	m := w.Backend.GetMetrics()

	return BackendMetrics{
		RequestCount:    m.RequestCount,
		ErrorCount:      m.ErrorCount,
		AverageLatency:  m.AverageLatency,
		LastHealthCheck: m.LastHealthCheck,
		IsHealthy:       m.IsHealthy,
		ConnectionTime:  m.ConnectionTime,
	}
}

type websocketBackendWrapper struct {
	*websocket.Backend
}

func (w *websocketBackendWrapper) GetMetrics() BackendMetrics {
	m := w.Backend.GetMetrics()

	return BackendMetrics{
		RequestCount:    m.RequestCount,
		ErrorCount:      m.ErrorCount,
		AverageLatency:  m.AverageLatency,
		LastHealthCheck: m.LastHealthCheck,
		IsHealthy:       m.IsHealthy,
		ConnectionTime:  m.ConnectionTime,
	}
}

type sseBackendWrapper struct {
	*sse.Backend
}

func (w *sseBackendWrapper) GetMetrics() BackendMetrics {
	m := w.Backend.GetMetrics()

	return BackendMetrics{
		RequestCount:    m.RequestCount,
		ErrorCount:      m.ErrorCount,
		AverageLatency:  m.AverageLatency,
		LastHealthCheck: m.LastHealthCheck,
		IsHealthy:       m.IsHealthy,
		ConnectionTime:  m.ConnectionTime,
	}
}

// DefaultFactory is the default backend factory implementation.
type DefaultFactory struct {
	logger  *zap.Logger
	metrics *metrics.Registry
}

// CreateBackendFactory creates a backend factory for instantiating backend implementations.
func CreateBackendFactory(logger *zap.Logger, metrics *metrics.Registry) *DefaultFactory {
	return &DefaultFactory{
		logger:  logger.With(zap.String("component", "backend_factory")),
		metrics: metrics,
	}
}

// CreateBackend creates a backend instance based on configuration.
//

func (f *DefaultFactory) CreateBackend(config BackendConfig) (Backend, error) {
	switch config.Protocol {
	case "stdio":
		return f.createStdioBackend(config)
	case "websocket", "ws", "wss":
		return f.createWebSocketBackend(config)
	case "sse", "server-sent-events":
		return f.createSSEBackend(config)
	default:
		return nil, customerrors.NewValidationError("unsupported backend protocol: " + config.Protocol ).
			WithComponent("backend_factory")
	}
}

// SupportedProtocols returns the list of supported backend protocols.
func (f *DefaultFactory) SupportedProtocols() []string {
	return []string{"stdio", "websocket", "ws", "wss", "sse", "server-sent-events"}
}

// createStdioBackend creates a stdio backend instance.
//

func (f *DefaultFactory) createStdioBackend(config BackendConfig) (Backend, error) {
	stdioConfig := stdio.Config{}

	// Parse basic configuration
	if err := f.parseStdioBasicConfig(&stdioConfig, config.Config); err != nil {
		return nil, err
	}

	// Parse health check configuration
	f.parseStdioHealthCheck(&stdioConfig, config.Config)

	// Parse process configuration
	f.parseStdioProcess(&stdioConfig, config.Config)

	return &stdioBackendWrapper{stdio.CreateStdioBackend(config.Name, stdioConfig, f.logger, f.metrics)}, nil
}

// parseStdioBasicConfig parses basic stdio backend configuration.
func (f *DefaultFactory) parseStdioBasicConfig(stdioConfig *stdio.Config, config map[string]interface{}) error {
	// Parse command
	if err := f.parseStdioCommand(stdioConfig, config); err != nil {
		return err
	}

	// Parse working directory
	if workingDir, ok := config["working_dir"].(string); ok {
		stdioConfig.WorkingDir = workingDir
	}

	// Parse environment variables
	f.parseStdioEnv(stdioConfig, config)

	// Parse timeout
	if timeout, ok := config["timeout"].(string); ok {
		if d, err := time.ParseDuration(timeout); err == nil {
			stdioConfig.Timeout = d
		}
	}

	return nil
}

// parseStdioCommand parses the command configuration.
func (f *DefaultFactory) parseStdioCommand(stdioConfig *stdio.Config, config map[string]interface{}) error {
	command, ok := config["command"].([]interface{})
	if !ok {
		return nil
	}

	stdioConfig.Command = make([]string, len(command))

	for i, cmd := range command {
		cmdStr, ok := cmd.(string)
		if !ok {
			return customerrors.NewValidationError(fmt.Sprintf("invalid command element at index %d", i)).
				WithComponent("backend_factory")
		}

		stdioConfig.Command[i] = cmdStr
	}

	return nil
}

// parseStdioEnv parses environment variables configuration.
func (f *DefaultFactory) parseStdioEnv(stdioConfig *stdio.Config, config map[string]interface{}) {
	env, ok := config["env"].(map[string]interface{})
	if !ok {
		return
	}

	stdioConfig.Env = make(map[string]string)

	for k, v := range env {
		if vStr, ok := v.(string); ok {
			stdioConfig.Env[k] = vStr
		}
	}
}

// parseStdioHealthCheck parses health check configuration.
func (f *DefaultFactory) parseStdioHealthCheck(stdioConfig *stdio.Config, config map[string]interface{}) {
	healthCheck, ok := config["health_check"].(map[string]interface{})
	if !ok {
		return
	}

	if enabled, ok := healthCheck["enabled"].(bool); ok {
		stdioConfig.HealthCheck.Enabled = enabled
	}

	if interval, ok := healthCheck["interval"].(string); ok {
		if d, err := time.ParseDuration(interval); err == nil {
			stdioConfig.HealthCheck.Interval = d
		}
	}

	if timeout, ok := healthCheck["timeout"].(string); ok {
		if d, err := time.ParseDuration(timeout); err == nil {
			stdioConfig.HealthCheck.Timeout = d
		}
	}
}

// parseStdioProcess parses process configuration.
func (f *DefaultFactory) parseStdioProcess(stdioConfig *stdio.Config, config map[string]interface{}) {
	process, ok := config["process"].(map[string]interface{})
	if !ok {
		return
	}

	if maxRestarts, ok := process["max_restarts"].(int); ok {
		stdioConfig.Process.MaxRestarts = maxRestarts
	}

	if restartDelay, ok := process["restart_delay"].(string); ok {
		if d, err := time.ParseDuration(restartDelay); err == nil {
			stdioConfig.Process.RestartDelay = d
		}
	}
}

// createWebSocketBackend creates a WebSocket backend instance.
//

func (f *DefaultFactory) createWebSocketBackend(config BackendConfig) (Backend, error) {
	wsConfig := websocket.Config{}

	// Parse basic configuration
	if err := f.parseWebSocketBasicConfig(&wsConfig, config.Config); err != nil {
		return nil, err
	}

	// Parse connection pool configuration
	f.parseWebSocketConnectionPool(&wsConfig, config.Config)

	// Parse health check configuration
	f.parseWebSocketHealthCheck(&wsConfig, config.Config)

	return &websocketBackendWrapper{websocket.CreateWebSocketBackend(config.Name, wsConfig, f.logger, f.metrics)}, nil
}

// parseWebSocketBasicConfig parses basic WebSocket configuration.
func (f *DefaultFactory) parseWebSocketBasicConfig(wsConfig *websocket.Config, config map[string]interface{}) error {
	// Parse endpoints
	if err := f.parseWebSocketEndpoints(wsConfig, config); err != nil {
		return err
	}

	// Parse headers
	f.parseWebSocketHeaders(wsConfig, config)

	// Parse origin
	if origin, ok := config["origin"].(string); ok {
		wsConfig.Origin = origin
	}

	// Parse timeouts
	f.parseWebSocketTimeouts(wsConfig, config)

	return nil
}

// parseWebSocketEndpoints parses WebSocket endpoints.
func (f *DefaultFactory) parseWebSocketEndpoints(wsConfig *websocket.Config, config map[string]interface{}) error {
	endpoints, ok := config["endpoints"].([]interface{})
	if !ok {
		return nil
	}

	wsConfig.Endpoints = make([]string, len(endpoints))

	for i, endpoint := range endpoints {
		endpointStr, ok := endpoint.(string)
		if !ok {
			return customerrors.NewValidationError(fmt.Sprintf("invalid endpoint element at index %d", i)).
				WithComponent("backend_factory")
		}

		wsConfig.Endpoints[i] = endpointStr
	}

	return nil
}

// parseWebSocketHeaders parses WebSocket headers.
func (f *DefaultFactory) parseWebSocketHeaders(wsConfig *websocket.Config, config map[string]interface{}) {
	headers, ok := config["headers"].(map[string]interface{})
	if !ok {
		return
	}

	wsConfig.Headers = make(map[string]string)

	for k, v := range headers {
		if vStr, ok := v.(string); ok {
			wsConfig.Headers[k] = vStr
		}
	}
}

// parseWebSocketTimeouts parses WebSocket timeout configuration.
func (f *DefaultFactory) parseWebSocketTimeouts(wsConfig *websocket.Config, config map[string]interface{}) {
	if timeout, ok := config["timeout"].(string); ok {
		if d, err := time.ParseDuration(timeout); err == nil {
			wsConfig.Timeout = d
		}
	}

	if handshakeTimeout, ok := config["handshake_timeout"].(string); ok {
		if d, err := time.ParseDuration(handshakeTimeout); err == nil {
			wsConfig.HandshakeTimeout = d
		}
	}
}

// parseWebSocketConnectionPool parses connection pool configuration.
func (f *DefaultFactory) parseWebSocketConnectionPool(wsConfig *websocket.Config, config map[string]interface{}) {
	connectionPool, ok := config["connection_pool"].(map[string]interface{})
	if !ok {
		return
	}

	if minSize, ok := connectionPool["min_size"].(int); ok {
		wsConfig.ConnectionPool.MinSize = minSize
	}

	if maxSize, ok := connectionPool["max_size"].(int); ok {
		wsConfig.ConnectionPool.MaxSize = maxSize
	}

	if maxIdle, ok := connectionPool["max_idle"].(string); ok {
		if d, err := time.ParseDuration(maxIdle); err == nil {
			wsConfig.ConnectionPool.MaxIdle = d
		}
	}

	if idleTimeout, ok := connectionPool["idle_timeout"].(string); ok {
		if d, err := time.ParseDuration(idleTimeout); err == nil {
			wsConfig.ConnectionPool.IdleTimeout = d
		}
	}
}

// parseWebSocketHealthCheck parses health check configuration.
func (f *DefaultFactory) parseWebSocketHealthCheck(wsConfig *websocket.Config, config map[string]interface{}) {
	healthCheck, ok := config["health_check"].(map[string]interface{})
	if !ok {
		return
	}

	if enabled, ok := healthCheck["enabled"].(bool); ok {
		wsConfig.HealthCheck.Enabled = enabled
	}

	if interval, ok := healthCheck["interval"].(string); ok {
		if d, err := time.ParseDuration(interval); err == nil {
			wsConfig.HealthCheck.Interval = d
		}
	}

	if timeout, ok := healthCheck["timeout"].(string); ok {
		if d, err := time.ParseDuration(timeout); err == nil {
			wsConfig.HealthCheck.Timeout = d
		}
	}
}

// createSSEBackend creates an SSE backend instance.
//

func (f *DefaultFactory) createSSEBackend(config BackendConfig) (Backend, error) {
	sseConfig := sse.Config{}

	f.parseSSEBasicConfig(&sseConfig, config.Config)
	f.parseSSEConnectionConfig(&sseConfig, config.Config)
	f.parseSSEHealthCheckConfig(&sseConfig, config.Config)

	return &sseBackendWrapper{sse.CreateSSEBackend(config.Name, sseConfig, f.logger, f.metrics)}, nil
}

// parseSSEBasicConfig parses basic SSE configuration options.
func (f *DefaultFactory) parseSSEBasicConfig(sseConfig *sse.Config, configMap map[string]interface{}) {
	if baseURL, ok := configMap["base_url"].(string); ok {
		sseConfig.BaseURL = baseURL
	}

	if streamEndpoint, ok := configMap["stream_endpoint"].(string); ok {
		sseConfig.StreamEndpoint = streamEndpoint
	}

	if requestEndpoint, ok := configMap["request_endpoint"].(string); ok {
		sseConfig.RequestEndpoint = requestEndpoint
	}

	if headers, ok := configMap["headers"].(map[string]interface{}); ok {
		sseConfig.Headers = make(map[string]string)

		for k, v := range headers {
			if vStr, ok := v.(string); ok {
				sseConfig.Headers[k] = vStr
			}
		}
	}
}

// parseSSEConnectionConfig parses SSE connection-related configuration options.
func (f *DefaultFactory) parseSSEConnectionConfig(sseConfig *sse.Config, configMap map[string]interface{}) {
	if timeout, ok := configMap["timeout"].(string); ok {
		if d, err := time.ParseDuration(timeout); err == nil {
			sseConfig.Timeout = d
		}
	}

	if reconnectDelay, ok := configMap["reconnect_delay"].(string); ok {
		if d, err := time.ParseDuration(reconnectDelay); err == nil {
			sseConfig.ReconnectDelay = d
		}
	}

	if maxReconnects, ok := configMap["max_reconnects"].(int); ok {
		sseConfig.MaxReconnects = maxReconnects
	}
}

// parseSSEHealthCheckConfig parses SSE health check configuration options.
func (f *DefaultFactory) parseSSEHealthCheckConfig(sseConfig *sse.Config, configMap map[string]interface{}) {
	healthCheck, ok := configMap["health_check"].(map[string]interface{})
	if !ok {
		return
	}

	if enabled, ok := healthCheck["enabled"].(bool); ok {
		sseConfig.HealthCheck.Enabled = enabled
	}

	if interval, ok := healthCheck["interval"].(string); ok {
		if d, err := time.ParseDuration(interval); err == nil {
			sseConfig.HealthCheck.Interval = d
		}
	}

	if timeout, ok := healthCheck["timeout"].(string); ok {
		if d, err := time.ParseDuration(timeout); err == nil {
			sseConfig.HealthCheck.Timeout = d
		}
	}
}
