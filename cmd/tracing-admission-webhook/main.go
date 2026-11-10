// Copyright 2026 The Kubernetes Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"k8s.io/klog/v2"
	"sigs.k8s.io/tracing-admission-webhook/pkg/webhook"
)

func main() {
	var (
		certFile          string
		keyFile           string
		port              int
		generateIfMissing bool
		samplingRate      float64
		requireSampled    bool
	)

	klog.InitFlags(nil)
	flag.StringVar(&certFile, "tls-cert-file", "/etc/webhook/certs/tls.crt", "Path to the TLS certificate file.")
	flag.StringVar(&keyFile, "tls-key-file", "/etc/webhook/certs/tls.key", "Path to the TLS private key file.")
	flag.IntVar(&port, "port", 8443, "HTTPS port for the mutating admission webhook server.")
	flag.BoolVar(&generateIfMissing, "generate-if-missing", false, "Generate a fresh W3C traceparent when incoming HTTP headers lack one.")
	flag.Float64Var(&samplingRate, "sampling-rate", 1.0, "Sampling rate [0.0, 1.0] when --generate-if-missing is enabled.")
	flag.BoolVar(&requireSampled, "require-sampled", false, "Only inject incoming traceparent headers when the W3C sampled flag (01) is set.")
	flag.Parse()

	if samplingRate < 0.0 || samplingRate > 1.0 {
		klog.ErrorS(errors.New("invalid sampling-rate"), "--sampling-rate must be between 0.0 and 1.0", "samplingRate", samplingRate)
		os.Exit(1)
	}

	if _, err := tls.LoadX509KeyPair(certFile, keyFile); err != nil {
		klog.ErrorS(err, "Failed to load initial TLS key pair", "certFile", certFile, "keyFile", keyFile)
		os.Exit(1)
	}

	mutator := webhook.NewMutator(webhook.Config{
		GenerateIfMissing: generateIfMissing,
		SamplingRate:      samplingRate,
		RequireSampled:    requireSampled,
	})
	handler := webhook.NewHandler(mutator)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		TLSConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
			GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
				cert, err := tls.LoadX509KeyPair(certFile, keyFile)
				if err != nil {
					return nil, err
				}
				return &cert, nil
			},
		},
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		klog.InfoS("Starting tracing mutating admission webhook",
			"port", port,
			"generateIfMissing", generateIfMissing,
			"samplingRate", samplingRate,
			"requireSampled", requireSampled,
		)
		if err := srv.ListenAndServeTLS("", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		klog.InfoS("Received shutdown signal; draining webhook server")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			klog.ErrorS(err, "Graceful shutdown failed")
			os.Exit(1)
		}
	case err := <-errCh:
		if err != nil {
			klog.ErrorS(err, "Webhook server exited with error")
			os.Exit(1)
		}
	}
}
