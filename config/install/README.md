# StatefulSet Leader Election Operator Installation Guide

This directory contains the complete installation manifests for the StatefulSet Leader Election Operator.

## Quick Installation

To install the operator with default configuration:

```bash
kubectl apply -f https://raw.githubusercontent.com/your-org/sts-leader-elect-operator/main/dist/install.yaml
```

Or from the repository root:

```bash
make deploy
```

## Production Installation

For production deployments, use the enhanced configuration:

```bash
# Build production manifests
make build-installer-production

# Apply the manifests
kubectl apply -f dist/install-production.yaml
```

## Configuration Options

### Resource Requirements

The operator is configured with the following resource requirements:

- **CPU Requests**: 50m (minimum required)
- **CPU Limits**: 200m (maximum allowed)
- **Memory Requests**: 128Mi (minimum required)
- **Memory Limits**: 256Mi (maximum allowed)
- **Ephemeral Storage Requests**: 100Mi
- **Ephemeral Storage Limits**: 1Gi

### High Availability

The production configuration includes:

- **Replicas**: 2 (with leader election for active-passive)
- **Pod Disruption Budget**: Ensures at least 1 replica is available
- **Topology Spread Constraints**: Distributes pods across nodes and zones
- **Rolling Update Strategy**: Zero-downtime updates

### Security

The operator runs with enhanced security:

- **Non-root user**: Runs as user ID 65532
- **Read-only root filesystem**: Prevents runtime modifications
- **Seccomp profile**: Uses RuntimeDefault for syscall filtering
- **Dropped capabilities**: All Linux capabilities are dropped

### Monitoring

The operator exposes metrics on port 8443:

- **Prometheus scraping**: Enabled via annotations
- **Health checks**: Available on `/healthz` and `/readyz`
- **Structured logging**: JSON format with configurable levels

## Verification

After installation, verify the operator is running:

```bash
# Check operator pods
kubectl get pods -n sts-leader-elect-operator-system

# Check operator logs
kubectl logs -n sts-leader-elect-operator-system deployment/sts-leader-elect-operator-controller-manager

# Verify CRDs are installed
kubectl get crd statefulsetlocks.app.anukkrit.me
```

## Uninstallation

To remove the operator:

```bash
make undeploy
```

Or manually:

```bash
kubectl delete -f dist/install.yaml
```

## Troubleshooting

### Common Issues

1. **RBAC Permissions**: Ensure the operator has proper permissions
2. **Resource Constraints**: Check if cluster has sufficient resources
3. **Network Policies**: Verify network policies allow operator communication

### Debug Mode

To enable debug logging:

```bash
kubectl patch deployment sts-leader-elect-operator-controller-manager \
  -n sts-leader-elect-operator-system \
  --type='json' \
  -p='[{"op": "replace", "path": "/spec/template/spec/containers/0/args/3", "value": "--zap-log-level=debug"}]'
```