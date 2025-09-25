// Package constants defines shared constants for the router service.
package constants

import "time"

// File permissions.
const (
	// DirPermissions is the standard directory permission (rwxr-x---).
	DirPermissions = 0o750
	// FilePermissions is the standard file permission (rw-------).
	FilePermissions = 0o600
	// PublicFilePermissions is for files that can be read by others (rw-r--r--).
	PublicFilePermissions = 0o600
)

// Buffer and size limits.
const (
	// DefaultBufferSize is the default buffer size for channels and slices.
	DefaultBufferSize = 100
	// SmallBufferSize is for small buffers.
	SmallBufferSize = 10
	// LargeBufferSize is for large buffers.
	LargeBufferSize = 1000
	// MinBufferSize is the minimum buffer size (4KB).
	MinBufferSize = 4096
	// MaxBufferSize is the maximum buffer size (1MB).
	MaxBufferSize = 1048576
	// DefaultReadBufferSize is the default read buffer size (64KB).
	DefaultReadBufferSize = 65536
	// DefaultWriteBufferSize is the default write buffer size (64KB).
	DefaultWriteBufferSize = 65536
	// DefaultMaxMessageSize is the default max message size (30KB).
	DefaultMaxMessageSize = 30000
)

// Timeouts and durations.
const (
	// ShortTimeout is for quick operations (1s).
	ShortTimeout = time.Second
	// GracefulShutdownTimeout is for graceful shutdown (2s).
	GracefulShutdownTimeout = 2 * time.Second
	// ProcessWaitTimeout is for process termination (3s).
	ProcessWaitTimeout = 3 * time.Second
	// ProcessQuickWaitTimeout is for quick process termination (1s).
	ProcessQuickWaitTimeout = time.Second
	// AutoDetectionTimeout is for protocol auto-detection (10s).
	AutoDetectionTimeout = 10 * time.Second
	// LongStreamTimeout is for long-running streams (5m).
	LongStreamTimeout = 300 * time.Second
	// CleanupTickerInterval is for periodic cleanup (5m).
	CleanupTickerInterval = 5 * time.Minute
	// HourlyTickerInterval is for hourly tasks (1h).
	HourlyTickerInterval = time.Hour
	// DailyRetention is for daily retention (24h).
	DailyRetention = 24 * time.Hour
	// DefaultTimeout is the default operation timeout.
	DefaultTimeout = 30 * time.Second
	// QuickOperationTimeout is for quick operations (5s).
	QuickOperationTimeout = 5 * time.Second
	// LongTimeout is for slow operations.
	LongTimeout = 60 * time.Second
	// DefaultKeepAlive is the default keep-alive duration.
	DefaultKeepAlive = 30 * time.Second
	// DefaultPingInterval is the default ping interval.
	DefaultPingInterval = 30 * time.Second
	// DefaultRetryDelay is the default retry delay.
	DefaultRetryDelay = 5 * time.Second
	// DefaultRetryMaxDelay is the maximum retry delay.
	DefaultRetryMaxDelay = 30 * time.Second
	// DefaultCleanupInterval is the default cleanup interval.
	DefaultCleanupInterval = 5 * time.Minute
	// DefaultBatchTimeout is the default batch timeout.
	DefaultBatchTimeout = 10 * time.Millisecond
	// MinimalBatchTimeout is the minimal batch timeout (1ms).
	MinimalBatchTimeout = time.Millisecond
	// DefaultHealthCheckTimeout is the default health check timeout.
	DefaultHealthCheckTimeout = 5 * time.Second
)

// Retry and threshold values.
const (
	// DefaultMaxRetries is the default maximum number of retries.
	DefaultMaxRetries = 3
	// DefaultRetryThreshold is the default retry threshold.
	DefaultRetryThreshold = 5
	// DefaultSuccessThreshold is the default success threshold.
	DefaultSuccessThreshold = 2
	// DefaultFailureThreshold is the default failure threshold.
	DefaultFailureThreshold = 5
	// DefaultErrorThreshold is the default error threshold.
	DefaultErrorThreshold = 10
	// DefaultMaxConcurrency is the default max concurrent operations.
	DefaultMaxConcurrency = 100
	// DefaultBatchSize is the default batch size.
	DefaultBatchSize = 100
	// DefaultMaxConnections is the default max connections.
	DefaultMaxConnections = 100
	// DefaultMaxConnectionsPerHost is the default max connections per host.
	DefaultMaxConnectionsPerHost = 10
	// DefaultConnectionPoolSize is the default connection pool size.
	DefaultConnectionPoolSize = 5
	// DefaultMaxIdleConnections is the default max idle connections.
	DefaultMaxIdleConnections = 50
	// DefaultMaxActiveConnections is the default max active connections.
	DefaultMaxActiveConnections = 200
	// DefaultIdleConnTimeout is the default idle connection timeout.
	DefaultIdleConnTimeout = 90 * time.Second
	// DefaultMaxRedirects is the default max redirects for HTTP.
	DefaultMaxRedirects = 3
	// DefaultSmallBufferSize is the default small buffer size (4KB).
	DefaultSmallBufferSize = 4096
	// DefaultMaxPooledObjects is the default max pooled objects.
	DefaultMaxPooledObjects = 50
	// DefaultMaxReconnectAttempts is the default max reconnect attempts.
	DefaultMaxReconnectAttempts = 3
	// DefaultFastReconnectDelay is for fast reconnection (1s).
	DefaultFastReconnectDelay = time.Second
)

// Circuit breaker and rate limiting.
const (
	// CircuitBreakerOpenDuration is how long circuit stays open.
	CircuitBreakerOpenDuration = 30 * time.Second
	// CircuitBreakerHalfOpenRequests is max requests in half-open state.
	CircuitBreakerHalfOpenRequests = 6
	// CircuitBreakerConsecutiveFailures is failures before opening.
	CircuitBreakerConsecutiveFailures = 5
	// CircuitBreakerConsecutiveSuccesses is successes to close from half-open.
	CircuitBreakerConsecutiveSuccesses = 2
	// DefaultRateLimit is the default rate limit per second.
	DefaultRateLimit = 1000
	// DefaultBurst is the default burst size.
	DefaultBurst = 100
)

// Pool and connection settings.
const (
	// DefaultPoolMinSize is the minimum pool size.
	DefaultPoolMinSize = 2
	// DefaultPoolMaxSize is the maximum pool size.
	DefaultPoolMaxSize = 10
	// DefaultPoolIdleSize is the idle pool size.
	DefaultPoolIdleSize = 5
	// DefaultMaxIdleConns is the max idle connections.
	DefaultMaxIdleConns = 100
	// DefaultMaxOpenConns is the max open connections.
	DefaultMaxOpenConns = 100
	// DefaultConnMaxLifetime is the max connection lifetime.
	DefaultConnMaxLifetime = time.Hour
	// DefaultConnMaxIdleTime is the max connection idle time.
	DefaultConnMaxIdleTime = 10 * time.Minute
)

// Ports and network.
const (
	// DefaultDebugPort is the default debug server port.
	DefaultDebugPort = 6060
	// DefaultMetricsPort is the default metrics server port.
	DefaultMetricsPort = 9090
	// DefaultHealthPort is the default health check port.
	DefaultHealthPort = 8080
)

// Percentages and ratios (as decimal multipliers).
const (
	// HighLoadThreshold is 80% utilization.
	HighLoadThreshold = 0.8
	// MediumLoadThreshold is 50% utilization.
	MediumLoadThreshold = 0.5
	// LowLoadThreshold is 20% utilization.
	LowLoadThreshold = 0.2
	// DefaultSampleRate is the default sampling rate.
	DefaultSampleRate = 0.1
)

// HTTP status codes (for consistency).
const (
	// StatusTooManyRequests is HTTP 429.
	StatusTooManyRequests = 429
	// StatusServiceUnavailable is HTTP 503.
	StatusServiceUnavailable = 503
	// StatusGatewayTimeout is HTTP 504.
	StatusGatewayTimeout = 504
)

// Miscellaneous.
const (
	// DefaultChannelBuffer is the default channel buffer size.
	DefaultChannelBuffer = 1
	// DefaultPriority is the default priority value.
	DefaultPriority = 100
	// MaxHeaderSize is the maximum header size.
	MaxHeaderSize = 4096
	// DefaultWeight is the default weight for load balancing.
	DefaultWeight = 100
	// DefaultQueueSize is the default queue size.
	DefaultQueueSize = 1000
	// DefaultCompressionLevel is the default gzip compression level.
	DefaultCompressionLevel = 6
	// DefaultDeduplicationCacheSize is the default deduplication cache size.
	DefaultDeduplicationCacheSize = 1000
	// DefaultDeduplicationTTLSeconds is the default deduplication TTL in seconds.
	DefaultDeduplicationTTLSeconds = 60
)
