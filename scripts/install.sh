#!/bin/bash

# MCP Bridge Installation Script
# Complete installation for development, staging, or production environments

set -euo pipefail

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

# Configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
INSTALL_PREFIX="${INSTALL_PREFIX:-/usr/local}"
CONFIG_DIR="${CONFIG_DIR:-/etc/mcp-bridge}"
DATA_DIR="${DATA_DIR:-/var/lib/mcp-bridge}"
LOG_DIR="${LOG_DIR:-/var/log/mcp-bridge}"
SYSTEMD_DIR="/etc/systemd/system"

# Installation options
ENVIRONMENT="development"
VERBOSE=false
AUTO_YES=false
SKIP_SYSTEMD=false
SKIP_CONFIG=false
UNINSTALL=false
DRY_RUN=false

# Logging
log() { echo -e "${GREEN}[OK]${NC} $*"; }
error() { echo -e "${RED}[ERROR]${NC} $*" >&2; }
warn() { echo -e "${YELLOW}[WARN]${NC} $*"; }
info() { echo -e "${CYAN}[INFO]${NC} $*"; }
debug() { [[ "$VERBOSE" == "true" ]] && echo -e "${BLUE}[DEBUG]${NC} $*"; }

# Usage
usage() {
    cat << EOF
Usage: $0 [OPTIONS]

Install MCP Bridge on your system

Options:
    -e, --environment ENV    Environment (development/staging/production)
    -p, --prefix PATH       Installation prefix (default: /usr/local)
    -v, --verbose           Enable verbose output
    -y, --yes              Auto-confirm all prompts
    --skip-systemd         Skip systemd service installation
    --skip-config          Skip configuration file creation
    --dry-run              Show what would be installed
    --uninstall            Uninstall MCP Bridge
    -h, --help             Show this help message

Environment Types:
    development   - Local development setup (default)
    staging      - Staging environment with production-like config
    production   - Full production installation with all security features

Examples:
    $0 --environment development
    $0 --environment production --yes
    $0 --prefix /opt/mcp --environment staging
    $0 --uninstall

EOF
    exit 0
}

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -e|--environment)
            ENVIRONMENT="$2"
            shift 2
            ;;
        -p|--prefix)
            INSTALL_PREFIX="$2"
            shift 2
            ;;
        -v|--verbose)
            VERBOSE=true
            shift
            ;;
        -y|--yes)
            AUTO_YES=true
            shift
            ;;
        --skip-systemd)
            SKIP_SYSTEMD=true
            shift
            ;;
        --skip-config)
            SKIP_CONFIG=true
            shift
            ;;
        --dry-run)
            DRY_RUN=true
            shift
            ;;
        --uninstall)
            UNINSTALL=true
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

# Validate environment
case "$ENVIRONMENT" in
    development|staging|production)
        ;;
    *)
        error "Invalid environment: $ENVIRONMENT"
        error "Must be one of: development, staging, production"
        exit 1
        ;;
esac

# Check if running as root for production
if [[ "$ENVIRONMENT" == "production" ]] && [[ $EUID -ne 0 ]]; then
    error "Production installation requires root privileges"
    error "Please run with sudo: sudo $0 --environment production"
    exit 1
fi

# Detect OS
detect_os() {
    if [[ -f /etc/os-release ]]; then
        . /etc/os-release
        OS=$ID
        VER=$VERSION_ID
    elif type lsb_release >/dev/null 2>&1; then
        OS=$(lsb_release -si | tr '[:upper:]' '[:lower:]')
        VER=$(lsb_release -sr)
    elif [[ -f /etc/lsb-release ]]; then
        . /etc/lsb-release
        OS=$(echo $DISTRIB_ID | tr '[:upper:]' '[:lower:]')
        VER=$DISTRIB_RELEASE
    else
        OS=$(uname -s | tr '[:upper:]' '[:lower:]')
        VER=$(uname -r)
    fi
    
    debug "Detected OS: $OS $VER"
}

# Confirm action
confirm() {
    if [[ "$AUTO_YES" == "true" ]]; then
        return 0
    fi
    
    local prompt="$1"
    read -p "$prompt (y/N): " -n 1 -r
    echo
    [[ $REPLY =~ ^[Yy]$ ]]
}

# Execute command (with dry-run support)
execute() {
    if [[ "$DRY_RUN" == "true" ]]; then
        echo "[DRY RUN] $*"
    else
        debug "Executing: $*"
        "$@"
    fi
}

# Uninstall function
uninstall() {
    echo -e "${BOLD}Uninstalling MCP Bridge...${NC}\n"
    
    if ! confirm "This will remove MCP Bridge from your system. Continue?"; then
        info "Uninstall cancelled"
        exit 0
    fi
    
    # Stop services
    if systemctl is-active mcp-gateway &>/dev/null; then
        info "Stopping mcp-gateway service..."
        execute sudo systemctl stop mcp-gateway
        execute sudo systemctl disable mcp-gateway
    fi
    
    if systemctl is-active mcp-router &>/dev/null; then
        info "Stopping mcp-router service..."
        execute sudo systemctl stop mcp-router
        execute sudo systemctl disable mcp-router
    fi
    
    # Remove binaries
    info "Removing binaries..."
    execute rm -f "$INSTALL_PREFIX/bin/mcp-gateway"
    execute rm -f "$INSTALL_PREFIX/bin/mcp-router"
    execute rm -f "$INSTALL_PREFIX/bin/mcp-migrate"
    
    # Remove systemd units
    info "Removing systemd service files..."
    execute rm -f "$SYSTEMD_DIR/mcp-gateway.service"
    execute rm -f "$SYSTEMD_DIR/mcp-router.service"
    
    # Remove configuration (with backup)
    if [[ -d "$CONFIG_DIR" ]]; then
        info "Backing up configuration to $CONFIG_DIR.backup..."
        execute mv "$CONFIG_DIR" "$CONFIG_DIR.backup"
    fi
    
    # Remove data directory (with confirmation)
    if [[ -d "$DATA_DIR" ]]; then
        if confirm "Remove data directory $DATA_DIR?"; then
            execute rm -rf "$DATA_DIR"
        else
            info "Data directory preserved: $DATA_DIR"
        fi
    fi
    
    # Remove log directory
    if [[ -d "$LOG_DIR" ]]; then
        info "Removing log directory..."
        execute rm -rf "$LOG_DIR"
    fi
    
    # Remove user and group
    if id "mcp-bridge" &>/dev/null; then
        info "Removing mcp-bridge user..."
        execute userdel mcp-bridge
    fi
    
    if getent group mcp-bridge &>/dev/null; then
        info "Removing mcp-bridge group..."
        execute groupdel mcp-bridge
    fi
    
    log "MCP Bridge uninstalled successfully"
    exit 0
}

# Create user and group
create_user() {
    if [[ "$ENVIRONMENT" == "development" ]]; then
        return 0
    fi
    
    if ! getent group mcp-bridge &>/dev/null; then
        info "Creating mcp-bridge group..."
        execute groupadd -r mcp-bridge
    fi
    
    if ! id "mcp-bridge" &>/dev/null; then
        info "Creating mcp-bridge user..."
        execute useradd -r -g mcp-bridge -d "$DATA_DIR" -s /sbin/nologin mcp-bridge
    fi
}

# Create directories
create_directories() {
    info "Creating directories..."
    
    execute mkdir -p "$INSTALL_PREFIX/bin"
    execute mkdir -p "$CONFIG_DIR"
    execute mkdir -p "$DATA_DIR"
    execute mkdir -p "$LOG_DIR"
    
    if [[ "$ENVIRONMENT" != "development" ]]; then
        execute chown -R mcp-bridge:mcp-bridge "$DATA_DIR"
        execute chown -R mcp-bridge:mcp-bridge "$LOG_DIR"
        execute chmod 750 "$DATA_DIR"
        execute chmod 750 "$LOG_DIR"
    fi
}

# Build binaries
build_binaries() {
    info "Building MCP Bridge binaries..."
    
    # Build Gateway
    if [[ -d "$PROJECT_ROOT/services/gateway" ]]; then
        debug "Building gateway..."
        cd "$PROJECT_ROOT/services/gateway"
        execute go build -ldflags="-s -w" -o "$PROJECT_ROOT/bin/mcp-gateway" ./cmd/mcp-gateway
    else
        warn "Gateway source not found"
    fi
    
    # Build Router
    if [[ -d "$PROJECT_ROOT/services/router" ]]; then
        debug "Building router..."
        cd "$PROJECT_ROOT/services/router"
        execute go build -ldflags="-s -w" -o "$PROJECT_ROOT/bin/mcp-router" ./cmd/mcp-router
    else
        warn "Router source not found"
    fi
    
    # Build migration tool
    if [[ -f "$PROJECT_ROOT/tools/redis-migrator/main.go" ]]; then
        debug "Building migration tool..."
        cd "$PROJECT_ROOT/tools/redis-migrator"
        execute go build -ldflags="-s -w" -o "$PROJECT_ROOT/bin/mcp-migrate" .
    fi
    
    cd "$PROJECT_ROOT"
}

# Install binaries
install_binaries() {
    info "Installing binaries to $INSTALL_PREFIX/bin..."
    
    if [[ -f "$PROJECT_ROOT/bin/mcp-gateway" ]]; then
        execute install -m 755 "$PROJECT_ROOT/bin/mcp-gateway" "$INSTALL_PREFIX/bin/"
    fi
    
    if [[ -f "$PROJECT_ROOT/bin/mcp-router" ]]; then
        execute install -m 755 "$PROJECT_ROOT/bin/mcp-router" "$INSTALL_PREFIX/bin/"
    fi
    
    if [[ -f "$PROJECT_ROOT/bin/mcp-migrate" ]]; then
        execute install -m 755 "$PROJECT_ROOT/bin/mcp-migrate" "$INSTALL_PREFIX/bin/"
    fi
}

# Install configuration files
install_configs() {
    if [[ "$SKIP_CONFIG" == "true" ]]; then
        return 0
    fi
    
    info "Installing configuration files..."
    
    # Gateway config
    local gateway_config="$CONFIG_DIR/gateway.yaml"
    if [[ ! -f "$gateway_config" ]]; then
        cat > "$gateway_config" << EOF
# MCP Gateway Configuration - $ENVIRONMENT
server:
  host: 0.0.0.0
  port: 8080
  read_timeout: 30s
  write_timeout: 30s

tls:
  enabled: $([ "$ENVIRONMENT" == "production" ] && echo "true" || echo "false")
  cert_file: $CONFIG_DIR/certs/gateway.crt
  key_file: $CONFIG_DIR/certs/gateway.key

auth:
  - type: bearer
    enabled: true
    token: "${MCP_AUTH_TOKEN:-changeme}"

routing:
  default_backend: primary
  health_check_interval: 10s

rate_limit:
  enabled: $([ "$ENVIRONMENT" == "production" ] && echo "true" || echo "false")
  requests_per_second: $([ "$ENVIRONMENT" == "production" ] && echo "100" || echo "1000")

sessions:
  provider: $([ "$ENVIRONMENT" == "production" ] && echo "redis" || echo "memory")
  redis:
    url: localhost:6379

logging:
  level: $([ "$ENVIRONMENT" == "production" ] && echo "info" || echo "debug")
  format: json
  output: $LOG_DIR/gateway.log

metrics:
  enabled: true
  port: 9090
EOF
        execute chmod 640 "$gateway_config"
    else
        warn "Gateway config already exists, skipping"
    fi
    
    # Router config
    local router_config="$CONFIG_DIR/router.yaml"
    if [[ ! -f "$router_config" ]]; then
        cat > "$router_config" << EOF
# MCP Router Configuration - $ENVIRONMENT
router:
  mode: $ENVIRONMENT

gateway_pool:
  endpoints:
    - url: ws://localhost:8080
      weight: 100
  connection_pool:
    size: $([ "$ENVIRONMENT" == "production" ] && echo "50" || echo "10")
    health_check_interval: 30s

auth:
  type: bearer
  token: "${MCP_AUTH_TOKEN:-changeme}"

secure_storage:
  enabled: $([ "$ENVIRONMENT" == "production" ] && echo "true" || echo "false")

logging:
  level: $([ "$ENVIRONMENT" == "production" ] && echo "info" || echo "debug")
  format: json
  output: $LOG_DIR/router.log

metrics:
  enabled: true
  port: 9091
EOF
        execute chmod 640 "$router_config"
    else
        warn "Router config already exists, skipping"
    fi
    
    if [[ "$ENVIRONMENT" != "development" ]]; then
        execute chown -R root:mcp-bridge "$CONFIG_DIR"
    fi
}

# Install systemd services
install_systemd() {
    if [[ "$SKIP_SYSTEMD" == "true" ]] || [[ "$ENVIRONMENT" == "development" ]]; then
        return 0
    fi
    
    info "Installing systemd service files..."
    
    # Gateway service
    cat > "$SYSTEMD_DIR/mcp-gateway.service" << EOF
[Unit]
Description=MCP Bridge Gateway Service
Documentation=https://github.com/actual-software/mcp-bridge
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=mcp-bridge
Group=mcp-bridge
ExecStart=$INSTALL_PREFIX/bin/mcp-gateway --config $CONFIG_DIR/gateway.yaml
ExecReload=/bin/kill -USR1 \$MAINPID
Restart=on-failure
RestartSec=5
StandardOutput=journal
StandardError=journal
SyslogIdentifier=mcp-gateway

# Security
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=$DATA_DIR $LOG_DIR

[Install]
WantedBy=multi-user.target
EOF
    
    # Router service
    cat > "$SYSTEMD_DIR/mcp-router.service" << EOF
[Unit]
Description=MCP Bridge Router Service
Documentation=https://github.com/actual-software/mcp-bridge
After=network-online.target mcp-gateway.service
Wants=network-online.target

[Service]
Type=simple
User=mcp-bridge
Group=mcp-bridge
ExecStart=$INSTALL_PREFIX/bin/mcp-router --config $CONFIG_DIR/router.yaml
ExecReload=/bin/kill -USR1 \$MAINPID
Restart=on-failure
RestartSec=5
StandardOutput=journal
StandardError=journal
SyslogIdentifier=mcp-router

# Security
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=$DATA_DIR $LOG_DIR

[Install]
WantedBy=multi-user.target
EOF
    
    execute systemctl daemon-reload
}

# Setup TLS certificates
setup_tls() {
    if [[ "$ENVIRONMENT" != "production" ]]; then
        return 0
    fi
    
    info "Setting up TLS certificates..."
    
    local cert_dir="$CONFIG_DIR/certs"
    execute mkdir -p "$cert_dir"
    
    if [[ ! -f "$cert_dir/gateway.crt" ]]; then
        warn "No TLS certificate found. Generating self-signed certificate..."
        execute openssl req -x509 -newkey rsa:4096 -nodes \
            -keyout "$cert_dir/gateway.key" \
            -out "$cert_dir/gateway.crt" \
            -days 365 \
            -subj "/C=US/ST=State/L=City/O=Organization/CN=mcp-gateway"
        
        execute chmod 600 "$cert_dir/gateway.key"
        execute chmod 644 "$cert_dir/gateway.crt"
        execute chown -R root:mcp-bridge "$cert_dir"
    fi
}

# Verify installation
verify_installation() {
    echo -e "\n${BOLD}Verifying Installation...${NC}\n"
    
    local all_good=true
    
    # Check binaries
    if [[ -x "$INSTALL_PREFIX/bin/mcp-gateway" ]]; then
        log "Gateway binary installed"
    else
        error "Gateway binary not found"
        all_good=false
    fi
    
    if [[ -x "$INSTALL_PREFIX/bin/mcp-router" ]]; then
        log "Router binary installed"
    else
        error "Router binary not found"
        all_good=false
    fi
    
    # Check configs
    if [[ -f "$CONFIG_DIR/gateway.yaml" ]]; then
        log "Gateway configuration installed"
    else
        error "Gateway configuration not found"
        all_good=false
    fi
    
    if [[ -f "$CONFIG_DIR/router.yaml" ]]; then
        log "Router configuration installed"
    else
        error "Router configuration not found"
        all_good=false
    fi
    
    # Check systemd services (if applicable)
    if [[ "$ENVIRONMENT" != "development" ]] && [[ "$SKIP_SYSTEMD" != "true" ]]; then
        if [[ -f "$SYSTEMD_DIR/mcp-gateway.service" ]]; then
            log "Gateway systemd service installed"
        else
            error "Gateway systemd service not found"
            all_good=false
        fi
        
        if [[ -f "$SYSTEMD_DIR/mcp-router.service" ]]; then
            log "Router systemd service installed"
        else
            error "Router systemd service not found"
            all_good=false
        fi
    fi
    
    if [[ "$all_good" == "true" ]]; then
        echo -e "\n${GREEN}${BOLD}✓ Installation verified successfully${NC}"
    else
        echo -e "\n${YELLOW}${BOLD}⚠ Installation completed with warnings${NC}"
    fi
}

# Show post-installation instructions
show_instructions() {
    echo -e "\n${BOLD}Installation Complete!${NC}\n"
    
    echo -e "${CYAN}Configuration files:${NC}"
    echo "  • Gateway: $CONFIG_DIR/gateway.yaml"
    echo "  • Router:  $CONFIG_DIR/router.yaml"
    
    echo -e "\n${CYAN}Binaries installed to:${NC}"
    echo "  • $INSTALL_PREFIX/bin/mcp-gateway"
    echo "  • $INSTALL_PREFIX/bin/mcp-router"
    
    if [[ "$ENVIRONMENT" != "development" ]]; then
        echo -e "\n${CYAN}Start services:${NC}"
        echo "  sudo systemctl start mcp-gateway"
        echo "  sudo systemctl start mcp-router"
        
        echo -e "\n${CYAN}Enable services at boot:${NC}"
        echo "  sudo systemctl enable mcp-gateway"
        echo "  sudo systemctl enable mcp-router"
        
        echo -e "\n${CYAN}View logs:${NC}"
        echo "  journalctl -u mcp-gateway -f"
        echo "  journalctl -u mcp-router -f"
    else
        echo -e "\n${CYAN}Start services (development):${NC}"
        echo "  $INSTALL_PREFIX/bin/mcp-gateway --config $CONFIG_DIR/gateway.yaml"
        echo "  $INSTALL_PREFIX/bin/mcp-router --config $CONFIG_DIR/router.yaml"
    fi
    
    echo -e "\n${CYAN}Check service health:${NC}"
    echo "  curl http://localhost:8080/health"
    echo "  curl http://localhost:9091/health"
    
    if [[ "$ENVIRONMENT" == "production" ]]; then
        echo -e "\n${YELLOW}Important:${NC}"
        echo "  • Update authentication tokens in configuration files"
        echo "  • Replace self-signed certificates with proper TLS certificates"
        echo "  • Configure firewall rules for ports 8080, 9090, 9091"
        echo "  • Review security settings in configuration files"
    fi
}

# Main installation
main() {
    echo -e "${BOLD}MCP Bridge Installation${NC}"
    echo -e "Environment: ${CYAN}$ENVIRONMENT${NC}"
    echo -e "Prefix: ${CYAN}$INSTALL_PREFIX${NC}\n"
    
    # Handle uninstall
    if [[ "$UNINSTALL" == "true" ]]; then
        uninstall
    fi
    
    # Detect OS
    detect_os
    
    # Confirm installation
    if ! confirm "Proceed with installation?"; then
        info "Installation cancelled"
        exit 0
    fi
    
    # Create user and group
    create_user
    
    # Create directories
    create_directories
    
    # Build binaries
    build_binaries
    
    # Install binaries
    install_binaries
    
    # Install configuration
    install_configs
    
    # Setup TLS
    setup_tls
    
    # Install systemd services
    install_systemd
    
    # Verify installation
    verify_installation
    
    # Show instructions
    show_instructions
    
    echo -e "\n${GREEN}${BOLD}Installation completed successfully!${NC}"
}

# Run main
main