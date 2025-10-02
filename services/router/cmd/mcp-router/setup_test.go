package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	common "github.com/actual-software/mcp-bridge/pkg/common/config"
	"github.com/actual-software/mcp-bridge/services/router/internal/config"
)

func TestSetupInteractive(t *testing.T) {
	tempHome := setupTestHome(t)
	tests := createSetupInteractiveTests()
	runSetupInteractiveTests(t, tempHome, tests)
}

func setupTestHome(t *testing.T) string {
	t.Helper()

	tempHome := t.TempDir()
	oldHome := os.Getenv("HOME")

	if err := os.Setenv("HOME", tempHome); err != nil {
		t.Fatalf("Failed to set environment variable: %v", err)
	}

	t.Cleanup(func() {
		if err := os.Setenv("HOME", oldHome); err != nil {
			t.Logf("Failed to restore env: %v", err)
		}
	})

	return tempHome
}

func createSetupInteractiveTests() []struct {
	name           string
	gatewayURL     string
	authToken      string
	useEnvToken    bool
	envTokenValue  string
	expectSuccess  bool
	expectedConfig *config.Config
} {
	return []struct {
		name           string
		gatewayURL     string
		authToken      string
		useEnvToken    bool
		envTokenValue  string
		expectSuccess  bool
		expectedConfig *config.Config
	}{
		{
			name:           "Direct token input",
			gatewayURL:     "wss://gateway.example.com",
			authToken:      "my-secret-token",
			expectSuccess:  true,
			expectedConfig: createExpectedConfig("wss://gateway.example.com", "my-secret-token", ""),
		},
		{
			name:           "Environment variable token",
			gatewayURL:     "ws://localhost:8080",
			authToken:      "",
			useEnvToken:    true,
			envTokenValue:  "env-token-123",
			expectSuccess:  true,
			expectedConfig: createExpectedConfig("ws://localhost:8080", "", "MCP_AUTH_TOKEN"),
		},
		{
			name:           "TCP URL with token",
			gatewayURL:     "tcp://gateway.internal:9000",
			authToken:      "tcp-token",
			expectSuccess:  true,
			expectedConfig: createExpectedConfig("tcp://gateway.internal:9000", "tcp-token", ""),
		},
		{
			name:          "Empty gateway URL",
			gatewayURL:    "",
			authToken:     "token",
			expectSuccess: false,
		},
		{
			name:          "No token provided",
			gatewayURL:    "wss://gateway.example.com",
			authToken:     "",
			useEnvToken:   false,
			expectSuccess: false,
		},
	}
}

func createExpectedConfig(url, token, tokenEnv string) *config.Config {
	return &config.Config{
		Version: 1,
		GatewayPool: config.GatewayPoolConfig{
			Endpoints: []config.GatewayEndpoint{
				{
					URL: url,
					Auth: common.AuthConfig{
						Type:     "bearer",
						Token:    token,
						TokenEnv: tokenEnv,
					},
				},
			},
		},
	}
}

func runSetupInteractiveTests(t *testing.T, tempHome string, tests []struct {
	name           string
	gatewayURL     string
	authToken      string
	useEnvToken    bool
	envTokenValue  string
	expectSuccess  bool
	expectedConfig *config.Config
}) {
	t.Helper()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setupEnvForTest(t, tt)
			input := fmt.Sprintf("%s\n%s\n", tt.gatewayURL, tt.authToken)

			err := runSetupWithInput(strings.NewReader(input), tempHome)

			if tt.expectSuccess {
				verifySuccessfulSetup(t, tempHome, tt, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func setupEnvForTest(t *testing.T, tt struct {
	name           string
	gatewayURL     string
	authToken      string
	useEnvToken    bool
	envTokenValue  string
	expectSuccess  bool
	expectedConfig *config.Config
}) {
	t.Helper()

	if tt.useEnvToken {
		if err := os.Setenv("MCP_AUTH_TOKEN", tt.envTokenValue); err != nil {
			t.Fatalf("Failed to set environment variable: %v", err)
		}

		t.Cleanup(func() { _ = os.Unsetenv("MCP_AUTH_TOKEN") })
	}
}

func verifySuccessfulSetup(t *testing.T, tempHome string, tt struct {
	name           string
	gatewayURL     string
	authToken      string
	useEnvToken    bool
	envTokenValue  string
	expectSuccess  bool
	expectedConfig *config.Config
}, err error) {
	t.Helper()

	require.NoError(t, err)

	configPath := filepath.Join(tempHome, ".config", "claude-cli", "mcp-router.yaml")
	assert.FileExists(t, configPath)

	cfg := loadConfigFromFile(t, configPath)
	verifyConfigContents(t, cfg, tt)

	_ = os.RemoveAll(filepath.Join(tempHome, ".config"))
}

func loadConfigFromFile(t *testing.T, configPath string) config.Config {
	t.Helper()

	data, err := os.ReadFile(filepath.Clean(configPath))
	require.NoError(t, err)

	var cfg config.Config

	err = yaml.Unmarshal(data, &cfg)
	require.NoError(t, err)

	return cfg
}

func verifyConfigContents(t *testing.T, cfg config.Config, tt struct {
	name           string
	gatewayURL     string
	authToken      string
	useEnvToken    bool
	envTokenValue  string
	expectSuccess  bool
	expectedConfig *config.Config
}) {
	t.Helper()

	assert.Equal(t, tt.expectedConfig.Version, cfg.Version)
	assert.Len(t, cfg.GatewayPool.Endpoints, 1)
	assert.Equal(t, tt.expectedConfig.GatewayPool.Endpoints[0].URL, cfg.GatewayPool.Endpoints[0].URL)
	assert.Equal(t, tt.expectedConfig.GatewayPool.Endpoints[0].Auth.Type, cfg.GatewayPool.Endpoints[0].Auth.Type)

	if tt.useEnvToken {
		assert.Equal(t, tt.expectedConfig.GatewayPool.Endpoints[0].Auth.TokenEnv, cfg.GatewayPool.Endpoints[0].Auth.TokenEnv)
		assert.Empty(t, cfg.GatewayPool.Endpoints[0].Auth.Token)
	} else {
		assert.Equal(t, tt.expectedConfig.GatewayPool.Endpoints[0].Auth.Token, cfg.GatewayPool.Endpoints[0].Auth.Token)
		assert.Empty(t, cfg.GatewayPool.Endpoints[0].Auth.TokenEnv)
	}
}

func TestSetupValidation(t *testing.T) {
	tests := []struct {
		name        string
		url         string
		shouldError bool
		errorMsg    string
	}{
		{
			name:        "Valid WebSocket URL",
			url:         "wss://gateway.example.com",
			shouldError: false,
		},
		{
			name:        "Valid WebSocket URL with port",
			url:         "ws://localhost:8080",
			shouldError: false,
		},
		{
			name:        "Valid TCP URL",
			url:         "tcp://gateway.internal:9000",
			shouldError: false,
		},
		{
			name:        "Valid secure TCP URL",
			url:         "tcps://gateway.example.com:9443",
			shouldError: false,
		},
		{
			name:        "Empty URL",
			url:         "",
			shouldError: true,
			errorMsg:    "gateway URL is required",
		},
		{
			name:        "URL with path",
			url:         "wss://gateway.example.com/mcp",
			shouldError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary home.
			tempHome := t.TempDir()

			// Prepare input.
			input := tt.url + "\ntest-token\n"

			// Run setup.
			err := runSetupWithInput(strings.NewReader(input), tempHome)

			if tt.shouldError {
				require.Error(t, err)

				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestSetupConfigCreation(t *testing.T) {
	t.Run("Creates config directory", func(t *testing.T) {
		tempHome := t.TempDir()

		input := "wss://gateway.example.com\ntoken123\n"
		err := runSetupWithInput(strings.NewReader(input), tempHome)
		require.NoError(t, err)

		// Verify directory structure.
		configDir := filepath.Join(tempHome, ".config", "claude-cli")
		assert.DirExists(t, configDir)

		// Verify file permissions.
		info, err := os.Stat(filepath.Join(configDir, "mcp-router.yaml"))
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	})

	t.Run("Overwrites existing config", func(t *testing.T) {
		tempHome := t.TempDir()
		configPath := filepath.Join(tempHome, ".config", "claude-cli", "mcp-router.yaml")

		// Create existing config.
		_ = os.MkdirAll(filepath.Dir(configPath), 0o750)
		_ = os.WriteFile(configPath, []byte("old: config"), 0o600)

		// Run setup.
		input := "wss://new-gateway.example.com\nnew-token\n"
		err := runSetupWithInput(strings.NewReader(input), tempHome)
		require.NoError(t, err)

		// Verify new config.
		data, err := os.ReadFile(filepath.Clean(configPath))
		require.NoError(t, err)
		assert.Contains(t, string(data), "new-gateway.example.com")
		assert.NotContains(t, string(data), "old: config")
	})
}

func TestSetupOutput(t *testing.T) {
	tempHome := t.TempDir()

	oldHome := os.Getenv("HOME")
	if err := os.Setenv("HOME", tempHome); err != nil {
		t.Fatalf("Failed to set environment variable: %v", err)
	}

	defer func() {
		if err := os.Setenv("HOME", oldHome); err != nil {
			t.Logf("Failed to restore env: %v", err)
		}
	}()

	// Capture output.
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Run setup.
	cmd := setupCmd()
	oldStdin := os.Stdin
	stdinR, stdinW, _ := os.Pipe()
	os.Stdin = stdinR

	// Write input.
	if _, err := fmt.Fprintln(stdinW, "wss://gateway.example.com"); err != nil {
		t.Logf("Failed to write: %v", err)
	}

	if _, err := fmt.Fprintln(stdinW, "test-token"); err != nil {
		t.Logf("Failed to write: %v", err)
	}

	_ = stdinW.Close()

	err := runSetup(cmd, []string{})

	// Restore.
	os.Stdin = oldStdin

	_ = w.Close()

	os.Stdout = oldStdout

	// Read output.
	output, _ := io.ReadAll(r)
	outputStr := string(output)

	// Verify output messages.
	require.NoError(t, err)
	assert.Contains(t, outputStr, "Welcome to MCP Router Setup!")
	assert.Contains(t, outputStr, "[1/3] Gateway URL:")
	assert.Contains(t, outputStr, "[2/3] Authentication:")
	assert.Contains(t, outputStr, "[3/3] Testing connection...")
	assert.Contains(t, outputStr, "✓ Configuration validated")
	assert.Contains(t, outputStr, "✓ Configuration saved")
	assert.Contains(t, outputStr, "Setup complete!")
}

// Helper function to run setup with custom input.
func runSetupWithInput(input io.Reader, homeDir string) error {
	// Save original stdin/stdout.
	oldStdin := os.Stdin
	oldStdout := os.Stdout
	oldHome := os.Getenv("HOME")

	// Set test home.
	if err := os.Setenv("HOME", homeDir); err != nil {
		return fmt.Errorf("failed to set environment variable: %w", err)
	}

	// Replace stdin.
	r, w, _ := os.Pipe()
	os.Stdin = r

	go func() {
		_, _ = io.Copy(w, input) // Best effort copy
		_ = w.Close()
	}()

	// Capture stdout.
	outR, outW, _ := os.Pipe()
	os.Stdout = outW

	// Run setup.
	cmd := setupCmd()
	err := runSetup(cmd, []string{})

	// Restore.
	os.Stdin = oldStdin

	_ = outW.Close()

	os.Stdout = oldStdout
	if err := os.Setenv("HOME", oldHome); err != nil {
		return fmt.Errorf("failed to restore HOME variable: %w", err)
	}

	// Drain output.
	go func() {
		_, _ = io.Copy(io.Discard, outR)
	}()

	return err
}

func TestConnectionTest(t *testing.T) {
	// Create mock gateway server.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate WebSocket upgrade.
		if r.Header.Get("Upgrade") == "websocket" {
			w.WriteHeader(http.StatusSwitchingProtocols)
		} else {
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	// Convert http:// to ws:// for the test.
	wsURL := strings.Replace(server.URL, "http://", "ws://", 1)

	t.Run("Test connection during setup", func(t *testing.T) {
		tempHome := t.TempDir()

		input := wsURL + "\ntest-token\n"
		err := runSetupWithInput(strings.NewReader(input), tempHome)

		// Should succeed even though we can't actually connect.
		// The setup command currently just validates the config.
		require.NoError(t, err)
	})
}
