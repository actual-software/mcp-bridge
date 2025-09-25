# MCP Bridge Documentation

Welcome to the comprehensive documentation for MCP Bridge - the enterprise-grade Model Context Protocol gateway and router system.

## 📚 **Documentation Index**

### **🚀 Getting Started**
- [Quick Start Guide](../README.md#quick-start) - Get MCP Bridge running in minutes
- [Installation Guide](../scripts/install.sh) - Production installation scripts
- [Configuration Guide](configuration.md) - Complete configuration reference

### **🏗️ Architecture & Design**
- [Architecture Overview](architecture.md) - System design and components
- [Security Architecture](SECURITY.md) - Security model and threat analysis
- [Protocol Implementation](protocol.md) - MCP protocol details
- [OWASP Security](OWASP_SECURITY.md) - OWASP security scanning and compliance
- [Threat Model](THREAT_MODEL.md) - STRIDE threat analysis
- [Incident Response](SECURITY_INCIDENT_RESPONSE.md) - Security incident procedures

### **🚀 Deployment Guides**
- [Docker Deployment](DOCKER_DEPLOYMENT.md) - Container-based deployment with Docker Compose
- [Helm Deployment](HELM_DEPLOYMENT.md) - Kubernetes deployment with Helm charts  
- [Kubernetes Deployment](../deployments/kubernetes/README.md) - Native Kubernetes manifests
- [Installation Validation](../test/installation/validation-summary.md) - Automated installation testing

### **⚙️ Operations**
- [Monitoring Guide](monitoring.md) - Prometheus, Grafana, and alerting
- [SLI/SLO Monitoring](SLI_SLO_MONITORING.md) - Service level indicators and objectives
- [Distributed Tracing](DISTRIBUTED_TRACING.md) - OpenTelemetry tracing guide
- [Performance Guide](performance.md) - Comprehensive performance benchmarks and optimization
- [Troubleshooting Guide](troubleshooting.md) - Common issues and solutions
- [TLS Configuration](tls.md) - Certificate management and encryption

### **🔒 Security**
- [Security Best Practices](SECURITY.md) - Comprehensive security guide
- [Security Tooling](security-tooling.md) - Automated security scanning and compliance
- [Authentication Guide](authentication.md) - Auth methods and configuration
- [TLS Configuration](tls.md) - Certificate management and encryption

### **🧪 Testing & Quality**
- [Production Testing Guide](../test/PRODUCTION_TESTING.md) - Production-ready testing with Docker
- [Test Infrastructure](../test/README.md) - Complete testing framework documentation
- [Testing Strategy](../TESTING.md) - Comprehensive testing documentation
- [Code Quality Report](../CODE_QUALITY_IMPROVEMENTS_SUMMARY.md) - Quality metrics and improvements
- [Production Readiness](../PRODUCTION_READINESS.md) - Production deployment checklist
- [Final Audit](../FINAL_AUDIT.md) - Complete production readiness assessment

### **🔧 Development**
- [Contributing Guide](../CONTRIBUTING.md) - How to contribute to the project
- [Code of Conduct](../CODE_OF_CONDUCT.md) - Community guidelines
- [API Documentation](api.md) - Complete API reference

### **📦 Release Information**
- [Changelog](../CHANGELOG.md) - Version history and release notes
- [Changelog Guide](changelog-guide.md) - Automated changelog and release process
- [Versioning Strategy](../docs/VERSIONING_STRATEGY.md) - Release process and semantic versioning
- [Enterprise Release Plan](../ENTERPRISE_RELEASE_PLAN.md) - Enterprise readiness roadmap

### **🎯 Service-Specific Documentation**
- [Gateway Service](../services/gateway/README.md) - Gateway-specific documentation
- [Router Service](../services/router/README.md) - Router-specific documentation

## 📖 **Documentation by Use Case**

### **For Developers**
1. Start with [Contributing Guide](../CONTRIBUTING.md)
2. Review [API Documentation](api.md)
3. Review [Architecture Overview](architecture.md)
4. Check [API Documentation](api.md)

### **For System Administrators**
1. Review [Security Best Practices](SECURITY.md)
2. Choose deployment method:
   - [Docker Deployment](DOCKER_DEPLOYMENT.md) for containers
   - [Helm Deployment](HELM_DEPLOYMENT.md) for Kubernetes
3. Configure [Monitoring](monitoring.md)
4. Configure [TLS & Encryption](tls.md)

### **For DevOps Engineers**
1. Review [Architecture Overview](architecture.md)
2. Set up CI/CD with [GitHub Actions](../.github/workflows/)
3. Deploy with [Helm Charts](../helm/mcp-bridge/)
4. Configure [Monitoring & Alerting](monitoring.md)

### **For Security Teams**
1. Review [Security Architecture](SECURITY.md)
2. Audit [Authentication Methods](authentication.md)
3. Configure [TLS & Encryption](tls.md)
4. Review [Threat Model](THREAT_MODEL.md)

### **For Operations Teams**
1. Review [Production Readiness](../PRODUCTION_READINESS.md)
2. Set up [Monitoring](monitoring.md)
3. Create [Troubleshooting Runbooks](troubleshooting.md)
4. Review [Performance Guide](performance.md)

## 🔍 **Quick Reference**

### **Configuration Files**
- Gateway: `services/gateway/configs/gateway.yaml`
- Router: `services/router/configs/router.yaml`
- Docker Compose: `docker-compose.yml`
- Helm Values: `helm/mcp-bridge/values.yaml`

### **Important URLs**
- Gateway Health: `https://gateway:8443/health`
- Gateway Metrics: `https://gateway:9090/metrics`
- Router Metrics: `http://router:9091/metrics`
- Prometheus: `http://prometheus:9092`
- Grafana: `http://grafana:3000`

### **Default Ports**
- Gateway HTTPS: `8443`
- Gateway Metrics: `9090`
- Router Metrics: `9091`
- Prometheus: `9092`
- Grafana: `3000`
- Redis: `6379`

## 📈 **Documentation Quality Metrics**

- **Total Documentation Files**: 65+
- **Coverage**: All major features documented
- **Formats**: Markdown, YAML, JSON
- **Languages**: English
- **Last Updated**: August 2025

## 🤝 **Contributing to Documentation**

We welcome documentation improvements! See our [Contributing Guide](../CONTRIBUTING.md) for:

- Documentation style guide
- Review process
- Submitting improvements
- Creating new guides

## 📞 **Getting Help**

### **Community Support**
- [GitHub Issues](https://github.com/poiley/mcp-bridge/issues) - Bug reports and feature requests
- [GitHub Discussions](https://github.com/poiley/mcp-bridge/discussions) - Community Q&A
- [Documentation](https://github.com/poiley/mcp-bridge/tree/main/docs) - This documentation

### **Enterprise Support**
- Priority support for enterprise customers
- Professional services for implementation
- Custom SLA agreements
- Dedicated customer success manager

---

**📝 Note**: This documentation is actively maintained and updated with each release. If you find any issues or have suggestions for improvement, please [open an issue](https://github.com/poiley/mcp-bridge/issues) or [start a discussion](https://github.com/poiley/mcp-bridge/discussions).