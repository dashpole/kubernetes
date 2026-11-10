# Tracing Admission Webhook (`sigs.k8s.io/tracing-admission-webhook`)

`tracing-admission-webhook` is an out-of-tree Kubernetes Mutating Admission Webhook maintained under **Kubernetes SIG Instrumentation**. It propagates W3C Trace Context (`traceparent` and `tracestate`) across asynchronous Kubernetes controller boundaries by atomically injecting `metadata.annotations["tracing.k8s.io/traceparent"]` when resources are created or their `.spec` is mutated.

## Overview & Architecture

In Kubernetes' declarative, level-triggered control plane, synchronous HTTP requests to `kube-apiserver` (KEP-647) terminate as soon as resource state is persisted to `etcd`. Downstream controllers (`kube-controller-manager` and `sigs.k8s.io/controller-runtime` operators) observe state asynchronously via watches and deduplicating workqueues.

`tracing-admission-webhook` bridges the synchronous API mutation plane and asynchronous controller reconciliation loops while upholding three core safety invariants:

1. **The Spec-Only Invariant**:
   - The webhook injects or updates `metadata.annotations["tracing.k8s.io/traceparent"]` (and `metadata.annotations["tracing.k8s.io/tracestate"]` when present) **strictly when an object is created (`CREATE`) or when `.spec` is semantically modified (`UPDATE`)**.
   - On `UPDATE`, the webhook compares `request.object.spec` against `request.oldObject.spec`. Metadata-only updates (labels, annotations, finalizers) and status updates leave existing trace annotations untouched.
   - **Subresource Updates Ignored**: All requests to `/status` (and any other subresource such as `/scale` or `/finalize`) are bypassed immediately with zero mutations, preventing reconciliation feedback loops and watch storms.
2. **Atomic Spec Bundling**:
   - Because the `tracing.k8s.io/traceparent` annotation patch is bundled into the exact same atomic admission transaction as the `.spec` write, it introduces zero extra `etcd` MVCC revisions and no spurious `metadata.generation` increments.
   - For workload controllers (`Deployment`, `ReplicaSet`, `StatefulSet`, `DaemonSet`, `Job`, `CronJob`, `ReplicationController`), the webhook also stamps `.spec.template.metadata.annotations["tracing.k8s.io/traceparent"]` so child `ReplicaSet`, `Job`, and `Pod` objects inherit causal trace context automatically.
3. **Fail-Open Reliability (`failurePolicy: Ignore`)**:
   - Both the static manifests and Helm chart configure `failurePolicy: Ignore` and a 5-second timeout so control-plane writes never block if the webhook is unavailable.

## OpenTelemetry `ObjectCarrier`

Package `sigs.k8s.io/tracing-admission-webhook/pkg/webhook` also provides `ObjectCarrier`, an implementation of OpenTelemetry's `propagation.TextMapCarrier` over `metav1.Object` mapping standard W3C keys (`traceparent`, `tracestate`) to Kubernetes-prefixed annotations (`tracing.k8s.io/traceparent`, `tracing.k8s.io/tracestate`):

```go
carrier := webhook.NewObjectCarrier(obj)
ctx = otel.GetTextMapPropagator().Extract(ctx, carrier)
triggerSpanCtx := trace.SpanContextFromContext(ctx)
```

## CLI Flags

| Flag | Default | Description |
| :--- | :--- | :--- |
| `--tls-cert-file` | `/etc/webhook/certs/tls.crt` | Path to the serving TLS certificate file (reloaded dynamically on rotation). |
| `--tls-key-file` | `/etc/webhook/certs/tls.key` | Path to the serving TLS private key file. |
| `--port` | `8443` | HTTPS port for the mutating admission webhook server. |
| `--generate-if-missing` | `false` | Generate a fresh W3C `traceparent` when incoming HTTP request headers lack one. |
| `--sampling-rate` | `1.0` | Probability `[0.0, 1.0]` of generating a sampled `traceparent` when `--generate-if-missing` is enabled. |
| `--require-sampled` | `false` | Only inject incoming `traceparent` headers when the W3C sampled flag (`01`) is set. |

## Installation

### Prerequisites

- Kubernetes `v1.26+` with `APIServerTracing` enabled on `kube-apiserver`
- [cert-manager](https://cert-manager.io/) installed in the cluster for webhook TLS certificate provisioning and CA bundle injection

### Option 1: Helm Chart

```bash
helm install tracing-admission-webhook ./deploy/charts/tracing-admission-webhook \
  --namespace tracing-system \
  --create-namespace
```

### Option 2: Static Manifests (with `cert-manager`)

```bash
kubectl apply -f deploy/manifests/webhook.yaml
```

## Development

```bash
# Run go vet, unit tests (with -race), and helm lint:
make verify

# Build the webhook binary:
make build
```

## Community, Discussion, Contribution, and Support

This project is governed by [Kubernetes SIG Instrumentation](https://github.com/kubernetes/community/tree/master/sig-instrumentation).
See [CONTRIBUTING.md](CONTRIBUTING.md) and [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md) for participation guidelines.
