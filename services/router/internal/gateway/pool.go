package gateway

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"github.com/actual-software/mcp-bridge/services/router/internal/config"
)

// LoadBalancingStrategy defines the strategy for selecting gateways.
type LoadBalancingStrategy string

const (
	RoundRobinStrategy       LoadBalancingStrategy = "round_robin"
	LeastConnectionsStrategy LoadBalancingStrategy = "least_connections"
	WeightedStrategy         LoadBalancingStrategy = "weighted"
	PriorityStrategy         LoadBalancingStrategy = "priority"
)

const (
	// HealthCheckWaitTimeout is the timeout for waiting for health checks to complete.
	HealthCheckWaitTimeout = 30 * time.Second
)

// EndpointHealth represents the health status of a gateway endpoint.
type EndpointHealth struct {
	IsHealthy            bool
	ConsecutiveFailures  int
	ConsecutiveSuccesses int
	LastChecked          time.Time
	LastError            error
}

// GatewayEndpointWrapper wraps a gateway client with metadata.
type GatewayEndpointWrapper struct {
	Client      GatewayClient
	Config      config.GatewayEndpoint
	Health      *EndpointHealth
	ActiveConns int64 // For least-connections strategy
	mu          sync.RWMutex
}

// GetActiveConnections returns the number of active connections (thread-safe).
func (g *GatewayEndpointWrapper) GetActiveConnections() int64 {
	return atomic.LoadInt64(&g.ActiveConns)
}

// IncrementConnections increments the active connection count.
func (g *GatewayEndpointWrapper) IncrementConnections() {
	atomic.AddInt64(&g.ActiveConns, 1)
}

// DecrementConnections decrements the active connection count.
func (g *GatewayEndpointWrapper) DecrementConnections() {
	atomic.AddInt64(&g.ActiveConns, -1)
}

// IsHealthy returns the current health status (thread-safe).
func (g *GatewayEndpointWrapper) IsHealthy() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()

	return g.Health.IsHealthy
}

// UpdateHealth updates the health status (thread-safe).
func (g *GatewayEndpointWrapper) UpdateHealth(healthy bool, err error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.Health.LastChecked = time.Now()
	g.Health.LastError = err

	if healthy {
		g.Health.ConsecutiveFailures = 0
		g.Health.ConsecutiveSuccesses++
		g.Health.IsHealthy = true
	} else {
		g.Health.ConsecutiveSuccesses = 0
		g.Health.ConsecutiveFailures++
		g.Health.IsHealthy = false
	}
}

// GatewayPool manages multiple gateway connections with load balancing.
type GatewayPool struct {
	endpoints        []*GatewayEndpointWrapper
	strategy         LoadBalancingStrategy
	config           config.LoadBalancerConfig
	serviceDiscovery config.ServiceDiscoveryConfig
	logger           *zap.Logger

	// Load balancing state.
	roundRobinIndex uint64
	mu              sync.RWMutex

	// Health checking.
	healthCheckTicker *time.Ticker
	ctx               context.Context
	cancel            context.CancelFunc
	wg                sync.WaitGroup
}

// NewGatewayPool creates a new gateway pool with the given configuration.
func NewGatewayPool(_ context.Context, cfg *config.Config, logger *zap.Logger) (*GatewayPool, error) {
	endpoints := cfg.GetGatewayEndpoints()
	if len(endpoints) == 0 {
		return nil, errors.New("no gateway endpoints configured")
	}

	lbConfig := cfg.GetLoadBalancerConfig()
	sdConfig := cfg.GetServiceDiscoveryConfig()

	// Create new root context for pool lifecycle - not using passed context
	ctx, cancel := context.WithCancel(context.Background())

	//nolint:contextcheck // Using new root context for pool lifecycle management
	pool := createGatewayPoolStruct(endpoints, lbConfig, sdConfig, logger, ctx, cancel)

	// Initialize endpoint wrappers
	cbConfig := cfg.GetCircuitBreakerConfig()
	pool.endpoints = createEndpointWrappers(endpoints, cbConfig, logger)

	if len(pool.endpoints) == 0 {
		cancel()

		return nil, errors.New("failed to create any gateway clients")
	}

	// Start health checking if enabled
	if sdConfig.Enabled {
		pool.startHealthChecking()
	}

	logPoolInitialization(logger, pool, sdConfig)

	return pool, nil
}

func createGatewayPoolStruct(
	endpoints []config.GatewayEndpoint,
	lbConfig config.LoadBalancerConfig,
	sdConfig config.ServiceDiscoveryConfig,
	logger *zap.Logger,
	ctx context.Context,
	cancel context.CancelFunc,
) *GatewayPool {
	return &GatewayPool{
		endpoints:        make([]*GatewayEndpointWrapper, 0, len(endpoints)),
		strategy:         LoadBalancingStrategy(lbConfig.Strategy),
		config:           lbConfig,
		serviceDiscovery: sdConfig,
		logger:           logger,
		ctx:              ctx,
		cancel:           cancel,
	}
}

func createEndpointWrappers(
	endpoints []config.GatewayEndpoint,
	cbConfig config.CircuitBreakerConfig,
	logger *zap.Logger,
) []*GatewayEndpointWrapper {
	var wrappers []*GatewayEndpointWrapper

	for _, endpointConfig := range endpoints {
		wrapper := createSingleEndpointWrapper(endpointConfig, cbConfig, logger)
		if wrapper != nil {
			wrappers = append(wrappers, wrapper)
		}
	}

	return wrappers
}

func createSingleEndpointWrapper(
	endpointConfig config.GatewayEndpoint,
	cbConfig config.CircuitBreakerConfig,
	logger *zap.Logger,
) *GatewayEndpointWrapper {
	// Create gateway config from endpoint config
	gatewayConfig := config.GatewayConfig{
		URL:        endpointConfig.URL,
		Auth:       endpointConfig.Auth,
		Connection: endpointConfig.Connection,
		TLS:        endpointConfig.TLS,
	}

	client, err := NewGatewayClient(gatewayConfig, logger)
	if err != nil {
		logger.Error("Failed to create gateway client",
			zap.String("url", endpointConfig.URL),
			zap.Error(err))

		return nil
	}

	// Wrap with circuit breaker if enabled
	finalClient := wrapWithCircuitBreakerIfEnabled(client, endpointConfig, cbConfig, logger)

	return &GatewayEndpointWrapper{
		Client: finalClient,
		Config: endpointConfig,
		Health: &EndpointHealth{
			IsHealthy:   true, // Start optimistically
			LastChecked: time.Now(),
		},
	}
}

//nolint:ireturn // Factory pattern requires interface return
func wrapWithCircuitBreakerIfEnabled(
	client GatewayClient,
	endpointConfig config.GatewayEndpoint,
	cbConfig config.CircuitBreakerConfig,
	logger *zap.Logger,
) GatewayClient {
	if !cbConfig.Enabled {
		return client
	}

	circuitBreakerConfig := CircuitBreakerConfig{
		FailureThreshold: cbConfig.FailureThreshold,
		RecoveryTimeout:  cbConfig.RecoveryTimeout,
		SuccessThreshold: cbConfig.SuccessThreshold,
		TimeoutDuration:  cbConfig.TimeoutDuration,
		MonitoringWindow: cbConfig.MonitoringWindow,
	}

	logger.Info("Circuit breaker enabled for endpoint",
		zap.String("url", endpointConfig.URL),
		zap.Int("failure_threshold", cbConfig.FailureThreshold))

	return NewCircuitBreaker(client, circuitBreakerConfig, logger.With(zap.String("endpoint", endpointConfig.URL)))
}

func logPoolInitialization(logger *zap.Logger, pool *GatewayPool, sdConfig config.ServiceDiscoveryConfig) {
	logger.Info("Gateway pool initialized",
		zap.Int("endpoints", len(pool.endpoints)),
		zap.String("strategy", string(pool.strategy)),
		zap.Bool("health_checks", sdConfig.Enabled))
}

// SelectEndpoint selects the best available endpoint based on the load balancing strategy.
func (p *GatewayPool) SelectEndpoint() (*GatewayEndpointWrapper, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	healthyEndpoints := make([]*GatewayEndpointWrapper, 0, len(p.endpoints))

	for _, endpoint := range p.endpoints {
		if endpoint.IsHealthy() {
			healthyEndpoints = append(healthyEndpoints, endpoint)
		}
	}

	if len(healthyEndpoints) == 0 {
		return nil, errors.New("no healthy gateway endpoints available")
	}

	switch p.strategy {
	case RoundRobinStrategy:
		return p.selectRoundRobin(healthyEndpoints), nil
	case LeastConnectionsStrategy:
		return p.selectLeastConnections(healthyEndpoints), nil
	case WeightedStrategy:
		return p.selectWeighted(healthyEndpoints), nil
	case PriorityStrategy:
		return p.selectPriority(healthyEndpoints), nil
	default:
		return p.selectRoundRobin(healthyEndpoints), nil
	}
}

// This is used when we want to retry connections after failures.
func (p *GatewayPool) SelectEndpointForRetry() (*GatewayEndpointWrapper, error) {
	selector := CreateEndpointSelector(p)

	return selector.SelectForRetry()
}

// selectRoundRobin implements round-robin load balancing.
func (p *GatewayPool) selectRoundRobin(endpoints []*GatewayEndpointWrapper) *GatewayEndpointWrapper {
	index := atomic.AddUint64(&p.roundRobinIndex, 1) - 1

	return endpoints[index%uint64(len(endpoints))]
}

// selectLeastConnections implements least-connections load balancing.
func (p *GatewayPool) selectLeastConnections(endpoints []*GatewayEndpointWrapper) *GatewayEndpointWrapper {
	if len(endpoints) == 1 {
		return endpoints[0]
	}

	selected := endpoints[0]
	minConnections := selected.GetActiveConnections()

	for _, endpoint := range endpoints[1:] {
		connections := endpoint.GetActiveConnections()
		if connections < minConnections {
			selected = endpoint
			minConnections = connections
		}
	}

	return selected
}

// selectWeighted implements weighted load balancing.
func (p *GatewayPool) selectWeighted(endpoints []*GatewayEndpointWrapper) *GatewayEndpointWrapper {
	totalWeight := 0
	for _, endpoint := range endpoints {
		totalWeight += endpoint.Config.Weight
	}

	if totalWeight == 0 {
		// Fallback to round-robin if no weights configured.
		return p.selectRoundRobin(endpoints)
	}

	// Generate random number within total weight.
	target := rand.Intn(totalWeight) // #nosec G404 - math/rand is acceptable for load balancing
	currentWeight := 0

	for _, endpoint := range endpoints {
		currentWeight += endpoint.Config.Weight
		if currentWeight > target {
			return endpoint
		}
	}

	// Fallback (shouldn't happen).
	return endpoints[len(endpoints)-1]
}

// selectPriority implements priority-based load balancing.
func (p *GatewayPool) selectPriority(endpoints []*GatewayEndpointWrapper) *GatewayEndpointWrapper {
	// Sort by priority (lower number = higher priority).
	maxPriority := endpoints[0].Config.Priority

	var highestPriorityEndpoints []*GatewayEndpointWrapper

	for _, endpoint := range endpoints {
		if endpoint.Config.Priority < maxPriority {
			maxPriority = endpoint.Config.Priority
			highestPriorityEndpoints = []*GatewayEndpointWrapper{endpoint}
		} else if endpoint.Config.Priority == maxPriority {
			highestPriorityEndpoints = append(highestPriorityEndpoints, endpoint)
		}
	}

	// If multiple endpoints have the same priority, use round-robin among them.
	return p.selectRoundRobin(highestPriorityEndpoints)
}

// GetEndpointByTags returns endpoints that match all specified tags.
func (p *GatewayPool) GetEndpointByTags(tags []string) ([]*GatewayEndpointWrapper, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var matchingEndpoints []*GatewayEndpointWrapper

	for _, endpoint := range p.endpoints {
		if endpoint.IsHealthy() && p.hasAllTags(endpoint.Config.Tags, tags) {
			matchingEndpoints = append(matchingEndpoints, endpoint)
		}
	}

	if len(matchingEndpoints) == 0 {
		return nil, fmt.Errorf("no healthy endpoints found with tags: %v", tags)
	}

	return matchingEndpoints, nil
}

// hasAllTags checks if the endpoint has all required tags.
func (p *GatewayPool) hasAllTags(endpointTags, requiredTags []string) bool {
	for _, required := range requiredTags {
		found := false

		for _, tag := range endpointTags {
			if tag == required {
				found = true

				break
			}
		}

		if !found {
			return false
		}
	}

	return true
}

// startHealthChecking starts the health checking goroutine.
func (p *GatewayPool) startHealthChecking() {
	p.healthCheckTicker = time.NewTicker(p.serviceDiscovery.HealthCheckInterval)
	p.wg.Add(1)

	go func() {
		defer p.wg.Done()
		defer p.healthCheckTicker.Stop()

		p.logger.Info("Starting health checks",
			zap.Duration("interval", p.serviceDiscovery.HealthCheckInterval))

		for {
			select {
			case <-p.ctx.Done():
				return
			case <-p.healthCheckTicker.C:
				p.performHealthChecks()
			}
		}
	}()
}

// performHealthChecks checks the health of all endpoints.
func (p *GatewayPool) performHealthChecks() {
	p.mu.RLock()
	endpoints := make([]*GatewayEndpointWrapper, len(p.endpoints))
	copy(endpoints, p.endpoints)
	p.mu.RUnlock()

	// Use a WaitGroup to track completion of health checks.
	var wg sync.WaitGroup
	for _, endpoint := range endpoints {
		wg.Add(1)

		go func(ep *GatewayEndpointWrapper) {
			defer wg.Done()

			p.checkEndpointHealth(ep)
		}(endpoint)
	}

	// Wait for all health checks to complete with timeout.
	done := make(chan struct{})

	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All health checks completed.
	case <-time.After(HealthCheckWaitTimeout):
		// Health checks taking too long, continue.
		p.logger.Warn("health checks did not complete within timeout")
	}
}

// checkEndpointHealth performs a health check on a single endpoint.
func (p *GatewayPool) checkEndpointHealth(endpoint *GatewayEndpointWrapper) {
	isHealthy := endpoint.Client.IsConnected()

	// Update health status.
	endpoint.UpdateHealth(isHealthy, nil)

	// Log health status changes.
	endpoint.mu.RLock()
	health := endpoint.Health
	endpoint.mu.RUnlock()

	// Determine if we should mark as unhealthy/healthy based on thresholds.
	if isHealthy && !health.IsHealthy && health.ConsecutiveSuccesses >= p.serviceDiscovery.HealthyThreshold {
		p.logger.Info("Endpoint marked healthy",
			zap.String("url", endpoint.Config.URL),
			zap.Int("consecutive_successes", health.ConsecutiveSuccesses))

		endpoint.Health.IsHealthy = true
	} else if !isHealthy && health.IsHealthy && health.ConsecutiveFailures >= p.serviceDiscovery.UnhealthyThreshold {
		p.logger.Warn("Endpoint marked unhealthy",
			zap.String("url", endpoint.Config.URL),
			zap.Int("consecutive_failures", health.ConsecutiveFailures))

		endpoint.Health.IsHealthy = false
	}
}

// GetStats returns statistics about the gateway pool.
func (p *GatewayPool) GetStats() map[string]interface{} {
	p.mu.RLock()
	defer p.mu.RUnlock()

	stats := map[string]interface{}{
		"total_endpoints":   len(p.endpoints),
		"healthy_endpoints": 0,
		"strategy":          string(p.strategy),
		"endpoints":         make([]map[string]interface{}, 0, len(p.endpoints)),
	}

	healthyCount := 0

	for _, endpoint := range p.endpoints {
		endpoint.mu.RLock()
		endpointStats := map[string]interface{}{
			"url":                   endpoint.Config.URL,
			"healthy":               endpoint.Health.IsHealthy,
			"active_connections":    endpoint.GetActiveConnections(),
			"weight":                endpoint.Config.Weight,
			"priority":              endpoint.Config.Priority,
			"tags":                  endpoint.Config.Tags,
			"last_checked":          endpoint.Health.LastChecked,
			"consecutive_failures":  endpoint.Health.ConsecutiveFailures,
			"consecutive_successes": endpoint.Health.ConsecutiveSuccesses,
		}
		endpoint.mu.RUnlock()

		if endpoint.IsHealthy() {
			healthyCount++
		}

		endpointsSlice, ok := stats["endpoints"].([]map[string]interface{})
		if !ok {
			endpointsSlice = make([]map[string]interface{}, 0)
		}

		stats["endpoints"] = append(endpointsSlice, endpointStats)
	}

	stats["healthy_endpoints"] = healthyCount

	return stats
}

// Close gracefully shuts down the gateway pool.
func (p *GatewayPool) Close() error {
	p.logger.Info("Shutting down gateway pool")

	// Cancel health checking.
	p.cancel()
	p.wg.Wait()

	// Close all gateway clients.
	p.mu.Lock()
	defer p.mu.Unlock()

	var errors []error

	for _, endpoint := range p.endpoints {
		if err := endpoint.Client.Close(); err != nil {
			errors = append(errors, fmt.Errorf("failed to close endpoint %s: %w", endpoint.Config.URL, err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("errors during shutdown: %v", errors)
	}

	return nil
}
