load("@io_bazel_rules_go//go:def.bzl", "go_library")

go_library(
    name = "go_default_library",
    srcs = ["utils.go"],
    importmap = "k8s.io/kubernetes/vendor/k8s.io/component-base/traces",
    importpath = "k8s.io/component-base/traces",
    visibility = ["//visibility:public"],
    deps = [
        "//vendor/go.opentelemetry.io/otel/api/global:go_default_library",
        "//vendor/go.opentelemetry.io/otel/api/kv:go_default_library",
        "//vendor/go.opentelemetry.io/otel/exporters/otlp:go_default_library",
        "//vendor/go.opentelemetry.io/otel/sdk/resource:go_default_library",
        "//vendor/go.opentelemetry.io/otel/sdk/trace:go_default_library",
        "//vendor/k8s.io/klog/v2:go_default_library",
    ],
)

filegroup(
    name = "package-srcs",
    srcs = glob(["**"]),
    tags = ["automanaged"],
    visibility = ["//visibility:private"],
)

filegroup(
    name = "all-srcs",
    srcs = [":package-srcs"],
    tags = ["automanaged"],
    visibility = ["//visibility:public"],
)
