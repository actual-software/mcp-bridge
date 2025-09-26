// Package main provides a standalone entry point for the Weather MCP server
package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	weather "weather-test"

	"go.uber.org/zap"
)

const defaultServerPort = 8080

func main() {
	// Parse flags
	port := flag.Int("port", defaultServerPort, "Server port")
	logLevel := flag.String("log-level", "info", "Log level (debug, info, warn, error)")
	version := flag.Bool("version", false, "Show version")
	health := flag.Bool("health", false, "Health check")
	flag.Parse()

	if *version {
		fmt.Println("Weather MCP Server v1.0.0")

		return
	}

	if *health {
		fmt.Println("OK")

		return
	}

	// Setup logger and server
	logger := setupLogger(*logLevel)
	httpServer := setupServer(*port, logger)

	// Run server with graceful shutdown
	runServerWithShutdown(logger, httpServer)
}

func setupLogger(logLevel string) *zap.Logger {
	config := zap.NewProductionConfig()

	switch logLevel {
	case "debug":
		config.Level = zap.NewAtomicLevelAt(zap.DebugLevel)
	case "warn":
		config.Level = zap.NewAtomicLevelAt(zap.WarnLevel)
	case "error":
		config.Level = zap.NewAtomicLevelAt(zap.ErrorLevel)
	default:
		config.Level = zap.NewAtomicLevelAt(zap.InfoLevel)
	}

	logger, err := config.Build()
	if err != nil {
		log.Fatal("Failed to create logger:", err)
	}

	return logger
}

func setupServer(port int, logger *zap.Logger) *weather.Server {
	// Create weather server
	weatherServer := weather.NewWeatherMCPServer(logger)
	_ = weatherServer // prevent unused variable warning

	// Create HTTP server
	serverConfig := weather.DefaultServerConfig()
	serverConfig.Port = port

	httpServer, err := weather.NewServer(serverConfig)
	if err != nil {
		logger.Fatal("Failed to create HTTP server", zap.Error(err))
	}

	return httpServer
}

func runServerWithShutdown(logger *zap.Logger, httpServer *weather.Server) {
	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start server in goroutine
	go func() {
		logger.Info("Starting Weather MCP server")

		if err := httpServer.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Fatal("Server failed", zap.Error(err))
		}
	}()

	// Wait for signal
	sig := <-sigChan
	logger.Info("Received signal, shutting down", zap.String("signal", sig.String()))

	// Graceful shutdown
	logger.Info("Shutdown complete")
}
