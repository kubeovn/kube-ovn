# Makefile for building and pushing Docker images

COMMIT = git-$(shell git rev-parse --short HEAD)
DATE = $(shell date +"%Y-%m-%d_%H:%M:%S")
IMAGE_BUILD_TARGETS = build-kube-ovn build-kube-ovn-dpdk build-dev build-debug base-amd64 base-amd64-dpdk base-arm64 build-kit image-kube-ovn image-kube-ovn-arm64 image-kube-ovn-debug image-kube-ovn-dpdk image-vpc-nat-gateway image-test release release-arm release-arm-debug push-release local-dev
ifneq ($(filter $(IMAGE_BUILD_TARGETS),$(MAKECMDGOALS)),)
IMAGE_REVISION ?= $(if $(GITHUB_SHA),$(GITHUB_SHA),$(shell git rev-parse HEAD))
IMAGE_REF_NAME ?= $(if $(GITHUB_HEAD_REF),$(GITHUB_HEAD_REF),$(if $(GITHUB_REF_NAME),$(GITHUB_REF_NAME),$(shell git symbolic-ref -q --short HEAD || git describe --tags --exact-match 2>/dev/null || git rev-parse --short HEAD)))
IMAGE_REVISION := $(IMAGE_REVISION)
IMAGE_REF_NAME := $(IMAGE_REF_NAME)
endif
IMAGE_LABELS = --label "org.opencontainers.image.source=github.com/kubeovn/kube-ovn" --label "org.opencontainers.image.revision=$(IMAGE_REVISION)" --label "org.opencontainers.image.ref.name=$(IMAGE_REF_NAME)"

GOLDFLAGS = -extldflags '-z now' -X github.com/kubeovn/kube-ovn/versions.COMMIT=$(COMMIT) -X github.com/kubeovn/kube-ovn/versions.VERSION=$(RELEASE_TAG) -X github.com/kubeovn/kube-ovn/versions.BUILDDATE=$(DATE)
ifdef DEBUG
GO_BUILD_FLAGS = -ldflags "$(GOLDFLAGS)"
else
GO_BUILD_FLAGS = -trimpath -ldflags "-w -s $(GOLDFLAGS)"
endif

GO_MOD_VERSION := $(shell awk '/^go[[:space:]]+/ { print $$2; exit }' go.mod)
ifeq ($(strip $(GO_MOD_VERSION)),)
$(error failed to determine Go version from go.mod)
endif
GOTOOLCHAIN_VERSION := go$(GO_MOD_VERSION)
MODERNIZE_ENV := GOTOOLCHAIN=$(GOTOOLCHAIN_VERSION)

.PHONY: build-go
build-go:
	go mod tidy
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(GO_BUILD_FLAGS) -o $(CURDIR)/dist/images/kube-ovn -v ./cmd/cni
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(GO_BUILD_FLAGS) -buildmode=pie -o $(CURDIR)/dist/images/kube-ovn-cmd -v ./cmd
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(GO_BUILD_FLAGS) -buildmode=pie -o $(CURDIR)/dist/images/kube-ovn-daemon -v ./cmd/daemon
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(GO_BUILD_FLAGS) -buildmode=pie -o $(CURDIR)/dist/images/kube-ovn-controller -v ./cmd/controller
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(GO_BUILD_FLAGS) -o $(CURDIR)/dist/images/test-server -v ./test/server

.PHONY: build-go-arm
build-go-arm:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build $(GO_BUILD_FLAGS) -o $(CURDIR)/dist/images/kube-ovn -v ./cmd/cni
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build $(GO_BUILD_FLAGS) -buildmode=pie -o $(CURDIR)/dist/images/kube-ovn-cmd -v ./cmd
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build $(GO_BUILD_FLAGS) -buildmode=pie -o $(CURDIR)/dist/images/kube-ovn-daemon -v ./cmd/daemon
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build $(GO_BUILD_FLAGS) -buildmode=pie -o $(CURDIR)/dist/images/kube-ovn-controller -v ./cmd/controller

.PHONY: build-kube-ovn
build-kube-ovn: build-debug build-go
	docker build $(IMAGE_LABELS) -t $(REGISTRY)/kube-ovn:$(RELEASE_TAG) --build-arg VERSION=$(RELEASE_TAG) -f dist/images/Dockerfile dist/images/
	docker build $(IMAGE_LABELS) -t $(REGISTRY)/kube-ovn:$(LEGACY_TAG) --build-arg VERSION=$(LEGACY_TAG) -f dist/images/Dockerfile dist/images/

.PHONY: build-kube-ovn-dpdk
build-kube-ovn-dpdk: build-go
	docker build $(IMAGE_LABELS) -t $(REGISTRY)/kube-ovn:$(RELEASE_TAG)-dpdk --build-arg BASE_TAG=$(RELEASE_TAG)-dpdk -f dist/images/Dockerfile dist/images/

.PHONY: build-dev
build-dev: build-go
	docker build $(IMAGE_LABELS) -t $(REGISTRY)/kube-ovn:$(DEV_TAG) --build-arg VERSION=$(RELEASE_TAG) -f dist/images/Dockerfile dist/images/

.PHONY: build-debug
build-debug:
	@DEBUG=1 $(MAKE) build-go
	docker build $(IMAGE_LABELS) -t $(REGISTRY)/kube-ovn:$(DEBUG_TAG) --build-arg BASE_TAG=$(DEBUG_TAG) -f dist/images/Dockerfile dist/images/

.PHONY: base-amd64
base-amd64:
	docker buildx build $(IMAGE_LABELS) --platform linux/amd64 --build-arg ARCH=amd64 --build-arg GO_VERSION --build-arg TRIVY_DB_REPOSITORY -t $(REGISTRY)/kube-ovn-base:$(RELEASE_TAG)-amd64 -o type=docker -f dist/images/Dockerfile.base dist/images/
	docker buildx build $(IMAGE_LABELS) --platform linux/amd64 --build-arg ARCH=amd64 --build-arg GO_VERSION --build-arg TRIVY_DB_REPOSITORY --build-arg LEGACY=true -t $(REGISTRY)/kube-ovn-base:$(LEGACY_TAG) -o type=docker -f dist/images/Dockerfile.base dist/images/
	docker buildx build $(IMAGE_LABELS) --platform linux/amd64 --build-arg ARCH=amd64 --build-arg GO_VERSION --build-arg TRIVY_DB_REPOSITORY --build-arg DEBUG=true -t $(REGISTRY)/kube-ovn-base:$(DEBUG_TAG)-amd64 -o type=docker -f dist/images/Dockerfile.base dist/images/

.PHONY: base-amd64-dpdk
base-amd64-dpdk:
	docker buildx build $(IMAGE_LABELS) --platform linux/amd64 --build-arg ARCH=amd64 -t $(REGISTRY)/kube-ovn-base:$(RELEASE_TAG)-amd64-dpdk -o type=docker -f dist/images/Dockerfile.base-dpdk dist/images/

.PHONY: base-arm64
base-arm64:
	docker buildx build $(IMAGE_LABELS) --platform linux/arm64 --build-arg ARCH=arm64 --build-arg GO_VERSION --build-arg TRIVY_DB_REPOSITORY -t $(REGISTRY)/kube-ovn-base:$(RELEASE_TAG)-arm64 -o type=docker -f dist/images/Dockerfile.base dist/images/
	docker buildx build $(IMAGE_LABELS) --platform linux/arm64 --build-arg ARCH=arm64 --build-arg GO_VERSION --build-arg TRIVY_DB_REPOSITORY --build-arg DEBUG=true -t $(REGISTRY)/kube-ovn-base:$(DEBUG_TAG)-arm64 -o type=docker -f dist/images/Dockerfile.base dist/images/

.PHONY: build-kit
build-kit: build-go
	DOCKER_BUILDKIT=1 docker build $(IMAGE_LABELS) -t $(REGISTRY)/kube-ovn:$(RELEASE_TAG) --build-arg VERSION=$(RELEASE_TAG) -o type=docker -f dist/images/Dockerfile dist/images/

.PHONY: image-kube-ovn
image-kube-ovn: image-kube-ovn-debug build-go
	docker buildx build $(IMAGE_LABELS) --platform linux/amd64 -t $(REGISTRY)/kube-ovn:$(RELEASE_TAG) --build-arg VERSION=$(RELEASE_TAG) -o type=docker -f dist/images/Dockerfile dist/images/
	docker buildx build $(IMAGE_LABELS) --platform linux/amd64 -t $(REGISTRY)/kube-ovn:$(LEGACY_TAG) --build-arg VERSION=$(LEGACY_TAG) -o type=docker -f dist/images/Dockerfile dist/images/

.PHONY: image-kube-ovn-arm64
image-kube-ovn-arm64: build-go-arm
	docker buildx build $(IMAGE_LABELS) --platform linux/arm64 -t $(REGISTRY)/kube-ovn:$(RELEASE_TAG) --build-arg VERSION=$(RELEASE_TAG) -o type=docker -f dist/images/Dockerfile dist/images/

.PHONY: image-kube-ovn-debug
image-kube-ovn-debug:
	@DEBUG=1 $(MAKE) build-go
	docker buildx build $(IMAGE_LABELS) --platform linux/amd64 -t $(REGISTRY)/kube-ovn:$(DEBUG_TAG) --build-arg BASE_TAG=$(DEBUG_TAG) -o type=docker -f dist/images/Dockerfile dist/images/

.PHONY: image-kube-ovn-dpdk
image-kube-ovn-dpdk: build-go
	docker buildx build $(IMAGE_LABELS) --platform linux/amd64 -t $(REGISTRY)/kube-ovn:$(RELEASE_TAG)-dpdk --build-arg VERSION=$(RELEASE_TAG) --build-arg BASE_TAG=$(RELEASE_TAG)-dpdk -o type=docker -f dist/images/Dockerfile dist/images/

.PHONY: image-vpc-nat-gateway
image-vpc-nat-gateway:
	docker buildx build $(IMAGE_LABELS) --platform linux/amd64 -t $(REGISTRY)/vpc-nat-gateway:$(RELEASE_TAG) -o type=docker -f dist/images/vpcnatgateway/Dockerfile dist/images/vpcnatgateway

.PHONY: image-test
image-test: build-go
	docker buildx build $(IMAGE_LABELS) --platform linux/amd64 -t $(REGISTRY)/test:$(RELEASE_TAG) -o type=docker -f dist/images/Dockerfile.test dist/images/

.PHONY: release
release: lint image-kube-ovn image-vpc-nat-gateway

.PHONY: release-arm
release-arm: release-arm-debug image-kube-ovn-arm64
	docker buildx build $(IMAGE_LABELS) --platform linux/arm64 -t $(REGISTRY)/vpc-nat-gateway:$(RELEASE_TAG) -o type=docker -f dist/images/vpcnatgateway/Dockerfile dist/images/vpcnatgateway

.PHONY: release-arm-debug
release-arm-debug:
	@DEBUG=1 $(MAKE) build-go-arm
	docker buildx build $(IMAGE_LABELS) --platform linux/arm64 -t $(REGISTRY)/kube-ovn:$(DEBUG_TAG) --build-arg BASE_TAG=$(DEBUG_TAG) -o type=docker -f dist/images/Dockerfile dist/images/

.PHONY: push-dev
push-dev:
	docker push $(REGISTRY)/kube-ovn:$(DEV_TAG)

.PHONY: push-release
push-release: release
	docker push $(REGISTRY)/kube-ovn:$(RELEASE_TAG)

.PHONY: tar-kube-ovn
tar-kube-ovn:
	docker save $(REGISTRY)/kube-ovn:$(RELEASE_TAG) $(REGISTRY)/kube-ovn:$(LEGACY_TAG) $(REGISTRY)/kube-ovn:$(DEBUG_TAG) -o kube-ovn.tar

.PHONY: tar-kube-ovn-dpdk
tar-kube-ovn-dpdk:
	docker save $(REGISTRY)/kube-ovn:$(RELEASE_TAG)-dpdk -o kube-ovn-dpdk.tar

.PHONY: tar-vpc-nat-gateway
tar-vpc-nat-gateway:
	docker save $(REGISTRY)/vpc-nat-gateway:$(RELEASE_TAG) -o vpc-nat-gateway.tar

.PHONY: tar
tar: tar-kube-ovn tar-vpc-nat-gateway

.PHONY: base-tar-amd64
base-tar-amd64:
	docker save $(REGISTRY)/kube-ovn-base:$(RELEASE_TAG)-amd64 $(REGISTRY)/kube-ovn-base:$(LEGACY_TAG) $(REGISTRY)/kube-ovn-base:$(DEBUG_TAG)-amd64 -o image-amd64.tar

.PHONY: base-tar-amd64-dpdk
base-tar-amd64-dpdk:
	docker save $(REGISTRY)/kube-ovn-base:$(RELEASE_TAG)-amd64-dpdk -o image-amd64-dpdk.tar

.PHONY: base-tar-arm64
base-tar-arm64:
	docker save $(REGISTRY)/kube-ovn-base:$(RELEASE_TAG)-arm64 $(REGISTRY)/kube-ovn-base:$(DEBUG_TAG)-arm64 -o image-arm64.tar

.PHONY: lint
lint:
    ifeq ($(CI),true)
		@echo "Running in GitHub Actions"
		golangci-lint run -v
		$(MODERNIZE_ENV) go tool github.com/kubeovn/kube-ovn/tools/modernize -test -skipgenerated ./...
    else
		@echo "Running in local environment"
		golangci-lint run -v --fix
		$(MODERNIZE_ENV) go tool github.com/kubeovn/kube-ovn/tools/modernize -test -skipgenerated -fix ./...
    endif

.PHONY: scan
scan:
	trivy image --exit-code=1 --ignore-unfixed --scanners vuln $(REGISTRY)/kube-ovn:$(RELEASE_TAG)
	trivy image --exit-code=1 --ignore-unfixed --scanners vuln $(REGISTRY)/vpc-nat-gateway:$(RELEASE_TAG)
