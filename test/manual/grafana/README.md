# Grafana Dashboard for StatefulSet Leader Election Operator

This folder contains a ready-to-import Grafana dashboard for visualizing metrics exposed by the StatefulSet Leader Election Operator.

Files:
- statefulsetlock-dashboard.json — the dashboard definition (import into Grafana)

## Prerequisites
- The operator is deployed and exposing Prometheus metrics (default endpoint: :8080/metrics).
- Prometheus is scraping the operator metrics. If using Prometheus Operator / kube-prometheus-stack, ensure the ServiceMonitor CRD is installed and apply the ServiceMonitor included in this repo (see config/manager/service_monitor.yaml or config/prometheus/monitor.yaml).
- Grafana has a Prometheus data source configured.

## Importing the Dashboard
1. Open Grafana → Dashboards → New → Import.
2. Upload the file test/manual/grafana/statefulsetlock-dashboard.json or paste its contents.
3. Select your Prometheus data source for the variable DS_PROMETHEUS.
4. Click Import.

## Dashboard Variables
- namespace: Kubernetes namespace(s) to filter by (supports All = .* regex and multi-select).
- statefulsetlock: Name(s) of StatefulSetLock resources (supports All and multi-select).
- statefulset: Target StatefulSet name(s) (supports All and multi-select).
- lease_name: Lease resource name(s) for renewal/expiration panels (supports All and multi-select).

These variables drive filtering in all panels using PromQL regex matchers (=~). Selecting "All" expands to ".*".

## Panels Included
- Leader Elections Rate (by reason) and total
- Leader Election Duration (p50/p95/p99)
- Lease Renewals Rate and Duration (p50/p95/p99)
- Reconciliation Errors Rate (by error_type) and total
- Reconciliation Duration (p50/p95/p99)
- Current Leader (instant table)
- Ready Pods (gauge)
- Lease Expiration Seconds (threshold-colored time series)
- Pod Labeling Errors Rate

Key Prometheus metric names used by the dashboard:
- statefulsetlock_leader_elections_total
- statefulsetlock_leader_election_duration_seconds_bucket/_sum/_count
- statefulsetlock_lease_renewals_total
- statefulsetlock_lease_renewal_duration_seconds_bucket/_sum/_count
- statefulsetlock_reconciliation_errors_total
- statefulsetlock_reconciliation_duration_seconds_bucket/_sum/_count
- statefulsetlock_current_leader_info
- statefulsetlock_ready_pods
- statefulsetlock_lease_expiration_seconds
- statefulsetlock_pod_labeling_errors_total

## Verify Prometheus Scraping
If panels are empty, verify Prometheus can see the metrics:
- Ensure the operator metrics Service exists (config/default/metrics_service.yaml).
- Ensure a ServiceMonitor or scrape job is configured (config/manager/service_monitor.yaml or config/prometheus/monitor.yaml).
- Example quick check in Prometheus UI:
  - rate(statefulsetlock_reconciliation_errors_total[5m])
  - histogram_quantile(0.95, sum by (le) (rate(statefulsetlock_reconciliation_duration_seconds_bucket[5m])))

## Notes
- The Ready Pods gauge default max is 10; adjust in the panel if your StatefulSets have larger replica counts.
- The Lease Expiration panel displays negative values if a lease is expired; thresholds are set to visually highlight imminent expiry.

