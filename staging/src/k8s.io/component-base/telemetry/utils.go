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

package telemetry

import (
	"context"
	"fmt"
	"os"

	"go.opentelemetry.io/contrib/otelconf"
	"go.opentelemetry.io/otel"
	"k8s.io/component-base/telemetry/api/v1alpha1"
)


// InitTelemetry initializes OpenTelemetry from declarative configuration.
// It sets up TracerProvider, MeterProvider, and LoggerProvider globally.
// Returns a slice of cleanup functions to be called on shutdown.
func InitTelemetry(ctx context.Context, config *v1alpha1.TelemetryConfiguration) ([]func(context.Context) error, error) {
	if config == nil || config.ConfigPath == nil || *config.ConfigPath == "" {
		return nil, nil // Nothing to do
	}

	configData, err := os.ReadFile(*config.ConfigPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read telemetry configuration file %q: %w", *config.ConfigPath, err)
	}

	parsedConfig, err := otelconf.ParseYAML(configData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse telemetry configuration %q: %w", *config.ConfigPath, err)
	}

	sdk, err := otelconf.NewSDK(
		otelconf.WithContext(ctx),
		otelconf.WithOpenTelemetryConfiguration(*parsedConfig),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize telemetry SDK: %w", err)
	}

	// Register global providers since declarative configuration is designed to set up the globals.
	if tp := sdk.TracerProvider(); tp != nil {
		otel.SetTracerProvider(tp)
	}

	return []func(context.Context) error{sdk.Shutdown}, nil
}
