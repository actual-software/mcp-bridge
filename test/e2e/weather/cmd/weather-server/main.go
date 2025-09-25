
// Package main provides the entry point for the Weather MCP server
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
	weather "weather-test"
)

const (
	// Default configuration values.
	defaultPort        = 8080
	defaultMetricsPort = 9090
)

var (
	// Version information (set at build time).
	Version   = "dev"
	BuildTime = "unknown"
)

func main() {
	// Define command-line flags
	var (
		configFile  = flag.String("config", "", "Path to configuration file (JSON or YAML)")
		port        = flag.Int("port", defaultPort, "Server port")
		host        = flag.String("host", "0.0.0.0", "Server host")
		metricsPort = flag.Int("metrics-port", defaultMetricsPort, "Metrics server port")
		logLevel    = flag.String("log-level", "info", "Log level (debug, info, warn, error)")
		logFormat   = flag.String("log-format", "json", "Log format (json, console)")
		tlsCert     = flag.String("tls-cert", "", "Path to TLS certificate file")
		tlsKey      = flag.String("tls-key", "", "Path to TLS key file")
		version     = flag.Bool("version", false, "Show version information")
		healthCheck = flag.Bool("health", false, "Run health check and exit")
	)

	flag.Parse()

	// Handle version flag
	if *version {
		fmt.Printf("Weather MCP Server\nVersion: %s\nBuild Time: %s\n", Version, BuildTime)
		os.Exit(0)
	}

	// Handle health check flag (for Docker health check)
	if *healthCheck {
		// Simple health check - just verify the process can start
		fmt.Println("OK")
		os.Exit(0)
	}

	// Load configuration
	config := loadConfig(*configFile, &weather.ServerConfig{
		Port:        *port,
		Host:        *host,
		MetricsPort: *metricsPort,
		LogLevel:    *logLevel,
		LogFormat:   *logFormat,
		TLSCertFile: *tlsCert,
		TLSKeyFile:  *tlsKey,
		TLSEnabled:  *tlsCert != "" && *tlsKey != "",
	})

	// Create and start server
	server, err := weather.NewServer(config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create server: %v\n", err)
		os.Exit(1)
	}

	// Run server with graceful shutdown
	if err := server.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
		os.Exit(1)
	}
}

// loadConfig loads configuration from file or returns defaults.
func loadConfig(configFile string, defaults *weather.ServerConfig) *weather.ServerConfig {
	if configFile == "" {
		// Use defaults merged with command-line flags
		if defaults.Port == 0 {
			return weather.DefaultServerConfig()
		}

		return mergeWithDefaults(defaults)
	}

	// Validate and read config file safely
	data, err := readConfigFileSafely(configFile) 
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to read config file: %v\n", err)
		os.Exit(1)
	}

	// Parse config based on extension
	var config weather.ServerConfig
	if isYAML(configFile) {
		if err := yaml.Unmarshal(data, &config); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to parse YAML config: %v\n", err)
			os.Exit(1)
		}
	} else {
		if err := json.Unmarshal(data, &config); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to parse JSON config: %v\n", err)
			os.Exit(1)
		}
	}

	// Merge with command-line flags (flags take precedence)
	return mergeConfigs(&config, defaults)
}

// mergeWithDefaults merges provided config with defaults.
func mergeWithDefaults(config *weather.ServerConfig) *weather.ServerConfig {
	defaults := weather.DefaultServerConfig()

	// Override defaults with provided values
	if config.Port != 0 {
		defaults.Port = config.Port
	}

	if config.Host != "" {
		defaults.Host = config.Host
	}

	if config.MetricsPort != 0 {
		defaults.MetricsPort = config.MetricsPort
	}

	if config.LogLevel != "" {
		defaults.LogLevel = config.LogLevel
	}

	if config.LogFormat != "" {
		defaults.LogFormat = config.LogFormat
	}

	if config.TLSCertFile != "" {
		defaults.TLSCertFile = config.TLSCertFile
		defaults.TLSEnabled = true
	}

	if config.TLSKeyFile != "" {
		defaults.TLSKeyFile = config.TLSKeyFile
		defaults.TLSEnabled = true
	}

	return defaults
}

// mergeConfigs merges file config with command-line config (command-line takes precedence).
func mergeConfigs(fileConfig, cmdConfig *weather.ServerConfig) *weather.ServerConfig {
	// Start with file config
	result := *fileConfig

	// Override with command-line values if provided
	if cmdConfig.Port != defaultPort { // Check if not default
		result.Port = cmdConfig.Port
	}

	if cmdConfig.Host != "0.0.0.0" {
		result.Host = cmdConfig.Host
	}

	if cmdConfig.MetricsPort != defaultMetricsPort {
		result.MetricsPort = cmdConfig.MetricsPort
	}

	if cmdConfig.LogLevel != "info" {
		result.LogLevel = cmdConfig.LogLevel
	}

	if cmdConfig.LogFormat != "json" {
		result.LogFormat = cmdConfig.LogFormat
	}

	if cmdConfig.TLSCertFile != "" {
		result.TLSCertFile = cmdConfig.TLSCertFile
		result.TLSEnabled = true
	}

	if cmdConfig.TLSKeyFile != "" {
		result.TLSKeyFile = cmdConfig.TLSKeyFile
		result.TLSEnabled = true
	}

	return &result
}

// readConfigFileSafely validates the file path and reads the config file safely.
func readConfigFileSafely(configFile string) ([]byte, error) {
	// Clean the path to prevent directory traversal
	cleanPath := filepath.Clean(configFile)
	
	// Ensure the file path doesn't contain dangerous patterns
	if strings.Contains(cleanPath, "..") {
		return nil, errors.New("invalid config file path: path traversal detected")
	}
	
	// Only allow files with specific extensions
	ext := strings.ToLower(filepath.Ext(cleanPath))
	if ext != ".yaml" && ext != ".yml" && ext != ".json" {
		return nil, errors.New("invalid config file extension: only .yaml, .yml, and .json are allowed")
	}
	
	return os.ReadFile(cleanPath)
}

// isYAML checks if a file is YAML based on extension.
func isYAML(filename string) bool {
	return len(filename) > 4 && (filename[len(filename)-4:] == ".yml" || filename[len(filename)-5:] == ".yaml")
}
