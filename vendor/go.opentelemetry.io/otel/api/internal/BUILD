load("@io_bazel_rules_go//go:def.bzl", "go_library")

go_library(
    name = "go_default_library",
    srcs = ["rawhelpers.go"],
    importmap = "k8s.io/kubernetes/vendor/go.opentelemetry.io/otel/api/internal",
    importpath = "go.opentelemetry.io/otel/api/internal",
    visibility = ["//vendor/go.opentelemetry.io/otel/api:__subpackages__"],
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
