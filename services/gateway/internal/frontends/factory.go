package frontends

import (
	"context"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap"

	"github.com/actual-software/mcp-bridge/services/gateway/internal/auth"
	customerrors "github.com/actual-software/mcp-bridge/services/gateway/internal/errors"
	httpfrontend "github.com/actual-software/mcp-bridge/services/gateway/internal/frontends/http"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/frontends/sse"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/frontends/stdio"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/frontends/tcp"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/frontends/types"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/frontends/websocket"
	"github.com/actual-software/mcp-bridge/services/router/pkg/mcp"
)

// Wrapper types to implement the Frontend interface

type stdioFrontendWrapper struct {
	*stdio.Frontend
}

func (w *stdioFrontendWrapper) GetMetrics() FrontendMetrics {
	m := w.Frontend.GetMetrics()

	return FrontendMetrics{
		ActiveConnections: m.ActiveConnections,
		TotalConnections:  m.TotalConnections,
		RequestCount:      m.RequestCount,
		ErrorCount:        m.ErrorCount,
		IsRunning:         m.IsRunning,
	}
}

// Adapter types for stdio frontend interfaces

type stdioRouterAdapter struct {
	router RequestRouter
}

func (a *stdioRouterAdapter) RouteRequest(
	ctx context.Context,
	req *mcp.Request,
	targetNamespace string,
) (*mcp.Response, error) {
	return a.router.RouteRequest(ctx, req, targetNamespace)
}

type stdioAuthAdapter struct {
	auth AuthProvider
}

func (a *stdioAuthAdapter) Authenticate(ctx context.Context, credentials map[string]string) (bool, error) {
	// stdio authentication is not currently used in the same way
	// This is a placeholder that always returns true for now
	// In production, this would need proper implementation
	return true, nil
}

func (a *stdioAuthAdapter) GetUserInfo(
	ctx context.Context,
	credentials map[string]string,
) (map[string]interface{}, error) {
	// Return empty user info for now
	return map[string]interface{}{}, nil
}

type stdioSessionAdapter struct {
	sessions SessionManager
}

func (a *stdioSessionAdapter) CreateSession(ctx context.Context, clientID string) (string, error) {
	// Create a basic claims object for the session
	claims := &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject: clientID,
		},
	}
	sess, err := a.sessions.CreateSession(claims)
	if err != nil {
		return "", err
	}

	return sess.ID, nil
}

func (a *stdioSessionAdapter) ValidateSession(ctx context.Context, sessionID string) (bool, error) {
	sess, err := a.sessions.GetSession(sessionID)
	if err != nil {
		return false, err
	}

	return sess != nil, nil
}

func (a *stdioSessionAdapter) DestroySession(ctx context.Context, sessionID string) error {
	return a.sessions.RemoveSession(sessionID)
}

// DefaultFactory is the default frontend factory implementation.
type DefaultFactory struct {
	logger *zap.Logger
}

// CreateFrontendFactory creates a frontend factory for instantiating frontend implementations.
func CreateFrontendFactory(logger *zap.Logger) *DefaultFactory {
	return &DefaultFactory{
		logger: logger.With(zap.String("component", "frontend_factory")),
	}
}

// CreateFrontend is a convenience function that creates a frontend without needing a factory instance.
//
//nolint:ireturn // Factory pattern legitimately requires interface return for polymorphism
func CreateFrontend(
	name string,
	protocol string,
	config map[string]interface{},
	router types.RequestRouter,
	auth types.AuthProvider,
	sessions types.SessionManager,
	logger *zap.Logger,
) (types.Frontend, error) {
	factory := CreateFrontendFactory(logger)

	return factory.CreateFrontend(
		types.FrontendConfig{
			Name:     name,
			Protocol: protocol,
			Config:   config,
		},
		router,
		auth,
		sessions,
	)
}

// CreateFrontend creates a frontend instance based on the provided configuration.
//
//nolint:ireturn // Factory pattern requires interface return
func (f *DefaultFactory) CreateFrontend(
	config FrontendConfig,
	router RequestRouter,
	auth AuthProvider,
	sessions SessionManager,
) (Frontend, error) {
	switch config.Protocol {
	case "stdio":
		return f.createStdioFrontend(config, router, auth, sessions)
	case "websocket":
		return f.createWebSocketFrontend(config, router, auth, sessions)
	case "http":
		return f.createHTTPFrontend(config, router, auth, sessions)
	case "sse":
		return f.createSSEFrontend(config, router, auth, sessions)
	case "tcp_binary", "tcp":
		return f.createTCPBinaryFrontend(config, router, auth, sessions)
	default:
		return nil, customerrors.NewValidationError("unsupported frontend protocol: " + config.Protocol)
	}
}

// SupportedProtocols returns the list of supported frontend protocols.
func (f *DefaultFactory) SupportedProtocols() []string {
	return []string{"stdio", "websocket", "http", "sse", "tcp_binary"}
}

// createStdioFrontend creates a stdio frontend instance.
//
//nolint:ireturn // Factory pattern requires interface return
func (f *DefaultFactory) createStdioFrontend(
	config FrontendConfig,
	router RequestRouter,
	auth AuthProvider,
	sessions SessionManager,
) (Frontend, error) {
	stdioConfig := stdio.Config{}

	// Parse enabled flag
	if enabled, ok := config.Config["enabled"].(bool); ok {
		stdioConfig.Enabled = enabled
	}

	// Parse modes configuration
	f.parseStdioModes(config.Config, &stdioConfig)

	// Parse process management configuration
	f.parseStdioProcessManagement(config.Config, &stdioConfig)

	// Create adapters for stdio-specific interfaces
	routerAdapter := &stdioRouterAdapter{router: router}
	authAdapter := &stdioAuthAdapter{auth: auth}
	sessionAdapter := &stdioSessionAdapter{sessions: sessions}

	return &stdioFrontendWrapper{
		stdio.CreateStdioFrontend(config.Name, stdioConfig, routerAdapter, authAdapter, sessionAdapter, f.logger),
	}, nil
}

// parseStdioModes parses the modes configuration for stdio frontend.
func (f *DefaultFactory) parseStdioModes(configMap map[string]interface{}, stdioConfig *stdio.Config) {
	modes, ok := configMap["modes"].([]interface{})
	if !ok {
		return
	}

	stdioConfig.Modes = make([]stdio.ModeConfig, len(modes))
	for i, mode := range modes {
		stdioConfig.Modes[i] = f.parseStdioMode(mode)
	}
}

// parseStdioMode parses a single mode configuration.
func (f *DefaultFactory) parseStdioMode(mode interface{}) stdio.ModeConfig {
	modeConfig := stdio.ModeConfig{}

	modeMap, ok := mode.(map[string]interface{})
	if !ok {
		return modeConfig
	}

	if modeType, ok := modeMap["type"].(string); ok {
		modeConfig.Type = modeType
	}

	if path, ok := modeMap["path"].(string); ok {
		modeConfig.Path = path
	}

	if permissions, ok := modeMap["permissions"].(string); ok {
		modeConfig.Permissions = permissions
	}

	if enabled, ok := modeMap["enabled"].(bool); ok {
		modeConfig.Enabled = enabled
	}

	return modeConfig
}

// parseStdioProcessManagement parses process management configuration.
func (f *DefaultFactory) parseStdioProcessManagement(configMap map[string]interface{}, stdioConfig *stdio.Config) {
	processMgmt, ok := configMap["process_management"].(map[string]interface{})
	if !ok {
		return
	}

	if maxClients, ok := processMgmt["max_concurrent_clients"].(int); ok {
		stdioConfig.Process.MaxConcurrentClients = maxClients
	}

	if timeout, ok := processMgmt["client_timeout"].(string); ok {
		if d, err := time.ParseDuration(timeout); err == nil {
			stdioConfig.Process.ClientTimeout = d
		}
	}

	if authRequired, ok := processMgmt["auth_required"].(bool); ok {
		stdioConfig.Process.AuthRequired = authRequired
	}
}

// createWebSocketFrontend creates a WebSocket frontend instance.
//
//nolint:ireturn // Factory pattern requires interface return
func (f *DefaultFactory) createWebSocketFrontend(
	config FrontendConfig,
	router RequestRouter,
	auth AuthProvider,
	sessions SessionManager,
) (Frontend, error) {
	wsConfig := websocket.Config{}

	// Parse configuration from map
	if host, ok := config.Config["host"].(string); ok {
		wsConfig.Host = host
	}
	if port, ok := config.Config["port"].(int); ok {
		wsConfig.Port = port
	}
	if maxConn, ok := config.Config["max_connections"].(int); ok {
		wsConfig.MaxConnections = maxConn
	}
	if readTimeout, ok := config.Config["read_timeout"].(string); ok {
		if d, err := time.ParseDuration(readTimeout); err == nil {
			wsConfig.ReadTimeout = d
		}
	}
	if writeTimeout, ok := config.Config["write_timeout"].(string); ok {
		if d, err := time.ParseDuration(writeTimeout); err == nil {
			wsConfig.WriteTimeout = d
		}
	}
	if pingInterval, ok := config.Config["ping_interval"].(string); ok {
		if d, err := time.ParseDuration(pingInterval); err == nil {
			wsConfig.PingInterval = d
		}
	}

	// Parse TLS config
	if tlsConfig, ok := config.Config["tls"].(map[string]interface{}); ok {
		if enabled, ok := tlsConfig["enabled"].(bool); ok {
			wsConfig.TLS.Enabled = enabled
		}
		if certFile, ok := tlsConfig["cert_file"].(string); ok {
			wsConfig.TLS.CertFile = certFile
		}
		if keyFile, ok := tlsConfig["key_file"].(string); ok {
			wsConfig.TLS.KeyFile = keyFile
		}
	}

	// Parse allowed origins
	if origins, ok := config.Config["allowed_origins"].([]interface{}); ok {
		for _, origin := range origins {
			if originStr, ok := origin.(string); ok {
				wsConfig.AllowedOrigins = append(wsConfig.AllowedOrigins, originStr)
			}
		}
	}

	return websocket.CreateWebSocketFrontend(config.Name, wsConfig, router, auth, sessions, f.logger), nil
}

// createHTTPFrontend creates an HTTP frontend instance.
//
//nolint:ireturn // Factory pattern requires interface return
func (f *DefaultFactory) createHTTPFrontend(
	config FrontendConfig,
	router RequestRouter,
	auth AuthProvider,
	sessions SessionManager,
) (Frontend, error) {
	httpConfig := httpfrontend.Config{}

	// Parse configuration from map
	if host, ok := config.Config["host"].(string); ok {
		httpConfig.Host = host
	}
	if port, ok := config.Config["port"].(int); ok {
		httpConfig.Port = port
	}
	if requestPath, ok := config.Config["request_path"].(string); ok {
		httpConfig.RequestPath = requestPath
	}
	if maxSize, ok := config.Config["max_request_size"].(int); ok {
		httpConfig.MaxRequestSize = int64(maxSize)
	}
	if readTimeout, ok := config.Config["read_timeout"].(string); ok {
		if d, err := time.ParseDuration(readTimeout); err == nil {
			httpConfig.ReadTimeout = d
		}
	}
	if writeTimeout, ok := config.Config["write_timeout"].(string); ok {
		if d, err := time.ParseDuration(writeTimeout); err == nil {
			httpConfig.WriteTimeout = d
		}
	}

	// Parse TLS config
	if tlsConfig, ok := config.Config["tls"].(map[string]interface{}); ok {
		if enabled, ok := tlsConfig["enabled"].(bool); ok {
			httpConfig.TLS.Enabled = enabled
		}
		if certFile, ok := tlsConfig["cert_file"].(string); ok {
			httpConfig.TLS.CertFile = certFile
		}
		if keyFile, ok := tlsConfig["key_file"].(string); ok {
			httpConfig.TLS.KeyFile = keyFile
		}
	}

	return httpfrontend.CreateHTTPFrontend(config.Name, httpConfig, router, auth, sessions, f.logger), nil
}

// createSSEFrontend creates an SSE frontend instance.
//
//nolint:ireturn // Factory pattern requires interface return
func (f *DefaultFactory) createSSEFrontend(
	config FrontendConfig,
	router RequestRouter,
	auth AuthProvider,
	sessions SessionManager,
) (Frontend, error) {
	sseConfig := sse.Config{}

	// Parse configuration from map
	if host, ok := config.Config["host"].(string); ok {
		sseConfig.Host = host
	}
	if port, ok := config.Config["port"].(int); ok {
		sseConfig.Port = port
	}
	if streamEndpoint, ok := config.Config["stream_endpoint"].(string); ok {
		sseConfig.StreamEndpoint = streamEndpoint
	}
	if requestEndpoint, ok := config.Config["request_endpoint"].(string); ok {
		sseConfig.RequestEndpoint = requestEndpoint
	}
	if keepAlive, ok := config.Config["keep_alive"].(string); ok {
		if d, err := time.ParseDuration(keepAlive); err == nil {
			sseConfig.KeepAlive = d
		}
	}
	if maxConn, ok := config.Config["max_connections"].(int); ok {
		sseConfig.MaxConnections = maxConn
	}

	// Parse TLS config
	if tlsConfig, ok := config.Config["tls"].(map[string]interface{}); ok {
		if enabled, ok := tlsConfig["enabled"].(bool); ok {
			sseConfig.TLS.Enabled = enabled
		}
		if certFile, ok := tlsConfig["cert_file"].(string); ok {
			sseConfig.TLS.CertFile = certFile
		}
		if keyFile, ok := tlsConfig["key_file"].(string); ok {
			sseConfig.TLS.KeyFile = keyFile
		}
	}

	return sse.CreateSSEFrontend(config.Name, sseConfig, router, auth, sessions, f.logger), nil
}

// createTCPBinaryFrontend creates a TCP Binary frontend instance.
//
//nolint:ireturn // Factory pattern requires interface return
func (f *DefaultFactory) createTCPBinaryFrontend(
	config FrontendConfig,
	router RequestRouter,
	auth AuthProvider,
	sessions SessionManager,
) (Frontend, error) {
	tcpConfig := tcp.Config{}

	// Parse configuration from map
	if host, ok := config.Config["host"].(string); ok {
		tcpConfig.Host = host
	}
	if port, ok := config.Config["port"].(int); ok {
		tcpConfig.Port = port
	}
	if maxConn, ok := config.Config["max_connections"].(int); ok {
		tcpConfig.MaxConnections = maxConn
	}
	if readTimeout, ok := config.Config["read_timeout"].(string); ok {
		if d, err := time.ParseDuration(readTimeout); err == nil {
			tcpConfig.ReadTimeout = d
		}
	}
	if writeTimeout, ok := config.Config["write_timeout"].(string); ok {
		if d, err := time.ParseDuration(writeTimeout); err == nil {
			tcpConfig.WriteTimeout = d
		}
	}
	if healthPort, ok := config.Config["health_port"].(int); ok {
		tcpConfig.HealthPort = healthPort
	}

	// Parse TLS config
	if tlsConfig, ok := config.Config["tls"].(map[string]interface{}); ok {
		if enabled, ok := tlsConfig["enabled"].(bool); ok {
			tcpConfig.TLS.Enabled = enabled
		}
		if certFile, ok := tlsConfig["cert_file"].(string); ok {
			tcpConfig.TLS.CertFile = certFile
		}
		if keyFile, ok := tlsConfig["key_file"].(string); ok {
			tcpConfig.TLS.KeyFile = keyFile
		}
	}

	return tcp.CreateTCPFrontend(config.Name, tcpConfig, router, auth, sessions, f.logger), nil
}
