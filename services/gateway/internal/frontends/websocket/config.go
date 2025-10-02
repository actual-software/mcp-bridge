package websocket

import (
	"time"

	"github.com/actual-software/mcp-bridge/services/gateway/internal/config"
)

const (
	defaultHost            = "0.0.0.0"
	defaultPort            = 8443
	defaultMaxConnections  = 10000
	defaultReadTimeout     = 60 * time.Second
	defaultWriteTimeout    = 60 * time.Second
	defaultPingInterval    = 30 * time.Second
	defaultPongTimeout     = 60 * time.Second
	defaultMaxMessageSize  = 1024 * 1024 // 1MB
	defaultReadBufferSize  = 1024
	defaultWriteBufferSize = 1024
	defaultWriteChSize     = 256
	shutdownTimeout        = 30 * time.Second
	streamCloseTimeout     = 5 * time.Second
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
	if c.PingInterval == 0 {
		c.PingInterval = defaultPingInterval
	}
	if c.PongTimeout == 0 {
		c.PongTimeout = defaultPongTimeout
	}
	if c.MaxMessageSize == 0 {
		c.MaxMessageSize = defaultMaxMessageSize
	}
}
