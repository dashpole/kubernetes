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
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
)

func TestHandler_Endpoints(t *testing.T) {
	handler := NewHandler(NewMutator(Config{}))
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	// 1. Healthz and Readyz
	for _, endpoint := range []string{"/healthz", "/readyz"} {
		req := httptest.NewRequest(http.MethodGet, endpoint, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s returned %d, want 200", endpoint, rec.Code)
		}
	}

	// 2. Method Not Allowed on GET /mutate
	{
		req := httptest.NewRequest(http.MethodGet, "/mutate", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("GET /mutate returned %d, want 405", rec.Code)
		}
	}

	// 3. Unsupported Media Type on text/plain or missing Content-Type
	for _, ct := range []string{"text/plain", ""} {
		req := httptest.NewRequest(http.MethodPost, "/mutate", strings.NewReader("{}"))
		if ct != "" {
			req.Header.Set("Content-Type", ct)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnsupportedMediaType {
			t.Fatalf("POST /mutate with Content-Type %q returned %d, want 415", ct, rec.Code)
		}
	}

	// 4. Request Entity Too Large
	{
		smallHandler := NewHandler(NewMutator(Config{}))
		smallHandler.maxBodyBytes = 32
		req := httptest.NewRequest(http.MethodPost, "/mutate", strings.NewReader(strings.Repeat("a", 128)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		smallHandler.ServeMutate(rec, req)
		if rec.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("oversized POST /mutate returned %d, want 413", rec.Code)
		}
	}

	// 5. Valid AdmissionReview preserves TypeMeta and returns JSONPatch
	{
		inReview := admissionv1.AdmissionReview{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "admission.k8s.io/v1",
				Kind:       "AdmissionReview",
			},
			Request: &admissionv1.AdmissionRequest{
				UID:       types.UID("req-123"),
				Operation: admissionv1.Create,
				Kind:      metav1.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"},
				Namespace: "default",
				Name:      "demo",
				Object:    runtime.RawExtension{Raw: []byte(`{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"demo"},"spec":{"replicas":1}}`)},
			},
		}
		body, err := json.Marshal(inReview)
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}

		req := httptest.NewRequest(http.MethodPost, "/mutate", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
		req.Header.Set("traceparent", validTraceparent1)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("POST /mutate returned %d (%s), want 200", rec.Code, rec.Body.String())
		}

		var outReview admissionv1.AdmissionReview
		if err := json.Unmarshal(rec.Body.Bytes(), &outReview); err != nil {
			t.Fatalf("json.Unmarshal response: %v", err)
		}
		if outReview.APIVersion != "admission.k8s.io/v1" || outReview.Kind != "AdmissionReview" {
			t.Fatalf("outReview TypeMeta = %s/%s, want admission.k8s.io/v1 AdmissionReview", outReview.APIVersion, outReview.Kind)
		}
		if outReview.Response == nil || !outReview.Response.Allowed {
			t.Fatalf("expected Allowed=true response")
		}
		if outReview.Response.UID != types.UID("req-123") {
			t.Fatalf("UID = %q, want req-123", outReview.Response.UID)
		}
		if len(outReview.Response.Patch) == 0 {
			t.Fatalf("expected non-empty JSONPatch in response")
		}
	}
}
