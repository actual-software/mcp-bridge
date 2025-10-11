package direct

import (
	"errors"
	"io"
	"net"
	"time"

	"github.com/actual-software/mcp-bridge/services/router/pkg/mcp"
	"go.uber.org/zap"
)

// StdioResponseReader handles response reading for StdioClient.
type StdioResponseReader struct {
	client *StdioClient
}

// CreateStdioResponseReader creates a new response reader.
func CreateStdioResponseReader(client *StdioClient) *StdioResponseReader {
	return &StdioResponseReader{
		client: client,
	}
}

// ReadResponseLoop reads responses continuously.
func (r *StdioResponseReader) ReadResponseLoop() {
	defer r.cleanup()

	for r.shouldContinueReading() {
		r.readSingleResponse()
	}
}

func (r *StdioResponseReader) cleanup() {
	r.client.wg.Done()
	// Use safe channel close to avoid panic from double close
	r.safeCloseDoneChannel()
}

func (r *StdioResponseReader) safeCloseDoneChannel() {
	select {
	case <-r.client.doneCh:
		// Already closed.
	default:
		close(r.client.doneCh)
	}
}

func (r *StdioResponseReader) shouldContinueReading() bool {
	select {
	case <-r.client.shutdownCh:
		return false
	default:
		return true
	}
}

func (r *StdioResponseReader) readSingleResponse() {
	r.setReadTimeout()

	response, err := r.decodeResponse()
	if err != nil {
		r.handleReadError(err)

		return
	}

	r.routeResponse(response)
}

func (r *StdioResponseReader) setReadTimeout() {
	r.client.mu.RLock()
	stdout := r.client.stdout
	r.client.mu.RUnlock()

	if deadliner, ok := stdout.(interface{ SetReadDeadline(time.Time) error }); ok {
		_ = deadliner.SetReadDeadline(time.Now().Add(time.Second))
	}
}

func (r *StdioResponseReader) decodeResponse() (*mcp.Response, error) {
	r.client.mu.RLock()
	decoder := r.client.stdoutDecoder
	r.client.mu.RUnlock()

	var response mcp.Response
	err := decoder.Decode(&response)

	return &response, err
}

func (r *StdioResponseReader) handleReadError(err error) {
	if r.isTimeoutError(err) {
		return
	}

	if r.isEOFError(err) {
		r.handleEOF()

		return
	}

	r.logDecodeError(err)
}

func (r *StdioResponseReader) isTimeoutError(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) {
		return r.shouldContinueReading()
	}

	return false
}

func (r *StdioResponseReader) isEOFError(err error) bool {
	return errors.Is(err, io.EOF)
}

func (r *StdioResponseReader) handleEOF() {
	r.client.logger.Info("process stdout closed")

	r.client.mu.Lock()
	r.client.state = StateDisconnected
	r.client.mu.Unlock()

	r.client.updateMetrics(func(m *ClientMetrics) {
		m.IsHealthy = false
	})
}

func (r *StdioResponseReader) logDecodeError(err error) {
	// Don't log if shutting down - check twice to minimize race window
	select {
	case <-r.client.shutdownCh:
		return
	default:
	}

	r.client.mu.RLock()
	startTime := r.client.startTime
	r.client.mu.RUnlock()

	// Check again right before logging
	select {
	case <-r.client.shutdownCh:
		return
	default:
		r.client.logger.Error("failed to decode response",
			zap.Error(err),
			zap.String("client_name", r.client.name),
			zap.Duration("uptime", time.Since(startTime)))
	}

	r.client.updateMetrics(func(m *ClientMetrics) {
		m.ErrorCount++
		m.IsHealthy = false
	})
}

func (r *StdioResponseReader) routeResponse(response *mcp.Response) {
	responseID := r.extractResponseID(response)
	if responseID == "" {
		return
	}

	r.deliverResponse(responseID, response)
}

func (r *StdioResponseReader) extractResponseID(response *mcp.Response) string {
	responseID, ok := response.ID.(string)
	if !ok {
		r.client.logger.Warn("received response with invalid ID type",
			zap.Any("response_id", response.ID))

		return ""
	}

	return responseID
}

func (r *StdioResponseReader) deliverResponse(responseID string, response *mcp.Response) {
	r.client.requestMapMu.RLock()
	defer r.client.requestMapMu.RUnlock()

	ch, exists := r.client.requestMap[responseID]
	if !exists {
		r.client.logger.Warn("received response for unknown request",
			zap.String("request_id", responseID))

		return
	}

	select {
	case ch <- response:
		// Response delivered successfully.
	default:
		r.client.logger.Warn("response channel full, dropping response",
			zap.String("request_id", responseID),
			zap.String("client_name", r.client.name),
			zap.Uint64("active_requests", uint64(len(r.client.requestMap))))
	}
}
