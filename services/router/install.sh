#!/bin/bash
set -euo pipefail

# MCP Local Router Installation Script
# https://github.com/actual-software/mcp-bridge

# Configuration
GITHUB_REPO="actual-software/mcp-bridge"
INSTALL_DIR="/usr/local/bin"
BINARY_NAME="mcp-router"
CONFIG_DIR="$HOME/.config/claude-cli"
COMPLETIONS_DIR_BASH="/usr/local/share/bash-completion/completions"
COMPLETIONS_DIR_ZSH="/usr/local/share/zsh/site-functions"

# Feature flags
SETUP_SECURE_STORAGE=true
SETUP_COMPLETIONS=true
INTERACTIVE_SETUP=true

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Verbose mode flag
VERBOSE=false

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --verbose|-v)
            VERBOSE=true
            shift
            ;;
        --no-secure-storage)
            SETUP_SECURE_STORAGE=false
            shift
            ;;
        --no-completions)
            SETUP_COMPLETIONS=false
            shift
            ;;
        --no-interactive)
            INTERACTIVE_SETUP=false
            shift
            ;;
        --binary-only)
            SETUP_SECURE_STORAGE=false
            SETUP_COMPLETIONS=false
            INTERACTIVE_SETUP=false
            shift
            ;;
        --help|-h)
            echo "MCP Local Router Installer"
            echo ""
            echo "Usage: curl -sSL https://raw.githubusercontent.com/poiley/mcp-bridge/main/services/router/install.sh | bash"
            echo "       curl -sSL https://raw.githubusercontent.com/poiley/mcp-bridge/main/services/router/install.sh | bash -s -- [options]"
            echo ""
            echo "Options:"
            echo "  --verbose, -v              Show detailed output"
            echo "  --no-secure-storage        Skip secure storage setup"
            echo "  --no-completions          Skip shell completion installation"
            echo "  --no-interactive          Skip interactive setup wizard"
            echo "  --binary-only             Install binary only, skip all setup"
            echo "  --help, -h                Show this help message"
            exit 0
            ;;
        *)
            echo -e "${RED}Error: Unknown option $1${NC}"
            exit 1
            ;;
    esac
done

# Logging functions
log() {
    echo -e "${BLUE}[$(date +'%H:%M:%S')]${NC} $*"
}

success() {
    echo -e "${GREEN}✓${NC} $*"
}

error() {
    echo -e "${RED}Error:${NC} $*" >&2
    if [[ "$VERBOSE" == "true" ]]; then
        echo -e "${RED}Stack trace:${NC}" >&2
        local i=0
        while caller $i; do
            ((i++))
        done | while read line sub file; do
            echo "  at $sub ($file:$line)" >&2
        done
    fi
    exit 1
}

warn() {
    echo -e "${YELLOW}Warning:${NC} $*"
}

verbose() {
    if [[ "$VERBOSE" == "true" ]]; then
        echo -e "${BLUE}[DEBUG]${NC} $*"
    fi
}

# Check if running with sudo when needed
check_permissions() {
    if [[ ! -w "$INSTALL_DIR" ]]; then
        error "Cannot write to $INSTALL_DIR\nPlease run with sudo:\n  curl -sSL https://raw.githubusercontent.com/${GITHUB_REPO}/main/services/router/install.sh | sudo bash"
    fi
}

# Detect OS and architecture
detect_system() {
    local os arch

    # Detect OS
    case "$(uname -s)" in
        Darwin)
            os="darwin"
            ;;
        Linux)
            os="linux"
            ;;
        MINGW*|CYGWIN*|MSYS*)
            os="windows"
            ;;
        *)
            error "Unsupported operating system: $(uname -s)"
            ;;
    esac

    # Detect architecture
    case "$(uname -m)" in
        x86_64|amd64)
            arch="amd64"
            ;;
        arm64|aarch64)
            arch="arm64"
            ;;
        *)
            error "Unsupported architecture: $(uname -m)"
            ;;
    esac

    echo "${os}-${arch}"
}

# Check for required dependencies
check_dependencies() {
    log "Checking system requirements..."
    
    # Check for curl
    if ! command -v curl &> /dev/null; then
        error "curl is required but not installed"
    fi
    verbose "Found curl: $(which curl)"
    
    # Check for secure storage requirements
    if [[ "$SETUP_SECURE_STORAGE" == "true" ]]; then
        case "$(uname -s)" in
            Darwin)
                # macOS - check for security command
                if ! command -v security &> /dev/null; then
                    warn "macOS Keychain tools not found"
                    SETUP_SECURE_STORAGE=false
                fi
                ;;
            Linux)
                # Linux - check for D-Bus and secret service
                if [[ -z "$DBUS_SESSION_BUS_ADDRESS" ]]; then
                    warn "D-Bus session not found, secure storage may not work"
                fi
                ;;
            MINGW*|CYGWIN*|MSYS*)
                # Windows - credential manager should be available
                verbose "Windows Credential Manager should be available"
                ;;
        esac
    fi
    
    # Check for Claude CLI
    if command -v claude-cli &> /dev/null; then
        success "Found Claude CLI at $(which claude-cli)"
    else
        warn "Claude CLI not found in PATH"
        warn "You'll need to configure it manually after installation"
    fi
}

# Get the latest release version from GitHub
get_latest_version() {
    log "Fetching latest version..."
    
    local api_url="https://api.github.com/repos/${GITHUB_REPO}/releases/latest"
    local version
    
    version=$(curl -sSf "$api_url" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')
    
    if [[ -z "$version" ]]; then
        error "Failed to fetch latest version from GitHub"
    fi
    
    verbose "Latest version: $version"
    echo "$version"
}

# Download and install the binary
install_binary() {
    local version="$1"
    local system="$2"
    
    local binary_name="${BINARY_NAME}-${system}"
    if [[ "$system" == "windows-"* ]]; then
        binary_name="${binary_name}.exe"
    fi
    
    local download_url="https://github.com/${GITHUB_REPO}/releases/download/${version}/${binary_name}"
    local temp_file=$(mktemp)
    local temp_checksum=$(mktemp)
    
    # Cleanup on exit
    trap "rm -f $temp_file $temp_checksum" EXIT
    
    log "Downloading ${BINARY_NAME} ${version} for ${system}..."
    verbose "URL: $download_url"
    
    # Download binary
    if ! curl -fsSL "$download_url" -o "$temp_file"; then
        error "Failed to download binary from $download_url"
    fi
    
    # Download checksums
    local checksum_url="https://github.com/${GITHUB_REPO}/releases/download/${version}/checksums.txt"
    if curl -fsSL "$checksum_url" -o "$temp_checksum" 2>/dev/null; then
        log "Verifying checksum..."
        local expected_checksum=$(grep "$binary_name" "$temp_checksum" | awk '{print $1}')
        local actual_checksum=$(sha256sum "$temp_file" | awk '{print $1}')
        
        if [[ "$expected_checksum" != "$actual_checksum" ]]; then
            error "Checksum verification failed\nExpected: $expected_checksum\nActual: $actual_checksum"
        fi
        success "Checksum verified"
    else
        warn "Could not download checksums file, skipping verification"
    fi
    
    # Make executable
    chmod +x "$temp_file"
    
    # Move to install directory
    log "Installing to ${INSTALL_DIR}/${BINARY_NAME}..."
    if ! mv "$temp_file" "${INSTALL_DIR}/${BINARY_NAME}"; then
        error "Failed to install binary to ${INSTALL_DIR}/${BINARY_NAME}"
    fi
    
    success "Binary installed successfully"
}

# Install shell completions
install_completions() {
    log "Installing shell completions..."
    
    # Create completion directories if they don't exist
    if [[ -d "/usr/local/share/bash-completion" ]] && [[ ! -d "$COMPLETIONS_DIR_BASH" ]]; then
        mkdir -p "$COMPLETIONS_DIR_BASH" 2>/dev/null || true
    fi
    
    if [[ -d "/usr/local/share/zsh" ]] && [[ ! -d "$COMPLETIONS_DIR_ZSH" ]]; then
        mkdir -p "$COMPLETIONS_DIR_ZSH" 2>/dev/null || true
    fi
    
    # Download and install bash completion
    if [[ -d "$COMPLETIONS_DIR_BASH" ]] && [[ -w "$COMPLETIONS_DIR_BASH" ]]; then
        local bash_completion_url="https://raw.githubusercontent.com/${GITHUB_REPO}/main/services/router/completions/bash/mcp-router"
        if curl -fsSL "$bash_completion_url" -o "${COMPLETIONS_DIR_BASH}/${BINARY_NAME}" 2>/dev/null; then
            success "Installed bash completions"
        else
            verbose "Could not download bash completions"
        fi
    else
        verbose "Skipping bash completions (directory not writable)"
    fi
    
    # Download and install zsh completion
    if [[ -d "$COMPLETIONS_DIR_ZSH" ]] && [[ -w "$COMPLETIONS_DIR_ZSH" ]]; then
        local zsh_completion_url="https://raw.githubusercontent.com/${GITHUB_REPO}/main/services/router/completions/zsh/_mcp-router"
        if curl -fsSL "$zsh_completion_url" -o "${COMPLETIONS_DIR_ZSH}/_${BINARY_NAME}" 2>/dev/null; then
            success "Installed zsh completions"
        else
            verbose "Could not download zsh completions"
        fi
    else
        verbose "Skipping zsh completions (directory not writable)"
    fi
}

# Setup secure token storage
setup_secure_storage() {
    if [[ "$SETUP_SECURE_STORAGE" != "true" ]]; then
        return
    fi
    
    log "Setting up secure token storage..."
    
    # Test secure storage
    if "${INSTALL_DIR}/${BINARY_NAME}" token doctor &>/dev/null; then
        success "Secure storage is available and working"
    else
        warn "Secure storage test failed, you may need to set it up manually"
        warn "Run 'mcp-router token doctor' for diagnostics"
    fi
}

# Create initial configuration
create_initial_config() {
    log "Creating initial configuration..."
    
    # Create config directory
    mkdir -p "$CONFIG_DIR"
    
    # Check if config already exists
    if [[ -f "$CONFIG_DIR/mcp-router.yaml" ]]; then
        warn "Configuration already exists at $CONFIG_DIR/mcp-router.yaml"
        warn "Skipping configuration creation"
        return
    fi
    
    # Create example configuration
    cat > "$CONFIG_DIR/mcp-router.yaml.example" <<'EOF'
# MCP Router Configuration
# See docs/configuration.md for all options

version: 1

gateway:
  # Gateway URL - update with your gateway address
  url: wss://mcp-gateway.example.com
  
  # Authentication configuration
  auth:
    # Option 1: Bearer token with secure storage (recommended)
    type: bearer
    token_secure_key: "gateway-token"  # Run: mcp-router token set --name gateway-token
    
    # Option 2: OAuth2
    # type: oauth2
    # client_id: "your-client-id"
    # client_secret_secure_key: "oauth-secret"
    # token_endpoint: "https://auth.example.com/token"
    # scopes: ["mcp:read", "mcp:write"]
    
    # Option 3: mTLS
    # type: mtls
  
  # Connection settings
  connection:
    timeout_ms: 5000
    keepalive_interval_ms: 30000
    
    # Connection pooling for high throughput
    pool:
      enabled: false  # Enable for production
      min_size: 2
      max_size: 10
  
  # TLS configuration
  tls:
    verify: true
    # ca_file: "/etc/ssl/certs/ca-certificates.crt"
    # For mTLS:
    # client_cert: "/path/to/client.crt"
    # client_key: "/path/to/client.key"

# Local settings
local:
  request_timeout_ms: 30000
  max_concurrent_requests: 100
  
  # Rate limiting (client-side)
  rate_limit:
    enabled: false
    requests_per_sec: 100
    burst: 200

# Logging configuration
logging:
  level: info  # debug, info, warn, error
  format: text # json, text

# Metrics endpoint
metrics:
  enabled: true
  endpoint: "localhost:9091"

# Advanced settings
advanced:
  # Protocol selection
  protocol: "websocket"  # websocket or tcp
  
  # Enable for binary protocol performance
  # protocol: "tcp"
  # tcp_address: "gateway.example.com:8444"
  
  # Compression (for binary protocol)
  compression:
    enabled: false
    algorithm: "gzip"  # gzip or zstd
    level: 6
EOF
    
    success "Created example configuration at $CONFIG_DIR/mcp-router.yaml.example"
    
    # Copy example to actual config if interactive
    if [[ "$INTERACTIVE_SETUP" == "true" ]]; then
        cp "$CONFIG_DIR/mcp-router.yaml.example" "$CONFIG_DIR/mcp-router.yaml"
        success "Created default configuration at $CONFIG_DIR/mcp-router.yaml"
    fi
}

# Run interactive setup wizard
run_setup_wizard() {
    if [[ "$INTERACTIVE_SETUP" != "true" ]]; then
        return
    fi
    
    log "Running interactive setup wizard..."
    
    # Run the setup command
    if "${INSTALL_DIR}/${BINARY_NAME}" setup --config "$CONFIG_DIR/mcp-router.yaml"; then
        success "Setup completed successfully"
    else
        warn "Setup wizard failed or was cancelled"
        warn "You can run 'mcp-router setup' later to complete configuration"
    fi
}

# Main installation process
main() {
    echo ""
    echo "MCP Local Router Installer"
    echo "=========================="
    echo ""
    
    # Step 1: Check system
    log "[1/8] Checking system..."
    check_permissions
    SYSTEM=$(detect_system)
    success "Detected system: $SYSTEM"
    
    # Step 2: Check dependencies
    log "[2/8] Checking dependencies..."
    check_dependencies
    
    # Step 3: Get latest version
    log "[3/8] Getting latest version..."
    VERSION=$(get_latest_version)
    success "Latest version: $VERSION"
    
    # Step 4: Download and install binary
    log "[4/8] Installing binary..."
    install_binary "$VERSION" "$SYSTEM"
    
    # Step 5: Install completions
    log "[5/8] Installing shell completions..."
    install_completions
    
    # Step 6: Setup secure storage
    log "[6/8] Setting up secure storage..."
    setup_secure_storage
    
    # Step 7: Create configuration
    log "[7/8] Creating configuration..."
    create_initial_config
    
    # Step 8: Run setup wizard
    log "[8/8] Running setup wizard..."
    run_setup_wizard
    
    # Done!
    echo ""
    success "Installation complete!"
    echo ""
    echo "Next steps:"
    if [[ "$INTERACTIVE_SETUP" != "true" ]]; then
        echo "  1. Store your authentication token securely:"
        echo "     ${BINARY_NAME} token set --name gateway-token"
        echo ""
        echo "  2. Configure your gateway connection:"
        echo "     Edit: $CONFIG_DIR/mcp-router.yaml"
        echo ""
        echo "  3. Test your connection:"
        echo "     ${BINARY_NAME} test"
    else
        echo "  1. Test your connection:"
        echo "     ${BINARY_NAME} test"
        echo ""
        echo "  2. View configuration:"
        echo "     ${BINARY_NAME} config show"
    fi
    echo ""
    echo "Common commands:"
    echo "  ${BINARY_NAME} setup              - Run setup wizard"
    echo "  ${BINARY_NAME} token list         - List stored tokens"
    echo "  ${BINARY_NAME} token doctor       - Diagnose secure storage"
    echo "  ${BINARY_NAME} --help            - Show all commands"
    echo ""
    echo "Documentation: https://github.com/${GITHUB_REPO}/tree/main/docs"
    echo ""
    echo "To uninstall later, run:"
    echo "  curl -sSL https://raw.githubusercontent.com/${GITHUB_REPO}/main/services/router/uninstall.sh | sudo bash"
    echo ""
}

# Run main function
main "$@"