# Architectural Decision Records

This directory contains the Architectural Decision Records (ADRs) for the MCP Bridge project.

## What is an ADR?

An Architectural Decision Record (ADR) is a document that captures an important architectural decision made along with its context and consequences.

## ADR Format

We use a lightweight ADR format that consists of:

- **Title**: ADR-NNNN: Brief decision title
- **Status**: Proposed, Accepted, Deprecated, Superseded
- **Context**: What is the issue that we're seeing that is motivating this decision?
- **Decision**: What is the change that we're proposing/doing?
- **Consequences**: What becomes easier or harder because of this decision?
- **Alternatives Considered**: What other options were evaluated?

## ADR Index

| ADR | Title | Status | Date |
|-----|-------|--------|------|
| [ADR-0001](0001-use-go-for-implementation.md) | Use Go for Implementation | Accepted | 2025-08-01 |
| [ADR-0002](0002-websocket-vs-grpc.md) | WebSocket vs gRPC for Client-Gateway Communication | Accepted | 2025-08-02 |
| [ADR-0003](0003-authentication-strategy.md) | Multi-Method Authentication Strategy | Accepted | 2025-08-03 |
| [ADR-0004](0004-credential-storage.md) | Platform-Native Credential Storage | Accepted | 2025-08-04 |
| [ADR-0005](0005-connection-pooling.md) | Connection Pooling Architecture | Accepted | 2025-08-05 |
| [ADR-0006](0006-redis-for-session-state.md) | Redis for Distributed Session State | Accepted | 2025-08-06 |
| [ADR-0007](0007-circuit-breaker-pattern.md) | Circuit Breaker Implementation | Accepted | 2025-08-07 |
| [ADR-0008](0008-observability-stack.md) | OpenTelemetry for Observability | Accepted | 2025-08-08 |
| [ADR-0009](0009-binary-protocol.md) | Binary Protocol for Performance | Accepted | 2025-08-09 |
| [ADR-0010](0010-rate-limiting-strategy.md) | Multi-Layer Rate Limiting | Accepted | 2025-08-10 |
| [ADR-0011](0011-zero-trust-security.md) | Zero-Trust Security Model | Accepted | 2025-08-11 |
| [ADR-0012](0012-horizontal-scaling.md) | Horizontal Scaling Strategy | Accepted | 2025-08-11 |

## Creating a New ADR

To create a new ADR:

1. Copy the template from `template.md`
2. Name it `NNNN-brief-decision-title.md` (increment the number)
3. Fill in all sections
4. Update this README with the new ADR
5. Submit a PR for review

## ADR Lifecycle

1. **Proposed**: Initial state when ADR is created
2. **Accepted**: ADR has been reviewed and accepted
3. **Deprecated**: ADR is no longer relevant but kept for history
4. **Superseded**: ADR has been replaced by another ADR

## Why ADRs?

- **Documentation**: Provides context for future developers
- **Transparency**: Makes decision-making process visible
- **History**: Maintains record of architectural evolution
- **Review**: Enables peer review of important decisions
- **Onboarding**: Helps new team members understand the system

## References

- [Documenting Architecture Decisions](https://cognitect.com/blog/2011/11/15/documenting-architecture-decisions) by Michael Nygard
- [Architecture Decision Records](https://adr.github.io/)
- [MADR](https://adr.github.io/madr/) - Markdown Architectural Decision Records