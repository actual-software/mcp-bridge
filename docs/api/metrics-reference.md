# Metrics Reference

Complete reference for all Prometheus metrics exposed by MCP Bridge Gateway and Router services.

## Gateway Metrics

Gateway metrics are exposed at `:9090/metrics`.

### Connection Metrics

#### `mcp_gateway_connections_total`
**Type**: Gauge
**Description**: Total number of active WebSocket connections
**Labels**: None
**Usage**: Monitor total WebSocket connection count

#### `mcp_gateway_connections_active`
**Type**: Gauge
**Description**: Number of currently active connections
**Labels**: None
**Usage**: Track real-time active connection count

#### `mcp_gateway_connections_rejected_total`
**Type**: Counter
**Description**: Total number of rejected connections
**Labels**: None
**Usage**: Monitor connection rejection rate due to limits or other failures

### TCP Connection Metrics

#### `mcp_gateway_tcp_connections_total`
**Type**: Gauge
**Description**: Total number of TCP Binary protocol connections
**Labels**: None
**Usage**: Monitor TCP Binary connection count

#### `mcp_gateway_tcp_connections_active`
**Type**: Gauge
**Description**: Number of currently active TCP connections
**Labels**: None
**Usage**: Track real-time active TCP connection count

### Request Metrics

#### `mcp_gateway_requests_total`
**Type**: Counter
**Description**: Total number of MCP requests
**Labels**:
- `method`: MCP method name (e.g., `tools/list`, `tools/call`)
- `status`: Request status (`success`, `error`)

**Usage**: Track request rate by method and status

**Example PromQL**:
```promql
# Total request rate
rate(mcp_gateway_requests_total[5m])

# Error rate by method
rate(mcp_gateway_requests_total{status="error"}[5m])
```

#### `mcp_gateway_request_duration_seconds`
**Type**: Histogram
**Description**: MCP request duration in seconds
**Labels**:
- `method`: MCP method name
- `status`: Request status

**Buckets**: Default Prometheus buckets (0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10)

**Usage**: Monitor request latency and percentiles

**Example PromQL**:
```promql
# p95 latency by method
histogram_quantile(0.95, rate(mcp_gateway_request_duration_seconds_bucket[5m]))

# Average latency
rate(mcp_gateway_request_duration_seconds_sum[5m]) / rate(mcp_gateway_request_duration_seconds_count[5m])
```

#### `mcp_gateway_requests_in_flight`
**Type**: Gauge
**Description**: Number of requests currently being processed
**Labels**: None
**Usage**: Monitor concurrent request processing

### Authentication Metrics

#### `mcp_gateway_auth_failures_total`
**Type**: Counter
**Description**: Total number of authentication failures
**Labels**:
- `reason`: Failure reason (`invalid_token`, `expired_token`, `missing_token`, `invalid_signature`, etc.)

**Usage**: Track authentication failure rate by reason

**Example PromQL**:
```promql
# Auth failure rate by reason
rate(mcp_gateway_auth_failures_total[5m])

# Total auth failures
sum(rate(mcp_gateway_auth_failures_total[5m]))
```

### Routing Metrics

#### `mcp_gateway_routing_errors_total`
**Type**: Counter
**Description**: Total number of routing errors
**Labels**:
- `reason`: Error reason (`no_backend`, `all_backends_down`, `circuit_open`, etc.)

**Usage**: Monitor routing failures

#### `mcp_gateway_endpoint_requests_total`
**Type**: Counter
**Description**: Total requests per backend endpoint
**Labels**:
- `endpoint`: Backend endpoint identifier
- `namespace`: MCP namespace
- `status`: Request status

**Usage**: Track request distribution across backends

**Example PromQL**:
```promql
# Request rate per endpoint
rate(mcp_gateway_endpoint_requests_total[5m])

# Top 5 endpoints by request count
topk(5, sum by (endpoint) (rate(mcp_gateway_endpoint_requests_total[5m])))
```

#### `mcp_gateway_circuit_breaker_state`
**Type**: Gauge
**Description**: Circuit breaker state by endpoint
**Labels**:
- `endpoint`: Backend endpoint identifier

**Values**:
- `0`: Closed (normal operation)
- `1`: Open (circuit is open, requests blocked)
- `2`: Half-open (testing recovery)

**Usage**: Monitor circuit breaker health

**Example PromQL**:
```promql
# Endpoints with open circuit breakers
mcp_gateway_circuit_breaker_state{} == 1

# Alert on circuit breaker open
ALERT CircuitBreakerOpen
  IF mcp_gateway_circuit_breaker_state == 1
  FOR 5m
  ANNOTATIONS {
    summary = "Circuit breaker open for {{ $labels.endpoint }}"
  }
```

### WebSocket Metrics

#### `mcp_gateway_websocket_messages_total`
**Type**: Counter
**Description**: Total WebSocket messages
**Labels**:
- `direction`: Message direction (`inbound`, `outbound`)
- `type`: Message type (`text`, `binary`, `ping`, `pong`)

**Usage**: Track WebSocket message flow

#### `mcp_gateway_websocket_bytes_total`
**Type**: Counter
**Description**: Total WebSocket bytes transferred
**Labels**:
- `direction`: Transfer direction (`inbound`, `outbound`)

**Usage**: Monitor WebSocket bandwidth usage

**Example PromQL**:
```promql
# WebSocket bandwidth (bytes/sec)
rate(mcp_gateway_websocket_bytes_total[5m])

# Inbound vs outbound ratio
rate(mcp_gateway_websocket_bytes_total{direction="inbound"}[5m])
/ rate(mcp_gateway_websocket_bytes_total{direction="outbound"}[5m])
```

### TCP Binary Protocol Metrics

#### `mcp_gateway_tcp_messages_total`
**Type**: Counter
**Description**: Total TCP Binary protocol messages by type
**Labels**:
- `direction`: Message direction (`inbound`, `outbound`)
- `type`: Message type (`request`, `response`, `notification`)

**Usage**: Track TCP Binary message flow

#### `mcp_gateway_tcp_bytes_total`
**Type**: Counter
**Description**: Total TCP Binary protocol bytes transferred
**Labels**:
- `direction`: Transfer direction (`inbound`, `outbound`)

**Usage**: Monitor TCP Binary bandwidth

#### `mcp_gateway_tcp_protocol_errors_total`
**Type**: Counter
**Description**: Total TCP Binary protocol errors
**Labels**:
- `error_type`: Error type (`invalid_version`, `invalid_length`, `parse_error`, etc.)

**Usage**: Track TCP Binary protocol errors

#### `mcp_gateway_tcp_message_duration_seconds`
**Type**: Histogram
**Description**: TCP Binary message processing duration
**Labels**:
- `message_type`: Type of message processed

**Buckets**: Default Prometheus buckets

**Usage**: Monitor TCP Binary message latency

### Error Metrics

#### `mcp_gateway_errors_total`
**Type**: Counter
**Description**: Total number of errors by code and component
**Labels**:
- `code`: Error code
- `component`: Component that generated error
- `operation`: Operation that failed

**Usage**: Track error rate across system

#### `mcp_gateway_errors_by_type_total`
**Type**: Counter
**Description**: Total number of errors by error type
**Labels**:
- `type`: Error type category

**Usage**: Track errors by type classification

#### `mcp_gateway_errors_by_component_total`
**Type**: Counter
**Description**: Total number of errors by component
**Labels**:
- `component`: Component identifier

**Usage**: Identify which components are generating errors

#### `mcp_gateway_errors_retryable_total`
**Type**: Counter
**Description**: Total number of retryable vs non-retryable errors
**Labels**:
- `retryable`: `true` or `false`

**Usage**: Understand error retry characteristics

#### `mcp_gateway_errors_by_http_status_total`
**Type**: Counter
**Description**: Total number of errors by HTTP status code
**Labels**:
- `status_code`: HTTP status code (e.g., `500`, `503`)
- `status_class`: HTTP status class (`4xx`, `5xx`)

**Usage**: Track HTTP error distribution

#### `mcp_gateway_errors_by_severity_total`
**Type**: Counter
**Description**: Total number of errors by severity level
**Labels**:
- `severity`: Severity level (`low`, `medium`, `high`, `critical`)

**Usage**: Prioritize error investigation by severity

#### `mcp_gateway_errors_by_operation_total`
**Type**: Counter
**Description**: Total number of errors by operation
**Labels**:
- `operation`: Operation name
- `component`: Component identifier

**Usage**: Track which operations are failing

#### `mcp_gateway_error_recovery_total`
**Type**: Counter
**Description**: Total number of error recovery attempts
**Labels**:
- `recovered`: `true` or `false`
- `error_type`: Type of error

**Usage**: Monitor error recovery effectiveness

#### `mcp_gateway_error_handling_duration_seconds`
**Type**: Histogram
**Description**: Time spent handling errors
**Labels**:
- `error_type`: Error type
- `component`: Component identifier

**Buckets**: Default Prometheus buckets

**Usage**: Monitor error handling overhead

## Router Metrics

Router metrics are exposed at `:9091/metrics`.

### Connection Metrics

#### `mcp_router_gateway_connections_total`
**Type**: Counter
**Description**: Total gateway connections established
**Labels**: None
**Usage**: Track gateway connection count over time

#### `mcp_router_gateway_connections_active`
**Type**: Gauge
**Description**: Currently active gateway connections
**Labels**: None
**Usage**: Monitor active gateway connections

#### `mcp_router_direct_connections_total`
**Type**: Counter
**Description**: Total direct MCP server connections
**Labels**:
- `protocol`: Connection protocol (`stdio`, `websocket`, `http`, `sse`)
- `server`: Server identifier

**Usage**: Track direct connection count by protocol

#### `mcp_router_direct_connections_active`
**Type**: Gauge
**Description**: Currently active direct connections
**Labels**:
- `protocol`: Connection protocol
- `server`: Server identifier

**Usage**: Monitor active direct connections

### Request Metrics

#### `mcp_router_requests_total`
**Type**: Counter
**Description**: Total requests processed
**Labels**:
- `method`: MCP method
- `target`: Target (`gateway` or server name)
- `status`: Request status

**Usage**: Track request rate and success

#### `mcp_router_request_duration_seconds`
**Type**: Histogram
**Description**: Request duration in seconds
**Labels**:
- `method`: MCP method
- `target`: Target destination

**Buckets**: Default Prometheus buckets

**Usage**: Monitor request latency

### Protocol Detection Metrics

#### `mcp_router_protocol_detection_total`
**Type**: Counter
**Description**: Protocol detection attempts
**Labels**:
- `detected_protocol`: Detected protocol
- `success`: `true` or `false`

**Usage**: Monitor protocol auto-detection accuracy

### Connection Pool Metrics

#### `mcp_router_pool_size`
**Type**: Gauge
**Description**: Connection pool size
**Labels**:
- `pool`: Pool identifier

**Usage**: Monitor pool utilization

#### `mcp_router_pool_waiters`
**Type**: Gauge
**Description**: Goroutines waiting for connection
**Labels**:
- `pool`: Pool identifier

**Usage**: Detect pool contention

## Example Queries

### Gateway Health

```promql
# Overall request success rate
sum(rate(mcp_gateway_requests_total{status="success"}[5m]))
/ sum(rate(mcp_gateway_requests_total[5m])) * 100

# Error budget (99.9% SLA)
1 - (sum(rate(mcp_gateway_requests_total{status="error"}[30d]))
/ sum(rate(mcp_gateway_requests_total[30d])))

# Active connections utilization
mcp_gateway_connections_active / on() group_left()
max_over_time(mcp_gateway_connections_active[1h]) * 100
```

### Performance Monitoring

```promql
# p95 latency by method
histogram_quantile(0.95,
  sum by (method, le) (rate(mcp_gateway_request_duration_seconds_bucket[5m]))
)

# Slow requests (> 1s)
sum(rate(mcp_gateway_request_duration_seconds_bucket{le="1"}[5m]))
- sum(rate(mcp_gateway_request_duration_seconds_bucket{le="0.5"}[5m]))

# Requests per second
sum(rate(mcp_gateway_requests_total[1m]))
```

### Error Analysis

```promql
# Top 5 error types
topk(5, sum by (type) (rate(mcp_gateway_errors_by_type_total[5m])))

# Critical errors
rate(mcp_gateway_errors_by_severity_total{severity="critical"}[5m])

# Error rate by component
sum by (component) (rate(mcp_gateway_errors_by_component_total[5m]))
```

### Circuit Breaker Monitoring

```promql
# Endpoints with circuit breaker issues
count(mcp_gateway_circuit_breaker_state > 0)

# Time with open circuit breakers
sum(mcp_gateway_circuit_breaker_state == 1)
```

## Alerting Rules

### Recommended Alerts

```yaml
groups:
  - name: mcp_gateway
    interval: 30s
    rules:
      # High error rate
      - alert: HighErrorRate
        expr: |
          (sum(rate(mcp_gateway_requests_total{status="error"}[5m]))
          / sum(rate(mcp_gateway_requests_total[5m]))) > 0.05
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: High error rate on Gateway
          description: Error rate is {{ $value | humanizePercentage }}

      # High latency
      - alert: HighLatency
        expr: |
          histogram_quantile(0.95,
            sum by (le) (rate(mcp_gateway_request_duration_seconds_bucket[5m]))
          ) > 1
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: High p95 latency on Gateway
          description: p95 latency is {{ $value }}s

      # Circuit breaker open
      - alert: CircuitBreakerOpen
        expr: mcp_gateway_circuit_breaker_state == 1
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: Circuit breaker open for {{ $labels.endpoint }}

      # High connection rejection rate
      - alert: HighConnectionRejectionRate
        expr: rate(mcp_gateway_connections_rejected_total[5m]) > 10
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: High connection rejection rate
          description: Rejecting {{ $value }} connections/sec

      # Authentication failures
      - alert: HighAuthFailureRate
        expr: |
          sum(rate(mcp_gateway_auth_failures_total[5m])) > 5
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: High authentication failure rate
          description: {{ $value }} auth failures/sec

  - name: mcp_router
    interval: 30s
    rules:
      # Gateway connection down
      - alert: GatewayConnectionDown
        expr: mcp_router_gateway_connections_active == 0
        for: 1m
        labels:
          severity: critical
        annotations:
          summary: Router has no active gateway connections

      # High request error rate
      - alert: RouterHighErrorRate
        expr: |
          (sum(rate(mcp_router_requests_total{status="error"}[5m]))
          / sum(rate(mcp_router_requests_total[5m]))) > 0.05
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: High router error rate
```

## Grafana Dashboards

Pre-built Grafana dashboards are available in `/deployments/kubernetes/monitoring/grafana/dashboards/`:

- **Gateway Overview**: Connection count, request rate, latency, error rate
- **Gateway Performance**: Detailed latency percentiles, throughput analysis
- **Gateway Errors**: Error breakdown by type, component, severity
- **Router Overview**: Gateway connections, direct connections, request metrics
- **Circuit Breakers**: Circuit breaker state monitoring across all endpoints

Import these dashboards using the Grafana UI or the provisioning system.

## Related Documentation

- [Monitoring Guide](../monitoring.md) - Complete monitoring setup
- [Troubleshooting Guide](../troubleshooting.md) - Using metrics for troubleshooting
- [Production Readiness](../../docs/PRODUCTION_READINESS.md) - Production checklist
- [SLI/SLO Monitoring](../SLI_SLO_MONITORING.md) - Service level objectives

---

**Last Updated**: 2025-10-01
