// Package weather provides a production-ready MCP server implementation
// for weather data using the Open-Meteo API for E2E testing.
package weather

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	// OpenMeteoAPIURL is the base URL for the Open-Meteo API (no auth required).
	OpenMeteoAPIURL = "https://api.open-meteo.com/v1"

	// DefaultHTTPTimeout is the default HTTP client timeout.
	DefaultHTTPTimeout = 10 * time.Second

	// MaxRetries for API calls.
	MaxRetries = 3

	// Rate limiter constants.
	rateLimitMaxRequests = 100
	rateLimitRefillRate  = 10
	rateLimitCapacity    = 90

	// WebSocket constants.
	wsReadBufferSize  = 1024
	wsWriteBufferSize = 1024
	wsPingPongTimeout = 60

	// Circuit breaker state constants.
	circuitBreakerStateOpen     = "open"
	circuitBreakerStateHalfOpen = "half-open"
	defaultMaxIdleConns         = 100
	defaultWSReadTimeout        = 30
	defaultRetryBackoff         = 10
	defaultCircuitFailures      = 5
	defaultCircuitSuccess       = 2
	circuitTimeoutSeconds       = 30
	bufferLength                = 16
	apiSuccessThreshold         = 0.9
	shutdownDelayTime           = 5
	pingInterval                = 30 // seconds
	messageChannelSize          = 10
	averageLatencyFactor        = 0.9
	latencyNewValueFactor       = 0.1
	metricsInterval             = 10 // seconds
	httpTimeout                 = 5  // seconds

	// RetryDelay between API call attempts.
	RetryDelay = 1 * time.Second
)

// WeatherMCPServer implements the MCP protocol for weather data.
type WeatherMCPServer struct {
	logger     *zap.Logger
	httpClient *http.Client
	wsUpgrader websocket.Upgrader

	// Connection management
	connections sync.Map

	// Metrics
	metrics   *ServerMetrics
	metricsMu sync.RWMutex

	// Health check
	healthStatus HealthStatus
	healthMu     sync.RWMutex

	// Rate limiting (per connection)
	rateLimiter *RateLimiter

	// Circuit breaker for API calls
	circuitBreaker *CircuitBreaker

	// Context for graceful shutdown
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// ServerMetrics tracks server performance metrics.
type ServerMetrics struct {
	RequestsTotal     int64
	RequestsSuccess   int64
	RequestsFailed    int64
	APICallsTotal     int64
	APICallsSuccess   int64
	APICallsFailed    int64
	ActiveConnections int64
	TotalConnections  int64
	MessagesSent      int64
	MessagesReceived  int64
	AverageLatencyMs  float64
	P95LatencyMs      float64
	P99LatencyMs      float64
	LastUpdated       time.Time
}

// HealthStatus represents the server's health state.
type HealthStatus struct {
	Status         string    `json:"status"`
	LastCheck      time.Time `json:"last_check"`
	APIHealthy     bool      `json:"api_healthy"`
	CircuitBreaker string    `json:"circuit_breaker"`
	ActiveConns    int       `json:"active_connections"`
	Uptime         string    `json:"uptime"`
	Version        string    `json:"version"`
}

// RateLimiter implements token bucket rate limiting.
type RateLimiter struct {
	tokens     int
	maxTokens  int
	refillRate time.Duration
	lastRefill time.Time
	mu         sync.Mutex
}

// CircuitBreaker implements circuit breaker pattern for API calls.
type CircuitBreaker struct {
	failureThreshold int
	successThreshold int
	timeout          time.Duration
	failures         int
	successes        int
	state            string // "closed", "open", "half-open"
	lastFailure      time.Time
	mu               sync.RWMutex
}

// MCPRequest represents an incoming MCP request.
type MCPRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	ID      interface{}     `json:"id"`
}

// MCPResponse represents an outgoing MCP response.
type MCPResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	Result  interface{} `json:"result,omitempty"`
	Error   *MCPError   `json:"error,omitempty"`
	ID      interface{} `json:"id"`
}

// MCPError represents an error in MCP protocol.
type MCPError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// WeatherParams represents parameters for weather requests.
type WeatherParams struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Timezone  string  `json:"timezone,omitempty"`
	Days      int     `json:"days,omitempty"`
}

// WeatherData represents weather information from Open-Meteo.
type WeatherData struct {
	Latitude  float64         `json:"latitude"`
	Longitude float64         `json:"longitude"`
	Timezone  string          `json:"timezone"`
	Elevation float64         `json:"elevation"`
	Current   *CurrentWeather `json:"current,omitempty"`
	Daily     *DailyWeather   `json:"daily,omitempty"`
	Hourly    *HourlyWeather  `json:"hourly,omitempty"`
}

// CurrentWeather represents current weather conditions.
type CurrentWeather struct {
	Time          string  `json:"time"`
	Temperature   float64 `json:"temperature_2m"`
	Humidity      int     `json:"relative_humidity_2m"`
	WindSpeed     float64 `json:"wind_speed_10m"`
	WindDirection int     `json:"wind_direction_10m"`
	Pressure      float64 `json:"surface_pressure"`
	CloudCover    int     `json:"cloud_cover"`
	Precipitation float64 `json:"precipitation"`
}

// DailyWeather represents daily forecast data.
type DailyWeather struct {
	Time          []string  `json:"time"`
	MaxTemp       []float64 `json:"temperature_2m_max"`
	MinTemp       []float64 `json:"temperature_2m_min"`
	Precipitation []float64 `json:"precipitation_sum"`
	WindSpeed     []float64 `json:"wind_speed_10m_max"`
}

// HourlyWeather represents hourly forecast data.
type HourlyWeather struct {
	Time          []string  `json:"time"`
	Temperature   []float64 `json:"temperature_2m"`
	Humidity      []int     `json:"relative_humidity_2m"`
	WindSpeed     []float64 `json:"wind_speed_10m"`
	Precipitation []float64 `json:"precipitation"`
}

// NewWeatherMCPServer creates a new weather MCP server instance.
func NewWeatherMCPServer(logger *zap.Logger) *WeatherMCPServer {
	if logger == nil {
		config := zap.NewProductionConfig()
		config.Level = zap.NewAtomicLevelAt(zapcore.InfoLevel)
		logger, _ = config.Build()
	}

	ctx, cancel := context.WithCancel(context.Background())

	server := &WeatherMCPServer{
		logger: logger,
		httpClient: &http.Client{
			Timeout: DefaultHTTPTimeout,
			Transport: &http.Transport{
				MaxIdleConns:        defaultMaxIdleConns,
				MaxIdleConnsPerHost: rateLimitRefillRate,
				IdleConnTimeout:     rateLimitCapacity * time.Second,
				DisableKeepAlives:   false,
			},
		},
		wsUpgrader: websocket.Upgrader{
			ReadBufferSize:  wsReadBufferSize,
			WriteBufferSize: wsWriteBufferSize,
			CheckOrigin: func(r *http.Request) bool {
				// For testing, allow all origins
				// In production, implement proper origin validation
				return true
			},
		},
		metrics: &ServerMetrics{
			LastUpdated: time.Now(),
		},
		rateLimiter: &RateLimiter{
			maxTokens:  rateLimitMaxRequests,
			tokens:     rateLimitMaxRequests,
			refillRate: time.Second,
			lastRefill: time.Now(),
		},
		circuitBreaker: &CircuitBreaker{
			failureThreshold: defaultCircuitFailures,
			successThreshold: defaultCircuitSuccess,
			timeout:          circuitTimeoutSeconds * time.Second,
			state:            "closed",
		},
		healthStatus: HealthStatus{
			Status:    "healthy",
			LastCheck: time.Now(),
			Version:   "1.0.0",
		},
		ctx:    ctx,
		cancel: cancel,
	}

	// Start background health checker
	server.wg.Add(1)

	go server.healthChecker()

	return server
}

// HandleWebSocket handles incoming WebSocket connections.
func (s *WeatherMCPServer) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Error("Failed to upgrade WebSocket", zap.Error(err))

		return
	}

	connID := fmt.Sprintf("conn-%d", time.Now().UnixNano())
	s.connections.Store(connID, conn)
	s.updateConnectionMetrics(1)

	s.logger.Info("New WebSocket connection", zap.String("conn_id", connID))

	// Handle connection lifecycle
	s.wg.Add(1)

	go func() {
		ctx := s.ctx
		s.handleConnection(ctx, connID, conn)
	}()
}

// handleConnection manages a single WebSocket connection.
func (s *WeatherMCPServer) handleConnection(ctx context.Context, connID string, conn *websocket.Conn) {
	defer func() {
		s.wg.Done()

		_ = conn.Close() // Best effort

		s.connections.Delete(connID)
		s.updateConnectionMetrics(-1)
		s.logger.Info("Connection closed", zap.String("conn_id", connID))
	}()

	// Set up ping/pong for keepalive
	_ = conn.SetReadDeadline(time.Now().Add(wsPingPongTimeout * time.Second)) // Initial deadline
	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(wsPingPongTimeout * time.Second)) // Reset deadline

		return nil
	})

	// Start ping ticker
	ticker := time.NewTicker(pingInterval * time.Second)
	defer ticker.Stop()

	// Message handling loop
	msgChan := make(chan []byte, messageChannelSize)
	errChan := make(chan error, 1)

	// Read goroutine
	go func() {
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				errChan <- err

				return
			}

			msgChan <- message
		}
	}()

	for {
		select {
		case <-s.ctx.Done():
			return

		case <-ticker.C:
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				s.logger.Error("Ping failed", zap.String("conn_id", connID), zap.Error(err))

				return
			}

		case message := <-msgChan:
			s.updateMetrics("messages_received", 1)
			s.handleMessage(ctx, connID, conn, message)

		case err := <-errChan:
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				s.logger.Error("WebSocket error", zap.String("conn_id", connID), zap.Error(err))
			}

			return
		}
	}
}

// handleMessage processes a single MCP message.
func (s *WeatherMCPServer) handleMessage(ctx context.Context, connID string, conn *websocket.Conn, message []byte) {
	start := time.Now()

	var request MCPRequest
	if err := json.Unmarshal(message, &request); err != nil {
		s.sendError(conn, nil, -32700, "Parse error", err.Error())

		return
	}

	// Check rate limit
	if !s.checkRateLimit(connID) {
		s.sendError(conn, request.ID, -32000, "Rate limit exceeded", "Too many requests")

		return
	}

	// Route to appropriate handler
	switch request.Method {
	case "tools/list":
		s.handleToolsList(conn, &request)
	case "tools/call":
		s.handleToolCall(ctx, conn, &request)
	case "initialize":
		s.handleInitialize(conn, &request)
	case "health":
		s.handleHealth(conn, &request)
	default:
		s.sendError(conn, request.ID, -32601, "Method not found", request.Method)
	}

	// Update latency metrics
	latency := time.Since(start)
	s.updateLatencyMetrics(latency)
}

// handleToolsList returns available tools.
func (s *WeatherMCPServer) handleToolsList(conn *websocket.Conn, request *MCPRequest) {
	tools := []map[string]interface{}{
		{
			"name":        "get_current_weather",
			"description": "Get current weather for a location",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"latitude": map[string]interface{}{
						"type":        "number",
						"description": "Latitude of the location",
					},
					"longitude": map[string]interface{}{
						"type":        "number",
						"description": "Longitude of the location",
					},
				},
				"required": []string{"latitude", "longitude"},
			},
		},
		{
			"name":        "get_forecast",
			"description": "Get weather forecast for a location",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"latitude": map[string]interface{}{
						"type":        "number",
						"description": "Latitude of the location",
					},
					"longitude": map[string]interface{}{
						"type":        "number",
						"description": "Longitude of the location",
					},
					"days": map[string]interface{}{
						"type":        "integer",
						"description": "Number of forecast days (1-16)",
						"minimum":     1,
						"maximum":     bufferLength,
					},
				},
				"required": []string{"latitude", "longitude"},
			},
		},
	}

	s.sendResponse(conn, request.ID, map[string]interface{}{
		"tools": tools,
	})
}

// handleToolCall executes a tool and returns results.
func (s *WeatherMCPServer) handleToolCall(ctx context.Context, conn *websocket.Conn, request *MCPRequest) {
	var params struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}

	if err := json.Unmarshal(request.Params, &params); err != nil {
		s.sendError(conn, request.ID, -32602, "Invalid params", err.Error())

		return
	}

	switch params.Name {
	case "get_current_weather":
		s.handleGetCurrentWeather(ctx, conn, request.ID, params.Arguments)
	case "get_forecast":
		s.handleGetForecast(ctx, conn, request.ID, params.Arguments)
	default:
		s.sendError(conn, request.ID, -32602, "Unknown tool", params.Name)
	}
}

// handleGetCurrentWeather fetches current weather data.
//

func (s *WeatherMCPServer) handleGetCurrentWeather(
	ctx context.Context, conn *websocket.Conn, requestID interface{}, arguments json.RawMessage) {
	var params WeatherParams
	if err := json.Unmarshal(arguments, &params); err != nil {
		s.sendError(conn, requestID, -32602, "Invalid arguments", err.Error())

		return
	}

	// Check circuit breaker
	if !s.circuitBreaker.Allow() {
		s.sendError(conn, requestID, -32000, "Service unavailable", "API circuit breaker open")

		return
	}

	// Build API URL
	apiURL := OpenMeteoAPIURL + "/forecast"
	u, _ := url.Parse(apiURL)
	q := u.Query()
	q.Set("latitude", strconv.FormatFloat(params.Latitude, 'f', 6, 64))
	q.Set("longitude", strconv.FormatFloat(params.Longitude, 'f', 6, 64))
	q.Set("current",
		"temperature_2m,relative_humidity_2m,wind_speed_10m,wind_direction_10m,surface_pressure,cloud_cover,precipitation")

	if params.Timezone != "" {
		q.Set("timezone", params.Timezone)
	}

	u.RawQuery = q.Encode()

	// Make API call with retry
	data, err := s.callAPIWithRetry(ctx, u.String())
	if err != nil {
		s.circuitBreaker.RecordFailure()
		s.sendError(conn, requestID, -32000, "API call failed", err.Error())

		return
	}

	s.circuitBreaker.RecordSuccess()

	// Parse response
	var weatherData WeatherData
	if err := json.Unmarshal(data, &weatherData); err != nil {
		s.sendError(conn, requestID, -32000, "Failed to parse weather data", err.Error())

		return
	}

	s.sendResponse(conn, requestID, map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": formatCurrentWeather(&weatherData),
			},
		},
	})
}

// handleGetForecast fetches weather forecast data.
//

func (s *WeatherMCPServer) handleGetForecast(
	ctx context.Context, conn *websocket.Conn, requestID interface{}, arguments json.RawMessage) {
	var params WeatherParams
	if err := json.Unmarshal(arguments, &params); err != nil {
		s.sendError(conn, requestID, -32602, "Invalid arguments", err.Error())

		return
	}

	// Default to 7 days if not specified
	if params.Days == 0 {
		params.Days = 7
	}

	// Check circuit breaker
	if !s.circuitBreaker.Allow() {
		s.sendError(conn, requestID, -32000, "Service unavailable", "API circuit breaker open")

		return
	}

	// Build API URL
	apiURL := OpenMeteoAPIURL + "/forecast"
	u, _ := url.Parse(apiURL)
	q := u.Query()
	q.Set("latitude", strconv.FormatFloat(params.Latitude, 'f', 6, 64))
	q.Set("longitude", strconv.FormatFloat(params.Longitude, 'f', 6, 64))
	q.Set("daily", "temperature_2m_max,temperature_2m_min,precipitation_sum,wind_speed_10m_max")
	q.Set("forecast_days", strconv.Itoa(params.Days))

	if params.Timezone != "" {
		q.Set("timezone", params.Timezone)
	}

	u.RawQuery = q.Encode()

	// Make API call with retry
	data, err := s.callAPIWithRetry(ctx, u.String())
	if err != nil {
		s.circuitBreaker.RecordFailure()
		s.sendError(conn, requestID, -32000, "API call failed", err.Error())

		return
	}

	s.circuitBreaker.RecordSuccess()

	// Parse response
	var weatherData WeatherData
	if err := json.Unmarshal(data, &weatherData); err != nil {
		s.sendError(conn, requestID, -32000, "Failed to parse weather data", err.Error())

		return
	}

	s.sendResponse(conn, requestID, map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type": "text",
				"text": formatForecast(&weatherData, params.Days),
			},
		},
	})
}

// handleInitialize handles MCP initialization.
func (s *WeatherMCPServer) handleInitialize(conn *websocket.Conn, request *MCPRequest) {
	s.sendResponse(conn, request.ID, map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities": map[string]interface{}{
			"tools": map[string]interface{}{},
		},
		"serverInfo": map[string]interface{}{
			"name":    "weather-mcp-server",
			"version": "1.0.0",
		},
	})
}

// handleHealth returns server health status.
func (s *WeatherMCPServer) handleHealth(conn *websocket.Conn, request *MCPRequest) {
	health := s.GetHealth()
	s.sendResponse(conn, request.ID, health)
}

// callAPIWithRetry makes an API call with retry logic.
func (s *WeatherMCPServer) callAPIWithRetry(ctx context.Context, url string) ([]byte, error) {
	var lastErr error

	for i := 0; i < MaxRetries; i++ {
		if i > 0 {
			time.Sleep(RetryDelay * time.Duration(i))
		}

		s.updateMetrics("api_calls_total", 1)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			lastErr = err
			s.logger.Error("Failed to create API request", zap.Error(err))

			continue
		}

		resp, err := s.httpClient.Do(req)
		if err != nil {
			lastErr = err

			continue
		}

		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("API returned status %d", resp.StatusCode)

			s.updateMetrics("api_calls_failed", 1)

			continue
		}

		data, err := io.ReadAll(resp.Body)
		if err != nil {
			lastErr = err

			continue
		}

		s.updateMetrics("api_calls_success", 1)

		return data, nil
	}

	s.updateMetrics("api_calls_failed", 1)

	return nil, fmt.Errorf("API call failed after %d retries: %w", MaxRetries, lastErr)
}

// sendResponse sends a successful response.
func (s *WeatherMCPServer) sendResponse(conn *websocket.Conn, requestID interface{}, result interface{}) {
	response := MCPResponse{
		JSONRPC: "2.0",
		Result:  result,
		ID:      requestID,
	}

	data, err := json.Marshal(response)
	if err != nil {
		s.logger.Error("Failed to marshal response", zap.Error(err))

		return
	}

	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		s.logger.Error("Failed to write response", zap.Error(err))

		return
	}

	s.updateMetrics("messages_sent", 1)
	s.updateMetrics("requests_success", 1)
}

// sendError sends an error response.
//

func (s *WeatherMCPServer) sendError(
	conn *websocket.Conn, requestID interface{}, code int, message string, data interface{}) {
	response := MCPResponse{
		JSONRPC: "2.0",
		Error: &MCPError{
			Code:    code,
			Message: message,
			Data:    data,
		},
		ID: requestID,
	}

	responseData, err := json.Marshal(response)
	if err != nil {
		s.logger.Error("Failed to marshal error response", zap.Error(err))

		return
	}

	if err := conn.WriteMessage(websocket.TextMessage, responseData); err != nil {
		s.logger.Error("Failed to write error response", zap.Error(err))

		return
	}

	s.updateMetrics("messages_sent", 1)
	s.updateMetrics("requests_failed", 1)
}

// checkRateLimit checks if a connection has exceeded rate limits.
func (s *WeatherMCPServer) checkRateLimit(connID string) bool {
	s.rateLimiter.mu.Lock()
	defer s.rateLimiter.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(s.rateLimiter.lastRefill)
	tokensToAdd := int(elapsed / s.rateLimiter.refillRate)

	if tokensToAdd > 0 {
		s.rateLimiter.tokens = min(s.rateLimiter.maxTokens, s.rateLimiter.tokens+tokensToAdd)
		s.rateLimiter.lastRefill = now
	}

	if s.rateLimiter.tokens <= 0 {
		return false
	}

	s.rateLimiter.tokens--

	return true
}

// updateMetrics updates server metrics.
func (s *WeatherMCPServer) updateMetrics(metric string, delta int64) {
	s.metricsMu.Lock()
	defer s.metricsMu.Unlock()

	switch metric {
	case "requests_total":
		s.metrics.RequestsTotal += delta
	case "requests_success":
		s.metrics.RequestsSuccess += delta
	case "requests_failed":
		s.metrics.RequestsFailed += delta
	case "api_calls_total":
		s.metrics.APICallsTotal += delta
	case "api_calls_success":
		s.metrics.APICallsSuccess += delta
	case "api_calls_failed":
		s.metrics.APICallsFailed += delta
	case "messages_sent":
		s.metrics.MessagesSent += delta
	case "messages_received":
		s.metrics.MessagesReceived += delta
	}

	s.metrics.LastUpdated = time.Now()
}

// updateConnectionMetrics updates connection count metrics.
func (s *WeatherMCPServer) updateConnectionMetrics(delta int64) {
	s.metricsMu.Lock()
	defer s.metricsMu.Unlock()

	s.metrics.ActiveConnections += delta
	if delta > 0 {
		s.metrics.TotalConnections += delta
	}
}

// updateLatencyMetrics updates latency metrics.
func (s *WeatherMCPServer) updateLatencyMetrics(latency time.Duration) {
	s.metricsMu.Lock()
	defer s.metricsMu.Unlock()

	ms := float64(latency.Milliseconds())

	// Simple running average (in production, use proper percentile calculation)
	if s.metrics.AverageLatencyMs == 0 {
		s.metrics.AverageLatencyMs = ms
	} else {
		s.metrics.AverageLatencyMs = (s.metrics.AverageLatencyMs * averageLatencyFactor) + (ms * latencyNewValueFactor)
	}

	// Update P95/P99 (simplified)
	if ms > s.metrics.P95LatencyMs {
		s.metrics.P95LatencyMs = ms
	}

	if ms > s.metrics.P99LatencyMs {
		s.metrics.P99LatencyMs = ms
	}
}

// GetMetrics returns current server metrics.
func (s *WeatherMCPServer) GetMetrics() ServerMetrics {
	s.metricsMu.RLock()
	defer s.metricsMu.RUnlock()

	return *s.metrics
}

// GetHealth returns current health status.
func (s *WeatherMCPServer) GetHealth() HealthStatus {
	s.healthMu.RLock()
	defer s.healthMu.RUnlock()

	return s.healthStatus
}

// healthChecker runs periodic health checks.
func (s *WeatherMCPServer) healthChecker() {
	defer s.wg.Done()

	ticker := time.NewTicker(metricsInterval * time.Second)
	defer ticker.Stop()

	startTime := time.Now()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.healthMu.Lock()

			// Check API health by making a simple request
			apiHealthy := s.checkAPIHealth()

			// Count active connections
			activeConns := 0

			s.connections.Range(func(_, _ interface{}) bool {
				activeConns++

				return true
			})

			// Determine overall health
			status := "healthy"
			if !apiHealthy {
				status = "degraded"
			}

			if s.circuitBreaker.GetState() == circuitBreakerStateOpen {
				status = "unhealthy"
			}

			s.healthStatus = HealthStatus{
				Status:         status,
				LastCheck:      time.Now(),
				APIHealthy:     apiHealthy,
				CircuitBreaker: s.circuitBreaker.GetState(),
				ActiveConns:    activeConns,
				Uptime:         time.Since(startTime).String(),
				Version:        "1.0.0",
			}

			s.healthMu.Unlock()
		}
	}
}

// checkAPIHealth verifies Open-Meteo API is accessible.
func (s *WeatherMCPServer) checkAPIHealth() bool {
	// Simple health check - verify we can reach the API
	url := OpenMeteoAPIURL + "/forecast?latitude=0&longitude=0&current=temperature_2m"

	ctx, cancel := context.WithTimeout(context.Background(), httpTimeout*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return false
	}

	defer func() { _ = resp.Body.Close() }()

	return resp.StatusCode == http.StatusOK
}

// Shutdown gracefully shuts down the server.
func (s *WeatherMCPServer) Shutdown() {
	s.logger.Info("Shutting down weather MCP server")

	// Cancel context to stop background tasks
	s.cancel()

	// Close all connections
	s.connections.Range(func(key, value interface{}) bool {
		if conn, ok := value.(*websocket.Conn); ok {
			_ = conn.WriteMessage(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, "Server shutting down"))
			_ = conn.Close() // Best effort
		}

		s.connections.Delete(key)

		return true
	})

	// Wait for all goroutines to finish
	s.wg.Wait()

	s.logger.Info("Weather MCP server shutdown complete")
}

// CircuitBreaker methods

// Allow checks if a request should be allowed.
func (cb *CircuitBreaker) Allow() bool {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	switch cb.state {
	case circuitBreakerStateOpen:
		if time.Since(cb.lastFailure) > cb.timeout {
			cb.mu.RUnlock()
			cb.mu.Lock()
			cb.state = circuitBreakerStateHalfOpen
			cb.successes = 0
			cb.mu.Unlock()
			cb.mu.RLock()

			return true
		}

		return false
	case circuitBreakerStateHalfOpen:
		return true
	default: // closed
		return true
	}
}

// RecordSuccess records a successful request.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures = 0

	if cb.state == circuitBreakerStateHalfOpen {
		cb.successes++
		if cb.successes >= cb.successThreshold {
			cb.state = "closed"
		}
	}
}

// RecordFailure records a failed request.
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures++
	cb.lastFailure = time.Now()

	if cb.failures >= cb.failureThreshold {
		cb.state = circuitBreakerStateOpen
		cb.successes = 0
	}
}

// GetState returns the current state.
func (cb *CircuitBreaker) GetState() string {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	return cb.state
}

// Helper functions

func min(a, b int) int {
	if a < b {
		return a
	}

	return b
}

func formatCurrentWeather(data *WeatherData) string {
	if data.Current == nil {
		return "No current weather data available"
	}

	return fmt.Sprintf(`Current Weather at %.2f°, %.2f°:
Temperature: %.1f°C
Humidity: %d%%
Wind Speed: %.1f km/h
Wind Direction: %d°
Pressure: %.1f hPa
Cloud Cover: %d%%
Precipitation: %.1f mm`,
		data.Latitude, data.Longitude,
		data.Current.Temperature,
		data.Current.Humidity,
		data.Current.WindSpeed,
		data.Current.WindDirection,
		data.Current.Pressure,
		data.Current.CloudCover,
		data.Current.Precipitation)
}

func formatForecast(data *WeatherData, days int) string {
	if data.Daily == nil || len(data.Daily.Time) == 0 {
		return "No forecast data available"
	}

	result := fmt.Sprintf("Weather Forecast for %.2f°, %.2f° (%d days):\n\n",
		data.Latitude, data.Longitude, days)

	for i := 0; i < len(data.Daily.Time) && i < days; i++ {
		result += fmt.Sprintf("Date: %s\n", data.Daily.Time[i])
		result += fmt.Sprintf("  Max Temp: %.1f°C\n", data.Daily.MaxTemp[i])
		result += fmt.Sprintf("  Min Temp: %.1f°C\n", data.Daily.MinTemp[i])
		result += fmt.Sprintf("  Precipitation: %.1f mm\n", data.Daily.Precipitation[i])
		result += fmt.Sprintf("  Max Wind: %.1f km/h\n\n", data.Daily.WindSpeed[i])
	}

	return result
}
