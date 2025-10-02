// Package loadbalancer provides load balancing algorithms for distributing requests across multiple endpoints.
package loadbalancer

import (
	"sync"
	"sync/atomic"

	"github.com/actual-software/mcp-bridge/services/gateway/internal/discovery"
)

// LoadBalancer defines the load balancing interface.
type LoadBalancer interface {
	Next() *discovery.Endpoint
	UpdateEndpoints(endpoints []*discovery.Endpoint)
}

// RoundRobin implements round-robin load balancing.
type RoundRobin struct {
	endpoints []*discovery.Endpoint
	current   uint64
	mu        sync.RWMutex
}

// NewRoundRobin creates a new round-robin load balancer.
func NewRoundRobin(endpoints []*discovery.Endpoint) *RoundRobin {
	return &RoundRobin{
		endpoints: endpoints,
	}
}

// Next returns the next healthy endpoint.
func (rr *RoundRobin) Next() *discovery.Endpoint {
	rr.mu.RLock()
	endpoints := rr.endpoints
	rr.mu.RUnlock()

	if len(endpoints) == 0 {
		return nil
	}

	// Find next healthy endpoint
	start := atomic.AddUint64(&rr.current, 1)
	for i := 0; i < len(endpoints); i++ {
		idx := (start + uint64(i)) % uint64(len(endpoints)) // #nosec G115 - i is bounded by endpoints length

		ep := endpoints[idx]
		if ep.Healthy {
			return ep
		}
	}

	return nil
}

// UpdateEndpoints updates the endpoint list.
func (rr *RoundRobin) UpdateEndpoints(endpoints []*discovery.Endpoint) {
	rr.mu.Lock()
	defer rr.mu.Unlock()

	rr.endpoints = endpoints
}

// LeastConnections implements least-connections load balancing.
type LeastConnections struct {
	endpoints   []*discovery.Endpoint
	connections map[string]int64
	mu          sync.RWMutex
}

// NewLeastConnections creates a new least-connections load balancer.
func NewLeastConnections(endpoints []*discovery.Endpoint) *LeastConnections {
	return &LeastConnections{
		endpoints:   endpoints,
		connections: make(map[string]int64),
	}
}

// Next returns the endpoint with the least connections.
func (lc *LeastConnections) Next() *discovery.Endpoint {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	if len(lc.endpoints) == 0 {
		return nil
	}

	var selected *discovery.Endpoint

	minConns := int64(^uint64(0) >> 1) // Max int64

	for _, ep := range lc.endpoints {
		if !ep.Healthy {
			continue
		}

		key := ep.Address
		conns := lc.connections[key]

		if conns < minConns {
			minConns = conns
			selected = ep
		}
	}

	if selected != nil {
		// Increment connection count
		lc.connections[selected.Address]++
	}

	return selected
}

// UpdateEndpoints updates the endpoint list.
func (lc *LeastConnections) UpdateEndpoints(endpoints []*discovery.Endpoint) {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	lc.endpoints = endpoints

	// Clean up old connections
	newMap := make(map[string]int64)

	for _, ep := range endpoints {
		if count, exists := lc.connections[ep.Address]; exists {
			newMap[ep.Address] = count
		}
	}

	lc.connections = newMap
}

// ReleaseConnection decrements the connection count for an endpoint.
func (lc *LeastConnections) ReleaseConnection(address string) {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	if count, exists := lc.connections[address]; exists && count > 0 {
		lc.connections[address]--
	}
}

// Weighted implements weighted load balancing.
type Weighted struct {
	endpoints   []*discovery.Endpoint
	weights     []int
	totalWeight int
	current     uint64
	mu          sync.RWMutex
}

// NewWeighted creates a new weighted load balancer.
func NewWeighted(endpoints []*discovery.Endpoint) *Weighted {
	w := &Weighted{
		endpoints: endpoints,
		weights:   make([]int, len(endpoints)),
	}
	w.calculateWeights()

	return w
}

// calculateWeights calculates the total weight.
func (w *Weighted) calculateWeights() {
	w.totalWeight = 0
	for i, ep := range w.endpoints {
		weight := ep.Weight
		if weight <= 0 {
			weight = 1
		}

		w.weights[i] = weight
		w.totalWeight += weight
	}
}

// Next returns the next endpoint based on weight.
func (w *Weighted) Next() *discovery.Endpoint {
	w.mu.RLock()
	defer w.mu.RUnlock()

	if len(w.endpoints) == 0 || w.totalWeight == 0 {
		return nil
	}

	// Simple weighted selection
	counter := atomic.AddUint64(&w.current, 1)
	position := int(counter % uint64(w.totalWeight)) // #nosec G115 - totalWeight is controlled and positive

	currentWeight := 0

	for i, ep := range w.endpoints {
		if !ep.Healthy {
			continue
		}

		currentWeight += w.weights[i]
		if position < currentWeight {
			return ep
		}
	}

	// Fallback to first healthy endpoint
	for _, ep := range w.endpoints {
		if ep.Healthy {
			return ep
		}
	}

	return nil
}

// UpdateEndpoints updates the endpoint list.
func (w *Weighted) UpdateEndpoints(endpoints []*discovery.Endpoint) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.endpoints = endpoints
	w.weights = make([]int, len(endpoints))
	w.calculateWeights()
}
