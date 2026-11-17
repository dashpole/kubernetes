#!/usr/bin/env python3
"""
End-to-end verification harness for Kubernetes OpenMetrics 2.0-rc exposition.

Starts temporary kubectl port-forward (kube-proxy) and 5 Prometheus HEAD instances:
  - prom-004 (9091): PrometheusText0.0.4
  - prom-om1 (9092): OpenMetricsText1.0.0 fallback 0.0.4
  - prom-om2 (9093): OpenMetricsText2.0.0-rc (--enable-feature=openmetrics2)
  - prom-om2-nh (9094): om2 + scrape_native_histograms: true + always_scrape_classic_histograms: true
  - prom-om2-nh-default (9095): om2 + scrape_native_histograms: true (default classic=false)

Collects raw endpoint headers/payloads across all 10 modified k8s endpoints + 2 external mixin jobs,
compares series, exact label-value sets (including counter _total, histogram le, summary quantile),
/api/v1/metadata, /api/v1/rules, /api/v1/alerts, native histograms, and kubernetes-mixin dashboard PromQL.
"""

import argparse
import glob
import json
import os
import re
import shutil
import signal
import subprocess
import sys
import time
import urllib.parse
import urllib.request
from collections import Counter, defaultdict
import yaml

ROOT_ENDPOINTS = [
    ("apiserver_metrics", "apiserver", "/metrics"),
    ("apiserver_slis", "apiserver", "/metrics/slis"),
    ("kubelet_metrics", "apiserver", "/api/v1/nodes/kubernetes-mixin-control-plane/proxy/metrics"),
    ("kubelet_cadvisor", "apiserver", "/api/v1/nodes/kubernetes-mixin-control-plane/proxy/metrics/cadvisor"),
    ("kubelet_resource", "apiserver", "/api/v1/nodes/kubernetes-mixin-control-plane/proxy/metrics/resource"),
    ("kubelet_probes", "apiserver", "/api/v1/nodes/kubernetes-mixin-control-plane/proxy/metrics/probes"),
    ("scheduler_metrics", "apiserver", "/api/v1/namespaces/kube-system/pods/https:kube-scheduler-kubernetes-mixin-control-plane:10259/proxy/metrics"),
    ("scheduler_resources", "apiserver", "/api/v1/namespaces/kube-system/pods/https:kube-scheduler-kubernetes-mixin-control-plane:10259/proxy/metrics/resources"),
    ("controller_manager_metrics", "apiserver", "/api/v1/namespaces/kube-system/pods/https:kube-controller-manager-kubernetes-mixin-control-plane:10257/proxy/metrics"),
    ("kube_proxy_metrics", "kube-proxy", "/metrics"),
    ("kube_state_metrics", "apiserver", "/api/v1/namespaces/default/services/kube-state-metrics:8080/proxy/metrics"),
    ("node_exporter_metrics", "apiserver", "/api/v1/namespaces/default/services/prometheus-node-exporter:9100/proxy/metrics"),
]

ACCEPT_HEADERS = {
    "prom004": "text/plain;version=0.0.4;escaping=allow-utf-8",
    "om100": "application/openmetrics-text;version=1.0.0;charset=utf-8;escaping=allow-utf-8",
    "om2rc": "application/openmetrics-text;version=2.0.0-rc;charset=utf-8;escaping=allow-utf-8",
}


def fetch_raw_endpoints(out_raw_dir, work_dir, apiserver_addr):
    os.makedirs(out_raw_dir, exist_ok=True)
    summary = {}
    for ep_name, kind, path in ROOT_ENDPOINTS:
        summary[ep_name] = {}
        for fmt_key, accept in ACCEPT_HEADERS.items():
            hdr_file = f"/tmp/om2_hdr_{ep_name}_{fmt_key}.txt"
            out_file = os.path.join(out_raw_dir, f"{ep_name}.{fmt_key}.txt")
            if kind == "apiserver":
                cmd = [
                    "curl", "-sk",
                    "--cacert", os.path.join(work_dir, "ca.crt"),
                    "--cert", os.path.join(work_dir, "client.crt"),
                    "--key", os.path.join(work_dir, "client.key"),
                    "-D", hdr_file,
                    "-o", out_file,
                    "-H", f"Accept: {accept}",
                    f"https://{apiserver_addr}{path}",
                ]
            else:
                cmd = [
                    "curl", "-sS",
                    "-D", hdr_file,
                    "-o", out_file,
                    "-H", f"Accept: {accept}",
                    f"http://127.0.0.1:10249{path}",
                ]
            subprocess.run(cmd, check=True)
            with open(hdr_file, "r", encoding="utf-8", errors="replace") as hf:
                hdr_lines = [l.strip() for l in hf if l.strip()]
            os.remove(hdr_file)
            status_line = hdr_lines[0] if hdr_lines else ""
            ct = ""
            for hl in hdr_lines:
                if hl.lower().startswith("content-type:"):
                    ct = hl.split(":", 1)[1].strip()
            with open(out_file, "rb") as bf:
                raw_bytes = bf.read()
            text = raw_bytes.decode("utf-8", errors="replace")
            lines = text.splitlines()
            type_cnt = 0
            unit_cnt = 0
            st_cnt = 0
            nh_cnt = 0
            comp_other_cnt = 0
            for ln in lines:
                if ln.startswith("# TYPE "):
                    type_cnt += 1
                elif ln.startswith("# UNIT "):
                    unit_cnt += 1
                elif not ln.startswith("#"):
                    if " st@" in ln:
                        st_cnt += 1
                    if " {count:" in ln:
                        if "schema:" in ln:
                            nh_cnt += 1
                        else:
                            comp_other_cnt += 1
            summary[ep_name][fmt_key] = {
                "status": status_line,
                "content_type": ct,
                "bytes": len(raw_bytes),
                "lines": len(lines),
                "has_eof": text.endswith("# EOF\n"),
                "type_lines": type_cnt,
                "unit_lines": unit_cnt,
                "st_lines": st_cnt,
                "native_hist_composite_lines": nh_cnt,
                "summary_or_other_composite_lines": comp_other_cnt,
            }
    return summary


def http_get_json(url):
    with urllib.request.urlopen(url, timeout=30) as resp:
        return json.loads(resp.read().decode("utf-8"))


def write_prom_config(base_cfg_path, out_cfg_path, native_hist=False, always_classic=True):
    with open(base_cfg_path) as f:
        cfg = yaml.safe_load(f)
    if native_hist:
        cfg["global"]["scrape_native_histograms"] = True
        if always_classic:
            cfg["global"]["always_scrape_classic_histograms"] = True
        else:
            cfg["global"].pop("always_scrape_classic_histograms", None)
    with open(out_cfg_path, "w") as f:
        yaml.safe_dump(cfg, f, sort_keys=False)


def wait_for_prom(port, timeout=90):
    deadline = time.time() + timeout
    last_diag = ""
    while time.time() < deadline:
        try:
            data = http_get_json(f"http://127.0.0.1:{port}/api/v1/targets")
            active = data["data"]["activeTargets"]
            bad_targets = [(t["labels"].get("job"), t["health"], t.get("lastError"), t.get("lastScrape")) for t in active if t["health"] != "up" or t.get("lastScrape", "") == "0001-01-01T00:00:00Z"]
            rules = http_get_json(f"http://127.0.0.1:{port}/api/v1/rules")
            groups = rules["data"]["groups"]
            bad_groups = [(g["name"], g.get("lastEvaluation")) for g in groups if not g.get("lastEvaluation") or g.get("lastEvaluation") == "0001-01-01T00:00:00Z"]
            last_diag = f"targets={len(active)} bad_targets={bad_targets} groups={len(groups)} bad_groups={bad_groups}"
            if len(active) == 12 and not bad_targets and len(groups) == 26 and not bad_groups:
                return True
        except Exception as e:
            last_diag = f"err={e}"
        time.sleep(1.5)
    raise RuntimeError(f"Prometheus on port {port} did not become healthy & evaluate all rules within {timeout}s ({last_diag})")


def collect_prom_data(port):
    targets = http_get_json(f"http://127.0.0.1:{port}/api/v1/targets")["data"]["activeTargets"]
    metadata = http_get_json(f"http://127.0.0.1:{port}/api/v1/metadata")["data"]
    rules = http_get_json(f"http://127.0.0.1:{port}/api/v1/rules")["data"]["groups"]
    alerts = http_get_json(f"http://127.0.0.1:{port}/api/v1/alerts")["data"]["alerts"]
    flags = http_get_json(f"http://127.0.0.1:{port}/api/v1/status/flags")["data"]
    status_cfg = yaml.safe_load(http_get_json(f"http://127.0.0.1:{port}/api/v1/status/config")["data"]["yaml"])
    # Query all active series via instant vector `{__name__!=""}` (preserves classic + native series)
    vec = http_get_json(f"http://127.0.0.1:{port}/api/v1/query?query=" + urllib.parse.quote('{__name__!=""}'))["data"]["result"]
    return {
        "targets": targets,
        "metadata": metadata,
        "rules": rules,
        "alerts": alerts,
        "flags": flags,
        "status_config": status_cfg,
        "vector": vec,
    }


def extract_dashboard_exprs(mixin_dir):
    exprs = set()
    for p in sorted(glob.glob(os.path.join(mixin_dir, "dashboards_out/*.json"))):
        with open(p) as f:
            d = json.load(f)
        def walk(obj):
            if isinstance(obj, dict):
                if "expr" in obj and isinstance(obj["expr"], str) and obj["expr"].strip():
                    exprs.add(obj["expr"])
                for v in obj.values():
                    walk(v)
            elif isinstance(obj, list):
                for v in obj:
                    walk(v)
        walk(d)
    return sorted(exprs)


def normalize_expr(expr):
    # Replace Grafana interval and label template variables just like live dashboard queries.
    s = expr
    s = s.replace("$__rate_interval", "1m").replace("${__rate_interval}", "1m")
    s = s.replace("$__interval", "1m").replace("${__interval}", "1m")
    s = s.replace("$__range", "5m").replace("${__range}", "5m")
    s = s.replace("$resolution", "1m").replace("${resolution}", "1m")
    for var in ("cluster", "instance", "namespace", "node", "pod", "type", "volume", "workload"):
        s = re.sub(rf'(\b[a-zA-Z0-9_]+\s*)=\s*"\$\{{?{var}\}}?"', r'\1=~".*"', s)
        s = s.replace(f"${{{var}}}", ".*").replace(f"${var}", ".*")
    return s


def eval_dashboards(ports_map, mixin_dir):
    exprs = extract_dashboard_exprs(mixin_dir)
    results = {}
    for name, port in ports_map.items():
        per_expr = {}
        errors = []
        non_empty = 0
        for expr in exprs:
            q = normalize_expr(expr)
            url = f"http://127.0.0.1:{port}/api/v1/query?query=" + urllib.parse.quote(q)
            try:
                res = http_get_json(url)
                if res.get("status") != "success":
                    errors.append({"expr": expr, "error": res})
                    per_expr[expr] = -1
                else:
                    cnt = len(res["data"]["result"])
                    per_expr[expr] = cnt
                    if cnt > 0:
                        non_empty += 1
            except Exception as e:
                errors.append({"expr": expr, "error": str(e)})
                per_expr[expr] = -1
        results[name] = {
            "total_exprs": len(exprs),
            "non_empty_count": non_empty,
            "error_count": len(errors),
            "errors": errors,
            "per_expr_counts": per_expr,
        }
    return exprs, results


def bootstrap_workdir(work_dir, mixin_dir):
    """Populate certs and Prometheus configs from kubeconfig + om2-experiment/prometheus-configs/ if missing."""
    os.makedirs(work_dir, exist_ok=True)
    need_certs = not all(os.path.exists(os.path.join(work_dir, f)) for f in ("ca.crt", "client.crt", "client.key"))
    raw_kcfg = subprocess.check_output(
        ["kubectl", "--context", "kind-kubernetes-mixin", "config", "view", "--raw", "--minify", "--flatten"],
        text=True,
    )
    kcfg = yaml.safe_load(raw_kcfg)
    server_url = kcfg["clusters"][0]["cluster"]["server"]
    apiserver_addr = server_url.replace("https://", "").replace("http://", "").rstrip("/")
    if need_certs:
        import base64
        with open(os.path.join(work_dir, "ca.crt"), "wb") as f:
            f.write(base64.b64decode(kcfg["clusters"][0]["cluster"]["certificate-authority-data"]))
        with open(os.path.join(work_dir, "client.crt"), "wb") as f:
            f.write(base64.b64decode(kcfg["users"][0]["user"]["client-certificate-data"]))
        with open(os.path.join(work_dir, "client.key"), "wb") as f:
            f.write(base64.b64decode(kcfg["users"][0]["user"]["client-key-data"]))

    script_dir = os.path.dirname(os.path.abspath(__file__))
    tpl_dir = os.path.join(script_dir, "prometheus-configs")
    for cfg_name in ("prom-004.yml", "prom-om1.yml", "prom-om2.yml", "prom-om2-nh.yml"):
        dst = os.path.join(work_dir, cfg_name)
        if not os.path.exists(dst) and os.path.isdir(tpl_dir):
            with open(os.path.join(tpl_dir, cfg_name)) as f:
                txt = f.read()
            txt = txt.replace("./kubernetes-mixin/", mixin_dir.rstrip("/") + "/")
            txt = txt.replace("./certs/", work_dir.rstrip("/") + "/")
            txt = txt.replace("127.0.0.1:37155", apiserver_addr)
            with open(dst, "w") as f:
                f.write(txt)
    return apiserver_addr


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--work-dir", default="/tmp/om2-test")
    parser.add_argument("--mixin-dir", default="/tmp/gh_workspaces/kubernetes-sigs_kubernetes-mixin_test-om2-mixin")
    parser.add_argument("--prom-bin", default="/tmp/prometheus-bin")
    parser.add_argument("--out-dir", default="/tmp/om2-test/evidence")
    args = parser.parse_args()

    os.makedirs(args.out_dir, exist_ok=True)
    raw_dir = os.path.join(args.out_dir, "raw")

    apiserver_addr = bootstrap_workdir(args.work_dir, args.mixin_dir)

    procs = []
    temp_dirs = []
    try:
        # 1. Start kubectl port-forward for kube-proxy
        kube_proxy_pod = subprocess.check_output(
            ["kubectl", "--context", "kind-kubernetes-mixin", "get", "pods", "-n", "kube-system", "-l", "k8s-app=kube-proxy", "-o", "jsonpath={.items[0].metadata.name}"],
            text=True,
        ).strip()
        pf_proc = subprocess.Popen(
            ["kubectl", "--context", "kind-kubernetes-mixin", "port-forward", "-n", "kube-system", f"pod/{kube_proxy_pod}", "10249:10249"],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )
        procs.append(pf_proc)
        # Wait for 10249
        for _ in range(30):
            try:
                urllib.request.urlopen("http://127.0.0.1:10249/metrics", timeout=1).close()
                break
            except Exception:
                time.sleep(0.3)

        # 2. Fetch raw snapshots for all 12 endpoints x 3 formats
        endpoint_summary = fetch_raw_endpoints(raw_dir, args.work_dir, apiserver_addr)
        with open(os.path.join(args.out_dir, "endpoint_negotiation.json"), "w") as f:
            json.dump(endpoint_summary, f, indent=2, sort_keys=True)

        # 3. Create temp prom-om2-nh-default.yml
        nh_def_cfg = os.path.join(args.work_dir, "prom-om2-nh-default.yml")
        write_prom_config(os.path.join(args.work_dir, "prom-om2.yml"), nh_def_cfg, native_hist=True, always_classic=False)

        instances = [
            ("004", 9091, os.path.join(args.work_dir, "prom-004.yml"), False),
            ("om1", 9092, os.path.join(args.work_dir, "prom-om1.yml"), False),
            ("om2", 9093, os.path.join(args.work_dir, "prom-om2.yml"), True),
            ("om2-nh", 9094, os.path.join(args.work_dir, "prom-om2-nh.yml"), True),
            ("om2-nh-default", 9095, nh_def_cfg, True),
        ]

        for name, port, cfg_path, om2_flag in instances:
            tsdb_dir = os.path.join(args.work_dir, f"verify-data-{name}")
            shutil.rmtree(tsdb_dir, ignore_errors=True)
            os.makedirs(tsdb_dir, exist_ok=True)
            temp_dirs.append(tsdb_dir)
            cmd = [
                args.prom_bin,
                f"--config.file={cfg_path}",
                f"--storage.tsdb.path={tsdb_dir}",
                f"--web.listen-address=127.0.0.1:{port}",
            ]
            if om2_flag:
                cmd.append("--enable-feature=openmetrics2")
            p = subprocess.Popen(cmd, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            procs.append(p)

        for name, port, _, _ in instances:
            wait_for_prom(port, timeout=220)
        # Allow 2 extra scrape/eval cycles so rate()-based recording rules & dashboards populate
        time.sleep(16)

        snapshots = {}
        ports_map = {name: port for name, port, _, _ in instances}
        for name, port in ports_map.items():
            snapshots[name] = collect_prom_data(port)

        # Targets summary
        targets_summary = {}
        for name, snap in snapshots.items():
            targets_summary[name] = [
                {
                    "job": t["labels"].get("job"),
                    "scrapePool": t.get("scrapePool"),
                    "health": t.get("health"),
                    "lastError": t.get("lastError"),
                    "lastScrapeDuration": t.get("lastScrapeDuration"),
                }
                for t in snap["targets"]
            ]
        with open(os.path.join(args.out_dir, "targets_summary.json"), "w") as f:
            json.dump(targets_summary, f, indent=2)

        # Prometheus runtime info (binary version, enable-feature flag, effective scrape_protocols)
        prom_version = subprocess.check_output([args.prom_bin, "--version"], text=True).strip()
        runtime_info = {"prometheus_version": prom_version, "instances": {}}
        for name, snap in snapshots.items():
            sc = snap["status_config"] or {}
            g = sc.get("global", {})
            protocols = sorted({tuple(j.get("scrape_protocols", [])) for j in sc.get("scrape_configs", [])})
            runtime_info["instances"][name] = {
                "enable_feature": snap["flags"].get("enable-feature", ""),
                "global": {
                    "scrape_interval": g.get("scrape_interval"),
                    "evaluation_interval": g.get("evaluation_interval"),
                    "scrape_native_histograms": g.get("scrape_native_histograms", False),
                    "always_scrape_classic_histograms": g.get("always_scrape_classic_histograms", False),
                },
                "scrape_protocols_configured": [list(p) for p in protocols],
            }
        with open(os.path.join(args.out_dir, "prometheus_runtime_info.json"), "w") as f:
            json.dump(runtime_info, f, indent=2)

        # Series & label-value parity
        def build_series_sets(vec):
            by_job_metric_labelnames = defaultdict(set)
            by_job_metric_labelvalues = defaultdict(set)
            by_job_count = Counter()
            nh_metrics = Counter()
            for item in vec:
                m = dict(item["metric"])
                job = m.get("job", "")
                name = m.get("__name__", "")
                by_job_count[job] += 1
                by_job_metric_labelnames[(job, name)].add(tuple(sorted(m.keys())))
                by_job_metric_labelvalues[(job, name)].add(tuple(sorted(m.items())))
                if "histogram" in item:
                    nh_metrics[(job, name)] += 1
            return by_job_count, by_job_metric_labelnames, by_job_metric_labelvalues, nh_metrics

        s_info = {n: build_series_sets(snapshots[n]["vector"]) for n in ports_map}

        # Compare 004 vs om1 vs om2 on job/metric labelnames and exact labelvalues
        pairs_004 = set(s_info["004"][1].keys())
        pairs_om1 = set(s_info["om1"][1].keys())
        pairs_om2 = set(s_info["om2"][1].keys())

        labelname_diffs_004_om2 = []
        labelvalue_diffs_004_om2 = []
        for k in sorted(pairs_004 | pairs_om2):
            if s_info["004"][1].get(k) != s_info["om2"][1].get(k):
                labelname_diffs_004_om2.append({"pair": list(k), "004": len(s_info["004"][1].get(k, ())), "om2": len(s_info["om2"][1].get(k, ()))})
            if s_info["004"][2].get(k) != s_info["om2"][2].get(k):
                only_004 = [dict(x) for x in sorted(s_info["004"][2].get(k, set()) - s_info["om2"][2].get(k, set()))]
                only_om2 = [dict(x) for x in sorted(s_info["om2"][2].get(k, set()) - s_info["004"][2].get(k, set()))]
                labelvalue_diffs_004_om2.append({"pair": list(k), "only_004_count": len(only_004), "only_om2_count": len(only_om2), "sample_only_004": only_004[:3], "sample_only_om2": only_om2[:3]})

        # Specific checks for histogram `le` values, summary `quantile` values, and counters
        le_004 = sorted({dict(x).get("le") for k, vs in s_info["004"][2].items() for x in vs if "le" in dict(x)})
        le_om2 = sorted({dict(x).get("le") for k, vs in s_info["om2"][2].items() for x in vs if "le" in dict(x)})
        q_004 = sorted({dict(x).get("quantile") for k, vs in s_info["004"][2].items() for x in vs if "quantile" in dict(x)})
        q_om2 = sorted({dict(x).get("quantile") for k, vs in s_info["om2"][2].items() for x in vs if "quantile" in dict(x)})

        series_parity = {
            "total_series_by_mode": {n: sum(s_info[n][0].values()) for n in ports_map},
            "series_per_job_by_mode": {n: dict(sorted(s_info[n][0].items())) for n in ports_map},
            "unique_job_metric_pairs": {"004": len(pairs_004), "om1": len(pairs_om1), "om2": len(pairs_om2)},
            "missing_pairs_004_vs_om2": sorted(list(k) for k in (pairs_004 - pairs_om2)),
            "extra_pairs_004_vs_om2": sorted(list(k) for k in (pairs_om2 - pairs_004)),
            "labelname_diffs_004_vs_om2": labelname_diffs_004_om2,
            "labelvalue_diffs_004_vs_om2_count": len(labelvalue_diffs_004_om2),
            "labelvalue_diffs_004_vs_om2": labelvalue_diffs_004_om2,
            "histogram_le_values_match": le_004 == le_om2,
            "histogram_le_values_count": len(le_004),
            "summary_quantile_values_match": q_004 == q_om2,
            "summary_quantile_values": q_004,
            "native_histogram_series_counts": {
                n: {f"{j}/{m}": c for (j, m), c in sorted(s_info[n][3].items())}
                for n in ["om2", "om2-nh", "om2-nh-default"]
            },
        }
        with open(os.path.join(args.out_dir, "series_and_labels_parity.json"), "w") as f:
            json.dump(series_parity, f, indent=2)

        # Metadata diff (# TYPE, # HELP, # UNIT via /api/v1/metadata and raw captures)
        m004 = snapshots["004"]["metadata"]
        mom1 = snapshots["om1"]["metadata"]
        mom2 = snapshots["om2"]["metadata"]
        type_diffs_004_om2 = []
        help_diffs_004_om2 = []
        unit_diffs_004_om2 = []
        for k in sorted(set(m004) & set(mom2)):
            t0 = sorted({x.get("type", "") for x in m004[k]})
            t2 = sorted({x.get("type", "") for x in mom2[k]})
            if t0 != t2:
                type_diffs_004_om2.append({"metric": k, "004": t0, "om2": t2})
            h0 = sorted({x.get("help", "") for x in m004[k]})
            h2 = sorted({x.get("help", "") for x in mom2[k]})
            if h0 != h2:
                help_diffs_004_om2.append({"metric": k, "004": h0, "om2": h2})
            u0 = sorted({x.get("unit", "") for x in m004[k]})
            u2 = sorted({x.get("unit", "") for x in mom2[k]})
            if u0 != u2:
                unit_diffs_004_om2.append({"metric": k, "004": u0, "om2": u2})

        raw_help_diffs_004_om2 = []
        for ep_name, _, _ in ROOT_ENDPOINTS:
            def parse_helps(p):
                hs = {}
                with open(p, encoding="utf-8", errors="replace") as rf:
                    for ln in rf:
                        if ln.startswith("# HELP "):
                            parts = ln.rstrip("\n").split(" ", 3)
                            if len(parts) >= 4:
                                hs[parts[2]] = parts[3]
                return hs
            h_raw_004 = parse_helps(os.path.join(raw_dir, f"{ep_name}.prom004.txt"))
            h_raw_om2 = parse_helps(os.path.join(raw_dir, f"{ep_name}.om2rc.txt"))
            if h_raw_004 != h_raw_om2:
                raw_help_diffs_004_om2.append({"endpoint": ep_name, "missing": sorted(set(h_raw_004) - set(h_raw_om2)), "extra": sorted(set(h_raw_om2) - set(h_raw_004))})

        unknown_in_om1 = sorted(k for k, v in mom1.items() if any(x.get("type") == "unknown" for x in v) and any(x.get("type") == "counter" for x in mom2.get(k, [])))
        metadata_diff = {
            "family_counts": {"004": len(m004), "om1": len(mom1), "om2": len(mom2)},
            "missing_families_004_vs_om2": sorted(set(m004) - set(mom2)),
            "extra_families_004_vs_om2": sorted(set(mom2) - set(m004)),
            "only_in_om1_vs_004": sorted(set(mom1) - set(m004)),
            "only_in_004_vs_om1": sorted(set(m004) - set(mom1)),
            "type_diffs_004_vs_om2": type_diffs_004_om2,
            "help_diffs_004_vs_om2": help_diffs_004_om2,
            "raw_help_diffs_004_vs_om2": raw_help_diffs_004_om2,
            "unit_diffs_004_vs_om2_count": len(unit_diffs_004_om2),
            "unit_diffs_004_vs_om2_sample": unit_diffs_004_om2[:15],
            "om1_counter_downgraded_to_unknown": unknown_in_om1,
        }
        with open(os.path.join(args.out_dir, "metadata_diff.json"), "w") as f:
            json.dump(metadata_diff, f, indent=2)

        # Rules & alerts parity (query recording rule result counts + rule group health)
        rec_names = sorted({r["name"] for g in snapshots["004"]["rules"] for r in g["rules"] if r["type"] == "recording"})
        rules_report = {}
        for name, snap in snapshots.items():
            unhealthy = [
                {"group": g["name"], "rule": r["name"], "health": r["health"], "lastError": r.get("lastError", "")}
                for g in snap["rules"]
                for r in g["rules"]
                if r.get("health") != "ok"
            ]
            rec_counts = {}
            for rn in rec_names:
                res = http_get_json(f"http://127.0.0.1:{ports_map[name]}/api/v1/query?query=" + urllib.parse.quote(rn))["data"]["result"]
                rec_counts[rn] = len(res)
            rules_report[name] = {
                "groups_count": len(snap["rules"]),
                "unhealthy_rules": unhealthy,
                "active_alerts": snap["alerts"],
                "recording_rule_counts": rec_counts,
                "non_empty_recording_rules": sum(1 for v in rec_counts.values() if v > 0),
            }
        # Diff recording rule counts vs 004
        rules_diff_vs_004 = {}
        for name in ["om1", "om2", "om2-nh", "om2-nh-default"]:
            diffs = []
            for rn in rec_names:
                c0 = rules_report["004"]["recording_rule_counts"][rn]
                cn = rules_report[name]["recording_rule_counts"][rn]
                if c0 != cn:
                    diffs.append({"record": rn, "004_series": c0, f"{name}_series": cn})
            rules_diff_vs_004[name] = diffs
        with open(os.path.join(args.out_dir, "rules_and_alerts_parity.json"), "w") as f:
            json.dump({"by_mode": rules_report, "recording_diffs_vs_004": rules_diff_vs_004}, f, indent=2)

        # Dashboard query parity
        exprs, dash_res = eval_dashboards(ports_map, args.mixin_dir)
        dash_diffs_vs_004 = {}
        for name in ["om1", "om2", "om2-nh", "om2-nh-default"]:
            diffs = []
            for e in exprs:
                c0 = dash_res["004"]["per_expr_counts"][e]
                cn = dash_res[name]["per_expr_counts"][e]
                if (c0 > 0) != (cn > 0) or (c0 == -1) != (cn == -1):
                    diffs.append({"expr": e, "004_count": c0, f"{name}_count": cn})
            dash_diffs_vs_004[name] = diffs
        dash_summary = {
            name: {k: v for k, v in d.items() if k != "per_expr_counts"}
            for name, d in dash_res.items()
        }
        empty_in_004_and_om2 = [e for e in exprs if dash_res["004"]["per_expr_counts"][e] == 0 and dash_res["om2"]["per_expr_counts"][e] == 0]
        with open(os.path.join(args.out_dir, "dashboards_query_parity.json"), "w") as f:
            json.dump({
                "summary": dash_summary,
                "non_empty_diffs_vs_004": dash_diffs_vs_004,
                "empty_in_both_004_and_om2_count": len(empty_in_004_and_om2),
                "empty_in_both_004_and_om2_sample": empty_in_004_and_om2[:25],
            }, f, indent=2)

        # Check otel-collector + lgtm inside cluster
        otel_logs = subprocess.check_output(
            ["kubectl", "--context", "kind-kubernetes-mixin", "logs", "-l", "app.kubernetes.io/name=opentelemetry-collector", "--tail=100"],
            text=True,
        )
        otel_metrics = subprocess.check_output(
            ["kubectl", "--context", "kind-kubernetes-mixin", "exec", "deploy/otel-collector-deployment-opentelemetry-collector", "--", "wget", "-qO-", "http://127.0.0.1:8888/metrics"],
            text=True,
        ) if False else ""
        # Check otel-collector accepted points or errors via kubectl get --raw proxy to pod metrics or logs
        otel_pod = subprocess.check_output(
            ["kubectl", "--context", "kind-kubernetes-mixin", "get", "pods", "-l", "app.kubernetes.io/name=opentelemetry-collector", "-o", "jsonpath={.items[0].metadata.name}"],
            text=True,
        ).strip()
        otel_internal = subprocess.check_output(
            ["kubectl", "--context", "kind-kubernetes-mixin", "get", "--raw", f"/api/v1/namespaces/default/pods/{otel_pod}:8888/proxy/metrics"],
            text=True,
        )
        otel_summary = {
            "pod": otel_pod,
            "error_lines_in_logs": [ln for ln in otel_logs.splitlines() if "error" in ln.lower()],
            "otelcol_process_uptime_and_points": [
                ln for ln in otel_internal.splitlines()
                if any(k in ln for k in ["otelcol_receiver_accepted_metric_points", "otelcol_receiver_refused_metric_points", "otelcol_exporter_sent_metric_points", "otelcol_exporter_send_failed_metric_points"]) and not ln.startswith("#")
            ],
        }
        with open(os.path.join(args.out_dir, "otel_collector_check.json"), "w") as f:
            json.dump(otel_summary, f, indent=2)

        print("DONE! Evidence saved to:", args.out_dir)
    finally:
        for p in procs:
            try:
                p.terminate()
            except Exception:
                pass
        for p in procs:
            try:
                p.wait(timeout=5)
            except Exception:
                try:
                    p.kill()
                except Exception:
                    pass
        for td in temp_dirs:
            shutil.rmtree(td, ignore_errors=True)


if __name__ == "__main__":
    main()
