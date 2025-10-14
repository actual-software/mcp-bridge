# Deploying MCP Gateway to actualai-staging

This guide walks through deploying mcp-gateway to the actualai-staging EKS cluster.

## Prerequisites

1. **kubectl** configured for actualai-staging cluster
2. **helm** (v3+) installed
3. **Docker** access to build and push images to GHCR
4. **AWS CLI** configured for actualai account

## Step 1: Verify Docker Images

The images are already built and pushed via GitHub Actions when you create a release tag.

Current release: **v1.0.0-rc2**

Images:
- `ghcr.io/actual-software/mcp-bridge-gateway:v1.0.0-rc2`
- `ghcr.io/actual-software/mcp-bridge-router:v1.0.0-rc2`

To verify (requires GHCR authentication):
```bash
# Authenticate to GHCR
gh auth token | docker login ghcr.io -u poiley --password-stdin

# Verify images exist
docker manifest inspect ghcr.io/actual-software/mcp-bridge-gateway:v1.0.0-rc2
```

**Note**: These are private images in GHCR, so you'll need to create an imagePullSecret in Kubernetes (see Step 1b).

## Step 1b: Create imagePullSecret for Private GHCR Images

Since the images are private in GHCR, create a Kubernetes secret for authentication:

```bash
# Create namespace first
kubectl create namespace mcp

# Create imagePullSecret using GitHub token
kubectl create secret docker-registry ghcr-secret \
  --docker-server=ghcr.io \
  --docker-username=poiley \
  --docker-password=$(gh auth token) \
  --namespace=mcp

# Verify secret was created
kubectl get secret ghcr-secret -n mcp
```

**Alternative**: Make the GHCR packages public:
- Go to https://github.com/orgs/actual-software/packages
- Find `mcp-bridge-gateway` and `mcp-bridge-router`
- Settings → Change visibility → Public

## Step 2: Generate Secrets

```bash
# Generate JWT secret key (base64 encoded random string)
JWT_SECRET=$(openssl rand -base64 32)
echo "JWT_SECRET=$JWT_SECRET"

# Generate Redis password
REDIS_PASSWORD=$(openssl rand -base64 24)
echo "REDIS_PASSWORD=$REDIS_PASSWORD"

# Save to a secure location or export for next step
export JWT_SECRET
export REDIS_PASSWORD
```

## Step 3: Configure TLS Certificates

### Option A: Use AWS Certificate Manager (Recommended)

If using AWS ACM with NLB:

1. Create/import certificate in ACM
2. Update `values-actualai-staging.yaml`:
   ```yaml
   gateway:
     service:
       annotations:
         service.beta.kubernetes.io/aws-load-balancer-ssl-cert: "arn:aws:acm:us-west-2:324037314017:certificate/YOUR-CERT-ID"
         service.beta.kubernetes.io/aws-load-balancer-backend-protocol: "tcp"

   tls:
     enabled: false  # TLS termination at NLB
   ```

### Option B: Use Self-Signed Certificates (Development/Testing)

```bash
# Create namespace
kubectl create namespace mcp

# Generate self-signed certificate
openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
  -keyout tls.key \
  -out tls.crt \
  -subj "/CN=mcp-gateway.staging.actualai.io/O=ActualAI"

# Create TLS secret
kubectl create secret tls mcp-gateway-tls \
  --cert=tls.crt \
  --key=tls.key \
  -n mcp

# Clean up local files
rm tls.key tls.crt
```

### Option C: Use cert-manager (Production)

```bash
# Install cert-manager (if not already installed)
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.13.0/cert-manager.yaml

# Create certificate issuer and certificate
# (Add cert-manager configuration files as needed)
```

## Step 4: Deploy with Helm

```bash
# Ensure you're on the right context
kubectl config use-context arn:aws:eks:us-west-2:324037314017:cluster/actualai-staging

# Namespace should already exist from Step 1b

# Deploy using Helm
cd deployment/helm/mcp-bridge

helm install mcp-gateway . \
  --namespace mcp \
  --values values-actualai-staging.yaml \
  --set-string secrets.jwtSecretKey="$JWT_SECRET" \
  --set-string secrets.redisPassword="$REDIS_PASSWORD" \
  --wait \
  --timeout 10m

# Or for upgrade
helm upgrade mcp-gateway . \
  --namespace mcp \
  --values values-actualai-staging.yaml \
  --set-string secrets.jwtSecretKey="$JWT_SECRET" \
  --set-string secrets.redisPassword="$REDIS_PASSWORD" \
  --wait \
  --timeout 10m
```

## Step 5: Verify Deployment

```bash
# Check pods
kubectl get pods -n mcp -l app.kubernetes.io/name=mcp-gateway

# Check services
kubectl get svc -n mcp

# Get LoadBalancer endpoint
kubectl get svc -n mcp mcp-bridge-gateway -o jsonpath='{.status.loadBalancer.ingress[0].hostname}'

# Check logs
kubectl logs -n mcp -l app.kubernetes.io/name=mcp-gateway --tail=100 -f

# Check health endpoints
kubectl port-forward -n mcp svc/mcp-bridge-gateway 9090:9090
curl http://localhost:9090/health
curl http://localhost:9090/metrics
```

## Step 6: Configure DNS (if needed)

```bash
# Get the NLB hostname
NLB_HOSTNAME=$(kubectl get svc -n mcp mcp-bridge-gateway -o jsonpath='{.status.loadBalancer.ingress[0].hostname}')

echo "Create a CNAME record:"
echo "  mcp-gateway.staging.actualai.io -> $NLB_HOSTNAME"
```

## Step 7: Test Connection

```bash
# Get LoadBalancer URL
GATEWAY_URL=$(kubectl get svc -n mcp mcp-bridge-gateway -o jsonpath='{.status.loadBalancer.ingress[0].hostname}')

# Test WebSocket connection (requires valid JWT)
wscat -c wss://$GATEWAY_URL:8443/ws

# Or test with curl (health check)
kubectl port-forward -n mcp svc/mcp-bridge-gateway 9090:9090 &
curl http://localhost:9090/health
```

## Troubleshooting

### Pods not starting

```bash
# Check pod status
kubectl describe pod -n mcp -l app.kubernetes.io/name=mcp-gateway

# Check events
kubectl get events -n mcp --sort-by='.lastTimestamp'

# Check logs
kubectl logs -n mcp -l app.kubernetes.io/name=mcp-gateway --previous
```

### Image pull errors

```bash
# Verify image exists
docker manifest inspect ghcr.io/actual-software/mcp-bridge/gateway:1.0.0

# Create image pull secret if needed (for private registry)
kubectl create secret docker-registry ghcr-secret \
  --docker-server=ghcr.io \
  --docker-username=USERNAME \
  --docker-password=$GITHUB_TOKEN \
  -n mcp
```

### TLS certificate issues

```bash
# Check secret
kubectl get secret mcp-gateway-tls -n mcp -o yaml

# Verify certificate
kubectl get secret mcp-gateway-tls -n mcp -o jsonpath='{.data.tls\.crt}' | base64 -d | openssl x509 -text -noout
```

### Redis connection issues

```bash
# Check Redis pod
kubectl get pods -n mcp -l app.kubernetes.io/name=redis

# Check Redis logs
kubectl logs -n mcp -l app.kubernetes.io/name=redis

# Test Redis connection
kubectl exec -it -n mcp mcp-bridge-gateway-xxxxx -- sh
# Inside pod:
# wget -O- redis://mcp-gateway-redis-master:6379
```

## Rollback

```bash
# List releases
helm list -n mcp

# Rollback to previous version
helm rollback mcp-gateway -n mcp

# Or uninstall completely
helm uninstall mcp-gateway -n mcp
kubectl delete namespace mcp
```

## Monitoring

```bash
# View metrics
kubectl port-forward -n mcp svc/mcp-bridge-gateway 9090:9090
curl http://localhost:9090/metrics

# Access Grafana (if configured)
# Import dashboard from deployment/monitoring/grafana/dashboards/

# View Prometheus metrics (if configured)
kubectl port-forward -n mcp svc/prometheus-server 9091:80
```

## Security Notes

1. **Secrets Management**: Consider using AWS Secrets Manager or HashiCorp Vault instead of Kubernetes secrets
2. **Network Policies**: Review and adjust network policies in `values-actualai-staging.yaml`
3. **TLS**: Always use valid certificates in production
4. **RBAC**: Review service account permissions
5. **Image Scanning**: Ensure images are scanned before deployment

## Clean Up

```bash
# Delete deployment
helm uninstall mcp-gateway -n mcp

# Delete namespace
kubectl delete namespace mcp

# Delete TLS secret (if exists outside namespace)
kubectl delete secret mcp-gateway-tls
```
