#!/bin/bash

# MCP Bridge Quick Start Script
# Get up and running with MCP Bridge in under 5 minutes!

set -euo pipefail

# Colors for beautiful output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
MAGENTA='\033[0;35m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m' # No Color

# Emoji for fun (can be disabled)
EMOJI_ENABLED=${EMOJI_ENABLED:-true}
if [[ "$EMOJI_ENABLED" == "true" ]]; then
    ROCKET="🚀"
    CHECK="✅"
    CROSS="❌"
    WARN="⚠️"
    INFO="ℹ️"
    TOOLS="🛠️"
    PACKAGE="📦"
    DOCKER="🐳"
    K8S="☸️"
    COFFEE="☕"
    PARTY="🎉"
else
    ROCKET="=>"
    CHECK="[OK]"
    CROSS="[ERROR]"
    WARN="[WARN]"
    INFO="[INFO]"
    TOOLS="[TOOLS]"
    PACKAGE="[PACKAGE]"
    DOCKER="[DOCKER]"
    K8S="[K8S]"
    COFFEE="[WAIT]"
    PARTY="[SUCCESS]"
fi

# Script configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$SCRIPT_DIR"
QUICKSTART_CONFIG="${PROJECT_ROOT}/.quickstart"
LOG_FILE="${PROJECT_ROOT}/quickstart.log"

# Default options
VERBOSE=false
SKIP_DEPS=false
SKIP_DOCKER=false
SKIP_BUILD=false
DEMO_MODE=false
ENVIRONMENT="development"

# Logging functions
log() {
    echo -e "${GREEN}$CHECK${NC} $*" | tee -a "$LOG_FILE"
}

error() {
    echo -e "${RED}$CROSS${NC} $*" | tee -a "$LOG_FILE" >&2
}

warn() {
    echo -e "${YELLOW}$WARN${NC} $*" | tee -a "$LOG_FILE"
}

info() {
    echo -e "${CYAN}$INFO${NC} $*" | tee -a "$LOG_FILE"
}

debug() {
    if [[ "$VERBOSE" == "true" ]]; then
        echo -e "${MAGENTA}[DEBUG]${NC} $*" | tee -a "$LOG_FILE"
    fi
}

# Print banner
print_banner() {
    clear
    echo -e "${BOLD}${CYAN}"
    cat << 'EOF'
 __  __  ____ ____    ____       _     _            
|  \/  |/ ___|  _ \  | __ ) _ __(_) __| | __ _  ___ 
| |\/| | |   | |_) | |  _ \| '__| |/ _` |/ _` |/ _ \
| |  | | |___|  __/  | |_) | |  | | (_| | (_| |  __/
|_|  |_|\____|_|     |____/|_|  |_|\__,_|\__, |\___|
                                          |___/      
EOF
    echo -e "${NC}"
    echo -e "${BOLD}Welcome to MCP Bridge Quick Start! ${ROCKET}${NC}"
    echo -e "This script will help you get up and running in minutes.\n"
}

# Show progress spinner
spinner() {
    local pid=$1
    local delay=0.1
    local spinstr='⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏'
    while [ "$(ps a | awk '{print $1}' | grep $pid)" ]; do
        local temp=${spinstr#?}
        printf " [%c]  " "$spinstr"
        local spinstr=$temp${spinstr%"$temp"}
        sleep $delay
        printf "\b\b\b\b\b\b"
    done
    printf "    \b\b\b\b"
}

# Check system requirements
check_system() {
    echo -e "\n${BOLD}${TOOLS} Checking System Requirements${NC}\n"
    
    local os_type=""
    local os_version=""
    local arch=$(uname -m)
    
    # Detect OS
    if [[ "$OSTYPE" == "darwin"* ]]; then
        os_type="macOS"
        os_version=$(sw_vers -productVersion)
    elif [[ "$OSTYPE" == "linux-gnu"* ]]; then
        os_type="Linux"
        if [[ -f /etc/os-release ]]; then
            . /etc/os-release
            os_version="$NAME $VERSION"
        fi
    else
        error "Unsupported operating system: $OSTYPE"
        exit 1
    fi
    
    info "Operating System: $os_type $os_version ($arch)"
    
    # Check available resources
    if [[ "$os_type" == "macOS" ]]; then
        local mem_gb=$(( $(sysctl -n hw.memsize) / 1073741824 ))
        local cpu_cores=$(sysctl -n hw.ncpu)
    else
        local mem_gb=$(( $(grep MemTotal /proc/meminfo | awk '{print $2}') / 1048576 ))
        local cpu_cores=$(nproc)
    fi
    
    info "Available Resources: ${cpu_cores} CPU cores, ${mem_gb}GB RAM"
    
    if [[ $mem_gb -lt 4 ]]; then
        warn "Less than 4GB RAM available. You may experience performance issues."
    fi
    
    echo ""
}

# Check and install dependencies
check_dependencies() {
    echo -e "${BOLD}${PACKAGE} Checking Dependencies${NC}\n"
    
    local missing_deps=()
    local optional_missing=()
    
    # Required dependencies
    local required_deps=(
        "git:Git version control"
        "go:Go programming language (1.21+)"
        "make:Build automation tool"
    )
    
    # Optional dependencies
    local optional_deps=(
        "docker:Container runtime"
        "docker-compose:Container orchestration"
        "kubectl:Kubernetes CLI"
        "redis-cli:Redis CLI"
        "jq:JSON processor"
        "yq:YAML processor"
    )
    
    # Check required dependencies
    for dep in "${required_deps[@]}"; do
        local cmd="${dep%%:*}"
        local desc="${dep##*:}"
        
        if command -v "$cmd" &> /dev/null; then
            local version=$($cmd --version 2>&1 | head -1)
            log "$desc: ${GREEN}Installed${NC} ($(echo $version | cut -d' ' -f3))"
        else
            error "$desc: ${RED}Not found${NC}"
            missing_deps+=("$cmd")
        fi
    done
    
    echo ""
    
    # Check optional dependencies
    for dep in "${optional_deps[@]}"; do
        local cmd="${dep%%:*}"
        local desc="${dep##*:}"
        
        if command -v "$cmd" &> /dev/null; then
            log "$desc: ${GREEN}Installed${NC}"
        else
            warn "$desc: ${YELLOW}Not found${NC} (optional)"
            optional_missing+=("$cmd")
        fi
    done
    
    echo ""
    
    # Handle missing dependencies
    if [[ ${#missing_deps[@]} -gt 0 ]]; then
        error "Missing required dependencies: ${missing_deps[*]}"
        echo ""
        
        if [[ "$os_type" == "macOS" ]] && command -v brew &> /dev/null; then
            echo "You can install them using Homebrew:"
            echo -e "${CYAN}  brew install ${missing_deps[*]}${NC}"
        elif [[ "$os_type" == "Linux" ]]; then
            echo "Please install the missing dependencies using your package manager."
        fi
        
        exit 1
    fi
    
    # Offer to install optional dependencies
    if [[ ${#optional_missing[@]} -gt 0 ]] && [[ "$SKIP_DEPS" == "false" ]]; then
        echo -e "${YELLOW}Would you like to install optional dependencies?${NC}"
        echo "Optional tools: ${optional_missing[*]}"
        read -p "Install? (y/N): " -n 1 -r
        echo ""
        
        if [[ $REPLY =~ ^[Yy]$ ]]; then
            install_optional_deps "${optional_missing[@]}"
        fi
    fi
}

# Install optional dependencies
install_optional_deps() {
    local deps=("$@")
    
    echo -e "\n${BOLD}Installing Optional Dependencies...${NC}\n"
    
    for dep in "${deps[@]}"; do
        case "$dep" in
            docker)
                if [[ "$os_type" == "macOS" ]]; then
                    info "Please install Docker Desktop from https://docker.com"
                else
                    curl -fsSL https://get.docker.com | sh
                fi
                ;;
            docker-compose)
                if command -v docker &> /dev/null; then
                    docker compose version &> /dev/null || \
                    curl -L "https://github.com/docker/compose/releases/latest/download/docker-compose-$(uname -s)-$(uname -m)" -o /usr/local/bin/docker-compose
                fi
                ;;
            jq)
                if [[ "$os_type" == "macOS" ]]; then
                    brew install jq 2>/dev/null || true
                else
                    sudo apt-get install -y jq 2>/dev/null || sudo yum install -y jq
                fi
                ;;
            *)
                warn "Skipping $dep - manual installation required"
                ;;
        esac
    done
}

# Setup development environment
setup_environment() {
    echo -e "\n${BOLD}${TOOLS} Setting Up Development Environment${NC}\n"
    
    # Create necessary directories
    local dirs=(
        "configs"
        "logs"
        "data"
        "backups"
        "services/gateway/bin"
        "services/router/bin"
    )
    
    for dir in "${dirs[@]}"; do
        if [[ ! -d "$PROJECT_ROOT/$dir" ]]; then
            mkdir -p "$PROJECT_ROOT/$dir"
            debug "Created directory: $dir"
        fi
    done
    
    log "Directory structure created"
    
    # Generate default configurations if they don't exist
    if [[ ! -f "$PROJECT_ROOT/configs/gateway.yaml" ]]; then
        generate_gateway_config
        log "Generated gateway configuration"
    fi
    
    if [[ ! -f "$PROJECT_ROOT/configs/router.yaml" ]]; then
        generate_router_config
        log "Generated router configuration"
    fi
    
    # Setup environment variables
    if [[ ! -f "$PROJECT_ROOT/.env" ]]; then
        generate_env_file
        log "Generated .env file"
    fi
    
    echo ""
}

# Generate gateway configuration
generate_gateway_config() {
    cat > "$PROJECT_ROOT/configs/gateway.yaml" << 'EOF'
# MCP Gateway Configuration (Development)
server:
  host: 0.0.0.0
  port: 8080
  read_timeout: 30s
  write_timeout: 30s

# TLS Configuration (disabled for development)
tls:
  enabled: false

# Authentication
auth:
  - type: bearer
    enabled: true
    token: "dev-token-12345"

# Routing
routing:
  default_backend: local
  backends:
    - name: local
      url: http://localhost:8081
      timeout: 10s

# Rate Limiting
rate_limit:
  enabled: true
  requests_per_second: 100
  burst: 200

# Sessions
sessions:
  provider: memory
  ttl: 1h

# Health Checks
health:
  enabled: true
  port: 8080
  path: /health
  check_interval: 10s

# Logging
logging:
  level: debug
  format: json

# Metrics
metrics:
  enabled: true
  port: 9090
  path: /metrics
EOF
}

# Generate router configuration
generate_router_config() {
    cat > "$PROJECT_ROOT/configs/router.yaml" << 'EOF'
# MCP Router Configuration (Development)
router:
  mode: development

# Gateway Connection
gateway_pool:
  endpoints:
    - url: ws://localhost:8080
      weight: 100
  connection_pool:
    size: 10
    min_idle: 2
    max_idle_time: 5m
    health_check_interval: 30s
  retry:
    enabled: true
    max_attempts: 3
    initial_interval: 100ms
    max_interval: 5s

# Authentication
auth:
  type: bearer
  token: "dev-token-12345"

# Secure Storage
secure_storage:
  enabled: false  # Disabled for development

# Metrics
metrics:
  enabled: true
  port: 9091
  path: /metrics

# Logging
logging:
  level: debug
  format: json
EOF
}

# Generate .env file
generate_env_file() {
    cat > "$PROJECT_ROOT/.env" << EOF
# MCP Bridge Environment Variables
ENVIRONMENT=development

# Service Ports
GATEWAY_PORT=8080
ROUTER_PORT=8081
METRICS_PORT=9090

# Redis (if using)
REDIS_URL=localhost:6379
REDIS_PASSWORD=

# Authentication
AUTH_TOKEN=dev-token-12345

# Logging
LOG_LEVEL=debug
LOG_FORMAT=json

# Development Settings
DEV_MODE=true
HOT_RELOAD=true
EOF
}

# Build the project
build_project() {
    echo -e "\n${BOLD}🔨 Building MCP Bridge${NC}\n"
    
    # Check if Go modules are initialized
    if [[ ! -f "$PROJECT_ROOT/go.mod" ]]; then
        warn "Go modules not initialized. Initializing..."
        cd "$PROJECT_ROOT"
        go mod init github.com/poiley/mcp-bridge
        go mod tidy
    fi
    
    # Build Gateway
    info "Building Gateway service..."
    if [[ -d "$PROJECT_ROOT/services/gateway" ]]; then
        cd "$PROJECT_ROOT/services/gateway"
        go build -o bin/mcp-gateway ./cmd/mcp-gateway &
        local gateway_pid=$!
        spinner $gateway_pid
        wait $gateway_pid
        if [[ $? -eq 0 ]]; then
            log "Gateway built successfully"
        else
            error "Gateway build failed"
        fi
    else
        warn "Gateway source not found, skipping build"
    fi
    
    # Build Router
    info "Building Router service..."
    if [[ -d "$PROJECT_ROOT/services/router" ]]; then
        cd "$PROJECT_ROOT/services/router"
        go build -o bin/mcp-router ./cmd/mcp-router &
        local router_pid=$!
        spinner $router_pid
        wait $router_pid
        if [[ $? -eq 0 ]]; then
            log "Router built successfully"
        else
            error "Router build failed"
        fi
    else
        warn "Router source not found, skipping build"
    fi
    
    cd "$PROJECT_ROOT"
    echo ""
}

# Setup Docker environment
setup_docker() {
    if [[ "$SKIP_DOCKER" == "true" ]] || ! command -v docker &> /dev/null; then
        return 0
    fi
    
    echo -e "\n${BOLD}${DOCKER} Setting Up Docker Environment${NC}\n"
    
    # Check if Docker is running
    if ! docker info &> /dev/null; then
        warn "Docker is not running. Please start Docker and try again."
        return 1
    fi
    
    # Create docker-compose.yml if it doesn't exist
    if [[ ! -f "$PROJECT_ROOT/docker-compose.yml" ]]; then
        generate_docker_compose
        log "Generated docker-compose.yml"
    fi
    
    # Pull required images
    info "Pulling Docker images..."
    docker-compose pull &> /dev/null &
    local pull_pid=$!
    spinner $pull_pid
    wait $pull_pid
    log "Docker images ready"
    
    echo ""
}

# Generate docker-compose.yml
generate_docker_compose() {
    cat > "$PROJECT_ROOT/docker-compose.yml" << 'EOF'
version: '3.8'

services:
  redis:
    image: redis:7-alpine
    container_name: mcp-redis
    ports:
      - "6379:6379"
    volumes:
      - redis-data:/data
    networks:
      - mcp-network

  prometheus:
    image: prom/prometheus:latest
    container_name: mcp-prometheus
    ports:
      - "9099:9090"
    volumes:
      - ./monitoring/prometheus:/etc/prometheus
      - prometheus-data:/prometheus
    command:
      - '--config.file=/etc/prometheus/prometheus.yml'
      - '--storage.tsdb.path=/prometheus'
    networks:
      - mcp-network

  grafana:
    image: grafana/grafana:latest
    container_name: mcp-grafana
    ports:
      - "3000:3000"
    environment:
      - GF_SECURITY_ADMIN_PASSWORD=admin
      - GF_INSTALL_PLUGINS=redis-datasource
    volumes:
      - grafana-data:/var/lib/grafana
      - ./monitoring/grafana/dashboards:/etc/grafana/provisioning/dashboards
    networks:
      - mcp-network

  jaeger:
    image: jaegertracing/all-in-one:latest
    container_name: mcp-jaeger
    ports:
      - "6831:6831/udp"
      - "14268:14268"
      - "16686:16686"
    environment:
      - COLLECTOR_ZIPKIN_HOST_PORT=:9411
    networks:
      - mcp-network

volumes:
  redis-data:
  prometheus-data:
  grafana-data:

networks:
  mcp-network:
    driver: bridge
EOF
}

# Start services
start_services() {
    echo -e "\n${BOLD}🚀 Starting Services${NC}\n"
    
    # Start Docker services if available
    if [[ -f "$PROJECT_ROOT/docker-compose.yml" ]] && command -v docker-compose &> /dev/null; then
        info "Starting Docker services..."
        docker-compose up -d redis prometheus grafana &> /dev/null
        log "Docker services started"
    fi
    
    # Start Gateway
    if [[ -x "$PROJECT_ROOT/services/gateway/bin/mcp-gateway" ]]; then
        info "Starting Gateway service..."
        nohup "$PROJECT_ROOT/services/gateway/bin/mcp-gateway" \
            --config "$PROJECT_ROOT/configs/gateway.yaml" \
            > "$PROJECT_ROOT/logs/gateway.log" 2>&1 &
        local gateway_pid=$!
        echo $gateway_pid > "$PROJECT_ROOT/.gateway.pid"
        sleep 2
        
        if kill -0 $gateway_pid 2>/dev/null; then
            log "Gateway started (PID: $gateway_pid)"
        else
            error "Gateway failed to start"
        fi
    fi
    
    # Start Router
    if [[ -x "$PROJECT_ROOT/services/router/bin/mcp-router" ]]; then
        info "Starting Router service..."
        nohup "$PROJECT_ROOT/services/router/bin/mcp-router" \
            --config "$PROJECT_ROOT/configs/router.yaml" \
            > "$PROJECT_ROOT/logs/router.log" 2>&1 &
        local router_pid=$!
        echo $router_pid > "$PROJECT_ROOT/.router.pid"
        sleep 2
        
        if kill -0 $router_pid 2>/dev/null; then
            log "Router started (PID: $router_pid)"
        else
            error "Router failed to start"
        fi
    fi
    
    echo ""
}

# Run health checks
run_health_checks() {
    echo -e "\n${BOLD}🏥 Running Health Checks${NC}\n"
    
    local all_healthy=true
    
    # Check Gateway health
    if curl -s -f http://localhost:8080/health > /dev/null 2>&1; then
        log "Gateway health check: ${GREEN}PASSED${NC}"
    else
        error "Gateway health check: ${RED}FAILED${NC}"
        all_healthy=false
    fi
    
    # Check Router health
    if curl -s -f http://localhost:9091/health > /dev/null 2>&1; then
        log "Router health check: ${GREEN}PASSED${NC}"
    else
        error "Router health check: ${RED}FAILED${NC}"
        all_healthy=false
    fi
    
    # Check metrics endpoints
    if curl -s -f http://localhost:9090/metrics > /dev/null 2>&1; then
        log "Gateway metrics: ${GREEN}AVAILABLE${NC}"
    else
        warn "Gateway metrics: ${YELLOW}NOT AVAILABLE${NC}"
    fi
    
    if curl -s -f http://localhost:9091/metrics > /dev/null 2>&1; then
        log "Router metrics: ${GREEN}AVAILABLE${NC}"
    else
        warn "Router metrics: ${YELLOW}NOT AVAILABLE${NC}"
    fi
    
    # Check Redis if available
    if command -v redis-cli &> /dev/null && redis-cli ping &> /dev/null; then
        log "Redis: ${GREEN}CONNECTED${NC}"
    else
        info "Redis: ${YELLOW}NOT CONNECTED${NC} (optional)"
    fi
    
    echo ""
    
    if [[ "$all_healthy" == "false" ]]; then
        warn "Some services are not healthy. Check logs for details."
        return 1
    fi
    
    return 0
}

# Demo mode - run example requests
run_demo() {
    if [[ "$DEMO_MODE" != "true" ]]; then
        return 0
    fi
    
    echo -e "\n${BOLD}🎭 Running Demo${NC}\n"
    
    info "Sending test request to Gateway..."
    
    # Send a test MCP request
    local response=$(curl -s -X POST http://localhost:8080/mcp \
        -H "Authorization: Bearer dev-token-12345" \
        -H "Content-Type: application/json" \
        -d '{
            "jsonrpc": "2.0",
            "method": "test.echo",
            "params": {"message": "Hello, MCP!"},
            "id": "demo-1"
        }' 2>/dev/null)
    
    if [[ -n "$response" ]]; then
        log "Received response:"
        echo "$response" | jq '.' 2>/dev/null || echo "$response"
    else
        warn "No response received"
    fi
    
    echo ""
}

# Show next steps
show_next_steps() {
    echo -e "\n${BOLD}${PARTY} MCP Bridge is Ready!${NC}\n"
    
    echo -e "${CYAN}${BOLD}Quick Links:${NC}"
    echo -e "  • Gateway Health: ${BLUE}http://localhost:8080/health${NC}"
    echo -e "  • Router Health:  ${BLUE}http://localhost:9091/health${NC}"
    echo -e "  • Metrics:        ${BLUE}http://localhost:9090/metrics${NC}"
    
    if command -v docker &> /dev/null && docker ps | grep -q grafana; then
        echo -e "  • Grafana:        ${BLUE}http://localhost:3000${NC} (admin/admin)"
    fi
    
    if command -v docker &> /dev/null && docker ps | grep -q jaeger; then
        echo -e "  • Jaeger:         ${BLUE}http://localhost:16686${NC}"
    fi
    
    echo -e "\n${CYAN}${BOLD}Useful Commands:${NC}"
    echo -e "  • View logs:      ${GREEN}tail -f logs/gateway.log${NC}"
    echo -e "  • Stop services:  ${GREEN}make stop${NC}"
    echo -e "  • Run tests:      ${GREEN}make test${NC}"
    echo -e "  • Clean all:      ${GREEN}make clean-all${NC}"
    
    echo -e "\n${CYAN}${BOLD}Configuration Files:${NC}"
    echo -e "  • Gateway: ${GREEN}configs/gateway.yaml${NC}"
    echo -e "  • Router:  ${GREEN}configs/router.yaml${NC}"
    echo -e "  • Env:     ${GREEN}.env${NC}"
    
    echo -e "\n${CYAN}${BOLD}Documentation:${NC}"
    echo -e "  • README:         ${GREEN}README.md${NC}"
    echo -e "  • API Docs:       ${GREEN}docs/api.md${NC}"
    echo -e "  • Configuration:  ${GREEN}docs/configuration.md${NC}"
    
    echo -e "\n${YELLOW}${BOLD}Tips:${NC}"
    echo -e "  • Run ${GREEN}make quickstart${NC} anytime to restart"
    echo -e "  • Check ${GREEN}quickstart.log${NC} for detailed output"
    echo -e "  • Join our Discord for help: ${BLUE}discord.gg/mcp-bridge${NC}"
    
    echo ""
}

# Cleanup on exit
cleanup() {
    if [[ -f "$PROJECT_ROOT/.gateway.pid" ]]; then
        local pid=$(cat "$PROJECT_ROOT/.gateway.pid")
        kill $pid 2>/dev/null || true
        rm -f "$PROJECT_ROOT/.gateway.pid"
    fi
    
    if [[ -f "$PROJECT_ROOT/.router.pid" ]]; then
        local pid=$(cat "$PROJECT_ROOT/.router.pid")
        kill $pid 2>/dev/null || true
        rm -f "$PROJECT_ROOT/.router.pid"
    fi
}

# Parse arguments
parse_args() {
    while [[ $# -gt 0 ]]; do
        case $1 in
            -v|--verbose)
                VERBOSE=true
                shift
                ;;
            --skip-deps)
                SKIP_DEPS=true
                shift
                ;;
            --skip-docker)
                SKIP_DOCKER=true
                shift
                ;;
            --skip-build)
                SKIP_BUILD=true
                shift
                ;;
            --demo)
                DEMO_MODE=true
                shift
                ;;
            --env)
                ENVIRONMENT="$2"
                shift 2
                ;;
            -h|--help)
                cat << EOF
Usage: $0 [OPTIONS]

Quick start script for MCP Bridge development

Options:
    -v, --verbose       Enable verbose output
    --skip-deps        Skip dependency installation
    --skip-docker      Skip Docker setup
    --skip-build       Skip building binaries
    --demo             Run demo after setup
    --env ENV          Environment (development/staging/production)
    -h, --help         Show this help message

Examples:
    $0                 # Standard quick start
    $0 --demo          # Quick start with demo
    $0 --skip-docker   # Quick start without Docker

EOF
                exit 0
                ;;
            *)
                error "Unknown option: $1"
                exit 1
                ;;
        esac
    done
}

# Main execution
main() {
    # Initialize
    > "$LOG_FILE"  # Clear log file
    trap cleanup EXIT
    
    # Parse arguments
    parse_args "$@"
    
    # Show banner
    print_banner
    
    # Check system
    check_system
    
    # Check dependencies
    check_dependencies
    
    # Setup environment
    setup_environment
    
    # Build project
    if [[ "$SKIP_BUILD" != "true" ]]; then
        build_project
    fi
    
    # Setup Docker
    setup_docker
    
    # Start services
    start_services
    
    # Run health checks
    if run_health_checks; then
        # Run demo if requested
        run_demo
        
        # Show next steps
        show_next_steps
        
        # Save quickstart state
        cat > "$QUICKSTART_CONFIG" << EOF
LAST_RUN=$(date -u +%Y-%m-%dT%H:%M:%SZ)
ENVIRONMENT=$ENVIRONMENT
DOCKER_ENABLED=$(command -v docker &> /dev/null && echo "true" || echo "false")
EOF
        
        echo -e "${BOLD}${GREEN}$PARTY Setup completed successfully! $PARTY${NC}"
    else
        echo -e "${BOLD}${YELLOW}Setup completed with warnings. Please check the logs.${NC}"
    fi
}

# Run main function
main "$@"