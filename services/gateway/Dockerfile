# Build stage
FROM golang:1.25-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates

# Disable Go workspace mode in Docker build
ENV GOWORK=off

# Set working directory
WORKDIR /build

# Copy only go.mod and go.sum files first for better caching
# Dependencies change less frequently than source code
COPY go.mod go.sum ./
COPY services/gateway/go.mod services/gateway/go.sum ./services/gateway/
COPY services/router/go.mod services/router/go.sum ./services/router/

# Download dependencies with cache mount for faster rebuilds
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    cd services/gateway && go mod download

# Copy source code (this invalidates less frequently than go.mod changes)
COPY services/gateway/ ./services/gateway/
COPY services/router/ ./services/router/
COPY pkg/ ./pkg/

# Build arguments
ARG VERSION=v1.0.0
ARG BUILD_TIME
ARG GIT_COMMIT=unknown
ARG TARGETOS=linux
ARG TARGETARCH=amd64

# Build the binary with cache mounts for much faster compilation
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    BUILD_TIME_VAL="${BUILD_TIME:-$(date +%FT%T%z)}" && \
    cd services/gateway && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
    -ldflags "-X main.Version=${VERSION} -X main.BuildTime=${BUILD_TIME_VAL} -X main.GitCommit=${GIT_COMMIT} -s -w" \
    -o /build/mcp-gateway \
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
EXPOSE 8080 9090 9091

# Health check
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:9091/healthz || exit 1

# Set entrypoint
ENTRYPOINT ["/app/mcp-gateway"]

# Default command
CMD ["--config", "/etc/mcp-gateway/gateway.yaml"]