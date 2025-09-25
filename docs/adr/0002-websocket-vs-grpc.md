# ADR-0002: WebSocket vs gRPC for Client-Gateway Communication

**Status**: Accepted

**Date**: 2025-08-02

**Authors**: @poiley

## Context

We need to choose the primary communication protocol between the Router (client-side) and Gateway (server-side) components. This decision impacts performance, compatibility, security, and operational complexity.

The MCP protocol itself is transport-agnostic, so we have flexibility in choosing the underlying transport mechanism.

### Requirements

- Bidirectional communication for request/response and server-push
- Support for long-lived connections
- Efficient binary message transmission
- Works through corporate proxies and firewalls
- TLS encryption support
- Connection multiplexing capabilities
- Client libraries for multiple languages

### Constraints

- Must work in restricted network environments
- Need to support both streaming and request/response patterns
- Should minimize latency and bandwidth usage
- Must handle connection failures gracefully

## Decision

We will use WebSocket as the primary protocol for Router-Gateway communication, with an optional high-performance binary TCP protocol for specialized deployments.

### Implementation Details

- WebSocket over TLS (WSS) for secure communication
- JSON-RPC 2.0 for message format by default
- Optional MessagePack for binary encoding
- Automatic reconnection with exponential backoff
- Connection pooling for multiple concurrent requests
- Optional binary TCP protocol for internal/controlled networks

## Consequences

### Positive

- **Firewall friendly**: Works over standard HTTPS ports (443/8443)
- **Proxy support**: Compatible with HTTP proxies and corporate networks
- **Bidirectional**: Native support for server-push and streaming
- **Wide support**: Excellent client library availability
- **Debugging**: Text-based protocol easier to debug and monitor
- **Browser compatible**: Can be used from web applications
- **Established standard**: Well-understood protocol with mature implementations

### Negative

- **Overhead**: HTTP upgrade handshake and frame overhead
- **Head-of-line blocking**: Single TCP connection can cause blocking
- **No multiplexing**: Single stream per connection (unlike HTTP/2)
- **Stateful**: Requires connection state management

### Neutral

- Text/binary flexibility allows optimization where needed
- Requires connection pooling for optimal performance
- Keep-alive mechanisms needed for long-lived connections

## Alternatives Considered

### Alternative 1: gRPC

**Description**: High-performance RPC framework using HTTP/2

**Pros**:
- Built-in code generation from protobuf
- Efficient binary protocol
- Multiplexing over single connection
- Built-in retry and deadline support
- Strong typing with protobuf

**Cons**:
- Poor proxy support (many corporate proxies don't support HTTP/2)
- More complex debugging (binary protocol)
- Requires protobuf schema management
- Less flexible for dynamic message formats
- Browser support requires gRPC-Web proxy

**Reason for rejection**: Proxy compatibility issues in enterprise environments and added complexity of protobuf schema management outweigh performance benefits.

### Alternative 2: Raw TCP Sockets

**Description**: Direct TCP connection with custom protocol

**Pros**:
- Maximum performance and control
- No protocol overhead
- Can implement custom optimizations
- Direct binary transmission

**Cons**:
- Blocked by most corporate firewalls
- No built-in encryption (must implement TLS)
- Must implement all protocol features
- Limited client library support
- Complex proxy traversal

**Reason for rejection**: Firewall/proxy issues make it unsuitable as primary protocol, though we support it as an optional optimization.

### Alternative 3: HTTP/2 with Server-Sent Events

**Description**: HTTP/2 for requests with SSE for server push

**Pros**:
- Standards-based approach
- Good proxy support
- Multiplexing support
- Built-in flow control

**Cons**:
- Unidirectional server push (SSE)
- More complex to implement bidirectional flow
- Two different mechanisms to manage
- SSE has reconnection overhead

**Reason for rejection**: Complexity of managing two different mechanisms and limitations of SSE for bidirectional communication.

### Alternative 4: QUIC/HTTP/3

**Description**: Next-generation protocol with UDP-based transport

**Pros**:
- Eliminates head-of-line blocking
- Faster connection establishment
- Better mobile/unreliable network support
- Built-in encryption

**Cons**:
- Limited proxy/firewall support
- Immature ecosystem
- UDP often blocked in corporate networks
- Limited server/client implementations

**Reason for rejection**: Too early in adoption curve and poor enterprise network support.

## References

- [RFC 6455 - The WebSocket Protocol](https://datatracker.ietf.org/doc/html/rfc6455)
- [WebSocket Security Considerations](https://datatracker.ietf.org/doc/html/rfc6455#section-10)
- [gRPC vs WebSocket](https://www.wallarm.com/what/grpc-vs-websocket)
- [HTTP/2 WebSocket Support](https://datatracker.ietf.org/doc/html/rfc8441)

## Notes

Future considerations:
- Monitor QUIC/HTTP/3 adoption for potential future migration
- Consider gRPC for internal service-to-service communication
- Evaluate WebTransport when it becomes widely available
- May add HTTP/2 support if proxy compatibility improves