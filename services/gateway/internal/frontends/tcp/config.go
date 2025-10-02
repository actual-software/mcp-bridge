package tcp

import (
	"time"

	"github.com/actual-software/mcp-bridge/services/gateway/internal/config"
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
		c.Host = "0.0.0.0"
	}
	if c.Port == 0 {
		c.Port = 8444
	}
	if c.MaxConnections == 0 {
		c.MaxConnections = 10000
	}
	if c.ReadTimeout == 0 {
		c.ReadTimeout = 60 * time.Second
	}
	if c.WriteTimeout == 0 {
		c.WriteTimeout = 60 * time.Second
	}
	if c.MaxMessageSize == 0 {
		c.MaxMessageSize = 10 * 1024 * 1024 // 10MB
	}
	if c.HealthPort == 0 {
		c.HealthPort = 9002
	}
}
