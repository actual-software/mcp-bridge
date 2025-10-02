package websocket

import (
	"time"

	"github.com/actual-software/mcp-bridge/services/gateway/internal/config"
)

// Config holds WebSocket-specific configuration.
type Config struct {
	Host           string           `mapstructure:"host"`
	Port           int              `mapstructure:"port"`
	TLS            config.TLSConfig `mapstructure:"tls"`
	MaxConnections int              `mapstructure:"max_connections"`
	ReadTimeout    time.Duration    `mapstructure:"read_timeout"`
	WriteTimeout   time.Duration    `mapstructure:"write_timeout"`
	PingInterval   time.Duration    `mapstructure:"ping_interval"`
	PongTimeout    time.Duration    `mapstructure:"pong_timeout"`
	MaxMessageSize int64            `mapstructure:"max_message_size"`
	AllowedOrigins []string         `mapstructure:"allowed_origins"`
}

// ApplyDefaults applies default values to the configuration.
func (c *Config) ApplyDefaults() {
	if c.Host == "" {
		c.Host = "0.0.0.0"
	}
	if c.Port == 0 {
		c.Port = 8443
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
	if c.PingInterval == 0 {
		c.PingInterval = 30 * time.Second
	}
	if c.PongTimeout == 0 {
		c.PongTimeout = 60 * time.Second
	}
	if c.MaxMessageSize == 0 {
		c.MaxMessageSize = 1024 * 1024 // 1MB
	}
}
