package metrics

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/poiley/mcp-bridge/services/router/internal/constants"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

const (
	testIterations          = 100
	testMaxIterations       = 1000
	httpStatusOK            = 200
	testTimeout             = 50
	httpStatusInternalError = 500
)

func TestPrometheusExporter_MetricsEndpoint(t *testing.T) {
	exporter := createTestPrometheusExporter(t)

	ctx, cancel := startPrometheusServer(t, exporter)
	defer cancel()

	// Get the actual address.
	addr := exporter.GetEndpoint()

	// Make request to metrics endpoint.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://%s/metrics", addr), nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to get metrics: %v", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status httpStatusOK, got %d", resp.StatusCode)
	}

	// Check content type.
	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "text/plain") {
		t.Errorf("Expected text/plain content type, got %s", contentType)
	}

	// Read and parse metrics.
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	metrics := string(body)
	validatePrometheusMetrics(t, metrics)
}

// createTestPrometheusExporter creates a configured Prometheus exporter with test metrics.
func createTestPrometheusExporter(t *testing.T) *PrometheusExporter {
	t.Helper()

	logger := zaptest.NewLogger(t)
	exporter := NewPrometheusExporter(":0", logger) // Use random port

	testMetrics := createTestRouterMetrics()

	exporter.SetRouterMetrics(func() *RouterMetrics {
		return testMetrics
	})

	return exporter
}

// createTestRouterMetrics creates test metrics data for Prometheus testing.
func createTestRouterMetrics() *RouterMetrics {
	return &RouterMetrics{
		RequestsTotal:     testIterations,
		ResponsesTotal:    95,
		ErrorsTotal:       5,
		ConnectionRetries: 3,
		ActiveConnections: 2,
		RequestDuration: map[string][]time.Duration{
			"initialize": {
				testIterations * time.Millisecond,
				httpStatusOK * time.Millisecond,
				150 * time.Millisecond,
			},
			"tools/call": {
				testTimeout * time.Millisecond,
				75 * time.Millisecond,
			},
		},
		ResponseSizes: []int{1024, 2048, 512, 4096},
	}
}

// startPrometheusServer starts the Prometheus server and waits for it to be ready.
func startPrometheusServer(t *testing.T, exporter *PrometheusExporter) (context.Context, context.CancelFunc) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		if err := exporter.Start(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Errorf("Server error: %v", err)
		}
	}()

	// Wait for server to start.
	time.Sleep(testIterations * time.Millisecond)

	return ctx, cancel
}

// validatePrometheusMetrics validates that expected metrics are present in the response.
func validatePrometheusMetrics(t *testing.T, metrics string) {
	t.Helper()

	expectedMetrics := []struct {
		name  string
		value string
	}{
		{"mcp_router_requests_total 100", ""},
		{"mcp_router_responses_total 95", ""},
		{"mcp_router_errors_total 5", ""},
		{"mcp_router_connection_retries_total 3", ""},
		{"mcp_router_active_connections 2", ""},
		{"mcp_router_up 1", ""},
	}

	for _, em := range expectedMetrics {
		if !strings.Contains(metrics, em.name) {
			t.Errorf("Metric %q not found in output", em.name)
		}
	}

	validatePrometheusHistograms(t, metrics)
}

// validatePrometheusHistograms validates that histogram metrics are present.
func validatePrometheusHistograms(t *testing.T, metrics string) {
	t.Helper()

	if !strings.Contains(metrics, "mcp_router_request_duration_seconds_bucket") {
		t.Error("Request duration histogram not found")
	}

	if !strings.Contains(metrics, `method="initialize"`) {
		t.Error("Initialize method label not found")
	}

	if !strings.Contains(metrics, `method="tools/call"`) {
		t.Error("tools/call method label not found")
	}

	if !strings.Contains(metrics, "mcp_router_response_size_bytes_bucket") {
		t.Error("Response size histogram not found")
	}
}

func TestPrometheusExporter_HealthEndpoint(t *testing.T) {
	logger := zaptest.NewLogger(t)

	exporter := NewPrometheusExporter(":0", logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		if err := exporter.Start(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Errorf("Server error: %v", err)
		}
	}()

	time.Sleep(testIterations * time.Millisecond)

	addr := exporter.GetEndpoint()

	// Make request to health endpoint.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://%s/health", addr), nil)
	if err != nil {
		t.Fatalf("Failed to create health request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to get health: %v", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status httpStatusOK, got %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected application/json content type, got %s", contentType)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	expected := `{"status":"healthy"}`
	if !strings.Contains(string(body), expected) {
		t.Errorf("Expected %s, got %s", expected, string(body))
	}
}

func TestPrometheusExporter_EmptyMetrics(t *testing.T) {
	logger := zaptest.NewLogger(t)

	exporter := NewPrometheusExporter(":0", logger)
	// Don't set metrics function - should handle nil gracefully.

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		if err := exporter.Start(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Errorf("Server error: %v", err)
		}
	}()

	time.Sleep(testIterations * time.Millisecond)

	addr := exporter.GetEndpoint()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://%s/metrics", addr), nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to get metrics: %v", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status httpStatusOK, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	// Should at least have the up metric.
	if !strings.Contains(string(body), "mcp_router_up 1") {
		t.Error("Expected up metric even with no router metrics")
	}
}

func TestPrometheusExporter_RequestDurationHistogram(t *testing.T) {
	logger := zaptest.NewLogger(t)
	exporter := NewPrometheusExporter(":0", logger)

	testMetrics := createRequestDurationTestMetrics()

	exporter.SetRouterMetrics(func() *RouterMetrics { return testMetrics })

	ctx, cancel := startPrometheusServer(t, exporter)
	defer cancel()

	time.Sleep(testIterations * time.Millisecond)

	addr := exporter.GetEndpoint()
	metrics := getPrometheusMetrics(t, ctx, addr)

	validateRequestDurationHistogram(t, metrics)
}

func createRequestDurationTestMetrics() *RouterMetrics {
	return &RouterMetrics{
		RequestDuration: map[string][]time.Duration{
			"test_method": {
				5 * time.Millisecond,
				15 * time.Millisecond,
				testTimeout * time.Millisecond,
				150 * time.Millisecond,
				httpStatusInternalError * time.Millisecond,
				1500 * time.Millisecond,
			},
		},
	}
}

func getPrometheusMetrics(t *testing.T, ctx context.Context, addr string) string {
	t.Helper()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://%s/metrics", addr), nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to get metrics: %v", err)
	}

	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	return string(body)
}

func validateRequestDurationHistogram(t *testing.T, metrics string) {
	t.Helper()

	// Parse histogram buckets.
	buckets := map[string]int{
		`le="0.005"`: 1, // 5ms
		`le="0.01"`:  1, // 5ms
		`le="0.025"`: 2, // 5ms, 15ms
		`le="0.05"`:  3, // 5ms, 15ms, 50ms
		`le="0.1"`:   3, // 5ms, 15ms, 50ms
		`le="0.25"`:  4, // + 150ms
		`le="0.5"`:   5, // + 500ms
		`le="1"`:     5, //
		`le="2.5"`:   6, // + 1500ms
		`le="+Inf"`:  6,
	}

	for bucket, expectedCount := range buckets {
		pattern := fmt.Sprintf(
			`mcp_router_request_duration_seconds_bucket{method="test_method",%s} %d`,
			bucket,
			expectedCount,
		)
		if !strings.Contains(metrics, pattern) {
			t.Errorf("Expected bucket %s with count %d not found", bucket, expectedCount)
		}
	}

	// Check sum.
	if !strings.Contains(metrics, `mcp_router_request_duration_seconds_sum{method="test_method"} 2.22`) {
		t.Error("Expected sum not found or incorrect")
	}

	// Check count.
	if !strings.Contains(metrics, `mcp_router_request_duration_seconds_count{method="test_method"} 6`) {
		t.Error("Expected count not found")
	}
}

func TestPrometheusExporter_ResponseSizeHistogram(t *testing.T) {
	logger := zaptest.NewLogger(t)
	exporter, ctx, cancel := setupHistogramTest(t, logger)
	defer cancel()

	metrics := fetchHistogramMetrics(t, ctx, exporter)
	verifyHistogramBuckets(t, metrics)
	verifyHistogramSum(t, metrics)
}

func setupHistogramTest(t *testing.T, logger *zap.Logger) (*PrometheusExporter, context.Context, context.CancelFunc) {
	t.Helper()
	exporter := NewPrometheusExporter(":0", logger)
	
	testMetrics := &RouterMetrics{
		ResponseSizes: []int{
			testTimeout,             // < testIterations
			httpStatusInternalError, // < testMaxIterations
			5000,                    // < 10000
			50000,                   // < 100000
			500000,                  // < 1000000
			5000000,                 // > 1000000
		},
	}

	exporter.SetRouterMetrics(func() *RouterMetrics {
		return testMetrics
	})

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		if err := exporter.Start(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Errorf("Server error: %v", err)
		}
	}()

	time.Sleep(testIterations * time.Millisecond)
	return exporter, ctx, cancel
}

func fetchHistogramMetrics(t *testing.T, ctx context.Context, exporter *PrometheusExporter) string {
	t.Helper()
	addr := exporter.GetEndpoint()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://%s/metrics", addr), nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to get metrics: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}
	return string(body)
}

func verifyHistogramBuckets(t *testing.T, metrics string) {
	t.Helper()
	buckets := map[string]int{
		`le="100"`:     1,
		`le="1000"`:    2,
		`le="10000"`:   3,
		`le="100000"`:  4,
		`le="1000000"`: 5,
		`le="+Inf"`:    6,
	}

	for bucket, expectedCount := range buckets {
		pattern := fmt.Sprintf(`mcp_router_response_size_bytes_bucket{%s} %d`, bucket, expectedCount)
		if !strings.Contains(metrics, pattern) {
			t.Errorf("Expected bucket %s with count %d not found", bucket, expectedCount)
		}
	}
}

func verifyHistogramSum(t *testing.T, metrics string) {
	t.Helper()
	expectedSum := testTimeout + httpStatusInternalError + 5000 + 50000 + 500000 + 5000000
	pattern := fmt.Sprintf(`mcp_router_response_size_bytes_sum %d`, expectedSum)
	if !strings.Contains(metrics, pattern) {
		t.Errorf("Expected sum %d not found", expectedSum)
	}
}

func TestPrometheusExporter_ConcurrentAccess(t *testing.T) {
	logger := zaptest.NewLogger(t)
	exporter, ctx, cancel := setupConcurrentTest(t, logger)
	defer cancel()

	addr := exporter.GetEndpoint()
	done := startMetricUpdates()
	errors := startConcurrentReads(ctx, addr)

	<-done
	verifyConcurrentAccess(t, errors)
}

func setupConcurrentTest(t *testing.T, logger *zap.Logger) (*PrometheusExporter, context.Context, context.CancelFunc) {
	t.Helper()
	exporter := NewPrometheusExporter(":0", logger)

	var requests uint64
	var responses uint64

	exporter.SetRouterMetrics(func() *RouterMetrics {
		return &RouterMetrics{
			RequestsTotal:  atomic.LoadUint64(&requests),
			ResponsesTotal: atomic.LoadUint64(&responses),
		}
	})

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		if err := exporter.Start(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Errorf("Server error: %v", err)
		}
	}()

	time.Sleep(testIterations * time.Millisecond)
	return exporter, ctx, cancel
}

func startMetricUpdates() chan bool {
	done := make(chan bool)
	var requests uint64
	var responses uint64

	go func() {
		for i := 0; i < testIterations; i++ {
			atomic.AddUint64(&requests, 1)
			atomic.AddUint64(&responses, 1)
			time.Sleep(time.Millisecond)
		}
		close(done)
	}()

	return done
}

func startConcurrentReads(ctx context.Context, addr string) chan error {
	errors := make(chan error, 10)

	for i := 0; i < constants.TestBatchSize; i++ {
		go func() {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://%s/metrics", addr), nil)
			if err != nil {
				errors <- fmt.Errorf("failed to create request: %w", err)
				return
			}

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				errors <- err
				return
			}
			_ = resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				errors <- fmt.Errorf("bad status: %d", resp.StatusCode)
			}
		}()
	}

	return errors
}

func verifyConcurrentAccess(t *testing.T, errors chan error) {
	t.Helper()
	select {
	case err := <-errors:
		t.Errorf("Concurrent access error: %v", err)
	default:
		// No errors.
	}
}

func TestPrometheusExporter_ServerShutdown(t *testing.T) {
	logger := zaptest.NewLogger(t)

	exporter := NewPrometheusExporter(":0", logger)

	ctx, cancel := context.WithCancel(context.Background())

	started := make(chan bool)
	stopped := make(chan bool)

	go func() {
		close(started)

		if err := exporter.Start(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Errorf("Server error: %v", err)
		}

		close(stopped)
	}()

	<-started
	time.Sleep(testIterations * time.Millisecond)

	// Cancel context to trigger shutdown.
	cancel()

	// Wait for shutdown with timeout.
	select {
	case <-stopped:
		// Successfully stopped.
	case <-time.After(6 * time.Second):
		t.Error("Server did not shut down within timeout")
	}
}

func TestPrometheusExporter_InvalidEndpoint(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Try to bind to an invalid address.
	exporter := NewPrometheusExporter("invalid:address:format", logger)

	ctx := context.Background()

	err := exporter.Start(ctx)
	if err == nil {
		t.Error("Expected error for invalid endpoint")
	}
}

func BenchmarkPrometheusExporter_MetricsGeneration(b *testing.B) {
	logger := zaptest.NewLogger(b)

	exporter := NewPrometheusExporter(":0", logger)

	testMetrics := &RouterMetrics{
		RequestsTotal:     1000000,
		ResponsesTotal:    999999,
		ErrorsTotal:       1,
		ConnectionRetries: 10,
		ActiveConnections: testIterations,
		RequestDuration: map[string][]time.Duration{
			"method1": generateDurations(testIterations),
			"method2": generateDurations(httpStatusOK),
			"method3": generateDurations(150),
		},
		ResponseSizes: generateSizes(httpStatusInternalError),
	}

	exporter.SetRouterMetrics(func() *RouterMetrics {
		return testMetrics
	})

	// Create a mock response writer.
	buf := &bytes.Buffer{}
	w := &mockResponseWriter{Buffer: buf}
	req := &http.Request{}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		buf.Reset()
		exporter.metricsHandler(w, req)
	}
}

// Helper functions.

func generateDurations(count int) []time.Duration {
	durations := make([]time.Duration, count)
	for i := 0; i < count; i++ {
		durations[i] = time.Duration(i%testMaxIterations) * time.Millisecond
	}

	return durations
}

func generateSizes(count int) []int {
	sizes := make([]int, count)
	for i := 0; i < count; i++ {
		sizes[i] = (i + 1) * 1024
	}

	return sizes
}

type mockResponseWriter struct {
	*bytes.Buffer
	headers http.Header
}

func (m *mockResponseWriter) Header() http.Header {
	if m.headers == nil {
		m.headers = make(http.Header)
	}

	return m.headers
}

func (m *mockResponseWriter) WriteHeader(statusCode int) {
	// Mock implementation.
}
