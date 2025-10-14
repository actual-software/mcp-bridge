package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/actual-software/mcp-bridge/services/gateway/internal/config"
	customerrors "github.com/actual-software/mcp-bridge/services/gateway/internal/errors"
)

// KubernetesDiscovery implements service discovery using Kubernetes API.
type KubernetesDiscovery struct {
	config config.ServiceDiscoveryConfig
	logger *zap.Logger
	client kubernetes.Interface

	// Service registry
	endpoints map[string][]Endpoint // namespace -> endpoints
	mu        sync.RWMutex

	// Watchers
	watchers []watch.Interface
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup

	// Endpoint change notification
	endpointChangeCallback func(namespace string)
	callbackMu             sync.RWMutex
}

// InitializeKubernetesServiceDiscovery creates and configures Kubernetes-based service discovery.
func InitializeKubernetesServiceDiscovery(
	cfg config.ServiceDiscoveryConfig,
	logger *zap.Logger,
) (*KubernetesDiscovery, error) {
	// Create Kubernetes client
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, customerrors.Wrap(err, "failed to create in-cluster config").
			WithComponent("discovery_k8s")
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, customerrors.Wrap(err, "failed to create Kubernetes client").
			WithComponent("discovery_k8s")
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &KubernetesDiscovery{
		config:    cfg,
		logger:    logger,
		client:    client,
		endpoints: make(map[string][]Endpoint),
		ctx:       ctx,
		cancel:    cancel,
	}, nil
}

// Start starts the service discovery.
func (d *KubernetesDiscovery) Start(ctx context.Context) error {
	d.logger.Info("Starting Kubernetes service discovery",
		zap.Strings("namespaces", d.config.NamespaceSelector),
		zap.Any("labels", d.config.LabelSelector),
	)

	// Initial discovery
	if err := d.discoverServices(); err != nil {
		return customerrors.Wrap(err, "initial service discovery failed").
			WithComponent("discovery_k8s")
	}

	// Start watchers for each namespace
	for _, namespace := range d.config.NamespaceSelector {
		if err := d.watchNamespace(namespace); err != nil {
			return customerrors.WrapWatchError(ctx, err, namespace)
		}
	}

	// Start periodic resync
	d.wg.Add(1)

	go d.periodicResync()

	return nil
}

// Stop stops the service discovery.
func (d *KubernetesDiscovery) Stop() {
	d.logger.Info("Stopping Kubernetes service discovery")

	// Cancel context
	d.cancel()

	// Stop all watchers
	for _, watcher := range d.watchers {
		watcher.Stop()
	}

	// Wait for goroutines
	d.wg.Wait()
}

// discoverServices performs initial service discovery.
func (d *KubernetesDiscovery) discoverServices() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Clear existing endpoints
	d.endpoints = make(map[string][]Endpoint)

	// Discover services in each namespace with retry
	for _, namespace := range d.config.NamespaceSelector {
		retryCount := 0
		maxRetries := KubernetesMaxRetries

		var endpoints []Endpoint

		var err error

		for retryCount < maxRetries {
			endpoints, err = d.discoverNamespaceServices(namespace)
			if err != nil {
				retryCount++
				d.logger.Warn("Failed to discover services in namespace, retrying",
					zap.String("namespace", namespace),
					zap.Error(err),
					zap.Int("retry", retryCount),
					zap.Int("max_retries", KubernetesMaxRetries),
				)

				if retryCount < maxRetries {
					// Exponential backoff: 1s, 2s, 4s
					time.Sleep(time.Duration(1<<uint(retryCount-1)) * time.Second) // #nosec G115 - retryCount is bounded

					continue
				}

				// All retries failed
				d.logger.Error("Failed to discover services in namespace after retries",
					zap.String("namespace", namespace),
					zap.Error(err),
				)

				continue
			}
			// Discovery succeeded
			break
		}

		// Group endpoints by MCP namespace
		for _, ep := range endpoints {
			d.endpoints[ep.Namespace] = append(d.endpoints[ep.Namespace], ep)
		}

		if len(endpoints) > 0 {
			d.logger.Info("Discovered MCP services",
				zap.String("k8s_namespace", namespace),
				zap.Int("count", len(endpoints)),
			)
		}
	}

	return nil
}

// discoverNamespaceServices discovers services in a specific namespace.
func (d *KubernetesDiscovery) discoverNamespaceServices(namespace string) ([]Endpoint, error) {
	// List services
	services, err := d.listNamespaceServices(namespace)
	if err != nil {
		return nil, err
	}

	// Process each service
	var endpoints []Endpoint

	for _, svc := range services.Items {
		serviceEndpoints := d.processService(namespace, &svc)
		endpoints = append(endpoints, serviceEndpoints...)
	}

	return endpoints, nil
}

// listNamespaceServices lists services in a namespace with label selector.
func (d *KubernetesDiscovery) listNamespaceServices(namespace string) (*v1.ServiceList, error) {
	labelSelector := labels.SelectorFromSet(d.config.LabelSelector).String()

	services, err := d.client.CoreV1().Services(namespace).List(d.ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, customerrors.Wrap(err, "failed to list services").
			WithComponent("discovery_k8s")
	}

	return services, nil
}

// processService processes a single Kubernetes service.
func (d *KubernetesDiscovery) processService(namespace string, svc *v1.Service) []Endpoint {
	// Validate service
	if !d.isValidMCPService(svc) {
		return nil
	}

	// Extract MCP configuration
	mcpConfig := d.extractMCPConfig(svc, namespace)
	if mcpConfig == nil {
		return nil
	}

	// Get service endpoints
	return d.getServiceEndpoints(namespace, svc, mcpConfig)
}

// isValidMCPService checks if a service is MCP-enabled.
func (d *KubernetesDiscovery) isValidMCPService(svc *v1.Service) bool {
	return svc.Annotations["mcp.bridge/enabled"] == "true"
}

// mcpServiceConfig holds MCP configuration for a service.
type mcpServiceConfig struct {
	namespace string
	scheme    string
	path      string
	tools     []ToolInfo
	weight    int
}

// extractMCPConfig extracts MCP configuration from service annotations.
func (d *KubernetesDiscovery) extractMCPConfig(svc *v1.Service, k8sNamespace string) *mcpServiceConfig {
	// Get MCP namespace
	mcpNamespace := svc.Annotations["mcp.bridge/namespace"]
	if mcpNamespace == "" {
		d.logger.Warn("Service missing MCP namespace annotation",
			zap.String("service", svc.Name),
			zap.String("namespace", k8sNamespace))

		return nil
	}

	config := &mcpServiceConfig{
		namespace: mcpNamespace,
		scheme:    svc.Annotations["mcp.bridge/protocol"], // http, https, ws, wss
		path:      svc.Annotations["mcp.bridge/path"],     // e.g., /mcp
		weight:    DefaultWeight,                          // Default weight
	}

	// Default to http if no protocol specified
	if config.scheme == "" {
		config.scheme = schemeHTTP
	}

	// Default to / if no path specified
	if config.path == "" {
		config.path = "/"
	}

	// Parse tools
	config.tools = d.parseTools(svc)

	// Parse weight
	if weight := svc.Annotations["mcp.bridge/weight"]; weight != "" {
		if _, err := fmt.Sscanf(weight, "%d", &config.weight); err != nil {
			config.weight = 1
		}
	}

	return config
}

// parseTools parses tools from service annotations.
func (d *KubernetesDiscovery) parseTools(svc *v1.Service) []ToolInfo {
	var tools []ToolInfo
	if toolsJSON := svc.Annotations["mcp.bridge/tools"]; toolsJSON != "" {
		if err := json.Unmarshal([]byte(toolsJSON), &tools); err != nil {
			d.logger.Error("Failed to parse tools annotation",
				zap.String("service", svc.Name),
				zap.Error(err))
		}
	}

	return tools
}

// getServiceEndpoints gets endpoints for a service.
func (d *KubernetesDiscovery) getServiceEndpoints(
	namespace string,
	svc *v1.Service,
	config *mcpServiceConfig,
) []Endpoint {
	// Get Kubernetes endpoints
	endpointList, err := d.client.CoreV1().Endpoints(namespace).Get(d.ctx, svc.Name, metav1.GetOptions{})
	if err != nil {
		d.logger.Error("Failed to get endpoints",
			zap.String("service", svc.Name),
			zap.Error(err))

		return nil
	}

	// Convert to MCP endpoints
	return d.convertToMCPEndpoints(endpointList, svc, namespace, config)
}

// convertToMCPEndpoints converts Kubernetes endpoints to MCP endpoints.
//
//nolint:staticcheck // SA1019: v1.Endpoints is deprecated but still widely used in k8s clusters.
func (d *KubernetesDiscovery) convertToMCPEndpoints(
	endpointList *v1.Endpoints,
	svc *v1.Service,
	namespace string,
	config *mcpServiceConfig,
) []Endpoint {
	var endpoints []Endpoint

	for _, subset := range endpointList.Subsets {
		subsetEndpoints := d.processSubset(&subset, svc, namespace, config)
		endpoints = append(endpoints, subsetEndpoints...)
	}

	return endpoints
}

// processSubset processes a single endpoint subset.
//
//nolint:staticcheck // SA1019: v1.EndpointSubset is deprecated but still widely used in k8s clusters.
func (d *KubernetesDiscovery) processSubset(
	subset *v1.EndpointSubset,
	svc *v1.Service,
	namespace string,
	config *mcpServiceConfig,
) []Endpoint {
	var endpoints []Endpoint

	for _, addr := range subset.Addresses {
		for _, port := range subset.Ports {
			if port.Name != "mcp" {
				continue
			}

			endpoint := d.createEndpoint(addr.IP, int(port.Port), svc.Name, namespace, config)
			endpoints = append(endpoints, endpoint)
		}
	}

	return endpoints
}

// createEndpoint creates a single MCP endpoint.
func (d *KubernetesDiscovery) createEndpoint(
	ip string,
	port int,
	serviceName, namespace string,
	config *mcpServiceConfig,
) Endpoint {
	endpoint := Endpoint{
		Service:   serviceName,
		Namespace: config.namespace,
		Address:   ip,
		Port:      port,
		Scheme:    config.scheme,
		Path:      config.path,
		Weight:    config.weight,
		Metadata: map[string]string{
			"k8s_namespace": namespace,
			"k8s_service":   serviceName,
		},
		Tools: config.tools,
	}
	endpoint.SetHealthy(true)
	return endpoint
}

// watchNamespace starts watching for service changes in a namespace.
func (d *KubernetesDiscovery) watchNamespace(namespace string) error {
	labelSelector := labels.SelectorFromSet(d.config.LabelSelector).String()

	// Watch services
	serviceWatcher, err := d.client.CoreV1().Services(namespace).Watch(d.ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return customerrors.WrapWatchError(d.ctx, err, "services")
	}

	d.watchers = append(d.watchers, serviceWatcher)

	// Watch endpoints
	endpointWatcher, err := d.client.CoreV1().Endpoints(namespace).Watch(d.ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return customerrors.WrapWatchError(d.ctx, err, "endpoints")
	}

	d.watchers = append(d.watchers, endpointWatcher)

	// Handle service events
	d.wg.Add(1)

	go func() {
		defer d.wg.Done()

		d.handleServiceEvents(namespace, serviceWatcher.ResultChan())
	}()

	// Handle endpoint events
	d.wg.Add(1)

	go func() {
		defer d.wg.Done()

		d.handleEndpointEvents(namespace, endpointWatcher.ResultChan())
	}()

	return nil
}

// handleServiceEvents handles service watch events.
func (d *KubernetesDiscovery) handleServiceEvents(namespace string, events <-chan watch.Event) {
	for {
		select {
		case <-d.ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				d.logger.Warn("Service watch channel closed", zap.String("namespace", namespace))

				return
			}

			switch event.Type {
			case watch.Added, watch.Modified, watch.Deleted:
				// Resync the namespace
				endpoints, err := d.discoverNamespaceServices(namespace)
				if err != nil {
					d.logger.Error("Failed to resync namespace",
						zap.String("namespace", namespace),
						zap.Error(err),
					)

					continue
				}

				d.mu.Lock()

				if len(endpoints) > 0 {
					d.endpoints[namespace] = endpoints
				} else {
					delete(d.endpoints, namespace)
				}

				d.mu.Unlock()

				d.logger.Debug("Service discovery updated",
					zap.String("namespace", namespace),
					zap.String("event", string(event.Type)),
					zap.Int("endpoints", len(endpoints)),
				)
			case watch.Bookmark:
				// Bookmarks are just for tracking position, no action needed
				continue
			case watch.Error:
				// Log error events
				d.logger.Error("Watch error event received",
					zap.String("namespace", namespace),
					zap.Any("object", event.Object),
				)

				continue
			}
		}
	}
}

// handleEndpointEvents handles endpoint watch events.
func (d *KubernetesDiscovery) handleEndpointEvents(namespace string, events <-chan watch.Event) {
	for {
		select {
		case <-d.ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				d.logger.Info("Endpoint watch closed", zap.String("namespace", namespace))

				return
			}

			switch event.Type {
			case watch.Added, watch.Modified, watch.Deleted:
				d.logger.Info("Endpoint event detected",
					zap.String("k8s_namespace", namespace),
					zap.String("type", string(event.Type)))

				// Refresh endpoints for this namespace
				endpoints, err := d.discoverNamespaceServices(namespace)
				if err != nil {
					d.logger.Error("Failed to refresh endpoints after event",
						zap.String("k8s_namespace", namespace),
						zap.Error(err))

					continue
				}

				// Track which MCP namespaces were affected
				affectedMCPNamespaces := d.trackAffectedMCPNamespaces(namespace, endpoints)

				// Update internal state and notify callbacks
				d.updateEndpointsAndNotify(namespace, endpoints, affectedMCPNamespaces)
			case watch.Bookmark, watch.Error:
				// Bookmark and Error events don't require action
				continue
			}
		}
	}
}

// trackAffectedMCPNamespaces identifies which MCP namespaces are affected by K8s namespace changes.
func (d *KubernetesDiscovery) trackAffectedMCPNamespaces(
	k8sNamespace string, newEndpoints []Endpoint,
) map[string]bool {
	affectedMCPNamespaces := make(map[string]bool)

	// Collect MCP namespaces before update
	d.mu.RLock()
	for mcpNs, eps := range d.endpoints {
		for _, ep := range eps {
			if k8sNs, ok := ep.Metadata["k8s_namespace"]; ok && k8sNs == k8sNamespace {
				affectedMCPNamespaces[mcpNs] = true
			}
		}
	}
	d.mu.RUnlock()

	// Collect MCP namespaces after update
	for _, ep := range newEndpoints {
		affectedMCPNamespaces[ep.Namespace] = true
	}

	return affectedMCPNamespaces
}

// updateEndpointsAndNotify updates endpoint state and notifies callbacks.
func (d *KubernetesDiscovery) updateEndpointsAndNotify(
	k8sNamespace string, newEndpoints []Endpoint, affectedMCPNamespaces map[string]bool,
) {
	// Update internal state - reorganize all endpoints by MCP namespace
	d.mu.Lock()
	// First remove old endpoints from this K8s namespace
	for mcpNs := range d.endpoints {
		filtered := make([]Endpoint, 0)
		for _, ep := range d.endpoints[mcpNs] {
			if k8sNs, ok := ep.Metadata["k8s_namespace"]; !ok || k8sNs != k8sNamespace {
				filtered = append(filtered, ep)
			}
		}

		if len(filtered) > 0 {
			d.endpoints[mcpNs] = filtered
		} else {
			delete(d.endpoints, mcpNs)
		}
	}

	// Then add new endpoints grouped by MCP namespace
	for _, ep := range newEndpoints {
		d.endpoints[ep.Namespace] = append(d.endpoints[ep.Namespace], ep)
	}
	d.mu.Unlock()

	d.logger.Info("Endpoints refreshed after K8s event",
		zap.String("k8s_namespace", k8sNamespace),
		zap.Strings("affected_mcp_namespaces", getMCPNamespacesList(affectedMCPNamespaces)),
		zap.Int("total_endpoints", len(newEndpoints)))

	// Notify callback for each affected MCP namespace
	d.callbackMu.RLock()
	callback := d.endpointChangeCallback
	d.callbackMu.RUnlock()

	if callback != nil {
		for mcpNs := range affectedMCPNamespaces {
			callback(mcpNs)
		}
	}
}

// getMCPNamespacesList converts a map of MCP namespaces to a sorted slice.
func getMCPNamespacesList(namespaces map[string]bool) []string {
	result := make([]string, 0, len(namespaces))
	for ns := range namespaces {
		result = append(result, ns)
	}

	return result
}

// periodicResync performs periodic full resync.
func (d *KubernetesDiscovery) periodicResync() {
	defer d.wg.Done()

	ticker := time.NewTicker(KubernetesResyncMinutes * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-d.ctx.Done():
			return
		case <-ticker.C:
			d.logger.Debug("Performing periodic resync")
			// Retry resync with exponential backoff
			retryCount := 0
			maxRetries := KubernetesMaxRetries

			for retryCount < maxRetries {
				if err := d.discoverServices(); err != nil {
					retryCount++
					d.logger.Warn("Periodic resync failed, retrying",
						zap.Error(err),
						zap.Int("retry", retryCount),
						zap.Int("max_retries", maxRetries),
					)

					if retryCount < maxRetries {
						// Exponential backoff: 5s, 10s, 20s
						// #nosec G115 - retryCount is bounded
						time.Sleep(time.Duration(BackoffBaseSeconds<<uint(retryCount-1)) * time.Second)

						continue
					}

					// All retries failed
					d.logger.Error("Periodic resync failed after retries", zap.Error(err))
				}
				// Resync succeeded
				break
			}
		}
	}
}

// GetEndpoints returns endpoints for a specific namespace.
func (d *KubernetesDiscovery) GetEndpoints(namespace string) []Endpoint {
	d.mu.RLock()
	defer d.mu.RUnlock()

	endpoints := d.endpoints[namespace]
	// Return a copy to avoid race conditions
	result := make([]Endpoint, len(endpoints))
	copy(result, endpoints)

	return result
}

// GetAllEndpoints returns all discovered endpoints.
func (d *KubernetesDiscovery) GetAllEndpoints() map[string][]Endpoint {
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
func (d *KubernetesDiscovery) ListNamespaces() []string {
	d.mu.RLock()
	defer d.mu.RUnlock()

	namespaces := make([]string, 0, len(d.endpoints))
	for ns := range d.endpoints {
		namespaces = append(namespaces, ns)
	}

	return namespaces
}

// RegisterEndpointChangeCallback registers a callback to be invoked when endpoints change.
func (d *KubernetesDiscovery) RegisterEndpointChangeCallback(callback func(namespace string)) {
	d.callbackMu.Lock()
	defer d.callbackMu.Unlock()
	d.endpointChangeCallback = callback
}
