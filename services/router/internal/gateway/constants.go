package gateway

// Common constants used throughout the gateway package.
const (
	defaultBufferSize     = 1024
	defaultHTTPPort       = 8080
	defaultHTTPSPort      = 8443
	defaultMaxConnections = 5
	defaultRetryCount     = 10
	defaultTimeoutSeconds = 30

	// Binary protocol constants.
	MaxPayloadSize = 10 * 1024 * 1024 // 10MB max payload
	MagicBytes     = 0x4D435000       // "MCP\x00" as uint32
	HeaderSize     = 16               // Size of binary frame header
)
