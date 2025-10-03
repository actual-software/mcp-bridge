FROM golang:1.25-alpine AS builder

# Install dependencies
RUN apk add --no-cache git make gcc musl-dev bash curl redis

# Set working directory
WORKDIR /app

# Copy go mod files and all source first
COPY go.mod go.sum ./
COPY services/gateway/go.mod services/gateway/go.sum ./services/gateway/
COPY services/router/go.mod services/router/go.sum ./services/router/

# Download dependencies (disable workspace mode since e2e modules aren't copied)
RUN GOWORK=off go mod download

# Copy all source code needed for tests
COPY test/ ./test/
COPY pkg/ ./pkg/
COPY services/ ./services/

# Build test binaries using full module paths
# Use GOWORK=off to avoid workspace mode, and build from test directories directly
WORKDIR /app/test/smoke
RUN GOWORK=off go test -c -o /bin/smoke.test .

WORKDIR /app/test/performance
RUN GOWORK=off go test -c -o /bin/performance.test .

# Reset workdir for final stage
WORKDIR /app

# Runtime stage
FROM alpine:latest

RUN apk add --no-cache bash curl redis netcat-openbsd jq

WORKDIR /test

# Copy test binaries
COPY --from=builder /bin/*.test /bin/
COPY --from=builder /app/test/run-tests.sh ./
COPY --from=builder /app/test/Makefile ./

# Make scripts executable
RUN chmod +x run-tests.sh

# Default command
CMD ["./run-tests.sh", "-l", "standard", "-t", "all"]