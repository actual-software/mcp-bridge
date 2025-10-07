package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gorilla/mux"
	"go.uber.org/zap"
)

// Constants for magic numbers.
const (
	DefaultErrorCode = -32000
	ReadWriteTimeout = 15 * time.Second
	IdleTimeout      = 60 * time.Second
)

// MCP protocol types.
type MCPRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
	ID      interface{} `json:"id"`
}

type MCPResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	Result  interface{} `json:"result,omitempty"`
	Error   *MCPError   `json:"error,omitempty"`
	ID      interface{} `json:"id"`
}

type MCPError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

type Tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema interface{} `json:"inputSchema"`
}

type TestMCPServer struct {
	logger    *zap.Logger
	tools     []Tool
	backendID string
}

// createEchoTool creates the echo tool definition.
func createEchoTool() Tool {
	return Tool{
		Name:        "echo",
		Description: "Echo back the input",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"message": map[string]interface{}{
					"type": "string",
				},
			},
			"required": []string{"message"},
		},
	}
}

// createMathTools creates the mathematical operation tools.
func createMathTools() []Tool {
	return []Tool{
		{
			Name:        "add",
			Description: "Add two numbers",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"a": map[string]interface{}{"type": "number"},
					"b": map[string]interface{}{"type": "number"},
				},
				"required": []string{"a", "b"},
			},
		},
		{
			Name:        "multiply",
			Description: "Multiply two numbers",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"a": map[string]interface{}{"type": "number"},
					"b": map[string]interface{}{"type": "number"},
				},
				"required": []string{"a", "b"},
			},
		},
		{
			Name:        "sum",
			Description: "Sum an array of numbers",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"numbers": map[string]interface{}{
						"type":  "array",
						"items": map[string]interface{}{"type": "number"},
					},
				},
				"required": []string{"numbers"},
			},
		},
	}
}

// createUtilityTools creates utility tools for testing.
func createUtilityTools() []Tool {
	return []Tool{
		{
			Name:        "error",
			Description: "Trigger an error for testing",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"code":    map[string]interface{}{"type": "number"},
					"message": map[string]interface{}{"type": "string"},
				},
			},
		},
		{
			Name:        "calculate",
			Description: "Perform calculation",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"expression": map[string]interface{}{"type": "string"},
				},
				"required": []string{"expression"},
			},
		},
	}
}

// createAllTools creates all available tools for the test server.
func createAllTools() []Tool {
	tools := []Tool{createEchoTool()}
	tools = append(tools, createMathTools()...)
	tools = append(tools, createUtilityTools()...)

	return tools
}

func NewTestMCPServer() *TestMCPServer {
	logger, _ := zap.NewDevelopment()

	// Get backend ID from environment variable
	backendID := os.Getenv("BACKEND_ID")
	if backendID == "" {
		backendID = "default"
	}

	// Combine all tools
	tools := createAllTools()

	return &TestMCPServer{
		logger:    logger,
		tools:     tools,
		backendID: backendID,
	}
}

func (s *TestMCPServer) handleMCP(w http.ResponseWriter, r *http.Request) {
	var req MCPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.sendError(w, -32700, "Parse error", req.ID)

		return
	}

	s.logger.Info("Received MCP request",
		zap.String("method", req.Method),
		zap.Any("id", req.ID))

	var response MCPResponse

	response.JSONRPC = "2.0"
	response.ID = req.ID

	switch req.Method {
	case "initialize":
		response.Result = map[string]interface{}{
			"protocolVersion": "1.0",
			"capabilities": map[string]interface{}{
				"tools": true,
			},
			"serverInfo": map[string]interface{}{
				"name":    "test-mcp-server",
				"version": "1.0.0",
			},
		}

	case "tools/list":
		response.Result = map[string]interface{}{
			"tools": s.tools,
		}

	case "tools/call":
		s.handleToolCall(&response, req)

	default:
		response.Error = &MCPError{
			Code:    -32601,
			Message: "Method not found",
		}
	}

	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.logger.Error("Failed to encode response", zap.Error(err))
	}
}

func (s *TestMCPServer) handleToolCall(response *MCPResponse, req MCPRequest) {
	params, ok := req.Params.(map[string]interface{})
	if !ok {
		response.Error = &MCPError{
			Code:    -32602,
			Message: "Invalid params",
		}

		return
	}

	toolName, ok := params["name"].(string)
	if !ok {
		response.Error = &MCPError{
			Code:    -32602,
			Message: "Missing tool name",
		}

		return
	}

	arguments, _ := params["arguments"].(map[string]interface{})

	switch toolName {
	case "echo":
		s.handleEcho(response, arguments)
	case "add":
		s.handleAdd(response, arguments)
	case "multiply":
		s.handleMultiply(response, arguments)
	case "error":
		s.handleError(response, arguments)
	case "calculate":
		s.handleCalculate(response, arguments)
	case "sum":
		s.handleSum(response, arguments)
	default:
		response.Error = &MCPError{
			Code:    -32601,
			Message: "Tool not found: " + toolName,
		}
	}
}

// handleEcho handles the echo tool call.
func (s *TestMCPServer) handleEcho(response *MCPResponse, arguments map[string]interface{}) {
	message, ok := arguments["message"].(string)
	if !ok {
		response.Error = &MCPError{
			Code:    -32602,
			Message: "Invalid message parameter",
		}

		return
	}

	// Include backend ID for load balancing verification
	echoMessage := fmt.Sprintf("Echo from %s: %s", s.backendID, message)

	response.Result = map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": echoMessage,
			},
		},
	}
}

// handleAdd handles the add tool call.
func (s *TestMCPServer) handleAdd(response *MCPResponse, arguments map[string]interface{}) {
	a, aOk := arguments["a"].(float64)
	b, bOk := arguments["b"].(float64)

	if !aOk || !bOk {
		response.Error = &MCPError{
			Code:    -32602,
			Message: "Invalid number parameters",
		}

		return
	}

	result := a + b
	response.Result = map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": result,
			},
		},
	}
}

// handleMultiply handles the multiply tool call.
func (s *TestMCPServer) handleMultiply(response *MCPResponse, arguments map[string]interface{}) {
	a, aOk := arguments["a"].(float64)
	b, bOk := arguments["b"].(float64)

	if !aOk || !bOk {
		response.Error = &MCPError{
			Code:    -32602,
			Message: "Invalid number parameters",
		}

		return
	}

	result := a * b
	response.Result = map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": result,
			},
		},
	}
}

// handleError handles the error tool call for testing error scenarios.
func (s *TestMCPServer) handleError(response *MCPResponse, arguments map[string]interface{}) {
	// Read error_code parameter (matches what tests send)
	code, _ := arguments["error_code"].(float64)
	message, _ := arguments["message"].(string)

	if message == "" {
		message = "Intentional error for testing"
	}

	if code == 0 {
		code = DefaultErrorCode
	}

	response.Error = &MCPError{
		Code:    int(code),
		Message: message,
	}
}

// handleCalculate handles the calculate tool call.
func (s *TestMCPServer) handleCalculate(response *MCPResponse, arguments map[string]interface{}) {
	expression, ok := arguments["expression"].(string)
	if !ok {
		response.Error = &MCPError{
			Code:    -32602,
			Message: "Invalid expression parameter",
		}

		return
	}

	// Simple calculator implementation (for testing)
	// In a real implementation, this would evaluate the expression
	response.Result = map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": "Result of " + expression,
			},
		},
	}
}

// handleSum handles the sum tool call.
func (s *TestMCPServer) handleSum(response *MCPResponse, arguments map[string]interface{}) {
	numbers, ok := arguments["numbers"].([]interface{})
	if !ok {
		response.Error = &MCPError{
			Code:    -32602,
			Message: "Invalid numbers parameter",
		}

		return
	}

	var sum float64
	for _, n := range numbers {
		if num, ok := n.(float64); ok {
			sum += num
		}
	}

	response.Result = map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": sum,
			},
		},
	}
}

func (s *TestMCPServer) sendError(w http.ResponseWriter, code int, message string, id interface{}) {
	response := MCPResponse{
		JSONRPC: "2.0",
		Error: &MCPError{
			Code:    code,
			Message: message,
		},
		ID: id,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.logger.Error("Failed to encode error response", zap.Error(err))
	}
}

func (s *TestMCPServer) healthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	response := map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"tools":     len(s.tools),
		"backend":   s.backendID,
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.logger.Error("Failed to encode health check response", zap.Error(err))
	}
}

func main() {
	server := NewTestMCPServer()

	r := mux.NewRouter()
	r.HandleFunc("/mcp", server.handleMCP).Methods("POST")
	r.HandleFunc("/health", server.healthCheck).Methods("GET")

	// CORS middleware for testing
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusOK)

				return
			}

			next.ServeHTTP(w, r)
		})
	})

	// Create server with timeouts to satisfy gosec G114
	httpServer := &http.Server{
		Addr:         ":3000",
		Handler:      r,
		ReadTimeout:  ReadWriteTimeout,
		WriteTimeout: ReadWriteTimeout,
		IdleTimeout:  IdleTimeout,
	}

	server.logger.Info("Starting test MCP server on :3000")
	log.Fatal(httpServer.ListenAndServe())
}
