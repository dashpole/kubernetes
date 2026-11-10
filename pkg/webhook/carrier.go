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
	"strings"

	"go.opentelemetry.io/otel/propagation"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// AnnotationPrefix is the Kubernetes annotation key prefix used to store
	// OpenTelemetry text map propagator fields on Kubernetes objects.
	AnnotationPrefix = "tracing.k8s.io/"
)

// Ensure ObjectCarrier implements propagation.TextMapCarrier at compile time.
var _ propagation.TextMapCarrier = ObjectCarrier{}

// ObjectCarrier adapts a Kubernetes metav1.Object to OpenTelemetry's
// propagation.TextMapCarrier interface using the "tracing.k8s.io/" prefix.
type ObjectCarrier struct {
	obj metav1.Object
}

// NewObjectCarrier returns an ObjectCarrier wrapping the given metav1.Object.
func NewObjectCarrier(obj metav1.Object) ObjectCarrier {
	return ObjectCarrier{obj: obj}
}

func mapCarrierKeyToAnnotation(key string) string {
	lower := strings.ToLower(key)
	if strings.HasPrefix(lower, AnnotationPrefix) {
		return lower
	}
	return AnnotationPrefix + lower
}

// Get retrieves a single value for a given propagator key.
func (c ObjectCarrier) Get(key string) string {
	if c.obj == nil {
		return ""
	}
	ann := c.obj.GetAnnotations()
	if ann == nil {
		return ""
	}
	return ann[mapCarrierKeyToAnnotation(key)]
}

// Set stores a key-value pair on the underlying Kubernetes object's annotations.
func (c ObjectCarrier) Set(key, val string) {
	if c.obj == nil || val == "" {
		return
	}
	ann := c.obj.GetAnnotations()
	if ann == nil {
		ann = make(map[string]string)
	}
	ann[mapCarrierKeyToAnnotation(key)] = val
	c.obj.SetAnnotations(ann)
}

// Keys lists the propagator keys stored in the carrier.
func (c ObjectCarrier) Keys() []string {
	if c.obj == nil {
		return nil
	}
	ann := c.obj.GetAnnotations()
	if len(ann) == 0 {
		return nil
	}
	var keys []string
	for k := range ann {
		if strings.HasPrefix(k, AnnotationPrefix) {
			keys = append(keys, strings.TrimPrefix(k, AnnotationPrefix))
		}
	}
	return keys
}
