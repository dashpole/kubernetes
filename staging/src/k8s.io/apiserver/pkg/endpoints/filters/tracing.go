package filters

import (
	"net/http"

	"go.opencensus.io/plugin/ochttp"
	"go.opencensus.io/trace"
)

// WithTracing adds tracing to requests if the incoming request is sampled
func WithTracing(handler http.Handler) http.Handler {
	return &ochttp.Handler{
		Handler: handler,
		StartOptions: trace.StartOptions{Sampler: trace.ProbabilitySampler(0)},
	}
}