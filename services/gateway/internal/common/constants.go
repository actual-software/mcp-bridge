package common

// Buffer size constants for channels and slices.
const (
	// Channel buffer sizes.
	DefaultChannelBuffer = 1    // Default buffer size for channels
	SmallChannelBuffer   = 10   // Small buffer for low-throughput channels
	MediumChannelBuffer  = 100  // Medium buffer for moderate throughput
	LargeChannelBuffer   = 1000 // Large buffer for high-throughput channels

	// Slice pre-allocation sizes.
	SmallSliceCapacity  = 5   // Small slice pre-allocation
	MediumSliceCapacity = 10  // Medium slice pre-allocation
	LargeSliceCapacity  = 100 // Large slice pre-allocation

	// Binary protocol sizes.
	BinaryHeaderSize = 12 // Binary protocol header size in bytes
	TokenSize        = 32 // Authentication token size in bytes
)
