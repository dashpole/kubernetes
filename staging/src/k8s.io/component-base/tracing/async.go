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
	"encoding/hex"
	"os"
	"strings"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"
	nooptrace "go.opentelemetry.io/otel/trace/noop"
)

var (
	globalAsyncProviders = make(map[string]trace.TracerProvider)
	globalAsyncMu        sync.Mutex
)

// GetOrCreateAsyncTracer returns a tracer configured for the given component
// (e.g. "kube-controller-manager", "kube-scheduler", "kubelet")
// pointing to the local OTLP collector at 127.0.0.1:4317 (or OTEL_EXPORTER_OTLP_ENDPOINT).
func GetOrCreateAsyncTracer(componentName string) trace.Tracer {
	globalAsyncMu.Lock()
	defer globalAsyncMu.Unlock()

	if tp, ok := globalAsyncProviders[componentName]; ok {
		return tp.Tracer(componentName)
	}

	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		endpoint = "127.0.0.1:4317"
	} else {
		endpoint = strings.TrimPrefix(endpoint, "http://")
		endpoint = strings.TrimPrefix(endpoint, "https://")
	}

	ctx := context.Background()
	exporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(endpoint),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		return nooptrace.NewTracerProvider().Tracer(componentName)
	}

	res, _ := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(componentName),
		),
	)

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithResource(res),
	)

	globalAsyncProviders[componentName] = tp
	return tp.Tracer(componentName)
}

// ParseW3CTraceparent extracts the TraceID and SpanID from a W3C traceparent string:
// format: 00-<32 hex trace-id>-<16 hex parent-id>-<2 hex flags>
func ParseW3CTraceparent(tp string) (trace.SpanContext, bool) {
	tp = strings.TrimSpace(tp)
	parts := strings.Split(tp, "-")
	if len(parts) < 4 || parts[0] != "00" {
		return trace.SpanContext{}, false
	}
	traceIDBytes, err := hex.DecodeString(parts[1])
	if err != nil || len(traceIDBytes) != 16 {
		return trace.SpanContext{}, false
	}
	spanIDBytes, err := hex.DecodeString(parts[2])
	if err != nil || len(spanIDBytes) != 8 {
		return trace.SpanContext{}, false
	}

	var traceID trace.TraceID
	copy(traceID[:], traceIDBytes)

	var spanID trace.SpanID
	copy(spanID[:], spanIDBytes)

	flags := trace.FlagsSampled
	return trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: flags,
		Remote:     true,
	}), true
}

// StartAsyncSpan creates a span for an asynchronous reconciler or controller operation.
// If traceparentStr is valid, the new span inherits the TraceID (for unified Cloud Trace waterfall rendering)
// and attaches a W3C Span Link pointing to the causal span context.
func StartAsyncSpan(ctx context.Context, tracer trace.Tracer, spanName string, traceparentStr string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	opts := []trace.SpanStartOption{
		trace.WithAttributes(attrs...),
	}

	if sc, ok := ParseW3CTraceparent(traceparentStr); ok && sc.IsValid() {
		opts = append(opts, trace.WithLinks(trace.Link{SpanContext: sc}))
		ctx = trace.ContextWithRemoteSpanContext(ctx, sc)
	}

	return tracer.Start(ctx, spanName, opts...)
}
