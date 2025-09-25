#!/bin/bash

# Common functions for MCP Bridge migrations
# Shared utilities used across different migration scripts

set -euo pipefail

# Color output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Project paths
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

# Logging functions
log() {
    echo -e "${GREEN}[$(date +'%Y-%m-%d %H:%M:%S')]${NC} $*"
}

error() {
    echo -e "${RED}[ERROR]${NC} $*" >&2
}

warn() {
    echo -e "${YELLOW}[WARNING]${NC} $*"
}

info() {
    echo -e "${BLUE}[INFO]${NC} $*"
}

# Backup file with timestamp
backup_file() {
    local file=$1
    if [[ -f "$file" ]]; then
        local backup="${file}.backup-$(date +%Y%m%d-%H%M%S)"
        cp "$file" "$backup"
        log "Backed up $file to $backup"
    fi
}

# Check if command exists
command_exists() {
    command -v "$1" &> /dev/null
}

# Wait for service to be ready
wait_for_service() {
    local service=$1
    local url=$2
    local max_attempts=${3:-30}
    local attempt=1
    
    info "Waiting for $service to be ready..."
    
    while [[ $attempt -le $max_attempts ]]; do
        if curl -s -f "$url" > /dev/null 2>&1; then
            log "$service is ready"
            return 0
        fi
        
        echo -n "."
        sleep 2
        ((attempt++))
    done
    
    error "$service failed to become ready after $max_attempts attempts"
    return 1
}

# Update YAML configuration using yq or sed
update_yaml_config() {
    local file=$1
    local key=$2
    local value=$3
    
    if command_exists yq; then
        yq eval "$key = $value" -i "$file"
    else
        # Fallback to sed for simple updates
        warn "yq not found, using sed for configuration update"
        # This is a simplified approach and may not work for all cases
        sed -i.tmp "s|^${key}:.*|${key}: ${value}|" "$file"
    fi
}

# Add new YAML section
add_yaml_section() {
    local file=$1
    local section=$2
    
    # Check if section already exists
    if grep -q "^${section%%:*}:" "$file" 2>/dev/null; then
        warn "Section '${section%%:*}' already exists in $file"
        return 0
    fi
    
    echo "" >> "$file"
    echo "$section" >> "$file"
}

# Validate YAML file
validate_yaml() {
    local file=$1
    
    if command_exists yq; then
        if yq eval '.' "$file" > /dev/null 2>&1; then
            return 0
        else
            error "Invalid YAML in $file"
            return 1
        fi
    else
        warn "yq not found, skipping YAML validation"
        return 0
    fi
}

# Redis operations
redis_cli() {
    if command_exists redis-cli; then
        redis-cli "$@"
    else
        error "redis-cli not found"
        return 1
    fi
}

# Check Redis connectivity
check_redis() {
    if redis_cli ping > /dev/null 2>&1; then
        return 0
    else
        return 1
    fi
}

# Backup Redis database
backup_redis() {
    local backup_file="${1:-redis-backup-$(date +%Y%m%d-%H%M%S).rdb}"
    
    if check_redis; then
        info "Creating Redis backup: $backup_file"
        redis_cli --rdb "$backup_file" 2>/dev/null || {
            # Fallback to BGSAVE
            redis_cli BGSAVE
            sleep 2
            local redis_dir=$(redis_cli CONFIG GET dir | tail -1)
            cp "${redis_dir}/dump.rdb" "$backup_file"
        }
        log "Redis backup created: $backup_file"
    else
        warn "Redis not accessible, skipping backup"
    fi
}

# Execute Redis migration script
execute_redis_migration() {
    local script=$1
    
    if check_redis; then
        redis_cli --eval <(echo "$script") 2>/dev/null || {
            error "Redis migration script failed"
            return 1
        }
    else
        warn "Redis not accessible, skipping migration"
    fi
}

# Kubernetes operations
kubectl_apply() {
    if command_exists kubectl; then
        kubectl apply -f "$@"
    else
        warn "kubectl not found, skipping Kubernetes operations"
    fi
}

# Check Kubernetes connectivity
check_kubernetes() {
    if command_exists kubectl; then
        kubectl cluster-info > /dev/null 2>&1
    else
        return 1
    fi
}

# Backup Kubernetes resources
backup_kubernetes_resources() {
    local namespace=${1:-mcp-system}
    local backup_file="${2:-k8s-backup-$(date +%Y%m%d-%H%M%S).yaml}"
    
    if check_kubernetes; then
        info "Backing up Kubernetes resources from namespace: $namespace"
        kubectl get all,configmap,secret,ingress -n "$namespace" -o yaml > "$backup_file"
        log "Kubernetes backup created: $backup_file"
    else
        warn "Kubernetes not accessible, skipping backup"
    fi
}

# Docker operations
docker_pull() {
    if command_exists docker; then
        docker pull "$@"
    else
        warn "Docker not found"
    fi
}

# Check if Docker image exists
docker_image_exists() {
    local image=$1
    
    if command_exists docker; then
        docker images -q "$image" 2>/dev/null | grep -q .
    else
        return 1
    fi
}

# Service management
stop_service() {
    local service=$1
    
    # Try systemctl first
    if command_exists systemctl; then
        if systemctl is-active "$service" > /dev/null 2>&1; then
            info "Stopping $service via systemctl"
            sudo systemctl stop "$service"
            return 0
        fi
    fi
    
    # Try docker-compose
    if command_exists docker-compose; then
        if docker-compose ps | grep -q "$service"; then
            info "Stopping $service via docker-compose"
            docker-compose stop "$service"
            return 0
        fi
    fi
    
    # Try pkill
    if pgrep -f "$service" > /dev/null 2>&1; then
        info "Stopping $service via pkill"
        pkill -f "$service"
        return 0
    fi
    
    warn "Service $service not found or already stopped"
}

start_service() {
    local service=$1
    
    # Try systemctl first
    if command_exists systemctl; then
        if systemctl list-unit-files | grep -q "$service"; then
            info "Starting $service via systemctl"
            sudo systemctl start "$service"
            return 0
        fi
    fi
    
    # Try docker-compose
    if command_exists docker-compose; then
        if docker-compose config --services | grep -q "$service"; then
            info "Starting $service via docker-compose"
            docker-compose up -d "$service"
            return 0
        fi
    fi
    
    warn "Cannot start service $service - manual intervention required"
}

# Version comparison
version_gt() {
    test "$(printf '%s\n' "$@" | sort -V | head -n 1)" != "$1"
}

version_lt() {
    test "$(printf '%s\n' "$@" | sort -V | head -n 1)" == "$1"
}

version_eq() {
    test "$1" == "$2"
}

# Configuration migration helpers
migrate_config_value() {
    local file=$1
    local old_key=$2
    local new_key=$3
    
    if grep -q "^${old_key}:" "$file" 2>/dev/null; then
        local value=$(grep "^${old_key}:" "$file" | cut -d':' -f2- | xargs)
        sed -i.tmp "s|^${old_key}:.*||" "$file"
        echo "${new_key}: ${value}" >> "$file"
        log "Migrated config: $old_key -> $new_key"
    fi
}

# Rename configuration key
rename_config_key() {
    local file=$1
    local old_key=$2
    local new_key=$3
    
    if [[ -f "$file" ]]; then
        sed -i.tmp "s|^${old_key}:|${new_key}:|g" "$file"
        log "Renamed config key: $old_key -> $new_key in $file"
    fi
}

# Remove deprecated configuration
remove_deprecated_config() {
    local file=$1
    local key=$2
    
    if [[ -f "$file" ]]; then
        sed -i.tmp "/^${key}:/d" "$file"
        log "Removed deprecated config: $key from $file"
    fi
}

# Validate configuration file
validate_config() {
    local file=$1
    local service=$2
    
    case "$service" in
        gateway)
            if [[ -x "${PROJECT_ROOT}/services/gateway/bin/mcp-gateway" ]]; then
                "${PROJECT_ROOT}/services/gateway/bin/mcp-gateway" --config "$file" --validate 2>/dev/null
            fi
            ;;
        router)
            if [[ -x "${PROJECT_ROOT}/services/router/bin/mcp-router" ]]; then
                "${PROJECT_ROOT}/services/router/bin/mcp-router" --config "$file" --validate 2>/dev/null
            fi
            ;;
        *)
            warn "Unknown service: $service"
            return 1
            ;;
    esac
}

# Create migration marker
create_migration_marker() {
    local version=$1
    local marker_file="${PROJECT_ROOT}/.migration-markers/${version}"
    
    mkdir -p "$(dirname "$marker_file")"
    cat > "$marker_file" << EOF
{
    "version": "$version",
    "date": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
    "user": "$(whoami)",
    "hostname": "$(hostname)"
}
EOF
    log "Created migration marker for version $version"
}

# Check if migration was already applied
is_migration_applied() {
    local version=$1
    local marker_file="${PROJECT_ROOT}/.migration-markers/${version}"
    
    [[ -f "$marker_file" ]]
}

# Export functions for use in migration scripts
export -f log error warn info
export -f backup_file command_exists wait_for_service
export -f update_yaml_config add_yaml_section validate_yaml
export -f redis_cli check_redis backup_redis execute_redis_migration
export -f kubectl_apply check_kubernetes backup_kubernetes_resources
export -f docker_pull docker_image_exists
export -f stop_service start_service
export -f version_gt version_lt version_eq
export -f migrate_config_value rename_config_key remove_deprecated_config
export -f validate_config create_migration_marker is_migration_applied