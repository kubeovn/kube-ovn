GOFILES_NOVENDOR=$(shell find . -type f -name '*.go' -not -path "./vendor/*")
GO_VERSION=1.12

REGISTRY=index.alauda.cn/alaudak8s
ROLES=node controller cni db
DEV_TAG=dev
RELEASE_TAG=$(shell cat VERSION)

.PHONY: build-dev-images build-go build-bin test lint up down halt suspend resume

build-dev-images: build-bin
	@for role in ${ROLES} ; do \
		docker build -t ${REGISTRY}/kube-ovn-$$role:${DEV_TAG} -f dist/images/Dockerfile.$$role dist/images/; \
		docker push ${REGISTRY}/kube-ovn-$$role:${DEV_TAG}; \
	done

build-go:
	CGO_ENABLED=0 GOOS=linux go build -o $(PWD)/dist/images/kube-ovn -ldflags "-w -s" -v ./cmd/cni
	CGO_ENABLED=0 GOOS=linux go build -o $(PWD)/dist/images/kube-ovn-controller -ldflags "-w -s" -v ./cmd/controller
	CGO_ENABLED=0 GOOS=linux go build -o $(PWD)/dist/images/kube-ovn-daemon -ldflags "-w -s" -v ./cmd/daemon

release: build-go
	@for role in ${ROLES} ; do \
		docker build -t ${REGISTRY}/kube-ovn-$$role:${RELEASE_TAG} -f dist/images/Dockerfile.$$role dist/images/; \
		docker push ${REGISTRY}/kube-ovn-$$role:${RELEASE_TAG}; \
	done

lint:
	@gofmt -d ${GOFILES_NOVENDOR} 
	@gofmt -l ${GOFILES_NOVENDOR} | read && echo "Code differs from gofmt's style" 1>&2 && exit 1 || true
	@GOOS=linux go vet ./...

test:
	GOOS=linux go test -cover -v ./...

build-bin: lint
	docker run --rm -e GOOS=linux -e GOCACHE=/tmp \
		-u $(shell id -u):$(shell id -g) \
		-v $(CURDIR):/go/src/github.com/alauda/kube-ovn:ro \
		-v $(CURDIR)/dist:/go/src/github.com/alauda/kube-ovn/dist/ \
		golang:$(GO_VERSION) /bin/bash -c '\
		cd /go/src/github.com/alauda/kube-ovn && \
		make test && \
		make build-go '

up:
	cd vagrant && vagrant up

down:
	cd vagrant && vagrant destroy -f

halt:
	cd vagrant && vagrant halt

resume:
	cd vagrant && vagrant resume

suspend:
	cd vagrant && vagrant suspend
