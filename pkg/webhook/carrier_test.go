// Copyright 2026 The Kubernetes Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package webhook

import (
	"context"
	"slices"
	"testing"

	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestObjectCarrier_NilSafety(t *testing.T) {
	var nilCarrier ObjectCarrier
	if got := nilCarrier.Get("traceparent"); got != "" {
		t.Fatalf("expected empty string from nil carrier, got %q", got)
	}
	nilCarrier.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	if keys := nilCarrier.Keys(); keys != nil {
		t.Fatalf("expected nil keys from nil carrier, got %v", keys)
	}

	pod := &corev1.Pod{}
	c := NewObjectCarrier(pod)
	if got := c.Get("traceparent"); got != "" {
		t.Fatalf("expected empty string when annotations map is nil, got %q", got)
	}
	if keys := c.Keys(); keys != nil {
		t.Fatalf("expected nil keys when annotations map is nil, got %v", keys)
	}
}

func TestObjectCarrier_RoundTripW3CTraceContext(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "demo-pod",
			Namespace: "default",
		},
	}

	traceID, err := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	if err != nil {
		t.Fatalf("TraceIDFromHex: %v", err)
	}
	spanID, err := trace.SpanIDFromHex("00f067aa0ba902b7")
	if err != nil {
		t.Fatalf("SpanIDFromHex: %v", err)
	}
	ts, err := trace.ParseTraceState("rojo=00f067aa0ba902b7,congo=t61rcWkgMzE")
	if err != nil {
		t.Fatalf("ParseTraceState: %v", err)
	}

	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
		TraceState: ts,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), sc)

	prop := propagation.TraceContext{}
	carrier := NewObjectCarrier(pod)
	prop.Inject(ctx, carrier)

	ann := pod.GetAnnotations()
	wantTP := "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	if got := ann[AnnotationPrefix+"traceparent"]; got != wantTP {
		t.Fatalf("annotations[%q] = %q, want %q", AnnotationPrefix+"traceparent", got, wantTP)
	}
	wantTS := "rojo=00f067aa0ba902b7,congo=t61rcWkgMzE"
	if got := ann[AnnotationPrefix+"tracestate"]; got != wantTS {
		t.Fatalf("annotations[%q] = %q, want %q", AnnotationPrefix+"tracestate", got, wantTS)
	}

	keys := carrier.Keys()
	for _, expectedKey := range []string{"traceparent", "tracestate"} {
		if !slices.Contains(keys, expectedKey) {
			t.Fatalf("Keys() = %v, missing %q", keys, expectedKey)
		}
	}

	extractedCtx := prop.Extract(context.Background(), carrier)
	gotSC := trace.SpanContextFromContext(extractedCtx)
	if !gotSC.IsValid() {
		t.Fatalf("extracted SpanContext is invalid")
	}
	if gotSC.TraceID() != traceID {
		t.Fatalf("TraceID = %s, want %s", gotSC.TraceID(), traceID)
	}
	if gotSC.SpanID() != spanID {
		t.Fatalf("SpanID = %s, want %s", gotSC.SpanID(), spanID)
	}
	if !gotSC.IsSampled() {
		t.Fatalf("expected sampled SpanContext")
	}
	if gotSC.TraceState().String() != wantTS {
		t.Fatalf("TraceState = %q, want %q", gotSC.TraceState().String(), wantTS)
	}
}
