# Security Tooling Documentation

## Overview

MCP Bridge implements comprehensive security tooling to ensure code quality, dependency safety, and compliance with security standards. This document describes the available security tools and how to use them.

## Table of Contents

1. [Quick Start](#quick-start)
2. [GitHub Actions Workflows](#github-actions-workflows)
3. [Local Security Scanning](#local-security-scanning)
4. [Security Tools](#security-tools)
5. [Compliance Reporting](#compliance-reporting)
6. [SBOM Generation](#sbom-generation)
7. [Best Practices](#best-practices)

## Quick Start

### Run Complete Security Scan

```bash
# Install security tools and run full scan
make security-scan

# Or use the script directly
./scripts/security-scan.sh
```

### CI Security Check

```bash
# Quick security check for CI/CD
make security-ci
```

## GitHub Actions Workflows

### 1. Security Scanning (`security.yml`)

- **Trigger**: Push, PR, Weekly schedule
- **Features**:
  - Gosec security scanner
  - Dependency vulnerability checks
  - Secret scanning with TruffleHog
  - Input validation verification
  - Security headers validation

### 2. Dependency Security (`dependency-security.yml`)

- **Trigger**: Push, PR, Daily schedule
- **Features**:
  - Trivy vulnerability scanning
  - Nancy Go dependency audit
  - Snyk security analysis
  - OSV scanner
  - License compliance checking
  - Dependency review for PRs

### 3. SBOM Generation (`sbom-generation.yml`)

- **Trigger**: Push to main, Tags, Manual
- **Features**:
  - CycloneDX SBOM generation
  - Syft SBOM in multiple formats
  - Container SBOM generation
  - SBOM signing with Cosign
  - Automatic release attachment

### 4. Compliance Reports (`compliance-report.yml`)

- **Trigger**: Weekly schedule, Manual
- **Features**:
  - SOC2 compliance assessment
  - ISO 27001 alignment check
  - PCI-DSS readiness evaluation
  - HIPAA framework assessment
  - GDPR compliance review
  - Executive summary generation

### 5. Comprehensive Security Pipeline (`security-comprehensive.yml`)

- **Trigger**: Push, PR, Daily schedule, Manual
- **Features**:
  - SAST with Semgrep and CodeQL
  - Container security scanning
  - Infrastructure security with Checkov
  - Supply chain security analysis
  - Runtime security analysis
  - Consolidated reporting

## Local Security Scanning

### Makefile Targets

```bash
# Install security tools
make security-deps

# Run SAST analysis
make security-sast

# Check for vulnerabilities
make security-vulns

# Scan for secrets
make security-secrets

# Generate SBOM
make security-sbom

# Check licenses
make security-licenses

# Generate security report
make security-report

# Container security scan
make security-container

# Compliance checks
make security-compliance
```

### Security Scan Script

The `scripts/security-scan.sh` script provides comprehensive security scanning:

```bash
# Run full security scan
./scripts/security-scan.sh

# Output includes:
# - SAST results
# - Dependency vulnerabilities
# - Secret detection
# - SBOM generation
# - License compliance
# - Compliance checks
# - Container security (if applicable)
```

## Security Tools

### Static Analysis

| Tool | Purpose | Installation |
|------|---------|--------------|
| Gosec | Go security checker | `go install github.com/securego/gosec/v2/cmd/gosec@latest` |
| Staticcheck | Go static analysis | `go install honnef.co/go/tools/cmd/staticcheck@latest` |
| Semgrep | Multi-language SAST | GitHub Action |
| CodeQL | Code vulnerability analysis | GitHub Action |

### Dependency Scanning

| Tool | Purpose | Installation |
|------|---------|--------------|
| Govulncheck | Go vulnerability database | `go install golang.org/x/vuln/cmd/govulncheck@latest` |
| Nancy | Vulnerability scanner for Go | `go install github.com/sonatype-nexus-community/nancy@latest` |
| Trivy | Container and dependency scanner | [Trivy Installation](https://github.com/aquasecurity/trivy) |
| Snyk | Vulnerability database scanning | GitHub Action |
| OSV Scanner | Open Source Vulnerabilities | GitHub Action |

### Secret Detection

| Tool | Purpose | Installation |
|------|---------|--------------|
| TruffleHog | Secret scanning | GitHub Action or [TruffleHog Installation](https://github.com/trufflesecurity/trufflehog) |
| Basic grep | Pattern-based detection | Built-in |

#### TruffleHog Configuration

TruffleHog is configured with `--exclude-globs` to skip intentional test fixtures:

**Excluded Globs:**
- `test/e2e/**/certs/**` - E2E test certificates for TLS/mTLS testing
- `**/*_test.go` - Go test files with embedded test keys
- `**/testdata/**` - Test fixture directories
- `node_modules/**` - Third-party dependencies

**Rationale:** Test certificates and fixtures are intentionally committed for E2E testing and are never used in production. These keys are:
- Generated specifically for test environments
- Not valid for any production system
- Required for testing TLS, mTLS, and authentication features
- Safe to commit as they have no security value outside tests

See `.trufflehog-exclude` for the full list of excluded patterns.

### SBOM Tools

| Tool | Purpose | Installation |
|------|---------|--------------|
| CycloneDX | SBOM generation | `go install github.com/CycloneDX/cyclonedx-gomod/cmd/cyclonedx-gomod@latest` |
| Syft | Multi-format SBOM | GitHub Action or [Syft Installation](https://github.com/anchore/syft) |
| Cosign | SBOM signing | GitHub Action |

### Compliance Tools

| Tool | Purpose | Installation |
|------|---------|--------------|
| go-licenses | License checking | `go install github.com/google/go-licenses@latest` |
| Checkov | IaC security | GitHub Action |
| Terrascan | IaC compliance | Docker image |

## Compliance Reporting

### Available Reports

1. **SOC2 Compliance Report**
   - Control environment assessment
   - Security controls validation
   - Monitoring and risk assessment

2. **ISO 27001 Compliance Report**
   - ISMS alignment check
   - Security control mapping
   - Policy and procedure review

3. **PCI-DSS Compliance Report**
   - Payment card security requirements
   - Network security assessment
   - Access control validation

4. **HIPAA Compliance Report**
   - Administrative safeguards
   - Technical safeguards
   - PHI protection readiness

5. **GDPR Compliance Report**
   - Data protection principles
   - Privacy by design assessment
   - Data subject rights support

### Generating Reports

```bash
# Generate all compliance reports (GitHub Actions)
# Manually trigger the workflow from GitHub UI

# Local compliance check
make security-compliance
```

## SBOM Generation

### Generate SBOM Locally

```bash
# Using Makefile
make security-sbom

# Direct generation
cd services/gateway
cyclonedx-gomod mod -json -output sbom-gateway.json
cyclonedx-gomod mod -xml -output sbom-gateway.xml

cd ../router
cyclonedx-gomod mod -json -output sbom-router.json
cyclonedx-gomod mod -xml -output sbom-router.xml
```

### SBOM Formats

- **CycloneDX**: JSON and XML formats
- **SPDX**: JSON format via Syft
- **Container SBOM**: For Docker images

### SBOM Signing

SBOMs are automatically signed in CI/CD using Cosign when creating releases.

## Best Practices

### 1. Regular Scanning

- Run security scans on every PR
- Daily vulnerability scanning in CI
- Weekly comprehensive security audits

### 2. Dependency Management

- Keep dependencies up to date
- Review dependency licenses
- Monitor for vulnerability disclosures
- Generate and maintain SBOMs

### 3. Secret Management

- Never commit secrets to the repository
- Use environment variables for sensitive data
- Implement secret scanning in CI/CD
- Rotate credentials regularly

### 4. Compliance

- Regular compliance assessments
- Document security controls
- Maintain audit trails
- Update security policies

### 5. Container Security

- Scan container images before deployment
- Use minimal base images
- Run containers as non-root
- Implement security policies in Kubernetes

## Security Contacts

For security issues or questions:

1. **Security Issues**: Open a security advisory in GitHub
2. **General Questions**: Create an issue with the `security` label
3. **Critical Vulnerabilities**: Follow responsible disclosure practices

## Automation

### Pre-commit Hooks

Add to `.pre-commit-config.yaml`:

```yaml
repos:
  - repo: https://github.com/securego/gosec
    rev: v2.18.2
    hooks:
      - id: gosec
```

### Git Hooks

```bash
# Add to .git/hooks/pre-push
#!/bin/bash
make security-ci
```

## Monitoring and Alerts

### GitHub Security Features

1. **Dependabot**: Automated dependency updates
2. **Code Scanning**: Automated vulnerability detection
3. **Secret Scanning**: Prevent secret commits
4. **Security Advisories**: Vulnerability reporting

### Metrics and KPIs

Track these security metrics:

- Vulnerability count by severity
- Time to remediation
- Dependency update frequency
- Compliance score trends
- SBOM coverage percentage

## Troubleshooting

### Common Issues

1. **Tool Installation Failures**
   ```bash
   # Ensure Go is in PATH
   export PATH=$PATH:$(go env GOPATH)/bin
   ```

2. **Scan Timeouts**
   ```bash
   # Increase timeout for large codebases
   timeout 600s ./scripts/security-scan.sh
   ```

3. **Permission Issues**
   ```bash
   # Ensure script is executable
   chmod +x scripts/security-scan.sh
   ```

## References

- [OWASP Top 10](https://owasp.org/www-project-top-ten/)
- [CIS Benchmarks](https://www.cisecurity.org/cis-benchmarks/)
- [NIST Cybersecurity Framework](https://www.nist.gov/cyberframework)
- [Go Security Best Practices](https://golang.org/doc/security/)