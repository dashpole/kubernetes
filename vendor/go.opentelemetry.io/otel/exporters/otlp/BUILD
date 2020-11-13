load("@io_bazel_rules_go//go:def.bzl", "go_library")

go_library(
    name = "go_default_library",
    srcs = [
        "connection.go",
        "doc.go",
        "options.go",
        "otlp.go",
    ],
    importmap = "k8s.io/kubernetes/vendor/go.opentelemetry.io/otel/exporters/otlp",
    importpath = "go.opentelemetry.io/otel/exporters/otlp",
    visibility = ["//visibility:public"],
    deps = [
        "//vendor/github.com/open-telemetry/opentelemetry-proto/gen/go/collector/metrics/v1:go_default_library",
        "//vendor/github.com/open-telemetry/opentelemetry-proto/gen/go/collector/trace/v1:go_default_library",
        "//vendor/go.opentelemetry.io/otel/exporters/otlp/internal/transform:go_default_library",
        "//vendor/go.opentelemetry.io/otel/sdk/export/metric:go_default_library",
        "//vendor/go.opentelemetry.io/otel/sdk/export/trace:go_default_library",
        "//vendor/google.golang.org/grpc:go_default_library",
        "//vendor/google.golang.org/grpc/credentials:go_default_library",
        "//vendor/google.golang.org/grpc/metadata:go_default_library",
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
        "//vendor/go.opentelemetry.io/otel/exporters/otlp/internal/transform:all-srcs",
    ],
    tags = ["automanaged"],
    visibility = ["//visibility:public"],
)
