package direct

import (
	"fmt"
	"time"
)

// HealthCheckRunner performs health checks for the status monitor.
type HealthCheckRunner struct {
	monitor *StatusMonitor
}

// CreateHealthCheckRunner creates a new health check runner.
func CreateHealthCheckRunner(monitor *StatusMonitor) *HealthCheckRunner {
	return &HealthCheckRunner{
		monitor: monitor,
	}
}

// ExecuteHealthChecks runs all health checks.
func (r *HealthCheckRunner) ExecuteHealthChecks() {
	managerMetrics := r.monitor.observability.GetManagerMetrics()
	clientMetrics := r.monitor.observability.GetAllClientMetrics()

	r.checkErrorRate(managerMetrics)
	r.checkMemoryUsage(managerMetrics)
	r.checkClientHealth(clientMetrics)
	r.checkGoroutineCount(managerMetrics)
}

func (r *HealthCheckRunner) checkErrorRate(metrics EnhancedManagerMetrics) {
	const (
		warningThreshold  = ErrorRateWarningThreshold
		criticalThreshold = ErrorRateCriticalThreshold
	)

	if metrics.ErrorRate > warningThreshold {
		r.addErrorRateAlert(metrics.ErrorRate, criticalThreshold)
	} else {
		r.monitor.ResolveHealthAlert("high-error-rate")
	}
}

func (r *HealthCheckRunner) addErrorRateAlert(errorRate float64, criticalThreshold float64) {
	alert := HealthAlert{
		ID:        "high-error-rate",
		Type:      "error_rate",
		Severity:  HealthStatusWarning,
		Message:   fmt.Sprintf("High error rate detected: %.2f%%", errorRate*PercentageBase),
		Component: "manager",
		LastSeen:  time.Now(),
	}

	if errorRate > criticalThreshold {
		alert.Severity = HealthStatusCritical
	}

	r.monitor.AddHealthAlert(alert)
}

func (r *HealthCheckRunner) checkMemoryUsage(metrics EnhancedManagerMetrics) {
	const (
		warningThreshold  = 512 * KilobyteFactor * KilobyteFactor            // 512MB
		criticalThreshold = KilobyteFactor * KilobyteFactor * KilobyteFactor // 1GB
	)

	if metrics.MemoryUsage > warningThreshold {
		r.addMemoryAlert(metrics.MemoryUsage, criticalThreshold)
	} else {
		r.monitor.ResolveHealthAlert("high-memory-usage")
	}
}

func (r *HealthCheckRunner) addMemoryAlert(memoryUsage uint64, criticalThreshold uint64) {
	alert := HealthAlert{
		ID:        "high-memory-usage",
		Type:      "memory",
		Severity:  HealthStatusWarning,
		Message:   fmt.Sprintf("High memory usage: %d MB", memoryUsage/(KilobyteFactor*KilobyteFactor)),
		Component: "system",
		LastSeen:  time.Now(),
	}

	if memoryUsage > criticalThreshold {
		alert.Severity = HealthStatusCritical
	}

	r.monitor.AddHealthAlert(alert)
}

func (r *HealthCheckRunner) checkClientHealth(clientMetrics map[string]DetailedClientMetrics) {
	unhealthyCount := r.countUnhealthyClients(clientMetrics)

	if unhealthyCount > 0 {
		r.addClientHealthAlert(unhealthyCount, len(clientMetrics))
	} else {
		r.monitor.ResolveHealthAlert("unhealthy-clients")
	}
}

func (r *HealthCheckRunner) countUnhealthyClients(clientMetrics map[string]DetailedClientMetrics) int {
	count := 0

	for _, metrics := range clientMetrics {
		if !metrics.IsHealthy {
			count++
		}
	}

	return count
}

func (r *HealthCheckRunner) addClientHealthAlert(unhealthyCount, totalCount int) {
	alert := HealthAlert{
		ID:        "unhealthy-clients",
		Type:      "client_health",
		Severity:  HealthStatusWarning,
		Message:   fmt.Sprintf("%d unhealthy clients detected", unhealthyCount),
		Component: "clients",
		LastSeen:  time.Now(),
	}

	// More than half unhealthy is critical.
	if unhealthyCount > totalCount/DivisionFactorTwo {
		alert.Severity = HealthStatusCritical
	}

	r.monitor.AddHealthAlert(alert)
}

func (r *HealthCheckRunner) checkGoroutineCount(metrics EnhancedManagerMetrics) {
	const (
		warningThreshold  = 1000
		criticalThreshold = 5000
	)

	if metrics.GoroutineCount > warningThreshold {
		r.addGoroutineAlert(metrics.GoroutineCount, criticalThreshold)
	} else {
		r.monitor.ResolveHealthAlert("high-goroutine-count")
	}
}

func (r *HealthCheckRunner) addGoroutineAlert(count int, criticalThreshold int) {
	alert := HealthAlert{
		ID:        "high-goroutine-count",
		Type:      "goroutines",
		Severity:  HealthStatusWarning,
		Message:   fmt.Sprintf("High goroutine count: %d", count),
		Component: "system",
		LastSeen:  time.Now(),
	}

	if count > criticalThreshold {
		alert.Severity = HealthStatusCritical
	}

	r.monitor.AddHealthAlert(alert)
}
