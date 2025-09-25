# Circuit Breakers and Resilience

MCP Gateway implements circuit breaker patterns to provide resilience against service failures and cascading outages.

## Overview

Circuit breakers monitor external service calls and automatically "open" when failure rates exceed thresholds, preventing further calls to failing services while allowing fallback mechanisms to handle requests.

## Redis Circuit Breaker

The primary circuit breaker implementation protects Redis operations used for:
- Rate limiting storage
- Session management  
- Distributed state coordination

### States

1. **Closed** (Normal Operation)
   - All requests pass through to Redis
   - Failure count is tracked
   - When failure threshold is reached, circuit opens

2. **Open** (Circuit Breaker Active)
   - No requests sent to Redis
   - All operations use fallback mechanisms
   - After timeout period, transitions to Half-Open

3. **Half-Open** (Testing Recovery)
   - Limited requests allowed to test Redis health
   - If requests succeed, circuit closes
   - If requests fail, circuit reopens

### Configuration

```yaml
circuit_breaker:
  enabled: true
  threshold: 5           # Failures before opening circuit
  timeout: 30            # Seconds before allowing retry
  max_requests: 1        # Max requests in half-open state
  interval: 60           # Failure counting window (seconds)
  success_ratio: 0.6     # Success ratio to close circuit
```

### Fallback Mechanisms

When the Redis circuit breaker opens:

#### Rate Limiting Fallback
- **Primary**: Redis-backed sliding window rate limiter
- **Fallback**: In-memory rate limiter with configurable burst window
- **Behavior**: Seamless transition, rate limits still enforced

#### Session Management Fallback
- **Primary**: Redis-backed persistent sessions
- **Fallback**: In-memory sessions (not persistent across restarts)
- **Behavior**: Sessions continue to work but are not shared across instances

## Metrics and Monitoring

### Circuit Breaker Metrics

```
mcp_gateway_circuit_breaker_state{endpoint="redis"}
```
- **0**: Closed (normal operation)
- **1**: Open (circuit breaker active)
- **2**: Half-Open (testing recovery)

### Rate Limiting Metrics

```
mcp_gateway_rate_limit_fallback_total{reason="circuit_open"}
```
Tracks when rate limiting falls back to in-memory due to circuit breaker.

### Health Monitoring

Circuit breaker health is included in health check endpoints:
- `/healthz`: Basic health (always returns OK if service is running)
- `/ready`: Readiness check (considers circuit breaker state)
- `/health`: Detailed health including circuit breaker states

## Implementation Details

### Redis Rate Limiter with Circuit Breaker

```go
type RedisRateLimiterWithCircuitBreaker struct {
    limiter        *RedisRateLimiter
    circuitBreaker *circuit.CircuitBreaker
    fallback       RateLimiter // In-memory fallback
}

func (r *RedisRateLimiterWithCircuitBreaker) Allow(ctx context.Context, key string, config auth.RateLimitConfig) (bool, error) {
    // Check circuit breaker state
    if r.circuitBreaker.IsOpen() {
        return r.fallback.Allow(ctx, key, config)
    }

    // Try Redis with circuit breaker protection
    var allowed bool
    var err error
    
    cbErr := r.circuitBreaker.Call(func() error {
        allowed, err = r.limiter.Allow(ctx, key, config)
        return err
    })
    
    if cbErr != nil {
        // Circuit opened or Redis failed, use fallback
        return r.fallback.Allow(ctx, key, config)
    }
    
    return allowed, nil
}
```

### Circuit Breaker Configuration

Default circuit breaker settings:
- **Failure Threshold**: 5 consecutive failures
- **Success Threshold**: 3 consecutive successes to close
- **Timeout**: 30 seconds before allowing retry
- **Max Requests**: 1 request allowed in half-open state

## Best Practices

### Monitoring
- Monitor circuit breaker state changes
- Set up alerts for circuit breaker openings
- Track fallback usage metrics

### Configuration Tuning
- Set failure threshold based on expected Redis reliability
- Configure timeout based on Redis recovery time
- Adjust success threshold for stability

### Testing
- Test circuit breaker behavior under Redis failures
- Verify fallback mechanisms work correctly
- Load test with circuit breaker scenarios

## Troubleshooting

### Circuit Breaker Won't Close
- Check Redis connectivity and health
- Verify success threshold configuration
- Review Redis error logs

### High Fallback Usage
- Monitor Redis performance and errors
- Check network connectivity to Redis
- Consider Redis scaling or clustering

### Rate Limiting Inconsistency
- Expected when circuit breaker is open
- In-memory fallback is per-instance, not shared
- Consider Redis clustering for high availability

## Future Enhancements

- Per-endpoint circuit breakers for upstream services
- Adaptive thresholds based on traffic patterns
- Circuit breaker dashboard and visualization
- Integration with service mesh circuit breakers