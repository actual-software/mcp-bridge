#!/bin/bash

# MCP Bridge Rollback Tool
# Emergency rollback procedure for failed upgrades

set -euo pipefail

# Color output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
BACKUP_DIR="${PROJECT_ROOT}/backups"
STATE_FILE="${PROJECT_ROOT}/.migration-state"
ROLLBACK_LOG="${PROJECT_ROOT}/rollback.log"

# Options
BACKUP_FILE=""
AUTO_DETECT=true
FORCE=false
DRY_RUN=false
SKIP_HEALTH_CHECK=false

# Logging
log() {
    echo -e "${GREEN}[$(date +'%Y-%m-%d %H:%M:%S')]${NC} $*" | tee -a "$ROLLBACK_LOG"
}

error() {
    echo -e "${RED}[ERROR]${NC} $*" | tee -a "$ROLLBACK_LOG" >&2
}

warn() {
    echo -e "${YELLOW}[WARNING]${NC} $*" | tee -a "$ROLLBACK_LOG"
}

info() {
    echo -e "${BLUE}[INFO]${NC} $*" | tee -a "$ROLLBACK_LOG"
}

# Usage
usage() {
    cat << EOF
Usage: $0 [OPTIONS]

MCP Bridge Emergency Rollback Tool

Options:
    -b, --backup FILE       Specific backup file to restore
    -a, --auto             Auto-detect latest backup (default)
    -f, --force            Force rollback even with warnings
    -d, --dry-run          Show what would be done without making changes
    -s, --skip-health      Skip health checks after rollback
    -h, --help             Show this help message

Examples:
    $0                                    # Auto-detect and rollback to latest backup
    $0 --backup backup-20240120.tar.gz   # Rollback to specific backup
    $0 --dry-run                         # Dry run rollback

EOF
    exit 0
}

# Parse arguments
parse_args() {
    while [[ $# -gt 0 ]]; do
        case $1 in
            -b|--backup)
                BACKUP_FILE="$2"
                AUTO_DETECT=false
                shift 2
                ;;
            -a|--auto)
                AUTO_DETECT=true
                shift
                ;;
            -f|--force)
                FORCE=true
                shift
                ;;
            -d|--dry-run)
                DRY_RUN=true
                shift
                ;;
            -s|--skip-health)
                SKIP_HEALTH_CHECK=true
                shift
                ;;
            -h|--help)
                usage
                ;;
            *)
                error "Unknown option: $1"
                usage
                ;;
        esac
    done
}

# Find latest backup
find_latest_backup() {
    if [[ ! -d "$BACKUP_DIR" ]]; then
        error "Backup directory not found: $BACKUP_DIR"
        return 1
    fi
    
    local latest=$(ls -t "$BACKUP_DIR"/*.tar.gz 2>/dev/null | head -1)
    
    if [[ -z "$latest" ]]; then
        error "No backup files found in $BACKUP_DIR"
        return 1
    fi
    
    echo "$latest"
}

# Validate backup file
validate_backup() {
    local backup=$1
    
    if [[ ! -f "$backup" ]]; then
        error "Backup file not found: $backup"
        return 1
    fi
    
    # Check if it's a valid tar.gz
    if ! tar -tzf "$backup" > /dev/null 2>&1; then
        error "Invalid backup file: $backup"
        return 1
    fi
    
    # Check for required components
    local temp_dir=$(mktemp -d)
    tar -xzf "$backup" -C "$temp_dir" 2>/dev/null
    
    local backup_root=$(find "$temp_dir" -maxdepth 1 -type d -name "backup-*" | head -1)
    if [[ -z "$backup_root" ]]; then
        backup_root="$temp_dir"
    fi
    
    # Check for manifest
    if [[ ! -f "${backup_root}/manifest.json" ]]; then
        warn "Backup manifest not found"
    else
        info "Backup details:"
        cat "${backup_root}/manifest.json" | jq '.' 2>/dev/null || cat "${backup_root}/manifest.json"
    fi
    
    rm -rf "$temp_dir"
    return 0
}

# Stop services
stop_services() {
    info "Stopping services..."
    
    if [[ "$DRY_RUN" == "true" ]]; then
        log "[DRY RUN] Would stop services"
        return 0
    fi
    
    # Try systemd
    if command -v systemctl &> /dev/null; then
        for service in mcp-gateway mcp-router; do
            if systemctl is-active "$service" &> /dev/null; then
                log "Stopping $service via systemd"
                sudo systemctl stop "$service"
            fi
        done
    fi
    
    # Try docker-compose
    if command -v docker-compose &> /dev/null; then
        if [[ -f "${PROJECT_ROOT}/deployment/local/docker-compose.yml" ]]; then
            log "Stopping services via docker-compose"
            docker-compose down
        fi
    fi
    
    # Try kubectl
    if command -v kubectl &> /dev/null; then
        if kubectl get namespace mcp-system &> /dev/null; then
            log "Scaling down Kubernetes deployments"
            kubectl scale deployment --all --replicas=0 -n mcp-system
        fi
    fi
    
    # Kill any remaining processes
    for process in mcp-gateway mcp-router; do
        if pgrep -f "$process" > /dev/null; then
            log "Killing $process processes"
            pkill -f "$process"
        fi
    done
    
    sleep 2
    log "Services stopped"
}

# Restore backup
restore_backup() {
    local backup=$1
    
    info "Restoring from backup: $backup"
    
    if [[ "$DRY_RUN" == "true" ]]; then
        log "[DRY RUN] Would restore from: $backup"
        return 0
    fi
    
    # Extract backup
    local temp_dir=$(mktemp -d)
    tar -xzf "$backup" -C "$temp_dir"
    
    local backup_root=$(find "$temp_dir" -maxdepth 1 -type d -name "backup-*" | head -1)
    if [[ -z "$backup_root" ]]; then
        backup_root="$temp_dir"
    fi
    
    # Restore configurations
    if [[ -d "${backup_root}/configs" ]]; then
        log "Restoring configuration files..."
        cp -r "${backup_root}/configs" "${PROJECT_ROOT}/"
    fi
    
    # Restore service configurations
    for service in gateway router; do
        if [[ -d "${backup_root}/services/${service}-configs" ]]; then
            log "Restoring $service configuration..."
            mkdir -p "${PROJECT_ROOT}/services/${service}"
            cp -r "${backup_root}/services/${service}-configs" "${PROJECT_ROOT}/services/${service}/configs"
        fi
    done
    
    # Restore Redis data if present
    if [[ -f "${backup_root}/redis-dump.rdb" ]]; then
        restore_redis "${backup_root}/redis-dump.rdb"
    fi
    
    # Restore Kubernetes resources if present
    if [[ -f "${backup_root}/k8s-resources.yaml" ]]; then
        restore_kubernetes "${backup_root}/k8s-resources.yaml"
    fi
    
    # Restore version file
    if [[ -f "${backup_root}/VERSION" ]]; then
        cp "${backup_root}/VERSION" "${PROJECT_ROOT}/VERSION"
    fi
    
    # Clean up
    rm -rf "$temp_dir"
    
    log "Backup restored successfully"
}

# Restore Redis data
restore_redis() {
    local redis_backup=$1
    
    if ! command -v redis-cli &> /dev/null; then
        warn "Redis CLI not found, skipping Redis restoration"
        return 0
    fi
    
    if ! redis-cli ping &> /dev/null; then
        warn "Redis not accessible, skipping Redis restoration"
        return 0
    fi
    
    log "Restoring Redis data..."
    
    # Flush current data
    redis-cli FLUSHALL
    
    # Restore from backup
    # Note: This is simplified. In production, use proper Redis restore
    redis-cli --rdb "$redis_backup" 2>/dev/null || {
        warn "Failed to restore Redis data directly"
        # Try alternative restore method
        local redis_dir=$(redis-cli CONFIG GET dir | tail -1)
        cp "$redis_backup" "${redis_dir}/dump.rdb"
        redis-cli SHUTDOWN NOSAVE
        sleep 2
        # Redis should restart automatically or via systemd
    }
    
    log "Redis data restored"
}

# Restore Kubernetes resources
restore_kubernetes() {
    local k8s_backup=$1
    
    if ! command -v kubectl &> /dev/null; then
        warn "kubectl not found, skipping Kubernetes restoration"
        return 0
    fi
    
    if ! kubectl cluster-info &> /dev/null; then
        warn "Kubernetes cluster not accessible, skipping"
        return 0
    fi
    
    log "Restoring Kubernetes resources..."
    
    # Apply backed up resources
    kubectl apply -f "$k8s_backup"
    
    log "Kubernetes resources restored"
}

# Start services
start_services() {
    info "Starting services..."
    
    if [[ "$DRY_RUN" == "true" ]]; then
        log "[DRY RUN] Would start services"
        return 0
    fi
    
    # Try systemd
    if command -v systemctl &> /dev/null; then
        for service in mcp-gateway mcp-router; do
            if systemctl list-unit-files | grep -q "$service"; then
                log "Starting $service via systemd"
                sudo systemctl start "$service"
            fi
        done
    fi
    
    # Try docker-compose
    if command -v docker-compose &> /dev/null; then
        if [[ -f "${PROJECT_ROOT}/deployment/local/docker-compose.yml" ]]; then
            log "Starting services via docker-compose"
            docker-compose up -d
        fi
    fi
    
    # Try kubectl
    if command -v kubectl &> /dev/null; then
        if kubectl get namespace mcp-system &> /dev/null; then
            log "Scaling up Kubernetes deployments"
            kubectl scale deployment mcp-gateway --replicas=3 -n mcp-system
            kubectl scale deployment mcp-router --replicas=3 -n mcp-system
        fi
    fi
    
    sleep 5
    log "Services started"
}

# Health check
health_check() {
    if [[ "$SKIP_HEALTH_CHECK" == "true" ]]; then
        warn "Skipping health checks"
        return 0
    fi
    
    info "Running health checks..."
    
    local all_healthy=true
    
    # Check gateway health
    if curl -s -f http://localhost:8080/healthz > /dev/null 2>&1; then
        log "✓ Gateway health check passed"
    else
        error "✗ Gateway health check failed"
        all_healthy=false
    fi
    
    # Check router health
    if curl -s -f http://localhost:9091/health > /dev/null 2>&1; then
        log "✓ Router health check passed"
    else
        error "✗ Router health check failed"
        all_healthy=false
    fi
    
    # Check Redis if applicable
    if command -v redis-cli &> /dev/null; then
        if redis-cli ping > /dev/null 2>&1; then
            log "✓ Redis health check passed"
        else
            warn "✗ Redis health check failed"
        fi
    fi
    
    if [[ "$all_healthy" == "true" ]]; then
        log "All health checks passed"
        return 0
    else
        error "Some health checks failed"
        return 1
    fi
}

# Update state file
update_state() {
    if [[ "$DRY_RUN" == "true" ]]; then
        return 0
    fi
    
    local rollback_date=$(date -u +%Y-%m-%dT%H:%M:%SZ)
    local current_version=$(cat "${PROJECT_ROOT}/VERSION" 2>/dev/null || echo "unknown")
    
    cat > "${STATE_FILE}" << EOF
last_rollback=$rollback_date
current_version=$current_version
rollback_log=$ROLLBACK_LOG
rollback_backup=$BACKUP_FILE
EOF
    
    log "State file updated"
}

# Main execution
main() {
    log "=== MCP Bridge Emergency Rollback ==="
    log "Starting rollback procedure..."
    
    # Parse arguments
    parse_args "$@"
    
    # Find or validate backup
    if [[ "$AUTO_DETECT" == "true" ]]; then
        BACKUP_FILE=$(find_latest_backup)
        info "Auto-detected backup: $BACKUP_FILE"
    fi
    
    if [[ -z "$BACKUP_FILE" ]]; then
        error "No backup file specified or found"
        exit 1
    fi
    
    # Validate backup
    validate_backup "$BACKUP_FILE"
    
    # Confirm rollback
    if [[ "$FORCE" == "false" ]] && [[ "$DRY_RUN" == "false" ]]; then
        echo -e "${YELLOW}WARNING: This will rollback MCP Bridge to a previous state${NC}"
        echo "Backup to restore: $BACKUP_FILE"
        read -p "Continue? (yes/no): " confirm
        if [[ "$confirm" != "yes" ]]; then
            info "Rollback cancelled"
            exit 0
        fi
    fi
    
    # Execute rollback
    stop_services
    restore_backup "$BACKUP_FILE"
    start_services
    
    # Verify rollback
    if ! health_check; then
        error "Rollback completed but health checks failed"
        error "Manual intervention may be required"
        exit 1
    fi
    
    # Update state
    update_state
    
    log "=== Rollback completed successfully ==="
    
    if [[ "$DRY_RUN" == "true" ]]; then
        info "This was a dry run. No actual changes were made."
    else
        info "System has been rolled back successfully"
        info "Current version: $(cat ${PROJECT_ROOT}/VERSION 2>/dev/null || echo 'unknown')"
    fi
}

# Trap errors
trap 'error "Rollback failed at line $LINENO"' ERR

# Run main function
main "$@"