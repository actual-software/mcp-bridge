# Build stage
FROM golang:1.23-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates

# Set working directory
WORKDIR /build

# Copy main project go.mod (needed for replace directive)
COPY go.mod go.sum ../../

# Copy go mod files for gateway
COPY services/gateway/go.mod services/gateway/go.sum ./

# Copy router go mod files (needed for replace directive)  
COPY services/router/go.mod services/router/go.sum ../router/

# Download dependencies
RUN go mod download

# Copy source code
COPY services/gateway/ ./
COPY services/router/ ../router/
COPY pkg/ ../../pkg/

# Build arguments
ARG VERSION=v1.0.0
ARG BUILD_TIME=unknown
ARG GIT_COMMIT=unknown

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags "-X main.Version=${VERSION} -X main.BuildTime=${BUILD_TIME} -X main.GitCommit=${GIT_COMMIT} -s -w" \
    -o mcp-gateway \
    ./cmd/mcp-gateway

# Runtime stage
FROM alpine:3.19

# Install runtime dependencies with retry for slow networks
RUN set -ex && \
    for i in 1 2 3; do \
        apk add --no-cache ca-certificates tzdata curl && break || \
        (echo "Retry $i/3 failed, waiting 10s..." && sleep 10); \
    done

# Create non-root user
RUN adduser -D -g '' appuser

# Set working directory
WORKDIR /app

# Copy binary from builder
COPY --from=builder /build/mcp-gateway /app/

# Create directories for config and TLS
RUN mkdir -p /etc/mcp-gateway /etc/tls && \
    chown -R appuser:appuser /app /etc/mcp-gateway /etc/tls

# Switch to non-root user
USER appuser

# Expose ports
EXPOSE 8443 9090

# Health check
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:9090/healthz || exit 1

# Set entrypoint
ENTRYPOINT ["/app/mcp-gateway"]

# Default command
CMD ["--config", "/etc/mcp-gateway/gateway.yaml"]