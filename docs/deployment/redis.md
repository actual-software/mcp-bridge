# Redis Production Deployment Guide

Redis is a critical component for the MCP Gateway, providing session storage, rate limiting, and distributed state management. This guide covers production-grade Redis deployment.

## Overview

Redis is used for:
- **Session Storage** - Distributed session state for horizontal scaling
- **Rate Limiting** - Sliding window counters with atomic operations
- **Circuit Breaker State** - Shared failure counters across gateway instances
- **Cache** - Response caching and deduplication

## Deployment Options

### 1. Redis Sentinel (Recommended for HA)

Redis Sentinel provides automatic failover and high availability.

#### Deployment
```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: redis-sentinel-config
  namespace: mcp-system
data:
  master.conf: |
    bind 0.0.0.0
    protected-mode yes
    requirepass REDIS_MASTER_PASSWORD
    masterauth REDIS_MASTER_PASSWORD
    maxmemory 2gb
    maxmemory-policy allkeys-lru
    save 900 1
    save 300 10
    save 60 10000
    
  sentinel.conf: |
    bind 0.0.0.0
    sentinel auth-pass mymaster REDIS_MASTER_PASSWORD
    sentinel down-after-milliseconds mymaster 5000
    sentinel parallel-syncs mymaster 1
    sentinel failover-timeout mymaster 10000
    sentinel deny-scripts-reconfig yes
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: redis-sentinel
  namespace: mcp-system
spec:
  serviceName: redis-sentinel
  replicas: 3
  selector:
    matchLabels:
      app: redis-sentinel
  template:
    metadata:
      labels:
        app: redis-sentinel
    spec:
      initContainers:
      - name: config
        image: redis:7-alpine
        command: 
        - sh
        - -c
        - |
          cp /tmp/redis/master.conf /etc/redis/redis.conf
          echo "replica-announce-ip $(POD_IP)" >> /etc/redis/redis.conf
        env:
        - name: POD_IP
          valueFrom:
            fieldRef:
              fieldPath: status.podIP
        volumeMounts:
        - name: config
          mountPath: /tmp/redis
        - name: redis-config
          mountPath: /etc/redis
      containers:
      - name: redis
        image: redis:7-alpine
        command: ["redis-server"]
        args: ["/etc/redis/redis.conf"]
        ports:
        - containerPort: 6379
        resources:
          requests:
            memory: "256Mi"
            cpu: "100m"
          limits:
            memory: "2Gi"
            cpu: "1000m"
        volumeMounts:
        - name: data
          mountPath: /data
        - name: redis-config
          mountPath: /etc/redis
        livenessProbe:
          exec:
            command:
            - redis-cli
            - -a
            - $(REDIS_PASSWORD)
            - ping
          initialDelaySeconds: 30
          timeoutSeconds: 5
        readinessProbe:
          exec:
            command:
            - redis-cli
            - -a
            - $(REDIS_PASSWORD)
            - ping
          initialDelaySeconds: 5
          timeoutSeconds: 1
        env:
        - name: REDIS_PASSWORD
          valueFrom:
            secretKeyRef:
              name: redis-secret
              key: password
      - name: sentinel
        image: redis:7-alpine
        command: ["redis-sentinel"]
        args: ["/etc/redis-sentinel/sentinel.conf"]
        ports:
        - containerPort: 26379
        volumeMounts:
        - name: sentinel-config
          mountPath: /etc/redis-sentinel
      volumes:
      - name: config
        configMap:
          name: redis-sentinel-config
      - name: redis-config
        emptyDir: {}
      - name: sentinel-config
        configMap:
          name: redis-sentinel-config
          items:
          - key: sentinel.conf
            path: sentinel.conf
  volumeClaimTemplates:
  - metadata:
      name: data
    spec:
      accessModes: ["ReadWriteOnce"]
      storageClassName: fast-ssd
      resources:
        requests:
          storage: 10Gi
```

#### Gateway Configuration
```yaml
sessions:
  provider: redis
  redis:
    addresses:
      - redis-sentinel-0.redis-sentinel:26379
      - redis-sentinel-1.redis-sentinel:26379
      - redis-sentinel-2.redis-sentinel:26379
    password: ${REDIS_PASSWORD}
    sentinel_master_name: mymaster
    pool_size: 100
    max_retries: 3
    dial_timeout: 5000
```

### 2. Redis Cluster (For Large Scale)

Redis Cluster provides automatic sharding and linear scalability.

#### Deployment
```bash
# Use Redis Operator for easier management
kubectl apply -f https://raw.githubusercontent.com/spotahome/redis-operator/master/manifests/databases.spotahome.com_redisfailovers.yaml

# Deploy Redis Cluster
kubectl apply -f - <<EOF
apiVersion: databases.spotahome.com/v1
kind: RedisFailover
metadata:
  name: mcp-redis-cluster
  namespace: mcp-system
spec:
  sentinel:
    replicas: 3
    resources:
      requests:
        memory: 100Mi
        cpu: 100m
      limits:
        memory: 200Mi
        cpu: 200m
  redis:
    replicas: 6  # 3 masters, 3 replicas
    resources:
      requests:
        memory: 500Mi
        cpu: 250m
      limits:
        memory: 2Gi
        cpu: 1000m
    storage:
      persistentVolumeClaim:
        metadata:
          name: redis-data
        spec:
          accessModes:
            - ReadWriteOnce
          resources:
            requests:
              storage: 10Gi
          storageClassName: fast-ssd
EOF
```

### 3. Managed Redis (Cloud Providers)

For production, consider managed services:

#### AWS ElastiCache
```yaml
# Gateway configuration for ElastiCache
sessions:
  provider: redis
  redis:
    addresses:
      - redis-cluster.abc123.cache.amazonaws.com:6379
    password: ${REDIS_AUTH_TOKEN}
    tls:
      enabled: true
      insecure_skip_verify: false
```

#### Google Cloud Memorystore
```yaml
sessions:
  provider: redis
  redis:
    url: redis://10.0.0.3:6379
    password: ${REDIS_PASSWORD}
    pool_size: 100
```

#### Azure Cache for Redis
```yaml
sessions:
  provider: redis
  redis:
    url: rediss://cache-name.redis.cache.windows.net:6380
    password: ${REDIS_ACCESS_KEY}
    tls:
      enabled: true
```

## Configuration Best Practices

### 1. Connection Pooling
```yaml
redis:
  pool_size: 100        # Adjust based on gateway replicas
  max_retries: 3
  dial_timeout: 5000    # 5 seconds
  read_timeout: 3000    # 3 seconds
  write_timeout: 3000   # 3 seconds
  pool_timeout: 4000    # 4 seconds
  idle_timeout: 240000  # 4 minutes
  max_conn_age: 0       # No maximum age
```

### 2. Memory Management
```yaml
# Redis configuration
maxmemory 4gb
maxmemory-policy allkeys-lru  # LRU eviction for all keys

# Alternative policies:
# - volatile-lru: LRU for keys with TTL
# - allkeys-lfu: LFU eviction (Redis 4.0+)
# - volatile-ttl: Evict keys with shortest TTL
```

### 3. Persistence Configuration
```yaml
# For session data (can be reconstructed)
save ""  # Disable RDB snapshots
appendonly no  # Disable AOF

# For critical data
save 900 1      # Snapshot after 900 sec if 1 key changed
save 300 10     # Snapshot after 300 sec if 10 keys changed
save 60 10000   # Snapshot after 60 sec if 10000 keys changed
appendonly yes
appendfsync everysec
```

### 4. Security Hardening
```yaml
# Redis configuration
requirepass very_long_random_password_here
rename-command FLUSHDB ""
rename-command FLUSHALL ""
rename-command KEYS ""
rename-command CONFIG "CONFIG_e8f9c6d5"

# Network isolation
bind 10.0.0.0/8  # Only cluster network
protected-mode yes

# TLS Configuration (Redis 6+)
tls-port 6380
port 0  # Disable non-TLS port
tls-cert-file /tls/redis.crt
tls-key-file /tls/redis.key
tls-ca-cert-file /tls/ca.crt
tls-dh-params-file /tls/redis.dh
```

## Monitoring and Alerting

### 1. Key Metrics
```yaml
# Prometheus ServiceMonitor
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: redis-monitoring
  namespace: mcp-system
spec:
  selector:
    matchLabels:
      app: redis
  endpoints:
  - port: metrics
    interval: 30s
```

### 2. Critical Alerts
```yaml
groups:
- name: redis.rules
  rules:
  - alert: RedisDown
    expr: redis_up == 0
    for: 5m
    annotations:
      summary: "Redis instance {{ $labels.instance }} is down"
      
  - alert: RedisMemoryHigh
    expr: redis_memory_used_bytes / redis_memory_max_bytes > 0.9
    for: 10m
    annotations:
      summary: "Redis memory usage is above 90%"
      
  - alert: RedisConnectionsHigh
    expr: redis_connected_clients > 1000
    for: 5m
    annotations:
      summary: "Redis has high number of connections"
      
  - alert: RedisMasterLinkDown
    expr: redis_master_link_up == 0
    for: 2m
    annotations:
      summary: "Redis replica lost connection to master"
```

### 3. Grafana Dashboard
Import dashboard ID: 11835 (Redis Dashboard for Prometheus)

Key panels to monitor:
- Commands/sec by command type
- Memory usage and eviction rate
- Client connections
- Replication lag
- Cache hit rate

## Performance Tuning

### 1. Kernel Parameters
```bash
# /etc/sysctl.conf
vm.overcommit_memory = 1
net.core.somaxconn = 65535
net.ipv4.tcp_max_syn_backlog = 65535

# Apply
sysctl -p
```

### 2. Redis Configuration
```conf
# High performance settings
tcp-backlog 511
timeout 0
tcp-keepalive 300
databases 16

# Disable expensive operations
slowlog-log-slower-than 10000
slowlog-max-len 128

# Optimize for latency
latency-monitor-threshold 100
```

### 3. Data Structure Optimization
```yaml
# Use Redis data types efficiently
rate_limiting:
  # Use Redis Sorted Sets for sliding windows
  window_type: sorted_set
  
sessions:
  # Use Redis Hashes for session data
  encoding: hash
  compression: true
```

## Backup and Recovery

### 1. Automated Backups
```yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: redis-backup
  namespace: mcp-system
spec:
  schedule: "0 2 * * *"  # Daily at 2 AM
  jobTemplate:
    spec:
      template:
        spec:
          containers:
          - name: backup
            image: redis:7-alpine
            command:
            - sh
            - -c
            - |
              redis-cli -h redis-master -a $REDIS_PASSWORD --rdb /backup/dump.rdb
              aws s3 cp /backup/dump.rdb s3://mcp-backups/redis/$(date +%Y%m%d)/dump.rdb
            env:
            - name: REDIS_PASSWORD
              valueFrom:
                secretKeyRef:
                  name: redis-secret
                  key: password
            volumeMounts:
            - name: backup
              mountPath: /backup
          volumes:
          - name: backup
            emptyDir: {}
          restartPolicy: OnFailure
```

### 2. Point-in-Time Recovery
```bash
# Enable AOF for PITR
appendonly yes
appendfilename "appendonly.aof"
appendfsync everysec
auto-aof-rewrite-percentage 100
auto-aof-rewrite-min-size 64mb
```

## Troubleshooting

### Common Issues

#### 1. "MISCONF Redis is configured to save RDB snapshots"
```bash
# Fix: Adjust save configuration or increase disk space
redis-cli CONFIG SET save ""
```

#### 2. "OOM command not allowed when used memory > 'maxmemory'"
```bash
# Fix: Increase maxmemory or adjust eviction policy
redis-cli CONFIG SET maxmemory 8gb
redis-cli CONFIG SET maxmemory-policy allkeys-lru
```

#### 3. High Latency
```bash
# Diagnose
redis-cli --latency
redis-cli --latency-history
redis-cli --latency-dist

# Common fixes:
# - Disable transparent huge pages
# - Adjust persistence settings
# - Use dedicated network
```

### Health Checks
```bash
# Basic health
redis-cli ping

# Detailed health
redis-cli INFO server
redis-cli INFO replication
redis-cli INFO memory
redis-cli INFO stats
```

## Migration Guide

### From In-Memory to Redis
```yaml
# Step 1: Deploy Redis
kubectl apply -f redis-deployment.yaml

# Step 2: Update gateway configuration
sessions:
  provider: redis  # Changed from "memory"
  redis:
    url: redis://redis:6379

# Step 3: Rolling restart gateways
kubectl rollout restart deployment/mcp-gateway -n mcp-system
```

### Redis Version Upgrade
```bash
# Step 1: Backup data
redis-cli --rdb backup.rdb

# Step 2: Deploy new version as replica
# Step 3: Promote replica to master
# Step 4: Update client connections
# Step 5: Decommission old instance
```