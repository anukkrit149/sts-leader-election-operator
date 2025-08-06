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

package tracing

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	// TracerName is the name of the tracer used by the operator
	TracerName = "statefulset-leader-election-operator"
)

var (
	// tracer is the global tracer instance
	tracer = otel.Tracer(TracerName)
)

// StartSpan starts a new span with the given name and attributes
func StartSpan(ctx context.Context, spanName string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	return tracer.Start(ctx, spanName, trace.WithAttributes(attrs...))
}

// AddSpanAttributes adds attributes to the current span
func AddSpanAttributes(span trace.Span, attrs ...attribute.KeyValue) {
	span.SetAttributes(attrs...)
}

// RecordError records an error in the current span
func RecordError(span trace.Span, err error) {
	span.RecordError(err)
}

// SetSpanStatus sets the status of the current span
func SetSpanStatus(span trace.Span, code codes.Code, description string) {
	span.SetStatus(code, description)
}

// Common attribute keys for consistent tracing
const (
	AttrNamespace       = "k8s.namespace"
	AttrStatefulSetLock = "statefulsetlock.name"
	AttrStatefulSet     = "statefulset.name"
	AttrPodName         = "pod.name"
	AttrLeaseName       = "lease.name"
	AttrLeaderPod       = "leader.pod"
	AttrElectionReason  = "election.reason"
	AttrErrorType       = "error.type"
	AttrReadyPods       = "pods.ready_count"
	AttrTotalPods       = "pods.total_count"
	AttrLeaseDuration   = "lease.duration_seconds"
	AttrLeaseExpired    = "lease.expired"
)

// Helper functions for creating common attributes
func NamespaceAttr(namespace string) attribute.KeyValue {
	return attribute.String(AttrNamespace, namespace)
}

func StatefulSetLockAttr(name string) attribute.KeyValue {
	return attribute.String(AttrStatefulSetLock, name)
}

func StatefulSetAttr(name string) attribute.KeyValue {
	return attribute.String(AttrStatefulSet, name)
}

func PodNameAttr(name string) attribute.KeyValue {
	return attribute.String(AttrPodName, name)
}

func LeaseNameAttr(name string) attribute.KeyValue {
	return attribute.String(AttrLeaseName, name)
}

func LeaderPodAttr(name string) attribute.KeyValue {
	return attribute.String(AttrLeaderPod, name)
}

func ElectionReasonAttr(reason string) attribute.KeyValue {
	return attribute.String(AttrElectionReason, reason)
}

func ErrorTypeAttr(errorType string) attribute.KeyValue {
	return attribute.String(AttrErrorType, errorType)
}

func ReadyPodsAttr(count int) attribute.KeyValue {
	return attribute.Int(AttrReadyPods, count)
}

func TotalPodsAttr(count int) attribute.KeyValue {
	return attribute.Int(AttrTotalPods, count)
}

func LeaseDurationAttr(seconds int32) attribute.KeyValue {
	return attribute.Int(AttrLeaseDuration, int(seconds))
}

func LeaseExpiredAttr(expired bool) attribute.KeyValue {
	return attribute.Bool(AttrLeaseExpired, expired)
}