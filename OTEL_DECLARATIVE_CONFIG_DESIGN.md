# OpenTelemetry Declarative Configuration in Kubernetes

## 1. Objective
Support the new OpenTelemetry (OTel) declarative configuration ([opentelemetry-configuration](https://github.com/open-telemetry/opentelemetry-configuration)) in Kubernetes components to provide a unified way to configure tracing, metrics, and logs exporting via OTLP.

## 2. Background
Kubernetes components currently support distributed tracing via `TracingConfiguration` (`k8s.io/component-base/tracing/api/v1`), which allows basic configuration of tracing endpoints and sampling rates. Metrics are exposed via Prometheus format on specific HTTP endpoints, and logging is primarily handled by `klog`. 

The OpenTelemetry project has introduced a declarative configuration mechanism, with a Go implementation available at `github.com/open-telemetry/opentelemetry-go-contrib/tree/main/otelconf`. Using this, we can easily instantiate Tracer, Meter, and Logger providers that can export telemetry to OTLP endpoints.

## 3. Configuration & API Design

Everything must be opt-in. The existing `TracingConfiguration` must be respected if the declarative configuration is not used. 

**Alternative Configuration Approaches:**

* **Approach A: New `OpenTelemetryConfiguration` with File Path (Recommended)**
  Introduce a new API under `k8s.io/component-base` (e.g., `k8s.io/component-base/telemetry/api/v1alpha1`). This structure would contain a path to the OpenTelemetry declarative configuration file.
  ```go
  type OpenTelemetryConfiguration struct {
      // ConfigPath is the path to the OpenTelemetry declarative configuration file.
      // +optional
      ConfigPath *string
  }
  ```
  *Pros:* 
  - Cleaner, separates all new OTel functionality from legacy configurations.
  - Avoids embedding massive, noisy YAML structures inside the component's primary config file.
  - Aligns with Kubernetes conventions for large external configs (e.g., Audit Policies, Credential Providers are all configured via file paths).
  - Validation failures happen cleanly (e.g., if the file doesn't exist, we error fast instead of relying on deferred `RawExtension` parsing).
  *Cons:* 
  - Requires administrators to mount and manage a separate configuration file.

* **Approach B: Embedding the OpenTelemetry Configuration Inline**
  Instead of pointing to a file, embed the entire OpenTelemetry configuration format directly into the component's configuration API. 
  ```go
  import "github.com/open-telemetry/opentelemetry-go-contrib/otelconf/v1alpha1" // pseudo-import

  type OpenTelemetryConfiguration struct {
      // Config holds the embedded OpenTelemetry configuration.
      // +optional
      Config *v1alpha1.OpenTelemetryConfiguration `json:"config,omitempty"`
  }
  ```
  *Pros:*
  - Simpler deployment for users, as the entire configuration is contained within the single component config (e.g., `KubeletConfiguration`). No need to manage a secondary file.
  *Cons:*
  - **API Compatibility & Lifecycle (Critical):** Kubernetes APIs are strictly versioned with rigid backwards-compatibility guarantees (e.g., no removing fields or changing defaults in GA APIs). Importing an external struct tightly couples K8s API stability to upstream OTel stability. If OTel introduces breaking changes or drops fields between OTel versions, it would force a breaking change on Kubernetes' API, violating K8s API policy.
  - **Code Generation Dependencies:** Kubernetes APIs require `DeepCopy`, `Conversion`, and `Defaulter` generation. Upstream OTel structs lack the Kubernetes-specific `+k8s:deepcopy-gen` comment tags. *Even if upstream maintainers were willing to add these tags*, relying on a non-Kubernetes repository to strictly follow K8s API review guidelines, defaulting semantics, and patch strategies is highly unlikely and strongly discouraged by Kubernetes API reviewers. It typically forces developers to manually duplicate the entire schema in the Kubernetes codebase anyway to insulate K8s from upstream churn.
* **Approach C: Inline Configuration via `runtime.RawExtension`**
  Embed the OpenTelemetry configuration directly as an opaque blob rather than a typed Go struct. We can use `k8s.io/apimachinery/pkg/runtime.RawExtension` (which is standard in K8s for embedding external/runtime-defined schemas) or a simple `string` containing the YAML.
  ```go
  import "k8s.io/apimachinery/pkg/runtime"

  type OpenTelemetryConfiguration struct {
      // Config holds the embedded OpenTelemetry configuration as an opaque API blob.
      // +optional
      Config runtime.RawExtension `json:"config,omitempty"`
  }
  ```
  *Pros:*
  - Preserves the deployment simplicity of Approach B (one single component config file, no extra mounts).
  - Completely sidesteps the API compatibility and code generation hurdles of Approach B. Because Kubernetes treats the embedded content as an opaque blob of bytes, it doesn't need to track upstream OpenTelemetry schema changes or generate deep-copy methods for them. The bytes are simply passed down to `otelconf` for parsing at runtime.
  *Cons:*
  - **Deferred Validation:** Because Kubernetes only sees a blind byte slice, strict K8s API validation toolchains cannot natively validate the internal contents of the OpenTelemetry config. If a user provides invalid OpenTelemetry YAML, it will not be caught during API serialization, but only later when the component starts and tries to initialize tracing.

* **Approach D: Tacking onto existing `TracingConfiguration`**
  Add a `ConfigPath` or inline `Config` field to the existing `TracingConfiguration`.
  *Pros:* No new high-level fields in component configs.
  *Cons:* The name `TracingConfiguration` becomes a misnomer since it would handle metrics and logs as well. This might also violate user expectations.

If both the new `OpenTelemetryConfiguration` and the legacy `TracingConfiguration` are provided, we should either prioritize the declarative config or error on validation.

**Handling `TracingConfiguration` Deprecation**
Since `TracingConfiguration` is already a `v1` API, it is subject to the Kubernetes API deprecation policy. It cannot be immediately removed. We should mark `TracingConfiguration` as deprecated and plan for its removal over several releases, pointing users to the new declarative configuration approach.

## 4. Signal Integration Design

### 4.1 Tracing
**Current implementation:** `k8s.io/component-base/tracing` manually initializes OTLP exporters and sets up `trace.TracerProvider`.
**New approach:** When a declarative configuration file is provided, use `otelconf.New()` to parse the file and initialize the `TracerProvider`. If omitted, fallback to the legacy behavior to ensure backward compatibility.

### 4.2 Metrics
**Requirement:** Export metrics using OTLP while keeping the existing Prometheus endpoint available.
**Current implementation:** Components register metrics with `k8s.io/component-base/metrics` (which wraps Prometheus), and expose them via a `/metrics` HTTP endpoint.
**New approach:** 
We can use the `bridges/prometheus` module from `opentelemetry-go-contrib`. Specifically, the bridge allows Prometheus metrics to be read by the OpenTelemetry SDK. We can create a Prometheus metric producer that wraps the existing Kubernetes metrics registry (the Prometheus gatherer).
When `otelconf` initializes the `MeterProvider`, it can be configured to periodically gather metrics from the existing Prometheus registry and export them via any OTLP exporters defined in the declarative configuration. This allows the existing HTTP `/metrics` endpoint to function normally while also pushing metrics using OTLP.

### 4.3 Logs
**Requirement:** Export logs using OTLP. Needs exploration regarding existing log outputs (stdout/files).
**Current implementation:** Kubernetes uses `klog`.
**New approach:** 
We can bridge `klog` to OpenTelemetry using one of the existing bridges (e.g., `logr` or `slog`, since `klog` integrates heavily with `logr`). When initializing the SDK using `otelconf`, we acquire a `LoggerProvider` and attach it to our logging pipeline.

**Alternatives for Log Destination:**
* **Alternative 1: Dual Output (Recommended initially)** 
  Configure the log routing so that logs continue being emitted to stdout/files exactly as they are today, but *also* get mirrored to the OpenTelemetry `LoggerProvider`. 
  *Pros:* Completely backward-compatible. Users relying on log scraping (e.g., Fluentd, Promtail) will not experience breakage.
  *Cons:* Duplicated log processing overhead.
* **Alternative 2: Exclusive OTLP Output**
  When OTLP logging is configured, suppress standard output logging to save resources.
  *Pros:* Saves CPU and memory, avoids duplicate log data.
  *Cons:* Highly disruptive. Any system tools or administrators examining systemd or container logs will find them empty.

**Recommendation for Logs:** Start with Dual Output (Alternative 1) as the default when OTel logs are configured, to maintain system observability. We could provide an additional flag or field (e.g., `DisableStandardOutput` inside `TelemetryConfiguration`) to optionally allow administrators to turn off standard logging if they rely exclusively on OTLP.
