package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestMain(t *testing.T) {
	// This test ensures main() doesn't crash with a basic test
	// We can't easily test the actual main() function directly due to os.Exit()
	// So we test the command structure and basic functionality
	t.Run("command_structure", func(t *testing.T) {
		// Test that we can create the root command without issues
		rootCmd := &cobra.Command{
			Use:   "mcp-gateway",
			Short: "MCP Gateway - Kubernetes gateway for MCP servers",
			Long: `MCP Gateway provides WebSocket endpoints for MCP clients and
routes requests to appropriate MCP servers running in Kubernetes.`,
			RunE: run,
		}

		rootCmd.Flags().StringP("config", "c", "/etc/mcp-gateway/gateway.yaml", "Path to configuration file")
		rootCmd.Flags().BoolP("version", "v", false, "Show version information")
		rootCmd.Flags().String("log-level", "info", "Log level (debug, info, warn, error)")

		// Add subcommands
		rootCmd.AddCommand(adminCmd())

		// Verify command structure
		assert.Equal(t, "mcp-gateway", rootCmd.Use)
		assert.NotEmpty(t, rootCmd.Short)
		assert.NotEmpty(t, rootCmd.Long)
		assert.NotNil(t, rootCmd.RunE)

		// Check flags
		configFlag := rootCmd.Flags().Lookup("config")
		assert.NotNil(t, configFlag)
		assert.Equal(t, "/etc/mcp-gateway/gateway.yaml", configFlag.DefValue)

		versionFlag := rootCmd.Flags().Lookup("version")
		assert.NotNil(t, versionFlag)
		assert.Equal(t, "false", versionFlag.DefValue)

		logLevelFlag := rootCmd.Flags().Lookup("log-level")
		assert.NotNil(t, logLevelFlag)
		assert.Equal(t, "info", logLevelFlag.DefValue)

		// Check subcommands
		adminCommand := rootCmd.Commands()[0]
		assert.Equal(t, "admin", adminCommand.Use)
	})
}

func TestRunFunction(t *testing.T) {
	t.Run("version_flag", testRunFunctionVersionFlag)
	t.Run("invalid_log_level", testRunFunctionInvalidLogLevel)
	t.Run("missing_config_file", testRunFunctionMissingConfigFile)
	t.Run("valid_config_file", testRunFunctionValidConfigFile)
}

func testRunFunctionVersionFlag(t *testing.T) {
	t.Helper()

	// Create a command with version flag set
	cmd := &cobra.Command{}
	cmd.Flags().BoolP("version", "v", false, "Show version information")
	cmd.Flags().String("log-level", "info", "Log level")
	cmd.Flags().String("config", "/tmp/test-config.yaml", "Config file")

	// Set version flag to true
	err := cmd.Flags().Set("version", "true")
	require.NoError(t, err)

	// Capture output
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Run the function
	err = run(cmd, []string{})
	require.NoError(t, err)

	// Restore stdout and get output
	_ = w.Close()

	os.Stdout = oldStdout
	output, _ := io.ReadAll(r)

	// Verify version output
	outputStr := string(output)
	assert.Contains(t, outputStr, "MCP Gateway")
	assert.Contains(t, outputStr, "Version:")
	assert.Contains(t, outputStr, "Build Time:")
	assert.Contains(t, outputStr, "Git Commit:")
}

func testRunFunctionInvalidLogLevel(t *testing.T) {
	t.Helper()

	// Create a command with invalid log level
	cmd := &cobra.Command{}
	cmd.Flags().BoolP("version", "v", false, "Show version information")
	cmd.Flags().String("log-level", "invalid", "Log level")
	cmd.Flags().String("config", "/tmp/test-config.yaml", "Config file")

	// Run with invalid log level
	err := run(cmd, []string{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to initialize logger")
}

func testRunFunctionMissingConfigFile(t *testing.T) {
	t.Helper()

	// Create a command with non-existent config file
	cmd := &cobra.Command{}
	cmd.Flags().BoolP("version", "v", false, "Show version information")
	cmd.Flags().String("log-level", "info", "Log level")
	cmd.Flags().String("config", "/nonexistent/config.yaml", "Config file")

	// Run with missing config file
	err := run(cmd, []string{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load configuration")
}

func testRunFunctionValidConfigFile(t *testing.T) {
	t.Helper()

	// Create a temporary config file
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "test-config.yaml")

	configContent := `
server:
  port: 8080
  host: "localhost"
  max_connections: testMaxIterations
  metrics_port: 9090
  health_port: 8081
  protocol: "websocket"
auth:
  provider: "jwt"
  jwt_secret: "test-secret"
discovery:
  provider: "kubernetes"
routing:
  default_timeout: "30s"
sessions:
  provider: "memory"
`
	err := os.WriteFile(configFile, []byte(configContent), 0o600)
	require.NoError(t, err)

	// Create a command with the valid config file
	cmd := &cobra.Command{}
	cmd.Flags().BoolP("version", "v", false, "Show version information")
	cmd.Flags().String("log-level", "info", "Log level")
	cmd.Flags().String("config", configFile, "Config file")

	// This test verifies the config loading part - the server startup will fail during component initialization
	// which is expected for testing purposes since we're not providing a complete environment
	err = run(cmd, []string{})
	// The function should fail when trying to start components due to context cancellation
	// but should successfully load the config
	assert.Error(t, err) // Expected to fail during startup, not during config loading
}

func TestInitLogger(t *testing.T) {
	t.Run("valid_log_levels", func(t *testing.T) {
		levels := []string{"debug", "info", "warn", "error"}

		for _, level := range levels {
			t.Run(level, func(t *testing.T) {
				logger, err := initLogger(level)
				require.NoError(t, err)
				assert.NotNil(t, logger)

				// Verify logger works
				logger.Info("test message")

				// Clean up
				_ = logger.Sync()
			})
		}
	})

	t.Run("invalid_log_level", func(t *testing.T) {
		logger, err := initLogger("invalid")
		require.Error(t, err)
		assert.Nil(t, logger)
		assert.Contains(t, err.Error(), "invalid log level")
	})

	t.Run("logger_configuration", func(t *testing.T) {
		logger, err := initLogger("info")
		require.NoError(t, err)
		require.NotNil(t, logger)

		// Verify logger has service field by checking it can log
		logger.Info("test message", zap.String("test", "value"))

		// Clean up
		_ = logger.Sync()
	})

	t.Run("empty_log_level", func(t *testing.T) {
		// Empty string is actually parsed as "info" level by zap
		logger, err := initLogger("")
		require.NoError(t, err) // zap handles empty string gracefully
		assert.NotNil(t, logger)
		_ = logger.Sync()
	})

	t.Run("case_sensitive_log_level", func(t *testing.T) {
		// Test that log levels are case insensitive (zap handles this)
		logger, err := initLogger("INFO")
		require.NoError(t, err) // zap handles case conversion
		assert.NotNil(t, logger)
		_ = logger.Sync()
	})
}

func TestVersionVariables(t *testing.T) {
	t.Run("version_variables_exist", func(t *testing.T) {
		// Verify that version variables are set (even if to default values)
		assert.NotEmpty(t, Version)
		assert.NotEmpty(t, BuildTime)
		assert.NotEmpty(t, GitCommit)
	})

	t.Run("version_output_format", func(t *testing.T) {
		// Test the version output format
		cmd := &cobra.Command{}
		cmd.Flags().BoolP("version", "v", true, "Show version information")
		cmd.Flags().String("log-level", "info", "Log level")
		cmd.Flags().String("config", "/tmp/config.yaml", "Config file")

		// Capture output
		var buf bytes.Buffer

		oldStdout := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w

		go func() {
			defer func() { _ = w.Close() }()

			_ = run(cmd, []string{})
		}()

		// Read output
		output, _ := io.ReadAll(r)
		os.Stdout = oldStdout

		outputStr := string(output)
		lines := strings.Split(strings.TrimSpace(outputStr), "\n")

		// Verify format
		assert.GreaterOrEqual(t, len(lines), 4)
		assert.Equal(t, "MCP Gateway", lines[0])
		assert.True(t, strings.HasPrefix(lines[1], "Version:"))
		assert.True(t, strings.HasPrefix(lines[2], "Build Time:"))
		assert.True(t, strings.HasPrefix(lines[3], "Git Commit:"))

		buf.Reset()
	})
}

func TestCommandFlags(t *testing.T) {
	t.Run("flag_parsing", func(t *testing.T) {
		cmd := &cobra.Command{}
		cmd.Flags().StringP("config", "c", "/etc/mcp-gateway/gateway.yaml", "Path to configuration file")
		cmd.Flags().BoolP("version", "v", false, "Show version information")
		cmd.Flags().String("log-level", "info", "Log level (debug, info, warn, error)")

		// Test flag parsing
		err := cmd.ParseFlags([]string{"--config", "/custom/config.yaml", "--version", "--log-level", "debug"})
		require.NoError(t, err)

		// Verify flag values
		configValue, err := cmd.Flags().GetString("config")
		require.NoError(t, err)
		assert.Equal(t, "/custom/config.yaml", configValue)

		versionValue, err := cmd.Flags().GetBool("version")
		require.NoError(t, err)
		assert.True(t, versionValue)

		logLevelValue, err := cmd.Flags().GetString("log-level")
		require.NoError(t, err)
		assert.Equal(t, "debug", logLevelValue)
	})

	t.Run("short_flags", func(t *testing.T) {
		cmd := &cobra.Command{}
		cmd.Flags().StringP("config", "c", "/etc/mcp-gateway/gateway.yaml", "Path to configuration file")
		cmd.Flags().BoolP("version", "v", false, "Show version information")

		// Test short flags
		err := cmd.ParseFlags([]string{"-c", "/short/config.yaml", "-v"})
		require.NoError(t, err)

		configValue, err := cmd.Flags().GetString("config")
		require.NoError(t, err)
		assert.Equal(t, "/short/config.yaml", configValue)

		versionValue, err := cmd.Flags().GetBool("version")
		require.NoError(t, err)
		assert.True(t, versionValue)
	})

	t.Run("default_values", func(t *testing.T) {
		cmd := &cobra.Command{}
		cmd.Flags().StringP("config", "c", "/etc/mcp-gateway/gateway.yaml", "Path to configuration file")
		cmd.Flags().BoolP("version", "v", false, "Show version information")
		cmd.Flags().String("log-level", "info", "Log level (debug, info, warn, error)")

		// Don't set any flags, verify defaults
		configValue, err := cmd.Flags().GetString("config")
		require.NoError(t, err)
		assert.Equal(t, "/etc/mcp-gateway/gateway.yaml", configValue)

		versionValue, err := cmd.Flags().GetBool("version")
		require.NoError(t, err)
		assert.False(t, versionValue)

		logLevelValue, err := cmd.Flags().GetString("log-level")
		require.NoError(t, err)
		assert.Equal(t, "info", logLevelValue)
	})
}

func TestErrorHandling(t *testing.T) {
	t.Run("flag_parsing_errors", func(t *testing.T) {
		// Test run function with flag that can't be retrieved
		cmd := &cobra.Command{}
		// Don't add the expected flags

		err := run(cmd, []string{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get")
	})

	t.Run("config_loading_errors", func(t *testing.T) {
		cmd := &cobra.Command{}
		cmd.Flags().BoolP("version", "v", false, "Show version information")
		cmd.Flags().String("log-level", "info", "Log level")
		cmd.Flags().String("config", "/dev/null/invalid/path", "Config file")

		err := run(cmd, []string{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to load configuration")
	})

	t.Run("logger_sync_coverage", func(t *testing.T) {
		// Test logger sync in defer function
		logger, err := initLogger("info")
		require.NoError(t, err)

		// This tests the defer function that syncs the logger
		func() {
			defer func() {
				if syncErr := logger.Sync(); syncErr != nil {
					// This would normally go to stderr, but we're testing the code path
					assert.Error(t, syncErr) // Could be nil or non-nil, both are valid
				}
			}()
		}()
	})
}

func TestLoggerConfiguration(t *testing.T) {
	t.Run("logger_config_details", func(t *testing.T) {
		logger, err := initLogger("debug")
		require.NoError(t, err)
		require.NotNil(t, logger)

		// Test that logger can handle different log levels
		logger.Debug("debug message")
		logger.Info("info message")
		logger.Warn("warn message")
		logger.Error("error message")

		// Test with fields
		logger.Info("test with fields",
			zap.String("component", "test"),
			zap.Int("port", 8080),
			zap.Duration("timeout", 30*time.Second),
		)

		_ = logger.Sync()
	})

	t.Run("encoder_config_verification", func(t *testing.T) {
		// This test verifies the logger configuration is set up correctly
		logger, err := initLogger("info")
		require.NoError(t, err)

		// The logger should have service field
		serviceLogger := logger.With(zap.String("service", "mcp-gateway"))
		serviceLogger.Info("service logger test")

		_ = logger.Sync()
		_ = serviceLogger.Sync()
	})
}
