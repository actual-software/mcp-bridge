#!/bin/bash

# MCP Bridge Stop Script
# Gracefully stop all MCP Bridge services

set -euo pipefail

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

# Configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"
FORCE=false
QUIET=false

# Logging
log() { [[ "$QUIET" == "false" ]] && echo -e "${GREEN}[OK]${NC} $*"; }
error() { echo -e "${RED}[ERROR]${NC} $*" >&2; }
warn() { [[ "$QUIET" == "false" ]] && echo -e "${YELLOW}[WARN]${NC} $*"; }
info() { [[ "$QUIET" == "false" ]] && echo -e "${CYAN}[INFO]${NC} $*"; }

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -f|--force)
            FORCE=true
            shift
            ;;
        -q|--quiet)
            QUIET=true
            shift
            ;;
        -h|--help)
            cat << EOF
Usage: $0 [OPTIONS]

Stop all MCP Bridge services

Options:
    -f, --force    Force stop (kill -9) if graceful stop fails
    -q, --quiet    Suppress output
    -h, --help     Show this help message

EOF
            exit 0
            ;;
        *)
            error "Unknown option: $1"
            exit 1
            ;;
    esac
done

info "Stopping MCP Bridge services..."

# Stop systemd services if they exist
stop_systemd_service() {
    local service=$1
    
    if systemctl is-active "$service" &>/dev/null; then
        info "Stopping $service (systemd)..."
        sudo systemctl stop "$service"
        log "$service stopped"
    fi
}

# Stop Docker services
stop_docker_services() {
    if [[ -f "$PROJECT_ROOT/docker-compose.yml" ]] && command -v docker-compose &>/dev/null; then
        if docker-compose ps -q | grep -q .; then
            info "Stopping Docker services..."
            docker-compose down
            log "Docker services stopped"
        fi
    fi
}

# Stop Kubernetes deployments
stop_kubernetes_services() {
    if command -v kubectl &>/dev/null && kubectl get namespace mcp-system &>/dev/null 2>&1; then
        info "Scaling down Kubernetes deployments..."
        kubectl scale deployment --all --replicas=0 -n mcp-system &>/dev/null
        log "Kubernetes deployments scaled down"
    fi
}

# Stop process by PID file
stop_by_pidfile() {
    local pidfile=$1
    local name=$2
    
    if [[ -f "$pidfile" ]]; then
        local pid=$(cat "$pidfile")
        if kill -0 "$pid" 2>/dev/null; then
            info "Stopping $name (PID: $pid)..."
            kill -TERM "$pid"
            
            # Wait for graceful shutdown
            local count=0
            while kill -0 "$pid" 2>/dev/null && [[ $count -lt 10 ]]; do
                sleep 1
                ((count++))
            done
            
            # Force kill if still running
            if kill -0 "$pid" 2>/dev/null; then
                if [[ "$FORCE" == "true" ]]; then
                    warn "Force killing $name..."
                    kill -9 "$pid"
                else
                    warn "$name did not stop gracefully. Use --force to force stop."
                fi
            else
                log "$name stopped"
            fi
            
            rm -f "$pidfile"
        else
            rm -f "$pidfile"
        fi
    fi
}

# Stop process by name
stop_by_name() {
    local process=$1
    
    local pids=$(pgrep -f "$process" 2>/dev/null || true)
    if [[ -n "$pids" ]]; then
        info "Stopping $process processes..."
        
        # Send TERM signal
        kill -TERM $pids 2>/dev/null || true
        
        # Wait for graceful shutdown
        local count=0
        while pgrep -f "$process" >/dev/null 2>&1 && [[ $count -lt 10 ]]; do
            sleep 1
            ((count++))
        done
        
        # Force kill if still running
        if pgrep -f "$process" >/dev/null 2>&1; then
            if [[ "$FORCE" == "true" ]]; then
                warn "Force killing $process..."
                pkill -9 -f "$process"
            else
                warn "$process did not stop gracefully. Use --force to force stop."
            fi
        else
            log "$process stopped"
        fi
    fi
}

# Main execution

# 1. Stop systemd services
stop_systemd_service "mcp-gateway"
stop_systemd_service "mcp-router"

# 2. Stop Docker services
stop_docker_services

# 3. Stop Kubernetes services
stop_kubernetes_services

# 4. Stop by PID files
stop_by_pidfile "$PROJECT_ROOT/.gateway.pid" "Gateway"
stop_by_pidfile "$PROJECT_ROOT/.router.pid" "Router"

# 5. Stop by process name
stop_by_name "mcp-gateway"
stop_by_name "mcp-router"

# 6. Clean up any orphaned ports
cleanup_ports() {
    local ports=(8080 8081 9090 9091)
    
    for port in "${ports[@]}"; do
        if lsof -i ":$port" &>/dev/null; then
            local pid=$(lsof -t -i ":$port" 2>/dev/null || true)
            if [[ -n "$pid" ]]; then
                warn "Port $port still in use by PID $pid"
                if [[ "$FORCE" == "true" ]]; then
                    kill -9 $pid 2>/dev/null || true
                    log "Killed process on port $port"
                fi
            fi
        fi
    done
}

cleanup_ports

info "All MCP Bridge services stopped"

# Clean up temporary files
if [[ -d "$PROJECT_ROOT/tmp" ]]; then
    rm -rf "$PROJECT_ROOT/tmp"
fi

# Show status
if [[ "$QUIET" == "false" ]]; then
    echo -e "\n${CYAN}Service Status:${NC}"
    
    # Check if any MCP processes are still running
    if pgrep -f "mcp-" >/dev/null 2>&1; then
        warn "Some MCP processes may still be running:"
        ps aux | grep -E "mcp-" | grep -v grep || true
    else
        log "No MCP processes running"
    fi
    
    # Check ports
    echo -e "\n${CYAN}Port Status:${NC}"
    for port in 8080 8081 9090 9091; do
        if lsof -i ":$port" &>/dev/null; then
            warn "Port $port is still in use"
        else
            log "Port $port is free"
        fi
    done
fi