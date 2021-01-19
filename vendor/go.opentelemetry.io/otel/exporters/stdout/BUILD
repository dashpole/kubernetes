load("@io_bazel_rules_go//go:def.bzl", "go_library")

go_library(
    name = "go_default_library",
    srcs = [
        "config.go",
        "doc.go",
        "exporter.go",
        "metric.go",
        "trace.go",
    ],
    importmap = "k8s.io/kubernetes/vendor/go.opentelemetry.io/otel/exporters/stdout",
    importpath = "go.opentelemetry.io/otel/exporters/stdout",
    visibility = ["//visibility:public"],
    deps = [
        "//vendor/go.opentelemetry.io/otel:go_default_library",
        "//vendor/go.opentelemetry.io/otel/label:go_default_library",
        "//vendor/go.opentelemetry.io/otel/metric:go_default_library",
        "//vendor/go.opentelemetry.io/otel/sdk/export/metric:go_default_library",
        "//vendor/go.opentelemetry.io/otel/sdk/export/metric/aggregation:go_default_library",
        "//vendor/go.opentelemetry.io/otel/sdk/export/trace:go_default_library",
        "//vendor/go.opentelemetry.io/otel/sdk/metric/controller/basic:go_default_library",
        "//vendor/go.opentelemetry.io/otel/sdk/metric/processor/basic:go_default_library",
        "//vendor/go.opentelemetry.io/otel/sdk/metric/selector/simple:go_default_library",
        "//vendor/go.opentelemetry.io/otel/sdk/trace:go_default_library",
        "//vendor/go.opentelemetry.io/otel/trace:go_default_library",
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
