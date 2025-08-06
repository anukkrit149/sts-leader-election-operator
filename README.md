# StatefulSet Leader Election Operator

A Kubernetes operator that manages leader/writer election for StatefulSet workloads, ensuring only one pod is designated as the "writer" at any given time with automatic failover capabilities.

## Description

The StatefulSet Leader Election Operator simplifies active-passive or leader-follower deployment patterns by offloading leader election logic from applications to the Kubernetes control plane. It introduces a `StatefulSetLock` Custom Resource that allows platform engineers to declaratively specify which StatefulSet requires leader election management.

Key features:
- **Automatic Leader Election**: Selects the lowest ordinal ready pod as the leader
- **Automatic Failover**: Detects when the current leader becomes unavailable and elects a new one
- **Pod Labeling**: Labels pods with `sts-role: writer` or `sts-role: reader` for easy identification
- **Lease Management**: Uses Kubernetes coordination.k8s.io/v1.Lease for distributed coordination
- **Observable Status**: Provides clear status reporting through standard Kubernetes conditions
- **Cleanup Handling**: Proper resource cleanup when StatefulSetLock resources are deleted

## Getting Started

### Prerequisites
- go version v1.22.0+
- docker version 17.03+
- kubectl version v1.11.3+
- Access to a Kubernetes v1.11.3+ cluster

### Quick Start

1. **Install the operator CRDs:**
   ```sh
   make install
   ```

2. **Deploy the operator:**
   ```sh
   make deploy IMG=<your-registry>/sts-leader-elect-operator:latest
   ```

3. **Create a sample StatefulSet and StatefulSetLock:**
   ```sh
   kubectl apply -k config/samples/
   ```

4. **Verify the leader election is working:**
   ```sh
   # Check the StatefulSetLock status
   kubectl get statefulsetlock postgres-cluster-lock -o yaml
   
   # Check pod labels
   kubectl get pods -l app=postgres --show-labels
   
   # Check the lease object
   kubectl get lease postgres-cluster-leader-lease -o yaml
   ```

### Usage Example

Here's a complete example of using the operator with a PostgreSQL StatefulSet:

```yaml
# StatefulSet with 3 replicas
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
        # ... container configuration
---
# StatefulSetLock to manage leader election
apiVersion: app.anukkrit.me/v1
kind: StatefulSetLock
metadata:
  name: postgres-cluster-lock
spec:
  statefulSetName: "postgres-cluster"
  leaseName: "postgres-cluster-leader-lease"
  leaseDurationSeconds: 30
```

After applying these resources:
- The operator will elect `postgres-cluster-0` as the leader (lowest ordinal)
- `postgres-cluster-0` will be labeled with `sts-role: writer`
- Other pods will be labeled with `sts-role: reader`
- A lease object will be created and maintained
- If the leader pod becomes unavailable, a new leader will be elected automatically

### To Deploy on the cluster
**Build and push your image to the location specified by `IMG`:**

```sh
make docker-build docker-push IMG=<some-registry>/sts-leader-elect-operator:tag
```

**NOTE:** This image ought to be published in the personal registry you specified.
And it is required to have access to pull the image from the working environment.
Make sure you have the proper permission to the registry if the above commands don’t work.

**Install the CRDs into the cluster:**

```sh
make install
```

**Deploy the Manager to the cluster with the image specified by `IMG`:**

```sh
make deploy IMG=<some-registry>/sts-leader-elect-operator:tag
```

> **NOTE**: If you encounter RBAC errors, you may need to grant yourself cluster-admin
privileges or be logged in as admin.

**Create instances of your solution**
You can apply the samples (examples) from the config/samples:

```sh
kubectl apply -k config/samples/
```

This will create:
- A PostgreSQL StatefulSet with 3 replicas
- A StatefulSetLock resource to manage leader election
- Services for accessing writer and reader pods separately

### To Uninstall
**Delete the instances (CRs) from the cluster:**

```sh
kubectl delete -k config/samples/
```

**Delete the APIs(CRDs) from the cluster:**

```sh
make uninstall
```

**UnDeploy the controller from the cluster:**

```sh
make undeploy
```

## API Reference

### StatefulSetLock

The `StatefulSetLock` is the primary Custom Resource provided by this operator.

#### Spec Fields

| Field | Type | Required | Description | Validation |
|-------|------|----------|-------------|------------|
| `statefulSetName` | string | Yes | Name of the target StatefulSet in the same namespace | MinLength: 1 |
| `leaseName` | string | Yes | Name of the coordination.k8s.io/v1.Lease object that will be created | MinLength: 1 |
| `leaseDurationSeconds` | int32 | Yes | Duration in seconds that the lease is valid | Min: 1, Max: 3600 |

#### Status Fields

| Field | Type | Description |
|-------|------|-------------|
| `writerPod` | string | Name of the pod currently holding the writer lock |
| `observedGeneration` | int64 | Last generation reconciled by the controller |
| `conditions` | []metav1.Condition | Current state conditions of the StatefulSetLock |

#### Condition Types

The operator reports the following condition types:

- **Available**: Indicates if the operator is functioning correctly for this StatefulSetLock
- **Progressing**: Indicates if the operator is actively working on changes
- **Error**: Indicates error conditions with detailed messages

#### Pod Labels

The operator automatically applies these labels to StatefulSet pods:

- `sts-role: writer` - Applied to the current leader pod
- `sts-role: reader` - Applied to all non-leader pods

These labels can be used in Service selectors to route traffic appropriately:

```yaml
# Service for write operations (routes to leader only)
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
# Service for read operations (routes to followers only)
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

### Leader Election Algorithm

The operator uses the following algorithm for leader election:

1. **Initial Election**: When no lease exists, elect the ready pod with the lowest ordinal index
2. **Lease Validation**: Check if the current lease holder is still ready and the lease hasn't expired
3. **Failover**: If the current leader is not ready or the lease has expired, elect a new leader
4. **Lease Renewal**: Renew the lease before expiration (at half the lease duration interval)

### Monitoring and Observability

#### Checking Leader Status

```sh
# View current leader
kubectl get statefulsetlock <name> -o jsonpath='{.status.writerPod}'

# View all pod labels
kubectl get pods -l app=<your-app> --show-labels

# View lease information
kubectl get lease <lease-name> -o yaml
```

#### Common kubectl Commands

```sh
# List all StatefulSetLocks (shortname: ssl)
kubectl get ssl

# Describe a StatefulSetLock for detailed information
kubectl describe ssl <name>

# Watch for changes in real-time
kubectl get ssl -w

# View operator logs
kubectl logs -n <operator-namespace> deployment/sts-leader-elect-operator-controller-manager
```

## Project Distribution

Following are the steps to build the installer and distribute this project to users.

1. Build the installer for the image built and published in the registry:

```sh
make build-installer IMG=<some-registry>/sts-leader-elect-operator:tag
```

NOTE: The makefile target mentioned above generates an 'install.yaml'
file in the dist directory. This file contains all the resources built
with Kustomize, which are necessary to install this project without
its dependencies.

2. Using the installer

Users can just run kubectl apply -f <URL for YAML BUNDLE> to install the project, i.e.:

```sh
kubectl apply -f https://raw.githubusercontent.com/<org>/sts-leader-elect-operator/<tag or branch>/dist/install.yaml
```

## Troubleshooting

### Common Issues

#### StatefulSetLock shows "Progressing" condition
- Check if the target StatefulSet exists in the same namespace
- Verify that at least one pod in the StatefulSet is in Ready state
- Check operator logs for detailed error messages

#### Leader election not working
- Ensure pods have proper readiness probes configured
- Verify the lease duration is appropriate for your use case
- Check if there are network policies blocking access to the coordination API

#### Pods not getting labeled
- Verify the operator has proper RBAC permissions to patch pods
- Check if there are admission controllers preventing label updates
- Review operator logs for permission errors

### Development and Testing

#### Running Tests

```sh
# Run unit tests
make test

# Run integration tests with envtest
make test-integration

# Run end-to-end tests (requires running cluster)
make test-e2e
```

#### Local Development

```sh
# Install CRDs
make install

# Run operator locally (outside cluster)
make run

# Generate code after API changes
make generate

# Update manifests after marker changes
make manifests
```

## Contributing

We welcome contributions! Please follow these guidelines:

1. **Fork the repository** and create a feature branch
2. **Write tests** for any new functionality
3. **Follow Go conventions** and run `make fmt` and `make vet`
4. **Update documentation** if you change APIs or behavior
5. **Submit a pull request** with a clear description of changes

### Development Setup

1. Clone the repository
2. Install dependencies: `go mod download`
3. Install kubebuilder and operator-sdk tools
4. Run tests: `make test`
5. Start contributing!

**NOTE:** Run `make help` for more information on all potential `make` targets

## Documentation

- [API Reference](docs/api.md) - Detailed documentation of the StatefulSetLock CRD
- [Usage Guide](docs/usage-guide.md) - Step-by-step guide with practical examples
- [Examples](config/samples/) - Sample manifests and usage examples

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

## License

Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

