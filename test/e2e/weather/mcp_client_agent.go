
// Package weather provides an intelligent MCP client agent for E2E testing
package weather

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

// Task status constants.
const (
	taskStatusFailed    = "failed"
	taskStatusCompleted = "completed"
)

// Agent configuration constants.
const (
	requestChannelSize      = 100
	responseChannelSize     = 100
	errorChannelSize        = 10
	numWorkerGoroutines     = 3
	initRequestTimeout      = 10 // seconds
	toolsRequestTimeout     = 5  // seconds
	weatherRequestTimeout   = 10 // seconds
	forecastDays            = 7
	forecastRequestTimeout  = 10 // seconds
	concurrentRequestTimeout = 5 // seconds
	parisLatitude           = 48.8566
	parisLongitude          = 2.3522
	taskCompletionDelay     = 100 // milliseconds
	baseLatitude            = 40.0
	baseLongitude           = -74.0
	latLonIncrement         = 0.1
	longRequestTimeout      = 30 // seconds
	shortSleepDelay         = 10 // milliseconds
	successRateMultiplier   = 100
	bufferSizeKB            = 1024
	bufferSizeMB            = 1024 * bufferSizeKB // 1MB
	defaultTimeout          = 5  // seconds
)

// MCPClientAgent represents an intelligent coding agent that communicates via MCP Router
// This simulates a real AI agent (like Claude) connecting through the Router via stdio.
type MCPClientAgent struct {
	ID           string
	logger       *zap.Logger
	routerPath   string
	routerConfig string

	// Process management
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser

	// Communication channels
	requestChan  chan *AgentRequest
	responseChan chan *AgentResponse
	errorChan    chan error

	// State management
	connected   atomic.Bool
	initialized atomic.Bool
	requestID   atomic.Int64

	// Request tracking
	pendingRequests sync.Map // map[interface{}]chan *AgentResponse

	// Context for cancellation
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Metrics
	metrics *AgentMetrics
}

// AgentRequest represents a request from the agent.
type AgentRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params"`
	ID      interface{} `json:"id"`
}

// AgentResponse represents a response to the agent.
type AgentResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *AgentError     `json:"error,omitempty"`
	ID      interface{}     `json:"id"`
}

// AgentError represents an error in the agent protocol.
type AgentError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// AgentMetrics tracks agent performance.
type AgentMetrics struct {
	RequestsSent      int64
	ResponsesReceived int64
	Errors            int64
	AverageLatencyMs  float64
	mu                sync.RWMutex
}

// AgentCapabilities defines what the agent can do.
type AgentCapabilities struct {
	SupportsTools     bool
	SupportsResources bool
	SupportsPrompts   bool
	MaxConcurrent     int
}

// NewMCPClientAgent creates a new intelligent MCP client agent.
func NewMCPClientAgent(id, routerPath, routerConfig string, logger *zap.Logger) *MCPClientAgent {
	if logger == nil {
		logger = zap.NewNop()
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &MCPClientAgent{
		ID:           id,
		logger:       logger.With(zap.String("agent_id", id)),
		routerPath:   routerPath,
		routerConfig: routerConfig,
		requestChan:  make(chan *AgentRequest, requestChannelSize),
		responseChan: make(chan *AgentResponse, responseChannelSize),
		errorChan:    make(chan error, errorChannelSize),
		ctx:          ctx,
		cancel:       cancel,
		metrics:      &AgentMetrics{},
	}
}

// Connect establishes connection to MCP Router via stdio.
func (agent *MCPClientAgent) Connect() error {
	agent.logger.Info("Connecting to MCP Router",
		zap.String("router_path", agent.routerPath),
		zap.String("config", agent.routerConfig))

	// Create command to run Router with validation
	routerPath := filepath.Clean(agent.routerPath)
	configPath := filepath.Clean(agent.routerConfig)
	
	// Validate paths to prevent command injection
	if strings.Contains(routerPath, " ") || strings.Contains(configPath, " ") {
		return fmt.Errorf("invalid path contains spaces: router=%s, config=%s", routerPath, configPath)
	}
	
	// #nosec G204 -- This is a test environment with validated router and config paths
	agent.cmd = exec.CommandContext(agent.ctx, routerPath, "--config", configPath)

	// Set environment variables for Router
	agent.cmd.Env = append(os.Environ(),
		"MCP_AGENT_ID="+agent.ID,
		"MCP_LOG_LEVEL=info",
		"MCP_AUTH_TOKEN=test-agent-token",
	)

	// Get stdio pipes
	var err error

	agent.stdin, err = agent.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to get stdin pipe: %w", err)
	}

	agent.stdout, err = agent.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to get stdout pipe: %w", err)
	}

	agent.stderr, err = agent.cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to get stderr pipe: %w", err)
	}

	// Start Router process
	if err := agent.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start router: %w", err)
	}

	agent.connected.Store(true)

	// Start communication handlers
	agent.wg.Add(numWorkerGoroutines)

	go agent.handleStdout()
	go agent.handleStderr()
	go agent.handleRequests()

	agent.logger.Info("Connected to MCP Router successfully")

	return nil
}

// Initialize performs MCP protocol initialization.
func (agent *MCPClientAgent) Initialize() error {
	if !agent.connected.Load() {
		return errors.New("agent not connected")
	}

	agent.logger.Info("Initializing MCP protocol")

	// Send initialize request
	initReq := &AgentRequest{
		JSONRPC: "2.0",
		Method:  "initialize",
		Params: map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]interface{}{
				"tools":     map[string]interface{}{},
				"resources": map[string]interface{}{},
				"prompts":   map[string]interface{}{},
			},
			"clientInfo": map[string]interface{}{
				"name":    "mcp-agent-" + agent.ID,
				"version": "1.0.0",
				"type":    "coding-agent",
			},
		},
		ID: agent.nextRequestID(),
	}

	response, err := agent.sendRequestAndWait(initReq, initRequestTimeout*time.Second)
	if err != nil {
		return fmt.Errorf("initialization failed: %w", err)
	}

	if response.Error != nil {
		return fmt.Errorf("initialization error: %s", response.Error.Message)
	}

	agent.initialized.Store(true)
	agent.logger.Info("MCP protocol initialized successfully")

	return nil
}

// ExecuteWeatherQuery demonstrates an intelligent weather query.
//

func (agent *MCPClientAgent) ExecuteWeatherQuery(location string, lat, lon float64) (string, error) {
	if !agent.initialized.Load() {
		return "", errors.New("agent not initialized")
	}

	agent.logger.Info("Executing weather query",
		zap.String("location", location),
		zap.Float64("latitude", lat),
		zap.Float64("longitude", lon))

	if err := agent.listAvailableTools(); err != nil {
		return "", err
	}

	weatherResp, err := agent.sendWeatherRequest(lat, lon)
	if err != nil {
		return "", err
	}

	return agent.parseWeatherResponse(weatherResp)
}

func (agent *MCPClientAgent) listAvailableTools() error {
	toolsReq := &AgentRequest{
		JSONRPC: "2.0",
		Method:  "tools/list",
		Params:  map[string]interface{}{},
		ID:      agent.nextRequestID(),
	}

	toolsResp, err := agent.sendRequestAndWait(toolsReq, toolsRequestTimeout*time.Second)
	if err != nil {
		return fmt.Errorf("failed to list tools: %w", err)
	}

	var toolsList struct {
		Tools []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"tools"`
	}

	if err := json.Unmarshal(toolsResp.Result, &toolsList); err != nil {
		return fmt.Errorf("failed to parse tools list: %w", err)
	}

	agent.logger.Info("Available tools", zap.Int("count", len(toolsList.Tools)))

	return nil
}

func (agent *MCPClientAgent) sendWeatherRequest(lat, lon float64) (*AgentResponse, error) {
	weatherReq := &AgentRequest{
		JSONRPC: "2.0",
		Method:  "tools/call",
		Params: map[string]interface{}{
			"name": "get_current_weather",
			"arguments": map[string]interface{}{
				"latitude":  lat,
				"longitude": lon,
			},
		},
		ID: agent.nextRequestID(),
	}

	weatherResp, err := agent.sendRequestAndWait(weatherReq, weatherRequestTimeout*time.Second)
	if err != nil {
		return nil, fmt.Errorf("weather query failed: %w", err)
	}

	if weatherResp.Error != nil {
		return nil, fmt.Errorf("weather query error: %s", weatherResp.Error.Message)
	}

	return weatherResp, nil
}

func (agent *MCPClientAgent) parseWeatherResponse(weatherResp *AgentResponse) (string, error) {
	var weatherResult struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}

	if err := json.Unmarshal(weatherResp.Result, &weatherResult); err != nil {
		return "", fmt.Errorf("failed to parse weather result: %w", err)
	}

	if len(weatherResult.Content) > 0 {
		return weatherResult.Content[0].Text, nil
	}

	return "", errors.New("no weather data in response")
}

// ExecuteComplexTask simulates a complex coding task with multiple tool calls.
//

func (agent *MCPClientAgent) ExecuteComplexTask(task string) (*TaskResult, error) {
	if !agent.initialized.Load() {
		return nil, errors.New("agent not initialized")
	}

	agent.logger.Info("Executing complex task", zap.String("task", task))

	result := &TaskResult{
		Task:      task,
		StartTime: time.Now(),
		Steps:     []TaskStep{},
	}

	agent.executeWeatherSteps(result)
	agent.executeForecastStep(result)
	agent.finalizeTaskResult(result)

	return result, nil
}

func (agent *MCPClientAgent) executeWeatherSteps(result *TaskResult) {
	locations := agent.getTaskLocations()

	for _, loc := range locations {
		step := TaskStep{
			Name:      "Query weather for " + loc.Name,
			StartTime: time.Now(),
		}

		weather, err := agent.ExecuteWeatherQuery(loc.Name, loc.Latitude, loc.Longitude)
		if err != nil {
			step.Error = err
			step.Status = taskStatusFailed
		} else {
			step.Result = weather
			step.Status = taskStatusCompleted
		}

		step.Duration = time.Since(step.StartTime)
		result.Steps = append(result.Steps, step)
	}
}

func (agent *MCPClientAgent) getTaskLocations() []struct {
	Name      string
	Latitude  float64
	Longitude float64
} {
	return []struct {
		Name      string
		Latitude  float64
		Longitude float64
	}{
		{"New York", 40.7128, -74.0060},
		{"London", 51.5074, -0.1278},
		{"Tokyo", 35.6762, 139.6503},
	}
}

func (agent *MCPClientAgent) executeForecastStep(result *TaskResult) {
	locations := agent.getTaskLocations()

	forecastReq := &AgentRequest{
		JSONRPC: "2.0",
		Method:  "tools/call",
		Params: map[string]interface{}{
			"name": "get_forecast",
			"arguments": map[string]interface{}{
				"latitude":  locations[0].Latitude,
				"longitude": locations[0].Longitude,
				"days":      forecastDays,
			},
		},
		ID: agent.nextRequestID(),
	}

	forecastStep := TaskStep{
		Name:      "Get 7-day forecast",
		StartTime: time.Now(),
	}

	forecastResp, err := agent.sendRequestAndWait(forecastReq, forecastRequestTimeout*time.Second)
	if err != nil {
		forecastStep.Error = err
		forecastStep.Status = taskStatusFailed
	} else {
		forecastStep.Result = string(forecastResp.Result)
		forecastStep.Status = taskStatusCompleted
	}

	forecastStep.Duration = time.Since(forecastStep.StartTime)
	result.Steps = append(result.Steps, forecastStep)
}

func (agent *MCPClientAgent) finalizeTaskResult(result *TaskResult) {
	result.Duration = time.Since(result.StartTime)
	result.Success = true

	for _, step := range result.Steps {
		if step.Status == taskStatusFailed {
			result.Success = false

			break
		}
	}
}

// SimulateConversation simulates a multi-turn conversation.
func (agent *MCPClientAgent) SimulateConversation() error {
	conversation := []struct {
		Query  string
		Action func() error
	}{
		{
			Query: "What tools are available?",
			Action: func() error {
				req := &AgentRequest{
					JSONRPC: "2.0",
					Method:  "tools/list",
					Params:  map[string]interface{}{},
					ID:      agent.nextRequestID(),
				}
				_, err := agent.sendRequestAndWait(req, concurrentRequestTimeout*time.Second)

				return err
			},
		},
		{
			Query: "Get current weather for Paris",
			Action: func() error {
				_, err := agent.ExecuteWeatherQuery("Paris", parisLatitude, parisLongitude)

				return err
			},
		},
		{
			Query: "Compare weather in multiple cities",
			Action: func() error {
				_, err := agent.ExecuteComplexTask("weather-comparison")

				return err
			},
		},
	}

	for i, turn := range conversation {
		agent.logger.Info("Conversation turn",
			zap.Int("turn", i+1),
			zap.String("query", turn.Query))

		if err := turn.Action(); err != nil {
			return fmt.Errorf("conversation turn %d failed: %w", i+1, err)
		}

		// Simulate thinking time
		time.Sleep(taskCompletionDelay * time.Millisecond)
	}

	return nil
}

// TestConcurrentRequests tests concurrent request handling.
//

func (agent *MCPClientAgent) TestConcurrentRequests(numRequests int) (*ConcurrencyTestResult, error) {
	if !agent.initialized.Load() {
		return nil, errors.New("agent not initialized")
	}

	agent.logger.Info("Testing concurrent requests", zap.Int("count", numRequests))

	result := &ConcurrencyTestResult{
		TotalRequests: numRequests,
		StartTime:     time.Now(),
	}

	successCount, errorCount := agent.executeConcurrentRequests(numRequests)
	agent.finalizeConcurrencyResult(result, successCount, errorCount, numRequests)

	return result, nil
}

func (agent *MCPClientAgent) executeConcurrentRequests(numRequests int) (int32, int32) {
	var wg sync.WaitGroup

	successCount := atomic.Int32{}
	errorCount := atomic.Int32{}

	// Launch concurrent requests
	for i := 0; i < numRequests; i++ {
		wg.Add(1)

		go func(requestNum int) {
			defer wg.Done()

			req := agent.createConcurrentRequest(requestNum)

			_, err := agent.sendRequestAndWait(req, longRequestTimeout*time.Second)
			if err != nil {
				errorCount.Add(1)
				agent.logger.Error("Concurrent request failed",
					zap.Int("request", requestNum),
					zap.Error(err))
			} else {
				successCount.Add(1)
			}
		}(i)

		// Small delay to avoid overwhelming the system
		if i%10 == 0 {
			time.Sleep(shortSleepDelay * time.Millisecond)
		}
	}

	// Wait for all requests to complete
	wg.Wait()

	return successCount.Load(), errorCount.Load()
}

func (agent *MCPClientAgent) createConcurrentRequest(requestNum int) *AgentRequest {
	// Vary the location for each request
	lat := baseLatitude + float64(requestNum)*latLonIncrement
	lon := baseLongitude + float64(requestNum)*latLonIncrement

	return &AgentRequest{
		JSONRPC: "2.0",
		Method:  "tools/call",
		Params: map[string]interface{}{
			"name": "get_current_weather",
			"arguments": map[string]interface{}{
				"latitude":  lat,
				"longitude": lon,
			},
		},
		ID: fmt.Sprintf("concurrent-%d", requestNum),
	}
}

func (agent *MCPClientAgent) finalizeConcurrencyResult(
	result *ConcurrencyTestResult, successCount, errorCount int32, numRequests int) {
	result.Duration = time.Since(result.StartTime)
	result.SuccessCount = int(successCount)
	result.ErrorCount = int(errorCount)
	result.SuccessRate = float64(result.SuccessCount) / float64(numRequests) * successRateMultiplier
	result.RequestsPerSecond = float64(numRequests) / result.Duration.Seconds()

	agent.logger.Info("Concurrent test completed",
		zap.Int("success", result.SuccessCount),
		zap.Int("errors", result.ErrorCount),
		zap.Float64("success_rate", result.SuccessRate),
		zap.Float64("rps", result.RequestsPerSecond))
}

// handleStdout processes stdout from Router.
func (agent *MCPClientAgent) handleStdout() {
	defer agent.wg.Done()

	scanner := bufio.NewScanner(agent.stdout)
	scanner.Buffer(make([]byte, bufferSizeMB), bufferSizeMB) // 1MB buffer

	for scanner.Scan() {
		line := scanner.Bytes()

		// Try to parse as JSON response
		var response AgentResponse
		if err := json.Unmarshal(line, &response); err == nil {
			agent.handleResponse(&response)
		} else {
			// Log non-JSON output
			agent.logger.Debug("Router output", zap.String("line", string(line)))
		}
	}

	if err := scanner.Err(); err != nil {
		agent.logger.Error("Error reading stdout", zap.Error(err))

		agent.errorChan <- err
	}
}

// handleStderr processes stderr from Router.
func (agent *MCPClientAgent) handleStderr() {
	defer agent.wg.Done()

	scanner := bufio.NewScanner(agent.stderr)
	for scanner.Scan() {
		line := scanner.Text()
		agent.logger.Debug("Router stderr", zap.String("line", line))
	}
}

// handleRequests processes outgoing requests.
func (agent *MCPClientAgent) handleRequests() {
	defer agent.wg.Done()

	for {
		select {
		case <-agent.ctx.Done():
			return

		case req := <-agent.requestChan:
			if err := agent.sendRequest(req); err != nil {
				agent.logger.Error("Failed to send request", zap.Error(err))

				agent.errorChan <- err
			}
		}
	}
}

// handleResponse processes incoming responses.
func (agent *MCPClientAgent) handleResponse(response *AgentResponse) {
	agent.metrics.mu.Lock()
	agent.metrics.ResponsesReceived++
	agent.metrics.mu.Unlock()

	// Find pending request
	if ch, ok := agent.pendingRequests.Load(response.ID); ok {
		respChan, ok := ch.(chan *AgentResponse)
		if !ok {
			agent.logger.Error("Invalid channel type in pending requests")

			return
		}

		select {
		case respChan <- response:
		case <-time.After(time.Second):
			agent.logger.Warn("Timeout sending response to channel",
				zap.Any("id", response.ID))
		}

		agent.pendingRequests.Delete(response.ID)
	} else {
		// Unsolicited response or notification
		agent.logger.Debug("Received unsolicited response",
			zap.Any("id", response.ID))
	}
}

// sendRequest sends a request to Router via stdin.
func (agent *MCPClientAgent) sendRequest(req *AgentRequest) error {
	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	// Write JSON followed by newline
	if _, err := agent.stdin.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("failed to write to stdin: %w", err)
	}

	agent.metrics.mu.Lock()
	agent.metrics.RequestsSent++
	agent.metrics.mu.Unlock()

	agent.logger.Debug("Sent request",
		zap.String("method", req.Method),
		zap.Any("id", req.ID))

	return nil
}

// sendRequestAndWait sends a request and waits for response.
func (agent *MCPClientAgent) sendRequestAndWait(req *AgentRequest, timeout time.Duration) (*AgentResponse, error) {
	// Create response channel
	respChan := make(chan *AgentResponse, 1)

	agent.pendingRequests.Store(req.ID, respChan)
	defer agent.pendingRequests.Delete(req.ID)

	// Send request
	if err := agent.sendRequest(req); err != nil {
		return nil, err
	}

	// Wait for response
	select {
	case response := <-respChan:
		return response, nil

	case <-time.After(timeout):
		agent.metrics.mu.Lock()
		agent.metrics.Errors++
		agent.metrics.mu.Unlock()

		return nil, fmt.Errorf("request timeout after %v", timeout)

	case <-agent.ctx.Done():
		return nil, errors.New("agent shutting down")
	}
}

// nextRequestID generates the next request ID.
func (agent *MCPClientAgent) nextRequestID() interface{} {
	return agent.requestID.Add(1)
}

// Disconnect closes the connection to Router.
func (agent *MCPClientAgent) Disconnect() error {
	agent.logger.Info("Disconnecting from MCP Router")

	agent.connected.Store(false)
	agent.initialized.Store(false)

	// Cancel context to stop goroutines
	agent.cancel()

	// Close stdin to signal Router to exit
	if agent.stdin != nil {
		_ = agent.stdin.Close() // Best effort
	}

	// Wait for Router process to exit
	if agent.cmd != nil {
		// Give it time to exit gracefully
		done := make(chan error, 1)

		go func() {
			done <- agent.cmd.Wait()
		}()

		select {
		case <-done:
			agent.logger.Info("Router exited gracefully")
		case <-time.After(defaultTimeout * time.Second):
			agent.logger.Warn("Router didn't exit gracefully, killing process")
			_ = agent.cmd.Process.Kill() // Force kill
		}
	}

	// Wait for goroutines to finish
	agent.wg.Wait()

	agent.logger.Info("Disconnected from MCP Router")

	return nil
}

// GetMetrics returns current agent metrics.
func (agent *MCPClientAgent) GetMetrics() AgentMetrics {
	agent.metrics.mu.RLock()
	defer agent.metrics.mu.RUnlock()
	// Return a copy without the mutex to avoid copying lock value
	return AgentMetrics{
		RequestsSent:      agent.metrics.RequestsSent,
		ResponsesReceived: agent.metrics.ResponsesReceived,
		Errors:            agent.metrics.Errors,
		AverageLatencyMs:  agent.metrics.AverageLatencyMs,
		// Note: deliberately not copying mu (sync.RWMutex)
	}
}

// Test result structures

// TaskResult represents the result of a complex task.
type TaskResult struct {
	Task      string
	StartTime time.Time
	Duration  time.Duration
	Steps     []TaskStep
	Success   bool
}

// TaskStep represents a single step in a task.
type TaskStep struct {
	Name      string
	StartTime time.Time
	Duration  time.Duration
	Result    string
	Error     error
	Status    string
}

// ConcurrencyTestResult represents results of concurrent testing.
type ConcurrencyTestResult struct {
	TotalRequests     int
	SuccessCount      int
	ErrorCount        int
	StartTime         time.Time
	Duration          time.Duration
	SuccessRate       float64
	RequestsPerSecond float64
}
