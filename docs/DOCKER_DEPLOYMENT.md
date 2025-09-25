# Docker Deployment Guide

This guide provides comprehensive instructions for deploying MCP Bridge using Docker containers with enterprise-grade security and monitoring.

## 🐳 **Quick Start**

### **Deploy with Docker Compose**
```bash
# Clone the repository
git clone https://github.com/poiley/mcp-bridge.git
cd mcp-bridge

# Configure environment
cp .env.example .env
# Edit .env with your settings

# Deploy the stack
docker-compose up -d

# Verify deployment
docker-compose ps
docker-compose logs -f gateway
```

### **Access Services**
- **Gateway**: https://localhost:8443
- **Router Metrics**: http://localhost:9091/metrics
- **Prometheus**: http://localhost:9092
- **Grafana**: http://localhost:3000 (admin/admin)

## 🏗️ **Container Images**

### **Gateway Image**
```dockerfile
# Production-ready, security-hardened
FROM scratch
COPY mcp-gateway /usr/local/bin/
USER 65534:65534
EXPOSE 8443 9090
```

**Features**:
- **Minimal attack surface** - Scratch base image
- **Non-root execution** - Runs as nobody user
- **Read-only filesystem** - Immutable container
- **Resource limits** - Memory and CPU constraints
- **Health checks** - Built-in health monitoring

### **Router Image**
```dockerfile
# Lightweight, secure client
FROM scratch
COPY mcp-router /usr/local/bin/
USER 65534:65534
EXPOSE 9091
```

**Features**:
- **Ultra-lightweight** - <10MB image size
- **Security-first** - No package vulnerabilities
- **Optimized binary** - Statically linked, minimal dependencies
- **Health monitoring** - Built-in health checks

## 🛡️ **Security Features**

### **Container Security**
- ✅ **Minimal base images** - Scratch images with no OS packages
- ✅ **Non-root execution** - Runs as unprivileged user (65534)
- ✅ **Read-only filesystems** - Immutable containers
- ✅ **No new privileges** - Prevents privilege escalation
- ✅ **Vulnerability scanning** - Automated security scanning
- ✅ **Resource limits** - CPU and memory constraints

### **Network Security**
- ✅ **Internal networking** - Isolated Docker network
- ✅ **TLS encryption** - End-to-end encryption
- ✅ **Port restrictions** - Minimal exposed ports
- ✅ **Firewall ready** - Configurable port exposure

### **Data Security**
- ✅ **Volume encryption** - Encrypted data at rest
- ✅ **Secret management** - Environment-based secrets
- ✅ **Audit logging** - Comprehensive log collection
- ✅ **Backup ready** - Persistent volume management

## 📊 **Monitoring Stack**

### **Metrics Collection**
```yaml
# Prometheus configuration
scrape_configs:
  - job_name: 'mcp-gateway'
    static_configs:
      - targets: ['gateway:9090']
  - job_name: 'mcp-router'
    static_configs:
      - targets: ['router:9091']
```

### **Visualization**
- **Grafana dashboards** for real-time monitoring
- **Alert manager** for proactive notifications
- **Log aggregation** with centralized collection

### **Key Metrics**
- Request latency and throughput
- Error rates and success rates
- Resource utilization (CPU, memory)
- Connection pool statistics
- Authentication success/failure rates

## 🔧 **Configuration**

### **Environment Variables**
```bash
# .env file configuration
JWT_SECRET_KEY=your-secret-key
MCP_AUTH_TOKEN=your-auth-token
REDIS_PASSWORD=your-redis-password
GRAFANA_PASSWORD=your-grafana-password

# Optional settings
MCP_LOG_LEVEL=info
MCP_ENV=production
```

### **Volume Mounts**
```yaml
volumes:
  # Configuration files
  - ./config/gateway.yaml:/etc/mcp-gateway/config.yaml:ro
  - ./config/router.yaml:/etc/mcp-router/config.yaml:ro
  
  # TLS certificates
  - ./certs:/etc/mcp-gateway/certs:ro
  
  # Persistent data
  - redis-data:/data
  - prometheus-data:/prometheus
  - grafana-data:/var/lib/grafana
```

### **Resource Limits**
```yaml
deploy:
  resources:
    limits:
      memory: 256M      # Maximum memory usage
      cpus: '0.5'       # Maximum CPU usage
    reservations:
      memory: 128M      # Guaranteed memory
      cpus: '0.25'      # Guaranteed CPU
```

## 🚀 **Production Deployment**

### **Docker Swarm**
```bash
# Initialize swarm
docker swarm init

# Deploy stack
docker stack deploy -c docker-compose.yml mcp-bridge

# Scale services
docker service scale mcp-bridge_gateway=3
```

### **Kubernetes Alternative**
```bash
# Convert to Kubernetes manifests
kompose convert -f docker-compose.yml

# Or use Helm charts (see helm/ directory)
helm install mcp-bridge ./helm/mcp-bridge
```

### **Load Balancer Integration**
```yaml
# nginx.conf
upstream mcp_gateway {
    server gateway1:8443;
    server gateway2:8443;
    server gateway3:8443;
}

server {
    listen 443 ssl;
    location / {
        proxy_pass https://mcp_gateway;
    }
}
```

## 🔍 **Health Checks**

### **Built-in Health Checks**
```bash
# Gateway health
curl -k https://localhost:8443/health

# Router health (internal)
docker exec mcp-router /usr/local/bin/mcp-router health

# Service status
docker-compose ps
```

### **Health Check Configuration**
```yaml
healthcheck:
  test: ["/usr/local/bin/mcp-gateway", "health"]
  interval: 30s
  timeout: 10s
  retries: 3
  start_period: 10s
```

## 📋 **Maintenance**

### **Updates**
```bash
# Pull latest images
docker-compose pull

# Recreate containers
docker-compose up -d --force-recreate

# Zero-downtime updates
docker-compose up -d --scale gateway=2
docker-compose up -d --scale gateway=1
```

### **Backup**
```bash
# Backup persistent data
docker run --rm -v mcp-bridge_redis-data:/data -v $(pwd):/backup \
  alpine tar czf /backup/redis-backup.tar.gz /data

# Backup Grafana dashboards
docker run --rm -v mcp-bridge_grafana-data:/data -v $(pwd):/backup \
  alpine tar czf /backup/grafana-backup.tar.gz /data
```

### **Logs**
```bash
# View logs
docker-compose logs -f gateway
docker-compose logs -f router

# Export logs
docker-compose logs --no-color > mcp-bridge.log

# Log rotation
docker-compose logs --tail=1000 > recent.log
```

## 🐛 **Troubleshooting**

### **Common Issues**

#### **Container Won't Start**
```bash
# Check container status
docker-compose ps

# View detailed logs
docker-compose logs gateway

# Check resource usage
docker stats
```

#### **Network Issues**
```bash
# Test internal connectivity
docker-compose exec gateway ping redis
docker-compose exec router ping gateway

# Check port bindings
docker port mcp-gateway
```

#### **Performance Issues**
```bash
# Monitor resource usage
docker stats --no-stream

# Check health status
docker-compose exec gateway /usr/local/bin/mcp-gateway health
```

### **Debug Mode**
```bash
# Enable debug logging
MCP_LOG_LEVEL=debug docker-compose up -d

# Run with verbose output
docker-compose up --verbose
```

## 🔐 **Security Scanning**

### **Automated Scanning**
The Docker images are automatically scanned for vulnerabilities using:

- **Trivy** - Comprehensive vulnerability scanner
- **Grype** - Anchore vulnerability scanner  
- **Docker Scout** - Docker's security analysis

### **Manual Scanning**
```bash
# Scan local images
trivy image mcp-gateway:latest
grype mcp-gateway:latest

# Scan running containers
docker scout cves mcp-gateway
```

### **Security Reports**
Security scan results are automatically uploaded to GitHub Security tab for detailed analysis and tracking.

## 📈 **Performance Tuning**

### **Container Optimization**
```yaml
# Optimized settings
services:
  gateway:
    deploy:
      resources:
        limits:
          memory: 512M
          cpus: '1.0'
    environment:
      - GOMAXPROCS=2
      - GOGC=100
```

### **Network Optimization**
```yaml
# Custom network settings
networks:
  mcp-network:
    driver: bridge
    driver_opts:
      com.docker.network.bridge.name: mcp-br0
    ipam:
      config:
        - subnet: 172.20.0.0/16
```

## 📚 **Additional Resources**

### **Documentation**
- [Configuration Reference](./CONFIGURATION.md)
- [Security Best Practices](./SECURITY.md)
- [Monitoring Guide](./MONITORING.md)
- [Kubernetes Deployment](../deployments/kubernetes/README.md)

### **Examples**
- [Development Setup](../docker-compose.dev.yml)
- [Production Setup](../docker-compose.prod.yml)
- [High Availability](../docker-compose.ha.yml)

### **Support**
- [GitHub Issues](https://github.com/poiley/mcp-bridge/issues)
- [Discussions](https://github.com/poiley/mcp-bridge/discussions)
- [Security Reports](mailto:security@example.com)

---

**The Docker deployment provides a production-ready, secure, and scalable foundation for MCP Bridge enterprise deployments.**