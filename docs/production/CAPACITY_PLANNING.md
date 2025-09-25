# Capacity Planning Guidelines

Comprehensive capacity planning guide for MCP Bridge production deployments.

## Table of Contents

- [Overview](#overview)
- [Baseline Metrics](#baseline-metrics)
- [Sizing Guidelines](#sizing-guidelines)
- [Growth Projections](#growth-projections)
- [Resource Calculation](#resource-calculation)
- [Scaling Strategies](#scaling-strategies)
- [Monitoring and Alerts](#monitoring-and-alerts)
- [Cost Optimization](#cost-optimization)
- [Planning Tools](#planning-tools)

## Overview

This guide provides methodologies and tools for planning MCP Bridge capacity to meet current and future demands while optimizing costs and performance.

### Key Principles

1. **Data-Driven Decisions**: Base capacity on actual metrics
2. **Proactive Planning**: Plan for growth before hitting limits
3. **Cost Efficiency**: Right-size resources to avoid waste
4. **High Availability**: Ensure redundancy and failover capacity
5. **Performance First**: Maintain SLAs under peak load

## Baseline Metrics

### Current System Metrics

```yaml
# baseline-metrics.yaml
system_metrics:
  gateway:
    requests_per_second: 1000
    avg_response_time_ms: 50
    p99_response_time_ms: 200
    concurrent_connections: 5000
    cpu_usage_percent: 40
    memory_usage_gb: 2.5
    network_bandwidth_mbps: 100
    
  router:
    connections_per_instance: 1000
    messages_per_second: 500
    cpu_usage_percent: 30
    memory_usage_gb: 1.5
    network_bandwidth_mbps: 50
    
  redis:
    operations_per_second: 10000
    memory_usage_gb: 8
    connection_count: 200
    dataset_size_gb: 6
    
  infrastructure:
    node_count: 6
    total_cpu_cores: 48
    total_memory_gb: 192
    storage_iops: 10000
    network_bandwidth_gbps: 10
```

### Performance Requirements

| Metric | Current | Target | Peak |
|--------|---------|--------|------|
| **Requests/sec** | 1,000 | 5,000 | 10,000 |
| **Concurrent Users** | 1,000 | 10,000 | 25,000 |
| **Response Time (p50)** | 25ms | 20ms | 30ms |
| **Response Time (p99)** | 200ms | 150ms | 500ms |
| **Availability** | 99.9% | 99.95% | 99.99% |
| **Data Retention** | 7 days | 30 days | 90 days |

### Resource Utilization Targets

```yaml
utilization_targets:
  cpu:
    normal: 40-60%     # Normal operations
    peak: 70-80%        # Peak load
    max: 85%            # Maximum before scaling
  
  memory:
    normal: 50-70%
    peak: 75-85%
    max: 90%
  
  network:
    normal: 30-50%
    peak: 60-70%
    max: 80%
  
  storage:
    normal: 40-60%
    peak: 70-80%
    max: 85%
```

## Sizing Guidelines

### Gateway Sizing

```python
#!/usr/bin/env python3
# gateway-sizing.py

def calculate_gateway_resources(requests_per_second, concurrent_connections):
    """Calculate required Gateway resources"""
    
    # CPU calculation (1 core per 500 RPS)
    cpu_cores = max(2, requests_per_second / 500)
    
    # Memory calculation (1GB base + 1MB per 10 connections)
    memory_gb = 1 + (concurrent_connections / 10 / 1000)
    
    # Instance count for HA (minimum 3)
    instance_count = max(3, int(cpu_cores / 4))  # 4 cores per instance
    
    # Network bandwidth (1Mbps per 10 RPS)
    bandwidth_mbps = requests_per_second / 10
    
    return {
        "cpu_cores_total": round(cpu_cores, 1),
        "memory_gb_total": round(memory_gb, 1),
        "instance_count": instance_count,
        "cpu_per_instance": round(cpu_cores / instance_count, 1),
        "memory_per_instance": round(memory_gb / instance_count, 1),
        "bandwidth_mbps": round(bandwidth_mbps, 1)
    }

# Example calculations
for rps in [1000, 5000, 10000]:
    resources = calculate_gateway_resources(rps, rps * 5)
    print(f"RPS {rps}: {resources}")
```

### Router Sizing

```python
#!/usr/bin/env python3
# router-sizing.py

def calculate_router_resources(client_connections, messages_per_second):
    """Calculate required Router resources"""
    
    # CPU calculation (1 core per 1000 connections)
    cpu_cores = max(1, client_connections / 1000)
    
    # Memory calculation (512MB base + 1MB per 100 connections)
    memory_gb = 0.5 + (client_connections / 100 / 1000)
    
    # Instance count based on connections
    instance_count = max(2, int(client_connections / 5000))
    
    # Network bandwidth (0.1Mbps per connection average)
    bandwidth_mbps = client_connections * 0.1
    
    return {
        "cpu_cores_total": round(cpu_cores, 1),
        "memory_gb_total": round(memory_gb, 1),
        "instance_count": instance_count,
        "connections_per_instance": int(client_connections / instance_count),
        "bandwidth_mbps": round(bandwidth_mbps, 1)
    }

# Example calculations
for connections in [1000, 10000, 25000]:
    resources = calculate_router_resources(connections, connections * 0.5)
    print(f"Connections {connections}: {resources}")
```

### Redis Sizing

```python
#!/usr/bin/env python3
# redis-sizing.py

def calculate_redis_resources(session_count, cache_size_gb, ops_per_second):
    """Calculate required Redis resources"""
    
    # Memory calculation
    session_memory_gb = session_count * 0.001  # 1KB per session average
    total_memory_gb = session_memory_gb + cache_size_gb
    
    # Add 50% overhead for Redis operations
    memory_with_overhead = total_memory_gb * 1.5
    
    # CPU calculation (1 core per 10k ops)
    cpu_cores = max(2, ops_per_second / 10000)
    
    # Replica count for HA
    replica_count = 2 if ops_per_second < 50000 else 3
    
    return {
        "memory_gb_primary": round(memory_with_overhead, 1),
        "memory_gb_total": round(memory_with_overhead * (1 + replica_count), 1),
        "cpu_cores": round(cpu_cores, 1),
        "replica_count": replica_count,
        "ops_per_second_max": ops_per_second * 1.5
    }

# Example calculations
print(calculate_redis_resources(10000, 5, 10000))
print(calculate_redis_resources(100000, 20, 50000))
```

### Kubernetes Node Sizing

```yaml
# node-sizing.yaml
node_configurations:
  small:
    instance_type: t3.large
    cpu: 2
    memory_gb: 8
    network_gbps: 5
    use_case: "Development/Testing"
    max_pods: 35
    
  medium:
    instance_type: m5.2xlarge
    cpu: 8
    memory_gb: 32
    network_gbps: 10
    use_case: "Production - Standard"
    max_pods: 58
    
  large:
    instance_type: m5.4xlarge
    cpu: 16
    memory_gb: 64
    network_gbps: 10
    use_case: "Production - High Load"
    max_pods: 234
    
  xlarge:
    instance_type: m5.8xlarge
    cpu: 32
    memory_gb: 128
    network_gbps: 10
    use_case: "Production - Very High Load"
    max_pods: 234

cluster_sizing:
  small:
    node_count: 3
    node_type: medium
    total_cpu: 24
    total_memory_gb: 96
    estimated_pods: 150
    monthly_cost: "$1,500"
    
  medium:
    node_count: 6
    node_type: large
    total_cpu: 96
    total_memory_gb: 384
    estimated_pods: 1000
    monthly_cost: "$6,000"
    
  large:
    node_count: 10
    node_type: xlarge
    total_cpu: 320
    total_memory_gb: 1280
    estimated_pods: 2000
    monthly_cost: "$20,000"
```

## Growth Projections

### Traffic Growth Model

```python
#!/usr/bin/env python3
# growth-projection.py

import math
from datetime import datetime, timedelta

def project_growth(current_value, growth_rate_monthly, months):
    """Project growth over time"""
    projections = []
    value = current_value
    
    for month in range(months + 1):
        date = datetime.now() + timedelta(days=30 * month)
        projections.append({
            "month": month,
            "date": date.strftime("%Y-%m"),
            "value": round(value),
            "growth_factor": round(value / current_value, 2)
        })
        value *= (1 + growth_rate_monthly)
    
    return projections

# Project different growth scenarios
scenarios = {
    "conservative": 0.05,  # 5% monthly
    "moderate": 0.10,      # 10% monthly
    "aggressive": 0.20     # 20% monthly
}

current_rps = 1000
for scenario, rate in scenarios.items():
    print(f"\n{scenario.upper()} Growth Scenario ({rate*100}% monthly):")
    projections = project_growth(current_rps, rate, 12)
    for p in projections[::3]:  # Show quarterly
        print(f"  Month {p['month']:2}: {p['value']:,} RPS (x{p['growth_factor']})")
```

### Capacity Planning Matrix

```python
#!/usr/bin/env python3
# capacity-matrix.py

def generate_capacity_matrix(base_metrics, growth_scenarios):
    """Generate capacity requirements matrix"""
    
    matrix = []
    for scenario in growth_scenarios:
        for month in [0, 3, 6, 12]:
            growth_factor = (1 + scenario["rate"]) ** month
            
            row = {
                "scenario": scenario["name"],
                "month": month,
                "requests_per_second": int(base_metrics["rps"] * growth_factor),
                "concurrent_users": int(base_metrics["users"] * growth_factor),
                "gateway_instances": max(3, int(growth_factor * 3)),
                "router_instances": max(2, int(growth_factor * 2)),
                "redis_memory_gb": round(base_metrics["redis_gb"] * growth_factor, 1),
                "total_cpu_cores": int(base_metrics["cpu_cores"] * growth_factor),
                "total_memory_gb": int(base_metrics["memory_gb"] * growth_factor),
                "estimated_cost": f"${int(base_metrics['monthly_cost'] * growth_factor):,}"
            }
            matrix.append(row)
    
    return matrix

# Generate matrix
base = {
    "rps": 1000,
    "users": 1000,
    "redis_gb": 8,
    "cpu_cores": 48,
    "memory_gb": 192,
    "monthly_cost": 5000
}

scenarios = [
    {"name": "Conservative", "rate": 0.05},
    {"name": "Moderate", "rate": 0.10},
    {"name": "Aggressive", "rate": 0.20}
]

matrix = generate_capacity_matrix(base, scenarios)
for row in matrix:
    if row["month"] in [0, 6, 12]:
        print(f"{row['scenario']:12} Month {row['month']:2}: "
              f"RPS={row['requests_per_second']:5,} "
              f"CPU={row['total_cpu_cores']:3} "
              f"Mem={row['total_memory_gb']:4}GB "
              f"Cost={row['estimated_cost']}")
```

## Resource Calculation

### Resource Calculator Script

```bash
#!/bin/bash
# resource-calculator.sh

calculate_resources() {
  local RPS=$1
  local USERS=$2
  local RETENTION_DAYS=$3
  
  echo "=== MCP Bridge Resource Calculator ==="
  echo "Input Parameters:"
  echo "  Requests/sec: $RPS"
  echo "  Concurrent Users: $USERS"
  echo "  Data Retention: $RETENTION_DAYS days"
  echo
  
  # Gateway calculations
  GATEWAY_CPU=$(echo "scale=1; $RPS / 500" | bc)
  GATEWAY_MEMORY=$(echo "scale=1; 1 + ($USERS / 10000)" | bc)
  GATEWAY_INSTANCES=$(echo "scale=0; if ($GATEWAY_CPU > 12) $GATEWAY_CPU / 4 else 3" | bc)
  
  echo "Gateway Requirements:"
  echo "  Instances: $GATEWAY_INSTANCES"
  echo "  CPU per instance: $(echo "scale=1; $GATEWAY_CPU / $GATEWAY_INSTANCES" | bc) cores"
  echo "  Memory per instance: $(echo "scale=1; $GATEWAY_MEMORY / $GATEWAY_INSTANCES" | bc) GB"
  echo
  
  # Router calculations
  ROUTER_CPU=$(echo "scale=1; $USERS / 1000" | bc)
  ROUTER_MEMORY=$(echo "scale=1; 0.5 + ($USERS / 1000)" | bc)
  ROUTER_INSTANCES=$(echo "scale=0; if ($USERS > 5000) $USERS / 5000 else 2" | bc)
  
  echo "Router Requirements:"
  echo "  Instances: $ROUTER_INSTANCES"
  echo "  CPU per instance: $(echo "scale=1; $ROUTER_CPU / $ROUTER_INSTANCES" | bc) cores"
  echo "  Memory per instance: $(echo "scale=1; $ROUTER_MEMORY / $ROUTER_INSTANCES" | bc) GB"
  echo
  
  # Redis calculations
  SESSION_SIZE_GB=$(echo "scale=1; $USERS * 0.001" | bc)
  CACHE_SIZE_GB=$(echo "scale=1; $RPS * 0.01" | bc)
  REDIS_MEMORY=$(echo "scale=1; ($SESSION_SIZE_GB + $CACHE_SIZE_GB) * 1.5" | bc)
  
  echo "Redis Requirements:"
  echo "  Memory (primary): $REDIS_MEMORY GB"
  echo "  Memory (with replicas): $(echo "scale=1; $REDIS_MEMORY * 3" | bc) GB"
  echo
  
  # Storage calculations
  LOGS_GB_DAY=$(echo "scale=1; $RPS * 86400 * 0.0001 / 1024" | bc)
  METRICS_GB_DAY=$(echo "scale=1; $RPS * 86400 * 0.00001 / 1024" | bc)
  TOTAL_STORAGE=$(echo "scale=1; ($LOGS_GB_DAY + $METRICS_GB_DAY) * $RETENTION_DAYS" | bc)
  
  echo "Storage Requirements:"
  echo "  Logs per day: $LOGS_GB_DAY GB"
  echo "  Metrics per day: $METRICS_GB_DAY GB"
  echo "  Total storage: $TOTAL_STORAGE GB"
  echo
  
  # Node calculations
  TOTAL_CPU=$(echo "scale=0; $GATEWAY_CPU + $ROUTER_CPU + 4" | bc)
  TOTAL_MEMORY=$(echo "scale=0; $GATEWAY_MEMORY + $ROUTER_MEMORY + $REDIS_MEMORY + 16" | bc)
  NODE_COUNT=$(echo "scale=0; if ($TOTAL_CPU > 48) $TOTAL_CPU / 16 else 3" | bc)
  
  echo "Kubernetes Cluster:"
  echo "  Minimum nodes: $NODE_COUNT"
  echo "  Total CPU needed: $TOTAL_CPU cores"
  echo "  Total Memory needed: $TOTAL_MEMORY GB"
  echo "  Recommended node type: $(
    if [ $TOTAL_CPU -gt 96 ]; then echo "m5.4xlarge"
    elif [ $TOTAL_CPU -gt 48 ]; then echo "m5.2xlarge"
    else echo "m5.xlarge"
    fi
  )"
}

# Example usage
calculate_resources 5000 10000 30
```

### Automated Sizing Recommendation

```python
#!/usr/bin/env python3
# sizing-recommendation.py

import json
import sys

class CapacityPlanner:
    def __init__(self):
        self.instance_types = {
            "t3.medium": {"cpu": 2, "memory": 4, "cost": 30},
            "t3.large": {"cpu": 2, "memory": 8, "cost": 60},
            "m5.large": {"cpu": 2, "memory": 8, "cost": 70},
            "m5.xlarge": {"cpu": 4, "memory": 16, "cost": 140},
            "m5.2xlarge": {"cpu": 8, "memory": 32, "cost": 280},
            "m5.4xlarge": {"cpu": 16, "memory": 64, "cost": 560},
            "c5.2xlarge": {"cpu": 8, "memory": 16, "cost": 250},
            "r5.xlarge": {"cpu": 4, "memory": 32, "cost": 180},
        }
    
    def recommend_deployment(self, requirements):
        """Recommend optimal deployment configuration"""
        
        recommendations = []
        
        # Gateway recommendation
        gateway_cpu_per_instance = requirements["rps"] / 500 / 3  # 3 instances minimum
        gateway_memory_per_instance = 2  # Base 2GB
        
        gateway_type = self._select_instance_type(
            gateway_cpu_per_instance,
            gateway_memory_per_instance
        )
        
        recommendations.append({
            "component": "Gateway",
            "instance_type": gateway_type,
            "instance_count": max(3, int(requirements["rps"] / 2000)),
            "total_cost": self.instance_types[gateway_type]["cost"] * max(3, int(requirements["rps"] / 2000))
        })
        
        # Router recommendation
        router_cpu_per_instance = requirements["users"] / 5000
        router_memory_per_instance = 1 + requirements["users"] / 10000
        
        router_type = self._select_instance_type(
            router_cpu_per_instance,
            router_memory_per_instance
        )
        
        recommendations.append({
            "component": "Router",
            "instance_type": router_type,
            "instance_count": max(2, int(requirements["users"] / 5000)),
            "total_cost": self.instance_types[router_type]["cost"] * max(2, int(requirements["users"] / 5000))
        })
        
        # Redis recommendation
        redis_memory = requirements["users"] * 0.001 + requirements["rps"] * 0.01
        redis_type = self._select_instance_type(2, redis_memory * 1.5)
        
        recommendations.append({
            "component": "Redis",
            "instance_type": redis_type,
            "instance_count": 3,  # Primary + 2 replicas
            "total_cost": self.instance_types[redis_type]["cost"] * 3
        })
        
        return recommendations
    
    def _select_instance_type(self, cpu_needed, memory_needed):
        """Select appropriate instance type"""
        suitable = []
        for name, specs in self.instance_types.items():
            if specs["cpu"] >= cpu_needed and specs["memory"] >= memory_needed:
                suitable.append((name, specs["cost"]))
        
        if suitable:
            return min(suitable, key=lambda x: x[1])[0]
        return "m5.4xlarge"  # Default to large instance
    
    def generate_report(self, requirements):
        """Generate capacity planning report"""
        recommendations = self.recommend_deployment(requirements)
        
        report = {
            "requirements": requirements,
            "recommendations": recommendations,
            "total_monthly_cost": sum(r["total_cost"] for r in recommendations),
            "summary": {
                "total_instances": sum(r["instance_count"] for r in recommendations),
                "total_cpu_cores": sum(
                    self.instance_types[r["instance_type"]]["cpu"] * r["instance_count"]
                    for r in recommendations
                ),
                "total_memory_gb": sum(
                    self.instance_types[r["instance_type"]]["memory"] * r["instance_count"]
                    for r in recommendations
                )
            }
        }
        
        return report

# Example usage
planner = CapacityPlanner()
requirements = {
    "rps": 5000,
    "users": 10000,
    "retention_days": 30
}

report = planner.generate_report(requirements)
print(json.dumps(report, indent=2))
```

## Scaling Strategies

### Horizontal Pod Autoscaling

```yaml
# hpa-configurations.yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: gateway-hpa
  namespace: mcp-system
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: mcp-gateway
  minReplicas: 3
  maxReplicas: 20
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
  - type: Pods
    pods:
      metric:
        name: http_requests_per_second
      target:
        type: AverageValue
        averageValue: "1000"
  behavior:
    scaleDown:
      stabilizationWindowSeconds: 300
      policies:
      - type: Percent
        value: 50
        periodSeconds: 60
    scaleUp:
      stabilizationWindowSeconds: 60
      policies:
      - type: Percent
        value: 100
        periodSeconds: 60
      - type: Pods
        value: 5
        periodSeconds: 60
---
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: router-hpa
  namespace: mcp-system
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: mcp-router
  minReplicas: 2
  maxReplicas: 10
  metrics:
  - type: Resource
    resource:
      name: cpu
      target:
        type: Utilization
        averageUtilization: 60
  - type: Pods
    pods:
      metric:
        name: active_connections
      target:
        type: AverageValue
        averageValue: "1000"
```

### Vertical Pod Autoscaling

```yaml
# vpa-configurations.yaml
apiVersion: autoscaling.k8s.io/v1
kind: VerticalPodAutoscaler
metadata:
  name: gateway-vpa
  namespace: mcp-system
spec:
  targetRef:
    apiVersion: "apps/v1"
    kind: Deployment
    name: mcp-gateway
  updatePolicy:
    updateMode: "Auto"
  resourcePolicy:
    containerPolicies:
    - containerName: gateway
      minAllowed:
        cpu: 500m
        memory: 1Gi
      maxAllowed:
        cpu: 4
        memory: 8Gi
      controlledResources: ["cpu", "memory"]
---
apiVersion: autoscaling.k8s.io/v1
kind: VerticalPodAutoscaler
metadata:
  name: router-vpa
  namespace: mcp-system
spec:
  targetRef:
    apiVersion: "apps/v1"
    kind: Deployment
    name: mcp-router
  updatePolicy:
    updateMode: "Auto"
  resourcePolicy:
    containerPolicies:
    - containerName: router
      minAllowed:
        cpu: 250m
        memory: 512Mi
      maxAllowed:
        cpu: 2
        memory: 4Gi
```

### Cluster Autoscaling

```yaml
# cluster-autoscaler.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: cluster-autoscaler
  namespace: kube-system
spec:
  template:
    spec:
      containers:
      - image: k8s.gcr.io/autoscaling/cluster-autoscaler:v1.21.0
        name: cluster-autoscaler
        command:
        - ./cluster-autoscaler
        - --v=4
        - --stderrthreshold=info
        - --cloud-provider=aws
        - --skip-nodes-with-local-storage=false
        - --expander=least-waste
        - --node-group-auto-discovery=asg:tag=k8s.io/cluster-autoscaler/enabled,k8s.io/cluster-autoscaler/mcp-cluster
        - --balance-similar-node-groups
        - --skip-nodes-with-system-pods=false
        env:
        - name: AWS_REGION
          value: us-west-2
```

## Monitoring and Alerts

### Capacity Monitoring Dashboard

```python
#!/usr/bin/env python3
# capacity-metrics.py

def generate_capacity_queries():
    """Generate Prometheus queries for capacity monitoring"""
    
    queries = {
        "cpu_utilization": """
            100 * (1 - avg(rate(node_cpu_seconds_total{mode="idle"}[5m])))
        """,
        
        "memory_utilization": """
            100 * (1 - (node_memory_MemAvailable_bytes / node_memory_MemTotal_bytes))
        """,
        
        "pod_capacity": """
            sum(kube_node_status_capacity{resource="pods"}) - 
            sum(kube_pod_info)
        """,
        
        "request_rate": """
            sum(rate(http_requests_total[5m])) * 60
        """,
        
        "connection_count": """
            sum(mcp_router_connections_active)
        """,
        
        "redis_memory": """
            redis_memory_used_bytes / redis_memory_max_bytes * 100
        """,
        
        "storage_usage": """
            100 * (node_filesystem_size_bytes - node_filesystem_avail_bytes) / 
            node_filesystem_size_bytes
        """,
        
        "network_bandwidth": """
            sum(rate(node_network_receive_bytes_total[5m]) + 
                rate(node_network_transmit_bytes_total[5m])) * 8
        """
    }
    
    return queries

# Generate Grafana dashboard JSON
dashboard = {
    "dashboard": {
        "title": "MCP Bridge Capacity Planning",
        "panels": []
    }
}

for name, query in generate_capacity_queries().items():
    panel = {
        "title": name.replace("_", " ").title(),
        "targets": [{
            "expr": query.strip(),
            "refId": "A"
        }],
        "type": "graph"
    }
    dashboard["dashboard"]["panels"].append(panel)

print(json.dumps(dashboard, indent=2))
```

### Capacity Alerts

```yaml
# capacity-alerts.yaml
groups:
- name: capacity_alerts
  interval: 30s
  rules:
  - alert: HighCPUUsage
    expr: |
      100 * (1 - avg(rate(node_cpu_seconds_total{mode="idle"}[5m]))) > 80
    for: 10m
    labels:
      severity: warning
      component: infrastructure
    annotations:
      summary: "High CPU usage detected"
      description: "CPU usage is {{ $value }}% - consider scaling up"
  
  - alert: HighMemoryUsage
    expr: |
      100 * (1 - (node_memory_MemAvailable_bytes / node_memory_MemTotal_bytes)) > 85
    for: 10m
    labels:
      severity: warning
      component: infrastructure
    annotations:
      summary: "High memory usage detected"
      description: "Memory usage is {{ $value }}% - consider scaling up"
  
  - alert: PodCapacityNearLimit
    expr: |
      (sum(kube_node_status_capacity{resource="pods"}) - sum(kube_pod_info)) < 20
    for: 5m
    labels:
      severity: critical
      component: kubernetes
    annotations:
      summary: "Pod capacity near limit"
      description: "Only {{ $value }} pod slots remaining"
  
  - alert: StorageCapacityLow
    expr: |
      100 * (node_filesystem_avail_bytes / node_filesystem_size_bytes) < 15
    for: 10m
    labels:
      severity: warning
      component: storage
    annotations:
      summary: "Storage capacity low"
      description: "Only {{ $value }}% storage remaining"
  
  - alert: RequestRateHigh
    expr: |
      sum(rate(http_requests_total[5m])) > 8000
    for: 10m
    labels:
      severity: info
      component: gateway
    annotations:
      summary: "Request rate approaching capacity"
      description: "Current rate: {{ $value }} req/s"
```

## Cost Optimization

### Cost Analysis Script

```python
#!/usr/bin/env python3
# cost-analysis.py

class CostAnalyzer:
    def __init__(self):
        self.instance_costs = {
            "t3.medium": 0.0416,
            "t3.large": 0.0832,
            "m5.large": 0.096,
            "m5.xlarge": 0.192,
            "m5.2xlarge": 0.384,
            "m5.4xlarge": 0.768,
        }
        
        self.storage_costs = {
            "gp3": 0.08,  # per GB-month
            "io2": 0.125,  # per GB-month
            "st1": 0.045,  # per GB-month
        }
        
        self.data_transfer_cost = 0.09  # per GB
    
    def calculate_monthly_cost(self, infrastructure):
        """Calculate total monthly cost"""
        
        # Compute costs
        compute_cost = sum(
            self.instance_costs.get(i["type"], 0) * i["count"] * 730
            for i in infrastructure["instances"]
        )
        
        # Storage costs
        storage_cost = sum(
            self.storage_costs.get(s["type"], 0) * s["size_gb"]
            for s in infrastructure["storage"]
        )
        
        # Data transfer costs
        transfer_cost = infrastructure["data_transfer_gb"] * self.data_transfer_cost
        
        # Additional services
        services_cost = infrastructure.get("additional_services", 0)
        
        total_cost = compute_cost + storage_cost + transfer_cost + services_cost
        
        return {
            "compute": round(compute_cost, 2),
            "storage": round(storage_cost, 2),
            "transfer": round(transfer_cost, 2),
            "services": round(services_cost, 2),
            "total": round(total_cost, 2)
        }
    
    def optimize_costs(self, current_infrastructure):
        """Suggest cost optimizations"""
        
        optimizations = []
        
        # Check for over-provisioned instances
        for instance in current_infrastructure["instances"]:
            if instance["utilization"] < 30:
                smaller_type = self._get_smaller_instance(instance["type"])
                if smaller_type:
                    savings = (self.instance_costs[instance["type"]] - 
                              self.instance_costs[smaller_type]) * instance["count"] * 730
                    optimizations.append({
                        "action": f"Downsize {instance['name']} from {instance['type']} to {smaller_type}",
                        "monthly_savings": round(savings, 2)
                    })
        
        # Check for Reserved Instance opportunities
        for instance in current_infrastructure["instances"]:
            if instance.get("on_demand", True):
                ri_savings = self.instance_costs[instance["type"]] * 0.4 * instance["count"] * 730
                optimizations.append({
                    "action": f"Purchase Reserved Instances for {instance['name']}",
                    "monthly_savings": round(ri_savings, 2)
                })
        
        return optimizations
    
    def _get_smaller_instance(self, instance_type):
        """Get next smaller instance type"""
        sizes = ["medium", "large", "xlarge", "2xlarge", "4xlarge"]
        for i, size in enumerate(sizes):
            if size in instance_type and i > 0:
                return instance_type.replace(size, sizes[i-1])
        return None

# Example usage
analyzer = CostAnalyzer()

current_infra = {
    "instances": [
        {"name": "gateway", "type": "m5.2xlarge", "count": 3, "utilization": 45},
        {"name": "router", "type": "m5.xlarge", "count": 2, "utilization": 25},
        {"name": "redis", "type": "m5.xlarge", "count": 3, "utilization": 60}
    ],
    "storage": [
        {"type": "gp3", "size_gb": 500},
        {"type": "io2", "size_gb": 100}
    ],
    "data_transfer_gb": 1000,
    "additional_services": 200
}

cost_breakdown = analyzer.calculate_monthly_cost(current_infra)
print("Current Monthly Costs:")
print(json.dumps(cost_breakdown, indent=2))

optimizations = analyzer.optimize_costs(current_infra)
print("\nCost Optimization Opportunities:")
for opt in optimizations:
    print(f"- {opt['action']}: Save ${opt['monthly_savings']}/month")
```

### Spot Instance Strategy

```yaml
# spot-instance-config.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: spot-instance-config
  namespace: mcp-system
data:
  strategy: |
    # Spot Instance Usage Strategy
    
    ## Suitable Workloads
    - Stateless services (Gateway)
    - Batch processing jobs
    - Development/testing environments
    - Non-critical replicas
    
    ## Not Suitable For
    - Stateful services (Redis primary)
    - Critical single points of failure
    - Services requiring consistent performance
    
    ## Mixed Instance Policy
    on_demand_base_capacity: 2
    spot_percentage_above_base: 70
    
    ## Spot Pools
    instance_types:
      - m5.large
      - m5a.large
      - m4.large
    
    ## Interruption Handling
    termination_grace_period: 120
    drain_on_spot_termination: true
```

## Planning Tools

### Capacity Planning Spreadsheet Template

```python
#!/usr/bin/env python3
# generate-planning-sheet.py

import pandas as pd
from datetime import datetime, timedelta

def generate_planning_spreadsheet():
    """Generate capacity planning spreadsheet"""
    
    # Create worksheets
    sheets = {}
    
    # Current State worksheet
    current_state = pd.DataFrame({
        "Component": ["Gateway", "Router", "Redis", "Storage"],
        "Current_Instances": [3, 2, 3, 1],
        "CPU_Per_Instance": [4, 2, 2, 0],
        "Memory_GB_Per_Instance": [8, 4, 16, 0],
        "Current_Load_%": [45, 35, 60, 40],
        "Max_Capacity": [3000, 5000, 50000, 1000]
    })
    sheets["Current State"] = current_state
    
    # Growth Projections worksheet
    months = []
    for i in range(13):
        date = datetime.now() + timedelta(days=30*i)
        months.append(date.strftime("%Y-%m"))
    
    growth_projections = pd.DataFrame({
        "Month": months,
        "Conservative_RPS": [1000 * (1.05 ** i) for i in range(13)],
        "Moderate_RPS": [1000 * (1.10 ** i) for i in range(13)],
        "Aggressive_RPS": [1000 * (1.20 ** i) for i in range(13)]
    })
    sheets["Growth Projections"] = growth_projections
    
    # Resource Requirements worksheet
    resource_reqs = pd.DataFrame({
        "RPS": [1000, 2000, 5000, 10000],
        "Gateway_Instances": [3, 4, 6, 10],
        "Router_Instances": [2, 3, 4, 6],
        "Total_CPU_Cores": [24, 36, 64, 120],
        "Total_Memory_GB": [64, 96, 192, 384],
        "Redis_Memory_GB": [8, 12, 24, 48],
        "Storage_TB": [0.5, 1, 2, 5]
    })
    sheets["Resource Requirements"] = resource_reqs
    
    # Cost Projections worksheet
    cost_projections = pd.DataFrame({
        "Scenario": ["Current", "6 Months", "1 Year"],
        "Compute_Cost": [2000, 3000, 5000],
        "Storage_Cost": [200, 300, 500],
        "Network_Cost": [300, 450, 750],
        "Total_Monthly": [2500, 3750, 6250],
        "Annual_Cost": [30000, 45000, 75000]
    })
    sheets["Cost Projections"] = cost_projections
    
    # Write to Excel
    with pd.ExcelWriter('capacity_planning.xlsx') as writer:
        for name, df in sheets.items():
            df.to_excel(writer, sheet_name=name, index=False)
    
    print("Generated capacity_planning.xlsx")

generate_planning_spreadsheet()
```

### Automated Capacity Report

```bash
#!/bin/bash
# generate-capacity-report.sh

echo "=== MCP Bridge Capacity Report ==="
echo "Generated: $(date)"
echo

# Current utilization
echo "## Current Utilization"
kubectl top nodes
echo

kubectl top pods -n mcp-system
echo

# Metrics summary
echo "## Key Metrics (Last 24h)"
curl -s http://prometheus:9090/api/v1/query?query=avg_over_time(up[24h]) | \
  jq -r '.data.result[0].value[1]'
echo

# Growth trends
echo "## Growth Trends"
echo "Requests/sec trend:"
for days in 1 7 30; do
  avg=$(curl -s "http://prometheus:9090/api/v1/query?query=avg_over_time(rate(http_requests_total[${days}d])[5m:])" | \
    jq -r '.data.result[0].value[1]')
  echo "  ${days}d average: $avg"
done
echo

# Capacity recommendations
echo "## Recommendations"
CPU_USAGE=$(kubectl top nodes --no-headers | awk '{sum+=$3} END {print sum/NR}' | sed 's/%//')
if (( $(echo "$CPU_USAGE > 70" | bc -l) )); then
  echo "⚠️  High CPU usage ($CPU_USAGE%) - consider adding nodes"
fi

MEMORY_USAGE=$(kubectl top nodes --no-headers | awk '{sum+=$5} END {print sum/NR}' | sed 's/%//')
if (( $(echo "$MEMORY_USAGE > 80" | bc -l) )); then
  echo "⚠️  High memory usage ($MEMORY_USAGE%) - consider adding memory"
fi

# Future requirements
echo
echo "## Projected Requirements (3 months)"
./resource-calculator.sh 2000 5000 30

echo
echo "Report complete. Review and plan accordingly."
```

## Capacity Planning Checklist

### Monthly Review

- [ ] Review current resource utilization
- [ ] Analyze growth trends
- [ ] Update growth projections
- [ ] Review cost optimization opportunities
- [ ] Update capacity alerts thresholds
- [ ] Test autoscaling policies
- [ ] Review and update documentation

### Quarterly Planning

- [ ] Conduct capacity planning review
- [ ] Update infrastructure roadmap
- [ ] Review and negotiate contracts
- [ ] Plan major scaling events
- [ ] Update disaster recovery capacity
- [ ] Budget review and forecasting
- [ ] Team training on new tools/processes

### Pre-Launch Checklist

- [ ] Load test at expected peak
- [ ] Verify autoscaling triggers
- [ ] Confirm backup capacity
- [ ] Review monitoring and alerts
- [ ] Update runbooks
- [ ] Communicate capacity limits
- [ ] Prepare scaling playbooks

## Best Practices

### Planning Guidelines

1. **Plan for 2x normal load**: Always have headroom
2. **Use percentiles, not averages**: P95/P99 for planning
3. **Include failure scenarios**: N+1 redundancy minimum
4. **Regular reviews**: Monthly metrics, quarterly planning
5. **Automate scaling**: Reduce human intervention

### Cost Management

1. **Right-size regularly**: Review utilization monthly
2. **Use commitment discounts**: RIs for stable workloads
3. **Implement tagging**: Track costs by component
4. **Set budget alerts**: Proactive cost management
5. **Regular optimization**: Quarterly cost reviews

### Performance Management

1. **Set clear SLAs**: Define acceptable performance
2. **Monitor continuously**: Real-time metrics
3. **Test regularly**: Load test quarterly
4. **Plan for peaks**: Holiday/event planning
5. **Document limits**: Know breaking points

## Appendix

### Quick Reference Formulas

```
Gateway Instances = ceiling(RPS / 2000)
Router Instances = ceiling(Concurrent_Users / 5000)
Redis Memory (GB) = (Users * 0.001 + RPS * 0.01) * 1.5
Storage (GB/day) = RPS * 86400 * 0.0001 / 1024
Network Bandwidth (Mbps) = RPS * 0.1 + Users * 0.01
```

### Useful Commands

```bash
# Get current capacity
kubectl describe nodes | grep -A 5 "Allocated resources"

# Check pod resource requests
kubectl describe pod -n mcp-system | grep -A 2 "Requests:"

# View HPA status
kubectl get hpa -n mcp-system --watch

# Check cluster autoscaler logs
kubectl logs -n kube-system deployment/cluster-autoscaler

# Resource usage over time
kubectl top pods -n mcp-system --containers --use-protocol-buffers
```

### Support Resources

- **Capacity Planning Team**: capacity@company.com
- **Infrastructure Team**: infra@company.com
- **Cost Optimization**: finops@company.com
- **Performance Team**: performance@company.com