package constants

import (
	"testing"
	"time"
)

const (
	testIterations = 100
)

func TestMetricsTimeouts(t *testing.T) {
	tests := []struct {
		name     string
		timeout  time.Duration
		minValue time.Duration
		maxValue time.Duration
		reason   string
	}{
		{
			name:     "MetricsServerReadTimeout",
			timeout:  MetricsServerReadTimeout,
			minValue: 1 * time.Second,
			maxValue: 30 * time.Second,
			reason:   "should be reasonable for metrics collection",
		},
		{
			name:     "MetricsServerWriteTimeout",
			timeout:  MetricsServerWriteTimeout,
			minValue: 1 * time.Second,
			maxValue: 30 * time.Second,
			reason:   "should be sufficient for metrics payload transmission",
		},
		{
			name:     "MetricsShutdownTimeout",
			timeout:  MetricsShutdownTimeout,
			minValue: 1 * time.Second,
			maxValue: 15 * time.Second,
			reason:   "should balance graceful shutdown with reasonable termination time",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.timeout < tt.minValue {
				t.Errorf("%s is too small: %v < %v (%s)", tt.name, tt.timeout, tt.minValue, tt.reason)
			}

			if tt.timeout > tt.maxValue {
				t.Errorf("%s is too large: %v > %v (%s)", tt.name, tt.timeout, tt.maxValue, tt.reason)
			}

			if tt.timeout <= 0 {
				t.Errorf("%s must be positive: %v", tt.name, tt.timeout)
			}
		})
	}
}

func TestGatewayTimeouts(t *testing.T) {
	tests := []struct {
		name     string
		timeout  time.Duration
		minValue time.Duration
		maxValue time.Duration
		reason   string
	}{
		{
			name:     "DefaultGatewayTimeout",
			timeout:  DefaultGatewayTimeout,
			minValue: 5 * time.Second,
			maxValue: 5 * time.Minute,
			reason:   "should account for network round-trip times plus processing",
		},
		{
			name:     "GatewayWriteDeadline",
			timeout:  GatewayWriteDeadline,
			minValue: 1 * time.Second,
			maxValue: 60 * time.Second,
			reason:   "must be long enough for typical message sizes",
		},
		{
			name:     "GatewayReadDeadline",
			timeout:  GatewayReadDeadline,
			minValue: 5 * time.Second,
			maxValue: 5 * time.Minute,
			reason:   "should balance responsiveness with network latency tolerance",
		},
		{
			name:     "GatewayPingDeadline",
			timeout:  GatewayPingDeadline,
			minValue: 1 * time.Second,
			maxValue: 30 * time.Second,
			reason:   "should be shorter than normal operations for faster failure detection",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.timeout < tt.minValue {
				t.Errorf("%s is too small: %v < %v (%s)", tt.name, tt.timeout, tt.minValue, tt.reason)
			}

			if tt.timeout > tt.maxValue {
				t.Errorf("%s is too large: %v > %v (%s)", tt.name, tt.timeout, tt.maxValue, tt.reason)
			}

			if tt.timeout <= 0 {
				t.Errorf("%s must be positive: %v", tt.name, tt.timeout)
			}
		})
	}

	// Test relationships between timeouts.
	if GatewayPingDeadline >= GatewayReadDeadline {
		t.Error("GatewayPingDeadline should be shorter than GatewayReadDeadline for faster failure detection")
	}

	if GatewayWriteDeadline > GatewayReadDeadline {
		t.Error("GatewayWriteDeadline should not exceed GatewayReadDeadline")
	}
}

func TestConnectionPoolTimeouts(t *testing.T) {
	tests := createConnectionPoolTests()
	runConnectionPoolTests(t, tests)
	verifyConnectionPoolRelationships(t)
}

func TestCircuitBreakerTimeouts(t *testing.T) {
	tests := []struct {
		name     string
		timeout  time.Duration
		minValue time.Duration
		maxValue time.Duration
		reason   string
	}{
		{
			name:     "DefaultCircuitBreakerRecoveryTimeout",
			timeout:  DefaultCircuitBreakerRecoveryTimeout,
			minValue: 5 * time.Second,
			maxValue: 5 * time.Minute,
			reason:   "should be based on typical service restart and warmup times",
		},
		{
			name:     "DefaultCircuitBreakerTimeout",
			timeout:  DefaultCircuitBreakerTimeout,
			minValue: 1 * time.Second,
			maxValue: 60 * time.Second,
			reason:   "should be shorter than client timeouts to enable proper error handling",
		},
		{
			name:     "DefaultCircuitBreakerMonitoringWindow",
			timeout:  DefaultCircuitBreakerMonitoringWindow,
			minValue: 10 * time.Second,
			maxValue: 10 * time.Minute,
			reason:   "should provide good statistical significance",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.timeout < tt.minValue {
				t.Errorf("%s is too small: %v < %v (%s)", tt.name, tt.timeout, tt.minValue, tt.reason)
			}

			if tt.timeout > tt.maxValue {
				t.Errorf("%s is too large: %v > %v (%s)", tt.name, tt.timeout, tt.maxValue, tt.reason)
			}

			if tt.timeout <= 0 {
				t.Errorf("%s must be positive: %v", tt.name, tt.timeout)
			}
		})
	}

	// Test relationships.
	if DefaultCircuitBreakerTimeout >= DefaultCircuitBreakerRecoveryTimeout {
		t.Error("DefaultCircuitBreakerTimeout should be less than DefaultCircuitBreakerRecoveryTimeout")
	}

	if DefaultCircuitBreakerRecoveryTimeout >= DefaultCircuitBreakerMonitoringWindow {
		t.Error("DefaultCircuitBreakerRecoveryTimeout should be less than DefaultCircuitBreakerMonitoringWindow")
	}
}

func TestRateLimiterTimeouts(t *testing.T) {
	tests := []struct {
		name     string
		timeout  time.Duration
		minValue time.Duration
		maxValue time.Duration
		reason   string
	}{
		{
			name:     "DefaultTokenBucketRefillInterval",
			timeout:  DefaultTokenBucketRefillInterval,
			minValue: 1 * time.Second,
			maxValue: 5 * time.Minute,
			reason:   "should provide reasonable refill granularity",
		},
		{
			name:     "RateLimiterBackoffMin",
			timeout:  RateLimiterBackoffMin,
			minValue: 1 * time.Millisecond,
			maxValue: testIterations * time.Millisecond,
			reason:   "should be barely perceptible to users",
		},
		{
			name:     "RateLimiterBackoffMax",
			timeout:  RateLimiterBackoffMax,
			minValue: testIterations * time.Millisecond,
			maxValue: 10 * time.Second,
			reason:   "should be reasonable maximum wait for rate limiting",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.timeout < tt.minValue {
				t.Errorf("%s is too small: %v < %v (%s)", tt.name, tt.timeout, tt.minValue, tt.reason)
			}

			if tt.timeout > tt.maxValue {
				t.Errorf("%s is too large: %v > %v (%s)", tt.name, tt.timeout, tt.maxValue, tt.reason)
			}

			if tt.timeout <= 0 {
				t.Errorf("%s must be positive: %v", tt.name, tt.timeout)
			}
		})
	}

	// Test relationships.
	if RateLimiterBackoffMin >= RateLimiterBackoffMax {
		t.Error("RateLimiterBackoffMin should be less than RateLimiterBackoffMax")
	}
}

func TestConnectionTimeouts(t *testing.T) {
	tests := []struct {
		name     string
		timeout  time.Duration
		minValue time.Duration
		maxValue time.Duration
		reason   string
	}{
		{
			name:     "ConnectionRetryBackoffMin",
			timeout:  ConnectionRetryBackoffMin,
			minValue: testIterations * time.Millisecond,
			maxValue: 10 * time.Second,
			reason:   "should allow brief network interruptions to resolve",
		},
		{
			name:     "ConnectionRetryBackoffMax",
			timeout:  ConnectionRetryBackoffMax,
			minValue: 10 * time.Second,
			maxValue: 5 * time.Minute,
			reason:   "should be reasonable maximum backoff for connection issues",
		},
		{
			name:     "ConnectionKeepaliveInterval",
			timeout:  ConnectionKeepaliveInterval,
			minValue: 5 * time.Second,
			maxValue: 5 * time.Minute,
			reason:   "should follow standard keepalive protocols",
		},
		{
			name:     "ConnectionEstablishTimeout",
			timeout:  ConnectionEstablishTimeout,
			minValue: 5 * time.Second,
			maxValue: 2 * time.Minute,
			reason:   "should handle slow DNS resolution and connection setup",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.timeout < tt.minValue {
				t.Errorf("%s is too small: %v < %v (%s)", tt.name, tt.timeout, tt.minValue, tt.reason)
			}

			if tt.timeout > tt.maxValue {
				t.Errorf("%s is too large: %v > %v (%s)", tt.name, tt.timeout, tt.maxValue, tt.reason)
			}

			if tt.timeout <= 0 {
				t.Errorf("%s must be positive: %v", tt.name, tt.timeout)
			}
		})
	}

	// Test relationships.
	if ConnectionRetryBackoffMin >= ConnectionRetryBackoffMax {
		t.Error("ConnectionRetryBackoffMin should be less than ConnectionRetryBackoffMax")
	}
}

func TestRouterCoreTimeouts(t *testing.T) {
	tests := []struct {
		name     string
		timeout  time.Duration
		minValue time.Duration
		maxValue time.Duration
		reason   string
	}{
		{
			name:     "DefaultReceiveTimeout",
			timeout:  DefaultReceiveTimeout,
			minValue: 1 * time.Second,
			maxValue: 30 * time.Second,
			reason:   "should provide responsive feel while allowing processing time",
		},
		{
			name:     "ConnectionStateCheckInterval",
			timeout:  ConnectionStateCheckInterval,
			minValue: 10 * time.Millisecond,
			maxValue: 1 * time.Second,
			reason:   "should provide sub-second responsiveness for state monitoring",
		},
		{
			name:     "ShortStateCheckInterval",
			timeout:  ShortStateCheckInterval,
			minValue: 1 * time.Millisecond,
			maxValue: testIterations * time.Millisecond,
			reason:   "should enable tight control loops without excessive overhead",
		},
		{
			name:     "ShutdownTimeout",
			timeout:  ShutdownTimeout,
			minValue: 1 * time.Second,
			maxValue: 30 * time.Second,
			reason:   "should balance graceful shutdown with reasonable termination",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.timeout < tt.minValue {
				t.Errorf("%s is too small: %v < %v (%s)", tt.name, tt.timeout, tt.minValue, tt.reason)
			}

			if tt.timeout > tt.maxValue {
				t.Errorf("%s is too large: %v > %v (%s)", tt.name, tt.timeout, tt.maxValue, tt.reason)
			}

			if tt.timeout <= 0 {
				t.Errorf("%s must be positive: %v", tt.name, tt.timeout)
			}
		})
	}

	// Test relationships.
	if ShortStateCheckInterval >= ConnectionStateCheckInterval {
		t.Error("ShortStateCheckInterval should be less than ConnectionStateCheckInterval")
	}

	if ConnectionStateCheckInterval >= DefaultReceiveTimeout {
		t.Error("ConnectionStateCheckInterval should be much less than DefaultReceiveTimeout")
	}
}

func TestAuthenticationTimeouts(t *testing.T) {
	tests := []struct {
		name     string
		timeout  time.Duration
		minValue time.Duration
		maxValue time.Duration
		reason   string
	}{
		{
			name:     "OAuth2TokenRefreshBuffer",
			timeout:  OAuth2TokenRefreshBuffer,
			minValue: 10 * time.Second,
			maxValue: 5 * time.Minute,
			reason:   "should provide safety margin for clock skew and processing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.timeout < tt.minValue {
				t.Errorf("%s is too small: %v < %v (%s)", tt.name, tt.timeout, tt.minValue, tt.reason)
			}

			if tt.timeout > tt.maxValue {
				t.Errorf("%s is too large: %v > %v (%s)", tt.name, tt.timeout, tt.maxValue, tt.reason)
			}

			if tt.timeout <= 0 {
				t.Errorf("%s must be positive: %v", tt.name, tt.timeout)
			}
		})
	}
}

func TestTimeoutConsistency(t *testing.T) {
	consistencyTests := createTimeoutConsistencyTests()
	runTimeoutConsistencyTests(t, consistencyTests)
}

func TestTimeoutScaling(t *testing.T) {
	// Test that timeouts scale appropriately relative to each other.
	scalingTests := []struct {
		name      string
		base      time.Duration
		baseName  string
		scaled    time.Duration
		scaleName string
		minRatio  float64
		maxRatio  float64
		reason    string
	}{
		{
			name:      "Health check vs max idle time",
			base:      DefaultHealthCheckInterval,
			baseName:  "DefaultHealthCheckInterval",
			scaled:    DefaultMaxIdleTime,
			scaleName: "DefaultMaxIdleTime",
			minRatio:  2.0,
			maxRatio:  20.0,
			reason:    "idle time should be several times longer than health check interval",
		},
		{
			name:      "Pool cleanup vs max lifetime",
			base:      PoolCleanupTimeout,
			baseName:  "PoolCleanupTimeout",
			scaled:    DefaultMaxLifetime,
			scaleName: "DefaultMaxLifetime",
			minRatio:  2.0,
			maxRatio:  100.0,
			reason:    "max lifetime should be much longer than cleanup timeout",
		},
		{
			name:      "Circuit breaker monitoring vs recovery",
			base:      DefaultCircuitBreakerRecoveryTimeout,
			baseName:  "DefaultCircuitBreakerRecoveryTimeout",
			scaled:    DefaultCircuitBreakerMonitoringWindow,
			scaleName: "DefaultCircuitBreakerMonitoringWindow",
			minRatio:  1.5,
			maxRatio:  10.0,
			reason:    "monitoring window should be longer than recovery timeout",
		},
	}

	for _, tt := range scalingTests {
		t.Run(tt.name, func(t *testing.T) {
			ratio := float64(tt.scaled) / float64(tt.base)
			if ratio < tt.minRatio {
				t.Errorf("%s to %s ratio too small: %.2f < %.2f (%s)",
					tt.baseName, tt.scaleName, ratio, tt.minRatio, tt.reason)
			}

			if ratio > tt.maxRatio {
				t.Errorf("%s to %s ratio too large: %.2f > %.2f (%s)",
					tt.baseName, tt.scaleName, ratio, tt.maxRatio, tt.reason)
			}
		})
	}
}

func BenchmarkTimeoutConstants(b *testing.B) {
	// Benchmark accessing timeout constants (should be negligible).
	timeouts := []time.Duration{
		MetricsServerReadTimeout,
		MetricsServerWriteTimeout,
		MetricsShutdownTimeout,
		DefaultGatewayTimeout,
		GatewayWriteDeadline,
		GatewayReadDeadline,
		GatewayPingDeadline,
		DefaultMaxIdleTime,
		DefaultMaxLifetime,
		DefaultAcquireTimeout,
		DefaultHealthCheckInterval,
		PoolCleanupTimeout,
		PoolStatisticsInterval,
		DefaultCircuitBreakerRecoveryTimeout,
		DefaultCircuitBreakerTimeout,
		DefaultCircuitBreakerMonitoringWindow,
		DefaultTokenBucketRefillInterval,
		RateLimiterBackoffMin,
		RateLimiterBackoffMax,
		ConnectionRetryBackoffMin,
		ConnectionRetryBackoffMax,
		ConnectionKeepaliveInterval,
		ConnectionEstablishTimeout,
		DefaultReceiveTimeout,
		ConnectionStateCheckInterval,
		ShortStateCheckInterval,
		ShutdownTimeout,
		OAuth2TokenRefreshBuffer,
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		for _, timeout := range timeouts {
			_ = timeout.Nanoseconds()
		}
	}
}

func TestTimeoutDocumentation(t *testing.T) {
	// Verify that timeout constants have meaningful values based on their documentation.
	documentationTests := []struct {
		name     string
		timeout  time.Duration
		expected time.Duration
		reason   string
	}{
		{
			name:     "MetricsServerReadTimeout matches documentation",
			timeout:  MetricsServerReadTimeout,
			expected: 10 * time.Second,
			reason:   "should match documented value for metrics collection",
		},
		{
			name:     "DefaultGatewayTimeout matches documentation",
			timeout:  DefaultGatewayTimeout,
			expected: 30 * time.Second,
			reason:   "should match documented network timeout",
		},
		{
			name:     "DefaultMaxIdleTime matches documentation",
			timeout:  DefaultMaxIdleTime,
			expected: 5 * time.Minute,
			reason:   "should match documented connection reuse timeout",
		},
		{
			name:     "DefaultReceiveTimeout matches documentation",
			timeout:  DefaultReceiveTimeout,
			expected: 5 * time.Second,
			reason:   "should match documented responsive timeout",
		},
		{
			name:     "OAuth2TokenRefreshBuffer matches documentation",
			timeout:  OAuth2TokenRefreshBuffer,
			expected: 30 * time.Second,
			reason:   "should match documented clock skew buffer",
		},
	}

	for _, tt := range documentationTests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.timeout != tt.expected {
				t.Errorf("%s: got %v, expected %v (%s)",
					tt.name, tt.timeout, tt.expected, tt.reason)
			}
		})
	}
}

// Helper functions for TestConnectionPoolTimeouts
func createConnectionPoolTests() []struct {
	name     string
	timeout  time.Duration
	minValue time.Duration
	maxValue time.Duration
	reason   string
} {
	return []struct {
		name     string
		timeout  time.Duration
		minValue time.Duration
		maxValue time.Duration
		reason   string
	}{
		{
			name:     "DefaultMaxIdleTime",
			timeout:  DefaultMaxIdleTime,
			minValue: 30 * time.Second,
			maxValue: 30 * time.Minute,
			reason:   "should be longer than typical request intervals to enable reuse",
		},
		{
			name:     "DefaultMaxLifetime",
			timeout:  DefaultMaxLifetime,
			minValue: 5 * time.Minute,
			maxValue: 2 * time.Hour,
			reason:   "should force periodic connection refresh for stability",
		},
		{
			name:     "DefaultAcquireTimeout",
			timeout:  DefaultAcquireTimeout,
			minValue: 1 * time.Second,
			maxValue: 30 * time.Second,
			reason:   "should allow for connection creation but prevent UI blocking",
		},
		{
			name:     "DefaultHealthCheckInterval",
			timeout:  DefaultHealthCheckInterval,
			minValue: 5 * time.Second,
			maxValue: 5 * time.Minute,
			reason:   "should balance reliability with performance impact",
		},
		{
			name:     "PoolCleanupTimeout",
			timeout:  PoolCleanupTimeout,
			minValue: 5 * time.Second,
			maxValue: 2 * time.Minute,
			reason:   "should provide reasonable time for cleanup without delaying shutdown",
		},
		{
			name:     "PoolStatisticsInterval",
			timeout:  PoolStatisticsInterval,
			minValue: 1 * time.Second,
			maxValue: 60 * time.Second,
			reason:   "should provide good granularity for observability",
		},
	}
}

func runConnectionPoolTests(t *testing.T, tests []struct {
	name     string
	timeout  time.Duration
	minValue time.Duration
	maxValue time.Duration
	reason   string
}) {
	t.Helper()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.timeout < tt.minValue {
				t.Errorf("%s is too small: %v < %v (%s)", tt.name, tt.timeout, tt.minValue, tt.reason)
			}

			if tt.timeout > tt.maxValue {
				t.Errorf("%s is too large: %v > %v (%s)", tt.name, tt.timeout, tt.maxValue, tt.reason)
			}

			if tt.timeout <= 0 {
				t.Errorf("%s must be positive: %v", tt.name, tt.timeout)
			}
		})
	}
}

func verifyConnectionPoolRelationships(t *testing.T) {
	t.Helper()

	if DefaultMaxIdleTime >= DefaultMaxLifetime {
		t.Error("DefaultMaxIdleTime should be less than DefaultMaxLifetime")
	}

	if DefaultAcquireTimeout >= DefaultMaxIdleTime {
		t.Error("DefaultAcquireTimeout should be much less than DefaultMaxIdleTime")
	}
}

// Helper functions for TestTimeoutConsistency
func createTimeoutConsistencyTests() []struct {
	name        string
	smaller     time.Duration
	smallerName string
	larger      time.Duration
	largerName  string
	reason      string
} {
	var tests []struct {
		name        string
		smaller     time.Duration
		smallerName string
		larger      time.Duration
		largerName  string
		reason      string
	}

	tests = append(tests, createGatewayTimeoutTests()...)
	tests = append(tests, createConnectionTimeoutTests()...)
	tests = append(tests, createBackoffTimeoutTests()...)

	return tests
}

func createGatewayTimeoutTests() []struct {
	name        string
	smaller     time.Duration
	smallerName string
	larger      time.Duration
	largerName  string
	reason      string
} {
	return []struct {
		name        string
		smaller     time.Duration
		smallerName string
		larger      time.Duration
		largerName  string
		reason      string
	}{
		{
			name:        "Ping deadline vs read deadline",
			smaller:     GatewayPingDeadline,
			smallerName: "GatewayPingDeadline",
			larger:      GatewayReadDeadline,
			largerName:  "GatewayReadDeadline",
			reason:      "ping should timeout faster for quick failure detection",
		},
		{
			name:        "Circuit breaker timeout vs gateway timeout",
			smaller:     DefaultCircuitBreakerTimeout,
			smallerName: "DefaultCircuitBreakerTimeout",
			larger:      DefaultGatewayTimeout,
			largerName:  "DefaultGatewayTimeout",
			reason:      "circuit breaker should trip before gateway timeout",
		},
	}
}

func createConnectionTimeoutTests() []struct {
	name        string
	smaller     time.Duration
	smallerName string
	larger      time.Duration
	largerName  string
	reason      string
} {
	return []struct {
		name        string
		smaller     time.Duration
		smallerName string
		larger      time.Duration
		largerName  string
		reason      string
	}{
		{
			name:        "Acquire timeout vs max idle time",
			smaller:     DefaultAcquireTimeout,
			smallerName: "DefaultAcquireTimeout",
			larger:      DefaultMaxIdleTime,
			largerName:  "DefaultMaxIdleTime",
			reason:      "connection acquisition should be faster than idle timeout",
		},
		{
			name:        "State check intervals",
			smaller:     ShortStateCheckInterval,
			smallerName: "ShortStateCheckInterval",
			larger:      ConnectionStateCheckInterval,
			largerName:  "ConnectionStateCheckInterval",
			reason:      "short intervals should be shorter than regular intervals",
		},
	}
}

func createBackoffTimeoutTests() []struct {
	name        string
	smaller     time.Duration
	smallerName string
	larger      time.Duration
	largerName  string
	reason      string
} {
	return []struct {
		name        string
		smaller     time.Duration
		smallerName string
		larger      time.Duration
		largerName  string
		reason      string
	}{
		{
			name:        "Rate limiter backoff range",
			smaller:     RateLimiterBackoffMin,
			smallerName: "RateLimiterBackoffMin",
			larger:      RateLimiterBackoffMax,
			largerName:  "RateLimiterBackoffMax",
			reason:      "minimum backoff should be less than maximum",
		},
		{
			name:        "Connection retry backoff range",
			smaller:     ConnectionRetryBackoffMin,
			smallerName: "ConnectionRetryBackoffMin",
			larger:      ConnectionRetryBackoffMax,
			largerName:  "ConnectionRetryBackoffMax",
			reason:      "minimum retry backoff should be less than maximum",
		},
	}
}

func runTimeoutConsistencyTests(t *testing.T, consistencyTests []struct {
	name        string
	smaller     time.Duration
	smallerName string
	larger      time.Duration
	largerName  string
	reason      string
}) {
	t.Helper()

	for _, tt := range consistencyTests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.smaller >= tt.larger {
				t.Errorf("%s (%v) should be less than %s (%v): %s",
					tt.smallerName, tt.smaller, tt.largerName, tt.larger, tt.reason)
			}
		})
	}
}
