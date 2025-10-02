// Package loadbalancer provides load balancing algorithms including cross-protocol hybrid load balancing.
package loadbalancer

import (
	"sync"
	"sync/atomic"

	"github.com/actual-software/mcp-bridge/services/gateway/internal/discovery"
)

// Strategy constants.
const (
	strategyLeastConnections = "least_connections"
	protocolHTTP             = "http"
	protocolStdio            = "stdio"
)

// HybridLoadBalancer implements cross-protocol load balancing.
// It can distribute requests across endpoints using different protocols (stdio, WebSocket, HTTP, SSE).
type HybridLoadBalancer struct {
	endpoints      []*discovery.Endpoint
	protocolGroups map[string][]*discovery.Endpoint // protocol -> endpoints
	protocolPref   []string                         // protocol preference order
	strategy       string                           // underlying strategy: round_robin, least_connections, weighted
	connections    map[string]int64                 // address -> connection count (for least_connections)
	current        uint64                           // counter for round_robin
	healthAware    bool                             // whether to consider health status
	mu             sync.RWMutex
}

// HybridConfig configures the hybrid load balancer.
type HybridConfig struct {
	Strategy            string   `yaml:"strategy"`              // round_robin, least_connections, weighted
	HealthAware         bool     `yaml:"health_aware"`          // consider health status
	ProtocolPreference  []string `yaml:"protocol_preference"`   // protocol order preference
	FallbackToUnhealthy bool     `yaml:"fallback_to_unhealthy"` // fallback to unhealthy if no healthy endpoints
}

// NewHybridLoadBalancer creates a new hybrid load balancer supporting cross-protocol load balancing.
func NewHybridLoadBalancer(endpoints []*discovery.Endpoint, config HybridConfig) *HybridLoadBalancer {
	h := &HybridLoadBalancer{
		endpoints:      endpoints,
		protocolGroups: make(map[string][]*discovery.Endpoint),
		protocolPref:   config.ProtocolPreference,
		strategy:       config.Strategy,
		connections:    make(map[string]int64),
		healthAware:    config.HealthAware,
	}

	// Set defaults
	if h.strategy == "" {
		h.strategy = "round_robin"
	}

	if len(h.protocolPref) == 0 {
		h.protocolPref = []string{protocolHTTP, "websocket", protocolStdio, "sse"} // Default preference
	}

	h.updateProtocolGroups()

	return h
}

// Next returns the next endpoint using cross-protocol load balancing.
func (h *HybridLoadBalancer) Next() *discovery.Endpoint {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if len(h.endpoints) == 0 {
		return nil
	}

	// Try to find endpoint from preferred protocols
	selected := h.selectFromPreferredProtocols()
	if selected != nil {
		return selected
	}

	// Fallback to any available endpoint if health checking is disabled
	return h.fallbackSelection()
}

// selectFromPreferredProtocols tries to select an endpoint from preferred protocols.
func (h *HybridLoadBalancer) selectFromPreferredProtocols() *discovery.Endpoint {
	for _, protocol := range h.protocolPref {
		selected := h.selectFromProtocol(protocol)
		if selected != nil {
			return selected
		}
	}

	return nil
}

// selectFromProtocol selects an endpoint from a specific protocol group.
func (h *HybridLoadBalancer) selectFromProtocol(protocol string) *discovery.Endpoint {
	endpoints := h.protocolGroups[protocol]
	if len(endpoints) == 0 {
		return nil
	}

	// Apply selection strategy
	selected := h.applyStrategy(endpoints)
	if selected == nil {
		return nil
	}

	// Check health if enabled
	if h.healthAware && !selected.Healthy {
		return nil
	}

	// Track connection for least_connections strategy
	h.trackConnection(selected)

	return selected
}

// applyStrategy applies the configured load balancing strategy.
func (h *HybridLoadBalancer) applyStrategy(endpoints []*discovery.Endpoint) *discovery.Endpoint {
	switch h.strategy {
	case strategyLeastConnections:
		return h.nextLeastConnections(endpoints)
	case "weighted":
		return h.nextWeighted(endpoints)
	default:
		return h.nextRoundRobin(endpoints)
	}
}

// trackConnection tracks connection count for least_connections strategy.
func (h *HybridLoadBalancer) trackConnection(endpoint *discovery.Endpoint) {
	if h.strategy == strategyLeastConnections {
		h.connections[endpoint.Address]++
	}
}

// fallbackSelection provides fallback endpoint selection.
func (h *HybridLoadBalancer) fallbackSelection() *discovery.Endpoint {
	if !h.healthAware {
		return h.nextRoundRobin(h.endpoints)
	}

	return nil
}

// nextRoundRobin implements round-robin within a protocol group.
func (h *HybridLoadBalancer) nextRoundRobin(endpoints []*discovery.Endpoint) *discovery.Endpoint {
	if len(endpoints) == 0 {
		return nil
	}

	start := atomic.AddUint64(&h.current, 1)
	for i := 0; i < len(endpoints); i++ {
		idx := (start + uint64(i)) % uint64(len(endpoints)) // #nosec G115 - i is bounded by endpoints length
		ep := endpoints[idx]

		if !h.healthAware || ep.Healthy {
			return ep
		}
	}

	return nil
}

// nextLeastConnections implements least-connections within a protocol group.
func (h *HybridLoadBalancer) nextLeastConnections(endpoints []*discovery.Endpoint) *discovery.Endpoint {
	if len(endpoints) == 0 {
		return nil
	}

	var candidates []*discovery.Endpoint

	minConns := int64(^uint64(0) >> 1) // Max int64

	// Find endpoints with minimum connections
	for _, ep := range endpoints {
		if h.healthAware && !ep.Healthy {
			continue
		}

		conns := h.connections[ep.Address]
		if conns < minConns {
			minConns = conns
			candidates = []*discovery.Endpoint{ep} // Reset candidates list
		} else if conns == minConns {
			candidates = append(candidates, ep) // Add to candidates
		}
	}

	if len(candidates) == 0 {
		return nil
	}

	// If multiple endpoints have same connection count, use round-robin among them
	if len(candidates) > 1 {
		idx := atomic.AddUint64(&h.current, 1) % uint64(len(candidates)) // #nosec G115 - len is controlled

		return candidates[idx]
	}

	return candidates[0]
}

// nextWeighted implements weighted selection within a protocol group.
func (h *HybridLoadBalancer) nextWeighted(endpoints []*discovery.Endpoint) *discovery.Endpoint {
	if len(endpoints) == 0 {
		return nil
	}

	// Get healthy endpoints and total weight
	healthyEndpoints, totalWeight := h.getHealthyWeightedEndpoints(endpoints)
	if len(healthyEndpoints) == 0 || totalWeight == 0 {
		return nil
	}

	// Select endpoint based on weight
	return h.selectWeightedEndpoint(healthyEndpoints, totalWeight)
}

// getHealthyWeightedEndpoints filters healthy endpoints and calculates total weight.
func (h *HybridLoadBalancer) getHealthyWeightedEndpoints(endpoints []*discovery.Endpoint) ([]*discovery.Endpoint, int) {
	totalWeight := 0
	healthyEndpoints := make([]*discovery.Endpoint, 0, len(endpoints))

	for _, ep := range endpoints {
		if h.healthAware && !ep.Healthy {
			continue
		}

		weight := h.getEndpointWeight(ep)
		totalWeight += weight

		healthyEndpoints = append(healthyEndpoints, ep)
	}

	return healthyEndpoints, totalWeight
}

// getEndpointWeight returns the effective weight for an endpoint.
func (h *HybridLoadBalancer) getEndpointWeight(ep *discovery.Endpoint) int {
	if ep.Weight <= 0 {
		return 1
	}

	return ep.Weight
}

// selectWeightedEndpoint selects an endpoint based on weighted distribution.
func (h *HybridLoadBalancer) selectWeightedEndpoint(
	endpoints []*discovery.Endpoint,
	totalWeight int,
) *discovery.Endpoint {
	counter := atomic.AddUint64(&h.current, 1)
	position := int(counter % uint64(totalWeight)) // #nosec G115 - totalWeight is controlled and positive
	currentWeight := 0

	for _, ep := range endpoints {
		weight := h.getEndpointWeight(ep)
		currentWeight += weight

		if position < currentWeight {
			return ep
		}
	}

	// Fallback to first endpoint (should not reach here normally)
	return endpoints[0]
}

// UpdateEndpoints updates the endpoint list and recalculates protocol groups.
func (h *HybridLoadBalancer) UpdateEndpoints(endpoints []*discovery.Endpoint) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.endpoints = endpoints
	h.updateProtocolGroups()

	// Clean up old connections for least_connections
	if h.strategy == strategyLeastConnections {
		newMap := make(map[string]int64)

		for _, ep := range endpoints {
			if count, exists := h.connections[ep.Address]; exists {
				newMap[ep.Address] = count
			}
		}

		h.connections = newMap
	}
}

// updateProtocolGroups groups endpoints by protocol.
func (h *HybridLoadBalancer) updateProtocolGroups() {
	h.protocolGroups = make(map[string][]*discovery.Endpoint)

	for _, ep := range h.endpoints {
		protocol := h.detectProtocol(ep)
		h.protocolGroups[protocol] = append(h.protocolGroups[protocol], ep)
	}
}

// detectProtocol detects the protocol from an endpoint.
func (h *HybridLoadBalancer) detectProtocol(ep *discovery.Endpoint) string {
	// Check metadata for explicit protocol
	if protocol, ok := ep.Metadata["protocol"]; ok {
		return protocol
	}

	// Infer from scheme
	switch ep.Scheme {
	case "ws", "wss":
		return "websocket"
	case protocolHTTP, "https":
		// Could be HTTP or SSE - check path or metadata
		if ep.Path == "/events" || ep.Metadata["sse"] == "true" {
			return "sse"
		}

		return protocolHTTP
	case protocolStdio:
		return protocolStdio
	default:
		// Try to infer from address format
		if ep.Address == "localhost" || ep.Port == 0 {
			return protocolStdio // Likely a command
		}

		return protocolHTTP // Default to HTTP
	}
}

// ReleaseConnection decrements the connection count for an endpoint (used by least_connections).
func (h *HybridLoadBalancer) ReleaseConnection(address string) {
	if h.strategy != "least_connections" {
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if count, exists := h.connections[address]; exists && count > 0 {
		h.connections[address]--
	}
}

// GetProtocolStats returns statistics about protocol distribution.
func (h *HybridLoadBalancer) GetProtocolStats() map[string]ProtocolStats {
	h.mu.RLock()
	defer h.mu.RUnlock()

	stats := make(map[string]ProtocolStats)

	for protocol, endpoints := range h.protocolGroups {
		healthy := 0
		unhealthy := 0
		totalWeight := 0

		for _, ep := range endpoints {
			if ep.Healthy {
				healthy++
			} else {
				unhealthy++
			}

			weight := ep.Weight
			if weight <= 0 {
				weight = 1
			}

			totalWeight += weight
		}

		stats[protocol] = ProtocolStats{
			Protocol:       protocol,
			TotalCount:     len(endpoints),
			HealthyCount:   healthy,
			UnhealthyCount: unhealthy,
			TotalWeight:    totalWeight,
			Preference:     h.getProtocolPreference(protocol),
		}
	}

	return stats
}

// getProtocolPreference returns the preference order for a protocol.
func (h *HybridLoadBalancer) getProtocolPreference(protocol string) int {
	for i, pref := range h.protocolPref {
		if pref == protocol {
			return i + 1 // 1-based preference
		}
	}

	return len(h.protocolPref) + 1 // Not in preference list
}

// ProtocolStats contains statistics for a protocol group.
type ProtocolStats struct {
	Protocol       string `json:"protocol"`
	TotalCount     int    `json:"total_count"`
	HealthyCount   int    `json:"healthy_count"`
	UnhealthyCount int    `json:"unhealthy_count"`
	TotalWeight    int    `json:"total_weight"`
	Preference     int    `json:"preference_order"`
}
