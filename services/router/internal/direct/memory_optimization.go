package direct

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"runtime"
	"runtime/debug"
	"sync"
	"time"

	"github.com/poiley/mcp-bridge/services/router/internal/constants"
	"go.uber.org/zap"
)

// Memory optimization constants.
const (
	DefaultGCPercent      = 75
	DefaultAlertThreshold = 0.8
)

// MemoryOptimizationConfig configures memory usage optimization.
type MemoryOptimizationConfig struct {
	// Enable object pooling for commonly allocated objects.
	EnableObjectPooling bool `mapstructure:"enable_object_pooling" yaml:"enable_object_pooling"`

	// Buffer pool configuration.
	BufferPoolConfig BufferPoolConfig `mapstructure:"buffer_pool" yaml:"buffer_pool"`

	// JSON encoder/decoder pool configuration.
	JSONPoolConfig JSONPoolConfig `mapstructure:"json_pool" yaml:"json_pool"`

	// Garbage collection tuning.
	GCConfig GCConfig `mapstructure:"gc_config" yaml:"gc_config"`

	// Memory monitoring and alerting.
	MemoryMonitoring MemoryMonitoringConfig `mapstructure:"memory_monitoring" yaml:"memory_monitoring"`
}

// BufferPoolConfig configures buffer object pooling.
type BufferPoolConfig struct {
	// Enable buffer pooling.
	Enabled bool `mapstructure:"enabled" yaml:"enabled"`

	// Initial buffer size for new buffers.
	InitialSize int `mapstructure:"initial_size" yaml:"initial_size"`

	// Maximum buffer size to keep in pool (larger buffers are discarded).
	MaxSize int `mapstructure:"max_size" yaml:"max_size"`

	// Maximum number of buffers to keep in pool.
	MaxPooledBuffers int `mapstructure:"max_pooled_buffers" yaml:"max_pooled_buffers"`
}

// JSONPoolConfig configures JSON encoder/decoder pooling.
type JSONPoolConfig struct {
	// Enable JSON encoder/decoder pooling.
	Enabled bool `mapstructure:"enabled" yaml:"enabled"`

	// Maximum number of encoders/decoders to pool.
	MaxPooledObjects int `mapstructure:"max_pooled_objects" yaml:"max_pooled_objects"`
}

// GCConfig configures garbage collection optimization.
type GCConfig struct {
	// Enable GC tuning.
	Enabled bool `mapstructure:"enabled" yaml:"enabled"`

	// Target percentage of heap growth before GC (default: 100).
	GCPercent int `mapstructure:"gc_percent" yaml:"gc_percent"`

	// Memory limit in bytes (0 = no limit).
	MemoryLimit uint64 `mapstructure:"memory_limit_bytes" yaml:"memory_limit_bytes"`

	// Force GC interval (0 = disabled).
	ForceGCInterval time.Duration `mapstructure:"force_gc_interval" yaml:"force_gc_interval"`
}

// MemoryMonitoringConfig configures memory usage monitoring.
type MemoryMonitoringConfig struct {
	// Enable memory monitoring.
	Enabled bool `mapstructure:"enabled" yaml:"enabled"`

	// Monitoring interval.
	MonitoringInterval time.Duration `mapstructure:"monitoring_interval" yaml:"monitoring_interval"`

	// Memory usage alert threshold (percentage of limit).
	AlertThreshold float64 `mapstructure:"alert_threshold" yaml:"alert_threshold"`

	// Log memory stats.
	LogMemoryStats bool `mapstructure:"log_memory_stats" yaml:"log_memory_stats"`
}

// MemoryOptimizer manages memory optimization for the direct client system.
type MemoryOptimizer struct {
	config MemoryOptimizationConfig
	logger *zap.Logger

	// Object pools.
	bufferPool  *sync.Pool
	encoderPool *sync.Pool
	decoderPool *sync.Pool

	// Monitoring.
	memoryStats     runtime.MemStats
	statsMu         sync.RWMutex
	shutdown        chan struct{}
	wg              sync.WaitGroup
	lastGCTime      time.Time
	lastMemoryAlert time.Time
}

// NewMemoryOptimizer creates a new memory optimizer.
func NewMemoryOptimizer(config MemoryOptimizationConfig, logger *zap.Logger) *MemoryOptimizer {
	// Set defaults.
	if config.BufferPoolConfig.InitialSize == 0 {
		config.BufferPoolConfig.InitialSize = 4096 // 4KB
	}

	if config.BufferPoolConfig.MaxSize == 0 {
		config.BufferPoolConfig.MaxSize = defaultBufferSize * defaultBufferSize // 1MB
	}

	if config.BufferPoolConfig.MaxPooledBuffers == 0 {
		config.BufferPoolConfig.MaxPooledBuffers = 100
	}

	if config.JSONPoolConfig.MaxPooledObjects == 0 {
		config.JSONPoolConfig.MaxPooledObjects = constants.DefaultMaxPooledObjects
	}

	if config.GCConfig.GCPercent == 0 {
		config.GCConfig.GCPercent = 100
	}

	if config.MemoryMonitoring.MonitoringInterval == 0 {
		config.MemoryMonitoring.MonitoringInterval = defaultTimeoutSeconds * time.Second
	}

	if config.MemoryMonitoring.AlertThreshold == 0 {
		config.MemoryMonitoring.AlertThreshold = 0.8 // 80%
	}

	optimizer := &MemoryOptimizer{
		config:   config,
		logger:   logger.With(zap.String("component", "memory_optimizer")),
		shutdown: make(chan struct{}),
	}

	// Initialize object pools if enabled.
	if config.EnableObjectPooling {
		optimizer.initializePools()
	}

	return optimizer
}

// Start begins memory optimization.
func (mo *MemoryOptimizer) Start() error {
	mo.logger.Info("starting memory optimizer",
		zap.Bool("object_pooling", mo.config.EnableObjectPooling),
		zap.Bool("gc_tuning", mo.config.GCConfig.Enabled),
		zap.Bool("memory_monitoring", mo.config.MemoryMonitoring.Enabled))

	// Apply GC configuration.
	if mo.config.GCConfig.Enabled {
		mo.applyGCConfig()
	}

	// Initialize memory stats.
	mo.statsMu.Lock()
	runtime.ReadMemStats(&mo.memoryStats)
	mo.statsMu.Unlock()

	// Start memory monitoring.
	if mo.config.MemoryMonitoring.Enabled {
		mo.wg.Add(1)

		go mo.memoryMonitoringLoop()
	}

	// Start forced GC if configured.
	if mo.config.GCConfig.ForceGCInterval > 0 {
		mo.wg.Add(1)

		go mo.forceGCLoop()
	}

	return nil
}

// Stop shuts down memory optimization.
func (mo *MemoryOptimizer) Stop() error {
	mo.logger.Info("stopping memory optimizer")

	// Signal shutdown (protect against double-close).
	select {
	case <-mo.shutdown:
		// Channel already closed.
	default:
		close(mo.shutdown)
	}

	mo.wg.Wait()

	mo.logger.Info("memory optimizer stopped")

	return nil
}

// initializePools creates object pools for reusable objects.
func (mo *MemoryOptimizer) initializePools() {
	// Buffer pool.
	if mo.config.BufferPoolConfig.Enabled {
		mo.bufferPool = &sync.Pool{
			New: func() interface{} {
				return bytes.NewBuffer(make([]byte, 0, mo.config.BufferPoolConfig.InitialSize))
			},
		}
	}

	// JSON encoder pool.
	if mo.config.JSONPoolConfig.Enabled {
		mo.encoderPool = &sync.Pool{
			New: func() interface{} {
				var buf bytes.Buffer

				return json.NewEncoder(&buf)
			},
		}

		// JSON decoder pool.
		mo.decoderPool = &sync.Pool{
			New: func() interface{} {
				return json.NewDecoder(bytes.NewReader(nil))
			},
		}
	}

	mo.logger.Info("object pools initialized",
		zap.Bool("buffer_pool", mo.config.BufferPoolConfig.Enabled),
		zap.Bool("json_pools", mo.config.JSONPoolConfig.Enabled))
}

// GetBuffer retrieves a buffer from the pool or creates a new one.
func (mo *MemoryOptimizer) GetBuffer() *bytes.Buffer {
	if mo.bufferPool != nil && mo.config.BufferPoolConfig.Enabled {
		buf, ok := mo.bufferPool.Get().(*bytes.Buffer)
		if !ok {
			return bytes.NewBuffer(make([]byte, 0, mo.config.BufferPoolConfig.InitialSize))
		}

		buf.Reset()

		return buf
	}

	return bytes.NewBuffer(make([]byte, 0, mo.config.BufferPoolConfig.InitialSize))
}

// PutBuffer returns a buffer to the pool.
func (mo *MemoryOptimizer) PutBuffer(buf *bytes.Buffer) {
	if mo.bufferPool != nil && mo.config.BufferPoolConfig.Enabled {
		// Don't pool buffers that are too large.
		if buf.Cap() > mo.config.BufferPoolConfig.MaxSize {
			return
		}

		mo.bufferPool.Put(buf)
	}
}

// GetJSONEncoder retrieves a JSON encoder from the pool or creates a new one.
func (mo *MemoryOptimizer) GetJSONEncoder(w io.Writer) *json.Encoder {
	if mo.encoderPool != nil && mo.config.JSONPoolConfig.Enabled {
		encoder, ok := mo.encoderPool.Get().(*json.Encoder)
		if !ok {
			return json.NewEncoder(w)
		}
		// Reset encoder to use new writer.
		*encoder = *json.NewEncoder(w)

		return encoder
	}

	return json.NewEncoder(w)
}

// PutJSONEncoder returns a JSON encoder to the pool.
func (mo *MemoryOptimizer) PutJSONEncoder(encoder *json.Encoder) {
	if mo.encoderPool != nil && mo.config.JSONPoolConfig.Enabled {
		mo.encoderPool.Put(encoder)
	}
}

// GetJSONDecoder retrieves a JSON decoder from the pool or creates a new one.
func (mo *MemoryOptimizer) GetJSONDecoder(reader *bytes.Reader) *json.Decoder {
	if mo.decoderPool != nil && mo.config.JSONPoolConfig.Enabled {
		decoder, ok := mo.decoderPool.Get().(*json.Decoder)
		if !ok {
			return json.NewDecoder(reader)
		}
		// Reset decoder to use new reader.
		*decoder = *json.NewDecoder(reader)

		return decoder
	}

	return json.NewDecoder(reader)
}

// PutJSONDecoder returns a JSON decoder to the pool.
func (mo *MemoryOptimizer) PutJSONDecoder(decoder *json.Decoder) {
	if mo.decoderPool != nil && mo.config.JSONPoolConfig.Enabled {
		mo.decoderPool.Put(decoder)
	}
}

// applyGCConfig applies garbage collection configuration.
func (mo *MemoryOptimizer) applyGCConfig() {
	oldPercent := debug.SetGCPercent(mo.config.GCConfig.GCPercent)

	if mo.config.GCConfig.MemoryLimit > 0 {
		// Safely convert uint64 to int64 with explicit bounds checking
		const maxValidMemoryLimit = math.MaxInt64
		if mo.config.GCConfig.MemoryLimit <= maxValidMemoryLimit {
			// Split the conversion to make it explicit for the linter
			memoryLimitValue := mo.config.GCConfig.MemoryLimit
			if memoryLimitValue <= math.MaxInt64 {
				memoryLimitInt64 := int64(memoryLimitValue)
				debug.SetMemoryLimit(memoryLimitInt64)
			}
		} else {
			mo.logger.Warn("memory limit too large, using maximum safe value",
				zap.Uint64("requested", mo.config.GCConfig.MemoryLimit),
				zap.Int64("using", math.MaxInt64))
			debug.SetMemoryLimit(math.MaxInt64)
		}
	}

	mo.logger.Info("applied GC configuration",
		zap.Int("old_gc_percent", oldPercent),
		zap.Int("new_gc_percent", mo.config.GCConfig.GCPercent),
		zap.Uint64("memory_limit_bytes", mo.config.GCConfig.MemoryLimit))
}

// memoryMonitoringLoop monitors memory usage and alerts.
func (mo *MemoryOptimizer) memoryMonitoringLoop() {
	defer mo.wg.Done()

	ticker := time.NewTicker(mo.config.MemoryMonitoring.MonitoringInterval)
	defer ticker.Stop()

	for {
		select {
		case <-mo.shutdown:
			return
		case <-ticker.C:
			mo.monitorMemory()
		}
	}
}

// monitorMemory checks current memory usage and generates alerts.
func (mo *MemoryOptimizer) monitorMemory() {
	mo.statsMu.Lock()
	runtime.ReadMemStats(&mo.memoryStats)
	stats := mo.memoryStats // Copy for logging
	mo.statsMu.Unlock()

	// Calculate memory usage percentage.
	var usagePercent float64
	if mo.config.GCConfig.MemoryLimit > 0 {
		usagePercent = float64(stats.Alloc) / float64(mo.config.GCConfig.MemoryLimit)
	}

	// Log memory stats if enabled.
	if mo.config.MemoryMonitoring.LogMemoryStats {
		mo.logger.Debug("memory stats",
			zap.Uint64("alloc_bytes", stats.Alloc),
			zap.Uint64("total_alloc_bytes", stats.TotalAlloc),
			zap.Uint64("sys_bytes", stats.Sys),
			zap.Uint32("num_gc", stats.NumGC),
			zap.Uint64("gc_pause_total_ns", stats.PauseTotalNs),
			zap.Float64("usage_percent", usagePercent*PercentageBase))
	}

	// Check for memory alerts.
	if usagePercent > mo.config.MemoryMonitoring.AlertThreshold {
		// Throttle alerts to avoid spam.
		if time.Since(mo.lastMemoryAlert) > 5*time.Minute {
			mo.logger.Warn("high memory usage detected",
				zap.Float64("usage_percent", usagePercent*PercentageBase),
				zap.Float64("threshold_percent", mo.config.MemoryMonitoring.AlertThreshold*PercentageBase),
				zap.Uint64("alloc_bytes", stats.Alloc),
				zap.Uint64("memory_limit_bytes", mo.config.GCConfig.MemoryLimit))
			mo.lastMemoryAlert = time.Now()
		}
	}
}

// forceGCLoop periodically forces garbage collection.
func (mo *MemoryOptimizer) forceGCLoop() {
	defer mo.wg.Done()

	ticker := time.NewTicker(mo.config.GCConfig.ForceGCInterval)
	defer ticker.Stop()

	for {
		select {
		case <-mo.shutdown:
			return
		case <-ticker.C:
			mo.forceGC()
		}
	}
}

// forceGC forces garbage collection and logs statistics.
func (mo *MemoryOptimizer) forceGC() {
	start := time.Now()

	var beforeStats runtime.MemStats
	runtime.ReadMemStats(&beforeStats)

	runtime.GC()

	var afterStats runtime.MemStats
	runtime.ReadMemStats(&afterStats)

	duration := time.Since(start)
	mo.lastGCTime = start

	mo.logger.Debug("forced garbage collection",
		zap.Duration("duration", duration),
		zap.Uint64("alloc_before", beforeStats.Alloc),
		zap.Uint64("alloc_after", afterStats.Alloc),
		zap.Uint64("freed_bytes", beforeStats.Alloc-afterStats.Alloc),
		zap.Uint32("gc_count", afterStats.NumGC))
}

// GetMemoryStats returns current memory statistics.
func (mo *MemoryOptimizer) GetMemoryStats() runtime.MemStats {
	mo.statsMu.RLock()
	defer mo.statsMu.RUnlock()

	return mo.memoryStats
}

// GetMemoryReport generates a comprehensive memory usage report.
func (mo *MemoryOptimizer) GetMemoryReport() map[string]interface{} {
	stats := mo.GetMemoryStats()

	report := map[string]interface{}{
		"alloc_bytes":         stats.Alloc,
		"total_alloc_bytes":   stats.TotalAlloc,
		"sys_bytes":           stats.Sys,
		"heap_alloc_bytes":    stats.HeapAlloc,
		"heap_sys_bytes":      stats.HeapSys,
		"heap_objects":        stats.HeapObjects,
		"stack_in_use_bytes":  stats.StackInuse,
		"stack_sys_bytes":     stats.StackSys,
		"mspan_in_use_bytes":  stats.MSpanInuse,
		"mspan_sys_bytes":     stats.MSpanSys,
		"mcache_in_use_bytes": stats.MCacheInuse,
		"mcache_sys_bytes":    stats.MCacheSys,
		"gc_count":            stats.NumGC,
		"gc_pause_total_ns":   stats.PauseTotalNs,
		"gc_percent":          mo.config.GCConfig.GCPercent,
		"memory_limit_bytes":  mo.config.GCConfig.MemoryLimit,
		"last_gc_time":        mo.lastGCTime,
		"object_pooling":      mo.config.EnableObjectPooling,
		"timestamp":           time.Now(),
	}

	// Add usage percentage if memory limit is set.
	if mo.config.GCConfig.MemoryLimit > 0 {
		report["usage_percent"] = float64(stats.Alloc) / float64(mo.config.GCConfig.MemoryLimit) * PercentageBase
	}

	return report
}

// OptimizeMemoryUsage performs immediate memory optimization.
func (mo *MemoryOptimizer) OptimizeMemoryUsage() {
	mo.logger.Info("performing memory optimization")

	// Force garbage collection.
	mo.forceGC()

	// Return unused memory to OS.
	debug.FreeOSMemory()

	mo.logger.Info("memory optimization completed")
}

// DefaultMemoryOptimizationConfig returns sensible defaults for memory optimization.
func DefaultMemoryOptimizationConfig() MemoryOptimizationConfig {
	return MemoryOptimizationConfig{
		EnableObjectPooling: true,
		BufferPoolConfig: BufferPoolConfig{
			Enabled:          true,
			InitialSize:      DefaultPageSize,                       // 4KB
			MaxSize:          defaultBufferSize * defaultBufferSize, // 1MB
			MaxPooledBuffers: DefaultTraceLimit,
		},
		JSONPoolConfig: JSONPoolConfig{
			Enabled:          true,
			MaxPooledObjects: constants.DefaultMaxPooledObjects,
		},
		GCConfig: GCConfig{
			Enabled:         true,
			GCPercent:       DefaultGCPercent,                            // More frequent GC for lower memory usage
			MemoryLimit:     512 * defaultBufferSize * defaultBufferSize, // 512MB limit
			ForceGCInterval: constants.CleanupTickerInterval,
		},
		MemoryMonitoring: MemoryMonitoringConfig{
			Enabled:            true,
			MonitoringInterval: defaultTimeoutSeconds * time.Second,
			AlertThreshold:     DefaultAlertThreshold, // 80% of memory limit
			LogMemoryStats:     false,                 // Disabled by default to reduce log noise
		},
	}
}
