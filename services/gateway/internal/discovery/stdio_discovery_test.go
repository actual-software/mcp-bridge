
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

func TestCreateStdioServiceDiscovery(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)

	tests := createStdioDiscoveryCreationTests()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sd, err := CreateStdioServiceDiscovery(tt.config, logger)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, sd)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, sd)
				stdioSD, ok := sd.(*StdioDiscovery)
				assert.True(t, ok)
				assert.NotNil(t, stdioSD)
			}
		})
	}
}

func createStdioDiscoveryCreationTests() []struct {
	name    string
	config  config.StdioDiscoveryConfig
	wantErr bool
} {
	return []struct {
		name    string
		config  config.StdioDiscoveryConfig
		wantErr bool
	}{
		{
			name: "valid config with single service",
			config: config.StdioDiscoveryConfig{
				Services: []config.StdioServiceConfig{
					{
						Name:      "test-service",
						Namespace: "default",
						Command:   []string{"echo", "test"},
						Weight:    10,
					},
				},
			},
			wantErr: false,
		},
		{
			name: "valid config with multiple services",
			config: config.StdioDiscoveryConfig{
				Services: []config.StdioServiceConfig{
					{
						Name:      "service1",
						Namespace: "ns1",
						Command:   []string{"echo", "service1"},
					},
					{
						Name:      "service2",
						Namespace: "ns2",
						Command:   []string{"echo", "service2"},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "empty services list",
			config: config.StdioDiscoveryConfig{
				Services: []config.StdioServiceConfig{},
			},
			wantErr: false,
		},
	}
}

func TestStdioDiscovery_ConfigDefaults(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)

	config := config.StdioDiscoveryConfig{
		Services: []config.StdioServiceConfig{
			{
				Name:    "test-service",
				Command: []string{"echo", "test"},
				// Missing namespace, weight, and health check config
			},
		},
	}

	sd, err := CreateStdioServiceDiscovery(config, logger)
	require.NoError(t, err)
	require.NotNil(t, sd)

	stdioSD, ok := sd.(*StdioDiscovery)
	require.True(t, ok, "Expected StdioDiscovery type")

	// Verify defaults were applied
	service := stdioSD.services["test-service"]
	assert.Equal(t, "default", service.Namespace)
	assert.Equal(t, 1, service.Weight)
	assert.Equal(t, 30*time.Second, service.HealthCheck.Interval)
	assert.Equal(t, 5*time.Second, service.HealthCheck.Timeout)
}

func TestStdioDiscovery_StartStop(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)

	config := config.StdioDiscoveryConfig{
		Services: []config.StdioServiceConfig{
			{
				Name:      "test-service",
				Namespace: "default",
				Command:   []string{"echo", "test"},
			},
		},
	}

	sd, err := CreateStdioServiceDiscovery(config, logger)
	require.NoError(t, err)

	// Test Start
	err = sd.Start(context.Background())
	assert.NoError(t, err)

	// Test Stop
	// Stop should not error
	sd.Stop()
}

func TestStdioDiscovery_GetEndpoints(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)

	config := config.StdioDiscoveryConfig{
		Services: []config.StdioServiceConfig{
			{
				Name:      "service1",
				Namespace: "ns1",
				Command:   []string{"echo", "service1"},
				Weight:    10,
				Metadata:  map[string]string{"type": "test"},
			},
			{
				Name:      "service2",
				Namespace: "ns2",
				Command:   []string{"echo", "service2"},
				Weight:    20,
			},
			{
				Name:      "service3",
				Namespace: "ns1", // Same namespace as service1
				Command:   []string{"echo", "service3"},
			},
		},
	}

	sd, err := CreateStdioServiceDiscovery(config, logger)
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
	assert.Equal(t, "test", service1Endpoint.Metadata["type"])

	// Test getting endpoints for different namespace
	ns2Endpoints := sd.GetEndpoints("ns2")
	assert.Len(t, ns2Endpoints, 1) // only service2

	// Test getting endpoints for non-existent namespace
	nonExistentEndpoints := sd.GetEndpoints("nonexistent")
	assert.Empty(t, nonExistentEndpoints)
}

func TestStdioDiscovery_GetAllEndpoints(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)

	config := config.StdioDiscoveryConfig{
		Services: []config.StdioServiceConfig{
			{
				Name:      "service1",
				Namespace: "ns1",
				Command:   []string{"echo", "service1"},
			},
			{
				Name:      "service2",
				Namespace: "ns2",
				Command:   []string{"echo", "service2"},
			},
			{
				Name:      "service3",
				Namespace: "ns1",
				Command:   []string{"echo", "service3"},
			},
		},
	}

	sd, err := CreateStdioServiceDiscovery(config, logger)
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

func TestStdioDiscovery_ListNamespaces(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)

	config := config.StdioDiscoveryConfig{
		Services: []config.StdioServiceConfig{
			{
				Name:      "service1",
				Namespace: "production",
				Command:   []string{"echo", "service1"},
			},
			{
				Name:      "service2",
				Namespace: "staging",
				Command:   []string{"echo", "service2"},
			},
			{
				Name:      "service3",
				Namespace: "development",
				Command:   []string{"echo", "service3"},
			},
			{
				Name:      "service4",
				Namespace: "production", // Duplicate namespace
				Command:   []string{"echo", "service4"},
			},
		},
	}

	sd, err := CreateStdioServiceDiscovery(config, logger)
	require.NoError(t, err)

	namespaces := sd.ListNamespaces()

	// Should have 3 unique namespaces
	assert.Len(t, namespaces, 3)
	assert.Contains(t, namespaces, "production")
	assert.Contains(t, namespaces, "staging")
	assert.Contains(t, namespaces, "development")
}

func TestStdioDiscovery_EmptyConfig(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)

	config := config.StdioDiscoveryConfig{
		Services: []config.StdioServiceConfig{},
	}

	sd, err := CreateStdioServiceDiscovery(config, logger)
	require.NoError(t, err)

	// Test with empty configuration
	endpoints := sd.GetEndpoints("default")
	assert.Empty(t, endpoints)

	allEndpoints := sd.GetAllEndpoints()
	assert.Empty(t, allEndpoints)

	namespaces := sd.ListNamespaces()
	assert.Empty(t, namespaces)
}

func TestStdioDiscovery_ServiceWithFullConfig(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)

	config := config.StdioDiscoveryConfig{
		Services: []config.StdioServiceConfig{
			{
				Name:       "full-service",
				Namespace:  "custom",
				Command:    []string{"echo", "full-service"},
				WorkingDir: "/tmp",
				Env: map[string]string{
					"NODE_ENV": "test",
					"PORT":     "8080",
				},
				Weight: testTimeout,
				Metadata: map[string]string{
					"version": "1.0.0",
					"type":    "microservice",
				},
				HealthCheck: config.StdioHealthCheckConfig{
					Enabled:  true,
					Interval: 60 * time.Second,
					Timeout:  10 * time.Second,
				},
			},
		},
	}

	sd, err := CreateStdioServiceDiscovery(config, logger)
	require.NoError(t, err)

	endpoints := sd.GetEndpoints("custom")
	require.Len(t, endpoints, 1)

	endpoint := endpoints[0]
	assert.Equal(t, "full-service", endpoint.Service)
	assert.Equal(t, "custom", endpoint.Namespace)
	assert.Equal(t, testTimeout, endpoint.Weight)
	assert.Equal(t, "1.0.0", endpoint.Metadata["version"])
	assert.Equal(t, "microservice", endpoint.Metadata["type"])
	assert.True(t, endpoint.Healthy) // Should be healthy by default
}

func TestStdioDiscovery_CreateMetadata(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)

	config := config.StdioDiscoveryConfig{
		Services: []config.StdioServiceConfig{
			{
				Name:       "metadata-service",
				Namespace:  "default",
				Command:    []string{"node", "server.js"},
				WorkingDir: "/app",
				Env: map[string]string{
					"NODE_ENV": "production",
					"PORT":     "3000",
				},
				Metadata: map[string]string{
					"framework": "express",
					"language":  "javascript",
				},
			},
		},
	}

	sd, err := CreateStdioServiceDiscovery(config, logger)
	require.NoError(t, err)

	endpoints := sd.GetEndpoints("default")
	require.Len(t, endpoints, 1)

	endpoint := endpoints[0]

	// Verify that metadata includes both service-specific and auto-generated metadata
	assert.Equal(t, "express", endpoint.Metadata["framework"])
	assert.Equal(t, "javascript", endpoint.Metadata["language"])

	// Should have discovery-specific metadata
	assert.Equal(t, "stdio", endpoint.Metadata["protocol"])
	assert.Equal(t, "node server.js", endpoint.Metadata["command"])
	assert.Equal(t, "/app", endpoint.Metadata["working_dir"])
	assert.Equal(t, "false", endpoint.Metadata["health_check_enabled"])

	// Should have environment variables (filtered for non-sensitive ones)
	assert.Equal(t, "production", endpoint.Metadata["env_node_env"])
	assert.Equal(t, "3000", endpoint.Metadata["env_port"])
}
