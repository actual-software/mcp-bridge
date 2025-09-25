# Backup and Restore Guide

Comprehensive backup and restore procedures for MCP Bridge production deployments.

## Table of Contents

- [Overview](#overview)
- [Backup Strategy](#backup-strategy)
- [What to Backup](#what-to-backup)
- [Backup Procedures](#backup-procedures)
- [Restore Procedures](#restore-procedures)
- [Automated Backup System](#automated-backup-system)
- [Verification and Testing](#verification-and-testing)
- [Retention Policies](#retention-policies)
- [Security Considerations](#security-considerations)

## Overview

This guide provides detailed procedures for backing up and restoring MCP Bridge components, ensuring data protection and rapid recovery capabilities.

### Backup Principles

- **3-2-1 Rule**: 3 copies of data, 2 different media types, 1 offsite copy
- **Automation**: Scheduled backups with minimal manual intervention
- **Encryption**: All backups encrypted at rest and in transit
- **Verification**: Regular restore testing to ensure backup integrity
- **Documentation**: Clear procedures for backup and restore operations

## Backup Strategy

### Backup Types

| Type | Frequency | Retention | Use Case |
|------|-----------|-----------|----------|
| **Full Backup** | Weekly | 4 weeks | Complete system restore |
| **Incremental** | Daily | 7 days | Point-in-time recovery |
| **Snapshot** | Hourly | 24 hours | Quick rollback |
| **Configuration** | On change | Forever | Config recovery |
| **Database** | Every 6 hours | 30 days | Data recovery |

### Recovery Objectives

- **RPO (Recovery Point Objective)**: < 1 hour for critical data
- **RTO (Recovery Time Objective)**: < 15 minutes for service restoration
- **Backup Window**: < 5 minutes for snapshot creation
- **Verification Time**: < 10 minutes for integrity check

## What to Backup

### Critical Components

```yaml
# Backup manifest
backup_components:
  configurations:
    - /etc/mcp-bridge/*.yaml
    - /etc/mcp-bridge/certs/
    - ~/.config/mcp-router/
    priority: critical
    
  application_data:
    - /var/lib/mcp-bridge/
    - Redis data (dump.rdb, appendonly.aof)
    priority: critical
    
  secrets:
    - TLS certificates
    - API keys and tokens
    - OAuth2 credentials
    priority: critical
    
  logs:
    - /var/log/mcp-bridge/
    - Systemd journal exports
    priority: medium
    
  metrics:
    - Prometheus data
    - Grafana dashboards
    priority: low
    
  documentation:
    - Custom runbooks
    - Configuration notes
    priority: medium
```

### Kubernetes Resources

```yaml
# K8s resources to backup
kubernetes:
  namespaces:
    - mcp-system
    - mcp-monitoring
  
  resources:
    - deployments
    - services
    - configmaps
    - secrets
    - persistentvolumeclaims
    - ingresses
    - networkpolicies
```

## Backup Procedures

### Manual Backup

#### 1. Configuration Backup

```bash
#!/bin/bash
# backup-config.sh

BACKUP_DIR="/backups/mcp-bridge/configs"
TIMESTAMP=$(date +%Y%m%d-%H%M%S)

# Create backup directory
mkdir -p "$BACKUP_DIR/$TIMESTAMP"

# Backup configurations
echo "Backing up configurations..."
cp -r /etc/mcp-bridge "$BACKUP_DIR/$TIMESTAMP/"
cp -r ~/.config/mcp-router "$BACKUP_DIR/$TIMESTAMP/"

# Backup Kubernetes configs
kubectl get configmap -n mcp-system -o yaml > "$BACKUP_DIR/$TIMESTAMP/k8s-configmaps.yaml"
kubectl get secret -n mcp-system -o yaml > "$BACKUP_DIR/$TIMESTAMP/k8s-secrets.yaml"

# Create tarball
tar -czf "$BACKUP_DIR/config-$TIMESTAMP.tar.gz" -C "$BACKUP_DIR" "$TIMESTAMP"

# Encrypt backup
openssl enc -aes-256-cbc -salt -in "$BACKUP_DIR/config-$TIMESTAMP.tar.gz" \
  -out "$BACKUP_DIR/config-$TIMESTAMP.tar.gz.enc" -k "$BACKUP_PASSWORD"

# Upload to S3
aws s3 cp "$BACKUP_DIR/config-$TIMESTAMP.tar.gz.enc" \
  s3://mcp-backups/configs/

echo "Configuration backup completed: config-$TIMESTAMP.tar.gz.enc"
```

#### 2. Data Backup

```bash
#!/bin/bash
# backup-data.sh

BACKUP_DIR="/backups/mcp-bridge/data"
TIMESTAMP=$(date +%Y%m%d-%H%M%S)

# Redis backup
echo "Backing up Redis..."
redis-cli BGSAVE
while [ $(redis-cli LASTSAVE) -eq $(redis-cli LASTSAVE) ]; do
  sleep 1
done
cp /var/lib/redis/dump.rdb "$BACKUP_DIR/redis-$TIMESTAMP.rdb"

# Application data
echo "Backing up application data..."
tar -czf "$BACKUP_DIR/appdata-$TIMESTAMP.tar.gz" \
  /var/lib/mcp-bridge/

# PostgreSQL backup (if used)
if command -v pg_dump &> /dev/null; then
  echo "Backing up PostgreSQL..."
  pg_dump -h localhost -U mcp_user -d mcp_db | gzip > "$BACKUP_DIR/postgres-$TIMESTAMP.sql.gz"
fi

# Create backup manifest
cat > "$BACKUP_DIR/manifest-$TIMESTAMP.json" << EOF
{
  "timestamp": "$TIMESTAMP",
  "components": ["redis", "appdata", "postgres"],
  "size": "$(du -sh $BACKUP_DIR/*$TIMESTAMP* | awk '{sum+=$1} END {print sum}')",
  "checksum": "$(sha256sum $BACKUP_DIR/*$TIMESTAMP* | cut -d' ' -f1)"
}
EOF

echo "Data backup completed"
```

#### 3. Full System Backup

```bash
#!/bin/bash
# backup-full.sh

BACKUP_ROOT="/backups/mcp-bridge"
TIMESTAMP=$(date +%Y%m%d-%H%M%S)
BACKUP_DIR="$BACKUP_ROOT/full-$TIMESTAMP"

mkdir -p "$BACKUP_DIR"

echo "Starting full system backup..."

# 1. Stop services for consistency
echo "Stopping services..."
systemctl stop mcp-gateway mcp-router

# 2. Backup all components
echo "Backing up configurations..."
cp -r /etc/mcp-bridge "$BACKUP_DIR/configs"

echo "Backing up data..."
cp -r /var/lib/mcp-bridge "$BACKUP_DIR/data"

echo "Backing up Redis..."
cp /var/lib/redis/dump.rdb "$BACKUP_DIR/redis.rdb"

echo "Backing up logs..."
cp -r /var/log/mcp-bridge "$BACKUP_DIR/logs"

# 3. Backup Kubernetes resources
echo "Backing up Kubernetes..."
kubectl get all,pvc,configmap,secret -n mcp-system -o yaml > "$BACKUP_DIR/k8s-resources.yaml"

# 4. Create backup archive
echo "Creating archive..."
tar -czf "$BACKUP_ROOT/full-backup-$TIMESTAMP.tar.gz" -C "$BACKUP_ROOT" "full-$TIMESTAMP"

# 5. Encrypt and upload
echo "Encrypting backup..."
openssl enc -aes-256-cbc -salt \
  -in "$BACKUP_ROOT/full-backup-$TIMESTAMP.tar.gz" \
  -out "$BACKUP_ROOT/full-backup-$TIMESTAMP.tar.gz.enc" \
  -k "$BACKUP_PASSWORD"

echo "Uploading to S3..."
aws s3 cp "$BACKUP_ROOT/full-backup-$TIMESTAMP.tar.gz.enc" \
  s3://mcp-backups/full/ \
  --storage-class GLACIER

# 6. Restart services
echo "Restarting services..."
systemctl start mcp-gateway mcp-router

# 7. Cleanup
rm -rf "$BACKUP_DIR"
rm "$BACKUP_ROOT/full-backup-$TIMESTAMP.tar.gz"

echo "Full backup completed: full-backup-$TIMESTAMP.tar.gz.enc"
```

## Restore Procedures

### Configuration Restore

```bash
#!/bin/bash
# restore-config.sh

BACKUP_FILE=$1
RESTORE_DIR="/tmp/restore-$$"

if [ -z "$BACKUP_FILE" ]; then
  echo "Usage: $0 <backup-file>"
  exit 1
fi

# Download from S3 if needed
if [[ "$BACKUP_FILE" == s3://* ]]; then
  aws s3 cp "$BACKUP_FILE" /tmp/
  BACKUP_FILE="/tmp/$(basename $BACKUP_FILE)"
fi

# Decrypt backup
echo "Decrypting backup..."
openssl enc -aes-256-cbc -d -in "$BACKUP_FILE" \
  -out "${BACKUP_FILE%.enc}" -k "$BACKUP_PASSWORD"

# Extract backup
echo "Extracting backup..."
mkdir -p "$RESTORE_DIR"
tar -xzf "${BACKUP_FILE%.enc}" -C "$RESTORE_DIR"

# Backup current configs
echo "Backing up current configuration..."
cp -r /etc/mcp-bridge /etc/mcp-bridge.backup.$(date +%s)

# Restore configurations
echo "Restoring configurations..."
cp -r "$RESTORE_DIR"/*/mcp-bridge/* /etc/mcp-bridge/

# Restore Kubernetes resources
if [ -f "$RESTORE_DIR"/*/k8s-configmaps.yaml ]; then
  echo "Restoring Kubernetes configurations..."
  kubectl apply -f "$RESTORE_DIR"/*/k8s-configmaps.yaml
fi

# Restart services
echo "Restarting services..."
systemctl restart mcp-gateway mcp-router

# Verify
echo "Verifying services..."
sleep 5
systemctl status mcp-gateway mcp-router

# Cleanup
rm -rf "$RESTORE_DIR"
rm "${BACKUP_FILE%.enc}"

echo "Configuration restore completed"
```

### Data Restore

```bash
#!/bin/bash
# restore-data.sh

BACKUP_TYPE=$1  # redis, postgres, or full
BACKUP_FILE=$2
TIMESTAMP=$(date +%Y%m%d-%H%M%S)

case "$BACKUP_TYPE" in
  redis)
    echo "Restoring Redis data..."
    # Stop Redis
    systemctl stop redis
    
    # Backup current data
    cp /var/lib/redis/dump.rdb "/var/lib/redis/dump.rdb.backup.$TIMESTAMP"
    
    # Restore data
    cp "$BACKUP_FILE" /var/lib/redis/dump.rdb
    chown redis:redis /var/lib/redis/dump.rdb
    
    # Start Redis
    systemctl start redis
    
    # Verify
    redis-cli ping
    ;;
    
  postgres)
    echo "Restoring PostgreSQL data..."
    # Create backup of current database
    pg_dump -h localhost -U mcp_user -d mcp_db | gzip > "/tmp/postgres-backup-$TIMESTAMP.sql.gz"
    
    # Drop and recreate database
    psql -U postgres -c "DROP DATABASE IF EXISTS mcp_db;"
    psql -U postgres -c "CREATE DATABASE mcp_db OWNER mcp_user;"
    
    # Restore data
    gunzip -c "$BACKUP_FILE" | psql -U mcp_user -d mcp_db
    
    # Verify
    psql -U mcp_user -d mcp_db -c "SELECT COUNT(*) FROM sessions;"
    ;;
    
  full)
    echo "Performing full restore..."
    # This would call both config and data restore procedures
    ./restore-config.sh "$BACKUP_FILE"
    ./restore-data.sh redis "${BACKUP_FILE%.tar.gz}/redis.rdb"
    ;;
    
  *)
    echo "Usage: $0 {redis|postgres|full} <backup-file>"
    exit 1
    ;;
esac

echo "Data restore completed"
```

### Point-in-Time Recovery

```bash
#!/bin/bash
# restore-pit.sh

TARGET_TIME=$1  # Format: YYYY-MM-DD HH:MM:SS

if [ -z "$TARGET_TIME" ]; then
  echo "Usage: $0 'YYYY-MM-DD HH:MM:SS'"
  exit 1
fi

echo "Performing point-in-time recovery to: $TARGET_TIME"

# Convert target time to timestamp
TARGET_TS=$(date -d "$TARGET_TIME" +%s)

# Find the closest backup
BACKUP_FILE=$(aws s3 ls s3://mcp-backups/incremental/ | \
  awk '{print $4}' | \
  while read file; do
    file_ts=$(echo $file | grep -oP '\d{8}-\d{6}' | sed 's/-//')
    file_epoch=$(date -d "$file_ts" +%s 2>/dev/null)
    if [ "$file_epoch" -le "$TARGET_TS" ]; then
      echo "$file_epoch $file"
    fi
  done | sort -n | tail -1 | awk '{print $2}')

if [ -z "$BACKUP_FILE" ]; then
  echo "No suitable backup found for target time"
  exit 1
fi

echo "Using backup: $BACKUP_FILE"

# Download and restore backup
aws s3 cp "s3://mcp-backups/incremental/$BACKUP_FILE" /tmp/
./restore-data.sh full "/tmp/$BACKUP_FILE"

# Apply transaction logs if available
if [ -d "/var/lib/mcp-bridge/wal" ]; then
  echo "Applying transaction logs..."
  for wal in /var/lib/mcp-bridge/wal/*.wal; do
    wal_ts=$(stat -c %Y "$wal")
    if [ "$wal_ts" -le "$TARGET_TS" ]; then
      echo "Applying: $wal"
      # Apply WAL file (implementation specific)
    fi
  done
fi

echo "Point-in-time recovery completed"
```

## Automated Backup System

### Backup Scheduler

```yaml
# kubernetes/cronjob-backup.yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: mcp-backup
  namespace: mcp-system
spec:
  schedule: "0 */6 * * *"  # Every 6 hours
  successfulJobsHistoryLimit: 3
  failedJobsHistoryLimit: 1
  jobTemplate:
    spec:
      template:
        metadata:
          labels:
            app: mcp-backup
        spec:
          serviceAccountName: mcp-backup
          containers:
          - name: backup
            image: mcp-bridge/backup:latest
            command: ["/scripts/automated-backup.sh"]
            env:
            - name: BACKUP_TYPE
              value: "incremental"
            - name: S3_BUCKET
              value: "mcp-backups"
            - name: BACKUP_PASSWORD
              valueFrom:
                secretKeyRef:
                  name: backup-credentials
                  key: password
            - name: AWS_ACCESS_KEY_ID
              valueFrom:
                secretKeyRef:
                  name: aws-credentials
                  key: access-key
            - name: AWS_SECRET_ACCESS_KEY
              valueFrom:
                secretKeyRef:
                  name: aws-credentials
                  key: secret-key
            volumeMounts:
            - name: data
              mountPath: /var/lib/mcp-bridge
              readOnly: true
            - name: backup-storage
              mountPath: /backups
          volumes:
          - name: data
            persistentVolumeClaim:
              claimName: mcp-data-pvc
          - name: backup-storage
            emptyDir: {}
          restartPolicy: OnFailure
```

### Automated Backup Script

```bash
#!/bin/bash
# automated-backup.sh

set -euo pipefail

# Configuration
BACKUP_TYPE=${BACKUP_TYPE:-incremental}
S3_BUCKET=${S3_BUCKET:-mcp-backups}
RETENTION_DAYS=${RETENTION_DAYS:-30}
TIMESTAMP=$(date +%Y%m%d-%H%M%S)
BACKUP_DIR="/backups/auto-$TIMESTAMP"

# Logging
log() {
  echo "[$(date +'%Y-%m-%d %H:%M:%S')] $*"
}

# Error handling
error_handler() {
  log "ERROR: Backup failed at line $1"
  # Send alert
  curl -X POST "$ALERT_WEBHOOK" \
    -H 'Content-Type: application/json' \
    -d "{\"text\":\"❌ Backup failed: $BACKUP_TYPE at $TIMESTAMP\"}"
  exit 1
}
trap 'error_handler $LINENO' ERR

# Main backup process
main() {
  log "Starting $BACKUP_TYPE backup..."
  
  mkdir -p "$BACKUP_DIR"
  
  case "$BACKUP_TYPE" in
    full)
      backup_full
      ;;
    incremental)
      backup_incremental
      ;;
    snapshot)
      backup_snapshot
      ;;
    *)
      log "Unknown backup type: $BACKUP_TYPE"
      exit 1
      ;;
  esac
  
  # Upload to S3
  log "Uploading to S3..."
  aws s3 cp "$BACKUP_DIR/" "s3://$S3_BUCKET/$BACKUP_TYPE/" \
    --recursive \
    --storage-class STANDARD_IA
  
  # Verify upload
  verify_backup
  
  # Clean up old backups
  cleanup_old_backups
  
  # Send success notification
  send_notification "success"
  
  log "Backup completed successfully"
}

backup_full() {
  log "Performing full backup..."
  
  # Configurations
  tar -czf "$BACKUP_DIR/configs.tar.gz" /etc/mcp-bridge/
  
  # Data
  tar -czf "$BACKUP_DIR/data.tar.gz" /var/lib/mcp-bridge/
  
  # Redis
  redis-cli BGSAVE
  sleep 5
  cp /var/lib/redis/dump.rdb "$BACKUP_DIR/redis.rdb"
  
  # Kubernetes
  kubectl get all,pvc,configmap,secret -n mcp-system -o yaml > "$BACKUP_DIR/k8s.yaml"
  
  # Create manifest
  create_manifest "full"
}

backup_incremental() {
  log "Performing incremental backup..."
  
  # Find last full backup
  LAST_FULL=$(aws s3 ls "s3://$S3_BUCKET/full/" | tail -1 | awk '{print $4}')
  
  if [ -z "$LAST_FULL" ]; then
    log "No full backup found, performing full backup instead"
    BACKUP_TYPE="full"
    backup_full
    return
  fi
  
  # Find changed files since last backup
  LAST_BACKUP_TIME=$(aws s3api head-object \
    --bucket "$S3_BUCKET" \
    --key "full/$LAST_FULL" \
    --query 'LastModified' \
    --output text)
  
  # Backup only changed files
  find /etc/mcp-bridge /var/lib/mcp-bridge \
    -newer "$LAST_BACKUP_TIME" \
    -type f \
    -exec tar -rf "$BACKUP_DIR/incremental.tar" {} \;
  
  gzip "$BACKUP_DIR/incremental.tar"
  
  # Create manifest
  create_manifest "incremental"
}

backup_snapshot() {
  log "Creating snapshot..."
  
  # Create filesystem snapshot (if supported)
  if command -v lvcreate &> /dev/null; then
    lvcreate -L10G -s -n mcp-snapshot /dev/vg0/mcp-data
    mount /dev/vg0/mcp-snapshot /mnt/snapshot
    tar -czf "$BACKUP_DIR/snapshot.tar.gz" /mnt/snapshot/
    umount /mnt/snapshot
    lvremove -f /dev/vg0/mcp-snapshot
  else
    # Fall back to regular backup
    tar -czf "$BACKUP_DIR/snapshot.tar.gz" \
      /etc/mcp-bridge/ \
      /var/lib/mcp-bridge/
  fi
  
  create_manifest "snapshot"
}

create_manifest() {
  local type=$1
  
  cat > "$BACKUP_DIR/manifest.json" << EOF
{
  "timestamp": "$TIMESTAMP",
  "type": "$type",
  "hostname": "$(hostname)",
  "version": "$(cat /etc/mcp-bridge/VERSION)",
  "files": $(find "$BACKUP_DIR" -type f -exec basename {} \; | jq -R -s -c 'split("\n")[:-1]'),
  "size": "$(du -sh $BACKUP_DIR | cut -f1)",
  "checksum": "$(find $BACKUP_DIR -type f -exec sha256sum {} \; | sha256sum | cut -d' ' -f1)"
}
EOF
}

verify_backup() {
  log "Verifying backup..."
  
  # Check if files exist in S3
  aws s3 ls "s3://$S3_BUCKET/$BACKUP_TYPE/" | grep "$TIMESTAMP" > /dev/null || {
    log "ERROR: Backup files not found in S3"
    exit 1
  }
  
  # Verify manifest
  aws s3 cp "s3://$S3_BUCKET/$BACKUP_TYPE/manifest.json" - | jq -e '.timestamp' > /dev/null || {
    log "ERROR: Invalid manifest"
    exit 1
  }
  
  log "Backup verification passed"
}

cleanup_old_backups() {
  log "Cleaning up old backups..."
  
  # Calculate cutoff date
  CUTOFF_DATE=$(date -d "$RETENTION_DAYS days ago" +%Y%m%d)
  
  # List and delete old backups
  aws s3 ls "s3://$S3_BUCKET/$BACKUP_TYPE/" | while read -r line; do
    file_date=$(echo "$line" | awk '{print $4}' | grep -oP '\d{8}' | head -1)
    if [ "$file_date" -lt "$CUTOFF_DATE" ]; then
      file_name=$(echo "$line" | awk '{print $4}')
      log "Deleting old backup: $file_name"
      aws s3 rm "s3://$S3_BUCKET/$BACKUP_TYPE/$file_name"
    fi
  done
}

send_notification() {
  local status=$1
  
  if [ "$status" = "success" ]; then
    message="✅ Backup completed successfully\nType: $BACKUP_TYPE\nTimestamp: $TIMESTAMP"
  else
    message="❌ Backup failed\nType: $BACKUP_TYPE\nTimestamp: $TIMESTAMP"
  fi
  
  # Send to Slack
  curl -X POST "$ALERT_WEBHOOK" \
    -H 'Content-Type: application/json' \
    -d "{\"text\":\"$message\"}"
  
  # Send metrics
  echo "mcp_backup_status{type=\"$BACKUP_TYPE\",status=\"$status\"} 1" | \
    curl -X POST --data-binary @- \
    http://prometheus-pushgateway:9091/metrics/job/backup
}

# Run main function
main
```

## Verification and Testing

### Backup Verification

```bash
#!/bin/bash
# verify-backup.sh

BACKUP_FILE=$1

echo "Verifying backup: $BACKUP_FILE"

# Check file exists and size
if [ ! -f "$BACKUP_FILE" ]; then
  echo "ERROR: Backup file not found"
  exit 1
fi

SIZE=$(stat -c%s "$BACKUP_FILE")
if [ "$SIZE" -lt 1024 ]; then
  echo "ERROR: Backup file too small ($SIZE bytes)"
  exit 1
fi

# Verify encryption
if [[ "$BACKUP_FILE" == *.enc ]]; then
  echo "Checking encryption..."
  openssl enc -aes-256-cbc -d -in "$BACKUP_FILE" \
    -out /dev/null -k "$BACKUP_PASSWORD" 2>/dev/null || {
    echo "ERROR: Cannot decrypt backup"
    exit 1
  }
fi

# Test extraction
TEMP_DIR=$(mktemp -d)
if [[ "$BACKUP_FILE" == *.enc ]]; then
  openssl enc -aes-256-cbc -d -in "$BACKUP_FILE" \
    -out "$TEMP_DIR/backup.tar.gz" -k "$BACKUP_PASSWORD"
  tar -tzf "$TEMP_DIR/backup.tar.gz" > /dev/null || {
    echo "ERROR: Cannot extract backup"
    rm -rf "$TEMP_DIR"
    exit 1
  }
else
  tar -tzf "$BACKUP_FILE" > /dev/null || {
    echo "ERROR: Cannot extract backup"
    exit 1
  }
fi

# Verify manifest
if tar -xzf "$BACKUP_FILE" -O manifest.json 2>/dev/null | jq -e . > /dev/null; then
  echo "Manifest valid"
else
  echo "WARNING: No manifest or invalid manifest"
fi

rm -rf "$TEMP_DIR"
echo "Backup verification passed"
```

### Restore Testing

```bash
#!/bin/bash
# test-restore.sh

echo "Starting restore test..."

# Create test namespace
kubectl create namespace mcp-restore-test

# Restore to test namespace
./restore-config.sh --namespace mcp-restore-test latest

# Verify services start
kubectl wait --for=condition=Ready pod -l app=mcp-gateway \
  -n mcp-restore-test --timeout=300s

# Run smoke tests
./scripts/smoke-test.sh --namespace mcp-restore-test

# Cleanup
kubectl delete namespace mcp-restore-test

echo "Restore test completed successfully"
```

## Retention Policies

### Policy Matrix

| Data Type | Daily | Weekly | Monthly | Yearly | Total Retention |
|-----------|-------|--------|---------|--------|-----------------|
| Full Backups | 7 | 4 | 12 | 5 | 5 years |
| Incremental | 7 | - | - | - | 7 days |
| Configs | 30 | 12 | 12 | ∞ | Forever |
| Logs | 7 | 4 | - | - | 30 days |
| Metrics | 30 | - | - | - | 30 days |

### Implementation

```bash
# retention-policy.sh
#!/bin/bash

apply_retention_policy() {
  local backup_type=$1
  local retention_days=$2
  
  find /backups/$backup_type -name "*.tar.gz*" -mtime +$retention_days -delete
  
  # S3 lifecycle policy
  aws s3api put-bucket-lifecycle-configuration \
    --bucket mcp-backups \
    --lifecycle-configuration file://s3-lifecycle.json
}

# s3-lifecycle.json
{
  "Rules": [
    {
      "Id": "DeleteOldBackups",
      "Status": "Enabled",
      "Filter": {"Prefix": "incremental/"},
      "Expiration": {"Days": 7}
    },
    {
      "Id": "TransitionToGlacier",
      "Status": "Enabled",
      "Filter": {"Prefix": "full/"},
      "Transitions": [
        {
          "Days": 30,
          "StorageClass": "GLACIER"
        }
      ],
      "Expiration": {"Days": 1825}
    }
  ]
}
```

## Security Considerations

### Encryption

```bash
# Encryption key management
export BACKUP_PASSWORD=$(aws secretsmanager get-secret-value \
  --secret-id mcp-backup-password \
  --query SecretString --output text)

# Rotate encryption keys
aws secretsmanager rotate-secret --secret-id mcp-backup-password
```

### Access Control

```yaml
# RBAC for backup service account
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: backup-operator
  namespace: mcp-system
rules:
- apiGroups: [""]
  resources: ["pods", "services", "configmaps", "secrets"]
  verbs: ["get", "list"]
- apiGroups: ["apps"]
  resources: ["deployments", "statefulsets"]
  verbs: ["get", "list"]
```

### Audit Logging

```bash
# Log all backup operations
log_backup_operation() {
  local operation=$1
  local status=$2
  
  echo "$(date -Iseconds) | $operation | $status | $(whoami)" >> /var/log/mcp-backup-audit.log
  
  # Send to SIEM
  logger -t mcp-backup -p auth.info "Backup $operation: $status"
}
```

## Monitoring and Alerts

### Backup Monitoring

```yaml
# Prometheus alerts
groups:
- name: backup_alerts
  rules:
  - alert: BackupFailed
    expr: mcp_backup_status{status="failed"} > 0
    for: 5m
    annotations:
      summary: "Backup failed for {{ $labels.type }}"
      
  - alert: BackupMissing
    expr: time() - mcp_backup_last_success > 86400
    for: 1h
    annotations:
      summary: "No successful backup in 24 hours"
      
  - alert: BackupStorageFull
    expr: disk_usage_percent{path="/backups"} > 90
    for: 10m
    annotations:
      summary: "Backup storage is {{ $value }}% full"
```

### Dashboard Metrics

```bash
# Export backup metrics
cat << EOF | curl -X POST --data-binary @- http://localhost:9091/metrics/job/backup
# HELP mcp_backup_size_bytes Size of last backup
# TYPE mcp_backup_size_bytes gauge
mcp_backup_size_bytes{type="full"} $BACKUP_SIZE

# HELP mcp_backup_duration_seconds Time taken for backup
# TYPE mcp_backup_duration_seconds gauge
mcp_backup_duration_seconds{type="full"} $DURATION

# HELP mcp_backup_last_success Timestamp of last successful backup
# TYPE mcp_backup_last_success gauge
mcp_backup_last_success{type="full"} $(date +%s)
EOF
```

## Recovery Procedures Checklist

### Pre-Recovery
- [ ] Identify recovery requirements
- [ ] Locate appropriate backup
- [ ] Verify backup integrity
- [ ] Prepare recovery environment
- [ ] Notify stakeholders

### During Recovery
- [ ] Stop affected services
- [ ] Create backup of current state
- [ ] Restore from backup
- [ ] Verify data integrity
- [ ] Start services

### Post-Recovery
- [ ] Run verification tests
- [ ] Monitor service health
- [ ] Document recovery process
- [ ] Update runbooks if needed
- [ ] Conduct post-mortem

## Appendix

### Backup Storage Sizing

```
Daily incremental: ~100MB
Weekly full: ~1GB
Monthly archive: ~1GB compressed

Annual storage requirement:
- Daily: 100MB × 7 = 700MB
- Weekly: 1GB × 4 = 4GB
- Monthly: 1GB × 12 = 12GB
- Yearly: 1GB × 5 = 5GB
Total: ~22GB + 20% overhead = ~27GB
```

### Recovery Time Estimates

| Scenario | Data Size | Recovery Time |
|----------|-----------|---------------|
| Config only | < 10MB | < 1 minute |
| Redis data | < 1GB | < 5 minutes |
| Full restore | < 10GB | < 15 minutes |
| Large dataset | > 100GB | 1-2 hours |

### Support Contacts

- Backup System Admin: backup-admin@company.com
- Storage Team: storage@company.com
- On-Call: Use PagerDuty
- Vendor Support: support@backup-vendor.com