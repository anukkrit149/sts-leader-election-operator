# Observability Features

The StatefulSet Leader Election Operator includes comprehensive observability features to help monitor and debug leader election operations.

## Structured Logging

The operator uses structured logging with consistent field names across all operations:

### Log Levels
- **Info**: Normal operations, leader changes, reconciliation events
- **Debug** (V=1): Detailed debugging information, resource fetching
- **Verbose** (V=2): Very detailed operational information
- **Error**: Failures and exceptions
- **Warn**: Recoverable issues

### Common Log Fields
- `statefulsetlock`: Name of the StatefulSetLock resource
- `namespace`: Kubernetes namespace
- `statefulset`: Target StatefulSet name
- `lease`: Lease name
- `pod`: Pod name
- `leader`: Current leader pod name
- `election_reason`: Reason for leader election
- `duration`: Operation duration
- `total_pods`: Total number of pods
- `ready_pods`: Number of ready pods

### Example Log Output
```json
{
  "level": "info",
  "ts": "2025-01-08T10:30:45.123Z",
  "msg": "Leader election completed",
  "statefulsetlock": "my-app-lock",
  "namespace": "default",
  "statefulset": "my-app",
  "leader": "my-app-0",
  "election_reason": "lease_expired",
  "duration": "150ms"
}
```

## Prometheus Metrics

The operator exposes comprehensive Prometheus metrics for monitoring:

### Leader Election Metrics
- `statefulsetlock_leader_elections_total`: Total number of leader elections performed
- `statefulsetlock_leader_election_duration_seconds`: Time taken for leader election operations

### Lease Management Metrics
- `statefulsetlock_lease_renewals_total`: Total number of lease renewals
- `statefulsetlock_lease_renewal_duration_seconds`: Time taken for lease renewal operations
- `statefulsetlock_lease_expiration_seconds`: Seconds until lease expiration (negative if expired)

### Reconciliation Metrics
- `statefulsetlock_reconciliation_errors_total`: Total number of reconciliation errors
- `statefulsetlock_reconciliation_duration_seconds`: Time taken for reconciliation operations

### Status Metrics
- `statefulsetlock_current_leader_info`: Information about the current leader (1 if leader exists, 0 otherwise)
- `statefulsetlock_ready_pods`: Number of ready pods in the StatefulSet
- `statefulsetlock_pod_labeling_errors_total`: Total number of pod labeling errors

### Metric Labels
All metrics include relevant labels for filtering and aggregation:
- `namespace`: Kubernetes namespace
- `statefulsetlock`: StatefulSetLock resource name
- `statefulset`: Target StatefulSet name
- `lease_name`: Lease name
- `leader_pod`: Current leader pod name
- `reason`: Election reason
- `error_type`: Type of error

### Example Prometheus Queries

**Current leader for all StatefulSetLocks:**
```promql
statefulsetlock_current_leader_info == 1
```

**Leader election rate:**
```promql
rate(statefulsetlock_leader_elections_total[5m])
```

**Average reconciliation duration:**
```promql
rate(statefulsetlock_reconciliation_duration_seconds_sum[5m]) / rate(statefulsetlock_reconciliation_duration_seconds_count[5m])
```

**Error rate by type:**
```promql
rate(statefulsetlock_reconciliation_errors_total[5m]) by (error_type)
```

## OpenTelemetry Tracing

The operator includes distributed tracing support using OpenTelemetry:

### Trace Operations
- `Reconcile`: Main reconciliation loop
- `performLeaderElection`: Leader election algorithm
- `fetchStatefulSetLock`: Resource fetching
- `fetchStatefulSet`: StatefulSet fetching
- `fetchLease`: Lease fetching
- `createOrUpdateLease`: Lease management
- `updatePodLabels`: Pod labeling operations
- `handleDeletion`: Resource cleanup

### Trace Attributes
- `k8s.namespace`: Kubernetes namespace
- `statefulsetlock.name`: StatefulSetLock name
- `statefulset.name`: StatefulSet name
- `pod.name`: Pod name
- `lease.name`: Lease name
- `leader.pod`: Leader pod name
- `election.reason`: Election reason
- `error.type`: Error type
- `pods.ready_count`: Number of ready pods
- `pods.total_count`: Total number of pods
- `lease.duration_seconds`: Lease duration
- `lease.expired`: Whether lease is expired

### Trace Status
Spans are marked with appropriate status codes:
- `OK`: Successful operations
- `ERROR`: Failed operations with error details

## Configuration

### Logging Configuration
Configure logging levels using command-line flags:
```bash
# Enable debug logging
--zap-log-level=debug

# Enable development mode with more verbose output
--zap-development=true

# Set specific verbosity level
--v=2
```

### Metrics Configuration
Access metrics at the /metrics path.

Local development (make run):
- The Makefile run target starts the manager with --metrics-bind-address=:8080 and --metrics-secure=false.
- Access: curl http://localhost:8080/metrics

Cluster defaults:
- Manifests configure secure metrics on :8443 with authn/authz.
- Access path is /metrics over HTTPS. Example from within the cluster or via port-forward:
  - kubectl -n <ns> port-forward svc/controller-manager-metrics-service 8443:8443
  - curl -k https://localhost:8443/metrics

Flags:
```bash
# Bind address (use :PORT to enable; 0 disables)
--metrics-bind-address=:8080
--metrics-bind-address=0

# Serve metrics over HTTPS (default: true). For local testing, set to false.
--metrics-secure=false

# Health/ready probes:
--health-probe-bind-address=:8081
```
Note: http://localhost:8443/ (root) will not return metrics. Use the correct path: /metrics, and https:// for secure mode.

### Tracing Configuration
OpenTelemetry tracing is automatically enabled. Configure tracing using standard OpenTelemetry environment variables:

```bash
# Set OTLP endpoint
export OTEL_EXPORTER_OTLP_ENDPOINT=http://jaeger:4317

# Set service name
export OTEL_SERVICE_NAME=statefulset-leader-election-operator

# Set resource attributes
export OTEL_RESOURCE_ATTRIBUTES=service.version=v1.0.0
```

## Monitoring Dashboard

### Grafana Dashboard
A sample Grafana dashboard is available that includes:
- Leader election frequency
- Reconciliation duration trends
- Error rates by type
- Current leader status
- Pod readiness metrics
- Lease expiration warnings

### Alerting Rules
Sample Prometheus alerting rules:

```yaml
groups:
- name: statefulsetlock.rules
  rules:
  - alert: StatefulSetLockNoLeader
    expr: statefulsetlock_current_leader_info == 0
    for: 5m
    labels:
      severity: warning
    annotations:
      summary: "StatefulSetLock has no leader"
      description: "StatefulSetLock {{ $labels.statefulsetlock }} in namespace {{ $labels.namespace }} has no leader for 5 minutes"

  - alert: StatefulSetLockHighErrorRate
    expr: rate(statefulsetlock_reconciliation_errors_total[5m]) > 0.1
    for: 2m
    labels:
      severity: critical
    annotations:
      summary: "High error rate in StatefulSetLock reconciliation"
      description: "StatefulSetLock {{ $labels.statefulsetlock }} has error rate > 0.1/sec"

  - alert: StatefulSetLockLeaseExpiringSoon
    expr: statefulsetlock_lease_expiration_seconds < 30 and statefulsetlock_lease_expiration_seconds > 0
    for: 1m
    labels:
      severity: warning
    annotations:
      summary: "StatefulSetLock lease expiring soon"
      description: "Lease {{ $labels.lease_name }} will expire in {{ $value }} seconds"
```

## Troubleshooting

### Common Issues

**High leader election frequency:**
- Check pod readiness and stability
- Review lease duration configuration
- Monitor network connectivity

**Reconciliation errors:**
- Check operator logs for specific error types
- Verify RBAC permissions
- Ensure StatefulSet and pods are healthy

**Missing metrics:**
- Verify metrics endpoint is accessible
- Check Prometheus scrape configuration
- Ensure operator is running with metrics enabled

### Debug Commands

**Check current leader:**
```bash
kubectl get statefulsetlock my-app-lock -o jsonpath='{.status.writerPod}'
```

**View operator logs:**
```bash
kubectl logs -l app.kubernetes.io/name=statefulset-leader-election-operator -f
```

**Check metrics:**
```bash
# Local (make run)
curl http://localhost:8080/metrics | grep statefulsetlock

# In-cluster via port-forward (secure HTTPS with self-signed cert)
kubectl -n <ns> port-forward svc/controller-manager-metrics-service 8443:8443
curl -k https://localhost:8443/metrics | grep statefulsetlock
```