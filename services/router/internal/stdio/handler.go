package stdio

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"go.uber.org/zap"
)

// Handler manages stdio input/output operations.
type Handler struct {
	logger     *zap.Logger
	stdinChan  chan<- []byte
	stdoutChan <-chan []byte
	reader     *bufio.Reader
	writer     *bufio.Writer
}

// NewHandler creates a new stdio handler.
func NewHandler(logger *zap.Logger, stdinChan chan<- []byte, stdoutChan <-chan []byte) *Handler {
	return &Handler{
		logger:     logger,
		stdinChan:  stdinChan,
		stdoutChan: stdoutChan,
		reader:     bufio.NewReader(os.Stdin),
		writer:     bufio.NewWriter(os.Stdout),
	}
}

// Run starts the stdio handler.
func (h *Handler) Run(ctx context.Context, wg *sync.WaitGroup) {
	// Start input handler.
	wg.Add(1)

	go h.handleStdin(ctx, wg)

	// Start output handler.
	wg.Add(1)

	go h.handleStdout(ctx, wg)
}

// handleStdin reads from stdin and sends to channel.
func (h *Handler) handleStdin(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	defer close(h.stdinChan)

	h.logger.Debug("Starting stdin handler")

	for {
		select {
		case <-ctx.Done():
			h.logger.Debug("Stdin handler shutting down")

			return
		default:
			if !h.processNextLine(ctx) {
				return
			}
		}
	}
}

// Returns false if the handler should exit, true to continue.
func (h *Handler) processNextLine(ctx context.Context) bool {
	line, err := h.reader.ReadBytes('\n')
	if err != nil {
		return h.handleReadError(ctx, line, err)
	}

	processedLine := h.cleanLine(line)
	if len(processedLine) == 0 {
		return true // Skip empty lines
	}

	h.logger.Debug("Read from stdin",
		zap.Int("bytes", len(processedLine)),
		zap.ByteString("data", processedLine),
	)

	return h.sendToChannel(ctx, processedLine)
}

// handleReadError processes read errors and decides whether to continue or exit.
func (h *Handler) handleReadError(ctx context.Context, line []byte, err error) bool {
	if errors.Is(err, io.EOF) {
		// Process any remaining data before EOF.
		if len(line) > 0 {
			processedLine := h.cleanLine(line)
			if len(processedLine) > 0 {
				h.logger.Debug("Read from stdin", zap.Int("bytes", len(processedLine)), zap.String("data", string(processedLine)))
				h.sendToChannel(ctx, processedLine)
			}
		}

		h.logger.Debug("EOF on stdin")

		return false
	}

	h.logger.Error("Error reading from stdin", zap.Error(err))
	// For persistent read errors, exit to avoid infinite loop.
	return false
}

// cleanLine removes newlines and carriage returns from the line.
func (h *Handler) cleanLine(line []byte) []byte {
	// Trim newline.
	if len(line) > 0 && line[len(line)-1] == '\n' {
		line = line[:len(line)-1]
	}

	if len(line) > 0 && line[len(line)-1] == '\r' {
		line = line[:len(line)-1]
	}

	return line
}

// sendToChannel sends the line to the stdin channel, respecting context cancellation.
func (h *Handler) sendToChannel(ctx context.Context, line []byte) bool {
	select {
	case h.stdinChan <- line:
		return true
	case <-ctx.Done():
		h.logger.Debug("Stdin handler shutting down")

		return false
	}
}

// handleStdout reads from channel and writes to stdout.
func (h *Handler) handleStdout(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	h.logger.Debug("Starting stdout handler")

	for {
		select {
		case <-ctx.Done():
			h.logger.Debug("Stdout handler shutting down")
			// Flush any remaining data.
			if err := h.writer.Flush(); err != nil {
				h.logger.Error("Error flushing stdout", zap.Error(err))
			}

			return
		case data := <-h.stdoutChan:
			if data == nil {
				continue
			}

			h.logger.Debug("Writing to stdout",
				zap.Int("bytes", len(data)),
				zap.ByteString("data", data),
			)

			// Write data.
			if _, err := h.writer.Write(data); err != nil {
				h.logger.Error("Error writing to stdout", zap.Error(err))

				continue
			}

			// Write newline.
			if _, err := h.writer.Write([]byte("\n")); err != nil {
				h.logger.Error("Error writing newline to stdout", zap.Error(err))

				continue
			}

			// Flush immediately for real-time output.
			if err := h.writer.Flush(); err != nil {
				h.logger.Error("Error flushing stdout", zap.Error(err))
			}
		}
	}
}

// WriteErrorf writes an error message to stderr.
func (h *Handler) WriteErrorf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	// Ignoring error: writing to stderr in error path.
	_, _ = fmt.Fprintf(os.Stderr, "Error: %s\n", msg)
}
