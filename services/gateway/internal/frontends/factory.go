package frontends

import (
	"time"

	"go.uber.org/zap"

	customerrors "github.com/actual-software/mcp-bridge/services/gateway/internal/errors"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/frontends/stdio"
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
	default:
		return nil, customerrors.NewValidationError("unsupported frontend protocol: " + config.Protocol)
	}
}

// SupportedProtocols returns the list of supported frontend protocols.
func (f *DefaultFactory) SupportedProtocols() []string {
	return []string{"stdio"}
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

	return &stdioFrontendWrapper{
		stdio.CreateStdioFrontend(config.Name, stdioConfig, router, auth, sessions, f.logger),
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
