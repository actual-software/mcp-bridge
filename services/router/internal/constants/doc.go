// Package constants provides centralized timeout and configuration constants for the MCP router.
//
// This package eliminates magic numbers throughout the codebase by providing.
// well-documented constants with clear rationale for their values. All timeouts
// and durations are based on practical experience with network operations,
// user experience requirements, and system stability needs.
//
// # Design Principles
//
// The timeout values follow these principles:
//
//   - User-facing operations should feel responsive (≤5 seconds)
//   - Network operations should account for latency and processing (10-30 seconds)
//   - Background maintenance should not interfere with active operations
//   - Graceful shutdown should allow reasonable completion time without hanging
//   - Circuit breakers should fail fast to prevent cascade failures
//
// # Categories
//
// Constants are organized by functional area:
//
//   - HTTP Server Timeouts: For metrics and health endpoints
//   - Gateway Client Timeouts: For network operations and deadlines
//   - Connection Pool Timeouts: For resource management and lifecycle
//   - Circuit Breaker Timeouts: For fault tolerance patterns
//   - Rate Limiter Configuration: For traffic control and backpressure
//   - Connection Management: For retry logic and keepalive behavior
//   - Router Operation Timeouts: For core router functionality
//   - OAuth2 Token Management: For authentication token lifecycle
//
// # Usage Guidelines
//
// When using these constants:
//
//  1. Prefer constants over magic numbers in all production code
//  2. Use context.WithTimeout() for operations that may hang
//  3. Consider user experience impact when choosing timeout values
//  4. Document any deviations from standard constants with rationale
//  5. Test timeout behavior under load and network failure conditions
//
// # Maintenance
//
// These values should be reviewed when:
//
//   - Performance characteristics change significantly
//   - User feedback indicates timeout issues
//   - New deployment environments have different latency profiles
//   - Load testing reveals suboptimal timeout behavior
//
// Changes to these constants affect system behavior and should be tested.
// thoroughly before deployment.
package constants
