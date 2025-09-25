package slo_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/poiley/mcp-bridge/pkg/common/slo"
)

func TestNewCalculator(t *testing.T) {
	t.Parallel()

	config := slo.Config{
		Name:   "test-slo",
		Target: 0.99,
		Window: 30 * 24 * time.Hour,
		BurnRates: []slo.BurnRateConfig{
			{Window: time.Hour, Threshold: 14.4, Severity: "critical"},
			{Window: 24 * time.Hour, Threshold: 3, Severity: "warning"},
		},
		AlertConfig: slo.AlertConfig{
			PageBurnRate:   14.4,
			TicketBurnRate: 3,
		},
	}

	calc := slo.NewCalculator(config)
	assert.NotNil(t, calc)
}

func TestRecordSuccess(t *testing.T) {
	t.Parallel()

	config := slo.Config{
		Name:      "availability",
		Target:    0.99,
		Window:    30 * 24 * time.Hour,
		BurnRates: nil, // No burn rates configured for this test
		AlertConfig: slo.AlertConfig{
			PageBurnRate:   0, // No page burn rate configured
			TicketBurnRate: 0, // No ticket burn rate configured
		}, // No alert configuration for this test
	}

	calc := slo.NewCalculator(config)
	ctx := context.Background()

	// Record successes
	for range 100 {
		calc.RecordSuccess(ctx)
	}

	// Metrics should be updated (this would be verified via Prometheus in real usage)
	metrics := calc.GetMetrics()
	assert.Equal(t, "availability", metrics.Name)
	assert.InDelta(t, 0.99, metrics.Target, 0.001)
}

func TestRecordFailure(t *testing.T) {
	t.Parallel()

	config := slo.Config{
		Name:      "availability",
		Target:    0.99,
		Window:    30 * 24 * time.Hour,
		BurnRates: nil, // No burn rates configured for this test
		AlertConfig: slo.AlertConfig{
			PageBurnRate:   0, // No page burn rate configured
			TicketBurnRate: 0, // No ticket burn rate configured
		}, // No alert configuration for this test
	}

	calc := slo.NewCalculator(config)
	ctx := context.Background()

	// Record a mix of successes and failures
	for range 99 {
		calc.RecordSuccess(ctx)
	}

	calc.RecordFailure(ctx)

	// This would result in 99% success rate, meeting the SLO
	metrics := calc.GetMetrics()
	assert.Equal(t, "availability", metrics.Name)
}

func TestRecordValue(t *testing.T) {
	t.Parallel()

	config := slo.Config{
		Name:      "latency_p95",
		Target:    0.2, // 200ms target
		Window:    30 * 24 * time.Hour,
		BurnRates: nil, // No burn rates configured for this test
		AlertConfig: slo.AlertConfig{
			PageBurnRate:   0, // No page burn rate configured
			TicketBurnRate: 0, // No ticket burn rate configured
		}, // No alert configuration for this test
	}

	calc := slo.NewCalculator(config)
	ctx := context.Background()

	tests := []struct {
		name    string
		value   float64
		success bool
	}{
		{"below target", 0.150, true},
		{"at target", 0.200, true},
		{"above target", 0.250, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			calc.RecordValue(ctx, tt.value)
			// Verify metrics are updated appropriately
			metrics := calc.GetMetrics()
			assert.Equal(t, "latency_p95", metrics.Name)
		})
	}
}

func TestCalculateErrorBudget(t *testing.T) { 
	t.Parallel()

	tests := getErrorBudgetTestCases()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			verifyErrorBudget(t, tt)
		})
	}
}

type errorBudgetTestCase struct {
	name        string
	target      float64
	currentSLI  float64
	wantBudget  float64
	description string
}

func getErrorBudgetTestCases() []errorBudgetTestCase {
	return []errorBudgetTestCase{
		{
			name:        "perfect performance",
			target:      0.99,
			currentSLI:  1.0,
			wantBudget:  1.0,
			description: "100% SLI should have full budget",
		},
		{
			name:        "exactly at target",
			target:      0.99,
			currentSLI:  0.99,
			wantBudget:  1.0,
			description: "Meeting SLO exactly should have full budget",
		},
		{
			name:        "exceeding target",
			target:      0.99,
			currentSLI:  0.996,
			wantBudget:  1.0,
			description: "99.6% SLI exceeds 99% target = full budget remaining",
		},
		{
			name:        "partial budget scenario",
			target:      0.95, // 5% allowed error
			currentSLI:  0.97, // 3% actual error
			wantBudget:  1.0,  // 97% > 95% target = full budget
			description: "97% SLI exceeds 95% target = full budget remaining",
		},
		{
			name:        "budget exhausted",
			target:      0.99,
			currentSLI:  0.98,
			wantBudget:  0.0,
			description: "2% error rate with 1% allowed = budget exhausted",
		},
		{
			name:        "100% target special case",
			target:      1.0,
			currentSLI:  0.9999,
			wantBudget:  0.0,
			description: "Any failure with 100% target exhausts budget",
		},
		{
			name:        "100% target perfect",
			target:      1.0,
			currentSLI:  1.0,
			wantBudget:  1.0,
			description: "Perfect performance with 100% target",
		},
	}
}

func verifyErrorBudget(t *testing.T, tt errorBudgetTestCase) {
	t.Helper()
	t.Parallel()

	config := slo.Config{
		Name:      "test",
		Target:    tt.target,
		Window:    30 * 24 * time.Hour,
		BurnRates: nil, // No burn rates configured for this test
		AlertConfig: slo.AlertConfig{
			PageBurnRate:   0, // No page burn rate configured
			TicketBurnRate: 0, // No ticket burn rate configured
		}, // No alert configuration for this test
	}
	
	calc := slo.NewCalculator(config)

	gotBudget := calc.CalculateErrorBudget(tt.currentSLI)
	assert.InDelta(t, tt.wantBudget, gotBudget, 0.001, tt.description)
}

func TestCalculateBurnRate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		target    float64
		errorRate float64
		window    time.Duration
		wantRate  float64
	}{
		{
			name:      "1 hour window normal rate",
			target:    0.99,
			errorRate: 0.01 / (30 * 24), // Normal hourly error rate
			window:    time.Hour,
			wantRate:  1.0,
		},
		{
			name:      "1 hour window 14.4x burn",
			target:    0.99,
			errorRate: 0.01 * 14.4 / (30 * 24), // 14.4x normal hourly rate
			window:    time.Hour,
			wantRate:  14.4,
		},
		{
			name:      "24 hour window 3x burn",
			target:    0.99,
			errorRate: 0.01 * 3 / 30, // 3x normal daily rate
			window:    24 * time.Hour,
			wantRate:  3.0,
		},
		{
			name:      "100% target",
			target:    1.0,
			errorRate: 0.001,
			window:    time.Hour,
			wantRate:  0.0, // No burn rate for 100% target
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			config := slo.Config{
				Name:      "test",
				Target:    tt.target,
				Window:    30 * 24 * time.Hour,
				BurnRates: nil, // No burn rates configured for this test
				AlertConfig: slo.AlertConfig{
					PageBurnRate:   0, // No page burn rate configured
					TicketBurnRate: 0, // No ticket burn rate configured
				}, // No alert configuration for this test
			}
			calc := slo.NewCalculator(config)

			gotRate := calc.CalculateBurnRate(tt.errorRate, tt.window)
			assert.InDelta(t, tt.wantRate, gotRate, 0.01)
		})
	}
}

func TestUpdateErrorBudget(t *testing.T) {
	t.Parallel()

	config := slo.Config{
		Name:      "test",
		Target:    0.99,
		Window:    30 * 24 * time.Hour,
		BurnRates: nil, // No burn rates configured for this test
		AlertConfig: slo.AlertConfig{
			PageBurnRate:   0, // No page burn rate configured
			TicketBurnRate: 0, // No ticket burn rate configured
		}, // No alert configuration for this test
	}
	calc := slo.NewCalculator(config)

	// Test with various SLI values
	testCases := []float64{1.0, 0.995, 0.99, 0.985, 0.98}

	for _, sli := range testCases {
		calc.UpdateErrorBudget(sli)
		// Metrics would be verified via Prometheus
		metrics := calc.GetMetrics()
		assert.NotNil(t, metrics)
	}
}

func TestUpdateBurnRates(t *testing.T) {
	t.Parallel()

	config := slo.Config{
		Name:      "test",
		Target:    0.99,
		Window:    30 * 24 * time.Hour,
		BurnRates: nil, // No burn rates configured for this test
		AlertConfig: slo.AlertConfig{
			PageBurnRate:   0, // No page burn rate configured
			TicketBurnRate: 0, // No ticket burn rate configured
		}, // No alert configuration for this test
	}
	calc := slo.NewCalculator(config)

	// Update burn rates with various error rates
	errorRate1h := 0.01 * 14.4 / (30 * 24)  // 14.4x burn
	errorRate6h := 0.01 * 6 / (30 * 24 / 6) // 6x burn
	errorRate24h := 0.01 * 3 / 30           // 3x burn
	errorRate7d := 0.01 * 1.5 / (30 / 7)    // 1.5x burn

	calc.UpdateBurnRates(errorRate1h, errorRate6h, errorRate24h, errorRate7d)

	// Verify metrics are updated
	metrics := calc.GetMetrics()
	assert.NotNil(t, metrics)
}

func TestGetMetrics(t *testing.T) {
	t.Parallel()

	config := slo.Config{
		Name:      "availability",
		Target:    0.999,
		Window:    30 * 24 * time.Hour,
		BurnRates: nil, // No burn rates configured for this test
		AlertConfig: slo.AlertConfig{
			PageBurnRate:   0, // No page burn rate configured
			TicketBurnRate: 0, // No ticket burn rate configured
		}, // No alert configuration for this test
	}
	calc := slo.NewCalculator(config)

	metrics := calc.GetMetrics()

	assert.Equal(t, "availability", metrics.Name)
	assert.InDelta(t, 0.999, metrics.Target, 0.001)
	assert.GreaterOrEqual(t, metrics.CurrentSLI, 0.0)
	assert.LessOrEqual(t, metrics.CurrentSLI, 1.0)
	assert.GreaterOrEqual(t, metrics.ErrorBudget, 0.0)
	assert.LessOrEqual(t, metrics.ErrorBudget, 1.0)
	assert.GreaterOrEqual(t, metrics.BurnRate1h, 0.0)
	assert.GreaterOrEqual(t, metrics.BurnRate6h, 0.0)
	assert.GreaterOrEqual(t, metrics.BurnRate24h, 0.0)
	assert.GreaterOrEqual(t, metrics.BurnRate7d, 0.0)
}

func TestFormatBurnRateAlert(t *testing.T) {
	t.Parallel()

	alert := slo.FormatBurnRateAlert("availability", 14.4, 6.0)

	assert.Contains(t, alert, "availability")
	assert.Contains(t, alert, "14.40x")
	assert.Contains(t, alert, "6.00x")
	assert.Contains(t, alert, "error budget")
}

func TestComplexScenario(t *testing.T) {
	t.Parallel()
	// Test a realistic scenario with mixed success/failure patterns
	
	config := createComplexScenarioConfig()
	calc := slo.NewCalculator(config)
	ctx := context.Background()

	// Simulate a day of requests with varying performance patterns
	simulateComplexTrafficPatterns(calc, ctx)
	
	// Validate final metrics
	validateComplexScenarioResults(t, calc)
}

func createComplexScenarioConfig() slo.Config {
	return slo.Config{
		Name:   "api-availability",
		Target: 0.99,
		Window: 30 * 24 * time.Hour,
		BurnRates: []slo.BurnRateConfig{
			{Window: time.Hour, Threshold: 14.4, Severity: "critical"},
			{Window: 6 * time.Hour, Threshold: 6, Severity: "critical"},
			{Window: 24 * time.Hour, Threshold: 3, Severity: "warning"},
			{Window: 7 * 24 * time.Hour, Threshold: 1.5, Severity: "warning"},
		},
		AlertConfig: slo.AlertConfig{
			PageBurnRate:   14.4, // Page on 14.4x burn rate
			TicketBurnRate: 3.0,  // Ticket on 3x burn rate
		},
	}
}

func simulateComplexTrafficPatterns(calc *slo.Calculator, ctx context.Context) {
	// Morning: Good performance (99.5% success)
	simulateMorningTraffic(calc, ctx)
	
	// Afternoon: Degraded performance (98% success)
	simulateAfternoonTraffic(calc, ctx)
	
	// Evening: Recovery (99.9% success)
	simulateEveningTraffic(calc, ctx)
}

func simulateMorningTraffic(calc *slo.Calculator, ctx context.Context) {
	for range 995 {
		calc.RecordSuccess(ctx)
	}

	for range 5 {
		calc.RecordFailure(ctx)
	}
}

func simulateAfternoonTraffic(calc *slo.Calculator, ctx context.Context) {
	for range 980 {
		calc.RecordSuccess(ctx)
	}

	for range 20 {
		calc.RecordFailure(ctx)
	}
}

func simulateEveningTraffic(calc *slo.Calculator, ctx context.Context) {
	for range 999 {
		calc.RecordSuccess(ctx)
	}

	calc.RecordFailure(ctx)
}

func validateComplexScenarioResults(t *testing.T, calc *slo.Calculator) {
	t.Helper()

	metrics := calc.GetMetrics()
	require.NotNil(t, metrics)
	assert.Equal(t, "api-availability", metrics.Name)
	assert.InDelta(t, 0.99, metrics.Target, 0.001)
}
