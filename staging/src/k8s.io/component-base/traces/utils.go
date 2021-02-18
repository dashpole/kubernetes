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

package traces

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/stdout"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/semconv"
	"google.golang.org/grpc"

	"k8s.io/klog/v2"
)

// InitTraces initializes tracing in the component.
func InitTraces(service, address string, opts ...grpc.DialOption) {
	// TODO(dashpole): replace with otlp exporter
	exporter, err := stdout.NewExporter([]stdout.Option{
		stdout.WithPrettyPrint(),
	}...)
	if err != nil {
		klog.Fatalf("failed to initialize stdout export pipeline: %v", err)
	}

	res, err := resource.New(context.Background(),
		resource.WithAttributes(
			semconv.ServiceNameKey.String(service),
		),
	)
	if err != nil {
		klog.Fatalf("Failed to create resource: %v", err)
	}

	otel.SetTracerProvider(sdktrace.NewTracerProvider(
		sdktrace.WithConfig(sdktrace.Config{
			// Preserve the sampling decision of the incoming parent, and do not start
			// additional spans otherwise.
			DefaultSampler: sdktrace.ParentBased(sdktrace.AlwaysSample())},
		),
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	))

	// Propagate trace context and baggage.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
}
