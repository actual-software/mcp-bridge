package discovery

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/actual-software/mcp-bridge/services/gateway/internal/config"
	customerrors "github.com/actual-software/mcp-bridge/services/gateway/internal/errors"
)

// protocolStdio is already defined in consul.go - commenting out to avoid redeclaration
// const (
// 	protocolStdio = "stdio"
// )

// StdioDiscovery implements service discovery for stdio-based MCP servers.
type StdioDiscovery struct {
	config   config.StdioDiscoveryConfig
	logger   *zap.Logger
	services map[string]*config.StdioServiceConfig
}

// CreateStdioServiceDiscovery creates a stdio-based (process communication) service discovery instance.
//

//nolint:ireturn // Factory pattern requires interface return
func CreateStdioServiceDiscovery(cfg config.StdioDiscoveryConfig, logger *zap.Logger) (ServiceDiscovery, error) {
	services := make(map[string]*config.StdioServiceConfig)

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

	return &StdioDiscovery{
		config:   cfg,
		logger:   logger.With(zap.String("discovery", protocolStdio)),
		services: services,
	}, nil
}

// GetServices returns all discovered stdio services as endpoints.
func (s *StdioDiscovery) GetServices(ctx context.Context) ([]Endpoint, error) {
	endpoints := make([]Endpoint, 0, len(s.services))

	for _, service := range s.services {
		endpoint := Endpoint{
			Service:   service.Name,
			Namespace: service.Namespace,
			Address:   strings.Join(service.Command, " "), // Store command as address
			Port:      0,                                  // Not applicable for stdio
			Scheme:    protocolStdio,
			Path:      service.WorkingDir,
			Weight:    service.Weight,
			Metadata:  s.createMetadata(service),
		}
		endpoint.SetHealthy(true) // Assume healthy initially

		// Check if the command is available
		if len(service.Command) > 0 {
			if _, err := exec.LookPath(service.Command[0]); err != nil {
				s.logger.Warn("command not found in PATH",
					zap.String("service", service.Name),
					zap.String("command", service.Command[0]),
					zap.Error(err))

				endpoint.SetHealthy(false)
			}
		}

		endpoints = append(endpoints, endpoint)
	}

	s.logger.Debug("discovered stdio services", zap.Int("count", len(endpoints)))

	return endpoints, nil
}

// GetService returns a specific service by name.
func (s *StdioDiscovery) GetService(ctx context.Context, serviceName string) (*Endpoint, error) {
	service, exists := s.services[serviceName]
	if !exists {
		return nil, customerrors.NewServiceNotFoundError(serviceName, "")
	}

	endpoint := &Endpoint{
		Service:   service.Name,
		Namespace: service.Namespace,
		Address:   strings.Join(service.Command, " "),
		Port:      0,
		Scheme:    "stdio",
		Path:      service.WorkingDir,
		Weight:    service.Weight,
		Metadata:  s.createMetadata(service),
	}
	endpoint.SetHealthy(true)

	// Check command availability
	if len(service.Command) > 0 {
		if _, err := exec.LookPath(service.Command[0]); err != nil {
			endpoint.SetHealthy(false)
		}
	}

	return endpoint, nil
}

// Watch monitors for changes in stdio service configuration.
func (s *StdioDiscovery) Watch(ctx context.Context) (<-chan []Endpoint, error) {
	ch := make(chan []Endpoint, 1)

	// For stdio discovery, we'll just send the initial list and then
	// periodically check command availability
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

// HealthCheck performs a health check on a stdio service.
func (s *StdioDiscovery) HealthCheck(ctx context.Context, endpoint *Endpoint) error {
	serviceName := endpoint.Service
	service, exists := s.services[serviceName]

	if !exists {
		return customerrors.NewServiceNotFoundError(serviceName, "")
	}

	if !service.HealthCheck.Enabled {
		return nil // Health check disabled
	}

	if len(service.Command) == 0 {
		return customerrors.NewInvalidServiceConfigError("no command configured", serviceName)
	}

	// Check if command is available
	if _, err := exec.LookPath(service.Command[0]); err != nil {
		return customerrors.NewInvalidServiceConfigError(
			fmt.Sprintf("command %s not found: %v", service.Command[0], err),
			serviceName,
		)
	}

	// For stdio services, we can't easily do a deep health check without starting the process
	// So we just verify the command exists and is executable
	return nil
}

// createMetadata creates metadata map with stdio-specific information.
func (s *StdioDiscovery) createMetadata(service *config.StdioServiceConfig) map[string]string {
	metadata := make(map[string]string)

	// Copy user-defined metadata
	for k, v := range service.Metadata {
		metadata[k] = v
	}

	// Add stdio-specific metadata
	metadata["protocol"] = protocolStdio
	metadata["command"] = strings.Join(service.Command, " ")
	metadata["working_dir"] = service.WorkingDir
	metadata["health_check_enabled"] = strconv.FormatBool(service.HealthCheck.Enabled)

	// Add environment variables as metadata (be careful with sensitive data)
	for k, v := range service.Env {
		// Only include non-sensitive environment variables
		if !strings.Contains(strings.ToLower(k), "password") &&
			!strings.Contains(strings.ToLower(k), "secret") &&
			!strings.Contains(strings.ToLower(k), "token") &&
			!strings.Contains(strings.ToLower(k), "key") {
			metadata["env_"+strings.ToLower(k)] = v
		}
	}

	return metadata
}

// GetAllEndpoints returns all endpoints from all namespaces.
func (s *StdioDiscovery) GetAllEndpoints() map[string][]Endpoint {
	endpoints, _ := s.GetServices(context.Background())
	result := make(map[string][]Endpoint)

	for _, endpoint := range endpoints {
		result[endpoint.Namespace] = append(result[endpoint.Namespace], endpoint)
	}

	return result
}

// Start starts the discovery service.
func (s *StdioDiscovery) Start(ctx context.Context) error {
	// For stdio discovery, no initialization is needed
	return nil
}

// Stop stops the discovery service.
func (s *StdioDiscovery) Stop() {
	// For stdio discovery, no cleanup is needed
}

// GetEndpoints returns endpoints for a specific namespace.
func (s *StdioDiscovery) GetEndpoints(namespace string) []Endpoint {
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
func (s *StdioDiscovery) ListNamespaces() []string {
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

// RegisterEndpointChangeCallback is a no-op for Stdio discovery.
func (s *StdioDiscovery) RegisterEndpointChangeCallback(callback func(namespace string)) {
	// Stdio discovery doesn't support dynamic endpoint change notifications yet
}
