#!/bin/bash

# MCP Bridge Changelog Management Script
# Handles changelog generation, version bumping, and release notes

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
CHANGELOG_FILE="CHANGELOG.md"
VERSION_FILE="VERSION"
COMMIT_TYPES="feat|fix|docs|style|refactor|perf|test|build|ci|chore|revert"

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

# Show usage
usage() {
    cat << EOF
Usage: $0 [COMMAND] [OPTIONS]

Commands:
    generate    Generate changelog from commits
    release     Create a new release with changelog
    validate    Validate commit messages
    preview     Preview changelog for next release
    version     Show or bump version

Options:
    -v, --version VERSION   Specify version (for release command)
    -f, --from TAG         Generate changelog from specific tag
    -t, --to TAG           Generate changelog to specific tag
    -d, --dry-run          Run without making changes
    -h, --help             Show this help message

Examples:
    $0 generate                    # Generate changelog from all commits
    $0 release -v 1.2.0           # Create release with version 1.2.0
    $0 release                     # Auto-detect version and create release
    $0 validate                    # Validate recent commit messages
    $0 preview                     # Preview next release changelog
    $0 version                     # Show current version
    $0 version patch              # Bump patch version
    $0 version minor              # Bump minor version
    $0 version major              # Bump major version

EOF
    exit 0
}

# Get current version
get_current_version() {
    if [ -f "$VERSION_FILE" ]; then
        cat "$VERSION_FILE"
    elif git describe --tags --abbrev=0 &>/dev/null; then
        git describe --tags --abbrev=0 | sed 's/^v//'
    else
        echo "0.0.0"
    fi
}

# Bump version
bump_version() {
    local current="$1"
    local bump_type="$2"
    
    IFS='.' read -ra PARTS <<< "$current"
    local major="${PARTS[0]:-0}"
    local minor="${PARTS[1]:-0}"
    local patch="${PARTS[2]:-0}"
    
    case "$bump_type" in
        major)
            major=$((major + 1))
            minor=0
            patch=0
            ;;
        minor)
            minor=$((minor + 1))
            patch=0
            ;;
        patch)
            patch=$((patch + 1))
            ;;
        *)
            echo "$current"
            return
            ;;
    esac
    
    echo "${major}.${minor}.${patch}"
}

# Detect version bump type from commits
detect_bump_type() {
    local from_tag="${1:-}"
    local to_ref="${2:-HEAD}"
    
    # Check for breaking changes
    if git log --format=%B "${from_tag}..${to_ref}" 2>/dev/null | grep -qE "BREAKING CHANGE:|^[^:]+!:"; then
        echo "major"
        return
    fi
    
    # Check for features
    if git log --format=%s "${from_tag}..${to_ref}" 2>/dev/null | grep -q "^feat"; then
        echo "minor"
        return
    fi
    
    # Default to patch
    echo "patch"
}

# Parse commits into categories
parse_commits() {
    local from_ref="${1:-}"
    local to_ref="${2:-HEAD}"
    
    declare -A categories
    categories["feat"]="✨ Features"
    categories["fix"]="🐛 Bug Fixes"
    categories["docs"]="📚 Documentation"
    categories["style"]="💎 Styles"
    categories["refactor"]="📦 Code Refactoring"
    categories["perf"]="🚀 Performance Improvements"
    categories["test"]="🚨 Tests"
    categories["build"]="🛠 Build System"
    categories["ci"]="⚙️ Continuous Integration"
    categories["chore"]="♻️ Chores"
    categories["revert"]="🗑 Reverts"
    
    declare -A commits_by_type
    
    # Get commits
    local range
    if [ -n "$from_ref" ]; then
        range="${from_ref}..${to_ref}"
    else
        range="${to_ref}"
    fi
    
    while IFS= read -r line; do
        local hash=$(echo "$line" | cut -d' ' -f1)
        local message=$(echo "$line" | cut -d' ' -f2-)
        
        # Extract type from conventional commit
        if [[ "$message" =~ ^($COMMIT_TYPES)(\(.*\))?!?:\ (.*)$ ]]; then
            local type="${BASH_REMATCH[1]}"
            local scope="${BASH_REMATCH[2]}"
            local subject="${BASH_REMATCH[3]}"
            
            # Check for breaking change
            if [[ "$message" =~ ! ]] || git log -1 --format=%B "$hash" | grep -q "BREAKING CHANGE:"; then
                commits_by_type["breaking"]+="- ⚠️ **BREAKING**: $subject ($hash)\\n"
            else
                commits_by_type["$type"]+="- $subject ($hash)\\n"
            fi
        else
            # Non-conventional commit
            commits_by_type["other"]+="- $message ($hash)\\n"
        fi
    done < <(git log --format="%h %s" "$range" 2>/dev/null)
    
    # Output organized commits
    if [ -n "${commits_by_type[breaking]:-}" ]; then
        echo "### ⚠️ BREAKING CHANGES"
        echo ""
        echo -e "${commits_by_type[breaking]}"
        echo ""
    fi
    
    for type in feat fix perf docs refactor build ci test style chore revert; do
        if [ -n "${commits_by_type[$type]:-}" ]; then
            echo "### ${categories[$type]}"
            echo ""
            echo -e "${commits_by_type[$type]}"
            echo ""
        fi
    done
    
    if [ -n "${commits_by_type[other]:-}" ]; then
        echo "### Other Changes"
        echo ""
        echo -e "${commits_by_type[other]}"
        echo ""
    fi
}

# Generate changelog
generate_changelog() {
    local from_tag="${1:-}"
    local to_tag="${2:-HEAD}"
    local version="${3:-}"
    
    log_info "Generating changelog from ${from_tag:-beginning} to ${to_tag}"
    
    # Determine version if not provided
    if [ -z "$version" ]; then
        local current_version=$(get_current_version)
        local bump_type=$(detect_bump_type "$from_tag" "$to_tag")
        version=$(bump_version "$current_version" "$bump_type")
    fi
    
    # Generate changelog entry
    local date=$(date +%Y-%m-%d)
    local changelog_entry="## [${version}] - ${date}\\n\\n"
    
    # Add commit information
    local commits=$(parse_commits "$from_tag" "$to_tag")
    if [ -n "$commits" ]; then
        changelog_entry+="$commits"
    else
        changelog_entry+="No significant changes\\n\\n"
    fi
    
    # Update CHANGELOG.md
    if [ -f "$CHANGELOG_FILE" ]; then
        # Insert after [Unreleased] section
        local temp_file=$(mktemp)
        awk -v entry="$changelog_entry" '
            /^## \[Unreleased\]/ {
                print
                print ""
                print entry
                next
            }
            {print}
        ' "$CHANGELOG_FILE" > "$temp_file"
        
        # Replace escaped newlines with actual newlines
        sed -i.bak 's/\\n/\n/g' "$temp_file"
        rm "${temp_file}.bak"
        
        mv "$temp_file" "$CHANGELOG_FILE"
    else
        # Create new CHANGELOG.md
        cat > "$CHANGELOG_FILE" << EOF
# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

$(echo -e "$changelog_entry")
EOF
    fi
    
    # Update VERSION file
    echo "$version" > "$VERSION_FILE"
    
    # Sync version across all files
    if [ -f "./scripts/sync-version.sh" ]; then
        log_info "Synchronizing version across all files..."
        ./scripts/sync-version.sh
    fi
    
    log_success "Changelog generated for version $version"
    return 0
}

# Validate commit messages
validate_commits() {
    local from_ref="${1:-HEAD~10}"
    local to_ref="${2:-HEAD}"
    
    log_info "Validating commits from $from_ref to $to_ref"
    
    local invalid_count=0
    local total_count=0
    
    while IFS= read -r line; do
        total_count=$((total_count + 1))
        local hash=$(echo "$line" | cut -d' ' -f1)
        local message=$(echo "$line" | cut -d' ' -f2-)
        
        if [[ ! "$message" =~ ^($COMMIT_TYPES)(\(.*\))?!?:\ .+$ ]]; then
            log_warning "Invalid commit message: $hash $message"
            invalid_count=$((invalid_count + 1))
        fi
    done < <(git log --format="%h %s" "${from_ref}..${to_ref}")
    
    if [ $invalid_count -eq 0 ]; then
        log_success "All $total_count commits follow conventional commit format"
        return 0
    else
        log_error "$invalid_count out of $total_count commits do not follow conventional commit format"
        return 1
    fi
}

# Preview changelog
preview_changelog() {
    local from_tag="${1:-$(git describe --tags --abbrev=0 2>/dev/null || echo "")}"
    local to_ref="${2:-HEAD}"
    
    log_info "Preview changelog for next release"
    echo ""
    
    local current_version=$(get_current_version)
    local bump_type=$(detect_bump_type "$from_tag" "$to_ref")
    local next_version=$(bump_version "$current_version" "$bump_type")
    
    echo "Current version: $current_version"
    echo "Detected bump type: $bump_type"
    echo "Next version: $next_version"
    echo ""
    echo "---"
    echo ""
    
    parse_commits "$from_tag" "$to_ref"
}

# Create release
create_release() {
    local version="${1:-}"
    local dry_run="${2:-false}"
    
    # Get last tag
    local last_tag=$(git describe --tags --abbrev=0 2>/dev/null || echo "")
    
    # Determine version if not provided
    if [ -z "$version" ]; then
        local current_version=$(get_current_version)
        local bump_type=$(detect_bump_type "$last_tag" "HEAD")
        version=$(bump_version "$current_version" "$bump_type")
    fi
    
    log_info "Creating release $version"
    
    if [ "$dry_run" = "true" ]; then
        log_warning "Dry run mode - no changes will be made"
    fi
    
    # Generate changelog
    if [ "$dry_run" != "true" ]; then
        generate_changelog "$last_tag" "HEAD" "$version"
    else
        log_info "[DRY RUN] Would generate changelog for $version"
    fi
    
    # Update version in Go files
    if [ "$dry_run" != "true" ]; then
        for file in services/gateway/version.go services/router/version.go; do
            if [ -f "$file" ]; then
                sed -i.bak "s/Version = \".*\"/Version = \"$version\"/" "$file"
                rm "${file}.bak"
                log_info "Updated version in $file"
            fi
        done
    else
        log_info "[DRY RUN] Would update version in Go files to $version"
    fi
    
    # Commit changes
    if [ "$dry_run" != "true" ]; then
        git add -A
        git commit -m "chore(release): v$version" || true
        git tag -a "v$version" -m "Release v$version"
        log_success "Created release commit and tag for v$version"
    else
        log_info "[DRY RUN] Would create commit and tag for v$version"
    fi
    
    log_success "Release v$version prepared successfully"
    
    if [ "$dry_run" != "true" ]; then
        echo ""
        echo "To push the release:"
        echo "  git push origin main"
        echo "  git push origin v$version"
    fi
}

# Show version
show_version() {
    local current=$(get_current_version)
    echo "Current version: $current"
    
    # Show next version options
    echo ""
    echo "Next version options:"
    echo "  Patch: $(bump_version "$current" "patch")"
    echo "  Minor: $(bump_version "$current" "minor")"
    echo "  Major: $(bump_version "$current" "major")"
}

# Main script
main() {
    local command="${1:-}"
    shift || true
    
    case "$command" in
        generate)
            local from_tag=""
            local to_tag="HEAD"
            
            while [[ $# -gt 0 ]]; do
                case $1 in
                    -f|--from)
                        from_tag="$2"
                        shift 2
                        ;;
                    -t|--to)
                        to_tag="$2"
                        shift 2
                        ;;
                    *)
                        shift
                        ;;
                esac
            done
            
            generate_changelog "$from_tag" "$to_tag"
            ;;
            
        release)
            local version=""
            local dry_run="false"
            
            while [[ $# -gt 0 ]]; do
                case $1 in
                    -v|--version)
                        version="$2"
                        shift 2
                        ;;
                    -d|--dry-run)
                        dry_run="true"
                        shift
                        ;;
                    *)
                        shift
                        ;;
                esac
            done
            
            create_release "$version" "$dry_run"
            ;;
            
        validate)
            validate_commits "$@"
            ;;
            
        preview)
            preview_changelog "$@"
            ;;
            
        version)
            if [ -n "${1:-}" ]; then
                case "$1" in
                    patch|minor|major)
                        local current=$(get_current_version)
                        local new_version=$(bump_version "$current" "$1")
                        echo "$new_version" > "$VERSION_FILE"
                        log_success "Version bumped to $new_version"
                        ;;
                    *)
                        log_error "Invalid bump type: $1"
                        exit 1
                        ;;
                esac
            else
                show_version
            fi
            ;;
            
        -h|--help|help)
            usage
            ;;
            
        *)
            log_error "Unknown command: $command"
            usage
            ;;
    esac
}

# Run main function
main "$@"