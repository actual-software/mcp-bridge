package discovery

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"github.com/poiley/mcp-bridge/services/gateway/internal/config"
	customerrors "github.com/poiley/mcp-bridge/services/gateway/internal/errors"
)

// protocolWebSocket is already defined in consul.go - commenting out to avoid redeclaration
// const (
// 	protocolWebSocket = "websocket"
// )

// WebSocketDiscovery implements service discovery for WebSocket-based MCP servers.
type WebSocketDiscovery struct {
	config   config.WebSocketDiscoveryConfig
	logger   *zap.Logger
	services map[string]*config.WebSocketServiceConfig
}

// CreateWebSocketServiceDiscovery creates a WebSocket-based service discovery instance.
//

func CreateWebSocketServiceDiscovery(
	cfg config.WebSocketDiscoveryConfig,
	logger *zap.Logger,
) (ServiceDiscovery, error) {
	services := make(map[string]*config.WebSocketServiceConfig)

	for i := range cfg.Services {
		service := &cfg.Services[i]

		// Set defaults
		if service.Weight == 0 {
			service.Weight = 1
		}

		if service.Namespace == "" {
			service.Namespace = "default"
		}

		if service.HealthCheck.Interval == 0 {
			service.HealthCheck.Interval = DefaultHealthCheckInterval
		}

		if service.HealthCheck.Timeout == 0 {
			service.HealthCheck.Timeout = DefaultHealthCheckTimeout
		}

		services[service.Name] = service
	}

	return &WebSocketDiscovery{
		config:   cfg,
		logger:   logger.With(zap.String("discovery", protocolWebSocket)),
		services: services,
	}, nil
}

// GetServices returns all discovered WebSocket services as endpoints.
func (w *WebSocketDiscovery) GetServices(ctx context.Context) ([]Endpoint, error) {
	var endpoints []Endpoint

	for _, service := range w.services {
		// Create an endpoint for each configured endpoint URL
		for _, endpointURL := range service.Endpoints {
			u, err := url.Parse(endpointURL)
			if err != nil {
				w.logger.Warn("invalid endpoint URL",
					zap.String("service", service.Name),
					zap.String("url", endpointURL),
					zap.Error(err))

				continue
			}

			// Determine port
			port := 80
			if u.Scheme == "wss" {
				port = 443
			}

			if u.Port() != "" {
				if p, err := strconv.Atoi(u.Port()); err == nil {
					port = p
				}
			}

			endpoint := Endpoint{
				Service:   service.Name,
				Namespace: service.Namespace,
				Address:   u.Hostname(),
				Port:      port,
				Scheme:    u.Scheme,
				Path:      u.Path,
				Weight:    service.Weight,
				Metadata:  w.createMetadata(service, endpointURL),
				Healthy:   true, // Assume healthy initially
			}

			// Perform basic connectivity check
			if w.config.Services != nil {
				if err := w.checkEndpointConnectivity(ctx, endpointURL, service); err != nil {
					w.logger.Warn("endpoint connectivity check failed",
						zap.String("service", service.Name),
						zap.String("endpoint", endpointURL),
						zap.Error(err))

					endpoint.Healthy = false
				}
			}

			endpoints = append(endpoints, endpoint)
		}
	}

	w.logger.Debug("discovered WebSocket services", zap.Int("count", len(endpoints)))

	return endpoints, nil
}

// GetService returns a specific service by name.
func (w *WebSocketDiscovery) GetService(ctx context.Context, serviceName string) (*Endpoint, error) {
	service, exists := w.services[serviceName]
	if !exists {
		return nil, customerrors.NewServiceNotFoundError(serviceName, "")
	}

	if len(service.Endpoints) == 0 {
		return nil, customerrors.NewNoHealthyInstancesError(serviceName, "", 0)
	}

	// Return the first endpoint as primary
	endpointURL := service.Endpoints[0]

	u, err := url.Parse(endpointURL)
	if err != nil {
		return nil, customerrors.NewInvalidEndpointError(service.Endpoints[0], err.Error())
	}

	port := DefaultHTTPPort
	if u.Scheme == "wss" {
		port = DefaultHTTPSPort
	}

	if u.Port() != "" {
		if p, err := strconv.Atoi(u.Port()); err == nil {
			port = p
		}
	}

	endpoint := &Endpoint{
		Service:   service.Name,
		Namespace: service.Namespace,
		Address:   u.Hostname(),
		Port:      port,
		Scheme:    u.Scheme,
		Path:      u.Path,
		Weight:    service.Weight,
		Metadata:  w.createMetadata(service, endpointURL),
		Healthy:   true,
	}

	// Check connectivity
	if err := w.checkEndpointConnectivity(ctx, endpointURL, service); err != nil {
		endpoint.Healthy = false
	}

	return endpoint, nil
}

// Watch monitors for changes in WebSocket service configuration.
func (w *WebSocketDiscovery) Watch(ctx context.Context) (<-chan []Endpoint, error) {
	ch := make(chan []Endpoint, 1)

	go func() {
		defer close(ch)

		ticker := time.NewTicker(LongTimeout)
		defer ticker.Stop()

		// Send initial list
		if endpoints, err := w.GetServices(ctx); err == nil {
			select {
			case ch <- endpoints:
			case <-ctx.Done():
				return
			}
		}

		// Periodically check and update
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if endpoints, err := w.GetServices(ctx); err == nil {
					select {
					case ch <- endpoints:
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}()

	return ch, nil
}

// HealthCheck performs a health check on a WebSocket service endpoint.
func (w *WebSocketDiscovery) HealthCheck(ctx context.Context, endpoint *Endpoint) error {
	serviceName := endpoint.Service
	service, exists := w.services[serviceName]

	if !exists {
		return customerrors.NewServiceNotFoundError(serviceName, "")
	}

	if !service.HealthCheck.Enabled {
		return nil // Health check disabled
	}

	// Reconstruct the WebSocket URL
	scheme := endpoint.Scheme
	if scheme == "" {
		scheme = "ws"
	}

	endpointURL := fmt.Sprintf("%s://%s:%d%s", scheme, endpoint.Address, endpoint.Port, endpoint.Path)

	return w.checkEndpointConnectivity(ctx, endpointURL, service)
}

// checkEndpointConnectivity performs a basic connectivity check.
func (w *WebSocketDiscovery) checkEndpointConnectivity(
	ctx context.Context,
	endpointURL string,
	service *config.WebSocketServiceConfig,
) error {
	// Create a context with timeout for the health check
	healthCtx, cancel := context.WithTimeout(ctx, service.HealthCheck.Timeout)
	defer cancel()

	// Create WebSocket dialer
	dialer := websocket.Dialer{
		HandshakeTimeout: MediumTimeout,
	}

	// Set up headers
	headers := http.Header{}
	for k, v := range service.Headers {
		headers.Set(k, v)
	}

	if service.Origin != "" {
		headers.Set("Origin", service.Origin)
	}

	// Attempt to connect
	conn, resp, err := dialer.DialContext(healthCtx, endpointURL, headers)
	if resp != nil {
		defer func() { _ = resp.Body.Close() }()
	}

	if err != nil {
		return customerrors.WrapHealthCheckError(ctx, err, service.Name, endpointURL)
	}

	defer func() { _ = conn.Close() }()

	// Send a ping to verify the connection is working
	if err := conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(ShortTimeout)); err != nil {
		return customerrors.WrapHealthCheckError(ctx, err, service.Name, endpointURL)
	}

	// The connection is working
	return nil
}

// createMetadata creates metadata map with WebSocket-specific information.
func (w *WebSocketDiscovery) createMetadata(
	service *config.WebSocketServiceConfig,
	endpointURL string,
) map[string]string {
	metadata := make(map[string]string)

	// Copy user-defined metadata
	for k, v := range service.Metadata {
		metadata[k] = v
	}

	// Add WebSocket-specific metadata
	metadata["protocol"] = protocolWebSocket
	metadata["endpoint_url"] = endpointURL
	metadata["health_check_enabled"] = strconv.FormatBool(service.HealthCheck.Enabled)
	metadata["tls_enabled"] = strconv.FormatBool(service.TLS.Enabled)

	if service.Origin != "" {
		metadata["origin"] = service.Origin
	}

	// Add all endpoints as comma-separated list
	if len(service.Endpoints) > 1 {
		metadata["all_endpoints"] = strings.Join(service.Endpoints, ",")
	}

	// Add header information (excluding sensitive data)
	for k, v := range service.Headers {
		// Only include non-sensitive headers
		if !strings.Contains(strings.ToLower(k), "authorization") &&
			!strings.Contains(strings.ToLower(k), "password") &&
			!strings.Contains(strings.ToLower(k), "secret") &&
			!strings.Contains(strings.ToLower(k), "token") &&
			!strings.Contains(strings.ToLower(k), "key") {
			metadata["header_"+strings.ToLower(k)] = v
		}
	}

	return metadata
}

// GetAllEndpoints returns all endpoints from all namespaces.
func (w *WebSocketDiscovery) GetAllEndpoints() map[string][]Endpoint {
	endpoints, _ := w.GetServices(context.Background())
	result := make(map[string][]Endpoint)

	for _, endpoint := range endpoints {
		result[endpoint.Namespace] = append(result[endpoint.Namespace], endpoint)
	}

	return result
}

// Start starts the discovery service.
func (w *WebSocketDiscovery) Start(ctx context.Context) error {
	return nil
}

// Stop stops the discovery service.
func (w *WebSocketDiscovery) Stop() {
}

// GetEndpoints returns endpoints for a specific namespace.
func (w *WebSocketDiscovery) GetEndpoints(namespace string) []Endpoint {
	endpoints, _ := w.GetServices(context.Background())

	var result []Endpoint

	for _, endpoint := range endpoints {
		if endpoint.Namespace == namespace {
			result = append(result, endpoint)
		}
	}

	return result
}

// ListNamespaces returns all available namespaces.
func (w *WebSocketDiscovery) ListNamespaces() []string {
	endpoints, _ := w.GetServices(context.Background())
	namespaces := make(map[string]bool)

	for _, endpoint := range endpoints {
		namespaces[endpoint.Namespace] = true
	}

	result := make([]string, 0, len(namespaces))
	for namespace := range namespaces {
		result = append(result, namespace)
	}

	return result
}
