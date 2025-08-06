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

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	// LeaderElectionsTotal tracks the total number of leader elections performed
	LeaderElectionsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "statefulsetlock_leader_elections_total",
			Help: "Total number of leader elections performed",
		},
		[]string{"namespace", "statefulsetlock", "statefulset", "reason"},
	)

	// LeaderElectionDuration tracks the time taken for leader election operations
	LeaderElectionDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "statefulsetlock_leader_election_duration_seconds",
			Help:    "Time taken for leader election operations",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"namespace", "statefulsetlock", "statefulset"},
	)

	// LeaseRenewalsTotal tracks the total number of lease renewals
	LeaseRenewalsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "statefulsetlock_lease_renewals_total",
			Help: "Total number of lease renewals performed",
		},
		[]string{"namespace", "statefulsetlock", "lease_name"},
	)

	// LeaseRenewalDuration tracks the time taken for lease renewal operations
	LeaseRenewalDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "statefulsetlock_lease_renewal_duration_seconds",
			Help:    "Time taken for lease renewal operations",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"namespace", "statefulsetlock", "lease_name"},
	)

	// ReconciliationErrorsTotal tracks the total number of reconciliation errors
	ReconciliationErrorsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "statefulsetlock_reconciliation_errors_total",
			Help: "Total number of reconciliation errors",
		},
		[]string{"namespace", "statefulsetlock", "error_type"},
	)

	// ReconciliationDuration tracks the time taken for reconciliation operations
	ReconciliationDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "statefulsetlock_reconciliation_duration_seconds",
			Help:    "Time taken for reconciliation operations",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"namespace", "statefulsetlock"},
	)

	// CurrentLeaderInfo provides information about the current leader
	CurrentLeaderInfo = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "statefulsetlock_current_leader_info",
			Help: "Information about the current leader (1 if leader exists, 0 otherwise)",
		},
		[]string{"namespace", "statefulsetlock", "statefulset", "leader_pod"},
	)

	// PodLabelingErrorsTotal tracks the total number of pod labeling errors
	PodLabelingErrorsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "statefulsetlock_pod_labeling_errors_total",
			Help: "Total number of pod labeling errors",
		},
		[]string{"namespace", "statefulsetlock", "pod_name"},
	)

	// ReadyPodsGauge tracks the number of ready pods in the StatefulSet
	ReadyPodsGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "statefulsetlock_ready_pods",
			Help: "Number of ready pods in the StatefulSet",
		},
		[]string{"namespace", "statefulsetlock", "statefulset"},
	)

	// LeaseExpirationGauge tracks when leases are about to expire (seconds until expiration)
	LeaseExpirationGauge = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "statefulsetlock_lease_expiration_seconds",
			Help: "Seconds until lease expiration (negative if expired)",
		},
		[]string{"namespace", "statefulsetlock", "lease_name"},
	)
)

// init registers all metrics with the controller-runtime metrics registry
func init() {
	metrics.Registry.MustRegister(
		LeaderElectionsTotal,
		LeaderElectionDuration,
		LeaseRenewalsTotal,
		LeaseRenewalDuration,
		ReconciliationErrorsTotal,
		ReconciliationDuration,
		CurrentLeaderInfo,
		PodLabelingErrorsTotal,
		ReadyPodsGauge,
		LeaseExpirationGauge,
	)
}

// RecordLeaderElection records a leader election event
func RecordLeaderElection(namespace, statefulsetlock, statefulset, reason string, duration float64) {
	LeaderElectionsTotal.WithLabelValues(namespace, statefulsetlock, statefulset, reason).Inc()
	LeaderElectionDuration.WithLabelValues(namespace, statefulsetlock, statefulset).Observe(duration)
}

// RecordLeaseRenewal records a lease renewal event
func RecordLeaseRenewal(namespace, statefulsetlock, leaseName string, duration float64) {
	LeaseRenewalsTotal.WithLabelValues(namespace, statefulsetlock, leaseName).Inc()
	LeaseRenewalDuration.WithLabelValues(namespace, statefulsetlock, leaseName).Observe(duration)
}

// RecordReconciliationError records a reconciliation error
func RecordReconciliationError(namespace, statefulsetlock, errorType string) {
	ReconciliationErrorsTotal.WithLabelValues(namespace, statefulsetlock, errorType).Inc()
}

// RecordReconciliationDuration records the duration of a reconciliation
func RecordReconciliationDuration(namespace, statefulsetlock string, duration float64) {
	ReconciliationDuration.WithLabelValues(namespace, statefulsetlock).Observe(duration)
}

// UpdateCurrentLeaderInfo updates the current leader information
func UpdateCurrentLeaderInfo(namespace, statefulsetlock, statefulset, leaderPod string) {
	// Clear previous leader metrics for this StatefulSetLock
	CurrentLeaderInfo.DeletePartialMatch(prometheus.Labels{
		"namespace":       namespace,
		"statefulsetlock": statefulsetlock,
		"statefulset":     statefulset,
	})

	// Set new leader metric
	if leaderPod != "" {
		CurrentLeaderInfo.WithLabelValues(namespace, statefulsetlock, statefulset, leaderPod).Set(1)
	}
}

// RecordPodLabelingError records a pod labeling error
func RecordPodLabelingError(namespace, statefulsetlock, podName string) {
	PodLabelingErrorsTotal.WithLabelValues(namespace, statefulsetlock, podName).Inc()
}

// UpdateReadyPodsCount updates the count of ready pods
func UpdateReadyPodsCount(namespace, statefulsetlock, statefulset string, count int) {
	ReadyPodsGauge.WithLabelValues(namespace, statefulsetlock, statefulset).Set(float64(count))
}

// UpdateLeaseExpiration updates the lease expiration time
func UpdateLeaseExpiration(namespace, statefulsetlock, leaseName string, secondsUntilExpiration float64) {
	LeaseExpirationGauge.WithLabelValues(namespace, statefulsetlock, leaseName).Set(secondsUntilExpiration)
}