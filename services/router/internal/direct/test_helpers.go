package direct

// safeIntToUint64 safely converts an int to uint64, returning 0 for negative values.
// This helper is used in tests to avoid G115 integer overflow warnings.
func safeIntToUint64(n int) uint64 {
	if n < 0 {
		return 0
	}
	return uint64(n)
}