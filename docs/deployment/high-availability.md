# High Availability Deployment Guide

This guide covers deploying MCP Bridge in a highly available configuration suitable for production workloads with high uptime requirements.

## Overview

A highly available MCP Bridge deployment provides:
- **Zero-downtime deployments** with rolling updates
- **Automatic failover** across multiple availability zones
- **Horizontal scaling** based on demand
- **Data persistence** and backup strategies
- **Disaster recovery** capabilities

## Architecture Components

### Gateway High Availability

#### Multi-Zone Deployment
```yaml
# Gateway deployment with zone anti-affinity
apiVersion: apps/v1
kind: Deployment
metadata:
  name: mcp-gateway
spec:
  replicas: 6  # 2 per zone minimum
  template:
    spec:
      affinity:
        podAntiAffinity:
          preferredDuringSchedulingIgnoredDuringExecution:
          - weight: 100
            podAffinityTerm:
              labelSelector:
                matchExpressions:
                - key: app
                  operator: In
                  values: ["mcp-gateway"]
              topologyKey: topology.kubernetes.io/zone
```

#### Pod Disruption Budget
```yaml
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: mcp-gateway-pdb
spec:
  minAvailable: 2
  selector:
    matchLabels:
      app: mcp-gateway
```

#### Horizontal Pod Autoscaler
```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: mcp-gateway-hpa
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: mcp-gateway
  minReplicas: 6
  maxReplicas: 50
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
  behavior:
    scaleUp:
      stabilizationWindowSeconds: 60
      policies:
      - type: Percent
        value: 100
        periodSeconds: 15
    scaleDown:
      stabilizationWindowSeconds: 300
      policies:
      - type: Percent
        value: 10
        periodSeconds: 60
```

### Redis High Availability

#### Redis Sentinel Setup
```yaml
# Redis master-slave with Sentinel
apiVersion: v1
kind: ConfigMap
metadata:
  name: redis-sentinel-config
data:
  sentinel.conf: |
    port 26379
    sentinel monitor mymaster redis-master 6379 2
    sentinel down-after-milliseconds mymaster 5000
    sentinel failover-timeout mymaster 10000
    sentinel parallel-syncs mymaster 1
```

#### Redis Cluster Alternative
For higher throughput, use Redis Cluster:
```yaml
# Redis cluster configuration
redis:
  cluster:
    enabled: true
    nodes: 6  # 3 masters, 3 slaves
    replicas: 1
```

### External Redis (Recommended)

Use managed Redis services for production:

#### AWS ElastiCache
```yaml
# Gateway configuration for ElastiCache
gateway:
  redis:
    url: "rediss://master.mcp-redis.xxxxx.cache.amazonaws.com:6380"
    tls:
      enabled: true
      skip_verify: false
```

#### Google Cloud Memorystore
```yaml
gateway:
  redis:
    url: "redis://10.x.x.x:6379"
    auth:
      password_env: REDIS_AUTH_TOKEN
```

## Load Balancing

### External Load Balancer
```yaml
apiVersion: v1
kind: Service
metadata:
  name: mcp-gateway-external
  annotations:
    service.beta.kubernetes.io/aws-load-balancer-type: "nlb"
    service.beta.kubernetes.io/aws-load-balancer-cross-zone-load-balancing-enabled: "true"
spec:
  type: LoadBalancer
  ports:
  - port: 443
    targetPort: 8443
    protocol: TCP
  - port: 9443  # TCP/Binary protocol
    targetPort: 9443
    protocol: TCP
  selector:
    app: mcp-gateway
```

### Ingress Controller
```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: mcp-gateway-ingress
  annotations:
    nginx.ingress.kubernetes.io/websocket-services: "mcp-gateway"
    nginx.ingress.kubernetes.io/proxy-read-timeout: "3600"
    nginx.ingress.kubernetes.io/proxy-send-timeout: "3600"
spec:
  tls:
  - hosts:
    - mcp.example.com
    secretName: mcp-tls-cert
  rules:
  - host: mcp.example.com
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: mcp-gateway
            port:
              number: 8443
```

## Monitoring and Alerting

### Prometheus Rules
```yaml
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: mcp-gateway-alerts
spec:
  groups:
  - name: mcp-gateway
    rules:
    - alert: MCPGatewayDown
      expr: up{job="mcp-gateway"} == 0
      for: 5m
      labels:
        severity: critical
      annotations:
        summary: "MCP Gateway is down"
    
    - alert: MCPGatewayHighLatency
      expr: histogram_quantile(0.95, mcp_gateway_request_duration_seconds_bucket) > 1
      for: 10m
      labels:
        severity: warning
      annotations:
        summary: "MCP Gateway high latency"
    
    - alert: MCPGatewayCircuitBreakerOpen
      expr: mcp_gateway_circuit_breaker_state == 1
      for: 1m
      labels:
        severity: warning
      annotations:
        summary: "MCP Gateway circuit breaker is open"
```

### Grafana Dashboard
Key metrics to monitor:
- Connection count and success rate
- Request latency percentiles (p50, p95, p99)
- Error rate by endpoint
- Circuit breaker states
- Redis connectivity and performance
- Pod CPU/Memory utilization
- Network throughput

## Backup and Disaster Recovery

### Configuration Backup
```bash
# Backup Kubernetes manifests
kubectl get all -n mcp-system -o yaml > mcp-backup-$(date +%Y%m%d).yaml

# Backup ConfigMaps and Secrets
kubectl get configmap,secret -n mcp-system -o yaml >> mcp-backup-$(date +%Y%m%d).yaml
```

### Redis Data Backup
```bash
# For managed Redis, use provider tools
# AWS ElastiCache automatic backups
aws elasticache create-snapshot --cache-cluster-id mcp-redis --snapshot-name mcp-backup-$(date +%Y%m%d)

# For self-hosted Redis
kubectl exec redis-master -- redis-cli BGSAVE
kubectl cp redis-master:/data/dump.rdb ./redis-backup-$(date +%Y%m%d).rdb
```

### Cross-Region Replication
```yaml
# Multi-region deployment example
# Primary region
apiVersion: v1
kind: Service
metadata:
  name: mcp-gateway-primary
  annotations:
    external-dns.alpha.kubernetes.io/hostname: mcp-primary.example.com

# Secondary region (passive)
apiVersion: v1
kind: Service
metadata:
  name: mcp-gateway-secondary
  annotations:
    external-dns.alpha.kubernetes.io/hostname: mcp-secondary.example.com
```

## Security Hardening

### Network Policies
```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: mcp-gateway-network-policy
spec:
  podSelector:
    matchLabels:
      app: mcp-gateway
  policyTypes:
  - Ingress
  - Egress
  ingress:
  - from:
    - namespaceSelector:
        matchLabels:
          name: istio-system
    ports:
    - protocol: TCP
      port: 8443
  egress:
  - to:
    - podSelector:
        matchLabels:
          app: redis
    ports:
    - protocol: TCP
      port: 6379
```

### Pod Security Standards
```yaml
apiVersion: v1
kind: Pod
metadata:
  name: mcp-gateway
spec:
  securityContext:
    runAsNonRoot: true
    runAsUser: 1000
    fsGroup: 2000
    seccompProfile:
      type: RuntimeDefault
  containers:
  - name: gateway
    securityContext:
      allowPrivilegeEscalation: false
      readOnlyRootFilesystem: true
      capabilities:
        drop:
        - ALL
```

## Performance Tuning

### Resource Allocation
```yaml
resources:
  requests:
    memory: "1Gi"
    cpu: "1000m"
  limits:
    memory: "2Gi"
    cpu: "2000m"
```

### JVM Tuning (if applicable)
```yaml
env:
- name: JAVA_OPTS
  value: "-Xms1g -Xmx1g -XX:+UseG1GC -XX:MaxGCPauseMillis=200"
```

### Connection Tuning
```yaml
gateway:
  server:
    max_connections: 100000
    max_connections_per_ip: 1000
    read_timeout: 30s
    write_timeout: 30s
  connection_pool:
    max_size: 50
    max_idle_time: 300s
```

## Validation and Testing

### Health Check Configuration
```yaml
livenessProbe:
  httpGet:
    path: /health
    port: 8443
    scheme: HTTPS
  initialDelaySeconds: 30
  periodSeconds: 10
  timeoutSeconds: 5
  failureThreshold: 3

readinessProbe:
  httpGet:
    path: /ready
    port: 8443
    scheme: HTTPS
  initialDelaySeconds: 5
  periodSeconds: 5
  timeoutSeconds: 3
  failureThreshold: 3
```

### Load Testing
```bash
# K6 load test for HA validation
k6 run --vus 1000 --duration 30m load-test.js

# Chaos engineering with chaos-monkey
kubectl apply -f chaos-monkey-deployment.yaml
```

## Maintenance Procedures

### Rolling Updates
```bash
# Zero-downtime deployment
kubectl set image deployment/mcp-gateway gateway=mcp-gateway:v2.0.0
kubectl rollout status deployment/mcp-gateway

# Rollback if needed
kubectl rollout undo deployment/mcp-gateway
```

### Scaling Operations
```bash
# Manual scaling
kubectl scale deployment mcp-gateway --replicas=10

# Update HPA limits
kubectl patch hpa mcp-gateway-hpa -p '{"spec":{"maxReplicas":20}}'
```

### Certificate Rotation
```bash
# Using cert-manager for automatic rotation
kubectl apply -f certificate-issuer.yaml
kubectl apply -f certificate.yaml

# Manual certificate update
kubectl create secret tls mcp-tls-cert --cert=cert.pem --key=key.pem --dry-run=client -o yaml | kubectl apply -f -
```

This high availability setup ensures your MCP Bridge deployment can handle production workloads with minimal downtime and automatic recovery from failures.