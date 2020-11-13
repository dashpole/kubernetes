load("@io_bazel_rules_go//go:def.bzl", "go_library")

go_library(
    name = "go_default_library",
    srcs = [
        "doc.go",
        "key.go",
        "kv.go",
    ],
    importmap = "k8s.io/kubernetes/vendor/go.opentelemetry.io/otel/api/kv",
    importpath = "go.opentelemetry.io/otel/api/kv",
    visibility = ["//visibility:public"],
    deps = ["//vendor/go.opentelemetry.io/otel/api/kv/value:go_default_library"],
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
        "//vendor/go.opentelemetry.io/otel/api/kv/value:all-srcs",
    ],
    tags = ["automanaged"],
    visibility = ["//visibility:public"],
)
