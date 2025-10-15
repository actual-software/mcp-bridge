package common

// ContextKey is a type for context keys to avoid collisions.
type ContextKey string

// Context keys for request metadata.
const (
	// SessionContextKey is the context key for session information.
	SessionContextKey ContextKey = "session"
)
