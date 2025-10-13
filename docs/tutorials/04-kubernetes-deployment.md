# Tutorial: Kubernetes Deployment

Deploy MCP Bridge Gateway to Kubernetes with high availability, monitoring, and production-ready configuration.

## Prerequisites

- Kubernetes cluster (1.24+) with kubectl configured
- Helm 3.10+ (optional but recommended)
- Basic Kubernetes knowledge
- 30-40 minutes

## What You'll Build

A production-ready MCP Bridge deployment with:
- High availability (3 replicas with pod anti-affinity)
- Kubernetes service discovery
- TLS encryption
- JWT authentication
- Redis for session storage
- Prometheus monitoring
- Horizontal pod autoscaling
- Network policies for security

## Architecture

```
┌─────────────────────────────────────────────────┐
│  Kubernetes Cluster                              │
│                                                  │
│  ┌──────────────────────────────────────┐      │
│  │  Namespace: mcp-system               │      │
│  │                                       │      │
│  │  ┌──────────────────────────────┐   │      │
│  │  │  LoadBalancer Service        │   │      │
│  │  │  (External: 443→8443)        │   │      │
│  │  └────────────┬─────────────────┘   │      │
│  │               │                      │      │
│  │  ┌────────────▼─────────────────┐   │      │
│  │  │  mcp-gateway Deployment      │   │      │
│  │  │  - 3 replicas                │   │      │
│  │  │  - Pod anti-affinity         │   │      │
│  │  │  - Rolling updates           │   │      │
│  │  └────────────┬─────────────────┘   │      │
│  │               │                      │      │
│  │  ┌────────────▼─────────────────┐   │      │
│  │  │  Redis (Session Storage)     │   │      │
│  │  └──────────────────────────────┘   │      │
│  │                                       │      │
│  │  ┌──────────────────────────────┐   │      │
│  │  │  Prometheus (Monitoring)     │   │      │
│  │  └──────────────────────────────┘   │      │
│  └───────────────────────────────────────┘      │
│                                                  │
│  ┌──────────────────────────────────────┐      │
│  │  Namespace: mcp-servers              │      │
│  │                                       │      │
│  │  ┌──────────────────────────────┐   │      │
│  │  │  MCP Server Pods             │   │      │
│  │  │  (Auto-discovered)           │   │      │
│  │  └──────────────────────────────┘   │      │
│  └───────────────────────────────────────┘      │
└──────────────────────────────────────────────────┘
```

## Step 1: Create Namespaces

```bash
# Create system namespace for gateway
kubectl create namespace mcp-system

# Create namespace for MCP servers
kubectl create namespace mcp-servers

# Verify
kubectl get namespaces | grep mcp
```

## Step 2: Create Secrets

Generate JWT secret and create Kubernetes secrets:

```bash
# Generate JWT secret
JWT_SECRET=$(openssl rand -base64 32)

# Create TLS certificates (or use cert-manager)
openssl req -x509 -newkey rsa:4096 -keyout tls.key -out tls.crt \
  -days 365 -nodes -subj "/CN=mcp-gateway.example.com"

# Create Kubernetes secret
kubectl create secret generic mcp-gateway-secrets \
  --namespace=mcp-system \
  --from-literal=jwt-secret-key="$JWT_SECRET" \
  --from-file=tls.crt=tls.crt \
  --from-file=tls.key=tls.key \
  --from-literal=redis-url="redis://mcp-redis:6379"

# Verify
kubectl get secrets -n mcp-system
```

## Step 3: Deploy Redis

Create `redis.yaml`:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: mcp-redis
  namespace: mcp-system
spec:
  replicas: 1
  selector:
    matchLabels:
      app: mcp-redis
  template:
    metadata:
      labels:
        app: mcp-redis
    spec:
      containers:
      - name: redis
        image: redis:7-alpine
        ports:
        - containerPort: 6379
        resources:
          requests:
            memory: "256Mi"
            cpu: "250m"
          limits:
            memory: "512Mi"
            cpu: "500m"
        volumeMounts:
        - name: redis-data
          mountPath: /data
      volumes:
      - name: redis-data
        emptyDir: {}
---
apiVersion: v1
kind: Service
metadata:
  name: mcp-redis
  namespace: mcp-system
spec:
  selector:
    app: mcp-redis
  ports:
  - port: 6379
    targetPort: 6379
```

```bash
kubectl apply -f redis.yaml
kubectl wait --for=condition=ready pod -l app=mcp-redis -n mcp-system --timeout=60s
```

## Step 4: Configure Gateway

Create `gateway-config.yaml`:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: mcp-gateway-config
  namespace: mcp-system
data:
  gateway.yaml: |
    version: 1

    server:
      host: 0.0.0.0
      port: 8443
      protocol: websocket
      tls:
        enabled: true
        cert_file: /etc/tls/tls.crt
        key_file: /etc/tls/tls.key

    auth:
      type: jwt
      jwt:
        issuer: mcp-gateway
        audience: mcp-services
        secret_key_env: JWT_SECRET_KEY
      per_message_auth: false

    discovery:
      provider: kubernetes
      kubernetes:
        in_cluster: true
        namespace_selector:
          - mcp-servers
        label_selector:
          mcp.bridge/enabled: "true"
        update_interval: 30s

    routing:
      strategy: least_connections
      health_check_interval: 30s
      circuit_breaker:
        failure_threshold: 5
        success_threshold: 2
        timeout: 30s

    sessions:
      type: redis
      ttl: 1h
      cleanup_interval: 5m
      redis:
        url_env: REDIS_URL

    rate_limit:
      enabled: true
      requests_per_sec: 1000
      burst: 200
      per_ip: true

    observability:
      metrics:
        enabled: true
        port: 9090
        path: /metrics

      logging:
        level: info
        format: json
        output: stdout
```

```bash
kubectl apply -f gateway-config.yaml
```

## Step 5: Create RBAC

Create `rbac.yaml`:

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: mcp-gateway
  namespace: mcp-system
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: mcp-gateway
rules:
- apiGroups: [""]
  resources: ["pods", "services", "endpoints"]
  verbs: ["get", "list", "watch"]
- apiGroups: [""]
  resources: ["namespaces"]
  verbs: ["get", "list"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: mcp-gateway
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: mcp-gateway
subjects:
- kind: ServiceAccount
  name: mcp-gateway
  namespace: mcp-system
```

```bash
kubectl apply -f rbac.yaml
```

## Step 6: Deploy Gateway

Create `gateway-deployment.yaml`:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: mcp-gateway
  namespace: mcp-system
  labels:
    app: mcp-gateway
spec:
  replicas: 3
  strategy:
    type: RollingUpdate
    rollingUpdate:
      maxUnavailable: 0
      maxSurge: 1
  selector:
    matchLabels:
      app: mcp-gateway
  template:
    metadata:
      labels:
        app: mcp-gateway
      annotations:
        prometheus.io/scrape: "true"
        prometheus.io/port: "9090"
        prometheus.io/path: "/metrics"
    spec:
      serviceAccountName: mcp-gateway
      # Ensure pods run on different nodes
      affinity:
        podAntiAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
          - labelSelector:
              matchExpressions:
              - key: app
                operator: In
                values:
                - mcp-gateway
            topologyKey: kubernetes.io/hostname
      containers:
      - name: gateway
        image: ghcr.io/actual-software/mcp-bridge/gateway:latest
        imagePullPolicy: IfNotPresent
        ports:
        - containerPort: 8443
          name: websocket
        - containerPort: 9090
          name: metrics
        env:
        - name: CONFIG_PATH
          value: "/etc/mcp-gateway/gateway.yaml"
        - name: JWT_SECRET_KEY
          valueFrom:
            secretKeyRef:
              name: mcp-gateway-secrets
              key: jwt-secret-key
        - name: REDIS_URL
          valueFrom:
            secretKeyRef:
              name: mcp-gateway-secrets
              key: redis-url
        - name: LOG_LEVEL
          value: "info"
        resources:
          requests:
            memory: "256Mi"
            cpu: "500m"
          limits:
            memory: "1Gi"
            cpu: "2000m"
        livenessProbe:
          httpGet:
            path: /healthz
            port: 9090
          initialDelaySeconds: 10
          periodSeconds: 30
          timeoutSeconds: 5
          failureThreshold: 3
        readinessProbe:
          httpGet:
            path: /ready
            port: 9090
          initialDelaySeconds: 5
          periodSeconds: 10
          timeoutSeconds: 5
          failureThreshold: 3
        volumeMounts:
        - name: config
          mountPath: /etc/mcp-gateway
          readOnly: true
        - name: tls-certs
          mountPath: /etc/tls
          readOnly: true
        securityContext:
          allowPrivilegeEscalation: false
          capabilities:
            drop:
            - ALL
          readOnlyRootFilesystem: true
          runAsNonRoot: true
          runAsUser: 1000
      volumes:
      - name: config
        configMap:
          name: mcp-gateway-config
      - name: tls-certs
        secret:
          secretName: mcp-gateway-secrets
          items:
          - key: tls.crt
            path: tls.crt
          - key: tls.key
            path: tls.key
```

```bash
kubectl apply -f gateway-deployment.yaml
kubectl wait --for=condition=ready pod -l app=mcp-gateway -n mcp-system --timeout=120s
```

## Step 7: Create Services

Create `gateway-service.yaml`:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: mcp-gateway
  namespace: mcp-system
  labels:
    app: mcp-gateway
spec:
  type: ClusterIP
  ports:
  - port: 8443
    targetPort: 8443
    name: websocket
  - port: 9090
    targetPort: 9090
    name: metrics
  selector:
    app: mcp-gateway
---
# External LoadBalancer for client access
apiVersion: v1
kind: Service
metadata:
  name: mcp-gateway-external
  namespace: mcp-system
  labels:
    app: mcp-gateway
spec:
  type: LoadBalancer
  ports:
  - port: 443
    targetPort: 8443
    name: websocket
  selector:
    app: mcp-gateway
  sessionAffinity: ClientIP
  sessionAffinityConfig:
    clientIP:
      timeoutSeconds: 3600
```

```bash
kubectl apply -f gateway-service.yaml

# Wait for LoadBalancer IP
kubectl get svc mcp-gateway-external -n mcp-system -w
```

## Step 8: Deploy Sample MCP Server

Create `mcp-server-example.yaml`:

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: mcp-servers
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: example-mcp-server
  namespace: mcp-servers
  labels:
    app: example-mcp-server
    mcp.bridge/enabled: "true"  # Gateway discovers this
spec:
  replicas: 2
  selector:
    matchLabels:
      app: example-mcp-server
  template:
    metadata:
      labels:
        app: example-mcp-server
        mcp.bridge/enabled: "true"
    spec:
      containers:
      - name: server
        image: your-registry/mcp-server:latest
        ports:
        - containerPort: 8080
          name: http
        resources:
          requests:
            memory: "128Mi"
            cpu: "100m"
          limits:
            memory: "256Mi"
            cpu: "500m"
---
apiVersion: v1
kind: Service
metadata:
  name: example-mcp-server
  namespace: mcp-servers
  labels:
    mcp.bridge/enabled: "true"
spec:
  selector:
    app: example-mcp-server
  ports:
  - port: 8080
    targetPort: 8080
    name: http
```

```bash
kubectl apply -f mcp-server-example.yaml
```

## Step 9: Configure Horizontal Pod Autoscaler

Create `hpa.yaml`:

```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: mcp-gateway
  namespace: mcp-system
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: mcp-gateway
  minReplicas: 3
  maxReplicas: 10
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
        value: 50
        periodSeconds: 60
    scaleDown:
      stabilizationWindowSeconds: 300
      policies:
      - type: Percent
        value: 25
        periodSeconds: 60
```

```bash
kubectl apply -f hpa.yaml
```

## Step 10: Set Up Monitoring

Create `prometheus-servicemonitor.yaml`:

```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: mcp-gateway
  namespace: mcp-system
  labels:
    app: mcp-gateway
spec:
  selector:
    matchLabels:
      app: mcp-gateway
  endpoints:
  - port: metrics
    interval: 30s
    path: /metrics
```

```bash
kubectl apply -f prometheus-servicemonitor.yaml
```

## Step 11: Test the Deployment

### Check Pod Status

```bash
# View all pods
kubectl get pods -n mcp-system

# Check gateway logs
kubectl logs -n mcp-system -l app=mcp-gateway --tail=50 -f

# Check Redis logs
kubectl logs -n mcp-system -l app=mcp-redis --tail=20
```

### Test Health Endpoints

```bash
# Port-forward to test locally
kubectl port-forward -n mcp-system svc/mcp-gateway 9090:9090

# Check health
curl http://localhost:9090/healthz
curl http://localhost:9090/ready

# Check metrics
curl http://localhost:9090/metrics | grep mcp_gateway
```

### Test Gateway Connection

```bash
# Get LoadBalancer IP
GATEWAY_IP=$(kubectl get svc mcp-gateway-external -n mcp-system -o jsonpath='{.status.loadBalancer.ingress[0].ip}')

# Test WebSocket connection
npm install -g wscat
wscat -c wss://$GATEWAY_IP:443 --no-check
```

## Step 12: Configure Ingress (Optional)

Create `ingress.yaml`:

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: mcp-gateway
  namespace: mcp-system
  annotations:
    cert-manager.io/cluster-issuer: letsencrypt-prod
    nginx.ingress.kubernetes.io/websocket-services: mcp-gateway
    nginx.ingress.kubernetes.io/ssl-redirect: "true"
spec:
  ingressClassName: nginx
  tls:
  - hosts:
    - gateway.yourdomain.com
    secretName: mcp-gateway-tls
  rules:
  - host: gateway.yourdomain.com
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

```bash
kubectl apply -f ingress.yaml
```

## Production Best Practices

### 1. Security

#### Network Policies

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: mcp-gateway
  namespace: mcp-system
spec:
  podSelector:
    matchLabels:
      app: mcp-gateway
  policyTypes:
  - Ingress
  - Egress
  ingress:
  - from:
    - namespaceSelector: {}
    ports:
    - protocol: TCP
      port: 8443
    - protocol: TCP
      port: 9090
  egress:
  - to:
    - namespaceSelector:
        matchLabels:
          name: mcp-servers
    ports:
    - protocol: TCP
      port: 8080
  - to:
    - podSelector:
        matchLabels:
          app: mcp-redis
    ports:
    - protocol: TCP
      port: 6379
```

#### Pod Security Policy

```yaml
apiVersion: policy/v1beta1
kind: PodSecurityPolicy
metadata:
  name: mcp-gateway
spec:
  privileged: false
  allowPrivilegeEscalation: false
  requiredDropCapabilities:
  - ALL
  volumes:
  - configMap
  - secret
  - emptyDir
  runAsUser:
    rule: MustRunAsNonRoot
  seLinux:
    rule: RunAsAny
  fsGroup:
    rule: RunAsAny
```

### 2. Resource Management

Set appropriate resource requests and limits:

```yaml
resources:
  requests:
    memory: "256Mi"
    cpu: "500m"
  limits:
    memory: "1Gi"
    cpu: "2000m"
```

### 3. High Availability

- **3+ replicas** with pod anti-affinity
- **Rolling updates** with maxUnavailable: 0
- **Redis replication** for session storage
- **Multi-zone** deployment

### 4. Monitoring

Key metrics to monitor:

```promql
# Connection count
mcp_gateway_active_connections

# Request rate
rate(mcp_gateway_requests_total[5m])

# Error rate
rate(mcp_gateway_errors_total[5m])

# Response time
histogram_quantile(0.95, rate(mcp_gateway_request_duration_seconds_bucket[5m]))
```

## Troubleshooting

### Pods Not Starting

```bash
# Check events
kubectl describe pod -n mcp-system <pod-name>

# Check logs
kubectl logs -n mcp-system <pod-name>

# Check resource constraints
kubectl top pods -n mcp-system
```

### Service Discovery Not Working

```bash
# Check RBAC permissions
kubectl auth can-i get pods --as=system:serviceaccount:mcp-system:mcp-gateway

# Check discovered services
kubectl logs -n mcp-system -l app=mcp-gateway | grep discovery

# Verify labels
kubectl get pods -n mcp-servers --show-labels
```

### Gateway Not Accessible

```bash
# Check service
kubectl get svc -n mcp-system mcp-gateway-external

# Check endpoints
kubectl get endpoints -n mcp-system mcp-gateway

# Check firewall rules
kubectl get networkpolicies -n mcp-system
```

### High Memory Usage

```bash
# Check metrics
kubectl top pods -n mcp-system

# Adjust resources
kubectl set resources deployment mcp-gateway -n mcp-system \
  --limits=memory=2Gi,cpu=4000m \
  --requests=memory=512Mi,cpu=1000m
```

## Cleanup

```bash
# Delete gateway
kubectl delete -f gateway-deployment.yaml
kubectl delete -f gateway-service.yaml
kubectl delete -f gateway-config.yaml
kubectl delete -f rbac.yaml

# Delete Redis
kubectl delete -f redis.yaml

# Delete secrets
kubectl delete secret mcp-gateway-secrets -n mcp-system

# Delete namespaces
kubectl delete namespace mcp-system
kubectl delete namespace mcp-servers
```

## Next Steps

- [Authentication & Security](08-authentication.md) - Secure your deployment
- [Monitoring & Observability](10-monitoring.md) - Set up comprehensive monitoring
- [High Availability Setup](06-ha-deployment.md) - Multi-region deployment
- [Load Balancing](07-load-balancing.md) - Configure load balancing strategies

## Summary

You've successfully:
- ✅ Deployed MCP Bridge Gateway to Kubernetes
- ✅ Configured Kubernetes service discovery
- ✅ Set up high availability with 3 replicas
- ✅ Enabled TLS and JWT authentication
- ✅ Configured Redis for session storage
- ✅ Set up monitoring with Prometheus
- ✅ Configured horizontal pod autoscaling
- ✅ Implemented security best practices

Your MCP Bridge is now running in production on Kubernetes!
