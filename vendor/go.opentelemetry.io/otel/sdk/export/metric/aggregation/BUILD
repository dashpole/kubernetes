load("@io_bazel_rules_go//go:def.bzl", "go_library")

go_library(
    name = "go_default_library",
    srcs = ["aggregation.go"],
    importmap = "k8s.io/kubernetes/vendor/go.opentelemetry.io/otel/sdk/export/metric/aggregation",
    importpath = "go.opentelemetry.io/otel/sdk/export/metric/aggregation",
    visibility = ["//visibility:public"],
    deps = ["//vendor/go.opentelemetry.io/otel/metric/number:go_default_library"],
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
