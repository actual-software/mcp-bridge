//go:build linux

package secure

import (
	"fmt"
	"os/exec"
	"strings"
)

// secretServiceStore implements TokenStore using Linux Secret Service (libsecret/secret-tool)
type secretServiceStore struct {
	appName string
}

// newSecretServiceStore creates a new Linux Secret Service-based token store
func newSecretServiceStore(appName string) (TokenStore, error) {
	// Check if secret-tool is available.
	if _, err := exec.LookPath("secret-tool"); err != nil {
		return nil, fmt.Errorf("secret-tool command not found, secret service unavailable")
	}

	return &secretServiceStore{
		appName: appName,
	}, nil
}

// Store saves a token to the Linux Secret Service.
func (s *secretServiceStore) Store(key, token string) error {
	serviceName := s.serviceName()

	cmd := exec.Command("secret-tool", "store",
		"--label", fmt.Sprintf("MCP Router Token (%s)", key),
		"service", serviceName,
		"account", key)

	cmd.Stdin = strings.NewReader(token)

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to store token in secret service: %w", err)
	}

	return nil
}

// Retrieve gets a token from the Linux Secret Service.
func (s *secretServiceStore) Retrieve(key string) (string, error) {
	serviceName := s.serviceName()

	cmd := exec.Command("secret-tool", "lookup",
		"service", serviceName,
		"account", key)

	output, err := cmd.Output()
	if err != nil {
		if cmd.ProcessState.ExitCode() == 1 {
			return "", ErrTokenNotFound
		}
		return "", fmt.Errorf("failed to retrieve token from secret service: %w", err)
	}

	// Remove trailing newline.
	token := strings.TrimSpace(string(output))
	return token, nil
}

// Delete removes a token from the Linux Secret Service.
func (s *secretServiceStore) Delete(key string) error {
	serviceName := s.serviceName()

	cmd := exec.Command("secret-tool", "clear",
		"service", serviceName,
		"account", key)

	if err := cmd.Run(); err != nil {
		// Don't treat "item not found" as an error (exit code 1).
		if cmd.ProcessState.ExitCode() == 1 {
			return nil
		}
		return fmt.Errorf("failed to delete token from secret service: %w", err)
	}

	return nil
}

// List returns all stored token keys.
func (s *secretServiceStore) List() ([]string, error) {
	serviceName := s.serviceName()

	cmd := exec.Command("secret-tool", "search",
		"service", serviceName)

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list tokens from secret service: %w", err)
	}

	// Parse the output to extract account names.
	lines := strings.Split(string(output), "\n")
	var keys []string

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "account = ") {
			account := strings.TrimPrefix(line, "account = ")
			keys = append(keys, account)
		}
	}

	return keys, nil
}

// serviceName returns the secret service name for this app
func (s *secretServiceStore) serviceName() string {
	return fmt.Sprintf("mcp-router-%s", s.appName)
}
