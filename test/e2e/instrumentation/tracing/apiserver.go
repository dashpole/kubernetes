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
	"strings"
	"time"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/kubernetes/test/e2e/framework"
	e2eskipper "k8s.io/kubernetes/test/e2e/framework/skipper"
	e2essh "k8s.io/kubernetes/test/e2e/framework/ssh"
	instrumentation "k8s.io/kubernetes/test/e2e/instrumentation/common"
)

// The API Server Tracing test ensures that an opentelemetry collector can
// collect traces from the API Server, and that context is correctly propagated
var _ = instrumentation.SIGDescribe("[Feature:APIServerTracing]", func() {
	var c clientset.Interface

	ginkgo.BeforeEach(func() {
		config, err := framework.LoadConfig()
		framework.ExpectNoError(err)
		c, err = clientset.NewForConfig(config)
		framework.ExpectNoError(err)
	})

	ginkgo.It("should send a request with a sampled trace context, and observe child spans in the apiserver logs", func() {
		e2eskipper.SkipUnlessSSHKeyPresent()

		ginkgo.By("Setting up OpenTelemetry to sample all requests")
		tp := sdktrace.NewTracerProvider(
			sdktrace.WithConfig(sdktrace.Config{
				DefaultSampler: sdktrace.AlwaysSample()},
			))
		otel.SetTracerProvider(tp)
		otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))

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
				"sudo cat /var/log/kube-apiserver.log",
				framework.APIAddress()+":22",
				framework.TestContext.Provider,
			)
			logs := result.Stdout
			if err != nil {
				return err
			}
			if result.Stderr != "" {
				return fmt.Errorf("non-empty stderr when querying for logs on the master: %v", result.Stderr)
			}
			// Check the opentelemetry collector logs to see if they contain our trace ID
			if strings.Contains(logs, traceID.String()) {
				return nil
			}
			return fmt.Errorf("failed to find trace ID %v in log: \n%v", traceID.String(), logs)
		}, 1*time.Minute, 10*time.Second).Should(gomega.BeNil())

	})
})
