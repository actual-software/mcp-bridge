# ADR-0003: Multi-Method Authentication Strategy

**Status**: Accepted

**Date**: 2025-08-03

**Authors**: @poiley

## Context

The MCP Bridge needs to authenticate clients connecting to the Gateway to ensure secure access to backend MCP servers. Different deployment environments have varying authentication requirements and existing infrastructure.

### Requirements

- Support multiple authentication methods for different use cases
- Integration with existing enterprise identity providers
- Secure credential storage and transmission
- Support for both user and service authentication
- Token refresh without connection interruption
- Audit logging for compliance

### Constraints

- Must not require changes to MCP protocol
- Cannot store credentials in plain text
- Must support standard authentication protocols
- Need to work with existing OAuth2/OIDC providers

## Decision

Implement a pluggable multi-method authentication system supporting:
1. **Bearer Token** (JWT) - Primary method for simplicity
2. **OAuth2** with token introspection - Enterprise SSO integration  
3. **Mutual TLS (mTLS)** - Zero-trust environments
4. **API Keys** - Service-to-service authentication

Authentication happens at the Gateway level, with the Router managing credential storage and token refresh.

### Implementation Details

```yaml
# Gateway configuration
auth:
  methods:
    - type: bearer
      jwks_url: "https://auth.example.com/.well-known/jwks.json"
      issuer: "https://auth.example.com"
      audience: "mcp-gateway"
    
    - type: oauth2
      introspection_url: "https://auth.example.com/oauth2/introspect"
      client_id: "gateway-client"
      client_secret: "${OAUTH_CLIENT_SECRET}"
    
    - type: mtls
      ca_cert: "/certs/ca.pem"
      require_cn: ["client.example.com"]
    
    - type: api_key
      header_name: "X-API-Key"
      keys_file: "/etc/mcp/api-keys.yaml"
```

## Consequences

### Positive

- **Flexibility**: Organizations can choose appropriate auth method
- **Enterprise ready**: Integrates with existing identity infrastructure
- **Security**: No credential storage in Gateway, tokens are short-lived
- **Standards-based**: Uses established protocols (OAuth2, JWT, mTLS)
- **Auditability**: All authentication attempts are logged
- **Scalable**: Stateless authentication (except OAuth2 introspection)
- **Defense in depth**: Multiple auth methods can be combined

### Negative

- **Complexity**: Multiple code paths for different auth methods
- **Configuration**: More complex setup and troubleshooting
- **Dependencies**: Requires external services for some methods
- **Performance**: OAuth2 introspection adds latency

### Neutral

- Token refresh requires Router-side implementation
- Different methods have different security properties
- Some methods require additional infrastructure

## Alternatives Considered

### Alternative 1: Single JWT-Only Authentication

**Description**: Only support JWT tokens for all authentication

**Pros**:
- Simple implementation and configuration
- Stateless and scalable
- Fast token validation

**Cons**:
- Doesn't integrate with all enterprise systems
- No support for certificate-based auth
- Requires JWT infrastructure

**Reason for rejection**: Too limiting for enterprise deployments with existing auth infrastructure.

### Alternative 2: Custom Authentication Protocol

**Description**: Design custom authentication protocol for MCP Bridge

**Pros**:
- Can optimize for specific use cases
- Full control over implementation
- No external dependencies

**Cons**:
- Security risks of custom crypto
- No ecosystem support
- Higher implementation effort
- Audit and compliance concerns

**Reason for rejection**: Security risks and lack of standards compliance outweigh benefits.

### Alternative 3: Delegate to Service Mesh

**Description**: Let Istio/Linkerd handle all authentication

**Pros**:
- Leverage existing service mesh features
- Consistent with cloud-native patterns
- Centralized policy management

**Cons**:
- Requires service mesh deployment
- Not all environments use service mesh
- Less control over auth flow
- Harder to support multiple methods

**Reason for rejection**: Too restrictive for deployments without service mesh.

## References

- [OAuth 2.0 RFC 6749](https://datatracker.ietf.org/doc/html/rfc6749)
- [JWT RFC 7519](https://datatracker.ietf.org/doc/html/rfc7519)
- [OAuth 2.0 Token Introspection RFC 7662](https://datatracker.ietf.org/doc/html/rfc7662)
- [Mutual TLS Authentication](https://datatracker.ietf.org/doc/html/rfc8705)

## Notes

Future enhancements:
- Add support for SAML for legacy enterprise systems
- Implement WebAuthn for passwordless authentication
- Add support for hardware tokens (FIDO2)
- Consider mTLS with certificate rotation automation