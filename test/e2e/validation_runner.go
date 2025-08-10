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

package e2e

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/anukkrit/statefulset-leader-election-operator/test/utils"
)

// ValidationRunner provides comprehensive end-to-end validation
var _ = Describe("Comprehensive E2E Validation", Ordered, func() {
	const (
		validationNamespace = "e2e-validation-test"
		operatorNamespace   = "sts-leader-elect-operator-system"
		testTimeout         = time.Minute * 10
		testInterval        = time.Second * 5
	)

	BeforeAll(func() {
		By("Setting up validation environment")

		// Create validation namespace
		cmd := exec.Command("kubectl", "create", "namespace", validationNamespace, "--dry-run=client", "-o", "yaml")
		output, err := utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred())

		cmd = exec.Command("kubectl", "apply", "-f", "-")
		cmd.Stdin = strings.NewReader(string(output))
		_, err = utils.Run(cmd)
		Expect(err).NotTo(HaveOccurred())
	})

	AfterAll(func() {
		By("Cleaning up validation environment")
		cmd := exec.Command("kubectl", "delete", "namespace", validationNamespace, "--ignore-not-found=true")
		_, _ = utils.Run(cmd)
	})

	Context("Requirements Validation", func() {
		It("should satisfy Requirement 8.1: Unit test coverage for helper functions", func() {
			By("Running unit tests with coverage")
			cmd := exec.Command("make", "test")
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			// Check that coverage report is generated
			Expect(string(output)).To(ContainSubstring("coverage:"))

			// Verify coverage file exists
			cmd = exec.Command("ls", "cover.out")
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should satisfy Requirement 8.2: Integration tests against simulated API server", func() {
			By("Running integration tests")
			cmd := exec.Command("go", "test", "./internal/controller/", "-v", "-run", "TestStatefulSetLockController")
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(output)).To(ContainSubstring("PASS"))
		})

		It("should satisfy Requirement 8.3: Happy path scenario testing", func() {
			By("Testing basic leader election scenario")

			// Create test StatefulSet
			testStsYAML := `
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: happy-path-test
  namespace: ` + validationNamespace + `
  labels:
    app: happy-path-test
spec:
  serviceName: happy-path-test-headless
  replicas: 3
  selector:
    matchLabels:
      app: happy-path-test
  template:
    metadata:
      labels:
        app: happy-path-test
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
  name: happy-path-test-headless
  namespace: ` + validationNamespace + `
spec:
  clusterIP: None
  selector:
    app: happy-path-test
  ports:
  - port: 80`

			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(testStsYAML)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			// Wait for pods to be ready
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "pods", "-l", "app=happy-path-test",
					"-n", validationNamespace, "--no-headers")
				output, err := utils.Run(cmd)
				if err != nil {
					return false
				}
				lines := strings.Split(strings.TrimSpace(string(output)), "\n")
				readyCount := 0
				for _, line := range lines {
					if strings.Contains(line, "Running") && strings.Contains(line, "1/1") {
						readyCount++
					}
				}
				return readyCount == 3
			}, testTimeout, testInterval).Should(BeTrue())

			// Create StatefulSetLock
			sslYAML := `
apiVersion: app.anukkrit.me/v1
kind: StatefulSetLock
metadata:
  name: happy-path-test-lock
  namespace: ` + validationNamespace + `
spec:
  statefulSetName: happy-path-test
  leaseName: happy-path-test-lease
  leaseDurationSeconds: 30`

			cmd = exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(sslYAML)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			// Verify lease creation
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "lease", "happy-path-test-lease", "-n", validationNamespace)
				_, err := utils.Run(cmd)
				return err == nil
			}, testTimeout, testInterval).Should(BeTrue())

			// Verify pod labeling
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "pods", "-l", "app=happy-path-test,sts-role=writer",
					"-n", validationNamespace, "--no-headers")
				output, err := utils.Run(cmd)
				if err != nil {
					return false
				}
				writerCount := len(strings.Split(strings.TrimSpace(string(output)), "\n"))
				if strings.TrimSpace(string(output)) == "" {
					writerCount = 0
				}

				cmd = exec.Command("kubectl", "get", "pods", "-l", "app=happy-path-test,sts-role=reader",
					"-n", validationNamespace, "--no-headers")
				output, err = utils.Run(cmd)
				if err != nil {
					return false
				}
				readerCount := len(strings.Split(strings.TrimSpace(string(output)), "\n"))
				if strings.TrimSpace(string(output)) == "" {
					readerCount = 0
				}

				return writerCount == 1 && readerCount == 2
			}, testTimeout, testInterval).Should(BeTrue())

			// Verify status update
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "statefulsetlock", "happy-path-test-lock",
					"-n", validationNamespace, "-o", "jsonpath={.status.writerPod}")
				output, err := utils.Run(cmd)
				return err == nil && strings.TrimSpace(string(output)) != ""
			}, testTimeout, testInterval).Should(BeTrue())
		})

		It("should satisfy Requirement 8.4: Failover scenario testing", func() {
			By("Testing leader failover when current leader becomes unavailable")

			// Get current leader
			cmd := exec.Command("kubectl", "get", "pods", "-l", "app=happy-path-test,sts-role=writer",
				"-n", validationNamespace, "-o", "jsonpath={.items[0].metadata.name}")
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			currentLeader := strings.TrimSpace(string(output))
			Expect(currentLeader).NotTo(BeEmpty())

			// Delete current leader pod
			cmd = exec.Command("kubectl", "delete", "pod", currentLeader, "-n", validationNamespace)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			// Wait for new leader election
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "pods", "-l", "app=happy-path-test,sts-role=writer",
					"-n", validationNamespace, "-o", "jsonpath={.items[0].metadata.name}")
				output, err := utils.Run(cmd)
				if err != nil {
					return false
				}
				newLeader := strings.TrimSpace(string(output))
				return newLeader != "" && newLeader != currentLeader
			}, testTimeout, testInterval).Should(BeTrue())

			// Verify pod labels are correct after failover
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "pods", "-l", "app=happy-path-test,sts-role=writer",
					"-n", validationNamespace, "--no-headers")
				output, err := utils.Run(cmd)
				if err != nil {
					return false
				}
				writerCount := len(strings.Split(strings.TrimSpace(string(output)), "\n"))
				if strings.TrimSpace(string(output)) == "" {
					writerCount = 0
				}
				return writerCount == 1
			}, testTimeout, testInterval).Should(BeTrue())
		})

		It("should satisfy Requirement 8.5: Resource cleanup testing", func() {
			By("Testing proper cleanup of associated resources")

			// Verify lease exists before deletion
			cmd := exec.Command("kubectl", "get", "lease", "happy-path-test-lease", "-n", validationNamespace)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			// Delete StatefulSetLock
			cmd = exec.Command("kubectl", "delete", "statefulsetlock", "happy-path-test-lock", "-n", validationNamespace)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			// Verify lease is cleaned up
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "lease", "happy-path-test-lease", "-n", validationNamespace)
				_, err := utils.Run(cmd)
				return err != nil // Should return error when lease doesn't exist
			}, testTimeout, testInterval).Should(BeTrue())
		})
	})

	Context("Performance and Scale Testing", func() {
		It("should handle multiple StatefulSets efficiently", func() {
			By("Creating multiple StatefulSets with StatefulSetLocks")

			numStatefulSets := 5
			for i := 1; i <= numStatefulSets; i++ {
				stsName := fmt.Sprintf("perf-test-%d", i)
				leaseName := fmt.Sprintf("perf-lease-%d", i)

				testYAML := fmt.Sprintf(`
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: %s
  namespace: %s
  labels:
    app: %s
spec:
  serviceName: %s-headless
  replicas: 2
  selector:
    matchLabels:
      app: %s
  template:
    metadata:
      labels:
        app: %s
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
  name: %s-headless
  namespace: %s
spec:
  clusterIP: None
  selector:
    app: %s
  ports:
  - port: 80
---
apiVersion: app.anukkrit.me/v1
kind: StatefulSetLock
metadata:
  name: %s-lock
  namespace: %s
spec:
  statefulSetName: %s
  leaseName: %s
  leaseDurationSeconds: 30`,
					stsName, validationNamespace, stsName, stsName, stsName, stsName,
					stsName, validationNamespace, stsName, stsName, validationNamespace,
					stsName, leaseName)

				cmd := exec.Command("kubectl", "apply", "-f", "-")
				cmd.Stdin = strings.NewReader(testYAML)
				_, err := utils.Run(cmd)
				Expect(err).NotTo(HaveOccurred())
			}

			// Wait for all StatefulSets to have ready pods
			for i := 1; i <= numStatefulSets; i++ {
				stsName := fmt.Sprintf("perf-test-%d", i)
				Eventually(func() bool {
					cmd := exec.Command("kubectl", "get", "pods", "-l", fmt.Sprintf("app=%s", stsName),
						"-n", validationNamespace, "--no-headers")
					output, err := utils.Run(cmd)
					if err != nil {
						return false
					}
					lines := strings.Split(strings.TrimSpace(string(output)), "\n")
					readyCount := 0
					for _, line := range lines {
						if strings.Contains(line, "Running") && strings.Contains(line, "1/1") {
							readyCount++
						}
					}
					return readyCount == 2
				}, testTimeout, testInterval).Should(BeTrue())
			}

			// Verify all have elected leaders
			for i := 1; i <= numStatefulSets; i++ {
				leaseName := fmt.Sprintf("perf-lease-%d", i)
				Eventually(func() bool {
					cmd := exec.Command("kubectl", "get", "lease", leaseName, "-n", validationNamespace)
					_, err := utils.Run(cmd)
					return err == nil
				}, testTimeout, testInterval).Should(BeTrue())
			}

			// Verify all have correct pod labels
			for i := 1; i <= numStatefulSets; i++ {
				stsName := fmt.Sprintf("perf-test-%d", i)
				Eventually(func() bool {
					cmd := exec.Command("kubectl", "get", "pods", "-l", fmt.Sprintf("app=%s,sts-role=writer", stsName),
						"-n", validationNamespace, "--no-headers")
					output, err := utils.Run(cmd)
					if err != nil {
						return false
					}
					writerCount := len(strings.Split(strings.TrimSpace(string(output)), "\n"))
					if strings.TrimSpace(string(output)) == "" {
						writerCount = 0
					}
					return writerCount == 1
				}, testTimeout, testInterval).Should(BeTrue())
			}
		})

		It("should maintain performance with large StatefulSets", func() {
			By("Creating StatefulSet with many replicas")

			largeTestYAML := `
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: large-scale-test
  namespace: ` + validationNamespace + `
  labels:
    app: large-scale-test
spec:
  serviceName: large-scale-test-headless
  replicas: 10
  selector:
    matchLabels:
      app: large-scale-test
  template:
    metadata:
      labels:
        app: large-scale-test
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
  name: large-scale-test-headless
  namespace: ` + validationNamespace + `
spec:
  clusterIP: None
  selector:
    app: large-scale-test
  ports:
  - port: 80
---
apiVersion: app.anukkrit.me/v1
kind: StatefulSetLock
metadata:
  name: large-scale-test-lock
  namespace: ` + validationNamespace + `
spec:
  statefulSetName: large-scale-test
  leaseName: large-scale-lease
  leaseDurationSeconds: 15`

			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(largeTestYAML)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			// Wait for pods to be ready (with extended timeout for large StatefulSet)
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "pods", "-l", "app=large-scale-test",
					"-n", validationNamespace, "--no-headers")
				output, err := utils.Run(cmd)
				if err != nil {
					return false
				}
				lines := strings.Split(strings.TrimSpace(string(output)), "\n")
				readyCount := 0
				for _, line := range lines {
					if strings.Contains(line, "Running") && strings.Contains(line, "1/1") {
						readyCount++
					}
				}
				return readyCount == 10
			}, testTimeout*2, testInterval).Should(BeTrue())

			// Verify leader election works with many pods
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "lease", "large-scale-lease", "-n", validationNamespace)
				_, err := utils.Run(cmd)
				return err == nil
			}, testTimeout, testInterval).Should(BeTrue())

			// Verify exactly one writer and nine readers
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "pods", "-l", "app=large-scale-test,sts-role=writer",
					"-n", validationNamespace, "--no-headers")
				output, err := utils.Run(cmd)
				if err != nil {
					return false
				}
				writerCount := len(strings.Split(strings.TrimSpace(string(output)), "\n"))
				if strings.TrimSpace(string(output)) == "" {
					writerCount = 0
				}

				cmd = exec.Command("kubectl", "get", "pods", "-l", "app=large-scale-test,sts-role=reader",
					"-n", validationNamespace, "--no-headers")
				output, err = utils.Run(cmd)
				if err != nil {
					return false
				}
				readerCount := len(strings.Split(strings.TrimSpace(string(output)), "\n"))
				if strings.TrimSpace(string(output)) == "" {
					readerCount = 0
				}

				return writerCount == 1 && readerCount == 9
			}, testTimeout, testInterval).Should(BeTrue())
		})
	})

	Context("Edge Cases and Error Handling", func() {
		It("should handle StatefulSet with no ready pods gracefully", func() {
			By("Creating StatefulSet with failing pods")

			failingTestYAML := `
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: failing-pods-test
  namespace: ` + validationNamespace + `
  labels:
    app: failing-pods-test
spec:
  serviceName: failing-pods-test-headless
  replicas: 2
  selector:
    matchLabels:
      app: failing-pods-test
  template:
    metadata:
      labels:
        app: failing-pods-test
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
  name: failing-pods-test-headless
  namespace: ` + validationNamespace + `
spec:
  clusterIP: None
  selector:
    app: failing-pods-test
  ports:
  - port: 80
---
apiVersion: app.anukkrit.me/v1
kind: StatefulSetLock
metadata:
  name: failing-pods-test-lock
  namespace: ` + validationNamespace + `
spec:
  statefulSetName: failing-pods-test
  leaseName: failing-pods-lease
  leaseDurationSeconds: 30`

			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(failingTestYAML)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			// Wait and verify no lease is created
			Consistently(func() bool {
				cmd := exec.Command("kubectl", "get", "lease", "failing-pods-lease", "-n", validationNamespace)
				_, err := utils.Run(cmd)
				return err != nil // Should consistently return error (lease doesn't exist)
			}, time.Second*30, testInterval).Should(BeTrue())

			// Verify StatefulSetLock status shows appropriate condition
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "statefulsetlock", "failing-pods-test-lock",
					"-n", validationNamespace, "-o", "jsonpath={.status.conditions}")
				output, err := utils.Run(cmd)
				return err == nil && strings.Contains(string(output), "condition")
			}, testTimeout, testInterval).Should(BeTrue())
		})

		It("should handle rapid scaling events correctly", func() {
			By("Testing rapid scale up and down operations")

			// Create initial StatefulSet
			scalingTestYAML := `
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: scaling-test
  namespace: ` + validationNamespace + `
  labels:
    app: scaling-test
spec:
  serviceName: scaling-test-headless
  replicas: 1
  selector:
    matchLabels:
      app: scaling-test
  template:
    metadata:
      labels:
        app: scaling-test
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
  name: scaling-test-headless
  namespace: ` + validationNamespace + `
spec:
  clusterIP: None
  selector:
    app: scaling-test
  ports:
  - port: 80
---
apiVersion: app.anukkrit.me/v1
kind: StatefulSetLock
metadata:
  name: scaling-test-lock
  namespace: ` + validationNamespace + `
spec:
  statefulSetName: scaling-test
  leaseName: scaling-test-lease
  leaseDurationSeconds: 30`

			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = strings.NewReader(scalingTestYAML)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			// Wait for initial setup
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "lease", "scaling-test-lease", "-n", validationNamespace)
				_, err := utils.Run(cmd)
				return err == nil
			}, testTimeout, testInterval).Should(BeTrue())

			// Scale up rapidly
			cmd = exec.Command("kubectl", "scale", "statefulset", "scaling-test", "--replicas=5", "-n", validationNamespace)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			// Wait for scale up
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "pods", "-l", "app=scaling-test",
					"-n", validationNamespace, "--no-headers")
				output, err := utils.Run(cmd)
				if err != nil {
					return false
				}
				lines := strings.Split(strings.TrimSpace(string(output)), "\n")
				readyCount := 0
				for _, line := range lines {
					if strings.Contains(line, "Running") && strings.Contains(line, "1/1") {
						readyCount++
					}
				}
				return readyCount == 5
			}, testTimeout, testInterval).Should(BeTrue())

			// Verify leader election still works
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "pods", "-l", "app=scaling-test,sts-role=writer",
					"-n", validationNamespace, "--no-headers")
				output, err := utils.Run(cmd)
				if err != nil {
					return false
				}
				writerCount := len(strings.Split(strings.TrimSpace(string(output)), "\n"))
				if strings.TrimSpace(string(output)) == "" {
					writerCount = 0
				}
				return writerCount == 1
			}, testTimeout, testInterval).Should(BeTrue())

			// Scale down rapidly
			cmd = exec.Command("kubectl", "scale", "statefulset", "scaling-test", "--replicas=2", "-n", validationNamespace)
			_, err = utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			// Verify leader election still works after scale down
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "pods", "-l", "app=scaling-test,sts-role=writer",
					"-n", validationNamespace, "--no-headers")
				output, err := utils.Run(cmd)
				if err != nil {
					return false
				}
				writerCount := len(strings.Split(strings.TrimSpace(string(output)), "\n"))
				if strings.TrimSpace(string(output)) == "" {
					writerCount = 0
				}

				cmd = exec.Command("kubectl", "get", "pods", "-l", "app=scaling-test,sts-role=reader",
					"-n", validationNamespace, "--no-headers")
				output, err = utils.Run(cmd)
				if err != nil {
					return false
				}
				readerCount := len(strings.Split(strings.TrimSpace(string(output)), "\n"))
				if strings.TrimSpace(string(output)) == "" {
					readerCount = 0
				}

				return writerCount == 1 && readerCount == 1
			}, testTimeout, testInterval).Should(BeTrue())
		})
	})
})
