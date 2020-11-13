load("@io_bazel_rules_go//go:def.bzl", "go_library")

go_library(
    name = "go_default_library",
    srcs = ["metrics.pb.go"],
    importmap = "k8s.io/kubernetes/vendor/github.com/open-telemetry/opentelemetry-proto/gen/go/metrics/v1",
    importpath = "github.com/open-telemetry/opentelemetry-proto/gen/go/metrics/v1",
    visibility = ["//visibility:public"],
    deps = [
        "//vendor/github.com/golang/protobuf/proto:go_default_library",
        "//vendor/github.com/open-telemetry/opentelemetry-proto/gen/go/common/v1:go_default_library",
        "//vendor/github.com/open-telemetry/opentelemetry-proto/gen/go/resource/v1:go_default_library",
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
