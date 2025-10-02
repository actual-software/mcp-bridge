# Tutorial: Your First MCP Server

Step-by-step guide to creating and deploying your first MCP server with MCP Bridge.

## Prerequisites

- MCP Bridge installed ([Installation Guide](../installation-and-setup.md))
- Basic knowledge of Python or Node.js
- 15-20 minutes

## What You'll Build

A simple weather forecast MCP server that:
- Exposes a `get_forecast` tool
- Accepts location as input
- Returns weather information
- Integrates with MCP Bridge gateway

## Step 1: Create the MCP Server

### Python Implementation

Create `weather_server.py`:

```python
#!/usr/bin/env python3
import json
import sys
from datetime import datetime

def get_forecast(location):
    """Get weather forecast for a location"""
    # In production, this would call a real weather API
    forecasts = {
        "San Francisco": "Sunny, 68°F",
        "New York": "Cloudy, 55°F",
        "London": "Rainy, 52°F"
    }

    forecast = forecasts.get(location, "Unknown location")

    return {
        "location": location,
        "forecast": forecast,
        "timestamp": datetime.now().isoformat()
    }

def handle_request(request):
    method = request.get('method')
    params = request.get('params', {})
    request_id = request.get('id')

    # Handle initialize
    if method == 'initialize':
        return {
            "jsonrpc": "2.0",
            "result": {
                "protocolVersion": "1.0",
                "capabilities": {"tools": {}},
                "serverInfo": {
                    "name": "weather-server",
                    "version": "1.0.0"
                }
            },
            "id": request_id
        }

    # Handle tools/list
    elif method == 'tools/list':
        return {
            "jsonrpc": "2.0",
            "result": {
                "tools": [{
                    "name": "get_forecast",
                    "description": "Get weather forecast for a location",
                    "inputSchema": {
                        "type": "object",
                        "properties": {
                            "location": {
                                "type": "string",
                                "description": "City name"
                            }
                        },
                        "required": ["location"]
                    }
                }]
            },
            "id": request_id
        }

    # Handle tools/call
    elif method == 'tools/call':
        tool_name = params.get('name')
        arguments = params.get('arguments', {})

        if tool_name == 'get_forecast':
            result = get_forecast(arguments.get('location'))
            return {
                "jsonrpc": "2.0",
                "result": {
                    "content": [{
                        "type": "text",
                        "text": f"Weather for {result['location']}: {result['forecast']}"
                    }]
                },
                "id": request_id
            }

    # Unknown method
    return {
        "jsonrpc": "2.0",
        "error": {
            "code": -32601,
            "message": f"Method not found: {method}"
        },
        "id": request_id
    }

def main():
    # Read JSON-RPC requests from stdin
    for line in sys.stdin:
        try:
            request = json.loads(line.strip())
            response = handle_request(request)
            print(json.dumps(response), flush=True)
        except Exception as e:
            error_response = {
                "jsonrpc": "2.0",
                "error": {
                    "code": -32603,
                    "message": f"Internal error: {str(e)}"
                },
                "id": None
            }
            print(json.dumps(error_response), flush=True)

if __name__ == '__main__':
    main()
```

Make it executable:
```bash
chmod +x weather_server.py
```

### Test the Server Locally

```bash
# Test with echo
echo '{"jsonrpc":"2.0","method":"tools/list","id":1}' | python weather_server.py

# Expected output:
# {"jsonrpc": "2.0", "result": {"tools": [{"name": "get_forecast", ...}]}, "id": 1}
```

## Step 2: Configure the Gateway

Create `gateway-weather.yaml`:

```yaml
version: 1

server:
  host: 0.0.0.0
  port: 8443
  protocol: websocket
  tls:
    enabled: false  # For development only

auth:
  provider: bearer

service_discovery:
  provider: stdio
  stdio:
    services:
      - name: weather
        namespace: tools
        command: ["python", "/app/weather_server.py"]
        working_dir: /app
        health_check:
          enabled: true
          interval: 30s

metrics:
  enabled: true
  endpoint: 0.0.0.0:9090

logging:
  level: info
  format: json
```

## Step 3: Start the Gateway

```bash
# Create app directory
mkdir -p /app
cp weather_server.py /app/

# Start gateway
mcp-gateway --config gateway-weather.yaml
```

Output:
```
{"level":"info","msg":"Starting MCP Gateway","port":8443}
{"level":"info","msg":"Discovered stdio service","name":"weather"}
{"level":"info","msg":"Gateway ready"}
```

## Step 4: Test with a Client

### Using WebSocket Client (wscat)

```bash
# Install wscat
npm install -g wscat

# Connect to gateway
wscat -c ws://localhost:8443

# In wscat prompt:
> {"jsonrpc":"2.0","method":"tools/list","id":1}
< {"jsonrpc":"2.0","result":{"tools":[...]},"id":1}

> {"jsonrpc":"2.0","method":"tools/call","params":{"name":"get_forecast","arguments":{"location":"San Francisco"}},"id":2}
< {"jsonrpc":"2.0","result":{"content":[{"type":"text","text":"Weather for San Francisco: Sunny, 68°F"}]},"id":2}
```

### Using HTTP Client (curl)

```bash
curl -X POST http://localhost:8443/mcp \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "method": "tools/call",
    "params": {
      "name": "get_forecast",
      "arguments": {"location": "New York"}
    },
    "id": 1
  }'
```

## Step 5: Monitor the Server

### Check Health

```bash
curl http://localhost:8443/health
```

Expected:
```json
{
  "status": "healthy",
  "services": {
    "weather": "healthy"
  }
}
```

### Check Metrics

```bash
curl http://localhost:9090/metrics | grep mcp_
```

Sample metrics:
```
mcp_gateway_requests_total{method="tools/list"} 1
mcp_gateway_requests_total{method="tools/call"} 1
mcp_gateway_request_duration_seconds_bucket{method="tools/call",le="0.1"} 1
```

## Step 6: Deploy to Production

### Option 1: Docker

Create `Dockerfile`:

```dockerfile
FROM python:3.11-slim

WORKDIR /app
COPY weather_server.py .

CMD ["python", "weather_server.py"]
```

Create `docker-compose.yml`:

```yaml
version: '3.8'

services:
  weather-server:
    build: .
    container_name: weather-mcp-server

  gateway:
    image: ghcr.io/actual-software/mcp-bridge/gateway:latest
    ports:
      - "8443:8443"
      - "9090:9090"
    volumes:
      - ./gateway-weather.yaml:/etc/mcp/gateway.yaml
    environment:
      - MCP_CONFIG=/etc/mcp/gateway.yaml
```

Start:
```bash
docker-compose up -d
```

### Option 2: Kubernetes

Create `weather-deployment.yaml`:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: weather-server
spec:
  replicas: 2
  selector:
    matchLabels:
      app: weather-server
  template:
    metadata:
      labels:
        app: weather-server
    spec:
      containers:
      - name: server
        image: myregistry/weather-server:1.0.0
        resources:
          limits:
            memory: "128Mi"
            cpu: "100m"
```

Deploy:
```bash
kubectl apply -f weather-deployment.yaml
```

Update gateway config for K8s discovery:

```yaml
service_discovery:
  provider: kubernetes
  kubernetes:
    in_cluster: true
    service_labels:
      app: weather-server
```

## Step 7: Add Authentication (Production)

Update `gateway-weather.yaml`:

```yaml
auth:
  provider: jwt
  jwt:
    issuer: mcp-gateway
    audience: mcp-tools
    secret_key_env: JWT_SECRET_KEY
```

Set secret:
```bash
export JWT_SECRET_KEY=$(openssl rand -base64 32)
```

Client must now send JWT token:
```bash
# Generate token (example with PyJWT)
python -c "
import jwt
import os
from datetime import datetime, timedelta

token = jwt.encode(
    {'aud': 'mcp-tools', 'exp': datetime.utcnow() + timedelta(hours=1)},
    os.environ['JWT_SECRET_KEY'],
    algorithm='HS256'
)
print(token)
"

# Use token
wscat -c ws://localhost:8443 -H "Authorization: Bearer $TOKEN"
```

## Troubleshooting

### Server Not Discovered

**Problem**: Gateway can't find stdio server

**Solution**:
```bash
# Check gateway logs
journalctl -u mcp-gateway -f

# Verify server runs independently
echo '{"jsonrpc":"2.0","method":"tools/list","id":1}' | python weather_server.py

# Check file permissions
ls -la weather_server.py
chmod +x weather_server.py
```

### Connection Refused

**Problem**: Can't connect to gateway

**Solution**:
```bash
# Check gateway is running
ps aux | grep mcp-gateway

# Check port is open
netstat -tlnp | grep 8443

# Check firewall
sudo ufw allow 8443
```

### Tool Call Fails

**Problem**: `tools/call` returns error

**Solution**:
```bash
# Enable debug logging
# In gateway-weather.yaml:
logging:
  level: debug

# Restart gateway
systemctl restart mcp-gateway

# Check detailed logs
journalctl -u mcp-gateway -f
```

## Next Steps

- [Add More Tools](02-multi-tool-server.md) - Create servers with multiple tools
- [Production Deployment](03-production-deployment.md) - Deploy with TLS, auth, and monitoring
- [Load Balancing](04-load-balancing.md) - Scale with multiple backend servers
- [Client Integration](../client-integration.md) - Build custom clients
- [Configuration Reference](../configuration.md) - Complete configuration options

## Summary

You've successfully:
- ✅ Created an MCP server with stdio protocol
- ✅ Configured MCP Bridge gateway
- ✅ Tested with WebSocket and HTTP clients
- ✅ Monitored server health and metrics
- ✅ Deployed to production environments

Your server is now ready to serve MCP requests through the gateway!
