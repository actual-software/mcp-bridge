
package discovery

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"

	"github.com/poiley/mcp-bridge/services/gateway/internal/config"
)

func TestCreateWebSocketServiceDiscovery(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)

	tests := createWebSocketDiscoveryCreationTests()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sd, err := CreateWebSocketServiceDiscovery(tt.config, logger)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, sd)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, sd)
				wsSD, ok := sd.(*WebSocketDiscovery)
				assert.True(t, ok)
				assert.NotNil(t, wsSD)
			}
		})
	}
}

func createWebSocketDiscoveryCreationTests() []struct {
	name    string
	config  config.WebSocketDiscoveryConfig
	wantErr bool
} {
	return []struct {
		name    string
		config  config.WebSocketDiscoveryConfig
		wantErr bool
	}{
		{
			name: "valid config with single service",
			config: config.WebSocketDiscoveryConfig{
				Services: []config.WebSocketServiceConfig{
					{
						Name:      "test-service",
						Namespace: "default",
						Endpoints: []string{"ws://localhost:8080/ws"},
						Weight:    10,
					},
				},
			},
			wantErr: false,
		},
		{
			name: "valid config with multiple services",
			config: config.WebSocketDiscoveryConfig{
				Services: []config.WebSocketServiceConfig{
					{
						Name:      "service1",
						Namespace: "ns1",
						Endpoints: []string{"ws://service1.example.com/ws"},
					},
					{
						Name:      "service2",
						Namespace: "ns2",
						Endpoints: []string{"wss://service2.example.com/ws"},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "empty services list",
			config: config.WebSocketDiscoveryConfig{
				Services: []config.WebSocketServiceConfig{},
			},
			wantErr: false,
		},
	}
}

func TestWebSocketDiscovery_ConfigDefaults(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)

	config := config.WebSocketDiscoveryConfig{
		Services: []config.WebSocketServiceConfig{
			{
				Name:      "test-service",
				Endpoints: []string{"ws://localhost:8080/ws"},
				// Missing namespace, weight, and health check config
			},
		},
	}

	sd, err := CreateWebSocketServiceDiscovery(config, logger)
	require.NoError(t, err)
	require.NotNil(t, sd)

	wsSD, ok := sd.(*WebSocketDiscovery)
	require.True(t, ok, "Expected WebSocketDiscovery type")

	// Verify defaults were applied
	service := wsSD.services["test-service"]
	assert.Equal(t, "default", service.Namespace)
	assert.Equal(t, 1, service.Weight)
	assert.Equal(t, 30*time.Second, service.HealthCheck.Interval)
	assert.Equal(t, 5*time.Second, service.HealthCheck.Timeout)
}

func TestWebSocketDiscovery_StartStop(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)

	config := config.WebSocketDiscoveryConfig{
		Services: []config.WebSocketServiceConfig{
			{
				Name:      "test-service",
				Namespace: "default",
				Endpoints: []string{"ws://localhost:8080/ws"},
			},
		},
	}

	sd, err := CreateWebSocketServiceDiscovery(config, logger)
	require.NoError(t, err)

	// Test Start
	err = sd.Start(context.Background())
	assert.NoError(t, err)

	// Test Stop
	// Stop should not error
	sd.Stop()
}

func TestWebSocketDiscovery_GetEndpoints(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)

	config := config.WebSocketDiscoveryConfig{
		Services: []config.WebSocketServiceConfig{
			{
				Name:      "service1",
				Namespace: "ns1",
				Endpoints: []string{"ws://service1.example.com/ws"},
				Weight:    10,
				Metadata:  map[string]string{"type": "api"},
			},
			{
				Name:      "service2",
				Namespace: "ns2",
				Endpoints: []string{"wss://service2.example.com/ws"},
				Weight:    20,
			},
			{
				Name:      "service3",
				Namespace: "ns1", // Same namespace as service1
				Endpoints: []string{"ws://service3.example.com/ws"},
			},
		},
	}

	sd, err := CreateWebSocketServiceDiscovery(config, logger)
	require.NoError(t, err)

	// Test getting endpoints for specific namespace
	ns1Endpoints := sd.GetEndpoints("ns1")
	assert.Len(t, ns1Endpoints, 2) // service1 and service3

	// Verify endpoint details for service1
	var service1Endpoint *Endpoint

	for i := range ns1Endpoints {
		if ns1Endpoints[i].Service == "service1" {
			service1Endpoint = &ns1Endpoints[i]

			break
		}
	}

	require.NotNil(t, service1Endpoint)
	assert.Equal(t, "service1", service1Endpoint.Service)
	assert.Equal(t, "ns1", service1Endpoint.Namespace)
	assert.Equal(t, 10, service1Endpoint.Weight)
	assert.Equal(t, "api", service1Endpoint.Metadata["type"])

	// Test getting endpoints for different namespace
	ns2Endpoints := sd.GetEndpoints("ns2")
	assert.Len(t, ns2Endpoints, 1) // only service2

	// Test getting endpoints for non-existent namespace
	nonExistentEndpoints := sd.GetEndpoints("nonexistent")
	assert.Empty(t, nonExistentEndpoints)
}

func TestWebSocketDiscovery_GetAllEndpoints(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)

	config := config.WebSocketDiscoveryConfig{
		Services: []config.WebSocketServiceConfig{
			{
				Name:      "service1",
				Namespace: "ns1",
				Endpoints: []string{"ws://service1.example.com/ws"},
			},
			{
				Name:      "service2",
				Namespace: "ns2",
				Endpoints: []string{"wss://service2.example.com/ws"},
			},
			{
				Name:      "service3",
				Namespace: "ns1",
				Endpoints: []string{"ws://service3.example.com/ws"},
			},
		},
	}

	sd, err := CreateWebSocketServiceDiscovery(config, logger)
	require.NoError(t, err)

	allEndpoints := sd.GetAllEndpoints()

	// Should have 2 namespaces
	assert.Len(t, allEndpoints, 2)
	assert.Contains(t, allEndpoints, "ns1")
	assert.Contains(t, allEndpoints, "ns2")

	// ns1 should have 2 services
	assert.Len(t, allEndpoints["ns1"], 2)

	// ns2 should have 1 service
	assert.Len(t, allEndpoints["ns2"], 1)

	// Verify total endpoint count
	totalCount := 0
	for _, endpoints := range allEndpoints {
		totalCount += len(endpoints)
	}

	assert.Equal(t, 3, totalCount)
}

func TestWebSocketDiscovery_ListNamespaces(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)

	config := config.WebSocketDiscoveryConfig{
		Services: []config.WebSocketServiceConfig{
			{
				Name:      "service1",
				Namespace: "production",
				Endpoints: []string{"wss://prod.example.com/ws"},
			},
			{
				Name:      "service2",
				Namespace: "staging",
				Endpoints: []string{"wss://staging.example.com/ws"},
			},
			{
				Name:      "service3",
				Namespace: "development",
				Endpoints: []string{"ws://dev.example.com/ws"},
			},
			{
				Name:      "service4",
				Namespace: "production", // Duplicate namespace
				Endpoints: []string{"wss://prod2.example.com/ws"},
			},
		},
	}

	sd, err := CreateWebSocketServiceDiscovery(config, logger)
	require.NoError(t, err)

	namespaces := sd.ListNamespaces()

	// Should have 3 unique namespaces
	assert.Len(t, namespaces, 3)
	assert.Contains(t, namespaces, "production")
	assert.Contains(t, namespaces, "staging")
	assert.Contains(t, namespaces, "development")
}

func TestWebSocketDiscovery_EmptyConfig(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)

	config := config.WebSocketDiscoveryConfig{
		Services: []config.WebSocketServiceConfig{},
	}

	sd, err := CreateWebSocketServiceDiscovery(config, logger)
	require.NoError(t, err)

	// Test with empty configuration
	endpoints := sd.GetEndpoints("default")
	assert.Empty(t, endpoints)

	allEndpoints := sd.GetAllEndpoints()
	assert.Empty(t, allEndpoints)

	namespaces := sd.ListNamespaces()
	assert.Empty(t, namespaces)
}

func TestWebSocketDiscovery_ServiceWithFullConfig(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)

	config := config.WebSocketDiscoveryConfig{
		Services: []config.WebSocketServiceConfig{
			{
				Name:      "full-service",
				Namespace: "custom",
				Endpoints: []string{"wss://api.example.com/ws", "wss://api2.example.com/ws"},
				Headers: map[string]string{
					"User-Agent":    "MCP-Gateway/1.0",
					"Authorization": "Bearer secret-token", // Should be filtered out
					"Origin":        "https://example.com",
				},
				Weight: testTimeout,
				Metadata: map[string]string{
					"version": "1.0.0",
					"type":    "api-gateway",
				},
				HealthCheck: config.WebSocketHealthCheckConfig{
					Enabled:  true,
					Interval: 60 * time.Second,
					Timeout:  10 * time.Second,
				},
				TLS: config.WebSocketTLSConfig{
					Enabled:            true,
					InsecureSkipVerify: false,
				},
				Origin: "https://example.com",
			},
		},
	}

	sd, err := CreateWebSocketServiceDiscovery(config, logger)
	require.NoError(t, err)

	endpoints := sd.GetEndpoints("custom")
	require.Len(t, endpoints, 2) // Should create one endpoint per WebSocket URL

	endpoint := endpoints[0]
	assert.Equal(t, "full-service", endpoint.Service)
	assert.Equal(t, "custom", endpoint.Namespace)
	assert.Equal(t, testTimeout, endpoint.Weight)
	assert.Equal(t, "1.0.0", endpoint.Metadata["version"])
	assert.Equal(t, "api-gateway", endpoint.Metadata["type"])
}

func TestWebSocketDiscovery_CreateMetadata(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)

	config := config.WebSocketDiscoveryConfig{
		Services: []config.WebSocketServiceConfig{
			{
				Name:      "metadata-service",
				Namespace: "default",
				Endpoints: []string{"wss://api1.example.com/ws", "wss://api2.example.com/ws"},
				Headers: map[string]string{
					"User-Agent":    "MCP-Gateway/1.0",
					"Authorization": "Bearer secret-token", // Should be filtered out
					"Origin":        "https://example.com",
					"X-API-Key":     "secret-key", // Should be filtered out
					"Content-Type":  "application/json",
				},
				Metadata: map[string]string{
					"framework": "websocket",
					"protocol":  "wss",
				},
				HealthCheck: config.WebSocketHealthCheckConfig{
					Enabled: true,
				},
				TLS: config.WebSocketTLSConfig{
					Enabled: true,
				},
				Origin: "https://example.com",
			},
		},
	}

	sd, err := CreateWebSocketServiceDiscovery(config, logger)
	require.NoError(t, err)

	endpoints := sd.GetEndpoints("default")
	require.Len(t, endpoints, 2) // One per WebSocket URL

	endpoint := endpoints[0]

	// Verify that metadata includes both service-specific and auto-generated metadata
	assert.Equal(t, "websocket", endpoint.Metadata["framework"])
	assert.Equal(t, "websocket", endpoint.Metadata["protocol"]) // Auto-generated takes precedence

	// Should have discovery-specific metadata
	assert.Equal(t, "websocket", endpoint.Metadata["protocol"])
	assert.Contains(t, endpoint.Metadata["endpoint_url"], "api") // Should contain one of the endpoints
	assert.Equal(t, "true", endpoint.Metadata["health_check_enabled"])
	assert.Equal(t, "true", endpoint.Metadata["tls_enabled"])
	assert.Equal(t, "https://example.com", endpoint.Metadata["origin"])

	// Should have all endpoints listed
	assert.Contains(t, endpoint.Metadata["all_endpoints"], "wss://api1.example.com/ws")
	assert.Contains(t, endpoint.Metadata["all_endpoints"], "wss://api2.example.com/ws")

	// Should have non-sensitive headers
	assert.Equal(t, "MCP-Gateway/1.0", endpoint.Metadata["header_user-agent"])
	assert.Equal(t, "https://example.com", endpoint.Metadata["header_origin"])
	assert.Equal(t, "application/json", endpoint.Metadata["header_content-type"])

	// Should NOT have sensitive headers
	assert.NotContains(t, endpoint.Metadata, "header_authorization")
	assert.NotContains(t, endpoint.Metadata, "header_x-api-key")
}

func TestWebSocketDiscovery_MetadataFiltering(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)

	config := config.WebSocketDiscoveryConfig{
		Services: []config.WebSocketServiceConfig{
			{
				Name:      "security-test",
				Endpoints: []string{"wss://secure.example.com/ws"},
				Headers: map[string]string{
					"Authorization": "Bearer token123",
					"X-API-Key":     "apikey456",
					"Password":      "secret789",
					"Secret-Header": "confidential",
					"Token":         "jwt-token",
					"User-Agent":    "MCP-Gateway/1.0",  // Should be included
					"Accept":        "application/json", // Should be included
				},
			},
		},
	}

	sd, err := CreateWebSocketServiceDiscovery(config, logger)
	require.NoError(t, err)

	endpoints := sd.GetEndpoints("default")
	require.Len(t, endpoints, 1)

	endpoint := endpoints[0]

	// Should include non-sensitive headers
	assert.Contains(t, endpoint.Metadata, "header_user-agent")
	assert.Contains(t, endpoint.Metadata, "header_accept")

	// Should exclude sensitive headers
	assert.NotContains(t, endpoint.Metadata, "header_authorization")
	assert.NotContains(t, endpoint.Metadata, "header_x-api-key")
	assert.NotContains(t, endpoint.Metadata, "header_password")
	assert.NotContains(t, endpoint.Metadata, "header_secret-header")
	assert.NotContains(t, endpoint.Metadata, "header_token")
}

func TestWebSocketDiscovery_URLParsing(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)

	tests := createWebSocketURLParsingTests()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			config := config.WebSocketDiscoveryConfig{
				Services: []config.WebSocketServiceConfig{
					{
						Name:      "test-service",
						Endpoints: []string{tt.url},
					},
				},
			}
			sd, err := CreateWebSocketServiceDiscovery(config, logger)
			require.NoError(t, err)

			endpoints := sd.GetEndpoints("default")
			require.Len(t, endpoints, 1)
			ep := endpoints[0]
			assert.Equal(t, tt.expectedHost, ep.Address)
			assert.Equal(t, tt.expectedPort, ep.Port)
			assert.Equal(t, tt.expectedScheme, ep.Scheme)
			assert.Equal(t, tt.expectedPath, ep.Path)
		})
	}
}

func createWebSocketURLParsingTests() []struct {
	name           string
	url            string
	expectedScheme string
	expectedHost   string
	expectedPort   int
	expectedPath   string
} {
	return []struct {
		name           string
		url            string
		expectedScheme string
		expectedHost   string
		expectedPort   int
		expectedPath   string
	}{
		{
			name:           "WebSocket with explicit port",
			url:            "ws://example.com:8080/ws",
			expectedScheme: "ws",
			expectedHost:   "example.com",
			expectedPort:   8080,
			expectedPath:   "/ws",
		},
		{
			name:           "Secure WebSocket default port",
			url:            "wss://secure.example.com/secure",
			expectedScheme: "wss",
			expectedHost:   "secure.example.com",
			expectedPort:   443,
			expectedPath:   "/secure",
		},
		{
			name:           "WebSocket default port",
			url:            "ws://plain.example.com/api",
			expectedScheme: "ws",
			expectedHost:   "plain.example.com",
			expectedPort:   80,
			expectedPath:   "/api",
		},
		{
			name:           "Custom port",
			url:            "wss://api.example.com:9443/websocket",
			expectedScheme: "wss",
			expectedHost:   "api.example.com",
			expectedPort:   9443,
			expectedPath:   "/websocket",
		},
	}
}

func TestWebSocketDiscovery_MultipleEndpoints(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)

	config := config.WebSocketDiscoveryConfig{
		Services: []config.WebSocketServiceConfig{
			{
				Name:      "multi-endpoint-service",
				Namespace: "default",
				Endpoints: []string{
					"ws://endpoint1.example.com/ws",
					"wss://endpoint2.example.com/ws",
					"ws://endpoint3.example.com:8080/ws",
				},
				Weight: 25,
			},
		},
	}

	sd, err := CreateWebSocketServiceDiscovery(config, logger)
	require.NoError(t, err)

	endpoints := sd.GetEndpoints("default")
	require.Len(t, endpoints, 3) // Should create one endpoint per WebSocket URL

	// All endpoints should have the same service name and namespace
	for _, ep := range endpoints {
		assert.Equal(t, "multi-endpoint-service", ep.Service)
		assert.Equal(t, "default", ep.Namespace)
		assert.Equal(t, 25, ep.Weight)
	}

	// Verify different addresses and ports
	addresses := make([]string, len(endpoints))
	ports := make([]int, len(endpoints))

	for i, ep := range endpoints {
		addresses[i] = ep.Address
		ports[i] = ep.Port
	}

	assert.Contains(t, addresses, "endpoint1.example.com")
	assert.Contains(t, addresses, "endpoint2.example.com")
	assert.Contains(t, addresses, "endpoint3.example.com")

	assert.Contains(t, ports, 80)   // default for ws://
	assert.Contains(t, ports, 443)  // default for wss://
	assert.Contains(t, ports, 8080) // explicit port
}
