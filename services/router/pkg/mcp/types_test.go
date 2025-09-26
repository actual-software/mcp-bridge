package mcp

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/poiley/mcp-bridge/services/router/internal/constants"
)

const (
	testIterations = 100
)

func TestNewRequest(t *testing.T) {
	tests := []struct {
		name   string
		method string
		params interface{}
		id     interface{}
	}{
		{
			name:   "Simple request with string ID",
			method: "initialize",
			params: map[string]string{"version": "1.0"},
			id:     "test-123",
		},
		{
			name:   "Request with numeric ID",
			method: "tools/list",
			params: nil,
			id:     42,
		},
		{
			name:   "Request with complex params",
			method: "tools/call",
			params: CallToolParams{
				Name:      "test-tool",
				Arguments: map[string]interface{}{"arg1": "value1"},
			},
			id: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := NewRequest(tt.method, tt.params, tt.id)

			if req.JSONRPC != constants.TestJSONRPCVersion {
				t.Errorf("Expected JSONRPC 2.0, got %s", req.JSONRPC)
			}

			if req.Method != tt.method {
				t.Errorf("Expected method %s, got %s", tt.method, req.Method)
			}

			if !reflect.DeepEqual(req.Params, tt.params) {
				t.Errorf("Params mismatch: expected %v, got %v", tt.params, req.Params)
			}

			if !reflect.DeepEqual(req.ID, tt.id) {
				t.Errorf("ID mismatch: expected %v, got %v", tt.id, req.ID)
			}
		})
	}
}

func TestNewResponse(t *testing.T) {
	tests := []struct {
		name   string
		result interface{}
		id     interface{}
	}{
		{
			name:   "Simple response",
			result: map[string]string{"status": "ok"},
			id:     "test-123",
		},
		{
			name: "Complex response",
			result: InitializeResult{
				ProtocolVersion: "1.0",
				Capabilities:    Capabilities{Resources: true},
				ServerInfo:      ServerInfo{Name: "test-server", Version: "1.0"},
			},
			id: 1,
		},
		{
			name:   "Nil result",
			result: nil,
			id:     42,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := NewResponse(tt.result, tt.id)

			if resp.JSONRPC != constants.TestJSONRPCVersion {
				t.Errorf("Expected JSONRPC 2.0, got %s", resp.JSONRPC)
			}

			if resp.Error != nil {
				t.Error("Expected no error in success response")
			}

			if !reflect.DeepEqual(resp.Result, tt.result) {
				t.Errorf("Result mismatch: expected %v, got %v", tt.result, resp.Result)
			}

			if !reflect.DeepEqual(resp.ID, tt.id) {
				t.Errorf("ID mismatch: expected %v, got %v", tt.id, resp.ID)
			}
		})
	}
}

func TestNewErrorResponse(t *testing.T) {
	tests := createErrorResponseTests()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := NewErrorResponse(tt.code, tt.message, tt.data, tt.id)
			validateErrorResponse(t, resp, tt)
		})
	}
}

func createErrorResponseTests() []struct {
	name    string
	code    int
	message string
	data    interface{}
	id      interface{}
} {
	return []struct {
		name    string
		code    int
		message string
		data    interface{}
		id      interface{}
	}{
		{
			name:    "Parse error",
			code:    ErrorCodeParseError,
			message: "Parse error",
			data:    nil,
			id:      nil,
		},
		{
			name:    "Method not found with data",
			code:    ErrorCodeMethodNotFound,
			message: "Method not found: unknown/method",
			data:    map[string]string{"method": "unknown/method"},
			id:      "test-123",
		},
		{
			name:    "Custom error",
			code:    ErrorCodeRateLimitExceeded,
			message: "Rate limit exceeded",
			data:    map[string]interface{}{"limit": testIterations, "reset": 1234567890},
			id:      42,
		},
	}
}

func validateErrorResponse(t *testing.T, resp *Response, tt struct {
	name    string
	code    int
	message string
	data    interface{}
	id      interface{}
}) {
	t.Helper()

	if resp.JSONRPC != constants.TestJSONRPCVersion {
		t.Errorf("Expected JSONRPC 2.0, got %s", resp.JSONRPC)
	}

	if resp.Result != nil {
		t.Error("Expected no result in error response")
	}

	if resp.Error == nil {
		t.Fatal("Expected error in error response")
	}

	if resp.Error.Code != tt.code {
		t.Errorf("Expected error code %d, got %d", tt.code, resp.Error.Code)
	}

	if resp.Error.Message != tt.message {
		t.Errorf("Expected error message '%s', got '%s'", tt.message, resp.Error.Message)
	}

	if !reflect.DeepEqual(resp.Error.Data, tt.data) {
		t.Errorf("Error data mismatch: expected %v, got %v", tt.data, resp.Error.Data)
	}

	if !reflect.DeepEqual(resp.ID, tt.id) {
		t.Errorf("ID mismatch: expected %v, got %v", tt.id, resp.ID)
	}
}

func TestRequest_JSONMarshaling(t *testing.T) {
	req := &Request{
		JSONRPC: constants.TestJSONRPCVersion,
		Method:  "tools/call",
		Params: CallToolParams{
			Name:      "test-tool",
			Arguments: map[string]string{"key": "value"},
		},
		ID: "test-123",
	}

	// Marshal.
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}

	// Verify JSON structure.
	var jsonMap map[string]interface{}
	if err := json.Unmarshal(data, &jsonMap); err != nil {
		t.Fatalf("Failed to unmarshal to map: %v", err)
	}

	if jsonMap["jsonrpc"] != constants.TestJSONRPCVersion {
		t.Error("JSON missing or incorrect jsonrpc field")
	}

	if jsonMap["method"] != "tools/call" {
		t.Error("JSON missing or incorrect method field")
	}

	if jsonMap["id"] != "test-123" {
		t.Error("JSON missing or incorrect id field")
	}

	// Unmarshal back.
	var decoded Request
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal request: %v", err)
	}

	if decoded.Method != req.Method {
		t.Errorf("Method mismatch after marshal/unmarshal")
	}
}

func TestResponse_JSONMarshaling(t *testing.T) {
	tests := []struct {
		name     string
		response *Response
		checkFor []string
	}{
		{
			name: "Success response",
			response: &Response{
				JSONRPC: constants.TestJSONRPCVersion,
				Result:  map[string]string{"status": "ok"},
				ID:      42,
			},
			checkFor: []string{"jsonrpc", "result", "id"},
		},
		{
			name: "Error response",
			response: &Response{
				JSONRPC: constants.TestJSONRPCVersion,
				Error: &Error{
					Code:    ErrorCodeInvalidParams,
					Message: "Invalid parameters",
					Data:    "Missing required field: name",
				},
				ID: "error-test",
			},
			checkFor: []string{"jsonrpc", "error", "id"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Marshal.
			data, err := json.Marshal(tt.response)
			if err != nil {
				t.Fatalf("Failed to marshal response: %v", err)
			}

			// Verify JSON structure.
			var jsonMap map[string]interface{}
			if err := json.Unmarshal(data, &jsonMap); err != nil {
				t.Fatalf("Failed to unmarshal to map: %v", err)
			}

			// Check required fields.
			for _, field := range tt.checkFor {
				if _, ok := jsonMap[field]; !ok {
					t.Errorf("JSON missing required field: %s", field)
				}
			}

			// Ensure error and result are mutually exclusive.
			_, hasResult := jsonMap["result"]
			_, hasError := jsonMap["error"]

			if hasResult && hasError {
				t.Error("Response should not have both result and error")
			}

			// Unmarshal back.
			var decoded Response
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("Failed to unmarshal response: %v", err)
			}
		})
	}
}

func TestError_StandardCodes(t *testing.T) {
	// Verify standard error codes match JSON-RPC 2.0 spec
	if ErrorCodeParseError != -32700 {
		t.Errorf("Parse error code should be -32700, got %d", ErrorCodeParseError)
	}

	if ErrorCodeInvalidRequest != -32600 {
		t.Errorf("Invalid request code should be -32600, got %d", ErrorCodeInvalidRequest)
	}

	if ErrorCodeMethodNotFound != -32601 {
		t.Errorf("Method not found code should be -32601, got %d", ErrorCodeMethodNotFound)
	}

	if ErrorCodeInvalidParams != -32602 {
		t.Errorf("Invalid params code should be -32602, got %d", ErrorCodeInvalidParams)
	}

	if ErrorCodeInternalError != -32603 {
		t.Errorf("Internal error code should be -32603, got %d", ErrorCodeInternalError)
	}
}

func TestInitializeParams_Marshaling(t *testing.T) {
	params := InitializeParams{
		ProtocolVersion: "1.0",
		Capabilities: Capabilities{
			Tools: &ToolsCapability{
				Namespaces: []string{"default", "custom"},
				TotalTools: 10,
			},
			Resources: true,
			Prompts:   false,
		},
		ClientInfo: ClientInfo{
			Name:    "test-client",
			Version: "1.0.0",
		},
	}

	// Marshal.
	data, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("Failed to marshal InitializeParams: %v", err)
	}

	// Unmarshal.
	var decoded InitializeParams
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal InitializeParams: %v", err)
	}

	// Verify.
	if decoded.ProtocolVersion != params.ProtocolVersion {
		t.Error("ProtocolVersion mismatch")
	}

	if decoded.Capabilities.Resources != params.Capabilities.Resources {
		t.Error("Capabilities.Resources mismatch")
	}

	if decoded.Capabilities.Tools == nil {
		t.Fatal("Capabilities.Tools is nil")
	}

	if len(decoded.Capabilities.Tools.Namespaces) != 2 {
		t.Errorf("Expected 2 namespaces, got %d", len(decoded.Capabilities.Tools.Namespaces))
	}

	if decoded.ClientInfo.Name != params.ClientInfo.Name {
		t.Error("ClientInfo.Name mismatch")
	}
}

func TestTool_Marshaling(t *testing.T) {
	tool := Tool{
		Name:        "example-tool",
		Description: "An example tool for testing",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"input": map[string]string{
					"type":        "string",
					"description": "Input parameter",
				},
			},
			"required": []string{"input"},
		},
		Namespace: "example",
		Version:   "1.0.0",
	}

	// Marshal.
	data, err := json.Marshal(tool)
	if err != nil {
		t.Fatalf("Failed to marshal Tool: %v", err)
	}

	// Unmarshal.
	var decoded Tool
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal Tool: %v", err)
	}

	// Verify basic fields.
	if decoded.Name != tool.Name {
		t.Errorf("Name mismatch: expected %s, got %s", tool.Name, decoded.Name)
	}

	if decoded.Description != tool.Description {
		t.Errorf("Description mismatch")
	}

	if decoded.Namespace != tool.Namespace {
		t.Errorf("Namespace mismatch")
	}

	if decoded.Version != tool.Version {
		t.Errorf("Version mismatch")
	}
}

func TestToolContent_Types(t *testing.T) {
	tests := createToolContentTestCases()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decoded := testToolContentSerialization(t, tt.content)
			validateToolContentType(t, tt.content, decoded)
			validateToolContentFields(t, tt.content, decoded)
		})
	}
}

// createToolContentTestCases creates test cases for different ToolContent types.
func createToolContentTestCases() []struct {
	name    string
	content ToolContent
} {
	return []struct {
		name    string
		content ToolContent
	}{
		{
			name: "Text content",
			content: ToolContent{
				Type: "text",
				Text: "Hello, world!",
			},
		},
		{
			name: "Image content",
			content: ToolContent{
				Type:     "image",
				MimeType: "image/png",
				Data:     "base64encodeddata",
			},
		},
		{
			name: "Resource content",
			content: ToolContent{
				Type: "resource",
				Resource: map[string]interface{}{
					"uri":  "resource://example",
					"name": "Example Resource",
				},
			},
		},
	}
}

// testToolContentSerialization tests marshaling and unmarshaling of ToolContent.
func testToolContentSerialization(t *testing.T, content ToolContent) ToolContent {
	t.Helper()

	// Marshal
	data, err := json.Marshal(content)
	if err != nil {
		t.Fatalf("Failed to marshal ToolContent: %v", err)
	}

	// Unmarshal
	var decoded ToolContent
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal ToolContent: %v", err)
	}

	return decoded
}

// validateToolContentType verifies that the content type matches expectations.
func validateToolContentType(t *testing.T, expected, decoded ToolContent) {
	t.Helper()

	if decoded.Type != expected.Type {
		t.Errorf("Type mismatch: expected %s, got %s", expected.Type, decoded.Type)
	}
}

// validateToolContentFields verifies type-specific fields match expectations.
func validateToolContentFields(t *testing.T, expected, decoded ToolContent) {
	t.Helper()

	switch expected.Type {
	case "text":
		validateTextContent(t, expected, decoded)
	case "image":
		validateImageContent(t, expected, decoded)
	case "resource":
		validateResourceContent(t, expected, decoded)
	}
}

// validateTextContent validates text-specific fields.
func validateTextContent(t *testing.T, expected, decoded ToolContent) {
	t.Helper()

	if decoded.Text != expected.Text {
		t.Error("Text content mismatch")
	}
}

// validateImageContent validates image-specific fields.
func validateImageContent(t *testing.T, expected, decoded ToolContent) {
	t.Helper()

	if decoded.MimeType != expected.MimeType {
		t.Error("MimeType mismatch")
	}

	if decoded.Data != expected.Data {
		t.Error("Data mismatch")
	}
}

// validateResourceContent validates resource-specific fields.
func validateResourceContent(t *testing.T, expected, decoded ToolContent) {
	t.Helper()

	if decoded.Resource == nil {
		t.Error("Resource is nil")
	}
}

func TestCallToolResult_Marshaling(t *testing.T) {
	result := CallToolResult{
		{
			Type: "text",
			Text: "First result",
		},
		{
			Type: "text",
			Text: "Second result",
		},
		{
			Type:     "image",
			MimeType: "image/jpeg",
			Data:     "base64data",
		},
	}

	// Marshal.
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Failed to marshal CallToolResult: %v", err)
	}

	// Unmarshal.
	var decoded CallToolResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal CallToolResult: %v", err)
	}

	// Verify.
	if len(decoded) != 3 {
		t.Errorf("Expected 3 tool contents, got %d", len(decoded))
	}

	if decoded[0].Type != "text" || decoded[0].Text != "First result" {
		t.Error("First result mismatch")
	}

	if decoded[2].Type != "image" || decoded[2].MimeType != "image/jpeg" {
		t.Error("Third result mismatch")
	}
}

func TestCapabilities_OmitEmpty(t *testing.T) {
	// Test that omitempty works correctly.
	caps := Capabilities{
		Resources: true,
		// Tools and Prompts should be omitted.
	}

	data, err := json.Marshal(caps)
	if err != nil {
		t.Fatal(err)
	}

	var jsonMap map[string]interface{}
	if err := json.Unmarshal(data, &jsonMap); err != nil {
		t.Fatal(err)
	}

	// Should have resources but not tools or prompts.
	if _, ok := jsonMap["resources"]; !ok {
		t.Error("Resources should be present")
	}

	if _, ok := jsonMap["tools"]; ok {
		t.Error("Tools should be omitted when nil")
	}

	if _, ok := jsonMap["prompts"]; ok {
		t.Error("Prompts should be omitted when false")
	}
}

func TestRequest_NilParams(t *testing.T) {
	// Test that nil params are handled correctly.
	req := NewRequest("test/method", nil, 1)

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}

	// Check that params field is omitted when nil.
	var jsonMap map[string]interface{}
	if err := json.Unmarshal(data, &jsonMap); err != nil {
		t.Fatal(err)
	}

	if _, ok := jsonMap["params"]; ok {
		t.Error("Params should be omitted when nil")
	}
}

func BenchmarkNewRequest(b *testing.B) {
	params := map[string]interface{}{
		"key1": "value1",
		"key2": 42,
		"key3": true,
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = NewRequest("test/method", params, i)
	}
}

func BenchmarkRequestMarshal(b *testing.B) {
	req := NewRequest("tools/call", CallToolParams{
		Name: "benchmark-tool",
		Arguments: map[string]interface{}{
			"input": "test data",
			"count": testIterations,
		},
	}, "bench-123")

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := json.Marshal(req)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkResponseUnmarshal(b *testing.B) {
	resp := NewResponse(InitializeResult{
		ProtocolVersion: "1.0",
		Capabilities:    Capabilities{Resources: true, Prompts: true},
		ServerInfo:      ServerInfo{Name: "bench-server", Version: "1.0.0"},
	}, 42)

	data, _ := json.Marshal(resp)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		var decoded Response
		if err := json.Unmarshal(data, &decoded); err != nil {
			b.Fatal(err)
		}
	}
}
