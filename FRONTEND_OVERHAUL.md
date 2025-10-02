# Frontend Protocol Implementation Overhaul

## Overview

This document outlines the work required to implement full frontend protocol support for the MCP Gateway. Currently, only the stdio frontend is fully implemented as a proper frontend. WebSocket and TCP Binary connections are handled through the HTTP server layer in `GatewayServer`, not as separate frontend implementations.

## Current State

### Implemented
- ✅ **stdio frontend**: Fully implemented in `services/gateway/internal/frontends/stdio/`
  - Unix socket mode
  - stdin/stdout mode
  - Named pipes mode (stub)
  - Complete connection management
  - Metrics tracking

### Partially Implemented (in server.go)
- ⚠️ **WebSocket**: Handled in `GatewayServer.handleWebSocket` (server.go:577-612)
- ⚠️ **TCP Binary**: Handled in `GatewayServer.acceptTCPConnections` (server.go:1240)
- ⚠️ **HTTP/SSE**: No frontend implementation (only backends exist)

## Architecture

### Frontend Interface

All frontends must implement the `Frontend` interface defined in `services/gateway/internal/frontends/interface.go`:

```go
type Frontend interface {
    // Start initializes the frontend and begins accepting connections
    Start(ctx context.Context) error

    // Stop gracefully shuts down the frontend
    Stop(ctx context.Context) error

    // GetName returns the frontend name
    GetName() string

    // GetProtocol returns the frontend protocol type
    GetProtocol() string

    // GetMetrics returns frontend-specific metrics
    GetMetrics() FrontendMetrics
}
```

### Frontend Metrics

```go
type FrontendMetrics struct {
    ActiveConnections uint64        `json:"active_connections"`
    TotalConnections  uint64        `json:"total_connections"`
    RequestCount      uint64        `json:"request_count"`
    ErrorCount        uint64        `json:"error_count"`
    IsRunning         bool          `json:"is_running"`
}
```

## Implementation Plan

### 1. WebSocket Frontend

**Location:** `services/gateway/internal/frontends/websocket/`

**Files to create:**
- `frontend.go` - Main WebSocket frontend implementation
- `frontend_test.go` - Comprehensive tests
- `config.go` - WebSocket-specific configuration

**Core Structure:**

```go
package websocket

type Config struct {
    Host             string        `mapstructure:"host"`
    Port             int           `mapstructure:"port"`
    TLS              TLSConfig     `mapstructure:"tls"`
    MaxConnections   int           `mapstructure:"max_connections"`
    ReadTimeout      time.Duration `mapstructure:"read_timeout"`
    WriteTimeout     time.Duration `mapstructure:"write_timeout"`
    PingInterval     time.Duration `mapstructure:"ping_interval"`
    PongTimeout      time.Duration `mapstructure:"pong_timeout"`
    MaxMessageSize   int64         `mapstructure:"max_message_size"`
    AllowedOrigins   []string      `mapstructure:"allowed_origins"`
}

type ClientConnection struct {
    id         string
    conn       *websocket.Conn
    session    *session.Session
    created    time.Time
    lastUsed   time.Time
    mu         sync.RWMutex
}

type Frontend struct {
    name     string
    config   Config
    logger   *zap.Logger
    router   RequestRouter
    auth     AuthProvider
    sessions SessionManager

    // WebSocket server
    server   *http.Server
    upgrader websocket.Upgrader

    // Connection management
    connections map[string]*ClientConnection
    connMu      sync.RWMutex
    connCount   int64

    // State management
    running    bool
    mu         sync.RWMutex

    // Metrics
    metrics    FrontendMetrics
    metricsMu  sync.RWMutex

    // Shutdown coordination
    shutdownCh chan struct{}
    wg         sync.WaitGroup
}
```

**Implementation Steps:**

1. **Extract from server.go**
   - Move WebSocket upgrade logic from `GatewayServer.handleWebSocket` (server.go:577-612)
   - Move authentication flow from `GatewayServer.authenticateWebSocketRequest` (server.go:630-657)
   - Move session creation from `GatewayServer.upgradeAndCreateSession` (server.go:659-703)

2. **Implement Frontend Interface**
   ```go
   func CreateWebSocketFrontend(
       name string,
       config Config,
       router RequestRouter,
       auth AuthProvider,
       sessions SessionManager,
       logger *zap.Logger,
   ) *Frontend

   func (f *Frontend) Start(ctx context.Context) error
   func (f *Frontend) Stop(ctx context.Context) error
   func (f *Frontend) GetName() string
   func (f *Frontend) GetProtocol() string
   func (f *Frontend) GetMetrics() FrontendMetrics
   ```

3. **Connection Handling**
   - Accept WebSocket upgrade requests
   - Authenticate connections (JWT/OAuth2/mTLS)
   - Create and manage sessions
   - Handle ping/pong keepalive
   - Track connection metrics
   - Implement graceful shutdown

4. **Request Processing**
   - Read JSON-RPC requests from WebSocket
   - Validate request format
   - Route through `RequestRouter` interface
   - Send responses back over WebSocket
   - Handle errors and timeouts

5. **Security Features**
   - Origin validation (CORS)
   - Connection limits (global and per-IP)
   - Rate limiting integration
   - TLS configuration
   - Security headers

**Reference Implementation:**
- Backend pattern: `services/gateway/internal/backends/websocket/backend.go`
- Connection pool: Lines 87-93
- Keepalive: Ping/pong handling in backend
- Shutdown: Graceful timeout pattern

**Estimated Effort:** 3-5 days, 800-1000 LOC

---

### 2. HTTP Frontend

**Location:** `services/gateway/internal/frontends/http/`

**Files to create:**
- `frontend.go` - HTTP request/response frontend
- `frontend_test.go` - Tests
- `config.go` - HTTP-specific configuration

**Core Structure:**

```go
package http

type Config struct {
    Host              string        `mapstructure:"host"`
    Port              int           `mapstructure:"port"`
    TLS               TLSConfig     `mapstructure:"tls"`
    RequestPath       string        `mapstructure:"request_path"`
    MaxRequestSize    int64         `mapstructure:"max_request_size"`
    ReadTimeout       time.Duration `mapstructure:"read_timeout"`
    WriteTimeout      time.Duration `mapstructure:"write_timeout"`
    IdleTimeout       time.Duration `mapstructure:"idle_timeout"`
    MaxHeaderBytes    int           `mapstructure:"max_header_bytes"`
}

type Frontend struct {
    name   string
    config Config
    logger *zap.Logger
    router RequestRouter
    auth   AuthProvider

    // HTTP server
    server     *http.Server
    mux        *http.ServeMux

    // State management
    running    bool
    mu         sync.RWMutex

    // Metrics
    metrics    FrontendMetrics
    metricsMu  sync.RWMutex

    // Shutdown coordination
    shutdownCh chan struct{}
}
```

**Implementation Steps:**

1. **Create HTTP Server**
   - HTTP server listening on configured port
   - TLS configuration support
   - Request routing to handler

2. **Request Handling**
   - Accept POST requests with JSON-RPC payloads
   - Parse and validate JSON-RPC format
   - Extract authentication credentials (headers, bearer tokens)
   - Route through `RequestRouter` interface
   - Return JSON-RPC response

3. **Support Patterns**
   - Synchronous request/response (standard)
   - Async with response callback URL (optional)
   - Batch request support (multiple requests in array)

4. **Security**
   - Request size limits
   - Header size limits
   - Authentication via headers
   - CORS configuration
   - Security headers (HSTS, CSP, etc.)

5. **Error Handling**
   - Timeout management
   - Proper HTTP status codes
   - JSON-RPC error format
   - Connection cleanup

**Reference Implementation:**
- SSE backend HTTP client: `services/gateway/internal/backends/sse/backend.go` (lines 149-153)
- Server setup patterns from `services/gateway/internal/server/server.go`

**Estimated Effort:** 2-3 days, 400-600 LOC

---

### 3. SSE Frontend (Server-Sent Events)

**Location:** `services/gateway/internal/frontends/sse/`

**Files to create:**
- `frontend.go` - SSE streaming frontend
- `frontend_test.go` - Tests
- `config.go` - SSE-specific configuration

**Core Structure:**

```go
package sse

type Config struct {
    Host            string        `mapstructure:"host"`
    Port            int           `mapstructure:"port"`
    TLS             TLSConfig     `mapstructure:"tls"`
    StreamEndpoint  string        `mapstructure:"stream_endpoint"`
    RequestEndpoint string        `mapstructure:"request_endpoint"`
    KeepAlive       time.Duration `mapstructure:"keep_alive"`
    MaxConnections  int           `mapstructure:"max_connections"`
    BufferSize      int           `mapstructure:"buffer_size"`
}

type StreamConnection struct {
    id       string
    writer   http.ResponseWriter
    flusher  http.Flusher
    created  time.Time
    lastUsed time.Time
    mu       sync.RWMutex
}

type Frontend struct {
    name   string
    config Config
    logger *zap.Logger
    router RequestRouter
    auth   AuthProvider

    // Dual HTTP servers
    streamServer  *http.Server  // For SSE streams (outbound)
    requestServer *http.Server  // For receiving requests (inbound)

    // Stream management
    streams   map[string]*StreamConnection
    streamsMu sync.RWMutex

    // Request correlation
    pendingRequests map[string]chan *mcp.Response
    pendingMu       sync.RWMutex

    // State management
    running    bool
    mu         sync.RWMutex

    // Metrics
    metrics    FrontendMetrics
    metricsMu  sync.RWMutex

    // Shutdown coordination
    shutdownCh chan struct{}
    wg         sync.WaitGroup
}
```

**Implementation Steps:**

1. **Dual Endpoint Pattern**
   - `/events` endpoint: SSE stream for responses (GET)
   - `/api/v1/request` endpoint: Receive requests (POST)
   - Correlation between stream and requests via session/auth

2. **Stream Management**
   - Accept SSE stream connections
   - Authenticate and create session
   - Maintain open HTTP connection
   - Send events in SSE format:
     ```
     event: message
     id: 12345
     data: {"jsonrpc":"2.0","result":{},"id":1}

     ```

3. **Request Handling**
   - Receive JSON-RPC requests on POST endpoint
   - Authenticate request
   - Route through `RequestRouter`
   - Send response via corresponding SSE stream
   - Handle client reconnection with event IDs

4. **Keepalive & Cleanup**
   - Send periodic keepalive comments (`:keepalive\n\n`)
   - Detect disconnected streams
   - Cleanup stale connections
   - Handle client reconnection

5. **Error Handling**
   - Stream disconnection detection
   - Request timeout management
   - Orphaned request cleanup
   - Graceful shutdown

**Reference Implementation:**
- SSE backend: `services/gateway/internal/backends/sse/backend.go` (lines 75-108)
- Event parsing and formatting patterns

**Estimated Effort:** 4-6 days, 700-900 LOC

---

### 4. TCP Binary Frontend

**Location:** `services/gateway/internal/frontends/tcp/`

**Files to create:**
- `frontend.go` - TCP binary protocol frontend
- `frontend_test.go` - Tests
- `config.go` - TCP-specific configuration

**Core Structure:**

```go
package tcp

import (
    "github.com/poiley/mcp-bridge/services/gateway/pkg/wire"
)

type Config struct {
    Host           string        `mapstructure:"host"`
    Port           int           `mapstructure:"port"`
    TLS            TLSConfig     `mapstructure:"tls"`
    MaxConnections int           `mapstructure:"max_connections"`
    ReadTimeout    time.Duration `mapstructure:"read_timeout"`
    WriteTimeout   time.Duration `mapstructure:"write_timeout"`
    MaxMessageSize int64         `mapstructure:"max_message_size"`
    HealthPort     int           `mapstructure:"health_port"`
}

type ClientConnection struct {
    id        string
    conn      net.Conn
    transport *wire.Transport
    session   *session.Session
    created   time.Time
    lastUsed  time.Time
    mu        sync.RWMutex
}

type Frontend struct {
    name   string
    config Config
    logger *zap.Logger
    router RequestRouter
    auth   AuthProvider
    sessions SessionManager

    // TCP listener
    listener net.Listener

    // Connection management
    connections map[string]*ClientConnection
    connMu      sync.RWMutex
    connCount   int64

    // State management
    running    bool
    mu         sync.RWMutex

    // Metrics
    metrics    FrontendMetrics
    metricsMu  sync.RWMutex

    // Shutdown coordination
    shutdownCh chan struct{}
    wg         sync.WaitGroup
}
```

**Implementation Steps:**

1. **Extract from server.go**
   - Move TCP listener logic from `GatewayServer.acceptTCPConnections` (server.go:1240)
   - Move connection handling
   - Integrate existing `pkg/wire` binary protocol

2. **Binary Protocol Handling**
   - Reuse `services/gateway/pkg/wire/Transport`
   - Length-prefixed message framing
   - Protocol version negotiation
   - Message type handling (request, response, ping, pong)

3. **Connection Management**
   - Accept TCP connections
   - Authenticate via binary auth message
   - Create and manage sessions
   - Track active connections
   - Handle connection lifecycle

4. **Request Processing**
   - Read binary messages using wire.Transport
   - Decode to JSON-RPC format
   - Route through `RequestRouter`
   - Encode responses to binary format
   - Send back over TCP connection

5. **Health Check Server** (optional)
   - Separate TCP health check port
   - Simple ping/pong protocol
   - Integration with health checker

**Reference Implementation:**
- Wire protocol: `services/gateway/pkg/wire/transport.go`
- TCP handling: `services/gateway/internal/server/server.go` (acceptTCPConnections)
- TCP health: `services/gateway/internal/server/tcp_health.go`

**Estimated Effort:** 3-4 days, 600-800 LOC

---

### 5. Factory Updates

**Location:** `services/gateway/internal/frontends/factory.go`

**Modifications Required:**

```go
package frontends

import (
    "github.com/poiley/mcp-bridge/services/gateway/internal/frontends/stdio"
    "github.com/poiley/mcp-bridge/services/gateway/internal/frontends/websocket"
    "github.com/poiley/mcp-bridge/services/gateway/internal/frontends/http"
    "github.com/poiley/mcp-bridge/services/gateway/internal/frontends/sse"
    "github.com/poiley/mcp-bridge/services/gateway/internal/frontends/tcp"
)

type DefaultFactory struct {
    logger *zap.Logger
}

func CreateFrontendFactory(logger *zap.Logger) *DefaultFactory {
    return &DefaultFactory{logger: logger}
}

func (f *DefaultFactory) CreateFrontend(
    config FrontendConfig,
    router RequestRouter,
    auth AuthProvider,
    sessions SessionManager,
) (Frontend, error) {
    switch config.Protocol {
    case "stdio":
        return f.createStdioFrontend(config, router, auth, sessions)
    case "websocket":
        return f.createWebSocketFrontend(config, router, auth, sessions)
    case "http":
        return f.createHTTPFrontend(config, router, auth, sessions)
    case "sse":
        return f.createSSEFrontend(config, router, auth, sessions)
    case "tcp_binary":
        return f.createTCPBinaryFrontend(config, router, auth, sessions)
    default:
        return nil, fmt.Errorf("unsupported frontend protocol: %s", config.Protocol)
    }
}

func (f *DefaultFactory) SupportedProtocols() []string {
    return []string{"stdio", "websocket", "http", "sse", "tcp_binary"}
}

// Implementation methods for each protocol
func (f *DefaultFactory) createWebSocketFrontend(...) (Frontend, error) { ... }
func (f *DefaultFactory) createHTTPFrontend(...) (Frontend, error) { ... }
func (f *DefaultFactory) createSSEFrontend(...) (Frontend, error) { ... }
func (f *DefaultFactory) createTCPBinaryFrontend(...) (Frontend, error) { ... }
```

**Estimated Effort:** 1 day, 200-300 LOC

---

### 6. Configuration Updates

**Location:** `services/gateway/internal/config/types.go`

**Add Frontend Configuration:**

```go
type ServerConfig struct {
    Host                   string        `mapstructure:"host"`
    Port                   int           `mapstructure:"port"`
    MetricsPort            int           `mapstructure:"metrics_port"`
    MaxConnections         int           `mapstructure:"max_connections"`
    MaxConnectionsPerIP    int           `mapstructure:"max_connections_per_ip"`
    ConnectionBufferSize   int           `mapstructure:"connection_buffer_size"`

    // Legacy protocol settings (deprecated)
    Protocol      string `mapstructure:"protocol"`       // Deprecated: use Frontends
    TCPPort       int    `mapstructure:"tcp_port"`       // Deprecated: use Frontends
    TCPHealthPort int    `mapstructure:"tcp_health_port"` // Deprecated: use Frontends

    // New frontend configuration
    Frontends []FrontendConfigEntry `mapstructure:"frontends"`

    TLS           TLSConfig     `mapstructure:"tls"`
    ReadTimeout   time.Duration `mapstructure:"read_timeout"`
    WriteTimeout  time.Duration `mapstructure:"write_timeout"`
    IdleTimeout   time.Duration `mapstructure:"idle_timeout"`
}

type FrontendConfigEntry struct {
    Name     string                 `mapstructure:"name"`
    Protocol string                 `mapstructure:"protocol"` // stdio, websocket, http, sse, tcp_binary
    Enabled  bool                   `mapstructure:"enabled"`
    Config   map[string]interface{} `mapstructure:"config"`
}
```

**Example Configuration:**

```yaml
version: 1
server:
  host: 0.0.0.0
  metrics_port: 9090
  max_connections: 50000
  connection_buffer_size: 65536

  # Multiple frontend configurations
  frontends:
    - name: "websocket-primary"
      protocol: "websocket"
      enabled: true
      config:
        port: 8443
        tls:
          enabled: true
          cert_file: /etc/mcp-gateway/tls/tls.crt
          key_file: /etc/mcp-gateway/tls/tls.key
        max_connections: 10000
        ping_interval: 30s
        allowed_origins:
          - https://app.example.com

    - name: "tcp-binary"
      protocol: "tcp_binary"
      enabled: true
      config:
        port: 8444
        tls:
          enabled: true
          cert_file: /etc/mcp-gateway/tls/tls.crt
          key_file: /etc/mcp-gateway/tls/tls.key
        max_connections: 5000
        health_port: 9002

    - name: "http-api"
      protocol: "http"
      enabled: true
      config:
        port: 8080
        request_path: /api/v1/mcp
        max_request_size: 1048576

    - name: "sse-stream"
      protocol: "sse"
      enabled: true
      config:
        port: 8081
        stream_endpoint: /events
        request_endpoint: /api/v1/request
        keep_alive: 30s

    - name: "stdio-local"
      protocol: "stdio"
      enabled: true
      config:
        modes:
          - type: "unix_socket"
            path: "/tmp/mcp-gateway.sock"
            enabled: true
          - type: "stdin_stdout"
            enabled: false
```

**Validation Updates:**

```go
func ValidateFrontendConfig(config *FrontendConfigEntry) error {
    if config.Name == "" {
        return errors.New("frontend name is required")
    }

    supportedProtocols := []string{"stdio", "websocket", "http", "sse", "tcp_binary"}
    if !contains(supportedProtocols, config.Protocol) {
        return fmt.Errorf("unsupported frontend protocol: %s", config.Protocol)
    }

    // Protocol-specific validation
    switch config.Protocol {
    case "websocket", "http", "sse", "tcp_binary":
        if port, ok := config.Config["port"].(int); !ok || port <= 0 || port > 65535 {
            return fmt.Errorf("invalid port for %s frontend", config.Protocol)
        }
    }

    return nil
}
```

**Estimated Effort:** 1 day, 100-200 LOC

---

## Integration with Gateway Server

### Current Architecture

```
GatewayServer (server.go)
├── handleWebSocket()      ← Handles WebSocket connections
├── acceptTCPConnections() ← Handles TCP binary connections
└── router                 ← Routes to backends
```

### Target Architecture

```
GatewayServer (server.go)
├── frontends []Frontend   ← Array of frontend instances
│   ├── WebSocket Frontend
│   ├── HTTP Frontend
│   ├── SSE Frontend
│   ├── TCP Binary Frontend
│   └── stdio Frontend
└── router                 ← Routes to backends (unchanged)
```

### Bootstrap Changes

**Current (simplified):**
```go
func BootstrapGatewayServer(cfg *config.Config, ...) (*GatewayServer, error) {
    server := &GatewayServer{
        config: cfg,
        router: router,
        // ... other fields
    }

    return server, nil
}

func (s *GatewayServer) Start(ctx context.Context) error {
    // Start HTTP server with WebSocket handler
    go s.server.ListenAndServe()

    // Start TCP listener
    if shouldStartTCP {
        go s.acceptTCPConnections()
    }

    return nil
}
```

**Target:**
```go
func BootstrapGatewayServer(cfg *config.Config, ...) (*GatewayServer, error) {
    server := &GatewayServer{
        config:    cfg,
        router:    router,
        frontends: make([]frontends.Frontend, 0),
    }

    // Create frontend factory
    factory := frontends.CreateFrontendFactory(logger)

    // Create all configured frontends
    for _, frontendCfg := range cfg.Server.Frontends {
        if !frontendCfg.Enabled {
            continue
        }

        frontend, err := factory.CreateFrontend(frontendCfg, router, auth, sessions)
        if err != nil {
            return nil, fmt.Errorf("failed to create %s frontend: %w", frontendCfg.Name, err)
        }

        server.frontends = append(server.frontends, frontend)
    }

    return server, nil
}

func (s *GatewayServer) Start(ctx context.Context) error {
    // Start all frontends
    for _, frontend := range s.frontends {
        if err := frontend.Start(ctx); err != nil {
            return fmt.Errorf("failed to start %s frontend: %w", frontend.GetName(), err)
        }

        s.logger.Info("frontend started",
            zap.String("name", frontend.GetName()),
            zap.String("protocol", frontend.GetProtocol()))
    }

    return nil
}

func (s *GatewayServer) Shutdown(ctx context.Context) error {
    // Stop all frontends
    for _, frontend := range s.frontends {
        if err := frontend.Stop(ctx); err != nil {
            s.logger.Error("failed to stop frontend",
                zap.String("name", frontend.GetName()),
                zap.Error(err))
        }
    }

    return nil
}
```

---

## Testing Requirements

### Unit Tests

Each frontend implementation must include:

1. **Configuration Tests**
   - Default value application
   - Validation logic
   - Invalid configuration handling

2. **Connection Tests**
   - Accept connections
   - Reject invalid connections
   - Connection limit enforcement
   - Per-IP limit enforcement

3. **Authentication Tests**
   - Successful authentication
   - Failed authentication
   - Missing credentials
   - Invalid credentials

4. **Request Processing Tests**
   - Valid requests
   - Invalid JSON-RPC format
   - Routing through RequestRouter
   - Response delivery
   - Error handling

5. **Lifecycle Tests**
   - Start/Stop
   - Graceful shutdown
   - Connection cleanup
   - Resource cleanup

6. **Metrics Tests**
   - Active connection tracking
   - Request count tracking
   - Error count tracking

### Integration Tests

Create `services/gateway/test/integration/frontend_protocols_test.go`:

```go
package integration

func TestWebSocketFrontend_E2E(t *testing.T) {
    // Start gateway with WebSocket frontend
    // Connect WebSocket client
    // Send JSON-RPC request
    // Verify response
    // Disconnect cleanly
}

func TestHTTPFrontend_E2E(t *testing.T) {
    // Start gateway with HTTP frontend
    // Send HTTP POST with JSON-RPC
    // Verify response
}

func TestSSEFrontend_E2E(t *testing.T) {
    // Start gateway with SSE frontend
    // Open SSE stream
    // Send request via POST
    // Verify response on stream
}

func TestTCPBinaryFrontend_E2E(t *testing.T) {
    // Start gateway with TCP Binary frontend
    // Connect TCP client
    // Send binary message
    // Verify binary response
}

func TestMultipleFrontends_Concurrent(t *testing.T) {
    // Start gateway with all frontends enabled
    // Send concurrent requests via different protocols
    // Verify all succeed
}
```

### Load Tests

Create `services/gateway/test/load/frontend_load_test.go`:

```go
func TestWebSocketFrontend_1000Connections(t *testing.T)
func TestHTTPFrontend_HighThroughput(t *testing.T)
func TestSSEFrontend_LongLivedStreams(t *testing.T)
func TestTCPBinaryFrontend_HighThroughput(t *testing.T)
```

### Security Tests

```go
func TestWebSocketFrontend_OriginValidation(t *testing.T)
func TestHTTPFrontend_RequestSizeLimits(t *testing.T)
func TestSSEFrontend_AuthenticationRequired(t *testing.T)
func TestTCPBinaryFrontend_ConnectionLimits(t *testing.T)
```

**Estimated Testing Effort:** 2-3 days, 500-700 LOC

---

## Migration Strategy

### Breaking Changes Approach

**Philosophy:** No backward compatibility. Clean break with immediate removal of old code.

### Phase 1: Implement New Frontends (Weeks 1-3)

**Goal:** Build all frontend implementations

1. Implement all frontend packages (WebSocket, HTTP, SSE, TCP Binary)
2. Add factory and configuration support
3. Write comprehensive tests for each frontend
4. Test frontends in isolation

**Success Criteria:**
- All frontends implemented and tested
- Unit test coverage > 80%
- Integration tests passing
- Load tests successful (10k+ connections)

### Phase 2: Rip and Replace (Week 4)

**Goal:** Remove old code and integrate new frontends

**Breaking Changes:**

1. **Remove from GatewayServer:**
   - `handleWebSocket()` method (server.go:577-612)
   - `authenticateWebSocketRequest()` method (server.go:630-657)
   - `upgradeAndCreateSession()` method (server.go:659-703)
   - `setupAndRegisterConnection()` method (server.go:705-736)
   - `handleClientRead()` method (server.go:739-791)
   - `handleClientWrite()` method (server.go:794-805)
   - `acceptTCPConnections()` method (server.go:1240-1259)
   - `handleAcceptedConnection()` method (server.go:1369-1393)
   - All TCP-related methods and fields

2. **Remove from GatewayServer struct:**
   ```go
   // DELETE THESE FIELDS
   upgrader         websocket.Upgrader
   connections      sync.Map
   tcpConnections   sync.Map
   tcpListener      net.Listener
   tcpHandler       *TCPHandler
   tcpHealthServer  *TCPHealthServer
   ```

3. **Add to GatewayServer struct:**
   ```go
   // NEW FIELDS
   frontends []frontends.Frontend
   ```

4. **Remove configuration fields from ServerConfig:**
   ```go
   // DELETE THESE
   Protocol      string `mapstructure:"protocol"`
   TCPPort       int    `mapstructure:"tcp_port"`
   TCPHealthPort int    `mapstructure:"tcp_health_port"`
   ```

5. **Update BootstrapGatewayServer:**
   - Remove WebSocket upgrader initialization
   - Remove HTTP handler setup for WebSocket
   - Remove TCP listener setup
   - Add frontend factory initialization
   - Add frontend creation loop

6. **Update GatewayServer.Start:**
   - Remove HTTP server startup (handled by frontends)
   - Remove TCP listener startup (handled by frontends)
   - Add frontend startup loop

7. **Update GatewayServer.Shutdown:**
   - Remove WebSocket connection cleanup
   - Remove TCP connection cleanup
   - Add frontend shutdown loop

**Files to Delete:**
- `services/gateway/internal/server/tcp_handler.go` (if exists)
- `services/gateway/internal/server/tcp_health.go` (move to frontends/tcp/)
- `services/gateway/internal/server/tcp_health_test.go` (move to frontends/tcp/)

**Tests to Update/Delete:**

Delete these test files (testing old implementation):
- `services/gateway/internal/server/websocket_origin_test.go` (replace with frontend test)
- `services/gateway/internal/server/per_ip_limit_test.go` (replace with frontend test)
- Tests in `services/gateway/internal/server/server_test.go` that test removed methods:
  - `TestGatewayServer_handleWebSocket_*`
  - `TestGatewayServer_processClientMessage`
  - `TestGatewayServer_sendResponse`
  - `TestGatewayServer_sendErrorResponse`

Update these test files:
- `services/gateway/internal/server/server_test.go` - Keep only tests for remaining methods
- `services/gateway/test/integration/gateway_integration_test.go` - Update to use new frontend config
- `services/gateway/test/integration/e2e_protocol_flows_test.go` - Update to use new frontends

**Configuration Migration:**

Old (DELETED):
```yaml
server:
  host: 0.0.0.0
  port: 8443
  protocol: both
  tcp_port: 8444
  tcp_health_port: 9002
```

New (REQUIRED):
```yaml
server:
  host: 0.0.0.0  # Only used for metrics server
  metrics_port: 9090
  max_connections: 50000

  frontends:
    - name: "websocket-primary"
      protocol: "websocket"
      enabled: true
      config:
        host: 0.0.0.0
        port: 8443
        tls:
          enabled: true
          cert_file: /etc/mcp-gateway/tls/tls.crt
          key_file: /etc/mcp-gateway/tls/tls.key

    - name: "tcp-binary"
      protocol: "tcp_binary"
      enabled: true
      config:
        host: 0.0.0.0
        port: 8444
        health_port: 9002
        tls:
          enabled: true
          cert_file: /etc/mcp-gateway/tls/tls.crt
          key_file: /etc/mcp-gateway/tls/tls.key
```

**Success Criteria:**
- All old code removed
- All tests passing with new architecture
- Configuration examples updated
- Documentation reflects new architecture only

### Phase 3: Documentation and Cleanup (Week 4)

**Goal:** Update all documentation and examples

1. Update README.md with new frontend configuration
2. Update configuration examples
3. Update deployment manifests (Kubernetes, Docker Compose)
4. Update troubleshooting guides
5. Add migration guide (for users upgrading)

**User-Facing Breaking Changes Documentation:**

Create `docs/BREAKING_CHANGES.md`:
```markdown
# Breaking Changes - Frontend Overhaul

## Configuration Changes

The gateway configuration format has changed significantly. The old `server.protocol`,
`server.port`, and `server.tcp_port` fields have been removed in favor of a flexible
frontend configuration system.

### Old Configuration (NO LONGER SUPPORTED)
```yaml
server:
  port: 8443
  protocol: both
  tcp_port: 8444
```

### New Configuration (REQUIRED)
```yaml
server:
  frontends:
    - name: "websocket"
      protocol: "websocket"
      enabled: true
      config:
        port: 8443

    - name: "tcp"
      protocol: "tcp_binary"
      enabled: true
      config:
        port: 8444
```

## Migration Steps

1. Update your configuration file to use the new `frontends` array
2. Each frontend now has its own configuration section
3. Enable/disable frontends individually
4. Multiple frontends of the same protocol are now supported
```

**Success Criteria:**
- All documentation updated
- No references to old configuration format
- Migration guide published
- Breaking changes clearly documented

---

## Effort Estimation

### Without Backward Compatibility

| Component | Complexity | Estimated LOC | Effort (Days) |
|-----------|------------|---------------|---------------|
| **WebSocket Frontend** | Medium | 800-1000 | 3-5 |
| **HTTP Frontend** | Low | 400-600 | 2-3 |
| **SSE Frontend** | Medium-High | 700-900 | 4-6 |
| **TCP Binary Frontend** | Medium | 600-800 | 3-4 |
| **Factory Updates** | Low | 200-300 | 1 |
| **Configuration** | Low | 100-200 | 1 |
| **Remove Old Code** | Low | -1500 LOC | 1-2 |
| **Fix/Update Tests** | Medium | 300-500 | 2-3 |
| **Integration Tests** | Medium | 500-700 | 2-3 |
| **Load Tests** | Medium | 200-300 | 1-2 |
| **Documentation** | Low | - | 1-2 |
| **Total** | | **3300-4500** | **18-28 days** |

**Time Saved:** 1-2 days by removing backward compatibility work

### Team Allocation

**Option 1: Serial Development (Single Developer)**
- Duration: 3.5-5.5 weeks
- Risk: Longer time to completion
- Benefit: Consistent architecture, single point of ownership

**Option 2: Parallel Development (Multiple Developers)**
- Developer 1: WebSocket + TCP Binary frontends (2 weeks)
- Developer 2: HTTP + SSE frontends (2 weeks)
- Developer 3: Configuration, factory, remove old code, fix tests (2 weeks)
- Duration: 2-3 weeks
- Risk: Need coordination on interfaces, merge conflicts
- Benefit: Faster completion

**Recommended:** Option 2 with daily sync meetings

---

## Success Criteria

### Functional Requirements

- ✅ All frontends implement the `Frontend` interface
- ✅ WebSocket frontend accepts connections on configured port
- ✅ HTTP frontend accepts JSON-RPC over HTTP POST
- ✅ SSE frontend supports dual endpoint pattern
- ✅ TCP Binary frontend uses existing wire protocol
- ✅ All frontends support authentication
- ✅ All frontends track metrics correctly
- ✅ Graceful shutdown for all frontends
- ✅ Connection limits enforced (global and per-IP)

### Non-Functional Requirements

- ✅ Support 10,000+ concurrent connections per frontend
- ✅ Request latency < 10ms (p95)
- ✅ Memory usage stable under load
- ✅ No connection leaks
- ✅ Graceful degradation under overload
- ✅ Test coverage > 80% for new code
- ✅ Zero breaking changes during migration

### Documentation Requirements

- ✅ Configuration examples for each frontend
- ✅ Migration guide from old to new architecture
- ✅ API documentation for Frontend interface
- ✅ Troubleshooting guide
- ✅ Performance tuning guide

---

## Implementation Decisions

### 1. Default Configuration

**Decision:** Only WebSocket and TCP Binary frontends enabled by default (matching current behavior)

```yaml
frontends:
  - name: "websocket"
    protocol: "websocket"
    enabled: true
    config:
      port: 8443

  - name: "tcp-binary"
    protocol: "tcp_binary"
    enabled: true
    config:
      port: 8444
```

**Rationale:** Maintains parity with current default behavior (protocol: both)

### 2. Metrics

**Decision:** Unified metrics endpoint at `:9090/metrics` with frontend labels

```
mcp_frontend_connections_active{frontend="websocket",protocol="websocket"} 150
mcp_frontend_connections_active{frontend="tcp-binary",protocol="tcp_binary"} 75
mcp_frontend_requests_total{frontend="websocket",protocol="websocket"} 5000
```

**Rationale:** Single Prometheus scrape endpoint, filterable by frontend/protocol

### 3. Health Checks

**Decision:** Unified health endpoint at `:9090/health` that checks all frontends

```json
{
  "status": "healthy",
  "frontends": {
    "websocket": {"status": "healthy", "connections": 150},
    "tcp-binary": {"status": "healthy", "connections": 75}
  }
}
```

**Rationale:** Kubernetes liveness/readiness probes need single endpoint

### 4. TLS Configuration

**Decision:** Per-frontend TLS configuration with optional global defaults

```yaml
# Global defaults (optional)
tls:
  cert_file: /etc/mcp-gateway/tls/tls.crt
  key_file: /etc/mcp-gateway/tls/tls.key
  min_version: "1.3"

frontends:
  - name: "websocket"
    protocol: "websocket"
    enabled: true
    config:
      port: 8443
      tls:  # Override global defaults
        enabled: true
        cert_file: /etc/certs/websocket.crt
        key_file: /etc/certs/websocket.key
```

**Rationale:** Flexibility for different certs per frontend while allowing shared config

### 5. Code Removal Strategy

**Decision:** Delete immediately, don't deprecate

**Files to Delete:**
- `GatewayServer.handleWebSocket` and related methods (500+ LOC)
- `GatewayServer.acceptTCPConnections` and related methods (400+ LOC)
- `services/gateway/internal/server/tcp_health.go` → Move to `frontends/tcp/health.go`
- Old WebSocket/TCP test files that test deleted code

**Rationale:** Clean break, no technical debt, simpler codebase

### 6. Test Strategy

**Decision:** Delete old tests, write new frontend-specific tests

**Test Organization:**
```
services/gateway/internal/frontends/
├── websocket/
│   ├── frontend_test.go           (unit tests)
│   └── frontend_integration_test.go (integration tests)
├── tcp/
│   ├── frontend_test.go
│   └── frontend_integration_test.go
└── ...
```

**Rationale:** Tests closer to implementation, easier to maintain

---

## References

### Existing Code

- **Frontend Interface:** `services/gateway/internal/frontends/interface.go`
- **stdio Frontend:** `services/gateway/internal/frontends/stdio/frontend.go`
- **WebSocket Backend:** `services/gateway/internal/backends/websocket/backend.go`
- **SSE Backend:** `services/gateway/internal/backends/sse/backend.go`
- **Wire Protocol:** `services/gateway/pkg/wire/transport.go`
- **Current Server:** `services/gateway/internal/server/server.go`

### External References

- [Model Context Protocol Specification](https://modelcontextprotocol.io)
- [JSON-RPC 2.0 Specification](https://www.jsonrpc.org/specification)
- [Server-Sent Events Specification](https://html.spec.whatwg.org/multipage/server-sent-events.html)
- [WebSocket Protocol RFC 6455](https://tools.ietf.org/html/rfc6455)

---

## Appendix: Code Examples

### Example Frontend Implementation

```go
// services/gateway/internal/frontends/websocket/frontend.go
package websocket

func (f *Frontend) Start(ctx context.Context) error {
    f.mu.Lock()
    defer f.mu.Unlock()

    if f.running {
        return fmt.Errorf("frontend already running")
    }

    // Create HTTP server for WebSocket upgrade
    mux := http.NewServeMux()
    mux.HandleFunc("/", f.handleWebSocketUpgrade)

    f.server = &http.Server{
        Addr:         fmt.Sprintf("%s:%d", f.config.Host, f.config.Port),
        Handler:      mux,
        ReadTimeout:  f.config.ReadTimeout,
        WriteTimeout: f.config.WriteTimeout,
    }

    // Start server
    go func() {
        var err error
        if f.config.TLS.Enabled {
            err = f.server.ListenAndServeTLS(f.config.TLS.CertFile, f.config.TLS.KeyFile)
        } else {
            err = f.server.ListenAndServe()
        }

        if err != nil && err != http.ErrServerClosed {
            f.logger.Error("server error", zap.Error(err))
        }
    }()

    f.running = true
    f.updateMetrics(func(m *FrontendMetrics) {
        m.IsRunning = true
    })

    f.logger.Info("websocket frontend started",
        zap.String("address", f.server.Addr))

    return nil
}

func (f *Frontend) handleWebSocketUpgrade(w http.ResponseWriter, r *http.Request) {
    // Connection limit check
    if f.connCount >= int64(f.config.MaxConnections) {
        http.Error(w, "connection limit reached", http.StatusServiceUnavailable)
        return
    }

    // Origin validation
    if !f.isAllowedOrigin(r.Header.Get("Origin")) {
        http.Error(w, "origin not allowed", http.StatusForbidden)
        return
    }

    // Authentication
    authClaims, err := f.auth.Authenticate(r.Context(), extractCredentials(r))
    if err != nil {
        http.Error(w, "authentication failed", http.StatusUnauthorized)
        return
    }

    // Upgrade to WebSocket
    conn, err := f.upgrader.Upgrade(w, r, nil)
    if err != nil {
        f.logger.Error("upgrade failed", zap.Error(err))
        return
    }

    // Create session
    sessionID, err := f.sessions.CreateSession(r.Context(), authClaims.Subject)
    if err != nil {
        conn.Close()
        return
    }

    // Create client connection
    clientConn := &ClientConnection{
        id:       sessionID,
        conn:     conn,
        created:  time.Now(),
        lastUsed: time.Now(),
    }

    // Register connection
    f.connMu.Lock()
    f.connections[sessionID] = clientConn
    atomic.AddInt64(&f.connCount, 1)
    f.connMu.Unlock()

    f.updateMetrics(func(m *FrontendMetrics) {
        m.ActiveConnections++
        m.TotalConnections++
    })

    // Handle connection in background
    f.wg.Add(1)
    go f.handleConnection(r.Context(), clientConn)
}
```

### Example Configuration Parsing

```go
// services/gateway/internal/frontends/factory.go

func (f *DefaultFactory) createWebSocketFrontend(
    config FrontendConfig,
    router RequestRouter,
    auth AuthProvider,
    sessions SessionManager,
) (Frontend, error) {
    // Parse WebSocket-specific config
    wsConfig := websocket.Config{}

    if port, ok := config.Config["port"].(int); ok {
        wsConfig.Port = port
    }

    if host, ok := config.Config["host"].(string); ok {
        wsConfig.Host = host
    }

    if maxConn, ok := config.Config["max_connections"].(int); ok {
        wsConfig.MaxConnections = maxConn
    }

    // Parse TLS config if present
    if tlsConfig, ok := config.Config["tls"].(map[string]interface{}); ok {
        wsConfig.TLS = parseTLSConfig(tlsConfig)
    }

    // Parse origins
    if origins, ok := config.Config["allowed_origins"].([]interface{}); ok {
        for _, origin := range origins {
            if originStr, ok := origin.(string); ok {
                wsConfig.AllowedOrigins = append(wsConfig.AllowedOrigins, originStr)
            }
        }
    }

    // Create frontend
    return websocket.CreateWebSocketFrontend(
        config.Name,
        wsConfig,
        router,
        auth,
        sessions,
        f.logger,
    ), nil
}
```

---

**Document Version:** 1.0
**Last Updated:** 2025-10-01
**Author:** MCP Bridge Team
**Status:** Planning
