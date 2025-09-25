package direct

import (
	"context"
	"errors"
	"fmt"

	"go.uber.org/zap"
)

// ProtocolDetector handles protocol detection for servers.
type ProtocolDetector struct {
	manager *DirectClientManager
}

// CreateProtocolDetector creates a new protocol detector.
func CreateProtocolDetector(manager *DirectClientManager) *ProtocolDetector {
	return &ProtocolDetector{
		manager: manager,
	}
}

// DetectProtocol detects the protocol for a server URL.
func (d *ProtocolDetector) DetectProtocol(ctx context.Context, serverURL string) (ClientType, error) {
	// Try cache first.
	protocol, err := d.checkCache(serverURL)
	if err == nil && protocol != "" {
		return protocol, err
	}

	if err != nil {
		return "", err
	}

	// Validate detection is enabled.
	if err := d.validateDetectionEnabled(); err != nil {
		return "", err
	}

	// Perform detection.
	protocol, err = d.performDetection(ctx, serverURL)

	// Cache result.
	d.cacheResult(serverURL, protocol)

	if err != nil {
		return "", err
	}

	return protocol, nil
}

func (d *ProtocolDetector) checkCache(serverURL string) (ClientType, error) {
	if !d.manager.config.AutoDetection.CacheResults {
		return "", nil
	}

	cache := &ProtocolCacheReader{manager: d.manager}

	return cache.ReadCache(serverURL)
}

func (d *ProtocolDetector) validateDetectionEnabled() error {
	if !d.manager.config.AutoDetection.Enabled {
		return errors.New("protocol auto-detection is disabled")
	}

	return nil
}

func (d *ProtocolDetector) performDetection(ctx context.Context, serverURL string) (ClientType, error) {
	detector := &ProtocolTester{
		manager:   d.manager,
		serverURL: serverURL,
	}

	ctx, cancel := context.WithTimeout(ctx, d.manager.config.AutoDetection.Timeout)
	defer cancel()

	return detector.TestProtocols(ctx)
}

func (d *ProtocolDetector) cacheResult(serverURL string, protocol ClientType) {
	if !d.manager.config.AutoDetection.CacheResults {
		return
	}

	d.manager.setCacheEntryWithExpiration(serverURL, protocol, d.manager.config.AutoDetection.CacheTTL)
}

// ProtocolCacheReader handles cache reading for protocol detection.
type ProtocolCacheReader struct {
	manager *DirectClientManager
}

// ReadCache reads from the protocol cache.
func (r *ProtocolCacheReader) ReadCache(serverURL string) (ClientType, error) {
	r.manager.cacheMu.RLock()
	defer r.manager.cacheMu.RUnlock()

	cached, exists := r.manager.protocolCache[serverURL]
	if !exists {
		r.recordCacheMiss()

		return "", nil
	}

	r.recordCacheHit()

	// Empty cache means previous detection failed.
	if cached == "" {
		return "", fmt.Errorf("could not detect compatible protocol for %s", serverURL)
	}

	return cached, nil
}

func (r *ProtocolCacheReader) recordCacheHit() {
	r.manager.updateMetrics(func(metrics *ManagerMetrics) {
		metrics.CacheHits++
	})
}

func (r *ProtocolCacheReader) recordCacheMiss() {
	r.manager.updateMetrics(func(metrics *ManagerMetrics) {
		metrics.CacheMisses++
	})
}

// ProtocolTester tests protocols for a server.
type ProtocolTester struct {
	manager   *DirectClientManager
	serverURL string
}

// TestProtocols tests protocols in order of likelihood.
func (t *ProtocolTester) TestProtocols(ctx context.Context) (ClientType, error) {
	hints := t.manager.getProtocolHints(t.serverURL)

	for _, protocol := range hints {
		if t.shouldStop(ctx) {
			return "", fmt.Errorf("protocol detection timed out for %s", t.serverURL)
		}

		t.logAttempt(protocol)

		if t.tryProtocol(ctx, protocol) {
			t.recordSuccess(protocol)

			return protocol, nil
		}

		// Check for timeout after failed attempt.
		if ctx.Err() != nil {
			return "", fmt.Errorf("protocol detection timed out for %s", t.serverURL)
		}
	}

	return "", fmt.Errorf("could not detect compatible protocol for %s", t.serverURL)
}

func (t *ProtocolTester) shouldStop(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}

func (t *ProtocolTester) logAttempt(protocol ClientType) {
	t.manager.logger.Debug("trying protocol detection",
		zap.String("server_url", t.serverURL),
		zap.String("protocol", string(protocol)))
}

func (t *ProtocolTester) tryProtocol(ctx context.Context, protocol ClientType) bool {
	return t.manager.canConnectWithProtocol(ctx, t.serverURL, protocol)
}

func (t *ProtocolTester) recordSuccess(protocol ClientType) {
	t.manager.updateMetrics(func(metrics *ManagerMetrics) {
		metrics.ProtocolDetections++
	})

	t.manager.logger.Info("protocol detected successfully",
		zap.String("server_url", t.serverURL),
		zap.String("protocol", string(protocol)))
}
