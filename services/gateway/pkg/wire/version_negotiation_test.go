package wire

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNegotiateVersion(t *testing.T) {
	tests := createVersionNegotiationTests()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			version, err := NegotiateVersion(tt.clientMin, tt.clientMax, tt.serverMin, tt.serverMax)
			verifyVersionNegotiationResult(t, version, err, tt.expectedVer, tt.expectError, tt.errorContains)
		})
	}
}

func createVersionNegotiationTests() []versionNegotiationTest {
	return getVersionNegotiationTestData()
}

type versionNegotiationTest struct {
	name          string
	clientMin     uint16
	clientMax     uint16
	serverMin     uint16
	serverMax     uint16
	expectedVer   uint16
	expectError   bool
	errorContains string
}

func getVersionNegotiationTestData() []versionNegotiationTest {
	var tests []versionNegotiationTest

	tests = append(tests, getSuccessfulNegotiationTests()...)
	tests = append(tests, getFailedNegotiationTests()...)
	tests = append(tests, getEdgeCaseNegotiationTests()...)

	return tests
}

func getSuccessfulNegotiationTests() []versionNegotiationTest {
	return []versionNegotiationTest{
		{
			name:        "Perfect match - same version",
			clientMin:   1,
			clientMax:   1,
			serverMin:   1,
			serverMax:   1,
			expectedVer: 1,
			expectError: false,
		},
		{
			name:        "Client and server overlap - use highest",
			clientMin:   1,
			clientMax:   3,
			serverMin:   2,
			serverMax:   4,
			expectedVer: 3,
			expectError: false,
		},
		{
			name:        "Client supports higher versions",
			clientMin:   1,
			clientMax:   5,
			serverMin:   1,
			serverMax:   3,
			expectedVer: 3,
			expectError: false,
		},
		{
			name:        "Server supports higher versions",
			clientMin:   1,
			clientMax:   3,
			serverMin:   1,
			serverMax:   5,
			expectedVer: 3,
			expectError: false,
		},
		{
			name:        "Single version overlap",
			clientMin:   1,
			clientMax:   2,
			serverMin:   2,
			serverMax:   3,
			expectedVer: 2,
			expectError: false,
		},
	}
}

func getFailedNegotiationTests() []versionNegotiationTest {
	return []versionNegotiationTest{
		{
			name:          "No overlap - client too low",
			clientMin:     1,
			clientMax:     2,
			serverMin:     3,
			serverMax:     4,
			expectError:   true,
			errorContains: "no compatible protocol version",
		},
		{
			name:          "No overlap - server too low",
			clientMin:     3,
			clientMax:     4,
			serverMin:     1,
			serverMax:     2,
			expectError:   true,
			errorContains: "no compatible protocol version",
		},
		{
			name:          "No overlap - gap in the middle",
			clientMin:     1,
			clientMax:     2,
			serverMin:     4,
			serverMax:     5,
			expectError:   true,
			errorContains: "no compatible protocol version",
		},
	}
}

func getEdgeCaseNegotiationTests() []versionNegotiationTest {
	return []versionNegotiationTest{
		{
			name:        "Edge case - minimum versions match",
			clientMin:   2,
			clientMax:   2,
			serverMin:   2,
			serverMax:   5,
			expectedVer: 2,
			expectError: false,
		},
		{
			name:        "Edge case - maximum versions match",
			clientMin:   1,
			clientMax:   3,
			serverMin:   3,
			serverMax:   3,
			expectedVer: 3,
			expectError: false,
		},
		{
			name:        "Large version ranges",
			clientMin:   1,
			clientMax:   testIterations,
			serverMin:   testTimeout,
			serverMax:   150,
			expectedVer: testIterations,
			expectError: false,
		},
	}
}

func verifyVersionNegotiationResult(
	t *testing.T, version uint16, err error, expectedVer uint16, expectError bool, errorContains string,
) {
	t.Helper()

	if expectError {
		require.Error(t, err)

		if errorContains != "" {
			assert.Contains(t, err.Error(), errorContains)
		}

		assert.Equal(t, uint16(0), version)
	} else {
		require.NoError(t, err)
		assert.Equal(t, expectedVer, version)
	}
}

func TestIsVersionSupported(t *testing.T) {
	tests := []struct {
		name      string
		version   uint16
		supported bool
	}{
		{
			name:      "Current version",
			version:   CurrentVersion,
			supported: true,
		},
		{
			name:      "Minimum version",
			version:   MinVersion,
			supported: true,
		},
		{
			name:      "Maximum version",
			version:   MaxVersion,
			supported: true,
		},
		{
			name:      "Version too low",
			version:   MinVersion - 1,
			supported: false,
		},
		{
			name:      "Version too high",
			version:   MaxVersion + 1,
			supported: false,
		},
		{
			name:      "Zero version",
			version:   0,
			supported: false,
		},
		{
			name:      "Very high version",
			version:   65535,
			supported: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			supported := IsVersionSupported(tt.version)
			assert.Equal(t, tt.supported, supported)
		})
	}
}

func TestVersionNegotiationPayload(t *testing.T) {
	t.Run("Create version negotiation payload", func(t *testing.T) {
		payload := VersionNegotiationPayload{
			MinVersion: MinVersion,
			MaxVersion: MaxVersion,
			Preferred:  CurrentVersion,
			Supported:  []uint16{1, 2, 3},
		}

		// Test that all fields are set correctly
		assert.Equal(t, MinVersion, payload.MinVersion)
		assert.Equal(t, MaxVersion, payload.MaxVersion)
		assert.Equal(t, CurrentVersion, payload.Preferred)
		assert.Equal(t, []uint16{1, 2, 3}, payload.Supported)
	})

	t.Run("Empty supported versions", func(t *testing.T) {
		payload := VersionNegotiationPayload{
			MinVersion: 1,
			MaxVersion: 2,
			Preferred:  2,
			Supported:  []uint16{},
		}

		assert.Empty(t, payload.Supported)
		assert.NotNil(t, payload.Supported)
	})

	t.Run("Nil supported versions", func(t *testing.T) {
		payload := VersionNegotiationPayload{
			MinVersion: 1,
			MaxVersion: 1,
			Preferred:  1,
			Supported:  nil,
		}

		assert.Nil(t, payload.Supported)
	})
}

func TestVersionAckPayload(t *testing.T) {
	t.Run("Create version ack payload", func(t *testing.T) {
		payload := VersionAckPayload{
			AgreedVersion: CurrentVersion,
		}

		assert.Equal(t, CurrentVersion, payload.AgreedVersion)
	})

	t.Run("Different agreed version", func(t *testing.T) {
		payload := VersionAckPayload{
			AgreedVersion: 2,
		}

		assert.Equal(t, uint16(2), payload.AgreedVersion)
	})

	t.Run("Zero version", func(t *testing.T) {
		payload := VersionAckPayload{
			AgreedVersion: 0,
		}

		assert.Equal(t, uint16(0), payload.AgreedVersion)
	})
}

func TestVersionConstants(t *testing.T) {
	t.Run("Version constants are valid", func(t *testing.T) {
		// Test that version constants are sensible
		assert.Positive(t, CurrentVersion, "CurrentVersion should be greater than 0")
		assert.GreaterOrEqual(t, CurrentVersion, MinVersion, "CurrentVersion should be >= MinVersion")
		assert.LessOrEqual(t, CurrentVersion, MaxVersion, "CurrentVersion should be <= MaxVersion")
		assert.LessOrEqual(t, MinVersion, MaxVersion, "MinVersion should be <= MaxVersion")
	})

	t.Run("Magic bytes constant", func(t *testing.T) {
		// Test that magic bytes constant is valid
		assert.Equal(t, uint32(0x4D435042), MagicBytes, "Magic bytes should be 'MCPB'")
	})

	t.Run("Header size constant", func(t *testing.T) {
		// Test that header size is reasonable
		assert.Equal(t, 12, HeaderSize, "Header size should be 12 bytes")
		assert.Positive(t, HeaderSize, "Header size should be positive")
	})
}

func TestMessageTypeConstants(t *testing.T) {
	t.Run("Message type values", func(t *testing.T) {
		// Test that message types have expected values and are unique
		assert.Equal(t, MessageTypeRequest, MessageType(0x0001))
		assert.Equal(t, MessageTypeResponse, MessageType(0x0002))
		assert.Equal(t, MessageTypeControl, MessageType(0x0003))
		assert.Equal(t, MessageTypeHealthCheck, MessageType(0x0004))
		assert.Equal(t, MessageTypeError, MessageType(0x0005))
		assert.Equal(t, MessageTypeVersionNegotiation, MessageType(0x0006))
		assert.Equal(t, MessageTypeVersionAck, MessageType(0x0007))
	})

	t.Run("Message types are unique", func(t *testing.T) {
		messageTypes := []MessageType{
			MessageTypeRequest,
			MessageTypeResponse,
			MessageTypeControl,
			MessageTypeHealthCheck,
			MessageTypeError,
			MessageTypeVersionNegotiation,
			MessageTypeVersionAck,
		}

		// Check that all message types are unique
		seen := make(map[MessageType]bool)

		for _, msgType := range messageTypes {
			assert.False(t, seen[msgType], "Message type %d should be unique", msgType)
			seen[msgType] = true
		}
	})
}

func TestVersionNegotiationEdgeCases(t *testing.T) {
	t.Run("Same min and max versions", func(t *testing.T) {
		version, err := NegotiateVersion(2, 2, 2, 2)

		require.NoError(t, err)
		assert.Equal(t, uint16(2), version)
	})

	t.Run("Client has wider range", func(t *testing.T) {
		version, err := NegotiateVersion(1, 10, 5, 6)

		require.NoError(t, err)
		assert.Equal(t, uint16(6), version)
	})

	t.Run("Server has wider range", func(t *testing.T) {
		version, err := NegotiateVersion(5, 6, 1, 10)

		require.NoError(t, err)
		assert.Equal(t, uint16(6), version)
	})

	t.Run("Adjacent non-overlapping ranges", func(t *testing.T) {
		_, err := NegotiateVersion(1, 2, 3, 4)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "no compatible protocol version")
	})

	t.Run("Inverted ranges (min > max)", func(t *testing.T) {
		// This tests the algorithm behavior with invalid input
		_, err := NegotiateVersion(3, 1, 2, 4) // client min > client max
		assert.Error(t, err)
	})
}

// Benchmark version negotiation performance.
func BenchmarkNegotiateVersion(b *testing.B) {
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = NegotiateVersion(1, 5, 3, 7)
	}
}

func BenchmarkIsVersionSupported(b *testing.B) {
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = IsVersionSupported(CurrentVersion)
	}
}
