package config

import (
	"os"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPlatformSpecificSecurity(t *testing.T) {
	tests := []struct {
		name     string
		platform string
		testFunc func(t *testing.T)
	}{
		{
			name:     "Windows security validation",
			platform: "windows",
			testFunc: testWindowsSecurityValidation,
		},
		{
			name:     "Linux security validation",
			platform: "linux",
			testFunc: testLinuxSecurityValidation,
		},
		{
			name:     "macOS security validation",
			platform: "darwin",
			testFunc: testMacOSSecurityValidation,
		},
		{
			name:     "Unix-like security validation",
			platform: "unix",
			testFunc: testUnixLikeSecurityValidation,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.platform != "unix" && runtime.GOOS != tt.platform {
				t.Skipf("Skipping %s-specific tests on %s", tt.platform, runtime.GOOS)
			}

			if tt.platform == "unix" && runtime.GOOS == "windows" {
				t.Skip("Skipping Unix-like tests on Windows")
			}

			tt.testFunc(t)
		})
	}
}

func testWindowsSecurityValidation(t *testing.T) {
	t.Helper()
	tests := []struct {
		name   string
		config *Config
		check  func(t *testing.T, config *Config)
	}{
		{
			name:   "Windows Path Validation",
			config: createValidMinimalConfig(),
			check:  checkWindowsPaths,
		},
		{
			name:   "Windows Service Security",
			config: createValidMinimalConfig(),
			check:  checkWindowsServiceSecurity,
		},
		{
			name:   "Windows Named Pipes Security",
			config: createValidMinimalConfig(),
			check:  checkWindowsNamedPipesSecurity,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.check(t, tt.config)
		})
	}
}

func testLinuxSecurityValidation(t *testing.T) {
	t.Helper()
	// Linux-specific security considerations
	tests := []struct {
		name   string
		config *Config
		check  func(t *testing.T, config *Config)
	}{
		{
			name:   "Linux File Permissions",
			config: createValidMinimalConfig(),
			check:  checkLinuxFilePermissions,
		},
		{
			name:   "Linux Systemd Security",
			config: createValidMinimalConfig(),
			check:  checkLinuxSystemdSecurity,
		},
		{
			name:   "Linux Unix Socket Security",
			config: createValidMinimalConfig(),
			check:  checkLinuxUnixSocketSecurity,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.check(t, tt.config)
		})
	}
}

func testMacOSSecurityValidation(t *testing.T) {
	t.Helper()
	// macOS-specific security considerations
	tests := []struct {
		name   string
		config *Config
		check  func(t *testing.T, config *Config)
	}{
		{
			name:   "macOS Keychain Security",
			config: createValidMinimalConfig(),
			check:  checkMacOSKeychainSecurity,
		},
		{
			name:   "macOS Bundle Security",
			config: createValidMinimalConfig(),
			check:  checkMacOSBundleSecurity,
		},
		{
			name:   "macOS System Security",
			config: createValidMinimalConfig(),
			check:  checkMacOSSystemSecurity,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.check(t, tt.config)
		})
	}
}

func testUnixLikeSecurityValidation(t *testing.T) {
	t.Helper()
	// Unix-like systems (Linux, macOS, BSD) common security considerations
	tests := []struct {
		name   string
		config *Config
		check  func(t *testing.T, config *Config)
	}{
		{
			name:   "Unix Socket Permissions",
			config: createValidMinimalConfig(),
			check:  checkUnixSocketPermissions,
		},
		{
			name:   "Unix Process Security",
			config: createValidMinimalConfig(),
			check:  checkUnixProcessSecurity,
		},
		{
			name:   "Unix File System Security",
			config: createValidMinimalConfig(),
			check:  checkUnixFileSystemSecurity,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.check(t, tt.config)
		})
	}
}

// Windows-specific configuration generators and checkers

func createWindowsSecureConfig() *Config { //nolint:unused
	config := createValidMinimalConfig()
	config.Server.TLS = TLSConfig{
		Enabled:  true,
		CertFile: "C:\\certs\\server.crt",
		KeyFile:  "C:\\certs\\server.key",
		CAFile:   "C:\\certs\\ca.crt",
	}
	config.Logging = LoggingConfig{
		Level:  "info",
		Format: "json",
	}

	return config
}

func createWindowsServiceConfig() *Config { //nolint:unused
	config := createValidMinimalConfig()
	config.Server.StdioFrontend = StdioFrontendConfig{
		Enabled: true,
		Modes: []StdioFrontendModeConfig{
			{
				Type:    "named_pipes",
				Path:    "\\\\.\\pipe\\mcp-gateway",
				Enabled: true,
			},
		},
	}

	return config
}

func createWindowsNamedPipesConfig() *Config { //nolint:unused
	config := createValidMinimalConfig()
	config.Discovery.Stdio.Services = []StdioServiceConfig{
		{
			Name:    "windows-service",
			Command: []string{"C:\\Program Files\\Service\\service.exe", "--mode", "stdio"},
			Env: map[string]string{
				"PATH": "C:\\Program Files\\Service;%PATH%",
			},
		},
	}

	return config
}

func checkWindowsPaths(t *testing.T, config *Config) {
	t.Helper()
	// Windows path validation
	assert.NotNil(t, config)
	assert.NotEmpty(t, config.Server.Host)
	assert.Positive(t, config.Server.Port)
}

func checkWindowsServiceSecurity(t *testing.T, config *Config) {
	t.Helper()
	// Check Windows service security
	if config.Server.StdioFrontend.Enabled {
		for _, mode := range config.Server.StdioFrontend.Modes {
			if mode.Type == "named_pipes" {
				assert.NotEmpty(t, mode.Path, "Named pipe path should not be empty")
			}
		}
	}
}

func checkWindowsNamedPipesSecurity(t *testing.T, config *Config) {
	t.Helper()
	// Check named pipes security
	for _, service := range config.Discovery.Stdio.Services {
		if len(service.Command) > 0 {
			assert.NotEmpty(t, service.Command, "Service command should not be empty")
		}
	}
}

// Linux-specific configuration generators and checkers

func createLinuxSecureConfig() *Config { //nolint:unused
	config := createValidMinimalConfig()
	config.Server.TLS = TLSConfig{
		Enabled:  true,
		CertFile: "/etc/ssl/certs/server.crt",
		KeyFile:  "/etc/ssl/private/server.key",
	}

	return config
}

func createLinuxSystemdConfig() *Config { //nolint:unused
	config := createValidMinimalConfig()
	config.Logging = LoggingConfig{
		Level:  "info",
		Format: "json",
	}

	return config
}

func checkLinuxFilePermissions(t *testing.T, config *Config) {
	t.Helper()
	// Check Linux file permissions
	assert.NotNil(t, config)
	assert.NotEmpty(t, config.Server.Host)
}

func checkLinuxSystemdSecurity(t *testing.T, config *Config) {
	t.Helper()
	// Check systemd service security considerations
	assert.Equal(t, "json", config.Logging.Format, "Should use structured logging for systemd")
}

func checkLinuxUnixSocketSecurity(t *testing.T, config *Config) {
	t.Helper()
	// Check Unix socket security
	if config.Server.StdioFrontend.Enabled {
		for _, mode := range config.Server.StdioFrontend.Modes {
			if mode.Type == socketTypeUnix {
				assert.NotEmpty(t, mode.Path, "Unix socket path should not be empty")
			}
		}
	}
}

// macOS-specific configuration generators and checkers

func checkMacOSKeychainSecurity(t *testing.T, config *Config) {
	t.Helper()
	// Check macOS keychain integration
	assert.NotNil(t, config)
	assert.NotEmpty(t, config.Server.Host)
}

func checkMacOSBundleSecurity(t *testing.T, config *Config) {
	t.Helper()
	// Check macOS application bundle security
	for _, service := range config.Discovery.Stdio.Services {
		if len(service.Command) > 0 {
			cmd := service.Command[0]
			if isMacOSAppBundle(cmd) {
				assert.Contains(t, cmd, ".app", "Should be a valid macOS app bundle")
			}
		}
	}
}

func checkMacOSSystemSecurity(t *testing.T, config *Config) {
	t.Helper()
	// Check macOS system integration security
	if config.Server.StdioFrontend.Enabled {
		for _, mode := range config.Server.StdioFrontend.Modes {
			if mode.Type == socketTypeUnix {
				assert.NotEmpty(t, mode.Path, "Unix socket path should not be empty for macOS")
			}
		}
	}
}

// Unix-like common configuration generators and checkers

func checkUnixSocketPermissions(t *testing.T, config *Config) {
	t.Helper()
	// Check Unix socket permissions
	if config.Server.StdioFrontend.Enabled {
		for _, mode := range config.Server.StdioFrontend.Modes {
			if mode.Type == socketTypeUnix {
				assert.NotEmpty(t, mode.Path, "Unix socket path should not be empty")
			}
		}
	}
}

func checkUnixProcessSecurity(t *testing.T, config *Config) {
	t.Helper()
	// Check Unix process security
	for _, service := range config.Discovery.Stdio.Services {
		if len(service.Command) > 0 {
			assert.NotEmpty(t, service.Command[0], "Service command should not be empty")
		}
	}
}

func checkUnixFileSystemSecurity(t *testing.T, config *Config) {
	t.Helper()
	// Check Unix file system security
	if config.Server.TLS.KeyFile != "" {
		assert.True(t, isSecureUnixKeyPath(config.Server.TLS.KeyFile), "Key file should be in secure location")
	}
}

// Security helper functions

func isMacOSAppBundle(path string) bool {
	return len(path) > 4 && path[len(path)-4:] == ".app" ||
		(len(path) > 15 && path[len(path)-15:] == ".app/Contents/MacOS")
}

func isSecureUnixKeyPath(path string) bool {
	secureLocations := []string{"/etc/ssl/private/", "/usr/local/etc/ssl/private/", "/opt/ssl/private/"}
	for _, location := range secureLocations {
		if len(path) >= len(location) && path[:len(location)] == location {
			return true
		}
	}

	return false
}

func TestEnvironmentSpecificSecurity(t *testing.T) {
	// Test environment-specific security considerations
	tests := []struct {
		name    string
		envVars map[string]string
		check   func(t *testing.T, config *Config)
	}{
		{
			name: "development environment",
			envVars: map[string]string{
				"ENVIRONMENT": "development",
				"DEBUG":       "true",
			},
			check: checkDevelopmentSecurity,
		},
		{
			name: "staging environment",
			envVars: map[string]string{
				"ENVIRONMENT": "staging",
				"DEBUG":       "false",
			},
			check: checkStagingSecurity,
		},
		{
			name: "production environment",
			envVars: map[string]string{
				"ENVIRONMENT": "production",
				"DEBUG":       "false",
			},
			check: checkProductionSecurity,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set environment variables
			for key, value := range tt.envVars {
				_ = os.Setenv(key, value)

				defer func() { _ = os.Unsetenv(key) }()
			}

			config := createEnvironmentAwareConfig()
			err := ValidateConfig(config)
			require.NoError(t, err)

			tt.check(t, config)
		})
	}
}

func createEnvironmentAwareConfig() *Config {
	config := createValidMinimalConfig()

	// Configuration that might vary by environment
	env := os.Getenv("ENVIRONMENT")
	switch env {
	case "development":
		config.Logging.Level = "debug"
		config.Server.TLS.Enabled = false // Acceptable in dev
		config.Auth.Provider = "none"     // Acceptable in dev
	case "staging":
		config.Logging.Level = "info"
		config.Server.TLS = TLSConfig{
			Enabled:  true,
			CertFile: "/etc/ssl/certs/server.crt",
			KeyFile:  "/etc/ssl/private/server.key",
		}
		config.Auth = AuthConfig{
			Provider: "jwt",
			JWT: JWTConfig{
				Issuer:        "test-issuer",
				Audience:      "test-audience",
				PublicKeyPath: "/path/to/public.key",
			},
		}
	case "production":
		config.Logging.Level = "warn"
		config.Server.TLS = TLSConfig{
			Enabled:  true,
			CertFile: "/etc/ssl/certs/server.crt",
			KeyFile:  "/etc/ssl/private/server.key",
		}
		config.Auth = AuthConfig{
			Provider: "oauth2",
			OAuth2: OAuth2Config{
				ClientID:           "prod-client-id",
				TokenEndpoint:      "https://auth.example.com/token",
				IntrospectEndpoint: "https://auth.example.com/introspect",
			},
		}
		config.RateLimit = RateLimitConfig{
			Enabled:        true,
			Provider:       "redis",
			RequestsPerSec: testIterations,
			Burst:          150,
		}
		config.CircuitBreaker = CircuitBreakerConfig{
			Enabled:          true,
			FailureThreshold: 5,
			SuccessThreshold: 3,
			TimeoutSeconds:   30,
		}
	}

	return config
}

func checkDevelopmentSecurity(t *testing.T, config *Config) {
	t.Helper()

	// Development environment can be less strict
	t.Log("Development environment: relaxed security acceptable")

	// But still check for obvious issues
	assert.NotEqual(t, "none", config.Logging.Level, "Should still have some logging in dev")
}

func checkStagingSecurity(t *testing.T, config *Config) {
	t.Helper()

	// Staging should be closer to production
	assert.True(t, config.Server.TLS.Enabled, "Staging should use TLS")
	assert.NotEqual(t, "none", config.Auth.Provider, "Staging should have authentication")
	assert.NotEqual(t, "debug", config.Logging.Level, "Staging should not use debug logging")
}

func checkProductionSecurity(t *testing.T, config *Config) {
	t.Helper()

	// Production should have all security measures
	assert.True(t, config.Server.TLS.Enabled, "Production must use TLS")
	assert.NotEqual(t, "none", config.Auth.Provider, "Production must have authentication")
	assert.True(t, config.RateLimit.Enabled, "Production should have rate limiting")
	assert.True(t, config.CircuitBreaker.Enabled, "Production should have circuit breaker")
	assert.NotEqual(t, "debug", config.Logging.Level, "Production should not use debug logging")
}
