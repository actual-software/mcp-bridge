package router

import (
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

// MetricsCollector handles all metrics collection and reporting.
type MetricsCollector struct {
	logger *zap.Logger

	// Atomic counters for thread-safe access.
	requestsTotal     uint64
	responsesTotal    uint64
	errorsTotal       uint64
	connectionRetries uint64
	directRequests    uint64 // Requests routed directly
	gatewayRequests   uint64 // Requests routed through gateway
	fallbackSuccesses uint64 // Successful fallbacks from direct to gateway
	fallbackFailures  uint64 // Failed fallbacks (both direct and gateway failed)

	// Detailed metrics with synchronization.
	requestDuration   sync.Map // map[string][]time.Duration
	responseSizes     []int
	responseSizesMu   sync.Mutex
	activeConnections int32 // Use atomic.AddInt32 to modify
}

// NewMetricsCollector creates a new metrics collector.
func NewMetricsCollector(logger *zap.Logger) *MetricsCollector {
	return &MetricsCollector{
		logger: logger,
	}
}

// IncrementRequests atomically increments the request counter.
func (mc *MetricsCollector) IncrementRequests() {
	atomic.AddUint64(&mc.requestsTotal, 1)
}

// IncrementResponses atomically increments the response counter.
func (mc *MetricsCollector) IncrementResponses() {
	atomic.AddUint64(&mc.responsesTotal, 1)
}

// IncrementErrors atomically increments the error counter.
func (mc *MetricsCollector) IncrementErrors() {
	atomic.AddUint64(&mc.errorsTotal, 1)
}

// IncrementDirectRequests increments the direct requests counter.
func (mc *MetricsCollector) IncrementDirectRequests() {
	atomic.AddUint64(&mc.directRequests, 1)
}

// IncrementGatewayRequests increments the gateway requests counter.
func (mc *MetricsCollector) IncrementGatewayRequests() {
	atomic.AddUint64(&mc.gatewayRequests, 1)
}

// IncrementFallbackSuccesses increments the successful fallbacks counter.
func (mc *MetricsCollector) IncrementFallbackSuccesses() {
	atomic.AddUint64(&mc.fallbackSuccesses, 1)
}

// IncrementFallbackFailures increments the failed fallbacks counter.
func (mc *MetricsCollector) IncrementFallbackFailures() {
	atomic.AddUint64(&mc.fallbackFailures, 1)
}

// IncrementConnectionRetries atomically increments the connection retry counter.
func (mc *MetricsCollector) IncrementConnectionRetries() {
	atomic.AddUint64(&mc.connectionRetries, 1)
}

// RecordRequestDuration records the duration of a request by method.
func (mc *MetricsCollector) RecordRequestDuration(method string, duration time.Duration) {
	val, _ := mc.requestDuration.LoadOrStore(method, []time.Duration{})

	durations, ok := val.([]time.Duration)
	if !ok {
		mc.logger.Error("Invalid type stored in RequestDuration map", zap.String("method", method))

		durations = []time.Duration{}
	}

	durations = append(durations, duration)
	mc.requestDuration.Store(method, durations)
}

// RecordResponseSize records the size of a response.
func (mc *MetricsCollector) RecordResponseSize(size int) {
	mc.responseSizesMu.Lock()
	defer mc.responseSizesMu.Unlock()

	mc.responseSizes = append(mc.responseSizes, size)
}

// SetActiveConnections sets the number of active connections.
func (mc *MetricsCollector) SetActiveConnections(count int32) {
	atomic.StoreInt32(&mc.activeConnections, count)
}

// AddActiveConnection atomically increments the active connection count.
func (mc *MetricsCollector) AddActiveConnection() {
	atomic.AddInt32(&mc.activeConnections, 1)
}

// RemoveActiveConnection atomically decrements the active connection count.
func (mc *MetricsCollector) RemoveActiveConnection() {
	atomic.AddInt32(&mc.activeConnections, -1)
}

// GetMetrics returns a snapshot of current metrics.
func (mc *MetricsCollector) GetMetrics() *Metrics {
	// Copy response sizes with proper synchronization.
	mc.responseSizesMu.Lock()
	responseSizesCopy := make([]int, len(mc.responseSizes))
	copy(responseSizesCopy, mc.responseSizes)
	mc.responseSizesMu.Unlock()

	// Convert sync.Map to regular map to avoid copy lock value issue
	requestDurationCopy := make(map[string][]time.Duration)

	mc.requestDuration.Range(func(key, value interface{}) bool {
		if k, ok := key.(string); ok {
			if v, ok := value.([]time.Duration); ok {
				// Create a copy of the slice to avoid shared references
				durCopy := make([]time.Duration, len(v))
				copy(durCopy, v)
				requestDurationCopy[k] = durCopy
			}
		}

		return true
	})

	return &Metrics{
		RequestsTotal:     atomic.LoadUint64(&mc.requestsTotal),
		ResponsesTotal:    atomic.LoadUint64(&mc.responsesTotal),
		ErrorsTotal:       atomic.LoadUint64(&mc.errorsTotal),
		ConnectionRetries: atomic.LoadUint64(&mc.connectionRetries),
		DirectRequests:    atomic.LoadUint64(&mc.directRequests),
		GatewayRequests:   atomic.LoadUint64(&mc.gatewayRequests),
		FallbackSuccesses: atomic.LoadUint64(&mc.fallbackSuccesses),
		FallbackFailures:  atomic.LoadUint64(&mc.fallbackFailures),
		RequestDuration:   requestDurationCopy,
		ResponseSizes:     responseSizesCopy,
		ActiveConnections: atomic.LoadInt32(&mc.activeConnections),
	}
}

// GetRequestsTotal returns the total number of requests processed.
func (mc *MetricsCollector) GetRequestsTotal() uint64 {
	return atomic.LoadUint64(&mc.requestsTotal)
}

// GetResponsesTotal returns the total number of responses sent.
func (mc *MetricsCollector) GetResponsesTotal() uint64 {
	return atomic.LoadUint64(&mc.responsesTotal)
}

// GetErrorsTotal returns the total number of errors encountered.
func (mc *MetricsCollector) GetErrorsTotal() uint64 {
	return atomic.LoadUint64(&mc.errorsTotal)
}

// GetConnectionRetries returns the total number of connection retries.
func (mc *MetricsCollector) GetConnectionRetries() uint64 {
	return atomic.LoadUint64(&mc.connectionRetries)
}

// GetActiveConnections returns the current number of active connections.
func (mc *MetricsCollector) GetActiveConnections() int32 {
	return atomic.LoadInt32(&mc.activeConnections)
}

// Reset clears all metrics (useful for testing).
func (mc *MetricsCollector) Reset() {
	atomic.StoreUint64(&mc.requestsTotal, 0)
	atomic.StoreUint64(&mc.responsesTotal, 0)
	atomic.StoreUint64(&mc.errorsTotal, 0)
	atomic.StoreUint64(&mc.connectionRetries, 0)
	atomic.StoreInt32(&mc.activeConnections, 0)

	// Clear request duration map.
	mc.requestDuration.Range(func(key, value interface{}) bool {
		mc.requestDuration.Delete(key)

		return true
	})

	// Clear response sizes.
	mc.responseSizesMu.Lock()
	mc.responseSizes = mc.responseSizes[:0]
	mc.responseSizesMu.Unlock()
}

// LogSummary logs a summary of current metrics.
func (mc *MetricsCollector) LogSummary() {
	metrics := mc.GetMetrics()
	mc.logger.Info("Metrics summary",
		zap.Uint64("requests_total", metrics.RequestsTotal),
		zap.Uint64("responses_total", metrics.ResponsesTotal),
		zap.Uint64("errors_total", metrics.ErrorsTotal),
		zap.Uint64("connection_retries", metrics.ConnectionRetries),
		zap.Int32("active_connections", metrics.ActiveConnections),
		zap.Int("response_sizes_count", len(metrics.ResponseSizes)),
	)
}
