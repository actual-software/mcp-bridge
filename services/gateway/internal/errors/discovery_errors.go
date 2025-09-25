package errors

import (
	"context"
	"net/http"
)

// discoveryContextKey is a type for discovery context keys.
type discoveryContextKey string

// Context keys for discovery-specific values.
const (
	discoveryContextKeyService   discoveryContextKey = "discovery.service"
	discoveryContextKeyNamespace discoveryContextKey = "discovery.namespace"
	discoveryContextKeyInstance  discoveryContextKey = "discovery.instance"
)

// Error codes for discovery operations.
const (
	ErrCodeServiceNotFound      = "DISCOVERY_SERVICE_NOT_FOUND"
	ErrCodeNoHealthyInstances   = "DISCOVERY_NO_HEALTHY_INSTANCES"
	ErrCodeRegistrationFailed   = "DISCOVERY_REGISTRATION_FAILED"
	ErrCodeDeregistrationFailed = "DISCOVERY_DEREGISTRATION_FAILED"
	ErrCodeHealthCheckFailed    = "DISCOVERY_HEALTH_CHECK_FAILED"
	ErrCodeWatchFailed          = "DISCOVERY_WATCH_FAILED"
	ErrCodeInvalidServiceConfig = "DISCOVERY_INVALID_SERVICE_CONFIG"
	ErrCodeProviderUnavailable  = "DISCOVERY_PROVIDER_UNAVAILABLE"
	ErrCodeInvalidEndpoint      = "DISCOVERY_INVALID_ENDPOINT"
)

// NewServiceNotFoundError creates an error for service not found.
func NewServiceNotFoundError(serviceName, namespace string) *GatewayError {
	return New(TypeNotFound, "service " + serviceName + " not found in namespace " + namespace).
		WithComponent("discovery").
		WithContext("service_name", serviceName).
		WithContext("namespace", namespace).
		WithContext("code", ErrCodeServiceNotFound).
		WithHTTPStatus(http.StatusNotFound)
}

// NewNoHealthyInstancesError creates an error when no healthy instances are available.
func NewNoHealthyInstancesError(serviceName, namespace string, totalInstances int) *GatewayError {
	return New(TypeUnavailable, "no healthy instances for service " + serviceName ).
		WithComponent("discovery").
		WithContext("service_name", serviceName).
		WithContext("namespace", namespace).
		WithContext("total_instances", totalInstances).
		WithContext("code", ErrCodeNoHealthyInstances).
		WithHTTPStatus(http.StatusServiceUnavailable)
}

// WrapRegistrationError wraps an error that occurred during service registration.
func WrapRegistrationError(ctx context.Context, err error, serviceName, instanceID string) *GatewayError {
	return WrapContext(ctx, err, "failed to register service").
		WithComponent("discovery").
		WithOperation("register").
		WithContext("service_name", serviceName).
		WithContext("instance_id", instanceID).
		WithContext("code", ErrCodeRegistrationFailed)
}

// WrapDeregistrationError wraps an error that occurred during service deregistration.
func WrapDeregistrationError(ctx context.Context, err error, serviceName, instanceID string) *GatewayError {
	return WrapContext(ctx, err, "failed to deregister service").
		WithComponent("discovery").
		WithOperation("deregister").
		WithContext("service_name", serviceName).
		WithContext("instance_id", instanceID).
		WithContext("code", ErrCodeDeregistrationFailed)
}

// WrapHealthCheckError wraps an error that occurred during health checking.
func WrapHealthCheckError(ctx context.Context, err error, serviceName, instanceID string) *GatewayError {
	return WrapContext(ctx, err, "health check failed").
		WithComponent("discovery").
		WithOperation("health_check").
		WithContext("service_name", serviceName).
		WithContext("instance_id", instanceID).
		WithContext("code", ErrCodeHealthCheckFailed)
}

// WrapWatchError wraps an error that occurred while watching for service changes.
func WrapWatchError(ctx context.Context, err error, serviceName string) *GatewayError {
	return WrapContext(ctx, err, "service watch failed").
		WithComponent("discovery").
		WithOperation("watch").
		WithContext("service_name", serviceName).
		WithContext("code", ErrCodeWatchFailed)
}

// NewInvalidServiceConfigError creates an error for invalid service configuration.
func NewInvalidServiceConfigError(reason string, serviceName string) *GatewayError {
	return New(TypeValidation, "invalid service configuration: " + reason ).
		WithComponent("discovery").
		WithContext("service_name", serviceName).
		WithContext("reason", reason).
		WithContext("code", ErrCodeInvalidServiceConfig).
		WithHTTPStatus(http.StatusBadRequest)
}

// NewProviderUnavailableError creates an error when the discovery provider is unavailable.
func NewProviderUnavailableError(provider string, reason string) *GatewayError {
	return New(TypeUnavailable, "discovery provider " + provider + " unavailable: " + reason ).
		WithComponent("discovery").
		WithContext("provider", provider).
		WithContext("reason", reason).
		WithContext("code", ErrCodeProviderUnavailable).
		WithHTTPStatus(http.StatusServiceUnavailable)
}

// NewInvalidEndpointError creates an error for invalid endpoint configuration.
func NewInvalidEndpointError(endpoint, reason string) *GatewayError {
	return New(TypeValidation, "invalid endpoint " + endpoint + ": " + reason ).
		WithComponent("discovery").
		WithContext("endpoint", endpoint).
		WithContext("reason", reason).
		WithContext("code", ErrCodeInvalidEndpoint).
		WithHTTPStatus(http.StatusBadRequest)
}

// EnrichContextWithService adds service discovery information to context for error tracking.
func EnrichContextWithService(ctx context.Context, serviceName, namespace, instanceID string) context.Context {
	if serviceName != "" {
		ctx = context.WithValue(ctx, discoveryContextKeyService, serviceName)
	}

	if namespace != "" {
		ctx = context.WithValue(ctx, discoveryContextKeyNamespace, namespace)
	}

	if instanceID != "" {
		ctx = context.WithValue(ctx, discoveryContextKeyInstance, instanceID)
	}

	return ctx
}
