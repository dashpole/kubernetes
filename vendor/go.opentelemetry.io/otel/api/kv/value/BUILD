load("@io_bazel_rules_go//go:def.bzl", "go_library")

go_library(
    name = "go_default_library",
    srcs = [
        "type_string.go",
        "value.go",
    ],
    importmap = "k8s.io/kubernetes/vendor/go.opentelemetry.io/otel/api/kv/value",
    importpath = "go.opentelemetry.io/otel/api/kv/value",
    visibility = ["//visibility:public"],
    deps = ["//vendor/go.opentelemetry.io/otel/api/internal:go_default_library"],
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
