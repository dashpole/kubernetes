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

package traces

import (
	"go.opentelemetry.io/otel/api/global"
	"go.opentelemetry.io/otel/api/kv"
	"go.opentelemetry.io/otel/exporters/otlp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"k8s.io/klog/v2"
)

// InitTraces initializes tracing in the component.
// Components must use the OTLP exporter, but can pass additional exporter
// options if needed
func InitTraces(service string, opts ...otlp.ExporterOption) {
	opts = append(opts, otlp.WithInsecure())
	exporter, err := otlp.NewExporter(opts...)
	if err != nil {
		klog.Fatalf("Failed to create OTLP exporter: %v", err)
	}

	// Use AlwaysParentSample() to preserve the sampling decision of the
	// parent, but not start additional spans.
	tp, err := sdktrace.NewProvider(
		sdktrace.WithConfig(sdktrace.Config{
			DefaultSampler: sdktrace.AlwaysParentSample()},
		),
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(resource.New(
			kv.Key("service.name").String(service),
		)),
	)
	if err != nil {
		klog.Fatalf("Failed to set provider: %v", err)
	}
	global.SetTraceProvider(tp)
}
