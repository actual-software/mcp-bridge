#!/bin/bash
# Install git hooks

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
GIT_HOOKS_DIR="${REPO_ROOT}/.git/hooks"

if [[ ! -d "${REPO_ROOT}/.git" ]]; then
    echo "❌ Not in a git repository"
    exit 1
fi

echo "Installing git hooks..."

cp "${SCRIPT_DIR}/hooks/pre-commit" "${GIT_HOOKS_DIR}/pre-commit"
chmod +x "${GIT_HOOKS_DIR}/pre-commit"
echo "✅ pre-commit hook installed (runs 'make check')"

cp "${SCRIPT_DIR}/hooks/pre-push" "${GIT_HOOKS_DIR}/pre-push"
chmod +x "${GIT_HOOKS_DIR}/pre-push"
echo "✅ pre-push hook installed (runs 'make validate')"

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "✅ Git hooks installed"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "Your workflow is now protected:"
echo "  git commit  →  runs 'make check' (30s)"
echo "  git push    →  runs 'make validate' (5min)"
echo ""
echo "To bypass: git commit --no-verify / git push --no-verify"
echo ""
