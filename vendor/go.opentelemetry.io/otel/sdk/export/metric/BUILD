load("@io_bazel_rules_go//go:def.bzl", "go_library")

go_library(
    name = "go_default_library",
    srcs = [
        "exportkind_string.go",
        "metric.go",
    ],
    importmap = "k8s.io/kubernetes/vendor/go.opentelemetry.io/otel/sdk/export/metric",
    importpath = "go.opentelemetry.io/otel/sdk/export/metric",
    visibility = ["//visibility:public"],
    deps = [
        "//vendor/go.opentelemetry.io/otel/label:go_default_library",
        "//vendor/go.opentelemetry.io/otel/metric:go_default_library",
        "//vendor/go.opentelemetry.io/otel/metric/number:go_default_library",
        "//vendor/go.opentelemetry.io/otel/sdk/export/metric/aggregation:go_default_library",
        "//vendor/go.opentelemetry.io/otel/sdk/resource:go_default_library",
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
    srcs = [
        ":package-srcs",
        "//vendor/go.opentelemetry.io/otel/sdk/export/metric/aggregation:all-srcs",
    ],
    tags = ["automanaged"],
    visibility = ["//visibility:public"],
)
