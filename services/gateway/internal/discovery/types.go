// Package discovery provides service discovery types and interfaces.
package discovery

import "context"

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
	Healthy   bool              `json:"healthy"`
}

// ToolInfo represents information about an MCP tool.
type ToolInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}
