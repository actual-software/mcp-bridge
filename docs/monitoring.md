# Monitoring and Observability Guide

Comprehensive guide for monitoring MCP Router and Gateway components in production.

## Table of Contents

- [Metrics Overview](#metrics-overview)
- [Prometheus Setup](#prometheus-setup)
- [Grafana Dashboards](#grafana-dashboards)
- [Alerting Rules](#alerting-rules)
- [Distributed Tracing](#distributed-tracing)
- [Log Aggregation](#log-aggregation)
- [Health Checks](#health-checks)
- [Performance Monitoring](#performance-monitoring)
- [SLO/SLI Definitions](#slosli-definitions)

## Metrics Overview

### Router Metrics

| Metric | Type | Description | Labels |
|--------|------|-------------|--------|
| `mcp_router_requests_total` | Counter | Total requests sent | method, status |
| `mcp_router_request_duration_seconds` | Histogram | Request latency | method, status |
| `mcp_router_response_size_bytes` | Histogram | Response size distribution | method |
| `mcp_router_active_connections` | Gauge | Current WebSocket/TCP connections | protocol |
| `mcp_router_connection_errors_total` | Counter | Connection failures | error_type |
| `mcp_router_connection_retries_total` | Counter | Reconnection attempts | |
| `mcp_router_auth_failures_total` | Counter | Authentication failures | reason |
| `mcp_router_rate_limit_exceeded_total` | Counter | Rate limit violations | |
| `mcp_router_pool_connections` | Gauge | Connection pool size | state |
| `mcp_router_secure_storage_operations_total` | Counter | Keychain operations | operation, result |

### Gateway Metrics

| Metric | Type | Description | Labels |
|--------|------|-------------|--------|
| `mcp_gateway_connections_total` | Gauge | Active connections | protocol, state |
| `mcp_gateway_requests_total` | Counter | Total requests | method, status, namespace |
| `mcp_gateway_request_duration_seconds` | Histogram | Request latency | method, backend |
| `mcp_gateway_backend_health` | Gauge | Backend health status | namespace, backend |
| `mcp_gateway_circuit_breaker_state` | Gauge | Circuit breaker state | namespace, state |
| `mcp_gateway_rate_limit_remaining` | Gauge | Rate limit capacity | user |
| `mcp_gateway_session_duration_seconds` | Histogram | Session length | |
| `mcp_gateway_auth_attempts_total` | Counter | Auth attempts | type, result |
| `mcp_gateway_tcp_frames_total` | Counter | TCP protocol frames | type, direction |
| `mcp_gateway_redis_operations_duration_seconds` | Histogram | Redis operation latency | operation |

## Prometheus Setup

### Configuration

```yaml
# prometheus.yml
global:
  scrape_interval: 15s
  evaluation_interval: 15s

scrape_configs:
  # Router metrics
  - job_name: 'mcp-router'
    static_configs:
      - targets: ['localhost:9091']
    relabel_configs:
      - source_labels: [__address__]
        target_label: instance
        replacement: 'router-local'

  # Gateway metrics
  - job_name: 'mcp-gateway'
    kubernetes_sd_configs:
      - role: pod
        namespaces:
          names:
            - mcp-system
    relabel_configs:
      - source_labels: [__meta_kubernetes_pod_label_app]
        action: keep
        regex: mcp-gateway
      - source_labels: [__meta_kubernetes_pod_name]
        target_label: instance
      - target_label: __address__
        replacement: '${1}:9090'
        source_labels: [__meta_kubernetes_pod_ip]

  # Redis exporter
  - job_name: 'redis'
    kubernetes_sd_configs:
      - role: pod
        namespaces:
          names:
            - mcp-system
    relabel_configs:
      - source_labels: [__meta_kubernetes_pod_label_app]
        action: keep
        regex: redis-exporter
```

### Federation Setup

For multi-region deployments:

```yaml
# Federation configuration
- job_name: 'federate'
  scrape_interval: 15s
  honor_labels: true
  metrics_path: '/federate'
  params:
    'match[]':
      - '{job=~"mcp-.*"}'
      - '{__name__=~"mcp_.*"}'
  static_configs:
    - targets:
      - 'prometheus-us-east.example.com:9090'
      - 'prometheus-eu-west.example.com:9090'
```

## Grafana Dashboards

### Router Dashboard

```json
{
  "dashboard": {
    "title": "MCP Router Overview",
    "panels": [
      {
        "title": "Request Rate",
        "targets": [{
          "expr": "rate(mcp_router_requests_total[5m])"
        }]
      },
      {
        "title": "Request Latency",
        "targets": [{
          "expr": "histogram_quantile(0.95, rate(mcp_router_request_duration_seconds_bucket[5m]))"
        }]
      },
      {
        "title": "Active Connections",
        "targets": [{
          "expr": "mcp_router_active_connections"
        }]
      },
      {
        "title": "Error Rate",
        "targets": [{
          "expr": "rate(mcp_router_requests_total{status=\"error\"}[5m])"
        }]
      }
    ]
  }
}
```

### Gateway Dashboard

Import dashboard ID: **15759** (MCP Gateway Dashboard)

Key panels:
- Connection distribution by protocol
- Request rate by namespace
- Backend health matrix
- Circuit breaker states
- Rate limit utilization
- Session distribution

### Custom Panels

#### Connection Pool Efficiency
```promql
# Pool utilization
mcp_router_pool_connections{state="active"} / 
mcp_router_pool_connections{state="total"} * 100
```

#### Authentication Success Rate
```promql
# Auth success rate
rate(mcp_gateway_auth_attempts_total{result="success"}[5m]) /
rate(mcp_gateway_auth_attempts_total[5m]) * 100
```

#### Binary Protocol Performance
```promql
# TCP vs WebSocket latency comparison
histogram_quantile(0.95, 
  rate(mcp_gateway_request_duration_seconds_bucket{protocol="tcp"}[5m])
) / 
histogram_quantile(0.95,
  rate(mcp_gateway_request_duration_seconds_bucket{protocol="websocket"}[5m])
)
```

## Alerting Rules

### Critical Alerts

```yaml
groups:
  - name: mcp_critical
    interval: 30s
    rules:
      # Gateway down
      - alert: MCPGatewayDown
        expr: up{job="mcp-gateway"} == 0
        for: 2m
        labels:
          severity: critical
          service: mcp
        annotations:
          summary: "MCP Gateway is down"
          description: "Gateway {{ $labels.instance }} has been down for more than 2 minutes"
      
      # High error rate
      - alert: MCPHighErrorRate
        expr: |
          rate(mcp_gateway_requests_total{status="error"}[5m]) > 0.05
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "High error rate detected"
          description: "Error rate is {{ $value | humanizePercentage }} over the last 5 minutes"
      
      # Redis unavailable
      - alert: MCPRedisDown
        expr: |
          redis_up{job="redis"} == 0
        for: 2m
        labels:
          severity: critical
        annotations:
          summary: "Redis is unavailable"
          description: "Redis instance {{ $labels.instance }} is down"
```

### Warning Alerts

```yaml
      # High memory usage
      - alert: MCPHighMemoryUsage
        expr: |
          process_resident_memory_bytes{job=~"mcp-.*"} / 1024 / 1024 / 1024 > 2
        for: 10m
        labels:
          severity: warning
        annotations:
          summary: "High memory usage"
          description: "{{ $labels.job }} is using {{ $value | humanize }}GB of memory"
      
      # Connection pool exhaustion
      - alert: MCPConnectionPoolExhausted
        expr: |
          mcp_router_pool_connections{state="idle"} == 0
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Connection pool exhausted"
          description: "No idle connections available in pool"
      
      # Certificate expiry
      - alert: MCPCertificateExpirySoon
        expr: |
          (x509_cert_expiry - time()) / 86400 < 30
        for: 1h
        labels:
          severity: warning
        annotations:
          summary: "Certificate expiring soon"
          description: "Certificate {{ $labels.subject }} expires in {{ $value }} days"
```

## Distributed Tracing

### Jaeger Setup

```yaml
# Gateway configuration
tracing:
  enabled: true
  service_name: mcp-gateway
  sampler_type: adaptive
  sampler_param: 1.0  # Start with 100% sampling
  agent_host: jaeger-agent.monitoring
  agent_port: 6831
```

### Key Traces

1. **Request Flow**
   - `router.sendRequest` → `gateway.handleRequest` → `backend.process`

2. **Authentication**
   - `auth.validate` → `session.create` → `redis.store`

3. **Circuit Breaker**
   - `circuitBreaker.allow` → `backend.call` → `circuitBreaker.record`

### Custom Spans

```go
// Add custom spans in code
span := opentracing.StartSpan("custom.operation")
defer span.Finish()

span.SetTag("namespace", namespace)
span.SetTag("user", session.User)
```

## Log Aggregation

### Structured Logging

#### Router Log Format
```json
{
  "timestamp": "2024-01-15T10:30:45.123Z",
  "level": "info",
  "service": "mcp-router",
  "connection_id": "conn-123",
  "method": "tools/call",
  "duration_ms": 45,
  "status": "success",
  "message": "Request completed"
}
```

#### Gateway Log Format
```json
{
  "timestamp": "2024-01-15T10:30:45.234Z",
  "level": "info",
  "service": "mcp-gateway",
  "session_id": "sess-456",
  "request_id": "req-789",
  "namespace": "default",
  "backend": "server-1",
  "message": "Request routed"
}
```

### Elasticsearch Queries

#### Find slow requests
```json
{
  "query": {
    "bool": {
      "must": [
        {"term": {"service": "mcp-gateway"}},
        {"range": {"duration_ms": {"gte": 1000}}}
      ]
    }
  }
}
```

#### Authentication failures
```json
{
  "query": {
    "bool": {
      "must": [
        {"term": {"level": "error"}},
        {"match": {"message": "authentication failed"}}
      ]
    }
  },
  "aggs": {
    "by_user": {
      "terms": {"field": "user.keyword"}
    }
  }
}
```

## Health Checks

### Router Health Endpoint

```bash
# Local health check
curl http://localhost:9091/health

# Response
{
  "status": "healthy",
  "gateway_connected": true,
  "secure_storage_available": true,
  "uptime_seconds": 3600,
  "version": "1.2.0"
}
```

### Gateway Health Endpoint

```bash
# Kubernetes health check
curl http://gateway:8080/health

# Response
{
  "status": "healthy",
  "backends": {
    "default": "healthy",
    "namespace-1": "healthy"
  },
  "redis": "connected",
  "sessions_active": 150
}
```

### TCP Health Check

```bash
# Binary protocol health
echo -ne "\x4D\x43\x50\x42\x01\x04\x00\x00\x00\x00\x00\x00" | \
  nc gateway 8445 | hexdump -C
```

## Performance Monitoring

### Key Performance Indicators

#### Latency Percentiles
```promql
# P50, P95, P99 latencies
histogram_quantile(0.50, rate(mcp_gateway_request_duration_seconds_bucket[5m]))
histogram_quantile(0.95, rate(mcp_gateway_request_duration_seconds_bucket[5m]))
histogram_quantile(0.99, rate(mcp_gateway_request_duration_seconds_bucket[5m]))
```

#### Throughput
```promql
# Requests per second
sum(rate(mcp_gateway_requests_total[1m]))

# Bytes per second
sum(rate(mcp_router_response_size_bytes_sum[1m]))
```

#### Error Budget
```promql
# Error budget consumption
(1 - (sum(rate(mcp_gateway_requests_total{status="success"}[5m])) /
      sum(rate(mcp_gateway_requests_total[5m])))) * 100
```

### Performance Dashboards

#### Latency Heatmap
```promql
# Latency distribution over time
sum(rate(mcp_gateway_request_duration_seconds_bucket[5m])) by (le)
```

#### Backend Performance Matrix
```promql
# Backend latency comparison
avg by (backend) (
  histogram_quantile(0.95,
    rate(mcp_gateway_request_duration_seconds_bucket[5m])
  )
)
```

## SLO/SLI Definitions

### Service Level Indicators (SLIs)

1. **Availability**
   ```promql
   sum(up{job=~"mcp-.*"}) / count(up{job=~"mcp-.*"}) * 100
   ```

2. **Latency**
   ```promql
   histogram_quantile(0.95, rate(mcp_gateway_request_duration_seconds_bucket[5m])) < 0.5
   ```

3. **Error Rate**
   ```promql
   sum(rate(mcp_gateway_requests_total{status="success"}[5m])) /
   sum(rate(mcp_gateway_requests_total[5m])) * 100
   ```

### Service Level Objectives (SLOs)

```yaml
slos:
  - name: "MCP Gateway Availability"
    sli: "availability"
    objective: 99.9
    window: "30d"
    
  - name: "MCP Request Success Rate"
    sli: "success_rate"
    objective: 99.5
    window: "30d"
    
  - name: "MCP P95 Latency"
    sli: "latency_p95"
    objective: 500  # milliseconds
    window: "30d"
```

### Error Budget Monitoring

```promql
# Monthly error budget remaining
(1 - (
  sum(increase(mcp_gateway_requests_total{status!="success"}[30d])) /
  sum(increase(mcp_gateway_requests_total[30d]))
)) * 100 - 99.9
```

## Custom Monitoring Solutions

### Business Metrics

```promql
# Methods usage
topk(10, sum by (method) (rate(mcp_gateway_requests_total[1h])))

# User activity
topk(20, sum by (user) (rate(mcp_gateway_requests_total[1h])))

# Namespace utilization
sum by (namespace) (rate(mcp_gateway_requests_total[5m]))
```

### Cost Monitoring

```promql
# Estimated monthly requests
sum(increase(mcp_gateway_requests_total[30d]))

# Data transfer estimate (GB)
sum(increase(mcp_router_response_size_bytes_sum[30d])) / 1024 / 1024 / 1024
```

### Capacity Planning

```promql
# Connection growth rate
predict_linear(mcp_gateway_connections_total[7d], 30 * 86400)

# Memory usage trend
predict_linear(process_resident_memory_bytes{job="mcp-gateway"}[7d], 30 * 86400)
```