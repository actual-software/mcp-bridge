# MCP Bridge Tutorials

Step-by-step guides for common MCP Bridge scenarios.


## Getting Started

### 📚 Beginner Tutorials

1. **[Your First MCP Server](01-first-mcp-server.md)** (15 min)
   - Create a simple MCP server
   - Configure the gateway
   - Test with clients
   - Deploy to production

2. **[Multi-Tool Server](02-multi-tool-server.md)** (20 min)
   - Build servers with multiple tools
   - Handle different input types
   - Return structured data
   - Error handling patterns

3. **[Client Integration](03-client-integration.md)** (30 min)
   - JavaScript/TypeScript clients (Browser & Node.js)
   - Python WebSocket and HTTP clients
   - Go TCP Binary client
   - CLI tools with curl and bash
   - Connection management and reconnection
   - Error handling patterns

## Deployment Tutorials

### 🚀 Production Deployment

4. **[Kubernetes Deployment](04-kubernetes-deployment.md)** (35 min)
   - Deploy gateway to K8s with high availability
   - Configure Kubernetes service discovery
   - Set up RBAC and network policies
   - Horizontal pod autoscaling
   - Production security best practices

5. **[Docker Compose Setup](05-docker-compose.md)** (30 min)
   - Complete containerized stack
   - Gateway, Router, Redis deployment
   - Prometheus and Grafana monitoring
   - Production hardening techniques

6. **[High Availability Setup](06-ha-deployment.md)** (50 min)
   - Multi-region deployment (3+ regions)
   - Redis Cluster with cross-region replication
   - Global load balancing with AWS/CloudFlare
   - Automated failover procedures
   - Disaster recovery and backups
   - 99.99% uptime SLA capability

## Advanced Topics

### 🔧 Advanced Features

7. **[Load Balancing](07-load-balancing.md)** (35 min)
   - 5 load balancing strategies (Round Robin, Least Connections, Weighted, IP Hash, Least Response Time)
   - Advanced health check configuration
   - Circuit breaker patterns
   - Sticky sessions and session affinity
   - Performance benchmarking

8. **[Authentication & Security](08-authentication.md)** (35 min)
   - Bearer token authentication
   - JWT with RSA keys
   - OAuth2 integration
   - mTLS (mutual TLS) setup
   - Per-message authentication
   - Security best practices

9. **[Protocol Selection Guide](09-protocols.md)** (25 min)
   - All 5 protocols explained (WebSocket, HTTP, SSE, TCP Binary, stdio)
   - Multi-frontend architecture
   - Performance comparison
   - Protocol selection decision tree
   - Client examples for each protocol
   - Configuration best practices

10. **[Monitoring & Observability](10-monitoring.md)** (40 min)
    - Complete Prometheus setup with custom alerts
    - Grafana dashboards for Gateway & Router
    - Jaeger distributed tracing
    - Loki log aggregation
    - Service Level Objectives (SLOs)
    - Custom exporters and metrics

## Troubleshooting Guides

### 🔍 Common Issues

11. **[Connection Troubleshooting](11-connection-troubleshooting.md)** (25 min)
    - Gateway startup failures
    - Client connection problems
    - Backend connectivity issues
    - Intermittent connection drops
    - High latency diagnosis
    - Complete debugging toolkit

12. **[Performance Tuning](12-performance-tuning.md)** (35 min)
    - Connection pool optimization
    - Buffer and runtime tuning
    - Caching strategies
    - Resource limit optimization
    - Benchmark results (85k RPS achieved)
    - Load testing methodology

## Use Case Tutorials

### 💼 Real-World Scenarios

13. **[API Gateway Pattern](13-api-gateway.md)** (30 min)
    - Microservice routing configuration
    - API versioning (v1, v2, v3)
    - Request/response transformation
    - Response aggregation
    - Per-API-key rate limiting

14. **[Multi-Tenant Setup](14-multi-tenant.md)** (30 min)
    - Kubernetes namespace-based isolation
    - Per-tenant authentication and JWT
    - Resource quotas and limits
    - Tenant-specific rate limiting
    - Network policies for isolation
    - Isolated metrics per tenant

15. **[Development Workflow](15-dev-workflow.md)** (25 min)
    - Local development environment setup
    - Hot reload with Air
    - Debugging with Delve and VS Code
    - Testing workflow (unit, integration, e2e)
    - CI/CD with GitHub Actions
    - Code quality tools and pre-commit hooks
    - Release automation

## Tutorial Format

Each tutorial follows this structure:

- **Prerequisites**: What you need before starting
- **What You'll Build**: Expected outcomes
- **Step-by-Step Instructions**: Detailed walkthrough
- **Testing**: How to verify it works
- **Troubleshooting**: Common issues and solutions
- **Next Steps**: Where to go from here

## Additional Resources

- [Usage Guide](../USAGE.md) - General usage documentation
- [Client Integration Guide](../client-integration.md) - Building clients
- [Server Integration Guide](../server-integration.md) - Building servers
- [Configuration Reference](../configuration.md) - All config options
- [Deployment Guides](../deployment/) - Production deployment

## Contributing Tutorials

Have a tutorial idea? We welcome contributions!

1. Fork the repository
2. Create your tutorial in `docs/tutorials/`
3. Follow the tutorial format above
4. Add it to this README
5. Submit a pull request

See [Contributing Guide](../../.github/CONTRIBUTING.md) for details.
