// Package traceutil provides various definitions and utilities that allow for
// common operations with our trace tooling, such as span creation, encoding, decoding,
// and enumeration of possible services.
package traceutil

import (
	"context"
	"encoding/base64"
	"log"

	"contrib.go.opencensus.io/exporter/ocagent"

	"go.opencensus.io/trace"
	"go.opencensus.io/trace/propagation"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog"
)

// TraceAnnotationKey is the annotation name where span context should be found
const TraceAnnotationKey string = "trace.kubernetes.io/context"

// InitializeExporter takes a ServiceType and sets the global OpenCensus exporter
// to export to that service on a specified Zipkin instance
func InitializeExporter(service string) {
	klog.Infof("OpenCensus trace exporter initializing with service %s", string(service))

	// create ocagent exporter
	exp, err := ocagent.NewExporter(ocagent.WithInsecure(), ocagent.WithServiceName(string(service)))
	if err != nil {
		log.Fatalf("Failed to create the agent exporter: %v", err)
	}
	// Only sample when the propagated parent SpanContext is sampled
	// Use ProbabilitySampler because it propagates the parent sampling decision.
    trace.ApplyConfig(trace.Config{DefaultSampler: trace.ProbabilitySampler(0)})

	trace.RegisterExporter(exp)

	return
}

// StartSpanFromObject takes an object to extract trace context from and the desired Span name and
// constructs a new Span from this information.  It mirrors trace.StartSpan, but for kubernetes objects.
func StartSpanFromObject(ctx context.Context, tracedResource meta.Object, name string) (context.Context, *trace.Span) {
	spanFromEncodedContext, ok := spanContextFromObject(tracedResource)
	if !ok {
		return ctx, &trace.Span{}
	}
	return trace.StartSpanWithRemoteParent(ctx, name, spanFromEncodedContext)
}

// spanContextFromObject takes an object to extract an encoded SpanContext from and returns the decoded SpanContext
func spanContextFromObject(tracedResource meta.Object) (trace.SpanContext, bool) {
	tracedResourceAnnotations := tracedResource.GetAnnotations()
	embeddedSpanContext, ok := tracedResourceAnnotations[TraceAnnotationKey]
	if !ok {
		return trace.SpanContext{}, false
	}

	decodedContextBytes, err := base64.StdEncoding.DecodeString(embeddedSpanContext)
	if err != nil {
		return trace.SpanContext{}, false
	}

	return propagation.FromBinary(decodedContextBytes)

}

// EncodeContextIntoObject encodes the SpanContext contained in the context into the provided object
func EncodeContextIntoObject(ctx context.Context, tracedResource meta.Object) {
	span := trace.FromContext(ctx)
	if span != nil {
		encodeSpanContextIntoObject(span.SpanContext(), tracedResource)
	}
}

func RemoveSpanContextFromObject(tracedResource meta.Object) {
	tracedResourceAnnotations := tracedResource.GetAnnotations()
	delete(tracedResourceAnnotations, TraceAnnotationKey)
	tracedResource.SetAnnotations(tracedResourceAnnotations)
}

// encodeSpanContextIntoObject takes a pointer to an object and a Span Context to embed
// Base64 encodes the wire format for the SpanContext, and puts it in the object's TraceContext field
func encodeSpanContextIntoObject(ctx trace.SpanContext, tracedResource meta.Object) {
	tracedResourceAnnotations := tracedResource.GetAnnotations()

	rawContextBytes := propagation.Binary(ctx)
	encodedContext := base64.StdEncoding.EncodeToString(rawContextBytes)

	tracedResourceAnnotations[TraceAnnotationKey] = encodedContext
	tracedResource.SetAnnotations(tracedResourceAnnotations)

	return
}
