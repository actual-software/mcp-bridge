//go:build linux
// +build linux

package secure

import (
	"fmt"
	"os/exec"
	"strings"
)

// secretServiceStore implements TokenStore using Linux Secret Service (via secret-tool)
type secretServiceStore struct {
	collection string
}

func newSecretServiceStore(appName string) (TokenStore, error) {
	// Check if secret-tool is available
	if _, err := exec.LookPath("secret-tool"); err != nil {
		return nil, fmt.Errorf("secret-tool not found: %w", err)
	}

	return &secretServiceStore{
		collection: fmt.Sprintf("%s-mcp-router", appName),
	}, nil
}

func (s *secretServiceStore) Store(key string, token string) error {
	// Store secret using secret-tool
	cmd := exec.Command("secret-tool", "store",
		"--label", fmt.Sprintf("%s Token", key),
		"application", s.collection,
		"key", key)

	// Pass token via stdin
	cmd.Stdin = strings.NewReader(token)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to store token: %w, output: %s", err, output)
	}

	return nil
}

func (s *secretServiceStore) Retrieve(key string) (string, error) {
	cmd := exec.Command("secret-tool", "lookup",
		"application", s.collection,
		"key", key)

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to retrieve token: %w", err)
	}

	// Remove trailing newline
	token := strings.TrimSpace(string(output))
	if token == "" {
		return "", fmt.Errorf("token not found: %s", key)
	}

	return token, nil
}

func (s *secretServiceStore) Delete(key string) error {
	cmd := exec.Command("secret-tool", "clear",
		"application", s.collection,
		"key", key)

	output, err := cmd.CombinedOutput()
	if err != nil {
		// It's okay if the secret doesn't exist
		if strings.Contains(string(output), "No matching") {
			return nil
		}
		return fmt.Errorf("failed to delete token: %w, output: %s", err, output)
	}

	return nil
}

func (s *secretServiceStore) List() ([]string, error) {
	// Use secret-tool search to find all keys for our application
	cmd := exec.Command("secret-tool", "search",
		"--all",
		"application", s.collection)

	output, err := cmd.Output()
	if err != nil {
		// No secrets found is not an error
		if strings.Contains(err.Error(), "No matching") {
			return []string{}, nil
		}
		return nil, fmt.Errorf("failed to list tokens: %w", err)
	}

	// Parse the output to extract keys
	var keys []string
	lines := strings.Split(string(output), "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "attribute.key = ") {
			key := strings.TrimPrefix(line, "attribute.key = ")
			keys = append(keys, key)
		}
	}

	return keys, nil
}
