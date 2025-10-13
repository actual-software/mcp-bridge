# Tutorial: API Gateway Pattern

Use MCP Bridge as an API gateway to route requests to microservices with versioning and transformation.

## Prerequisites

- MCP Bridge Gateway deployed
- Multiple microservices to route to
- Understanding of API gateway concepts
- 25-30 minutes

## What You'll Build

An API gateway that:
- Routes to multiple microservices
- Supports API versioning (v1, v2)
- Transforms requests/responses
- Aggregates multiple backend calls
- Rate limits per API key

## Architecture

```
Clients → MCP Gateway (API Gateway)
           ├─→ Auth Service (v1)
           ├─→ User Service (v1, v2)
           ├─→ Payment Service (v1)
           └─→ Analytics Service (v1)
```

## Configuration

```yaml
version: 1

server:
  host: 0.0.0.0
  port: 8443

# API Gateway routing
routing:
  strategy: least_connections

  # Route based on tool name pattern
  rules:
    - pattern: "auth/*"
      backends:
        - http://auth-service:8080
      version: v1

    - pattern: "users/*"
      backends:
        - http://user-service-v1:8080
        - http://user-service-v2:8081
      version_header: X-API-Version

    - pattern: "payments/*"
      backends:
        - http://payment-service:8080
      rate_limit:
        requests_per_sec: 100

# Request transformation
transformation:
  enabled: true
  rules:
    - match: "users/create"
      add_headers:
        X-Source: "api-gateway"
      remove_headers:
        - X-Internal-Token

# API versioning
versioning:
  enabled: true
  header: X-API-Version
  default_version: v1
  supported_versions:
    - v1
    - v2

auth:
  type: jwt
  jwt:
    issuer: "api-gateway"
    secret_key_env: JWT_SECRET_KEY

rate_limit:
  enabled: true
  per_api_key: true
  requests_per_sec: 1000
```

## Example Microservices

### Auth Service

```python
# auth-service.py
from flask import Flask, request, jsonify

app = Flask(__name__)

@app.route('/mcp', methods=['POST'])
def handle_mcp():
    data = request.json
    method = data.get('method')

    if method == 'tools/call':
        tool = data['params']['name']

        if tool == 'auth/login':
            return jsonify({
                "jsonrpc": "2.0",
                "result": {
                    "content": [{
                        "type": "text",
                        "text": "Login successful"
                    }]
                },
                "id": data['id']
            })

    return jsonify({"error": "Method not found"}), 404

if __name__ == '__main__':
    app.run(host='0.0.0.0', port=8080)
```

## API Versioning

```yaml
# Route based on version header
routing:
  version_routing:
    enabled: true
    header: X-API-Version

    mappings:
      v1:
        users: http://user-service-v1:8080
        payments: http://payment-service-v1:8080

      v2:
        users: http://user-service-v2:8080
        payments: http://payment-service-v2:8080
```

## Request Aggregation

```yaml
# Combine multiple backend calls
aggregation:
  enabled: true

  patterns:
    - name: "user_profile"
      combine:
        - tool: "users/get"
        - tool: "users/preferences"
        - tool: "users/activity"
      merge_strategy: "deep"
```

## Rate Limiting Per API Key

```yaml
rate_limit:
  enabled: true
  per_api_key: true

  limits:
    - api_key_pattern: "prod-*"
      requests_per_sec: 1000

    - api_key_pattern: "dev-*"
      requests_per_sec: 100

    - api_key_pattern: "free-*"
      requests_per_sec: 10
```

## Next Steps

- [Multi-Tenant Setup](14-multi-tenant.md)
- [Authentication & Security](08-authentication.md)

## Summary

API Gateway features:
- ✅ Microservice routing
- ✅ API versioning
- ✅ Request transformation
- ✅ Response aggregation
- ✅ Per-key rate limiting
