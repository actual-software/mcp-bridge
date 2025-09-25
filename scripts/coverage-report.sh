#!/bin/bash

# MCP Bridge - Comprehensive Coverage Report Generator
# This script provides accurate, production-focused test coverage metrics

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
PURPLE='\033[0;35m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color
BOLD='\033[1m'

# Configuration
COVERAGE_THRESHOLD=60
CORE_THRESHOLD=70
GATEWAY_THRESHOLD=60
ROUTER_THRESHOLD=60

echo -e "${BOLD}${BLUE}🔍 MCP Bridge - Production Coverage Report${NC}"
echo -e "================================================="
echo ""

# Helper functions
print_header() {
    echo -e "${BOLD}${PURPLE}$1${NC}"
}

print_success() {
    echo -e "${GREEN}✅ $1${NC}"
}

print_warning() {
    echo -e "${YELLOW}⚠️  $1${NC}"
}

print_error() {
    echo -e "${RED}❌ $1${NC}"
}

print_info() {
    echo -e "${CYAN}ℹ️  $1${NC}"
}

# Function to extract coverage percentage from go tool cover output
extract_coverage() {
    local coverage_file="$1"
    if [[ -f "$coverage_file" ]]; then
        go tool cover -func="$coverage_file" | grep total | awk '{print $NF}' | sed 's/%//'
    else
        echo "0"
    fi
}

# Function to check if coverage meets threshold
check_threshold() {
    local coverage="$1"
    local threshold="$2"
    local name="$3"
    
    if (( $(echo "$coverage >= $threshold" | bc -l) )); then
        print_success "$name: ${coverage}% (≥ ${threshold}%)"
        return 0
    else
        print_error "$name: ${coverage}% (< ${threshold}%)"
        return 1
    fi
}

# Function to run production-focused coverage
run_production_coverage() {
    local service="$1"
    local service_path="services/$service"
    
    if [[ ! -d "$service_path" ]]; then
        print_error "Service directory $service_path not found"
        return 1
    fi
    
    print_header "📊 Running $service coverage..."
    
    cd "$service_path"
    
    # Run tests with coverage, excluding test files and infrastructure
    if go test -coverprofile=production-coverage.out \
        -covermode=count \
        -timeout=5m \
        $(go list ./... | grep -v "/test/" | grep -v "/testutil/" | grep -v "/examples/") 2>/dev/null; then
        
        local coverage=$(extract_coverage "production-coverage.out")
        echo -e "  ${BOLD}Production Code Coverage: ${coverage}%${NC}"
        
        # Generate detailed breakdown
        echo ""
        echo "  📋 Package Breakdown:"
        go tool cover -func=production-coverage.out | grep -v total | while read line; do
            echo "    $line"
        done
        
        cd - >/dev/null
        echo "$coverage"
        return 0
    else
        print_warning "$service tests failed or timed out"
        cd - >/dev/null
        echo "0"
        return 1
    fi
}

# Function to calculate core libraries coverage
run_core_libraries_coverage() {
    print_header "🏗️  Core Libraries Coverage"
    
    # Test core libraries (pkg/common, internal/secure)
    local core_packages=(
        "./pkg/common/..."
        "./internal/secure/..."
    )
    
    local temp_coverage="core-libs-coverage.out"
    
    if go test -coverprofile="$temp_coverage" \
        -covermode=count \
        -timeout=3m \
        "${core_packages[@]}" 2>/dev/null; then
        
        local coverage=$(extract_coverage "$temp_coverage")
        echo -e "  ${BOLD}Core Libraries Coverage: ${coverage}%${NC}"
        
        echo ""
        echo "  📋 Core Package Breakdown:"
        go tool cover -func="$temp_coverage" | grep -E "(pkg/common|internal/secure)" | while read line; do
            echo "    $line"
        done
        
        rm -f "$temp_coverage"
        echo "$coverage"
        return 0
    else
        print_warning "Core libraries tests failed"
        rm -f "$temp_coverage"
        echo "0"
        return 1
    fi
}

# Main execution
main() {
    echo -e "${BOLD}📈 PRODUCTION-FOCUSED COVERAGE ANALYSIS${NC}"
    echo ""
    
    # Store original directory
    local original_dir=$(pwd)
    
    # Initialize results
    local gateway_coverage=0
    local router_coverage=0
    local core_coverage=0
    local overall_pass=true
    
    # 1. Gateway Service Coverage
    print_header "🌐 Gateway Service"
    gateway_coverage=$(run_production_coverage "gateway")
    if ! check_threshold "$gateway_coverage" "$GATEWAY_THRESHOLD" "Gateway Service"; then
        overall_pass=false
    fi
    echo ""
    
    # 2. Router Service Coverage
    print_header "🔧 Router Service"
    router_coverage=$(run_production_coverage "router")
    if ! check_threshold "$router_coverage" "$ROUTER_THRESHOLD" "Router Service"; then
        overall_pass=false
    fi
    echo ""
    
    # 3. Core Libraries Coverage
    core_coverage=$(run_core_libraries_coverage)
    if ! check_threshold "$core_coverage" "$CORE_THRESHOLD" "Core Libraries"; then
        overall_pass=false
    fi
    echo ""
    
    # 4. Summary Report
    print_header "📊 COVERAGE SUMMARY"
    echo ""
    echo -e "${BOLD}Production Services:${NC}"
    echo -e "  Gateway Service:    ${gateway_coverage}%"
    echo -e "  Router Service:     ${router_coverage}%"
    echo -e "  Core Libraries:     ${core_coverage}%"
    echo ""
    
    # Calculate weighted average (services are more important)
    local weighted_avg
    weighted_avg=$(echo "scale=1; ($gateway_coverage * 0.4 + $router_coverage * 0.4 + $core_coverage * 0.2)" | bc)
    
    echo -e "${BOLD}Weighted Production Average: ${weighted_avg}%${NC}"
    echo -e "${CYAN}(Gateway 40% + Router 40% + Core 20%)${NC}"
    echo ""
    
    # 5. Thresholds Check
    print_header "🎯 THRESHOLD ANALYSIS"
    echo ""
    
    if (( $(echo "$weighted_avg >= $COVERAGE_THRESHOLD" | bc -l) )); then
        print_success "Overall weighted coverage ${weighted_avg}% meets ${COVERAGE_THRESHOLD}% threshold"
    else
        print_error "Overall weighted coverage ${weighted_avg}% below ${COVERAGE_THRESHOLD}% threshold"
        overall_pass=false
    fi
    
    echo ""
    
    # 6. Recommendations
    print_header "💡 COVERAGE INSIGHTS"
    echo ""
    
    if (( $(echo "$gateway_coverage >= 65" | bc -l) )); then
        print_success "Gateway coverage is excellent"
    elif (( $(echo "$gateway_coverage >= 60" | bc -l) )); then
        print_info "Gateway coverage is good, room for improvement"
    else
        print_warning "Gateway coverage needs attention"
    fi
    
    if (( $(echo "$router_coverage >= 75" | bc -l) )); then
        print_success "Router coverage is excellent"
    elif (( $(echo "$router_coverage >= 60" | bc -l) )); then
        print_success "Router coverage meets target"
    else
        print_warning "Router coverage needs attention"
    fi
    
    if (( $(echo "$core_coverage >= 85" | bc -l) )); then
        print_success "Core libraries coverage is outstanding"
    elif (( $(echo "$core_coverage >= 70" | bc -l) )); then
        print_success "Core libraries coverage is good"
    else
        print_warning "Core libraries coverage needs improvement"
    fi
    
    echo ""
    
    # 7. What to Advertise
    print_header "📢 COVERAGE METRICS TO ADVERTISE"
    echo ""
    echo -e "${BOLD}Recommended metrics for documentation/badges:${NC}"
    echo ""
    echo -e "  🎯 ${BOLD}\"Production Coverage: ${weighted_avg}%\"${NC}"
    echo -e "     (Weighted average of production services)"
    echo ""
    echo -e "  🔧 ${BOLD}\"Router Service: ${router_coverage}%\"${NC}"
    echo -e "     (Primary service with comprehensive test suite)"
    echo ""
    echo -e "  🌐 ${BOLD}\"Gateway Service: ${gateway_coverage}%\"${NC}"
    echo -e "     (API gateway with integration tests)"
    echo ""
    echo -e "  🏗️  ${BOLD}\"Core Libraries: ${core_coverage}%\"${NC}"
    echo -e "     (Shared utilities and security components)"
    echo ""
    
    # 8. Badge Recommendations
    print_header "🏷️  BADGE RECOMMENDATIONS"
    echo ""
    
    local badge_color="red"
    local badge_status="failing"
    
    if (( $(echo "$weighted_avg >= 80" | bc -l) )); then
        badge_color="brightgreen"
        badge_status="excellent"
    elif (( $(echo "$weighted_avg >= 70" | bc -l) )); then
        badge_color="green"
        badge_status="good"
    elif (( $(echo "$weighted_avg >= 60" | bc -l) )); then
        badge_color="yellow"
        badge_status="passing"
    fi
    
    echo -e "  Shields.io Badge URL:"
    echo -e "  ${CYAN}https://img.shields.io/badge/coverage-${weighted_avg}%25-${badge_color}${NC}"
    echo ""
    echo -e "  README.md Badge Markdown:"
    echo -e "  ${CYAN}![Coverage](https://img.shields.io/badge/coverage-${weighted_avg}%25-${badge_color})${NC}"
    echo ""
    
    # 9. Final Status
    print_header "🏁 FINAL RESULT"
    echo ""
    
    if $overall_pass; then
        print_success "All coverage thresholds met! 🎉"
        echo -e "${GREEN}${BOLD}✅ COVERAGE STATUS: PASSING${NC}"
        cd "$original_dir"
        return 0
    else
        print_error "Some coverage thresholds not met"
        echo -e "${RED}${BOLD}❌ COVERAGE STATUS: NEEDS IMPROVEMENT${NC}"
        cd "$original_dir"
        return 1
    fi
}

# Create coverage directory for artifacts
mkdir -p coverage-reports

# Run main function
main "$@"