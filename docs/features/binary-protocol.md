# Binary TCP Protocol

The MCP Binary Protocol provides a high-performance alternative to WebSocket for scenarios requiring maximum throughput and minimal latency.

## Overview

The binary protocol offers:
- **10x throughput** compared to WebSocket JSON
- **50% lower latency** through binary encoding
- **30% less bandwidth** with compact message format
- **Native multiplexing** for concurrent requests
- **Built-in compression** support

## Quick Start

### Client Configuration
```yaml
# mcp-router.yaml
gateway:
  url: tcp://gateway.example.com:8444  # Note: tcp:// protocol
  
advanced:
  protocol: tcp
  compression:
    enabled: true
    algorithm: zstd  # zstd, gzip, or none
    level: 3        # 1-9 for gzip, 1-22 for zstd
```

### Gateway Configuration
```yaml
server:
  protocol: both     # Enable both WebSocket and TCP
  tcp_port: 8444
  tcp_health_port: 9002
  
  # TCP-specific settings
  tcp:
    max_frame_size: 10485760  # 10MB
    read_buffer_size: 65536   # 64KB
    write_buffer_size: 65536  # 64KB
    keepalive_period: 30s
    keepalive_probes: 3
```

## Protocol Specification

### Wire Format

```
+----------------+----------------+----------------+----------------+
|  Magic Bytes   |    Version     |  Message Type  |   Reserved     |
|    (4 bytes)   |    (1 byte)    |    (1 byte)    |   (2 bytes)    |
+----------------+----------------+----------------+----------------+
|                        Payload Length (4 bytes)                    |
+----------------+----------------+----------------+----------------+
|                           Payload Data                             |
|                         (variable length)                          |
+----------------+----------------+----------------+----------------+
```

### Header Fields

| Field | Size | Description |
|-------|------|-------------|
| Magic Bytes | 4 bytes | `0x4D435042` ("MCPB") |
| Version | 1 byte | Protocol version (currently 1) |
| Message Type | 1 byte | See message types below |
| Reserved | 2 bytes | Reserved for future use |
| Payload Length | 4 bytes | Length of payload in bytes |

### Message Types

```go
const (
    MessageTypeRequest      = 0x01
    MessageTypeResponse     = 0x02
    MessageTypeError        = 0x03
    MessageTypePing         = 0x04
    MessageTypePong         = 0x05
    MessageTypeAuth         = 0x06
    MessageTypeAuthResponse = 0x07
    MessageTypeClose        = 0x08
)
```

## Authentication

### Initial Authentication
```
Client → Gateway: AUTH message with credentials
Gateway → Client: AUTH_RESPONSE with session token
Client → Gateway: REQUEST messages with session token
```

### Per-Message Authentication
```yaml
# Enable in gateway config
auth:
  per_message_auth: true
  per_message_cache: 300  # Cache auth for 5 minutes
```

Each message includes auth header:
```
+----------------+----------------+
|  Auth Type     |  Auth Length   |
|   (1 byte)     |   (2 bytes)    |
+----------------+----------------+
|          Auth Data              |
|       (variable length)         |
+----------------+----------------+
```

## Performance Optimization

### Connection Pooling
```yaml
connection:
  pool:
    enabled: true
    min_size: 5      # Minimum TCP connections
    max_size: 20     # Maximum TCP connections
    
    # Reuse connections efficiently
    max_idle_time_ms: 300000      # 5 minutes
    max_lifetime_ms: 1800000      # 30 minutes
    
    # Health checking
    health_check_interval_ms: 30000
    health_check_timeout_ms: 5000
```

### Buffer Tuning
```yaml
# Client-side tuning
local:
  tcp_nodelay: true           # Disable Nagle's algorithm
  read_buffer_size: 131072    # 128KB for high throughput
  write_buffer_size: 131072   # 128KB
  
# OS-level tuning (Linux)
# /etc/sysctl.conf
net.core.rmem_max = 134217728    # 128MB
net.core.wmem_max = 134217728    # 128MB
net.ipv4.tcp_rmem = 4096 87380 134217728
net.ipv4.tcp_wmem = 4096 65536 134217728
```

### Compression Settings
```yaml
compression:
  enabled: true
  algorithm: zstd
  level: 3  # Good balance of speed vs ratio
  
  # Compression thresholds
  min_size: 1024  # Only compress > 1KB
  
  # Per-message type settings
  types:
    request: true
    response: true
    error: false  # Errors are usually small
```

## Monitoring

### Metrics
The binary protocol exposes additional metrics:

```prometheus
# Connection metrics
mcp_tcp_connections_active{state="active|idle|closing"}
mcp_tcp_bytes_sent_total{connection_id="..."}
mcp_tcp_bytes_received_total{connection_id="..."}

# Frame metrics
mcp_tcp_frames_sent_total{type="request|response|ping|pong"}
mcp_tcp_frames_received_total{type="request|response|ping|pong"}
mcp_tcp_frame_size_bytes{type="...", quantile="0.5|0.9|0.99"}

# Compression metrics
mcp_tcp_compression_ratio{algorithm="zstd|gzip"}
mcp_tcp_compression_time_seconds{operation="compress|decompress"}
```

### Health Checks
```bash
# TCP health check endpoint
nc -zv gateway.example.com 9002

# Detailed health check
echo -ne "\x4D\x43\x50\x42\x01\x04\x00\x00\x00\x00\x00\x00" | \
  nc gateway.example.com 8444 | \
  hexdump -C
```

## Deployment Considerations

### Kubernetes Service
```yaml
apiVersion: v1
kind: Service
metadata:
  name: mcp-gateway-tcp
  namespace: mcp-system
spec:
  type: LoadBalancer
  selector:
    app: mcp-gateway
  ports:
  - name: tcp
    port: 8444
    protocol: TCP
    targetPort: 8444
  - name: tcp-health
    port: 9002
    protocol: TCP
    targetPort: 9002
  
  # Session affinity for connection pooling
  sessionAffinity: ClientIP
  sessionAffinityConfig:
    clientIP:
      timeoutSeconds: 10800  # 3 hours
```

### Load Balancer Configuration

#### AWS NLB
```yaml
metadata:
  annotations:
    service.beta.kubernetes.io/aws-load-balancer-type: "nlb"
    service.beta.kubernetes.io/aws-load-balancer-cross-zone-load-balancing-enabled: "true"
    service.beta.kubernetes.io/aws-load-balancer-backend-protocol: "tcp"
```

#### GCP
```yaml
metadata:
  annotations:
    cloud.google.com/l4-rbs: "enabled"
    networking.gke.io/load-balancer-type: "Internal"
```

### Firewall Rules
```bash
# Allow TCP traffic
gcloud compute firewall-rules create mcp-tcp \
  --allow tcp:8444,tcp:9002 \
  --source-ranges 10.0.0.0/8 \
  --target-tags mcp-gateway
```

## Troubleshooting

### Connection Issues

#### "Connection refused"
```bash
# Check if TCP port is listening
netstat -tlnp | grep 8444

# Test connectivity
telnet gateway.example.com 8444

# Check firewall rules
iptables -L -n | grep 8444
```

#### "Invalid magic bytes"
```bash
# Verify protocol configuration
curl -v http://gateway.example.com:8444
# Should fail with protocol error

# Test with correct protocol
echo -ne "\x4D\x43\x50\x42\x01\x04\x00\x00\x00\x00\x00\x00" | nc gateway.example.com 8444
```

### Performance Issues

#### High Latency
```bash
# Check TCP tuning
sysctl net.ipv4.tcp_nodelay
sysctl net.core.rmem_max

# Monitor retransmissions
netstat -s | grep -i retrans

# Check network path
mtr --tcp --port 8444 gateway.example.com
```

#### Low Throughput
```yaml
# Increase buffer sizes
local:
  read_buffer_size: 262144   # 256KB
  write_buffer_size: 262144  # 256KB

# Enable compression
compression:
  enabled: true
  algorithm: zstd
  level: 1  # Fastest compression
```

## Migration Guide

### From WebSocket to TCP

1. **Update client configuration:**
```yaml
# Change URL protocol
gateway:
  url: tcp://gateway.example.com:8444  # was: wss://

# Enable TCP protocol
advanced:
  protocol: tcp  # was: websocket
```

2. **Test connection:**
```bash
mcp-router test --protocol tcp
```

3. **Monitor during migration:**
```bash
# Compare performance metrics
curl http://localhost:9091/metrics | grep -E "mcp_router_(request_duration|response_size)"
```

### Gradual Migration
```yaml
# Gateway: Enable both protocols
server:
  protocol: both

# Clients: Migrate gradually
# Phase 1: 10% TCP
# Phase 2: 50% TCP  
# Phase 3: 100% TCP
```

## Best Practices

### 1. Use Connection Pooling
```yaml
pool:
  enabled: true
  min_size: 5
  max_size: 20
```

### 2. Enable Compression for Large Payloads
```yaml
compression:
  enabled: true
  min_size: 4096  # Only compress > 4KB
```

### 3. Monitor Protocol-Specific Metrics
```yaml
# Alerting rule
- alert: HighTCPRetransmissions
  expr: rate(mcp_tcp_retransmissions_total[5m]) > 10
  annotations:
    summary: "High TCP retransmission rate"
```

### 4. Implement Graceful Shutdown
```go
// Client-side graceful shutdown
func (c *TCPClient) Shutdown(ctx context.Context) error {
    // Send CLOSE message
    c.SendClose()
    
    // Wait for pending requests
    c.waitGroup.Wait()
    
    // Close connections
    return c.pool.Close()
}
```

## Performance Benchmarks

### Throughput Comparison
| Protocol | Messages/sec | Latency P50 | Latency P99 | CPU Usage |
|----------|-------------|-------------|-------------|-----------|
| WebSocket JSON | 10,000 | 5ms | 25ms | 40% |
| TCP Binary | 100,000 | 0.5ms | 2ms | 25% |
| TCP + Compression | 85,000 | 0.7ms | 3ms | 30% |

### Bandwidth Usage
| Payload Size | WebSocket | TCP Binary | TCP + zstd | Savings |
|--------------|-----------|------------|------------|---------|
| 1 KB | 1.5 KB | 1.1 KB | 0.8 KB | 47% |
| 10 KB | 13.2 KB | 10.1 KB | 4.2 KB | 68% |
| 100 KB | 128 KB | 100.1 KB | 28 KB | 78% |