package frontends

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	"github.com/actual-software/mcp-bridge/services/router/pkg/mcp"
)

const (
	testIterations = 100
	testTimeout    = 50
)

// Mock implementations for testing

type mockRequestRouter struct{}

func (m *mockRequestRouter) RouteRequest(
	ctx context.Context,
	req *mcp.Request,
	targetNamespace string,
) (*mcp.Response, error) {
	return &mcp.Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  map[string]interface{}{"message": "mock response"},
	}, nil
}

type mockAuthProvider struct {
	shouldAuthenticate bool
}

func (m *mockAuthProvider) Authenticate(
	ctx context.Context,
	credentials map[string]string,
) (bool, error) {
	return m.shouldAuthenticate, nil
}

func (m *mockAuthProvider) GetUserInfo(
	ctx context.Context,
	credentials map[string]string,
) (map[string]interface{}, error) {
	return map[string]interface{}{
		"user_id": "test-user",
		"scopes":  []string{"read", "write"},
	}, nil
}

type mockSessionManager struct{}

func (m *mockSessionManager) CreateSession(ctx context.Context, clientID string) (string, error) {
	return "mock-session-id", nil
}

func (m *mockSessionManager) ValidateSession(ctx context.Context, sessionID string) (bool, error) {
	return sessionID == "mock-session-id", nil
}

func (m *mockSessionManager) DestroySession(ctx context.Context, sessionID string) error {
	return nil
}

func TestCreateFrontendFactory(t *testing.T) {
	t.Parallel()

	logger := zaptest.NewLogger(t)
	factory := CreateFrontendFactory(logger)

	require.NotNil(t, factory)
	assert.NotNil(t, factory.logger)
}

func TestDefaultFactory_SupportedProtocols(t *testing.T) {
	t.Parallel()

	logger := zaptest.NewLogger(t)
	factory := CreateFrontendFactory(logger)

	protocols := factory.SupportedProtocols()
	expected := []string{"stdio"}

	assert.Equal(t, expected, protocols)
}

func TestDefaultFactory_CreateFrontend_UnsupportedProtocol(t *testing.T) {
	t.Parallel()

	logger := zaptest.NewLogger(t)
	factory := CreateFrontendFactory(logger)
	router := &mockRequestRouter{}
	auth := &mockAuthProvider{shouldAuthenticate: true}
	sessions := &mockSessionManager{}

	config := FrontendConfig{
		Name:     "test",
		Protocol: "unsupported",
		Config:   map[string]interface{}{},
	}

	frontend, err := factory.CreateFrontend(config, router, auth, sessions)

	require.Error(t, err)
	assert.Nil(t, frontend)
	assert.Contains(t, err.Error(), "unsupported frontend protocol: unsupported")
}

func TestDefaultFactory_CreateStdioFrontend(t *testing.T) {
	t.Parallel()

	tests := createStdioFrontendTests()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			logger := zaptest.NewLogger(t)
			factory := CreateFrontendFactory(logger)
			router := &mockRequestRouter{}
			auth := &mockAuthProvider{shouldAuthenticate: true}
			sessions := &mockSessionManager{}

			frontendConfig := FrontendConfig{
				Name:     "test-stdio",
				Protocol: "stdio",
				Config:   tt.config,
			}

			frontend, err := factory.CreateFrontend(frontendConfig, router, auth, sessions)

			if tt.expectSuccess {
				require.NoError(t, err)
				assert.NotNil(t, frontend)
				assert.Equal(t, "test-stdio", frontend.GetName())
				assert.Equal(t, "stdio", frontend.GetProtocol())
			} else {
				require.Error(t, err)
				assert.Nil(t, frontend)

				if tt.expectError != "" {
					assert.Contains(t, err.Error(), tt.expectError)
				}
			}
		})
	}
}

func createStdioFrontendTests() []struct {
	name          string
	config        map[string]interface{}
	expectSuccess bool
	expectError   string
} {
	var tests []struct {
		name          string
		config        map[string]interface{}
		expectSuccess bool
		expectError   string
	}

	tests = append(tests, createValidStdioTests()...)
	tests = append(tests, createStdioEdgeCaseTests()...)

	return tests
}

func createValidStdioTests() []struct {
	name          string
	config        map[string]interface{}
	expectSuccess bool
	expectError   string
} {
	var tests []struct {
		name          string
		config        map[string]interface{}
		expectSuccess bool
		expectError   string
	}

	tests = append(tests, createBasicStdioTests()...)
	tests = append(tests, createComplexStdioTests()...)

	return tests
}

func createBasicStdioTests() []struct {
	name          string
	config        map[string]interface{}
	expectSuccess bool
	expectError   string
} {
	return []struct {
		name          string
		config        map[string]interface{}
		expectSuccess bool
		expectError   string
	}{
		{
			name: "Valid minimal config",
			config: map[string]interface{}{
				"enabled": true,
			},
			expectSuccess: true,
		},
		{
			name: "Valid full config",
			config: map[string]interface{}{
				"enabled": true,
				"modes": []interface{}{
					map[string]interface{}{
						"type":        "unix_socket",
						"path":        "/tmp/mcp.sock",
						"permissions": "600",
						"enabled":     true,
					},
					map[string]interface{}{
						"type":    "stdin_stdout",
						"enabled": true,
					},
				},
				"process_management": map[string]interface{}{
					"max_concurrent_clients": 10,
					"client_timeout":         "30s",
					"auth_required":          true,
				},
			},
			expectSuccess: true,
		},
	}
}

func createComplexStdioTests() []struct {
	name          string
	config        map[string]interface{}
	expectSuccess bool
	expectError   string
} {
	return []struct {
		name          string
		config        map[string]interface{}
		expectSuccess bool
		expectError   string
	}{
		{
			name: "Complex modes configuration",
			config: map[string]interface{}{
				"enabled": true,
				"modes": []interface{}{
					map[string]interface{}{
						"type":        "unix_socket",
						"path":        "/var/run/mcp.sock",
						"permissions": "644",
						"enabled":     true,
					},
					map[string]interface{}{
						"type":    "stdin_stdout",
						"enabled": false,
					},
				},
				"process_management": map[string]interface{}{
					"max_concurrent_clients": testTimeout,
					"client_timeout":         "60s",
					"auth_required":          false,
				},
			},
			expectSuccess: true,
		},
	}
}

func createStdioEdgeCaseTests() []struct {
	name          string
	config        map[string]interface{}
	expectSuccess bool
	expectError   string
} {
	return []struct {
		name          string
		config        map[string]interface{}
		expectSuccess bool
		expectError   string
	}{
		{
			name: "Disabled frontend",
			config: map[string]interface{}{
				"enabled": false,
			},
			expectSuccess: true,
		},
		{
			name: "Invalid timeout format",
			config: map[string]interface{}{
				"enabled": true,
				"process_management": map[string]interface{}{
					"client_timeout": "invalid-duration",
				},
			},
			expectSuccess: true, // Invalid duration is ignored
		},
		{
			name:          "Empty config",
			config:        map[string]interface{}{},
			expectSuccess: true,
		},
		{
			name:          "Nil config",
			config:        nil,
			expectSuccess: true,
		},
	}
}

func TestFrontendWrapper_GetMetrics(t *testing.T) {
	t.Parallel()

	logger := zaptest.NewLogger(t)
	factory := CreateFrontendFactory(logger)
	router := &mockRequestRouter{}
	auth := &mockAuthProvider{shouldAuthenticate: true}
	sessions := &mockSessionManager{}

	config := FrontendConfig{
		Name:     "test-stdio",
		Protocol: "stdio",
		Config: map[string]interface{}{
			"enabled": true,
		},
	}

	frontend, err := factory.CreateFrontend(config, router, auth, sessions)

	require.NoError(t, err)
	require.NotNil(t, frontend)

	// Test that GetMetrics returns a properly structured FrontendMetrics
	metrics := frontend.GetMetrics()

	// Verify the metrics structure is correct
	assert.IsType(t, FrontendMetrics{}, metrics)
	// All frontends should start with zero metrics
	assert.Equal(t, uint64(0), metrics.ActiveConnections)
	assert.Equal(t, uint64(0), metrics.TotalConnections)
	assert.Equal(t, uint64(0), metrics.RequestCount)
	assert.Equal(t, uint64(0), metrics.ErrorCount)
	// IsRunning should be false for newly created frontend
	assert.False(t, metrics.IsRunning)
}

func TestDefaultFactory_ComplexConfigurationParsing(t *testing.T) {
	t.Parallel()

	logger := zaptest.NewLogger(t)
	factory := CreateFrontendFactory(logger)
	router := &mockRequestRouter{}
	auth := &mockAuthProvider{shouldAuthenticate: true}
	sessions := &mockSessionManager{}

	// Test complex stdio configuration with all possible fields
	stdioConfig := FrontendConfig{
		Name:     "complex-stdio",
		Protocol: "stdio",
		Config: map[string]interface{}{
			"enabled": true,
			"modes": []interface{}{
				map[string]interface{}{
					"type":        "unix_socket",
					"path":        "/app/sockets/mcp.sock",
					"permissions": "660",
					"enabled":     true,
				},
				map[string]interface{}{
					"type":    "stdin_stdout",
					"enabled": true,
				},
			},
			"process_management": map[string]interface{}{
				"max_concurrent_clients": testIterations,
				"client_timeout":         "120s",
				"auth_required":          true,
			},
		},
	}

	frontend, err := factory.CreateFrontend(stdioConfig, router, auth, sessions)

	require.NoError(t, err)
	assert.NotNil(t, frontend)
	assert.Equal(t, "complex-stdio", frontend.GetName())
	assert.Equal(t, "stdio", frontend.GetProtocol())
}

func TestDefaultFactory_EdgeCases(t *testing.T) {
	t.Parallel()

	logger := zaptest.NewLogger(t)
	factory := CreateFrontendFactory(logger)
	router := &mockRequestRouter{}
	auth := &mockAuthProvider{shouldAuthenticate: true}
	sessions := &mockSessionManager{}

	tests := createFactoryEdgeCaseTests()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			frontend, err := factory.CreateFrontend(tt.config, router, auth, sessions)
			if tt.expectError {
				require.Error(t, err)
				assert.Nil(t, frontend)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, frontend)
			}
		})
	}
}

func createFactoryEdgeCaseTests() []struct {
	name        string
	config      FrontendConfig
	expectError bool
} {
	var tests []struct {
		name        string
		config      FrontendConfig
		expectError bool
	}

	tests = append(tests, createNilConfigTests()...)
	tests = append(tests, createInvalidTypeTests()...)

	return tests
}

func createNilConfigTests() []struct {
	name        string
	config      FrontendConfig
	expectError bool
} {
	return []struct {
		name        string
		config      FrontendConfig
		expectError bool
	}{
		{
			name: "empty name",
			config: FrontendConfig{
				Name:     "",
				Protocol: "stdio",
				Config: map[string]interface{}{
					"enabled": true,
				},
			},
			expectError: false,
		},
		{
			name: "nil config map",
			config: FrontendConfig{
				Name:     "test-nil",
				Protocol: "stdio",
				Config:   nil,
			},
			expectError: false,
		},
		{
			name: "empty config map",
			config: FrontendConfig{
				Name:     "test-empty",
				Protocol: "stdio",
				Config:   map[string]interface{}{},
			},
			expectError: false,
		},
	}
}

func createInvalidTypeTests() []struct {
	name        string
	config      FrontendConfig
	expectError bool
} {
	return []struct {
		name        string
		config      FrontendConfig
		expectError bool
	}{
		{
			name: "invalid protocol",
			config: FrontendConfig{
				Name:     "test-invalid",
				Protocol: "invalid-protocol",
				Config: map[string]interface{}{
					"enabled": true,
				},
			},
			expectError: true,
		},
		{
			name: "numeric values in config",
			config: FrontendConfig{
				Name:     "test-numeric",
				Protocol: "stdio",
				Config: map[string]interface{}{
					"enabled":             123,
					"invalid_numeric_key": 456.789,
				},
			},
			expectError: false,
		},
		{
			name: "complex invalid structure",
			config: FrontendConfig{
				Name:     "test-complex",
				Protocol: "stdio",
				Config: map[string]interface{}{
					"enabled": true,
					"modes": map[string]interface{}{
						"invalid": "structure",
					},
				},
			},
			expectError: false,
		},
	}
}

func TestDefaultFactory_DependencyInjection(t *testing.T) {
	t.Parallel()

	logger := zaptest.NewLogger(t)
	factory := CreateFrontendFactory(logger)

	// Test different combinations of dependencies
	tests := []struct {
		name     string
		router   RequestRouter
		auth     AuthProvider
		sessions SessionManager
	}{
		{
			name:     "all valid dependencies",
			router:   &mockRequestRouter{},
			auth:     &mockAuthProvider{shouldAuthenticate: true},
			sessions: &mockSessionManager{},
		},
		{
			name:     "auth that rejects",
			router:   &mockRequestRouter{},
			auth:     &mockAuthProvider{shouldAuthenticate: false},
			sessions: &mockSessionManager{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			config := FrontendConfig{
				Name:     "dependency-test",
				Protocol: "stdio",
				Config: map[string]interface{}{
					"enabled": true,
				},
			}

			frontend, err := factory.CreateFrontend(config, tt.router, tt.auth, tt.sessions)
			require.NoError(t, err)
			assert.NotNil(t, frontend)

			// Verify the frontend was created with the correct dependencies
			assert.Equal(t, "dependency-test", frontend.GetName())
			assert.Equal(t, "stdio", frontend.GetProtocol())
		})
	}
}

// Benchmark tests to ensure factory performance.
func BenchmarkDefaultFactory_CreateStdioFrontend(b *testing.B) {
	logger := zap.NewNop()
	factory := CreateFrontendFactory(logger)
	router := &mockRequestRouter{}
	auth := &mockAuthProvider{shouldAuthenticate: true}
	sessions := &mockSessionManager{}

	config := FrontendConfig{
		Name:     "bench-stdio",
		Protocol: "stdio",
		Config: map[string]interface{}{
			"enabled": true,
		},
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		frontend, err := factory.CreateFrontend(config, router, auth, sessions)
		if err != nil {
			b.Fatal(err)
		}

		_ = frontend
	}
}

func TestDefaultFactory_ConfigurationDefaults(t *testing.T) {
	t.Parallel()

	logger := zaptest.NewLogger(t)
	factory := CreateFrontendFactory(logger)
	router := &mockRequestRouter{}
	auth := &mockAuthProvider{shouldAuthenticate: true}
	sessions := &mockSessionManager{}

	// Test that missing configuration fields use proper defaults
	config := FrontendConfig{
		Name:     "defaults-test",
		Protocol: "stdio",
		Config: map[string]interface{}{
			// Only provide minimal config to test defaults
			"enabled": true,
		},
	}

	frontend, err := factory.CreateFrontend(config, router, auth, sessions)

	require.NoError(t, err)
	assert.NotNil(t, frontend)

	// Verify that the frontend was created successfully with defaults
	metrics := frontend.GetMetrics()

	assert.Equal(t, uint64(0), metrics.ActiveConnections)
	assert.Equal(t, uint64(0), metrics.TotalConnections)
	assert.False(t, metrics.IsRunning)
}

func TestDefaultFactory_ModesConfigurationParsing(t *testing.T) {
	t.Parallel()

	logger := zaptest.NewLogger(t)
	factory := CreateFrontendFactory(logger)
	router := &mockRequestRouter{}
	auth := &mockAuthProvider{shouldAuthenticate: true}
	sessions := &mockSessionManager{}

	tests := createModesConfigurationTests()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			config := FrontendConfig{
				Name:     "modes-test",
				Protocol: "stdio",
				Config: map[string]interface{}{
					"enabled": true,
					"modes":   tt.modes,
				},
			}

			frontend, err := factory.CreateFrontend(config, router, auth, sessions)
			if tt.expect {
				require.NoError(t, err)
				assert.NotNil(t, frontend)
			} else {
				require.Error(t, err)
				assert.Nil(t, frontend)
			}
		})
	}
}

func createModesConfigurationTests() []struct {
	name   string
	modes  interface{}
	expect bool
} {
	return []struct {
		name   string
		modes  interface{}
		expect bool
	}{
		{
			name: "valid modes array",
			modes: []interface{}{
				map[string]interface{}{
					"type":        "unix_socket",
					"path":        "/tmp/test.sock",
					"permissions": "600",
					"enabled":     true,
				},
			},
			expect: true,
		},
		{
			name:   "empty modes array",
			modes:  []interface{}{},
			expect: true,
		},
		{
			name:   "nil modes",
			modes:  nil,
			expect: true,
		},
		{
			name:   "invalid modes type",
			modes:  "invalid",
			expect: true, // Should be ignored gracefully
		},
		{
			name: "modes with invalid map",
			modes: []interface{}{
				"invalid-mode-item",
			},
			expect: true, // Should be ignored gracefully
		},
	}
}
