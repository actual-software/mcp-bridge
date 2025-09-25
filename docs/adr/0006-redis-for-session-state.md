# ADR-0006: Redis for Distributed Session State

**Status**: Accepted

**Date**: 2025-08-06

**Authors**: @poiley

## Context

The Gateway component needs to maintain session state for connected clients, including authentication tokens, rate limiting counters, and connection metadata. In a horizontally scaled deployment, this state must be shared across multiple Gateway instances.

### Requirements

- Share session state across multiple Gateway instances
- Fast read/write performance (sub-millisecond)
- Support for TTL/expiration
- Atomic operations for rate limiting
- High availability
- Support for different data structures

### Constraints

- Must support horizontal scaling
- Cannot lose session data on Gateway restart
- Need to handle network partitions gracefully
- Must support thousands of concurrent sessions

## Decision

Use Redis as the distributed session store for the Gateway, with local caching for performance optimization.

### Implementation Details

```go
// Session storage structure
type Session struct {
    ID           string
    ClientID     string
    AuthToken    string
    CreatedAt    time.Time
    LastActivity time.Time
    Metadata     map[string]string
}

// Redis data model
// Sessions: HASH "session:{id}" -> session data
// Rate limits: STRING "rate:{clientId}:{window}" -> count (with TTL)
// Active connections: SET "connections:{gateway}" -> session IDs
// Connection mapping: HASH "client:{clientId}" -> gateway instance
```

## Consequences

### Positive

- **Performance**: In-memory storage with sub-millisecond latency
- **Scalability**: Proven to handle millions of operations per second
- **Features**: Rich data structures (strings, hashes, sets, sorted sets)
- **TTL support**: Automatic expiration of sessions and rate limit windows
- **Persistence**: Optional persistence with RDB/AOF
- **Ecosystem**: Mature with excellent client libraries
- **Monitoring**: Good observability with INFO commands and metrics

### Negative

- **Additional dependency**: Requires Redis deployment and maintenance
- **Memory cost**: All data must fit in memory
- **Network calls**: Adds network latency vs local storage
- **Single point of failure**: Without clustering/replication

### Neutral

- Requires Redis Cluster or Sentinel for HA
- Memory usage needs capacity planning
- Backup strategy needed for persistence

## Alternatives Considered

### Alternative 1: In-Memory with Gossip Protocol

**Description**: Each Gateway maintains local state and syncs via gossip

**Pros**:
- No external dependencies
- Very low latency
- Resilient to network partitions

**Cons**:
- Complex implementation
- Eventual consistency issues
- Difficult to debug
- Memory usage multiplied by instance count

**Reason for rejection**: Complexity and consistency challenges outweigh benefits.

### Alternative 2: PostgreSQL/MySQL

**Description**: Use relational database for session storage

**Pros**:
- ACID guarantees
- Complex queries possible
- Existing database infrastructure
- Persistent by default

**Cons**:
- Higher latency (disk I/O)
- Connection pool limits
- Not optimized for session workload
- Complex caching layer needed

**Reason for rejection**: Performance not suitable for high-frequency session operations.

### Alternative 3: Hazelcast/Ignite

**Description**: In-memory data grid solution

**Pros**:
- Distributed by design
- Rich features
- Near-cache support
- SQL query support

**Cons**:
- More complex than Redis
- Larger memory footprint
- Steeper learning curve
- JVM-based (if using Hazelcast)

**Reason for rejection**: Added complexity not justified for our use case.

### Alternative 4: etcd

**Description**: Distributed key-value store used by Kubernetes

**Pros**:
- Strong consistency
- Watch support for changes
- Already present in K8s clusters
- Built-in leader election

**Cons**:
- Not optimized for high-frequency writes
- Limited data structures
- Size limitations
- Performance limitations at scale

**Reason for rejection**: Performance characteristics not suitable for session storage.

## References

- [Redis Documentation](https://redis.io/documentation)
- [Redis Persistence](https://redis.io/docs/manual/persistence/)
- [Redis Cluster Specification](https://redis.io/docs/reference/cluster-spec/)
- [Redis Best Practices](https://redis.com/redis-best-practices/)

## Notes

Implementation considerations:
- Use Redis Cluster for production deployments
- Implement circuit breaker for Redis failures
- Consider Redis Streams for event sourcing
- Monitor memory usage and set appropriate eviction policies
- Use pipelining for batch operations
- Implement local caching with short TTL for hot data