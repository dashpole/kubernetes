load("@io_bazel_rules_go//go:def.bzl", "go_library")

go_library(
    name = "go_default_library",
    srcs = ["noop.go"],
    importmap = "k8s.io/kubernetes/vendor/go.opentelemetry.io/otel/internal/trace/noop",
    importpath = "go.opentelemetry.io/otel/internal/trace/noop",
    visibility = ["//vendor/go.opentelemetry.io/otel:__subpackages__"],
    deps = ["//vendor/go.opentelemetry.io/otel/trace:go_default_library"],
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
