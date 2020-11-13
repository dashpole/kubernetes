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

package opentelemetry

import (
	"fmt"
	"io/ioutil"
	"net/url"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/apiserver/pkg/apis/apiserver"
	"k8s.io/apiserver/pkg/apis/apiserver/install"
	"k8s.io/apiserver/pkg/apis/apiserver/v1alpha1"
	"sigs.k8s.io/yaml"
)

var (
	cfgScheme = runtime.NewScheme()

	defaultPort = int32(55680)
	defaultURL  = "localhost:55680"
)

func init() {
	install.Install(cfgScheme)
}

// ReadOpenTelemetryConfiguration reads the opentelemetry configuration from a file
func ReadOpenTelemetryConfiguration(configFilePath string) (*apiserver.OpenTelemetryClientConfiguration, error) {
	if configFilePath == "" {
		return nil, fmt.Errorf("opentelemetry config file was empty")
	}
	data, err := ioutil.ReadFile(configFilePath)
	if err != nil {
		return nil, fmt.Errorf("unable to read opentelemetry configuration from %q [%v]", configFilePath, err)
	}
	var decodedConfig v1alpha1.OpenTelemetryClientConfiguration
	err = yaml.Unmarshal(data, &decodedConfig)
	if err != nil {
		// we got an error where the decode wasn't related to a missing type
		return nil, err
	}
	if decodedConfig.Kind != "OpenTelemetryClientConfiguration" {
		return nil, fmt.Errorf("invalid service configuration object %q", decodedConfig.Kind)
	}
	internalConfig := &apiserver.OpenTelemetryClientConfiguration{}
	if err := cfgScheme.Convert(&decodedConfig, internalConfig, nil); err != nil {
		// we got an error where the decode wasn't related to a missing type
		return nil, err
	}
	return internalConfig, nil
}

// DefaultOpenTelemetryConfiguration defaults unset fields in the OpenTelemetryClientConfiguration
func DefaultOpenTelemetryConfiguration(config *apiserver.OpenTelemetryClientConfiguration) {
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

// ValidateOpenTelemetryConfiguration validates the opentelemetry configuration
func ValidateOpenTelemetryConfiguration(config *apiserver.OpenTelemetryClientConfiguration) field.ErrorList {
	allErrs := field.ErrorList{}
	if config == nil {
		// OpenTelemetry is disabled
		return allErrs
	}
	if config.Service != nil && config.URL != nil {
		allErrs = append(allErrs, field.Invalid(
			field.NewPath("service"),
			config.Service,
			"Service and URL cannot both be set"))
	}
	if config.Service != nil {
		allErrs = append(allErrs, validateService(config.Service, field.NewPath("service"))...)
	}
	if config.URL != nil {
		allErrs = append(allErrs, validateURL(*config.URL, field.NewPath("url"))...)
	}
	return allErrs
}

func validateService(service *apiserver.ServiceReference, fldPath *field.Path) field.ErrorList {
	allErrors := field.ErrorList{}

	if len(service.Name) == 0 {
		allErrors = append(allErrors, field.Required(fldPath.Child("name"), "service name is required"))
	}

	if len(service.Namespace) == 0 {
		allErrors = append(allErrors, field.Required(fldPath.Child("namespace"), "service namespace is required"))
	}
	return allErrors
}

func validateURL(u string, fldPath *field.Path) field.ErrorList {
	errs := field.ErrorList{}
	_, err := url.Parse(u)
	if err != nil {
		return append(errs, field.Invalid(
			fldPath, u,
			fmt.Sprintf("Unable to parse URL: %v", err)))
	}
	return errs
}
