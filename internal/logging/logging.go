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

package logging

import (
	"context"
	"time"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// LoggerFromContext returns a logger from the context with common fields
func LoggerFromContext(ctx context.Context) logr.Logger {
	return log.FromContext(ctx)
}

// WithStatefulSetLock adds StatefulSetLock-specific fields to the logger
func WithStatefulSetLock(logger logr.Logger, namespace, name string) logr.Logger {
	return logger.WithValues(
		"statefulsetlock", name,
		"namespace", namespace,
	)
}

// WithStatefulSet adds StatefulSet-specific fields to the logger
func WithStatefulSet(logger logr.Logger, name string) logr.Logger {
	return logger.WithValues("statefulset", name)
}

// WithLease adds lease-specific fields to the logger
func WithLease(logger logr.Logger, name string) logr.Logger {
	return logger.WithValues("lease", name)
}

// WithPod adds pod-specific fields to the logger
func WithPod(logger logr.Logger, name string) logr.Logger {
	return logger.WithValues("pod", name)
}

// WithLeader adds leader-specific fields to the logger
func WithLeader(logger logr.Logger, leaderPod string) logr.Logger {
	return logger.WithValues("leader", leaderPod)
}

// WithElectionReason adds election reason to the logger
func WithElectionReason(logger logr.Logger, reason string) logr.Logger {
	return logger.WithValues("election_reason", reason)
}

// WithDuration adds duration to the logger
func WithDuration(logger logr.Logger, duration time.Duration) logr.Logger {
	return logger.WithValues("duration", duration.String())
}

// WithError adds error information to the logger
func WithError(logger logr.Logger, err error) logr.Logger {
	return logger.WithValues("error", err.Error())
}

// WithPodCounts adds pod count information to the logger
func WithPodCounts(logger logr.Logger, total, ready int) logr.Logger {
	return logger.WithValues(
		"total_pods", total,
		"ready_pods", ready,
	)
}

// WithLeaseInfo adds lease information to the logger
func WithLeaseInfo(logger logr.Logger, holder string, expired bool, durationSeconds int32) logr.Logger {
	return logger.WithValues(
		"lease_holder", holder,
		"lease_expired", expired,
		"lease_duration_seconds", durationSeconds,
	)
}

// LogLevel constants for consistent logging levels
const (
	// DebugLevel for detailed debugging information
	DebugLevel = 1
	// VerboseLevel for verbose operational information
	VerboseLevel = 2
)

// Debug logs at debug level (V(1))
func Debug(logger logr.Logger, msg string, keysAndValues ...interface{}) {
	logger.V(DebugLevel).Info(msg, keysAndValues...)
}

// Verbose logs at verbose level (V(2))
func Verbose(logger logr.Logger, msg string, keysAndValues ...interface{}) {
	logger.V(VerboseLevel).Info(msg, keysAndValues...)
}

// Info logs at info level
func Info(logger logr.Logger, msg string, keysAndValues ...interface{}) {
	logger.Info(msg, keysAndValues...)
}

// Error logs at error level
func Error(logger logr.Logger, err error, msg string, keysAndValues ...interface{}) {
	logger.Error(err, msg, keysAndValues...)
}

// LogReconciliationStart logs the start of a reconciliation
func LogReconciliationStart(logger logr.Logger, namespace, name string) {
	Info(WithStatefulSetLock(logger, namespace, name), "Starting reconciliation")
}

// LogReconciliationEnd logs the end of a reconciliation
func LogReconciliationEnd(logger logr.Logger, namespace, name string, duration time.Duration, err error) {
	loggerWithFields := WithStatefulSetLock(WithDuration(logger, duration), namespace, name)
	if err != nil {
		Error(WithError(loggerWithFields, err), err, "Reconciliation failed")
	} else {
		Info(loggerWithFields, "Reconciliation completed successfully")
	}
}

// LogLeaderElection logs leader election events
func LogLeaderElection(logger logr.Logger, namespace, statefulsetlock, newLeader, reason string) {
	Info(
		WithLeader(WithElectionReason(WithStatefulSetLock(logger, namespace, statefulsetlock), reason), newLeader),
		"Leader election completed",
	)
}

// LogLeaseOperation logs lease operations
func LogLeaseOperation(logger logr.Logger, operation, leaseName, holder string, duration time.Duration) {
	Info(
		WithDuration(WithLease(WithLeader(logger, holder), leaseName), duration),
		operation,
	)
}

// LogPodOperation logs pod operations
func LogPodOperation(logger logr.Logger, operation, podName, role string) {
	Info(
		WithPod(logger, podName),
		operation,
		"role", role,
	)
}

// LogValidationError logs validation errors
func LogValidationError(logger logr.Logger, field, message string) {
	Error(
		logger,
		nil,
		"Validation error",
		"field", field,
		"message", message,
	)
}

// LogResourceFetch logs resource fetching operations
func LogResourceFetch(logger logr.Logger, resourceType, name string, found bool, duration time.Duration) {
	loggerWithFields := WithDuration(logger, duration)
	if found {
		Debug(loggerWithFields, "Resource fetched successfully", "type", resourceType, "name", name)
	} else {
		Debug(loggerWithFields, "Resource not found", "type", resourceType, "name", name)
	}
}
