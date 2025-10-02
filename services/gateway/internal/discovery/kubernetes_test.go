//nolint:staticcheck // SA1019: Using deprecated v1.Endpoints API which is still widely used in k8s clusters.
package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/actual-software/mcp-bridge/services/gateway/internal/config"
	"github.com/actual-software/mcp-bridge/services/gateway/test/testutil"
)

const providerKubernetes = "kubernetes"

//nolint:ireturn // Test helper returns kubernetes interface
func createKubernetesTestClient(t *testing.T, kubeconfig string) kubernetes.Interface {
	t.Helper()

	// Create Kubernetes client using the test cluster
	k8sConfig, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		t.Fatalf("Failed to build kubeconfig: %v", err)
	}

	client, err := kubernetes.NewForConfig(k8sConfig)
	if err != nil {
		t.Fatalf("Failed to create Kubernetes client: %v", err)
	}

	return client
}

func testBasicKubernetesConnectivity(t *testing.T, client kubernetes.Interface) {
	t.Helper()

	// Test basic Kubernetes connectivity
	nodes, err := client.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("Failed to list nodes: %v", err)
	}

	assert.NotEmpty(t, nodes.Items, "Should have at least one node")
	t.Logf("Found %d nodes in the cluster", len(nodes.Items))
}

func testNamespaceOperations(t *testing.T, client kubernetes.Interface) {
	t.Helper()

	// Test namespace operations
	namespace := &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-mcp-namespace",
		},
	}

	// Create test namespace
	_, err := client.CoreV1().Namespaces().Create(context.Background(), namespace, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create test namespace: %v", err)
	}

	// List namespaces to verify creation
	namespaces, err := client.CoreV1().Namespaces().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("Failed to list namespaces: %v", err)
	}

	found := false

	for _, ns := range namespaces.Items {
		if ns.Name == "test-mcp-namespace" {
			found = true

			break
		}
	}

	assert.True(t, found, "Test namespace should be created")

	// Create a test service in the namespace so discovery can find it
	service := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service",
			Namespace: "test-mcp-namespace",
		},
		Spec: v1.ServiceSpec{
			Ports: []v1.ServicePort{
				{
					Name:     "http",
					Port:     8080,
					Protocol: v1.ProtocolTCP,
				},
			},
			Selector: map[string]string{
				"app": "test",
			},
		},
	}

	_, err = client.CoreV1().Services("test-mcp-namespace").Create(context.Background(), service, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create test service: %v", err)
	}

	// Create endpoints for the service
	endpoints := &v1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service",
			Namespace: "test-mcp-namespace",
		},
		Subsets: []v1.EndpointSubset{
			{
				Addresses: []v1.EndpointAddress{
					{
						IP: "10.0.0.1",
					},
				},
				Ports: []v1.EndpointPort{
					{
						Name:     "http",
						Port:     8080,
						Protocol: v1.ProtocolTCP,
					},
				},
			},
		},
	}

	_, err = client.CoreV1().Endpoints("test-mcp-namespace").Create(
		context.Background(),
		endpoints,
		metav1.CreateOptions{},
	)
	if err != nil {
		t.Fatalf("Failed to create test endpoints: %v", err)
	}

	// Cleanup namespace

	defer func() {
		err = client.CoreV1().Namespaces().Delete(context.Background(), "test-mcp-namespace", metav1.DeleteOptions{})
		if err != nil {
			t.Errorf("Failed to delete test namespace: %v", err)
		}
	}()
}

func testKubernetesDiscoveryIntegration(t *testing.T, client kubernetes.Interface, kubeconfig string) {
	t.Helper()

	// Test KubernetesDiscovery integration with real cluster
	cfg := config.ServiceDiscoveryConfig{
		Provider:          providerKubernetes,
		NamespaceSelector: []string{"test-mcp-namespace"},
		Kubernetes: config.KubernetesDiscoveryConfig{
			ConfigPath: kubeconfig,
		},
	}

	logger := testutil.NewTestLogger(t)

	// Create discovery instance with real client
	discoveryCtx, discoveryCancel := context.WithCancel(context.Background())
	discovery := &KubernetesDiscovery{
		config:    cfg,
		logger:    logger,
		client:    client,
		endpoints: make(map[string][]Endpoint),
		ctx:       discoveryCtx,
		cancel:    discoveryCancel,
	}

	// Test discovery operations
	testCtx, testCancel := context.WithTimeout(context.Background(), 30*time.Second)

	defer testCancel()

	err := discovery.Start(testCtx)
	if err != nil {
		t.Fatalf("Failed to start discovery: %v", err)
	}

	// Give discovery time to initialize
	time.Sleep(2 * time.Second)

	// Test namespace listing
	namespaceList := discovery.ListNamespaces()

	assert.Contains(t, namespaceList, "test-mcp-namespace", "Discovery should find our test namespace")
	t.Logf("Discovery found namespaces: %v", namespaceList)

	// Cleanup
	discovery.Stop()
}

func TestNewKubernetesDiscovery(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping Kubernetes integration test in short mode")
	}

	// Setup Kubernetes infrastructure
	cleanup, kubeconfig, err := setupKubernetesInfrastructure(t)
	if err != nil {
		t.Skipf("Skipping test: %v", err)
	}

	defer cleanup()

	client := createKubernetesTestClient(t, kubeconfig)
	testBasicKubernetesConnectivity(t, client)
	testNamespaceOperations(t, client)
	testKubernetesDiscoveryIntegration(t, client, kubeconfig)
}

func TestKubernetesDiscovery_DiscoverNamespaceServices(t *testing.T) {
	logger := testutil.NewTestLogger(t)
	tests := createKubernetesNamespaceServiceTests()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runNamespaceServiceTest(t, tt, logger)
		})
	}
}

func createKubernetesNamespaceServiceTests() []struct {
	name              string
	namespace         string
	services          []runtime.Object
	endpoints         []runtime.Object
	config            config.ServiceDiscoveryConfig
	expectedEndpoints int
	expectedTools     int
} {
	enabledTests := createEnabledServiceTests()
	disabledTests := createDisabledServiceTests()
	invalidTests := createInvalidServiceTests()
	multipleTests := createMultipleServiceTests()

	var allTests []struct {
		name              string
		namespace         string
		services          []runtime.Object
		endpoints         []runtime.Object
		config            config.ServiceDiscoveryConfig
		expectedEndpoints int
		expectedTools     int
	}

	allTests = append(allTests, enabledTests...)
	allTests = append(allTests, disabledTests...)
	allTests = append(allTests, invalidTests...)
	allTests = append(allTests, multipleTests...)

	return allTests
}

func createEnabledServiceTests() []struct {
	name              string
	namespace         string
	services          []runtime.Object
	endpoints         []runtime.Object
	config            config.ServiceDiscoveryConfig
	expectedEndpoints int
	expectedTools     int
} {
	return []struct {
		name              string
		namespace         string
		services          []runtime.Object
		endpoints         []runtime.Object
		config            config.ServiceDiscoveryConfig
		expectedEndpoints int
		expectedTools     int
	}{
		createEnabledServiceTestCase(),
	}
}

func createDisabledServiceTests() []struct {
	name              string
	namespace         string
	services          []runtime.Object
	endpoints         []runtime.Object
	config            config.ServiceDiscoveryConfig
	expectedEndpoints int
	expectedTools     int
} {
	return []struct {
		name              string
		namespace         string
		services          []runtime.Object
		endpoints         []runtime.Object
		config            config.ServiceDiscoveryConfig
		expectedEndpoints int
		expectedTools     int
	}{
		{
			name:      "Skip non-MCP enabled service",
			namespace: "test-namespace",
			services: []runtime.Object{
				&v1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "regular-service",
						Namespace: "test-namespace",
						Labels: map[string]string{
							"app": "mcp",
						},
						Annotations: map[string]string{
							"mcp.bridge/enabled": "false",
						},
					},
				},
			},
			config: config.ServiceDiscoveryConfig{
				LabelSelector: map[string]string{"app": "mcp"},
			},
			expectedEndpoints: 0,
		},
	}
}

func createInvalidServiceTests() []struct {
	name              string
	namespace         string
	services          []runtime.Object
	endpoints         []runtime.Object
	config            config.ServiceDiscoveryConfig
	expectedEndpoints int
	expectedTools     int
} {
	var tests []struct {
		name              string
		namespace         string
		services          []runtime.Object
		endpoints         []runtime.Object
		config            config.ServiceDiscoveryConfig
		expectedEndpoints int
		expectedTools     int
	}

	tests = append(tests, createMissingAnnotationTests()...)
	tests = append(tests, createInvalidToolsJSONTests()...)

	return tests
}

func createMissingAnnotationTests() []struct {
	name              string
	namespace         string
	services          []runtime.Object
	endpoints         []runtime.Object
	config            config.ServiceDiscoveryConfig
	expectedEndpoints int
	expectedTools     int
} {
	return []struct {
		name              string
		namespace         string
		services          []runtime.Object
		endpoints         []runtime.Object
		config            config.ServiceDiscoveryConfig
		expectedEndpoints int
		expectedTools     int
	}{
		{
			name:      "Service missing MCP namespace annotation",
			namespace: "test-namespace",
			services: []runtime.Object{
				&v1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "incomplete-service",
						Namespace: "test-namespace",
						Labels: map[string]string{
							"app": "mcp",
						},
						Annotations: map[string]string{
							"mcp.bridge/enabled": "true",
							// Missing namespace annotation
						},
					},
				},
			},
			config: config.ServiceDiscoveryConfig{
				LabelSelector: map[string]string{"app": "mcp"},
			},
			expectedEndpoints: 0,
		},
	}
}

func createInvalidToolsJSONTests() []struct {
	name              string
	namespace         string
	services          []runtime.Object
	endpoints         []runtime.Object
	config            config.ServiceDiscoveryConfig
	expectedEndpoints int
	expectedTools     int
} {
	return []struct {
		name              string
		namespace         string
		services          []runtime.Object
		endpoints         []runtime.Object
		config            config.ServiceDiscoveryConfig
		expectedEndpoints int
		expectedTools     int
	}{
		{
			name:      "Invalid tools JSON",
			namespace: "test-namespace",
			services: []runtime.Object{
				&v1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "bad-tools-service",
						Namespace: "test-namespace",
						Labels: map[string]string{
							"app": "mcp",
						},
						Annotations: map[string]string{
							"mcp.bridge/enabled":   "true",
							"mcp.bridge/namespace": "mcp-namespace",
							"mcp.bridge/tools":     `invalid json`,
						},
					},
				},
			},
			endpoints: []runtime.Object{
				&v1.Endpoints{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "bad-tools-service",
						Namespace: "test-namespace",
					},
					Subsets: []v1.EndpointSubset{
						{
							Addresses: []v1.EndpointAddress{{IP: "10.0.0.1"}},
							Ports:     []v1.EndpointPort{{Name: "mcp", Port: 8080}},
						},
					},
				},
			},
			config: config.ServiceDiscoveryConfig{
				LabelSelector: map[string]string{"app": "mcp"},
			},
			expectedEndpoints: 1,
			expectedTools:     0,
		},
	}
}

func createMultipleServiceTests() []struct {
	name              string
	namespace         string
	services          []runtime.Object
	endpoints         []runtime.Object
	config            config.ServiceDiscoveryConfig
	expectedEndpoints int
	expectedTools     int
} {
	return []struct {
		name              string
		namespace         string
		services          []runtime.Object
		endpoints         []runtime.Object
		config            config.ServiceDiscoveryConfig
		expectedEndpoints int
		expectedTools     int
	}{
		{
			name:              "Multiple services with different weights",
			namespace:         "test-namespace",
			services:          createMultipleServices(),
			endpoints:         createMultipleEndpoints(),
			config:            createMultipleServiceConfig(),
			expectedEndpoints: 2,
		},
	}
}

func createMultipleServices() []runtime.Object {
	return []runtime.Object{
		&v1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "service-1",
				Namespace: "test-namespace",
				Labels:    map[string]string{"app": "mcp"},
				Annotations: map[string]string{
					"mcp.bridge/enabled":   "true",
					"mcp.bridge/namespace": "mcp-namespace",
					"mcp.bridge/weight":    "200",
				},
			},
		},
		&v1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "service-2",
				Namespace: "test-namespace",
				Labels:    map[string]string{"app": "mcp"},
				Annotations: map[string]string{
					"mcp.bridge/enabled":   "true",
					"mcp.bridge/namespace": "mcp-namespace",
					// No weight specified, should use default testIterations
				},
			},
		},
	}
}

func createMultipleEndpoints() []runtime.Object {
	return []runtime.Object{
		&v1.Endpoints{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "service-1",
				Namespace: "test-namespace",
			},
			Subsets: []v1.EndpointSubset{
				{
					Addresses: []v1.EndpointAddress{{IP: "10.0.0.1"}},
					Ports:     []v1.EndpointPort{{Name: "mcp", Port: 8080}},
				},
			},
		},
		&v1.Endpoints{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "service-2",
				Namespace: "test-namespace",
			},
			Subsets: []v1.EndpointSubset{
				{
					Addresses: []v1.EndpointAddress{{IP: "10.0.0.2"}},
					Ports:     []v1.EndpointPort{{Name: "mcp", Port: 8080}},
				},
			},
		},
	}
}

func createMultipleServiceConfig() config.ServiceDiscoveryConfig {
	return config.ServiceDiscoveryConfig{
		LabelSelector: map[string]string{"app": "mcp"},
	}
}

// runNamespaceServiceTest runs a single namespace service discovery test.
func runNamespaceServiceTest(t *testing.T, tt struct {
	name              string
	namespace         string
	services          []runtime.Object
	endpoints         []runtime.Object
	config            config.ServiceDiscoveryConfig
	expectedEndpoints int
	expectedTools     int
}, logger *zap.Logger,
) {
	t.Helper()
	// Setup test client and discovery
	fakeClient := setupTestClient(tt.services, tt.endpoints)
	discovery := setupTestDiscovery(tt.config, logger, fakeClient)

	// Run discovery
	endpoints, err := discovery.discoverNamespaceServices(tt.namespace)
	if err != nil {
		t.Fatalf("Failed to discover services: %v", err)
	}

	// Verify results
	verifyEndpointCount(t, len(endpoints), tt.expectedEndpoints)
	verifyEndpointDetails(t, endpoints, tt.expectedTools, tt.name)
}

// setupTestClient creates a fake Kubernetes client for testing.
func setupTestClient(services, endpoints []runtime.Object) *fake.Clientset {
	objects := make([]runtime.Object, 0, len(services)+len(endpoints))
	objects = append(objects, services...)
	objects = append(objects, endpoints...)

	return fake.NewSimpleClientset(objects...)
}

// setupTestDiscovery creates a test KubernetesDiscovery instance.
func setupTestDiscovery(
	config config.ServiceDiscoveryConfig,
	logger *zap.Logger,
	client *fake.Clientset,
) *KubernetesDiscovery {
	ctx, cancel := context.WithCancel(context.Background())

	return &KubernetesDiscovery{
		config:    config,
		logger:    logger,
		client:    client,
		endpoints: make(map[string][]Endpoint),
		ctx:       ctx,
		cancel:    cancel,
	}
}

// verifyEndpointCount checks if the expected number of endpoints were discovered.
func verifyEndpointCount(t *testing.T, actual, expected int) {
	t.Helper()

	if actual != expected {
		t.Errorf("Expected %d endpoints, got %d", expected, actual)
	}
}

// verifyEndpointDetails verifies the details of discovered endpoints.
func verifyEndpointDetails(t *testing.T, endpoints []Endpoint, expectedTools int, testName string) {
	t.Helper()

	for _, ep := range endpoints {
		verifyBasicEndpointFields(t, ep)
		verifyEndpointTools(t, ep, expectedTools)
		verifyEndpointWeights(t, ep, testName)
	}
}

// verifyBasicEndpointFields checks basic endpoint fields.
func verifyBasicEndpointFields(t *testing.T, ep Endpoint) {
	t.Helper()

	if ep.Namespace == "" {
		t.Error("Endpoint missing namespace")
	}

	if ep.Address == "" {
		t.Error("Endpoint missing address")
	}

	if ep.Port == 0 {
		t.Error("Endpoint missing port")
	}

	if !ep.Healthy {
		t.Error("Expected endpoint to be healthy")
	}
}

// verifyEndpointTools checks endpoint tools count.
func verifyEndpointTools(t *testing.T, ep Endpoint, expectedTools int) {
	t.Helper()

	if expectedTools > 0 && len(ep.Tools) != expectedTools {
		t.Errorf("Expected %d tools, got %d", expectedTools, len(ep.Tools))
	}
}

// verifyEndpointWeights checks endpoint weights for specific test cases.
func verifyEndpointWeights(t *testing.T, ep Endpoint, testName string) {
	t.Helper()

	if testName != "Multiple services with different weights" {
		return
	}

	if ep.Service == "service-1" && ep.Weight != 200 {
		t.Errorf("Expected weight 200 for service-1, got %d", ep.Weight)
	}

	if ep.Service == "service-2" && ep.Weight != 100 {
		t.Errorf("Expected default weight 100 for service-2, got %d", ep.Weight)
	}
}

func TestKubernetesDiscovery_GetEndpoints(t *testing.T) {
	logger := testutil.NewTestLogger(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	discovery := &KubernetesDiscovery{
		logger: logger,
		endpoints: map[string][]Endpoint{
			"namespace1": {
				{Service: "service1", Namespace: "namespace1", Address: "10.0.0.1", Port: 8080},
				{Service: "service2", Namespace: "namespace1", Address: "10.0.0.2", Port: 8080},
			},
			"namespace2": {
				{Service: "service3", Namespace: "namespace2", Address: "10.0.0.3", Port: 8080},
			},
		},
		mu:     sync.RWMutex{},
		ctx:    ctx,
		cancel: cancel,
	}

	tests := []struct {
		name              string
		namespace         string
		expectedEndpoints int
	}{
		{
			name:              "Get endpoints for existing namespace",
			namespace:         "namespace1",
			expectedEndpoints: 2,
		},
		{
			name:              "Get endpoints for another namespace",
			namespace:         "namespace2",
			expectedEndpoints: 1,
		},
		{
			name:              "Get endpoints for non-existent namespace",
			namespace:         "namespace3",
			expectedEndpoints: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			endpoints := discovery.GetEndpoints(tt.namespace)
			if len(endpoints) != tt.expectedEndpoints {
				t.Errorf("Expected %d endpoints, got %d", tt.expectedEndpoints, len(endpoints))
			}
		})
	}
}

func TestKubernetesDiscovery_GetAllEndpoints(t *testing.T) {
	logger := testutil.NewTestLogger(t)

	originalEndpoints := map[string][]Endpoint{
		"namespace1": {
			{Service: "service1", Namespace: "namespace1", Address: "10.0.0.1", Port: 8080},
		},
		"namespace2": {
			{Service: "service2", Namespace: "namespace2", Address: "10.0.0.2", Port: 8080},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	discovery := &KubernetesDiscovery{
		logger:    logger,
		endpoints: originalEndpoints,
		mu:        sync.RWMutex{},
		ctx:       ctx,
		cancel:    cancel,
	}

	allEndpoints := discovery.GetAllEndpoints()

	// Verify we got a copy
	if len(allEndpoints) != 2 {
		t.Errorf("Expected 2 namespaces, got %d", len(allEndpoints))
	}

	// Modify the returned map and verify original is unchanged
	allEndpoints["namespace3"] = []Endpoint{{Service: "service3"}}

	if len(discovery.endpoints) != 2 {
		t.Error("Original endpoints were modified")
	}
}

func TestKubernetesDiscovery_ListNamespaces(t *testing.T) {
	logger := testutil.NewTestLogger(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	discovery := &KubernetesDiscovery{
		logger: logger,
		endpoints: map[string][]Endpoint{
			"namespace1": {{Service: "service1"}},
			"namespace2": {{Service: "service2"}},
			"namespace3": {{Service: "service3"}},
		},
		mu:     sync.RWMutex{},
		ctx:    ctx,
		cancel: cancel,
	}

	namespaces := discovery.ListNamespaces()

	if len(namespaces) != 3 {
		t.Errorf("Expected 3 namespaces, got %d", len(namespaces))
	}

	// Check all namespaces are present
	namespaceMap := make(map[string]bool)

	for _, ns := range namespaces {
		namespaceMap[ns] = true
	}

	for _, expected := range []string{"namespace1", "namespace2", "namespace3"} {
		if !namespaceMap[expected] {
			t.Errorf("Missing namespace: %s", expected)
		}
	}
}

func TestKubernetesDiscovery_WatchEvents(t *testing.T) {
	logger := testutil.NewTestLogger(t)
	fakeClient := fake.NewSimpleClientset()
	fakeWatcher := watch.NewFake()
	fakeClient.PrependWatchReactor("services", k8stesting.DefaultWatchReactor(fakeWatcher, nil))

	ctx, cancel := context.WithCancel(context.Background())

	defer cancel()

	discovery := createKubernetesDiscoveryForWatch(ctx, cancel, logger, fakeClient)
	eventChan := make(chan watch.Event)

	startWatching(discovery, eventChan)

	testService := createTestServiceForWatch()
	testServiceEventHandling(eventChan, testService)
	stopWatching(cancel, eventChan, discovery)
}

func createKubernetesDiscoveryForWatch(
	ctx context.Context,
	cancel context.CancelFunc,
	logger *zap.Logger,
	fakeClient *fake.Clientset,
) *KubernetesDiscovery {
	return &KubernetesDiscovery{
		config: config.ServiceDiscoveryConfig{
			NamespaceSelector: []string{"test-namespace"},
			LabelSelector:     map[string]string{"app": "mcp"},
		},
		logger:    logger,
		client:    fakeClient,
		endpoints: make(map[string][]Endpoint),
		ctx:       ctx,
		cancel:    cancel,
		wg:        sync.WaitGroup{},
	}
}

func startWatching(discovery *KubernetesDiscovery, eventChan chan watch.Event) {
	discovery.wg.Add(1)

	go func() {
		defer discovery.wg.Done()

		discovery.handleServiceEvents("test-namespace", eventChan)
	}()
}

func createTestServiceForWatch() *v1.Service {
	return &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service",
			Namespace: "test-namespace",
			Labels:    map[string]string{"app": "mcp"},
			Annotations: map[string]string{
				"mcp.bridge/enabled":   "true",
				"mcp.bridge/namespace": "mcp-namespace",
			},
		},
	}
}

func testServiceEventHandling(eventChan chan watch.Event, testService *v1.Service) {
	eventChan <- watch.Event{Type: watch.Added, Object: testService}

	time.Sleep(100 * time.Millisecond)

	modifiedService := testService.DeepCopy()

	modifiedService.Annotations["mcp.bridge/weight"] = "200"
	eventChan <- watch.Event{Type: watch.Modified, Object: modifiedService}

	time.Sleep(100 * time.Millisecond)

	eventChan <- watch.Event{Type: watch.Deleted, Object: testService}

	time.Sleep(100 * time.Millisecond)
}

func stopWatching(cancel context.CancelFunc, eventChan chan watch.Event, discovery *KubernetesDiscovery) {
	cancel()
	close(eventChan)
	discovery.wg.Wait()
}

func TestKubernetesDiscovery_PeriodicResync(t *testing.T) {
	logger := testutil.NewTestLogger(t)
	fakeClient := fake.NewSimpleClientset()

	ctx, cancel := context.WithCancel(context.Background())

	discovery := &KubernetesDiscovery{
		config: config.ServiceDiscoveryConfig{
			NamespaceSelector: []string{"test-namespace"},
			LabelSelector:     map[string]string{"app": "mcp"},
		},
		logger:    logger,
		client:    fakeClient,
		endpoints: make(map[string][]Endpoint),
		ctx:       ctx,
		cancel:    cancel,
		wg:        sync.WaitGroup{},
	}

	// Track resync through log messages or other observable behavior

	// Start periodic resync with a short interval for testing
	discovery.wg.Add(1)

	go func() {
		defer discovery.wg.Done()

		ticker := time.NewTicker(100 * time.Millisecond)

		defer ticker.Stop()

		for {
			select {
			case <-discovery.ctx.Done():
				return
			case <-ticker.C:
				_ = discovery.discoverServices()
			}
		}
	}()

	// Wait for at least 2 resyncs
	time.Sleep(250 * time.Millisecond)

	// Stop the resync
	cancel()
	discovery.wg.Wait()
}

func TestKubernetesDiscovery_StartStop(t *testing.T) {
	logger := testutil.NewTestLogger(t)
	testService, testEndpoint := createStartStopTestObjects()
	fakeClient := createStartStopFakeClient(testService, testEndpoint)

	ctx, cancel := context.WithCancel(context.Background())

	defer cancel()

	discovery := createStartStopDiscovery(ctx, cancel, logger, fakeClient)

	testDiscoveryStartStop(t, discovery)
}

func createStartStopTestObjects() (*v1.Service, *v1.Endpoints) {
	testService := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service",
			Namespace: "test-namespace",
			Labels:    map[string]string{"app": "mcp"},
			Annotations: map[string]string{
				"mcp.bridge/enabled":   "true",
				"mcp.bridge/namespace": "mcp-namespace",
			},
		},
	}
	testEndpoint := &v1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service",
			Namespace: "test-namespace",
		},
		Subsets: []v1.EndpointSubset{
			{
				Addresses: []v1.EndpointAddress{{IP: "10.0.0.1"}},
				Ports:     []v1.EndpointPort{{Name: "mcp", Port: 8080}},
			},
		},
	}

	return testService, testEndpoint
}

func createStartStopFakeClient(testService *v1.Service, testEndpoint *v1.Endpoints) *fake.Clientset {
	fakeClient := fake.NewSimpleClientset(testService, testEndpoint)
	fakeServiceWatcher := watch.NewFake()
	fakeEndpointWatcher := watch.NewFake()

	fakeClient.PrependWatchReactor("services", k8stesting.DefaultWatchReactor(fakeServiceWatcher, nil))
	fakeClient.PrependWatchReactor(
		"endpoints",
		k8stesting.DefaultWatchReactor(fakeEndpointWatcher, nil),
	)

	return fakeClient
}

func createStartStopDiscovery(
	ctx context.Context,
	cancel context.CancelFunc,
	logger *zap.Logger,
	fakeClient *fake.Clientset,
) *KubernetesDiscovery {
	return &KubernetesDiscovery{
		config: config.ServiceDiscoveryConfig{
			NamespaceSelector: []string{"test-namespace"},
			LabelSelector:     map[string]string{"app": "mcp"},
		},
		logger:    logger,
		client:    fakeClient,
		endpoints: make(map[string][]Endpoint),
		ctx:       ctx,
		cancel:    cancel,
		wg:        sync.WaitGroup{},
	}
}

func testDiscoveryStartStop(t *testing.T, discovery *KubernetesDiscovery) {
	t.Helper()

	err := discovery.Start(context.Background())
	if err != nil {
		t.Fatalf("Failed to start discovery: %v", err)
	}

	time.Sleep(100 * time.Millisecond) // Give some time for initialization

	endpoints := discovery.GetEndpoints("mcp-namespace")
	if len(endpoints) != 1 {
		t.Errorf("Expected 1 endpoint, got %d", len(endpoints))
	}

	discovery.Stop()

	select {
	case <-time.After(1 * time.Second):
		t.Error("Discovery did not shut down cleanly")
	default:
	}
}

func TestEndpoint_Serialization(t *testing.T) {
	endpoint := Endpoint{
		Service:   "test-service",
		Namespace: "test-namespace",
		Address:   "10.0.0.1",
		Port:      8080,
		Weight:    150,
		Metadata: map[string]string{
			"k8s_namespace": "k8s-namespace",
			"k8s_service":   "k8s-service",
		},
		Tools: []ToolInfo{
			{Name: "tool1", Description: "Tool 1 description"},
			{Name: "tool2", Description: "Tool 2 description"},
		},
		Healthy: true,
	}

	// Test JSON serialization
	data, err := json.Marshal(endpoint)
	if err != nil {
		t.Fatalf("Failed to marshal endpoint: %v", err)
	}

	// Test JSON deserialization
	var decoded Endpoint

	err = json.Unmarshal(data, &decoded)
	if err != nil {
		t.Fatalf("Failed to unmarshal endpoint: %v", err)
	}

	// Verify fields
	if decoded.Service != endpoint.Service {
		t.Errorf("Service mismatch: expected %s, got %s", endpoint.Service, decoded.Service)
	}

	if decoded.Port != endpoint.Port {
		t.Errorf("Port mismatch: expected %d, got %d", endpoint.Port, decoded.Port)
	}

	if len(decoded.Tools) != len(endpoint.Tools) {
		t.Errorf("Tools count mismatch: expected %d, got %d", len(endpoint.Tools), len(decoded.Tools))
	}
}

// Mock implementation for testing.
type MockServiceDiscovery struct {
	endpoints map[string][]Endpoint
	mu        sync.RWMutex
	started   bool
	stopped   bool
}

func NewMockServiceDiscovery() *MockServiceDiscovery {
	return &MockServiceDiscovery{
		endpoints: make(map[string][]Endpoint),
	}
}

func (m *MockServiceDiscovery) Start(_ context.Context) error {
	m.mu.Lock()

	defer m.mu.Unlock()

	if m.started {
		return errors.New("already started")
	}

	m.started = true

	return nil
}

func (m *MockServiceDiscovery) Stop() {
	m.mu.Lock()

	defer m.mu.Unlock()

	m.stopped = true
}

func (m *MockServiceDiscovery) GetEndpoints(namespace string) []Endpoint {
	m.mu.RLock()

	defer m.mu.RUnlock()

	return m.endpoints[namespace]
}

func (m *MockServiceDiscovery) GetAllEndpoints() map[string][]Endpoint {
	m.mu.RLock()

	defer m.mu.RUnlock()

	result := make(map[string][]Endpoint)

	for k, v := range m.endpoints {
		result[k] = v
	}

	return result
}

func (m *MockServiceDiscovery) ListNamespaces() []string {
	m.mu.RLock()

	defer m.mu.RUnlock()

	namespaces := make([]string, 0, len(m.endpoints))

	for ns := range m.endpoints {
		namespaces = append(namespaces, ns)
	}

	return namespaces
}

func (m *MockServiceDiscovery) AddEndpoint(namespace string, endpoint Endpoint) {
	m.mu.Lock()

	defer m.mu.Unlock()

	m.endpoints[namespace] = append(m.endpoints[namespace], endpoint)
}

func TestMockServiceDiscovery(t *testing.T) {
	mock := NewMockServiceDiscovery()

	// Test Start
	err := mock.Start(context.Background())
	if err != nil {
		t.Errorf("Failed to start: %v", err)
	}

	// Test double start
	err = mock.Start(context.Background())
	if err == nil {
		t.Error("Expected error on double start")
	}

	// Add test endpoints
	mock.AddEndpoint("ns1", Endpoint{Service: "service1", Address: "10.0.0.1"})
	mock.AddEndpoint("ns1", Endpoint{Service: "service2", Address: "10.0.0.2"})
	mock.AddEndpoint("ns2", Endpoint{Service: "service3", Address: "10.0.0.3"})

	// Test GetEndpoints
	endpoints := mock.GetEndpoints("ns1")
	if len(endpoints) != 2 {
		t.Errorf("Expected 2 endpoints, got %d", len(endpoints))
	}

	// Test ListNamespaces
	namespaces := mock.ListNamespaces()
	if len(namespaces) != 2 {
		t.Errorf("Expected 2 namespaces, got %d", len(namespaces))
	}

	// Test Stop
	mock.Stop()

	if !mock.stopped {
		t.Error("Expected service to be stopped")
	}
}

// createEnabledServiceTestCase creates a test case for MCP enabled service.
func createEnabledServiceTestCase() struct {
	name              string
	namespace         string
	services          []runtime.Object
	endpoints         []runtime.Object
	config            config.ServiceDiscoveryConfig
	expectedEndpoints int
	expectedTools     int
} {
	return struct {
		name              string
		namespace         string
		services          []runtime.Object
		endpoints         []runtime.Object
		config            config.ServiceDiscoveryConfig
		expectedEndpoints int
		expectedTools     int
	}{
		name:              "Discover MCP enabled service",
		namespace:         "test-namespace",
		services:          createEnabledTestServices(),
		endpoints:         createEnabledTestEndpoints(),
		config:            createEnabledTestConfig(),
		expectedEndpoints: 2,
		expectedTools:     1,
	}
}

func createEnabledTestServices() []runtime.Object {
	return []runtime.Object{
		&v1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "mcp-service",
				Namespace:   "test-namespace",
				Labels:      map[string]string{"app": "mcp"},
				Annotations: createEnabledServiceAnnotations(),
			},
		},
	}
}

func createEnabledServiceAnnotations() map[string]string {
	return map[string]string{
		"mcp.bridge/enabled":   "true",
		"mcp.bridge/namespace": "mcp-namespace",
		"mcp.bridge/weight":    "150",
		"mcp.bridge/tools":     `[{"name":"tool1","description":"Test tool"}]`,
	}
}

func createEnabledTestEndpoints() []runtime.Object {
	return []runtime.Object{
		&v1.Endpoints{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "mcp-service",
				Namespace: "test-namespace",
			},
			Subsets: createEnabledEndpointSubsets(),
		},
	}
}

func createEnabledEndpointSubsets() []v1.EndpointSubset {
	return []v1.EndpointSubset{
		{
			Addresses: []v1.EndpointAddress{
				{IP: "10.0.0.1"},
				{IP: "10.0.0.2"},
			},
			Ports: []v1.EndpointPort{
				{Name: "mcp", Port: 8080},
				{Name: "metrics", Port: 9090},
			},
		},
	}
}

func createEnabledTestConfig() config.ServiceDiscoveryConfig {
	return config.ServiceDiscoveryConfig{
		LabelSelector: map[string]string{"app": "mcp"},
	}
}

// setupKubernetesInfrastructure creates a local Kubernetes cluster for testing.
func setupKubernetesInfrastructure(t *testing.T) (cleanup func(), kubeconfig string, err error) {
	t.Helper()

	ctx := context.Background() // Test infrastructure context

	// Check if Docker is available
	if !isDockerAvailable() {
		return nil, "", errors.New("Docker is not available for Kubernetes setup")
	}

	// Check if kind is available
	if !isKindAvailable() {
		return nil, "", errors.New("kind (Kubernetes in Docker) is not available")
	}

	// Create unique cluster name
	clusterName := fmt.Sprintf("test-mcp-%d", time.Now().Unix())

	// Create kind cluster

	// #nosec G204 - test code using controlled inputs
	createCmd := exec.CommandContext(ctx, "kind", "create", "cluster", "--name", clusterName, "--wait", "60s")

	createOutput, err := createCmd.CombinedOutput()
	if err != nil {
		return nil, "", fmt.Errorf("failed to create kind cluster: %w, output: %s", err, string(createOutput))
	}

	t.Logf("Created kind cluster: %s", clusterName)

	// Get kubeconfig path
	kubeconfigPath := filepath.Join(os.TempDir(), "kubeconfig-"+clusterName)

	// Export kubeconfig

	// #nosec G204 - test code using controlled inputs
	exportCmd := exec.CommandContext(ctx, "kind", "export", "kubeconfig",
		"--name", clusterName, "--kubeconfig", kubeconfigPath)

	exportOutput, err := exportCmd.CombinedOutput()
	if err != nil {
		// Cleanup on failure
		// #nosec G204 - test cleanup code
		_ = exec.CommandContext(ctx, "kind", "delete", "cluster", "--name", clusterName).Run()

		return nil, "", fmt.Errorf("failed to export kubeconfig: %w, output: %s", err, string(exportOutput))
	}

	// Wait for cluster to be ready
	if err := waitForKubernetesCluster(kubeconfigPath, 60*time.Second); err != nil {
		// Cleanup on failure
		// #nosec G204 - test cleanup code
		_ = exec.CommandContext(ctx, "kind", "delete", "cluster", "--name", clusterName).Run()
		_ = os.Remove(kubeconfigPath)

		return nil, "", fmt.Errorf("Kubernetes cluster failed to become ready: %w", err)
	}

	t.Logf("Kubernetes cluster is ready, kubeconfig: %s", kubeconfigPath)

	cleanup = func() {
		t.Logf("Cleaning up kind cluster: %s", clusterName)

		// #nosec G204 - test cleanup code with controlled inputs
		deleteCmd := exec.CommandContext(ctx, "kind", "delete", "cluster", "--name", clusterName)
		if output, err := deleteCmd.CombinedOutput(); err != nil {
			t.Logf("Failed to delete kind cluster: %v, output: %s", err, string(output))
		}

		// Remove kubeconfig file
		if err := os.Remove(kubeconfigPath); err != nil {
			t.Logf("Failed to remove kubeconfig file: %v", err)
		}
	}

	return cleanup, kubeconfigPath, nil
}

// isKindAvailable checks if kind is available.
func isKindAvailable() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

	defer cancel()

	cmd := exec.CommandContext(ctx, "kind", "version")

	return cmd.Run() == nil
}

// waitForKubernetesCluster waits for the Kubernetes cluster to become ready.
func waitForKubernetesCluster(kubeconfigPath string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		// Try to create a client and check if cluster is ready
		k8sConfig, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
		if err != nil {
			time.Sleep(2 * time.Second)

			continue
		}

		client, err := kubernetes.NewForConfig(k8sConfig)
		if err != nil {
			time.Sleep(2 * time.Second)

			continue
		}

		// Try to list nodes to verify cluster is working
		_, err = client.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
		if err == nil {
			return nil
		}

		time.Sleep(2 * time.Second)
	}

	return fmt.Errorf("Kubernetes cluster did not become ready within %v", timeout)
}
