IMAGE_REPO ?= registry.k8s.io/tracing-admission-webhook/tracing-admission-webhook
IMAGE_TAG ?= v0.1.0
BINARY_NAME ?= tracing-admission-webhook

.PHONY: all build test vet helm-lint verify docker-build clean

all: verify build

build:
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/$(BINARY_NAME) ./cmd/tracing-admission-webhook

test:
	go test -race -v -coverprofile=coverage.out ./...

vet:
	go vet ./...

helm-lint:
	helm lint deploy/charts/tracing-admission-webhook

verify: vet test helm-lint

docker-build:
	docker build -t $(IMAGE_REPO):$(IMAGE_TAG) .

clean:
	rm -rf bin/ coverage.out
