package frontends

import (
	"github.com/actual-software/mcp-bridge/services/gateway/internal/frontends/types"
)

// Re-export types from types package for backward compatibility

type Frontend = types.Frontend
type FrontendMetrics = types.FrontendMetrics
type RequestRouter = types.RequestRouter
type AuthProvider = types.AuthProvider
type SessionManager = types.SessionManager
type FrontendConfig = types.FrontendConfig
type Factory = types.Factory
