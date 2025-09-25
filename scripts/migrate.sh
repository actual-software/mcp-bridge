#!/bin/bash

# MCP Bridge Migration Tool
# Handles version upgrades, configuration migrations, and data migrations

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
MIGRATIONS_DIR="${PROJECT_ROOT}/migrations"
BACKUP_DIR="${PROJECT_ROOT}/backups"
STATE_FILE="${PROJECT_ROOT}/.migration-state"
LOG_FILE="${PROJECT_ROOT}/migration.log"

# Version detection
CURRENT_VERSION=""
TARGET_VERSION=""
DRY_RUN=false
FORCE=false
ROLLBACK=false
SKIP_BACKUP=false

# Logging functions
log() {
    echo -e "${GREEN}[$(date +'%Y-%m-%d %H:%M:%S')]${NC} $*" | tee -a "$LOG_FILE"
}

error() {
    echo -e "${RED}[ERROR]${NC} $*" | tee -a "$LOG_FILE" >&2
}

warn() {
    echo -e "${YELLOW}[WARNING]${NC} $*" | tee -a "$LOG_FILE"
}

info() {
    echo -e "${BLUE}[INFO]${NC} $*" | tee -a "$LOG_FILE"
}

# Usage information
usage() {
    cat << EOF
Usage: $0 [OPTIONS] TARGET_VERSION

MCP Bridge Migration Tool - Safely migrate between versions

Options:
    -c, --current VERSION    Current version (auto-detected if not specified)
    -d, --dry-run           Perform dry run without making changes
    -f, --force             Force migration even with warnings
    -r, --rollback          Rollback to previous version
    -s, --skip-backup       Skip backup creation (not recommended)
    -h, --help              Show this help message

Examples:
    $0 1.1.0                     # Upgrade to version 1.1.0
    $0 --dry-run 1.1.0          # Dry run upgrade to 1.1.0
    $0 --rollback               # Rollback to previous version
    $0 --current 1.0.0 1.1.0    # Upgrade from 1.0.0 to 1.1.0

Supported Versions:
    1.0.0-rc1 -> 1.0.0
    1.0.0 -> 1.1.0
    1.1.0 -> 1.2.0
    1.2.0 -> 2.0.0

EOF
    exit 0
}

# Parse command line arguments
parse_args() {
    while [[ $# -gt 0 ]]; do
        case $1 in
            -c|--current)
                CURRENT_VERSION="$2"
                shift 2
                ;;
            -d|--dry-run)
                DRY_RUN=true
                shift
                ;;
            -f|--force)
                FORCE=true
                shift
                ;;
            -r|--rollback)
                ROLLBACK=true
                shift
                ;;
            -s|--skip-backup)
                SKIP_BACKUP=true
                shift
                ;;
            -h|--help)
                usage
                ;;
            -*)
                error "Unknown option: $1"
                usage
                ;;
            *)
                TARGET_VERSION="$1"
                shift
                ;;
        esac
    done
}

# Detect current version
detect_current_version() {
    if [[ -z "$CURRENT_VERSION" ]]; then
        # Try to detect from various sources
        if [[ -f "${PROJECT_ROOT}/VERSION" ]]; then
            CURRENT_VERSION=$(cat "${PROJECT_ROOT}/VERSION")
        elif [[ -f "${STATE_FILE}" ]]; then
            CURRENT_VERSION=$(grep "current_version=" "${STATE_FILE}" | cut -d'=' -f2)
        elif command -v git &> /dev/null; then
            CURRENT_VERSION=$(git describe --tags --abbrev=0 2>/dev/null || echo "unknown")
        else
            error "Cannot detect current version. Please specify with -c option."
            exit 1
        fi
    fi
    
    info "Current version: $CURRENT_VERSION"
}

# Validate version format
validate_version() {
    local version=$1
    if [[ ! "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[a-zA-Z0-9]+)?$ ]]; then
        error "Invalid version format: $version"
        error "Expected format: X.Y.Z or X.Y.Z-suffix"
        return 1
    fi
    return 0
}

# Compare versions
version_compare() {
    local v1=$1
    local v2=$2
    
    # Remove pre-release suffixes for comparison
    v1=${v1%%-*}
    v2=${v2%%-*}
    
    if [[ "$v1" == "$v2" ]]; then
        echo 0
    elif [[ "$(printf '%s\n' "$v1" "$v2" | sort -V | head -n1)" == "$v1" ]]; then
        echo -1
    else
        echo 1
    fi
}

# Check migration path exists
check_migration_path() {
    local from=$1
    local to=$2
    local migration_script="${MIGRATIONS_DIR}/${from}_to_${to}.sh"
    
    if [[ -f "$migration_script" ]]; then
        return 0
    fi
    
    # Check for multi-step migration path
    case "${from}_to_${to}" in
        "1.0.0-rc1_to_1.1.0")
            # Need intermediate step through 1.0.0
            if [[ -f "${MIGRATIONS_DIR}/1.0.0-rc1_to_1.0.0.sh" ]] && \
               [[ -f "${MIGRATIONS_DIR}/1.0.0_to_1.1.0.sh" ]]; then
                return 0
            fi
            ;;
    esac
    
    return 1
}

# Create backup
create_backup() {
    if [[ "$SKIP_BACKUP" == "true" ]]; then
        warn "Skipping backup creation"
        return 0
    fi
    
    local backup_name="backup-${CURRENT_VERSION}-$(date +%Y%m%d-%H%M%S)"
    local backup_path="${BACKUP_DIR}/${backup_name}"
    
    info "Creating backup: $backup_name"
    
    if [[ "$DRY_RUN" == "false" ]]; then
        mkdir -p "$backup_path"
        
        # Backup configuration files
        if [[ -d "${PROJECT_ROOT}/configs" ]]; then
            cp -r "${PROJECT_ROOT}/configs" "${backup_path}/"
        fi
        
        # Backup service files
        for service in gateway router; do
            if [[ -d "${PROJECT_ROOT}/services/${service}" ]]; then
                mkdir -p "${backup_path}/services"
                cp -r "${PROJECT_ROOT}/services/${service}/configs" "${backup_path}/services/${service}-configs" 2>/dev/null || true
            fi
        done
        
        # Backup Redis data if applicable
        if command -v redis-cli &> /dev/null; then
            redis-cli --rdb "${backup_path}/redis-dump.rdb" 2>/dev/null || true
        fi
        
        # Backup Kubernetes resources if applicable
        if command -v kubectl &> /dev/null; then
            kubectl get all -n mcp-system -o yaml > "${backup_path}/k8s-resources.yaml" 2>/dev/null || true
        fi
        
        # Create backup manifest
        cat > "${backup_path}/manifest.json" << EOF
{
    "timestamp": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
    "from_version": "$CURRENT_VERSION",
    "to_version": "$TARGET_VERSION",
    "backup_name": "$backup_name",
    "components": ["configs", "redis", "kubernetes"]
}
EOF
        
        # Compress backup
        tar -czf "${backup_path}.tar.gz" -C "${BACKUP_DIR}" "${backup_name}"
        rm -rf "${backup_path}"
        
        log "Backup created: ${backup_path}.tar.gz"
    else
        log "[DRY RUN] Would create backup: $backup_name"
    fi
    
    # Store backup location for rollback
    echo "last_backup=${backup_path}.tar.gz" >> "${STATE_FILE}"
}

# Pre-migration checks
pre_migration_checks() {
    info "Running pre-migration checks..."
    
    local checks_passed=true
    
    # Check disk space
    local available_space=$(df -k "${PROJECT_ROOT}" | awk 'NR==2 {print $4}')
    if [[ $available_space -lt 1048576 ]]; then # Less than 1GB
        error "Insufficient disk space. At least 1GB required."
        checks_passed=false
    fi
    
    # Check for running services
    for service in mcp-gateway mcp-router; do
        if pgrep -f "$service" > /dev/null 2>&1; then
            warn "Service $service is running. It should be stopped before migration."
            if [[ "$FORCE" == "false" ]]; then
                checks_passed=false
            fi
        fi
    done
    
    # Check Redis connectivity if used
    if [[ -f "${PROJECT_ROOT}/configs/gateway.yaml" ]]; then
        if grep -q "provider: redis" "${PROJECT_ROOT}/configs/gateway.yaml" 2>/dev/null; then
            if ! command -v redis-cli &> /dev/null; then
                warn "Redis CLI not found. Cannot verify Redis connectivity."
            elif ! redis-cli ping > /dev/null 2>&1; then
                error "Cannot connect to Redis. Please ensure Redis is accessible."
                checks_passed=false
            fi
        fi
    fi
    
    # Check Kubernetes cluster if applicable
    if [[ -d "${PROJECT_ROOT}/deployments/kubernetes" ]]; then
        if command -v kubectl &> /dev/null; then
            if ! kubectl cluster-info > /dev/null 2>&1; then
                warn "Cannot connect to Kubernetes cluster."
            fi
        fi
    fi
    
    if [[ "$checks_passed" == "false" ]]; then
        error "Pre-migration checks failed. Use --force to override."
        exit 1
    fi
    
    log "Pre-migration checks passed"
}

# Execute migration
execute_migration() {
    local from=$1
    local to=$2
    local migration_script="${MIGRATIONS_DIR}/${from}_to_${to}.sh"
    
    info "Executing migration: $from -> $to"
    
    if [[ -f "$migration_script" ]]; then
        if [[ "$DRY_RUN" == "true" ]]; then
            log "[DRY RUN] Would execute: $migration_script"
            # Show what the migration would do
            grep "^#" "$migration_script" | grep -E "^# (DESCRIPTION|CHANGES):" || true
        else
            # Source the migration script
            source "$migration_script"
            
            # Execute migration function
            if type -t migrate_${from//./_}_to_${to//./_} &> /dev/null; then
                migrate_${from//./_}_to_${to//./_}
            else
                error "Migration function not found in $migration_script"
                exit 1
            fi
        fi
    else
        error "Migration script not found: $migration_script"
        exit 1
    fi
}

# Multi-step migration
execute_multi_step_migration() {
    local from=$1
    local to=$2
    
    info "Executing multi-step migration: $from -> $to"
    
    case "${from}_to_${to}" in
        "1.0.0-rc1_to_1.1.0")
            execute_migration "1.0.0-rc1" "1.0.0"
            execute_migration "1.0.0" "1.1.0"
            ;;
        "1.0.0-rc1_to_1.2.0")
            execute_migration "1.0.0-rc1" "1.0.0"
            execute_migration "1.0.0" "1.1.0"
            execute_migration "1.1.0" "1.2.0"
            ;;
        *)
            error "Unknown multi-step migration path: $from -> $to"
            exit 1
            ;;
    esac
}

# Post-migration tasks
post_migration_tasks() {
    info "Running post-migration tasks..."
    
    if [[ "$DRY_RUN" == "false" ]]; then
        # Update version file
        echo "$TARGET_VERSION" > "${PROJECT_ROOT}/VERSION"
        
        # Update migration state
        cat > "${STATE_FILE}" << EOF
current_version=$TARGET_VERSION
previous_version=$CURRENT_VERSION
migration_date=$(date -u +%Y-%m-%dT%H:%M:%SZ)
migration_log=$LOG_FILE
EOF
        
        # Verify services can start
        info "Verifying service configuration..."
        
        # Test configuration files
        if [[ -f "${PROJECT_ROOT}/services/gateway/bin/mcp-gateway" ]]; then
            "${PROJECT_ROOT}/services/gateway/bin/mcp-gateway" --config "${PROJECT_ROOT}/configs/gateway.yaml" --validate 2>/dev/null || \
                warn "Gateway configuration validation failed"
        fi
        
        if [[ -f "${PROJECT_ROOT}/services/router/bin/mcp-router" ]]; then
            "${PROJECT_ROOT}/services/router/bin/mcp-router" --config "${PROJECT_ROOT}/configs/router.yaml" --validate 2>/dev/null || \
                warn "Router configuration validation failed"
        fi
        
        log "Post-migration tasks completed"
    else
        log "[DRY RUN] Would run post-migration tasks"
    fi
}

# Rollback function
perform_rollback() {
    info "Performing rollback..."
    
    if [[ ! -f "${STATE_FILE}" ]]; then
        error "No migration state found. Cannot rollback."
        exit 1
    fi
    
    local last_backup=$(grep "last_backup=" "${STATE_FILE}" | cut -d'=' -f2)
    local previous_version=$(grep "previous_version=" "${STATE_FILE}" | cut -d'=' -f2)
    
    if [[ -z "$last_backup" ]] || [[ ! -f "$last_backup" ]]; then
        error "Backup file not found: $last_backup"
        exit 1
    fi
    
    info "Restoring from backup: $last_backup"
    
    if [[ "$DRY_RUN" == "false" ]]; then
        # Extract backup
        local temp_dir=$(mktemp -d)
        tar -xzf "$last_backup" -C "$temp_dir"
        
        # Find backup directory
        local backup_dir=$(find "$temp_dir" -maxdepth 1 -type d -name "backup-*" | head -1)
        
        # Restore configurations
        if [[ -d "${backup_dir}/configs" ]]; then
            cp -r "${backup_dir}/configs" "${PROJECT_ROOT}/"
        fi
        
        # Restore service configs
        for service in gateway router; do
            if [[ -d "${backup_dir}/services/${service}-configs" ]]; then
                mkdir -p "${PROJECT_ROOT}/services/${service}"
                cp -r "${backup_dir}/services/${service}-configs" "${PROJECT_ROOT}/services/${service}/configs"
            fi
        done
        
        # Restore Redis if applicable
        if [[ -f "${backup_dir}/redis-dump.rdb" ]] && command -v redis-cli &> /dev/null; then
            redis-cli --rdb-restore "${backup_dir}/redis-dump.rdb" 2>/dev/null || \
                warn "Failed to restore Redis data"
        fi
        
        # Update version
        echo "$previous_version" > "${PROJECT_ROOT}/VERSION"
        
        # Clean up
        rm -rf "$temp_dir"
        
        log "Rollback completed. Restored to version: $previous_version"
    else
        log "[DRY RUN] Would rollback to: $previous_version"
    fi
}

# Health check after migration
health_check() {
    info "Running health checks..."
    
    local health_passed=true
    
    # Check gateway health endpoint if running
    if curl -s -f http://localhost:8080/healthz > /dev/null 2>&1; then
        log "Gateway health check: PASSED"
    else
        warn "Gateway health check: FAILED or not running"
    fi
    
    # Check router health endpoint if running
    if curl -s -f http://localhost:9091/health > /dev/null 2>&1; then
        log "Router health check: PASSED"
    else
        warn "Router health check: FAILED or not running"
    fi
    
    return 0
}

# Main execution
main() {
    # Initialize
    mkdir -p "$MIGRATIONS_DIR" "$BACKUP_DIR"
    touch "$LOG_FILE"
    
    log "=== MCP Bridge Migration Tool ==="
    log "Starting migration process..."
    
    # Parse arguments
    parse_args "$@"
    
    # Handle rollback
    if [[ "$ROLLBACK" == "true" ]]; then
        perform_rollback
        exit 0
    fi
    
    # Validate target version
    if [[ -z "$TARGET_VERSION" ]]; then
        error "Target version not specified"
        usage
    fi
    
    validate_version "$TARGET_VERSION" || exit 1
    
    # Detect current version
    detect_current_version
    validate_version "$CURRENT_VERSION" || exit 1
    
    # Check if migration is needed
    if [[ "$CURRENT_VERSION" == "$TARGET_VERSION" ]]; then
        info "Already at version $TARGET_VERSION. No migration needed."
        exit 0
    fi
    
    # Determine migration direction
    local direction=$(version_compare "$CURRENT_VERSION" "$TARGET_VERSION")
    if [[ $direction -eq 1 ]]; then
        warn "Downgrade detected: $CURRENT_VERSION -> $TARGET_VERSION"
        if [[ "$FORCE" == "false" ]]; then
            error "Downgrades require --force flag"
            exit 1
        fi
    fi
    
    # Check migration path exists
    if ! check_migration_path "$CURRENT_VERSION" "$TARGET_VERSION"; then
        error "No migration path from $CURRENT_VERSION to $TARGET_VERSION"
        exit 1
    fi
    
    # Run pre-migration checks
    pre_migration_checks
    
    # Create backup
    create_backup
    
    # Execute migration
    if [[ -f "${MIGRATIONS_DIR}/${CURRENT_VERSION}_to_${TARGET_VERSION}.sh" ]]; then
        execute_migration "$CURRENT_VERSION" "$TARGET_VERSION"
    else
        execute_multi_step_migration "$CURRENT_VERSION" "$TARGET_VERSION"
    fi
    
    # Run post-migration tasks
    post_migration_tasks
    
    # Run health checks
    health_check
    
    log "=== Migration completed successfully ==="
    log "Migrated from $CURRENT_VERSION to $TARGET_VERSION"
    
    if [[ "$DRY_RUN" == "true" ]]; then
        info "This was a dry run. No actual changes were made."
    fi
}

# Run main function
main "$@"