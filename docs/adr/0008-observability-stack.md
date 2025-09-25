# ADR-0008: OpenTelemetry for Observability

**Status**: Accepted

**Date**: 2025-08-08

**Authors**: @poiley

## Context

MCP Bridge requires comprehensive observability to monitor system health, debug issues, and ensure SLA compliance. We need distributed tracing to track requests across Router and Gateway components, metrics for performance monitoring, and structured logging for debugging.

### Requirements

- Distributed tracing across all components
- Metrics collection with minimal performance impact
- Structured logging with correlation IDs
- Support for multiple backend systems (Datadog, New Relic, Jaeger)
- Auto-instrumentation where possible
- Custom business metrics support
- Low overhead (< 1% performance impact)

### Constraints

- Must work with existing monitoring infrastructure
- Cannot significantly impact latency
- Must support high cardinality metrics
- Need to control data volume for cost management

## Decision

Adopt OpenTelemetry (OTel) as the unified observability framework for traces, metrics, and logs, with Prometheus for metrics storage and various backend options for traces.

### Implementation Details

```yaml
# OpenTelemetry configuration
observability:
  # Tracing
  tracing:
    enabled: true
    sampling_rate: 0.1  # 10% sampling in production
    exporter: otlp
    endpoint: "otel-collector:4317"
    
  # Metrics  
  metrics:
    enabled: true
    interval: 15s
    exporters:
      - prometheus  # Pull-based
      - otlp       # Push-based
    
  # Logging
  logging:
    enabled: true
    level: info
    format: json
    correlation: true  # Add trace/span IDs

# Key metrics to track
metrics:
  - mcp_request_duration_seconds
  - mcp_request_total
  - mcp_active_connections
  - mcp_error_rate
  - mcp_auth_success_rate
  - mcp_circuit_breaker_state
```

## Consequences

### Positive

- **Vendor neutral**: Not locked into specific observability vendor
- **Comprehensive**: Single framework for traces, metrics, and logs
- **Auto-instrumentation**: Automatic instrumentation for common libraries
- **Standards-based**: Industry standard with wide adoption
- **Flexible**: Multiple exporter options for different backends
- **Context propagation**: Automatic trace context propagation
- **Low overhead**: Efficient implementation with sampling support
- **Rich ecosystem**: Extensive integrations available

### Negative

- **Complexity**: OTel configuration can be complex
- **Maturity**: Some components still evolving
- **Resource usage**: Collector requires additional resources
- **Learning curve**: Team needs to understand OTel concepts

### Neutral

- Requires OpenTelemetry Collector deployment
- Need to manage sampling strategies
- Storage backend selection still required

## Alternatives Considered

### Alternative 1: Prometheus + Jaeger + ELK

**Description**: Separate tools for each observability pillar

**Pros**:
- Mature, proven tools
- Each tool optimized for its purpose
- Large community support
- Good documentation

**Cons**:
- No unified instrumentation
- Manual correlation between signals
- Multiple SDKs to manage
- Higher operational overhead

**Reason for rejection**: Lack of unified instrumentation and correlation makes troubleshooting harder.

### Alternative 2: Proprietary APM (Datadog/New Relic)

**Description**: Use single vendor APM solution

**Pros**:
- Integrated experience
- Advanced analytics features
- Managed service (SaaS)
- Good automatic instrumentation

**Cons**:
- Vendor lock-in
- High cost at scale
- Data residency concerns
- Limited customization

**Reason for rejection**: Vendor lock-in and cost concerns for high-volume system.

### Alternative 3: Custom Observability

**Description**: Build custom observability solution

**Pros**:
- Full control
- Optimized for specific needs
- No external dependencies

**Cons**:
- High development effort
- Maintenance burden
- No ecosystem benefits
- Reinventing the wheel

**Reason for rejection**: Unjustified development and maintenance cost.

### Alternative 4: Service Mesh Observability

**Description**: Rely on Istio/Linkerd for observability

**Pros**:
- Automatic for mesh traffic
- No application changes
- Unified configuration
- Good L7 visibility

**Cons**:
- Requires service mesh
- Limited to network layer
- No application-level metrics
- Less flexibility

**Reason for rejection**: Not all deployments will use service mesh, and need application-level observability.

## References

- [OpenTelemetry Specification](https://opentelemetry.io/docs/reference/specification/)
- [OpenTelemetry Go SDK](https://opentelemetry.io/docs/instrumentation/go/)
- [W3C Trace Context](https://www.w3.org/TR/trace-context/)
- [Prometheus Best Practices](https://prometheus.io/docs/practices/)
- [Distributed Tracing Best Practices](https://www.cncf.io/blog/2022/05/18/distributed-tracing-best-practices/)

## Notes

Implementation guidelines:
- Start with 0.1% sampling and adjust based on volume
- Use baggage for custom context propagation
- Implement head-based sampling with option for tail-based
- Create custom dashboards for SLI/SLO monitoring
- Regular review of high-cardinality metrics
- Implement trace-to-logs correlation
- Consider edge sampling for high-volume endpoints