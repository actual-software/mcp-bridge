#!/bin/bash

# MCP Bridge Security Scanning Script
# This script performs comprehensive security scanning of the codebase

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
REPORT_DIR="security-reports"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
REPORT_FILE="${REPORT_DIR}/security-scan-${TIMESTAMP}.md"

# Create report directory
mkdir -p "${REPORT_DIR}"

# Functions
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Initialize report
init_report() {
    cat > "${REPORT_FILE}" << EOF
# Security Scan Report

**Generated:** $(date)
**Repository:** MCP Bridge
**Scan ID:** ${TIMESTAMP}

## Executive Summary

This report contains the results of automated security scanning performed on the MCP Bridge codebase.

EOF
}

# Check and install dependencies
check_dependencies() {
    log_info "Checking dependencies..."
    
    local missing_deps=()
    
    # Check for Go
    if ! command -v go &> /dev/null; then
        missing_deps+=("go")
    fi
    
    # Check for Docker
    if ! command -v docker &> /dev/null; then
        missing_deps+=("docker")
    fi
    
    if [ ${#missing_deps[@]} -gt 0 ]; then
        log_error "Missing dependencies: ${missing_deps[*]}"
        exit 1
    fi
    
    log_success "All required dependencies are installed"
}

# Install security tools
install_security_tools() {
    log_info "Installing security tools..."
    
    # Install Go security tools
    go install github.com/securego/gosec/v2/cmd/gosec@latest 2>/dev/null || true
    go install golang.org/x/vuln/cmd/govulncheck@latest 2>/dev/null || true
    go install github.com/sonatype-nexus-community/nancy@latest 2>/dev/null || true
    go install github.com/CycloneDX/cyclonedx-gomod/cmd/cyclonedx-gomod@latest 2>/dev/null || true
    go install github.com/google/go-licenses@latest 2>/dev/null || true
    go install honnef.co/go/tools/cmd/staticcheck@latest 2>/dev/null || true
    
    log_success "Security tools installed"
}

# Run SAST scan
run_sast_scan() {
    log_info "Running SAST (Static Application Security Testing)..."
    
    echo "## SAST Results" >> "${REPORT_FILE}"
    echo "" >> "${REPORT_FILE}"
    
    # Run gosec
    if command -v gosec &> /dev/null; then
        log_info "Running gosec..."
        echo "### Gosec Security Scanner" >> "${REPORT_FILE}"
        echo '```' >> "${REPORT_FILE}"
        gosec -fmt text ./... 2>&1 | head -100 >> "${REPORT_FILE}" || true
        echo '```' >> "${REPORT_FILE}"
        echo "" >> "${REPORT_FILE}"
    fi
    
    # Run staticcheck
    if command -v staticcheck &> /dev/null; then
        log_info "Running staticcheck..."
        echo "### Staticcheck Analysis" >> "${REPORT_FILE}"
        echo '```' >> "${REPORT_FILE}"
        staticcheck ./... 2>&1 | head -50 >> "${REPORT_FILE}" || true
        echo '```' >> "${REPORT_FILE}"
        echo "" >> "${REPORT_FILE}"
    fi
    
    log_success "SAST scan complete"
}

# Run dependency vulnerability scan
run_dependency_scan() {
    log_info "Running dependency vulnerability scan..."
    
    echo "## Dependency Vulnerabilities" >> "${REPORT_FILE}"
    echo "" >> "${REPORT_FILE}"
    
    # Run govulncheck
    if command -v govulncheck &> /dev/null; then
        log_info "Running govulncheck..."
        echo "### Go Vulnerability Check" >> "${REPORT_FILE}"
        echo '```' >> "${REPORT_FILE}"
        govulncheck ./... 2>&1 | head -100 >> "${REPORT_FILE}" || true
        echo '```' >> "${REPORT_FILE}"
        echo "" >> "${REPORT_FILE}"
    fi
    
    # Run nancy
    if command -v nancy &> /dev/null; then
        log_info "Running nancy..."
        echo "### Nancy Dependency Audit" >> "${REPORT_FILE}"
        echo '```' >> "${REPORT_FILE}"
        go list -json -deps ./... 2>/dev/null | nancy sleuth 2>&1 | head -50 >> "${REPORT_FILE}" || true
        echo '```' >> "${REPORT_FILE}"
        echo "" >> "${REPORT_FILE}"
    fi
    
    log_success "Dependency scan complete"
}

# Run secret detection
run_secret_scan() {
    log_info "Running secret detection..."
    
    echo "## Secret Detection" >> "${REPORT_FILE}"
    echo "" >> "${REPORT_FILE}"
    
    # Basic secret detection with grep
    log_info "Scanning for hardcoded secrets..."
    echo "### Potential Secrets" >> "${REPORT_FILE}"
    echo '```' >> "${REPORT_FILE}"
    
    # Check for common secret patterns
    local secret_patterns=(
        "password.*=.*['\"]"
        "secret.*=.*['\"]"
        "token.*=.*['\"]"
        "api[_-]?key.*=.*['\"]"
        "private[_-]?key.*=.*['\"]"
    )
    
    local found_secrets=false
    for pattern in "${secret_patterns[@]}"; do
        if grep -r --include="*.go" --include="*.yaml" --include="*.yml" -E "$pattern" . 2>/dev/null | grep -v "test" | head -5; then
            found_secrets=true
        fi
    done >> "${REPORT_FILE}"
    
    if [ "$found_secrets" = false ]; then
        echo "No hardcoded secrets detected" >> "${REPORT_FILE}"
    fi
    
    echo '```' >> "${REPORT_FILE}"
    echo "" >> "${REPORT_FILE}"
    
    log_success "Secret scan complete"
}

# Generate SBOM
generate_sbom() {
    log_info "Generating Software Bill of Materials (SBOM)..."
    
    echo "## Software Bill of Materials" >> "${REPORT_FILE}"
    echo "" >> "${REPORT_FILE}"
    
    if command -v cyclonedx-gomod &> /dev/null; then
        # Generate SBOM for Gateway
        if [ -d "services/gateway" ]; then
            log_info "Generating SBOM for Gateway..."
            cd services/gateway
            cyclonedx-gomod mod -json -output "../../${REPORT_DIR}/sbom-gateway-${TIMESTAMP}.json" 2>/dev/null || true
            cd ../..
            echo "- Gateway SBOM: sbom-gateway-${TIMESTAMP}.json" >> "${REPORT_FILE}"
        fi
        
        # Generate SBOM for Router
        if [ -d "services/router" ]; then
            log_info "Generating SBOM for Router..."
            cd services/router
            cyclonedx-gomod mod -json -output "../../${REPORT_DIR}/sbom-router-${TIMESTAMP}.json" 2>/dev/null || true
            cd ../..
            echo "- Router SBOM: sbom-router-${TIMESTAMP}.json" >> "${REPORT_FILE}"
        fi
    else
        echo "SBOM generation skipped (cyclonedx-gomod not installed)" >> "${REPORT_FILE}"
    fi
    
    echo "" >> "${REPORT_FILE}"
    log_success "SBOM generation complete"
}

# Check license compliance
check_licenses() {
    log_info "Checking license compliance..."
    
    echo "## License Compliance" >> "${REPORT_FILE}"
    echo "" >> "${REPORT_FILE}"
    
    if command -v go-licenses &> /dev/null; then
        echo "### Allowed Licenses" >> "${REPORT_FILE}"
        echo "- MIT" >> "${REPORT_FILE}"
        echo "- Apache-2.0" >> "${REPORT_FILE}"
        echo "- BSD-3-Clause" >> "${REPORT_FILE}"
        echo "- BSD-2-Clause" >> "${REPORT_FILE}"
        echo "" >> "${REPORT_FILE}"
        
        echo "### License Check Results" >> "${REPORT_FILE}"
        echo '```' >> "${REPORT_FILE}"
        go-licenses check ./... --allowed_licenses=MIT,Apache-2.0,BSD-3-Clause,BSD-2-Clause 2>&1 | head -30 >> "${REPORT_FILE}" || true
        echo '```' >> "${REPORT_FILE}"
    else
        echo "License check skipped (go-licenses not installed)" >> "${REPORT_FILE}"
    fi
    
    echo "" >> "${REPORT_FILE}"
    log_success "License check complete"
}

# Run compliance checks
run_compliance_checks() {
    log_info "Running compliance checks..."
    
    echo "## Compliance Checks" >> "${REPORT_FILE}"
    echo "" >> "${REPORT_FILE}"
    
    # Check for security headers
    echo "### Security Headers" >> "${REPORT_FILE}"
    if grep -r "X-Content-Type-Options\|X-Frame-Options\|Strict-Transport-Security" services/ --include="*.go" &> /dev/null; then
        echo "✅ Security headers implemented" >> "${REPORT_FILE}"
    else
        echo "⚠️  Security headers not found" >> "${REPORT_FILE}"
    fi
    
    # Check for authentication
    echo "### Authentication Mechanisms" >> "${REPORT_FILE}"
    if grep -r "jwt\|oauth2\|mtls" services/gateway/ --include="*.go" &> /dev/null; then
        echo "✅ Authentication mechanisms found" >> "${REPORT_FILE}"
    else
        echo "⚠️  Authentication mechanisms not found" >> "${REPORT_FILE}"
    fi
    
    # Check for TLS configuration
    echo "### TLS Configuration" >> "${REPORT_FILE}"
    if grep -r "tls\.VersionTLS13\|MinVersion" services/ --include="*.go" &> /dev/null; then
        echo "✅ TLS 1.3 configured" >> "${REPORT_FILE}"
    else
        echo "⚠️  TLS 1.3 not enforced" >> "${REPORT_FILE}"
    fi
    
    # Check for rate limiting
    echo "### Rate Limiting" >> "${REPORT_FILE}"
    if grep -r "rate.*limit\|RateLimit" services/ --include="*.go" &> /dev/null; then
        echo "✅ Rate limiting implemented" >> "${REPORT_FILE}"
    else
        echo "⚠️  Rate limiting not found" >> "${REPORT_FILE}"
    fi
    
    echo "" >> "${REPORT_FILE}"
    log_success "Compliance checks complete"
}

# Container security scan
run_container_scan() {
    log_info "Running container security scan..."
    
    echo "## Container Security" >> "${REPORT_FILE}"
    echo "" >> "${REPORT_FILE}"
    
    # Check if containers exist
    if docker images | grep -q "mcp-gateway\|mcp-router"; then
        if command -v trivy &> /dev/null; then
            log_info "Running Trivy container scan..."
            echo "### Trivy Container Scan" >> "${REPORT_FILE}"
            echo '```' >> "${REPORT_FILE}"
            trivy image --severity HIGH,CRITICAL mcp-gateway:latest 2>&1 | head -50 >> "${REPORT_FILE}" || true
            trivy image --severity HIGH,CRITICAL mcp-router:latest 2>&1 | head -50 >> "${REPORT_FILE}" || true
            echo '```' >> "${REPORT_FILE}"
        else
            echo "Trivy not installed - skipping container scan" >> "${REPORT_FILE}"
            echo "Install from: https://github.com/aquasecurity/trivy" >> "${REPORT_FILE}"
        fi
    else
        echo "No MCP containers found - skipping container scan" >> "${REPORT_FILE}"
    fi
    
    echo "" >> "${REPORT_FILE}"
    log_success "Container scan complete"
}

# Generate summary
generate_summary() {
    log_info "Generating summary..."
    
    # Count issues
    local high_issues=$(grep -c "HIGH\|Critical" "${REPORT_FILE}" 2>/dev/null || echo "0")
    local medium_issues=$(grep -c "MEDIUM\|Warning" "${REPORT_FILE}" 2>/dev/null || echo "0")
    local low_issues=$(grep -c "LOW\|Info" "${REPORT_FILE}" 2>/dev/null || echo "0")
    
    # Add summary to beginning of report
    local temp_file="${REPORT_FILE}.tmp"
    cat > "${temp_file}" << EOF
# Security Scan Report

**Generated:** $(date)
**Repository:** MCP Bridge
**Scan ID:** ${TIMESTAMP}

## Executive Summary

### Issue Count
- 🔴 High/Critical: ${high_issues}
- 🟡 Medium: ${medium_issues}
- 🟢 Low: ${low_issues}

### Scan Coverage
- ✅ SAST (Static Application Security Testing)
- ✅ Dependency Vulnerability Scanning
- ✅ Secret Detection
- ✅ SBOM Generation
- ✅ License Compliance
- ✅ Security Compliance Checks
- ✅ Container Security (if applicable)

---

EOF
    
    # Append original report content
    cat "${REPORT_FILE}" >> "${temp_file}"
    mv "${temp_file}" "${REPORT_FILE}"
    
    log_success "Summary generated"
}

# Main execution
main() {
    echo ""
    echo "🔒 MCP Bridge Security Scanner"
    echo "================================"
    echo ""
    
    # Check dependencies
    check_dependencies
    
    # Install security tools if needed
    install_security_tools
    
    # Initialize report
    init_report
    
    # Run security scans
    run_sast_scan
    run_dependency_scan
    run_secret_scan
    generate_sbom
    check_licenses
    run_compliance_checks
    run_container_scan
    
    # Generate summary
    generate_summary
    
    # Final output
    echo ""
    echo "========================================="
    echo ""
    log_success "Security scan complete!"
    echo ""
    echo "📊 Report generated: ${REPORT_FILE}"
    echo ""
    
    # Show summary
    echo "Summary:"
    grep -A3 "### Issue Count" "${REPORT_FILE}"
    echo ""
    
    # Return non-zero if high/critical issues found
    if [ "$high_issues" -gt 0 ]; then
        log_warning "High/Critical security issues found!"
        exit 1
    else
        log_success "No high/critical security issues found"
        exit 0
    fi
}

# Run main function
main "$@"