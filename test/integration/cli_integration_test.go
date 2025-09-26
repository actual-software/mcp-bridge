// Integration test files allow flexible style
//

package integration

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
)

// CLIIntegrationTestSuite tests CLI commands for both gateway and router.
type CLIIntegrationTestSuite struct {
	suite.Suite
	ctx           context.Context
	gatewayBinary string
	routerBinary  string
	projectRoot   string
}

// SetupSuite runs before all CLI tests.
func (s *CLIIntegrationTestSuite) SetupSuite() {
	s.ctx = context.Background()

	// Find project root
	wd, err := os.Getwd()
	s.Require().NoError(err)
	s.projectRoot = filepath.Join(wd, "..", "..")

	// Build binaries for testing
	s.buildBinaries()
}

// buildBinaries builds the CLI binaries for testing.
func (s *CLIIntegrationTestSuite) buildBinaries() {
	s.T().Log("Building CLI binaries for testing")

	// Build gateway binary
	gatewayCmd := exec.CommandContext(context.Background(), "go", "build",
		"-o", "mcp-gateway-test", "./services/gateway/cmd/mcp-gateway")
	gatewayCmd.Dir = s.projectRoot
	err := gatewayCmd.Run()
	s.Require().NoError(err, "Failed to build gateway binary")
	s.gatewayBinary = filepath.Join(s.projectRoot, "mcp-gateway-test")

	// Build router binary
	routerCmd := exec.CommandContext(context.Background(), "go", "build",
		"-o", "mcp-router-test", "./services/router/cmd/mcp-router")
	routerCmd.Dir = s.projectRoot
	err = routerCmd.Run()
	s.Require().NoError(err, "Failed to build router binary")
	s.routerBinary = filepath.Join(s.projectRoot, "mcp-router-test")

	// Verify binaries exist
	s.Require().FileExists(s.gatewayBinary, "Gateway binary should exist")
	s.Require().FileExists(s.routerBinary, "Router binary should exist")
}

// TearDownSuite cleans up after all CLI tests.
func (s *CLIIntegrationTestSuite) TearDownSuite() {
	// Clean up test binaries
	if s.gatewayBinary != "" {
		_ = os.Remove(s.gatewayBinary)
	}

	if s.routerBinary != "" {
		_ = os.Remove(s.routerBinary)
	}
}

// TestGatewayVersionCommand tests the gateway version command.
// safeExecCommand creates an exec.Cmd with validated binary path.
func (s *CLIIntegrationTestSuite) safeExecCommand(ctx context.Context, binary string, args ...string) *exec.Cmd {
	// For test binaries, we control the paths completely
	// These are built in SetupSuite with hardcoded names
	allowedBinaries := map[string]bool{
		s.gatewayBinary: true,
		s.routerBinary:  true,
	}

	if !allowedBinaries[binary] {
		s.T().Fatalf("Unexpected binary path: %s", binary)
	}

	// All args must be hardcoded strings passed by the test, not variables
	return exec.CommandContext(ctx, binary, args...)
}

func (s *CLIIntegrationTestSuite) TestGatewayVersionCommand() {
	s.T().Log("Testing gateway version command")

	ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
	defer cancel()

	cmd := s.safeExecCommand(ctx, s.gatewayBinary, "--version")
	cmd.Dir = s.projectRoot

	output, err := cmd.Output()
	s.Require().NoError(err, "Version command should succeed")

	outputStr := string(output)
	s.Contains(outputStr, "MCP Gateway", "Should contain gateway name")
	s.Contains(outputStr, "Version:", "Should contain version info")
	s.Contains(outputStr, "Build Time:", "Should contain build time")
	s.Contains(outputStr, "Git Commit:", "Should contain git commit")
}

// TestGatewayAdminStatusCommand tests the gateway admin status command.
func (s *CLIIntegrationTestSuite) TestGatewayAdminStatusCommand() {
	s.T().Log("Testing gateway admin status command")

	ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
	defer cancel()

	cmd := s.safeExecCommand(ctx, s.gatewayBinary, "admin", "status")
	cmd.Dir = s.projectRoot

	output, err := cmd.Output()
	s.Require().NoError(err, "Admin status command should succeed")

	outputStr := string(output)
	s.Contains(outputStr, "MCP Gateway Status", "Should contain status header")
	s.Contains(outputStr, "Version:", "Should contain version")
	s.Contains(outputStr, "Status:", "Should contain status")
	s.Contains(outputStr, "Uptime:", "Should contain uptime")
	s.Contains(outputStr, "Connections:", "Should contain connections")
	s.Contains(outputStr, "Namespaces:", "Should contain namespaces")
	s.Contains(outputStr, "Endpoints:", "Should contain endpoints")
}

// TestGatewayTokenCommands tests the gateway token management commands.
func (s *CLIIntegrationTestSuite) TestGatewayTokenCommands() {
	s.T().Log("Testing gateway token commands")

	// Test token create
	s.Run("TokenCreate", func() {
		ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
		defer cancel()

		// Set JWT secret for testing
		env := append(os.Environ(), "JWT_SECRET_KEY=test-secret-for-cli-integration")

		cmd := s.safeExecCommand(ctx, s.gatewayBinary, "admin", "token", "create", "--user", "test-user", "--expiry", "1h")
		cmd.Dir = s.projectRoot
		cmd.Env = env

		output, err := cmd.Output()
		s.Require().NoError(err, "Token create command should succeed")

		outputStr := string(output)
		s.Contains(outputStr, "Creating new auth token", "Should contain creation message")
		s.Contains(outputStr, "Token:", "Should contain token")
		s.Contains(outputStr, "Expires:", "Should contain expiry")
		s.Contains(outputStr, "User: test-user", "Should contain user")
		s.Contains(outputStr, "Scopes:", "Should contain scopes")
	})

	// Test token list
	s.Run("TokenList", func() {
		ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
		defer cancel()

		cmd := s.safeExecCommand(ctx, s.gatewayBinary, "admin", "token", "list")
		cmd.Dir = s.projectRoot

		output, err := cmd.Output()
		s.Require().NoError(err, "Token list command should succeed")

		outputStr := string(output)
		s.Contains(outputStr, "Active Tokens", "Should contain tokens header")
		s.Contains(outputStr, "ID", "Should contain ID column")
		s.Contains(outputStr, "User", "Should contain User column")
		s.Contains(outputStr, "Expires", "Should contain Expires column")
		s.Contains(outputStr, "Scopes", "Should contain Scopes column")
		s.Contains(outputStr, "Total:", "Should contain total count")
	})

	// Test token revoke
	s.Run("TokenRevoke", func() {
		ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
		defer cancel()

		cmd := s.safeExecCommand(ctx, s.gatewayBinary, "admin", "token", "revoke", "test-token-id")
		cmd.Dir = s.projectRoot

		output, err := cmd.Output()
		s.Require().NoError(err, "Token revoke command should succeed")

		outputStr := string(output)
		s.Contains(outputStr, "Revoking token test-token-id", "Should contain revoke message")
		s.Contains(outputStr, "✓ Token revoked successfully", "Should contain success message")
	})
}

// TestGatewayNamespaceCommands tests the gateway namespace commands.
func (s *CLIIntegrationTestSuite) TestGatewayNamespaceCommands() {
	s.T().Log("Testing gateway namespace commands")

	ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
	defer cancel()

	cmd := s.safeExecCommand(ctx, s.gatewayBinary, "admin", "namespace", "list")
	cmd.Dir = s.projectRoot

	output, err := cmd.Output()
	s.Require().NoError(err, "Namespace list command should succeed")

	outputStr := string(output)
	s.Contains(outputStr, "Discovered MCP Namespaces", "Should contain namespaces header")
	s.Contains(outputStr, "Namespace:", "Should contain namespace entries")
	s.Contains(outputStr, "Service:", "Should contain service info")
	s.Contains(outputStr, "Endpoints:", "Should contain endpoint count")
	s.Contains(outputStr, "Status:", "Should contain status")
	s.Contains(outputStr, "Tools:", "Should contain tools list")
}

// TestGatewayDebugCommands tests the gateway debug commands.
func (s *CLIIntegrationTestSuite) TestGatewayDebugCommands() {
	s.T().Log("Testing gateway debug commands")

	// Test debug health
	s.Run("DebugHealth", func() {
		ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
		defer cancel()

		cmd := s.safeExecCommand(ctx, s.gatewayBinary, "admin", "debug", "health")
		cmd.Dir = s.projectRoot

		output, err := cmd.Output()
		s.Require().NoError(err, "Debug health command should succeed")

		outputStr := string(output)
		s.Contains(outputStr, "Health Check Results", "Should contain health header")
		s.Contains(outputStr, "Gateway API", "Should contain gateway API check")
		s.Contains(outputStr, "Redis Connection", "Should contain Redis check")
		s.Contains(outputStr, "Service Discovery", "Should contain service discovery check")
		s.Contains(outputStr, "Endpoints", "Should contain endpoints check")
		s.Contains(outputStr, "Overall Status:", "Should contain overall status")
	})

	// Test debug metrics
	s.Run("DebugMetrics", func() {
		ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
		defer cancel()

		cmd := s.safeExecCommand(ctx, s.gatewayBinary, "admin", "debug", "metrics")
		cmd.Dir = s.projectRoot

		output, err := cmd.Output()
		s.Require().NoError(err, "Debug metrics command should succeed")

		outputStr := string(output)
		s.Contains(outputStr, "Current Metrics", "Should contain metrics header")
		s.Contains(outputStr, "mcp_gateway_connections_active", "Should contain connection metrics")
		s.Contains(outputStr, "mcp_gateway_requests_total", "Should contain request metrics")
		s.Contains(outputStr, "mcp_gateway_request_duration_p99", "Should contain latency metrics")
		s.Contains(outputStr, "mcp_gateway_errors_total", "Should contain error metrics")
	})
}

// TestRouterVersionCommand tests the router version command.
func (s *CLIIntegrationTestSuite) TestRouterVersionCommand() {
	s.T().Log("Testing router version command")

	ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
	defer cancel()

	cmd := s.safeExecCommand(ctx, s.routerBinary, "--version")
	cmd.Dir = s.projectRoot

	output, err := cmd.Output()
	s.Require().NoError(err, "Version command should succeed")

	outputStr := string(output)
	s.Contains(outputStr, "MCP Router", "Should contain router name")
	s.Contains(outputStr, "Version:", "Should contain version info")
	s.Contains(outputStr, "Build Time:", "Should contain build time")
	s.Contains(outputStr, "Git Commit:", "Should contain git commit")
}

// TestRouterVersionSubcommand tests the router version subcommand.
func (s *CLIIntegrationTestSuite) TestRouterVersionSubcommand() {
	s.T().Log("Testing router version subcommand")

	ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
	defer cancel()

	cmd := s.safeExecCommand(ctx, s.routerBinary, "version")
	cmd.Dir = s.projectRoot

	output, err := cmd.Output()
	s.Require().NoError(err, "Version subcommand should succeed")

	outputStr := string(output)
	// Router version subcommand has different output format
	s.NotEmpty(outputStr, "Should produce some output")
}

// TestRouterUpdateCheckCommand tests the router update check command.
func (s *CLIIntegrationTestSuite) TestRouterUpdateCheckCommand() {
	s.T().Log("Testing router update check command")

	ctx, cancel := context.WithTimeout(s.ctx, 15*time.Second) // Longer timeout for network call
	defer cancel()

	cmd := s.safeExecCommand(ctx, s.routerBinary, "update-check")
	cmd.Dir = s.projectRoot

	output, err := cmd.Output()
	// This may fail if no network access, so we'll check both success and failure cases
	outputStr := string(output)

	if err != nil {
		// Network might not be available in test environment
		s.T().Logf("Update check failed (expected in test environment): %v", err)
		s.T().Logf("Output: %s", outputStr)
	} else {
		s.T().Logf("Update check succeeded with output: %s", outputStr)
		s.NotEmpty(outputStr, "Should produce some output")
	}
}

// TestRouterSetupCommand tests the router setup command.
func (s *CLIIntegrationTestSuite) TestRouterSetupCommand() {
	s.T().Log("Testing router setup command")

	ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
	defer cancel()

	cmd := s.safeExecCommand(ctx, s.routerBinary, "setup", "--help")
	cmd.Dir = s.projectRoot

	output, err := cmd.Output()
	s.Require().NoError(err, "Setup help command should succeed")

	outputStr := string(output)
	s.Contains(outputStr, "setup", "Should contain setup command info")
	s.Contains(outputStr, "Usage:", "Should contain usage info")
}

// TestInvalidCommands tests that invalid commands produce appropriate errors.
func (s *CLIIntegrationTestSuite) TestInvalidCommands() {
	s.T().Log("Testing invalid commands")

	// Test invalid gateway command
	s.Run("InvalidGatewayCommand", func() {
		ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
		defer cancel()

		cmd := s.safeExecCommand(ctx, s.gatewayBinary, "invalid-command")
		cmd.Dir = s.projectRoot

		output, err := cmd.CombinedOutput()
		s.Require().Error(err, "Invalid command should fail")

		outputStr := string(output)
		s.Contains(strings.ToLower(outputStr), "error", "Should contain error message")
	})

	// Test invalid router command
	s.Run("InvalidRouterCommand", func() {
		ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
		defer cancel()

		cmd := s.safeExecCommand(ctx, s.routerBinary, "invalid-command")
		cmd.Dir = s.projectRoot

		output, err := cmd.CombinedOutput()
		s.Require().Error(err, "Invalid command should fail")

		outputStr := string(output)
		s.Contains(strings.ToLower(outputStr), "error", "Should contain error message")
	})
}

// TestCommandHelp tests help output for various commands.
func (s *CLIIntegrationTestSuite) TestCommandHelp() {
	s.T().Log("Testing command help")

	// Test gateway help
	s.Run("GatewayHelp", func() {
		ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
		defer cancel()

		cmd := s.safeExecCommand(ctx, s.gatewayBinary, "--help")
		cmd.Dir = s.projectRoot

		output, err := cmd.Output()
		s.Require().NoError(err, "Help command should succeed")

		outputStr := string(output)
		s.Contains(outputStr, "mcp-gateway", "Should contain command name")
		s.Contains(outputStr, "Usage:", "Should contain usage info")
		s.Contains(outputStr, "Flags:", "Should contain flags info")
		s.Contains(outputStr, "Available Commands:", "Should contain available commands")
	})

	// Test router help
	s.Run("RouterHelp", func() {
		ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
		defer cancel()

		cmd := s.safeExecCommand(ctx, s.routerBinary, "--help")
		cmd.Dir = s.projectRoot

		output, err := cmd.Output()
		s.Require().NoError(err, "Help command should succeed")

		outputStr := string(output)
		s.Contains(outputStr, "mcp-router", "Should contain command name")
		s.Contains(outputStr, "Usage:", "Should contain usage info")
		s.Contains(outputStr, "Flags:", "Should contain flags info")
		s.Contains(outputStr, "Available Commands:", "Should contain available commands")
	})
}

// Run the CLI integration test suite.
func TestCLIIntegrationSuite(t *testing.T) {
	// Cannot run in parallel: CLI tests build binaries and use filesystem operations
	// Skip if in short mode
	if testing.Short() {
		t.Skip("Skipping CLI integration tests in short mode")
	}

	suite.Run(t, new(CLIIntegrationTestSuite))
}
