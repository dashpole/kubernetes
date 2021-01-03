load("@io_bazel_rules_go//go:def.bzl", "go_library")

go_library(
    name = "go_default_library",
    srcs = ["rawhelpers.go"],
    importmap = "k8s.io/kubernetes/vendor/go.opentelemetry.io/otel/internal",
    importpath = "go.opentelemetry.io/otel/internal",
    visibility = ["//vendor/go.opentelemetry.io/otel:__subpackages__"],
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
        "//vendor/go.opentelemetry.io/otel/internal/baggage:all-srcs",
        "//vendor/go.opentelemetry.io/otel/internal/global:all-srcs",
        "//vendor/go.opentelemetry.io/otel/internal/metric:all-srcs",
        "//vendor/go.opentelemetry.io/otel/internal/trace/noop:all-srcs",
        "//vendor/go.opentelemetry.io/otel/internal/trace/parent:all-srcs",
    ],
    tags = ["automanaged"],
    visibility = ["//visibility:public"],
)
