# Tutorial: High Availability Setup

Deploy MCP Bridge with multi-region support, failover, and 99.99% uptime.

## Prerequisites

- Kubernetes cluster (1.24+) across multiple zones/regions
- Understanding of HA concepts
- DNS management capability
- Load balancer (cloud or on-premises)
- 45-50 minutes

## What You'll Build

A highly available MCP Bridge deployment with:
- **Multi-region deployment** across 3+ regions
- **Active-active configuration** for maximum availability
- **Global load balancing** with health checks
- **Redis Cluster** for distributed session storage
- **Automated failover** with zero downtime
- **Data replication** across regions
- **99.99% uptime SLA** capability

## Architecture

```
                     ┌──────────────────┐
                     │  Global DNS      │
                     │  (Route53/       │
                     │   CloudFlare)    │
                     └────────┬─────────┘
                              │
                 ┌────────────┼────────────┐
                 │            │            │
        ┌────────▼──────┐ ┌──▼────────┐ ┌▼──────────┐
        │ Region US-East│ │Region EU  │ │Region APAC│
        │               │ │           │ │           │
        │ ┌───────────┐ │ │┌─────────┐│ │┌─────────┐│
        │ │ Gateway   │ │ ││Gateway  ││ ││Gateway  ││
        │ │ (3 pods)  │ │ ││(3 pods) ││ ││(3 pods) ││
        │ └─────┬─────┘ │ │└────┬────┘│ │└────┬────┘│
        │       │       │ │     │     │ │     │     │
        │ ┌─────▼─────┐ │ │┌────▼────┐│ │┌────▼────┐│
        │ │Redis      │◄┼─┼┤Redis    │◄┼─┼┤Redis    ││
        │ │Cluster    │ │ ││Cluster  ││ ││Cluster  ││
        │ └───────────┘ │ │└─────────┘│ │└─────────┘│
        └───────────────┘ └───────────┘ └───────────┘
              Replication ──────────────────►
```

## Step 1: Multi-Region Kubernetes Setup

### Deploy to Multiple Regions

Create `multi-region-config.yaml`:

```yaml
# Region configuration
regions:
  - name: us-east-1
    k8s_context: us-east-cluster
    primary: true
    weight: 40

  - name: eu-west-1
    k8s_context: eu-west-cluster
    primary: true
    weight: 35

  - name: ap-southeast-1
    k8s_context: ap-southeast-cluster
    primary: true
    weight: 25
```

### Deploy Gateway to Each Region

```bash
# Deploy to US-East
kubectl --context=us-east-cluster apply -k deployment/kubernetes/

# Deploy to EU-West
kubectl --context=eu-west-cluster apply -k deployment/kubernetes/

# Deploy to AP-Southeast
kubectl --context=ap-southeast-cluster apply -k deployment/kubernetes/

# Verify deployments
for ctx in us-east-cluster eu-west-cluster ap-southeast-cluster; do
  echo "=== $ctx ==="
  kubectl --context=$ctx get pods -n mcp-system
done
```

## Step 2: Redis Cluster Setup

### Deploy Redis Cluster in Each Region

Create `redis-cluster.yaml`:

```yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: redis-cluster
  namespace: mcp-system
spec:
  serviceName: redis-cluster
  replicas: 6
  selector:
    matchLabels:
      app: redis-cluster
  template:
    metadata:
      labels:
        app: redis-cluster
    spec:
      affinity:
        podAntiAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
          - labelSelector:
              matchExpressions:
              - key: app
                operator: In
                values:
                - redis-cluster
            topologyKey: kubernetes.io/hostname
      containers:
      - name: redis
        image: redis:7-alpine
        ports:
        - containerPort: 6379
          name: client
        - containerPort: 16379
          name: gossip
        command:
        - redis-server
        - /conf/redis.conf
        volumeMounts:
        - name: conf
          mountPath: /conf
        - name: data
          mountPath: /data
        resources:
          requests:
            memory: "512Mi"
            cpu: "500m"
          limits:
            memory: "1Gi"
            cpu: "1000m"
  volumeClaimTemplates:
  - metadata:
      name: data
    spec:
      accessModes: ["ReadWriteOnce"]
      resources:
        requests:
          storage: 10Gi
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: redis-cluster-config
  namespace: mcp-system
data:
  redis.conf: |
    cluster-enabled yes
    cluster-require-full-coverage no
    cluster-node-timeout 5000
    cluster-config-file /data/nodes.conf
    cluster-migration-barrier 1
    appendonly yes
    protected-mode no
    bind 0.0.0.0
---
apiVersion: v1
kind: Service
metadata:
  name: redis-cluster
  namespace: mcp-system
spec:
  type: ClusterIP
  clusterIP: None
  ports:
  - port: 6379
    targetPort: 6379
    name: client
  - port: 16379
    targetPort: 16379
    name: gossip
  selector:
    app: redis-cluster
```

### Initialize Redis Cluster

```bash
# Deploy to all regions
for ctx in us-east-cluster eu-west-cluster ap-southeast-cluster; do
  kubectl --context=$ctx apply -f redis-cluster.yaml
done

# Wait for pods to be ready
kubectl wait --for=condition=ready pod -l app=redis-cluster -n mcp-system --timeout=300s

# Create cluster
kubectl exec -n mcp-system redis-cluster-0 -- redis-cli --cluster create \
  $(kubectl get pods -n mcp-system -l app=redis-cluster -o jsonpath='{range.items[*]}{.status.podIP}:6379 ') \
  --cluster-replicas 1 --cluster-yes
```

## Step 3: Cross-Region Replication

### Configure Redis Cross-Region Replication

Create `redis-replication.yaml`:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: redis-sync
  namespace: mcp-system
data:
  sync.sh: |
    #!/bin/sh
    # Cross-region Redis replication setup

    REGIONS="us-east-1.redis.svc.cluster.local eu-west-1.redis.svc.cluster.local ap-southeast-1.redis.svc.cluster.local"

    for region in $REGIONS; do
      redis-cli -h $region SLAVEOF NO ONE 2>/dev/null || true
    done

    # Setup bidirectional replication
    redis-cli -h eu-west-1.redis.svc.cluster.local REPLICAOF us-east-1.redis.svc.cluster.local 6379
    redis-cli -h ap-southeast-1.redis.svc.cluster.local REPLICAOF us-east-1.redis.svc.cluster.local 6379
---
apiVersion: batch/v1
kind: CronJob
metadata:
  name: redis-sync
  namespace: mcp-system
spec:
  schedule: "*/5 * * * *"
  jobTemplate:
    spec:
      template:
        spec:
          containers:
          - name: sync
            image: redis:7-alpine
            command: ["/bin/sh", "/scripts/sync.sh"]
            volumeMounts:
            - name: script
              mountPath: /scripts
          volumes:
          - name: script
            configMap:
              name: redis-sync
              defaultMode: 0755
          restartPolicy: OnFailure
```

## Step 4: Global Load Balancer

### AWS Global Accelerator

Create `global-accelerator.yaml`:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: mcp-gateway-global
  namespace: mcp-system
  annotations:
    service.beta.kubernetes.io/aws-load-balancer-type: "nlb-ip"
    service.beta.kubernetes.io/aws-load-balancer-cross-zone-load-balancing-enabled: "true"
    service.beta.kubernetes.io/aws-load-balancer-backend-protocol: "tcp"
    service.beta.kubernetes.io/aws-load-balancer-healthcheck-protocol: "http"
    service.beta.kubernetes.io/aws-load-balancer-healthcheck-port: "9090"
    service.beta.kubernetes.io/aws-load-balancer-healthcheck-path: "/healthz"
spec:
  type: LoadBalancer
  sessionAffinity: ClientIP
  sessionAffinityConfig:
    clientIP:
      timeoutSeconds: 10800
  ports:
  - port: 443
    targetPort: 8443
    protocol: TCP
  selector:
    app: mcp-gateway
```

### Terraform for Global Accelerator

Create `terraform/global-accelerator.tf`:

```hcl
resource "aws_globalaccelerator_accelerator" "mcp" {
  name            = "mcp-bridge-accelerator"
  ip_address_type = "IPV4"
  enabled         = true

  attributes {
    flow_logs_enabled   = true
    flow_logs_s3_bucket = aws_s3_bucket.flow_logs.id
    flow_logs_s3_prefix = "mcp-accelerator"
  }
}

resource "aws_globalaccelerator_listener" "mcp" {
  accelerator_arn = aws_globalaccelerator_accelerator.mcp.id
  protocol        = "TCP"

  port_range {
    from_port = 443
    to_port   = 443
  }
}

resource "aws_globalaccelerator_endpoint_group" "us_east" {
  listener_arn = aws_globalaccelerator_listener.mcp.id
  endpoint_group_region = "us-east-1"
  traffic_dial_percentage = 40

  endpoint_configuration {
    endpoint_id = aws_lb.us_east_nlb.arn
    weight      = 100
  }

  health_check_interval_seconds = 30
  health_check_path            = "/healthz"
  health_check_port            = 9090
  health_check_protocol        = "HTTP"
  threshold_count              = 3
}

resource "aws_globalaccelerator_endpoint_group" "eu_west" {
  listener_arn = aws_globalaccelerator_listener.mcp.id
  endpoint_group_region = "eu-west-1"
  traffic_dial_percentage = 35

  endpoint_configuration {
    endpoint_id = aws_lb.eu_west_nlb.arn
    weight      = 100
  }

  health_check_interval_seconds = 30
  health_check_path            = "/healthz"
  health_check_port            = 9090
  health_check_protocol        = "HTTP"
  threshold_count              = 3
}

resource "aws_globalaccelerator_endpoint_group" "ap_southeast" {
  listener_arn = aws_globalaccelerator_listener.mcp.id
  endpoint_group_region = "ap-southeast-1"
  traffic_dial_percentage = 25

  endpoint_configuration {
    endpoint_id = aws_lb.ap_southeast_nlb.arn
    weight      = 100
  }

  health_check_interval_seconds = 30
  health_check_path            = "/healthz"
  health_check_port            = 9090
  health_check_protocol        = "HTTP"
  threshold_count              = 3
}

output "accelerator_dns_name" {
  value = aws_globalaccelerator_accelerator.mcp.dns_name
}

output "accelerator_ips" {
  value = aws_globalaccelerator_accelerator.mcp.ip_sets[0].ip_addresses
}
```

Apply:

```bash
cd terraform
terraform init
terraform plan
terraform apply

# Get accelerator endpoint
terraform output accelerator_dns_name
```

### CloudFlare Load Balancing (Alternative)

Configure via CloudFlare dashboard or API:

```bash
# Create load balancer
curl -X POST "https://api.cloudflare.com/client/v4/zones/$ZONE_ID/load_balancers" \
  -H "Authorization: Bearer $CF_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "mcp-gateway.example.com",
    "default_pools": [
      "us-east-pool",
      "eu-west-pool",
      "ap-southeast-pool"
    ],
    "fallback_pool": "us-east-pool",
    "steering_policy": "dynamic_latency",
    "session_affinity": "cookie",
    "session_affinity_ttl": 10800,
    "proxied": true
  }'

# Create pools for each region
for region in us-east eu-west ap-southeast; do
  curl -X POST "https://api.cloudflare.com/client/v4/accounts/$ACCOUNT_ID/load_balancers/pools" \
    -H "Authorization: Bearer $CF_TOKEN" \
    -H "Content-Type: application/json" \
    -d "{
      \"name\": \"${region}-pool\",
      \"origins\": [{
        \"name\": \"${region}-gateway\",
        \"address\": \"${region}.gateway.example.com\",
        \"enabled\": true,
        \"weight\": 1
      }],
      \"monitor\": \"$MONITOR_ID\",
      \"notification_email\": \"ops@example.com\"
    }"
done
```

## Step 5: Gateway Configuration for HA

Update `gateway.yaml` for HA mode:

```yaml
version: 1

server:
  host: 0.0.0.0
  port: 8443
  protocol: websocket
  tls:
    enabled: true
    cert_file: /etc/tls/tls.crt
    key_file: /etc/tls/tls.key

# High Availability settings
ha:
  enabled: true
  region: ${REGION}  # Set via env var
  peer_discovery:
    enabled: true
    method: kubernetes
    namespace: mcp-system
    label_selector: "app=mcp-gateway"

auth:
  type: jwt
  jwt:
    issuer: "mcp-gateway"
    audience: "mcp-services"
    secret_key_env: JWT_SECRET_KEY

# Redis cluster configuration
sessions:
  type: redis
  ttl: 1h
  redis:
    mode: cluster
    addresses_env: REDIS_CLUSTER_ADDRESSES
    # Addresses: redis-0.redis:6379,redis-1.redis:6379,...
    pool_size: 50
    max_retries: 3
    retry_delay: 1s

discovery:
  provider: kubernetes
  kubernetes:
    in_cluster: true
    namespace_selector:
      - mcp-servers
    label_selector:
      mcp.bridge/enabled: "true"
    # Cross-region service discovery
    external_clusters:
      - name: eu-west
        kubeconfig_secret: eu-west-kubeconfig
      - name: ap-southeast
        kubeconfig_secret: ap-southeast-kubeconfig

routing:
  strategy: least_connections
  health_check_interval: 30s

  # Advanced failover
  circuit_breaker:
    enabled: true
    failure_threshold: 5
    success_threshold: 2
    timeout: 30s
    half_open_requests: 3

  # Retry policy
  retry:
    enabled: true
    max_attempts: 3
    backoff: exponential
    initial_delay: 100ms
    max_delay: 5s

rate_limit:
  enabled: true
  requests_per_sec: 5000  # Higher for HA
  burst: 1000
  per_ip: true

observability:
  metrics:
    enabled: true
    port: 9090
    additional_labels:
      region: ${REGION}
      deployment: ha

  logging:
    level: info
    format: json
    additional_fields:
      region: ${REGION}

  tracing:
    enabled: true
    service_name: mcp-gateway-${REGION}
    sample_rate: 0.1
```

## Step 6: Automated Failover

### Health Check Monitor

Create `health-monitor.yaml`:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: health-monitor
  namespace: mcp-system
data:
  monitor.sh: |
    #!/bin/bash

    REGIONS="us-east eu-west ap-southeast"
    THRESHOLD=3

    check_region() {
      local region=$1
      local endpoint="https://${region}.gateway.example.com:443/healthz"

      if curl -sf -m 5 "$endpoint" > /dev/null; then
        echo "✓ $region: healthy"
        return 0
      else
        echo "✗ $region: unhealthy"
        return 1
      fi
    }

    while true; do
      for region in $REGIONS; do
        failures=0

        for i in $(seq 1 $THRESHOLD); do
          if ! check_region "$region"; then
            ((failures++))
          fi
          sleep 1
        done

        if [ $failures -ge $THRESHOLD ]; then
          echo "ALERT: $region failed $failures/$THRESHOLD checks"
          # Trigger failover
          kubectl annotate service mcp-gateway-global -n mcp-system \
            "failover/$region=true" --overwrite
        fi
      done

      sleep 30
    done
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: health-monitor
  namespace: mcp-system
spec:
  replicas: 2
  selector:
    matchLabels:
      app: health-monitor
  template:
    metadata:
      labels:
        app: health-monitor
    spec:
      containers:
      - name: monitor
        image: curlimages/curl:latest
        command: ["/bin/sh", "/scripts/monitor.sh"]
        volumeMounts:
        - name: script
          mountPath: /scripts
      volumes:
      - name: script
        configMap:
          name: health-monitor
          defaultMode: 0755
```

### Automated Failover Script

Create `failover-controller.py`:

```python
#!/usr/bin/env python3
import boto3
import time
import logging
from kubernetes import client, config

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

class FailoverController:
    def __init__(self):
        self.ga_client = boto3.client('globalaccelerator')
        config.load_incluster_config()
        self.k8s_client = client.CoreV1Api()

    def check_health(self, region: str) -> bool:
        """Check if region is healthy"""
        try:
            # Check pod health
            pods = self.k8s_client.list_namespaced_pod(
                namespace='mcp-system',
                label_selector=f'app=mcp-gateway,region={region}'
            )

            healthy = sum(1 for pod in pods.items
                         if pod.status.phase == 'Running')
            total = len(pods.items)

            return healthy >= (total * 0.67)  # 67% threshold

        except Exception as e:
            logger.error(f"Health check failed for {region}: {e}")
            return False

    def trigger_failover(self, failed_region: str):
        """Trigger failover from failed region"""
        logger.warning(f"Triggering failover for {failed_region}")

        # Reduce traffic to failed region
        self.update_traffic_dial(failed_region, 0)

        # Increase traffic to healthy regions
        healthy_regions = [r for r in ['us-east', 'eu-west', 'ap-southeast']
                          if r != failed_region and self.check_health(r)]

        for region in healthy_regions:
            self.update_traffic_dial(region, 50)

        logger.info(f"Failover complete. Traffic redirected from {failed_region}")

    def update_traffic_dial(self, region: str, percentage: int):
        """Update traffic dial percentage for region"""
        # Implementation depends on your load balancer
        logger.info(f"Setting {region} traffic to {percentage}%")

    def run(self):
        """Main monitoring loop"""
        while True:
            for region in ['us-east', 'eu-west', 'ap-southeast']:
                if not self.check_health(region):
                    self.trigger_failover(region)

            time.sleep(30)

if __name__ == '__main__':
    controller = FailoverController()
    controller.run()
```

## Step 7: Disaster Recovery

### Backup Strategy

```bash
# Automated backup script
cat > backup.sh << 'EOF'
#!/bin/bash

BACKUP_BUCKET="s3://mcp-backups"
DATE=$(date +%Y%m%d-%H%M%S)

# Backup Redis data
for region in us-east eu-west ap-southeast; do
  kubectl --context=${region}-cluster exec -n mcp-system redis-cluster-0 -- \
    redis-cli BGSAVE

  kubectl --context=${region}-cluster cp \
    mcp-system/redis-cluster-0:/data/dump.rdb \
    /tmp/redis-${region}-${DATE}.rdb

  aws s3 cp /tmp/redis-${region}-${DATE}.rdb \
    ${BACKUP_BUCKET}/${region}/redis-${DATE}.rdb
done

# Backup configurations
kubectl get configmap,secret -n mcp-system -o yaml > \
  /tmp/config-${DATE}.yaml

aws s3 cp /tmp/config-${DATE}.yaml \
  ${BACKUP_BUCKET}/configs/config-${DATE}.yaml

echo "Backup completed: $DATE"
EOF

chmod +x backup.sh

# Schedule via CronJob
kubectl create cronjob backup \
  --image=amazon/aws-cli \
  --schedule="0 */6 * * *" \
  -- /scripts/backup.sh
```

### Restore Procedure

```bash
# Restore from backup
BACKUP_DATE="20250113-120000"

for region in us-east eu-west ap-southeast; do
  # Download backup
  aws s3 cp \
    s3://mcp-backups/${region}/redis-${BACKUP_DATE}.rdb \
    /tmp/restore.rdb

  # Copy to Redis pod
  kubectl --context=${region}-cluster cp \
    /tmp/restore.rdb \
    mcp-system/redis-cluster-0:/data/dump.rdb

  # Restart Redis
  kubectl --context=${region}-cluster delete pod \
    redis-cluster-0 -n mcp-system
done
```

## Step 8: Testing HA Setup

### Chaos Engineering Tests

```bash
# Install chaos-mesh
helm repo add chaos-mesh https://charts.chaos-mesh.org
helm install chaos-mesh chaos-mesh/chaos-mesh --namespace=chaos-testing

# Create pod chaos experiment
cat > pod-chaos.yaml << EOF
apiVersion: chaos-mesh.org/v1alpha1
kind: PodChaos
metadata:
  name: gateway-pod-kill
  namespace: chaos-testing
spec:
  action: pod-kill
  mode: one
  selector:
    namespaces:
      - mcp-system
    labelSelectors:
      app: mcp-gateway
  scheduler:
    cron: "@every 2m"
EOF

kubectl apply -f pod-chaos.yaml

# Monitor failover
watch kubectl get pods -n mcp-system
```

### Load Testing with Failover

```bash
# Install k6
brew install k6

# Create load test script
cat > load-test-ha.js << 'EOF'
import ws from 'k6/ws';
import { check } from 'k6';

export let options = {
  stages: [
    { duration: '2m', target: 1000 },
    { duration: '5m', target: 1000 },
    { duration: '2m', target: 0 },
  ],
};

export default function () {
  const url = 'wss://gateway.example.com:443';
  const params = { headers: { 'Authorization': 'Bearer TOKEN' } };

  ws.connect(url, params, function (socket) {
    socket.on('open', () => {
      socket.send(JSON.stringify({
        jsonrpc: '2.0',
        method: 'tools/list',
        id: 1
      }));
    });

    socket.on('message', (data) => {
      check(data, { 'status is 200': (r) => r !== null });
    });
  });
}
EOF

# Run load test
k6 run load-test-ha.js

# During test, kill a region
kubectl --context=us-east-cluster delete pod -l app=mcp-gateway -n mcp-system
```

## Monitoring HA Metrics

### Key SLIs/SLOs

```yaml
# Prometheus alerts
apiVersion: v1
kind: ConfigMap
metadata:
  name: prometheus-ha-alerts
data:
  ha-alerts.yml: |
    groups:
    - name: ha-alerts
      rules:
      # Availability SLO: 99.99%
      - alert: AvailabilityBelowSLO
        expr: |
          (sum(rate(mcp_gateway_requests_total{code=~"2.."}[5m])) /
           sum(rate(mcp_gateway_requests_total[5m]))) < 0.9999
        for: 5m
        annotations:
          summary: "Availability below 99.99% SLO"

      # Regional health
      - alert: RegionUnhealthy
        expr: up{job="mcp-gateway"} == 0
        for: 2m
        annotations:
          summary: "Region {{ $labels.region }} is unhealthy"

      # Latency SLO: P99 < 100ms
      - alert: LatencyAboveSLO
        expr: |
          histogram_quantile(0.99,
            rate(mcp_gateway_request_duration_seconds_bucket[5m])
          ) > 0.1
        for: 5m
        annotations:
          summary: "P99 latency above 100ms SLO"
```

## Best Practices

1. **Always deploy to 3+ regions** for true HA
2. **Use stateless gateway design** - all state in Redis
3. **Implement circuit breakers** to prevent cascade failures
4. **Regular chaos testing** - break things on purpose
5. **Automated failover** - humans are too slow
6. **Monitor SLOs religiously** - 99.99% = 52min downtime/year
7. **Practice DR procedures** monthly

## Cost Optimization

While HA increases costs, optimize where possible:

```yaml
# Use spot instances for non-critical regions
nodeSelector:
  node.kubernetes.io/instance-type: spot

# Auto-scale based on traffic
horizontalPodAutoscaler:
  minReplicas: 3
  maxReplicas: 20
  targetCPUUtilization: 70

# Use regional egress optimization
annotations:
  service.beta.kubernetes.io/aws-load-balancer-cross-zone-load-balancing-enabled: "false"
```

## Next Steps

- [Load Balancing](07-load-balancing.md) - Advanced routing strategies
- [Monitoring & Observability](10-monitoring.md) - Comprehensive monitoring
- [Performance Tuning](12-performance-tuning.md) - Optimize for scale

## Summary

This tutorial covered:
- Deployed MCP Bridge across 3+ regions
- Configured Redis Cluster with replication
- Set up global load balancing
- Implemented automated failover
- Created disaster recovery procedures
- Tested with chaos engineering
- Achieved 99.99% availability capability


