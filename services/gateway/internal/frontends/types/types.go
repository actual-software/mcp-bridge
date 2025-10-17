package types

import (
	"context"
	"net/http"

	"github.com/actual-software/mcp-bridge/services/gateway/internal/auth"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/session"
	"github.com/actual-software/mcp-bridge/services/router/pkg/mcp"
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

	// GetHandler returns the HTTP handler for this frontend
	// This allows multiple frontends to share a single HTTP server
	GetHandler() http.Handler

	// GetAddress returns the host:port address this frontend listens on
	// Used to group frontends that share the same address
	GetAddress() string

	// SetServer injects the shared HTTP server into this frontend
	// Called by the server manager after creating the shared server
	SetServer(server *http.Server)
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
	Authenticate(r *http.Request) (*auth.Claims, error)
	ValidateToken(tokenString string) (*auth.Claims, error)
}

// SessionManager manages client sessions.
type SessionManager interface {
	CreateSession(claims *auth.Claims) (*session.Session, error)
	GetSession(id string) (*session.Session, error)
	UpdateSession(session *session.Session) error
	RemoveSession(id string) error
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
