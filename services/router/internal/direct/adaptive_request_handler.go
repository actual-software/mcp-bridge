package direct

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/actual-software/mcp-bridge/services/router/pkg/mcp"
)

// AdaptiveRequestHandler handles requests with adaptive mechanisms.
type AdaptiveRequestHandler struct {
	manager *DirectClientManager
}

// CreateAdaptiveRequestHandler creates a new adaptive request handler.
func CreateAdaptiveRequestHandler(manager *DirectClientManager) *AdaptiveRequestHandler {
	return &AdaptiveRequestHandler{
		manager: manager,
	}
}

// HandleRequest processes a request with adaptive mechanisms.
func (h *AdaptiveRequestHandler) HandleRequest(
	ctx context.Context,
	serverURL string,
	req *mcp.Request,
) (*mcp.Response, error) {
	startTime := time.Now()
	traceID := h.generateTraceID(req)

	var (
		response *mcp.Response
		lastErr  error
		client   DirectClient
	)

	err := h.executeWithAdaptive(ctx, serverURL, req, &response, &lastErr, &client)

	h.recordRequestTrace(traceID, startTime, serverURL, req, response, client, err)
	h.updateClientMetrics(serverURL, startTime, req, response, err)

	if err != nil {
		return nil, err
	}

	return response, nil
}

func (h *AdaptiveRequestHandler) generateTraceID(req *mcp.Request) string {
	return fmt.Sprintf("req_%d_%s", time.Now().UnixNano(), req.ID)
}

func (h *AdaptiveRequestHandler) executeWithAdaptive(
	ctx context.Context,
	serverURL string,
	req *mcp.Request,
	response **mcp.Response,
	lastErr *error,
	client *DirectClient,
) error {
	return h.manager.ExecuteWithAdaptiveMechanisms(ctx, serverURL, func(adaptiveCtx context.Context) error {
		var err error

		*client, err = h.manager.GetClient(adaptiveCtx, serverURL)
		if err != nil {
			return err
		}

		resp, err := (*client).SendRequest(adaptiveCtx, req)
		if err != nil {
			*lastErr = err

			return err
		}

		*response = resp

		return nil
	})
}

func (h *AdaptiveRequestHandler) recordRequestTrace(
	traceID string,
	startTime time.Time,
	serverURL string,
	req *mcp.Request,
	response *mcp.Response,
	client DirectClient,
	err error,
) {
	if h.manager.observability == nil {
		return
	}

	trace := h.buildRequestTrace(traceID, startTime, serverURL, req, response, client, err)
	h.addAdaptiveMetadata(&trace)
	h.manager.observability.RecordRequest(trace)
}

func (h *AdaptiveRequestHandler) buildRequestTrace(
	traceID string,
	startTime time.Time,
	serverURL string,
	req *mcp.Request,
	response *mcp.Response,
	client DirectClient,
	err error,
) RequestTrace {
	trace := RequestTrace{
		ID:         traceID,
		Timestamp:  startTime,
		ClientName: serverURL,
		ServerURL:  serverURL,
		Duration:   time.Since(startTime),
		Success:    err == nil,
		Metadata:   make(map[string]interface{}),
	}

	if client != nil {
		trace.Protocol = client.GetProtocol()
	}

	if req != nil {
		trace.Method = req.Method
		if reqBytes, marshalErr := json.Marshal(req); marshalErr == nil {
			trace.RequestSize = len(reqBytes)
		}
	}

	if response != nil {
		if respBytes, marshalErr := json.Marshal(response); marshalErr == nil {
			trace.ResponseSize = len(respBytes)
		}
	}

	if err != nil {
		trace.Error = err.Error()
	}

	return trace
}

func (h *AdaptiveRequestHandler) addAdaptiveMetadata(trace *RequestTrace) {
	if adaptiveStats := h.manager.GetAdaptiveStats(); adaptiveStats != nil {
		trace.Metadata["timeout_adapted"] = adaptiveStats["timeout_adapted"]
		trace.Metadata["retry_attempted"] = adaptiveStats["retry_attempted"]
		trace.Metadata["current_timeout"] = adaptiveStats["current_timeout"]
		trace.Metadata["retry_count"] = adaptiveStats["retry_count"]
	}
}

func (h *AdaptiveRequestHandler) updateClientMetrics(
	serverURL string,
	startTime time.Time,
	req *mcp.Request,
	response *mcp.Response,
	err error,
) {
	existingMetrics, exists := h.manager.observability.GetClientMetrics(serverURL)
	if !exists {
		return
	}

	updater := &ClientMetricsUpdater{
		metrics:   existingMetrics,
		startTime: startTime,
		duration:  time.Since(startTime),
		err:       err,
	}

	updater.updateBasicMetrics()
	updater.updateLatencyStats()
	updater.updateErrorStats()
	updater.updateRates()
	updater.updateBytesTransferred(req, response)
}

// ClientMetricsUpdater handles client metrics updates.
type ClientMetricsUpdater struct {
	metrics   *DetailedClientMetrics
	startTime time.Time
	duration  time.Duration
	err       error
}

func (u *ClientMetricsUpdater) updateBasicMetrics() {
	u.metrics.RequestCount++
	u.metrics.LastUsed = u.startTime
	u.metrics.LastMetricsUpdate = time.Now()
}

func (u *ClientMetricsUpdater) updateLatencyStats() {
	if u.metrics.RequestCount == 1 {
		u.initializeLatencyStats()
	} else {
		u.updateRunningLatencyStats()
	}
}

func (u *ClientMetricsUpdater) initializeLatencyStats() {
	u.metrics.MinLatency = u.duration
	u.metrics.MaxLatency = u.duration
	u.metrics.AverageLatency = u.duration
	u.metrics.P50Latency = u.duration
	u.metrics.P95Latency = u.duration
	u.metrics.P99Latency = u.duration
}

func (u *ClientMetricsUpdater) updateRunningLatencyStats() {
	if u.duration < u.metrics.MinLatency {
		u.metrics.MinLatency = u.duration
	}

	if u.duration > u.metrics.MaxLatency {
		u.metrics.MaxLatency = u.duration
	}

	// Simple running average with safe conversion
	const maxSafeInt64 = 1<<63 - 1
	if u.metrics.RequestCount > 0 && u.metrics.RequestCount <= maxSafeInt64 {
		// Safe conversion: RequestCount is verified to be <= maxSafeInt64
		// #nosec G115 - explicit bounds check ensures RequestCount fits in int64
		count := int64(u.metrics.RequestCount)
		u.metrics.AverageLatency = time.Duration(
			(int64(u.metrics.AverageLatency)*(count-1) + int64(u.duration)) / count)
	}
}

func (u *ClientMetricsUpdater) updateErrorStats() {
	if u.err == nil {
		u.metrics.ConsecutiveFailures = 0
		u.metrics.IsHealthy = true

		return
	}

	u.metrics.ErrorCount++
	u.metrics.LastError = u.err.Error()
	u.metrics.LastErrorTime = time.Now()
	u.metrics.ConsecutiveFailures++
	u.metrics.IsHealthy = false

	u.categorizeError()
}

func (u *ClientMetricsUpdater) categorizeError() {
	if u.metrics.ErrorsByType == nil {
		u.metrics.ErrorsByType = make(map[string]uint64)
	}

	errorType := u.determineErrorType()
	u.metrics.ErrorsByType[errorType]++

	switch errorType {
	case "timeout_error":
		u.metrics.TimeoutCount++
	case "connection_error":
		u.metrics.ConnectionErrors++
	case "protocol_error":
		u.metrics.ProtocolErrors++
	}
}

func (u *ClientMetricsUpdater) determineErrorType() string {
	errStr := u.err.Error()

	if strings.Contains(errStr, "timeout") || strings.Contains(errStr, "context deadline exceeded") {
		return "timeout_error"
	}

	if strings.Contains(errStr, "connection") || strings.Contains(errStr, "network") {
		return "connection_error"
	}

	if strings.Contains(errStr, "protocol") {
		return "protocol_error"
	}

	return "unknown_error"
}

func (u *ClientMetricsUpdater) updateRates() {
	u.metrics.ErrorRate = float64(u.metrics.ErrorCount) / float64(u.metrics.RequestCount)
	u.metrics.SuccessRate = 1.0 - u.metrics.ErrorRate
}

func (u *ClientMetricsUpdater) updateBytesTransferred(req *mcp.Request, response *mcp.Response) {
	if req != nil {
		if reqBytes, err := json.Marshal(req); err == nil {
			u.metrics.BytesSent += uint64(len(reqBytes))
		}
	}

	if response != nil {
		if respBytes, err := json.Marshal(response); err == nil {
			u.metrics.BytesReceived += uint64(len(respBytes))
		}
	}
}
