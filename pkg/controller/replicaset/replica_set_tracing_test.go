/*
Copyright 2026 The Kubernetes Authors.

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

package replicaset

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	oteltrace "go.opentelemetry.io/otel/trace"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/component-base/tracing"
	"k8s.io/ktesting"
	"k8s.io/kubernetes/pkg/controller"
	"k8s.io/kubernetes/pkg/controller/testutil"
)

func TestSyncReplicaSetTracing(t *testing.T) {
	_, ctx := ktesting.NewTestContext(t)

	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSpanProcessor(recorder),
	)
	defer func() { _ = tp.Shutdown(context.Background()) }()

	ctx = tracing.ContextWithTracerProvider(ctx, tp)

	labelMap := map[string]string{"foo": "bar"}
	rs := newReplicaSet(1, labelMap)
	rs.UID = types.UID("rs-uid-456")
	rs.Annotations = map[string]string{
		tracing.AnnotationPrefix + "traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
	}

	client := fake.NewSimpleClientset(rs)
	stopCh := make(chan struct{})
	defer close(stopCh)
	manager, informers := testNewReplicaSetControllerFromClient(t, client, stopCh, BurstReplicas)
	informers.Apps().V1().ReplicaSets().Informer().GetIndexer().Add(rs)

	fakePodControl := controller.FakePodControl{}
	manager.podControl = &fakePodControl

	if err := manager.syncReplicaSet(ctx, testutil.GetKey(rs, t)); err != nil {
		t.Fatalf("unexpected syncReplicaSet error: %v", err)
	}

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 recorded span, got %d", len(spans))
	}
	span := spans[0]
	if span.Name() != "reconcile ReplicaSet" {
		t.Errorf("expected span name %q, got %q", "reconcile ReplicaSet", span.Name())
	}
	if span.Parent().IsValid() {
		t.Errorf("expected independent root span, got parent %+v", span.Parent())
	}
	if len(span.Links()) != 1 {
		t.Fatalf("expected 1 span link, got %d", len(span.Links()))
	}
	expectedTraceID, _ := oteltrace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	if span.Links()[0].SpanContext.TraceID() != expectedTraceID {
		t.Errorf("unexpected linked trace ID: %s", span.Links()[0].SpanContext.TraceID())
	}

	attrs := make(map[attribute.Key]attribute.Value)
	for _, a := range span.Attributes() {
		attrs[a.Key] = a.Value
	}
	if got := attrs[semconv.K8SReplicaSetNameKey].AsString(); got != rs.Name {
		t.Errorf("expected %s=%s, got %q", semconv.K8SReplicaSetNameKey, rs.Name, got)
	}
	if got := attrs[semconv.K8SReplicaSetUIDKey].AsString(); got != "rs-uid-456" {
		t.Errorf("expected %s=rs-uid-456, got %q", semconv.K8SReplicaSetUIDKey, got)
	}
	if got := attrs[semconv.K8SNamespaceNameKey].AsString(); got != rs.Namespace {
		t.Errorf("expected %s=%s, got %q", semconv.K8SNamespaceNameKey, rs.Namespace, got)
	}
}
