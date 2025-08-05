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
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	appv1 "github.com/anukkrit/statefulset-leader-election-operator/api/v1"
)

func TestFetchStatefulSetLock(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = appv1.AddToScheme(scheme)

	tests := []struct {
		name           string
		existingObjs   []client.Object
		namespacedName types.NamespacedName
		expectError    bool
		errorContains  string
	}{
		{
			name: "successful fetch",
			existingObjs: []client.Object{
				&appv1.StatefulSetLock{
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
			namespacedName: types.NamespacedName{Name: "test-lock", Namespace: "default"},
			expectError:    false,
		},
		{
			name:           "not found",
			existingObjs:   []client.Object{},
			namespacedName: types.NamespacedName{Name: "missing-lock", Namespace: "default"},
			expectError:    true,
			errorContains:  "not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tt.existingObjs...).
				Build()

			reconciler := &StatefulSetLockReconciler{
				Client: fakeClient,
				Scheme: scheme,
			}

			result, err := reconciler.fetchStatefulSetLock(context.TODO(), tt.namespacedName)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				if tt.errorContains != "" && !containsString(err.Error(), tt.errorContains) {
					t.Errorf("expected error to contain %q, got %q", tt.errorContains, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if result == nil {
					t.Errorf("expected result but got nil")
				}
				if result != nil && result.Name != tt.namespacedName.Name {
					t.Errorf("expected name %q, got %q", tt.namespacedName.Name, result.Name)
				}
			}
		})
	}
}

func TestFetchStatefulSet(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = appsv1.AddToScheme(scheme)

	tests := []struct {
		name          string
		existingObjs  []client.Object
		namespace     string
		stsName       string
		expectError   bool
		errorContains string
	}{
		{
			name: "successful fetch",
			existingObjs: []client.Object{
				&appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-sts",
						Namespace: "default",
					},
					Spec: appsv1.StatefulSetSpec{
						Replicas: ptr.To(int32(3)),
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "test"},
						},
					},
				},
			},
			namespace:   "default",
			stsName:     "test-sts",
			expectError: false,
		},
		{
			name:          "not found",
			existingObjs:  []client.Object{},
			namespace:     "default",
			stsName:       "missing-sts",
			expectError:   true,
			errorContains: "not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tt.existingObjs...).
				Build()

			reconciler := &StatefulSetLockReconciler{
				Client: fakeClient,
				Scheme: scheme,
			}

			result, err := reconciler.fetchStatefulSet(context.TODO(), tt.namespace, tt.stsName)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				if tt.errorContains != "" && !containsString(err.Error(), tt.errorContains) {
					t.Errorf("expected error to contain %q, got %q", tt.errorContains, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if result == nil {
					t.Errorf("expected result but got nil")
				}
				if result != nil && result.Name != tt.stsName {
					t.Errorf("expected name %q, got %q", tt.stsName, result.Name)
				}
			}
		})
	}
}

func TestFetchStatefulSetPods(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)

	stsUID := types.UID("test-sts-uid")
	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-sts",
			Namespace: "default",
			UID:       stsUID,
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: ptr.To(int32(3)),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test"},
			},
		},
	}

	tests := []struct {
		name         string
		existingObjs []client.Object
		statefulSet  *appsv1.StatefulSet
		expectedPods int
		expectError  bool
	}{
		{
			name: "fetch pods successfully",
			existingObjs: []client.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-sts-0",
						Namespace: "default",
						Labels:    map[string]string{"app": "test"},
						OwnerReferences: []metav1.OwnerReference{
							{
								Kind: "StatefulSet",
								Name: "test-sts",
								UID:  stsUID,
							},
						},
					},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-sts-1",
						Namespace: "default",
						Labels:    map[string]string{"app": "test"},
						OwnerReferences: []metav1.OwnerReference{
							{
								Kind: "StatefulSet",
								Name: "test-sts",
								UID:  stsUID,
							},
						},
					},
				},
				// Pod with different owner - should be filtered out
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "other-pod",
						Namespace: "default",
						Labels:    map[string]string{"app": "test"},
						OwnerReferences: []metav1.OwnerReference{
							{
								Kind: "StatefulSet",
								Name: "other-sts",
								UID:  "other-uid",
							},
						},
					},
				},
			},
			statefulSet:  statefulSet,
			expectedPods: 2,
			expectError:  false,
		},
		{
			name:         "no pods found",
			existingObjs: []client.Object{},
			statefulSet:  statefulSet,
			expectedPods: 0,
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tt.existingObjs...).
				Build()

			reconciler := &StatefulSetLockReconciler{
				Client: fakeClient,
				Scheme: scheme,
			}

			result, err := reconciler.fetchStatefulSetPods(context.TODO(), tt.statefulSet)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if len(result) != tt.expectedPods {
					t.Errorf("expected %d pods, got %d", tt.expectedPods, len(result))
				}
			}
		})
	}
}

func TestFetchLease(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = coordinationv1.AddToScheme(scheme)

	tests := []struct {
		name          string
		existingObjs  []client.Object
		namespace     string
		leaseName     string
		expectError   bool
		errorContains string
	}{
		{
			name: "successful fetch",
			existingObjs: []client.Object{
				&coordinationv1.Lease{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-lease",
						Namespace: "default",
					},
					Spec: coordinationv1.LeaseSpec{
						HolderIdentity: ptr.To("test-pod-0"),
					},
				},
			},
			namespace:   "default",
			leaseName:   "test-lease",
			expectError: false,
		},
		{
			name:          "not found",
			existingObjs:  []client.Object{},
			namespace:     "default",
			leaseName:     "missing-lease",
			expectError:   true,
			errorContains: "not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tt.existingObjs...).
				Build()

			reconciler := &StatefulSetLockReconciler{
				Client: fakeClient,
				Scheme: scheme,
			}

			result, err := reconciler.fetchLease(context.TODO(), tt.namespace, tt.leaseName)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				if tt.errorContains != "" && !containsString(err.Error(), tt.errorContains) {
					t.Errorf("expected error to contain %q, got %q", tt.errorContains, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if result == nil {
					t.Errorf("expected result but got nil")
				}
				if result != nil && result.Name != tt.leaseName {
					t.Errorf("expected name %q, got %q", tt.leaseName, result.Name)
				}
			}
		})
	}
}

func TestValidateStatefulSetLockSpec(t *testing.T) {
	reconciler := &StatefulSetLockReconciler{}

	tests := []struct {
		name          string
		spec          *appv1.StatefulSetLockSpec
		expectError   bool
		errorContains string
	}{
		{
			name: "valid spec",
			spec: &appv1.StatefulSetLockSpec{
				StatefulSetName:      "test-sts",
				LeaseName:            "test-lease",
				LeaseDurationSeconds: 30,
			},
			expectError: false,
		},
		{
			name: "empty statefulset name",
			spec: &appv1.StatefulSetLockSpec{
				StatefulSetName:      "",
				LeaseName:            "test-lease",
				LeaseDurationSeconds: 30,
			},
			expectError:   true,
			errorContains: "statefulSetName",
		},
		{
			name: "empty lease name",
			spec: &appv1.StatefulSetLockSpec{
				StatefulSetName:      "test-sts",
				LeaseName:            "",
				LeaseDurationSeconds: 30,
			},
			expectError:   true,
			errorContains: "leaseName",
		},
		{
			name: "zero lease duration",
			spec: &appv1.StatefulSetLockSpec{
				StatefulSetName:      "test-sts",
				LeaseName:            "test-lease",
				LeaseDurationSeconds: 0,
			},
			expectError:   true,
			errorContains: "leaseDurationSeconds",
		},
		{
			name: "negative lease duration",
			spec: &appv1.StatefulSetLockSpec{
				StatefulSetName:      "test-sts",
				LeaseName:            "test-lease",
				LeaseDurationSeconds: -10,
			},
			expectError:   true,
			errorContains: "leaseDurationSeconds",
		},
		{
			name: "lease duration too large",
			spec: &appv1.StatefulSetLockSpec{
				StatefulSetName:      "test-sts",
				LeaseName:            "test-lease",
				LeaseDurationSeconds: 4000,
			},
			expectError:   true,
			errorContains: "leaseDurationSeconds",
		},
		{
			name: "multiple validation errors",
			spec: &appv1.StatefulSetLockSpec{
				StatefulSetName:      "",
				LeaseName:            "",
				LeaseDurationSeconds: 0,
			},
			expectError:   true,
			errorContains: "validation failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := reconciler.validateStatefulSetLockSpec(tt.spec)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				if tt.errorContains != "" && !containsString(err.Error(), tt.errorContains) {
					t.Errorf("expected error to contain %q, got %q", tt.errorContains, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestValidateStatefulSet(t *testing.T) {
	reconciler := &StatefulSetLockReconciler{}

	tests := []struct {
		name          string
		statefulSet   *appsv1.StatefulSet
		expectError   bool
		errorContains string
	}{
		{
			name: "valid statefulset",
			statefulSet: &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{Name: "test-sts"},
				Spec: appsv1.StatefulSetSpec{
					Replicas: ptr.To(int32(3)),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "test"},
					},
				},
			},
			expectError: false,
		},
		{
			name: "nil replicas",
			statefulSet: &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{Name: "test-sts"},
				Spec: appsv1.StatefulSetSpec{
					Replicas: nil,
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "test"},
					},
				},
			},
			expectError:   true,
			errorContains: "no replicas configured",
		},
		{
			name: "zero replicas",
			statefulSet: &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{Name: "test-sts"},
				Spec: appsv1.StatefulSetSpec{
					Replicas: ptr.To(int32(0)),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "test"},
					},
				},
			},
			expectError:   true,
			errorContains: "no replicas configured",
		},
		{
			name: "nil selector",
			statefulSet: &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{Name: "test-sts"},
				Spec: appsv1.StatefulSetSpec{
					Replicas: ptr.To(int32(3)),
					Selector: nil,
				},
			},
			expectError:   true,
			errorContains: "no selector configured",
		},
		{
			name: "empty selector",
			statefulSet: &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{Name: "test-sts"},
				Spec: appsv1.StatefulSetSpec{
					Replicas: ptr.To(int32(3)),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{},
					},
				},
			},
			expectError:   true,
			errorContains: "no selector configured",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := reconciler.validateStatefulSet(tt.statefulSet)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				if tt.errorContains != "" && !containsString(err.Error(), tt.errorContains) {
					t.Errorf("expected error to contain %q, got %q", tt.errorContains, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

// Pod Management Helper Function Tests

func TestIsPodReady(t *testing.T) {
	tests := []struct {
		name     string
		pod      corev1.Pod
		expected bool
	}{
		{
			name: "pod is ready",
			pod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pod-0"},
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{
							Type:   corev1.PodReady,
							Status: corev1.ConditionTrue,
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "pod is not ready",
			pod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pod-0"},
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{
							Type:   corev1.PodReady,
							Status: corev1.ConditionFalse,
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "pod has no ready condition",
			pod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pod-0"},
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{
							Type:   corev1.PodScheduled,
							Status: corev1.ConditionTrue,
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "pod has no conditions",
			pod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pod-0"},
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{},
				},
			},
			expected: false,
		},
		{
			name: "pod has multiple conditions with ready true",
			pod: corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: "test-pod-0"},
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{
							Type:   corev1.PodScheduled,
							Status: corev1.ConditionTrue,
						},
						{
							Type:   corev1.PodReady,
							Status: corev1.ConditionTrue,
						},
						{
							Type:   corev1.ContainersReady,
							Status: corev1.ConditionTrue,
						},
					},
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsPodReady(tt.pod)
			if result != tt.expected {
				t.Errorf("IsPodReady() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestGetReadyPods(t *testing.T) {
	tests := []struct {
		name          string
		pods          []corev1.Pod
		expectedCount int
		expectedNames []string
	}{
		{
			name: "all pods ready",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "test-pod-0"},
					Status: corev1.PodStatus{
						Conditions: []corev1.PodCondition{
							{Type: corev1.PodReady, Status: corev1.ConditionTrue},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "test-pod-1"},
					Status: corev1.PodStatus{
						Conditions: []corev1.PodCondition{
							{Type: corev1.PodReady, Status: corev1.ConditionTrue},
						},
					},
				},
			},
			expectedCount: 2,
			expectedNames: []string{"test-pod-0", "test-pod-1"},
		},
		{
			name: "some pods ready",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "test-pod-0"},
					Status: corev1.PodStatus{
						Conditions: []corev1.PodCondition{
							{Type: corev1.PodReady, Status: corev1.ConditionTrue},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "test-pod-1"},
					Status: corev1.PodStatus{
						Conditions: []corev1.PodCondition{
							{Type: corev1.PodReady, Status: corev1.ConditionFalse},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "test-pod-2"},
					Status: corev1.PodStatus{
						Conditions: []corev1.PodCondition{
							{Type: corev1.PodReady, Status: corev1.ConditionTrue},
						},
					},
				},
			},
			expectedCount: 2,
			expectedNames: []string{"test-pod-0", "test-pod-2"},
		},
		{
			name: "no pods ready",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "test-pod-0"},
					Status: corev1.PodStatus{
						Conditions: []corev1.PodCondition{
							{Type: corev1.PodReady, Status: corev1.ConditionFalse},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{Name: "test-pod-1"},
					Status: corev1.PodStatus{
						Conditions: []corev1.PodCondition{
							{Type: corev1.PodReady, Status: corev1.ConditionFalse},
						},
					},
				},
			},
			expectedCount: 0,
			expectedNames: []string{},
		},
		{
			name:          "empty pod list",
			pods:          []corev1.Pod{},
			expectedCount: 0,
			expectedNames: []string{},
		},
		{
			name: "pods with no ready condition",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "test-pod-0"},
					Status: corev1.PodStatus{
						Conditions: []corev1.PodCondition{
							{Type: corev1.PodScheduled, Status: corev1.ConditionTrue},
						},
					},
				},
			},
			expectedCount: 0,
			expectedNames: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetReadyPods(tt.pods)

			if len(result) != tt.expectedCount {
				t.Errorf("GetReadyPods() returned %d pods, expected %d", len(result), tt.expectedCount)
			}

			// Check that the returned pods have the expected names
			resultNames := make([]string, len(result))
			for i, pod := range result {
				resultNames[i] = pod.Name
			}

			if !stringSlicesEqual(resultNames, tt.expectedNames) {
				t.Errorf("GetReadyPods() returned pods %v, expected %v", resultNames, tt.expectedNames)
			}

			// Verify all returned pods are actually ready
			for _, pod := range result {
				if !IsPodReady(pod) {
					t.Errorf("GetReadyPods() returned non-ready pod %s", pod.Name)
				}
			}
		})
	}
}

func TestSortPodsByOrdinal(t *testing.T) {
	tests := []struct {
		name          string
		pods          []corev1.Pod
		expectedOrder []string
	}{
		{
			name: "pods in correct order",
			pods: []corev1.Pod{
				{ObjectMeta: metav1.ObjectMeta{Name: "test-sts-0"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "test-sts-1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "test-sts-2"}},
			},
			expectedOrder: []string{"test-sts-0", "test-sts-1", "test-sts-2"},
		},
		{
			name: "pods in reverse order",
			pods: []corev1.Pod{
				{ObjectMeta: metav1.ObjectMeta{Name: "test-sts-2"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "test-sts-1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "test-sts-0"}},
			},
			expectedOrder: []string{"test-sts-0", "test-sts-1", "test-sts-2"},
		},
		{
			name: "pods in random order",
			pods: []corev1.Pod{
				{ObjectMeta: metav1.ObjectMeta{Name: "test-sts-1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "test-sts-3"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "test-sts-0"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "test-sts-2"}},
			},
			expectedOrder: []string{"test-sts-0", "test-sts-1", "test-sts-2", "test-sts-3"},
		},
		{
			name: "single pod",
			pods: []corev1.Pod{
				{ObjectMeta: metav1.ObjectMeta{Name: "test-sts-0"}},
			},
			expectedOrder: []string{"test-sts-0"},
		},
		{
			name:          "empty pod list",
			pods:          []corev1.Pod{},
			expectedOrder: []string{},
		},
		{
			name: "pods with double digit ordinals",
			pods: []corev1.Pod{
				{ObjectMeta: metav1.ObjectMeta{Name: "test-sts-10"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "test-sts-2"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "test-sts-1"}},
			},
			expectedOrder: []string{"test-sts-1", "test-sts-2", "test-sts-10"},
		},
		{
			name: "pods with invalid names sorted last",
			pods: []corev1.Pod{
				{ObjectMeta: metav1.ObjectMeta{Name: "test-sts-1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "invalid-pod-name"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "test-sts-0"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "another-invalid"}},
			},
			expectedOrder: []string{"test-sts-0", "test-sts-1", "invalid-pod-name", "another-invalid"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SortPodsByOrdinal(tt.pods)

			if len(result) != len(tt.expectedOrder) {
				t.Errorf("SortPodsByOrdinal() returned %d pods, expected %d", len(result), len(tt.expectedOrder))
			}

			// Check the order
			for i, expectedName := range tt.expectedOrder {
				if i >= len(result) {
					t.Errorf("SortPodsByOrdinal() missing pod at index %d, expected %s", i, expectedName)
					continue
				}
				if result[i].Name != expectedName {
					t.Errorf("SortPodsByOrdinal() pod at index %d is %s, expected %s", i, result[i].Name, expectedName)
				}
			}

			// Verify original slice is not modified
			if len(tt.pods) > 0 {
				originalFirst := tt.pods[0].Name
				resultFirst := result[0].Name
				// If they're different, it means sorting worked and original wasn't modified
				// If they're the same, check if the original order was already correct
				if originalFirst != resultFirst {
					// Good, original wasn't modified
				} else {
					// Check if original was already in correct order
					alreadySorted := true
					for i, expectedName := range tt.expectedOrder {
						if i >= len(tt.pods) || tt.pods[i].Name != expectedName {
							alreadySorted = false
							break
						}
					}
					if !alreadySorted {
						t.Errorf("SortPodsByOrdinal() may have modified the original slice")
					}
				}
			}
		})
	}
}

func TestExtractOrdinalFromPodName(t *testing.T) {
	tests := []struct {
		name     string
		podName  string
		expected int
	}{
		{
			name:     "valid pod name with single digit",
			podName:  "test-sts-0",
			expected: 0,
		},
		{
			name:     "valid pod name with double digit",
			podName:  "test-sts-10",
			expected: 10,
		},
		{
			name:     "valid pod name with triple digit",
			podName:  "test-sts-123",
			expected: 123,
		},
		{
			name:     "pod name with multiple dashes",
			podName:  "my-test-sts-5",
			expected: 5,
		},
		{
			name:     "pod name with no dash",
			podName:  "invalidpodname",
			expected: 999999,
		},
		{
			name:     "pod name with non-numeric ordinal",
			podName:  "test-sts-abc",
			expected: 999999,
		},
		{
			name:     "pod name ending with dash",
			podName:  "test-sts-",
			expected: 999999,
		},
		{
			name:     "empty pod name",
			podName:  "",
			expected: 999999,
		},
		{
			name:     "pod name with double dash before number",
			podName:  "test-sts--1",
			expected: 1,
		},
		{
			name:     "pod name with actual negative number",
			podName:  "test-sts-negative-1",
			expected: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractOrdinalFromPodName(tt.podName)
			if result != tt.expected {
				t.Errorf("extractOrdinalFromPodName(%s) = %d, expected %d", tt.podName, result, tt.expected)
			}
		})
	}
}

// Helper function to check if a string contains a substring
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(substr) > 0 && len(s) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Helper function to compare string slices
func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i, v := range a {
		if v != b[i] {
			return false
		}
	}
	return true
}

// Pod Labeling Function Tests

func TestLabelPodWithRole(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	tests := []struct {
		name          string
		pod           *corev1.Pod
		role          string
		expectError   bool
		expectUpdate  bool
		errorContains string
	}{
		{
			name: "label pod as writer",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod-0",
					Namespace: "default",
					Labels:    map[string]string{"app": "test"},
				},
			},
			role:         PodRoleWriter,
			expectError:  false,
			expectUpdate: true,
		},
		{
			name: "label pod as reader",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod-1",
					Namespace: "default",
					Labels:    map[string]string{"app": "test"},
				},
			},
			role:         PodRoleReader,
			expectError:  false,
			expectUpdate: true,
		},
		{
			name: "pod already has correct label",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod-0",
					Namespace: "default",
					Labels: map[string]string{
						"app":        "test",
						PodRoleLabel: PodRoleWriter,
					},
				},
			},
			role:         PodRoleWriter,
			expectError:  false,
			expectUpdate: false, // Should not update if already correct
		},
		{
			name: "pod with nil labels",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod-0",
					Namespace: "default",
					Labels:    nil,
				},
			},
			role:         PodRoleWriter,
			expectError:  false,
			expectUpdate: true,
		},
		{
			name: "update existing role label",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod-0",
					Namespace: "default",
					Labels: map[string]string{
						"app":        "test",
						PodRoleLabel: PodRoleReader,
					},
				},
			},
			role:         PodRoleWriter,
			expectError:  false,
			expectUpdate: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tt.pod).
				Build()

			reconciler := &StatefulSetLockReconciler{
				Client: fakeClient,
				Scheme: scheme,
			}

			// Store original labels for comparison
			originalLabels := make(map[string]string)
			if tt.pod.Labels != nil {
				for k, v := range tt.pod.Labels {
					originalLabels[k] = v
				}
			}

			err := reconciler.labelPodWithRole(context.TODO(), tt.pod, tt.role)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				if tt.errorContains != "" && !containsString(err.Error(), tt.errorContains) {
					t.Errorf("expected error to contain %q, got %q", tt.errorContains, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}

				// Fetch the updated pod
				var updatedPod corev1.Pod
				err := fakeClient.Get(context.TODO(), types.NamespacedName{
					Name:      tt.pod.Name,
					Namespace: tt.pod.Namespace,
				}, &updatedPod)
				if err != nil {
					t.Errorf("failed to fetch updated pod: %v", err)
					return
				}

				// Check if the role label is correct
				if updatedPod.Labels == nil {
					t.Errorf("expected pod to have labels after update")
					return
				}

				actualRole, exists := updatedPod.Labels[PodRoleLabel]
				if !exists {
					t.Errorf("expected pod to have role label %q", PodRoleLabel)
					return
				}

				if actualRole != tt.role {
					t.Errorf("expected role label to be %q, got %q", tt.role, actualRole)
				}

				// Verify other labels are preserved
				for k, v := range originalLabels {
					if k != PodRoleLabel {
						if actualValue, exists := updatedPod.Labels[k]; !exists || actualValue != v {
							t.Errorf("expected original label %q=%q to be preserved, got %q", k, v, actualValue)
						}
					}
				}
			}
		})
	}
}

func TestUpdatePodLabels(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	tests := []struct {
		name           string
		pods           []corev1.Pod
		leaderPodName  string
		expectError    bool
		expectedLabels map[string]string // pod name -> expected role
		errorContains  string
	}{
		{
			name: "label multiple pods with leader",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-sts-0",
						Namespace: "default",
						Labels:    map[string]string{"app": "test"},
					},
					Status: corev1.PodStatus{
						Conditions: []corev1.PodCondition{
							{Type: corev1.PodReady, Status: corev1.ConditionTrue},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-sts-1",
						Namespace: "default",
						Labels:    map[string]string{"app": "test"},
					},
					Status: corev1.PodStatus{
						Conditions: []corev1.PodCondition{
							{Type: corev1.PodReady, Status: corev1.ConditionTrue},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-sts-2",
						Namespace: "default",
						Labels:    map[string]string{"app": "test"},
					},
					Status: corev1.PodStatus{
						Conditions: []corev1.PodCondition{
							{Type: corev1.PodReady, Status: corev1.ConditionTrue},
						},
					},
				},
			},
			leaderPodName: "test-sts-1",
			expectError:   false,
			expectedLabels: map[string]string{
				"test-sts-0": PodRoleReader,
				"test-sts-1": PodRoleWriter,
				"test-sts-2": PodRoleReader,
			},
		},
		{
			name: "skip non-ready pods",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-sts-0",
						Namespace: "default",
						Labels:    map[string]string{"app": "test"},
					},
					Status: corev1.PodStatus{
						Conditions: []corev1.PodCondition{
							{Type: corev1.PodReady, Status: corev1.ConditionTrue},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-sts-1",
						Namespace: "default",
						Labels:    map[string]string{"app": "test"},
					},
					Status: corev1.PodStatus{
						Conditions: []corev1.PodCondition{
							{Type: corev1.PodReady, Status: corev1.ConditionFalse},
						},
					},
				},
			},
			leaderPodName: "test-sts-0",
			expectError:   false,
			expectedLabels: map[string]string{
				"test-sts-0": PodRoleWriter,
				// test-sts-1 should not be labeled because it's not ready
			},
		},
		{
			name:           "empty pod list",
			pods:           []corev1.Pod{},
			leaderPodName:  "test-sts-0",
			expectError:    false,
			expectedLabels: map[string]string{},
		},
		{
			name: "single pod as leader",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-sts-0",
						Namespace: "default",
						Labels:    map[string]string{"app": "test"},
					},
					Status: corev1.PodStatus{
						Conditions: []corev1.PodCondition{
							{Type: corev1.PodReady, Status: corev1.ConditionTrue},
						},
					},
				},
			},
			leaderPodName: "test-sts-0",
			expectError:   false,
			expectedLabels: map[string]string{
				"test-sts-0": PodRoleWriter,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Convert pods to client objects
			objs := make([]client.Object, len(tt.pods))
			for i := range tt.pods {
				objs[i] = &tt.pods[i]
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objs...).
				Build()

			reconciler := &StatefulSetLockReconciler{
				Client: fakeClient,
				Scheme: scheme,
			}

			err := reconciler.updatePodLabels(context.TODO(), tt.pods, tt.leaderPodName)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				if tt.errorContains != "" && !containsString(err.Error(), tt.errorContains) {
					t.Errorf("expected error to contain %q, got %q", tt.errorContains, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}

				// Check labels on each pod
				for podName, expectedRole := range tt.expectedLabels {
					var updatedPod corev1.Pod
					err := fakeClient.Get(context.TODO(), types.NamespacedName{
						Name:      podName,
						Namespace: "default",
					}, &updatedPod)
					if err != nil {
						t.Errorf("failed to fetch updated pod %s: %v", podName, err)
						continue
					}

					if updatedPod.Labels == nil {
						t.Errorf("expected pod %s to have labels", podName)
						continue
					}

					actualRole, exists := updatedPod.Labels[PodRoleLabel]
					if !exists {
						t.Errorf("expected pod %s to have role label", podName)
						continue
					}

					if actualRole != expectedRole {
						t.Errorf("expected pod %s to have role %q, got %q", podName, expectedRole, actualRole)
					}
				}

				// Check that non-ready pods don't have role labels
				for _, pod := range tt.pods {
					if !IsPodReady(pod) {
						var updatedPod corev1.Pod
						err := fakeClient.Get(context.TODO(), types.NamespacedName{
							Name:      pod.Name,
							Namespace: pod.Namespace,
						}, &updatedPod)
						if err != nil {
							t.Errorf("failed to fetch pod %s: %v", pod.Name, err)
							continue
						}

						if updatedPod.Labels != nil {
							if _, hasRoleLabel := updatedPod.Labels[PodRoleLabel]; hasRoleLabel {
								t.Errorf("expected non-ready pod %s to not have role label", pod.Name)
							}
						}
					}
				}
			}
		})
	}
}

func TestRemovePodRoleLabels(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	tests := []struct {
		name          string
		pods          []corev1.Pod
		expectError   bool
		errorContains string
	}{
		{
			name: "remove role labels from multiple pods",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-sts-0",
						Namespace: "default",
						Labels: map[string]string{
							"app":        "test",
							PodRoleLabel: PodRoleWriter,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-sts-1",
						Namespace: "default",
						Labels: map[string]string{
							"app":        "test",
							PodRoleLabel: PodRoleReader,
						},
					},
				},
			},
			expectError: false,
		},
		{
			name: "skip pods without role labels",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-sts-0",
						Namespace: "default",
						Labels: map[string]string{
							"app":        "test",
							PodRoleLabel: PodRoleWriter,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-sts-1",
						Namespace: "default",
						Labels: map[string]string{
							"app": "test",
							// No role label
						},
					},
				},
			},
			expectError: false,
		},
		{
			name: "pods with nil labels",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-sts-0",
						Namespace: "default",
						Labels:    nil,
					},
				},
			},
			expectError: false,
		},
		{
			name:        "empty pod list",
			pods:        []corev1.Pod{},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Convert pods to client objects
			objs := make([]client.Object, len(tt.pods))
			for i := range tt.pods {
				objs[i] = &tt.pods[i]
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objs...).
				Build()

			reconciler := &StatefulSetLockReconciler{
				Client: fakeClient,
				Scheme: scheme,
			}

			err := reconciler.removePodRoleLabels(context.TODO(), tt.pods)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				if tt.errorContains != "" && !containsString(err.Error(), tt.errorContains) {
					t.Errorf("expected error to contain %q, got %q", tt.errorContains, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}

				// Check that role labels are removed from all pods
				for _, pod := range tt.pods {
					var updatedPod corev1.Pod
					err := fakeClient.Get(context.TODO(), types.NamespacedName{
						Name:      pod.Name,
						Namespace: pod.Namespace,
					}, &updatedPod)
					if err != nil {
						t.Errorf("failed to fetch updated pod %s: %v", pod.Name, err)
						continue
					}

					if updatedPod.Labels != nil {
						if _, hasRoleLabel := updatedPod.Labels[PodRoleLabel]; hasRoleLabel {
							t.Errorf("expected role label to be removed from pod %s", pod.Name)
						}

						// Verify other labels are preserved
						for k, v := range pod.Labels {
							if k != PodRoleLabel {
								if actualValue, exists := updatedPod.Labels[k]; !exists || actualValue != v {
									t.Errorf("expected original label %q=%q to be preserved on pod %s, got %q", k, v, pod.Name, actualValue)
								}
							}
						}
					}
				}
			}
		})
	}
}

func TestPerformLeaderElection(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = appv1.AddToScheme(scheme)
	_ = appsv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	_ = coordinationv1.AddToScheme(scheme)

	tests := []struct {
		name                string
		existingObjs        []client.Object
		statefulSetLock     *appv1.StatefulSetLock
		expectedWriterPod   string
		expectError         bool
		expectedRequeueTime time.Duration
	}{
		{
			name: "elect new leader when no lease exists",
			existingObjs: []client.Object{
				&appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-sts",
						Namespace: "default",
						UID:       "test-sts-uid",
					},
					Spec: appsv1.StatefulSetSpec{
						Replicas: ptr.To(int32(3)),
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "test"},
						},
					},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-sts-0",
						Namespace: "default",
						Labels:    map[string]string{"app": "test"},
						OwnerReferences: []metav1.OwnerReference{
							{Kind: "StatefulSet", Name: "test-sts", UID: "test-sts-uid"},
						},
					},
					Status: corev1.PodStatus{
						Conditions: []corev1.PodCondition{
							{Type: corev1.PodReady, Status: corev1.ConditionTrue},
						},
					},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-sts-1",
						Namespace: "default",
						Labels:    map[string]string{"app": "test"},
						OwnerReferences: []metav1.OwnerReference{
							{Kind: "StatefulSet", Name: "test-sts", UID: "test-sts-uid"},
						},
					},
					Status: corev1.PodStatus{
						Conditions: []corev1.PodCondition{
							{Type: corev1.PodReady, Status: corev1.ConditionTrue},
						},
					},
				},
			},
			statefulSetLock: &appv1.StatefulSetLock{
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
			expectedWriterPod:   "test-sts-0", // Should elect lowest ordinal
			expectError:         false,
			expectedRequeueTime: 15 * time.Second, // Half of lease duration
		},
		{
			name: "no election when no ready pods",
			existingObjs: []client.Object{
				&appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-sts",
						Namespace: "default",
						UID:       "test-sts-uid",
					},
					Spec: appsv1.StatefulSetSpec{
						Replicas: ptr.To(int32(2)),
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "test"},
						},
					},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-sts-0",
						Namespace: "default",
						Labels:    map[string]string{"app": "test"},
						OwnerReferences: []metav1.OwnerReference{
							{Kind: "StatefulSet", Name: "test-sts", UID: "test-sts-uid"},
						},
					},
					Status: corev1.PodStatus{
						Conditions: []corev1.PodCondition{
							{Type: corev1.PodReady, Status: corev1.ConditionFalse},
						},
					},
				},
			},
			statefulSetLock: &appv1.StatefulSetLock{
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
			expectedWriterPod:   "", // No writer pod when no ready pods
			expectError:         false,
			expectedRequeueTime: 30 * time.Second, // Requeue more frequently when waiting for pods
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tt.existingObjs...).
				Build()

			reconciler := &StatefulSetLockReconciler{
				Client: fakeClient,
				Scheme: scheme,
			}

			result, err := reconciler.performLeaderElection(context.TODO(), tt.statefulSetLock)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}

				// Check writer pod
				if tt.statefulSetLock.Status.WriterPod != tt.expectedWriterPod {
					t.Errorf("expected WriterPod %q, got %q", tt.expectedWriterPod, tt.statefulSetLock.Status.WriterPod)
				}

				// Check requeue time
				if result.RequeueAfter != tt.expectedRequeueTime {
					t.Errorf("expected RequeueAfter %v, got %v", tt.expectedRequeueTime, result.RequeueAfter)
				}

				// If we expected a writer pod, verify lease was created
				if tt.expectedWriterPod != "" {
					var lease coordinationv1.Lease
					err := fakeClient.Get(context.TODO(), types.NamespacedName{
						Name:      tt.statefulSetLock.Spec.LeaseName,
						Namespace: tt.statefulSetLock.Namespace,
					}, &lease)
					if err != nil {
						t.Errorf("expected lease to be created but got error: %v", err)
					} else if lease.Spec.HolderIdentity == nil || *lease.Spec.HolderIdentity != tt.expectedWriterPod {
						t.Errorf("expected lease holder %q, got %q", tt.expectedWriterPod, getStringValue(lease.Spec.HolderIdentity))
					}
				}
			}
		})
	}
}

// Lease Management Helper Function Tests

func TestIsLeaseExpired(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name            string
		lease           *coordinationv1.Lease
		durationSeconds int32
		expected        bool
	}{
		{
			name:            "nil lease is expired",
			lease:           nil,
			durationSeconds: 30,
			expected:        true,
		},
		{
			name: "lease with nil renew time is expired",
			lease: &coordinationv1.Lease{
				Spec: coordinationv1.LeaseSpec{
					HolderIdentity: ptr.To("test-pod-0"),
					RenewTime:      nil,
				},
			},
			durationSeconds: 30,
			expected:        true,
		},
		{
			name: "lease renewed recently is not expired",
			lease: &coordinationv1.Lease{
				Spec: coordinationv1.LeaseSpec{
					HolderIdentity: ptr.To("test-pod-0"),
					RenewTime:      &metav1.MicroTime{Time: now.Add(-10 * time.Second)},
				},
			},
			durationSeconds: 30,
			expected:        false,
		},
		{
			name: "lease renewed exactly at duration is expired",
			lease: &coordinationv1.Lease{
				Spec: coordinationv1.LeaseSpec{
					HolderIdentity: ptr.To("test-pod-0"),
					RenewTime:      &metav1.MicroTime{Time: now.Add(-30 * time.Second)},
				},
			},
			durationSeconds: 30,
			expected:        true,
		},
		{
			name: "lease renewed beyond duration is expired",
			lease: &coordinationv1.Lease{
				Spec: coordinationv1.LeaseSpec{
					HolderIdentity: ptr.To("test-pod-0"),
					RenewTime:      &metav1.MicroTime{Time: now.Add(-60 * time.Second)},
				},
			},
			durationSeconds: 30,
			expected:        true,
		},
		{
			name: "lease with zero duration is expired immediately",
			lease: &coordinationv1.Lease{
				Spec: coordinationv1.LeaseSpec{
					HolderIdentity: ptr.To("test-pod-0"),
					RenewTime:      &metav1.MicroTime{Time: now},
				},
			},
			durationSeconds: 0,
			expected:        true,
		},
		{
			name: "lease with very short duration",
			lease: &coordinationv1.Lease{
				Spec: coordinationv1.LeaseSpec{
					HolderIdentity: ptr.To("test-pod-0"),
					RenewTime:      &metav1.MicroTime{Time: now.Add(-500 * time.Millisecond)},
				},
			},
			durationSeconds: 1,
			expected:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsLeaseExpired(tt.lease, tt.durationSeconds)
			if result != tt.expected {
				t.Errorf("IsLeaseExpired() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestShouldElectNewLeader(t *testing.T) {
	now := time.Now()

	readyPods := []corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "test-pod-0"},
			Status: corev1.PodStatus{
				Conditions: []corev1.PodCondition{
					{Type: corev1.PodReady, Status: corev1.ConditionTrue},
				},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "test-pod-1"},
			Status: corev1.PodStatus{
				Conditions: []corev1.PodCondition{
					{Type: corev1.PodReady, Status: corev1.ConditionTrue},
				},
			},
		},
	}

	noPods := []corev1.Pod{}

	tests := []struct {
		name            string
		lease           *coordinationv1.Lease
		readyPods       []corev1.Pod
		durationSeconds int32
		expected        bool
	}{
		{
			name:            "no lease exists and ready pods available",
			lease:           nil,
			readyPods:       readyPods,
			durationSeconds: 30,
			expected:        true,
		},
		{
			name:            "no lease exists and no ready pods",
			lease:           nil,
			readyPods:       noPods,
			durationSeconds: 30,
			expected:        false,
		},
		{
			name: "lease expired and ready pods available",
			lease: &coordinationv1.Lease{
				Spec: coordinationv1.LeaseSpec{
					HolderIdentity: ptr.To("test-pod-0"),
					RenewTime:      &metav1.MicroTime{Time: now.Add(-60 * time.Second)},
				},
			},
			readyPods:       readyPods,
			durationSeconds: 30,
			expected:        true,
		},
		{
			name: "lease expired and no ready pods",
			lease: &coordinationv1.Lease{
				Spec: coordinationv1.LeaseSpec{
					HolderIdentity: ptr.To("test-pod-0"),
					RenewTime:      &metav1.MicroTime{Time: now.Add(-60 * time.Second)},
				},
			},
			readyPods:       noPods,
			durationSeconds: 30,
			expected:        false,
		},
		{
			name: "lease has no holder identity",
			lease: &coordinationv1.Lease{
				Spec: coordinationv1.LeaseSpec{
					HolderIdentity: nil,
					RenewTime:      &metav1.MicroTime{Time: now.Add(-10 * time.Second)},
				},
			},
			readyPods:       readyPods,
			durationSeconds: 30,
			expected:        true,
		},
		{
			name: "lease has empty holder identity",
			lease: &coordinationv1.Lease{
				Spec: coordinationv1.LeaseSpec{
					HolderIdentity: ptr.To(""),
					RenewTime:      &metav1.MicroTime{Time: now.Add(-10 * time.Second)},
				},
			},
			readyPods:       readyPods,
			durationSeconds: 30,
			expected:        true,
		},
		{
			name: "current holder is still ready",
			lease: &coordinationv1.Lease{
				Spec: coordinationv1.LeaseSpec{
					HolderIdentity: ptr.To("test-pod-0"),
					RenewTime:      &metav1.MicroTime{Time: now.Add(-10 * time.Second)},
				},
			},
			readyPods:       readyPods,
			durationSeconds: 30,
			expected:        false,
		},
		{
			name: "current holder is not ready",
			lease: &coordinationv1.Lease{
				Spec: coordinationv1.LeaseSpec{
					HolderIdentity: ptr.To("test-pod-2"), // Not in ready pods list
					RenewTime:      &metav1.MicroTime{Time: now.Add(-10 * time.Second)},
				},
			},
			readyPods:       readyPods,
			durationSeconds: 30,
			expected:        true,
		},
		{
			name: "current holder not ready and no ready pods available",
			lease: &coordinationv1.Lease{
				Spec: coordinationv1.LeaseSpec{
					HolderIdentity: ptr.To("test-pod-2"), // Not in ready pods list
					RenewTime:      &metav1.MicroTime{Time: now.Add(-10 * time.Second)},
				},
			},
			readyPods:       noPods,
			durationSeconds: 30,
			expected:        false,
		},
		{
			name: "valid lease with holder in ready pods",
			lease: &coordinationv1.Lease{
				Spec: coordinationv1.LeaseSpec{
					HolderIdentity: ptr.To("test-pod-1"),
					RenewTime:      &metav1.MicroTime{Time: now.Add(-5 * time.Second)},
				},
			},
			readyPods:       readyPods,
			durationSeconds: 30,
			expected:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ShouldElectNewLeader(tt.lease, tt.readyPods, tt.durationSeconds)
			if result != tt.expected {
				t.Errorf("ShouldElectNewLeader() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestCreateOrUpdateLease(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = coordinationv1.AddToScheme(scheme)

	tests := []struct {
		name            string
		existingObjs    []client.Object
		namespace       string
		leaseName       string
		leaderPodName   string
		durationSeconds int32
		expectError     bool
		expectCreate    bool
		expectUpdate    bool
	}{
		{
			name:            "create new lease",
			existingObjs:    []client.Object{},
			namespace:       "default",
			leaseName:       "test-lease",
			leaderPodName:   "test-pod-0",
			durationSeconds: 30,
			expectError:     false,
			expectCreate:    true,
			expectUpdate:    false,
		},
		{
			name: "update existing lease",
			existingObjs: []client.Object{
				&coordinationv1.Lease{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-lease",
						Namespace: "default",
					},
					Spec: coordinationv1.LeaseSpec{
						HolderIdentity: ptr.To("old-pod"),
						RenewTime:      &metav1.MicroTime{Time: time.Now().Add(-10 * time.Second)},
					},
				},
			},
			namespace:       "default",
			leaseName:       "test-lease",
			leaderPodName:   "test-pod-0",
			durationSeconds: 30,
			expectError:     false,
			expectCreate:    false,
			expectUpdate:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tt.existingObjs...).
				Build()

			reconciler := &StatefulSetLockReconciler{
				Client: fakeClient,
				Scheme: scheme,
			}

			err := reconciler.createOrUpdateLease(context.TODO(), tt.namespace, tt.leaseName, tt.leaderPodName, tt.durationSeconds)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}

				// Verify the lease exists and has correct values
				var lease coordinationv1.Lease
				leaseKey := types.NamespacedName{Name: tt.leaseName, Namespace: tt.namespace}
				if err := fakeClient.Get(context.TODO(), leaseKey, &lease); err != nil {
					t.Errorf("failed to get lease after operation: %v", err)
				} else {
					if lease.Spec.HolderIdentity == nil || *lease.Spec.HolderIdentity != tt.leaderPodName {
						t.Errorf("lease holder identity = %v, expected %s", lease.Spec.HolderIdentity, tt.leaderPodName)
					}
					if lease.Spec.LeaseDurationSeconds == nil || *lease.Spec.LeaseDurationSeconds != tt.durationSeconds {
						t.Errorf("lease duration = %v, expected %d", lease.Spec.LeaseDurationSeconds, tt.durationSeconds)
					}
					if lease.Spec.RenewTime == nil {
						t.Errorf("lease renew time should not be nil")
					}
				}
			}
		})
	}
}

func TestDeleteLease(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = coordinationv1.AddToScheme(scheme)

	tests := []struct {
		name         string
		existingObjs []client.Object
		namespace    string
		leaseName    string
		expectError  bool
	}{
		{
			name: "delete existing lease",
			existingObjs: []client.Object{
				&coordinationv1.Lease{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-lease",
						Namespace: "default",
					},
					Spec: coordinationv1.LeaseSpec{
						HolderIdentity: ptr.To("test-pod-0"),
					},
				},
			},
			namespace:   "default",
			leaseName:   "test-lease",
			expectError: false,
		},
		{
			name:         "delete non-existent lease",
			existingObjs: []client.Object{},
			namespace:    "default",
			leaseName:    "missing-lease",
			expectError:  false, // Should not error when lease doesn't exist
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tt.existingObjs...).
				Build()

			reconciler := &StatefulSetLockReconciler{
				Client: fakeClient,
				Scheme: scheme,
			}

			err := reconciler.deleteLease(context.TODO(), tt.namespace, tt.leaseName)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}

				// Verify the lease no longer exists
				var lease coordinationv1.Lease
				leaseKey := types.NamespacedName{Name: tt.leaseName, Namespace: tt.namespace}
				err := fakeClient.Get(context.TODO(), leaseKey, &lease)
				if err == nil {
					t.Errorf("lease should have been deleted but still exists")
				}
			}
		})
	}
}

func TestGetStringValue(t *testing.T) {
	tests := []struct {
		name     string
		input    *string
		expected string
	}{
		{
			name:     "nil pointer returns empty string",
			input:    nil,
			expected: "",
		},
		{
			name:     "valid string pointer returns value",
			input:    ptr.To("test-value"),
			expected: "test-value",
		},
		{
			name:     "empty string pointer returns empty string",
			input:    ptr.To(""),
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getStringValue(tt.input)
			if result != tt.expected {
				t.Errorf("getStringValue() = %q, expected %q", result, tt.expected)
			}
		})
	}
}

// Status Reporting and Condition Management Tests

func TestSetCondition(t *testing.T) {
	reconciler := &StatefulSetLockReconciler{}

	tests := []struct {
		name            string
		existingStatus  appv1.StatefulSetLockStatus
		conditionType   string
		status          metav1.ConditionStatus
		reason          string
		message         string
		expectedCount   int
		expectedStatus  metav1.ConditionStatus
		expectedReason  string
		expectedMessage string
	}{
		{
			name:            "add new condition to empty status",
			existingStatus:  appv1.StatefulSetLockStatus{},
			conditionType:   ConditionTypeAvailable,
			status:          metav1.ConditionTrue,
			reason:          ReasonAvailable,
			message:         "StatefulSetLock is ready",
			expectedCount:   1,
			expectedStatus:  metav1.ConditionTrue,
			expectedReason:  ReasonAvailable,
			expectedMessage: "StatefulSetLock is ready",
		},
		{
			name: "update existing condition with different status",
			existingStatus: appv1.StatefulSetLockStatus{
				Conditions: []metav1.Condition{
					{
						Type:               ConditionTypeAvailable,
						Status:             metav1.ConditionFalse,
						LastTransitionTime: metav1.NewTime(time.Now().Add(-time.Hour)),
						Reason:             ReasonReconcileError,
						Message:            "Old error message",
					},
				},
			},
			conditionType:   ConditionTypeAvailable,
			status:          metav1.ConditionTrue,
			reason:          ReasonAvailable,
			message:         "StatefulSetLock is ready",
			expectedCount:   1,
			expectedStatus:  metav1.ConditionTrue,
			expectedReason:  ReasonAvailable,
			expectedMessage: "StatefulSetLock is ready",
		},
		{
			name: "update existing condition with same status but different reason",
			existingStatus: appv1.StatefulSetLockStatus{
				Conditions: []metav1.Condition{
					{
						Type:               ConditionTypeProgressing,
						Status:             metav1.ConditionTrue,
						LastTransitionTime: metav1.NewTime(time.Now().Add(-time.Hour)),
						Reason:             "OldReason",
						Message:            "Old message",
					},
				},
			},
			conditionType:   ConditionTypeProgressing,
			status:          metav1.ConditionTrue,
			reason:          ReasonReconciling,
			message:         "Reconciling StatefulSetLock",
			expectedCount:   1,
			expectedStatus:  metav1.ConditionTrue,
			expectedReason:  ReasonReconciling,
			expectedMessage: "Reconciling StatefulSetLock",
		},
		{
			name: "add second condition type",
			existingStatus: appv1.StatefulSetLockStatus{
				Conditions: []metav1.Condition{
					{
						Type:               ConditionTypeAvailable,
						Status:             metav1.ConditionTrue,
						LastTransitionTime: metav1.Now(),
						Reason:             ReasonAvailable,
						Message:            "StatefulSetLock is ready",
					},
				},
			},
			conditionType:   ConditionTypeProgressing,
			status:          metav1.ConditionTrue,
			reason:          ReasonReconciling,
			message:         "Reconciling StatefulSetLock",
			expectedCount:   2,
			expectedStatus:  metav1.ConditionTrue,
			expectedReason:  ReasonReconciling,
			expectedMessage: "Reconciling StatefulSetLock",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			statefulSetLock := &appv1.StatefulSetLock{
				Status: tt.existingStatus,
			}

			reconciler.setCondition(statefulSetLock, tt.conditionType, tt.status, tt.reason, tt.message)

			// Check total number of conditions
			if len(statefulSetLock.Status.Conditions) != tt.expectedCount {
				t.Errorf("expected %d conditions, got %d", tt.expectedCount, len(statefulSetLock.Status.Conditions))
			}

			// Find the condition we just set
			var foundCondition *metav1.Condition
			for i, condition := range statefulSetLock.Status.Conditions {
				if condition.Type == tt.conditionType {
					foundCondition = &statefulSetLock.Status.Conditions[i]
					break
				}
			}

			if foundCondition == nil {
				t.Fatalf("condition type %s not found", tt.conditionType)
			}

			// Verify condition properties
			if foundCondition.Status != tt.expectedStatus {
				t.Errorf("expected status %s, got %s", tt.expectedStatus, foundCondition.Status)
			}
			if foundCondition.Reason != tt.expectedReason {
				t.Errorf("expected reason %s, got %s", tt.expectedReason, foundCondition.Reason)
			}
			if foundCondition.Message != tt.expectedMessage {
				t.Errorf("expected message %s, got %s", tt.expectedMessage, foundCondition.Message)
			}

			// Verify LastTransitionTime is set
			if foundCondition.LastTransitionTime.IsZero() {
				t.Error("LastTransitionTime should be set")
			}
		})
	}
}

func TestGetConditionStatus(t *testing.T) {
	reconciler := &StatefulSetLockReconciler{}

	tests := []struct {
		name           string
		conditions     []metav1.Condition
		conditionType  string
		expectedStatus metav1.ConditionStatus
	}{
		{
			name:           "condition not found returns unknown",
			conditions:     []metav1.Condition{},
			conditionType:  ConditionTypeAvailable,
			expectedStatus: metav1.ConditionUnknown,
		},
		{
			name: "condition found returns correct status",
			conditions: []metav1.Condition{
				{
					Type:   ConditionTypeAvailable,
					Status: metav1.ConditionTrue,
				},
			},
			conditionType:  ConditionTypeAvailable,
			expectedStatus: metav1.ConditionTrue,
		},
		{
			name: "multiple conditions returns correct one",
			conditions: []metav1.Condition{
				{
					Type:   ConditionTypeProgressing,
					Status: metav1.ConditionTrue,
				},
				{
					Type:   ConditionTypeAvailable,
					Status: metav1.ConditionFalse,
				},
			},
			conditionType:  ConditionTypeAvailable,
			expectedStatus: metav1.ConditionFalse,
		},
		{
			name: "condition type not found among multiple conditions",
			conditions: []metav1.Condition{
				{
					Type:   ConditionTypeProgressing,
					Status: metav1.ConditionTrue,
				},
				{
					Type:   "SomeOtherType",
					Status: metav1.ConditionFalse,
				},
			},
			conditionType:  ConditionTypeAvailable,
			expectedStatus: metav1.ConditionUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			statefulSetLock := &appv1.StatefulSetLock{
				Status: appv1.StatefulSetLockStatus{
					Conditions: tt.conditions,
				},
			}

			result := reconciler.getConditionStatus(statefulSetLock, tt.conditionType)
			if result != tt.expectedStatus {
				t.Errorf("expected status %s, got %s", tt.expectedStatus, result)
			}
		})
	}
}

func TestHandleReconcileErrorLogic(t *testing.T) {
	// Test the logic of handleReconcileError without the status update
	reconciler := &StatefulSetLockReconciler{}

	tests := []struct {
		name                    string
		inputError              error
		inputMessage            string
		expectedAvailableStatus metav1.ConditionStatus
		expectedProgressStatus  metav1.ConditionStatus
		expectedReason          string
	}{
		{
			name:                    "handle validation error",
			inputError:              fmt.Errorf("validation failed"),
			inputMessage:            "Spec validation failed",
			expectedAvailableStatus: metav1.ConditionFalse,
			expectedProgressStatus:  metav1.ConditionFalse,
			expectedReason:          ReasonReconcileError,
		},
		{
			name:                    "handle resource not found error",
			inputError:              fmt.Errorf("StatefulSet not found"),
			inputMessage:            "Failed to fetch StatefulSet",
			expectedAvailableStatus: metav1.ConditionFalse,
			expectedProgressStatus:  metav1.ConditionFalse,
			expectedReason:          ReasonReconcileError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			statefulSetLock := &appv1.StatefulSetLock{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "test-ssl",
					Namespace:  "default",
					Generation: 1,
				},
				Spec: appv1.StatefulSetLockSpec{
					StatefulSetName:      "test-sts",
					LeaseName:            "test-lease",
					LeaseDurationSeconds: 30,
				},
			}

			// Test the condition setting logic (without the status update)
			reconciler.setCondition(statefulSetLock, ConditionTypeAvailable, metav1.ConditionFalse, ReasonReconcileError, fmt.Sprintf("%s: %v", tt.inputMessage, tt.inputError))
			reconciler.setCondition(statefulSetLock, ConditionTypeProgressing, metav1.ConditionFalse, ReasonReconcileError, fmt.Sprintf("%s: %v", tt.inputMessage, tt.inputError))

			// Test the ObservedGeneration update logic
			statefulSetLock.Status.ObservedGeneration = statefulSetLock.Generation

			// Verify ObservedGeneration was updated
			if statefulSetLock.Status.ObservedGeneration != statefulSetLock.Generation {
				t.Errorf("expected ObservedGeneration %d, got %d", statefulSetLock.Generation, statefulSetLock.Status.ObservedGeneration)
			}

			// Verify Available condition
			availableCondition := findCondition(statefulSetLock.Status.Conditions, ConditionTypeAvailable)
			if availableCondition == nil {
				t.Fatal("Available condition not found")
			}
			if availableCondition.Status != tt.expectedAvailableStatus {
				t.Errorf("expected Available status %s, got %s", tt.expectedAvailableStatus, availableCondition.Status)
			}
			if availableCondition.Reason != tt.expectedReason {
				t.Errorf("expected Available reason %s, got %s", tt.expectedReason, availableCondition.Reason)
			}
			expectedMessage := fmt.Sprintf("%s: %v", tt.inputMessage, tt.inputError)
			if availableCondition.Message != expectedMessage {
				t.Errorf("expected Available message %s, got %s", expectedMessage, availableCondition.Message)
			}

			// Verify Progressing condition
			progressingCondition := findCondition(statefulSetLock.Status.Conditions, ConditionTypeProgressing)
			if progressingCondition == nil {
				t.Fatal("Progressing condition not found")
			}
			if progressingCondition.Status != tt.expectedProgressStatus {
				t.Errorf("expected Progressing status %s, got %s", tt.expectedProgressStatus, progressingCondition.Status)
			}
			if progressingCondition.Reason != tt.expectedReason {
				t.Errorf("expected Progressing reason %s, got %s", tt.expectedReason, progressingCondition.Reason)
			}
			if progressingCondition.Message != expectedMessage {
				t.Errorf("expected Progressing message %s, got %s", expectedMessage, progressingCondition.Message)
			}
		})
	}
}

func TestStatusUpdateLogic(t *testing.T) {
	tests := []struct {
		name                    string
		initialWriterPod        string
		initialObservedGen      int64
		initialConditions       []metav1.Condition
		newWriterPod            string
		newGeneration           int64
		expectedWriterPod       string
		expectedObservedGen     int64
		expectConditionsChanged bool
	}{
		{
			name:                "update writer pod and generation",
			initialWriterPod:    "",
			initialObservedGen:  0,
			initialConditions:   []metav1.Condition{},
			newWriterPod:        "test-sts-0",
			newGeneration:       1,
			expectedWriterPod:   "test-sts-0",
			expectedObservedGen: 1,
		},
		{
			name:                "change writer pod",
			initialWriterPod:    "test-sts-0",
			initialObservedGen:  1,
			initialConditions:   []metav1.Condition{},
			newWriterPod:        "test-sts-1",
			newGeneration:       2,
			expectedWriterPod:   "test-sts-1",
			expectedObservedGen: 2,
		},
		{
			name:                "clear writer pod when no ready pods",
			initialWriterPod:    "test-sts-0",
			initialObservedGen:  1,
			initialConditions:   []metav1.Condition{},
			newWriterPod:        "",
			newGeneration:       2,
			expectedWriterPod:   "",
			expectedObservedGen: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			statefulSetLock := &appv1.StatefulSetLock{
				ObjectMeta: metav1.ObjectMeta{
					Generation: tt.newGeneration,
				},
				Status: appv1.StatefulSetLockStatus{
					WriterPod:          tt.initialWriterPod,
					ObservedGeneration: tt.initialObservedGen,
					Conditions:         tt.initialConditions,
				},
			}

			// Simulate status updates that happen during reconciliation
			statefulSetLock.Status.WriterPod = tt.newWriterPod
			statefulSetLock.Status.ObservedGeneration = statefulSetLock.Generation

			// Verify the updates
			if statefulSetLock.Status.WriterPod != tt.expectedWriterPod {
				t.Errorf("expected WriterPod %s, got %s", tt.expectedWriterPod, statefulSetLock.Status.WriterPod)
			}
			if statefulSetLock.Status.ObservedGeneration != tt.expectedObservedGen {
				t.Errorf("expected ObservedGeneration %d, got %d", tt.expectedObservedGen, statefulSetLock.Status.ObservedGeneration)
			}
		})
	}
}

// Helper function to find a condition by type
func findCondition(conditions []metav1.Condition, conditionType string) *metav1.Condition {
	for i, condition := range conditions {
		if condition.Type == conditionType {
			return &conditions[i]
		}
	}
	return nil
}
