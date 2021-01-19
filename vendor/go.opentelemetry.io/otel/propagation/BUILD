load("@io_bazel_rules_go//go:def.bzl", "go_library")

go_library(
    name = "go_default_library",
    srcs = [
        "baggage.go",
        "doc.go",
        "propagation.go",
        "trace_context.go",
    ],
    importmap = "k8s.io/kubernetes/vendor/go.opentelemetry.io/otel/propagation",
    importpath = "go.opentelemetry.io/otel/propagation",
    visibility = ["//visibility:public"],
    deps = [
        "//vendor/go.opentelemetry.io/otel/internal/baggage:go_default_library",
        "//vendor/go.opentelemetry.io/otel/label:go_default_library",
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
