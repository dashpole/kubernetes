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

package filters

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/endpoints/request"
	webhookutil "k8s.io/apiserver/pkg/util/webhook"
)

func TestWithTracingWebhookPassthroughForNonPrivilegedCaller(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.NeverSample())),
		sdktrace.WithSpanProcessor(recorder),
	)
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
	})

	const incomingTraceparent = "00-4bf92f3577b34da6a3ce929d0e0e4737-00f067aa0ba902b7-01"
	var webhookTraceparent string

	webhookRT := webhookutil.WebhookTracingWrapperFor(tp)(roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		webhookTraceparent = req.Header.Get("traceparent")
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
	}))

	handler := WithTracing(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		webhookReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, "https://webhook.example.svc/mutate", http.NoBody)
		if err != nil {
			t.Errorf("failed to create webhook request: %v", err)
			return
		}
		if _, err := webhookRT.RoundTrip(webhookReq); err != nil {
			t.Errorf("webhook roundtrip failed: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}), tp)

	// Non-privileged controller ServiceAccount caller: kube-apiserver must NOT sample
	// its own internal span, but MUST pass the caller's original traceparent through to webhooks.
	req := httptest.NewRequest(http.MethodPost, "/apis/apps/v1/namespaces/default/replicasets", http.NoBody)
	req.Header.Set("traceparent", incomingTraceparent)
	req = req.WithContext(request.WithUser(req.Context(), &user.DefaultInfo{
		Name:   "system:serviceaccount:kube-system:deployment-controller",
		Groups: []string{"system:serviceaccounts", "system:authenticated"},
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if len(recorder.Ended()) != 0 {
		t.Fatalf("expected 0 internal kube-apiserver spans recorded for non-privileged caller, got %d", len(recorder.Ended()))
	}
	if webhookTraceparent != incomingTraceparent {
		t.Fatalf("webhook traceparent = %q, want %q", webhookTraceparent, incomingTraceparent)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
