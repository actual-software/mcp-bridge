#!/bin/bash

# Migration script from version 1.0.0 to 1.1.0
# DESCRIPTION: Adds new configuration options, updates Redis schema, and migrates to new metrics format
# CHANGES:
#   - Add new rate limiting configuration options
#   - Update Redis key structure for session management
#   - Migrate Prometheus metrics to new naming convention
#   - Add new health check endpoints configuration

set -euo pipefail

# Source common migration functions
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/common.sh" 2>/dev/null || true

# Migration function
migrate_1_0_0_to_1_1_0() {
    log "Starting migration from 1.0.0 to 1.1.0..."
    
    # Step 1: Update configuration files
    update_gateway_config
    update_router_config
    
    # Step 2: Migrate Redis data
    migrate_redis_data
    
    # Step 3: Update Kubernetes manifests
    update_kubernetes_manifests
    
    # Step 4: Update monitoring configuration
    update_monitoring_config
    
    log "Migration from 1.0.0 to 1.1.0 completed successfully"
}

# Update gateway configuration
update_gateway_config() {
    local config_file="${PROJECT_ROOT}/configs/gateway.yaml"
    
    if [[ ! -f "$config_file" ]]; then
        warn "Gateway config not found at $config_file"
        return 0
    fi
    
    info "Updating gateway configuration..."
    
    # Backup original config
    cp "$config_file" "${config_file}.bak"
    
    # Add new configuration sections using yq or manual insertion
    if command -v yq &> /dev/null; then
        # Add new rate limiting options
        yq eval '.rate_limit.advanced = {
            "sliding_window": true,
            "sync_interval": "1s",
            "cleanup_interval": "60s"
        }' -i "$config_file"
        
        # Add new health check configuration
        yq eval '.health.checks = {
            "tcp_enabled": true,
            "tcp_port": 8081,
            "check_interval": "10s",
            "timeout": "5s"
        }' -i "$config_file"
        
        # Add new observability options
        yq eval '.observability = {
            "tracing": {
                "enabled": true,
                "provider": "otlp",
                "endpoint": "localhost:4317",
                "sample_rate": 0.1
            },
            "metrics": {
                "histogram_buckets": [0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 5, 10]
            }
        }' -i "$config_file"
    else
        # Manual configuration update
        cat >> "$config_file" << 'EOF'

# Added in version 1.1.0
rate_limit:
  advanced:
    sliding_window: true
    sync_interval: 1s
    cleanup_interval: 60s

health:
  checks:
    tcp_enabled: true
    tcp_port: 8081
    check_interval: 10s
    timeout: 5s

observability:
  tracing:
    enabled: true
    provider: otlp
    endpoint: localhost:4317
    sample_rate: 0.1
  metrics:
    histogram_buckets: [0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 5, 10]
EOF
    fi
    
    log "Gateway configuration updated"
}

# Update router configuration
update_router_config() {
    local config_file="${PROJECT_ROOT}/configs/router.yaml"
    
    if [[ ! -f "$config_file" ]]; then
        warn "Router config not found at $config_file"
        return 0
    fi
    
    info "Updating router configuration..."
    
    # Backup original config
    cp "$config_file" "${config_file}.bak"
    
    if command -v yq &> /dev/null; then
        # Add connection pooling improvements
        yq eval '.gateway_pool.connection_pool = {
            "max_idle_conns": 100,
            "max_open_conns": 200,
            "conn_max_lifetime": "5m",
            "conn_max_idle_time": "1m",
            "health_check_interval": "30s"
        }' -i "$config_file"
        
        # Add retry configuration
        yq eval '.gateway_pool.retry = {
            "max_attempts": 3,
            "initial_interval": "100ms",
            "max_interval": "5s",
            "multiplier": 2.0,
            "randomization_factor": 0.1
        }' -i "$config_file"
    else
        # Manual update
        cat >> "$config_file" << 'EOF'

# Added in version 1.1.0
gateway_pool:
  connection_pool:
    max_idle_conns: 100
    max_open_conns: 200
    conn_max_lifetime: 5m
    conn_max_idle_time: 1m
    health_check_interval: 30s
  retry:
    max_attempts: 3
    initial_interval: 100ms
    max_interval: 5s
    multiplier: 2.0
    randomization_factor: 0.1
EOF
    fi
    
    log "Router configuration updated"
}

# Migrate Redis data structure
migrate_redis_data() {
    if ! command -v redis-cli &> /dev/null; then
        info "Redis CLI not found, skipping Redis migration"
        return 0
    fi
    
    # Check if Redis is configured and accessible
    if ! redis-cli ping > /dev/null 2>&1; then
        info "Redis not accessible, skipping Redis migration"
        return 0
    fi
    
    info "Migrating Redis data structure..."
    
    # Create Lua script for atomic migration
    local lua_script='
    -- Migration script for 1.0.0 to 1.1.0
    local cursor = "0"
    local count = 0
    
    -- Migrate session keys
    repeat
        local result = redis.call("SCAN", cursor, "MATCH", "session:*", "COUNT", 100)
        cursor = result[1]
        local keys = result[2]
        
        for _, key in ipairs(keys) do
            -- Old format: session:user_id:session_id
            -- New format: mcp:session:user_id:session_id
            local new_key = "mcp:" .. key
            
            -- Copy data to new key
            local data = redis.call("GET", key)
            if data then
                redis.call("SET", new_key, data)
                redis.call("EXPIRE", new_key, 3600) -- Set 1 hour TTL
                -- Mark old key for deletion
                redis.call("EXPIRE", key, 60) -- Delete old key in 60 seconds
                count = count + 1
            end
        end
    until cursor == "0"
    
    -- Migrate rate limit keys
    cursor = "0"
    repeat
        local result = redis.call("SCAN", cursor, "MATCH", "ratelimit:*", "COUNT", 100)
        cursor = result[1]
        local keys = result[2]
        
        for _, key in ipairs(keys) do
            -- Old format: ratelimit:user_id
            -- New format: mcp:ratelimit:v2:user_id
            local new_key = key:gsub("^ratelimit:", "mcp:ratelimit:v2:")
            
            -- Convert to new data structure
            local old_count = redis.call("GET", key)
            if old_count then
                -- Create new structure with sliding window
                redis.call("ZADD", new_key, redis.call("TIME")[1], old_count)
                redis.call("EXPIRE", new_key, 60)
                -- Mark old key for deletion
                redis.call("DEL", key)
                count = count + 1
            end
        end
    until cursor == "0"
    
    return count
    '
    
    # Execute migration script
    local migrated_count=$(redis-cli --eval <(echo "$lua_script") 2>/dev/null || echo "0")
    
    log "Migrated $migrated_count Redis keys"
    
    # Create Redis migration marker
    redis-cli SET "mcp:migration:1.1.0" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" EX 86400 > /dev/null 2>&1 || true
}

# Update Kubernetes manifests
update_kubernetes_manifests() {
    local k8s_dir="${PROJECT_ROOT}/deployments/kubernetes"
    
    if [[ ! -d "$k8s_dir" ]]; then
        info "Kubernetes deployments not found, skipping"
        return 0
    fi
    
    info "Updating Kubernetes manifests..."
    
    # Update Gateway deployment
    local gateway_deploy="${k8s_dir}/gateway-deployment.yaml"
    if [[ -f "$gateway_deploy" ]]; then
        # Backup original
        cp "$gateway_deploy" "${gateway_deploy}.bak"
        
        if command -v yq &> /dev/null; then
            # Add new environment variables
            yq eval '.spec.template.spec.containers[0].env += [
                {"name": "OTEL_EXPORTER_OTLP_ENDPOINT", "value": "http://otel-collector:4317"},
                {"name": "OTEL_SERVICE_NAME", "value": "mcp-gateway"},
                {"name": "OTEL_TRACES_EXPORTER", "value": "otlp"},
                {"name": "MCP_HEALTH_TCP_ENABLED", "value": "true"},
                {"name": "MCP_HEALTH_TCP_PORT", "value": "8081"}
            ]' -i "$gateway_deploy"
            
            # Add health check port
            yq eval '.spec.template.spec.containers[0].ports += [
                {"name": "health-tcp", "containerPort": 8081, "protocol": "TCP"}
            ]' -i "$gateway_deploy"
            
            # Update resource limits
            yq eval '.spec.template.spec.containers[0].resources.limits.memory = "2Gi"' -i "$gateway_deploy"
            yq eval '.spec.template.spec.containers[0].resources.limits.cpu = "2000m"' -i "$gateway_deploy"
        fi
    fi
    
    # Update Router deployment
    local router_deploy="${k8s_dir}/router-deployment.yaml"
    if [[ -f "$router_deploy" ]]; then
        cp "$router_deploy" "${router_deploy}.bak"
        
        if command -v yq &> /dev/null; then
            # Add new environment variables
            yq eval '.spec.template.spec.containers[0].env += [
                {"name": "MCP_ROUTER_POOL_SIZE", "value": "100"},
                {"name": "MCP_ROUTER_RETRY_ENABLED", "value": "true"},
                {"name": "MCP_ROUTER_RETRY_MAX_ATTEMPTS", "value": "3"}
            ]' -i "$router_deploy"
        fi
    fi
    
    # Create new ConfigMap for observability
    cat > "${k8s_dir}/observability-configmap.yaml" << 'EOF'
apiVersion: v1
kind: ConfigMap
metadata:
  name: mcp-observability
  namespace: mcp-system
data:
  otel-config.yaml: |
    receivers:
      otlp:
        protocols:
          grpc:
            endpoint: 0.0.0.0:4317
          http:
            endpoint: 0.0.0.0:4318
    processors:
      batch:
        timeout: 1s
        send_batch_size: 1024
    exporters:
      prometheus:
        endpoint: "0.0.0.0:8889"
      jaeger:
        endpoint: jaeger:14250
        tls:
          insecure: true
    service:
      pipelines:
        traces:
          receivers: [otlp]
          processors: [batch]
          exporters: [jaeger]
        metrics:
          receivers: [otlp]
          processors: [batch]
          exporters: [prometheus]
EOF
    
    log "Kubernetes manifests updated"
}

# Update monitoring configuration
update_monitoring_config() {
    local monitoring_dir="${PROJECT_ROOT}/monitoring"
    
    if [[ ! -d "$monitoring_dir" ]]; then
        mkdir -p "$monitoring_dir"
    fi
    
    info "Updating monitoring configuration..."
    
    # Update Prometheus scrape configs
    local prom_config="${monitoring_dir}/prometheus/prometheus.yml"
    if [[ -f "$prom_config" ]]; then
        cp "$prom_config" "${prom_config}.bak"
        
        # Add new scrape job for TCP health checks
        cat >> "$prom_config" << 'EOF'

  # TCP Health check metrics (added in 1.1.0)
  - job_name: 'mcp-tcp-health'
    static_configs:
      - targets: ['localhost:8081']
    metrics_path: /metrics
    scrape_interval: 15s
EOF
    fi
    
    # Create new Grafana dashboard for v1.1.0 metrics
    mkdir -p "${monitoring_dir}/grafana/dashboards"
    cat > "${monitoring_dir}/grafana/dashboards/mcp-1.1.0-dashboard.json" << 'EOF'
{
  "dashboard": {
    "title": "MCP Bridge v1.1.0 Metrics",
    "version": "1.1.0",
    "panels": [
      {
        "title": "Connection Pool Utilization",
        "type": "graph",
        "targets": [
          {
            "expr": "mcp_gateway_pool_connections_active / mcp_gateway_pool_connections_max"
          }
        ]
      },
      {
        "title": "Retry Rate",
        "type": "stat",
        "targets": [
          {
            "expr": "rate(mcp_router_retries_total[5m])"
          }
        ]
      },
      {
        "title": "TCP Health Check Latency",
        "type": "graph",
        "targets": [
          {
            "expr": "mcp_health_tcp_latency_seconds"
          }
        ]
      }
    ]
  }
}
EOF
    
    log "Monitoring configuration updated"
}

# Execute migration if called directly
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
    migrate_1_0_0_to_1_1_0
fi