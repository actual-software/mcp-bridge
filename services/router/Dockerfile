# Build stage
FROM golang:1.25-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates

# Disable Go workspace mode in Docker build
ENV GOWORK=off

# Set working directory
WORKDIR /build

# Copy the entire project structure to satisfy module dependencies
COPY . /src/
WORKDIR /src/services/router

# Download dependencies
RUN go mod download
RUN go mod tidy

# Build arguments
ARG VERSION=v1.0.0
ARG BUILD_TIME
ARG GIT_COMMIT=unknown
ARG TARGETOS=linux
ARG TARGETARCH=amd64

# Build the binary
RUN BUILD_TIME_VAL="${BUILD_TIME:-$(date +%FT%T%z)}" && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
    -ldflags "-X main.Version=${VERSION} -X main.BuildTime=${BUILD_TIME_VAL} -X main.GitCommit=${GIT_COMMIT} -s -w" \
    -o mcp-router \
    ./cmd/mcp-router

# Runtime stage
FROM alpine:3.19

# Install runtime dependencies
# Install runtime dependencies with retry for slow networks
RUN set -ex && \
    for i in 1 2 3; do \
        apk add --no-cache ca-certificates tzdata curl wget && break || \
        (echo "Retry $i/3 failed, waiting 10s..." && sleep 10); \
    done

# Create non-root user
RUN adduser -D -g '' appuser

# Set working directory
WORKDIR /app

# Copy binary from builder
COPY --from=builder /src/services/router/mcp-router /app/

# Create directories for config
RUN mkdir -p /etc/mcp-router && \
    chown -R appuser:appuser /app /etc/mcp-router

# Switch to non-root user
USER appuser

# Expose metrics port
EXPOSE 9091

# Health check
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:9091/metrics || exit 1

# Set entrypoint
ENTRYPOINT ["/app/mcp-router"]

# Default command
CMD ["--config", "/etc/mcp-router/router.yaml"]