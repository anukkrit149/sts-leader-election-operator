package controller

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	appv1 "github.com/anukkrit/statefulset-leader-election-operator/api/v1"
)

func TestFindStatefulSetLocksForPod(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = appv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	tests := []struct {
		name                 string
		pod                  *corev1.Pod
		statefulSetLocks     []appv1.StatefulSetLock
		expectedRequestCount int
		expectedRequests     []reconcile.Request
	}{
		{
			name: "pod owned by statefulset with corresponding lock",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-sts-0",
					Namespace: "default",
					OwnerReferences: []metav1.OwnerReference{
						{
							Kind: "StatefulSet",
							Name: "test-sts",
							UID:  "test-uid",
						},
					},
				},
			},
			statefulSetLocks: []appv1.StatefulSetLock{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-lock",
						Namespace: "default",
					},
					Spec: appv1.StatefulSetLockSpec{
						StatefulSetName:      "test-sts",
						LeaseName:            "test-lease",
						LeaseDurationSeconds: 30,
					},
				},
			},
			expectedRequestCount: 1,
			expectedRequests: []reconcile.Request{
				{
					NamespacedName: types.NamespacedName{
						Namespace: "default",
						Name:      "test-lock",
					},
				},
			},
		},
		{
			name: "pod not owned by any statefulset with lock",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "unrelated-pod",
					Namespace: "default",
				},
			},
			statefulSetLocks: []appv1.StatefulSetLock{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-lock",
						Namespace: "default",
					},
					Spec: appv1.StatefulSetLockSpec{
						StatefulSetName:      "test-sts",
						LeaseName:            "test-lease",
						LeaseDurationSeconds: 30,
					},
				},
			},
			expectedRequestCount: 0,
			expectedRequests:     []reconcile.Request{},
		},
		{
			name: "pod follows naming convention but no owner reference",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-sts-0",
					Namespace: "default",
				},
			},
			statefulSetLocks: []appv1.StatefulSetLock{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-lock",
						Namespace: "default",
					},
					Spec: appv1.StatefulSetLockSpec{
						StatefulSetName:      "test-sts",
						LeaseName:            "test-lease",
						LeaseDurationSeconds: 30,
					},
				},
			},
			expectedRequestCount: 1,
			expectedRequests: []reconcile.Request{
				{
					NamespacedName: types.NamespacedName{
						Namespace: "default",
						Name:      "test-lock",
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fake client with StatefulSetLock resources
			objs := make([]client.Object, len(tt.statefulSetLocks))
			for i := range tt.statefulSetLocks {
				objs[i] = &tt.statefulSetLocks[i]
			}
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()

			reconciler := &StatefulSetLockReconciler{
				Client: fakeClient,
				Scheme: scheme,
			}

			ctx := context.Background()
			requests := reconciler.findStatefulSetLocksForPod(ctx, tt.pod)

			if len(requests) != tt.expectedRequestCount {
				t.Errorf("expected %d requests, got %d", tt.expectedRequestCount, len(requests))
			}

			for i, expectedReq := range tt.expectedRequests {
				if i >= len(requests) {
					t.Errorf("missing expected request: %v", expectedReq)
					continue
				}
				if requests[i] != expectedReq {
					t.Errorf("expected request %v, got %v", expectedReq, requests[i])
				}
			}
		})
	}
}

func TestFindStatefulSetLocksForLease(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = appv1.AddToScheme(scheme)
	_ = coordinationv1.AddToScheme(scheme)

	tests := []struct {
		name                 string
		lease                *coordinationv1.Lease
		statefulSetLocks     []appv1.StatefulSetLock
		expectedRequestCount int
		expectedRequests     []reconcile.Request
	}{
		{
			name: "lease referenced by statefulsetlock",
			lease: &coordinationv1.Lease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-lease",
					Namespace: "default",
				},
			},
			statefulSetLocks: []appv1.StatefulSetLock{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-lock",
						Namespace: "default",
					},
					Spec: appv1.StatefulSetLockSpec{
						StatefulSetName:      "test-sts",
						LeaseName:            "test-lease",
						LeaseDurationSeconds: 30,
					},
				},
			},
			expectedRequestCount: 1,
			expectedRequests: []reconcile.Request{
				{
					NamespacedName: types.NamespacedName{
						Namespace: "default",
						Name:      "test-lock",
					},
				},
			},
		},
		{
			name: "lease not referenced by any statefulsetlock",
			lease: &coordinationv1.Lease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "unrelated-lease",
					Namespace: "default",
				},
			},
			statefulSetLocks: []appv1.StatefulSetLock{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-lock",
						Namespace: "default",
					},
					Spec: appv1.StatefulSetLockSpec{
						StatefulSetName:      "test-sts",
						LeaseName:            "test-lease",
						LeaseDurationSeconds: 30,
					},
				},
			},
			expectedRequestCount: 0,
			expectedRequests:     []reconcile.Request{},
		},
		{
			name: "multiple statefulsetlocks reference same lease",
			lease: &coordinationv1.Lease{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "shared-lease",
					Namespace: "default",
				},
			},
			statefulSetLocks: []appv1.StatefulSetLock{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-lock-1",
						Namespace: "default",
					},
					Spec: appv1.StatefulSetLockSpec{
						StatefulSetName:      "test-sts-1",
						LeaseName:            "shared-lease",
						LeaseDurationSeconds: 30,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-lock-2",
						Namespace: "default",
					},
					Spec: appv1.StatefulSetLockSpec{
						StatefulSetName:      "test-sts-2",
						LeaseName:            "shared-lease",
						LeaseDurationSeconds: 30,
					},
				},
			},
			expectedRequestCount: 2,
			expectedRequests: []reconcile.Request{
				{
					NamespacedName: types.NamespacedName{
						Namespace: "default",
						Name:      "test-lock-1",
					},
				},
				{
					NamespacedName: types.NamespacedName{
						Namespace: "default",
						Name:      "test-lock-2",
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fake client with StatefulSetLock resources
			objs := make([]client.Object, len(tt.statefulSetLocks))
			for i := range tt.statefulSetLocks {
				objs[i] = &tt.statefulSetLocks[i]
			}
			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()

			reconciler := &StatefulSetLockReconciler{
				Client: fakeClient,
				Scheme: scheme,
			}

			ctx := context.Background()
			requests := reconciler.findStatefulSetLocksForLease(ctx, tt.lease)

			if len(requests) != tt.expectedRequestCount {
				t.Errorf("expected %d requests, got %d", tt.expectedRequestCount, len(requests))
			}

			// Check that all expected requests are present (order may vary)
			for _, expectedReq := range tt.expectedRequests {
				found := false
				for _, actualReq := range requests {
					if actualReq == expectedReq {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("missing expected request: %v", expectedReq)
				}
			}
		})
	}
}

func TestIsPodOwnedByStatefulSet(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	reconciler := &StatefulSetLockReconciler{
		Scheme: scheme,
	}

	tests := []struct {
		name            string
		pod             *corev1.Pod
		statefulSetName string
		expected        bool
	}{
		{
			name: "pod owned by statefulset via owner reference",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-sts-0",
					Namespace: "default",
					OwnerReferences: []metav1.OwnerReference{
						{
							Kind: "StatefulSet",
							Name: "test-sts",
							UID:  "test-uid",
						},
					},
				},
			},
			statefulSetName: "test-sts",
			expected:        true,
		},
		{
			name: "pod follows naming convention but no owner reference",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-sts-0",
					Namespace: "default",
				},
			},
			statefulSetName: "test-sts",
			expected:        true,
		},
		{
			name: "pod with different owner reference",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-sts-0",
					Namespace: "default",
					OwnerReferences: []metav1.OwnerReference{
						{
							Kind: "StatefulSet",
							Name: "other-sts",
							UID:  "other-uid",
						},
					},
				},
			},
			statefulSetName: "test-sts",
			expected:        false,
		},
		{
			name: "pod with non-numeric suffix",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-sts-abc",
					Namespace: "default",
				},
			},
			statefulSetName: "test-sts",
			expected:        false,
		},
		{
			name: "pod with different name pattern",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "unrelated-pod",
					Namespace: "default",
				},
			},
			statefulSetName: "test-sts",
			expected:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			result := reconciler.isPodOwnedByStatefulSet(ctx, tt.pod, tt.statefulSetName)

			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}
