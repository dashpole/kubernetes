load("@io_bazel_rules_go//go:def.bzl", "go_library")

go_library(
    name = "go_default_library",
    srcs = [
        "grpctrace.go",
        "interceptor.go",
    ],
    importmap = "k8s.io/kubernetes/vendor/go.opentelemetry.io/otel/plugin/grpctrace",
    importpath = "go.opentelemetry.io/otel/plugin/grpctrace",
    visibility = ["//visibility:public"],
    deps = [
        "//vendor/github.com/golang/protobuf/proto:go_default_library",
        "//vendor/go.opentelemetry.io/otel/api/correlation:go_default_library",
        "//vendor/go.opentelemetry.io/otel/api/global:go_default_library",
        "//vendor/go.opentelemetry.io/otel/api/kv:go_default_library",
        "//vendor/go.opentelemetry.io/otel/api/propagation:go_default_library",
        "//vendor/go.opentelemetry.io/otel/api/trace:go_default_library",
        "//vendor/google.golang.org/grpc:go_default_library",
        "//vendor/google.golang.org/grpc/metadata:go_default_library",
        "//vendor/google.golang.org/grpc/peer:go_default_library",
        "//vendor/google.golang.org/grpc/status:go_default_library",
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
