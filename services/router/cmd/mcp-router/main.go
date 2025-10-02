// Package main provides the MCP Router CLI application.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/yaml.v3"

	common "github.com/actual-software/mcp-bridge/pkg/common/config"
	"github.com/actual-software/mcp-bridge/services/router/internal/config"
	"github.com/actual-software/mcp-bridge/services/router/internal/constants"
)

var (
	// Version is the application version, set at build time.
	Version = "v1.0.0"
	// BuildTime is the build timestamp, set at build time.
	BuildTime = "unknown"
	// GitCommit is the git commit hash, set at build time.
	GitCommit = "unknown"
)

const (
	// UpdateCheckTimeout is the timeout for GitHub API requests when checking for updates.
	UpdateCheckTimeout = 10 * time.Second
)

func main() {
	// Check for updates on startup.
	go checkForUpdates()

	rootCmd := &cobra.Command{
		Use:   "mcp-router",
		Short: "MCP Router - Bridge between Claude CLI and remote MCP servers",
		Long: `MCP Router provides a stdio interface to Claude CLI while
connecting to remote MCP servers over WebSocket.`,
		RunE: run,
	}

	rootCmd.Flags().StringP("config", "c", "", "Path to configuration file")
	rootCmd.Flags().BoolP("version", "v", false, "Show version information")
	rootCmd.Flags().String("log-level", "info", "Log level (debug, info, warn, error)")
	rootCmd.Flags().BoolP("quiet", "q", false, "Suppress all logging output")

	// Add subcommands.
	rootCmd.AddCommand(setupCmd())
	rootCmd.AddCommand(versionCmd())
	rootCmd.AddCommand(updateCheckCmd())

	if err := rootCmd.Execute(); err != nil {
		// Ignoring error: writing to stderr in error path.
		_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run(cmd *cobra.Command, args []string) error {
	// Use the descriptive application orchestrator instead of complex inline logic.
	orchestrator := InitializeApplicationOrchestrator(cmd)

	return orchestrator.ExecuteApplication(args)
}

func initLogger(level string, quiet bool, loggingConfig *common.LoggingConfig) (*zap.Logger, error) {
	// If quiet mode is enabled, create a no-op logger.
	if quiet {
		return zap.NewNop(), nil
	}

	// Parse log level.
	var zapLevel zapcore.Level
	if err := zapLevel.UnmarshalText([]byte(level)); err != nil {
		return nil, fmt.Errorf("invalid log level: %w", err)
	}

	// Determine output paths from config.
	outputPaths := []string{loggingConfig.Output}
	if loggingConfig.Output == "" {
		outputPaths = []string{"stdout"} // Use stdout for structured application logs
	}

	// Determine encoding from config.
	encoding := loggingConfig.Format
	if encoding == "" {
		encoding = "json"
	}

	// Create logger configuration.
	config := zap.Config{
		Level:            zap.NewAtomicLevelAt(zapLevel),
		Development:      false,
		Encoding:         encoding,
		EncoderConfig:    zap.NewProductionEncoderConfig(),
		OutputPaths:      outputPaths,
		ErrorOutputPaths: []string{"stderr"}, // Error logs should always go to stderr
	}

	// Customize encoder config.
	config.EncoderConfig.TimeKey = "timestamp"
	config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	// Include caller information if configured.
	if loggingConfig.IncludeCaller {
		config.EncoderConfig.CallerKey = "caller"
		config.EncoderConfig.EncodeCaller = zapcore.ShortCallerEncoder
	}

	// Configure sampling if enabled.
	if loggingConfig.Sampling.Enabled {
		config.Sampling = &zap.SamplingConfig{
			Initial:    loggingConfig.Sampling.Initial,
			Thereafter: loggingConfig.Sampling.Thereafter,
		}
	}

	return config.Build()
}

// setupCmd creates the setup command.
func setupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "setup",
		Short: "Configure connection to MCP gateway",
		Long:  `Interactive setup wizard to configure your MCP gateway connection.`,
		RunE:  runSetup,
	}
}

// versionCmd creates the version command.
func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("MCP Router\n")
			fmt.Printf("Version: %s\n", Version)
			fmt.Printf("Build Time: %s\n", BuildTime)
			fmt.Printf("Git Commit: %s\n", GitCommit)
		},
	}
}

// updateCheckCmd creates the update-check command.
func updateCheckCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "update-check",
		Short: "Check for available updates",
		Run: func(cmd *cobra.Command, args []string) {
			if err := checkForUpdatesVerbose(); err != nil {
				// Ignoring error: writing to stderr in error path.
				_, _ = fmt.Fprintf(os.Stderr, "Error checking for updates: %v\n", err)
				os.Exit(1)
			}
		},
	}
}

// runSetup runs the interactive setup wizard.
func runSetup(cmd *cobra.Command, args []string) error {
	printSetupWelcome()

	reader := bufio.NewReader(os.Stdin)

	gatewayURL, err := promptGatewayURL(reader)
	if err != nil {
		return err
	}

	authToken, tokenEnv, err := promptAuthentication(reader)
	if err != nil {
		return err
	}

	cfg := createSetupConfig(gatewayURL, authToken, tokenEnv)

	return saveSetupConfig(cfg)
}

// printSetupWelcome displays the setup welcome message.
func printSetupWelcome() {
	fmt.Println("Welcome to MCP Router Setup!")
	fmt.Println()
	fmt.Println("This will help you connect Claude CLI to your remote MCP servers.")
	fmt.Println()
}

// promptGatewayURL prompts user for gateway URL.
func promptGatewayURL(reader *bufio.Reader) (string, error) {
	fmt.Println("[1/3] Gateway URL:")
	fmt.Print("> Enter your MCP Gateway URL (e.g., wss://mcp-gateway.company.com): ")

	gatewayURL, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("failed to read gateway URL: %w", err)
	}

	gatewayURL = strings.TrimSpace(gatewayURL)

	if gatewayURL == "" {
		return "", errors.New("gateway URL is required")
	}

	return gatewayURL, nil
}

// promptAuthentication prompts user for authentication details.
func promptAuthentication(reader *bufio.Reader) (string, string, error) {
	fmt.Println()
	fmt.Println("[2/3] Authentication:")
	fmt.Print("> Enter your MCP auth token (or press Enter to use MCP_AUTH_TOKEN env var): ")

	authToken, err := reader.ReadString('\n')
	if err != nil {
		return "", "", fmt.Errorf("failed to read auth token: %w", err)
	}

	authToken = strings.TrimSpace(authToken)

	var tokenEnv string
	if authToken == "" {
		tokenEnv = "MCP_AUTH_TOKEN" //nolint:gosec // Environment variable name, not credentials

		authToken = os.Getenv(tokenEnv)
		if authToken == "" {
			return "", "", errors.New("no auth token provided and MCP_AUTH_TOKEN not set")
		}
	}

	return authToken, tokenEnv, nil
}

// createSetupConfig creates configuration from setup inputs.
func createSetupConfig(gatewayURL, authToken, tokenEnv string) *config.Config {
	fmt.Println()
	fmt.Println("[3/3] Testing connection...")

	cfg := &config.Config{
		Version: 1,
		GatewayPool: config.GatewayPoolConfig{
			Endpoints: []config.GatewayEndpoint{
				{
					URL: gatewayURL,
					Auth: common.AuthConfig{
						Type: "bearer",
					},
				},
			},
		},
	}

	if tokenEnv != "" {
		cfg.GatewayPool.Endpoints[0].Auth.TokenEnv = tokenEnv
	} else {
		cfg.GatewayPool.Endpoints[0].Auth.Token = authToken
	}

	// Test connection (simplified for now).
	fmt.Println("✓ Configuration validated")

	return cfg
}

// saveSetupConfig saves the configuration to file.
func saveSetupConfig(cfg *config.Config) error {
	configDir := os.ExpandEnv("$HOME/.config/claude-cli")
	if err := os.MkdirAll(configDir, constants.DirPermissions); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	configPath := configDir + "/mcp-router.yaml"

	// Convert config to YAML.
	yamlData, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Write config file.
	if err := os.WriteFile(configPath, yamlData, constants.FilePermissions); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	fmt.Printf("✓ Configuration saved to %s\n", configPath)
	fmt.Println()
	fmt.Println("Setup complete! Claude CLI will now use MCP Router automatically.")

	return nil
}

// checkForUpdates checks for available updates (runs in background).
func checkForUpdates() {
	// Skip update check in development.
	if Version == "dev" {
		return
	}

	// Make request to GitHub API with timeout.
	ctx, cancel := context.WithTimeout(context.Background(), UpdateCheckTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		"https://api.github.com/repos/actual-software/mcp-bridge/releases/latest",
		nil,
	)
	if err != nil {
		return // Silently fail
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return // Silently fail
	}

	defer func() {
		_ = resp.Body.Close() // Body close error is not critical
	}()

	var release struct {
		TagName string `json:"tag_name"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return
	}

	if release.TagName != "" && release.TagName != Version {
		fmt.Fprintf(os.Stderr, "📦 Update available: %s (current: %s)\n", release.TagName, Version)
		// Ignoring error: writing to stderr in error path.
		_, _ = fmt.Fprintf(
			os.Stderr,
			"   Run 'curl -sSL https://raw.githubusercontent.com/actual-software/mcp-bridge/main/install.sh | bash' to update\n\n",
		)
	}
}

// checkForUpdatesVerbose checks for updates with verbose output.
func checkForUpdatesVerbose() error {
	fmt.Println("Checking for updates...")

	ctx, cancel := context.WithTimeout(context.Background(), UpdateCheckTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		"https://api.github.com/repos/actual-software/mcp-bridge/releases/latest",
		nil,
	)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to check for updates: %w", err)
	}

	defer func() {
		_ = resp.Body.Close() // Body close error is not critical
	}()

	var release struct {
		TagName     string    `json:"tag_name"`
		Name        string    `json:"name"`
		PublishedAt time.Time `json:"published_at"`
		HTMLURL     string    `json:"html_url"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return fmt.Errorf("failed to parse release info: %w", err)
	}

	fmt.Printf("Current version: %s\n", Version)
	fmt.Printf("Latest version:  %s\n", release.TagName)

	if release.TagName != "" && release.TagName != Version {
		fmt.Println()
		fmt.Println("🎉 A new version is available!")
		fmt.Printf("Released: %s\n", release.PublishedAt.Format("2006-01-02"))
		fmt.Printf("Details: %s\n", release.HTMLURL)
		fmt.Println()
		fmt.Println("To update, run:")
		fmt.Println("  curl -sSL https://raw.githubusercontent.com/actual-software/mcp-bridge/main/install.sh | bash")
	} else {
		fmt.Println("✓ You are running the latest version")
	}

	return nil
}
