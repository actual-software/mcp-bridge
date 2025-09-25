package direct

import (
	"time"

	"go.uber.org/zap"
)

// TimeoutAdapter handles adaptive timeout adjustments.
type TimeoutAdapter struct {
	timeout *AdaptiveTimeout
	metrics *TimeoutMetrics
}

// TimeoutMetrics holds calculated metrics for timeout adaptation.
type TimeoutMetrics struct {
	SuccessRatio float64
	AvgDuration  time.Duration
	SampleSize   int
}

// CreateTimeoutAdapter creates a new timeout adapter.
func CreateTimeoutAdapter(timeout *AdaptiveTimeout) *TimeoutAdapter {
	return &TimeoutAdapter{
		timeout: timeout,
	}
}

// AdaptTimeout adapts the timeout based on request history.
func (a *TimeoutAdapter) AdaptTimeout() {
	a.timeout.mu.Lock()
	defer a.timeout.mu.Unlock()

	if !a.hasEnoughData() {
		return
	}

	a.metrics = a.calculateMetrics()
	oldTimeout := a.timeout.currentTimeout

	a.adjustTimeout()

	a.logIfSignificantChange(oldTimeout)
	a.timeout.lastAdaptation = time.Now()
}

func (a *TimeoutAdapter) hasEnoughData() bool {
	return len(a.timeout.requestHistory) >= MinSampleSize
}

func (a *TimeoutAdapter) calculateMetrics() *TimeoutMetrics {
	successCount := 0
	totalDuration := time.Duration(0)

	for _, req := range a.timeout.requestHistory {
		if req.Success {
			successCount++
		}

		totalDuration += req.Duration
	}

	sampleSize := len(a.timeout.requestHistory)

	return &TimeoutMetrics{
		SuccessRatio: float64(successCount) / float64(sampleSize),
		AvgDuration:  totalDuration / time.Duration(sampleSize),
		SampleSize:   sampleSize,
	}
}

func (a *TimeoutAdapter) adjustTimeout() {
	if a.shouldIncreaseTimeout() {
		a.increaseTimeout()
	} else if a.shouldDecreaseTimeout() {
		a.decreaseTimeout()
	}
}

func (a *TimeoutAdapter) shouldIncreaseTimeout() bool {
	return a.metrics.SuccessRatio < a.timeout.config.SuccessRatio
}

func (a *TimeoutAdapter) shouldDecreaseTimeout() bool {
	return a.metrics.SuccessRatio >= a.timeout.config.SuccessRatio &&
		a.metrics.AvgDuration < a.timeout.currentTimeout/3
}

func (a *TimeoutAdapter) increaseTimeout() {
	calculator := &TimeoutIncreaseCalculator{
		timeout: a.timeout,
		metrics: a.metrics,
	}
	a.timeout.currentTimeout = calculator.Calculate()
}

func (a *TimeoutAdapter) decreaseTimeout() {
	calculator := &TimeoutDecreaseCalculator{
		timeout: a.timeout,
		metrics: a.metrics,
	}
	a.timeout.currentTimeout = calculator.Calculate()
}

func (a *TimeoutAdapter) logIfSignificantChange(oldTimeout time.Duration) {
	if abs(a.timeout.currentTimeout-oldTimeout) <= time.Second {
		return
	}

	a.timeout.logger.Info("adapted timeout",
		zap.Duration("old_timeout", oldTimeout),
		zap.Duration("new_timeout", a.timeout.currentTimeout),
		zap.Float64("success_ratio", a.metrics.SuccessRatio),
		zap.Duration("avg_duration", a.metrics.AvgDuration),
		zap.Int("sample_size", a.metrics.SampleSize))
}

// TimeoutIncreaseCalculator calculates timeout increases.
type TimeoutIncreaseCalculator struct {
	timeout *AdaptiveTimeout
	metrics *TimeoutMetrics
}

// Calculate calculates the new increased timeout.
func (c *TimeoutIncreaseCalculator) Calculate() time.Duration {
	factor := 1.0 + c.timeout.config.AdaptationRate
	newTimeout := time.Duration(float64(c.timeout.currentTimeout) * factor)

	// Consider average duration if it's significant.
	if c.metrics.AvgDuration > c.timeout.currentTimeout/DivisionFactorTwo {
		newTimeout = time.Duration(float64(c.metrics.AvgDuration) * SafetyLearningFactor)
	}

	return c.enforceMaxLimit(newTimeout)
}

func (c *TimeoutIncreaseCalculator) enforceMaxLimit(timeout time.Duration) time.Duration {
	if timeout > c.timeout.config.MaxTimeout {
		return c.timeout.config.MaxTimeout
	}

	return timeout
}

// TimeoutDecreaseCalculator calculates timeout decreases.
type TimeoutDecreaseCalculator struct {
	timeout *AdaptiveTimeout
	metrics *TimeoutMetrics
}

// Calculate calculates the new decreased timeout.
func (c *TimeoutDecreaseCalculator) Calculate() time.Duration {
	factor := 1.0 - c.timeout.config.AdaptationRate
	newTimeout := time.Duration(float64(c.timeout.currentTimeout) * factor)

	newTimeout = c.ensureReasonableTimeout(newTimeout)

	return c.enforceMinLimit(newTimeout)
}

func (c *TimeoutDecreaseCalculator) ensureReasonableTimeout(timeout time.Duration) time.Duration {
	minReasonable := time.Duration(float64(c.metrics.AvgDuration) * FloatDivisionFactorTwo)
	if timeout < minReasonable {
		return minReasonable
	}

	return timeout
}

func (c *TimeoutDecreaseCalculator) enforceMinLimit(timeout time.Duration) time.Duration {
	if timeout < c.timeout.config.MinTimeout {
		return c.timeout.config.MinTimeout
	}

	return timeout
}
