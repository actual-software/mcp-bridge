package router

// Common constants used throughout the router package.
const (
	defaultTimeoutSeconds    = 30
	defaultMaxTimeoutSeconds = 60
	defaultMaxConnections    = 5
	defaultRetryCount        = 10
	defaultBufferSize        = 1024

	// StateChangeChannelBuffer is the buffer size for connection state change channels.
	StateChangeChannelBuffer = 10

	// DefaultRetryCountLimited is a smaller retry count for specific operations.
	DefaultRetryCountLimited = 3
)
