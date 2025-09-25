#!/bin/bash
# Pre-pull Docker images for E2E tests to avoid timeouts on slow networks

echo "Pre-pulling Docker images for E2E tests..."
echo "This may take a while on slow connections..."

# Base images used in Dockerfiles
IMAGES=(
    "golang:1.23-alpine"
    "alpine:3.19"
    "alpine:latest"
    "busybox:1.35"
    "kindest/node:v1.33.1"
    "scratch"
)

# Pull images with retry logic
for IMAGE in "${IMAGES[@]}"; do
    echo "Pulling $IMAGE..."
    for i in 1 2 3; do
        if docker pull "$IMAGE"; then
            echo "✓ Successfully pulled $IMAGE"
            break
        else
            echo "✗ Failed to pull $IMAGE (attempt $i/3)"
            if [ $i -lt 3 ]; then
                echo "  Waiting 30s before retry..."
                sleep 30
            fi
        fi
    done
done

echo "Pre-pull complete!"
echo "Note: Run this script before running E2E tests on slow networks"