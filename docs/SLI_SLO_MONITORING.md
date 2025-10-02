# Service Level Indicators (SLI) and Service Level Objectives (SLO) for MCP Bridge

This document defines the Service Level Indicators (SLIs) and Service Level Objectives (SLOs) for the MCP Bridge system, providing a framework for measuring and maintaining service reliability.

## Overview

SLIs are carefully selected metrics that measure the level of service provided to users. SLOs are the target values or ranges for these metrics that define acceptable service quality.

## Core SLIs and SLOs

### 1. Availability

**SLI Definition**: The percentage of time the service successfully responds to health check requests.

```yaml
sli:
  name: service_availability
  query: |
    1 - (
      sum(rate(health_check_failures_total[5m])) /
      sum(rate(health_check_attempts_total[5m]))
    )
```

**SLOs**:
- **Gateway**: 99.9% availability (43.2 minutes downtime/month)
- **Router**: 99.5% availability (3.6 hours downtime/month)

### 2. Request Success Rate

**SLI Definition**: The percentage of non-4xx requests that complete successfully.

```yaml
sli:
  name: request_success_rate
  query: |
    sum(rate(http_requests_total{status!~"4.."}[5m])) -
    sum(rate(http_requests_total{status=~"5.."}[5m])) /
    sum(rate(http_requests_total{status!~"4.."}[5m]))
```

**SLOs**:
- **Gateway API**: 99.9% success rate
- **MCP Protocol**: 99.5% success rate
- **WebSocket Connections**: 99.0% success rate

### 3. Latency

**SLI Definition**: The percentage of requests that complete within acceptable time limits.

```yaml
sli:
  name: latency_p95
  query: |
    histogram_quantile(0.95, 
      sum(rate(http_request_duration_seconds_bucket[5m])) 
      by (le, service)
    )
```

**SLOs**:
- **P50 Latency**: < 50ms
- **P95 Latency**: < 200ms
- **P99 Latency**: < 500ms

### 4. Authentication Success Rate

**SLI Definition**: The percentage of authentication attempts that succeed (excluding invalid credentials).

```yaml
sli:
  name: auth_success_rate
  query: |
    sum(rate(auth_success_total[5m])) /
    (sum(rate(auth_success_total[5m])) + 
     sum(rate(auth_failures_total{reason!="invalid_credentials"}[5m])))
```

**SLO**: 99.95% success rate for valid authentication attempts

### 5. Connection Stability

**SLI Definition**: The percentage of established connections that remain stable for their intended duration.

```yaml
sli:
  name: connection_stability
  query: |
    1 - (
      sum(rate(connection_dropped_total{reason!="client_disconnect"}[5m])) /
      sum(rate(connection_established_total[5m]))
    )
```

**SLO**: 99.5% connection stability

## Secondary SLIs

### 6. Circuit Breaker Health

**SLI Definition**: The percentage of time circuit breakers remain closed (healthy).

```yaml
sli:
  name: circuit_breaker_health
  query: |
    1 - avg(circuit_breaker_state{state="open"})
```

**SLO**: 95% of circuit breakers closed

### 7. Resource Utilization

**SLI Definition**: Services operate within resource limits.

```yaml
sli:
  name: resource_utilization
  queries:
    cpu: |
      avg(rate(container_cpu_usage_seconds_total[5m])) /
      avg(container_spec_cpu_quota/container_spec_cpu_period)
    memory: |
      avg(container_memory_usage_bytes) /
      avg(container_spec_memory_limit_bytes)
```

**SLOs**:
- CPU utilization < 80%
- Memory utilization < 85%

### 8. Error Budget Consumption

**SLI Definition**: The rate at which the error budget is being consumed.

```yaml
sli:
  name: error_budget_consumption_rate
  query: |
    (1 - slo_target) - (1 - current_sli_value) /
    (1 - slo_target)
```

**SLO**: Error budget consumption rate < 1.0 (not exceeding budget)

## Implementation

### Prometheus Recording Rules

```yaml
# /deployment/monitoring/prometheus-rules.yaml
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: mcp-bridge-slo-rules
spec:
  groups:
    - name: sli_calculations
      interval: 30s
      rules:
        # Availability SLI
        - record: sli:service_availability:ratio_rate5m
          expr: |
            1 - (
              sum by (service) (rate(health_check_failures_total[5m])) /
              sum by (service) (rate(health_check_attempts_total[5m]))
            )
          
        # Request Success Rate SLI
        - record: sli:request_success_rate:ratio_rate5m
          expr: |
            (
              sum by (service) (rate(http_requests_total{status!~"4.."}[5m])) -
              sum by (service) (rate(http_requests_total{status=~"5.."}[5m]))
            ) /
            sum by (service) (rate(http_requests_total{status!~"4.."}[5m]))
            
        # Latency SLI
        - record: sli:latency_p95:seconds
          expr: |
            histogram_quantile(0.95,
              sum by (service, le) (rate(http_request_duration_seconds_bucket[5m]))
            )
            
        # Error Budget Calculation
        - record: slo:error_budget:ratio
          expr: |
            1 - (
              (1 - sli:service_availability:ratio_rate5m) /
              (1 - 0.999)
            )
          labels:
            slo: "availability"
            target: "99.9%"
```

### Grafana Dashboard Configuration

```json
{
  "dashboard": {
    "title": "MCP Bridge SLI/SLO Dashboard",
    "panels": [
      {
        "title": "Service Availability SLO",
        "targets": [
          {
            "expr": "sli:service_availability:ratio_rate5m",
            "legendFormat": "Current: {{service}}"
          },
          {
            "expr": "0.999",
            "legendFormat": "SLO Target (99.9%)"
          }
        ],
        "alert": {
          "conditions": [
            {
              "evaluator": {
                "params": [0.999],
                "type": "lt"
              },
              "query": {
                "params": ["A", "5m", "now"]
              },
              "reducer": {
                "params": [],
                "type": "avg"
              },
              "type": "query"
            }
          ],
          "executionErrorState": "alerting",
          "frequency": "1m",
          "handler": 1,
          "name": "Service Availability SLO Violation",
          "noDataState": "alerting",
          "notifications": []
        }
      },
      {
        "title": "Error Budget Burn Rate",
        "targets": [
          {
            "expr": "rate(slo:error_budget:ratio[1h])",
            "legendFormat": "Hourly Burn Rate"
          },
          {
            "expr": "rate(slo:error_budget:ratio[24h])",
            "legendFormat": "Daily Burn Rate"
          }
        ]
      },
      {
        "title": "Request Latency Distribution",
        "targets": [
          {
            "expr": "histogram_quantile(0.5, sum(rate(http_request_duration_seconds_bucket[5m])) by (le))",
            "legendFormat": "P50"
          },
          {
            "expr": "histogram_quantile(0.95, sum(rate(http_request_duration_seconds_bucket[5m])) by (le))",
            "legendFormat": "P95"
          },
          {
            "expr": "histogram_quantile(0.99, sum(rate(http_request_duration_seconds_bucket[5m])) by (le))",
            "legendFormat": "P99"
          }
        ]
      }
    ]
  }
}
```

### Alerting Rules

```yaml
# /deployment/monitoring/alerting-rules.yaml
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: mcp-bridge-slo-alerts
spec:
  groups:
    - name: slo_alerts
      interval: 30s
      rules:
        # Fast Burn Alert (2% budget in 1 hour)
        - alert: SLOErrorBudgetFastBurn
          expr: |
            (
              1 - sli:service_availability:ratio_rate5m < 0.999
            ) * 3600 > 0.02
          for: 5m
          labels:
            severity: critical
            team: platform
          annotations:
            summary: "Error budget fast burn for {{ $labels.service }}"
            description: "{{ $labels.service }} is consuming error budget at {{ $value | humanizePercentage }} per hour"
            
        # Slow Burn Alert (10% budget in 24 hours)
        - alert: SLOErrorBudgetSlowBurn
          expr: |
            (
              1 - avg_over_time(sli:service_availability:ratio_rate5m[24h]) < 0.999
            ) * 24 > 0.10
          for: 1h
          labels:
            severity: warning
            team: platform
          annotations:
            summary: "Error budget slow burn for {{ $labels.service }}"
            description: "{{ $labels.service }} has consumed {{ $value | humanizePercentage }} of error budget in 24h"
            
        # Latency SLO Violation
        - alert: LatencySLOViolation
          expr: |
            sli:latency_p95:seconds > 0.2
          for: 5m
          labels:
            severity: warning
            team: platform
          annotations:
            summary: "P95 latency exceeds SLO"
            description: "{{ $labels.service }} P95 latency is {{ $value }}s (SLO: 200ms)"
```

## SLO Reviews and Adjustments

### Monthly SLO Review Process

1. **Data Collection**
   - Export SLI metrics for the past month
   - Calculate actual vs. target performance
   - Identify patterns and trends

2. **Analysis**
   - Review error budget consumption
   - Analyze SLO violations and their causes
   - Assess if SLOs are too aggressive or lenient

3. **Adjustments**
   - Propose SLO changes based on data
   - Consider user feedback and business requirements
   - Update monitoring and alerting accordingly

### SLO Review Dashboard Queries

```sql
-- Monthly SLO Performance Summary
SELECT 
  service,
  slo_name,
  AVG(sli_value) as avg_sli,
  MIN(sli_value) as min_sli,
  PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY sli_value) as p95_sli,
  slo_target,
  COUNT(CASE WHEN sli_value < slo_target THEN 1 END) as violations,
  COUNT(*) as total_measurements,
  (COUNT(CASE WHEN sli_value >= slo_target THEN 1 END)::FLOAT / COUNT(*)) as success_rate
FROM slo_measurements
WHERE timestamp >= NOW() - INTERVAL '30 days'
GROUP BY service, slo_name, slo_target;

-- Error Budget Consumption Trend
SELECT 
  date_trunc('day', timestamp) as day,
  service,
  slo_name,
  1 - AVG(sli_value) as error_rate,
  (1 - slo_target) as error_budget,
  ((1 - AVG(sli_value)) / (1 - slo_target)) as budget_consumption_rate
FROM slo_measurements
WHERE timestamp >= NOW() - INTERVAL '30 days'
GROUP BY day, service, slo_name, slo_target
ORDER BY day, service;
```

## Integration with Distributed Tracing

### Trace-based SLIs

Leverage OpenTelemetry traces to calculate more accurate SLIs:

```yaml
# Trace-based latency SLI
sli:
  name: trace_based_latency_p95
  query: |
    histogram_quantile(0.95,
      sum by (le, service) (
        rate(span_duration_milliseconds_bucket{
          span_kind="server",
          status_code!="error"
        }[5m])
      )
    )

# Trace-based error rate
sli:
  name: trace_based_error_rate
  query: |
    sum by (service) (rate(span_duration_milliseconds_count{status_code="error"}[5m])) /
    sum by (service) (rate(span_duration_milliseconds_count[5m]))
```

### Correlating Metrics and Traces

```go
// Example: Adding trace context to SLO violations
func recordSLOViolation(ctx context.Context, sloName string, actual, target float64) {
    span := trace.SpanFromContext(ctx)
    
    // Record violation as span event
    span.AddEvent("slo_violation", trace.WithAttributes(
        attribute.String("slo.name", sloName),
        attribute.Float64("slo.actual", actual),
        attribute.Float64("slo.target", target),
        attribute.Float64("slo.violation_margin", target - actual),
    ))
    
    // Emit metric
    sloViolationCounter.WithLabelValues(sloName).Inc()
    
    // Create exemplar linking metric to trace
    sloViolationGauge.WithLabelValues(sloName).Set(actual)
}
```

## Best Practices

### 1. SLO Selection
- Choose SLIs that directly impact user experience
- Start with fewer, well-understood SLOs
- Ensure SLOs are measurable and actionable

### 2. Error Budget Policy
- Define clear actions when error budget is exhausted
- Prioritize reliability work when budget is low
- Allow for planned maintenance windows

### 3. Monitoring Coverage
- Ensure all critical user journeys are covered
- Monitor from multiple perspectives (client, server, network)
- Include both real-time and historical analysis

### 4. Communication
- Share SLO status with stakeholders
- Include SLO performance in incident reviews
- Document SLO violations and remediation

## Conclusion

These SLI/SLO definitions provide a comprehensive framework for measuring and maintaining the reliability of the MCP Bridge system. Regular reviews and adjustments ensure the objectives remain aligned with user needs and business requirements.