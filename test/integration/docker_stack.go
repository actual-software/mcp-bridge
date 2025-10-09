// Package integration provides Docker-based integration testing utilities for MCP services.
//
// Integration utilities allow flexible style
//

package integration

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
)

const (
	// Network timeouts.
	httpClientTimeout        = 5 * time.Second
	healthCheckRetryInterval = 2 * time.Second

	// HTTP status codes.
	httpStatusServerError = 500
)

// DockerStack manages the Docker Compose services for integration testing.
type DockerStack struct {
	t           *testing.T
	logger      *zap.Logger
	composeDir  string
	composeFile string
	composeCmd  []string // docker compose command (either "docker compose" or "docker-compose")
	services    map[string]string
	cleanup     []func()
}

// NewDockerStack creates a new Docker stack manager for integration tests.
// getDockerComposeCommand returns the appropriate docker-compose command.
// Tries "docker compose" (v2) first, then falls back to "docker-compose" (v1).
func getDockerComposeCommand() ([]string, error) {
	// Try docker compose (v2) first
	cmd := exec.CommandContext(context.Background(), "docker", "compose", "version")
	if err := cmd.Run(); err == nil {
		return []string{"docker", "compose"}, nil
	}

	// Fall back to docker-compose (v1)
	cmd = exec.CommandContext(context.Background(), "docker-compose", "version")
	if err := cmd.Run(); err == nil {
		return []string{"docker-compose"}, nil
	}

	return nil, fmt.Errorf("neither 'docker compose' nor 'docker-compose' found in PATH")
}

func NewDockerStack(t *testing.T) *DockerStack {
	t.Helper()

	logger, err := zap.NewDevelopment()
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}

	composeCmd, err := getDockerComposeCommand()
	if err != nil {
		t.Fatalf("Failed to find docker compose command: %v", err)
	}

	return &DockerStack{
		t:           t,
		logger:      logger,
		composeDir:  ".",
		composeFile: "docker-compose.test.yml",
		composeCmd:  composeCmd,
		services:    make(map[string]string),
		cleanup:     make([]func(), 0),
	}
}

// SetComposeFile sets the Docker Compose file to use.
func (ds *DockerStack) SetComposeFile(file string) {
	ds.composeFile = file
}

// SetComposeDir sets the directory containing the Docker Compose file.
func (ds *DockerStack) SetComposeDir(dir string) {
	ds.composeDir = dir
}

// StartServices starts all services defined in the compose file.
func (ds *DockerStack) StartServices() error {
	ds.logger.Info("Starting Docker services",
		zap.String("compose_file", ds.composeFile),
		zap.String("compose_dir", ds.composeDir))

	args := append([]string{}, ds.composeCmd...)
	args = append(args, "-f", ds.composeFile, "up", "-d")
	//nolint:gosec // Test environment
	cmd := exec.CommandContext(context.Background(), args[0], args[1:]...)
	cmd.Dir = ds.composeDir

	output, err := cmd.CombinedOutput()
	if err != nil {
		ds.logger.Error("Failed to start services",
			zap.Error(err),
			zap.String("output", string(output)),
			zap.String("working_dir", cmd.Dir),
			zap.String("compose_file", ds.composeFile))

		return fmt.Errorf("failed to start Docker services: %w", err)
	}

	ds.logger.Info("Docker services started")
	ds.addCleanup(func() {
		_ = ds.StopServices()
	})

	return nil
}

// StopServices stops all running services.
func (ds *DockerStack) StopServices() error {
	args := append([]string{}, ds.composeCmd...)
	args = append(args, "-f", ds.composeFile, "down", "-v")
	//nolint:gosec // Test environment
	cmd := exec.CommandContext(context.Background(), args[0], args[1:]...)
	cmd.Dir = ds.composeDir

	output, err := cmd.CombinedOutput()
	if err != nil {
		ds.logger.Error("Failed to stop services", zap.Error(err), zap.String("output", string(output)))

		return fmt.Errorf("failed to stop Docker services: %w", err)
	}

	ds.logger.Info("Docker services stopped")

	return nil
}

// WaitForService waits for a service to become healthy.
func (ds *DockerStack) WaitForService(serviceName, healthURL string, timeout time.Duration) error {
	client := &http.Client{Timeout: httpClientTimeout}
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, healthURL, nil)

		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()

			if resp.StatusCode < httpStatusServerError {
				ds.logger.Info("Service became healthy", zap.String("service", serviceName))

				return nil
			}
		}

		ds.logger.Debug("Waiting for service",
			zap.String("service", serviceName),
			zap.String("url", healthURL),
			zap.Error(err))

		time.Sleep(healthCheckRetryInterval)
	}

	return fmt.Errorf("service %s did not become healthy within %v", serviceName, timeout)
}

// GetServiceURL returns the URL for a service.
func (ds *DockerStack) GetServiceURL(serviceName string) (string, error) {
	if url, exists := ds.services[serviceName]; exists {
		return url, nil
	}

	return "", fmt.Errorf("service %s not found", serviceName)
}

// AddService registers a service URL.
func (ds *DockerStack) AddService(name, url string) {
	ds.services[name] = url
}

// RunCommand executes a command in a service container.
func (ds *DockerStack) RunCommand(serviceName string, command ...string) ([]byte, error) {
	args := append([]string{}, ds.composeCmd...)
	args = append(args, "exec", "-T", serviceName)
	args = append(args, command...)
	//nolint:gosec // Test environment
	cmd := exec.CommandContext(context.Background(), args[0], args[1:]...)
	cmd.Dir = ds.composeDir

	output, err := cmd.CombinedOutput()
	if err != nil {
		ds.logger.Error("Command failed in container",
			zap.String("service", serviceName),
			zap.Strings("command", command),
			zap.Error(err),
			zap.String("output", string(output)))

		return nil, fmt.Errorf("command failed in container %s: %w", serviceName, err)
	}

	return output, nil
}

// GetContainerLogs retrieves logs from a container.
func (ds *DockerStack) GetContainerLogs(serviceName string) ([]byte, error) {
	args := append([]string{}, ds.composeCmd...)
	args = append(args, "-f", ds.composeFile, "logs", serviceName)
	//nolint:gosec // Test environment
	cmd := exec.CommandContext(context.Background(), args[0], args[1:]...)
	cmd.Dir = ds.composeDir

	return cmd.CombinedOutput()
}

// RestartService restarts a specific service.
func (ds *DockerStack) RestartService(serviceName string) error {
	args := append([]string{}, ds.composeCmd...)
	args = append(args, "-f", ds.composeFile, "restart", serviceName)
	//nolint:gosec // Test environment
	cmd := exec.CommandContext(context.Background(), args[0], args[1:]...)
	cmd.Dir = ds.composeDir

	output, err := cmd.CombinedOutput()
	if err != nil {
		ds.logger.Error("Failed to restart service",
			zap.String("service", serviceName),
			zap.Error(err),
			zap.String("output", string(output)))

		return fmt.Errorf("failed to restart service %s: %w", serviceName, err)
	}

	ds.logger.Info("Service restarted", zap.String("service", serviceName))

	return nil
}

// ScaleService scales a service to the specified number of replicas.
func (ds *DockerStack) ScaleService(serviceName string, replicas int) error {
	args := append([]string{}, ds.composeCmd...)
	args = append(args, "-f", ds.composeFile, "up", "-d", "--scale",
		fmt.Sprintf("%s=%d", serviceName, replicas), serviceName)
	//nolint:gosec // Test environment
	cmd := exec.CommandContext(context.Background(), args[0], args[1:]...)
	cmd.Dir = ds.composeDir

	output, err := cmd.CombinedOutput()
	if err != nil {
		ds.logger.Error("Failed to scale service",
			zap.String("service", serviceName),
			zap.Int("replicas", replicas),
			zap.Error(err),
			zap.String("output", string(output)))

		return fmt.Errorf("failed to scale service %s: %w", serviceName, err)
	}

	ds.logger.Info("Service scaled",
		zap.String("service", serviceName),
		zap.Int("replicas", replicas))

	return nil
}

// IsServiceHealthy checks if a service is currently healthy.
func (ds *DockerStack) IsServiceHealthy(serviceName, healthURL string) bool {
	client := &http.Client{Timeout: httpClientTimeout}

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, healthURL, nil)

	resp, err := client.Do(req)
	if err != nil {
		return false
	}

	defer func() { _ = resp.Body.Close() }()

	return resp.StatusCode < httpStatusServerError
}

// WaitForPort waits for a port to become available.
func (ds *DockerStack) WaitForPort(host string, port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	address := fmt.Sprintf("%s:%d", host, port)

	for time.Now().Before(deadline) {
		dialer := &net.Dialer{}

		conn, err := dialer.DialContext(context.Background(), "tcp", address)
		if err == nil {
			_ = conn.Close()

			ds.logger.Info("Port became available",
				zap.String("address", address))

			return nil
		}

		time.Sleep(1 * time.Second)
	}

	return fmt.Errorf("port %s did not become available within %v", address, timeout)
}

// addCleanup adds a cleanup function.
func (ds *DockerStack) addCleanup(cleanup func()) {
	ds.cleanup = append(ds.cleanup, cleanup)
}

// Cleanup performs cleanup of all resources.
func (ds *DockerStack) Cleanup() {
	for i := len(ds.cleanup) - 1; i >= 0; i-- {
		ds.cleanup[i]()
	}
}

// NetworkInfo holds information about Docker networks.
type NetworkInfo struct {
	Name   string
	Driver string
	Subnet string
}

// CreateNetwork creates a Docker network for testing.
func (ds *DockerStack) CreateNetwork(info NetworkInfo) error {
	args := []string{"network", "create", "--driver", info.Driver}
	if info.Subnet != "" {
		args = append(args, "--subnet", info.Subnet)
	}

	args = append(args, info.Name)

	cmd := exec.CommandContext(context.Background(), "docker", args...) //nolint:gosec // Test environment

	output, err := cmd.CombinedOutput()
	if err != nil && !strings.Contains(string(output), "already exists") {
		return fmt.Errorf("failed to create network %s: %w", info.Name, err)
	}

	ds.addCleanup(func() {
		_ = ds.RemoveNetwork(info.Name)
	})

	ds.logger.Info("Network created", zap.String("name", info.Name))

	return nil
}

// RemoveNetwork removes a Docker network.
func (ds *DockerStack) RemoveNetwork(name string) error {
	cmd := exec.CommandContext(context.Background(), "docker", "network", "rm", name)

	output, err := cmd.CombinedOutput()
	if err != nil && !strings.Contains(string(output), "not found") {
		return fmt.Errorf("failed to remove network %s: %w", name, err)
	}

	ds.logger.Info("Network removed", zap.String("name", name))

	return nil
}
