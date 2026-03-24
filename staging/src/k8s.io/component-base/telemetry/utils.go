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

	"github.com/go-logr/logr"
	"go.opentelemetry.io/contrib/bridges/otellogr"
	"go.opentelemetry.io/contrib/otelconf"
	"go.opentelemetry.io/otel"
	telemetryapi "k8s.io/component-base/telemetry/api/v1alpha1"
	"k8s.io/klog/v2"
)


// InitTelemetry initializes OpenTelemetry providers based on the declarative TelemetryConfiguration.
func InitTelemetry(ctx context.Context, config *telemetryapi.TelemetryConfiguration) ([]func(context.Context) error, error) {
	if config == nil || config.ConfigPath == nil || *config.ConfigPath == "" {
		return nil, nil // Opt-in only
	}

	configData, err := os.ReadFile(*config.ConfigPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read telemetry configuration: %w", err)
	}

	parsedConfig, err := otelconf.ParseYAML(configData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse telemetry configuration %q: %w", *config.ConfigPath, err)
	}

	meterOpts, err := hackPrometheusBridge(ctx, parsedConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to configure metrics bridge hack: %w", err)
	}

	sdk, err := otelconf.NewSDK(
		otelconf.WithContext(ctx),
		otelconf.WithMeterProviderOptions(meterOpts...),
		otelconf.WithOpenTelemetryConfiguration(*parsedConfig),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize telemetry SDK: %w", err)
	}

	// Step 8: Logging Bridge
	// We use a custom teeSink to retain klog's original text/json output 
	// while simultaneously exporting to the OpenTelemetry LoggerProvider via otellogr.
	if parsedConfig.LoggerProvider != nil && len(parsedConfig.LoggerProvider.Processors) > 0 {
		importOtellogrAndBridgeToKlog(sdk)
	}

	// Register global providers since declarative configuration is designed to set up the globals.
	if tp := sdk.TracerProvider(); tp != nil {
		otel.SetTracerProvider(tp)
	}

	return []func(context.Context) error{sdk.Shutdown}, nil
}

// teeSink duplicates log records to two logr.LogSinks
type teeSink struct {
	sink1 logr.LogSink
	sink2 logr.LogSink
}

func (t teeSink) Init(info logr.RuntimeInfo) {
	t.sink1.Init(info)
	t.sink2.Init(info)
}

func (t teeSink) Enabled(level int) bool {
	return t.sink1.Enabled(level) || t.sink2.Enabled(level)
}

func (t teeSink) Info(level int, msg string, keysAndValues ...interface{}) {
	if t.sink1.Enabled(level) {
		t.sink1.Info(level, msg, keysAndValues...)
	}
	if t.sink2.Enabled(level) {
		t.sink2.Info(level, msg, keysAndValues...)
	}
}

func (t teeSink) Error(err error, msg string, keysAndValues ...interface{}) {
	t.sink1.Error(err, msg, keysAndValues...)
	t.sink2.Error(err, msg, keysAndValues...)
}

func (t teeSink) WithValues(keysAndValues ...interface{}) logr.LogSink {
	return teeSink{
		sink1: t.sink1.WithValues(keysAndValues...),
		sink2: t.sink2.WithValues(keysAndValues...),
	}
}

func (t teeSink) WithName(name string) logr.LogSink {
	return teeSink{
		sink1: t.sink1.WithName(name),
		sink2: t.sink2.WithName(name),
	}
}

func (t teeSink) WithCallDepth(depth int) logr.LogSink {
	s1 := t.sink1
	if cw, ok := t.sink1.(logr.CallDepthLogSink); ok {
		s1 = cw.WithCallDepth(depth)
	}
	s2 := t.sink2
	if cw, ok := t.sink2.(logr.CallDepthLogSink); ok {
		s2 = cw.WithCallDepth(depth)
	}
	return teeSink{
		sink1: s1,
		sink2: s2,
	}
}

func importOtellogrAndBridgeToKlog(sdk otelconf.SDK) {
	// get the primary Klog sink
	baseSink := klog.Background().GetSink()
	
	// get the OTLP sink from the bridge
	otelSink := otellogr.NewLogSink("k8s.io/component-base/telemetry/klog", otellogr.WithLoggerProvider(sdk.LoggerProvider()))
	
	dtSink := teeSink{
		sink1: baseSink,
		sink2: otelSink,
	}
	
	// Create a new logger with the tee sink and replace Klog's background logger
	duoLogger := logr.New(dtSink)
	klog.SetLogger(duoLogger)
}
