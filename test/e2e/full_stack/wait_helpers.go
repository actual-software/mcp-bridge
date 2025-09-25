package main

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// WaitHelper provides utilities for waiting on conditions in tests.
type WaitHelper struct {
	t              *testing.T
	defaultTimeout time.Duration
	pollInterval   time.Duration
}

// NewWaitHelper creates a new wait helper with default settings.
func NewWaitHelper(t *testing.T) *WaitHelper {
	t.Helper()

	return &WaitHelper{
		t:              t,
		defaultTimeout: time.Duration(defaultTimeoutSeconds) * time.Second,
		pollInterval:   time.Duration(defaultPollIntervalMillis) * time.Millisecond,
	}
}

// WithTimeout sets a custom timeout for wait operations.
func (w *WaitHelper) WithTimeout(timeout time.Duration) *WaitHelper {
	w.defaultTimeout = timeout

	return w
}

// WithPollInterval sets a custom poll interval for wait operations.
func (w *WaitHelper) WithPollInterval(interval time.Duration) *WaitHelper {
	w.pollInterval = interval

	return w
}

// WaitForCondition waits for a condition to become true.
func (w *WaitHelper) WaitForCondition(name string, condition func() bool) {
	w.t.Helper()
	require.Eventually(w.t, condition, w.defaultTimeout, w.pollInterval,
		"Timeout waiting for condition: %s", name)
}

// WaitForConditionWithContext waits for a condition with context support.
func (w *WaitHelper) WaitForConditionWithContext(ctx context.Context, name string, condition func() bool) error {
	w.t.Helper()

	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()

	timeout := time.After(w.defaultTimeout)

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled while waiting for %s: %w", name, ctx.Err())
		case <-timeout:
			return fmt.Errorf("timeout waiting for condition: %s", name)
		case <-ticker.C:
			if condition() {
				w.t.Logf("Condition met: %s", name)

				return nil
			}
		}
	}
}

// WaitForFile waits for a file to exist and optionally have content.
func (w *WaitHelper) WaitForFile(path string, checkContent func([]byte) bool) {
	w.t.Helper()

	condition := func() bool {
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			return false
		}

		if checkContent != nil {
			// #nosec G304 - path is provided by test code
			data, err := os.ReadFile(path)
			if err != nil {
				return false
			}

			return checkContent(data)
		}

		return info.Size() > 0
	}

	w.WaitForCondition("file exists: "+path, condition)
}

// WaitForFiles waits for multiple files to exist.
func (w *WaitHelper) WaitForFiles(paths ...string) {
	w.t.Helper()

	for _, path := range paths {
		w.WaitForFile(path, nil)
	}
}

// WaitForServiceHealth waits for a service to report healthy status.
func (w *WaitHelper) WaitForServiceHealth(checkHealth func() error) {
	w.t.Helper()

	condition := func() bool {
		return checkHealth() == nil
	}

	w.WaitForCondition("service health check", condition)
}

// WaitForPort waits for a network port to become available.
func (w *WaitHelper) WaitForPort(host string, port int, checkFunc func() error) {
	w.t.Helper()

	condition := func() bool {
		if checkFunc != nil {
			return checkFunc() == nil
		}
		// Basic port check could be added here
		return true
	}

	w.WaitForCondition(fmt.Sprintf("port %s:%d available", host, port), condition)
}

// WaitBriefly pauses briefly for operations that need settling time.
// Use sparingly and only when proper conditions cannot be determined.
func (w *WaitHelper) WaitBriefly(reason string) {
	w.t.Helper()
	w.t.Logf("Brief wait: %s", reason)
	time.Sleep(w.pollInterval)
}
