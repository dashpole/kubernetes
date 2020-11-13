load("@io_bazel_rules_go//go:def.bzl", "go_library")

go_library(
    name = "go_default_library",
    srcs = [
        "common.go",
        "config.go",
        "doc.go",
        "handler.go",
        "transport.go",
        "wrap.go",
    ],
    importmap = "k8s.io/kubernetes/vendor/go.opentelemetry.io/otel/plugin/othttp",
    importpath = "go.opentelemetry.io/otel/plugin/othttp",
    visibility = ["//visibility:public"],
    deps = [
        "//vendor/go.opentelemetry.io/otel/api/global:go_default_library",
        "//vendor/go.opentelemetry.io/otel/api/kv:go_default_library",
        "//vendor/go.opentelemetry.io/otel/api/propagation:go_default_library",
        "//vendor/go.opentelemetry.io/otel/api/trace:go_default_library",
        "//vendor/google.golang.org/grpc/codes:go_default_library",
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
