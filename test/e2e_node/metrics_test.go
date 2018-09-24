/*
Copyright 2018 The Kubernetes Authors.

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

package e2e_node

import (
	"fmt"
	"time"

	corestats "k8s.io/kubernetes/pkg/kubelet/apis/corestats/v1alpha1"
	"k8s.io/kubernetes/test/e2e/framework"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
)

var _ = framework.KubeDescribe("GRPCMetrics API [NodeConformance]", func() {
	f := framework.NewDefaultFramework("grpc-metrics-test")
	Context("when querying /core", func() {
		AfterEach(func() {
			if !CurrentGinkgoTestDescription().Failed {
				return
			}
			if framework.TestContext.DumpLogsOnFailure {
				framework.LogFailedContainers(f.ClientSet, f.Namespace.Name, framework.Logf)
			}
			By("Recording processes in system cgroups")
			recordSystemCgroupProcesses()
		})
		It("should report resource usage through the core stats api", func() {
			const pod0 = "stats-busybox-0"
			const pod1 = "stats-busybox-1"

			By("Creating test pods")
			numRestarts := int32(1)
			pods := getSummaryTestPods(f, numRestarts, pod0, pod1)
			f.PodClient().CreateBatch(pods)

			Eventually(func() error {
				for _, pod := range pods {
					err := verifyPodRestartCount(f, pod.Name, len(pod.Spec.Containers), numRestarts)
					if err != nil {
						return err
					}
				}
				return nil
			}, time.Minute, 5*time.Second).Should(BeNil())

			// Wait for cAdvisor to collect 2 stats points
			time.Sleep(15 * time.Second)

			// Setup expectations.
			const (
				maxStartAge = time.Hour * 24 * 365 // 1 year
				maxStatsAge = time.Minute
			)
			// fetch node so we can know proper node memory bounds for unconstrained cgroups
			node := getLocalNode(f)
			memoryCapacity := node.Status.Capacity["memory"]
			memoryLimit := memoryCapacity.Value()
			// Expectations for system containers.
			usageExpectations := ptrMatchAllFields(gstruct.Fields{
				"CpuUsageCoreNanoSeconds": And(
					BeNumerically(">=", 10000000),
					BeNumerically("<=", 1E15)),
				"MemoryWorkingSetBytes": And(
					BeNumerically(">=", 1*framework.Kb),
					BeNumerically("<=", memoryLimit)),
				"EphemeralStorageUsageBytes": And(
					BeNumerically(">=", framework.Kb),
					BeNumerically("<=", 100000*framework.Mb)),
			})
			podExpectations := ptrMatchAllFields(gstruct.Fields{
				"Name":      gstruct.Ignore(),
				"Namespace": gstruct.Ignore(),
				"Usage":     usageExpectations,
				"Containers": gstruct.MatchAllElements(grpcCoreObjectID, gstruct.Elements{
					"busybox-container": ptrMatchAllFields(gstruct.Fields{
						"Name":  Equal("busybox-container"),
						"Usage": usageExpectations,
					}),
				}),
			})
			matchExpectations := ptrMatchAllFields(gstruct.Fields{
				"Node": usageExpectations,
				"Pods": gstruct.MatchElements(grpcCoreObjectID, gstruct.IgnoreExtras, gstruct.Elements{
					fmt.Sprintf("%s::%s", f.Namespace.Name, pod0): podExpectations,
					fmt.Sprintf("%s::%s", f.Namespace.Name, pod1): podExpectations,
				}),
			})

			By("Validating /core")
			// Give pods a minute to actually start up.
			Eventually(getGRPCCoreStats, 1*time.Minute, 15*time.Second).Should(matchExpectations)
			// Then the summary should match the expectations a few more times.
			Consistently(getGRPCCoreStats, 30*time.Second, 15*time.Second).Should(matchExpectations)
		})
	})
})

// Mapping function for gstruct.MatchAllElements
func grpcCoreObjectID(element interface{}) string {
	switch el := element.(type) {
	case *corestats.PodUsage:
		return fmt.Sprintf("%s::%s", el.Namespace, el.Name)
	case *corestats.ContainerUsage:
		return el.Name
	default:
		framework.Failf("Unknown type: %T", el)
		return "???"
	}
}
