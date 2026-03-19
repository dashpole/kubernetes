package tracing

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// TracingAnnotationPrefix is the prefix for tracing-related annotations.
	TracingAnnotationPrefix = "tracing.k8s.io/"

	// TraceparentAnnotation is the annotation key for W3C traceparent.
	TraceparentAnnotation = TracingAnnotationPrefix + "traceparent"

	// BaggageAnnotation is the annotation key for W3C baggage.
	BaggageAnnotation = TracingAnnotationPrefix + "baggage"
)

// AnnotationCarrier implements propagation.TextMapCarrier for Kubernetes object annotations.
// This allows OpenTelemetry propagators to read/write trace context directly to/from
// Kubernetes object annotations using a standard interface.
//
// The carrier uses a fixed prefix (TracingAnnotationPrefix = "tracing.k8s.io/") and
// appends the propagator's keys (e.g., "traceparent", "baggage") to form the full
// annotation keys (e.g., "tracing.k8s.io/traceparent"). This makes the specific
// headers/keys pluggable via the propagator while maintaining a consistent namespace.
type AnnotationCarrier struct {
	object metav1.Object
}

// NewAnnotationCarrier creates a new AnnotationCarrier for the given object.
func NewAnnotationCarrier(obj metav1.Object) *AnnotationCarrier {
	return &AnnotationCarrier{object: obj}
}

// Get returns the value for the given key from annotations.
// The key from the propagator (e.g., "traceparent") is prefixed with TracingAnnotationPrefix.
func (c *AnnotationCarrier) Get(key string) string {
	annotations := c.object.GetAnnotations()
	if annotations == nil {
		return ""
	}
	return annotations[TracingAnnotationPrefix+key]
}

// Set stores the key-value pair in annotations.
// The key from the propagator (e.g., "traceparent") is prefixed with TracingAnnotationPrefix.
func (c *AnnotationCarrier) Set(key, value string) {
	annotations := c.object.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations[TracingAnnotationPrefix+key] = value
	c.object.SetAnnotations(annotations)
}

// Keys returns all keys in the carrier (without the prefix).
// Returns the propagator keys (e.g., "traceparent", "baggage") for annotations
// matching TracingAnnotationPrefix.
func (c *AnnotationCarrier) Keys() []string {
	annotations := c.object.GetAnnotations()
	var keys []string
	for k := range annotations {
		if len(k) > len(TracingAnnotationPrefix) && k[:len(TracingAnnotationPrefix)] == TracingAnnotationPrefix {
			keys = append(keys, k[len(TracingAnnotationPrefix):])
		}
	}
	return keys
}

// InjectContext injects the trace context from ctx into the object's annotations.
func InjectContext(ctx context.Context, obj metav1.Object) {
	carrier := NewAnnotationCarrier(obj)
	otel.GetTextMapPropagator().Inject(ctx, carrier)
}

// ExtractContext extracts trace context from the object's annotations.
func ExtractContext(ctx context.Context, obj metav1.Object) context.Context {
	carrier := NewAnnotationCarrier(obj)
	return otel.GetTextMapPropagator().Extract(ctx, carrier)
}

// StartReconcileSpan starts a new root span for reconciliation, linked to any
// trace context stored in the object's annotations.
func StartReconcileSpan(
	ctx context.Context,
	name string,
	obj metav1.Object,
	tracer trace.Tracer,
	options ...trace.SpanStartOption,
) (context.Context, trace.Span) {
	opts := []trace.SpanStartOption{
		trace.WithSpanKind(trace.SpanKindConsumer),
	}
	opts = append(opts, options...)

	// Extract stored context and create a link if present
	extractedCtx := ExtractContext(context.Background(), obj)
	remoteSpanCtx := trace.SpanContextFromContext(extractedCtx)

	if remoteSpanCtx.IsValid() {
		// Create a new root span with a link to the original context
		opts = append(opts,
			trace.WithLinks(trace.Link{SpanContext: remoteSpanCtx}),
			trace.WithNewRoot(),
		)
	}

	return tracer.Start(ctx, name, opts...)
}
