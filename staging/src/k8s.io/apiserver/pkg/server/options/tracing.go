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

package options

import (
	"context"
	"fmt"
	"net"

	"github.com/spf13/pflag"
	"go.opentelemetry.io/otel/exporters/otlp"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"google.golang.org/grpc"

	"k8s.io/apiserver/pkg/server"
	"k8s.io/apiserver/pkg/server/egressselector"
	"k8s.io/apiserver/pkg/tracing"
	"k8s.io/component-base/traces"
	"k8s.io/utils/path"
)

const apiserverService = "kube-apiserver"

// TracingOptions contain configuration options for tracing
// exporters
type TracingOptions struct {
	// ConfigFile is the file path with api-server tracing configuration.
	ConfigFile string
}

// NewTracingOptions creates a new instance of TracingOptions
func NewTracingOptions() *TracingOptions {
	return &TracingOptions{}
}

// AddFlags adds flags related to tracing to the specified FlagSet
func (o *TracingOptions) AddFlags(fs *pflag.FlagSet) {
	if o == nil {
		return
	}

	fs.StringVar(&o.ConfigFile, "tracing-config-file", o.ConfigFile,
		"File with apiserver tracing configuration.")
}

// ApplyTo adds the tracing settings to the global configuration.
func (o *TracingOptions) ApplyTo(es *egressselector.EgressSelector, c *server.Config) error {
	if o == nil || o.ConfigFile == "" {
		return nil
	}

	npConfig, err := tracing.ReadTracingConfiguration(o.ConfigFile)
	if err != nil {
		return fmt.Errorf("failed to read tracing config: %v", err)
	}

	errs := tracing.ValidateTracingConfiguration(npConfig)
	if len(errs) > 0 {
		return fmt.Errorf("failed to validate tracing configuration: %v", errs.ToAggregate())
	}

	if npConfig.URL == nil {
		return fmt.Errorf("URL was nil, but must be non-nil")
	}

	opts := []otlp.ExporterOption{
		otlp.WithAddress(*npConfig.URL),
	}
	if es != nil {
		// Only use the egressselector dialer if egressselector is enabled.
		// URL is on the "ControlPlane" network
		egressDialer, err := es.Lookup(egressselector.ControlPlane.AsNetworkContext())
		if err != nil {
			return err
		}

		otelDialer := func(ctx context.Context, addr string) (net.Conn, error) {
			return egressDialer(ctx, "tcp", addr)
		}
		opts = append(opts, otlp.WithGRPCDialOption(grpc.WithContextDialer(otelDialer)))
	}

	sampler := sdktrace.NeverSample()
	if npConfig.SamplingRatePerMillion != nil && *npConfig.SamplingRatePerMillion > 0 {
		sampler = sdktrace.TraceIDRatioBased(float64(*npConfig.SamplingRatePerMillion) / float64(1000000))
	}

	tp := traces.NewProvider(context.Background(), apiserverService, sampler, opts...)
	c.TracerProvider = &tp
	return nil
}

// Validate verifies flags passed to TracingOptions.
func (o *TracingOptions) Validate() (errs []error) {
	if o == nil || o.ConfigFile == "" {
		return
	}

	if exists, err := path.Exists(path.CheckFollowSymlink, o.ConfigFile); !exists || err != nil {
		errs = append(errs, fmt.Errorf("tracing-config-file %s does not exist", o.ConfigFile))
	}
	return
}
