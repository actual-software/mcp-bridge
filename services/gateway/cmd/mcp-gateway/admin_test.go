package main

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdminCmd(t *testing.T) {
	t.Run("admin_command_structure", func(t *testing.T) {
		cmd := adminCmd()

		assert.Equal(t, "admin", cmd.Use)
		assert.Equal(t, "Administrative commands", cmd.Short)
		assert.NotEmpty(t, cmd.Long)

		// Check subcommands
		subcommands := cmd.Commands()
		assert.Len(t, subcommands, 4)

		cmdNames := make([]string, len(subcommands))
		for i, subcmd := range subcommands {
			cmdNames[i] = subcmd.Use
		}

		assert.Contains(t, cmdNames, "status")
		assert.Contains(t, cmdNames, "token")
		assert.Contains(t, cmdNames, "namespace")
		assert.Contains(t, cmdNames, "debug")
	})
}

func TestStatusCmd(t *testing.T) {
	t.Run("status_command_output", func(t *testing.T) {
		cmd := statusCmd()

		assert.Equal(t, "status", cmd.Use)
		assert.Equal(t, "Show gateway status", cmd.Short)

		// Execute command and verify it doesn't error
		// Since we can't easily capture the output (commands use fmt.Println directly),
		// we just verify the command structure and that it executes without error
		// The actual output would be tested in integration tests
		err := cmd.RunE(cmd, []string{})
		require.NoError(t, err)
	})
}

func TestTokenCmd(t *testing.T) {
	t.Run("token_command_structure", func(t *testing.T) {
		cmd := tokenCmd()

		assert.Equal(t, "token", cmd.Use)
		assert.Equal(t, "Manage authentication tokens", cmd.Short)

		// Check subcommands
		subcommands := cmd.Commands()
		assert.Len(t, subcommands, 3)

		cmdNames := make([]string, len(subcommands))
		for i, subcmd := range subcommands {
			cmdNames[i] = subcmd.Use
		}

		assert.Contains(t, cmdNames, "create")
		assert.Contains(t, cmdNames, "list")
		assert.Contains(t, cmdNames, "revoke [token-id]")
	})
}

func TestTokenCreateCmd(t *testing.T) {
	t.Run("token_create_default_flags", testTokenCreateDefaultFlags)
	t.Run("token_create_with_default_values", testTokenCreateWithDefaultValues)
	t.Run("token_create_with_custom_flags", testTokenCreateWithCustomFlags)
	t.Run("token_create_with_invalid_expiry", testTokenCreateWithInvalidExpiry)
	t.Run("token_create_without_jwt_secret", testTokenCreateWithoutJWTSecret)
}

func testTokenCreateDefaultFlags(t *testing.T) {
	t.Helper()

	cmd := tokenCreateCmd()

	assert.Equal(t, "create", cmd.Use)
	assert.Equal(t, "Create a new authentication token", cmd.Short)

	// Check flags
	userFlag := cmd.Flags().Lookup("user")
	assert.NotNil(t, userFlag)
	assert.Equal(t, "default", userFlag.DefValue)

	expiryFlag := cmd.Flags().Lookup("expiry")
	assert.NotNil(t, expiryFlag)
	assert.Equal(t, "8760h", expiryFlag.DefValue)

	scopesFlag := cmd.Flags().Lookup("scopes")
	assert.NotNil(t, scopesFlag)
	assert.Empty(t, scopesFlag.DefValue)
}

func testTokenCreateWithDefaultValues(t *testing.T) {
	t.Helper()

	cmd := tokenCreateCmd()

	// Set environment variable for consistent testing
	originalSecret := os.Getenv("JWT_SECRET_KEY")

	_ = os.Setenv("JWT_SECRET_KEY", "test-secret-key")

	defer func() { _ = os.Setenv("JWT_SECRET_KEY", originalSecret) }()

	// Execute with default values
	// Verify that the command executed without error
	// The token creation logic is tested separately
	err := cmd.RunE(cmd, []string{})
	require.NoError(t, err)
}

func testTokenCreateWithCustomFlags(t *testing.T) {
	t.Helper()

	cmd := tokenCreateCmd()

	// Set flags
	err := cmd.Flags().Set("user", "testuser")
	require.NoError(t, err)
	err = cmd.Flags().Set("expiry", "24h")
	require.NoError(t, err)
	err = cmd.Flags().Set("scopes", "mcp:k8s:read,mcp:git:write")
	require.NoError(t, err)

	// Set environment variable
	originalSecret := os.Getenv("JWT_SECRET_KEY")

	_ = os.Setenv("JWT_SECRET_KEY", "test-secret-key")

	defer func() { _ = os.Setenv("JWT_SECRET_KEY", originalSecret) }()

	// Execute
	// Verify that the command executed without error with custom flags
	err = cmd.RunE(cmd, []string{})
	require.NoError(t, err)
}

func testTokenCreateWithInvalidExpiry(t *testing.T) {
	t.Helper()

	cmd := tokenCreateCmd()

	// Set invalid expiry
	err := cmd.Flags().Set("expiry", "invalid-duration")
	require.NoError(t, err)

	// Execute
	err = cmd.RunE(cmd, []string{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid expiry duration")
}

func testTokenCreateWithoutJWTSecret(t *testing.T) {
	t.Helper()

	cmd := tokenCreateCmd()

	// Clear JWT secret
	originalSecret := os.Getenv("JWT_SECRET_KEY")
	_ = os.Unsetenv("JWT_SECRET_KEY")

	defer func() {
		if originalSecret != "" {
			_ = os.Setenv("JWT_SECRET_KEY", originalSecret)
		}
	}()

	// Execute
	// Verify that the command executed without error
	// The warning message goes to stdout, which we can't easily capture in this test
	err := cmd.RunE(cmd, []string{})
	require.NoError(t, err)
}

func TestTokenListCmd(t *testing.T) {
	t.Run("token_list_execution", func(t *testing.T) {
		cmd := tokenListCmd()

		assert.Equal(t, "list", cmd.Use)
		assert.Equal(t, "List active tokens", cmd.Short)

		// Execute
		// Verify command executed without error
		err := cmd.RunE(cmd, []string{})
		require.NoError(t, err)
	})
}

func TestTokenRevokeCmd(t *testing.T) {
	t.Run("token_revoke_success", func(t *testing.T) {
		cmd := tokenRevokeCmd()

		assert.Equal(t, "revoke [token-id]", cmd.Use)
		assert.Equal(t, "Revoke a token", cmd.Short)

		// Execute with token ID
		// Verify command executed without error
		err := cmd.RunE(cmd, []string{"tok_test123"})
		require.NoError(t, err)
	})

	t.Run("token_revoke_args_validation", func(t *testing.T) {
		cmd := tokenRevokeCmd()

		// Test that Args validation function exists and works properly
		// We can't compare function pointers directly, so test the behavior
		assert.NotNil(t, cmd.Args, "Args validation should be set")

		// Test with correct number of args (should pass)
		err := cmd.Args(cmd, []string{"tok_test123"})
		require.NoError(t, err, "Should accept exactly one argument")

		// Test with wrong number of args (should fail)
		err = cmd.Args(cmd, []string{})
		require.Error(t, err, "Should reject zero arguments")

		err = cmd.Args(cmd, []string{"tok_1", "tok_2"})
		assert.Error(t, err, "Should reject two arguments")
	})
}

func TestNamespaceCmd(t *testing.T) {
	t.Run("namespace_command_structure", func(t *testing.T) {
		cmd := namespaceCmd()

		assert.Equal(t, "namespace", cmd.Use)
		assert.Equal(t, "Manage MCP namespaces", cmd.Short)

		// Check subcommands
		subcommands := cmd.Commands()
		assert.Len(t, subcommands, 1)
		assert.Equal(t, "list", subcommands[0].Use)
	})
}

func TestNamespaceListCmd(t *testing.T) {
	t.Run("namespace_list_execution", func(t *testing.T) {
		cmd := namespaceListCmd()

		assert.Equal(t, "list", cmd.Use)
		assert.Equal(t, "List discovered MCP namespaces", cmd.Short)

		// Execute
		// Verify command executed without error
		err := cmd.RunE(cmd, []string{})
		require.NoError(t, err)
	})
}

func TestDebugCmd(t *testing.T) {
	t.Run("debug_command_structure", func(t *testing.T) {
		cmd := debugCmd()

		assert.Equal(t, "debug", cmd.Use)
		assert.Equal(t, "Debug tools", cmd.Short)

		// Check subcommands
		subcommands := cmd.Commands()
		assert.Len(t, subcommands, 2)

		cmdNames := make([]string, len(subcommands))
		for i, subcmd := range subcommands {
			cmdNames[i] = subcmd.Use
		}

		assert.Contains(t, cmdNames, "health")
		assert.Contains(t, cmdNames, "metrics")
	})
}

func TestDebugHealthCmd(t *testing.T) {
	t.Run("debug_health_execution", func(t *testing.T) {
		cmd := debugHealthCmd()

		assert.Equal(t, "health", cmd.Use)
		assert.Equal(t, "Check health endpoints", cmd.Short)

		// Execute
		// Verify command executed without error
		err := cmd.RunE(cmd, []string{})
		require.NoError(t, err)
	})
}

func TestDebugMetricsCmd(t *testing.T) {
	t.Run("debug_metrics_execution", func(t *testing.T) {
		cmd := debugMetricsCmd()

		assert.Equal(t, "metrics", cmd.Use)
		assert.Equal(t, "Show current metrics", cmd.Short)

		// Execute
		// Verify command executed without error
		err := cmd.RunE(cmd, []string{})
		require.NoError(t, err)
	})
}

func TestGenerateRandomSecret(t *testing.T) {
	t.Run("generate_random_secret_success", func(t *testing.T) {
		secret := generateRandomSecret()

		assert.NotEmpty(t, secret)
		assert.Greater(t, len(secret), 20) // Base64 encoded 32 bytes should be longer

		// Generate another and ensure they're different
		secret2 := generateRandomSecret()
		assert.NotEqual(t, secret, secret2)
	})

	t.Run("generate_random_secret_properties", func(t *testing.T) {
		secret := generateRandomSecret()

		// Should be base64 encoded
		assert.NotContains(t, secret, " ")
		assert.NotContains(t, secret, "\n")
		assert.NotContains(t, secret, "\t")

		// Should be reasonable length for base64 encoded 32 bytes
		assert.Greater(t, len(secret), 40)
	})
}

func TestGenerateTokenID(t *testing.T) {
	t.Run("generate_token_id_success", func(t *testing.T) {
		tokenID := generateTokenID()

		assert.NotEmpty(t, tokenID)
		assert.True(t, strings.HasPrefix(tokenID, "tok_"))
		assert.Greater(t, len(tokenID), 10)

		// Generate another and ensure they're different
		tokenID2 := generateTokenID()
		assert.NotEqual(t, tokenID, tokenID2)
	})

	t.Run("generate_token_id_format", func(t *testing.T) {
		tokenID := generateTokenID()

		// Should have correct format
		assert.True(t, strings.HasPrefix(tokenID, "tok_"))
		assert.NotContains(t, tokenID, " ")
		assert.NotContains(t, tokenID, "\n")

		// Should be URL-safe base64 (no padding, no special chars)
		suffix := strings.TrimPrefix(tokenID, "tok_")
		assert.NotContains(t, suffix, "+")
		assert.NotContains(t, suffix, "/")
		assert.NotContains(t, suffix, "=")
	})
}

func TestUtilityFunctions(t *testing.T) {
	t.Run("version_variables_integration", testVersionVariablesIntegration)
	t.Run("token_creation_edge_cases", testTokenCreationEdgeCases)
	t.Run("command_help_text", testCommandHelpText)
}

func testVersionVariablesIntegration(t *testing.T) {
	t.Helper()

	// Test that version variables work in context
	originalVersion := Version
	originalBuildTime := BuildTime
	originalGitCommit := GitCommit

	Version = "v2.0.0-test"
	BuildTime = "2025-01-01T00:00:00Z"
	GitCommit = "abcd1234"

	defer func() {
		Version = originalVersion
		BuildTime = originalBuildTime
		GitCommit = originalGitCommit
	}()

	// Test status command with custom version
	cmd := statusCmd()

	var buf bytes.Buffer

	cmd.SetOut(&buf)

	err := cmd.RunE(cmd, []string{})
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "v2.0.0-test")
	assert.Contains(t, output, "2025-01-01T00:00:00Z")
	assert.Contains(t, output, "abcd1234")
}

func testTokenCreationEdgeCases(t *testing.T) {
	t.Helper()

	cmd := tokenCreateCmd()

	// Test with empty scopes
	err := cmd.Flags().Set("scopes", "")
	require.NoError(t, err)

	// Set environment variable
	originalSecret := os.Getenv("JWT_SECRET_KEY")

	_ = os.Setenv("JWT_SECRET_KEY", "test-secret")

	defer func() { _ = os.Setenv("JWT_SECRET_KEY", originalSecret) }()

	// Verify command executed without error with empty scopes
	err = cmd.RunE(cmd, []string{})
	require.NoError(t, err)
}

func testCommandHelpText(t *testing.T) {
	t.Helper()

	// Test that all commands have proper help text
	commands := []*cobra.Command{
		adminCmd(),
		statusCmd(),
		tokenCmd(),
		tokenCreateCmd(),
		tokenListCmd(),
		tokenRevokeCmd(),
		namespaceCmd(),
		namespaceListCmd(),
		debugCmd(),
		debugHealthCmd(),
		debugMetricsCmd(),
	}

	for _, cmd := range commands {
		assert.NotEmpty(t, cmd.Use, "Command should have Use field")
		assert.NotEmpty(t, cmd.Short, "Command should have Short description")
	}
}
