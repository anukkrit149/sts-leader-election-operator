# Deployment Configuration Guide

This document provides comprehensive guidance for deploying the StatefulSet Leader Election Operator in various environments.

## Overview

The operator supports multiple deployment configurations:

- **Development**: Basic configuration for testing and development
- **Production**: Enhanced configuration with high availability and security
- **Custom**: Flexible configuration for specific requirements

## Deployment Configurations

### Development Deployment

For development and testing environments:

```bash
# Deploy with default configuration
make deploy

# Or using kubectl directly
kubectl apply -f dist/install.yaml
```

**Characteristics:**
- Single replica
- Basic resource limits
- Standard security settings
- Suitable for development clusters

### Production Deployment

For production environments with enhanced reliability:

```bash
# Build production manifests
make build-installer-production

# Deploy production configuration
kubectl apply -f dist/install-production.yaml
```

**Characteristics:**
- Multiple replicas (2) with leader election
- Enhanced resource limits and requests
- Pod Disruption Budget for high availability
- Topology spread constraints
- Enhanced security context
- Comprehensive health checks
- Network policies (optional)
- Service monitoring integration

## Resource Requirements

### Minimum Requirements

| Resource | Development | Production |
|----------|-------------|------------|
| CPU Request | 50m | 50m |
| CPU Limit | 200m | 200m |
| Memory Request | 128Mi | 128Mi |
| Memory Limit | 256Mi | 256Mi |
| Ephemeral Storage Request | 100Mi | 100Mi |
| Ephemeral Storage Limit | 1Gi | 1Gi |

### Scaling Considerations

The operator is designed to be lightweight and efficient:

- **CPU**: Scales with the number of StatefulSetLocks managed
- **Memory**: Primarily used for caching Kubernetes resources
- **Network**: Minimal bandwidth requirements for API calls

## High Availability Configuration

### Leader Election

The operator uses Kubernetes leader election for high availability:

- **Lease Duration**: 15 seconds (configurable)
- **Renew Deadline**: 10 seconds
- **Retry Period**: 2 seconds

### Pod Disruption Budget

Ensures at least one replica is available during:
- Node maintenance
- Cluster upgrades
- Voluntary pod evictions

### Topology Spread Constraints

Distributes pods across:
- **Nodes**: Ensures pods run on different nodes
- **Zones**: Distributes across availability zones (when available)

## Security Configuration

### Pod Security Context

```yaml
securityContext:
  runAsNonRoot: true
  runAsUser: 65532
  runAsGroup: 65532
  fsGroup: 65532
  seccompProfile:
    type: RuntimeDefault
```

### Container Security Context

```yaml
securityContext:
  allowPrivilegeEscalation: false
  readOnlyRootFilesystem: true
  capabilities:
    drop:
    - "ALL"
```

### Network Policies

Optional network policies restrict:
- Ingress to health and metrics endpoints
- Egress to Kubernetes API and DNS

## Health Checks

### Liveness Probe

Monitors operator health:
- **Endpoint**: `/healthz`
- **Port**: 8081
- **Initial Delay**: 15 seconds
- **Period**: 20 seconds
- **Timeout**: 5 seconds
- **Failure Threshold**: 3

### Readiness Probe

Determines when operator is ready:
- **Endpoint**: `/readyz`
- **Port**: 8081
- **Initial Delay**: 5 seconds
- **Period**: 10 seconds
- **Timeout**: 5 seconds
- **Failure Threshold**: 3

### Startup Probe

Handles slow startup scenarios:
- **Endpoint**: `/readyz`
- **Port**: 8081
- **Initial Delay**: 10 seconds
- **Period**: 10 seconds
- **Timeout**: 5 seconds
- **Failure Threshold**: 30

## Monitoring and Observability

### Metrics

The operator exposes Prometheus metrics on port 8443:

- **Controller metrics**: Reconciliation rates, errors, duration
- **Leader election metrics**: Leadership changes, lease renewals
- **Custom metrics**: StatefulSetLock status, pod labeling events

### Logging

Structured logging with configurable levels:

```yaml
args:
- --zap-log-level=info          # info, debug, error
- --zap-encoder=json            # json, console
- --zap-time-encoding=iso8601   # iso8601, epoch
```

### Service Monitor

For Prometheus Operator integration:

```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: controller-manager-metrics
spec:
  selector:
    matchLabels:
      control-plane: controller-manager
  endpoints:
  - port: https
    path: /metrics
    interval: 30s
```

## Environment Variables

The operator supports the following environment variables:

| Variable | Description | Default |
|----------|-------------|---------|
| `WATCH_NAMESPACE` | Namespace to watch (empty = all) | All namespaces |
| `POD_NAME` | Current pod name | Auto-detected |
| `OPERATOR_NAME` | Operator identifier | sts-leader-elect-operator |
| `METRICS_ADDR` | Metrics server address | :8443 |
| `HEALTH_ADDR` | Health server address | :8081 |

## Customization

### Custom Resource Limits

Override default resource limits:

```yaml
# In kustomization.yaml
patches:
- patch: |
    - op: replace
      path: /spec/template/spec/containers/0/resources/limits/cpu
      value: 500m
    - op: replace
      path: /spec/template/spec/containers/0/resources/limits/memory
      value: 512Mi
  target:
    kind: Deployment
    name: controller-manager
```

### Custom Arguments

Add custom operator arguments:

```yaml
# In kustomization.yaml
patches:
- patch: |
    - op: add
      path: /spec/template/spec/containers/0/args/-
      value: --concurrent-reconciles=5
  target:
    kind: Deployment
    name: controller-manager
```

### Namespace Scoping

Limit operator to specific namespaces:

```yaml
env:
- name: WATCH_NAMESPACE
  value: "production,staging"
```

## Validation

Use the provided validation script to verify deployment:

```bash
./scripts/validate-deployment.sh
```

The script checks:
- Namespace and CRD installation
- Deployment readiness
- Pod health status
- Health endpoint availability
- RBAC permissions
- Basic functionality with test resources

## Troubleshooting

### Common Issues

1. **Insufficient RBAC permissions**
   ```bash
   kubectl describe clusterrole sts-leader-elect-operator-manager-role
   ```

2. **Resource constraints**
   ```bash
   kubectl describe pod -n sts-leader-elect-operator-system
   ```

3. **Network connectivity**
   ```bash
   kubectl logs -n sts-leader-elect-operator-system deployment/sts-leader-elect-operator-controller-manager
   ```

### Debug Mode

Enable debug logging:

```bash
kubectl patch deployment sts-leader-elect-operator-controller-manager \
  -n sts-leader-elect-operator-system \
  --type='json' \
  -p='[{"op": "replace", "path": "/spec/template/spec/containers/0/args/3", "value": "--zap-log-level=debug"}]'
```

### Health Check Debugging

Test health endpoints directly:

```bash
kubectl port-forward -n sts-leader-elect-operator-system deployment/sts-leader-elect-operator-controller-manager 8081:8081

# In another terminal
curl http://localhost:8081/healthz
curl http://localhost:8081/readyz
```

## Upgrade Procedures

### Rolling Updates

The operator supports rolling updates:

```bash
# Update image
kubectl set image deployment/sts-leader-elect-operator-controller-manager \
  manager=anukkrit149/sts-leader-elect-operator:v0.1.0 \
  -n sts-leader-elect-operator-system

# Monitor rollout
kubectl rollout status deployment/sts-leader-elect-operator-controller-manager \
  -n sts-leader-elect-operator-system
```

### Rollback

If issues occur during upgrade:

```bash
kubectl rollout undo deployment/sts-leader-elect-operator-controller-manager \
  -n sts-leader-elect-operator-system
```

## Backup and Recovery

### Configuration Backup

Backup operator configuration:

```bash
kubectl get all,configmaps,secrets,pdb,networkpolicies \
  -n sts-leader-elect-operator-system \
  -o yaml > operator-backup.yaml
```

### StatefulSetLock Resources

Backup managed resources:

```bash
kubectl get statefulsetlocks.app.anukkrit.me --all-namespaces \
  -o yaml > statefulsetlocks-backup.yaml
```

This comprehensive deployment configuration ensures the operator runs reliably in production environments while providing flexibility for various deployment scenarios.