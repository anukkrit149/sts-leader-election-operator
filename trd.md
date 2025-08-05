Of course. Here is a Technical Requirements Document (TRD) generated from the provided plan.

***

# Technical Requirements Document: StatefulSet Leader Election Operator

**Version:** 1.0
**Date:** August 5, 2025
**Status:** Draft

## 1. Introduction

This document provides the detailed technical requirements for the **StatefulSet Leader Election Operator**. The primary purpose of this Kubernetes operator is to manage a leader/writer lock for a designated `StatefulSet`. It ensures that at any given time, only a single pod within the set is designated as the "writer," capable of handling write operations. The operator will automate the election of the writer pod, manage failovers when the writer becomes unavailable, and provide clear, observable status through a Custom Resource.

This system addresses the common pattern of active-passive or leader-follower deployments for stateful applications, simplifying the architecture by offloading the leader election logic from the application to the Kubernetes control plane.

---

## 2. System Overview & Architecture

The system consists of three main components: a Custom Resource Definition (CRD), the controller (operator), and the native Kubernetes resources it manages.

* **`StatefulSetLock` Custom Resource (CR):** This is the user-facing API. A user creates a `StatefulSetLock` instance to specify which `StatefulSet` they want to manage.
* **Controller:** This is the core logic of the operator. It runs in the cluster, watches for changes to `StatefulSetLock` resources and the resources they manage, and performs actions to ensure the desired state is met.
* **Managed Resources:** The controller interacts with `StatefulSets`, `Pods`, and `Leases` to perform its function.

The high-level interaction between these components is as follows:



1.  A **User** creates a `StatefulSetLock` CR, targeting a specific `StatefulSet`.
2.  The **Operator** (Controller) is notified of the new CR.
3.  The Operator inspects the target **`StatefulSet`** and its associated **`Pods`**.
4.  It creates or updates a **`Lease`** object to act as a distributed lock, assigning the first ready pod as the leader.
5.  It labels the leader pod as `sts-role: writer` and all other pods as `sts-role: reader`.
6.  The Operator continuously monitors the `Pods` and the `Lease` to handle failovers and renew the lease.
7.  It updates the `StatefulSetLock` CR's `status` field to reflect the current writer pod.

---

## 3. Functional Requirements

### 3.1. `StatefulSetLock` Custom Resource Definition (CRD)

The operator shall introduce a new API group `app.anukkrit.me` with a resource named `StatefulSetLock`.

* **FR-1.1: Spec Definition:** The `StatefulSetLockSpec` must contain the following fields:
    * `statefulSetName` (string): The name of the target `StatefulSet` within the same namespace. **This field is required.**
    * `leaseName` (string): The name of the `coordination.k8s.io/v1.Lease` object to be used for locking. **This field is required.**
    * `leaseDurationSeconds` (integer): The duration in seconds that the lease is valid before it expires. **This field is required.**

* **FR-1.2: Status Definition:** The `StatefulSetLockStatus` must contain the following fields for observability:
    * `writerPod` (string): The name of the pod currently holding the writer lock.
    * `observedGeneration` (integer): The last `metadata.generation` of the `StatefulSetLock` that was reconciled by the controller.
    * `conditions` (array): A standard Kubernetes conditions array (containing `type`, `status`, `reason`, `message`, `lastTransitionTime`) to report the operator's state. At a minimum, it should support `Available` and `Progressing` conditions.

### 3.2. Controller Reconciliation Logic

The controller's reconciliation loop must implement the following logic.



* **FR-2.1: Resource Fetching:** On reconciliation, the controller must fetch the `StatefulSetLock` instance, the target `StatefulSet`, all associated `Pods`, and the `Lease` object specified in the spec.
* **FR-2.2: Finalizer Management:**
    * The controller must ensure a finalizer (e.g., `statefulsetlock.app.anukkrit.me/finalizer`) is present on the `StatefulSetLock` CR.
    * Upon deletion of the `StatefulSetLock` CR (indicated by `deletionTimestamp`), the controller must delete the associated `Lease` object and then remove the finalizer to allow garbage collection.
* **FR-2.3: Leader Election:**
    * The controller must identify all pods managed by the `StatefulSet` that are in a `Ready` state.
    * The ready pods must be sorted deterministically based on their ordinal index (e.g., `pod-0`, `pod-1`).
    * A new leader election must be triggered if:
        * The `Lease` object does not exist.
        * The `Lease` has expired (current time > `renewTime` + `leaseDurationSeconds`).
        * The pod identified in `holderIdentity` of the `Lease` is no longer in a `Ready` state.
    * The first pod in the sorted list of ready pods shall be elected as the new leader.
* **FR-2.4: Lease Management:**
    * When a new leader is elected, the controller must create or update the `Lease` object, setting the `holderIdentity` to the new leader pod's name and updating the `renewTime`.
    * If the current lease is valid and the holder is a ready pod, the controller must update the `renewTime` of the `Lease` to renew it.
* **FR-2.5: Pod Labeling:**
    * The controller must ensure the pod holding the writer lease has the label `sts-role: writer`.
    * The controller must ensure all other pods in the `StatefulSet` have the label `sts-role: reader`.
* **FR-2.6: Status Updates:** The controller must update the `StatefulSetLock.status` with the name of the current `writerPod` and the current operational conditions.
* **FR-2.7: Reconciliation Scheduling:** The reconciliation loop shall be requeued to run at a frequency of **half the `leaseDurationSeconds`** to ensure timely lease renewal.

### 3.3. Controller Watches

* **FR-3.1: Resource Watching:** In addition to watching `StatefulSetLock` resources, the controller must be configured to trigger reconciliation in response to events from:
    * **Managed `Pods`:** To react quickly to pod failures.
    * **Managed `Leases`:** To react to external modifications or deletions of the lock.

---

## 4. Security Requirements (RBAC)

The operator's `ServiceAccount` requires specific RBAC permissions to function. The following permissions must be granted via a `ClusterRole` and `ClusterRoleBinding`.

| API Group                  | Resources              | Verbs                                    |
| -------------------------- | ---------------------- | ---------------------------------------- |
| `app.anukkrit.me`            | `statefulsetlocks`     | `get`, `list`, `watch`, `update`, `patch`|
| `app.anukkrit.me`            | `statefulsetlocks/status` | `update`, `patch`                      |
| `app.anukkrit.me`            | `statefulsetlocks/finalizers` | `update`                               |
| `apps`                     | `statefulsets`         | `get`, `list`, `watch`                   |
| `coordination.k8s.io`      | `leases`               | `get`, `list`, `watch`, `create`, `update`, `patch`, `delete` |
| `""` (core)                | `pods`                 | `get`, `list`, `watch`, `update`, `patch`|
| `""` (core)                | `events`               | `create`, `patch`                      |

---

## 5. Testing Requirements

The operator's reliability must be validated through a comprehensive testing strategy.

* **TR-1: Unit Tests:**
    * **TR-1.1:** Pure helper functions, such as the pod sorting logic and pod readiness checks, must be tested in isolation.
    * **TR-1.2:** All unit tests shall be located in `internal/controller/statefulsetlock_controller_helpers_test.go`.

* **TR-2: Integration Tests (`envtest`):**
    * The full reconciliation loop must be tested against a simulated Kubernetes API server provided by `envtest`.
    * The following scenarios must be covered by integration tests in `internal/controller/statefulsetlock_controller_test.go`:
        * **TR-2.1 (Happy Path):** Upon creation of a `StatefulSet` and a `StatefulSetLock`, assert that a `Lease` is created, a writer is elected, and pods are labeled `writer`/`reader` correctly.
        * **TR-2.2 (Leader Failover):** After a successful election, simulate the leader pod becoming `NotReady`. Assert that once the lease expires, a new leader is elected from the remaining ready pods and labels are updated.
        * **TR-2.3 (CR Deletion):** Upon deletion of the `StatefulSetLock` CR, assert that the controller's finalizer logic correctly deletes the associated `Lease` object.
        * **TR-2.4 (Lease Renewal):** Assert that the controller successfully renews a valid lease before it expires.

---

## 6. Documentation Requirements

* **DR-1: README File:** The project's `README.md` must be updated to include:
    * A clear and concise description of the operator's purpose and functionality.
    * Instructions on how to install and use the operator.
    * A description of the `StatefulSetLock` CRD fields.

* **DR-2: Sample Manifests:** A complete, working example must be provided in the `config/samples/` directory. This example must include YAML manifests for:
    * A sample `StatefulSet`.
    * A corresponding `StatefulSetLock` CR to manage the sample `StatefulSet`.

---

## 7. Glossary

* **Controller:** The active component of an operator that watches resources and works to bring the current state of the system towards the desired state.
* **CRD (Custom Resource Definition):** A Kubernetes feature that allows users to create their own resource types.
* **Finalizer:** A mechanism in Kubernetes that allows controllers to implement pre-deletion hooks to clean up associated external resources.
* **Lease:** A lightweight Kubernetes resource (`coordination.k8s.io/v1.Lease`) designed for distributed locking and leader election.
* **Operator:** A method of packaging, deploying, and managing a Kubernetes application. It combines a Custom Resource and a controller.
* **Reconciliation Loop:** The core control loop in a controller that is triggered by resource changes and executes the logic to enforce the desired state.
* **StatefulSet:** A Kubernetes workload API object used to manage stateful applications.