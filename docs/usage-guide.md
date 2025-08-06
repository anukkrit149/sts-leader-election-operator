# Usage Guide

This guide walks you through using the StatefulSet Leader Election Operator with practical examples.

## Basic Usage

### Step 1: Deploy the Operator

First, install the CRDs and deploy the operator:

```bash
# Install CRDs
make install

# Deploy the operator
make deploy IMG=<your-registry>/sts-leader-elect-operator:latest
```

### Step 2: Create a StatefulSet

Create a StatefulSet that needs leader election. Here's a PostgreSQL example:

```yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: postgres-cluster
spec:
  serviceName: postgres-headless
  replicas: 3
  selector:
    matchLabels:
      app: postgres
  template:
    metadata:
      labels:
        app: postgres
    spec:
      containers:
      - name: postgres
        image: postgres:15
        # ... configuration
        readinessProbe:
          exec:
            command: ["pg_isready", "-U", "postgres"]
          initialDelaySeconds: 15
          periodSeconds: 5
```

**Important**: Ensure your StatefulSet pods have proper readiness probes configured, as the operator uses pod readiness to determine leader eligibility.

### Step 3: Create a StatefulSetLock

Create a StatefulSetLock resource to enable leader election:

```yaml
apiVersion: app.anukkrit.me/v1
kind: StatefulSetLock
metadata:
  name: postgres-cluster-lock
spec:
  statefulSetName: "postgres-cluster"
  leaseName: "postgres-cluster-leader-lease"
  leaseDurationSeconds: 30
```

### Step 4: Verify Leader Election

Check that leader election is working:

```bash
# Check the current leader
kubectl get ssl postgres-cluster-lock -o jsonpath='{.status.writerPod}'

# View pod labels
kubectl get pods -l app=postgres --show-labels

# Check the lease object
kubectl get lease postgres-cluster-leader-lease -o yaml
```

## Advanced Usage

### Using Pod Labels for Traffic Routing

Create services that route traffic based on the pod labels:

```yaml
# Service for write operations (leader only)
apiVersion: v1
kind: Service
metadata:
  name: postgres-writer
spec:
  selector:
    app: postgres
    sts-role: writer
  ports:
  - port: 5432

---
# Service for read operations (followers only)
apiVersion: v1
kind: Service
metadata:
  name: postgres-reader
spec:
  selector:
    app: postgres
    sts-role: reader
  ports:
  - port: 5432
```

### Configuring Lease Duration

Choose an appropriate lease duration based on your requirements:

- **High Availability (15-30 seconds)**: Quick failover but more frequent reconciliation
- **Standard (30-60 seconds)**: Balanced approach for most use cases
- **Low Frequency (60-300 seconds)**: Less overhead but slower failover

```yaml
spec:
  leaseDurationSeconds: 30  # Failover within ~30 seconds
```

### Monitoring Leader Changes

Watch for leader changes in real-time:

```bash
# Watch StatefulSetLock status
kubectl get ssl postgres-cluster-lock -w

# Watch pod labels
kubectl get pods -l app=postgres --show-labels -w

# View operator logs
kubectl logs -n <operator-namespace> deployment/sts-leader-elect-operator-controller-manager -f
```

## Common Patterns

### Database Primary-Replica Setup

For databases like PostgreSQL, MySQL, or MongoDB:

1. Configure the StatefulSet with proper readiness probes
2. Create a StatefulSetLock with appropriate lease duration
3. Use separate services for read and write traffic
4. Configure your application to use different endpoints for reads and writes

### Message Queue Leader Election

For message queues or stream processors:

```yaml
apiVersion: app.anukkrit.me/v1
kind: StatefulSetLock
metadata:
  name: kafka-controller-lock
spec:
  statefulSetName: "kafka-cluster"
  leaseName: "kafka-controller-lease"
  leaseDurationSeconds: 15  # Quick failover for message processing
```

### Batch Job Coordination

For batch processing where only one instance should run:

```yaml
apiVersion: app.anukkrit.me/v1
kind: StatefulSetLock
metadata:
  name: batch-processor-lock
spec:
  statefulSetName: "batch-processor"
  leaseName: "batch-processor-lease"
  leaseDurationSeconds: 300  # Longer duration for batch jobs
```

## Troubleshooting

### No Leader Elected

If no leader is being elected:

1. Check if StatefulSet pods are ready:
   ```bash
   kubectl get pods -l app=<your-app>
   ```

2. Verify readiness probes are configured and passing
3. Check operator logs for errors:
   ```bash
   kubectl logs -n <operator-namespace> deployment/sts-leader-elect-operator-controller-manager
   ```

### Leader Not Changing During Failover

If the leader doesn't change when a pod becomes unavailable:

1. Verify the lease duration is appropriate
2. Check if the pod is actually marked as NotReady:
   ```bash
   kubectl describe pod <pod-name>
   ```

3. Ensure the operator has proper RBAC permissions

### Pod Labels Not Applied

If pods aren't getting labeled:

1. Check operator RBAC permissions for pod patching
2. Verify there are no admission controllers blocking label updates
3. Review operator logs for permission errors

## Best Practices

1. **Always use readiness probes** - The operator relies on pod readiness for leader election
2. **Choose appropriate lease duration** - Balance between failover speed and system overhead
3. **Monitor operator logs** - Set up log aggregation for the operator
4. **Use separate services** - Create distinct services for read and write traffic
5. **Test failover scenarios** - Regularly test that failover works as expected
6. **Resource limits** - Set appropriate resource limits for the operator deployment