/*
Copyright 2020 The Kubernetes Authors.

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

package metrics

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apimachineryversion "k8s.io/apimachinery/pkg/version"
)

func TestResetHandler(t *testing.T) {
	currentVersion := apimachineryversion.Info{
		Major:      "1",
		Minor:      "17",
		GitVersion: "v1.17.1-alpha-1.12345",
	}
	registry := newKubeRegistry(currentVersion)
	resetHandler := HandlerWithReset(registry, HandlerOpts{})
	testCases := []struct {
		desc         string
		method       string
		expectedBody string
	}{
		{
			desc:         "Should return empty body on a get",
			method:       http.MethodGet,
			expectedBody: "",
		},
		{
			desc:         "Should return 'metrics reset' in the body on a delete",
			method:       http.MethodDelete,
			expectedBody: "metrics reset\n",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			req, err := http.NewRequest(tc.method, "http://sample.com/metrics", nil)
			if err != nil {
				t.Fatalf("Error creating http request")
			}
			rec := httptest.NewRecorder()
			resetHandler.ServeHTTP(rec, req)
			body, err := io.ReadAll(rec.Result().Body)
			if err != nil {
				t.Fatalf("Error reading response body")
			}
			if string(body) != tc.expectedBody {
				t.Errorf("Got '%s' as the response body, but want '%v'", body, tc.expectedBody)
			}
		})
	}
}

func TestHandlerOpenMetricsNegotiation(t *testing.T) {
	registry := newKubeRegistry(apimachineryversion.Info{
		Major:      "1",
		Minor:      "17",
		GitVersion: "v1.17.1-alpha-1.12345",
	})
	counter := NewCounter(&CounterOpts{
		Name:           "test_openmetrics_requests_total",
		Help:           "Test counter for OpenMetrics negotiation.",
		StabilityLevel: ALPHA,
	})
	registry.MustRegister(counter)
	counter.Inc()

	handlers := map[string]http.Handler{
		"HandlerFor":       HandlerFor(registry, HandlerOpts{}),
		"HandlerWithReset": HandlerWithReset(registry, HandlerOpts{}),
	}
	testCases := []struct {
		name               string
		accept             string
		wantContentTypeSub string
		wantEOF            bool
	}{
		{
			name:               "OpenMetrics 2.0-rc",
			accept:             "application/openmetrics-text;version=2.0.0-rc;charset=utf-8",
			wantContentTypeSub: "application/openmetrics-text; version=2.0.0-rc",
			wantEOF:            true,
		},
		{
			name:               "OpenMetrics 1.0.0",
			accept:             "application/openmetrics-text;version=1.0.0;charset=utf-8",
			wantContentTypeSub: "application/openmetrics-text; version=1.0.0",
			wantEOF:            true,
		},
		{
			name:               "Prometheus 0.0.4 fallback",
			accept:             "text/plain;version=0.0.4",
			wantContentTypeSub: "text/plain; version=0.0.4",
			wantEOF:            false,
		},
	}

	for handlerName, handler := range handlers {
		for _, tc := range testCases {
			t.Run(handlerName+"/"+tc.name, func(t *testing.T) {
				req, err := http.NewRequest(http.MethodGet, "http://sample.com/metrics", nil)
				if err != nil {
					t.Fatalf("Error creating http request: %v", err)
				}
				req.Header.Set("Accept", tc.accept)
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)

				res := rec.Result()
				if res.StatusCode != http.StatusOK {
					t.Fatalf("got status %d, want %d", res.StatusCode, http.StatusOK)
				}
				gotCT := res.Header.Get("Content-Type")
				if !strings.Contains(gotCT, tc.wantContentTypeSub) {
					t.Errorf("got Content-Type %q, want substring %q", gotCT, tc.wantContentTypeSub)
				}
				body, err := io.ReadAll(res.Body)
				if err != nil {
					t.Fatalf("Error reading response body: %v", err)
				}
				gotEOF := strings.HasSuffix(string(body), "# EOF\n")
				if gotEOF != tc.wantEOF {
					t.Errorf("got EOF=%v, want %v (body=%q)", gotEOF, tc.wantEOF, string(body))
				}
			})
		}
	}
}
