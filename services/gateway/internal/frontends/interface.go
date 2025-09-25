package frontends

import (
	"context"

	"github.com/poiley/mcp-bridge/services/router/pkg/mcp"
)

// Frontend represents a frontend protocol handler for accepting MCP client connections.
type Frontend interface {
	// Start initializes the frontend and begins accepting connections
	Start(ctx context.Context) error

	// Stop gracefully shuts down the frontend
	Stop(ctx context.Context) error

	// GetName returns the frontend name
	GetName() string

	// GetProtocol returns the frontend protocol type
	GetProtocol() string

	// GetMetrics returns frontend-specific metrics
	GetMetrics() FrontendMetrics
}

// FrontendMetrics contains performance and health metrics for a frontend.
type FrontendMetrics struct {
	ActiveConnections uint64 `json:"active_connections"`
	TotalConnections  uint64 `json:"total_connections"`
	RequestCount      uint64 `json:"request_count"`
	ErrorCount        uint64 `json:"error_count"`
	IsRunning         bool   `json:"is_running"`
}

// RequestRouter handles routing requests from frontends to backends.
type RequestRouter interface {
	RouteRequest(ctx context.Context, req *mcp.Request, targetNamespace string) (*mcp.Response, error)
}

// AuthProvider handles authentication for frontend connections.
type AuthProvider interface {
	Authenticate(ctx context.Context, credentials map[string]string) (bool, error)
	GetUserInfo(ctx context.Context, credentials map[string]string) (map[string]interface{}, error)
}

// SessionManager manages client sessions.
type SessionManager interface {
	CreateSession(ctx context.Context, clientID string) (string, error)
	ValidateSession(ctx context.Context, sessionID string) (bool, error)
	DestroySession(ctx context.Context, sessionID string) error
}

// FrontendConfig contains configuration for creating a frontend.
type FrontendConfig struct {
	Name     string                 `mapstructure:"name"`
	Protocol string                 `mapstructure:"protocol"`
	Config   map[string]interface{} `mapstructure:"config"`
}

// Factory creates Frontend instances.
type Factory interface {
	CreateFrontend(
		config FrontendConfig,
		router RequestRouter,
		auth AuthProvider,
		sessions SessionManager,
	) (Frontend, error)
	SupportedProtocols() []string
}
