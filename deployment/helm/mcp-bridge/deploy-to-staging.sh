#!/bin/bash
set -euo pipefail

# Deploy mcp-gateway to actualai-staging
# Usage: ./deploy-to-staging.sh

NAMESPACE="mcp"
RELEASE_NAME="mcp-gateway"
CLUSTER_CONTEXT="arn:aws:eks:us-west-2:324037314017:cluster/actualai-staging"

echo "🚀 Deploying MCP Gateway to actualai-staging"
echo "================================================"

# Switch to correct context
echo "📍 Switching to actualai-staging context..."
kubectl config use-context "$CLUSTER_CONTEXT"

# Create namespace if it doesn't exist
if ! kubectl get namespace "$NAMESPACE" &> /dev/null; then
    echo "📦 Creating namespace $NAMESPACE..."
    kubectl create namespace "$NAMESPACE"
else
    echo "✓ Namespace $NAMESPACE already exists"
fi

# Create imagePullSecret if it doesn't exist
if ! kubectl get secret ghcr-secret -n "$NAMESPACE" &> /dev/null; then
    echo "🔐 Creating imagePullSecret for GHCR..."
    if ! command -v gh &> /dev/null; then
        echo "❌ GitHub CLI (gh) not found. Please install it first."
        echo "   brew install gh"
        exit 1
    fi

    kubectl create secret docker-registry ghcr-secret \
        --docker-server=ghcr.io \
        --docker-username=poiley \
        --docker-password="$(gh auth token)" \
        --namespace="$NAMESPACE"
    echo "✓ imagePullSecret created"
else
    echo "✓ imagePullSecret already exists"
fi

# Generate secrets if not provided
if [ -z "${JWT_SECRET:-}" ]; then
    echo "🔑 Generating JWT secret..."
    JWT_SECRET=$(openssl rand -base64 32)
fi

if [ -z "${REDIS_PASSWORD:-}" ]; then
    echo "🔑 Generating Redis password..."
    REDIS_PASSWORD=$(openssl rand -base64 24)
fi

# Create TLS secret if it doesn't exist (self-signed for testing)
if ! kubectl get secret mcp-gateway-tls -n "$NAMESPACE" &> /dev/null; then
    echo "🔒 Creating self-signed TLS certificate..."
    TEMP_DIR=$(mktemp -d)
    openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
        -keyout "$TEMP_DIR/tls.key" \
        -out "$TEMP_DIR/tls.crt" \
        -subj "/CN=mcp-gateway.staging.actualai.io/O=ActualAI" \
        2>/dev/null

    kubectl create secret tls mcp-gateway-tls \
        --cert="$TEMP_DIR/tls.crt" \
        --key="$TEMP_DIR/tls.key" \
        -n "$NAMESPACE"

    rm -rf "$TEMP_DIR"
    echo "✓ TLS secret created (self-signed)"
else
    echo "✓ TLS secret already exists"
fi

# Deploy with Helm
echo ""
echo "🎯 Deploying Helm chart..."
cd "$(dirname "$0")"

if helm list -n "$NAMESPACE" | grep -q "$RELEASE_NAME"; then
    echo "📦 Upgrading existing release..."
    helm upgrade "$RELEASE_NAME" . \
        --namespace "$NAMESPACE" \
        --values values-actualai-staging.yaml \
        --set-string secrets.jwtSecretKey="$JWT_SECRET" \
        --set-string secrets.redisPassword="$REDIS_PASSWORD" \
        --wait \
        --timeout 10m
else
    echo "📦 Installing new release..."
    helm install "$RELEASE_NAME" . \
        --namespace "$NAMESPACE" \
        --values values-actualai-staging.yaml \
        --set-string secrets.jwtSecretKey="$JWT_SECRET" \
        --set-string secrets.redisPassword="$REDIS_PASSWORD" \
        --wait \
        --timeout 10m
fi

echo ""
echo "✅ Deployment complete!"
echo ""
echo "📊 Status:"
kubectl get pods -n "$NAMESPACE" -l app.kubernetes.io/name=mcp-gateway
echo ""
kubectl get svc -n "$NAMESPACE" -l app.kubernetes.io/name=mcp-gateway
echo ""

# Get LoadBalancer endpoint
echo "🌐 LoadBalancer endpoint:"
LB_HOSTNAME=$(kubectl get svc -n "$NAMESPACE" "$RELEASE_NAME-gateway" -o jsonpath='{.status.loadBalancer.ingress[0].hostname}' 2>/dev/null || echo "pending...")
echo "   $LB_HOSTNAME"
echo ""

echo "💡 To check logs:"
echo "   kubectl logs -n $NAMESPACE -l app.kubernetes.io/name=mcp-gateway -f"
echo ""
echo "💡 To check health:"
echo "   kubectl port-forward -n $NAMESPACE svc/$RELEASE_NAME-gateway 9090:9090"
echo "   curl http://localhost:9090/health"
echo ""
echo "💡 Saved secrets (keep these safe!):"
echo "   JWT_SECRET=$JWT_SECRET"
echo "   REDIS_PASSWORD=$REDIS_PASSWORD"
