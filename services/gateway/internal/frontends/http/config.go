package http

import (
	"time"

	"github.com/actual-software/mcp-bridge/services/gateway/internal/config"
)

const (
	defaultHost           = "0.0.0.0"
	defaultPort           = 8080
	defaultRequestPath    = "/api/v1/mcp"
	defaultMaxRequestSize = 1024 * 1024 // 1MB
	defaultReadTimeout    = 30 * time.Second
	defaultWriteTimeout   = 30 * time.Second
	defaultIdleTimeout    = 120 * time.Second
	defaultMaxHeaderBytes = 1 << 20 // 1MB
	shutdownTimeout       = 30 * time.Second
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
		c.Host = defaultHost
	}
	if c.Port == 0 {
		c.Port = defaultPort
	}
	if c.RequestPath == "" {
		c.RequestPath = defaultRequestPath
	}
	if c.MaxRequestSize == 0 {
		c.MaxRequestSize = defaultMaxRequestSize
	}
	if c.ReadTimeout == 0 {
		c.ReadTimeout = defaultReadTimeout
	}
	if c.WriteTimeout == 0 {
		c.WriteTimeout = defaultWriteTimeout
	}
	if c.IdleTimeout == 0 {
		c.IdleTimeout = defaultIdleTimeout
	}
	if c.MaxHeaderBytes == 0 {
		c.MaxHeaderBytes = defaultMaxHeaderBytes
	}
}
