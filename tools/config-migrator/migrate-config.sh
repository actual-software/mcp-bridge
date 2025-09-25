#!/bin/bash

# MCP Bridge Configuration Migration Tool
# Handles configuration updates between versions

set -euo pipefail

# Color output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Script directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$(dirname "$SCRIPT_DIR")")"

# Configuration
CONFIG_MIGRATIONS_DIR="${PROJECT_ROOT}/migrations/config"
BACKUP_DIR="${PROJECT_ROOT}/backups/config"

# Logging
log() { echo -e "${GREEN}[$(date +'%Y-%m-%d %H:%M:%S')]${NC} $*"; }
error() { echo -e "${RED}[ERROR]${NC} $*" >&2; }
warn() { echo -e "${YELLOW}[WARNING]${NC} $*"; }
info() { echo -e "${BLUE}[INFO]${NC} $*"; }

# Parse arguments
DRY_RUN=false
VALIDATE_ONLY=false
CONFIG_FILE=""
OUTPUT_FILE=""

while [[ $# -gt 0 ]]; do
    case $1 in
        -d|--dry-run)
            DRY_RUN=true
            shift
            ;;
        -v|--validate)
            VALIDATE_ONLY=true
            shift
            ;;
        -c|--config)
            CONFIG_FILE="$2"
            shift 2
            ;;
        -o|--output)
            OUTPUT_FILE="$2"
            shift 2
            ;;
        -h|--help)
            cat << EOF
Usage: $0 [OPTIONS] CONFIG_FILE

Migrate MCP Bridge configuration files to latest format

Options:
    -d, --dry-run       Show what would be changed without modifying files
    -v, --validate      Validate configuration only
    -c, --config FILE   Configuration file to migrate
    -o, --output FILE   Output file (default: overwrites input)
    -h, --help          Show this help message

Examples:
    $0 --dry-run configs/gateway.yaml
    $0 --validate configs/router.yaml
    $0 -c configs/gateway.yaml -o configs/gateway-new.yaml

EOF
            exit 0
            ;;
        *)
            CONFIG_FILE="$1"
            shift
            ;;
    esac
done

# Validate input
if [[ -z "$CONFIG_FILE" ]]; then
    error "Configuration file not specified"
    exit 1
fi

if [[ ! -f "$CONFIG_FILE" ]]; then
    error "Configuration file not found: $CONFIG_FILE"
    exit 1
fi

# Set output file
if [[ -z "$OUTPUT_FILE" ]]; then
    OUTPUT_FILE="$CONFIG_FILE"
fi

# Detect configuration type
detect_config_type() {
    local file=$1
    
    if grep -q "gateway:" "$file" 2>/dev/null || grep -q "routing:" "$file" 2>/dev/null; then
        echo "gateway"
    elif grep -q "router:" "$file" 2>/dev/null || grep -q "gateway_pool:" "$file" 2>/dev/null; then
        echo "router"
    else
        echo "unknown"
    fi
}

# Load migration rules
load_migration_rules() {
    local config_type=$1
    local rules_file="${CONFIG_MIGRATIONS_DIR}/${config_type}-migrations.json"
    
    if [[ ! -f "$rules_file" ]]; then
        warn "No migration rules found for $config_type"
        echo "{}"
        return
    fi
    
    cat "$rules_file"
}

# Apply migrations using yq
apply_migrations() {
    local input_file=$1
    local output_file=$2
    local config_type=$3
    
    # Create temporary file
    local temp_file=$(mktemp)
    cp "$input_file" "$temp_file"
    
    info "Applying migrations for $config_type configuration..."
    
    # Check for yq
    if ! command -v yq &> /dev/null; then
        warn "yq not found. Installing yq for configuration migration..."
        if [[ "$DRY_RUN" == "false" ]]; then
            # Install yq based on OS
            case "$(uname -s)" in
                Linux*)
                    wget -qO /tmp/yq https://github.com/mikefarah/yq/releases/latest/download/yq_linux_amd64
                    chmod +x /tmp/yq
                    alias yq=/tmp/yq
                    ;;
                Darwin*)
                    brew install yq 2>/dev/null || {
                        wget -qO /tmp/yq https://github.com/mikefarah/yq/releases/latest/download/yq_darwin_amd64
                        chmod +x /tmp/yq
                        alias yq=/tmp/yq
                    }
                    ;;
            esac
        fi
    fi
    
    # Apply common migrations
    case "$config_type" in
        gateway)
            apply_gateway_migrations "$temp_file"
            ;;
        router)
            apply_router_migrations "$temp_file"
            ;;
        *)
            warn "Unknown configuration type: $config_type"
            ;;
    esac
    
    # Copy to output
    if [[ "$DRY_RUN" == "false" ]]; then
        cp "$temp_file" "$output_file"
        log "Configuration migrated to: $output_file"
    else
        log "[DRY RUN] Would write to: $output_file"
        echo "=== Changes ==="
        diff -u "$input_file" "$temp_file" || true
    fi
    
    rm -f "$temp_file"
}

# Gateway-specific migrations
apply_gateway_migrations() {
    local file=$1
    
    # Add new required sections if missing
    
    # 1. Add observability section if missing
    if ! yq eval '.observability' "$file" | grep -q -v "null"; then
        info "Adding observability configuration..."
        yq eval '.observability = {
            "metrics": {
                "enabled": true,
                "port": 9090,
                "path": "/metrics"
            },
            "tracing": {
                "enabled": false,
                "provider": "otlp",
                "endpoint": "localhost:4317",
                "sample_rate": 0.1
            },
            "logging": {
                "level": "info",
                "format": "json"
            }
        }' -i "$file"
    fi
    
    # 2. Add health check configuration if missing
    if ! yq eval '.health' "$file" | grep -q -v "null"; then
        info "Adding health check configuration..."
        yq eval '.health = {
            "enabled": true,
            "port": 8080,
            "path": "/health",
            "checks": {
                "redis": true,
                "backends": true
            }
        }' -i "$file"
    fi
    
    # 3. Migrate deprecated fields
    
    # Migrate old rate_limit format
    if yq eval '.rate_limit.requests_per_second' "$file" | grep -q -v "null"; then
        info "Migrating rate limit configuration..."
        local rps=$(yq eval '.rate_limit.requests_per_second' "$file")
        yq eval ".rate_limit.limits.default = ${rps}" -i "$file"
        yq eval 'del(.rate_limit.requests_per_second)' -i "$file"
    fi
    
    # Migrate old session storage
    if yq eval '.sessions.storage' "$file" | grep -q -v "null"; then
        info "Migrating session storage configuration..."
        local storage=$(yq eval '.sessions.storage' "$file")
        yq eval ".sessions.provider = \"$storage\"" -i "$file"
        yq eval 'del(.sessions.storage)' -i "$file"
    fi
    
    # 4. Add default values for new fields
    
    # Connection pooling
    if ! yq eval '.server.connection_pool' "$file" | grep -q -v "null"; then
        info "Adding connection pool configuration..."
        yq eval '.server.connection_pool = {
            "max_idle": 100,
            "max_open": 1000,
            "max_lifetime": "5m"
        }' -i "$file"
    fi
    
    # Circuit breaker
    if ! yq eval '.routing.circuit_breaker' "$file" | grep -q -v "null"; then
        info "Adding circuit breaker configuration..."
        yq eval '.routing.circuit_breaker = {
            "enabled": true,
            "threshold": 5,
            "timeout": "10s",
            "half_open_requests": 3
        }' -i "$file"
    fi
}

# Router-specific migrations
apply_router_migrations() {
    local file=$1
    
    # 1. Add retry configuration if missing
    if ! yq eval '.gateway_pool.retry' "$file" | grep -q -v "null"; then
        info "Adding retry configuration..."
        yq eval '.gateway_pool.retry = {
            "enabled": true,
            "max_attempts": 3,
            "initial_interval": "100ms",
            "max_interval": "5s",
            "multiplier": 2.0
        }' -i "$file"
    fi
    
    # 2. Add connection pool settings
    if ! yq eval '.gateway_pool.connection_pool' "$file" | grep -q -v "null"; then
        info "Adding connection pool configuration..."
        yq eval '.gateway_pool.connection_pool = {
            "size": 10,
            "min_idle": 2,
            "max_idle_time": "5m",
            "health_check_interval": "30s"
        }' -i "$file"
    fi
    
    # 3. Migrate old gateway URL format
    if yq eval '.gateway_url' "$file" | grep -q -v "null"; then
        info "Migrating gateway URL configuration..."
        local url=$(yq eval '.gateway_url' "$file")
        yq eval ".gateway_pool.endpoints[0].url = \"$url\"" -i "$file"
        yq eval 'del(.gateway_url)' -i "$file"
    fi
    
    # 4. Add metrics configuration
    if ! yq eval '.metrics' "$file" | grep -q -v "null"; then
        info "Adding metrics configuration..."
        yq eval '.metrics = {
            "enabled": true,
            "port": 9091,
            "path": "/metrics"
        }' -i "$file"
    fi
}

# Validate configuration
validate_config() {
    local file=$1
    local config_type=$2
    
    info "Validating $config_type configuration..."
    
    local errors=0
    
    # Common validations
    if ! yq eval '.' "$file" > /dev/null 2>&1; then
        error "Invalid YAML syntax"
        ((errors++))
    fi
    
    case "$config_type" in
        gateway)
            # Check required gateway fields
            for field in "server.port" "routing"; do
                if yq eval ".$field" "$file" | grep -q "null"; then
                    error "Missing required field: $field"
                    ((errors++))
                fi
            done
            
            # Validate port ranges
            local port=$(yq eval '.server.port' "$file")
            if [[ "$port" -lt 1 || "$port" -gt 65535 ]]; then
                error "Invalid port number: $port"
                ((errors++))
            fi
            ;;
            
        router)
            # Check required router fields
            for field in "gateway_pool"; do
                if yq eval ".$field" "$file" | grep -q "null"; then
                    error "Missing required field: $field"
                    ((errors++))
                fi
            done
            ;;
    esac
    
    if [[ $errors -eq 0 ]]; then
        log "✓ Configuration is valid"
        return 0
    else
        error "Configuration validation failed with $errors errors"
        return 1
    fi
}

# Main execution
main() {
    log "Starting configuration migration..."
    
    # Detect configuration type
    CONFIG_TYPE=$(detect_config_type "$CONFIG_FILE")
    info "Detected configuration type: $CONFIG_TYPE"
    
    # Create backup
    if [[ "$DRY_RUN" == "false" ]] && [[ "$VALIDATE_ONLY" == "false" ]]; then
        mkdir -p "$BACKUP_DIR"
        BACKUP_FILE="${BACKUP_DIR}/$(basename "$CONFIG_FILE").backup-$(date +%Y%m%d-%H%M%S)"
        cp "$CONFIG_FILE" "$BACKUP_FILE"
        log "Backup created: $BACKUP_FILE"
    fi
    
    if [[ "$VALIDATE_ONLY" == "true" ]]; then
        validate_config "$CONFIG_FILE" "$CONFIG_TYPE"
        exit $?
    fi
    
    # Apply migrations
    apply_migrations "$CONFIG_FILE" "$OUTPUT_FILE" "$CONFIG_TYPE"
    
    # Validate migrated configuration
    if [[ "$DRY_RUN" == "false" ]]; then
        validate_config "$OUTPUT_FILE" "$CONFIG_TYPE"
    fi
    
    log "Configuration migration completed successfully"
}

# Run main function
main