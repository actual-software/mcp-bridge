package main

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWaitHelperCondition(t *testing.T) {
	helper := NewWaitHelper(t).WithTimeout(2 * time.Second)

	counter := 0
	start := time.Now()

	helper.WaitForCondition("counter reaches 3", func() bool {
		counter++

		return counter >= 3
	})

	elapsed := time.Since(start)

	assert.Equal(t, 3, counter)
	assert.Less(t, elapsed, 500*time.Millisecond, "Should complete quickly")
}

func TestWaitHelperFile(t *testing.T) {
	helper := NewWaitHelper(t).WithTimeout(2 * time.Second)

	// Create a temporary file
	tmpFile := "/tmp/test_wait_helper.txt"

	defer func() {
		if err := os.Remove(tmpFile); err != nil {
			t.Logf("Failed to remove temp file: %v", err)
		}
	}()

	// Start goroutine to create file after delay
	go func() {
		time.Sleep(200 * time.Millisecond)

		if err := os.WriteFile(tmpFile, []byte("test"), 0600); err != nil {
			t.Logf("Failed to write file: %v", err)
		}
	}()

	start := time.Now()

	helper.WaitForFile(tmpFile, nil)

	elapsed := time.Since(start)

	assert.FileExists(t, tmpFile)
	assert.Greater(t, elapsed, 200*time.Millisecond, "Should wait for file")
	assert.Less(t, elapsed, 500*time.Millisecond, "Should complete quickly after file appears")
}

func TestWaitHelperFiles(t *testing.T) {
	helper := NewWaitHelper(t).WithTimeout(2 * time.Second)

	// Create temporary files
	files := []string{
		"/tmp/test_wait_1.txt",
		"/tmp/test_wait_2.txt",
		"/tmp/test_wait_3.txt",
	}

	// Clean up
	defer func() {
		for _, f := range files {
			if err := os.Remove(f); err != nil {
				t.Logf("Failed to remove file %s: %v", f, err)
			}
		}
	}()

	// Create files immediately
	for _, f := range files {
		require.NoError(t, os.WriteFile(f, []byte("test"), 0600))
	}

	// Should complete quickly since files exist
	start := time.Now()

	helper.WaitForFiles(files...)

	elapsed := time.Since(start)

	assert.Less(t, elapsed, 200*time.Millisecond, "Should complete quickly")
}

func TestWaitHelperBriefly(t *testing.T) {
	helper := NewWaitHelper(t).WithPollInterval(50 * time.Millisecond)

	start := time.Now()

	helper.WaitBriefly("test brief wait")

	elapsed := time.Since(start)

	assert.Greater(t, elapsed, 40*time.Millisecond)
	assert.Less(t, elapsed, 100*time.Millisecond)
}

func TestWaitHelperTimeouts(t *testing.T) {
	// This test verifies that timeout works correctly
	// We use t.Run with a sub-test that we expect to fail
	t.Run("Timeout triggers correctly", func(t *testing.T) {
		// Create a mock test that captures failures
		mockT := &testing.T{}
		helper := NewWaitHelper(mockT).WithTimeout(100 * time.Millisecond)

		start := time.Now()
		// This condition never becomes true
		helper.WaitForCondition("never true", func() bool {
			return false
		})

		elapsed := time.Since(start)

		// The helper should timeout
		assert.True(t, mockT.Failed(), "Mock test should have failed")
		assert.Greater(t, elapsed, 100*time.Millisecond)
		assert.Less(t, elapsed, 200*time.Millisecond)
	})
}
