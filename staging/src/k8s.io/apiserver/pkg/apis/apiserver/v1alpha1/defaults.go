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

package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apiserver/pkg/apis/apiserver"
)

var (
	defaultPort = int32(4317)
	defaultURL  = "localhost:4317"
)

func addDefaultingFuncs(scheme *runtime.Scheme) error {
	return RegisterDefaults(scheme)
}

// SetDefaults_OpenTelemetryClientConfiguration defaults unset fields in the OpenTelemetryClientConfiguration
func SetDefaults_OpenTelemetryClientConfiguration(config *apiserver.OpenTelemetryClientConfiguration) {
	if config == nil {
		return
	}
	// Default the service port to the default OTLP port
	if config.Service != nil && config.Service.Port == nil {
		config.Service.Port = &defaultPort
	}
	// If niether URL or service is set, use the default URL
	if config.Service == nil && config.URL == nil {
		config.URL = &defaultURL
	}
}
