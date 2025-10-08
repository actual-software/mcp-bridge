package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/actual-software/mcp-bridge/test/testutil/e2e"
)

const (
	// Timeouts and intervals.
	serviceWaitTimeout     = 60 * time.Second
	serviceCheckInterval   = 2 * time.Second
	portReleaseDelay       = 2 * time.Second
	httpEndpointTimeout    = 30 * time.Second
	certificateWaitTimeout = 30 * time.Second
	httpClientTimeout      = 5 * time.Second
	certCheckInterval      = 100 * time.Millisecond

	// File permissions.
	dirPermissions  = 0750
	filePermissions = 0600
)

// DockerStack manages the Docker Compose services for E2E testing.
type DockerStack struct {
	t           *testing.T
	logger      *zap.Logger
	composeDir  string
	composeFile string            // compose file name
	services    map[string]string // service name -> URL
	cleanup     []func()
}

// NewDockerStack creates a new Docker stack manager.
func NewDockerStack(t *testing.T) *DockerStack {
	t.Helper()

	logger := e2e.NewTestLogger()

	// Get the directory containing the docker-compose file
	wd, err := os.Getwd()
	require.NoError(t, err)

	return &DockerStack{
		t:           t,
		logger:      logger,
		composeDir:  wd,
		composeFile: "docker-compose.e2e.yml", // default compose file
		services:    make(map[string]string),
		cleanup:     make([]func(), 0),
	}
}

// Start launches the Docker Compose stack and waits for services to be ready.
func (ds *DockerStack) Start(ctx context.Context) error {
	ds.logger.Info("Starting Docker Compose stack")

	// Generate TLS certificates for testing
	if err := ds.generateTLSCertificates(ctx); err != nil {
		return fmt.Errorf("failed to generate TLS certificates: %w", err)
	}

	// Ensure we clean up on exit
	const cleanupTimeout = 30 * time.Second
	//nolint:contextcheck // Cleanup functions cannot accept context
	ds.cleanup = append(ds.cleanup, func() {
		// Use a new context for cleanup since the original might be cancelled
		cleanupCtx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
		defer cancel()

		ds.Stop(cleanupCtx)
	})

	// First, clean up any existing containers from previous runs
	ds.logger.Info("Cleaning up any existing containers")
	cleanupCmd := exec.CommandContext(ctx, "docker-compose", // #nosec G204 - compose file is controlled
		"-f", ds.composeFile,
		"down", "-v", "--remove-orphans")
	cleanupCmd.Dir = ds.composeDir
	// Ignore errors from cleanup - containers might not exist
	_ = cleanupCmd.Run()

	// Wait a moment for ports to be released
	time.Sleep(portReleaseDelay)

	// Start services with BuildKit enabled for better caching
	cmd := exec.CommandContext(ctx, "docker-compose", // #nosec G204 - compose file is controlled
		"-f", ds.composeFile,
		"up", "-d", "--build")
	cmd.Dir = ds.composeDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	// Enable BuildKit for better build performance
	cmd.Env = append(os.Environ(),
		"DOCKER_BUILDKIT=1",
		"COMPOSE_DOCKER_CLI_BUILD=1",
	)

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to start docker-compose: %w", err)
	}

	// Wait for services to be healthy
	services := []string{"redis", "gateway", "test-mcp-server"}
	for _, service := range services {
		if err := ds.waitForService(ctx, service); err != nil {
			return fmt.Errorf("service %s failed to start: %w", service, err)
		}
	}

	// Set service URLs
	ds.services["gateway"] = "https://localhost:8443"
	ds.services["test-mcp-server"] = "http://localhost:3000"
	ds.services["redis"] = "redis://localhost:6379"
	ds.services["prometheus"] = "http://localhost:9092"

	ds.logger.Info("Docker Compose stack is ready",
		zap.Any("services", ds.services))

	return nil
}

// Stop shuts down the Docker Compose stack.
//

func (ds *DockerStack) Stop(ctx context.Context) {
	ds.logger.Info("Stopping Docker Compose stack")

	cmd := exec.CommandContext(ctx, "docker-compose", // #nosec G204 - compose file is controlled
		"-f", ds.composeFile,
		"down", "-v", "--remove-orphans")
	cmd.Dir = ds.composeDir

	if err := cmd.Run(); err != nil {
		ds.logger.Error("Failed to stop docker-compose", zap.Error(err))
	}
}

// Cleanup runs all registered cleanup functions.
func (ds *DockerStack) Cleanup() {
	for i := len(ds.cleanup) - 1; i >= 0; i-- {
		ds.cleanup[i]()
	}
}

// GetServiceURL returns the URL for a service.
func (ds *DockerStack) GetServiceURL(service string) (string, bool) {
	url, exists := ds.services[service]

	return url, exists
}

// GetGatewayURL returns the gateway WebSocket URL.
func (ds *DockerStack) GetGatewayURL() string {
	return "wss://localhost:8443/ws"
}

// GetGatewayHTTPURL returns the gateway HTTP URL.
func (ds *DockerStack) GetGatewayHTTPURL() string {
	return "https://localhost:8443"
}

// RestartService restarts a specific service (useful for resilience testing).
func (ds *DockerStack) RestartService(ctx context.Context, service string) error {
	ds.logger.Info("Restarting service", zap.String("service", service))

	// Stop the service
	cmd := exec.CommandContext(ctx, "docker-compose", // #nosec G204 - service name is controlled
		"-f", ds.composeFile,
		"stop", service)
	cmd.Dir = ds.composeDir

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to stop service %s: %w", service, err)
	}

	// Wait a moment for the port to be released
	time.Sleep(portReleaseDelay)

	// Start the service
	cmd = exec.CommandContext(ctx, "docker-compose", // #nosec G204 - service name is controlled
		"-f", ds.composeFile,
		"start", service)
	cmd.Dir = ds.composeDir

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to start service %s: %w", service, err)
	}

	// Wait for it to be healthy again
	return ds.waitForService(ctx, service)
}

// GetServiceLogs returns logs for a specific service.
//

func (ds *DockerStack) GetServiceLogs(ctx context.Context, service string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker-compose", // #nosec G204 - service name is controlled
		"-f", ds.composeFile,
		"logs", "--no-color", service)
	cmd.Dir = ds.composeDir

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get logs for %s: %w", service, err)
	}

	return string(output), nil
}

// waitForService waits for a service to be healthy.
func (ds *DockerStack) waitForService(ctx context.Context, service string) error {
	ds.logger.Info("Waiting for service to be ready", zap.String("service", service))

	timeout := serviceWaitTimeout
	if deadline, ok := ctx.Deadline(); ok {
		timeout = time.Until(deadline)
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(serviceCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Get service logs for debugging
			logs, _ := ds.GetServiceLogs(ctx, service)

			return fmt.Errorf("timeout waiting for service %s: %s", service, logs)

		case <-ticker.C:
			if ds.isServiceHealthy(ctx, service) {
				ds.logger.Info("Service is ready", zap.String("service", service))

				return nil
			}
		}
	}
}

// isServiceHealthy checks if a service is healthy.
//

func (ds *DockerStack) isServiceHealthy(ctx context.Context, service string) bool {
	// First check docker-compose health status
	cmd := exec.CommandContext(ctx, "docker-compose", // #nosec G204 - service name is controlled
		"-f", ds.composeFile,
		"ps", "-q", service)
	cmd.Dir = ds.composeDir

	output, err := cmd.Output()
	if err != nil || len(strings.TrimSpace(string(output))) == 0 {
		return false
	}

	// Check container health status
	containerID := strings.TrimSpace(string(output))
	cmd = exec.CommandContext(ctx, "docker", "inspect", // #nosec G204 - containerID from docker ps
		"--format={{.State.Health.Status}}", containerID)

	output, err = cmd.Output()
	if err != nil {
		// If no health check is defined, check if container is running
		cmd = exec.CommandContext(ctx, "docker", "inspect", // #nosec G204 - containerID from docker ps
			"--format={{.State.Status}}", containerID)

		output, err = cmd.Output()
		if err != nil {
			return false
		}

		return strings.TrimSpace(string(output)) == "running"
	}

	healthStatus := strings.TrimSpace(string(output))

	return healthStatus == "healthy"
}

// GetContainerIP returns the IP address of a service container.
func (ds *DockerStack) GetContainerIP(service string) (string, error) {
	ctx := context.Background()
	cmd := exec.CommandContext(ctx, "docker-compose", // #nosec G204 - service name is controlled
		"-f", ds.composeFile,
		"exec", "-T", service, "hostname", "-i")
	cmd.Dir = ds.composeDir

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get IP for %s: %w", service, err)
	}

	return strings.TrimSpace(string(output)), nil
}

// WaitForHTTPEndpoint waits for an HTTP endpoint to be available.
func (ds *DockerStack) WaitForHTTPEndpoint(ctx context.Context, url string) error {
	ds.logger.Info("Waiting for HTTP endpoint", zap.String("url", url))

	timeout := httpEndpointTimeout
	if deadline, ok := ctx.Deadline(); ok {
		timeout = time.Until(deadline)
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	// For TLS endpoints, create a client that accepts the self-signed certificate
	client := &http.Client{
		Timeout: httpClientTimeout,
	}

	// Only configure TLS for HTTPS URLs
	if strings.HasPrefix(url, "https://") {
		client.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, // #nosec G402 - Skip certificate verification for E2E tests only
			},
		}
	}

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for endpoint %s", url)

		case <-ticker.C:
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)

			resp, err := client.Do(req)
			if err != nil {
				ds.logger.Debug("Health check failed", zap.String("url", url), zap.Error(err))

				continue
			}

			if err := resp.Body.Close(); err != nil {
				ds.logger.Warn("Failed to close response body", zap.Error(err))
			}

			if resp.StatusCode >= 200 && resp.StatusCode < 400 {
				ds.logger.Info("HTTP endpoint is ready", zap.String("url", url))

				return nil
			}

			ds.logger.Debug("Health check returned non-success status",
				zap.String("url", url),
				zap.Int("status", resp.StatusCode))
		}
	}
}

// ExecuteInService executes a command in a running service container.
func (ds *DockerStack) ExecuteInService(service string, command ...string) (string, error) {
	ctx := context.Background()
	args := []string{"exec", "-T", service}
	args = append(args, command...)

	cmd := exec.CommandContext(ctx, "docker-compose", args...) // #nosec G204 - service and command are controlled
	cmd.Dir = ds.composeDir

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to execute command in %s: %w", service, err)
	}

	return string(output), nil
}

// SaveServiceLogs saves logs from all services to files (useful for debugging).
func (ds *DockerStack) SaveServiceLogs(outputDir string) error {
	services := []string{"gateway", "test-mcp-server", "redis"}

	if err := os.MkdirAll(outputDir, dirPermissions); err != nil { // #nosec G301 - directory permissions
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	for _, service := range services {
		logs, err := ds.GetServiceLogs(context.Background(), service)
		if err != nil {
			ds.logger.Warn("Failed to get logs",
				zap.String("service", service),
				zap.Error(err))

			continue
		}

		logFile := filepath.Join(outputDir, service+".log")
		if err := os.WriteFile(logFile, []byte(logs), filePermissions); err != nil { // #nosec G306 - file permissions
			ds.logger.Warn("Failed to save logs",
				zap.String("service", service),
				zap.String("file", logFile),
				zap.Error(err))
		}
	}

	return nil
}

// generateTLSCertificates generates TLS certificates for testing.
//

func (ds *DockerStack) generateTLSCertificates(ctx context.Context) error {
	ds.logger.Info("Generating TLS certificates for testing")

	// Run the certificate generation script
	cmd := exec.CommandContext(ctx, "./scripts/generate-certs.sh", "./certs", "localhost")
	cmd.Dir = ds.composeDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to generate certificates: %w", err)
	}

	ds.logger.Info("TLS certificates generated successfully")

	// Ensure all certificate files are fully written to disk
	ds.logger.Info("Syncing certificate files to disk")

	// Use sync to ensure files are written to disk
	if err := exec.CommandContext(ctx, "sync").Run(); err != nil {
		ds.logger.Warn("Failed to sync files to disk", zap.Error(err))
	}

	// Wait for certificates to be ready and valid instead of using arbitrary delay
	if err := ds.waitForCertificateReadiness(); err != nil {
		return fmt.Errorf("certificates not ready: %w", err)
	}

	return nil
}

// waitForCertificateReadiness waits for certificate files to be ready and valid.
//

//nolint:funlen // Certificate validation requires thorough checks
func (ds *DockerStack) waitForCertificateReadiness() error {
	certDir := filepath.Join(ds.composeDir, "certs")
	requiredFiles := []string{"tls.crt", "tls.key", "ca.crt", "ca.key"}

	ds.logger.Info("Waiting for certificate files to be ready", zap.String("cert_dir", certDir))

	// Wait up to 30 seconds for certificates to be ready
	timeout := time.Now().Add(certificateWaitTimeout)

	for time.Now().Before(timeout) {
		allReady := true

		for _, filename := range requiredFiles {
			certPath := filepath.Join(certDir, filename)

			// Check if file exists
			info, err := os.Stat(certPath)
			if err != nil {
				ds.logger.Debug("Certificate file not ready",
					zap.String("file", certPath),
					zap.Error(err))

				allReady = false

				break
			}

			// Check if file is not empty
			if info.Size() == 0 {
				ds.logger.Debug("Certificate file is empty",
					zap.String("file", certPath))

				allReady = false

				break
			}

			// For certificate files, verify they can be parsed
			if strings.HasSuffix(filename, ".crt") {
				if err := ds.validateCertificateFile(certPath); err != nil {
					ds.logger.Debug("Certificate file validation failed",
						zap.String("file", certPath),
						zap.Error(err))

					allReady = false

					break
				}
			}

			// For key files, verify they can be parsed
			if strings.HasSuffix(filename, ".key") {
				if err := ds.validatePrivateKeyFile(certPath); err != nil {
					ds.logger.Debug("Private key file validation failed",
						zap.String("file", certPath),
						zap.Error(err))

					allReady = false

					break
				}
			}
		}

		if allReady {
			ds.logger.Info("All certificate files are ready and valid")

			return nil
		}

		// Wait before checking again
		time.Sleep(certCheckInterval)
	}

	return errors.New("timeout waiting for certificate files to be ready")
}

// validateCertificateFile validates that a certificate file is properly formatted.
func (ds *DockerStack) validateCertificateFile(certPath string) error {
	certPEM, err := os.ReadFile(certPath) // #nosec G304 - certPath is from controlled list
	if err != nil {
		return fmt.Errorf("failed to read certificate file: %w", err)
	}

	block, _ := pem.Decode(certPEM)
	if block == nil {
		return errors.New("failed to parse certificate PEM")
	}

	_, err = x509.ParseCertificate(block.Bytes)
	if err != nil {
		return fmt.Errorf("failed to parse certificate: %w", err)
	}

	return nil
}

// validatePrivateKeyFile validates that a private key file is properly formatted.
func (ds *DockerStack) validatePrivateKeyFile(keyPath string) error {
	keyPEM, err := os.ReadFile(keyPath) // #nosec G304 - keyPath is from controlled list
	if err != nil {
		return fmt.Errorf("failed to read private key file: %w", err)
	}

	block, _ := pem.Decode(keyPEM)
	if block == nil {
		return errors.New("failed to parse private key PEM")
	}

	// Try to parse as different key types
	if _, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return nil
	}

	if _, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		return nil
	}

	if _, err := x509.ParseECPrivateKey(block.Bytes); err == nil {
		return nil
	}

	return errors.New("failed to parse private key")
}
