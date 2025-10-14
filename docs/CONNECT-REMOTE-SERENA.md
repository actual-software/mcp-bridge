# Connecting Claude Code to Remote Serena MCP Server

This guide explains how to connect your local Claude Code instance to the Serena MCP server deployed in the actualai-staging Kubernetes cluster.

## Architecture Overview

```
┌─────────────────┐         ┌──────────────────┐         ┌─────────────────┐
│  Claude Code    │◄───────►│  MCP Gateway     │◄───────►│  Serena MCP     │
│  (Local)        │         │  (K8s/EKS)       │         │  (K8s/EKS)      │
└─────────────────┘         └──────────────────┘         └─────────────────┘
   Your Machine              actualai-staging            actualai-staging
```

## Connection Methods

### Option 1: Direct Port Forward to Serena (Simplest)

Port forward directly to the Serena service in Kubernetes.

**Setup:**

```bash
# Port forward Serena to localhost
kubectl port-forward -n mcp svc/serena-mcp 8080:8080
```

**Claude Code Configuration:**

Add to your Claude Code MCP settings (`~/.config/claude-code/mcp.json` or via Claude Code settings):

```json
{
  "mcpServers": {
    "serena-staging": {
      "command": "npx",
      "args": [
        "-y",
        "@modelcontextprotocol/server-http-client",
        "http://localhost:8080"
      ]
    }
  }
}
```

**Pros:**
- ✅ Simple setup
- ✅ Direct connection to Serena
- ✅ No authentication required
- ✅ Full access to all Serena tools

**Cons:**
- ❌ Requires keeping `kubectl port-forward` running
- ❌ No security/authentication
- ❌ Single connection (no load balancing)
- ❌ Cannot share across team

**When to use:** Local development and testing

---

### Option 2: Port Forward to Gateway (Through Load Balancer)

Use the MCP Gateway as a proxy with port forwarding.

**Setup:**

```bash
# Port forward the gateway service
kubectl port-forward -n mcp svc/mcp-gateway-mcp-bridge-gateway 8443:8443
```

**Claude Code Configuration:**

```json
{
  "mcpServers": {
    "serena-via-gateway": {
      "command": "npx",
      "args": [
        "-y",
        "@modelcontextprotocol/server-http-client",
        "https://localhost:8443"
      ],
      "env": {
        "NODE_TLS_REJECT_UNAUTHORIZED": "0"
      }
    }
  }
}
```

**Pros:**
- ✅ Goes through gateway (centralized routing)
- ✅ Gateway features available (rate limiting, metrics)
- ✅ Can route to multiple backends

**Cons:**
- ❌ Requires port-forward running
- ❌ TLS certificate issues (self-signed)
- ❌ Gateway currently requires JWT authentication
- ❌ More complex than direct connection

**When to use:** Testing gateway routing locally

---

### Option 3: Connect via LoadBalancer (Production)

Connect directly to the AWS LoadBalancer endpoint.

**LoadBalancer URL:**
```
k8s-mcp-mcpgatew-dae950c5b5-2f1dace9a3e601aa.elb.us-west-2.amazonaws.com:8443
```

**Claude Code Configuration:**

```json
{
  "mcpServers": {
    "serena-production": {
      "command": "npx",
      "args": [
        "-y",
        "@modelcontextprotocol/server-http-client",
        "https://k8s-mcp-mcpgatew-dae950c5b5-2f1dace9a3e601aa.elb.us-west-2.amazonaws.com:8443"
      ],
      "env": {
        "MCP_AUTH_TOKEN": "your-jwt-token-here",
        "NODE_TLS_REJECT_UNAUTHORIZED": "0"
      }
    }
  }
}
```

**Prerequisites:**
1. Generate JWT token for authentication
2. Configure DNS/TLS properly or disable TLS verification
3. Ensure LoadBalancer security groups allow your IP

**Pros:**
- ✅ Production-grade connection
- ✅ No local port forwarding needed
- ✅ Can share across team
- ✅ High availability (multiple gateway replicas)
- ✅ Gateway features (auth, rate limiting, metrics)

**Cons:**
- ❌ Requires authentication setup
- ❌ TLS certificate management
- ❌ Network security configuration
- ❌ More complex troubleshooting

**When to use:** Production usage, team sharing

---

### Option 4: MCP Router (Recommended for Production)

Deploy the MCP Router locally or as a sidecar to manage connections.

**Setup:**

```bash
# Install mcp-router locally
go install github.com/actual-software/mcp-bridge/services/router/cmd/mcp-router@latest

# Or use Docker
docker run -p 8080:8080 \
  ghcr.io/actual-software/mcp-bridge-router:1.0.0-rc2 \
  --config router-config.yaml
```

**Router Configuration (`router-config.yaml`):**

```yaml
server:
  port: 8080
  protocol: http

gateway_pools:
  - name: staging-pool
    gateways:
      - url: https://k8s-mcp-mcpgatew-dae950c5b5-2f1dace9a3e601aa.elb.us-west-2.amazonaws.com:8443
        weight: 100
    health_check:
      enabled: true
      interval: 30s

auth:
  jwt:
    secret_key_env: JWT_SECRET_KEY
```

**Claude Code Configuration:**

```json
{
  "mcpServers": {
    "serena-routed": {
      "command": "npx",
      "args": [
        "-y",
        "@modelcontextprotocol/server-http-client",
        "http://localhost:8080"
      ]
    }
  }
}
```

**Pros:**
- ✅ Local router handles complexity
- ✅ Connection pooling
- ✅ Automatic failover
- ✅ Load balancing across gateways
- ✅ Credential management
- ✅ Protocol translation

**Cons:**
- ❌ Additional component to run
- ❌ More initial setup

**When to use:** Production desktop applications, team deployments

---

## Quick Start: Recommended Approach

For immediate testing, use **Option 1** (Direct Port Forward):

```bash
# Terminal 1: Start port forward
kubectl port-forward -n mcp svc/serena-mcp 8080:8080

# Terminal 2: Configure Claude Code
# Add to MCP settings:
{
  "mcpServers": {
    "serena": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-http-client", "http://localhost:8080"]
    }
  }
}

# Restart Claude Code or reload MCP servers
```

## Current Limitations

### Serena HTTP Protocol Issue

Currently, Serena is deployed with `streamable-http` transport, but the exact HTTP endpoints are returning 404:

```bash
# These return 404:
curl http://serena-mcp:8080/
curl http://serena-mcp:8080/sse
curl http://serena-mcp:8080/message
```

**This means the connection will fail until we:**

1. **Verify Serena's actual HTTP endpoints:**
   ```bash
   kubectl exec -n mcp serena-mcp-xxx -- curl localhost:8080
   ```

2. **Check Serena documentation for correct endpoints:**
   - Serena may use different paths for streamable-http
   - May need to use SSE protocol differently
   - Might need to send POST requests with specific headers

3. **Consider using Serena via stdio instead:**
   Update deployment to use stdio transport and create a bridge service

### Gateway Authentication

The MCP Gateway currently requires JWT authentication. To connect through the gateway, you need to:

1. Generate a JWT token:
   ```bash
   # Get the JWT secret from Kubernetes
   JWT_SECRET=$(kubectl get secret -n mcp mcp-gateway-mcp-bridge-secrets \
     -o jsonpath='{.data.jwt-secret-key}' | base64 -d)

   # Generate a token (requires jwt CLI tool or script)
   # Example payload: {"sub": "user@example.com", "exp": <timestamp>}
   ```

2. Pass the token in Claude Code configuration:
   ```json
   {
     "env": {
       "MCP_AUTH_TOKEN": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."
     }
   }
   ```

## Troubleshooting

### Port Forward Dies
```bash
# Use autossh or a wrapper script to auto-restart
while true; do
  kubectl port-forward -n mcp svc/serena-mcp 8080:8080
  sleep 2
done
```

### Connection Refused
- Verify port forward is running: `lsof -i :8080`
- Check Serena pod is healthy: `kubectl get pods -n mcp -l app=serena-mcp`
- Check Serena logs: `kubectl logs -n mcp -l app=serena-mcp`

### 404 Errors
- Serena may not be exposing HTTP endpoints correctly
- Check Serena startup logs for actual endpoint paths
- Verify streamable-http transport is compatible with MCP HTTP client

### TLS Certificate Errors
```bash
# Temporary workaround for self-signed certs
export NODE_TLS_REJECT_UNAUTHORIZED=0
```

For production, properly configure TLS certificates.

## Next Steps

1. **Fix Serena HTTP endpoints** - Investigate why Serena returns 404
2. **Set up DNS** - Create `mcp-gateway.staging.actualai.io` CNAME
3. **Configure proper TLS** - Use AWS ACM or cert-manager
4. **Set up authentication** - Generate and distribute JWT tokens
5. **Deploy MCP Router** - For production desktop client connections

## Resources

- [MCP HTTP Transport Spec](https://spec.modelcontextprotocol.io/specification/2024-11-05/transports/#http-with-sse)
- [Serena Documentation](https://github.com/oraios/serena)
- [MCP Gateway Architecture](../services/gateway/docs/ARCHITECTURE.md)
- [MCP Router Setup](../services/router/README.md)
