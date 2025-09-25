// Package testutil provides common utilities and constants for tests.
package testutil

import "time"

// Common test timing constants to avoid magic numbers in tests.
const (
	// Short delays for ensuring operations complete.
	ShortDelay = 10 * time.Millisecond

	// Medium delays for waiting on async operations.
	MediumDelay = 50 * time.Millisecond

	// Long delays for complex operations.
	LongDelay = 100 * time.Millisecond

	// Timeout values for test operations.
	ShortTimeout  = 1 * time.Second
	MediumTimeout = 5 * time.Second
	LongTimeout   = 10 * time.Second

	// Retry configuration for tests.
	DefaultMaxRetries = 3
	RetryDelay        = 10 * time.Millisecond

	// Buffer sizes for test channels.
	SmallBufferSize  = 1
	MediumBufferSize = 10
	LargeBufferSize  = 100

	// Common test iterations.
	SmallIterations  = 5
	MediumIterations = 10
	LargeIterations  = 100
)
