package gateway

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/poiley/mcp-bridge/pkg/common/config"
	routerConfig "github.com/poiley/mcp-bridge/services/router/internal/config"
	"github.com/poiley/mcp-bridge/services/router/internal/constants"
)

func TestNewGatewayPool(t *testing.T) {
	logger := zap.NewNop()
	tests := getGatewayPoolTests()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testGatewayPoolCreation(t, tt, logger)
		})
	}
}

type gatewayPoolTest struct {
	name        string
	config      *routerConfig.Config
	wantErr     bool
	wantErrMsg  string
	expectCount int
}

func getGatewayPoolTests() []gatewayPoolTest {
	return []gatewayPoolTest{
		{
			name: "valid single endpoint",
			config: &routerConfig.Config{
				GatewayPool: routerConfig.GatewayPoolConfig{
					Endpoints: []routerConfig.GatewayEndpoint{
						{
							URL:  fmt.Sprintf("ws://localhost:%d", constants.TestHTTPPort),
							Auth: config.AuthConfig{Type: "bearer", Token: "test-token"},
						},
					},
				},
			},
			expectCount: 1,
		},
		{
			name: "valid multiple endpoints",
			config: &routerConfig.Config{
				GatewayPool: routerConfig.GatewayPoolConfig{
					Endpoints: []routerConfig.GatewayEndpoint{
						{
							URL:  fmt.Sprintf("ws://localhost:%d", constants.TestHTTPPort),
							Auth: config.AuthConfig{Type: "bearer", Token: "token1"},
						},
						{
							URL:  fmt.Sprintf("ws://localhost:%d", constants.TestHTTPPort+1),
							Auth: config.AuthConfig{Type: "bearer", Token: "token2"},
						},
					},
				},
			},
			expectCount: 2,
		},
		{
			name: "no endpoints",
			config: &routerConfig.Config{
				GatewayPool: routerConfig.GatewayPoolConfig{
					Endpoints: []routerConfig.GatewayEndpoint{},
				},
			},
			wantErr:    true,
			wantErrMsg: "no gateway endpoints configured",
		},
	}
}

func testGatewayPoolCreation(t *testing.T, tt gatewayPoolTest, logger *zap.Logger) {
	t.Helper()
	ctx := context.Background()
	pool, err := NewGatewayPool(ctx, tt.config, logger)

	if tt.wantErr {
		verifyGatewayPoolError(t, err, tt.wantErrMsg)
		return
	}

	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
		return
	}

	verifyGatewayPool(t, pool, tt.expectCount)
	_ = pool.Close()
}

func verifyGatewayPoolError(t *testing.T, err error, wantErrMsg string) {
	t.Helper()
	if err == nil {
		t.Errorf("Expected error but got none")
	} else if wantErrMsg != "" && err.Error() != wantErrMsg {
		t.Errorf("Expected error '%s', got '%s'", wantErrMsg, err.Error())
	}
}

func verifyGatewayPool(t *testing.T, pool *GatewayPool, expectCount int) {
	t.Helper()
	if pool == nil {
		t.Error("Expected non-nil pool")
		return
	}

	if len(pool.endpoints) != expectCount {
		t.Errorf("Expected %d endpoints, got %d", expectCount, len(pool.endpoints))
	}
}

func TestGatewayPool_LoadBalancing(t *testing.T) {
	logger := zap.NewNop()
	config := createLoadBalancingConfig()
	pool := setupLoadBalancingPool(t, config, logger)
	defer cleanupPool(t, pool)

	tests := getLoadBalancingTests()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testLoadBalancingStrategy(t, pool, tt.strategy)
		})
	}
}

func createLoadBalancingConfig() *routerConfig.Config {
	return &routerConfig.Config{
		GatewayPool: routerConfig.GatewayPoolConfig{
			Endpoints: []routerConfig.GatewayEndpoint{
				{
					URL:      fmt.Sprintf("ws://localhost:%d", constants.TestHTTPPort),
					Auth:     config.AuthConfig{Type: "bearer", Token: "token1"},
					Weight:   1,
					Priority: 1,
					Tags:     []string{"primary"},
				},
				{
					URL:      "ws://localhost:constants.TestWebSocketPort",
					Auth:     config.AuthConfig{Type: "bearer", Token: "token2"},
					Weight:   2,
					Priority: 2,
					Tags:     []string{"secondary"},
				},
			},
			LoadBalancer: routerConfig.LoadBalancerConfig{
				Strategy: "round_robin",
			},
		},
	}
}

func setupLoadBalancingPool(t *testing.T, config *routerConfig.Config, logger *zap.Logger) *GatewayPool {
	t.Helper()
	ctx := context.Background()
	pool, err := NewGatewayPool(ctx, config, logger)
	if err != nil {
		t.Fatalf("Failed to create pool: %v", err)
	}
	return pool
}

func cleanupPool(t *testing.T, pool *GatewayPool) {
	t.Helper()
	if err := pool.Close(); err != nil {
		t.Logf("Failed to close pool: %v", err)
	}
}

func getLoadBalancingTests() []struct {
	name     string
	strategy LoadBalancingStrategy
} {
	return []struct {
		name     string
		strategy LoadBalancingStrategy
	}{
		{"round_robin", RoundRobinStrategy},
		{"least_connections", LeastConnectionsStrategy},
		{"weighted", WeightedStrategy},
		{"priority", PriorityStrategy},
	}
}

func testLoadBalancingStrategy(t *testing.T, pool *GatewayPool, strategy LoadBalancingStrategy) {
	t.Helper()
	pool.strategy = strategy

	var selectedURLs []string
	for i := 0; i < 4; i++ {
		endpoint, err := pool.SelectEndpoint()
		if err != nil {
			t.Errorf("SelectEndpoint failed: %v", err)
			continue
		}
		selectedURLs = append(selectedURLs, endpoint.Config.URL)
	}

	if len(selectedURLs) == 0 {
		t.Error("No endpoints selected")
	}
}

func TestGatewayPool_HealthChecking(t *testing.T) {
	logger := zap.NewNop()

	config := &routerConfig.Config{
		GatewayPool: routerConfig.GatewayPoolConfig{
			Endpoints: []routerConfig.GatewayEndpoint{
				{
					URL:  fmt.Sprintf("ws://localhost:%d", constants.TestHTTPPort),
					Auth: config.AuthConfig{Type: "bearer", Token: "token1"},
				},
			},
			ServiceDiscovery: routerConfig.ServiceDiscoveryConfig{
				Enabled:             true,
				HealthCheckInterval: testIterations * time.Millisecond,
				UnhealthyThreshold:  2,
				HealthyThreshold:    2,
			},
		},
	}

	ctx := context.Background()
	pool, err := NewGatewayPool(ctx, config, logger)
	if err != nil {
		t.Fatalf("Failed to create pool: %v", err)
	}

	defer func() {
		if err := pool.Close(); err != nil {
			t.Logf("Failed to close pool: %v", err)
		}
	}()

	// Wait a bit for health checks to run.
	time.Sleep(httpStatusOK * time.Millisecond)

	stats := pool.GetStats()
	if stats == nil {
		t.Error("Expected stats to be non-nil")
	}

	totalEndpoints, ok := stats["total_endpoints"].(int)
	if !ok || totalEndpoints != 1 {
		t.Errorf("Expected 1 total endpoint, got %v", stats["total_endpoints"])
	}
}

func TestGatewayPool_GetEndpointByTags(t *testing.T) {
	logger := zap.NewNop()
	config := createTaggedEndpointsConfig()
	pool := setupTaggedPool(t, config, logger)
	defer cleanupPool(t, pool)

	tests := getTagTests()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testTagSelection(t, pool, tt)
		})
	}
}

func createTaggedEndpointsConfig() *routerConfig.Config {
	return &routerConfig.Config{
		GatewayPool: routerConfig.GatewayPoolConfig{
			Endpoints: []routerConfig.GatewayEndpoint{
				{
					URL:  fmt.Sprintf("ws://localhost:%d", constants.TestHTTPPort),
					Auth: config.AuthConfig{Type: "bearer", Token: "token1"},
					Tags: []string{"docker", "containers"},
				},
				{
					URL:  "ws://localhost:constants.TestWebSocketPort",
					Auth: config.AuthConfig{Type: "bearer", Token: "token2"},
					Tags: []string{"k8s", "kubernetes"},
				},
				{
					URL:  "ws://localhost:8082",
					Auth: config.AuthConfig{Type: "bearer", Token: "token3"},
					Tags: []string{"tools", "general"},
				},
			},
		},
	}
}

func setupTaggedPool(t *testing.T, config *routerConfig.Config, logger *zap.Logger) *GatewayPool {
	t.Helper()
	ctx := context.Background()
	pool, err := NewGatewayPool(ctx, config, logger)
	if err != nil {
		t.Fatalf("Failed to create pool: %v", err)
	}
	return pool
}

type tagTest struct {
	name        string
	tags        []string
	expectCount int
	expectErr   bool
}

func getTagTests() []tagTest {
	return []tagTest{
		{
			name:        "single matching tag",
			tags:        []string{"docker"},
			expectCount: 1,
		},
		{
			name:        "multiple matching tags",
			tags:        []string{"docker", "containers"},
			expectCount: 1,
		},
		{
			name:      "no matching tags",
			tags:      []string{"nonexistent"},
			expectErr: true,
		},
		{
			name:      "partial match",
			tags:      []string{"docker", "nonexistent"},
			expectErr: true,
		},
	}
}

func testTagSelection(t *testing.T, pool *GatewayPool, tt tagTest) {
	t.Helper()
	endpoints, err := pool.GetEndpointByTags(tt.tags)

	if tt.expectErr {
		if err == nil {
			t.Error("Expected error but got none")
		}
		return
	}

	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
		return
	}

	if len(endpoints) != tt.expectCount {
		t.Errorf("Expected %d endpoints, got %d", tt.expectCount, len(endpoints))
	}
}

func TestGatewayPool_ConcurrentAccess(t *testing.T) {
	logger := zap.NewNop()

	config := &routerConfig.Config{
		GatewayPool: routerConfig.GatewayPoolConfig{
			Endpoints: []routerConfig.GatewayEndpoint{
				{
					URL:  fmt.Sprintf("ws://localhost:%d", constants.TestHTTPPort),
					Auth: config.AuthConfig{Type: "bearer", Token: "token1"},
				},
				{
					URL:  "ws://localhost:constants.TestWebSocketPort",
					Auth: config.AuthConfig{Type: "bearer", Token: "token2"},
				},
			},
		},
	}

	ctx := context.Background()
	pool, err := NewGatewayPool(ctx, config, logger)
	if err != nil {
		t.Fatalf("Failed to create pool: %v", err)
	}

	defer func() {
		if err := pool.Close(); err != nil {
			t.Logf("Failed to close pool: %v", err)
		}
	}()

	// Test concurrent endpoint selection.
	const numGoroutines = 10

	const selectionsPerGoroutine = testIterations

	var wg sync.WaitGroup

	errors := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for j := 0; j < selectionsPerGoroutine; j++ {
				_, err := pool.SelectEndpoint()
				if err != nil {
					errors <- err

					return
				}
			}
		}()
	}

	wg.Wait()
	close(errors)

	// Check for any errors.
	for err := range errors {
		t.Errorf("Concurrent access error: %v", err)
	}
}

func TestGatewayEndpointWrapper_ConnectionTracking(t *testing.T) {
	wrapper := &GatewayEndpointWrapper{
		Health: &EndpointHealth{
			IsHealthy: true,
		},
	}

	// Test initial state.
	if wrapper.GetActiveConnections() != 0 {
		t.Errorf("Expected 0 active connections, got %d", wrapper.GetActiveConnections())
	}

	// Test increment.
	wrapper.IncrementConnections()

	if wrapper.GetActiveConnections() != 1 {
		t.Errorf("Expected 1 active connection, got %d", wrapper.GetActiveConnections())
	}

	// Test multiple increments.
	wrapper.IncrementConnections()
	wrapper.IncrementConnections()

	if wrapper.GetActiveConnections() != 3 {
		t.Errorf("Expected 3 active connections, got %d", wrapper.GetActiveConnections())
	}

	// Test decrement.
	wrapper.DecrementConnections()

	if wrapper.GetActiveConnections() != 2 {
		t.Errorf("Expected 2 active connections, got %d", wrapper.GetActiveConnections())
	}

	// Test health status.
	if !wrapper.IsHealthy() {
		t.Error("Expected endpoint to be healthy")
	}

	// Test health update.
	wrapper.UpdateHealth(false, nil)

	if wrapper.IsHealthy() {
		t.Error("Expected endpoint to be unhealthy after update")
	}
}
