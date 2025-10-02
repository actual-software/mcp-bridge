FROM golang:1.24-alpine AS builder

# Install dependencies
RUN apk add --no-cache git make gcc musl-dev bash curl redis

# Set working directory
WORKDIR /test

# Copy go mod files
COPY go.mod go.sum ./
COPY services/gateway/go.mod services/gateway/go.sum ./services/gateway/
COPY services/router/go.mod services/router/go.sum ./services/router/

# Download dependencies
RUN go mod download

# Copy test source code
COPY test/ ./test/
COPY pkg/ ./pkg/
COPY internal/ ./internal/

# Build test binaries if needed
RUN go test -c ./test/smoke -o /bin/smoke.test
RUN go test -c ./test/contract -o /bin/contract.test
RUN go test -c ./test/performance -o /bin/performance.test

# Runtime stage
FROM alpine:latest

RUN apk add --no-cache bash curl redis netcat-openbsd jq

WORKDIR /test

# Copy test binaries
COPY --from=builder /bin/*.test /bin/
COPY test/run-tests.sh ./
COPY test/Makefile ./

# Make scripts executable
RUN chmod +x run-tests.sh

# Default command
CMD ["./run-tests.sh", "-l", "standard", "-t", "all"]