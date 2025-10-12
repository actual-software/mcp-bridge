# MCP Gateway Architecture

## Overview

The MCP Gateway is a universal protocol gateway that enables any MCP client to connect to any MCP server regardless of protocol. It provides a high-performance, scalable architecture with comprehensive protocol support, intelligent load balancing, service discovery, and production-ready resilience features.

## Universal Protocol Architecture

```mermaid
graph TB
    subgraph "Client Layer"
        WS_Client[WebSocket Clients]
        HTTP_Client[HTTP/REST Clients]
        SSE_Client[SSE Clients]
        TCP_Client[TCP Binary Clients]
        STDIO_Client[stdio/CLI Clients]
    end

    subgraph "MCP Gateway - Frontend Layer"
        WS_Frontend[WebSocket Frontend<br/>Port 8443]
        HTTP_Frontend[HTTP Frontend<br/>Port 8080]
        SSE_Frontend[SSE Frontend<br/>Port 8081]
        TCP_Frontend[TCP Binary Frontend<br/>Port 8444]
        STDIO_Frontend[stdio Frontend<br/>Unix Socket]
    end

    subgraph "MCP Gateway - Core"
        Auth[Authentication<br/>JWT/OAuth2/mTLS]
        RateLimit[Rate Limiter<br/>Redis/Memory]
        Router[Request Router]
        LB[Load Balancer<br/>Cross-Protocol]
        Discovery[Service Discovery<br/>K8s/Consul/Static]
        CircuitBreaker[Circuit Breaker]
        SessionMgr[Session Manager<br/>Redis-backed]
    end

    subgraph "MCP Gateway - Backend Layer"
        STDIO_Backend[stdio Backend]
        WS_Backend[WebSocket Backend]
        SSE_Backend[SSE Backend]
        HTTP_Backend[HTTP Backend]
        TCP_Backend[TCP Binary Backend]
    end

    subgraph "MCP Server Layer"
        Server1[MCP Server 1<br/>stdio]
        Server2[MCP Server 2<br/>WebSocket]
        Server3[MCP Server 3<br/>SSE]
        Server4[MCP Server 4<br/>HTTP]
        Server5[MCP Server 5<br/>TCP]
    end

    subgraph "Shared State"
        Redis[(Redis)<br/>Sessions/Rate Limiting]
    end

    subgraph "Observability"
        Metrics[Prometheus Metrics<br/>Port 9090]
        Logs[Structured Logging<br/>JSON]
        Health[Health Endpoints<br/>Port 8090]
    end

    WS_Client --> WS_Frontend
    HTTP_Client --> HTTP_Frontend
    SSE_Client --> SSE_Frontend
    TCP_Client --> TCP_Frontend
    STDIO_Client --> STDIO_Frontend

    WS_Frontend --> Auth
    HTTP_Frontend --> Auth
    SSE_Frontend --> Auth
    TCP_Frontend --> Auth
    STDIO_Frontend --> Auth

    Auth --> RateLimit
    RateLimit --> Router
    Router --> LB
    LB --> CircuitBreaker
    CircuitBreaker --> STDIO_Backend
    CircuitBreaker --> WS_Backend
    CircuitBreaker --> SSE_Backend
    CircuitBreaker --> HTTP_Backend
    CircuitBreaker --> TCP_Backend

    Discovery -.->|Updates| LB
    SessionMgr -.->|State| Router
    RateLimit <-.->|Check/Store| Redis
    SessionMgr <-.->|Store/Retrieve| Redis

    STDIO_Backend --> Server1
    WS_Backend --> Server2
    SSE_Backend --> Server3
    HTTP_Backend --> Server4
    TCP_Backend --> Server5

    Router -.->|Export| Metrics
    Auth -.->|Log| Logs
    CircuitBreaker -.->|Report| Health

    style Auth fill:#ffe1e1
    style RateLimit fill:#ffe1e1
    style Router fill:#fff4e1
    style LB fill:#e1f5ff
    style Discovery fill:#e1ffe1
    style CircuitBreaker fill:#ffe1f5
```

## Core Components

### 1. Frontend Layer
Multi-protocol frontend handlers that accept client connections:
- **WebSocket Frontend**: Full bidirectional communication (port 8443)
- **HTTP Frontend**: REST-style request/response (port 8080)
- **SSE Frontend**: Server-Sent Events with dual endpoints (port 8081)
- **TCP Binary Frontend**: High-performance wire protocol (port 8444)
- **stdio Frontend**: Process/CLI integration (Unix socket)

### 2. Authentication & Authorization
Validates and enforces access control:
- JWT token validation with configurable issuers
- OAuth2 token introspection support
- mTLS certificate validation
- Per-message authentication (optional)
- Token claim-based authorization

### 3. Rate Limiting
Multi-layer rate limiting with circuit breaker protection:
- Sliding window algorithm for accurate limiting
- Burst control for traffic spikes
- Redis-backed for distributed state
- Automatic fallback to in-memory
- Per-user and per-IP limits

### 4. Request Router
Protocol-aware message routing:
- Request/response correlation
- Namespace-based routing
- Protocol transformation
- Message validation and sanitization
- Error handling and propagation

### 5. Load Balancer
Intelligent traffic distribution across backends:
- **Cross-Protocol Load Balancing**: Routes across mixed protocol backends
- **Round Robin**: Even distribution
- **Least Connections**: Routes to least loaded endpoint
- **Weighted**: Respects endpoint weights
- **Latency-Based**: Routes based on measured latency

### 6. Service Discovery
Automatic backend discovery and health monitoring:
- **Kubernetes**: Label and annotation-based discovery
- **Consul**: Service catalog integration
- **Static**: Configuration-based backends
- Auto-protocol detection
- Continuous health checks

### 7. Circuit Breaker
Failure detection and graceful degradation:
- Per-endpoint circuit breakers
- Configurable failure thresholds
- Half-open state for recovery testing
- Redis operations protection
- Automatic fallback to in-memory

### 8. Backend Connectors
Protocol-specific backend communication:
- **stdio**: Process spawning and management
- **WebSocket**: Connection pooling and reuse
- **SSE**: Stream management with reconnection
- **HTTP**: Request/response handling
- **TCP Binary**: Wire protocol communication

## Request Flow - WebSocket

```mermaid
sequenceDiagram
    participant Client
    participant WS_Frontend as WebSocket Frontend
    participant Auth
    participant RateLimit as Rate Limiter
    participant Router
    participant LB as Load Balancer
    participant CB as Circuit Breaker
    participant Backend
    participant Server as MCP Server

    Client->>WS_Frontend: WebSocket Upgrade + JWT
    WS_Frontend->>Auth: Validate Token
    Auth-->>WS_Frontend: Token Valid

    WS_Frontend->>Client: WebSocket Connected

    Client->>WS_Frontend: MCP Request
    WS_Frontend->>Auth: Check Per-Message Auth (if enabled)
    Auth->>RateLimit: Check Rate Limit
    RateLimit->>Redis: Increment Counter
    Redis-->>RateLimit: Allowed

    RateLimit->>Router: Route Request
    Router->>Router: Validate & Parse
    Router->>LB: Select Backend
    LB->>Discovery: Get Healthy Endpoints
    Discovery-->>LB: Endpoint List
    LB->>LB: Apply Strategy (Round Robin/Least Conn)

    LB->>CB: Check Circuit State
    CB-->>LB: Closed (OK)

    LB->>Backend: Forward Request
    Backend->>Server: Protocol-Specific Call
    Server-->>Backend: Response
    Backend-->>Router: Forward Response
    Router-->>WS_Frontend: Send Response
    WS_Frontend-->>Client: MCP Response

    Note over CB,Server: If backend fails repeatedly,<br/>circuit opens
```

## Request Flow - Cross-Protocol

```mermaid
sequenceDiagram
    participant Client as HTTP Client
    participant HTTP_Frontend as HTTP Frontend
    participant Router
    participant LB as Load Balancer
    participant STDIO_Backend as stdio Backend
    participant Server as stdio MCP Server

    Client->>HTTP_Frontend: HTTP POST /api/v1/mcp
    HTTP_Frontend->>Router: Parse JSON-RPC

    Router->>LB: Route to namespace
    LB->>LB: No HTTP backends available
    LB->>LB: Cross-protocol fallback
    LB->>STDIO_Backend: Select stdio backend

    STDIO_Backend->>Server: Write JSON to stdin
    Server->>STDIO_Backend: Read JSON from stdout
    STDIO_Backend->>Router: Return Response

    Router->>HTTP_Frontend: Format HTTP Response
    HTTP_Frontend->>Client: HTTP 200 + JSON body

    Note over LB,STDIO_Backend: Gateway transparently<br/>bridges protocols
```

## Service Discovery Flow

```mermaid
flowchart TD
    Start([Gateway Starts]) --> InitDiscovery[Initialize Service Discovery]

    InitDiscovery --> CheckProviders{Which<br/>Providers?}

    CheckProviders -->|Kubernetes| K8sInit[Connect to K8s API]
    CheckProviders -->|Consul| ConsulInit[Connect to Consul]
    CheckProviders -->|Static| StaticInit[Load Static Config]

    K8sInit --> K8sWatch[Watch Services with<br/>mcp.bridge/enabled]
    ConsulInit --> ConsulWatch[Watch Services with<br/>mcp- prefix]
    StaticInit --> StaticLoad[Load Configured Backends]

    K8sWatch --> DetectProtocol{Auto-detect<br/>Protocol?}
    ConsulWatch --> DetectProtocol
    StaticLoad --> DetectProtocol

    DetectProtocol -->|Yes| ProbeBackend[Probe Backend Protocol]
    DetectProtocol -->|No| UseAnnotation[Use Configured Protocol]

    ProbeBackend --> RegisterBackend[Register Backend in LB]
    UseAnnotation --> RegisterBackend

    RegisterBackend --> StartHealthCheck[Start Health Check]

    StartHealthCheck --> HealthCheck{Health<br/>Check Pass?}

    HealthCheck -->|Pass| MarkHealthy[Mark Endpoint Healthy]
    HealthCheck -->|Fail| MarkUnhealthy[Mark Endpoint Unhealthy]

    MarkHealthy --> UpdateLB[Update Load Balancer]
    MarkUnhealthy --> UpdateLB

    UpdateLB --> Wait[Wait for Refresh Interval]
    Wait --> HealthCheck

    style K8sWatch fill:#e1f5ff
    style ConsulWatch fill:#e1f5ff
    style RegisterBackend fill:#e1ffe1
    style MarkHealthy fill:#e1ffe1
    style MarkUnhealthy fill:#ffe1e1
```

## Load Balancing Decision Flow

```mermaid
flowchart TD
    Start([Request Received]) --> GetNamespace[Extract Target Namespace]

    GetNamespace --> GetBackends[Get All Backends for Namespace]
    GetBackends --> FilterHealthy[Filter Healthy Backends]

    FilterHealthy --> CheckCB{Check Circuit<br/>Breakers}

    CheckCB --> FilterClosed[Filter Closed/Half-Open Circuits]
    FilterClosed --> HasBackends{Any Healthy<br/>Backends?}

    HasBackends -->|No| CrossProtocol{Cross-Protocol<br/>Enabled?}
    HasBackends -->|Yes| ApplyStrategy[Apply LB Strategy]

    CrossProtocol -->|Yes| FindAltProtocol[Find Alternative Protocol]
    CrossProtocol -->|No| ReturnError[Return 503 Service Unavailable]

    FindAltProtocol --> HasAlt{Alternative<br/>Found?}
    HasAlt -->|Yes| ApplyStrategy
    HasAlt -->|No| ReturnError

    ApplyStrategy --> CheckStrategy{Which<br/>Strategy?}

    CheckStrategy -->|Round Robin| RoundRobin[Select Next in Rotation]
    CheckStrategy -->|Least Connections| LeastConn[Select Least Loaded]
    CheckStrategy -->|Weighted| Weighted[Select by Weight]
    CheckStrategy -->|Latency-Based| Latency[Select Lowest Latency]

    RoundRobin --> SendRequest[Send Request to Backend]
    LeastConn --> SendRequest
    Weighted --> SendRequest
    Latency --> SendRequest

    SendRequest --> TrackMetrics[Track Connection/Latency]
    TrackMetrics --> AwaitResponse{Response<br/>Status?}

    AwaitResponse -->|Success| RecordSuccess[Record Success]
    AwaitResponse -->|Error| RecordFailure[Record Failure]
    AwaitResponse -->|Timeout| RecordFailure

    RecordSuccess --> UpdateCB[Update Circuit Breaker]
    RecordFailure --> UpdateCB

    UpdateCB --> CheckThreshold{Failure<br/>Threshold?}
    CheckThreshold -->|Exceeded| OpenCircuit[Open Circuit]
    CheckThreshold -->|OK| End([Return Response])

    OpenCircuit --> End
    ReturnError --> End

    style FilterHealthy fill:#e1ffe1
    style ApplyStrategy fill:#fff4e1
    style SendRequest fill:#e1f5ff
    style RecordFailure fill:#ffe1e1
    style OpenCircuit fill:#ffe1e1
```

## Circuit Breaker State Machine

```mermaid
stateDiagram-v2
    [*] --> Closed: Circuit initialized

    Closed --> Open: Failure threshold exceeded
    Closed --> Closed: Request success

    Open --> HalfOpen: Timeout expired
    Open --> Open: Requests rejected

    HalfOpen --> Closed: Success threshold reached
    HalfOpen --> Open: Any failure
    HalfOpen --> HalfOpen: Limited requests allowed

    Closed: State: 0 (Normal)
    Closed: All requests pass through
    Closed: Failures tracked

    Open: State: 1 (Failed)
    Open: All requests rejected
    Open: Fallback activated
    Open: Timeout timer running

    HalfOpen: State: 2 (Testing)
    HalfOpen: Limited requests allowed
    HalfOpen: Testing recovery
    HalfOpen: One failure reopens circuit
```

## Rate Limiting Flow

```mermaid
sequenceDiagram
    participant Client
    participant Frontend
    participant Auth
    participant RateLimit as Rate Limiter
    participant Redis
    participant CircuitBreaker as CB
    participant Handler

    Client->>Frontend: Request + JWT
    Frontend->>Auth: Extract Token Claims
    Auth->>Auth: Parse rate_limit claim

    Auth->>RateLimit: Check Rate Limit (user_id)
    RateLimit->>CircuitBreaker: Check Redis Circuit

    alt Circuit Closed (Redis Available)
        CircuitBreaker->>Redis: INCR key with TTL
        Redis-->>CircuitBreaker: Current Count
        CircuitBreaker-->>RateLimit: Count from Redis

        alt Within Limit
            RateLimit-->>Frontend: Allowed
            Frontend->>Handler: Process Request
            Handler-->>Client: Response
        else Rate Limit Exceeded
            RateLimit-->>Frontend: Rate Limited
            Frontend-->>Client: 429 Too Many Requests
        end
    else Circuit Open (Redis Failed)
        CircuitBreaker->>RateLimit: Use In-Memory Fallback
        RateLimit->>RateLimit: Check Local Counter

        alt Within Limit
            RateLimit-->>Frontend: Allowed (Degraded)
            Frontend->>Handler: Process Request
            Handler-->>Client: Response
        else Rate Limit Exceeded
            RateLimit-->>Frontend: Rate Limited
            Frontend-->>Client: 429 Too Many Requests
        end
    end

    Note over CircuitBreaker,Redis: Circuit breaker protects<br/>against Redis failures
```

## Session Management

```mermaid
flowchart TD
    Start([Client Connects]) --> CheckSession{Existing<br/>Session?}

    CheckSession -->|Yes| LoadSession[Load Session from Redis]
    CheckSession -->|No| CreateSession[Create New Session]

    LoadSession --> ValidateSession{Session<br/>Valid?}
    ValidateSession -->|Yes| RestoreState[Restore Client State]
    ValidateSession -->|No| CreateSession

    CreateSession --> GenerateID[Generate Session ID]
    GenerateID --> StoreRedis[Store in Redis with TTL]

    RestoreState --> ProcessRequest[Process Request]
    StoreRedis --> ProcessRequest

    ProcessRequest --> UpdateSession[Update Session State]
    UpdateSession --> SaveRedis[Save to Redis]

    SaveRedis --> CheckActivity{Client<br/>Active?}
    CheckActivity -->|Yes| ExtendTTL[Extend Session TTL]
    CheckActivity -->|No| AllowExpire[Allow Natural Expiry]

    ExtendTTL --> Wait[Wait for Next Request]
    AllowExpire --> Cleanup[Session Expires]
    Wait --> ProcessRequest

    Cleanup --> End([Session Ended])

    style CreateSession fill:#e1ffe1
    style LoadSession fill:#e1f5ff
    style SaveRedis fill:#fff4e1
    style Cleanup fill:#ffe1e1
```

## Authentication Flow

```mermaid
flowchart TD
    Start([Request Received]) --> CheckHeader{Authorization<br/>Header?}

    CheckHeader -->|No| Reject[Reject: 401 Unauthorized]
    CheckHeader -->|Yes| ParseHeader[Parse Bearer Token]

    ParseHeader --> CheckType{Auth<br/>Type?}

    CheckType -->|JWT| ValidateJWT[Validate JWT Signature]
    CheckType -->|OAuth2| IntrospectToken[Introspect OAuth2 Token]
    CheckType -->|mTLS| ValidateCert[Validate Client Certificate]

    ValidateJWT --> CheckExpiry{Token<br/>Expired?}
    IntrospectToken --> CheckExpiry
    ValidateCert --> CheckExpiry

    CheckExpiry -->|Yes| Reject
    CheckExpiry -->|No| CheckClaims[Verify Required Claims]

    CheckClaims --> CheckScopes{Required<br/>Scopes?}

    CheckScopes -->|Missing| Reject2[Reject: 403 Forbidden]
    CheckScopes -->|Present| ExtractLimits[Extract Rate Limit Claims]

    ExtractLimits --> CacheToken{Per-Message<br/>Auth?}

    CacheToken -->|Yes| CacheValidation[Cache Validation (5 min)]
    CacheToken -->|No| Proceed[Proceed to Rate Limiting]

    CacheValidation --> Proceed
    Reject --> End([Return Error])
    Reject2 --> End
    Proceed --> Next([Next Stage])

    style ValidateJWT fill:#e1f5ff
    style CheckClaims fill:#fff4e1
    style ExtractLimits fill:#e1ffe1
    style Reject fill:#ffe1e1
    style Reject2 fill:#ffe1e1
```

## Performance Characteristics

### Throughput
- **112,000 requests/second** sustained throughput (load tested)
- **P50 latency**: 1.8ms at 10,000 concurrent connections
- **P99 latency**: 5.2ms at 10,000 concurrent connections
- Connection pooling for all backend protocols

### Scalability
- **Horizontal scaling**: Shared state via Redis enables unlimited replicas
- **Connection limits**: 50,000+ concurrent WebSocket connections per instance
- **Backend pooling**: Configurable connection pools per backend
- **Automatic discovery**: Dynamically scales with backend additions/removals

### Resource Efficiency
- **Memory**: ~200MB baseline, ~10KB per active connection
- **CPU**: Efficient goroutine-based concurrency
- **Network**: Zero-copy operations where possible
- **Storage**: Redis for shared state (sessions, rate limits, circuit breakers)

## Security Architecture

### Defense in Depth
1. **Network Layer**: TLS 1.3 for all external connections
2. **Transport Layer**: mTLS for backend communication (optional)
3. **Application Layer**: JWT/OAuth2 token validation
4. **Request Layer**: Input validation and sanitization
5. **Rate Limiting**: Multi-layer DoS protection
6. **Circuit Breakers**: Graceful degradation under attack

### Input Validation
- **Request IDs**: UUID format enforcement
- **Method names**: Alphanumeric + underscore only
- **Namespace**: Path injection prevention
- **Message size**: Configurable limits per protocol
- **UTF-8 enforcement**: All string fields validated

### Security Headers
All HTTP responses include:
- `Strict-Transport-Security`: HSTS enforcement
- `Content-Security-Policy`: XSS prevention
- `X-Frame-Options`: Clickjacking prevention
- `X-Content-Type-Options`: MIME sniffing prevention

## Observability

### Metrics (Prometheus)
- **Connection metrics**: Total, active, rejected by protocol
- **Request metrics**: Total, duration, in-flight by method
- **Protocol metrics**: Messages, bytes by protocol and direction
- **Error metrics**: Auth failures, rate limits, protocol errors
- **Circuit breaker metrics**: State changes, success/failure rates
- **Backend metrics**: Request counts, latencies per endpoint

### Logging (Structured JSON)
- **Request/Response logging**: Trace ID correlation
- **Auth events**: Successes and failures with reasons
- **Service discovery**: Backend additions/removals
- **Circuit breaker**: State changes and reasons
- **Error details**: Full stack traces for debugging

### Health Checks
- **Overall health**: `/health` - All subsystems
- **Liveness**: `/healthz` - Process alive
- **Readiness**: `/ready` - Ready for traffic
- **Frontend health**: `/health/frontend/{protocol}`
- **Backend health**: `/health/backend/{name}`
- **Component health**: `/health/components` - Redis, discovery, etc.

## Future Enhancements

### Planned Features
1. **Request Priority**: Priority queues for critical requests
2. **Advanced Routing**: Content-based routing rules
3. **Protocol Translation**: Automatic protocol negotiation
4. **Distributed Tracing**: Full OpenTelemetry integration
5. **Request Deduplication**: Idempotency key support
6. **A/B Testing**: Traffic splitting for canary deployments

### Extensibility
- **Plugin system**: Custom authentication providers
- **Middleware pipeline**: Request/response transformation
- **Custom metrics**: User-defined metric exporters
- **Protocol adapters**: Support for additional protocols
