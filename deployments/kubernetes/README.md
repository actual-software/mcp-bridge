# Kubernetes Deployment

This directory contains Kubernetes manifests for deploying the MCP Bridge system in a production Kubernetes environment.

## Quick Deploy

Deploy the complete MCP Bridge system:

```bash
kubectl apply -k .
```

This will create:
- Namespaces for system components
- Gateway deployment with Redis backend
- RBAC and network policies
- Monitoring setup with service monitors
- MCP server deployments

## Components

### Gateway Service (`gateway/`)
- **Deployment**: Multi-replica gateway with HPA
- **Redis**: Session storage and rate limiting backend
- **Services**: LoadBalancer and ClusterIP services
- **Security**: RBAC, NetworkPolicy, and secrets management

### MCP Servers (`mcp-servers/`)
- **K8s Tools**: Kubernetes management MCP server
- **Namespace**: Isolated namespace for MCP servers

### Monitoring (`monitoring/`)
- **ServiceMonitor**: Prometheus metrics collection
- **Grafana Dashboards**: Performance and health monitoring

## Configuration

### Environment Variables

Configure the gateway deployment by editing `gateway/secrets.env`:

```bash
# JWT secret for authentication
JWT_SECRET_KEY=your-jwt-secret-here

# Redis connection (optional, defaults to redis:6379)
REDIS_URL=redis://redis:6379/0
```

### Resource Limits

Default resource limits are conservative. For production, adjust in `gateway/deployment.yaml`:

```yaml
resources:
  requests:
    memory: "512Mi"
    cpu: "500m"
  limits:
    memory: "1Gi" 
    cpu: "1000m"
```

### Horizontal Pod Autoscaling

HPA is configured in `gateway/hpa.yaml` with defaults:
- Min replicas: 3
- Max replicas: 10
- Target CPU: 70%
- Target Memory: 80%

## Security

### RBAC
The gateway service account has minimal required permissions:
- Read access to services and endpoints (for service discovery)
- No cluster-admin or write permissions

### Network Policies
- Gateway can communicate with Redis and MCP servers
- MCP servers isolated from each other
- External traffic only allowed to gateway service

### Secrets Management
- JWT secrets stored in Kubernetes secrets
- Redis passwords (if enabled) in separate secret
- TLS certificates via cert-manager integration

## Monitoring

Prometheus metrics are exposed at:
- Gateway: `:9090/metrics`
- Redis: `:6379/metrics` (if Redis exporter enabled)

Key metrics to monitor:
- `mcp_gateway_connections_total`
- `mcp_gateway_request_duration_seconds`
- `mcp_gateway_circuit_breaker_state`

## Troubleshooting

### Check Gateway Health
```bash
kubectl get pods -n mcp-system
kubectl logs -n mcp-system deployment/mcp-gateway
```

### Test Connectivity
```bash
kubectl port-forward -n mcp-system svc/mcp-gateway 8443:8443
curl -k https://localhost:8443/health
```

### Redis Connection Issues
```bash
kubectl exec -n mcp-system deployment/mcp-gateway -- nc -zv redis 6379
```

## Customization

### Using Kustomize

Create a custom overlay:

```bash
# Create overlay directory
mkdir -p overlays/production

# Create kustomization.yaml
cat > overlays/production/kustomization.yaml << EOF
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

namespace: mcp-production

resources:
- ../../base

patchesStrategicMerge:
- replica-patch.yaml
- resource-patch.yaml
EOF

# Apply custom overlay
kubectl apply -k overlays/production
```

### Helm Alternative

For Helm users, convert to Helm chart:

```bash
helm create mcp-bridge
# Copy manifests to templates/
# Add values.yaml for configuration
```

## High Availability

For production HA setup:
- Use external Redis cluster
- Deploy across multiple zones
- Configure pod disruption budgets
- Use persistent volumes for stateful components

See [High Availability Guide](../docs/deployment/high-availability.md) for detailed HA configuration.