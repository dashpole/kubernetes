load("@io_bazel_rules_go//go:def.bzl", "go_library")

go_library(
    name = "go_default_library",
    srcs = [
        "atomicfields.go",
        "doc.go",
        "refcount_mapped.go",
        "sdk.go",
    ],
    importmap = "k8s.io/kubernetes/vendor/go.opentelemetry.io/otel/sdk/metric",
    importpath = "go.opentelemetry.io/otel/sdk/metric",
    visibility = ["//visibility:public"],
    deps = [
        "//vendor/go.opentelemetry.io/otel:go_default_library",
        "//vendor/go.opentelemetry.io/otel/internal/metric:go_default_library",
        "//vendor/go.opentelemetry.io/otel/label:go_default_library",
        "//vendor/go.opentelemetry.io/otel/metric:go_default_library",
        "//vendor/go.opentelemetry.io/otel/metric/number:go_default_library",
        "//vendor/go.opentelemetry.io/otel/sdk/export/metric:go_default_library",
        "//vendor/go.opentelemetry.io/otel/sdk/metric/aggregator:go_default_library",
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
        "//vendor/go.opentelemetry.io/otel/sdk/metric/aggregator:all-srcs",
        "//vendor/go.opentelemetry.io/otel/sdk/metric/controller/basic:all-srcs",
        "//vendor/go.opentelemetry.io/otel/sdk/metric/controller/time:all-srcs",
        "//vendor/go.opentelemetry.io/otel/sdk/metric/processor/basic:all-srcs",
        "//vendor/go.opentelemetry.io/otel/sdk/metric/selector/simple:all-srcs",
    ],
    tags = ["automanaged"],
    visibility = ["//visibility:public"],
)
