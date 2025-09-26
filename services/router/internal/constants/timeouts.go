// Package constants defines shared timeout and duration constants used throughout the router service.
//
// This package centralizes magic numbers and provides clear documentation for why.
// specific timeout values were chosen. These values are based on practical experience
// with network operations, user experience requirements, and system stability needs.
package constants

import "time"

// These values balance client wait time with server resource protection.
const (
	// Value chosen to allow reasonable metrics collection time.
	MetricsServerReadTimeout = defaultRetryCount * time.Second

	// Should be sufficient for metrics payload transmission.
	MetricsServerWriteTimeout = defaultRetryCount * time.Second

	// Balances graceful shutdown with reasonable termination time.
	MetricsShutdownTimeout = defaultMaxConnections * time.Second
)

const (
	defaultTimeoutSeconds    = 30
	defaultMaxTimeoutSeconds = 60
	defaultRetryCount        = 10
	defaultMaxConnections    = 5
	defaultMaxRetries        = 100
)

// These values are critical for network operation reliability.
const (
	// Based on typical network round-trip times plus processing.
	DefaultGatewayTimeout = defaultTimeoutSeconds * time.Second

	// Must be long enough for typical message sizes.
	GatewayWriteDeadline = defaultRetryCount * time.Second

	// Balances responsiveness with network latency tolerance.
	GatewayReadDeadline = defaultTimeoutSeconds * time.Second

	// Shorter than normal operations for faster failure detection.
	GatewayPingDeadline = defaultMaxConnections * time.Second
)

// These values optimize connection reuse and resource management.
const (
	// Longer than typical request intervals to enable reuse.
	DefaultMaxIdleTime = 5 * time.Minute

	// Forces periodic connection refresh for stability.
	DefaultMaxLifetime = 30 * time.Minute

	// Allows for connection creation but prevents UI blocking.
	DefaultAcquireTimeout = defaultMaxConnections * time.Second

	// Balances reliability with performance impact.
	DefaultHealthCheckInterval = defaultTimeoutSeconds * time.Second

	// Reasonable time for cleanup without delaying shutdown.
	PoolCleanupTimeout = defaultTimeoutSeconds * time.Second

	// 10 seconds provides good granularity for observability.
	PoolStatisticsInterval = defaultRetryCount * time.Second
)

// These values implement fault tolerance patterns.
const (
	// Based on typical service restart and warmup times.
	DefaultCircuitBreakerRecoveryTimeout = defaultTimeoutSeconds * time.Second

	// Shorter than client timeouts to enable proper error handling.
	DefaultCircuitBreakerTimeout = defaultRetryCount * time.Second

	// One minute provides good statistical significance.
	DefaultCircuitBreakerMonitoringWindow = defaultMaxTimeoutSeconds * time.Second
)

// These values implement traffic control and backpressure.
const (
	// 30 seconds provides reasonable refill granularity.
	DefaultTokenBucketRefillInterval = defaultTimeoutSeconds * time.Second

	// 10ms is barely perceptible to users.
	RateLimiterBackoffMin = 10 * time.Millisecond

	// 1 second is reasonable maximum wait for rate limiting.
	RateLimiterBackoffMax = 1 * time.Second
)

// These values handle connection lifecycle and reliability.
const (
	// 1 second allows brief network interruptions to resolve.
	ConnectionRetryBackoffMin = 1 * time.Second

	// 60 seconds is reasonable maximum backoff for connection issues.
	ConnectionRetryBackoffMax = defaultMaxTimeoutSeconds * time.Second

	// 30 seconds is standard for keepalive protocols.
	ConnectionKeepaliveInterval = defaultTimeoutSeconds * time.Second

	// 30 seconds handles slow DNS resolution and connection setup.
	ConnectionEstablishTimeout = defaultTimeoutSeconds * time.Second
)

// These values control core router behavior and user experience.
const (
	// 5 seconds provides responsive feel while allowing processing time.
	DefaultReceiveTimeout = defaultMaxConnections * time.Second

	// 100ms provides sub-second responsiveness for state monitoring.
	ConnectionStateCheckInterval = 100 * time.Millisecond

	// 10ms enables tight control loops without excessive overhead.
	ShortStateCheckInterval = 10 * time.Millisecond

	// 5 seconds balances graceful shutdown with reasonable termination.
	ShutdownTimeout = defaultMaxConnections * time.Second
)

// These values handle authentication token lifecycle.
const (
	// 30 seconds provides safety margin for clock skew and processing.
	OAuth2TokenRefreshBuffer = defaultTimeoutSeconds * time.Second
)
