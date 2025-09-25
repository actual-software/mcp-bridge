package server

import (
	"context"

	"github.com/poiley/mcp-bridge/services/router/pkg/mcp"
)

// RouterInterface defines the router interface for testing.
type RouterInterface interface {
	RouteRequest(ctx context.Context, req *mcp.Request, targetNamespace string) (*mcp.Response, error)
}
