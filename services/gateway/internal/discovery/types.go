// Package discovery provides service discovery types and interfaces.
package discovery

import (
	"context"
	"encoding/json"
	"sync/atomic"
)

// ServiceDiscovery interface for discovering MCP services.
type ServiceDiscovery interface {
	Start(ctx context.Context) error
	Stop()
	GetEndpoints(namespace string) []Endpoint
	GetAllEndpoints() map[string][]Endpoint
	ListNamespaces() []string
	// RegisterEndpointChangeCallback registers a callback to be invoked when endpoints change
	RegisterEndpointChangeCallback(callback func(namespace string))
}

// Endpoint represents an MCP server endpoint.
type Endpoint struct {
	Service   string            `json:"service"`
	Namespace string            `json:"namespace"`
	Address   string            `json:"address"`
	Port      int               `json:"port"`
	Scheme    string            `json:"scheme"` // URL scheme: http, https, ws, wss
	Path      string            `json:"path"`   // URL path: /mcp
	Weight    int               `json:"weight"`
	Metadata  map[string]string `json:"metadata"`
	Tools     []ToolInfo        `json:"tools"`
	healthy   atomic.Bool       `json:"-"` // Thread-safe health status
}

// IsHealthy returns the health status of the endpoint.
func (e *Endpoint) IsHealthy() bool {
	return e.healthy.Load()
}

// SetHealthy sets the health status of the endpoint.
func (e *Endpoint) SetHealthy(healthy bool) {
	e.healthy.Store(healthy)
}

// MarshalJSON implements custom JSON marshaling for Endpoint.
func (e *Endpoint) MarshalJSON() ([]byte, error) {
	type Alias Endpoint
	return json.Marshal(&struct {
		Healthy bool `json:"healthy"`
		*Alias
	}{
		Healthy: e.IsHealthy(),
		Alias:   (*Alias)(e),
	})
}

// UnmarshalJSON implements custom JSON unmarshaling for Endpoint.
func (e *Endpoint) UnmarshalJSON(data []byte) error {
	type Alias Endpoint
	aux := &struct {
		Healthy bool `json:"healthy"`
		*Alias
	}{
		Alias: (*Alias)(e),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	e.SetHealthy(aux.Healthy)
	return nil
}

// ToolInfo represents information about an MCP tool.
type ToolInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}
