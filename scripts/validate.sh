#!/bin/bash
# Full validation - Mirrors all CI checks locally
# Run before pushing important changes
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "${REPO_ROOT}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Track failures
FAILED_CHECKS=()
PASSED_CHECKS=()

print_header() {
    echo -e "\n${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${BLUE}$1${NC}"
    echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}\n"
}

print_success() {
    echo -e "${GREEN}✅ $1${NC}"
    PASSED_CHECKS+=("$1")
}

print_error() {
    echo -e "${RED}❌ $1${NC}"
    FAILED_CHECKS+=("$1")
}

print_warning() {
    echo -e "${YELLOW}⚠️  $1${NC}"
}

run_check() {
    local check_name="$1"
    local check_command="$2"

    echo -e "${BLUE}Running: ${check_name}${NC}"

    if eval "$check_command"; then
        print_success "$check_name"
        return 0
    else
        print_error "$check_name FAILED"
        return 1
    fi
}

# =============================================================================
# PHASE 1: BUILD VALIDATION
# =============================================================================
print_header "PHASE 1: BUILD VALIDATION"

echo "🧹 Cleaning previous builds..."
make clean || true

echo "🔨 Building all services..."
if make build; then
    print_success "Build: All services compiled successfully"

    # Verify binaries exist
    if [[ -f "services/gateway/bin/mcp-gateway" ]] && [[ -f "services/router/bin/mcp-router" ]]; then
        print_success "Build: Binaries verified"
    else
        print_error "Build: Binaries not found after build"
        exit 1
    fi
else
    print_error "Build: Compilation failed"
    echo "🛑 CRITICAL: Code does not compile. Fix build errors before proceeding."
    exit 1
fi

# =============================================================================
# PHASE 2: CODE FORMATTING
# =============================================================================
print_header "PHASE 2: CODE FORMATTING"

echo "📝 Checking code formatting with gofmt..."
UNFORMATTED=$(gofmt -s -l services/ pkg/ 2>/dev/null || true)
if [[ -z "$UNFORMATTED" ]]; then
    print_success "Format: Code is properly formatted"
else
    print_error "Format: Code is not properly formatted"
    echo "Files needing formatting:"
    echo "$UNFORMATTED"
    echo "Run 'make fmt' to fix"
    exit 1
fi

echo "📦 Checking import organization..."
if ! command -v goimports >/dev/null 2>&1; then
    echo "Installing goimports..."
    go install golang.org/x/tools/cmd/goimports@latest
fi

UNORGANIZED=$(goimports -l services/ pkg/ 2>/dev/null || true)
if [[ -z "$UNORGANIZED" ]]; then
    print_success "Imports: Properly organized"
else
    print_error "Imports: Not properly organized"
    echo "Files with unorganized imports:"
    echo "$UNORGANIZED"
    echo "Run 'goimports -w .' to fix"
    exit 1
fi

# =============================================================================
# PHASE 3: GO MODULE VALIDATION
# =============================================================================
print_header "PHASE 3: GO MODULE VALIDATION"

echo "🔍 Checking go.mod and go.sum are tidy..."

# Check root module
go mod tidy
if [[ -n "$(git status --porcelain go.mod go.sum)" ]]; then
    print_error "go.mod/go.sum: Root module not tidy"
    git diff go.mod go.sum
    exit 1
else
    print_success "go.mod/go.sum: Root module is tidy"
fi

# Check gateway module
cd services/gateway
go mod tidy
if [[ -n "$(git status --porcelain go.mod go.sum)" ]]; then
    print_error "go.mod/go.sum: Gateway module not tidy"
    git diff go.mod go.sum
    cd "${REPO_ROOT}"
    exit 1
else
    print_success "go.mod/go.sum: Gateway module is tidy"
fi
cd "${REPO_ROOT}"

# Check router module
cd services/router
go mod tidy
if [[ -n "$(git status --porcelain go.mod go.sum)" ]]; then
    print_error "go.mod/go.sum: Router module not tidy"
    git diff go.mod go.sum
    cd "${REPO_ROOT}"
    exit 1
else
    print_success "go.mod/go.sum: Router module is tidy"
fi
cd "${REPO_ROOT}"

# =============================================================================
# PHASE 4: LINTING (mirrors audit-lint.sh)
# =============================================================================
print_header "PHASE 4: LINTING"

echo "🔍 Running golangci-lint audit..."
if make audit-lint; then
    print_success "Lint: No issues found"
else
    print_error "Lint: Issues found"
    echo "Check audit-results/ for details"
    exit 1
fi

# =============================================================================
# PHASE 5: TESTS (mirrors audit-test.sh)
# =============================================================================
print_header "PHASE 5: TESTS"

echo "🧪 Running comprehensive test audit..."
if make audit-test; then
    print_success "Tests: All tests passed"
else
    print_error "Tests: Some tests failed"
    echo "Check audit-results/ for details"
    exit 1
fi

# =============================================================================
# PHASE 6: COVERAGE (mirrors audit-coverage.sh)
# =============================================================================
print_header "PHASE 6: COVERAGE"

echo "📊 Running coverage audit..."
if make audit-coverage; then
    print_success "Coverage: Meets threshold"
else
    print_error "Coverage: Below threshold"
    echo "Check audit-results/ for details"
    # Note: Coverage failures might be acceptable depending on context
    # Uncomment next line to make coverage failures block pushes:
    # exit 1
fi

# =============================================================================
# PHASE 7: CODE QUALITY CHECKS
# =============================================================================
print_header "PHASE 7: CODE QUALITY CHECKS"

echo "📚 Checking documentation completeness..."
REQUIRED_DOCS=("README.md" "docs/SECURITY.md" "services/gateway/README.md" "services/router/README.md")
for doc in "${REQUIRED_DOCS[@]}"; do
    if [[ ! -f "$doc" ]]; then
        print_error "Documentation: Missing $doc"
        exit 1
    fi
done
print_success "Documentation: All required files present"

echo "🔄 Checking cyclomatic complexity..."
if ! command -v gocyclo >/dev/null 2>&1; then
    echo "Installing gocyclo..."
    go install github.com/fzipp/gocyclo/cmd/gocyclo@latest
fi

HIGH_COMPLEXITY=$(gocyclo -over 15 services/ 2>/dev/null || true)
if [[ -n "$HIGH_COMPLEXITY" ]]; then
    print_warning "Complexity: Functions with high complexity found:"
    echo "$HIGH_COMPLEXITY"
    print_warning "Consider refactoring (not blocking)"
else
    print_success "Complexity: All functions within acceptable limits"
fi

# =============================================================================
# PHASE 8: BUILD VERIFICATION (mirrors build-verification.yml)
# =============================================================================
print_header "PHASE 8: BUILD VERIFICATION"

echo "✅ Verifying build artifacts..."
for service in gateway router; do
    if [[ ! -f "services/${service}/bin/mcp-${service}" ]]; then
        print_error "Build Verification: ${service} binary missing"
        exit 1
    fi

    # Test binary execution
    if ./services/${service}/bin/mcp-${service} --help >/dev/null 2>&1; then
        print_success "Build Verification: ${service} binary executes"
    else
        print_error "Build Verification: ${service} binary fails to execute"
        exit 1
    fi
done

# =============================================================================
# FINAL SUMMARY
# =============================================================================
print_header "FULL VALIDATION SUMMARY"

echo -e "${GREEN}✅ Passed Checks (${#PASSED_CHECKS[@]}):${NC}"
for check in "${PASSED_CHECKS[@]}"; do
    echo -e "  ${GREEN}✓${NC} $check"
done

if [[ ${#FAILED_CHECKS[@]} -gt 0 ]]; then
    echo -e "\n${RED}❌ Failed Checks (${#FAILED_CHECKS[@]}):${NC}"
    for check in "${FAILED_CHECKS[@]}"; do
        echo -e "  ${RED}✗${NC} $check"
    done

    echo -e "\n${RED}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${RED}❌ VALIDATION FAILED${NC}"
    echo -e "${RED}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo ""
    echo "Fix issues above before pushing."
    exit 1
else
    echo -e "\n${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${GREEN}✅ FULL VALIDATION PASSED${NC}"
    echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo ""
    echo "All CI checks passed. Safe to push."
    exit 0
fi
