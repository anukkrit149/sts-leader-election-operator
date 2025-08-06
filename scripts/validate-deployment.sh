#!/bin/bash

# StatefulSet Leader Election Operator Deployment Validation Script

set -e

NAMESPACE="sts-leader-elect-operator-system"
DEPLOYMENT_NAME="sts-leader-elect-operator-controller-manager"
TIMEOUT=300

echo "🚀 Validating StatefulSet Leader Election Operator deployment..."

# Function to wait for condition
wait_for_condition() {
    local condition="$1"
    local timeout="$2"
    local interval=5
    local elapsed=0

    while [ $elapsed -lt $timeout ]; do
        if eval "$condition"; then
            return 0
        fi
        sleep $interval
        elapsed=$((elapsed + interval))
        echo "⏳ Waiting... (${elapsed}s/${timeout}s)"
    done
    
    echo "❌ Timeout waiting for condition: $condition"
    return 1
}

# Check if namespace exists
echo "📋 Checking namespace..."
if ! kubectl get namespace "$NAMESPACE" >/dev/null 2>&1; then
    echo "❌ Namespace $NAMESPACE not found"
    exit 1
fi
echo "✅ Namespace $NAMESPACE exists"

# Check if CRD is installed
echo "📋 Checking Custom Resource Definition..."
if ! kubectl get crd statefulsetlocks.app.anukkrit.me >/dev/null 2>&1; then
    echo "❌ StatefulSetLock CRD not found"
    exit 1
fi
echo "✅ StatefulSetLock CRD is installed"

# Check deployment exists
echo "📋 Checking deployment..."
if ! kubectl get deployment "$DEPLOYMENT_NAME" -n "$NAMESPACE" >/dev/null 2>&1; then
    echo "❌ Deployment $DEPLOYMENT_NAME not found"
    exit 1
fi
echo "✅ Deployment $DEPLOYMENT_NAME exists"

# Wait for deployment to be ready
echo "📋 Waiting for deployment to be ready..."
wait_for_condition "kubectl get deployment $DEPLOYMENT_NAME -n $NAMESPACE -o jsonpath='{.status.readyReplicas}' | grep -q '^[1-9]'" $TIMEOUT
echo "✅ Deployment is ready"

# Check pod status
echo "📋 Checking pod status..."
PODS=$(kubectl get pods -n "$NAMESPACE" -l control-plane=controller-manager -o jsonpath='{.items[*].metadata.name}')
for pod in $PODS; do
    if ! kubectl get pod "$pod" -n "$NAMESPACE" -o jsonpath='{.status.phase}' | grep -q "Running"; then
        echo "❌ Pod $pod is not running"
        kubectl describe pod "$pod" -n "$NAMESPACE"
        exit 1
    fi
    echo "✅ Pod $pod is running"
done

# Check health endpoints
echo "📋 Checking health endpoints..."
for pod in $PODS; do
    echo "  Checking health endpoint for pod $pod..."
    if ! kubectl exec -n "$NAMESPACE" "$pod" -- wget -q --spider http://localhost:8081/healthz; then
        echo "❌ Health endpoint not responding for pod $pod"
        exit 1
    fi
    
    echo "  Checking readiness endpoint for pod $pod..."
    if ! kubectl exec -n "$NAMESPACE" "$pod" -- wget -q --spider http://localhost:8081/readyz; then
        echo "❌ Readiness endpoint not responding for pod $pod"
        exit 1
    fi
done
echo "✅ Health endpoints are responding"

# Check RBAC permissions
echo "📋 Checking RBAC permissions..."
SERVICE_ACCOUNT="$DEPLOYMENT_NAME"
if ! kubectl auth can-i get statefulsetlocks.app.anukkrit.me --as=system:serviceaccount:$NAMESPACE:$SERVICE_ACCOUNT >/dev/null 2>&1; then
    echo "❌ Service account lacks required permissions"
    exit 1
fi
echo "✅ RBAC permissions are configured correctly"

# Check metrics endpoint (if accessible)
echo "📋 Checking metrics endpoint..."
for pod in $PODS; do
    if kubectl exec -n "$NAMESPACE" "$pod" -- wget -q --spider http://localhost:8443/metrics 2>/dev/null; then
        echo "✅ Metrics endpoint is accessible for pod $pod"
    else
        echo "⚠️  Metrics endpoint may require authentication for pod $pod"
    fi
done

# Test basic functionality with a sample StatefulSetLock
echo "📋 Testing basic functionality..."
cat <<EOF | kubectl apply -f -
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: test-sts
  namespace: default
spec:
  serviceName: test-service
  replicas: 2
  selector:
    matchLabels:
      app: test
  template:
    metadata:
      labels:
        app: test
    spec:
      containers:
      - name: test
        image: nginx:alpine
        ports:
        - containerPort: 80
---
apiVersion: app.anukkrit.me/v1
kind: StatefulSetLock
metadata:
  name: test-lock
  namespace: default
spec:
  statefulSetName: test-sts
  leaseName: test-lease
  leaseDurationSeconds: 30
EOF

# Wait for StatefulSet pods to be ready
echo "⏳ Waiting for test StatefulSet pods to be ready..."
wait_for_condition "kubectl get pods -l app=test -o jsonpath='{.items[?(@.status.phase==\"Running\")].metadata.name}' | wc -w | grep -q '^[1-9]'" 60

# Wait for StatefulSetLock to be processed
echo "⏳ Waiting for StatefulSetLock to be processed..."
sleep 10

# Check if lease was created
if kubectl get lease test-lease -n default >/dev/null 2>&1; then
    echo "✅ Lease was created successfully"
else
    echo "❌ Lease was not created"
    kubectl describe statefulsetlock test-lock -n default
    exit 1
fi

# Check if pods are labeled
LABELED_PODS=$(kubectl get pods -l app=test,sts-role -o name | wc -l)
if [ "$LABELED_PODS" -gt 0 ]; then
    echo "✅ Pods are labeled with roles"
else
    echo "❌ Pods are not labeled with roles"
    kubectl get pods -l app=test --show-labels
    exit 1
fi

# Cleanup test resources
echo "🧹 Cleaning up test resources..."
kubectl delete statefulsetlock test-lock -n default --ignore-not-found=true
kubectl delete statefulset test-sts -n default --ignore-not-found=true
kubectl delete lease test-lease -n default --ignore-not-found=true

echo "🎉 All validation checks passed! The operator is deployed and functioning correctly."