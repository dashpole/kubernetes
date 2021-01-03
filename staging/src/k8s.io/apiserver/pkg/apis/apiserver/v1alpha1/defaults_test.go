/*
Copyright 2019 The Kubernetes Authors.

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

package v1alpha1

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apiserver/pkg/apis/apiserver"
)

func TestSetDefaults_OpenTelemetryClientConfiguration(t *testing.T) {
	otherPort := int32(12378)
	otherURL := "foo:12345"
	testcases := []struct {
		name                string
		expectedURL         *string
		expectedServicePort *int32
		config              *apiserver.OpenTelemetryClientConfiguration
	}{
		{
			name:        "all-empty",
			expectedURL: &defaultURL,
			config: &apiserver.OpenTelemetryClientConfiguration{
				TypeMeta: metav1.TypeMeta{
					Kind:       "",
					APIVersion: "",
				},
			},
		},
		{
			name:                "empty-service",
			expectedServicePort: &defaultPort,
			config: &apiserver.OpenTelemetryClientConfiguration{
				TypeMeta: metav1.TypeMeta{
					Kind:       "",
					APIVersion: "",
				},
				Service: &apiserver.ServiceReference{},
			},
		},
		{
			name:        "existing-url",
			expectedURL: &otherURL,
			config: &apiserver.OpenTelemetryClientConfiguration{
				TypeMeta: metav1.TypeMeta{
					Kind:       "",
					APIVersion: "",
				},
				URL: &otherURL,
			},
		},
		{
			name:                "existing-service-port",
			expectedServicePort: &otherPort,
			config: &apiserver.OpenTelemetryClientConfiguration{
				TypeMeta: metav1.TypeMeta{
					Kind:       "",
					APIVersion: "",
				},
				Service: &apiserver.ServiceReference{Port: &otherPort},
			},
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			SetDefaults_OpenTelemetryClientConfiguration(tc.config)
			if tc.expectedURL != nil && *tc.expectedURL != *tc.config.URL {
				t.Errorf("Calling SetDefaults_OpenTelemetryClientConfiguration expected URL %v, got %v", *tc.expectedURL, *tc.config.URL)
			}
			if tc.expectedServicePort != nil && *tc.expectedServicePort != *tc.config.Service.Port {
				t.Errorf("Calling SetDefaults_OpenTelemetryClientConfiguration expected Service.Port %v, got %v", *tc.expectedServicePort, *tc.config.Service.Port)
			}
		})
	}
}
