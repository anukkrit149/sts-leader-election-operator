# Changelog

All notable changes to the StatefulSet Leader Election Operator will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0] - 2025-01-08

### Added
- Initial release of StatefulSet Leader Election Operator
- Custom Resource Definition (CRD) for StatefulSetLock
- Automatic leader election for StatefulSet pods
- Pod labeling with `sts-role: writer` and `sts-role: reader`
- Lease-based leader election with configurable duration
- Comprehensive status reporting with conditions
- RBAC configuration for secure operation
- Integration tests for happy path and failover scenarios
- End-to-end validation tests
- Comprehensive documentation and examples
- Observability features (logging, metrics, tracing)
- Production-ready deployment configuration
- Container image build and release automation

### Features
- Supports Kubernetes 1.31+
- Built with operator-sdk following best practices
- Automatic failover when leader pod becomes unavailable
- Configurable lease duration for election timing
- Structured logging with multiple log levels
- Prometheus metrics for monitoring
- OpenTelemetry tracing support
- Finalizer-based cleanup for resource management

[Unreleased]: https://github.com/anukkrit/statefulset-leader-election-operator/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/anukkrit/statefulset-leader-election-operator/releases/tag/v0.1.0