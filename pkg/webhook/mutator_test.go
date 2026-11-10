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
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	jsonpatch "gopkg.in/evanphx/json-patch.v4"
	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
)

const (
	validTraceparent1 = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
	validTraceparent2 = "00-a4f92f3577b34da6a3ce929d0e0e4736-b0f067aa0ba902b7-01"
	unsampledTP       = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-00"
	validTracestate1  = "vendor1=value1"
)

func applyPatchAndDecode(t *testing.T, rawObj []byte, patchBytes []byte) map[string]any {
	t.Helper()
	patch, err := jsonpatch.DecodePatch(patchBytes)
	if err != nil {
		t.Fatalf("DecodePatch(%s) failed: %v", string(patchBytes), err)
	}
	patchedRaw, err := patch.Apply(rawObj)
	if err != nil {
		t.Fatalf("patch.Apply(%s) on %s failed: %v", string(patchBytes), string(rawObj), err)
	}
	var out map[string]any
	if err := json.Unmarshal(patchedRaw, &out); err != nil {
		t.Fatalf("json.Unmarshal patched object failed: %v", err)
	}
	return out
}

func getNestedAnnotation(obj map[string]any, key string, path ...string) string {
	curr := obj
	for _, seg := range path {
		next, ok := curr[seg].(map[string]any)
		if !ok {
			return ""
		}
		curr = next
	}
	meta, ok := curr["metadata"].(map[string]any)
	if !ok {
		return ""
	}
	ann, ok := meta["annotations"].(map[string]any)
	if !ok {
		return ""
	}
	val, _ := ann[key].(string)
	return val
}

func TestMutator_SpecOnlyInvariant(t *testing.T) {
	baseDeployment := `{
		"apiVersion": "apps/v1",
		"kind": "Deployment",
		"metadata": {
			"name": "frontend",
			"namespace": "default",
			"annotations": {
				"tracing.k8s.io/traceparent": "` + validTraceparent1 + `",
				"tracing.k8s.io/tracestate": "` + validTracestate1 + `"
			}
		},
		"spec": {
			"replicas": 2,
			"template": {
				"metadata": {
					"labels": {"app": "frontend"},
					"annotations": {
						"tracing.k8s.io/traceparent": "` + validTraceparent1 + `",
						"tracing.k8s.io/tracestate": "` + validTracestate1 + `"
					}
				},
				"spec": {
					"containers": [{"name": "nginx", "image": "nginx:1.27"}]
				}
			}
		},
		"status": {
			"readyReplicas": 1
		}
	}`

	scaledDeployment := strings.Replace(baseDeployment, `"replicas": 2`, `"replicas": 5`, 1)
	statusChangedDeployment := strings.Replace(baseDeployment, `"readyReplicas": 1`, `"readyReplicas": 2`, 1)
	labelChangedDeployment := strings.Replace(baseDeployment, `"name": "frontend"`, `"name": "frontend", "labels": {"env": "prod"}`, 1)

	cronJobCreate := `{
		"apiVersion": "batch/v1",
		"kind": "CronJob",
		"metadata": {"name": "nightly", "namespace": "default"},
		"spec": {
			"schedule": "0 0 * * *",
			"jobTemplate": {
				"spec": {
					"template": {
						"spec": {"containers": [{"name": "job", "image": "busybox"}]}
					}
				}
			}
		}
	}`

	tests := []struct {
		name      string
		cfg       Config
		req       *admissionv1.AdmissionRequest
		headers   http.Header
		wantPatch bool
		wantTopTP string
		wantTopTS string
	}{
		{
			name: "CREATE Deployment injects top-level metadata.annotations without mutating pod template",
			req: &admissionv1.AdmissionRequest{
				UID:       types.UID("uid-create-1"),
				Operation: admissionv1.Create,
				Kind:      metav1.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
				Namespace: "default",
				Name:      "web",
				Object: runtime.RawExtension{Raw: []byte(`{
					"apiVersion": "apps/v1",
					"kind": "Deployment",
					"metadata": {"name": "web", "namespace": "default"},
					"spec": {
						"replicas": 1,
						"template": {
							"metadata": {"labels": {"app": "web"}},
							"spec": {"containers": [{"name": "web", "image": "nginx:alpine"}]}
						}
					}
				}`)},
			},
			headers: http.Header{
				"Traceparent": []string{validTraceparent1},
				"Tracestate":  []string{validTracestate1},
			},
			wantPatch: true,
			wantTopTP: validTraceparent1,
			wantTopTS: validTracestate1,
		},
		{
			name: "CREATE CronJob injects top-level metadata.annotations",
			req: &admissionv1.AdmissionRequest{
				UID:       types.UID("uid-create-cron"),
				Operation: admissionv1.Create,
				Kind:      metav1.GroupVersionKind{Group: "batch", Version: "v1", Kind: "CronJob"},
				Namespace: "default",
				Name:      "nightly",
				Object:    runtime.RawExtension{Raw: []byte(cronJobCreate)},
			},
			headers: http.Header{
				"Traceparent": []string{validTraceparent1},
			},
			wantPatch: true,
			wantTopTP: validTraceparent1,
		},
		{
			name: "UPDATE ConfigMap with data change updates traceparent (specless resource)",
			req: &admissionv1.AdmissionRequest{
				UID:       types.UID("uid-update-cm"),
				Operation: admissionv1.Update,
				Kind:      metav1.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"},
				Namespace: "default",
				Name:      "app-config",
				OldObject: runtime.RawExtension{Raw: []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"app-config"},"data":{"k":"v1"}}`)},
				Object:    runtime.RawExtension{Raw: []byte(`{"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"app-config"},"data":{"k":"v2"}}`)},
			},
			headers: http.Header{
				"Traceparent": []string{validTraceparent1},
			},
			wantPatch: true,
			wantTopTP: validTraceparent1,
		},
		{
			name: "UPDATE Deployment with .spec mutation updates traceparent and clears stale tracestate",
			req: &admissionv1.AdmissionRequest{
				UID:       types.UID("uid-update-spec"),
				Operation: admissionv1.Update,
				Kind:      metav1.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
				Namespace: "default",
				Name:      "frontend",
				OldObject: runtime.RawExtension{Raw: []byte(baseDeployment)},
				Object:    runtime.RawExtension{Raw: []byte(scaledDeployment)},
			},
			headers: http.Header{
				"Traceparent": []string{validTraceparent2},
			},
			wantPatch: true,
			wantTopTP: validTraceparent2,
			wantTopTS: "",
		},
		{
			name: "UPDATE Deployment /status subresource is completely ignored even if spec payload differs",
			req: &admissionv1.AdmissionRequest{
				UID:         types.UID("uid-status-subresource"),
				Operation:   admissionv1.Update,
				SubResource: "status",
				Kind:        metav1.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
				Namespace:   "default",
				Name:        "frontend",
				OldObject:   runtime.RawExtension{Raw: []byte(baseDeployment)},
				Object:      runtime.RawExtension{Raw: []byte(scaledDeployment)},
			},
			headers: http.Header{
				"Traceparent": []string{validTraceparent2},
			},
			wantPatch: false,
		},
		{
			name: "UPDATE Deployment /scale subresource is ignored",
			req: &admissionv1.AdmissionRequest{
				UID:         types.UID("uid-scale-subresource"),
				Operation:   admissionv1.Update,
				SubResource: "scale",
				Kind:        metav1.GroupVersionKind{Group: "autoscaling", Version: "v1", Kind: "Scale"},
				Namespace:   "default",
				Name:        "frontend",
				OldObject:   runtime.RawExtension{Raw: []byte(baseDeployment)},
				Object:      runtime.RawExtension{Raw: []byte(scaledDeployment)},
			},
			headers: http.Header{
				"Traceparent": []string{validTraceparent2},
			},
			wantPatch: false,
		},
		{
			name: "UPDATE Deployment with only .status changed on main resource is bypassed (Spec-Only Invariant)",
			req: &admissionv1.AdmissionRequest{
				UID:       types.UID("uid-update-status-only"),
				Operation: admissionv1.Update,
				Kind:      metav1.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
				Namespace: "default",
				Name:      "frontend",
				OldObject: runtime.RawExtension{Raw: []byte(baseDeployment)},
				Object:    runtime.RawExtension{Raw: []byte(statusChangedDeployment)},
			},
			headers: http.Header{
				"Traceparent": []string{validTraceparent2},
			},
			wantPatch: false,
		},
		{
			name: "UPDATE Deployment with only metadata.labels changed is bypassed (Spec-Only Invariant)",
			req: &admissionv1.AdmissionRequest{
				UID:       types.UID("uid-update-labels-only"),
				Operation: admissionv1.Update,
				Kind:      metav1.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
				Namespace: "default",
				Name:      "frontend",
				OldObject: runtime.RawExtension{Raw: []byte(baseDeployment)},
				Object:    runtime.RawExtension{Raw: []byte(labelChangedDeployment)},
			},
			headers: http.Header{
				"Traceparent": []string{validTraceparent2},
			},
			wantPatch: false,
		},
		{
			name: "DELETE operation is ignored",
			req: &admissionv1.AdmissionRequest{
				UID:       types.UID("uid-delete"),
				Operation: admissionv1.Delete,
				Kind:      metav1.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
				Namespace: "default",
				Name:      "frontend",
				OldObject: runtime.RawExtension{Raw: []byte(baseDeployment)},
			},
			headers: http.Header{
				"Traceparent": []string{validTraceparent2},
			},
			wantPatch: false,
		},
		{
			name: "CREATE with invalid traceparent header and GenerateIfMissing=false emits no patch",
			req: &admissionv1.AdmissionRequest{
				UID:       types.UID("uid-invalid-tp"),
				Operation: admissionv1.Create,
				Kind:      metav1.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"},
				Namespace: "default",
				Name:      "pod-1",
				Object:    runtime.RawExtension{Raw: []byte(`{"apiVersion":"v1","kind":"Pod","metadata":{"name":"pod-1"},"spec":{"containers":[{"name":"c","image":"nginx"}]}}`)},
			},
			headers: http.Header{
				"Traceparent": []string{"00-invalid-traceparent-01"},
			},
			wantPatch: false,
		},
		{
			name: "CREATE with RequireSampled=true skips unsampled traceparent even when GenerateIfMissing=true",
			cfg:  Config{RequireSampled: true, GenerateIfMissing: true, SamplingRate: 1.0},
			req: &admissionv1.AdmissionRequest{
				UID:       types.UID("uid-unsampled"),
				Operation: admissionv1.Create,
				Kind:      metav1.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"},
				Namespace: "default",
				Name:      "pod-1",
				Object:    runtime.RawExtension{Raw: []byte(`{"apiVersion":"v1","kind":"Pod","metadata":{"name":"pod-1"},"spec":{"containers":[{"name":"c","image":"nginx"}]}}`)},
			},
			headers: http.Header{
				"Traceparent": []string{unsampledTP},
			},
			wantPatch: false,
		},
		{
			name: "CREATE with RequireSampled=true skips unsampled pre-existing object traceparent",
			cfg:  Config{RequireSampled: true, GenerateIfMissing: true, SamplingRate: 1.0},
			req: &admissionv1.AdmissionRequest{
				UID:       types.UID("uid-unsampled-obj"),
				Operation: admissionv1.Create,
				Kind:      metav1.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
				Namespace: "default",
				Name:      "web",
				Object: runtime.RawExtension{Raw: []byte(`{
					"apiVersion": "apps/v1",
					"kind": "Deployment",
					"metadata": {
						"name": "web",
						"annotations": {"tracing.k8s.io/traceparent": "` + unsampledTP + `"}
					},
					"spec": {
						"replicas": 1,
						"template": {
							"spec": {"containers": [{"name": "web", "image": "nginx:alpine"}]}
						}
					}
				}`)},
			},
			headers:   http.Header{},
			wantPatch: false,
		},
		{
			name: "UPDATE Deployment with .spec mutation and no HTTP traceparent removes stale prior-generation trace annotations",
			req: &admissionv1.AdmissionRequest{
				UID:       types.UID("uid-update-untraced-spec"),
				Operation: admissionv1.Update,
				Kind:      metav1.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
				Namespace: "default",
				Name:      "frontend",
				OldObject: runtime.RawExtension{Raw: []byte(baseDeployment)},
				Object:    runtime.RawExtension{Raw: []byte(scaledDeployment)},
			},
			headers:   http.Header{},
			wantPatch: true,
			wantTopTP: "",
			wantTopTS: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mutator := NewMutator(tc.cfg)
			resp := mutator.Mutate(context.Background(), tc.req, tc.headers)
			if !resp.Allowed {
				t.Fatalf("expected Allowed=true, got false")
			}
			if resp.UID != tc.req.UID {
				t.Fatalf("resp.UID = %q, want %q", resp.UID, tc.req.UID)
			}

			if !tc.wantPatch {
				if len(resp.Patch) != 0 {
					t.Fatalf("expected nil/empty Patch, got %s", string(resp.Patch))
				}
				return
			}

			if len(resp.Patch) == 0 {
				t.Fatalf("expected non-empty JSONPatch, got empty")
			}
			if resp.PatchType == nil || *resp.PatchType != admissionv1.PatchTypeJSONPatch {
				t.Fatalf("expected PatchTypeJSONPatch, got %v", resp.PatchType)
			}

			patchedObj := applyPatchAndDecode(t, tc.req.Object.Raw, resp.Patch)
			if gotTP := getNestedAnnotation(patchedObj, AnnotationPrefix+"traceparent"); gotTP != tc.wantTopTP {
				t.Errorf("top-level %s = %q, want %q", AnnotationPrefix+"traceparent", gotTP, tc.wantTopTP)
			}
			if gotTS := getNestedAnnotation(patchedObj, AnnotationPrefix+"tracestate"); gotTS != tc.wantTopTS {
				t.Errorf("top-level %s = %q, want %q", AnnotationPrefix+"tracestate", gotTS, tc.wantTopTS)
			}
		})
	}
}

func TestMutator_GenerateIfMissingAndFailOpen(t *testing.T) {
	mutator := NewMutator(Config{
		GenerateIfMissing: true,
		SamplingRate:      1.0,
	})

	req := &admissionv1.AdmissionRequest{
		UID:       types.UID("uid-gen"),
		Operation: admissionv1.Create,
		Kind:      metav1.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"},
		Namespace: "default",
		Name:      "pod-gen",
		Object:    runtime.RawExtension{Raw: []byte(`{"apiVersion":"v1","kind":"Pod","spec":{"containers":[{"name":"c","image":"nginx"}]}}`)},
	}

	resp := mutator.Mutate(context.Background(), req, nil)
	if !resp.Allowed || len(resp.Patch) == 0 {
		t.Fatalf("expected generated patch when GenerateIfMissing=true and SamplingRate=1.0")
	}
	patched := applyPatchAndDecode(t, req.Object.Raw, resp.Patch)
	gotTP := getNestedAnnotation(patched, AnnotationPrefix+"traceparent")
	if !strings.HasPrefix(gotTP, "00-") || !strings.HasSuffix(gotTP, "-01") || len(gotTP) != 55 {
		t.Fatalf("generated traceparent %q is not a valid 55-char sampled W3C traceparent", gotTP)
	}

	// Verify SamplingRate=0.0 does not generate
	zeroMutator := NewMutator(Config{
		GenerateIfMissing: true,
		SamplingRate:      0.0,
	})
	zeroResp := zeroMutator.Mutate(context.Background(), req, nil)
	if !zeroResp.Allowed || len(zeroResp.Patch) != 0 {
		t.Fatalf("expected 0 patches when SamplingRate=0.0")
	}

	// Verify malformed JSON fails open (Allowed: true, Patch: nil)
	badReq := &admissionv1.AdmissionRequest{
		UID:       types.UID("uid-bad-json"),
		Operation: admissionv1.Create,
		Object:    runtime.RawExtension{Raw: []byte(`{not-valid-json`)},
	}
	badResp := mutator.Mutate(context.Background(), badReq, http.Header{"Traceparent": []string{validTraceparent1}})
	if !badResp.Allowed || len(badResp.Patch) != 0 {
		t.Fatalf("expected fail-open Allowed=true with nil patch on malformed JSON, got Allowed=%v Patch=%s", badResp.Allowed, string(badResp.Patch))
	}
}
