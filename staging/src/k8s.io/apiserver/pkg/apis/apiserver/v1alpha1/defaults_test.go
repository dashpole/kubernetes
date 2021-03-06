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

package v1alpha1

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apiserver/pkg/apis/apiserver"
)

func TestSetDefaults_TracingConfiguration(t *testing.T) {
	otherSamplePerMillion := int32(12378)
	otherURL := "foo:12345"
	testcases := []struct {
		name                     string
		expectedURL              *string
		expectedSamplePerMillion *int32
		config                   *apiserver.TracingConfiguration
	}{
		{
			name:                     "all-empty",
			expectedURL:              &defaultURL,
			expectedSamplePerMillion: &defaultSamplingRate,
			config: &apiserver.TracingConfiguration{
				TypeMeta: metav1.TypeMeta{
					Kind:       "",
					APIVersion: "",
				},
			},
		},
		{
			name:        "existing-url",
			expectedURL: &otherURL,
			config: &apiserver.TracingConfiguration{
				TypeMeta: metav1.TypeMeta{
					Kind:       "",
					APIVersion: "",
				},
				URL: &otherURL,
			},
		},
		{
			name:                     "existing-samples-per-million",
			expectedSamplePerMillion: &otherSamplePerMillion,
			config: &apiserver.TracingConfiguration{
				TypeMeta: metav1.TypeMeta{
					Kind:       "",
					APIVersion: "",
				},
				SamplingRatePerMillion: &otherSamplePerMillion,
			},
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			SetDefaults_TracingConfiguration(tc.config)
			if tc.expectedURL != nil && *tc.expectedURL != *tc.config.URL {
				t.Errorf("Calling SetDefaults_TracingConfiguration expected URL %v, got %v", *tc.expectedURL, *tc.config.URL)
			}
			if tc.expectedSamplePerMillion != nil && *tc.expectedSamplePerMillion != *tc.config.SamplingRatePerMillion {
				t.Errorf("Calling SetDefaults_TracingConfiguration expected Service.Port %v, got %v", *tc.expectedSamplePerMillion, *tc.config.SamplingRatePerMillion)
			}
		})
	}
}
