package stdio

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/actual-software/mcp-bridge/services/router/internal/constants"

	"go.uber.org/zap"

	"github.com/actual-software/mcp-bridge/test/testutil"
)

const (
	testIterations          = 100
	testMaxIterations       = 1000
	httpStatusOK            = 200
	testTimeout             = 50
	httpStatusInternalError = 500
)

func TestNewHandler(t *testing.T) {
	logger := testutil.NewTestLogger(t)
	stdinChan := make(chan []byte, 10)
	stdoutChan := make(chan []byte, 10)

	handler := NewHandler(logger, stdinChan, stdoutChan)

	if handler == nil {
		t.Fatal("Expected handler to be created")

		return
	}

	if handler.logger != logger {
		t.Error("Logger not set correctly")
	}

	if handler.stdinChan != stdinChan {
		t.Error("Stdin channel not set correctly")
	}

	if handler.stdoutChan != stdoutChan {
		t.Error("Stdout channel not set correctly")
	}

	if handler.reader == nil {
		t.Error("Reader not initialized")
	}

	if handler.writer == nil {
		t.Error("Writer not initialized")
	}
}

func TestHandler_handleStdin(t *testing.T) {
	tests := createHandleStdinTests()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := testutil.NewTestLogger(t)
			stdinChan := make(chan []byte, 10)
			handler := setupStdinHandler(logger, stdinChan, tt.input)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			received := runStdinHandlerTest(t, ctx, cancel, handler, stdinChan, tt)

			validateStdinResults(t, tt, received, stdinChan)
		})
	}
}

func createHandleStdinTests() []struct {
	name      string
	input     string
	expected  []string
	ctxCancel bool
	cancelAt  int
} {
	return []struct {
		name      string
		input     string
		expected  []string
		ctxCancel bool
		cancelAt  int
	}{
		{
			name:     "Single line input",
			input:    "test message\n",
			expected: []string{"test message"},
		},
		{
			name:     "Multiple lines",
			input:    "line1\nline2\nline3\n",
			expected: []string{"line1", "line2", "line3"},
		},
		{
			name:     "Lines with carriage return",
			input:    "line1\r\nline2\r\n",
			expected: []string{"line1", "line2"},
		},
		{
			name:     "Empty lines ignored",
			input:    "line1\n\n\nline2\n",
			expected: []string{"line1", "line2"},
		},
		{
			name:     "No trailing newline",
			input:    "test message",
			expected: []string{"test message"},
		},
		{
			name:      "Context cancelled mid-read",
			input:     "line1\nline2\nline3\n",
			expected:  []string{"line1"},
			ctxCancel: true,
			cancelAt:  1,
		},
	}
}

func setupStdinHandler(logger *zap.Logger, stdinChan chan []byte, input string) *Handler {
	return &Handler{
		logger:    logger,
		stdinChan: stdinChan,
		reader:    bufio.NewReader(strings.NewReader(input)),
	}
}

func runStdinHandlerTest(
	t *testing.T,
	ctx context.Context,
	cancel context.CancelFunc,
	handler *Handler,
	stdinChan chan []byte,
	tt struct {
		name      string
		input     string
		expected  []string
		ctxCancel bool
		cancelAt  int
	},
) []string {
	t.Helper()

	wg := &sync.WaitGroup{}
	wg.Add(1)

	// Collect received messages.
	var received []string

	done := make(chan bool)

	go func() {
		for {
			select {
			case msg, ok := <-stdinChan:
				if !ok {
					done <- true

					return
				}

				received = append(received, string(msg))
				if tt.ctxCancel && len(received) >= tt.cancelAt {
					cancel()
				}
			case <-time.After(testIterations * time.Millisecond):
				done <- true

				return
			}
		}
	}()

	// Run handler.
	go handler.handleStdin(ctx, wg)

	// Wait for completion.
	<-done
	wg.Wait()

	return received
}

func validateStdinResults(t *testing.T, tt struct {
	name      string
	input     string
	expected  []string
	ctxCancel bool
	cancelAt  int
}, received []string, stdinChan chan []byte) {
	t.Helper()

	// Verify results.
	if !tt.ctxCancel && len(received) != len(tt.expected) {
		t.Errorf("Expected %d messages, got %d", len(tt.expected), len(received))
	}

	for i := 0; i < len(received) && i < len(tt.expected); i++ {
		if received[i] != tt.expected[i] {
			t.Errorf("Message %d: expected '%s', got '%s'", i, tt.expected[i], received[i])
		}
	}

	// Verify channel is closed.
	select {
	case _, ok := <-stdinChan:
		if ok {
			t.Error("Expected stdin channel to be closed")
		}
	default:
		if !tt.ctxCancel {
			t.Error("Channel should be readable when closed")
		}
	}
}

func TestHandler_handleStdin_ErrorHandling(t *testing.T) {
	// Test EOF handling.
	t.Run("EOF handling", func(t *testing.T) {
		logger := testutil.NewTestLogger(t)
		stdinChan := make(chan []byte, 10)
		handler := &Handler{
			logger:    logger,
			stdinChan: stdinChan,
			reader:    bufio.NewReader(strings.NewReader("")), // Empty reader will return EOF
		}

		ctx := context.Background()
		wg := &sync.WaitGroup{}
		wg.Add(1)

		handler.handleStdin(ctx, wg)

		// Channel should be closed.
		_, ok := <-stdinChan
		if ok {
			t.Error("Expected channel to be closed on EOF")
		}
	})

	// Test read error handling.
	t.Run("Read error handling", func(t *testing.T) {
		logger := testutil.NewTestLogger(t)
		stdinChan := make(chan []byte, 10)

		// Create a reader that returns an error after first read.
		errorReader := &errorAfterNReader{
			reader: strings.NewReader("line1\nline2\n"),
			n:      1,
			err:    errors.New("read error"),
		}

		handler := &Handler{
			logger:    logger,
			stdinChan: stdinChan,
			reader:    bufio.NewReader(errorReader),
		}

		ctx, cancel := context.WithTimeout(context.Background(), httpStatusOK*time.Millisecond)
		defer cancel()

		wg := &sync.WaitGroup{}
		wg.Add(1)

		go handler.handleStdin(ctx, wg)

		// Should receive first line.
		select {
		case msg := <-stdinChan:
			if string(msg) != "line1" {
				t.Errorf("Expected 'line1', got '%s'", string(msg))
			}
		case <-time.After(testIterations * time.Millisecond):
			t.Fatal("Timeout waiting for message")
		}

		wg.Wait()
	})
}

func TestHandler_handleStdout(t *testing.T) {
	tests := createHandleStdoutTests()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outputBuf, handler, stdoutChan, ctx, cancel := setupStdoutHandler(t)
			defer cancel()

			wg := &sync.WaitGroup{}
			wg.Add(1)

			// Start handler.
			go handler.handleStdout(ctx, wg)

			sendStdoutMessages(t, stdoutChan, tt, cancel)

			// Allow time for processing.
			time.Sleep(testTimeout * time.Millisecond)
			cancel()
			wg.Wait()

			verifyStdoutOutput(t, tt, outputBuf)
		})
	}
}

func createHandleStdoutTests() []struct {
	name      string
	messages  []string
	expected  string
	ctxCancel bool
	cancelAt  int
} {
	return []struct {
		name      string
		messages  []string
		expected  string
		ctxCancel bool
		cancelAt  int
	}{
		{
			name:     "Single message",
			messages: []string{"test output"},
			expected: "test output\n",
		},
		{
			name:     "Multiple messages",
			messages: []string{"line1", "line2", "line3"},
			expected: "line1\nline2\nline3\n",
		},
		{
			name:     "Empty message ignored",
			messages: []string{"line1", "", "line2"},
			expected: "line1\nline2\n",
		},
		{
			name:      "Context cancelled",
			messages:  []string{"line1", "line2", "line3"},
			expected:  "line1\n",
			ctxCancel: true,
			cancelAt:  1,
		},
	}
}

func setupStdoutHandler(t *testing.T) (*bytes.Buffer, *Handler, chan []byte, context.Context, context.CancelFunc) {
	t.Helper()

	var outputBuf bytes.Buffer

	writer := bufio.NewWriter(&outputBuf)
	logger := testutil.NewTestLogger(t)
	stdoutChan := make(chan []byte, 10)

	handler := &Handler{
		logger:     logger,
		stdoutChan: stdoutChan,
		writer:     writer,
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &outputBuf, handler, stdoutChan, ctx, cancel
}

func sendStdoutMessages(t *testing.T, stdoutChan chan []byte, tt struct {
	name      string
	messages  []string
	expected  string
	ctxCancel bool
	cancelAt  int
}, cancel context.CancelFunc) {
	t.Helper()

	// Send messages.
	for i, msg := range tt.messages {
		if msg == "" {
			stdoutChan <- nil

			continue
		}

		stdoutChan <- []byte(msg)

		// Small delay to ensure ordering and processing.
		time.Sleep(constants.TestTickInterval)

		if tt.ctxCancel && i+1 >= tt.cancelAt {
			// Give a bit more time for the message to be processed before cancelling.
			time.Sleep(20 * time.Millisecond)
			cancel()

			break
		}
	}
}

func verifyStdoutOutput(t *testing.T, tt struct {
	name      string
	messages  []string
	expected  string
	ctxCancel bool
	cancelAt  int
}, outputBuf *bytes.Buffer) {
	t.Helper()

	// Verify output.
	output := outputBuf.String()
	if output != tt.expected {
		t.Errorf("Expected output '%s', got '%s'", tt.expected, output)
	}
}

func TestHandler_handleStdout_WriteErrorf(t *testing.T) {
	logger := testutil.NewTestLogger(t)
	stdoutChan := make(chan []byte, 10)

	// Create a writer that returns an error.
	errorWriter := &alwaysErrorWriter{}

	handler := &Handler{
		logger:     logger,
		stdoutChan: stdoutChan,
		writer:     bufio.NewWriter(errorWriter),
	}

	ctx, cancel := context.WithTimeout(context.Background(), testIterations*time.Millisecond)
	defer cancel()

	wg := &sync.WaitGroup{}
	wg.Add(1)

	go handler.handleStdout(ctx, wg)

	// Send a message.
	stdoutChan <- []byte("test message")

	wg.Wait()
	// Should handle error gracefully (logged but not crash).
}

func TestHandler_Run(t *testing.T) {
	// Create mock stdin/stdout.
	stdinReader := strings.NewReader("input1\ninput2\n")

	var stdoutBuf bytes.Buffer

	stdoutWriter := bufio.NewWriter(&stdoutBuf)

	logger := testutil.NewTestLogger(t)
	stdinChan := make(chan []byte, 10)
	stdoutChan := make(chan []byte, 10)

	handler := &Handler{
		logger:     logger,
		stdinChan:  stdinChan,
		stdoutChan: stdoutChan,
		reader:     bufio.NewReader(stdinReader),
		writer:     stdoutWriter,
	}

	ctx, cancel := context.WithCancel(context.Background())
	wg := &sync.WaitGroup{}

	// Start handler.
	handler.Run(ctx, wg)

	// Process some messages.
	go func() {
		for msg := range stdinChan {
			// Echo back with prefix.
			response := append([]byte("echo: "), msg...)
			stdoutChan <- response
		}
	}()

	// Let it run briefly.
	time.Sleep(testIterations * time.Millisecond)

	// Cancel and wait.
	cancel()
	wg.Wait()

	// Check output.
	output := stdoutBuf.String()
	if !strings.Contains(output, "echo: input1") {
		t.Errorf("Expected to see 'echo: input1' in output, got: %s", output)
	}
}

func TestHandler_WriteErrorf(t *testing.T) {
	handler := &Handler{}

	// Capture stderr.
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	// Write error.
	handler.WriteErrorf("test error: %s", "details")

	// Restore stderr.
	_ = w.Close()

	os.Stderr = oldStderr

	// Read captured output.
	var buf bytes.Buffer

	if _, err := io.Copy(&buf, r); err != nil {
		t.Logf("Failed to copy data: %v", err)
	}

	output := buf.String()
	expected := "Error: test error: details\n"

	if output != expected {
		t.Errorf("Expected '%s', got '%s'", expected, output)
	}
}

func TestHandler_ConcurrentAccess(t *testing.T) {
	stdinChan := make(chan []byte, testIterations)
	stdoutChan := make(chan []byte, testIterations)

	handler := setupConcurrentHandler(t, stdinChan, stdoutChan)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wg := &sync.WaitGroup{}
	handler.Run(ctx, wg)

	var readWg sync.WaitGroup

	readCount := make([]int, 3)

	runConcurrentReaders(stdinChan, readCount, &readWg)
	runConcurrentWriters(stdoutChan, ctx)

	// Let it run.
	time.Sleep(300 * time.Millisecond)
	cancel()
	wg.Wait()
	readWg.Wait()

	validateConcurrentResults(t, readCount)
}

// setupConcurrentHandler creates a handler with large input data for concurrent testing.
func setupConcurrentHandler(t *testing.T, stdinChan, stdoutChan chan []byte) *Handler {
	t.Helper()

	logger := testutil.NewTestLogger(t)

	var inputLines []string
	for i := 0; i < testTimeout; i++ {
		inputLines = append(inputLines, fmt.Sprintf("line%d", i))
	}

	input := strings.Join(inputLines, "\n") + "\n"

	return &Handler{
		logger:     logger,
		stdinChan:  stdinChan,
		stdoutChan: stdoutChan,
		reader:     bufio.NewReader(strings.NewReader(input)),
		writer:     bufio.NewWriter(&bytes.Buffer{}),
	}
}

// runConcurrentReaders starts multiple concurrent reader goroutines.
func runConcurrentReaders(stdinChan chan []byte, readCount []int, readWg *sync.WaitGroup) {
	for i := 0; i < 3; i++ {
		readWg.Add(1)

		go func(id int) {
			defer readWg.Done()

			readCount[id] = processConcurrentReads(stdinChan)
		}(i)
	}
}

// processConcurrentReads processes messages from stdin channel and counts them.
func processConcurrentReads(stdinChan chan []byte) int {
	count := 0
	deadline := time.After(httpStatusInternalError * time.Millisecond)

	for {
		select {
		case msg, ok := <-stdinChan:
			if !ok {
				return count
			}

			if msg != nil {
				count++
			}
		case <-deadline:
			return count
		}
	}
}

// runConcurrentWriters starts multiple concurrent writer goroutines.
func runConcurrentWriters(stdoutChan chan []byte, ctx context.Context) {
	for i := 0; i < 20; i++ {
		go func(id int) {
			select {
			case stdoutChan <- []byte(fmt.Sprintf("output%d", id)):
			case <-ctx.Done():
			}
		}(i)
	}
}

// validateConcurrentResults verifies that some messages were processed.
func validateConcurrentResults(t *testing.T, readCount []int) {
	t.Helper()

	totalRead := 0
	for _, count := range readCount {
		totalRead += count
	}

	if totalRead == 0 {
		t.Error("Expected some messages to be read")
	}
}

// Helper types for testing.

type errorAfterNReader struct {
	reader io.Reader
	n      int
	count  int
	err    error
}

func (r *errorAfterNReader) Read(p []byte) (n int, err error) {
	if r.count >= r.n {
		return 0, r.err
	}

	r.count++

	return r.reader.Read(p)
}

type alwaysErrorWriter struct{}

func (w *alwaysErrorWriter) Write(p []byte) (n int, err error) {
	return 0, errors.New("write error")
}

func BenchmarkHandler_StdinProcessing(b *testing.B) {
	// Create input with many lines.
	var lines []string
	for i := 0; i < testMaxIterations; i++ {
		lines = append(lines, fmt.Sprintf("benchmark line %d with some data", i))
	}

	input := strings.Join(lines, "\n") + "\n"

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		b.StopTimer()

		logger := zap.NewNop()
		stdinChan := make(chan []byte, testIterations)
		handler := &Handler{
			logger:    logger,
			stdinChan: stdinChan,
			reader:    bufio.NewReader(strings.NewReader(input)),
		}

		ctx, cancel := context.WithCancel(context.Background())
		wg := &sync.WaitGroup{}
		wg.Add(1)

		// Drain channel.
		go func() {
			for range stdinChan {
			}
		}()

		b.StartTimer()
		handler.handleStdin(ctx, wg)
		b.StopTimer()

		cancel()
	}
}

func BenchmarkHandler_StdoutProcessing(b *testing.B) {
	messages := make([][]byte, testMaxIterations)
	for i := 0; i < testMaxIterations; i++ {
		messages[i] = []byte(fmt.Sprintf("benchmark output line %d", i))
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		b.StopTimer()

		var buf bytes.Buffer

		logger := zap.NewNop()
		stdoutChan := make(chan []byte, testIterations)
		handler := &Handler{
			logger:     logger,
			stdoutChan: stdoutChan,
			writer:     bufio.NewWriter(&buf),
		}

		ctx, cancel := context.WithCancel(context.Background())
		wg := &sync.WaitGroup{}
		wg.Add(1)

		go handler.handleStdout(ctx, wg)

		b.StartTimer()

	messageLoop:
		for _, msg := range messages {
			select {
			case stdoutChan <- msg:
			case <-ctx.Done():
				break messageLoop
			}
		}

		b.StopTimer()

		cancel()
		wg.Wait()
	}
}
