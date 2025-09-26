// Package weather provides comprehensive E2E testing for MCP Gateway and Router with real Weather MCP server
package weather

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

// WeatherE2ETestSuite is defined in k8s_manifests.go

// setupE2ETestSuite initializes the E2E test suite.
func setupE2ETestSuite(t *testing.T) *WeatherE2ETestSuite {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)

	// Create kind cluster for testing
	suite := &WeatherE2ETestSuite{
		namespace:  fmt.Sprintf("mcp-e2e-test-%d", time.Now().Unix()),
		ctx:        ctx,
		cancelFunc: cancel,
	}

	// Set up cleanup to run on test failure or panic
	t.Cleanup(func() {
		suite.cleanup(t)
	})

	// Create kind cluster and build/load images
	if err := suite.createKindCluster(t); err != nil {
		t.Fatalf("Failed to create kind cluster: %v", err)
	}

	// Use the kind cluster context - always use kubeconfig file for KinD clusters
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		home, _ := os.UserHomeDir()
		kubeconfig = filepath.Join(home, ".kube", "config")
	}

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		t.Fatalf("Failed to create kubernetes config: %v", err)
	}

	// Create clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		t.Fatalf("Failed to create kubernetes clientset: %v", err)
	}

	// Update suite with clientset
	suite.k8sClient = clientset

	return suite
}

// TestWeatherMCPE2EFullStack runs the complete E2E test.
func TestWeatherMCPE2EFullStack(t *testing.T) {
	suite := setupE2ETestSuite(t)

	t.Log("Starting Weather MCP E2E Full Stack Test")

	// Step 1: Create namespace
	t.Log("Creating test namespace:", suite.namespace)
	suite.createNamespace(t)

	// Step 2: Deploy Redis for Gateway session management
	t.Log("Deploying Redis for session management")
	suite.deployRedis(t)

	// Step 3: Deploy Weather MCP Server
	t.Log("Deploying Weather MCP Server")
	suite.deployWeatherMCPServer(t)

	// Step 4: Deploy MCP Gateway
	t.Log("Deploying MCP Gateway")
	suite.deployMCPGateway(t)

	// Step 5: Deploy MCP Router
	t.Log("Deploying MCP Router")
	suite.deployMCPRouter(t)

	// Step 6: Wait for all deployments to be ready
	t.Log("Waiting for all deployments to be ready")
	suite.waitForDeployments(t)

	// Step 7: Run MCP Client Agent test
	t.Log("Running MCP Client Agent E2E test")
	suite.runClientAgentTest(t)

	// Step 8: Validate complete flow
	t.Log("Validating complete Agent → Router → Gateway → Weather flow")
	suite.validateCompleteFlow(t)

	t.Log("✅ Weather MCP E2E Full Stack Test completed successfully")
}

// createNamespace is implemented in k8s_manifests.go

// deployRedis deploys Redis for Gateway session management.
func (suite *WeatherE2ETestSuite) deployRedis(t *testing.T) {
	t.Helper()
	// Create Redis deployment
	deployment := suite.createRedisDeployment()

	_, err := suite.k8sClient.AppsV1().Deployments(suite.namespace).Create(suite.ctx, deployment, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create Redis deployment: %v", err)
	}

	// Create Redis service
	service := suite.createRedisService()

	_, err = suite.k8sClient.CoreV1().Services(suite.namespace).Create(suite.ctx, service, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create Redis service: %v", err)
	}
}

// deployWeatherMCPServer deploys the Weather MCP server.
func (suite *WeatherE2ETestSuite) deployWeatherMCPServer(t *testing.T) {
	t.Helper()
	// Create Weather MCP Server deployment
	deployment := suite.createWeatherMCPDeployment()

	_, err := suite.k8sClient.AppsV1().Deployments(suite.namespace).Create(suite.ctx, deployment, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create weather MCP deployment: %v", err)
	}

	// Create Weather MCP Server service
	service := suite.createWeatherMCPService()

	_, err = suite.k8sClient.CoreV1().Services(suite.namespace).Create(suite.ctx, service, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create weather MCP service: %v", err)
	}
}

// deployMCPGateway deploys the MCP Gateway.
func (suite *WeatherE2ETestSuite) deployMCPGateway(t *testing.T) {
	t.Helper()
	// Generate TLS certificates
	t.Log("Generating TLS certificates for Gateway")

	if err := suite.generateTLSCertificates(); err != nil {
		t.Fatalf("Failed to generate TLS certificates: %v", err)
	}

	// Create TLS secret
	t.Log("Creating TLS secret in Kubernetes")

	if err := suite.createTLSSecret(); err != nil {
		t.Fatalf("Failed to create TLS secret: %v", err)
	}

	// Create service accounts
	suite.createServiceAccounts(t)

	// Create Gateway ConfigMap
	configMap := suite.createGatewayConfigMap()

	_, err := suite.k8sClient.CoreV1().ConfigMaps(suite.namespace).Create(suite.ctx, configMap, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create gateway ConfigMap: %v", err)
	}

	// Create Gateway deployment
	deployment := suite.createGatewayDeployment()
	// Update image reference for our E2E test
	for i := range deployment.Spec.Template.Spec.Containers {
		if deployment.Spec.Template.Spec.Containers[i].Name == "gateway" {
			deployment.Spec.Template.Spec.Containers[i].Image = "mcp-gateway:e2e"
		}
	}

	_, err = suite.k8sClient.AppsV1().Deployments(suite.namespace).Create(suite.ctx, deployment, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create gateway deployment: %v", err)
	}

	// Create Gateway service
	service := suite.createGatewayService()

	_, err = suite.k8sClient.CoreV1().Services(suite.namespace).Create(suite.ctx, service, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create gateway service: %v", err)
	}
}

// deployMCPRouter deploys the MCP Router.
func (suite *WeatherE2ETestSuite) deployMCPRouter(t *testing.T) {
	t.Helper()
	// Create Router ConfigMap
	configMap := suite.createRouterConfigMap()

	_, err := suite.k8sClient.CoreV1().ConfigMaps(suite.namespace).Create(suite.ctx, configMap, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create router ConfigMap: %v", err)
	}

	// Create Router deployment
	deployment := suite.createRouterDeployment()
	// Update image reference for our E2E test
	for i := range deployment.Spec.Template.Spec.Containers {
		if deployment.Spec.Template.Spec.Containers[i].Name == "router" {
			deployment.Spec.Template.Spec.Containers[i].Image = "mcp-router:e2e"
		}
	}

	_, err = suite.k8sClient.AppsV1().Deployments(suite.namespace).Create(suite.ctx, deployment, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create router deployment: %v", err)
	}

	// Create Router service
	service := suite.createRouterService()

	_, err = suite.k8sClient.CoreV1().Services(suite.namespace).Create(suite.ctx, service, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create router service: %v", err)
	}
}

// waitForDeployments waits for all deployments to be ready.
func (suite *WeatherE2ETestSuite) waitForDeployments(t *testing.T) {
	t.Helper()

	deployments := []string{"redis", "weather-mcp-server", "mcp-gateway", "mcp-router"}

	for _, deploymentName := range deployments {
		t.Logf("Waiting for deployment %s to be ready...", deploymentName)

		err := wait.PollUntilContextTimeout(suite.ctx, 10*time.Second, 5*time.Minute, true,
			func(ctx context.Context) (bool, error) {
				deployment, err := suite.k8sClient.AppsV1().Deployments(suite.namespace).Get(
					ctx, deploymentName, metav1.GetOptions{})
				if err != nil {
					t.Logf("Error getting deployment %s: %v", deploymentName, err)

					return false, nil
				}

				return deployment.Status.ReadyReplicas == *deployment.Spec.Replicas, nil
			})
		if err != nil {
			t.Fatalf("Deployment %s did not become ready in time: %v", deploymentName, err)
		}

		t.Logf("✓ Deployment %s is ready", deploymentName)
	}
}

// runClientAgentTest runs the MCP Client Agent test.
func (suite *WeatherE2ETestSuite) runClientAgentTest(t *testing.T) {
	t.Helper()
	// This will run locally but connect to the Router running in the cluster
	// We need to port-forward to the Router service for stdio connection
	// For simplicity in this E2E test, we'll create a test pod that runs the client agent

	// Create a job that runs the MCP Client Agent test
	suite.createClientAgentTestJob(t)

	// Wait for the job to complete and verify results
	suite.waitForJobCompletion(t, "mcp-client-agent-test")
}

// validateCompleteFlow validates the end-to-end flow.
func (suite *WeatherE2ETestSuite) validateCompleteFlow(t *testing.T) {
	t.Helper()
	// Check pod logs to validate the complete flow worked
	pods, err := suite.k8sClient.CoreV1().Pods(suite.namespace).List(suite.ctx, metav1.ListOptions{
		LabelSelector: "app=mcp-client-agent-test",
	})
	if err != nil {
		t.Fatalf("Failed to get test pods: %v", err)
	}

	if len(pods.Items) == 0 {
		t.Fatal("No test pods found")
	}

	// Get logs from the test pod
	podName := pods.Items[0].Name

	logs, err := suite.k8sClient.CoreV1().Pods(suite.namespace).GetLogs(podName, &corev1.PodLogOptions{}).DoRaw(suite.ctx)
	if err != nil {
		t.Fatalf("Failed to get pod logs: %v", err)
	}

	logStr := string(logs)
	t.Logf("Test execution logs:\n%s", logStr)

	// Validate that the logs contain evidence of successful E2E flow
	requiredPatterns := []string{
		"Connected to MCP Router successfully",
		"Received weather data from Open-Meteo API",
		"E2E test completed successfully",
	}

	for _, pattern := range requiredPatterns {
		if !contains(logStr, pattern) {
			t.Errorf("Expected pattern not found in logs: %s", pattern)
		}
	}

	if t.Failed() {
		t.Fatal("E2E flow validation failed")
	}

	t.Log("✅ Complete Agent → Router → Gateway → Weather flow validated successfully")
}

// Helper function to check if a string contains a substring.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > len(substr) && s[:len(substr)] == substr) ||
		(len(s) > len(substr) && s[len(s)-len(substr):] == substr) ||
		containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	if len(substr) == 0 {
		return true
	}

	if len(s) < len(substr) {
		return false
	}

	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}

	return false
}
