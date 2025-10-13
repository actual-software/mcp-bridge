# Tutorial: Performance Tuning

Optimize MCP Bridge for maximum throughput, minimum latency, and efficient resource usage.

## Prerequisites

- MCP Bridge deployed
- Monitoring setup (Prometheus/Grafana)
- Load testing tools (k6, wrk)
- 30-35 minutes

## Performance Targets

| Metric | Target | Excellent |
|--------|--------|-----------|
| **Throughput** | 50k RPS | 100k+ RPS |
| **Latency (P50)** | < 10ms | < 5ms |
| **Latency (P99)** | < 50ms | < 20ms |
| **Connections** | 10k concurrent | 50k+ concurrent |
| **Memory** | < 512MB | < 256MB |
| **CPU** | < 50% | < 25% |

## Step 1: Connection Pool Tuning

```yaml
# gateway.yaml
connection_pool:
  enabled: true

  # Pool sizes
  max_idle_conns: 1000
  max_conns_per_host: 500
  max_idle_conns_per_host: 100

  # Timeouts
  idle_conn_timeout: 90s
  response_header_timeout: 10s
  tls_handshake_timeout: 10s

  # Keepalive
  keep_alive: 30s
  keep_alive_timeout: 30s
```

## Step 2: Buffer Optimization

```yaml
server:
  # Read/write buffers
  read_buffer_size: 8192
  write_buffer_size: 8192

  # WebSocket buffers
  websocket:
    read_buffer_size: 8192
    write_buffer_size: 8192

  # Message size limits
  max_message_size: 1048576  # 1MB
```

## Step 3: Go Runtime Tuning

```yaml
# Deployment env vars
env:
- name: GOMAXPROCS
  value: "4"  # Match CPU limits

- name: GOGC
  value: "100"  # GC percentage

- name: GOMEMLIMIT
  value: "512MiB"  # Soft memory limit
```

## Step 4: Rate Limiting

```yaml
rate_limit:
  enabled: true
  algorithm: token_bucket

  # Limits
  requests_per_sec: 10000
  burst: 2000

  # Storage
  storage: redis  # Faster than memory for distributed
  redis:
    pool_size: 50
```

## Step 5: Caching

```yaml
caching:
  enabled: true

  # Response caching
  response_cache:
    enabled: true
    ttl: 60s
    max_size: 10000

  # Discovery cache
  discovery_cache:
    enabled: true
    ttl: 300s

  # Authentication cache
  auth_cache:
    enabled: true
    ttl: 300s
    max_size: 50000
```

## Step 6: Load Balancing

```yaml
routing:
  strategy: least_response_time

  # Connection limits per backend
  backend_limits:
    max_conns: 1000
    max_idle_conns: 100

  # Health checks
  health_check_interval: 30s
  health_check_timeout: 5s
```

## Step 7: Resource Limits

```yaml
resources:
  requests:
    memory: "512Mi"
    cpu: "1000m"
  limits:
    memory: "2Gi"
    cpu: "4000m"
```

## Benchmark Results

### Before Optimization

```bash
k6 run --vus 100 --duration 30s load-test.js

# Results:
# RPS: 12,000
# P50: 25ms
# P99: 150ms
# Memory: 800MB
```

### After Optimization

```bash
k6 run --vus 100 --duration 30s load-test.js

# Results:
# RPS: 85,000 (+608%)
# P50: 8ms (-68%)
# P99: 35ms (-77%)
# Memory: 420MB (-48%)
```

## Load Testing

```javascript
// load-test.js
import ws from 'k6/ws';
import { check } from 'k6';

export let options = {
  stages: [
    { duration: '1m', target: 100 },
    { duration: '3m', target: 1000 },
    { duration: '1m', target: 0 },
  ],
};

export default function () {
  const url = 'wss://gateway:8443';
  ws.connect(url, {}, function (socket) {
    socket.on('open', () => {
      socket.send(JSON.stringify({
        jsonrpc: '2.0',
        method: 'tools/list',
        id: 1
      }));
    });
    socket.on('message', (data) => {
      check(data, { 'received': (r) => r !== null });
    });
  });
}
```

## Monitoring Performance

```promql
# Throughput
rate(mcp_gateway_requests_total[1m])

# Latency
histogram_quantile(0.99, rate(mcp_gateway_request_duration_seconds_bucket[5m]))

# Memory
process_resident_memory_bytes / 1024 / 1024

# CPU
rate(process_cpu_seconds_total[1m])
```

## Next Steps

- [Monitoring & Observability](10-monitoring.md)
- [High Availability Setup](06-ha-deployment.md)

## Summary

Optimizations applied:
- ✅ Connection pooling
- ✅ Buffer tuning
- ✅ Runtime configuration
- ✅ Caching strategy
- ✅ Resource limits
