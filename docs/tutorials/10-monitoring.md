# Tutorial: Monitoring & Observability

Set up comprehensive monitoring for MCP Bridge with Prometheus, Grafana, distributed tracing, and alerting.

## Prerequisites

- MCP Bridge Gateway deployed
- Basic understanding of Prometheus and Grafana
- Kubernetes cluster (for some examples)
- 35-40 minutes

## What You'll Build

Complete observability stack:
- **Prometheus** - Metrics collection
- **Grafana** - Visualization dashboards
- **Jaeger** - Distributed tracing
- **Loki** - Log aggregation
- **Alertmanager** - Alert routing
- **Custom dashboards** - Gateway & Router monitoring

## Architecture

```
┌─────────────────────────────────────────────────┐
│  MCP Bridge Components                           │
│  ┌──────────┐         ┌──────────┐             │
│  │ Gateway  │────────►│  Router  │             │
│  │ :9090    │         │  :9091   │             │
│  └────┬─────┘         └────┬─────┘             │
│       │ metrics            │ metrics            │
│       │ traces             │ traces             │
│       │ logs               │ logs               │
└───────┼────────────────────┼─────────────────────┘
        │                    │
        ▼                    ▼
┌───────────────────────────────────────────────────┐
│  Observability Stack                              │
│  ┌──────────────┐  ┌──────────────┐             │
│  │  Prometheus  │  │    Jaeger    │             │
│  │    :9090     │  │    :16686    │             │
│  └──────┬───────┘  └──────────────┘             │
│         │                                         │
│  ┌──────▼───────┐  ┌──────────────┐             │
│  │   Grafana    │  │     Loki     │             │
│  │    :3000     │  │    :3100     │             │
│  └──────────────┘  └──────────────┘             │
└───────────────────────────────────────────────────┘
```

## Step 1: Deploy Prometheus

### Install Prometheus Operator

```bash
# Add Prometheus Operator
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo update

# Install
helm install prometheus prometheus-community/kube-prometheus-stack \
  --namespace monitoring \
  --create-namespace \
  --values prometheus-values.yaml
```

### Prometheus Values

Create `prometheus-values.yaml`:

```yaml
prometheus:
  prometheusSpec:
    retention: 15d
    retentionSize: "50GB"

    storageSpec:
      volumeClaimTemplate:
        spec:
          accessModes: ["ReadWriteOnce"]
          resources:
            requests:
              storage: 100Gi

    resources:
      requests:
        memory: "2Gi"
        cpu: "1000m"
      limits:
        memory: "4Gi"
        cpu: "2000m"

    # Scrape MCP Bridge
    additionalScrapeConfigs:
    - job_name: 'mcp-gateway'
      kubernetes_sd_configs:
      - role: pod
        namespaces:
          names:
          - mcp-system
      relabel_configs:
      - source_labels: [__meta_kubernetes_pod_label_app]
        regex: mcp-gateway
        action: keep
      - source_labels: [__meta_kubernetes_pod_ip]
        target_label: __address__
        replacement: '$1:9090'

    - job_name: 'mcp-router'
      kubernetes_sd_configs:
      - role: pod
        namespaces:
          names:
          - mcp-system
      relabel_configs:
      - source_labels: [__meta_kubernetes_pod_label_app]
        regex: mcp-router
        action: keep
      - source_labels: [__meta_kubernetes_pod_ip]
        target_label: __address__
        replacement: '$1:9091'

grafana:
  enabled: true
  adminPassword: "admin"

  persistence:
    enabled: true
    size: 10Gi

  dashboardProviders:
    dashboardproviders.yaml:
      apiVersion: 1
      providers:
      - name: 'mcp-bridge'
        orgId: 1
        folder: 'MCP Bridge'
        type: file
        disableDeletion: false
        editable: true
        options:
          path: /var/lib/grafana/dashboards/mcp-bridge

alertmanager:
  enabled: true
  config:
    global:
      resolve_timeout: 5m
    route:
      group_by: ['alertname', 'cluster', 'service']
      group_wait: 10s
      group_interval: 10s
      repeat_interval: 12h
      receiver: 'default'
      routes:
      - match:
          severity: critical
        receiver: 'pagerduty'
      - match:
          severity: warning
        receiver: 'slack'
    receivers:
    - name: 'default'
      email_configs:
      - to: 'ops@example.com'
    - name: 'pagerduty'
      pagerduty_configs:
      - service_key: 'YOUR_KEY'
    - name: 'slack'
      slack_configs:
      - api_url: 'YOUR_WEBHOOK'
        channel: '#alerts'
```

## Step 2: Create Grafana Dashboards

### Gateway Dashboard

Create `gateway-dashboard.json`:

```json
{
  "dashboard": {
    "title": "MCP Gateway Metrics",
    "panels": [
      {
        "title": "Request Rate",
        "targets": [{
          "expr": "rate(mcp_gateway_requests_total[5m])"
        }],
        "type": "graph"
      },
      {
        "title": "Error Rate",
        "targets": [{
          "expr": "rate(mcp_gateway_errors_total[5m]) / rate(mcp_gateway_requests_total[5m])"
        }],
        "type": "graph"
      },
      {
        "title": "Latency (P50, P95, P99)",
        "targets": [
          {
            "expr": "histogram_quantile(0.50, rate(mcp_gateway_request_duration_seconds_bucket[5m]))",
            "legendFormat": "P50"
          },
          {
            "expr": "histogram_quantile(0.95, rate(mcp_gateway_request_duration_seconds_bucket[5m]))",
            "legendFormat": "P95"
          },
          {
            "expr": "histogram_quantile(0.99, rate(mcp_gateway_request_duration_seconds_bucket[5m]))",
            "legendFormat": "P99"
          }
        ],
        "type": "graph"
      },
      {
        "title": "Active Connections",
        "targets": [{
          "expr": "mcp_gateway_active_connections"
        }],
        "type": "graph"
      },
      {
        "title": "Backend Health",
        "targets": [{
          "expr": "mcp_gateway_backend_healthy"
        }],
        "type": "stat"
      },
      {
        "title": "Circuit Breaker State",
        "targets": [{
          "expr": "mcp_gateway_circuit_breaker_state"
        }],
        "type": "stat"
      }
    ]
  }
}
```

Import dashboard:

```bash
# Create ConfigMap with dashboard
kubectl create configmap mcp-gateway-dashboard \
  --from-file=gateway-dashboard.json \
  -n monitoring

# Label for auto-import
kubectl label configmap mcp-gateway-dashboard \
  grafana_dashboard=1 \
  -n monitoring
```

## Step 3: Configure Distributed Tracing

### Deploy Jaeger

```bash
# Install Jaeger Operator
kubectl create namespace observability
kubectl apply -f https://github.com/jaegertracing/jaeger-operator/releases/download/v1.49.0/jaeger-operator.yaml -n observability

# Deploy Jaeger instance
cat <<EOF | kubectl apply -f -
apiVersion: jaegertracing.io/v1
kind: Jaeger
metadata:
  name: mcp-jaeger
  namespace: observability
spec:
  strategy: production
  storage:
    type: elasticsearch
    options:
      es:
        server-urls: http://elasticsearch:9200
  ingress:
    enabled: true
  query:
    resources:
      limits:
        cpu: 500m
        memory: 512Mi
  collector:
    resources:
      limits:
        cpu: 1000m
        memory: 1Gi
EOF
```

### Enable Tracing in Gateway

Update `gateway.yaml`:

```yaml
observability:
  tracing:
    enabled: true
    service_name: "mcp-gateway"

    # Jaeger exporter
    exporter: jaeger
    jaeger:
      endpoint: "http://mcp-jaeger-collector.observability:14268/api/traces"

    # Sampling
    sampler:
      type: probabilistic
      param: 0.1  # 10% sampling

    # Trace all requests or selective
    trace_requests: true
    trace_tool_calls: true
    trace_discovery: true
```

### View Traces

```bash
# Port-forward Jaeger UI
kubectl port-forward svc/mcp-jaeger-query -n observability 16686:16686

# Open http://localhost:16686
# Search for service: mcp-gateway
```

## Step 4: Log Aggregation with Loki

### Deploy Loki Stack

```bash
helm install loki grafana/loki-stack \
  --namespace monitoring \
  --set loki.persistence.enabled=true \
  --set loki.persistence.size=50Gi \
  --set promtail.enabled=true \
  --set grafana.enabled=false
```

### Configure Gateway Logging

```yaml
observability:
  logging:
    level: info
    format: json
    output: stdout

    # Structured logging
    additional_fields:
      service: "mcp-gateway"
      environment: "production"
      region: "${REGION}"

    # Log sampling
    sampling:
      enabled: true
      rate: 0.1  # Sample 10% of logs

    # Sensitive data
    redact_fields:
      - "password"
      - "token"
      - "secret"
```

### Query Logs in Grafana

```promql
# All gateway logs
{app="mcp-gateway"}

# Error logs only
{app="mcp-gateway"} |= "level=error"

# Logs with latency > 100ms
{app="mcp-gateway"} | json | duration > 0.1

# Aggregated error rate
sum(rate({app="mcp-gateway"} |= "level=error" [5m]))
```

## Step 5: Alerting Rules

### Create Alert Rules

Create `alerts.yaml`:

```yaml
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: mcp-gateway-alerts
  namespace: monitoring
spec:
  groups:
  - name: mcp-gateway
    interval: 30s
    rules:
    # High error rate
    - alert: HighErrorRate
      expr: |
        (
          rate(mcp_gateway_errors_total[5m])
          /
          rate(mcp_gateway_requests_total[5m])
        ) > 0.05
      for: 5m
      labels:
        severity: warning
      annotations:
        summary: "High error rate on {{ $labels.instance }}"
        description: "Error rate is {{ $value | humanizePercentage }}"

    # High latency
    - alert: HighLatency
      expr: |
        histogram_quantile(0.95,
          rate(mcp_gateway_request_duration_seconds_bucket[5m])
        ) > 0.5
      for: 5m
      labels:
        severity: warning
      annotations:
        summary: "High latency on {{ $labels.instance }}"
        description: "P95 latency is {{ $value }}s"

    # Gateway down
    - alert: GatewayDown
      expr: up{job="mcp-gateway"} == 0
      for: 1m
      labels:
        severity: critical
      annotations:
        summary: "Gateway {{ $labels.instance }} is down"

    # High connection count
    - alert: HighConnectionCount
      expr: mcp_gateway_active_connections > 5000
      for: 10m
      labels:
        severity: warning
      annotations:
        summary: "High connection count on {{ $labels.instance }}"
        description: "{{ $value }} active connections"

    # Circuit breaker open
    - alert: CircuitBreakerOpen
      expr: mcp_gateway_circuit_breaker_state == 1
      for: 5m
      labels:
        severity: critical
      annotations:
        summary: "Circuit breaker open for {{ $labels.backend }}"

    # Backend unhealthy
    - alert: BackendUnhealthy
      expr: mcp_gateway_backend_healthy == 0
      for: 2m
      labels:
        severity: warning
      annotations:
        summary: "Backend {{ $labels.backend }} is unhealthy"

    # Memory usage high
    - alert: HighMemoryUsage
      expr: |
        process_resident_memory_bytes{job="mcp-gateway"}
        / 1024 / 1024 / 1024 > 0.8
      for: 10m
      labels:
        severity: warning
      annotations:
        summary: "High memory usage on {{ $labels.instance }}"

    # Disk space low (Redis)
    - alert: LowDiskSpace
      expr: |
        (
          node_filesystem_avail_bytes{mountpoint="/data"}
          /
          node_filesystem_size_bytes{mountpoint="/data"}
        ) < 0.2
      for: 5m
      labels:
        severity: warning
      annotations:
        summary: "Low disk space on {{ $labels.instance }}"
```

Apply alerts:

```bash
kubectl apply -f alerts.yaml
```

## Step 6: Service Level Objectives (SLOs)

### Define SLOs

Create `slos.yaml`:

```yaml
# SLO: 99.9% availability
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: mcp-slos
  namespace: monitoring
spec:
  groups:
  - name: slos
    interval: 1m
    rules:
    # Availability SLO
    - record: slo:availability:ratio
      expr: |
        sum(rate(mcp_gateway_requests_total{code=~"2.."}[5m]))
        /
        sum(rate(mcp_gateway_requests_total[5m]))

    - alert: AvailabilitySLOBreach
      expr: slo:availability:ratio < 0.999
      for: 5m
      labels:
        severity: critical
      annotations:
        summary: "Availability SLO breached"
        description: "Current availability: {{ $value | humanizePercentage }}"

    # Latency SLO (P95 < 100ms)
    - record: slo:latency:p95
      expr: |
        histogram_quantile(0.95,
          rate(mcp_gateway_request_duration_seconds_bucket[5m])
        )

    - alert: LatencySLOBreach
      expr: slo:latency:p95 > 0.1
      for: 5m
      labels:
        severity: warning
      annotations:
        summary: "Latency SLO breached"
        description: "P95 latency: {{ $value }}s (target: 0.1s)"
```

## Step 7: Custom Exporters

### Redis Exporter

```bash
# Deploy Redis exporter
kubectl apply -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: redis-exporter
  namespace: mcp-system
spec:
  replicas: 1
  selector:
    matchLabels:
      app: redis-exporter
  template:
    metadata:
      labels:
        app: redis-exporter
    spec:
      containers:
      - name: exporter
        image: oliver006/redis_exporter:latest
        ports:
        - containerPort: 9121
        env:
        - name: REDIS_ADDR
          value: "redis:6379"
---
apiVersion: v1
kind: Service
metadata:
  name: redis-exporter
  namespace: mcp-system
  labels:
    app: redis-exporter
spec:
  ports:
  - port: 9121
    targetPort: 9121
  selector:
    app: redis-exporter
EOF
```

## Step 8: Monitoring Best Practices

### Key Metrics to Monitor

```yaml
# Golden Signals
metrics:
  # Latency
  - mcp_gateway_request_duration_seconds
  # Traffic
  - mcp_gateway_requests_total
  # Errors
  - mcp_gateway_errors_total
  # Saturation
  - mcp_gateway_active_connections
  - process_cpu_seconds_total
  - process_resident_memory_bytes

# RED Method (Rate, Errors, Duration)
red_metrics:
  rate: rate(mcp_gateway_requests_total[5m])
  errors: rate(mcp_gateway_errors_total[5m])
  duration: histogram_quantile(0.95, rate(mcp_gateway_request_duration_seconds_bucket[5m]))

# USE Method (Utilization, Saturation, Errors)
use_metrics:
  utilization: process_cpu_seconds_total / total_cpu
  saturation: mcp_gateway_active_connections / max_connections
  errors: rate(mcp_gateway_errors_total[5m])
```

### Retention Policies

```yaml
prometheus:
  retention: 15d  # Short-term storage

  # Remote write to long-term storage
  remoteWrite:
  - url: https://prometheus.example.com/write
    writeRelabelConfigs:
    - sourceLabels: [__name__]
      regex: 'mcp_gateway_.*'
      action: keep
```

## Troubleshooting

### Metrics Not Appearing

```bash
# Check scrape targets
curl http://prometheus:9090/api/v1/targets

# Test metrics endpoint
curl http://gateway:9090/metrics

# Check ServiceMonitor
kubectl get servicemonitor -n monitoring
```

### High Cardinality Issues

```yaml
# Reduce label cardinality
observability:
  metrics:
    # Drop high-cardinality labels
    relabel_configs:
    - source_labels: [user_id]
      action: drop

    # Aggregate before recording
    - source_labels: [status_code]
      regex: '(2|3|4|5)..'
      target_label: status_class
```

## Next Steps

- [Performance Tuning](12-performance-tuning.md) - Optimize based on metrics
- [High Availability Setup](06-ha-deployment.md) - Monitor HA deployment
- [Connection Troubleshooting](11-connection-troubleshooting.md) - Debug with metrics

## Summary

This tutorial covered:
- Deployed Prometheus, Grafana, Jaeger, and Loki
- Created custom dashboards for Gateway and Router
- Configured distributed tracing
- Set up log aggregation
- Defined alerting rules and SLOs
- Implemented monitoring best practices


