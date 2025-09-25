
package discovery

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap/zaptest"

	"github.com/poiley/mcp-bridge/services/gateway/internal/config"
)

func TestCreateServiceDiscoveryProvider_Static(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)

	cfg := config.ServiceDiscoveryConfig{
		Provider: "static",
		Static: config.StaticDiscoveryConfig{
			Endpoints: map[string][]config.EndpointConfig{
				"default": {
					{
						URL:    "http://localhost:8080",
						Labels: map[string]interface{}{"protocol": "http"},
					},
				},
			},
		},
	}

	sd, err := CreateServiceDiscoveryProvider(cfg, logger)
	assert.NoError(t, err)
	assert.NotNil(t, sd)

	// Verify it's a static discovery
	staticSD, ok := sd.(*StaticDiscovery)
	assert.True(t, ok)
	assert.NotNil(t, staticSD)
}

func TestCreateServiceDiscoveryProvider_LegacyMode(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)

	// Test using legacy Mode field instead of Provider
	cfg := config.ServiceDiscoveryConfig{
		Mode: "static", // Using legacy field
		Static: config.StaticDiscoveryConfig{
			Endpoints: map[string][]config.EndpointConfig{
				"default": {
					{
						URL:    "http://localhost:8080",
						Labels: map[string]interface{}{"protocol": "http"},
					},
				},
			},
		},
	}

	sd, err := CreateServiceDiscoveryProvider(cfg, logger)
	assert.NoError(t, err)
	assert.NotNil(t, sd)

	// Verify it's a static discovery
	staticSD, ok := sd.(*StaticDiscovery)
	assert.True(t, ok)
	assert.NotNil(t, staticSD)
}

func TestCreateServiceDiscoveryProvider_ProviderOverridesMode(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)

	// Test that Provider takes precedence over Mode
	cfg := config.ServiceDiscoveryConfig{
		Provider: "static",
		Mode:     "kubernetes", // Should be ignored
		Static: config.StaticDiscoveryConfig{
			Endpoints: map[string][]config.EndpointConfig{
				"default": {
					{
						URL:    "http://localhost:8080",
						Labels: map[string]interface{}{"protocol": "http"},
					},
				},
			},
		},
	}

	sd, err := CreateServiceDiscoveryProvider(cfg, logger)
	assert.NoError(t, err)
	assert.NotNil(t, sd)

	// Should create static discovery, not kubernetes
	staticSD, ok := sd.(*StaticDiscovery)
	assert.True(t, ok)
	assert.NotNil(t, staticSD)
}

func TestCreateServiceDiscoveryProvider_DefaultKubernetes(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)

	// Test that it defaults to kubernetes when no provider specified
	cfg := config.ServiceDiscoveryConfig{
		// No Provider or Mode specified
	}

	sd, err := CreateServiceDiscoveryProvider(cfg, logger)

	// This will fail because we don't have k8s config, but we can check the error
	assert.Error(t, err)
	assert.Nil(t, sd)
}

func TestCreateServiceDiscoveryProvider_UnsupportedProvider(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)

	cfg := config.ServiceDiscoveryConfig{
		Provider: "unsupported-provider",
	}

	sd, err := CreateServiceDiscoveryProvider(cfg, logger)
	assert.Error(t, err)
	assert.Nil(t, sd)
	assert.Contains(t, err.Error(), "invalid service configuration")
}

func TestCreateServiceDiscoveryProvider_WebSocketAliases(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)

	testCases := []string{"websocket", "ws"}

	for _, provider := range testCases {
		t.Run(provider, func(t *testing.T) {
			t.Parallel()
			cfg := config.ServiceDiscoveryConfig{
				Provider: provider,
				WebSocket: config.WebSocketDiscoveryConfig{
					Services: []config.WebSocketServiceConfig{
						{
							Name:      "test-service",
							Namespace: "default",
							Endpoints: []string{"ws://localhost:8080/ws"},
						},
					},
				},
			}

			sd, err := CreateServiceDiscoveryProvider(cfg, logger)

			// WebSocket discovery should be created successfully with valid config
			assert.NoError(t, err)
			assert.NotNil(t, sd)

			// Verify it's a WebSocket discovery
			wsSD, ok := sd.(*WebSocketDiscovery)
			assert.True(t, ok)
			assert.NotNil(t, wsSD)
		})
	}
}

func TestCreateServiceDiscoveryProvider_SSEAliases(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)

	testCases := []string{"sse", "server-sent-events"}

	for _, provider := range testCases {
		t.Run(provider, func(t *testing.T) {
			t.Parallel()
			cfg := config.ServiceDiscoveryConfig{
				Provider: provider,
				SSE: config.SSEDiscoveryConfig{
					Services: []config.SSEServiceConfig{
						{
							Name:           "test-service",
							Namespace:      "default",
							BaseURL:        "http://localhost:8080",
							StreamEndpoint: "/sse",
						},
					},
				},
			}

			sd, err := CreateServiceDiscoveryProvider(cfg, logger)

			// SSE discovery should be created successfully with valid config
			assert.NoError(t, err)
			assert.NotNil(t, sd)

			// Verify it's an SSE discovery
			sseSD, ok := sd.(*SSEDiscovery)
			assert.True(t, ok)
			assert.NotNil(t, sseSD)
		})
	}
}

func TestCreateServiceDiscoveryProvider_STDIO(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)

	cfg := config.ServiceDiscoveryConfig{
		Provider: "stdio",
		Stdio: config.StdioDiscoveryConfig{
			Services: []config.StdioServiceConfig{
				{
					Name:      "test-service",
					Namespace: "default",
					Command:   []string{"echo", "test"},
				},
			},
		},
	}

	sd, err := CreateServiceDiscoveryProvider(cfg, logger)

	// STDIO discovery should be created successfully with valid config
	assert.NoError(t, err)
	assert.NotNil(t, sd)

	// Verify it's a STDIO discovery
	stdioSD, ok := sd.(*StdioDiscovery)
	assert.True(t, ok)
	assert.NotNil(t, stdioSD)
}

func TestCreateServiceDiscoveryProvider_StaticWithInvalidConfig(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)

	cfg := config.ServiceDiscoveryConfig{
		Provider: "static",
		Static: config.StaticDiscoveryConfig{
			Endpoints: nil, // Invalid - should cause error
		},
	}

	sd, err := CreateServiceDiscoveryProvider(cfg, logger)
	assert.Error(t, err)
	assert.Nil(t, sd)
	assert.Contains(t, err.Error(), "static endpoints configuration is required")
}
