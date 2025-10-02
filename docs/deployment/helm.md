# Helm Deployment Guide

This guide provides comprehensive instructions for deploying MCP Bridge using Helm charts on Kubernetes clusters.

## 🚀 **Quick Start**

### **Prerequisites**

- Kubernetes 1.19+
- Helm 3.8.0+
- kubectl configured to access your cluster
- PV provisioner for persistent storage

### **Install MCP Bridge**

```bash
# Add the MCP Bridge Helm repository (future)
# helm repo add mcp-bridge https://charts.mcp-bridge.io
# helm repo update

# For now, clone the repository
git clone https://github.com/actual-software/mcp-bridge.git
cd mcp-bridge

# Install with default values
helm install mcp-bridge ./deployment/helm/mcp-bridge

# Or install with custom release name
helm install my-mcp-bridge ./deployment/helm/mcp-bridge
```

### **Verify Installation**

```bash
# Check deployment status
kubectl get pods -l app.kubernetes.io/name=mcp-bridge

# Run tests
helm test mcp-bridge

# Access services
kubectl port-forward service/mcp-bridge-gateway 8443:8443
kubectl port-forward service/mcp-bridge-router 9091:9091
```

## 🏗️ **Chart Architecture**

### **Components Deployed**

- **Gateway**: Enterprise-grade API gateway (2 replicas by default)
- **Router**: Client-side router (1 replica by default)
- **Redis**: Session storage and rate limiting (optional)
- **ConfigMaps**: Configuration management
- **Secrets**: Credential management
- **Services**: Internal and external access
- **Ingress**: External HTTP(S) access (optional)
- **ServiceMonitor**: Prometheus metrics collection (optional)

### **Chart Structure**

```
helm/mcp-bridge/
├── Chart.yaml                     # Chart metadata
├── values.yaml                    # Default configuration
├── values-production.yaml         # Production values
├── values-development.yaml        # Development values
├── templates/
│   ├── gateway-deployment.yaml    # Gateway deployment
│   ├── router-deployment.yaml     # Router deployment
│   ├── gateway-service.yaml       # Gateway service
│   ├── router-service.yaml        # Router service
│   ├── gateway-ingress.yaml       # Ingress configuration
│   ├── gateway-hpa.yaml           # Horizontal Pod Autoscaler
│   ├── gateway-pdb.yaml           # Pod Disruption Budget
│   ├── secrets.yaml               # Secret management
│   ├── serviceaccount.yaml        # Service accounts
│   ├── networkpolicy.yaml         # Network policies
│   ├── servicemonitor.yaml        # Prometheus monitoring
│   └── tests/                     # Helm tests
└── README.md                      # Chart documentation
```

## ⚙️ **Configuration**

### **Basic Configuration**

```bash
# Install with custom values
helm install mcp-bridge ./deployment/helm/mcp-bridge \
  --set gateway.replicaCount=3 \
  --set router.resources.limits.memory=512Mi \
  --set redis.auth.password=mypassword
```

### **Production Configuration**

```bash
# Install with production values
helm install mcp-bridge ./deployment/helm/mcp-bridge \
  -f helm/mcp-bridge/values-production.yaml \
  --set gateway.ingress.hosts[0].host=mcp.your-domain.com
```

### **Development Configuration**

```bash
# Install with development values
helm install mcp-bridge ./deployment/helm/mcp-bridge \
  -f helm/mcp-bridge/values-development.yaml
```

### **Custom Values File**

```yaml
# custom-values.yaml
gateway:
  replicaCount: 5
  resources:
    limits:
      cpu: 2000m
      memory: 1Gi
  ingress:
    enabled: true
    className: nginx
    hosts:
      - host: mcp-gateway.example.com
        paths:
          - path: /
            pathType: Prefix

tls:
  enabled: true
  cert: |
    -----BEGIN CERTIFICATE-----
    ... your certificate ...
  key: |
    -----BEGIN PRIVATE KEY-----
    ... your private key ...

redis:
  master:
    persistence:
      size: 20Gi
```

```bash
helm install mcp-bridge ./deployment/helm/mcp-bridge -f custom-values.yaml
```

## 🔧 **Key Configuration Parameters**

### **Gateway Configuration**

| Parameter | Description | Default |
|-----------|-------------|---------|
| `gateway.enabled` | Enable Gateway deployment | `true` |
| `gateway.replicaCount` | Number of Gateway replicas | `2` |
| `gateway.image.repository` | Gateway image repository | `ghcr.io/actual-software/mcp-bridge/gateway` |
| `gateway.resources.limits.cpu` | Gateway CPU limit | `1000m` |
| `gateway.resources.limits.memory` | Gateway memory limit | `512Mi` |
| `gateway.autoscaling.enabled` | Enable HPA | `true` |
| `gateway.autoscaling.minReplicas` | Minimum replicas | `2` |
| `gateway.autoscaling.maxReplicas` | Maximum replicas | `10` |
| `gateway.ingress.enabled` | Enable ingress | `false` |
| `gateway.service.type` | Service type | `ClusterIP` |

### **Router Configuration**

| Parameter | Description | Default |
|-----------|-------------|---------|
| `router.enabled` | Enable Router deployment | `true` |
| `router.replicaCount` | Number of Router replicas | `1` |
| `router.image.repository` | Router image repository | `ghcr.io/actual-software/mcp-bridge/router` |
| `router.resources.limits.cpu` | Router CPU limit | `500m` |
| `router.resources.limits.memory` | Router memory limit | `256Mi` |

### **Security Configuration**

| Parameter | Description | Default |
|-----------|-------------|---------|
| `tls.enabled` | Enable TLS | `true` |
| `tls.create` | Auto-generate certificates | `true` |
| `networkPolicy.enabled` | Enable network policies | `true` |
| `podSecurityContext.runAsUser` | Run as user ID | `65534` |
| `securityContext.readOnlyRootFilesystem` | Read-only filesystem | `true` |

### **Monitoring Configuration**

| Parameter | Description | Default |
|-----------|-------------|---------|
| `serviceMonitor.enabled` | Enable Prometheus monitoring | `true` |
| `serviceMonitor.interval` | Scrape interval | `30s` |
| `prometheus.enabled` | Deploy Prometheus | `false` |
| `grafana.enabled` | Deploy Grafana | `false` |

## 🚀 **Deployment Scenarios**

### **Development Deployment**

```bash
# Minimal resources, debug logging, no persistence
helm install mcp-dev ./deployment/helm/mcp-bridge \
  -f helm/mcp-bridge/values-development.yaml \
  --namespace mcp-dev \
  --create-namespace
```

**Features:**
- Single replica for each component
- Debug logging enabled
- No persistence (faster startup)
- Self-signed certificates
- No ingress (use port-forward)

### **Staging Deployment**

```bash
# Production-like with reduced resources
helm install mcp-staging ./deployment/helm/mcp-bridge \
  --set gateway.replicaCount=2 \
  --set router.replicaCount=1 \
  --set redis.master.persistence.size=10Gi \
  --set gateway.ingress.enabled=true \
  --set gateway.ingress.hosts[0].host=mcp-staging.company.com \
  --namespace mcp-staging \
  --create-namespace
```

### **Production Deployment**

```bash
# High availability, monitoring, security
helm install mcp-prod ./deployment/helm/mcp-bridge \
  -f helm/mcp-bridge/values-production.yaml \
  --set gateway.ingress.hosts[0].host=mcp.company.com \
  --set secrets.jwtSecretKey="$(openssl rand -base64 32)" \
  --set secrets.mcpAuthToken="$(openssl rand -hex 32)" \
  --namespace mcp-production \
  --create-namespace
```

### **Multi-Environment Deployment**

```bash
# Deploy to multiple namespaces
for env in dev staging prod; do
  helm install mcp-$env ./deployment/helm/mcp-bridge \
    -f helm/mcp-bridge/values-$env.yaml \
    --namespace mcp-$env \
    --create-namespace
done
```

## 🔍 **Operations**

### **Upgrading**

```bash
# Upgrade to new version
helm upgrade mcp-bridge ./deployment/helm/mcp-bridge

# Upgrade with new values
helm upgrade mcp-bridge ./deployment/helm/mcp-bridge \
  -f new-values.yaml

# Rollback if needed
helm rollback mcp-bridge 1
```

### **Scaling**

```bash
# Manual scaling
kubectl scale deployment mcp-bridge-gateway --replicas=5

# Or update Helm values
helm upgrade mcp-bridge ./deployment/helm/mcp-bridge \
  --set gateway.replicaCount=5
```

### **Monitoring**

```bash
# Check status
helm status mcp-bridge
kubectl get pods -l app.kubernetes.io/name=mcp-bridge

# View logs
kubectl logs -l app.kubernetes.io/component=gateway
kubectl logs -l app.kubernetes.io/component=router

# Check metrics
kubectl port-forward service/mcp-bridge-gateway 9090:9090
curl http://localhost:9090/metrics
```

### **Testing**

```bash
# Run Helm tests
helm test mcp-bridge

# Manual health checks
kubectl port-forward service/mcp-bridge-gateway 8443:8443
curl -k https://localhost:8443/health
```

## 🛡️ **Security Best Practices**

### **Certificate Management**

```bash
# Use cert-manager for automatic certificates
kubectl apply -f https://github.com/jetstack/cert-manager/releases/download/v1.12.0/cert-manager.yaml

# Create ClusterIssuer
cat <<EOF | kubectl apply -f -
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: letsencrypt-prod
spec:
  acme:
    server: https://acme-v02.api.letsencrypt.org/directory
    email: admin@your-domain.com
    privateKeySecretRef:
      name: letsencrypt-prod
    solvers:
    - http01:
        ingress:
          class: nginx
EOF
```

### **Secret Management**

```bash
# Use external secret management
helm install mcp-bridge ./deployment/helm/mcp-bridge \
  --set secrets.create=false \
  --set tls.create=false

# Create secrets separately
kubectl create secret generic mcp-bridge-secrets \
  --from-literal=jwt-secret-key="$(openssl rand -base64 32)" \
  --from-literal=mcp-auth-token="$(openssl rand -hex 32)"

kubectl create secret tls mcp-bridge-tls \
  --cert=path/to/tls.crt \
  --key=path/to/tls.key
```

### **Network Security**

```bash
# Enable network policies
helm upgrade mcp-bridge ./deployment/helm/mcp-bridge \
  --set networkPolicy.enabled=true
```

## 🔧 **Troubleshooting**

### **Common Issues**

#### **ImagePullBackOff**
```bash
# Check image registry access
kubectl describe pod mcp-bridge-gateway-xxx

# Add image pull secrets if needed
kubectl create secret docker-registry regcred \
  --docker-server=ghcr.io \
  --docker-username=your-username \
  --docker-password=your-token

helm upgrade mcp-bridge ./deployment/helm/mcp-bridge \
  --set global.imagePullSecrets[0].name=regcred
```

#### **CrashLoopBackOff**
```bash
# Check logs
kubectl logs mcp-bridge-gateway-xxx

# Check configuration
kubectl get configmap mcp-bridge-gateway-config -o yaml
```

#### **Service Not Accessible**
```bash
# Check service endpoints
kubectl get endpoints mcp-bridge-gateway

# Check ingress
kubectl describe ingress mcp-bridge-gateway

# Test internal connectivity
kubectl run -it --rm debug --image=busybox --restart=Never -- sh
nslookup mcp-bridge-gateway
wget -O- http://mcp-bridge-gateway:8443/health
```

### **Debug Mode**

```bash
# Enable debug logging
helm upgrade mcp-bridge ./deployment/helm/mcp-bridge \
  --set gateway.env.MCP_LOG_LEVEL=debug \
  --set router.env.MCP_LOG_LEVEL=debug
```

### **Resource Issues**

```bash
# Check resource usage
kubectl top pods -l app.kubernetes.io/name=mcp-bridge
kubectl describe nodes

# Adjust resource limits
helm upgrade mcp-bridge ./deployment/helm/mcp-bridge \
  --set gateway.resources.limits.memory=1Gi \
  --set gateway.resources.limits.cpu=2000m
```

## 📊 **Monitoring Integration**

### **Prometheus Integration**

```bash
# Deploy with Prometheus
helm install mcp-bridge ./deployment/helm/mcp-bridge \
  --set prometheus.enabled=true \
  --set serviceMonitor.enabled=true
```

### **Grafana Dashboards**

```bash
# Deploy with Grafana
helm install mcp-bridge ./deployment/helm/mcp-bridge \
  --set grafana.enabled=true \
  --set grafana.adminPassword=admin123
```

### **Custom Monitoring**

```yaml
# Custom ServiceMonitor
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: mcp-bridge-custom
spec:
  selector:
    matchLabels:
      app.kubernetes.io/name: mcp-bridge
  endpoints:
  - port: metrics
    interval: 15s
    path: /metrics
```

## 🔄 **CI/CD Integration**

### **ArgoCD**

```yaml
# argocd-application.yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: mcp-bridge
  namespace: argocd
spec:
  project: default
  source:
    repoURL: https://github.com/actual-software/mcp-bridge
    targetRevision: HEAD
    path: helm/mcp-bridge
    helm:
      valueFiles:
      - values-production.yaml
  destination:
    server: https://kubernetes.default.svc
    namespace: mcp-production
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
```

### **GitLab CI**

```yaml
# .gitlab-ci.yml
deploy:
  stage: deploy
  script:
    - helm upgrade --install mcp-bridge ./deployment/helm/mcp-bridge
        -f helm/mcp-bridge/values-production.yaml
        --namespace mcp-production
        --create-namespace
  only:
    - main
```

### **GitHub Actions**

```yaml
# .github/workflows/deploy.yml
name: Deploy to Kubernetes
on:
  push:
    branches: [main]
jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
    - uses: actions/checkout@v3
    - name: Deploy with Helm
      run: |
        helm upgrade --install mcp-bridge ./deployment/helm/mcp-bridge \
          -f helm/mcp-bridge/values-production.yaml \
          --namespace mcp-production \
          --create-namespace
```

## 📚 **Additional Resources**

### **Helm Commands Reference**

```bash
# Chart management
helm create my-chart
helm lint ./deployment/helm/mcp-bridge
helm template mcp-bridge ./deployment/helm/mcp-bridge
helm package ./deployment/helm/mcp-bridge

# Release management
helm list
helm status mcp-bridge
helm history mcp-bridge
helm get values mcp-bridge
helm get manifest mcp-bridge

# Repository management
helm repo add stable https://charts.helm.sh/stable
helm repo update
helm search repo mcp-bridge
```

### **Kubernetes Resources**

- [Kubernetes Documentation](https://kubernetes.io/docs/)
- [Helm Documentation](https://helm.sh/docs/)
- [Prometheus Operator](https://prometheus-operator.dev/)
- [cert-manager](https://cert-manager.io/docs/)

### **Support**

- [GitHub Issues](https://github.com/actual-software/mcp-bridge/issues)
- [Discussions](https://github.com/actual-software/mcp-bridge/discussions)
- [Documentation](https://github.com/actual-software/mcp-bridge/tree/main/docs)

---

**The Helm chart provides a production-ready, scalable, and secure deployment of MCP Bridge on Kubernetes with comprehensive configuration options and operational features.**