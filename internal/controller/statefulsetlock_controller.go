/*
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
*/

package controller

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel/codes"
	appsv1 "k8s.io/api/apps/v1"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	appv1 "github.com/anukkrit/statefulset-leader-election-operator/api/v1"
	"github.com/anukkrit/statefulset-leader-election-operator/internal/logging"
	"github.com/anukkrit/statefulset-leader-election-operator/internal/metrics"
	"github.com/anukkrit/statefulset-leader-election-operator/internal/tracing"
)

const (
	// StatefulSetLockFinalizer is the finalizer used for cleanup
	StatefulSetLockFinalizer = "statefulsetlock.app.anukkrit.me/finalizer"

	// Pod role labels
	PodRoleLabel  = "sts-role"
	PodRoleWriter = "writer"
	PodRoleReader = "reader"

	// Condition types
	ConditionTypeAvailable   = "Available"
	ConditionTypeProgressing = "Progressing"

	// Condition reasons
	ReasonReconciling         = "Reconciling"
	ReasonReconcileError      = "ReconcileError"
	ReasonAvailable           = "Available"
	ReasonStatefulSetNotFound = "StatefulSetNotFound"
	ReasonValidationError     = "ValidationError"
	ReasonLeaseNotFound       = "LeaseNotFound"
	ReasonPodsNotFound        = "PodsNotFound"
)

// ValidationError represents a validation error with details
type ValidationError struct {
	Field   string
	Message string
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("validation error for field %s: %s", e.Field, e.Message)
}

// StatefulSetLockReconciler reconciles a StatefulSetLock object
type StatefulSetLockReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// fetchStatefulSetLock fetches the StatefulSetLock resource
func (r *StatefulSetLockReconciler) fetchStatefulSetLock(ctx context.Context, namespacedName types.NamespacedName) (*appv1.StatefulSetLock, error) {
	ctx, span := tracing.StartSpan(ctx, "fetchStatefulSetLock",
		tracing.NamespaceAttr(namespacedName.Namespace),
		tracing.StatefulSetLockAttr(namespacedName.Name),
	)
	defer span.End()

	logger := logging.LoggerFromContext(ctx)
	start := time.Now()

	var statefulSetLock appv1.StatefulSetLock
	if err := r.Get(ctx, namespacedName, &statefulSetLock); err != nil {
		duration := time.Since(start)
		if errors.IsNotFound(err) {
			logging.LogResourceFetch(logger, "StatefulSetLock", namespacedName.String(), false, duration)
			tracing.SetSpanStatus(span, codes.Error, "StatefulSetLock not found")
			return nil, fmt.Errorf("StatefulSetLock %s not found", namespacedName)
		}
		logging.LogResourceFetch(logger, "StatefulSetLock", namespacedName.String(), false, duration)
		tracing.RecordError(span, err)
		tracing.SetSpanStatus(span, codes.Error, "Failed to fetch StatefulSetLock")
		return nil, fmt.Errorf("failed to fetch StatefulSetLock %s: %w", namespacedName, err)
	}

	duration := time.Since(start)
	logging.LogResourceFetch(logger, "StatefulSetLock", namespacedName.String(), true, duration)
	tracing.SetSpanStatus(span, codes.Ok, "StatefulSetLock fetched successfully")
	return &statefulSetLock, nil
}

// fetchStatefulSet fetches the target StatefulSet resource
func (r *StatefulSetLockReconciler) fetchStatefulSet(ctx context.Context, namespace, name string) (*appsv1.StatefulSet, error) {
	ctx, span := tracing.StartSpan(ctx, "fetchStatefulSet",
		tracing.NamespaceAttr(namespace),
		tracing.StatefulSetAttr(name),
	)
	defer span.End()

	logger := logging.LoggerFromContext(ctx)
	start := time.Now()

	var statefulSet appsv1.StatefulSet
	namespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}

	if err := r.Get(ctx, namespacedName, &statefulSet); err != nil {
		duration := time.Since(start)
		if errors.IsNotFound(err) {
			logging.LogResourceFetch(logger, "StatefulSet", name, false, duration)
			tracing.SetSpanStatus(span, codes.Error, "StatefulSet not found")
			return nil, fmt.Errorf("StatefulSet %s not found in namespace %s", name, namespace)
		}
		logging.LogResourceFetch(logger, "StatefulSet", name, false, duration)
		tracing.RecordError(span, err)
		tracing.SetSpanStatus(span, codes.Error, "Failed to fetch StatefulSet")
		return nil, fmt.Errorf("failed to fetch StatefulSet %s in namespace %s: %w", name, namespace, err)
	}

	duration := time.Since(start)
	logging.LogResourceFetch(logger, "StatefulSet", name, true, duration)
	tracing.SetSpanStatus(span, codes.Ok, "StatefulSet fetched successfully")
	return &statefulSet, nil
}

// fetchStatefulSetPods fetches all pods belonging to the StatefulSet
func (r *StatefulSetLockReconciler) fetchStatefulSetPods(ctx context.Context, statefulSet *appsv1.StatefulSet) ([]corev1.Pod, error) {
	var podList corev1.PodList

	// Create label selector for StatefulSet pods
	labelSelector := client.MatchingLabels{}
	if statefulSet.Spec.Selector != nil && statefulSet.Spec.Selector.MatchLabels != nil {
		labelSelector = client.MatchingLabels(statefulSet.Spec.Selector.MatchLabels)
	}

	listOpts := []client.ListOption{
		client.InNamespace(statefulSet.Namespace),
		labelSelector,
	}

	if err := r.List(ctx, &podList, listOpts...); err != nil {
		return nil, fmt.Errorf("failed to list pods for StatefulSet %s: %w", statefulSet.Name, err)
	}

	// Filter pods that are owned by this StatefulSet
	var ownedPods []corev1.Pod
	for _, pod := range podList.Items {
		for _, ownerRef := range pod.OwnerReferences {
			if ownerRef.Kind == "StatefulSet" && ownerRef.Name == statefulSet.Name && ownerRef.UID == statefulSet.UID {
				ownedPods = append(ownedPods, pod)
				break
			}
		}
	}

	return ownedPods, nil
}

// fetchLease fetches the coordination lease resource
func (r *StatefulSetLockReconciler) fetchLease(ctx context.Context, namespace, name string) (*coordinationv1.Lease, error) {
	ctx, span := tracing.StartSpan(ctx, "fetchLease",
		tracing.NamespaceAttr(namespace),
		tracing.LeaseNameAttr(name),
	)
	defer span.End()

	logger := logging.LoggerFromContext(ctx)
	start := time.Now()

	var lease coordinationv1.Lease
	namespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}

	if err := r.Get(ctx, namespacedName, &lease); err != nil {
		duration := time.Since(start)
		if errors.IsNotFound(err) {
			logging.LogResourceFetch(logger, "Lease", name, false, duration)
			tracing.SetSpanStatus(span, codes.Error, "Lease not found")
			return nil, fmt.Errorf("Lease %s not found in namespace %s", name, namespace)
		}
		logging.LogResourceFetch(logger, "Lease", name, false, duration)
		tracing.RecordError(span, err)
		tracing.SetSpanStatus(span, codes.Error, "Failed to fetch Lease")
		return nil, fmt.Errorf("failed to fetch Lease %s in namespace %s: %w", name, namespace, err)
	}

	duration := time.Since(start)
	logging.LogResourceFetch(logger, "Lease", name, true, duration)
	tracing.SetSpanStatus(span, codes.Ok, "Lease fetched successfully")
	return &lease, nil
}

// validateStatefulSetLockSpec validates the StatefulSetLock specification
func (r *StatefulSetLockReconciler) validateStatefulSetLockSpec(spec *appv1.StatefulSetLockSpec) error {
	var validationErrors []ValidationError

	// Validate StatefulSetName
	if spec.StatefulSetName == "" {
		validationErrors = append(validationErrors, ValidationError{
			Field:   "statefulSetName",
			Message: "cannot be empty",
		})
	}

	// Validate LeaseName
	if spec.LeaseName == "" {
		validationErrors = append(validationErrors, ValidationError{
			Field:   "leaseName",
			Message: "cannot be empty",
		})
	}

	// Validate LeaseDurationSeconds
	if spec.LeaseDurationSeconds <= 0 {
		validationErrors = append(validationErrors, ValidationError{
			Field:   "leaseDurationSeconds",
			Message: "must be greater than 0",
		})
	}

	if spec.LeaseDurationSeconds > 3600 {
		validationErrors = append(validationErrors, ValidationError{
			Field:   "leaseDurationSeconds",
			Message: "must be less than or equal to 3600 seconds",
		})
	}

	// Return combined validation errors
	if len(validationErrors) > 0 {
		var errorMessages []string
		for _, err := range validationErrors {
			errorMessages = append(errorMessages, err.Error())
		}
		return fmt.Errorf("validation failed: %v", errorMessages)
	}

	return nil
}

// validateStatefulSet validates that the StatefulSet is in a valid state
func (r *StatefulSetLockReconciler) validateStatefulSet(statefulSet *appsv1.StatefulSet) error {
	if statefulSet.Spec.Replicas == nil || *statefulSet.Spec.Replicas == 0 {
		return fmt.Errorf("StatefulSet %s has no replicas configured", statefulSet.Name)
	}

	if statefulSet.Spec.Selector == nil || len(statefulSet.Spec.Selector.MatchLabels) == 0 {
		return fmt.Errorf("StatefulSet %s has no selector configured", statefulSet.Name)
	}

	return nil
}

// +kubebuilder:rbac:groups=app.anukkrit.me,resources=statefulsetlocks,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=app.anukkrit.me,resources=statefulsetlocks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=app.anukkrit.me,resources=statefulsetlocks/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;patch
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *StatefulSetLockReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ctx, span := tracing.StartSpan(ctx, "Reconcile",
		tracing.NamespaceAttr(req.Namespace),
		tracing.StatefulSetLockAttr(req.Name),
	)
	defer span.End()

	logger := logging.LoggerFromContext(ctx)
	start := time.Now()

	logging.LogReconciliationStart(logger, req.Namespace, req.Name)

	// Fetch the StatefulSetLock instance
	var statefulSetLock appv1.StatefulSetLock
	if err := r.Get(ctx, req.NamespacedName, &statefulSetLock); err != nil {
		duration := time.Since(start)
		if errors.IsNotFound(err) {
			logging.Info(logger, "StatefulSetLock resource not found, likely deleted")
			logging.LogReconciliationEnd(logger, req.Namespace, req.Name, duration, nil)
			tracing.SetSpanStatus(span, codes.Ok, "StatefulSetLock not found (deleted)")
			return ctrl.Result{}, nil
		}
		logging.Error(logger, err, "Failed to get StatefulSetLock")
		logging.LogReconciliationEnd(logger, req.Namespace, req.Name, duration, err)
		metrics.RecordReconciliationError(req.Namespace, req.Name, "fetch_error")
		tracing.RecordError(span, err)
		tracing.SetSpanStatus(span, codes.Error, "Failed to fetch StatefulSetLock")
		return ctrl.Result{}, err
	}

	// Add span attributes with StatefulSetLock details
	tracing.AddSpanAttributes(span,
		tracing.StatefulSetAttr(statefulSetLock.Spec.StatefulSetName),
		tracing.LeaseNameAttr(statefulSetLock.Spec.LeaseName),
		tracing.LeaseDurationAttr(statefulSetLock.Spec.LeaseDurationSeconds),
	)

	// Handle deletion
	if statefulSetLock.DeletionTimestamp != nil {
		logging.Info(logger, "StatefulSetLock is being deleted, handling cleanup")
		result, err := r.handleDeletion(ctx, &statefulSetLock)
		duration := time.Since(start)
		logging.LogReconciliationEnd(logger, req.Namespace, req.Name, duration, err)
		if err != nil {
			metrics.RecordReconciliationError(req.Namespace, req.Name, "deletion_error")
			tracing.RecordError(span, err)
			tracing.SetSpanStatus(span, codes.Error, "Deletion failed")
		} else {
			metrics.RecordReconciliationDuration(req.Namespace, req.Name, duration.Seconds())
			tracing.SetSpanStatus(span, codes.Ok, "Deletion completed")
		}
		return result, err
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(&statefulSetLock, StatefulSetLockFinalizer) {
		logging.Info(logger, "Adding finalizer to StatefulSetLock")
		controllerutil.AddFinalizer(&statefulSetLock, StatefulSetLockFinalizer)
		if err := r.Update(ctx, &statefulSetLock); err != nil {
			duration := time.Since(start)
			logging.Error(logger, err, "Failed to add finalizer")
			logging.LogReconciliationEnd(logger, req.Namespace, req.Name, duration, err)
			metrics.RecordReconciliationError(req.Namespace, req.Name, "finalizer_error")
			tracing.RecordError(span, err)
			tracing.SetSpanStatus(span, codes.Error, "Failed to add finalizer")
			return ctrl.Result{}, err
		}
		duration := time.Since(start)
		logging.LogReconciliationEnd(logger, req.Namespace, req.Name, duration, nil)
		metrics.RecordReconciliationDuration(req.Namespace, req.Name, duration.Seconds())
		tracing.SetSpanStatus(span, codes.Ok, "Finalizer added")
		return ctrl.Result{Requeue: true}, nil
	}

	// Set progressing condition
	r.setCondition(&statefulSetLock, ConditionTypeProgressing, metav1.ConditionTrue, ReasonReconciling, "Reconciling StatefulSetLock")

	// Perform leader election reconciliation
	// check do we need a leader election - pre condition for leader election - webhook mutation
	result, err := r.performLeaderElection(ctx, &statefulSetLock)
	if err != nil {
		metrics.RecordReconciliationError(req.Namespace, req.Name, "leader_election_error")
		tracing.RecordError(span, err)
		return r.handleReconcileError(ctx, &statefulSetLock, err, "Failed to perform leader election")
	}

	// Set available condition on successful reconciliation only if not already set to false
	if r.getConditionStatus(&statefulSetLock, ConditionTypeAvailable) != metav1.ConditionFalse {
		r.setCondition(&statefulSetLock, ConditionTypeAvailable, metav1.ConditionTrue, ReasonAvailable, "StatefulSetLock is ready")
	}

	// Update status
	statefulSetLock.Status.ObservedGeneration = statefulSetLock.Generation
	if err := r.Status().Update(ctx, &statefulSetLock); err != nil {
		duration := time.Since(start)
		logging.Error(logger, err, "Failed to update StatefulSetLock status")
		logging.LogReconciliationEnd(logger, req.Namespace, req.Name, duration, err)
		metrics.RecordReconciliationError(req.Namespace, req.Name, "status_update_error")
		tracing.RecordError(span, err)
		tracing.SetSpanStatus(span, codes.Error, "Failed to update status")
		return ctrl.Result{}, err
	}

	// Update metrics with current leader info
	metrics.UpdateCurrentLeaderInfo(req.Namespace, req.Name, statefulSetLock.Spec.StatefulSetName, statefulSetLock.Status.WriterPod)

	duration := time.Since(start)
	logging.Info(logging.WithLeader(logger, statefulSetLock.Status.WriterPod), "Reconciliation completed successfully")
	logging.LogReconciliationEnd(logger, req.Namespace, req.Name, duration, nil)
	metrics.RecordReconciliationDuration(req.Namespace, req.Name, duration.Seconds())
	tracing.AddSpanAttributes(span, tracing.LeaderPodAttr(statefulSetLock.Status.WriterPod))
	tracing.SetSpanStatus(span, codes.Ok, "Reconciliation completed successfully")

	// Return the result from leader election (includes requeue timing)
	return result, nil
}

// handleDeletion handles the cleanup logic when a StatefulSetLock is being deleted
func (r *StatefulSetLockReconciler) handleDeletion(ctx context.Context, statefulSetLock *appv1.StatefulSetLock) (ctrl.Result, error) {
	ctx, span := tracing.StartSpan(ctx, "handleDeletion",
		tracing.NamespaceAttr(statefulSetLock.Namespace),
		tracing.StatefulSetLockAttr(statefulSetLock.Name),
		tracing.StatefulSetAttr(statefulSetLock.Spec.StatefulSetName),
		tracing.LeaseNameAttr(statefulSetLock.Spec.LeaseName),
	)
	defer span.End()

	logger := logging.LoggerFromContext(ctx)
	start := time.Now()

	if !controllerutil.ContainsFinalizer(statefulSetLock, StatefulSetLockFinalizer) {
		logging.Info(logger, "Finalizer not present, skipping cleanup")
		tracing.SetSpanStatus(span, codes.Ok, "No finalizer present")
		return ctrl.Result{}, nil
	}

	logging.Info(
		logging.WithStatefulSetLock(logger, statefulSetLock.Namespace, statefulSetLock.Name),
		"Performing cleanup for StatefulSetLock",
	)

	// Remove pod role labels before deleting the lease
	if statefulSet, err := r.fetchStatefulSet(ctx, statefulSetLock.Namespace, statefulSetLock.Spec.StatefulSetName); err == nil {
		if allPods, err := r.fetchStatefulSetPods(ctx, statefulSet); err == nil {
			if err := r.removePodRoleLabels(ctx, allPods); err != nil {
				logging.Error(logger, err, "Failed to remove pod role labels during cleanup")
				metrics.RecordPodLabelingError(statefulSetLock.Namespace, statefulSetLock.Name, "cleanup")
				// Continue with cleanup even if labeling fails
			}
		} else {
			logging.Error(logger, err, "Failed to fetch pods during cleanup, skipping label removal")
		}
	} else {
		logging.Error(logger, err, "Failed to fetch StatefulSet during cleanup, skipping label removal")
	}

	// Delete the associated lease
	if err := r.deleteLease(ctx, statefulSetLock.Namespace, statefulSetLock.Spec.LeaseName); err != nil {
		duration := time.Since(start)
		logging.Error(
			logging.WithDuration(logger, duration),
			err,
			"Failed to delete lease during cleanup",
		)
		metrics.RecordReconciliationError(statefulSetLock.Namespace, statefulSetLock.Name, "lease_deletion_error")
		tracing.RecordError(span, err)
		tracing.SetSpanStatus(span, codes.Error, "Failed to delete lease")
		return ctrl.Result{RequeueAfter: time.Minute * 1}, err
	}

	// Clear metrics for this StatefulSetLock
	metrics.UpdateCurrentLeaderInfo(statefulSetLock.Namespace, statefulSetLock.Name, statefulSetLock.Spec.StatefulSetName, "")

	// Remove finalizer after successful cleanup
	controllerutil.RemoveFinalizer(statefulSetLock, StatefulSetLockFinalizer)
	if err := r.Update(ctx, statefulSetLock); err != nil {
		duration := time.Since(start)
		logging.Error(
			logging.WithDuration(logger, duration),
			err,
			"Failed to remove finalizer",
		)
		metrics.RecordReconciliationError(statefulSetLock.Namespace, statefulSetLock.Name, "finalizer_removal_error")
		tracing.RecordError(span, err)
		tracing.SetSpanStatus(span, codes.Error, "Failed to remove finalizer")
		return ctrl.Result{}, err
	}

	duration := time.Since(start)
	logging.Info(
		logging.WithDuration(logger, duration),
		"Cleanup completed, finalizer removed",
	)
	tracing.SetSpanStatus(span, codes.Ok, "Cleanup completed successfully")
	return ctrl.Result{}, nil
}

// getConditionStatus gets the status of a condition in the StatefulSetLock status
func (r *StatefulSetLockReconciler) getConditionStatus(statefulSetLock *appv1.StatefulSetLock, conditionType string) metav1.ConditionStatus {
	for _, condition := range statefulSetLock.Status.Conditions {
		if condition.Type == conditionType {
			return condition.Status
		}
	}
	return metav1.ConditionUnknown
}

// setCondition sets or updates a condition in the StatefulSetLock status
func (r *StatefulSetLockReconciler) setCondition(statefulSetLock *appv1.StatefulSetLock, conditionType string, status metav1.ConditionStatus, reason, message string) {
	condition := metav1.Condition{
		Type:               conditionType,
		Status:             status,
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	}

	// Find existing condition
	for i, existingCondition := range statefulSetLock.Status.Conditions {
		if existingCondition.Type == conditionType {
			// Update existing condition if status changed
			if existingCondition.Status != status || existingCondition.Reason != reason {
				statefulSetLock.Status.Conditions[i] = condition
			} else {
				// Keep the original LastTransitionTime if status hasn't changed
				condition.LastTransitionTime = existingCondition.LastTransitionTime
				statefulSetLock.Status.Conditions[i] = condition
			}
			return
		}
	}

	// Add new condition if not found
	statefulSetLock.Status.Conditions = append(statefulSetLock.Status.Conditions, condition)
}

// handleReconcileError handles errors during reconciliation and updates conditions
func (r *StatefulSetLockReconciler) handleReconcileError(ctx context.Context, statefulSetLock *appv1.StatefulSetLock, err error, message string) (ctrl.Result, error) {
	logger := logging.LoggerFromContext(ctx)

	logging.Error(
		logging.WithStatefulSetLock(logger, statefulSetLock.Namespace, statefulSetLock.Name),
		err,
		message,
	)

	// Record error metrics
	metrics.RecordReconciliationError(statefulSetLock.Namespace, statefulSetLock.Name, "reconcile_error")

	// Set error conditions
	r.setCondition(statefulSetLock, ConditionTypeAvailable, metav1.ConditionFalse, ReasonReconcileError, fmt.Sprintf("%s: %v", message, err))
	r.setCondition(statefulSetLock, ConditionTypeProgressing, metav1.ConditionFalse, ReasonReconcileError, fmt.Sprintf("%s: %v", message, err))

	// Update status with error conditions
	statefulSetLock.Status.ObservedGeneration = statefulSetLock.Generation
	if statusErr := r.Status().Update(ctx, statefulSetLock); statusErr != nil {
		logging.Error(
			logging.WithStatefulSetLock(logger, statefulSetLock.Namespace, statefulSetLock.Name),
			statusErr,
			"Failed to update status with error condition",
		)
		metrics.RecordReconciliationError(statefulSetLock.Namespace, statefulSetLock.Name, "status_update_error")
		return ctrl.Result{}, statusErr
	}

	// Return with requeue after delay for transient errors
	return ctrl.Result{RequeueAfter: time.Minute * 1}, err
}

// Pod Management Helper Functions

// IsPodReady checks if a pod is in Ready condition
func IsPodReady(pod corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

// GetReadyPods returns pods in Ready state from a list of pods
func GetReadyPods(pods []corev1.Pod) []corev1.Pod {
	var readyPods []corev1.Pod
	for _, pod := range pods {
		if IsPodReady(pod) {
			readyPods = append(readyPods, pod)
		}
	}
	return readyPods
}

// SortPodsByOrdinal sorts pods by their ordinal index for deterministic ordering
// StatefulSet pods follow the naming pattern: <statefulset-name>-<ordinal>
func SortPodsByOrdinal(pods []corev1.Pod) []corev1.Pod {
	// Create a copy to avoid modifying the original slice
	sortedPods := make([]corev1.Pod, len(pods))
	copy(sortedPods, pods)

	sort.Slice(sortedPods, func(i, j int) bool {
		ordinalI := extractOrdinalFromPodName(sortedPods[i].Name)
		ordinalJ := extractOrdinalFromPodName(sortedPods[j].Name)
		return ordinalI < ordinalJ
	})

	return sortedPods
}

// extractOrdinalFromPodName extracts the ordinal number from a StatefulSet pod name
// Expected format: <statefulset-name>-<ordinal>
func extractOrdinalFromPodName(podName string) int {
	// Find the last dash in the pod name
	lastDashIndex := strings.LastIndex(podName, "-")
	if lastDashIndex == -1 {
		// If no dash found, return a high number to sort it last
		return 999999
	}

	// Extract the ordinal part
	ordinalStr := podName[lastDashIndex+1:]

	// Check if the ordinal string is empty
	if ordinalStr == "" {
		return 999999
	}

	ordinal, err := strconv.Atoi(ordinalStr)
	if err != nil || ordinal < 0 {
		// If conversion fails or ordinal is negative, return a high number to sort it last
		return 999999
	}

	return ordinal
}

// Lease Management Helper Functions

// IsLeaseExpired checks if a lease has expired based on the lease duration
func IsLeaseExpired(lease *coordinationv1.Lease, durationSeconds int32) bool {
	if lease == nil {
		return true
	}

	// If lease has no renew time, consider it expired
	if lease.Spec.RenewTime == nil {
		return true
	}

	// Calculate expiration time
	renewTime := lease.Spec.RenewTime.Time
	leaseDuration := time.Duration(durationSeconds) * time.Second
	expirationTime := renewTime.Add(leaseDuration)

	// Check if current time is past expiration
	return time.Now().After(expirationTime)
}

// ShouldElectNewLeader determines if a new leader election should be triggered
func ShouldElectNewLeader(lease *coordinationv1.Lease, readyPods []corev1.Pod, durationSeconds int32) bool {
	// If no lease exists, we need to elect a leader
	if lease == nil {
		return len(readyPods) > 0
	}

	// If lease is expired, we need to elect a new leader
	if IsLeaseExpired(lease, durationSeconds) {
		return len(readyPods) > 0
	}

	// If lease exists but has no holder identity, we need to elect a leader
	if lease.Spec.HolderIdentity == nil || *lease.Spec.HolderIdentity == "" {
		return len(readyPods) > 0
	}

	// Check if the current lease holder is still ready
	currentHolder := *lease.Spec.HolderIdentity
	for _, pod := range readyPods {
		if pod.Name == currentHolder {
			// Current holder is still ready, no need for new election
			return false
		}
	}

	// Current holder is not ready, need new election if we have ready pods
	return len(readyPods) > 0
}

// createOrUpdateLease creates a new lease or updates an existing one with the given leader
func (r *StatefulSetLockReconciler) createOrUpdateLease(ctx context.Context, namespace, leaseName, leaderPodName string, durationSeconds int32) error {
	ctx, span := tracing.StartSpan(ctx, "createOrUpdateLease",
		tracing.NamespaceAttr(namespace),
		tracing.LeaseNameAttr(leaseName),
		tracing.LeaderPodAttr(leaderPodName),
		tracing.LeaseDurationAttr(durationSeconds),
	)
	defer span.End()

	logger := logging.LoggerFromContext(ctx)
	start := time.Now()

	// Try to fetch existing lease
	existingLease, err := r.fetchLease(ctx, namespace, leaseName)
	if err != nil && !strings.Contains(err.Error(), "not found") {
		tracing.RecordError(span, err)
		tracing.SetSpanStatus(span, codes.Error, "Failed to fetch existing lease")
		return fmt.Errorf("failed to fetch lease %s: %w", leaseName, err)
	}

	now := metav1.NewMicroTime(time.Now())
	operation := "lease_renewal"

	if existingLease == nil {
		// Create new lease
		operation = "lease_creation"
		logging.Info(logging.WithLease(logging.WithLeader(logger, leaderPodName), leaseName), "Creating new lease")

		lease := &coordinationv1.Lease{
			ObjectMeta: metav1.ObjectMeta{
				Name:      leaseName,
				Namespace: namespace,
			},
			Spec: coordinationv1.LeaseSpec{
				HolderIdentity:       &leaderPodName,
				LeaseDurationSeconds: &durationSeconds,
				RenewTime:            &now,
			},
		}

		if err := r.Create(ctx, lease); err != nil {
			tracing.RecordError(span, err)
			tracing.SetSpanStatus(span, codes.Error, "Failed to create lease")
			return fmt.Errorf("failed to create lease %s: %w", leaseName, err)
		}

		duration := time.Since(start)
		logging.LogLeaseOperation(logger, "Lease created successfully", leaseName, leaderPodName, duration)
		tracing.SetSpanStatus(span, codes.Ok, "Lease created successfully")
	} else {
		// Update existing lease
		oldLeader := getStringValue(existingLease.Spec.HolderIdentity)
		logging.Info(
			logging.WithLease(logging.WithLeader(logger, leaderPodName), leaseName),
			"Updating existing lease",
			"old_leader", oldLeader,
		)

		existingLease.Spec.HolderIdentity = &leaderPodName
		existingLease.Spec.LeaseDurationSeconds = &durationSeconds
		existingLease.Spec.RenewTime = &now

		if err := r.Update(ctx, existingLease); err != nil {
			tracing.RecordError(span, err)
			tracing.SetSpanStatus(span, codes.Error, "Failed to update lease")
			return fmt.Errorf("failed to update lease %s: %w", leaseName, err)
		}

		duration := time.Since(start)
		logging.LogLeaseOperation(logger, "Lease updated successfully", leaseName, leaderPodName, duration)
		tracing.SetSpanStatus(span, codes.Ok, "Lease updated successfully")
	}

	// Record metrics
	duration := time.Since(start)
	if operation == "lease_creation" {
		// For new leases, we consider this a leader election
		metrics.RecordLeaderElection(namespace, "", "", "new_lease", duration.Seconds())
	} else {
		// For existing leases, this is a renewal
		metrics.RecordLeaseRenewal(namespace, "", leaseName, duration.Seconds())
	}

	return nil
}

// deleteLease deletes the specified lease resource
func (r *StatefulSetLockReconciler) deleteLease(ctx context.Context, namespace, leaseName string) error {
	ctx, span := tracing.StartSpan(ctx, "deleteLease",
		tracing.NamespaceAttr(namespace),
		tracing.LeaseNameAttr(leaseName),
	)
	defer span.End()

	logger := logging.LoggerFromContext(ctx)
	start := time.Now()

	// Try to fetch the lease first
	lease, err := r.fetchLease(ctx, namespace, leaseName)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			duration := time.Since(start)
			logging.Info(
				logging.WithDuration(logging.WithLease(logger, leaseName), duration),
				"Lease already deleted or does not exist",
			)
			tracing.SetSpanStatus(span, codes.Ok, "Lease already deleted")
			return nil
		}
		tracing.RecordError(span, err)
		tracing.SetSpanStatus(span, codes.Error, "Failed to fetch lease for deletion")
		return fmt.Errorf("failed to fetch lease %s for deletion: %w", leaseName, err)
	}

	// Delete the lease
	logging.Info(logging.WithLease(logger, leaseName), "Deleting lease")
	if err := r.Delete(ctx, lease); err != nil {
		tracing.RecordError(span, err)
		tracing.SetSpanStatus(span, codes.Error, "Failed to delete lease")
		return fmt.Errorf("failed to delete lease %s: %w", leaseName, err)
	}

	duration := time.Since(start)
	logging.Info(
		logging.WithDuration(logging.WithLease(logger, leaseName), duration),
		"Successfully deleted lease",
	)
	tracing.SetSpanStatus(span, codes.Ok, "Lease deleted successfully")
	return nil
}

// performLeaderElection performs the core leader election algorithm
func (r *StatefulSetLockReconciler) performLeaderElection(ctx context.Context, statefulSetLock *appv1.StatefulSetLock) (ctrl.Result, error) {
	ctx, span := tracing.StartSpan(ctx, "performLeaderElection",
		tracing.NamespaceAttr(statefulSetLock.Namespace),
		tracing.StatefulSetLockAttr(statefulSetLock.Name),
		tracing.StatefulSetAttr(statefulSetLock.Spec.StatefulSetName),
		tracing.LeaseNameAttr(statefulSetLock.Spec.LeaseName),
		tracing.LeaseDurationAttr(statefulSetLock.Spec.LeaseDurationSeconds),
	)
	defer span.End()

	logger := logging.LoggerFromContext(ctx)
	start := time.Now()
	// todo: check need to leader election needed

	logging.Info(logger, "Starting leader election process")

	// Step 1: Validate StatefulSetLock spec
	if err := r.validateStatefulSetLockSpec(&statefulSetLock.Spec); err != nil {
		logging.Error(logger, err, "StatefulSetLock spec validation failed")
		r.setCondition(statefulSetLock, ConditionTypeAvailable, metav1.ConditionFalse, ReasonValidationError, fmt.Sprintf("Spec validation failed: %v", err))
		metrics.RecordReconciliationError(statefulSetLock.Namespace, statefulSetLock.Name, "validation_error")
		tracing.RecordError(span, err)
		tracing.SetSpanStatus(span, codes.Error, "Spec validation failed")
		return ctrl.Result{RequeueAfter: time.Minute * 1}, err
	}

	// Step 2: Fetch and validate StatefulSet
	statefulSet, err := r.fetchStatefulSet(ctx, statefulSetLock.Namespace, statefulSetLock.Spec.StatefulSetName)
	if err != nil {
		logging.Error(logging.WithStatefulSet(logger, statefulSetLock.Spec.StatefulSetName), err, "Failed to fetch StatefulSet")
		r.setCondition(statefulSetLock, ConditionTypeAvailable, metav1.ConditionFalse, ReasonStatefulSetNotFound, fmt.Sprintf("StatefulSet not found: %v", err))
		metrics.RecordReconciliationError(statefulSetLock.Namespace, statefulSetLock.Name, "statefulset_not_found")
		tracing.RecordError(span, err)
		tracing.SetSpanStatus(span, codes.Error, "StatefulSet not found")
		return ctrl.Result{RequeueAfter: time.Minute * 1}, err
	}

	if err := r.validateStatefulSet(statefulSet); err != nil {
		logging.Error(logging.WithStatefulSet(logger, statefulSet.Name), err, "StatefulSet validation failed")
		r.setCondition(statefulSetLock, ConditionTypeAvailable, metav1.ConditionFalse, ReasonValidationError, fmt.Sprintf("StatefulSet validation failed: %v", err))
		metrics.RecordReconciliationError(statefulSetLock.Namespace, statefulSetLock.Name, "statefulset_validation_error")
		tracing.RecordError(span, err)
		tracing.SetSpanStatus(span, codes.Error, "StatefulSet validation failed")
		return ctrl.Result{RequeueAfter: time.Minute * 1}, err
	}

	// Step 3: Fetch StatefulSet pods
	allPods, err := r.fetchStatefulSetPods(ctx, statefulSet)
	if err != nil {
		logging.Error(logging.WithStatefulSet(logger, statefulSet.Name), err, "Failed to fetch StatefulSet pods")
		r.setCondition(statefulSetLock, ConditionTypeAvailable, metav1.ConditionFalse, ReasonPodsNotFound, fmt.Sprintf("Failed to fetch pods: %v", err))
		metrics.RecordReconciliationError(statefulSetLock.Namespace, statefulSetLock.Name, "pods_fetch_error")
		tracing.RecordError(span, err)
		tracing.SetSpanStatus(span, codes.Error, "Failed to fetch pods")
		return ctrl.Result{RequeueAfter: time.Minute * 1}, err
	}

	// Step 4: Filter and sort ready pods
	readyPods := GetReadyPods(allPods)
	sortedReadyPods := SortPodsByOrdinal(readyPods)

	// Update metrics with pod counts
	metrics.UpdateReadyPodsCount(statefulSetLock.Namespace, statefulSetLock.Name, statefulSetLock.Spec.StatefulSetName, len(readyPods))

	// Add tracing attributes for pod counts
	tracing.AddSpanAttributes(span,
		tracing.TotalPodsAttr(len(allPods)),
		tracing.ReadyPodsAttr(len(readyPods)),
	)

	logging.Info(
		logging.WithPodCounts(logger, len(allPods), len(readyPods)),
		"Pod status summary",
		"ready_pod_names", getPodNames(sortedReadyPods),
	)

	// Step 5: Fetch existing lease (if any)
	existingLease, err := r.fetchLease(ctx, statefulSetLock.Namespace, statefulSetLock.Spec.LeaseName)
	if err != nil && !strings.Contains(err.Error(), "not found") {
		logging.Error(logging.WithLease(logger, statefulSetLock.Spec.LeaseName), err, "Failed to fetch lease")
		r.setCondition(statefulSetLock, ConditionTypeAvailable, metav1.ConditionFalse, ReasonLeaseNotFound, fmt.Sprintf("Failed to fetch lease: %v", err))
		metrics.RecordReconciliationError(statefulSetLock.Namespace, statefulSetLock.Name, "lease_fetch_error")
		tracing.RecordError(span, err)
		tracing.SetSpanStatus(span, codes.Error, "Failed to fetch lease")
		return ctrl.Result{RequeueAfter: time.Minute * 1}, err
	}

	// Step 6: Determine if new leader election is needed
	needsNewElection := ShouldElectNewLeader(existingLease, sortedReadyPods, statefulSetLock.Spec.LeaseDurationSeconds)
	leaseExpired := IsLeaseExpired(existingLease, statefulSetLock.Spec.LeaseDurationSeconds)

	// Update lease expiration metrics
	if existingLease != nil {
		var secondsUntilExpiration float64
		if existingLease.Spec.RenewTime != nil {
			renewTime := existingLease.Spec.RenewTime.Time
			leaseDuration := time.Duration(statefulSetLock.Spec.LeaseDurationSeconds) * time.Second
			expirationTime := renewTime.Add(leaseDuration)
			secondsUntilExpiration = time.Until(expirationTime).Seconds()
		}
		metrics.UpdateLeaseExpiration(statefulSetLock.Namespace, statefulSetLock.Name, statefulSetLock.Spec.LeaseName, secondsUntilExpiration)
	}

	// Add tracing attributes for lease status
	tracing.AddSpanAttributes(span,
		tracing.LeaseExpiredAttr(leaseExpired),
	)

	if existingLease != nil {
		currentHolder := getStringValue(existingLease.Spec.HolderIdentity)
		logging.Info(
			logging.WithLeaseInfo(logging.WithLease(logger, statefulSetLock.Spec.LeaseName), currentHolder, leaseExpired, statefulSetLock.Spec.LeaseDurationSeconds),
			"Current lease status",
			"needs_new_election", needsNewElection,
		)
		tracing.AddSpanAttributes(span, tracing.LeaderPodAttr(currentHolder))
	} else {
		logging.Info(logger, "No existing lease found", "needs_new_election", needsNewElection)
	}

	// Step 7: Handle case where no ready pods are available
	if len(sortedReadyPods) == 0 {
		logging.Info(logger, "No ready pods available for leader election")
		r.setCondition(statefulSetLock, ConditionTypeAvailable, metav1.ConditionFalse, ReasonPodsNotFound, "No ready pods available for leader election")
		statefulSetLock.Status.WriterPod = ""
		// Clear current leader metrics
		metrics.UpdateCurrentLeaderInfo(statefulSetLock.Namespace, statefulSetLock.Name, statefulSetLock.Spec.StatefulSetName, "")
		tracing.SetSpanStatus(span, codes.Ok, "No ready pods available")
		// Requeue more frequently when waiting for pods to become ready
		return ctrl.Result{RequeueAfter: time.Second * 30}, nil
	}

	// Step 8: Handle scenarios based on election needs
	if needsNewElection {
		electionStart := time.Now()

		// Elect new leader (lowest ordinal ready pod)
		newLeader := sortedReadyPods[0]
		electionReason := r.getElectionReason(existingLease, sortedReadyPods, statefulSetLock.Spec.LeaseDurationSeconds)

		logging.LogLeaderElection(logger, statefulSetLock.Namespace, statefulSetLock.Name, newLeader.Name, electionReason)

		// Add tracing attributes for the new leader
		tracing.AddSpanAttributes(span,
			tracing.LeaderPodAttr(newLeader.Name),
			tracing.ElectionReasonAttr(electionReason),
		)

		// Step 9: Create or update lease with new leader
		if err := r.createOrUpdateLease(ctx, statefulSetLock.Namespace, statefulSetLock.Spec.LeaseName, newLeader.Name, statefulSetLock.Spec.LeaseDurationSeconds); err != nil {
			logging.Error(logging.WithLeader(logger, newLeader.Name), err, "Failed to create or update lease")
			r.setCondition(statefulSetLock, ConditionTypeAvailable, metav1.ConditionFalse, ReasonReconcileError, fmt.Sprintf("Failed to update lease: %v", err))
			metrics.RecordReconciliationError(statefulSetLock.Namespace, statefulSetLock.Name, "lease_update_error")
			tracing.RecordError(span, err)
			tracing.SetSpanStatus(span, codes.Error, "Failed to update lease")
			return ctrl.Result{RequeueAfter: time.Minute * 1}, err
		}

		// Record leader election metrics
		electionDuration := time.Since(electionStart)
		metrics.RecordLeaderElection(statefulSetLock.Namespace, statefulSetLock.Name, statefulSetLock.Spec.StatefulSetName, electionReason, electionDuration.Seconds())

		// Update status with new leader
		statefulSetLock.Status.WriterPod = newLeader.Name
		logging.Info(logging.WithLeader(logger, newLeader.Name), "Successfully elected new leader")

		// Step 10: Update pod labels with new leadership
		if err := r.updatePodLabels(ctx, allPods, newLeader.Name); err != nil {
			logging.Error(logging.WithLeader(logger, newLeader.Name), err, "Failed to update pod labels after leader election")
			metrics.RecordPodLabelingError(statefulSetLock.Namespace, statefulSetLock.Name, newLeader.Name)
			// Don't fail the reconciliation for labeling errors, but log them
			// The lease and status have been updated successfully
		}

	} else {
		// No new election needed, update status with current leader
		if existingLease != nil && existingLease.Spec.HolderIdentity != nil {
			currentLeader := *existingLease.Spec.HolderIdentity
			statefulSetLock.Status.WriterPod = currentLeader
			logging.Info(logging.WithLeader(logger, currentLeader), "Current leader confirmed")

			// Renew lease to maintain leadership
			if err := r.createOrUpdateLease(ctx, statefulSetLock.Namespace, statefulSetLock.Spec.LeaseName, currentLeader, statefulSetLock.Spec.LeaseDurationSeconds); err != nil {
				logging.Error(logging.WithLeader(logger, currentLeader), err, "Failed to renew lease")
				r.setCondition(statefulSetLock, ConditionTypeAvailable, metav1.ConditionFalse, ReasonReconcileError, fmt.Sprintf("Failed to renew lease: %v", err))
				metrics.RecordReconciliationError(statefulSetLock.Namespace, statefulSetLock.Name, "lease_renewal_error")
				tracing.RecordError(span, err)
				tracing.SetSpanStatus(span, codes.Error, "Failed to renew lease")
				return ctrl.Result{RequeueAfter: time.Minute * 1}, err
			}
			logging.Info(logging.WithLeader(logger, currentLeader), "Successfully renewed lease")

			// Update pod labels to ensure they are correct (in case of pod restarts or label drift)
			if err := r.updatePodLabels(ctx, allPods, currentLeader); err != nil {
				logging.Error(logging.WithLeader(logger, currentLeader), err, "Failed to update pod labels during lease renewal")
				metrics.RecordPodLabelingError(statefulSetLock.Namespace, statefulSetLock.Name, currentLeader)
				// Don't fail the reconciliation for labeling errors, but log them
			}
		}
	}

	// Step 11: Schedule next reconciliation at half the lease duration
	requeueAfter := time.Duration(statefulSetLock.Spec.LeaseDurationSeconds/2) * time.Second
	duration := time.Since(start)

	logging.Info(
		logging.WithDuration(logging.WithLeader(logger, statefulSetLock.Status.WriterPod), duration),
		"Leader election completed successfully",
		"next_reconciliation", requeueAfter,
	)

	tracing.SetSpanStatus(span, codes.Ok, "Leader election completed successfully")
	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

// getElectionReason returns a human-readable reason for why a new election was triggered
func (r *StatefulSetLockReconciler) getElectionReason(lease *coordinationv1.Lease, readyPods []corev1.Pod, durationSeconds int32) string {
	if lease == nil {
		return "no lease exists"
	}

	if IsLeaseExpired(lease, durationSeconds) {
		return "lease expired"
	}

	if lease.Spec.HolderIdentity == nil || *lease.Spec.HolderIdentity == "" {
		return "lease has no holder identity"
	}

	// Check if current holder is still ready
	currentHolder := *lease.Spec.HolderIdentity
	for _, pod := range readyPods {
		if pod.Name == currentHolder {
			return "current holder still ready (should not elect new leader)"
		}
	}

	return fmt.Sprintf("current holder '%s' is not ready", currentHolder)
}

// getPodNames extracts pod names from a slice of pods for logging
func getPodNames(pods []corev1.Pod) []string {
	names := make([]string, len(pods))
	for i, pod := range pods {
		names[i] = pod.Name
	}
	return names
}

// getStringValue safely gets the value of a string pointer
func getStringValue(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// Pod Labeling Functions

// labelPodAsWriter labels a pod with the writer role
func (r *StatefulSetLockReconciler) labelPodAsWriter(ctx context.Context, pod *corev1.Pod) error {
	return r.labelPodWithRole(ctx, pod, PodRoleWriter)
}

// labelPodAsReader labels a pod with the reader role
func (r *StatefulSetLockReconciler) labelPodAsReader(ctx context.Context, pod *corev1.Pod) error {
	return r.labelPodWithRole(ctx, pod, PodRoleReader)
}

// labelPodWithRole labels a pod with the specified role
func (r *StatefulSetLockReconciler) labelPodWithRole(ctx context.Context, pod *corev1.Pod, role string) error {
	ctx, span := tracing.StartSpan(ctx, "labelPodWithRole",
		tracing.NamespaceAttr(pod.Namespace),
		tracing.PodNameAttr(pod.Name),
	)
	defer span.End()

	logger := logging.LoggerFromContext(ctx)

	// Check if the pod already has the correct label
	if currentRole, exists := pod.Labels[PodRoleLabel]; exists && currentRole == role {
		logging.Verbose(logging.WithPod(logger, pod.Name), "Pod already has correct role label", "role", role)
		tracing.SetSpanStatus(span, codes.Ok, "Pod already has correct label")
		return nil
	}

	// Create a copy of the pod to modify
	podCopy := pod.DeepCopy()

	// Initialize labels map if it doesn't exist
	if podCopy.Labels == nil {
		podCopy.Labels = make(map[string]string)
	}

	// Set the role label
	podCopy.Labels[PodRoleLabel] = role

	// Update the pod
	logging.LogPodOperation(logger, "Updating pod role label", pod.Name, role)
	if err := r.Patch(ctx, podCopy, client.MergeFrom(pod)); err != nil {
		tracing.RecordError(span, err)
		tracing.SetSpanStatus(span, codes.Error, "Failed to label pod")
		return fmt.Errorf("failed to label pod %s with role %s: %w", pod.Name, role, err)
	}

	logging.LogPodOperation(logger, "Successfully labeled pod", pod.Name, role)
	tracing.SetSpanStatus(span, codes.Ok, "Pod labeled successfully")
	return nil
}

// updatePodLabels updates all pod labels based on the current leader
func (r *StatefulSetLockReconciler) updatePodLabels(ctx context.Context, allPods []corev1.Pod, leaderPodName string) error {
	ctx, span := tracing.StartSpan(ctx, "updatePodLabels",
		tracing.LeaderPodAttr(leaderPodName),
		tracing.TotalPodsAttr(len(allPods)),
	)
	defer span.End()

	logger := logging.LoggerFromContext(ctx)
	start := time.Now()

	logging.Info(
		logging.WithLeader(logger, leaderPodName),
		"Updating pod labels",
		"total_pods", len(allPods),
	)

	var labelingErrors []error
	readyPodsLabeled := 0

	for i := range allPods {
		pod := &allPods[i]

		// Skip pods that are not ready - we don't want to label unhealthy pods
		if !IsPodReady(*pod) {
			logging.Verbose(logging.WithPod(logger, pod.Name), "Skipping labeling for non-ready pod")
			continue
		}

		readyPodsLabeled++
		var err error
		if pod.Name == leaderPodName {
			// Label as writer
			err = r.labelPodAsWriter(ctx, pod)
		} else {
			// Label as reader
			err = r.labelPodAsReader(ctx, pod)
		}

		if err != nil {
			logging.Error(logging.WithPod(logger, pod.Name), err, "Failed to update pod label")
			labelingErrors = append(labelingErrors, err)
		}
	}

	duration := time.Since(start)

	// Return combined errors if any occurred
	if len(labelingErrors) > 0 {
		var errorMessages []string
		for _, err := range labelingErrors {
			errorMessages = append(errorMessages, err.Error())
		}
		logging.Error(
			logging.WithDuration(logging.WithLeader(logger, leaderPodName), duration),
			nil,
			"Failed to update some pod labels",
			"error_count", len(labelingErrors),
			"ready_pods_processed", readyPodsLabeled,
		)
		tracing.SetSpanStatus(span, codes.Error, fmt.Sprintf("Failed to label %d pods", len(labelingErrors)))
		return fmt.Errorf("failed to update some pod labels: %v", errorMessages)
	}

	logging.Info(
		logging.WithDuration(logging.WithLeader(logger, leaderPodName), duration),
		"Successfully updated all pod labels",
		"ready_pods_labeled", readyPodsLabeled,
	)
	tracing.AddSpanAttributes(span, tracing.ReadyPodsAttr(readyPodsLabeled))
	tracing.SetSpanStatus(span, codes.Ok, "All pod labels updated successfully")
	return nil
}

// removePodRoleLabels removes role labels from all pods (used during cleanup)
func (r *StatefulSetLockReconciler) removePodRoleLabels(ctx context.Context, allPods []corev1.Pod) error {
	ctx, span := tracing.StartSpan(ctx, "removePodRoleLabels",
		tracing.TotalPodsAttr(len(allPods)),
	)
	defer span.End()

	logger := logging.LoggerFromContext(ctx)
	start := time.Now()

	logging.Info(logger, "Removing role labels from all pods", "total_pods", len(allPods))

	var labelingErrors []error
	podsProcessed := 0

	for i := range allPods {
		pod := &allPods[i]

		// Check if the pod has the role label
		if _, exists := pod.Labels[PodRoleLabel]; !exists {
			logging.Verbose(logging.WithPod(logger, pod.Name), "Pod does not have role label, skipping")
			continue
		}

		podsProcessed++

		// Create a copy of the pod to modify
		podCopy := pod.DeepCopy()

		// Remove the role label
		delete(podCopy.Labels, PodRoleLabel)

		// Update the pod
		logging.LogPodOperation(logger, "Removing role label from pod", pod.Name, "")
		if err := r.Patch(ctx, podCopy, client.MergeFrom(pod)); err != nil {
			logging.Error(logging.WithPod(logger, pod.Name), err, "Failed to remove role label from pod")
			labelingErrors = append(labelingErrors, err)
		} else {
			logging.LogPodOperation(logger, "Successfully removed role label from pod", pod.Name, "")
		}
	}

	duration := time.Since(start)

	// Return combined errors if any occurred
	if len(labelingErrors) > 0 {
		var errorMessages []string
		for _, err := range labelingErrors {
			errorMessages = append(errorMessages, err.Error())
		}
		logging.Error(
			logging.WithDuration(logger, duration),
			nil,
			"Failed to remove role labels from some pods",
			"error_count", len(labelingErrors),
			"pods_processed", podsProcessed,
		)
		tracing.SetSpanStatus(span, codes.Error, fmt.Sprintf("Failed to remove labels from %d pods", len(labelingErrors)))
		return fmt.Errorf("failed to remove role labels from some pods: %v", errorMessages)
	}

	logging.Info(
		logging.WithDuration(logger, duration),
		"Successfully removed role labels from all pods",
		"pods_processed", podsProcessed,
	)
	tracing.AddSpanAttributes(span, tracing.TotalPodsAttr(podsProcessed))
	tracing.SetSpanStatus(span, codes.Ok, "All role labels removed successfully")
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *StatefulSetLockReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&appv1.StatefulSetLock{}).
		Watches(
			&corev1.Pod{},
			handler.EnqueueRequestsFromMapFunc(r.findStatefulSetLocksForPod),
		).
		Watches(
			&coordinationv1.Lease{},
			handler.EnqueueRequestsFromMapFunc(r.findStatefulSetLocksForLease),
		).
		Complete(r)
}

// findStatefulSetLocksForPod finds StatefulSetLock resources that should be reconciled
// when a Pod changes. This enables quick reaction to pod state changes.
func (r *StatefulSetLockReconciler) findStatefulSetLocksForPod(ctx context.Context, pod client.Object) []reconcile.Request {
	logger := log.FromContext(ctx)

	// List all StatefulSetLock resources in the same namespace as the pod
	var statefulSetLockList appv1.StatefulSetLockList
	if err := r.List(ctx, &statefulSetLockList, client.InNamespace(pod.GetNamespace())); err != nil {
		logger.Error(err, "Failed to list StatefulSetLock resources for pod watch", "pod", pod.GetName())
		return nil
	}

	var requests []reconcile.Request

	// Check if this pod belongs to any StatefulSet that has a corresponding StatefulSetLock
	for _, ssl := range statefulSetLockList.Items {
		// Check if the pod belongs to the StatefulSet referenced by this StatefulSetLock
		if r.isPodOwnedByStatefulSet(ctx, pod, ssl.Spec.StatefulSetName) {
			logger.V(1).Info("Pod change triggers StatefulSetLock reconciliation",
				"pod", pod.GetName(),
				"statefulSetLock", ssl.Name,
				"statefulSet", ssl.Spec.StatefulSetName)

			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: ssl.Namespace,
					Name:      ssl.Name,
				},
			})
		}
	}

	return requests
}

// findStatefulSetLocksForLease finds StatefulSetLock resources that should be reconciled
// when a Lease changes. This enables quick reaction to external lease modifications.
func (r *StatefulSetLockReconciler) findStatefulSetLocksForLease(ctx context.Context, lease client.Object) []reconcile.Request {
	logger := log.FromContext(ctx)

	// List all StatefulSetLock resources in the same namespace as the lease
	var statefulSetLockList appv1.StatefulSetLockList
	if err := r.List(ctx, &statefulSetLockList, client.InNamespace(lease.GetNamespace())); err != nil {
		logger.Error(err, "Failed to list StatefulSetLock resources for lease watch", "lease", lease.GetName())
		return nil
	}

	var requests []reconcile.Request

	// Check if this lease is referenced by any StatefulSetLock
	for _, ssl := range statefulSetLockList.Items {
		if ssl.Spec.LeaseName == lease.GetName() {
			logger.V(1).Info("Lease change triggers StatefulSetLock reconciliation",
				"lease", lease.GetName(),
				"statefulSetLock", ssl.Name)

			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: ssl.Namespace,
					Name:      ssl.Name,
				},
			})
		}
	}

	return requests
}

// isPodOwnedByStatefulSet checks if a pod is owned by the specified StatefulSet
func (r *StatefulSetLockReconciler) isPodOwnedByStatefulSet(ctx context.Context, pod client.Object, statefulSetName string) bool {
	// Check owner references for StatefulSet ownership first
	for _, ownerRef := range pod.GetOwnerReferences() {
		if ownerRef.Kind == "StatefulSet" {
			// If it's owned by a StatefulSet, only return true if it's the right one
			return ownerRef.Name == statefulSetName
		}
	}

	// If no StatefulSet owner reference, check naming convention as fallback
	// StatefulSet pods follow the pattern: <statefulset-name>-<ordinal>
	podName := pod.GetName()
	expectedPrefix := statefulSetName + "-"
	if strings.HasPrefix(podName, expectedPrefix) {
		// Extract the suffix after the prefix
		suffix := podName[len(expectedPrefix):]
		// Check if the suffix is a valid ordinal (numeric)
		if _, err := strconv.Atoi(suffix); err == nil {
			return true
		}
	}

	return false
}
