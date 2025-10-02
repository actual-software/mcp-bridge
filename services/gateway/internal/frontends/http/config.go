package http

import (
	"time"

	"github.com/actual-software/mcp-bridge/services/gateway/internal/config"
)

// Config holds HTTP-specific configuration.
type Config struct {
	Host           string           `mapstructure:"host"`
	Port           int              `mapstructure:"port"`
	TLS            config.TLSConfig `mapstructure:"tls"`
	RequestPath    string           `mapstructure:"request_path"`
	MaxRequestSize int64            `mapstructure:"max_request_size"`
	ReadTimeout    time.Duration    `mapstructure:"read_timeout"`
	WriteTimeout   time.Duration    `mapstructure:"write_timeout"`
	IdleTimeout    time.Duration    `mapstructure:"idle_timeout"`
	MaxHeaderBytes int              `mapstructure:"max_header_bytes"`
}

// ApplyDefaults applies default values to the configuration.
func (c *Config) ApplyDefaults() {
	if c.Host == "" {
		c.Host = "0.0.0.0"
	}
	if c.Port == 0 {
		c.Port = 8080
	}
	if c.RequestPath == "" {
		c.RequestPath = "/api/v1/mcp"
	}
	if c.MaxRequestSize == 0 {
		c.MaxRequestSize = 1024 * 1024 // 1MB
	}
	if c.ReadTimeout == 0 {
		c.ReadTimeout = 30 * time.Second
	}
	if c.WriteTimeout == 0 {
		c.WriteTimeout = 30 * time.Second
	}
	if c.IdleTimeout == 0 {
		c.IdleTimeout = 120 * time.Second
	}
	if c.MaxHeaderBytes == 0 {
		c.MaxHeaderBytes = 1 << 20 // 1MB
	}
}
