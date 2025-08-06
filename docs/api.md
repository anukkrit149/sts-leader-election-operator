# API Documentation

## StatefulSetLock Custom Resource

The `StatefulSetLock` Custom Resource Definition (CRD) is the primary API provided by the StatefulSet Leader Election Operator.

### Resource Information

- **API Version**: `app.anukkrit.me/v1`
- **Kind**: `StatefulSetLock`
- **Scope**: Namespaced
- **Short Name**: `ssl`

### Spec Fields

#### statefulSetName

- **Type**: `string`
- **Required**: Yes
- **Description**: The name of the target StatefulSet in the same namespace that requires leader election management.
- **Validation**: 
  - Minimum length: 1 character
  - Must be a valid Kubernetes resource name
- **Example**: `"postgres-cluster"`

#### leaseName

- **Type**: `string`
- **Required**: Yes
- **Description**: The name of the `coordination.k8s.io/v1.Lease` object that will be created and managed by the operator for coordination between pods.
- **Validation**: 
  - Minimum length: 1 character
  - Must be a valid Kubernetes resource name
- **Example**: `"postgres-cluster-leader-lease"`
- **Note**: The lease will be created in the same namespace as the StatefulSetLock

#### leaseDurationSeconds

- **Type**: `int32`
- **Required**: Yes
- **Description**: The duration in seconds for how long the lease is considered valid. After this duration expires without renewal, a new leader election will be triggered.
- **Validation**: 
  - Minimum value: 1 second
  - Maximum value: 3600 seconds (1 hour)
- **Example**: `30`
- **Recommendations**:
  - For high-availability workloads: 15-30 seconds
  - For less critical workloads: 60-300 seconds
  - Consider your application's tolerance for leadership gaps

### Status Fields

#### writerPod

- **Type**: `string`
- **Description**: The name of the pod currently holding the writer lock and designated as the leader.
- **Example**: `"postgres-cluster-0"`
- **Note**: This field is empty when no leader is currently elected

#### observedGeneration

- **Type**: `int64`
- **Description**: The last `.metadata.generation` that was reconciled by the controller. This is used to track whether the controller has processed the latest changes to the resource.
- **Example**: `1`

#### conditions

- **Type**: `[]metav1.Condition`
- **Description**: An array of conditions that represent the current state of the StatefulSetLock resource.

##### Condition Types

**Available**
- **Type**: `"Available"`
- **Status**: `"True"` | `"False"` | `"Unknown"`
- **Reason**: Various reasons indicating the availability state
- **Message**: Human-readable message explaining the condition
- **Description**: Indicates whether the StatefulSetLock is functioning correctly and a leader is successfully elected and maintained.

**Progressing**
- **Type**: `"Progressing"`
- **Status**: `"True"` | `"False"` | `"Unknown"`
- **Reason**: Various reasons indicating the progress state
- **Message**: Human-readable message explaining the condition
- **Description**: Indicates whether the operator is actively working on changes, such as electing a new leader or updating pod labels.

### Example Resource

```yaml
apiVersion: app.anukkrit.me/v1
kind: StatefulSetLock
metadata:
  name: postgres-cluster-lock
  namespace: default
spec:
  statefulSetName: "postgres-cluster"
  leaseName: "postgres-cluster-leader-lease"
  leaseDurationSeconds: 30
status:
  writerPod: "postgres-cluster-0"
  observedGeneration: 1
  conditions:
  - type: Available
    status: "True"
    reason: LeaderElected
    message: "Leader postgres-cluster-0 successfully elected and lease maintained"
    lastTransitionTime: "2025-01-08T10:00:00Z"
  - type: Progressing
    status: "False"
    reason: LeaderStable
    message: "Current leader is healthy and lease is valid"
    lastTransitionTime: "2025-01-08T10:00:00Z"
```

### kubectl Commands

The operator registers custom columns for easier viewing:

```bash
# List StatefulSetLocks with custom columns
kubectl get statefulsetlock
# Output:
# NAME                   STATEFULSET      WRITER POD           LEASE DURATION   AGE
# postgres-cluster-lock  postgres-cluster postgres-cluster-0   30               5m

# Use short name
kubectl get ssl

# Get detailed information
kubectl describe ssl postgres-cluster-lock

# Watch for changes
kubectl get ssl -w

# Get status in JSON format
kubectl get ssl postgres-cluster-lock -o json | jq '.status'
```

### Related Resources

When a StatefulSetLock is created, the operator interacts with several other Kubernetes resources:

#### Lease Object
- **API Version**: `coordination.k8s.io/v1`
- **Kind**: `Lease`
- **Name**: As specified in `spec.leaseName`
- **Purpose**: Stores the current leader identity and lease timing information

#### Pod Labels
The operator adds labels to StatefulSet pods:
- `sts-role: writer` - Applied to the current leader pod
- `sts-role: reader` - Applied to all non-leader pods

#### Events
The operator generates Kubernetes events for important state changes:
- Leader election events
- Failover events
- Error conditions

### Validation and Constraints

The CRD includes OpenAPI schema validation:

```yaml
# Validation rules applied at the API level
spec:
  statefulSetName:
    type: string
    minLength: 1
  leaseName:
    type: string
    minLength: 1
  leaseDurationSeconds:
    type: integer
    minimum: 1
    maximum: 3600
```

Additional runtime validation:
- StatefulSet must exist in the same namespace
- StatefulSet must have at least one ready pod for leader election
- Lease name must not conflict with existing leases from other sources