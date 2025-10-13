# Tutorial: Connection Troubleshooting

Diagnose and fix common connection issues in MCP Bridge deployments.

## Prerequisites

- Basic understanding of networking
- Access to gateway logs and metrics
- kubectl or docker access
- 20-25 minutes

## Common Issues

### Issue 1: Gateway Won't Start

**Symptoms**: Gateway pod crashes or fails to start

**Diagnosis**:

```bash
# Check pod status
kubectl get pods -n mcp-system

# View logs
kubectl logs -n mcp-system -l app=mcp-gateway --tail=100

# Check events
kubectl describe pod -n mcp-system <pod-name>
```

**Common Causes**:

1. **Invalid Configuration**
```bash
# Validate config
kubectl get configmap mcp-gateway-config -n mcp-system -o yaml

# Check for syntax errors
mcp-gateway --config gateway.yaml --validate
```

2. **Missing Secrets**
```bash
# Check secrets exist
kubectl get secrets -n mcp-system

# Verify JWT secret
kubectl get secret mcp-gateway-secrets -n mcp-system -o jsonpath='{.data.jwt-secret-key}' | base64 -d
```

3. **Port Already in Use**
```bash
# Check what's using the port
netstat -tlnp | grep 8443

# Or in Kubernetes
kubectl get svc -n mcp-system
```

**Solutions**:

```bash
# Fix config syntax
kubectl edit configmap mcp-gateway-config -n mcp-system

# Create missing secret
kubectl create secret generic mcp-gateway-secrets \
  --from-literal=jwt-secret-key=$(openssl rand -base64 32) \
  -n mcp-system

# Change port in config
kubectl patch configmap mcp-gateway-config -n mcp-system --type=json \
  -p='[{"op": "replace", "path": "/data/gateway.yaml/server/port", "value": "8444"}]'
```

### Issue 2: Clients Can't Connect

**Symptoms**: Connection refused, timeout, or immediate disconnect

**Diagnosis**:

```bash
# Test from outside cluster
curl -v http://gateway-external-ip:8443/health

# Test from inside cluster
kubectl run test --image=curlimages/curl -it --rm -- \
  curl -v http://mcp-gateway.mcp-system:8443/health

# Check service
kubectl get svc mcp-gateway -n mcp-system
```

**Common Causes**:

1. **Firewall Rules**
```bash
# Check security groups (AWS)
aws ec2 describe-security-groups --group-ids sg-xxx

# Check firewall (on-prem)
sudo iptables -L -n | grep 8443
```

2. **Network Policy**
```bash
# Check network policies
kubectl get networkpolicies -n mcp-system

# Describe policy
kubectl describe networkpolicy mcp-gateway -n mcp-system
```

3. **LoadBalancer Not Ready**
```bash
# Check LoadBalancer status
kubectl get svc mcp-gateway-external -n mcp-system

# If pending, check cloud provider
kubectl describe svc mcp-gateway-external -n mcp-system
```

**Solutions**:

```bash
# Allow port in firewall
sudo ufw allow 8443/tcp

# Create permissive network policy (temporary)
kubectl apply -f - <<EOF
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: allow-all-ingress
  namespace: mcp-system
spec:
  podSelector:
    matchLabels:
      app: mcp-gateway
  policyTypes:
  - Ingress
  ingress:
  - {}
EOF

# Use NodePort as workaround
kubectl patch svc mcp-gateway -n mcp-system -p '{"spec":{"type":"NodePort"}}'
```

### Issue 3: Backend Server Unreachable

**Symptoms**: Gateway starts but can't reach MCP servers

**Diagnosis**:

```bash
# Check discovery
kubectl logs -n mcp-system -l app=mcp-gateway | grep discovery

# Check backend health
curl http://gateway:9090/metrics | grep mcp_backend_healthy

# Test backend directly
kubectl run test --image=curlimages/curl -it --rm -- \
  curl -v http://backend-server:8080/health
```

**Common Causes**:

1. **Wrong Service Discovery Config**
```yaml
# Check discovery config
discovery:
  provider: kubernetes  # Wrong provider?
  kubernetes:
    namespace_selector:
      - wrong-namespace  # Wrong namespace?
```

2. **Backend Not Running**
```bash
# Check backend pods
kubectl get pods -n mcp-servers

# Check backend logs
kubectl logs -n mcp-servers -l app=mcp-server
```

3. **DNS Resolution Issues**
```bash
# Test DNS from gateway pod
kubectl exec -n mcp-system deployment/mcp-gateway -- \
  nslookup backend-server.mcp-servers.svc.cluster.local
```

**Solutions**:

```bash
# Fix discovery config
kubectl edit configmap mcp-gateway-config -n mcp-system

# Restart backend
kubectl rollout restart deployment/mcp-server -n mcp-servers

# Use IP instead of DNS temporarily
kubectl get pods -n mcp-servers -o wide  # Get pod IP
# Update config with direct IP
```

### Issue 4: Intermittent Connection Drops

**Symptoms**: Connections work but drop unexpectedly

**Diagnosis**:

```bash
# Check for OOM kills
kubectl get pods -n mcp-system --watch

# View events
kubectl get events -n mcp-system --sort-by='.lastTimestamp'

# Check resource usage
kubectl top pods -n mcp-system
```

**Common Causes**:

1. **Resource Limits Too Low**
```bash
# Check limits
kubectl get deployment mcp-gateway -n mcp-system -o yaml | grep -A 5 resources
```

2. **Idle Timeout**
```yaml
# Check timeout settings
server:
  read_timeout: 60s  # Too short?
  write_timeout: 60s
```

3. **Load Balancer Timeout**
```bash
# Check LB idle timeout (AWS)
aws elbv2 describe-load-balancers --names mcp-gateway-lb \
  --query 'LoadBalancers[0].IdleTimeout'
```

**Solutions**:

```bash
# Increase resource limits
kubectl set resources deployment mcp-gateway -n mcp-system \
  --limits=memory=2Gi,cpu=2000m \
  --requests=memory=512Mi,cpu=500m

# Increase timeouts
kubectl patch configmap mcp-gateway-config -n mcp-system --type=json \
  -p='[
    {"op": "replace", "path": "/data/gateway.yaml/server/read_timeout", "value": "300s"},
    {"op": "replace", "path": "/data/gateway.yaml/server/write_timeout", "value": "300s"}
  ]'

# Configure keepalive
kubectl patch configmap mcp-gateway-config -n mcp-system --type=json \
  -p='[{"op": "add", "path": "/data/gateway.yaml/server/keep_alive", "value": "true"}]'
```

### Issue 5: High Latency

**Symptoms**: Slow responses, timeouts

**Diagnosis**:

```bash
# Check latency metrics
curl http://gateway:9090/metrics | grep request_duration

# Run trace
kubectl exec -n mcp-system deployment/mcp-gateway -- \
  curl -v http://backend:8080/health

# Check network latency
kubectl exec -n mcp-system deployment/mcp-gateway -- \
  ping backend-server
```

**Solutions**:

```bash
# Enable connection pooling
kubectl patch configmap mcp-gateway-config -n mcp-system --type=json \
  -p='[{
    "op": "add",
    "path": "/data/gateway.yaml/connection_pool",
    "value": {
      "enabled": true,
      "max_idle_conns": 100,
      "max_conns_per_host": 50
    }
  }]'

# Adjust timeouts
kubectl patch configmap mcp-gateway-config -n mcp-system --type=json \
  -p='[{
    "op": "replace",
    "path": "/data/gateway.yaml/routing/health_check_timeout",
    "value": "10s"
  }]'
```

## Debugging Tools

### Enable Debug Logging

```bash
# Enable debug mode
kubectl set env deployment/mcp-gateway LOG_LEVEL=debug -n mcp-system

# View debug logs
kubectl logs -f -n mcp-system -l app=mcp-gateway | grep DEBUG
```

### Network Debugging

```bash
# Install debugging pod
kubectl run netdebug --image=nicolaka/netshoot -it --rm -- bash

# Inside pod:
# Test connectivity
curl -v http://mcp-gateway.mcp-system:8443

# Check DNS
nslookup mcp-gateway.mcp-system.svc.cluster.local

# Trace route
traceroute mcp-gateway.mcp-system

# Check ports
nc -zv mcp-gateway.mcp-system 8443
```

### Metrics Analysis

```bash
# Connection errors
curl http://gateway:9090/metrics | grep connection_errors_total

# Backend status
curl http://gateway:9090/metrics | grep backend_healthy

# Request rate
curl http://gateway:9090/metrics | grep requests_total
```

## Troubleshooting Checklist

```bash
# 1. Check pod status
kubectl get pods -n mcp-system

# 2. Check logs
kubectl logs -n mcp-system -l app=mcp-gateway --tail=50

# 3. Check service
kubectl get svc -n mcp-system

# 4. Test connectivity
kubectl run test --image=curlimages/curl -it --rm -- \
  curl -v http://mcp-gateway.mcp-system:8443/health

# 5. Check config
kubectl get configmap mcp-gateway-config -n mcp-system -o yaml

# 6. Check secrets
kubectl get secrets -n mcp-system

# 7. Check events
kubectl get events -n mcp-system --sort-by='.lastTimestamp'

# 8. Check metrics
curl http://gateway:9090/metrics

# 9. Check resource usage
kubectl top pods -n mcp-system

# 10. Check network policies
kubectl get networkpolicies -n mcp-system
```

## Next Steps

- [Performance Tuning](12-performance-tuning.md)
- [Monitoring & Observability](10-monitoring.md)
- [Load Balancing](07-load-balancing.md)

## Summary

Common issues resolved:
- Gateway startup failures
- Client connection problems
- Backend connectivity issues
- Connection drops
- High latency problems
