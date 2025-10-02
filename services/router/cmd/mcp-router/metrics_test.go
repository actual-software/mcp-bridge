package main

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"

	common "github.com/poiley/mcp-bridge/pkg/common/config"
	"github.com/poiley/mcp-bridge/services/router/internal/config"
	"github.com/poiley/mcp-bridge/services/router/internal/metrics"
	"github.com/poiley/mcp-bridge/services/router/internal/router"
)

func TestMetricsServer(t *testing.T) {
	cleanup := setupTestAuthToken(t)
	defer cleanup()

	configPath := createTestMetricsConfig(t)
	runMetricsServerTest(t, configPath)
}

func createTestMetricsConfig(t *testing.T) string {
	t.Helper()

	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")

	cfg := &config.Config{
		Version: 1,
		GatewayPool: config.GatewayPoolConfig{
			Endpoints: []config.GatewayEndpoint{
				{
					URL: "ws://localhost:8080",
					Auth: common.AuthConfig{
						Type: "bearer",
					},
				},
			},
		},
		Metrics: common.MetricsConfig{
			Enabled:  true,
			Endpoint: "localhost:0", // Use port 0 for automatic assignment
		},
	}

	data, err := yaml.Marshal(cfg)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(configPath, data, 0o600))

	return configPath
}

func runMetricsServerTest(t *testing.T, configPath string) {
	t.Helper()

	t.Run("Metrics server starts and stops", func(t *testing.T) {
		runWithMetrics := createMetricsRunFunction(t)

		cmd := &cobra.Command{
			Use:  "test",
			RunE: runWithMetrics,
		}
		cmd.Flags().StringP("config", "c", "", "Config file")
		cmd.Flags().String("log-level", "info", "Log level")

		cmd.SetArgs([]string{"--config", configPath})
		err := cmd.Execute()

		require.NoError(t, err)
	})
}

func createMetricsRunFunction(t *testing.T) func(*cobra.Command, []string) error {
	t.Helper()

	return func(cmd *cobra.Command, args []string) error {
		logger, cfg, err := setupMetricsTest(cmd)
		if err != nil {
			return err
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		return runMetricsServerWithTests(t, ctx, cancel, cfg, logger)
	}
}

func setupMetricsTest(cmd *cobra.Command) (*zap.Logger, *config.Config, error) {
	logLevel, _ := cmd.Flags().GetString("log-level")

	logger, err := initLogger(logLevel, false, &common.LoggingConfig{
		Level:  "info",
		Format: "json",
		Output: "stderr",
	})
	if err != nil {
		return nil, nil, err
	}

	configPath, _ := cmd.Flags().GetString("config")

	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, nil, err
	}

	return logger, cfg, nil
}

func runMetricsServerWithTests(
	t *testing.T,
	ctx context.Context,
	cancel context.CancelFunc,
	cfg *config.Config,
	logger *zap.Logger,
) error {
	t.Helper()

	r, _ := router.New(cfg, logger) //nolint:contextcheck // router.New creates its own context internally

	var metricsWg sync.WaitGroup

	var metricsExporter *metrics.PrometheusExporter

	if !cfg.Metrics.Enabled || cfg.Metrics.Endpoint == "" {
		return nil
	}

	metricsExporter = metrics.NewPrometheusExporter(cfg.Metrics.Endpoint, logger)
	setupMetricsCollection(metricsExporter, r)

	metricsWg.Add(1)

	go func() {
		defer metricsWg.Done()

		go func() {
			if err := metricsExporter.Start(ctx); err != nil {
				t.Logf("Metrics exporter start failed: %v", err)
			}
		}()
	}()

	time.Sleep(testIterations * time.Millisecond)

	endpoint := metricsExporter.GetEndpoint()
	testEndpoint(t, ctx, endpoint+"/metrics")
	testEndpoint(t, ctx, endpoint+"/health")

	cancel()
	metricsWg.Wait()

	return nil
}

func setupMetricsCollection(metricsExporter *metrics.PrometheusExporter, r *router.LocalRouter) {
	metricsExporter.SetRouterMetrics(func() *metrics.RouterMetrics {
		if r == nil {
			return &metrics.RouterMetrics{}
		}

		rm := r.GetMetrics()

		return &metrics.RouterMetrics{
			RequestsTotal:     rm.RequestsTotal,
			ResponsesTotal:    rm.ResponsesTotal,
			ErrorsTotal:       rm.ErrorsTotal,
			ConnectionRetries: rm.ConnectionRetries,
			ActiveConnections: rm.ActiveConnections,
		}
	})
}

func setupTestAuthToken(t *testing.T) func() {
	t.Helper()

	oldToken := os.Getenv("MCP_AUTH_TOKEN")
	if err := os.Setenv("MCP_AUTH_TOKEN", "test-token"); err != nil {
		t.Fatalf("Failed to set environment variable: %v", err)
	}

	return func() {
		if oldToken != "" {
			if err := os.Setenv("MCP_AUTH_TOKEN", oldToken); err != nil {
				t.Fatalf("Failed to set environment variable: %v", err)
			}
		} else {
			_ = os.Unsetenv("MCP_AUTH_TOKEN")
		}
	}
}

func createTestConfigWithMetrics(t *testing.T, enabled bool) string {
	t.Helper()
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.yaml")

	cfg := &config.Config{
		Version: 1,
		GatewayPool: config.GatewayPoolConfig{
			Endpoints: []config.GatewayEndpoint{
				{
					URL: "ws://localhost:8080",
					Auth: common.AuthConfig{
						Type: "bearer",
					},
				},
			},
		},
		Metrics: common.MetricsConfig{
			Enabled: enabled,
		},
	}

	data, err := yaml.Marshal(cfg)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(configPath, data, 0o600))

	return configPath
}

func TestMetricsDisabled(t *testing.T) {
	cleanup := setupTestAuthToken(t)
	defer cleanup()

	configPath := createTestConfigWithMetrics(t, false)

	testRun := func(cmd *cobra.Command, args []string) error {
		configPath, _ := cmd.Flags().GetString("config")

		cfg, err := config.Load(configPath)
		if err != nil {
			return err
		}

		assert.False(t, cfg.Metrics.Enabled)

		return nil
	}

	cmd := &cobra.Command{Use: "test", RunE: testRun}
	cmd.Flags().StringP("config", "c", "", "Config file")
	cmd.SetArgs([]string{"--config", configPath})

	err := cmd.Execute()
	require.NoError(t, err)
}

func TestMetricsCollection(t *testing.T) {
	logger := createTestLogger()
	mockMetrics := createMockMetrics()

	runMetricsCollectionTest(t, logger, mockMetrics)
}

func createTestLogger() *zap.Logger {
	logger, _ := initLogger("info", false, &common.LoggingConfig{
		Level:  "info",
		Format: "json",
		Output: "stderr",
	})

	return logger
}

func createMockMetrics() *router.Metrics {
	mockMetrics := &router.Metrics{
		RequestsTotal:     testIterations,
		ResponsesTotal:    95,
		ErrorsTotal:       5,
		ConnectionRetries: 3,
		ActiveConnections: 2,
		RequestDuration:   make(map[string][]time.Duration),
		ResponseSizes:     []int{testIterations, httpStatusOK, 300},
		ResponseSizesMu:   sync.Mutex{},
	}

	mockMetrics.RequestDuration["initialize"] = []time.Duration{
		time.Millisecond * testIterations,
		time.Millisecond * httpStatusOK,
	}
	mockMetrics.RequestDuration["tools/list"] = []time.Duration{
		time.Millisecond * testTimeout,
	}

	return mockMetrics
}

func runMetricsCollectionTest(t *testing.T, logger *zap.Logger, mockMetrics *router.Metrics) {
	t.Helper()

	exporter := metrics.NewPrometheusExporter("localhost:0", logger)
	setupMockMetricsCallback(exporter, mockMetrics)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		if err := exporter.Start(ctx); err != nil {
			t.Logf("Exporter start failed: %v", err)
		}
	}()

	time.Sleep(testIterations * time.Millisecond)

	metricsOutput := fetchMetricsOutput(t, ctx, exporter.GetEndpoint())
	verifyMetricsOutput(t, metricsOutput)
}

func setupMockMetricsCallback(exporter *metrics.PrometheusExporter, mockMetrics *router.Metrics) {
	exporter.SetRouterMetrics(func() *metrics.RouterMetrics {
		rm := &metrics.RouterMetrics{
			RequestsTotal:     mockMetrics.RequestsTotal,
			ResponsesTotal:    mockMetrics.ResponsesTotal,
			ErrorsTotal:       mockMetrics.ErrorsTotal,
			ConnectionRetries: mockMetrics.ConnectionRetries,
			ActiveConnections: mockMetrics.ActiveConnections,
			RequestDuration:   make(map[string][]time.Duration),
		}

		for method, durations := range mockMetrics.RequestDuration {
			rm.RequestDuration[method] = durations
		}

		mockMetrics.ResponseSizesMu.Lock()
		rm.ResponseSizes = append([]int{}, mockMetrics.ResponseSizes...)
		mockMetrics.ResponseSizesMu.Unlock()

		return rm
	})
}

func fetchMetricsOutput(t *testing.T, ctx context.Context, endpoint string) string {
	t.Helper()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+endpoint+"/metrics", nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Logf("Failed to close: %v", err)
		}
	}()

	body := make([]byte, 4096)
	n, _ := resp.Body.Read(body)

	return string(body[:n])
}

func verifyMetricsOutput(t *testing.T, metricsOutput string) {
	t.Helper()

	assert.Contains(t, metricsOutput, "mcp_router_requests_total 100")
	assert.Contains(t, metricsOutput, "mcp_router_responses_total 95")
	assert.Contains(t, metricsOutput, "mcp_router_errors_total 5")
	assert.Contains(t, metricsOutput, "mcp_router_connection_retries_total 3")
	assert.Contains(t, metricsOutput, "mcp_router_active_connections 2")
}

func testEndpoint(t *testing.T, ctx context.Context, endpoint string) {
	t.Helper()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+endpoint, nil)
	if err != nil {
		t.Errorf("Failed to create request for %s: %v", endpoint, err)

		return
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Logf("Request to %s failed: %v", endpoint, err)

		return
	}

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	if err := resp.Body.Close(); err != nil {
		t.Logf("Failed to close response body: %v", err)
	}
}
