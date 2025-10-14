# Service Discovery Provider Validation Bug

## Summary

The gateway's service discovery provider validation in `config_loader.go` only allows `kubernetes` and `static` providers, even though the codebase implements and supports additional discovery modes including `sse`, `websocket`, and `stdio`.

## Bug Location

**File:** `services/gateway/internal/server/config_loader.go`
**Lines:** 234-240

```go
// validateDiscoveryProvider validates the discovery provider type.
func validateDiscoveryProvider(provider string) error {
	if provider != "" && provider != AuthProviderKubernetes && provider != AuthProviderStatic {
		return fmt.Errorf("unsupported service discovery mode: %s", provider)
	}

	return nil
}
```

## Problem

The validation function only checks for two constants:
- `AuthProviderKubernetes` (value: `"kubernetes"`)
- `AuthProviderStatic` (value: `"static"`)

However, the gateway has full implementations for additional discovery providers in `internal/discovery/`:

| Provider | Implementation File | Config Validation | Status |
|----------|-------------------|-------------------|---------|
| `static` | `static.go` | ✅ `ValidateStaticDiscoveryConfig()` | ✅ Allowed |
| `kubernetes` | `kubernetes.go` | ✅ `ValidateKubernetesDiscoveryConfig()` | ✅ Allowed |
| `sse` | `sse_discovery.go` | ✅ `ValidateSSEDiscoveryConfig()` | ❌ Blocked by validation |
| `websocket` | *websocket implementation* | ✅ `ValidateWebSocketDiscoveryConfig()` | ❌ Blocked by validation |
| `stdio` | `stdio_discovery.go` | ✅ `ValidateStdioDiscoveryConfig()` | ❌ Blocked by validation |

## Impact

When attempting to use any non-whitelisted provider (e.g., `sse`), the gateway fails to start with:

```
Error: failed to load configuration: configuration validation failed: unsupported service discovery mode: sse
```

This prevents users from:
1. Using SSE discovery for MCP servers that expose Server-Sent Events endpoints
2. Using WebSocket discovery for dynamic WebSocket-based service registration
3. Using Stdio discovery for process-based service management

## Reproduction

1. Configure gateway with SSE service discovery:
   ```yaml
   service_discovery:
     provider: sse
     sse:
       services:
         - name: serena-mcp
           namespace: coding-assistant
           base_url: http://serena-mcp:8080
           stream_endpoint: /sse
           request_endpoint: /message
   ```

2. Attempt to start the gateway

3. Gateway crashes with validation error:
   ```
   Error: failed to load configuration: configuration validation failed:
   unsupported service discovery mode: sse
   ```

## Expected Behavior

The validation should accept all implemented discovery providers:
- `static`
- `kubernetes`
- `sse`
- `websocket`
- `stdio`

## Proposed Fix

Update `validateDiscoveryProvider()` in `services/gateway/internal/server/config_loader.go`:

```go
// validateDiscoveryProvider validates the discovery provider type.
func validateDiscoveryProvider(provider string) error {
	if provider == "" {
		return nil
	}

	validProviders := map[string]bool{
		"static":     true,
		"kubernetes": true,
		"sse":        true,
		"websocket":  true,
		"stdio":      true,
	}

	if !validProviders[provider] {
		return fmt.Errorf("unsupported service discovery mode: %s (valid options: static, kubernetes, sse, websocket, stdio)", provider)
	}

	return nil
}
```

**Alternative approach** - Remove the validation entirely since provider-specific validation already exists in `validateProviderConfig()` at line 489:

```go
func validateProviderConfig(provider string, config *ServiceDiscoveryConfig) error {
	switch provider {
	case "static":
		return ValidateStaticDiscoveryConfig(&config.Static)
	case "kubernetes":
		return ValidateKubernetesDiscoveryConfig(&config.Kubernetes)
	case "stdio":
		return ValidateStdioDiscoveryConfig(&config.Stdio)
	case "websocket":
		return ValidateWebSocketDiscoveryConfig(&config.WebSocket)
	case "sse":
		return ValidateSSEDiscoveryConfig(&config.SSE)
	default:
		return nil
	}
}
```

This function already validates all providers correctly, making the restrictive check in `validateDiscoveryProvider()` redundant.

## Workaround

Currently, users must use either:
1. **Static discovery** - Manually configure endpoint URLs
2. **Kubernetes discovery** - Requires RBAC setup and service account tokens

For SSE-based MCP servers (like Serena), users must configure them as static endpoints rather than using the native SSE discovery mechanism.

## Related Code

- **Discovery factory:** `services/gateway/internal/discovery/factory.go`
- **Config types:** `services/gateway/internal/config/types.go:116-128`
- **Config validation:** `services/gateway/internal/config/validation.go:488-504`

## Testing Recommendations

After fixing, test all discovery providers:

```bash
# Test SSE discovery
go test ./services/gateway/internal/discovery -run TestSSEDiscovery

# Test WebSocket discovery
go test ./services/gateway/internal/discovery -run TestWebSocketDiscovery

# Test Stdio discovery
go test ./services/gateway/internal/discovery -run TestStdioDiscovery

# Integration test with all providers
go test ./services/gateway/test/integration -run TestServiceDiscoveryProviders
```

## Priority

**High** - This bug prevents users from using 60% of the implemented service discovery features, forcing suboptimal workarounds.

## Version Affected

- Gateway version: v1.0.0-rc2
- All previous versions

## Reporter

Discovered during deployment of Serena MCP server to actualai-staging cluster on 2025-10-13.
