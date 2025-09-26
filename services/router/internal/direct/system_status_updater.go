package direct

import (
	"fmt"
	"math"
	"runtime"
	"time"
)

// SystemStatusUpdater handles system status updates.
type SystemStatusUpdater struct {
	monitor *StatusMonitor
}

// CreateSystemStatusUpdater creates a new system status updater.
func CreateSystemStatusUpdater(monitor *StatusMonitor) *SystemStatusUpdater {
	return &SystemStatusUpdater{
		monitor: monitor,
	}
}

// UpdateStatus updates the system status.
func (u *SystemStatusUpdater) UpdateStatus() {
	memStats := u.collectMemoryStats()
	managerMetrics := u.monitor.observability.GetManagerMetrics()
	clientMetrics := u.monitor.observability.GetAllClientMetrics()

	healthScore := u.monitor.calculateHealthScore(managerMetrics, clientMetrics)
	overallHealth := u.determineOverallHealth(float64(healthScore))

	longestRunning, mostActive := u.findNotableClients(clientMetrics)
	problematicClients := u.identifyProblematicClients(clientMetrics)

	u.monitor.systemStatus = u.buildSystemStatus(
		memStats,
		managerMetrics,
		clientMetrics,
		healthScore,
		overallHealth,
		longestRunning,
		mostActive,
		problematicClients,
	)
}

func (u *SystemStatusUpdater) collectMemoryStats() runtime.MemStats {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	return m
}

func (u *SystemStatusUpdater) determineOverallHealth(healthScore float64) string {
	const (
		excellentThreshold = 90.0
		goodThreshold      = 70.0
		warningThreshold   = 50.0
	)

	switch {
	case healthScore >= excellentThreshold:
		return "excellent"
	case healthScore >= goodThreshold:
		return "good"
	case healthScore >= warningThreshold:
		return HealthStatusWarning
	default:
		return HealthStatusCritical
	}
}

func (u *SystemStatusUpdater) findNotableClients(
	clientMetrics map[string]DetailedClientMetrics,
) (ClientBasicInfo, ClientBasicInfo) {
	var (
		longestRunning, mostActive ClientBasicInfo
		maxAge                     time.Duration
		maxRequests                uint64
	)

	for name, metrics := range clientMetrics {
		if client := u.checkLongestRunning(name, metrics, &maxAge); client.Name != "" {
			longestRunning = client
		}

		if client := u.checkMostActive(name, metrics, &maxRequests); client.Name != "" {
			mostActive = client
		}
	}

	return longestRunning, mostActive
}

func (u *SystemStatusUpdater) checkLongestRunning(
	name string,
	metrics DetailedClientMetrics,
	maxAge *time.Duration,
) ClientBasicInfo {
	age := time.Since(metrics.ConnectionTime)
	if age > *maxAge {
		*maxAge = age

		return ClientBasicInfo{
			Name:        name,
			Protocol:    metrics.Protocol,
			URL:         metrics.ServerURL,
			State:       metrics.ConnectionState,
			MetricValue: age,
		}
	}

	return ClientBasicInfo{}
}

func (u *SystemStatusUpdater) checkMostActive(
	name string,
	metrics DetailedClientMetrics,
	maxRequests *uint64,
) ClientBasicInfo {
	if metrics.RequestCount > *maxRequests {
		*maxRequests = metrics.RequestCount

		return ClientBasicInfo{
			Name:        name,
			Protocol:    metrics.Protocol,
			URL:         metrics.ServerURL,
			State:       metrics.ConnectionState,
			MetricValue: metrics.RequestCount,
		}
	}

	return ClientBasicInfo{}
}

func (u *SystemStatusUpdater) identifyProblematicClients(
	clientMetrics map[string]DetailedClientMetrics,
) []ClientProblem {
	var problematicClients []ClientProblem

	for name, metrics := range clientMetrics {
		if problem := u.checkConsecutiveFailures(name, metrics); problem.ClientName != "" {
			problematicClients = append(problematicClients, problem)
		}

		if problem := u.checkErrorRate(name, metrics); problem.ClientName != "" {
			problematicClients = append(problematicClients, problem)
		}
	}

	return problematicClients
}

func (u *SystemStatusUpdater) checkConsecutiveFailures(name string, metrics DetailedClientMetrics) ClientProblem {
	if metrics.ConsecutiveFailures == 0 {
		return ClientProblem{}
	}

	severity := "warning"
	if metrics.ConsecutiveFailures > ConsecutiveFailuresCriticalThreshold {
		severity = "critical"
	}

	return ClientProblem{
		ClientName:    name,
		Protocol:      metrics.Protocol,
		ProblemType:   "consecutive_failures",
		Description:   fmt.Sprintf("Client has %d consecutive failures", metrics.ConsecutiveFailures),
		Severity:      severity,
		FirstObserved: metrics.LastErrorTime,
		LastObserved:  metrics.LastErrorTime,
		Occurrences:   int(metrics.ConsecutiveFailures),
	}
}

func (u *SystemStatusUpdater) checkErrorRate(name string, metrics DetailedClientMetrics) ClientProblem {
	if metrics.ErrorRate <= ErrorRateWarningThreshold {
		return ClientProblem{}
	}

	severity := "warning"
	if metrics.ErrorRate > ErrorRateCriticalThreshold {
		severity = "critical"
	}

	return ClientProblem{
		ClientName:    name,
		Protocol:      metrics.Protocol,
		ProblemType:   "high_error_rate",
		Description:   fmt.Sprintf("Client has %.2f%% error rate", metrics.ErrorRate*PercentageBase),
		Severity:      severity,
		FirstObserved: time.Now(), // We don't track when this started
		LastObserved:  time.Now(),
		Occurrences:   1,
	}
}

func (u *SystemStatusUpdater) buildSystemStatus(
	memStats runtime.MemStats,
	managerMetrics EnhancedManagerMetrics,
	clientMetrics map[string]DetailedClientMetrics,
	healthScore int,
	overallHealth string,
	longestRunning ClientBasicInfo,
	mostActive ClientBasicInfo,
	problematicClients []ClientProblem,
) *SystemStatus {
	return &SystemStatus{
		Version:   "1.0.0-dev",
		BuildTime: "development",
		StartTime: u.monitor.startTime,
		Uptime:    time.Since(u.monitor.startTime),

		ManagerStatus: u.buildManagerStatus(),
		ClientsOverview: u.buildClientsOverview(
			clientMetrics,
			managerMetrics,
			longestRunning,
			mostActive,
			problematicClients,
		),
		SystemResources: u.buildSystemResources(memStats, clientMetrics, managerMetrics),
		ConfigSummary:   u.buildConfigSummary(),
		HealthIndicators: u.buildHealthIndicators(
			overallHealth,
			healthScore,
			managerMetrics,
			clientMetrics,
		),
	}
}

func (u *SystemStatusUpdater) buildManagerStatus() ManagerStatus {
	return ManagerStatus{
		Running: u.monitor.manager != nil,
		ComponentsHealthy: map[string]bool{
			"observability":    u.monitor.observability != nil,
			"connection_pool":  true,
			"adaptive_timeout": true,
			"adaptive_retry":   true,
		},
		BackgroundTasks: map[string]TaskStatus{
			"metrics_collection": {
				Name:    "metrics_collection",
				Running: true,
				LastRun: time.Now(),
			},
			"health_monitoring": {
				Name:    "health_monitoring",
				Running: true,
				LastRun: time.Now(),
			},
		},
	}
}

func (u *SystemStatusUpdater) buildClientsOverview(
	clientMetrics map[string]DetailedClientMetrics,
	managerMetrics EnhancedManagerMetrics,
	longestRunning ClientBasicInfo,
	mostActive ClientBasicInfo,
	problematicClients []ClientProblem,
) ClientsOverview {
	return ClientsOverview{
		Total:              len(clientMetrics),
		ByProtocol:         managerMetrics.ClientsByProtocol,
		ByState:            managerMetrics.ClientsByState,
		Healthy:            managerMetrics.TotalClients - len(problematicClients),
		Unhealthy:          len(problematicClients),
		RecentlyActive:     u.monitor.countRecentlyActiveClients(clientMetrics),
		LongestRunning:     longestRunning,
		MostActive:         mostActive,
		ProblematicClients: problematicClients,
	}
}

func (u *SystemStatusUpdater) buildSystemResources(
	memStats runtime.MemStats,
	clientMetrics map[string]DetailedClientMetrics,
	managerMetrics EnhancedManagerMetrics,
) SystemResources {
	return SystemResources{
		CPU: CPUInfo{
			UsagePercent: 0,
			CoreCount:    runtime.NumCPU(),
		},
		Memory: MemoryInfo{
			AllocBytes:     memStats.Alloc,
			SysBytes:       memStats.Sys,
			HeapAllocBytes: memStats.HeapAlloc,
			HeapSysBytes:   memStats.HeapSys,
			HeapObjects:    memStats.HeapObjects,
			StackBytes:     memStats.StackSys,
			GCStats: GCInfo{
				NumGC: memStats.NumGC,
				PauseTotal: func() time.Duration {
					if memStats.PauseTotalNs <= math.MaxInt64 {
						return time.Duration(memStats.PauseTotalNs)
					}

					return time.Duration(math.MaxInt64)
				}(),
				NextGC: memStats.NextGC,
				LastGC: func() time.Time {
					if memStats.LastGC <= math.MaxInt64 {
						return time.Unix(0, int64(memStats.LastGC))
					}

					return time.Unix(0, math.MaxInt64)
				}(),
			},
		},
		Network: NetworkInfo{
			ConnectionsTotal:   len(clientMetrics),
			ConnectionsByState: managerMetrics.ClientsByState,
		},
		FileDescriptors: FileDescriptorInfo{
			Open: 0,
		},
		Goroutines: runtime.NumGoroutine(),
	}
}

func (u *SystemStatusUpdater) buildConfigSummary() ConfigurationSummary {
	return ConfigurationSummary{
		DefaultTimeout:       defaultTimeoutSeconds * time.Second,
		MaxConnections:       PercentageBase,
		HealthCheckEnabled:   true,
		AutoDetectionEnabled: true,
		FallbackEnabled:      true,
		ProtocolsSupported:   []string{"stdio", "websocket", "http", "sse"},
		AdaptiveMechanisms:   []string{"timeout", "retry"},
	}
}

func (u *SystemStatusUpdater) buildHealthIndicators(
	overallHealth string,
	healthScore int,
	managerMetrics EnhancedManagerMetrics,
	clientMetrics map[string]DetailedClientMetrics,
) HealthIndicators {
	return HealthIndicators{
		OverallHealth:  overallHealth,
		HealthScore:    healthScore,
		CriticalAlerts: u.monitor.getAlertsBySeverity("critical"),
		Warnings:       u.monitor.getAlertsBySeverity("warning"),
		PerformanceMetrics: PerformanceIndicators{
			ThroughputRPS:             managerMetrics.RequestsPerSecond,
			ErrorRate:                 managerMetrics.ErrorRate,
			SuccessRate:               1.0 - managerMetrics.ErrorRate,
			RequestLatencyP50:         u.monitor.calculateAverageLatency(clientMetrics),
			RequestLatencyP95:         time.Duration(0),
			RequestLatencyP99:         time.Duration(0),
			ConnectionPoolUtilization: 0,
		},
	}
}
