/*
Copyright 2024 The Kubernetes Authors.

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
	"path/filepath"

	"k8s.io/apimachinery/pkg/util/validation/field"
)

// ValidateTelemetryConfiguration validates the given telemetry configuration.
func ValidateTelemetryConfiguration(config *TelemetryConfiguration, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}

	if config == nil {
		return allErrs
	}

	if config.ConfigPath != nil {
		if len(*config.ConfigPath) == 0 {
			allErrs = append(allErrs, field.Invalid(fldPath.Child("configPath"), *config.ConfigPath, "must not be empty if specified"))
		} else if !filepath.IsAbs(*config.ConfigPath) {
			allErrs = append(allErrs, field.Invalid(fldPath.Child("configPath"), *config.ConfigPath, "must be an absolute path"))
		}
	}

	return allErrs
}

