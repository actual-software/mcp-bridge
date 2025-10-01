package validation

import (
	"regexp"
	"strings"
	"testing"
)

const (
	testIterations          = 100
	httpStatusInternalError = 500
)

func TestValidateNamespace(t *testing.T) {
	tests := createNamespaceValidationTests()
	runNamespaceValidationTests(t, tests)
}

func createNamespaceValidationTests() []struct {
	name      string
	namespace string
	wantErr   bool
	errMsg    string
} {
	basicTests := createBasicNamespaceTests()
	errorTests := createNamespaceErrorTests()
	edgeCaseTests := createNamespaceEdgeCaseTests()

	tests := make([]struct {
		name      string
		namespace string
		wantErr   bool
		errMsg    string
	}, 0, len(basicTests)+len(errorTests)+len(edgeCaseTests))

	tests = append(tests, basicTests...)
	tests = append(tests, errorTests...)
	tests = append(tests, edgeCaseTests...)

	return tests
}

func createBasicNamespaceTests() []struct {
	name      string
	namespace string
	wantErr   bool
	errMsg    string
} {
	return []struct {
		name      string
		namespace string
		wantErr   bool
		errMsg    string
	}{
		{
			name:      "valid namespace",
			namespace: "test-namespace",
			wantErr:   false,
		},
		{
			name:      "valid namespace with dots",
			namespace: "test.namespace.v1",
			wantErr:   false,
		},
		{
			name:      "valid namespace with underscores",
			namespace: "test_namespace_v1",
			wantErr:   false,
		},
	}
}

func createNamespaceErrorTests() []struct {
	name      string
	namespace string
	wantErr   bool
	errMsg    string
} {
	return []struct {
		name      string
		namespace string
		wantErr   bool
		errMsg    string
	}{
		{
			name:      "empty namespace",
			namespace: "",
			wantErr:   true,
			errMsg:    "namespace cannot be empty",
		},
		{
			name:      "namespace too long",
			namespace: strings.Repeat("a", MaxNamespaceLength+1),
			wantErr:   true,
			errMsg:    "namespace too long",
		},
		{
			name:      "namespace with invalid characters",
			namespace: "test-namespace!@#",
			wantErr:   true,
			errMsg:    "namespace contains invalid characters",
		},
		{
			name:      "namespace with spaces",
			namespace: "test namespace",
			wantErr:   true,
			errMsg:    "namespace contains invalid characters",
		},
	}
}

func createNamespaceEdgeCaseTests() []struct {
	name      string
	namespace string
	wantErr   bool
	errMsg    string
} {
	return []struct {
		name      string
		namespace string
		wantErr   bool
		errMsg    string
	}{
		{
			name:      "namespace with directory traversal",
			namespace: "../test",
			wantErr:   true,
			errMsg:    "namespace contains invalid characters",
		},
		{
			name:      "namespace with null byte",
			namespace: "test\\x00namespace",
			wantErr:   true,
			errMsg:    "namespace contains invalid characters",
		},
		{
			name:      "single character namespace",
			namespace: "a",
			wantErr:   false,
		},
		{
			name:      "namespace at max length",
			namespace: strings.Repeat("a", MaxNamespaceLength),
			wantErr:   false,
		},
		{
			name:      "non-UTF8 namespace",
			namespace: string([]byte{0xFF, 0xFE, 0xFD}),
			wantErr:   true,
			errMsg:    "namespace contains invalid UTF-8",
		},
	}
}

func runNamespaceValidationTests(t *testing.T, tests []struct {
	name      string
	namespace string
	wantErr   bool
	errMsg    string
}) {
	t.Helper()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateNamespace(tt.namespace)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateNamespace() error = %v, wantErr %v", err, tt.wantErr)
			}

			if err != nil && tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("ValidateNamespace() error = %v, want error containing %v", err, tt.errMsg)
			}
		})
	}
}

func TestValidateMethod(t *testing.T) {
	tests := createMethodValidationTests()
	runMethodValidationTests(t, tests)
}

func createMethodValidationTests() []struct {
	name    string
	method  string
	wantErr bool
	errMsg  string
} {
	basicTests := createBasicMethodTests()
	errorTests := createMethodErrorTests()
	edgeCaseTests := createMethodEdgeCaseTests()

	tests := make([]struct {
		name    string
		method  string
		wantErr bool
		errMsg  string
	}, 0, len(basicTests)+len(errorTests)+len(edgeCaseTests))

	tests = append(tests, basicTests...)
	tests = append(tests, errorTests...)
	tests = append(tests, edgeCaseTests...)

	return tests
}

func createBasicMethodTests() []struct {
	name    string
	method  string
	wantErr bool
	errMsg  string
} {
	return []struct {
		name    string
		method  string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid method",
			method:  "tools/list",
			wantErr: false,
		},
		{
			name:    "valid method with dots",
			method:  "resources.list",
			wantErr: false,
		},
		{
			name:    "valid method with hyphens",
			method:  "server-info/get",
			wantErr: false,
		},
		{
			name:    "valid method with underscores",
			method:  "get_server_info",
			wantErr: false,
		},
	}
}

func createMethodErrorTests() []struct {
	name    string
	method  string
	wantErr bool
	errMsg  string
} {
	return []struct {
		name    string
		method  string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "empty method",
			method:  "",
			wantErr: true,
			errMsg:  "method cannot be empty",
		},
		{
			name:    "method too long",
			method:  strings.Repeat("a", MaxMethodLength+1),
			wantErr: true,
			errMsg:  "method too long",
		},
		{
			name:    "method with invalid characters",
			method:  "tools/list!@#",
			wantErr: true,
			errMsg:  "method contains invalid characters",
		},
		{
			name:    "method with spaces",
			method:  "tools list",
			wantErr: true,
			errMsg:  "method contains invalid characters",
		},
	}
}

func createMethodEdgeCaseTests() []struct {
	name    string
	method  string
	wantErr bool
	errMsg  string
} {
	return []struct {
		name    string
		method  string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "method with directory traversal",
			method:  "../../../etc/passwd",
			wantErr: true,
			errMsg:  "method contains directory traversal",
		},
		{
			name:    "method with null byte",
			method:  "tools\\x00list",
			wantErr: true,
			errMsg:  "method contains invalid characters",
		},
		{
			name:    "method at max length",
			method:  strings.Repeat("a", MaxMethodLength),
			wantErr: false,
		},
		{
			name:    "non-UTF8 method",
			method:  string([]byte{0xFF, 0xFE, 0xFD}),
			wantErr: true,
			errMsg:  "method contains invalid UTF-8",
		},
	}
}

func runMethodValidationTests(t *testing.T, tests []struct {
	name    string
	method  string
	wantErr bool
	errMsg  string
}) {
	t.Helper()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMethod(tt.method)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateMethod() error = %v, wantErr %v", err, tt.wantErr)
			}

			if err != nil && tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("ValidateMethod() error = %v, want error containing %v", err, tt.errMsg)
			}
		})
	}
}

func TestValidateRequestID(t *testing.T) {
	tests := createRequestIDValidationTests()
	runRequestIDValidationTests(t, tests)
}

func createRequestIDValidationTests() []struct {
	name    string
	id      interface{}
	wantErr bool
	errMsg  string
} {
	validTests := createValidRequestIDTests()
	errorTests := createRequestIDErrorTests()

	tests := make([]struct {
		name    string
		id      interface{}
		wantErr bool
		errMsg  string
	}, 0, len(validTests)+len(errorTests))

	tests = append(tests, validTests...)
	tests = append(tests, errorTests...)

	return tests
}

func createValidRequestIDTests() []struct {
	name    string
	id      interface{}
	wantErr bool
	errMsg  string
} {
	return []struct {
		name    string
		id      interface{}
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid string ID",
			id:      "req-12345",
			wantErr: false,
		},
		{
			name:    "valid UUID",
			id:      "550e8400-e29b-41d4-a716-446655440000",
			wantErr: false,
		},
		{
			name:    "valid numeric ID",
			id:      12345,
			wantErr: false,
		},
		{
			name:    "valid float64 ID",
			id:      12345.0,
			wantErr: false,
		},
		{
			name:    "valid int64 ID",
			id:      int64(12345),
			wantErr: false,
		},
		{
			name:    "ID at max length",
			id:      strings.Repeat("a", MaxIDLength),
			wantErr: false,
		},
	}
}

func createRequestIDErrorTests() []struct {
	name    string
	id      interface{}
	wantErr bool
	errMsg  string
} {
	return []struct {
		name    string
		id      interface{}
		wantErr bool
		errMsg  string
	}{
		{
			name:    "empty string ID",
			id:      "",
			wantErr: true,
			errMsg:  "ID cannot be empty",
		},
		{
			name:    "ID too long",
			id:      strings.Repeat("a", MaxIDLength+1),
			wantErr: true,
			errMsg:  "ID too long",
		},
		{
			name:    "invalid ID type",
			id:      true,
			wantErr: true,
			errMsg:  "invalid ID type",
		},
		{
			name:    "non-UTF8 ID",
			id:      string([]byte{0xFF, 0xFE}),
			wantErr: true,
			errMsg:  "ID contains invalid UTF-8",
		},
	}
}

func runRequestIDValidationTests(t *testing.T, tests []struct {
	name    string
	id      interface{}
	wantErr bool
	errMsg  string
}) {
	t.Helper()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRequestID(tt.id)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateRequestID() error = %v, wantErr %v", err, tt.wantErr)
			}

			if err != nil && tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("ValidateRequestID() error = %v, want error containing %v", err, tt.errMsg)
			}
		})
	}
}

func TestValidateToken(t *testing.T) {
	tests := createTokenValidationTests()
	runTokenValidationTests(t, tests)
}

func createTokenValidationTests() []struct {
	name    string
	token   string
	wantErr bool
	errMsg  string
} {
	validTests := createValidTokenTests()
	errorTests := createTokenErrorTests()

	tests := make([]struct {
		name    string
		token   string
		wantErr bool
		errMsg  string
	}, 0, len(validTests)+len(errorTests))

	tests = append(tests, validTests...)
	tests = append(tests, errorTests...)

	return tests
}

func createValidTokenTests() []struct {
	name    string
	token   string
	wantErr bool
	errMsg  string
} {
	return []struct {
		name    string
		token   string
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid JWT token",
			token: "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0Ijo" +
				"xNTE2MjM5MDIyfQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c",
			wantErr: false,
		},
		{
			name:    "valid bearer token",
			token:   "sk-1234567890abcdefghijklmnopqrstuvwxyz",
			wantErr: false,
		},
		{
			name:    "valid base64 token",
			token:   "YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXo=",
			wantErr: false,
		},
	}
}

func createTokenErrorTests() []struct {
	name    string
	token   string
	wantErr bool
	errMsg  string
} {
	return []struct {
		name    string
		token   string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "empty token",
			token:   "",
			wantErr: true,
			errMsg:  "token cannot be empty",
		},
		{
			name:    "token too long",
			token:   strings.Repeat("a", MaxTokenLength+1),
			wantErr: true,
			errMsg:  "token too long",
		},
		{
			name:    "token with null byte",
			token:   "token\x00value",
			wantErr: true,
			errMsg:  "token contains invalid characters",
		},
		{
			name:    "token with newline",
			token:   "token\nvalue",
			wantErr: true,
			errMsg:  "token contains invalid characters",
		},
		{
			name:    "token with carriage return",
			token:   "token\rvalue",
			wantErr: true,
			errMsg:  "token contains invalid characters",
		},
		{
			name:    "non-UTF8 token",
			token:   string([]byte{0xFF, 0xFE, 0xFD}),
			wantErr: true,
			errMsg:  "token contains invalid UTF-8",
		},
	}
}

func runTokenValidationTests(t *testing.T, tests []struct {
	name    string
	token   string
	wantErr bool
	errMsg  string
}) {
	t.Helper()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateToken(tt.token)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateToken() error = %v, wantErr %v", err, tt.wantErr)
			}

			if err != nil && tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("ValidateToken() error = %v, want error containing %v", err, tt.errMsg)
			}
		})
	}
}

func TestValidateURL(t *testing.T) {
	tests := createURLValidationTests()
	runURLValidationTests(t, tests)
}

func createURLValidationTests() []struct {
	name    string
	url     string
	wantErr bool
	errMsg  string
} {
	validTests := createValidURLTests()
	errorTests := createURLErrorTests()

	tests := make([]struct {
		name    string
		url     string
		wantErr bool
		errMsg  string
	}, 0, len(validTests)+len(errorTests))

	tests = append(tests, validTests...)
	tests = append(tests, errorTests...)

	return tests
}

func createValidURLTests() []struct {
	name    string
	url     string
	wantErr bool
	errMsg  string
} {
	return []struct {
		name    string
		url     string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid HTTP URL",
			url:     "http://example.com",
			wantErr: false,
		},
		{
			name:    "valid HTTPS URL",
			url:     "https://example.com/path/to/resource",
			wantErr: false,
		},
		{
			name:    "valid WebSocket URL",
			url:     "ws://example.com:8080/ws",
			wantErr: false,
		},
		{
			name:    "valid secure WebSocket URL",
			url:     "wss://example.com/ws",
			wantErr: false,
		},
		{
			name:    "valid TCP URL",
			url:     "tcp://example.com:9000",
			wantErr: false,
		},
		{
			name:    "valid secure TCP URL",
			url:     "tcps://example.com:9443",
			wantErr: false,
		},
		{
			name:    "localhost URL",
			url:     "http://localhost:8080",
			wantErr: false,
		},
		{
			name:    "IP address URL",
			url:     "https://192.168.1.1:443/api",
			wantErr: false,
		},
	}
}

func createURLErrorTests() []struct {
	name    string
	url     string
	wantErr bool
	errMsg  string
} {
	return []struct {
		name    string
		url     string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "empty URL",
			url:     "",
			wantErr: true,
			errMsg:  "URL cannot be empty",
		},
		{
			name:    "URL too long",
			url:     "https://example.com/" + strings.Repeat("a", MaxURLLength),
			wantErr: true,
			errMsg:  "URL too long",
		},
		{
			name:    "invalid URL scheme",
			url:     "ftp://example.com",
			wantErr: true,
			errMsg:  "invalid URL scheme",
		},
		{
			name:    "URL without scheme",
			url:     "example.com",
			wantErr: true,
			errMsg:  "invalid URL scheme",
		},
		{
			name:    "URL without host",
			url:     "https://",
			wantErr: true,
			errMsg:  "URL missing host",
		},
		{
			name:    "non-UTF8 URL",
			url:     "https://example.com/" + string([]byte{0xFF, 0xFE}),
			wantErr: true,
			errMsg:  "URL contains invalid UTF-8",
		},
	}
}

func runURLValidationTests(t *testing.T, tests []struct {
	name    string
	url     string
	wantErr bool
	errMsg  string
}) {
	t.Helper()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateURL() error = %v, wantErr %v", err, tt.wantErr)
			}

			if err != nil && tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("ValidateURL() error = %v, want error containing %v", err, tt.errMsg)
			}
		})
	}
}

func TestValidateIP(t *testing.T) {
	tests := []struct {
		name    string
		ip      string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid IPv4",
			ip:      "192.168.1.1",
			wantErr: false,
		},
		{
			name:    "valid IPv6",
			ip:      "2001:0db8:85a3:0000:0000:8a2e:0370:7334",
			wantErr: false,
		},
		{
			name:    "valid IPv6 short form",
			ip:      "::1",
			wantErr: false,
		},
		{
			name:    "empty IP",
			ip:      "",
			wantErr: true,
			errMsg:  "IP cannot be empty",
		},
		{
			name:    "invalid IP",
			ip:      "256.256.256.256",
			wantErr: true,
			errMsg:  "invalid IP address",
		},
		{
			name:    "hostname instead of IP",
			ip:      "example.com",
			wantErr: true,
			errMsg:  "invalid IP address",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateIP(tt.ip)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateIP() error = %v, wantErr %v", err, tt.wantErr)
			}

			if err != nil && tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("ValidateIP() error = %v, want error containing %v", err, tt.errMsg)
			}
		})
	}
}

func TestIsValidHostname(t *testing.T) {
	tests := createHostnameValidationTests()
	runHostnameValidationTests(t, tests)
}

func createHostnameValidationTests() []struct {
	name     string
	hostname string
	want     bool
} {
	validTests := createValidHostnameTests()
	invalidTests := createInvalidHostnameTests()

	tests := make([]struct {
		name     string
		hostname string
		want     bool
	}, 0, len(validTests)+len(invalidTests))

	tests = append(tests, validTests...)
	tests = append(tests, invalidTests...)

	return tests
}

func createValidHostnameTests() []struct {
	name     string
	hostname string
	want     bool
} {
	return []struct {
		name     string
		hostname string
		want     bool
	}{
		{
			name:     "valid hostname",
			hostname: "example.com",
			want:     true,
		},
		{
			name:     "valid subdomain",
			hostname: "api.example.com",
			want:     true,
		},
		{
			name:     "valid with numbers",
			hostname: "api2.example.com",
			want:     true,
		},
		{
			name:     "valid with hyphens",
			hostname: "api-server.example.com",
			want:     true,
		},
		{
			name:     "localhost",
			hostname: "localhost",
			want:     true,
		},
	}
}

func createInvalidHostnameTests() []struct {
	name     string
	hostname string
	want     bool
} {
	return []struct {
		name     string
		hostname string
		want     bool
	}{
		{
			name:     "empty hostname",
			hostname: "",
			want:     false,
		},
		{
			name:     "hostname too long",
			hostname: strings.Repeat("a", 256),
			want:     false,
		},
		{
			name:     "label too long",
			hostname: strings.Repeat("a", 64) + ".com",
			want:     false,
		},
		{
			name:     "starts with hyphen",
			hostname: "-example.com",
			want:     false,
		},
		{
			name:     "ends with hyphen",
			hostname: "example-.com",
			want:     false,
		},
		{
			name:     "contains underscore",
			hostname: "exam_ple.com",
			want:     false,
		},
		{
			name:     "contains special char",
			hostname: "exam!ple.com",
			want:     false,
		},
	}
}

func runHostnameValidationTests(t *testing.T, tests []struct {
	name     string
	hostname string
	want     bool
}) {
	t.Helper()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isValidHostname(tt.hostname); got != tt.want {
				t.Errorf("isValidHostname() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSanitizeString(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "clean string",
			input: "hello world",
			want:  "hello world",
		},
		{
			name:  "string with null bytes",
			input: "hello\x00world",
			want:  "helloworld",
		},
		{
			name:  "string with newlines",
			input: "hello\nworld",
			want:  "hello world",
		},
		{
			name:  "string with carriage returns",
			input: "hello\rworld",
			want:  "hello world",
		},
		{
			name:  "string with whitespace",
			input: "  hello world  ",
			want:  "hello world",
		},
		{
			name:  "string with multiple issues",
			input: "  hello\x00\n\rworld  ",
			want:  "hello  world",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SanitizeString(tt.input); got != tt.want {
				t.Errorf("SanitizeString() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidationConstants(t *testing.T) {
	// Test that constants have reasonable values
	validateConstantRanges(t)
}

func validateConstantRanges(t *testing.T) {
	t.Helper()

	tests := []struct {
		name  string
		value int
		min   int
		max   int
	}{
		{"MaxNamespaceLength", MaxNamespaceLength, 1, httpStatusInternalError},
		{"MaxMethodLength", MaxMethodLength, 1, httpStatusInternalError},
		{"MaxIDLength", MaxIDLength, 1, httpStatusInternalError},
		{"MaxTokenLength", MaxTokenLength, testIterations, 10000},
		{"MaxURLLength", MaxURLLength, testIterations, 10000},
	}

	for _, tt := range tests {
		if tt.value < tt.min || tt.value > tt.max {
			t.Errorf("%s has unreasonable value: %d (expected between %d and %d)",
				tt.name, tt.value, tt.min, tt.max)
		}
	}
}

func TestValidationPatterns(t *testing.T) {
	// Test that regex patterns work correctly
	testCases := []struct {
		pattern *regexp.Regexp
		valid   []string
		invalid []string
	}{
		{
			pattern: regexp.MustCompile(`^[a-zA-Z0-9._-]+$`),
			valid:   []string{"test", "test-namespace", "test.namespace", "test_namespace", "test123"},
			invalid: []string{"test namespace", "test@namespace", "test!namespace"},
		},
		{
			pattern: regexp.MustCompile(`^[a-zA-Z0-9._/-]+$`),
			valid:   []string{"test", "test/method", "test.method", "test-method", "test_method", "test/sub/method"},
			invalid: []string{"test method", "test@method", "test!method"},
		},
		{
			pattern: regexp.MustCompile(`^[a-zA-Z0-9_-]+$`),
			valid:   []string{"test", "test-id", "test_id", "test123", "550e8400-e29b-41d4-a716-446655440000"},
			invalid: []string{"test id", "test@id", "test!id", "test.id", "test/id"},
		},
	}

	for _, tc := range testCases {
		for _, valid := range tc.valid {
			if !tc.pattern.MatchString(valid) {
				t.Errorf("Pattern %v should match %q but doesn't", tc.pattern, valid)
			}
		}

		for _, invalid := range tc.invalid {
			if tc.pattern.MatchString(invalid) {
				t.Errorf("Pattern %v should not match %q but does", tc.pattern, invalid)
			}
		}
	}
}

// Benchmark tests to ensure validation performance.
func BenchmarkValidateNamespace(b *testing.B) {
	namespace := "test-namespace-123"
	for i := 0; i < b.N; i++ {
		_ = ValidateNamespace(namespace)
	}
}

func BenchmarkValidateMethod(b *testing.B) {
	method := "tools/list"
	for i := 0; i < b.N; i++ {
		_ = ValidateMethod(method)
	}
}

func BenchmarkValidateRequestID(b *testing.B) {
	id := "req-12345-abcdef"
	for i := 0; i < b.N; i++ {
		_ = ValidateRequestID(id)
	}
}

func BenchmarkValidateToken(b *testing.B) {
	const testJWT = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0Ijo" +
		"xNTE2MjM5MDIyfQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"

	token := testJWT
	for i := 0; i < b.N; i++ {
		_ = ValidateToken(token)
	}
}

func BenchmarkValidateURL(b *testing.B) {
	url := "https://example.com/api/v1/resources?param=value"
	for i := 0; i < b.N; i++ {
		_ = ValidateURL(url)
	}
}
