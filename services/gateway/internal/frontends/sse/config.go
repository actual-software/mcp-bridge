package sse

import (
	"time"

	"github.com/actual-software/mcp-bridge/services/gateway/internal/config"
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
		c.Host = "0.0.0.0"
	}
	if c.Port == 0 {
		c.Port = 8081
	}
	if c.StreamEndpoint == "" {
		c.StreamEndpoint = "/events"
	}
	if c.RequestEndpoint == "" {
		c.RequestEndpoint = "/api/v1/request"
	}
	if c.KeepAlive == 0 {
		c.KeepAlive = 30 * time.Second
	}
	if c.MaxConnections == 0 {
		c.MaxConnections = 10000
	}
	if c.BufferSize == 0 {
		c.BufferSize = 256
	}
	if c.ReadTimeout == 0 {
		c.ReadTimeout = 30 * time.Second
	}
	if c.WriteTimeout == 0 {
		c.WriteTimeout = 30 * time.Second
	}
}
