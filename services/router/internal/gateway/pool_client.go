package gateway

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/poiley/mcp-bridge/services/router/internal/config"
	"github.com/poiley/mcp-bridge/services/router/pkg/mcp"
)

// PoolClient implements GatewayClient interface using a gateway pool for load balancing.
type PoolClient struct {
	pool            *GatewayPool
	logger          *zap.Logger
	config          config.LoadBalancerConfig
	namespaceRouter *NamespaceRouter

	// Current active endpoint for connection affinity.
	currentEndpoint *GatewayEndpointWrapper
}

// NewPoolClient creates a new pool-based gateway client.
func NewPoolClient(cfg *config.Config, logger *zap.Logger) (*PoolClient, error) {
	pool, err := NewGatewayPool(cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create gateway pool: %w", err)
	}

	// Initialize namespace router.
	nsConfig := cfg.GetNamespaceRoutingConfig()

	var namespaceRouter *NamespaceRouter

	if nsConfig.Enabled {
		// Convert config types.
		gatewayNsConfig := NamespaceRoutingConfig{
			Enabled: nsConfig.Enabled,
			Rules:   make([]NamespaceRoutingRule, len(nsConfig.Rules)),
		}
		for i, rule := range nsConfig.Rules {
			gatewayNsConfig.Rules[i] = NamespaceRoutingRule{
				Pattern:     rule.Pattern,
				Tags:        rule.Tags,
				Priority:    rule.Priority,
				Description: rule.Description,
			}
		}

		namespaceRouter, err = NewNamespaceRouter(gatewayNsConfig, logger)
		if err != nil {
			return nil, fmt.Errorf("failed to create namespace router: %w", err)
		}

		logger.Info("Namespace routing enabled", zap.Int("rules", len(gatewayNsConfig.Rules)))
	}

	return &PoolClient{
		pool:            pool,
		logger:          logger,
		config:          cfg.GetLoadBalancerConfig(),
		namespaceRouter: namespaceRouter,
	}, nil
}

// Connect establishes connection using the selected endpoint.
func (pc *PoolClient) Connect(ctx context.Context) error {
	endpoint, err := pc.pool.SelectEndpoint()
	if err != nil {
		// If no healthy endpoints available, try the retry selection.
		endpoint, err = pc.pool.SelectEndpointForRetry()
		if err != nil {
			return fmt.Errorf("failed to select endpoint: %w", err)
		}
	}

	// Try to connect to the selected endpoint.
	err = pc.connectToEndpoint(ctx, endpoint)
	if err != nil {
		// Mark endpoint as unhealthy and try another.
		endpoint.UpdateHealth(false, err)
		pc.logger.Warn("Failed to connect to endpoint, trying another",
			zap.String("url", endpoint.Config.URL),
			zap.Error(err))

		// Retry with different endpoint.
		return pc.connectWithRetry(ctx)
	}

	pc.currentEndpoint = endpoint
	endpoint.IncrementConnections()
	endpoint.UpdateHealth(true, nil)

	pc.logger.Info("Connected to gateway endpoint",
		zap.String("url", endpoint.Config.URL),
		zap.String("strategy", string(pc.pool.strategy)))

	return nil
}

// connectWithRetry attempts to connect to different endpoints with retries.
func (pc *PoolClient) connectWithRetry(ctx context.Context) error {
	maxRetries := pc.config.RetryCount
	if maxRetries <= 0 {
		maxRetries = 3
	}

	for attempt := 0; attempt < maxRetries; attempt++ {
		endpoint, err := pc.pool.SelectEndpointForRetry()
		if err != nil {
			if attempt == maxRetries-1 {
				return fmt.Errorf("no endpoints available after %d attempts: %w", maxRetries, err)
			}

			time.Sleep(time.Duration(attempt+1) * time.Second) // Exponential backoff

			continue
		}

		err = pc.connectToEndpoint(ctx, endpoint)
		if err == nil {
			pc.currentEndpoint = endpoint
			endpoint.IncrementConnections()
			endpoint.UpdateHealth(true, nil)

			pc.logger.Info("Connected to gateway endpoint after retry",
				zap.String("url", endpoint.Config.URL),
				zap.Int("attempt", attempt+1))

			return nil
		}

		endpoint.UpdateHealth(false, err)
		pc.logger.Warn("Connection attempt failed",
			zap.String("url", endpoint.Config.URL),
			zap.Int("attempt", attempt+1),
			zap.Error(err))

		// Wait before next retry.
		if attempt < maxRetries-1 {
			time.Sleep(time.Duration(attempt+1) * time.Second)
		}
	}

	return fmt.Errorf("failed to connect after %d attempts", maxRetries)
}

// connectToEndpoint connects to a specific endpoint.
func (pc *PoolClient) connectToEndpoint(ctx context.Context, endpoint *GatewayEndpointWrapper) error {
	return endpoint.Client.Connect(ctx)
}

// SendRequest sends a request through the current endpoint.
func (pc *PoolClient) SendRequest(req *mcp.Request) error {
	// Use namespace routing if enabled.
	if pc.namespaceRouter != nil && pc.namespaceRouter.config.Enabled {
		pc.handleNamespaceRouting(req)
	}

	if pc.currentEndpoint == nil {
		return errors.New("not connected to any endpoint")
	}

	err := pc.currentEndpoint.Client.SendRequest(req)
	if err != nil {
		pc.currentEndpoint.UpdateHealth(false, err)
		pc.logger.Error("Failed to send request",
			zap.String("url", pc.currentEndpoint.Config.URL),
			zap.String("method", req.Method),
			zap.String("request_id", fmt.Sprintf("%v", req.ID)),
			zap.Error(err))

		return fmt.Errorf("failed to send request: %w", err)
	}

	return nil
}

// ReceiveResponse receives a response from the current endpoint.
func (pc *PoolClient) ReceiveResponse() (*mcp.Response, error) {
	if pc.currentEndpoint == nil {
		return nil, errors.New("not connected to any endpoint")
	}

	resp, err := pc.currentEndpoint.Client.ReceiveResponse()
	if err != nil {
		pc.currentEndpoint.UpdateHealth(false, err)
		pc.logger.Error("Failed to receive response",
			zap.String("url", pc.currentEndpoint.Config.URL),
			zap.Error(err))

		return nil, fmt.Errorf("failed to receive response: %w", err)
	}

	return resp, nil
}

// SendPing sends a ping through the current endpoint.
func (pc *PoolClient) SendPing() error {
	if pc.currentEndpoint == nil {
		return errors.New("not connected to any endpoint")
	}

	err := pc.currentEndpoint.Client.SendPing()
	if err != nil {
		pc.currentEndpoint.UpdateHealth(false, err)
		pc.logger.Warn("Ping failed",
			zap.String("url", pc.currentEndpoint.Config.URL),
			zap.Error(err))

		return fmt.Errorf("ping failed: %w", err)
	}

	return nil
}

// IsConnected returns true if connected to any endpoint.
func (pc *PoolClient) IsConnected() bool {
	if pc.currentEndpoint == nil {
		return false
	}

	connected := pc.currentEndpoint.Client.IsConnected()
	if !connected {
		pc.currentEndpoint.UpdateHealth(false, errors.New("connection lost"))
	}

	return connected
}

// Close closes the current connection and cleans up.
func (pc *PoolClient) Close() error {
	var err error

	if pc.currentEndpoint != nil {
		err = pc.currentEndpoint.Client.Close()
		pc.currentEndpoint.DecrementConnections()
		pc.currentEndpoint = nil
	}

	if pc.pool != nil {
		poolErr := pc.pool.Close()
		if err == nil {
			err = poolErr
		}
	}

	return err
}

// GetPoolStats returns statistics about the underlying pool.
func (pc *PoolClient) GetPoolStats() map[string]interface{} {
	if pc.pool == nil {
		return map[string]interface{}{"error": "pool not initialized"}
	}

	return pc.pool.GetStats()
}

// SelectEndpointByTags selects an endpoint that matches specific tags for namespace routing.
func (pc *PoolClient) SelectEndpointByTags(tags []string) error {
	endpoints, err := pc.pool.GetEndpointByTags(tags)
	if err != nil {
		return fmt.Errorf("failed to find endpoint with tags %v: %w", tags, err)
	}

	// For now, select the first matching endpoint.
	// In the future, we could apply load balancing among matching endpoints.
	selectedEndpoint := endpoints[0]

	// Close current connection if different endpoint.
	if pc.currentEndpoint != nil && pc.currentEndpoint != selectedEndpoint {
		if pc.currentEndpoint.Client.IsConnected() {
			_ = pc.currentEndpoint.Client.Close()
			pc.currentEndpoint.DecrementConnections()
		}
	}

	// Connect to the selected endpoint if not already connected.
	if !selectedEndpoint.Client.IsConnected() {
		ctx, cancel := context.WithTimeout(context.Background(), defaultTimeoutSeconds*time.Second)
		defer cancel()

		err = pc.connectToEndpoint(ctx, selectedEndpoint)
		if err != nil {
			selectedEndpoint.UpdateHealth(false, err)

			return fmt.Errorf("failed to connect to endpoint with tags %v: %w", tags, err)
		}
	}

	pc.currentEndpoint = selectedEndpoint
	selectedEndpoint.IncrementConnections()
	selectedEndpoint.UpdateHealth(true, nil)

	pc.logger.Info("Selected endpoint by tags",
		zap.Strings("tags", tags),
		zap.String("url", selectedEndpoint.Config.URL))

	return nil
}

// ForceReconnect forces a reconnection using the load balancing strategy.
func (pc *PoolClient) ForceReconnect(ctx context.Context) error {
	// Close current connection.
	if pc.currentEndpoint != nil {
		_ = pc.currentEndpoint.Client.Close()
		pc.currentEndpoint.DecrementConnections()
		pc.currentEndpoint.UpdateHealth(false, errors.New("forced reconnection"))
		pc.currentEndpoint = nil
	}

	// Reconnect using standard logic.
	return pc.Connect(ctx)
}

func (pc *PoolClient) handleNamespaceRouting(req *mcp.Request) {
	tags, err := pc.namespaceRouter.RouteRequest(req)
	if err != nil {
		pc.logger.Warn("Namespace routing failed, using current endpoint",
			zap.String("method", req.Method),
			zap.Error(err))

		return
	}

	// Try to select endpoint by tags.
	err = pc.SelectEndpointByTags(tags)
	if err != nil {
		pc.logger.Warn("Failed to select endpoint by tags, using current endpoint",
			zap.Strings("tags", tags),
			zap.Error(err))

		return
	}

	pc.logger.Debug("Request routed by namespace",
		zap.String("method", req.Method),
		zap.Strings("tags", tags),
		zap.String("endpoint", pc.currentEndpoint.Config.URL))
}
