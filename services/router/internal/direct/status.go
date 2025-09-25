package direct

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/poiley/mcp-bridge/services/router/internal/constants"
	"go.uber.org/zap"
)

// Status threshold constants.
const (
	ThousandMillisThreshold = 1000
	FiveThousandMillisThreshold = 5000
)


// DirectClientStatus contains detailed status information for a direct client.
type DirectClientStatus struct {
	// Basic identification.
	Name     string          `json:"name"`
	URL      string          `json:"url"`
	Protocol string          `json:"protocol"`
	State    ConnectionState `json:"state"`

	// Connection information.
	LastConnected   time.Time     `json:"last_connected"`
	LastHealthCheck time.Time     `json:"last_health_check"`
	ConnectionAge   time.Duration `json:"connection_age"`

	// Performance metrics.
	Metrics DetailedClientMetrics `json:"metrics"`

	// Configuration details.
	Configuration map[string]interface{} `json:"configuration"`

	// Runtime information.
	RuntimeInfo ClientRuntimeInfo `json:"runtime_info"`
}

// ClientRuntimeInfo contains runtime information about a client.
type ClientRuntimeInfo struct {
	PID           int               `json:"pid,omitempty"`            // For stdio clients
	ProcessName   string            `json:"process_name,omitempty"`   // For stdio clients
	LocalAddress  string            `json:"local_address,omitempty"`  // For network clients
	RemoteAddress string            `json:"remote_address,omitempty"` // For network clients
	ConnectionID  string            `json:"connection_id"`
	UserAgent     string            `json:"user_agent,omitempty"`
	Headers       map[string]string `json:"headers,omitempty"`
	TLSInfo       TLSConnectionInfo `json:"tls_info,omitempty"`
	ResourceUsage ResourceUsage     `json:"resource_usage"`
}

// TLSConnectionInfo contains TLS connection details.
type TLSConnectionInfo struct {
	Enabled     bool      `json:"enabled"`
	Version     string    `json:"version,omitempty"`
	CipherSuite string    `json:"cipher_suite,omitempty"`
	Certificate string    `json:"certificate,omitempty"`
	ValidFrom   time.Time `json:"valid_from,omitempty"`
	ValidUntil  time.Time `json:"valid_until,omitempty"`
}

// ResourceUsage tracks resource consumption for a client.
type ResourceUsage struct {
	MemoryBytes        uint64        `json:"memory_bytes"`
	CPUTime            time.Duration `json:"cpu_time"`
	OpenFiles          int           `json:"open_files"`
	NetworkConnections int           `json:"network_connections"`
	LastUpdated        time.Time     `json:"last_updated"`
}

// SystemStatus represents overall system health and status.
type SystemStatus struct {
	// Basic system info.
	Version   string        `json:"version"`
	BuildTime string        `json:"build_time"`
	StartTime time.Time     `json:"start_time"`
	Uptime    time.Duration `json:"uptime"`

	// Manager status.
	ManagerStatus ManagerStatus `json:"manager_status"`

	// Client status overview.
	ClientsOverview ClientsOverview `json:"clients_overview"`

	// System resources.
	SystemResources SystemResources `json:"system_resources"`

	// Configuration summary.
	ConfigSummary ConfigurationSummary `json:"config_summary"`

	// Health indicators.
	HealthIndicators HealthIndicators `json:"health_indicators"`
}

// ManagerStatus represents the status of the DirectClientManager.
type ManagerStatus struct {
	Running            bool                  `json:"running"`
	ComponentsHealthy  map[string]bool       `json:"components_healthy"`
	LastError          string                `json:"last_error,omitempty"`
	LastErrorTime      time.Time             `json:"last_error_time,omitempty"`
	BackgroundTasks    map[string]TaskStatus `json:"background_tasks"`
	Configuration      DirectConfig          `json:"configuration"`
	AdaptiveMechanisms AdaptiveStatus        `json:"adaptive_mechanisms"`
}

// TaskStatus represents the status of a background task.
type TaskStatus struct {
	Name           string        `json:"name"`
	Running        bool          `json:"running"`
	LastRun        time.Time     `json:"last_run"`
	RunCount       uint64        `json:"run_count"`
	FailureCount   uint64        `json:"failure_count"`
	AverageRuntime time.Duration `json:"average_runtime"`
	LastError      string        `json:"last_error,omitempty"`
}

// AdaptiveStatus represents the status of adaptive mechanisms.
type AdaptiveStatus struct {
	TimeoutMechanism AdaptiveMechanismStatus `json:"timeout_mechanism"`
	RetryMechanism   AdaptiveMechanismStatus `json:"retry_mechanism"`
}

// AdaptiveMechanismStatus represents the status of an adaptive mechanism.
type AdaptiveMechanismStatus struct {
	Enabled           bool                   `json:"enabled"`
	RequestsProcessed uint64                 `json:"requests_processed"`
	AdaptationsCount  uint64                 `json:"adaptations_count"`
	CurrentSettings   map[string]interface{} `json:"current_settings"`
	LearningProgress  float64                `json:"learning_progress"`
}

// ClientsOverview provides a high-level overview of all clients.
type ClientsOverview struct {
	Total              int             `json:"total"`
	ByProtocol         map[string]int  `json:"by_protocol"`
	ByState            map[string]int  `json:"by_state"`
	Healthy            int             `json:"healthy"`
	Unhealthy          int             `json:"unhealthy"`
	RecentlyActive     int             `json:"recently_active"`
	LongestRunning     ClientBasicInfo `json:"longest_running"`
	MostActive         ClientBasicInfo `json:"most_active"`
	ProblematicClients []ClientProblem `json:"problematic_clients"`
}

// ClientBasicInfo contains basic information about a client.
type ClientBasicInfo struct {
	Name        string          `json:"name"`
	Protocol    string          `json:"protocol"`
	URL         string          `json:"url"`
	State       ConnectionState `json:"state"`
	MetricValue interface{}     `json:"metric_value"`
}

// ClientProblem represents a problematic client.
type ClientProblem struct {
	ClientName    string    `json:"client_name"`
	Protocol      string    `json:"protocol"`
	ProblemType   string    `json:"problem_type"`
	Description   string    `json:"description"`
	Severity      string    `json:"severity"`
	FirstObserved time.Time `json:"first_observed"`
	LastObserved  time.Time `json:"last_observed"`
	Occurrences   int       `json:"occurrences"`
}

// SystemResources tracks system resource usage.
type SystemResources struct {
	CPU             CPUInfo            `json:"cpu"`
	Memory          MemoryInfo         `json:"memory"`
	Network         NetworkInfo        `json:"network"`
	FileDescriptors FileDescriptorInfo `json:"file_descriptors"`
	Goroutines      int                `json:"goroutines"`
}

// CPUInfo contains CPU usage information.
type CPUInfo struct {
	UsagePercent float64   `json:"usage_percent"`
	LoadAverage  []float64 `json:"load_average,omitempty"`
	CoreCount    int       `json:"core_count"`
}

// MemoryInfo contains memory usage information.
type MemoryInfo struct {
	AllocBytes     uint64 `json:"alloc_bytes"`
	SysBytes       uint64 `json:"sys_bytes"`
	HeapAllocBytes uint64 `json:"heap_alloc_bytes"`
	HeapSysBytes   uint64 `json:"heap_sys_bytes"`
	HeapObjects    uint64 `json:"heap_objects"`
	StackBytes     uint64 `json:"stack_bytes"`
	GCStats        GCInfo `json:"gc_stats"`
}

// GCInfo contains garbage collection statistics.
type GCInfo struct {
	NumGC      uint32        `json:"num_gc"`
	PauseTotal time.Duration `json:"pause_total"`
	PauseNs    []uint64      `json:"pause_ns"`
	NextGC     uint64        `json:"next_gc"`
	LastGC     time.Time     `json:"last_gc"`
}

// NetworkInfo contains network statistics.
type NetworkInfo struct {
	ConnectionsTotal   int            `json:"connections_total"`
	ConnectionsByState map[string]int `json:"connections_by_state"`
	BytesSent          uint64         `json:"bytes_sent"`
	BytesReceived      uint64         `json:"bytes_received"`
	PacketsSent        uint64         `json:"packets_sent"`
	PacketsReceived    uint64         `json:"packets_received"`
}

// FileDescriptorInfo contains file descriptor usage.
type FileDescriptorInfo struct {
	Open    int     `json:"open"`
	Limit   int     `json:"limit"`
	Percent float64 `json:"percent_used"`
}

// ConfigurationSummary provides a summary of current configuration.
type ConfigurationSummary struct {
	DefaultTimeout       time.Duration `json:"default_timeout"`
	MaxConnections       int           `json:"max_connections"`
	HealthCheckEnabled   bool          `json:"health_check_enabled"`
	AutoDetectionEnabled bool          `json:"auto_detection_enabled"`
	FallbackEnabled      bool          `json:"fallback_enabled"`
	ProtocolsSupported   []string      `json:"protocols_supported"`
	AdaptiveMechanisms   []string      `json:"adaptive_mechanisms"`
}

// HealthIndicators provides health status indicators.
type HealthIndicators struct {
	OverallHealth      string                `json:"overall_health"`
	HealthScore        int                   `json:"health_score"`
	CriticalAlerts     []HealthAlert         `json:"critical_alerts"`
	Warnings           []HealthAlert         `json:"warnings"`
	PerformanceMetrics PerformanceIndicators `json:"performance_metrics"`
}

// HealthAlert represents a health alert.
type HealthAlert struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	Severity  string    `json:"severity"`
	Message   string    `json:"message"`
	Component string    `json:"component"`
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
	Count     int       `json:"count"`
	Resolved  bool      `json:"resolved"`
}

// PerformanceIndicators contains key performance indicators.
type PerformanceIndicators struct {
	RequestLatencyP50         time.Duration `json:"request_latency_p50"`
	RequestLatencyP95         time.Duration `json:"request_latency_p95"`
	RequestLatencyP99         time.Duration `json:"request_latency_p99"`
	ThroughputRPS             float64       `json:"throughput_rps"`
	ErrorRate                 float64       `json:"error_rate"`
	SuccessRate               float64       `json:"success_rate"`
	ConnectionPoolUtilization float64       `json:"connection_pool_utilization"`
}

// StatusMonitor manages status collection and monitoring.
type StatusMonitor struct {
	manager       *DirectClientManager
	observability *ObservabilityManager
	logger        *zap.Logger
	mu            sync.RWMutex

	// Status data.
	systemStatus *SystemStatus
	startTime    time.Time
	alerts       []HealthAlert
	alertIndex   map[string]int

	// Background monitoring.
	shutdown chan struct{}
	wg       sync.WaitGroup
}

// NewStatusMonitor creates a new status monitor.
func NewStatusMonitor(
	manager *DirectClientManager,
	observability *ObservabilityManager,
	logger *zap.Logger,
) *StatusMonitor {
	return &StatusMonitor{
		manager:       manager,
		observability: observability,
		logger:        logger.With(zap.String("component", "status_monitor")),
		startTime:     time.Now(),
		alerts:        make([]HealthAlert, 0),
		alertIndex:    make(map[string]int),
		shutdown:      make(chan struct{}),
	}
}

// Start begins status monitoring.
func (sm *StatusMonitor) Start(ctx context.Context) error {
	sm.logger.Info("starting status monitor")

	// Start background monitoring.
	sm.wg.Add(1)

	go sm.monitoringLoop()

	// Start alert processing.
	sm.wg.Add(1)

	go sm.alertProcessingLoop()

	return nil
}

// Stop shuts down status monitoring.
func (sm *StatusMonitor) Stop(ctx context.Context) error {
	sm.logger.Info("stopping status monitor")

	// Signal shutdown.
	select {
	case <-sm.shutdown:
		// Already closed.
	default:
		close(sm.shutdown)
	}

	// Wait for background routines.
	done := make(chan struct{})

	go func() {
		sm.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		sm.logger.Info("status monitor stopped")
	case <-time.After(defaultMaxConnections * time.Second):
		sm.logger.Warn("status monitor stop timeout")
	case <-ctx.Done():
		return ctx.Err()
	}

	return nil
}

// GetSystemStatus returns current system status.
func (sm *StatusMonitor) GetSystemStatus() SystemStatus {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	sm.updateSystemStatus()

	return *sm.systemStatus
}

// GetClientStatus returns detailed status for a specific client.
func (sm *StatusMonitor) GetClientStatus(clientName string) (*DirectClientStatus, error) {
	// This would be implemented by individual client types.
	// For now, return a basic implementation.
	if clientMetrics, exists := sm.observability.GetClientMetrics(clientName); exists {
		return &DirectClientStatus{
			Name:            clientName,
			URL:             clientMetrics.ServerURL,
			Protocol:        clientMetrics.Protocol,
			State:           clientMetrics.ConnectionState,
			LastConnected:   clientMetrics.ConnectionTime,
			LastHealthCheck: clientMetrics.LastHealthCheck,
			ConnectionAge:   time.Since(clientMetrics.ConnectionTime),
			Metrics:         *clientMetrics,
			Configuration:   make(map[string]interface{}),
			RuntimeInfo: ClientRuntimeInfo{
				ConnectionID: fmt.Sprintf("%s-%d", clientName, clientMetrics.ConnectionTime.Unix()),
				ResourceUsage: ResourceUsage{
					MemoryBytes: clientMetrics.MemoryUsage,
					LastUpdated: clientMetrics.LastMetricsUpdate,
				},
			},
		}, nil
	}

	return nil, fmt.Errorf("client not found: %s", clientName)
}

// GetHealthAlerts returns current health alerts.
func (sm *StatusMonitor) GetHealthAlerts() []HealthAlert {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	// Return a copy of alerts.
	alerts := make([]HealthAlert, len(sm.alerts))
	copy(alerts, sm.alerts)

	return alerts
}

// AddHealthAlert adds a new health alert.
func (sm *StatusMonitor) AddHealthAlert(alert HealthAlert) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Check if alert already exists.
	if existingIndex, exists := sm.alertIndex[alert.ID]; exists {
		// Update existing alert.
		sm.alerts[existingIndex].LastSeen = alert.LastSeen
		sm.alerts[existingIndex].Count++
		sm.alerts[existingIndex].Message = alert.Message
	} else {
		// Add new alert.
		alert.FirstSeen = alert.LastSeen
		alert.Count = 1
		sm.alerts = append(sm.alerts, alert)
		sm.alertIndex[alert.ID] = len(sm.alerts) - 1
	}

	sm.logger.Info("health alert added",
		zap.String("alert_id", alert.ID),
		zap.String("severity", alert.Severity),
		zap.String("message", alert.Message))
}

// ResolveHealthAlert resolves a health alert.
func (sm *StatusMonitor) ResolveHealthAlert(alertID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if index, exists := sm.alertIndex[alertID]; exists {
		sm.alerts[index].Resolved = true
		sm.logger.Info("health alert resolved", zap.String("alert_id", alertID))
	}
}

// ExportStatus exports all status information in JSON format.
func (sm *StatusMonitor) ExportStatus() ([]byte, error) {
	systemStatus := sm.GetSystemStatus()
	alerts := sm.GetHealthAlerts()

	export := map[string]interface{}{
		"system_status":    systemStatus,
		"health_alerts":    alerts,
		"export_timestamp": time.Now(),
	}

	return json.MarshalIndent(export, "", "  ")
}

// monitoringLoop runs the background monitoring task.
func (sm *StatusMonitor) monitoringLoop() {
	defer sm.wg.Done()

	ticker := time.NewTicker(defaultTimeoutSeconds * time.Second) // Monitor every 30 seconds
	defer ticker.Stop()

	for {
		select {
		case <-sm.shutdown:
			return
		case <-ticker.C:
			sm.runHealthChecks()
		}
	}
}

// runHealthChecks performs health checks and generates alerts.
func (sm *StatusMonitor) runHealthChecks() {
	runner := CreateHealthCheckRunner(sm)
	runner.ExecuteHealthChecks()
}

// alertProcessingLoop processes and manages health alerts.
func (sm *StatusMonitor) alertProcessingLoop() {
	defer sm.wg.Done()

	ticker := time.NewTicker(constants.CleanupTickerInterval)
	defer ticker.Stop()

	for {
		select {
		case <-sm.shutdown:
			return
		case <-ticker.C:
			sm.processAlerts()
		}
	}
}

// processAlerts processes and cleans up old alerts.
func (sm *StatusMonitor) processAlerts() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	now := time.Now()
	activeAlerts := make([]HealthAlert, 0)
	newAlertIndex := make(map[string]int)

	// Remove alerts that are resolved and older than 1 hour.
	for _, alert := range sm.alerts {
		if alert.Resolved && now.Sub(alert.LastSeen) > time.Hour {
			continue // Skip old resolved alerts
		}

		// Keep active or recent alerts.
		newAlertIndex[alert.ID] = len(activeAlerts)
		activeAlerts = append(activeAlerts, alert)
	}

	removed := len(sm.alerts) - len(activeAlerts)
	sm.alerts = activeAlerts
	sm.alertIndex = newAlertIndex

	if removed > 0 {
		sm.logger.Debug("cleaned up old alerts", zap.Int("removed", removed))
	}
}

// updateSystemStatus updates the system status information.
func (sm *StatusMonitor) updateSystemStatus() {
	updater := CreateSystemStatusUpdater(sm)
	updater.UpdateStatus()
}

// calculateHealthScore calculates an overall health score (0-100).
func (sm *StatusMonitor) calculateHealthScore(
	managerMetrics EnhancedManagerMetrics,
	clientMetrics map[string]DetailedClientMetrics,
) int {
	score := 100

	const (
		errorRatePenalty      = 50 // Maximum points deducted for error rate
		unhealthyClientPenalty = 30 // Maximum points deducted for unhealthy clients
	)

	// Reduce score based on error rate.
	if managerMetrics.ErrorRate > 0 {
		score -= int(managerMetrics.ErrorRate * errorRatePenalty)
	}

	// Reduce score based on unhealthy clients.
	unhealthyCount := 0

	for _, metrics := range clientMetrics {
		if !metrics.IsHealthy {
			unhealthyCount++
		}
	}

	if len(clientMetrics) > 0 {
		unhealthyRatio := float64(unhealthyCount) / float64(len(clientMetrics))
		score -= int(unhealthyRatio * unhealthyClientPenalty)
	}

	// Reduce score based on memory usage.
	if managerMetrics.MemoryUsage > 512*defaultBufferSize*defaultBufferSize { // > 512MB
		score -= 10
	}

	if managerMetrics.MemoryUsage > defaultBufferSize*defaultBufferSize*defaultBufferSize { // > 1GB
		score -= 20
	}

	// Reduce score based on goroutine count.
	if managerMetrics.GoroutineCount > ThousandMillisThreshold {
		score -= 10
	}

	if managerMetrics.GoroutineCount > FiveThousandMillisThreshold {
		score -= 20
	}

	if score < 0 {
		score = 0
	}

	return score
}

// countRecentlyActiveClients counts clients active in the last 5 minutes.
func (sm *StatusMonitor) countRecentlyActiveClients(clientMetrics map[string]DetailedClientMetrics) int {
	count := 0
	cutoff := time.Now().Add(-constants.CleanupTickerInterval)

	for _, metrics := range clientMetrics {
		if metrics.LastUsed.After(cutoff) {
			count++
		}
	}

	return count
}

// calculateAverageLatency calculates the average latency across all clients.
func (sm *StatusMonitor) calculateAverageLatency(clientMetrics map[string]DetailedClientMetrics) time.Duration {
	if len(clientMetrics) == 0 {
		return 0
	}

	var totalLatency time.Duration

	count := 0

	for _, metrics := range clientMetrics {
		if metrics.AverageLatency > 0 {
			totalLatency += metrics.AverageLatency
			count++
		}
	}

	if count == 0 {
		return 0
	}

	return totalLatency / time.Duration(count)
}

// getAlertsBySeverity returns alerts filtered by severity.
func (sm *StatusMonitor) getAlertsBySeverity(severity string) []HealthAlert {
	var filtered []HealthAlert

	for _, alert := range sm.alerts {
		if alert.Severity == severity && !alert.Resolved {
			filtered = append(filtered, alert)
		}
	}

	return filtered
}
