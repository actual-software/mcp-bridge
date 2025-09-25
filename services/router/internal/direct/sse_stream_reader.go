package direct

import (
	"bufio"
	"errors"
	"io"
	"strings"

	"go.uber.org/zap"
)

// SSEStreamReader handles SSE stream reading operations.
type SSEStreamReader struct {
	client *SSEClient
}

// CreateSSEStreamReader creates a new SSE stream reader.
func CreateSSEStreamReader(client *SSEClient) *SSEStreamReader {
	return &SSEStreamReader{
		client: client,
	}
}

// ReadStreamLoop continuously reads from the SSE stream.
func (r *SSEStreamReader) ReadStreamLoop() {
	defer r.cleanup()

	for r.shouldContinueReading() {
		r.readAndProcessEvent()
	}
}

func (r *SSEStreamReader) cleanup() {
	r.client.wg.Done()
	// Use safe channel close to avoid panic from double close
	r.safeCloseDoneChannel()
}

func (r *SSEStreamReader) safeCloseDoneChannel() {
	select {
	case <-r.client.doneCh:
		// Already closed.
	default:
		close(r.client.doneCh)
	}
}

func (r *SSEStreamReader) shouldContinueReading() bool {
	select {
	case <-r.client.shutdownCh:
		return false
	default:
		return true
	}
}

func (r *SSEStreamReader) readAndProcessEvent() {
	reader := r.getStreamReader()
	if reader == nil {
		r.client.logger.Debug("SSE stream reader is nil, stopping read loop")

		return
	}

	event, err := r.readSSEEvent(reader)
	if err != nil {
		r.handleReadError(err)

		return
	}

	if event != nil {
		r.client.processSSEEvent(event)
	}
}

func (r *SSEStreamReader) getStreamReader() *bufio.Reader {
	r.client.streamMu.RLock()
	defer r.client.streamMu.RUnlock()

	return r.client.streamReader
}

func (r *SSEStreamReader) handleReadError(err error) {
	r.logError(err)
	r.updateClientState(err)
	r.recordErrorMetrics()
}

func (r *SSEStreamReader) logError(err error) {
	if r.isExpectedClosure(err) {
		r.client.logger.Debug("SSE stream closed", zap.Error(err))
	} else {
		r.client.logger.Error("SSE read error", zap.Error(err))
	}
}

func (r *SSEStreamReader) isExpectedClosure(err error) bool {
	return errors.Is(err, io.EOF) ||
		strings.Contains(err.Error(), "use of closed network connection")
}

func (r *SSEStreamReader) updateClientState(err error) {
	r.client.mu.Lock()
	defer r.client.mu.Unlock()

	if r.isClosingState() {
		// Already closing, keep current state.
		return
	}

	if r.isExpectedClosure(err) {
		// For EOF/closed connection, stay connected but mark as unhealthy.
		r.client.logger.Debug("stream closed but maintaining connected state for potential reconnection")
	} else {
		// For other errors, transition to disconnected.
		r.client.state = StateDisconnected
	}
}

func (r *SSEStreamReader) isClosingState() bool {
	return r.client.state == StateClosing || r.client.state == StateClosed
}

func (r *SSEStreamReader) recordErrorMetrics() {
	r.client.updateMetrics(func(m *ClientMetrics) {
		m.ErrorCount++
		m.IsHealthy = false
	})
}

// readSSEEvent reads a single SSE event from the stream.
func (r *SSEStreamReader) readSSEEvent(reader *bufio.Reader) (*SSEEvent, error) {
	parser := &SSEEventParser{
		event: &SSEEvent{},
	}

	return parser.ParseEvent(reader)
}

// SSEEventParser handles parsing of SSE events.
type SSEEventParser struct {
	event *SSEEvent
}

// ParseEvent parses a single SSE event from the reader.
func (p *SSEEventParser) ParseEvent(reader *bufio.Reader) (*SSEEvent, error) {
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}

		line = strings.TrimRight(line, "\n\r")

		if p.isEventComplete(line) {
			return p.event, nil
		}

		p.parseLine(line)
	}
}

func (p *SSEEventParser) isEventComplete(line string) bool {
	if line != "" {
		return false
	}

	return p.event.Data != "" || p.event.Event != ""
}

func (p *SSEEventParser) parseLine(line string) {
	switch {
	case strings.HasPrefix(line, "data:"):
		p.parseDataField(line)
	case strings.HasPrefix(line, "event:"):
		p.parseEventField(line)
	case strings.HasPrefix(line, "id:"):
		p.parseIDField(line)
	case strings.HasPrefix(line, "retry:"):
		// Parse retry time (not used in MCP).
	case len(line) > 0 && line[0] == ':':
		// Comment line, ignore.
	}
}

func (p *SSEEventParser) parseDataField(line string) {
	data := strings.TrimSpace(line[5:])
	if p.event.Data == "" {
		p.event.Data = data
	} else {
		p.event.Data += "\n" + data
	}
}

func (p *SSEEventParser) parseEventField(line string) {
	p.event.Event = strings.TrimSpace(line[6:])
}

func (p *SSEEventParser) parseIDField(line string) {
	p.event.ID = strings.TrimSpace(line[3:])
}
