load("@io_bazel_rules_go//go:def.bzl", "go_library")

go_library(
    name = "go_default_library",
    srcs = ["aggregator.go"],
    importmap = "k8s.io/kubernetes/vendor/go.opentelemetry.io/otel/sdk/metric/aggregator",
    importpath = "go.opentelemetry.io/otel/sdk/metric/aggregator",
    visibility = ["//visibility:public"],
    deps = [
        "//vendor/go.opentelemetry.io/otel/metric:go_default_library",
        "//vendor/go.opentelemetry.io/otel/metric/number:go_default_library",
        "//vendor/go.opentelemetry.io/otel/sdk/export/metric:go_default_library",
        "//vendor/go.opentelemetry.io/otel/sdk/export/metric/aggregation:go_default_library",
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
        "//vendor/go.opentelemetry.io/otel/sdk/metric/aggregator/exact:all-srcs",
        "//vendor/go.opentelemetry.io/otel/sdk/metric/aggregator/histogram:all-srcs",
        "//vendor/go.opentelemetry.io/otel/sdk/metric/aggregator/lastvalue:all-srcs",
        "//vendor/go.opentelemetry.io/otel/sdk/metric/aggregator/minmaxsumcount:all-srcs",
        "//vendor/go.opentelemetry.io/otel/sdk/metric/aggregator/sum:all-srcs",
    ],
    tags = ["automanaged"],
    visibility = ["//visibility:public"],
)
