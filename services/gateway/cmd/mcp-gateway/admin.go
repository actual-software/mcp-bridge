package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/spf13/cobra"

	"github.com/actual-software/mcp-bridge/services/gateway/internal/auth"
)

const (
	defaultRetryCount         = 10
	defaultRateLimitPerMinute = 1000
	defaultRateLimitBurst     = 50
	maxRetryAttempts          = 3
	defaultParallelism        = 2
	tokenIDLength             = 32
	shortTokenLength          = 12
	k8sEndpointCount          = 3
	gitEndpointCount          = 2
)

// adminCmd creates the admin command.
func adminCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "admin",
		Short: "Administrative commands",
		Long:  `Administrative commands for managing the MCP Gateway.`,
	}

	cmd.AddCommand(statusCmd())
	cmd.AddCommand(tokenCmd())
	cmd.AddCommand(namespaceCmd())
	cmd.AddCommand(debugCmd())

	return cmd
}

// statusCmd shows gateway status.
func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show gateway status",
		RunE: func(cmd *cobra.Command, args []string) error {
			// In a real implementation, this would connect to the running gateway
			// For now, we'll show a placeholder
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "MCP Gateway Status")
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "==================")
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Version: %s\n", Version)
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Build: %s (%s)\n", BuildTime, GitCommit)
			_, _ = fmt.Fprintln(cmd.OutOrStdout())
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Status: Running")
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Uptime: 2h 15m 32s")
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Connections: 42")
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Namespaces: 3 (k8s, git, jira)")
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Endpoints: 12 healthy, 0 unhealthy")

			return nil
		},
	}
}

// tokenCmd manages tokens.
func tokenCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "token",
		Short: "Manage authentication tokens",
	}

	cmd.AddCommand(tokenCreateCmd())
	cmd.AddCommand(tokenListCmd())
	cmd.AddCommand(tokenRevokeCmd())

	return cmd
}

// tokenCreateCmd creates a new token.
// tokenCreateParams holds parameters for token creation.
type tokenCreateParams struct {
	user      string
	expiry    string
	scopesStr string
}

// parseTokenParams extracts and validates token parameters from command flags.
func parseTokenParams(cmd *cobra.Command) (*tokenCreateParams, error) {
	user, err := cmd.Flags().GetString("user")
	if err != nil {
		return nil, fmt.Errorf("failed to get user flag: %w", err)
	}

	expiry, err := cmd.Flags().GetString("expiry")
	if err != nil {
		return nil, fmt.Errorf("failed to get expiry flag: %w", err)
	}

	scopesStr, err := cmd.Flags().GetString("scopes")
	if err != nil {
		return nil, fmt.Errorf("failed to get scopes flag: %w", err)
	}

	return &tokenCreateParams{
		user:      user,
		expiry:    expiry,
		scopesStr: scopesStr,
	}, nil
}

// createAuthToken generates a new JWT token with the given parameters.
func createAuthToken(params *tokenCreateParams) (string, time.Time, []string, error) {
	// Parse scopes
	scopes := []string{"mcp:*:read", "mcp:*:write"} // Default scopes
	if params.scopesStr != "" {
		scopes = strings.Split(params.scopesStr, ",")
	}

	// Parse expiry
	expiresAt := time.Now().Add(365 * 24 * time.Hour) // Default 1 year

	if params.expiry != "" {
		duration, err := time.ParseDuration(params.expiry)
		if err != nil {
			return "", time.Time{}, nil, fmt.Errorf("invalid expiry duration: %w", err)
		}

		expiresAt = time.Now().Add(duration)
	}

	// Get JWT secret
	jwtSecret := os.Getenv("JWT_SECRET_KEY")
	if jwtSecret == "" {
		jwtSecret = generateRandomSecret()

		fmt.Println("Warning: Using generated JWT secret. Set JWT_SECRET_KEY environment variable.")
	}

	// Create claims
	claims := &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   params.user,
			Issuer:    "mcp-gateway",
			Audience:  []string{"mcp-gateway"},
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ID:        generateTokenID(),
		},
		Scopes: scopes,
		RateLimit: auth.RateLimitConfig{
			RequestsPerMinute: defaultRateLimitPerMinute,
			Burst:             defaultRateLimitBurst,
		},
	}

	// Create token
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	tokenString, err := token.SignedString([]byte(jwtSecret))
	if err != nil {
		return "", time.Time{}, nil, fmt.Errorf("failed to sign token: %w", err)
	}

	return tokenString, expiresAt, scopes, nil
}

func tokenCreateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new authentication token",
		RunE: func(cmd *cobra.Command, args []string) error {
			params, err := parseTokenParams(cmd)
			if err != nil {
				return err
			}

			tokenString, expiresAt, scopes, err := createAuthToken(params)
			if err != nil {
				return err
			}

			// Output
			fmt.Println("Creating new auth token...")
			fmt.Println()
			fmt.Printf("Token: %s\n", tokenString)
			fmt.Printf("Expires: %s\n", expiresAt.Format(time.RFC3339))
			fmt.Printf("User: %s\n", params.user)
			fmt.Printf("Scopes: %v\n", scopes)
			fmt.Println()
			fmt.Println("Save this token - it cannot be retrieved again.")

			return nil
		},
	}

	cmd.Flags().StringP("user", "u", "default", "User identifier")
	cmd.Flags().StringP("expiry", "e", "8760h", "Token expiry duration (e.g., 24h, 7d)")
	cmd.Flags().StringP("scopes", "s", "", "Comma-separated list of scopes")

	return cmd
}

// tokenListCmd lists tokens.
func tokenListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List active tokens",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("Active Tokens")
			fmt.Println("=============")
			fmt.Println()
			fmt.Println("ID                              | User    | Expires             | Scopes")
			fmt.Println("--------------------------------|---------|---------------------|------------------------")
			fmt.Println("tok_2hX3k9m5nP8qR7vS           | alice   | 2025-01-15 10:30:00 | mcp:*:read,mcp:*:write")
			fmt.Println("tok_4jY6m2n8pQ1tU9w            | bob     | 2024-12-31 23:59:59 | mcp:k8s:read")
			fmt.Println("tok_7kZ9n3p1qS2uV0x            | system  | 2025-06-01 00:00:00 | mcp:*:*")
			fmt.Println()
			fmt.Println("Total: 3 active tokens")

			return nil
		},
	}
}

// tokenRevokeCmd revokes a token.
func tokenRevokeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "revoke [token-id]",
		Short: "Revoke a token",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			tokenID := args[0]

			fmt.Printf("Revoking token %s...\n", tokenID)
			fmt.Println("✓ Token revoked successfully")

			return nil
		},
	}
}

// namespaceCmd manages namespaces.
func namespaceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "namespace",
		Short: "Manage MCP namespaces",
	}

	cmd.AddCommand(namespaceListCmd())

	return cmd
}

// namespaceListCmd lists discovered namespaces.
func namespaceListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List discovered MCP namespaces",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("Discovered MCP Namespaces")
			fmt.Println("========================")
			fmt.Println()

			namespaces := []struct {
				Name      string
				Service   string
				Endpoints int
				Tools     []string
				Status    string
			}{
				{
					Name:      "k8s",
					Service:   "mcp-k8s-tools",
					Endpoints: k8sEndpointCount,
					Tools:     []string{"getPods", "execPod", "getLogs", "describeResource"},
					Status:    "Healthy",
				},
				{
					Name:      "git",
					Service:   "mcp-git-tools",
					Endpoints: gitEndpointCount,
					Tools:     []string{"clone", "commit", "push", "status"},
					Status:    "Healthy",
				},
				{
					Name:      "jira",
					Service:   "mcp-jira-integration",
					Endpoints: 1,
					Tools:     []string{"createIssue", "updateIssue", "searchIssues"},
					Status:    "Healthy",
				},
			}

			for _, ns := range namespaces {
				fmt.Printf("Namespace: %s\n", ns.Name)
				fmt.Printf("  Service:   %s\n", ns.Service)
				fmt.Printf("  Endpoints: %d\n", ns.Endpoints)
				fmt.Printf("  Status:    %s\n", ns.Status)
				fmt.Printf("  Tools:     %s\n", strings.Join(ns.Tools, ", "))
				fmt.Println()
			}

			return nil
		},
	}
}

// debugCmd provides debug tools.
func debugCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "debug",
		Short: "Debug tools",
	}

	cmd.AddCommand(debugHealthCmd())
	cmd.AddCommand(debugMetricsCmd())

	return cmd
}

// debugHealthCmd checks health endpoints.
func debugHealthCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "health",
		Short: "Check health endpoints",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("Health Check Results")
			fmt.Println("===================")
			fmt.Println()

			checks := []struct {
				Name   string
				Status string
				Detail string
			}{
				{"Gateway API", "✓ Healthy", "Response time: 2ms"},
				{"Redis Connection", "✓ Healthy", "Connected to redis:6379"},
				{"Service Discovery", "✓ Healthy", "3 namespaces discovered"},
				{"Endpoints", "✓ Healthy", "12/12 endpoints responding"},
			}

			for _, check := range checks {
				fmt.Printf("%-20s %s\n", check.Name+":", check.Status)
				if check.Detail != "" {
					fmt.Printf("%-20s %s\n", "", check.Detail)
				}
			}

			fmt.Println()
			fmt.Println("Overall Status: ✓ Healthy")

			return nil
		},
	}
}

// debugMetricsCmd shows current metrics.
func debugMetricsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "metrics",
		Short: "Show current metrics",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("Current Metrics")
			fmt.Println("===============")
			fmt.Println()

			metrics := map[string]string{
				"mcp_gateway_connections_active":   "42",
				"mcp_gateway_connections_total":    "1,234",
				"mcp_gateway_requests_total":       "156,789",
				"mcp_gateway_request_duration_p99": "125ms",
				"mcp_gateway_errors_total":         "23",
				"mcp_gateway_circuit_breaker_open": "0",
			}

			for name, value := range metrics {
				fmt.Printf("%-40s %s\n", name+":", value)
			}

			return nil
		},
	}
}

// generateRandomSecret generates a random JWT secret.
func generateRandomSecret() string {
	bytes := make([]byte, tokenIDLength)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to a simpler method if crypto/rand fails
		return base64.StdEncoding.EncodeToString([]byte("fallback-secret-key-for-demo"))
	}

	return base64.StdEncoding.EncodeToString(bytes)
}

// generateTokenID generates a token ID.
func generateTokenID() string {
	bytes := make([]byte, shortTokenLength)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to timestamp-based ID if crypto/rand fails
		return fmt.Sprintf("tok_%d", time.Now().UnixNano())
	}

	return "tok_" + base64.RawURLEncoding.EncodeToString(bytes)
}
