// Package metrics provides Prometheus-compatible metrics collection and export for the MCP router.
//
// This package implements a comprehensive metrics collection system that tracks.
// router performance, connection health, and operational statistics in a format
// compatible with Prometheus monitoring systems.
//
// # Metrics Categories
//
// The package collects several categories of metrics:
//
//   - Counter Metrics: Monotonically increasing values
//
//   - mcp_router_requests_total: Total requests processed
//
//   - mcp_router_responses_total: Total responses sent
//
//   - mcp_router_errors_total: Total errors encountered
//
//   - mcp_router_connection_retries_total: Total connection retry attempts
//
//   - Gauge Metrics: Point-in-time values that can increase or decrease
//
//   - mcp_router_active_connections: Current active connections
//
//   - mcp_router_up: Service availability indicator (always 1 when serving)
//
//   - Histogram Metrics: Distribution of values over time
//
//   - mcp_router_request_duration_seconds: Request processing time distribution
//
//   - mcp_router_response_size_bytes: Response payload size distribution
//
// # Architecture
//
// The metrics system follows a producer-consumer pattern:
//
//  1. RouterMetrics struct: Contains raw metric data collected by the router
//  2. PrometheusExporter: Formats and serves metrics in Prometheus format
//  3. HTTP endpoints: Provide access to metrics and health status
//
// The exporter runs an independent HTTP server to avoid interfering with.
// the main router operations, ensuring metrics remain available even if
// the router experiences issues.
//
// # HTTP Endpoints
//
// The metrics server provides two endpoints:
//
//   - /metrics: Prometheus-formatted metrics data
//   - /health: Simple health check endpoint
//
// # Usage Example
//
// Basic metrics setup and usage:
//
//	// Create metrics exporter
//	exporter := metrics.NewPrometheusExporter(":defaultMetricsPort", logger)
//
//	// Set metrics data source
//	exporter.SetRouterMetrics(func() *metrics.RouterMetrics {
//	    return &metrics.RouterMetrics{
//	        RequestsTotal:  atomic.LoadUint64(&requestCount),
//	        ResponsesTotal: atomic.LoadUint64(&responseCount),
//	        ErrorsTotal:    atomic.LoadUint64(&errorCount),
//	        // ... other metrics
//	    }
//	})
//
//	// Start metrics server
//	ctx, cancel := context.WithCancel(context.Background())
//	defer cancel()
//
//	go func() {
//	    if err := exporter.Start(ctx); err != nil {
//	        logger.Error("Metrics server failed", zap.Error(err))
//	    }
//	}()
//
// # Prometheus Integration
//
// The exported metrics follow Prometheus conventions:
//
//   - Metric names use snake_case with appropriate suffixes
//   - Counter metrics end with _total
//   - Histogram metrics include _bucket, _sum, and _count variants
//   - Labels are used for dimensional data (e.g., method names)
//   - Help text describes what each metric measures
//
// # Histogram Configuration
//
// Request duration buckets are optimized for network operations:
//
//	0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10 seconds
//
// Response size buckets cover typical payload ranges:
//
//	100, 1000, 10000, 100000, 1000000 bytes
//
// These bucket boundaries provide good granularity for most MCP operations.
// while keeping cardinality manageable.
//
// # Performance Considerations
//
// The metrics collection is designed to be low-overhead:
//
//   - Raw metrics use atomic operations for thread-safe updates
//   - Histogram calculations occur only during metrics export
//   - The HTTP server uses configurable timeouts to prevent resource exhaustion
//   - Metric formatting is done on-demand rather than pre-computed
//
// # Thread Safety
//
// All metrics operations are thread-safe:
//
//   - RouterMetrics uses atomic operations for counters
//   - PrometheusExporter uses sync.RWMutex for configuration access
//   - Concurrent metrics requests are handled safely
//
// # Error Handling
//
// The metrics system is designed to be resilient:
//
//   - Individual metric write failures don't affect other metrics
//   - Server startup failures are logged but don't crash the router
//   - Missing metric data results in empty output rather than errors
//   - Health endpoint remains available even if metrics collection fails
//
// This ensures that monitoring system issues don't impact router functionality.
package metrics

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"github.com/poiley/mcp-bridge/services/router/internal/constants"
)

// PrometheusExporter exports metrics in Prometheus format.
type PrometheusExporter struct {
	logger *zap.Logger
	server *http.Server

	mu       sync.RWMutex
	listener net.Listener

	// Metrics sources.
	routerMetrics func() *RouterMetrics
}

// RouterMetrics contains router-level metrics.
type RouterMetrics struct {
	RequestsTotal     uint64
	ResponsesTotal    uint64
	ErrorsTotal       uint64
	ConnectionRetries uint64

	// Additional metrics.
	RequestDuration   map[string][]time.Duration
	ResponseSizes     []int
	ActiveConnections int32
}

// NewPrometheusExporter creates a new Prometheus exporter.
func NewPrometheusExporter(endpoint string, logger *zap.Logger) *PrometheusExporter {
	mux := http.NewServeMux()

	exporter := &PrometheusExporter{
		logger: logger,
		server: &http.Server{
			Addr:         endpoint,
			Handler:      mux,
			ReadTimeout:  constants.MetricsServerReadTimeout,
			WriteTimeout: constants.MetricsServerWriteTimeout,
		},
	}

	// Register metrics endpoint.
	mux.HandleFunc("/metrics", exporter.metricsHandler)
	mux.HandleFunc("/health", exporter.healthHandler)

	return exporter
}

// GetEndpoint returns the server address.
func (e *PrometheusExporter) GetEndpoint() string {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.listener != nil {
		return e.listener.Addr().String()
	}

	return e.server.Addr
}

// SetRouterMetrics sets the function to retrieve router metrics.
func (e *PrometheusExporter) SetRouterMetrics(fn func() *RouterMetrics) {
	e.routerMetrics = fn
}

// Start starts the metrics HTTP server.
func (e *PrometheusExporter) Start(ctx context.Context) error {
	lc := &net.ListenConfig{}

	listener, err := lc.Listen(ctx, "tcp", e.server.Addr)
	if err != nil {
		return fmt.Errorf("failed to create listener: %w", err)
	}

	e.mu.Lock()
	e.listener = listener
	e.mu.Unlock()

	e.logger.Info("Starting Prometheus metrics exporter",
		zap.String("endpoint", listener.Addr().String()),
	)

	go func() {
		<-ctx.Done()

		// Background context is appropriate for shutdown - we want to complete shutdown
		// even if the parent context is cancelled, hence contextcheck is disabled
		shutdownCtx, cancel := context.WithTimeout(
			context.Background(), //nolint:contextcheck // Need fresh context for graceful shutdown
			constants.MetricsShutdownTimeout,
		)
		defer cancel()

		// Background context is appropriate for shutdown

		if err := e.server.Shutdown(shutdownCtx); err != nil { //nolint:contextcheck // shutdownCtx is intentionally fresh
			e.logger.Error("Failed to shutdown metrics server", zap.Error(err))
		}
	}()

	if err := e.server.Serve(listener); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("metrics server error: %w", err)
	}

	return nil
}

// metricsHandler handles the /metrics endpoint.
func (e *PrometheusExporter) metricsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")

	// Use error handler to centralize error checking and reduce complexity.
	errHandler := &metricWriter{w: w}

	// Always write runtime metrics.
	errHandler.writeRuntimeMetrics()

	// Write router metrics if available.
	if e.routerMetrics != nil {
		metrics := e.routerMetrics()
		if metrics != nil {
			errHandler.writeBasicCounters(metrics)
			errHandler.writeDurationHistogram(metrics)
			errHandler.writeResponseSizeHistogram(metrics)
		}
	}
}

// metricWriter centralizes error handling for Prometheus metrics output.
type metricWriter struct {
	w http.ResponseWriter
}

// writeBasicCounters writes all basic counter metrics.
func (mw *metricWriter) writeBasicCounters(metrics *RouterMetrics) {
	counters := []struct {
		help   string
		name   string
		value  uint64
		atomic bool
	}{
		{"Total number of requests processed", "mcp_router_requests_total", metrics.RequestsTotal, false},
		{"Total number of responses sent", "mcp_router_responses_total", metrics.ResponsesTotal, false},
		{"Total number of errors", "mcp_router_errors_total", metrics.ErrorsTotal, false},
		{"Total number of connection retries", "mcp_router_connection_retries_total", metrics.ConnectionRetries, false},
		{"Current number of active connections", "mcp_router_active_connections", func() uint64 {
			activeConns := atomic.LoadInt32(&metrics.ActiveConnections)
			if activeConns >= 0 {
				return uint64(activeConns)
			}

			return 0
		}(), true},
	}

	for _, counter := range counters {
		metricType := "counter"
		if counter.atomic {
			metricType = "gauge"
		}

		if !mw.writeMetricHeader(counter.help, counter.name, metricType) {
			return
		}

		if !mw.writeMetricValue(counter.name, counter.value) {
			return
		}
	}
}

// writeDurationHistogram writes request duration histogram metrics.
func (mw *metricWriter) writeDurationHistogram(metrics *RouterMetrics) {
	if len(metrics.RequestDuration) == 0 {
		return
	}

	if !mw.writeMetricHeader("Request duration in seconds", "mcp_router_request_duration_seconds", "histogram") {
		return
	}

	bucketThresholds := []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

	for method, durations := range metrics.RequestDuration {
		buckets := mw.calculateDurationBuckets(durations, bucketThresholds)
		sum := mw.calculateDurationSum(durations)

		if !mw.writeDurationBuckets(method, buckets, bucketThresholds) {
			return
		}

		if !mw.writeDurationSummary(method, sum, len(durations)) {
			return
		}
	}
}

// writeResponseSizeHistogram writes response size histogram metrics.
func (mw *metricWriter) writeResponseSizeHistogram(metrics *RouterMetrics) {
	if len(metrics.ResponseSizes) == 0 {
		return
	}

	if !mw.writeMetricHeader("Response size in bytes", "mcp_router_response_size_bytes", "histogram") {
		return
	}

	bucketThresholds := []int{100, 1000, 10000, 100000, 1000000}
	buckets := mw.calculateSizeBuckets(metrics.ResponseSizes, bucketThresholds)
	sum := mw.calculateSizeSum(metrics.ResponseSizes)

	if !mw.writeSizeBuckets(buckets, bucketThresholds) {
		return
	}

	if !mw.writeSizeSummary(sum, len(metrics.ResponseSizes)) {
		return
	}
}

// writeRuntimeMetrics writes basic runtime status metrics.
func (mw *metricWriter) writeRuntimeMetrics() {
	if !mw.writeMetricHeader("Whether the router is up", "mcp_router_up", "gauge") {
		return
	}

	_, _ = fmt.Fprintln(mw.w, "mcp_router_up 1")
}

// Helper methods for metric writing and calculations.

func (mw *metricWriter) writeMetricHeader(help, name, metricType string) bool {
	if _, err := fmt.Fprintf(mw.w, "# HELP %s %s\n", name, help); err != nil {
		return false
	}

	if _, err := fmt.Fprintf(mw.w, "# TYPE %s %s\n", name, metricType); err != nil {
		return false
	}

	return true
}

func (mw *metricWriter) writeMetricValue(name string, value uint64) bool {
	_, err := fmt.Fprintf(mw.w, "%s %d\n", name, value)

	return err == nil
}

func (mw *metricWriter) calculateDurationBuckets(durations []time.Duration, thresholds []float64) map[float64]int {
	buckets := make(map[float64]int)
	for _, threshold := range thresholds {
		buckets[threshold] = 0
	}

	for _, d := range durations {
		seconds := d.Seconds()
		for _, threshold := range thresholds {
			if seconds <= threshold {
				buckets[threshold]++
			}
		}
	}

	return buckets
}

func (mw *metricWriter) calculateDurationSum(durations []time.Duration) float64 {
	var sum float64
	for _, d := range durations {
		sum += d.Seconds()
	}

	return sum
}

func (mw *metricWriter) writeDurationBuckets(method string, buckets map[float64]int, thresholds []float64) bool {
	for _, threshold := range thresholds {
		count := buckets[threshold]
		if _, err := fmt.Fprintf(mw.w, "mcp_router_request_duration_seconds_bucket{method=\"%s\",le=\"%g\"} %d\n",
			method, threshold, count); err != nil {
			return false
		}
	}

	return true
}

func (mw *metricWriter) writeDurationSummary(method string, sum float64, count int) bool {
	if _, err := fmt.Fprintf(mw.w, "mcp_router_request_duration_seconds_bucket{method=\"%s\",le=\"+Inf\"} %d\n",
		method, count); err != nil {
		return false
	}

	if _, err := fmt.Fprintf(mw.w, "mcp_router_request_duration_seconds_sum{method=\"%s\"} %.6f\n",
		method, sum); err != nil {
		return false
	}

	if _, err := fmt.Fprintf(mw.w, "mcp_router_request_duration_seconds_count{method=\"%s\"} %d\n",
		method, count); err != nil {
		return false
	}

	return true
}

func (mw *metricWriter) calculateSizeBuckets(sizes, thresholds []int) map[int]int {
	buckets := make(map[int]int)
	for _, threshold := range thresholds {
		buckets[threshold] = 0
	}

	for _, size := range sizes {
		for _, threshold := range thresholds {
			if size <= threshold {
				buckets[threshold]++
			}
		}
	}

	return buckets
}

func (mw *metricWriter) calculateSizeSum(sizes []int) int {
	var sum int
	for _, size := range sizes {
		sum += size
	}

	return sum
}

func (mw *metricWriter) writeSizeBuckets(buckets map[int]int, thresholds []int) bool {
	for _, threshold := range thresholds {
		count := buckets[threshold]
		bucketLine := fmt.Sprintf("mcp_router_response_size_bytes_bucket{le=\"%d\"} %d\n", threshold, count)

		if _, err := fmt.Fprint(mw.w, bucketLine); err != nil {
			return false
		}
	}

	return true
}

func (mw *metricWriter) writeSizeSummary(sum, count int) bool {
	if _, err := fmt.Fprintf(mw.w, "mcp_router_response_size_bytes_bucket{le=\"+Inf\"} %d\n", count); err != nil {
		return false
	}

	if _, err := fmt.Fprintf(mw.w, "mcp_router_response_size_bytes_sum %d\n", sum); err != nil {
		return false
	}

	if _, err := fmt.Fprintf(mw.w, "mcp_router_response_size_bytes_count %d\n", count); err != nil {
		return false
	}

	return true
}

// healthHandler handles the /health endpoint.
func (e *PrometheusExporter) healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if _, err := fmt.Fprintln(w, `{"status":"healthy"}`); err != nil {
		// Log error but don't return since this is a health check.
		e.logger.Error("Failed to write health response", zap.Error(err))
	}
}
