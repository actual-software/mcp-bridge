package health

// Health check thresholds.
const (
	// HighRetryThreshold is the threshold for considering retry count as high.
	HighRetryThreshold = 10

	// HighErrorRateThreshold is the threshold percentage for considering error rate as high.
	HighErrorRateThreshold = 10

	// PercentageMultiplier is used to convert decimal to percentage.
	PercentageMultiplier = 100
)
