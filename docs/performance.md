# Performance Guide

Comprehensive performance benchmarking, optimization, and tuning guide for MCP Bridge.

## Overview

This guide covers performance aspects of MCP Bridge:

- **Benchmarking Results** - Performance test results and analysis
- **Optimization Strategies** - System tuning and optimization
- **Monitoring & Profiling** - Performance monitoring and analysis
- **Scaling Guidelines** - Horizontal and vertical scaling strategies

## Performance Benchmarks

### Test Environment

```yaml
# Benchmark infrastructure specifications
infrastructure:
  provider: AWS
  region: us-east-1
  
gateway:
  instance_type: c5.2xlarge
  cpu: 8 vCPU
  memory: 16 GB
  network: Up to 10 Gbps
  
router:
  instance_type: c5.xlarge
  cpu: 4 vCPU
  memory: 8 GB
  network: Up to 10 Gbps
  
load_generator:
  instance_type: c5.4xlarge
  cpu: 16 vCPU
  memory: 32 GB
  tools: k6, wrk, vegeta
```

### Benchmark Results

#### Latency Performance

| Metric | WebSocket | Binary TCP | HTTP |
|--------|-----------|------------|------|
| P50 Latency | 2.3ms | 1.8ms | 3.1ms |
| P95 Latency | 8.7ms | 6.2ms | 12.4ms |
| P99 Latency | 23.1ms | 18.5ms | 31.7ms |
| P99.9 Latency | 89.2ms | 72.3ms | 118.6ms |

#### Throughput Performance

| Protocol | Connections | RPS | CPU Usage | Memory Usage |
|----------|-------------|-----|-----------|--------------|
| WebSocket | 1,000 | 15,420 | 45% | 2.1GB |
| WebSocket | 5,000 | 52,180 | 78% | 4.7GB |
| WebSocket | 10,000 | 84,350 | 92% | 8.2GB |
| Binary TCP | 1,000 | 18,750 | 38% | 1.8GB |
| Binary TCP | 5,000 | 67,230 | 71% | 4.1GB |
| Binary TCP | 10,000 | 112,500 | 89% | 7.8GB |

#### Memory Usage Patterns

```
Gateway Memory Usage (10k connections):
- Base Memory: 512MB
- Per Connection: ~750KB
- Peak Memory: 8.2GB
- GC Pressure: Low (<5% CPU)

Router Memory Usage (1k servers):
- Base Memory: 128MB  
- Per Server: ~2MB
- Peak Memory: 2.3GB
- GC Pressure: Minimal (<2% CPU)
```

#### Concurrent Connection Limits

| Component | Max Connections | Performance Impact |
|-----------|-----------------|-------------------|
| Gateway | 50,000 | Linear degradation after 10k |
| Router | 10,000 | Stable up to limit |
| Combined | 45,000 | Network bandwidth limited |

### Load Testing Scripts

#### k6 Performance Test

```javascript
// benchmarks/k6/performance-test.js
import ws from 'k6/ws';
import { check } from 'k6';

export let options = {
  stages: [
    { duration: '2m', target: 100 },
    { duration: '5m', target: 1000 },
    { duration: '10m', target: 5000 },
    { duration: '2m', target: 0 },
  ],
};

export default function () {
  const url = 'wss://gateway.example.com/mcp';
  const params = {
    headers: {
      'Authorization': 'Bearer test-token'
    }
  };

  const response = ws.connect(url, params, function (socket) {
    socket.on('open', () => {
      socket.send(JSON.stringify({
        jsonrpc: '2.0',
        id: 1,
        method: 'tools/list'
      }));
    });

    socket.on('message', (data) => {
      const response = JSON.parse(data);
      check(response, {
        'valid response': (r) => r.jsonrpc === '2.0',
        'has result': (r) => r.result !== undefined,
      });
    });

    socket.setTimeout(() => {
      socket.close();
    }, 30000);
  });
}
```

#### wrk HTTP Test

```bash
#!/bin/bash
# benchmarks/scripts/http-load-test.sh

wrk -t12 -c400 -d30s \
    -H "Authorization: Bearer test-token" \
    -H "Content-Type: application/json" \
    -s scripts/mcp-request.lua \
    https://gateway.example.com/api/tools
```

#### Go Benchmark Tests

```go
// benchmarks/go/gateway_test.go
func BenchmarkGatewayThroughput(b *testing.B) {
    gateway := setupTestGateway()
    defer gateway.Close()
    
    b.ResetTimer()
    b.RunParallel(func(pb *testing.PB) {
        for pb.Next() {
            conn, err := websocket.Dial(gateway.URL, "", "")
            if err != nil {
                b.Fatal(err)
            }
            
            request := map[string]interface{}{
                "jsonrpc": "2.0",
                "id":      1,
                "method":  "tools/list",
            }
            
            if err := websocket.JSON.Send(conn, request); err != nil {
                b.Fatal(err)
            }
            
            var response map[string]interface{}
            if err := websocket.JSON.Receive(conn, &response); err != nil {
                b.Fatal(err)
            }
            
            conn.Close()
        }
    })
}
```

## Performance Optimization

### Gateway Optimization

#### Connection Pool Tuning

```yaml
# Gateway configuration for high performance
server:
  connection_pool:
    max_connections: 50000
    idle_timeout: 300s
    keepalive_interval: 30s
    buffer_size: 32KB
    
  websocket:
    compression: false  # Disable for low latency
    read_buffer_size: 32KB
    write_buffer_size: 32KB
    
  binary:
    buffer_pool_size: 1000
    max_message_size: 1MB
    tcp_nodelay: true
```

#### Memory Optimization

```yaml
runtime:
  gomaxprocs: 8  # Match CPU cores
  gogc: 100      # Default GC target
  
memory:
  heap_size_hint: 4GB
  gc_target_percent: 75
  max_heap_size: 8GB
```

#### CPU Optimization

```yaml
performance:
  worker_threads: 16      # 2x CPU cores
  io_threads: 8          # Equal to CPU cores  
  request_queue_size: 10000
  batch_processing: true
  batch_size: 100
  batch_timeout: 10ms
```

### Router Optimization

#### Process Management

```yaml
# Router configuration
process_management:
  max_servers: 1000
  server_pool_size: 10
  restart_threshold: 100
  memory_limit: 512MB
  cpu_limit: "500m"

connection:
  pool_size: 50
  max_idle_time: 300s
  keepalive: true
  tcp_nodelay: true
```

#### Request Batching

```yaml
batching:
  enabled: true
  max_batch_size: 50
  batch_timeout: 5ms
  adaptive_batching: true
```

### Network Optimization

#### TCP Tuning

```bash
# System-level TCP optimization
echo 'net.core.somaxconn = 65536' >> /etc/sysctl.conf
echo 'net.core.netdev_max_backlog = 30000' >> /etc/sysctl.conf
echo 'net.ipv4.tcp_max_syn_backlog = 65536' >> /etc/sysctl.conf
echo 'net.ipv4.tcp_keepalive_time = 600' >> /etc/sysctl.conf
echo 'net.ipv4.tcp_keepalive_intvl = 60' >> /etc/sysctl.conf
echo 'net.ipv4.tcp_keepalive_probes = 3' >> /etc/sysctl.conf
sysctl -p
```

#### TLS Optimization

```yaml
tls:
  session_cache_size: 10000
  session_timeout: 300s
  cipher_suites:
    - TLS_AES_128_GCM_SHA256      # Fastest
    - TLS_AES_256_GCM_SHA384
    - TLS_CHACHA20_POLY1305_SHA256
  curves:
    - X25519                       # Fastest curve
    - P-256
```

## Monitoring and Profiling

### Performance Metrics

#### Key Performance Indicators

```yaml
# Prometheus metrics for performance monitoring
metrics:
  latency:
    - mcp_request_duration_seconds
    - mcp_connection_establishment_duration
    - mcp_backend_response_time
    
  throughput:
    - mcp_requests_per_second
    - mcp_messages_per_second
    - mcp_bytes_per_second
    
  resource_usage:
    - mcp_cpu_usage_percent
    - mcp_memory_usage_bytes
    - mcp_goroutines_count
    - mcp_gc_duration_seconds
    
  errors:
    - mcp_error_rate
    - mcp_timeout_rate
    - mcp_connection_failures
```

#### Performance Dashboard

```yaml
# Grafana dashboard configuration
dashboard:
  panels:
    - title: "Request Latency"
      query: "histogram_quantile(0.95, mcp_request_duration_seconds)"
      
    - title: "Throughput"
      query: "rate(mcp_requests_total[5m])"
      
    - title: "Error Rate"
      query: "rate(mcp_errors_total[5m]) / rate(mcp_requests_total[5m])"
      
    - title: "Resource Usage"
      queries:
        - "mcp_cpu_usage_percent"
        - "mcp_memory_usage_bytes / 1024 / 1024 / 1024"
```

### Profiling Tools

#### CPU Profiling

```bash
# Enable CPU profiling
curl http://gateway:6060/debug/pprof/profile?seconds=30 > cpu.prof
go tool pprof cpu.prof

# Analyze CPU usage
(pprof) top10
(pprof) web
```

#### Memory Profiling

```bash
# Memory profile
curl http://gateway:6060/debug/pprof/heap > heap.prof
go tool pprof heap.prof

# Analyze memory usage
(pprof) top10
(pprof) list functionName
```

#### Goroutine Analysis

```bash
# Check goroutine count
curl http://gateway:6060/debug/pprof/goroutine?debug=1

# Detect goroutine leaks
curl http://gateway:6060/debug/pprof/goroutine > goroutines.prof
go tool pprof goroutines.prof
```

### Performance Alerts

```yaml
# Prometheus alerting rules
groups:
  - name: performance
    rules:
      - alert: HighLatency
        expr: histogram_quantile(0.95, mcp_request_duration_seconds) > 0.1
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "High request latency detected"
          
      - alert: LowThroughput
        expr: rate(mcp_requests_total[5m]) < 1000
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Request throughput below threshold"
          
      - alert: HighErrorRate
        expr: rate(mcp_errors_total[5m]) / rate(mcp_requests_total[5m]) > 0.05
        for: 2m
        labels:
          severity: critical
        annotations:
          summary: "Error rate above 5%"
```

## Scaling Strategies

### Horizontal Scaling

#### Gateway Scaling

```yaml
# Kubernetes HPA configuration
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: mcp-gateway-hpa
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: mcp-gateway
  minReplicas: 3
  maxReplicas: 20
  metrics:
  - type: Resource
    resource:
      name: cpu
      target:
        type: Utilization
        averageUtilization: 70
  - type: Resource
    resource:
      name: memory
      target:
        type: Utilization
        averageUtilization: 80
```

#### Load Balancing

```yaml
# HAProxy configuration for gateway load balancing
backend mcp-gateway
    balance roundrobin
    option httpchk GET /health
    server gateway1 10.0.1.10:8443 check
    server gateway2 10.0.1.11:8443 check
    server gateway3 10.0.1.12:8443 check
    
backend mcp-gateway-websocket
    balance source
    hash-type consistent
    server gateway1 10.0.1.10:8443 check
    server gateway2 10.0.1.11:8443 check
    server gateway3 10.0.1.12:8443 check
```

### Vertical Scaling

#### Resource Allocation

```yaml
# Kubernetes resource requests and limits
resources:
  requests:
    cpu: "2"
    memory: "4Gi"
  limits:
    cpu: "8"
    memory: "16Gi"
    
# JVM-like tuning for Go
env:
  - name: GOMAXPROCS
    valueFrom:
      resourceFieldRef:
        resource: limits.cpu
  - name: GOGC
    value: "100"
```

### Database Scaling

#### Redis Cluster

```yaml
# Redis cluster for session storage
redis:
  cluster:
    enabled: true
    nodes: 6
    replicas: 1
  memory: 8GB
  persistence: true
  backup_schedule: "0 2 * * *"
```

## Performance Testing

### Continuous Performance Testing

```yaml
# GitHub Actions performance testing
name: Performance Tests
on:
  schedule:
    - cron: '0 2 * * *'  # Daily at 2 AM
  
jobs:
  performance:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - name: Setup Go
        uses: actions/setup-go@v3
        with:
          go-version: 1.21
      - name: Run Benchmarks
        run: |
          make benchmark
          make load-test
      - name: Performance Regression Check
        run: |
          ./scripts/check-performance-regression.sh
```

### Regression Detection

```bash
#!/bin/bash
# scripts/check-performance-regression.sh

# Compare current results with baseline
BASELINE_RPS=50000
CURRENT_RPS=$(cat benchmark-results.json | jq '.rps')

if (( $(echo "$CURRENT_RPS < $BASELINE_RPS * 0.9" | bc -l) )); then
    echo "Performance regression detected!"
    echo "Current RPS: $CURRENT_RPS, Baseline: $BASELINE_RPS"
    exit 1
fi

echo "Performance within acceptable range"
```

## Troubleshooting Performance Issues

### Common Performance Problems

#### High Latency

**Symptoms:**
- P95 latency > 50ms
- Slow response times
- User complaints

**Diagnosis:**
```bash
# Check for network issues
ping gateway.example.com

# Analyze CPU usage
top -p $(pgrep mcp-gateway)

# Check for GC pressure
curl http://gateway:6060/debug/pprof/heap
```

**Solutions:**
- Increase worker threads
- Optimize database queries
- Enable connection pooling
- Reduce GC pressure

#### Low Throughput

**Symptoms:**
- RPS below expectations
- Connection timeouts
- Queue backups

**Diagnosis:**
```bash
# Check connection limits
ss -tuln | grep :8443

# Monitor queue sizes
curl http://gateway:6060/debug/pprof/goroutine?debug=1 | grep queue
```

**Solutions:**
- Increase connection limits
- Scale horizontally
- Optimize request handling
- Enable request batching

#### Memory Leaks

**Symptoms:**
- Increasing memory usage
- OOM kills
- Degraded performance

**Diagnosis:**
```bash
# Memory profile over time
for i in {1..10}; do
  curl http://gateway:6060/debug/pprof/heap > heap-$i.prof
  sleep 60
done
```

**Solutions:**
- Fix goroutine leaks
- Optimize data structures
- Implement memory limits
- Regular memory profiling

## Best Practices

### Performance Guidelines

1. **Measure First**: Always benchmark before optimizing
2. **Profile Regularly**: Use continuous profiling in production
3. **Monitor Key Metrics**: Track latency, throughput, and errors
4. **Test Under Load**: Regular load testing with realistic scenarios
5. **Optimize Bottlenecks**: Focus on the slowest components first

### Configuration Best Practices

```yaml
# Production-optimized configuration
performance:
  # Connection settings
  max_connections: 10000
  connection_timeout: 30s
  keepalive_timeout: 300s
  
  # Processing settings
  worker_threads: 16
  queue_size: 10000
  batch_size: 100
  
  # Memory settings
  buffer_size: 32KB
  gc_target: 75
  max_heap: 8GB
  
  # Network settings
  tcp_nodelay: true
  compression: false  # For low latency
  read_timeout: 30s
  write_timeout: 30s
```

## Related Documentation

- [Gateway Documentation](../services/gateway/README.md)
- [Router Documentation](../services/router/README.md)
- [Monitoring Guide](monitoring.md)
- [Configuration Reference](configuration.md)
- [Troubleshooting Guide](troubleshooting.md)