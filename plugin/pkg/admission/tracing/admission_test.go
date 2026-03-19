package tracing

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apiserver/pkg/admission"
	"k8s.io/component-base/tracing"
)

func TestTracingContextAdmission(t *testing.T) {
	otel.SetTextMapPropagator(propagation.TraceContext{})
	plugin := NewPlugin()

	tests := []struct {
		name        string
		operation   admission.Operation
		subresource string
		expectInject bool
	}{
		{
			name:         "CREATE operation should inject",
			operation:    admission.Create,
			subresource:  "",
			expectInject: true,
		},
		{
			name:         "UPDATE operation should inject",
			operation:    admission.Update,
			subresource:  "",
			expectInject: true,
		},
		{
			name:         "UPDATE status should not inject",
			operation:    admission.Update,
			subresource:  "status",
			expectInject: false,
		},
		{
			name:         "DELETE operation does not run admission here usually, but if it did it's handled by generic plugin",
			operation:    admission.Delete,
			subresource:  "",
			expectInject: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Create a mock span and context
			traceID, _ := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4737")
			spanID, _ := trace.SpanIDFromHex("00f067aa0ba902b7")
			
			spanContext := trace.NewSpanContext(trace.SpanContextConfig{
				TraceID:    traceID,
				SpanID:     spanID,
				TraceFlags: trace.FlagsSampled,
			})
			
			ctx := trace.ContextWithSpanContext(context.Background(), spanContext)

			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
				},
			}

			attr := admission.NewAttributesRecord(
				pod,
				nil,
				corev1.SchemeGroupVersion.WithKind("Pod"),
				pod.Namespace,
				pod.Name,
				corev1.SchemeGroupVersion.WithResource("pods"),
				tc.subresource,
				tc.operation,
				nil,
				false,
				nil,
			)

			// We need to check if the plugin actually handles the operation
			if !plugin.Handles(tc.operation) {
				if tc.expectInject {
					t.Errorf("plugin should handle operation %v", tc.operation)
				}
				return
			}

			err := plugin.Admit(ctx, attr, nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Check results
			annotatedTrace := pod.GetAnnotations()[tracing.TraceparentAnnotation]

			if tc.expectInject {
				if annotatedTrace == "" {
					t.Errorf("expected traceparent annotation but got none")
				}
				expectedTraceparent := "00-4bf92f3577b34da6a3ce929d0e0e4737-00f067aa0ba902b7-01"
				if annotatedTrace != expectedTraceparent {
					t.Errorf("expected traceparent %q, got %q", expectedTraceparent, annotatedTrace)
				}
			} else {
				if annotatedTrace != "" {
					t.Errorf("expected no traceparent annotation, got %q", annotatedTrace)
				}
			}
		})
	}
}
