# Distributed Tracing Guide

## Overview

MCP Bridge implements comprehensive distributed tracing using OpenTelemetry, providing end-to-end visibility across all service interactions. This enables:

- **Request Flow Visualization**: Track requests from Router → Gateway → MCP Server
- **Performance Analysis**: Identify bottlenecks and optimize critical paths
- **Error Tracking**: Correlate errors across service boundaries
- **Service Dependencies**: Understand service interactions and dependencies

## Architecture

### Components

1. **Router Service**: Entry point for client requests
   - Initiates trace context for all requests
   - Propagates context to gateway connections
   - Records WebSocket and stdio events

2. **Gateway Service**: Routes requests to MCP servers
   - Continues traces from router
   - Creates child spans for backend connections
   - Records authentication, routing, and pool operations

3. **Common Tracing Library**: Shared utilities
   - WebSocket-specific attributes
   - MCP protocol attributes
   - Connection pool metrics

### Trace Flow

```
Client Request
    │
    ├─[Router Span]────────────────┐
    │   ├─ Authentication          │
    │   ├─ Connection Pool Lookup  │
    │   └─ WebSocket Message       │
    │                              │
    └─[Gateway Span]───────────────┤
        ├─ Request Validation      │
        ├─ Rate Limiting           │
        ├─ Service Discovery       │
        └─ Backend Connection      │
                                   │
         [MCP Server Response]─────┘
```

## Configuration

### Basic Configuration

```yaml
# Router configuration
tracing:
  enabled: true
  service_name: "mcp-router"
  service_version: "1.0.0"
  environment: "production"
  
  # Sampling
  sampler_type: "traceidratio"  # Options: always_on, always_off, traceidratio
  sampler_param: 0.1             # Sample 10% of traces
  
  # Export
  exporter_type: "otlp"          # Options: otlp, stdout
  otlp_endpoint: "http://localhost:4317"
  otlp_insecure: true
```

### Gateway Configuration

```yaml
# Gateway configuration
tracing:
  enabled: true
  service_name: "mcp-gateway"
  service_version: "1.0.0"
  environment: "production"
  
  # Same structure as router
  sampler_type: "traceidratio"
  sampler_param: 0.1
  
  exporter_type: "otlp"
  otlp_endpoint: "http://localhost:4317"
  otlp_insecure: true
```

### Sampling Strategies

1. **Always On** (`always_on`)
   - Samples all traces
   - Use for debugging or low-traffic environments

2. **Always Off** (`always_off`)
   - Disables sampling
   - Use when tracing overhead must be minimized

3. **Trace ID Ratio** (`traceidratio`)
   - Probabilistic sampling based on trace ID
   - Ensures consistent sampling decisions across services
   - `sampler_param`: 0.0 (0%) to 1.0 (100%)

### Exporters

1. **OTLP Exporter** (`otlp`)
   - Sends traces to OpenTelemetry Collector
   - Supports gRPC protocol
   - Configure with `otlp_endpoint` and `otlp_insecure`

2. **Stdout Exporter** (`stdout`)
   - Prints traces to standard output
   - Useful for debugging and development
   - Human-readable format

## Instrumentation

### Router Service

The router automatically instruments:

- **Stdio Operations**
  ```go
  ctx, span := t.tracer.StartSpan(ctx, "stdio.read",
      trace.WithSpanKind(trace.SpanKindServer))
  defer span.End()
  ```

- **Gateway Connections**
  ```go
  ctx, span := t.tracer.StartSpan(ctx, "gateway.connect",
      trace.WithSpanKind(trace.SpanKindClient))
  defer span.End()
  ```

- **Message Routing**
  ```go
  span.SetAttributes(
      attribute.String("mcp.method", method),
      attribute.String("mcp.message.id", messageID),
  )
  ```

### Gateway Service

The gateway instruments:

- **HTTP/WebSocket Requests**
  ```go
  // Automatic instrumentation via middleware
  handler = tracer.HTTPMiddleware(handler)
  ```

- **Authentication**
  ```go
  ctx, span := tracer.StartSpan(ctx, "auth.verify")
  span.SetAttributes(tracing.AuthenticationAttributes(
      authType, success, userID)...)
  ```

- **Backend Connections**
  ```go
  ctx, span := tracer.StartSpan(ctx, "backend.request")
  span.SetAttributes(tracing.GatewayRouteAttributes(
      serviceName, targetHost, targetPort)...)
  ```

### Common Attributes

The tracing library provides standard attributes:

```go
// WebSocket connection attributes
attrs := tracing.WebSocketSpanAttributes(r, remoteAddr)

// MCP message attributes
attrs := tracing.MCPMessageAttributes(msgType, method, id)

// Connection pool attributes
attrs := tracing.ConnectionPoolAttributes(poolType, size, active, idle)

// Rate limiting attributes
attrs := tracing.RateLimitAttributes(limited, limit, remaining, resetTime)
```

## Context Propagation

### HTTP Headers

Trace context is propagated via W3C Trace Context headers:
- `traceparent`: Contains trace ID, span ID, and flags
- `tracestate`: Vendor-specific trace information

### WebSocket Headers

For WebSocket connections, context is propagated during handshake:

```go
// Inject context into WebSocket headers
headers := make(map[string]string)
tracer.InjectTraceContextToMap(ctx, headers)

// Extract context from headers
ctx = tracer.ExtractTraceContextFromMap(ctx, headers)
```

### MCP Protocol

Trace context can be included in MCP message metadata:

```json
{
  "jsonrpc": "2.0",
  "method": "tools/list",
  "id": "1",
  "params": {
    "_meta": {
      "traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
    }
  }
}
```

## Deployment

### With OpenTelemetry Collector

1. **Deploy OpenTelemetry Collector**
   ```yaml
   receivers:
     otlp:
       protocols:
         grpc:
           endpoint: 0.0.0.0:4317
   
   processors:
     batch:
       timeout: 1s
       send_batch_size: 1024
   
   exporters:
     jaeger:
       endpoint: jaeger:14250
       tls:
         insecure: true
   
   service:
     pipelines:
       traces:
         receivers: [otlp]
         processors: [batch]
         exporters: [jaeger]
   ```

2. **Configure Services**
   ```yaml
   tracing:
     exporter_type: "otlp"
     otlp_endpoint: "otel-collector:4317"
     otlp_insecure: true
   ```

### With Jaeger

Deploy Jaeger All-in-One:
```bash
docker run -d --name jaeger \
  -p 16686:16686 \
  -p 4317:4317 \
  jaegertracing/all-in-one:latest \
  --collector.otlp.enabled=true
```

### Kubernetes Deployment

Use the provided Helm charts with tracing enabled:

```yaml
# values.yaml
gateway:
  tracing:
    enabled: true
    exporter_type: otlp
    otlp_endpoint: "otel-collector.observability:4317"

router:
  tracing:
    enabled: true
    exporter_type: otlp
    otlp_endpoint: "otel-collector.observability:4317"
```

## Performance Considerations

### Sampling

- **Production**: Use trace ID ratio sampling (0.1-1%)
- **Staging**: Higher sampling rate (10-50%)
- **Development**: Always on sampling

### Resource Usage

- **Memory**: ~2-5MB per service for tracer
- **CPU**: <1% overhead with 1% sampling
- **Network**: ~1KB per trace span

### Best Practices

1. **Use Appropriate Span Names**
   ```go
   // Good: Descriptive and consistent
   "gateway.auth.jwt.verify"
   "router.connection.websocket.read"
   
   // Bad: Generic or unclear
   "operation"
   "process"
   ```

2. **Add Relevant Attributes**
   ```go
   span.SetAttributes(
       attribute.String("service.name", serviceName),
       attribute.Int("request.size", size),
       attribute.Bool("cache.hit", hit),
   )
   ```

3. **Record Errors Properly**
   ```go
   if err != nil {
       span.RecordError(err)
       span.SetStatus(codes.Error, err.Error())
   }
   ```

4. **Use Span Events for Important Moments**
   ```go
   span.AddEvent("circuit_breaker_opened",
       trace.WithAttributes(
           attribute.Int("failure_count", failures),
       ),
   )
   ```

## Troubleshooting

### No Traces Appearing

1. Check tracing is enabled:
   ```yaml
   tracing:
     enabled: true
   ```

2. Verify exporter configuration:
   ```bash
   # Check connectivity
   telnet localhost 4317
   ```

3. Enable debug logging:
   ```yaml
   logging:
     level: debug
   ```

### Missing Spans

1. Ensure context propagation:
   ```go
   // Always pass context
   ctx = tracer.ExtractTraceContext(ctx, headers)
   ```

2. Check sampling configuration:
   ```yaml
   sampler_type: "always_on"  # For debugging
   ```

### Performance Impact

1. Reduce sampling rate:
   ```yaml
   sampler_param: 0.001  # 0.1%
   ```

2. Use batch exporter:
   ```yaml
   # Automatically batched by SDK
   exporter_type: "otlp"
   ```

## Example Traces

### Successful Request Flow

```
mcp-router: stdio.read (10ms)
  └─ mcp-router: gateway.request (8ms)
      └─ mcp-gateway: http.request (7ms)
          ├─ mcp-gateway: auth.verify (1ms)
          ├─ mcp-gateway: rate_limit.check (0.5ms)
          └─ mcp-gateway: backend.forward (5ms)
```

### Error Trace

```
mcp-router: stdio.read (102ms) [ERROR]
  └─ mcp-router: gateway.request (100ms) [ERROR]
      └─ mcp-gateway: http.request (15ms) [ERROR]
          ├─ mcp-gateway: auth.verify (1ms) [OK]
          └─ mcp-gateway: backend.forward (13ms) [ERROR]
              └─ error: "connection refused"
```

## Integration with Monitoring

### Grafana Dashboards

Import trace data from Jaeger:
1. Add Jaeger data source
2. Use Trace ID in log queries
3. Create service dependency graphs

### Alerts

Configure alerts based on trace data:
- High error rates
- Increased latency
- Service availability

### SLO Tracking

Use trace data for SLO metrics:
- Request success rate
- P95/P99 latency
- Error budget tracking