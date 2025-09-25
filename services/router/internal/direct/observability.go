package direct

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/poiley/mcp-bridge/services/router/internal/constants"
	"go.uber.org/zap"
)

const (
	// HealthStatusHealthy represents a healthy system status.
	HealthStatusHealthy = "healthy"
	// HealthStatusWarning represents a warning system status.
	HealthStatusWarning = "warning"
	// HealthStatusCritical represents a critical system status.
	HealthStatusCritical = "critical"
)

// ObservabilityConfig configures metrics collection and observability features.
type ObservabilityConfig struct {
	// Enable metrics collection.
	MetricsEnabled bool `mapstructure:"metrics_enabled" yaml:"metrics_enabled"`

	// Metrics collection interval.
	MetricsInterval time.Duration `mapstructure:"metrics_interval" yaml:"metrics_interval"`

	// Enable request tracing.
	TracingEnabled bool `mapstructure:"tracing_enabled" yaml:"tracing_enabled"`

	// Maximum number of traces to keep in memory.
	MaxTraces int `mapstructure:"max_traces" yaml:"max_traces"`

	// Enable performance profiling.
	ProfilingEnabled bool `mapstructure:"profiling_enabled" yaml:"profiling_enabled"`

	// Enable detailed health monitoring.
	HealthMonitoringEnabled bool `mapstructure:"health_monitoring_enabled" yaml:"health_monitoring_enabled"`

	// Retention period for metrics history.
	MetricsRetention time.Duration `mapstructure:"metrics_retention" yaml:"metrics_retention"`

	// Enable memory usage tracking.
	MemoryTrackingEnabled bool `mapstructure:"memory_tracking_enabled" yaml:"memory_tracking_enabled"`
}

// DetailedClientMetrics provides comprehensive client performance data.
type DetailedClientMetrics struct {
	// Basic metrics (from original ClientMetrics).
	RequestCount    uint64        `json:"request_count"`
	ErrorCount      uint64        `json:"error_count"`
	AverageLatency  time.Duration `json:"average_latency"`
	LastHealthCheck time.Time     `json:"last_health_check"`
	IsHealthy       bool          `json:"is_healthy"`
	ConnectionTime  time.Time     `json:"connection_time"`
	LastUsed        time.Time     `json:"last_used"`

	// Enhanced metrics.
	MinLatency        time.Duration   `json:"min_latency"`
	MaxLatency        time.Duration   `json:"max_latency"`
	P50Latency        time.Duration   `json:"p50_latency"`
	P95Latency        time.Duration   `json:"p95_latency"`
	P99Latency        time.Duration   `json:"p99_latency"`
	RequestsPerSecond float64         `json:"requests_per_second"`
	ErrorRate         float64         `json:"error_rate"`
	SuccessRate       float64         `json:"success_rate"`
	ConnectionState   ConnectionState `json:"connection_state"`
	Protocol          string          `json:"protocol"`
	ServerURL         string          `json:"server_url"`

	// Error breakdown.
	ErrorsByType     map[string]uint64 `json:"errors_by_type"`
	TimeoutCount     uint64            `json:"timeout_count"`
	ConnectionErrors uint64            `json:"connection_errors"`
	ProtocolErrors   uint64            `json:"protocol_errors"`

	// Performance tracking.
	BytesSent      uint64 `json:"bytes_sent"`
	BytesReceived  uint64 `json:"bytes_received"`
	MemoryUsage    uint64 `json:"memory_usage_bytes"`
	GoroutineCount int    `json:"goroutine_count"`

	// Health status details.
	HealthHistory       []HealthCheckResult `json:"health_history"`
	LastError           string              `json:"last_error"`
	LastErrorTime       time.Time           `json:"last_error_time"`
	ConsecutiveFailures uint64              `json:"consecutive_failures"`

	// Timing information.
	CreatedAt         time.Time `json:"created_at"`
	LastMetricsUpdate time.Time `json:"last_metrics_update"`
}

// HealthCheckResult represents a health check outcome.
type HealthCheckResult struct {
	Timestamp time.Time     `json:"timestamp"`
	Success   bool          `json:"success"`
	Latency   time.Duration `json:"latency"`
	Error     string        `json:"error,omitempty"`
}

// EnhancedManagerMetrics provides comprehensive manager-level observability.
type EnhancedManagerMetrics struct {
	// Basic metrics (from original ManagerMetrics).
	TotalClients       int           `json:"total_clients"`
	ActiveConnections  int           `json:"active_connections"`
	FailedConnections  int           `json:"failed_connections"`
	ProtocolDetections int           `json:"protocol_detections"`
	CacheHits          int           `json:"cache_hits"`
	CacheMisses        int           `json:"cache_misses"`
	AverageLatency     time.Duration `json:"average_latency"`
	LastUpdate         time.Time     `json:"last_update"`

	// Enhanced metrics.
	ClientsByProtocol map[string]int `json:"clients_by_protocol"`
	ClientsByState    map[string]int `json:"clients_by_state"`
	TotalRequests     uint64         `json:"total_requests"`
	TotalErrors       uint64         `json:"total_errors"`
	RequestsPerSecond float64        `json:"requests_per_second"`
	ErrorRate         float64        `json:"error_rate"`
	CacheHitRate      float64        `json:"cache_hit_rate"`

	// Performance metrics.
	MemoryUsage    uint64        `json:"memory_usage_bytes"`
	GoroutineCount int           `json:"goroutine_count"`
	HeapObjects    uint64        `json:"heap_objects"`
	GCCount        uint64        `json:"gc_count"`
	GCPauseTotal   time.Duration `json:"gc_pause_total"`

	// Connection pool metrics.
	PoolStats map[string]interface{} `json:"pool_stats"`

	// System resource usage.
	CPUUsage  float64 `json:"cpu_usage_percent"`
	OpenFiles int     `json:"open_files"`

	// Adaptive mechanism metrics.
	AdaptiveTimeoutStats map[string]interface{} `json:"adaptive_timeout_stats"`
	AdaptiveRetryStats   map[string]interface{} `json:"adaptive_retry_stats"`
}

// RequestTrace represents a traced request for debugging and observability.
type RequestTrace struct {
	ID           string                 `json:"id"`
	Timestamp    time.Time              `json:"timestamp"`
	ClientName   string                 `json:"client_name"`
	Protocol     string                 `json:"protocol"`
	ServerURL    string                 `json:"server_url"`
	Method       string                 `json:"method"`
	Duration     time.Duration          `json:"duration"`
	Success      bool                   `json:"success"`
	Error        string                 `json:"error,omitempty"`
	RequestSize  int                    `json:"request_size_bytes"`
	ResponseSize int                    `json:"response_size_bytes"`
	Metadata     map[string]interface{} `json:"metadata"`
}

// PerformanceProfile captures system performance at a point in time.
type PerformanceProfile struct {
	Timestamp      time.Time        `json:"timestamp"`
	MemoryStats    runtime.MemStats `json:"memory_stats"`
	GoroutineCount int              `json:"goroutine_count"`
	CGOCalls       int64            `json:"cgo_calls"`
	OpenFiles      int              `json:"open_files"`
	CPUUsage       float64          `json:"cpu_usage_percent"`
}

// ObservabilityManager manages metrics collection, health monitoring, and tracing.
type ObservabilityManager struct {
	config ObservabilityConfig
	logger *zap.Logger
	mu     sync.RWMutex

	// Metrics storage.
	clientMetrics  map[string]*DetailedClientMetrics
	managerMetrics *EnhancedManagerMetrics
	metricsHistory []EnhancedManagerMetrics

	// Request tracing.
	traces     []RequestTrace
	traceIndex int
	maxTraces  int

	// Performance profiling.
	profiles     []PerformanceProfile
	profileIndex int
	maxProfiles  int

	// Background tasks.
	shutdown chan struct{}
	wg       sync.WaitGroup

	// Counters for atomic operations.
	requestCounter int64
	errorCounter   int64
}

// NewObservabilityManager creates a new observability manager.
func NewObservabilityManager(config ObservabilityConfig, logger *zap.Logger) *ObservabilityManager {
	// Set defaults.
	if config.MetricsInterval == 0 {
		config.MetricsInterval = defaultTimeoutSeconds * time.Second
	}

	if config.MaxTraces == 0 {
		config.MaxTraces = 1000
	}

	if config.MetricsRetention == 0 {
		config.MetricsRetention = constants.DailyRetention
	}

	maxProfiles := 100

	om := &ObservabilityManager{
		config:        config,
		logger:        logger.With(zap.String("component", "observability_manager")),
		clientMetrics: make(map[string]*DetailedClientMetrics),
		managerMetrics: &EnhancedManagerMetrics{
			ClientsByProtocol:    make(map[string]int),
			ClientsByState:       make(map[string]int),
			PoolStats:            make(map[string]interface{}),
			AdaptiveTimeoutStats: make(map[string]interface{}),
			AdaptiveRetryStats:   make(map[string]interface{}),
		},
		traces:      make([]RequestTrace, config.MaxTraces),
		maxTraces:   config.MaxTraces,
		profiles:    make([]PerformanceProfile, maxProfiles),
		maxProfiles: maxProfiles,
		shutdown:    make(chan struct{}),
	}

	return om
}

// Start begins observability collection.
func (om *ObservabilityManager) Start(ctx context.Context) error {
	if !om.config.MetricsEnabled && !om.config.TracingEnabled && !om.config.ProfilingEnabled {
		om.logger.Info("observability disabled, skipping start")

		return nil
	}

	om.logger.Info("starting observability manager",
		zap.Bool("metrics_enabled", om.config.MetricsEnabled),
		zap.Bool("tracing_enabled", om.config.TracingEnabled),
		zap.Bool("profiling_enabled", om.config.ProfilingEnabled),
		zap.Duration("metrics_interval", om.config.MetricsInterval))

	// Start metrics collection.
	if om.config.MetricsEnabled {
		om.wg.Add(1)

		go om.metricsCollectionLoop()
	}

	// Start performance profiling.
	if om.config.ProfilingEnabled {
		om.wg.Add(1)

		go om.performanceProfilingLoop()
	}

	// Start metrics cleanup.
	if om.config.MetricsRetention > 0 {
		om.wg.Add(1)

		go om.metricsCleanupLoop()
	}

	return nil
}

// Stop shuts down observability collection.
func (om *ObservabilityManager) Stop(ctx context.Context) error {
	om.logger.Info("stopping observability manager")

	// Signal shutdown.
	select {
	case <-om.shutdown:
		// Already closed.
	default:
		close(om.shutdown)
	}

	// Wait for background routines.
	done := make(chan struct{})

	go func() {
		om.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		om.logger.Info("observability manager stopped")
	case <-time.After(defaultRetryCount * time.Second):
		om.logger.Warn("observability manager stop timeout")
	case <-ctx.Done():
		return ctx.Err()
	}

	return nil
}

// RecordClientMetrics updates metrics for a specific client.
func (om *ObservabilityManager) RecordClientMetrics(clientName string, metrics DetailedClientMetrics) {
	if !om.config.MetricsEnabled {
		return
	}

	om.mu.Lock()
	defer om.mu.Unlock()

	metrics.LastMetricsUpdate = time.Now()
	om.clientMetrics[clientName] = &metrics
}

// RecordRequest records a request trace.
func (om *ObservabilityManager) RecordRequest(trace RequestTrace) {
	if !om.config.TracingEnabled {
		return
	}

	atomic.AddInt64(&om.requestCounter, 1)

	if !trace.Success {
		atomic.AddInt64(&om.errorCounter, 1)
	}

	om.mu.Lock()
	defer om.mu.Unlock()

	// Add to circular buffer.
	om.traces[om.traceIndex] = trace
	om.traceIndex = (om.traceIndex + 1) % om.maxTraces
}

// RecordHealthCheck records a health check result for a client.
func (om *ObservabilityManager) RecordHealthCheck(clientName string, result HealthCheckResult) {
	if !om.config.HealthMonitoringEnabled {
		return
	}

	om.mu.Lock()
	defer om.mu.Unlock()

	if clientMetrics, exists := om.clientMetrics[clientName]; exists {
		// Keep last 10 health check results.
		if len(clientMetrics.HealthHistory) >= DefaultSampleWindowSize {
			clientMetrics.HealthHistory = clientMetrics.HealthHistory[1:]
		}

		clientMetrics.HealthHistory = append(clientMetrics.HealthHistory, result)

		// Update consecutive failures counter.
		if result.Success {
			clientMetrics.ConsecutiveFailures = 0
		} else {
			clientMetrics.ConsecutiveFailures++
			clientMetrics.LastError = result.Error
			clientMetrics.LastErrorTime = result.Timestamp
		}
	}
}

// GetClientMetrics returns detailed metrics for a specific client.
func (om *ObservabilityManager) GetClientMetrics(clientName string) (*DetailedClientMetrics, bool) {
	om.mu.RLock()
	defer om.mu.RUnlock()

	metrics, exists := om.clientMetrics[clientName]
	if !exists {
		return nil, false
	}

	// Return a copy to prevent race conditions.
	metricsCopy := *metrics

	return &metricsCopy, true
}

// GetAllClientMetrics returns metrics for all clients.
func (om *ObservabilityManager) GetAllClientMetrics() map[string]DetailedClientMetrics {
	om.mu.RLock()
	defer om.mu.RUnlock()

	result := make(map[string]DetailedClientMetrics)
	for name, metrics := range om.clientMetrics {
		result[name] = *metrics
	}

	return result
}

// GetManagerMetrics returns current manager metrics.
func (om *ObservabilityManager) GetManagerMetrics() EnhancedManagerMetrics {
	om.mu.RLock()
	defer om.mu.RUnlock()

	// Update runtime metrics.
	om.updateManagerMetrics()

	// Return a copy.
	return *om.managerMetrics
}

// GetRequestTraces returns recent request traces.
func (om *ObservabilityManager) GetRequestTraces(limit int) []RequestTrace {
	if !om.config.TracingEnabled {
		return nil
	}

	om.mu.RLock()
	defer om.mu.RUnlock()

	if limit <= 0 || limit > om.maxTraces {
		limit = om.maxTraces
	}

	traces := make([]RequestTrace, 0, limit)

	// Get traces in reverse chronological order.
	for i := 0; i < limit && i < om.maxTraces; i++ {
		idx := (om.traceIndex - 1 - i + om.maxTraces) % om.maxTraces

		trace := om.traces[idx]
		if !trace.Timestamp.IsZero() {
			traces = append(traces, trace)
		}
	}

	return traces
}

// GetPerformanceProfiles returns recent performance profiles.
func (om *ObservabilityManager) GetPerformanceProfiles(limit int) []PerformanceProfile {
	if !om.config.ProfilingEnabled {
		return nil
	}

	om.mu.RLock()
	defer om.mu.RUnlock()

	if limit <= 0 || limit > om.maxProfiles {
		limit = om.maxProfiles
	}

	profiles := make([]PerformanceProfile, 0, limit)

	// Get profiles in reverse chronological order.
	for i := 0; i < limit && i < om.maxProfiles; i++ {
		idx := (om.profileIndex - 1 - i + om.maxProfiles) % om.maxProfiles

		profile := om.profiles[idx]
		if !profile.Timestamp.IsZero() {
			profiles = append(profiles, profile)
		}
	}

	return profiles
}

// GetMetricsHistory returns historical manager metrics.
func (om *ObservabilityManager) GetMetricsHistory(limit int) []EnhancedManagerMetrics {
	om.mu.RLock()
	defer om.mu.RUnlock()

	if limit <= 0 || limit > len(om.metricsHistory) {
		limit = len(om.metricsHistory)
	}

	// Return most recent entries.
	start := len(om.metricsHistory) - limit
	if start < 0 {
		start = 0
	}

	return append([]EnhancedManagerMetrics{}, om.metricsHistory[start:]...)
}

// ExportMetrics exports all metrics in JSON format.
func (om *ObservabilityManager) ExportMetrics() ([]byte, error) {
	export := map[string]interface{}{
		"manager_metrics":      om.GetManagerMetrics(),
		"client_metrics":       om.GetAllClientMetrics(),
		"request_traces":       om.GetRequestTraces(DefaultTraceLimit),
		"performance_profiles": om.GetPerformanceProfiles(DefaultSampleWindowSize),
		"metrics_history":      om.GetMetricsHistory(DefaultMetricsHistoryHours),
		"export_timestamp":     time.Now(),
	}

	return json.MarshalIndent(export, "", "  ")
}

// metricsCollectionLoop runs the metrics collection background task.
func (om *ObservabilityManager) metricsCollectionLoop() {
	defer om.wg.Done()

	ticker := time.NewTicker(om.config.MetricsInterval)
	defer ticker.Stop()

	for {
		select {
		case <-om.shutdown:
			return
		case <-ticker.C:
			om.collectMetrics()
		}
	}
}

// collectMetrics collects and updates manager metrics.
func (om *ObservabilityManager) collectMetrics() {
	om.mu.Lock()
	defer om.mu.Unlock()

	om.updateManagerMetrics()

	// Add to history.
	metricsCopy := *om.managerMetrics
	metricsCopy.LastUpdate = time.Now()
	om.metricsHistory = append(om.metricsHistory, metricsCopy)

	om.logger.Debug("metrics collected",
		zap.Int("total_clients", om.managerMetrics.TotalClients),
		zap.Int("active_connections", om.managerMetrics.ActiveConnections),
		zap.Float64("requests_per_second", om.managerMetrics.RequestsPerSecond),
		zap.Uint64("memory_usage", om.managerMetrics.MemoryUsage))
}

// updateManagerMetrics updates the current manager metrics.
func (om *ObservabilityManager) updateManagerMetrics() {
	// Get runtime statistics.
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	// Update basic counts and protocol breakdown.
	om.updateClientCounts()

	// Update request statistics and rates.
	om.updateRequestMetrics()

	// Update memory and runtime metrics.
	om.updateRuntimeMetrics(&m)

	// Update cache hit rate.
	om.updateCacheMetrics()
}

// updateClientCounts updates client counts and protocol breakdown.
func (om *ObservabilityManager) updateClientCounts() {
	om.managerMetrics.TotalClients = len(om.clientMetrics)

	protocolCounts := make(map[string]int)
	stateCounts := make(map[string]int)
	activeCount := 0

	for _, metrics := range om.clientMetrics {
		protocolCounts[metrics.Protocol]++
		stateCounts[metrics.ConnectionState.String()]++

		if metrics.IsHealthy && metrics.ConnectionState == StateConnected || metrics.ConnectionState == StateHealthy {
			activeCount++
		}
	}

	om.managerMetrics.ClientsByProtocol = protocolCounts
	om.managerMetrics.ClientsByState = stateCounts
	om.managerMetrics.ActiveConnections = activeCount
}

// updateRequestMetrics updates request statistics and rates.
func (om *ObservabilityManager) updateRequestMetrics() {
	requestCount := atomic.LoadInt64(&om.requestCounter)
	errorCount := atomic.LoadInt64(&om.errorCounter)

	if requestCount >= 0 {
		om.managerMetrics.TotalRequests = uint64(requestCount)
	}

	if errorCount >= 0 {
		om.managerMetrics.TotalErrors = uint64(errorCount)
	}

	// Calculate rates.
	if requestCount > 0 {
		om.managerMetrics.ErrorRate = float64(errorCount) / float64(requestCount)

		// Calculate requests per second (rough estimate based on uptime).
		uptime := time.Since(time.Now().Add(-om.config.MetricsInterval))
		if uptime > 0 {
			om.managerMetrics.RequestsPerSecond = float64(requestCount) / uptime.Seconds()
		}
	}
}

// updateRuntimeMetrics updates memory and runtime metrics.
func (om *ObservabilityManager) updateRuntimeMetrics(m *runtime.MemStats) {
	om.managerMetrics.MemoryUsage = m.Alloc
	om.managerMetrics.GoroutineCount = runtime.NumGoroutine()
	om.managerMetrics.HeapObjects = m.HeapObjects

	om.managerMetrics.GCCount = uint64(m.NumGC)
	if m.PauseTotalNs <= math.MaxInt64 {
		om.managerMetrics.GCPauseTotal = time.Duration(m.PauseTotalNs)
	}
}

// updateCacheMetrics updates cache hit rate.
func (om *ObservabilityManager) updateCacheMetrics() {
	totalCacheOps := om.managerMetrics.CacheHits + om.managerMetrics.CacheMisses
	if totalCacheOps > 0 {
		om.managerMetrics.CacheHitRate = float64(om.managerMetrics.CacheHits) / float64(totalCacheOps)
	}
}

// performanceProfilingLoop collects performance profiles periodically.
func (om *ObservabilityManager) performanceProfilingLoop() {
	defer om.wg.Done()

	ticker := time.NewTicker(constants.CleanupTickerInterval)
	defer ticker.Stop()

	for {
		select {
		case <-om.shutdown:
			return
		case <-ticker.C:
			om.collectPerformanceProfile()
		}
	}
}

// collectPerformanceProfile captures current system performance.
func (om *ObservabilityManager) collectPerformanceProfile() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	profile := PerformanceProfile{
		Timestamp:      time.Now(),
		MemoryStats:    m,
		GoroutineCount: runtime.NumGoroutine(),
		CGOCalls:       runtime.NumCgoCall(),
		// Note: OpenFiles and CPUUsage would require platform-specific code.
		// For now, we'll set them to 0 or calculate them differently.
	}

	om.mu.Lock()
	om.profiles[om.profileIndex] = profile
	om.profileIndex = (om.profileIndex + 1) % om.maxProfiles
	om.mu.Unlock()

	om.logger.Debug("performance profile collected",
		zap.Time("timestamp", profile.Timestamp),
		zap.Uint64("heap_alloc", m.Alloc),
		zap.Int("goroutines", profile.GoroutineCount))
}

// metricsCleanupLoop removes old metrics data based on retention policy.
func (om *ObservabilityManager) metricsCleanupLoop() {
	defer om.wg.Done()

	ticker := time.NewTicker(constants.HourlyTickerInterval)
	defer ticker.Stop()

	for {
		select {
		case <-om.shutdown:
			return
		case <-ticker.C:
			om.cleanupMetrics()
		}
	}
}

// cleanupMetrics removes old metrics data.
func (om *ObservabilityManager) cleanupMetrics() {
	om.mu.Lock()
	defer om.mu.Unlock()

	cutoff := time.Now().Add(-om.config.MetricsRetention)

	// Clean up metrics history.
	var filteredHistory []EnhancedManagerMetrics

	for _, metrics := range om.metricsHistory {
		if metrics.LastUpdate.After(cutoff) {
			filteredHistory = append(filteredHistory, metrics)
		}
	}

	removed := len(om.metricsHistory) - len(filteredHistory)
	om.metricsHistory = filteredHistory

	if removed > 0 {
		om.logger.Debug("cleaned up old metrics",
			zap.Int("removed_entries", removed),
			zap.Duration("retention_period", om.config.MetricsRetention))
	}
}

// GenerateHealthReport generates a comprehensive health report.
func (om *ObservabilityManager) GenerateHealthReport() map[string]interface{} {
	managerMetrics := om.GetManagerMetrics()
	clientMetrics := om.GetAllClientMetrics()

	// Calculate overall health score (0-100).
	var healthScore float64 = 100

	// Reduce score based on error rate.
	if managerMetrics.ErrorRate > 0 {
		healthScore -= (managerMetrics.ErrorRate * DefaultProfileSampleSize) // Up to 50 points for errors
	}

	// Reduce score based on unhealthy clients.
	unhealthyCount := 0
	totalClients := len(clientMetrics)

	for _, metrics := range clientMetrics {
		if !metrics.IsHealthy {
			unhealthyCount++
		}
	}

	if totalClients > 0 {
		unhealthyRatio := float64(unhealthyCount) / float64(totalClients)
		healthScore -= (unhealthyRatio * DefaultLowThresholdPercent) // Up to 30 points for unhealthy clients
	}

	// Reduce score based on memory usage (if very high).
	if managerMetrics.MemoryUsage > defaultBufferSize*defaultBufferSize*defaultBufferSize { // > 1GB
		healthScore -= ScoreReductionHigh
	}

	if healthScore < 0 {
		healthScore = 0
	}

	status := HealthStatusHealthy
	if healthScore < DefaultMediumThresholdPercent {
		status = HealthStatusCritical
	} else if healthScore < DefaultHighThresholdPercent {
		status = HealthStatusWarning
	}

	return map[string]interface{}{
		"overall_status":      status,
		"health_score":        int(healthScore),
		"total_clients":       totalClients,
		"healthy_clients":     totalClients - unhealthyCount,
		"unhealthy_clients":   unhealthyCount,
		"error_rate":          fmt.Sprintf("%.2f%%", managerMetrics.ErrorRate*PercentageBase),
		"requests_per_second": managerMetrics.RequestsPerSecond,
		"memory_usage_mb":     managerMetrics.MemoryUsage / (defaultBufferSize * defaultBufferSize),
		"goroutines":          managerMetrics.GoroutineCount,
		"uptime_seconds":      time.Since(time.Now().Add(-om.config.MetricsInterval)).Seconds(),
		"timestamp":           time.Now(),
	}
}
