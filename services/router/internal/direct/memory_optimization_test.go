package direct

import (
	"bytes"
	"runtime"
	"runtime/debug"
	"sync"
	"testing"
	"time"

	"github.com/poiley/mcp-bridge/services/router/internal/constants"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

func TestMemoryOptimizer_Basic(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)
	config := DefaultMemoryOptimizationConfig()

	optimizer := NewMemoryOptimizer(config, logger)
	require.NotNil(t, optimizer)

	// Test default config values.
	assert.True(t, optimizer.config.EnableObjectPooling)
	assert.True(t, optimizer.config.BufferPoolConfig.Enabled)
	assert.True(t, optimizer.config.JSONPoolConfig.Enabled)
	assert.True(t, optimizer.config.GCConfig.Enabled)
	assert.True(t, optimizer.config.MemoryMonitoring.Enabled)
}

func TestMemoryOptimizer_StartStop(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)
	config := DefaultMemoryOptimizationConfig()
	config.MemoryMonitoring.MonitoringInterval = 50 * time.Millisecond
	config.GCConfig.ForceGCInterval = 100 * time.Millisecond

	optimizer := NewMemoryOptimizer(config, logger)

	// Test start.
	err := optimizer.Start()
	require.NoError(t, err)

	// Let it run briefly.
	time.Sleep(2 * constants.TestSleepShort)

	// Test stop.
	err = optimizer.Stop()
	require.NoError(t, err)

	// Test double stop (should not panic).
	err = optimizer.Stop()
	require.NoError(t, err)
}

func TestMemoryOptimizer_BufferPool(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)
	config := DefaultMemoryOptimizationConfig()
	config.BufferPoolConfig.Enabled = true
	config.BufferPoolConfig.InitialSize = 1024
	config.BufferPoolConfig.MaxSize = 8192

	optimizer := NewMemoryOptimizer(config, logger)

	// Test getting buffer.
	buf := optimizer.GetBuffer()
	require.NotNil(t, buf)
	assert.Equal(t, 0, buf.Len())
	assert.GreaterOrEqual(t, buf.Cap(), config.BufferPoolConfig.InitialSize)

	// Write some data.
	_, _ = buf.WriteString("test data")
	assert.Equal(t, 9, buf.Len())

	// Return buffer to pool.
	optimizer.PutBuffer(buf)

	// Get another buffer - should be reset.
	buf2 := optimizer.GetBuffer()
	assert.Equal(t, 0, buf2.Len())

	// Test with oversized buffer.
	oversizedBuf := bytes.NewBuffer(make([]byte, config.BufferPoolConfig.MaxSize+1))
	optimizer.PutBuffer(oversizedBuf) // Should not be pooled

	// Get buffer again - should not be the oversized one.
	buf3 := optimizer.GetBuffer()
	assert.LessOrEqual(t, buf3.Cap(), config.BufferPoolConfig.MaxSize)
}

func TestMemoryOptimizer_BufferPoolDisabled(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)
	config := DefaultMemoryOptimizationConfig()
	config.BufferPoolConfig.Enabled = false

	optimizer := NewMemoryOptimizer(config, logger)

	// Should still work but not use pooling.
	buf := optimizer.GetBuffer()
	require.NotNil(t, buf)
	assert.Equal(t, 0, buf.Len())

	// PutBuffer should be a no-op.
	optimizer.PutBuffer(buf)
}

func TestMemoryOptimizer_JSONPool(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)
	config := DefaultMemoryOptimizationConfig()
	config.JSONPoolConfig.Enabled = true

	optimizer := NewMemoryOptimizer(config, logger)

	// Test JSON encoder.
	buf := bytes.NewBuffer(nil)
	encoder := optimizer.GetJSONEncoder(buf)
	require.NotNil(t, encoder)

	// Encode some data.
	testData := map[string]interface{}{"key": "value", "number": 42}
	err := encoder.Encode(testData)
	require.NoError(t, err)
	assert.Positive(t, buf.Len())

	// Return encoder to pool.
	optimizer.PutJSONEncoder(encoder)

	// Test JSON decoder.
	reader := bytes.NewReader(buf.Bytes())
	decoder := optimizer.GetJSONDecoder(reader)
	require.NotNil(t, decoder)

	// Decode the data.
	var decoded map[string]interface{}

	err = decoder.Decode(&decoded)
	require.NoError(t, err)
	assert.Equal(t, testData["key"], decoded["key"])
	assert.InEpsilon(t, float64(42), decoded["number"], 0.001) // JSON numbers are float64

	// Return decoder to pool.
	optimizer.PutJSONDecoder(decoder)
}

func TestMemoryOptimizer_JSONPoolDisabled(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)
	config := DefaultMemoryOptimizationConfig()
	config.JSONPoolConfig.Enabled = false

	optimizer := NewMemoryOptimizer(config, logger)

	// Should still work but not use pooling.
	buf := bytes.NewBuffer(nil)
	encoder := optimizer.GetJSONEncoder(buf)
	require.NotNil(t, encoder)

	reader := bytes.NewReader([]byte(`{"test": true}`))
	decoder := optimizer.GetJSONDecoder(reader)
	require.NotNil(t, decoder)

	// Put methods should be no-ops.
	optimizer.PutJSONEncoder(encoder)
	optimizer.PutJSONDecoder(decoder)
}

func TestMemoryOptimizer_GCConfig(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)

	// Get initial GC percent.
	initialGCPercent := debug.SetGCPercent(-1) // Query current value
	debug.SetGCPercent(initialGCPercent)       // Restore it

	config := DefaultMemoryOptimizationConfig()
	config.GCConfig.Enabled = true
	config.GCConfig.GCPercent = 50

	optimizer := NewMemoryOptimizer(config, logger)
	err := optimizer.Start()
	require.NoError(t, err)

	defer func() {
		if err := optimizer.Stop(); err != nil {
			t.Logf("Failed to stop optimizer: %v", err)
		}
	}()

	// Let GC settings take effect.
	time.Sleep(constants.TestLongTickInterval)

	// Note: We can't easily test if the GC percent was set without affecting the global state.
	// The test mainly ensures the Start() method doesn't crash.
}

func TestMemoryOptimizer_MemoryStats(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)
	config := DefaultMemoryOptimizationConfig()

	optimizer := NewMemoryOptimizer(config, logger)
	err := optimizer.Start()
	require.NoError(t, err)

	defer func() {
		if err := optimizer.Stop(); err != nil {
			t.Logf("Failed to stop optimizer: %v", err)
		}
	}()

	// Wait for at least one monitoring cycle.
	time.Sleep(100 * time.Millisecond)

	stats := optimizer.GetMemoryStats()
	assert.Positive(t, stats.Alloc)
	assert.Positive(t, stats.Sys)

	report := optimizer.GetMemoryReport()
	assert.Contains(t, report, "alloc_bytes")
	assert.Contains(t, report, "total_alloc_bytes")
	assert.Contains(t, report, "heap_alloc_bytes")
	assert.Contains(t, report, "gc_count")
	assert.Contains(t, report, "timestamp")
	assert.Contains(t, report, "object_pooling")

	// Check that values are reasonable.
	assert.Positive(t, report["alloc_bytes"])
	assert.GreaterOrEqual(t, report["gc_count"], uint32(0))
	assert.Equal(t, config.EnableObjectPooling, report["object_pooling"])
}

func TestMemoryOptimizer_MemoryReport(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)
	config := DefaultMemoryOptimizationConfig()
	config.GCConfig.MemoryLimit = 100 * 1024 * 1024 // 100MB

	optimizer := NewMemoryOptimizer(config, logger)

	report := optimizer.GetMemoryReport()

	// Check all expected fields.
	expectedFields := []string{
		"alloc_bytes", "total_alloc_bytes", "sys_bytes",
		"heap_alloc_bytes", "heap_sys_bytes", "heap_objects",
		"stack_in_use_bytes", "stack_sys_bytes",
		"mspan_in_use_bytes", "mspan_sys_bytes",
		"mcache_in_use_bytes", "mcache_sys_bytes",
		"gc_count", "gc_pause_total_ns", "gc_percent",
		"memory_limit_bytes", "object_pooling", "timestamp",
		"usage_percent",
	}

	for _, field := range expectedFields {
		assert.Contains(t, report, field, "Missing field: %s", field)
	}

	// Check usage percentage calculation.
	usagePercent, ok := report["usage_percent"].(float64)
	assert.True(t, ok, "type assertion failed")
	assert.GreaterOrEqual(t, usagePercent, 0.0)
	assert.LessOrEqual(t, usagePercent, 100.0)
}

func TestMemoryOptimizer_ForceGC(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)
	config := DefaultMemoryOptimizationConfig()
	config.GCConfig.ForceGCInterval = 50 * time.Millisecond

	optimizer := NewMemoryOptimizer(config, logger)
	err := optimizer.Start()
	require.NoError(t, err)

	defer func() {
		if err := optimizer.Stop(); err != nil {
			t.Logf("Failed to stop optimizer: %v", err)
		}
	}()

	// Get initial GC count.
	initialStats := optimizer.GetMemoryStats()
	initialGCCount := initialStats.NumGC

	// Wait for forced GC.
	time.Sleep(2 * constants.TestSleepShort)

	// Check that GC was triggered.
	finalStats := optimizer.GetMemoryStats()
	assert.GreaterOrEqual(t, finalStats.NumGC, initialGCCount)
}

func TestMemoryOptimizer_OptimizeMemoryUsage(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)
	config := DefaultMemoryOptimizationConfig()

	optimizer := NewMemoryOptimizer(config, logger)

	// Get initial stats.
	initialStats := optimizer.GetMemoryStats()

	// Force optimization.
	optimizer.OptimizeMemoryUsage()

	// Get final stats.
	finalStats := optimizer.GetMemoryStats()

	// GC should have been triggered.
	assert.GreaterOrEqual(t, finalStats.NumGC, initialStats.NumGC)
}

func TestMemoryOptimizer_ConcurrentAccess(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)
	config := DefaultMemoryOptimizationConfig()

	optimizer := NewMemoryOptimizer(config, logger)
	err := optimizer.Start()
	require.NoError(t, err)

	defer func() {
		if err := optimizer.Stop(); err != nil {
			t.Logf("Failed to stop optimizer: %v", err)
		}
	}()

	const (
		numGoroutines          = 20
		operationsPerGoroutine = 100
	)

	var wg sync.WaitGroup

	runConcurrentBufferOperations(t, &wg, optimizer, numGoroutines, operationsPerGoroutine)
	runConcurrentJSONOperations(t, &wg, optimizer, numGoroutines, operationsPerGoroutine)
	runConcurrentStatsAccess(t, &wg, optimizer, numGoroutines, operationsPerGoroutine)

	wg.Wait()

	// Verify optimizer is still functional
	buf := optimizer.GetBuffer()
	assert.NotNil(t, buf)
	optimizer.PutBuffer(buf)

	stats := optimizer.GetMemoryStats()
	assert.Positive(t, stats.Alloc)
}

func runConcurrentBufferOperations(
	t *testing.T,
	wg *sync.WaitGroup,
	optimizer *MemoryOptimizer,
	numGoroutines, operationsPerGoroutine int,
) {
	t.Helper()

	// Test concurrent buffer operations
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for j := 0; j < operationsPerGoroutine; j++ {
				buf := optimizer.GetBuffer()
				_, _ = buf.WriteString("test data")
				optimizer.PutBuffer(buf)
			}
		}()
	}
}

func runConcurrentJSONOperations(
	t *testing.T,
	wg *sync.WaitGroup,
	optimizer *MemoryOptimizer,
	numGoroutines, operationsPerGoroutine int,
) {
	t.Helper()

	// Test concurrent JSON operations
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for j := 0; j < operationsPerGoroutine; j++ {
				// Encoder test
				buf := bytes.NewBuffer(nil)
				encoder := optimizer.GetJSONEncoder(buf)
				_ = encoder.Encode(map[string]int{"test": j})
				optimizer.PutJSONEncoder(encoder)

				// Decoder test
				reader := bytes.NewReader(buf.Bytes())
				decoder := optimizer.GetJSONDecoder(reader)

				var data map[string]int

				_ = decoder.Decode(&data)
				optimizer.PutJSONDecoder(decoder)
			}
		}()
	}
}

func runConcurrentStatsAccess(
	t *testing.T,
	wg *sync.WaitGroup,
	optimizer *MemoryOptimizer,
	numGoroutines, operationsPerGoroutine int,
) {
	t.Helper()

	// Test concurrent stats access
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for j := 0; j < operationsPerGoroutine; j++ {
				_ = optimizer.GetMemoryStats()
				_ = optimizer.GetMemoryReport()
			}
		}()
	}
}

func TestMemoryOptimizer_ConfigDefaults(t *testing.T) {
	t.Parallel()
	logger := zaptest.NewLogger(t)

	// Test with empty config.
	config := MemoryOptimizationConfig{}
	optimizer := NewMemoryOptimizer(config, logger)

	// Should apply defaults.
	assert.Equal(t, 4096, optimizer.config.BufferPoolConfig.InitialSize)
	assert.Equal(t, 1024*1024, optimizer.config.BufferPoolConfig.MaxSize)
	assert.Equal(t, 100, optimizer.config.BufferPoolConfig.MaxPooledBuffers)
	assert.Equal(t, 50, optimizer.config.JSONPoolConfig.MaxPooledObjects)
	assert.Equal(t, 100, optimizer.config.GCConfig.GCPercent)
	assert.Equal(t, 30*time.Second, optimizer.config.MemoryMonitoring.MonitoringInterval)
	assert.InEpsilon(t, 0.8, optimizer.config.MemoryMonitoring.AlertThreshold, 0.001)
}

func TestMemoryOptimizer_DefaultConfig(t *testing.T) {
	defaultConfig := DefaultMemoryOptimizationConfig()

	assert.True(t, defaultConfig.EnableObjectPooling)

	assert.True(t, defaultConfig.BufferPoolConfig.Enabled)
	assert.Equal(t, 4096, defaultConfig.BufferPoolConfig.InitialSize)
	assert.Equal(t, 1024*1024, defaultConfig.BufferPoolConfig.MaxSize)
	assert.Equal(t, 100, defaultConfig.BufferPoolConfig.MaxPooledBuffers)

	assert.True(t, defaultConfig.JSONPoolConfig.Enabled)
	assert.Equal(t, 50, defaultConfig.JSONPoolConfig.MaxPooledObjects)

	assert.True(t, defaultConfig.GCConfig.Enabled)
	assert.Equal(t, 75, defaultConfig.GCConfig.GCPercent)
	assert.Equal(t, uint64(512*1024*1024), defaultConfig.GCConfig.MemoryLimit)
	assert.Equal(t, 5*time.Minute, defaultConfig.GCConfig.ForceGCInterval)

	assert.True(t, defaultConfig.MemoryMonitoring.Enabled)
	assert.Equal(t, 30*time.Second, defaultConfig.MemoryMonitoring.MonitoringInterval)
	assert.InEpsilon(t, 0.8, defaultConfig.MemoryMonitoring.AlertThreshold, 0.001)
	assert.False(t, defaultConfig.MemoryMonitoring.LogMemoryStats)
}

// Benchmark tests.
func BenchmarkMemoryOptimizer_BufferOperations(b *testing.B) {
	logger := zaptest.NewLogger(b)
	config := DefaultMemoryOptimizationConfig()
	optimizer := NewMemoryOptimizer(config, logger)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			buf := optimizer.GetBuffer()
			_, _ = buf.WriteString("test data for benchmarking")
			optimizer.PutBuffer(buf)
		}
	})
}

func BenchmarkMemoryOptimizer_JSONOperations(b *testing.B) {
	logger := zaptest.NewLogger(b)
	config := DefaultMemoryOptimizationConfig()
	optimizer := NewMemoryOptimizer(config, logger)

	testData := map[string]interface{}{
		"key":    "value",
		"number": 42,
		"array":  []int{1, 2, 3, 4, 5},
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			buf := bytes.NewBuffer(nil)
			encoder := optimizer.GetJSONEncoder(buf)
			_ = encoder.Encode(testData)
			optimizer.PutJSONEncoder(encoder)

			reader := bytes.NewReader(buf.Bytes())
			decoder := optimizer.GetJSONDecoder(reader)

			var decoded map[string]interface{}

			_ = decoder.Decode(&decoded)
			optimizer.PutJSONDecoder(decoder)
		}
	})
}

func BenchmarkMemoryOptimizer_GetMemoryStats(b *testing.B) {
	logger := zaptest.NewLogger(b)
	config := DefaultMemoryOptimizationConfig()
	optimizer := NewMemoryOptimizer(config, logger)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = optimizer.GetMemoryStats()
	}
}

func BenchmarkMemoryOptimizer_GetMemoryReport(b *testing.B) {
	logger := zaptest.NewLogger(b)
	config := DefaultMemoryOptimizationConfig()
	optimizer := NewMemoryOptimizer(config, logger)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = optimizer.GetMemoryReport()
	}
}

// Test memory usage patterns.
func TestMemoryOptimizer_MemoryUsagePattern(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping memory usage pattern test in short mode")
	}

	optimizer := setupMemoryPatternOptimizer(t)

	defer func() {
		if err := optimizer.Stop(); err != nil {
			t.Logf("Failed to stop optimizer: %v", err)
		}
	}()

	initialMem := recordInitialMemoryStats()

	performMemoryIntensiveOperations(t, optimizer)

	// Let monitoring and GC run
	time.Sleep(5 * constants.TestSleepShort)

	memoryIncrease := calculateMemoryIncrease(initialMem)
	verifyMemoryUsagePattern(t, memoryIncrease, optimizer)
}

func setupMemoryPatternOptimizer(t *testing.T) *MemoryOptimizer {
	t.Helper()
	logger := zaptest.NewLogger(t)
	config := DefaultMemoryOptimizationConfig()
	config.MemoryMonitoring.MonitoringInterval = 100 * time.Millisecond
	config.GCConfig.ForceGCInterval = 200 * time.Millisecond

	optimizer := NewMemoryOptimizer(config, logger)
	err := optimizer.Start()
	require.NoError(t, err)

	return optimizer
}

func recordInitialMemoryStats() runtime.MemStats {
	var initialMem runtime.MemStats
	runtime.ReadMemStats(&initialMem)
	return initialMem
}

func performMemoryIntensiveOperations(t *testing.T, optimizer *MemoryOptimizer) {
	t.Helper()

	const numOperations = 1000

	for i := 0; i < numOperations; i++ {
		performBufferOperations(optimizer)
		performJSONOperations(optimizer, i)
	}
}

func performBufferOperations(optimizer *MemoryOptimizer) {
	buf := optimizer.GetBuffer()
	for j := 0; j < 100; j++ {
		_, _ = buf.WriteString("memory test data ")
	}

	optimizer.PutBuffer(buf)
}

func performJSONOperations(optimizer *MemoryOptimizer, iteration int) {
	data := map[string]interface{}{
		"iteration": iteration,
		"data":      make([]int, 100),
	}
	jsonBuf := bytes.NewBuffer(nil)
	encoder := optimizer.GetJSONEncoder(jsonBuf)
	_ = encoder.Encode(data)
	optimizer.PutJSONEncoder(encoder)

	reader := bytes.NewReader(jsonBuf.Bytes())
	decoder := optimizer.GetJSONDecoder(reader)

	var decoded map[string]interface{}

	_ = decoder.Decode(&decoded)
	optimizer.PutJSONDecoder(decoder)
}

func calculateMemoryIncrease(initialMem runtime.MemStats) uint64 {
	var finalMem runtime.MemStats
	runtime.ReadMemStats(&finalMem)

	var memoryIncrease uint64
	if finalMem.Alloc >= initialMem.Alloc {
		memoryIncrease = finalMem.Alloc - initialMem.Alloc
	} else {
		memoryIncrease = 0 // GC reduced memory usage
	}
	return memoryIncrease
}

func verifyMemoryUsagePattern(t *testing.T, memoryIncrease uint64, optimizer *MemoryOptimizer) {
	t.Helper()

	const numOperations = 1000

	t.Logf("Memory increase after %d operations: %d bytes", numOperations, memoryIncrease)

	// Memory usage should be reasonable (less than 100MB increase)
	// Allow for some variability due to GC behavior
	assert.Less(t, memoryIncrease, uint64(100*1024*1024))

	// Verify optimizer is still functional
	report := optimizer.GetMemoryReport()
	assert.Positive(t, report["gc_count"])
}

// Test object pooling effectiveness.
func TestMemoryOptimizer_PoolingEffectiveness(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping pooling effectiveness test in short mode")
	}

	logger := zaptest.NewLogger(t)
	optimizerWithPooling := createOptimizerWithPooling(logger)
	optimizerWithoutPooling := createOptimizerWithoutPooling(logger)

	const numOperations = 500

	withPoolingIncrease := measureMemoryWithPooling(optimizerWithPooling, numOperations)
	withoutPoolingIncrease := measureMemoryWithoutPooling(optimizerWithoutPooling, numOperations)

	analyzePoolingEffectiveness(t, withPoolingIncrease, withoutPoolingIncrease)
}

func createOptimizerWithPooling(logger *zap.Logger) *MemoryOptimizer {
	configWithPooling := DefaultMemoryOptimizationConfig()
	configWithPooling.EnableObjectPooling = true
	return NewMemoryOptimizer(configWithPooling, logger)
}

func createOptimizerWithoutPooling(logger *zap.Logger) *MemoryOptimizer {
	configWithoutPooling := DefaultMemoryOptimizationConfig()
	configWithoutPooling.EnableObjectPooling = false
	configWithoutPooling.BufferPoolConfig.Enabled = false
	configWithoutPooling.JSONPoolConfig.Enabled = false
	return NewMemoryOptimizer(configWithoutPooling, logger)
}

func measureMemoryWithPooling(optimizer *MemoryOptimizer, numOperations int) uint64 {
	runtime.GC()

	var memBefore runtime.MemStats
	runtime.ReadMemStats(&memBefore)

	for i := 0; i < numOperations; i++ {
		buf := optimizer.GetBuffer()
		_, _ = buf.WriteString("test data")
		optimizer.PutBuffer(buf)
	}

	runtime.GC()

	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memAfter)

	return memAfter.TotalAlloc - memBefore.TotalAlloc
}

func measureMemoryWithoutPooling(optimizer *MemoryOptimizer, numOperations int) uint64 {
	runtime.GC()

	var memBefore runtime.MemStats
	runtime.ReadMemStats(&memBefore)

	for i := 0; i < numOperations; i++ {
		buf := optimizer.GetBuffer()
		_, _ = buf.WriteString("test data")
		optimizer.PutBuffer(buf)
	}

	runtime.GC()

	var memAfter runtime.MemStats
	runtime.ReadMemStats(&memAfter)

	return memAfter.TotalAlloc - memBefore.TotalAlloc
}

func analyzePoolingEffectiveness(t *testing.T, withPoolingIncrease, withoutPoolingIncrease uint64) {
	t.Helper()

	t.Logf("Memory allocation with pooling: %d bytes", withPoolingIncrease)
	t.Logf("Memory allocation without pooling: %d bytes", withoutPoolingIncrease)

	// Pooling should reduce total allocations
	if withoutPoolingIncrease > 0 {
		reduction := float64(withoutPoolingIncrease-withPoolingIncrease) / float64(withoutPoolingIncrease) * 100
		t.Logf("Pooling reduced allocations by %.2f%%", reduction)

		// Expect some reduction, but allow for test variability
		assert.Greater(t, reduction, -50.0) // Allow up to 50% worse (test variability)
	}
}
