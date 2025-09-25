
package discovery

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/poiley/mcp-bridge/services/gateway/internal/config"
)

func TestCreateStaticServiceDiscovery(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)
	tests := createStaticDiscoveryCreationTests()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sd, err := CreateStaticServiceDiscovery(tt.config, logger)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				assert.Nil(t, sd)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, sd)
				assert.NotNil(t, sd.endpoints)
			}
		})
	}
}

func createStaticDiscoveryCreationTests() []struct {
	name        string
	config      config.ServiceDiscoveryConfig
	wantErr     bool
	errContains string
} {
	return []struct {
		name        string
		config      config.ServiceDiscoveryConfig
		wantErr     bool
		errContains string
	}{
		{
			name: "valid config",
			config: config.ServiceDiscoveryConfig{
				Static: config.StaticDiscoveryConfig{
					Endpoints: map[string][]config.EndpointConfig{
						"default": {
							{
								URL: "http://localhost:8080",
								Labels: map[string]interface{}{
									"protocol": "http",
									"version":  "1.0",
								},
							},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "nil endpoints",
			config: config.ServiceDiscoveryConfig{
				Static: config.StaticDiscoveryConfig{
					Endpoints: nil,
				},
			},
			wantErr:     true,
			errContains: "static endpoints configuration is required",
		},
		{
			name: "empty endpoints map",
			config: config.ServiceDiscoveryConfig{
				Static: config.StaticDiscoveryConfig{
					Endpoints: make(map[string][]config.EndpointConfig),
				},
			},
			wantErr: false,
		},
	}
}

func TestStaticDiscovery_LoadEndpoints(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)
	config := createStaticLoadEndpointsConfig()
	sd, err := CreateStaticServiceDiscovery(config, logger)
	require.NoError(t, err)
	require.NotNil(t, sd)

	verifyLoadedEndpoints(t, sd)
}

func createStaticLoadEndpointsConfig() config.ServiceDiscoveryConfig {
	return config.ServiceDiscoveryConfig{
		Static: config.StaticDiscoveryConfig{
			Endpoints: map[string][]config.EndpointConfig{
				"default": {
					{
						URL: "http://localhost:8080/mcp",
						Labels: map[string]interface{}{
							"protocol": "http",
							"version":  "1.0",
							"env":      "test",
						},
					},
					{
						URL: "ws://localhost:8081/ws",
						Labels: map[string]interface{}{
							"protocol": "websocket",
							"version":  "1.1",
						},
					},
				},
				"production": {
					{
						URL: "https://prod.example.com:8443/mcp",
						Labels: map[string]interface{}{
							"protocol": "https",
							"env":      "production",
						},
					},
				},
			},
		},
	}
}

func verifyLoadedEndpoints(t *testing.T, sd *StaticDiscovery) {
	t.Helper()
	// Verify endpoints were loaded correctly
	allEndpoints := sd.GetAllEndpoints()
	
	// Count total endpoints across all namespaces
	totalCount := 0
	for _, endpoints := range allEndpoints {
		totalCount += len(endpoints)
	}
	assert.Equal(t, 3, totalCount) // 2 default + 1 production

	// Check default namespace endpoints
	defaultEndpoints := sd.GetEndpoints("default")
	assert.Len(t, defaultEndpoints, 2)

	// Verify first endpoint
	ep1 := defaultEndpoints[0]
	assert.Equal(t, "localhost", ep1.Address)
	assert.Equal(t, 8080, ep1.Port)
	assert.Equal(t, "http", ep1.Scheme)
	assert.Equal(t, "/mcp", ep1.Path)
	assert.Equal(t, "default", ep1.Namespace)
	assert.Equal(t, "http", ep1.Metadata["protocol"])
	assert.Equal(t, "1.0", ep1.Metadata["version"])
	assert.Equal(t, "test", ep1.Metadata["env"])

	// Check production namespace endpoints
	prodEndpoints := sd.GetEndpoints("production")
	assert.Len(t, prodEndpoints, 1)

	ep3 := prodEndpoints[0]
	assert.Equal(t, "prod.example.com", ep3.Address)
	assert.Equal(t, 8443, ep3.Port)
	assert.Equal(t, "https", ep3.Scheme)
	assert.Equal(t, "/mcp", ep3.Path)
	assert.Equal(t, "production", ep3.Namespace)
	assert.Equal(t, "https", ep3.Metadata["protocol"])
	assert.Equal(t, "production", ep3.Metadata["env"])
}

func TestStaticDiscovery_GetEndpoints(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)

	config := config.ServiceDiscoveryConfig{
		Static: config.StaticDiscoveryConfig{
			Endpoints: map[string][]config.EndpointConfig{
				"namespace1": {
					{
						URL: "http://service1.example.com:8080",
						Labels: map[string]interface{}{
							"protocol": "http",
						},
					},
				},
				"namespace2": {
					{
						URL: "ws://service2.example.com:8081",
						Labels: map[string]interface{}{
							"protocol": "websocket",
						},
					},
				},
			},
		},
	}

	sd, err := CreateStaticServiceDiscovery(config, logger)
	require.NoError(t, err)

	// Test getting existing namespaces
	ns1Endpoints := sd.GetEndpoints("namespace1")
	assert.Len(t, ns1Endpoints, 1)
	assert.Equal(t, "service1.example.com", ns1Endpoints[0].Address)
	assert.Equal(t, 8080, ns1Endpoints[0].Port)

	ns2Endpoints := sd.GetEndpoints("namespace2")
	assert.Len(t, ns2Endpoints, 1)
	assert.Equal(t, "service2.example.com", ns2Endpoints[0].Address)
	assert.Equal(t, 8081, ns2Endpoints[0].Port)

	// Test getting non-existent namespace
	nonExistentEndpoints := sd.GetEndpoints("nonexistent")
	assert.Empty(t, nonExistentEndpoints)
}

func TestStaticDiscovery_GetAllEndpoints(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)

	config := config.ServiceDiscoveryConfig{
		Static: config.StaticDiscoveryConfig{
			Endpoints: map[string][]config.EndpointConfig{
				"ns1": {
					{URL: "http://service1.com", Labels: map[string]interface{}{"protocol": "http"}},
					{URL: "http://service2.com", Labels: map[string]interface{}{"protocol": "http"}},
				},
				"ns2": {
					{URL: "ws://service3.com", Labels: map[string]interface{}{"protocol": "websocket"}},
				},
			},
		},
	}

	sd, err := CreateStaticServiceDiscovery(config, logger)
	require.NoError(t, err)

	allEndpoints := sd.GetAllEndpoints()

	// Flatten all endpoints to count total
	var totalEndpoints []Endpoint
	for _, nsEndpoints := range allEndpoints {
		totalEndpoints = append(totalEndpoints, nsEndpoints...)
	}

	assert.Len(t, totalEndpoints, 3)

	// Verify all endpoints are present by checking addresses
	addresses := make([]string, len(totalEndpoints))
	for i, ep := range totalEndpoints {
		addresses[i] = ep.Address
	}

	assert.Contains(t, addresses, "service1.com")
	assert.Contains(t, addresses, "service2.com")
	assert.Contains(t, addresses, "service3.com")
}

func TestStaticDiscovery_ListNamespaces(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)

	config := config.ServiceDiscoveryConfig{
		Static: config.StaticDiscoveryConfig{
			Endpoints: map[string][]config.EndpointConfig{
				"production":  {{URL: "http://prod.com", Labels: map[string]interface{}{"env": "prod"}}},
				"staging":     {{URL: "http://staging.com", Labels: map[string]interface{}{"env": "staging"}}},
				"development": {{URL: "http://dev.com", Labels: map[string]interface{}{"env": "dev"}}},
			},
		},
	}

	sd, err := CreateStaticServiceDiscovery(config, logger)
	require.NoError(t, err)

	namespaces := sd.ListNamespaces()
	assert.Len(t, namespaces, 3)
	assert.Contains(t, namespaces, "production")
	assert.Contains(t, namespaces, "staging")
	assert.Contains(t, namespaces, "development")
}

func TestStaticDiscovery_StartStop(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)

	config := config.ServiceDiscoveryConfig{
		Static: config.StaticDiscoveryConfig{
			Endpoints: map[string][]config.EndpointConfig{
				"default": {
					{URL: "http://localhost:8080", Labels: map[string]interface{}{"protocol": "http"}},
				},
			},
		},
	}

	sd, err := CreateStaticServiceDiscovery(config, logger)
	require.NoError(t, err)

	// Test Start - should be no-op for static discovery
	err = sd.Start(context.Background())
	assert.NoError(t, err)

	// Test Stop - should be no-op for static discovery
	sd.Stop()

	// Multiple starts/stops should be safe
	err = sd.Start(context.Background())
	assert.NoError(t, err)
	sd.Stop()
}

func TestStaticDiscovery_InvalidURL(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)

	config := config.ServiceDiscoveryConfig{
		Static: config.StaticDiscoveryConfig{
			Endpoints: map[string][]config.EndpointConfig{
				"default": {
					{
						URL:    "://invalid-url",
						Labels: map[string]interface{}{"protocol": "http"},
					},
				},
			},
		},
	}

	sd, err := CreateStaticServiceDiscovery(config, logger)

	// The current implementation logs errors but doesn't fail creation
	// It should still create the discovery service but skip invalid endpoints
	assert.NoError(t, err)
	assert.NotNil(t, sd)

	// The invalid endpoint should be skipped, so default namespace should have 0 endpoints
	endpoints := sd.GetEndpoints("default")
	assert.Empty(t, endpoints)
}

func TestStaticDiscovery_EmptyNamespace(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)

	config := config.ServiceDiscoveryConfig{
		Static: config.StaticDiscoveryConfig{
			Endpoints: map[string][]config.EndpointConfig{
				"": { // Empty namespace
					{
						URL:    "http://localhost:8080",
						Labels: map[string]interface{}{"protocol": "http"},
					},
				},
			},
		},
	}

	sd, err := CreateStaticServiceDiscovery(config, logger)
	require.NoError(t, err)

	// Should be able to get endpoints for empty namespace
	endpoints := sd.GetEndpoints("")
	assert.Len(t, endpoints, 1)
	assert.Equal(t, "localhost", endpoints[0].Address)
	assert.Equal(t, 8080, endpoints[0].Port)

	// Empty namespace should appear in namespace list
	namespaces := sd.ListNamespaces()
	assert.Contains(t, namespaces, "")
}

func TestStaticDiscovery_URLParsing(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)
	tests := createStaticURLParsingTests()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			config := config.ServiceDiscoveryConfig{
				Static: config.StaticDiscoveryConfig{
					Endpoints: map[string][]config.EndpointConfig{
						"test": {
							{
								URL:    tt.url,
								Labels: map[string]interface{}{"test": "true"},
							},
						},
					},
				},
			}
			sd, err := CreateStaticServiceDiscovery(config, logger)
			require.NoError(t, err)
			endpoints := sd.GetEndpoints("test")
			require.Len(t, endpoints, 1)
			ep := endpoints[0]
			assert.Equal(t, tt.expectedAddr, ep.Address)
			assert.Equal(t, tt.expectedPort, ep.Port)
			assert.Equal(t, tt.expectedScheme, ep.Scheme)
			assert.Equal(t, tt.expectedPath, ep.Path)
		})
	}
}

func createStaticURLParsingTests() []struct {
	name           string
	url            string
	expectedAddr   string
	expectedPort   int
	expectedScheme string
	expectedPath   string
} {
	return []struct {
		name           string
		url            string
		expectedAddr   string
		expectedPort   int
		expectedScheme string
		expectedPath   string
	}{
		{
			name:           "HTTP with explicit port",
			url:            "http://example.com:8080/api",
			expectedAddr:   "example.com",
			expectedPort:   8080,
			expectedScheme: "http",
			expectedPath:   "/api",
		},
		{
			name:           "HTTPS default port",
			url:            "https://secure.example.com/secure",
			expectedAddr:   "secure.example.com",
			expectedPort:   443,
			expectedScheme: "https",
			expectedPath:   "/secure",
		},
		{
			name:           "HTTP default port",
			url:            "http://plain.example.com",
			expectedAddr:   "plain.example.com",
			expectedPort:   80,
			expectedScheme: "http",
			expectedPath:   "",
		},
		{
			name:           "WebSocket",
			url:            "ws://ws.example.com:9000/ws",
			expectedAddr:   "ws.example.com",
			expectedPort:   9000,
			expectedScheme: "ws",
			expectedPath:   "/ws",
		},
	}
}
