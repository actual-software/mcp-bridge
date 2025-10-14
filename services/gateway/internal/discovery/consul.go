package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	consulapi "github.com/hashicorp/consul/api"
	"go.uber.org/zap"

	"github.com/actual-software/mcp-bridge/services/gateway/internal/config"
	customerrors "github.com/actual-software/mcp-bridge/services/gateway/internal/errors"
)

// Protocol and scheme constants for service discovery.
const (
	schemeHTTPS       = "https"
	schemeHTTP        = "http"
	schemeWSS         = "wss"
	protocolWebSocket = "websocket"
	protocolSSE       = "sse"
	protocolStdio     = "stdio"
	servicePrefix     = "mcp/"
	defaultNamespace  = "default"
)

// ConsulDiscovery implements service discovery using Consul.
type ConsulDiscovery struct {
	config config.ServiceDiscoveryConfig
	logger *zap.Logger
	client *consulapi.Client

	// Service registry
	endpoints map[string][]Endpoint // namespace -> endpoints
	mu        sync.RWMutex

	// Watch management
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	lastIndex uint64
}

// ConsulConfig contains Consul-specific configuration.
type ConsulConfig struct {
	Address       string `mapstructure:"address"         yaml:"address"`
	ServicePrefix string `mapstructure:"service_prefix"  yaml:"service_prefix"`
	Datacenter    string `mapstructure:"datacenter"      yaml:"datacenter"`
	Token         string `mapstructure:"token"           yaml:"token"`
	TLSEnabled    bool   `mapstructure:"tls_enabled"     yaml:"tls_enabled"`
	TLSSkipVerify bool   `mapstructure:"tls_skip_verify" yaml:"tls_skip_verify"`
	WatchTimeout  string `mapstructure:"watch_timeout"   yaml:"watch_timeout"`
}

// InitializeConsulServiceDiscovery creates and configures Consul-based service discovery.
func InitializeConsulServiceDiscovery(
	cfg config.ServiceDiscoveryConfig,
	consulCfg ConsulConfig,
	logger *zap.Logger,
) (*ConsulDiscovery, error) {
	// Create Consul client configuration
	clientConfig := consulapi.DefaultConfig()

	if consulCfg.Address != "" {
		clientConfig.Address = consulCfg.Address
	}

	if consulCfg.Datacenter != "" {
		clientConfig.Datacenter = consulCfg.Datacenter
	}

	if consulCfg.Token != "" {
		clientConfig.Token = consulCfg.Token
	}

	// Configure TLS
	if consulCfg.TLSEnabled {
		clientConfig.Scheme = schemeHTTPS
		if consulCfg.TLSSkipVerify {
			clientConfig.TLSConfig.InsecureSkipVerify = true
		}
	}

	client, err := consulapi.NewClient(clientConfig)
	if err != nil {
		return nil, customerrors.Wrap(err, "failed to create Consul client").
			WithComponent("discovery_consul")
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &ConsulDiscovery{
		config:    cfg,
		logger:    logger,
		client:    client,
		endpoints: make(map[string][]Endpoint),
		ctx:       ctx,
		cancel:    cancel,
	}, nil
}

// Start starts the Consul service discovery.
func (d *ConsulDiscovery) Start(_ context.Context) error {
	d.logger.Info("Starting Consul service discovery")

	// Initial discovery
	if err := d.discoverServices(); err != nil {
		return customerrors.Wrap(err, "initial service discovery failed").
			WithComponent("discovery_consul")
	}

	// Start service watcher
	d.wg.Add(1)

	go d.watchServices()

	// Start periodic health checks
	d.wg.Add(1)

	go d.periodicHealthCheck()

	return nil
}

// Stop stops the Consul service discovery.
func (d *ConsulDiscovery) Stop() {
	d.logger.Info("Stopping Consul service discovery")

	// Cancel context to stop watchers
	d.cancel()

	// Wait for goroutines
	d.wg.Wait()
}

// discoverServices performs initial service discovery from Consul.
func (d *ConsulDiscovery) discoverServices() error {
	// Get service prefix from config (default: "mcp/")
	// servicePrefix is defined as a constant
	d.logger.Debug("Discovering Consul services", zap.String("prefix", servicePrefix))

	// List all services with the MCP prefix
	services, _, err := d.client.Catalog().Services(&consulapi.QueryOptions{})
	if err != nil {
		return customerrors.Wrap(err, "failed to list Consul services").
			WithComponent("discovery_consul")
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	// Clear existing endpoints
	d.endpoints = make(map[string][]Endpoint)

	for serviceName, tags := range services {
		// Skip services that don't match our prefix
		if !strings.HasPrefix(serviceName, servicePrefix) {
			continue
		}

		// Check if service has MCP tag
		if !d.hasTag(tags, "mcp") {
			continue
		}

		// Get healthy service instances
		serviceEntries, _, err := d.client.Health().Service(serviceName, "", true, &consulapi.QueryOptions{})
		if err != nil {
			d.logger.Warn("Failed to get service health",
				zap.String("service", serviceName),
				zap.Error(err))

			continue
		}

		for _, entry := range serviceEntries {
			endpoint, err := d.consulServiceToEndpoint(entry, servicePrefix)
			if err != nil {
				d.logger.Warn("Failed to convert Consul service to endpoint",
					zap.String("service", serviceName),
					zap.Error(err))

				continue
			}

			if endpoint.Namespace != "" {
				d.endpoints[endpoint.Namespace] = append(d.endpoints[endpoint.Namespace], *endpoint)
			}
		}
	}

	// Log discovery results
	totalEndpoints := 0

	for ns, eps := range d.endpoints {
		d.logger.Info("Discovered MCP services in namespace",
			zap.String("namespace", ns),
			zap.Int("count", len(eps)))

		totalEndpoints += len(eps)
	}

	d.logger.Info("Consul service discovery completed", zap.Int("total_endpoints", totalEndpoints))

	return nil
}

// consulServiceToEndpoint converts a Consul service entry to our Endpoint format.
func (d *ConsulDiscovery) consulServiceToEndpoint(
	entry *consulapi.ServiceEntry,
	servicePrefix string,
) (*Endpoint, error) {
	service := entry.Service

	// Extract namespace
	namespace := d.extractNamespace(service, servicePrefix)

	// Determine scheme and port
	scheme := d.determineScheme(service)

	// Parse service metadata
	tools := d.parseTools(service)
	weight := d.parseWeight(service)
	path := d.parsePath(service)

	// Build endpoint
	endpoint := d.buildEndpoint(entry, namespace, scheme, path, weight, tools)

	// Add all service metadata
	d.addServiceMetadata(endpoint, service)

	return endpoint, nil
}

// extractNamespace extracts the MCP namespace from the service.
func (d *ConsulDiscovery) extractNamespace(service *consulapi.AgentService, servicePrefix string) string {
	// Try to extract from service name prefix
	mcpNamespace := strings.TrimPrefix(service.Service, servicePrefix)
	if mcpNamespace != service.Service {
		return mcpNamespace
	}

	// Fallback to metadata
	if ns, ok := service.Meta["mcp_namespace"]; ok {
		return ns
	}

	return defaultNamespace
}

// determineScheme determines the scheme based on service tags.
func (d *ConsulDiscovery) determineScheme(service *consulapi.AgentService) string {
	if d.hasTag(service.Tags, "https") {
		return schemeHTTPS
	}

	if d.hasTag(service.Tags, "websocket") || d.hasTag(service.Tags, "ws") {
		if d.hasTag(service.Tags, "tls") {
			return schemeWSS
		}

		return "ws"
	}

	return schemeHTTP
}

// parseTools parses tools from service metadata.
func (d *ConsulDiscovery) parseTools(service *consulapi.AgentService) []ToolInfo {
	toolsJSON, ok := service.Meta["mcp_tools"]
	if !ok {
		return nil
	}

	var tools []ToolInfo
	if err := json.Unmarshal([]byte(toolsJSON), &tools); err != nil {
		d.logger.Warn("Failed to parse tools metadata",
			zap.String("service", service.Service),
			zap.Error(err))

		return nil
	}

	return tools
}

// parseWeight parses the weight from service metadata.
func (d *ConsulDiscovery) parseWeight(service *consulapi.AgentService) int {
	weightStr, ok := service.Meta["mcp_weight"]
	if !ok {
		return DefaultWeight
	}

	weight, err := strconv.Atoi(weightStr)
	if err != nil {
		return DefaultWeight
	}

	return weight
}

// parsePath parses the path from service metadata.
func (d *ConsulDiscovery) parsePath(service *consulapi.AgentService) string {
	if path, ok := service.Meta["mcp_path"]; ok {
		return path
	}

	return "/"
}

// buildEndpoint creates the endpoint structure.
func (d *ConsulDiscovery) buildEndpoint(
	entry *consulapi.ServiceEntry,
	namespace, scheme, path string,
	weight int,
	tools []ToolInfo,
) *Endpoint {
	service := entry.Service

	endpoint := &Endpoint{
		Service:   service.Service,
		Namespace: namespace,
		Address:   service.Address,
		Port:      service.Port,
		Scheme:    scheme,
		Path:      path,
		Weight:    weight,
		Metadata: map[string]string{
			"consul_id":         service.ID,
			"consul_datacenter": entry.Node.Datacenter,
			"consul_node":       entry.Node.Node,
			"protocol":          d.inferProtocol(service),
		},
		Tools: tools,
	}
	endpoint.SetHealthy(d.isHealthy(entry))

	return endpoint
}

// addServiceMetadata adds all service metadata to the endpoint.
func (d *ConsulDiscovery) addServiceMetadata(endpoint *Endpoint, service *consulapi.AgentService) {
	for k, v := range service.Meta {
		endpoint.Metadata[k] = v
	}
}

// watchServices watches for service changes in Consul.
func (d *ConsulDiscovery) watchServices() {
	defer d.wg.Done()

	watchTimeout := ConsulWatchTimeoutMinutes * time.Minute

	for {
		select {
		case <-d.ctx.Done():
			return
		default:
		}

		// Watch for service changes
		opts := &consulapi.QueryOptions{
			WaitIndex: d.lastIndex,
			WaitTime:  watchTimeout,
		}

		_, meta, err := d.client.Catalog().Services(opts)
		if err != nil {
			d.logger.Error("Consul service watch failed", zap.Error(err))
			time.Sleep(ConsulWatchErrorDelaySeconds * time.Second)

			continue
		}

		// Update last index for long polling
		d.lastIndex = meta.LastIndex

		// Check if services changed
		if meta.LastIndex > opts.WaitIndex {
			d.logger.Debug("Consul services changed, updating discovery")

			if err := d.discoverServices(); err != nil {
				d.logger.Error("Failed to update service discovery", zap.Error(err))
			}
		}
	}
}

// periodicHealthCheck performs periodic health checks and cleanup.
func (d *ConsulDiscovery) periodicHealthCheck() {
	defer d.wg.Done()

	ticker := time.NewTicker(ConsulHealthCheckSeconds * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-d.ctx.Done():
			return
		case <-ticker.C:
			if err := d.updateHealthStatus(); err != nil {
				d.logger.Error("Failed to update health status", zap.Error(err))
			}
		}
	}
}

// updateHealthStatus updates the health status of all endpoints.
func (d *ConsulDiscovery) updateHealthStatus() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// servicePrefix is defined as a constant

	for namespace, endpoints := range d.endpoints {
		for i := range endpoints {
			endpoint := &endpoints[i]

			// Get current health from Consul
			serviceName := servicePrefix + namespace
			if endpoint.Metadata["consul_id"] != "" {
				serviceName = endpoint.Service
			}

			serviceEntries, _, err := d.client.Health().Service(serviceName, "", false, &consulapi.QueryOptions{})
			if err != nil {
				d.logger.Warn("Failed to check service health",
					zap.String("service", serviceName),
					zap.Error(err))

				continue
			}

			// Find matching service instance
			healthy := false

			for _, entry := range serviceEntries {
				if entry.Service.Address == endpoint.Address && entry.Service.Port == endpoint.Port {
					healthy = d.isHealthy(entry)

					break
				}
			}

			if endpoint.IsHealthy() != healthy {
				d.logger.Debug("Endpoint health status changed",
					zap.String("endpoint", fmt.Sprintf("%s:%d", endpoint.Address, endpoint.Port)),
					zap.Bool("healthy", healthy))

				endpoint.SetHealthy(healthy)
			}
		}
	}

	return nil
}

// hasTag checks if a tag exists in the tag list.
func (d *ConsulDiscovery) hasTag(tags []string, tag string) bool {
	for _, t := range tags {
		if strings.EqualFold(t, tag) {
			return true
		}
	}

	return false
}

// isHealthy determines if a Consul service entry is healthy.
func (d *ConsulDiscovery) isHealthy(entry *consulapi.ServiceEntry) bool {
	for _, check := range entry.Checks {
		if check.Status != consulapi.HealthPassing {
			return false
		}
	}

	return true
}

// inferProtocol infers the MCP protocol from Consul service information.
func (d *ConsulDiscovery) inferProtocol(service *consulapi.AgentService) string {
	// Check explicit protocol metadata
	if protocol, ok := service.Meta["mcp_protocol"]; ok {
		return protocol
	}

	// Infer from tags
	for _, tag := range service.Tags {
		switch strings.ToLower(tag) {
		case "websocket", "ws":
			return protocolWebSocket
		case "sse", "server-sent-events":
			return protocolSSE
		case "stdio", "process":
			return protocolStdio
		case "http", "rest":
			return "http"
		}
	}

	// Default to HTTP
	return schemeHTTP
}

// GetEndpoints returns endpoints for a specific namespace.
func (d *ConsulDiscovery) GetEndpoints(namespace string) []Endpoint {
	d.mu.RLock()
	defer d.mu.RUnlock()

	endpoints := d.endpoints[namespace]
	// Return a copy to avoid race conditions
	result := make([]Endpoint, len(endpoints))
	copy(result, endpoints)

	return result
}

// GetAllEndpoints returns all discovered endpoints.
func (d *ConsulDiscovery) GetAllEndpoints() map[string][]Endpoint {
	d.mu.RLock()
	defer d.mu.RUnlock()

	// Return a copy
	result := make(map[string][]Endpoint)

	for k, v := range d.endpoints {
		endpoints := make([]Endpoint, len(v))
		copy(endpoints, v)
		result[k] = endpoints
	}

	return result
}

// ListNamespaces returns all namespaces with discovered services.
func (d *ConsulDiscovery) ListNamespaces() []string {
	d.mu.RLock()
	defer d.mu.RUnlock()

	namespaces := make([]string, 0, len(d.endpoints))
	for ns := range d.endpoints {
		namespaces = append(namespaces, ns)
	}

	return namespaces
}

// RegisterEndpointChangeCallback is a no-op for Consul discovery.
func (d *ConsulDiscovery) RegisterEndpointChangeCallback(callback func(namespace string)) {
	// Consul discovery doesn't support dynamic endpoint change notifications yet
}
