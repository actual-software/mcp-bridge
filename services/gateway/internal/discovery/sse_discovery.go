package discovery

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/poiley/mcp-bridge/services/gateway/internal/config"
	customerrors "github.com/poiley/mcp-bridge/services/gateway/internal/errors"
)

// SSE discovery constants.
const (
	defaultNamespaceSSE = "default"
	schemeHTTPSSSE      = "https"
	// protocolSSE is already defined in consul.go - commenting out to avoid redeclaration
	// protocolSSE         = "sse".
)

// SSEDiscovery implements service discovery for SSE-based MCP servers.
type SSEDiscovery struct {
	config     config.SSEDiscoveryConfig
	logger     *zap.Logger
	services   map[string]*config.SSEServiceConfig
	httpClient *http.Client
}

// CreateSSEServiceDiscovery creates a Server-Sent Events based service discovery instance.
//

//nolint:ireturn // Factory pattern requires interface return
func CreateSSEServiceDiscovery(cfg config.SSEDiscoveryConfig, logger *zap.Logger) (ServiceDiscovery, error) {
	services := make(map[string]*config.SSEServiceConfig)

	for i := range cfg.Services {
		service := &cfg.Services[i]

		// Set defaults
		if service.Weight == 0 {
			service.Weight = 1
		}

		if service.Namespace == "" {
			service.Namespace = defaultNamespaceSSE
		}

		if service.StreamEndpoint == "" {
			service.StreamEndpoint = "/events"
		}

		if service.RequestEndpoint == "" {
			service.RequestEndpoint = "/api/v1/request"
		}

		if service.Timeout == 0 {
			service.Timeout = MediumTimeout
		}

		if service.HealthCheck.Interval == 0 {
			service.HealthCheck.Interval = DefaultHealthCheckInterval
		}

		if service.HealthCheck.Timeout == 0 {
			service.HealthCheck.Timeout = DefaultHealthCheckTimeout
		}

		services[service.Name] = service
	}

	// Create HTTP client for health checks
	httpClient := &http.Client{
		Timeout: MediumTimeout,
	}

	return &SSEDiscovery{
		config:     cfg,
		logger:     logger.With(zap.String("discovery", protocolSSE)),
		services:   services,
		httpClient: httpClient,
	}, nil
}

// GetServices returns all discovered SSE services as endpoints.
func (s *SSEDiscovery) GetServices(ctx context.Context) ([]Endpoint, error) {
	endpoints := make([]Endpoint, 0, len(s.services))

	for _, service := range s.services {
		u, err := url.Parse(service.BaseURL)
		if err != nil {
			s.logger.Warn("invalid base URL",
				zap.String("service", service.Name),
				zap.String("url", service.BaseURL),
				zap.Error(err))

			continue
		}

		// Determine port
		port := 80
		if u.Scheme == schemeHTTPSSSE {
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
			Metadata:  s.createMetadata(service),
			Healthy:   true, // Assume healthy initially
		}

		// Perform basic connectivity check
		if err := s.checkServiceConnectivity(ctx, service); err != nil {
			s.logger.Warn("service connectivity check failed",
				zap.String("service", service.Name),
				zap.String("base_url", service.BaseURL),
				zap.Error(err))

			endpoint.Healthy = false
		}

		endpoints = append(endpoints, endpoint)
	}

	s.logger.Debug("discovered SSE services", zap.Int("count", len(endpoints)))

	return endpoints, nil
}

// GetService returns a specific service by name.
func (s *SSEDiscovery) GetService(ctx context.Context, serviceName string) (*Endpoint, error) {
	service, exists := s.services[serviceName]
	if !exists {
		return nil, customerrors.NewServiceNotFoundError(serviceName, "")
	}

	u, err := url.Parse(service.BaseURL)
	if err != nil {
		return nil, customerrors.NewInvalidEndpointError(service.BaseURL, err.Error())
	}

	port := 80
	if u.Scheme == schemeHTTPSSSE {
		port = 443
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
		Metadata:  s.createMetadata(service),
		Healthy:   true,
	}

	// Check connectivity
	if err := s.checkServiceConnectivity(ctx, service); err != nil {
		endpoint.Healthy = false
	}

	return endpoint, nil
}

// Watch monitors for changes in SSE service configuration.
func (s *SSEDiscovery) Watch(ctx context.Context) (<-chan []Endpoint, error) {
	ch := make(chan []Endpoint, 1)

	go func() {
		defer close(ch)

		ticker := time.NewTicker(LongTimeout)
		defer ticker.Stop()

		// Send initial list
		if endpoints, err := s.GetServices(ctx); err == nil {
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
				if endpoints, err := s.GetServices(ctx); err == nil {
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

// HealthCheck performs a health check on an SSE service endpoint.
func (s *SSEDiscovery) HealthCheck(ctx context.Context, endpoint *Endpoint) error {
	serviceName := endpoint.Service
	service, exists := s.services[serviceName]

	if !exists {
		return customerrors.NewServiceNotFoundError(serviceName, "")
	}

	if !service.HealthCheck.Enabled {
		return nil // Health check disabled
	}

	return s.checkServiceConnectivity(ctx, service)
}

// checkServiceConnectivity performs connectivity checks for an SSE service.
func (s *SSEDiscovery) checkServiceConnectivity(ctx context.Context, service *config.SSEServiceConfig) error {
	// Create a context with timeout for the health check
	healthCtx, cancel := context.WithTimeout(ctx, service.HealthCheck.Timeout)
	defer cancel()

	// Check both the stream endpoint and request endpoint
	streamURL := service.BaseURL + service.StreamEndpoint
	requestURL := service.BaseURL + service.RequestEndpoint

	// Check stream endpoint (should accept SSE connections)
	if err := s.checkSSEEndpoint(healthCtx, streamURL, service); err != nil {
		return customerrors.WrapHealthCheckError(ctx, err, service.Name, streamURL)
	}

	// Check request endpoint (should accept HTTP POST)
	if err := s.checkHTTPEndpoint(healthCtx, requestURL, service); err != nil {
		return customerrors.WrapHealthCheckError(ctx, err, service.Name, requestURL)
	}

	return nil
}

// checkSSEEndpoint checks if the SSE stream endpoint is accessible.
func (s *SSEDiscovery) checkSSEEndpoint(ctx context.Context, streamURL string, service *config.SSEServiceConfig) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, streamURL, nil)
	if err != nil {
		return customerrors.Wrap(err, "failed to create SSE request").
			WithComponent("discovery_sse")
	}

	// Set SSE headers
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")

	// Add custom headers
	for k, v := range service.Headers {
		req.Header.Set(k, v)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return customerrors.Wrap(err, "failed to connect to SSE stream").
			WithComponent("discovery_sse")
	}

	defer func() {
		if err := resp.Body.Close(); err != nil {
			s.logger.Debug("Failed to close response body", zap.Error(err))
		}
	}()

	// Check status code
	if resp.StatusCode != http.StatusOK {
		return customerrors.New(customerrors.TypeInternal, fmt.Sprintf("SSE stream returned status %d", resp.StatusCode)).
			WithComponent("discovery_sse").
			WithContext("status_code", resp.StatusCode)
	}

	// Check content type
	contentType := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "text/event-stream") {
		return customerrors.New(customerrors.TypeValidation, "invalid content type for SSE: "+contentType).
			WithComponent("discovery_sse").
			WithContext("content_type", contentType)
	}

	return nil
}

// checkHTTPEndpoint checks if the HTTP request endpoint is accessible.
func (s *SSEDiscovery) checkHTTPEndpoint(
	ctx context.Context,
	requestURL string,
	service *config.SSEServiceConfig,
) error {
	// Try a HEAD request first to avoid sending actual data
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, requestURL, nil)
	if err != nil {
		return customerrors.Wrap(err, "failed to create HTTP request").
			WithComponent("discovery_sse")
	}

	// Add custom headers
	for k, v := range service.Headers {
		req.Header.Set(k, v)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return customerrors.Wrap(err, "failed to connect to HTTP endpoint").
			WithComponent("discovery_sse")
	}

	defer func() {
		if err := resp.Body.Close(); err != nil {
			s.logger.Debug("Failed to close response body", zap.Error(err))
		}
	}()

	// For HEAD requests, we expect either 200 or 405 (Method Not Allowed)
	// 405 is acceptable because it means the endpoint exists but doesn't support HEAD
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusMethodNotAllowed {
		return customerrors.New(customerrors.TypeInternal, fmt.Sprintf("HTTP endpoint returned status %d", resp.StatusCode)).
			WithComponent("discovery_sse").
			WithContext("status_code", resp.StatusCode)
	}

	return nil
}

// createMetadata creates metadata map with SSE-specific information.
func (s *SSEDiscovery) createMetadata(service *config.SSEServiceConfig) map[string]string {
	metadata := make(map[string]string)

	// Copy user-defined metadata
	for k, v := range service.Metadata {
		metadata[k] = v
	}

	// Add SSE-specific metadata
	metadata["protocol"] = protocolSSE
	metadata["base_url"] = service.BaseURL
	metadata["stream_endpoint"] = service.StreamEndpoint
	metadata["request_endpoint"] = service.RequestEndpoint
	metadata["health_check_enabled"] = strconv.FormatBool(service.HealthCheck.Enabled)
	metadata["timeout"] = service.Timeout.String()

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
func (s *SSEDiscovery) GetAllEndpoints() map[string][]Endpoint {
	endpoints, _ := s.GetServices(context.Background())
	result := make(map[string][]Endpoint)

	for _, endpoint := range endpoints {
		result[endpoint.Namespace] = append(result[endpoint.Namespace], endpoint)
	}

	return result
}

// Start starts the discovery service.
func (s *SSEDiscovery) Start(ctx context.Context) error {
	return nil
}

// Stop stops the discovery service.
func (s *SSEDiscovery) Stop() {
}

// GetEndpoints returns endpoints for a specific namespace.
func (s *SSEDiscovery) GetEndpoints(namespace string) []Endpoint {
	endpoints, _ := s.GetServices(context.Background())

	var result []Endpoint

	for _, endpoint := range endpoints {
		if endpoint.Namespace == namespace {
			result = append(result, endpoint)
		}
	}

	return result
}

// ListNamespaces returns all available namespaces.
func (s *SSEDiscovery) ListNamespaces() []string {
	endpoints, _ := s.GetServices(context.Background())
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
