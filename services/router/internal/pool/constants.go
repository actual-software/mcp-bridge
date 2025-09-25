package pool

// Pool configuration constants.
const (
	// DefaultMinPoolSize is the default minimum number of connections in the pool.
	DefaultMinPoolSize = 2

	// DefaultMaxPoolSize is the default maximum number of connections in the pool.
	DefaultMaxPoolSize = 10

	// BackgroundWorkerCount is the number of background worker goroutines for pool maintenance.
	BackgroundWorkerCount = 2
)
