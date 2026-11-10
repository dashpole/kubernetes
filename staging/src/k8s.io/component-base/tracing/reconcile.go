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
	"sort"
	"strings"
	"sync"

	"go.opentelemetry.io/otel/propagation"
	oteltrace "go.opentelemetry.io/otel/trace"
	noopoteltrace "go.opentelemetry.io/otel/trace/noop"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// AnnotationPrefix is the canonical Kubernetes annotation key prefix used
	// to persist OpenTelemetry text map propagator keys on Kubernetes objects.
	AnnotationPrefix = "tracing.k8s.io/"
)

// ObjectCarrier adapts metav1.Object annotations to OpenTelemetry's
// propagation.TextMapCarrier interface using the "tracing.k8s.io/" key prefix.
type ObjectCarrier struct {
	obj metav1.Object
}

var _ propagation.TextMapCarrier = ObjectCarrier{}

// NewObjectCarrier returns an ObjectCarrier backed by obj's annotations.
func NewObjectCarrier(obj metav1.Object) ObjectCarrier {
	return ObjectCarrier{obj: obj}
}

func isNilObject(obj metav1.Object) bool {
	if obj == nil {
		return true
	}
	v := reflect.ValueOf(obj)
	return v.Kind() == reflect.Pointer && v.IsNil()
}

func mapCarrierKeyToAnnotation(key string) string {
	lower := strings.ToLower(key)
	if strings.HasPrefix(lower, AnnotationPrefix) {
		return lower
	}
	return AnnotationPrefix + lower
}

func mapAnnotationToCarrierKey(key string) string {
	return strings.TrimPrefix(key, AnnotationPrefix)
}

// Get returns the value associated with the passed key from the object's annotations.
func (c ObjectCarrier) Get(key string) string {
	if isNilObject(c.obj) {
		return ""
	}
	ann := c.obj.GetAnnotations()
	if len(ann) == 0 {
		return ""
	}
	return ann[mapCarrierKeyToAnnotation(key)]
}

// Set stores the key-value pair into the object's annotations.
func (c ObjectCarrier) Set(key string, val string) {
	if isNilObject(c.obj) || val == "" {
		return
	}
	ann := c.obj.GetAnnotations()
	if ann == nil {
		ann = make(map[string]string, 1)
	}
	ann[mapCarrierKeyToAnnotation(key)] = val
	c.obj.SetAnnotations(ann)
}

// Keys lists the keys stored in this carrier.
func (c ObjectCarrier) Keys() []string {
	if isNilObject(c.obj) {
		return nil
	}
	ann := c.obj.GetAnnotations()
	if len(ann) == 0 {
		return nil
	}
	keys := make([]string, 0, len(ann))
	for k := range ann {
		if strings.HasPrefix(k, AnnotationPrefix) {
			keys = append(keys, mapAnnotationToCarrierKey(k))
		}
	}
	sort.Strings(keys)
	return keys
}

// WithObjects extracts SpanContexts from the annotations of the provided
// Kubernetes objects and returns an oteltrace.SpanStartOption that attaches
// them as OpenTelemetry Span Links on the started span.
func WithObjects(objs ...metav1.Object) oteltrace.SpanStartOption {
	var links []oteltrace.Link
	prop := Propagators()
	for _, obj := range objs {
		if sc := extractSpanContext(prop, obj); sc.IsValid() {
			links = appendUniqueLink(links, sc)
		}
	}
	return oteltrace.WithLinks(links...)
}

func extractSpanContext(prop propagation.TextMapPropagator, obj metav1.Object) oteltrace.SpanContext {
	if isNilObject(obj) {
		return oteltrace.SpanContext{}
	}
	extractedCtx := prop.Extract(context.Background(), NewObjectCarrier(obj))
	return oteltrace.SpanContextFromContext(extractedCtx)
}

func appendUniqueLink(links []oteltrace.Link, sc oteltrace.SpanContext) []oteltrace.Link {
	for _, existing := range links {
		if existing.SpanContext.TraceID() == sc.TraceID() && existing.SpanContext.SpanID() == sc.SpanID() {
			return links
		}
	}
	return append(links, oteltrace.Link{SpanContext: sc})
}

const (
	defaultMaxLinksPerObject = 8
	defaultMaxTrackedObjects = 10000
)

type trackedObjectLinks struct {
	pending  []oteltrace.Link
	lastSeen oteltrace.SpanContext
}

// LinkTracker accumulates SpanContexts observed by Informer event handlers for
// Kubernetes objects so that multiple updates coalesced by a controller's
// workqueue before a single reconciliation are all linked to the resulting
// reconciliation span.
type LinkTracker struct {
	mu                sync.Mutex
	entries           map[string]*trackedObjectLinks
	maxLinksPerObject int
	maxTrackedObjects int
}

// NewLinkTracker returns a LinkTracker with bounded per-object and total capacity.
func NewLinkTracker() *LinkTracker {
	return &LinkTracker{
		entries:           make(map[string]*trackedObjectLinks),
		maxLinksPerObject: defaultMaxLinksPerObject,
		maxTrackedObjects: defaultMaxTrackedObjects,
	}
}

func objectTrackingKey(obj metav1.Object) string {
	if uid := string(obj.GetUID()); uid != "" {
		return "uid:" + uid
	}
	return "name:" + obj.GetNamespace() + "/" + obj.GetName()
}

// Record extracts the SpanContext from obj's annotations and appends it to the
// pending Span Links for obj if it differs from the most recently recorded
// SpanContext for that object.
func (t *LinkTracker) Record(obj metav1.Object) {
	if t == nil || isNilObject(obj) {
		return
	}
	sc := extractSpanContext(Propagators(), obj)
	if !sc.IsValid() {
		return
	}
	key := objectTrackingKey(obj)

	t.mu.Lock()
	defer t.mu.Unlock()

	entry, ok := t.entries[key]
	if !ok {
		if len(t.entries) >= t.maxTrackedObjects {
			for k := range t.entries {
				delete(t.entries, k)
				break
			}
		}
		entry = &trackedObjectLinks{}
		t.entries[key] = entry
	}
	if entry.lastSeen.IsValid() &&
		entry.lastSeen.TraceID() == sc.TraceID() &&
		entry.lastSeen.SpanID() == sc.SpanID() {
		return
	}
	entry.lastSeen = sc
	entry.pending = appendUniqueLink(entry.pending, sc)
	if len(entry.pending) > t.maxLinksPerObject {
		entry.pending = entry.pending[len(entry.pending)-t.maxLinksPerObject:]
	}
}

// Forget removes any tracked Span Links for obj (e.g., when obj is deleted).
func (t *LinkTracker) Forget(obj metav1.Object) {
	if t == nil || isNilObject(obj) {
		return
	}
	key := objectTrackingKey(obj)
	t.mu.Lock()
	delete(t.entries, key)
	t.mu.Unlock()
}

// WithObjects drains all pending Span Links accumulated for objs since their
// last reconciliation, merges them with the current SpanContext on each object,
// and returns an oteltrace.SpanStartOption that attaches them as Span Links.
func (t *LinkTracker) WithObjects(objs ...metav1.Object) oteltrace.SpanStartOption {
	if t == nil {
		return WithObjects(objs...)
	}
	var links []oteltrace.Link
	prop := Propagators()

	t.mu.Lock()
	defer t.mu.Unlock()

	for _, obj := range objs {
		if isNilObject(obj) {
			continue
		}
		key := objectTrackingKey(obj)
		if entry, ok := t.entries[key]; ok {
			for _, l := range entry.pending {
				links = appendUniqueLink(links, l.SpanContext)
			}
			entry.pending = nil
		}
		if sc := extractSpanContext(prop, obj); sc.IsValid() {
			if entry, ok := t.entries[key]; ok {
				entry.lastSeen = sc
			}
			links = appendUniqueLink(links, sc)
		}
	}
	return oteltrace.WithLinks(links...)
}

type tracerProviderContextKey struct{}

func isNoopTracerProvider(tp oteltrace.TracerProvider) bool {
	if tp == nil {
		return true
	}
	if _, ok := tp.(*noopTracerProvider); ok {
		return true
	}
	if _, ok := tp.(noopoteltrace.TracerProvider); ok {
		return true
	}
	return false
}

// ContextWithTracerProvider returns a new context with the given TracerProvider attached.
func ContextWithTracerProvider(ctx context.Context, tp oteltrace.TracerProvider) context.Context {
	if isNoopTracerProvider(tp) {
		return ctx
	}
	return context.WithValue(ctx, tracerProviderContextKey{}, tp)
}

// TracerProviderFromContext returns the TracerProvider associated with ctx, or
// a no-op TracerProvider if none is configured.
func TracerProviderFromContext(ctx context.Context) oteltrace.TracerProvider {
	if ctx != nil {
		if tp, ok := ctx.Value(tracerProviderContextKey{}).(oteltrace.TracerProvider); ok && tp != nil {
			return tp
		}
	}
	return NewNoopTracerProvider()
}

// TracerFromContext returns an OpenTelemetry Tracer from the TracerProvider
// associated with ctx, or a no-op Tracer if none is configured.
func TracerFromContext(ctx context.Context) oteltrace.Tracer {
	return TracerProviderFromContext(ctx).Tracer(instrumentationScope)
}
