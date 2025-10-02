# MCP Bridge Helm Chart

This Helm chart deploys MCP Bridge with universal protocol support (Model Context Protocol Gateway and Router) on a Kubernetes cluster using the Helm package manager, providing comprehensive protocol conversion and intelligent routing capabilities.

## Prerequisites

- Kubernetes 1.19+
- Helm 3.8.0+
- PV provisioner support in the underlying infrastructure

## Installing the Chart

To install the chart with the release name `my-mcp-bridge`:

```bash
helm install my-mcp-bridge ./helm/mcp-bridge
```

The command deploys MCP Bridge on the Kubernetes cluster in the default configuration. The [Parameters](#parameters) section lists the parameters that can be configured during installation.

## Uninstalling the Chart

To uninstall/delete the `my-mcp-bridge` deployment:

```bash
helm delete my-mcp-bridge
```

## Parameters

### Global parameters

| Name                           | Description                                     | Value |
| ------------------------------ | ----------------------------------------------- | ----- |
| `global.imageRegistry`         | Global Docker image registry                    | `""`  |
| `global.imagePullSecrets`      | Global Docker registry secret names as an array| `[]`  |

### Common parameters

| Name                | Description                          | Value |
| ------------------- | ------------------------------------ | ----- |
| `nameOverride`      | String to partially override names   | `""`  |
| `fullnameOverride`  | String to fully override names       | `""`  |

### Image parameters

| Name                | Description                      | Value                          |
| ------------------- | -------------------------------- | ------------------------------ |
| `image.repository`  | Image repository                 | `ghcr.io/actual-software/mcp-bridge`   |
| `image.pullPolicy`  | Image pull policy                | `IfNotPresent`                 |
| `image.tag`         | Image tag                        | `1.0.0-rc1`                    |

### Service Account parameters

| Name                          | Description                                           | Value  |
| ----------------------------- | ----------------------------------------------------- | ------ |
| `serviceAccount.create`       | Specifies whether a service account should be created| `true` |
| `serviceAccount.annotations`  | Annotations to add to the service account            | `{}`   |
| `serviceAccount.name`         | The name of the service account to use.              | `""`   |

### Gateway parameters (Universal Protocol Support)

| Name                                 | Description                               | Value             |
| ------------------------------------ | ----------------------------------------- | ----------------- |
| `gateway.enabled`                    | Enable Gateway deployment                 | `true`            |
| `gateway.replicaCount`               | Number of Gateway replicas                | `2`               |
| `gateway.image.repository`           | Gateway image repository                  | `ghcr.io/actual-software/mcp-bridge/gateway` |
| `gateway.image.tag`                  | Gateway image tag                         | `1.0.0-rc1`       |
| `gateway.service.type`               | Gateway service type                      | `ClusterIP`       |
| `gateway.service.port`               | Gateway WebSocket service port            | `8443`            |
| `gateway.service.tcpPort`            | Gateway TCP Binary service port           | `8444`            |
| `gateway.service.stdioSocket`        | Gateway stdio Unix socket path           | `/tmp/mcp-gateway.sock` |
| `gateway.protocols.enabled`         | Enable all protocol support              | `["websocket","tcp_binary","stdio","http"]` |
| `gateway.protocols.autoDetect`       | Enable protocol auto-detection           | `true`            |
| `gateway.protocols.crossLoadBalance` | Enable cross-protocol load balancing     | `true`            |
| `gateway.ingress.enabled`            | Enable ingress record generation          | `false`           |
| `gateway.resources.limits.cpu`       | Gateway CPU limit                         | `1000m`           |
| `gateway.resources.limits.memory`    | Gateway memory limit                      | `512Mi`           |
| `gateway.autoscaling.enabled`        | Enable horizontal pod autoscaler          | `true`            |
| `gateway.autoscaling.minReplicas`    | Minimum number of Gateway replicas       | `2`               |
| `gateway.autoscaling.maxReplicas`    | Maximum number of Gateway replicas       | `10`              |

### Router parameters (Direct Protocol Support)

| Name                                 | Description                               | Value             |
| ------------------------------------ | ----------------------------------------- | ----------------- |
| `router.enabled`                     | Enable Router deployment                  | `true`            |
| `router.replicaCount`                | Number of Router replicas                 | `1`               |
| `router.image.repository`            | Router image repository                   | `ghcr.io/actual-software/mcp-bridge/router` |
| `router.image.tag`                   | Router image tag                          | `1.0.0-rc1`       |
| `router.service.type`                | Router service type                       | `ClusterIP`       |
| `router.service.port`                | Router service port                       | `9091`            |
| `router.directClients.enabled`       | Enable direct protocol clients           | `true`            |
| `router.directClients.protocols`     | Supported direct protocols               | `["stdio","websocket","http","sse"]` |
| `router.directClients.autoDetect`    | Enable protocol auto-detection           | `true`            |
| `router.directClients.pooling`       | Enable connection pooling                | `true`            |
| `router.directClients.memoryOpt`     | Enable memory optimization               | `true`            |
| `router.performance.observability`   | Enable performance observability         | `true`            |
| `router.resources.limits.cpu`        | Router CPU limit                          | `500m`            |
| `router.resources.limits.memory`     | Router memory limit                       | `256Mi`           |

### Security parameters

| Name                                    | Description                                    | Value     |
| --------------------------------------- | ---------------------------------------------- | --------- |
| `podSecurityContext.runAsNonRoot`       | Set containers' Security Context runAsNonRoot | `true`    |
| `podSecurityContext.runAsUser`          | Set containers' Security Context runAsUser    | `65534`   |
| `securityContext.allowPrivilegeEscalation` | Set container's privilege escalation       | `false`   |
| `securityContext.capabilities.drop`     | List of capabilities to be dropped            | `["ALL"]` |
| `securityContext.readOnlyRootFilesystem`| Set container's Security Context readOnlyRootFilesystem | `true` |

### TLS parameters

| Name                    | Description                      | Value                |
| ----------------------- | -------------------------------- | -------------------- |
| `tls.enabled`           | Enable TLS configuration         | `true`               |
| `tls.secretName`        | Name of the TLS secret           | `mcp-bridge-tls`     |
| `tls.create`            | Create TLS secret automatically  | `true`               |

### Monitoring parameters (Universal Protocol Metrics)

| Name                              | Description                           | Value     |
| --------------------------------- | ------------------------------------- | --------- |
| `serviceMonitor.enabled`          | Create ServiceMonitor resource        | `true`    |
| `serviceMonitor.interval`         | Interval at which metrics are scraped| `30s`     |
| `serviceMonitor.scrapeTimeout`    | Timeout after which the scrape is ended | `10s`  |
| `serviceMonitor.protocolMetrics`  | Enable protocol-specific metrics     | `true`    |
| `serviceMonitor.performanceMetrics`| Enable performance optimization metrics | `true` |
| `monitoring.healthChecks.protocols`| Enable protocol-specific health checks | `true`  |
| `monitoring.healthChecks.loadBalancer`| Enable load balancer health monitoring | `true` |
| `monitoring.predictiveHealth`     | Enable predictive health monitoring   | `true`    |

### Dependency parameters

| Name                    | Description                      | Value   |
| ----------------------- | -------------------------------- | ------- |
| `redis.enabled`         | Enable Redis subchart            | `true`  |
| `prometheus.enabled`    | Enable Prometheus subchart       | `false` |
| `grafana.enabled`       | Enable Grafana subchart          | `false` |

## Configuration

The chart can be configured using the following methods:

1. **values.yaml file**: Modify the values.yaml file before installation
2. **--set flag**: Use the --set flag during installation
3. **values files**: Use multiple values files with -f flag

### Example: Custom configuration

```bash
helm install my-mcp-bridge ./helm/mcp-bridge \
  --set gateway.replicaCount=3 \
  --set router.resources.limits.memory=512Mi \
  --set redis.auth.password=mypassword
```

### Example: Production configuration with Universal Protocol Support

```yaml
# production-values.yaml
gateway:
  replicaCount: 3
  protocols:
    enabled: ["websocket", "tcp_binary", "stdio", "http"]
    autoDetect: true
    crossLoadBalance: true
  service:
    port: 8443        # WebSocket
    tcpPort: 8444     # TCP Binary
  resources:
    limits:
      cpu: 2000m
      memory: 1Gi
    requests:
      cpu: 500m
      memory: 256Mi
  autoscaling:
    enabled: true
    minReplicas: 3
    maxReplicas: 20

router:
  directClients:
    enabled: true
    protocols: ["stdio", "websocket", "http", "sse"]
    autoDetect: true
    pooling: true
    memoryOpt: true
  performance:
    observability: true
  resources:
    limits:
      cpu: 1000m
      memory: 512Mi

# Universal Service Discovery Support
serviceDiscovery:
  providers:
    kubernetes:
      enabled: true
      autoDetectProtocols: true
    consul:
      enabled: false
    static:
      enabled: true

monitoring:
  healthChecks:
    protocols: true
    loadBalancer: true
  predictiveHealth: true
  protocolMetrics: true

redis:
  master:
    persistence:
      size: 20Gi
    resources:
      limits:
        memory: 512Mi

tls:
  enabled: true
  cert: |
    -----BEGIN CERTIFICATE-----
    ... your certificate ...
  key: |
    -----BEGIN PRIVATE KEY-----
    ... your private key ...
```

```bash
helm install my-mcp-bridge ./helm/mcp-bridge -f production-values.yaml
```

## Monitoring

The chart includes support for monitoring through:

- **ServiceMonitor**: Automatic metrics collection by Prometheus
- **Health checks**: Built-in health endpoints
- **Grafana dashboards**: Available through the Grafana subchart

### Accessing metrics

```bash
# Gateway universal protocol metrics
kubectl port-forward service/my-mcp-bridge-gateway 9090:9090
curl http://localhost:9090/metrics

# Gateway protocol-specific health checks
curl http://localhost:9090/health/protocols
curl http://localhost:9090/health/stdio
curl http://localhost:9090/health/websocket
curl http://localhost:9090/health/load-balancer

# Router direct client metrics  
kubectl port-forward service/my-mcp-bridge-router 9091:9091
curl http://localhost:9091/metrics

# Router protocol detection and performance
curl http://localhost:9091/health/direct-clients
curl http://localhost:9091/health/protocol-detection
curl http://localhost:9091/api/v1/performance
```

## Security

The chart implements security best practices:

- **Non-root containers**: All containers run as non-root user
- **Read-only filesystem**: Containers use read-only root filesystem
- **Security contexts**: Proper security contexts applied
- **Network policies**: Optional network isolation
- **TLS encryption**: TLS support for secure communication

## Troubleshooting

### Common issues

1. **ImagePullBackOff**: Ensure image registry access and credentials
2. **CrashLoopBackOff**: Check logs and configuration
3. **Service not accessible**: Verify service and ingress configuration

### Debug commands

```bash
# Check pod status
kubectl get pods -l app.kubernetes.io/name=mcp-bridge

# View logs
kubectl logs -l app.kubernetes.io/component=gateway
kubectl logs -l app.kubernetes.io/component=router

# Describe resources
kubectl describe deployment my-mcp-bridge-gateway
kubectl describe service my-mcp-bridge-gateway

# Test connectivity
helm test my-mcp-bridge
```

## Upgrading

To upgrade the chart:

```bash
helm upgrade my-mcp-bridge ./helm/mcp-bridge
```

### Version compatibility

| Chart Version | App Version | Kubernetes Version |
| ------------- | ----------- | ------------------ |
| 1.0.0-rc1     | 1.0.0-rc1   | >=1.19.0           |

## Development

### Testing locally

```bash
# Lint the chart
helm lint ./helm/mcp-bridge

# Template the chart
helm template my-mcp-bridge ./helm/mcp-bridge

# Install in dry-run mode
helm install my-mcp-bridge ./helm/mcp-bridge --dry-run --debug
```

### Contributing

1. Make changes to the chart
2. Update the version in Chart.yaml
3. Test the changes
4. Submit a pull request

## License

This chart is licensed under the MIT License. See the LICENSE file for details.