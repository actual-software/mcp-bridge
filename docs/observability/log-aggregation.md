# Log Aggregation Setup Guide

Comprehensive guide for setting up centralized log aggregation for MCP Bridge using ELK Stack, Fluentd, and other popular solutions.

## Table of Contents

- [Overview](#overview)
- [ELK Stack Setup](#elk-stack-setup)
- [Fluentd Configuration](#fluentd-configuration)
- [Loki + Grafana Setup](#loki--grafana-setup)
- [Log Format Standards](#log-format-standards)
- [Security Considerations](#security-considerations)
- [Performance Tuning](#performance-tuning)

## Overview

MCP Bridge generates structured logs from multiple components that need to be aggregated, indexed, and made searchable for debugging and monitoring purposes.

### Log Sources

- **Gateway Service**: Connection events, authentication, routing decisions
- **Router Service**: Protocol translation, message flow, errors
- **Health Checkers**: Endpoint status changes, health check results
- **Metrics Exporters**: Performance data, resource utilization

## ELK Stack Setup

### 1. Elasticsearch Configuration

```yaml
# elasticsearch-config.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: elasticsearch-config
  namespace: mcp-system
data:
  elasticsearch.yml: |
    cluster.name: mcp-logs
    node.name: ${HOSTNAME}
    
    # Network settings
    network.host: 0.0.0.0
    http.port: 9200
    
    # Discovery
    discovery.seed_hosts:
      - elasticsearch-0.elasticsearch
      - elasticsearch-1.elasticsearch
      - elasticsearch-2.elasticsearch
    cluster.initial_master_nodes:
      - elasticsearch-0
      - elasticsearch-1
      - elasticsearch-2
    
    # Index settings
    action.auto_create_index: ".monitoring*,.watches,.triggered_watches,.watcher-history*,.ml*,mcp-*"
    
    # Security
    xpack.security.enabled: true
    xpack.security.transport.ssl.enabled: true
    xpack.security.transport.ssl.verification_mode: certificate
    xpack.security.transport.ssl.keystore.path: /usr/share/elasticsearch/config/elastic-certificates.p12
    xpack.security.transport.ssl.truststore.path: /usr/share/elasticsearch/config/elastic-certificates.p12
    
    # Performance
    indices.memory.index_buffer_size: 30%
    indices.queries.cache.size: 15%
    
    # ILM (Index Lifecycle Management)
    xpack.ilm.enabled: true
    
---
# Elasticsearch StatefulSet
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: elasticsearch
  namespace: mcp-system
spec:
  serviceName: elasticsearch
  replicas: 3
  selector:
    matchLabels:
      app: elasticsearch
  template:
    metadata:
      labels:
        app: elasticsearch
    spec:
      containers:
      - name: elasticsearch
        image: docker.elastic.co/elasticsearch/elasticsearch:8.11.0
        resources:
          requests:
            memory: "2Gi"
            cpu: "1000m"
          limits:
            memory: "4Gi"
            cpu: "2000m"
        ports:
        - containerPort: 9200
          name: http
        - containerPort: 9300
          name: transport
        env:
        - name: cluster.name
          value: mcp-logs
        - name: ES_JAVA_OPTS
          value: "-Xms2g -Xmx2g"
        volumeMounts:
        - name: data
          mountPath: /usr/share/elasticsearch/data
        - name: config
          mountPath: /usr/share/elasticsearch/config/elasticsearch.yml
          subPath: elasticsearch.yml
  volumeClaimTemplates:
  - metadata:
      name: data
    spec:
      accessModes: [ "ReadWriteOnce" ]
      resources:
        requests:
          storage: 100Gi
```

### 2. Logstash Configuration

```yaml
# logstash-config.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: logstash-config
  namespace: mcp-system
data:
  logstash.yml: |
    http.host: "0.0.0.0"
    xpack.monitoring.elasticsearch.hosts: [ "http://elasticsearch:9200" ]
    
  pipeline.conf: |
    input {
      # Beats input for Filebeat
      beats {
        port => 5044
        ssl => true
        ssl_certificate => "/etc/logstash/ssl/logstash.crt"
        ssl_key => "/etc/logstash/ssl/logstash.key"
      }
      
      # TCP input for direct log shipping
      tcp {
        port => 5000
        codec => json_lines
      }
      
      # Syslog input
      syslog {
        port => 5514
        type => "syslog"
      }
    }
    
    filter {
      # Parse MCP Gateway logs
      if [service] == "mcp-gateway" {
        grok {
          match => {
            "message" => "%{TIMESTAMP_ISO8601:timestamp} %{LOGLEVEL:level} %{DATA:component} %{GREEDYDATA:msg}"
          }
        }
        
        # Extract structured fields
        json {
          source => "msg"
          target => "mcp"
        }
        
        # Add metadata
        mutate {
          add_field => {
            "[@metadata][index_prefix]" => "mcp-gateway"
            "environment" => "${ENVIRONMENT:production}"
          }
        }
      }
      
      # Parse MCP Router logs
      if [service] == "mcp-router" {
        grok {
          match => {
            "message" => "%{TIMESTAMP_ISO8601:timestamp} %{LOGLEVEL:level} \[%{DATA:component}\] %{GREEDYDATA:msg}"
          }
        }
        
        # Extract JSON fields if present
        if [msg] =~ /^\{.*\}$/ {
          json {
            source => "msg"
            target => "router"
          }
        }
        
        mutate {
          add_field => {
            "[@metadata][index_prefix]" => "mcp-router"
          }
        }
      }
      
      # Parse authentication events
      if [mcp][event_type] == "auth" {
        mutate {
          add_tag => [ "security", "authentication" ]
        }
        
        # Anonymize sensitive data
        mutate {
          gsub => [
            "mcp.token", ".", "*",
            "mcp.password", ".", "*"
          ]
        }
      }
      
      # Enrich with GeoIP for connection logs
      if [client_ip] {
        geoip {
          source => "client_ip"
          target => "geoip"
        }
      }
      
      # Calculate durations
      if [mcp][start_time] and [mcp][end_time] {
        ruby {
          code => "
            start_time = event.get('[mcp][start_time]')
            end_time = event.get('[mcp][end_time]')
            duration = Time.parse(end_time) - Time.parse(start_time)
            event.set('[mcp][duration_ms]', duration * 1000)
          "
        }
      }
      
      # Add fingerprint for deduplication
      fingerprint {
        source => ["message", "timestamp", "service"]
        target => "[@metadata][fingerprint]"
        method => "SHA256"
      }
    }
    
    output {
      # Output to Elasticsearch
      elasticsearch {
        hosts => ["elasticsearch:9200"]
        index => "%{[@metadata][index_prefix]}-%{+YYYY.MM.dd}"
        document_id => "%{[@metadata][fingerprint]}"
        
        # ILM settings
        ilm_enabled => true
        ilm_rollover_alias => "%{[@metadata][index_prefix]}"
        ilm_pattern => "{now/d}-000001"
        ilm_policy => "mcp-logs-policy"
        
        # Security
        user => "${ELASTIC_USER}"
        password => "${ELASTIC_PASSWORD}"
        ssl_certificate_verification => true
      }
      
      # Alert on errors
      if [level] == "ERROR" or [level] == "FATAL" {
        http {
          url => "${ALERT_WEBHOOK_URL}"
          http_method => "post"
          format => "json"
          mapping => {
            "service" => "%{service}"
            "level" => "%{level}"
            "message" => "%{message}"
            "timestamp" => "%{timestamp}"
          }
        }
      }
      
      # Debug output
      if [@metadata][debug] {
        stdout {
          codec => rubydebug
        }
      }
    }
```

### 3. Kibana Configuration

```yaml
# kibana-config.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: kibana-config
  namespace: mcp-system
data:
  kibana.yml: |
    server.name: mcp-kibana
    server.host: "0.0.0.0"
    elasticsearch.hosts: [ "http://elasticsearch:9200" ]
    
    # Security
    elasticsearch.username: "${ELASTIC_USER}"
    elasticsearch.password: "${ELASTIC_PASSWORD}"
    xpack.security.enabled: true
    xpack.encryptedSavedObjects.encryptionKey: "${KIBANA_ENCRYPTION_KEY}"
    
    # Monitoring
    monitoring.ui.container.elasticsearch.enabled: true
    
    # Telemetry
    telemetry.enabled: false
    
    # Default space
    xpack.spaces.enabled: true
    
    # Saved objects
    savedObjects.maxImportExportSize: 10000
    
---
# Kibana Dashboards
apiVersion: v1
kind: ConfigMap
metadata:
  name: kibana-dashboards
  namespace: mcp-system
data:
  mcp-overview.json: |
    {
      "version": "8.11.0",
      "objects": [
        {
          "id": "mcp-log-overview",
          "type": "dashboard",
          "attributes": {
            "title": "MCP Bridge Log Overview",
            "panels": [
              {
                "type": "visualization",
                "title": "Log Volume by Service",
                "query": {
                  "query": "service:*",
                  "language": "kuery"
                }
              },
              {
                "type": "visualization",
                "title": "Error Rate",
                "query": {
                  "query": "level:ERROR OR level:FATAL",
                  "language": "kuery"
                }
              },
              {
                "type": "visualization",
                "title": "Authentication Events",
                "query": {
                  "query": "tags:authentication",
                  "language": "kuery"
                }
              },
              {
                "type": "saved_search",
                "title": "Recent Errors",
                "columns": ["timestamp", "service", "level", "message"],
                "sort": [["timestamp", "desc"]],
                "query": {
                  "query": "level:ERROR",
                  "language": "kuery"
                }
              }
            ]
          }
        }
      ]
    }
```

## Fluentd Configuration

### 1. Fluentd DaemonSet

```yaml
# fluentd-config.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: fluentd-config
  namespace: mcp-system
data:
  fluent.conf: |
    # Input plugins
    <source>
      @type tail
      path /var/log/containers/mcp-*.log
      pos_file /var/log/fluentd-containers.log.pos
      tag kubernetes.*
      read_from_head true
      <parse>
        @type json
        time_format %Y-%m-%dT%H:%M:%S.%NZ
      </parse>
    </source>
    
    # Collect systemd logs
    <source>
      @type systemd
      path /run/log/journal
      tag systemd
      <entry>
        fields_strip_underscores true
        fields_lowercase true
      </entry>
    </source>
    
    # Forward from applications
    <source>
      @type forward
      port 24224
      bind 0.0.0.0
    </source>
    
    # Filter plugins
    <filter kubernetes.**>
      @type kubernetes_metadata
      @id filter_kube_metadata
      kubernetes_url "#{ENV['KUBERNETES_URL']}"
      verify_ssl "#{ENV['KUBERNETES_VERIFY_SSL']}"
    </filter>
    
    # Parse MCP application logs
    <filter kubernetes.var.log.containers.mcp-**>
      @type parser
      key_name log
      reserve_data true
      remove_key_name_field true
      <parse>
        @type multi_format
        <pattern>
          format json
        </pattern>
        <pattern>
          format regexp
          expression /^(?<time>[^ ]+) (?<level>[^ ]+) (?<component>[^ ]+) (?<message>.*)$/
          time_format %Y-%m-%dT%H:%M:%S.%NZ
        </pattern>
      </parse>
    </filter>
    
    # Add metadata
    <filter **>
      @type record_transformer
      <record>
        hostname ${hostname}
        environment ${ENV['ENVIRONMENT']}
        cluster ${ENV['CLUSTER_NAME']}
        region ${ENV['AWS_REGION']}
      </record>
    </filter>
    
    # Detect exceptions
    <filter **>
      @type detect_exceptions
      remove_tag_prefix kubernetes
      message log
      multiline_flush_interval 5
    </filter>
    
    # Buffer configuration
    <match **>
      @type elasticsearch
      @id out_es
      @log_level info
      include_tag_key true
      host "#{ENV['ELASTICSEARCH_HOST']}"
      port "#{ENV['ELASTICSEARCH_PORT']}"
      user "#{ENV['ELASTICSEARCH_USER']}"
      password "#{ENV['ELASTICSEARCH_PASSWORD']}"
      scheme https
      ssl_verify true
      ssl_version TLSv1_2
      
      logstash_format true
      logstash_prefix mcp-logs
      logstash_dateformat %Y.%m.%d
      
      # ILM settings
      enable_ilm true
      ilm_policy_id mcp-logs-policy
      ilm_policy {
        "policy": {
          "phases": {
            "hot": {
              "min_age": "0ms",
              "actions": {
                "rollover": {
                  "max_age": "1d",
                  "max_size": "50GB"
                }
              }
            },
            "warm": {
              "min_age": "7d",
              "actions": {
                "shrink": {
                  "number_of_shards": 1
                },
                "forcemerge": {
                  "max_num_segments": 1
                }
              }
            },
            "delete": {
              "min_age": "30d",
              "actions": {
                "delete": {}
              }
            }
          }
        }
      }
      
      <buffer>
        @type file
        path /var/log/fluentd-buffers/kubernetes.system.buffer
        flush_mode interval
        retry_type exponential_backoff
        flush_thread_count 2
        flush_interval 5s
        retry_forever
        retry_max_interval 30
        chunk_limit_size 2M
        queue_limit_length 8
        overflow_action block
      </buffer>
    </match>
    
---
# Fluentd DaemonSet
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: fluentd
  namespace: mcp-system
spec:
  selector:
    matchLabels:
      name: fluentd
  template:
    metadata:
      labels:
        name: fluentd
    spec:
      serviceAccountName: fluentd
      containers:
      - name: fluentd
        image: fluent/fluentd-kubernetes-daemonset:v1.16-debian-elasticsearch8-1
        env:
        - name: ELASTICSEARCH_HOST
          value: "elasticsearch"
        - name: ELASTICSEARCH_PORT
          value: "9200"
        - name: ELASTICSEARCH_USER
          valueFrom:
            secretKeyRef:
              name: elasticsearch-credentials
              key: username
        - name: ELASTICSEARCH_PASSWORD
          valueFrom:
            secretKeyRef:
              name: elasticsearch-credentials
              key: password
        - name: ENVIRONMENT
          value: "production"
        - name: CLUSTER_NAME
          value: "mcp-cluster"
        resources:
          limits:
            memory: 512Mi
          requests:
            cpu: 100m
            memory: 200Mi
        volumeMounts:
        - name: config
          mountPath: /fluentd/etc
        - name: varlog
          mountPath: /var/log
        - name: varlibdockercontainers
          mountPath: /var/lib/docker/containers
          readOnly: true
      volumes:
      - name: config
        configMap:
          name: fluentd-config
      - name: varlog
        hostPath:
          path: /var/log
      - name: varlibdockercontainers
        hostPath:
          path: /var/lib/docker/containers
```

## Loki + Grafana Setup

### Loki Configuration

```yaml
# loki-config.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: loki-config
  namespace: mcp-system
data:
  loki.yaml: |
    auth_enabled: false
    
    server:
      http_listen_port: 3100
      grpc_listen_port: 9096
    
    common:
      path_prefix: /tmp/loki
      storage:
        filesystem:
          chunks_directory: /tmp/loki/chunks
          rules_directory: /tmp/loki/rules
      replication_factor: 1
      ring:
        instance_addr: 127.0.0.1
        kvstore:
          store: inmemory
    
    schema_config:
      configs:
        - from: 2023-01-01
          store: boltdb-shipper
          object_store: filesystem
          schema: v11
          index:
            prefix: index_
            period: 24h
    
    ruler:
      alertmanager_url: http://alertmanager:9093
    
    analytics:
      reporting_enabled: false
    
    limits_config:
      enforce_metric_name: false
      reject_old_samples: true
      reject_old_samples_max_age: 168h
      max_entries_limit_per_query: 5000
      
    chunk_store_config:
      max_look_back_period: 0s
    
    table_manager:
      retention_deletes_enabled: true
      retention_period: 720h
```

### Promtail Configuration (Log Shipper for Loki)

```yaml
# promtail-config.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: promtail-config
  namespace: mcp-system
data:
  promtail.yaml: |
    server:
      http_listen_port: 9080
      grpc_listen_port: 0
    
    positions:
      filename: /tmp/positions.yaml
    
    clients:
      - url: http://loki:3100/loki/api/v1/push
    
    scrape_configs:
      - job_name: kubernetes-pods
        kubernetes_sd_configs:
          - role: pod
        pipeline_stages:
          - docker: {}
          - kubernetes:
              namespace_labels:
                - namespace
              pod_labels:
                - app
                - component
          - match:
              selector: '{app="mcp-gateway"}'
              stages:
                - json:
                    expressions:
                      level: level
                      timestamp: timestamp
                      component: component
                      message: message
                - labels:
                    level:
                    component:
                - timestamp:
                    source: timestamp
                    format: RFC3339
          - match:
              selector: '{app="mcp-router"}'
              stages:
                - regex:
                    expression: '^(?P<timestamp>[^ ]+) (?P<level>[^ ]+) \[(?P<component>[^\]]+)\] (?P<message>.*)$'
                - labels:
                    level:
                    component:
                - timestamp:
                    source: timestamp
                    format: RFC3339
        relabel_configs:
          - source_labels:
              - __meta_kubernetes_pod_controller_name
            regex: ([0-9a-z-.]+?)(-[0-9a-f]{8,10})?
            action: replace
            target_label: __tmp_controller_name
          - source_labels:
              - __meta_kubernetes_pod_label_app
            action: replace
            target_label: app
          - source_labels:
              - __meta_kubernetes_pod_label_component
            action: replace
            target_label: component
          - source_labels:
              - __meta_kubernetes_namespace
            action: replace
            target_label: namespace
```

## Log Format Standards

### Structured Logging Format

```go
// Standard log fields for MCP components
type LogEntry struct {
    Timestamp   time.Time              `json:"timestamp"`
    Level       string                 `json:"level"`
    Service     string                 `json:"service"`
    Component   string                 `json:"component"`
    Message     string                 `json:"message"`
    TraceID     string                 `json:"trace_id,omitempty"`
    SpanID      string                 `json:"span_id,omitempty"`
    UserID      string                 `json:"user_id,omitempty"`
    RequestID   string                 `json:"request_id,omitempty"`
    Method      string                 `json:"method,omitempty"`
    Path        string                 `json:"path,omitempty"`
    StatusCode  int                    `json:"status_code,omitempty"`
    Duration    float64                `json:"duration_ms,omitempty"`
    Error       string                 `json:"error,omitempty"`
    Stack       string                 `json:"stack,omitempty"`
    Metadata    map[string]interface{} `json:"metadata,omitempty"`
}
```

### Log Levels

- **DEBUG**: Detailed diagnostic information
- **INFO**: General informational messages
- **WARN**: Warning messages for potentially harmful situations
- **ERROR**: Error events that might still allow the application to continue
- **FATAL**: Critical problems that cause the application to abort

## Security Considerations

### 1. Log Sanitization

```yaml
# Sensitive data patterns to redact
redaction_patterns:
  - name: "API Keys"
    pattern: '(api[_-]?key|apikey)["'\'']?\s*[:=]\s*["'\'']?([a-zA-Z0-9_-]+)'
    replacement: "$1=REDACTED"
  
  - name: "Passwords"
    pattern: '(password|passwd|pwd)["'\'']?\s*[:=]\s*["'\'']?([^\s"'\'']+)'
    replacement: "$1=REDACTED"
  
  - name: "JWT Tokens"
    pattern: 'Bearer\s+[A-Za-z0-9-_]+\.[A-Za-z0-9-_]+\.[A-Za-z0-9-_]+'
    replacement: "Bearer REDACTED"
  
  - name: "Credit Cards"
    pattern: '\b(?:\d[ -]*?){13,16}\b'
    replacement: "XXXX-XXXX-XXXX-XXXX"
```

### 2. Access Control

```yaml
# RBAC for log access
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: log-reader
  namespace: mcp-system
rules:
- apiGroups: [""]
  resources: ["pods/log"]
  verbs: ["get", "list"]
  
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: log-reader-binding
  namespace: mcp-system
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: log-reader
subjects:
- kind: ServiceAccount
  name: log-aggregator
  namespace: mcp-system
```

## Performance Tuning

### 1. Buffer Settings

```yaml
# Optimal buffer configuration for high-volume logging
buffer_config:
  # Fluentd buffer
  fluentd:
    chunk_limit_size: 5M
    queue_limit_length: 32
    flush_interval: 5s
    retry_max_interval: 30s
    overflow_action: drop_oldest_chunk
  
  # Logstash buffer
  logstash:
    pipeline.batch.size: 125
    pipeline.batch.delay: 50
    pipeline.workers: 4
    queue.type: persisted
    queue.max_bytes: 1gb
```

### 2. Index Optimization

```json
{
  "index_patterns": ["mcp-*"],
  "settings": {
    "number_of_shards": 3,
    "number_of_replicas": 1,
    "refresh_interval": "5s",
    "translog.durability": "async",
    "translog.sync_interval": "5s",
    "translog.flush_threshold_size": "512mb"
  },
  "mappings": {
    "properties": {
      "timestamp": {
        "type": "date",
        "format": "strict_date_time"
      },
      "level": {
        "type": "keyword"
      },
      "service": {
        "type": "keyword"
      },
      "message": {
        "type": "text",
        "fields": {
          "keyword": {
            "type": "keyword",
            "ignore_above": 256
          }
        }
      }
    }
  }
}
```

### 3. Resource Limits

```yaml
# Resource recommendations for log aggregation components
resources:
  elasticsearch:
    requests:
      memory: "4Gi"
      cpu: "2"
    limits:
      memory: "8Gi"
      cpu: "4"
    java_opts: "-Xms4g -Xmx4g"
  
  logstash:
    requests:
      memory: "2Gi"
      cpu: "1"
    limits:
      memory: "4Gi"
      cpu: "2"
    java_opts: "-Xms1g -Xmx1g"
  
  fluentd:
    requests:
      memory: "200Mi"
      cpu: "100m"
    limits:
      memory: "500Mi"
      cpu: "500m"
```

## Monitoring the Log Pipeline

### Metrics to Track

1. **Input Rate**: Logs per second being ingested
2. **Processing Lag**: Time between log generation and indexing
3. **Error Rate**: Failed log parsing or shipping
4. **Storage Usage**: Disk space consumed by indices
5. **Query Performance**: Search response times

### Alert Rules

```yaml
alerts:
  - name: "High Log Ingestion Lag"
    condition: "avg(log_processing_lag_seconds) > 60"
    severity: "warning"
    
  - name: "Log Pipeline Error Rate High"
    condition: "rate(log_pipeline_errors_total[5m]) > 0.01"
    severity: "critical"
    
  - name: "Elasticsearch Disk Usage High"
    condition: "elasticsearch_filesystem_data_available_bytes / elasticsearch_filesystem_data_size_bytes < 0.1"
    severity: "critical"
```

## Troubleshooting

### Common Issues

1. **Logs not appearing in Elasticsearch**
   - Check Fluentd/Logstash pod logs
   - Verify network connectivity to Elasticsearch
   - Check index patterns and permissions

2. **High memory usage**
   - Reduce buffer sizes
   - Implement more aggressive log rotation
   - Use sampling for high-volume logs

3. **Slow queries**
   - Optimize index mappings
   - Implement proper index lifecycle management
   - Use time-based queries with appropriate ranges

## Best Practices

1. **Use structured logging** in all applications
2. **Implement log sampling** for high-volume, low-value logs
3. **Set up index lifecycle management** for automatic cleanup
4. **Monitor the monitoring system** - track log pipeline health
5. **Regular backup** of Elasticsearch indices
6. **Implement log retention policies** based on compliance requirements
7. **Use correlation IDs** for tracing requests across services
8. **Standardize log formats** across all services
9. **Implement real-time alerting** for critical errors
10. **Regular security audits** of log access and content