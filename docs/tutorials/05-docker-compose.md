# Tutorial: Docker Compose Setup

Deploy a complete MCP Bridge stack with Docker Compose including Gateway, Router, Redis, and monitoring.

## Prerequisites

- Docker 24.0+ installed
- Docker Compose 2.0+ installed
- Basic understanding of Docker
- 25-30 minutes

## What You'll Build

A complete containerized MCP Bridge deployment with:
- **Gateway** - Routes requests to backend MCP servers
- **Router** - Client-side component for direct connections
- **Redis** - Session storage and rate limiting
- **Prometheus** - Metrics collection
- **Grafana** - Monitoring dashboards
- **Sample MCP Server** - Example backend service

## Architecture

```
┌──────────────────────────────────────────────────┐
│  Docker Compose Stack                             │
│                                                   │
│  ┌─────────────┐    ┌──────────────┐            │
│  │   Gateway   │───►│    Redis     │            │
│  │   :8443     │    │    :6379     │            │
│  └──────┬──────┘    └──────────────┘            │
│         │                                         │
│         │ discover                                │
│         ▼                                         │
│  ┌─────────────┐                                 │
│  │ MCP Server  │                                 │
│  │   :8080     │                                 │
│  └─────────────┘                                 │
│                                                   │
│  ┌─────────────┐    ┌──────────────┐            │
│  │ Prometheus  │◄───│   Grafana    │            │
│  │   :9090     │    │    :3000     │            │
│  └─────────────┘    └──────────────┘            │
└───────────────────────────────────────────────────┘
```

## Step 1: Create Project Directory

```bash
mkdir mcp-bridge-docker
cd mcp-bridge-docker

# Create directory structure
mkdir -p config certs data/{redis,prometheus,grafana}
```

## Step 2: Create Configuration Files

### Gateway Configuration

Create `config/gateway.yaml`:

```yaml
version: 1

server:
  host: 0.0.0.0
  port: 8443
  protocol: websocket
  tls:
    enabled: false  # TLS termination at load balancer

auth:
  type: jwt
  jwt:
    issuer: "mcp-gateway"
    audience: "mcp-services"
    secret_key_env: JWT_SECRET_KEY
  per_message_auth: false

discovery:
  provider: static
  static:
    endpoints:
      default:
        - url: http://mcp-server:8080

routing:
  strategy: least_connections
  health_check_interval: 30s
  circuit_breaker:
    failure_threshold: 5
    success_threshold: 2
    timeout: 30s

sessions:
  type: redis
  ttl: 1h
  redis:
    url_env: REDIS_URL

rate_limit:
  enabled: true
  requests_per_sec: 1000
  burst: 200
  per_ip: true

observability:
  metrics:
    enabled: true
    port: 9090
    path: /metrics
  logging:
    level: info
    format: json
    output: stdout
```

### Router Configuration

Create `config/router.yaml`:

```yaml
version: 1

gateway:
  url: ws://gateway:8443
  auth:
    type: bearer
    token_env: GATEWAY_AUTH_TOKEN

servers:
  - name: local-server
    type: stdio
    command: ["/usr/local/bin/my-mcp-server"]
    working_dir: /app

observability:
  metrics:
    enabled: true
    port: 9091
  logging:
    level: info
    format: json
```

### Prometheus Configuration

Create `config/prometheus.yml`:

```yaml
global:
  scrape_interval: 15s
  evaluation_interval: 15s
  external_labels:
    cluster: 'docker-compose'
    environment: 'development'

scrape_configs:
  - job_name: 'mcp-gateway'
    static_configs:
      - targets: ['gateway:9090']
        labels:
          service: 'gateway'

  - job_name: 'mcp-router'
    static_configs:
      - targets: ['router:9091']
        labels:
          service: 'router'

  - job_name: 'redis'
    static_configs:
      - targets: ['redis-exporter:9121']
        labels:
          service: 'redis'
```

## Step 3: Create Docker Compose File

Create `docker-compose.yml`:

```yaml
version: '3.8'

services:
  # MCP Gateway
  gateway:
    image: ghcr.io/actual-software/mcp-bridge/gateway:latest
    container_name: mcp-gateway
    restart: unless-stopped
    ports:
      - "8443:8443"  # WebSocket
      - "9090:9090"  # Metrics
    environment:
      - CONFIG_FILE=/etc/mcp/gateway.yaml
      - JWT_SECRET_KEY=${JWT_SECRET_KEY}
      - REDIS_URL=redis://redis:6379
      - LOG_LEVEL=info
    volumes:
      - ./config/gateway.yaml:/etc/mcp/gateway.yaml:ro
    depends_on:
      redis:
        condition: service_healthy
    networks:
      - mcp-network
    healthcheck:
      test: ["CMD", "wget", "--quiet", "--tries=1", "--spider", "http://localhost:9090/healthz"]
      interval: 30s
      timeout: 10s
      retries: 3
      start_period: 10s

  # MCP Router
  router:
    image: ghcr.io/actual-software/mcp-bridge/router:latest
    container_name: mcp-router
    restart: unless-stopped
    ports:
      - "9091:9091"  # Metrics
    environment:
      - CONFIG_FILE=/etc/mcp/router.yaml
      - GATEWAY_AUTH_TOKEN=${GATEWAY_AUTH_TOKEN}
      - LOG_LEVEL=info
    volumes:
      - ./config/router.yaml:/etc/mcp/router.yaml:ro
    depends_on:
      gateway:
        condition: service_healthy
    networks:
      - mcp-network

  # Sample MCP Server
  mcp-server:
    build:
      context: ./server
      dockerfile: Dockerfile
    container_name: mcp-server
    restart: unless-stopped
    ports:
      - "8080:8080"
    environment:
      - PORT=8080
    networks:
      - mcp-network
    healthcheck:
      test: ["CMD", "wget", "--quiet", "--tries=1", "--spider", "http://localhost:8080/health"]
      interval: 30s
      timeout: 10s
      retries: 3

  # Redis for sessions and rate limiting
  redis:
    image: redis:7-alpine
    container_name: mcp-redis
    restart: unless-stopped
    ports:
      - "6379:6379"
    command: redis-server --appendonly yes
    volumes:
      - redis-data:/data
    networks:
      - mcp-network
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 10s
      timeout: 5s
      retries: 3

  # Redis Exporter for Prometheus
  redis-exporter:
    image: oliver006/redis_exporter:latest
    container_name: redis-exporter
    restart: unless-stopped
    ports:
      - "9121:9121"
    environment:
      - REDIS_ADDR=redis:6379
    depends_on:
      - redis
    networks:
      - mcp-network

  # Prometheus for metrics
  prometheus:
    image: prom/prometheus:latest
    container_name: mcp-prometheus
    restart: unless-stopped
    ports:
      - "9093:9090"
    volumes:
      - ./config/prometheus.yml:/etc/prometheus/prometheus.yml:ro
      - prometheus-data:/prometheus
    command:
      - '--config.file=/etc/prometheus/prometheus.yml'
      - '--storage.tsdb.path=/prometheus'
      - '--web.console.libraries=/usr/share/prometheus/console_libraries'
      - '--web.console.templates=/usr/share/prometheus/consoles'
    networks:
      - mcp-network

  # Grafana for dashboards
  grafana:
    image: grafana/grafana:latest
    container_name: mcp-grafana
    restart: unless-stopped
    ports:
      - "3000:3000"
    environment:
      - GF_SECURITY_ADMIN_PASSWORD=${GRAFANA_PASSWORD:-admin}
      - GF_USERS_ALLOW_SIGN_UP=false
    volumes:
      - grafana-data:/var/lib/grafana
      - ./config/grafana-datasources.yml:/etc/grafana/provisioning/datasources/datasources.yml:ro
    depends_on:
      - prometheus
    networks:
      - mcp-network

networks:
  mcp-network:
    driver: bridge

volumes:
  redis-data:
  prometheus-data:
  grafana-data:
```

## Step 4: Create Grafana Datasource

Create `config/grafana-datasources.yml`:

```yaml
apiVersion: 1

datasources:
  - name: Prometheus
    type: prometheus
    access: proxy
    url: http://prometheus:9090
    isDefault: true
    editable: false
```

## Step 5: Create Environment File

Create `.env`:

```bash
# JWT Secret (generate with: openssl rand -base64 32)
JWT_SECRET_KEY=your-secret-key-here

# Gateway Auth Token for Router
GATEWAY_AUTH_TOKEN=your-auth-token-here

# Grafana Admin Password
GRAFANA_PASSWORD=admin

# Optional: Redis Password
REDIS_PASSWORD=
```

Generate secrets:

```bash
# Generate JWT secret
echo "JWT_SECRET_KEY=$(openssl rand -base64 32)" >> .env

# Generate auth token
echo "GATEWAY_AUTH_TOKEN=$(openssl rand -base64 32)" >> .env
```

## Step 6: Create Sample MCP Server

Create `server/Dockerfile`:

```dockerfile
FROM python:3.11-slim

WORKDIR /app

COPY server.py .
COPY requirements.txt .

RUN pip install --no-cache-dir -r requirements.txt

EXPOSE 8080

CMD ["python", "server.py"]
```

Create `server/server.py`:

```python
#!/usr/bin/env python3
from flask import Flask, request, jsonify
import json

app = Flask(__name__)

@app.route('/health', methods=['GET'])
def health():
    return jsonify({"status": "healthy"})

@app.route('/mcp', methods=['POST'])
def handle_mcp():
    data = request.json
    method = data.get('method')

    if method == 'tools/list':
        return jsonify({
            "jsonrpc": "2.0",
            "result": {
                "tools": [{
                    "name": "echo",
                    "description": "Echo back the input",
                    "inputSchema": {
                        "type": "object",
                        "properties": {
                            "message": {"type": "string"}
                        }
                    }
                }]
            },
            "id": data.get('id')
        })

    elif method == 'tools/call':
        params = data.get('params', {})
        return jsonify({
            "jsonrpc": "2.0",
            "result": {
                "content": [{
                    "type": "text",
                    "text": f"Echo: {params.get('arguments', {}).get('message', '')}"
                }]
            },
            "id": data.get('id')
        })

    return jsonify({"error": "Method not found"}), 404

if __name__ == '__main__':
    app.run(host='0.0.0.0', port=8080)
```

Create `server/requirements.txt`:

```
Flask==3.0.0
```

## Step 7: Start the Stack

```bash
# Start all services
docker-compose up -d

# View logs
docker-compose logs -f

# Check status
docker-compose ps
```

Expected output:

```
NAME                 STATUS              PORTS
mcp-gateway          healthy             0.0.0.0:8443->8443/tcp, 0.0.0.0:9090->9090/tcp
mcp-router           running             0.0.0.0:9091->9091/tcp
mcp-redis            healthy             0.0.0.0:6379->6379/tcp
mcp-server           healthy             0.0.0.0:8080->8080/tcp
mcp-prometheus       running             0.0.0.0:9093->9090/tcp
mcp-grafana          running             0.0.0.0:3000->3000/tcp
```

## Step 8: Test the Deployment

### Test Gateway

```bash
# Test WebSocket connection
npm install -g wscat
wscat -c ws://localhost:8443

> {"jsonrpc":"2.0","method":"tools/list","id":1}
< {"jsonrpc":"2.0","result":{"tools":[...]},"id":1}
```

### Test Metrics

```bash
# Gateway metrics
curl http://localhost:9090/metrics | grep mcp_gateway

# Router metrics
curl http://localhost:9091/metrics | grep mcp_router

# Prometheus
curl http://localhost:9093/api/v1/query?query=up
```

### Access Grafana

1. Open http://localhost:3000
2. Login with `admin` / `admin` (or your configured password)
3. Navigate to Explore → Prometheus
4. Query: `rate(mcp_gateway_requests_total[5m])`

## Step 9: Production Hardening

### Enable TLS

Add TLS certificates:

```bash
# Generate self-signed cert (or use real certs)
openssl req -x509 -newkey rsa:4096 -nodes \
  -keyout certs/tls.key \
  -out certs/tls.crt \
  -days 365 \
  -subj "/CN=gateway.example.com"
```

Update `config/gateway.yaml`:

```yaml
server:
  tls:
    enabled: true
    cert_file: /etc/certs/tls.crt
    key_file: /etc/certs/tls.key
```

Update `docker-compose.yml`:

```yaml
gateway:
  volumes:
    - ./certs:/etc/certs:ro
```

### Resource Limits

```yaml
services:
  gateway:
    deploy:
      resources:
        limits:
          cpus: '1.0'
          memory: 512M
        reservations:
          cpus: '0.5'
          memory: 256M
```

### Health Checks

All services already have health checks configured. Monitor them:

```bash
# Check health status
docker-compose ps

# View specific health
docker inspect mcp-gateway | jq '.[0].State.Health'
```

## Step 10: Monitoring and Maintenance

### View Logs

```bash
# All services
docker-compose logs -f

# Specific service
docker-compose logs -f gateway

# With timestamps
docker-compose logs -f --timestamps gateway
```

### Backup Redis Data

```bash
# Backup
docker exec mcp-redis redis-cli BGSAVE
docker cp mcp-redis:/data/dump.rdb ./backup/

# Restore
docker cp ./backup/dump.rdb mcp-redis:/data/
docker-compose restart redis
```

### Scale Gateway

```bash
# Scale to 3 instances
docker-compose up -d --scale gateway=3

# With load balancer (add nginx)
```

### Update Services

```bash
# Pull latest images
docker-compose pull

# Recreate containers
docker-compose up -d --force-recreate

# Or rolling update
docker-compose up -d --no-deps --build gateway
```

## Troubleshooting

### Gateway Not Starting

```bash
# Check logs
docker-compose logs gateway

# Check configuration
docker-compose exec gateway cat /etc/mcp/gateway.yaml

# Validate config
docker-compose exec gateway mcp-gateway --validate
```

### Redis Connection Issues

```bash
# Test Redis connection
docker-compose exec gateway nc -zv redis 6379

# Check Redis logs
docker-compose logs redis

# Test Redis directly
docker-compose exec redis redis-cli ping
```

### Network Issues

```bash
# Inspect network
docker network inspect mcp-bridge-docker_mcp-network

# Test connectivity
docker-compose exec gateway ping mcp-server
```

## Cleanup

```bash
# Stop all services
docker-compose down

# Remove volumes (WARNING: deletes data)
docker-compose down -v

# Remove images
docker-compose down --rmi all
```

## Production Deployment

For production, consider:

1. **Use Docker Swarm or Kubernetes** for orchestration
2. **External Redis cluster** for high availability
3. **Load balancer** (nginx, Traefik) in front of Gateway
4. **Persistent volumes** for data
5. **Secrets management** (Docker secrets, Vault)
6. **Monitoring alerts** (Alertmanager)
7. **Backup strategy** for Redis and configs
8. **Log aggregation** (ELK, Loki)

## Next Steps

- [Kubernetes Deployment](04-kubernetes-deployment.md) - Deploy to K8s
- [Authentication & Security](08-authentication.md) - Secure your deployment
- [Monitoring & Observability](10-monitoring.md) - Advanced monitoring setup

## Summary

You've successfully:
- ✅ Deployed complete MCP Bridge stack with Docker Compose
- ✅ Set up Gateway, Router, and Redis
- ✅ Configured Prometheus and Grafana monitoring
- ✅ Created a sample MCP server
- ✅ Tested the entire stack
- ✅ Learned production hardening techniques

Your MCP Bridge is now running in containers with monitoring!
