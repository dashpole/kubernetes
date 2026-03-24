package telemetry

import (
	"context"
	"fmt"
	"time"
	_ "unsafe" // necessary for go:linkname

	"go.opentelemetry.io/contrib/bridges/prometheus"
	"go.opentelemetry.io/contrib/otelconf"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"k8s.io/component-base/metrics/legacyregistry"
)

// HACK: otelconf v0.22.0 natively ignores all `producers` configured in the MetricReader
// declarative config, meaning we cannot cleanly inject the prometheus bridge producer
// directly through the declarative configuration.
//
// This file contains a workaround using `go:linkname` to access otelconf's unexported
// exporter constructors. We manually extract the Exporter from the read config, construct 
// it using otelconf's logic, and then construct our OWN PeriodicReader that includes
// the prometheus metric.Producer. Finally, we wipe the readers from the config so 
// otelconf doesn't duplicate them.
//
// When upstream otelconf supports metric producers for prometheus, this entire file 
// should be deleted and replaced with native declarative configurability.

//go:linkname otlpHTTPMetricExporter go.opentelemetry.io/contrib/otelconf.otlpHTTPMetricExporter
func otlpHTTPMetricExporter(ctx context.Context, otlpConfig *otelconf.OTLPHttpMetricExporter) (sdkmetric.Exporter, error)

//go:linkname otlpGRPCMetricExporter go.opentelemetry.io/contrib/otelconf.otlpGRPCMetricExporter
func otlpGRPCMetricExporter(ctx context.Context, otlpConfig *otelconf.OTLPGrpcMetricExporter) (sdkmetric.Exporter, error)

func hackPrometheusBridge(ctx context.Context, parsedConfig *otelconf.OpenTelemetryConfiguration) ([]sdkmetric.Option, error) {
	if parsedConfig.MeterProvider == nil || len(parsedConfig.MeterProvider.Readers) == 0 {
		return nil, nil
	}

	var opts []sdkmetric.Option
	promProducer := prometheus.NewMetricProducer(prometheus.WithGatherer(legacyregistry.DefaultGatherer))

	for _, reader := range parsedConfig.MeterProvider.Readers {
		if reader.Periodic == nil {
			continue
		}

		var exp sdkmetric.Exporter
		var err error
		exporterConfig := reader.Periodic.Exporter

		if exporterConfig.OTLPHttp != nil {
			exp, err = otlpHTTPMetricExporter(ctx, exporterConfig.OTLPHttp)
		} else if exporterConfig.OTLPGrpc != nil {
			exp, err = otlpGRPCMetricExporter(ctx, exporterConfig.OTLPGrpc)
		}

		if err != nil {
			return nil, fmt.Errorf("failed to initialize hacked exporter: %w", err)
		}

		if exp != nil {
			ropts := []sdkmetric.PeriodicReaderOption{
				sdkmetric.WithProducer(promProducer),
			}
			if reader.Periodic.Interval != nil {
				ropts = append(ropts, sdkmetric.WithInterval(time.Duration(*reader.Periodic.Interval)*time.Millisecond))
			}
			if reader.Periodic.Timeout != nil {
				ropts = append(ropts, sdkmetric.WithTimeout(time.Duration(*reader.Periodic.Timeout)*time.Millisecond))
			}
			opts = append(opts, sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exp, ropts...)))
		}
	}

	// Remove the original readers from the config so they aren't instantiated natively
	parsedConfig.MeterProvider.Readers = nil

	return opts, nil
}
