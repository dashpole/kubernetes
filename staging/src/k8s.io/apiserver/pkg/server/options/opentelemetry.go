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

package options

import (
	"context"
	"fmt"
	"net"

	"github.com/spf13/pflag"
	"go.opentelemetry.io/otel/exporters/otlp"
	"google.golang.org/grpc"

	"k8s.io/apiserver/pkg/opentelemetry"
	"k8s.io/apiserver/pkg/server/egressselector"
	"k8s.io/component-base/traces"
	"k8s.io/utils/path"
)

// OpenTelemetryOptions contain configuration options for opentelemetry
// exporters
type OpenTelemetryOptions struct {
	// ConfigFile is the file path with api-server opentelemetry configuration.
	ConfigFile string
}

// NewOpenTelemetryOptions creates a new instance of OpenTelemetryOptions
func NewOpenTelemetryOptions() *OpenTelemetryOptions {
	return &OpenTelemetryOptions{}
}

// AddFlags adds flags related to opentelemetry to the specified FlagSet
func (o *OpenTelemetryOptions) AddFlags(fs *pflag.FlagSet) {
	if o == nil {
		return
	}

	fs.StringVar(&o.ConfigFile, "opentelemetry-config-file", o.ConfigFile,
		"File with apiserver opentelemetry configuration.")
}

// Apply adds the opentelemetry settings to the global configuration.
func (o *OpenTelemetryOptions) Apply(es *egressselector.EgressSelector) error {
	if o == nil || o.ConfigFile == "" {
		return nil
	}

	npConfig, err := opentelemetry.ReadOpenTelemetryConfiguration(o.ConfigFile)
	if err != nil {
		return fmt.Errorf("failed to read opentelemetry config: %v", err)
	}

	opentelemetry.DefaultOpenTelemetryConfiguration(npConfig)
	errs := opentelemetry.ValidateOpenTelemetryConfiguration(npConfig)
	if len(errs) > 0 {
		return fmt.Errorf("failed to validate opentelemetry configuration: %v", errs.ToAggregate())
	}

	opts := []otlp.ExporterOption{}
	if npConfig.URL != nil {
		opts = append(opts, otlp.WithAddress(*npConfig.URL))

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
	}
	if npConfig.Service != nil {
		addr := fmt.Sprintf("%s.%s:%d", npConfig.Service.Name, npConfig.Service.Namespace, *npConfig.Service.Port)
		opts = append(opts, otlp.WithAddress(addr))

		if es != nil {
			// Only use the egressselector dialer if egressselector is enabled.
			// Note that if not using egress selectors, it will try and call
			// out to the service on the master.  For this to work, services
			// must be accessible from the master, and would require running
			// kube-proxy or a similar implementation of kubernetes services.

			// Service is on the "Cluster" newtork
			egressDialer, err := es.Lookup(egressselector.Cluster.AsNetworkContext())
			if err != nil {
				return err
			}

			otelDialer := func(ctx context.Context, addr string) (net.Conn, error) {
				return egressDialer(ctx, "tcp", addr)
			}
			opts = append(opts, otlp.WithGRPCDialOption(grpc.WithContextDialer(otelDialer)))
		}
	}

	traces.InitTraces("kube-apiserver", opts...)
	return nil
}

// Validate verifies flags passed to OpenTelemetryOptions.
func (o *OpenTelemetryOptions) Validate() (errs []error) {
	if o == nil || o.ConfigFile == "" {
		return
	}

	if exists, err := path.Exists(path.CheckFollowSymlink, o.ConfigFile); !exists || err != nil {
		errs = append(errs, fmt.Errorf("opentelemetry-config-file %s does not exist", o.ConfigFile))
	}
	return
}
