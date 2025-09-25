package direct

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/poiley/mcp-bridge/services/router/pkg/mcp"
)

// HTTPRequestProcessor handles HTTP request processing.
type HTTPRequestProcessor struct {
	client *HTTPClient
}

// CreateHTTPRequestProcessor creates a new HTTP request processor.
func CreateHTTPRequestProcessor(client *HTTPClient) *HTTPRequestProcessor {
	return &HTTPRequestProcessor{
		client: client,
	}
}

// ProcessRequest sends an MCP request and returns the response.
func (p *HTTPRequestProcessor) ProcessRequest(ctx context.Context, req *mcp.Request) (*mcp.Response, error) {
	startTime := time.Now() // Capture start time for latency calculation

	if err := p.validateClientState(); err != nil {
		return nil, err
	}

	p.ensureRequestID(req)

	reqBody, err := p.marshalRequest(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := p.createHTTPRequest(ctx, reqBody)
	if err != nil {
		return nil, err
	}

	resp, err := p.executeRequest(ctx, httpReq)
	if err != nil {
		return nil, err
	}

	defer func() { _ = resp.Body.Close() }()

	if err := p.validateHTTPResponse(resp); err != nil {
		return nil, err
	}

	return p.parseResponse(resp, startTime)
}

func (p *HTTPRequestProcessor) validateClientState() error {
	p.client.mu.RLock()
	defer p.client.mu.RUnlock()

	if p.client.state != StateConnected && p.client.state != StateHealthy {
		return NewConnectionError(p.client.url, "http", "client not connected", ErrClientNotConnected)
	}

	return nil
}

func (p *HTTPRequestProcessor) ensureRequestID(req *mcp.Request) {
	if req.ID == nil || req.ID == "" {
		requestID := fmt.Sprintf("%s-%d", p.client.name, atomic.AddUint64(&p.client.requestID, 1))
		req.ID = requestID
	}
}

func (p *HTTPRequestProcessor) marshalRequest(req *mcp.Request) ([]byte, error) {
	if p.shouldUseOptimizedMarshaling() {
		return p.marshalWithOptimization(req)
	}

	return p.marshalStandard(req)
}

func (p *HTTPRequestProcessor) shouldUseOptimizedMarshaling() bool {
	return p.client.memoryOptimizer != nil && p.client.config.Performance.ReuseEncoders
}

func (p *HTTPRequestProcessor) marshalWithOptimization(req *mcp.Request) ([]byte, error) {
	buf := p.client.memoryOptimizer.GetBuffer()
	defer p.client.memoryOptimizer.PutBuffer(buf)

	encoder := p.client.memoryOptimizer.GetJSONEncoder(buf)
	defer p.client.memoryOptimizer.PutJSONEncoder(encoder)

	if err := encoder.Encode(req); err != nil {
		p.recordError()

		return nil, NewRequestError(p.client.url, "http", "failed to encode request", err)
	}

	return buf.Bytes(), nil
}

func (p *HTTPRequestProcessor) marshalStandard(req *mcp.Request) ([]byte, error) {
	reqBody, err := json.Marshal(req)
	if err != nil {
		p.recordError()

		return nil, NewRequestError(p.client.url, "http", "failed to marshal request", err)
	}

	return reqBody, nil
}

func (p *HTTPRequestProcessor) createHTTPRequest(ctx context.Context, reqBody []byte) (*http.Request, error) {
	httpReq, err := http.NewRequestWithContext(ctx, p.client.config.Method, p.client.config.URL, bytes.NewReader(reqBody))
	if err != nil {
		p.recordError()

		return nil, NewRequestError(p.client.url, "http", "failed to create HTTP request", err)
	}

	p.setRequestHeaders(httpReq)

	return httpReq, nil
}

func (p *HTTPRequestProcessor) setRequestHeaders(httpReq *http.Request) {
	for key, value := range p.client.config.Headers {
		httpReq.Header.Set(key, value)
	}

	httpReq.Header.Set("User-Agent", p.client.config.Client.UserAgent)
}

func (p *HTTPRequestProcessor) executeRequest(ctx context.Context, httpReq *http.Request) (*http.Response, error) {
	timeout := p.calculateTimeout(ctx)

	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	httpReq = httpReq.WithContext(reqCtx)

	resp, err := p.client.client.Do(httpReq)
	if err != nil {
		return nil, p.handleRequestError(ctx, err, timeout)
	}

	return resp, nil
}

func (p *HTTPRequestProcessor) calculateTimeout(ctx context.Context) time.Duration {
	timeout := p.client.config.Timeout
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining < timeout {
			timeout = remaining
		}
	}

	return timeout
}

func (p *HTTPRequestProcessor) handleRequestError(ctx context.Context, err error, timeout time.Duration) error {
	p.recordError()

	if ctx.Err() == context.DeadlineExceeded {
		return NewTimeoutError(p.client.url, "http", timeout, err)
	}

	p.markUnhealthy()

	return NewRequestError(p.client.url, "http", "HTTP request failed", err)
}

func (p *HTTPRequestProcessor) validateHTTPResponse(resp *http.Response) error {
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		p.recordError()

		bodyBytes, _ := io.ReadAll(resp.Body)

		return NewRequestError(p.client.url, "http",
			fmt.Sprintf("HTTP error %d: %s", resp.StatusCode, string(bodyBytes)), nil)
	}

	return nil
}

func (p *HTTPRequestProcessor) parseResponse(resp *http.Response, startTime time.Time) (*mcp.Response, error) {
	if p.shouldUseOptimizedParsing() {
		return p.parseWithOptimization(resp, startTime)
	}

	return p.parseStandard(resp, startTime)
}

func (p *HTTPRequestProcessor) shouldUseOptimizedParsing() bool {
	return p.client.memoryOptimizer != nil && p.client.config.Performance.ReuseEncoders
}

func (p *HTTPRequestProcessor) parseWithOptimization(resp *http.Response, startTime time.Time) (*mcp.Response, error) {
	buf := p.client.memoryOptimizer.GetBuffer()
	defer p.client.memoryOptimizer.PutBuffer(buf)

	if _, err := buf.ReadFrom(resp.Body); err != nil {
		p.recordError()

		return nil, NewRequestError(p.client.url, "http", "failed to read response body", err)
	}

	reader := bytes.NewReader(buf.Bytes())

	decoder := p.client.memoryOptimizer.GetJSONDecoder(reader)
	defer p.client.memoryOptimizer.PutJSONDecoder(decoder)

	var mcpResp mcp.Response
	if err := decoder.Decode(&mcpResp); err != nil {
		p.recordError()

		return nil, NewRequestError(p.client.url, "http", "failed to decode response", err)
	}

	p.recordSuccess(startTime)

	return &mcpResp, nil
}

func (p *HTTPRequestProcessor) parseStandard(resp *http.Response, startTime time.Time) (*mcp.Response, error) {
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		p.recordError()

		return nil, NewRequestError(p.client.url, "http", "failed to read response body", err)
	}

	var mcpResp mcp.Response
	if err := json.Unmarshal(respBody, &mcpResp); err != nil {
		p.recordError()

		return nil, NewRequestError(p.client.url, "http", "failed to unmarshal response", err)
	}

	p.recordSuccess(startTime)

	return &mcpResp, nil
}

func (p *HTTPRequestProcessor) recordError() {
	p.client.updateMetrics(func(m *ClientMetrics) {
		m.ErrorCount++
	})
}

func (p *HTTPRequestProcessor) recordSuccess(startTime time.Time) {
	latency := time.Since(startTime)

	p.client.updateMetrics(func(m *ClientMetrics) {
		m.RequestCount++

		m.LastUsed = time.Now()
		if m.AverageLatency == 0 {
			m.AverageLatency = latency
		} else {
			m.AverageLatency = (m.AverageLatency + latency) / DivisionFactorTwo
		}
	})
}

func (p *HTTPRequestProcessor) markUnhealthy() {
	p.client.mu.Lock()
	p.client.state = StateUnhealthy
	p.client.mu.Unlock()
}
