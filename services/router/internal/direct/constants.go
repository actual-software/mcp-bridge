package direct

import "time"

// Common constants used throughout the direct package.
const (
	defaultBufferSize     = 1024
	defaultMaxConnections = 5
	defaultMaxRetries     = 100
	defaultRetryCount     = 10
	defaultTimeoutSeconds = 30
	httpStatusOK          = 200

	// Mathematical and algorithmic constants.
	DivisionFactorTwo      = 2
	FloatDivisionFactorTwo = 2.0
	MinSampleSize          = 10
	HashBucketSizeMedium   = 32
	HashBucketSizeLarge    = 64

	// Buffer sizes in kilobytes.
	KilobyteFactor    = 1024
	StdioBufferSizeKB = 64
	HTTPBufferSizeKB  = 32

	// Time durations in seconds.
	DefaultDirectTimeoutSeconds = 10
	DefaultRetryDelaySeconds    = 5
	LongTimeoutSeconds          = 300

	// Percentages and counts.
	PercentageBase                = 100
	DefaultSampleWindowSize       = 10
	DefaultMaxRestarts            = 3
	DefaultTraceLimit             = 100
	DefaultMetricsHistoryHours    = 24
	DefaultProfileSampleSize      = 50
	DefaultHighThresholdPercent   = 80
	DefaultMediumThresholdPercent = 50
	DefaultLowThresholdPercent    = 30

	// Buffer and page sizes.
	DefaultPageSize = 4096

	// Score reduction constants.
	ScoreReductionHigh = 20

	// Time delays in milliseconds.
	DefaultBaseDelayMillis = 100

	// Connection settings.
	DefaultMaxIdleConnsPerHost = 2

	// Error thresholds.
	ConsecutiveFailuresCriticalThreshold = 5
	ErrorRateWarningThreshold            = 0.1
	ErrorRateCriticalThreshold           = 0.3

	// Learning and adaptation constants.
	DefaultLearningPeriod          = 5 * time.Minute
	SafetyLearningFactor           = 2.5
	DefaultHandshakeTimeoutSeconds = 3

	// Retry thresholds as decimals.
	LowSuccessRateThreshold  = 0.5
	HighSuccessRateThreshold = 0.7
	LowBackoffMultiplier     = 0.8
	HighBackoffMultiplier    = 1.5
)
