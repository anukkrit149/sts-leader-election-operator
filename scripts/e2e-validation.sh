#!/bin/bash

# End-to-End Validation Script for StatefulSet Leader Election Operator
# This script performs comprehensive testing of the operator functionality

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
NAMESPACE="e2e-validation"
OPERATOR_NAMESPACE="sts-leader-elect-operator-system"
IMAGE_NAME="sts-leader-elect-operator:e2e-test"
TIMEOUT=300 # 5 minutes

# Logging functions
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Cleanup function
cleanup() {
    log_info "Cleaning up test resources..."
    kubectl delete namespace $NAMESPACE --ignore-not-found=true
    kubectl delete namespace $OPERATOR_NAMESPACE --ignore-not-found=true
    log_info "Cleanup completed"
}

# Trap to ensure cleanup on exit
trap cleanup EXIT

# Check prerequisites
check_prerequisites() {
    log_info "Checking prerequisites..."
    
    # Check if kubectl is available
    if ! command -v kubectl &> /dev/null; then
        log_error "kubectl is not installed or not in PATH"
        exit 1
    fi
    
    # Check if kind is available (for local testing)
    if ! command -v kind &> /dev/null; then
        log_warning "kind is not installed. Assuming running against existing cluster."
    fi
    
    # Check if make is available
    if ! command -v make &> /dev/null; then
        log_error "make is not installed or not in PATH"
        exit 1
    fi
    
    # Check cluster connectivity
    if ! kubectl cluster-info &> /dev/null; then
        log_error "Cannot connect to Kubernetes cluster"
        exit 1
    fi
    
    log_success "Prerequisites check passed"
}

# Build and deploy operator
deploy_operator() {
    log_info "Building and deploying operator..."
    
    # Build the operator image
    log_info "Building operator image..."
    make docker-build IMG=$IMAGE_NAME
    
    # Load image to kind if available
    if command -v kind &> /dev/null && kind get clusters | grep -q "kind"; then
        log_info "Loading image to kind cluster..."
        kind load docker-image $IMAGE_NAME
    fi
    
    # Install CRDs
    log_info "Installing CRDs..."
    make install
    
    # Deploy operator
    log_info "Deploying operator..."
    make deploy IMG=$IMAGE_NAME
    
    # Wait for operator to be ready
    log_info "Waiting for operator to be ready..."
    kubectl wait --for=condition=available --timeout=${TIMEOUT}s deployment/sts-leader-elect-operator-controller-manager -n $OPERATOR_NAMESPACE
    
    log_success "Operator deployed successfully"
}

# Create test namespace
create_test_namespace() {
    log_info "Creating test namespace..."
    kubectl create namespace $NAMESPACE --dry-run=client -o yaml | kubectl apply -f -
    log_success "Test namespace created"
}

# Test basic functionality
test_basic_functionality() {
    log_info "Testing basic functionality..."
    
    # Create test StatefulSet
    log_info "Creating test StatefulSet..."
    cat <<EOF | kubectl apply -n $NAMESPACE -f -
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: test-basic
  labels:
    app: test-basic
spec:
  serviceName: test-basic-headless
  replicas: 3
  selector:
    matchLabels:
      app: test-basic
  template:
    metadata:
      labels:
        app: test-basic
    spec:
      containers:
      - name: nginx
        image: nginx:alpine
        ports:
        - containerPort: 80
        readinessProbe:
          httpGet:
            path: /
            port: 80
          initialDelaySeconds: 5
          periodSeconds: 5
---
apiVersion: v1
kind: Service
metadata:
  name: test-basic-headless
spec:
  clusterIP: None
  selector:
    app: test-basic
  ports:
  - port: 80
EOF
    
    # Wait for pods to be ready
    log_info "Waiting for StatefulSet pods to be ready..."
    kubectl wait --for=condition=ready --timeout=${TIMEOUT}s pod -l app=test-basic -n $NAMESPACE
    
    # Create StatefulSetLock
    log_info "Creating StatefulSetLock..."
    cat <<EOF | kubectl apply -n $NAMESPACE -f -
apiVersion: app.anukkrit.me/v1
kind: StatefulSetLock
metadata:
  name: test-basic-lock
spec:
  statefulSetName: test-basic
  leaseName: test-basic-lease
  leaseDurationSeconds: 30
EOF
    
    # Wait for leader election
    log_info "Waiting for leader election..."
    timeout=60
    while [ $timeout -gt 0 ]; do
        if kubectl get lease test-basic-lease -n $NAMESPACE &> /dev/null; then
            break
        fi
        sleep 2
        timeout=$((timeout - 2))
    done
    
    if [ $timeout -le 0 ]; then
        log_error "Leader election did not occur within timeout"
        return 1
    fi
    
    # Verify pod labels
    log_info "Verifying pod labels..."
    writer_count=$(kubectl get pods -l app=test-basic,sts-role=writer -n $NAMESPACE --no-headers | wc -l)
    reader_count=$(kubectl get pods -l app=test-basic,sts-role=reader -n $NAMESPACE --no-headers | wc -l)
    
    if [ "$writer_count" -ne 1 ]; then
        log_error "Expected 1 writer pod, found $writer_count"
        return 1
    fi
    
    if [ "$reader_count" -ne 2 ]; then
        log_error "Expected 2 reader pods, found $reader_count"
        return 1
    fi
    
    # Verify StatefulSetLock status
    log_info "Verifying StatefulSetLock status..."
    writer_pod=$(kubectl get statefulsetlock test-basic-lock -n $NAMESPACE -o jsonpath='{.status.writerPod}')
    if [ -z "$writer_pod" ]; then
        log_error "StatefulSetLock status does not show writer pod"
        return 1
    fi
    
    log_success "Basic functionality test passed"
}

# Test leader failover
test_leader_failover() {
    log_info "Testing leader failover..."
    
    # Get current leader
    current_leader=$(kubectl get pods -l app=test-basic,sts-role=writer -n $NAMESPACE -o jsonpath='{.items[0].metadata.name}')
    log_info "Current leader: $current_leader"
    
    # Simulate leader failure by deleting the pod
    log_info "Simulating leader failure..."
    kubectl delete pod $current_leader -n $NAMESPACE
    
    # Wait for new leader election
    log_info "Waiting for new leader election..."
    timeout=120
    while [ $timeout -gt 0 ]; do
        new_leader=$(kubectl get pods -l app=test-basic,sts-role=writer -n $NAMESPACE -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")
        if [ -n "$new_leader" ] && [ "$new_leader" != "$current_leader" ]; then
            log_info "New leader elected: $new_leader"
            break
        fi
        sleep 2
        timeout=$((timeout - 2))
    done
    
    if [ $timeout -le 0 ]; then
        log_error "New leader was not elected within timeout"
        return 1
    fi
    
    # Verify pod labels after failover
    writer_count=$(kubectl get pods -l app=test-basic,sts-role=writer -n $NAMESPACE --no-headers | wc -l)
    if [ "$writer_count" -ne 1 ]; then
        log_error "Expected 1 writer pod after failover, found $writer_count"
        return 1
    fi
    
    log_success "Leader failover test passed"
}

# Test resource cleanup
test_resource_cleanup() {
    log_info "Testing resource cleanup..."
    
    # Verify lease exists
    if ! kubectl get lease test-basic-lease -n $NAMESPACE &> /dev/null; then
        log_error "Lease does not exist before cleanup test"
        return 1
    fi
    
    # Delete StatefulSetLock
    log_info "Deleting StatefulSetLock..."
    kubectl delete statefulsetlock test-basic-lock -n $NAMESPACE
    
    # Wait for lease to be cleaned up
    log_info "Waiting for lease cleanup..."
    timeout=60
    while [ $timeout -gt 0 ]; do
        if ! kubectl get lease test-basic-lease -n $NAMESPACE &> /dev/null; then
            break
        fi
        sleep 2
        timeout=$((timeout - 2))
    done
    
    if [ $timeout -le 0 ]; then
        log_error "Lease was not cleaned up within timeout"
        return 1
    fi
    
    log_success "Resource cleanup test passed"
}

# Test multiple StatefulSets
test_multiple_statefulsets() {
    log_info "Testing multiple StatefulSets..."
    
    # Create multiple StatefulSets
    for i in {1..3}; do
        log_info "Creating StatefulSet test-multi-$i..."
        cat <<EOF | kubectl apply -n $NAMESPACE -f -
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: test-multi-$i
  labels:
    app: test-multi-$i
spec:
  serviceName: test-multi-$i-headless
  replicas: 2
  selector:
    matchLabels:
      app: test-multi-$i
  template:
    metadata:
      labels:
        app: test-multi-$i
    spec:
      containers:
      - name: nginx
        image: nginx:alpine
        ports:
        - containerPort: 80
        readinessProbe:
          httpGet:
            path: /
            port: 80
          initialDelaySeconds: 5
          periodSeconds: 5
---
apiVersion: v1
kind: Service
metadata:
  name: test-multi-$i-headless
spec:
  clusterIP: None
  selector:
    app: test-multi-$i
  ports:
  - port: 80
---
apiVersion: app.anukkrit.me/v1
kind: StatefulSetLock
metadata:
  name: test-multi-$i-lock
spec:
  statefulSetName: test-multi-$i
  leaseName: test-multi-$i-lease
  leaseDurationSeconds: 30
EOF
    done
    
    # Wait for all pods to be ready
    for i in {1..3}; do
        log_info "Waiting for StatefulSet test-multi-$i pods to be ready..."
        kubectl wait --for=condition=ready --timeout=${TIMEOUT}s pod -l app=test-multi-$i -n $NAMESPACE
    done
    
    # Verify leader election for all StatefulSets
    for i in {1..3}; do
        log_info "Verifying leader election for test-multi-$i..."
        timeout=60
        while [ $timeout -gt 0 ]; do
            if kubectl get lease test-multi-$i-lease -n $NAMESPACE &> /dev/null; then
                break
            fi
            sleep 2
            timeout=$((timeout - 2))
        done
        
        if [ $timeout -le 0 ]; then
            log_error "Leader election did not occur for test-multi-$i within timeout"
            return 1
        fi
        
        # Verify pod labels
        writer_count=$(kubectl get pods -l app=test-multi-$i,sts-role=writer -n $NAMESPACE --no-headers | wc -l)
        reader_count=$(kubectl get pods -l app=test-multi-$i,sts-role=reader -n $NAMESPACE --no-headers | wc -l)
        
        if [ "$writer_count" -ne 1 ] || [ "$reader_count" -ne 1 ]; then
            log_error "Incorrect pod labels for test-multi-$i: writer=$writer_count, reader=$reader_count"
            return 1
        fi
    done
    
    log_success "Multiple StatefulSets test passed"
}

# Test edge cases
test_edge_cases() {
    log_info "Testing edge cases..."
    
    # Test StatefulSet with no ready pods
    log_info "Testing StatefulSet with no ready pods..."
    cat <<EOF | kubectl apply -n $NAMESPACE -f -
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: test-failing
  labels:
    app: test-failing
spec:
  serviceName: test-failing-headless
  replicas: 2
  selector:
    matchLabels:
      app: test-failing
  template:
    metadata:
      labels:
        app: test-failing
    spec:
      containers:
      - name: failing-container
        image: nginx:alpine
        ports:
        - containerPort: 80
        readinessProbe:
          exec:
            command: ["false"]
          initialDelaySeconds: 1
          periodSeconds: 1
---
apiVersion: v1
kind: Service
metadata:
  name: test-failing-headless
spec:
  clusterIP: None
  selector:
    app: test-failing
  ports:
  - port: 80
---
apiVersion: app.anukkrit.me/v1
kind: StatefulSetLock
metadata:
  name: test-failing-lock
spec:
  statefulSetName: test-failing
  leaseName: test-failing-lease
  leaseDurationSeconds: 30
EOF
    
    # Wait a bit and verify no lease is created
    sleep 30
    if kubectl get lease test-failing-lease -n $NAMESPACE &> /dev/null; then
        log_error "Lease should not be created when no pods are ready"
        return 1
    fi
    
    log_success "Edge cases test passed"
}

# Run performance test
test_performance() {
    log_info "Testing performance with larger StatefulSet..."
    
    # Create StatefulSet with more replicas
    cat <<EOF | kubectl apply -n $NAMESPACE -f -
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: test-perf
  labels:
    app: test-perf
spec:
  serviceName: test-perf-headless
  replicas: 8
  selector:
    matchLabels:
      app: test-perf
  template:
    metadata:
      labels:
        app: test-perf
    spec:
      containers:
      - name: nginx
        image: nginx:alpine
        ports:
        - containerPort: 80
        readinessProbe:
          httpGet:
            path: /
            port: 80
          initialDelaySeconds: 5
          periodSeconds: 5
---
apiVersion: v1
kind: Service
metadata:
  name: test-perf-headless
spec:
  clusterIP: None
  selector:
    app: test-perf
  ports:
  - port: 80
---
apiVersion: app.anukkrit.me/v1
kind: StatefulSetLock
metadata:
  name: test-perf-lock
spec:
  statefulSetName: test-perf
  leaseName: test-perf-lease
  leaseDurationSeconds: 10
EOF
    
    # Wait for pods to be ready
    log_info "Waiting for performance test pods to be ready..."
    kubectl wait --for=condition=ready --timeout=${TIMEOUT}s pod -l app=test-perf -n $NAMESPACE
    
    # Verify leader election
    timeout=60
    while [ $timeout -gt 0 ]; do
        if kubectl get lease test-perf-lease -n $NAMESPACE &> /dev/null; then
            break
        fi
        sleep 2
        timeout=$((timeout - 2))
    done
    
    if [ $timeout -le 0 ]; then
        log_error "Leader election did not occur for performance test within timeout"
        return 1
    fi
    
    # Verify pod labels
    writer_count=$(kubectl get pods -l app=test-perf,sts-role=writer -n $NAMESPACE --no-headers | wc -l)
    reader_count=$(kubectl get pods -l app=test-perf,sts-role=reader -n $NAMESPACE --no-headers | wc -l)
    
    if [ "$writer_count" -ne 1 ] || [ "$reader_count" -ne 7 ]; then
        log_error "Incorrect pod labels for performance test: writer=$writer_count, reader=$reader_count"
        return 1
    fi
    
    log_success "Performance test passed"
}

# Generate test report
generate_report() {
    log_info "Generating test report..."
    
    echo "========================================="
    echo "StatefulSet Leader Election Operator"
    echo "End-to-End Test Report"
    echo "========================================="
    echo "Date: $(date)"
    echo "Cluster: $(kubectl config current-context)"
    echo ""
    
    # Operator status
    echo "Operator Status:"
    kubectl get deployment sts-leader-elect-operator-controller-manager -n $OPERATOR_NAMESPACE -o wide
    echo ""
    
    # CRD status
    echo "CRD Status:"
    kubectl get crd statefulsetlocks.app.anukkrit.me
    echo ""
    
    # Test resources
    echo "Test Resources:"
    kubectl get statefulsets,pods,leases,statefulsetlocks -n $NAMESPACE
    echo ""
    
    # Operator logs (last 50 lines)
    echo "Operator Logs (last 50 lines):"
    kubectl logs -n $OPERATOR_NAMESPACE deployment/sts-leader-elect-operator-controller-manager --tail=50
    echo ""
    
    log_success "Test report generated"
}

# Main execution
main() {
    log_info "Starting StatefulSet Leader Election Operator E2E Validation"
    
    check_prerequisites
    create_test_namespace
    deploy_operator
    
    # Run all tests
    test_basic_functionality
    test_leader_failover
    test_resource_cleanup
    test_multiple_statefulsets
    test_edge_cases
    test_performance
    
    generate_report
    
    log_success "All E2E validation tests passed!"
}

# Run main function
main "$@"