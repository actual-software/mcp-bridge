#!/bin/bash
set -euo pipefail

# MCP Local Router Uninstallation Script
# https://github.com/poiley/mcp-bridge

# Configuration
INSTALL_DIR="/usr/local/bin"
BINARY_NAME="mcp-router"
CONFIG_DIR="$HOME/.config/claude-cli"
CONFIG_FILE="$CONFIG_DIR/mcp-router.yaml"
COMPLETIONS_DIR_BASH="/usr/local/share/bash-completion/completions"
COMPLETIONS_DIR_ZSH="/usr/local/share/zsh/site-functions"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Logging functions
log() {
    echo -e "${BLUE}[$(date +'%H:%M:%S')]${NC} $*"
}

success() {
    echo -e "${GREEN}✓${NC} $*"
}

error() {
    echo -e "${RED}Error:${NC} $*" >&2
    exit 1
}

warn() {
    echo -e "${YELLOW}Warning:${NC} $*"
}

# Check if running with sudo when needed
check_permissions() {
    if [[ ! -w "$INSTALL_DIR" ]]; then
        error "Cannot write to $INSTALL_DIR\nPlease run with sudo:\n  curl -sSL https://raw.githubusercontent.com/poiley/mcp-bridge/main/services/router/uninstall.sh | sudo bash"
    fi
}

# Backup configuration file
backup_config() {
    if [[ -f "$CONFIG_FILE" ]]; then
        local backup_name="${CONFIG_FILE}.backup.$(date +%Y%m%d-%H%M%S)"
        log "Backing up configuration..."
        cp "$CONFIG_FILE" "$backup_name"
        success "Configuration backed up to: $backup_name"
        echo "$backup_name"
    else
        echo ""
    fi
}

# Remove binary
remove_binary() {
    local binary_path="${INSTALL_DIR}/${BINARY_NAME}"
    
    if [[ -f "$binary_path" ]]; then
        log "Removing binary..."
        rm -f "$binary_path"
        success "Removed: $binary_path"
    else
        warn "Binary not found at $binary_path"
    fi
}

# Remove shell completions
remove_completions() {
    log "Removing shell completions..."
    
    # Remove bash completion
    local bash_completion="${COMPLETIONS_DIR_BASH}/${BINARY_NAME}"
    if [[ -f "$bash_completion" ]]; then
        rm -f "$bash_completion"
        success "Removed bash completion"
    fi
    
    # Remove zsh completion
    local zsh_completion="${COMPLETIONS_DIR_ZSH}/_${BINARY_NAME}"
    if [[ -f "$zsh_completion" ]]; then
        rm -f "$zsh_completion"
        success "Removed zsh completion"
    fi
}

# Remove configuration
remove_config() {
    if [[ -f "$CONFIG_FILE" ]]; then
        log "Removing configuration..."
        rm -f "$CONFIG_FILE"
        success "Removed: $CONFIG_FILE"
    fi
}

# Update Claude CLI configuration
update_claude_cli() {
    log "Updating Claude CLI configuration..."
    
    # Check if claude-cli exists
    if ! command -v claude-cli &> /dev/null; then
        warn "Claude CLI not found, skipping configuration cleanup"
        return
    fi
    
    # Remove MCP router server entries from Claude CLI configuration
    local claude_config="$HOME/.config/claude-cli/config.json"
    local claude_config_yaml="$HOME/.config/claude-cli/config.yaml"
    
    # Check for JSON config file
    if [[ -f "$claude_config" ]]; then
        log "Removing MCP router entries from Claude CLI JSON config..."
        # Create backup
        cp "$claude_config" "${claude_config}.backup.$(date +%Y%m%d-%H%M%S)"
        
        # Remove mcp-router server entries (if jq is available)
        if command -v jq &> /dev/null; then
            jq 'del(.mcpServers[] | select(.command | test("mcp-router")))' "$claude_config" > "${claude_config}.tmp" && mv "${claude_config}.tmp" "$claude_config"
        else
            warn "jq not available, manual cleanup of Claude CLI config may be needed"
        fi
    fi
    
    # Check for YAML config file
    if [[ -f "$claude_config_yaml" ]]; then
        log "Removing MCP router entries from Claude CLI YAML config..."
        # Create backup
        cp "$claude_config_yaml" "${claude_config_yaml}.backup.$(date +%Y%m%d-%H%M%S)"
        
        # Remove mcp-router server entries using sed (basic approach)
        sed -i.bak '/mcp-router/d' "$claude_config_yaml" 2>/dev/null || true
    fi
    
    success "Claude CLI configuration updated"
}

# Main uninstallation process
main() {
    echo ""
    echo "MCP Local Router Uninstaller"
    echo "============================"
    echo ""
    
    # Check permissions
    check_permissions
    
    # Show what will be removed
    echo "This will remove:"
    [[ -f "${INSTALL_DIR}/${BINARY_NAME}" ]] && echo "  ✓ ${INSTALL_DIR}/${BINARY_NAME}"
    [[ -f "$CONFIG_FILE" ]] && echo "  ✓ $CONFIG_FILE"
    [[ -f "${COMPLETIONS_DIR_BASH}/${BINARY_NAME}" ]] && echo "  ✓ ${COMPLETIONS_DIR_BASH}/${BINARY_NAME}"
    [[ -f "${COMPLETIONS_DIR_ZSH}/_${BINARY_NAME}" ]] && echo "  ✓ ${COMPLETIONS_DIR_ZSH}/_${BINARY_NAME}"
    echo "  ✓ MCP router configuration from Claude CLI"
    echo ""
    
    # Confirm
    read -p "Continue? [y/N]: " -n 1 -r
    echo ""
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        echo "Uninstallation cancelled"
        exit 0
    fi
    echo ""
    
    # Step 1: Backup configuration
    log "[1/5] Backing up configuration..."
    BACKUP_FILE=$(backup_config)
    
    # Step 2: Remove binary
    log "[2/5] Removing binary..."
    remove_binary
    
    # Step 3: Remove completions
    log "[3/5] Removing shell completions..."
    remove_completions
    
    # Step 4: Remove configuration
    log "[4/5] Removing configuration..."
    remove_config
    
    # Step 5: Update Claude CLI
    log "[5/5] Updating Claude CLI configuration..."
    update_claude_cli
    
    # Done!
    echo ""
    success "MCP Local Router has been uninstalled"
    
    if [[ -n "$BACKUP_FILE" ]]; then
        echo ""
        echo "💡 Your configuration was backed up to:"
        echo "   $BACKUP_FILE"
        echo ""
        echo "To restore it later:"
        echo "   cp $BACKUP_FILE $CONFIG_FILE"
    fi
    echo ""
}

# Run main function
main "$@"