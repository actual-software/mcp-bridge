package health

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// HealthStatus represents the health status of a component.
type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "healthy"
	HealthStatusUnhealthy HealthStatus = "unhealthy"
	HealthStatusDegraded  HealthStatus = "degraded"
	HealthStatusUnknown   HealthStatus = "unknown"
)

// HealthCheckResult represents the result of a health check.
type HealthCheckResult struct {
	ComponentName string                 `json:"component_name"`
	Status        HealthStatus           `json:"status"`
	Message       string                 `json:"message"`
	Timestamp     time.Time              `json:"timestamp"`
	Duration      time.Duration          `json:"duration"`
	Details       map[string]interface{} `json:"details,omitempty"`
	Error         error                  `json:"error,omitempty"`
}

// HealthChecker defines the interface for component health checks.
type HealthChecker interface {
	// CheckHealth performs a health check and returns the result.
	CheckHealth(ctx context.Context) HealthCheckResult

	// GetComponentName returns the name of the component being checked.
	GetComponentName() string
}

// CompositeHealthChecker aggregates multiple health checkers.
type CompositeHealthChecker struct {
	checkers []HealthChecker
	logger   *zap.Logger
	mu       sync.RWMutex
}

// NewCompositeHealthChecker creates a new composite health checker.
func NewCompositeHealthChecker(logger *zap.Logger) *CompositeHealthChecker {
	return &CompositeHealthChecker{
		checkers: make([]HealthChecker, 0),
		logger:   logger,
	}
}

// AddChecker adds a health checker to the composite.
func (c *CompositeHealthChecker) AddChecker(checker HealthChecker) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.checkers = append(c.checkers, checker)
	c.logger.Info("Added health checker", zap.String("component", checker.GetComponentName()))
}

// RemoveChecker removes a health checker by component name.
func (c *CompositeHealthChecker) RemoveChecker(componentName string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	for i, checker := range c.checkers {
		if checker.GetComponentName() == componentName {
			c.checkers = append(c.checkers[:i], c.checkers[i+1:]...)
			c.logger.Info("Removed health checker", zap.String("component", componentName))

			return true
		}
	}

	return false
}

// CheckHealth performs health checks on all registered components.
func (c *CompositeHealthChecker) CheckHealth(ctx context.Context) []HealthCheckResult {
	c.mu.RLock()
	checkers := make([]HealthChecker, len(c.checkers))
	copy(checkers, c.checkers)
	c.mu.RUnlock()

	results := make([]HealthCheckResult, len(checkers))

	var wg sync.WaitGroup

	// Run health checks concurrently.
	for i, checker := range checkers {
		wg.Add(1)

		go func(index int, hc HealthChecker) {
			defer wg.Done()

			results[index] = hc.CheckHealth(ctx)
		}(i, checker)
	}

	wg.Wait()

	return results
}

// GetOverallHealth returns the overall system health status.
// createComponentDetail creates a detail map for a health check result.
func createComponentDetail(result HealthCheckResult) map[string]interface{} {
	detail := map[string]interface{}{
		"name":      result.ComponentName,
		"status":    string(result.Status),
		"message":   result.Message,
		"timestamp": result.Timestamp,
		"duration":  result.Duration.String(),
	}

	if result.Error != nil {
		detail["error"] = result.Error.Error()
	}

	return detail
}

// determineOverallStatus determines the overall health status based on component counts.
func determineOverallStatus(healthyCount, degradedCount, unhealthyCount int) (HealthStatus, string) {
	if unhealthyCount > 0 {
		return HealthStatusUnhealthy, fmt.Sprintf("%d components unhealthy, %d degraded, %d healthy",
			unhealthyCount, degradedCount, healthyCount)
	}

	if degradedCount > 0 {
		return HealthStatusDegraded, fmt.Sprintf("%d components degraded, %d healthy",
			degradedCount, healthyCount)
	}

	if healthyCount == 0 {
		return HealthStatusUnknown, "No components registered"
	}

	return HealthStatusHealthy, "All components healthy"
}

func (c *CompositeHealthChecker) GetOverallHealth(ctx context.Context) HealthCheckResult {
	results := c.CheckHealth(ctx)

	overallResult := HealthCheckResult{
		ComponentName: "system",
		Status:        HealthStatusHealthy,
		Message:       "All components healthy",
		Timestamp:     time.Now(),
		Details: map[string]interface{}{
			"component_count": len(results),
			"components":      make([]map[string]interface{}, 0, len(results)),
		},
	}

	healthyCount := 0
	degradedCount := 0
	unhealthyCount := 0

	for _, result := range results {
		componentDetail := createComponentDetail(result)

		components, ok := overallResult.Details["components"].([]map[string]interface{})
		if !ok {
			components = make([]map[string]interface{}, 0)
		}

		overallResult.Details["components"] = append(components, componentDetail)

		switch result.Status {
		case HealthStatusHealthy:
			healthyCount++
		case HealthStatusDegraded:
			degradedCount++
		case HealthStatusUnhealthy:
			unhealthyCount++
		case HealthStatusUnknown:
			// Unknown status doesn't affect counts
		}
	}

	overallResult.Details["healthy_count"] = healthyCount
	overallResult.Details["degraded_count"] = degradedCount
	overallResult.Details["unhealthy_count"] = unhealthyCount

	// Determine overall status.
	overallResult.Status, overallResult.Message = determineOverallStatus(healthyCount, degradedCount, unhealthyCount)

	return overallResult
}

// GatewayPoolHealthChecker checks the health of a gateway pool.
type GatewayPoolHealthChecker struct {
	pool interface {
		GetStats() map[string]interface{}
	}
	componentName string
}

// NewGatewayPoolHealthChecker creates a health checker for a gateway pool.
func NewGatewayPoolHealthChecker(pool interface {
	GetStats() map[string]interface{}
}, componentName string,
) *GatewayPoolHealthChecker {
	return &GatewayPoolHealthChecker{
		pool:          pool,
		componentName: componentName,
	}
}

// CheckHealth checks the health of the gateway pool.
func (g *GatewayPoolHealthChecker) CheckHealth(ctx context.Context) HealthCheckResult {
	start := time.Now()

	result := HealthCheckResult{
		ComponentName: g.componentName,
		Timestamp:     start,
	}

	defer func() {
		result.Duration = time.Since(start)
	}()

	stats := g.pool.GetStats()
	totalEndpoints := 0
	healthyEndpoints := 0

	if total, ok := stats["total_endpoints"].(int); ok {
		totalEndpoints = total
	}

	if healthy, ok := stats["healthy_endpoints"].(int); ok {
		healthyEndpoints = healthy
	}

	result.Details = map[string]interface{}{
		"total_endpoints":   totalEndpoints,
		"healthy_endpoints": healthyEndpoints,
		"strategy":          stats["strategy"],
	}

	if endpoints, ok := stats["endpoints"].([]map[string]interface{}); ok {
		result.Details["endpoint_details"] = endpoints
	}

	if totalEndpoints == 0 { 
		result.Status = HealthStatusUnhealthy
		result.Message = "No endpoints configured"
	} else if healthyEndpoints == 0 {
		result.Status = HealthStatusUnhealthy
		result.Message = "No healthy endpoints available"
	} else if healthyEndpoints < totalEndpoints {
		result.Status = HealthStatusDegraded
		result.Message = fmt.Sprintf("%d of %d endpoints healthy", healthyEndpoints, totalEndpoints)
	} else {
		result.Status = HealthStatusHealthy
		result.Message = fmt.Sprintf("All %d endpoints healthy", totalEndpoints)
	}

	return result
}

// GetComponentName returns the component name.
func (g *GatewayPoolHealthChecker) GetComponentName() string {
	return g.componentName
}

// ConnectionManagerHealthChecker checks the health of a connection manager.
type ConnectionManagerHealthChecker struct {
	connMgr interface {
		GetState() interface{}
		GetRetryCount() uint64
	}
	componentName string
}

// NewConnectionManagerHealthChecker creates a health checker for a connection manager.
func NewConnectionManagerHealthChecker(connMgr interface {
	GetState() interface{}
	GetRetryCount() uint64
}, componentName string,
) *ConnectionManagerHealthChecker {
	return &ConnectionManagerHealthChecker{
		connMgr:       connMgr,
		componentName: componentName,
	}
}

// CheckHealth checks the health of the connection manager.
func (c *ConnectionManagerHealthChecker) CheckHealth(ctx context.Context) HealthCheckResult {
	start := time.Now()

	result := HealthCheckResult{
		ComponentName: c.componentName,
		Timestamp:     start,
	}

	defer func() {
		result.Duration = time.Since(start)
	}()

	state := c.connMgr.GetState()
	retryCount := c.connMgr.GetRetryCount()

	result.Details = map[string]interface{}{
		"connection_state": fmt.Sprintf("%v", state),
		"retry_count":      retryCount,
	}

	stateStr := fmt.Sprintf("%v", state)
	switch stateStr {
	case "CONNECTED":
		result.Status = HealthStatusHealthy
		result.Message = "Connection established"
	case "CONNECTING", "RECONNECTING":
		result.Status = HealthStatusDegraded
		result.Message = fmt.Sprintf("Connection in %s state", stateStr)
	case "ERROR":
		result.Status = HealthStatusUnhealthy
		result.Message = "Connection in error state"
	case "SHUTDOWN":
		result.Status = HealthStatusUnhealthy
		result.Message = "Connection manager shutdown"
	default:
		result.Status = HealthStatusUnknown
		result.Message = "Unknown connection state: " + stateStr
	}

	// Consider high retry count as degraded.
	if retryCount > HighRetryThreshold {
		result.Status = HealthStatusDegraded
		result.Message += fmt.Sprintf(" (high retry count: %d)", retryCount)
	}

	return result
}

// GetComponentName returns the component name.
func (c *ConnectionManagerHealthChecker) GetComponentName() string {
	return c.componentName
}

// MetricsHealthChecker checks the health of metrics collection.
type MetricsHealthChecker struct {
	metrics interface {
		GetStats() map[string]interface{}
	}
	componentName string
}

// NewMetricsHealthChecker creates a health checker for metrics.
func NewMetricsHealthChecker(metrics interface {
	GetStats() map[string]interface{}
}, componentName string,
) *MetricsHealthChecker {
	return &MetricsHealthChecker{
		metrics:       metrics,
		componentName: componentName,
	}
}

// CheckHealth checks the health of metrics collection.
func (m *MetricsHealthChecker) CheckHealth(ctx context.Context) HealthCheckResult {
	start := time.Now()

	result := HealthCheckResult{
		ComponentName: m.componentName,
		Timestamp:     start,
	}

	defer func() {
		result.Duration = time.Since(start)
	}()

	stats := m.metrics.GetStats()
	result.Details = stats

	// Check if metrics are being collected.
	if totalRequests, ok := stats["requests_total"].(uint64); ok && totalRequests > 0 {
		result.Status = HealthStatusHealthy
		result.Message = fmt.Sprintf("Metrics collecting (%d requests processed)", totalRequests)
	} else {
		result.Status = HealthStatusDegraded
		result.Message = "No metrics data collected yet"
	}

	// Check error rate.
	if totalRequests, ok := stats["requests_total"].(uint64); ok {
		if totalErrors, ok := stats["errors_total"].(uint64); ok && totalRequests > 0 {
			errorRate := float64(totalErrors) / float64(totalRequests) * PercentageMultiplier
			result.Details["error_rate_percent"] = errorRate

			if errorRate > HighErrorRateThreshold { // More than 10% error rate
				result.Status = HealthStatusDegraded
				result.Message += fmt.Sprintf(" (high error rate: %.1f%%)", errorRate)
			}
		}
	}

	return result
}

// GetComponentName returns the component name.
func (m *MetricsHealthChecker) GetComponentName() string {
	return m.componentName
}

// CustomHealthChecker allows for custom health check functions.
type CustomHealthChecker struct {
	componentName string
	checkFunc     func(ctx context.Context) HealthCheckResult
}

// NewCustomHealthChecker creates a custom health checker.
func NewCustomHealthChecker(
	componentName string,
	checkFunc func(ctx context.Context) HealthCheckResult,
) *CustomHealthChecker {
	return &CustomHealthChecker{
		componentName: componentName,
		checkFunc:     checkFunc,
	}
}

// CheckHealth executes the custom health check function.
func (c *CustomHealthChecker) CheckHealth(ctx context.Context) HealthCheckResult {
	return c.checkFunc(ctx)
}

// GetComponentName returns the component name.
func (c *CustomHealthChecker) GetComponentName() string {
	return c.componentName
}
