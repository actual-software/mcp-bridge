package gateway

import (
	"errors"
	"math/rand"
	"sync/atomic"
)

// EndpointSelector handles endpoint selection for retry scenarios.
type EndpointSelector struct {
	pool *GatewayPool
}

// CreateEndpointSelector creates a new endpoint selector.
func CreateEndpointSelector(pool *GatewayPool) *EndpointSelector {
	return &EndpointSelector{
		pool: pool,
	}
}

// SelectForRetry selects an endpoint for retry operations.
func (s *EndpointSelector) SelectForRetry() (*GatewayEndpointWrapper, error) {
	s.pool.mu.RLock()
	defer s.pool.mu.RUnlock()

	if err := s.validateEndpoints(); err != nil {
		return nil, err
	}

	// Try healthy endpoints first.
	if endpoint := s.selectFromHealthyEndpoints(); endpoint != nil {
		return endpoint, nil
	}

	// Fallback to all endpoints for retry scenarios.
	return s.selectFromAllEndpoints(), nil
}

func (s *EndpointSelector) validateEndpoints() error {
	if len(s.pool.endpoints) == 0 {
		return errors.New("no gateway endpoints configured")
	}

	return nil
}

func (s *EndpointSelector) selectFromHealthyEndpoints() *GatewayEndpointWrapper {
	healthyEndpoints := s.collectHealthyEndpoints()

	if len(healthyEndpoints) == 0 {
		return nil
	}

	return s.selectByStrategy(healthyEndpoints)
}

func (s *EndpointSelector) collectHealthyEndpoints() []*GatewayEndpointWrapper {
	healthyEndpoints := make([]*GatewayEndpointWrapper, 0, len(s.pool.endpoints))

	for _, endpoint := range s.pool.endpoints {
		if endpoint.IsHealthy() {
			healthyEndpoints = append(healthyEndpoints, endpoint)
		}
	}

	return healthyEndpoints
}

func (s *EndpointSelector) selectFromAllEndpoints() *GatewayEndpointWrapper {
	return s.selectByStrategy(s.pool.endpoints)
}

func (s *EndpointSelector) selectByStrategy(endpoints []*GatewayEndpointWrapper) *GatewayEndpointWrapper {
	switch s.pool.strategy {
	case RoundRobinStrategy:
		return s.applyRoundRobin(endpoints)
	case LeastConnectionsStrategy:
		return s.applyLeastConnections(endpoints)
	case WeightedStrategy:
		return s.applyWeighted(endpoints)
	case PriorityStrategy:
		return s.applyPriority(endpoints)
	default:
		return s.applyRoundRobin(endpoints)
	}
}

// Strategy implementations.

func (s *EndpointSelector) applyRoundRobin(endpoints []*GatewayEndpointWrapper) *GatewayEndpointWrapper {
	index := atomic.AddUint64(&s.pool.roundRobinIndex, 1) - 1

	return endpoints[index%uint64(len(endpoints))]
}

func (s *EndpointSelector) applyLeastConnections(endpoints []*GatewayEndpointWrapper) *GatewayEndpointWrapper {
	if len(endpoints) == 1 {
		return endpoints[0]
	}

	selected := endpoints[0]
	minConnections := selected.GetActiveConnections()

	for _, endpoint := range endpoints[1:] {
		if connections := endpoint.GetActiveConnections(); connections < minConnections {
			selected = endpoint
			minConnections = connections
		}
	}

	return selected
}

func (s *EndpointSelector) applyWeighted(endpoints []*GatewayEndpointWrapper) *GatewayEndpointWrapper {
	totalWeight := s.calculateTotalWeight(endpoints)

	if totalWeight == 0 {
		return s.applyRoundRobin(endpoints)
	}

	return s.selectByWeight(endpoints, totalWeight)
}

func (s *EndpointSelector) calculateTotalWeight(endpoints []*GatewayEndpointWrapper) int {
	totalWeight := 0
	for _, endpoint := range endpoints {
		totalWeight += endpoint.Config.Weight
	}

	return totalWeight
}

func (s *EndpointSelector) selectByWeight(
	endpoints []*GatewayEndpointWrapper,
	totalWeight int,
) *GatewayEndpointWrapper {
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

func (s *EndpointSelector) applyPriority(endpoints []*GatewayEndpointWrapper) *GatewayEndpointWrapper {
	highestPriorityEndpoints := s.findHighestPriorityEndpoints(endpoints)

	// If multiple endpoints have the same priority, use round-robin among them.
	if len(highestPriorityEndpoints) > 1 {
		return s.applyRoundRobin(highestPriorityEndpoints)
	}

	return highestPriorityEndpoints[0]
}

func (s *EndpointSelector) findHighestPriorityEndpoints(endpoints []*GatewayEndpointWrapper) []*GatewayEndpointWrapper {
	maxPriority := endpoints[0].Config.Priority
	highestPriorityEndpoints := []*GatewayEndpointWrapper{}

	for _, endpoint := range endpoints {
		if endpoint.Config.Priority < maxPriority {
			maxPriority = endpoint.Config.Priority
			highestPriorityEndpoints = []*GatewayEndpointWrapper{endpoint}
		} else if endpoint.Config.Priority == maxPriority {
			highestPriorityEndpoints = append(highestPriorityEndpoints, endpoint)
		}
	}

	return highestPriorityEndpoints
}
