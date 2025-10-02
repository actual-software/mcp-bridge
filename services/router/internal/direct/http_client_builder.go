package direct

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/actual-software/mcp-bridge/services/router/internal/constants"
	"go.uber.org/zap"
)

const (
	// SchemeHTTP represents the HTTP scheme.
	SchemeHTTP = "http"
	// SchemeHTTPS represents the HTTPS scheme.
	SchemeHTTPS = "https"
)

// HTTPClientBuilder provides a fluent interface for building HTTP clients.
type HTTPClientBuilder struct {
	name            string
	serverURL       string
	config          HTTPClientConfig
	logger          *zap.Logger
	memoryOptimizer *MemoryOptimizer
}

// CreateHTTPClientBuilder initializes a new HTTP client builder.
func CreateHTTPClientBuilder(name, serverURL string) *HTTPClientBuilder {
	return &HTTPClientBuilder{
		name:      name,
		serverURL: serverURL,
		config:    HTTPClientConfig{},
	}
}

// WithConfig sets the configuration.
func (b *HTTPClientBuilder) WithConfig(config HTTPClientConfig) *HTTPClientBuilder {
	b.config = config

	return b
}

// WithLogger sets the logger.
func (b *HTTPClientBuilder) WithLogger(logger *zap.Logger) *HTTPClientBuilder {
	b.logger = logger

	return b
}

// WithMemoryOptimizer sets the memory optimizer.
func (b *HTTPClientBuilder) WithMemoryOptimizer(optimizer *MemoryOptimizer) *HTTPClientBuilder {
	b.memoryOptimizer = optimizer

	return b
}

// Build creates the HTTP client with all configurations.
func (b *HTTPClientBuilder) Build() (*HTTPClient, error) {
	if err := b.validateAndSetDefaults(); err != nil {
		return nil, err
	}

	transport := b.buildTransport()
	httpClient := b.buildHTTPClient(transport)

	return b.createClient(httpClient)
}

// validateAndSetDefaults validates configuration and sets defaults.
func (b *HTTPClientBuilder) validateAndSetDefaults() error {
	// Use serverURL if config URL is not set.
	if b.config.URL == "" {
		b.config.URL = b.serverURL
	}

	// Validate URL.
	parsedURL, err := url.Parse(b.config.URL)
	if err != nil {
		return NewConfigError(b.serverURL, "http", "invalid HTTP URL", err)
	}

	if parsedURL.Scheme != SchemeHTTP && parsedURL.Scheme != SchemeHTTPS {
		return NewConfigError(b.serverURL, "http", "URL must use http:// or https:// scheme", nil)
	}

	b.applyDefaults()

	return nil
}

// applyDefaults applies default values to configuration.
func (b *HTTPClientBuilder) applyDefaults() {
	b.applyBasicDefaults()
	b.applyClientDefaults()
	b.applyPerformanceDefaults()
	b.initializeHeaders()
}

// applyBasicDefaults sets basic configuration defaults.
func (b *HTTPClientBuilder) applyBasicDefaults() {
	if b.config.Method == "" {
		b.config.Method = "POST"
	}

	if b.config.Timeout == 0 {
		b.config.Timeout = defaultTimeoutSeconds * time.Second
	}

	if b.config.HealthCheck.Interval == 0 {
		b.config.HealthCheck.Interval = defaultTimeoutSeconds * time.Second
	}

	if b.config.HealthCheck.Timeout == 0 {
		b.config.HealthCheck.Timeout = defaultMaxConnections * time.Second
	}
}

// applyClientDefaults sets client transport defaults.
func (b *HTTPClientBuilder) applyClientDefaults() {
	if b.config.Client.MaxIdleConns == 0 {
		b.config.Client.MaxIdleConns = DefaultSampleWindowSize
	}

	if b.config.Client.MaxIdleConnsPerHost == 0 {
		b.config.Client.MaxIdleConnsPerHost = DefaultMaxIdleConnsPerHost
	}

	if b.config.Client.IdleConnTimeout == 0 {
		b.config.Client.IdleConnTimeout = constants.DefaultIdleConnTimeout
	}

	if b.config.Client.MaxRedirects == 0 {
		b.config.Client.MaxRedirects = constants.DefaultMaxRedirects
	}

	if b.config.Client.UserAgent == "" {
		b.config.Client.UserAgent = "MCP-Router-HTTP-Client/1.0"
	}
}

// applyPerformanceDefaults sets performance optimization defaults.
func (b *HTTPClientBuilder) applyPerformanceDefaults() {
	if !b.config.Performance.EnableCompression {
		b.config.Performance.EnableCompression = true
	}

	if b.config.Performance.CompressionThreshold == 0 {
		b.config.Performance.CompressionThreshold = defaultBufferSize
	}

	if !b.config.Performance.EnableHTTP2 {
		b.config.Performance.EnableHTTP2 = true
	}

	if b.config.Performance.MaxConnsPerHost == 0 {
		b.config.Performance.MaxConnsPerHost = DefaultSampleWindowSize
	}

	if !b.config.Performance.ReuseEncoders {
		b.config.Performance.ReuseEncoders = true
	}

	if b.config.Performance.ResponseBufferSize == 0 {
		b.config.Performance.ResponseBufferSize = HTTPBufferSizeKB * KilobyteFactor
	}
}

// initializeHeaders ensures headers map is initialized.
func (b *HTTPClientBuilder) initializeHeaders() {
	if b.config.Headers == nil {
		b.config.Headers = make(map[string]string)
	}

	// Set default headers.
	if _, exists := b.config.Headers["Content-Type"]; !exists {
		b.config.Headers["Content-Type"] = "application/json"
	}

	if _, exists := b.config.Headers["User-Agent"]; !exists {
		b.config.Headers["User-Agent"] = b.config.Client.UserAgent
	}
}

// buildTransport creates the HTTP transport.
func (b *HTTPClientBuilder) buildTransport() *http.Transport {
	transport := &http.Transport{
		MaxIdleConns:        b.config.Client.MaxIdleConns,
		MaxIdleConnsPerHost: b.config.Client.MaxIdleConnsPerHost,
		IdleConnTimeout:     b.config.Client.IdleConnTimeout,
		DisableCompression:  !b.config.Performance.EnableCompression,
		ForceAttemptHTTP2:   b.config.Performance.EnableHTTP2,
		MaxConnsPerHost:     b.config.Performance.MaxConnsPerHost,
	}

	// Proxy configuration would go here if supported.

	return transport
}

// buildHTTPClient creates the HTTP client with transport.
func (b *HTTPClientBuilder) buildHTTPClient(transport *http.Transport) *http.Client {
	client := &http.Client{
		Transport: transport,
		Timeout:   b.config.Timeout,
	}

	// Configure redirect policy.
	if !b.config.Client.FollowRedirects {
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
	} else if b.config.Client.MaxRedirects > 0 {
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			if len(via) >= b.config.Client.MaxRedirects {
				return fmt.Errorf("too many redirects (%d)", len(via))
			}

			return nil
		}
	}

	return client
}

// createClient creates the final HTTPClient instance.
func (b *HTTPClientBuilder) createClient(httpClient *http.Client) (*HTTPClient, error) {
	client := &HTTPClient{
		name:            b.name,
		url:             b.config.URL,
		config:          b.config,
		logger:          b.logger,
		client:          httpClient,
		memoryOptimizer: b.memoryOptimizer,
		state:           StateDisconnected,
		startTime:       time.Now(),
		metrics:         ClientMetrics{},
		shutdownCh:      make(chan struct{}),
		doneCh:          make(chan struct{}),
	}

	// Start health check if enabled.
	if b.config.HealthCheck.Enabled {
		client.wg.Add(1)

		go client.healthCheckLoop(context.Background())
	}

	return client, nil
}

// CreateOptimizedHTTPClient is the new name for NewHTTPClientWithMemoryOptimizer.
func CreateOptimizedHTTPClient(
	name, serverURL string,
	config HTTPClientConfig,
	logger *zap.Logger,
	memoryOptimizer *MemoryOptimizer,
) (*HTTPClient, error) {
	return CreateHTTPClientBuilder(name, serverURL).
		WithConfig(config).
		WithLogger(logger).
		WithMemoryOptimizer(memoryOptimizer).
		Build()
}
