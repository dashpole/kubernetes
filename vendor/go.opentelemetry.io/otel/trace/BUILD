load("@io_bazel_rules_go//go:def.bzl", "go_library")

go_library(
    name = "go_default_library",
    srcs = [
        "config.go",
        "doc.go",
        "trace.go",
        "trace_noop.go",
    ],
    importmap = "k8s.io/kubernetes/vendor/go.opentelemetry.io/otel/trace",
    importpath = "go.opentelemetry.io/otel/trace",
    visibility = ["//visibility:public"],
    deps = [
        "//vendor/go.opentelemetry.io/otel/codes:go_default_library",
        "//vendor/go.opentelemetry.io/otel/label:go_default_library",
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
