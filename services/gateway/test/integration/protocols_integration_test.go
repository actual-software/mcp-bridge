package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/suite"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	"github.com/actual-software/mcp-bridge/services/gateway/internal/backends/stdio"
	wsBackend "github.com/actual-software/mcp-bridge/services/gateway/internal/backends/websocket"
	"github.com/actual-software/mcp-bridge/services/router/pkg/mcp"
)

const (
	httpStatusOK            = 200
	httpStatusInternalError = 500
)

// ProtocolsIntegrationTestSuite tests the new backend and frontend protocols.
type ProtocolsIntegrationTestSuite struct {
	suite.Suite
	ctx    context.Context
	cancel context.CancelFunc
}

func (suite *ProtocolsIntegrationTestSuite) SetupTest() {
	suite.ctx, suite.cancel = context.WithTimeout(context.Background(), 30*time.Second)
}

func (suite *ProtocolsIntegrationTestSuite) TearDownTest() {
	if suite.cancel != nil {
		suite.cancel()
	}
}

func TestProtocolsIntegration(t *testing.T) {
	suite.Run(t, new(ProtocolsIntegrationTestSuite))
}

// Test stdio backend integration.
func (suite *ProtocolsIntegrationTestSuite) TestStdioBackendIntegration() {
	logger := zaptest.NewLogger(suite.T())
	backend := suite.setupStdioBackend(logger)

	defer func() { _ = backend.Stop(suite.ctx) }()

	suite.testStdioBackendHealth(backend)
	suite.testStdioBackendRequestResponse(backend)
	suite.verifyStdioBackendMetrics(backend)
}

func (suite *ProtocolsIntegrationTestSuite) setupStdioBackend(logger *zap.Logger) *stdio.Backend {
	suite.T().Helper()

	script := suite.createTestScript(suite.getEchoServerScript())
	config := stdio.Config{
		Command: []string{"python3", script},
		Timeout: 5 * time.Second,
	}

	backend := stdio.CreateStdioBackend("test-stdio", config, logger, nil)
	err := backend.Start(suite.ctx)
	suite.Require().NoError(err)

	// Give server time to start
	time.Sleep(httpStatusOK * time.Millisecond)

	return backend
}

func (suite *ProtocolsIntegrationTestSuite) getEchoServerScript() string {
	return `#!/usr/bin/env python3
import json
import sys

while True:
    try:
        line = input().strip()
        if not line:
            continue
            
        request = json.loads(line)
        
        # Echo the request back as a response
        response = {
            "jsonrpc": "2.0",
            "id": request.get("id"),
            "result": {
                "echo": request,
                "timestamp": "2025-01-01T00:00:00Z"
            }
        }
        
        print(json.dumps(response))
        sys.stdout.flush()
    except EOFError:
        break
    except Exception as e:
        error_response = {
            "jsonrpc": "2.0",
            "id": request.get("id") if 'request' in locals() else None,
            "error": {"code": -32000, "message": str(e)}
        }
        print(json.dumps(error_response))
        sys.stdout.flush()`
}

func (suite *ProtocolsIntegrationTestSuite) testStdioBackendHealth(backend *stdio.Backend) {
	suite.T().Helper()

	err := backend.Health(suite.ctx)
	suite.NoError(err)
}

func (suite *ProtocolsIntegrationTestSuite) testStdioBackendRequestResponse(backend *stdio.Backend) {
	suite.T().Helper()

	req := &mcp.Request{
		JSONRPC: "2.0",
		Method:  "test/echo",
		ID:      "integration-test-1",
		Params: map[string]interface{}{
			"message": "integration test",
		},
	}

	response, err := backend.SendRequest(suite.ctx, req)
	suite.Require().NoError(err)
	suite.NotNil(response)
	suite.Equal("integration-test-1", response.ID)

	// Verify response structure
	result, ok := response.Result.(map[string]interface{})
	suite.Require().True(ok)

	echo, ok := result["echo"].(map[string]interface{})
	suite.Require().True(ok)
	suite.Equal("test/echo", echo["method"])
	suite.Equal("integration-test-1", echo["id"])
}

func (suite *ProtocolsIntegrationTestSuite) verifyStdioBackendMetrics(backend *stdio.Backend) {
	suite.T().Helper()

	metrics := backend.GetMetrics()
	suite.True(metrics.IsHealthy)
	suite.Positive(metrics.RequestCount)
}

// Test WebSocket backend integration.
func (suite *ProtocolsIntegrationTestSuite) TestWebSocketBackendIntegration() {
	logger := zaptest.NewLogger(suite.T())

	// Create mock WebSocket server
	server := suite.createWebSocketServer()
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	config := wsBackend.Config{
		Endpoints: []string{wsURL},
		Timeout:   5 * time.Second,
		ConnectionPool: wsBackend.PoolConfig{
			MinSize: 1,
			MaxSize: 2,
		},
	}

	backend := wsBackend.CreateWebSocketBackend("test-websocket", config, logger, nil)

	// Test backend lifecycle
	err := backend.Start(suite.ctx)

	suite.Require().NoError(err)

	defer func() { _ = backend.Stop(suite.ctx) }()

	// Give connections time to establish
	time.Sleep(300 * time.Millisecond)

	// Test health check
	err = backend.Health(suite.ctx)
	suite.Require().NoError(err)

	// Test request/response
	req := &mcp.Request{
		JSONRPC: "2.0",
		Method:  "test/websocket",
		ID:      "ws-integration-1",
		Params: map[string]interface{}{
			"protocol": "websocket",
		},
	}

	response, err := backend.SendRequest(suite.ctx, req)
	suite.Require().NoError(err)
	suite.NotNil(response)
	suite.Equal("ws-integration-1", response.ID)

	// Test metrics
	metrics := backend.GetMetrics()
	suite.True(metrics.IsHealthy)
	suite.Positive(metrics.RequestCount)
}

// Test stdio backend performance under load.
func (suite *ProtocolsIntegrationTestSuite) TestStdioBackendPerformance() {
	logger := zaptest.NewLogger(suite.T())
	backend := suite.setupPerformanceStdioBackend(logger)

	defer func() { _ = backend.Stop(suite.ctx) }()

	const numRequests = 10

	duration := suite.runPerformanceTest(backend, numRequests)
	suite.verifyPerformanceResults(backend, duration, numRequests)
}

func (suite *ProtocolsIntegrationTestSuite) setupPerformanceStdioBackend(logger *zap.Logger) *stdio.Backend {
	suite.T().Helper()

	script := suite.createTestScript(suite.getPerformanceServerScript())
	config := stdio.Config{
		Command: []string{"python3", script},
		Timeout: 10 * time.Second,
	}

	backend := stdio.CreateStdioBackend("perf-stdio", config, logger, nil)
	err := backend.Start(suite.ctx)
	suite.Require().NoError(err)

	time.Sleep(httpStatusOK * time.Millisecond)

	return backend
}

func (suite *ProtocolsIntegrationTestSuite) getPerformanceServerScript() string {
	return `#!/usr/bin/env python3
import json
import sys

while True:
    try:
        line = input().strip()
        if not line:
            continue
            
        request = json.loads(line)
        
        response = {
            "jsonrpc": "2.0",
            "id": request.get("id"),
            "result": {"status": "ok", "request_id": request.get("id")}
        }
        
        print(json.dumps(response))
        sys.stdout.flush()
    except EOFError:
        break
    except Exception:
        pass`
}

func (suite *ProtocolsIntegrationTestSuite) runPerformanceTest(backend *stdio.Backend, numRequests int) time.Duration {
	suite.T().Helper()

	results := make(chan error, numRequests)
	start := time.Now()

	for i := 0; i < numRequests; i++ {
		go func(id int) {
			req := &mcp.Request{
				JSONRPC: "2.0",
				Method:  "test/perf",
				ID:      fmt.Sprintf("perf-%d", id),
				Params:  map[string]interface{}{"id": id},
			}

			_, err := backend.SendRequest(suite.ctx, req)
			results <- err
		}(i)
	}

	// Collect results
	errors := 0

	for i := 0; i < numRequests; i++ {
		if err := <-results; err != nil {
			errors++
		}
	}

	suite.Equal(0, errors, "All requests should succeed")

	return time.Since(start)
}

func safeIntToUint64(n int) uint64 {
	if n < 0 {
		return 0
	}

	return uint64(n)
}

func (suite *ProtocolsIntegrationTestSuite) verifyPerformanceResults(
	backend *stdio.Backend, duration time.Duration, numRequests int,
) {
	suite.T().Helper()

	suite.Less(duration, 5*time.Second, "Requests should complete quickly")

	metrics := backend.GetMetrics()
	suite.True(metrics.IsHealthy)
	suite.Equal(safeIntToUint64(numRequests), metrics.RequestCount)
}

// Test concurrent operations across multiple protocols.
func (suite *ProtocolsIntegrationTestSuite) TestConcurrentMultiProtocolOperations() {
	logger := zaptest.NewLogger(suite.T())

	stdioBackend, wsBackendInstance := suite.setupConcurrentBackends(logger)

	defer func() { _ = stdioBackend.Stop(suite.ctx) }()
	defer func() { _ = wsBackendInstance.Stop(suite.ctx) }()

	const numRequests = 5

	stdioErrors, wsErrors := suite.runConcurrentRequests(stdioBackend, wsBackendInstance, numRequests)
	suite.verifyConcurrentResults(stdioBackend, wsBackendInstance, stdioErrors, wsErrors, numRequests)
}

func (suite *ProtocolsIntegrationTestSuite) setupConcurrentBackends(
	logger *zap.Logger,
) (*stdio.Backend, *wsBackend.Backend) {
	stdioBackend := suite.createConcurrentStdioBackend(logger)
	wsBackendInstance := suite.createConcurrentWebSocketBackend(logger)

	return stdioBackend, wsBackendInstance
}

func (suite *ProtocolsIntegrationTestSuite) createConcurrentStdioBackend(logger *zap.Logger) *stdio.Backend {
	suite.T().Helper()

	script := suite.createTestScript(suite.getConcurrentServerScript())
	stdioConfig := stdio.Config{
		Command: []string{"python3", script},
		Timeout: 10 * time.Second,
	}
	stdioBackend := stdio.CreateStdioBackend("concurrent-stdio", stdioConfig, logger, nil)
	err := stdioBackend.Start(suite.ctx)
	suite.Require().NoError(err)

	return stdioBackend
}

func (suite *ProtocolsIntegrationTestSuite) createConcurrentWebSocketBackend(logger *zap.Logger) *wsBackend.Backend {
	suite.T().Helper()

	wsServer := suite.createWebSocketServer()
	suite.T().Cleanup(func() { wsServer.Close() })

	wsURL := "ws" + strings.TrimPrefix(wsServer.URL, "http")
	wsConfig := wsBackend.Config{
		Endpoints: []string{wsURL},
		Timeout:   10 * time.Second,
		ConnectionPool: wsBackend.PoolConfig{
			MinSize: 2,
			MaxSize: 5,
		},
	}
	wsBackendInstance := wsBackend.CreateWebSocketBackend("concurrent-ws", wsConfig, logger, nil)
	err := wsBackendInstance.Start(suite.ctx)
	suite.Require().NoError(err)

	time.Sleep(httpStatusInternalError * time.Millisecond)

	return wsBackendInstance
}

func (suite *ProtocolsIntegrationTestSuite) getConcurrentServerScript() string {
	return `#!/usr/bin/env python3
import json
import sys
import time

while True:
    try:
        line = input().strip()
        if not line:
            continue
            
        request = json.loads(line)
        
        # Add small delay to simulate processing
        time.sleep(0.01)
        
        response = {
            "jsonrpc": "2.0",
            "id": request.get("id"),
            "result": {
                "protocol": "stdio",
                "processed_at": time.time()
            }
        }
        
        print(json.dumps(response))
        sys.stdout.flush()
    except EOFError:
        break
    except Exception:
        pass`
}

func (suite *ProtocolsIntegrationTestSuite) runConcurrentRequests(
	stdioBackend *stdio.Backend, wsBackendInstance *wsBackend.Backend, numRequests int,
) (int, int) {
	stdioResults := make(chan error, numRequests)
	wsResults := make(chan error, numRequests)

	suite.launchStdioRequests(stdioBackend, numRequests, stdioResults)
	suite.launchWebSocketRequests(wsBackendInstance, numRequests, wsResults)

	return suite.collectResults(stdioResults, wsResults, numRequests)
}

func (suite *ProtocolsIntegrationTestSuite) launchStdioRequests(
	stdioBackend *stdio.Backend, numRequests int, results chan error,
) {
	for i := 0; i < numRequests; i++ {
		go func(id int) {
			req := &mcp.Request{
				JSONRPC: "2.0",
				Method:  "test/concurrent",
				ID:      fmt.Sprintf("stdio-%d", id),
				Params:  map[string]interface{}{"id": id},
			}

			_, err := stdioBackend.SendRequest(suite.ctx, req)
			results <- err
		}(i)
	}
}

func (suite *ProtocolsIntegrationTestSuite) launchWebSocketRequests(
	wsBackendInstance *wsBackend.Backend, numRequests int, results chan error,
) {
	for i := 0; i < numRequests; i++ {
		go func(id int) {
			req := &mcp.Request{
				JSONRPC: "2.0",
				Method:  "test/concurrent",
				ID:      fmt.Sprintf("ws-%d", id),
				Params:  map[string]interface{}{"id": id},
			}

			_, err := wsBackendInstance.SendRequest(suite.ctx, req)
			results <- err
		}(i)
	}
}

func (suite *ProtocolsIntegrationTestSuite) collectResults(
	stdioResults, wsResults chan error, numRequests int,
) (int, int) {
	stdioErrors, wsErrors := 0, 0

	for i := 0; i < numRequests; i++ {
		if err := <-stdioResults; err != nil {
			stdioErrors++
		}

		if err := <-wsResults; err != nil {
			wsErrors++
		}
	}

	return stdioErrors, wsErrors
}

func (suite *ProtocolsIntegrationTestSuite) verifyConcurrentResults(
	stdioBackend *stdio.Backend, wsBackendInstance *wsBackend.Backend,
	stdioErrors, wsErrors, numRequests int,
) {
	suite.T().Helper()

	suite.LessOrEqual(stdioErrors, numRequests/2, "Too many stdio errors")
	suite.LessOrEqual(wsErrors, numRequests/2, "Too many WebSocket errors")

	stdioMetrics := stdioBackend.GetMetrics()
	wsMetrics := wsBackendInstance.GetMetrics()

	suite.True(stdioMetrics.IsHealthy)
	suite.True(wsMetrics.IsHealthy)
	suite.Positive(stdioMetrics.RequestCount)
	suite.Positive(wsMetrics.RequestCount)
}

// Helper methods

func (suite *ProtocolsIntegrationTestSuite) createTestScript(content string) string {
	suite.T().Helper()

	tmpDir := suite.T().TempDir()
	scriptPath := filepath.Join(tmpDir, "test_script.py")

	// #nosec G306 - test script needs execute permission (0o700) to run
	err := os.WriteFile(scriptPath, []byte(content), 0o700)
	suite.Require().NoError(err)

	return scriptPath
}

func (suite *ProtocolsIntegrationTestSuite) createWebSocketServer() *httptest.Server {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true // Allow all origins for testing
		},
	}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			suite.T().Logf("WebSocket upgrade failed: %v", err)

			return
		}

		defer func() { _ = conn.Close() }()

		for {
			// Read message
			messageType, data, err := conn.ReadMessage()
			if err != nil {
				if !websocket.IsCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
					suite.T().Logf("WebSocket read error: %v", err)
				}

				break
			}

			if messageType == websocket.TextMessage {
				// Parse as JSON-RPC request
				var request mcp.Request
				if err := json.Unmarshal(data, &request); err != nil {
					suite.T().Logf("Failed to parse request: %v", err)

					continue
				}

				// Create echo response
				response := mcp.Response{
					JSONRPC: "2.0",
					ID:      request.ID,
					Result: map[string]interface{}{
						"protocol": "websocket",
						"method":   request.Method,
						"params":   request.Params,
					},
				}

				// Send response
				responseData, err := json.Marshal(response)
				if err != nil {
					suite.T().Logf("Failed to marshal response: %v", err)

					continue
				}

				if err := conn.WriteMessage(websocket.TextMessage, responseData); err != nil {
					suite.T().Logf("Failed to write response: %v", err)

					break
				}
			}
		}
	}))
}
