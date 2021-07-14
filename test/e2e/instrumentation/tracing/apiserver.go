/*
Copyright 2021 The Kubernetes Authors.

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
	"net/http"
	"strings"
	"time"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/component-base/traces"
	"k8s.io/kubernetes/test/e2e/framework"
	e2epod "k8s.io/kubernetes/test/e2e/framework/pod"
	e2eskipper "k8s.io/kubernetes/test/e2e/framework/skipper"
	e2essh "k8s.io/kubernetes/test/e2e/framework/ssh"
	instrumentation "k8s.io/kubernetes/test/e2e/instrumentation/common"
)

const (
	podName       = "otel-collector"
	containerName = "collector"
	httpTarget    = "/api/v1/nodes"
)

// The API Server Tracing test ensures that an opentelemetry collector can
// collect traces from the API Server, and that context is correctly propagated
// Spans are sent:  API Server --> OpenTelemetry Collector Pod --> Logs
var _ = instrumentation.SIGDescribe("[Feature:APIServerTracing]", func() {
	f := framework.NewDefaultFramework("apiserver-tracing")
	var c clientset.Interface
	var otelPod *v1.Pod

	ginkgo.BeforeEach(func() {
		config, err := framework.LoadConfig()
		framework.ExpectNoError(err)
		// Use otelhttp with the client-go client to ensure context is propagated.
		config.Wrap(func(rt http.RoundTripper) http.RoundTripper {
			return otelhttp.NewTransport(rt)
		})
		c, err = clientset.NewForConfig(config)
		framework.ExpectNoError(err)

		ginkgo.By("Creating an opentelemetry collector on the master node, which logs spans to stdout.")
		masterNode, err := masterNodeName(c)
		framework.ExpectNoError(err)
		otelPod, err = c.CoreV1().Pods(f.Namespace.Name).Create(context.Background(), opentelemetryCollectorPod(masterNode), metav1.CreateOptions{})
		framework.ExpectNoError(err)

		_, err = c.CoreV1().ConfigMaps(f.Namespace.Name).Create(context.Background(), opentelemetryConfigmap(), metav1.CreateOptions{})
		framework.ExpectNoError(err)

		ready := e2epod.CheckPodsRunningReady(f.ClientSet, f.Namespace.Name, []string{podName}, 20*time.Second)
		framework.ExpectEqual(ready, true)
	})

	ginkgo.It("should send a request with a sampled trace context, and observe child spans from the collector pod", func() {
		e2eskipper.SkipUnlessSSHKeyPresent()

		ginkgo.By("Setting up OpenTelemetry.")
		tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
		otel.SetTracerProvider(tp)
		otel.SetTextMapPropagator(traces.Propagators())

		ginkgo.By("Creating a context with a sampled parent span.")
		// Note: After https://github.com/open-telemetry/opentelemetry-go/issues/2031
		// is fixed, and we update our opentelemetry-go version, we won't be
		// able to control sampling with parent spans anymore.  We will need to
		// resolve https://github.com/kubernetes/kubernetes/issues/103186 in
		// order to make this work again.
		ctx, _ := tp.Tracer("apiservertest").Start(context.Background(), "OpenTelemetrySpan")

		ginkgo.By(fmt.Sprintf("Checking for http target: %v in logs.", httpTarget))
		gomega.Eventually(func() error {
			// Send any request using the context with a sampled span
			_, err := c.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
			if err != nil {
				return err
			}

			// Get logs from the opentelemetry collector pod on the master.
			// We use SSH because we can't fetch logs from master pods using
			// the pod logs subresource.
			result, err := e2essh.SSH(
				fmt.Sprintf("sudo cat /var/log/pods/%v_%v_%v/%v/*", f.Namespace.Name, podName, otelPod.UID, containerName),
				framework.APIAddress()+":22",
				framework.TestContext.Provider,
			)
			if err != nil {
				return err
			}
			if result.Stderr != "" {
				return fmt.Errorf("non-empty stderr when querying for logs on the master: %v", result.Stderr)
			}
			logs := result.Stdout
			// Check the opentelemetry collector logs to see if they contain
			// our request. Since the apiserver has a ParentBased(NeverSample)
			// sampler, it will only generate spans for our request with
			// sampled parent above, and not for other requests.
			if strings.Contains(logs, httpTarget) {
				return nil
			}
			return fmt.Errorf("failed to find trace ID %v in log: \n%v", httpTarget, logs)
		}, 1*time.Minute, 10*time.Second).Should(gomega.BeNil())

	})
})

// masterNodeName fetches the first master node if there are multiple.
// This won't work well for clusters with more than one master node.
func masterNodeName(c clientset.Interface) (string, error) {
	masterName := framework.TestContext.CloudConfig.MasterName
	if masterName == "" {
		masterName = framework.APIAddress()
	}
	_, err := c.CoreV1().Nodes().Get(context.Background(), masterName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to find master node %s.  Specify it with --kube-master", masterName)
	}
	return masterName, nil
}

func opentelemetryCollectorPod(masterNode string) *v1.Pod {
	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: podName,
		},
		Spec: v1.PodSpec{
			NodeName: masterNode,
			Containers: []v1.Container{{
				Name:  containerName,
				Image: "otel/opentelemetry-collector:0.29.0",
				Args: []string{
					"--config=/conf/otel-collector-config.yaml",
					"--log-level=DEBUG",
				},
				Ports: []v1.ContainerPort{{
					ContainerPort: 4317,
					HostPort:      4317,
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
