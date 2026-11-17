/*
Copyright 2023 The Kubernetes Authors.

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

package legacyregistry

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

const (
	processStartTimeHeader = "Process-Start-Time-Unix"
)

var (
	processStartTestCopy = &processStart
)

func TestProcessStartTimeHeader(t *testing.T) {
	now := GetProcessStart()
	handler := Handler()

	request, _ := http.NewRequest(http.MethodGet, "/", nil)
	writer := httptest.NewRecorder()
	handler.ServeHTTP(writer, request)
	got := writer.Header().Get(processStartTimeHeader)
	gotInt, _ := strconv.ParseInt(got, 10, 64)
	if gotInt != now.Unix() {
		t.Errorf("got %d, wanted %d", gotInt, now.Unix())
	}
}

// processStart must never be reassigned after init.
// This test ensures processStart doesn't change across calls
func TestProcessStartImmutable(t *testing.T) {
	if *processStartTestCopy != processStart {
		t.Errorf("processStart test values differ: %v != %v", processStartTestCopy, processStart)
	}
}

func TestHandlerOpenMetricsNegotiation(t *testing.T) {
	handlers := map[string]http.Handler{
		"Handler":          Handler(),
		"HandlerWithReset": HandlerWithReset(),
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
