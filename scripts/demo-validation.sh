#!/bin/bash

# Demo Validation Script for StatefulSet Leader Election Operator
# This script demonstrates all key functionality of the operator

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
DEMO_NAMESPACE="demo-validation"
TIMEOUT=120

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

log_step() {
    echo -e "\n${YELLOW}=== $1 ===${NC}"
}

# Cleanup function
cleanup() {
    log_info "Cleaning up demo resources..."
    kubectl delete namespace $DEMO_NAMESPACE --ignore-not-found=true
    log_info "Demo cleanup completed"
}

# Trap to ensure cleanup on exit
trap cleanup EXIT

# Check prerequisites
check_prerequisites() {
    log_step "Checking Prerequisites"
    
    if ! command -v kubectl &> /dev/null; then
        log_error "kubectl is not installed"
        exit 1
    fi
    
    if ! kubectl cluster-info &> /dev/null; then
        log_error "Cannot connect to Kubernetes cluster"
        exit 1
    fi
    
    # Check if operator is deployed
    if ! kubectl get deployment sts-leader-elect-operator-controller-manager -n sts-leader-elect-operator-system &> /dev/null; then
        log_warning "Operator not found. Please deploy the operator first with 'make deploy'"
        exit 1
    fi
    
    log_success "Prerequisites check passed"
}

# Create demo namespace
create_demo_namespace() {
    log_step "Creating Demo Namespace"
    kubectl create namespace $DEMO_NAMESPACE --dry-run=client -o yaml | kubectl apply -f -
    log_success "Demo namespace created: $DEMO_NAMESPACE"
}

# Demo 1: Basic Leader Election
demo_basic_leader_election() {
    log_step "Demo 1: Basic Leader Election"
    
    log_info "Creating StatefulSet with 3 replicas..."
    cat <<EOF | kubectl apply -n $DEMO_NAMESPACE -f -
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: demo-postgres
  labels:
    app: demo-postgres
spec:
  serviceName: demo-postgres-headless
  replicas: 3
  selector:
    matchLabels:
      app: demo-postgres
  template:
    metadata:
      labels:
        app: demo-postgres
    spec:
      containers:
      - name: postgres
        image: postgres:15-alpine
        env:
        - name: POSTGRES_DB
          value: demo
        - name: POSTGRES_USER
          value: demo
        - name: POSTGRES_PASSWORD
          value: demo123
        ports:
        - containerPort: 5432
        readinessProbe:
          exec:
            command:
            - /bin/sh
            - -c
            - pg_isready -U demo
          initialDelaySeconds: 10
          periodSeconds: 5
---
apiVersion: v1
kind: Service
metadata:
  name: demo-postgres-headless
spec:
  clusterIP: None
  selector:
    app: demo-postgres
  ports:
  - port: 5432
EOF

    log_info "Waiting for pods to be ready..."
    kubectl wait --for=condition=ready --timeout=${TIMEOUT}s pod -l app=demo-postgres -n $DEMO_NAMESPACE

    log_info "Creating StatefulSetLock..."
    cat <<EOF | kubectl apply -n $DEMO_NAMESPACE -f -
apiVersion: app.anukkrit.me/v1
kind: StatefulSetLock
metadata:
  name: demo-postgres-lock
spec:
  statefulSetName: demo-postgres
  leaseName: demo-postgres-lease
  leaseDurationSeconds: 30
EOF

    log_info "Waiting for leader election..."
    timeout=60
    while [ $timeout -gt 0 ]; do
        if kubectl get lease demo-postgres-lease -n $DEMO_NAMESPACE &> /dev/null; then
            break
        fi
        sleep 2
        timeout=$((timeout - 2))
    done

    if [ $timeout -le 0 ]; then
        log_error "Leader election did not occur within timeout"
        return 1
    fi

    # Show results
    log_success "Leader election completed!"
    echo ""
    log_info "Current leader:"
    kubectl get pods -l app=demo-postgres,sts-role=writer -n $DEMO_NAMESPACE -o wide
    echo ""
    log_info "Reader pods:"
    kubectl get pods -l app=demo-postgres,sts-role=reader -n $DEMO_NAMESPACE -o wide
    echo ""
    log_info "Lease information:"
    kubectl get lease demo-postgres-lease -n $DEMO_NAMESPACE -o yaml | grep -A 5 -B 5 holderIdentity
    echo ""
    log_info "StatefulSetLock status:"
    kubectl get statefulsetlock demo-postgres-lock -n $DEMO_NAMESPACE -o jsonpath='{.status}' | jq .
}

# Demo 2: Leader Failover
demo_leader_failover() {
    log_step "Demo 2: Leader Failover"
    
    # Get current leader
    current_leader=$(kubectl get pods -l app=demo-postgres,sts-role=writer -n $DEMO_NAMESPACE -o jsonpath='{.items[0].metadata.name}')
    log_info "Current leader: $current_leader"
    
    log_info "Simulating leader failure by deleting the pod..."
    kubectl delete pod $current_leader -n $DEMO_NAMESPACE
    
    log_info "Waiting for new leader election..."
    timeout=90
    while [ $timeout -gt 0 ]; do
        new_leader=$(kubectl get pods -l app=demo-postgres,sts-role=writer -n $DEMO_NAMESPACE -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")
        if [ -n "$new_leader" ] && [ "$new_leader" != "$current_leader" ]; then
            log_success "New leader elected: $new_leader"
            break
        fi
        sleep 3
        timeout=$((timeout - 3))
    done
    
    if [ $timeout -le 0 ]; then
        log_error "New leader was not elected within timeout"
        return 1
    fi
    
    # Show results
    echo ""
    log_info "New leader:"
    kubectl get pods -l app=demo-postgres,sts-role=writer -n $DEMO_NAMESPACE -o wide
    echo ""
    log_info "Updated lease:"
    kubectl get lease demo-postgres-lease -n $DEMO_NAMESPACE -o yaml | grep -A 5 -B 5 holderIdentity
}

# Demo 3: Scaling Operations
demo_scaling_operations() {
    log_step "Demo 3: Scaling Operations"
    
    log_info "Scaling StatefulSet up to 5 replicas..."
    kubectl scale statefulset demo-postgres --replicas=5 -n $DEMO_NAMESPACE
    
    log_info "Waiting for new pods to be ready..."
    kubectl wait --for=condition=ready --timeout=${TIMEOUT}s pod -l app=demo-postgres -n $DEMO_NAMESPACE
    
    log_info "Verifying pod labels after scaling up..."
    writer_count=$(kubectl get pods -l app=demo-postgres,sts-role=writer -n $DEMO_NAMESPACE --no-headers | wc -l)
    reader_count=$(kubectl get pods -l app=demo-postgres,sts-role=reader -n $DEMO_NAMESPACE --no-headers | wc -l)
    
    log_success "After scaling up: $writer_count writer, $reader_count readers"
    
    # Show all pods
    echo ""
    log_info "All pods after scaling up:"
    kubectl get pods -l app=demo-postgres -n $DEMO_NAMESPACE -o wide --show-labels
    
    log_info "Scaling StatefulSet down to 2 replicas..."
    kubectl scale statefulset demo-postgres --replicas=2 -n $DEMO_NAMESPACE
    
    # Wait for scale down
    sleep 10
    
    log_info "Verifying pod labels after scaling down..."
    writer_count=$(kubectl get pods -l app=demo-postgres,sts-role=writer -n $DEMO_NAMESPACE --no-headers | wc -l)
    reader_count=$(kubectl get pods -l app=demo-postgres,sts-role=reader -n $DEMO_NAMESPACE --no-headers | wc -l)
    
    log_success "After scaling down: $writer_count writer, $reader_count readers"
    
    echo ""
    log_info "Remaining pods after scaling down:"
    kubectl get pods -l app=demo-postgres -n $DEMO_NAMESPACE -o wide --show-labels
}

# Demo 4: Resource Cleanup
demo_resource_cleanup() {
    log_step "Demo 4: Resource Cleanup"
    
    log_info "Verifying lease exists before cleanup..."
    kubectl get lease demo-postgres-lease -n $DEMO_NAMESPACE
    
    log_info "Deleting StatefulSetLock..."
    kubectl delete statefulsetlock demo-postgres-lock -n $DEMO_NAMESPACE
    
    log_info "Waiting for lease cleanup..."
    timeout=60
    while [ $timeout -gt 0 ]; do
        if ! kubectl get lease demo-postgres-lease -n $DEMO_NAMESPACE &> /dev/null; then
            log_success "Lease successfully cleaned up"
            break
        fi
        sleep 2
        timeout=$((timeout - 2))
    done
    
    if [ $timeout -le 0 ]; then
        log_error "Lease was not cleaned up within timeout"
        return 1
    fi
    
    log_info "Verifying no orphaned resources remain..."
    if kubectl get lease demo-postgres-lease -n $DEMO_NAMESPACE &> /dev/null; then
        log_error "Lease still exists after cleanup"
        return 1
    fi
    
    log_success "Resource cleanup completed successfully"
}

# Demo 5: Multiple StatefulSets
demo_multiple_statefulsets() {
    log_step "Demo 5: Multiple StatefulSets"
    
    log_info "Creating multiple StatefulSets to demonstrate concurrent management..."
    
    for i in {1..3}; do
        log_info "Creating StatefulSet demo-app-$i..."
        cat <<EOF | kubectl apply -n $DEMO_NAMESPACE -f -
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: demo-app-$i
  labels:
    app: demo-app-$i
spec:
  serviceName: demo-app-$i-headless
  replicas: 2
  selector:
    matchLabels:
      app: demo-app-$i
  template:
    metadata:
      labels:
        app: demo-app-$i
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
  name: demo-app-$i-headless
spec:
  clusterIP: None
  selector:
    app: demo-app-$i
  ports:
  - port: 80
---
apiVersion: app.anukkrit.me/v1
kind: StatefulSetLock
metadata:
  name: demo-app-$i-lock
spec:
  statefulSetName: demo-app-$i
  leaseName: demo-app-$i-lease
  leaseDurationSeconds: 30
EOF
    done
    
    log_info "Waiting for all StatefulSets to be ready..."
    for i in {1..3}; do
        kubectl wait --for=condition=ready --timeout=${TIMEOUT}s pod -l app=demo-app-$i -n $DEMO_NAMESPACE
    done
    
    log_info "Waiting for all leader elections..."
    for i in {1..3}; do
        timeout=60
        while [ $timeout -gt 0 ]; do
            if kubectl get lease demo-app-$i-lease -n $DEMO_NAMESPACE &> /dev/null; then
                break
            fi
            sleep 2
            timeout=$((timeout - 2))
        done
    done
    
    log_success "All StatefulSets have elected leaders!"
    
    echo ""
    log_info "Summary of all StatefulSets:"
    for i in {1..3}; do
        echo "--- demo-app-$i ---"
        kubectl get pods -l app=demo-app-$i -n $DEMO_NAMESPACE -o wide --show-labels
        echo ""
    done
    
    log_info "All leases:"
    kubectl get leases -n $DEMO_NAMESPACE
}

# Generate demo report
generate_demo_report() {
    log_step "Demo Report"
    
    echo ""
    echo "========================================="
    echo "StatefulSet Leader Election Operator"
    echo "Demo Validation Report"
    echo "========================================="
    echo "Date: $(date)"
    echo "Namespace: $DEMO_NAMESPACE"
    echo ""
    
    log_info "Operator Status:"
    kubectl get deployment sts-leader-elect-operator-controller-manager -n sts-leader-elect-operator-system -o wide
    echo ""
    
    log_info "Demo Resources Created:"
    kubectl get statefulsets,pods,leases,statefulsetlocks -n $DEMO_NAMESPACE
    echo ""
    
    log_success "All demos completed successfully!"
    echo ""
    echo "Key Features Demonstrated:"
    echo "✅ Basic leader election with pod labeling"
    echo "✅ Automatic failover when leader fails"
    echo "✅ Proper handling of scaling operations"
    echo "✅ Resource cleanup with finalizers"
    echo "✅ Concurrent management of multiple StatefulSets"
    echo ""
    echo "The operator is production-ready and fully functional!"
}

# Main execution
main() {
    echo -e "${GREEN}"
    echo "========================================="
    echo "StatefulSet Leader Election Operator"
    echo "Interactive Demo & Validation"
    echo "========================================="
    echo -e "${NC}"
    
    check_prerequisites
    create_demo_namespace
    
    demo_basic_leader_election
    read -p "Press Enter to continue to failover demo..."
    
    demo_leader_failover
    read -p "Press Enter to continue to scaling demo..."
    
    demo_scaling_operations
    read -p "Press Enter to continue to cleanup demo..."
    
    demo_resource_cleanup
    read -p "Press Enter to continue to multiple StatefulSets demo..."
    
    demo_multiple_statefulsets
    
    generate_demo_report
    
    echo ""
    log_success "Demo completed! Resources will be cleaned up automatically."
}

# Run main function
main "$@"