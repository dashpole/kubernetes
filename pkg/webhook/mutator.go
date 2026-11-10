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
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"reflect"
	"strings"

	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/klog/v2"
)

// PatchOperation represents a single RFC 6902 JSONPatch operation.
type PatchOperation struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value any    `json:"value,omitempty"`
}

// Config configures the behavior of the Mutator.
type Config struct {
	// GenerateIfMissing controls whether the webhook generates a fresh W3C
	// traceparent when the incoming admission HTTP request does not carry one.
	// Defaults to false (strict propagation of incoming API server traceparent).
	GenerateIfMissing bool

	// SamplingRate is the probability [0.0, 1.0] of generating a sampled
	// traceparent when GenerateIfMissing is true.
	SamplingRate float64

	// RequireSampled, when true, skips injecting incoming traceparent headers
	// whose W3C sampled flag (0x01) is not set.
	RequireSampled bool
}

// Mutator evaluates AdmissionRequests and emits RFC 6902 JSONPatches that
// inject OpenTelemetry TextMapPropagator fields under tracing.k8s.io/* strictly
// on resource creation or desired-state mutation.
type Mutator struct {
	cfg        Config
	propagator propagation.TextMapPropagator
}

// NewMutator creates a new Mutator with the provided configuration.
func NewMutator(cfg Config) *Mutator {
	return &Mutator{
		cfg:        cfg,
		propagator: propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}),
	}
}

// Mutate processes an AdmissionRequest and incoming HTTP headers, enforcing the
// Spec-Only Invariant:
//  1. Subresource requests (e.g. /status, /scale, /finalize) are ignored completely.
//  2. Operations other than CREATE and UPDATE are ignored.
//  3. On UPDATE, .spec is compared for semantic equality against oldObject.spec;
//     if .spec did not change, the request is bypassed with zero patches.
//  4. Valid W3C traceparent (and tracestate) headers are injected into
//     metadata.annotations["tracing.k8s.io/traceparent"] and workload pod templates.
//     If an UPDATE mutates .spec without an active trace context, any stale trace
//     annotations from prior generations are removed.
func (m *Mutator) Mutate(ctx context.Context, req *admissionv1.AdmissionRequest, headers http.Header) *admissionv1.AdmissionResponse {
	if req == nil {
		return &admissionv1.AdmissionResponse{Allowed: true}
	}

	resp := &admissionv1.AdmissionResponse{
		UID:     req.UID,
		Allowed: true,
	}

	// 1. Subresource Filter: Ignore /status and all other subresource operations.
	if req.SubResource != "" {
		klog.V(4).InfoS("Ignoring subresource admission request",
			"subResource", req.SubResource,
			"namespace", req.Namespace,
			"name", req.Name,
			"kind", req.Kind.Kind,
		)
		return resp
	}

	// 2. Operation Filter: Only intercept CREATE and UPDATE.
	if req.Operation != admissionv1.Create && req.Operation != admissionv1.Update {
		return resp
	}

	if len(req.Object.Raw) == 0 {
		return resp
	}

	var newObj map[string]any
	if err := json.Unmarshal(req.Object.Raw, &newObj); err != nil {
		klog.ErrorS(err, "Failed to unmarshal admission request object; failing open",
			"namespace", req.Namespace,
			"name", req.Name,
			"kind", req.Kind.Kind,
		)
		return resp
	}

	// 3. Spec-Only Invariant on UPDATE: verify that .spec actually changed.
	var oldObj map[string]any
	if req.Operation == admissionv1.Update {
		if len(req.OldObject.Raw) == 0 {
			klog.V(4).InfoS("Skipping UPDATE without oldObject",
				"namespace", req.Namespace,
				"name", req.Name,
				"kind", req.Kind.Kind,
			)
			return resp
		}
		if err := json.Unmarshal(req.OldObject.Raw, &oldObj); err != nil {
			klog.ErrorS(err, "Failed to unmarshal admission request oldObject; failing open",
				"namespace", req.Namespace,
				"name", req.Name,
				"kind", req.Kind.Kind,
			)
			return resp
		}

		if !hasDesiredStateChange(newObj, oldObj) {
			klog.V(4).InfoS("Bypassing UPDATE because desired state (excluding metadata and status) is unchanged",
				"namespace", req.Namespace,
				"name", req.Name,
				"kind", req.Kind.Kind,
			)
			return resp
		}
	}

	// 4. Extract or resolve propagated fields via the configured TextMapPropagator.
	fields := m.resolvePropagatedFields(ctx, req.Operation, newObj, oldObj, headers)
	if len(fields) == 0 && req.Operation == admissionv1.Create {
		return resp
	}

	// 5. Build atomic RFC 6902 JSONPatch operations for top-level metadata.annotations.
	// On UPDATE, if fields is empty, buildObjectPatches removes any stale
	// tracing.k8s.io/* propagator annotations from prior generations so new
	// reconciliations never link to stale traces.
	patches := buildObjectPatches(newObj, fields, m.propagator.Fields())
	if len(patches) == 0 {
		return resp
	}

	patchBytes, err := json.Marshal(patches)
	if err != nil {
		klog.ErrorS(err, "Failed to marshal JSONPatch operations; failing open",
			"namespace", req.Namespace,
			"name", req.Name,
			"kind", req.Kind.Kind,
		)
		return resp
	}

	patchType := admissionv1.PatchTypeJSONPatch
	resp.PatchType = &patchType
	resp.Patch = patchBytes

	klog.V(3).InfoS("Injected trace context annotation patch",
		"operation", req.Operation,
		"namespace", req.Namespace,
		"name", req.Name,
		"kind", req.Kind.Kind,
		"patchCount", len(patches),
	)
	return resp
}

func (m *Mutator) resolvePropagatedFields(ctx context.Context, op admissionv1.Operation, newObj, oldObj map[string]any, headers http.Header) propagation.MapCarrier {
	if headers != nil {
		extractedCtx := m.propagator.Extract(ctx, propagation.HeaderCarrier(headers))
		sc := trace.SpanContextFromContext(extractedCtx)
		if sc.IsValid() {
			if m.cfg.RequireSampled && !sc.IsSampled() {
				return nil
			}
			carrier := propagation.MapCarrier{}
			m.propagator.Inject(extractedCtx, carrier)
			return carrier
		}
	}

	// If no valid HTTP trace context header was provided, check whether the caller
	// explicitly set valid tracing.k8s.io/* propagator annotations on newObj
	// (on CREATE, or on UPDATE when newObj's annotations differ from oldObj's).
	if hasUpdatedPropagatedAnnotations(newObj, oldObj, op, m.propagator.Fields()) {
		if existing, valid := extractExistingObjectFields(ctx, m.propagator, m.cfg.RequireSampled, newObj); valid {
			return existing
		}
		if m.cfg.RequireSampled {
			return nil
		}
	}

	if m.cfg.GenerateIfMissing && isSampled(m.cfg.SamplingRate) {
		if tp, err := generateTraceparent(true); err == nil {
			extractedCtx := m.propagator.Extract(ctx, propagation.MapCarrier{"traceparent": tp})
			carrier := propagation.MapCarrier{}
			m.propagator.Inject(extractedCtx, carrier)
			return carrier
		}
	}

	return nil
}

func hasUpdatedPropagatedAnnotations(newObj, oldObj map[string]any, op admissionv1.Operation, fields []string) bool {
	for _, field := range fields {
		annKey := mapCarrierKeyToAnnotation(field)
		newVal := getObjectAnnotation(newObj, annKey)
		oldVal := getObjectAnnotation(oldObj, annKey)
		if newVal != "" && (op == admissionv1.Create || newVal != oldVal) {
			return true
		}
	}
	return false
}

func getObjectAnnotation(obj map[string]any, key string) string {
	if obj == nil {
		return ""
	}
	metaMap, ok := obj["metadata"].(map[string]any)
	if !ok {
		return ""
	}
	annMap, ok := metaMap["annotations"].(map[string]any)
	if !ok {
		return ""
	}
	val, _ := annMap[key].(string)
	return val
}

func extractExistingObjectFields(ctx context.Context, prop propagation.TextMapPropagator, requireSampled bool, obj map[string]any) (propagation.MapCarrier, bool) {
	input := propagation.MapCarrier{}
	for _, field := range prop.Fields() {
		if val := getObjectAnnotation(obj, mapCarrierKeyToAnnotation(field)); val != "" {
			input[field] = val
		}
	}
	if len(input) == 0 {
		return nil, false
	}

	extractedCtx := prop.Extract(ctx, input)
	sc := trace.SpanContextFromContext(extractedCtx)
	if !sc.IsValid() {
		return nil, false
	}
	if requireSampled && !sc.IsSampled() {
		return nil, false
	}

	normalized := propagation.MapCarrier{}
	prop.Inject(extractedCtx, normalized)
	return normalized, true
}

func hasDesiredStateChange(newObj, oldObj map[string]any) bool {
	for k, newVal := range newObj {
		if k == "metadata" || k == "status" {
			continue
		}
		if !reflect.DeepEqual(newVal, oldObj[k]) {
			return true
		}
	}
	for k := range oldObj {
		if k == "metadata" || k == "status" {
			continue
		}
		if _, exists := newObj[k]; !exists {
			return true
		}
	}
	return false
}

func buildObjectPatches(rootObj map[string]any, fields propagation.MapCarrier, propagatorFields []string) []PatchOperation {
	// Only patch top-level /metadata/annotations. Do not mutate nested
	// .spec.template.metadata.annotations (which would alter pod template
	// hashes and trigger unnecessary Pod rollouts on Deployment scaling).
	return buildAnnotationPatches(rootObj, nil, fields, propagatorFields)
}

func buildAnnotationPatches(rootObj map[string]any, basePath []string, fields propagation.MapCarrier, propagatorFields []string) []PatchOperation {
	targetObj := rootObj
	for _, seg := range basePath {
		next, ok := targetObj[seg].(map[string]any)
		if !ok {
			return nil
		}
		targetObj = next
	}

	// When fields is empty (an untraced UPDATE), remove any stale
	// tracing.k8s.io/* propagator annotations from prior generations.
	if len(fields) == 0 {
		metaMap, ok := targetObj["metadata"].(map[string]any)
		if !ok {
			return nil
		}
		annMap, ok := metaMap["annotations"].(map[string]any)
		if !ok {
			return nil
		}
		var removals []PatchOperation
		for _, field := range propagatorFields {
			annKey := mapCarrierKeyToAnnotation(field)
			if _, exists := annMap[annKey]; exists {
				removals = append(removals, PatchOperation{
					Op:   "remove",
					Path: joinJSONPointer(basePath, "metadata", "annotations", annKey),
				})
			}
		}
		return removals
	}

	desiredAnnotations := make(map[string]string, len(fields))
	for _, field := range propagatorFields {
		if val := fields.Get(field); val != "" {
			desiredAnnotations[mapCarrierKeyToAnnotation(field)] = val
		}
	}

	metaRaw, hasMeta := targetObj["metadata"]
	if !hasMeta || metaRaw == nil {
		return []PatchOperation{
			{
				Op:   "add",
				Path: joinJSONPointer(basePath, "metadata"),
				Value: map[string]any{
					"annotations": desiredAnnotations,
				},
			},
		}
	}

	metaMap, ok := metaRaw.(map[string]any)
	if !ok {
		return nil
	}

	annRaw, hasAnn := metaMap["annotations"]
	if !hasAnn || annRaw == nil {
		return []PatchOperation{
			{
				Op:    "add",
				Path:  joinJSONPointer(basePath, "metadata", "annotations"),
				Value: desiredAnnotations,
			},
		}
	}

	annMap, ok := annRaw.(map[string]any)
	if !ok {
		return nil
	}

	var patches []PatchOperation
	for _, field := range propagatorFields {
		annKey := mapCarrierKeyToAnnotation(field)
		if val, ok := desiredAnnotations[annKey]; ok {
			if existing, _ := annMap[annKey].(string); existing != val {
				patches = append(patches, PatchOperation{
					Op:    "add",
					Path:  joinJSONPointer(basePath, "metadata", "annotations", annKey),
					Value: val,
				})
			}
		} else if _, hasExisting := annMap[annKey]; hasExisting {
			patches = append(patches, PatchOperation{
				Op:   "remove",
				Path: joinJSONPointer(basePath, "metadata", "annotations", annKey),
			})
		}
	}

	return patches
}

// joinJSONPointer constructs an RFC 6901 JSON Pointer path from base segments
// and leaf tokens, escaping '~' as '~0' and '/' as '~1'.
func joinJSONPointer(base []string, leaf ...string) string {
	var b strings.Builder
	for _, seg := range base {
		b.WriteByte('/')
		b.WriteString(escapeJSONPointerToken(seg))
	}
	for _, seg := range leaf {
		b.WriteByte('/')
		b.WriteString(escapeJSONPointerToken(seg))
	}
	return b.String()
}

func escapeJSONPointerToken(token string) string {
	escaped := strings.ReplaceAll(token, "~", "~0")
	return strings.ReplaceAll(escaped, "/", "~1")
}

func generateTraceparent(sampled bool) (string, error) {
	var traceID [16]byte
	if _, err := rand.Read(traceID[:]); err != nil {
		return "", err
	}
	var spanID [8]byte
	if _, err := rand.Read(spanID[:]); err != nil {
		return "", err
	}
	flags := "00"
	if sampled {
		flags = "01"
	}
	return fmt.Sprintf("00-%s-%s-%s", hex.EncodeToString(traceID[:]), hex.EncodeToString(spanID[:]), flags), nil
}

func isSampled(rate float64) bool {
	if rate >= 1.0 {
		return true
	}
	if rate <= 0.0 {
		return false
	}
	const precision = 1_000_000
	n, err := rand.Int(rand.Reader, big.NewInt(precision))
	if err != nil {
		return false
	}
	return float64(n.Int64())/float64(precision) < rate
}
