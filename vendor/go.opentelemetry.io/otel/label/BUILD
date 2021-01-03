load("@io_bazel_rules_go//go:def.bzl", "go_library")

go_library(
    name = "go_default_library",
    srcs = [
        "doc.go",
        "encoder.go",
        "iterator.go",
        "key.go",
        "kv.go",
        "set.go",
        "type_string.go",
        "value.go",
    ],
    importmap = "k8s.io/kubernetes/vendor/go.opentelemetry.io/otel/label",
    importpath = "go.opentelemetry.io/otel/label",
    visibility = ["//visibility:public"],
    deps = ["//vendor/go.opentelemetry.io/otel/internal:go_default_library"],
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
