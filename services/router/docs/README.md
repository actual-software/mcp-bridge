# MCP Router Documentation

Welcome to the comprehensive documentation for MCP Router. This documentation covers installation, configuration, operation, and troubleshooting of the router.

## Quick Links

- **[Getting Started](../README.md)** - Installation and basic setup
- **[Architecture](ARCHITECTURE.md)** - Detailed architecture and design (NEW)
- **[Migration Guide](MIGRATION.md)** - Upgrading from older versions
- **[Integration Guide](INTEGRATION.md)** - Gateway integration details
- **[API Reference](API.md)** - Configuration and API documentation
- **[Wire Protocol](WIRE_PROTOCOL.md)** - Protocol specifications
- **[Security Guide](SECURITY.md)** - Security best practices
- **[Troubleshooting](TROUBLESHOOTING.md)** - Common issues and solutions

## Documentation Overview

### For Users

1. **[README](../README.md)** - Start here for installation and basic configuration
2. **[Migration Guide](MIGRATION.md)** - If upgrading from an older version
3. **[Troubleshooting](TROUBLESHOOTING.md)** - When things don't work as expected
4. **[Security Guide](SECURITY.md)** - For production deployments

### For Administrators

1. **[Integration Guide](INTEGRATION.md)** - Detailed gateway integration
2. **[API Reference](API.md)** - Complete configuration reference
3. **[Security Guide](SECURITY.md)** - Security hardening
4. **[Wire Protocol](WIRE_PROTOCOL.md)** - Protocol details for debugging

### For Developers

1. **[Architecture](ARCHITECTURE.md)** - System design and component details
2. **[Wire Protocol](WIRE_PROTOCOL.md)** - Protocol implementation details
3. **[API Reference](API.md)** - Interface specifications
4. **[Integration Guide](INTEGRATION.md)** - Gateway feature support

## Key Features

### Multi-Protocol Support
- **WebSocket** (ws://, wss://) - Standard MCP protocol
- **TCP/Binary** (tcp://, tcps://) - High-performance binary protocol

### Authentication Methods
- **Bearer Tokens** - With per-message authentication
- **OAuth2** - Automatic token refresh
- **mTLS** - Certificate-based authentication

### Advanced Features
- **Request Queueing** - Buffers requests when disconnected (v2.0+)
- **Event-Driven Architecture** - No polling loops, fully reactive (v2.0+)
- **Passive Initialization** - Client-controlled protocol flow (v2.0+)
- **Rate Limiting** - Client-side request throttling
- **Circuit Breaker** - Automatic failure recovery
- **Metrics & Monitoring** - Prometheus-compatible metrics
- **Health Checks** - Built-in health endpoints

## Common Tasks

### Initial Setup

1. Install the router:
   ```bash
   curl -sSL https://raw.githubusercontent.com/poiley/mcp-bridge/main/services/router/install.sh | bash
   ```

2. Run setup wizard:
   ```bash
   mcp-router setup
   ```

3. Test connection:
   ```bash
   echo '{"jsonrpc":"2.0","method":"initialize","id":"1"}' | mcp-router
   ```

### Configuration

Create `~/.config/claude-cli/mcp-router.yaml`:

```yaml
gateway:
  url: "wss://gateway.example.com:8443"
  auth:
    type: bearer
    token_env: MCP_AUTH_TOKEN
```

Set environment variable:
```bash
export MCP_AUTH_TOKEN="your-token-here"
```

### Monitoring

Check health:
```bash
curl http://localhost:9091/health
```

View metrics:
```bash
curl http://localhost:9091/metrics | grep mcp_router
```

Enable debug logging:
```bash
MCP_LOGGING_LEVEL=debug mcp-router
```

## Architecture Overview

```
┌─────────────┐         ┌──────────────┐         ┌─────────────┐
│  Claude CLI │ <stdio> │  MCP Router  │ <ws/tcp>│ MCP Gateway │
└─────────────┘         └──────────────┘         └─────────────┘
                               │
                        ┌──────┴───────┐
                        │   Features   │
                        ├──────────────┤
                        │ - Auth       │
                        │ - Rate Limit │
                        │ - Metrics    │
                        │ - Retry      │
                        └──────────────┘
```

## Version Compatibility

| Router Version | Gateway Version | Protocol Support | Notes |
|----------------|-----------------|------------------|-------|
| 2.0+ | 2.0+ | WebSocket, TCP/Binary | Full feature support |
| 2.0+ | 1.x | WebSocket only | Limited features |
| 1.x | 1.x | WebSocket only | Legacy support |

## Support Channels

- **Issues**: [GitHub Issues](https://github.com/anthropics/mcp/issues)
- **Discussions**: [GitHub Discussions](https://github.com/anthropics/mcp/discussions)
- **Security**: security@example.com

## Contributing

See [CONTRIBUTING.md](../CONTRIBUTING.md) for guidelines on:
- Reporting bugs
- Requesting features
- Submitting pull requests
- Code style guidelines

## License

MCP Router is licensed under the MIT License. See [LICENSE](../LICENSE) for details.