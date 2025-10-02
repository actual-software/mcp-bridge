package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"

	common "github.com/poiley/mcp-bridge/pkg/common/config"

	"github.com/poiley/mcp-bridge/services/router/internal/config"
)

// Test variables.
var (
	testVersion   = "v1.0.0"
	testBuildTime = "2024-01-01T00:00:00Z"
	testGitCommit = "abc123"
)

func TestMain(m *testing.M) {
	// Set test versions.
	Version = testVersion
	BuildTime = testBuildTime
	GitCommit = testGitCommit

	// Run tests.
	code := m.Run()
	os.Exit(code)
}

func TestVersionCommand(t *testing.T) {
	// Capture output.
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Run version command.
	cmd := versionCmd()
	cmd.Run(cmd, []string{})

	// Restore stdout.
	if err := w.Close(); err != nil {
		t.Logf("Failed to close: %v", err)
	}

	os.Stdout = oldStdout

	// Read output.
	output := new(bytes.Buffer)
	if _, err := io.Copy(output, r); err != nil {
		t.Logf("Failed to copy data: %v", err)
	}

	// Verify output.
	outputStr := output.String()
	assert.Contains(t, outputStr, "MCP Router")
	assert.Contains(t, outputStr, "Version: "+testVersion)
	assert.Contains(t, outputStr, "Build Time: "+testBuildTime)
	assert.Contains(t, outputStr, "Git Commit: "+testGitCommit)
}

func TestVersionFlag(t *testing.T) {
	// Create root command.
	rootCmd := &cobra.Command{
		Use:  "mcp-router",
		RunE: run,
	}
	rootCmd.Flags().BoolP("version", "v", false, "Show version information")
	rootCmd.Flags().String("log-level", "info", "Log level")
	rootCmd.Flags().StringP("config", "c", "", "Config file")

	// Capture output.
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Set version flag.
	rootCmd.SetArgs([]string{"--version"})
	err := rootCmd.Execute()

	// Restore stdout.
	if err := w.Close(); err != nil {
		t.Logf("Failed to close: %v", err)
	}

	os.Stdout = oldStdout

	// Read output.
	output := new(bytes.Buffer)
	if _, err := io.Copy(output, r); err != nil {
		t.Logf("Failed to copy data: %v", err)
	}

	// Verify.
	require.NoError(t, err)

	outputStr := output.String()
	assert.Contains(t, outputStr, testVersion)
}

func TestInitLogger(t *testing.T) {
	tests := []struct {
		name      string
		level     string
		expectErr bool
	}{
		{
			name:      "Valid debug level",
			level:     "debug",
			expectErr: false,
		},
		{
			name:      "Valid info level",
			level:     "info",
			expectErr: false,
		},
		{
			name:      "Valid warn level",
			level:     "warn",
			expectErr: false,
		},
		{
			name:      "Valid error level",
			level:     "error",
			expectErr: false,
		},
		{
			name:      "Invalid level",
			level:     "invalid",
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, err := initLogger(tt.level, false, &common.LoggingConfig{
				Level:  "info",
				Format: "json",
				Output: "stderr",
			})

			if tt.expectErr {
				require.Error(t, err)
				assert.Nil(t, logger)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, logger)

				// Verify logger level.
				atom := zap.NewAtomicLevel()
				if err := atom.UnmarshalText([]byte(tt.level)); err != nil {
					t.Fatalf("Failed to unmarshal log level: %v", err)
				}

				assert.Equal(t, atom.Level(), logger.Level())
			}
		})
	}
}

func TestRunSetup(t *testing.T) {
	// Create temporary directory for config.
	tempDir := t.TempDir()

	oldHome := os.Getenv("HOME")
	if err := os.Setenv("HOME", tempDir); err != nil {
		t.Fatalf("Failed to set environment variable: %v", err)
	}

	defer func() {
		if err := os.Setenv("HOME", oldHome); err != nil {
			t.Fatalf("Failed to restore HOME: %v", err)
		}
	}()

	tests := []struct {
		name      string
		input     string
		envToken  string
		expectErr bool
	}{
		{
			name:      "Valid setup with direct token",
			input:     "wss://gateway.example.com\nmytoken123\n",
			expectErr: false,
		},
		{
			name:      "Valid setup with env token",
			input:     "wss://gateway.example.com\n\n",
			envToken:  "env-token-123",
			expectErr: false,
		},
		{
			name:      "Empty gateway URL",
			input:     "\n",
			expectErr: true,
		},
		{
			name:      "No token provided",
			input:     "wss://gateway.example.com\n\n",
			envToken:  "",
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runSetupTest(t, tt, tempDir)
		})
	}
}

// runSetupTest is a helper function that runs a single setup test case.
func runSetupTest(t *testing.T, tt struct {
	name      string
	input     string
	envToken  string
	expectErr bool
}, tempDir string) {
	t.Helper()
	// Set up environment.
	if tt.envToken != "" {
		if err := os.Setenv("MCP_AUTH_TOKEN", tt.envToken); err != nil {
			t.Fatalf("Failed to set environment variable: %v", err)
		}

		defer func() {
			_ = os.Unsetenv("MCP_AUTH_TOKEN")
		}()
	}

	// Replace stdin with test input.
	oldStdin := os.Stdin
	r, w, _ := os.Pipe()
	os.Stdin = r

	_, _ = w.WriteString(tt.input)
	if err := w.Close(); err != nil {
		t.Logf("Failed to close: %v", err)
	}

	defer func() { os.Stdin = oldStdin }()

	// Capture output.
	oldStdout := os.Stdout
	outR, outW, _ := os.Pipe()
	os.Stdout = outW

	// Run setup.
	cmd := setupCmd()
	err := runSetup(cmd, []string{})

	// Restore stdout.
	if err := outW.Close(); err != nil {
		t.Logf("Failed to close: %v", err)
	}

	os.Stdout = oldStdout

	// Read output.
	output := new(bytes.Buffer)
	if _, err := io.Copy(output, outR); err != nil {
		t.Logf("Failed to copy data: %v", err)
	}

	if tt.expectErr {
		require.Error(t, err)
	} else {
		verifySetupSuccess(t, err, tempDir, tt.envToken)
	}
}

// verifySetupSuccess verifies that the setup completed successfully.
func verifySetupSuccess(t *testing.T, err error, tempDir, envToken string) {
	t.Helper()
	require.NoError(t, err)

	// Verify config file was created.
	configPath := filepath.Join(tempDir, ".config", "claude-cli", "mcp-router.yaml")
	assert.FileExists(t, configPath)

	// Verify config content.
	data, readErr := os.ReadFile(filepath.Clean(configPath))
	require.NoError(t, readErr)

	var cfg config.Config

	unmarshalErr := yaml.Unmarshal(data, &cfg)
	require.NoError(t, unmarshalErr)

	assert.Len(t, cfg.GatewayPool.Endpoints, 1)
	assert.Equal(t, "wss://gateway.example.com", cfg.GatewayPool.Endpoints[0].URL)
	assert.Equal(t, "bearer", cfg.GatewayPool.Endpoints[0].Auth.Type)

	if envToken != "" {
		assert.Equal(t, "MCP_AUTH_TOKEN", cfg.GatewayPool.Endpoints[0].Auth.TokenEnv)
	} else {
		assert.Equal(t, "mytoken123", cfg.GatewayPool.Endpoints[0].Auth.Token)
	}
}

func TestUpdateCheckCommand(t *testing.T) {
	// Create mock GitHub API server.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/poiley/mcp-bridge/releases/latest" {
			w.Header().Set("Content-Type", "application/json")

			_, _ = w.Write([]byte(`{
				"tag_name": "v2.0.0",
				"name": "Version 2.0.0",
				"published_at": "2024-01-15T10:00:00Z",
				"html_url": "https://github.com/poiley/mcp-bridge/releases/tag/v2.0.0"
			}`)) // Ignore write error in test server
		}
	}))
	defer server.Close()

	// Override GitHub API URL in test.
	oldClient := http.DefaultClient
	http.DefaultClient = &http.Client{
		Transport: &testTransport{
			server: server,
		},
	}

	defer func() { http.DefaultClient = oldClient }()

	// Capture output.
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Run update check.
	cmd := updateCheckCmd()
	cmd.Run(cmd, []string{})

	// Restore stdout.
	if err := w.Close(); err != nil {
		t.Logf("Failed to close: %v", err)
	}

	os.Stdout = oldStdout

	// Read output.
	output := new(bytes.Buffer)
	if _, err := io.Copy(output, r); err != nil {
		t.Logf("Failed to copy data: %v", err)
	}

	// Verify output.
	outputStr := output.String()
	assert.Contains(t, outputStr, "Current version: v1.0.0")
	assert.Contains(t, outputStr, "Latest version:  v2.0.0")
	assert.Contains(t, outputStr, "A new version is available!")
}

func TestCheckForUpdates(t *testing.T) {
	t.Run("Skip in dev version", func(t *testing.T) {
		oldVersion := Version
		Version = "dev"

		defer func() { Version = oldVersion }()

		// Should return immediately without making any requests.
		checkForUpdates()
		// No assertions needed - just verify it doesn't panic.
	})

	t.Run("New version available", func(t *testing.T) {
		// Create mock server.
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")

			_, _ = w.Write([]byte(`{"tag_name": "v2.0.0"}`)) // Ignore write error in test server
		}))
		defer server.Close()

		// Override HTTP client.
		oldClient := http.DefaultClient
		http.DefaultClient = &http.Client{
			Transport: &testTransport{server: server},
		}

		defer func() { http.DefaultClient = oldClient }()

		// Capture stderr.
		oldStderr := os.Stderr
		r, w, _ := os.Pipe()
		os.Stderr = w

		// Run check.
		checkForUpdates()

		// Restore stderr.
		if err := w.Close(); err != nil {
			t.Logf("Failed to close: %v", err)
		}

		os.Stderr = oldStderr

		// Read output.
		output := new(bytes.Buffer)
		if _, err := io.Copy(output, r); err != nil {
			t.Logf("Failed to copy data: %v", err)
		}

		// Verify.
		outputStr := output.String()
		assert.Contains(t, outputStr, "Update available: v2.0.0")
	})
}
func TestRunCommand(t *testing.T) {
	t.Run("Missing config", func(t *testing.T) {
		testMissingConfig(t)
	})

	t.Run("Invalid log level", func(t *testing.T) {
		testInvalidLogLevel(t)
	})

	t.Run("Context cancellation", func(t *testing.T) {
		testContextCancellation(t)
	})
}

// testMissingConfig tests the missing config scenario.
func testMissingConfig(t *testing.T) {
	t.Helper()

	rootCmd := &cobra.Command{
		Use:  "mcp-router",
		RunE: run,
	}
	rootCmd.Flags().StringP("config", "c", "", "Config file")
	rootCmd.Flags().BoolP("version", "v", false, "Version")
	rootCmd.Flags().String("log-level", "info", "Log level")
	rootCmd.Flags().BoolP("quiet", "q", false, "Suppress all logging output")

	// Set non-existent config file.
	rootCmd.SetArgs([]string{"--config", "/non/existent/config.yaml"})
	err := rootCmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load configuration")
}

// testInvalidLogLevel tests the invalid log level scenario.
func testInvalidLogLevel(t *testing.T) {
	t.Helper()
	// Set environment variable for auth token.
	oldToken := os.Getenv("MCP_AUTH_TOKEN")
	if err := os.Setenv("MCP_AUTH_TOKEN", "test-token"); err != nil {
		t.Fatalf("Failed to set environment variable: %v", err)
	}

	defer func() {
		if oldToken != "" {
			if err := os.Setenv("MCP_AUTH_TOKEN", oldToken); err != nil {
				t.Fatalf("Failed to set environment variable: %v", err)
			}
		} else {
			_ = os.Unsetenv("MCP_AUTH_TOKEN")
		}
	}()

	// Create minimal config file.
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")

	cfg := &config.Config{
		Version: 1,
		GatewayPool: config.GatewayPoolConfig{
			Endpoints: []config.GatewayEndpoint{
				{
					URL:  "ws://localhost:8080",
					Auth: common.AuthConfig{Type: "bearer", Token: "test-token"},
				},
			},
		},
	}

	configData, err := yaml.Marshal(cfg)
	require.NoError(t, err)
	err = os.WriteFile(configPath, configData, 0o600)
	require.NoError(t, err)

	rootCmd := &cobra.Command{
		Use:  "mcp-router",
		RunE: run,
	}
	rootCmd.Flags().StringP("config", "c", "", "Config file")
	rootCmd.Flags().BoolP("version", "v", false, "Version")
	rootCmd.Flags().String("log-level", "invalid", "Log level")
	rootCmd.Flags().BoolP("quiet", "q", false, "Suppress all logging output")

	// Set the config file and invalid log level.
	rootCmd.SetArgs([]string{"--config", configPath, "--log-level", "invalid"})
	err = rootCmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to initialize logger")
}

// testContextCancellation tests the context cancellation scenario.
func testContextCancellation(t *testing.T) {
	t.Helper()
	// Set environment variable for auth token.
	oldToken := os.Getenv("MCP_AUTH_TOKEN")
	if err := os.Setenv("MCP_AUTH_TOKEN", "test-token"); err != nil {
		t.Fatalf("Failed to set environment variable: %v", err)
	}

	defer restoreAuthToken(t, oldToken)

	// Create minimal config file.
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")

	cfg := createTestConfig()
	data, err := yaml.Marshal(cfg)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(configPath, data, 0o600))

	// Create command with valid config.
	rootCmd := createTestRootCommand()
	rootCmd.SetArgs([]string{"--config", configPath})
	err = rootCmd.Execute()
	require.NoError(t, err)
}

// restoreAuthToken restores the MCP_AUTH_TOKEN environment variable.
func restoreAuthToken(t *testing.T, oldToken string) {
	t.Helper()

	if oldToken != "" {
		if err := os.Setenv("MCP_AUTH_TOKEN", oldToken); err != nil {
			t.Fatalf("Failed to set environment variable: %v", err)
		}
	} else {
		_ = os.Unsetenv("MCP_AUTH_TOKEN")
	}
}

// createTestConfig creates a minimal test configuration.
func createTestConfig() *config.Config {
	return &config.Config{
		Version: 1,
		GatewayPool: config.GatewayPoolConfig{
			Endpoints: []config.GatewayEndpoint{
				{
					URL: "ws://localhost:8080",
					Auth: common.AuthConfig{
						Type: "bearer",
					},
				},
			},
		},
	}
}

// createTestRootCommand creates a test root command with cancellation handling.
func createTestRootCommand() *cobra.Command {
	rootCmd := &cobra.Command{
		Use: "mcp-router",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Override run to use test logger and immediate cancellation.
			logLevel, _ := cmd.Flags().GetString("log-level")
			_, err := initLogger(logLevel, false, &common.LoggingConfig{
				Level:  "info",
				Format: "json",
				Output: "stderr",
			})
			if err != nil {
				return err
			}

			configPath, _ := cmd.Flags().GetString("config")
			_, err = config.Load(configPath)
			if err != nil {
				return err
			}

			// Create context that's immediately cancelled.
			ctx, cancel := context.WithCancel(context.Background())
			cancel()

			// Should handle cancelled context gracefully.
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(time.Second):
				return errors.New("timeout waiting for cancellation")
			}
		},
	}

	rootCmd.Flags().StringP("config", "c", "", "Config file")
	rootCmd.Flags().BoolP("version", "v", false, "Version")
	rootCmd.Flags().String("log-level", "info", "Log level")

	return rootCmd
}

// testTransport redirects requests to test server.
type testTransport struct {
	server *httptest.Server
}

func (t *testTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Redirect to test server.
	req.URL.Scheme = "http"
	req.URL.Host = t.server.Listener.Addr().String()

	return http.DefaultTransport.RoundTrip(req)
}
