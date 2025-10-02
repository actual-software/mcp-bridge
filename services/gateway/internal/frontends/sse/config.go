package sse

import (
	"time"

	"github.com/actual-software/mcp-bridge/services/gateway/internal/config"
)

const (
	defaultHost            = "0.0.0.0"
	defaultPort            = 8081
	defaultStreamEndpoint  = "/events"
	defaultRequestEndpoint = "/api/v1/request"
	defaultKeepAlive       = 30 * time.Second
	defaultMaxConnections  = 10000
	defaultBufferSize      = 256
	defaultReadTimeout     = 30 * time.Second
	defaultWriteTimeout    = 30 * time.Second
	streamCloseTimeout     = 5 * time.Second
	shutdownTimeout        = 30 * time.Second
)

// Config holds SSE-specific configuration.
type Config struct {
	Host            string           `mapstructure:"host"`
	Port            int              `mapstructure:"port"`
	TLS             config.TLSConfig `mapstructure:"tls"`
	StreamEndpoint  string           `mapstructure:"stream_endpoint"`
	RequestEndpoint string           `mapstructure:"request_endpoint"`
	KeepAlive       time.Duration    `mapstructure:"keep_alive"`
	MaxConnections  int              `mapstructure:"max_connections"`
	BufferSize      int              `mapstructure:"buffer_size"`
	ReadTimeout     time.Duration    `mapstructure:"read_timeout"`
	WriteTimeout    time.Duration    `mapstructure:"write_timeout"`
}

// ApplyDefaults applies default values to the configuration.
func (c *Config) ApplyDefaults() {
	if c.Host == "" {
		c.Host = defaultHost
	}
	if c.Port == 0 {
		c.Port = defaultPort
	}
	if c.StreamEndpoint == "" {
		c.StreamEndpoint = defaultStreamEndpoint
	}
	if c.RequestEndpoint == "" {
		c.RequestEndpoint = defaultRequestEndpoint
	}
	if c.KeepAlive == 0 {
		c.KeepAlive = defaultKeepAlive
	}
	if c.MaxConnections == 0 {
		c.MaxConnections = defaultMaxConnections
	}
	if c.BufferSize == 0 {
		c.BufferSize = defaultBufferSize
	}
	if c.ReadTimeout == 0 {
		c.ReadTimeout = defaultReadTimeout
	}
	if c.WriteTimeout == 0 {
		c.WriteTimeout = defaultWriteTimeout
	}
}
