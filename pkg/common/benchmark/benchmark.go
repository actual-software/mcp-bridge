// Package benchmark provides common benchmarking utilities
package benchmark

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

const (
	// resultChannelMultiplier determines channel buffer size relative to concurrency.
	resultChannelMultiplier = 100
	// warmupWorkerDivisor for calculating warmup workers from concurrency.
	warmupWorkerDivisor = 10
	// maxWarmupWorkers limits the number of warmup workers.
	maxWarmupWorkers = 10
	// warmupSleepMilliseconds between warmup operations.
	warmupSleepMilliseconds = 100
	// percentageMultiplier for percentage calculations.
	percentageMultiplier = 100
	// bytesToMBDivisor for converting bytes to megabytes.
	bytesToMBDivisor = 1024
)

// Config defines benchmark configuration.
type Config struct {
	Name         string
	Duration     time.Duration
	Concurrency  int
	RPS          int // Requests per second (0 = unlimited)
	WarmupTime   time.Duration
	CooldownTime time.Duration
}

// Result contains benchmark results.
type Result struct {
	Name          string
	Duration      time.Duration
	TotalRequests int64
	SuccessCount  int64
	ErrorCount    int64

	// Latency metrics (in nanoseconds)
	MinLatency int64
	MaxLatency int64
	AvgLatency int64
	P50Latency int64
	P95Latency int64
	P99Latency int64

	// Throughput metrics
	RequestsPerSec float64
	BytesPerSec    float64

	// Resource metrics
	AvgCPU      float64
	MaxMemoryMB int64

	// Error details
	ErrorTypes map[string]int64
}

// RequestResult represents a single request result.
type RequestResult struct {
	Success   bool
	Latency   time.Duration
	BytesRead int64
	Error     error
	Timestamp time.Time
}

// WorkerFunc is the function that performs the actual work being benchmarked.
type WorkerFunc func(ctx context.Context) *RequestResult

// Runner executes benchmarks.
type Runner struct {
	config Config
	worker WorkerFunc
	logger *zap.Logger

	// Metrics collection
	results   chan *RequestResult
	errors    map[string]*int64
	latencies []int64

	// Control
	ctx       context.Context
	cancel    context.CancelFunc
	waitGroup sync.WaitGroup
}

// NewRunner creates a new benchmark runner.
func NewRunner(config Config, worker WorkerFunc, logger *zap.Logger) *Runner {
	ctx, cancel := context.WithCancel(context.Background())

	return &Runner{
		config:    config,
		worker:    worker,
		logger:    logger,
		results:   make(chan *RequestResult, config.Concurrency*resultChannelMultiplier),
		errors:    make(map[string]*int64),
		latencies: make([]int64, 0, defaultLatencySliceCapacity),
		ctx:       ctx,
		cancel:    cancel,
		waitGroup: sync.WaitGroup{}, // Zero value WaitGroup for goroutine synchronization
	}
}

// Run executes the benchmark.
func (r *Runner) Run() (*Result, error) {
	r.logger.Info("Starting benchmark",
		zap.String("name", r.config.Name),
		zap.Duration("duration", r.config.Duration),
		zap.Int("concurrency", r.config.Concurrency),
		zap.Int("target_rps", r.config.RPS))

	// Warmup phase
	if r.config.WarmupTime > 0 {
		r.logger.Info("Starting warmup phase", zap.Duration("warmup_time", r.config.WarmupTime))
		r.runWarmup()
	}

	// Reset metrics after warmup
	r.results = make(chan *RequestResult, r.config.Concurrency*resultChannelMultiplier)

	// Start result collector
	resultsDone := make(chan struct{})

	go func() {
		r.collectResults()
		close(resultsDone)
	}()

	// Run benchmark
	startTime := time.Now()

	benchCtx, benchCancel := context.WithTimeout(r.ctx, r.config.Duration)
	defer benchCancel()

	// Start workers
	for range r.config.Concurrency {
		r.waitGroup.Add(1)

		if r.config.RPS > 0 {
			// Rate-limited worker
			go r.runRateLimitedWorker(benchCtx, r.config.RPS/r.config.Concurrency)
		} else {
			// Unlimited worker
			go r.runWorker(benchCtx)
		}
	}

	// Wait for workers to complete
	r.waitGroup.Wait()
	close(r.results)

	// Wait for results collection to complete
	<-resultsDone

	duration := time.Since(startTime)

	// Cooldown phase
	if r.config.CooldownTime > 0 {
		r.logger.Info("Starting cooldown phase", zap.Duration("cooldown_time", r.config.CooldownTime))
		time.Sleep(r.config.CooldownTime)
	}

	// Calculate and return results
	return r.calculateResults(duration), nil
}

// Stop gracefully stops the benchmark.
func (r *Runner) Stop() {
	r.cancel()
	r.waitGroup.Wait()
}

func (r *Runner) runWarmup() {
	warmupCtx, cancel := context.WithTimeout(r.ctx, r.config.WarmupTime)
	defer cancel()

	// Run a few workers for warmup
	warmupWorkers := minInt(r.config.Concurrency/warmupWorkerDivisor+1, maxWarmupWorkers)

	var waitGroup sync.WaitGroup

	for range warmupWorkers {
		waitGroup.Add(1)

		go func() {
			defer waitGroup.Done()

			for {
				select {
				case <-warmupCtx.Done():
					return
				default:
					r.worker(warmupCtx)
					time.Sleep(warmupSleepMilliseconds * time.Millisecond)
				}
			}
		}()
	}

	waitGroup.Wait()
}

func (r *Runner) runWorker(ctx context.Context) {
	defer r.waitGroup.Done()

	for {
		select {
		case <-ctx.Done():
			return
		default:
			result := r.worker(ctx)
			select {
			case r.results <- result:
			case <-ctx.Done():
				return
			}
		}
	}
}

func (r *Runner) runRateLimitedWorker(ctx context.Context, rps int) {
	defer r.waitGroup.Done()

	if rps <= 0 {
		r.runWorker(ctx)

		return
	}

	ticker := time.NewTicker(time.Second / time.Duration(rps))
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			result := r.worker(ctx)
			select {
			case r.results <- result:
			case <-ctx.Done():
				return
			}
		}
	}
}

func (r *Runner) collectResults() {
	for result := range r.results {
		if result.Success {
			atomic.AddInt64(&successCount, 1)
		} else {
			atomic.AddInt64(&errorCount, 1)

			if result.Error != nil {
				r.recordError(result.Error.Error())
			}
		}

		// Store latency for percentile calculation
		r.latencies = append(r.latencies, result.Latency.Nanoseconds())

		atomic.AddInt64(&totalRequests, 1)
		atomic.AddInt64(&totalBytes, result.BytesRead)

		// Update min/max latency
		latencyNs := result.Latency.Nanoseconds()
		updateMinMax(&minLatency, &maxLatency, latencyNs)
	}
}

func (r *Runner) recordError(errStr string) {
	if _, exists := r.errors[errStr]; !exists {
		var count int64

		r.errors[errStr] = &count
	}

	atomic.AddInt64(r.errors[errStr], 1)
}

func (r *Runner) calculateResults(duration time.Duration) *Result {
	result := &Result{
		Name:           r.config.Name,
		Duration:       duration,
		TotalRequests:  atomic.LoadInt64(&totalRequests),
		SuccessCount:   atomic.LoadInt64(&successCount),
		ErrorCount:     atomic.LoadInt64(&errorCount),
		MinLatency:     atomic.LoadInt64(&minLatency),
		MaxLatency:     atomic.LoadInt64(&maxLatency),
		AvgLatency:     0, // Will be calculated below
		P50Latency:     0, // Will be calculated below
		P95Latency:     0, // Will be calculated below
		P99Latency:     0, // Will be calculated below
		RequestsPerSec: 0, // Will be calculated below
		BytesPerSec:    0, // Will be calculated below
		AvgCPU:         0, // Will be calculated below
		MaxMemoryMB:    0, // Will be calculated below
		ErrorTypes:     make(map[string]int64),
	}

	// Calculate throughput
	if duration > 0 {
		result.RequestsPerSec = float64(result.TotalRequests) / duration.Seconds()
		result.BytesPerSec = float64(atomic.LoadInt64(&totalBytes)) / duration.Seconds()
	}

	// Calculate latency percentiles
	if len(r.latencies) > 0 {
		result.AvgLatency = calculateAverage(r.latencies)
		result.P50Latency = calculatePercentile(r.latencies, p50Percentile)
		result.P95Latency = calculatePercentile(r.latencies, p95Percentile)
		result.P99Latency = calculatePercentile(r.latencies, p99Percentile)
	}

	// Copy error counts
	for errStr, count := range r.errors {
		result.ErrorTypes[errStr] = atomic.LoadInt64(count)
	}

	return result
}

// Global metrics (using atomics for thread safety).
var (
	totalRequests int64
	successCount  int64
	errorCount    int64
	totalBytes    int64
	minLatency    int64 = 1<<63 - 1
	maxLatency    int64
)

const (
	// defaultLatencySliceCapacity is the default capacity for storing latency measurements.
	defaultLatencySliceCapacity = 1000000
	// p50Percentile is the 50th percentile.
	p50Percentile = 50
	// p95Percentile is the 95th percentile.
	p95Percentile = 95
	// p99Percentile is the 99th percentile.
	p99Percentile = 99
	// percentToFraction converts percentile to fraction.
	percentToFraction = 100.0
)

// Helper functions.
func minInt(a, b int) int {
	if a < b {
		return a
	}

	return b
}

func updateMinMax(minVal, maxVal *int64, value int64) {
	for {
		oldMin := atomic.LoadInt64(minVal)
		if value >= oldMin {
			break
		}

		if atomic.CompareAndSwapInt64(minVal, oldMin, value) {
			break
		}
	}

	for {
		oldMax := atomic.LoadInt64(maxVal)
		if value <= oldMax {
			break
		}

		if atomic.CompareAndSwapInt64(maxVal, oldMax, value) {
			break
		}
	}
}

func calculateAverage(values []int64) int64 {
	if len(values) == 0 {
		return 0
	}

	var sum int64
	for _, v := range values {
		sum += v
	}

	return sum / int64(len(values))
}

func calculatePercentile(values []int64, percentile float64) int64 {
	if len(values) == 0 {
		return 0
	}

	// Note: This is a simple implementation. For production use,
	// consider using a more efficient algorithm like t-digest
	index := int(float64(len(values)) * percentile / percentToFraction)
	if index >= len(values) {
		index = len(values) - 1
	}

	return values[index]
}

// Format returns a formatted string representation of the results.
func (r *Result) Format() string {
	var builder strings.Builder

	builder.WriteString(fmt.Sprintf("Benchmark: %s\n", r.Name))
	builder.WriteString(fmt.Sprintf("Duration: %v\n", r.Duration))
	builder.WriteString(fmt.Sprintf("Total Requests: %d\n", r.TotalRequests))
	builder.WriteString(fmt.Sprintf("Success: %d (%.2f%%)\n",
		r.SuccessCount, float64(r.SuccessCount)/float64(r.TotalRequests)*percentageMultiplier))
	builder.WriteString(fmt.Sprintf("Errors: %d (%.2f%%)\n",
		r.ErrorCount, float64(r.ErrorCount)/float64(r.TotalRequests)*percentageMultiplier))
	builder.WriteString("\n")

	builder.WriteString("Latency:\n")
	builder.WriteString(fmt.Sprintf("  Min: %v\n", time.Duration(r.MinLatency)))
	builder.WriteString(fmt.Sprintf("  Avg: %v\n", time.Duration(r.AvgLatency)))
	builder.WriteString(fmt.Sprintf("  P50: %v\n", time.Duration(r.P50Latency)))
	builder.WriteString(fmt.Sprintf("  P95: %v\n", time.Duration(r.P95Latency)))
	builder.WriteString(fmt.Sprintf("  P99: %v\n", time.Duration(r.P99Latency)))
	builder.WriteString(fmt.Sprintf("  Max: %v\n", time.Duration(r.MaxLatency)))
	builder.WriteString("\n")

	builder.WriteString("Throughput:\n")
	builder.WriteString(fmt.Sprintf("  Requests/sec: %.2f\n", r.RequestsPerSec))
	builder.WriteString(fmt.Sprintf("  Bytes/sec: %.2f MB\n", r.BytesPerSec/bytesToMBDivisor/bytesToMBDivisor))

	if len(r.ErrorTypes) > 0 {
		builder.WriteString("\nError Types:\n")

		for errType, count := range r.ErrorTypes {
			builder.WriteString(fmt.Sprintf("  %s: %d\n", errType, count))
		}
	}

	return builder.String()
}
