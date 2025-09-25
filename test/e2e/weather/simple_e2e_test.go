
// Package weather provides simplified E2E testing for Weather MCP server deployment
package weather

import (
	"context"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// TestWeatherMCPServerDeployment tests just the Weather MCP server deployment and connectivity.
//

func TestWeatherMCPServerDeployment(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping Kubernetes test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	suite := setupE2ETestSuiteWithContext(t, ctx, cancel)
	namespace := "mcp-e2e-test-1754950631" // From current test

	t.Log("✅ Testing deployed Weather MCP Server")

	validateWeatherDeployment(t, ctx, suite.k8sClient, namespace)
	service := getWeatherService(t, ctx, suite.k8sClient, namespace)
	runConnectivityTest(t, ctx, suite, namespace, service.Spec.ClusterIP)

	t.Log("✅ Weather MCP Server deployment and connectivity test completed successfully")
	t.Log("🎉 E2E infrastructure with real Weather MCP server is working!")
}

func setupE2ETestSuiteWithContext(t *testing.T, ctx context.Context, cancel context.CancelFunc) *WeatherE2ETestSuite {
	t.Helper()

	suite := setupE2ETestSuite(t)
	suite.ctx = ctx
	suite.cancelFunc = cancel

	return suite
}

func validateWeatherDeployment(t *testing.T, ctx context.Context, clientset *kubernetes.Clientset, namespace string) {
	t.Helper()

	deployment, err := clientset.AppsV1().Deployments(namespace).Get(ctx, "weather-mcp-server", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get weather deployment: %v", err)
	}

	t.Logf("Weather MCP Server deployment status: %d/%d replicas ready",
		deployment.Status.ReadyReplicas, *deployment.Spec.Replicas)

	if deployment.Status.ReadyReplicas != *deployment.Spec.Replicas {
		t.Fatal("Weather MCP Server deployment is not ready")
	}
}

func getWeatherService(
	t *testing.T, ctx context.Context, clientset *kubernetes.Clientset, namespace string) *corev1.Service {
	t.Helper()

	service, err := clientset.CoreV1().Services(namespace).Get(ctx, "weather-mcp-server", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Failed to get weather service: %v", err)
	}

	t.Logf("Weather MCP Server service: %s:%d", service.Spec.ClusterIP, service.Spec.Ports[0].Port)

	return service
}

func runConnectivityTest(t *testing.T, ctx context.Context, suite *WeatherE2ETestSuite, namespace, serviceIP string) {
	t.Helper()

	t.Log("Creating test pod to verify Weather MCP Server connectivity...")

	testPodName := "weather-connectivity-test"
	testPod := suite.createConnectivityTestPod(testPodName, namespace, serviceIP)

	_, err := suite.k8sClient.CoreV1().Pods(namespace).Create(ctx, testPod, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create test pod: %v", err)
	}

	waitForConnectivityTest(t, ctx, suite.k8sClient, namespace, testPodName)
}

func waitForConnectivityTest(
	t *testing.T, ctx context.Context, clientset *kubernetes.Clientset, namespace, testPodName string) {
	t.Helper()

	// Wait for test pod to complete
	t.Log("Waiting for connectivity test to complete...")
	time.Sleep(30 * time.Second)

	// Get test results from pod logs
	logs, err := clientset.CoreV1().Pods(namespace).GetLogs(testPodName, &corev1.PodLogOptions{}).DoRaw(ctx)
	if err != nil {
		t.Fatalf("Failed to get test pod logs: %v", err)
	}

	logStr := string(logs)
	t.Logf("Connectivity test results:\n%s", logStr)

	// Validate results
	if !strings.Contains(logStr, "Weather MCP Server is accessible") {
		t.Error("Weather MCP Server connectivity test failed")
	}

	if !strings.Contains(logStr, "Health check passed") {
		t.Error("Weather MCP Server health check failed")
	}
}
