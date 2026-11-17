# Kubernetes OpenMetrics 2.0-rc End-to-End Validation Experiment

This directory contains the reproducible configs, verification script, and captured primary evidence for testing OpenMetrics 2.0-rc (`application/openmetrics-text; version=2.0.0-rc`) exposition from Kubernetes control-plane and node components against Prometheus `HEAD` (`--enable-feature=openmetrics2`) and `kubernetes-sigs/kubernetes-mixin`.

## Summary

End-to-end validation of OpenMetrics 2.0-rc exposition from Kubernetes components succeeded with zero missing series, zero labelset or label-value mismatches across all `50,925` series (`2,176` `(job, __name__)` pairs), zero `# TYPE` or `# HELP` metadata diffs, and zero `kubernetes-mixin` rule, alert, or dashboard query errors.

The primary upstream blocker before opening a Kubernetes PR is that `prometheus/client_golang` `main` (`c86c2471ded9`, containing `prometheus/client_golang#2132` and `#2177`) has not been tagged into a release newer than `v1.25.0` (which `kubernetes/kubernetes` currently vendors). Pinning `main` resolves to `v1.24.2-0.20261113202007-c86c2471ded9`, which is semver-lower than `v1.25.0`.

## Experiment Setup & Reproduction

- **Kubernetes**: `kubernetes/kubernetes` `master` (`d9489cf81cb`) patched in the parent commit (`34c6419d6505a425974f484e08bd33fb5b99fc4e`) with `github.com/prometheus/client_golang` pinned to `c86c2471ded9` (`hack/pin-dependency.sh` + `hack/update-vendor.sh`), `EnableOpenMetrics: true` in `staging/src/k8s.io/component-base/metrics/legacyregistry/registry.go` (`Handler()`) and `staging/src/k8s.io/component-base/metrics/http.go` (`HandlerOpts.toPromhttpHandlerOpts()`), and OpenMetrics negotiation unit tests in `http_test.go` and `legacyregistry/registry_test.go`.
- **Custom `kind` node image**: `kindest/node:om2-test` built from source (`kind build node-image --type=source --image=kindest/node:om2-test`) using `kind` `v0.34.0-alpha` (`9fde029c`), because `kind v0.31.0` emits `kubeadm.k8s.io/v1beta3` which `kubeadm` v1.38 rejects.
- **`kubernetes-mixin`**: `kubernetes-sigs/kubernetes-mixin` `master` (`2002557`) deployed via `make dev` (`lgtm`, `otel-collector-contrib v0.163.2`, `kube-state-metrics`, `prometheus-node-exporter`). See [kubernetes-mixin-lgtm.patch](kubernetes-mixin-lgtm.patch) for the `kindest/node:om2-test` and `kubeadm.k8s.io/v1beta4` `extraArgs` adjustments.
- **Prometheus**: built from `prometheus/prometheus` `main` (`f52b059137e73cc05ebfc7024d3b46beee657b15`), running 5 parallel instances scraping all 12 jobs (`kube-apiserver`, `kube-apiserver-slis`, `kubelet`, `cadvisor`, `kubelet-resource`, `kubelet-probes`, `kube-scheduler`, `kube-scheduler-resources`, `kube-controller-manager`, `kube-proxy`, `kube-state-metrics`, `node-exporter`) and evaluating all `kubernetes-mixin` recording and alerting rules:
  1. `004` (`prometheus-configs/prom-004.yml`): `scrape_protocols: [PrometheusText0.0.4]`
  2. `om1` (`prometheus-configs/prom-om1.yml`): `scrape_protocols: [OpenMetricsText1.0.0, PrometheusText0.0.4]`
  3. `om2` (`prometheus-configs/prom-om2.yml`): `scrape_protocols: [OpenMetricsText2.0.0-rc, OpenMetricsText1.0.0, PrometheusText0.0.4]` with `--enable-feature=openmetrics2`
  4. `om2-nh` (`prometheus-configs/prom-om2-nh.yml`): `om2` plus `scrape_native_histograms: true` and `always_scrape_classic_histograms: true`
  5. `om2-nh-default` (`prometheus-configs/prom-om2-nh-default.yml`): `om2` plus `scrape_native_histograms: true` only (default `always_scrape_classic_histograms: false`)
- **Re-running verification**: With the `kind-kubernetes-mixin` cluster running and Prometheus `HEAD` built, run `python3 om2-experiment/verify_om2.py --mixin-dir <path-to-kubernetes-mixin> --prom-bin <path-to-prometheus>`. The script automatically extracts `ca.crt`, `client.crt`, `client.key`, and the apiserver address from `kubectl --context kind-kubernetes-mixin config view`, renders the templates from `prometheus-configs/`, port-forwards `kube-proxy`, launches all 5 Prometheus instances, waits for all 12 targets and 26 rule groups (including the `3m` `kube-apiserver-availability.rules` group), and writes all evidence files.

## Evidence Files

- [verify_om2.py](verify_om2.py): Reproducible verification harness used to capture raw endpoints, run all 5 Prometheus configurations against the live `kind` cluster, check `otel-collector-contrib`, and generate the JSON evidence reports.
- [results/prometheus_runtime_info.json](results/prometheus_runtime_info.json): Prometheus `--version` output, `/api/v1/status/flags` (`enable-feature`), and `/api/v1/status/config` (`scrape_protocols`, native histogram flags) from all 5 running instances.
- [results/endpoint_negotiation.json](results/endpoint_negotiation.json): Exact HTTP status, `Content-Type` header, byte/line counts, `# EOF` checks, `# TYPE`, `# UNIT`, `st@`, and composite native histogram line counts for all 12 endpoints across `prom004`, `om100`, and `om2rc` (raw captures in `results/raw/`).
- [results/targets_summary.json](results/targets_summary.json): `/api/v1/targets` health across all 5 Prometheus instances.
- [results/series_and_labels_parity.json](results/series_and_labels_parity.json): Full per-job series counts, `(job, __name__)` parity, exact label-name and label-value parity (`le` buckets and `quantile` labels), and native histogram series counts.
- [results/metadata_diff.json](results/metadata_diff.json): `/api/v1/metadata` diff (`type`, `help`, `unit`, family counts) and raw `# HELP` diff across `004`, `om1`, and `om2`.
- [results/rules_and_alerts_parity.json](results/rules_and_alerts_parity.json): Rule group health, active alerts, and recording rule result counts across all 5 Prometheus modes.
- [results/dashboards_query_parity.json](results/dashboards_query_parity.json): PromQL evaluation results for all `389` unique queries from `kubernetes-mixin` `dashboards_out/*.json` across all 5 Prometheus modes.
- [results/otel_collector_check.json](results/otel_collector_check.json): In-cluster `otel-collector-contrib v0.163.2` log error scan and receiver/exporter point counters.

## Key Measurements & Findings

### 1. Content Negotiation & Payload Size (`results/endpoint_negotiation.json`)

All 10 modified Kubernetes endpoints negotiated `application/openmetrics-text; version=2.0.0-rc; charset=utf-8; escaping=allow-utf-8` (HTTP 200, valid `# EOF` trailer), while unmodified external exporters (`kube-state-metrics`, `prometheus-node-exporter`) negotiated `text/plain; version=0.0.4`. Note that Kubernetes handlers leave `HandlerOpts.Registry` nil, so `promhttp_metric_handler_errors_total` is not exposed by Kubernetes endpoints; zero scrape/encode failures were established via HTTP 200, valid `# EOF`, and Prometheus `health == "up"` across all 12 targets.

| Endpoint | 0.0.4 Bytes / Lines | OM2-rc Bytes / Lines | Size Ratio | `# UNIT` | `st@` Lines | Composite Native Hist Lines |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| `kube-apiserver /metrics` | 4,734,210 / 34,879 | 1,089,948 / 5,445 | 4.34x smaller | 108 | 3,498 | 1,408 |
| `kube-controller-manager /metrics` | 432,307 / 4,442 | 200,779 / 1,676 | 2.15x smaller | 108 | 457 | 71 |
| `kube-scheduler /metrics` | 364,547 / 3,221 | 170,333 / 1,233 | 2.14x smaller | 108 | 288 | 95 |
| `kubelet /metrics` | 256,338 / 2,635 | 158,557 / 1,332 | 1.62x smaller | 108 | 270 | 91 |
| `kube-proxy /metrics` | 107,529 / 1,158 | 93,075 / 916 | 1.16x smaller | 108 | 97 | 13 |
| `kubelet /metrics/probes` | 57,457 / 345 | 17,000 / 60 | 3.38x smaller | 0 | 49 | 22 |
| `kubelet /metrics/cadvisor` | 1,189,730 / 3,391 | 1,196,379 / 3,392 | ~1.0x | 0 | 0 | 0 |
| `kube-apiserver /metrics/slis` | 24,270 / 237 | 27,672 / 238 | ~1.0x | 0 | 123 | 0 |
| `kubelet /metrics/resource` | 16,012 / 135 | 16,175 / 136 | ~1.0x | 0 | 0 | 0 |
| `kube-scheduler /metrics/resources` | 3,740 / 19 | 3,746 / 20 | ~1.0x | 0 | 0 | 0 |

### 2. Series, Label Values, & Metadata Parity (`results/series_and_labels_parity.json`, `results/metadata_diff.json`)

- **100% series and label-value parity**: Across `004`, `om1`, and `om2`, all 12 jobs scraped `50,925` total series across `2,176` `(job, __name__)` pairs (`715` Prometheus recording rule output series had empty `job`, bringing non-rule scraped series across the 12 jobs to `50,210`). Comparing `004` vs `om2` produced `0` missing/extra `(job, __name__)` pairs, `0` label-name diffs, and `0` label-value diffs (`labelvalue_diffs_004_vs_om2_count: 0`), including all `332` distinct histogram `le` bucket boundaries and all `7` summary `quantile` values (`0.0`, `0.25`, `0.5`, `0.75`, `0.9`, `0.99`, `1.0`).
- **Metadata preservation vs OM1**: Both `004` and `om2` produced `1,004` identical metadata family entries (`0` missing, `0` extra, `0` `# TYPE` diffs, `0` `/api/v1/metadata` `help` diffs, and `0` raw `# HELP` diffs across all 12 endpoints). Under `om1`, `client_golang` downgraded the 9 Kubernetes counters registered without a `_total` suffix to `# TYPE ... unknown` in raw text (`authenticated_user_requests`, `authentication_attempts`, `container_memory_failcnt`, `endpoint_slice_controller_changes`, `endpoint_slice_controller_syncs`, `kubelet_certificate_manager_client_expiration_renew_errors`, `kubelet_evented_pleg_connection_error_count`, `kubelet_evented_pleg_connection_success_count`, `kubelet_pleg_discard_events`; in `/api/v1/metadata`, `endpoint_slice_controller_changes` showed `counter` because `endpoint_slice_controller_changes_total` stripped `_total` onto the same key, leaving the other `8` as `unknown`). Stripping `_total` on `241` k8s-only counters, colliding `endpoint_slice_controller_changes_total` (`-1`), and splitting `5` shared counters between `om1` k8s endpoints and `0.0.4` external exporters (`go_memstats_frees`, `go_memstats_mallocs`, `process_cpu_seconds`, `process_network_receive_bytes`, `process_network_transmit_bytes`) inflated `om1` `/api/v1/metadata` to `1,008` families. OM2-rc preserves `# TYPE ... counter` and exact counter names (`_total` untouched).
- **`# UNIT` & `st@`**: Five endpoints (`kube-apiserver`, `kube-controller-manager`, `kube-scheduler`, `kubelet`, `kube-proxy`) emitted `108` `# UNIT` entries on `go_*` metrics (`68` counters, `32` gauges, `8` histograms), exposing `# UNIT` metadata absent in `0.0.4`. Six endpoints emitted inline `st@` start timestamps.

### 3. `kubernetes-mixin` Rules, Alerts, Dashboards, & Native Histograms

- **Rules & alerts (`results/rules_and_alerts_parity.json`)**: All `26` rule groups (`16` recording groups / `72` rules / `43` unique `record:` names, plus `10` alerting groups / `65` rules / `57` unique `alert:` names) evaluated with `health == "ok"` across all 5 modes. `004`, `om1`, `om2`, and `om2-nh` all populated `39/43` recording rules identically (`4` remained empty for unexercised error/drop paths) and evaluated `0` firing/pending alerts identically across all 5 modes.
- **Dashboard queries (`results/dashboards_query_parity.json`)**: After normalizing Grafana template variables (`$cluster`, `$instance`, `$namespace`, `$node`, `$pod`, `$type`, `$volume`, `$workload`, `$__rate_interval`, `$__interval`, `$__range`, `$resolution`), all `389` unique PromQL expressions extracted from `dashboards_out/*.json` succeeded with `0` errors and identical non-empty coverage (`305/389`) across `004`, `om1`, `om2`, and `om2-nh` (`0` parity diffs). The remaining `84` queries were empty identically in both `004` and `om2` because the idle single-node `kind` cluster had no Windows exporter nodes, PVCs/volume stats, or non-empty error/read/write `apiserver_request:availability30d` series.
- **OTel Collector compatibility (`results/otel_collector_check.json`)**: In-cluster `otelcol-contrib v0.163.2` logged zero errors and accepted/sent `290,764` metric points (`0` refused) from its configured targets (`cadvisor`, `kubelet`, `kube-scheduler`, `kube-scheduler-resources`, `kube-state-metrics`, `node-exporter`) via `Accept` fallback.
- **Native histograms pitfall (`om2-nh` vs `om2-nh-default`)**: With `scrape_native_histograms: true` and `always_scrape_classic_histograms: true` (`om2-nh`), total series increased from `50,925` to `52,625` (adding native histograms across `kube-apiserver`, `kube-controller-manager`, `kube-proxy`, `kube-scheduler`, `kubelet`, and `kubelet-probes`) while keeping all `39/43` recording rules and `305/389` dashboard queries populated. When `always_scrape_classic_histograms` was left at default `false` (`om2-nh-default`), Prometheus dropped classic `_bucket` series (`18,517` total series remaining), causing `17` populated recording rules (including all `7` `apiserver_request:burnrate*` SLO rules and all `4` `cluster_quantile:*:histogram_quantile` rules) and `26` dashboard queries (`279/389` remaining) to drop to `0` series.

### 4. Unit Tests in `kubernetes/kubernetes`

Added `TestHandlerOpenMetricsNegotiation` in `staging/src/k8s.io/component-base/metrics/http_test.go` and `legacyregistry/registry_test.go` (12 subtests covering `HandlerFor`, `HandlerWithReset`, and `legacyregistry.Handler` across `2.0.0-rc`, `1.0.0`, and `0.0.4`). `go test` passed on `k8s.io/component-base/metrics/...`, `k8s.io/apiserver/pkg/server/routes/...`, `cmd/kube-proxy/app/...`, `pkg/scheduler/metrics/...`, `pkg/kubelet/metrics/...`, and `pkg/kubelet/server/...`.

## Compatibility Gaps & Prerequisites Before Upstream Kubernetes PR

1. **Tagged `prometheus/client_golang` release `> v1.25.0`**: Kubernetes vendors `v1.25.0`. `client_golang` `main` (`c86c2471ded9`) currently resolves semver-lower (`v1.24.2-0.20261113202007-c86c2471ded9`); `hack/tools/instrumentation/go.mod` also pins `v1.25.0`.
2. **OpenMetrics `2.0.0` (final) vs `2.0.0-rc`**: `client_golang` currently negotiates `version=2.0.0-rc`; Kubernetes PR timing determines whether to target final `2.0.0` or experimental `2.0.0-rc`.
3. **`component-base/metrics.HandlerOpts` opt-out semantics**: `HandlerOpts` aliases `promhttp.HandlerOpts` (`bool` `EnableOpenMetrics`), where `false` cannot distinguish "unset (use default)" from "explicitly disabled". Unconditionally enabling it in `toPromhttpHandlerOpts()` and `legacyregistry.Handler()` prevents opting out without adding a `DisableOpenMetrics` option or feature gate.
4. **Stricter OM2 encoder validation & kubelet `ContinueOnError`**: `expfmt.MetricFamilyToOpenMetrics20` rejects negative counter/histogram/summary values and invalid UTF-8. While `kubelet` uses `ContinueOnError` (with nil `Registry` and `ErrorLog`), `legacyregistry.Handler()` uses `HTTPErrorOnError`; broader e2e runs should confirm no custom collectors trigger encoder rejections.
5. **PromQL classic bucket retention with native histograms**: Operators enabling both `--enable-feature=openmetrics2` and `scrape_native_histograms: true` must also set `always_scrape_classic_histograms: true` to keep `kubernetes-mixin` classic `_bucket` recording rules and dashboards working.
