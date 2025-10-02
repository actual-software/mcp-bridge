package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"go.uber.org/zap"

	customerrors "github.com/actual-software/mcp-bridge/services/gateway/internal/errors"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/health"
	"github.com/actual-software/mcp-bridge/services/gateway/internal/metrics"
	"github.com/actual-software/mcp-bridge/services/gateway/pkg/wire"
)

// TCPHealthServer handles TCP health check requests.
type TCPHealthServer struct {
	port     int
	listener net.Listener
	checker  *health.Checker
	metrics  *metrics.Registry
	logger   *zap.Logger
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	mu       sync.Mutex
}

// CreateTCPHealthCheckServer creates a TCP-based health monitoring server.
func CreateTCPHealthCheckServer(
	port int,
	checker *health.Checker,
	metrics *metrics.Registry,
	logger *zap.Logger,
) *TCPHealthServer {
	ctx, cancel := context.WithCancel(context.Background())

	return &TCPHealthServer{
		port:    port,
		checker: checker,
		metrics: metrics,
		logger:  logger,
		ctx:     ctx,
		cancel:  cancel,
	}
}

// Start starts the TCP health check server.
func (s *TCPHealthServer) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.port == 0 {
		// TCP health check disabled
		return nil
	}

	// Create listener
	var err error

	lc := &net.ListenConfig{}

	s.listener, err = lc.Listen(context.Background(), "tcp", fmt.Sprintf(":%d", s.port))
	if err != nil {
		return customerrors.Wrap(err, "failed to start TCP health listener").
			WithComponent("tcp_health").
			WithOperation("start")
	}

	s.logger.Info("TCP health check server started", zap.Int("port", s.port))

	// Start accept loop
	s.wg.Add(1)

	go s.acceptLoop()

	return nil
}

// Stop stops the TCP health check server.
func (s *TCPHealthServer) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Cancel context to signal shutdown
	s.cancel()

	// Close listener
	if s.listener != nil {
		if err := s.listener.Close(); err != nil {
			s.logger.Error("Error closing TCP health listener", zap.Error(err))
		}
	}

	// Wait for accept loop to finish
	s.wg.Wait()

	s.logger.Info("TCP health check server stopped")

	return nil
}

// acceptLoop accepts incoming TCP health check connections.
func (s *TCPHealthServer) acceptLoop() {
	defer s.wg.Done()

	for {
		select {
		case <-s.ctx.Done():
			return
		default:
		}

		// Set accept deadline
		if tcpListener, ok := s.listener.(*net.TCPListener); ok {
			if err := tcpListener.SetDeadline(time.Now().Add(time.Second)); err != nil {
				s.logger.Warn("failed to set TCP health listener deadline", zap.Error(err))
			}
		}

		conn, err := s.listener.Accept()
		if err != nil {
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				continue
			}

			if s.ctx.Err() != nil {
				return
			}

			s.logger.Error("Failed to accept TCP health connection", zap.Error(err))

			continue
		}

		// Handle connection
		s.wg.Add(1)

		go func() {
			defer s.wg.Done()

			s.handleConnection(conn)
		}()
	}
}

// handleConnection handles a TCP health check connection.
func (s *TCPHealthServer) handleConnection(conn net.Conn) {
	defer s.closeConnection(conn)

	remoteAddr := conn.RemoteAddr().String()
	s.logger.Debug("TCP health check connection", zap.String("remote", remoteAddr))

	// Setup transport
	transport := s.setupTransport(conn)
	if transport == nil {
		return
	}
	defer s.closeTransport(transport)

	// Receive and validate message
	msg, err := s.receiveHealthMessage(transport, remoteAddr)
	if err != nil {
		return // Error already logged
	}

	// Process health check and send response
	s.processHealthCheck(transport, msg, remoteAddr)
}

// closeConnection safely closes the network connection.
func (s *TCPHealthServer) closeConnection(conn net.Conn) {
	if err := conn.Close(); err != nil {
		s.logger.Debug("Error closing TCP health connection", zap.Error(err))
	}
}

// closeTransport safely closes the wire transport.
func (s *TCPHealthServer) closeTransport(transport *wire.Transport) {
	if err := transport.Close(); err != nil {
		s.logger.Debug("Error closing TCP health transport", zap.Error(err))
	}
}

// setupTransport creates and configures the wire transport.
func (s *TCPHealthServer) setupTransport(conn net.Conn) *wire.Transport {
	transport := wire.NewTransport(conn)

	// Set read timeout
	if err := transport.SetReadDeadline(time.Now().Add(defaultMaxConnections * time.Second)); err != nil {
		s.logger.Debug("Failed to set read deadline for health check", zap.Error(err))
	}

	return transport
}

// receiveHealthMessage receives and validates the health check message.
func (s *TCPHealthServer) receiveHealthMessage(transport *wire.Transport, remoteAddr string) (interface{}, error) {
	msgType, msg, err := transport.ReceiveMessage()
	if err != nil {
		s.logger.Error("Failed to receive health check message", zap.Error(err))
		s.metrics.IncrementTCPProtocolErrors("health_receive_error")

		return nil, err
	}

	// Validate message type
	if msgType != wire.MessageTypeHealthCheck {
		s.logger.Warn("Unexpected message type on health port",
			zap.Uint16("type", uint16(msgType)),
			zap.String("remote", remoteAddr))
		s.metrics.IncrementTCPProtocolErrors("health_invalid_message")

		return nil, customerrors.New(customerrors.TypeValidation, "invalid message type").
			WithComponent("tcp_health").
			WithOperation("extract_request")
	}

	s.metrics.IncrementTCPMessages("inbound", "health_check")

	return msg, nil
}

// processHealthCheck processes the health check and sends response.
func (s *TCPHealthServer) processHealthCheck(transport *wire.Transport, msg interface{}, remoteAddr string) {
	// Get health status
	status := s.checker.GetStatus()

	// Build response based on request type
	healthResponse := s.buildHealthResponse(&status, msg)

	// Send the response
	if err := s.sendHealthResponse(transport, healthResponse); err != nil {
		return // Error already logged
	}

	s.logger.Debug("TCP health check completed",
		zap.String("remote", remoteAddr),
		zap.Bool("healthy", status.Healthy))
}

// buildHealthResponse builds the appropriate health response.
func (s *TCPHealthServer) buildHealthResponse(status *health.Status, msg interface{}) map[string]interface{} {
	// Default detailed response
	response := map[string]interface{}{
		"healthy":   status.Healthy,
		"checks":    status.Checks,
		"timestamp": time.Now().Unix(),
	}

	// Check for specific check type request
	if req, ok := msg.(map[string]interface{}); ok {
		if checkType, ok := req["check"].(string); ok && checkType == "simple" {
			// Return simple response
			return map[string]interface{}{
				"healthy": status.Healthy,
			}
		}
	}

	return response
}

// sendHealthResponse marshals and sends the health response.
func (s *TCPHealthServer) sendHealthResponse(transport *wire.Transport, response map[string]interface{}) error {
	// Marshal response
	payload, err := json.Marshal(response)
	if err != nil {
		s.logger.Error("Failed to marshal health response", zap.Error(err))

		return err
	}

	// Create response frame
	frame := &wire.Frame{
		Version:     wire.CurrentVersion,
		MessageType: wire.MessageTypeHealthCheck,
		Payload:     payload,
	}

	// Send frame
	if err := frame.Encode(transport.GetWriter()); err != nil {
		s.logger.Error("Failed to send health response", zap.Error(err))
		s.metrics.IncrementTCPProtocolErrors("health_send_error")

		return err
	}

	// Flush transport
	if err := transport.Flush(); err != nil {
		s.logger.Error("Failed to flush health response", zap.Error(err))
		s.metrics.IncrementTCPProtocolErrors("health_flush_error")

		return err
	}

	s.metrics.IncrementTCPMessages("outbound", "health_check")

	return nil
}
