/*
Copyright 2021 The Kubernetes Authors.

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

package opentelemetry

import (
	"fmt"
	"io/ioutil"
	"os"
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apiserver/pkg/apis/apiserver"
)

var (
	localhost = "localhost:4317"
)

func strptr(s string) *string {
	return &s
}

func TestReadOpenTelemetryConfiguration(t *testing.T) {
	testcases := []struct {
		name           string
		contents       string
		createFile     bool
		expectedResult *apiserver.OpenTelemetryClientConfiguration
		expectedError  *string
	}{
		{
			name:           "empty",
			createFile:     true,
			contents:       ``,
			expectedResult: &apiserver.OpenTelemetryClientConfiguration{},
			expectedError:  nil,
		},
		{
			name:           "absent",
			createFile:     false,
			contents:       ``,
			expectedResult: nil,
			expectedError:  strptr("unable to read opentelemetry configuration from \"test-opentelemetry-config-absent\": open test-opentelemetry-config-absent: no such file or directory"),
		},
		{
			name:       "v1alpha1",
			createFile: true,
			contents: `
apiVersion: apiserver.config.k8s.io/v1alpha1
kind: OpenTelemetryClientConfiguration
url: localhost:4317
`,
			expectedResult: &apiserver.OpenTelemetryClientConfiguration{
				TypeMeta: metav1.TypeMeta{
					Kind:       "",
					APIVersion: "",
				},
				URL: &localhost,
			},
			expectedError: nil,
		},
		{
			name:       "wrong_type",
			createFile: true,
			contents: `
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: agent
spec:
  selector:
    matchLabels:
      k8s-app: agent
  template:
    metadata:
      labels:
        k8s-app: agent
    spec:
      containers:
        - image: k8s.gcr.io/busybox
          name: agent
`,
			expectedResult: nil,
			expectedError:  strptr("unable to decode opentelemetry configuration data: no kind \"DaemonSet\" is registered for version \"apps/v1\" in scheme \"k8s.io/apiserver/pkg/opentelemetry/config.go:32\""),
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			proxyConfig := fmt.Sprintf("test-opentelemetry-config-%s", tc.name)
			if tc.createFile {
				f, err := ioutil.TempFile("", proxyConfig)
				if err != nil {
					t.Fatal(err)
				}
				defer os.Remove(f.Name())
				if err := ioutil.WriteFile(f.Name(), []byte(tc.contents), os.FileMode(0755)); err != nil {
					t.Fatal(err)
				}
				proxyConfig = f.Name()
			}
			config, err := ReadOpenTelemetryConfiguration(proxyConfig)
			if err == nil && tc.expectedError != nil {
				t.Errorf("calling ReadOpenTelemetryConfiguration expected error: %s, did not get it", *tc.expectedError)
			}
			if err != nil && tc.expectedError == nil {
				t.Errorf("unexpected error calling ReadOpenTelemetryConfiguration got: %#v", err)
			}
			if err != nil && tc.expectedError != nil && err.Error() != *tc.expectedError {
				t.Errorf("calling ReadOpenTelemetryConfiguration expected error: %s, got %#v", *tc.expectedError, err)
			}
			if !reflect.DeepEqual(config, tc.expectedResult) {
				t.Errorf("problem with configuration returned from ReadOpenTelemetryConfiguration expected: %#v, got: %#v", tc.expectedResult, config)
			}
		})
	}
}

func TestValidateOpenTelemetryConfiguration(t *testing.T) {
	port := int32(12378)
	testcases := []struct {
		name        string
		expectError bool
		contents    *apiserver.OpenTelemetryClientConfiguration
	}{
		{
			name:        "url-valid",
			expectError: false,
			contents: &apiserver.OpenTelemetryClientConfiguration{
				TypeMeta: metav1.TypeMeta{
					Kind:       "",
					APIVersion: "",
				},
				URL: &localhost,
			},
		},
		{
			name:        "service-valid",
			expectError: false,
			contents: &apiserver.OpenTelemetryClientConfiguration{
				TypeMeta: metav1.TypeMeta{
					Kind:       "",
					APIVersion: "",
				},
				Service: &apiserver.ServiceReference{
					Name:      "service",
					Namespace: "namespace",
				},
			},
		},
		{
			name:        "service-valid-with-port",
			expectError: false,
			contents: &apiserver.OpenTelemetryClientConfiguration{
				TypeMeta: metav1.TypeMeta{
					Kind:       "",
					APIVersion: "",
				},
				Service: &apiserver.ServiceReference{
					Name:      "service",
					Namespace: "namespace",
					Port:      &port,
				},
			},
		},
		{
			name:        "service-invalid-name",
			expectError: true,
			contents: &apiserver.OpenTelemetryClientConfiguration{
				TypeMeta: metav1.TypeMeta{
					Kind:       "",
					APIVersion: "",
				},
				Service: &apiserver.ServiceReference{
					Namespace: "namespace",
				},
			},
		},
		{
			name:        "service-invalid-namespace",
			expectError: true,
			contents: &apiserver.OpenTelemetryClientConfiguration{
				TypeMeta: metav1.TypeMeta{
					Kind:       "",
					APIVersion: "",
				},
				Service: &apiserver.ServiceReference{
					Name: "service",
				},
			},
		},
		{
			name:        "url-and-service-invalid",
			expectError: true,
			contents: &apiserver.OpenTelemetryClientConfiguration{
				TypeMeta: metav1.TypeMeta{
					Kind:       "",
					APIVersion: "",
				},
				URL: &localhost,
				Service: &apiserver.ServiceReference{
					Name:      "service",
					Namespace: "namespace",
				},
			},
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			errs := ValidateOpenTelemetryConfiguration(tc.contents)
			if tc.expectError == false && len(errs) != 0 {
				t.Errorf("Calling ValidateOpenTelemetryConfiguration expected no error, got %v", errs)
			} else if tc.expectError == true && len(errs) == 0 {
				t.Errorf("Calling ValidateOpenTelemetryConfiguration expected error, got no error")
			}
		})
	}
}
