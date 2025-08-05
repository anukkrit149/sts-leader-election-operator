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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	appv1 "github.com/anukkrit/statefulset-leader-election-operator/api/v1"
)

var _ = Describe("StatefulSetLock Controller", func() {
	Context("Happy Path Integration Tests", func() {
		var (
			ctx                context.Context
			controllerReconciler *StatefulSetLockReconciler
			testNamespace      string
			statefulSetName    string
			leaseName          string
			statefulSetLockName string
		)

		BeforeEach(func() {
			ctx = context.Background()
			controllerReconciler = &StatefulSetLockReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			testNamespace = "default"
			// Make resource names unique per test to avoid conflicts
			testSuffix := fmt.Sprintf("%d-%d", GinkgoRandomSeed(), time.Now().UnixNano())
			statefulSetName = fmt.Sprintf("test-sts-%s", testSuffix)
			leaseName = fmt.Sprintf("test-lease-%s", testSuffix)
			statefulSetLockName = fmt.Sprintf("test-ssl-%s", testSuffix)
		})

		AfterEach(func() {
			// Clean up all resources created during the test
			By("Cleaning up StatefulSetLock")
			ssl := &appv1.StatefulSetLock{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: statefulSetLockName, Namespace: testNamespace}, ssl); err == nil {
				Expect(k8sClient.Delete(ctx, ssl)).To(Succeed())
			}

			By("Cleaning up StatefulSet")
			sts := &appsv1.StatefulSet{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: statefulSetName, Namespace: testNamespace}, sts); err == nil {
				Expect(k8sClient.Delete(ctx, sts)).To(Succeed())
			}

			By("Cleaning up Pods")
			podList := &corev1.PodList{}
			if err := k8sClient.List(ctx, podList, client.InNamespace(testNamespace), client.MatchingLabels{"app": "test"}); err == nil {
				for _, pod := range podList.Items {
					Expect(k8sClient.Delete(ctx, &pod)).To(Succeed())
				}
			}

			By("Cleaning up Lease")
			lease := &coordinationv1.Lease{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: leaseName, Namespace: testNamespace}, lease); err == nil {
				Expect(k8sClient.Delete(ctx, lease)).To(Succeed())
			}
		})

		It("should create StatefulSetLock with lease and pod labeling", func() {
			By("Creating a StatefulSet with 3 replicas")
			statefulSet := createTestStatefulSet(statefulSetName, testNamespace, 3)
			Expect(k8sClient.Create(ctx, statefulSet)).To(Succeed())

			By("Creating pods for the StatefulSet")
			pods := createTestPods(statefulSetName, testNamespace, 3, 3, statefulSet) // 3 pods, all ready
			for i, pod := range pods {
				Expect(k8sClient.Create(ctx, &pod)).To(Succeed())
				
				// Update the status separately (required for envtest)
				createdPod := &corev1.Pod{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: pod.Name, Namespace: testNamespace}, createdPod)).To(Succeed())
				
				// Set the status on the fetched pod
				isReady := i < 3 // All 3 pods should be ready
				createdPod.Status.Phase = func() corev1.PodPhase {
					if isReady {
						return corev1.PodRunning
					}
					return corev1.PodPending
				}()
				createdPod.Status.Conditions = []corev1.PodCondition{
					{
						Type: corev1.PodReady,
						Status: func() corev1.ConditionStatus {
							if isReady {
								return corev1.ConditionTrue
							}
							return corev1.ConditionFalse
						}(),
					},
				}
				
				Expect(k8sClient.Status().Update(ctx, createdPod)).To(Succeed())
			}



			By("Creating a StatefulSetLock")
			statefulSetLock := &appv1.StatefulSetLock{
				ObjectMeta: metav1.ObjectMeta{
					Name:      statefulSetLockName,
					Namespace: testNamespace,
				},
				Spec: appv1.StatefulSetLockSpec{
					StatefulSetName:      statefulSetName,
					LeaseName:            leaseName,
					LeaseDurationSeconds: 30,
				},
			}
			Expect(k8sClient.Create(ctx, statefulSetLock)).To(Succeed())

			By("Reconciling the StatefulSetLock")
			// First reconciliation adds finalizer
			result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: statefulSetLockName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())
			
			// If requeue is requested, run reconciliation again
			if result.Requeue || result.RequeueAfter > 0 {
				_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: types.NamespacedName{Name: statefulSetLockName, Namespace: testNamespace},
				})
				Expect(err).NotTo(HaveOccurred())
			}

			By("Verifying that a lease was created")
			lease := &coordinationv1.Lease{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: leaseName, Namespace: testNamespace}, lease)
			}, "10s", "1s").Should(Succeed())

			By("Verifying that the lease has the correct holder identity")
			Expect(lease.Spec.HolderIdentity).NotTo(BeNil())
			Expect(*lease.Spec.HolderIdentity).To(Equal(fmt.Sprintf("%s-0", statefulSetName))) // Lowest ordinal should be leader

			By("Verifying that pods are labeled correctly")
			Eventually(func() bool {
				// Check leader pod has writer label
				leaderPodName := fmt.Sprintf("%s-0", statefulSetName)
				leaderPod := &corev1.Pod{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: leaderPodName, Namespace: testNamespace}, leaderPod); err != nil {
					return false
				}
				if leaderPod.Labels["sts-role"] != "writer" {
					return false
				}

				// Check non-leader pods have reader labels
				for i := 1; i < 3; i++ {
					pod := &corev1.Pod{}
					podName := fmt.Sprintf("%s-%d", statefulSetName, i)
					if err := k8sClient.Get(ctx, types.NamespacedName{Name: podName, Namespace: testNamespace}, pod); err != nil {
						return false
					}
					if pod.Labels["sts-role"] != "reader" {
						return false
					}
				}
				return true
			}, "10s", "1s").Should(BeTrue())

			By("Verifying that the StatefulSetLock status is updated")
			Eventually(func() bool {
				updatedSSL := &appv1.StatefulSetLock{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: statefulSetLockName, Namespace: testNamespace}, updatedSSL); err != nil {
					return false
				}
				expectedLeaderName := fmt.Sprintf("%s-0", statefulSetName)
				return updatedSSL.Status.WriterPod == expectedLeaderName && updatedSSL.Status.ObservedGeneration == updatedSSL.Generation
			}, "10s", "1s").Should(BeTrue())
		})

		It("should select the correct pod as leader (lowest ordinal)", func() {
			By("Creating a StatefulSet with 5 replicas")
			statefulSet := createTestStatefulSet(statefulSetName, testNamespace, 5)
			Expect(k8sClient.Create(ctx, statefulSet)).To(Succeed())

			By("Creating pods with mixed readiness - only pods 2, 3, 4 are ready")
			podReadiness := []bool{false, false, true, true, true} // Pods 0,1 not ready; 2,3,4 ready
			for i := 0; i < 5; i++ {
				podName := fmt.Sprintf("%s-%d", statefulSetName, i)
				pod := createPodWithReadiness(podName, testNamespace, podReadiness[i], statefulSet)
				Expect(k8sClient.Create(ctx, &pod)).To(Succeed())
				
				// Update the status separately (required for envtest)
				createdPod := &corev1.Pod{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: podName, Namespace: testNamespace}, createdPod)).To(Succeed())
				
				createdPod.Status.Phase = func() corev1.PodPhase {
					if podReadiness[i] {
						return corev1.PodRunning
					}
					return corev1.PodPending
				}()
				createdPod.Status.Conditions = []corev1.PodCondition{
					{
						Type: corev1.PodReady,
						Status: func() corev1.ConditionStatus {
							if podReadiness[i] {
								return corev1.ConditionTrue
							}
							return corev1.ConditionFalse
						}(),
					},
				}
				
				Expect(k8sClient.Status().Update(ctx, createdPod)).To(Succeed())
			}

			By("Creating a StatefulSetLock")
			statefulSetLock := &appv1.StatefulSetLock{
				ObjectMeta: metav1.ObjectMeta{
					Name:      statefulSetLockName,
					Namespace: testNamespace,
				},
				Spec: appv1.StatefulSetLockSpec{
					StatefulSetName:      statefulSetName,
					LeaseName:            leaseName,
					LeaseDurationSeconds: 30,
				},
			}
			Expect(k8sClient.Create(ctx, statefulSetLock)).To(Succeed())

			By("Reconciling the StatefulSetLock")
			// First reconciliation adds finalizer
			result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: statefulSetLockName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())
			
			// If requeue is requested, run reconciliation again
			if result.Requeue || result.RequeueAfter > 0 {
				_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: types.NamespacedName{Name: statefulSetLockName, Namespace: testNamespace},
				})
				Expect(err).NotTo(HaveOccurred())
			}

			By("Verifying that the lowest ordinal ready pod is selected as leader")
			expectedLeaderName := fmt.Sprintf("%s-2", statefulSetName) // Pod 2 should be leader since 0,1 are not ready
			lease := &coordinationv1.Lease{}
			Eventually(func() string {
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: leaseName, Namespace: testNamespace}, lease); err != nil {
					return ""
				}
				if lease.Spec.HolderIdentity == nil {
					return ""
				}
				return *lease.Spec.HolderIdentity
			}, "10s", "1s").Should(Equal(expectedLeaderName))

			By("Verifying that only the leader pod is labeled as writer")
			Eventually(func() bool {
				leaderPod := &corev1.Pod{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: expectedLeaderName, Namespace: testNamespace}, leaderPod); err != nil {
					return false
				}
				return leaderPod.Labels["sts-role"] == "writer"
			}, "10s", "1s").Should(BeTrue())

			By("Verifying that other ready pods are labeled as readers")
			Eventually(func() bool {
				for i := 3; i <= 4; i++ {
					podName := fmt.Sprintf("%s-%d", statefulSetName, i)
					pod := &corev1.Pod{}
					if err := k8sClient.Get(ctx, types.NamespacedName{Name: podName, Namespace: testNamespace}, pod); err != nil {
						return false
					}
					if pod.Labels["sts-role"] != "reader" {
						return false
					}
				}
				return true
			}, "10s", "1s").Should(BeTrue())
		})

		It("should update status to reflect current leader correctly", func() {
			By("Creating a StatefulSet with 2 replicas")
			statefulSet := createTestStatefulSet(statefulSetName, testNamespace, 2)
			Expect(k8sClient.Create(ctx, statefulSet)).To(Succeed())

			By("Creating ready pods")
			pods := createTestPods(statefulSetName, testNamespace, 2, 2, statefulSet)
			for i, pod := range pods {
				Expect(k8sClient.Create(ctx, &pod)).To(Succeed())
				
				// Update the status separately (required for envtest)
				createdPod := &corev1.Pod{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: pod.Name, Namespace: testNamespace}, createdPod)).To(Succeed())
				
				// Set the status on the fetched pod
				podReady := i < 2 // Both pods should be ready
				createdPod.Status.Phase = func() corev1.PodPhase {
					if podReady {
						return corev1.PodRunning
					}
					return corev1.PodPending
				}()
				createdPod.Status.Conditions = []corev1.PodCondition{
					{
						Type: corev1.PodReady,
						Status: func() corev1.ConditionStatus {
							if podReady {
								return corev1.ConditionTrue
							}
							return corev1.ConditionFalse
						}(),
					},
				}
				
				Expect(k8sClient.Status().Update(ctx, createdPod)).To(Succeed())
			}

			By("Creating a StatefulSetLock")
			statefulSetLock := &appv1.StatefulSetLock{
				ObjectMeta: metav1.ObjectMeta{
					Name:      statefulSetLockName,
					Namespace: testNamespace,
				},
				Spec: appv1.StatefulSetLockSpec{
					StatefulSetName:      statefulSetName,
					LeaseName:            leaseName,
					LeaseDurationSeconds: 30,
				},
			}
			Expect(k8sClient.Create(ctx, statefulSetLock)).To(Succeed())

			By("Reconciling the StatefulSetLock")
			// First reconciliation adds finalizer
			result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: statefulSetLockName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())
			
			// If requeue is requested, run reconciliation again
			if result.Requeue || result.RequeueAfter > 0 {
				_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: types.NamespacedName{Name: statefulSetLockName, Namespace: testNamespace},
				})
				Expect(err).NotTo(HaveOccurred())
			}

			By("Verifying that the status reflects the correct leader")
			expectedLeaderName := fmt.Sprintf("%s-0", statefulSetName)
			Eventually(func() bool {
				updatedSSL := &appv1.StatefulSetLock{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: statefulSetLockName, Namespace: testNamespace}, updatedSSL); err != nil {
					return false
				}
				return updatedSSL.Status.WriterPod == expectedLeaderName
			}, "10s", "1s").Should(BeTrue())

			By("Verifying that the observedGeneration is updated")
			Eventually(func() bool {
				updatedSSL := &appv1.StatefulSetLock{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: statefulSetLockName, Namespace: testNamespace}, updatedSSL); err != nil {
					return false
				}
				return updatedSSL.Status.ObservedGeneration == updatedSSL.Generation
			}, "10s", "1s").Should(BeTrue())

			By("Verifying that conditions are set correctly")
			Eventually(func() bool {
				updatedSSL := &appv1.StatefulSetLock{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: statefulSetLockName, Namespace: testNamespace}, updatedSSL); err != nil {
					return false
				}
				
				// Check for Available condition
				availableCondition := findCondition(updatedSSL.Status.Conditions, "Available")
				if availableCondition == nil || availableCondition.Status != metav1.ConditionTrue {
					return false
				}
				
				return true
			}, "10s", "1s").Should(BeTrue())
		})
	})

	Context("Failover Scenario Integration Tests", func() {
		var (
			ctx                context.Context
			controllerReconciler *StatefulSetLockReconciler
			testNamespace      string
			statefulSetName    string
			leaseName          string
			statefulSetLockName string
		)

		BeforeEach(func() {
			ctx = context.Background()
			controllerReconciler = &StatefulSetLockReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			testNamespace = "default"
			// Make resource names unique per test to avoid conflicts
			testSuffix := fmt.Sprintf("%d-%d", GinkgoRandomSeed(), time.Now().UnixNano())
			statefulSetName = fmt.Sprintf("test-sts-%s", testSuffix)
			leaseName = fmt.Sprintf("test-lease-%s", testSuffix)
			statefulSetLockName = fmt.Sprintf("test-ssl-%s", testSuffix)
		})

		AfterEach(func() {
			// Clean up all resources created during the test
			By("Cleaning up StatefulSetLock")
			ssl := &appv1.StatefulSetLock{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: statefulSetLockName, Namespace: testNamespace}, ssl); err == nil {
				Expect(k8sClient.Delete(ctx, ssl)).To(Succeed())
			}

			By("Cleaning up StatefulSet")
			sts := &appsv1.StatefulSet{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: statefulSetName, Namespace: testNamespace}, sts); err == nil {
				Expect(k8sClient.Delete(ctx, sts)).To(Succeed())
			}

			By("Cleaning up Pods")
			podList := &corev1.PodList{}
			if err := k8sClient.List(ctx, podList, client.InNamespace(testNamespace), client.MatchingLabels{"app": "test"}); err == nil {
				for _, pod := range podList.Items {
					Expect(k8sClient.Delete(ctx, &pod)).To(Succeed())
				}
			}

			By("Cleaning up Lease")
			lease := &coordinationv1.Lease{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: leaseName, Namespace: testNamespace}, lease); err == nil {
				Expect(k8sClient.Delete(ctx, lease)).To(Succeed())
			}
		})

		It("should elect new leader when current leader pod becomes NotReady", func() {
			By("Creating a StatefulSet with 3 replicas")
			statefulSet := createTestStatefulSet(statefulSetName, testNamespace, 3)
			Expect(k8sClient.Create(ctx, statefulSet)).To(Succeed())

			By("Creating pods for the StatefulSet - all initially ready")
			pods := createTestPods(statefulSetName, testNamespace, 3, 3, statefulSet)
			for _, pod := range pods {
				Expect(k8sClient.Create(ctx, &pod)).To(Succeed())
				
				// Update the status separately (required for envtest)
				createdPod := &corev1.Pod{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: pod.Name, Namespace: testNamespace}, createdPod)).To(Succeed())
				
				// All pods start as ready
				createdPod.Status.Phase = corev1.PodRunning
				createdPod.Status.Conditions = []corev1.PodCondition{
					{
						Type:   corev1.PodReady,
						Status: corev1.ConditionTrue,
					},
				}
				
				Expect(k8sClient.Status().Update(ctx, createdPod)).To(Succeed())
			}

			By("Creating a StatefulSetLock with short lease duration for faster testing")
			statefulSetLock := &appv1.StatefulSetLock{
				ObjectMeta: metav1.ObjectMeta{
					Name:      statefulSetLockName,
					Namespace: testNamespace,
				},
				Spec: appv1.StatefulSetLockSpec{
					StatefulSetName:      statefulSetName,
					LeaseName:            leaseName,
					LeaseDurationSeconds: 5, // Short duration for faster test
				},
			}
			Expect(k8sClient.Create(ctx, statefulSetLock)).To(Succeed())

			By("Reconciling to establish initial leader")
			result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: statefulSetLockName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())
			
			if result.Requeue || result.RequeueAfter > 0 {
				_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: types.NamespacedName{Name: statefulSetLockName, Namespace: testNamespace},
				})
				Expect(err).NotTo(HaveOccurred())
			}

			By("Verifying initial leader is pod-0")
			initialLeaderName := fmt.Sprintf("%s-0", statefulSetName)
			lease := &coordinationv1.Lease{}
			Eventually(func() string {
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: leaseName, Namespace: testNamespace}, lease); err != nil {
					return ""
				}
				if lease.Spec.HolderIdentity == nil {
					return ""
				}
				return *lease.Spec.HolderIdentity
			}, "10s", "1s").Should(Equal(initialLeaderName))

			By("Verifying initial leader pod is labeled as writer")
			Eventually(func() string {
				leaderPod := &corev1.Pod{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: initialLeaderName, Namespace: testNamespace}, leaderPod); err != nil {
					return ""
				}
				return leaderPod.Labels["sts-role"]
			}, "10s", "1s").Should(Equal("writer"))

			By("Simulating leader pod becoming NotReady")
			leaderPod := &corev1.Pod{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: initialLeaderName, Namespace: testNamespace}, leaderPod)).To(Succeed())
			
			// Update pod status to NotReady
			leaderPod.Status.Phase = corev1.PodPending
			leaderPod.Status.Conditions = []corev1.PodCondition{
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionFalse,
					Reason: "PodNotReady",
				},
			}
			Expect(k8sClient.Status().Update(ctx, leaderPod)).To(Succeed())

			By("Waiting for lease to expire and triggering reconciliation")
			// Wait for lease to expire (5 seconds + buffer)
			time.Sleep(6 * time.Second)
			
			// Trigger reconciliation
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: statefulSetLockName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying new leader is elected (should be pod-1)")
			newLeaderName := fmt.Sprintf("%s-1", statefulSetName)
			Eventually(func() string {
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: leaseName, Namespace: testNamespace}, lease); err != nil {
					return ""
				}
				if lease.Spec.HolderIdentity == nil {
					return ""
				}
				return *lease.Spec.HolderIdentity
			}, "10s", "1s").Should(Equal(newLeaderName))

			By("Verifying pod labels are updated during leadership transition")
			Eventually(func() bool {
				// Check new leader has writer label
				newLeaderPod := &corev1.Pod{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: newLeaderName, Namespace: testNamespace}, newLeaderPod); err != nil {
					return false
				}
				if newLeaderPod.Labels["sts-role"] != "writer" {
					return false
				}

				// Check remaining pod has reader label
				remainingPodName := fmt.Sprintf("%s-2", statefulSetName)
				remainingPod := &corev1.Pod{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: remainingPodName, Namespace: testNamespace}, remainingPod); err != nil {
					return false
				}
				if remainingPod.Labels["sts-role"] != "reader" {
					return false
				}

				// Note: The old leader pod (which is NotReady) retains its previous label
				// since the controller only updates labels for ready pods
				// This is the expected behavior

				return true
			}, "10s", "1s").Should(BeTrue())

			By("Verifying StatefulSetLock status reflects new leader after failover")
			Eventually(func() string {
				updatedSSL := &appv1.StatefulSetLock{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: statefulSetLockName, Namespace: testNamespace}, updatedSSL); err != nil {
					return ""
				}
				return updatedSSL.Status.WriterPod
			}, "10s", "1s").Should(Equal(newLeaderName))
		})

		It("should handle multiple failovers correctly", func() {
			By("Creating a StatefulSet with 4 replicas")
			statefulSet := createTestStatefulSet(statefulSetName, testNamespace, 4)
			Expect(k8sClient.Create(ctx, statefulSet)).To(Succeed())

			By("Creating pods for the StatefulSet - all initially ready")
			pods := createTestPods(statefulSetName, testNamespace, 4, 4, statefulSet)
			for _, pod := range pods {
				Expect(k8sClient.Create(ctx, &pod)).To(Succeed())
				
				// Update the status separately (required for envtest)
				createdPod := &corev1.Pod{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: pod.Name, Namespace: testNamespace}, createdPod)).To(Succeed())
				
				// All pods start as ready
				createdPod.Status.Phase = corev1.PodRunning
				createdPod.Status.Conditions = []corev1.PodCondition{
					{
						Type:   corev1.PodReady,
						Status: corev1.ConditionTrue,
					},
				}
				
				Expect(k8sClient.Status().Update(ctx, createdPod)).To(Succeed())
			}

			By("Creating a StatefulSetLock with short lease duration")
			statefulSetLock := &appv1.StatefulSetLock{
				ObjectMeta: metav1.ObjectMeta{
					Name:      statefulSetLockName,
					Namespace: testNamespace,
				},
				Spec: appv1.StatefulSetLockSpec{
					StatefulSetName:      statefulSetName,
					LeaseName:            leaseName,
					LeaseDurationSeconds: 3, // Very short for faster testing
				},
			}
			Expect(k8sClient.Create(ctx, statefulSetLock)).To(Succeed())

			By("Establishing initial leader (pod-0)")
			result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: statefulSetLockName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())
			
			if result.Requeue || result.RequeueAfter > 0 {
				_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: types.NamespacedName{Name: statefulSetLockName, Namespace: testNamespace},
				})
				Expect(err).NotTo(HaveOccurred())
			}

			// Verify initial leader
			firstLeaderName := fmt.Sprintf("%s-0", statefulSetName)
			lease := &coordinationv1.Lease{}
			Eventually(func() string {
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: leaseName, Namespace: testNamespace}, lease); err != nil {
					return ""
				}
				if lease.Spec.HolderIdentity == nil {
					return ""
				}
				return *lease.Spec.HolderIdentity
			}, "10s", "1s").Should(Equal(firstLeaderName))

			By("First failover: Making pod-0 NotReady")
			pod0 := &corev1.Pod{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: firstLeaderName, Namespace: testNamespace}, pod0)).To(Succeed())
			pod0.Status.Phase = corev1.PodPending
			pod0.Status.Conditions = []corev1.PodCondition{
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionFalse,
				},
			}
			Expect(k8sClient.Status().Update(ctx, pod0)).To(Succeed())

			// Wait for lease expiration and reconcile
			time.Sleep(4 * time.Second)
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: statefulSetLockName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify new leader is pod-1
			secondLeaderName := fmt.Sprintf("%s-1", statefulSetName)
			Eventually(func() string {
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: leaseName, Namespace: testNamespace}, lease); err != nil {
					return ""
				}
				if lease.Spec.HolderIdentity == nil {
					return ""
				}
				return *lease.Spec.HolderIdentity
			}, "10s", "1s").Should(Equal(secondLeaderName))

			By("Second failover: Making pod-1 NotReady")
			pod1 := &corev1.Pod{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: secondLeaderName, Namespace: testNamespace}, pod1)).To(Succeed())
			pod1.Status.Phase = corev1.PodPending
			pod1.Status.Conditions = []corev1.PodCondition{
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionFalse,
				},
			}
			Expect(k8sClient.Status().Update(ctx, pod1)).To(Succeed())

			// Wait for lease expiration and reconcile
			time.Sleep(4 * time.Second)
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: statefulSetLockName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify new leader is pod-2
			thirdLeaderName := fmt.Sprintf("%s-2", statefulSetName)
			Eventually(func() string {
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: leaseName, Namespace: testNamespace}, lease); err != nil {
					return ""
				}
				if lease.Spec.HolderIdentity == nil {
					return ""
				}
				return *lease.Spec.HolderIdentity
			}, "10s", "1s").Should(Equal(thirdLeaderName))

			By("Verifying final state has correct labels and status")
			Eventually(func() bool {
				// Check current leader has writer label
				currentLeaderPod := &corev1.Pod{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: thirdLeaderName, Namespace: testNamespace}, currentLeaderPod); err != nil {
					return false
				}
				if currentLeaderPod.Labels["sts-role"] != "writer" {
					return false
				}

				// Check remaining ready pod has reader label
				pod3Name := fmt.Sprintf("%s-3", statefulSetName)
				pod3 := &corev1.Pod{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: pod3Name, Namespace: testNamespace}, pod3); err != nil {
					return false
				}
				if pod3.Labels["sts-role"] != "reader" {
					return false
				}

				// Check status reflects current leader
				updatedSSL := &appv1.StatefulSetLock{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: statefulSetLockName, Namespace: testNamespace}, updatedSSL); err != nil {
					return false
				}
				if updatedSSL.Status.WriterPod != thirdLeaderName {
					return false
				}

				return true
			}, "10s", "1s").Should(BeTrue())
		})

		It("should handle leader recovery scenarios", func() {
			By("Creating a StatefulSet with 3 replicas")
			statefulSet := createTestStatefulSet(statefulSetName, testNamespace, 3)
			Expect(k8sClient.Create(ctx, statefulSet)).To(Succeed())

			By("Creating pods - initially only pod-1 and pod-2 are ready")
			pods := createTestPods(statefulSetName, testNamespace, 3, 2, statefulSet)
			for i, pod := range pods {
				Expect(k8sClient.Create(ctx, &pod)).To(Succeed())
				
				// Update the status separately (required for envtest)
				createdPod := &corev1.Pod{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: pod.Name, Namespace: testNamespace}, createdPod)).To(Succeed())
				
				// Pod-0 is not ready, pod-1 and pod-2 are ready
				isReady := i >= 1
				createdPod.Status.Phase = func() corev1.PodPhase {
					if isReady {
						return corev1.PodRunning
					}
					return corev1.PodPending
				}()
				createdPod.Status.Conditions = []corev1.PodCondition{
					{
						Type: corev1.PodReady,
						Status: func() corev1.ConditionStatus {
							if isReady {
								return corev1.ConditionTrue
							}
							return corev1.ConditionFalse
						}(),
					},
				}
				
				Expect(k8sClient.Status().Update(ctx, createdPod)).To(Succeed())
			}

			By("Creating StatefulSetLock")
			statefulSetLock := &appv1.StatefulSetLock{
				ObjectMeta: metav1.ObjectMeta{
					Name:      statefulSetLockName,
					Namespace: testNamespace,
				},
				Spec: appv1.StatefulSetLockSpec{
					StatefulSetName:      statefulSetName,
					LeaseName:            leaseName,
					LeaseDurationSeconds: 5,
				},
			}
			Expect(k8sClient.Create(ctx, statefulSetLock)).To(Succeed())

			By("Establishing initial leader (should be pod-1 since pod-0 is not ready)")
			result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: statefulSetLockName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())
			
			if result.Requeue || result.RequeueAfter > 0 {
				_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: types.NamespacedName{Name: statefulSetLockName, Namespace: testNamespace},
				})
				Expect(err).NotTo(HaveOccurred())
			}

			// Verify pod-1 is the leader
			currentLeaderName := fmt.Sprintf("%s-1", statefulSetName)
			lease := &coordinationv1.Lease{}
			Eventually(func() string {
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: leaseName, Namespace: testNamespace}, lease); err != nil {
					return ""
				}
				if lease.Spec.HolderIdentity == nil {
					return ""
				}
				return *lease.Spec.HolderIdentity
			}, "10s", "1s").Should(Equal(currentLeaderName))

			By("Making pod-0 ready (lower ordinal than current leader)")
			pod0 := &corev1.Pod{}
			pod0Name := fmt.Sprintf("%s-0", statefulSetName)
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: pod0Name, Namespace: testNamespace}, pod0)).To(Succeed())
			pod0.Status.Phase = corev1.PodRunning
			pod0.Status.Conditions = []corev1.PodCondition{
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionTrue,
				},
			}
			Expect(k8sClient.Status().Update(ctx, pod0)).To(Succeed())

			By("Triggering reconciliation - leader should NOT change immediately (lease still valid)")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: statefulSetLockName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			// Leader should still be pod-1 (lease is still valid)
			Consistently(func() string {
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: leaseName, Namespace: testNamespace}, lease); err != nil {
					return ""
				}
				if lease.Spec.HolderIdentity == nil {
					return ""
				}
				return *lease.Spec.HolderIdentity
			}, "3s", "500ms").Should(Equal(currentLeaderName))

			By("Waiting for lease to expire and reconciling")
			time.Sleep(6 * time.Second)
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: statefulSetLockName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying leadership transitions to lower ordinal pod-0")
			newLeaderName := fmt.Sprintf("%s-0", statefulSetName)
			Eventually(func() string {
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: leaseName, Namespace: testNamespace}, lease); err != nil {
					return ""
				}
				if lease.Spec.HolderIdentity == nil {
					return ""
				}
				return *lease.Spec.HolderIdentity
			}, "10s", "1s").Should(Equal(newLeaderName))

			By("Verifying labels and status are updated correctly")
			Eventually(func() bool {
				// Check new leader has writer label
				newLeaderPod := &corev1.Pod{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: newLeaderName, Namespace: testNamespace}, newLeaderPod); err != nil {
					return false
				}
				if newLeaderPod.Labels["sts-role"] != "writer" {
					return false
				}

				// Check old leader now has reader label
				oldLeaderPod := &corev1.Pod{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: currentLeaderName, Namespace: testNamespace}, oldLeaderPod); err != nil {
					return false
				}
				if oldLeaderPod.Labels["sts-role"] != "reader" {
					return false
				}

				// Check status reflects new leader
				updatedSSL := &appv1.StatefulSetLock{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: statefulSetLockName, Namespace: testNamespace}, updatedSSL); err != nil {
					return false
				}
				if updatedSSL.Status.WriterPod != newLeaderName {
					return false
				}

				return true
			}, "10s", "1s").Should(BeTrue())
		})
	})

	Context("Resource Cleanup Integration Tests", func() {
		var (
			ctx                context.Context
			controllerReconciler *StatefulSetLockReconciler
			testNamespace      string
			statefulSetName    string
			leaseName          string
			statefulSetLockName string
		)

		BeforeEach(func() {
			ctx = context.Background()
			controllerReconciler = &StatefulSetLockReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			testNamespace = "default"
			// Make resource names unique per test to avoid conflicts
			testSuffix := fmt.Sprintf("%d-%d", GinkgoRandomSeed(), time.Now().UnixNano())
			statefulSetName = fmt.Sprintf("test-sts-%s", testSuffix)
			leaseName = fmt.Sprintf("test-lease-%s", testSuffix)
			statefulSetLockName = fmt.Sprintf("test-ssl-%s", testSuffix)
		})

		AfterEach(func() {
			// Clean up all resources created during the test
			By("Cleaning up StatefulSetLock")
			ssl := &appv1.StatefulSetLock{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: statefulSetLockName, Namespace: testNamespace}, ssl); err == nil {
				Expect(k8sClient.Delete(ctx, ssl)).To(Succeed())
			}

			By("Cleaning up StatefulSet")
			sts := &appsv1.StatefulSet{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: statefulSetName, Namespace: testNamespace}, sts); err == nil {
				Expect(k8sClient.Delete(ctx, sts)).To(Succeed())
			}

			By("Cleaning up Pods")
			podList := &corev1.PodList{}
			if err := k8sClient.List(ctx, podList, client.InNamespace(testNamespace), client.MatchingLabels{"app": "test"}); err == nil {
				for _, pod := range podList.Items {
					Expect(k8sClient.Delete(ctx, &pod)).To(Succeed())
				}
			}

			By("Cleaning up Lease")
			lease := &coordinationv1.Lease{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: leaseName, Namespace: testNamespace}, lease); err == nil {
				Expect(k8sClient.Delete(ctx, lease)).To(Succeed())
			}
		})

		It("should trigger finalizer cleanup when StatefulSetLock is deleted", func() {
			By("Creating a StatefulSet with 2 replicas")
			statefulSet := createTestStatefulSet(statefulSetName, testNamespace, 2)
			Expect(k8sClient.Create(ctx, statefulSet)).To(Succeed())

			By("Creating ready pods")
			pods := createTestPods(statefulSetName, testNamespace, 2, 2, statefulSet)
			for _, pod := range pods {
				Expect(k8sClient.Create(ctx, &pod)).To(Succeed())
				
				// Update the status separately (required for envtest)
				createdPod := &corev1.Pod{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: pod.Name, Namespace: testNamespace}, createdPod)).To(Succeed())
				
				createdPod.Status.Phase = corev1.PodRunning
				createdPod.Status.Conditions = []corev1.PodCondition{
					{
						Type:   corev1.PodReady,
						Status: corev1.ConditionTrue,
					},
				}
				
				Expect(k8sClient.Status().Update(ctx, createdPod)).To(Succeed())
			}

			By("Creating a StatefulSetLock")
			statefulSetLock := &appv1.StatefulSetLock{
				ObjectMeta: metav1.ObjectMeta{
					Name:      statefulSetLockName,
					Namespace: testNamespace,
				},
				Spec: appv1.StatefulSetLockSpec{
					StatefulSetName:      statefulSetName,
					LeaseName:            leaseName,
					LeaseDurationSeconds: 30,
				},
			}
			Expect(k8sClient.Create(ctx, statefulSetLock)).To(Succeed())

			By("Reconciling to establish initial state")
			result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: statefulSetLockName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())
			
			if result.Requeue || result.RequeueAfter > 0 {
				_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: types.NamespacedName{Name: statefulSetLockName, Namespace: testNamespace},
				})
				Expect(err).NotTo(HaveOccurred())
			}

			By("Verifying finalizer is added")
			Eventually(func() bool {
				updatedSSL := &appv1.StatefulSetLock{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: statefulSetLockName, Namespace: testNamespace}, updatedSSL); err != nil {
					return false
				}
				return controllerutil.ContainsFinalizer(updatedSSL, "statefulsetlock.app.anukkrit.me/finalizer")
			}, "10s", "1s").Should(BeTrue())

			By("Verifying lease is created")
			lease := &coordinationv1.Lease{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: leaseName, Namespace: testNamespace}, lease)
			}, "10s", "1s").Should(Succeed())

			By("Verifying pods are labeled")
			Eventually(func() bool {
				leaderPod := &corev1.Pod{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf("%s-0", statefulSetName), Namespace: testNamespace}, leaderPod); err != nil {
					return false
				}
				if leaderPod.Labels["sts-role"] != "writer" {
					return false
				}

				readerPod := &corev1.Pod{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf("%s-1", statefulSetName), Namespace: testNamespace}, readerPod); err != nil {
					return false
				}
				return readerPod.Labels["sts-role"] == "reader"
			}, "10s", "1s").Should(BeTrue())

			By("Deleting the StatefulSetLock")
			Expect(k8sClient.Delete(ctx, statefulSetLock)).To(Succeed())

			By("Reconciling to trigger cleanup")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: statefulSetLockName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying associated lease is deleted during cleanup")
			Eventually(func() bool {
				lease := &coordinationv1.Lease{}
				err := k8sClient.Get(ctx, types.NamespacedName{Name: leaseName, Namespace: testNamespace}, lease)
				return errors.IsNotFound(err)
			}, "10s", "1s").Should(BeTrue())

			By("Verifying pod role labels are removed during cleanup")
			Eventually(func() bool {
				leaderPod := &corev1.Pod{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf("%s-0", statefulSetName), Namespace: testNamespace}, leaderPod); err != nil {
					return false
				}
				if _, exists := leaderPod.Labels["sts-role"]; exists {
					return false
				}

				readerPod := &corev1.Pod{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf("%s-1", statefulSetName), Namespace: testNamespace}, readerPod); err != nil {
					return false
				}
				_, exists := readerPod.Labels["sts-role"]
				return !exists
			}, "10s", "1s").Should(BeTrue())

			By("Verifying finalizer is removed after successful cleanup")
			Eventually(func() bool {
				ssl := &appv1.StatefulSetLock{}
				err := k8sClient.Get(ctx, types.NamespacedName{Name: statefulSetLockName, Namespace: testNamespace}, ssl)
				return errors.IsNotFound(err)
			}, "10s", "1s").Should(BeTrue())
		})

		It("should delete associated lease during cleanup", func() {
			By("Creating a StatefulSet with 1 replica")
			statefulSet := createTestStatefulSet(statefulSetName, testNamespace, 1)
			Expect(k8sClient.Create(ctx, statefulSet)).To(Succeed())

			By("Creating a ready pod")
			pods := createTestPods(statefulSetName, testNamespace, 1, 1, statefulSet)
			pod := pods[0]
			Expect(k8sClient.Create(ctx, &pod)).To(Succeed())
			
			// Update the status separately (required for envtest)
			createdPod := &corev1.Pod{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: pod.Name, Namespace: testNamespace}, createdPod)).To(Succeed())
			
			createdPod.Status.Phase = corev1.PodRunning
			createdPod.Status.Conditions = []corev1.PodCondition{
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionTrue,
				},
			}
			
			Expect(k8sClient.Status().Update(ctx, createdPod)).To(Succeed())

			By("Creating a StatefulSetLock")
			statefulSetLock := &appv1.StatefulSetLock{
				ObjectMeta: metav1.ObjectMeta{
					Name:      statefulSetLockName,
					Namespace: testNamespace,
				},
				Spec: appv1.StatefulSetLockSpec{
					StatefulSetName:      statefulSetName,
					LeaseName:            leaseName,
					LeaseDurationSeconds: 30,
				},
			}
			Expect(k8sClient.Create(ctx, statefulSetLock)).To(Succeed())

			By("Reconciling to create lease")
			result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: statefulSetLockName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())
			
			if result.Requeue || result.RequeueAfter > 0 {
				_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: types.NamespacedName{Name: statefulSetLockName, Namespace: testNamespace},
				})
				Expect(err).NotTo(HaveOccurred())
			}

			By("Verifying lease exists")
			lease := &coordinationv1.Lease{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: leaseName, Namespace: testNamespace}, lease)
			}, "10s", "1s").Should(Succeed())

			By("Storing lease UID for verification")
			leaseUID := lease.UID

			By("Deleting the StatefulSetLock")
			Expect(k8sClient.Delete(ctx, statefulSetLock)).To(Succeed())

			By("Reconciling to trigger cleanup")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: statefulSetLockName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying the specific lease is deleted")
			Eventually(func() bool {
				lease := &coordinationv1.Lease{}
				err := k8sClient.Get(ctx, types.NamespacedName{Name: leaseName, Namespace: testNamespace}, lease)
				if errors.IsNotFound(err) {
					return true
				}
				// If lease still exists, verify it's not the same one (different UID)
				return lease.UID != leaseUID
			}, "10s", "1s").Should(BeTrue())
		})

		It("should ensure finalizer is removed after successful cleanup", func() {
			By("Creating a StatefulSet with 1 replica")
			statefulSet := createTestStatefulSet(statefulSetName, testNamespace, 1)
			Expect(k8sClient.Create(ctx, statefulSet)).To(Succeed())

			By("Creating a ready pod")
			pods := createTestPods(statefulSetName, testNamespace, 1, 1, statefulSet)
			pod := pods[0]
			Expect(k8sClient.Create(ctx, &pod)).To(Succeed())
			
			// Update the status separately (required for envtest)
			createdPod := &corev1.Pod{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: pod.Name, Namespace: testNamespace}, createdPod)).To(Succeed())
			
			createdPod.Status.Phase = corev1.PodRunning
			createdPod.Status.Conditions = []corev1.PodCondition{
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionTrue,
				},
			}
			
			Expect(k8sClient.Status().Update(ctx, createdPod)).To(Succeed())

			By("Creating a StatefulSetLock")
			statefulSetLock := &appv1.StatefulSetLock{
				ObjectMeta: metav1.ObjectMeta{
					Name:      statefulSetLockName,
					Namespace: testNamespace,
				},
				Spec: appv1.StatefulSetLockSpec{
					StatefulSetName:      statefulSetName,
					LeaseName:            leaseName,
					LeaseDurationSeconds: 30,
				},
			}
			Expect(k8sClient.Create(ctx, statefulSetLock)).To(Succeed())

			By("Reconciling to establish initial state")
			result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: statefulSetLockName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())
			
			if result.Requeue || result.RequeueAfter > 0 {
				_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: types.NamespacedName{Name: statefulSetLockName, Namespace: testNamespace},
				})
				Expect(err).NotTo(HaveOccurred())
			}

			By("Verifying finalizer is present")
			Eventually(func() bool {
				updatedSSL := &appv1.StatefulSetLock{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: statefulSetLockName, Namespace: testNamespace}, updatedSSL); err != nil {
					return false
				}
				return controllerutil.ContainsFinalizer(updatedSSL, "statefulsetlock.app.anukkrit.me/finalizer")
			}, "10s", "1s").Should(BeTrue())

			By("Deleting the StatefulSetLock")
			Expect(k8sClient.Delete(ctx, statefulSetLock)).To(Succeed())

			By("Verifying StatefulSetLock has deletion timestamp but still exists due to finalizer")
			Eventually(func() bool {
				ssl := &appv1.StatefulSetLock{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: statefulSetLockName, Namespace: testNamespace}, ssl); err != nil {
					return false
				}
				return ssl.DeletionTimestamp != nil && controllerutil.ContainsFinalizer(ssl, "statefulsetlock.app.anukkrit.me/finalizer")
			}, "10s", "1s").Should(BeTrue())

			By("Reconciling to trigger cleanup")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: statefulSetLockName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying StatefulSetLock is completely deleted after finalizer removal")
			Eventually(func() bool {
				ssl := &appv1.StatefulSetLock{}
				err := k8sClient.Get(ctx, types.NamespacedName{Name: statefulSetLockName, Namespace: testNamespace}, ssl)
				return errors.IsNotFound(err)
			}, "10s", "1s").Should(BeTrue())
		})

		It("should handle cleanup behavior with various error conditions", func() {
			By("Creating a StatefulSet with 2 replicas")
			statefulSet := createTestStatefulSet(statefulSetName, testNamespace, 2)
			Expect(k8sClient.Create(ctx, statefulSet)).To(Succeed())

			By("Creating ready pods")
			pods := createTestPods(statefulSetName, testNamespace, 2, 2, statefulSet)
			for _, pod := range pods {
				Expect(k8sClient.Create(ctx, &pod)).To(Succeed())
				
				// Update the status separately (required for envtest)
				createdPod := &corev1.Pod{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: pod.Name, Namespace: testNamespace}, createdPod)).To(Succeed())
				
				createdPod.Status.Phase = corev1.PodRunning
				createdPod.Status.Conditions = []corev1.PodCondition{
					{
						Type:   corev1.PodReady,
						Status: corev1.ConditionTrue,
					},
				}
				
				Expect(k8sClient.Status().Update(ctx, createdPod)).To(Succeed())
			}

			By("Creating a StatefulSetLock")
			statefulSetLock := &appv1.StatefulSetLock{
				ObjectMeta: metav1.ObjectMeta{
					Name:      statefulSetLockName,
					Namespace: testNamespace,
				},
				Spec: appv1.StatefulSetLockSpec{
					StatefulSetName:      statefulSetName,
					LeaseName:            leaseName,
					LeaseDurationSeconds: 30,
				},
			}
			Expect(k8sClient.Create(ctx, statefulSetLock)).To(Succeed())

			By("Reconciling to establish initial state")
			result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: statefulSetLockName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())
			
			if result.Requeue || result.RequeueAfter > 0 {
				_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: types.NamespacedName{Name: statefulSetLockName, Namespace: testNamespace},
				})
				Expect(err).NotTo(HaveOccurred())
			}

			By("Verifying lease and pod labels are created")
			lease := &coordinationv1.Lease{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: leaseName, Namespace: testNamespace}, lease)
			}, "10s", "1s").Should(Succeed())

			Eventually(func() bool {
				leaderPod := &corev1.Pod{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf("%s-0", statefulSetName), Namespace: testNamespace}, leaderPod); err != nil {
					return false
				}
				return leaderPod.Labels["sts-role"] == "writer"
			}, "10s", "1s").Should(BeTrue())

			By("Simulating error condition: Deleting StatefulSet before StatefulSetLock cleanup")
			Expect(k8sClient.Delete(ctx, statefulSet)).To(Succeed())

			By("Deleting the StatefulSetLock")
			Expect(k8sClient.Delete(ctx, statefulSetLock)).To(Succeed())

			By("Reconciling to trigger cleanup with missing StatefulSet")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: statefulSetLockName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying cleanup continues despite StatefulSet being missing")
			Eventually(func() bool {
				lease := &coordinationv1.Lease{}
				err := k8sClient.Get(ctx, types.NamespacedName{Name: leaseName, Namespace: testNamespace}, lease)
				return errors.IsNotFound(err)
			}, "10s", "1s").Should(BeTrue())

			By("Verifying StatefulSetLock is eventually deleted despite errors")
			Eventually(func() bool {
				ssl := &appv1.StatefulSetLock{}
				err := k8sClient.Get(ctx, types.NamespacedName{Name: statefulSetLockName, Namespace: testNamespace}, ssl)
				return errors.IsNotFound(err)
			}, "10s", "1s").Should(BeTrue())
		})

		It("should handle cleanup when lease is already deleted externally", func() {
			By("Creating a StatefulSet with 1 replica")
			statefulSet := createTestStatefulSet(statefulSetName, testNamespace, 1)
			Expect(k8sClient.Create(ctx, statefulSet)).To(Succeed())

			By("Creating a ready pod")
			pods := createTestPods(statefulSetName, testNamespace, 1, 1, statefulSet)
			pod := pods[0]
			Expect(k8sClient.Create(ctx, &pod)).To(Succeed())
			
			// Update the status separately (required for envtest)
			createdPod := &corev1.Pod{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: pod.Name, Namespace: testNamespace}, createdPod)).To(Succeed())
			
			createdPod.Status.Phase = corev1.PodRunning
			createdPod.Status.Conditions = []corev1.PodCondition{
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionTrue,
				},
			}
			
			Expect(k8sClient.Status().Update(ctx, createdPod)).To(Succeed())

			By("Creating a StatefulSetLock")
			statefulSetLock := &appv1.StatefulSetLock{
				ObjectMeta: metav1.ObjectMeta{
					Name:      statefulSetLockName,
					Namespace: testNamespace,
				},
				Spec: appv1.StatefulSetLockSpec{
					StatefulSetName:      statefulSetName,
					LeaseName:            leaseName,
					LeaseDurationSeconds: 30,
				},
			}
			Expect(k8sClient.Create(ctx, statefulSetLock)).To(Succeed())

			By("Reconciling to create lease")
			result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: statefulSetLockName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())
			
			if result.Requeue || result.RequeueAfter > 0 {
				_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: types.NamespacedName{Name: statefulSetLockName, Namespace: testNamespace},
				})
				Expect(err).NotTo(HaveOccurred())
			}

			By("Verifying lease exists")
			lease := &coordinationv1.Lease{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: leaseName, Namespace: testNamespace}, lease)
			}, "10s", "1s").Should(Succeed())

			By("Manually deleting the lease to simulate external deletion")
			Expect(k8sClient.Delete(ctx, lease)).To(Succeed())

			By("Verifying lease is deleted")
			Eventually(func() bool {
				lease := &coordinationv1.Lease{}
				err := k8sClient.Get(ctx, types.NamespacedName{Name: leaseName, Namespace: testNamespace}, lease)
				return errors.IsNotFound(err)
			}, "10s", "1s").Should(BeTrue())

			By("Deleting the StatefulSetLock")
			Expect(k8sClient.Delete(ctx, statefulSetLock)).To(Succeed())

			By("Reconciling to trigger cleanup with already-deleted lease")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: statefulSetLockName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying cleanup completes successfully despite lease already being deleted")
			Eventually(func() bool {
				ssl := &appv1.StatefulSetLock{}
				err := k8sClient.Get(ctx, types.NamespacedName{Name: statefulSetLockName, Namespace: testNamespace}, ssl)
				return errors.IsNotFound(err)
			}, "10s", "1s").Should(BeTrue())
		})

		It("should handle cleanup when pods are deleted before StatefulSetLock cleanup", func() {
			By("Creating a StatefulSet with 2 replicas")
			statefulSet := createTestStatefulSet(statefulSetName, testNamespace, 2)
			Expect(k8sClient.Create(ctx, statefulSet)).To(Succeed())

			By("Creating ready pods")
			pods := createTestPods(statefulSetName, testNamespace, 2, 2, statefulSet)
			for _, pod := range pods {
				Expect(k8sClient.Create(ctx, &pod)).To(Succeed())
				
				// Update the status separately (required for envtest)
				createdPod := &corev1.Pod{}
				Expect(k8sClient.Get(ctx, types.NamespacedName{Name: pod.Name, Namespace: testNamespace}, createdPod)).To(Succeed())
				
				createdPod.Status.Phase = corev1.PodRunning
				createdPod.Status.Conditions = []corev1.PodCondition{
					{
						Type:   corev1.PodReady,
						Status: corev1.ConditionTrue,
					},
				}
				
				Expect(k8sClient.Status().Update(ctx, createdPod)).To(Succeed())
			}

			By("Creating a StatefulSetLock")
			statefulSetLock := &appv1.StatefulSetLock{
				ObjectMeta: metav1.ObjectMeta{
					Name:      statefulSetLockName,
					Namespace: testNamespace,
				},
				Spec: appv1.StatefulSetLockSpec{
					StatefulSetName:      statefulSetName,
					LeaseName:            leaseName,
					LeaseDurationSeconds: 30,
				},
			}
			Expect(k8sClient.Create(ctx, statefulSetLock)).To(Succeed())

			By("Reconciling to establish initial state")
			result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: statefulSetLockName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())
			
			if result.Requeue || result.RequeueAfter > 0 {
				_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: types.NamespacedName{Name: statefulSetLockName, Namespace: testNamespace},
				})
				Expect(err).NotTo(HaveOccurred())
			}

			By("Verifying pods are labeled")
			Eventually(func() bool {
				leaderPod := &corev1.Pod{}
				if err := k8sClient.Get(ctx, types.NamespacedName{Name: fmt.Sprintf("%s-0", statefulSetName), Namespace: testNamespace}, leaderPod); err != nil {
					return false
				}
				return leaderPod.Labels["sts-role"] == "writer"
			}, "10s", "1s").Should(BeTrue())

			By("Manually deleting all pods before StatefulSetLock cleanup")
			for _, pod := range pods {
				Expect(k8sClient.Delete(ctx, &pod)).To(Succeed())
			}

			By("Verifying pods are deleted")
			Eventually(func() bool {
				for _, pod := range pods {
					existingPod := &corev1.Pod{}
					if err := k8sClient.Get(ctx, types.NamespacedName{Name: pod.Name, Namespace: testNamespace}, existingPod); !errors.IsNotFound(err) {
						return false
					}
				}
				return true
			}, "10s", "1s").Should(BeTrue())

			By("Deleting the StatefulSetLock")
			Expect(k8sClient.Delete(ctx, statefulSetLock)).To(Succeed())

			By("Reconciling to trigger cleanup with deleted pods")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: statefulSetLockName, Namespace: testNamespace},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying cleanup completes successfully despite pods being deleted")
			Eventually(func() bool {
				ssl := &appv1.StatefulSetLock{}
				err := k8sClient.Get(ctx, types.NamespacedName{Name: statefulSetLockName, Namespace: testNamespace}, ssl)
				return errors.IsNotFound(err)
			}, "10s", "1s").Should(BeTrue())

			By("Verifying lease is still cleaned up")
			Eventually(func() bool {
				lease := &coordinationv1.Lease{}
				err := k8sClient.Get(ctx, types.NamespacedName{Name: leaseName, Namespace: testNamespace}, lease)
				return errors.IsNotFound(err)
			}, "10s", "1s").Should(BeTrue())
		})
	})
})

// Helper functions for integration tests

// createTestStatefulSet creates a test StatefulSet with the specified name, namespace, and replicas
func createTestStatefulSet(name, namespace string, replicas int32) *appsv1.StatefulSet {
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: ptr.To(replicas),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "test"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "test"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "nginx:latest",
						},
					},
				},
			},
		},
	}
}

// createTestPods creates test pods for a StatefulSet with specified readiness
func createTestPods(stsName, namespace string, count, readyCount int, statefulSet *appsv1.StatefulSet) []corev1.Pod {
	pods := make([]corev1.Pod, count)
	
	for i := 0; i < count; i++ {
		podName := fmt.Sprintf("%s-%d", stsName, i)
		isReady := i < readyCount
		
		pods[i] = corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      podName,
				Namespace: namespace,
				Labels:    map[string]string{"app": "test"},
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion: "apps/v1",
						Kind:       "StatefulSet",
						Name:       stsName,
						UID:        statefulSet.UID,
					},
				},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "test-container",
						Image: "nginx:latest",
					},
				},
			},
			Status: corev1.PodStatus{
				Phase: func() corev1.PodPhase {
					if isReady {
						return corev1.PodRunning
					}
					return corev1.PodPending
				}(),
				Conditions: []corev1.PodCondition{
					{
						Type:   corev1.PodReady,
						Status: func() corev1.ConditionStatus {
							if isReady {
								return corev1.ConditionTrue
							}
							return corev1.ConditionFalse
						}(),
					},
				},
			},
		}
	}
	
	return pods
}

// createPodWithReadiness creates a single pod with specified readiness
func createPodWithReadiness(name, namespace string, ready bool, statefulSet *appsv1.StatefulSet) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    map[string]string{"app": "test"},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "apps/v1",
					Kind:       "StatefulSet",
					Name:       statefulSet.Name,
					UID:        statefulSet.UID,
				},
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "test-container",
					Image: "nginx:latest",
				},
			},
		},
		Status: corev1.PodStatus{
			Phase: func() corev1.PodPhase {
				if ready {
					return corev1.PodRunning
				}
				return corev1.PodPending
			}(),
			Conditions: []corev1.PodCondition{
				{
					Type: corev1.PodReady,
					Status: func() corev1.ConditionStatus {
						if ready {
							return corev1.ConditionTrue
						}
						return corev1.ConditionFalse
					}(),
				},
			},
		},
	}
}



