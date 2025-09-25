package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// SecurityTestScenario represents a security test scenario.
type SecurityTestScenario struct {
	Name        string
	ConfigYAML  string
	EnvVars     map[string]string
	FileTokens  map[string]string
	ExpectError bool
	Validate    func(*Config) error
}

// SecurityTestRunner executes security test scenarios.
type SecurityTestRunner struct {
	t         *testing.T
	scenarios []SecurityTestScenario
}

// CreateSecurityTestRunner creates a new security test runner.
func CreateSecurityTestRunner(t *testing.T) *SecurityTestRunner {
	t.Helper()

	return &SecurityTestRunner{
		t:         t,
		scenarios: []SecurityTestScenario{},
	}
}

// AddScenario adds a test scenario.
func (r *SecurityTestRunner) AddScenario(scenario SecurityTestScenario) {
	r.scenarios = append(r.scenarios, scenario)
}

// ExecuteScenarios runs all test scenarios.
func (r *SecurityTestRunner) ExecuteScenarios() {
	for _, scenario := range r.scenarios {
		r.t.Run(scenario.Name, func(t *testing.T) {
			executor := &ScenarioExecutor{
				t:        t,
				scenario: scenario,
			}
			executor.Execute()
		})
	}
}

// ScenarioExecutor executes a single test scenario.
type ScenarioExecutor struct {
	t        *testing.T
	scenario SecurityTestScenario
	tempDir  string
}

// Execute runs the test scenario.
func (e *ScenarioExecutor) Execute() {
	e.setupEnvironment()
	defer e.cleanupEnvironment()

	config := e.loadConfiguration()
	e.validateConfiguration(config)
}

func (e *ScenarioExecutor) setupEnvironment() {
	// Create temp directory.
	tempDir, err := os.MkdirTemp("", "config-test-*")
	require.NoError(e.t, err)
	e.tempDir = tempDir

	// Set environment variables.
	for key, value := range e.scenario.EnvVars {
		e.t.Setenv(key, value)
	}

	// Create token files.
	e.createTokenFiles()
}

const (
	secureFilePermissions = 0600 // Owner read/write only
	expectedEndpointCount = 3    // Expected number of endpoints in multi-endpoint test
)

func (e *ScenarioExecutor) createTokenFiles() {
	for filename, content := range e.scenario.FileTokens {
		filePath := filepath.Join(e.tempDir, filename)
		err := os.WriteFile(filePath, []byte(content), secureFilePermissions)
		require.NoError(e.t, err)
	}
}

func (e *ScenarioExecutor) loadConfiguration() *Config {
	// Prepare config YAML with file paths.
	configYAML := e.prepareConfigYAML()

	// Write config file.
	configPath := filepath.Join(e.tempDir, "config.yaml")
	err := os.WriteFile(configPath, []byte(configYAML), secureFilePermissions)
	require.NoError(e.t, err)

	// Load config.
	config, err := Load(configPath)

	if e.scenario.ExpectError {
		assert.Error(e.t, err)

		return nil
	}

	require.NoError(e.t, err)
	require.NotNil(e.t, config)

	return config
}

func (e *ScenarioExecutor) prepareConfigYAML() string {
	configYAML := e.scenario.ConfigYAML

	// Replace file placeholders with actual paths.
	if len(e.scenario.FileTokens) > 0 {
		args := make([]interface{}, 0)
		for filename := range e.scenario.FileTokens {
			args = append(args, filepath.Join(e.tempDir, filename))
		}

		if len(args) > 0 {
			configYAML = fmt.Sprintf(configYAML, args...)
		}
	}

	return configYAML
}

func (e *ScenarioExecutor) validateConfiguration(config *Config) {
	if config == nil || e.scenario.Validate == nil {
		return
	}

	err := e.scenario.Validate(config)
	assert.NoError(e.t, err)
}

func (e *ScenarioExecutor) cleanupEnvironment() {
	if e.tempDir != "" {
		_ = os.RemoveAll(e.tempDir)
	}
}

// BuildMultiEndpointScenario creates a multi-endpoint security scenario.
func BuildMultiEndpointScenario() SecurityTestScenario {
	return SecurityTestScenario{
		Name: "Multi-endpoint with different auth types",
		ConfigYAML: `
version: 1
gateway_pool:
  endpoints:
    - url: wss://bearer-endpoint.com
      auth:
        type: bearer
        token_env: BEARER_TOKEN
      tags: ["bearer"]
    - url: wss://oauth2-endpoint.com
      auth:
        type: oauth2
        client_id: oauth-client
        client_secret_env: OAUTH_SECRET
        token_endpoint: https://auth.example.com/token
        scopes: ["read", "write"]
      tags: ["oauth2"]
    - url: wss://mtls-endpoint.com
      auth:
        type: mtls
        client_cert: %s
        client_key: %s
      tags: ["mtls"]
`,
		EnvVars: map[string]string{
			"BEARER_TOKEN": "bearer-token-123",
			"OAUTH_SECRET": "oauth-secret-456",
		},
		FileTokens: map[string]string{
			"client.crt": "-----BEGIN CERTIFICATE-----\nMOCK_CERT_DATA\n-----END CERTIFICATE-----",
			"client.key": "-----BEGIN PRIVATE KEY-----\nMOCK_KEY_DATA\n-----END PRIVATE KEY-----",
		},
		Validate: validateMultiEndpoint,
	}
}

func validateMultiEndpoint(c *Config) error {
	endpoints := c.GetGatewayEndpoints()
	if len(endpoints) != expectedEndpointCount {
		return fmt.Errorf("expected %d endpoints, got %d", expectedEndpointCount, len(endpoints))
	}

	// Verify bearer endpoint.
	if endpoints[0].Auth.Type != "bearer" || endpoints[0].Auth.Token != "bearer-token-123" {
		return errors.New("bearer endpoint not configured correctly")
	}

	// Verify OAuth2 endpoint.
	if endpoints[1].Auth.Type != "oauth2" || endpoints[1].Auth.ClientSecret != "oauth-secret-456" {
		return errors.New("oauth2 endpoint not configured correctly")
	}

	// Verify mTLS endpoint.
	if endpoints[2].Auth.Type != "mtls" || endpoints[2].Auth.ClientCert == "" {
		return errors.New("mtls endpoint not configured correctly")
	}

	return nil
}

// BuildSecureStorageScenario creates a secure token storage scenario.
func BuildSecureStorageScenario() SecurityTestScenario {
	return SecurityTestScenario{
		Name: "Secure token storage integration",
		ConfigYAML: `
version: 1
gateway_pool:
  endpoints:
    - url: wss://secure-storage.com
      auth:
        type: bearer
        token_secure_key: secure-app-token-v1
        token_file: %s
      tags: ["secure"]
`,
		FileTokens: map[string]string{
			"secure-token.txt": "file-stored-secure-token-789",
		},
		Validate: validateSecureStorage,
	}
}

func validateSecureStorage(c *Config) error {
	endpoints := c.GetGatewayEndpoints()
	if len(endpoints) != 1 {
		return fmt.Errorf("expected 1 endpoint, got %d", len(endpoints))
	}

	if endpoints[0].Auth.Token != "file-stored-secure-token-789" {
		return fmt.Errorf("secure token not loaded correctly: got %s", endpoints[0].Auth.Token)
	}

	return nil
}

// BuildOAuth2PasswordScenario creates an OAuth2 password grant scenario.
func BuildOAuth2PasswordScenario() SecurityTestScenario {
	return SecurityTestScenario{
		Name: "OAuth2 password grant with secure storage",
		ConfigYAML: `
version: 1
gateway_pool:
  endpoints:
    - url: wss://oauth2-password.com
      auth:
        type: oauth2
        grant_type: password
        client_id: password-client
        client_secret_env: PASSWORD_CLIENT_SECRET
        username: testuser
        password_env: PASSWORD_USER_PASSWORD
        token_endpoint: https://auth.example.com/token
        scopes: ["profile", "email"]
`,
		EnvVars: map[string]string{
			"PASSWORD_CLIENT_SECRET": "password-client-secret",
			"PASSWORD_USER_PASSWORD": "user-password-123",
		},
		Validate: validateOAuth2Password,
	}
}

func validateOAuth2Password(c *Config) error {
	endpoints := c.GetGatewayEndpoints()
	if len(endpoints) != 1 {
		return fmt.Errorf("expected 1 endpoint, got %d", len(endpoints))
	}

	auth := endpoints[0].Auth
	if auth.GrantType != "password" {
		return fmt.Errorf("expected password grant type, got %s", auth.GrantType)
	}

	if auth.ClientSecret != "password-client-secret" {
		return errors.New("client secret not loaded correctly")
	}

	if auth.Password != "user-password-123" {
		return errors.New("password not loaded correctly")
	}

	return nil
}
