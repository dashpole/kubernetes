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
	"net/http"
	"strings"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	oteltrace "go.opentelemetry.io/otel/trace"
	noopoteltrace "go.opentelemetry.io/otel/trace/noop"

	"k8s.io/client-go/transport"
	v1 "k8s.io/component-base/tracing/api/v1"
)

// TracerProvider is an OpenTelemetry TracerProvider which can be shut down
type TracerProvider interface {
	oteltrace.TracerProvider
	Shutdown(context.Context) error
}

type noopTracerProvider struct {
	oteltrace.TracerProvider
}

func (n *noopTracerProvider) Shutdown(context.Context) error {
	return nil
}

func NewNoopTracerProvider() TracerProvider {
	return &noopTracerProvider{TracerProvider: noopoteltrace.NewTracerProvider()}
}

// NewProvider creates a TracerProvider in a component, and enforces recommended tracing behavior
func NewProvider(ctx context.Context,
	tracingConfig *v1.TracingConfiguration,
	addedOpts []otlptracegrpc.Option,
	resourceOpts []resource.Option,
) (TracerProvider, error) {
	if tracingConfig == nil {
		return NewNoopTracerProvider(), nil
	}
	opts := append([]otlptracegrpc.Option{}, addedOpts...)
	if tracingConfig.Endpoint != nil {
		opts = append(opts, otlptracegrpc.WithEndpoint(*tracingConfig.Endpoint))
	}
	opts = append(opts, otlptracegrpc.WithInsecure())
	exporter, err := otlptracegrpc.New(ctx, opts...)
	if err != nil {
		return nil, err
	}
	res, err := resource.New(ctx, resourceOpts...)
	if err != nil {
		return nil, err
	}

	// sampler respects parent span's sampling rate or
	// otherwise never samples.
	sampler := sdktrace.NeverSample()
	// Or, emit spans for a fraction of transactions
	if tracingConfig.SamplingRatePerMillion != nil && *tracingConfig.SamplingRatePerMillion > 0 {
		sampler = sdktrace.TraceIDRatioBased(float64(*tracingConfig.SamplingRatePerMillion) / float64(1000000))
	}
	// batch span processor to aggregate spans before export.
	bsp := sdktrace.NewBatchSpanProcessor(exporter)
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(newLinkBasedSampler(sdktrace.ParentBased(sampler))),
		sdktrace.WithSpanProcessor(bsp),
		sdktrace.WithResource(res),
	)
	return tp, nil
}

// linkBasedSampler wraps a delegate sampler (such as ParentBased) so that
// internal controller root spans (started via WithObjects) that carry a
// sampled Span Link are also sampled, while excluding SpanKindServer public
// endpoint spans (where otelhttp attaches untrusted incoming HTTP headers as links).
type linkBasedSampler struct {
	delegate sdktrace.Sampler
}

func newLinkBasedSampler(delegate sdktrace.Sampler) sdktrace.Sampler {
	return linkBasedSampler{delegate: delegate}
}

func (s linkBasedSampler) ShouldSample(p sdktrace.SamplingParameters) sdktrace.SamplingResult {
	psc := oteltrace.SpanContextFromContext(p.ParentContext)
	if !psc.IsValid() && p.Kind != oteltrace.SpanKindServer {
		var sampledLinks []oteltrace.SpanContext
		for _, link := range p.Links {
			if link.SpanContext.IsValid() && link.SpanContext.IsSampled() {
				sampledLinks = append(sampledLinks, link.SpanContext)
			}
		}
		if len(sampledLinks) > 0 {
			return sdktrace.SamplingResult{
				Decision:   sdktrace.RecordAndSample,
				Tracestate: combineLinkedTraceStates(sampledLinks),
			}
		}
	}
	return s.delegate.ShouldSample(p)
}

// combineLinkedTraceStates computes the TraceState for a new root span linked
// to one or more sampled SpanContexts:
//   - If all sampled links share a single TraceID, that trace's TraceState is
//     preserved (with the minimum rejection threshold th if multiple links
//     carry W3C/OpenTelemetry Consistent Probability Sampling "ot=th:..." values).
//   - If sampled links originate from multiple distinct TraceIDs, vendor-specific
//     TraceState entries are not copied from an arbitrary link; if any link
//     carries an OpenTelemetry Consistent Probability Sampling threshold
//     ("ot=th:..."), the combined TraceState carries "ot=th:<min_th>" (representing
//     the maximum sampling probability among the linked traces).
func combineLinkedTraceStates(sampledLinks []oteltrace.SpanContext) oteltrace.TraceState {
	firstTraceID := sampledLinks[0].TraceID()
	singleTraceID := true
	minTH := ""
	for _, sc := range sampledLinks {
		if sc.TraceID() != firstTraceID {
			singleTraceID = false
		}
		if th := extractOTelThreshold(sc.TraceState().Get("ot")); th != "" {
			if minTH == "" || compareOTelThreshold(th, minTH) < 0 {
				minTH = th
			}
		}
	}

	if singleTraceID {
		baseTS := sampledLinks[0].TraceState()
		if minTH != "" {
			if updatedOT := replaceOTelThreshold(baseTS.Get("ot"), minTH); updatedOT != "" {
				if ts, err := baseTS.Insert("ot", updatedOT); err == nil {
					return ts
				}
			}
		}
		return baseTS
	}

	if minTH != "" {
		var ts oteltrace.TraceState
		if updated, err := ts.Insert("ot", "th:"+minTH); err == nil {
			return updated
		}
	}
	return oteltrace.TraceState{}
}

func extractOTelThreshold(otVal string) string {
	if otVal == "" {
		return ""
	}
	for _, part := range strings.Split(otVal, ";") {
		if strings.HasPrefix(part, "th:") {
			th := strings.TrimPrefix(part, "th:")
			if isValidOTelThresholdHex(th) {
				return strings.ToLower(th)
			}
		}
	}
	return ""
}

func replaceOTelThreshold(otVal, newTH string) string {
	if otVal == "" {
		return "th:" + newTH
	}
	parts := strings.Split(otVal, ";")
	replaced := false
	for i, part := range parts {
		if strings.HasPrefix(part, "th:") {
			parts[i] = "th:" + newTH
			replaced = true
		}
	}
	if !replaced {
		parts = append([]string{"th:" + newTH}, parts...)
	}
	return strings.Join(parts, ";")
}

func isValidOTelThresholdHex(th string) bool {
	if len(th) == 0 || len(th) > 14 {
		return false
	}
	for i := 0; i < len(th); i++ {
		c := th[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// compareOTelThreshold compares two OpenTelemetry Consistent Probability
// Sampling rejection thresholds ("0" or up to 14 hex digits representing
// fractional bits). Smaller rejection threshold means higher sampling probability.
func compareOTelThreshold(a, b string) int {
	normA := normalizeOTelThreshold(a)
	normB := normalizeOTelThreshold(b)
	return strings.Compare(normA, normB)
}

func normalizeOTelThreshold(th string) string {
	if th == "0" {
		return "00000000000000"
	}
	if len(th) >= 14 {
		return th[:14]
	}
	return th + strings.Repeat("0", 14-len(th))
}

func (s linkBasedSampler) Description() string {
	return "LinkBased(" + s.delegate.Description() + ")"
}

// WithTracing adds tracing to requests if the incoming request is sampled
func WithTracing(handler http.Handler, tp oteltrace.TracerProvider, spanName string) http.Handler {
	opts := []otelhttp.Option{
		otelhttp.WithPropagators(Propagators()),
		otelhttp.WithTracerProvider(tp),
	}
	wrappedHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Add the http.target attribute to the otelhttp span
		// Workaround for https://github.com/open-telemetry/opentelemetry-go-contrib/issues/3743
		if r.URL != nil {
			oteltrace.SpanFromContext(r.Context()).SetAttributes(attribute.Key("http.target").String(r.URL.RequestURI()))
		}
		handler.ServeHTTP(w, r)
	})
	// With Noop TracerProvider, the otelhttp still handles context propagation.
	// See https://github.com/open-telemetry/opentelemetry-go/tree/main/example/passthrough
	return otelhttp.NewHandler(wrappedHandler, spanName, opts...)
}

// WrapperFor can be used to add tracing to a *rest.Config.
// Example usage:
// tp := NewProvider(...)
// config, _ := rest.InClusterConfig()
// config.Wrap(WrapperFor(tp))
// kubeclient, _ := clientset.NewForConfig(config)
func WrapperFor(tp oteltrace.TracerProvider) transport.WrapperFunc {
	return func(rt http.RoundTripper) http.RoundTripper {
		opts := []otelhttp.Option{
			otelhttp.WithPropagators(Propagators()),
			otelhttp.WithTracerProvider(tp),
		}
		// With Noop TracerProvider, the otelhttp still handles context propagation.
		// See https://github.com/open-telemetry/opentelemetry-go/tree/main/example/passthrough
		return otelhttp.NewTransport(rt, opts...)
	}
}

// Propagators returns the recommended set of propagators.
func Propagators() propagation.TextMapPropagator {
	return propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{})
}
