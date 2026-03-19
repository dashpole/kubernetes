package tracing

import (
	"context"
	"io"

	"go.opentelemetry.io/otel/propagation"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apiserver/pkg/admission"
	"k8s.io/component-base/tracing"
)

// PluginName indicates name of admission plugin.
const PluginName = "TracingContext"

// Register registers a plugin.
func Register(plugins *admission.Plugins) {
	plugins.Register(PluginName, func(config io.Reader) (admission.Interface, error) {
		return NewPlugin(), nil
	})
}

// Plugin is an implementation of admission.MutationInterface.
type Plugin struct {
	*admission.Handler
	propagator propagation.TextMapPropagator
}

var _ admission.MutationInterface = &Plugin{}

// NewPlugin creates a new admission plugin.
func NewPlugin() *Plugin {
	return &Plugin{
		Handler:    admission.NewHandler(admission.Create, admission.Update),
		propagator: propagation.TraceContext{},
	}
}

// Admit makes an admission decision based on the request attributes and injects trace context.
func (p *Plugin) Admit(ctx context.Context, a admission.Attributes, o admission.ObjectInterfaces) error {
	// Ignore all requests to subresources or resources that don't support objects.
	if len(a.GetSubresource()) != 0 || a.GetObject() == nil {
		return nil
	}

	metaObj, err := meta.Accessor(a.GetObject())
	if err != nil {
		// Not a standard object with metadata, ignore.
		return nil
	}

	// Inject trace context from ctx into the object's annotations.
	// We use the W3C propagator to convert the context to traceparent/baggage strings.
	tracing.InjectContext(ctx, metaObj, p.propagator)

	return nil
}
