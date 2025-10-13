# Tutorial: Multi-Tenant Setup

Configure MCP Bridge for multi-tenant deployments with namespace isolation, per-tenant authentication, and resource limits.

## Prerequisites

- MCP Bridge Gateway deployed on Kubernetes
- Understanding of multi-tenancy concepts
- 25-30 minutes

## What You'll Build

Multi-tenant architecture with:
- Namespace-based tenant isolation
- Per-tenant authentication
- Resource quotas per tenant
- Tenant-specific rate limits
- Isolated metrics and logging

## Architecture

```
┌──────────────────────────────────────┐
│  MCP Gateway                          │
├──────────────────────────────────────┤
│  Tenant Router                        │
│   ├─ Tenant A → Namespace: tenant-a  │
│   ├─ Tenant B → Namespace: tenant-b  │
│   └─ Tenant C → Namespace: tenant-c  │
└──────────────────────────────────────┘
```

## Configuration

```yaml
version: 1

# Multi-tenant settings
multi_tenant:
  enabled: true
  isolation_mode: namespace

  # Tenant identification
  tenant_identification:
    method: jwt_claim  # or header, subdomain
    claim_name: tenant_id
    header_name: X-Tenant-ID

  # Default tenant
  default_tenant: default

# Discovery per tenant
discovery:
  provider: kubernetes
  kubernetes:
    namespace_selector:
      - tenant-*  # All tenant namespaces

    # Label tenant servers
    label_selector:
      mcp.bridge/tenant: "${TENANT_ID}"

# Authentication per tenant
auth:
  type: jwt
  jwt:
    multi_tenant: true
    issuer_per_tenant: true
    issuers:
      tenant-a: "https://auth.tenant-a.com"
      tenant-b: "https://auth.tenant-b.com"
      tenant-c: "https://auth.tenant-c.com"

# Rate limiting per tenant
rate_limit:
  enabled: true
  per_tenant: true

  tenant_limits:
    tenant-a:
      requests_per_sec: 1000
      burst: 2000

    tenant-b:
      requests_per_sec: 500
      burst: 1000

    tenant-c:
      requests_per_sec: 100
      burst: 200
```

## Tenant Namespace Setup

```bash
# Create tenant namespaces
for tenant in a b c; do
  kubectl create namespace tenant-${tenant}

  # Label namespace
  kubectl label namespace tenant-${tenant} \
    mcp.bridge/tenant=tenant-${tenant}

  # Set resource quota
  kubectl apply -f - <<EOF
apiVersion: v1
kind: ResourceQuota
metadata:
  name: tenant-quota
  namespace: tenant-${tenant}
spec:
  hard:
    requests.cpu: "4"
    requests.memory: 8Gi
    limits.cpu: "8"
    limits.memory: 16Gi
    persistentvolumeclaims: "10"
    services.loadbalancers: "2"
EOF
done
```

## Deploy Tenant MCP Servers

```yaml
# tenant-server.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: mcp-server
  namespace: tenant-a
  labels:
    mcp.bridge/tenant: tenant-a
spec:
  replicas: 2
  selector:
    matchLabels:
      app: mcp-server
      tenant: tenant-a
  template:
    metadata:
      labels:
        app: mcp-server
        tenant: tenant-a
        mcp.bridge/tenant: tenant-a
    spec:
      containers:
      - name: server
        image: your-mcp-server:latest
        env:
        - name: TENANT_ID
          value: "tenant-a"
---
apiVersion: v1
kind: Service
metadata:
  name: mcp-server
  namespace: tenant-a
  labels:
    mcp.bridge/tenant: tenant-a
spec:
  selector:
    app: mcp-server
  ports:
  - port: 8080
```

## Network Policies

```yaml
# Isolate tenant traffic
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: tenant-isolation
  namespace: tenant-a
spec:
  podSelector: {}
  policyTypes:
  - Ingress
  - Egress
  ingress:
  - from:
    - namespaceSelector:
        matchLabels:
          name: mcp-system
  egress:
  - to:
    - namespaceSelector:
        matchLabels:
          name: mcp-system
```

## Per-Tenant Metrics

```yaml
observability:
  metrics:
    enabled: true
    additional_labels:
      tenant: "${TENANT_ID}"

  # Separate metrics per tenant
  tenant_metrics:
    enabled: true
    prefix: "tenant_"
```

## Testing Multi-Tenancy

```bash
# Test tenant A
TOKEN_A=$(generate-token tenant-a)
curl -H "Authorization: Bearer $TOKEN_A" \
  https://gateway/api/v1/mcp

# Test tenant B
TOKEN_B=$(generate-token tenant-b)
curl -H "Authorization: Bearer $TOKEN_B" \
  https://gateway/api/v1/mcp

# Verify isolation
kubectl get pods -n tenant-a
kubectl get pods -n tenant-b
```

## Next Steps

- [API Gateway Pattern](13-api-gateway.md)
- [Authentication & Security](08-authentication.md)

## Summary

Multi-tenant features:
- ✅ Namespace isolation
- ✅ Per-tenant auth
- ✅ Resource quotas
- ✅ Rate limiting
- ✅ Network policies
