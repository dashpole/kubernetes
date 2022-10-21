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
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	traceservice "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	tracev1 "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/grpc"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	genericfeatures "k8s.io/apiserver/pkg/features"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	client "k8s.io/client-go/kubernetes"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
	kubeapiservertesting "k8s.io/kubernetes/cmd/kube-apiserver/app/testing"
	"k8s.io/kubernetes/test/integration/framework"
)

func TestAPIServerTracing(t *testing.T) {
	// Listen for traces from the API Server before starting it, so the
	// API Server will successfully connect right away during the test.
	listener, err := net.Listen("tcp", "localhost:")
	if err != nil {
		t.Fatal(err)
	}
	// Write the configuration for tracing to a file
	tracingConfigFile, err := os.CreateTemp("", "tracing-config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tracingConfigFile.Name())

	if err := os.WriteFile(tracingConfigFile.Name(), []byte(fmt.Sprintf(`
apiVersion: apiserver.config.k8s.io/v1alpha1
kind: TracingConfiguration
samplingRatePerMillion: 1000000
endpoint: %s`, listener.Addr().String())), os.FileMode(0755)); err != nil {
		t.Fatal(err)
	}
	testAPIServerTracing(t,
		listener,
		[]string{"--tracing-config-file=" + tracingConfigFile.Name()},
	)
}

func TestAPIServerTracingWithEgressSelector(t *testing.T) {
	// Listen for traces from the API Server before starting it, so the
	// API Server will successfully connect right away during the test.
	listener, err := net.Listen("tcp", "localhost:")
	if err != nil {
		t.Fatal(err)
	}
	// Use an egress selector which doesn't have a controlplane config to ensure
	// tracing works in that context. Write the egress selector configuration to a file.
	egressSelectorConfigFile, err := os.CreateTemp("", "egress_selector_configuration.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(egressSelectorConfigFile.Name())

	if err := os.WriteFile(egressSelectorConfigFile.Name(), []byte(`
apiVersion: apiserver.config.k8s.io/v1beta1
kind: EgressSelectorConfiguration
egressSelections:
- name: cluster
  connection:
    proxyProtocol: Direct
    transport:`), os.FileMode(0755)); err != nil {
		t.Fatal(err)
	}

	// Write the configuration for tracing to a file
	tracingConfigFile, err := os.CreateTemp("", "tracing-config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tracingConfigFile.Name())

	if err := os.WriteFile(tracingConfigFile.Name(), []byte(fmt.Sprintf(`
apiVersion: apiserver.config.k8s.io/v1alpha1
kind: TracingConfiguration
samplingRatePerMillion: 1000000
endpoint: %s`, listener.Addr().String())), os.FileMode(0755)); err != nil {
		t.Fatal(err)
	}
	testAPIServerTracing(t,
		listener,
		[]string{
			"--tracing-config-file=" + tracingConfigFile.Name(),
			"--egress-selector-config-file=" + egressSelectorConfigFile.Name(),
		},
	)
}

func testAPIServerTracing(t *testing.T, listener net.Listener, apiserverArgs []string) {
	defer featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, genericfeatures.APIServerTracing, true)()

	traceFound := make(chan struct{})
	defer close(traceFound)
	srv := grpc.NewServer()
	fakeServer := &traceServer{t: t}
	fakeServer.resetExpectations([]*spanExpectation{})
	traceservice.RegisterTraceServiceServer(srv, fakeServer)

	go srv.Serve(listener)
	defer srv.Stop()

	// Start the API Server with our tracing configuration
	testServer := kubeapiservertesting.StartTestServerOrDie(t,
		kubeapiservertesting.NewDefaultTestServerOptions(),
		apiserverArgs,
		framework.SharedEtcd(),
	)
	defer testServer.TearDownFn()
	clientSet, err := client.NewForConfig(testServer.ClientConfig)
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		desc          string
		apiCall       func(*client.Clientset) error
		expectedTrace []*spanExpectation
	}{
		{
			desc: "create node",
			apiCall: func(c *client.Clientset) error {
				_, err = clientSet.CoreV1().Nodes().Create(context.Background(),
					&v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "fake"}}, metav1.CreateOptions{})
				return err
			},
			expectedTrace: []*spanExpectation{
				{
					name: "KubernetesAPI",
					attributes: map[string]func(*commonv1.AnyValue) bool{
						"http.user_agent": func(v *commonv1.AnyValue) bool {
							return strings.HasPrefix(v.GetStringValue(), "tracing.test")
						},
						"http.target": func(v *commonv1.AnyValue) bool {
							return v.GetStringValue() == "/api/v1/nodes"
						},
						"http.method": func(v *commonv1.AnyValue) bool {
							return v.GetStringValue() == "POST"
						},
					},
				},
				{
					name: "etcdserverpb.KV/Txn",
					attributes: map[string]func(*commonv1.AnyValue) bool{
						"rpc.system": func(v *commonv1.AnyValue) bool {
							return v.GetStringValue() == "grpc"
						},
					},
					events: []string{"message"},
				},
			},
		},
		{
			desc: "get node",
			apiCall: func(c *client.Clientset) error {
				_, err = clientSet.CoreV1().Nodes().Get(context.Background(), "fake", metav1.GetOptions{})
				return err
			},
			expectedTrace: []*spanExpectation{
				{
					name: "KubernetesAPI",
					attributes: map[string]func(*commonv1.AnyValue) bool{
						"http.user_agent": func(v *commonv1.AnyValue) bool {
							return strings.HasPrefix(v.GetStringValue(), "tracing.test")
						},
						"http.target": func(v *commonv1.AnyValue) bool {
							return v.GetStringValue() == "/api/v1/nodes/fake"
						},
						"http.method": func(v *commonv1.AnyValue) bool {
							return v.GetStringValue() == "GET"
						},
					},
				},
				{
					name: "etcdserverpb.KV/Txn",
					attributes: map[string]func(*commonv1.AnyValue) bool{
						"rpc.system": func(v *commonv1.AnyValue) bool {
							return v.GetStringValue() == "grpc"
						},
					},
					events: []string{"message"},
				},
			},
		},
		{
			desc: "list nodes",
			apiCall: func(c *client.Clientset) error {
				_, err = clientSet.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
				return err
			},
			expectedTrace: []*spanExpectation{
				{
					name: "KubernetesAPI",
					attributes: map[string]func(*commonv1.AnyValue) bool{
						"http.user_agent": func(v *commonv1.AnyValue) bool {
							return strings.HasPrefix(v.GetStringValue(), "tracing.test")
						},
						"http.target": func(v *commonv1.AnyValue) bool {
							return v.GetStringValue() == "/api/v1/nodes"
						},
						"http.method": func(v *commonv1.AnyValue) bool {
							return v.GetStringValue() == "GET"
						},
					},
				},
				{
					name: "etcdserverpb.KV/Txn",
					attributes: map[string]func(*commonv1.AnyValue) bool{
						"rpc.system": func(v *commonv1.AnyValue) bool {
							return v.GetStringValue() == "grpc"
						},
					},
					events: []string{"message"},
				},
			},
		},
		{
			desc: "update node",
			apiCall: func(c *client.Clientset) error {
				_, err = clientSet.CoreV1().Nodes().Update(context.Background(),
					&v1.Node{ObjectMeta: metav1.ObjectMeta{
						Name:        "fake",
						Annotations: map[string]string{"foo": "bar"},
					}}, metav1.UpdateOptions{})
				return err
			},
			expectedTrace: []*spanExpectation{
				{
					name: "KubernetesAPI",
					attributes: map[string]func(*commonv1.AnyValue) bool{
						"http.user_agent": func(v *commonv1.AnyValue) bool {
							return strings.HasPrefix(v.GetStringValue(), "tracing.test")
						},
						"http.target": func(v *commonv1.AnyValue) bool {
							return v.GetStringValue() == "/api/v1/nodes/fake"
						},
						"http.method": func(v *commonv1.AnyValue) bool {
							return v.GetStringValue() == "PUT"
						},
					},
				},
				{
					name: "etcdserverpb.KV/Txn",
					attributes: map[string]func(*commonv1.AnyValue) bool{
						"rpc.system": func(v *commonv1.AnyValue) bool {
							return v.GetStringValue() == "grpc"
						},
					},
					events: []string{"message"},
				},
			},
		},
		{
			desc: "patch node",
			apiCall: func(c *client.Clientset) error {
				oldNode := &v1.Node{ObjectMeta: metav1.ObjectMeta{
					Name:        "fake",
					Annotations: map[string]string{"foo": "bar"},
				}}
				oldData, err := json.Marshal(oldNode)
				if err != nil {
					return err
				}
				newNode := &v1.Node{ObjectMeta: metav1.ObjectMeta{
					Name:        "fake",
					Annotations: map[string]string{"foo": "bar"},
					Labels:      map[string]string{"hello": "world"},
				}}
				newData, err := json.Marshal(newNode)
				if err != nil {
					return err
				}

				patchBytes, err := strategicpatch.CreateTwoWayMergePatch(oldData, newData, v1.Node{})
				if err != nil {
					return err
				}
				_, err = clientSet.CoreV1().Nodes().Patch(context.Background(), "fake", types.StrategicMergePatchType, patchBytes, metav1.PatchOptions{})
				return err
			},
			expectedTrace: []*spanExpectation{
				{
					name: "KubernetesAPI",
					attributes: map[string]func(*commonv1.AnyValue) bool{
						"http.user_agent": func(v *commonv1.AnyValue) bool {
							return strings.HasPrefix(v.GetStringValue(), "tracing.test")
						},
						"http.target": func(v *commonv1.AnyValue) bool {
							return v.GetStringValue() == "/api/v1/nodes/fake"
						},
						"http.method": func(v *commonv1.AnyValue) bool {
							return v.GetStringValue() == "PATCH"
						},
					},
				},
				{
					name: "etcdserverpb.KV/Txn",
					attributes: map[string]func(*commonv1.AnyValue) bool{
						"rpc.system": func(v *commonv1.AnyValue) bool {
							return v.GetStringValue() == "grpc"
						},
					},
					events: []string{"message"},
				},
			},
		},
		{
			desc: "delete node",
			apiCall: func(c *client.Clientset) error {
				return clientSet.CoreV1().Nodes().Delete(context.Background(), "fake", metav1.DeleteOptions{})
			},
			expectedTrace: []*spanExpectation{
				{
					name: "KubernetesAPI",
					attributes: map[string]func(*commonv1.AnyValue) bool{
						"http.user_agent": func(v *commonv1.AnyValue) bool {
							return strings.HasPrefix(v.GetStringValue(), "tracing.test")
						},
						"http.target": func(v *commonv1.AnyValue) bool {
							return v.GetStringValue() == "/api/v1/nodes/fake"
						},
						"http.method": func(v *commonv1.AnyValue) bool {
							return v.GetStringValue() == "DELETE"
						},
					},
				},
				{
					name: "etcdserverpb.KV/Txn",
					attributes: map[string]func(*commonv1.AnyValue) bool{
						"rpc.system": func(v *commonv1.AnyValue) bool {
							return v.GetStringValue() == "grpc"
						},
					},
					events: []string{"message"},
				},
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			fakeServer.resetExpectations(tc.expectedTrace)

			// Make our call to the API server
			if err := tc.apiCall(clientSet); err != nil {
				t.Fatal(err)
			}

			// Wait for a span to be recorded from our request
			select {
			case <-fakeServer.traceFound:
			case <-time.After(30 * time.Second):
				t.Fatal("Timed out waiting for trace")
			}
		})
	}
}

// spanExpectation is the expectation for a single span
type spanExpectation struct {
	name       string
	attributes attributeExpectation
	events     eventExpectation
	met        bool
}

// eventExpectation is the expectation for an event attached to a span.
// It is comprised of event names.
type eventExpectation []string

// matches returns true if all expected events exist in the list of input events.
func (e eventExpectation) matches(events []*tracev1.Span_Event) bool {
	for _, wantEvent := range e {
		var matched bool
		for _, event := range events {
			if event.Name == wantEvent {
				matched = true
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

// eventExpectation is the expectation for an event attached to a span.
// It is a map from attribute key, to a value-matching function.
type attributeExpectation map[string]func(*commonv1.AnyValue) bool

// matches returns true if all expected attributes exist in the intput list of attributes.
func (a attributeExpectation) matches(attrs []*commonv1.KeyValue) bool {
	for key, checkVal := range a {
		var matched bool
		for _, attr := range attrs {
			if attr.GetKey() != key {
				continue
			}
			if !checkVal(attr.GetValue()) {
				return false
			}
			matched = true
		}
		if !matched {
			return false
		}
	}
	return true
}

// traceServer implements TracesServiceServer, which can receive spans from the
// API Server via OTLP.
type traceServer struct {
	t *testing.T
	traceservice.UnimplementedTraceServiceServer
	// the lock guards the per-scenario state below
	lock         sync.Mutex
	traceFound   chan struct{}
	expectations []*spanExpectation
}

func (t *traceServer) Export(ctx context.Context, req *traceservice.ExportTraceServiceRequest) (*traceservice.ExportTraceServiceResponse, error) {
	t.lock.Lock()
	defer t.lock.Unlock()

	// updateExpectations marks all expectations met by the request as met
	t.updateExpectations(req)
	// if all expectations are met, notify the test scenario by closing traceFound
	if t.allExpectationsMet() {
		select {
		case <-t.traceFound:
			// traceFound is already closed
		default:
			close(t.traceFound)
		}
	}
	return &traceservice.ExportTraceServiceResponse{}, nil
}

// allExpectationsMet returns true if all span expectations the server is
// looking for have been satisfied.
func (t *traceServer) allExpectationsMet() bool {
	for _, expectation := range t.expectations {
		if !expectation.met {
			return false
		}
	}
	return true
}

// updateExpectations finds all expectations that are met by a span in the
// incoming request.
func (t *traceServer) updateExpectations(req *traceservice.ExportTraceServiceRequest) {
	for _, resourceSpans := range req.GetResourceSpans() {
		for _, instrumentationSpans := range resourceSpans.GetScopeSpans() {
			for _, expectation := range t.expectations {
				for _, span := range instrumentationSpans.GetSpans() {
					if span.Name != expectation.name {
						continue
					}
					if !expectation.attributes.matches(span.GetAttributes()) {
						continue
					}
					if !expectation.events.matches(span.GetEvents()) {
						continue
					}
					t.t.Logf("span found: %+v", span)
					expectation.met = true
				}
			}
		}
	}
}

// resetExpectations is used by a new test scenario to set new expectations for
// the test server.
func (t *traceServer) resetExpectations(newExpectations []*spanExpectation) {
	t.lock.Lock()
	defer t.lock.Unlock()
	t.traceFound = make(chan struct{})
	t.expectations = newExpectations
}
