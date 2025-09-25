# MCP Gateway Monitoring

This directory contains monitoring configurations for the MCP Gateway, including Prometheus alerts, SLO rules, and Grafana dashboards.

## Overview

The monitoring stack provides comprehensive observability for:
- Error tracking and analysis
- Request tracing and correlation
- Backend health monitoring  
- Performance metrics and SLOs
- Distributed tracing integration

## Components

### Prometheus Configuration

#### SLO Rules (`prometheus-slo-rules.yaml`)
- Availability SLO: 99.9% success rate
- Latency SLO: 95% of requests under 500ms
- Error budget tracking
- Request rate calculations

#### Alerts (`prometheus-slo-alerts.yaml`)
- SLO violation alerts
- Error rate spike detection
- Latency threshold breaches
- Authentication failure monitoring
- Circuit breaker state changes

### Grafana Dashboards

Located in `dashboards/` directory:

#### 1. Error Analysis Dashboard (`error-analysis.json`)
- Total error rate and success rate gauges
- Error breakdown by type, severity, and component
- HTTP status code distribution
- Retryable vs non-retryable error tracking
- Top error patterns table

#### 2. Request Tracing Dashboard (`request-tracing.json`)
- Trace timeline visualization with Loki integration
- Request latency percentiles (p50, p95, p99)
- Failed request logs with trace correlation
- Request rate by method and namespace
- Jaeger distributed tracing integration

#### 3. Backend Errors Dashboard (`backend-errors.json`)
- Backend-specific error rates
- Health gauges for WebSocket, Stdio, and SSE backends
- Connection error tracking
- Backend error logs with context
- Error handling latency metrics

## Metrics Exported

The gateway exports metrics on port 9090 at `/metrics` endpoint:

### Error Metrics
- `gateway_errors_total`: Counter of errors by type, component, severity, HTTP status
- `gateway_error_handling_duration_seconds`: Histogram of error handling latency
- `gateway_error_recovery_total`: Counter of recovered errors

### Request Metrics
- `gateway_requests_total`: Counter of total requests by method, namespace
- `gateway_request_duration_seconds`: Histogram of request latencies
- `gateway_backend_requests_total`: Counter of backend requests by type

### Connection Metrics
- `gateway_websocket_connections`: Gauge of active WebSocket connections
- `gateway_session_count`: Gauge of active sessions
- `gateway_connection_limit_reached_total`: Counter of connection limit hits

## Log Aggregation Queries

See `../logging/queries.md` for pre-built LogQL queries for:
- Error analysis and patterns
- Request flow tracing
- Authentication monitoring
- Performance analysis
- Circuit breaker tracking

## Setup Instructions

### 1. Deploy Prometheus

```bash
kubectl apply -f prometheus-slo-rules.yaml
kubectl apply -f prometheus-slo-alerts.yaml
```

### 2. Import Grafana Dashboards

1. Access Grafana UI
2. Navigate to Dashboards → Import
3. Upload JSON files from `dashboards/` directory
4. Configure data sources:
   - Prometheus for metrics
   - Loki for logs
   - Jaeger for traces (optional)

### 3. Configure Data Sources

#### Prometheus
```yaml
url: http://prometheus:9090
```

#### Loki
```yaml
url: http://loki:3100
```

#### Jaeger (Optional)
```yaml
url: http://jaeger:16686
```

## Dashboard Usage

### Error Analysis
- Monitor overall error rates and patterns
- Identify problematic components
- Track error severity distribution
- Analyze HTTP status codes

### Request Tracing
- Search for specific trace IDs
- Correlate logs across components
- Monitor latency trends
- Identify slow requests

### Backend Monitoring
- Track backend-specific errors
- Monitor connection health
- Analyze error handling performance
- Debug backend issues

## Alert Configuration

Alerts are sent to configured notification channels. Update the alert manager configuration to add:
- Slack webhooks
- PagerDuty integration
- Email notifications
- Custom webhooks

## Best Practices

1. **Regular Review**: Review error patterns weekly
2. **SLO Monitoring**: Track error budget consumption
3. **Capacity Planning**: Monitor connection and request rates
4. **Incident Response**: Use trace IDs for debugging
5. **Dashboard Customization**: Adjust time ranges and filters as needed

## Troubleshooting

### High Error Rate
1. Check Error Analysis dashboard
2. Filter by component to isolate issues
3. Review error logs with trace IDs
4. Check backend health gauges

### Latency Issues
1. Review Request Tracing dashboard
2. Check p95/p99 percentiles
3. Analyze slow request logs
4. Monitor backend response times

### Connection Problems
1. Check Backend Errors dashboard
2. Review WebSocket upgrade failures
3. Monitor authentication errors
4. Check connection limits

## Integration with CI/CD

The dashboards can be automatically deployed using:
```bash
# Export dashboard
curl -X GET http://grafana/api/dashboards/uid/error-analysis > error-analysis.json

# Import dashboard
curl -X POST http://grafana/api/dashboards/db \
  -H "Content-Type: application/json" \
  -d @error-analysis.json
```

## Future Enhancements

- [ ] Add custom metrics for business logic
- [ ] Implement anomaly detection
- [ ] Add cost tracking metrics
- [ ] Create mobile-friendly dashboards
- [ ] Add synthetic monitoring