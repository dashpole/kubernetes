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

package tracing

import (
	"context"
	"reflect"
	"testing"

	"go.opentelemetry.io/otel/baggage"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	oteltrace "go.opentelemetry.io/otel/trace"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestObjectCarrier(t *testing.T) {
	var nilMeta *metav1.ObjectMeta
	nilCarrier := NewObjectCarrier(nilMeta)
	if got := nilCarrier.Get("traceparent"); got != "" {
		t.Fatalf("expected empty string from nil object carrier, got %q", got)
	}
	nilCarrier.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4737-00f067aa0ba902b7-01")
	if keys := nilCarrier.Keys(); keys != nil {
		t.Fatalf("expected nil keys from nil object carrier, got %v", keys)
	}

	obj := &metav1.ObjectMeta{Name: "demo", Namespace: "default"}
	carrier := NewObjectCarrier(obj)
	if got := carrier.Get("traceparent"); got != "" {
		t.Fatalf("expected empty string on unannotated object, got %q", got)
	}
	if keys := carrier.Keys(); keys != nil {
		t.Fatalf("expected nil keys on unannotated object, got %v", keys)
	}

	traceID, _ := oteltrace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4737")
	spanID, _ := oteltrace.SpanIDFromHex("00f067aa0ba902b7")
	ts, _ := oteltrace.ParseTraceState("ot=p:8")
	sc := oteltrace.NewSpanContext(oteltrace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: oteltrace.FlagsSampled,
		TraceState: ts,
		Remote:     true,
	})
	member, _ := baggage.NewMember("env", "prod")
	bag, _ := baggage.New(member)

	ctx := oteltrace.ContextWithRemoteSpanContext(context.Background(), sc)
	ctx = baggage.ContextWithBaggage(ctx, bag)
	Propagators().Inject(ctx, carrier)

	if got := obj.GetAnnotations()[AnnotationPrefix+"traceparent"]; got != "00-4bf92f3577b34da6a3ce929d0e0e4737-00f067aa0ba902b7-01" {
		t.Fatalf("unexpected traceparent annotation: %q", got)
	}
	if got := obj.GetAnnotations()[AnnotationPrefix+"tracestate"]; got != "ot=p:8" {
		t.Fatalf("unexpected tracestate annotation: %q", got)
	}
	if got := obj.GetAnnotations()[AnnotationPrefix+"baggage"]; got != "env=prod" {
		t.Fatalf("unexpected baggage annotation: %q", got)
	}

	wantKeys := []string{"baggage", "traceparent", "tracestate"}
	if gotKeys := carrier.Keys(); !reflect.DeepEqual(gotKeys, wantKeys) {
		t.Fatalf("carrier.Keys() = %v, want %v", gotKeys, wantKeys)
	}

	extractedCtx := Propagators().Extract(context.Background(), carrier)
	extractedSC := oteltrace.SpanContextFromContext(extractedCtx)
	if !extractedSC.IsValid() || extractedSC.TraceID() != traceID || extractedSC.SpanID() != spanID || !extractedSC.IsSampled() {
		t.Fatalf("unexpected extracted SpanContext: %+v", extractedSC)
	}
	if gotTS := extractedSC.TraceState().String(); gotTS != "ot=p:8" {
		t.Fatalf("unexpected extracted TraceState: %q", gotTS)
	}
	if gotBag := baggage.FromContext(extractedCtx).Member("env").Value(); gotBag != "prod" {
		t.Fatalf("unexpected extracted baggage: %q", gotBag)
	}
}

func TestWithObjectsAndLinkBasedSampling(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(newLinkBasedSampler(sdktrace.ParentBased(sdktrace.NeverSample()))),
		sdktrace.WithSpanProcessor(recorder),
	)
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
	})
	tracer := tp.Tracer(instrumentationScope)

	// 1. Sampled object annotation triggers link-based sampling even when root sampler is NeverSample.
	sampledObj := &metav1.ObjectMeta{
		Name:      "web",
		Namespace: "default",
		Annotations: map[string]string{
			AnnotationPrefix + "traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4737-00f067aa0ba902b7-01",
			AnnotationPrefix + "tracestate":  "ot=p:8",
		},
	}

	_, span := tracer.Start(context.Background(), "reconcile Deployment", WithObjects(sampledObj))
	span.End()

	ended := recorder.Ended()
	if len(ended) != 1 {
		t.Fatalf("expected 1 recorded span, got %d", len(ended))
	}
	got := ended[0]
	if got.Parent().IsValid() {
		t.Fatalf("expected root span without parent, got parent %+v", got.Parent())
	}
	if len(got.Links()) != 1 {
		t.Fatalf("expected 1 span link, got %d", len(got.Links()))
	}
	if got.Links()[0].SpanContext.TraceID().String() != "4bf92f3577b34da6a3ce929d0e0e4737" {
		t.Fatalf("unexpected linked TraceID: %s", got.Links()[0].SpanContext.TraceID())
	}
	if got.SpanContext().TraceState().String() != "ot=p:8" {
		t.Fatalf("expected inherited TraceState ot=p:8, got %q", got.SpanContext().TraceState())
	}

	// 2. Unsampled object annotation does not sample when fallback is NeverSample.
	unsampledObj := &metav1.ObjectMeta{
		Name:      "web",
		Namespace: "default",
		Annotations: map[string]string{
			AnnotationPrefix + "traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4737-00f067aa0ba902b7-00",
		},
	}
	_, span2 := tracer.Start(context.Background(), "reconcile Deployment", WithObjects(unsampledObj))
	span2.End()
	if len(recorder.Ended()) != 1 {
		t.Fatalf("expected unsampled link not to be recorded, got %d spans", len(recorder.Ended()))
	}

	// 3. SpanKindServer root spans with sampled links (e.g., kube-apiserver public endpoints)
	// must NOT trigger link-based sampling.
	linkSC := got.Links()[0].SpanContext
	_, serverSpan := tracer.Start(
		context.Background(),
		"POST /apis/apps/v1/namespaces/{:namespace}/deployments",
		oteltrace.WithNewRoot(),
		oteltrace.WithSpanKind(oteltrace.SpanKindServer),
		oteltrace.WithLinks(oteltrace.Link{SpanContext: linkSC}),
	)
	serverSpan.End()
	if len(recorder.Ended()) != 1 {
		t.Fatalf("expected SpanKindServer public endpoint link not to trigger sampling, got %d spans", len(recorder.Ended()))
	}
}

func TestLinkTrackerCoalescedUpdates(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(newLinkBasedSampler(sdktrace.ParentBased(sdktrace.NeverSample()))),
		sdktrace.WithSpanProcessor(recorder),
	)
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
	})
	tracer := tp.Tracer(instrumentationScope)
	tracker := NewLinkTracker()

	const (
		tp1 = "00-11111111111111111111111111111111-0000000000000001-01"
		tp2 = "00-22222222222222222222222222222222-0000000000000002-01"
	)

	// Two updates to the same object (same UID) arrive via Informer events before the worker reconciles.
	v1Obj := &metav1.ObjectMeta{
		UID:       "uid-deploy-1",
		Name:      "web",
		Namespace: "default",
		Annotations: map[string]string{
			AnnotationPrefix + "traceparent": tp1,
		},
	}
	v2Obj := &metav1.ObjectMeta{
		UID:       "uid-deploy-1",
		Name:      "web",
		Namespace: "default",
		Annotations: map[string]string{
			AnnotationPrefix + "traceparent": tp2,
		},
	}
	tracker.Record(v1Obj)
	tracker.Record(v2Obj)
	// Duplicate informer observation of v2Obj (e.g., status update) is deduplicated.
	tracker.Record(v2Obj)

	_, span := tracer.Start(context.Background(), "reconcile Deployment", tracker.WithObjects(v2Obj))
	span.End()

	ended := recorder.Ended()
	if len(ended) != 1 {
		t.Fatalf("expected 1 recorded span, got %d", len(ended))
	}
	links := ended[0].Links()
	if len(links) != 2 {
		t.Fatalf("expected 2 coalesced span links, got %d", len(links))
	}
	if links[0].SpanContext.TraceID().String() != "11111111111111111111111111111111" {
		t.Fatalf("links[0] TraceID = %s, want 11111111111111111111111111111111", links[0].SpanContext.TraceID())
	}
	if links[1].SpanContext.TraceID().String() != "22222222222222222222222222222222" {
		t.Fatalf("links[1] TraceID = %s, want 22222222222222222222222222222222", links[1].SpanContext.TraceID())
	}

	// Next reconcile when no new spec mutation occurred links only to the current annotation on v2Obj.
	_, span2 := tracer.Start(context.Background(), "reconcile Deployment", tracker.WithObjects(v2Obj))
	span2.End()
	if len(recorder.Ended()) != 2 {
		t.Fatalf("expected 2 recorded spans, got %d", len(recorder.Ended()))
	}
	if len(recorder.Ended()[1].Links()) != 1 {
		t.Fatalf("expected 1 span link on subsequent reconcile, got %d", len(recorder.Ended()[1].Links()))
	}

	tracker.Forget(v2Obj)
}

func TestLinkBasedSamplerMultiLinkTraceState(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(newLinkBasedSampler(sdktrace.ParentBased(sdktrace.NeverSample()))),
		sdktrace.WithSpanProcessor(recorder),
	)
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
	})
	tracer := tp.Tracer(instrumentationScope)

	// 1. Multiple sampled links from different TraceIDs without "ot=th:..." must NOT
	// arbitrarily pick one vendor's TraceState for the new root span.
	objA := &metav1.ObjectMeta{
		UID:  "uid-a",
		Name: "a",
		Annotations: map[string]string{
			AnnotationPrefix + "traceparent": "00-11111111111111111111111111111111-0000000000000001-01",
			AnnotationPrefix + "tracestate":  "vendora=1",
		},
	}
	objB := &metav1.ObjectMeta{
		UID:  "uid-b",
		Name: "b",
		Annotations: map[string]string{
			AnnotationPrefix + "traceparent": "00-22222222222222222222222222222222-0000000000000002-01",
			AnnotationPrefix + "tracestate":  "vendorb=2",
		},
	}
	_, span1 := tracer.Start(context.Background(), "reconcile Deployment", WithObjects(objA, objB))
	span1.End()

	if gotTS := recorder.Ended()[0].SpanContext().TraceState().String(); gotTS != "" {
		t.Fatalf("expected empty root TraceState when merging distinct vendor traces, got %q", gotTS)
	}

	// 2. Multiple sampled links from different TraceIDs with Consistent Probability Sampling
	// "ot=th:..." combine by taking the minimum rejection threshold (maximum sampling probability).
	objC := &metav1.ObjectMeta{
		UID:  "uid-c",
		Name: "c",
		Annotations: map[string]string{
			AnnotationPrefix + "traceparent": "00-33333333333333333333333333333333-0000000000000003-01",
			AnnotationPrefix + "tracestate":  "vendora=1,ot=th:c;rv:11111111111111",
		},
	}
	objD := &metav1.ObjectMeta{
		UID:  "uid-d",
		Name: "d",
		Annotations: map[string]string{
			AnnotationPrefix + "traceparent": "00-44444444444444444444444444444444-0000000000000004-01",
			AnnotationPrefix + "tracestate":  "vendorb=2,ot=th:4;rv:22222222222222",
		},
	}
	_, span2 := tracer.Start(context.Background(), "reconcile Deployment", WithObjects(objC, objD))
	span2.End()

	if gotTS := recorder.Ended()[1].SpanContext().TraceState().String(); gotTS != "ot=th:4" {
		t.Fatalf("expected combined consistent probability TraceState \"ot=th:4\", got %q", gotTS)
	}
}
