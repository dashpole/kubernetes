FROM golang:1.26 AS builder

WORKDIR /workspace

COPY go.mod go.sum ./
RUN go mod download

COPY cmd/ cmd/
COPY pkg/ pkg/

ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /workspace/tracing-admission-webhook ./cmd/tracing-admission-webhook

FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/tracing-admission-webhook /tracing-admission-webhook
USER 65532:65532

ENTRYPOINT ["/tracing-admission-webhook"]
