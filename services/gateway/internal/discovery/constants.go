package discovery

import "time"

// Common constants for service discovery.
const (
	// Default values.
	DefaultWeight              = 100
	DefaultHealthCheckTimeout  = 5 * time.Second
	DefaultHealthCheckInterval = 30 * time.Second

	// Timeout values.
	ShortTimeout  = 5 * time.Second
	MediumTimeout = 10 * time.Second
	LongTimeout   = 30 * time.Second

	// Retry and delay values.
	DefaultMaxRetries  = 5
	RetryDelayMillis   = 100
	TestDelayMillis    = 50
	BackoffBaseSeconds = 5 // Base for exponential backoff

	// Consul-specific constants.
	ConsulWatchTimeoutMinutes    = 10
	ConsulHealthCheckSeconds     = 30
	ConsulWatchErrorDelaySeconds = 5

	// Kubernetes-specific constants.
	KubernetesResyncMinutes = 5
	KubernetesMaxRetries    = 3

	// HTTP port constants.
	DefaultHTTPPort  = 80
	DefaultHTTPSPort = 443
)
