// Package slo provides utilities for calculating SLI/SLO metrics
package slo

import (
	"context"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	// Time constants for burn rate calculations.
	hoursIn6     = 6
	hoursIn24    = 24
	hoursIn7Days = 7 * 24
	daysInMonth  = 30

	// SLI thresholds.
	defaultSLI = 0.999
	lowSLI     = 0.8
)

// Config defines configuration for an SLO.
type Config struct {
	Name        string
	Target      float64
	Window      time.Duration
	BurnRates   []BurnRateConfig
	AlertConfig AlertConfig
}

// BurnRateConfig defines burn rate alert thresholds.
type BurnRateConfig struct {
	Window    time.Duration
	Threshold float64
	Severity  string
}

// AlertConfig defines alerting configuration.
type AlertConfig struct {
	PageBurnRate   float64 // e.g., 14.4 for 2% in 1 hour
	TicketBurnRate float64 // e.g., 3 for 10% in 24 hours
}

// Calculator calculates SLI/SLO metrics.
type Calculator struct {
	config  Config
	metrics *sloMetrics
}

type sloMetrics struct {
	sliValue          prometheus.Gauge
	errorBudget       prometheus.Gauge
	burnRate1h        prometheus.Gauge
	burnRate6h        prometheus.Gauge
	burnRate24h       prometheus.Gauge
	burnRate7d        prometheus.Gauge
	violations        prometheus.Counter
	budgetExhausted   prometheus.Gauge
	measurementTotal  prometheus.Counter
	measurementErrors prometheus.Counter
}

var (
	// Global metric vectors to avoid duplicate registration.
	sliValueVec = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name:        "slo_sli_value",
		Help:        "Current SLI value",
		Namespace:   "",  // Empty namespace
		Subsystem:   "",  // Empty subsystem
		ConstLabels: nil, // No constant labels
	}, []string{"slo_name"})

	errorBudgetVec = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name:        "slo_error_budget_ratio",
		Help:        "Remaining error budget ratio",
		Namespace:   "",  // Empty namespace
		Subsystem:   "",  // Empty subsystem
		ConstLabels: nil, // No constant labels
	}, []string{"slo_name"})

	burnRate1hVec = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name:        "slo_burn_rate_1h",
		Help:        "Error budget burn rate over 1 hour",
		Namespace:   "",  // Empty namespace
		Subsystem:   "",  // Empty subsystem
		ConstLabels: nil, // No constant labels
	}, []string{"slo_name"})

	burnRate6hVec = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name:        "slo_burn_rate_6h",
		Help:        "Error budget burn rate over 6 hours",
		Namespace:   "",  // Empty namespace
		Subsystem:   "",  // Empty subsystem
		ConstLabels: nil, // No constant labels
	}, []string{"slo_name"})

	burnRate24hVec = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name:        "slo_burn_rate_24h",
		Help:        "Error budget burn rate over 24 hours",
		Namespace:   "",  // Empty namespace
		Subsystem:   "",  // Empty subsystem
		ConstLabels: nil, // No constant labels
	}, []string{"slo_name"})

	burnRate7dVec = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name:        "slo_burn_rate_7d",
		Help:        "Error budget burn rate over 7 days",
		Namespace:   "",  // Empty namespace
		Subsystem:   "",  // Empty subsystem
		ConstLabels: nil, // No constant labels
	}, []string{"slo_name"})

	violationsVec = promauto.NewCounterVec(prometheus.CounterOpts{
		Name:        "slo_violations_total",
		Help:        "Total number of SLO violations",
		Namespace:   "",  // Empty namespace
		Subsystem:   "",  // Empty subsystem
		ConstLabels: nil, // No constant labels
	}, []string{"slo_name"})

	budgetExhaustedVec = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name:        "slo_budget_exhausted",
		Help:        "Whether error budget is exhausted (1) or not (0)",
		Namespace:   "",  // Empty namespace
		Subsystem:   "",  // Empty subsystem
		ConstLabels: nil, // No constant labels
	}, []string{"slo_name"})

	measurementTotalVec = promauto.NewCounterVec(prometheus.CounterOpts{
		Name:        "slo_measurements_total",
		Help:        "Total number of SLI measurements",
		Namespace:   "",  // Empty namespace
		Subsystem:   "",  // Empty subsystem
		ConstLabels: nil, // No constant labels
	}, []string{"slo_name"})

	measurementErrorsVec = promauto.NewCounterVec(prometheus.CounterOpts{
		Name:        "slo_measurement_errors_total",
		Help:        "Total number of failed SLI measurements",
		Namespace:   "",  // Empty namespace
		Subsystem:   "",  // Empty subsystem
		ConstLabels: nil, // No constant labels
	}, []string{"slo_name"})
)

// NewCalculator creates a new SLO calculator.
func NewCalculator(config Config) *Calculator {
	metrics := &sloMetrics{
		sliValue:          sliValueVec.WithLabelValues(config.Name),
		errorBudget:       errorBudgetVec.WithLabelValues(config.Name),
		burnRate1h:        burnRate1hVec.WithLabelValues(config.Name),
		burnRate6h:        burnRate6hVec.WithLabelValues(config.Name),
		burnRate24h:       burnRate24hVec.WithLabelValues(config.Name),
		burnRate7d:        burnRate7dVec.WithLabelValues(config.Name),
		violations:        violationsVec.WithLabelValues(config.Name),
		budgetExhausted:   budgetExhaustedVec.WithLabelValues(config.Name),
		measurementTotal:  measurementTotalVec.WithLabelValues(config.Name),
		measurementErrors: measurementErrorsVec.WithLabelValues(config.Name),
	}

	return &Calculator{
		config:  config,
		metrics: metrics,
	}
}

// RecordSuccess records a successful SLI measurement.
func (c *Calculator) RecordSuccess(ctx context.Context) {
	c.recordMeasurement(ctx, true)
}

// RecordFailure records a failed SLI measurement.
func (c *Calculator) RecordFailure(ctx context.Context) {
	c.recordMeasurement(ctx, false)
}

// RecordValue records an SLI value directly (e.g., latency).
func (c *Calculator) RecordValue(_ context.Context, value float64) {
	c.metrics.measurementTotal.Inc()

	// For latency SLOs, success is when value is below target
	success := value <= c.config.Target
	c.updateMetrics(success)

	// Record the actual value
	c.metrics.sliValue.Set(value)
}

func (c *Calculator) recordMeasurement(_ context.Context, success bool) {
	c.metrics.measurementTotal.Inc()

	if !success {
		c.metrics.measurementErrors.Inc()
	}

	c.updateMetrics(success)
}

func (c *Calculator) updateMetrics(success bool) {
	// Update SLI value (1.0 for success, 0.0 for failure)
	sliValue := 0.0
	if success {
		sliValue = 1.0
	}

	c.metrics.sliValue.Set(sliValue)

	// Check for violation
	if sliValue < c.config.Target {
		c.metrics.violations.Inc()
	}
	// Note: Burn rate calculations would typically be done by Prometheus
	// recording rules based on the rate of violations over time windows
} 

// CalculateErrorBudget calculates the remaining error budget.
func (c *Calculator) CalculateErrorBudget(currentSLI float64) float64 {
	if c.config.Target == 1.0 {
		// Special case: 100% target means any failure exhausts budget
		if currentSLI < 1.0 {
			return 0.0
		}

		return 1.0
	}

	// If we're at or above the target, we have full budget
	if currentSLI >= c.config.Target {
		return 1.0
	}

	// Error budget = 1 - (actual_error_rate / allowed_error_rate)
	allowedErrorRate := 1.0 - c.config.Target
	actualErrorRate := 1.0 - currentSLI

	if actualErrorRate >= allowedErrorRate {
		return 0.0 // Budget exhausted
	}

	return 1.0 - (actualErrorRate / allowedErrorRate)
}

// CalculateBurnRate calculates the burn rate for a given window.
func (c *Calculator) CalculateBurnRate(errorRate float64, window time.Duration) float64 {
	// Burn rate = (error_rate_in_window / allowed_error_rate) * (month / window)
	allowedErrorRate := 1.0 - c.config.Target
	if allowedErrorRate == 0 {
		return 0 // 100% SLO target
	}

	monthHours := float64(daysInMonth * hoursIn24)
	windowHours := window.Hours()

	return (errorRate / allowedErrorRate) * (monthHours / windowHours)
}

// UpdateErrorBudget updates the error budget metric.
func (c *Calculator) UpdateErrorBudget(currentSLI float64) {
	budget := c.CalculateErrorBudget(currentSLI)
	c.metrics.errorBudget.Set(budget)

	// Update exhausted flag
	if budget <= 0 {
		c.metrics.budgetExhausted.Set(1)
	} else {
		c.metrics.budgetExhausted.Set(0)
	}
}

// UpdateBurnRates updates burn rate metrics based on error rates over different windows.
func (c *Calculator) UpdateBurnRates(errorRate1h, errorRate6h, errorRate24h, errorRate7d float64) {
	c.metrics.burnRate1h.Set(c.CalculateBurnRate(errorRate1h, time.Hour))
	c.metrics.burnRate6h.Set(c.CalculateBurnRate(errorRate6h, hoursIn6*time.Hour))
	c.metrics.burnRate24h.Set(c.CalculateBurnRate(errorRate24h, hoursIn24*time.Hour))
	c.metrics.burnRate7d.Set(c.CalculateBurnRate(errorRate7d, hoursIn7Days*time.Hour))
}

// GetMetrics returns the current metrics for external monitoring.
func (c *Calculator) GetMetrics() Metrics {
	return Metrics{
		Name:            c.config.Name,
		Target:          c.config.Target,
		CurrentSLI:      c.getCurrentSLI(),
		ErrorBudget:     c.getCurrentErrorBudget(),
		BurnRate1h:      c.getBurnRate1h(),
		BurnRate6h:      c.getBurnRate6h(),
		BurnRate24h:     c.getBurnRate24h(),
		BurnRate7d:      c.getBurnRate7d(),
		BudgetExhausted: c.isBudgetExhausted(),
	}
}

// Metrics represents current SLO metrics.
type Metrics struct {
	Name            string
	Target          float64
	CurrentSLI      float64
	ErrorBudget     float64
	BurnRate1h      float64
	BurnRate6h      float64
	BurnRate24h     float64
	BurnRate7d      float64
	BudgetExhausted bool
}

// Helper methods to get current metric values.
func (c *Calculator) getCurrentSLI() float64 {
	// This would typically query Prometheus, but for now return a placeholder
	return defaultSLI
}

func (c *Calculator) getCurrentErrorBudget() float64 {
	// This would typically query Prometheus, but for now return a placeholder
	return lowSLI
}

func (c *Calculator) getBurnRate1h() float64 {
	// This would typically query Prometheus, but for now return a placeholder
	return 1.0
}

func (c *Calculator) getBurnRate6h() float64 {
	// This would typically query Prometheus, but for now return a placeholder
	return 1.0
}

func (c *Calculator) getBurnRate24h() float64 {
	// This would typically query Prometheus, but for now return a placeholder
	return 1.0
}

func (c *Calculator) getBurnRate7d() float64 {
	// This would typically query Prometheus, but for now return a placeholder
	return 1.0
}

func (c *Calculator) isBudgetExhausted() bool {
	// This would typically query Prometheus, but for now return a placeholder
	return false
}

// FormatBurnRateAlert formats a burn rate alert message.
func FormatBurnRateAlert(sloName string, burnRate1h, burnRate6h float64) string {
	return fmt.Sprintf(
		"SLO '%s' error budget burn rate is too high\n"+
			"1h burn rate: %.2fx normal\n"+
			"6h burn rate: %.2fx normal\n"+
			"At this rate, the monthly error budget will be exhausted soon.",
		sloName, burnRate1h, burnRate6h,
	)
}
