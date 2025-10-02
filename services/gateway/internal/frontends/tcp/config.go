package tcp

import (
	"time"

	"github.com/actual-software/mcp-bridge/services/gateway/internal/config"
)

const (
	defaultHost           = "0.0.0.0"
	defaultPort           = 8444
	defaultMaxConnections = 10000
	defaultReadTimeout    = 60 * time.Second
	defaultWriteTimeout   = 60 * time.Second
	defaultMaxMessageSize = 10 * 1024 * 1024 // 10MB
	defaultHealthPort     = 9002
)

// Config holds TCP Binary-specific configuration.
type Config struct {
	Host           string           `mapstructure:"host"`
	Port           int              `mapstructure:"port"`
	TLS            config.TLSConfig `mapstructure:"tls"`
	MaxConnections int              `mapstructure:"max_connections"`
	ReadTimeout    time.Duration    `mapstructure:"read_timeout"`
	WriteTimeout   time.Duration    `mapstructure:"write_timeout"`
	MaxMessageSize int64            `mapstructure:"max_message_size"`
	HealthPort     int              `mapstructure:"health_port"`
}

// ApplyDefaults applies default values to the configuration.
func (c *Config) ApplyDefaults() {
	if c.Host == "" {
		c.Host = defaultHost
	}
	if c.Port == 0 {
		c.Port = defaultPort
	}
	if c.MaxConnections == 0 {
		c.MaxConnections = defaultMaxConnections
	}
	if c.ReadTimeout == 0 {
		c.ReadTimeout = defaultReadTimeout
	}
	if c.WriteTimeout == 0 {
		c.WriteTimeout = defaultWriteTimeout
	}
	if c.MaxMessageSize == 0 {
		c.MaxMessageSize = defaultMaxMessageSize
	}
	if c.HealthPort == 0 {
		c.HealthPort = defaultHealthPort
	}
}
