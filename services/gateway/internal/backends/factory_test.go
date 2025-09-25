
package backends

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

func TestCreateBackendFactory(t *testing.T) {
	t.Parallel()

	logger := zaptest.NewLogger(t)
	factory := CreateBackendFactory(logger, nil)

	require.NotNil(t, factory)
	assert.NotNil(t, factory.logger)
}

func TestDefaultFactory_SupportedProtocols(t *testing.T) {
	t.Parallel()

	logger := zaptest.NewLogger(t)
	factory := CreateBackendFactory(logger, nil)

	protocols := factory.SupportedProtocols()
	expected := []string{"stdio", "websocket", "ws", "wss", "sse", "server-sent-events"}

	assert.Equal(t, expected, protocols)
}

func TestDefaultFactory_CreateBackend_UnsupportedProtocol(t *testing.T) {
	t.Parallel()

	logger := zaptest.NewLogger(t)
	factory := CreateBackendFactory(logger, nil)

	config := BackendConfig{
		Name:     "test",
		Protocol: "unsupported",
		Config:   map[string]interface{}{},
	}

	backend, err := factory.CreateBackend(config)
	require.Error(t, err)
	assert.Nil(t, backend)
	assert.Contains(t, err.Error(), "unsupported backend protocol: unsupported")
}

func TestDefaultFactory_CreateStdioBackend(t *testing.T) {
	t.Parallel()

	tests := createStdioBackendTestCases()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			runStdioBackendTest(t, tt)
		})
	}
}

type stdioBackendTestCase struct {
	name          string
	config        map[string]interface{}
	expectSuccess bool
	expectError   string
}

func createStdioBackendTestCases() []stdioBackendTestCase {
	return []stdioBackendTestCase{
		{
			name: "Valid minimal config",
			config: map[string]interface{}{
				"command": []interface{}{"echo", "hello"},
			},
			expectSuccess: true,
		},
		{
			name: "Valid full config",
			config: map[string]interface{}{
				"command":     []interface{}{"node", "server.js"},
				"working_dir": "/app",
				"env": map[string]interface{}{
					"NODE_ENV": "production",
					"PORT":     "3000",
				},
				"timeout": "30s",
				"health_check": map[string]interface{}{
					"enabled":  true,
					"interval": "10s",
					"timeout":  "5s",
				},
				"process": map[string]interface{}{
					"max_restarts":  3,
					"restart_delay": "5s",
				},
			},
			expectSuccess: true,
		},
		{
			name: "Invalid command type",
			config: map[string]interface{}{
				"command": "invalid-string-command",
			},
			expectSuccess: true, // Will be ignored, defaults to empty slice
		},
		{
			name: "Invalid command element",
			config: map[string]interface{}{
				"command": []interface{}{"valid", 123, "invalid"},
			},
			expectSuccess: false,
			expectError:   "invalid command element at index 1",
		},
		{
			name: "Invalid timeout format",
			config: map[string]interface{}{
				"command": []interface{}{"echo", "test"},
				"timeout": "invalid-duration",
			},
			expectSuccess: true, // Invalid duration is ignored
		},
		{
			name:          "Empty config",
			config:        map[string]interface{}{},
			expectSuccess: true,
		},
	}
}

func runStdioBackendTest(t *testing.T, tt stdioBackendTestCase) {
	t.Helper()

	logger := zaptest.NewLogger(t)
	factory := CreateBackendFactory(logger, nil)

	backendConfig := BackendConfig{
		Name:     "test-stdio",
		Protocol: "stdio",
		Config:   tt.config,
	}

	backend, err := factory.CreateBackend(backendConfig)

	if tt.expectSuccess {
		require.NoError(t, err)
		assert.NotNil(t, backend)
		assert.Equal(t, "test-stdio", backend.GetName())
		assert.Equal(t, "stdio", backend.GetProtocol())
	} else {
		require.Error(t, err)
		assert.Nil(t, backend)

		if tt.expectError != "" {
			assert.Contains(t, err.Error(), tt.expectError)
		}
	}
}

func TestDefaultFactory_CreateWebSocketBackend(t *testing.T) {
	t.Parallel()

	tests := createWebSocketBackendTestCases()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			runWebSocketBackendTest(t, tt)
		})
	}
}

type webSocketBackendTestCase struct {
	name          string
	protocol      string
	config        map[string]interface{}
	expectSuccess bool
	expectError   string
}

func createWebSocketBackendTestCases() []webSocketBackendTestCase {
	return []webSocketBackendTestCase{
		createValidWebSocketConfigTestCase(),
		createValidWSConfigTestCase(),
		createValidWSSConfigTestCase(),
		createValidFullWebSocketConfigTestCase(),
		createInvalidEndpointTypeTestCase(),
		createInvalidHeadersTypeTestCase(),
		createEmptyConfigTestCase(),
	}
}

func createValidWebSocketConfigTestCase() webSocketBackendTestCase {
	return webSocketBackendTestCase{
		name:     "Valid websocket config",
		protocol: "websocket",
		config: map[string]interface{}{
			"endpoints": []interface{}{"ws://localhost:8080/mcp"},
		},
		expectSuccess: true,
	}
}

func createValidWSConfigTestCase() webSocketBackendTestCase {
	return webSocketBackendTestCase{
		name:     "Valid ws config",
		protocol: "ws",
		config: map[string]interface{}{
			"endpoints": []interface{}{"ws://localhost:8080/mcp"},
		},
		expectSuccess: true,
	}
}

func createValidWSSConfigTestCase() webSocketBackendTestCase {
	return webSocketBackendTestCase{
		name:     "Valid wss config",
		protocol: "wss",
		config: map[string]interface{}{
			"endpoints": []interface{}{"wss://localhost:8443/mcp"},
		},
		expectSuccess: true,
	}
}

func createValidFullWebSocketConfigTestCase() webSocketBackendTestCase {
	return webSocketBackendTestCase{
		name:     "Valid full websocket config",
		protocol: "websocket",
		config: map[string]interface{}{
			"endpoints": []interface{}{
				"ws://localhost:8080/mcp",
				"ws://localhost:8081/mcp",
			},
			"headers": map[string]interface{}{
				"Authorization": "Bearer token123",
				"User-Agent":    "MCP-Gateway/1.0",
			},
			"origin":            "https://example.com",
			"timeout":           "30s",
			"handshake_timeout": "10s",
			"connection_pool": map[string]interface{}{
				"min_size":     2,
				"max_size":     10,
				"max_idle":     "5m",
				"idle_timeout": "1m",
			},
			"health_check": map[string]interface{}{
				"enabled":  true,
				"interval": "15s",
				"timeout":  "5s",
			},
		},
		expectSuccess: true,
	}
}

func createInvalidEndpointTypeTestCase() webSocketBackendTestCase {
	return webSocketBackendTestCase{
		name:     "Invalid endpoint type",
		protocol: "websocket",
		config: map[string]interface{}{
			"endpoints": []interface{}{"valid", 123, "invalid"},
		},
		expectSuccess: false,
		expectError:   "invalid endpoint element at index 1",
	}
}

func createInvalidHeadersTypeTestCase() webSocketBackendTestCase {
	return webSocketBackendTestCase{
		name:     "Invalid headers type",
		protocol: "websocket",
		config: map[string]interface{}{
			"endpoints": []interface{}{"ws://localhost:8080/mcp"},
			"headers": map[string]interface{}{
				"valid":   "value",
				"invalid": 123,
			},
		},
		expectSuccess: true, // Invalid headers are ignored
	}
}

func createEmptyConfigTestCase() webSocketBackendTestCase {
	return webSocketBackendTestCase{
		name:          "Empty config",
		protocol:      "websocket",
		config:        map[string]interface{}{},
		expectSuccess: true,
	}
}

func runWebSocketBackendTest(t *testing.T, tt webSocketBackendTestCase) {
	t.Helper()

	logger := zaptest.NewLogger(t)
	factory := CreateBackendFactory(logger, nil)

	backendConfig := BackendConfig{
		Name:     "test-websocket",
		Protocol: tt.protocol,
		Config:   tt.config,
	}

	backend, err := factory.CreateBackend(backendConfig)

	verifyWebSocketBackendResult(t, tt, backend, err)
}

func verifyWebSocketBackendResult(t *testing.T, tt webSocketBackendTestCase, backend Backend, err error) {
	t.Helper()

	if tt.expectSuccess {
		verifySuccessfulWebSocketBackendCreation(t, backend, err)
	} else {
		verifyFailedWebSocketBackendCreation(t, tt, backend, err)
	}
}

func verifySuccessfulWebSocketBackendCreation(t *testing.T, backend Backend, err error) {
	t.Helper()

	require.NoError(t, err)
	assert.NotNil(t, backend)
	assert.Equal(t, "test-websocket", backend.GetName())
	assert.Equal(t, "websocket", backend.GetProtocol())
}

func verifyFailedWebSocketBackendCreation(t *testing.T, tt webSocketBackendTestCase, backend Backend, err error) {
	t.Helper()

	require.Error(t, err)
	assert.Nil(t, backend)

	if tt.expectError != "" {
		assert.Contains(t, err.Error(), tt.expectError)
	}
}

func TestDefaultFactory_CreateSSEBackend(t *testing.T) {
	t.Parallel()

	tests := createSSEBackendTestCases()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			runSSEBackendTest(t, tt)
		})
	}
}

type sseBackendTestCase struct {
	name          string
	protocol      string
	config        map[string]interface{}
	expectSuccess bool
	expectError   string
}

func createSSEBackendTestCases() []sseBackendTestCase {
	return []sseBackendTestCase{
		createValidSSEConfigTestCase(),
		createValidServerSentEventsConfigTestCase(),
		createValidFullSSEConfigTestCase(),
		createInvalidSSEHeadersTypeTestCase(),
		createInvalidSSETimeoutFormatTestCase(),
		createEmptySSEConfigTestCase(),
	}
}

func createValidSSEConfigTestCase() sseBackendTestCase {
	return sseBackendTestCase{
		name:     "Valid sse config",
		protocol: "sse",
		config: map[string]interface{}{
			"base_url":         "http://localhost:8080",
			"stream_endpoint":  "/events",
			"request_endpoint": "/mcp",
		},
		expectSuccess: true,
	}
}

func createValidServerSentEventsConfigTestCase() sseBackendTestCase {
	return sseBackendTestCase{
		name:     "Valid server-sent-events config",
		protocol: "server-sent-events",
		config: map[string]interface{}{
			"base_url":         "https://api.example.com",
			"stream_endpoint":  "/stream",
			"request_endpoint": "/request",
		},
		expectSuccess: true,
	}
}

func createValidFullSSEConfigTestCase() sseBackendTestCase {
	return sseBackendTestCase{
		name:     "Valid full SSE config",
		protocol: "sse",
		config: map[string]interface{}{
			"base_url":         "https://api.example.com",
			"stream_endpoint":  "/events",
			"request_endpoint": "/mcp",
			"headers": map[string]interface{}{
				"Authorization": "Bearer token123",
				"Accept":        "text/event-stream",
			},
			"timeout":         "60s",
			"reconnect_delay": "5s",
			"max_reconnects":  10,
			"health_check": map[string]interface{}{
				"enabled":  true,
				"interval": "30s",
				"timeout":  "10s",
			},
		},
		expectSuccess: true,
	}
}

func createInvalidSSEHeadersTypeTestCase() sseBackendTestCase {
	return sseBackendTestCase{
		name:     "Invalid headers type",
		protocol: "sse",
		config: map[string]interface{}{
			"base_url": "http://localhost:8080",
			"headers": map[string]interface{}{
				"valid":   "value",
				"invalid": 123,
			},
		},
		expectSuccess: true, // Invalid headers are ignored
	}
}

func createInvalidSSETimeoutFormatTestCase() sseBackendTestCase {
	return sseBackendTestCase{
		name:     "Invalid timeout format",
		protocol: "sse",
		config: map[string]interface{}{
			"base_url": "http://localhost:8080",
			"timeout":  "invalid-duration",
		},
		expectSuccess: true, // Invalid duration is ignored
	}
}

func createEmptySSEConfigTestCase() sseBackendTestCase {
	return sseBackendTestCase{
		name:          "Empty config",
		protocol:      "sse",
		config:        map[string]interface{}{},
		expectSuccess: true,
	}
}

func runSSEBackendTest(t *testing.T, tt sseBackendTestCase) {
	t.Helper()

	logger := zaptest.NewLogger(t)
	factory := CreateBackendFactory(logger, nil)

	backendConfig := BackendConfig{
		Name:     "test-sse",
		Protocol: tt.protocol,
		Config:   tt.config,
	}

	backend, err := factory.CreateBackend(backendConfig)

	verifySSEBackendResult(t, tt, backend, err)
}

func verifySSEBackendResult(t *testing.T, tt sseBackendTestCase, backend Backend, err error) {
	t.Helper()

	if tt.expectSuccess {
		verifySuccessfulSSEBackendCreation(t, backend, err)
	} else {
		verifyFailedSSEBackendCreation(t, tt, backend, err)
	}
}

func verifySuccessfulSSEBackendCreation(t *testing.T, backend Backend, err error) {
	t.Helper()

	require.NoError(t, err)
	assert.NotNil(t, backend)
	assert.Equal(t, "test-sse", backend.GetName())
	assert.Equal(t, "sse", backend.GetProtocol())
}

func verifyFailedSSEBackendCreation(t *testing.T, tt sseBackendTestCase, backend Backend, err error) {
	t.Helper()

	require.Error(t, err)
	assert.Nil(t, backend)

	if tt.expectError != "" {
		assert.Contains(t, err.Error(), tt.expectError)
	}
}

func TestBackendWrappers_GetMetrics(t *testing.T) {
	t.Parallel()

	logger := zaptest.NewLogger(t)
	factory := CreateBackendFactory(logger, nil)

	tests := createMetricsTestCases()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			runMetricsTest(t, factory, tt)
		})
	}
}

type metricsTestCase struct {
	name     string
	protocol string
	config   BackendConfig
}

func createMetricsTestCases() []metricsTestCase {
	return []metricsTestCase{
		{
			name:     "stdio wrapper metrics",
			protocol: "stdio",
			config: BackendConfig{
				Name:     "test-stdio",
				Protocol: "stdio",
				Config: map[string]interface{}{
					"command": []interface{}{"echo", "test"},
				},
			},
		},
		{
			name:     "websocket wrapper metrics",
			protocol: "websocket",
			config: BackendConfig{
				Name:     "test-ws",
				Protocol: "websocket",
				Config: map[string]interface{}{
					"endpoints": []interface{}{"ws://localhost:8080/mcp"},
				},
			},
		},
		{
			name:     "sse wrapper metrics",
			protocol: "sse",
			config: BackendConfig{
				Name:     "test-sse",
				Protocol: "sse",
				Config: map[string]interface{}{
					"base_url": "http://localhost:8080",
				},
			},
		},
	}
}

func runMetricsTest(t *testing.T, factory *DefaultFactory, tt metricsTestCase) {
	t.Helper()

	backend, err := factory.CreateBackend(tt.config)
	require.NoError(t, err)
	require.NotNil(t, backend)

	// Test that GetMetrics returns a properly structured BackendMetrics
	metrics := backend.GetMetrics()

	// Verify the metrics structure is correct
	assert.IsType(t, BackendMetrics{}, metrics)
	// All backends should start with zero metrics
	assert.Equal(t, uint64(0), metrics.RequestCount)
	assert.Equal(t, uint64(0), metrics.ErrorCount)
	assert.Equal(t, time.Duration(0), metrics.AverageLatency)
}

func TestDefaultFactory_ComplexConfigurationParsing(t *testing.T) {
	t.Parallel()

	logger := zaptest.NewLogger(t)
	factory := CreateBackendFactory(logger, nil)

	// Test complex stdio configuration with all possible fields
	stdioConfig := BackendConfig{
		Name:     "complex-stdio",
		Protocol: "stdio",
		Config: map[string]interface{}{
			"command":     []interface{}{"python", "-m", "mcp_server"},
			"working_dir": "/app/mcp",
			"env": map[string]interface{}{
				"PYTHONPATH": "/app/lib",
				"DEBUG":      "true",
				"PORT":       "8080",
			},
			"timeout": "45s",
			"health_check": map[string]interface{}{
				"enabled":  true,
				"interval": "20s",
				"timeout":  "8s",
			},
			"process": map[string]interface{}{
				"max_restarts":  5,
				"restart_delay": "10s",
			},
		},
	}

	backend, err := factory.CreateBackend(stdioConfig)
	require.NoError(t, err)
	assert.NotNil(t, backend)
	assert.Equal(t, "complex-stdio", backend.GetName())
	assert.Equal(t, "stdio", backend.GetProtocol())
}

func TestDefaultFactory_EdgeCases(t *testing.T) {
	t.Parallel()

	logger := zaptest.NewLogger(t)
	factory := CreateBackendFactory(logger, nil)

	tests := []struct {
		name        string
		config      BackendConfig
		expectError bool
	}{
		{
			name: "nil config map",
			config: BackendConfig{
				Name:     "nil-config",
				Protocol: "stdio",
				Config:   nil,
			},
			expectError: false, // Should handle nil gracefully
		},
		{
			name: "empty name",
			config: BackendConfig{
				Name:     "",
				Protocol: "stdio",
				Config:   map[string]interface{}{},
			},
			expectError: false, // Empty name is allowed
		},
		{
			name: "numeric values in string fields",
			config: BackendConfig{
				Name:     "numeric-test",
				Protocol: "websocket",
				Config: map[string]interface{}{
					"endpoints": []interface{}{"ws://localhost:8080/mcp"},
					"origin":    123, // Should be ignored
				},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			backend, err := factory.CreateBackend(tt.config)

			if tt.expectError {
				require.Error(t, err)
				assert.Nil(t, backend)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, backend)
			}
		})
	}
}

// Benchmark tests to ensure factory performance.
func BenchmarkDefaultFactory_CreateStdioBackend(b *testing.B) {
	logger := zap.NewNop()
	factory := CreateBackendFactory(logger, nil)

	config := BackendConfig{
		Name:     "bench-stdio",
		Protocol: "stdio",
		Config: map[string]interface{}{
			"command": []interface{}{"echo", "test"},
		},
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		backend, err := factory.CreateBackend(config)
		if err != nil {
			b.Fatal(err)
		}

		_ = backend
	}
}

func BenchmarkDefaultFactory_CreateWebSocketBackend(b *testing.B) {
	logger := zap.NewNop()
	factory := CreateBackendFactory(logger, nil)

	config := BackendConfig{
		Name:     "bench-ws",
		Protocol: "websocket",
		Config: map[string]interface{}{
			"endpoints": []interface{}{"ws://localhost:8080/mcp"},
		},
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		backend, err := factory.CreateBackend(config)
		if err != nil {
			b.Fatal(err)
		}

		_ = backend
	}
}

func BenchmarkDefaultFactory_CreateSSEBackend(b *testing.B) {
	logger := zap.NewNop()
	factory := CreateBackendFactory(logger, nil)

	config := BackendConfig{
		Name:     "bench-sse",
		Protocol: "sse",
		Config: map[string]interface{}{
			"base_url": "http://localhost:8080",
		},
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		backend, err := factory.CreateBackend(config)
		if err != nil {
			b.Fatal(err)
		}

		_ = backend
	}
}
