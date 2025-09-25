# Repository Audit Script

A comprehensive repository quality analysis tool that performs linting, testing, and coverage analysis.

## Features

### 🔍 Linting Analysis
- Runs `golangci-lint` across the entire repository
- Reports total issues by linter type and file
- Identifies top problematic files
- Saves detailed results and summaries

### 🚀 Test Analysis  
- Executes all tests with `go test ./...`
- Reports passed, failed, and skipped tests
- Identifies failing tests and packages
- Measures test execution duration
- Tracks test coverage across packages

### 📊 Coverage Analysis
- Generates comprehensive coverage reports
- Creates HTML coverage visualization
- Identifies files with low coverage (<70%)
- Compares against configurable threshold (default: 80%)
- Provides per-package coverage breakdown

## Usage

### Basic Usage
```bash
./audit.sh
```

### With Custom Options
```bash
# Set custom timeout and coverage threshold
./audit.sh --timeout 20m --coverage 85

# Clean previous audit results
./audit.sh --clean

# Show help
./audit.sh --help
```

### Options
- `-h, --help` - Show help message
- `-t, --timeout DURATION` - Set timeout for operations (default: 15m)
- `-c, --coverage NUM` - Set coverage threshold percentage (default: 80)
- `--clean` - Clean previous audit results

## Output

The script generates a comprehensive audit report in the `audit-results/` directory:

### Generated Files
- `audit-report-[timestamp].md` - Comprehensive markdown report
- `lint-results-[timestamp].txt` - Detailed linting output
- `lint-summary-[timestamp].txt` - Linting summary and statistics
- `test-results-[timestamp].txt` - Detailed test execution output  
- `test-summary-[timestamp].txt` - Test summary and statistics
- `coverage-results-[timestamp].txt` - Coverage execution output
- `coverage-summary-[timestamp].txt` - Coverage summary and statistics
- `coverage-profile-[timestamp].out` - Go coverage profile
- `coverage-report-[timestamp].html` - HTML coverage visualization

### Exit Codes
- `0` - All checks passed (linting clean, tests pass, coverage meets threshold)
- `1` - One or more checks failed

### Sample Output
```
================================================
📊 FINAL AUDIT SUMMARY  
================================================

❌ Repository audit: FAIL
❌ Quality issues detected - see detailed reports
📊 Linting: 4 issues
📊 Tests: 0 failures  
📊 Coverage: 34.2%
```

## Integration

### CI/CD Pipeline
Add to your CI/CD pipeline for automated quality checks:

```yaml
# Example GitHub Actions
- name: Run Repository Audit
  run: ./audit.sh
  
# Example GitLab CI  
audit:
  script:
    - ./audit.sh
  artifacts:
    reports:
      coverage: audit-results/coverage-profile-*.out
```

### Pre-commit Hook
Use as a pre-commit hook for local development:

```bash
# Add to .git/hooks/pre-commit
#!/bin/bash
./audit.sh --timeout 5m
```

## Requirements

### Prerequisites
- Go installed and available in PATH
- `golangci-lint` installed at `~/go/bin/golangci-lint`
- `bc` calculator (optional, for duration calculations)

### Installation
1. Copy `audit.sh` to your repository root
2. Make executable: `chmod +x audit.sh`  
3. Run: `./audit.sh`

## Customization

### Coverage Threshold
Adjust the default coverage threshold by editing the script:
```bash
COVERAGE_THRESHOLD=85  # Change from default 80%
```

### Timeout Settings
Modify default timeout for long-running repositories:
```bash
TIMEOUT="20m"  # Change from default 15m
```

### Golangci-lint Path
Update the golangci-lint path if installed elsewhere:
```bash
# In the script, update the path check
if ! command -v /usr/local/bin/golangci-lint >/dev/null 2>&1; then
```

## Troubleshooting

### Common Issues

**"golangci-lint not found"**
- Install golangci-lint: `go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest`
- Or update the path in the script

**"Permission denied"**  
- Make script executable: `chmod +x audit.sh`

**Timeout issues**
- Increase timeout: `./audit.sh --timeout 30m`
- Or update default in script

**Coverage collection fails**
- Ensure all tests compile and run successfully
- Check for build constraints or missing dependencies

## Examples

### Perfect Repository (All Green)
```
✅ Repository audit: PASS
✅ All quality checks passed!
📊 Linting: 0 issues
📊 Tests: 0 failures
📊 Coverage: 85.4%
```

### Repository with Issues
```
❌ Repository audit: FAIL  
❌ Quality issues detected - see detailed reports
📊 Linting: 12 issues
📊 Tests: 2 failures
📊 Coverage: 34.2%
```

This audit script provides comprehensive quality analysis to help maintain high code standards and catch issues before they reach production.