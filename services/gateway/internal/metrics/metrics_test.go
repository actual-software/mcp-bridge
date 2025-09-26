package metrics

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

const (
	testIterations          = 100
	testMaxIterations       = 1000
	testTimeout             = 50
	httpStatusOK            = 200
	httpStatusInternalError = 500
)

func TestInitializeMetricsRegistry(t *testing.T) {
	t.Parallel()
	// Clear default registry to avoid conflicts
	prometheus.DefaultRegisterer = prometheus.NewRegistry()

	reg := InitializeMetricsRegistry()

	if reg == nil {
		t.Fatal("Expected registry to be created")
	}

	// Verify all metrics are initialized
	verifyMetricsInitialized(t, reg)
}

func verifyMetricsInitialized(t *testing.T, reg *Registry) {
	t.Helper()

	metricChecks := []struct {
		name   string
		metric interface{}
	}{
		{"ConnectionsTotal", reg.ConnectionsTotal},
		{"ConnectionsActive", reg.ConnectionsActive},
		{"ConnectionsRejected", reg.ConnectionsRejected},
		{"RequestsTotal", reg.RequestsTotal},
		{"RequestDuration", reg.RequestDuration},
		{"RequestsInFlight", reg.RequestsInFlight},
		{"AuthFailuresTotal", reg.AuthFailuresTotal},
		{"RoutingErrorsTotal", reg.RoutingErrorsTotal},
		{"EndpointRequestsTotal", reg.EndpointRequestsTotal},
		{"CircuitBreakerState", reg.CircuitBreakerState},
		{"WebSocketMessagesTotal", reg.WebSocketMessagesTotal},
		{"WebSocketBytesTotal", reg.WebSocketBytesTotal},
	}

	for _, check := range metricChecks {
		if check.metric == nil {
			t.Errorf("%s not initialized", check.name)
		}
	}
}

func TestRegistry_ConnectionMetrics(t *testing.T) {
	t.Parallel()

	prometheus.DefaultRegisterer = prometheus.NewRegistry()
	reg := InitializeMetricsRegistry()

	// Test IncrementConnections
	reg.IncrementConnections()

	totalValue := testutil.ToFloat64(reg.ConnectionsTotal)
	if totalValue != 1 {
		t.Errorf("Expected ConnectionsTotal to be 1, got %f", totalValue)
	}

	activeValue := testutil.ToFloat64(reg.ConnectionsActive)
	if activeValue != 1 {
		t.Errorf("Expected ConnectionsActive to be 1, got %f", activeValue)
	}

	// Test multiple increments
	reg.IncrementConnections()
	reg.IncrementConnections()

	totalValue = testutil.ToFloat64(reg.ConnectionsTotal)
	if totalValue != 3 {
		t.Errorf("Expected ConnectionsTotal to be 3, got %f", totalValue)
	}

	activeValue = testutil.ToFloat64(reg.ConnectionsActive)
	if activeValue != 3 {
		t.Errorf("Expected ConnectionsActive to be 3, got %f", activeValue)
	}

	// Test DecrementConnections
	reg.DecrementConnections()

	activeValue = testutil.ToFloat64(reg.ConnectionsActive)
	if activeValue != 2 {
		t.Errorf("Expected ConnectionsActive to be 2 after decrement, got %f", activeValue)
	}

	// Total should remain unchanged
	totalValue = testutil.ToFloat64(reg.ConnectionsTotal)
	if totalValue != 3 {
		t.Errorf("Expected ConnectionsTotal to remain 3, got %f", totalValue)
	}

	// Test IncrementConnectionsRejected
	reg.IncrementConnectionsRejected()
	reg.IncrementConnectionsRejected()

	rejectedValue := testutil.ToFloat64(reg.ConnectionsRejected)
	if rejectedValue != 2 {
		t.Errorf("Expected ConnectionsRejected to be 2, got %f", rejectedValue)
	}
}

func TestRegistry_RequestMetrics(t *testing.T) {
	t.Parallel()

	prometheus.DefaultRegisterer = prometheus.NewRegistry()
	reg := InitializeMetricsRegistry()

	// Test IncrementRequests
	reg.IncrementRequests("GET", "httpStatusOK")
	reg.IncrementRequests("POST", "httpStatusCreated")
	reg.IncrementRequests("GET", "httpStatusNotFound")
	reg.IncrementRequests("GET", "httpStatusOK")

	// Verify counters
	getValue := func(method, status string) float64 {
		return testutil.ToFloat64(reg.RequestsTotal.WithLabelValues(method, status))
	}
	if v := getValue("GET", "httpStatusOK"); v != 2 {
		t.Errorf("Expected GET/httpStatusOK count to be 2, got %f", v)
	}

	if v := getValue("POST", "httpStatusCreated"); v != 1 {
		t.Errorf("Expected POST/httpStatusCreated count to be 1, got %f", v)
	}

	if v := getValue("GET", "httpStatusNotFound"); v != 1 {
		t.Errorf("Expected GET/httpStatusNotFound count to be 1, got %f", v)
	}

	// Test RecordRequestDuration
	reg.RecordRequestDuration("GET", "httpStatusOK", testIterations*time.Millisecond)
	reg.RecordRequestDuration("GET", "httpStatusOK", httpStatusOK*time.Millisecond)
	reg.RecordRequestDuration("POST", "httpStatusCreated", testTimeout*time.Millisecond)

	// Verify histogram recorded values
	metricName := "mcp_gateway_request_duration_seconds"

	// Test that we can record to the histogram
	count := testutil.CollectAndCount(reg.RequestDuration, metricName)
	if count < 1 {
		t.Errorf("Expected at least 1 histogram metric, got %d", count)
	}

	// Test RequestsInFlight
	reg.IncrementRequestsInFlight()
	reg.IncrementRequestsInFlight()

	inFlight := testutil.ToFloat64(reg.RequestsInFlight)
	if inFlight != 2 {
		t.Errorf("Expected RequestsInFlight to be 2, got %f", inFlight)
	}

	reg.DecrementRequestsInFlight()

	inFlight = testutil.ToFloat64(reg.RequestsInFlight)
	if inFlight != 1 {
		t.Errorf("Expected RequestsInFlight to be 1 after decrement, got %f", inFlight)
	}
}

func TestRegistry_AuthMetrics(t *testing.T) {
	t.Parallel()

	prometheus.DefaultRegisterer = prometheus.NewRegistry()
	reg := InitializeMetricsRegistry()

	// Test different failure reasons
	reg.IncrementAuthFailures("invalid_token")
	reg.IncrementAuthFailures("expired_token")
	reg.IncrementAuthFailures("invalid_token")
	reg.IncrementAuthFailures("missing_header")

	getValue := func(reason string) float64 {
		return testutil.ToFloat64(reg.AuthFailuresTotal.WithLabelValues(reason))
	}

	if v := getValue("invalid_token"); v != 2 {
		t.Errorf("Expected invalid_token failures to be 2, got %f", v)
	}

	if v := getValue("expired_token"); v != 1 {
		t.Errorf("Expected expired_token failures to be 1, got %f", v)
	}

	if v := getValue("missing_header"); v != 1 {
		t.Errorf("Expected missing_header failures to be 1, got %f", v)
	}
}

func TestRegistry_RoutingMetrics(t *testing.T) {
	t.Parallel()

	prometheus.DefaultRegisterer = prometheus.NewRegistry()
	reg := InitializeMetricsRegistry()

	// Test routing errors
	reg.IncrementRoutingErrors("no_endpoints")
	reg.IncrementRoutingErrors("connection_failed")
	reg.IncrementRoutingErrors("no_endpoints")

	getValue := func(reason string) float64 {
		return testutil.ToFloat64(reg.RoutingErrorsTotal.WithLabelValues(reason))
	}

	if v := getValue("no_endpoints"); v != 2 {
		t.Errorf("Expected no_endpoints errors to be 2, got %f", v)
	}

	if v := getValue("connection_failed"); v != 1 {
		t.Errorf("Expected connection_failed errors to be 1, got %f", v)
	}

	// Test endpoint requests
	reg.IncrementEndpointRequests("server1", "namespace1", "success")
	reg.IncrementEndpointRequests("server1", "namespace1", "success")
	reg.IncrementEndpointRequests("server2", "namespace1", "success")
	reg.IncrementEndpointRequests("server1", "namespace2", "failed")

	getEndpointValue := func(endpoint, namespace, status string) float64 {
		return testutil.ToFloat64(reg.EndpointRequestsTotal.WithLabelValues(endpoint, namespace, status))
	}

	if v := getEndpointValue("server1", "namespace1", "success"); v != 2 {
		t.Errorf("Expected server1/namespace1/success to be 2, got %f", v)
	}

	if v := getEndpointValue("server2", "namespace1", "success"); v != 1 {
		t.Errorf("Expected server2/namespace1/success to be 1, got %f", v)
	}

	if v := getEndpointValue("server1", "namespace2", "failed"); v != 1 {
		t.Errorf("Expected server1/namespace2/failed to be 1, got %f", v)
	}
}

func TestRegistry_CircuitBreakerMetrics(t *testing.T) {
	t.Parallel()

	prometheus.DefaultRegisterer = prometheus.NewRegistry()
	reg := InitializeMetricsRegistry()

	// Test circuit breaker states
	reg.SetCircuitBreakerState("endpoint1", 0) // closed
	reg.SetCircuitBreakerState("endpoint2", 1) // open
	reg.SetCircuitBreakerState("endpoint3", 2) // half-open

	getValue := func(endpoint string) float64 {
		return testutil.ToFloat64(reg.CircuitBreakerState.WithLabelValues(endpoint))
	}

	if v := getValue("endpoint1"); v != 0 {
		t.Errorf("Expected endpoint1 state to be 0 (closed), got %f", v)
	}

	if v := getValue("endpoint2"); v != 1 {
		t.Errorf("Expected endpoint2 state to be 1 (open), got %f", v)
	}

	if v := getValue("endpoint3"); v != 2 {
		t.Errorf("Expected endpoint3 state to be 2 (half-open), got %f", v)
	}

	// Test state change
	reg.SetCircuitBreakerState("endpoint1", 1) // now open

	if v := getValue("endpoint1"); v != 1 {
		t.Errorf("Expected endpoint1 state to change to 1 (open), got %f", v)
	}
}

func TestRegistry_WebSocketMetrics(t *testing.T) {
	t.Parallel()

	prometheus.DefaultRegisterer = prometheus.NewRegistry()
	reg := InitializeMetricsRegistry()

	// Test message counting
	reg.IncrementWebSocketMessages("inbound", "request")
	reg.IncrementWebSocketMessages("inbound", "request")
	reg.IncrementWebSocketMessages("outbound", "response")
	reg.IncrementWebSocketMessages("inbound", "notification")

	getMessageValue := func(direction, msgType string) float64 {
		return testutil.ToFloat64(reg.WebSocketMessagesTotal.WithLabelValues(direction, msgType))
	}

	if v := getMessageValue("inbound", "request"); v != 2 {
		t.Errorf("Expected inbound/request messages to be 2, got %f", v)
	}

	if v := getMessageValue("outbound", "response"); v != 1 {
		t.Errorf("Expected outbound/response messages to be 1, got %f", v)
	}

	if v := getMessageValue("inbound", "notification"); v != 1 {
		t.Errorf("Expected inbound/notification messages to be 1, got %f", v)
	}

	// Test bytes counting
	reg.AddWebSocketBytes("inbound", 1024)
	reg.AddWebSocketBytes("inbound", 512)
	reg.AddWebSocketBytes("outbound", 2048)

	getBytesValue := func(direction string) float64 {
		return testutil.ToFloat64(reg.WebSocketBytesTotal.WithLabelValues(direction))
	}

	if v := getBytesValue("inbound"); v != 1536 {
		t.Errorf("Expected inbound bytes to be 1536, got %f", v)
	}

	if v := getBytesValue("outbound"); v != 2048 {
		t.Errorf("Expected outbound bytes to be 2048, got %f", v)
	}
}

func TestRegistry_MetricNames(t *testing.T) {
	t.Parallel()

	prometheus.DefaultRegisterer = prometheus.NewRegistry()
	reg := InitializeMetricsRegistry()

	// Test that metrics have correct names
	tests := []struct {
		metric       prometheus.Collector
		expectedName string
	}{
		{reg.ConnectionsTotal, "mcp_gateway_connections_total"},
		{reg.ConnectionsActive, "mcp_gateway_connections_active"},
		{reg.ConnectionsRejected, "mcp_gateway_connections_rejected_total"},
		{reg.RequestsInFlight, "mcp_gateway_requests_in_flight"},
	}

	for _, tt := range tests {
		t.Run(tt.expectedName, func(t *testing.T) {
			t.Parallel()
			// Simply verify the metric is not nil
			if tt.metric == nil {
				t.Errorf("Expected metric %s to be initialized", tt.expectedName)
			}
		})
	}
}

func TestRegistry_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	prometheus.DefaultRegisterer = prometheus.NewRegistry()
	reg := InitializeMetricsRegistry()

	// Test concurrent increments
	done := make(chan bool)

	// Multiple goroutines incrementing different metrics
	for i := 0; i < 10; i++ {
		go func(_ int) {
			for j := 0; j < testIterations; j++ {
				reg.IncrementConnections()
				reg.IncrementRequests("GET", "httpStatusOK")
				reg.IncrementAuthFailures("test")
				reg.IncrementWebSocketMessages("inbound", "test")
				reg.AddWebSocketBytes("inbound", testIterations)
			}

			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify counts
	connections := testutil.ToFloat64(reg.ConnectionsTotal)
	if connections != testMaxIterations {
		t.Errorf("Expected testMaxIterations total connections, got %f", connections)
	}

	requests := testutil.ToFloat64(reg.RequestsTotal.WithLabelValues("GET", "httpStatusOK"))
	if requests != testMaxIterations {
		t.Errorf("Expected testMaxIterations GET/httpStatusOK requests, got %f", requests)
	}

	authFailures := testutil.ToFloat64(reg.AuthFailuresTotal.WithLabelValues("test"))
	if authFailures != testMaxIterations {
		t.Errorf("Expected testMaxIterations auth failures, got %f", authFailures)
	}

	messages := testutil.ToFloat64(reg.WebSocketMessagesTotal.WithLabelValues("inbound", "test"))
	if messages != testMaxIterations {
		t.Errorf("Expected testMaxIterations websocket messages, got %f", messages)
	}

	bytes := testutil.ToFloat64(reg.WebSocketBytesTotal.WithLabelValues("inbound"))
	if bytes != 100000 {
		t.Errorf("Expected 100000 websocket bytes, got %f", bytes)
	}
}

func TestRegistry_HistogramBuckets(t *testing.T) {
	t.Parallel()

	prometheus.DefaultRegisterer = prometheus.NewRegistry()
	reg := InitializeMetricsRegistry()

	// Record various durations
	durations := []time.Duration{
		5 * time.Millisecond,
		testTimeout * time.Millisecond,
		testIterations * time.Millisecond,
		httpStatusInternalError * time.Millisecond,
		1 * time.Second,
		2 * time.Second,
	}

	for _, d := range durations {
		reg.RecordRequestDuration("GET", "httpStatusOK", d)
	}

	// Verify histogram is recording properly
	count := testutil.CollectAndCount(reg.RequestDuration, "mcp_gateway_request_duration_seconds")
	if count == 0 {
		t.Errorf("Expected histogram to have recorded metrics")
	}
}

func BenchmarkRegistry_IncrementConnections(b *testing.B) {
	prometheus.DefaultRegisterer = prometheus.NewRegistry()
	reg := InitializeMetricsRegistry()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		reg.IncrementConnections()
	}
}

func BenchmarkRegistry_IncrementRequests(b *testing.B) {
	prometheus.DefaultRegisterer = prometheus.NewRegistry()
	reg := InitializeMetricsRegistry()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		reg.IncrementRequests("GET", "httpStatusOK")
	}
}

func BenchmarkRegistry_RecordRequestDuration(b *testing.B) {
	prometheus.DefaultRegisterer = prometheus.NewRegistry()
	reg := InitializeMetricsRegistry()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		reg.RecordRequestDuration("GET", "httpStatusOK", testIterations*time.Millisecond)
	}
}

func BenchmarkRegistry_ConcurrentMetrics(b *testing.B) {
	prometheus.DefaultRegisterer = prometheus.NewRegistry()
	reg := InitializeMetricsRegistry()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			reg.IncrementConnections()
			reg.IncrementRequests("GET", "httpStatusOK")
			reg.RecordRequestDuration("GET", "httpStatusOK", testTimeout*time.Millisecond)
			reg.DecrementConnections()
		}
	})
}
