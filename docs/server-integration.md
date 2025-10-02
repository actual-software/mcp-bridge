# Server Integration Guide

Complete guide for integrating MCP servers with MCP Bridge gateway for universal protocol support.

## Table of Contents

- [Overview](#overview)
- [Server Protocols](#server-protocols)
- [Configuration Methods](#configuration-methods)
- [Implementation Examples](#implementation-examples)
- [Health Checks](#health-checks)
- [Load Balancing](#load-balancing)
- [Production Deployment](#production-deployment)

## Overview

MCP Bridge gateway supports multiple ways to connect to backend MCP servers:

| Protocol | Transport | Use Case | Configuration |
|----------|-----------|----------|---------------|
| stdio | Process/subprocess | Local CLI tools | Command and args |
| WebSocket | Network/ws(s):// | Remote servers | WebSocket URL |
| HTTP | Network/http(s):// | REST APIs | Base URL |
| SSE | Network/http(s):// | Streaming | Stream endpoint |

## Server Protocols

### stdio (Subprocess) Servers

**Best for**: Local command-line tools, scripts, and processes

**Protocol**: JSON-RPC over stdin/stdout

**Configuration**:

```yaml
service_discovery:
  provider: stdio
  stdio:
    services:
      - name: weather
        namespace: tools
        command: ["python", "/app/weather_server.py"]
        working_dir: /app
        env:
          API_KEY: "secret"
        weight: 1
        health_check:
          enabled: true
          interval: 30s
          timeout: 5s
```

**Server implementation** (Python):

```python
#!/usr/bin/env python3
import json
import sys

def handle_initialize(params):
    return {
        "protocolVersion": "1.0",
        "capabilities": {
            "tools": {}
        },
        "serverInfo": {
            "name": "weather-server",
            "version": "1.0.0"
        }
    }

def handle_tools_list(params):
    return {
        "tools": [
            {
                "name": "get_forecast",
                "description": "Get weather forecast for a location",
                "inputSchema": {
                    "type": "object",
                    "properties": {
                        "location": {"type": "string"}
                    },
                    "required": ["location"]
                }
            }
        ]
    }

def handle_tools_call(params):
    tool_name = params['name']
    arguments = params.get('arguments', {})

    if tool_name == "get_forecast":
        location = arguments['location']
        # Implement forecast logic
        return {
            "content": [
                {
                    "type": "text",
                    "text": f"Forecast for {location}: Sunny, 72°F"
                }
            ]
        }

def main():
    for line in sys.stdin:
        try:
            request = json.loads(line.strip())
            method = request.get('method')
            params = request.get('params', {})

            result = None
            if method == 'initialize':
                result = handle_initialize(params)
            elif method == 'tools/list':
                result = handle_tools_list(params)
            elif method == 'tools/call':
                result = handle_tools_call(params)
            else:
                response = {
                    "jsonrpc": "2.0",
                    "error": {
                        "code": -32601,
                        "message": f"Method not found: {method}"
                    },
                    "id": request.get('id')
                }
                print(json.dumps(response), flush=True)
                continue

            response = {
                "jsonrpc": "2.0",
                "result": result,
                "id": request.get('id')
            }

            print(json.dumps(response), flush=True)

        except Exception as e:
            error_response = {
                "jsonrpc": "2.0",
                "error": {
                    "code": -32603,
                    "message": str(e)
                },
                "id": request.get('id') if 'request' in locals() else None
            }
            print(json.dumps(error_response), flush=True)

if __name__ == '__main__':
    main()
```

**Node.js stdio server**:

```javascript
#!/usr/bin/env node
const readline = require('readline');

const rl = readline.createInterface({
  input: process.stdin,
  output: process.stdout,
  terminal: false
});

function handleInitialize(params) {
  return {
    protocolVersion: '1.0',
    capabilities: { tools: {} },
    serverInfo: {
      name: 'tools-server',
      version: '1.0.0'
    }
  };
}

function handleToolsList(params) {
  return {
    tools: [
      {
        name: 'execute_command',
        description: 'Execute a shell command',
        inputSchema: {
          type: 'object',
          properties: {
            command: { type: 'string' }
          },
          required: ['command']
        }
      }
    ]
  };
}

function handleToolsCall(params) {
  const { name, arguments: args } = params;

  if (name === 'execute_command') {
    // Implement command execution
    return {
      content: [
        { type: 'text', text: `Executed: ${args.command}` }
      ]
    };
  }

  throw new Error(`Unknown tool: ${name}`);
}

rl.on('line', (line) => {
  try {
    const request = JSON.parse(line);
    const { method, params, id } = request;

    let result;
    if (method === 'initialize') {
      result = handleInitialize(params);
    } else if (method === 'tools/list') {
      result = handleToolsList(params);
    } else if (method === 'tools/call') {
      result = handleToolsCall(params);
    } else {
      console.log(JSON.stringify({
        jsonrpc: '2.0',
        error: { code: -32601, message: `Method not found: ${method}` },
        id
      }));
      return;
    }

    console.log(JSON.stringify({
      jsonrpc: '2.0',
      result,
      id
    }));

  } catch (error) {
    console.log(JSON.stringify({
      jsonrpc: '2.0',
      error: { code: -32603, message: error.message },
      id: null
    }));
  }
});
```

### WebSocket Servers

**Best for**: Real-time bidirectional communication, long-lived connections

**Configuration**:

```yaml
service_discovery:
  provider: websocket
  websocket:
    services:
      - name: chat
        namespace: ai
        endpoints:
          - wss://chat-server.example.com:8080
          - wss://chat-server-backup.example.com:8080
        headers:
          X-API-Key: "secret"
        weight: 2
        tls:
          enabled: true
          insecure_skip_verify: false
          cert_file: /etc/certs/client.crt
          key_file: /etc/certs/client.key
        health_check:
          enabled: true
          interval: 30s
          timeout: 5s
```

**Server implementation** (Python with websockets):

```python
import asyncio
import json
import websockets

async def handle_client(websocket, path):
    async for message in websocket:
        try:
            request = json.loads(message)
            method = request.get('method')
            params = request.get('params', {})

            if method == 'initialize':
                result = {
                    "protocolVersion": "1.0",
                    "capabilities": {"tools": {}},
                    "serverInfo": {"name": "chat-server", "version": "1.0.0"}
                }
            elif method == 'tools/list':
                result = {
                    "tools": [{
                        "name": "chat",
                        "description": "Send a chat message",
                        "inputSchema": {
                            "type": "object",
                            "properties": {
                                "message": {"type": "string"}
                            },
                            "required": ["message"]
                        }
                    }]
                }
            elif method == 'tools/call':
                tool_name = params['name']
                arguments = params.get('arguments', {})

                if tool_name == 'chat':
                    result = {
                        "content": [{
                            "type": "text",
                            "text": f"Response to: {arguments['message']}"
                        }]
                    }
            else:
                await websocket.send(json.dumps({
                    "jsonrpc": "2.0",
                    "error": {"code": -32601, "message": f"Method not found: {method}"},
                    "id": request.get('id')
                }))
                continue

            response = {
                "jsonrpc": "2.0",
                "result": result,
                "id": request.get('id')
            }

            await websocket.send(json.dumps(response))

        except Exception as e:
            await websocket.send(json.dumps({
                "jsonrpc": "2.0",
                "error": {"code": -32603, "message": str(e)},
                "id": request.get('id') if 'request' in locals() else None
            }))

async def main():
    async with websockets.serve(handle_client, "0.0.0.0", 8080):
        await asyncio.Future()  # Run forever

if __name__ == "__main__":
    asyncio.run(main())
```

### HTTP Servers

**Best for**: RESTful services, stateless request/response

**Configuration**:

```yaml
service_discovery:
  provider: static
  static:
    endpoints:
      api:
        - url: https://api-server.example.com
          labels:
            protocol: http
            health_check_path: /health
            request_endpoint: /mcp/request
            timeout: 30s
```

**Server implementation** (Node.js with Express):

```javascript
const express = require('express');
const app = express();

app.use(express.json());

const tools = [
  {
    name: 'search',
    description: 'Search for information',
    inputSchema: {
      type: 'object',
      properties: {
        query: { type: 'string' }
      },
      required: ['query']
    }
  }
];

app.post('/mcp/request', (req, res) => {
  const { method, params, id } = req.body;

  try {
    let result;

    if (method === 'initialize') {
      result = {
        protocolVersion: '1.0',
        capabilities: { tools: {} },
        serverInfo: { name: 'api-server', version: '1.0.0' }
      };
    } else if (method === 'tools/list') {
      result = { tools };
    } else if (method === 'tools/call') {
      const { name, arguments: args } = params;

      if (name === 'search') {
        result = {
          content: [{
            type: 'text',
            text: `Search results for: ${args.query}`
          }]
        };
      } else {
        throw new Error(`Unknown tool: ${name}`);
      }
    } else {
      return res.json({
        jsonrpc: '2.0',
        error: { code: -32601, message: `Method not found: ${method}` },
        id
      });
    }

    res.json({
      jsonrpc: '2.0',
      result,
      id
    });

  } catch (error) {
    res.status(500).json({
      jsonrpc: '2.0',
      error: { code: -32603, message: error.message },
      id
    });
  }
});

app.get('/health', (req, res) => {
  res.json({ status: 'healthy' });
});

app.listen(8080, () => {
  console.log('MCP HTTP server listening on port 8080');
});
```

### SSE (Server-Sent Events) Servers

**Best for**: Streaming responses, real-time updates

**Configuration**:

```yaml
service_discovery:
  provider: sse
  sse:
    services:
      - name: events
        namespace: streaming
        base_url: https://sse-server.example.com
        stream_endpoint: /mcp/stream
        request_endpoint: /mcp/request
        timeout: 5m
        health_check:
          enabled: true
          interval: 30s
```

**Server implementation** (Python with FastAPI):

```python
from fastapi import FastAPI
from fastapi.responses import StreamingResponse
import json
import asyncio

app = FastAPI()

@app.post("/mcp/request")
async def handle_request(request: dict):
    method = request.get('method')
    params = request.get('params', {})
    request_id = request.get('id')

    if method == 'initialize':
        return {
            "jsonrpc": "2.0",
            "result": {
                "protocolVersion": "1.0",
                "capabilities": {"tools": {}},
                "serverInfo": {"name": "sse-server", "version": "1.0.0"}
            },
            "id": request_id
        }

    elif method == 'tools/list':
        return {
            "jsonrpc": "2.0",
            "result": {
                "tools": [{
                    "name": "stream_data",
                    "description": "Stream data in real-time",
                    "inputSchema": {
                        "type": "object",
                        "properties": {
                            "count": {"type": "integer"}
                        }
                    }
                }]
            },
            "id": request_id
        }

@app.get("/mcp/stream")
async def stream_events():
    async def event_generator():
        for i in range(10):
            event_data = {
                "type": "update",
                "data": {"count": i, "message": f"Event {i}"}
            }
            yield f"data: {json.dumps(event_data)}\n\n"
            await asyncio.sleep(1)

    return StreamingResponse(
        event_generator(),
        media_type="text/event-stream"
    )

@app.get("/health")
async def health():
    return {"status": "healthy"}
```

## Configuration Methods

### Static Configuration

Manually define all backend servers:

```yaml
service_discovery:
  provider: static
  static:
    endpoints:
      # Group backends by namespace
      weather:
        - url: http://weather-1.local:8080
          labels:
            region: us-west
            protocol: http
        - url: http://weather-2.local:8080
          labels:
            region: us-east
            protocol: http

      tools:
        - url: stdio://tools-server
          labels:
            command: ["node", "/app/tools.js"]
```

### Kubernetes Service Discovery

Automatically discover services in K8s:

```yaml
service_discovery:
  provider: kubernetes
  kubernetes:
    in_cluster: true
    namespace_pattern: "mcp-*"
    service_labels:
      mcp-enabled: "true"
      mcp-protocol: "http"
  refresh_rate: 30s
```

**Label your Kubernetes services**:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: weather-service
  namespace: mcp-production
  labels:
    mcp-enabled: "true"
    mcp-protocol: "http"
    mcp-namespace: weather
  annotations:
    mcp.bridge/health-check-path: "/health"
    mcp.bridge/request-path: "/mcp"
spec:
  selector:
    app: weather-server
  ports:
  - name: http
    port: 8080
    targetPort: 8080
```

### Dynamic Service Registration

Use API to register services at runtime:

```bash
curl -X POST https://gateway.example.com:8443/admin/services \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "dynamic-service",
    "namespace": "tools",
    "protocol": "websocket",
    "url": "ws://dynamic-server:8080",
    "weight": 1,
    "health_check": {
      "enabled": true,
      "interval": "30s"
    }
  }'
```

## Health Checks

### Implementing Health Checks

**stdio servers**: Gateway sends `ping` method periodically

```python
def handle_ping(params):
    return {"status": "ok", "timestamp": time.time()}
```

**HTTP/WebSocket servers**: Implement health endpoint

```javascript
app.get('/health', (req, res) => {
  // Check dependencies, database, etc.
  const isHealthy = checkDependencies();

  res.status(isHealthy ? 200 : 503).json({
    status: isHealthy ? 'healthy' : 'unhealthy',
    timestamp: Date.now(),
    details: {
      database: 'connected',
      cache: 'available'
    }
  });
});
```

### Health Check Configuration

```yaml
service_discovery:
  static:
    endpoints:
      myservice:
        - url: http://myserver:8080
          labels:
            health_check_enabled: true
            health_check_interval: 30s
            health_check_timeout: 5s
            health_check_path: /health
            unhealthy_threshold: 3
            healthy_threshold: 2
```

**Health check states**:
- **Healthy**: Passing health checks
- **Unhealthy**: Failing health checks (removed from pool)
- **Unknown**: No health check configured

## Load Balancing

### Load Balancing Strategies

**Round Robin** (default):
```yaml
routing:
  strategy: round_robin
```

**Least Connections**:
```yaml
routing:
  strategy: least_connections
```

**Weighted Round Robin**:
```yaml
service_discovery:
  static:
    endpoints:
      api:
        - url: http://api-1:8080
          labels:
            weight: 3  # 3x more traffic
        - url: http://api-2:8080
          labels:
            weight: 1
```

**Consistent Hashing** (session affinity):
```yaml
routing:
  strategy: consistent_hash
  hash_key: client_id  # Or: ip_address, session_id
```

### Namespace-Based Routing

Route requests to specific backends based on method patterns:

```yaml
routing:
  namespace_routing:
    enabled: true
    rules:
      # Route weather/* methods to weather servers
      - pattern: "^weather/.*"
        namespace: weather
        priority: 1

      # Route ai/* methods to ai servers
      - pattern: "^ai/.*"
        namespace: ai
        priority: 1

      # Default routing for other methods
      - pattern: ".*"
        namespace: default
        priority: 0
```

## Production Deployment

### Containerizing MCP Servers

**Dockerfile** (Python stdio server):

```dockerfile
FROM python:3.11-slim

WORKDIR /app

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY server.py .

CMD ["python", "server.py"]
```

**Dockerfile** (Node.js HTTP server):

```dockerfile
FROM node:18-alpine

WORKDIR /app

COPY package*.json ./
RUN npm ci --production

COPY server.js .

EXPOSE 8080
CMD ["node", "server.js"]
```

### Kubernetes Deployment

**Deployment manifest**:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: weather-mcp-server
  namespace: mcp-production
spec:
  replicas: 3
  selector:
    matchLabels:
      app: weather-server
  template:
    metadata:
      labels:
        app: weather-server
        mcp-enabled: "true"
    spec:
      containers:
      - name: server
        image: myregistry/weather-server:1.0.0
        ports:
        - containerPort: 8080
          name: http
        env:
        - name: API_KEY
          valueFrom:
            secretKeyRef:
              name: weather-secrets
              key: api-key
        livenessProbe:
          httpGet:
            path: /health
            port: 8080
          initialDelaySeconds: 10
          periodSeconds: 30
        readinessProbe:
          httpGet:
            path: /health
            port: 8080
          initialDelaySeconds: 5
          periodSeconds: 10
        resources:
          requests:
            memory: "128Mi"
            cpu: "100m"
          limits:
            memory: "256Mi"
            cpu: "500m"

---
apiVersion: v1
kind: Service
metadata:
  name: weather-mcp-server
  namespace: mcp-production
  labels:
    mcp-enabled: "true"
    mcp-protocol: "http"
    mcp-namespace: weather
spec:
  selector:
    app: weather-server
  ports:
  - port: 8080
    targetPort: 8080
    name: http
  type: ClusterIP
```

### Monitoring and Logging

**Export metrics** (Prometheus format):

```python
from prometheus_client import Counter, Histogram, start_http_server

request_count = Counter('mcp_requests_total', 'Total requests', ['method'])
request_duration = Histogram('mcp_request_duration_seconds', 'Request duration')

@request_duration.time()
def handle_request(request):
    method = request.get('method')
    request_count.labels(method=method).inc()
    # Handle request...

# Start metrics server
start_http_server(9090)
```

**Structured logging**:

```javascript
const winston = require('winston');

const logger = winston.createLogger({
  format: winston.format.json(),
  transports: [
    new winston.transports.Console()
  ]
});

app.post('/mcp/request', (req, res) => {
  const { method, params, id } = req.body;

  logger.info('MCP request received', {
    method,
    request_id: id,
    client_ip: req.ip,
    timestamp: new Date().toISOString()
  });

  // Handle request...
});
```

### Security Best Practices

**1. Validate inputs**:

```python
def validate_params(params, schema):
    # Validate against JSON schema
    jsonschema.validate(instance=params, schema=schema)

def handle_tools_call(params):
    validate_params(params, {
        "type": "object",
        "properties": {
            "name": {"type": "string"},
            "arguments": {"type": "object"}
        },
        "required": ["name"]
    })
    # Process request...
```

**2. Implement rate limiting**:

```javascript
const rateLimit = require('express-rate-limit');

const limiter = rateLimit({
  windowMs: 60 * 1000, // 1 minute
  max: 100 // Limit each IP to 100 requests per minute
});

app.use('/mcp', limiter);
```

**3. Use authentication**:

```python
def verify_token(token):
    try:
        payload = jwt.decode(token, SECRET_KEY, algorithms=['HS256'])
        return payload
    except jwt.InvalidTokenError:
        raise UnauthorizedError()

@app.post('/mcp/request')
async def handle_request(request: Request):
    token = request.headers.get('Authorization', '').replace('Bearer ', '')
    user = verify_token(token)
    # Process authenticated request...
```

**4. Sanitize outputs**:

```javascript
function sanitizeOutput(text) {
  // Remove sensitive patterns
  return text
    .replace(/\b[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}\b/gi, '[EMAIL]')
    .replace(/\b\d{3}-\d{2}-\d{4}\b/g, '[SSN]')
    .replace(/\b(?:\d{4}[-\s]?){3}\d{4}\b/g, '[CARD]');
}
```

### Error Handling

**Return proper JSON-RPC errors**:

```python
def create_error_response(code, message, request_id=None, data=None):
    error = {
        "jsonrpc": "2.0",
        "error": {
            "code": code,
            "message": message
        },
        "id": request_id
    }
    if data:
        error["error"]["data"] = data
    return error

# Usage
try:
    result = process_request(params)
except ValueError as e:
    return create_error_response(-32602, "Invalid params", request_id, {"detail": str(e)})
except Exception as e:
    logger.error(f"Internal error: {e}")
    return create_error_response(-32603, "Internal error", request_id)
```

### Testing MCP Servers

**Unit tests**:

```python
import unittest

class TestMCPServer(unittest.TestCase):
    def test_tools_list(self):
        request = {
            "jsonrpc": "2.0",
            "method": "tools/list",
            "id": 1
        }
        response = handle_request(request)
        self.assertEqual(response['jsonrpc'], '2.0')
        self.assertIn('tools', response['result'])

    def test_tools_call(self):
        request = {
            "jsonrpc": "2.0",
            "method": "tools/call",
            "params": {
                "name": "get_forecast",
                "arguments": {"location": "NYC"}
            },
            "id": 2
        }
        response = handle_request(request)
        self.assertIn('content', response['result'])
```

**Integration tests with gateway**:

```bash
# Start gateway with test configuration
docker-compose -f docker-compose.test.yml up -d

# Register test server
curl -X POST http://localhost:8443/admin/services \
  -d '{"name":"test-server","url":"http://test-server:8080"}'

# Run tests
pytest tests/integration/

# Cleanup
docker-compose -f docker-compose.test.yml down
```

## See Also

- [Usage Guide](USAGE.md) - General usage documentation
- [Client Integration Guide](client-integration.md) - Building clients
- [Configuration Reference](configuration.md) - Complete configuration options
- [Protocol Documentation](protocol.md) - MCP protocol specification
- [Deployment Guide](deployment/production.md) - Production deployment
- [Monitoring Guide](monitoring.md) - Observability and metrics
