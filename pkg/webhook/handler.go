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
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

// DefaultMaxRequestBodyBytes caps the AdmissionReview payload size (10 MiB,
// matching kube-apiserver's default max request size limits).
const DefaultMaxRequestBodyBytes int64 = 10 * 1024 * 1024

// Handler serves Kubernetes MutatingAdmissionWebhook HTTP endpoints.
type Handler struct {
	mutator      *Mutator
	maxBodyBytes int64
}

// NewHandler constructs a new HTTP Handler wrapping the given Mutator.
func NewHandler(mutator *Mutator) *Handler {
	if mutator == nil {
		mutator = NewMutator(Config{})
	}
	return &Handler{
		mutator:      mutator,
		maxBodyBytes: DefaultMaxRequestBodyBytes,
	}
}

// RegisterRoutes registers /mutate, /healthz, and /readyz on the provided ServeMux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/mutate", h.ServeMutate)
	mux.HandleFunc("/healthz", h.ServeHealthz)
	mux.HandleFunc("/readyz", h.ServeHealthz)
}

// ServeMutate handles POST /mutate containing an admission.k8s.io/v1 AdmissionReview.
func (h *Handler) ServeMutate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed; expected POST", http.StatusMethodNotAllowed)
		return
	}

	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		http.Error(w, "unsupported media type; expected application/json", http.StatusUnsupportedMediaType)
		return
	}

	limit := h.maxBodyBytes
	if limit <= 0 {
		limit = DefaultMaxRequestBodyBytes
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	defer func() {
		_ = r.Body.Close()
	}()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}

	var review admissionv1.AdmissionReview
	if err := json.Unmarshal(body, &review); err != nil {
		http.Error(w, "could not decode AdmissionReview JSON", http.StatusBadRequest)
		return
	}

	if review.Request == nil {
		http.Error(w, "AdmissionReview.Request is nil", http.StatusBadRequest)
		return
	}

	resp := h.mutator.Mutate(r.Context(), review.Request, r.Header)

	outReview := admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			APIVersion: admissionv1.SchemeGroupVersion.String(),
			Kind:       "AdmissionReview",
		},
		Response: resp,
	}
	if review.APIVersion != "" {
		outReview.APIVersion = review.APIVersion
	}
	if review.Kind != "" {
		outReview.Kind = review.Kind
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(outReview); err != nil {
		klog.ErrorS(err, "Failed to encode AdmissionReview response")
	}
}

// ServeHealthz responds 200 OK to liveness and readiness probes.
func (h *Handler) ServeHealthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}
