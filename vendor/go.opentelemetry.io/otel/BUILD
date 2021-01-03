load("@io_bazel_rules_go//go:def.bzl", "go_library")

go_library(
    name = "go_default_library",
    srcs = [
        "doc.go",
        "error_handler.go",
        "handler.go",
        "propagation.go",
        "trace.go",
        "version.go",
    ],
    importmap = "k8s.io/kubernetes/vendor/go.opentelemetry.io/otel",
    importpath = "go.opentelemetry.io/otel",
    visibility = ["//visibility:public"],
    deps = [
        "//vendor/go.opentelemetry.io/otel/internal/global:go_default_library",
        "//vendor/go.opentelemetry.io/otel/propagation:go_default_library",
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
    srcs = [
        ":package-srcs",
        "//vendor/go.opentelemetry.io/otel/codes:all-srcs",
        "//vendor/go.opentelemetry.io/otel/exporters/stdout:all-srcs",
        "//vendor/go.opentelemetry.io/otel/internal:all-srcs",
        "//vendor/go.opentelemetry.io/otel/label:all-srcs",
        "//vendor/go.opentelemetry.io/otel/metric:all-srcs",
        "//vendor/go.opentelemetry.io/otel/propagation:all-srcs",
        "//vendor/go.opentelemetry.io/otel/sdk/export/metric:all-srcs",
        "//vendor/go.opentelemetry.io/otel/sdk/export/trace:all-srcs",
        "//vendor/go.opentelemetry.io/otel/sdk/instrumentation:all-srcs",
        "//vendor/go.opentelemetry.io/otel/sdk/internal:all-srcs",
        "//vendor/go.opentelemetry.io/otel/sdk/metric:all-srcs",
        "//vendor/go.opentelemetry.io/otel/sdk/resource:all-srcs",
        "//vendor/go.opentelemetry.io/otel/sdk/trace:all-srcs",
        "//vendor/go.opentelemetry.io/otel/semconv:all-srcs",
        "//vendor/go.opentelemetry.io/otel/trace:all-srcs",
        "//vendor/go.opentelemetry.io/otel/unit:all-srcs",
    ],
    tags = ["automanaged"],
    visibility = ["//visibility:public"],
)
