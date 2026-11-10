# Contributing to Tracing Admission Webhook

We welcome contributions to `tracing-admission-webhook`, a subproject of [Kubernetes SIG Instrumentation](https://github.com/kubernetes/community/tree/master/sig-instrumentation).

## Before You Start

- Sign the [CNCF Contributor License Agreement (CLA)](https://git.k8s.io/community/CLA.md).
- Read the [Kubernetes Contributor Guide](https://git.k8s.io/community/contributors/guide/README.md).

## Development Workflow

1. Run unit and race tests:
   ```bash
   make test
   ```
2. Run static analysis and Helm chart lint checks:
   ```bash
   make verify
   ```
3. Build the webhook binary:
   ```bash
   make build
   ```
