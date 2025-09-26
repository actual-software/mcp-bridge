package direct

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
)

// StdioProcessBuilder handles process initialization for StdioClient.
type StdioProcessBuilder struct {
	client *StdioClient
	cmd    *exec.Cmd
}

// InitializeStdioProcess creates a new StdioProcessBuilder.
func InitializeStdioProcess(client *StdioClient) *StdioProcessBuilder {
	return &StdioProcessBuilder{
		client: client,
	}
}

// BuildAndStartProcess builds and starts the stdio process.
func (b *StdioProcessBuilder) BuildAndStartProcess(ctx context.Context) error {
	if err := b.createCommand(ctx); err != nil {
		return err
	}

	if err := b.configureEnvironment(); err != nil {
		return err
	}

	pipes, err := b.createPipes()
	if err != nil {
		return err
	}

	if err := b.configurePipes(pipes); err != nil {
		b.closePipes(pipes)

		return err
	}

	if err := b.startProcessExecution(); err != nil {
		b.closePipes(pipes)

		return err
	}

	b.client.cmd = b.cmd
	b.startBackgroundMonitoring()

	return nil
}

func (b *StdioProcessBuilder) createCommand(ctx context.Context) error {
	if len(b.client.config.Command) == 0 {
		return errors.New("command not specified")
	}

	// Command from trusted configuration
	b.cmd = exec.CommandContext(ctx, b.client.config.Command[0], b.client.config.Command[1:]...)

	if b.client.config.WorkingDir != "" {
		b.cmd.Dir = b.client.config.WorkingDir
	}

	return nil
}

func (b *StdioProcessBuilder) configureEnvironment() error {
	b.cmd.Env = os.Environ()

	for key, value := range b.client.config.Env {
		b.cmd.Env = append(b.cmd.Env, fmt.Sprintf("%s=%s", key, value))
	}

	return nil
}

// ProcessPipes holds the created pipes.
type ProcessPipes struct {
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser
}

func (b *StdioProcessBuilder) createPipes() (*ProcessPipes, error) {
	stdin, err := b.cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	stdout, err := b.cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()

		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := b.cmd.StderrPipe()
	if err != nil {
		_ = stdin.Close()
		_ = stdout.Close()

		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	return &ProcessPipes{
		stdin:  stdin,
		stdout: stdout,
		stderr: stderr,
	}, nil
}

func (b *StdioProcessBuilder) configurePipes(pipes *ProcessPipes) error {
	b.client.stdin = pipes.stdin
	b.client.stdout = pipes.stdout
	b.client.stderr = pipes.stderr

	b.configureStdinBuffering(pipes.stdin)
	b.configureStdoutBuffering(pipes.stdout)

	return nil
}

func (b *StdioProcessBuilder) configureStdinBuffering(stdin io.WriteCloser) {
	if b.client.config.Performance.EnableBufferedIO {
		b.setupBufferedStdin(stdin)
	} else {
		b.setupUnbufferedStdin(stdin)
	}
}

func (b *StdioProcessBuilder) setupBufferedStdin(stdin io.WriteCloser) {
	b.client.stdinBuf = bufio.NewWriterSize(stdin, b.client.config.Performance.StdinBufferSize)

	if b.shouldUseMemoryOptimizer() {
		b.client.stdinEncoder = b.client.memoryOptimizer.GetJSONEncoder(b.client.stdinBuf)
	} else {
		b.client.stdinEncoder = json.NewEncoder(b.client.stdinBuf)
	}
}

func (b *StdioProcessBuilder) setupUnbufferedStdin(stdin io.WriteCloser) {
	if b.shouldUseMemoryOptimizer() {
		buf := bufio.NewWriterSize(stdin, defaultBufferSize)
		b.client.stdinBuf = buf
		b.client.stdinEncoder = b.client.memoryOptimizer.GetJSONEncoder(buf)
	} else {
		b.client.stdinEncoder = json.NewEncoder(stdin)
	}
}

func (b *StdioProcessBuilder) configureStdoutBuffering(stdout io.ReadCloser) {
	if b.client.config.Performance.EnableBufferedIO {
		b.client.stdoutBuf = bufio.NewReaderSize(stdout, b.client.config.Performance.StdoutBufferSize)
		b.client.stdoutDecoder = json.NewDecoder(b.client.stdoutBuf)
	} else {
		b.client.stdoutDecoder = json.NewDecoder(stdout)
	}
}

func (b *StdioProcessBuilder) shouldUseMemoryOptimizer() bool {
	return b.client.memoryOptimizer != nil && b.client.config.Performance.ReuseEncoders
}

func (b *StdioProcessBuilder) startProcessExecution() error {
	if err := b.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start process: %w", err)
	}

	return nil
}

func (b *StdioProcessBuilder) closePipes(pipes *ProcessPipes) {
	if pipes.stdin != nil {
		_ = pipes.stdin.Close()
	}

	if pipes.stdout != nil {
		_ = pipes.stdout.Close()
	}

	if pipes.stderr != nil {
		_ = pipes.stderr.Close()
	}
}

func (b *StdioProcessBuilder) startBackgroundMonitoring() {
	b.client.wg.Add(1)

	go b.client.monitorStderr()
}
