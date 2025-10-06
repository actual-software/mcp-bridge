package k8se2e

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
)

// Constants for configuration values.
const (
	configFilePermissions = 0600
	certDirPermissions    = 0750
	httpClientTimeout     = 5 * time.Second
	healthCheckInterval   = 2 * time.Second
	serviceWaitDelay      = 10 * time.Second
)

// KubernetesStack manages the Kubernetes cluster and services for E2E testing.
type KubernetesStack struct {
	t           *testing.T
	logger      *zap.Logger
	clusterName string
	namespace   string
	services    map[string]string
	cleanup     []func()
	cleanupCtx  context.Context
}

// NewKubernetesStack creates a new Kubernetes stack manager.
func NewKubernetesStack(t *testing.T) *KubernetesStack {
	t.Helper()

	logger, _ := zap.NewDevelopment()

	return &KubernetesStack{
		t:           t,
		logger:      logger,
		clusterName: "mcp-e2e-test",
		namespace:   "mcp-e2e",
		services:    make(map[string]string),
		cleanup:     make([]func(), 0),
	}
}

// Start creates the cluster and deploys all services.
func (ks *KubernetesStack) Start(ctx context.Context) error {
	ks.logger.Info("Starting Kubernetes test environment")

	// Store context for cleanup operations
	ks.cleanupCtx = ctx

	// SAFETY CHECK: Verify we're not on a production cluster before creating KinD cluster
	if err := ks.verifyKubernetesContext(ctx); err != nil {
		return fmt.Errorf("context safety verification failed: %w", err)
	}

	// Clean up Docker resources to ensure we have enough space
	if err := ks.cleanupDockerResources(ctx); err != nil {
		ks.logger.Warn("Failed to cleanup Docker resources", zap.Error(err))
		// Continue anyway - this is not critical
	}

	// Create KinD cluster
	if err := ks.createCluster(ctx); err != nil {
		return fmt.Errorf("failed to create cluster: %w", err)
	}

	// SAFETY: Ensure we're using the correct KinD context, not production clusters
	// #nosec G204 - kubectl command with controlled test inputs
	contextCmd := exec.CommandContext(ctx, "kubectl", "config", "use-context", "kind-"+ks.clusterName)
	if err := contextCmd.Run(); err != nil {
		return fmt.Errorf("failed to set kubectl context to kind-%s: %w", ks.clusterName, err)
	}

	ks.logger.Info("Set kubectl context to KinD cluster", zap.String("context", "kind-"+ks.clusterName))

	// Create namespace
	if err := ks.createNamespace(ctx); err != nil {
		return fmt.Errorf("failed to create namespace: %w", err)
	}

	// Build and load images
	if err := ks.buildAndLoadImages(ctx); err != nil {
		return fmt.Errorf("failed to build and load images: %w", err)
	}

	// Generate TLS certificates
	if err := ks.generateTLSCertificates(ctx); err != nil {
		return fmt.Errorf("failed to generate TLS certificates: %w", err)
	}

	// Deploy services
	if err := ks.deployServices(ctx); err != nil {
		return fmt.Errorf("failed to deploy services: %w", err)
	}

	// Wait for services to be ready
	if err := ks.waitForServices(ctx); err != nil {
		return fmt.Errorf("services failed to become ready: %w", err)
	}

	// Set service URLs
	ks.services["gateway"] = "https://localhost:30443"
	ks.services["gateway-ws"] = "wss://localhost:30443/mcp"
	ks.services["gateway-health"] = "http://localhost:30080/healthz"
	ks.services["metrics"] = "http://localhost:30090/metrics"

	ks.logger.Info("Kubernetes stack is ready", zap.Any("services", ks.services))

	return nil
}

// createCluster creates a KinD cluster.

func (ks *KubernetesStack) createCluster(ctx context.Context) error {
	ks.logger.Info("Creating KinD cluster", zap.String("name", ks.clusterName))

	// Clean up any existing cluster
	if err := ks.cleanupExistingCluster(ctx); err != nil {
		return err
	}

	// Create and configure cluster
	configPath, err := ks.createKindConfig()
	if err != nil {
		return err
	}

	if err := ks.executeClusterCreation(ctx, configPath); err != nil {
		return err
	}

	// Setup kubectl context and verify
	return ks.setupKubectlContext(ctx)
}

func (ks *KubernetesStack) cleanupExistingCluster(ctx context.Context) error {
	// #nosec G204 - command with controlled test inputs
	checkCmd := exec.CommandContext(ctx, "kind", "get", "clusters")

	output, err := checkCmd.Output()
	if err == nil && strings.Contains(string(output), ks.clusterName) {
		ks.logger.Info("Cluster already exists, deleting it first")
		// #nosec G204 - command with controlled test inputs
		deleteCmd := exec.CommandContext(ctx, "kind", "delete", "cluster", "--name", ks.clusterName)
		_ = deleteCmd.Run()
	}

	return nil
}

func (ks *KubernetesStack) createKindConfig() (string, error) {
	kindConfig := `kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
name: ` + ks.clusterName + `
nodes:
- role: control-plane
  kubeadmConfigPatches:
  - |
    kind: InitConfiguration
    nodeRegistration:
      kubeletExtraArgs:
        node-labels: "ingress-ready=true"
  extraPortMappings:
  - containerPort: 30443
    hostPort: 30443
    protocol: TCP
  - containerPort: 30080
    hostPort: 30080
    protocol: TCP
  - containerPort: 30090
    hostPort: 30090
    protocol: TCP
- role: worker
`

	configPath := filepath.Join(os.TempDir(), "kind-config.yaml")
	if err := os.WriteFile(configPath, []byte(kindConfig), configFilePermissions); err != nil {
		return "", fmt.Errorf("failed to write kind config: %w", err)
	}

	ks.cleanup = append(ks.cleanup, func() {
		_ = os.Remove(configPath)
	})

	return configPath, nil
}

func (ks *KubernetesStack) executeClusterCreation(ctx context.Context, configPath string) error {
	// #nosec G204 - kind command with controlled test inputs
	createCmd := exec.CommandContext(ctx, "kind", "create", "cluster",
		"--name", ks.clusterName,
		"--config", configPath,
		"--wait", "300s")
	createCmd.Stdout = os.Stdout
	createCmd.Stderr = os.Stderr

	if err := createCmd.Run(); err != nil {
		return fmt.Errorf("failed to create kind cluster: %w", err)
	}

	ks.cleanup = append(ks.cleanup, func() {
		ks.destroyCluster(ks.cleanupCtx)
	})

	return nil
}

func (ks *KubernetesStack) setupKubectlContext(ctx context.Context) error {
	// #nosec G204 - command with controlled test inputs
	contextCmd := exec.CommandContext(ctx, "kubectl", "config", "use-context", "kind-"+ks.clusterName)
	if err := contextCmd.Run(); err != nil {
		return fmt.Errorf("failed to set kubectl context: %w", err)
	}

	// #nosec G204 - command with controlled test inputs
	verifyCmd := exec.CommandContext(ctx, "kubectl", "config", "current-context")

	currentContext, err := verifyCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to verify kubectl context: %w", err)
	}

	expectedContext := "kind-" + ks.clusterName

	actualContext := strings.TrimSpace(string(currentContext))
	if actualContext != expectedContext {
		return fmt.Errorf("kubectl context verification failed: expected '%s', got '%s'", expectedContext, actualContext)
	}

	ks.logger.Info("Successfully verified kubectl context", zap.String("context", actualContext))

	return nil
}

// createNamespace creates the test namespace.
func (ks *KubernetesStack) createNamespace(ctx context.Context) error {
	ks.logger.Info("Creating namespace", zap.String("namespace", ks.namespace))

	// #nosec G204 - command with controlled test inputs
	cmd := exec.CommandContext(ctx, "kubectl", "create", "namespace", ks.namespace)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to create namespace: %w", err)
	}

	return nil
}

// checkDocker checks if Docker is available and running.
func (ks *KubernetesStack) checkDocker(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "docker", "version")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker is not available: %w", err)
	}

	return nil
}

// buildAndLoadSingleImage builds and loads a single Docker image.
func (ks *KubernetesStack) buildAndLoadSingleImage(
	ctx context.Context,
	projectRoot, imageName, dockerfilePath, contextPath string,
) error {
	// Build the image
	// #nosec G204 - docker build command with controlled test inputs
	buildCmd := exec.CommandContext(
		ctx, "docker", "build",
		"-f", filepath.Join(projectRoot, dockerfilePath),
		"-t", imageName,
		filepath.Join(projectRoot, contextPath),
	)
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr

	ks.logger.Info("Building Docker image", zap.String("image", imageName))

	if err := buildCmd.Run(); err != nil {
		return fmt.Errorf("failed to build image %s: %w", imageName, err)
	}

	// Load the image into KinD
	// #nosec G204 - command with controlled test inputs
	loadCmd := exec.CommandContext(ctx, "kind", "load", "docker-image", imageName, "--name", ks.clusterName)
	if err := loadCmd.Run(); err != nil {
		return fmt.Errorf("failed to load image %s into KinD: %w", imageName, err)
	}

	return nil
}

// buildAndLoadImages builds Docker images and loads them into KinD.
func (ks *KubernetesStack) buildAndLoadImages(ctx context.Context) error {
	ks.logger.Info("Building and loading Docker images")

	// First check if Docker is available
	if err := ks.checkDocker(ctx); err != nil {
		return err
	}

	projectRoot, err := ks.getProjectRoot()
	if err != nil {
		return fmt.Errorf("failed to get project root: %w", err)
	}

	ks.logger.Info("DEBUG: Project root detected", zap.String("projectRoot", projectRoot))

	images := []struct {
		name       string
		dockerfile string
		context    string
	}{
		{
			name:       "mcp-gateway:test",
			dockerfile: "services/gateway/Dockerfile",
			context:    ".",
		},
		{
			name:       "mcp-router:test",
			dockerfile: "services/router/Dockerfile",
			context:    ".",
		},
		{
			name:       "test-mcp-server:test",
			dockerfile: "test/e2e/full_stack/test-mcp-server/Dockerfile",
			context:    "test/e2e/full_stack/test-mcp-server",
		},
	}

	for _, img := range images {
		if err := ks.buildAndLoadSingleImage(ctx, projectRoot, img.name, img.dockerfile, img.context); err != nil {
			return err
		}
	}

	return nil
}

// deployServices deploys all required services to Kubernetes.
func (ks *KubernetesStack) deployServices(ctx context.Context) error {
	ks.logger.Info("Deploying services to Kubernetes")

	manifests := []string{
		ks.generateRBACManifest(),
		ks.generateRedisManifest(),
		ks.generateTestMCPServerManifest(),
		ks.generateGatewayConfigMap(),
		ks.generateGatewayManifest(),
		ks.generateGatewayService(),
	}

	for i, manifest := range manifests {
		ks.logger.Info("Applying manifest", zap.Int("index", i+1), zap.Int("total", len(manifests)))

		// #nosec G204 - command with controlled test inputs
		cmd := exec.CommandContext(ctx, "kubectl", "apply", "-f", "-")

		cmd.Stdin = strings.NewReader(manifest)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to apply manifest %d: %w", i+1, err)
		}
	}

	return nil
}

// waitForServices waits for all services to be ready.
func (ks *KubernetesStack) waitForServices(ctx context.Context) error {
	ks.logger.Info("Waiting for services to be ready")

	deployments := []string{"redis", "test-mcp-server", "mcp-gateway"}

	for _, deployment := range deployments {
		ks.logger.Info("Waiting for deployment", zap.String("deployment", deployment))

		// #nosec G204 - kubectl command with controlled test inputs
		cmd := exec.CommandContext(ctx, "kubectl", "wait", "--for=condition=available",
			"deployment/"+deployment,
			"-n", ks.namespace,
			"--timeout=180s")

		if err := cmd.Run(); err != nil {
			return fmt.Errorf("deployment %s failed to become ready: %w", deployment, err)
		}
	}

	// Wait a bit more for services to fully stabilize
	time.Sleep(serviceWaitDelay)

	return nil
}

// GetGatewayURL returns the gateway WebSocket URL.
func (ks *KubernetesStack) GetGatewayURL() string {
	return ks.services["gateway-ws"]
}

// GetGatewayHTTPURL returns the gateway HTTP URL.
func (ks *KubernetesStack) GetGatewayHTTPURL() string {
	return ks.services["gateway"]
}

// GetGatewayHealthURL returns the gateway health URL (HTTP, no TLS).
func (ks *KubernetesStack) GetGatewayHealthURL() string {
	return ks.services["gateway-health"]
}

// GetServiceLogs returns logs from a specific service.
func (ks *KubernetesStack) GetServiceLogs(service string) (string, error) {
	ctx := context.Background()
	// #nosec G204 - command with controlled test inputs
	cmd := exec.CommandContext(ctx, "kubectl", "logs", "-n", ks.namespace, "deployment/"+service, "--tail=100")
	output, err := cmd.Output()

	return string(output), err
}

// checkPodStatus checks and logs the status of pods for a deployment.

// createHTTPClient creates an HTTP client with appropriate settings.
func (ks *KubernetesStack) createHTTPClient() *http.Client {
	return &http.Client{
		Timeout: httpClientTimeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, // #nosec G402 - acceptable for test environment
			},
		},
	}
}

// checkHTTPEndpointOnce checks the HTTP endpoint once.
func (ks *KubernetesStack) checkHTTPEndpointOnce(
	ctx context.Context,
	client *http.Client,
	url string,
	attemptNum int,
) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	resp, err := client.Do(req)
	if err != nil {
		if attemptNum > 0 && attemptNum%10 == 0 {
			ks.logger.Debug("Still waiting for endpoint",
				zap.String("url", url),
				zap.Int("attempt", attemptNum),
				zap.Error(err))
		}

		return err
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNoContent {
		ks.logger.Info("HTTP endpoint ready", zap.String("url", url))

		return nil
	}

	return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
}

// logFinalDiagnostics logs diagnostics when endpoint check fails.
func (ks *KubernetesStack) logFinalDiagnostics(ctx context.Context, url string) {
	ks.logger.Error("HTTP endpoint did not become ready",
		zap.String("url", url),
		zap.String("suggestion", "Check pod status with: kubectl get pods -n "+ks.namespace))

	// Try to get pod status for debugging
	// #nosec G204 - kubectl command with controlled test inputs
	if statusOutput, err := exec.CommandContext(ctx, "kubectl", "get", "pods", "-n", ks.namespace).Output(); err == nil {
		ks.logger.Info("Current pod status", zap.String("pods", string(statusOutput)))
	}
}

// WaitForHTTPEndpoint waits for an HTTP endpoint to be available.
func (ks *KubernetesStack) WaitForHTTPEndpoint(ctx context.Context, url string) error {
	ks.logger.Info("Waiting for HTTP endpoint", zap.String("url", url))

	client := ks.createHTTPClient()

	for i := 0; i < 30; i++ {
		if err := ks.checkHTTPEndpointOnce(ctx, client, url, i); err == nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(healthCheckInterval):
			// Continue waiting
		}
	}

	ks.logFinalDiagnostics(ctx, url)

	return fmt.Errorf("HTTP endpoint %s did not become ready", url)
}

// Cleanup stops all services and cleans up resources.
func (ks *KubernetesStack) Cleanup() {
	ks.logger.Info("Cleaning up Kubernetes stack")

	for i := len(ks.cleanup) - 1; i >= 0; i-- {
		ks.cleanup[i]()
	}
}

// Stop is an alias for Cleanup to match the existing interface.
func (ks *KubernetesStack) Stop() {
	ks.Cleanup()
}

// cleanupDockerResources cleans up Docker to ensure we have enough space for tests.
func (ks *KubernetesStack) cleanupDockerResources(ctx context.Context) error {
	ks.logger.Info("Cleaning up Docker resources to free space")

	// Run docker system prune to clean up unused resources
	pruneCmd := exec.CommandContext(ctx, "docker", "system", "prune", "-af", "--volumes")

	output, err := pruneCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to prune Docker resources: %w (output: %s)", err, string(output))
	}

	ks.logger.Info("Docker cleanup completed", zap.String("output", string(output)))

	return nil
}

// verifyKubernetesContext ensures we're not accidentally on a production cluster.
func (ks *KubernetesStack) verifyKubernetesContext(ctx context.Context) error {
	ks.logger.Info("Verifying Kubernetes context safety")

	// Get current context
	// #nosec G204 - command with controlled test inputs
	currentContextCmd := exec.CommandContext(ctx, "kubectl", "config", "current-context")

	currentContext, err := currentContextCmd.Output()
	if err != nil {
		// No current context is fine - we'll create our own
		ks.logger.Info("No current kubectl context set, safe to proceed")

		return nil
	}

	contextStr := strings.TrimSpace(string(currentContext))
	ks.logger.Info("Current kubectl context", zap.String("context", contextStr))

	// List of patterns that indicate production or important clusters
	dangerousPatterns := []string{
		"prod",
		"production",
		"arn:aws:eks",   // AWS EKS clusters
		"gke_",          // Google GKE clusters
		"aks-",          // Azure AKS clusters
		"camunda",       // Specific production cluster
		"matellio",      // Specific production cluster
		"actualai-test", // Test clusters that shouldn't be touched
		"actualai-dev",  // Dev clusters that shouldn't be touched
	}

	// Check if current context matches any dangerous patterns
	for _, pattern := range dangerousPatterns {
		if strings.Contains(strings.ToLower(contextStr), strings.ToLower(pattern)) {
			ks.logger.Error("SAFETY CHECK FAILED: Current context appears to be a production/important cluster",
				zap.String("context", contextStr),
				zap.String("matched_pattern", pattern))

			return fmt.Errorf(
				"refusing to run tests: current kubectl context '%s' appears to be a production or important cluster. "+
					"Please switch to a safe context or unset the current context",
				contextStr,
			)
		}
	}

	// If the context is a KinD cluster, that's fine
	if strings.HasPrefix(contextStr, "kind-") {
		ks.logger.Info("Current context is a KinD cluster, safe to proceed", zap.String("context", contextStr))

		return nil
	}

	// If context exists but isn't recognized as dangerous or KinD, warn but proceed
	ks.logger.Warn("Current context is not recognized, will create isolated KinD cluster",
		zap.String("context", contextStr))

	return nil
}

// destroyCluster destroys the KinD cluster.
func (ks *KubernetesStack) destroyCluster(ctx context.Context) {
	ks.logger.Info("Destroying cluster", zap.String("name", ks.clusterName))

	// #nosec G204 - command with controlled test inputs
	cmd := exec.CommandContext(ctx, "kind", "delete", "cluster", "--name", ks.clusterName)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	_ = cmd.Run()
}

// generateTLSCertificates generates TLS certificates for the gateway.
func (ks *KubernetesStack) generateTLSCertificates(ctx context.Context) error {
	ks.logger.Info("Generating TLS certificates for gateway")

	// Create temporary directory for certificates
	certDir := filepath.Join(os.TempDir(), "mcp-e2e-certs")
	if err := os.MkdirAll(certDir, certDirPermissions); err != nil {
		return fmt.Errorf("failed to create cert directory: %w", err)
	}

	ks.cleanup = append(ks.cleanup, func() {
		_ = os.RemoveAll(certDir)
	})

	keyPath := filepath.Join(certDir, "tls.key")
	certPath := filepath.Join(certDir, "tls.crt")

	// Generate private key
	// #nosec G204 - openssl command with controlled test inputs
	keyCmd := exec.CommandContext(
		ctx, "openssl", "genrsa", "-out", keyPath, "2048",
	)
	if err := keyCmd.Run(); err != nil {
		return fmt.Errorf("failed to generate private key: %w", err)
	}

	// Generate certificate
	// #nosec G204 - openssl command with controlled test inputs
	certCmd := exec.CommandContext(
		ctx, "openssl", "req", "-new", "-x509", "-key", keyPath,
		"-out", certPath, "-days", "365", "-subj", "/CN=mcp-gateway/O=mcp-e2e-test",
		"-addext", "subjectAltName=DNS:mcp-gateway,DNS:mcp-gateway."+ks.namespace+
			".svc.cluster.local,DNS:localhost,IP:127.0.0.1",
	)
	if err := certCmd.Run(); err != nil {
		return fmt.Errorf("failed to generate certificate: %w", err)
	}

	// Create Kubernetes TLS secret
	// #nosec G204 - kubectl command with controlled test inputs
	secretCmd := exec.CommandContext(ctx, "kubectl", "create", "secret", "tls", "gateway-tls",
		"--cert="+certPath, "--key="+keyPath, "-n", ks.namespace)
	if err := secretCmd.Run(); err != nil {
		return fmt.Errorf("failed to create TLS secret: %w", err)
	}

	ks.logger.Info("TLS certificates generated and secret created")

	return nil
}

// getProjectRoot finds the project root directory using shared infrastructure.
func (ks *KubernetesStack) getProjectRoot() (string, error) {
	// Start from current directory
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current directory: %w", err)
	}

	// Look for the root go.mod that contains "module github.com/actual-software/mcp-bridge"
	for {
		goModPath := filepath.Join(dir, "go.mod")
		// #nosec G304 - reading go.mod with controlled path in test context
		if content, err := os.ReadFile(goModPath); err == nil {
			if strings.Contains(string(content), "module github.com/actual-software/mcp-bridge") {
				return dir, nil
			}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("could not find project root (no go.mod with module github.com/actual-software/mcp-bridge found)")
		}

		dir = parent
	}
}

// Manifest generation methods follow...

func (ks *KubernetesStack) generateRBACManifest() string {
	return `apiVersion: v1
kind: ServiceAccount
metadata:
  name: mcp-gateway
  namespace: ` + ks.namespace + `
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: mcp-gateway-` + ks.namespace + `
rules:
- apiGroups: [""]
  resources: ["services", "endpoints"]
  verbs: ["get", "list", "watch"]
- apiGroups: [""]
  resources: ["pods"]
  verbs: ["get", "list", "watch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: mcp-gateway-` + ks.namespace + `
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: mcp-gateway-` + ks.namespace + `
subjects:
- kind: ServiceAccount
  name: mcp-gateway
  namespace: ` + ks.namespace + `
`
}

func (ks *KubernetesStack) generateRedisManifest() string {
	return `apiVersion: apps/v1
kind: Deployment
metadata:
  name: redis
  namespace: ` + ks.namespace + `
spec:
  replicas: 1
  selector:
    matchLabels:
      app: redis
  template:
    metadata:
      labels:
        app: redis
    spec:
      containers:
      - name: redis
        image: redis:7-alpine
        ports:
        - containerPort: 6379
        resources:
          requests:
            memory: "64Mi"
            cpu: "250m"
          limits:
            memory: "128Mi"
            cpu: "500m"
---
apiVersion: v1
kind: Service
metadata:
  name: redis
  namespace: ` + ks.namespace + `
spec:
  selector:
    app: redis
  ports:
  - port: 6379
    targetPort: 6379
`
}

func (ks *KubernetesStack) generateTestMCPServerManifest() string {
	return `apiVersion: apps/v1
kind: Deployment
metadata:
  name: test-mcp-server
  namespace: ` + ks.namespace + `
spec:
  replicas: 2
  selector:
    matchLabels:
      app: test-mcp-server
  template:
    metadata:
      labels:
        app: test-mcp-server
    spec:
      containers:
      - name: test-mcp-server
        image: test-mcp-server:test
        imagePullPolicy: Never
        ports:
        - containerPort: 3000
        env:
        - name: BACKEND_ID
          valueFrom:
            fieldRef:
              fieldPath: metadata.name
        resources:
          requests:
            memory: "64Mi"
            cpu: "250m"
          limits:
            memory: "128Mi"
            cpu: "500m"
---
apiVersion: v1
kind: Service
metadata:
  name: test-mcp-server
  namespace: ` + ks.namespace + `
spec:
  selector:
    app: test-mcp-server
  ports:
  - port: 3000
    targetPort: 3000
`
}

func (ks *KubernetesStack) generateGatewayConfigMap() string {
	return `apiVersion: v1
kind: ConfigMap
metadata:
  name: gateway-config
  namespace: ` + ks.namespace + `
data:
  gateway.yaml: |
    server:
      host: "0.0.0.0"
      port: 8443
      health_port: 8080
      metrics_port: 9091
      tls:
        enabled: true
        cert_file: "/etc/tls/tls.crt"
        key_file: "/etc/tls/tls.key"
        min_version: "1.2"
      frontends:
        - name: "websocket"
          protocol: "websocket"
          enabled: true
          config:
            port: 8443
            bind_addr: "0.0.0.0:8443"
            path: "/ws"
            tls:
              enabled: true
              cert_file: "/etc/tls/tls.crt"
              key_file: "/etc/tls/tls.key"
              min_version: "1.2"
        - name: "http"
          protocol: "http"
          enabled: true
          config:
            port: 8443
            bind_addr: "0.0.0.0:8443"
            path: "/mcp"
            tls:
              enabled: true
              cert_file: "/etc/tls/tls.crt"
              key_file: "/etc/tls/tls.key"
              min_version: "1.2"
    sessions:
      provider: redis
      redis:
        url: "redis://redis:6379/0"
    service_discovery:
      provider: static
      static:
        endpoints:
          system:
            - url: "http://test-mcp-server:3000"
              labels:
                namespace: "mcp-e2e"
          test-mcp-server:
            - url: "http://test-mcp-server:3000"
              labels:
                namespace: "mcp-e2e"
    routing:
      backends:
        - name: "test-mcp-server"
          endpoint: "http://test-mcp-server:3000"
          weight: 100
          health_check:
            enabled: true
            path: "/health"
            interval: "10s"
            timeout: "5s"
    auth:
      provider: jwt
      jwt:
        secret_key_env: "JWT_SECRET_KEY"
        issuer: "mcp-gateway-e2e"
        audience: "mcp-clients"
    logging:
      level: "debug"
      format: "json"
`
}

func (ks *KubernetesStack) generateGatewayManifest() string {
	return `apiVersion: apps/v1
kind: Deployment
metadata:
  name: mcp-gateway
  namespace: ` + ks.namespace + `
spec:
  replicas: 1
  selector:
    matchLabels:
      app: mcp-gateway
  template:
    metadata:
      labels:
        app: mcp-gateway
    spec:
      serviceAccountName: mcp-gateway
      containers:
      - name: gateway
        image: mcp-gateway:test
        imagePullPolicy: Never
        command: ["/app/mcp-gateway"]
        args: ["--config", "/etc/mcp-gateway/gateway.yaml", "--log-level", "debug"]
        ports:
        - containerPort: 8443
        - containerPort: 8080
        - containerPort: 9091
        env:
        - name: JWT_SECRET_KEY
          value: "test-secret-key-for-e2e-testing"
        volumeMounts:
        - name: config
          mountPath: /etc/mcp-gateway
          readOnly: true
        - name: tls-certs
          mountPath: /etc/tls
          readOnly: true
        resources:
          requests:
            memory: "128Mi"
            cpu: "250m"
          limits:
            memory: "256Mi"
            cpu: "500m"
      volumes:
      - name: config
        configMap:
          name: gateway-config
      - name: tls-certs
        secret:
          secretName: gateway-tls
`
}

func (ks *KubernetesStack) generateGatewayService() string {
	return `apiVersion: v1
kind: Service
metadata:
  name: mcp-gateway
  namespace: ` + ks.namespace + `
spec:
  type: NodePort
  selector:
    app: mcp-gateway
  ports:
  - name: https
    port: 8443
    targetPort: 8443
    nodePort: 30443
  - name: health
    port: 8080
    targetPort: 8080
    nodePort: 30080
  - name: metrics
    port: 9091
    targetPort: 9091
    nodePort: 30090
`
}
