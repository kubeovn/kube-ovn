GOFILES_NOVENDOR=$(shell find . -type f -name '*.go' -not -path "./vendor/*")
GO_VERSION=1.13

REGISTRY=index.alauda.cn/alaudak8s
ROLES=node controller cni db webhook pinger
DEV_TAG=dev
RELEASE_TAG=$(shell cat VERSION)

.PHONY: build-dev-images build-go build-bin test lint up down halt suspend resume kind

build-dev-images: build-bin
	@for role in ${ROLES} ; do \
		docker build -t ${REGISTRY}/kube-ovn-$$role:${DEV_TAG} -f dist/images/Dockerfile.$$role dist/images/; \
		docker push ${REGISTRY}/kube-ovn-$$role:${DEV_TAG}; \
	done

build-go:
	CGO_ENABLED=0 GOOS=linux go build -o $(PWD)/dist/images/kube-ovn -ldflags "-w -s" -v ./cmd/cni
	CGO_ENABLED=0 GOOS=linux go build -o $(PWD)/dist/images/kube-ovn-controller -ldflags "-w -s" -v ./cmd/controller
	CGO_ENABLED=0 GOOS=linux go build -o $(PWD)/dist/images/kube-ovn-daemon -ldflags "-w -s" -v ./cmd/daemon
	CGO_ENABLED=0 GOOS=linux go build -o $(PWD)/dist/images/kube-ovn-webhook -ldflags "-w -s" -v ./cmd/webhook
	CGO_ENABLED=0 GOOS=linux go build -o $(PWD)/dist/images/kube-ovn-pinger -ldflags "-w -s" -v ./cmd/pinger

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

kind-init:
	kind delete cluster --name=kube-ovn
	kind create cluster --config yamls/kind.yaml --name kube-ovn
	@for role in ${ROLES} ; do \
		kind load docker-image --name kube-ovn ${REGISTRY}/kube-ovn-$$role:${RELEASE_TAG}; \
	done
	kubectl label node kube-ovn-control-plane kube-ovn/role=master
	kubectl apply -f yamls/crd.yaml
	kubectl apply -f yamls/ovn.yaml
	kubectl apply -f yamls/kube-ovn.yaml

kind-init-ha:
	kind delete cluster --name=kube-ovn
	kind create cluster --config yamls/kind.yaml --name kube-ovn
	@for role in ${ROLES} ; do \
		kind load docker-image --name kube-ovn ${REGISTRY}/kube-ovn-$$role:${RELEASE_TAG}; \
	done
	kubectl label node --all kube-ovn/role=master
	kubectl apply -f yamls/crd.yaml
	kubectl apply -f yamls/ovn-ha.yaml
	kubectl apply -f yamls/kube-ovn.yaml

kind-reload:
	@for role in ${ROLES} ; do \
		kind load docker-image ${REGISTRY}/kube-ovn-$$role:${RELEASE_TAG}; \
	done
	kubectl delete pod -n kube-ovn --all

kind-clean:
	kind delete cluster --name=kube-ovn
