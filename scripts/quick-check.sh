#!/bin/bash
# Quick validation for dev loop (~30 seconds)
# Catches most issues fast without full test suite

set -e

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${REPO_ROOT}"

RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m'

echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${BLUE}Quick Validation (build + format + basic checks)${NC}"
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}\n"

FAILED=0

# 1. Build
echo -e "${BLUE}[1/4] Building all services...${NC}"
if make build >/dev/null 2>&1; then
    echo -e "${GREEN}✅ Build passed${NC}"
else
    echo -e "${RED}❌ Build failed${NC}"
    make build
    exit 1
fi

# 2. Format check
echo -e "${BLUE}[2/4] Checking code formatting...${NC}"
UNFORMATTED=$(gofmt -s -l services/ pkg/ 2>/dev/null || true)
if [[ -z "$UNFORMATTED" ]]; then
    echo -e "${GREEN}✅ Format check passed${NC}"
else
    echo -e "${RED}❌ Format check failed${NC}"
    echo "Run: make fmt"
    FAILED=1
fi

# 3. Import organization
echo -e "${BLUE}[3/4] Checking import organization...${NC}"
if ! command -v goimports >/dev/null 2>&1; then
    go install golang.org/x/tools/cmd/goimports@latest
fi
UNORGANIZED=$(goimports -l services/ pkg/ 2>/dev/null || true)
if [[ -z "$UNORGANIZED" ]]; then
    echo -e "${GREEN}✅ Imports check passed${NC}"
else
    echo -e "${RED}❌ Imports check failed${NC}"
    echo "Run: goimports -w ."
    FAILED=1
fi

# 4. Basic lint (fast subset)
echo -e "${BLUE}[4/4] Running basic linters...${NC}"
if ! command -v golangci-lint >/dev/null 2>&1; then
    echo "golangci-lint not found, skipping"
else
    # Run with short timeout for quick feedback
    if golangci-lint run --timeout=30s ./... >/dev/null 2>&1; then
        echo -e "${GREEN}✅ Basic lint passed${NC}"
    else
        echo -e "${RED}❌ Basic lint failed${NC}"
        echo "Run: make validate (for full checks)"
        FAILED=1
    fi
fi

echo ""
if [[ $FAILED -eq 0 ]]; then
    echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${GREEN}✅ Quick validation passed${NC}"
    echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo "Run 'make validate' before pushing for full CI checks"
    exit 0
else
    echo -e "${RED}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${RED}❌ Quick validation failed${NC}"
    echo -e "${RED}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    exit 1
fi
