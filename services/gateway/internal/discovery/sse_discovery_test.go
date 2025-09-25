
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

// Test constants.
const (
	testService1 = "service1"
)

func TestCreateSSEServiceDiscovery(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)
	tests := createSSEDiscoveryCreationTests()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sd, err := CreateSSEServiceDiscovery(tt.config, logger)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, sd)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, sd)
				sseSD, ok := sd.(*SSEDiscovery)
				assert.True(t, ok)
				assert.NotNil(t, sseSD)
				assert.NotNil(t, sseSD.httpClient)
			}
		})
	}
}

func createSSEDiscoveryCreationTests() []struct {
	name    string
	config  config.SSEDiscoveryConfig
	wantErr bool
} {
	return []struct {
		name    string
		config  config.SSEDiscoveryConfig
		wantErr bool
	}{
		{
			name: "valid config with single service",
			config: config.SSEDiscoveryConfig{
				Services: []config.SSEServiceConfig{
					{
						Name:      "test-service",
						Namespace: "default",
						BaseURL:   "http://localhost:8080",
						Weight:    10,
					},
				},
			},
			wantErr: false,
		},
		{
			name: "valid config with multiple services",
			config: config.SSEDiscoveryConfig{
				Services: []config.SSEServiceConfig{
					{
						Name:      testService1,
						Namespace: "ns1",
						BaseURL:   "http://service1.example.com",
					},
					{
						Name:      "service2",
						Namespace: "ns2",
						BaseURL:   "https://service2.example.com",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "empty services list",
			config: config.SSEDiscoveryConfig{
				Services: []config.SSEServiceConfig{},
			},
			wantErr: false,
		},
	}
}

func TestSSEDiscovery_ConfigDefaults(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)

	config := config.SSEDiscoveryConfig{
		Services: []config.SSEServiceConfig{
			{
				Name:    "test-service",
				BaseURL: "http://localhost:8080",
				// Missing namespace, weight, endpoints, timeout, and health check config
			},
		},
	}

	sd, err := CreateSSEServiceDiscovery(config, logger)
	require.NoError(t, err)
	require.NotNil(t, sd)

	sseSD, ok := sd.(*SSEDiscovery)
	require.True(t, ok, "Expected SSEDiscovery type")

	// Verify defaults were applied
	service := sseSD.services["test-service"]
	assert.Equal(t, "default", service.Namespace)
	assert.Equal(t, 1, service.Weight)
	assert.Equal(t, "/events", service.StreamEndpoint)
	assert.Equal(t, "/api/v1/request", service.RequestEndpoint)
	assert.Equal(t, 10*time.Second, service.Timeout)
	assert.Equal(t, 30*time.Second, service.HealthCheck.Interval)
	assert.Equal(t, 5*time.Second, service.HealthCheck.Timeout)
}

func TestSSEDiscovery_StartStop(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)

	config := config.SSEDiscoveryConfig{
		Services: []config.SSEServiceConfig{
			{
				Name:      "test-service",
				Namespace: "default",
				BaseURL:   "http://localhost:8080",
			},
		},
	}

	sd, err := CreateSSEServiceDiscovery(config, logger)
	require.NoError(t, err)

	// Test Start
	err = sd.Start(context.Background())
	assert.NoError(t, err)

	// Test Stop
	// Stop should not error
	sd.Stop()
}

func TestSSEDiscovery_GetEndpoints(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)

	config := config.SSEDiscoveryConfig{
		Services: []config.SSEServiceConfig{
			{
				Name:      "service1",
				Namespace: "ns1",
				BaseURL:   "http://service1.example.com",
				Weight:    10,
				Metadata:  map[string]string{"type": "api"},
			},
			{
				Name:      "service2",
				Namespace: "ns2",
				BaseURL:   "https://service2.example.com",
				Weight:    20,
			},
			{
				Name:      "service3",
				Namespace: "ns1", // Same namespace as service1
				BaseURL:   "http://service3.example.com",
			},
		},
	}

	sd, err := CreateSSEServiceDiscovery(config, logger)
	require.NoError(t, err)

	// Test getting endpoints for specific namespace
	ns1Endpoints := sd.GetEndpoints("ns1")
	assert.Len(t, ns1Endpoints, 2) // service1 and service3

	// Verify endpoint details for service1
	var service1Endpoint *Endpoint

	for i := range ns1Endpoints {
		if ns1Endpoints[i].Service == testService1 {
			service1Endpoint = &ns1Endpoints[i]

			break
		}
	}

	require.NotNil(t, service1Endpoint)
	assert.Equal(t, testService1, service1Endpoint.Service)
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

func TestSSEDiscovery_GetAllEndpoints(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)

	config := config.SSEDiscoveryConfig{
		Services: []config.SSEServiceConfig{
			{
				Name:      "service1",
				Namespace: "ns1",
				BaseURL:   "http://service1.example.com",
			},
			{
				Name:      "service2",
				Namespace: "ns2",
				BaseURL:   "https://service2.example.com",
			},
			{
				Name:      "service3",
				Namespace: "ns1",
				BaseURL:   "http://service3.example.com",
			},
		},
	}

	sd, err := CreateSSEServiceDiscovery(config, logger)
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

func TestSSEDiscovery_ListNamespaces(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)

	config := config.SSEDiscoveryConfig{
		Services: []config.SSEServiceConfig{
			{
				Name:      "service1",
				Namespace: "production",
				BaseURL:   "https://prod.example.com",
			},
			{
				Name:      "service2",
				Namespace: "staging",
				BaseURL:   "https://staging.example.com",
			},
			{
				Name:      "service3",
				Namespace: "development",
				BaseURL:   "http://dev.example.com",
			},
			{
				Name:      "service4",
				Namespace: "production", // Duplicate namespace
				BaseURL:   "https://prod2.example.com",
			},
		},
	}

	sd, err := CreateSSEServiceDiscovery(config, logger)
	require.NoError(t, err)

	namespaces := sd.ListNamespaces()

	// Should have 3 unique namespaces
	assert.Len(t, namespaces, 3)
	assert.Contains(t, namespaces, "production")
	assert.Contains(t, namespaces, "staging")
	assert.Contains(t, namespaces, "development")
}

func TestSSEDiscovery_EmptyConfig(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)

	config := config.SSEDiscoveryConfig{
		Services: []config.SSEServiceConfig{},
	}

	sd, err := CreateSSEServiceDiscovery(config, logger)
	require.NoError(t, err)

	// Test with empty configuration
	endpoints := sd.GetEndpoints("default")
	assert.Empty(t, endpoints)

	allEndpoints := sd.GetAllEndpoints()
	assert.Empty(t, allEndpoints)

	namespaces := sd.ListNamespaces()
	assert.Empty(t, namespaces)
}

func TestSSEDiscovery_ServiceWithFullConfig(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)

	config := config.SSEDiscoveryConfig{
		Services: []config.SSEServiceConfig{
			{
				Name:            "full-service",
				Namespace:       "custom",
				BaseURL:         "https://api.example.com",
				StreamEndpoint:  "/events/stream",
				RequestEndpoint: "/api/v2/request",
				Headers: map[string]string{
					"User-Agent":    "MCP-Gateway/1.0",
					"Authorization": "Bearer secret-token", // Should be filtered out
					"Accept":        "text/event-stream",
				},
				Weight: testTimeout,
				Metadata: map[string]string{
					"version": "1.0.0",
					"type":    "api-gateway",
				},
				HealthCheck: config.SSEHealthCheckConfig{
					Enabled:  true,
					Interval: 60 * time.Second,
					Timeout:  10 * time.Second,
				},
				Timeout: 30 * time.Second,
			},
		},
	}

	sd, err := CreateSSEServiceDiscovery(config, logger)
	require.NoError(t, err)

	endpoints := sd.GetEndpoints("custom")
	require.Len(t, endpoints, 1)

	endpoint := endpoints[0]
	assert.Equal(t, "full-service", endpoint.Service)
	assert.Equal(t, "custom", endpoint.Namespace)
	assert.Equal(t, testTimeout, endpoint.Weight)
	assert.Equal(t, "1.0.0", endpoint.Metadata["version"])
	assert.Equal(t, "api-gateway", endpoint.Metadata["type"])
}

func TestSSEDiscovery_CreateMetadata(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)

	config := config.SSEDiscoveryConfig{
		Services: []config.SSEServiceConfig{
			{
				Name:            "metadata-service",
				Namespace:       "default",
				BaseURL:         "https://api.example.com",
				StreamEndpoint:  "/events/stream",
				RequestEndpoint: "/api/v2/request",
				Headers: map[string]string{
					"User-Agent":    "MCP-Gateway/1.0",
					"Authorization": "Bearer secret-token", // Should be filtered out
					"Accept":        "text/event-stream",
					"X-API-Key":     "secret-key", // Should be filtered out
					"Content-Type":  "application/json",
				},
				Metadata: map[string]string{
					"framework": "sse",
					"protocol":  "https",
				},
				HealthCheck: config.SSEHealthCheckConfig{
					Enabled: true,
				},
				Timeout: 25 * time.Second,
			},
		},
	}

	sd, err := CreateSSEServiceDiscovery(config, logger)
	require.NoError(t, err)

	endpoints := sd.GetEndpoints("default")
	require.Len(t, endpoints, 1)

	endpoint := endpoints[0]

	// Verify that metadata includes both service-specific and auto-generated metadata
	assert.Equal(t, "sse", endpoint.Metadata["framework"])
	assert.Equal(t, "sse", endpoint.Metadata["protocol"]) // Auto-generated takes precedence

	// Should have discovery-specific metadata
	assert.Equal(t, "sse", endpoint.Metadata["protocol"]) // Auto-generated takes precedence
	assert.Equal(t, "https://api.example.com", endpoint.Metadata["base_url"])
	assert.Equal(t, "/events/stream", endpoint.Metadata["stream_endpoint"])
	assert.Equal(t, "/api/v2/request", endpoint.Metadata["request_endpoint"])
	assert.Equal(t, "true", endpoint.Metadata["health_check_enabled"])
	assert.Equal(t, "25s", endpoint.Metadata["timeout"])

	// Should have non-sensitive headers
	assert.Equal(t, "MCP-Gateway/1.0", endpoint.Metadata["header_user-agent"])
	assert.Equal(t, "text/event-stream", endpoint.Metadata["header_accept"])
	assert.Equal(t, "application/json", endpoint.Metadata["header_content-type"])

	// Should NOT have sensitive headers
	assert.NotContains(t, endpoint.Metadata, "header_authorization")
	assert.NotContains(t, endpoint.Metadata, "header_x-api-key")
}

func TestSSEDiscovery_MetadataFiltering(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)

	config := config.SSEDiscoveryConfig{
		Services: []config.SSEServiceConfig{
			{
				Name:    "security-test",
				BaseURL: "https://secure.example.com",
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

	sd, err := CreateSSEServiceDiscovery(config, logger)
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

func TestSSEDiscovery_URLParsing(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)
	tests := createSSEURLParsingTests()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			config := config.SSEDiscoveryConfig{
				Services: []config.SSEServiceConfig{
					{
						Name:    "test-service",
						BaseURL: tt.baseURL,
					},
				},
			}
			sd, err := CreateSSEServiceDiscovery(config, logger)
			require.NoError(t, err)
			endpoints := sd.GetEndpoints("default")
			require.Len(t, endpoints, 1)
			ep := endpoints[0]
			assert.Equal(t, tt.expectedHost, ep.Address)
			assert.Equal(t, tt.expectedPort, ep.Port)
			assert.Equal(t, tt.expectedScheme, ep.Scheme)
		})
	}
}

func createSSEURLParsingTests() []struct {
	name           string
	baseURL        string
	expectedScheme string
	expectedHost   string
	expectedPort   int
} {
	return []struct {
		name           string
		baseURL        string
		expectedScheme string
		expectedHost   string
		expectedPort   int
	}{
		{
			name:           "HTTP with explicit port",
			baseURL:        "http://example.com:8080",
			expectedScheme: "http",
			expectedHost:   "example.com",
			expectedPort:   8080,
		},
		{
			name:           "HTTPS default port",
			baseURL:        "https://secure.example.com",
			expectedScheme: "https",
			expectedHost:   "secure.example.com",
			expectedPort:   443,
		},
		{
			name:           "HTTP default port",
			baseURL:        "http://plain.example.com",
			expectedScheme: "http",
			expectedHost:   "plain.example.com",
			expectedPort:   80,
		},
		{
			name:           "Custom port",
			baseURL:        "https://api.example.com:9443",
			expectedScheme: "https",
			expectedHost:   "api.example.com",
			expectedPort:   9443,
		},
	}
}
