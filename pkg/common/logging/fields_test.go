package logging

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFieldConstants(t *testing.T) {
	t.Parallel()
	testServiceIdentificationFields(t)
	testConnectionAndNetworkFields(t)
	testRequestResponseFields(t)
	testSessionAndAuthFields(t)
	testErrorHandlingFields(t)
	testPoolAndResourceFields(t)
	testRateLimitingFields(t)
	testMetricsAndMonitoringFields(t)
	testDebuggingFields(t)
}

func testServiceIdentificationFields(t *testing.T) {
	t.Helper()
	assert.Equal(t, "service", FieldService)
	assert.Equal(t, "component", FieldComponent)
	assert.Equal(t, "version", FieldVersion)
}

func testConnectionAndNetworkFields(t *testing.T) {
	t.Helper()
	assert.Equal(t, "connection_id", FieldConnectionID)
	assert.Equal(t, "connection_type", FieldConnectionType)
	assert.Equal(t, "remote_addr", FieldRemoteAddr)
	assert.Equal(t, "local_addr", FieldLocalAddr)
	assert.Equal(t, "protocol", FieldProtocol)
	assert.Equal(t, "url", FieldURL)
	assert.Equal(t, "endpoint", FieldEndpoint)
	assert.Equal(t, "gateway", FieldGateway)
	assert.Equal(t, "backend", FieldBackend)
}

func testRequestResponseFields(t *testing.T) {
	t.Helper()
	assert.Equal(t, "request_id", FieldRequestID)
	assert.Equal(t, "correlation_id", FieldCorrelationID)
	assert.Equal(t, "trace_id", FieldTraceID)
	assert.Equal(t, "span_id", FieldSpanID)
	assert.Equal(t, "method", FieldMethod)
	assert.Equal(t, "namespace", FieldNamespace)
	assert.Equal(t, "status", FieldStatus)
	assert.Equal(t, "status_code", FieldStatusCode)
	assert.Equal(t, "duration_ms", FieldDuration)
	assert.Equal(t, "size_bytes", FieldSize)
	assert.Equal(t, "timeout_ms", FieldTimeout)
}

func testSessionAndAuthFields(t *testing.T) {
	t.Helper()
	assert.Equal(t, "session_id", FieldSessionID)
	assert.Equal(t, "user_id", FieldUserID)
	assert.Equal(t, "user", FieldUser)
	assert.Equal(t, "auth_type", FieldAuthType)
	assert.Equal(t, "client_id", FieldClientID)
}

func testErrorHandlingFields(t *testing.T) {
	t.Helper()
	assert.Equal(t, "error", FieldError)
	assert.Equal(t, "error_type", FieldErrorType)
	assert.Equal(t, "error_code", FieldErrorCode)
	assert.Equal(t, "retry_count", FieldRetryCount)
	assert.Equal(t, "attempt", FieldAttempt)
}

func testPoolAndResourceFields(t *testing.T) {
	t.Helper()
	assert.Equal(t, "pool_size", FieldPoolSize)
	assert.Equal(t, "pool_min", FieldPoolMin)
	assert.Equal(t, "pool_max", FieldPoolMax)
	assert.Equal(t, "active_connections", FieldActiveConns)
	assert.Equal(t, "idle_connections", FieldIdleConns)
	assert.Equal(t, "total_connections", FieldTotalConns)
}

func testRateLimitingFields(t *testing.T) {
	t.Helper()
	assert.Equal(t, "rate_limit", FieldRateLimit)
	assert.Equal(t, "rate_burst", FieldRateBurst)
	assert.Equal(t, "rate_exceeded", FieldRateExceeded)
	assert.Equal(t, "rate_remaining", FieldRateRemaining)
}

func testMetricsAndMonitoringFields(t *testing.T) {
	t.Helper()
	assert.Equal(t, "metric_name", FieldMetricName)
	assert.Equal(t, "metric_value", FieldMetricValue)
	assert.Equal(t, "healthy", FieldHealthy)
	assert.Equal(t, "state", FieldState)
}

func testDebuggingFields(t *testing.T) {
	t.Helper()
	assert.Equal(t, "trace", FieldTrace)
	assert.Equal(t, "debug", FieldDebug)
	assert.Equal(t, "call_stack", FieldCallStack)
}

func TestServiceNames(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "mcp-router", ServiceRouter)
	assert.Equal(t, "mcp-gateway", ServiceGateway)
}

func TestFieldConstantsUniqueness(t *testing.T) {
	t.Parallel()

	fields := getAllFieldConstants()
	fieldValues := getAllFieldValues()

	verifyFieldUniqueness(t, fieldValues)
	verifyFieldCount(t, fields, fieldValues)
}

func getAllFieldConstants() map[string]bool {
	return map[string]bool{
		FieldService:        true,
		FieldComponent:      true,
		FieldVersion:        true,
		FieldConnectionID:   true,
		FieldConnectionType: true,
		FieldRemoteAddr:     true,
		FieldLocalAddr:      true,
		FieldProtocol:       true,
		FieldURL:            true,
		FieldEndpoint:       true,
		FieldGateway:        true,
		FieldBackend:        true,
		FieldRequestID:      true,
		FieldCorrelationID:  true,
		FieldTraceID:        true,
		FieldSpanID:         true,
		FieldMethod:         true,
		FieldNamespace:      true,
		FieldStatus:         true,
		FieldStatusCode:     true,
		FieldDuration:       true,
		FieldSize:           true,
		FieldTimeout:        true,
		FieldSessionID:      true,
		FieldUserID:         true,
		FieldUser:           true,
		FieldAuthType:       true,
		FieldClientID:       true,
		FieldError:          true,
		FieldErrorType:      true,
		FieldErrorCode:      true,
		FieldRetryCount:     true,
		FieldAttempt:        true,
		FieldPoolSize:       true,
		FieldPoolMin:        true,
		FieldPoolMax:        true,
		FieldActiveConns:    true,
		FieldIdleConns:      true,
		FieldTotalConns:     true,
		FieldRateLimit:      true,
		FieldRateBurst:      true,
		FieldRateExceeded:   true,
		FieldRateRemaining:  true,
		FieldMetricName:     true,
		FieldMetricValue:    true,
		FieldHealthy:        true,
		FieldState:          true,
		FieldTrace:          true,
		FieldDebug:          true,
		FieldCallStack:      true,
	}
}

func getAllFieldValues() []string {
	return []string{
		FieldService,
		FieldComponent,
		FieldVersion,
		FieldConnectionID,
		FieldConnectionType,
		FieldRemoteAddr,
		FieldLocalAddr,
		FieldProtocol,
		FieldURL,
		FieldEndpoint,
		FieldGateway,
		FieldBackend,
		FieldRequestID,
		FieldCorrelationID,
		FieldTraceID,
		FieldSpanID,
		FieldMethod,
		FieldNamespace,
		FieldStatus,
		FieldStatusCode,
		FieldDuration,
		FieldSize,
		FieldTimeout,
		FieldSessionID,
		FieldUserID,
		FieldUser,
		FieldAuthType,
		FieldClientID,
		FieldError,
		FieldErrorType,
		FieldErrorCode,
		FieldRetryCount,
		FieldAttempt,
		FieldPoolSize,
		FieldPoolMin,
		FieldPoolMax,
		FieldActiveConns,
		FieldIdleConns,
		FieldTotalConns,
		FieldRateLimit,
		FieldRateBurst,
		FieldRateExceeded,
		FieldRateRemaining,
		FieldMetricName,
		FieldMetricValue,
		FieldHealthy,
		FieldState,
		FieldTrace,
		FieldDebug,
		FieldCallStack,
	}
}

func verifyFieldUniqueness(t *testing.T, fieldValues []string) {
	t.Helper()

	uniqueValues := make(map[string]bool)
	for _, value := range fieldValues {
		if uniqueValues[value] {
			t.Errorf("Duplicate field value found: %s", value)
		}

		uniqueValues[value] = true
	}
}

func verifyFieldCount(t *testing.T, fields map[string]bool, fieldValues []string) {
	t.Helper()

	uniqueValues := make(map[string]bool)
	for _, value := range fieldValues {
		uniqueValues[value] = true
	}

	expectedFieldCount := len(fields)
	actualFieldCount := len(uniqueValues)
	assert.Equal(t, expectedFieldCount, actualFieldCount, "Number of unique field constants should match expected count")
}

func TestServiceNamesUniqueness(t *testing.T) {
	t.Parallel()

	services := []string{ServiceRouter, ServiceGateway}
	uniqueServices := make(map[string]bool)

	for _, service := range services {
		if uniqueServices[service] {
			t.Errorf("Duplicate service name found: %s", service)
		}

		uniqueServices[service] = true
	}

	assert.Len(t, uniqueServices, len(services), "All service names should be unique")
}

func TestFieldNamingConventions(t *testing.T) {
	t.Parallel()

	fieldNames := getAllFieldValues()
	for _, fieldName := range fieldNames {
		validateFieldNaming(t, fieldName)
	}
}

func validateFieldNaming(t *testing.T, fieldName string) {
	t.Helper()
	// Check that field name is not empty
	assert.NotEmpty(t, fieldName, "Field name should not be empty")

	// Check that field name doesn't start or end with underscore
	assert.NotEqual(t, '_', fieldName[0], "Field name should not start with underscore: %s", fieldName)
	assert.NotEqual(t, '_', fieldName[len(fieldName)-1], "Field name should not end with underscore: %s", fieldName)

	// Check that field name is lowercase (snake_case convention)
	for i, char := range fieldName {
		if char >= 'A' && char <= 'Z' {
			t.Errorf("Field name should be lowercase (snake_case): %s at position %d", fieldName, i)
		}
	}
}

func TestServiceNamingConventions(t *testing.T) {
	t.Parallel()

	services := []string{ServiceRouter, ServiceGateway}

	for _, serviceName := range services {
		// Check that service name is not empty
		assert.NotEmpty(t, serviceName, "Service name should not be empty")

		// Check that service name follows kebab-case convention (lowercase with hyphens)
		for i, char := range serviceName {
			if char >= 'A' && char <= 'Z' {
				t.Errorf("Service name should be lowercase (kebab-case): %s at position %d", serviceName, i)
			}
		}

		// Check that service name starts with "mcp-"
		assert.Greater(t, len(serviceName), 4, "Service name should be longer than 'mcp-': %s", serviceName)

		if len(serviceName) > 4 {
			assert.Equal(t, "mcp-", serviceName[:4], "Service name should start with 'mcp-': %s", serviceName)
		}
	}
}

func TestFieldConstantsConsistency(t *testing.T) {
	t.Parallel()
	// Test that related field constants are consistent
	// For example, fields that represent the same concept should have consistent naming
	// Connection-related fields should all contain "connection" or related terms
	connectionFields := map[string]string{
		FieldConnectionID:   "connection_id",
		FieldConnectionType: "connection_type",
		FieldActiveConns:    "active_connections",
		FieldIdleConns:      "idle_connections",
		FieldTotalConns:     "total_connections",
	}

	for constant, expected := range connectionFields {
		assert.Equal(t, expected, constant)
	}

	// Error-related fields should all contain "error"
	errorFields := map[string]string{
		FieldError:     "error",
		FieldErrorType: "error_type",
		FieldErrorCode: "error_code",
	}

	for constant, expected := range errorFields {
		assert.Equal(t, expected, constant)
	}

	// Rate-related fields should all contain "rate"
	rateFields := map[string]string{
		FieldRateLimit:     "rate_limit",
		FieldRateBurst:     "rate_burst",
		FieldRateExceeded:  "rate_exceeded",
		FieldRateRemaining: "rate_remaining",
	}

	for constant, expected := range rateFields {
		assert.Equal(t, expected, constant)
	}
}
