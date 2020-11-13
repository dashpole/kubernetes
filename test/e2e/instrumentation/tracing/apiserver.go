/*
Copyright 2020 The Kubernetes Authors.

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

package tracing

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	"go.opentelemetry.io/otel/api/global"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/kubernetes/test/e2e/framework"
	e2epod "k8s.io/kubernetes/test/e2e/framework/pod"
	e2eskipper "k8s.io/kubernetes/test/e2e/framework/skipper"
	e2essh "k8s.io/kubernetes/test/e2e/framework/ssh"
	instrumentation "k8s.io/kubernetes/test/e2e/instrumentation/common"
)

const (
	podName       = "otel-collector"
	containerName = "collector"
)

// The API Server Tracing test ensures that an opentelemetry collector can
// collect traces from the API Server, and that context is correctly propagated
var _ = instrumentation.SIGDescribe("[Feature:APIServerTracing]", func() {
	f := framework.NewDefaultFramework("apiserver-tracing")
	var c clientset.Interface
	var otelPod *v1.Pod

	ginkgo.BeforeEach(func() {
		config, err := framework.LoadConfig()
		framework.ExpectNoError(err)
		c, err = clientset.NewForConfig(config)
		framework.ExpectNoError(err)

		ginkgo.By("Creating an opentelemetry collector on the master node, which logs spans to stdout.")
		masterNode, err := masterNodeName(c)
		framework.ExpectNoError(err)
		otelPod, err = c.CoreV1().Pods(f.Namespace.Name).Create(context.TODO(), opentelemetryCollectorPod(masterNode), metav1.CreateOptions{})
		framework.ExpectNoError(err)

		_, err = c.CoreV1().ConfigMaps(f.Namespace.Name).Create(context.TODO(), opentelemetryConfigmap(), metav1.CreateOptions{})
		framework.ExpectNoError(err)

		ready := e2epod.CheckPodsRunningReady(f.ClientSet, f.Namespace.Name, []string{podName}, 20*time.Second)
		framework.ExpectEqual(ready, true)
	})

	ginkgo.It("should send a request with a sampled trace context, and observe child spans from the collector pod", func() {
		e2eskipper.SkipUnlessSSHKeyPresent()

		ginkgo.By("Setting up OpenTelemetry to sample all requests")
		tp, err := sdktrace.NewProvider(
			sdktrace.WithConfig(sdktrace.Config{
				DefaultSampler: sdktrace.AlwaysSample()},
			))
		// This is needed because the no-op tracer doesn't propagate the SpanContext
		// https://github.com/open-telemetry/opentelemetry-go/issues/877#issuecomment-651398357
		framework.ExpectNoError(err)
		global.SetTraceProvider(tp)

		ginkgo.By("Creating a context with a sampled parent span")
		ctx, span := tp.Tracer("apiservertest").Start(context.Background(), "OpenTelemetrySpan")

		traceID := span.SpanContext().TraceID
		ginkgo.By(fmt.Sprintf("Checking for Trace ID: %v in logs.", span.SpanContext().TraceID))
		gomega.Eventually(func() error {
			// Send any request using the context with a sampled span
			_, err := c.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
			if err != nil {
				return err
			}

			// Get logs from the opentelemetry collector pod on the master.
			// We must use SSH because we can't fetch logs from master pods
			// using the pod logs subresource.
			result, err := e2essh.SSH(
				fmt.Sprintf("sudo cat /var/log/pods/%v_%v_%v/%v/*", f.Namespace.Name, podName, otelPod.UID, containerName),
				framework.APIAddress()+":22",
				framework.TestContext.Provider,
			)
			logs := result.Stdout
			if err != nil {
				return err
			}
			if result.Stderr != "" {
				return fmt.Errorf("Non-empty stderr when querying for logs on the master: %v", result.Stderr)
			}
			// Check the opentelemetry collector logs to see if they contain our trace ID
			if strings.Contains(logs, traceID.String()) {
				return nil
			}
			return fmt.Errorf("Failed to find trace ID %v in log: \n%v", traceID.String(), logs)
		}, 1*time.Minute, 10*time.Second).Should(gomega.BeNil())

	})
})

func masterNodeName(c clientset.Interface) (string, error) {
	nodes, err := c.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	framework.ExpectNoError(err)
	for _, node := range nodes.Items {
		if strings.HasSuffix(node.Name, "master") {
			return node.Name, nil
		}
	}
	return "", fmt.Errorf("Didn't find master node in list of nodes")
}

func opentelemetryCollectorPod(masterNode string) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: podName,
		},
		Spec: v1.PodSpec{
			NodeName: masterNode,
			Containers: []v1.Container{{
				Name: containerName,
				// Use the v0.4.0 release, which is compatible with the v0.3.0
				// opentelemetry-proto.  We can't upgrade opentelemetry until
				// the grpc dependency is updated.
				// https://github.com/kubernetes/kubernetes/issues/93320
				Image: "otel/opentelemetry-collector:0.4.0",
				Args: []string{
					"--config=/conf/otel-collector-config.yaml",
				},
				Ports: []v1.ContainerPort{{
					ContainerPort: 55680,
					HostPort:      55680,
				}},
				VolumeMounts: []v1.VolumeMount{{
					Name:      "otel-collector-config-vol",
					MountPath: "/conf",
				}},
			}},
			Volumes: []v1.Volume{{
				Name: "otel-collector-config-vol",
				VolumeSource: v1.VolumeSource{
					ConfigMap: &v1.ConfigMapVolumeSource{
						LocalObjectReference: v1.LocalObjectReference{
							Name: "otel-collector-conf",
						},
					},
				},
			}},
		},
	}
}

func opentelemetryConfigmap() *v1.ConfigMap {
	return &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: "otel-collector-conf",
		},
		Data: map[string]string{
			"otel-collector-config.yaml": `receivers:
  otlp:
    protocols:
      grpc:
      http:
exporters:
  logging:
    logLevel: debug
service:
  pipelines:
    traces:
      receivers: [otlp]
      exporters: [logging]`,
		},
	}
}
