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

package deployment

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	oteltrace "go.opentelemetry.io/otel/trace"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/component-base/tracing"
	"k8s.io/klog/v2/ktesting"
	"k8s.io/kubernetes/pkg/controller/testutil"
)

func TestSyncDeploymentTracing(t *testing.T) {
	_, ctx := ktesting.NewTestContext(t)

	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSpanProcessor(recorder),
	)
	defer func() { _ = tp.Shutdown(context.Background()) }()

	ctx = tracing.ContextWithTracerProvider(ctx, tp)

	f := newFixture(t)
	d := newDeployment("foo", 1, nil, nil, nil, map[string]string{"foo": "bar"})
	d.UID = types.UID("deploy-uid-123")
	d.Annotations[tracing.AnnotationPrefix+"traceparent"] = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"

	f.dLister = append(f.dLister, d)
	f.objects = append(f.objects, d)

	createdRS := newReplicaSet(d, "foo-42", 1)
	f.expectCreateRSAction(createdRS)
	f.expectUpdateDeploymentStatusAction(d)
	f.expectUpdateDeploymentStatusAction(d)

	f.run(ctx, testutil.GetKey(d, t))

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 recorded span, got %d", len(spans))
	}
	span := spans[0]
	if span.Name() != "reconcile Deployment" {
		t.Errorf("expected span name %q, got %q", "reconcile Deployment", span.Name())
	}
	if span.Parent().IsValid() {
		t.Errorf("expected independent root span, got parent %+v", span.Parent())
	}
	if len(span.Links()) != 1 {
		t.Fatalf("expected 1 span link to triggering mutation, got %d", len(span.Links()))
	}
	expectedTriggerTraceID, _ := oteltrace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	expectedTriggerSpanID, _ := oteltrace.SpanIDFromHex("00f067aa0ba902b7")
	if span.Links()[0].SpanContext.TraceID() != expectedTriggerTraceID || span.Links()[0].SpanContext.SpanID() != expectedTriggerSpanID {
		t.Errorf("unexpected span link context: %+v", span.Links()[0].SpanContext)
	}

	attrs := make(map[attribute.Key]attribute.Value)
	for _, a := range span.Attributes() {
		attrs[a.Key] = a.Value
	}
	if got := attrs[semconv.K8SDeploymentNameKey].AsString(); got != "foo" {
		t.Errorf("expected %s=foo, got %q", semconv.K8SDeploymentNameKey, got)
	}
	if got := attrs[semconv.K8SDeploymentUIDKey].AsString(); got != "deploy-uid-123" {
		t.Errorf("expected %s=deploy-uid-123, got %q", semconv.K8SDeploymentUIDKey, got)
	}
	if got := attrs[semconv.K8SNamespaceNameKey].AsString(); got != d.Namespace {
		t.Errorf("expected %s=%s, got %q", semconv.K8SNamespaceNameKey, d.Namespace, got)
	}
}
