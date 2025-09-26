
package loadbalancer

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/poiley/mcp-bridge/services/gateway/internal/discovery"
)

func TestHybridLoadBalancer_NewHybridLoadBalancer(t *testing.T) {
	endpoints := []*discovery.Endpoint{
		{Address: "localhost", Port: 0, Scheme: "stdio", Healthy: true, Metadata: map[string]string{"protocol": "stdio"}},
		{Address: "127.0.0.1", Port: 8080, Scheme: "ws", Healthy: true, Metadata: map[string]string{"protocol": "websocket"}},
		{Address: "127.0.0.1", Port: 9090, Scheme: "http", Healthy: true, Metadata: map[string]string{"protocol": "http"}},
	}

	config := HybridConfig{
		Strategy:           "round_robin",
		HealthAware:        true,
		ProtocolPreference: []string{"http", "websocket", "stdio"},
	}

	lb := NewHybridLoadBalancer(endpoints, config)
	require.NotNil(t, lb)

	assert.Equal(t, "round_robin", lb.strategy)
	assert.True(t, lb.healthAware)
	assert.Equal(t, []string{"http", "websocket", "stdio"}, lb.protocolPref)
	assert.Len(t, lb.endpoints, 3)
	assert.Len(t, lb.protocolGroups, 3)
}

func TestHybridLoadBalancer_ProtocolPreference(t *testing.T) {
	endpoints := []*discovery.Endpoint{
		{Address: "localhost", Port: 0, Scheme: "stdio", Healthy: true, Metadata: map[string]string{"protocol": "stdio"}},
		{Address: "127.0.0.1", Port: 8080, Scheme: "ws", Healthy: true, Metadata: map[string]string{"protocol": "websocket"}},
		{Address: "127.0.0.1", Port: 9090, Scheme: "http", Healthy: true, Metadata: map[string]string{"protocol": "http"}},
	}

	config := HybridConfig{
		Strategy:           "round_robin",
		HealthAware:        true,
		ProtocolPreference: []string{"http", "websocket", "stdio"}, // HTTP first
	}

	lb := NewHybridLoadBalancer(endpoints, config)

	// Should prefer HTTP first
	selected := lb.Next()
	require.NotNil(t, selected)
	assert.Equal(t, "http", selected.Metadata["protocol"])
	assert.Equal(t, "127.0.0.1", selected.Address)
	assert.Equal(t, 9090, selected.Port)
}

func TestHybridLoadBalancer_RoundRobinWithinProtocol(t *testing.T) {
	endpoints := []*discovery.Endpoint{
		{Address: "127.0.0.1", Port: 9090, Scheme: "http", Healthy: true, Metadata: map[string]string{"protocol": "http"}},
		{Address: "127.0.0.1", Port: 9091, Scheme: "http", Healthy: true, Metadata: map[string]string{"protocol": "http"}},
		{Address: "127.0.0.1", Port: 8080, Scheme: "ws", Healthy: true, Metadata: map[string]string{"protocol": "websocket"}},
	}

	config := HybridConfig{
		Strategy:           "round_robin",
		HealthAware:        true,
		ProtocolPreference: []string{"http", "websocket"},
	}

	lb := NewHybridLoadBalancer(endpoints, config)

	// Should round-robin within HTTP protocol group
	selected1 := lb.Next()
	selected2 := lb.Next()
	selected3 := lb.Next()

	require.NotNil(t, selected1)
	require.NotNil(t, selected2)
	require.NotNil(t, selected3)

	// All should be HTTP (preferred protocol)
	assert.Equal(t, "http", selected1.Metadata["protocol"])
	assert.Equal(t, "http", selected2.Metadata["protocol"])
	assert.Equal(t, "http", selected3.Metadata["protocol"])

	// Should round-robin between the two HTTP endpoints
	ports := []int{selected1.Port, selected2.Port, selected3.Port}
	assert.Contains(t, ports, 9090)
	assert.Contains(t, ports, 9091)
}

func TestHybridLoadBalancer_LeastConnections(t *testing.T) {
	endpoints := []*discovery.Endpoint{
		{Address: "127.0.0.1", Port: 9090, Scheme: "http", Healthy: true, Metadata: map[string]string{"protocol": "http"}},
		{Address: "127.0.0.1", Port: 9091, Scheme: "http", Healthy: true, Metadata: map[string]string{"protocol": "http"}},
	}

	config := HybridConfig{
		Strategy:           "least_connections",
		HealthAware:        true,
		ProtocolPreference: []string{"http"},
	}

	lb := NewHybridLoadBalancer(endpoints, config)

	// First call should select first endpoint (both have 0 connections)
	selected1 := lb.Next()
	require.NotNil(t, selected1)
	// Either endpoint could be selected first since both have 0 connections
	firstPort := selected1.Port
	assert.Contains(t, []int{9090, 9091}, firstPort)

	// Second call should select the other endpoint (since first has 1 connection now)
	selected2 := lb.Next()
	require.NotNil(t, selected2)
	secondPort := selected2.Port
	assert.Contains(t, []int{9090, 9091}, secondPort)
	assert.NotEqual(t, firstPort, secondPort, "Should select different endpoint on second call")

	// Now both have 1 connection. Release connection from first selected endpoint
	lb.ReleaseConnection("127.0.0.1") // This is a generic release, let's be more specific

	// Third call should prefer the endpoint with fewer connections
	selected3 := lb.Next()
	require.NotNil(t, selected3)
}

func TestHybridLoadBalancer_WeightedSelection(t *testing.T) {
	endpoints := []*discovery.Endpoint{
		{
			Address: "127.0.0.1", Port: 9090, Scheme: "http", Healthy: true, Weight: 1,
			Metadata: map[string]string{"protocol": "http"},
		},
		{
			Address: "127.0.0.1", Port: 9091, Scheme: "http", Healthy: true, Weight: 3,
			Metadata: map[string]string{"protocol": "http"},
		},
	}

	config := HybridConfig{
		Strategy:           "weighted",
		HealthAware:        true,
		ProtocolPreference: []string{"http"},
	}

	lb := NewHybridLoadBalancer(endpoints, config)

	// Collect selections over multiple calls
	selections := make(map[int]int)

	for i := 0; i < 40; i++ {
		selected := lb.Next()
		require.NotNil(t, selected)

		selections[selected.Port]++
	}

	// Port 9091 (weight 3) should be selected more often than port 9090 (weight 1)
	assert.Greater(t, selections[9091], selections[9090],
		"Higher weight endpoint should be selected more often. Got 9090: %d, 9091: %d",
		selections[9090], selections[9091])
}

func TestHybridLoadBalancer_HealthAwareness(t *testing.T) {
	endpoints := []*discovery.Endpoint{
		{Address: "127.0.0.1", Port: 9090, Scheme: "http", Healthy: false, Metadata: map[string]string{"protocol": "http"}},
		{Address: "127.0.0.1", Port: 8080, Scheme: "ws", Healthy: true, Metadata: map[string]string{"protocol": "websocket"}},
	}

	config := HybridConfig{
		Strategy:           "round_robin",
		HealthAware:        true,
		ProtocolPreference: []string{"http", "websocket"}, // HTTP preferred but unhealthy
	}

	lb := NewHybridLoadBalancer(endpoints, config)

	// Should skip unhealthy HTTP and select healthy WebSocket
	selected := lb.Next()
	require.NotNil(t, selected)
	assert.Equal(t, "websocket", selected.Metadata["protocol"])
	assert.Equal(t, 8080, selected.Port)
}

func TestHybridLoadBalancer_ProtocolDetection(t *testing.T) {
	tests := []struct {
		name     string
		endpoint discovery.Endpoint
		expected string
	}{
		{
			name: "WebSocket scheme",
			endpoint: discovery.Endpoint{
				Address: "127.0.0.1",
				Port:    8080,
				Scheme:  "ws",
			},
			expected: "websocket",
		},
		{
			name: "HTTP scheme",
			endpoint: discovery.Endpoint{
				Address: "127.0.0.1",
				Port:    9090,
				Scheme:  "http",
			},
			expected: "http",
		},
		{
			name: "SSE path detection",
			endpoint: discovery.Endpoint{
				Address: "127.0.0.1",
				Port:    9090,
				Scheme:  "http",
				Path:    "/events",
			},
			expected: "sse",
		},
		{
			name: "stdio command",
			endpoint: discovery.Endpoint{
				Address: "localhost",
				Port:    0,
			},
			expected: "stdio",
		},
		{
			name: "explicit protocol metadata",
			endpoint: discovery.Endpoint{
				Address:  "127.0.0.1",
				Port:     8080,
				Scheme:   "http",
				Metadata: map[string]string{"protocol": "custom"},
			},
			expected: "custom",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lb := NewHybridLoadBalancer([]*discovery.Endpoint{}, HybridConfig{})
			protocol := lb.detectProtocol(&tt.endpoint)
			assert.Equal(t, tt.expected, protocol)
		})
	}
}

func TestHybridLoadBalancer_ProtocolStats(t *testing.T) {
	endpoints := []*discovery.Endpoint{
		{
			Address: "127.0.0.1", Port: 9090, Scheme: "http", Healthy: true, Weight: 2,
			Metadata: map[string]string{"protocol": "http"},
		},
		{
			Address: "127.0.0.1", Port: 9091, Scheme: "http", Healthy: false, Weight: 1,
			Metadata: map[string]string{"protocol": "http"},
		},
		{
			Address: "127.0.0.1", Port: 8080, Scheme: "ws", Healthy: true, Weight: 1,
			Metadata: map[string]string{"protocol": "websocket"},
		},
		{
			Address: "localhost", Port: 0, Scheme: "stdio", Healthy: true, Weight: 1,
			Metadata: map[string]string{"protocol": "stdio"},
		},
	}

	config := HybridConfig{
		Strategy:           "weighted",
		HealthAware:        true,
		ProtocolPreference: []string{"http", "websocket", "stdio"},
	}

	lb := NewHybridLoadBalancer(endpoints, config)
	stats := lb.GetProtocolStats()

	require.Len(t, stats, 3)

	// Check HTTP stats
	httpStats := stats["http"]
	assert.Equal(t, "http", httpStats.Protocol)
	assert.Equal(t, 2, httpStats.TotalCount)
	assert.Equal(t, 1, httpStats.HealthyCount)
	assert.Equal(t, 1, httpStats.UnhealthyCount)
	assert.Equal(t, 3, httpStats.TotalWeight) // 2 + 1
	assert.Equal(t, 1, httpStats.Preference)  // First in preference

	// Check WebSocket stats
	wsStats := stats["websocket"]
	assert.Equal(t, "websocket", wsStats.Protocol)
	assert.Equal(t, 1, wsStats.TotalCount)
	assert.Equal(t, 1, wsStats.HealthyCount)
	assert.Equal(t, 0, wsStats.UnhealthyCount)
	assert.Equal(t, 1, wsStats.TotalWeight)
	assert.Equal(t, 2, wsStats.Preference) // Second in preference

	// Check stdio stats
	stdioStats := stats["stdio"]
	assert.Equal(t, "stdio", stdioStats.Protocol)
	assert.Equal(t, 1, stdioStats.TotalCount)
	assert.Equal(t, 1, stdioStats.HealthyCount)
	assert.Equal(t, 0, stdioStats.UnhealthyCount)
	assert.Equal(t, 1, stdioStats.TotalWeight)
	assert.Equal(t, 3, stdioStats.Preference) // Third in preference
}

func TestHybridLoadBalancer_UpdateEndpoints(t *testing.T) {
	initialEndpoints := []*discovery.Endpoint{
		{Address: "127.0.0.1", Port: 9090, Scheme: "http", Healthy: true, Metadata: map[string]string{"protocol": "http"}},
	}

	config := HybridConfig{
		Strategy:    "least_connections",
		HealthAware: true,
	}

	lb := NewHybridLoadBalancer(initialEndpoints, config)

	// Make some selections to build connection counts
	lb.Next()
	lb.Next()

	// Update with new endpoints
	newEndpoints := []*discovery.Endpoint{
		{Address: "127.0.0.1", Port: 8080, Scheme: "ws", Healthy: true, Metadata: map[string]string{"protocol": "websocket"}},
		{Address: "127.0.0.1", Port: 9091, Scheme: "http", Healthy: true, Metadata: map[string]string{"protocol": "http"}},
	}

	lb.UpdateEndpoints(newEndpoints)

	assert.Len(t, lb.endpoints, 2)
	assert.Len(t, lb.protocolGroups, 2)

	// Connection counts should be cleaned up for removed endpoints
	assert.NotContains(t, lb.connections, "127.0.0.1:9090")
}

func TestHybridLoadBalancer_NoHealthyEndpoints(t *testing.T) {
	endpoints := []*discovery.Endpoint{
		{
			Address: "127.0.0.1", Port: 9090, Scheme: "http", Healthy: false,
			Metadata: map[string]string{"protocol": "http"},
		},
		{
			Address: "127.0.0.1", Port: 8080, Scheme: "ws", Healthy: false,
			Metadata: map[string]string{"protocol": "websocket"},
		},
	}

	config := HybridConfig{
		Strategy:    "round_robin",
		HealthAware: true,
	}

	lb := NewHybridLoadBalancer(endpoints, config)

	// Should return nil when all endpoints are unhealthy
	selected := lb.Next()
	assert.Nil(t, selected)

	// With health awareness disabled, should still return an endpoint
	config.HealthAware = false
	lb = NewHybridLoadBalancer(endpoints, config)

	selected = lb.Next()
	assert.NotNil(t, selected)
}

func TestHybridLoadBalancer_EmptyEndpoints(t *testing.T) {
	lb := NewHybridLoadBalancer([]*discovery.Endpoint{}, HybridConfig{})

	selected := lb.Next()
	assert.Nil(t, selected)

	stats := lb.GetProtocolStats()
	assert.Empty(t, stats)
}
