package backends

import (
	"context"
	"time"

	"github.com/poiley/mcp-bridge/services/router/pkg/mcp"
)

// Backend represents a backend MCP server connection.
type Backend interface {
	// Start initializes the backend connection
	Start(ctx context.Context) error

	// SendRequest sends an MCP request to the backend and returns the response
	SendRequest(ctx context.Context, req *mcp.Request) (*mcp.Response, error)

	// Health checks if the backend is healthy
	Health(ctx context.Context) error

	// Stop gracefully shuts down the backend connection
	Stop(ctx context.Context) error

	// GetName returns the backend name
	GetName() string

	// GetProtocol returns the backend protocol type
	GetProtocol() string

	// GetMetrics returns backend-specific metrics
	GetMetrics() BackendMetrics
}

// BackendMetrics contains performance and health metrics for a backend.
type BackendMetrics struct {
	RequestCount    uint64        `json:"request_count"`
	ErrorCount      uint64        `json:"error_count"`
	AverageLatency  time.Duration `json:"average_latency"`
	LastHealthCheck time.Time     `json:"last_health_check"`
	IsHealthy       bool          `json:"is_healthy"`
	ConnectionTime  time.Time     `json:"connection_time"`
}

// BackendConfig contains configuration for creating a backend.
type BackendConfig struct {
	Name     string                 `mapstructure:"name"`
	Protocol string                 `mapstructure:"protocol"`
	Config   map[string]interface{} `mapstructure:"config"`
}

// Factory creates Backend instances.
type Factory interface {
	CreateBackend(config BackendConfig) (Backend, error)
	SupportedProtocols() []string
}
