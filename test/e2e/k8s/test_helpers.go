package k8se2e

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/actual-software/mcp-bridge/test/testutil/e2e"
)

// This ensures that the complete protocol handshake works correctly and that
// tools can be discovered and executed successfully.
//
// Test Flow:
// 1. Protocol Initialization - Establish MCP session and exchange capabilities
// 2. Server Information Validation - Verify server metadata compliance
// 3. Tool Discovery - List all available tools from the server
// 4. Tool Schema Validation - Ensure each tool has proper metadata
// 5. Tool Execution - Execute a simple tool to validate end-to-end processing
// 6. Response Content Validation - Verify tool processed input correctly
//
// This test serves as the foundation for all other MCP protocol tests,
// ensuring basic connectivity and protocol compliance before testing
// more complex scenarios.
// testBasicMCPFlowWithoutInit tests basic MCP flow without initialization
// (assumes client is already initialized).
func testBasicMCPFlowWithoutInit(t *testing.T, client *e2e.MCPClient) {
	t.Helper()
	// Verify client is already initialized
	require.True(t, client.IsInitialized(), "Client must be initialized before this test")

	// Step 1: List available tools
	t.Log("Step 1: Listing available tools")

	tools, err := client.ListTools()
	require.NoError(t, err, "List tools failed")
	require.NotEmpty(t, tools, "No tools available")

	// Log discovered tools
	for _, tool := range tools {
		t.Logf("Discovered tool: %s - %s", tool.Name, tool.Description)
	}

	// Step 2: Execute the echo tool
	t.Log("Step 2: Executing echo tool")

	response, err := client.CallTool("echo", map[string]interface{}{
		"message": "Hello from Kubernetes!",
	})
	require.NoError(t, err, "Echo tool call failed")
	require.NotNil(t, response, "Tool response is nil")

	// Validate response structure
	err = e2e.AssertValidMCPResponse(response)
	require.NoError(t, err, "Invalid tool response")

	// Verify we got a successful result
	require.Nil(t, response.Error, "Tool call returned an error")
	require.NotNil(t, response.Result, "Tool call result is nil")
}

//

// testMultipleToolExecution tests various tool executions.
//

func testMultipleToolExecution(t *testing.T, client *e2e.MCPClient) {
	t.Helper()

	testMathTools(t, client)
	testCalculateExpression(t, client)
}

func testMathTools(t *testing.T, client *e2e.MCPClient) {
	t.Helper()
	testAddTool(t, client)
	testMultiplyTool(t, client)
	testSumTool(t, client)
}

func testAddTool(t *testing.T, client *e2e.MCPClient) {
	t.Helper()

	const (
		testNumberA = 15.5
		testNumberB = 26.5
		expectedSum = 42.0
	)

	addResp, err := client.CallTool("add", map[string]interface{}{
		"a": testNumberA,
		"b": testNumberB,
	})
	require.NoError(t, err, "Add tool call failed")

	err = e2e.AssertToolCallSuccess(addResp)
	require.NoError(t, err, "Add tool call validation failed")

	text, err := e2e.ExtractTextFromToolResponse(addResp)
	require.NoError(t, err, "Failed to extract text from add response")
	assert.Contains(t, text, "42.00", "Add result should be 42.00")
}

func testMultiplyTool(t *testing.T, client *e2e.MCPClient) {
	t.Helper()

	const (
		multiplyX = 6.0
		multiplyY = 7.0
	)

	multiplyResp, err := client.CallTool("multiply", map[string]interface{}{
		"x": multiplyX,
		"y": multiplyY,
	})
	require.NoError(t, err, "Multiply tool call failed")

	err = e2e.AssertToolCallSuccess(multiplyResp)
	require.NoError(t, err, "Multiply tool call validation failed")

	text, err := e2e.ExtractTextFromToolResponse(multiplyResp)
	require.NoError(t, err, "Failed to extract text from multiply response")
	assert.Contains(t, text, "42.00", "Multiply result should be 42.00")
}

func testSumTool(t *testing.T, client *e2e.MCPClient) {
	t.Helper()

	sumResp, err := client.CallTool("sum", map[string]interface{}{
		"numbers": []interface{}{10.0, 15.0, 17.0},
	})
	require.NoError(t, err, "Sum tool call failed")

	err = e2e.AssertToolCallSuccess(sumResp)
	require.NoError(t, err, "Sum tool call validation failed")

	text, err := e2e.ExtractTextFromToolResponse(sumResp)
	require.NoError(t, err, "Failed to extract text from sum response")
	assert.Contains(t, text, "42.00", "Sum result should be 42.00")
}

func testCalculateExpression(t *testing.T, client *e2e.MCPClient) {
	t.Helper()

	calcResp, err := client.CallTool("calculate", map[string]interface{}{
		"expression": "2 + 2 * 20",
	})
	require.NoError(t, err, "Calculate tool call failed")

	err = e2e.AssertToolCallSuccess(calcResp)
	require.NoError(t, err, "Calculate tool call validation failed")

	text, err := e2e.ExtractTextFromToolResponse(calcResp)
	require.NoError(t, err, "Failed to extract text from calculate response")
	assert.Contains(t, text, "42", "Calculate result should contain 42")
}

// testErrorHandling tests error scenarios and recovery.
func testErrorHandling(t *testing.T, client *e2e.MCPClient) {
	t.Helper()
	// Test intentional error tool
	errorResp, err := client.CallTool("error", map[string]interface{}{
		"error_code": -32603,
	})
	require.NoError(t, err, "Error tool call should not fail at transport level")

	err = e2e.AssertToolCallError(errorResp, -32603)
	require.NoError(t, err, "Error tool should return expected error")

	// Test non-existent tool
	notFoundResp, err := client.CallTool("nonexistent_tool", map[string]interface{}{})
	require.NoError(t, err, "Non-existent tool call should not fail at transport level")

	err = e2e.AssertToolCallError(notFoundResp, -32601)
	require.NoError(t, err, "Non-existent tool should return method not found error")

	// Test invalid arguments
	invalidResp, err := client.CallTool("add", map[string]interface{}{
		"invalid_param": "not_a_number",
	})
	require.NoError(t, err, "Invalid arguments should not fail at transport level")

	// Should either succeed (server is lenient) or return an error
	if invalidResp.Error != nil {
		assert.NotEqual(t, 0, invalidResp.Error.Code, "Error should have non-zero code")
		assert.NotEmpty(t, invalidResp.Error.Message, "Error should have message")
	}

	// Verify that after errors, normal operations still work
	echoResp, err := client.CallTool("echo", map[string]interface{}{
		"message": "Recovery test",
	})
	require.NoError(t, err, "Echo after errors should work")

	err = e2e.AssertToolCallSuccess(echoResp)
	require.NoError(t, err, "Echo after errors should be successful")
}
