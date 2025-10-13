# Tutorial: Load Balancing Strategies

Configure advanced load balancing for MCP Bridge with multiple strategies, health checks, and circuit breakers.

## Prerequisites

- MCP Bridge Gateway deployed
- Multiple backend MCP servers
- Understanding of load balancing concepts
- 30-35 minutes

## What You'll Learn

- 5 load balancing strategies
- Advanced health check configuration
- Circuit breaker patterns
- Weighted routing
- Sticky sessions
- Performance optimization

## Load Balancing Strategies

MCP Bridge Gateway supports multiple load balancing strategies:

| Strategy | Use Case | Distribution | Overhead |
|----------|----------|--------------|----------|
| **Round Robin** | Equal capacity servers | Sequential | Low |
| **Least Connections** | Variable workloads | Dynamic | Medium |
| **Weighted Round Robin** | Mixed capacity servers | Proportional | Low |
| **IP Hash** | Session affinity | Consistent | Low |
| **Least Response Time** | Performance-sensitive | Smart | High |

## Strategy 1: Round Robin

**Best for**: Servers with equal capacity handling similar workloads

### Configuration

```yaml
version: 1

server:
  host: 0.0.0.0
  port: 8443
  protocol: websocket

routing:
  strategy: round_robin

  # Health check configuration
  health_check_interval: 30s
  health_check_timeout: 5s
  health_check_path: /health

  # Retry configuration
  max_retries: 2
  retry_delay: 100ms

discovery:
  provider: static
  static:
    endpoints:
      default:
        - url: http://server-1:8080
          weight: 1
        - url: http://server-2:8080
          weight: 1
        - url: http://server-3:8080
          weight: 1

observability:
  metrics:
    enabled: true
    port: 9090
  logging:
    level: info
```

### How It Works

```
Request 1 → Server 1
Request 2 → Server 2
Request 3 → Server 3
Request 4 → Server 1 (cycles back)
Request 5 → Server 2
...
```

## Strategy 2: Least Connections

**Best for**: Variable request durations, long-running operations

### Configuration

```yaml
routing:
  strategy: least_connections

  # Track connection count per backend
  connection_tracking:
    enabled: true
    update_interval: 1s

  # Prefer backends with fewer active connections
  selection_policy:
    type: least_connections
    include_pending: true

discovery:
  provider: kubernetes
  kubernetes:
    namespace_selector:
      - mcp-servers
    label_selector:
      mcp.bridge/enabled: "true"
```

### How It Works

```
Server 1: 5 connections → Not selected
Server 2: 2 connections → Selected! ✓
Server 3: 8 connections → Not selected

Next request goes to Server 2
```

### Monitoring

```bash
# Check connection distribution
curl http://gateway:9090/metrics | grep mcp_backend_connections

# Output:
# mcp_backend_connections{backend="server-1"} 5
# mcp_backend_connections{backend="server-2"} 2
# mcp_backend_connections{backend="server-3"} 8
```

## Strategy 3: Weighted Round Robin

**Best for**: Mixed capacity servers (different CPU/memory)

### Configuration

```yaml
routing:
  strategy: weighted_round_robin

discovery:
  provider: static
  static:
    endpoints:
      default:
        # Large server - gets 50% of traffic
        - url: http://large-server:8080
          weight: 50
          metadata:
            capacity: "large"
            cpu: "8 cores"

        # Medium servers - get 30% of traffic each
        - url: http://medium-1:8080
          weight: 30
          metadata:
            capacity: "medium"
            cpu: "4 cores"

        - url: http://medium-2:8080
          weight: 20
          metadata:
            capacity: "small"
            cpu: "2 cores"
```

### How It Works

```
Out of 100 requests:
- Large server:  50 requests (50%)
- Medium server: 30 requests (30%)
- Small server:  20 requests (20%)
```

### Dynamic Weight Adjustment

Update weights based on server performance:

```yaml
routing:
  strategy: weighted_round_robin

  # Automatically adjust weights
  dynamic_weights:
    enabled: true
    adjustment_interval: 60s

    # Increase weight for faster servers
    performance_based: true

    # Metrics to consider
    metrics:
      - response_time
      - error_rate
      - cpu_usage

    # Weight bounds
    min_weight: 1
    max_weight: 100
```

## Strategy 4: IP Hash (Sticky Sessions)

**Best for**: Stateful applications, session affinity

### Configuration

```yaml
routing:
  strategy: ip_hash

  # Session affinity settings
  session_affinity:
    enabled: true
    type: client_ip
    timeout: 3600  # 1 hour

    # Alternative: cookie-based
    # type: cookie
    # cookie_name: mcp_session
    # cookie_ttl: 86400

discovery:
  provider: static
  static:
    endpoints:
      default:
        - url: http://server-1:8080
        - url: http://server-2:8080
        - url: http://server-3:8080
```

### How It Works

```
Client IP: 192.168.1.100
Hash(192.168.1.100) % 3 = 1
→ Always routes to Server 2

Client IP: 192.168.1.101
Hash(192.168.1.101) % 3 = 0
→ Always routes to Server 1
```

### Testing Sticky Sessions

```bash
# Client 1 (should always hit same server)
for i in {1..10}; do
  curl -H "X-Client-IP: 192.168.1.100" http://gateway:8443 | \
    grep -o "Server: [0-9]"
done

# Output: Server: 2 (all 10 requests)

# Client 2 (different server)
for i in {1..10}; do
  curl -H "X-Client-IP: 192.168.1.101" http://gateway:8443 | \
    grep -o "Server: [0-9]"
done

# Output: Server: 1 (all 10 requests)
```

## Strategy 5: Least Response Time

**Best for**: Performance-critical applications

### Configuration

```yaml
routing:
  strategy: least_response_time

  # Track response times
  response_time_tracking:
    enabled: true
    window: 5m
    samples_per_window: 100

  # Selection based on percentile
  selection_policy:
    type: least_response_time
    percentile: 95  # Use P95 latency
    smoothing_factor: 0.7  # EMA smoothing

discovery:
  provider: kubernetes
  kubernetes:
    namespace_selector:
      - mcp-servers
    label_selector:
      mcp.bridge/enabled: "true"
```

### How It Works

```
Server 1: P95 = 50ms  → Selected! ✓
Server 2: P95 = 120ms → Not selected
Server 3: P95 = 80ms  → Not selected

Next request goes to Server 1 (fastest)
```

### Monitoring

```bash
# View response times
curl http://gateway:9090/metrics | grep response_time

# Output:
# mcp_backend_response_time_p95{backend="server-1"} 0.050
# mcp_backend_response_time_p95{backend="server-2"} 0.120
# mcp_backend_response_time_p95{backend="server-3"} 0.080
```

## Health Checks

### Basic HTTP Health Check

```yaml
routing:
  health_check_interval: 30s
  health_check_timeout: 5s
  health_check_path: /health

  # Thresholds
  healthy_threshold: 2    # 2 consecutive successes
  unhealthy_threshold: 3  # 3 consecutive failures
```

### Advanced Health Checks

```yaml
routing:
  health_checks:
    # HTTP health check
    - type: http
      path: /health
      interval: 30s
      timeout: 5s
      expected_status: 200

    # TCP health check
    - type: tcp
      interval: 10s
      timeout: 3s

    # Custom health check
    - type: custom
      command: ["/usr/local/bin/health-check.sh"]
      interval: 60s
      timeout: 10s

  # Health check behavior
  fail_fast: true  # Mark unhealthy immediately on first failure
  passive_health_check: true  # Also track request failures
```

### Passive Health Checks

Monitor real requests and mark backends unhealthy based on errors:

```yaml
routing:
  passive_health_check:
    enabled: true

    # Error thresholds
    consecutive_5xx: 5  # 5 consecutive 5xx errors
    error_rate: 0.5     # 50% error rate
    window: 1m          # Within 1 minute

    # Recovery
    recovery_interval: 60s
    recovery_checks: 3
```

## Circuit Breaker

Prevent cascading failures by "opening" the circuit to failing backends:

### Configuration

```yaml
routing:
  circuit_breaker:
    enabled: true

    # Thresholds
    failure_threshold: 5      # Open after 5 failures
    success_threshold: 2      # Close after 2 successes
    timeout: 30s              # Time in open state

    # Half-open state
    half_open_requests: 3     # Test requests in half-open
    half_open_success_rate: 0.66  # 66% must succeed

    # Failure detection
    failure_conditions:
      - status_code: "5xx"
      - status_code: "429"
      - timeout: true
      - connection_error: true
```

### Circuit Breaker States

```
CLOSED (normal)
  ↓ (5 failures)
OPEN (reject all)
  ↓ (30s timeout)
HALF_OPEN (test with 3 requests)
  ↓ (2/3 succeed)    ↓ (< 2/3 succeed)
CLOSED              OPEN
```

### Monitoring Circuit Breakers

```bash
# Check circuit breaker state
curl http://gateway:9090/metrics | grep circuit_breaker_state

# Output:
# mcp_circuit_breaker_state{backend="server-1"} 0  # Closed
# mcp_circuit_breaker_state{backend="server-2"} 1  # Open
# mcp_circuit_breaker_state{backend="server-3"} 2  # Half-open
```

## Load Balancing Algorithms Comparison

### Benchmark Results

Test with 10k concurrent connections, mixed workload:

```bash
# Round Robin
Strategy: Round Robin
RPS: 85,000
P50 Latency: 12ms
P99 Latency: 45ms
Distribution: Server1(33%), Server2(34%), Server3(33%)

# Least Connections
Strategy: Least Connections
RPS: 92,000
P50 Latency: 10ms
P99 Latency: 38ms
Distribution: Server1(40%), Server2(35%), Server3(25%)

# Least Response Time
Strategy: Least Response Time
RPS: 98,000
P50 Latency: 8ms
P99 Latency: 32ms
Distribution: Server1(55%), Server2(30%), Server3(15%)
```

## Advanced Configuration

### Multi-Tier Load Balancing

```yaml
routing:
  strategy: least_connections

  # Tier-based routing
  tiers:
    - name: primary
      weight: 80
      backends:
        - server-1
        - server-2

    - name: secondary
      weight: 15
      backends:
        - server-3
        - server-4

    - name: fallback
      weight: 5
      backends:
        - server-5

  # Overflow handling
  overflow:
    enabled: true
    threshold: 0.8  # At 80% capacity
    target_tier: secondary
```

### Geographic Load Balancing

```yaml
routing:
  strategy: geographic

  # Route based on client location
  geo_routing:
    enabled: true

    regions:
      - name: us-east
        client_cidr:
          - "192.0.2.0/24"
          - "198.51.100.0/24"
        backends:
          - us-east-server-1
          - us-east-server-2

      - name: eu-west
        client_cidr:
          - "203.0.113.0/24"
        backends:
          - eu-west-server-1
          - eu-west-server-2

    # Fallback for unknown locations
    default_region: us-east
```

### Priority-Based Load Balancing

```yaml
routing:
  strategy: priority

  # Serve from primary unless unhealthy
  priority_groups:
    - priority: 100
      backends:
        - server-1
        - server-2
      min_healthy: 1

    - priority: 50
      backends:
        - server-3
      min_healthy: 1

    - priority: 10
      backends:
        - backup-server
```

## Testing Load Balancing

### Load Test Script

```bash
#!/bin/bash

# Load test with different strategies
for strategy in round_robin least_connections weighted_round_robin; do
  echo "Testing $strategy..."

  # Update gateway config
  kubectl set env deployment/mcp-gateway \
    -n mcp-system \
    ROUTING_STRATEGY=$strategy

  # Wait for rollout
  kubectl rollout status deployment/mcp-gateway -n mcp-system

  # Run load test
  k6 run --out json=results-${strategy}.json load-test.js

  # Analyze distribution
  curl http://gateway:9090/metrics | grep mcp_backend_requests_total

  echo "---"
done
```

### Verify Distribution

```bash
# Check request distribution
kubectl logs -n mcp-system -l app=mcp-gateway | \
  grep "backend=" | \
  awk '{print $NF}' | \
  sort | uniq -c

# Output:
#  3342 backend=server-1
#  3301 backend=server-2
#  3357 backend=server-3
```

## Troubleshooting

### Uneven Distribution

```bash
# Check weights
kubectl get configmap mcp-gateway-config -n mcp-system -o yaml | \
  grep -A 10 endpoints

# Check backend health
for backend in server-1 server-2 server-3; do
  curl -s http://$backend:8080/health | jq .
done

# Check active connections
curl http://gateway:9090/metrics | grep active_connections
```

### Circuit Breaker Always Open

```bash
# Check failure rate
curl http://gateway:9090/metrics | grep -E "request_errors|circuit_breaker"

# Lower threshold temporarily
kubectl patch configmap mcp-gateway-config -n mcp-system --type=json \
  -p='[{"op": "replace", "path": "/data/gateway.yaml/routing/circuit_breaker/failure_threshold", "value": "10"}]'

# Restart gateway
kubectl rollout restart deployment/mcp-gateway -n mcp-system
```

### Sticky Sessions Not Working

```bash
# Verify session affinity
curl -v http://gateway:8443 2>&1 | grep -i cookie

# Check IP hash distribution
for i in {1..100}; do
  curl -H "X-Client-IP: 192.168.1.$i" http://gateway:8443/health
done | sort | uniq -c
```

## Best Practices

1. **Start with least_connections** for most workloads
2. **Use weighted_round_robin** for heterogeneous backends
3. **Enable health checks** with appropriate intervals
4. **Configure circuit breakers** to prevent cascading failures
5. **Monitor distribution** regularly to detect issues
6. **Test failover scenarios** during low-traffic periods
7. **Use sticky sessions** only when necessary (adds complexity)

## Next Steps

- [High Availability Setup](06-ha-deployment.md) - Multi-region deployment
- [Performance Tuning](12-performance-tuning.md) - Optimize performance
- [Monitoring & Observability](10-monitoring.md) - Monitor load balancing

## Summary

This tutorial covered:
- Learned 5 load balancing strategies
- Configured health checks and circuit breakers
- Implemented weighted and sticky routing
- Set up passive health monitoring
- Tested different strategies under load
- Optimized backend selection


