# Operational Runbooks

Step-by-step procedures for common operational tasks and incident response for MCP Bridge.

## Table of Contents

- [Overview](#overview)
- [Service Operations](#service-operations)
- [Incident Response](#incident-response)
- [Maintenance Procedures](#maintenance-procedures)
- [Performance Tuning](#performance-tuning)
- [Security Operations](#security-operations)
- [Troubleshooting Guides](#troubleshooting-guides)
- [Emergency Procedures](#emergency-procedures)

## Overview

These runbooks provide standardized procedures for operating MCP Bridge in production. Each runbook follows a consistent format with clear steps, expected outcomes, and rollback procedures.

### Runbook Format

Each runbook contains:
- **Purpose**: What the runbook accomplishes
- **Prerequisites**: Required access, tools, and conditions
- **Steps**: Detailed procedure with commands
- **Verification**: How to confirm success
- **Rollback**: How to undo changes if needed
- **Escalation**: When and how to escalate

## Service Operations

### Runbook: Service Health Check

**Purpose**: Verify all MCP Bridge components are healthy

**Prerequisites**:
- kubectl access to cluster
- Read access to monitoring systems

**Steps**:

```bash
#!/bin/bash
# health-check.sh

echo "=== MCP Bridge Health Check ==="
echo "Time: $(date)"

# 1. Check Gateway health
echo -n "Gateway Health: "
if curl -sf http://gateway:8080/health > /dev/null; then
  echo "✓ OK"
else
  echo "✗ FAILED"
  EXIT_CODE=1
fi

# 2. Check Router health
echo -n "Router Health: "
if curl -sf http://router:9091/health > /dev/null; then
  echo "✓ OK"
else
  echo "✗ FAILED"
  EXIT_CODE=1
fi

# 3. Check Redis connectivity
echo -n "Redis Health: "
if redis-cli ping | grep -q PONG; then
  echo "✓ OK"
else
  echo "✗ FAILED"
  EXIT_CODE=1
fi

# 4. Check Kubernetes pods
echo "Kubernetes Pods:"
kubectl get pods -n mcp-system --no-headers | while read line; do
  name=$(echo $line | awk '{print $1}')
  status=$(echo $line | awk '{print $3}')
  ready=$(echo $line | awk '{print $2}')
  if [[ "$status" == "Running" ]] && [[ "$ready" == *"/"* ]]; then
    echo "  $name: ✓ $status ($ready)"
  else
    echo "  $name: ✗ $status ($ready)"
    EXIT_CODE=1
  fi
done

# 5. Check metrics endpoint
echo -n "Metrics Available: "
if curl -sf http://gateway:9090/metrics | grep -q mcp_; then
  echo "✓ OK"
else
  echo "✗ FAILED"
fi

exit ${EXIT_CODE:-0}
```

**Verification**:
- All checks should return "OK"
- Exit code should be 0

**Escalation**:
- If any check fails, proceed to specific troubleshooting runbook
- Page on-call if multiple components are down

### Runbook: Service Restart

**Purpose**: Safely restart MCP Bridge services

**Prerequisites**:
- Admin access to Kubernetes cluster
- Maintenance window scheduled (if production)

**Steps**:

```bash
#!/bin/bash
# restart-service.sh

SERVICE=$1  # gateway or router
NAMESPACE=${2:-mcp-system}

if [[ -z "$SERVICE" ]]; then
  echo "Usage: $0 <gateway|router> [namespace]"
  exit 1
fi

echo "=== Restarting MCP $SERVICE ==="

# 1. Check current state
echo "Current state:"
kubectl get deployment mcp-$SERVICE -n $NAMESPACE

# 2. Scale down to 0
echo "Scaling down..."
kubectl scale deployment mcp-$SERVICE -n $NAMESPACE --replicas=0

# 3. Wait for pods to terminate
echo "Waiting for pods to terminate..."
kubectl wait --for=delete pod -l app=mcp-$SERVICE -n $NAMESPACE --timeout=60s

# 4. Clear any persistent state if needed
if [[ "$SERVICE" == "gateway" ]]; then
  echo "Clearing gateway session cache..."
  redis-cli -n 1 FLUSHDB
fi

# 5. Scale back up
echo "Scaling up..."
REPLICAS=$(kubectl get deployment mcp-$SERVICE -n $NAMESPACE -o jsonpath='{.spec.replicas}')
kubectl scale deployment mcp-$SERVICE -n $NAMESPACE --replicas=${REPLICAS:-3}

# 6. Wait for pods to be ready
echo "Waiting for pods to be ready..."
kubectl wait --for=condition=Ready pod -l app=mcp-$SERVICE -n $NAMESPACE --timeout=300s

# 7. Verify health
sleep 10
./health-check.sh

# 8. Run smoke tests to verify functionality
echo "Running smoke tests..."
make -f Makefile.test test-docker-quick || echo "Warning: Some smoke tests failed"

echo "Restart completed"
```

**Verification**:
- All pods should be Running
- Health endpoints should respond
- No errors in logs

**Rollback**:
- If service won't start, check logs: `kubectl logs -l app=mcp-$SERVICE -n $NAMESPACE`
- Restore previous version if needed: `kubectl rollout undo deployment mcp-$SERVICE -n $NAMESPACE`

### Runbook: Scale Service

**Purpose**: Scale MCP Bridge services up or down

**Prerequisites**:
- Kubernetes admin access
- Understanding of current load

**Steps**:

```bash
#!/bin/bash
# scale-service.sh

SERVICE=$1
REPLICAS=$2
NAMESPACE=${3:-mcp-system}

if [[ -z "$SERVICE" ]] || [[ -z "$REPLICAS" ]]; then
  echo "Usage: $0 <gateway|router> <replicas> [namespace]"
  exit 1
fi

echo "=== Scaling MCP $SERVICE to $REPLICAS replicas ==="

# 1. Check current replicas
CURRENT=$(kubectl get deployment mcp-$SERVICE -n $NAMESPACE -o jsonpath='{.status.replicas}')
echo "Current replicas: $CURRENT"
echo "Target replicas: $REPLICAS"

# 2. Check resource availability
echo "Checking cluster resources..."
kubectl top nodes

# 3. Scale deployment
echo "Scaling deployment..."
kubectl scale deployment mcp-$SERVICE -n $NAMESPACE --replicas=$REPLICAS

# 4. Monitor scaling progress
echo "Monitoring scale operation..."
kubectl rollout status deployment mcp-$SERVICE -n $NAMESPACE --timeout=300s

# 5. Verify all pods are ready
echo "Verifying pods..."
kubectl get pods -l app=mcp-$SERVICE -n $NAMESPACE

# 6. Update HPA if exists
if kubectl get hpa mcp-$SERVICE-hpa -n $NAMESPACE 2>/dev/null; then
  echo "Updating HPA min/max replicas..."
  kubectl patch hpa mcp-$SERVICE-hpa -n $NAMESPACE \
    --patch "{\"spec\":{\"minReplicas\":$REPLICAS,\"maxReplicas\":$((REPLICAS * 2))}}"
fi

# 7. Verify service health
./health-check.sh

echo "Scaling completed successfully"
```

**Verification**:
- Correct number of pods running
- All pods healthy
- Load distributed evenly

## Incident Response

### Runbook: High Memory Usage

**Purpose**: Investigate and resolve high memory usage

**Prerequisites**:
- Monitoring access
- kubectl access
- Permission to restart services

**Steps**:

```bash
#!/bin/bash
# high-memory-response.sh

echo "=== High Memory Usage Response ==="

# 1. Identify the service with high memory
echo "Checking memory usage..."
kubectl top pods -n mcp-system --sort-by=memory

# 2. Get detailed metrics
POD=$(kubectl top pods -n mcp-system --sort-by=memory --no-headers | head -1 | awk '{print $1}')
echo "Highest memory pod: $POD"

# 3. Check for memory leaks
echo "Recent memory trend:"
kubectl exec $POD -n mcp-system -- sh -c 'cat /proc/meminfo | grep -E "MemTotal|MemFree|MemAvailable"'

# 4. Collect heap dump (if Go service)
echo "Collecting heap profile..."
kubectl exec $POD -n mcp-system -- sh -c 'curl -s http://localhost:6060/debug/pprof/heap > /tmp/heap.prof'
kubectl cp mcp-system/$POD:/tmp/heap.prof ./heap-$(date +%s).prof

# 5. Check for large caches
echo "Checking Redis memory..."
redis-cli INFO memory | grep used_memory_human

# 6. Temporary mitigation - restart if critical
MEMORY_PERCENT=$(kubectl top pod $POD -n mcp-system --no-headers | awk '{print $3}' | sed 's/%//')
if [[ $MEMORY_PERCENT -gt 90 ]]; then
  echo "Memory critical (${MEMORY_PERCENT}%), restarting pod..."
  kubectl delete pod $POD -n mcp-system
  
  # Wait for replacement
  sleep 30
  kubectl wait --for=condition=Ready pod -l app=$(echo $POD | cut -d- -f1-2) -n mcp-system --timeout=120s
fi

# 7. Long-term fix - adjust limits if needed
echo "Current resource limits:"
kubectl get pod $POD -n mcp-system -o jsonpath='{.spec.containers[0].resources}'

echo "Consider updating deployment with increased limits if pattern continues"
```

**Verification**:
- Memory usage should decrease
- Service should remain healthy
- No OOM kills in events

**Escalation**:
- If memory continues to grow, page development team
- Collect heap dumps for analysis

### Runbook: Service Unavailable

**Purpose**: Restore service availability quickly

**Prerequisites**:
- On-call access
- Admin permissions

**Steps**:

```bash
#!/bin/bash
# service-unavailable.sh

SERVICE=$1
echo "=== Service Unavailable Response for $SERVICE ==="

# 1. Quick health check
echo "Quick health check..."
curl -f http://$SERVICE:8080/health || true

# 2. Check pods
echo "Checking pods..."
kubectl get pods -n mcp-system -l app=mcp-$SERVICE

# 3. Check recent events
echo "Recent events:"
kubectl get events -n mcp-system --sort-by='.lastTimestamp' | head -20

# 4. Check for crashloops
RESTARTS=$(kubectl get pods -n mcp-system -l app=mcp-$SERVICE -o jsonpath='{.items[0].status.containerStatuses[0].restartCount}')
if [[ $RESTARTS -gt 5 ]]; then
  echo "Pod in crashloop (restarts: $RESTARTS)"
  
  # Get last logs before crash
  kubectl logs -n mcp-system -l app=mcp-$SERVICE --previous --tail=50
  
  # Try rolling back
  echo "Rolling back to previous version..."
  kubectl rollout undo deployment mcp-$SERVICE -n mcp-system
  kubectl rollout status deployment mcp-$SERVICE -n mcp-system
fi

# 5. Check network connectivity
echo "Checking network..."
kubectl exec -n mcp-system deployment/mcp-$SERVICE -- nslookup kubernetes.default

# 6. Restart if necessary
if ! curl -sf http://$SERVICE:8080/health; then
  echo "Service still down, forcing restart..."
  kubectl rollout restart deployment mcp-$SERVICE -n mcp-system
  kubectl rollout status deployment mcp-$SERVICE -n mcp-system
fi

# 7. Verify recovery
sleep 30
./health-check.sh
```

**Verification**:
- Service responds to health checks
- No error events in last 5 minutes
- Metrics being collected

## Maintenance Procedures

### Runbook: Certificate Renewal

**Purpose**: Renew TLS certificates before expiration

**Prerequisites**:
- Certificate files or cert-manager access
- Maintenance window

**Steps**:

```bash
#!/bin/bash
# renew-certificates.sh

echo "=== Certificate Renewal ==="

# 1. Check current certificate expiration
echo "Current certificate status:"
kubectl get certificate -n mcp-system
openssl x509 -in /etc/mcp-bridge/certs/tls.crt -noout -dates

# 2. Backup current certificates
echo "Backing up current certificates..."
kubectl get secret mcp-tls -n mcp-system -o yaml > mcp-tls-backup-$(date +%s).yaml

# 3. Generate new certificates (if manual)
if [[ "$1" == "manual" ]]; then
  echo "Generating new certificates..."
  openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
    -keyout tls.key -out tls.crt \
    -subj "/CN=mcp-gateway.mcp-system.svc.cluster.local"
  
  # Create new secret
  kubectl create secret tls mcp-tls-new \
    --cert=tls.crt --key=tls.key -n mcp-system
else
  # Using cert-manager
  echo "Triggering cert-manager renewal..."
  kubectl annotate certificate mcp-cert -n mcp-system \
    cert-manager.io/issue-temporary-certificate="true" --overwrite
fi

# 4. Update services to use new certificates
echo "Updating services..."
kubectl set env deployment/mcp-gateway -n mcp-system CERT_REFRESH=$(date +%s)
kubectl set env deployment/mcp-router -n mcp-system CERT_REFRESH=$(date +%s)

# 5. Rolling restart
echo "Rolling restart..."
kubectl rollout restart deployment mcp-gateway mcp-router -n mcp-system
kubectl rollout status deployment mcp-gateway -n mcp-system
kubectl rollout status deployment mcp-router -n mcp-system

# 6. Verify new certificates
echo "Verifying new certificates..."
openssl s_client -connect gateway:8443 -servername gateway < /dev/null | \
  openssl x509 -noout -dates

echo "Certificate renewal completed"
```

**Verification**:
- New expiration date should be 1 year out
- Services should restart without errors
- TLS connections should work

### Runbook: Database Maintenance

**Purpose**: Perform routine database maintenance

**Prerequisites**:
- Database admin access
- Maintenance window
- Recent backup

**Steps**:

```bash
#!/bin/bash
# database-maintenance.sh

echo "=== Database Maintenance ==="

# 1. Check current database size
echo "Current database status:"
redis-cli INFO memory
redis-cli DBSIZE

# 2. Create backup
echo "Creating backup..."
redis-cli BGSAVE
while [[ $(redis-cli LASTSAVE) -eq $(redis-cli LASTSAVE) ]]; do
  sleep 1
done
cp /var/lib/redis/dump.rdb /backups/redis-maintenance-$(date +%s).rdb

# 3. Clean up expired keys
echo "Cleaning expired keys..."
redis-cli --scan --pattern "*" | while read key; do
  TTL=$(redis-cli TTL "$key")
  if [[ $TTL -eq -2 ]]; then
    redis-cli DEL "$key"
  fi
done

# 4. Optimize memory
echo "Optimizing memory..."
redis-cli MEMORY PURGE
redis-cli MEMORY DOCTOR

# 5. Compact AOF if used
if [[ -f /var/lib/redis/appendonly.aof ]]; then
  echo "Compacting AOF..."
  redis-cli BGREWRITEAOF
  while [[ $(redis-cli INFO persistence | grep aof_rewrite_in_progress:1) ]]; do
    sleep 1
  done
fi

# 6. Update statistics
redis-cli CONFIG RESETSTAT

# 7. Verify health
redis-cli ping

echo "Database maintenance completed"
```

**Verification**:
- Memory usage should be optimized
- No errors in Redis logs
- Performance metrics should improve

## Performance Tuning

### Runbook: Optimize Gateway Performance

**Purpose**: Tune Gateway for better performance

**Prerequisites**:
- Performance metrics baseline
- Load testing capability

**Steps**:

```bash
#!/bin/bash
# tune-gateway-performance.sh

echo "=== Gateway Performance Tuning ==="

# 1. Analyze current performance
echo "Current performance metrics:"
curl -s http://gateway:9090/metrics | grep -E "http_request_duration|go_goroutines|process_resident_memory"

# 2. Adjust connection pool settings
echo "Updating connection pool..."
kubectl patch configmap mcp-gateway-config -n mcp-system --type merge -p '
{
  "data": {
    "connection_pool_size": "100",
    "max_idle_connections": "50",
    "connection_timeout": "30s"
  }
}'

# 3. Tune garbage collection
echo "Tuning GC settings..."
kubectl set env deployment/mcp-gateway -n mcp-system \
  GOGC=100 \
  GOMEMLIMIT=2GiB

# 4. Adjust resource limits
echo "Updating resource limits..."
kubectl patch deployment mcp-gateway -n mcp-system --type json -p '
[{
  "op": "replace",
  "path": "/spec/template/spec/containers/0/resources",
  "value": {
    "requests": {"memory": "1Gi", "cpu": "500m"},
    "limits": {"memory": "2Gi", "cpu": "2000m"}
  }
}]'

# 5. Enable connection reuse
kubectl patch configmap mcp-gateway-config -n mcp-system --type merge -p '
{
  "data": {
    "keep_alive_enabled": "true",
    "keep_alive_timeout": "90s",
    "tcp_no_delay": "true"
  }
}'

# 6. Restart with new settings
kubectl rollout restart deployment mcp-gateway -n mcp-system
kubectl rollout status deployment mcp-gateway -n mcp-system

# 7. Run performance test
echo "Running performance test..."
ab -n 10000 -c 100 -H "Authorization: Bearer test-token" http://gateway:8080/health

echo "Performance tuning completed"
```

**Verification**:
- Response times should improve
- Memory usage should stabilize
- Throughput should increase

### Runbook: Cache Optimization

**Purpose**: Optimize caching for better performance

**Steps**:

```bash
#!/bin/bash
# optimize-cache.sh

echo "=== Cache Optimization ==="

# 1. Analyze cache hit rates
echo "Current cache statistics:"
redis-cli INFO stats | grep -E "keyspace_hits|keyspace_misses"

# 2. Identify hot keys
echo "Hot keys analysis:"
redis-cli --hotkeys

# 3. Adjust cache TTLs
echo "Optimizing TTLs..."
redis-cli --scan --pattern "session:*" | while read key; do
  redis-cli EXPIRE "$key" 3600  # 1 hour for sessions
done

redis-cli --scan --pattern "cache:*" | while read key; do
  redis-cli EXPIRE "$key" 300  # 5 minutes for cache
done

# 4. Configure cache eviction policy
redis-cli CONFIG SET maxmemory-policy allkeys-lru
redis-cli CONFIG SET maxmemory 2gb

# 5. Warm up cache
echo "Warming up cache..."
./scripts/cache-warmup.sh

echo "Cache optimization completed"
```

## Security Operations

### Runbook: Security Incident Response

**Purpose**: Respond to security incidents

**Prerequisites**:
- Security team contact
- Incident response access

**Steps**:

```bash
#!/bin/bash
# security-incident.sh

INCIDENT_TYPE=$1  # intrusion, dos, data-breach

echo "=== Security Incident Response: $INCIDENT_TYPE ==="

# 1. Isolate affected systems
echo "Isolating affected systems..."
kubectl cordon node-1 node-2  # Prevent new pods
kubectl label namespace mcp-system security-incident=active

# 2. Collect evidence
echo "Collecting evidence..."
mkdir -p /incident/$(date +%Y%m%d-%H%M%S)
cd /incident/$(date +%Y%m%d-%H%M%S)

# Collect logs
kubectl logs -n mcp-system --all-containers=true --since=24h > k8s-logs.txt
docker logs $(docker ps -q) > docker-logs.txt
journalctl --since="24 hours ago" > system-logs.txt

# Network connections
ss -tunap > network-connections.txt
iptables -L -n -v > firewall-rules.txt

# 3. Block suspicious activity
case "$INCIDENT_TYPE" in
  intrusion)
    # Block suspicious IPs
    kubectl apply -f - <<EOF
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: emergency-block
  namespace: mcp-system
spec:
  podSelector: {}
  policyTypes:
  - Ingress
  ingress:
  - from:
    - podSelector:
        matchLabels:
          trusted: "true"
EOF
    ;;
  
  dos)
    # Enable rate limiting
    kubectl patch configmap mcp-gateway-config -n mcp-system --type merge -p '
    {
      "data": {
        "rate_limit_enabled": "true",
        "rate_limit_rps": "10",
        "circuit_breaker_enabled": "true"
      }
    }'
    kubectl rollout restart deployment mcp-gateway -n mcp-system
    ;;
  
  data-breach)
    # Rotate all credentials
    ./scripts/rotate-all-credentials.sh
    # Revoke all sessions
    redis-cli FLUSHDB
    ;;
esac

# 4. Notify security team
curl -X POST $SECURITY_WEBHOOK -d "{\"incident\": \"$INCIDENT_TYPE\", \"time\": \"$(date)\"}"

# 5. Document incident
cat > incident-report.md <<EOF
# Security Incident Report

**Type**: $INCIDENT_TYPE
**Time**: $(date)
**Affected Systems**: MCP Bridge
**Actions Taken**:
- Systems isolated
- Evidence collected
- Mitigation applied
**Status**: Under investigation
EOF

echo "Initial response completed. Continue with security team procedures."
```

**Verification**:
- Suspicious activity should stop
- Systems should be isolated
- Evidence should be preserved

### Runbook: Credential Rotation

**Purpose**: Rotate all credentials and secrets

**Steps**:

```bash
#!/bin/bash
# rotate-credentials.sh

echo "=== Credential Rotation ==="

# 1. Generate new credentials
echo "Generating new credentials..."
NEW_TOKEN=$(openssl rand -hex 32)
NEW_PASSWORD=$(openssl rand -base64 32)

# 2. Update Kubernetes secrets
echo "Updating Kubernetes secrets..."
kubectl create secret generic mcp-credentials-new \
  --from-literal=token=$NEW_TOKEN \
  --from-literal=password=$NEW_PASSWORD \
  -n mcp-system

# 3. Update deployments to use new secrets
kubectl patch deployment mcp-gateway -n mcp-system -p '
{
  "spec": {
    "template": {
      "spec": {
        "containers": [{
          "name": "gateway",
          "envFrom": [{
            "secretRef": {
              "name": "mcp-credentials-new"
            }
          }]
        }]
      }
    }
  }
}'

# 4. Rolling restart
kubectl rollout restart deployment mcp-gateway mcp-router -n mcp-system
kubectl rollout status deployment mcp-gateway -n mcp-system

# 5. Revoke old credentials
redis-cli DEL $(redis-cli --scan --pattern "session:*")

# 6. Delete old secret
kubectl delete secret mcp-credentials -n mcp-system
kubectl patch secret mcp-credentials-new -n mcp-system \
  -p '{"metadata":{"name":"mcp-credentials"}}'

echo "Credential rotation completed"
```

## Troubleshooting Guides

### Runbook: Connection Issues

**Purpose**: Diagnose and fix connection problems

**Steps**:

```bash
#!/bin/bash
# troubleshoot-connections.sh

echo "=== Connection Troubleshooting ==="

# 1. Test basic connectivity
echo "Testing network connectivity..."
kubectl run test-pod --image=nicolaka/netshoot -it --rm -- bash -c "
  nslookup mcp-gateway
  nc -zv mcp-gateway 8080
  curl -v http://mcp-gateway:8080/health
"

# 2. Check service endpoints
echo "Checking service endpoints..."
kubectl get endpoints -n mcp-system

# 3. Verify network policies
echo "Network policies:"
kubectl get networkpolicy -n mcp-system

# 4. Check ingress
echo "Ingress status:"
kubectl get ingress -n mcp-system
kubectl describe ingress mcp-ingress -n mcp-system

# 5. Test from different locations
for node in $(kubectl get nodes -o name); do
  echo "Testing from $node..."
  kubectl debug $node -it --image=nicolaka/netshoot -- \
    curl -s -o /dev/null -w "%{http_code}" http://mcp-gateway.mcp-system:8080/health
done

# 6. Check DNS resolution
kubectl exec -n mcp-system deployment/mcp-gateway -- nslookup kubernetes.default

echo "Connection troubleshooting completed"
```

### Runbook: Performance Degradation

**Purpose**: Identify and resolve performance issues

**Steps**:

```bash
#!/bin/bash
# troubleshoot-performance.sh

echo "=== Performance Troubleshooting ==="

# 1. Check resource utilization
echo "Resource utilization:"
kubectl top nodes
kubectl top pods -n mcp-system

# 2. Analyze slow queries
echo "Slow operations:"
redis-cli SLOWLOG GET 10

# 3. Check for throttling
kubectl get --raw /apis/metrics.k8s.io/v1beta1/namespaces/mcp-system/pods | \
  jq '.items[] | select(.containers[].usage.cpu > .containers[].requests.cpu)'

# 4. Review recent changes
kubectl rollout history deployment -n mcp-system

# 5. Profile the application
POD=$(kubectl get pod -n mcp-system -l app=mcp-gateway -o jsonpath='{.items[0].metadata.name}')
kubectl exec $POD -n mcp-system -- \
  curl -s http://localhost:6060/debug/pprof/profile?seconds=30 > cpu.prof

# 6. Check for memory leaks
kubectl exec $POD -n mcp-system -- \
  curl -s http://localhost:6060/debug/pprof/heap > heap.prof

echo "Performance analysis completed. Review profiles for details."
```

## Emergency Procedures

### Runbook: Complete Service Failure

**Purpose**: Recover from complete service failure

**Steps**:

```bash
#!/bin/bash
# emergency-recovery.sh

echo "=== EMERGENCY: Complete Service Recovery ==="

# 1. Assess the situation
echo "System status:"
kubectl get all -n mcp-system
kubectl get nodes
kubectl get events -n mcp-system --sort-by='.lastTimestamp' | head -20

# 2. Attempt quick recovery
echo "Attempting quick recovery..."
kubectl delete pods --all -n mcp-system
sleep 30

# 3. If still down, restore from backup
if ! curl -sf http://gateway:8080/health; then
  echo "Quick recovery failed, restoring from backup..."
  ./scripts/disaster-recovery.sh auto latest
fi

# 4. If backup restore fails, rebuild
if ! curl -sf http://gateway:8080/health; then
  echo "Backup restore failed, rebuilding services..."
  kubectl delete namespace mcp-system
  kubectl create namespace mcp-system
  kubectl apply -f deployments/emergency-minimal.yaml
fi

# 5. Gradually restore full service
echo "Restoring full service..."
kubectl apply -f deployments/kubernetes/

# 6. Verify recovery
./health-check.sh

echo "Emergency recovery completed"
```

### Runbook: Data Corruption Recovery

**Purpose**: Recover from data corruption

**Steps**:

```bash
#!/bin/bash
# recover-corruption.sh

echo "=== Data Corruption Recovery ==="

# 1. Stop all writes
echo "Stopping all writes..."
kubectl scale deployment mcp-gateway mcp-router -n mcp-system --replicas=0

# 2. Identify corruption extent
echo "Analyzing corruption..."
redis-cli --scan | while read key; do
  if ! redis-cli GET "$key" > /dev/null 2>&1; then
    echo "Corrupted key: $key"
    echo "$key" >> corrupted-keys.txt
  fi
done

# 3. Backup corrupted data for analysis
tar -czf corrupted-data-$(date +%s).tar.gz /var/lib/redis/

# 4. Restore from clean backup
echo "Restoring from backup..."
LATEST_BACKUP=$(aws s3 ls s3://mcp-backups/daily/ | tail -1 | awk '{print $4}')
aws s3 cp s3://mcp-backups/daily/$LATEST_BACKUP /tmp/
./scripts/restore-data.sh redis /tmp/$LATEST_BACKUP

# 5. Replay recent transactions if available
if [ -f /var/log/mcp-bridge/transaction.log ]; then
  echo "Replaying recent transactions..."
  ./scripts/replay-transactions.sh /var/log/mcp-bridge/transaction.log
fi

# 6. Restart services
kubectl scale deployment mcp-gateway mcp-router -n mcp-system --replicas=3

# 7. Verify data integrity
./scripts/verify-data-integrity.sh

echo "Data corruption recovery completed"
```

## Runbook Index

### By Category

**Service Management**
- [Service Health Check](#runbook-service-health-check)
- [Service Restart](#runbook-service-restart)
- [Scale Service](#runbook-scale-service)

**Incident Response**
- [High Memory Usage](#runbook-high-memory-usage)
- [Service Unavailable](#runbook-service-unavailable)
- [Security Incident](#runbook-security-incident-response)

**Maintenance**
- [Certificate Renewal](#runbook-certificate-renewal)
- [Database Maintenance](#runbook-database-maintenance)
- [Credential Rotation](#runbook-credential-rotation)

**Performance**
- [Gateway Performance Tuning](#runbook-optimize-gateway-performance)
- [Cache Optimization](#runbook-cache-optimization)
- [Performance Degradation](#runbook-performance-degradation)

**Emergency**
- [Complete Service Failure](#runbook-complete-service-failure)
- [Data Corruption Recovery](#runbook-data-corruption-recovery)

### By Urgency

**Critical (Immediate Response)**
- Complete Service Failure
- Security Incident
- Data Corruption

**High (< 1 hour)**
- Service Unavailable
- High Memory Usage
- Performance Degradation

**Medium (< 4 hours)**
- Certificate Renewal (if expiring soon)
- Connection Issues

**Low (Scheduled)**
- Database Maintenance
- Performance Tuning
- Credential Rotation

## Best Practices

### Runbook Development

1. **Keep runbooks current**: Update after every incident
2. **Test regularly**: Run through procedures monthly
3. **Version control**: Track all changes in Git
4. **Peer review**: Have team review new runbooks
5. **Automation**: Convert manual steps to scripts

### During Incidents

1. **Follow the runbook**: Don't skip steps
2. **Document actions**: Log everything you do
3. **Communicate status**: Update stakeholders regularly
4. **Escalate when needed**: Don't hesitate to get help
5. **Post-mortem**: Always conduct after incidents

### Maintenance Windows

1. **Schedule in advance**: Give users notice
2. **Have rollback plan**: Be ready to revert
3. **Test in staging**: Never test in production first
4. **Monitor closely**: Watch metrics during changes
5. **Document changes**: Update runbooks after

## Support Information

### Escalation Path

1. **Level 1**: On-call engineer - Basic runbooks
2. **Level 2**: Senior engineer - Complex issues
3. **Level 3**: Platform team - Infrastructure issues
4. **Level 4**: Development team - Code fixes required

### Contacts

- **On-Call**: Via PagerDuty
- **Platform Team**: platform@company.com
- **Security Team**: security@company.com
- **Vendor Support**: Check vendor portal

### Resources

- **Monitoring**: Grafana dashboards
- **Logs**: Elasticsearch/Kibana
- **Metrics**: Prometheus
- **Documentation**: Internal wiki
- **Automation**: Jenkins/GitLab CI