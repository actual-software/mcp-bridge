# MCP Gateway Log Aggregation Queries

## Error Analysis Queries

### 1. Error Rate by Type (Last Hour)
```logql
{app="mcp-gateway"} 
  | json 
  | error_type != "" 
  | __error__="" 
  | unwrap duration [1h] 
  | by (error_type) 
  | rate[5m]
```

### 2. Top Error Messages with Context
```logql
{app="mcp-gateway", level="error"} 
  | json 
  | line_format "{{.msg}} - {{.error_context}}"
  | pattern `<msg> - <context>`
  | topk(20, count by (msg, context))
```

### 3. Errors by Component and Operation
```logql
{app="mcp-gateway"} 
  | json 
  | component != "" 
  | operation != ""
  | __error__=""
  | line_format "{{.component}}/{{.operation}}: {{.msg}}"
  | pattern `<component>/<operation>: <msg>`
  | count by (component, operation, msg)
```

### 4. High Severity Errors with Stack Traces
```logql
{app="mcp-gateway", level="error"} 
  | json 
  | severity =~ "HIGH|CRITICAL"
  | line_format "{{.msg}}\n{{.stack_trace}}"
```

### 5. Retryable vs Non-Retryable Errors
```logql
sum by (retryable) (
  count_over_time(
    {app="mcp-gateway"} 
    | json 
    | retryable != ""
    [5m]
  )
)
```

## Request Tracing Queries

### 6. Request Flow by Trace ID
```logql
{app="mcp-gateway"} 
  | json 
  | trace_id="<TRACE_ID>"
  | line_format "{{.timestamp}} [{{.request_id}}] {{.msg}}"
```

### 7. Slow Requests (>1s)
```logql
{app="mcp-gateway"} 
  | json 
  | msg="request completed"
  | duration > 1s
  | line_format "{{.trace_id}} {{.method}} {{.duration}}"
```

### 8. Failed Requests by User
```logql
{app="mcp-gateway"} 
  | json 
  | msg="request failed"
  | user_id != ""
  | topk(10, count by (user_id))
```

## Authentication & Authorization Queries

### 9. Authentication Failures
```logql
{app="mcp-gateway"} 
  | json 
  | operation="websocket_auth"
  | error_type="UNAUTHORIZED"
  | line_format "{{.client_ip}} - {{.error}}"
  | count by (client_ip)
```

### 10. Rate Limit Violations
```logql
{app="mcp-gateway"} 
  | json 
  | error_type="RATE_LIMIT"
  | line_format "{{.client_ip}} {{.user_id}}"
  | count by (client_ip, user_id)
```

## Backend Error Queries

### 11. Backend Errors by Type
```logql
{app="mcp-gateway"} 
  | json 
  | component =~ ".*_backend"
  | error != ""
  | count by (component, backend_name)
```

### 12. WebSocket Connection Errors
```logql
{app="mcp-gateway"} 
  | json 
  | operation="websocket_upgrade" OR operation="websocket_auth"
  | error != ""
  | line_format "{{.client_ip}} {{.error}}"
```

## Performance & Latency Queries

### 13. Request Latency Percentiles
```logql
quantile_over_time(0.95,
  {app="mcp-gateway"} 
  | json 
  | msg="request completed"
  | unwrap duration [5m]
) by (method)
```

### 14. Error Handling Latency
```logql
{app="mcp-gateway"} 
  | json 
  | msg="request failed"
  | duration != ""
  | histogram_quantile(0.95, 
      sum(rate(duration[5m])) by (le, error_type)
    )
```

## Circuit Breaker & Health Queries

### 15. Circuit Breaker State Changes
```logql
{app="mcp-gateway"} 
  | json 
  | msg =~ ".*circuit breaker.*"
  | line_format "{{.endpoint}} {{.state}}"
```

### 16. Unhealthy Backends
```logql
{app="mcp-gateway"} 
  | json 
  | msg =~ ".*health check failed.*"
  | line_format "{{.backend_name}} {{.address}}"
  | count by (backend_name, address)
```

## Session & Connection Queries

### 17. Session Creation Failures
```logql
{app="mcp-gateway"} 
  | json 
  | operation="create_session"
  | error != ""
  | line_format "{{.user_id}} {{.error}}"
```

### 18. Connection Limit Reached
```logql
{app="mcp-gateway"} 
  | json 
  | msg="Connection limit reached"
  | count by (client_ip)
```

## Alerting Queries

### 19. Critical Errors Alert
```logql
sum(rate(
  {app="mcp-gateway"} 
  | json 
  | severity="CRITICAL"
  [1m]
)) > 0.1
```

### 20. Error Rate Spike Alert
```logql
sum(rate(
  {app="mcp-gateway", level="error"}
  [5m]
)) > 10
```

## Debugging Queries

### 21. Errors with Full Context
```logql
{app="mcp-gateway", level="error"} 
  | json 
  | line_format `
Time: {{.timestamp}}
Trace: {{.trace_id}}
Request: {{.request_id}}
User: {{.user_id}}
Component: {{.component}}
Operation: {{.operation}}
Error Type: {{.error_type}}
Message: {{.msg}}
Context: {{.error_context}}
`
```

### 22. Request Path Analysis
```logql
{app="mcp-gateway"} 
  | json 
  | trace_id != ""
  | line_format "{{.component}} -> {{.operation}}"
  | pattern `<component> -> <operation>`
  | count by (component, operation)
```

## Dashboard Queries

### 23. Error Dashboard Overview
```logql
sum by (error_type) (
  count_over_time(
    {app="mcp-gateway", level="error"} 
    | json 
    [5m]
  )
)
```

### 24. Request Success Rate
```logql
100 * (
  sum(rate({app="mcp-gateway"} |= "request completed" [5m])) /
  sum(rate({app="mcp-gateway"} |~ "request (completed|failed)" [5m]))
)
```

### 25. Top Error Patterns
```logql
topk(10,
  sum by (error_code) (
    count_over_time(
      {app="mcp-gateway"} 
      | json 
      | error_code != ""
      [1h]
    )
  )
)
```

## Usage Instructions

1. Replace placeholder values like `<TRACE_ID>` with actual values
2. Adjust time ranges (`[5m]`, `[1h]`) based on your needs
3. Use these queries in Grafana Loki or your log aggregation tool
4. Combine with Prometheus metrics for comprehensive monitoring

## Query Optimization Tips

1. Use specific labels to reduce data scanning
2. Add time constraints to limit query scope
3. Use `line_format` for better readability
4. Leverage `pattern` for structured extraction
5. Cache frequently used queries