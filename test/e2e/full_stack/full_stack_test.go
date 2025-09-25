package main

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/poiley/mcp-bridge/test/testutil/e2e"
)

// Service URLs and endpoints - removed unused constants

// RouterAdapter adapts RouterController to implement e2e.RouterInterface.
type RouterAdapter struct {
	router *RouterController
}

func (r *RouterAdapter) SendRequestAndWait(req e2e.MCPRequest, timeout time.Duration) ([]byte, error) {
	return r.router.SendRequestAndWait(req, timeout)
}

// DockerStackInterface defines common methods for all docker stack types.
type DockerStackInterface interface {
	GetGatewayHTTPURL() string
	GetGatewayURL() string
	GetServiceLogs(ctx context.Context, service string) (string, error)
}

// TestFullStackEndToEnd is the main comprehensive E2E test
// TestFullStackEndToEnd validates the complete MCP protocol flow from client to server
// through the gateway and router architecture. This is the primary integration test
// that ensures all components work together correctly in a production-like environment.
//
// Test Architecture:
//   - Docker Compose stack with Gateway, Router, and Test MCP Server
//   - Real network connections (no mocking)
//   - JWT authentication with proper token validation
//   - WebSocket protocol for client-gateway communication
//   - STDIO protocol for router-server communication
//
// Coverage:
//   - Complete MCP protocol handshake and initialization
//   - Tool discovery and execution
//   - Error handling and propagation
//   - Concurrent request processing
//   - Network resilience and recovery
// setupFullStackTest initializes the test environment.
func setupFullStackTest(t *testing.T, ctx context.Context) (*DockerStack, *e2e.MCPClient) {
	t.Helper()

	logger, _ := zap.NewDevelopment()

	// Phase 1: Infrastructure Setup
	logger.Info("Starting Docker Compose stack")

	stack := NewDockerStack(t)
	t.Cleanup(stack.Cleanup)

	err := stack.Start(ctx)
	require.NoError(t, err, "Failed to start Docker stack")

	// Phase 2: Service Readiness Verification
	logger.Info("Waiting for services to be ready")

	err = stack.WaitForHTTPEndpoint(ctx, stack.GetGatewayHTTPURL()+"/health")
	require.NoError(t, err, "Gateway health check failed")

	// Phase 3: Router Setup and Authentication
	logger.Info("Setting up router controller")

	router := NewRouterController(t, stack.GetGatewayURL())
	t.Cleanup(func() {
		if err := router.Stop(); err != nil {
			t.Errorf("Failed to stop router: %v", err)
		}
	})

	err = router.BuildRouter()
	require.NoError(t, err, "Failed to build router binary")

	err = router.Start() 
	require.NoError(t, err, "Failed to start router")

	// Phase 4: MCP Client Setup
	// Create a wrapper to adapt RouterController to RouterInterface
	routerAdapter := &RouterAdapter{router: router}
	client := e2e.NewMCPClient(routerAdapter, logger)

	logger.Info("Initializing MCP client")

	_, err = client.Initialize()
	require.NoError(t, err, "Failed to initialize MCP client")

	return stack, client
}

func TestFullStackEndToEnd(t *testing.T) {
	// Skip if E2E tests are disabled
	if os.Getenv("SKIP_E2E_TESTS") == skipE2ETestsValue {
		t.Skip("Skipping E2E tests (SKIP_E2E_TESTS=true)")
	}

	// Set a reasonable timeout for the entire test
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	stack, client := setupFullStackTest(t, ctx)
	runComprehensiveTestScenarios(t, client, stack)
}

func runComprehensiveTestScenarios(t *testing.T, client *e2e.MCPClient, stack DockerStackInterface) {
	t.Helper()
	// Phase 5: Comprehensive Test Scenarios
	// Each test scenario validates a different aspect of the MCP protocol
	// and system behavior under various conditions

	// Test 1: Basic Protocol Flow
	// Validates the fundamental MCP operations: initialize, list tools, call tool
	t.Run("BasicMCPFlow", func(t *testing.T) {
		testBasicMCPFlow(t, client)
	})

	// Test 2: Multiple Tool Execution
	// Validates concurrent tool execution and proper response handling
	t.Run("MultipleToolExecution", func(t *testing.T) {
		testMultipleToolExecution(t, client)
	})

	// Test 3: Error Handling
	// Validates proper error propagation from server through gateway to client
	t.Run("ErrorHandling", func(t *testing.T) {
		testErrorHandling(t, client)
	})

	// Test 4: Concurrent Requests
	// Validates system behavior under concurrent load and proper request isolation
	t.Run("ConcurrentRequests", func(t *testing.T) {
		testConcurrentRequests(t, client)
	})

	// Test 5: Network Resilience
	// Validates recovery from network interruptions and service restarts
	t.Run("NetworkResilience", func(t *testing.T) {
		testNetworkResilience(t, client, stack)
	})
}

// TestAuthenticationSecurity tests various authentication mechanisms
// TestAuthenticationSecurity validates all security mechanisms implemented
// in the MCP Gateway and Router architecture. This comprehensive test suite
// ensures that unauthorized access is prevented and proper security controls
// are enforced at all system boundaries.
//
// Security Architecture Under Test:
//   - JWT Bearer Token Authentication (RFC 7519)
//   - Token expiration and lifecycle management
//   - TLS/SSL certificate validation and encryption
//   - Security headers for web vulnerability prevention
//   - Authentication bypass attempt detection
//
// Attack Vectors Tested:
//   - Expired token usage attempts
//   - Malformed or invalid token signatures
//   - Missing authentication headers
//   - Token tampering and forgery attempts
//   - TLS downgrade attacks
//
// Compliance Standards:
//   - OWASP Authentication Security Requirements
//   - RFC 7519 (JSON Web Token) specification
//   - RFC 8446 (TLS 1.3) security requirements
// MOVED TO: auth_test.go - TestAuthenticationSecurity

// TestLoadBalancingAndDiscovery tests load balancing with multiple backends.
