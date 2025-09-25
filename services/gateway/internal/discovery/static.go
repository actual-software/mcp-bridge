package discovery

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"sync"

	"go.uber.org/zap"

	"github.com/poiley/mcp-bridge/services/gateway/internal/config"
	customerrors "github.com/poiley/mcp-bridge/services/gateway/internal/errors"
)

// StaticDiscovery implements service discovery using static configuration.
type StaticDiscovery struct {
	config    config.ServiceDiscoveryConfig
	logger    *zap.Logger
	endpoints map[string][]Endpoint
	mu        sync.RWMutex
}

// CreateStaticServiceDiscovery creates a static (configuration-based) service discovery instance.
func CreateStaticServiceDiscovery(cfg config.ServiceDiscoveryConfig, logger *zap.Logger) (*StaticDiscovery, error) {
	if cfg.Static.Endpoints == nil {
		return nil, customerrors.NewInvalidServiceConfigError("static endpoints configuration is required", "static")
	}

	sd := &StaticDiscovery{
		config:    cfg,
		logger:    logger,
		endpoints: make(map[string][]Endpoint),
	}

	// Convert configuration to internal endpoints
	if err := sd.loadEndpoints(); err != nil {
		return nil, customerrors.Wrap(err, "failed to load static endpoints").
			WithComponent("discovery_static")
	}

	return sd, nil
}

// loadEndpoints converts configuration endpoints to internal endpoint format.
func (sd *StaticDiscovery) loadEndpoints() error {
	sd.mu.Lock()
	defer sd.mu.Unlock()

	for namespace, configs := range sd.config.Static.Endpoints {
		var endpoints []Endpoint

		for _, cfg := range configs {
			endpoint, err := sd.configToEndpoint(namespace, cfg)
			if err != nil {
				sd.logger.Error("Failed to parse endpoint configuration",
					zap.String("namespace", namespace),
					zap.String("url", cfg.URL),
					zap.Error(err))

				continue
			}

			endpoints = append(endpoints, endpoint)
		}

		if len(endpoints) > 0 {
			sd.endpoints[namespace] = endpoints
			sd.logger.Info("Loaded static endpoints",
				zap.String("namespace", namespace),
				zap.Int("count", len(endpoints)))
		}
	}

	return nil
}

// configToEndpoint converts a configuration endpoint to an internal Endpoint.
func (sd *StaticDiscovery) configToEndpoint(namespace string, cfg config.EndpointConfig) (Endpoint, error) {
	parsedURL, err := url.Parse(cfg.URL)
	if err != nil {
		return Endpoint{}, customerrors.NewInvalidEndpointError(cfg.URL, err.Error())
	}

	// Extract port from URL
	port := 80 // default

	if parsedURL.Port() != "" {
		if p, err := strconv.Atoi(parsedURL.Port()); err == nil {
			port = p
		}
	} else if parsedURL.Scheme == "https" {
		port = 443
	}

	// Convert labels to metadata
	metadata := make(map[string]string)

	for k, v := range cfg.Labels {
		if s, ok := v.(string); ok {
			metadata[k] = s
		} else {
			metadata[k] = fmt.Sprintf("%v", v)
		}
	}

	return Endpoint{
		Service:   namespace + "-service", // Generate service name
		Namespace: namespace,
		Address:   parsedURL.Hostname(),
		Port:      port,
		Scheme:    parsedURL.Scheme, // Store URL scheme (ws, wss, http, https)
		Path:      parsedURL.Path,   // Store URL path (/mcp)
		Weight:    DefaultWeight,    // Default weight
		Metadata:  metadata,
		Tools:     []ToolInfo{}, // Static discovery doesn't provide tool info
		Healthy:   true,         // Assume healthy for static endpoints
	}, nil
}

// Start implements ServiceDiscovery interface.
func (sd *StaticDiscovery) Start(_ context.Context) error {
	sd.logger.Info("Starting static service discovery")

	return nil // Static discovery doesn't need to start any background processes
}

// Stop implements ServiceDiscovery interface.
func (sd *StaticDiscovery) Stop() {
	sd.logger.Info("Stopping static service discovery")
}

// GetEndpoints implements ServiceDiscovery interface.
func (sd *StaticDiscovery) GetEndpoints(namespace string) []Endpoint {
	sd.mu.RLock()
	defer sd.mu.RUnlock()

	endpoints, exists := sd.endpoints[namespace]
	if !exists {
		return []Endpoint{}
	}

	// Return a copy to avoid concurrent access issues
	result := make([]Endpoint, len(endpoints))
	copy(result, endpoints)

	return result
}

// GetAllEndpoints implements ServiceDiscovery interface.
func (sd *StaticDiscovery) GetAllEndpoints() map[string][]Endpoint {
	sd.mu.RLock()
	defer sd.mu.RUnlock()

	// Return a deep copy to avoid concurrent access issues
	result := make(map[string][]Endpoint)

	for namespace, endpoints := range sd.endpoints {
		endpointsCopy := make([]Endpoint, len(endpoints))
		copy(endpointsCopy, endpoints)
		result[namespace] = endpointsCopy
	}

	return result
}

// ListNamespaces implements ServiceDiscovery interface.
func (sd *StaticDiscovery) ListNamespaces() []string {
	sd.mu.RLock()
	defer sd.mu.RUnlock()

	namespaces := make([]string, 0, len(sd.endpoints))
	for namespace := range sd.endpoints {
		namespaces = append(namespaces, namespace)
	}

	return namespaces
}
