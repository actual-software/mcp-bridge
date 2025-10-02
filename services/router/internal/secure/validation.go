package secure

import (
	"fmt"
	"strings"
)

// sanitizeTokenInput validates input before passing to token store operations.
// It rejects inputs that are known to cause issues with credential storage systems.
func sanitizeTokenInput(s string) error {
	// Reject null bytes - many storage systems cannot handle them
	if strings.Contains(s, "\x00") {
		return fmt.Errorf("input contains null bytes which are not supported")
	}

	// Reject excessively large inputs - storage systems have size limits
	const maxInputSize = 256 * 1024 // 256KB limit
	if len(s) > maxInputSize {
		return fmt.Errorf("input size (%d bytes) exceeds maximum supported size (%d bytes)", len(s), maxInputSize)
	}

	// Reject excessive binary data that may cause encoding issues
	nonPrintable := 0
	for _, r := range s {
		if r < 32 || r > 126 {
			nonPrintable++
		}
	}
	if len(s) > 0 && float64(nonPrintable)/float64(len(s)) > 0.3 {
		return fmt.Errorf("input contains excessive binary data (>30%% non-printable) which may cause storage issues")
	}

	return nil
}
